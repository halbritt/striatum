package mutations

import (
	"context"
	"testing"

	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// RFC 0138 (#453) — opt-in terminal-gap recovery for a STRICT fan-in barrier with a
// permanently-unrecoverable REQUIRED seat. These fixtures fence the Option B
// behavior: the sealed gap is admitted ONLY for an opted-in barrier, ONLY for a
// PROVABLY-DEAD seat (the SAME supervisedAgentConfirmedDead oracle the quorum path
// reuses, not a new liveness check), and ONLY within the declared budget — and a
// merely-slow seat is never a gap. Composes with RFC 0135's is_terminal_gap
// predicate disjunct (no predicate fork; TestBarrierPredicateHasNoRefCount stays
// green).

// TestStrictFaninDefaultDoesNotFireOnDeadRequiredSeat is RFC 0138 AC#1 (the FMA-003
// reproduction): a strict fan-in barrier with NO sealed-gap opt-in, with one
// declared required seat permanently dead (supervisedAgentConfirmedDead true), with
// no completed job and no quarantine — STAYS BLOCKED. BarrierReadySQL returns FALSE.
// No automatic fire. This is the default (and the only behavior when the opt-in is
// off).
func TestStrictFaninDefaultDoesNotFireOnDeadRequiredSeat(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_fanin_gap_default"
	runID := "run_fanin_gap_default"
	barrierID := "barrier_fanin_gap_default"
	seatLive := "author_live"
	seatDead := "author_dead"

	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{"workflow_id": "wf"})

	// The live seat completes at attempt 1; the dead seat has only an UNCOMPLETED
	// running job (no completed attempt, no quarantine) whose lane is provably dead.
	intgSeedSession(t, ctx, runner, repoID, runID, "sess_live", "implementer", "lane_live", nil, "active")
	intgSeedSession(t, ctx, runner, repoID, runID, "sess_dead", "implementer", "lane_dead", nil, "active")
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_live1", seatLive, "completed", 1)
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_dead1", seatDead, "running", 1)
	bindLeaseToJob(t, ctx, runner, repoID, runID, "job_dead1", "sess_dead", "lease_dead")
	seedSupervisorPointer(t, ctx, runner, repoID, runID, "sess_dead", "lost", 0)

	tx := beginBarrierTx(t, ctx, pool)
	// NO opt-in: fanin_tolerates_sealed_gap stays false (default).
	if err := recordFaninFreezePoint(ctx, tx, repoID, faninFreezePoint{
		BarrierID:               barrierID,
		RunID:                   runID,
		DownstreamWorkflowJobID: "synth",
		FrozenTipSHA:            "0000000000000000000000000000000000000000",
		DeclaredSiblingJobIDs:   []string{seatLive, seatDead},
	}); err != nil {
		t.Fatalf("record freeze point: %v", err)
	}
	stageRow(t, ctx, tx, repoID, barrierID, runID, seatLive, "job_live1", 1)

	ready, err := faninBarrierReady(ctx, tx, repoID, barrierID)
	if err != nil {
		t.Fatalf("readiness (dead required seat, opt-in OFF): %v", err)
	}
	if ready {
		t.Fatal("AC#1: a strict fan-in with a provably-dead REQUIRED seat must NOT fire when the sealed-gap opt-in is OFF (it parks in needs_operator) — the default is unchanged")
	}
	_ = tx.Rollback(ctx)
}

// TestStrictFaninOptInFiresOnlyForProvablyDeadSeat is RFC 0138 AC#3: with
// fanin_tolerates_sealed_gap=true and max_sealed_gaps=1, a PROVABLY-DEAD required
// seat is admitted as a terminal gap and the barrier fires; a merely-SLOW (not
// provably dead) required seat does NOT fire (silence != consent — the D214b axiom
// carries over). It parameterizes the seat's liveness oracle and asserts fire iff
// dead.
func TestStrictFaninOptInFiresOnlyForProvablyDeadSeat(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_fanin_gap_optin"
	runID := "run_fanin_gap_optin"
	barrierID := "barrier_fanin_gap_optin"
	seatLive := "author_live"
	seatDead := "author_dead"

	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{"workflow_id": "wf"})

	intgSeedSession(t, ctx, runner, repoID, runID, "sess_live", "implementer", "lane_live", nil, "active")
	intgSeedSession(t, ctx, runner, repoID, runID, "sess_dead", "implementer", "lane_dead", nil, "active")
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_live1", seatLive, "completed", 1)
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_dead1", seatDead, "running", 1)
	bindLeaseToJob(t, ctx, runner, repoID, runID, "job_dead1", "sess_dead", "lease_dead")

	tx := beginBarrierTx(t, ctx, pool)
	// OPT IN: tolerate one sealed gap.
	if err := recordFaninFreezePoint(ctx, tx, repoID, faninFreezePoint{
		BarrierID:               barrierID,
		RunID:                   runID,
		DownstreamWorkflowJobID: "synth",
		FrozenTipSHA:            "0000000000000000000000000000000000000000",
		DeclaredSiblingJobIDs:   []string{seatLive, seatDead},
		ToleratesSealedGap:      true,
		MaxSealedGaps:           1,
	}); err != nil {
		t.Fatalf("record freeze point: %v", err)
	}
	stageRow(t, ctx, tx, repoID, barrierID, runID, seatLive, "job_live1", 1)

	// The dead seat's lane is ATTACHED (not provably dead) → a merely-slow seat →
	// silence is not consent → the barrier must NOT fire even with the opt-in.
	seedSupervisorPointer(t, ctx, runner, repoID, runID, "sess_dead", "attached", 0)
	if ready, err := faninBarrierReady(ctx, tx, repoID, barrierID); err != nil {
		t.Fatalf("readiness (slow seat, opt-in ON): %v", err)
	} else if ready {
		t.Fatal("AC#3: a merely-SLOW required seat must NOT be sealed as a gap even with the opt-in (silence != consent)")
	}

	// Now the seat's lane is provably dead (a `lost` pointer is conclusive). It
	// becomes a sealed terminal gap within budget 1 → the barrier fires DEGRADED.
	retargetSupervisorPointerState(t, ctx, runner, repoID, "sess_dead", "lost")
	if ready, err := faninBarrierReady(ctx, tx, repoID, barrierID); err != nil {
		t.Fatalf("readiness (provably-dead seat, opt-in ON): %v", err)
	} else if !ready {
		t.Fatal("AC#3: a PROVABLY-DEAD required seat within budget must be admitted as a sealed gap so the barrier fires degraded")
	}
	_ = tx.Rollback(ctx)
}

// TestStrictFaninOptInBudgetBoundsTheGap is RFC 0138 AC#4: with max_sealed_gaps=1
// and TWO provably-dead required seats, the barrier does NOT fire — the second dead
// seat exceeds the budget and stays blocking (mirroring resolvePanelSeats'
// beyond-budget rule). The author declared how short a join is tolerable; the second
// dead leg exceeds it.
func TestStrictFaninOptInBudgetBoundsTheGap(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_fanin_gap_budget"
	runID := "run_fanin_gap_budget"
	barrierID := "barrier_fanin_gap_budget"
	seatLive := "author_live"
	seatDeadA := "author_dead_a"
	seatDeadB := "author_dead_b"

	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{"workflow_id": "wf"})

	intgSeedSession(t, ctx, runner, repoID, runID, "sess_live", "implementer", "lane_live", nil, "active")
	intgSeedSession(t, ctx, runner, repoID, runID, "sess_dead_a", "implementer", "lane_dead_a", nil, "active")
	intgSeedSession(t, ctx, runner, repoID, runID, "sess_dead_b", "implementer", "lane_dead_b", nil, "active")
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_live1", seatLive, "completed", 1)
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_dead_a1", seatDeadA, "running", 1)
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_dead_b1", seatDeadB, "running", 1)
	bindLeaseToJob(t, ctx, runner, repoID, runID, "job_dead_a1", "sess_dead_a", "lease_dead_a")
	bindLeaseToJob(t, ctx, runner, repoID, runID, "job_dead_b1", "sess_dead_b", "lease_dead_b")
	seedSupervisorPointer(t, ctx, runner, repoID, runID, "sess_dead_a", "lost", 0)
	seedSupervisorPointer(t, ctx, runner, repoID, runID, "sess_dead_b", "lost", 0)

	tx := beginBarrierTx(t, ctx, pool)
	if err := recordFaninFreezePoint(ctx, tx, repoID, faninFreezePoint{
		BarrierID:               barrierID,
		RunID:                   runID,
		DownstreamWorkflowJobID: "synth",
		FrozenTipSHA:            "0000000000000000000000000000000000000000",
		DeclaredSiblingJobIDs:   []string{seatLive, seatDeadA, seatDeadB},
		ToleratesSealedGap:      true,
		MaxSealedGaps:           1, // only ONE gap tolerated
	}); err != nil {
		t.Fatalf("record freeze point: %v", err)
	}
	stageRow(t, ctx, tx, repoID, barrierID, runID, seatLive, "job_live1", 1)

	if ready, err := faninBarrierReady(ctx, tx, repoID, barrierID); err != nil {
		t.Fatalf("readiness (two dead seats, budget 1): %v", err)
	} else if ready {
		t.Fatal("AC#4: two provably-dead required seats exceed a budget of 1 — the barrier must NOT fire (the budget is a ceiling on tolerated incompleteness)")
	}
	_ = tx.Rollback(ctx)
}

// TestStrictFaninSealedGapAppearsInManifestWithDamageCode is part of RFC 0138 AC#5
// (completeness is never silently forged): when the barrier fires with a sealed gap,
// the join_manifest.v1 records the dead seat with status=terminal_gap and a
// damage_code (required_seat_unrecoverable) — NOT staged_live — so a downstream
// reader can always tell a complete join from a degraded one.
func TestStrictFaninSealedGapAppearsInManifestWithDamageCode(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_fanin_gap_manifest"
	runID := "run_fanin_gap_manifest"
	barrierID := "barrier_fanin_gap_manifest"
	seatLive := "author_live"
	seatDead := "author_dead"

	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{"workflow_id": "wf"})

	intgSeedSession(t, ctx, runner, repoID, runID, "sess_live", "implementer", "lane_live", nil, "active")
	intgSeedSession(t, ctx, runner, repoID, runID, "sess_dead", "implementer", "lane_dead", nil, "active")
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_live1", seatLive, "completed", 1)
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_dead1", seatDead, "running", 1)
	bindLeaseToJob(t, ctx, runner, repoID, runID, "job_dead1", "sess_dead", "lease_dead")
	seedSupervisorPointer(t, ctx, runner, repoID, runID, "sess_dead", "lost", 0)

	tx := beginBarrierTx(t, ctx, pool)
	if err := recordFaninFreezePoint(ctx, tx, repoID, faninFreezePoint{
		BarrierID:               barrierID,
		RunID:                   runID,
		DownstreamWorkflowJobID: "synth",
		FrozenTipSHA:            "0000000000000000000000000000000000000000",
		DeclaredSiblingJobIDs:   []string{seatLive, seatDead},
		ToleratesSealedGap:      true,
		MaxSealedGaps:           1,
	}); err != nil {
		t.Fatalf("record freeze point: %v", err)
	}
	stageRow(t, ctx, tx, repoID, barrierID, runID, seatLive, "job_live1", 1)

	if ready, err := faninBarrierReady(ctx, tx, repoID, barrierID); err != nil || !ready {
		t.Fatalf("barrier should fire degraded with one sealed gap: ready=%v err=%v", ready, err)
	}

	edges, err := faninBarrierManifest(ctx, tx, repoID, barrierID)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	bySeat := map[string]faninManifestInEdge{}
	for _, e := range edges {
		bySeat[e.EntityID] = e
	}
	live, ok := bySeat[seatLive]
	if !ok || live.Status != "staged_live" {
		t.Fatalf("live seat must be staged_live in the manifest, got %+v", live)
	}
	dead, ok := bySeat[seatDead]
	if !ok {
		t.Fatalf("dead seat missing from manifest")
	}
	if dead.Status != "terminal_gap" {
		t.Fatalf("AC#5: the sealed-gap seat must be recorded with status=terminal_gap (not staged_live / not silently dropped), got %q", dead.Status)
	}
	if dead.DamageCode != "required_seat_unrecoverable" {
		t.Fatalf("AC#5: the sealed-gap seat must carry damage_code=required_seat_unrecoverable, got %q", dead.DamageCode)
	}
	_ = tx.Rollback(ctx)
}
