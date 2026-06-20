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

// TestApplyOwnerBundlesSelfHealsMissingEarlierObject is FMA-007 / GH #458: when
// an earlier bundle's object went missing and a *later* (newly-pending) bundle
// depends on it, the normal apply path stops at the missing-object error
// (`relation does not exist`) and the daemon would fail closed at startup. The
// fix re-applies every bundle in ascending order once — bundle 0001 re-creates
// the missing object before bundle 0002's GRANT depends on it — and the apply
// then succeeds. This asserts the run self-heals (object back, no error).
func TestApplyOwnerBundlesSelfHealsMissingEarlierObject(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("initial apply owner bundles: %v", err)
	}

	// Simulate the FMA-007 skew: an early-bundle object (schema_authority,
	// created by bundle 0001 and GRANTed-on by bundle 0002) went missing, and
	// the stamps for that bundle and every later one were lost — so the normal
	// pending loop re-runs from bundle 0002, which depends on the missing object.
	// Bundle 0001's stamp REMAINS, so the pending loop will NOT re-run 0001
	// (the exact gap the self-heal must close).
	if err := pool.Runner.Exec(ctx, "DROP TABLE striatumd.schema_authority CASCADE"); err != nil {
		t.Fatalf("drop schema_authority: %v", err)
	}
	// schema_authority is gone, so its stamp rows went with it; re-create a
	// minimal owner_bundle_meta state recording ONLY bundle 0001 as applied.
	// (owner_bundle_meta itself survives the CASCADE — it is a distinct table.)
	if err := pool.Runner.Exec(ctx,
		"DELETE FROM striatumd.owner_bundle_meta WHERE version >= 2"); err != nil {
		t.Fatalf("trim owner_bundle_meta to version 1: %v", err)
	}

	current, err := db.OwnerBundleVersion(ctx, pool.Runner)
	if err != nil {
		t.Fatalf("read owner bundle version: %v", err)
	}
	if current != 1 {
		t.Fatalf("owner bundle version = %d; want 1 (only bundle 0001 stamped)", current)
	}

	// Sanity: schema_authority really is gone, so bundle 0002 would fail without
	// the ordered self-heal.
	present, err := pool.Runner.QueryScalar(ctx,
		"SELECT (to_regclass('striatumd.schema_authority') IS NOT NULL)::text")
	if err != nil {
		t.Fatalf("probe schema_authority: %v", err)
	}
	if present != "false" {
		t.Fatalf("schema_authority present = %q before self-heal; want false", present)
	}

	// The fix: ApplyOwnerBundles hits the missing-object error on bundle 0002,
	// runs the ordered idempotent reconciliation (re-applying bundle 0001 first),
	// and succeeds.
	if _, version, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply after missing earlier object did not self-heal: %v", err)
	} else if version != db.LatestOwnerBundleVersion {
		t.Fatalf("post-self-heal version = %d; want %d", version, db.LatestOwnerBundleVersion)
	}

	// The missing earlier-bundle object is restored.
	present, err = pool.Runner.QueryScalar(ctx,
		"SELECT (to_regclass('striatumd.schema_authority') IS NOT NULL)::text")
	if err != nil {
		t.Fatalf("probe schema_authority after self-heal: %v", err)
	}
	if present != "true" {
		t.Fatalf("schema_authority present = %q after self-heal; want true", present)
	}
}

// TestReapplyAllOwnerBundlesIsOrderedAndIdempotent is the FMA-007 / GH #458
// ordered reconciliation primitive: re-applying every shipped bundle (regardless
// of stamp) is a safe no-op on an already-current database and re-creates a
// missing earlier object. Each bundle is idempotent DDL in its own transaction.
func TestReapplyAllOwnerBundlesIsOrderedAndIdempotent(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("initial apply owner bundles: %v", err)
	}

	// Re-applying everything against a current database is a no-op (idempotent)
	// and returns the full ascending version list.
	ran, err := db.ReapplyAllOwnerBundles(ctx, pool.Runner, nil, "test")
	if err != nil {
		t.Fatalf("reapply on current db: %v", err)
	}
	shipped, err := db.OwnerBundles()
	if err != nil {
		t.Fatalf("enumerate owner bundles: %v", err)
	}
	if len(ran) != len(shipped) {
		t.Fatalf("reapply ran %d bundles; want %d (every shipped bundle)", len(ran), len(shipped))
	}
	for i := 1; i < len(ran); i++ {
		if ran[i] <= ran[i-1] {
			t.Fatalf("reapply order not ascending: %v", ran)
		}
	}
	if version, err := db.OwnerBundleVersion(ctx, pool.Runner); err != nil || version != db.LatestOwnerBundleVersion {
		t.Fatalf("post-reapply version = %d err=%v; want %d", version, err, db.LatestOwnerBundleVersion)
	}
}
