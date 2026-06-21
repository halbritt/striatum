package mutations

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// TestRecordRunFaninFreezePointsShadowDefaultIsNoOp proves the SHADOW default of the
// live fan-out seam (#527): with STRIATUM_BARRIER_FANIN unset, the fan-out recorder
// writes NO freeze point even for a textbook fan-in topology, so the staging hook +
// dispatcher stay no-ops and the shipped D206 per-completion merge remains the sole,
// unchanged fan-in path.
func TestRecordRunFaninFreezePointsShadowDefaultIsNoOp(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	repoID := "repo_fanout_shadow"
	runID := "run_fanout_shadow"
	intgSeedRepo(t, ctx, pool.Runner, repoID)
	intgSeedRun(t, ctx, pool.Runner, repoID, runID, map[string]any{"workflow_id": "wf"})

	tx := beginBarrierTx(t, ctx, pool)
	// A real fan-in: two authors feed one synthesis seat, plus a confirmed frozen tip.
	edges := []dependencyEdge{
		{fromID: "author_a", toID: "synth"},
		{fromID: "author_b", toID: "synth"},
	}
	frozenTip := "1111111111111111111111111111111111111111"

	withFaninAssemblyFlag(t, false, func() {
		if err := recordRunFaninFreezePoints(ctx, tx, repoID, runID, frozenTip, edges); err != nil {
			t.Fatalf("shadow fan-out recorder errored (must be a clean no-op): %v", err)
		}
	})
	if n := countFreezePoints(t, ctx, tx, repoID, runID); n != 0 {
		t.Fatalf("shadow default recorded %d freeze point(s); the default must record none", n)
	}
}

// TestRecordRunFaninFreezePointsRecordsDeclaredSiblingSet proves the OPT-IN live
// fan-out path (#527): with STRIATUM_BARRIER_FANIN=1 and a confirmed frozen tip, the
// recorder writes exactly one freeze point per downstream join seat with >= 2
// upstream siblings, declaring the sorted sibling set against the frozen tip — and a
// plain chain edge (single upstream) records nothing. This is the missing production
// caller of recordFaninFreezePoint that D246's revisit trigger named, and it wires
// end-to-end into the staging-at-completion predicate (faninBarrierForSeat).
func TestRecordRunFaninFreezePointsRecordsDeclaredSiblingSet(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	repoID := "repo_fanout_record"
	runID := "run_fanout_record"
	intgSeedRepo(t, ctx, pool.Runner, repoID)
	intgSeedRun(t, ctx, pool.Runner, repoID, runID, map[string]any{"workflow_id": "wf"})

	tx := beginBarrierTx(t, ctx, pool)
	// "synth" is a fan-in over three authors; "report" is a plain chain edge off
	// "synth" (single upstream) and must NOT get a freeze point.
	edges := []dependencyEdge{
		{fromID: "author_b", toID: "synth"},
		{fromID: "author_a", toID: "synth"},
		{fromID: "author_c", toID: "synth"},
		{fromID: "synth", toID: "report"},
	}
	frozenTip := "2222222222222222222222222222222222222222"

	withFaninAssemblyFlag(t, true, func() {
		if err := recordRunFaninFreezePoints(ctx, tx, repoID, runID, frozenTip, edges); err != nil {
			t.Fatalf("opt-in fan-out recorder: %v", err)
		}
	})

	// Exactly one freeze point — for the fan-in seat "synth", not the chain edge.
	rows, err := queryRows(ctx, tx, `
		SELECT barrier_id, downstream_workflow_job_id, frozen_tip_sha
		  FROM striatumd.fanin_freeze_points
		 WHERE repository_id = $1 AND run_id = $2`, repoID, runID)
	if err != nil {
		t.Fatalf("read freeze points: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("recorded %d freeze point(s); want exactly 1 (only the fan-in seat, not the chain edge)", len(rows))
	}
	row := rows[0]
	if got := fmt.Sprint(row["downstream_workflow_job_id"]); got != "synth" {
		t.Fatalf("downstream seat = %q; want synth", got)
	}
	if got := fmt.Sprint(row["frozen_tip_sha"]); got != frozenTip {
		t.Fatalf("frozen tip = %q; want %q", got, frozenTip)
	}
	if got := fmt.Sprint(row["barrier_id"]); got != faninBarrierID(runID, "synth") {
		t.Fatalf("barrier id = %q; want %q", got, faninBarrierID(runID, "synth"))
	}
	declared := declaredSiblingsOf(t, ctx, tx, repoID, faninBarrierID(runID, "synth"))
	want := []string{"author_a", "author_b", "author_c"}
	if len(declared) != len(want) {
		t.Fatalf("declared siblings = %v; want %v", declared, want)
	}
	for i := range want {
		if declared[i] != want[i] {
			t.Fatalf("declared siblings = %v; want sorted %v", declared, want)
		}
	}

	// THE CUTOVER SEAM: the staging-at-completion predicate now resolves THIS freeze
	// point for a completing declared in-edge — proving the fan-out recording wires
	// end-to-end into the staging path (with no freeze point recorded it would no-op).
	barrierID, found, ferr := faninBarrierForSeat(ctx, tx, repoID, runID, "author_a")
	if ferr != nil {
		t.Fatalf("faninBarrierForSeat: %v", ferr)
	}
	if !found || barrierID != faninBarrierID(runID, "synth") {
		t.Fatalf("a declared in-edge must resolve to the recorded barrier; found=%v id=%q", found, barrierID)
	}
	// An undeclared / chain-edge seat resolves to no barrier (the staging hook no-ops).
	if _, undeclaredFound, uerr := faninBarrierForSeat(ctx, tx, repoID, runID, "report"); uerr != nil {
		t.Fatalf("faninBarrierForSeat undeclared: %v", uerr)
	} else if undeclaredFound {
		t.Fatal("the chain-edge seat must not resolve to a fan-in barrier")
	}
}

// TestRecordRunFaninFreezePointsNoOpWithoutConfirmedTip proves the recorder is a
// no-op (even opted in) when the run branch is not yet confirmed: with no frozen tip
// there is no base to record against, so the run stays on the unchanged per-completion
// path rather than recording a ghost freeze point.
func TestRecordRunFaninFreezePointsNoOpWithoutConfirmedTip(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	repoID := "repo_fanout_unconfirmed"
	runID := "run_fanout_unconfirmed"
	intgSeedRepo(t, ctx, pool.Runner, repoID)
	intgSeedRun(t, ctx, pool.Runner, repoID, runID, map[string]any{"workflow_id": "wf"})

	tx := beginBarrierTx(t, ctx, pool)
	edges := []dependencyEdge{
		{fromID: "author_a", toID: "synth"},
		{fromID: "author_b", toID: "synth"},
	}
	withFaninAssemblyFlag(t, true, func() {
		// Empty tip (branch not confirmed) and a non-SHA tip both no-op.
		if err := recordRunFaninFreezePoints(ctx, tx, repoID, runID, "", edges); err != nil {
			t.Fatalf("empty-tip recorder errored: %v", err)
		}
		if err := recordRunFaninFreezePoints(ctx, tx, repoID, runID, "not-a-sha", edges); err != nil {
			t.Fatalf("non-sha-tip recorder errored: %v", err)
		}
	})
	if n := countFreezePoints(t, ctx, tx, repoID, runID); n != 0 {
		t.Fatalf("recorded %d freeze point(s) without a confirmed tip; want 0", n)
	}
}

// --- helpers ---

func countFreezePoints(t *testing.T, ctx context.Context, runner db.TxRunner, repoID, runID string) int {
	t.Helper()
	rows, err := queryRows(ctx, runner, `
		SELECT 1 FROM striatumd.fanin_freeze_points
		 WHERE repository_id = $1 AND run_id = $2`, repoID, runID)
	if err != nil {
		t.Fatalf("count freeze points: %v", err)
	}
	return len(rows)
}

func declaredSiblingsOf(t *testing.T, ctx context.Context, runner db.TxRunner, repoID, barrierID string) []string {
	t.Helper()
	rows, err := queryRows(ctx, runner, `
		SELECT sib.value AS seat
		  FROM striatumd.fanin_freeze_points fp,
		       jsonb_array_elements_text(fp.declared_sibling_job_ids) AS sib(value)
		 WHERE fp.repository_id = $1 AND fp.barrier_id = $2`, repoID, barrierID)
	if err != nil {
		t.Fatalf("read declared siblings: %v", err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprint(r["seat"]))
	}
	sort.Strings(out)
	return out
}
