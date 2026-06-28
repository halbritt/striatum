package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

type recordsMigrationRunner struct {
	execSQL  string
	execArgs []any
}

func (r *recordsMigrationRunner) Exec(_ context.Context, sql string, args ...any) error {
	r.execSQL = sql
	r.execArgs = append([]any(nil), args...)
	return nil
}

func (r *recordsMigrationRunner) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "FROM striatumd.repositories") {
		return runPrepareRowsFromMaps([]map[string]any{{"blob_bucket": "bucket-1"}}), nil
	}
	return runPrepareRowsFromMaps(nil), nil
}

func (r *recordsMigrationRunner) QueryRow(context.Context, string, ...any) db.Row {
	return fakeRow{err: pgx.ErrNoRows}
}

func (r *recordsMigrationRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", nil
}

func (r *recordsMigrationRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, nil
}

type recordsMigrationBlob struct {
	putKey string
	putSHA string
}

func (b *recordsMigrationBlob) GetBytes(context.Context, string, string, string) ([]byte, error) {
	return nil, nil
}

func (b *recordsMigrationBlob) RemoteSha256(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}

func (b *recordsMigrationBlob) PutBytes(_ context.Context, _ string, key string, body []byte, _ string) (string, error) {
	sum := sha256.Sum256(body)
	b.putKey = key
	b.putSHA = hex.EncodeToString(sum[:])
	return b.putSHA, nil
}

func TestHandleRecordsMigrationImportUploadsAndIndexesGeneratedRecord(t *testing.T) {
	previous := packageBlobClient
	blob := &recordsMigrationBlob{}
	packageBlobClient = blob
	t.Cleanup(func() { packageBlobClient = previous })

	body := []byte("historical record\n")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	runner := &recordsMigrationRunner{}
	result, err := HandleRecordsMigrationImport(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{
			"repository_id":   "repo_1",
			"source_path":     "docs/records/audits/report.md",
			"source_commit":   "abcdef1234567890",
			"record_class":    "audit_record",
			"content_sha256":  digest,
			"body_base64":     base64.StdEncoding.EncodeToString(body),
			"import_batch_id": "batch_1",
		},
	})
	if err != nil {
		t.Fatalf("HandleRecordsMigrationImport: %v", err)
	}
	if result["status"] != "uploaded" || result["deletion_allowed"] != false {
		t.Fatalf("result = %#v", result)
	}
	if blob.putKey == "" || blob.putSHA != digest {
		t.Fatalf("blob upload = key %q sha %q, want sha %s", blob.putKey, blob.putSHA, digest)
	}
	if !strings.Contains(runner.execSQL, "INSERT INTO striatumd.generated_records") || !strings.Contains(runner.execSQL, "ON CONFLICT") {
		t.Fatalf("exec SQL did not upsert generated_records: %s", runner.execSQL)
	}
	if len(runner.execArgs) < 16 || runner.execArgs[2] != "docs/records/audits/report.md" || runner.execArgs[8] != digest || runner.execArgs[15] != "batch_1" {
		t.Fatalf("exec args = %#v", runner.execArgs)
	}
}

func TestHandleRecordsMigrationImportAllowsEmptyBody(t *testing.T) {
	previous := packageBlobClient
	blob := &recordsMigrationBlob{}
	packageBlobClient = blob
	t.Cleanup(func() { packageBlobClient = previous })

	sum := sha256.Sum256(nil)
	digest := hex.EncodeToString(sum[:])
	runner := &recordsMigrationRunner{}
	result, err := HandleRecordsMigrationImport(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{
			"repository_id":  "repo_1",
			"source_path":    "docs/records/audits/empty.md",
			"record_class":   "audit_record",
			"content_sha256": digest,
			"body_base64":    "",
		},
	})
	if err != nil {
		t.Fatalf("HandleRecordsMigrationImport: %v", err)
	}
	if result["size_bytes"] != 0 || blob.putSHA != digest {
		t.Fatalf("result=%#v blob_sha=%s want empty-body digest %s", result, blob.putSHA, digest)
	}
}
