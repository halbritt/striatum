package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/halbritt/striatum/go/pkg/blob"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

const latestRepoLocalSchemaVersion = 16

type repoRegistration struct {
	RepositoryID  string
	RepoRoot      string
	RepoIdentity  string
	StateDBPath   string
	SchemaVersion int
	State         string
	BlobBucket    string
	BlobCreatedAt *time.Time
}

func (s Service) RepoInit(ctx context.Context, envelope rpc.Envelope) (map[string]any, error) {
	if s.Runner == nil {
		return nil, rpc.NewError("daemon_db_missing", "repo.init requires daemon PostgreSQL", nil)
	}
	repo, err := s.resolveRepoForInit(ctx, envelope.Params)
	if err != nil {
		return nil, err
	}
	stateDir, err := initOperationalScratch(repo)
	if err != nil {
		return nil, err
	}
	// #537 / #539: provision the committee POSIX ACLs (lane-writable repo tree +
	// inheritable defaults for both the lane and the daemon owner) so a
	// clone/worktree-registered repo supports `review_only_artifact` lanes and so
	// lane-written committee provenance stays operator-manageable without sudo.
	// Best-effort and idempotent: applies on both fresh registration and
	// re-adopt, no-op for owner-run lanes / missing lane user / no setfacl. A
	// failure is surfaced in the result, never fatal to registration.
	aclProvisioned, aclErr := ProvisionCommitteeACLsResult(repo)
	identity, err := repoIdentity(repo)
	if err != nil {
		return nil, err
	}
	existing, err := s.findInitRegistration(ctx, "repo_identity", identity)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Re-adopt of an already-registered repo. Backfill the blob
		// bucket if blob storage was configured after the original
		// adopt; otherwise return the existing row untouched.
		if s.BlobClient != nil && existing.BlobBucket == "" {
			if err := s.provisionAndRecord(ctx, existing, envelope.Params); err != nil {
				return nil, err
			}
		}
		return WithCommitteeACLResult(repoInitResult(*existing, true), aclProvisioned, aclErr), nil
	}
	existing, err = s.findInitRegistration(ctx, "repo_root", repo)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, rpc.NewError("path_conflict", "active repository path is occupied by a different repo identity", nil)
	}

	repositoryID := stringParam(envelope.Params, "repository_id")
	if repositoryID == "" {
		repositoryID = "repo_" + randomHex(16)
	}
	displayName := stringParam(envelope.Params, "display_name")
	if displayName == "" {
		displayName = filepath.Base(repo)
	}

	// RFC 0072: provision the blob bucket BEFORE the INSERT so a refusal
	// (repo_blob_conflict, blob_apply_required) does not leave a
	// half-registered row behind. With no blob configured, we skip
	// silently and record a NULL bucket on the row.
	var blobBucket string
	var blobCreatedAt *time.Time
	if s.BlobClient != nil {
		bucket, createdAt, err := s.provisionBucket(ctx, repositoryID, envelope.Params)
		if err != nil {
			return nil, err
		}
		blobBucket = bucket
		blobCreatedAt = createdAt
	}

	var blobBucketArg any
	if blobBucket != "" {
		blobBucketArg = blobBucket
	}
	if err := s.Runner.Exec(ctx, `
		INSERT INTO striatumd.repositories(repository_id, repo_identity, repo_root,
		  state_db_path, display_name, registered_at, removed_at, last_seen_at,
		  last_schema_version, state, settings_json, blob_bucket, blob_created_at)
		VALUES ($1, $2, $3, $4, $5, now(), NULL, now(), $6, 'active', '{}'::jsonb, $7, $8)`,
		repositoryID,
		identity,
		repo,
		stateDir,
		displayName,
		latestRepoLocalSchemaVersion,
		blobBucketArg,
		nullableTime(blobCreatedAt),
	); err != nil {
		return nil, err
	}
	return WithCommitteeACLResult(repoInitResult(repoRegistration{
		RepositoryID:  repositoryID,
		RepoRoot:      repo,
		RepoIdentity:  identity,
		StateDBPath:   stateDir,
		SchemaVersion: latestRepoLocalSchemaVersion,
		State:         "active",
		BlobBucket:    blobBucket,
		BlobCreatedAt: blobCreatedAt,
	}, false), aclProvisioned, aclErr), nil
}

// provisionBucket runs the RFC 0072 adopt-time blob provisioning and
// returns the resulting bucket name and creation timestamp. Errors are
// already RPC-shaped (`repo_blob_conflict`, `blob_apply_required`,
// `blob_provision_failed`) and can be returned directly to the client.
func (s Service) provisionBucket(ctx context.Context, repositoryID string, params map[string]any) (string, *time.Time, error) {
	bucket := stringParam(params, "blob_bucket")
	if bucket == "" {
		bucket = blob.DefaultBucketName("", repositoryID)
	}
	apply := boolParam(params, "apply_blob_creation")
	result, err := s.BlobClient.Provision(ctx, bucket, blob.ProvisionOptions{
		ApplyCreation: apply,
		RepositoryID:  repositoryID,
	})
	if errors.Is(err, blob.ErrApplyRequired) {
		return "", nil, rpc.NewError(
			"blob_apply_required",
			fmt.Sprintf("bucket %q does not exist; re-run repo add with --apply-blob-creation to create it", bucket),
			map[string]any{"bucket": bucket},
		)
	}
	if err != nil {
		return "", nil, rpc.NewError("blob_provision_failed", err.Error(), map[string]any{"bucket": bucket})
	}
	if result.Refused != "" {
		return "", nil, rpc.NewError("repo_blob_conflict", result.Refused, map[string]any{
			"bucket":        bucket,
			"repository_id": repositoryID,
		})
	}
	now := time.Now().UTC()
	return result.BucketName, &now, nil
}

// provisionAndRecord backfills blob_bucket on an existing registration
// row. Used when the operator enables blob storage after a repo was
// originally adopted without it.
func (s Service) provisionAndRecord(ctx context.Context, reg *repoRegistration, params map[string]any) error {
	bucket, createdAt, err := s.provisionBucket(ctx, reg.RepositoryID, params)
	if err != nil {
		return err
	}
	if err := s.Runner.Exec(ctx, `
		UPDATE striatumd.repositories
		   SET blob_bucket = $1, blob_created_at = $2
		 WHERE repository_id = $3`,
		bucket, createdAt, reg.RepositoryID,
	); err != nil {
		return err
	}
	reg.BlobBucket = bucket
	reg.BlobCreatedAt = createdAt
	return nil
}

func boolParam(params map[string]any, key string) bool {
	if value, ok := params[key].(bool); ok {
		return value
	}
	if value, ok := params[key].(string); ok {
		switch strings.ToLower(value) {
		case "1", "true", "yes", "on":
			return true
		}
	}
	return false
}

func (s Service) resolveRepoForInit(ctx context.Context, params map[string]any) (string, error) {
	path := stringParam(params, "path")
	if path == "" {
		path = stringParam(params, "repo_root")
	}
	if path != "" {
		return canonicalRepo(path)
	}
	repositoryID := stringParam(params, "repository_id")
	if repositoryID == "" {
		return "", rpc.NewError("schema_invalid", "repo.init requires path or repository_id", nil)
	}
	existing, err := s.findInitRegistration(ctx, "repository_id", repositoryID)
	if err != nil {
		return "", err
	}
	if existing == nil {
		return "", rpc.NewError("repo_not_registered", "repo.init repository_id is not registered", nil)
	}
	return canonicalRepo(existing.RepoRoot)
}

func (s Service) findInitRegistration(ctx context.Context, field string, value string) (*repoRegistration, error) {
	if value == "" {
		return nil, nil
	}
	where := "repository_id = $1"
	switch field {
	case "repo_identity":
		where = "repo_identity = $1"
	case "repo_root":
		where = "repo_root = $1"
	}
	row := s.Runner.QueryRow(ctx, `
		SELECT repository_id, repo_root, repo_identity, state_db_path,
		       last_schema_version, state, blob_bucket, blob_created_at
		  FROM striatumd.repositories
		 WHERE `+where+` AND state != 'removed'
		 ORDER BY repository_id
		 LIMIT 1`, value)
	var found repoRegistration
	var blobBucket *string
	var blobCreatedAt *time.Time
	err := row.Scan(
		&found.RepositoryID,
		&found.RepoRoot,
		&found.RepoIdentity,
		&found.StateDBPath,
		&found.SchemaVersion,
		&found.State,
		&blobBucket,
		&blobCreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if blobBucket != nil {
		found.BlobBucket = *blobBucket
	}
	found.BlobCreatedAt = blobCreatedAt
	return &found, nil
}

func repoInitResult(row repoRegistration, already bool) map[string]any {
	result := map[string]any{
		"repository_id":      row.RepositoryID,
		"repo_root":          row.RepoRoot,
		"repo_identity":      row.RepoIdentity,
		"state_db_path":      row.StateDBPath,
		"schema_version":     row.SchemaVersion,
		"state":              row.State,
		"already_registered": already,
		"substrate":          "postgres",
		"sqlite_dependency":  false,
		"python_dependency":  false,
		"source_import_mode": "none",
	}
	if row.BlobBucket != "" {
		result["blob_bucket"] = row.BlobBucket
	}
	if row.BlobCreatedAt != nil {
		result["blob_created_at"] = row.BlobCreatedAt.UTC().Format(time.RFC3339)
	}
	return result
}

func canonicalRepo(value string) (string, error) {
	if hasParentTraversal(value) {
		return "", rpc.NewError("path_traversal", "repo path traversal is not allowed", nil)
	}
	expanded, err := expandHome(value)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		expanded = filepath.Join(wd, expanded)
	}
	if hasSymlinkComponent(expanded) {
		return "", rpc.NewError("symlink_refused", "repo registration refuses symlink paths", nil)
	}
	resolved, err := filepath.EvalSymlinks(expanded)
	if err != nil {
		return "", rpc.NewError("repo_not_found", "repo path does not exist", map[string]any{"path": expanded})
	}
	stateDir := filepath.Join(resolved, ".striatum")
	if hasSymlinkComponent(stateDir) {
		return "", rpc.NewError("symlink_refused", "repo scratch directory symlink is not allowed", nil)
	}
	return resolved, nil
}

func initOperationalScratch(repo string) (string, error) {
	stateDir := filepath.Join(repo, ".striatum")
	if hasSymlinkComponent(stateDir) {
		return "", rpc.NewError("symlink_refused", "repo scratch directory symlink is not allowed", nil)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "scratch"), 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(stateDir, 0o700)
	_ = os.Chmod(filepath.Join(stateDir, "scratch"), 0o700)
	if err := ensureGitignore(repo); err != nil {
		return "", err
	}
	return stateDir, nil
}

func ensureGitignore(repo string) error {
	path := filepath.Join(repo, ".gitignore")
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(body), "\n") {
		if line == ".striatum/" {
			return nil
		}
	}
	prefix := ""
	if len(body) > 0 && !strings.HasSuffix(string(body), "\n") {
		prefix = "\n"
	}
	return os.WriteFile(path, []byte(string(body)+prefix+".striatum/\n"), 0o644)
}

func repoIdentity(repo string) (string, error) {
	info, err := os.Stat(repo)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("repo stat is not syscall.Stat_t")
	}
	return fmt.Sprintf("inode:%d:%d:root:%s", stat.Dev, stat.Ino, repo), nil
}

func hasParentTraversal(value string) bool {
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if part == ".." {
			return true
		}
	}
	return false
}

func hasSymlinkComponent(path string) bool {
	current := ""
	if filepath.IsAbs(path) {
		current = string(os.PathSeparator)
	}
	for _, part := range strings.Split(filepath.Clean(path), string(os.PathSeparator)) {
		if part == "" || part == string(os.PathSeparator) {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

func expandHome(value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if value == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(value, "~/")), nil
	}
	return value, nil
}

func randomHex(bytesLen int) string {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func stringParam(params map[string]any, key string) string {
	if value, ok := params[key].(string); ok {
		return value
	}
	return ""
}
