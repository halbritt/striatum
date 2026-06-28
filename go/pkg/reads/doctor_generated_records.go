package reads

import (
	"context"
	"fmt"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
)

const (
	generatedRecordTableProbeFailed      = "generated_record_table_probe_failed"
	generatedRecordDuplicateRows         = "generated_record_duplicate_rows"
	generatedRecordBlobMetadataMissing   = "generated_record_blob_metadata_missing"
	generatedRecordBlobMissing           = "generated_record_blob_missing"
	generatedRecordBlobKeyHashMismatch   = "generated_record_blob_key_hash_mismatch"
	generatedRecordBlobBodyVerifyFailed  = "generated_record_blob_body_verify_failed"
	generatedRecordContentHashMismatch   = "generated_record_content_hash_mismatch"
	generatedRecordIntegrityReadFailed   = "generated_record_integrity_read_failed"
	generatedRecordRepositoryBucketEmpty = "generated_record_repository_blob_bucket_missing"
)

func doctorGeneratedRecordIntegrity(ctx context.Context, runner db.Runner, repositoryID string, blobBlock map[string]any) (map[string]any, []string, []map[string]any) {
	block := map[string]any{
		"checked": false,
		"skipped": artifactAnchorSkipReason(repositoryID, blobBlock),
	}
	if block["skipped"] != "" {
		return block, nil, nil
	}
	probeRows, err := collectRows(ctx, runner, `SELECT to_regclass('striatumd.generated_records')::text AS table_name`)
	if err != nil {
		block["checked"] = true
		block["skipped"] = nil
		block["error"] = err.Error()
		record := generatedRecordProblemRecord(generatedRecordTableProbeFailed, "", "", err.Error())
		return block, []string{generatedRecordProblemString(generatedRecordTableProbeFailed, "schema", err.Error())}, []map[string]any{record}
	}
	if len(probeRows) == 0 || stringFrom(probeRows[0], "table_name") == "" {
		block["skipped"] = "generated_records_table_absent"
		return block, nil, nil
	}
	block["checked"] = true
	block["skipped"] = nil

	bucket, err := lookupRepoBlobBucketRead(ctx, runner, repositoryID)
	if err != nil {
		block["error"] = err.Error()
		record := generatedRecordProblemRecord(generatedRecordIntegrityReadFailed, "", "", err.Error())
		return block, []string{generatedRecordProblemString(generatedRecordIntegrityReadFailed, "repository", err.Error())}, []map[string]any{record}
	}
	if bucket == "" {
		record := generatedRecordProblemRecord(generatedRecordRepositoryBucketEmpty, "", "", "repository has no blob_bucket")
		block["problem_count"] = 1
		return block, []string{generatedRecordProblemString(generatedRecordRepositoryBucketEmpty, repositoryID, "repository has no blob_bucket")}, []map[string]any{record}
	}

	rows, err := collectRows(ctx, runner, `
		SELECT record_id, source_path, source_commit, record_class,
		       content_sha256, blob_key, blob_sha256, content_type, size_bytes
		  FROM striatumd.generated_records
		 WHERE repository_id = $1
		   AND status = 'indexed'
		 ORDER BY source_path, record_id`, repositoryID)
	if err != nil {
		block["error"] = err.Error()
		record := generatedRecordProblemRecord(generatedRecordIntegrityReadFailed, "", "", err.Error())
		return block, []string{generatedRecordProblemString(generatedRecordIntegrityReadFailed, "rows", err.Error())}, []map[string]any{record}
	}
	duplicateRows, err := collectRows(ctx, runner, `
		SELECT source_path, COALESCE(source_commit, '') AS source_commit, COUNT(*) AS c
		  FROM striatumd.generated_records
		 WHERE repository_id = $1
		   AND status = 'indexed'
		 GROUP BY source_path, COALESCE(source_commit, '')
		HAVING COUNT(*) > 1
		 ORDER BY source_path`, repositoryID)
	if err != nil {
		block["error"] = err.Error()
		record := generatedRecordProblemRecord(generatedRecordIntegrityReadFailed, "", "", err.Error())
		return block, []string{generatedRecordProblemString(generatedRecordIntegrityReadFailed, "duplicates", err.Error())}, []map[string]any{record}
	}

	problems := []string{}
	records := []map[string]any{}
	for _, row := range duplicateRows {
		sourcePath := stringFrom(row, "source_path")
		detail := fmt.Sprintf("%d generated_records rows share source path/commit", intFrom(row, "c"))
		problems = append(problems, generatedRecordProblemString(generatedRecordDuplicateRows, sourcePath, detail))
		records = append(records, generatedRecordProblemRecord(generatedRecordDuplicateRows, "", sourcePath, detail))
	}

	checked := 0
	for _, row := range rows {
		checked++
		recordID := stringFrom(row, "record_id")
		sourcePath := stringFrom(row, "source_path")
		contentSHA := strings.TrimSpace(stringFrom(row, "content_sha256"))
		blobKey := strings.TrimSpace(stringFrom(row, "blob_key"))
		blobSHA := strings.TrimSpace(stringFrom(row, "blob_sha256"))
		if contentSHA == "" || blobKey == "" || blobSHA == "" {
			detail := "blob_key/blob_sha256/content_sha256 must all be present"
			problems = append(problems, generatedRecordProblemString(generatedRecordBlobMetadataMissing, recordID, detail))
			records = append(records, generatedRecordProblemRecord(generatedRecordBlobMetadataMissing, recordID, sourcePath, detail))
			continue
		}
		if contentSHA != blobSHA {
			detail := fmt.Sprintf("content_sha256=%s blob_sha256=%s", contentSHA, blobSHA)
			problems = append(problems, generatedRecordProblemString(generatedRecordContentHashMismatch, recordID, detail))
			records = append(records, generatedRecordProblemRecord(generatedRecordContentHashMismatch, recordID, sourcePath, detail))
			continue
		}
		remoteSHA, exists, err := packageBlobClient.RemoteSha256(ctx, bucket, blobKey)
		if err != nil {
			detail := err.Error()
			problems = append(problems, generatedRecordProblemString(generatedRecordBlobBodyVerifyFailed, recordID, detail))
			records = append(records, generatedRecordProblemRecord(generatedRecordBlobBodyVerifyFailed, recordID, sourcePath, detail))
			continue
		}
		if !exists {
			detail := "blob key not found: " + blobKey
			problems = append(problems, generatedRecordProblemString(generatedRecordBlobMissing, recordID, detail))
			records = append(records, generatedRecordProblemRecord(generatedRecordBlobMissing, recordID, sourcePath, detail))
			continue
		}
		if remoteSHA == "" {
			detail := "blob lacks X-Striatum-Sha256 metadata"
			problems = append(problems, generatedRecordProblemString(generatedRecordBlobMetadataMissing, recordID, detail))
			records = append(records, generatedRecordProblemRecord(generatedRecordBlobMetadataMissing, recordID, sourcePath, detail))
			continue
		}
		if remoteSHA != blobSHA {
			detail := fmt.Sprintf("row blob_sha256=%s remote sha256=%s", blobSHA, remoteSHA)
			problems = append(problems, generatedRecordProblemString(generatedRecordBlobKeyHashMismatch, recordID, detail))
			records = append(records, generatedRecordProblemRecord(generatedRecordBlobKeyHashMismatch, recordID, sourcePath, detail))
			continue
		}
		if _, err := packageBlobClient.GetBytes(ctx, bucket, blobKey, blobSHA); err != nil {
			detail := err.Error()
			problems = append(problems, generatedRecordProblemString(generatedRecordBlobBodyVerifyFailed, recordID, detail))
			records = append(records, generatedRecordProblemRecord(generatedRecordBlobBodyVerifyFailed, recordID, sourcePath, detail))
		}
	}
	block["record_count"] = len(rows)
	block["checked_count"] = checked
	block["duplicate_source_count"] = len(duplicateRows)
	block["problem_count"] = len(problems)
	return block, problems, records
}

func generatedRecordProblemString(code, id, detail string) string {
	if id == "" {
		id = "unknown"
	}
	if detail == "" {
		return code + "." + id
	}
	return code + "." + id + ": " + detail
}

func generatedRecordProblemRecord(code, recordID, sourcePath, detail string) map[string]any {
	context := map[string]any{}
	if sourcePath != "" {
		context["source_path"] = sourcePath
	}
	if detail != "" {
		context["detail"] = detail
	}
	record := map[string]any{
		"check":   code,
		"context": context,
	}
	if recordID != "" {
		record["id"] = recordID
	}
	return record
}
