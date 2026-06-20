package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	LatestDaemonDBVersion = 42
	MigrationLockKey      = 332933
)

//go:embed sql/*.sql
var migrationFS embed.FS

type Migration struct {
	Version int
	Label   string
	Path    string
	SQL     string
}

func Migrations() ([]Migration, error) {
	labels := map[int]string{
		1:  "baseline daemon postgres substrate",
		2:  "daemon rpc supervision and apply receipts",
		3:  "cross-repo workflows and MCP mutation scope",
		4:  "dogfood surgical recovery capability",
		5:  "repo-local workflow state substrate",
		6:  "events chain anchors + repo_event_chain_heads",
		7:  "decision propagation projections",
		8:  "lane evidence publish guard",
		9:  "blob storage references (RFC 0072)",
		10: "artifact blob reference update trigger (GH #27)",
		11: "stateful escalation inbox (RFC 0062)",
		12: "MCP activity liveness timestamps (RFC 0077)",
		13: "workflow accepted-risk records (D124)",
		14: "auto-finalize circuit breakers (TODO 56)",
		15: "conversation trajectories (RFC 0081)",
		16: "interrogation sessions (RFC 0082)",
		17: "multi-party conversation (RFC 0086)",
		18: "attempt-scoped artifacts (RFC 0095 / GH #84)",
		19: "PTY-activity + tool-call liveness timestamps (RFC 0101 Phase 1)",
		20: "per-job autonomous-recovery budgets (RFC 0101 Phase 3 Slice 2)",
		21: "needs_operator run state + run_escalated_at (RFC 0101 Phase 4)",
		22: "per-session capability-token binding (RFC 0096 V2 / GH #135)",
		23: "multi-principal trust model (RFC 0107)",
		24: "verdict provenance stamp (RFC 0118 P0-1 / GH #240)",
		25: "run completion mode (RFC 0118 P0-4 / GH #240)",
		26: "run completion record (RFC 0118 P1-5 / GH #240)",
		27: "spawn-authorization grants for daemon auto_spawn scheduler (RFC 0122 / #212)",
		28: "plain-dir job workspaces (RFC 0127 P0 / D195)",
		29: "fan-in sealed barrier freeze + staging tables (RFC 0135 P1 / #345)",
		30: "barrier-assembly journal state (RFC 0135 P2 / #346)",
		31: "barrier_status read/audit view (RFC 0135 P3 / #347)",
		32: "panel-quorum dissent ledger (RFC 0135 P4 / #339)",
		33: "reap supervisors stranded on terminal runs (#417)",
		34: "conversation post_dialog_hook declaration (RFC 0094 §1 / RFC 0095 Phase 3 / #402)",
		35: "confidence-gated pipe-lane escalation state (RFC 0131 131-C / #336)",
		37: "resolve blockers stranded open on terminal runs (#420)",
		38: "durable buffer for no-reader supervised_push packets (FMA-006 / #456)",
		39: "opt-in terminal-gap tolerance for a strict fan-in with a provably-dead required seat (RFC 0138 / #453)",
		40: "drop `state` from non-partial idx_process_supervisor_pointers_run to cut HOT-defeating index churn (RFC 0139 Direction 2 / #421)",
		41: "event_chain_segments runtime sealing ledger (RFC 0136 P1 / D242 / #387)",
		42: "daemon-owned operator attestations for the gate-enforced PINNED→VERIFIABLE trust boundary (RFC 0141 / D243 / #482)",
	}
	entries, err := migrationFS.ReadDir("sql")
	if err != nil {
		return nil, err
	}
	migrations := []Migration{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if err != nil {
			return nil, err
		}
		path := "sql/" + entry.Name()
		body, err := migrationFS.ReadFile(path)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, Migration{
			Version: version,
			Label:   labels[version],
			Path:    path,
			SQL:     string(body),
		})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

func deriveMigrationLockKey(ctx context.Context, runner Runner) (int64, error) {
	dbName, err := runner.QueryScalar(ctx, "SELECT current_database()")
	if err != nil {
		return 0, err
	}
	schemaName := "striatumd"
	sum := sha256.Sum256([]byte(dbName + ":" + schemaName))
	var val uint64
	for i := 0; i < 8; i++ {
		val = (val << 8) | uint64(sum[i])
	}
	return int64(val), nil
}

func ApplyMigrations(ctx context.Context, runner Runner, daemonVersion string) (int, error) {
	lockKey, err := deriveMigrationLockKey(ctx, runner)
	if err != nil {
		return 0, err
	}
	if err := runner.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return 0, err
	}
	unlocked := false
	defer func() {
		if !unlocked {
			_ = runner.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey)
		}
	}()
	if err := ensureMetaTable(ctx, runner); err != nil {
		return 0, err
	}
	current, err := ReadSchemaVersion(ctx, runner)
	if err != nil {
		return 0, err
	}
	if current > LatestDaemonDBVersion {
		return 0, fmt.Errorf("daemon PostgreSQL schema version %d is newer than supported %d", current, LatestDaemonDBVersion)
	}
	migrations, err := Migrations()
	if err != nil {
		return 0, err
	}
	for _, migration := range migrations {
		if migration.Version <= current {
			if err := verifyRecordedHash(ctx, runner, migration); err != nil {
				return 0, err
			}
			continue
		}
		if err := applyOne(ctx, runner, migration, daemonVersion); err != nil {
			return 0, err
		}
		current = migration.Version
	}
	if err := runner.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
		return current, err
	}
	unlocked = true
	return current, nil
}

func ReadSchemaVersion(ctx context.Context, runner Runner) (int, error) {
	value, err := runner.QueryScalar(ctx, "SELECT value FROM striatumd.schema_meta WHERE key = 'substrate_version'")
	if err != nil || value == "" {
		return 0, nil
	}
	version, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	return version, nil
}

func (m Migration) SHA256() string {
	sum := sha256.Sum256([]byte(m.SQL))
	return hex.EncodeToString(sum[:])
}

func MigrationSHASet() (map[string]string, error) {
	migrations, err := Migrations()
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, migration := range migrations {
		result[filepath.Base(migration.Path)] = migration.SHA256()
	}
	return result, nil
}

// VerifyMigrationsSHASource compares the embedded migration SHAs against the
// SQL files on disk at the supplied path. The comparison is by-filename and
// guards against drift between the Go-embedded SQL and the Python source-of-
// truth tree. Returns a non-nil error if any file is missing, differs, or the
// source tree contains newer migration files the Go binary did not embed.
func VerifyMigrationsSHASource(path string) error {
	embedded, err := MigrationSHASet()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read source migration directory: %w", err)
	}
	source := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(path, entry.Name()))
		if err != nil {
			return fmt.Errorf("read source migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(body)
		source[entry.Name()] = hex.EncodeToString(sum[:])
	}
	for name, embeddedSHA := range embedded {
		actual, ok := source[name]
		if !ok {
			return fmt.Errorf("source migration %s is missing", name)
		}
		if actual != embeddedSHA {
			return fmt.Errorf("migration %s sha mismatch: embedded=%s source=%s", name, embeddedSHA, actual)
		}
	}
	for name := range source {
		if _, ok := embedded[name]; !ok {
			return fmt.Errorf("source migration %s is newer than the embedded Go daemon migrations; rebuild go/bin/striatumd", name)
		}
	}
	return nil
}

func ensureMetaTable(ctx context.Context, runner Runner) error {
	return runner.Exec(ctx, `
CREATE SCHEMA IF NOT EXISTS striatumd;
CREATE TABLE IF NOT EXISTS striatumd.schema_meta (
  key text PRIMARY KEY,
  value text NOT NULL
);`)
}

// scalarQuerier is the read surface shared by Runner and TxRunner that
// verifyRecordedHash needs. Both PgxRunner/PgxTxRunner and the test fakes
// satisfy it, so the hash check can run either against the pool (the <= current
// pre-apply pass) or inside the apply transaction (the FMA-008 in-progress
// version check) without duplicating the comparison.
type scalarQuerier interface {
	QueryScalar(ctx context.Context, sql string, args ...any) (string, error)
}

func verifyRecordedHash(ctx context.Context, runner Runner, migration Migration) error {
	return verifyRecordedHashTx(ctx, runner, migration)
}

func verifyRecordedHashTx(ctx context.Context, q scalarQuerier, migration Migration) error {
	value, err := q.QueryScalar(ctx, "SELECT sha256 FROM striatumd.schema_migrations WHERE version = $1", migration.Version)
	if err != nil || value == "" {
		return nil
	}
	if strings.TrimSpace(value) != migration.SHA256() {
		return fmt.Errorf("daemon PostgreSQL migration %d hash mismatch", migration.Version)
	}
	return nil
}

// applyOne runs a single migration's DDL and BOTH version stamps
// (schema_migrations + schema_meta.substrate_version) inside ONE transaction so
// that a crash either applies the migration AND records its version, or applies
// nothing. This closes the FMA-002 crash window where the DDL committed in its
// own implicit transaction but the version stamps were separate autocommit
// writes: a crash in the gap left the schema DDL-applied yet version-unstamped,
// the restart re-ran the same migration, and atomicity rested entirely on an
// unenforced per-migration idempotency convention.
//
// CONSTRAINT: some PostgreSQL DDL cannot run inside a transaction block (notably
// CREATE INDEX CONCURRENTLY and certain ALTER TYPE ... ADD VALUE forms). The
// embedded runner migrations (sql/*.sql) are inventoried to contain NONE of
// these — CONCURRENTLY is confined to the separate owner-bundle apply path
// (sql/owner/*.sql via owner.go), which has its own deploy story — so this wrap
// is unconditional and guarded by TestRunnerMigrationsHaveNoNonTransactionalDDL.
// If a future runner migration ever needs non-transactional DDL, that guard
// fails loudly and applyOne must grow an explicit, hash-asserted fallback for
// the flagged migration rather than silently dropping the whole runner's
// transactional atomicity.
func applyOne(ctx context.Context, runner Runner, migration Migration, daemonVersion string) error {
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
	if err := tx.Exec(ctx, migration.SQL); err != nil {
		return err
	}
	if err := tx.Exec(
		ctx,
		`INSERT INTO striatumd.schema_migrations(version, label, sha256, daemon_version)
VALUES ($1, $2, $3, $4)
ON CONFLICT (version) DO NOTHING`,
		migration.Version,
		migration.Label,
		migration.SHA256(),
		daemonVersion,
	); err != nil {
		return err
	}
	if err := tx.Exec(
		ctx,
		`INSERT INTO striatumd.schema_meta(key, value)
VALUES ('substrate_version', $1)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		strconv.Itoa(migration.Version),
	); err != nil {
		return err
	}
	// FMA-008: the schema_migrations insert above is ON CONFLICT (version) DO
	// NOTHING. If a pre-fix crashed deploy already stamped a DIFFERENT sha for
	// this version (precisely the in-progress migration most likely to have
	// drifted), the insert no-ops and the stale sha would persist silently —
	// and the <= current verifyRecordedHash pass never reaches this version
	// because it is not yet <= current. Verify the recorded hash inside the same
	// transaction so a drifted stamp aborts the apply (rolling back the DDL)
	// instead of committing over a mismatch.
	if err := verifyRecordedHashTx(ctx, tx, migration); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}
