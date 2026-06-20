package reads

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	worktreeAnchorNone        = "none"
	worktreeAnchorRunBranch   = "run_branch"
	worktreeAnchorJobPin      = "job_pin"
	worktreeAnchorUnreachable = "unreachable"
)

func addWorktreeAnchorProjection(ctx context.Context, row map[string]any) {
	projection := probeWorktreeAnchor(ctx, row)
	row["head"] = projection["head"]
	row["reachable"] = projection["reachable"]
	row["anchor"] = projection["anchor"]
	row["anchored_ref"] = projection["anchored_ref"]
	row["checked_refs"] = projection["checked_refs"]
	if errText := stringFrom(projection, "probe_error"); errText != "" {
		row["probe_error"] = errText
	}
}

func doctorWorktreeRefSafety(ctx context.Context, runner any, repositoryID string) (map[string]any, []string, []map[string]any, []string, []map[string]any) {
	block := map[string]any{
		"checked":   false,
		"worktrees": []map[string]any{},
	}
	if repositoryID == "" {
		return block, nil, nil, nil, nil
	}
	rows, err := collectRows(ctx, runner, `
		SELECT w.worktree_id, w.run_id, w.job_id, w.lease_id,
		       w.base_branch, w.worktree_path, w.state, w.created_at,
		       w.released_at, w.removed_at, j.workflow_job_id,
		       j.state AS job_state, r.repo_root, r.branch_name,
		       r.state AS run_state
		  FROM striatumd.job_worktrees w
		  JOIN striatumd.jobs j
		    ON j.repository_id = w.repository_id
		   AND j.job_id = w.job_id
		  JOIN striatumd.runs r
		    ON r.repository_id = w.repository_id
		   AND r.run_id = w.run_id
		 WHERE w.repository_id = $1
		   AND w.state IN ('active', 'abandoned')
		 ORDER BY w.created_at, w.worktree_id`,
		repositoryID,
	)
	if err != nil {
		block["error"] = err.Error()
		return block, nil, nil, nil, nil
	}
	block["checked"] = true
	problems := []string{}
	records := []map[string]any{}
	warnings := []string{}
	warningRecords := []map[string]any{}
	defaultRefByRoot := map[string]string{}
	for _, row := range rows {
		addWorktreeAnchorProjection(ctx, row)
		if stringFrom(row, "anchor") != worktreeAnchorUnreachable {
			// #290 fan-in reachability invariant: in a still-RUNNING run, a completed
			// repo-write job's commit stack must be reachable from the RUN BRANCH — not
			// merely from a refs/striatum pin. The run branch is probed first (see
			// durableWorktreeProbeRefs), so a "job_pin" classification means the head is
			// reachable via a pin but is NOT on the run branch. That is a stranded fan-in
			// author: a downstream worktree seeded from the run branch would never see
			// it — the exact bug #290 fixes at completion via a conflict-free fan-in
			// merge. Emit a warning (not an ok-reddening problem) scoped to running runs,
			// so the green doctor baseline and historical/terminal/default-branch-merged
			// runs are untouched; this only fires on a live integration regression.
			if stringFrom(row, "anchor") == worktreeAnchorJobPin &&
				stringFrom(row, "job_state") == "completed" &&
				stringFrom(row, "run_state") == "running" {
				worktreeID := stringFrom(row, "worktree_id")
				warnings = append(warnings, fmt.Sprintf(
					"fanin_sibling_unintegrated.%s: completed job %s worktree HEAD %s is reachable only via a refs/striatum pin, not from the run branch of a still-running run; a downstream worktree seeded from the run branch would not see it — recover/anchor it to integrate the fan-in sibling",
					worktreeID, stringFrom(row, "job_id"), stringFrom(row, "head"),
				))
				warningRecords = append(warningRecords, worktreeReclassRecord("fanin_sibling_unintegrated", worktreeID, row, ""))
			}
			continue
		}
		worktreeID := stringFrom(row, "worktree_id")
		jobID := stringFrom(row, "job_id")
		head := stringFrom(row, "head")
		repoRoot := strings.TrimSpace(stringFrom(row, "repo_root"))
		remediation := worktreeAnchorRemediation(row)

		// Legibility rule 1 (D-doctor-integrity-legibility): a worktree HEAD that
		// is reachable from the repository default branch is durably preserved —
		// normal post-merge run-branch deletion (AGENTS.md: "Do not strand pushed
		// branches") drops the run branch but not the merged content. The operator
		// should still create a refs/striatum pin, so keep it visible as a warning
		// rather than an ok-reddening problem.
		defaultRef := resolveDefaultRefCached(ctx, repoRoot, defaultRefByRoot)
		if defaultRef != "" && head != "" && readGitAncestor(ctx, repoRoot, head, defaultRef) {
			warnings = append(warnings, fmt.Sprintf(
				"worktree_unanchored_on_default_branch.%s: worktree HEAD %s is preserved on the default branch (%s) but has no refs/striatum pin; run %s to anchor it",
				worktreeID, head, defaultRef, remediation,
			))
			warningRecords = append(warningRecords, worktreeReclassRecord("worktree_unanchored_on_default_branch", worktreeID, row, defaultRef))
			continue
		}

		// Legibility rule 2: a worktree whose run is in a terminal debris state
		// (canceled/failed) is archived leftover, not an active durability gap.
		// `worktree release --force` refuses an undurable published artifact and the
		// worktree dir is owned by the lane sandbox user, so this debris cannot be
		// physically cleaned — emit a warning instead of a permanent problem.
		if terminalDebrisRunState(stringFrom(row, "run_state")) {
			warnings = append(warnings, fmt.Sprintf(
				"worktree_debris_terminal_run.%s: worktree HEAD %s belongs to a %s run; archived debris, not an active durability gap",
				worktreeID, head, stringFrom(row, "run_state"),
			))
			warningRecords = append(warningRecords, worktreeReclassRecord("worktree_debris_terminal_run", worktreeID, row, ""))
			continue
		}

		problems = append(problems, fmt.Sprintf(
			"worktree_head_unreachable.%s: worktree HEAD %s is not reachable from the run branch or refs/striatum pins; run %s while the worktree exists",
			worktreeID, head, remediation,
		))
		records = append(records, worktreeProblemRecord("worktree_head_unreachable", worktreeID, row))
		if stringFrom(row, "job_state") == "completed" {
			problems = append(problems, fmt.Sprintf(
				"job_completed_without_anchor.%s: completed job worktree HEAD %s is not reachable from a durable ref",
				jobID, head,
			))
			records = append(records, worktreeProblemRecord("job_completed_without_anchor", jobID, row))
		}
	}
	block["worktrees"] = rows
	return block, problems, records, warnings, warningRecords
}

// terminalDebrisRunState reports whether a run state represents abandoned,
// archived work whose worktree/artifact leftovers are debris rather than active
// durability gaps. Successful (`completed`) runs are intentionally excluded:
// their preservation is verified against durable refs / the default branch.
func terminalDebrisRunState(state string) bool {
	switch strings.TrimSpace(state) {
	case "canceled", "failed":
		return true
	default:
		return false
	}
}

// resolveDefaultRefCached resolves the repository default-branch ref once per
// repo root, memoizing the (possibly empty) result so a doctor pass that scans
// hundreds of rows issues at most one resolution per repo.
func resolveDefaultRefCached(ctx context.Context, repoRoot string, cache map[string]string) string {
	if repoRoot == "" {
		return ""
	}
	if ref, ok := cache[repoRoot]; ok {
		return ref
	}
	ref := readGitDefaultBranchRef(ctx, repoRoot)
	cache[repoRoot] = ref
	return ref
}

// readGitDefaultBranchRef resolves the repository's default-branch ref without
// hardcoding "main". It prefers the remote HEAD symbolic ref, then common remote
// default branches, then local ones. It degrades safely to "" (callers fall
// back to the run-branch/pin-only behavior) and never crashes or hangs: every
// git call is ctx-cancellable and a missing ref is treated as "not resolvable".
func readGitDefaultBranchRef(ctx context.Context, repoRoot string) string {
	if strings.TrimSpace(repoRoot) == "" {
		return ""
	}
	if out, err := readGitOutput(ctx, repoRoot, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			return ref
		}
	}
	for _, ref := range []string{
		"refs/remotes/origin/main",
		"refs/remotes/origin/master",
		"refs/heads/main",
		"refs/heads/master",
	} {
		if _, err := readGitCommit(ctx, repoRoot, ref); err == nil {
			return ref
		}
	}
	return ""
}

// readGitLocalDefaultBranchRef resolves ONLY the repository's LOCAL default
// branch (refs/heads/main, then refs/heads/master), never a remote ref. It exists
// because readGitDefaultBranchRef prefers the remote default (origin/HEAD ->
// origin/main) so the artifact-anchor doctor checks can ALSO consult the local
// default branch that `run integrate --into <branch>` advances before the
// operator pushes to origin (#504): a deliverable that is live on the locally
// integrated default branch is superseded/preserved, not lost, even while the
// remote tip is still stale. It degrades safely to "" and is ctx-cancellable.
func readGitLocalDefaultBranchRef(ctx context.Context, repoRoot string) string {
	if strings.TrimSpace(repoRoot) == "" {
		return ""
	}
	for _, ref := range []string{
		"refs/heads/main",
		"refs/heads/master",
	} {
		if _, err := readGitCommit(ctx, repoRoot, ref); err == nil {
			return ref
		}
	}
	return ""
}

// worktreeReclassRecord builds a verbose record for a reclassified
// (warning, not problem) worktree finding, mirroring worktreeProblemRecord but
// carrying the run state and any preserving ref.
func worktreeReclassRecord(check, id string, row map[string]any, preservedRef string) map[string]any {
	record := worktreeProblemRecord(check, id, row)
	contextMap := record["context"].(map[string]any)
	contextMap["run_state"] = row["run_state"]
	if preservedRef != "" {
		contextMap["preserved_ref"] = preservedRef
	}
	return record
}

func worktreeProblemRecord(check, id string, row map[string]any) map[string]any {
	return map[string]any{
		"check": check,
		"id":    id,
		"context": map[string]any{
			"worktree_id":  row["worktree_id"],
			"run_id":       row["run_id"],
			"job_id":       row["job_id"],
			"head":         row["head"],
			"anchor":       row["anchor"],
			"checked_refs": row["checked_refs"],
			"remediation":  worktreeAnchorRemediation(row),
		},
	}
}

func worktreeAnchorRemediation(row map[string]any) string {
	return fmt.Sprintf(
		"striatum worktree anchor %s %s %s",
		stringFrom(row, "run_id"),
		stringFrom(row, "job_id"),
		stringFrom(row, "worktree_id"),
	)
}

func probeWorktreeAnchor(ctx context.Context, row map[string]any) map[string]any {
	result := map[string]any{
		"head":         nil,
		"reachable":    false,
		"anchor":       worktreeAnchorNone,
		"anchored_ref": nil,
		"checked_refs": []string{},
	}
	state := strings.TrimSpace(stringFrom(row, "state"))
	if state != "active" && state != "abandoned" {
		return result
	}
	repoRoot := strings.TrimSpace(stringFrom(row, "repo_root"))
	if repoRoot == "" {
		result["probe_error"] = "repo_root_missing"
		return result
	}
	worktreeRoot, err := readWorktreeTarget(repoRoot, stringFrom(row, "worktree_path"))
	if err != nil {
		result["probe_error"] = err.Error()
		return result
	}
	if _, err := os.Stat(worktreeRoot); err != nil {
		result["probe_error"] = "worktree_path_missing"
		return result
	}

	refs := durableWorktreeProbeRefs(ctx, repoRoot, row)
	result["checked_refs"] = refs
	head, err := readGitCommit(ctx, worktreeRoot, "HEAD")
	if err != nil {
		result["probe_error"] = "worktree_head_unavailable"
		return result
	}
	result["head"] = head
	if len(refs) == 0 {
		result["anchor"] = worktreeAnchorUnreachable
		return result
	}
	for _, ref := range refs {
		if readGitAncestor(ctx, repoRoot, head, ref) {
			result["reachable"] = true
			result["anchored_ref"] = ref
			if strings.HasPrefix(ref, "refs/heads/") {
				result["anchor"] = worktreeAnchorRunBranch
			} else {
				result["anchor"] = worktreeAnchorJobPin
			}
			return result
		}
	}
	result["anchor"] = worktreeAnchorUnreachable
	return result
}

func readWorktreeTarget(repoRoot, pathText string) (string, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", fmt.Errorf("repo_root_invalid")
	}
	pathText = strings.TrimSpace(pathText)
	if pathText == "" {
		return "", fmt.Errorf("worktree_path_missing")
	}
	var target string
	if filepath.IsAbs(pathText) {
		target = filepath.Clean(pathText)
	} else {
		target = filepath.Clean(filepath.Join(root, filepath.FromSlash(pathText)))
	}
	worktreesRoot := filepath.Join(root, ".striatum", "worktrees")
	if !readPathWithin(worktreesRoot, target) {
		return "", fmt.Errorf("worktree_path_outside_scratch")
	}
	return target, nil
}

func readPathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func durableWorktreeProbeRefs(ctx context.Context, repoRoot string, row map[string]any) []string {
	refs := []string{}
	if branch := strings.TrimSpace(stringFrom(row, "branch_name")); branch != "" {
		refs = appendUniqueString(refs, branchToRef(branch))
	} else if branch := strings.TrimSpace(stringFrom(row, "base_branch")); branch != "" {
		refs = appendUniqueString(refs, branchToRef(branch))
	}
	runID := strings.TrimSpace(stringFrom(row, "run_id"))
	jobID := strings.TrimSpace(stringFrom(row, "job_id"))
	if runID != "" && jobID != "" {
		refs = appendUniqueString(refs, "refs/striatum/"+runID+"/"+jobID)
	}
	if runID != "" {
		out, err := readGitOutput(ctx, repoRoot, "for-each-ref", "--format=%(refname)", "refs/striatum/"+runID+"/")
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					refs = appendUniqueString(refs, line)
				}
			}
		}
	}
	return refs
}

func branchToRef(branch string) string {
	if strings.HasPrefix(branch, "refs/") {
		return branch
	}
	return "refs/heads/" + branch
}

func appendUniqueString(items []string, item string) []string {
	for _, existing := range items {
		if existing == item {
			return items
		}
	}
	return append(items, item)
}

func readGitCommit(ctx context.Context, repoRoot, ref string) (string, error) {
	return readGitOutput(ctx, repoRoot, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
}

func readGitAncestor(ctx context.Context, repoRoot, ancestor, descendantRef string) bool {
	if _, err := readGitCommit(ctx, repoRoot, descendantRef); err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "merge-base", "--is-ancestor", ancestor, descendantRef)
	return cmd.Run() == nil
}

func readGitOutput(ctx context.Context, repoRoot string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", repoRoot}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
