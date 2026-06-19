package db_test

import (
	"context"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// TestOwnerBundleAppliesAndIsIdempotent applies the production owner bundle SQL
// against a migrated database and asserts: version goes 0 -> 1, the objects
// exist, re-apply is a no-op, and the capability stamp the parity checker reads
// is present (RFC 0110 §8.1).
func TestOwnerBundleAppliesAndIsIdempotent(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	if v, err := db.OwnerBundleVersion(ctx, pool.Runner); err != nil || v != 0 {
		t.Fatalf("pre-apply version = %d, err = %v; want 0", v, err)
	}

	applied, version, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test")
	if err != nil {
		t.Fatalf("apply owner bundles: %v", err)
	}
	// A fresh database applies every shipped bundle. The version reaches
	// LatestOwnerBundleVersion, but the count is the number of files actually
	// shipped — the version sequence may carry a reserved gap (e.g. 0011 reserved
	// for a concurrent change), so len(applied) tracks OwnerBundles(), not the
	// latest version number.
	shipped, err := db.OwnerBundles()
	if err != nil {
		t.Fatalf("enumerate owner bundles: %v", err)
	}
	if version != db.LatestOwnerBundleVersion || len(applied) != len(shipped) {
		t.Fatalf("apply result applied=%v version=%d; want %d bundles applied to version %d", applied, version, len(shipped), db.LatestOwnerBundleVersion)
	}

	// Re-apply is idempotent: nothing applied, version unchanged.
	applied2, version2, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test")
	if err != nil {
		t.Fatalf("re-apply owner bundles: %v", err)
	}
	if len(applied2) != 0 || version2 != db.LatestOwnerBundleVersion {
		t.Fatalf("re-apply applied=%v version=%d; want [], %d", applied2, version2, db.LatestOwnerBundleVersion)
	}

	// Objects exist.
	checks := map[string]string{
		"daemon_auth_registry table": "SELECT (to_regclass('striatumd.daemon_auth_registry') IS NOT NULL)::text",
		"daemon_auth_log table":      "SELECT (to_regclass('striatumd.daemon_auth_log') IS NOT NULL)::text",
		"schema_authority table":     "SELECT (to_regclass('striatumd.schema_authority') IS NOT NULL)::text",
		"assert_daemon_authority fn": "SELECT (to_regprocedure('striatumd.assert_daemon_authority()') IS NOT NULL)::text",
		"audit_v3_row_hash fn":       "SELECT (to_regproc('striatumd.audit_v3_row_hash') IS NOT NULL)::text",
		"append_audit_row fn":        "SELECT (to_regproc('striatumd.append_audit_row') IS NOT NULL)::text",
		"events lock_wait_us":        "SELECT (to_regclass('striatumd.events') IS NOT NULL AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='striatumd' AND table_name='events' AND column_name='lock_wait_us'))::text",
		"audit_log lock_wait_us":     "SELECT (to_regclass('striatumd.audit_log') IS NOT NULL AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='striatumd' AND table_name='audit_log' AND column_name='lock_wait_us'))::text",
	}
	for name, sql := range checks {
		got, err := pool.Runner.QueryScalar(ctx, sql)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != "true" {
			t.Errorf("%s missing after apply", name)
		}
	}

	stamp, err := pool.Runner.QueryScalar(ctx,
		"SELECT requires_daemon_auth::text FROM striatumd.schema_authority WHERE capability = 'audit_sd_append'")
	if err != nil {
		t.Fatalf("read capability stamp: %v", err)
	}
	if stamp != "true" {
		t.Fatalf("audit_sd_append stamp requires_daemon_auth = %q; want true", stamp)
	}
}

// TestOwnerBundleEighteenTransfersCohortOwnershipToRuntime is GH #442 / #441,
// the closest pgtest can get to the prod fix: after the owner bundles apply, the
// pre-split runtime cohort is owned by striatumd_rw, so the runtime role can ALTER
// those tables (the property migration 0035/0036 need). pgtest is SINGLE-ROLE so
// it cannot reproduce the bootstrap-owned starting condition, but it DOES exercise
// the real bundle 0018 SQL: the GRANT CREATE prerequisite + the guarded
// ALTER … OWNER TO striatumd_rw transfer, and proves a striatumd_rw-membership
// session can subsequently run an ADD COLUMN against a transferred table.
func TestOwnerBundleEighteenTransfersCohortOwnershipToRuntime(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundles: %v", err)
	}

	// Every cohort table the bundle transfers must now be striatumd_rw-owned.
	for _, table := range []string{
		"job_recovery_state",
		"barrier_staged_contributions",
		"barrier_state",
		"fanin_freeze_points",
		"conversations",
		"conversation_post_dialog_hooks",
		"dissent_ledger",
		"interrogations",
		"job_workspaces",
		"spawn_authorization_grants",
	} {
		owner, err := pool.Runner.QueryScalar(ctx,
			"SELECT tableowner FROM pg_tables WHERE schemaname='striatumd' AND tablename=$1", table)
		if err != nil {
			t.Fatalf("read owner of %s: %v", table, err)
		}
		if owner != "striatumd_rw" {
			t.Fatalf("after bundle 0018, striatumd.%s owner = %q; want striatumd_rw", table, owner)
		}
	}

	// The runtime role can now ALTER a transferred table (the exact shape of
	// migration 0035's ADD COLUMN). SET ROLE / ALTER / RESET must run on the SAME
	// physical connection, so acquire one rather than using the pool directly.
	conn, err := pool.RawPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET ROLE striatumd_rw"); err != nil {
		t.Fatalf("set role striatumd_rw: %v", err)
	}
	_, alterErr := conn.Exec(ctx,
		"ALTER TABLE striatumd.job_recovery_state ADD COLUMN IF NOT EXISTS pgtest_442_probe int")
	if _, err := conn.Exec(ctx, "RESET ROLE"); err != nil {
		t.Fatalf("reset role: %v", err)
	}
	if alterErr != nil {
		t.Fatalf("striatumd_rw could not ALTER the transferred job_recovery_state (the #442 fix property): %v", alterErr)
	}
	if _, err := conn.Exec(ctx,
		"ALTER TABLE striatumd.job_recovery_state DROP COLUMN IF EXISTS pgtest_442_probe"); err != nil {
		t.Fatalf("clean up probe column: %v", err)
	}
}
