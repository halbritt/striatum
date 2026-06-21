package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/halbritt/striatum/go/pkg/rpc"
)

type publishedArtifactDurabilityProblem struct {
	ArtifactID    string
	LogicalName   string
	RepoPath      string
	ContentSHA256 string
	Reason        string
}

type artifactDurabilityCheck struct {
	RepositoryID string
	RepoRoot     string
	Job          map[string]any
	Worktree     map[string]any
}

// publishedArtifactDurabilityProbe is the shared prefix for the durability
// surfaces: it resolves the active worktree and returns the durability problems
// for the job's published repo-write artifacts. done=true means the caller
// should simply return err (a no-op for non-repo-write / no-worktree-required
// jobs, or the worktreeRequiredError when the worktree is gone).
func publishedArtifactDurabilityProbe(ctx context.Context, runner any, repositoryID string, job map[string]any, surface string) (artifactDurabilityCheck, []publishedArtifactDurabilityProblem, bool, error) {
	if !isRepoWrite(job) {
		return artifactDurabilityCheck{}, nil, true, nil
	}
	required, worktree, err := worktreeRequirementForJob(ctx, runner, repositoryID, job)
	if err != nil {
		return artifactDurabilityCheck{}, nil, true, err
	}
	if !required {
		return artifactDurabilityCheck{}, nil, true, nil
	}
	if worktree == nil {
		return artifactDurabilityCheck{}, nil, true, worktreeRequiredError(job, surface)
	}
	repoRoot, err := activeRepositoryRoot(ctx, runner, repositoryID)
	if err != nil {
		return artifactDurabilityCheck{}, nil, true, err
	}
	check := artifactDurabilityCheck{
		RepositoryID: repositoryID,
		RepoRoot:     repoRoot,
		Job:          job,
		Worktree:     worktree,
	}
	problems, err := publishedArtifactDurabilityProblems(ctx, runner, check)
	if err != nil {
		return check, nil, true, err
	}
	return check, problems, false, nil
}

// ensurePerJobPublishedArtifactsDurable is the read-only durability GATE: it
// refuses when a published repo-write artifact body is not reconstructable from
// the worktree HEAD. It performs no remediation, so recovery surfaces
// (recovery.reseal, recovery.resume --complete) use it as a pure check —
// completion advances only once the body is genuinely durable.
func ensurePerJobPublishedArtifactsDurable(ctx context.Context, runner any, repositoryID string, job map[string]any, surface string) error {
	_, problems, done, err := publishedArtifactDurabilityProbe(ctx, runner, repositoryID, job, surface)
	if done {
		return err
	}
	if len(problems) == 0 {
		return nil
	}
	return publishedArtifactsNotDurableError(surface, job, problems)
}

// ensurePublishedArtifactsDurableWithPorter is the work.complete variant: when a
// published body is not yet durable it invokes the daemon-as-porter (RFC 0125,
// D192) to force-add + commit the artifact files the lane wrote into the
// (possibly detached / ignored) per-job worktree, then re-probes. This is scoped
// to the lane completion path on purpose — a lane should not have to commit its
// own publication (git.commit_apply refuses the detached worktree, #281; `git
// add` skips a repo-ignored declared path, #278). Anything still not durable
// (the lane could not write the worktree at all, #272) remains a residual the
// operator resolves out of band, then recovery.reseal.
func ensurePublishedArtifactsDurableWithPorter(ctx context.Context, runner any, repositoryID string, job map[string]any, surface string) error {
	check, problems, done, err := publishedArtifactDurabilityProbe(ctx, runner, repositoryID, job, surface)
	if done {
		return err
	}
	if len(problems) == 0 {
		return nil
	}
	problems, err = porterCommitWorktreeArtifacts(ctx, runner, repositoryID, job, check, problems)
	if err != nil {
		return err
	}
	if len(problems) == 0 {
		return nil
	}
	return publishedArtifactsNotDurableError(surface, job, problems)
}

// anchorReviewJobPublishedArtifacts is the #551 review.verdict-completion variant
// of work.complete's "durable -> anchor" sequence. A repo-write verdict-bearing
// job (phase_synthesis / review) writes its synthesis (e.g. collaboration_ledger)
// into the per-job worktree, publishes it via artifact.publish, then completes via
// review.verdict — but completeReviewJob historically flipped the job to completed
// without ever committing/anchoring that file, so it stayed untracked in the
// worktree and the run branch never advanced (downstream gated jobs forked from a
// tip blind to it). This runs the SAME daemon-as-porter commit + run-ref anchor
// work.complete runs, in the SAME transaction, in the SAME order (porter ->
// anchor -> caller flips state). It is a strict no-op when:
//   - the job is not a repo-write job (publishedArtifactDurabilityProbe returns
//     done with no problems, and anchorActiveWorktreeForJob returns nil), or
//   - no active per-job worktree exists for the job (the stale-lane recovery path,
//     recovery.go applyVerdict, may complete a verdict job whose worktree is gone;
//     recovery owns its own durability machinery and must not start failing on a
//     newly-required worktree here — so we skip silently when the worktree is
//     absent rather than raising worktreeRequiredError).
//
// Anchoring only ever commits the declared artifact paths the lane wrote into THIS
// job's worktree (the porter is path-scoped); blob-backed artifacts
// (blob_exhaust / git_pointer_manifest) carry a blob_key and are skipped by the
// durability probe, so this never touches them.
func anchorReviewJobPublishedArtifacts(ctx context.Context, runner any, repositoryID string, job map[string]any, sessionID, leaseID string) error {
	if !isRepoWrite(job) {
		return nil
	}
	// Only proceed when an active per-job worktree exists. This deliberately does
	// not raise worktreeRequiredError (unlike work.complete): the stale-lane
	// recovery path reaches completeReviewJob with a possibly-absent worktree and
	// recovery owns durability separately, so a missing worktree must remain a
	// silent skip here, never a new completion failure.
	worktree, err := activeWorktreeForJob(ctx, runner, repositoryID, fmt.Sprint(job["job_id"]))
	if err != nil {
		return err
	}
	if worktree == nil {
		return nil
	}
	if err := ensurePublishedArtifactsDurableWithPorter(ctx, runner, repositoryID, job, "review.verdict"); err != nil {
		return err
	}
	anchorPayload, err := anchorActiveWorktreeForJob(ctx, runner, repositoryID, job)
	if err != nil {
		return err
	}
	if anchorPayload != nil {
		if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "job.commits_anchored", sessionID, job["job_id"], nullable(job["current_message_id"]), nil, leaseID, anchorPayload); err != nil {
			return err
		}
	}
	return nil
}

// porterCommitWorktreeArtifacts is the daemon-as-porter last-mile commit
// (RFC 0125, D192). For each still-undurable published artifact whose body the
// lane DID write into the per-job worktree, the daemon force-adds it (past the
// target repo's ignore rules — #278) and commits it onto the worktree's
// possibly-detached HEAD (#281), then re-probes durability. It deliberately
// commits only the declared artifact paths (a targeted `git add -f -- <path>`),
// never the whole worktree, and returns the residual problems for any artifact
// whose file is absent on disk (the lane could not enter the worktree, #272 —
// not remediable here). The subsequent anchorActiveWorktreeForJob in the
// completion flow advances the durable run ref over the new commit.
func porterCommitWorktreeArtifacts(ctx context.Context, runner any, repositoryID string, job map[string]any, check artifactDurabilityCheck, problems []publishedArtifactDurabilityProblem) ([]publishedArtifactDurabilityProblem, error) {
	target, err := worktreeTarget(check.RepoRoot, fmt.Sprint(check.Worktree["worktree_path"]))
	if err != nil {
		return problems, err
	}
	staged := 0
	for _, problem := range problems {
		if problem.Reason != "missing_from_head" && problem.Reason != "head_content_mismatch" {
			continue
		}
		repoPath, ok := cleanDurableArtifactPath(problem.RepoPath)
		if !ok {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(target, filepath.FromSlash(repoPath))); statErr != nil {
			// The lane never managed to write the body into the worktree (#272);
			// the porter cannot reconstruct it from here.
			continue
		}
		added, err := runGitWorktreeCommand(ctx, target, "add", "-f", "--", repoPath)
		if err != nil {
			return problems, err
		}
		if added.ExitCode != 0 {
			continue
		}
		staged++
	}
	if staged == 0 {
		return problems, nil
	}
	// Nothing to commit means the staged paths already matched HEAD; re-probe
	// rather than failing an empty commit.
	if diff, err := runGitWorktreeCommand(ctx, target, "diff", "--cached", "--quiet"); err != nil {
		return problems, err
	} else if diff.ExitCode == 0 {
		return publishedArtifactDurabilityProblems(ctx, runner, check)
	}
	committed, err := runGitWorktreeCommand(ctx, target,
		"-c", "user.name=striatum-porter", "-c", "user.email=porter@striatum.local",
		"commit", "-m", fmt.Sprintf("striatum: durable artifact publication (job %s)", fmt.Sprint(job["job_id"])))
	if err != nil {
		return problems, err
	}
	if committed.ExitCode != 0 {
		return problems, rpc.NewError("invalid_transition",
			gitWorktreeErrorMessage("daemon-porter commit failed", committed), nil)
	}
	return publishedArtifactDurabilityProblems(ctx, runner, check)
}

func publishedArtifactDurabilityProblems(ctx context.Context, runner any, check artifactDurabilityCheck) ([]publishedArtifactDurabilityProblem, error) {
	target, err := worktreeTarget(check.RepoRoot, fmt.Sprint(check.Worktree["worktree_path"]))
	if err != nil {
		return nil, err
	}
	rows, err := queryRows(ctx, runner, `
		SELECT a.artifact_id, a.logical_name, a.repo_path, a.content_sha256, a.blob_key
		  FROM striatumd.artifacts a
		 WHERE a.repository_id = $1
		   AND a.run_id = $2
		   AND a.job_id = $3
		   AND a.attempt = $4
		 ORDER BY a.created_at, a.artifact_id`,
		check.RepositoryID, check.Job["run_id"], check.Job["job_id"], check.Job["attempt"])
	if err != nil {
		return nil, err
	}
	problems := []publishedArtifactDurabilityProblem{}
	for _, row := range rows {
		blobKey := strings.TrimSpace(fmt.Sprint(nullable(row["blob_key"])))
		if blobKey != "" && blobKey != "<nil>" {
			continue
		}
		problem := publishedArtifactDurabilityProblem{
			ArtifactID:    fmt.Sprint(row["artifact_id"]),
			LogicalName:   fmt.Sprint(row["logical_name"]),
			RepoPath:      fmt.Sprint(row["repo_path"]),
			ContentSHA256: fmt.Sprint(row["content_sha256"]),
		}
		repoPath, ok := cleanDurableArtifactPath(problem.RepoPath)
		if !ok {
			problem.Reason = "invalid_repo_path"
			problems = append(problems, problem)
			continue
		}
		committedSHA, found, err := gitCommittedFileSHA256(ctx, target, repoPath)
		if err != nil {
			return nil, err
		}
		if !found {
			problem.Reason = "missing_from_head"
			problems = append(problems, problem)
			continue
		}
		if committedSHA != problem.ContentSHA256 {
			problem.Reason = "head_content_mismatch"
			problems = append(problems, problem)
		}
	}
	return problems, nil
}

func cleanDurableArtifactPath(pathText string) (string, bool) {
	pathText = strings.TrimSpace(filepath.ToSlash(pathText))
	if pathText == "" || strings.HasPrefix(pathText, "/") {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(pathText))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func gitCommittedFileSHA256(ctx context.Context, worktreeRoot, repoPath string) (string, bool, error) {
	result, err := runGitWorktreeCommand(ctx, worktreeRoot, "show", "HEAD:"+repoPath)
	if err != nil {
		return "", false, err
	}
	if result.ExitCode != 0 {
		return "", false, nil
	}
	sum := sha256.Sum256([]byte(result.Stdout))
	return hex.EncodeToString(sum[:]), true, nil
}

func publishedArtifactsNotDurableError(surface string, job map[string]any, problems []publishedArtifactDurabilityProblem) error {
	return rpc.NewError("invalid_transition", fmt.Sprintf(
		"%s refused: published artifact content is not durable outside the per-job worktree; commit the artifact file or publish it through blob-backed storage before retrying",
		surface,
	), map[string]any{
		"run_id":    job["run_id"],
		"job_id":    job["job_id"],
		"artifacts": publishedArtifactDurabilityProblemMaps(problems),
	})
}

func publishedArtifactDurabilityProblemMaps(problems []publishedArtifactDurabilityProblem) []map[string]any {
	items := make([]map[string]any, 0, len(problems))
	for _, p := range problems {
		items = append(items, map[string]any{
			"artifact_id":    p.ArtifactID,
			"logical_name":   p.LogicalName,
			"repo_path":      p.RepoPath,
			"content_sha256": p.ContentSHA256,
			"reason":         p.Reason,
		})
	}
	return items
}
