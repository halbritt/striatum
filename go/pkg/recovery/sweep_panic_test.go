package recovery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// sweepPanicFakeRunner is a no-PG runner for the panic-degrade tests. It answers
// the per-sweep enumeration query (repositories JOIN runs / spawn grants) with a
// fixed set of (repository_id, run_id) candidates, answers the cursor read-side
// latch query with an empty latch, and records every cursor upsert Exec so a test
// can assert which run was marked degraded vs active.
type sweepPanicFakeRunner struct {
	candidates []sweepCandidate
	cursors    []recordedCursor
}

type sweepCandidate struct {
	repositoryID string
	runID        string
}

type recordedCursor struct {
	cursorKind string
	state      string
	resultJSON map[string]any
}

func (r *sweepPanicFakeRunner) Exec(_ context.Context, sql string, args ...any) error {
	if strings.Contains(sql, "striatumd.scheduler_cursors") {
		// INSERT INTO ... scheduler_cursors VALUES ($1 repo, $2 run, kind literal,
		// ..., $3::jsonb result, $4 state). cursor_kind is a SQL literal, not a bind.
		kind := "recovery"
		if strings.Contains(sql, "'auto_spawn'") {
			kind = "auto_spawn"
		}
		result, _ := args[2].(map[string]any)
		state, _ := args[3].(string)
		r.cursors = append(r.cursors, recordedCursor{cursorKind: kind, state: state, resultJSON: result})
	}
	return nil
}

func (r *sweepPanicFakeRunner) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "claimable_job_count") {
		// Read-side latch query: return one empty latch row.
		return sweepCursorRowsFromMaps([]map[string]any{{}}), nil
	}
	// Enumeration query: return the configured candidates as (repo, run) rows.
	return newCandidateRows(r.candidates), nil
}

func (r *sweepPanicFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return nil
}

func (r *sweepPanicFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("not implemented")
}

func (r *sweepPanicFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("not implemented")
}

func (r *sweepPanicFakeRunner) degradedRuns() []string {
	out := []string{}
	for _, c := range r.cursors {
		if c.state == "sweep_degraded" || c.state == "spawn_degraded" {
			out = append(out, c.state)
		}
	}
	return out
}

func (r *sweepPanicFakeRunner) activeRuns() int {
	n := 0
	for _, c := range r.cursors {
		if c.state == "active" {
			n++
		}
	}
	return n
}

// candidateRows is a pgx.Rows that yields (repository_id, run_id) string columns.
type candidateRows struct {
	rows  []sweepCandidate
	index int
}

func newCandidateRows(rows []sweepCandidate) *candidateRows {
	return &candidateRows{rows: rows, index: -1}
}

func (r *candidateRows) Close() {}

func (r *candidateRows) Err() error { return nil }

func (r *candidateRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }

func (r *candidateRows) FieldDescriptions() []pgconn.FieldDescription {
	return []pgconn.FieldDescription{{Name: "repository_id"}, {Name: "run_id"}, {Name: "created_at"}}
}

func (r *candidateRows) Next() bool {
	r.index++
	return r.index < len(r.rows)
}

func (r *candidateRows) Scan(dest ...any) error {
	if r.index < 0 || r.index >= len(r.rows) {
		return errors.New("no current row")
	}
	cur := r.rows[r.index]
	values := []any{cur.repositoryID, cur.runID, any(nil)}
	for i := range dest {
		if i >= len(values) {
			break
		}
		switch target := dest[i].(type) {
		case *string:
			s, _ := values[i].(string)
			*target = s
		case *any:
			*target = values[i]
		default:
			return errors.New("unsupported candidate rows scan target")
		}
	}
	return nil
}

func (r *candidateRows) Values() ([]any, error) {
	if r.index < 0 || r.index >= len(r.rows) {
		return nil, errors.New("no current row")
	}
	cur := r.rows[r.index]
	return []any{cur.repositoryID, cur.runID, any(nil)}, nil
}

func (r *candidateRows) RawValues() [][]byte { return nil }

func (r *candidateRows) Conn() *pgx.Conn { return nil }

// TestActiveRunSweepPanicDegradesRunAndContinues asserts that when the per-run
// recovery unit PANICS for one run, ActiveRunSweep.SweepOnce does NOT propagate
// the panic (which would kill the single-writer daemon, FMA-001 / issue #451):
// the panicking run is recorded as a degraded cursor and the loop continues to
// the next run, which completes normally.
func TestActiveRunSweepPanicDegradesRunAndContinues(t *testing.T) {
	runner := &sweepPanicFakeRunner{candidates: []sweepCandidate{
		{repositoryID: "repo_a", runID: "run_poison"},
		{repositoryID: "repo_a", runID: "run_healthy"},
	}}

	var seen []string
	sweep := ActiveRunSweep{
		Runner: runner,
		Author: "test",
		sweepRun: func(_ context.Context, _ db.Runner, repositoryID, runID, _ string) (map[string]any, error) {
			seen = append(seen, runID)
			if runID == "run_poison" {
				panic("synthetic nil-deref classifying a malformed row")
			}
			return map[string]any{"status": "ok"}, nil
		},
	}

	// Must not panic; the per-run recover converts the panic into a degraded error.
	result, err := func() (out map[string]any, err error) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SweepOnce propagated a panic to the scheduler loop: %v", r)
			}
		}()
		return sweep.SweepOnce(context.Background())
	}()
	if err != nil {
		t.Fatalf("SweepOnce returned a top-level error; a single poison run must degrade, not fail the whole sweep: %v", err)
	}

	if len(seen) != 2 || seen[0] != "run_poison" || seen[1] != "run_healthy" {
		t.Fatalf("sweep did not continue to the next run after the panic; visited=%v", seen)
	}
	if got := runner.degradedRuns(); len(got) != 1 {
		t.Fatalf("expected exactly one degraded cursor for the poison run, got %v", got)
	}
	if runner.activeRuns() != 1 {
		t.Fatalf("expected the healthy run to record an active cursor, cursors=%v", runner.cursors)
	}

	sweeps, _ := result["sweeps"].([]map[string]any)
	if len(sweeps) != 2 {
		t.Fatalf("expected 2 sweep entries (degraded + healthy), got %#v", result["sweeps"])
	}
	if _, ok := sweeps[0]["error"]; !ok {
		t.Fatalf("poison run sweep entry should carry an error, got %#v", sweeps[0])
	}
	if _, ok := sweeps[1]["result"]; !ok {
		t.Fatalf("healthy run sweep entry should carry a result, got %#v", sweeps[1])
	}
}

// TestAutoSpawnSweepPanicDegradesRunAndContinues asserts the same panic-degrade
// guarantee for the auto_spawn scheduler loop, which previously had NO recover.
func TestAutoSpawnSweepPanicDegradesRunAndContinues(t *testing.T) {
	runner := &sweepPanicFakeRunner{candidates: []sweepCandidate{
		{repositoryID: "repo_b", runID: "run_poison"},
		{repositoryID: "repo_b", runID: "run_healthy"},
	}}

	var seen []string
	sweep := AutoSpawnSweep{
		Runner: runner,
		Author: "test",
		spawnRun: func(_ context.Context, _ db.Runner, repositoryID, runID, _ string) (map[string]any, error) {
			seen = append(seen, runID)
			if runID == "run_poison" {
				panic("synthetic panic in scheduler spawn")
			}
			return map[string]any{"spawned": 0}, nil
		},
	}

	result, err := func() (out map[string]any, err error) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("AutoSpawnSweep.SweepOnce propagated a panic to the scheduler loop: %v", r)
			}
		}()
		return sweep.SweepOnce(context.Background())
	}()
	if err != nil {
		t.Fatalf("auto_spawn SweepOnce returned a top-level error; a poison run must degrade, not fail the sweep: %v", err)
	}

	if len(seen) != 2 || seen[0] != "run_poison" || seen[1] != "run_healthy" {
		t.Fatalf("auto_spawn sweep did not continue after the panic; visited=%v", seen)
	}
	if got := runner.degradedRuns(); len(got) != 1 {
		t.Fatalf("expected exactly one spawn_degraded cursor for the poison run, got %v", got)
	}
	if runner.activeRuns() != 1 {
		t.Fatalf("expected the healthy run to record an active cursor, cursors=%v", runner.cursors)
	}

	sweeps, _ := result["sweeps"].([]map[string]any)
	if len(sweeps) != 2 {
		t.Fatalf("expected 2 sweep entries (degraded + healthy), got %#v", result["sweeps"])
	}
}

// TestRunPerRunSweepConvertsPanicToError is the focused unit check that the
// per-run wrapper returns a non-nil error (and nil result) on panic, rather than
// unwinding the stack.
func TestRunPerRunSweepConvertsPanicToError(t *testing.T) {
	result, err := runPerRunSweep(context.Background(), func(context.Context, db.Runner, string, string, string) (map[string]any, error) {
		panic("boom")
	}, nil, "repo", "run", "author")
	if err == nil {
		t.Fatalf("runPerRunSweep should convert a panic into an error")
	}
	if !strings.Contains(err.Error(), "sweep panic recovered") {
		t.Fatalf("error should name the recovered panic, got %v", err)
	}
	if result != nil {
		t.Fatalf("result should be nil on panic, got %#v", result)
	}
}
