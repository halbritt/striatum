package mutations

import (
	"context"
	"fmt"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// HandleRunIntegrate is the RFC 0108 Phase 4 serialized, gated integration step.
// It merges a COMPLETED run's branch into a target mainline branch — one run at a
// time per repository (serialized on lockRepo, the same per-repo lock the Phase
// 2/3 run.start gates take) — and NEVER auto-resolves a conflict: a conflicting
// merge is refused with merge_conflict naming the paths, leaving mainline
// untouched, so two operators' parallel runs integrate cleanly or surface the
// conflict to a human rather than corrupting the mainline.
//
// The merge is pure git plumbing — `merge-tree --write-tree` (a read-only 3-way
// merge simulation) to detect conflicts and compute the merged tree, then
// `commit-tree` + a compare-and-swap `update-ref` to advance the mainline ref —
// so it NEVER mutates a working tree or index. The operator's checkout (whatever
// branch it is on) is untouched; only the mainline ref advances, exactly as a
// fast-forwarding remote push would. This keeps integration safe to run against a
// live repository with other runs' worktrees checked out.
func HandleRunIntegrate(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "run.integrate requires run_id", nil)
	}
	into := strings.TrimSpace(stringParam(envelope, "into"))
	if into == "" {
		return nil, rpc.NewError("schema_invalid", "run.integrate requires into (the target mainline branch to integrate the run branch into)", nil)
	}
	repoRoot, err := activeRepositoryRoot(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// Serialize integration per repository (one merge at a time), the RFC 0108
		// "serialized, gated" invariant. Held across the git plumbing so a
		// concurrent integration cannot interleave its update-ref.
		if err := lockRepo(ctx, tx, repositoryID); err != nil {
			return nil, err
		}
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true)
		if err != nil {
			return nil, err
		}
		if state := fmt.Sprint(run["state"]); state != "completed" {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("run is %q; only a completed run can be integrated", state), nil)
		}
		// RFC 0135 P6 cutover (#354, D216): the run is the entity; its integration gate
		// is the RUN-ENTITY sealed barrier — the run is terminal-acceptable AND every
		// declared job-level sealed barrier inside it has fired (job barriers compose
		// into the run barrier). runEntityBarrierReady expresses this through the P0
		// db.BarrierReadySQL shape (entity_kind='run'). A run that declares NO job-level
		// barriers (every run on the default path — recordFaninFreezePoint records a
		// freeze point only when the STRIATUM_BARRIER_FANIN shadow opt-in is on, #527)
		// has an empty in-edge set, so the barrier reduces EXACTLY to
		// the `state == 'completed'` check above — proven byte-identical by
		// TestRunIntegrateIsTheRunEntityBarrier and TestRunIntegrateRunEntityBarrierGate.
		// This RETIRES the bare terminal-state default in favor of the composing
		// run-entity barrier without changing any current run's outcome, and makes a
		// future fan-in barrier compose into the integrate gate for free. The bare
		// `state` check is kept above for its precise error message and to short-circuit
		// the barrier query on a non-terminal run.
		//
		// Fallback: STRIATUM_BARRIER_RUN_ENTITY=0 forces the legacy terminal-state-only
		// gate (the recoverable kill switch), so a composition regression is reversible
		// without redeploying older code.
		if barrierRunEntityEnabled() {
			ready, err := runEntityBarrierReady(ctx, tx, repositoryID, runID)
			if err != nil {
				return nil, err
			}
			if !ready {
				return nil, rpc.NewError("invalid_transition",
					"run-entity barrier is not ready: the run is completed but a declared job-level barrier inside it has not fired (RFC 0135 P6); recover the outstanding barrier before integrating",
					map[string]any{"run_id": runID})
			}
		}
		runBranch := fmt.Sprint(run["branch_name"])
		if runBranch == "" || runBranch == "<nil>" {
			return nil, rpc.NewError("invalid_transition", "run has no branch to integrate", nil)
		}
		if nullable(run["branch_confirmed_at"]) == nil {
			return nil, rpc.NewError("invalid_transition", "run branch must be confirmed before integration", nil)
		}
		if runBranch == into {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("run branch and integration target are the same branch %q", into), nil)
		}
		// Idempotent: a run already integrated into this target is a no-op.
		if prior, err := runIntegratedInto(ctx, tx, repositoryID, runID, into); err != nil {
			return nil, err
		} else if prior != "" {
			return map[string]any{
				"run_id": runID, "into": into, "run_branch": runBranch,
				"merge_commit": prior, "integrated": true, "status": "already_integrated",
			}, nil
		}

		// Compute the merged tree + integration commit via the shared run-entity
		// assembly (the same merge-tree → conflict-detection → commit-tree plumbing
		// the RFC 0135 P6 run-entity barrier uses, factored into one place so the live
		// integrate path and the barrier path cannot drift). This is a pure
		// computation — it writes NO refs and never mutates a working tree; the
		// side-effecting event-append + CAS update-ref stay inline below, byte-for-byte
		// as RFC 0108 shipped them.
		asm, err := assembleRunEntityIntegration(ctx, repoRoot, runID, runBranch, into)
		if err != nil {
			return nil, err
		}
		intoSHA, mergeCommit := asm.IntoSHA, asm.MergeCommit

		// Record the integration in the event chain BEFORE advancing the ref: git is
		// not transactional with the DB, so the order that minimizes inconsistency is
		// append-then-update-ref. If update-ref fails, the appended event rolls back
		// with the transaction (no event, no ref move). The remaining tiny window — a
		// DB commit failure after a successful update-ref — is rare and non-corrupting
		// (idempotency re-merges cleanly).
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "run.integrated", nil, nil, nil, nil, nil, map[string]any{
			"into":         into,
			"run_branch":   runBranch,
			"merge_commit": mergeCommit,
			"base":         intoSHA,
		}); err != nil {
			return nil, err
		}

		// Compare-and-swap the mainline ref LAST: advance only if it still points at
		// the base we merged against (belt-and-suspenders under lockRepo, and it
		// catches an out-of-band git move between rev-parse and here).
		updateOut, exit, err := integrateGit(ctx, repoRoot, "update-ref", "refs/heads/"+into, mergeCommit, intoSHA)
		if err != nil {
			return nil, err
		}
		if exit != 0 {
			return nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("update-ref of %q failed (did mainline move concurrently?): %s", into, strings.TrimSpace(updateOut)), nil)
		}

		return map[string]any{
			"run_id": runID, "into": into, "run_branch": runBranch,
			"merge_commit": mergeCommit, "base": intoSHA, "integrated": true, "status": "integrated",
		}, nil
	})
}

// runIntegratedInto returns the merge_commit of a prior run.integrated event for
// this run into the given target, or "" when the run has not been integrated there.
func runIntegratedInto(ctx context.Context, runner any, repositoryID, runID, into string) (string, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT payload_json
		  FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND event_type = 'run.integrated'
		 ORDER BY event_id DESC`, repositoryID, runID)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		payload := asMap(row["payload_json"])
		if fmt.Sprint(payload["into"]) == into {
			return fmt.Sprint(payload["merge_commit"]), nil
		}
	}
	return "", nil
}

// integrateRevParse resolves a local branch to its commit sha, erroring with a
// clear message when the branch does not exist.
func integrateRevParse(ctx context.Context, repoRoot, branch string) (string, error) {
	out, exit, err := integrateGit(ctx, repoRoot, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	if exit != 0 {
		return "", rpc.NewError("invalid_transition", fmt.Sprintf("branch %q does not exist in the repository", branch), nil)
	}
	sha := firstLine(out)
	if !isFullGitSHA(sha) {
		return "", rpc.NewError("invalid_transition", fmt.Sprintf("could not resolve branch %q", branch), nil)
	}
	return sha, nil
}

// integrateGit runs a git plumbing command in repoRoot and returns its combined
// stdout, exit code, and any non-exit error. It reuses the worktree git runner
// (which never touches a working tree for these read/plumbing subcommands).
func integrateGit(ctx context.Context, repoRoot string, args ...string) (string, int, error) {
	result, err := runGitWorktreeCommand(ctx, repoRoot, args...)
	if err != nil {
		return "", 0, err
	}
	out := result.Stdout
	if strings.TrimSpace(out) == "" {
		out = result.Stderr
	}
	return out, result.ExitCode, nil
}

// mergeTreeWriteTree runs `git merge-tree --write-tree <a> <b>` and returns its
// stdout, stderr, and exit code SEPARATELY. The conflicted-file-info section the
// conflict parser needs (`<mode> <oid> <stage>\t<path>` lines) is written to
// stdout; diagnostics go to stderr. integrateGit collapses the two (returning
// stderr only when stdout is empty), which can hand parseMergeTreeConflicts the
// wrong stream and yield an empty conflict set on a non-zero exit — the #327
// "rejected with 0 conflicting paths" mislabel. Callers parse stdout for paths
// and use stderr only for diagnostics.
func mergeTreeWriteTree(ctx context.Context, repoRoot, a, b string) (stdout, stderr string, exit int, err error) {
	res, err := runGitWorktreeCommand(ctx, repoRoot, "merge-tree", "--write-tree", a, b)
	if err != nil {
		return "", "", 0, err
	}
	return res.Stdout, res.Stderr, res.ExitCode, nil
}

// gitBlobOIDAtPath returns the blob OID of repoPath at ref, and whether it
// exists there. Used to compare a path's content across two commits.
func gitBlobOIDAtPath(ctx context.Context, repoRoot, ref, repoPath string) (string, bool) {
	out, exit, err := integrateGit(ctx, repoRoot, "rev-parse", "--verify", "--quiet", ref+":"+repoPath)
	if err != nil || exit != 0 {
		return "", false
	}
	sha := firstLine(out)
	if !isFullGitSHA(sha) {
		return "", false
	}
	return sha, true
}

// filterRealFanInConflicts drops any path whose blob is byte-identical between
// the run tip and the sibling head. In a parallel job group a later sibling's
// worktree is seeded from a run-branch tip that already contains an earlier
// sibling's output (the RFC 0101 / #327 worktree-base race), so a uniform
// diff-from-fan-out-base surfaces that already-integrated path as a phantom
// add/add. Identical content is never a genuine two-writer overlap, so it is not
// a real conflict; a path with differing content on the two sides is kept and
// still rejected loudly.
func filterRealFanInConflicts(ctx context.Context, repoRoot, tip, head string, conflicts []string) []string {
	real := make([]string, 0, len(conflicts))
	for _, p := range conflicts {
		tipBlob, tipOK := gitBlobOIDAtPath(ctx, repoRoot, tip, p)
		headBlob, headOK := gitBlobOIDAtPath(ctx, repoRoot, head, p)
		if tipOK && headOK && tipBlob == headBlob {
			continue // already-integrated sibling output, byte-identical
		}
		real = append(real, p)
	}
	return real
}

// parseMergeTreeConflicts extracts the conflicting paths from a
// `git merge-tree --write-tree` conflict report: the conflicted-file section
// lists `<mode> <oid> <stage>\t<path>` lines, so every tab-delimited line names a
// conflicting path.
func parseMergeTreeConflicts(out string) []string {
	seen := map[string]bool{}
	paths := []string{}
	for _, line := range strings.Split(out, "\n") {
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		path := strings.TrimSpace(line[tab+1:])
		if path != "" && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return strings.TrimSpace(s)
}
