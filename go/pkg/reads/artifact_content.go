package reads

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// HandleArtifactGetContent fetches the body of an artifact and returns
// it base64-encoded inside an RPC envelope. Source of bytes:
//
//   - blob-routed artifact (blob_key set on the row): GET from the
//     per-repo S3 bucket, verify sha256 against blob_sha256, return.
//     Refuses with blob_disabled when the daemon was started without
//     STRIATUM_BLOB_ENDPOINT but the artifact references a blob.
//   - legacy artifact (blob_key NULL): read repo_path from disk via
//     striatumd.repositories.repo_root. Bodies still anchor on the
//     content_sha256 column; the disk file is verified against it.
//
// Required params: repository_id, artifact_id.
//
// Response shape (RFC 0072):
//
//	{
//	  "artifact_id": "...",
//	  "content_type": "text/markdown; charset=utf-8",
//	  "body_base64": "...",
//	  "sha256":      "...",
//	  "verified":    true,
//	  "source":      "blob" | "repo_path"
//	}
func HandleArtifactGetContent(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	artifactID := stringParam(envelope, "artifact_id")
	if artifactID == "" {
		return nil, rpc.NewError("schema_invalid", "artifact.get_content requires artifact_id", nil)
	}
	rows, err := collectRows(ctx, runner,
		`SELECT a.artifact_id, a.run_id, a.repo_path, a.content_sha256, a.artifact_kind,
		        a.blob_key, a.blob_sha256, a.blob_content_type`+artifactPlacementProjection(ctx, runner, "a")+`
		   FROM striatumd.artifacts a
		  WHERE a.repository_id = $1 AND a.artifact_id = $2
		  LIMIT 1`,
		repositoryID, artifactID,
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, rpc.NewError("not_found", "artifact not found: "+artifactID, nil)
	}
	row := rows[0]
	decorateArtifactPlacements(rows)
	blobKey := stringFrom(row, "blob_key")
	blobSha256 := stringFrom(row, "blob_sha256")
	contentSha256 := stringFrom(row, "content_sha256")

	if blobKey != "" {
		return getContentFromBlob(ctx, runner, repositoryID, artifactID, row, blobKey, blobSha256, contentSha256)
	}
	return getContentFromRepoPath(ctx, runner, repositoryID, artifactID, row, contentSha256)
}

// HandleArtifactListForRun lists artifacts for a run with the
// blob-aware columns. Distinct from HandleListArtifacts because the
// list-for-run shape is targeted at the web UI's per-run artifact
// sidebar; it returns blob_key alongside the legacy repo_path so the
// renderer can route via /artifacts/<id> regardless.
func HandleArtifactListForRun(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "artifact.list_for_run requires run_id", nil)
	}
	items, err := collectRows(ctx, runner,
		`SELECT a.artifact_id, a.run_id, a.job_id, a.session_id, a.logical_name,
		        a.artifact_kind, a.repo_path, a.content_sha256, a.size_bytes,
		        a.publish_mode, a.created_at, a.author_line,
		        a.blob_key, a.blob_sha256, a.blob_content_type`+artifactPlacementProjection(ctx, runner, "a")+artifactProvenanceColumns+`
		   FROM striatumd.artifacts a`+artifactProvenanceJoins+`
		  WHERE a.repository_id = $1 AND a.run_id = $2
		  ORDER BY a.created_at ASC`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	decorateArtifactPlacements(items)
	decorateArtifactProvenance(items)
	return map[string]any{
		"run_id": runID,
		"count":  len(items),
		"items":  items,
	}, nil
}

func getContentFromBlob(
	ctx context.Context,
	runner db.Runner,
	repositoryID, artifactID string,
	row map[string]any,
	blobKey, blobSha256, contentSha256 string,
) (map[string]any, error) {
	if packageBlobClient == nil {
		return nil, rpc.NewError(
			"blob_disabled",
			fmt.Sprintf("artifact %s references blob storage but the daemon is configured without STRIATUM_BLOB_ENDPOINT", artifactID),
			map[string]any{"blob_key": blobKey},
		)
	}
	bucket, err := lookupRepoBlobBucketRead(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	if bucket == "" {
		return nil, rpc.NewError(
			"blob_disabled",
			fmt.Sprintf("artifact %s references blob storage but repository %s has no blob_bucket configured", artifactID, repositoryID),
			map[string]any{"blob_key": blobKey},
		)
	}
	expected := blobSha256
	if expected == "" {
		expected = contentSha256
	}
	body, err := packageBlobClient.GetBytes(ctx, bucket, blobKey, expected)
	if err != nil {
		return nil, rpc.NewError("blob_read_failed", err.Error(), map[string]any{
			"bucket": bucket,
			"key":    blobKey,
		})
	}
	contentType := stringFrom(row, "blob_content_type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return map[string]any{
		"artifact_id":  artifactID,
		"content_type": contentType,
		"body_base64":  base64.StdEncoding.EncodeToString(body),
		"sha256":       expected,
		"verified":     true,
		"source":       "blob",
		"placement":    stringFrom(row, "placement"),
	}, nil
}

func getContentFromRepoPath(
	ctx context.Context,
	runner db.Runner,
	repositoryID, artifactID string,
	row map[string]any,
	contentSha256 string,
) (map[string]any, error) {
	repoPath := stringFrom(row, "repo_path")
	if repoPath == "" {
		return nil, rpc.NewError("artifact_error", "artifact has neither blob_key nor repo_path", map[string]any{
			"artifact_id": artifactID,
		})
	}
	repoRoot, err := lookupRepoRoot(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	if repoRoot == "" {
		return nil, rpc.NewError("not_found", "repository not registered: "+repositoryID, nil)
	}
	absPath := filepath.Join(repoRoot, repoPath)
	if !strings.HasPrefix(absPath, repoRoot+string(filepath.Separator)) && absPath != repoRoot {
		return nil, rpc.NewError("artifact_error", "artifact repo_path escapes repo_root", map[string]any{
			"artifact_id": artifactID,
		})
	}
	body, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, rpc.NewError("not_found", "artifact body file does not exist on disk", map[string]any{
				"artifact_id": artifactID,
				"repo_path":   repoPath,
			})
		}
		return nil, rpc.NewError("artifact_error", err.Error(), nil)
	}
	if contentSha256 != "" {
		sum := sha256.Sum256(body)
		got := hex.EncodeToString(sum[:])
		if got != contentSha256 {
			return nil, rpc.NewError("artifact_error", "repo_path sha256 mismatch", map[string]any{
				"expected": contentSha256,
				"got":      got,
			})
		}
	}
	return map[string]any{
		"artifact_id":  artifactID,
		"content_type": "application/octet-stream",
		"body_base64":  base64.StdEncoding.EncodeToString(body),
		"sha256":       contentSha256,
		"verified":     contentSha256 != "",
		"source":       "repo_path",
		"placement":    stringFrom(row, "placement"),
	}, nil
}

func lookupRepoBlobBucketRead(ctx context.Context, runner db.Runner, repositoryID string) (string, error) {
	rows, err := collectRows(ctx, runner,
		`SELECT blob_bucket FROM striatumd.repositories
		  WHERE repository_id = $1 AND state != 'removed' LIMIT 1`,
		repositoryID,
	)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return stringFrom(rows[0], "blob_bucket"), nil
}

func lookupRepoRoot(ctx context.Context, runner db.Runner, repositoryID string) (string, error) {
	rows, err := collectRows(ctx, runner,
		`SELECT repo_root FROM striatumd.repositories
		  WHERE repository_id = $1 AND state != 'removed' LIMIT 1`,
		repositoryID,
	)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return stringFrom(rows[0], "repo_root"), nil
}
