package mutations

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// TestRecoveryResealRequeuesSameAttemptWhenBodyDurable is the RFC 0125 P1-2
// (#271/#273) happy path: a repo-write job whose required artifact body was made
// durable (committed to the worktree HEAD) is, after recovery.reseal, requeued
// on the SAME attempt — jobs.attempt is unchanged, no duplicate attempt rows are
// minted, the artifact row stays single-per-logical-name at that attempt, and a
// fresh session can claim the requeued (pending) work message.
func TestRecoveryResealRequeuesSameAttemptWhenBodyDurable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "reseal_durable", true)

	payload := []byte("resealed artifact body\n")
	// Make the published body durable: commit docs/out.txt into the per-job
	// worktree HEAD with the exact bytes the artifact row records.
	commitWorktreeFile(t, ids.worktreeRoot, "docs/out.txt", string(payload))
	seedPublishedArtifact(t, ctx, runner, ids, "art_reseal_durable", "out", "docs/out.txt", payload, nil)

	// Precondition snapshot: the live attempt.
	attemptBefore := intField(t, ctx, runner, ids, "attempt")

	result, err := HandleRecoveryReseal(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
	}))
	if err != nil {
		t.Fatalf("HandleRecoveryReseal (durable body): %v", err)
	}
	if result["status"] != "resealed" {
		t.Fatalf("reseal status = %#v, want resealed", result["status"])
	}
	if result["new_state"] != "queued" {
		t.Fatalf("reseal new_state = %#v, want queued", result["new_state"])
	}

	// attempt UNCHANGED (no bump, contrast run.retry_job / reopenJobForAttempt).
	attemptAfter := intField(t, ctx, runner, ids, "attempt")
	if attemptAfter != attemptBefore {
		t.Fatalf("attempt bumped %d -> %d; reseal must not bump the attempt", attemptBefore, attemptAfter)
	}
	if got := fmt.Sprint(result["attempt"]); got != fmt.Sprint(attemptBefore) {
		t.Fatalf("result attempt = %s, want unchanged %d", got, attemptBefore)
	}

	// Exactly one artifacts row per logical_name at that attempt (no duplicate
	// provenance).
	count := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.artifacts
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND logical_name = 'out' AND attempt = $4`,
		ids.repoID, ids.runID, ids.jobID, attemptBefore)
	if count != 1 {
		t.Fatalf("artifacts rows for logical_name=out attempt=%d = %d, want exactly 1", attemptBefore, count)
	}

	// The job is requeued and claimable: state=queued with a live pending work
	// message.
	jobRow, err := oneRow(ctx, runner, `
		SELECT state, current_message_id FROM striatumd.jobs
		 WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID)
	if err != nil {
		t.Fatalf("read job row: %v", err)
	}
	if jobRow["state"] != "queued" {
		t.Fatalf("job state = %#v, want queued", jobRow["state"])
	}
	pending := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.queue_messages
		 WHERE repository_id = $1 AND job_id = $2 AND kind = 'work' AND state = 'pending'`,
		ids.repoID, ids.jobID)
	if pending != 1 {
		t.Fatalf("pending work messages = %d, want exactly 1 (claimable)", pending)
	}

	// A durable recovery.resealed event was emitted on the same attempt.
	evtAttempt := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND event_type = 'recovery.resealed'`,
		ids.repoID, ids.runID, ids.jobID)
	if evtAttempt != 1 {
		t.Fatalf("recovery.resealed events = %d, want exactly 1", evtAttempt)
	}
}

// TestRecoveryResealPorterCommitsUndurableBody asserts the recovery path can
// repair the common same-attempt case: the lane wrote the artifact body into the
// worktree but never committed it. recovery.reseal must porter-commit, anchor,
// and requeue the same attempt.
func TestRecoveryResealPorterCommitsUndurableBody(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "reseal_uncommitted", true)

	payload := []byte("uncommitted artifact body\n")
	// Write the file into the worktree but do NOT commit it: recovery.reseal must
	// use the same daemon-porter path as work.complete/review.verdict.
	if err := os.MkdirAll(filepath.Join(ids.worktreeRoot, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ids.worktreeRoot, "docs", "out.txt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	seedPublishedArtifact(t, ctx, runner, ids, "art_reseal_uncommitted", "out", "docs/out.txt", payload, nil)

	if mustGitExit(t, repoRoot, "cat-file", "-e", "refs/heads/"+ids.runBranch+":docs/out.txt") == 0 {
		t.Fatalf("precondition failed: run branch %s already contains docs/out.txt", ids.runBranch)
	}

	attemptBefore := intField(t, ctx, runner, ids, "attempt")

	result, err := HandleRecoveryReseal(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
	}))
	if err != nil {
		t.Fatalf("HandleRecoveryReseal (uncommitted body): %v", err)
	}
	if result["status"] != "resealed" {
		t.Fatalf("reseal status = %#v, want resealed", result["status"])
	}

	got := gitRun(t, repoRoot, "show", "refs/heads/"+ids.runBranch+":docs/out.txt")
	if string(payload) != got {
		t.Fatalf("refs/heads/%s:docs/out.txt = %q, want %q", ids.runBranch, got, string(payload))
	}
	attemptAfter := intField(t, ctx, runner, ids, "attempt")
	if attemptAfter != attemptBefore {
		t.Fatalf("attempt bumped %d -> %d; reseal must not bump the attempt", attemptBefore, attemptAfter)
	}
	if got := jobState(t, ctx, runner, ids.repoID, ids.jobID); got != "queued" {
		t.Fatalf("job state after reseal = %q, want queued", got)
	}
}

func TestRecoveryResealPreservesOpenHumanCheckpoint(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "reseal_checkpoint", true)

	payload := []byte("checkpoint ledger body\n")
	repoPath := "docs/ledger.md"
	if err := os.MkdirAll(filepath.Join(ids.worktreeRoot, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ids.worktreeRoot, repoPath), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	seedPublishedArtifact(t, ctx, runner, ids, "art_reseal_checkpoint", "collaboration_ledger_cycle_2", repoPath, payload, nil)

	job, err := rowByID(ctx, runner, ids.repoID, "jobs", "job_id", ids.jobID, false)
	if err != nil {
		t.Fatalf("load job: %v", err)
	}
	checkpointID, err := openHumanCheckpoint(ctx, runner, ids.repoID, job, ids.sessionID, ids.leaseID, "needs revision")
	if err != nil {
		t.Fatalf("open checkpoint: %v", err)
	}

	// Simulate the bad same-attempt rerun observed in the live recovery: the
	// re-executed lane cannot publish the same logical artifact again, so it
	// blocks with an immutable_artifact_conflict while the real checkpoint is
	// still open.
	now := time.Now().UTC()
	rerunLeaseID := ids.leaseID + "_rerun"
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.leases (
		  repository_id, lease_id, run_id, resource_type, resource_id, owner_session_id,
		  state, acquired_at, expires_at
		) VALUES ($1,$2,$3,'job',$4,$5,'active',$6,$7)`,
		ids.repoID, rerunLeaseID, ids.runID, ids.jobID, ids.sessionID, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("insert rerun lease: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'blocked', current_lease_id = $1
		 WHERE repository_id = $2 AND job_id = $3`,
		rerunLeaseID, ids.repoID, ids.jobID); err != nil {
		t.Fatalf("mark job blocked: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.queue_messages
		   SET state = 'acked', current_lease_id = $1, updated_at = $2
		 WHERE repository_id = $3 AND message_id = $4`,
		rerunLeaseID, now, ids.repoID, ids.messageID); err != nil {
		t.Fatalf("mark message acked: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.blockers (
		  repository_id, blocker_id, run_id, job_id, session_id, severity,
		  blocker_kind, description, state, created_at
		) VALUES ($1,'blk_reseal_conflict',$2,$3,$4,'blocked',
		  'immutable_artifact_conflict','duplicate same-attempt publish','open',$5)`,
		ids.repoID, ids.runID, ids.jobID, ids.sessionID, now); err != nil {
		t.Fatalf("insert conflict blocker: %v", err)
	}

	attemptBefore := intField(t, ctx, runner, ids, "attempt")
	result, err := HandleRecoveryReseal(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
	}))
	if err != nil {
		t.Fatalf("HandleRecoveryReseal (checkpoint): %v", err)
	}
	if result["status"] != "resealed_checkpoint" {
		t.Fatalf("reseal status = %#v, want resealed_checkpoint", result["status"])
	}
	if result["new_state"] != "waiting_human" {
		t.Fatalf("reseal new_state = %#v, want waiting_human", result["new_state"])
	}
	if result["blocker_id"] != checkpointID {
		t.Fatalf("reseal blocker_id = %#v, want %s", result["blocker_id"], checkpointID)
	}
	if got := jobState(t, ctx, runner, ids.repoID, ids.jobID); got != "waiting_human" {
		t.Fatalf("job state after reseal = %q, want waiting_human", got)
	}
	if got := blockerState(t, ctx, runner, ids.repoID, checkpointID); got != "open" {
		t.Fatalf("checkpoint blocker state = %q, want open", got)
	}
	if got := blockerState(t, ctx, runner, ids.repoID, "blk_reseal_conflict"); got != "resolved" {
		t.Fatalf("conflict blocker state = %q, want resolved", got)
	}
	if attemptAfter := intField(t, ctx, runner, ids, "attempt"); attemptAfter != attemptBefore {
		t.Fatalf("attempt bumped %d -> %d; checkpoint reseal must not bump", attemptBefore, attemptAfter)
	}
	if active := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.leases
		 WHERE repository_id = $1 AND resource_id = $2 AND state = 'active'`,
		ids.repoID, ids.jobID); active != 0 {
		t.Fatalf("active leases after checkpoint reseal = %d, want 0", active)
	}
	message, err := oneRow(ctx, runner, `
		SELECT state, current_lease_id
		  FROM striatumd.queue_messages
		 WHERE repository_id = $1 AND message_id = $2`, ids.repoID, ids.messageID)
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	if message["state"] != "blocked" || nullable(message["current_lease_id"]) != nil {
		t.Fatalf("message after checkpoint reseal = %#v, want blocked with no lease", message)
	}
	if resealed := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND event_type = 'recovery.resealed'
		   AND payload_json->>'checkpoint_restored' = 'true'`,
		ids.repoID, ids.runID, ids.jobID); resealed != 1 {
		t.Fatalf("checkpoint recovery.resealed events = %d, want exactly 1", resealed)
	}
}

// TestRecoveryResealRefusesWhenBodyAbsent asserts the gate: when no file exists
// for the published artifact body, the porter cannot reconstruct it and
// recovery.reseal refuses rather than silently completing.
func TestRecoveryResealRefusesWhenBodyAbsent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "reseal_absent", true)

	payload := []byte("absent artifact body\n")
	seedPublishedArtifact(t, ctx, runner, ids, "art_reseal_absent", "out", "docs/out.txt", payload, nil)

	attemptBefore := intField(t, ctx, runner, ids, "attempt")

	_, err := HandleRecoveryReseal(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
	}))
	if err == nil {
		t.Fatalf("reseal succeeded with an absent body; want refusal")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok {
		t.Fatalf("reseal error = %T (%v), want *rpc.Error", err, err)
	}
	if rpcErr.Code != "invalid_transition" {
		t.Fatalf("reseal error code = %q, want invalid_transition", rpcErr.Code)
	}
	if rpcErr.Details == nil || rpcErr.Details["artifacts"] == nil {
		t.Fatalf("reseal refusal details = %#v, want an artifacts list naming the undurable bodies", rpcErr.Details)
	}

	// The attempt is untouched after a refusal.
	attemptAfter := intField(t, ctx, runner, ids, "attempt")
	if attemptAfter != attemptBefore {
		t.Fatalf("attempt changed %d -> %d on a refused reseal", attemptBefore, attemptAfter)
	}
	// No reseal event was emitted.
	evts := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND event_type = 'recovery.resealed'`,
		ids.repoID, ids.runID, ids.jobID)
	if evts != 0 {
		t.Fatalf("recovery.resealed events on a refused reseal = %d, want 0", evts)
	}
}

func intField(t *testing.T, ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs, column string) int {
	t.Helper()
	row, err := oneRow(ctx, runner, fmt.Sprintf(`
		SELECT %s AS v FROM striatumd.jobs
		 WHERE repository_id = $1 AND job_id = $2`, column), ids.repoID, ids.jobID)
	if err != nil {
		t.Fatalf("read job %s: %v", column, err)
	}
	return intValue(row["v"])
}

func scalarInt(t *testing.T, ctx context.Context, runner db.Runner, query string, args ...any) int {
	t.Helper()
	row, err := oneRow(ctx, runner, query, args...)
	if err != nil {
		t.Fatalf("scalar query: %v", err)
	}
	for _, v := range row {
		return intValue(v)
	}
	t.Fatalf("scalar query returned no columns")
	return 0
}
