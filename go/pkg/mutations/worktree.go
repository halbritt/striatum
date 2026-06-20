package mutations

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

const (
	worktreeStateDir = ".striatum"
	worktreeSubdir   = "worktrees"
	// workspacesSubdir holds RFC 0127 plain-dir workspaces (no .git), parallel
	// to worktreeSubdir which holds the legacy per-job git worktrees.
	workspacesSubdir = "workspaces"

	// workspaceKindPerJob is the legacy default: a per-job git worktree the lane
	// writes into. workspaceKindPlainDir is the RFC 0127 P0 opt-in: a plain,
	// daemon-owned, .git-free directory staged from the run-branch base.
	workspaceKindPerJob   = "per_job"
	workspaceKindPlainDir = "plain_dir"
)

type worktreeCreateInputs struct {
	Job        map[string]any
	Workflow   map[string]any
	RunID      string
	BaseBranch string
	// BranchBase is the run's recorded branch_base (the commit/ref the confirmed
	// branch should fork from). It may be empty for pre-RFC 0117 runs; the
	// ref-ensure path then falls back to HEAD (#183 compatibility).
	BranchBase string
}

type gitWorktreeResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

var runGitWorktreeCommand = defaultRunGitWorktreeCommand

func HandleWorktreeCreate(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredStringParam(envelope.Params, "session_id")
	if err != nil {
		return nil, err
	}
	jobID, err := requiredStringParam(envelope.Params, "job_id")
	if err != nil {
		return nil, err
	}
	leaseID, err := requiredStringParam(envelope.Params, "lease_id")
	if err != nil {
		return nil, err
	}
	// RFC 0127 P0 (D195): opt-in plain-dir workspace_kind. Default keeps the
	// legacy per-job git-worktree path unchanged; validate the enum before any DB
	// or filesystem work.
	workspaceKind := strings.TrimSpace(stringParam(envelope, "workspace_kind"))
	if workspaceKind == "" {
		workspaceKind = workspaceKindPerJob
	}
	if workspaceKind != workspaceKindPerJob && workspaceKind != workspaceKindPlainDir {
		return nil, rpc.NewError("schema_invalid", fmt.Sprintf("workspace_kind must be %q or %q", workspaceKindPerJob, workspaceKindPlainDir), nil)
	}
	repoRoot, err := activeRepositoryRoot(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}

	var inputs worktreeCreateInputs
	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		validated, err := validatedWorktreeCreateInputs(ctx, tx, repositoryID, sessionID, jobID, leaseID)
		if err != nil {
			return nil, err
		}
		inputs = validated
		return map[string]any{}, nil
	}); err != nil {
		return nil, err
	}

	if workspaceKind == workspaceKindPlainDir {
		return handlePlainDirWorkspaceCreate(ctx, runner, repositoryID, sessionID, jobID, leaseID, repoRoot, inputs)
	}

	worktreeID, err := newID("wt")
	if err != nil {
		return nil, err
	}
	relative := filepath.ToSlash(filepath.Join(worktreeStateDir, worktreeSubdir, worktreeID))
	target, err := worktreeTarget(repoRoot, relative)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, err
	}

	if err := ensureWorktreeBaseBranchRef(ctx, repoRoot, inputs.BaseBranch, inputs.BranchBase); err != nil {
		return nil, err
	}

	result, err := runGitWorktreeCommand(ctx, repoRoot, "worktree", "add", "--detach", target, inputs.BaseBranch)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, rpc.NewError("invalid_transition", gitWorktreeErrorMessage("git worktree add failed", result), nil)
	}

	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		validated, err := validatedWorktreeCreateInputs(ctx, tx, repositoryID, sessionID, jobID, leaseID)
		if err != nil {
			return nil, err
		}
		now := nowString()
		if err := tx.Exec(ctx, `
			INSERT INTO striatumd.job_worktrees (
			  repository_id, worktree_id, run_id, job_id, lease_id,
			  base_branch, worktree_path, state, created_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'active',$8)`,
			repositoryID,
			worktreeID,
			validated.RunID,
			jobID,
			leaseID,
			validated.BaseBranch,
			relative,
			now,
		); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, validated.RunID, "worktree.created", sessionID, jobID, nil, nil, leaseID, map[string]any{
			"worktree_id":   worktreeID,
			"worktree_path": relative,
			"base_branch":   validated.BaseBranch,
		}); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	}); err != nil {
		return nil, err
	}

	memoryDigest := maybeRenderRecallShelf(ctx, runner, packageRecallDigestOptions, repositoryID, inputs, target)

	return map[string]any{
		"worktree_id":   worktreeID,
		"worktree_path": relative,
		"base_branch":   inputs.BaseBranch,
		"memory_digest": memoryDigest,
	}, nil
}

// ensureWorktreeBaseBranchRef makes the confirmed branch ref exist before
// `git worktree add --detach <branch>` resolves it. Pre-RFC 0117 runs may have
// a confirmed branch name but no actual ref (#183), so the create path repairs
// that state with `git branch <name> <base>` and never `git checkout -b`.
func ensureWorktreeBaseBranchRef(ctx context.Context, repoRoot, branch, branchBase string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil
	}
	// Already a resolvable ref (branch, tag, or commit)? Nothing to do.
	verify, err := runGitWorktreeCommand(ctx, repoRoot, "rev-parse", "--verify", "--quiet", branch+"^{commit}")
	if err != nil {
		return err
	}
	if verify.ExitCode == 0 {
		return nil
	}
	base := strings.TrimSpace(branchBase)
	if base == "" {
		base = "HEAD"
	}
	created, err := runGitWorktreeCommand(ctx, repoRoot, "branch", branch, base)
	if err != nil {
		return err
	}
	if created.ExitCode != 0 {
		// A concurrent worktree create for the same run may have created the
		// branch between our probe and this create; if the ref resolves now,
		// the goal (ref exists) is met — losing that race is not a failure.
		verify, verr := runGitWorktreeCommand(ctx, repoRoot, "rev-parse", "--verify", "--quiet", branch+"^{commit}")
		if verr == nil && verify.ExitCode == 0 {
			return nil
		}
		return rpc.NewError("invalid_transition", gitWorktreeErrorMessage("git branch create for worktree base failed", created), nil)
	}
	return nil
}

func HandleWorktreeRelease(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	worktreeID, err := requiredStringParam(envelope.Params, "worktree_id")
	if err != nil {
		return nil, err
	}
	force := boolParam(envelope, "force")
	repoRoot, err := activeRepositoryRoot(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}

	var row map[string]any
	var job map[string]any
	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		loaded, err := worktreeRow(ctx, tx, repositoryID, worktreeID, false)
		if err != nil {
			return nil, err
		}
		row = loaded
		loadedJob, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", fmt.Sprint(row["job_id"]), false)
		if err != nil {
			return nil, err
		}
		job = loadedJob
		return map[string]any{}, nil
	}); err != nil {
		return nil, err
	}
	if fmt.Sprint(row["state"]) != "active" {
		return map[string]any{
			"status":      "already_released",
			"worktree_id": worktreeID,
			"state":       fmt.Sprint(row["state"]),
		}, nil
	}

	target, err := worktreeTarget(repoRoot, fmt.Sprint(row["worktree_path"]))
	if err != nil {
		return nil, err
	}
	if !pathExists(target) {
		if force && terminalJobStates[fmt.Sprint(job["state"])] {
			return forceReleaseMissingWorktree(ctx, runner, repositoryID, worktreeID, repoRoot)
		}
		if force {
			return nil, missingWorktreeRequiresTerminalJobError(worktreeID, row, job)
		}
		return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
			"worktree %s path is missing on disk; pass --force after confirming the owning job is terminal to retire the row",
			worktreeID,
		), map[string]any{
			"worktree_id":   worktreeID,
			"worktree_path": row["worktree_path"],
			"job_id":        job["job_id"],
			"job_state":     job["state"],
			"force":         false,
		})
	}
	reachability, err := worktreeHeadReachability(ctx, repoRoot, target, row)
	if err != nil {
		return nil, err
	}
	if !reachability.Reachable && !force {
		return nil, rpc.NewError("worktree_head_unreachable", fmt.Sprintf(
			"worktree %s HEAD %s is not reachable from the run branch or refs/striatum pins; re-run work.complete to anchor it, or pass --force to discard it explicitly",
			worktreeID, reachability.Head,
		), map[string]any{
			"worktree_id":  worktreeID,
			"head":         reachability.Head,
			"checked_refs": reachability.CheckedRefs,
		})
	}
	problems, err := publishedArtifactDurabilityProblems(ctx, runner, artifactDurabilityCheck{
		RepositoryID: repositoryID,
		RepoRoot:     repoRoot,
		Job:          job,
		Worktree:     row,
	})
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, publishedArtifactsNotDurableError("worktree.release", job, problems)
	}
	result, err := runGitWorktreeCommand(ctx, repoRoot, "worktree", "remove", "--force", target)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 && pathExists(target) {
		return nil, rpc.NewError("invalid_transition", gitWorktreeErrorMessage("git worktree remove failed", result), nil)
	}

	var releaseResult map[string]any
	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		loaded, err := worktreeRow(ctx, tx, repositoryID, worktreeID, true)
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(loaded["state"]) != "active" {
			releaseResult = map[string]any{
				"status":      "already_released",
				"worktree_id": worktreeID,
				"state":       fmt.Sprint(loaded["state"]),
			}
			return map[string]any{}, nil
		}
		now := nowString()
		if err := tx.Exec(ctx, `
			UPDATE striatumd.job_worktrees
			   SET state = 'removed', released_at = $1, removed_at = $2
			 WHERE repository_id = $3 AND worktree_id = $4`,
			now,
			now,
			repositoryID,
			worktreeID,
		); err != nil {
			return nil, err
		}
		eventType := "worktree.released"
		status := "released"
		if force && !reachability.Reachable {
			eventType = "worktree.force_released"
			status = "force_released"
		}
		if _, err := appendEvent(ctx, tx, repositoryID, loaded["run_id"], eventType, nil, loaded["job_id"], nil, nil, loaded["lease_id"], map[string]any{
			"worktree_id":   worktreeID,
			"worktree_path": fmt.Sprint(loaded["worktree_path"]),
			"head":          nullableString(reachability.Head),
			"reachable":     reachability.Reachable,
			"checked_refs":  reachability.CheckedRefs,
		}); err != nil {
			return nil, err
		}
		releaseResult = map[string]any{
			"status":      status,
			"worktree_id": worktreeID,
			"state":       "removed",
		}
		return map[string]any{}, nil
	}); err != nil {
		return nil, err
	}
	return releaseResult, nil
}

func forceReleaseMissingWorktree(ctx context.Context, runner db.Runner, repositoryID, worktreeID, repoRoot string) (map[string]any, error) {
	var releaseResult map[string]any
	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		loaded, err := worktreeRow(ctx, tx, repositoryID, worktreeID, true)
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(loaded["state"]) != "active" {
			releaseResult = map[string]any{
				"status":      "already_released",
				"worktree_id": worktreeID,
				"state":       fmt.Sprint(loaded["state"]),
			}
			return map[string]any{}, nil
		}
		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", fmt.Sprint(loaded["job_id"]), true)
		if err != nil {
			return nil, err
		}
		if !terminalJobStates[fmt.Sprint(job["state"])] {
			return nil, missingWorktreeRequiresTerminalJobError(worktreeID, loaded, job)
		}
		target, err := worktreeTarget(repoRoot, fmt.Sprint(loaded["worktree_path"]))
		if err != nil {
			return nil, err
		}
		if pathExists(target) {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
				"worktree %s path exists on disk; retry worktree release so reachability and artifact durability can be checked",
				worktreeID,
			), map[string]any{
				"worktree_id":   worktreeID,
				"worktree_path": loaded["worktree_path"],
				"job_id":        job["job_id"],
				"job_state":     job["state"],
				"force":         true,
			})
		}
		now := nowString()
		if err := tx.Exec(ctx, `
			UPDATE striatumd.job_worktrees
			   SET state = 'removed', released_at = $1, removed_at = $2
			 WHERE repository_id = $3 AND worktree_id = $4`,
			now,
			now,
			repositoryID,
			worktreeID,
		); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, loaded["run_id"], "worktree.force_released", nil, loaded["job_id"], nil, nil, loaded["lease_id"], map[string]any{
			"worktree_id":     worktreeID,
			"worktree_path":   fmt.Sprint(loaded["worktree_path"]),
			"missing_on_disk": true,
			"head":            nil,
			"reachable":       false,
			"checked_refs":    []string{},
			"job_state":       fmt.Sprint(job["state"]),
		}); err != nil {
			return nil, err
		}
		releaseResult = map[string]any{
			"status":          "force_released",
			"worktree_id":     worktreeID,
			"state":           "removed",
			"missing_on_disk": true,
		}
		return map[string]any{}, nil
	}); err != nil {
		return nil, err
	}
	return releaseResult, nil
}

func missingWorktreeRequiresTerminalJobError(worktreeID string, worktree, job map[string]any) *rpc.Error {
	return rpc.NewError("invalid_transition", fmt.Sprintf(
		"worktree %s path is missing on disk and job %s is %s; missing-on-disk release requires a terminal job",
		worktreeID, job["job_id"], job["state"],
	), map[string]any{
		"worktree_id":   worktreeID,
		"worktree_path": worktree["worktree_path"],
		"job_id":        job["job_id"],
		"job_state":     job["state"],
		"force":         true,
	})
}

type worktreeAnchorTarget struct {
	Worktree  map[string]any
	RunBranch string
	Attempt   int
}

func HandleWorktreeAnchor(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID, err := requiredStringParam(envelope.Params, "run_id")
	if err != nil {
		return nil, err
	}
	jobID, err := requiredStringParam(envelope.Params, "job_id")
	if err != nil {
		return nil, err
	}
	worktreeID, err := requiredStringParam(envelope.Params, "worktree_id")
	if err != nil {
		return nil, err
	}
	repoRoot, err := activeRepositoryRoot(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}

	target, err := loadCompletedWorktreeAnchorTarget(ctx, runner, repositoryID, runID, jobID, worktreeID, false)
	if err != nil {
		return nil, err
	}
	worktreeRoot, err := worktreeTarget(repoRoot, fmt.Sprint(target.Worktree["worktree_path"]))
	if err != nil {
		return nil, err
	}
	if !pathExists(worktreeRoot) {
		return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
			"worktree %s path is missing on disk; cannot anchor completed job %s",
			worktreeID, jobID,
		), map[string]any{
			"run_id":        runID,
			"job_id":        jobID,
			"worktree_id":   worktreeID,
			"worktree_path": target.Worktree["worktree_path"],
		})
	}

	anchorPayload, err := anchorWorktreeCommitStack(ctx, repoRoot, runID, jobID, target.RunBranch, target.Worktree, target.Attempt)
	if err != nil {
		return nil, err
	}
	anchorPayload["remediation"] = "worktree.anchor"

	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		locked, err := loadCompletedWorktreeAnchorTarget(ctx, tx, repositoryID, runID, jobID, worktreeID, true)
		if err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "worktree.anchored", nil, jobID, nil, nil, locked.Worktree["lease_id"], anchorPayload); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	}); err != nil {
		return nil, err
	}

	return map[string]any{
		"status":        "anchored",
		"run_id":        runID,
		"job_id":        jobID,
		"worktree_id":   worktreeID,
		"worktree_path": target.Worktree["worktree_path"],
		"anchor":        anchorPayload,
	}, nil
}

type worktreeGCCheck struct {
	Remove        bool
	Reason        string
	Target        string
	MissingOnDisk bool
	Reachability  worktreeReachability
	// DirtyPaths is the set of uncommitted change paths when a worktree is
	// skipped with reason dirty_uncommitted_work (#298); empty otherwise.
	DirtyPaths []string
}

func HandleWorktreeGC(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(stringParam(envelope, "run_id"))
	sweepPins := boolParam(envelope, "sweep_pins")
	if sweepPins && runID == "" {
		return nil, rpc.NewError("schema_invalid", "worktree.gc sweep_pins requires run_id to scope the pin sweep to a single run", nil)
	}

	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		if err := lockRepo(ctx, tx, repositoryID); err != nil {
			return nil, err
		}
		args := []any{repositoryID}
		where := "WHERE w.repository_id = $1 AND w.state IN ('active', 'abandoned')"
		if runID != "" {
			args = append(args, runID)
			where += " AND w.run_id = $2"
		}
		rows, err := queryRows(ctx, tx, `
			SELECT w.worktree_id, w.run_id, w.job_id, w.lease_id,
			       w.base_branch, w.worktree_path, w.state,
			       j.state AS job_state, j.workflow_job_id, j.attempt,
			       r.repo_root, r.branch_name
			  FROM striatumd.job_worktrees w
			  JOIN striatumd.jobs j
			    ON j.repository_id = w.repository_id
			   AND j.job_id = w.job_id
			  JOIN striatumd.runs r
			    ON r.repository_id = w.repository_id
			   AND r.run_id = w.run_id
			  `+where+`
			 ORDER BY w.created_at, w.worktree_id
			 FOR UPDATE OF w`, args...)
		if err != nil {
			return nil, err
		}

		removed := []map[string]any{}
		skipped := []map[string]any{}
		for _, row := range rows {
			repoRoot := strings.TrimSpace(fmt.Sprint(row["repo_root"]))
			check, err := worktreeGCDecision(ctx, repoRoot, row)
			base := map[string]any{
				"worktree_id":     row["worktree_id"],
				"worktree_path":   row["worktree_path"],
				"run_id":          row["run_id"],
				"job_id":          row["job_id"],
				"workflow_job_id": row["workflow_job_id"],
				"state":           row["state"],
				"job_state":       row["job_state"],
			}
			if err != nil {
				base["reason"] = check.Reason
				if check.Reason == "" {
					base["reason"] = "probe_failed"
				}
				base["error"] = err.Error()
				skipped = append(skipped, base)
				continue
			}
			if !check.Remove {
				base["reason"] = check.Reason
				base["head"] = nullableString(check.Reachability.Head)
				base["reachable"] = check.Reachability.Reachable
				base["checked_refs"] = check.Reachability.CheckedRefs
				// #298: surface the uncommitted paths and the deferred recovery verb
				// so the operator knows the worktree was preserved (not discarded) and
				// how to dispose of its dirt without manual capture.
				if check.Reason == "dirty_uncommitted_work" {
					base["dirty_paths"] = check.DirtyPaths
					base["next_action"] = "recovery quarantine-lane <run-id> <job-id>"
				}
				skipped = append(skipped, base)
				continue
			}
			if !check.MissingOnDisk {
				problems, err := publishedArtifactDurabilityProblems(ctx, tx, artifactDurabilityCheck{
					RepositoryID: repositoryID,
					RepoRoot:     repoRoot,
					Job:          row,
					Worktree:     row,
				})
				if err != nil {
					return nil, err
				}
				if len(problems) > 0 {
					base["reason"] = "published_artifact_not_durable"
					base["head"] = nullableString(check.Reachability.Head)
					base["reachable"] = check.Reachability.Reachable
					base["checked_refs"] = check.Reachability.CheckedRefs
					base["artifacts"] = publishedArtifactDurabilityProblemMaps(problems)
					skipped = append(skipped, base)
					continue
				}

				result, err := runGitWorktreeCommand(ctx, repoRoot, "worktree", "remove", "--force", check.Target)
				if err != nil {
					return nil, err
				}
				if result.ExitCode != 0 && pathExists(check.Target) {
					return nil, rpc.NewError("invalid_transition", gitWorktreeErrorMessage("git worktree remove failed", result), nil)
				}
			}
			now := nowString()
			if err := tx.Exec(ctx, `
				UPDATE striatumd.job_worktrees
				   SET state = 'removed',
				       released_at = COALESCE(released_at, $1),
				       removed_at = $2
				 WHERE repository_id = $3
				   AND worktree_id = $4
				   AND state IN ('active', 'abandoned')`,
				now, now, repositoryID, row["worktree_id"]); err != nil {
				return nil, err
			}
			if _, err := appendEvent(ctx, tx, repositoryID, row["run_id"], "worktree.gc_removed", nil, row["job_id"], nil, nil, row["lease_id"], map[string]any{
				"worktree_id":     row["worktree_id"],
				"worktree_path":   row["worktree_path"],
				"head":            nullableString(check.Reachability.Head),
				"reachable":       check.Reachability.Reachable,
				"checked_refs":    check.Reachability.CheckedRefs,
				"job_state":       row["job_state"],
				"missing_on_disk": check.MissingOnDisk,
			}); err != nil {
				return nil, err
			}
			base["status"] = "removed"
			base["missing_on_disk"] = check.MissingOnDisk
			if check.Reason != "" {
				base["reason"] = check.Reason
			}
			base["head"] = nullableString(check.Reachability.Head)
			base["reachable"] = check.Reachability.Reachable
			base["checked_refs"] = check.Reachability.CheckedRefs
			removed = append(removed, base)
		}
		response := map[string]any{
			"status":        "ok",
			"removed_count": len(removed),
			"skipped_count": len(skipped),
			"removed":       removed,
			"skipped":       skipped,
		}
		if sweepPins {
			run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, false)
			if err != nil {
				return nil, err
			}
			repoRoot := strings.TrimSpace(fmt.Sprint(run["repo_root"]))
			deleted, retained, err := sweepRunPins(ctx, repoRoot, runID,
				strings.TrimSpace(fmt.Sprint(run["branch_name"])),
				strings.TrimSpace(fmt.Sprint(run["branch_base"])))
			if err != nil {
				return nil, err
			}
			if _, err := appendEvent(ctx, tx, repositoryID, runID, "worktree.pins_swept", nil, nil, nil, nil, nil, map[string]any{
				"deleted":        deleted,
				"retained":       retained,
				"deleted_count":  len(deleted),
				"retained_count": len(retained),
			}); err != nil {
				return nil, err
			}
			response["pins_swept"] = true
			response["pins_deleted"] = deleted
			response["pins_retained"] = retained
			response["pins_deleted_count"] = len(deleted)
			response["pins_retained_count"] = len(retained)
		}
		return response, nil
	})
}

// sweepRunPins implements #214 (RFC 0117 Open Question 1): on explicit run
// closeout it deletes refs/striatum pins whose tip is already reachable from the
// run's integrated history (the run branch or its integration base), and retains
// divergent pins — commits that exist only under the pin — for operator
// inspection. It resolves both the attempt-namespaced and legacy ref shapes,
// never deletes a divergent pin, and never runs git gc. The compare-and-delete
// against the evaluated tip keeps the sweep idempotent and safe against a
// concurrent re-anchor.
func sweepRunPins(ctx context.Context, repoRoot, runID, runBranch, baseBranch string) (deleted, retained []map[string]any, err error) {
	integratedRefs := []string{}
	seenRef := map[string]bool{}
	for _, branch := range []string{runBranch, baseBranch} {
		branch = strings.TrimSpace(branch)
		if branch == "" || branch == "<nil>" {
			continue
		}
		ref := "refs/heads/" + branch
		if seenRef[ref] {
			continue
		}
		if _, exit, e := integrateGit(ctx, repoRoot, "rev-parse", "--verify", "--quiet", ref); e == nil && exit == 0 {
			integratedRefs = append(integratedRefs, ref)
			seenRef[ref] = true
		}
	}

	out, exit, err := integrateGit(ctx, repoRoot, "for-each-ref", "--format=%(refname) %(objectname)", "refs/striatum/"+runID+"/")
	if err != nil {
		return nil, nil, err
	}
	if exit != 0 {
		return nil, nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("for-each-ref of refs/striatum/%s/ failed while sweeping pins: %s", runID, strings.TrimSpace(out)), nil)
	}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		ref, tip := parts[0], parts[1]
		reachable := false
		via := ""
		for _, integratedRef := range integratedRefs {
			ok, e := gitIsAncestor(ctx, repoRoot, tip, integratedRef)
			if e != nil {
				return nil, nil, e
			}
			if ok {
				reachable = true
				via = integratedRef
				break
			}
		}
		entry := map[string]any{"ref": ref, "tip": tip}
		if !reachable {
			entry["reason"] = "divergent"
			retained = append(retained, entry)
			continue
		}
		dout, dexit, derr := integrateGit(ctx, repoRoot, "update-ref", "-d", ref, tip)
		if derr != nil {
			return nil, nil, derr
		}
		if dexit != 0 {
			entry["reason"] = "delete_failed"
			entry["detail"] = strings.TrimSpace(dout)
			retained = append(retained, entry)
			continue
		}
		entry["reason"] = "reachable_from"
		entry["integrated_ref"] = via
		deleted = append(deleted, entry)
	}
	return deleted, retained, nil
}

func worktreeGCDecision(ctx context.Context, repoRoot string, row map[string]any) (worktreeGCCheck, error) {
	if !terminalJobStates[fmt.Sprint(row["job_state"])] {
		return worktreeGCCheck{Reason: "job_not_terminal"}, nil
	}
	target, err := worktreeTarget(repoRoot, fmt.Sprint(row["worktree_path"]))
	if err != nil {
		return worktreeGCCheck{Reason: "invalid_path"}, err
	}
	if !pathExists(target) {
		return worktreeGCCheck{Remove: true, Reason: "missing_on_disk", Target: target, MissingOnDisk: true}, nil
	}
	reachability, err := worktreeHeadReachability(ctx, repoRoot, target, row)
	if err != nil {
		return worktreeGCCheck{Reason: "probe_failed", Target: target}, err
	}
	if !reachability.Reachable {
		return worktreeGCCheck{Reason: "head_unreachable", Target: target, Reachability: reachability}, nil
	}
	// #298: a present-on-disk worktree with uncommitted work is NOT safe to
	// `worktree remove --force` — that silently DISCARDS the dirt (the
	// silent-data-loss this guard closes). Skip it with a distinct reason and
	// defer disposition to `recovery quarantine-lane`, which snapshots the
	// uncommitted work to a durable quarantine ref BEFORE removing the worktree.
	// Probed only after the cheaper terminal/reachability gates so the common
	// clean-worktree path pays nothing extra.
	changedPaths, err := quarantineWorktreeChangedPaths(ctx, target)
	if err != nil {
		return worktreeGCCheck{Reason: "probe_failed", Target: target, Reachability: reachability}, err
	}
	if len(changedPaths) > 0 {
		return worktreeGCCheck{Reason: "dirty_uncommitted_work", Target: target, Reachability: reachability, DirtyPaths: changedPaths}, nil
	}
	return worktreeGCCheck{Remove: true, Target: target, Reachability: reachability}, nil
}

func loadCompletedWorktreeAnchorTarget(ctx context.Context, runner any, repositoryID, runID, jobID, worktreeID string, forUpdate bool) (worktreeAnchorTarget, error) {
	run, err := rowByIDOrNotFound(ctx, runner, repositoryID, "runs", "run_id", runID, forUpdate, "run")
	if err != nil {
		return worktreeAnchorTarget{}, err
	}
	job, err := rowByIDOrNotFound(ctx, runner, repositoryID, "jobs", "job_id", jobID, forUpdate, "job")
	if err != nil {
		return worktreeAnchorTarget{}, err
	}
	if fmt.Sprint(job["run_id"]) != runID {
		return worktreeAnchorTarget{}, rpc.NewError("invalid_transition", fmt.Sprintf(
			"job %s belongs to run %s, not %s",
			jobID, job["run_id"], runID,
		), map[string]any{
			"run_id":          runID,
			"job_id":          jobID,
			"actual_run_id":   job["run_id"],
			"expected_run_id": runID,
		})
	}
	if state := fmt.Sprint(job["state"]); state != "completed" {
		return worktreeAnchorTarget{}, rpc.NewError("invalid_transition", fmt.Sprintf(
			"worktree anchor requires completed job %s; job is %s",
			jobID, state,
		), map[string]any{"run_id": runID, "job_id": jobID, "job_state": state})
	}
	if !isRepoWrite(job) {
		return worktreeAnchorTarget{}, rpc.NewError("invalid_transition", fmt.Sprintf(
			"worktree anchor requires a repo-write job; job %s is not repo-write",
			jobID,
		), map[string]any{"run_id": runID, "job_id": jobID})
	}
	worktree, err := worktreeRow(ctx, runner, repositoryID, worktreeID, forUpdate)
	if err != nil {
		return worktreeAnchorTarget{}, err
	}
	if fmt.Sprint(worktree["run_id"]) != runID || fmt.Sprint(worktree["job_id"]) != jobID {
		return worktreeAnchorTarget{}, rpc.NewError("invalid_transition", fmt.Sprintf(
			"worktree %s belongs to run %s job %s, not run %s job %s",
			worktreeID, worktree["run_id"], worktree["job_id"], runID, jobID,
		), map[string]any{
			"worktree_id":     worktreeID,
			"actual_run_id":   worktree["run_id"],
			"actual_job_id":   worktree["job_id"],
			"expected_run_id": runID,
			"expected_job_id": jobID,
		})
	}
	worktreeState := fmt.Sprint(worktree["state"])
	if worktreeState != "active" && worktreeState != "abandoned" {
		return worktreeAnchorTarget{}, rpc.NewError("invalid_transition", fmt.Sprintf(
			"worktree %s is %s; only active or abandoned worktrees can be anchored",
			worktreeID, worktreeState,
		), map[string]any{"worktree_id": worktreeID, "state": worktreeState})
	}
	runBranch := strings.TrimSpace(fmt.Sprint(run["branch_name"]))
	if runBranch == "" || runBranch == "<nil>" || nullable(run["branch_confirmed_at"]) == nil {
		return worktreeAnchorTarget{}, rpc.NewError("invalid_transition", "run branch must be confirmed before worktree anchor remediation", map[string]any{"run_id": runID})
	}
	return worktreeAnchorTarget{
		Worktree:  worktree,
		RunBranch: runBranch,
		Attempt:   intValue(job["attempt"]),
	}, nil
}

func rowByIDOrNotFound(ctx context.Context, runner any, repositoryID, table, column, value string, forUpdate bool, label string) (map[string]any, error) {
	row, err := rowByID(ctx, runner, repositoryID, table, column, value, forUpdate)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, rpc.NewError("not_found", fmt.Sprintf("%s not found: %s", label, value), map[string]any{
			"resource_type": label,
			"resource_id":   value,
		})
	}
	return row, err
}

func anchorActiveWorktreeForJob(ctx context.Context, runner any, repositoryID string, job map[string]any) (map[string]any, error) {
	if !isRepoWrite(job) {
		return nil, nil
	}
	required, worktree, err := worktreeRequirementForJob(ctx, runner, repositoryID, job)
	if err != nil {
		return nil, err
	}
	if required && worktree == nil {
		return nil, worktreeRequiredError(job, "work.complete")
	}
	if worktree == nil {
		return nil, err
	}
	repoRoot, err := activeRepositoryRoot(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return nil, err
	}
	runBranch := strings.TrimSpace(fmt.Sprint(run["branch_name"]))
	if runBranch == "" || runBranch == "<nil>" || nullable(run["branch_confirmed_at"]) == nil {
		return nil, rpc.NewError("invalid_transition", "repo-write worktree commits require a confirmed run branch before completion", nil)
	}
	runID := fmt.Sprint(job["run_id"])
	jobID := fmt.Sprint(job["job_id"])
	attempt := intValue(job["attempt"])
	payload, err := anchorWorktreeCommitStack(ctx, repoRoot, runID, jobID, runBranch, worktree, attempt)
	if err != nil {
		return nil, err
	}
	// RFC 0135 P1 (#354, D246) — the staging-at-completion hook. SHADOW: it is a
	// strict no-op unless the fan-in fold is opted in (STRIATUM_BARRIER_FANIN=1) AND
	// the completing seat is a declared in-edge of a recorded fan-in freeze point.
	// Until recordFaninFreezePoint is wired into a live fan-out, no run declares a
	// fan-in, so this never stages on any current run and the default per-completion
	// merge (done above by anchorWorktreeCommitStack) remains the sole fan-in path.
	// It is ADDITIVE to the merge: it records the durable staging witness without
	// changing how the run branch advanced. The per-job worktree HEAD the anchor
	// recorded (payload["head"]) is the staged contribution commit.
	if barrierFaninAssemblyEnabled() {
		if tx, ok := runner.(db.TxRunner); ok {
			workflowJobID := strings.TrimSpace(fmt.Sprint(job["workflow_job_id"]))
			head := strings.TrimSpace(fmt.Sprint(payload["head"]))
			if workflowJobID != "" && workflowJobID != "<nil>" && isFullGitSHA(head) {
				stagedRef, serr := stageFaninContributionAtCompletion(ctx, tx, repoRoot, repositoryID, runID, workflowJobID, jobID, head, attempt)
				if serr != nil {
					return nil, serr
				}
				if stagedRef != "" {
					payload["fanin_staged_ref"] = stagedRef
				}
			}
		}
	}
	return payload, nil
}

func requireActiveWorktreeForJob(ctx context.Context, runner any, repositoryID string, job map[string]any, surface string) (map[string]any, error) {
	required, worktree, err := worktreeRequirementForJob(ctx, runner, repositoryID, job)
	if err != nil {
		return nil, err
	}
	if required && worktree == nil {
		return nil, worktreeRequiredError(job, surface)
	}
	return worktree, nil
}

func worktreeRequirementForJob(ctx context.Context, runner any, repositoryID string, job map[string]any) (bool, map[string]any, error) {
	if !isRepoWrite(job) {
		return false, nil, nil
	}
	worktree, err := activeWorktreeForJob(ctx, runner, repositoryID, fmt.Sprint(job["job_id"]))
	if err != nil {
		return false, nil, err
	}
	// #310: the claim packet resolves the lane via a session fallback when the job
	// row's lane_selector_json carries no lane_id (claim.go: laneID = session
	// lane_id), so the lane believes it is per-job isolated and supervises as
	// `striatum-lane`. The publish-time gate historically read ONLY the job
	// selector here, so an empty selector made the per-job worktree NOT required —
	// and artifactSourcePath then resolved the write target to repoRoot, letting a
	// repo-write lane write straight into the operator's shared, tracked checkout
	// (bypassing the RFC 0125 porter, which only writes INSIDE the worktree). Mirror
	// the claim-time fallback so the per-job worktree is correctly required.
	laneID, err := jobLaneIDWithSessionFallback(ctx, runner, repositoryID, job)
	if err != nil {
		return false, nil, err
	}
	if strings.TrimSpace(laneID) == "" {
		return false, worktree, nil
	}
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return false, nil, err
	}
	snapshotID := strings.TrimSpace(fmt.Sprint(run["workflow_snapshot_id"]))
	if snapshotID == "" || snapshotID == "<nil>" {
		return false, worktree, nil
	}
	snapshot, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", snapshotID, false)
	if err != nil {
		return false, nil, err
	}
	workflow := asMap(snapshot["workflow_json"])
	required := laneWorktreeIsolation(workflow, laneID) == "per_job"
	return required, worktree, nil
}

func worktreeRequiredError(job map[string]any, surface string) error {
	jobID := strings.TrimSpace(fmt.Sprint(job["job_id"]))
	workflowJobID := strings.TrimSpace(fmt.Sprint(job["workflow_job_id"]))
	laneID := strings.TrimSpace(jobLaneID(job))
	message := fmt.Sprintf("%s requires an active per-job worktree for repo-write job %s before touching repository files", surface, jobID)
	if workflowJobID != "" && workflowJobID != "<nil>" {
		message += fmt.Sprintf(" (%s)", workflowJobID)
	}
	message += "; run worktree.create with the active session/job/lease from the work packet, then retry"
	return rpc.NewError("worktree_required", message, map[string]any{
		"job_id":          jobID,
		"workflow_job_id": nullableString(workflowJobID),
		"lane_id":         nullableString(laneID),
		"surface":         surface,
		"required_action": "worktree.create",
	})
}

func anchorWorktreeCommitStack(ctx context.Context, repoRoot, runID, jobID, runBranch string, worktree map[string]any, attempt int) (map[string]any, error) {
	target, err := worktreeTarget(repoRoot, fmt.Sprint(worktree["worktree_path"]))
	if err != nil {
		return nil, err
	}
	head, err := gitRevParseCommit(ctx, target, "HEAD")
	if err != nil {
		return nil, err
	}
	runRef := "refs/heads/" + runBranch
	payload := map[string]any{
		"anchor":        "none",
		"head":          head,
		"worktree_id":   worktree["worktree_id"],
		"worktree_path": worktree["worktree_path"],
		"run_branch":    runBranch,
		"run_ref":       runRef,
	}
	runTip, err := gitRevParseCommit(ctx, repoRoot, runRef)
	if err != nil {
		payload["run_branch_missing"] = true
		return pinWorktreeCommitStack(ctx, repoRoot, runID, jobID, head, payload, attempt)
	}
	if head == runTip {
		payload["reason"] = "head_already_at_run_branch"
		return payload, nil
	}
	if ok, err := gitIsAncestor(ctx, repoRoot, runTip, head); err != nil {
		return nil, err
	} else if ok {
		out, exit, err := integrateGit(ctx, repoRoot, "update-ref", runRef, head, runTip)
		if err != nil {
			return nil, err
		}
		if exit == 0 {
			payload["anchor"] = "run_branch_ff"
			payload["from"] = runTip
			payload["to"] = head
			return payload, nil
		}
		payload["ff_failed"] = strings.TrimSpace(out)
	}
	// #290: head cannot fast-forward the run branch — a concurrent fan-in sibling
	// advanced it after this worktree forked from the run-branch tip. Do NOT leave
	// this sibling stranded under a pin only (the original bug: a downstream job's
	// worktree, seeded from the run branch, never sees the pinned stack). Integrate
	// the sibling's disjoint subtree into the run branch with a conflict-free
	// content merge (parallel fan-in lanes are authored to write disjoint scopes,
	// RFC 0101; an overlap is surfaced LOUDLY, never silently last-writer-wins),
	// then ALSO pin the exact commit stack for provenance — now reachable from the
	// run-branch HEAD (the merge commit's second parent), satisfying the fan-in
	// reachability invariant doctor asserts.
	merge, err := fanInIntegrateRunBranch(ctx, repoRoot, runRef, runTip, head, jobID, attempt)
	if err != nil {
		return nil, err
	}
	pinned, err := pinWorktreeCommitStack(ctx, repoRoot, runID, jobID, head, payload, attempt)
	if err != nil {
		return nil, err
	}
	pinned["anchor"] = "run_branch_fanin_merge"
	pinned["fanin_merge"] = merge
	return pinned, nil
}

// fanInIntegrateRunBranch advances the run branch to include a fan-in sibling's
// commit stack (head) that can no longer fast-forward it, WITHOUT a worktree
// checkout (it operates on the object database). It is the per-completion form of
// the RFC 0101 / #290 join: the run branch is the single integration point, so a
// downstream job's worktree (seeded from the run branch after its upstream
// siblings complete) sees every sibling under any completion order.
//
//   - If head became fast-forwardable after a re-read (it lost the earlier FF
//     compare-and-swap race), advance the ref to head directly.
//   - Otherwise perform a 3-way content merge via `git merge-tree --write-tree`.
//     Because parallel fan-in lanes write disjoint subtrees, the merge has no
//     overlapping changes; if it DOES conflict (two siblings wrote the same path)
//     that is surfaced as a loud `git_commit_apply_failed` rather than silently
//     resolved to a last writer, which would re-strand one sibling's work — the
//     exact failure mode #290 exists to remove.
//
// The run ref is advanced with a compare-and-swap `update-ref <new> <expected>`
// and retried against concurrent sibling movement; if a concurrent integration
// already made head reachable, it returns idempotently.
func fanInIntegrateRunBranch(ctx context.Context, repoRoot, runRef, runTip, head, jobID string, attempt int) (map[string]any, error) {
	const maxAttempts = 6
	tip := runTip
	for i := 0; i < maxAttempts; i++ {
		if ok, err := gitIsAncestor(ctx, repoRoot, head, tip); err != nil {
			return nil, err
		} else if ok {
			// A concurrent sibling already integrated this stack; nothing to do.
			return map[string]any{"integration": "already_reachable", "run_tip": tip, "retries": i}, nil
		}
		var newRef, mode, mergedTree string
		if ok, err := gitIsAncestor(ctx, repoRoot, tip, head); err != nil {
			return nil, err
		} else if ok {
			newRef = head
			mode = "fast_forward"
		} else {
			stdout, stderr, exit, err := mergeTreeWriteTree(ctx, repoRoot, tip, head)
			if err != nil {
				return nil, err
			}
			if exit != 0 {
				// Parse the conflicted-file-info section from stdout, then drop any
				// path that is byte-identical between the run tip and head — an
				// already-integrated sibling's output reached via the #327
				// parallel-group worktree-base race, not a genuine two-writer
				// overlap. Only a real (differing-content) conflict is a
				// disjoint-write-scope violation.
				conflicts := parseMergeTreeConflicts(stdout)
				real := filterRealFanInConflicts(ctx, repoRoot, tip, head, conflicts)
				if len(real) > 0 {
					return nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf(
						"fan-in integration of job %s (attempt %d) conflicts in %d path(s): %s. Two parallel siblings wrote overlapping paths; parallel fan-in lanes must use disjoint write scopes (RFC 0101). The conflict is surfaced rather than silently resolved to a last writer (which would re-strand one sibling's work).",
						jobID, attempt, len(real), strings.Join(real, ", ")),
						map[string]any{"conflicting_paths": real, "job_id": jobID, "attempt": attempt})
				}
				// Non-zero exit but no real content conflict: never mislabel this
				// as a disjoint-scope violation (the #327 "rejected with 0
				// conflicting paths" wedge). Surface the raw git output so a
				// genuine merge-tree/plumbing failure stays loud and accurately
				// diagnosed; an operator can re-drive via worktree.anchor.
				detail := strings.TrimSpace(stdout + "\n" + stderr)
				return nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf(
					"fan-in merge-tree for job %s (attempt %d) exited %d with no real content conflict (any flagged paths were byte-identical to the already-integrated run tip; not a write-scope violation): %s",
					jobID, attempt, exit, detail),
					map[string]any{"job_id": jobID, "attempt": attempt, "ignored_identical_paths": conflicts})
			}
			tree := firstLine(stdout)
			if !isFullGitSHA(tree) {
				return nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("fan-in merge-tree produced invalid tree oid %q for job %s", tree, jobID), nil)
			}
			msg := fmt.Sprintf("striatum fan-in: integrate %s (attempt %d) into run branch", jobID, attempt)
			// Set an explicit committer identity (RFC 0127 retired the lane git
			// identity, and the daemon must not depend on ambient git config), mirroring
			// HandleRunIntegrate's commit-tree.
			cout, cexit, err := integrateGit(ctx, repoRoot,
				"-c", "user.name=striatum-fanin", "-c", "user.email=fanin@striatum.local",
				"commit-tree", tree, "-p", tip, "-p", head, "-m", msg)
			if err != nil {
				return nil, err
			}
			if cexit != 0 {
				return nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("fan-in commit-tree failed for job %s: %s", jobID, strings.TrimSpace(cout)), nil)
			}
			newRef = firstLine(cout)
			if !isFullGitSHA(newRef) {
				return nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("fan-in commit-tree produced invalid commit oid %q for job %s", newRef, jobID), nil)
			}
			mergedTree = tree
			mode = "merge"
		}
		_, uexit, err := integrateGit(ctx, repoRoot, "update-ref", runRef, newRef, tip)
		if err != nil {
			return nil, err
		}
		if uexit == 0 {
			result := map[string]any{"integration": mode, "from": tip, "to": newRef, "retries": i}
			if mode == "merge" {
				result["merge_commit"] = newRef
				result["merged_tree"] = mergedTree
				result["sibling_head"] = head
			}
			return result, nil
		}
		refreshed, err := gitRevParseCommit(ctx, repoRoot, runRef)
		if err != nil {
			return nil, err
		}
		if refreshed == tip {
			return nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("fan-in update-ref compare-and-swap failed for the run branch without movement (job %s)", jobID), nil)
		}
		tip = refreshed
	}
	return nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("fan-in integration of job %s exhausted %d compare-and-swap retries advancing the run branch", jobID, maxAttempts), nil)
}

// pinWorktreeCommitStack anchors a worktree's diverged HEAD under an
// attempt-namespaced pin (#215, RFC 0117 Open Question 4):
// refs/striatum/<run>/<job>/<attempt>. Namespacing by attempt means a later
// attempt's pin can no longer clobber an earlier attempt's pin, so revision
// cycles keep durable provenance for every attempt's commit stack. Reads
// resolve both this shape and the legacy refs/striatum/<run>/<job> shape.
func pinWorktreeCommitStack(ctx context.Context, repoRoot, runID, jobID, head string, payload map[string]any, attempt int) (map[string]any, error) {
	pinRef := attemptPinRef(runID, jobID, attempt)
	out, exit, err := integrateGit(ctx, repoRoot, "update-ref", pinRef, head)
	if err != nil {
		return nil, err
	}
	if exit != 0 {
		// A pre-#215 run may already hold a job-only pin at the legacy shape
		// refs/striatum/<run>/<job>. That ref occupies the <job> name, so git
		// cannot create the attempt directory beneath it (a ref D/F conflict).
		// Do not rewrite the historical pin or migrate the repo; fall back to
		// the legacy job-only ref for this legacy run. New runs never reach this
		// branch and stay attempt-namespaced, and reads resolve both shapes.
		legacyRef := legacyPinRef(runID, jobID)
		if legacyPinExists(ctx, repoRoot, legacyRef) {
			lout, lexit, lerr := integrateGit(ctx, repoRoot, "update-ref", legacyRef, head)
			if lerr != nil {
				return nil, lerr
			}
			if lexit == 0 {
				payload["anchor"] = "job_pin"
				payload["pin_ref"] = legacyRef
				payload["pin_shape"] = "legacy"
				payload["pin_attempt"] = attempt
				return payload, nil
			}
			out = lout
		}
		return nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("update-ref of %q failed while anchoring worktree commits: %s", pinRef, strings.TrimSpace(out)), nil)
	}
	payload["anchor"] = "job_pin"
	payload["pin_ref"] = pinRef
	payload["pin_shape"] = "attempt"
	payload["pin_attempt"] = attempt
	return payload, nil
}

// attemptPinRef builds the current attempt-namespaced pin ref shape. Attempt
// numbers are 1-based; a missing/non-positive attempt is normalized to 1 so the
// ref is always well-formed.
func attemptPinRef(runID, jobID string, attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("refs/striatum/%s/%s/%d", runID, jobID, attempt)
}

// legacyPinRef builds the pre-#215 job-only pin ref shape, retained for
// backward-compatible reads and the legacy-fallback write path.
func legacyPinRef(runID, jobID string) string {
	return "refs/striatum/" + runID + "/" + jobID
}

func legacyPinExists(ctx context.Context, repoRoot, ref string) bool {
	_, exit, err := integrateGit(ctx, repoRoot, "rev-parse", "--verify", "--quiet", ref)
	return err == nil && exit == 0
}

type worktreeReachability struct {
	Head        string
	Reachable   bool
	CheckedRefs []string
}

func worktreeHeadReachability(ctx context.Context, repoRoot, worktreeRoot string, row map[string]any) (worktreeReachability, error) {
	head, err := gitRevParseCommit(ctx, worktreeRoot, "HEAD")
	if err != nil {
		return worktreeReachability{}, err
	}
	refs := durableWorktreeRefs(ctx, repoRoot, row)
	for _, ref := range refs {
		ok, err := gitIsAncestor(ctx, repoRoot, head, ref)
		if err != nil {
			return worktreeReachability{}, err
		}
		if ok {
			return worktreeReachability{Head: head, Reachable: true, CheckedRefs: refs}, nil
		}
	}
	return worktreeReachability{Head: head, Reachable: false, CheckedRefs: refs}, nil
}

func durableWorktreeRefs(ctx context.Context, repoRoot string, row map[string]any) []string {
	seen := map[string]bool{}
	refs := []string{}
	add := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			return
		}
		if _, err := gitRevParseCommit(ctx, repoRoot, ref); err == nil {
			refs = append(refs, ref)
			seen[ref] = true
		}
	}
	if branch := strings.TrimSpace(fmt.Sprint(row["base_branch"])); branch != "" && branch != "<nil>" {
		add("refs/heads/" + branch)
	}
	runID := strings.TrimSpace(fmt.Sprint(row["run_id"]))
	jobID := strings.TrimSpace(fmt.Sprint(row["job_id"]))
	if runID != "" && runID != "<nil>" && jobID != "" && jobID != "<nil>" {
		add("refs/striatum/" + runID + "/" + jobID)
	}
	if runID != "" && runID != "<nil>" {
		out, exit, err := integrateGit(ctx, repoRoot, "for-each-ref", "--format=%(refname)", "refs/striatum/"+runID+"/")
		if err == nil && exit == 0 {
			for _, line := range strings.Split(out, "\n") {
				add(line)
			}
		}
	}
	return refs
}

func gitRevParseCommit(ctx context.Context, repoRoot, rev string) (string, error) {
	out, exit, err := integrateGit(ctx, repoRoot, "rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	if exit != 0 {
		return "", rpc.NewError("invalid_transition", fmt.Sprintf("git commit %q does not resolve", rev), nil)
	}
	sha := firstLine(out)
	if !isFullGitSHA(sha) {
		return "", rpc.NewError("invalid_transition", fmt.Sprintf("git commit %q resolved to invalid sha %q", rev, sha), nil)
	}
	return sha, nil
}

func gitIsAncestor(ctx context.Context, repoRoot, ancestor, descendant string) (bool, error) {
	_, exit, err := integrateGit(ctx, repoRoot, "merge-base", "--is-ancestor", ancestor, descendant)
	if err != nil {
		return false, err
	}
	switch exit {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("merge-base --is-ancestor failed for %s -> %s", ancestor, descendant), nil)
	}
}

func validatedWorktreeCreateInputs(ctx context.Context, runner any, repositoryID, sessionID, jobID, leaseID string) (worktreeCreateInputs, error) {
	job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		return worktreeCreateInputs{}, err
	}
	lease, err := activeLeaseFor(ctx, runner, repositoryID, leaseID, sessionID, jobID)
	if err != nil {
		return worktreeCreateInputs{}, err
	}
	session, err := rowByID(ctx, runner, repositoryID, "sessions", "session_id", sessionID, true)
	if err != nil {
		return worktreeCreateInputs{}, err
	}
	if fmt.Sprint(session["state"]) != "active" {
		return worktreeCreateInputs{}, rpc.NewError("lease_error", "lease owner session is not active", nil)
	}
	if fmt.Sprint(lease["resource_type"]) != "job" {
		return worktreeCreateInputs{}, rpc.NewError("lease_error", "lease does not belong to a job", nil)
	}
	if fmt.Sprint(lease["run_id"]) != fmt.Sprint(job["run_id"]) {
		return worktreeCreateInputs{}, rpc.NewError("lease_error", "lease does not belong to the job run", nil)
	}
	if !isRepoWrite(job) {
		return worktreeCreateInputs{}, rpc.NewError("invalid_transition", "worktree create requires a repo-write job", nil)
	}
	if active, err := activeWorktreeForJob(ctx, runner, repositoryID, jobID); err != nil {
		return worktreeCreateInputs{}, err
	} else if active != nil {
		return worktreeCreateInputs{}, rpc.NewError("invalid_transition", "job already has an active worktree", nil)
	}

	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), true)
	if err != nil {
		return worktreeCreateInputs{}, err
	}
	snapshot, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(run["workflow_snapshot_id"]), false)
	if err != nil {
		return worktreeCreateInputs{}, err
	}
	workflow := asMap(snapshot["workflow_json"])
	laneID := jobLaneID(job)
	if laneWorktreeIsolation(workflow, laneID) != "per_job" {
		return worktreeCreateInputs{}, rpc.NewError("invalid_transition", "lane is not configured for worktree_isolation: per_job", nil)
	}
	baseBranch := fmt.Sprint(run["branch_name"])
	if baseBranch == "" || baseBranch == "<nil>" {
		return worktreeCreateInputs{}, rpc.NewError("invalid_transition", "run has no confirmed branch for worktree base", nil)
	}
	if nullable(run["branch_confirmed_at"]) == nil {
		return worktreeCreateInputs{}, rpc.NewError("invalid_transition", "run branch must be confirmed before worktree create", nil)
	}
	branchBase := strings.TrimSpace(fmt.Sprint(run["branch_base"]))
	if branchBase == "<nil>" {
		branchBase = ""
	}
	return worktreeCreateInputs{
		Job:        job,
		Workflow:   workflow,
		RunID:      fmt.Sprint(job["run_id"]),
		BaseBranch: baseBranch,
		BranchBase: branchBase,
	}, nil
}

func activeRepositoryRoot(ctx context.Context, runner any, repositoryID string) (string, error) {
	row, err := oneRow(ctx, runner, `
		SELECT repo_root
		  FROM striatumd.repositories
		 WHERE repository_id = $1 AND state = 'active'`,
		repositoryID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", rpc.NewError("repo_not_registered", "daemon RPC repository is not registered", nil)
	}
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(fmt.Sprint(row["repo_root"]))
	if root == "" || root == "<nil>" {
		return "", rpc.NewError("invalid_transition", "registered repository has no repo_root", nil)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func activeWorktreeForJob(ctx context.Context, runner any, repositoryID, jobID string) (map[string]any, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT *
		  FROM striatumd.job_worktrees
		 WHERE repository_id = $1 AND job_id = $2 AND state = 'active'
		 LIMIT 1
		 FOR UPDATE`,
		repositoryID,
		jobID,
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func worktreeRow(ctx context.Context, runner any, repositoryID, worktreeID string, forUpdate bool) (map[string]any, error) {
	suffix := ""
	if forUpdate {
		suffix = " FOR UPDATE"
	}
	row, err := oneRow(ctx, runner, `
		SELECT *
		  FROM striatumd.job_worktrees
		 WHERE repository_id = $1 AND worktree_id = $2`+suffix,
		repositoryID,
		worktreeID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, rpc.NewError("not_found", fmt.Sprintf("could not find job_worktrees row for %q", worktreeID), nil)
	}
	return row, err
}

func jobLaneID(job map[string]any) string {
	lane, _ := asMap(job["lane_selector_json"])["lane_id"].(string)
	return lane
}

// jobLaneIDWithSessionFallback resolves the effective lane id for a job the same
// way the claim packet does (#310): prefer the job's lane_selector_json lane_id,
// and when that is absent fall back to the lane_id of the session that owns the
// job's active lease — the lane the daemon actually supervised the work under.
// This keeps the publish-time worktree-isolation decision consistent with the
// claim-time packet, so a lane that believes it is per-job isolated is held to
// the per-job worktree boundary at publish time too. A job with no active lease
// (none in flight) keeps the empty selector value, preserving prior behavior.
func jobLaneIDWithSessionFallback(ctx context.Context, runner any, repositoryID string, job map[string]any) (string, error) {
	if lane := strings.TrimSpace(jobLaneID(job)); lane != "" {
		return lane, nil
	}
	row, err := oneRow(ctx, runner, `
		SELECT s.lane_id
		  FROM striatumd.leases l
		  JOIN striatumd.sessions s
		    ON s.repository_id = l.repository_id AND s.session_id = l.owner_session_id
		 WHERE l.repository_id = $1 AND l.resource_type = 'job' AND l.resource_id = $2
		   AND l.state = 'active'
		 ORDER BY l.acquired_at DESC
		 LIMIT 1`, repositoryID, fmt.Sprint(job["job_id"]))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	lane, _ := nullable(row["lane_id"]).(string)
	return strings.TrimSpace(lane), nil
}

func worktreeTarget(repoRoot string, pathText string) (string, error) {
	return confinedScratchTarget(repoRoot, pathText, worktreeSubdir, "worktree")
}

// workspaceTarget confines an RFC 0127 plain-dir workspace path to
// .striatum/workspaces, with the same symlink/traversal safety the legacy
// worktree path enforces.
func workspaceTarget(repoRoot string, pathText string) (string, error) {
	return confinedScratchTarget(repoRoot, pathText, workspacesSubdir, "workspace")
}

// confinedScratchTarget resolves a repository-relative scratch path to an
// absolute path confined to .striatum/<subdir>, rejecting paths that escape the
// repository or traverse a symlinked component. label names the surface in error
// messages ("worktree" / "workspace").
func confinedScratchTarget(repoRoot, pathText, subdir, label string) (string, error) {
	if strings.TrimSpace(pathText) == "" {
		return "", rpc.NewError("invalid_transition", label+" path must stay under "+worktreeStateDir+"/"+subdir, nil)
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	raw := filepath.FromSlash(pathText)
	target := raw
	if !filepath.IsAbs(raw) {
		target = filepath.Join(root, raw)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	target = filepath.Clean(target)
	if !pathWithin(root, target) {
		return "", rpc.NewError("invalid_transition", label+" path must stay inside the repository", nil)
	}
	scratchRoot := filepath.Join(root, worktreeStateDir, subdir)
	if worktreePathHasSymlinkComponent(root, scratchRoot) || worktreePathHasSymlinkComponent(root, target) {
		return "", rpc.NewError("invalid_transition", label+" path must not traverse symlinks", nil)
	}
	if !pathWithin(scratchRoot, target) {
		return "", rpc.NewError("invalid_transition", label+" path must stay under "+worktreeStateDir+"/"+subdir, nil)
	}
	return target, nil
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel))
}

func worktreePathHasSymlinkComponent(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return true
	}
	current := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return !errors.Is(err, os.ErrNotExist)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

func defaultRunGitWorktreeCommand(ctx context.Context, repoRoot string, args ...string) (gitWorktreeResult, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoRoot
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := gitWorktreeResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, err
}

func gitWorktreeErrorMessage(prefix string, result gitWorktreeResult) string {
	message := strings.TrimSpace(result.Stderr)
	if message == "" {
		message = strings.TrimSpace(result.Stdout)
	}
	if len(message) > 200 {
		message = message[:200] + "..."
	}
	if message == "" {
		return prefix
	}
	return prefix + ": " + message
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func requiredStringParam(params map[string]any, key string) (string, error) {
	value, ok := params[key]
	if !ok {
		return "", rpc.NewError("schema_invalid", key+" must be a non-empty string", nil)
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return "", rpc.NewError("schema_invalid", key+" must be a non-empty string", nil)
	}
	return text, nil
}

// handlePlainDirWorkspaceCreate implements the RFC 0127 P0 (D195) opt-in: instead
// of `git worktree add --detach`, the daemon creates a plain, daemon-owned,
// .git-free directory under .striatum/workspaces/<id>, stages the run-branch base
// content into it, and records the base tree sha it staged in job_workspaces
// BEFORE the lane starts. The lane sees no git repo at all (a pure byte
// producer); the durable base-tree pin lets a retrospective reconstruct the exact
// diff (base tree XOR published tree) without trusting the lane. This is the only
// P0 behavior — daemon-side diff/write-scope, the porter commit from the plain
// dir, and the default flip are P1-P3 and are intentionally not implemented here.
func handlePlainDirWorkspaceCreate(ctx context.Context, runner db.Runner, repositoryID, sessionID, jobID, leaseID, repoRoot string, inputs worktreeCreateInputs) (map[string]any, error) {
	workspaceID, err := newID("ws")
	if err != nil {
		return nil, err
	}
	relative := filepath.ToSlash(filepath.Join(worktreeStateDir, workspacesSubdir, workspaceID))
	target, err := workspaceTarget(repoRoot, relative)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, err
	}

	// Repair a confirmed-but-unborn run branch ref (#183) exactly like the git
	// worktree path, so base staging resolves a real commit/tree.
	if err := ensureWorktreeBaseBranchRef(ctx, repoRoot, inputs.BaseBranch, inputs.BranchBase); err != nil {
		return nil, err
	}

	// Stage the base content (no .git) and pin the durable base tree sha. This is
	// the "before" state recorded before the lane runs.
	baseTreeSHA, err := stagePlainDirBaseContent(ctx, repoRoot, inputs.BaseBranch, target)
	if err != nil {
		_ = os.RemoveAll(target)
		return nil, err
	}

	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		validated, err := validatedWorktreeCreateInputs(ctx, tx, repositoryID, sessionID, jobID, leaseID)
		if err != nil {
			return nil, err
		}
		if active, err := activeWorkspaceForJob(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		} else if active != nil {
			return nil, rpc.NewError("invalid_transition", "job already has an active workspace", nil)
		}
		now := nowString()
		if err := tx.Exec(ctx, `
			INSERT INTO striatumd.job_workspaces (
			  repository_id, workspace_id, run_id, job_id, lease_id,
			  workspace_kind, base_branch, base_tree_sha, workspace_path, state, created_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'active',$10)`,
			repositoryID,
			workspaceID,
			validated.RunID,
			jobID,
			leaseID,
			workspaceKindPlainDir,
			validated.BaseBranch,
			baseTreeSHA,
			relative,
			now,
		); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, validated.RunID, "workspace.created", sessionID, jobID, nil, nil, leaseID, map[string]any{
			"workspace_id":   workspaceID,
			"workspace_kind": workspaceKindPlainDir,
			"workspace_path": relative,
			"base_branch":    validated.BaseBranch,
			"base_tree_sha":  baseTreeSHA,
		}); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	}); err != nil {
		_ = os.RemoveAll(target)
		return nil, err
	}

	return map[string]any{
		"workspace_id":   workspaceID,
		"workspace_kind": workspaceKindPlainDir,
		"workspace_path": relative,
		"base_branch":    inputs.BaseBranch,
		"base_tree_sha":  baseTreeSHA,
		// Memory-digest grounding (recall shelf) is a worktree convenience deferred
		// for plain-dir parity; reported skipped to keep the return shape stable.
		"memory_digest": map[string]any{"status": "skipped"},
	}, nil
}

func activeWorkspaceForJob(ctx context.Context, runner any, repositoryID, jobID string) (map[string]any, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT *
		  FROM striatumd.job_workspaces
		 WHERE repository_id = $1 AND job_id = $2 AND state = 'active'
		 LIMIT 1
		 FOR UPDATE`,
		repositoryID,
		jobID,
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// stagePlainDirBaseContent stages the base content of ref into a plain, .git-free
// directory and returns the tree sha it staged. It is the RFC 0127 P0 "before"
// pin: the daemon owns the staging and records the exact tree the lane will
// diverge from, so a retrospective reconstructs the diff without trusting the
// lane. Staging uses `git archive` (a tar stream of the tree) extracted
// in-process, so the directory never contains a .git and no external tar binary
// is required.
func stagePlainDirBaseContent(ctx context.Context, repoRoot, ref, target string) (string, error) {
	treeSHA, err := gitRevParseTree(ctx, repoRoot, ref)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	if err := extractGitArchive(ctx, repoRoot, treeSHA, target); err != nil {
		return "", err
	}
	return treeSHA, nil
}

func gitRevParseTree(ctx context.Context, repoRoot, ref string) (string, error) {
	out, exit, err := integrateGit(ctx, repoRoot, "rev-parse", "--verify", ref+"^{tree}")
	if err != nil {
		return "", err
	}
	if exit != 0 {
		return "", rpc.NewError("invalid_transition", fmt.Sprintf("git tree %q does not resolve for plain-dir staging", ref), nil)
	}
	sha := firstLine(out)
	if !isFullGitSHA(sha) {
		return "", rpc.NewError("invalid_transition", fmt.Sprintf("git tree %q resolved to invalid sha %q", ref, sha), nil)
	}
	return sha, nil
}

// extractGitArchive writes the tree's tar archive into target. The full archive
// is read before extraction so the git child cannot deadlock on a stalled pipe.
func extractGitArchive(ctx context.Context, repoRoot, treeish, target string) error {
	cmd := exec.CommandContext(ctx, "git", "archive", "--format=tar", treeish)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return rpc.NewError("invalid_transition", gitArchiveErrorMessage(stderr.String(), err), nil)
	}
	return extractTarStream(tar.NewReader(bytes.NewReader(out)), target)
}

// extractTarStream extracts a tar stream into target, confining every entry to
// target (rejecting absolute or escaping paths defensively even though the tar
// comes from a daemon-controlled `git archive`).
//
// SAFE FOR SINGLE-TREE `git archive` INPUT ONLY. The confinement check tests the
// cleaned entry path, not the resolved path, and the TypeSymlink branch writes
// header.Linkname without validating the link target. That is sound here because
// the source is one pinned git tree, which cannot store both a symlink and a
// directory at the same name, so no later entry can traverse an earlier-created
// symlink. Any reuse for a general/untrusted tar must first add resolved-path
// (lstat-per-component) confinement and symlink-target validation (RFC 0127
// review round 2, finding 1).
func extractTarStream(reader *tar.Reader, target string) error {
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return rpc.NewError("invalid_transition", "plain-dir staging archive read failed: "+err.Error(), nil)
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if name == "." {
			continue
		}
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return rpc.NewError("invalid_transition", fmt.Sprintf("plain-dir staging refused unsafe archive path %q", header.Name), nil)
		}
		dest := filepath.Join(target, name)
		if dest != target && !strings.HasPrefix(dest, target+string(os.PathSeparator)) {
			return rpc.NewError("invalid_transition", fmt.Sprintf("plain-dir staging refused archive path %q escaping the workspace", header.Name), nil)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			file, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, reader); err != nil { //nolint:gosec // daemon-controlled git archive of a pinned tree
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Link target is written unvalidated; safe only for single-tree
			// git-archive input (see the extractTarStream doc comment).
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, dest); err != nil {
				return err
			}
		default:
			// Skip other entry types (extended headers, etc.).
		}
	}
}

func gitArchiveErrorMessage(stderr string, err error) string {
	message := strings.TrimSpace(stderr)
	if message == "" {
		message = err.Error()
	}
	if len(message) > 200 {
		message = message[:200] + "..."
	}
	return "git archive for plain-dir staging failed: " + message
}
