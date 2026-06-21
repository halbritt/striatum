package db_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// twoRoleRuntimeFloor mirrors the unexported futureRuntimeMigrationOwnerDDLFloor
// in migrations_test.go and twoRoleRuntimeFloor in go/pkg/pgtest/two_role.go (the
// RFC 0142 P0 fixture): migrations strictly below it were applied as the owner
// at bootstrap, and only versions >= it must stay applyable by the runtime role
// striatumd_rw (RFC 0110 / D215). The two-role apply test stages the floor +
// owner bundles as the owner and then applies everything at-or-above the floor as
// the runtime role, which is exactly the production daemon-restart migrate path.
//
// Collapsing this magic floor to a single home is RFC 0142 P0 review finding F-1
// (low, deferred): migrations_test.go is white-box `package db` and cannot import
// pgtest without an import cycle, so the three sites cross-reference each other to
// keep the floor from drifting silently.
const twoRoleRuntimeFloor = 27

// TestFullMigrationSetAppliesAsNonOwnerRoleOnTwoRoleDB is the dynamic,
// defense-in-depth complement to the static #508 FK-owner build guards
// (TestFutureRuntimeMigrationsDoNotFKOwnerHeldTables et al.): it applies the FULL
// migration set against a real PostgreSQL database under the two-role ownership
// model (RFC 0110 / D215 / #442), applying the above-floor runtime migrations as
// the NON-owner runtime role striatumd_rw — the exact privilege posture the live
// daemon uses on every restart.
//
// D236 / #442 / #508 single-role blind spot: the default pgtest path (and the
// stock CI postgres job) apply every migration as ONE privileged role, so a
// runtime migration that needs an owner privilege it does not have — an ALTER of
// a bootstrap-owned table, or a FOREIGN KEY into an owner-held table (the
// migration 0041 / D242 incident that crash-looped prod with `permission denied
// for table repositories`, SQLSTATE 42501) — passes single-role CI and only fails
// on a live two-role deploy. This test reproduces the two-role posture
// dynamically so that a privilege regression which slips past the static guard
// (e.g. through a DDL shape the regex scanner does not recognize) surfaces in CI
// as a failing migrate, not as a deploy-time crash-loop.
//
// The staging mirrors the production runbook (docs/how-to/postgres-transition.md):
//  1. the OWNER applies the floor migrations (1..floor-1) at bootstrap;
//  2. the OWNER applies the owner bundles (the SECURITY DEFINER functions, the
//     protected-surface revokes, and — load-bearing here — bundle 0018/0019's
//     ALTER … OWNER TO striatumd_rw cohort transfers plus GRANT CREATE ON SCHEMA
//     striatumd, which the runtime role needs to own the tables it creates and to
//     ALTER the transferred ones);
//  3. the RUNTIME role striatumd_rw applies the above-floor migrations (>= floor)
//     — and must succeed with no 42501.
func TestFullMigrationSetAppliesAsNonOwnerRoleOnTwoRoleDB(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv(pgtest.EnvPGTestURL))
	if baseURL == "" {
		t.Skip(pgtest.EnvPGTestURL + " not set; skipping live two-role migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// A throwaway database, created and (for the floor + owner bundles) driven as
	// the connecting OWNER role. Objects created by the floor migrations are thus
	// owner-owned — the bootstrap-owned starting condition the single-role pgtest
	// path cannot reproduce.
	ownerURL := createTwoRoleTestDatabase(t, ctx, baseURL)
	ensureRuntimeRoleForTwoRoleTest(t, ctx, baseURL)

	ownerPool, err := pgxpool.New(ctx, ownerURL)
	if err != nil {
		t.Fatalf("connect owner pool: %v", err)
	}
	defer ownerPool.Close()
	ownerRunner := db.PgxRunner{Pool: ownerPool}

	// Step 1: the OWNER applies the bootstrap floor migrations only (1..floor-1),
	// recording the version stamps exactly like the production applyOne path, so
	// the above-floor migrations are still pending when the runtime role takes
	// over. ApplyMigrations always runs the whole embedded set, so the floor-only
	// apply is driven directly here rather than through it (no product change).
	floorVersion := applyFloorMigrationsAsOwner(t, ctx, ownerRunner)

	// Step 2: the OWNER applies the owner bundles (transfers the runtime cohort,
	// grants striatumd_rw CREATE on the schema, installs the SD functions and the
	// protected-surface revokes). This is `striatum daemon owner-ddl apply` in the
	// runbook, run with the owner DSN BEFORE the runtime role applies the rest.
	if _, _, err := db.ApplyOwnerBundles(ctx, ownerRunner, "two-role-test"); err != nil {
		t.Fatalf("owner: apply owner bundles: %v", err)
	}

	// Step 2b: provision the runtime role's schema-level grants exactly as the
	// production deploy does. Schema USAGE and sequence USAGE/SELECT are NOT carried
	// by any migration or owner bundle SQL — `daemon doctor --provision-rw-role`
	// (and the bootstrap runbook) add them out-of-band, which is why the live
	// striatum_daemon DB shows striatumd_rw with schema USAGE even though no
	// embedded SQL grants it. pgtest's roleSetupStatements mirror this; the
	// two-role test must too, or the runtime role's above-floor CREATE TABLEs fail
	// "permission denied for schema striatumd" on a setup gap rather than a real
	// regression. (Owner bundle 0018 already grants CREATE ON SCHEMA striatumd.)
	for _, stmt := range []string{
		"GRANT USAGE ON SCHEMA striatumd TO striatumd_rw",
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA striatumd TO striatumd_rw",
	} {
		if err := ownerRunner.Exec(ctx, stmt); err != nil {
			t.Fatalf("owner: provision striatumd_rw schema grant (%q): %v", stmt, err)
		}
	}

	// Step 3: the RUNTIME role striatumd_rw applies every remaining (above-floor)
	// migration. This is the exact privilege posture of the live daemon restart
	// migrate (ConnectAndMigrate runs as striatumd_rw, RFC 0110 §9.1). A foreign
	// key into an owner-held table or an ALTER of a bootstrap-owned table here
	// fails with 42501 — the failure mode this whole test exists to catch.
	runtimePool := newRuntimeRolePool(t, ctx, ownerURL)
	defer runtimePool.Close()
	runtimeConn, err := runtimePool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire runtime-role connection: %v", err)
	}
	defer runtimeConn.Release()
	runtimeRunner := singleConnRunner{conn: runtimeConn}

	finalVersion, err := db.ApplyMigrations(ctx, runtimeRunner, "two-role-test")
	if err != nil {
		t.Fatalf("runtime role striatumd_rw could not apply the above-floor migration set on a two-role DB "+
			"(this is the #508 / #442 / D236 class — a runtime migration needs an owner privilege it lacks, "+
			"e.g. a FOREIGN KEY into an owner-held table → 42501): %v", err)
	}
	if finalVersion != db.LatestDaemonDBVersion {
		t.Fatalf("after two-role apply, schema version = %d, want %d", finalVersion, db.LatestDaemonDBVersion)
	}
	if floorVersion >= finalVersion {
		t.Fatalf("test precondition: floor version %d must be below the final version %d so the runtime role actually applied above-floor DDL", floorVersion, finalVersion)
	}

	// The runtime role really applied the above-floor migrations: a sample of the
	// above-floor runtime tables must exist (and be visible to striatumd_rw).
	for _, table := range []string{
		"event_chain_segments",  // migration 0041 (the D242 incident table)
		"verifier_attestations", // migration 0042
		"supervisor_buffered_packets",
		"job_workspaces",
	} {
		exists, err := runtimeRunner.QueryScalar(ctx, fmt.Sprintf(
			"SELECT (to_regclass('striatumd.%s') IS NOT NULL)::text", table))
		if err != nil {
			t.Fatalf("check above-floor runtime table %s: %v", table, err)
		}
		if exists != "true" {
			t.Fatalf("above-floor runtime table striatumd.%s missing after two-role apply", table)
		}
	}
}

// TestTwoRoleApplyCatchesForbiddenOwnerFK is the negative control proving the
// two-role apply test above actually exercises the privilege boundary it claims
// to: a synthetic above-floor runtime migration that declares a FOREIGN KEY into
// the owner-held striatumd.repositories table — the literal migration 0041 / D242
// regression — must FAIL when applied as the runtime role striatumd_rw with
// `permission denied for table repositories` (SQLSTATE 42501). Without this the
// positive test could pass for the wrong reason (e.g. the runtime role silently
// owning everything), and a real regression would not be caught.
func TestTwoRoleApplyCatchesForbiddenOwnerFK(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv(pgtest.EnvPGTestURL))
	if baseURL == "" {
		t.Skip(pgtest.EnvPGTestURL + " not set; skipping live two-role migration negative control")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Lay down the full real schema as the owner via the standard pgtest path
	// (objects owner-owned, striatumd_rw a cluster role with membership), then
	// apply the owner bundles so the cohort transfers + grants are in place — the
	// same end state the live daemon migrates against.
	pool := pgtest.Pool(t)
	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "two-role-neg"); err != nil {
		t.Fatalf("apply owner bundles: %v", err)
	}

	runtimePool := newRuntimeRolePool(t, ctx, pool.URL)
	defer runtimePool.Close()
	runtimeConn, err := runtimePool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire runtime-role connection: %v", err)
	}
	defer runtimeConn.Release()

	// The forbidden DDL: a runtime-applied table with an inbound FK into the
	// owner-held repositories table. The runtime role has no REFERENCES privilege
	// on owner-held tables, so this must fail 42501 — exactly the prod 0041 crash.
	_, execErr := runtimeConn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS striatumd.two_role_neg_probe (
		  repository_id text NOT NULL REFERENCES striatumd.repositories(repository_id),
		  id text PRIMARY KEY
		)`)
	// Best-effort cleanup as the OWNER (the runtime role may lack DROP if the
	// table was created owner-owned in a partial run; the owner can always drop).
	t.Cleanup(func() {
		_ = pool.Runner.Exec(context.Background(), "DROP TABLE IF EXISTS striatumd.two_role_neg_probe")
	})

	if execErr == nil {
		t.Fatal("a runtime-role FK into the owner-held repositories table unexpectedly SUCCEEDED; " +
			"the two-role apply test is not actually exercising the privilege boundary it claims to (the #508 negative control is void)")
	}
	var pgErr *pgconn.PgError
	if !errors.As(execErr, &pgErr) {
		t.Fatalf("forbidden owner FK failed with a non-PostgreSQL error: %v", execErr)
	}
	if pgErr.Code != "42501" {
		t.Fatalf("forbidden owner FK failed with SQLSTATE %s (%s); want 42501 insufficient_privilege (the 0041 / D242 crash-loop signature)", pgErr.Code, pgErr.Message)
	}
}

// applyFloorMigrationsAsOwner applies, as the owner, every migration strictly
// below the runtime floor — recording the version stamps exactly like the
// production applyOne path (DDL + schema_migrations + schema_meta in one
// transaction) — and returns the resulting (floor-1) schema version. It does NOT
// use db.ApplyMigrations because that always runs the whole embedded set; here
// the owner lays down only the bootstrap floor so the runtime role applies the
// rest, faithfully reproducing the two-role deploy order.
func applyFloorMigrationsAsOwner(t *testing.T, ctx context.Context, runner db.Runner) int {
	t.Helper()
	migrations, err := db.Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	// The meta tables (schema_meta, schema_migrations) come from migration 0001,
	// so running the floor migrations in order bootstraps them before the stamps.
	if err := runner.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS striatumd;
		CREATE TABLE IF NOT EXISTS striatumd.schema_meta (
		  key text PRIMARY KEY,
		  value text NOT NULL
		);`); err != nil {
		t.Fatalf("owner: ensure meta table: %v", err)
	}
	current := 0
	for _, migration := range migrations {
		if migration.Version >= twoRoleRuntimeFloor {
			break
		}
		tx, err := runner.BeginTx(ctx)
		if err != nil {
			t.Fatalf("owner: begin tx for migration %d: %v", migration.Version, err)
		}
		if err := tx.Exec(ctx, migration.SQL); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("owner: apply floor migration %d: %v", migration.Version, err)
		}
		if err := tx.Exec(ctx,
			`INSERT INTO striatumd.schema_migrations(version, label, sha256, daemon_version)
			 VALUES ($1, $2, $3, $4) ON CONFLICT (version) DO NOTHING`,
			migration.Version, migration.Label, migration.SHA256(), "two-role-test"); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("owner: stamp floor migration %d: %v", migration.Version, err)
		}
		if err := tx.Exec(ctx,
			`INSERT INTO striatumd.schema_meta(key, value) VALUES ('substrate_version', $1)
			 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
			fmt.Sprintf("%d", migration.Version)); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("owner: stamp substrate_version for migration %d: %v", migration.Version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("owner: commit floor migration %d: %v", migration.Version, err)
		}
		current = migration.Version
	}
	return current
}

// createTwoRoleTestDatabase creates an isolated throwaway database (named like the
// pgtest databases so the pgtest stale sweep can reclaim a leaked one), returns
// its owner DSN, and registers a Cleanup that drops it. The connecting role owns
// it.
func createTwoRoleTestDatabase(t *testing.T, ctx context.Context, baseURL string) string {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse %s: %v", pgtest.EnvPGTestURL, err)
	}
	name := fmt.Sprintf("striatum_pgtest_%d_%d", time.Now().UnixNano(), os.Getpid())
	adminPool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatalf("connect admin database: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `CREATE DATABASE `+quoteTwoRoleIdent(name)); err != nil {
		adminPool.Close()
		t.Fatalf("create two-role test database: %v", err)
	}
	// Grant striatumd_rw the SAME database-level privileges the production
	// two-role deploy grants it: CONNECT plus CREATE (the live striatum_daemon DB
	// grants `striatumd_rw` CREATE so the daemon's startup `CREATE SCHEMA IF NOT
	// EXISTS striatumd` in ensureMetaTable runs as the runtime role — PostgreSQL
	// checks database CREATE before the IF NOT EXISTS short-circuit, so without it
	// even a no-op CREATE SCHEMA fails 42501). This grant reproduces the prod
	// posture so the test fails only on a real migration privilege regression, not
	// on a setup deficiency. The schema USAGE/sequence grants migration 0005 needs
	// are issued by the migration itself once the runtime role exists.
	if _, err := adminPool.Exec(ctx,
		`GRANT CONNECT, CREATE, TEMP ON DATABASE `+quoteTwoRoleIdent(name)+` TO striatumd_rw`); err != nil {
		adminPool.Close()
		t.Fatalf("grant database privileges to striatumd_rw: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = adminPool.Exec(dropCtx, `
			SELECT pg_terminate_backend(pid)
			  FROM pg_stat_activity
			 WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
		_, _ = adminPool.Exec(dropCtx, `DROP DATABASE IF EXISTS `+quoteTwoRoleIdent(name))
		adminPool.Close()
	})
	dbURL := *parsed
	dbURL.Path = "/" + name
	return dbURL.String()
}

// ensureRuntimeRoleForTwoRoleTest creates the cluster-wide striatumd_rw runtime
// role if absent (idempotent, tolerant of a concurrent creator), mirroring the
// pgtest harness so migration 0005 provisions its grant surface. It never drops
// the role.
func ensureRuntimeRoleForTwoRoleTest(t *testing.T, ctx context.Context, baseURL string) {
	t.Helper()
	adminPool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatalf("connect to ensure runtime role: %v", err)
	}
	defer adminPool.Close()
	if _, err := adminPool.Exec(ctx, `
		DO $$
		BEGIN
		  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'striatumd_rw') THEN
		    CREATE ROLE striatumd_rw;
		  END IF;
		EXCEPTION WHEN duplicate_object OR unique_violation THEN
		  NULL;
		END
		$$;`); err != nil {
		t.Fatalf("ensure striatumd_rw role: %v", err)
	}
}

// newRuntimeRolePool builds a pgxpool whose every connection runs
// `SET ROLE striatumd_rw` on connect, so all statements execute with the runtime
// role's privileges — the same AfterConnect SET ROLE the pgtest unprivileged pool
// uses. A pool (with the role pinned per connection) keeps the session-scoped
// advisory lock and unlock that ApplyMigrations takes on the same backend when a
// singleConnRunner holds one acquired connection across the whole apply.
func newRuntimeRolePool(t *testing.T, ctx context.Context, dbURL string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse runtime-role pool url: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE striatumd_rw")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create runtime-role pool: %v", err)
	}
	return pool
}

func quoteTwoRoleIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
