package mutations

import (
	"context"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
)

// pointerStateOf returns the most-recent supervisor pointer state for a session.
func pointerStateOf(t *testing.T, ctx context.Context, runner db.Runner, repoID, sessionID string) string {
	t.Helper()
	row, err := oneRow(ctx, runner, `
		SELECT state FROM striatumd.process_supervisor_pointers
		 WHERE repository_id = $1 AND session_id = $2
		 ORDER BY updated_at DESC, supervisor_id DESC
		 LIMIT 1`, repoID, sessionID)
	if err != nil {
		t.Fatalf("read pointer state: %v", err)
	}
	if row["state"] == nil {
		return ""
	}
	return row["state"].(string)
}

// retargetSupervisorPointerState rewrites the supervisor pointer state for the
// #302 seed so a single seeded scenario can model both the "no durable signal"
// residual (pointer still attached, no readable probe) and the durable-signal
// fix (pointer recorded 'stopped' at clean exit).
func retargetSupervisorPointerState(t *testing.T, ctx context.Context, runner db.Runner, repoID, sessionID, state string) {
	t.Helper()
	if err := runner.Exec(ctx, `
		UPDATE striatumd.process_supervisor_pointers
		   SET state = $3
		 WHERE repository_id = $1 AND session_id = $2`,
		repoID, sessionID, state); err != nil {
		t.Fatalf("retarget pointer state: %v", err)
	}
}

// TestSweep302NoSignalResidualUnrecoveredThenRecoveredByDurableSignal is the
// #302 RESIDUAL (deferred by PR #318): a queued job behind a released prior-claim
// lease whose owner session stays state='active' with fresh frozen activity, and
// where the supervisor probe is NO LONGER READABLE (the pane died / the helper
// was torn down, so a live tmux/PID probe returns tmux_unavailable). With no
// unforgeable dead signal, supervisedAgentConfirmedDead returns false, the
// session reads as possibly-live, and the sweep does NOT reclaim the queued job —
// the run wedges until a manual `session close --requeue-job`.
//
// The fix records a DURABLE clean-exit signal at idle exit (work.await_packet ->
// no_work / exit_session) by stamping the lane's supervisor pointer 'stopped'
// BEFORE the pane/helper goes away. supervisedAgentConfirmedDead reads that
// recorded 'stopped' pointer as conclusive (an unforgeable recorded-exit fact)
// and the sweep reclaims the session + leaves the job claimable — even though no
// live probe is possible.
//
// The single test exercises both halves against the SAME seed so the residual is
// provably the unrecovered case and the durable signal is provably what flips it:
//   - PHASE A (residual, pre-fix behavior): pointer 'attached' + UNAVAILABLE probe
//     -> NOT recovered (acted_count == 0). This is the wedge #318 deferred.
//   - PHASE B (durable signal): the SAME pointer now recorded 'stopped' (what the
//     await-packet idle-exit chokepoint writes) + the SAME unavailable probe ->
//     recovered (acted_count == 1, session closed, job still claimable).
func TestSweep302NoSignalResidualUnrecoveredThenRecoveredByDurableSignal(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_302_no_signal_residual"

	runID, jobID, msgID, sessionID, _ := seedDeadPaneActiveSession302(t, ctx, runner, repoID)

	// The supervisor probe is UNAVAILABLE for the whole test: the pane is gone /
	// helper torn down, so a live tmux probe can no longer judge the lane. This is
	// the exact #302 residual condition — no unforgeable LIVE-probe dead signal.
	restore := probeLaneLiveness
	probeLaneLiveness = func(context.Context, map[string]any, int, string) gosupervisor.LaneLiveness {
		return gosupervisor.LaneLiveness{Backed: "tmux", Alive: false, Class: string(gosupervisor.TmuxLivenessUnavailable)}
	}
	t.Cleanup(func() { probeLaneLiveness = restore })

	// PHASE A — the residual: pointer is non-terminal ('attached') and the probe is
	// unavailable, so there is NO unforgeable dead signal. The sweep must NOT
	// reclaim it (acting here would re-introduce the #145/#147 false-requeue class).
	resA, err := SweepRun(ctx, runner, repoID, runID, "")
	if err != nil {
		t.Fatalf("phase-A sweep: %v", err)
	}
	if got := intValue(recoveryActionsFromSweep(t, resA)["acted_count"]); got != 0 {
		t.Fatalf("phase-A acted_count = %d, want 0 (no unforgeable dead signal — the #302 residual must NOT be recovered on an unavailable probe alone)", got)
	}
	if got := sessionStateOf(t, ctx, runner, repoID, sessionID); got != "active" {
		t.Fatalf("phase-A session state = %q, want active (still wedged — the residual)", got)
	}

	// PHASE B — the durable signal: the lane's clean idle-exit recorded the pointer
	// 'stopped' (what recordCleanLaneExitSignal writes from the await-packet
	// chokepoint). The probe is STILL unavailable, but the recorded terminal
	// pointer is an unforgeable dead signal, so the sweep now reclaims it.
	retargetSupervisorPointerState(t, ctx, runner, repoID, sessionID, "stopped")

	resB, err := SweepRun(ctx, runner, repoID, runID, "")
	if err != nil {
		t.Fatalf("phase-B sweep: %v", err)
	}
	summary := recoveryActionsFromSweep(t, resB)
	if intValue(summary["acted_count"]) != 1 {
		t.Fatalf("phase-B acted_count = %v, want 1 (the durable 'stopped' pointer is an unforgeable dead signal and must drive recovery); %#v", summary["acted_count"], summary)
	}
	acts, _ := summary["actions"].([]map[string]any)
	if len(acts) != 1 || acts[0]["acted"] != true || acts[0]["stalled_owner_closed"] != true {
		t.Fatalf("phase-B expected one acted recovery that closed the exited owner; got %#v", summary["actions"])
	}
	if got := sessionStateOf(t, ctx, runner, repoID, sessionID); got != "closed" {
		t.Fatalf("phase-B session state = %q, want closed (the #302 residual recovery)", got)
	}
	if got := jobState(t, ctx, runner, repoID, jobID); got != "queued" {
		t.Fatalf("phase-B job state = %q, want queued (still claimable)", got)
	}
	if got := messageState(t, ctx, runner, repoID, msgID); got != "pending" {
		t.Fatalf("phase-B message state = %q, want pending", got)
	}
}

// TestSweep302DoesNotReclaimAmbiguousProbeWithoutTerminalSignal is the SAFETY
// invariant: a genuinely possibly-live lane whose supervisor probe is transiently
// UNREADABLE (tmux_unavailable / pid_identity_unavailable) and that has NO
// recorded terminal pointer state must STILL be treated as possibly-live and left
// alone. This is the #145/#147 false-requeue guard: only the RECORDED terminal
// exit counts as unforgeable; an ambiguous probe alone never reclaims an active
// session. (PHASE A above already proves this for tmux_unavailable; this case
// pins the pid_identity_unavailable variant — kill(pid,0) succeeded, the process
// is alive, /proc was momentarily unreadable — explicitly, since that is the
// exact #145 signal that force-expired a LIVE agent's lease.)
func TestSweep302DoesNotReclaimAmbiguousProbeWithoutTerminalSignal(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_302_ambiguous_probe_safety"

	runID, jobID, _, sessionID, _ := seedDeadPaneActiveSession302(t, ctx, runner, repoID)

	// The pointer is NON-terminal (attached) — no recorded clean exit — and the
	// probe is pid_identity_unavailable: kill(pid,0) succeeded (alive) but /proc was
	// momentarily unreadable. Treating that as dead is the #145/#147 false-requeue.
	restore := probeLaneLiveness
	probeLaneLiveness = func(context.Context, map[string]any, int, string) gosupervisor.LaneLiveness {
		return gosupervisor.LaneLiveness{Backed: "pid", Alive: false, Class: gosupervisor.PIDLivenessIdentityUnavailable, ObservedPID: 7777}
	}
	t.Cleanup(func() { probeLaneLiveness = restore })

	res, err := SweepRun(ctx, runner, repoID, runID, "")
	if err != nil {
		t.Fatalf("safety sweep: %v", err)
	}
	if got := intValue(recoveryActionsFromSweep(t, res)["acted_count"]); got != 0 {
		t.Fatalf("acted_count = %d, want 0 (#145/#147 guard: an ambiguous probe with NO recorded terminal exit must NOT reclaim a possibly-live active session)", got)
	}
	if got := sessionStateOf(t, ctx, runner, repoID, sessionID); got != "active" {
		t.Fatalf("session state = %q, want active (the live lane must be left alone)", got)
	}
	if got := jobState(t, ctx, runner, repoID, jobID); got != "queued" {
		t.Fatalf("job state = %q, want queued (untouched)", got)
	}
}

// TestAwaitPacketRecordsDurableCleanExitSignalOnIdleExit proves the producing
// half of the fix end-to-end: when the daemon returns the idle-exit envelope
// (work.await_packet -> no_work / exit_session) the await-packet handler stamps
// the lane's supervisor pointer 'stopped' (the durable, recorded-exit dead
// signal) BEFORE the pane/helper tears down — while leaving the SESSION row
// active so the sweep remains the component that closes it (the #291/#302
// queued-scan acts only on an active bound session). After the signal is
// recorded, an unavailable-probe sweep reclaims the session, closing the
// producer→consumer loop the fix establishes.
func TestAwaitPacketRecordsDurableCleanExitSignalOnIdleExit(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_302_await_records_signal"
	runID := "run_" + repoID
	sessionID := "sess_" + repoID

	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{
		"workflow_id": "wf",
		"roles":       map[string]any{"reviewer": map[string]any{}},
		"lanes":       map[string]any{"claude": map[string]any{"display_model": "Claude"}},
	})
	intgSeedSession(t, ctx, runner, repoID, runID, sessionID, "reviewer", "claude", []string{"write"}, "active")
	// intgAttest seeds an 'attached' supervisor + pointer so awaitSessionBackendReady
	// reports the backend live, and the pointer is the non-terminal row the durable
	// signal must flip.
	intgAttest(t, ctx, runner, repoID, runID, sessionID, "claude")

	// Stub claim to report no work, and run the await loop fast to its deadline so
	// the genuine no-work idle-exit envelope (not transient load) is returned.
	restoreClaim := awaitClaimNext
	restoreTimeout := awaitPacketTimeout
	restorePoll := awaitPacketPollInterval
	awaitClaimNext = func(context.Context, db.Runner, rpc.Envelope) (map[string]any, error) {
		return map[string]any{"status": "no_work"}, nil
	}
	awaitPacketTimeout = 30 * time.Millisecond
	awaitPacketPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		awaitClaimNext = restoreClaim
		awaitPacketTimeout = restoreTimeout
		awaitPacketPollInterval = restorePoll
	})

	// Precondition: the pointer is attached (non-terminal) before the idle exit.
	if got := pointerStateOf(t, ctx, runner, repoID, sessionID); got != "attached" {
		t.Fatalf("precondition pointer state = %q, want attached", got)
	}

	res, err := HandleAwaitPacket(ctx, runner, intgEnv(repoID, map[string]any{"session_id": sessionID}))
	if err != nil {
		t.Fatalf("await_packet: %v", err)
	}
	if got := res["status"]; got != "no_work" {
		t.Fatalf("status = %v, want no_work", got)
	}
	if got := res["idle_behavior"]; got != "exit_session" {
		t.Fatalf("idle_behavior = %v, want exit_session", got)
	}

	// The durable clean-exit signal is recorded: the pointer is now 'stopped'.
	if got := pointerStateOf(t, ctx, runner, repoID, sessionID); got != "stopped" {
		t.Fatalf("pointer state after idle exit = %q, want stopped (the durable dead signal must be recorded)", got)
	}
	// The SESSION is intentionally left active — the sweep, not the await handler,
	// closes it (so the #291/#302 queued-scan can act on the active bound session).
	if got := sessionStateOf(t, ctx, runner, repoID, sessionID); got != "active" {
		t.Fatalf("session state after idle exit = %q, want active (only the pointer is marked terminal)", got)
	}
}
