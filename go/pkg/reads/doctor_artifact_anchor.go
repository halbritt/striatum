package reads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/halbritt/striatum/go/pkg/db"
)

const (
	artifactAnchorHashMismatch   = "artifact_anchor_hash_mismatch"
	artifactAnchorMissingFile    = "artifact_anchor_missing_file"
	artifactBlobMetadataMissing  = "artifact_blob_metadata_missing"
	artifactBlobBodyVerifyFailed = "artifact_blob_body_verify_failed"
)

func doctorArtifactAnchorIntegrity(ctx context.Context, runner db.Runner, repositoryID string, blobBlock map[string]any) (map[string]any, []string, []map[string]any) {
	block := map[string]any{
		"checked": false,
		"skipped": artifactAnchorSkipReason(repositoryID, blobBlock),
	}
	if block["skipped"] != "" {
		return block, nil, nil
	}

	rows, err := collectRows(ctx, runner, `
		SELECT a.repository_id, a.artifact_id, a.run_id, a.job_id, a.logical_name, a.repo_path,
		       a.content_sha256, a.artifact_kind, a.blob_key,
		       a.blob_sha256, a.blob_content_type`+artifactPlacementProjectionAny(ctx, runner, "a")+`,
		       j.workflow_job_id, j.attempt, j.write_scope_json,
		       r.repo_root, r.branch_name
		  FROM striatumd.artifacts a
		  JOIN striatumd.jobs j
		    ON j.repository_id = a.repository_id
		   AND j.job_id = a.job_id
		  JOIN striatumd.runs r
		    ON r.repository_id = a.repository_id
		   AND r.run_id = a.run_id
		 WHERE a.repository_id = $1
		   AND j.state = 'completed'
		   AND COALESCE(j.write_scope_json->>'repo_write', 'false') = 'true'
		 ORDER BY a.run_id, a.job_id, a.created_at, a.artifact_id`,
		repositoryID,
	)
	if err != nil {
		block["error"] = err.Error()
		return block, nil, nil
	}

	block["checked"] = true
	block["skipped"] = nil
	block["artifact_count"] = len(rows)
	decorateArtifactPlacements(rows)
	problems := []string{}
	records := []map[string]any{}
	gitChecked := 0
	blobChecked := 0
	bucket := ""
	if packageBlobClient != nil {
		var err error
		bucket, err = lookupRepoBlobBucketRead(ctx, runner, repositoryID)
		if err != nil {
			block["error"] = err.Error()
			return block, nil, nil
		}
	}
	for _, row := range rows {
		placement := artifactcontracts.ResolvePlacement(stringFrom(row, "artifact_kind"), row["placement"])
		row["placement"] = placement
		var problem string
		var record map[string]any
		if artifactcontracts.PlacementUsesBlob(placement) {
			blobChecked++
			problem, record = checkBlobExhaustArtifact(ctx, row, bucket)
		} else if artifactcontracts.PlacementUsesGitAnchor(placement) {
			gitChecked++
			problem, record = checkArtifactAnchor(ctx, row)
		}
		if problem == "" {
			continue
		}
		problems = append(problems, problem)
		records = append(records, record)
	}
	block["git_anchor_count"] = gitChecked
	block["blob_exhaust_count"] = blobChecked
	block["problem_count"] = len(problems)
	return block, problems, records
}

func checkBlobExhaustArtifact(ctx context.Context, row map[string]any, bucket string) (string, map[string]any) {
	blobKey := strings.TrimSpace(stringFrom(row, "blob_key"))
	expected := strings.TrimSpace(stringFrom(row, "blob_sha256"))
	if expected == "" {
		expected = strings.TrimSpace(stringFrom(row, "content_sha256"))
	}
	if blobKey == "" || expected == "" {
		return artifactBlobProblem(artifactBlobMetadataMissing, row, "blob_key_or_sha_missing")
	}
	if packageBlobClient == nil {
		return "", nil
	}
	if bucket == "" {
		return artifactBlobProblem(artifactBlobBodyVerifyFailed, row, "repository_blob_bucket_missing")
	}
	if _, err := packageBlobClient.GetBytes(ctx, bucket, blobKey, expected); err != nil {
		return artifactBlobProblem(artifactBlobBodyVerifyFailed, row, err.Error())
	}
	return "", nil
}

func artifactAnchorSkipReason(repositoryID string, blobBlock map[string]any) string {
	if strings.TrimSpace(repositoryID) == "" {
		return "repository_id_missing"
	}
	if blobBlock["configured"] != true {
		return "blob_not_configured"
	}
	if blobBlock["reachable"] != true {
		return "blob_unreachable"
	}
	if stringFrom(blobBlock, "bucket_status") != "ok" {
		return "blob_bucket_not_ok"
	}
	return ""
}

func checkArtifactAnchor(ctx context.Context, row map[string]any) (string, map[string]any) {
	repoPath, ok := cleanArtifactAnchorPath(stringFrom(row, "repo_path"))
	if !ok {
		return artifactAnchorProblem(artifactAnchorMissingFile, row, "", "", "invalid_repo_path")
	}
	expected := strings.TrimSpace(stringFrom(row, "content_sha256"))
	repoRoot := strings.TrimSpace(stringFrom(row, "repo_root"))
	refs := durableWorktreeProbeRefs(ctx, repoRoot, row)
	if repoRoot == "" || expected == "" || len(refs) == 0 {
		return "", nil
	}

	checkedRefs := []string{}
	fileFound := false
	var mismatchAnchor artifactAnchorProbe
	var firstAnchor artifactAnchorProbe
	for _, ref := range refs {
		commit, err := readGitCommit(ctx, repoRoot, ref)
		if err != nil {
			continue
		}
		checkedRefs = appendUniqueString(checkedRefs, ref)
		if firstAnchor.Ref == "" {
			firstAnchor = artifactAnchorProbe{Ref: ref, Commit: commit}
		}
		probe, err := readGitBlobSHA256(ctx, repoRoot, commit, repoPath)
		if err != nil {
			return artifactAnchorProblem(artifactAnchorMissingFile, row, ref, commit, err.Error())
		}
		if !probe.Exists {
			continue
		}
		fileFound = true
		if probe.SHA256 == expected {
			return "", nil
		}
		if mismatchAnchor.Ref == "" {
			mismatchAnchor = artifactAnchorProbe{Ref: ref, Commit: commit, SHA256: probe.SHA256}
		}
	}
	row["checked_refs"] = checkedRefs
	if fileFound {
		return artifactAnchorProblem(artifactAnchorHashMismatch, row, mismatchAnchor.Ref, mismatchAnchor.Commit, mismatchAnchor.SHA256)
	}
	return artifactAnchorProblem(artifactAnchorMissingFile, row, firstAnchor.Ref, firstAnchor.Commit, "path_not_present_in_checked_anchors")
}

type artifactAnchorProbe struct {
	Ref    string
	Commit string
	SHA256 string
	Exists bool
}

func readGitBlobSHA256(ctx context.Context, repoRoot, commit, repoPath string) (artifactAnchorProbe, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "show", commit+":"+repoPath)
	body, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return artifactAnchorProbe{Commit: commit, Exists: false}, nil
		}
		return artifactAnchorProbe{}, err
	}
	sum := sha256.Sum256(body)
	return artifactAnchorProbe{Commit: commit, SHA256: hex.EncodeToString(sum[:]), Exists: true}, nil
}

func cleanArtifactAnchorPath(pathText string) (string, bool) {
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

func artifactAnchorProblem(check string, row map[string]any, anchorRef, anchorCommit, detail string) (string, map[string]any) {
	artifactID := stringFrom(row, "artifact_id")
	repoPath := stringFrom(row, "repo_path")
	message := fmt.Sprintf("%s.%s: artifact %s at %s does not match its durable git anchor", check, artifactID, artifactID, repoPath)
	if check == artifactAnchorMissingFile {
		message = fmt.Sprintf("%s.%s: artifact %s missing at %s in durable git anchor", check, artifactID, artifactID, repoPath)
	}
	contextMap := map[string]any{
		"repository_id":  row["repository_id"],
		"run_id":         row["run_id"],
		"job_id":         row["job_id"],
		"artifact_id":    row["artifact_id"],
		"logical_name":   row["logical_name"],
		"repo_path":      row["repo_path"],
		"content_sha256": row["content_sha256"],
		"placement":      row["placement"],
		"anchor_kind":    artifactAnchorKind(anchorRef),
		"anchor_ref":     nullableString(anchorRef),
		"anchor_commit":  nullableString(anchorCommit),
		"checked_refs":   row["checked_refs"],
	}
	if check == artifactAnchorHashMismatch {
		contextMap["anchor_content_sha256"] = detail
	} else {
		contextMap["reason"] = detail
	}
	record := map[string]any{
		"check":   check,
		"id":      artifactID,
		"context": contextMap,
	}
	return message, record
}

func artifactBlobProblem(check string, row map[string]any, detail string) (string, map[string]any) {
	artifactID := stringFrom(row, "artifact_id")
	message := fmt.Sprintf("%s.%s: blob-exhaust artifact %s does not have a verified blob body", check, artifactID, artifactID)
	if check == artifactBlobMetadataMissing {
		message = fmt.Sprintf("%s.%s: blob-exhaust artifact %s is missing blob metadata", check, artifactID, artifactID)
	}
	record := map[string]any{
		"check": check,
		"id":    artifactID,
		"context": map[string]any{
			"repository_id":     row["repository_id"],
			"run_id":            row["run_id"],
			"job_id":            row["job_id"],
			"artifact_id":       row["artifact_id"],
			"logical_name":      row["logical_name"],
			"repo_path":         row["repo_path"],
			"content_sha256":    row["content_sha256"],
			"placement":         row["placement"],
			"blob_key":          row["blob_key"],
			"blob_sha256":       row["blob_sha256"],
			"blob_content_type": row["blob_content_type"],
			"reason":            detail,
		},
	}
	return message, record
}

func artifactAnchorKind(ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "refs/heads/") {
		return worktreeAnchorRunBranch
	}
	return worktreeAnchorJobPin
}
