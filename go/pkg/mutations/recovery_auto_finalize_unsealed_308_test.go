package mutations

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
)

// seedUnsealedPublishedFinalJob308 drives the seeded worktree-required job into
// the #308 shape: a final (synthesis) lane published its required artifact
// (durable + git-anchored on the run branch), then its process was SIGTERM-killed
// in the publish->work.complete window. The owning session stays state='active'
// with FRESH frozen activity that proves it engaged the work protocol and emitted
// output but never sealed (agent_exited_unsealed): no last_work_complete_at, but
// a last_tool_call_started_at and last_pty_activity_at. Its supervisor pointer's
// pane probes DEAD (the unforgeable confirmed-dead signal). The lease is expired
// and there is no on-disk file at the main repo_root (the per-job worktree was
// torn down), so the auto-publish pass cannot complete it and the decision tree
// reaches the confirmed-dead agent_exited_unsealed default branch.
func seedUnsealedPublishedFinalJob308(t *testing.T, ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs) {
	t.Helper()
	// Declare ONE required handoff artifact (synthesis deliverable).
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
	fresh := now.Add(-1 * time.Second)
	// The session stays state='active' with frozen-recent activity that proves it
	// engaged the protocol (tool call) and emitted output (pty) but never sealed
	// (no last_work_complete_at) -> deadAgentExitedUnsealed == true.
	if err := runner.Exec(ctx, `
		UPDATE striatumd.sessions
		   SET state = 'active', registered_at = $3, last_mcp_request_at = $3,
		       last_tools_list_at = $3, last_await_packet_at = $3,
		       last_packet_delivered_at = $3, last_ack_at = $3, last_work_block_at = $3,
		       last_session_heartbeat_at = $3, last_pty_activity_at = $3,
		       last_heartbeat_at = $3, last_tool_call_started_at = $3,
		       last_work_complete_at = NULL
		 WHERE repository_id = $1 AND session_id = $2`, ids.repoID, ids.sessionID, fresh); err != nil {
		t.Fatalf("stamp unsealed-exit session activity: %v", err)
	}
	// The lease is expired (the kill happened; lease aged out) and the job's lease
	// pointer cleared -> the default branch's confirmedDead probe is the sole signal.
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases SET state = 'expired', expires_at = $1, released_at = $1, release_reason = 'expired'
		 WHERE repository_id = $2 AND lease_id = $3`,
		now.Add(-time.Minute), ids.repoID, ids.leaseID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET current_lease_id = NULL
		 WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID); err != nil {
		t.Fatalf("clear job lease pointer: %v", err)
	}
	// seedWorktreeRequiredJob already attested a process_supervisor_pointer for the
	// session (intgAttest); pin its pid so the confirmedDead probe has a recorded
	// agent to judge (the pane will probe dead via the test's probeLaneLiveness).
	if err := runner.Exec(ctx, `
		UPDATE striatumd.process_supervisor_pointers SET pid = 8888, state = 'attached'
		 WHERE repository_id = $1 AND session_id = $2`, ids.repoID, ids.sessionID); err != nil {
		t.Fatalf("pin supervisor pointer pid: %v", err)
	}
}

// TestSweep308AutoFinalizesUnsealedPublishedFinalJob reproduces #308(A): an
// auto-driven final job killed in the publish->work.complete window, whose
// required artifact is already published and body-reconstructable on the run
// branch. The autonomous recovery sweep must AUTO-FINALIZE it from the durable
// artifact (the D200 path) instead of requeue_same_attempt -> escalate ->
// needs_operator. Before the fix the sweep requeued (max_attempts=1, budget 1,
// already spent), exhausted, and wedged the run at needs_operator.
func TestSweep308AutoFinalizesUnsealedPublishedFinalJob(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "unsealed_308", true)

	payload := []byte("durable ideation synthesis deliverable\n")
	// Make the body durable ON THE RUN BRANCH (the #308 premise): commit it in the
	// worktree, then point the run branch ref at that commit so the
	// reconstructability probe finds it reachable. Do NOT leave a copy at the main
	// repo_root, so the auto-publish-from-disk pass cannot complete it (the worktree
	// was torn down) and the decision tree must handle it.
	sha := commitWorktreeFile(t, ids.worktreeRoot, "docs/out.txt", string(payload))
	gitRun(t, repoRoot, "update-ref", "refs/heads/"+ids.runBranch, sha)
	seedPublishedArtifact(t, ctx, runner, ids, "art_unsealed_308", "out", "docs/out.txt", payload, nil)
	seedUnsealedPublishedFinalJob308(t, ctx, runner, ids)

	restore := probeLaneLiveness
	probeLaneLiveness = func(context.Context, map[string]any, int, string) gosupervisor.LaneLiveness {
		return gosupervisor.LaneLiveness{Backed: "tmux", Alive: false, Class: string(gosupervisor.TmuxLivenessPaneDead), ObservedPID: 8888}
	}
	t.Cleanup(func() { probeLaneLiveness = restore })

	result, err := SweepRun(ctx, runner, ids.repoID, ids.runID, "")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// The job was auto-finalized, NOT requeued.
	if got := jobState(t, ctx, runner, ids.repoID, ids.jobID); got != "completed" {
		t.Fatalf("job state = %q, want completed (auto-finalized from durable artifact); recovery_actions=%#v", got, result["recovery_actions"])
	}
	// requeue_count must NOT have climbed: the artifact was already published, so
	// re-running a max_attempts=1 synthesis lane is the wrong reflex.
	requeue, _, escalation := jobRecoveryCounts(t, ctx, runner, ids.repoID, ids.jobID)
	if requeue != 0 {
		t.Fatalf("requeue_count = %d, want 0 (auto-finalize must not requeue a published artifact)", requeue)
	}
	if escalation {
		t.Fatalf("escalation_pending = true, want false (auto-finalize avoids the escalation)")
	}
	// The decision tree reports an auto_finalize action that acted.
	summary := recoveryActionsFromSweep(t, result)
	acts, _ := summary["actions"].([]map[string]any)
	foundFinalize := false
	for _, a := range acts {
		if a["action"] == "auto_finalize_unsealed" && a["acted"] == true {
			foundFinalize = true
		}
	}
	if !foundFinalize {
		t.Fatalf("expected an acted auto_finalize_unsealed action; got %#v", summary["actions"])
	}
	// The single-job run then completes (no needs_operator wedge).
	runState := scalarStr(t, ctx, runner, `
		SELECT state FROM striatumd.runs WHERE repository_id = $1 AND run_id = $2`, ids.repoID, ids.runID)
	if runState == "needs_operator" {
		t.Fatalf("run state = needs_operator, want it auto-finalized away from the wedge")
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

// TestSweep308DoesNotAutoFinalizeWhenArtifactMissing is the #308 SAFETY guard:
// a confirmed-dead agent_exited_unsealed job whose required artifact is NOT
// present must still requeue (then escalate per budget), NOT auto-finalize. The
// finalize path only fires when the deliverable is durable.
func TestSweep308DoesNotAutoFinalizeWhenArtifactMissing(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "unsealed_308_missing", true)

	// Declare the required artifact but DO NOT publish it (no artifact row, nothing
	// on the run branch). The deliverable is not durable.
	seedUnsealedPublishedFinalJob308(t, ctx, runner, ids)

	restore := probeLaneLiveness
	probeLaneLiveness = func(context.Context, map[string]any, int, string) gosupervisor.LaneLiveness {
		return gosupervisor.LaneLiveness{Backed: "tmux", Alive: false, Class: string(gosupervisor.TmuxLivenessPaneDead), ObservedPID: 8888}
	}
	t.Cleanup(func() { probeLaneLiveness = restore })

	result, err := SweepRun(ctx, runner, ids.repoID, ids.runID, "")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// The job was NOT finalized — it requeued (the artifact is missing). With
	// max_attempts handling aside, the decision tree must NOT complete it.
	if got := jobState(t, ctx, runner, ids.repoID, ids.jobID); got == "completed" {
		t.Fatalf("job state = completed, want NOT completed (artifact missing must not auto-finalize); recovery_actions=%#v", result["recovery_actions"])
	}
	summary := recoveryActionsFromSweep(t, result)
	acts, _ := summary["actions"].([]map[string]any)
	for _, a := range acts {
		if a["action"] == "auto_finalize_unsealed" && a["acted"] == true {
			t.Fatalf("auto_finalize_unsealed acted on a job with a missing artifact; %#v", a)
		}
	}
}
