package mutations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/rpc"
)

func enforceWriteScopeClean(ctx context.Context, runner any, repositoryID string, job map[string]any) error {
	scope := asMap(job["write_scope_json"])
	if !isRepoWriteScope(scope) {
		return nil
	}
	allowed := stringListFromAny(scope["allowed_paths"])
	forbidden := stringListFromAny(scope["forbidden_paths"])
	if len(allowed) == 0 && len(forbidden) == 0 {
		return nil
	}
	repo, err := rowByID(ctx, runner, repositoryID, "repositories", "repository_id", repositoryID, false)
	if err != nil {
		return err
	}
	repoRoot := fmt.Sprint(repo["repo_root"])
	paths, err := gitTouchedPathsSinceBaseline(ctx, repoRoot, job)
	if err != nil {
		return rpc.NewError("invalid_transition", "write_scope check failed: "+err.Error(), nil)
	}
	ignoredPaths, err := publishedRunArtifactIgnoredPaths(ctx, runner, repositoryID, repoRoot, job, paths)
	if err != nil {
		return rpc.NewError("invalid_transition", "write_scope check failed: "+err.Error(), nil)
	}
	// #46: paths already dirty/changed at claim time (the baseline) that lie
	// outside allowed_paths are pre-existing operator state, not job writes.
	// Operator actions on them (e.g. committing an unrelated untracked file
	// mid-run, which removes it from `git status` and thus reads as "touched")
	// must not count as this job violating its write scope. Forbidden paths are
	// still enforced — the violation matcher checks forbidden before ignored.
	if ignoredPaths == nil {
		ignoredPaths = map[string]bool{}
	}
	for clean := range baselinePreexistingOutOfScope(job, allowed) {
		ignoredPaths[clean] = true
	}
	violations := writeScopeViolationsWithIgnored(paths, allowed, forbidden, ignoredPaths)
	if len(violations) == 0 {
		return nil
	}
	return rpc.NewError("invalid_transition", "write_scope violation: changed paths outside allowed_paths or inside forbidden_paths", map[string]any{
		"job_id":          job["job_id"],
		"workflow_job_id": job["workflow_job_id"],
		"violations":      violations,
		"allowed_paths":   allowed,
		"forbidden_paths": forbidden,
	})
}

func isRepoWriteScope(scope map[string]any) bool {
	return scope["repo_write"] == true || fmt.Sprint(scope["mode"]) == "repo_write"
}

func stringListFromAny(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return append([]string(nil), typed...)
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func gitChangedPaths(ctx context.Context, repoRoot string) ([]string, error) {
	snapshots, err := gitChangedPathSnapshots(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(snapshots))
	for _, item := range snapshots {
		paths = append(paths, item.Path)
	}
	return paths, nil
}

type gitPathSnapshot struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

func gitTouchedPathsSinceBaseline(ctx context.Context, repoRoot string, job map[string]any) ([]string, error) {
	current, err := gitChangedPathSnapshots(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	baseline := gitBaselineFromJob(job)
	if len(baseline) == 0 {
		paths := make([]string, 0, len(current))
		for _, item := range current {
			paths = append(paths, item.Path)
		}
		return paths, nil
	}
	currentByPath := map[string]string{}
	for _, item := range current {
		currentByPath[item.Path] = item.Hash
	}
	touched := []string{}
	for path, currentHash := range currentByPath {
		if baselineHash, ok := baseline[path]; !ok || baselineHash != currentHash {
			touched = append(touched, path)
		}
	}
	for path, baselineHash := range baseline {
		if _, ok := currentByPath[path]; !ok && baselineHash != "" {
			touched = append(touched, path)
		}
	}
	sort.Strings(touched)
	return dedupeStrings(touched), nil
}

// baselinePreexistingOutOfScope returns the normalized baseline paths (paths
// already dirty/changed when the job claimed) that lie outside allowed_paths —
// pre-existing operator state the job did not write. These are excluded from
// write_scope violations so an operator action on an unrelated out-of-scope path
// during a live job (e.g. committing an untracked file) is not blamed on the job.
func baselinePreexistingOutOfScope(job map[string]any, allowed []string) map[string]bool {
	out := map[string]bool{}
	allowedMatchers := normalizedScopeMatchers(allowed)
	if len(allowedMatchers) == 0 {
		return out
	}
	for path := range gitBaselineFromJob(job) {
		clean, ok := normalizeScopePath(path)
		if !ok {
			continue
		}
		if !pathMatchesAny(clean, allowedMatchers) {
			out[clean] = true
		}
	}
	return out
}

func gitBaselineFromJob(job map[string]any) map[string]string {
	raw := asMap(job["write_scope_baseline"])
	entries := asList(raw["changed_paths"])
	out := map[string]string{}
	for _, entry := range entries {
		item := asMap(entry)
		path, _ := item["path"].(string)
		hash, _ := item["hash"].(string)
		if path != "" {
			out[path] = hash
		}
	}
	return out
}

func gitChangedPathSnapshots(ctx context.Context, repoRoot string) ([]gitPathSnapshot, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return nil, fmt.Errorf("repository root is empty")
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", "-C", repoRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	output, err := cmd.Output()
	if cmdCtx.Err() != nil {
		return nil, fmt.Errorf("git status timed out")
	}
	if err != nil {
		return nil, err
	}
	paths := parseGitPorcelainZ(output)
	snapshots := make([]gitPathSnapshot, 0, len(paths))
	for _, path := range paths {
		hash, err := hashRepoPath(repoRoot, path)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, gitPathSnapshot{Path: path, Hash: hash})
	}
	return snapshots, nil
}

func hashRepoPath(repoRoot, path string) (string, error) {
	full := filepath.Join(repoRoot, filepath.FromSlash(path))
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if info.IsDir() {
		return "dir", nil
	}
	body, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func parseGitPorcelainZ(output []byte) []string {
	records := bytes.Split(output, []byte{0})
	paths := make([]string, 0, len(records))
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 {
			continue
		}
		status := string(record[:2])
		path := string(record[3:])
		if path != "" {
			paths = append(paths, filepath.ToSlash(path))
		}
		if strings.Contains(status, "R") || strings.Contains(status, "C") {
			if i+1 < len(records) && len(records[i+1]) > 0 {
				i++
				paths = append(paths, filepath.ToSlash(string(records[i])))
			}
		}
	}
	sort.Strings(paths)
	return dedupeStrings(paths)
}

func writeScopeViolations(paths []string, allowed []string, forbidden []string) []string {
	return writeScopeViolationsWithIgnored(paths, allowed, forbidden, nil)
}

func writeScopeViolationsWithIgnored(paths []string, allowed []string, forbidden []string, ignored map[string]bool) []string {
	allowedMatchers := normalizedScopeMatchers(allowed)
	forbiddenMatchers := normalizedScopeMatchers(forbidden)
	violations := make([]string, 0)
	for _, path := range paths {
		clean, ok := normalizeScopePath(path)
		if !ok {
			violations = append(violations, path)
			continue
		}
		if pathMatchesAny(clean, forbiddenMatchers) {
			violations = append(violations, clean)
			continue
		}
		if ignored[clean] {
			continue
		}
		if len(allowedMatchers) > 0 && !pathMatchesAny(clean, allowedMatchers) {
			violations = append(violations, clean)
		}
	}
	return dedupeStrings(violations)
}

func publishedRunArtifactIgnoredPaths(ctx context.Context, runner any, repositoryID, repoRoot string, job map[string]any, paths []string) (map[string]bool, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	runID := fmt.Sprint(job["run_id"])
	jobID := fmt.Sprint(job["job_id"])
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(jobID) == "" {
		return nil, nil
	}
	touched := map[string]bool{}
	for _, path := range paths {
		clean, ok := normalizeScopePath(path)
		if ok {
			touched[clean] = true
		}
	}
	if len(touched) == 0 {
		return nil, nil
	}
	rows, err := queryRows(ctx, runner, `
		SELECT repo_path, content_sha256
		  FROM striatumd.artifacts
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND job_id != $3`, repositoryID, runID, jobID)
	if err != nil {
		return nil, err
	}
	ignored := map[string]bool{}
	for _, row := range rows {
		clean, ok := normalizeScopePath(fmt.Sprint(row["repo_path"]))
		if !ok || !touched[clean] {
			continue
		}
		currentHash, err := hashRepoPath(repoRoot, clean)
		if err != nil {
			return nil, err
		}
		if currentHash != "" && currentHash == fmt.Sprint(row["content_sha256"]) {
			ignored[clean] = true
		}
	}
	if len(ignored) == 0 {
		return nil, nil
	}
	return ignored, nil
}

func normalizedScopeMatchers(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		clean, ok := normalizeScopePath(path)
		if ok {
			out = append(out, clean)
		}
	}
	return dedupeStrings(out)
}

func normalizeScopePath(path string) (string, bool) {
	path = strings.TrimSpace(filepath.ToSlash(path))
	if path == "" || path == "." || path == "./" {
		return ".", true
	}
	if strings.HasPrefix(path, "/") || strings.Contains(path, "../") || path == ".." {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "." || clean == "" {
		return ".", true
	}
	if strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
		return "", false
	}
	return strings.TrimSuffix(clean, "/"), true
}

func pathMatchesAny(path string, matchers []string) bool {
	for _, matcher := range matchers {
		if matcher == "." || path == matcher || strings.HasPrefix(path, matcher+"/") {
			return true
		}
	}
	return false
}

func dedupeStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	sort.Strings(items)
	out := items[:0]
	var prev string
	for i, item := range items {
		if i == 0 || item != prev {
			out = append(out, item)
			prev = item
		}
	}
	return out
}
