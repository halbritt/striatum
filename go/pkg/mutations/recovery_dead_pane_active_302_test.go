package mutations

import (
	"context"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
)

// seedDeadPaneActiveSession302 reproduces the #302 wedge: a supervised lane
// claimed a job, published its artifact, then the agent process exited and the
// tmux pane died cleanly. The job fell back to 'queued' (its prior claim's lease
// was released by work.complete / no_work teardown), but the lane's session row
// stays state='active' with FRESH activity (the death froze its timestamps a
// moment ago). The recorded supervisor pointer's pane probes DEAD.
//
// This differs from the #291 leaseless seed in two #302-specific ways:
//   - the job has a RELEASED lease from its prior claim (owner = the dead session),
//     so the #291 bound-session join (which requires l.owner_session_id IS NULL)
//     never fires and the session is resolved via that released lease instead.
//   - the session's activity is recent (not past the protocol-idle deadline), so
//     neither CASE 1 (session-dead) nor CASE 2 (honest stall) fires on the session
//     signal; the masked-dead supervisor probe is the SOLE recovery signal — the
//     exact #147 Symptom B shape, but for a queued job behind a released lease.
func seedDeadPaneActiveSession302(t *testing.T, ctx context.Context, runner db.Runner, repoID string) (runID, jobID, msgID, sessionID, leaseID string) {
	t.Helper()
	runID = "run_" + repoID
	jobID = "job_diverge_" + repoID
	msgID = "msg_diverge_" + repoID
	sessionID = "sess_diverger_" + repoID
	leaseID = "lease_prior_" + repoID

	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{
		"workflow_id": "wf",
		"roles":       map[string]any{"reviewer": map[string]any{}},
		"lanes":       map[string]any{"claude": map[string]any{}},
		"jobs": []any{
			map[string]any{"id": "diverge", "type": "build", "role_id": "reviewer"},
		},
	})
	// The lane's session stays state='active'. Its activity is FRESH (1s ago) — the
	// process death froze the timestamps a moment ago — so the session/lease signal
	// reports neither dead nor stalled.
	intgSeedSession(t, ctx, runner, repoID, runID, sessionID, "reviewer", "claude", []string{"write"}, "active")
	now := time.Now().UTC()
	fresh := now.Add(-1 * time.Second)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.sessions
		   SET registered_at = $3, last_mcp_request_at = $3, last_tools_list_at = $3,
		       last_await_packet_at = $3, last_packet_delivered_at = $3, last_ack_at = $3,
		       last_work_block_at = $3, last_session_heartbeat_at = $3,
		       last_pty_activity_at = $3, last_heartbeat_at = $3,
		       last_tool_call_started_at = $3
		 WHERE repository_id = $1 AND session_id = $2`, repoID, sessionID, fresh); err != nil {
		t.Fatalf("stamp fresh session activity: %v", err)
	}

	// The job is QUEUED again with a PENDING work message — its prior claim's lease
	// was released on the no_work teardown, so current_lease_id is NULL.
	wsArg, err := db.JSONBArg(runner, map[string]any{"mode": "repo_write", "repo_write": true, "allowed_paths": []any{"docs/"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, idempotency_key, expected_artifacts_json, write_scope_json,
		  lane_selector_json, current_message_id, current_lease_id, created_at, ready_at
		) VALUES ($1,$2,$3,'diverge',1,'queued','reviewer','Diverge','build',
		          'idem_diverge_'||$1,'[]'::jsonb,$4::jsonb,'{"lane_id":"claude"}'::jsonb,$5,NULL,$6,$6)`,
		repoID, jobID, runID, wsArg, msgID, now); err != nil {
		t.Fatalf("insert queued diverge job: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.queue_messages (
		  repository_id, message_id, run_id, job_id, kind, state, priority,
		  target_role_id, target_lane_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,'work','pending',0,'reviewer','claude',$5,$5)`,
		repoID, msgID, runID, jobID, now); err != nil {
		t.Fatalf("insert pending work message: %v", err)
	}
	// A RELEASED lease from the prior claim, owned by the now-pane-dead session.
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.leases (
		  repository_id, lease_id, run_id, resource_type, resource_id,
		  owner_session_id, state, acquired_at, expires_at, last_heartbeat_at,
		  released_at, release_reason
		) VALUES ($1,$2,$3,'job',$4,$5,'released',$6,$7,$6,$6,'work_complete')`,
		repoID, leaseID, runID, jobID, sessionID, fresh, fresh.Add(15*time.Minute)); err != nil {
		t.Fatalf("insert released prior lease: %v", err)
	}
	// A recorded supervisor pointer whose tmux pane is dead (the #302 telltale).
	seedSupervisorPointer(t, ctx, runner, repoID, runID, sessionID, "attached", 7777)
	return runID, jobID, msgID, sessionID, leaseID
}

// TestSweep302RecoversDeadPaneActiveSessionQueuedJob reproduces #302: a queued
// job whose owning supervised session stays state='active' but whose tmux pane is
// dead (post-publish), behind a released prior-claim lease. The recovery sweep
// must detect the masked-dead pane and reclaim it — close the falsely-active
// session and leave the job claimable — so a single lane death cannot wedge an
// unattended run.
func TestSweep302RecoversDeadPaneActiveSessionQueuedJob(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_302_dead_pane_active"

	runID, jobID, msgID, sessionID, _ := seedDeadPaneActiveSession302(t, ctx, runner, repoID)

	restore := probeLaneLiveness
	probeLaneLiveness = func(context.Context, map[string]any, int, string) gosupervisor.LaneLiveness {
		// The tmux pane is positively dead (the divergent_ideation telltale).
		return gosupervisor.LaneLiveness{Backed: "tmux", Alive: false, Class: string(gosupervisor.TmuxLivenessPaneDead), ObservedPID: 7777}
	}
	t.Cleanup(func() { probeLaneLiveness = restore })

	result, err := SweepRun(ctx, runner, repoID, runID, "")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	summary := recoveryActionsFromSweep(t, result)
	if intValue(summary["acted_count"]) != 1 {
		t.Fatalf("acted_count = %v, want 1 (dead-pane active-session queued job must be recovered); %#v", summary["acted_count"], summary)
	}
	acts, _ := summary["actions"].([]map[string]any)
	if len(acts) != 1 || acts[0]["acted"] != true || acts[0]["stalled_owner_closed"] != true {
		t.Fatalf("expected one acted recovery that closed the dead-pane owner; got %#v", summary["actions"])
	}

	// The falsely-active session is now closed so it can never wedge the run again.
	if got := sessionStateOf(t, ctx, runner, repoID, sessionID); got != "closed" {
		t.Fatalf("session state = %q, want closed (the #302 recovery)", got)
	}
	// The job remains claimable: still queued with a pending work message, no lease.
	if got := jobState(t, ctx, runner, repoID, jobID); got != "queued" {
		t.Fatalf("job state = %q, want queued (still claimable)", got)
	}
	if got := messageState(t, ctx, runner, repoID, msgID); got != "pending" {
		t.Fatalf("message state = %q, want pending", got)
	}

	// Convergent: a second sweep with the dead session now closed is a no-op.
	result2, err := SweepRun(ctx, runner, repoID, runID, "")
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if got := intValue(recoveryActionsFromSweep(t, result2)["acted_count"]); got != 0 {
		t.Fatalf("second sweep acted_count = %d, want 0 (convergent)", got)
	}
}
