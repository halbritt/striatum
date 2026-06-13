package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5/pgconn"
)

// ArtifactRow is the content of one striatumd.artifacts row, independent of the
// write path. Nullable columns are typed any so a caller can pass nil or a value
// straight through, keeping the direct-INSERT path byte-identical to the historical
// inline INSERTs (RunID is NOT NULL but typed any so a caller can pass a map-scanned
// value directly). publish_mode and attempt fall back to their column-default shape
// when empty/nil.
type ArtifactRow struct {
	RepositoryID    string
	ArtifactID      string
	RunID           any
	JobID           any
	SessionID       any
	LogicalName     string
	ArtifactKind    string
	RepoPath        string
	ContentSHA256   string
	SizeBytes       int
	PublishMode     string
	CreatedAt       any
	AuthorLine      any
	BlobKey         any
	BlobSHA256      any
	BlobContentType any
	Attempt         any
	Placement       any
}

// AppendArtifactInTx writes one artifacts row inside the caller's mutation
// transaction. When the active write-boundary phase has reached audit_artifacts
// (RFC 0110 §7 P1) it routes through the owner-owned SECURITY DEFINER
// striatumd.append_artifact_row — which asserts daemon authority before the
// INSERT, so a leaked runtime credential cannot forge an artifact — otherwise it
// performs the historical direct INSERT (behavior-neutral until the operator
// adopts P1 and applies owner bundle 0003). The two paths emit an identical row.
func AppendArtifactInTx(ctx context.Context, tx TxRunner, row ArtifactRow) error {
	if ActiveWriteBoundary().AtLeast(PhaseAuditArtifacts) {
		return appendArtifactRowSD(ctx, tx, row)
	}
	return appendArtifactRowDirect(ctx, tx, row)
}

func publishModeOrDefault(mode string) string {
	if mode == "" {
		return "create"
	}
	return mode
}

type scalarQueryer interface {
	QueryScalar(context.Context, string, ...any) (string, error)
}

func ArtifactPlacementColumnPresent(ctx context.Context, runner scalarQueryer) bool {
	value, err := runner.QueryScalar(ctx, `
		SELECT EXISTS (
		  SELECT 1
		    FROM information_schema.columns
		   WHERE table_schema = 'striatumd'
		     AND table_name = 'artifacts'
		     AND column_name = 'placement'
		)::text`)
	return err == nil && value == "true"
}

func artifactPlacementAppendOverloadPresent(ctx context.Context, runner scalarQueryer) bool {
	value, err := runner.QueryScalar(ctx, `
		SELECT (to_regprocedure('striatumd.append_artifact_row(text,text,text,text,text,text,text,text,text,bigint,text,timestamptz,text,text,text,text,integer,text)') IS NOT NULL)::text`)
	return err == nil && value == "true"
}

// appendArtifactRowDirect is the pre-P1 path: a direct runtime-role INSERT.
func appendArtifactRowDirect(ctx context.Context, tx TxRunner, row ArtifactRow) error {
	if ArtifactPlacementColumnPresent(ctx, tx) {
		return tx.Exec(ctx, `
		INSERT INTO striatumd.artifacts (
		  repository_id, artifact_id, run_id, job_id, session_id, logical_name,
		  artifact_kind, repo_path, content_sha256, size_bytes, publish_mode,
		  created_at, author_line, blob_key, blob_sha256, blob_content_type, attempt,
		  placement
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,COALESCE($17,1),NULLIF($18::text,''))`,
			row.RepositoryID, row.ArtifactID, row.RunID, row.JobID, row.SessionID, row.LogicalName,
			row.ArtifactKind, row.RepoPath, row.ContentSHA256, row.SizeBytes, publishModeOrDefault(row.PublishMode),
			row.CreatedAt, row.AuthorLine, row.BlobKey, row.BlobSHA256, row.BlobContentType, row.Attempt, row.Placement)
	}
	return tx.Exec(ctx, `
		INSERT INTO striatumd.artifacts (
		  repository_id, artifact_id, run_id, job_id, session_id, logical_name,
		  artifact_kind, repo_path, content_sha256, size_bytes, publish_mode,
		  created_at, author_line, blob_key, blob_sha256, blob_content_type, attempt
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,COALESCE($17,1))`,
		row.RepositoryID, row.ArtifactID, row.RunID, row.JobID, row.SessionID, row.LogicalName,
		row.ArtifactKind, row.RepoPath, row.ContentSHA256, row.SizeBytes, publishModeOrDefault(row.PublishMode),
		row.CreatedAt, row.AuthorLine, row.BlobKey, row.BlobSHA256, row.BlobContentType, row.Attempt)
}

// appendArtifactRowSD is the P1 path: the owner-owned SECURITY DEFINER append
// function. A lost/again-rotated authority surfaces as a structured
// daemon_auth_lost (RFC 0110 §4.5) rather than a bare 28000.
func appendArtifactRowSD(ctx context.Context, tx TxRunner, row ArtifactRow) error {
	if artifactPlacementAppendOverloadPresent(ctx, tx) {
		_, err := tx.QueryScalar(ctx,
			`SELECT striatumd.append_artifact_row($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
			row.RepositoryID, row.ArtifactID, row.RunID, row.JobID, row.SessionID, row.LogicalName,
			row.ArtifactKind, row.RepoPath, row.ContentSHA256, row.SizeBytes, publishModeOrDefault(row.PublishMode),
			row.CreatedAt, row.AuthorLine, row.BlobKey, row.BlobSHA256, row.BlobContentType, row.Attempt, row.Placement)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == sqlStateInvalidAuthorization {
				return rpc.NewError(
					"daemon_auth_lost",
					"daemon authority is no longer valid (registry row missing/superseded — restart the daemon to re-bootstrap, or check for a concurrent rotator)",
					map[string]any{"sqlstate": sqlStateInvalidAuthorization},
				)
			}
			return fmt.Errorf("append_artifact_row (sd): %w", err)
		}
		return nil
	}
	_, err := tx.QueryScalar(ctx,
		`SELECT striatumd.append_artifact_row($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		row.RepositoryID, row.ArtifactID, row.RunID, row.JobID, row.SessionID, row.LogicalName,
		row.ArtifactKind, row.RepoPath, row.ContentSHA256, row.SizeBytes, publishModeOrDefault(row.PublishMode),
		row.CreatedAt, row.AuthorLine, row.BlobKey, row.BlobSHA256, row.BlobContentType, row.Attempt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == sqlStateInvalidAuthorization {
			return rpc.NewError(
				"daemon_auth_lost",
				"daemon authority is no longer valid (registry row missing/superseded — restart the daemon to re-bootstrap, or check for a concurrent rotator)",
				map[string]any{"sqlstate": sqlStateInvalidAuthorization},
			)
		}
		return fmt.Errorf("append_artifact_row (sd): %w", err)
	}
	return nil
}
