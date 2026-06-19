package mutations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/reads"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

// LaneQuarantinedEventType is the append-only event recovery quarantine-lane
// writes when it snapshots a terminal run's dirty lane worktree to a durable
// quarantine ref. It mirrors reads.DebrisPrunedEventType's audit role for the
// #303 prune-debris verb: the event is the durable record of the disposition,
// keyed on (run_id, job_id) with the snapshot commit and changed paths in its
// payload.
const LaneQuarantinedEventType = "recovery.lane_quarantined"

// HandleRecoveryQuarantineLane is GH #298: daemon-owned recovery for a
// canceled/terminal run's DIRTY lane worktree.
//
// Background. When a run is canceled (or fails) while a per-job lane worktree
// still holds uncommitted repo-write work, no recovery verb preserves OR
// disposes of that work:
//   - recovery complete-stalled refuses canceled/failed runs (it applies only to
//     running/needs_operator runs).
//   - worktree gc / worktree release `worktree remove --force` a worktree once
//     its job is terminal — and for a dirty worktree that SILENTLY DISCARDS the
//     uncommitted change (data loss).
//
// Operators were left to hand-quarantine the work to a branch — exactly the
// manual worktree capture AGENTS.md forbids ("do not paste over a broken
// runner").
//
// quarantine-lane closes that gap, daemon-owned, with no checkout-mutating user
// command:
//  1. Detect dirt via `git status --porcelain` in the worktree (gitCommitChangedPaths).
//  2. Snapshot the uncommitted work to a commit WITHOUT disturbing the lane: a
//     write-tree against a scratch index (read-tree HEAD + add -A + write-tree),
//     then commit-tree with parent = worktree HEAD and the integrator identity.
//  3. Quarantine: update-ref refs/striatum/quarantine/<run>/<job>/<attempt> to the
//     snapshot commit (reachable + auditable, survives gc, in the same
//     refs/striatum namespace durableWorktreeRefs understands).
//  4. Record an append-only recovery.lane_quarantined event {snapshot_commit,
//     changed_paths, run_id, job_id, attempt} (the #303 debris_pruned audit pattern).
//  5. Clean: ONLY AFTER the snapshot ref is durable, `worktree remove --force`
//     and mark the job_worktrees row removed (mirrors worktree gc).
//
// Guards mirror recovery_prune_debris.go: requireRepositoryID + run_id/job_id,
// withTx + lockRun, and a hard REFUSE with invalid_transition when the run is
// NOT terminal (canceled/failed). --dry-run previews the changed paths and the
// would-be quarantine ref and writes nothing. It is idempotent: a re-run after
// the worktree is gone (or already quarantined) is a no-op. A CLEAN worktree has
// nothing to quarantine and is reported clean (and still gc'd to retire the row).
func HandleRecoveryQuarantineLane(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	jobID := stringParam(envelope, "job_id")
	if runID == "" || jobID == "" {
		return nil, rpc.NewError("schema_invalid", "recovery.quarantine_lane requires run_id and job_id", nil)
	}
	dryRun := boolParam(envelope, "dry_run")

	repoRoot, err := activeRepositoryRoot(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}

	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first (mirrors HandleRecoveryPruneDebris).
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, false)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rpc.NewError("not_found", fmt.Sprintf("could not find runs row for %q", runID), nil)
		}
		if err != nil {
			return nil, err
		}
		runState := fmt.Sprint(run["state"])
		// Run-state gate: only a TERMINAL (canceled/failed) run leaves orphaned
		// lane dirt. Refuse anything else (active/completed) so the verb can never
		// snapshot/discard a live or successful run's in-flight worktree.
		//
		// RFC 0138 (#453): this verb is the OPERATOR's manual exit for a terminal
		// run's dead lane; it is NOT the live-run case. For a LIVE strict fan-in run
		// wedged on a provably-dead REQUIRED seat, the automatic, opt-in,
		// provably-dead-only exit is the sealed terminal gap
		// (resolveFaninSealedGapSeats in barrier_fanin.go, fanin_tolerates_sealed_gap +
		// max_sealed_gaps on the freeze record). The two do not overlap: quarantine-lane
		// snapshots a canceled run's dirty worktree; the sealed gap fires a live run's
		// barrier degraded. Without the opt-in, the doctor reason
		// strict_fanin_required_seat_unrecoverable surfaces the operator's bounded
		// recovery (cancel the run, then optionally quarantine the now-terminal seat).
		if !reads.ArtifactDebrisRunStateTerminal(runState) {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
				"run is %s; quarantine-lane applies only to a terminal run (canceled/failed)", runState), nil)
		}

		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rpc.NewError("not_found", fmt.Sprintf("could not find jobs row for %q", jobID), nil)
		}
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(job["run_id"]) != runID {
			return nil, rpc.NewError("invalid_transition", "job does not belong to the given run_id", nil)
		}
		attempt := intValue(job["attempt"])

		// Resolve the lane worktree row for this job. Idempotency: a re-run after a
		// prior quarantine finds no active/abandoned worktree -> no-op.
		worktree, err := quarantineLaneWorktree(ctx, tx, repositoryID, jobID)
		if err != nil {
			return nil, err
		}
		base := map[string]any{
			"run_id":    runID,
			"run_state": runState,
			"job_id":    jobID,
			"attempt":   attempt,
		}
		if worktree == nil {
			base["status"] = "already_quarantined"
			base["reason"] = "no_active_worktree"
			base["next_actions"] = []string{"recovery quarantine-lane is a no-op; the lane worktree row is already retired"}
			return base, nil
		}
		worktreeID := fmt.Sprint(worktree["worktree_id"])
		leaseID := worktree["lease_id"]
		base["worktree_id"] = worktreeID
		base["worktree_path"] = worktree["worktree_path"]

		target, err := worktreeTarget(repoRoot, fmt.Sprint(worktree["worktree_path"]))
		if err != nil {
			return nil, err
		}

		// Worktree gone on disk: nothing to snapshot. Retire the row idempotently
		// (mirrors the missing-on-disk gc path) and record a clean disposition.
		if !pathExists(target) {
			if dryRun {
				base["status"] = "would_quarantine"
				base["dirty"] = false
				base["missing_on_disk"] = true
				base["changed_paths"] = []string{}
				base["next_actions"] = []string{"recovery quarantine-lane (omit --dry-run to retire the missing worktree row)"}
				return base, nil
			}
			if err := markWorktreeRemoved(ctx, tx, repositoryID, worktreeID); err != nil {
				return nil, err
			}
			if _, err := appendEvent(ctx, tx, repositoryID, runID, LaneQuarantinedEventType, nil, jobID, nil, nil, leaseID, map[string]any{
				"run_id":          runID,
				"job_id":          jobID,
				"attempt":         attempt,
				"worktree_id":     worktreeID,
				"worktree_path":   worktree["worktree_path"],
				"dirty":           false,
				"missing_on_disk": true,
				"changed_paths":   []string{},
				"source":          "recovery.quarantine_lane",
			}); err != nil {
				return nil, err
			}
			base["status"] = "clean"
			base["dirty"] = false
			base["missing_on_disk"] = true
			base["changed_paths"] = []string{}
			base["next_actions"] = []string{"missing worktree row retired; nothing to quarantine"}
			return base, nil
		}

		// Detect dirt with the same porcelain probe the publish guard parses
		// (--porcelain=v1 -z --untracked-files=all, via parseGitPorcelainZ), so the
		// changed-paths set matches what the lane left behind exactly.
		changedPaths, err := quarantineWorktreeChangedPaths(ctx, target)
		if err != nil {
			return nil, err
		}
		base["changed_paths"] = changedPaths

		quarantineRef := quarantineLaneRef(runID, jobID, attempt)
		base["quarantine_ref"] = quarantineRef

		// CLEAN worktree: no uncommitted work to preserve. Report clean and still
		// gc the worktree to retire its row (the deferred-to disposition).
		if len(changedPaths) == 0 {
			if dryRun {
				base["status"] = "would_quarantine"
				base["dirty"] = false
				base["next_actions"] = []string{"recovery quarantine-lane (omit --dry-run to gc the clean worktree)"}
				return base, nil
			}
			result, err := runGitWorktreeCommand(ctx, repoRoot, "worktree", "remove", "--force", target)
			if err != nil {
				return nil, err
			}
			if result.ExitCode != 0 && pathExists(target) {
				return nil, rpc.NewError("invalid_transition", gitWorktreeErrorMessage("git worktree remove failed", result), nil)
			}
			if err := markWorktreeRemoved(ctx, tx, repositoryID, worktreeID); err != nil {
				return nil, err
			}
			if _, err := appendEvent(ctx, tx, repositoryID, runID, LaneQuarantinedEventType, nil, jobID, nil, nil, leaseID, map[string]any{
				"run_id":        runID,
				"job_id":        jobID,
				"attempt":       attempt,
				"worktree_id":   worktreeID,
				"worktree_path": worktree["worktree_path"],
				"dirty":         false,
				"changed_paths": []string{},
				"source":        "recovery.quarantine_lane",
			}); err != nil {
				return nil, err
			}
			base["status"] = "clean"
			base["dirty"] = false
			base["next_actions"] = []string{"clean worktree gc'd; nothing to quarantine"}
			return base, nil
		}

		base["dirty"] = true

		// Snapshot the uncommitted work to a commit WITHOUT disturbing the lane's
		// working tree (write-tree against a scratch index), so the would-be
		// quarantine commit can be previewed and computed before any mutation.
		snapshotCommit, err := snapshotDirtyWorktree(ctx, target)
		if err != nil {
			return nil, err
		}
		base["snapshot_commit"] = snapshotCommit

		if dryRun {
			base["status"] = "would_quarantine"
			base["next_actions"] = []string{"recovery quarantine-lane (omit --dry-run to quarantine + gc)"}
			return base, nil
		}

		// Quarantine: make the snapshot durable+auditable under the refs/striatum
		// namespace BEFORE touching the worktree, so the work is never discarded
		// without first being preserved (the #298 silent-data-loss fix). update-ref
		// here is unconditional create/update; the ref is idempotent under re-run.
		out, exit, err := integrateGit(ctx, repoRoot, "update-ref", quarantineRef, snapshotCommit)
		if err != nil {
			return nil, err
		}
		if exit != 0 {
			return nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf(
				"update-ref of %q failed while quarantining lane worktree %s: %s", quarantineRef, worktreeID, strings.TrimSpace(out)), nil)
		}

		// Record the disposition (append-only audit) BEFORE the worktree removal so
		// the durable chain records the snapshot even if the removal later errors.
		if _, err := appendEvent(ctx, tx, repositoryID, runID, LaneQuarantinedEventType, nil, jobID, nil, nil, leaseID, map[string]any{
			"run_id":          runID,
			"job_id":          jobID,
			"attempt":         attempt,
			"worktree_id":     worktreeID,
			"worktree_path":   worktree["worktree_path"],
			"dirty":           true,
			"changed_paths":   changedPaths,
			"snapshot_commit": snapshotCommit,
			"quarantine_ref":  quarantineRef,
			"source":          "recovery.quarantine_lane",
		}); err != nil {
			return nil, err
		}

		// Clean: only now that the snapshot ref is durable, remove the worktree and
		// retire its row (mirrors HandleWorktreeGC's removal + row update).
		result, err := runGitWorktreeCommand(ctx, repoRoot, "worktree", "remove", "--force", target)
		if err != nil {
			return nil, err
		}
		if result.ExitCode != 0 && pathExists(target) {
			return nil, rpc.NewError("invalid_transition", gitWorktreeErrorMessage("git worktree remove failed", result), nil)
		}
		if err := markWorktreeRemoved(ctx, tx, repositoryID, worktreeID); err != nil {
			return nil, err
		}

		base["status"] = "quarantined"
		base["next_actions"] = []string{
			fmt.Sprintf("inspect the quarantined work with: git log -p %s", quarantineRef),
			"the worktree is removed; the uncommitted work is preserved under the quarantine ref",
		}
		return base, nil
	})
}

// quarantineLaneWorktree resolves the lane worktree row to quarantine for a job:
// the active (or abandoned) per-job worktree. It returns nil (no error) when no
// such row exists, which the handler treats as an idempotent no-op (a prior
// quarantine already retired the row).
func quarantineLaneWorktree(ctx context.Context, runner any, repositoryID, jobID string) (map[string]any, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT *
		  FROM striatumd.job_worktrees
		 WHERE repository_id = $1 AND job_id = $2 AND state IN ('active', 'abandoned')
		 ORDER BY created_at DESC
		 LIMIT 1
		 FOR UPDATE`,
		repositoryID, jobID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// markWorktreeRemoved retires a job_worktrees row, mirroring the gc/release
// removal update (state='removed' with release/removal timestamps), guarded on
// the active/abandoned states so a concurrent removal does not double-update.
func markWorktreeRemoved(ctx context.Context, tx db.TxRunner, repositoryID, worktreeID string) error {
	now := nowString()
	return tx.Exec(ctx, `
		UPDATE striatumd.job_worktrees
		   SET state = 'removed',
		       released_at = COALESCE(released_at, $1),
		       removed_at = $2
		 WHERE repository_id = $3
		   AND worktree_id = $4
		   AND state IN ('active', 'abandoned')`,
		now, now, repositoryID, worktreeID)
}

// quarantineLaneRef builds the durable, auditable quarantine ref for a lane
// worktree snapshot. It lives under refs/striatum/quarantine/<run>/<job>/<attempt>
// so it is reachable (survives git gc), auditable (a real commit log), and in the
// same refs/striatum namespace the durability/pin readers walk. Attempt numbers
// are 1-based; a missing/non-positive attempt normalizes to 1.
func quarantineLaneRef(runID, jobID string, attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("refs/striatum/quarantine/%s/%s/%d", runID, jobID, attempt)
}

// snapshotDirtyWorktree captures ALL uncommitted work in a worktree (tracked
// modifications, deletions, AND untracked files) into a single commit WITHOUT
// disturbing the lane's working tree or its index. It stages into a scratch
// index file (GIT_INDEX_FILE) seeded from HEAD, then write-tree + commit-tree
// with parent = worktree HEAD and the integrator identity (RFC 0127 retired the
// lane git identity, so the daemon supplies an explicit committer; mirrors
// integrate.go / fanInIntegrateRunBranch). The real index and working tree are
// never read or written, so the lane is left exactly as it was found.
func snapshotDirtyWorktree(ctx context.Context, worktreeRoot string) (string, error) {
	head, err := gitRevParseCommit(ctx, worktreeRoot, "HEAD")
	if err != nil {
		return "", err
	}

	scratchIndex, err := os.CreateTemp("", "striatum-quarantine-index-*")
	if err != nil {
		return "", rpc.NewError("git_commit_apply_failed", "could not create scratch index for quarantine snapshot: "+err.Error(), nil)
	}
	scratchPath := scratchIndex.Name()
	_ = scratchIndex.Close()
	// Start from an empty scratch index file; read-tree populates it from HEAD.
	_ = os.Remove(scratchPath)
	defer func() { _ = os.Remove(scratchPath) }()

	env := append(os.Environ(), "GIT_INDEX_FILE="+scratchPath)
	// Seed the scratch index from HEAD, then stage every change (-A: tracked
	// modifications, deletions, and untracked additions) into it.
	if out, exit, err := runGitWithEnv(ctx, worktreeRoot, env, "read-tree", head); err != nil {
		return "", err
	} else if exit != 0 {
		return "", rpc.NewError("git_commit_apply_failed", "read-tree of HEAD into scratch index failed: "+strings.TrimSpace(out), nil)
	}
	if out, exit, err := runGitWithEnv(ctx, worktreeRoot, env, "add", "-A"); err != nil {
		return "", err
	} else if exit != 0 {
		return "", rpc.NewError("git_commit_apply_failed", "staging changes into scratch index failed: "+strings.TrimSpace(out), nil)
	}
	treeOut, exit, err := runGitWithEnv(ctx, worktreeRoot, env, "write-tree")
	if err != nil {
		return "", err
	}
	if exit != 0 {
		return "", rpc.NewError("git_commit_apply_failed", "write-tree of scratch index failed: "+strings.TrimSpace(treeOut), nil)
	}
	tree := firstLine(treeOut)
	if !isFullGitSHA(tree) {
		return "", rpc.NewError("git_commit_apply_failed", fmt.Sprintf("quarantine write-tree produced invalid tree oid %q", tree), nil)
	}

	msg := "striatum quarantine-lane: snapshot of canceled-run lane worktree uncommitted work"
	commitOut, cexit, err := integrateGit(ctx, worktreeRoot,
		"-c", "user.name=striatum-integrator", "-c", "user.email=integrator@striatum.local",
		"commit-tree", tree, "-p", head, "-m", msg)
	if err != nil {
		return "", err
	}
	if cexit != 0 {
		return "", rpc.NewError("git_commit_apply_failed", "quarantine commit-tree failed: "+strings.TrimSpace(commitOut), nil)
	}
	commit := firstLine(commitOut)
	if !isFullGitSHA(commit) {
		return "", rpc.NewError("git_commit_apply_failed", fmt.Sprintf("quarantine commit-tree produced invalid commit oid %q", commit), nil)
	}
	return commit, nil
}

// quarantineWorktreeChangedPaths returns the set of uncommitted change paths in
// a worktree (tracked modifications, deletions, AND untracked additions), using
// the same NUL-delimited porcelain probe + parser the publish/write-scope guard
// uses (parseGitPorcelainZ). An empty result means the worktree is clean.
func quarantineWorktreeChangedPaths(ctx context.Context, worktreeRoot string) ([]string, error) {
	result, err := runGitWorktreeCommand(ctx, worktreeRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, rpc.NewError("git_commit_apply_failed", gitWorktreeErrorMessage("git status probe failed for quarantine", result), nil)
	}
	return parseGitPorcelainZ([]byte(result.Stdout)), nil
}

// runGitWithEnv runs a git command in repoRoot with extra environment (e.g.
// GIT_INDEX_FILE) layered on top of the daemon environment, returning combined
// output, exit code, and any non-exit error. It mirrors defaultRunGitWorktreeCommand
// but supports the scratch-index env the quarantine snapshot needs and never
// touches the real index.
func runGitWithEnv(ctx context.Context, repoRoot string, env []string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoRoot
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if strings.TrimSpace(out) == "" {
		out = stderr.String()
	}
	if err == nil {
		return out, 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out, exitErr.ExitCode(), nil
	}
	return out, 0, err
}
