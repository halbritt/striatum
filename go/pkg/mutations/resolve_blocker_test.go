package mutations

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// seedRetryCompletionJob seeds a repo+run with one running build job (holding an
// active lease) plus a downstream blocked review job so the run does not
// auto-complete when the build finishes. It returns runID, jobID, sessionID,
// leaseID, messageID.
func seedRetryCompletionJob(t *testing.T, ctx context.Context, runner db.Runner, repoID string) (string, string, string, string, string) {
	t.Helper()
	runID := "run_" + repoID
	builder := "sess_builder_" + repoID
	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{
		"workflow_id": "wf",
		"roles":       map[string]any{"implementer": map[string]any{}, "reviewer": map[string]any{}},
		"lanes":       map[string]any{"claude": map[string]any{"display_model": "Claude"}},
	})
	intgSeedSession(t, ctx, runner, repoID, runID, builder, "implementer", "claude", nil, "active")
	intgAttest(t, ctx, runner, repoID, runID, builder, "claude")

	now := time.Now().UTC()
	jobID := "job_retry"
	leaseID := "lease_retry"
	messageID := "msg_retry"
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, title, job_type, role_id,
		  state, idempotency_key, created_at, current_lease_id, current_message_id, attempt
		) VALUES ($1,$2,$3,'build','Build','build','implementer','running','idem_retry',$4,$5,$6,2)`,
		repoID, jobID, runID, now, leaseID, messageID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	// downstream review job keeps the run from auto-completing on build finish.
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, title, job_type, role_id,
		  state, idempotency_key, created_at
		) VALUES ($1,'job_retry_review',$2,'review','Review','review','reviewer','blocked','idem_retry_review',$3)`,
		repoID, runID, now); err != nil {
		t.Fatalf("insert review job: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.queue_messages (
		  repository_id, message_id, run_id, job_id, kind, state, priority,
		  target_role_id, payload_json, claim_count, max_claims, created_at, updated_at,
		  current_lease_id
		) VALUES ($1,$2,$3,$4,'work','claimed',100,'implementer','{}'::jsonb,2,5,$5,$5,$6)`,
		repoID, messageID, runID, jobID, now, leaseID); err != nil {
		t.Fatalf("insert queue message: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.leases (
		  repository_id, lease_id, run_id, resource_type, resource_id, owner_session_id,
		  state, acquired_at, expires_at
		) VALUES ($1,$2,$3,'job',$4,$5,'active',$6,$7)`,
		repoID, leaseID, runID, jobID, builder, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("insert lease: %v", err)
	}
	return runID, jobID, builder, leaseID, messageID
}

// TestRetryCompletionResolvesPriorAttemptBlocker is the #304 regression: a
// severity='blocked' (non-escalation) work.block blocker raised on an EARLIER
// attempt must be resolved when the job later completes via the recovery/retry
// completion path. Before the fix, only the normal work.complete path resolved
// open blockers; the recovery completion helpers (completeRecoveredJob and its
// siblings) emitted job.completed but never resolved the dangling blocker, so it
// stayed open forever and kept inflating the open_blockers frontier.
func TestRetryCompletionResolvesPriorAttemptBlocker(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_retry_blocker"
	runID, jobID, builder, leaseID, _ := seedRetryCompletionJob(t, ctx, runner, repoID)

	// Attempt 1 raised a blocked-severity write_scope blocker (the issue's
	// dirty_tree.scope_violation / control_plane.write_scope shape). It is still
	// open while attempt 2 runs.
	seedOpenBlocker(t, ctx, runner, repoID, runID, jobID, builder, "blk_prior_attempt", "blocked", "control_plane.write_scope")

	// The job completes on attempt 2 via the recovery completion path
	// (recovery.resume --complete -> completeRecoveredJob), NOT the normal
	// work.complete path.
	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		return completeRecoveredJob(ctx, tx, repoID, jobID, builder, leaseID, "recovered completion")
	}); err != nil {
		t.Fatalf("completeRecoveredJob: %v", err)
	}

	// The prior-attempt blocker is resolved by the recovery completion path.
	if got := blockerState(t, ctx, runner, repoID, "blk_prior_attempt"); got != "resolved" {
		t.Fatalf("prior-attempt blocker should be resolved when the job completes via retry, got %q", got)
	}

	// A blocker.resolved_on_completion event was emitted for it.
	ev, err := oneRow(ctx, runner, `
		SELECT count(*) AS n
		  FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2
		   AND event_type = 'blocker.resolved_on_completion'
		   AND payload_json->>'blocker_id' = 'blk_prior_attempt'`, repoID, runID)
	if err != nil {
		t.Fatalf("count resolution events: %v", err)
	}
	if intValue(ev["n"]) != 1 {
		t.Fatalf("want exactly one blocker.resolved_on_completion for blk_prior_attempt, got %v", ev["n"])
	}
}

// TestResolveBlockerVerbClosesDanglingBlocker is the #304 operator verb: a
// dangling blocked-severity, non-escalation blocker (e.g. one that predates the
// completion-path fix) can be closed by `recovery resolve-blocker <id>`, which
// flips it to resolved and emits a blocker.resolved event so the open_blockers
// frontier reconciles.
func TestResolveBlockerVerbClosesDanglingBlocker(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_resolve_blocker_verb"
	runID, jobID, builder, _, _ := seedRetryCompletionJob(t, ctx, runner, repoID)

	// Mark the build job completed to mirror the issue: the gate is long gone but
	// the blocker dangles open.
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET state = 'completed', current_lease_id = NULL
		 WHERE repository_id = $1 AND job_id = $2`, repoID, jobID); err != nil {
		t.Fatalf("complete job: %v", err)
	}
	seedOpenBlocker(t, ctx, runner, repoID, runID, jobID, builder, "blk_dangling", "blocked", "write_scope.parent_dirty_unrelated")

	res, err := HandleRecoveryResolveBlocker(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id": "blk_dangling",
		"note":       "stale gate, job long completed",
	}))
	if err != nil {
		t.Fatalf("resolve-blocker: %v", err)
	}
	if got := fmt.Sprint(res["status"]); got != "resolved" {
		t.Fatalf("status = %q, want resolved", got)
	}

	if got := blockerState(t, ctx, runner, repoID, "blk_dangling"); got != "resolved" {
		t.Fatalf("blocker state = %q, want resolved", got)
	}

	ev, err := oneRow(ctx, runner, `
		SELECT count(*) AS n
		  FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2
		   AND event_type = 'blocker.resolved'
		   AND payload_json->>'blocker_id' = 'blk_dangling'
		   AND payload_json->>'reason' = 'operator_resolve_blocker'`, repoID, runID)
	if err != nil {
		t.Fatalf("count resolution events: %v", err)
	}
	if intValue(ev["n"]) != 1 {
		t.Fatalf("want exactly one operator blocker.resolved for blk_dangling, got %v", ev["n"])
	}

	// Resolving again is refused (no longer open).
	if _, err := HandleRecoveryResolveBlocker(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id": "blk_dangling",
	})); err == nil {
		t.Fatalf("resolving an already-resolved blocker should be refused")
	}
}

// TestResolveBlockerVerbRefusesEscalationAndCheckpoint guards the safety
// boundary: the operator verb must NOT discharge a human-adjudication gate. A
// human_checkpoint blocker and an escalation-class blocker are both refused and
// stay open (operators use checkpoint resolve / escalation resolve instead).
func TestResolveBlockerVerbRefusesEscalationAndCheckpoint(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_resolve_blocker_refuse"
	runID, jobID, builder, _, _ := seedRetryCompletionJob(t, ctx, runner, repoID)

	// A human_checkpoint blocker. The blockers table stores it state='open'
	// (the JOB, not the blocker, goes to waiting_human); the human_checkpoint
	// severity is what marks it as a human-adjudication gate.
	seedOpenBlocker(t, ctx, runner, repoID, runID, jobID, builder, "blk_checkpoint", "human_checkpoint", "revision_routing")
	// An escalation-class blocker (recovery_exhausted).
	seedOpenBlocker(t, ctx, runner, repoID, runID, jobID, builder, "blk_escalation", "blocked", "recovery_exhausted")

	if _, err := HandleRecoveryResolveBlocker(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id": "blk_checkpoint",
	})); err == nil {
		t.Fatalf("resolve-blocker must refuse a human_checkpoint blocker")
	}
	if got := blockerState(t, ctx, runner, repoID, "blk_checkpoint"); got != "open" {
		t.Fatalf("checkpoint blocker must stay open, got %q", got)
	}

	if _, err := HandleRecoveryResolveBlocker(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id": "blk_escalation",
	})); err == nil {
		t.Fatalf("resolve-blocker must refuse an escalation-class blocker")
	}
	if got := blockerState(t, ctx, runner, repoID, "blk_escalation"); got != "open" {
		t.Fatalf("escalation blocker must stay open, got %q", got)
	}

	// Unknown blocker id is not_found.
	if _, err := HandleRecoveryResolveBlocker(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id": "blk_does_not_exist",
	})); err == nil {
		t.Fatalf("resolve-blocker must refuse an unknown blocker id")
	}
}
