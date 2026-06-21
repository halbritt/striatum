package mutations

import (
	"context"
	"fmt"
	"os"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// RFC 0135 P1/P2 (#354, D246) — the barrier_assembly DISPATCHER + the
// staging-at-completion hook that, together, turn the previously dead-code fan-in
// barrier machinery (recordFaninFreezePoint / stageFaninContribution /
// runBarrierAssembly in barrier_fanin.go + barrier_assembly.go) into a wired,
// end-to-end-exercisable path — while keeping the shipped D206 per-completion merge
// (fanInIntegrateRunBranch in worktree.go) the DEFAULT.
//
// CUTOVER DISCIPLINE (RFC 0133 / RFC 0135 "Migration and rollout", D233). The
// fan-in fold stays SHADOW: STRIATUM_BARRIER_FANIN defaults OFF, so neither the
// staging-at-completion hook nor the dispatcher does anything on the default path.
// The same-final-tree equivalence fixtures (TestFaninBarrierSameFinalTreeAsPerCompletion
// in barrier_fanin_pg_test.go and TestFaninAssemblyDispatchSameFinalTreeAsPerCompletion
// here) prove the staging + dispatch assembly produces the BYTE-IDENTICAL run-branch
// tree the legacy per-completion merge produces, for representative fan-in cases.
// THAT fixture is the gate a future operator flip is justified by; this PR ships the
// code in shadow (the safe shipped state), not a default change.
//
// POLARITY NOTE. STRIATUM_BARRIER_FANIN is opt-IN (default OFF), the inverse of the
// P4 quorum / P6 run-entity kill switches (which default ON and OFF disables). D233
// keeps fan-in (P1) and assembly (P2) in shadow because the staging-at-completion
// path changes how every fan-in sibling's work reaches the run branch — the
// highest-blast-radius surface — so it flips only after an operator confirms the
// equivalence fixture against a real deployment with owner bundle 0013 applied.

// barrierFaninAssemblyEnabled reports whether the RFC 0135 P1/P2 fan-in
// staging-at-completion + barrier_assembly dispatch path is opted in. It is OFF by
// default (shadow): only STRIATUM_BARRIER_FANIN=1 turns it on. With it off the
// shipped D206 per-completion run-branch merge stays the sole fan-in path and the
// staging hook + dispatcher are strict no-ops, so the default behavior is unchanged.
func barrierFaninAssemblyEnabled() bool {
	return os.Getenv("STRIATUM_BARRIER_FANIN") == "1"
}

// stageFaninContributionAtCompletion is the staging-at-completion hook (RFC 0135
// P1). When the fan-in fold is opted in AND the completing seat is a declared
// in-edge of a recorded fan-in freeze point in its run, it stages the seat's
// completion as an attempt-addressed contribution (stageFaninContribution), so the
// barrier can later fire and assemble. It is ADDITIVE to (never a replacement for)
// the legacy per-completion merge that the completion path runs inline — staging
// records the contribution durably; the default integrate still happens — so a
// shadow deployment accumulates the durable staging witness without changing how the
// run branch advances.
//
// It is a strict NO-OP on the default path: with STRIATUM_BARRIER_FANIN unset (every
// run on the default path, since recordRunFaninFreezePoints — the live fan-out caller,
// #527 — also no-ops when the opt-in is off, so no run declares a fan-in freeze
// point), or when the seat belongs to no recorded fan-in freeze point, it returns
// ("", nil) without touching git or PG. So wiring this into the completion path cannot
// change any default-path run's outcome.
//
// commitSHA is the seat's completed worktree HEAD (the same head the per-completion
// merge folds). attempt is the seat's live attempt (the seal). It returns the
// staging ref written (empty when it no-ops).
func stageFaninContributionAtCompletion(ctx context.Context, runner db.TxRunner, repoRoot, repositoryID, runID, workflowJobID, jobID, commitSHA string, attempt int) (string, error) {
	if !barrierFaninAssemblyEnabled() {
		return "", nil
	}
	barrierID, found, err := faninBarrierForSeat(ctx, runner, repositoryID, runID, workflowJobID)
	if err != nil {
		return "", err
	}
	if !found {
		// The seat is not a declared fan-in in-edge: no freeze point waits on it. The
		// default per-completion path is the only thing that runs (this hook no-ops).
		return "", nil
	}
	return stageFaninContribution(ctx, runner, repoRoot, repositoryID, barrierID, runID, workflowJobID, jobID, commitSHA, attempt)
}

// faninBarrierForSeat resolves the fan-in barrier (freeze record) a completing seat
// is a DECLARED in-edge of, scoped to the seat's run. A seat appears in at most one
// fan-in barrier per run (the freeze record declares its sibling set once at
// fan-out). It returns ("", false, nil) when the seat is not a declared fan-in
// in-edge — the case for every run on the default path, since the live fan-out caller
// (recordRunFaninFreezePoints, #527) records a freeze point only when the
// STRIATUM_BARRIER_FANIN shadow opt-in is on, which keeps the staging hook a no-op
// for every default-path run.
func faninBarrierForSeat(ctx context.Context, runner db.TxRunner, repositoryID, runID, workflowJobID string) (string, bool, error) {
	// Match the seat against each freeze record's declared sibling set via
	// jsonb_array_elements_text — the same expansion the barrier evaluator
	// (barrier_fanin.go) and the doctor use, rather than the jsonb `?` containment
	// operator (which collides with the pgx parameter placeholder).
	rows, err := queryRows(ctx, runner, `
		SELECT fp.barrier_id
		  FROM striatumd.fanin_freeze_points fp
		 WHERE fp.repository_id = $1 AND fp.run_id = $2
		   AND EXISTS (
		     SELECT 1
		       FROM jsonb_array_elements_text(fp.declared_sibling_job_ids) AS sib(value)
		      WHERE sib.value = $3
		   )
		 ORDER BY fp.barrier_id
		 LIMIT 1`,
		repositoryID, runID, workflowJobID)
	if err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	return fmt.Sprint(rows[0]["barrier_id"]), true, nil
}

// DispatchBarrierAssembly is the barrier_assembly job DISPATCHER (RFC 0135 P2): the
// production entry point that runs the recoverable, two-phase-journaled assembly
// (runBarrierAssembly) for a fired fan-in barrier, advancing the run branch to the
// deterministic assembled commit. It is the only production caller of
// runBarrierAssembly; the daemon's job loop invokes it when a barrier_assembly job
// is claimed for a barrier whose readiness predicate (faninBarrierReady) is true.
//
// SHADOW DISCIPLINE. It refuses unless the fan-in fold is opted in
// (STRIATUM_BARRIER_FANIN=1) AND owner bundle 0013 has widened the jobs job_type
// CHECK to permit 'barrier_assembly' (jobBarrierAssemblyTypePermitted). With the
// opt-in off — the default — it returns a clear invalid_transition rather than
// silently advancing the run branch by a path the default does not use, so a
// behind-deployment or a default deployment can never reach the assembly by
// accident.
//
// It runs inside one transaction holding the per-run advisory lock (RFC 0104 fire
// serialization), exactly as runBarrierAssembly requires. runRef is the run-branch
// ref the assembly CAS-advances.
func DispatchBarrierAssembly(ctx context.Context, runner db.Runner, repositoryID, runID, barrierID, runRef, assemblyWorkflowJobID, assemblyJobID string) (barrierAssemblyResult, error) {
	if !barrierFaninAssemblyEnabled() {
		return barrierAssemblyResult{}, rpc.NewError("invalid_transition",
			"barrier_assembly dispatch is opt-in (STRIATUM_BARRIER_FANIN unset); the shipped per-completion run-branch merge (D206) is the default fan-in path",
			map[string]any{"barrier_id": barrierID, "run_id": runID})
	}
	repoRoot, err := activeRepositoryRoot(ctx, runner, repositoryID)
	if err != nil {
		return barrierAssemblyResult{}, err
	}
	var out barrierAssemblyResult
	_, err = withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// Owner bundle 0013 must permit the barrier_assembly job_type, or the job that
		// drives this dispatch could not have been persisted; probe defensively so a
		// behind-deployment refuses loudly rather than advancing the run branch off a
		// path the deployment cannot record.
		permitted, perr := jobBarrierAssemblyTypePermitted(ctx, tx)
		if perr != nil {
			return nil, perr
		}
		if !permitted {
			return nil, rpc.NewError("invalid_transition",
				"barrier_assembly job_type is not permitted by the live jobs CHECK; apply owner bundle 0013 before dispatching the assembly",
				map[string]any{"barrier_id": barrierID, "run_id": runID})
		}
		// Serialize fire per run (RFC 0104): the assembly CAS-advances the run branch,
		// so it must not interleave with another fire/integration on the same run.
		if lerr := lockRun(ctx, tx, repositoryID, runID); lerr != nil {
			return nil, lerr
		}
		// The barrier must actually be ready (every declared in-edge live-staged or a
		// terminal gap) before we assemble — never assemble a partial join.
		ready, rerr := faninBarrierReady(ctx, tx, repositoryID, barrierID)
		if rerr != nil {
			return nil, rerr
		}
		if !ready {
			return nil, rpc.NewError("invalid_transition",
				"fan-in barrier is not ready: a declared in-edge is not live-staged and is not a terminal gap; do not assemble a partial join",
				map[string]any{"barrier_id": barrierID, "run_id": runID})
		}
		res, aerr := runBarrierAssembly(ctx, tx, repoRoot, runRef, repositoryID, barrierID, assemblyWorkflowJobID, assemblyJobID)
		if aerr != nil {
			return nil, aerr
		}
		out = res
		return map[string]any{"status": "assembled"}, nil
	})
	if err != nil {
		return barrierAssemblyResult{}, err
	}
	return out, nil
}
