package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/halbritt/striatum/go/pkg/blob"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// HandleCorpusMigrateHistoricalDogfoodFile is the per-file RPC used by
// the `striatum corpus migrate-historical-dogfoods` CLI subcommand to
// bulk-migrate `docs/dogfood/` into the per-repo S3 bucket. One call
// per file: the CLI walks the on-disk tree, base64-encodes each
// body, and POSTs through this handler. Centralizing the upload here
// keeps the CLI wrapper free of S3 client details; the same `*blob.Client`
// the live publish path uses is reused.
//
// Required params: repository_id, dogfood_id, rel_path, body_base64.
// Optional params: content_type, dry_run.
//
// Response shape:
//
//	{
//	  "status": "uploaded" | "skipped_already_present" | "would_upload",
//	  "blob_key": "dogfood-historical/<dogfood_id>/<rel_path>",
//	  "sha256": "...",
//	  "size_bytes": N
//	}
//
// Idempotency: when the remote object exists and its
// `X-Striatum-Sha256` user-metadata header matches the local body's
// sha256, status is `skipped_already_present`. With `dry_run: true`,
// the handler does the HEAD probe but skips the PUT and returns
// `would_upload` / `skipped_already_present` accordingly so the
// CLI's dry-run counts match what the live run will do.
func HandleCorpusMigrateHistoricalDogfoodFile(
	ctx context.Context,
	runner db.Runner,
	envelope rpc.Envelope,
) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	dogfoodID := stringParam(envelope, "dogfood_id")
	relPath := stringParam(envelope, "rel_path")
	bodyB64 := stringParam(envelope, "body_base64")
	if dogfoodID == "" || relPath == "" || bodyB64 == "" {
		return nil, rpc.NewError(
			"schema_invalid",
			"corpus.migrate_historical_dogfood_file requires dogfood_id, rel_path, body_base64",
			nil,
		)
	}
	if strings.Contains(dogfoodID, "/") || dogfoodID == "." || dogfoodID == ".." {
		return nil, rpc.NewError("schema_invalid", "dogfood_id must not contain path separators", nil)
	}
	if strings.HasPrefix(relPath, "/") || strings.Contains(relPath, "..") {
		return nil, rpc.NewError("schema_invalid", "rel_path must be a sanitized repo-relative path", nil)
	}
	if packageBlobClient == nil {
		return nil, rpc.NewError(
			"blob_disabled",
			"daemon is not configured for blob storage (STRIATUM_BLOB_ENDPOINT unset)",
			nil,
		)
	}
	body, err := base64.StdEncoding.DecodeString(bodyB64)
	if err != nil {
		return nil, rpc.NewError("schema_invalid", "body_base64 decode failed: "+err.Error(), nil)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])

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

	key := blob.HistoricalDogfoodKey(dogfoodID, relPath)

	// Idempotency probe: HEAD the remote and compare sha256 metadata.
	existing, exists, err := packageBlobClient.RemoteSha256(ctx, bucket, key)
	if err != nil {
		return nil, rpc.NewError("blob_head_failed", err.Error(), map[string]any{
			"bucket": bucket,
			"key":    key,
		})
	}
	if exists && existing == digest {
		return map[string]any{
			"status":     "skipped_already_present",
			"blob_key":   key,
			"sha256":     digest,
			"size_bytes": len(body),
		}, nil
	}

	dryRun := boolParam(envelope, "dry_run")
	if dryRun {
		return map[string]any{
			"status":     "would_upload",
			"blob_key":   key,
			"sha256":     digest,
			"size_bytes": len(body),
		}, nil
	}

	contentType := stringParam(envelope, "content_type")
	if contentType == "" {
		contentType = artifactContentType(relPath)
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
	return map[string]any{
		"status":     "uploaded",
		"blob_key":   key,
		"sha256":     digest,
		"size_bytes": len(body),
	}, nil
}
