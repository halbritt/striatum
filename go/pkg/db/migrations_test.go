package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const futureRuntimeMigrationOwnerDDLFloor = 27

var runtimeMigrationOwnerDDLPattern = regexp.MustCompile(`(?is)\b(?:ALTER|DROP)\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(striatumd\.[a-z_][a-z0-9_]*)`)

// ownerBundleLiteralTransferPattern matches a literal
// `ALTER TABLE [ONLY] striatumd.<name> OWNER TO striatumd_rw` ownership transfer
// in an owner bundle (the direct, non-loop form). The captured table is then
// alterable by a runtime migration.
var ownerBundleLiteralTransferPattern = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:ONLY\s+)?striatumd\.([a-z_][a-z0-9_]*)\s+OWNER\s+TO\s+striatumd_rw`)

// ownerBundleCohortTransferPattern matches the cohort-loop form owner bundle 0018
// uses: a `format('ALTER TABLE striatumd.%I OWNER TO striatumd_rw', …)` over a
// `v_cohort … ARRAY[ … ]` literal. When a bundle carries BOTH the cohort array
// and the `format(... OWNER TO striatumd_rw)` transfer over it, every
// single-quoted name inside that ARRAY[…] block is a table the bundle transfers
// to runtime ownership.
var ownerBundleCohortArrayPattern = regexp.MustCompile(`(?is)ARRAY\s*\[(.*?)\]`)
var ownerBundleFormatTransferPattern = regexp.MustCompile(`(?is)format\([^)]*ALTER\s+TABLE\s+striatumd\.%I\s+OWNER\s+TO\s+striatumd_rw`)
var cohortTableNamePattern = regexp.MustCompile(`'([a-z_][a-z0-9_]*)'`)

// runtimeOwnedTablesAlterable is the set of striatumd.* tables an above-floor
// runtime migration may legitimately ALTER, DERIVED from the owner bundles'
// actual `ALTER TABLE … OWNER TO striatumd_rw` ownership transfers rather than a
// name-based premise. THIS IS THE OWNERSHIP-AWARE GUARD (GH #442 / #441).
//
// The prior blanket allowlist (`"striatumd.job_recovery_state": true`, added by
// #336) was UNSOUND: it reasoned "created by a runtime migration ⇒ runtime-owned,"
// but on a two-role deploy the runbook applies the sql/NNNN runtime migrations as
// the OWNER role (`daemon migrate-db --admin-url`), so a runtime-migration-CREATEd
// table is owned by the BOOTSTRAP role (halbritt), NOT striatumd_rw. A runtime
// ALTER of it then crash-loops the daemon with `must be owner of table …` (42501,
// the prod incident #441/#442). pgtest is single-role and never reproduced it.
//
// The fix makes the allowlist truthful: a table is alterable-by-runtime ONLY when
// an owner bundle has TRANSFERRED its ownership to striatumd_rw (owner bundle
// 0018), which is applied as the owner before any runtime migration ALTERs it. So
// the guard passes BECAUSE the table is genuinely runtime-owned at ALTER time, not
// because of a name on a list. If owner bundle 0018 is ever dropped, the set goes
// empty and 0035/0036 immediately FAIL the guard — exactly the trap D187/D215
// describe, no escape.
func runtimeOwnedTablesAlterable(t *testing.T) map[string]bool {
	t.Helper()
	bundles, err := OwnerBundles()
	if err != nil {
		t.Fatalf("load owner bundles for ownership-transfer derivation: %v", err)
	}
	allowed := map[string]bool{}
	for _, bundle := range bundles {
		// Direct literal transfers: ALTER TABLE striatumd.<name> OWNER TO striatumd_rw.
		for _, m := range ownerBundleLiteralTransferPattern.FindAllStringSubmatch(bundle.SQL, -1) {
			allowed["striatumd."+strings.ToLower(m[1])] = true
		}
		// Cohort-loop transfers: a format('ALTER TABLE striatumd.%I OWNER TO
		// striatumd_rw', …) over a v_cohort ARRAY[…] literal. Only extract the
		// cohort-array names, and only when the transfer-over-the-cohort is present,
		// so unrelated quoted strings (role names, stamp capabilities) cannot widen
		// the allowlist.
		if ownerBundleFormatTransferPattern.MatchString(bundle.SQL) {
			for _, arr := range ownerBundleCohortArrayPattern.FindAllStringSubmatch(bundle.SQL, -1) {
				for _, m := range cohortTableNamePattern.FindAllStringSubmatch(arr[1], -1) {
					allowed["striatumd."+strings.ToLower(m[1])] = true
				}
			}
		}
	}
	return allowed
}

func runtimeMigrationOwnerDDLViolations(migration Migration, runtimeOwned map[string]bool) []string {
	matches := runtimeMigrationOwnerDDLPattern.FindAllStringSubmatch(migration.SQL, -1)
	violations := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		cleaned := strings.Join(strings.Fields(match[0]), " ")
		table := strings.ToLower(match[1])
		if runtimeOwned[table] {
			// A table an owner bundle has transferred to striatumd_rw ownership;
			// the runtime role may legitimately ALTER it (ownership-aware, GH #442).
			continue
		}
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		violations = append(violations, cleaned)
	}
	sort.Strings(violations)
	return violations
}

type fakeRow struct {
	value string
	err   error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) == 0 {
		return nil
	}
	switch target := dest[0].(type) {
	case *string:
		*target = r.value
	case **string:
		if r.value == "" {
			*target = nil
		} else {
			v := r.value
			*target = &v
		}
	}
	return nil
}

type fakeRunner struct {
	scalars map[string]string
	execs   []string
	// txs records every transaction handed out via BeginTx, in order, so a test
	// can inspect commit/rollback disposition and the statements each carried.
	txs []*fakeTx
	// failExecContaining, when non-empty, makes any tx Exec whose SQL contains
	// this substring fail. It simulates a crash/error between the migration DDL
	// and the version stamp and lets a test assert the whole transaction rolls
	// back (FMA-002 atomicity).
	failExecContaining string
}

// fakeTx is the transaction-scoped sibling of fakeRunner. It records the
// statements executed inside the transaction and whether it was committed or
// rolled back, so applyOne's atomicity is observable without a live PostgreSQL.
// On commit it publishes its statements to the parent's execs; on rollback it
// does not — mirroring real transactional visibility.
type fakeTx struct {
	parent     *fakeRunner
	execs      []string
	committed  bool
	rolledBack bool
}

func (f *fakeRunner) Exec(_ context.Context, sql string, _ ...any) error {
	f.execs = append(f.execs, sql)
	return nil
}

func (f *fakeRunner) QueryRow(_ context.Context, sql string, _ ...any) Row {
	for key, value := range f.scalars {
		if strings.Contains(sql, key) {
			return fakeRow{value: value}
		}
	}
	return fakeRow{err: pgx.ErrNoRows}
}

func (f *fakeRunner) QueryScalar(_ context.Context, sql string, _ ...any) (string, error) {
	for key, value := range f.scalars {
		if strings.Contains(sql, key) {
			return value, nil
		}
	}
	return "", nil
}

func (f *fakeRunner) BeginTx(_ context.Context) (TxRunner, error) {
	tx := &fakeTx{parent: f}
	f.txs = append(f.txs, tx)
	return tx, nil
}

func (t *fakeTx) Exec(_ context.Context, sql string, _ ...any) error {
	if needle := t.parent.failExecContaining; needle != "" && strings.Contains(sql, needle) {
		return errors.New("injected exec failure for: " + needle)
	}
	t.execs = append(t.execs, sql)
	return nil
}

func (t *fakeTx) QueryRow(_ context.Context, sql string, _ ...any) Row {
	return t.parent.QueryRow(context.Background(), sql)
}

func (t *fakeTx) QueryScalar(_ context.Context, sql string, _ ...any) (string, error) {
	return t.parent.QueryScalar(context.Background(), sql)
}

func (t *fakeTx) Commit(_ context.Context) error {
	t.committed = true
	// Publish the transaction's statements to the parent only on commit, so
	// fakeRunner.execs reflects durably-applied SQL (rolled-back work stays
	// invisible), matching real transactional semantics.
	t.parent.execs = append(t.parent.execs, t.execs...)
	return nil
}

func (t *fakeTx) Rollback(_ context.Context) error {
	t.rolledBack = true
	return nil
}

func TestMigrationsAreOrdered(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(migrations) != LatestDaemonDBVersion {
		t.Fatalf("migration count = %d, want %d", len(migrations), LatestDaemonDBVersion)
	}
	for index, migration := range migrations {
		if migration.Version != index+1 {
			t.Fatalf("migration %d has version %d", index, migration.Version)
		}
		if migration.SHA256() == "" {
			t.Fatalf("migration %d missing sha", migration.Version)
		}
	}
}

func TestVerifyMigrationsSHASourceRejectsExtraSourceMigration(t *testing.T) {
	// Post-RFC-0078 the embedded migrations are the canonical source of truth
	// (the Python source tree is gone). Reconstruct a source dir from the embed,
	// add a spurious newer file, and assert VerifyMigrationsSHASource rejects it.
	tmp := t.TempDir()
	entries, err := migrationFS.ReadDir("sql")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := migrationFS.ReadFile(filepath.Join("sql", entry.Name()))
		if err != nil {
			t.Fatalf("read embedded migration: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tmp, entry.Name()), body, 0o644); err != nil {
			t.Fatalf("write copied migration: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "9999_future.sql"), []byte("SELECT 1;\n"), 0o644); err != nil {
		t.Fatalf("write future migration: %v", err)
	}

	err = VerifyMigrationsSHASource(tmp)

	if err == nil || !strings.Contains(err.Error(), "newer than the embedded Go daemon migrations") {
		t.Fatalf("expected extra migration refusal, got %v", err)
	}
}

func TestMigrationFiveCarriesRepoLocalWorkflowTables(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migrationFive *Migration
	for index := range migrations {
		if migrations[index].Version == 5 {
			migrationFive = &migrations[index]
			break
		}
	}
	if migrationFive == nil {
		t.Fatal("migration 5 is missing")
	}
	for _, table := range []string{
		"workflow_snapshots",
		"runs",
		"sessions",
		"jobs",
		"queue_messages",
		"leases",
		"work_packets",
		"events",
	} {
		if !strings.Contains(migrationFive.SQL, "striatumd."+table) {
			t.Fatalf("migration 5 missing repo-local table %s", table)
		}
	}
}

func TestMigrationFourteenCarriesAutoFinalizeCircuitBreakers(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migrationFourteen *Migration
	for index := range migrations {
		if migrations[index].Version == 14 {
			migrationFourteen = &migrations[index]
			break
		}
	}
	if migrationFourteen == nil {
		t.Fatal("migration 14 is missing")
	}
	for _, needle := range []string{
		"striatumd.auto_finalize_circuit_breakers",
		"PRIMARY KEY (repository_id, run_id, workflow_job_id, cause)",
		"auto_finalize_circuit_breakers_open_idx",
		"GRANT SELECT, INSERT, UPDATE, DELETE",
	} {
		if !strings.Contains(migrationFourteen.SQL, needle) {
			t.Fatalf("migration 14 missing %q", needle)
		}
	}
}

// TestMigrationSixteenInterrogationsIsOwnershipSafe is RFC 0082 Required Test 9:
// the interrogations migration creates a new table + grants the runtime role
// and does NOT ALTER owner tables nor declare foreign keys referencing
// owner-held tables (regression guard for the RFC 0081 incident).
func TestMigrationSixteenInterrogationsIsOwnershipSafe(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration *Migration
	for index := range migrations {
		if migrations[index].Version == 16 {
			migration = &migrations[index]
			break
		}
	}
	if migration == nil {
		t.Fatal("migration 16 is missing")
	}
	sql := migration.SQL
	for _, needle := range []string{
		"CREATE TABLE IF NOT EXISTS striatumd.interrogations",
		"PRIMARY KEY (repository_id, interrogation_id)",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON striatumd.interrogations TO striatumd_rw",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("migration 16 missing %q", needle)
		}
	}
	// Ownership-safety: no ALTER of any table, and no foreign keys to the
	// owner-held tables. striatumd_rw must be able to apply this migration.
	for _, forbidden := range []string{
		"ALTER TABLE",
		"REFERENCES striatumd.repositories",
		"REFERENCES striatumd.runs",
		"REFERENCES striatumd.sessions",
		"FOREIGN KEY",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration 16 must not contain %q (owner-table dependency); referential integrity is enforced in Go", forbidden)
		}
	}
}

// TestMigrationTwentyThreePrincipalsIsOwnershipSafe is RFC 0107: the
// multi-principal migration creates NEW tables + grants the runtime role and
// does NOT ALTER any owner-held table nor declare a foreign key referencing one
// (regression guard for the RFC 0081 owner-table crash-loop). The only FK it
// declares is principal_clients -> principals, both NEW tables the runtime role
// owns; principal_clients.client_id is a bare text column with no FK to the
// owner-held clients table (audit attribution / referential integrity is
// enforced in Go, exactly like audit_log.client_id).
func TestMigrationTwentyThreePrincipalsIsOwnershipSafe(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration *Migration
	for index := range migrations {
		if migrations[index].Version == 23 {
			migration = &migrations[index]
			break
		}
	}
	if migration == nil {
		t.Fatal("migration 23 is missing")
	}
	sql := migration.SQL
	for _, needle := range []string{
		"CREATE TABLE IF NOT EXISTS striatumd.principals",
		"CREATE TABLE IF NOT EXISTS striatumd.principal_clients",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON striatumd.principals TO striatumd_rw",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON striatumd.principal_clients TO striatumd_rw",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("migration 23 missing %q", needle)
		}
	}
	// Ownership-safety: no ALTER of any table, and no foreign key to any
	// owner-held table. striatumd_rw must be able to apply this migration.
	for _, forbidden := range []string{
		"ALTER TABLE",
		"REFERENCES striatumd.clients",
		"REFERENCES striatumd.client_capabilities",
		"REFERENCES striatumd.repositories",
		"REFERENCES striatumd.sessions",
		"REFERENCES striatumd.runs",
		"FOREIGN KEY",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration 23 must not contain %q (owner-table dependency); attribution is enforced in Go", forbidden)
		}
	}
}

// TestMigrationTwentyEightJobWorkspacesIsOwnershipSafe is RFC 0127 P0 (D195): the
// plain-dir workspace migration creates a NEW runtime-owned table + grants the
// runtime role, recording the staged base tree sha. Like migrations 16/23 it must
// NOT ALTER any table (the regular-migration owner-DDL guard forbids ALTER of
// striatumd.* for versions >= 27 outright) and must declare NO foreign key
// (referential integrity is enforced in Go in the workspace.create handler), so
// the runtime role striatumd_rw can apply it without the RFC 0081 owner-table
// crash-loop.
func TestMigrationTwentyEightJobWorkspacesIsOwnershipSafe(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration *Migration
	for index := range migrations {
		if migrations[index].Version == 28 {
			migration = &migrations[index]
			break
		}
	}
	if migration == nil {
		t.Fatal("migration 28 is missing")
	}
	sql := migration.SQL
	for _, needle := range []string{
		"CREATE TABLE IF NOT EXISTS striatumd.job_workspaces",
		"PRIMARY KEY (repository_id, workspace_id)",
		"base_tree_sha text NOT NULL",
		"workspace_kind text NOT NULL CHECK (workspace_kind IN ('plain_dir'))",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON striatumd.job_workspaces TO striatumd_rw",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("migration 28 missing %q", needle)
		}
	}
	// Ownership-safety: no ALTER/DROP of any table, and no foreign key to any
	// table. striatumd_rw must be able to apply this migration.
	for _, forbidden := range []string{
		"ALTER TABLE",
		"DROP TABLE",
		"REFERENCES striatumd.repositories",
		"REFERENCES striatumd.runs",
		"REFERENCES striatumd.jobs",
		"REFERENCES striatumd.leases",
		"FOREIGN KEY",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration 28 must not contain %q (owner-table dependency / future-runtime-DDL guard); integrity is enforced in Go", forbidden)
		}
	}
}

// TestMigrationEighteenWidensArtifactAttemptScope is RFC 0095 §1 / GH #84:
// migration 18 records the producing attempt on each artifact and widens BOTH
// artifact unique keys with `attempt`, so a re-opened attempt may republish the
// same logical_name/path under its own attempt. It is owner-applied (it ALTERs
// the owner-held artifacts table), so unlike migrations >=16 it is ALLOWED to
// ALTER an owner table and there is no ownership-safety guard here.
func TestMigrationEighteenWidensArtifactAttemptScope(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration *Migration
	for index := range migrations {
		if migrations[index].Version == 18 {
			migration = &migrations[index]
			break
		}
	}
	if migration == nil {
		t.Fatal("migration 18 is missing")
	}
	sql := migration.SQL
	for _, needle := range []string{
		"ALTER TABLE striatumd.artifacts",
		"ADD COLUMN IF NOT EXISTS attempt integer NOT NULL DEFAULT 1",
		"DROP CONSTRAINT IF EXISTS artifacts_repository_id_run_id_job_id_logical_name_key",
		"UNIQUE (repository_id, run_id, job_id, logical_name, attempt)",
		"DROP CONSTRAINT IF EXISTS artifacts_repository_id_run_id_repo_path_content_sha256_key",
		"UNIQUE (repository_id, run_id, repo_path, content_sha256, attempt)",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("migration 18 missing %q", needle)
		}
	}
}

// TestMigrationNineteenAddsPTYAndToolLivenessColumns is RFC 0101 Phase 1: the
// migration adds last_pty_activity_at / last_tool_call_started_at /
// last_tool_call_finished_at to the owner-held sessions table so the classifier
// can fuse PTY + tool-call signals (#80/#83/#117). Like migration 12 it ALTERs
// an owner table, so it is owner-applied; it is idempotent (every ADD COLUMN is
// IF NOT EXISTS) so a re-run is a safe no-op.
func TestMigrationNineteenAddsPTYAndToolLivenessColumns(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration *Migration
	for index := range migrations {
		if migrations[index].Version == 19 {
			migration = &migrations[index]
			break
		}
	}
	if migration == nil {
		t.Fatal("migration 19 is missing")
	}
	sql := migration.SQL
	for _, needle := range []string{
		"ALTER TABLE striatumd.sessions",
		"ADD COLUMN IF NOT EXISTS last_pty_activity_at timestamptz",
		"ADD COLUMN IF NOT EXISTS last_tool_call_started_at timestamptz",
		"ADD COLUMN IF NOT EXISTS last_tool_call_finished_at timestamptz",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("migration 19 missing %q", needle)
		}
	}
	if migration.Label == "" {
		t.Fatal("migration 19 has no label")
	}
}

// TestMigrationTwentyNineFaninBarrierIsOwnershipSafe is RFC 0135 P1 (D215/D216):
// the fan-in sealed-barrier migration creates the two NEW runtime-owned tables the
// live-seal JOIN barrier reads (the append-only freeze record + the
// attempt-addressed staging table) and grants the runtime role, with the
// freeze table granted SELECT, INSERT ONLY (append-only) backed by a refuse-trigger.
// Like migrations 16/23/28 it must NOT ALTER/DROP any owner table (the
// future-runtime-DDL guard forbids ALTER/DROP of striatumd.* for versions >= 27
// outright) and must declare NO foreign key to striatumd.jobs (the FK-to-owner-
// table trap, D215): the seat identity is carried as bare columns and referential
// integrity is enforced in Go (the live-seal JOIN at evaluation time).
func TestMigrationTwentyNineFaninBarrierIsOwnershipSafe(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration *Migration
	for index := range migrations {
		if migrations[index].Version == 29 {
			migration = &migrations[index]
			break
		}
	}
	if migration == nil {
		t.Fatal("migration 29 is missing")
	}
	sql := migration.SQL
	for _, needle := range []string{
		"CREATE TABLE IF NOT EXISTS striatumd.fanin_freeze_points",
		"CREATE TABLE IF NOT EXISTS striatumd.barrier_staged_contributions",
		"PRIMARY KEY (repository_id, barrier_id)",
		"PRIMARY KEY (repository_id, barrier_id, workflow_job_id, attempt)",
		// The freeze record is append-only: SELECT, INSERT only + a refuse-trigger.
		"GRANT SELECT, INSERT ON striatumd.fanin_freeze_points TO striatumd_rw",
		"refuse_repo_append_only_change",
		"BEFORE UPDATE ON striatumd.fanin_freeze_points",
		"BEFORE DELETE ON striatumd.fanin_freeze_points",
		// The staging table carries full DML (a requeue tombstones a stale row).
		"GRANT SELECT, INSERT, UPDATE, DELETE ON striatumd.barrier_staged_contributions TO striatumd_rw",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("migration 29 missing %q", needle)
		}
	}
	// Ownership-safety: no ALTER/DROP of any table, and no foreign key to any
	// table. striatumd_rw must be able to apply this migration; the seat identity
	// (repository_id, run_id, workflow_job_id, attempt) is bare columns and the
	// referential integrity is enforced in Go.
	for _, forbidden := range []string{
		"ALTER TABLE",
		"DROP TABLE",
		"REFERENCES striatumd.repositories",
		"REFERENCES striatumd.runs",
		"REFERENCES striatumd.jobs",
		"FOREIGN KEY",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration 29 must not contain %q (owner-table dependency / future-runtime-DDL guard); integrity is enforced in Go", forbidden)
		}
	}
}

// TestMigrationThirtyBarrierStateIsOwnershipSafe is RFC 0135 P2 (D215/D216): the
// barrier-assembly state migration creates ONE NEW runtime-owned table
// (striatumd.barrier_state) carrying the two-phase journal columns
// (target_commit_sha + tree_sha) and grants the runtime role full DML. Like
// migration 29 it must NOT ALTER/DROP any owner table (the future-runtime-DDL guard
// forbids ALTER/DROP of striatumd.* for versions >= 27 outright), must declare NO
// foreign key to striatumd.jobs (the FK-to-owner-table trap, D215 — the
// barrier_assembly job_type CHECK widening is OWNER BUNDLE 0013, not this runtime
// migration), and must carry an explicit GRANT guarded by an IF EXISTS striatumd_rw
// probe (pgtest masks an omitted grant).
func TestMigrationThirtyBarrierStateIsOwnershipSafe(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration *Migration
	for index := range migrations {
		if migrations[index].Version == 30 {
			migration = &migrations[index]
			break
		}
	}
	if migration == nil {
		t.Fatal("migration 30 is missing")
	}
	sql := migration.SQL
	for _, needle := range []string{
		"CREATE TABLE IF NOT EXISTS striatumd.barrier_state",
		"PRIMARY KEY (repository_id, barrier_id)",
		// The two-phase journal columns (RFC 0133 / RFC 0135 Risks).
		"target_commit_sha",
		"tree_sha",
		// state walks sealed -> assembling -> committed|failed.
		"CHECK (state IN ('sealed','assembling','committed','failed'))",
		// Explicit GRANT guarded by an IF EXISTS striatumd_rw probe.
		"GRANT SELECT, INSERT, UPDATE, DELETE ON striatumd.barrier_state TO striatumd_rw",
		"rolname = 'striatumd_rw'",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("migration 30 missing %q", needle)
		}
	}
	// Ownership-safety: no ALTER/DROP of any table, no foreign key to any owner
	// table, and (per the task guardrail) no owner-table-FK text even in comments.
	for _, forbidden := range []string{
		"ALTER TABLE",
		"DROP TABLE",
		"REFERENCES striatumd.repositories",
		"REFERENCES striatumd.runs",
		"REFERENCES striatumd.jobs",
		"FOREIGN KEY",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration 30 must not contain %q (owner-table dependency / future-runtime-DDL guard); integrity is enforced in Go", forbidden)
		}
	}
}

// TestMigrationThirtyOneBarrierStatusViewIsOwnershipSafe is RFC 0135 P3 (D215/D216):
// the barrier_status read/audit view migration (#347) creates a NEW view over the
// runtime barrier tables. CREATE VIEW is runtime-safe — it does NOT ALTER/DROP any
// owner-held table (so the future-runtime-DDL guard, which forbids ALTER/DROP of
// striatumd.* for versions >= 27, never fires), needs no owner bundle, and carries
// no FOREIGN KEY (a view has no referential keys). It must carry an explicit GRANT
// guarded by an IF EXISTS striatumd_rw probe (pgtest masks an omitted grant).
func TestMigrationThirtyOneBarrierStatusViewIsOwnershipSafe(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration *Migration
	for index := range migrations {
		if migrations[index].Version == 31 {
			migration = &migrations[index]
			break
		}
	}
	if migration == nil {
		t.Fatal("migration 31 is missing")
	}
	sql := migration.SQL
	for _, needle := range []string{
		"CREATE OR REPLACE VIEW striatumd.barrier_status",
		"BARRIER_BLOCKED",
		// Read-only GRANT guarded by an IF EXISTS striatumd_rw probe.
		"GRANT SELECT ON striatumd.barrier_status TO striatumd_rw",
		"rolname = 'striatumd_rw'",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("migration 31 missing %q", needle)
		}
	}
	// Ownership-safety: no ALTER/DROP of any table, and no foreign key (a view has
	// no referential keys; integrity is the live-seal JOIN in the view body).
	for _, forbidden := range []string{
		"ALTER TABLE",
		"DROP TABLE",
		"FOREIGN KEY",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration 31 must not contain %q (owner-table dependency / future-runtime-DDL guard)", forbidden)
		}
	}
}

func TestFutureRuntimeMigrationsDoNotCarryOwnerDDL(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	runtimeOwned := runtimeOwnedTablesAlterable(t)
	for _, migration := range migrations {
		if migration.Version < futureRuntimeMigrationOwnerDDLFloor {
			continue
		}
		if violations := runtimeMigrationOwnerDDLViolations(migration, runtimeOwned); len(violations) > 0 {
			t.Fatalf("migration %d carries owner-table DDL forbidden in regular runtime migrations: %v", migration.Version, violations)
		}
	}
}

// TestOwnershipAwareGuardCatchesAlterAbsentTransfer is the GH #442 / #441
// regression: the owner-DDL guard MUST catch an above-floor runtime ALTER of a
// bootstrap-owned table when no owner bundle has transferred that table to
// striatumd_rw — there is NO name-based allowlist escape. It proves both that the
// guard would have caught migration 0035's ALTER of job_recovery_state with the
// ownership transfer removed (the prod crash-loop), AND that the live tree passes
// only BECAUSE owner bundle 0018 truthfully transfers ownership before the
// runtime migrations run.
func TestOwnershipAwareGuardCatchesAlterAbsentTransfer(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration0035 *Migration
	for index := range migrations {
		if migrations[index].Version == 35 {
			migration0035 = &migrations[index]
			break
		}
	}
	if migration0035 == nil {
		t.Fatal("migration 35 is missing")
	}

	// With NO ownership-transfer guarantee (empty allowlist) the guard FIRES on
	// 0035's ALTER of the bootstrap-owned job_recovery_state — the prod 42501.
	withoutTransfer := runtimeMigrationOwnerDDLViolations(*migration0035, map[string]bool{})
	if len(withoutTransfer) == 0 {
		t.Fatal("ownership-aware guard must catch migration 0035's ALTER of job_recovery_state absent an owner-bundle ownership transfer (the #442 crash-loop)")
	}
	foundJRS := false
	for _, v := range withoutTransfer {
		if strings.Contains(v, "striatumd.job_recovery_state") {
			foundJRS = true
		}
	}
	if !foundJRS {
		t.Fatalf("expected the job_recovery_state ALTER among violations, got %v", withoutTransfer)
	}

	// The live allowlist is DERIVED from the owner bundles' actual ownership
	// transfers (owner bundle 0018), not a name list. It must contain
	// job_recovery_state (so the live guard passes truthfully) and it must NOT be
	// empty (a missing/regressed bundle 0018 would empty it and re-red 0035).
	runtimeOwned := runtimeOwnedTablesAlterable(t)
	if !runtimeOwned["striatumd.job_recovery_state"] {
		t.Fatal("owner bundle 0018 must transfer job_recovery_state to striatumd_rw so migration 0035's ALTER is truthfully runtime-safe")
	}
	if !runtimeOwned["striatumd.barrier_staged_contributions"] {
		t.Fatal("owner bundle 0018 must transfer barrier_staged_contributions to striatumd_rw so migration 0036's ALTER is truthfully runtime-safe")
	}
	withTransfer := runtimeMigrationOwnerDDLViolations(*migration0035, runtimeOwned)
	if len(withTransfer) != 0 {
		t.Fatalf("migration 0035 must pass the ownership-aware guard once bundle 0018 transfers job_recovery_state ownership; got violations %v", withTransfer)
	}
}

// TestOwnershipTransferBundleCoversAboveFloorRuntimeAlters proves the allowlist
// and the ownership-transfer bundle are CONSISTENT: every striatumd.* table an
// above-floor runtime migration ALTERs must be one the owner bundles transfer to
// striatumd_rw. This is the standing invariant that keeps a NEW above-floor
// runtime ALTER from sneaking past — if a future migration ALTERs a table no
// owner bundle has transferred, this test (and the main guard) goes red until the
// transfer is added to owner bundle 0018's cohort (or the ALTER is moved to an
// owner bundle).
func TestOwnershipTransferBundleCoversAboveFloorRuntimeAlters(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	runtimeOwned := runtimeOwnedTablesAlterable(t)
	for _, migration := range migrations {
		if migration.Version < futureRuntimeMigrationOwnerDDLFloor {
			continue
		}
		for _, match := range runtimeMigrationOwnerDDLPattern.FindAllStringSubmatch(migration.SQL, -1) {
			table := strings.ToLower(match[1])
			if !runtimeOwned[table] {
				t.Fatalf("migration %d ALTERs %s but no owner bundle transfers it to striatumd_rw; add it to owner bundle 0018's cohort or move the DDL to an owner bundle (GH #442)", migration.Version, table)
			}
		}
	}
}

func TestRuntimeMigrationOwnerDDLGuardDetectsForbiddenDDL(t *testing.T) {
	migration := Migration{
		Version: futureRuntimeMigrationOwnerDDLFloor,
		SQL: `
			ALTER TABLE striatumd.runs ADD COLUMN IF NOT EXISTS unsafe text;
			DROP TABLE IF EXISTS ONLY striatumd.sessions;
			CREATE TABLE IF NOT EXISTS striatumd.new_runtime_table (id text PRIMARY KEY);
		`,
	}

	violations := runtimeMigrationOwnerDDLViolations(migration, runtimeOwnedTablesAlterable(t))

	expected := []string{
		"ALTER TABLE striatumd.runs",
		"DROP TABLE IF EXISTS ONLY striatumd.sessions",
	}
	if !reflect.DeepEqual(violations, expected) {
		t.Fatalf("violations = %#v, want %#v", violations, expected)
	}
}

// TestMigration33ReapsTerminalRunSupervisors (#417): the backfill marks every
// supervisor still in a live state on an already-terminal run stopped across all
// three supervisor tables, and is pure DML (no owner-table DDL), so striatumd_rw
// applies it cleanly on a two-role production daemon.
func TestMigration33ReapsTerminalRunSupervisors(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration *Migration
	for index := range migrations {
		if migrations[index].Version == 33 {
			migration = &migrations[index]
			break
		}
	}
	if migration == nil {
		t.Fatal("migration 33 is missing")
	}
	sql := migration.SQL
	for _, needle := range []string{
		"UPDATE striatumd.process_supervisors",
		"UPDATE striatumd.daemon_supervisors",
		"UPDATE striatumd.process_supervisor_pointers",
		"r.state IN ('completed', 'failed', 'canceled')",
		"state IN ('starting', 'attached', 'detached')",
		"state = 'stopped'",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("migration 33 missing %q", needle)
		}
	}
	if violations := runtimeMigrationOwnerDDLViolations(*migration, runtimeOwnedTablesAlterable(t)); len(violations) > 0 {
		t.Fatalf("migration 33 must be pure DML, found owner DDL: %v", violations)
	}
}

// TestMigration37ResolvesTerminalRunBlockers (#420): the backfill resolves every
// still-open blocker on an already-terminal run (plus the escalation_inbox mirror),
// and is pure DML (no owner-table DDL), so striatumd_rw applies it cleanly.
func TestMigration37ResolvesTerminalRunBlockers(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration *Migration
	for index := range migrations {
		if migrations[index].Version == 37 {
			migration = &migrations[index]
			break
		}
	}
	if migration == nil {
		t.Fatal("migration 37 is missing")
	}
	sql := migration.SQL
	for _, needle := range []string{
		"UPDATE striatumd.blockers",
		"UPDATE striatumd.escalation_inbox",
		"r.state IN ('completed', 'failed', 'canceled')",
		"b.state = 'open'",
		"state = 'resolved'",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("migration 37 missing %q", needle)
		}
	}
	if violations := runtimeMigrationOwnerDDLViolations(*migration, runtimeOwnedTablesAlterable(t)); len(violations) > 0 {
		t.Fatalf("migration 37 must be pure DML, found owner DDL: %v", violations)
	}
}

// TestMigration38SupervisorBufferedPacketsIsOwnershipSafe (#456 / FMA-006): the
// durable no-reader push-packet buffer creates ONE NEW runtime-owned table
// (striatumd.supervisor_buffered_packets) and grants the runtime role full DML.
// Like migrations 29/30 it must NOT ALTER/DROP any owner table (the future-runtime-
// DDL guard forbids ALTER/DROP of striatumd.* for versions >= 27 outright) and must
// declare NO foreign key to any owner-held table (repository_id / supervisor_id are
// bare columns; the supervisor lifecycle owns its own teardown), so striatumd_rw can
// both create and write it without a later runtime migration touching a
// bootstrap-owned table (the #442 trap).
func TestMigration38SupervisorBufferedPacketsIsOwnershipSafe(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var migration *Migration
	for index := range migrations {
		if migrations[index].Version == 38 {
			migration = &migrations[index]
			break
		}
	}
	if migration == nil {
		t.Fatal("migration 38 is missing")
	}
	sql := migration.SQL
	for _, needle := range []string{
		"CREATE TABLE IF NOT EXISTS striatumd.supervisor_buffered_packets",
		"PRIMARY KEY (repository_id, supervisor_id, pipe_path, seq)",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON striatumd.supervisor_buffered_packets TO striatumd_rw",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("migration 38 missing %q", needle)
		}
	}
	for _, forbidden := range []string{
		"ALTER TABLE",
		"DROP TABLE",
		"REFERENCES striatumd.repositories",
		"REFERENCES striatumd.runs",
		"REFERENCES striatumd.jobs",
		"FOREIGN KEY",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration 38 must not contain %q (owner-table dependency / future-runtime-DDL guard); integrity is enforced in Go", forbidden)
		}
	}
	if violations := runtimeMigrationOwnerDDLViolations(*migration, runtimeOwnedTablesAlterable(t)); len(violations) > 0 {
		t.Fatalf("migration 38 must not carry owner DDL, found: %v", violations)
	}
}

func TestApplyMigrationsRecordsVersion(t *testing.T) {
	runner := &fakeRunner{scalars: map[string]string{}}
	version, err := ApplyMigrations(context.Background(), runner, "test")
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if version != LatestDaemonDBVersion {
		t.Fatalf("version = %d, want %d", version, LatestDaemonDBVersion)
	}
	joined := strings.Join(runner.execs, "\n")
	if !strings.Contains(joined, "substrate_version") {
		t.Fatalf("substrate version was not recorded")
	}
}

// TestApplyOneIsTransactional (FMA-002) asserts that applyOne runs the migration
// DDL and BOTH version stamps (schema_migrations + schema_meta.substrate_version)
// inside ONE transaction that is committed exactly once. Before the fix these
// were three separate autocommit Exec calls, so a crash between the DDL and the
// stamps left the schema DDL-applied yet version-unstamped.
func TestApplyOneIsTransactional(t *testing.T) {
	runner := &fakeRunner{scalars: map[string]string{}}
	migration := Migration{Version: 99, Label: "test atomic apply", SQL: "CREATE TABLE striatumd.fma002_probe (id int);"}

	if err := applyOne(context.Background(), runner, migration, "test"); err != nil {
		t.Fatalf("applyOne: %v", err)
	}

	if len(runner.txs) != 1 {
		t.Fatalf("applyOne opened %d transactions, want exactly 1", len(runner.txs))
	}
	tx := runner.txs[0]
	if !tx.committed {
		t.Fatal("applyOne did not commit the transaction")
	}
	if tx.rolledBack {
		t.Fatal("applyOne rolled back a successful transaction")
	}
	// The DDL and both stamps must all have run inside the single transaction —
	// none of them leaked to an autocommit Exec on the pool. (The pool .execs
	// only contains what the tx published on commit.)
	joined := strings.Join(tx.execs, "\n")
	for _, needle := range []string{
		"fma002_probe", // the migration DDL
		"INSERT INTO striatumd.schema_migrations", // the version stamp
		"substrate_version",                       // the schema_meta stamp
	} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("transaction did not carry %q; statements=%q", needle, tx.execs)
		}
	}
	if len(tx.execs) != 3 {
		t.Fatalf("transaction carried %d statements, want exactly 3 (DDL + 2 stamps): %q", len(tx.execs), tx.execs)
	}
}

// TestApplyOneRollsBackWhenStampFails (FMA-002) simulates a failure AFTER the
// migration DDL but DURING the version stamp and asserts the whole transaction
// is rolled back (nothing committed) and the error propagates. This is the
// crash-between-DDL-and-stamp window: with the atomic wrap the DDL never lands
// unless the version is also stamped, so a restart never re-runs a half-applied
// migration.
func TestApplyOneRollsBackWhenStampFails(t *testing.T) {
	runner := &fakeRunner{
		scalars:            map[string]string{},
		failExecContaining: "INSERT INTO striatumd.schema_migrations",
	}
	migration := Migration{Version: 99, Label: "test rollback", SQL: "CREATE TABLE striatumd.fma002_probe (id int);"}

	err := applyOne(context.Background(), runner, migration, "test")
	if err == nil {
		t.Fatal("applyOne should have failed when the version stamp errored")
	}

	if len(runner.txs) != 1 {
		t.Fatalf("applyOne opened %d transactions, want exactly 1", len(runner.txs))
	}
	tx := runner.txs[0]
	if tx.committed {
		t.Fatal("applyOne committed a transaction whose stamp failed (FMA-002: half-applied schema)")
	}
	if !tx.rolledBack {
		t.Fatal("applyOne did not roll back the failed transaction")
	}
	// Nothing reaches the durable pool execs because the tx never committed.
	if joined := strings.Join(runner.execs, "\n"); strings.Contains(joined, "fma002_probe") {
		t.Fatalf("rolled-back migration DDL leaked to the durable pool: %q", runner.execs)
	}
}

// TestApplyOneVerifiesInProgressVersionHash (FMA-008) asserts that applyOne
// verifies the recorded sha for exactly the version it is (re)applying — the
// in-progress migration that the <= current verifyRecordedHash pass never
// reaches because it is not yet <= current. If a pre-fix crashed deploy already
// stamped a DIFFERENT sha for this version, the ON CONFLICT DO NOTHING insert
// no-ops, so the in-tx hash check is what catches the drift and aborts the apply.
func TestApplyOneVerifiesInProgressVersionHash(t *testing.T) {
	migration := Migration{Version: 99, Label: "test hash verify", SQL: "CREATE TABLE striatumd.fma008_probe (id int);"}
	runner := &fakeRunner{scalars: map[string]string{
		// A stale sha already recorded for this version by a prior partial deploy.
		"SELECT sha256 FROM striatumd.schema_migrations": "deadbeefstalehashthatdoesnotmatch",
	}}

	err := applyOne(context.Background(), runner, migration, "test")
	if err == nil {
		t.Fatal("applyOne should fail when the in-progress version's recorded hash mismatches")
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch error, got: %v", err)
	}
	if len(runner.txs) != 1 || runner.txs[0].committed {
		t.Fatal("applyOne must not commit when the in-progress hash mismatches")
	}
	if !runner.txs[0].rolledBack {
		t.Fatal("applyOne must roll back when the in-progress hash mismatches")
	}
}

// TestRunnerMigrationsHaveNoNonTransactionalDDL guards the precondition that
// makes applyOne's unconditional transaction wrap safe (FMA-002): the embedded
// runner migrations (sql/*.sql) must contain NO DDL that cannot run inside a
// transaction block. If a future runner migration introduces CREATE INDEX
// CONCURRENTLY, ALTER TYPE ... ADD VALUE (in a multi-statement body), VACUUM,
// REINDEX, or similar, this test fails loudly so applyOne is updated with an
// explicit, hash-asserted fallback for the flagged migration rather than silently
// dropping the whole runner's atomicity. (CONCURRENTLY is legitimately confined
// to the separate owner-bundle path, sql/owner/*.sql, which this guard excludes.)
func TestRunnerMigrationsHaveNoNonTransactionalDDL(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	// Patterns that PostgreSQL forbids inside an explicit transaction block.
	nonTransactional := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\s+CONCURRENTLY\b`),
		regexp.MustCompile(`(?i)\bDROP\s+INDEX\s+CONCURRENTLY\b`),
		regexp.MustCompile(`(?i)\bREINDEX\b.*\bCONCURRENTLY\b`),
		regexp.MustCompile(`(?i)\bALTER\s+TYPE\b.*\bADD\s+VALUE\b`),
		regexp.MustCompile(`(?i)\bVACUUM\b`),
		regexp.MustCompile(`(?i)\bCREATE\s+DATABASE\b`),
		regexp.MustCompile(`(?i)\bALTER\s+SYSTEM\b`),
		regexp.MustCompile(`(?i)\bCREATE\s+TABLESPACE\b`),
	}
	for _, migration := range migrations {
		for _, pattern := range nonTransactional {
			if pattern.MatchString(migration.SQL) {
				t.Fatalf("runner migration %d (%s) contains non-transactional DDL matching %q; applyOne wraps every migration in a single transaction, so this body cannot apply. Add an explicit hash-asserted fallback in applyOne for this migration before merging.", migration.Version, migration.Path, pattern.String())
			}
		}
	}
}

func TestDeriveMigrationLockKey(t *testing.T) {
	ctx := context.Background()
	runner := &fakeRunner{scalars: map[string]string{
		"current_database": "test_db",
	}}
	key, err := deriveMigrationLockKey(ctx, runner)
	if err != nil {
		t.Fatalf("deriveMigrationLockKey error: %v", err)
	}
	if key == 0 {
		t.Fatal("expected non-zero migration lock key")
	}
}
