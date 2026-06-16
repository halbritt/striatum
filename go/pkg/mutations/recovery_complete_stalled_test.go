package mutations

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// seedCompleteStalledDeadEnd drives the seeded worktree-required job into the
// #292 dead-end: a required handoff artifact is declared + published, the agent's
// lane is dead (lease expired, session closed), autonomous recovery raised a
// recovery_exhausted blocker + escalation_inbox row, and the run is
// needs_operator. The job stays `running` (recovery never moved it terminal).
func seedCompleteStalledDeadEnd(t *testing.T, ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs) {
	t.Helper()
	expectedArg, err := db.JSONBArg(runner, []any{
		map[string]any{"logical_name": "out", "kind": "handoff", "path": "docs/out.txt", "required": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET expected_artifacts_json = $1::jsonb
		 WHERE repository_id = $2 AND job_id = $3`, expectedArg, ids.repoID, ids.jobID); err != nil {
		t.Fatalf("declare required artifact: %v", err)
	}
	// Dead lane: lease expired, session closed.
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases SET state = 'expired', expires_at = $1
		 WHERE repository_id = $2 AND lease_id = $3`,
		time.Now().UTC().Add(-time.Hour), ids.repoID, ids.leaseID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.sessions SET state = 'closed', closed_at = $1
		 WHERE repository_id = $2 AND session_id = $3`,
		time.Now().UTC(), ids.repoID, ids.sessionID); err != nil {
		t.Fatalf("close session: %v", err)
	}
	// Autonomous recovery escalation: blocker + escalation_inbox (same id), run
	// flipped to needs_operator — exactly what escalateExhaustedJobs produces.
	blockerID := "blk_" + ids.jobID
	now := time.Now().UTC()
	payloadArg, err := db.JSONBArg(runner, map[string]any{
		"schema_version": "striatum.recovery_escalation.v1",
		"blocker_kind":   recoveryExhaustedBlockerKind,
		"stall_class":    stallClassAgentExitedUnsealed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.blockers (
		  repository_id, blocker_id, run_id, job_id, session_id, severity,
		  blocker_kind, description, state, created_at, payload_json
		) VALUES ($1,$2,$3,$4,NULL,'blocked',$5,'recovery exhausted','open',$6,$7::jsonb)`,
		ids.repoID, blockerID, ids.runID, ids.jobID, recoveryExhaustedBlockerKind, now, payloadArg); err != nil {
		t.Fatalf("insert blocker: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.escalation_inbox (
		  repository_id, escalation_id, run_id, job_id, session_id,
		  blocker_id, blocker_kind, severity, state, created_at, payload_json
		) VALUES ($1,$2,$3,$4,NULL,$2,$5,'blocked','pending',$6,$7::jsonb)`,
		ids.repoID, blockerID, ids.runID, ids.jobID, recoveryExhaustedBlockerKind, now, payloadArg); err != nil {
		t.Fatalf("insert escalation_inbox: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.runs SET state = 'needs_operator'
		 WHERE repository_id = $1 AND run_id = $2`, ids.repoID, ids.runID); err != nil {
		t.Fatalf("flip run needs_operator: %v", err)
	}
}

// TestRecoveryCompleteStalledFinalizesDurableJob is the GH #292 happy path: a
// recovery-exhausted job whose required artifact body is durable on the run
// branch is completed non-destructively. The job goes terminal `completed`, the
// recovery_exhausted blocker + escalation are resolved, and the run is restored
// from needs_operator (here it then completes, since this is the only job).
func TestRecoveryCompleteStalledFinalizesDurableJob(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "cs_durable", true)

	payload := []byte("durable synthesis deliverable\n")
	// Make the body durable ON THE RUN BRANCH (the #292 premise): commit it in
	// the worktree, then point refs/heads/<runBranch> at that commit so the
	// reconstructability probe finds it reachable from the run branch HEAD.
	sha := commitWorktreeFile(t, ids.worktreeRoot, "docs/out.txt", string(payload))
	gitRun(t, repoRoot, "update-ref", "refs/heads/"+ids.runBranch, sha)
	seedPublishedArtifact(t, ctx, runner, ids, "art_cs_durable", "out", "docs/out.txt", payload, nil)
	seedCompleteStalledDeadEnd(t, ctx, runner, ids)

	result, err := HandleRecoveryCompleteStalled(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
	}))
	if err != nil {
		t.Fatalf("HandleRecoveryCompleteStalled (durable): %v", err)
	}
	if result["status"] != "completed" {
		t.Fatalf("status = %#v, want completed", result["status"])
	}

	jobState := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.jobs WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID)
	if jobState != "completed" {
		t.Fatalf("job state = %q, want completed", jobState)
	}

	// The recovery_exhausted blocker + its escalation_inbox row are resolved.
	openBlockers := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.blockers
		 WHERE repository_id = $1 AND job_id = $2 AND state = 'open'`, ids.repoID, ids.jobID)
	if openBlockers != 0 {
		t.Fatalf("open blockers = %d, want 0 (recovery_exhausted resolved)", openBlockers)
	}
	pendingEscalations := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.escalation_inbox
		 WHERE repository_id = $1 AND job_id = $2 AND state <> 'resolved'`, ids.repoID, ids.jobID)
	if pendingEscalations != 0 {
		t.Fatalf("unresolved escalations = %d, want 0", pendingEscalations)
	}

	// The run was restored from needs_operator (the #292 fix) — a run.resumed
	// event proves the flip even though the run then completes.
	resumed := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND event_type = 'run.resumed'
		   AND payload_json->>'reason' = 'recovery_exhausted_moot_after_job_completed'`,
		ids.repoID, ids.runID)
	if resumed != 1 {
		t.Fatalf("run.resumed (needs_operator restore) events = %d, want 1", resumed)
	}
	runState := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.runs WHERE repository_id = $1 AND run_id = $2`, ids.repoID, ids.runID)
	if runState == "needs_operator" {
		t.Fatalf("run state = %q, want it restored away from needs_operator", runState)
	}

	// A provenance event distinct from a live work.complete was recorded.
	stalledEvents := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND event_type = 'recovery.stalled_job_completed'`, ids.repoID, ids.runID, ids.jobID)
	if stalledEvents != 1 {
		t.Fatalf("recovery.stalled_job_completed events = %d, want 1", stalledEvents)
	}
}

// TestRecoveryCompleteStalledRefusesUnreconstructableBody asserts the durability
// gate: when the run branch carries the declared path with a DIFFERENT body
// (positive corruption evidence), complete-stalled refuses and leaves the job +
// run + blocker untouched. The deliverable is not actually durable.
func TestRecoveryCompleteStalledRefusesUnreconstructableBody(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "cs_corrupt", true)

	// Artifact ROW records the sha of payloadA, but the run branch carries a
	// DIFFERENT body at the same path -> reconstruction fails (content mismatch).
	payloadA := []byte("the body the artifact row hashes\n")
	payloadB := []byte("a different body on the run branch\n")
	sha := commitWorktreeFile(t, ids.worktreeRoot, "docs/out.txt", string(payloadB))
	gitRun(t, repoRoot, "update-ref", "refs/heads/"+ids.runBranch, sha)
	seedPublishedArtifact(t, ctx, runner, ids, "art_cs_corrupt", "out", "docs/out.txt", payloadA, nil)
	seedCompleteStalledDeadEnd(t, ctx, runner, ids)

	_, err := HandleRecoveryCompleteStalled(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
	}))
	if err == nil {
		t.Fatalf("complete-stalled succeeded with an unreconstructable body; want refusal")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" {
		t.Fatalf("error = %T %v, want *rpc.Error invalid_transition", err, err)
	}
	if rpcErr.Details == nil || rpcErr.Details["unreconstructable"] == nil {
		t.Fatalf("refusal details = %#v, want an unreconstructable list", rpcErr.Details)
	}

	// Untouched: job still running, run still needs_operator, blocker still open.
	if got := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.jobs WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID); got != "running" {
		t.Fatalf("job state = %q after refusal, want running (untouched)", got)
	}
	if got := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.runs WHERE repository_id = $1 AND run_id = $2`, ids.repoID, ids.runID); got != "needs_operator" {
		t.Fatalf("run state = %q after refusal, want needs_operator (untouched)", got)
	}
	if got := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.blockers WHERE repository_id = $1 AND job_id = $2 AND state = 'open'`,
		ids.repoID, ids.jobID); got != 1 {
		t.Fatalf("open blockers = %d after refusal, want 1 (untouched)", got)
	}
}

// TestRecoveryCompleteStalledRefusesVerdictCapableJob asserts the RFC 0118
// guard: a verdict-capable job (job_type 'review', which is also how a
// phase_synthesis job is recorded) is refused unconditionally, because
// finalizing it from an artifact would bypass verdict attestation.
func TestRecoveryCompleteStalledRefusesVerdictCapableJob(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "cs_verdict", true)
	seedCompleteStalledDeadEnd(t, ctx, runner, ids)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET job_type = 'review'
		 WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID); err != nil {
		t.Fatalf("set job_type: %v", err)
	}

	_, err := HandleRecoveryCompleteStalled(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
	}))
	if err == nil {
		t.Fatalf("complete-stalled finalized a verdict-capable job; want refusal")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" {
		t.Fatalf("error = %T %v, want *rpc.Error invalid_transition", err, err)
	}
	if got := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.jobs WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID); got != "running" {
		t.Fatalf("job state = %q after verdict-capable refusal, want running", got)
	}
}

// TestRecoveryCompleteStalledRequiresExhaustedBlockerOrForce asserts the
// dead-end guard: without an open recovery_exhausted blocker the verb refuses,
// and --force overrides it (completing the durable job).
func TestRecoveryCompleteStalledRequiresExhaustedBlockerOrForce(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "cs_force", true)

	payload := []byte("durable deliverable, no blocker\n")
	sha := commitWorktreeFile(t, ids.worktreeRoot, "docs/out.txt", string(payload))
	gitRun(t, repoRoot, "update-ref", "refs/heads/"+ids.runBranch, sha)
	seedPublishedArtifact(t, ctx, runner, ids, "art_cs_force", "out", "docs/out.txt", payload, nil)
	// Declare the required artifact WITHOUT the recovery_exhausted dead-end; the
	// run stays running. (No seedCompleteStalledDeadEnd.)
	expectedArg, err := db.JSONBArg(runner, []any{
		map[string]any{"logical_name": "out", "kind": "handoff", "path": "docs/out.txt", "required": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET expected_artifacts_json = $1::jsonb
		 WHERE repository_id = $2 AND job_id = $3`, expectedArg, ids.repoID, ids.jobID); err != nil {
		t.Fatalf("declare required artifact: %v", err)
	}
	// Dead lane (the liveness guard refuses a live active lease before the
	// blocker precondition is even reached).
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases SET state = 'expired', expires_at = $1
		 WHERE repository_id = $2 AND lease_id = $3`,
		time.Now().UTC().Add(-time.Hour), ids.repoID, ids.leaseID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	if _, err := HandleRecoveryCompleteStalled(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
	})); err == nil {
		t.Fatalf("complete-stalled succeeded without a recovery_exhausted blocker and no --force; want refusal")
	} else if rpcErr, ok := err.(*rpc.Error); !ok || rpcErr.Code != "invalid_transition" {
		t.Fatalf("error = %T %v, want *rpc.Error invalid_transition", err, err)
	} else if !strings.Contains(rpcErr.Message, "recovery_exhausted") {
		t.Fatalf("refusal message = %q, want it to cite the recovery_exhausted-blocker precondition", rpcErr.Message)
	}

	// --force completes it.
	result, err := HandleRecoveryCompleteStalled(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
		"force":  true,
	}))
	if err != nil {
		t.Fatalf("complete-stalled --force: %v", err)
	}
	if result["status"] != "completed" {
		t.Fatalf("forced status = %#v, want completed", result["status"])
	}
	if got := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.jobs WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID); got != "completed" {
		t.Fatalf("job state = %q after --force, want completed", got)
	}
}

// TestRecoveryCompleteStalledRefusesLiveLease asserts the liveness guard: even
// with --force, a job that still holds a live (active, unexpired) lease is NOT
// finalized — the lane is alive, and complete-stalled only finalizes a dead lane.
func TestRecoveryCompleteStalledRefusesLiveLease(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "cs_live", true)

	payload := []byte("durable deliverable, live lane\n")
	sha := commitWorktreeFile(t, ids.worktreeRoot, "docs/out.txt", string(payload))
	gitRun(t, repoRoot, "update-ref", "refs/heads/"+ids.runBranch, sha)
	seedPublishedArtifact(t, ctx, runner, ids, "art_cs_live", "out", "docs/out.txt", payload, nil)
	// The fixture seeds an ACTIVE lease expiring +1h: the lane is alive. Do NOT
	// seed the dead-end. --force must still refuse.
	_, err := HandleRecoveryCompleteStalled(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
		"force":  true,
	}))
	if err == nil {
		t.Fatalf("complete-stalled --force finalized a live-leased job; want refusal")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" {
		t.Fatalf("error = %T %v, want *rpc.Error invalid_transition", err, err)
	}
	if !strings.Contains(rpcErr.Message, "live active lease") {
		t.Fatalf("refusal message = %q, want it to cite the live active lease", rpcErr.Message)
	}
	if got := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.jobs WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID); got != "running" {
		t.Fatalf("job state = %q after live-lease refusal, want running", got)
	}
}

// TestRecoveryCompleteStalledFinalizesRequeueRenewedDeadLease is the GH #309
// regression: a confirmed-dead lane (session state='closed', no pid) whose lease
// a recovery requeue RENEWED (so it is 'active' and not yet past its time
// deadline) must still be finalizable. Before the fix the liveness guard keyed on
// the lease TIME deadline ("lane is alive"), so the requeue-renewed lease read as
// live and the operator had to wait out a fiction (~31 min observed). The fix
// makes the guard key on SESSION liveness: a stopped/closed/dead-pid session is
// not alive regardless of how recently a requeue renewed its lease.
func TestRecoveryCompleteStalledFinalizesRequeueRenewedDeadLease(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "cs_renewed_dead", true)

	payload := []byte("durable synthesis deliverable\n")
	sha := commitWorktreeFile(t, ids.worktreeRoot, "docs/out.txt", string(payload))
	gitRun(t, repoRoot, "update-ref", "refs/heads/"+ids.runBranch, sha)
	seedPublishedArtifact(t, ctx, runner, ids, "art_cs_renewed_dead", "out", "docs/out.txt", payload, nil)
	seedCompleteStalledDeadEnd(t, ctx, runner, ids)

	// The #309 trap: a recovery requeue RENEWED the dead lane's lease, so it is
	// 'active' with a future expiry (+1h) even though the bound session is
	// confirmed dead (seedCompleteStalledDeadEnd closed the session). Without the
	// fix the time-keyed liveness guard reads this as a live lane and refuses.
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'active', expires_at = $1, released_at = NULL, release_reason = NULL
		 WHERE repository_id = $2 AND lease_id = $3`,
		time.Now().UTC().Add(time.Hour), ids.repoID, ids.leaseID); err != nil {
		t.Fatalf("renew lease via requeue: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET current_lease_id = $1
		 WHERE repository_id = $2 AND job_id = $3`, ids.leaseID, ids.repoID, ids.jobID); err != nil {
		t.Fatalf("re-pin renewed lease to job: %v", err)
	}

	result, err := HandleRecoveryCompleteStalled(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
	}))
	if err != nil {
		t.Fatalf("complete-stalled refused a confirmed-dead lane behind a requeue-renewed lease (the #309 defect): %v", err)
	}
	if result["status"] != "completed" {
		t.Fatalf("status = %#v, want completed", result["status"])
	}
	if got := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.jobs WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID); got != "completed" {
		t.Fatalf("job state = %q, want completed", got)
	}
	// The renewed lease is released by the finalize.
	if got := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.leases WHERE repository_id = $1 AND lease_id = $2`, ids.repoID, ids.leaseID); got != "released" {
		t.Fatalf("lease state = %q, want released after finalize", got)
	}
}

// TestRecoveryCompleteStalledStillRefusesLiveActiveLeaseAndSession is the #309
// SAFETY guard: the live-lane invariant (D200 — "finalizes a dead lane only")
// must survive the fix. When the bound session is STILL active AND the lease is
// active+unexpired, complete-stalled must still refuse, because the lane really is
// alive. (The fix only relaxes the guard when the session is confirmed dead.)
func TestRecoveryCompleteStalledStillRefusesLiveActiveLeaseAndSession(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "cs_live_session", true)

	payload := []byte("durable deliverable, live session\n")
	sha := commitWorktreeFile(t, ids.worktreeRoot, "docs/out.txt", string(payload))
	gitRun(t, repoRoot, "update-ref", "refs/heads/"+ids.runBranch, sha)
	seedPublishedArtifact(t, ctx, runner, ids, "art_cs_live_session", "out", "docs/out.txt", payload, nil)
	// The fixture seeds an ACTIVE session + ACTIVE lease (+1h). Do NOT close the
	// session: the lane is genuinely alive. --force must still refuse.
	_, err := HandleRecoveryCompleteStalled(ctx, runner, intgEnv(ids.repoID, map[string]any{
		"run_id": ids.runID,
		"job_id": ids.jobID,
		"force":  true,
	}))
	if err == nil {
		t.Fatalf("complete-stalled --force finalized a live-session+live-lease job; want refusal")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" {
		t.Fatalf("error = %T %v, want *rpc.Error invalid_transition", err, err)
	}
	if !strings.Contains(rpcErr.Message, "live") {
		t.Fatalf("refusal message = %q, want it to cite the live lane", rpcErr.Message)
	}
	if got := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.jobs WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID); got != "running" {
		t.Fatalf("job state = %q after live-session refusal, want running", got)
	}
}

func scalarStr(t *testing.T, ctx context.Context, runner db.Runner, query string, args ...any) string {
	t.Helper()
	row, err := oneRow(ctx, runner, query, args...)
	if err != nil {
		t.Fatalf("scalar query: %v", err)
	}
	for _, v := range row {
		return fmt.Sprint(v)
	}
	t.Fatalf("scalar query returned no columns")
	return ""
}
