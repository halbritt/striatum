// Package admin contains daemon-global administrative RPC handlers.
package admin

import (
	"context"

	"github.com/halbritt/striatum/go/pkg/blob"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

type Service struct {
	Runner        db.Runner
	DaemonVersion string
	ShutdownHook  ShutdownFunc
	KeyRotateHook KeyRotateFunc
	// BlobClient is the daemon's S3 client. Nil when blob storage is
	// not configured (STRIATUM_BLOB_ENDPOINT unset); RepoInit then
	// records a NULL striatumd.repositories.blob_bucket and the
	// publish path falls back to repo-path bodies even for blob_exhaust
	// placements.
	BlobClient *blob.Client
}

func (s Service) Register(server *rpc.Server) {
	if s.Runner == nil {
		return
	}
	server.Register("daemon.migrate", s.Migrate)
	server.Register("daemon.token.create", s.CreateToken)
	server.Register("daemon.token.revoke", s.RevokeToken)
	server.Register("daemon.token.rotate", s.RotateToken)
	server.Register("daemon.key.rotate", s.KeyRotate)
	server.Register("daemon.shutdown", s.Shutdown)
	server.Register("repo.init", s.RepoInit)
}

func (s Service) Migrate(ctx context.Context, envelope rpc.Envelope) (map[string]any, error) {
	versionLabel := s.DaemonVersion
	if versionLabel == "" {
		versionLabel = "unknown"
	}
	version, err := db.ApplyMigrations(ctx, s.Runner, versionLabel)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":             "migrated",
		"substrate":          "postgres",
		"substrate_schema":   version,
		"latest_schema":      db.LatestDaemonDBVersion,
		"migration_count":    db.LatestDaemonDBVersion,
		"daemon_version":     versionLabel,
		"sqlite_dependency":  false,
		"python_dependency":  false,
		"source_import_mode": "none",
	}, nil
}
