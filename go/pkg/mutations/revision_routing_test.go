package mutations

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// revisionFixture seeds a repo + run with a synthesis job (`synth`) that is
// already completed and a design-review job (`review_design_codex`) that is
// running with an active lease, mirroring run_651e20f3... from issue #63 F1.
// The `cycles` argument is written verbatim into the workflow snapshot.
func revisionFixture(t *testing.T, ctx context.Context, runner db.Runner, repoID string, cycles []any) (runID, reviewJobID, synthJobID, sessionID, leaseID string) {
	t.Helper()
	runID = "run_" + repoID
	reviewJobID = "job_review_" + repoID
	synthJobID = "job_synth_" + repoID
	sessionID = "sess_reviewer_" + repoID
	leaseID = "lease_" + repoID

	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{
		"workflow_id": "wf",
		"roles":       map[string]any{"reviewer": map[string]any{}, "synthesizer": map[string]any{}},
		"lanes":       map[string]any{"codex": map[string]any{}, "claude": map[string]any{}},
		"jobs": []any{
			map[string]any{"id": "synth", "type": "synthesis", "role_id": "synthesizer"},
			map[string]any{"id": "review_design_codex", "type": "review", "role_id": "reviewer", "review_posture": "threat_model"},
		},
		"cycles": cycles,
	})

	now := time.Now().UTC()

	// Completed upstream synthesis job (the cycle target).
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, idempotency_key, expected_artifacts_json, created_at, completed_at
		) VALUES ($1,$2,$3,'synth',1,'completed','synthesizer','Synthesis','synthesis','idem_synth_'||$1,'[]'::jsonb,$4,$4)`,
		repoID, synthJobID, runID, now); err != nil {
		t.Fatalf("insert synth job: %v", err)
	}

	// Running design-review job (the work message FK requires the job to
	// exist first, so insert the job, then the message, then back-fill the
	// job's current_message_id / current_lease_id pointers).
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, idempotency_key, expected_artifacts_json, created_at, started_at
		) VALUES ($1,$2,$3,'review_design_codex',1,'running','reviewer','Design Review','review','idem_review_'||$1,'[]'::jsonb,$4,$4)`,
		repoID, reviewJobID, runID, now); err != nil {
		t.Fatalf("insert review job: %v", err)
	}

	msgID := "msg_review_" + repoID
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.queue_messages (
		  repository_id, message_id, run_id, job_id, kind, state, priority,
		  target_role_id, target_lane_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,'work','acked',0,'reviewer','codex',$5,$5)`,
		repoID, msgID, runID, reviewJobID, now); err != nil {
		t.Fatalf("insert review message: %v", err)
	}

	intgSeedSession(t, ctx, runner, repoID, runID, sessionID, "reviewer", "codex", []string{"review"}, "active")
	intgAttest(t, ctx, runner, repoID, runID, sessionID, "codex")

	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.leases (
		  repository_id, lease_id, run_id, resource_type, resource_id,
		  owner_session_id, state, acquired_at, expires_at
		) VALUES ($1,$2,$3,'job',$4,$5,'active',NOW(),NOW() + INTERVAL '1 hour')`,
		repoID, leaseID, runID, reviewJobID, sessionID); err != nil {
		t.Fatalf("insert lease: %v", err)
	}

	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET current_message_id = $3, current_lease_id = $4
		 WHERE repository_id = $1 AND job_id = $2`,
		repoID, reviewJobID, msgID, leaseID); err != nil {
		t.Fatalf("link review job pointers: %v", err)
	}
	return runID, reviewJobID, synthJobID, sessionID, leaseID
}

func jobState(t *testing.T, ctx context.Context, runner any, repoID, jobID string) string {
	t.Helper()
	row, err := oneRow(ctx, runner, `SELECT state FROM striatumd.jobs WHERE repository_id = $1 AND job_id = $2`, repoID, jobID)
	if err != nil {
		t.Fatalf("select job %s: %v", jobID, err)
	}
	return fmt.Sprint(row["state"])
}

func openRevisionBlockers(t *testing.T, ctx context.Context, runner any, repoID, jobID string) int {
	t.Helper()
	row, err := oneRow(ctx, runner, `
		SELECT count(*) AS n FROM striatumd.blockers
		 WHERE repository_id = $1 AND job_id = $2 AND state = 'open'
		   AND blocker_kind = 'revision_routing'`, repoID, jobID)
	if err != nil {
		t.Fatalf("count blockers: %v", err)
	}
	return intValue(row["n"])
}

// F1 (a): a needs_revision verdict with a matching cycle routes to the target
// and re-opens it; no revision_routing human_checkpoint is created.
func TestNeedsRevisionRoutesToMatchingCycle(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_revision_route"
	cycles := []any{
		map[string]any{
			"from":            "review_design_codex",
			"to":              "synth",
			"on_verdict":      "needs_revision",
			"max_iterations":  float64(2),
			"allow_same_lane": true,
		},
	}
	runID, reviewJobID, synthJobID, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, cycles)

	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     reviewJobID,
		"lease_id":   leaseID,
		"verdict":    "needs_revision",
	}))
	if err != nil {
		t.Fatalf("record verdict: %v", err)
	}
	if fmt.Sprint(result["status"]) != "revision_routed" {
		t.Fatalf("status = %v, want revision_routed; result=%#v", result["status"], result)
	}
	if fmt.Sprint(result["cycle_target_job_id"]) != synthJobID {
		t.Fatalf("cycle_target_job_id = %v, want %s", result["cycle_target_job_id"], synthJobID)
	}
	if intValue(result["cycle_iteration"]) != 1 {
		t.Fatalf("cycle_iteration = %v, want 1", result["cycle_iteration"])
	}
	// #476 negative control: a single-reviewer cycle has no sibling gating seats, so
	// the legibility count is 0 (the field is always present).
	if got := intValue(result["in_flight_sibling_gating_seats"]); got != 0 {
		t.Fatalf("in_flight_sibling_gating_seats = %v, want 0 for a single-reviewer cycle", got)
	}

	if got := jobState(t, ctx, runner, repoID, reviewJobID); got != "completed" {
		t.Fatalf("review job state = %q, want completed", got)
	}
	if got := jobState(t, ctx, runner, repoID, synthJobID); got != "queued" {
		t.Fatalf("synth job state = %q, want queued (re-opened for revision)", got)
	}
	if n := openRevisionBlockers(t, ctx, runner, repoID, reviewJobID); n != 0 {
		t.Fatalf("expected no revision_routing blocker, got %d", n)
	}

	// A fresh pending work message must exist for the re-opened synth job.
	msgRow, err := oneRow(ctx, runner, `
		SELECT count(*) AS n FROM striatumd.queue_messages
		 WHERE repository_id = $1 AND job_id = $2 AND kind = 'work' AND state = 'pending'`,
		repoID, synthJobID)
	if err != nil {
		t.Fatalf("count synth messages: %v", err)
	}
	if intValue(msgRow["n"]) != 1 {
		t.Fatalf("expected 1 pending work message for synth, got %v", msgRow["n"])
	}

	// The cycle-routed event must be recorded for max_iterations accounting.
	evtRow, err := oneRow(ctx, runner, `
		SELECT count(*) AS n FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND event_type = 'revision.cycle_routed'`,
		repoID, runID)
	if err != nil {
		t.Fatalf("count routed events: %v", err)
	}
	if intValue(evtRow["n"]) != 1 {
		t.Fatalf("expected 1 revision.cycle_routed event, got %v", evtRow["n"])
	}
}

// F1 (b): a needs_revision verdict with NO matching cycle still produces the
// revision_routing human_checkpoint (preserve current behavior).
func TestNeedsRevisionWithoutCycleOpensCheckpoint(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_revision_no_cycle"
	// No cycles declared.
	_, reviewJobID, synthJobID, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, []any{})

	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     reviewJobID,
		"lease_id":   leaseID,
		"verdict":    "needs_revision",
	}))
	if err != nil {
		t.Fatalf("record verdict: %v", err)
	}
	if fmt.Sprint(result["status"]) != "waiting_human" {
		t.Fatalf("status = %v, want waiting_human; result=%#v", result["status"], result)
	}
	if fmt.Sprint(result["blocker_id"]) == "" || result["blocker_id"] == nil {
		t.Fatalf("expected a blocker_id, got %#v", result)
	}
	if got := jobState(t, ctx, runner, repoID, reviewJobID); got != "waiting_human" {
		t.Fatalf("review job state = %q, want waiting_human", got)
	}
	// Upstream synth must remain untouched (still completed).
	if got := jobState(t, ctx, runner, repoID, synthJobID); got != "completed" {
		t.Fatalf("synth job state = %q, want completed (untouched)", got)
	}
	if n := openRevisionBlockers(t, ctx, runner, repoID, reviewJobID); n != 1 {
		t.Fatalf("expected 1 revision_routing blocker, got %d", n)
	}
}

// F1 (c): when the cycle's max_iterations budget is exhausted, the verdict
// falls back to a human checkpoint rather than re-routing forever.
func TestNeedsRevisionExhaustedCycleOpensCheckpoint(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_revision_exhausted"
	cycles := []any{
		map[string]any{
			"from":            "review_design_codex",
			"to":              "synth",
			"on_verdict":      "needs_revision",
			"max_iterations":  float64(1),
			"allow_same_lane": true,
		},
	}
	runID, reviewJobID, synthJobID, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, cycles)

	// Pre-record one prior routing so the budget (1) is already spent.
	now := time.Now().UTC()
	if _, err := appendEvent(ctx, runner, repoID, runID, "revision.cycle_routed", nil, reviewJobID, nil, nil, nil, map[string]any{
		"from_workflow_job_id": "review_design_codex",
		"to_workflow_job_id":   "synth",
		"iteration":            1,
	}); err != nil {
		t.Fatalf("seed prior routing event: %v", err)
	}
	_ = now

	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     reviewJobID,
		"lease_id":   leaseID,
		"verdict":    "needs_revision",
	}))
	if err != nil {
		t.Fatalf("record verdict: %v", err)
	}
	if fmt.Sprint(result["status"]) != "waiting_human" {
		t.Fatalf("status = %v, want waiting_human (budget exhausted); result=%#v", result["status"], result)
	}
	if got := jobState(t, ctx, runner, repoID, synthJobID); got != "completed" {
		t.Fatalf("synth job state = %q, want completed (not re-opened when exhausted)", got)
	}
	if n := openRevisionBlockers(t, ctx, runner, repoID, reviewJobID); n != 1 {
		t.Fatalf("expected 1 revision_routing blocker, got %d", n)
	}
}

// F3 (a): a completed cycle-target job can be re-opened for revision through
// the operator run.retry_job path.
func TestRetryJobReopensCompletedCycleTarget(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_retry_cycle_target"
	cycles := []any{
		map[string]any{
			"from":            "review_design_codex",
			"to":              "synth",
			"on_verdict":      "needs_revision",
			"max_iterations":  float64(2),
			"allow_same_lane": true,
		},
	}
	runID, _, synthJobID, _, _ := revisionFixture(t, ctx, runner, repoID, cycles)

	result, err := HandleRunRetryJob(ctx, runner, intgEnv(repoID, map[string]any{
		"run_id": runID,
		"job_id": synthJobID,
	}))
	if err != nil {
		t.Fatalf("retry completed cycle target: %v", err)
	}
	if fmt.Sprint(result["previous_state"]) != "completed" {
		t.Fatalf("previous_state = %v, want completed", result["previous_state"])
	}
	if fmt.Sprint(result["new_state"]) != "queued" {
		t.Fatalf("new_state = %v, want queued", result["new_state"])
	}
	if got := jobState(t, ctx, runner, repoID, synthJobID); got != "queued" {
		t.Fatalf("synth job state = %q, want queued", got)
	}
}

// F3 (b): a completed job that is NOT a cycle target remains non-retriable.
func TestRetryJobRejectsCompletedNonCycleTarget(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_retry_non_cycle"
	// review_design_codex -> synth, but we'll try to retry the review (a
	// completed non-target) instead.
	cycles := []any{
		map[string]any{
			"from":            "review_design_codex",
			"to":              "synth",
			"on_verdict":      "needs_revision",
			"max_iterations":  float64(2),
			"allow_same_lane": true,
		},
	}
	runID, reviewJobID, _, _, _ := revisionFixture(t, ctx, runner, repoID, cycles)

	// Force the review job to completed so it is a completed non-cycle-target.
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET state = 'completed', current_lease_id = NULL
		 WHERE repository_id = $1 AND job_id = $2`, repoID, reviewJobID); err != nil {
		t.Fatalf("force review completed: %v", err)
	}

	_, err := HandleRunRetryJob(ctx, runner, intgEnv(repoID, map[string]any{
		"run_id": runID,
		"job_id": reviewJobID,
	}))
	if err == nil {
		t.Fatal("expected retry of completed non-cycle-target to be rejected")
	}
}

// seedDependency wires a job_dependencies edge (toJob depends on fromJob) with
// the given gate JSON.
func seedDependency(t *testing.T, ctx context.Context, runner db.Runner, repoID, toJobID, fromJobID string, gate map[string]any) {
	t.Helper()
	gateArg, err := db.JSONBArg(runner, gate)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.job_dependencies (repository_id, job_id, depends_on_job_id, gate_json)
		VALUES ($1,$2,$3,$4::jsonb)
		ON CONFLICT (repository_id, job_id, depends_on_job_id) DO NOTHING`,
		repoID, toJobID, fromJobID, gateArg); err != nil {
		t.Fatalf("insert dependency %s->%s: %v", fromJobID, toJobID, err)
	}
}

// seedBlockedJob inserts a blocked job (no message/lease) for the given
// workflow id and type.
func seedBlockedJob(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, jobID, workflowJobID, jobType, roleID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, idempotency_key, expected_artifacts_json, created_at
		) VALUES ($1,$2,$3,$4,1,'blocked',$5,$6,$7,'idem_'||$4||'_'||$1,'[]'::jsonb,$8)`,
		repoID, jobID, runID, workflowJobID, roleID, workflowJobID, jobType, now); err != nil {
		t.Fatalf("insert blocked job %s: %v", jobID, err)
	}
}

// completeSynthAndGateDownstream completes the (re-opened, queued) synth job and
// runs the daemon's downstream gating, exactly as work.complete does for a
// non-review job. Used by the end-to-end revision-loop test to advance the run
// after the cycle target re-completes.
func completeSynthAndGateDownstream(t *testing.T, ctx context.Context, runner db.Runner, repoID, synthJobID string) {
	t.Helper()
	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		now := nowString()
		if err := tx.Exec(ctx, `
			UPDATE striatumd.jobs
			   SET state = 'completed', completed_at = $1, current_lease_id = NULL,
			       current_message_id = NULL
			 WHERE repository_id = $2 AND job_id = $3`, now, repoID, synthJobID); err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'completed', completed_at = $1, updated_at = $1
			 WHERE repository_id = $2 AND job_id = $3 AND state IN ('pending','claimed','acked')`,
			now, repoID, synthJobID); err != nil {
			return nil, err
		}
		if err := maybeEnqueueDownstream(ctx, tx, repoID, synthJobID); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	}); err != nil {
		t.Fatalf("complete synth + gate downstream: %v", err)
	}
}

// reclaimReviewRunning puts a (re-enqueued, queued) review job back into the
// running state with a fresh session, lease and acked work message, mirroring
// claim+ack so a fresh verdict can be recorded against it.
func reclaimReviewRunning(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, reviewJobID string) (sessionID, leaseID string) {
	t.Helper()
	now := time.Now().UTC()
	sessionID = "sess_reviewer2_" + reviewJobID
	leaseID = "lease2_" + reviewJobID

	intgSeedSessionOrdinal(t, ctx, runner, repoID, runID, sessionID, "reviewer", "codex", []string{"review"}, "active", 2)
	// dbf2013b: record-verdict requires a live attested lane backend; attest the
	// fresh revision-cycle reviewer so the backend gate passes.
	intgAttest(t, ctx, runner, repoID, runID, sessionID, "codex")

	// Re-use the pending work message the re-enqueue already published for the
	// job (a fresh insert would trip uq_active_work_message_per_job); move it to
	// acked, the state recordVerdict expects.
	msgRow, err := oneRow(ctx, runner, `
		SELECT message_id FROM striatumd.queue_messages
		 WHERE repository_id = $1 AND job_id = $2 AND kind = 'work' AND state = 'pending'
		 ORDER BY created_at DESC LIMIT 1`, repoID, reviewJobID)
	if err != nil {
		t.Fatalf("find re-enqueued review message: %v", err)
	}
	msgID := fmt.Sprint(msgRow["message_id"])
	if err := runner.Exec(ctx, `
		UPDATE striatumd.queue_messages
		   SET state = 'acked', acked_at = $3, updated_at = $3
		 WHERE repository_id = $1 AND message_id = $2`, repoID, msgID, now); err != nil {
		t.Fatalf("ack reclaim review message: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.leases (
		  repository_id, lease_id, run_id, resource_type, resource_id,
		  owner_session_id, state, acquired_at, expires_at
		) VALUES ($1,$2,$3,'job',$4,$5,'active',NOW(),NOW() + INTERVAL '1 hour')`,
		repoID, leaseID, runID, reviewJobID, sessionID); err != nil {
		t.Fatalf("insert reclaim lease: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'running', started_at = $5, current_message_id = $3, current_lease_id = $4
		 WHERE repository_id = $1 AND job_id = $2`,
		repoID, reviewJobID, msgID, leaseID, now); err != nil {
		t.Fatalf("reclaim review running: %v", err)
	}
	return sessionID, leaseID
}

func countVerdicts(t *testing.T, ctx context.Context, runner any, repoID, jobID string) int {
	t.Helper()
	row, err := oneRow(ctx, runner, `
		SELECT count(*) AS n FROM striatumd.verdicts
		 WHERE repository_id = $1 AND job_id = $2`, repoID, jobID)
	if err != nil {
		t.Fatalf("count verdicts %s: %v", jobID, err)
	}
	return intValue(row["n"])
}

// Bug A (CRITICAL end-to-end): a needs_revision verdict re-opens the cycle
// target AND re-blocks its transitive downstream (the review that triggered the
// cycle, plus a gated implement job). When the target re-completes the review
// re-runs; a subsequent accepting verdict supersedes the stale needs_revision
// and makes the downstream implement job reachable. This proves the full
// review-after-revision loop (RFC 0083), which the original fix left broken:
// the reviews never re-ran and the run could finish with the revision
// unreviewed.
func TestRevisionCycleReReviewsAndUnblocksDownstream(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_revision_full_loop"
	cycles := []any{
		map[string]any{
			"from":            "review_design_codex",
			"to":              "synth",
			"on_verdict":      "needs_revision",
			"max_iterations":  float64(3),
			"allow_same_lane": true,
		},
	}
	runID, reviewJobID, synthJobID, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, cycles)

	// Wire the dependency chain synth -> review -> implement. The review depends
	// on synth (accept gate); implement depends on review with a verdict gate so
	// it only unblocks once the review accepts.
	implementJobID := "job_impl_" + repoID
	seedBlockedJob(t, ctx, runner, repoID, runID, implementJobID, "implement", "generic", "synthesizer")
	seedDependency(t, ctx, runner, repoID, reviewJobID, synthJobID, map[string]any{
		"on": "completed", "from": "synth", "to": "review_design_codex",
	})
	seedDependency(t, ctx, runner, repoID, implementJobID, reviewJobID, map[string]any{
		"on": "completed", "from": "review_design_codex", "to": "implement",
		"requires_verdict": []any{"accept", "accept_with_findings"},
	})

	// Step 1: review records needs_revision -> routes to synth.
	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     reviewJobID,
		"lease_id":   leaseID,
		"verdict":    "needs_revision",
	}))
	if err != nil {
		t.Fatalf("record needs_revision: %v", err)
	}
	if fmt.Sprint(result["status"]) != "revision_routed" {
		t.Fatalf("status = %v, want revision_routed; result=%#v", result["status"], result)
	}
	if got := jobState(t, ctx, runner, repoID, synthJobID); got != "queued" {
		t.Fatalf("synth state = %q, want queued (re-opened)", got)
	}
	// The triggering review is transitive downstream of synth and must be
	// re-blocked (not left completed) so it re-runs after the revision.
	if got := jobState(t, ctx, runner, repoID, reviewJobID); got != "blocked" {
		t.Fatalf("review state = %q, want blocked (re-blocked for re-review)", got)
	}
	// RFC 0126 P0 (D194): the stale needs_revision verdict is NOT cleared — verdict
	// history is now append-only. It survives the re-block (and, where the
	// review_generation columns are present, is rendered non-current by generation
	// mismatch); the fresh round's accept is recorded by a different session and
	// becomes the latest verdict.
	if n := countVerdicts(t, ctx, runner, repoID, reviewJobID); n != 1 {
		t.Fatalf("review verdicts after re-block = %d, want 1 (append-only; prior round preserved)", n)
	}
	// In production a revision round spans the time the build takes to re-run and
	// the reviewer to re-review (minutes), so the fresh round's verdict is
	// unambiguously newer than the preserved prior round. This fixture compresses
	// the whole loop into one wall-clock second and nowString() truncates to the
	// second, so without aging the two rounds tie on created_at and the random
	// verdict_id tiebreak in the latest-verdict read could surface the stale
	// needs_revision. Age the preserved prior round to model the real time gap —
	// RFC 0126 P0 keeps it as durable, non-current history rather than DELETEing it
	// (the generation-scoped gate that makes this unconditional lands in P2).
	if err := runner.Exec(ctx, `
		UPDATE striatumd.verdicts SET created_at = created_at - INTERVAL '1 hour'
		 WHERE repository_id = $1 AND job_id = $2`, repoID, reviewJobID); err != nil {
		t.Fatalf("age preserved prior-round verdict: %v", err)
	}
	// implement was already blocked and stays blocked (it never reached).
	if got := jobState(t, ctx, runner, repoID, implementJobID); got != "blocked" {
		t.Fatalf("implement state = %q, want blocked", got)
	}

	// Step 2: synth re-completes -> downstream gating re-enqueues the review.
	completeSynthAndGateDownstream(t, ctx, runner, repoID, synthJobID)
	if got := jobState(t, ctx, runner, repoID, reviewJobID); got != "queued" {
		t.Fatalf("review state after synth re-complete = %q, want queued (re-enqueued)", got)
	}
	if got := jobState(t, ctx, runner, repoID, implementJobID); got != "blocked" {
		t.Fatalf("implement state = %q, want blocked (review not yet accepted)", got)
	}

	// Step 3: re-claim the review and record a fresh accepting verdict. The new
	// verdict (recorded by a different session) becomes the latest over the
	// preserved-but-non-current prior round; the downstream gate sees an accept
	// and the implement job becomes reachable.
	sessionID2, leaseID2 := reclaimReviewRunning(t, ctx, runner, repoID, runID, reviewJobID)
	acceptResult, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID2,
		"job_id":     reviewJobID,
		"lease_id":   leaseID2,
		"verdict":    "accept",
	}))
	if err != nil {
		t.Fatalf("record fresh accept: %v", err)
	}
	if fmt.Sprint(acceptResult["status"]) != "completed" {
		t.Fatalf("accept status = %v, want completed; result=%#v", acceptResult["status"], acceptResult)
	}
	if got := jobState(t, ctx, runner, repoID, reviewJobID); got != "completed" {
		t.Fatalf("review state after accept = %q, want completed", got)
	}
	if got := jobState(t, ctx, runner, repoID, implementJobID); got != "queued" {
		t.Fatalf("implement state after accept = %q, want queued (downstream unblocked)", got)
	}
	// Sanity: the latest verdict on the review is the fresh accept.
	latest, err := latestVerdict(ctx, runner, repoID, reviewJobID)
	if err != nil {
		t.Fatalf("latest verdict: %v", err)
	}
	if latest != "accept" {
		t.Fatalf("latest review verdict = %q, want accept", latest)
	}
}

// Bug B: when the cycle target is already non-terminal (an operator retried it
// concurrently so it is queued/running), the needs_revision verdict + cycle
// routed event must still be recorded and the re-open simply skipped — NOT
// error and roll back the whole verdict transaction.
func TestRevisionCycleTargetAlreadyRunningRecordsVerdict(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_revision_target_running"
	cycles := []any{
		map[string]any{
			"from":            "review_design_codex",
			"to":              "synth",
			"on_verdict":      "needs_revision",
			"max_iterations":  float64(2),
			"allow_same_lane": true,
		},
	}
	runID, reviewJobID, synthJobID, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, cycles)

	// Simulate a concurrent operator retry: the synth target is already running.
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET state = 'running', completed_at = NULL
		 WHERE repository_id = $1 AND job_id = $2`, repoID, synthJobID); err != nil {
		t.Fatalf("force synth running: %v", err)
	}

	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     reviewJobID,
		"lease_id":   leaseID,
		"verdict":    "needs_revision",
	}))
	if err != nil {
		t.Fatalf("record verdict against running target must not error: %v", err)
	}
	if fmt.Sprint(result["status"]) != "revision_routed" {
		t.Fatalf("status = %v, want revision_routed; result=%#v", result["status"], result)
	}
	if result["target_already_open"] != true {
		t.Fatalf("expected target_already_open=true; result=%#v", result)
	}
	// The verdict transaction committed: the verdict row and the cycle-routed
	// event both persist.
	if n := countVerdicts(t, ctx, runner, repoID, reviewJobID); n != 1 {
		t.Fatalf("verdict count = %d, want 1 (verdict recorded, not rolled back)", n)
	}
	evtRow, err := oneRow(ctx, runner, `
		SELECT count(*) AS n FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND event_type = 'revision.cycle_routed'`,
		repoID, runID)
	if err != nil {
		t.Fatalf("count routed events: %v", err)
	}
	if intValue(evtRow["n"]) != 1 {
		t.Fatalf("expected 1 revision.cycle_routed event, got %v", evtRow["n"])
	}
	// The running target is untouched (not clobbered back to blocked/queued).
	if got := jobState(t, ctx, runner, repoID, synthJobID); got != "running" {
		t.Fatalf("synth state = %q, want running (left alone)", got)
	}
	// The triggering review still completes (the verdict routed).
	if got := jobState(t, ctx, runner, repoID, reviewJobID); got != "completed" {
		t.Fatalf("review state = %q, want completed", got)
	}
}

// Bug C: a completed cycle target whose only declaring cycle is keyed on a
// non-needs_revision verdict is NOT retriable through run.retry_job. Only
// genuine needs_revision revision targets unlock the F3 retry relaxation.
func TestRetryJobRejectsNonNeedsRevisionCycleTarget(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_retry_non_needs_revision"
	cycles := []any{
		map[string]any{
			"from":            "review_design_codex",
			"to":              "synth",
			"on_verdict":      "reject", // not needs_revision
			"max_iterations":  float64(2),
			"allow_same_lane": true,
		},
	}
	runID, _, synthJobID, _, _ := revisionFixture(t, ctx, runner, repoID, cycles)

	_, err := HandleRunRetryJob(ctx, runner, intgEnv(repoID, map[string]any{
		"run_id": runID,
		"job_id": synthJobID,
	}))
	if err == nil {
		t.Fatal("expected retry of completed non-needs_revision cycle target to be rejected")
	}
	if got := jobState(t, ctx, runner, repoID, synthJobID); got != "completed" {
		t.Fatalf("synth state = %q, want completed (not retried)", got)
	}
}

// #77: a review that feeds an adjudicator (downstream phase_synthesis whose
// dependency gate does not require a clearing verdict) must NOT open its own
// revision checkpoint on needs_revision — it completes and enqueues the
// adjudicator to weigh the dissent.
func TestNeedsRevisionFeedingAdjudicatorIsAbsorbed(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_revision_adjudicator_absorb"
	runID, reviewJobID, synthJobID, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, []any{})

	adjJobID := "job_adjudicate_" + repoID
	seedBlockedJob(t, ctx, runner, repoID, runID, adjJobID, "adjudicate", "synthesis", "adjudicator")
	seedDependency(t, ctx, runner, repoID, adjJobID, reviewJobID, map[string]any{}) // no requires_verdict -> absorbs

	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID, "job_id": reviewJobID, "lease_id": leaseID, "verdict": "needs_revision",
	}))
	if err != nil {
		t.Fatalf("record verdict: %v", err)
	}
	if fmt.Sprint(result["status"]) != "completed" {
		t.Fatalf("#77: review feeding an adjudicator must complete (absorbed), got status=%v result=%#v", result["status"], result)
	}
	if result["absorbed_by_adjudicator"] != true {
		t.Fatalf("expected absorbed_by_adjudicator=true, got %#v", result)
	}
	if got := jobState(t, ctx, runner, repoID, reviewJobID); got != "completed" {
		t.Fatalf("review job state = %q, want completed", got)
	}
	if n := openRevisionBlockers(t, ctx, runner, repoID, reviewJobID); n != 0 {
		t.Fatalf("#77: no revision_routing checkpoint should open, got %d", n)
	}
	if got := jobState(t, ctx, runner, repoID, adjJobID); got != "queued" {
		t.Fatalf("adjudicator job state = %q, want queued (enqueued to absorb dissent)", got)
	}
	if got := jobState(t, ctx, runner, repoID, synthJobID); got != "completed" {
		t.Fatalf("synth job state = %q, want completed (untouched)", got)
	}
}

// #77 guard: a downstream phase_synthesis whose gate REQUIRES a clearing verdict
// from this reviewer does not absorb — needs_revision still opens a checkpoint.
func TestNeedsRevisionFeedingGatedSynthesisStillCheckpoints(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_revision_gated_synth"
	runID, reviewJobID, _, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, []any{})

	gatedJobID := "job_gated_synth_" + repoID
	seedBlockedJob(t, ctx, runner, repoID, runID, gatedJobID, "gate", "synthesis", "synthesizer")
	seedDependency(t, ctx, runner, repoID, gatedJobID, reviewJobID, map[string]any{
		"requires_verdict": []any{"accept", "accept_with_findings"},
	})

	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID, "job_id": reviewJobID, "lease_id": leaseID, "verdict": "needs_revision",
	}))
	if err != nil {
		t.Fatalf("record verdict: %v", err)
	}
	if fmt.Sprint(result["status"]) != "waiting_human" {
		t.Fatalf("#77 guard: a clearing-verdict gate must still checkpoint, got status=%v", result["status"])
	}
	if n := openRevisionBlockers(t, ctx, runner, repoID, reviewJobID); n != 1 {
		t.Fatalf("expected 1 revision_routing blocker, got %d", n)
	}
}

// #476 (RFC 0154 alternative A — legibility): when one final reviewer in a
// multi-reviewer gating cohort records needs_revision while a SIBLING gating
// reviewer feeding the same downstream gate is still in flight, the route still
// fires on the first dissent (today's behavior; the WAIT debounce is the D250
// opt-in, not yet implemented), but the routed event + result now record
// in_flight_sibling_gating_seats so the short-circuit is operator-visible. The
// single-reviewer fixture (TestNeedsRevisionRoutesToMatchingCycle) is the
// negative control: it has no siblings, so the count is 0.
func TestNeedsRevisionRecordsInFlightSiblingGatingSeats(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_revision_sibling_476"
	now := time.Now().UTC()

	intgSeedRepo(t, ctx, runner, repoID)
	runID := "run_" + repoID
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{
		"workflow_id": "wf",
		"roles":       map[string]any{"reviewer": map[string]any{}, "author": map[string]any{}, "synthesizer": map[string]any{}},
		"lanes":       map[string]any{"codex": map[string]any{}, "claude": map[string]any{}},
		"jobs": []any{
			map[string]any{"id": "author", "type": "synthesis", "role_id": "author"},
			map[string]any{"id": "review_final_codex", "type": "review", "role_id": "reviewer"},
			map[string]any{"id": "review_final_claude", "type": "review", "role_id": "reviewer"},
			map[string]any{"id": "gate", "type": "synthesis", "role_id": "synthesizer"},
		},
		// Both final reviewers declare a revision cycle back to the shared author.
		"cycles": []any{
			map[string]any{"from": "review_final_codex", "to": "author", "on_verdict": "needs_revision", "max_iterations": float64(2), "allow_same_lane": true},
			map[string]any{"from": "review_final_claude", "to": "author", "on_verdict": "needs_revision", "max_iterations": float64(2), "allow_same_lane": true},
		},
	})

	authorJobID := "job_author_" + repoID
	gateJobID := "job_gate_" + repoID
	reviewCodexJobID := "job_review_codex_" + repoID
	reviewClaudeJobID := "job_review_claude_" + repoID

	// Completed author (the cycle target).
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, idempotency_key, expected_artifacts_json, created_at, completed_at
		) VALUES ($1,$2,$3,'author',1,'completed','author','Author','synthesis','idem_author_'||$1,'[]'::jsonb,$4,$4)`,
		repoID, authorJobID, runID, now); err != nil {
		t.Fatalf("insert author job: %v", err)
	}
	// Blocked downstream gate the two reviewers feed (the cohort denominator).
	seedBlockedJob(t, ctx, runner, repoID, runID, gateJobID, "gate", "synthesis", "synthesizer")

	// Reviewer A (codex): running with an active lease, about to record needs_revision.
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, idempotency_key, expected_artifacts_json, created_at, started_at
		) VALUES ($1,$2,$3,'review_final_codex',1,'running','reviewer','Final Review Codex','review','idem_rc_'||$1,'[]'::jsonb,$4,$4)`,
		repoID, reviewCodexJobID, runID, now); err != nil {
		t.Fatalf("insert reviewer codex job: %v", err)
	}
	msgID := "msg_rc_" + repoID
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.queue_messages (
		  repository_id, message_id, run_id, job_id, kind, state, priority,
		  target_role_id, target_lane_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,'work','acked',0,'reviewer','codex',$5,$5)`,
		repoID, msgID, runID, reviewCodexJobID, now); err != nil {
		t.Fatalf("insert reviewer codex message: %v", err)
	}
	sessionID := "sess_rc_" + repoID
	leaseID := "lease_rc_" + repoID
	intgSeedSession(t, ctx, runner, repoID, runID, sessionID, "reviewer", "codex", []string{"review"}, "active")
	intgAttest(t, ctx, runner, repoID, runID, sessionID, "codex")
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.leases (
		  repository_id, lease_id, run_id, resource_type, resource_id,
		  owner_session_id, state, acquired_at, expires_at
		) VALUES ($1,$2,$3,'job',$4,$5,'active',NOW(),NOW() + INTERVAL '1 hour')`,
		repoID, leaseID, runID, reviewCodexJobID, sessionID); err != nil {
		t.Fatalf("insert reviewer codex lease: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET current_message_id = $3, current_lease_id = $4
		 WHERE repository_id = $1 AND job_id = $2`,
		repoID, reviewCodexJobID, msgID, leaseID); err != nil {
		t.Fatalf("link reviewer codex pointers: %v", err)
	}

	// Reviewer B (claude): STILL IN FLIGHT (claimed) — the sibling the route
	// short-circuits past.
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, idempotency_key, expected_artifacts_json, created_at, started_at
		) VALUES ($1,$2,$3,'review_final_claude',1,'claimed','reviewer','Final Review Claude','review','idem_rcl_'||$1,'[]'::jsonb,$4,$4)`,
		repoID, reviewClaudeJobID, runID, now); err != nil {
		t.Fatalf("insert reviewer claude job: %v", err)
	}

	// Both reviewers feed the gate (the frozen gating-seat denominator) and cycle
	// back to the author.
	seedDependency(t, ctx, runner, repoID, gateJobID, reviewCodexJobID, map[string]any{})
	seedDependency(t, ctx, runner, repoID, gateJobID, reviewClaudeJobID, map[string]any{})

	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     reviewCodexJobID,
		"lease_id":   leaseID,
		"verdict":    "needs_revision",
	}))
	if err != nil {
		t.Fatalf("record verdict: %v", err)
	}
	if fmt.Sprint(result["status"]) != "revision_routed" {
		t.Fatalf("status = %v, want revision_routed; result=%#v", result["status"], result)
	}
	// The in-flight claude sibling is counted; the routing codex seat is excluded.
	if got := intValue(result["in_flight_sibling_gating_seats"]); got != 1 {
		t.Fatalf("in_flight_sibling_gating_seats = %v, want 1 (claude still claimed); result=%#v", result["in_flight_sibling_gating_seats"], result)
	}

	// The routed event carries the same count for `why <run_id>` / run-summary legibility.
	evtRow, err := oneRow(ctx, runner, `
		SELECT payload_json->>'in_flight_sibling_gating_seats' AS n
		  FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND event_type = 'revision.cycle_routed'
		 ORDER BY event_id DESC LIMIT 1`, repoID, runID)
	if err != nil {
		t.Fatalf("read routed event: %v", err)
	}
	if fmt.Sprint(evtRow["n"]) != "1" {
		t.Fatalf("revision.cycle_routed in_flight_sibling_gating_seats = %v, want 1", evtRow["n"])
	}
}
