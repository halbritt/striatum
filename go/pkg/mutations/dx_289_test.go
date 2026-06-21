package mutations

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
)

// #289 unit: deadAgentExitedUnsealed separates "engaged the work protocol +
// produced output, never sealed" from a hard crash or a sealed session.
func TestDeadAgentExitedUnsealedPredicate(t *testing.T) {
	now := time.Now().UTC()
	tp := func(d time.Duration) *time.Time { v := now.Add(d); return &v }

	cases := []struct {
		name string
		act  sessionliveness.Activity
		want bool
	}{
		{
			name: "tool call + pty, no work.complete -> unsealed",
			act:  sessionliveness.Activity{LastToolCallStartedAt: tp(-time.Minute), LastPTYActivityAt: tp(-time.Second)},
			want: true,
		},
		{
			name: "finished tool call + pty, no work.complete -> unsealed",
			act:  sessionliveness.Activity{LastToolCallFinishedAt: tp(-time.Minute), LastPTYActivityAt: tp(-time.Second)},
			want: true,
		},
		{
			name: "did seal (work.complete set) -> not unsealed",
			act:  sessionliveness.Activity{LastWorkCompleteAt: tp(-time.Minute), LastToolCallFinishedAt: tp(-2 * time.Minute), LastPTYActivityAt: tp(-time.Minute)},
			want: false,
		},
		{
			name: "pty only, no tool call -> not unsealed (no protocol engagement)",
			act:  sessionliveness.Activity{LastPTYActivityAt: tp(-time.Second)},
			want: false,
		},
		{
			name: "tool call only, no pty -> not unsealed (no output)",
			act:  sessionliveness.Activity{LastToolCallStartedAt: tp(-time.Minute)},
			want: false,
		},
		{
			name: "nothing -> not unsealed (hard early crash)",
			act:  sessionliveness.Activity{},
			want: false,
		},
	}
	for _, tc := range cases {
		if got := deadAgentExitedUnsealed(tc.act); got != tc.want {
			t.Errorf("%s: deadAgentExitedUnsealed = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// #289 unit: the recovery policy parses max_unsealed_requeues and defaults it.
func TestRecoveryPolicyUnsealedBudget(t *testing.T) {
	def := recoveryPolicyFromWorkflow(map[string]any{})
	if def.maxUnsealedRequeues != defaultMaxUnsealedRequeues {
		t.Fatalf("default maxUnsealedRequeues = %d, want %d", def.maxUnsealedRequeues, defaultMaxUnsealedRequeues)
	}
	if defaultMaxUnsealedRequeues >= defaultMaxRequeues {
		t.Fatalf("unsealed default (%d) must be smaller than the hard-crash requeue default (%d)", defaultMaxUnsealedRequeues, defaultMaxRequeues)
	}
	p := recoveryPolicyFromWorkflow(map[string]any{
		"recovery_policy": map[string]any{"max_unsealed_requeues": float64(0)},
	})
	if p.maxUnsealedRequeues != 0 {
		t.Fatalf("override maxUnsealedRequeues = %d, want 0", p.maxUnsealedRequeues)
	}
	neg := recoveryPolicyFromWorkflow(map[string]any{
		"recovery_policy": map[string]any{"max_unsealed_requeues": float64(-3)},
	})
	if neg.maxUnsealedRequeues != 0 {
		t.Fatalf("negative override clamps to 0, got %d", neg.maxUnsealedRequeues)
	}
}

// #478 / D249 unit: the agent_exited_unsealed requeue budget is
// lane-kind-differentiated. A READ-ONLY reviewer lane (repoWrite == false) gets a
// budget STRICTLY GREATER than a STATEFUL repo-write lane (repoWrite == true), so
// a single transient reviewer unsealed exit (the common cause is transient
// Anthropic-API unavailability during end-of-session wind-down) is a clean
// fresh-session retry rather than an immediate whole-run escalation. The global
// default (the stateful bound) is unchanged, so the pinned
// defaultMaxUnsealedRequeues < defaultMaxRequeues invariant — asserted in
// TestRecoveryPolicyUnsealedBudget above — still holds.
func TestRecoveryPolicyReviewerUnsealedBudgetExceedsStateful(t *testing.T) {
	def := recoveryPolicyFromWorkflow(map[string]any{})

	statefulBound := def.unsealedRequeueBudget(true)
	reviewerBound := def.unsealedRequeueBudget(false)

	// The stateful selector must still equal the unchanged tight default.
	if statefulBound != defaultMaxUnsealedRequeues {
		t.Fatalf("stateful (repo-write) unsealed bound = %d, want the unchanged tight default %d",
			statefulBound, defaultMaxUnsealedRequeues)
	}
	// The reviewer selector must be STRICTLY LARGER than the stateful bound (#478).
	if reviewerBound <= statefulBound {
		t.Fatalf("reviewer (read-only) unsealed bound (%d) must exceed the stateful bound (%d) so a transient reviewer unsealed exit is a clean retry, not an immediate escalation",
			reviewerBound, statefulBound)
	}
	// The reviewer bound must never exceed the hard-crash requeue budget: a hard
	// crash is never out-budgeted by a reviewer unsealed exit.
	if reviewerBound > def.maxRequeues {
		t.Fatalf("reviewer unsealed bound (%d) must not exceed max_requeues (%d)", reviewerBound, def.maxRequeues)
	}

	// Operator override: a workflow can raise the reviewer bound up to max_requeues.
	over := recoveryPolicyFromWorkflow(map[string]any{
		"recovery_policy": map[string]any{
			"max_requeues":                   float64(4),
			"max_reviewer_unsealed_requeues": float64(4),
		},
	})
	if got := over.unsealedRequeueBudget(false); got != 4 {
		t.Fatalf("reviewer override bound = %d, want 4", got)
	}
	if got := over.unsealedRequeueBudget(true); got != defaultMaxUnsealedRequeues {
		t.Fatalf("stateful bound under reviewer override = %d, want the unchanged %d", got, defaultMaxUnsealedRequeues)
	}

	// Clamp: a reviewer override below the stateful bound is raised to the stateful
	// bound (never tighter than a stateful lane), and one above max_requeues is
	// capped at max_requeues.
	lowReq := recoveryPolicyFromWorkflow(map[string]any{
		"recovery_policy": map[string]any{
			"max_requeues":                   float64(1),
			"max_reviewer_unsealed_requeues": float64(5),
		},
	})
	if got := lowReq.unsealedRequeueBudget(false); got != 1 {
		t.Fatalf("reviewer bound capped at max_requeues = %d, want 1", got)
	}
}

// #289 unit: the escalation remediation is class-specific — the unsealed class
// points the operator at the worktree (the deliverable may already be there).
func TestSuggestedOperatorActionsUnsealed(t *testing.T) {
	unsealed := fmt.Sprint(suggestedOperatorActions(stallClassAgentExitedUnsealed))
	if !strings.Contains(unsealed, "complete-but-unsealed") && !strings.Contains(unsealed, "before work.complete") {
		t.Fatalf("unsealed remediation should mention the unsealed deliverable; got %s", unsealed)
	}
	hard := fmt.Sprint(suggestedOperatorActions(stallClassAgentPIDDead))
	if !strings.Contains(hard, "write_scope") {
		t.Fatalf("hard-crash remediation should be the generic set; got %s", hard)
	}
	if unsealed == hard {
		t.Fatalf("unsealed and hard-crash remediations must differ")
	}
}

// jobLastStallClass reads the recorded stall class for a job's recovery row.
func jobLastStallClass(t *testing.T, ctx context.Context, runner any, repoID, jobID string) string {
	t.Helper()
	row, err := oneRow(ctx, runner, `
		SELECT last_stall_class FROM striatumd.job_recovery_state
		 WHERE repository_id = $1 AND job_id = $2`, repoID, jobID)
	if err != nil {
		if isNoRows(err) {
			return ""
		}
		t.Fatalf("read last_stall_class: %v", err)
	}
	return fmt.Sprint(nullable(row["last_stall_class"]))
}

// makeConfirmedDeadActiveSessionWithOutput turns the stalled-active fixture into a
// CASE-3 confirmed-dead lane that LOOKS alive by protocol (so CASE 1/2 do not
// fire), with the unsealed-output activity signature: a finished tool call + PTY
// output, but no work.complete. It seeds a supervisor pointer and stubs the
// liveness probe to report the agent process dead.
func makeConfirmedDeadActiveSessionWithOutput(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, sessionID string, withOutput bool) {
	t.Helper()
	now := time.Now().UTC()
	// Fresh protocol/heartbeat signals so the session is neither dead nor stalled
	// (forces the default branch where supervisedAgentConfirmedDead is the signal).
	var tool, pty any
	if withOutput {
		tool = now
		pty = now
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.sessions
		   SET registered_at = $3, last_mcp_request_at = $3,
		       last_session_heartbeat_at = $3, last_work_heartbeat_at = $3,
		       last_tool_call_started_at = $4, last_tool_call_finished_at = $4,
		       last_pty_activity_at = $5, last_work_complete_at = NULL
		 WHERE repository_id = $1 AND session_id = $2`,
		repoID, sessionID, now, tool, pty); err != nil {
		t.Fatalf("set confirmed-dead-active activity: %v", err)
	}
	supID := "sup_" + sessionID
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.process_supervisor_pointers (
		  repository_id, supervisor_id, daemon_supervisor_id, run_id, session_id,
		  pid, pid_start_time, state, updated_at, metadata_json
		) VALUES ($1,$2,$3,$4,$5,4242,'','attached',$6,'{}'::jsonb)`,
		repoID, supID, "dsup_"+sessionID, runID, sessionID, now); err != nil {
		t.Fatalf("seed supervisor pointer: %v", err)
	}
}

func stubDeadProbe(t *testing.T) {
	t.Helper()
	restore := probeLaneLiveness
	probeLaneLiveness = func(pctx context.Context, metadata map[string]any, pid int, expectedStart string) gosupervisor.LaneLiveness {
		return gosupervisor.LaneLiveness{Backed: "plain_pty", Alive: false, Class: "pid_gone", ObservedPID: pid}
	}
	t.Cleanup(func() { probeLaneLiveness = restore })
}

// #289 integration: a confirmed-dead agent that produced output but never sealed
// is classified agent_exited_unsealed (distinct from agent_pid_dead), requeued on
// the first sweep.
func TestSweepClassifiesUnsealedExitDistinctly(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_sweep_unsealed_class"
	runID, jobID, _, _, sessionID := seedStalledSessionActiveLane(t, ctx, runner, repoID)
	makeConfirmedDeadActiveSessionWithOutput(t, ctx, runner, repoID, runID, sessionID, true)
	stubDeadProbe(t)

	result, err := SweepRun(ctx, runner, repoID, runID, "")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	summary := recoveryActionsFromSweep(t, result)
	if intValue(summary["acted_count"]) != 1 {
		t.Fatalf("acted_count = %v, want 1 (unsealed dead agent requeued once); %#v", summary["acted_count"], summary)
	}
	if got := jobLastStallClass(t, ctx, runner, repoID, jobID); got != stallClassAgentExitedUnsealed {
		t.Fatalf("last_stall_class = %q, want %q", got, stallClassAgentExitedUnsealed)
	}
	requeue, _, escalation := jobRecoveryCounts(t, ctx, runner, repoID, jobID)
	if requeue != 1 {
		t.Fatalf("requeue_count = %d, want 1", requeue)
	}
	if escalation {
		t.Fatalf("escalation_pending = true, want false on the first unsealed requeue")
	}
}

// #289 integration: the unsealed class escalates after a SMALLER budget than a
// hard crash. At requeue_count=1 the unsealed class (limit 1) escalates, while a
// hard agent_pid_dead (limit 2) would still requeue — proving the budget
// divergence — and the escalation payload carries the inspect-the-worktree
// remediation.
func TestSweepUnsealedExitEscalatesOnSmallerBudget(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner

	// Unsealed: at count 1 (== default maxUnsealedRequeues) the sweep escalates.
	repoU := "repo_sweep_unsealed_budget"
	runU, jobU, _, _, sessU := seedStalledSessionActiveLane(t, ctx, runner, repoU)
	makeConfirmedDeadActiveSessionWithOutput(t, ctx, runner, repoU, runU, sessU, true)
	stubDeadProbe(t)
	preseedRequeueBudget(t, ctx, runner, repoU, runU, jobU, 1)

	resU, err := SweepRun(ctx, runner, repoU, runU, "")
	if err != nil {
		t.Fatalf("unsealed sweep: %v", err)
	}
	sumU := recoveryActionsFromSweep(t, resU)
	if intValue(sumU["escalation_pending_count"]) != 1 {
		t.Fatalf("unsealed escalation_pending_count = %v, want 1 (limit 1 reached); %#v", sumU["escalation_pending_count"], sumU)
	}
	if got := jobState(t, ctx, runner, repoU, jobU); got != "running" {
		t.Fatalf("unsealed job state = %q, want running (escalated, not requeued)", got)
	}

	// Hard crash: same count 1, but limit 2 -> still requeues (NOT escalated).
	repoH := "repo_sweep_hardcrash_budget"
	runH, jobH, _, _, sessH := seedStalledSessionActiveLane(t, ctx, runner, repoH)
	makeConfirmedDeadActiveSessionWithOutput(t, ctx, runner, repoH, runH, sessH, false)
	preseedRequeueBudget(t, ctx, runner, repoH, runH, jobH, 1)

	resH, err := SweepRun(ctx, runner, repoH, runH, "")
	if err != nil {
		t.Fatalf("hard-crash sweep: %v", err)
	}
	sumH := recoveryActionsFromSweep(t, resH)
	if intValue(sumH["acted_count"]) != 1 {
		t.Fatalf("hard-crash acted_count = %v, want 1 (limit 2 not yet reached at count 1); %#v", sumH["acted_count"], sumH)
	}
	if got := jobLastStallClass(t, ctx, runner, repoH, jobH); got != stallClassAgentPIDDead {
		t.Fatalf("hard-crash last_stall_class = %q, want %q", got, stallClassAgentPIDDead)
	}

	// The unsealed escalation payload carries the distinct remediation.
	row, err := oneRow(ctx, runner, `
		SELECT payload_json::text AS payload FROM striatumd.escalation_inbox
		 WHERE repository_id = $1 AND run_id = $2`, repoU, runU)
	if err != nil {
		t.Fatalf("read escalation payload: %v", err)
	}
	payload := fmt.Sprint(row["payload"])
	if !strings.Contains(payload, stallClassAgentExitedUnsealed) {
		t.Fatalf("escalation payload missing the unsealed stall class: %s", payload)
	}
	if !strings.Contains(payload, "complete-but-unsealed") {
		t.Fatalf("escalation payload missing inspect-the-worktree remediation: %s", payload)
	}
}

// #289 integration (finding #1 lock): a confirmed-dead agent whose activity has
// AGED past the liveness deadlines — so Classify would call it ProtocolStalled —
// must still be handled as a dead lane (requeue_same_attempt + unsealed class),
// NOT mis-routed to the CASE 2 stalled-but-alive transfer. A dead agent's
// timestamps freeze at death, so a delayed sweep would otherwise transfer it on
// the wrong (larger) budget with no inspect-the-worktree remediation.
func TestSweepConfirmedDeadStalledRoutesToUnsealedNotTransfer(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_sweep_dead_stalled_unsealed"
	runID, jobID, _, _, sessionID := seedStalledSessionActiveLane(t, ctx, runner, repoID)

	// AGED output signature: a tool call + PTY output, but well past every liveness
	// deadline (so protocol classifies stalled), and no work.complete.
	aged := time.Now().UTC().Add(-30 * time.Minute)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.sessions
		   SET last_tool_call_started_at = $3, last_tool_call_finished_at = $3,
		       last_pty_activity_at = $3, last_work_complete_at = NULL
		 WHERE repository_id = $1 AND session_id = $2`, repoID, sessionID, aged); err != nil {
		t.Fatalf("set aged unsealed activity: %v", err)
	}
	supID := "sup_" + sessionID
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.process_supervisor_pointers (
		  repository_id, supervisor_id, daemon_supervisor_id, run_id, session_id,
		  pid, pid_start_time, state, updated_at, metadata_json
		) VALUES ($1,$2,$3,$4,$5,4242,'','attached',$6,'{}'::jsonb)`,
		repoID, supID, "dsup_"+sessionID, runID, sessionID, time.Now().UTC()); err != nil {
		t.Fatalf("seed supervisor pointer: %v", err)
	}
	stubDeadProbe(t)

	result, err := SweepRun(ctx, runner, repoID, runID, "")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	summary := recoveryActionsFromSweep(t, result)
	acts, _ := summary["actions"].([]map[string]any)
	if len(acts) != 1 || acts[0]["action"] != "requeue_same_attempt" {
		t.Fatalf("expected one requeue_same_attempt (dead lane), got %#v", summary["actions"])
	}
	if got := jobLastStallClass(t, ctx, runner, repoID, jobID); got != stallClassAgentExitedUnsealed {
		t.Fatalf("last_stall_class = %q, want %q (must not be a generic stalled transfer)", got, stallClassAgentExitedUnsealed)
	}
	requeue, transfer, _ := jobRecoveryCounts(t, ctx, runner, repoID, jobID)
	if requeue != 1 || transfer != 0 {
		t.Fatalf("budgets requeue=%d transfer=%d, want requeue=1 transfer=0 (dead-lane path, not transfer)", requeue, transfer)
	}
}

// makeJobReadOnlyReviewerLane converts the seeded repo-write fixture job into a
// READ-ONLY reviewer lane (no repo_write) — the lane kind #478 / D249 grants the
// larger unsealed-requeue budget. write_scope is cleared of repo_write so
// isRepoWrite(row) is false for it.
func makeJobReadOnlyReviewerLane(t *testing.T, ctx context.Context, runner db.Runner, repoID, jobID string) {
	t.Helper()
	ws, err := db.JSONBArg(runner, map[string]any{"mode": "read_only", "repo_write": false})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET write_scope_json = $3::jsonb
		 WHERE repository_id = $1 AND job_id = $2`, repoID, jobID, ws); err != nil {
		t.Fatalf("set read-only reviewer write_scope: %v", err)
	}
}

// #478 / D249 integration: a READ-ONLY reviewer lane that exits unsealed at
// requeue_count=1 must STILL REQUEUE (its budget is the larger reviewer bound,
// limit 2) rather than escalate the whole run — whereas the byte-identical
// STATEFUL repo-write fixture escalates at the same count (limit 1, proven by
// TestSweepUnsealedExitEscalatesOnSmallerBudget). This is the #478 fix: one
// transient reviewer unsealed exit no longer takes down a multi-stage run.
func TestSweepReviewerUnsealedExitRequeuesNotEscalates(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner

	repoR := "repo_sweep_reviewer_unsealed_budget"
	runR, jobR, _, _, sessR := seedStalledSessionActiveLane(t, ctx, runner, repoR)
	makeJobReadOnlyReviewerLane(t, ctx, runner, repoR, jobR)
	makeConfirmedDeadActiveSessionWithOutput(t, ctx, runner, repoR, runR, sessR, true)
	stubDeadProbe(t)
	// Same starting count (1) at which the stateful repo-write lane escalates.
	preseedRequeueBudget(t, ctx, runner, repoR, runR, jobR, 1)

	res, err := SweepRun(ctx, runner, repoR, runR, "")
	if err != nil {
		t.Fatalf("reviewer sweep: %v", err)
	}
	sum := recoveryActionsFromSweep(t, res)
	// It REQUEUED (acted), not escalated.
	if intValue(sum["acted_count"]) != 1 {
		t.Fatalf("reviewer acted_count = %v, want 1 (requeued on the larger reviewer budget); %#v", sum["acted_count"], sum)
	}
	if intValue(sum["escalation_pending_count"]) != 0 {
		t.Fatalf("reviewer escalation_pending_count = %v, want 0 (reviewer budget not yet exhausted at count 1); %#v", sum["escalation_pending_count"], sum)
	}
	acts, _ := sum["actions"].([]map[string]any)
	if len(acts) != 1 || acts[0]["action"] != "requeue_same_attempt" || acts[0]["acted"] != true {
		t.Fatalf("expected one requeue_same_attempt acted=true; got %#v", sum["actions"])
	}
	// The action reports the larger reviewer limit (2), not the stateful limit (1).
	if got := intValue(acts[0]["limit"]); got != defaultMaxReviewerUnsealedRequeues {
		t.Fatalf("reviewer requeue action limit = %d, want the reviewer bound %d", got, defaultMaxReviewerUnsealedRequeues)
	}
	if got := jobLastStallClass(t, ctx, runner, repoR, jobR); got != stallClassAgentExitedUnsealed {
		t.Fatalf("reviewer last_stall_class = %q, want %q", got, stallClassAgentExitedUnsealed)
	}

	// The reviewer lane DOES eventually escalate (never un-escalatable): a FRESH
	// reviewer fixture preseeded AT the reviewer bound escalates on its first sweep
	// — proving the larger budget is a delay, not an exemption.
	repoB := "repo_sweep_reviewer_unsealed_at_bound"
	runB, jobB, _, _, sessB := seedStalledSessionActiveLane(t, ctx, runner, repoB)
	makeJobReadOnlyReviewerLane(t, ctx, runner, repoB, jobB)
	makeConfirmedDeadActiveSessionWithOutput(t, ctx, runner, repoB, runB, sessB, true)
	stubDeadProbe(t)
	preseedRequeueBudget(t, ctx, runner, repoB, runB, jobB, defaultMaxReviewerUnsealedRequeues)

	resB, err := SweepRun(ctx, runner, repoB, runB, "")
	if err != nil {
		t.Fatalf("reviewer at-bound sweep: %v", err)
	}
	sumB := recoveryActionsFromSweep(t, resB)
	if intValue(sumB["escalation_pending_count"]) != 1 {
		t.Fatalf("reviewer escalation_pending_count at the reviewer bound = %v, want 1 (must still escalate eventually); %#v", sumB["escalation_pending_count"], sumB)
	}
	if got := jobState(t, ctx, runner, repoB, jobB); got != "running" {
		t.Fatalf("reviewer at-bound job state = %q, want running (escalated, not requeued)", got)
	}
}

func preseedRequeueBudget(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, jobID string, count int) {
	t.Helper()
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.job_recovery_state (
		  repository_id, run_id, job_id, requeue_count, last_recovery_action,
		  last_recovery_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,'requeue_same_attempt',$5,$5,$5)`,
		repoID, runID, jobID, count, now); err != nil {
		t.Fatalf("preseed requeue budget: %v", err)
	}
}
