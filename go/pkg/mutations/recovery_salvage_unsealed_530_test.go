package mutations

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// seedWrittenButUnpublishedWorktreeArtifact530 drives the seeded per-job-worktree
// fixture into the #530 shape: the lane WROTE and committed its required
// deliverable into its isolated per-job worktree, then exited unsealed BEFORE
// artifact.publish (e.g. a transient Anthropic-API outage during end-of-session
// wind-down). So: NO artifact row, NO file in the main repo_root, the body present
// only in the per-job worktree (committed, so the worktree HEAD anchors it). The
// owning session is confirmed-dead-shaped (stopped) and the lease expired, so the
// liveness guard treats the lane as dead and complete-stalled may finalize it. It
// returns the committed body so the test can assert the salvaged artifact.
func seedWrittenButUnpublishedWorktreeArtifact530(t *testing.T, ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs) (relPath, body string) {
	t.Helper()
	relPath = "docs/out.txt"

	// Declare ONE required handoff artifact, attempt 1, max_attempts 1 (a final job).
	expectedArg, err := db.JSONBArg(runner, []any{
		map[string]any{"logical_name": "out", "kind": "handoff", "path": relPath, "required": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET expected_artifacts_json = $1::jsonb, max_attempts = 1
		 WHERE repository_id = $2 AND job_id = $3`, expectedArg, ids.repoID, ids.jobID); err != nil {
		t.Fatalf("declare required artifact: %v", err)
	}

	// The body must carry the byline the auto-publish byline check expects for this
	// session/job. Derive it the same way the salvage path does, then embed it in a
	// title block so markdownTitleBlockAuthorLines finds it.
	job, err := rowByID(ctx, runner, ids.repoID, "jobs", "job_id", ids.jobID, true)
	if err != nil {
		t.Fatalf("read job: %v", err)
	}
	byline, err := expectedAuthorLine(ctx, runner, ids.repoID, job, ids.sessionID)
	if err != nil {
		t.Fatalf("derive expected byline: %v", err)
	}
	body = "# Salvaged deliverable\n\n" + byline + "\n\nThe lane wrote and committed this, then died before work.complete.\n"

	// Commit the body INTO the per-job worktree (not the main repo_root), then point
	// the worktree's anchor pin AND the worktree HEAD at that commit so the
	// reconstructability probe finds the body reachable. Do NOT write a copy at the
	// main repo_root or publish an artifact row — that is the #530 premise.
	sha := commitWorktreeFile(t, ids.worktreeRoot, relPath, body)
	// Anchor the committed deliverable on a refs/striatum attempt pin so the
	// reconstructability gate (which probes the attempt pins first) resolves it.
	gitRun(t, ids.worktreeRoot, "update-ref", attemptPinRef(ids.runID, ids.jobID, 1), sha)

	// The owning session is confirmed-dead-shaped (stopped) and the lease expired.
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		UPDATE striatumd.sessions SET state = 'stopped'
		 WHERE repository_id = $1 AND session_id = $2`, ids.repoID, ids.sessionID); err != nil {
		t.Fatalf("stop session: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases SET state = 'expired', expires_at = $1, released_at = $1, release_reason = 'expired'
		 WHERE repository_id = $2 AND lease_id = $3`,
		now.Add(-time.Minute), ids.repoID, ids.leaseID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	// Raise the recovery_exhausted dead-end blocker complete-stalled keys on.
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.blockers (
		  repository_id, blocker_id, run_id, job_id, severity, blocker_kind,
		  description, state, created_at
		) VALUES ($1,$2,$3,$4,'blocked',$5,'agent exited unsealed; recovery exhausted','open',$6)`,
		ids.repoID, "blk_530_"+ids.jobID, ids.runID, ids.jobID, recoveryExhaustedBlockerKind, now); err != nil {
		t.Fatalf("insert recovery_exhausted blocker: %v", err)
	}
	// Escalate the run (the wedge complete-stalled clears).
	if err := runner.Exec(ctx, `
		UPDATE striatumd.runs SET state = 'needs_operator' WHERE repository_id = $1 AND run_id = $2`,
		ids.repoID, ids.runID); err != nil {
		t.Fatalf("set run needs_operator: %v", err)
	}
	return relPath, body
}

// TestCompleteStalledSalvagesWrittenButUnpublishedWorktreeArtifact reproduces
// #530: a per-job lane wrote and committed its required deliverable into its
// isolated worktree, then exited unsealed before artifact.publish. There is NO
// artifact row, so the original complete-stalled refused at the
// verifyRequiredArtifacts gate and the expensive work was discarded. With the
// #530 salvage, complete-stalled scans the per-job worktree, publishes the missing
// artifact row from the committed body, and finalizes the job from durable
// provenance.
func TestCompleteStalledSalvagesWrittenButUnpublishedWorktreeArtifact(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "salvage_530", true)
	relPath, body := seedWrittenButUnpublishedWorktreeArtifact530(t, ctx, runner, ids)

	// Precondition: no artifact row exists yet (publish never happened).
	preRows := artifactRowsForLogical(t, ctx, runner, ids.repoID, ids.jobID, "out")
	if len(preRows) != 0 {
		t.Fatalf("precondition: expected 0 artifact rows before salvage, got %d", len(preRows))
	}

	result, err := HandleRecoveryCompleteStalled(ctx, runner, completeStalledEnvelope530(ids))
	if err != nil {
		t.Fatalf("complete-stalled: %v", err)
	}

	if result["status"] != "completed" {
		t.Fatalf("complete-stalled status = %v, want completed; %#v", result["status"], result)
	}
	if result["salvaged_artifact_count"] == nil {
		t.Fatalf("expected salvaged_artifact_count in result (the row was salvaged from the worktree); %#v", result)
	}
	// The job is completed and the run is no longer wedged.
	if got := jobState(t, ctx, runner, ids.repoID, ids.jobID); got != "completed" {
		t.Fatalf("job state = %q, want completed", got)
	}
	runState := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.runs WHERE repository_id = $1 AND run_id = $2`, ids.repoID, ids.runID)
	if runState == "needs_operator" {
		t.Fatalf("run state = needs_operator, want it finalized away from the wedge")
	}
	// The salvaged artifact row exists with the committed body's content hash.
	postRows := artifactRowsForLogical(t, ctx, runner, ids.repoID, ids.jobID, "out")
	if len(postRows) != 1 {
		t.Fatalf("expected exactly 1 salvaged artifact row, got %d", len(postRows))
	}
	wantSha := sha256Hex([]byte(body))
	if got := scalarStr(t, ctx, runner, `
		SELECT content_sha256 FROM striatumd.artifacts
		 WHERE repository_id = $1 AND job_id = $2 AND logical_name = 'out'`, ids.repoID, ids.jobID); got != wantSha {
		t.Fatalf("salvaged artifact content_sha256 = %q, want %q (the committed worktree body)", got, wantSha)
	}
	_ = relPath
}

// TestCompleteStalledStillRefusesWhenNoArtifactAnywhere is the #530 safety guard:
// when the deliverable is NOT on disk anywhere (main checkout or worktree) and no
// artifact row exists, complete-stalled must still REFUSE (there is nothing to
// salvage or finalize) rather than complete an empty job.
func TestCompleteStalledStillRefusesWhenNoArtifactAnywhere(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "salvage_530_missing", true)
	// Same #530 shape but DO NOT write/commit any deliverable.
	seedWrittenButUnpublishedWorktreeArtifact530NoFile(t, ctx, runner, ids)

	_, err := HandleRecoveryCompleteStalled(ctx, runner, completeStalledEnvelope530(ids))
	if err == nil {
		t.Fatalf("complete-stalled completed a job with no artifact anywhere; it must refuse")
	}
	if got := jobState(t, ctx, runner, ids.repoID, ids.jobID); got == "completed" {
		t.Fatalf("job state = completed, want NOT completed (nothing to salvage)")
	}
}

// seedWrittenButUnpublishedWorktreeArtifact530NoFile sets up the #530 shape but
// writes nothing to disk — the negative control.
func seedWrittenButUnpublishedWorktreeArtifact530NoFile(t *testing.T, ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs) {
	t.Helper()
	expectedArg, err := db.JSONBArg(runner, []any{
		map[string]any{"logical_name": "out", "kind": "handoff", "path": "docs/out.txt", "required": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET expected_artifacts_json = $1::jsonb, max_attempts = 1
		 WHERE repository_id = $2 AND job_id = $3`, expectedArg, ids.repoID, ids.jobID); err != nil {
		t.Fatalf("declare required artifact: %v", err)
	}
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		UPDATE striatumd.sessions SET state = 'stopped'
		 WHERE repository_id = $1 AND session_id = $2`, ids.repoID, ids.sessionID); err != nil {
		t.Fatalf("stop session: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases SET state = 'expired', expires_at = $1, released_at = $1, release_reason = 'expired'
		 WHERE repository_id = $2 AND lease_id = $3`,
		now.Add(-time.Minute), ids.repoID, ids.leaseID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.blockers (
		  repository_id, blocker_id, run_id, job_id, severity, blocker_kind,
		  description, state, created_at
		) VALUES ($1,$2,$3,$4,'blocked',$5,'agent exited unsealed; recovery exhausted','open',$6)`,
		ids.repoID, "blk_530nf_"+ids.jobID, ids.runID, ids.jobID, recoveryExhaustedBlockerKind, now); err != nil {
		t.Fatalf("insert recovery_exhausted blocker: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.runs SET state = 'needs_operator' WHERE repository_id = $1 AND run_id = $2`,
		ids.repoID, ids.runID); err != nil {
		t.Fatalf("set run needs_operator: %v", err)
	}
}

func completeStalledEnvelope530(ids worktreeRequiredFixtureIDs) rpc.Envelope {
	return rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		Params: map[string]any{
			"repository_id": ids.repoID,
			"run_id":        ids.runID,
			"job_id":        ids.jobID,
		},
	}
}
