package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
	recordsmigration "github.com/halbritt/striatum/go/pkg/records/migration"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// HandleRecordsMigrationImport imports one historical generated-record body
// through the daemon-owned blob client and indexes it in generated_records.
//
// The CLI performs the filesystem walk and sends one safe inventory entry per
// request. This handler owns the durable mutation: blob PUT/readback and the
// generated_records upsert. It never deletes source files.
func HandleRecordsMigrationImport(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sourcePath := recordsmigration.NormalizePath(stringParam(envelope, "source_path"))
	bodyB64, hasBody := envelope.Params["body_base64"].(string)
	recordClass := strings.TrimSpace(stringParam(envelope, "record_class"))
	if sourcePath == "" || !hasBody || recordClass == "" {
		return nil, rpc.NewError("schema_invalid", "records.migration.import requires source_path, body_base64, and record_class", nil)
	}
	if recordsmigration.PathEscapesRepository(sourcePath) {
		return nil, rpc.NewError("path_outside_scope", "source_path must be repository-relative and must not escape the repository", map[string]any{"source_path": sourcePath})
	}
	if packageBlobClient == nil {
		return nil, rpc.NewError("blob_disabled", "daemon is not configured for blob storage (STRIATUM_BLOB_ENDPOINT unset)", nil)
	}
	body, err := base64.StdEncoding.DecodeString(bodyB64)
	if err != nil {
		return nil, rpc.NewError("schema_invalid", "body_base64 decode failed: "+err.Error(), nil)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	expected := strings.ToLower(strings.TrimSpace(stringParam(envelope, "content_sha256")))
	if expected != "" && expected != digest {
		return nil, rpc.NewError("sha256_mismatch", "body sha256 does not match manifest content_sha256", map[string]any{
			"source_path": sourcePath,
			"expected":    expected,
			"got":         digest,
		})
	}
	sourceCommit := strings.TrimSpace(stringParam(envelope, "source_commit"))
	recordID := strings.TrimSpace(stringParam(envelope, "record_id"))
	if recordID == "" {
		recordID = recordsmigration.RecordID(sourceCommit, sourcePath, digest)
	}
	blobKey := strings.TrimSpace(stringParam(envelope, "blob_key"))
	if blobKey == "" {
		blobKey = recordsmigration.BlobKey(sourceCommit, sourcePath, digest)
	}
	if recordsmigration.PathEscapesRepository(strings.TrimPrefix(blobKey, "records/historical/")) || strings.HasPrefix(blobKey, "/") || strings.Contains(blobKey, "..") {
		return nil, rpc.NewError("path_outside_scope", "blob_key must be a safe relative blob key", map[string]any{"blob_key": blobKey})
	}
	contentType := stringParam(envelope, "content_type")
	if contentType == "" {
		contentType = artifactContentType(sourcePath)
	}
	retentionClass := strings.TrimSpace(stringParam(envelope, "retention_class"))
	if retentionClass == "" {
		retentionClass = recordsmigration.DefaultRetentionClass
	}
	importBatchID := strings.TrimSpace(stringParam(envelope, "import_batch_id"))
	if importBatchID == "" {
		importBatchID = "manual"
	}
	bundleID := strings.TrimSpace(stringParam(envelope, "bundle_id"))
	runID := strings.TrimSpace(stringParam(envelope, "run_id"))
	jobID := strings.TrimSpace(stringParam(envelope, "job_id"))
	artifactID := strings.TrimSpace(stringParam(envelope, "artifact_id"))

	bucket, err := lookupRepoBlobBucket(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	if bucket == "" {
		return nil, rpc.NewError(
			"blob_disabled",
			fmt.Sprintf("repository %s has no blob_bucket configured (run `striatum repo add <path> --apply-blob-creation` first)", repositoryID),
			map[string]any{"repository_id": repositoryID},
		)
	}

	remoteSha, exists, err := packageBlobClient.RemoteSha256(ctx, bucket, blobKey)
	if err != nil {
		return nil, rpc.NewError("blob_head_failed", err.Error(), map[string]any{"bucket": bucket, "key": blobKey})
	}
	if exists && remoteSha != "" && remoteSha != digest {
		return nil, rpc.NewError("sha256_mismatch", "existing blob key has a different X-Striatum-Sha256 value", map[string]any{
			"bucket":   bucket,
			"key":      blobKey,
			"expected": digest,
			"got":      remoteSha,
		})
	}

	status := "uploaded"
	if boolParam(envelope, "dry_run") {
		status = "would_import"
	} else {
		if !exists || remoteSha == "" {
			uploadedSha, err := packageBlobClient.PutBytes(ctx, bucket, blobKey, body, contentType)
			if err != nil {
				return nil, rpc.NewError("blob_publish_failed", err.Error(), map[string]any{"bucket": bucket, "key": blobKey})
			}
			if uploadedSha != digest {
				return nil, rpc.NewError("blob_publish_failed", "sha256 mismatch after upload", map[string]any{"bucket": bucket, "key": blobKey, "expected": digest, "got": uploadedSha})
			}
		} else {
			status = "skipped_already_present"
		}
		if err := runner.Exec(ctx, `
			INSERT INTO striatumd.generated_records (
			  repository_id, record_id, source_path, source_commit, record_class,
			  run_id, job_id, artifact_id, content_sha256, blob_key, blob_sha256,
			  content_type, size_bytes, retention_class, bundle_id, import_batch_id, status
			) VALUES (
			  $1, $2, $3, NULLIF($4, ''), $5,
			  NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9, $10, $11,
			  $12, $13, $14, NULLIF($15, ''), NULLIF($16, ''), 'indexed'
			)
			ON CONFLICT (repository_id, record_id) DO UPDATE
			   SET source_path = EXCLUDED.source_path,
			       source_commit = EXCLUDED.source_commit,
			       record_class = EXCLUDED.record_class,
			       run_id = EXCLUDED.run_id,
			       job_id = EXCLUDED.job_id,
			       artifact_id = EXCLUDED.artifact_id,
			       content_sha256 = EXCLUDED.content_sha256,
			       blob_key = EXCLUDED.blob_key,
			       blob_sha256 = EXCLUDED.blob_sha256,
			       content_type = EXCLUDED.content_type,
			       size_bytes = EXCLUDED.size_bytes,
			       retention_class = EXCLUDED.retention_class,
			       bundle_id = EXCLUDED.bundle_id,
			       import_batch_id = EXCLUDED.import_batch_id,
			       status = 'indexed'`,
			repositoryID, recordID, sourcePath, sourceCommit, recordClass,
			runID, jobID, artifactID, digest, blobKey, digest,
			contentType, int64(len(body)), retentionClass, bundleID, importBatchID,
		); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"schema_version":   recordsmigration.ImportSchemaVersion,
		"status":           status,
		"record_id":        recordID,
		"source_path":      sourcePath,
		"source_commit":    sourceCommit,
		"record_class":     recordClass,
		"content_sha256":   digest,
		"blob_key":         blobKey,
		"blob_sha256":      digest,
		"content_type":     contentType,
		"size_bytes":       len(body),
		"retention_class":  retentionClass,
		"import_batch_id":  importBatchID,
		"deletion_allowed": false,
	}, nil
}
