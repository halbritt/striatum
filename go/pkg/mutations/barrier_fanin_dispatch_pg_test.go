package mutations

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// withFaninAssemblyFlag runs fn with STRIATUM_BARRIER_FANIN forced to a known value,
// restoring the prior env afterward. The flag default is OFF (shadow); the equivalence
// fixture pins the opted-in (assembly) path against the legacy per-completion default.
func withFaninAssemblyFlag(t *testing.T, on bool, fn func()) {
	t.Helper()
	prior, had := os.LookupEnv("STRIATUM_BARRIER_FANIN")
	if on {
		_ = os.Setenv("STRIATUM_BARRIER_FANIN", "1")
	} else {
		_ = os.Unsetenv("STRIATUM_BARRIER_FANIN")
	}
	defer func() {
		if had {
			_ = os.Setenv("STRIATUM_BARRIER_FANIN", prior)
		} else {
			_ = os.Unsetenv("STRIATUM_BARRIER_FANIN")
		}
	}()
	fn()
}

// seedFaninDispatchRepo registers a repository row whose repo_root is the real git
// repo on disk, so DispatchBarrierAssembly's activeRepositoryRoot resolves to the
// temp dir the test seeds (intgSeedRepo pins repo_root to /tmp/<repoID>, which is not
// the test's git repo).
func seedFaninDispatchRepo(t *testing.T, ctx context.Context, runner interface {
	Exec(ctx context.Context, sql string, args ...any) error
}, repoID, repoRoot string) {
	t.Helper()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.repositories (
		  repository_id, repo_identity, repo_root, state_db_path, display_name,
		  registered_at, last_schema_version, state
		) VALUES ($1,$2,$3,$4,'repo',$5,16,'active')`,
		repoID, "ident_"+repoID, repoRoot, repoRoot+"/.striatum", time.Now().UTC()); err != nil {
		t.Fatalf("insert repository (repo_root=%s): %v", repoRoot, err)
	}
}

// TestFaninAssemblyDispatchSameFinalTreeAsPerCompletion is the RFC 0135 P1/P2 (#354,
// D246) end-to-end EQUIVALENCE FIXTURE for the staging-at-completion hook + the
// barrier_assembly DISPATCHER. It proves the wired opt-in path —
// stageFaninContributionAtCompletion staging each disjoint sibling, then
// DispatchBarrierAssembly folding them onto the frozen tip and CAS-advancing the run
// branch — produces a run-branch tree BYTE-IDENTICAL to the shipped D206
// per-completion merge (fanInIntegrateRunBranch), for two disjoint-write-scope
// siblings. This is the gate that justifies a future operator flip of
// STRIATUM_BARRIER_FANIN to live; the code ships in shadow (flag OFF) regardless.
func TestFaninAssemblyDispatchSameFinalTreeAsPerCompletion(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner
	applyBarrierAssemblyOwnerBundle(t, ctx, runner)

	// --- Reference: the shipped D206 per-completion merge into a run branch. ---
	d206Root := t.TempDir()
	base := gitInit(t, d206Root)
	runBranch := "wf/dispatch-equiv"
	gitRun(t, d206Root, "branch", runBranch, base)
	headA := commitSiblingOffBase(t, d206Root, base, "docs/a.txt", "A\n")
	if _, err := fanInIntegrateRunBranch(ctx, d206Root, "refs/heads/"+runBranch, base, headA, "job_a", 1); err != nil {
		t.Fatalf("D206 integrate sibling A: %v", err)
	}
	headB := commitSiblingOffBase(t, d206Root, base, "docs/b.txt", "B\n")
	runTip := gitRevParse(t, d206Root, "refs/heads/"+runBranch)
	if _, err := fanInIntegrateRunBranch(ctx, d206Root, "refs/heads/"+runBranch, runTip, headB, "job_b", 1); err != nil {
		t.Fatalf("D206 integrate sibling B: %v", err)
	}
	d206Tree := gitRun(t, d206Root, "rev-parse", "refs/heads/"+runBranch+"^{tree}")

	// --- The wired opt-in path: stage-at-completion + dispatch the assembly. ---
	barrierRoot := t.TempDir()
	bbase := gitInit(t, barrierRoot)
	runRef := "refs/heads/" + runBranch
	gitRun(t, barrierRoot, "branch", runBranch, bbase)
	bHeadA := commitSiblingOffBase(t, barrierRoot, bbase, "docs/a.txt", "A\n")
	bHeadB := commitSiblingOffBase(t, barrierRoot, bbase, "docs/b.txt", "B\n")

	repoID := "repo_fanin_dispatch"
	runID := "run_fanin_dispatch"
	barrierID := "barrier_fanin_dispatch"
	seedFaninDispatchRepo(t, ctx, runner, repoID, barrierRoot)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{"workflow_id": "wf"})
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_a", "author_a", "completed", 1)
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_b", "author_b", "completed", 1)

	// Record the fan-in freeze point (the fan-out declaration the staging hook keys on).
	tx := beginBarrierTx(t, ctx, pool)
	if err := recordFaninFreezePoint(ctx, tx, repoID, faninFreezePoint{
		BarrierID:               barrierID,
		RunID:                   runID,
		DownstreamWorkflowJobID: "synth",
		FrozenTipSHA:            bbase,
		DeclaredSiblingJobIDs:   []string{"author_a", "author_b"},
	}); err != nil {
		t.Fatalf("record freeze point: %v", err)
	}

	// Stage BOTH siblings through the production staging-at-completion hook (flag ON).
	// The hook resolves each seat's fan-in barrier from the freeze record and stages
	// the contribution; with the flag off it would be a strict no-op (asserted below).
	withFaninAssemblyFlag(t, true, func() {
		refA, err := stageFaninContributionAtCompletion(ctx, tx, barrierRoot, repoID, runID, "author_a", "job_a", bHeadA, 1)
		if err != nil {
			t.Fatalf("stage-at-completion sibling A: %v", err)
		}
		if refA == "" {
			t.Fatal("stage-at-completion sibling A returned no ref; the hook should have staged a declared in-edge")
		}
		refB, err := stageFaninContributionAtCompletion(ctx, tx, barrierRoot, repoID, runID, "author_b", "job_b", bHeadB, 1)
		if err != nil {
			t.Fatalf("stage-at-completion sibling B: %v", err)
		}
		if refB == "" {
			t.Fatal("stage-at-completion sibling B returned no ref")
		}
	})
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit staged contributions: %v", err)
	}

	// Dispatch the assembly through the production dispatcher (flag ON). It locks the
	// run, verifies readiness, runs the two-phase-journaled assembly, and CAS-advances
	// the run branch to the deterministic assembled commit.
	var assembledTree string
	withFaninAssemblyFlag(t, true, func() {
		res, err := DispatchBarrierAssembly(ctx, runner, repoID, runID, barrierID, runRef, "asm", "job_asm")
		if err != nil {
			t.Fatalf("DispatchBarrierAssembly: %v", err)
		}
		if res.Mode != "assembled" {
			t.Fatalf("dispatch mode = %q; want assembled", res.Mode)
		}
		assembledTree = gitRun(t, barrierRoot, "rev-parse", runRef+"^{tree}")
		if got := gitRevParse(t, barrierRoot, runRef); got != res.CommitSHA {
			t.Fatalf("run branch tip %s != assembled commit %s", got, res.CommitSHA)
		}
	})

	// THE EQUIVALENCE: the dispatched assembly's final run-branch tree is byte-identical
	// to the D206 per-completion merge tree.
	if assembledTree != d206Tree {
		barrierFiles := gitRun(t, barrierRoot, "ls-tree", "-r", "--name-only", assembledTree)
		d206Files := gitRun(t, d206Root, "ls-tree", "-r", "--name-only", d206Tree)
		t.Fatalf("dispatch assembly tree %s != D206 per-completion tree %s\ndispatch files:\n%s\nD206 files:\n%s",
			assembledTree, d206Tree, barrierFiles, d206Files)
	}
}

// TestFaninAssemblyDispatchShadowDefaultIsNoOp proves the SHADOW default: with
// STRIATUM_BARRIER_FANIN unset, the staging-at-completion hook is a strict no-op
// (stages nothing, returns "") and the dispatcher REFUSES — so wiring these into the
// completion path cannot change any current run's behavior.
func TestFaninAssemblyDispatchShadowDefaultIsNoOp(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	root := t.TempDir()
	bbase := gitInit(t, root)
	head := commitSiblingOffBase(t, root, bbase, "docs/a.txt", "A\n")

	repoID := "repo_fanin_shadow"
	runID := "run_fanin_shadow"
	barrierID := "barrier_fanin_shadow"
	seedFaninDispatchRepo(t, ctx, runner, repoID, root)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{"workflow_id": "wf"})
	seedFaninSeatJob(t, ctx, runner, repoID, runID, "job_a", "author_a", "completed", 1)

	tx := beginBarrierTx(t, ctx, pool)
	if err := recordFaninFreezePoint(ctx, tx, repoID, faninFreezePoint{
		BarrierID:               barrierID,
		RunID:                   runID,
		DownstreamWorkflowJobID: "synth",
		FrozenTipSHA:            bbase,
		DeclaredSiblingJobIDs:   []string{"author_a"},
	}); err != nil {
		t.Fatalf("record freeze point: %v", err)
	}

	// Default (flag OFF): the staging hook is a strict no-op EVEN for a declared
	// in-edge with a recorded freeze point.
	withFaninAssemblyFlag(t, false, func() {
		ref, err := stageFaninContributionAtCompletion(ctx, tx, root, repoID, runID, "author_a", "job_a", head, 1)
		if err != nil {
			t.Fatalf("shadow staging hook errored (must be a clean no-op): %v", err)
		}
		if ref != "" {
			t.Fatalf("shadow staging hook staged a contribution (ref=%q); the default must not stage", ref)
		}
	})
	// No staging row was written.
	staged, err := queryRows(ctx, tx, `
		SELECT 1 FROM striatumd.barrier_staged_contributions
		 WHERE repository_id = $1 AND barrier_id = $2`, repoID, barrierID)
	if err != nil {
		t.Fatalf("read staged rows: %v", err)
	}
	if len(staged) != 0 {
		t.Fatalf("shadow path wrote %d staging row(s); the default must stage nothing", len(staged))
	}
	_ = tx.Rollback(ctx)

	// The dispatcher refuses on the default path (opt-in off).
	withFaninAssemblyFlag(t, false, func() {
		if _, err := DispatchBarrierAssembly(ctx, runner, repoID, runID, barrierID, "refs/heads/wf", "asm", "job_asm"); err == nil {
			t.Fatal("DispatchBarrierAssembly must REFUSE with STRIATUM_BARRIER_FANIN unset (shadow default)")
		}
	})
}

// TestFaninBarrierForSeatResolvesDeclaredInEdge proves faninBarrierForSeat resolves
// the fan-in barrier a declared seat belongs to, and returns (found=false) for a seat
// no freeze record declares — which is why the staging hook no-ops on every run that
// declared no fan-in (every run today, since recordFaninFreezePoint has no live
// fan-out caller).
func TestFaninBarrierForSeatResolvesDeclaredInEdge(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_seat_resolve"
	runID := "run_seat_resolve"
	barrierID := "barrier_seat_resolve"
	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{"workflow_id": "wf"})

	tx := beginBarrierTx(t, ctx, pool)
	if err := recordFaninFreezePoint(ctx, tx, repoID, faninFreezePoint{
		BarrierID:               barrierID,
		RunID:                   runID,
		DownstreamWorkflowJobID: "synth",
		FrozenTipSHA:            "0000000000000000000000000000000000000000",
		DeclaredSiblingJobIDs:   []string{"author_a", "author_b"},
	}); err != nil {
		t.Fatalf("record freeze point: %v", err)
	}

	got, found, err := faninBarrierForSeat(ctx, tx, repoID, runID, "author_a")
	if err != nil {
		t.Fatalf("faninBarrierForSeat declared seat: %v", err)
	}
	if !found || got != barrierID {
		t.Fatalf("declared seat author_a should resolve to %q, got found=%v id=%q", barrierID, found, got)
	}

	_, found, err = faninBarrierForSeat(ctx, tx, repoID, runID, "author_undeclared")
	if err != nil {
		t.Fatalf("faninBarrierForSeat undeclared seat: %v", err)
	}
	if found {
		t.Fatal("an undeclared seat must resolve to no fan-in barrier (found=false) so the staging hook no-ops")
	}
	_ = tx.Rollback(ctx)
}
