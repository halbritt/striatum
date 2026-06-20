package mutations

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// seedPendingSiblingJob inserts a minimal non-terminal (queued) job into the same run as the
// fixture so maybeCompleteRun finds non-terminal work and returns early — used
// to isolate completion tests to the per-job path without exercising full run
// finalization (which the bare pgtest runner cannot support).
func seedPendingSiblingJob(t *testing.T, ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs, suffix string) {
	t.Helper()
	laneArg, err := db.JSONBArg(runner, map[string]any{"lane_id": "codex"})
	if err != nil {
		t.Fatal(err)
	}
	emptyArg, err := db.JSONBArg(runner, []any{})
	if err != nil {
		t.Fatal(err)
	}
	scopeArg, err := db.JSONBArg(runner, map[string]any{"mode": "records_only"})
	if err != nil {
		t.Fatal(err)
	}
	capArg, err := db.JSONBArg(runner, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  lane_selector_json, title, job_type, idempotency_key, expected_artifacts_json,
		  capability_requirements_json, write_scope_json, created_at
		) VALUES ($1,$2,$3,'sibling',1,'queued','author',
		  $4::jsonb,'Sibling','build',$5,$6::jsonb,$7::jsonb,$8::jsonb,$9)`,
		ids.repoID, "job_"+suffix, ids.runID, laneArg, "idem_"+suffix, emptyArg, capArg, scopeArg, now); err != nil {
		t.Fatalf("insert sibling job: %v", err)
	}
}

// TestAutoFinalizeRefusesNonDurableOptionalArtifact is the FMA-005 (#455)
// regression: completeAutoFinalizedJob must enforce the SAME per-job durability
// floor as its recovery twin completeRecoveredJob. Previously the auto-finalize
// path called only verifyRequiredArtifacts (a required-artifact row-presence
// check), so it could seal a job `completed` while a NON-required published
// repo-write artifact body was non-durable outside the per-job worktree — a
// floor completeRecoveredJob (which also calls ensurePerJobPublishedArtifactsDurable)
// would have refused. Here the job declares NO required artifacts (so
// verifyRequiredArtifacts passes) yet has a published artifact whose body is
// written into the worktree but never committed, so the durability probe must
// refuse and the job must stay `running`.
func TestAutoFinalizeRefusesNonDurableOptionalArtifact(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "af_durability_optional", true)

	// Publish an artifact whose body lives in the worktree but is NOT committed,
	// so it cannot be reconstructed from the worktree HEAD. The fixture job
	// declares no required artifacts (expected_artifacts_json = []), so this is
	// a purely OPTIONAL published artifact — verifyRequiredArtifacts is happy
	// with it, but the per-job durability floor must not be.
	payload := []byte("published optional body but not committed\n")
	if err := os.MkdirAll(filepath.Join(ids.worktreeRoot, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ids.worktreeRoot, "docs", "out.txt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	seedPublishedArtifact(t, ctx, runner, ids, "art_af_optional_undurable", "out", "docs/out.txt", payload, nil)

	_, err := completeAutoFinalizedJob(ctx, runner, ids.repoID, ids.jobID, ids.sessionID, ids.leaseID, ids.messageID)
	if err == nil {
		t.Fatalf("auto-finalize sealed a job with a non-durable optional artifact; want a per-job durability refusal (FMA-005)")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok {
		t.Fatalf("auto-finalize error = %T (%v), want *rpc.Error", err, err)
	}
	if rpcErr.Code != "invalid_transition" {
		t.Fatalf("auto-finalize error code = %q, want invalid_transition", rpcErr.Code)
	}
	if rpcErr.Details == nil || rpcErr.Details["artifacts"] == nil {
		t.Fatalf("durability refusal details = %#v, want an artifacts list naming the undurable body", rpcErr.Details)
	}

	// The refusal must leave the job exactly where it was: still `running`, not
	// sealed `completed`.
	jobRow, err := oneRow(ctx, runner, `
		SELECT state FROM striatumd.jobs
		 WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID)
	if err != nil {
		t.Fatalf("read job row: %v", err)
	}
	if jobRow["state"] != "running" {
		t.Fatalf("job state = %#v after a refused auto-finalize, want running (must not seal completed)", jobRow["state"])
	}
}

// TestAutoFinalizeCompletesWhenOptionalArtifactDurable is the happy-path twin:
// once the published body IS committed to the worktree HEAD, the durability
// floor passes and completeAutoFinalizedJob seals the job `completed` — proving
// the new gate refuses only genuinely non-durable bodies, never a durable one.
func TestAutoFinalizeCompletesWhenOptionalArtifactDurable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "af_durability_durable", true)

	payload := []byte("published optional body, committed\n")
	// Commit the published body into the worktree HEAD with the exact bytes the
	// artifact row records, so the durability probe is satisfied.
	commitWorktreeFile(t, ids.worktreeRoot, "docs/out.txt", string(payload))
	seedPublishedArtifact(t, ctx, runner, ids, "art_af_optional_durable", "out", "docs/out.txt", payload, nil)

	// Seed a sibling non-terminal (queued) job in the same run so maybeCompleteRun finds the
	// run still has non-terminal work and returns early — this isolates the test
	// to the durability gate + job completion (the FMA-005 surface) without
	// exercising full run finalization, which the bare pgtest runner does not
	// support (it lacks the supervisor terminal-cleanup interface).
	seedPendingSiblingJob(t, ctx, runner, ids, "af_durability_durable_sibling")

	result, err := completeAutoFinalizedJob(ctx, runner, ids.repoID, ids.jobID, ids.sessionID, ids.leaseID, ids.messageID)
	if err != nil {
		t.Fatalf("auto-finalize refused a durable optional artifact: %v", err)
	}
	if result["status"] != "completed" {
		t.Fatalf("auto-finalize status = %#v, want completed", result["status"])
	}

	jobRow, err := oneRow(ctx, runner, `
		SELECT state FROM striatumd.jobs
		 WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID)
	if err != nil {
		t.Fatalf("read job row: %v", err)
	}
	if jobRow["state"] != "completed" {
		t.Fatalf("job state = %#v, want completed", jobRow["state"])
	}
}
