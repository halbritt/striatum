package reads

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
	recordsmigration "github.com/halbritt/striatum/go/pkg/records/migration"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

func HandleRecordsMigrationMaterialize(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	if packageBlobClient == nil {
		return nil, rpc.NewError("blob_disabled", "daemon is not configured for blob storage", nil)
	}
	bucket, err := lookupRepoBlobBucketRead(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	if bucket == "" {
		return nil, rpc.NewError("blob_disabled", fmt.Sprintf("repository %s has no blob_bucket configured", repositoryID), nil)
	}
	rows, err := generatedRecordSelectorRows(ctx, runner, repositoryID, envelope)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, rpc.NewError("not_found", "no generated_records rows matched selector", nil)
	}
	records := make([]map[string]any, 0, len(rows))
	manifest := make([]recordsmigration.ManifestEntry, 0, len(rows))
	for _, row := range rows {
		recordID := stringFrom(row, "record_id")
		sourcePath := stringFrom(row, "source_path")
		blobKey := stringFrom(row, "blob_key")
		expected := firstNonEmpty(stringFrom(row, "blob_sha256"), stringFrom(row, "content_sha256"))
		if blobKey == "" || expected == "" {
			return nil, rpc.NewError("blob_read_failed", "generated record row lacks blob metadata", map[string]any{
				"record_id": recordID,
				"blob_key":  blobKey,
			})
		}
		body, err := packageBlobClient.GetBytes(ctx, bucket, blobKey, expected)
		if err != nil {
			return nil, rpc.NewError("blob_read_failed", err.Error(), map[string]any{"bucket": bucket, "key": blobKey, "record_id": recordID})
		}
		entry := recordsmigration.ManifestEntry{
			Path:         sourcePath,
			Size:         int64(len(body)),
			SHA256:       expected,
			SourceCommit: stringFrom(row, "source_commit"),
			RecordClass:  stringFrom(row, "record_class"),
		}
		manifest = append(manifest, entry)
		records = append(records, map[string]any{
			"record_id":      recordID,
			"source_path":    sourcePath,
			"source_commit":  entry.SourceCommit,
			"record_class":   entry.RecordClass,
			"content_type":   firstNonEmpty(stringFrom(row, "content_type"), contentTypeForRelPath(sourcePath)),
			"content_sha256": expected,
			"blob_key":       blobKey,
			"size_bytes":     len(body),
			"body_base64":    base64.StdEncoding.EncodeToString(body),
			"source":         "blob",
			"verified":       true,
		})
	}
	return map[string]any{
		"schema_version":   recordsmigration.MaterializeSchemaVersion,
		"record_count":     len(records),
		"records":          records,
		"manifest":         manifest,
		"deletion_allowed": false,
	}, nil
}

func HandleRecordsMigrationVerify(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	entries, err := manifestEntriesParam(envelope)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, rpc.NewError("schema_invalid", "records.migration.verify requires non-empty entries", nil)
	}
	if packageBlobClient == nil {
		return nil, rpc.NewError("blob_disabled", "daemon is not configured for blob storage", nil)
	}
	bucket, err := lookupRepoBlobBucketRead(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	if bucket == "" {
		return nil, rpc.NewError("blob_disabled", fmt.Sprintf("repository %s has no blob_bucket configured", repositoryID), nil)
	}
	problems := []map[string]any{}
	reconstructed := []recordsmigration.ManifestEntry{}
	for _, entry := range entries {
		entry.Path = recordsmigration.NormalizePath(entry.Path)
		rows, err := collectRows(ctx, runner, `
			SELECT record_id, source_path, source_commit, record_class,
			       content_sha256, blob_key, blob_sha256, content_type, size_bytes
			  FROM striatumd.generated_records
			 WHERE repository_id = $1
			   AND source_path = $2
			   AND ($3 = '' OR source_commit = $3)
			   AND status = 'indexed'
			 ORDER BY created_at DESC, record_id ASC`,
			repositoryID, entry.Path, strings.TrimSpace(entry.SourceCommit),
		)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			problems = append(problems, migrationProblem("generated_record_index_missing", entry.Path, "", "no generated_records row matched source path/commit"))
			continue
		}
		if len(rows) > 1 {
			problems = append(problems, migrationProblem("generated_record_duplicate_rows", entry.Path, "", fmt.Sprintf("%d generated_records rows matched source path/commit", len(rows))))
			continue
		}
		row := rows[0]
		recordID := stringFrom(row, "record_id")
		blobKey := stringFrom(row, "blob_key")
		blobSHA := stringFrom(row, "blob_sha256")
		contentSHA := stringFrom(row, "content_sha256")
		if blobKey == "" || blobSHA == "" || contentSHA == "" {
			problems = append(problems, migrationProblem("generated_record_blob_metadata_missing", entry.Path, recordID, "blob_key/blob_sha256/content_sha256 must all be present"))
			continue
		}
		remoteSHA, exists, err := packageBlobClient.RemoteSha256(ctx, bucket, blobKey)
		if err != nil {
			problems = append(problems, migrationProblem("generated_record_blob_head_failed", entry.Path, recordID, err.Error()))
			continue
		}
		if !exists {
			problems = append(problems, migrationProblem("generated_record_blob_missing", entry.Path, recordID, "blob key not found: "+blobKey))
			continue
		}
		if remoteSHA == "" {
			problems = append(problems, migrationProblem("generated_record_blob_metadata_missing", entry.Path, recordID, "blob lacks X-Striatum-Sha256 metadata"))
			continue
		}
		if remoteSHA != blobSHA {
			problems = append(problems, migrationProblem("generated_record_blob_key_hash_mismatch", entry.Path, recordID, fmt.Sprintf("row blob_sha256=%s remote sha256=%s", blobSHA, remoteSHA)))
			continue
		}
		body, err := packageBlobClient.GetBytes(ctx, bucket, blobKey, blobSHA)
		if err != nil {
			problems = append(problems, migrationProblem("generated_record_blob_body_verify_failed", entry.Path, recordID, err.Error()))
			continue
		}
		reconstructed = append(reconstructed, recordsmigration.ManifestEntry{
			Path:         entry.Path,
			Size:         int64(len(body)),
			SHA256:       blobSHA,
			SourceCommit: stringFrom(row, "source_commit"),
			RecordClass:  stringFrom(row, "record_class"),
		})
	}
	for _, problem := range recordsmigration.CompareManifests(entries, reconstructed) {
		problems = append(problems, migrationProblem(problem.Code, problem.Path, "", problem.Detail))
	}
	return map[string]any{
		"schema_version":    recordsmigration.VerificationSchemaVersion,
		"checked_count":     len(entries),
		"reconstructed":     reconstructed,
		"problem_count":     len(problems),
		"problems":          problems,
		"reconstructable":   len(problems) == 0,
		"deletion_allowed":  false,
		"deletion_blocked":  "historical source deletion is not authorized by this verifier slice",
		"scratch_required":  ".striatum/scratch",
		"verification_mode": "blob_index_reconstruction",
	}, nil
}

func generatedRecordSelectorRows(ctx context.Context, runner db.Runner, repositoryID string, envelope rpc.Envelope) ([]map[string]any, error) {
	recordID := strings.TrimSpace(stringParam(envelope, "record_id"))
	importBatchID := strings.TrimSpace(stringParam(envelope, "import_batch_id"))
	sourcePath := recordsmigration.NormalizePath(stringParam(envelope, "source_path"))
	limit := 1000
	if raw, ok := envelope.Params["limit"].(int); ok && raw > 0 && raw < limit {
		limit = raw
	}
	base := `SELECT record_id, source_path, source_commit, record_class,
	                content_sha256, blob_key, blob_sha256, content_type, size_bytes
	           FROM striatumd.generated_records
	          WHERE repository_id = $1 AND status = 'indexed'`
	switch {
	case recordID != "":
		return collectRows(ctx, runner, base+` AND record_id = $2 ORDER BY source_path LIMIT $3`, repositoryID, recordID, limit)
	case importBatchID != "":
		return collectRows(ctx, runner, base+` AND import_batch_id = $2 ORDER BY source_path LIMIT $3`, repositoryID, importBatchID, limit)
	case sourcePath != "":
		if recordsmigration.PathEscapesRepository(sourcePath) {
			return nil, rpc.NewError("path_outside_scope", "source_path must be repository-relative", map[string]any{"source_path": sourcePath})
		}
		return collectRows(ctx, runner, base+` AND source_path = $2 ORDER BY source_path LIMIT $3`, repositoryID, sourcePath, limit)
	default:
		return nil, rpc.NewError("schema_invalid", "records.migration.materialize requires record_id, import_batch_id, or source_path", nil)
	}
}

func manifestEntriesParam(envelope rpc.Envelope) ([]recordsmigration.ManifestEntry, error) {
	raw, ok := envelope.Params["entries"]
	if !ok {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, rpc.NewError("schema_invalid", "entries must be a JSON array", nil)
	}
	entries := make([]recordsmigration.ManifestEntry, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, rpc.NewError("schema_invalid", "entries must contain JSON objects", nil)
		}
		entry := recordsmigration.ManifestEntry{
			Path:         stringFrom(row, "path"),
			SourceCommit: stringFrom(row, "source_commit"),
			RecordClass:  stringFrom(row, "record_class"),
			SHA256:       strings.ToLower(strings.TrimSpace(stringFrom(row, "sha256"))),
			Size:         int64From(row, "size"),
		}
		if entry.Path == "" || entry.SHA256 == "" {
			return nil, rpc.NewError("schema_invalid", "manifest entries require path and sha256", nil)
		}
		if recordsmigration.PathEscapesRepository(entry.Path) {
			return nil, rpc.NewError("path_outside_scope", "manifest entry path must be repository-relative", map[string]any{"path": entry.Path})
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func migrationProblem(code, path, recordID, detail string) map[string]any {
	problem := map[string]any{"code": code}
	if path != "" {
		problem["path"] = path
	}
	if recordID != "" {
		problem["record_id"] = recordID
	}
	if detail != "" {
		problem["detail"] = detail
	}
	return problem
}
