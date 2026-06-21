package mutations

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
)

// RFC 0133 fan-in barrier cutover — the LIVE FAN-OUT seam (#527, the last leg of
// #354). This is the missing production caller of recordFaninFreezePoint that D246's
// revisit trigger names: "the wiring of recordFaninFreezePoint into a live fan-out
// path so a real fan-in run exercises the barrier end-to-end". With this wired, the
// staging-at-completion hook (stageFaninContributionAtCompletion, worktree.go) and
// the barrier_assembly dispatcher (DispatchBarrierAssembly, barrier_fanin_dispatch.go)
// finally have a freeze record to key on — they stage and assemble for a run that
// declared a fan-in, instead of no-oping on every run because no run declares one.
//
// CUTOVER DISCIPLINE (RFC 0133 "Migration and rollout", D233/D246). This stays
// SHADOW: it is a strict NO-OP unless the fan-in fold is opted in
// (STRIATUM_BARRIER_FANIN=1, barrierFaninAssemblyEnabled). With the opt-in off — the
// default — no freeze point is ever recorded, so the staging hook and dispatcher
// remain no-ops and the shipped D206 per-completion run-branch merge
// (fanInIntegrateRunBranch) stays the SOLE fan-in path, byte-for-byte unchanged. The
// recorder is ADDITIVE to that merge: even when opted in it only WRITES the durable
// freeze record (which the staging hook keys on); it never replaces the per-completion
// merge and never makes the downstream gate wait on the barrier. Flipping
// STRIATUM_BARRIER_FANIN to the live default — and retiring fanInIntegrateRunBranch —
// remains the operator go-live step gated by the same-final-tree equivalence fixture
// confirmed against a real deployment (D246), and is NOT done here.
//
// WHAT A FAN-IN IS, structurally. A downstream job that depends on TWO OR MORE
// upstream sibling jobs is a fan-in join: the siblings run in parallel and the
// downstream job consumes their combined work (the canonical case is a phase's
// synthesis job depending on every author seat in the phase). The freeze record
// declares, once at fan-out, the immutable sibling set the barrier later joins. A
// downstream job with a single upstream is a plain chain edge, not a fan-in, so no
// freeze point is recorded for it.

// faninBarrierID derives the deterministic, stable barrier id for a fan-in keyed on
// its downstream (join) seat within a run. The downstream workflow_job_id is stable
// across recovery (job_id churns), so the barrier id is stable too — the staging hook
// (faninBarrierForSeat) and the dispatcher both resolve the barrier from the run +
// declared sibling set, not from this id, but a deterministic id keeps the freeze
// record idempotent (the recorder's ON CONFLICT DO NOTHING) across a re-prepare.
func faninBarrierID(runID, downstreamWorkflowJobID string) string {
	return fmt.Sprintf("barrier_fanin_%s_%s", runID, downstreamWorkflowJobID)
}

// recordRunFaninFreezePoints is the live fan-out caller (#527). At run materialization
// it inspects the workflow's dependency edges, finds every downstream join seat with
// two or more upstream siblings, and records one immutable fan-in freeze point per
// such seat declaring its sibling set, frozen at the run-branch tip.
//
// It is a STRICT NO-OP unless STRIATUM_BARRIER_FANIN=1 (shadow default off) — the
// recoverable kill switch. It is ALSO a no-op when the run branch is not yet confirmed
// (no frozen tip is knowable, so recording one would be a ghost base): a fan-in run
// whose branch is confirmed later simply does not declare a barrier this cycle, which
// keeps it on the unchanged per-completion path rather than recording a freeze point
// against an unconfirmed tip. frozenTip is the confirmed run-branch tip SHA ("" when
// the branch is not yet confirmed).
//
// declaredSiblingJobIDs is keyed on the stable workflow_job_id (the seat identity the
// staging predicate JOINs on), NOT the churning job_id. The set is sorted for a
// deterministic, reproducible freeze record.
func recordRunFaninFreezePoints(ctx context.Context, runner db.TxRunner, repositoryID, runID, frozenTip string, edges []dependencyEdge) error {
	if !barrierFaninAssemblyEnabled() {
		// Shadow default: the opt-in is off, so the live fan-out records nothing and
		// the staging hook + dispatcher stay no-ops. The per-completion merge (D206)
		// is the sole fan-in path, byte-for-byte unchanged.
		return nil
	}
	frozenTip = strings.TrimSpace(frozenTip)
	if !isFullGitSHA(frozenTip) {
		// The run branch is not confirmed yet (or the tip is unresolvable): there is no
		// frozen base to record against, so do not declare a barrier this cycle. The run
		// stays on the unchanged per-completion path.
		return nil
	}
	// Group upstream siblings by their downstream join seat. A downstream with >= 2
	// distinct upstreams is a fan-in; a downstream with exactly one upstream is a plain
	// chain edge and is skipped.
	siblings := map[string]map[string]struct{}{}
	for _, edge := range edges {
		from := strings.TrimSpace(edge.fromID)
		to := strings.TrimSpace(edge.toID)
		if from == "" || to == "" || from == to {
			continue
		}
		set := siblings[to]
		if set == nil {
			set = map[string]struct{}{}
			siblings[to] = set
		}
		set[from] = struct{}{}
	}
	// Record one freeze point per fan-in join seat, in deterministic seat order so a
	// re-prepare writes the identical record set (the recorder is idempotent anyway).
	downstreams := make([]string, 0, len(siblings))
	for to := range siblings {
		downstreams = append(downstreams, to)
	}
	sort.Strings(downstreams)
	for _, to := range downstreams {
		set := siblings[to]
		if len(set) < 2 {
			continue
		}
		declared := make([]string, 0, len(set))
		for from := range set {
			declared = append(declared, from)
		}
		sort.Strings(declared)
		if err := recordFaninFreezePoint(ctx, runner, repositoryID, faninFreezePoint{
			BarrierID:               faninBarrierID(runID, to),
			RunID:                   runID,
			DownstreamWorkflowJobID: to,
			FrozenTipSHA:            frozenTip,
			DeclaredSiblingJobIDs:   declared,
		}); err != nil {
			return fmt.Errorf("record fan-in freeze point for downstream %q: %w", to, err)
		}
	}
	return nil
}
