package mutations

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/agentloop"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
)

// TestReservedExitCodeStaysInLockstepWithAgentloop pins the daemon-side mirror of
// the reserved RFC 0143 Slice A floor exit code (97) to the agentloop authority, so
// the two cannot silently drift (the daemon reads the wrapper's exit code).
func TestReservedExitCodeStaysInLockstepWithAgentloop(t *testing.T) {
	if ExitUnrecoverableAcrossRotation != agentloop.ExitUnrecoverableAcrossRotation {
		t.Fatalf("mutations.ExitUnrecoverableAcrossRotation=%d != agentloop.ExitUnrecoverableAcrossRotation=%d",
			ExitUnrecoverableAcrossRotation, agentloop.ExitUnrecoverableAcrossRotation)
	}
	if ExitUnrecoverableAcrossRotation != 97 {
		t.Fatalf("reserved floor exit code = %d, want 97", ExitUnrecoverableAcrossRotation)
	}
}

// TestRecoverySweepClassifiesDaemonObservedStaleEpochAsUnrecoverableAcrossRotation is
// acceptance criterion (a): a confirmed-dead unsealed lane WITH a LIVE
// daemon.stale_epoch_rotation observation for its session classifies the typed
// session_unrecoverable_across_rotation floor (not the generic agent_exited_unsealed).
func TestRecoverySweepClassifiesDaemonObservedStaleEpochAsUnrecoverableAcrossRotation(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_rot_floor_fires"
	runID, jobID, _, _, sessionID := seedStalledSessionActiveLane(t, ctx, runner, repoID)
	makeConfirmedDeadActiveSessionWithOutput(t, ctx, runner, repoID, runID, sessionID, true)
	stubDeadProbe(t)

	// T1: the daemon observed (and recorded) a stale-epoch rejection for this session.
	if err := RecordStaleEpochRejection(ctx, runner, repoID, runID, sessionID); err != nil {
		t.Fatalf("record stale-epoch observation: %v", err)
	}

	if _, err := SweepRun(ctx, runner, repoID, runID, ""); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := jobLastStallClass(t, ctx, runner, repoID, jobID); got != stallClassSessionUnrecoverableAcrossRotation {
		t.Fatalf("last_stall_class = %q, want %q (T1 fires the floor)", got, stallClassSessionUnrecoverableAcrossRotation)
	}
}

// TestRecoverySweepClassifiesDirectExit97AsUnrecoverableAcrossRotation is the
// T2-direct carrier (acceptance (a)/(e) trusted-direct half): the wrapper's OWN
// supervisor.agent_exited exit_code==97 fires the floor even without a T1 observation.
func TestRecoverySweepClassifiesDirectExit97AsUnrecoverableAcrossRotation(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_rot_floor_direct97"
	runID, jobID, _, _, sessionID := seedStalledSessionActiveLane(t, ctx, runner, repoID)
	makeConfirmedDeadActiveSessionWithOutput(t, ctx, runner, repoID, runID, sessionID, true)
	stubDeadProbe(t)

	// The wrapper's own reap recorded exit 97 on the owning session.
	if _, err := appendEvent(ctx, runner, repoID, runID, "supervisor.agent_exited", sessionID, jobID, nil, nil, nil,
		map[string]any{"exit_code": agentloop.ExitUnrecoverableAcrossRotation}); err != nil {
		t.Fatalf("record agent_exited exit 97: %v", err)
	}

	if _, err := SweepRun(ctx, runner, repoID, runID, ""); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := jobLastStallClass(t, ctx, runner, repoID, jobID); got != stallClassSessionUnrecoverableAcrossRotation {
		t.Fatalf("last_stall_class = %q, want %q (T2-direct exit 97 fires the floor)", got, stallClassSessionUnrecoverableAcrossRotation)
	}
}

// TestOrdinaryUnsealedExitWithNoObservationStaysAgentExitedUnsealed is acceptance
// criterion (c) baseline: an ordinary unsealed dead lane with NO observation and NO
// direct exit 97 stays agent_exited_unsealed — the floor does not over-fire.
func TestOrdinaryUnsealedExitWithNoObservationStaysAgentExitedUnsealed(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_rot_floor_baseline"
	runID, jobID, _, _, sessionID := seedStalledSessionActiveLane(t, ctx, runner, repoID)
	makeConfirmedDeadActiveSessionWithOutput(t, ctx, runner, repoID, runID, sessionID, true)
	stubDeadProbe(t)

	if _, err := SweepRun(ctx, runner, repoID, runID, ""); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := jobLastStallClass(t, ctx, runner, repoID, jobID); got != stallClassAgentExitedUnsealed {
		t.Fatalf("last_stall_class = %q, want %q (no observation -> generic class)", got, stallClassAgentExitedUnsealed)
	}
}

// TestRecoveredSessionClearsStaleEpochObservationThenOrdinaryDeathStaysUnsealed is
// the BINDING Correction 1 (acceptance (b)): a session that recorded a stale-epoch
// observation, then RECONNECTED (superseding it), then dies ORDINARILY must classify
// agent_exited_unsealed — NOT the typed floor. The supersede is what prevents the
// over-fire the v2 falsifier_1 established.
func TestRecoveredSessionClearsStaleEpochObservationThenOrdinaryDeathStaysUnsealed(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_rot_floor_recovered"
	runID, jobID, _, _, sessionID := seedStalledSessionActiveLane(t, ctx, runner, repoID)
	makeConfirmedDeadActiveSessionWithOutput(t, ctx, runner, repoID, runID, sessionID, true)
	stubDeadProbe(t)

	// 1. The daemon observed a stale-epoch rejection for this session.
	if err := RecordStaleEpochRejection(ctx, runner, repoID, runID, sessionID); err != nil {
		t.Fatalf("record stale-epoch observation: %v", err)
	}
	// Sanity: it is LIVE before the recovery.
	if live, err := hasLiveStaleEpochObservation(ctx, runner, repoID, sessionID); err != nil || !live {
		t.Fatalf("expected a LIVE observation before recovery; live=%v err=%v", live, err)
	}
	// 2. The session reconnected across the rotation (presented the current epoch),
	//    superseding the observation (Correction 1).
	if err := RecordStaleEpochRecovered(ctx, runner, repoID, runID, sessionID); err != nil {
		t.Fatalf("record stale-epoch recovery: %v", err)
	}
	if live, err := hasLiveStaleEpochObservation(ctx, runner, repoID, sessionID); err != nil || live {
		t.Fatalf("observation must NOT be live after recovery; live=%v err=%v", live, err)
	}

	// 3. The session later dies ORDINARILY (unsealed) — it must classify generically.
	if _, err := SweepRun(ctx, runner, repoID, runID, ""); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := jobLastStallClass(t, ctx, runner, repoID, jobID); got != stallClassAgentExitedUnsealed {
		t.Fatalf("last_stall_class = %q, want %q (Correction 1: a RECOVERED session's later ordinary death is NOT the floor)", got, stallClassAgentExitedUnsealed)
	}
}

// TestTmuxPaneDeadStatus97WithoutDaemonObservationStaysUnsealed is the A5 / GD-2
// negative (Correction 2): a same-uid `respawn-pane … exit 97` drives only the
// forgeable #{pane_dead_status}==97 — with NO T1 observation and NO direct-path
// exit 97 — and must NOT record the typed class. It stays agent_exited_unsealed.
func TestTmuxPaneDeadStatus97WithoutDaemonObservationStaysUnsealed(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_rot_floor_tmux_forge"
	runID, jobID, _, _, sessionID := seedStalledSessionActiveLane(t, ctx, runner, repoID)
	makeConfirmedDeadActiveSessionWithOutput(t, ctx, runner, repoID, runID, sessionID, true)

	// The pane is dead with #{pane_dead_status}==97 — the forgeable tmux carrier — but
	// there is NO daemon observation and NO wrapper exit 97. Stub the probe to return
	// a dead tmux pane carrying PaneDeadStatus=97.
	forged := agentloop.ExitUnrecoverableAcrossRotation
	restore := probeLaneLiveness
	probeLaneLiveness = func(context.Context, map[string]any, int, string) gosupervisor.LaneLiveness {
		tmux := gosupervisor.TmuxLiveness{
			Class:           gosupervisor.TmuxLivenessPaneDead,
			State:           "lost",
			Healthy:         false,
			ObservedPanePID: 8888,
			PaneDeadStatus:  &forged,
		}
		return gosupervisor.LaneLiveness{Backed: "tmux", Alive: false, Class: string(gosupervisor.TmuxLivenessPaneDead), Tmux: &tmux, ObservedPID: 8888}
	}
	t.Cleanup(func() { probeLaneLiveness = restore })

	if _, err := SweepRun(ctx, runner, repoID, runID, ""); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := jobLastStallClass(t, ctx, runner, repoID, jobID); got != stallClassAgentExitedUnsealed {
		t.Fatalf("last_stall_class = %q, want %q (Correction 2: a bare/forged tmux 97 with no T1 is NOT the floor)", got, stallClassAgentExitedUnsealed)
	}
}

// TestTypedFloorGrantsNoAutoSealAuthority is the BINDING Correction 2 observability-
// only invariant (acceptance (d)): the typed floor routes the IDENTICAL
// finalize-or-escalate path as agent_exited_unsealed and seals NOTHING on its own
// strength. With NO durable published artifact, a rotation-locked lane ESCALATES for
// an operator requeue (the job is not auto-completed) — exactly as the generic class
// would, just with a legible rotation reason. The floor never auto-seals.
func TestTypedFloorGrantsNoAutoSealAuthority(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_rot_floor_no_autoseal"
	runID, jobID, _, _, sessionID := seedStalledSessionActiveLane(t, ctx, runner, repoID)
	makeConfirmedDeadActiveSessionWithOutput(t, ctx, runner, repoID, runID, sessionID, true)
	stubDeadProbe(t)

	// Drive the floor (T1) AND exhaust the unsealed budget so the sweep must choose
	// the escalate path (no durable artifact was published for this fixture job).
	if err := RecordStaleEpochRejection(ctx, runner, repoID, runID, sessionID); err != nil {
		t.Fatalf("record stale-epoch observation: %v", err)
	}
	preseedRequeueBudget(t, ctx, runner, repoID, runID, jobID, 1)

	if _, err := SweepRun(ctx, runner, repoID, runID, ""); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// The typed class was recorded (legible reason)...
	if got := jobLastStallClass(t, ctx, runner, repoID, jobID); got != stallClassSessionUnrecoverableAcrossRotation {
		t.Fatalf("last_stall_class = %q, want %q", got, stallClassSessionUnrecoverableAcrossRotation)
	}
	// ...but the job was NOT auto-sealed/completed by the floor's strength — it
	// escalated for the operator requeue, identical to the agent_exited_unsealed path.
	if got := jobState(t, ctx, runner, repoID, jobID); got == "completed" {
		t.Fatalf("job state = completed: the typed floor must NOT auto-seal (observability-only); it must escalate for an operator requeue")
	}
	_, _, escalation := jobRecoveryCounts(t, ctx, runner, repoID, jobID)
	if !escalation {
		t.Fatalf("escalation_pending = false: the rotation-locked lane with no durable artifact must escalate (no auto-seal)")
	}

	// The escalation reason is legibly the rotation class (acceptance (a) legibility).
	row, err := oneRow(ctx, runner, `
		SELECT payload_json::text AS payload FROM striatumd.escalation_inbox
		 WHERE repository_id = $1 AND run_id = $2`, repoID, runID)
	if err != nil {
		t.Fatalf("read escalation payload: %v", err)
	}
	payload := fmt.Sprint(row["payload"])
	if !strings.Contains(payload, stallClassSessionUnrecoverableAcrossRotation) {
		t.Fatalf("escalation payload missing the typed rotation class: %s", payload)
	}
}

// TestRotationLockedLaneWithFreshLaneUIDLeaseCapabilityResealsDurableArtifact is
// the RFC 0143 Slice B positive path: a rotation-locked lane WITH a durable
// expected artifact only completes when the daemon can prove the owning
// supervisor still holds the active per-lane UID lease/generation.
func TestRotationLockedLaneWithFreshLaneUIDLeaseCapabilityResealsDurableArtifact(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "rot_finalize", true)

	payload := []byte("durable rotation deliverable\n")
	sha := commitWorktreeFile(t, ids.worktreeRoot, "docs/out.txt", string(payload))
	gitRun(t, repoRoot, "update-ref", "refs/heads/"+ids.runBranch, sha)
	seedPublishedArtifact(t, ctx, runner, ids, "art_rot_finalize", "out", "docs/out.txt", payload, nil)
	seedUnsealedPublishedFinalJob308(t, ctx, runner, ids)
	refreshWorkLeaseForRotationReseal(t, ctx, runner, ids, time.Minute)
	seedLaneUIDResealAuthority(t, ctx, runner, ids, 7, 7, "", "")

	// The daemon observed a stale-epoch rejection for this owning session (T1).
	if err := RecordStaleEpochRejection(ctx, runner, ids.repoID, ids.runID, ids.sessionID); err != nil {
		t.Fatalf("record stale-epoch observation: %v", err)
	}

	restore := probeLaneLiveness
	probeLaneLiveness = func(context.Context, map[string]any, int, string) gosupervisor.LaneLiveness {
		return gosupervisor.LaneLiveness{Backed: "tmux", Alive: false, Class: string(gosupervisor.TmuxLivenessPaneDead), ObservedPID: 8888}
	}
	t.Cleanup(func() { probeLaneLiveness = restore })

	result, err := SweepRun(ctx, runner, ids.repoID, ids.runID, "")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// Same finalize path as agent_exited_unsealed: the job completes from the durable
	// artifact (NOT a floor-minted seal), and requeue_count does not climb.
	if got := jobState(t, ctx, runner, ids.repoID, ids.jobID); got != "completed" {
		t.Fatalf("job state = %q, want completed (auto-finalized from durable artifact)", got)
	}
	requeue, _, _ := jobRecoveryCounts(t, ctx, runner, ids.repoID, ids.jobID)
	if requeue != 0 {
		t.Fatalf("requeue_count = %d, want 0", requeue)
	}
	// ...but Slice B records the narrow reseal action, not the generic finalize path.
	summary := recoveryActionsFromSweep(t, result)
	acts, _ := summary["actions"].([]map[string]any)
	action, ok := findRecoveryAction(acts, "capability_reseal")
	if !ok || action["acted"] != true || action["stall_class"] != stallClassSessionUnrecoverableAcrossRotation {
		t.Fatalf("capability_reseal action missing or wrong; actions=%#v", acts)
	}
	resealedEvents := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND event_type = 'recovery.capability_resealed'`, ids.repoID, ids.runID, ids.jobID)
	if resealedEvents != 1 {
		t.Fatalf("recovery.capability_resealed events = %d, want 1", resealedEvents)
	}
}

func TestRotationCapabilityResealRejectsStaleLaneUIDGeneration(t *testing.T) {
	ids, result, runner := sweepRotationDurableArtifactWithAuthority(t, "rot_stale_generation", func(ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs) {
		refreshWorkLeaseForRotationReseal(t, ctx, runner, ids, time.Minute)
		seedLaneUIDResealAuthority(t, ctx, runner, ids, 7, 8, "", "")
	})
	assertRotationResealUnavailable(t, result, "lane_uid_generation_mismatch")
	assertRotationNotCompleted(t, runner, ids, "stale generation")
}

func TestRotationCapabilityResealRejectsSiblingLaneUIDLeaseReplay(t *testing.T) {
	ids, result, runner := sweepRotationDurableArtifactWithAuthority(t, "rot_sibling_replay", func(ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs) {
		refreshWorkLeaseForRotationReseal(t, ctx, runner, ids, time.Minute)
		seedLaneUIDResealAuthority(t, ctx, runner, ids, 7, 7, "sess_sibling_replay", "sup_sibling_replay")
	})
	assertRotationResealUnavailable(t, result, "lane_uid_generation_mismatch")
	assertRotationNotCompleted(t, runner, ids, "sibling uid lease replay")
}

func TestRotationCapabilityResealRejectsForeignRunWorkLease(t *testing.T) {
	ids, result, runner := sweepRotationDurableArtifactWithAuthority(t, "rot_work_lease_foreign_run", func(ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs) {
		refreshWorkLeaseForRotationReseal(t, ctx, runner, ids, time.Minute)
		seedLaneUIDResealAuthority(t, ctx, runner, ids, 7, 7, "", "")
		foreignRunID := ids.runID + "_foreign"
		seedForeignRunForWorkLeaseRetarget(t, ctx, runner, ids, foreignRunID)
		if err := runner.Exec(ctx, `
			UPDATE striatumd.leases
			   SET run_id = $3
			 WHERE repository_id = $1 AND lease_id = $2`,
			ids.repoID, ids.leaseID, foreignRunID); err != nil {
			t.Fatalf("retarget work lease run: %v", err)
		}
	})
	assertRotationResealUnavailable(t, result, "reseal_work_lease_run_mismatch")
	assertRotationNotCompleted(t, runner, ids, "foreign-run work lease")
}

func TestRotationCapabilityResealRejectsForeignRunLaneUIDLease(t *testing.T) {
	ids, result, runner := sweepRotationDurableArtifactWithAuthority(t, "rot_uid_lease_foreign_run", func(ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs) {
		refreshWorkLeaseForRotationReseal(t, ctx, runner, ids, time.Minute)
		uidLeaseID := seedLaneUIDResealAuthority(t, ctx, runner, ids, 7, 7, "", "")
		if err := runner.Exec(ctx, `
			UPDATE striatumd.lane_uid_leases
			   SET run_id = $3
			 WHERE repository_id = $1 AND lease_id = $2`,
			ids.repoID, uidLeaseID, ids.runID+"_foreign"); err != nil {
			t.Fatalf("retarget lane uid lease run: %v", err)
		}
	})
	assertRotationResealUnavailable(t, result, "lane_uid_lease_run_mismatch")
	assertRotationNotCompleted(t, runner, ids, "foreign-run lane uid lease")
}

func TestRotationCapabilityResealBeyondGraceKeepsTypedFloor(t *testing.T) {
	ids, result, runner := sweepRotationDurableArtifactWithAuthority(t, "rot_grace_expired", func(ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs) {
		expireWorkLeaseForRotationReseal(t, ctx, runner, ids, -2*time.Minute)
		seedLaneUIDResealAuthority(t, ctx, runner, ids, 7, 7, "", "")
	})
	assertRotationResealUnavailable(t, result, "reseal_grace_expired")
	assertRotationNotCompleted(t, runner, ids, "expired reseal grace")
}

func TestRotationCapabilityResealUsesExpectedArtifactsOnly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "rot_unexpected_artifact", true)

	payload := []byte("wrong-path rotation deliverable\n")
	sha := commitWorktreeFile(t, ids.worktreeRoot, "docs/wrong.txt", string(payload))
	gitRun(t, repoRoot, "update-ref", "refs/heads/"+ids.runBranch, sha)
	seedPublishedArtifact(t, ctx, runner, ids, "art_rot_wrong", "wrong", "docs/wrong.txt", payload, nil)
	seedUnsealedPublishedFinalJob308(t, ctx, runner, ids)
	refreshWorkLeaseForRotationReseal(t, ctx, runner, ids, time.Minute)
	seedLaneUIDResealAuthority(t, ctx, runner, ids, 7, 7, "", "")
	if err := RecordStaleEpochRejection(ctx, runner, ids.repoID, ids.runID, ids.sessionID); err != nil {
		t.Fatalf("record stale-epoch observation: %v", err)
	}
	stubRotationDeadProbe(t)

	result, err := SweepRun(ctx, runner, ids.repoID, ids.runID, "")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := jobState(t, ctx, runner, ids.repoID, ids.jobID); got == "completed" {
		t.Fatalf("job state = completed: reseal must use daemon expected artifacts, not unexpected lane input; recovery_actions=%#v", result["recovery_actions"])
	}
	summary := recoveryActionsFromSweep(t, result)
	acts, _ := summary["actions"].([]map[string]any)
	if action, ok := findRecoveryAction(acts, "capability_reseal"); ok && action["acted"] == true {
		t.Fatalf("capability_reseal acted with only an unexpected artifact path; actions=%#v", acts)
	}
}

func sweepRotationDurableArtifactWithAuthority(t *testing.T, suffix string, configure func(context.Context, db.Runner, worktreeRequiredFixtureIDs)) (worktreeRequiredFixtureIDs, map[string]any, db.Runner) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, suffix, true)

	payload := []byte("durable rotation deliverable\n")
	sha := commitWorktreeFile(t, ids.worktreeRoot, "docs/out.txt", string(payload))
	gitRun(t, repoRoot, "update-ref", "refs/heads/"+ids.runBranch, sha)
	seedPublishedArtifact(t, ctx, runner, ids, "art_"+suffix, "out", "docs/out.txt", payload, nil)
	seedUnsealedPublishedFinalJob308(t, ctx, runner, ids)
	configure(ctx, runner, ids)
	if err := RecordStaleEpochRejection(ctx, runner, ids.repoID, ids.runID, ids.sessionID); err != nil {
		t.Fatalf("record stale-epoch observation: %v", err)
	}
	stubRotationDeadProbe(t)

	result, err := SweepRun(ctx, runner, ids.repoID, ids.runID, "")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	return ids, result, runner
}

func seedLaneUIDResealAuthority(t *testing.T, ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs, leaseGeneration, metadataGeneration int64, leaseSessionID, leaseSupervisorID string) string {
	t.Helper()
	if leaseSessionID == "" {
		leaseSessionID = ids.sessionID
	}
	if leaseSupervisorID == "" {
		leaseSupervisorID = "sup_" + ids.sessionID
	}
	uidLeaseID := "luid_" + ids.sessionID
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.lane_uid_leases (
		  repository_id, lease_id, pool_uid, pool_user, generation,
		  run_id, session_id, supervisor_id, state, leased_at
		) VALUES ($1,$2,65042,'lane-pool-rfc0143', $3, $4, $5, $6, 'active', $7)`,
		ids.repoID, uidLeaseID, leaseGeneration, ids.runID, leaseSessionID, leaseSupervisorID, now); err != nil {
		t.Fatalf("insert lane uid lease: %v", err)
	}
	metadataJSON := fmt.Sprintf(`{"lane_uid_lease_id":%q,"lane_uid":65042,"lane_uid_generation":%d}`, uidLeaseID, metadataGeneration)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.process_supervisor_pointers
		   SET metadata_json = metadata_json || $3::jsonb
		 WHERE repository_id = $1 AND session_id = $2`,
		ids.repoID, ids.sessionID, metadataJSON); err != nil {
		t.Fatalf("update supervisor metadata: %v", err)
	}
	return uidLeaseID
}

func seedForeignRunForWorkLeaseRetarget(t *testing.T, ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs, foreignRunID string) {
	t.Helper()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.runs (
		  repository_id, run_id, workflow_snapshot_id, repo_root, state,
		  branch_name, branch_base, branch_confirmed_at, branch_confirmed_by, created_at
		)
		SELECT repository_id, $2, workflow_snapshot_id, repo_root, state,
		       branch_name || '-foreign', branch_base, branch_confirmed_at, branch_confirmed_by, created_at
		  FROM striatumd.runs
		 WHERE repository_id = $1 AND run_id = $3`,
		ids.repoID, foreignRunID, ids.runID); err != nil {
		t.Fatalf("insert foreign run for work lease retarget: %v", err)
	}
}

func refreshWorkLeaseForRotationReseal(t *testing.T, ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs, ttl time.Duration) {
	t.Helper()
	expiresAt := time.Now().UTC().Add(ttl)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'active', expires_at = $3, released_at = NULL, release_reason = NULL
		 WHERE repository_id = $1 AND lease_id = $2`,
		ids.repoID, ids.leaseID, expiresAt); err != nil {
		t.Fatalf("refresh work lease: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET current_lease_id = $3
		 WHERE repository_id = $1 AND job_id = $2`,
		ids.repoID, ids.jobID, ids.leaseID); err != nil {
		t.Fatalf("restore job lease pointer: %v", err)
	}
}

func expireWorkLeaseForRotationReseal(t *testing.T, ctx context.Context, runner db.Runner, ids worktreeRequiredFixtureIDs, age time.Duration) {
	t.Helper()
	expiresAt := time.Now().UTC().Add(age)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'expired', expires_at = $3, released_at = $3, release_reason = 'expired'
		 WHERE repository_id = $1 AND lease_id = $2`,
		ids.repoID, ids.leaseID, expiresAt); err != nil {
		t.Fatalf("expire work lease: %v", err)
	}
}

func assertRotationResealUnavailable(t *testing.T, result map[string]any, reason string) {
	t.Helper()
	summary := recoveryActionsFromSweep(t, result)
	acts, _ := summary["actions"].([]map[string]any)
	action, ok := findRecoveryAction(acts, "capability_reseal_unavailable")
	if !ok {
		t.Fatalf("capability_reseal_unavailable action missing; actions=%#v", acts)
	}
	if action["reason"] != reason {
		t.Fatalf("capability_reseal_unavailable reason = %#v, want %q; action=%#v", action["reason"], reason, action)
	}
	if action["stall_class"] != stallClassSessionUnrecoverableAcrossRotation {
		t.Fatalf("stall_class = %#v, want %q", action["stall_class"], stallClassSessionUnrecoverableAcrossRotation)
	}
}

func assertRotationNotCompleted(t *testing.T, runner db.Runner, ids worktreeRequiredFixtureIDs, reason string) {
	t.Helper()
	ctx := context.Background()
	if got := jobState(t, ctx, runner, ids.repoID, ids.jobID); got == "completed" {
		t.Fatalf("job state = completed: %s must fail closed", reason)
	}
	if got := jobLastStallClass(t, ctx, runner, ids.repoID, ids.jobID); got != stallClassSessionUnrecoverableAcrossRotation {
		t.Fatalf("last_stall_class = %q, want %q", got, stallClassSessionUnrecoverableAcrossRotation)
	}
	resealedEvents := scalarInt(t, ctx, runner, `
		SELECT count(*) FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND event_type = 'recovery.capability_resealed'`, ids.repoID, ids.runID, ids.jobID)
	if resealedEvents != 0 {
		t.Fatalf("recovery.capability_resealed events = %d, want 0", resealedEvents)
	}
}

func findRecoveryAction(actions []map[string]any, name string) (map[string]any, bool) {
	for _, action := range actions {
		if action["action"] == name {
			return action, true
		}
	}
	return nil, false
}

func stubRotationDeadProbe(t *testing.T) {
	t.Helper()
	restore := probeLaneLiveness
	probeLaneLiveness = func(context.Context, map[string]any, int, string) gosupervisor.LaneLiveness {
		return gosupervisor.LaneLiveness{Backed: "tmux", Alive: false, Class: string(gosupervisor.TmuxLivenessPaneDead), ObservedPID: 8888}
	}
	t.Cleanup(func() { probeLaneLiveness = restore })
}
