package reads

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/blob"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

type recordsMigrationReadRunner struct {
	doctorFakeRunner
	generatedRows []map[string]any
}

func (r recordsMigrationReadRunner) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "FROM striatumd.repositories"):
		return dashboardAllRowsFromMaps([]map[string]any{{"blob_bucket": "bucket-1", "repo_root": "/repo"}}), nil
	case strings.Contains(sql, "FROM striatumd.generated_records"):
		return dashboardAllRowsFromMaps(r.generatedRows), nil
	default:
		return dashboardAllRowsFromMaps(nil), nil
	}
}

type recordsMigrationReadBlob struct {
	body   []byte
	sha    string
	exists bool
}

func (b recordsMigrationReadBlob) BucketExists(context.Context, string) (bool, error) {
	return true, nil
}
func (b recordsMigrationReadBlob) HeadObject(context.Context, string, string) (int64, string, error) {
	return int64(len(b.body)), "text/markdown; charset=utf-8", nil
}
func (b recordsMigrationReadBlob) ListByPrefix(context.Context, string, string) ([]blob.ListObjectEntry, error) {
	return nil, nil
}
func (b recordsMigrationReadBlob) ListCommonPrefixes(context.Context, string, string, string) ([]string, error) {
	return nil, nil
}
func (b recordsMigrationReadBlob) PutBytes(context.Context, string, string, []byte, string) (string, error) {
	return b.sha, nil
}
func (b recordsMigrationReadBlob) Reachable(context.Context) error { return nil }
func (b recordsMigrationReadBlob) RemoteSha256(context.Context, string, string) (string, bool, error) {
	return b.sha, b.exists, nil
}
func (b recordsMigrationReadBlob) GetBytes(context.Context, string, string, string) ([]byte, error) {
	if !b.exists {
		return nil, errors.New("missing")
	}
	return b.body, nil
}

func TestRecordsMigrationVerifyReportsMissingBlob(t *testing.T) {
	previous := packageBlobClient
	packageBlobClient = recordsMigrationReadBlob{exists: false}
	t.Cleanup(func() { packageBlobClient = previous })

	row := map[string]any{
		"record_id":      "rec_1",
		"source_path":    "docs/records/audits/report.md",
		"source_commit":  "commit_1",
		"record_class":   "audit_record",
		"content_sha256": strings.Repeat("a", 64),
		"blob_key":       "records/historical/commit/report.md",
		"blob_sha256":    strings.Repeat("a", 64),
		"content_type":   "text/markdown; charset=utf-8",
		"size_bytes":     int64(7),
	}
	result, err := HandleRecordsMigrationVerify(context.Background(), &recordsMigrationReadRunner{generatedRows: []map[string]any{row}}, rpc.Envelope{
		Params: map[string]any{
			"repository_id": "repo_1",
			"entries": []any{map[string]any{
				"path":          "docs/records/audits/report.md",
				"source_commit": "commit_1",
				"record_class":  "audit_record",
				"sha256":        strings.Repeat("a", 64),
				"size":          int64(7),
			}},
		},
	})
	if err != nil {
		t.Fatalf("HandleRecordsMigrationVerify: %v", err)
	}
	problems := result["problems"].([]map[string]any)
	if len(problems) == 0 || problems[0]["code"] != "generated_record_blob_missing" {
		t.Fatalf("problems = %#v, want generated_record_blob_missing", problems)
	}
	if result["reconstructable"] != false || result["deletion_allowed"] != false {
		t.Fatalf("result = %#v", result)
	}
}

func TestRecordsMigrationVerifyRejectsEscapingManifestPath(t *testing.T) {
	_, err := HandleRecordsMigrationVerify(context.Background(), &recordsMigrationReadRunner{}, rpc.Envelope{
		Params: map[string]any{
			"repository_id": "repo_1",
			"entries": []any{map[string]any{
				"path":   "../outside.md",
				"sha256": strings.Repeat("a", 64),
				"size":   int64(1),
			}},
		},
	})
	if err == nil {
		t.Fatal("HandleRecordsMigrationVerify accepted an escaping manifest path")
	}
	if rpcErr, ok := err.(*rpc.Error); !ok || rpcErr.Code != "path_outside_scope" {
		t.Fatalf("err = %#v, want path_outside_scope", err)
	}
}

func TestDoctorGeneratedRecordIntegrityReportsMetadataMissing(t *testing.T) {
	result, problems, records := doctorGeneratedRecordIntegrity(
		context.Background(),
		&doctorGeneratedRecordRunner{rows: []map[string]any{{
			"record_id":      "rec_missing_meta",
			"source_path":    "docs/records/audits/report.md",
			"content_sha256": strings.Repeat("a", 64),
		}}},
		"repo_1",
		healthyBlobBlock(),
	)
	if result["checked"] != true || len(problems) != 1 || len(records) != 1 {
		t.Fatalf("result=%#v problems=%#v records=%#v", result, problems, records)
	}
	if records[0]["check"] != generatedRecordBlobMetadataMissing {
		t.Fatalf("record = %#v", records[0])
	}
}

type doctorGeneratedRecordRunner struct {
	doctorFakeRunner
	rows []map[string]any
}

func (r doctorGeneratedRecordRunner) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "to_regclass"):
		return dashboardAllRowsFromMaps([]map[string]any{{"table_name": "striatumd.generated_records"}}), nil
	case strings.Contains(sql, "FROM striatumd.repositories"):
		return dashboardAllRowsFromMaps([]map[string]any{{"blob_bucket": "bucket-1", "repo_root": "/repo"}}), nil
	case strings.Contains(sql, "GROUP BY"):
		return dashboardAllRowsFromMaps(nil), nil
	case strings.Contains(sql, "FROM striatumd.generated_records"):
		return dashboardAllRowsFromMaps(r.rows), nil
	default:
		return dashboardAllRowsFromMaps([]map[string]any{{"c": int64(0)}}), nil
	}
}
