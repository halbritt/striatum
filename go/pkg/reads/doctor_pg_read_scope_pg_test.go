package reads

import (
	"context"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

func readScopeGateBySurface(t *testing.T, block map[string]any, surface string) map[string]any {
	t.Helper()
	gates, ok := block["partial_projection_gates"].([]map[string]any)
	if !ok {
		t.Fatalf("partial_projection_gates = %#v", block["partial_projection_gates"])
	}
	for _, gate := range gates {
		if gate["surface"] == surface {
			return gate
		}
	}
	t.Fatalf("no %s gate in %#v", surface, gates)
	return nil
}

// TestPgReadScopePostureDerivation pins the RFC 0114 doctor derivation against
// a live database: the posture is broad_runtime_select before any bundle,
// flips to partial_projection_gated when identity_projection_read is stamped
// and the privilege/ownership probes verify, reports grant_drift (and
// downgrades) when a closed surface is re-granted, recovers after
// ReassertReadRevokes, and stays broad with only the 0005 column gate.
func TestPgReadScopePostureDerivation(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	// Bundle absent: no authority schema, no stamps, no claims.
	block := pgReadScopeDoctorBlock(ctx, pool.Runner)
	if block["posture"] != pgReadScopeBroadRuntimeSelect {
		t.Fatalf("pre-bundle posture = %v, want %s", block["posture"], pgReadScopeBroadRuntimeSelect)
	}
	if gate := readScopeGateBySurface(t, block, "principals"); gate["stamped"] != false {
		t.Fatalf("pre-bundle principals gate claims a stamp: %#v", gate)
	}

	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	// Bundle applied and verified: partial_projection_gated, every gate
	// stamped+verified, table gates owner_ok, still no private-read claim.
	block = pgReadScopeDoctorBlock(ctx, pool.Runner)
	if block["posture"] != pgReadScopePartialProjectionGated {
		t.Fatalf("post-bundle posture = %v (errors=%v), want %s",
			block["posture"], block["errors"], pgReadScopePartialProjectionGated)
	}
	if block["private_read_denial"] != false {
		t.Fatalf("private_read_denial = %v, want false (RFC 0113 R2/R3 are out of scope)", block["private_read_denial"])
	}
	columnScoped, ok := block["column_scoped_surfaces"].([]string)
	if !ok || !containsStringItem(columnScoped, "clients") {
		t.Fatalf("column_scoped_surfaces = %#v, want clients", block["column_scoped_surfaces"])
	}
	sensitive, ok := block["representative_sensitive_surfaces"].([]string)
	if !ok || containsStringItem(sensitive, "clients") {
		t.Fatalf("representative_sensitive_surfaces = %#v, want clients excluded after column-scoped classification",
			block["representative_sensitive_surfaces"])
	}
	for _, surface := range []string{"clients", "principals", "principal_clients", "client_sessions"} {
		gate := readScopeGateBySurface(t, block, surface)
		if gate["stamped"] != true || gate["verified"] != true {
			t.Fatalf("post-bundle %s gate not stamped+verified: %#v", surface, gate)
		}
	}
	for _, surface := range []string{"principals", "client_sessions"} {
		if gate := readScopeGateBySurface(t, block, surface); gate["owner_ok"] != true {
			t.Fatalf("post-bundle %s table gate owner_ok = %v, want true", surface, gate["owner_ok"])
		}
	}
	if _, drifted := block["grant_drift"]; drifted {
		t.Fatalf("post-bundle grant_drift reported: %#v", block["grant_drift"])
	}

	// Grant drift: a re-granted closed surface downgrades the posture and is
	// named; ReassertReadRevokes (the owner-ddl apply re-run) repairs it.
	if err := pool.Runner.Exec(ctx, "GRANT SELECT ON striatumd.principals TO striatumd_rw"); err != nil {
		t.Fatalf("simulate grant drift: %v", err)
	}
	block = pgReadScopeDoctorBlock(ctx, pool.Runner)
	if block["posture"] != pgReadScopeBroadRuntimeSelect {
		t.Fatalf("drifted posture = %v, want %s", block["posture"], pgReadScopeBroadRuntimeSelect)
	}
	drift, ok := block["grant_drift"].([]string)
	if !ok || len(drift) != 1 || drift[0] != "principals" {
		t.Fatalf("grant_drift = %#v, want [principals]", block["grant_drift"])
	}
	if gate := readScopeGateBySurface(t, block, "principals"); gate["verified"] != false {
		t.Fatalf("drifted principals gate still verified: %#v", gate)
	}

	if err := db.ReassertReadRevokes(ctx, pool.Runner); err != nil {
		t.Fatalf("reassert read revokes: %v", err)
	}
	block = pgReadScopeDoctorBlock(ctx, pool.Runner)
	if block["posture"] != pgReadScopePartialProjectionGated {
		t.Fatalf("repaired posture = %v (errors=%v), want %s",
			block["posture"], block["errors"], pgReadScopePartialProjectionGated)
	}

	// 0005-only state: with the identity stamp gone, the only verified gate is
	// the clients column gate, which does not claim a table-level closure —
	// the posture stays broad_runtime_select exactly as before bundle 0006.
	if err := pool.Runner.Exec(ctx,
		"DELETE FROM striatumd.schema_authority WHERE capability = 'identity_projection_read'"); err != nil {
		t.Fatalf("remove identity stamp: %v", err)
	}
	block = pgReadScopeDoctorBlock(ctx, pool.Runner)
	if block["posture"] != pgReadScopeBroadRuntimeSelect {
		t.Fatalf("0005-only posture = %v, want %s", block["posture"], pgReadScopeBroadRuntimeSelect)
	}
	if _, drifted := block["grant_drift"]; drifted {
		t.Fatalf("0005-only grant_drift reported: %#v", block["grant_drift"])
	}
	clientsGate := readScopeGateBySurface(t, block, "clients")
	if clientsGate["stamped"] != true || clientsGate["verified"] != true {
		t.Fatalf("0005-only clients gate not stamped+verified: %#v", clientsGate)
	}
}
