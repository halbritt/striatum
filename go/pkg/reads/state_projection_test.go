package reads

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

// stateProjectionFakeRunner is a minimal read-only fake that answers only the
// two queries buildStateProjection issues: the run-state read and the per-job
// (workflow_job_id, state) read. It lets the contract test exercise the helper
// in isolation without standing up the full HandleStatus/HandleDashboard fakes.
type stateProjectionFakeRunner struct {
	runState string
	jobs     []map[string]any
}

func (stateProjectionFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("state projection must be read-only")
}

func (stateProjectionFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return dashboardAllFakeRow{}
}

func (stateProjectionFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected scalar query")
}

func (stateProjectionFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("state projection must not open a transaction")
}

func (r stateProjectionFakeRunner) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "SELECT state FROM striatumd.runs"):
		if r.runState == "" {
			return dashboardAllRowsFromMaps(nil), nil
		}
		return dashboardAllRowsFromMaps([]map[string]any{{"state": r.runState}}), nil
	case strings.Contains(sql, "SELECT workflow_job_id, state") && strings.Contains(sql, "FROM striatumd.jobs"):
		return dashboardAllRowsFromMaps(r.jobs), nil
	default:
		return nil, errors.New("unexpected query: " + sql)
	}
}

// assertStateProjectionShape verifies the RFC 0157 (D251) pinned contract:
// a top-level object {run_state, jobs:[{id,state}]} where jobs[] carries ONLY
// the id+state pair (no attempt/role_id leakage).
func assertStateProjectionShape(t *testing.T, surface string, projection any) map[string]any {
	t.Helper()
	block, ok := projection.(map[string]any)
	if !ok {
		t.Fatalf("%s: state_projection is %T, want map[string]any", surface, projection)
	}
	if _, present := block["run_state"]; !present {
		t.Fatalf("%s: state_projection missing run_state key: %#v", surface, block)
	}
	jobsAny, present := block["jobs"]
	if !present {
		t.Fatalf("%s: state_projection missing jobs key: %#v", surface, block)
	}
	jobs, ok := jobsAny.([]map[string]any)
	if !ok {
		t.Fatalf("%s: state_projection.jobs is %T, want []map[string]any", surface, jobsAny)
	}
	for i, job := range jobs {
		if _, ok := job["id"]; !ok {
			t.Fatalf("%s: state_projection.jobs[%d] missing id: %#v", surface, i, job)
		}
		if _, ok := job["state"]; !ok {
			t.Fatalf("%s: state_projection.jobs[%d] missing state: %#v", surface, i, job)
		}
		// The pinned shape is the lowest common denominator: id + state ONLY.
		for key := range job {
			if key != "id" && key != "state" {
				t.Fatalf("%s: state_projection.jobs[%d] has unexpected key %q (pinned shape is id+state only): %#v", surface, i, key, job)
			}
		}
	}
	return block
}

func TestBuildStateProjectionSingleRun(t *testing.T) {
	runner := stateProjectionFakeRunner{
		runState: "running",
		jobs: []map[string]any{
			{"workflow_job_id": "draft", "state": "completed"},
			{"workflow_job_id": "review", "state": "running"},
		},
	}
	block, err := buildStateProjection(context.Background(), runner, "repo_a", "run_a")
	if err != nil {
		t.Fatalf("buildStateProjection: %v", err)
	}
	assertStateProjectionShape(t, "buildStateProjection", block)
	if block["run_state"] != "running" {
		t.Fatalf("run_state = %#v, want running", block["run_state"])
	}
	jobs := block["jobs"].([]map[string]any)
	if len(jobs) != 2 {
		t.Fatalf("jobs len = %d, want 2: %#v", len(jobs), jobs)
	}
	if jobs[0]["id"] != "draft" || jobs[0]["state"] != "completed" {
		t.Fatalf("jobs[0] = %#v", jobs[0])
	}
	if jobs[1]["id"] != "review" || jobs[1]["state"] != "running" {
		t.Fatalf("jobs[1] = %#v", jobs[1])
	}
}

func TestBuildStateProjectionRepoWideIsNullEmpty(t *testing.T) {
	// No single run in scope (empty runID) -> the helper must not touch the DB and
	// must return run_state=null with an empty jobs list. We use a runner that
	// errors on ANY query to prove the no-run path short-circuits.
	block, err := buildStateProjection(context.Background(), stateProjectionFakeRunner{}, "repo_a", "")
	if err != nil {
		t.Fatalf("buildStateProjection (repo-wide): %v", err)
	}
	assertStateProjectionShape(t, "buildStateProjection(repo-wide)", block)
	if block["run_state"] != nil {
		t.Fatalf("repo-wide run_state = %#v, want nil", block["run_state"])
	}
	jobs := block["jobs"].([]map[string]any)
	if len(jobs) != 0 {
		t.Fatalf("repo-wide jobs len = %d, want 0", len(jobs))
	}
}

// TestStateProjectionAcrossReadSurfaces is the RFC 0157 cross-surface golden
// test: the same {run_state, jobs:[{id,state}]} block appears identically on
// run.summary, dashboard, and status, and dashboard also carries a top-level
// `state` mirroring state_projection.run_state.
func TestStateProjectionDashboardSurface(t *testing.T) {
	result, err := HandleDashboard(context.Background(), dashboardFakeRunner{}, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_a", "run_id": "run_a"},
	})
	if err != nil {
		t.Fatalf("HandleDashboard: %v", err)
	}
	block := assertStateProjectionShape(t, "dashboard", result["state_projection"])
	// RFC 0157: dashboard gains a top-level `state` mirroring run_state.
	if result["state"] != block["run_state"] {
		t.Fatalf("dashboard top-level state %#v != state_projection.run_state %#v", result["state"], block["run_state"])
	}
	if result["state"] != "running" {
		t.Fatalf("dashboard top-level state = %#v, want running", result["state"])
	}
}

func TestStateProjectionDashboardNoRunIsNullEmpty(t *testing.T) {
	result, err := HandleDashboard(context.Background(), dashboardNoRunFakeRunner{}, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_a"},
	})
	if err != nil {
		t.Fatalf("HandleDashboard (no run): %v", err)
	}
	if result["state"] != nil {
		t.Fatalf("dashboard no-run top-level state = %#v, want nil", result["state"])
	}
	block := assertStateProjectionShape(t, "dashboard(no run)", result["state_projection"])
	if block["run_state"] != nil {
		t.Fatalf("dashboard no-run state_projection.run_state = %#v, want nil", block["run_state"])
	}
}

func TestStateProjectionRunSummarySurface(t *testing.T) {
	result, err := HandleRunSummary(context.Background(), runSummaryFakeRunner{}, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_a", "run_id": "run_a"},
	})
	if err != nil {
		t.Fatalf("HandleRunSummary: %v", err)
	}
	block := assertStateProjectionShape(t, "run.summary", result["state_projection"])
	if block["run_state"] != "running" {
		t.Fatalf("run.summary state_projection.run_state = %#v, want running", block["run_state"])
	}
	jobs := block["jobs"].([]map[string]any)
	if len(jobs) != 1 || jobs[0]["id"] != "draft" || jobs[0]["state"] != "running" {
		t.Fatalf("run.summary state_projection.jobs = %#v", jobs)
	}
	// run.summary keeps its own rich .jobs[] list (with attempt/role_id) untouched.
	rich, ok := result["jobs"].([]map[string]any)
	if !ok || len(rich) != 1 {
		t.Fatalf("run.summary .jobs = %#v, want the existing rich list (must not change)", result["jobs"])
	}
	if _, ok := rich[0]["attempt"]; !ok {
		t.Fatalf("run.summary .jobs[0] lost its attempt field (additive guarantee broken): %#v", rich[0])
	}
}

func TestStateProjectionStatusSurface(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	result, err := HandleStatus(context.Background(), statusFakeRunner{repoRoot: repoRoot, runFound: true}, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_a", "run_id": "run_a"},
	})
	if err != nil {
		t.Fatalf("HandleStatus: %v", err)
	}
	block := assertStateProjectionShape(t, "status", result["state_projection"])
	if block["run_state"] != "running" {
		t.Fatalf("status state_projection.run_state = %#v, want running", block["run_state"])
	}
	jobs := block["jobs"].([]map[string]any)
	if len(jobs) != 2 {
		t.Fatalf("status state_projection.jobs len = %d, want 2: %#v", len(jobs), jobs)
	}
	// status keeps its own .jobs{} state->count map untouched (additive guarantee).
	if _, ok := result["jobs"].(map[string]int); !ok {
		t.Fatalf("status .jobs is %T, want the existing state->count map[string]int (must not change)", result["jobs"])
	}
}

// dashboardNoRunFakeRunner answers the latest-run lookup with zero rows so
// HandleDashboard takes its no-run-in-scope path.
type dashboardNoRunFakeRunner struct{}

func (dashboardNoRunFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("dashboard must be read-only")
}

func (dashboardNoRunFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return dashboardAllFakeRow{}
}

func (dashboardNoRunFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected scalar query")
}

func (dashboardNoRunFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("dashboard must not open a transaction")
}

func (dashboardNoRunFakeRunner) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "SELECT run_id FROM striatumd.runs"):
		return dashboardAllRowsFromMaps(nil), nil
	default:
		return nil, errors.New("unexpected query (no-run path should short-circuit): " + sql)
	}
}
