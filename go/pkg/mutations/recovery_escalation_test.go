package mutations

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/reads"
)

// runStateOf reads a run's current state.
func runStateOf(t *testing.T, ctx context.Context, runner any, repoID, runID string) string {
	t.Helper()
	row, err := oneRow(ctx, runner, `SELECT state FROM striatumd.runs WHERE repository_id = $1 AND run_id = $2`, repoID, runID)
	if err != nil {
		t.Fatalf("read run state for %s: %v", runID, err)
	}
	return fmt.Sprint(row["state"])
}

// recoveryExhaustedEscalation reads the single pending recovery_exhausted
// escalation_inbox row for a run (and its blocker), returning the escalation_id
// and the structured payload. Fails if there is not exactly one.
func recoveryExhaustedEscalation(t *testing.T, ctx context.Context, runner any, repoID, runID string) (escalationID string, payload map[string]any) {
	t.Helper()
	rows, err := queryRows(ctx, runner, `
		SELECT ei.escalation_id, ei.blocker_id, ei.state AS ei_state, ei.severity,
		       ei.payload_json AS ei_payload, b.state AS b_state, b.blocker_kind,
		       b.description, b.payload_json AS b_payload
		  FROM striatumd.escalation_inbox ei
		  JOIN striatumd.blockers b
		    ON b.repository_id = ei.repository_id AND b.blocker_id = ei.blocker_id
		 WHERE ei.repository_id = $1 AND ei.run_id = $2
		   AND ei.blocker_kind = 'recovery_exhausted'`, repoID, runID)
	if err != nil {
		t.Fatalf("read recovery_exhausted escalation: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("recovery_exhausted escalation row count = %d, want 1", len(rows))
	}
	row := rows[0]
	if got := fmt.Sprint(row["ei_state"]); got != "pending" {
		t.Fatalf("escalation_inbox state = %q, want pending", got)
	}
	if got := fmt.Sprint(row["b_state"]); got != "open" {
		t.Fatalf("blocker state = %q, want open", got)
	}
	if got := fmt.Sprint(row["blocker_kind"]); got != "recovery_exhausted" {
		t.Fatalf("blocker_kind = %q, want recovery_exhausted", got)
	}
	return fmt.Sprint(row["escalation_id"]), asMap(row["ei_payload"])
}

// preseedExhaustedBudget pre-seeds the per-job recovery budget at the default
// max_requeues (2) so the next sweep is the over-budget attempt that flags
// escalation_pending. Mirrors TestSweepBudgetExhaustionSetsEscalationPending.
func preseedExhaustedBudget(t *testing.T, ctx context.Context, runner any, repoID, runID, jobID string) {
	t.Helper()
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		t.Fatalf("runner does not support exec")
	}
	now := time.Now().UTC()
	if err := exec.Exec(ctx, `
		INSERT INTO striatumd.job_recovery_state (
		  repository_id, run_id, job_id, requeue_count, last_recovery_action,
		  last_recovery_at, created_at, updated_at
		) VALUES ($1,$2,$3,2,'requeue_same_attempt',$4,$4,$4)`,
		repoID, runID, jobID, now); err != nil {
		t.Fatalf("preseed exhausted budget: %v", err)
	}
}

// TestSweepEscalatesBudgetExhaustedJobToNeedsOperator: a dead/stalled job driven
// past its budget (escalation_pending=true) is, on the next HandleRecoveryAuto,
// turned into a recovery_exhausted blocker + a pending escalation_inbox row with
// the structured payload, has run_escalated_at stamped, and the run is flipped
// to needs_operator. (RFC 0101 Phase 4 core path.)
func TestSweepEscalatesBudgetExhaustedJobToNeedsOperator(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_escalate_needs_operator"
	runID, jobID, _, _, _ := seedDeadLaneRepoWriteJob(t, ctx, runner, repoID)
	preseedExhaustedBudget(t, ctx, runner, repoID, runID, jobID)

	// Sanity: run starts running.
	if got := runStateOf(t, ctx, runner, repoID, runID); got != "running" {
		t.Fatalf("pre-sweep run state = %q, want running", got)
	}

	result, err := SweepRun(ctx, runner, repoID, runID, "")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// Surface assertion: the sweep result reports the raised escalation.
	escSummary, ok := result["escalations"].(map[string]any)
	if !ok {
		t.Fatalf("escalations summary missing or wrong type: %#v", result["escalations"])
	}
	if intValue(escSummary["raised_count"]) != 1 {
		t.Fatalf("raised_count = %v, want 1; %#v", escSummary["raised_count"], escSummary)
	}

	// Run flipped to needs_operator.
	if got := runStateOf(t, ctx, runner, repoID, runID); got != "needs_operator" {
		t.Fatalf("post-sweep run state = %q, want needs_operator", got)
	}

	// A recovery_exhausted blocker + pending escalation_inbox row with the
	// structured payload exists.
	_, payload := recoveryExhaustedEscalation(t, ctx, runner, repoID, runID)
	if got := fmt.Sprint(payload["schema_version"]); got != "striatum.recovery_escalation.v1" {
		t.Fatalf("payload schema_version = %q, want striatum.recovery_escalation.v1", got)
	}
	if payload["is_escalation"] != true {
		t.Fatalf("payload is_escalation = %v, want true", payload["is_escalation"])
	}
	if got := fmt.Sprint(payload["job_id"]); got != jobID {
		t.Fatalf("payload job_id = %q, want %s", got, jobID)
	}
	if got := fmt.Sprint(payload["stuck_job"]); got == "" || got == "<nil>" {
		t.Fatalf("payload stuck_job missing: %q", got)
	}
	actions, ok := payload["suggested_operator_actions"].([]any)
	if !ok || len(actions) != 3 {
		t.Fatalf("suggested_operator_actions = %#v, want 3 entries", payload["suggested_operator_actions"])
	}

	// run_escalated_at stamped on the budget row (idempotency guard).
	row, err := oneRow(ctx, runner, `
		SELECT run_escalated_at IS NOT NULL AS set FROM striatumd.job_recovery_state
		 WHERE repository_id = $1 AND job_id = $2`, repoID, jobID)
	if err != nil {
		t.Fatalf("read run_escalated_at: %v", err)
	}
	if row["set"] != true {
		t.Fatalf("run_escalated_at not set after escalation")
	}
}

// TestEscalationNamesOffendingJobAndLane is the #311 carve-out: the
// recovery_exhausted escalation must NAME the single offending job + lane, not
// just emit the bare "recovery_exhausted" reason. seedDeadLaneRepoWriteJob pins
// lane_selector_json={"lane_id":"claude"} on the implement job, so the lane is
// resolvable. Asserts the lane lands in (a) the escalation blocker/inbox
// payload_json, (b) the blocker description, and (c) the run.needs_operator
// event's structured stuck_jobs array (with the stable reason code preserved).
func TestEscalationNamesOffendingJobAndLane(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_escalate_names_lane"
	runID, jobID, _, _, _ := seedDeadLaneRepoWriteJob(t, ctx, runner, repoID)
	preseedExhaustedBudget(t, ctx, runner, repoID, runID, jobID)

	if _, err := SweepRun(ctx, runner, repoID, runID, ""); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := runStateOf(t, ctx, runner, repoID, runID); got != "needs_operator" {
		t.Fatalf("post-sweep run state = %q, want needs_operator", got)
	}

	const wantLane = "claude" // seedDeadLaneRepoWriteJob pins lane_id=claude.

	// (a) The escalation payload names the lane.
	_, payload := recoveryExhaustedEscalation(t, ctx, runner, repoID, runID)
	if got := fmt.Sprint(payload["lane"]); got != wantLane {
		t.Fatalf("escalation payload lane = %q, want %q", got, wantLane)
	}

	// (b) The blocker description mentions the lane.
	descRow, err := oneRow(ctx, runner, `
		SELECT description FROM striatumd.blockers
		 WHERE repository_id = $1 AND run_id = $2 AND blocker_kind = 'recovery_exhausted'`, repoID, runID)
	if err != nil {
		t.Fatalf("read blocker description: %v", err)
	}
	desc := fmt.Sprint(descRow["description"])
	if !strings.Contains(desc, "lane="+wantLane) {
		t.Fatalf("blocker description %q does not mention lane=%s", desc, wantLane)
	}

	// (c) The run.needs_operator event keeps the stable reason AND carries a
	// stuck_jobs entry naming the offending workflow_job_id + lane.
	ev, err := oneRow(ctx, runner, `
		SELECT payload_json FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND event_type = 'run.needs_operator'
		 ORDER BY event_id DESC LIMIT 1`, repoID, runID)
	if err != nil {
		t.Fatalf("read run.needs_operator event: %v", err)
	}
	evPayload := asMap(ev["payload_json"])
	if got := fmt.Sprint(evPayload["reason"]); got != "recovery_exhausted" {
		t.Fatalf("event reason = %q, want recovery_exhausted (stable code must be preserved)", got)
	}
	stuckJobsRaw, ok := evPayload["stuck_jobs"].([]any)
	if !ok || len(stuckJobsRaw) != 1 {
		t.Fatalf("stuck_jobs = %#v, want exactly 1 entry", evPayload["stuck_jobs"])
	}
	stuck := asMap(stuckJobsRaw[0])
	if got := fmt.Sprint(stuck["workflow_job_id"]); got != "implement" {
		t.Fatalf("stuck_jobs[0].workflow_job_id = %q, want implement", got)
	}
	if got := fmt.Sprint(stuck["lane"]); got != wantLane {
		t.Fatalf("stuck_jobs[0].lane = %q, want %q", got, wantLane)
	}
}

// TestNeedsOperatorRunIsNotSwept: once a run is needs_operator the recovery
// sweep's active-run query (recovery/sweep.go: state IN ('running','paused'))
// excludes it, and re-running HandleRecoveryAuto raises no duplicate escalation
// (the run_escalated_at guard). Asserts both: (1) the active-run query does not
// return the needs_operator run, (2) a direct re-sweep is idempotent.
func TestNeedsOperatorRunIsNotSwept(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_needs_operator_not_swept"
	runID, jobID, _, _, _ := seedDeadLaneRepoWriteJob(t, ctx, runner, repoID)
	preseedExhaustedBudget(t, ctx, runner, repoID, runID, jobID)

	if _, err := SweepRun(ctx, runner, repoID, runID, ""); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if got := runStateOf(t, ctx, runner, repoID, runID); got != "needs_operator" {
		t.Fatalf("run state = %q, want needs_operator", got)
	}
	escIDFirst, _ := recoveryExhaustedEscalation(t, ctx, runner, repoID, runID)

	// (1) The active-run sweep query (mirrors recovery/sweep.go) must NOT return a
	// needs_operator run. seedDeadLaneRepoWriteJob seeds an 'active' repository so
	// this only filters on run state.
	activeRows, err := queryRows(ctx, runner, `
		SELECT runs.run_id
		  FROM striatumd.repositories r
		  JOIN striatumd.runs runs ON runs.repository_id = r.repository_id
		 WHERE r.state = 'active' AND runs.state IN ('running','paused')
		   AND r.repository_id = $1`, repoID)
	if err != nil {
		t.Fatalf("active-run query: %v", err)
	}
	for _, row := range activeRows {
		if fmt.Sprint(row["run_id"]) == runID {
			t.Fatalf("needs_operator run %s must be excluded from the active-run sweep query", runID)
		}
	}

	// (2) A direct re-sweep is idempotent: no duplicate escalation rows, no
	// duplicate blockers, the run_escalated_at guard holds.
	result, err := SweepRun(ctx, runner, repoID, runID, "")
	if err != nil {
		t.Fatalf("re-sweep: %v", err)
	}
	escSummary, _ := result["escalations"].(map[string]any)
	if intValue(escSummary["raised_count"]) != 0 {
		t.Fatalf("re-sweep raised_count = %v, want 0 (idempotent); %#v", escSummary["raised_count"], escSummary)
	}
	escIDSecond, _ := recoveryExhaustedEscalation(t, ctx, runner, repoID, runID)
	if escIDSecond != escIDFirst {
		t.Fatalf("re-sweep created a new escalation %q (was %q); must be idempotent", escIDSecond, escIDFirst)
	}
	// Still exactly one blocker of this kind.
	blkRows, err := queryRows(ctx, runner, `
		SELECT blocker_id FROM striatumd.blockers
		 WHERE repository_id = $1 AND run_id = $2 AND blocker_kind = 'recovery_exhausted'`, repoID, runID)
	if err != nil {
		t.Fatalf("count recovery_exhausted blockers: %v", err)
	}
	if len(blkRows) != 1 {
		t.Fatalf("recovery_exhausted blocker count = %d, want 1 (no duplicate)", len(blkRows))
	}
}

// TestEscalationResolveClearsNeedsOperator: resolving the recovery_exhausted
// escalation via reads.HandleEscalationResolve flips the run
// needs_operator -> running, and the run is then sweepable/claimable again.
func TestEscalationResolveClearsNeedsOperator(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_resolve_clears_needs_operator"
	runID, jobID, _, _, _ := seedDeadLaneRepoWriteJob(t, ctx, runner, repoID)
	preseedExhaustedBudget(t, ctx, runner, repoID, runID, jobID)

	if _, err := SweepRun(ctx, runner, repoID, runID, ""); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := runStateOf(t, ctx, runner, repoID, runID); got != "needs_operator" {
		t.Fatalf("run state = %q, want needs_operator", got)
	}
	escalationID, _ := recoveryExhaustedEscalation(t, ctx, runner, repoID, runID)

	resolved, err := reads.HandleEscalationResolve(ctx, runner, intgEnv(repoID, map[string]any{
		"escalation_id":   escalationID,
		"resolution_note": "transferred the stuck job to a fresh session and re-prepared write_scope",
	}))
	if err != nil {
		t.Fatalf("escalation resolve: %v", err)
	}
	if fmt.Sprint(resolved["status"]) != "resolved" {
		t.Fatalf("resolve status = %v, want resolved", resolved["status"])
	}

	// Run flipped back to running.
	if got := runStateOf(t, ctx, runner, repoID, runID); got != "running" {
		t.Fatalf("post-resolve run state = %q, want running", got)
	}

	// The escalation_inbox + blocker rows are resolved.
	row, err := oneRow(ctx, runner, `
		SELECT ei.state AS ei_state, b.state AS b_state
		  FROM striatumd.escalation_inbox ei
		  JOIN striatumd.blockers b
		    ON b.repository_id = ei.repository_id AND b.blocker_id = ei.blocker_id
		 WHERE ei.repository_id = $1 AND ei.escalation_id = $2`, repoID, escalationID)
	if err != nil {
		t.Fatalf("read resolved escalation: %v", err)
	}
	if got := fmt.Sprint(row["ei_state"]); got != "resolved" {
		t.Fatalf("escalation_inbox state = %q, want resolved", got)
	}
	if got := fmt.Sprint(row["b_state"]); got != "resolved" {
		t.Fatalf("blocker state = %q, want resolved", got)
	}

	// The run is now sweepable again (back in the active-run set).
	activeRows, err := queryRows(ctx, runner, `
		SELECT runs.run_id
		  FROM striatumd.repositories r
		  JOIN striatumd.runs runs ON runs.repository_id = r.repository_id
		 WHERE r.state = 'active' AND runs.state IN ('running','paused')
		   AND r.repository_id = $1 AND runs.run_id = $2`, repoID, runID)
	if err != nil {
		t.Fatalf("active-run query: %v", err)
	}
	if len(activeRows) != 1 {
		t.Fatalf("run not back in active-run set after resolve; got %d rows", len(activeRows))
	}
}
