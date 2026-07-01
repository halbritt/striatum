package reads

import (
	"context"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/db"
)

const (
	pgReadScopeBroadRuntimeSelect     = "broad_runtime_select"
	pgReadScopePartialProjectionGated = "partial_projection_gated"
	pgReadScopePrivateReadDenial      = "private_read_denial"
)

// pgReadScopeGate is one read-scope closure claim: a surface an owner bundle
// stamps closed, plus how to verify it live. An empty deniedColumns means a
// table-level gate (full runtime SELECT denial); otherwise it is a column gate
// in the bundle 0005 style.
type pgReadScopeGate struct {
	surface       string
	ownerBundle   int
	stamp         string
	deniedColumns []string
}

// pgReadScopeGates is the static doctor gate map — the doctor analog of
// db.readScopeReasserts (RFC 0114). Each stamped gate must hold live:
// has_table_privilege / has_column_privilege('striatumd_rw', ..., 'SELECT')
// false, and for table gates pg_tables.tableowner <> 'striatumd_rw' — the
// ownership probe that catches a missed/undone transfer, which privilege
// probes alone cannot (a runtime-owned table is one self-GRANT from open).
var pgReadScopeGates = []pgReadScopeGate{
	{surface: "clients", ownerBundle: 5, stamp: "auth_projection_read",
		deniedColumns: []string{"token_hash", "token_salt"}},
	{surface: "principals", ownerBundle: 6, stamp: "identity_projection_read"},
	{surface: "principal_clients", ownerBundle: 6, stamp: "identity_projection_read",
		deniedColumns: []string{"principal_id"}},
	{surface: "client_sessions", ownerBundle: 6, stamp: "identity_projection_read"},
}

// pgReadScopeDoctorBlock reports the RFC 0110 / #164 read-scope posture. It is
// deliberately separate from pg_write_boundary: RFC 0110's shipped guarantees
// close mutation paths, not private reads by a live runtime credential.
//
// Since RFC 0114 the posture is DERIVED, not hard-coded: schema_authority
// stamps (runtime-readable by design, owner bundle 0002) name which gates the
// applied bundles claim, and live privilege/ownership probes verify each claim
// surface-by-surface. Posture rules:
//
//   - any stamped gate whose probe fails → broad_runtime_select, plus a
//     grant_drift array naming the failing surfaces (remedy: re-run
//     `striatum daemon owner-ddl apply`, which re-runs ReassertReadRevokes);
//   - else if every runtime_sensitive_select table is covered by a verified
//     table-level gate → private_read_denial (unreachable until RFC 0113
//     R2/R3; kept in the rule so the graduation is mechanical, not editorial).
//     runtime_column_scoped_select tables are excluded from that broad-SELECT
//     cohort only while their denied-column gates verify;
//   - else if at least one verified table-level gate exists →
//     partial_projection_gated;
//   - else (column gates only — the 0005-only state) → broad_runtime_select.
//
// Like the other doctor blocks it never fails closed: probe errors are
// captured in "errors" and count against verification, so doctor still
// returns and never over-claims a closed surface.
func pgReadScopeDoctorBlock(ctx context.Context, runner db.Runner) map[string]any {
	sensitiveSurfaces := db.RuntimeSensitiveReadTables()
	columnScopedSurfaces := db.RuntimeColumnScopedReadTables()
	stamps, blockErrors := pgReadScopeStamps(ctx, runner)

	gates := make([]map[string]any, 0, len(pgReadScopeGates))
	grantDrift := []string{}
	verifiedTableGates := map[string]bool{}
	verifiedColumnGates := map[string]bool{}
	for _, gate := range pgReadScopeGates {
		stamped := stamps[gate.stamp]
		tableGate := len(gate.deniedColumns) == 0
		entry := map[string]any{
			"surface":             gate.surface,
			"owner_bundle":        gate.ownerBundle,
			"authority_stamp":     gate.stamp,
			"stamped":             stamped,
			"verified":            false,
			"owner_ok":            false,
			"private_read_denial": false,
		}
		if tableGate {
			entry["gate"] = "table"
		} else {
			entry["gate"] = "columns"
			entry["denied_columns"] = gate.deniedColumns
		}
		if stamped && runner != nil {
			verified, ownerOK, probeErrors := pgReadScopeProbeGate(ctx, runner, gate)
			blockErrors = append(blockErrors, probeErrors...)
			entry["verified"] = verified
			entry["owner_ok"] = ownerOK
			if !verified || (tableGate && !ownerOK) {
				grantDrift = append(grantDrift, gate.surface)
			} else if tableGate {
				verifiedTableGates[gate.surface] = true
			} else {
				verifiedColumnGates[gate.surface] = true
			}
		}
		gates = append(gates, entry)
	}

	posture := pgReadScopeBroadRuntimeSelect
	switch {
	case len(grantDrift) > 0:
		posture = pgReadScopeBroadRuntimeSelect
	case pgReadScopeAllCovered(sensitiveSurfaces, verifiedTableGates) &&
		pgReadScopeColumnScopedCovered(columnScopedSurfaces, verifiedColumnGates):
		posture = pgReadScopePrivateReadDenial
	case len(verifiedTableGates) > 0:
		posture = pgReadScopePartialProjectionGated
	}
	selectScope := "broad"
	switch posture {
	case pgReadScopePartialProjectionGated:
		selectScope = "partial"
	case pgReadScopePrivateReadDenial:
		selectScope = "denied"
	}

	block := map[string]any{
		"posture":                     posture,
		"runtime_role_select_scope":   selectScope,
		"private_read_denial":         posture == pgReadScopePrivateReadDenial,
		"inventory_source":            "go/pkg/db/read_authority_inventory.go",
		"sensitive_surface_count":     len(sensitiveSurfaces),
		"column_scoped_surface_count": len(columnScopedSurfaces),
		"column_scoped_surfaces":      columnScopedSurfaces,
		"partial_projection_gates":    gates,
		"errors":                      blockErrors,
		"bounded_by": []string{
			"L0 runtime credential rotation makes captured DSN strings stale after daemon restart",
			"L2 lane isolation prevents sandboxed lanes from reaching PostgreSQL once adopted",
		},
		"representative_sensitive_surfaces": sensitiveSurfaces,
		"note": "RFC 0110 does not claim read confidentiality against a leaked live runtime credential; " +
			"RFC 0113 R1 (bundle 0005) makes clients a column-scoped read surface by gating token secrets, and bundle 0006 gates principal/session identity, but #164 remains open until every broad sensitive read surface is bounded.",
	}
	if len(grantDrift) > 0 {
		block["grant_drift"] = grantDrift
		block["grant_drift_remedy"] = "re-run `striatum daemon owner-ddl apply` (re-applies ReassertReadRevokes); for ownership drift re-apply the owner bundle preconditions (RFC 0114)"
	}
	return block
}

// pgReadScopeStamps reads the schema_authority capability stamps as the
// runtime role (the runtime_parity_select grant exists precisely for this kind
// of capability parity). A nil runner or an authority-less schema yields no
// stamps — the pre-bundle posture.
func pgReadScopeStamps(ctx context.Context, runner db.Runner) (map[string]bool, []string) {
	stamps := map[string]bool{}
	if runner == nil {
		return stamps, []string{}
	}
	errors := []string{}
	present, err := runner.QueryScalar(ctx,
		"SELECT (to_regclass('striatumd.schema_authority') IS NOT NULL)::text")
	if err != nil {
		return stamps, append(errors, "pg_read_scope.stamps_failed: "+err.Error())
	}
	if present != "true" {
		return stamps, errors
	}
	rows, err := collectRows(ctx, runner,
		"SELECT capability FROM striatumd.schema_authority WHERE requires_daemon_auth = true")
	if err != nil {
		return stamps, append(errors, "pg_read_scope.stamps_failed: "+err.Error())
	}
	for _, row := range rows {
		if capability, ok := row["capability"].(string); ok {
			stamps[capability] = true
		}
	}
	return stamps, errors
}

// pgReadScopeProbeGate verifies one stamped gate live: privilege probes for
// the denied surface, and the table-ownership probe. A probe error counts as
// unverified (never over-claim) and is reported.
func pgReadScopeProbeGate(ctx context.Context, runner db.Runner, gate pgReadScopeGate) (verified bool, ownerOK bool, errors []string) {
	verified = true
	if len(gate.deniedColumns) == 0 {
		granted, err := runner.QueryScalar(ctx,
			"SELECT has_table_privilege('striatumd_rw', 'striatumd.'||$1, 'SELECT')::text", gate.surface)
		if err != nil {
			errors = append(errors, fmt.Sprintf("pg_read_scope.probe_failed.%s: %v", gate.surface, err))
			verified = false
		} else if granted != "false" {
			verified = false
		}
	} else {
		for _, column := range gate.deniedColumns {
			granted, err := runner.QueryScalar(ctx,
				"SELECT has_column_privilege('striatumd_rw', 'striatumd.'||$1, $2, 'SELECT')::text", gate.surface, column)
			if err != nil {
				errors = append(errors, fmt.Sprintf("pg_read_scope.probe_failed.%s.%s: %v", gate.surface, column, err))
				verified = false
			} else if granted != "false" {
				verified = false
			}
		}
	}
	owner, err := runner.QueryScalar(ctx,
		"SELECT (tableowner <> 'striatumd_rw')::text FROM pg_tables WHERE schemaname = 'striatumd' AND tablename = $1", gate.surface)
	if err != nil {
		errors = append(errors, fmt.Sprintf("pg_read_scope.owner_probe_failed.%s: %v", gate.surface, err))
	}
	ownerOK = err == nil && owner == "true"
	return verified, ownerOK, errors
}

// pgReadScopeAllCovered reports whether every runtime_sensitive_select table
// is covered by a verified table-level gate — the mechanical
// private_read_denial graduation rule (unreachable until RFC 0113 R2/R3).
func pgReadScopeAllCovered(sensitive []string, verifiedTableGates map[string]bool) bool {
	if len(sensitive) == 0 {
		return false
	}
	for _, table := range sensitive {
		if !verifiedTableGates[table] {
			return false
		}
	}
	return true
}

func pgReadScopeColumnScopedCovered(columnScoped []string, verifiedColumnGates map[string]bool) bool {
	for _, table := range columnScoped {
		if !verifiedColumnGates[table] {
			return false
		}
	}
	return true
}
