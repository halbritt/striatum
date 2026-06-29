package reads

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

type doctorGeneratedRecordBlob struct {
	sha      string
	exists   bool
	getDelay time.Duration
	getErr   error
	active   atomic.Int64
	max      atomic.Int64
}

func (b *doctorGeneratedRecordBlob) BucketExists(context.Context, string) (bool, error) {
	return true, nil
}
func (b *doctorGeneratedRecordBlob) HeadObject(context.Context, string, string) (int64, string, error) {
	return 0, "text/markdown; charset=utf-8", nil
}
func (b *doctorGeneratedRecordBlob) ListByPrefix(context.Context, string, string) ([]blob.ListObjectEntry, error) {
	return nil, nil
}
func (b *doctorGeneratedRecordBlob) ListCommonPrefixes(context.Context, string, string, string) ([]string, error) {
	return nil, nil
}
func (b *doctorGeneratedRecordBlob) PutBytes(context.Context, string, string, []byte, string) (string, error) {
	return b.sha, nil
}
func (b *doctorGeneratedRecordBlob) Reachable(context.Context) error { return nil }
func (b *doctorGeneratedRecordBlob) RemoteSha256(context.Context, string, string) (string, bool, error) {
	return b.sha, b.exists, nil
}
func (b *doctorGeneratedRecordBlob) GetBytes(ctx context.Context, _, _, _ string) ([]byte, error) {
	active := b.active.Add(1)
	for {
		max := b.max.Load()
		if active <= max || b.max.CompareAndSwap(max, active) {
			break
		}
	}
	defer b.active.Add(-1)
	if b.getDelay > 0 {
		timer := time.NewTimer(b.getDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if b.getErr != nil {
		return nil, b.getErr
	}
	return []byte("ok"), nil
}

func TestRecordsMigrationVerifyReportsMissingBlob(t *testing.T) {
	previous := packageBlobClient
	packageBlobClient = recordsMigrationReadBlob{exists: false}
	t.Cleanup(func() { packageBlobClient = previous })

	row := map[string]any{
		"record_id":      "rec_1",
		"source_path":    "docs/audits/report.md",
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
				"path":          "docs/audits/report.md",
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

func TestRecordsMigrationVerifyAcceptsJSONNumberManifestSize(t *testing.T) {
	previous := packageBlobClient
	sha := strings.Repeat("a", 64)
	packageBlobClient = recordsMigrationReadBlob{body: []byte("hello"), sha: sha, exists: true}
	t.Cleanup(func() { packageBlobClient = previous })

	row := map[string]any{
		"record_id":      "rec_1",
		"source_path":    "docs/audits/report.md",
		"source_commit":  "commit_1",
		"record_class":   "audit_record",
		"content_sha256": sha,
		"blob_key":       "records/historical/commit/report.md",
		"blob_sha256":    sha,
		"content_type":   "text/markdown; charset=utf-8",
		"size_bytes":     int64(5),
	}
	result, err := HandleRecordsMigrationVerify(context.Background(), &recordsMigrationReadRunner{generatedRows: []map[string]any{row}}, rpc.Envelope{
		Params: map[string]any{
			"repository_id": "repo_1",
			"entries": []any{map[string]any{
				"path":          "docs/audits/report.md",
				"source_commit": "commit_1",
				"record_class":  "audit_record",
				"sha256":        sha,
				"size":          json.Number("5"),
			}},
		},
	})
	if err != nil {
		t.Fatalf("HandleRecordsMigrationVerify: %v", err)
	}
	if result["reconstructable"] != true {
		t.Fatalf("result = %#v, want reconstructable", result)
	}
	if problems := result["problems"].([]map[string]any); len(problems) != 0 {
		t.Fatalf("problems = %#v, want none", problems)
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
			"source_path":    "docs/audits/report.md",
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

func TestDoctorGeneratedRecordIntegrityReportsBodyVerifyFailure(t *testing.T) {
	previous := packageBlobClient
	sha := strings.Repeat("a", 64)
	packageBlobClient = &doctorGeneratedRecordBlob{sha: sha, exists: true, getErr: errors.New("sha mismatch")}
	t.Cleanup(func() { packageBlobClient = previous })

	result, problems, records := doctorGeneratedRecordIntegrity(
		context.Background(),
		&doctorGeneratedRecordRunner{rows: []map[string]any{{
			"record_id":      "rec_bad_body",
			"source_path":    "docs/audits/report.md",
			"content_sha256": sha,
			"blob_key":       "records/historical/report.md",
			"blob_sha256":    sha,
		}}},
		"repo_1",
		healthyBlobBlock(),
	)
	if result["checked"] != true || len(problems) != 1 || len(records) != 1 {
		t.Fatalf("result=%#v problems=%#v records=%#v", result, problems, records)
	}
	if records[0]["check"] != generatedRecordBlobBodyVerifyFailed {
		t.Fatalf("record = %#v, want body verify failure", records[0])
	}
}

func TestDoctorGeneratedRecordIntegrityVerifiesBlobBodiesConcurrently(t *testing.T) {
	previous := packageBlobClient
	sha := strings.Repeat("a", 64)
	fakeBlob := &doctorGeneratedRecordBlob{sha: sha, exists: true, getDelay: 20 * time.Millisecond}
	packageBlobClient = fakeBlob
	t.Cleanup(func() { packageBlobClient = previous })

	rows := []map[string]any{}
	for i := 0; i < 8; i++ {
		rows = append(rows, map[string]any{
			"record_id":      "rec_" + string(rune('a'+i)),
			"source_path":    "docs/audits/report.md",
			"content_sha256": sha,
			"blob_key":       "records/historical/report-" + string(rune('a'+i)) + ".md",
			"blob_sha256":    sha,
		})
	}
	result, problems, records := doctorGeneratedRecordIntegrity(
		context.Background(),
		&doctorGeneratedRecordRunner{rows: rows},
		"repo_1",
		healthyBlobBlock(),
	)
	if len(problems) != 0 || len(records) != 0 {
		t.Fatalf("problems=%#v records=%#v", problems, records)
	}
	if result["checked_count"] != len(rows) {
		t.Fatalf("checked_count = %#v, want %d", result["checked_count"], len(rows))
	}
	if fakeBlob.max.Load() < 2 {
		t.Fatalf("max concurrent GetBytes calls = %d, want at least 2", fakeBlob.max.Load())
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
