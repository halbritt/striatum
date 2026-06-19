package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client is the daemon's gateway to S3-compatible blob storage.
//
// Concurrent-safe: the underlying minio-go client carries its own
// connection pool; Client embeds it without extra locking. Methods
// accept a context.Context so callers can deadline reads/writes
// per-RPC.
type Client struct {
	cfg *Config
	mc  *minio.Client

	// readObjectBytes fetches the full body of bucket/key. It is a field
	// (not a direct c.mc.GetObject call) so the PutBytes post-upload
	// content-readback verification can be exercised by unit tests with a
	// stubbed store — including a deliberately truncated/corrupt readback
	// — without a live S3/Garage endpoint. nil means "use the real minio
	// GetObject path" (c.getObjectBytes).
	readObjectBytes func(ctx context.Context, bucket, key string) ([]byte, error)
}

// New returns a new Client backed by minio-go. Returns an error if the
// configuration is invalid or the underlying client cannot be
// constructed; does not attempt any S3 round-trip — call Reachable for
// that.
func New(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("blob.New: nil config")
	}
	if cfg.Endpoint == "" {
		return nil, errors.New("blob.New: empty endpoint")
	}
	opts := &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       cfg.UseSSL,
		Region:       cfg.Region,
		BucketLookup: minio.BucketLookupAuto,
	}
	if cfg.PathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}
	mc, err := minio.New(cfg.Endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("blob.New: %w", err)
	}
	return &Client{cfg: cfg, mc: mc}, nil
}

// Reachable returns nil if the S3 endpoint accepts the configured
// credentials and the bucket-list call succeeds. Errors are
// authentication, network, or endpoint mismatches. Does not require
// any specific bucket to exist.
func (c *Client) Reachable(ctx context.Context) error {
	_, err := c.mc.ListBuckets(ctx)
	return err
}

// BucketExists reports whether the named bucket exists on the
// configured endpoint. Distinct from Reachable: a configured endpoint
// can be reachable without the per-repo bucket having been provisioned
// yet.
func (c *Client) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return c.mc.BucketExists(ctx, bucket)
}

// CreateBucket creates the named bucket on the configured endpoint
// using the configured region. The bucket is created with default
// (private) ACL; versioning is not enabled. Returns nil if the bucket
// already exists.
//
// CreateBucket is intended for adopt-time provisioning behind an
// explicit --apply-blob-creation flag, mirroring
// `daemon doctor --apply-migrations`. Production publish paths do not
// create buckets implicitly.
func (c *Client) CreateBucket(ctx context.Context, bucket string) error {
	if err := ValidateBucketName(bucket); err != nil {
		return fmt.Errorf("blob.CreateBucket: %w", err)
	}
	exists, err := c.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("blob.CreateBucket: head: %w", err)
	}
	if exists {
		return nil
	}
	return c.mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: c.cfg.Region})
}

// ErrContentReadbackMismatch is returned by PutBytes when the body read
// back from the store after the upload does not hash to the sha256 of
// the body that was uploaded. It signals a corrupt or truncated PUT that
// happened to satisfy the size check — the blob on the store is not
// byte-identical to its recorded content_sha256. Callers (artifact
// publish) must treat this as a hard publish failure so the corruption is
// rejected at publish time rather than caught late at the run-completion
// reconstructability gate (FMA-004, issue #454).
var ErrContentReadbackMismatch = errors.New("blob.PutBytes: stored content sha256 does not match uploaded body")

// PutBytes uploads body to bucket at key with the given content type.
// The sha256 of body is computed before upload, then verified against
// the post-upload object: first the object SIZE via a HEAD round-trip,
// then — and this is the integrity-critical step — the actual STORED
// CONTENT is read back and re-hashed and compared to the recorded
// sha256. A size-only check cannot catch a corrupt-but-same-length PUT;
// the content readback can. On any mismatch PutBytes fails loudly
// (ErrContentReadbackMismatch) so a corrupt blob is rejected at publish
// time, not late at the run-completion reconstructability gate. Returns
// the sha256 hex digest on success.
//
// PutBytes is the canonical publish path: callers (the daemon's
// artifact.publish handler) pass the already-validated artifact body
// in; PutBytes does not validate front-matter, byline, or any other
// artifact-shape contract — that runs upstream.
func (c *Client) PutBytes(ctx context.Context, bucket, key string, body []byte, contentType string) (string, error) {
	if bucket == "" || key == "" {
		return "", fmt.Errorf("blob.PutBytes: empty bucket=%q or key=%q", bucket, key)
	}
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])
	reader := bytes.NewReader(body)
	_, err := c.mc.PutObject(ctx, bucket, key, reader, int64(len(body)), minio.PutObjectOptions{
		ContentType:  contentType,
		UserMetadata: map[string]string{"X-Striatum-Sha256": hexSum},
	})
	if err != nil {
		return "", fmt.Errorf("blob.PutBytes: put: %w", err)
	}
	stat, err := c.mc.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return "", fmt.Errorf("blob.PutBytes: stat: %w", err)
	}
	if stat.Size != int64(len(body)) {
		return "", fmt.Errorf("blob.PutBytes: size mismatch: put=%d stat=%d", len(body), stat.Size)
	}
	// Content readback verification (FMA-004 / #454). Size equality is
	// necessary but not sufficient: a truncated/corrupt PUT that lands at
	// the same byte length stores a body whose readback hash differs from
	// the recorded sha. Garage does not return a trustworthy strong
	// content hash on the HEAD/PUT (the ETag is not a portable sha256, and
	// is md5-shaped only for single-part PUTs), so the safe default is to
	// read the stored object back and re-hash it. This is bounded: one
	// extra GET per put, only on the publish path.
	stored, err := c.readBack(ctx, bucket, key)
	if err != nil {
		return "", fmt.Errorf("blob.PutBytes: readback: %w", err)
	}
	storedSum := sha256.Sum256(stored)
	if storedHex := hex.EncodeToString(storedSum[:]); storedHex != hexSum {
		return "", fmt.Errorf("%w: bucket=%s key=%s expected=%s got=%s storedLen=%d", ErrContentReadbackMismatch, bucket, key, hexSum, storedHex, len(stored))
	}
	return hexSum, nil
}

// readBack reads the full stored body of bucket/key for post-upload
// content verification. It dispatches to the injectable readObjectBytes
// hook when set (unit tests stub a corrupt/truncated store there);
// otherwise it goes to the real minio GetObject path.
func (c *Client) readBack(ctx context.Context, bucket, key string) ([]byte, error) {
	if c.readObjectBytes != nil {
		return c.readObjectBytes(ctx, bucket, key)
	}
	return c.getObjectBytes(ctx, bucket, key)
}

// getObjectBytes fetches the full body of bucket/key from the live
// S3/Garage endpoint. Unlike GetBytes it carries no expected-sha
// contract; it is the raw readback used internally by PutBytes to verify
// the just-uploaded object's stored content.
func (c *Client) getObjectBytes(ctx context.Context, bucket, key string) ([]byte, error) {
	obj, err := c.mc.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	defer func() { _ = obj.Close() }()
	body, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return body, nil
}

// GetBytes fetches the object at bucket/key, verifies its sha256
// against the expected value, and returns the body. A non-empty
// expectedSha256 is required: a blob fetch without an integrity anchor
// is not part of the artifact contract.
//
// Errors are returned for not-found, network failures, sha256
// mismatches, and read failures. Callers may log mismatches as audit
// events; this layer does not.
func (c *Client) GetBytes(ctx context.Context, bucket, key, expectedSha256 string) ([]byte, error) {
	if bucket == "" || key == "" {
		return nil, fmt.Errorf("blob.GetBytes: empty bucket=%q or key=%q", bucket, key)
	}
	if expectedSha256 == "" {
		return nil, fmt.Errorf("blob.GetBytes: empty expectedSha256 for %s/%s", bucket, key)
	}
	obj, err := c.mc.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("blob.GetBytes: get: %w", err)
	}
	defer func() { _ = obj.Close() }()
	body, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("blob.GetBytes: read: %w", err)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != expectedSha256 {
		return nil, fmt.Errorf("blob.GetBytes: sha256 mismatch: expected=%s got=%s", expectedSha256, got)
	}
	return body, nil
}

// HeadObject returns the size and content type of bucket/key without
// fetching the body. Used by the daemon doctor round-trip check and
// the artifact listing flows.
func (c *Client) HeadObject(ctx context.Context, bucket, key string) (size int64, contentType string, err error) {
	stat, err := c.mc.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, "", err
	}
	return stat.Size, stat.ContentType, nil
}

// ListObjectEntry is a minimal listing record. The full minio.ObjectInfo
// carries fields we do not need at this layer; reducing the surface
// keeps the daemon RPC contract narrow.
type ListObjectEntry struct {
	Key          string
	Size         int64
	LastModified string // ISO-8601, set only when minio reports a timestamp.
}

// ListByPrefix returns every object under the given key prefix in
// alphabetical (server-default) order. Recursive: no delimiter is
// passed, so subkeys appear flat.
//
// Used by the historical-dogfood browse surface to enumerate files
// under `dogfood-historical/<dogfood_id>/`. For larger buckets where
// pagination matters, callers should pass a more specific prefix; the
// current historical migration produces ~1.3k keys total which list
// in a single round-trip.
func (c *Client) ListByPrefix(ctx context.Context, bucket, prefix string) ([]ListObjectEntry, error) {
	opts := minio.ListObjectsOptions{Prefix: prefix, Recursive: true}
	out := make([]ListObjectEntry, 0, 32)
	for object := range c.mc.ListObjects(ctx, bucket, opts) {
		if object.Err != nil {
			return nil, object.Err
		}
		entry := ListObjectEntry{Key: object.Key, Size: object.Size}
		if !object.LastModified.IsZero() {
			entry.LastModified = object.LastModified.UTC().Format("2006-01-02T15:04:05Z")
		}
		out = append(out, entry)
	}
	return out, nil
}

// ListCommonPrefixes enumerates the immediate-child "directories"
// under the given prefix, using the supplied delimiter (typically "/").
// Used to enumerate `dogfood-historical/<dogfood_id>/` entries
// without listing every file under each.
func (c *Client) ListCommonPrefixes(ctx context.Context, bucket, prefix, delimiter string) ([]string, error) {
	opts := minio.ListObjectsOptions{Prefix: prefix, Recursive: false}
	prefixes := make([]string, 0, 32)
	for object := range c.mc.ListObjects(ctx, bucket, opts) {
		if object.Err != nil {
			return nil, object.Err
		}
		// minio-go reports "directory" entries with a key ending in
		// the delimiter and a zero size. Plain object keys also come
		// through this iterator; filter to the directory shape.
		if delimiter != "" && strings.HasSuffix(object.Key, delimiter) {
			prefixes = append(prefixes, object.Key)
		}
	}
	return prefixes, nil
}

// RemoteSha256 reads the X-Striatum-Sha256 user metadata header from
// the named object. Used by the historical-migration flow to
// distinguish "already uploaded with matching content" from "needs
// upload". Returns:
//
//   - (sha256, true, nil) when the object exists and the metadata
//     header is present.
//   - ("", true, nil) when the object exists but the metadata header
//     is missing (object was uploaded by a non-striatum path).
//   - ("", false, nil) when the object does not exist.
//   - ("", false, error) on any other failure (network, auth, etc.).
func (c *Client) RemoteSha256(ctx context.Context, bucket, key string) (string, bool, error) {
	stat, err := c.mc.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		var s3err minio.ErrorResponse
		if errors.As(err, &s3err) && (s3err.Code == "NoSuchKey" || s3err.Code == "NoSuchBucket") {
			return "", false, nil
		}
		return "", false, err
	}
	// minio-go normalizes user metadata under stat.UserMetadata with
	// the leading x-amz-meta- stripped. Values are case-preserved as
	// the server returns them; case-insensitive lookup avoids surprises.
	for k, v := range stat.UserMetadata {
		if strings.EqualFold(k, "X-Striatum-Sha256") {
			return v, true, nil
		}
	}
	return "", true, nil
}
