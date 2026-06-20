package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// fakeS3 is a deliberately tiny, in-memory, path-style S3 endpoint that
// implements only the verbs PutBytes exercises: PUT (store), HEAD
// (StatObject), GET (readback). It exists so the FMA-004 (#454)
// post-upload content-readback verification can be tested end-to-end
// through the real minio-go client without a live Garage/S3 backend.
//
// corruptOnGet, when set, returns a body on GET that has the SAME byte
// length as the stored object but DIFFERENT content — exactly the
// truncated/corrupt-but-size-matching case a size-only check cannot
// detect and the content readback must.
type fakeS3 struct {
	mu           sync.Mutex
	objects      map[string][]byte
	corruptOnGet bool
}

func newFakeS3() *fakeS3 { return &fakeS3{objects: map[string][]byte{}} }

// decodeAWSChunked strips AWS v4 streaming-signature chunk framing
// (`<hexsize>;chunk-signature=...\r\n<data>\r\n`, terminated by a
// zero-length chunk) and returns the concatenated payload. Enough of the
// format for minio-go single-object PUTs over plain HTTP.
func decodeAWSChunked(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	rest := raw
	for {
		nl := indexCRLF(rest)
		if nl < 0 {
			break
		}
		header := string(rest[:nl])
		rest = rest[nl+2:]
		sizeField := header
		if semi := strings.IndexByte(header, ';'); semi >= 0 {
			sizeField = header[:semi]
		}
		size, err := strconv.ParseInt(strings.TrimSpace(sizeField), 16, 64)
		if err != nil || size == 0 {
			break
		}
		if int64(len(rest)) < size {
			out = append(out, rest...)
			break
		}
		out = append(out, rest[:size]...)
		rest = rest[size:]
		// Trailing CRLF after the chunk data.
		if len(rest) >= 2 && rest[0] == '\r' && rest[1] == '\n' {
			rest = rest[2:]
		}
	}
	return out
}

func indexCRLF(b []byte) int {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == '\r' && b[i+1] == '\n' {
			return i
		}
	}
	return -1
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// minio-go parses Last-Modified on HEAD/GET responses and rejects a
	// missing/invalid value, so every object response carries a valid
	// RFC1123 GMT timestamp.
	lastMod := time.Unix(0, 0).UTC().Format(http.TimeFormat)
	// Path-style addressing: /<bucket>/<key...>. The leading element is
	// the bucket; the remainder is the object key.
	trimmed := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	key := ""
	if len(parts) == 2 {
		key = parts[1]
	}

	switch r.Method {
	case http.MethodPut:
		raw := make([]byte, 0, r.ContentLength)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			if n > 0 {
				raw = append(raw, buf[:n]...)
			}
			if err != nil {
				break
			}
		}
		body := raw
		// minio-go uses AWS v4 streaming signatures over plain HTTP, which
		// wraps the payload in aws-chunked framing. Strip it so the stored
		// object is the true content.
		if strings.EqualFold(r.Header.Get("x-amz-content-sha256"), "STREAMING-AWS4-HMAC-SHA256-PAYLOAD") {
			body = decodeAWSChunked(raw)
		}
		f.mu.Lock()
		f.objects[key] = body
		f.mu.Unlock()
		sum := sha256.Sum256(body)
		w.Header().Set("ETag", "\""+hex.EncodeToString(sum[:])+"\"")
		w.WriteHeader(http.StatusOK)
	case http.MethodHead:
		f.mu.Lock()
		obj, ok := f.objects[key]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(obj)))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Last-Modified", lastMod)
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		f.mu.Lock()
		obj, ok := f.objects[key]
		corrupt := f.corruptOnGet
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		out := obj
		if corrupt {
			// Same length, different bytes — defeats the size check,
			// must be caught by the content readback.
			out = make([]byte, len(obj))
			for i := range out {
				out[i] = obj[i] ^ 0xFF
			}
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(out)))
		w.Header().Set("Last-Modified", lastMod)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// newClientForFakeS3 builds a blob.Client whose minio-go client talks to
// the given fake endpoint using path-style addressing (Garage's mode).
func newClientForFakeS3(t *testing.T, endpoint string) *Client {
	t.Helper()
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	mc, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4("ak", "sk", ""),
		Secure:       false,
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatalf("minio.New: %v", err)
	}
	return &Client{cfg: &Config{Endpoint: u.Host, Region: "us-east-1", PathStyle: true}, mc: mc}
}

// TestPutBytesHappyPathThroughRealClient confirms a correct, uncorrupted
// upload passes the post-upload content readback and returns the body's
// sha256.
func TestPutBytesHappyPathThroughRealClient(t *testing.T) {
	fake := newFakeS3()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	c := newClientForFakeS3(t, srv.URL)
	body := []byte("the quick brown fox jumps over the lazy dog\n")
	want := sha256.Sum256(body)
	wantHex := hex.EncodeToString(want[:])

	got, err := c.PutBytes(context.Background(), "repo-bucket", "runs/r1/j1/art.md", body, "text/markdown")
	if err != nil {
		t.Fatalf("PutBytes happy path: unexpected error: %v", err)
	}
	if got != wantHex {
		t.Fatalf("PutBytes returned sha %s, want %s", got, wantHex)
	}
}

// TestPutBytesRejectsCorruptReadback is the core FMA-004 regression: the
// store returns a body of the SAME length but DIFFERENT content on
// readback. The size check passes; the content readback must catch the
// corruption and PutBytes must fail with ErrContentReadbackMismatch so
// artifact publish rejects the corrupt blob at publish time.
func TestPutBytesRejectsCorruptReadback(t *testing.T) {
	fake := newFakeS3()
	fake.corruptOnGet = true
	srv := httptest.NewServer(fake)
	defer srv.Close()

	c := newClientForFakeS3(t, srv.URL)
	body := []byte("artifact body that the store will silently corrupt")

	_, err := c.PutBytes(context.Background(), "repo-bucket", "runs/r1/j1/art.md", body, "text/markdown")
	if err == nil {
		t.Fatal("PutBytes accepted a corrupt (same-length, different-content) readback; expected rejection")
	}
	if !errors.Is(err, ErrContentReadbackMismatch) {
		t.Fatalf("PutBytes returned %v, want ErrContentReadbackMismatch", err)
	}
}

// TestPutBytesReadbackVerificationViaStub exercises the readback-compare
// logic directly through the injectable hook, independent of the HTTP
// path: a stub that returns truncated content must trigger
// ErrContentReadbackMismatch, and a faithful stub must pass.
func TestPutBytesReadbackVerificationViaStub(t *testing.T) {
	body := []byte("hello content readback")
	sum := sha256.Sum256(body)
	wantHex := hex.EncodeToString(sum[:])

	t.Run("faithful readback passes", func(t *testing.T) {
		c := &Client{readObjectBytes: func(_ context.Context, _, _ string) ([]byte, error) {
			return append([]byte(nil), body...), nil
		}}
		got, err := c.verifyReadbackForTest(context.Background(), "b", "k", body, wantHex)
		if err != nil {
			t.Fatalf("faithful readback: unexpected error: %v", err)
		}
		if got != wantHex {
			t.Fatalf("got %s, want %s", got, wantHex)
		}
	})

	t.Run("truncated readback rejected", func(t *testing.T) {
		c := &Client{readObjectBytes: func(_ context.Context, _, _ string) ([]byte, error) {
			return body[:len(body)-3], nil
		}}
		_, err := c.verifyReadbackForTest(context.Background(), "b", "k", body, wantHex)
		if !errors.Is(err, ErrContentReadbackMismatch) {
			t.Fatalf("truncated readback: got %v, want ErrContentReadbackMismatch", err)
		}
	})
}

// verifyReadbackForTest reproduces PutBytes's post-upload readback +
// compare step against the injectable hook, isolated from the
// PutObject/StatObject HTTP calls so the verification logic is unit
// testable without any S3 endpoint at all.
func (c *Client) verifyReadbackForTest(ctx context.Context, bucket, key string, body []byte, expectedHex string) (string, error) {
	stored, err := c.readBack(ctx, bucket, key)
	if err != nil {
		return "", err
	}
	storedSum := sha256.Sum256(stored)
	if storedHex := hex.EncodeToString(storedSum[:]); storedHex != expectedHex {
		return "", ErrContentReadbackMismatch
	}
	return expectedHex, nil
}
