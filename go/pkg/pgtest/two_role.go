package pgtest

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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// twoRoleRuntimeFloor mirrors the unexported futureRuntimeMigrationOwnerDDLFloor
// in go/pkg/db/migrations_test.go (and twoRoleRuntimeFloor in
// migrations_two_role_pg_test.go): runtime migrations strictly below it were
// applied as the owner at bootstrap; only versions >= it apply as the runtime
// role striatumd_rw on a live daemon restart (RFC 0110 / D215). The two-role
// fixture reproduces that split so the tables above-floor migrations CREATE are
// striatumd_rw-owned, exactly as in production.
const twoRoleRuntimeFloor = 27

// twoRoleSUTPassword is the throwaway password the dedicated SUT login role
// authenticates with over TCP. The fixture databases are ephemeral and
// local-only; this mirrors the static passwords the authority-bootstrap pg tests
// use ('pw' / 'oldpw').
const twoRoleSUTPassword = "p0_two_role_sut_pw"

// TwoRoleFixture is an ephemeral PostgreSQL database bootstrapped to
// production's owner/runtime ownership split (RFC 0110 / D215), plus a dedicated,
// non-superuser, non-owner LOGIN role for the system-under-test (RFC 0142 P0 /
// D248 / GH #442). It is the executable oracle for the two-role 42501 trap: an
// owner-held-table touch by the SUT role fails SQLSTATE 42501, while a legal
// runtime operation succeeds — the discrimination the single-role pgtest.Pools
// path cannot reproduce.
//
// C1 (escape-proof SUT): SUTPool's session/login user is SUTRole — a dedicated
// LOGIN role that is NOT the owner, NOT a superuser, and a member of striatumd_rw
// ONLY (so it has exactly the runtime role's privileges and ownership rights).
// Because the login user is itself constrained, `RESET ROLE` / `SET ROLE NONE`
// cannot escape back to the owner the way `SET ROLE striatumd_rw` inside a
// privileged owner connection would.
type TwoRoleFixture struct {
	// OwnerPool runs as the bootstrap owner (the STRIATUM_PG_TEST_URL DSN user):
	// it owns the owner-held tables and is Phase A's authority. Use it to inspect
	// ownership or seed owner-owned state.
	OwnerPool *db.Pool
	// SUTPool runs every Phase B probe as SUTRole. RESET ROLE on it cannot reach
	// the owner (C1).
	SUTPool *db.Pool
	// OwnerURL is the owner DSN of the ephemeral database.
	OwnerURL string
	// OwnerRole is current_user of OwnerPool (the bootstrap owner role).
	OwnerRole string
	// SUTRole is the dedicated constrained LOGIN role behind SUTPool.
	SUTRole string
	// DBName is the ephemeral database name.
	DBName string
}

// TwoRole builds a TwoRoleFixture or skips when STRIATUM_PG_TEST_URL is unset.
// It discharges RFC 0142 P0's binding constraints C1–C5 and self-checks role
// isolation (C4) before returning, so a red 42501 a caller observes is trusted
// only when isolation provably holds.
//
// Like the rest of the _pg_test.go suite it needs a real cluster; the no-network
// verifier sandbox skips it.
func TwoRole(t *testing.T) *TwoRoleFixture {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv(EnvPGTestURL))
	if baseURL == "" {
		t.Skip(EnvPGTestURL + " not set; skipping live two-role PostgreSQL test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	testURL, drop := createDatabase(t, ctx, baseURL)
	t.Cleanup(drop)
	dbName := mustDBName(t, testURL)

	// The canonical, cluster-wide striatumd_rw runtime role (NOLOGIN), so the
	// migrations provision its grant surface and the owner bundles can transfer
	// ownership to it.
	ensureRuntimeRole(t, ctx, baseURL)

	ownerPool, err := db.Connect(ctx, testURL, "pgtest-two-role")
	if err != nil {
		t.Fatalf("connect two-role owner pool: %v", err)
	}
	t.Cleanup(ownerPool.Close)
	ownerRole, err := ownerPool.Runner.QueryScalar(ctx, "SELECT current_user")
	if err != nil {
		t.Fatalf("read owner current_user: %v", err)
	}

	// C3 — non-superuser bootstrap. ApplyOwnerBundles issues
	// `ALTER TABLE … OWNER TO striatumd_rw`, which requires the executing role to
	// be a member of striatumd_rw. Under a superuser owner DSN that membership is
	// implicit; under a NON-superuser owner it is not, so provision it for Phase A
	// and REVOKE it before Phase B. We only grant/revoke the membership WE add
	// (skip when the owner already holds it — e.g. a superuser owner), so a
	// superuser DSN's standing membership is left exactly as we found it.
	//
	// CONCURRENCY GUARD (RFC 0142 P0 review F-2): this GRANT/REVOKE mutates the
	// SHARED cluster owner role, so it is correct ONLY because the two-role tests
	// run sequentially — none call t.Parallel(). Do NOT add t.Parallel() to a
	// two-role test under a non-superuser owner DSN without first serializing
	// Phase A: with concurrent fixtures, fixture B could observe the membership
	// fixture A granted (ownerHadRW=true), skip its own grant, and then fixture A's
	// REVOKE would strip the membership out from under B mid-Phase-A and fail B's
	// ownership transfers. Serialize with a session advisory lock around Phase A,
	// or give each fixture a distinct owner role, before parallelizing.
	ownerHadRW := boolScalar(t, ctx, ownerPool.Runner,
		"SELECT pg_has_role('striatumd_rw', 'MEMBER')::text")
	if !ownerHadRW {
		if err := ownerPool.Runner.Exec(ctx, "GRANT striatumd_rw TO CURRENT_USER"); err != nil {
			t.Fatalf("C3: grant striatumd_rw to the non-superuser owner DSN for the ownership transfers: %v", err)
		}
	}

	// Phase A — bootstrap as the owner: apply the floor migrations (owner-owned
	// tables), then the owner bundles (transfer the runtime cohort to striatumd_rw
	// and grant it CREATE ON SCHEMA striatumd), then the out-of-band schema/DB
	// grants the runtime role needs.
	applyFloorMigrationsAsOwner(t, ctx, ownerPool.Runner)
	if _, _, err := db.ApplyOwnerBundles(ctx, ownerPool.Runner, "pgtest-two-role"); err != nil {
		t.Fatalf("Phase A: apply owner bundles: %v", err)
	}
	// Schema USAGE / sequence grants and the database-level CREATE are NOT carried
	// by any migration or owner bundle; `daemon doctor --provision-rw-role` and the
	// bootstrap runbook add them out-of-band, and the fixture must too — otherwise
	// the runtime role's above-floor CREATE TABLEs fail on a setup gap rather than
	// a real regression. (PostgreSQL checks database CREATE before a no-op
	// `CREATE SCHEMA IF NOT EXISTS` short-circuits, so the DB-level CREATE is
	// required even though striatumd already exists.)
	for _, stmt := range []string{
		fmt.Sprintf("GRANT CONNECT, CREATE, TEMP ON DATABASE %s TO striatumd_rw", quoteIdent(dbName)),
		"GRANT USAGE ON SCHEMA striatumd TO striatumd_rw",
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA striatumd TO striatumd_rw",
	} {
		if err := ownerPool.Runner.Exec(ctx, stmt); err != nil {
			t.Fatalf("Phase A: provision striatumd_rw grant (%q): %v", stmt, err)
		}
	}

	// C2 — bootstrap ownership fidelity. The above-floor (>= floor) migrations MUST
	// be applied AS striatumd_rw so the tables they CREATE
	// (supervisor_buffered_packets/0038, event_chain_segments/0041,
	// verifier_attestations/0042, …) are striatumd_rw-owned in the fixture exactly
	// as in prod — NOT owner-owned (which would false-red a legal runtime ALTER).
	// This is the live daemon-restart posture (ConnectAndMigrate runs as
	// striatumd_rw, RFC 0110 §9.1). SET ROLE here is a Phase A bootstrap detail,
	// not the escape-proof SUT path C1 governs.
	applyAboveFloorMigrationsAsRuntimeRole(t, ctx, testURL)

	// C1 — the dedicated, escape-proof SUT login role. It is a member of
	// striatumd_rw ONLY (inherits the runtime role's privileges + ownership rights
	// over the transferred / above-floor tables) and is neither the owner nor a
	// superuser, so a `RESET ROLE` in a candidate migration falls back to this
	// constrained login, as in prod.
	sutRole := twoRoleSUTRoleName(dbName)
	registerSUTRoleCleanup(t, baseURL, testURL, sutRole)
	for _, stmt := range []string{
		fmt.Sprintf("DROP ROLE IF EXISTS %s", quoteIdent(sutRole)),
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s'", quoteIdent(sutRole), twoRoleSUTPassword),
		fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", quoteIdent(dbName), quoteIdent(sutRole)),
		fmt.Sprintf("GRANT striatumd_rw TO %s", quoteIdent(sutRole)),
	} {
		if err := ownerPool.Runner.Exec(ctx, stmt); err != nil {
			t.Fatalf("C1: provision dedicated SUT login role (%q): %v", stmt, err)
		}
	}

	// C3 (cont.) — revoke the bootstrap transfer membership BEFORE Phase B so it
	// cannot leak into the SUT's isolation (the C3/C4 interaction), and prove it
	// is gone. (We only added it when ownerHadRW was false; a non-superuser owner
	// must report MEMBER=false after the revoke.)
	if !ownerHadRW {
		if err := ownerPool.Runner.Exec(ctx, "REVOKE striatumd_rw FROM CURRENT_USER"); err != nil {
			t.Fatalf("C3: revoke the bootstrap striatumd_rw membership from the owner DSN: %v", err)
		}
		if boolScalar(t, ctx, ownerPool.Runner, "SELECT pg_has_role('striatumd_rw', 'MEMBER')::text") {
			t.Fatalf("C3 gate: the bootstrap striatumd_rw membership was not revoked from the owner before Phase B")
		}
	}

	// C5 — pin the SUT connection's search_path to prod's value so relation
	// resolution / pg_temp shadowing cannot diverge. Low severity (the 42501 gates
	// on the resolved relation's ownership regardless), cheap fidelity hardening.
	sutURL := twoRoleSUTURL(t, testURL, sutRole)
	sutPool, err := connectSUTPool(ctx, sutURL, "striatumd, public")
	if err != nil {
		t.Fatalf("C1: connect as the dedicated SUT login role %q: %v", sutRole, err)
	}
	t.Cleanup(sutPool.Close)

	fixture := &TwoRoleFixture{
		OwnerPool: ownerPool,
		SUTPool:   sutPool,
		OwnerURL:  testURL,
		OwnerRole: ownerRole,
		SUTRole:   sutRole,
		DBName:    dbName,
	}
	// C4 — assert role isolation at probe time (after C3's grant is revoked).
	fixture.assertIsolation(t, ctx)
	return fixture
}

// assertIsolation is the C4 self-check: it aborts the fixture loudly unless the
// SUT role is provably isolated from owner power, so a red 42501 is trusted only
// when isolation holds. It runs on the SUT connection AFTER C3's bootstrap grant
// is revoked.
func (f *TwoRoleFixture) assertIsolation(t *testing.T, ctx context.Context) {
	t.Helper()
	r := f.SUTPool.Runner

	currentUser, err := r.QueryScalar(ctx, "SELECT current_user")
	if err != nil {
		t.Fatalf("C4 self-check: read current_user: %v", err)
	}
	if currentUser != f.SUTRole {
		t.Fatalf("C4 self-check: SUT connection current_user = %q, want the dedicated login role %q (the SUT must not run as the owner)", currentUser, f.SUTRole)
	}

	if boolScalar(t, ctx, r, "SELECT rolsuper::text FROM pg_roles WHERE rolname = current_user") {
		t.Fatalf("C4 self-check: SUT role %q is a SUPERUSER; a superuser bypasses every ownership check, so a red 42501 could never be trusted", f.SUTRole)
	}

	// Not a member of and does not inherit privileges from the owner role.
	// pg_has_role follows the whole membership graph, so this also catches a
	// transitive owner edge through striatumd_rw (e.g. a leaked C3 grant).
	for _, mode := range []string{"MEMBER", "USAGE"} {
		if boolScalar(t, ctx, r,
			fmt.Sprintf("SELECT pg_has_role($1, '%s')::text", mode), f.OwnerRole) {
			t.Fatalf("C4 self-check: SUT role %q has %s on the owner role %q; ownership / REFERENCES could leak through that edge and false-green the red test", f.SUTRole, mode, f.OwnerRole)
		}
	}

	// events is owner-held; job_recovery_state was transferred to striatumd_rw by
	// owner bundle 0018. If either is wrong the red test (events) or the green
	// control (job_recovery_state) is meaningless.
	eventsOwner, err := r.QueryScalar(ctx,
		"SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid = 'striatumd.events'::regclass")
	if err != nil {
		t.Fatalf("C4 self-check: read relowner(events): %v", err)
	}
	if eventsOwner == f.SUTRole || eventsOwner == "striatumd_rw" {
		t.Fatalf("C4 self-check: striatumd.events is owned by %q; it must stay owner-held (not the SUT role and not striatumd_rw) or the red 42501 oracle is void", eventsOwner)
	}
	jrsOwner, err := r.QueryScalar(ctx,
		"SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid = 'striatumd.job_recovery_state'::regclass")
	if err != nil {
		t.Fatalf("C4 self-check: read relowner(job_recovery_state): %v", err)
	}
	if jrsOwner != "striatumd_rw" {
		t.Fatalf("C4 self-check: striatumd.job_recovery_state owner = %q, want striatumd_rw (owner bundle 0018's transfer must have landed) or the green control is meaningless", jrsOwner)
	}

	// C5: the search_path pin must be in effect on the SUT connection.
	searchPath, err := r.QueryScalar(ctx, "SHOW search_path")
	if err != nil {
		t.Fatalf("C5 self-check: read search_path: %v", err)
	}
	if !strings.Contains(searchPath, "striatumd") {
		t.Fatalf("C5 self-check: SUT search_path = %q, want it pinned to include striatumd", searchPath)
	}
}

// applyFloorMigrationsAsOwner applies, as the owner, every migration strictly
// below the runtime floor — recording the version stamps exactly like the
// production applyOne path — so the above-floor migrations are still pending when
// the runtime role takes over. It mirrors the production two-role deploy order
// (the floor + owner bundles are bootstrap-applied as the owner; ongoing
// migrations apply as striatumd_rw). It is driven directly rather than through
// db.ApplyMigrations (which always runs the whole embedded set) — no product
// change.
func applyFloorMigrationsAsOwner(t *testing.T, ctx context.Context, runner db.Runner) {
	t.Helper()
	migrations, err := db.Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	// The meta tables (schema_meta, schema_migrations) come from migration 0001, so
	// running the floor migrations in order bootstraps them before the stamps.
	if err := runner.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS striatumd;
		CREATE TABLE IF NOT EXISTS striatumd.schema_meta (
		  key text PRIMARY KEY,
		  value text NOT NULL
		);`); err != nil {
		t.Fatalf("Phase A: ensure meta table: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version >= twoRoleRuntimeFloor {
			break
		}
		tx, err := runner.BeginTx(ctx)
		if err != nil {
			t.Fatalf("Phase A: begin tx for floor migration %d: %v", migration.Version, err)
		}
		if err := tx.Exec(ctx, migration.SQL); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("Phase A: apply floor migration %d: %v", migration.Version, err)
		}
		if err := tx.Exec(ctx,
			`INSERT INTO striatumd.schema_migrations(version, label, sha256, daemon_version)
			 VALUES ($1, $2, $3, $4) ON CONFLICT (version) DO NOTHING`,
			migration.Version, migration.Label, migration.SHA256(), "pgtest-two-role"); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("Phase A: stamp floor migration %d: %v", migration.Version, err)
		}
		if err := tx.Exec(ctx,
			`INSERT INTO striatumd.schema_meta(key, value) VALUES ('substrate_version', $1)
			 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
			fmt.Sprintf("%d", migration.Version)); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("Phase A: stamp substrate_version for migration %d: %v", migration.Version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("Phase A: commit floor migration %d: %v", migration.Version, err)
		}
	}
}

// applyAboveFloorMigrationsAsRuntimeRole applies every remaining (above-floor)
// migration as the runtime role striatumd_rw over a SINGLE held connection (so
// the session advisory lock ApplyMigrations takes is released on the same
// backend). The connection SET ROLEs to striatumd_rw, so the tables the
// above-floor migrations CREATE are striatumd_rw-owned — the C2 fidelity a
// Phase-A-as-owner bootstrap gets wrong.
func applyAboveFloorMigrationsAsRuntimeRole(t *testing.T, ctx context.Context, testURL string) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(testURL)
	if err != nil {
		t.Fatalf("parse runtime-role bootstrap pool url: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE striatumd_rw")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create runtime-role bootstrap pool: %v", err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire runtime-role connection: %v", err)
	}
	defer conn.Release()

	version, err := db.ApplyMigrations(ctx, singleConnRunner{conn: conn}, "pgtest-two-role")
	if err != nil {
		t.Fatalf("C2: apply the above-floor migration set as striatumd_rw (the live daemon-restart posture; a runtime migration needing an owner privilege it lacks would 42501 here): %v", err)
	}
	if version != db.LatestDaemonDBVersion {
		t.Fatalf("C2: after the two-role bootstrap schema version = %d, want %d", version, db.LatestDaemonDBVersion)
	}
}

// twoRoleSUTRoleName derives the dedicated SUT login role name from the ephemeral
// database name, truncated to PostgreSQL's 63-byte identifier limit.
func twoRoleSUTRoleName(dbName string) string {
	role := "striatumd_sut_" + dbName
	if len(role) > 63 {
		role = role[:63]
	}
	return role
}

// twoRoleSUTURL rebuilds the owner DSN into a TCP login URL for the SUT role: the
// owner connects over the local (peer) socket, while the dedicated login role
// authenticates over TCP with a password — the same split the authority-bootstrap
// pg tests use. When the base DSN carries no TCP host it falls back to
// localhost:5432.
func twoRoleSUTURL(t *testing.T, testURL, sutRole string) string {
	t.Helper()
	parsed, err := url.Parse(testURL)
	if err != nil {
		t.Fatalf("parse two-role test URL: %v", err)
	}
	host := strings.TrimSpace(parsed.Host)
	if host == "" {
		host = "localhost:5432"
	}
	query := parsed.Query()
	// Drop a unix-socket host override if present; the SUT connects over TCP.
	query.Del("host")
	sutURL := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(sutRole, twoRoleSUTPassword),
		Host:     host,
		Path:     parsed.Path,
		RawQuery: query.Encode(),
	}
	return sutURL.String()
}

// connectSUTPool opens the SUT pool and pins its search_path to prod's value on
// every connection (C5). It mirrors db.Connect's simple-protocol default so
// multi-statement probe DDL (e.g. `RESET ROLE; ALTER TABLE …`) runs unchanged.
func connectSUTPool(ctx context.Context, postgresURL, searchPath string) (*db.Pool, error) {
	cfg, err := pgxpool.ParseConfig(postgresURL)
	if err != nil {
		return nil, fmt.Errorf("parse SUT pool url: %w", err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+searchPath)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &db.Pool{
		URL:     postgresURL,
		Runner:  db.PgxRunner{Pool: pool},
		RawPool: pool,
		Close:   pool.Close,
	}, nil
}

// registerSUTRoleCleanup drops the cluster-wide SUT login role on test cleanup.
// It clears the role's in-database and shared (CONNECT) privileges first so the
// DROP ROLE succeeds even though the ephemeral database is dropped later; all
// steps are best-effort, mirroring pgtest.go's per-test login-role cleanup.
func registerSUTRoleCleanup(t *testing.T, baseURL, testURL, sutRole string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if dbPool, err := pgxpool.New(ctx, testURL); err == nil {
			_, _ = dbPool.Exec(ctx, fmt.Sprintf("DROP OWNED BY %s", quoteIdent(sutRole)))
			dbPool.Close()
		}
		if adminPool, err := pgxpool.New(ctx, baseURL); err == nil {
			_, _ = adminPool.Exec(ctx, fmt.Sprintf("REVOKE striatumd_rw FROM %s", quoteIdent(sutRole)))
			_, _ = adminPool.Exec(ctx, fmt.Sprintf("DROP ROLE IF EXISTS %s", quoteIdent(sutRole)))
			adminPool.Close()
		}
	})
}

func mustDBName(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	return strings.TrimPrefix(parsed.Path, "/")
}

// boolScalar runs a scalar query expected to yield a PostgreSQL boolean rendered
// as text ("true"/"false") and returns it as a Go bool.
func boolScalar(t *testing.T, ctx context.Context, runner db.Runner, sql string, args ...any) bool {
	t.Helper()
	value, err := runner.QueryScalar(ctx, sql, args...)
	if err != nil {
		t.Fatalf("scalar bool query %q: %v", sql, err)
	}
	return value == "true"
}

// singleConnRunner is a db.Runner bound to one acquired pooled connection, so a
// session advisory lock and the statements it guards run on the same physical
// backend (ApplyMigrations holds a session-level pg_advisory_lock across the
// whole apply).
type singleConnRunner struct{ conn *pgxpool.Conn }

func (r singleConnRunner) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := r.conn.Exec(ctx, sql, args...)
	return err
}

func (r singleConnRunner) QueryRow(ctx context.Context, sql string, args ...any) db.Row {
	return r.conn.QueryRow(ctx, sql, args...)
}

func (r singleConnRunner) QueryScalar(ctx context.Context, sql string, args ...any) (string, error) {
	var v string
	if err := r.conn.QueryRow(ctx, sql, args...).Scan(&v); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

func (r singleConnRunner) BeginTx(ctx context.Context) (db.TxRunner, error) {
	tx, err := r.conn.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &db.PgxTxRunner{Tx: tx}, nil
}
