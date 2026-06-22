package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const EnvDaemonDBURL = "STRIATUM_DAEMON_DB_URL"

// connResetDestroyCount counts pooled connections destroyed on release because
// they carried ambiguous (non-idle) transaction state — the RFC 0110 §4.7
// bounded-discard signal. doctor reads it via ConnResetDestroyCount to flag a
// reconnect storm.
var connResetDestroyCount int64

// ConnResetDestroyCount returns the number of connections destroyed on release
// for carrying leftover transaction state (RFC 0110 §4.7 / OPS-12).
func ConnResetDestroyCount() int64 {
	return atomic.LoadInt64(&connResetDestroyCount)
}

type Config struct {
	URL        string
	Source     string
	ConfigPath string
}

func ResolveConfig(explicitURL string) Config {
	configPath := DefaultConfigPath()
	if explicitURL != "" {
		return Config{URL: explicitURL, Source: "--postgres-url", ConfigPath: configPath}
	}
	if envURL := os.Getenv(EnvDaemonDBURL); envURL != "" {
		return Config{URL: envURL, Source: EnvDaemonDBURL, ConfigPath: configPath}
	}
	if fileURL := readConfigURL(configPath); fileURL != "" {
		return Config{URL: fileURL, Source: configPath, ConfigPath: configPath}
	}
	return Config{ConfigPath: configPath}
}

// ValidateDSN parses a PostgreSQL connection URL without connecting, reporting a
// deterministic configuration error for an empty or malformed DSN. It is the
// shape check behind `striatumd -check-config` and the startup config guard; a
// reachable-but-down database is NOT a DSN error (that is a transient
// connectivity failure the daemon retries). The URL is redacted in the error.
func ValidateDSN(rawURL string) error {
	if strings.TrimSpace(rawURL) == "" {
		return fmt.Errorf("empty PostgreSQL connection URL")
	}
	if _, err := pgxpool.ParseConfig(rawURL); err != nil {
		// Do not echo rawURL: RedactURL cannot redact a URL it cannot parse, so it
		// would leak the password for exactly the malformed input this rejects.
		// pgx's own parse error already redacts the userinfo password.
		return fmt.Errorf("invalid PostgreSQL connection URL: %w", err)
	}
	return nil
}

func DefaultConfigPath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "striatum", "daemon.toml")
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "striatum", "daemon.toml")
}

func RedactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.User != nil {
		username := parsed.User.Username()
		if _, ok := parsed.User.Password(); ok {
			parsed.User = url.UserPassword(username, "<redacted>")
		}
	}
	query := parsed.Query()
	for _, key := range []string{"password", "pass", "token", "sslpassword"} {
		if _, ok := query[key]; ok {
			query.Set(key, "<redacted>")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// Row mirrors pgx.Row for callers that do not want to depend on pgx directly.
// Concrete implementations returned by Runner / TxRunner are pgx.Row values.
type Row = pgx.Row

// Runner is the database access surface used by the daemon. Calls are
// parameterized; multi-statement DDL is allowed because the runtime is
// configured for the PostgreSQL simple protocol.
type Runner interface {
	Exec(ctx context.Context, sql string, args ...any) error
	QueryRow(ctx context.Context, sql string, args ...any) Row
	QueryScalar(ctx context.Context, sql string, args ...any) (string, error)
	BeginTx(ctx context.Context) (TxRunner, error)
}

// TxRunner is the transaction-scoped sibling of Runner. Callers must call
// Commit or Rollback before the surrounding context is cancelled.
type TxRunner interface {
	Exec(ctx context.Context, sql string, args ...any) error
	QueryRow(ctx context.Context, sql string, args ...any) Row
	QueryScalar(ctx context.Context, sql string, args ...any) (string, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// boundExecer is the optional extended-protocol exec surface used by the RFC
// 0110 authority prelude (C-EXTENDED-AUTH-PRELUDE). The production transaction
// (*PgxTxRunner) implements it; canned test fakes do not, and skip the prelude
// (they have no real session to label). It is matched structurally, not added
// to TxRunner, so adding a label-bearing prelude does not force every fake to
// grow a method.
type boundExecer interface {
	ExecBound(ctx context.Context, sql string, args ...any) error
}

// Pool wraps a pgxpool.Pool with the Runner surface and a Close helper. The
// caller owns the pool lifetime and must invoke Close on shutdown.
//
// V1.7: RawPool exposes the underlying pgxpool.Pool so consumers that need
// pgx-typed access (e.g. SupervisorPointerStore for transactional reads
// with explicit row scanning) can construct against it without forcing
// the Runner abstraction to grow surface area.
type Pool struct {
	URL     string
	Runner  Runner
	RawPool *pgxpool.Pool
	Close   func()
}

// PgxRunner is a thin adapter from a pgx connection pool to the Runner
// interface used by the rest of the daemon.
type PgxRunner struct {
	Pool *pgxpool.Pool
}

func (r PgxRunner) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := r.Pool.Exec(ctx, sql, args...)
	return err
}

func (r PgxRunner) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return r.Pool.QueryRow(ctx, sql, args...)
}

// QueryRowBound executes a single row query over the extended protocol with
// bound parameters. It is used for read-side SECURITY DEFINER projections that
// must pass the daemon-authority secret without exposing it in query text.
func (r PgxRunner) QueryRowBound(ctx context.Context, sql string, args ...any) Row {
	return r.Pool.QueryRow(ctx, sql, boundExecArgs(args...)...)
}

// ExecBound is the pool-scoped sibling of PgxTxRunner.ExecBound: an
// extended-protocol exec for daemon-authorized SECURITY DEFINER calls (e.g.
// the RFC 0114 link_client_to_principal write function) whose secret argument
// must never appear in pg_stat_activity query text.
func (r PgxRunner) ExecBound(ctx context.Context, sql string, args ...any) error {
	_, err := r.Pool.Exec(ctx, sql, boundExecArgs(args...)...)
	return err
}

func (r PgxRunner) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return r.Pool.Query(ctx, sql, args...)
}

func (r PgxRunner) QueryScalar(ctx context.Context, sql string, args ...any) (string, error) {
	var value string
	err := r.Pool.QueryRow(ctx, sql, args...).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func (r PgxRunner) BeginTx(ctx context.Context) (TxRunner, error) {
	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	return &PgxTxRunner{Tx: tx}, nil
}

// PgxTxRunner is the transaction-scoped twin of PgxRunner.
type PgxTxRunner struct {
	Tx pgx.Tx
}

func (t *PgxTxRunner) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := t.Tx.Exec(ctx, sql, args...)
	return err
}

// boundExecArgs prepends the extended-protocol exec mode to a parameter list.
// Under pgx the leading args may carry a QueryExecMode that overrides the pool
// default; QueryExecModeExec uses the extended wire protocol (Parse/Bind/
// Execute), so bound values travel in Bind messages and never interpolate into
// the SQL text observable via pg_stat_activity.query. This is the sole
// transport the RFC 0110 authority/attribution prelude is permitted to use
// (C-EXTENDED-AUTH-PRELUDE); the pool default stays simple protocol so
// multi-statement migration DDL keeps working.
func boundExecArgs(args ...any) []any {
	bound := make([]any, 0, len(args)+1)
	bound = append(bound, pgx.QueryExecModeExec)
	bound = append(bound, args...)
	return bound
}

// ExecBound executes sql over the extended protocol with bound parameters,
// regardless of the pool's default exec mode. The RFC 0110 prelude uses only
// this method so the daemon-authority secret and attribution labels stay out of
// pg_stat_activity query text.
func (t *PgxTxRunner) ExecBound(ctx context.Context, sql string, args ...any) error {
	_, err := t.Tx.Exec(ctx, sql, boundExecArgs(args...)...)
	return err
}

func (t *PgxTxRunner) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return t.Tx.QueryRow(ctx, sql, args...)
}

// QueryRowBound is the transaction-scoped sibling of PgxRunner.QueryRowBound.
func (t *PgxTxRunner) QueryRowBound(ctx context.Context, sql string, args ...any) Row {
	return t.Tx.QueryRow(ctx, sql, boundExecArgs(args...)...)
}

func (t *PgxTxRunner) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.Tx.Query(ctx, sql, args...)
}

func (t *PgxTxRunner) QueryScalar(ctx context.Context, sql string, args ...any) (string, error) {
	var value string
	err := t.Tx.QueryRow(ctx, sql, args...).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func (t *PgxTxRunner) Commit(ctx context.Context) error {
	return t.Tx.Commit(ctx)
}

func (t *PgxTxRunner) Rollback(ctx context.Context) error {
	return t.Tx.Rollback(ctx)
}

// Connect opens a pgx connection pool for the daemon. The pool is configured
// with an application_name tagged with the daemon version and the PostgreSQL
// simple protocol so that multi-statement migration files run unchanged.
func Connect(ctx context.Context, postgresURL string, daemonVersion string) (*Pool, error) {
	if postgresURL == "" {
		return nil, errors.New("daemon PostgreSQL URL is not configured")
	}
	cfg, err := pgxpool.ParseConfig(postgresURL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres url: %w", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "striatumd-go/" + daemonVersion
	if _, ok := cfg.ConnConfig.RuntimeParams["statement_timeout"]; !ok {
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = "60000"
	}
	// Simple protocol keeps migration DDL with multiple statements working and
	// still binds parameters safely via client-side quoting; the daemon's hot
	// queries are low cardinality and do not need server-side prepared plans.
	// The RFC 0110 prelude overrides this per-call via ExecBound.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	// RFC 0110 §4.7 bounded-discard reset (C-ATTR-RESET-FAIL, OPS-12): the
	// authority/attribution GUCs are transaction-local, so a clean commit or
	// rollback already discards them and the connection returns to the pool
	// normally (no DISCARD on the hot path). A connection released with leftover
	// transaction state (mid-prelude cancel/timeout, handler panic, unknown
	// outcome) is ambiguous: destroy it rather than reuse it, and count the
	// destroy so doctor can surface a reconnect storm (a mass-cancel/PG-stress
	// event) as a posture signal instead of a silent churn.
	cfg.AfterRelease = func(c *pgx.Conn) bool {
		if c.IsClosed() {
			return false
		}
		if c.PgConn().TxStatus() != 'I' {
			atomic.AddInt64(&connResetDestroyCount, 1)
			return false
		}
		return true
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Pool{
		URL:     postgresURL,
		Runner:  PgxRunner{Pool: pool},
		RawPool: pool,
		Close:   pool.Close,
	}, nil
}

// ConnectAndMigrate opens the pool and applies pending migrations using the
// daemon version label for provenance.
func ConnectAndMigrate(ctx context.Context, postgresURL string, daemonVersion string) (*Pool, int, error) {
	config := ResolveConfig(postgresURL)
	if config.URL == "" {
		return nil, 0, errors.New("daemon PostgreSQL URL is not configured")
	}
	pool, err := Connect(ctx, config.URL, daemonVersion)
	if err != nil {
		return nil, 0, err
	}
	// RFC 0142 Layer 2 (owner-bundle watermark interlock): BEFORE applying any
	// runtime migration, verify the applied owner-bundle watermark satisfies the
	// frontier this binary was built against. On a shortfall this returns a typed
	// awaiting_owner_ddl error and we apply NOTHING — the database is left
	// untouched and the daemon halts cleanly (apoptosis), instead of running a
	// runtime migration that depends on the pending owner DDL and crash-looping
	// the single writer (RFC 0142 failure #2; #442 / D248). The in-sync and
	// tolerate-forward cases return nil and boot proceeds exactly as before.
	if err := CheckOwnerBundleWatermark(ctx, pool.Runner); err != nil {
		pool.Close()
		return nil, 0, err
	}
	version, err := ApplyMigrations(ctx, pool.Runner, daemonVersion)
	if err != nil {
		pool.Close()
		return nil, 0, err
	}
	return pool, version, nil
}

func readConfigURL(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "postgres_url") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				return strings.Trim(strings.TrimSpace(parts[1]), "\"'")
			}
		}
	}
	return ""
}
