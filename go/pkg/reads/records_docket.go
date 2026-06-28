package reads

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/records"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// HandleRecordsDocket renders the RFC 0171 run docket from daemon-indexed
// artifact metadata and any generated_records rows already imported for the run.
func HandleRecordsDocket(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "records.docket requires run_id", nil)
	}
	format := strings.ToLower(strings.TrimSpace(stringParam(envelope, "format")))
	if format == "" {
		format = "markdown"
	}
	if format != "markdown" && format != "json" {
		return nil, rpc.NewError("schema_invalid", "records.docket format must be markdown or json", map[string]any{"format": format})
	}
	docket, err := recordsDocketForRun(ctx, runner, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	entryCount := len(docket.Normalize().Entries)
	if entryCount == 0 {
		return nil, rpc.NewError("not_found", "no artifact or generated-record entries found for run: "+runID, map[string]any{"run_id": runID})
	}
	root, err := docket.MerkleRoot()
	if err != nil {
		return nil, err
	}
	var body string
	contentType := "text/markdown; charset=utf-8"
	switch format {
	case "json":
		rendered, err := docket.RenderJSON()
		if err != nil {
			return nil, err
		}
		body = string(rendered) + "\n"
		contentType = "application/json"
	default:
		body, err = docket.RenderMarkdown()
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"run_id":         runID,
		"format":         format,
		"content_type":   contentType,
		"schema_version": records.SchemaVersion,
		"merkle_root":    root,
		"entry_count":    entryCount,
		"body":           body,
	}, nil
}

func recordsDocketForRun(ctx context.Context, runner db.Runner, repositoryID, runID string) (records.Docket, error) {
	artifactRows, err := collectRows(ctx, runner,
		`SELECT a.artifact_id, a.run_id, a.job_id, a.logical_name, a.artifact_kind,
		        a.repo_path, a.content_sha256, a.size_bytes,
		        a.blob_key, a.blob_sha256, a.blob_content_type`+artifactPlacementProjection(ctx, runner, "a")+`
		   FROM striatumd.artifacts a
		  WHERE a.repository_id = $1 AND a.run_id = $2
		  ORDER BY a.created_at ASC, a.artifact_id ASC`,
		repositoryID, runID,
	)
	if err != nil {
		return records.Docket{}, err
	}
	decorateArtifactPlacements(artifactRows)
	entries := make([]records.Entry, 0, len(artifactRows))
	for _, row := range artifactRows {
		entries = append(entries, artifactDocketEntry(row))
	}
	recordRows, err := collectRows(ctx, runner,
		`SELECT record_id, source_path, record_class, run_id, job_id, artifact_id,
		        content_sha256, blob_key, blob_sha256, content_type, size_bytes,
		        retention_class
		   FROM striatumd.generated_records
		  WHERE repository_id = $1 AND run_id = $2
		  ORDER BY created_at ASC, record_id ASC`,
		repositoryID, runID,
	)
	if err != nil {
		return records.Docket{}, err
	}
	for _, row := range recordRows {
		entries = append(entries, generatedRecordDocketEntry(row))
	}
	return records.Docket{RunID: runID, Entries: entries}, nil
}

func artifactDocketEntry(row map[string]any) records.Entry {
	placement := stringFrom(row, "placement")
	blobKey := stringFrom(row, "blob_key")
	repoPath := stringFrom(row, "repo_path")
	if placement == artifactcontracts.PlacementBlobExhaust && blobKey == "" && repoPath != "" {
		placement = artifactcontracts.PlacementGitPublication
	}
	contentType := stringFrom(row, "blob_content_type")
	if contentType == "" {
		contentType = docketContentType(repoPath)
	}
	return records.Entry{
		RunID:          stringFrom(row, "run_id"),
		ArtifactID:     stringFrom(row, "artifact_id"),
		JobID:          stringFrom(row, "job_id"),
		LogicalName:    stringFrom(row, "logical_name"),
		Kind:           stringFrom(row, "artifact_kind"),
		Placement:      placement,
		RetentionClass: docketRetentionClass(placement),
		ContentSHA256:  firstNonEmpty(stringFrom(row, "content_sha256"), stringFrom(row, "blob_sha256")),
		BlobKey:        blobKey,
		RepoPath:       repoPath,
		ContentType:    contentType,
		SizeBytes:      int64From(row, "size_bytes"),
		URI:            "striatum://artifact/" + stringFrom(row, "artifact_id"),
	}
}

func generatedRecordDocketEntry(row map[string]any) records.Entry {
	recordID := stringFrom(row, "record_id")
	contentType := stringFrom(row, "content_type")
	if contentType == "" {
		contentType = docketContentType(stringFrom(row, "source_path"))
	}
	return records.Entry{
		RunID:          stringFrom(row, "run_id"),
		RecordID:       recordID,
		JobID:          stringFrom(row, "job_id"),
		SourcePath:     stringFrom(row, "source_path"),
		Class:          stringFrom(row, "record_class"),
		Placement:      artifactcontracts.PlacementBlobExhaust,
		RetentionClass: firstNonEmpty(stringFrom(row, "retention_class"), "generated_record"),
		ContentSHA256:  firstNonEmpty(stringFrom(row, "content_sha256"), stringFrom(row, "blob_sha256")),
		BlobKey:        stringFrom(row, "blob_key"),
		ContentType:    contentType,
		SizeBytes:      int64From(row, "size_bytes"),
		URI:            "striatum://record/" + recordID,
	}
}

func docketRetentionClass(placement string) string {
	switch placement {
	case artifactcontracts.PlacementBlobExhaust:
		return "generated_exhaust"
	case artifactcontracts.PlacementGitPointerManifest:
		return "pointer_manifest"
	default:
		return "durable_provenance"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func int64From(m map[string]any, key string) int64 {
	switch value := m[key].(type) {
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	case json.Number:
		parsed, err := value.Int64()
		if err == nil {
			return parsed
		}
		return 0
	default:
		return 0
	}
}

func docketContentType(pathText string) string {
	lower := strings.ToLower(pathText)
	switch {
	case strings.HasSuffix(lower, ".md"):
		return "text/markdown; charset=utf-8"
	case strings.HasSuffix(lower, ".json"):
		return "application/json"
	case strings.HasSuffix(lower, ".txt"):
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
