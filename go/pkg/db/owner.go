package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// LatestOwnerBundleVersion is the highest owner-DDL bundle the binary ships.
// Owner bundles (RFC 0110 §8.1) carry owner-only DDL — the authority registry,
// SECURITY DEFINER write functions, capability stamps, and the phased DML
// revokes — that the runtime role cannot perform. They are applied OUT-OF-BAND
// as the database owner via `striatum daemon owner-ddl apply`, never through the
// runtime-role ApplyMigrations path (RFC 0079 §5).
const LatestOwnerBundleVersion = 18

//go:embed sql/owner/*.sql
var ownerBundleFS embed.FS

var ownerBundleLabels = map[int]string{
	1:  "authority schema + v3 hash + phase 0 audit_only (RFC 0110 N+1)",
	2:  "runtime read grant on schema_authority for capability parity (RFC 0110 N+1)",
	3:  "phase 1 audit_artifacts: append_artifact_row SD fn + artifacts INSERT revoke (RFC 0110 §7)",
	4:  "phase 2 full: append_event_row SD fn (in-DB v3 hash + transcript exclusion) + events INSERT revoke (RFC 0110 §7)",
	5:  "runtime read scope R1: token-secret projections + clients secret-column SELECT revoke (RFC 0113)",
	6:  "runtime read scope R1 step 2: principal/session identity projections + SELECT revokes + ownership transfer (RFC 0114)",
	7:  "artifact placement column + append_artifact_row placement overload (RFC 0123)",
	8:  "artifact metadata full-text search column and GIN index (RFC 0119 / D179)",
	9:  "build-owned review_generation columns on jobs + verdicts (RFC 0126 P0 / D194 / GH #282)",
	10: "wedged_no_tool_progress liveness stall class on sessions CHECK (GH #324)",
	11: "hot event-read covering index on events (GH #330)",
	12: "quarantined job state on jobs CHECK (per-job quarantine + finalize-the-majority, GH #311)",
	13: "barrier_assembly job_type on jobs CHECK (recoverable fan-in assembly job, RFC 0135 P2 / GH #346)",
	14: "event/audit chain-head lock wait gauges for doctor convoy warnings (GH #372 / GH #379)",
	15: "FK covering indexes on events/audit_log (GH #386)",
	16: "verify job_type on jobs CHECK (executable verification gate / sandboxed verifier lane, RFC 0134 D227 / GH #395)",
	17: "synthetic pipe-read liveness column on sessions for pipe-transport lanes (RFC 0131 131-future / GH #350)",
	18: "transfer pre-split runtime-table cohort (job_recovery_state etc.) ownership to striatumd_rw so runtime migrations may ALTER them (GH #442 / #441)",
}

// OwnerBundle is one versioned owner-DDL bundle file.
type OwnerBundle struct {
	Version int
	Label   string
	Path    string
	SQL     string
}

// SHA256 is the content hash recorded in owner_bundle_meta on apply.
func (b OwnerBundle) SHA256() string {
	sum := sha256.Sum256([]byte(b.SQL))
	return hex.EncodeToString(sum[:])
}

// OwnerBundles returns the embedded owner bundles in ascending version order.
func OwnerBundles() ([]OwnerBundle, error) {
	entries, err := ownerBundleFS.ReadDir("sql/owner")
	if err != nil {
		return nil, err
	}
	var bundles []OwnerBundle
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("owner bundle %s has no leading version: %w", entry.Name(), err)
		}
		body, err := ownerBundleFS.ReadFile("sql/owner/" + entry.Name())
		if err != nil {
			return nil, err
		}
		bundles = append(bundles, OwnerBundle{
			Version: version,
			Label:   ownerBundleLabels[version],
			Path:    "sql/owner/" + entry.Name(),
			SQL:     string(body),
		})
	}
	sort.Slice(bundles, func(i, j int) bool { return bundles[i].Version < bundles[j].Version })
	return bundles, nil
}

// OwnerBundleVersion returns the highest owner bundle version applied to the
// database, or 0 when no bundle (and hence no owner_bundle_meta table) exists.
func OwnerBundleVersion(ctx context.Context, runner Runner) (int, error) {
	present, err := runner.QueryScalar(ctx,
		"SELECT (to_regclass('striatumd.owner_bundle_meta') IS NOT NULL)::text")
	if err != nil {
		return 0, err
	}
	if present != "true" {
		return 0, nil
	}
	value, err := runner.QueryScalar(ctx,
		"SELECT COALESCE(MAX(version), 0)::text FROM striatumd.owner_bundle_meta")
	if err != nil {
		return 0, err
	}
	version, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	return version, nil
}

// ApplyOwnerBundles applies every owner bundle newer than the recorded version,
// as the owner role behind runner. Each version applies in a single transaction
// that stamps owner_bundle_meta last, so a partially-applied bundle cannot
// persist (atomic per version); re-applying a stamped version is a no-op. It
// returns the versions applied this call and the resulting version.
//
// FMA-007 / GH #458 self-heal: the normal apply path skips any bundle whose
// version is already stamped, so if an *earlier* bundle's objects went missing
// (a credential/ownership skew or a hand-edited database) the later bundle that
// depends on them fails on a missing cross-bundle object. When that happens this
// re-applies *every* shipped bundle in ascending order once — each bundle is
// idempotent DDL in its own transaction, so a missing earlier object is
// re-created before the later bundle that needs it — and retries. Only if the
// ordered reconciliation still cannot satisfy the dependency does it return a
// legible, actionable error; the daemon stays fail-closed as the final safety
// property. The reconciliation never creates objects outside the bundle DDL.
func ApplyOwnerBundles(ctx context.Context, runner Runner, daemonVersion string) ([]int, int, error) {
	if daemonVersion == "" {
		daemonVersion = "dev"
	}
	bundles, err := OwnerBundles()
	if err != nil {
		return nil, 0, err
	}
	current, err := OwnerBundleVersion(ctx, runner)
	if err != nil {
		return nil, 0, err
	}
	applied, current, err := applyPendingOwnerBundles(ctx, runner, bundles, current, daemonVersion)
	if err == nil {
		return applied, current, nil
	}
	// A non-dependency failure (or a reconciliation already attempted) is
	// returned as-is, fail-closed.
	if !isCrossBundleDependencyError(err) {
		return applied, current, err
	}
	// Ordered idempotent reconciliation: re-apply every shipped bundle so a
	// missing earlier object is re-created before the later bundle depends on
	// it. Each bundle is idempotent, so already-present objects are no-ops.
	reapplied, reErr := ReapplyAllOwnerBundles(ctx, runner, bundles, daemonVersion)
	if reErr != nil {
		// Reconciliation could not heal the gap (e.g. the missing object is not
		// produced by any earlier bundle, or the owner lacks the privilege to
		// re-create it). Surface the actionable message; fail-closed.
		return applied, current, reErr
	}
	// Reconciliation re-ran the full bundle set; recompute the resulting state
	// and report every version it touched.
	final, verErr := OwnerBundleVersion(ctx, runner)
	if verErr != nil {
		return reapplied, current, verErr
	}
	return reapplied, final, nil
}

// applyPendingOwnerBundles applies, in ascending order, each bundle newer than
// current. A failure on a missing cross-bundle object is wrapped legibly so the
// caller (and the operator) sees which bundle failed, which object is missing,
// and the one-step remediation, instead of a raw `relation does not exist`.
func applyPendingOwnerBundles(ctx context.Context, runner Runner, bundles []OwnerBundle, current int, daemonVersion string) ([]int, int, error) {
	var applied []int
	for _, bundle := range bundles {
		if bundle.Version <= current {
			continue
		}
		if err := applyOneOwnerBundle(ctx, runner, bundle, daemonVersion); err != nil {
			return applied, current, wrapOwnerBundleApplyError(bundle, err)
		}
		applied = append(applied, bundle.Version)
		current = bundle.Version
	}
	return applied, current, nil
}

// ReapplyAllOwnerBundles re-applies every shipped owner bundle in ascending
// numeric order, regardless of the recorded version. Each bundle is idempotent
// DDL applied in its own transaction (so an already-present object is a no-op
// and the stamp INSERT is ON CONFLICT DO NOTHING), which makes this the
// ordered self-heal for a database whose earlier-bundle objects went missing
// (FMA-007 / GH #458): a missing object is re-created by its owning bundle
// before any later bundle depends on it. It returns the versions it (re)ran.
// A failure is wrapped with the same legible remediation as the normal path.
func ReapplyAllOwnerBundles(ctx context.Context, runner Runner, bundles []OwnerBundle, daemonVersion string) ([]int, error) {
	if daemonVersion == "" {
		daemonVersion = "dev"
	}
	if bundles == nil {
		loaded, err := OwnerBundles()
		if err != nil {
			return nil, err
		}
		bundles = loaded
	}
	var ran []int
	for _, bundle := range bundles {
		if err := applyOneOwnerBundle(ctx, runner, bundle, daemonVersion); err != nil {
			return ran, wrapOwnerBundleApplyError(bundle, err)
		}
		ran = append(ran, bundle.Version)
	}
	return ran, nil
}

// crossBundleDependencySQLStates are the PostgreSQL undefined-object codes a
// bundle raises when an object an earlier bundle should have created is absent:
// undefined_table (relation does not exist), undefined_column, undefined_function,
// and the generic undefined_object. These are the FMA-007 cross-bundle failures.
var crossBundleDependencySQLStates = map[string]struct{}{
	"42P01": {}, // undefined_table
	"42703": {}, // undefined_column
	"42883": {}, // undefined_function
	"42704": {}, // undefined_object
}

// isCrossBundleDependencyError reports whether err is a missing-object failure
// of the kind a re-applied later bundle raises when an earlier bundle's object
// is gone (the FMA-007 self-heal trigger).
func isCrossBundleDependencyError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	_, ok := crossBundleDependencySQLStates[pgErr.Code]
	return ok
}

// wrapOwnerBundleApplyError annotates a bundle-apply failure. For a missing
// cross-bundle object it produces an actionable message naming the failing
// bundle, the missing object, and the one-step remediation; other errors keep
// the existing wrapping so unrelated failures stay legible too.
func wrapOwnerBundleApplyError(bundle OwnerBundle, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if _, ok := crossBundleDependencySQLStates[pgErr.Code]; ok {
			missing := strings.TrimSpace(pgErr.Message)
			if missing == "" {
				missing = "an object an earlier owner bundle should have created"
			}
			return fmt.Errorf(
				"apply owner bundle %d (%s): missing cross-bundle dependency (%s: %s); "+
					"re-run `striatum daemon owner-ddl apply` to re-create the earlier bundle's objects in order, "+
					"and if it still fails restore the missing object as the database owner: %w",
				bundle.Version, bundle.Label, pgErr.Code, missing, err)
		}
	}
	return fmt.Errorf("apply owner bundle %d (%s): %w", bundle.Version, bundle.Label, err)
}

// capabilityProtectedTable maps each SD-append capability stamp to the table
// whose direct runtime-role INSERT that phase revokes (RFC 0110 §7). The
// protected set is derived from the stamps rather than a static list so a
// deployment revokes exactly the surfaces its applied owner bundles have closed
// — never a surface whose bundle is unapplied (which would break that surface's
// writes for a daemon that has not adopted the phase).
var capabilityProtectedTable = map[string]string{
	"audit_sd_append":    "audit_log",
	"artifact_sd_append": "artifacts",
	"event_sd_append":    "events",
}

// ReassertWriteRevokes re-applies the protected-surface INSERT revokes for every
// phase whose capability the owner bundle has stamped (RFC 0110 §6,
// C-GRANT-DRIFT). Any privilege-granting step (migration helper, doctor
// repair-grants) must call this afterwards so a stray GRANT cannot quietly
// reopen a closed surface. Run as the owner; a no-op when no authority schema is
// present.
func ReassertWriteRevokes(ctx context.Context, runner Runner) error {
	stamped, present, err := readStampedCapabilities(ctx, runner)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	for _, capability := range stamped {
		table, ok := capabilityProtectedTable[capability]
		if !ok {
			continue
		}
		if err := runner.Exec(ctx, fmt.Sprintf(
			"REVOKE INSERT ON striatumd.%s FROM striatumd_rw", table)); err != nil {
			return fmt.Errorf("reassert revoke on %s: %w", table, err)
		}
	}
	return nil
}

// readScopeReasserts maps each read-projection capability stamp to the revoke
// + grant-back statements its owner bundle uses to close the surface (the read
// analog of capabilityProtectedTable). Stamps drive re-assertion, so a
// deployment re-closes exactly the surfaces its applied bundles closed — never
// a surface whose bundle is unapplied. The statement lists restate the bundle
// SQL verbatim; the bundle and this map together fully determine the
// post-close ACL.
var readScopeReasserts = map[string][]string{
	// Bundle 0005 (RFC 0113 R1): only the token secret columns on clients are
	// closed; non-secret client metadata stays directly readable.
	"auth_projection_read": {
		"REVOKE SELECT ON striatumd.clients FROM striatumd_rw",
		`GRANT SELECT (
		  client_id, client_kind, display_name, token_id,
		  created_at, expires_at, revoked_at, last_used_at
		) ON striatumd.clients TO striatumd_rw`,
	},
	// Bundle 0006 (RFC 0114): principals and client_sessions are fully read
	// denied; principal_clients keeps the column gate (principal_id denied,
	// client_id/linked_at/unlinked_at preserved for the live UPDATE ... WHERE
	// in admin/tokens.go). DML grant-backs are restated so a drift repair
	// cannot strand the write surface.
	"identity_projection_read": {
		"REVOKE SELECT ON striatumd.client_sessions FROM striatumd_rw",
		"GRANT INSERT, UPDATE, DELETE ON striatumd.client_sessions TO striatumd_rw",
		"REVOKE SELECT ON striatumd.principals FROM striatumd_rw",
		"GRANT INSERT, UPDATE, DELETE ON striatumd.principals TO striatumd_rw",
		"REVOKE SELECT ON striatumd.principal_clients FROM striatumd_rw",
		`GRANT SELECT (client_id, linked_at, unlinked_at)
		  ON striatumd.principal_clients TO striatumd_rw`,
		"GRANT INSERT, UPDATE, DELETE ON striatumd.principal_clients TO striatumd_rw",
	},
}

// ReassertReadRevokes re-applies read-scope revokes for authority-protected
// read projections stamped by owner bundles. Like ReassertWriteRevokes it is
// map-driven from the stamps, so it re-closes exactly the read surfaces the
// applied bundles closed. Run as the owner; a no-op when no authority schema
// is present. `striatum daemon owner-ddl apply` calls it after
// ApplyOwnerBundles, making a re-run the documented grant-drift repair.
func ReassertReadRevokes(ctx context.Context, runner Runner) error {
	stamped, present, err := readStampedCapabilities(ctx, runner)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	for _, capability := range stamped {
		stmts, ok := readScopeReasserts[capability]
		if !ok {
			continue
		}
		for _, stmt := range stmts {
			if err := runner.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("reassert read revokes for %s: %w", capability, err)
			}
		}
	}
	return nil
}

func applyOneOwnerBundle(ctx context.Context, runner Runner, bundle OwnerBundle, daemonVersion string) error {
	tx, err := runner.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()
	if err := tx.Exec(ctx, bundle.SQL); err != nil {
		return err
	}
	// The bundle creates owner_bundle_meta (IF NOT EXISTS); the stamp is the
	// last write in the same transaction, so the marker and the objects commit
	// together or not at all.
	if err := tx.Exec(ctx,
		`INSERT INTO striatumd.owner_bundle_meta(version, label, sha256, daemon_version)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (version) DO NOTHING`,
		bundle.Version, bundle.Label, bundle.SHA256(), daemonVersion,
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}
