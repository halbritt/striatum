package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/halbritt/striatum/go/pkg/blob"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// HandleArtifactBackfillBlob copies an artifact's repo-path body into
// the per-repo S3 bucket for an artifact row that was published before
// RFC 0072 step 3 wired blob uploads into artifact.publish, and updates
// the row's blob_key / blob_sha256 / blob_content_type columns.
//
// Used to recover artifacts that resolve to blob_exhaust (explicit placement or
// legacy kind default — finding, synthesis, etc.) but landed only on
// repo_path because the publishing daemon binary predated step 3.
//
// Idempotent: if the row already has a non-empty blob_key, the handler
// returns status=skipped_already_in_blob without re-uploading.
//
// Required params: repository_id, artifact_id.
// Optional params: dry_run (bool).
func HandleArtifactBackfillBlob(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	artifactID := stringParam(envelope, "artifact_id")
	if artifactID == "" {
		return nil, rpc.NewError("schema_invalid", "artifact.backfill_blob requires artifact_id", nil)
	}
	if packageBlobClient == nil {
		return nil, rpc.NewError(
			"blob_disabled",
			"daemon is not configured for blob storage (STRIATUM_BLOB_ENDPOINT unset)",
			nil,
		)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		return backfillArtifactBlob(ctx, tx, repositoryID, artifactID, boolParam(envelope, "dry_run"))
	})
}

func backfillArtifactBlob(
	ctx context.Context,
	runner any,
	repositoryID string,
	artifactID string,
	dryRun bool,
) (map[string]any, error) {
	placementColumn := ""
	if dbRunner, ok := runner.(db.Runner); ok && db.ArtifactPlacementColumnPresent(ctx, dbRunner) {
		placementColumn = ", a.placement"
	}
	row, err := oneRow(ctx, runner, `
		SELECT a.artifact_id, a.run_id, a.job_id, a.logical_name, a.artifact_kind,
		       a.repo_path, a.content_sha256, a.size_bytes,
		       a.blob_key, r.repo_root`+placementColumn+`
		  FROM striatumd.artifacts a
		  JOIN striatumd.repositories r
		    ON r.repository_id = a.repository_id
		 WHERE a.repository_id = $1 AND a.artifact_id = $2
		 LIMIT 1`, repositoryID, artifactID)
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, rpc.NewError("not_found", "artifact not found: "+artifactID, nil)
		}
		return nil, err
	}

	existingBlobKey, _ := row["blob_key"].(string)
	if existingBlobKey != "" {
		return map[string]any{
			"status":      "skipped_already_in_blob",
			"artifact_id": artifactID,
			"blob_key":    existingBlobKey,
		}, nil
	}

	kind, _ := row["artifact_kind"].(string)
	placement := artifactcontracts.ResolvePlacement(kind, row["placement"])
	if !artifactcontracts.PlacementUsesBlob(placement) {
		return nil, rpc.NewError(
			"invalid_transition",
			fmt.Sprintf("artifact placement %q is not blob_exhaust; only blob-exhaust artifacts are eligible for backfill", placement),
			map[string]any{"skipped_kind": kind, "placement": placement},
		)
	}

	runID, _ := row["run_id"].(string)
	jobID, _ := row["job_id"].(string)
	logicalName, _ := row["logical_name"].(string)
	repoPath, _ := row["repo_path"].(string)
	contentSha256, _ := row["content_sha256"].(string)
	repoRoot, _ := row["repo_root"].(string)

	if runID == "" || jobID == "" || logicalName == "" || repoPath == "" || repoRoot == "" {
		return nil, rpc.NewError(
			"invalid_transition",
			"artifact row missing run_id / job_id / logical_name / repo_path / repo_root for blob key derivation",
			nil,
		)
	}

	if strings.HasPrefix(repoPath, "/") || strings.Contains(repoPath, "..") {
		return nil, rpc.NewError("schema_invalid", "repo_path must be a sanitized repo-relative path", nil)
	}
	absPath := filepath.Join(repoRoot, repoPath)
	body, err := os.ReadFile(absPath)
	if err != nil {
		return nil, rpc.NewError("file_read_failed", err.Error(), map[string]any{
			"repo_root": repoRoot,
			"repo_path": repoPath,
		})
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if contentSha256 != "" && digest != contentSha256 {
		return nil, rpc.NewError(
			"sha256_mismatch",
			"file body sha256 does not match artifact.content_sha256 — repo file has drifted from the published artifact",
			map[string]any{
				"repo_path": repoPath,
				"file_sha":  digest,
				"row_sha":   contentSha256,
			},
		)
	}

	bucket, err := lookupRepoBlobBucket(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	if bucket == "" {
		return nil, rpc.NewError(
			"blob_disabled",
			fmt.Sprintf("repository %s has no blob_bucket configured", repositoryID),
			map[string]any{"repository_id": repositoryID},
		)
	}

	key := blob.ArtifactKey(runID, jobID, logicalName)
	contentType := artifactContentType(repoPath)

	if dryRun {
		return map[string]any{
			"status":      "would_backfill",
			"artifact_id": artifactID,
			"blob_key":    key,
			"blob_sha256": digest,
			"size_bytes":  len(body),
			"kind":        kind,
		}, nil
	}

	uploadedSha, err := packageBlobClient.PutBytes(ctx, bucket, key, body, contentType)
	if err != nil {
		return nil, rpc.NewError("blob_publish_failed", err.Error(), map[string]any{
			"bucket": bucket,
			"key":    key,
		})
	}
	if uploadedSha != digest {
		return nil, rpc.NewError("blob_publish_failed", "sha256 mismatch after upload", map[string]any{
			"bucket":   bucket,
			"key":      key,
			"expected": digest,
			"got":      uploadedSha,
		})
	}

	tx, ok := runner.(db.TxRunner)
	if !ok {
		return nil, fmt.Errorf("backfillArtifactBlob requires a TxRunner")
	}
	if err := tx.Exec(ctx, `
		UPDATE striatumd.artifacts
		   SET blob_key = $1, blob_sha256 = $2, blob_content_type = $3
		 WHERE repository_id = $4 AND artifact_id = $5`,
		key, digest, contentType, repositoryID, artifactID); err != nil {
		return nil, err
	}

	if _, err := appendEvent(ctx, runner, repositoryID, runID, "artifact.blob_backfilled", nil, jobID, nil, artifactID, nil, map[string]any{
		"artifact_id": artifactID,
		"blob_key":    key,
		"blob_sha256": digest,
		"size_bytes":  len(body),
		"kind":        kind,
	}); err != nil {
		return nil, err
	}

	return map[string]any{
		"status":      "backfilled",
		"artifact_id": artifactID,
		"blob_key":    key,
		"blob_sha256": digest,
		"size_bytes":  len(body),
		"kind":        kind,
	}, nil
}
