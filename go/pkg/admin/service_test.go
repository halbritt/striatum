package admin

import (
	"context"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

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
	if target, ok := dest[0].(*string); ok {
		*target = r.value
	}
	return nil
}

type fakeRunner struct {
	scalars map[string]string
	execs   []string
}

// fakeTx is the transaction-scoped sibling of fakeRunner used by the admin
// service test. It records statements executed inside the transaction and
// publishes them to the parent's execs only on Commit — mirroring real
// transactional visibility so that applyOne's atomicity (FMA-002) is
// observable without a live PostgreSQL connection.
type fakeTx struct {
	parent *fakeRunner
	execs  []string
}

func (f *fakeRunner) Exec(_ context.Context, sql string, _ ...any) error {
	f.execs = append(f.execs, sql)
	return nil
}

func (f *fakeRunner) QueryRow(_ context.Context, sql string, _ ...any) db.Row {
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

func (f *fakeRunner) BeginTx(_ context.Context) (db.TxRunner, error) {
	return &fakeTx{parent: f}, nil
}

func (t *fakeTx) Exec(_ context.Context, sql string, _ ...any) error {
	t.execs = append(t.execs, sql)
	return nil
}

func (t *fakeTx) QueryRow(_ context.Context, sql string, _ ...any) db.Row {
	return t.parent.QueryRow(context.Background(), sql)
}

func (t *fakeTx) QueryScalar(_ context.Context, sql string, _ ...any) (string, error) {
	return t.parent.QueryScalar(context.Background(), sql)
}

func (t *fakeTx) Commit(_ context.Context) error {
	// Publish the transaction's statements to the parent only on commit, so
	// fakeRunner.execs reflects durably-applied SQL — matching real transactional
	// semantics and allowing the migrate test to observe substrate_version stamps.
	t.parent.execs = append(t.parent.execs, t.execs...)
	return nil
}

func (t *fakeTx) Rollback(_ context.Context) error {
	return nil
}

func TestRegisterInstallsDaemonMigrate(t *testing.T) {
	server := rpc.NewServer()
	Service{Runner: &fakeRunner{}, DaemonVersion: "test"}.Register(server)

	if server.Handlers["daemon.migrate"] == nil {
		t.Fatal("daemon.migrate was not registered")
	}
}

func TestDaemonMigrateAppliesEmbeddedPostgresMigrations(t *testing.T) {
	runner := &fakeRunner{scalars: map[string]string{}}
	result, err := Service{Runner: runner, DaemonVersion: "test-daemon"}.Migrate(
		context.Background(),
		rpc.Envelope{Params: map[string]any{}},
	)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result["status"] != "migrated" {
		t.Fatalf("status = %v", result["status"])
	}
	if result["substrate_schema"] != db.LatestDaemonDBVersion {
		t.Fatalf("substrate_schema = %v, want %d", result["substrate_schema"], db.LatestDaemonDBVersion)
	}
	if result["sqlite_dependency"] != false {
		t.Fatalf("sqlite_dependency = %v", result["sqlite_dependency"])
	}
	if result["python_dependency"] != false {
		t.Fatalf("python_dependency = %v", result["python_dependency"])
	}
	joined := strings.Join(runner.execs, "\n")
	if !strings.Contains(joined, "pg_advisory_lock") {
		t.Fatal("migration lock was not acquired")
	}
	if !strings.Contains(joined, "substrate_version") {
		t.Fatal("substrate version was not recorded")
	}
}
