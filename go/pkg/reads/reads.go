// Package reads contains the Go RFC 0048 Phase B transition handlers for
// read-surface CLI verbs. Each exported Handle* function mirrors a Python
// read handler in src/striatum/daemon_pg/handlers/reads/ and returns the same
// top-level JSON shape so compatibility fixtures can compare the two paths.
//
// Scope (this file holds the shared helpers; per-method files in this
// package hold the handlers):
//   - status, dashboard, doctor, why — core operator reads.
//   - list.runs, list.sessions, list.jobs, list.artifacts,
//     list.workflows — listing reads.
//   - run.summary, evidence.export, corpus.export — reporting reads.
//
// Most handlers scope by repository_id. dashboard.all is the daemon-global
// aggregate read, and escalation.resolve is the one exception to the
// SELECT-only rule because its Python parity handler lives beside the
// escalation projection.
package reads

import (
	"context"
	"fmt"
	"strconv"

	"github.com/halbritt/striatum/go/pkg/blob"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

// Queryer narrows db.Runner to the multi-row Query method used by the
// read package; crossrepo uses the same shape internally.
type Queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func queryer(runner any) (Queryer, error) {
	q, ok := runner.(Queryer)
	if !ok {
		return nil, fmt.Errorf("runner does not support row queries")
	}
	return q, nil
}

func requireRepositoryID(envelope rpc.Envelope) (string, error) {
	value, _ := envelope.Params["repository_id"].(string)
	if value == "" {
		return "", rpc.NewError("repo_not_registered", "daemon RPC route requires repository_id", nil)
	}
	return value, nil
}

func stringParam(envelope rpc.Envelope, key string) string {
	if value, ok := envelope.Params[key].(string); ok {
		return value
	}
	return ""
}

func collectRows(ctx context.Context, runner any, sql string, args ...any) ([]map[string]any, error) {
	q, err := queryer(runner)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToMap)
}

func intFrom(m map[string]any, key string) int {
	switch value := m[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		parsed, _ := strconv.Atoi(value)
		return parsed
	default:
		return 0
	}
}

// Options carries handler-construction dependencies that not all read
// handlers share. Today only artifact.get_content reads BlobClient.
// BlobClient may be nil when the daemon is started without
// STRIATUM_BLOB_ENDPOINT; artifact.get_content then refuses for
// blob-routed artifacts but still serves legacy repo-path bodies.
type Options struct {
	BlobClient      *blob.Client
	StriatumVersion string
}

type readBlobClient interface {
	BucketExists(context.Context, string) (bool, error)
	GetBytes(context.Context, string, string, string) ([]byte, error)
	HeadObject(context.Context, string, string) (int64, string, error)
	ListByPrefix(context.Context, string, string) ([]blob.ListObjectEntry, error)
	ListCommonPrefixes(context.Context, string, string, string) ([]string, error)
	PutBytes(context.Context, string, string, []byte, string) (string, error)
	Reachable(context.Context) error
	RemoteSha256(context.Context, string, string) (string, bool, error)
}

// packageBlobClient mirrors mutations.packageBlobClient: a single
// daemon-startup-time blob client read by the artifact.get_content
// handler. nil means blob storage is disabled.
var packageBlobClient readBlobClient
var packageStriatumVersion = "dev"

// Register wires every read handler in this package onto the rpc.Server.
// Call from cmd/striatumd/main.go after the registry-side handlers land.
func Register(server *rpc.Server, runner db.Runner, opts ...Options) {
	if runner == nil {
		return
	}
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.BlobClient != nil {
		packageBlobClient = o.BlobClient
	} else {
		packageBlobClient = nil
	}
	if o.StriatumVersion != "" {
		packageStriatumVersion = o.StriatumVersion
	}
	server.Register("status", makeHandler(runner, HandleStatus))
	server.Register("dashboard", makeHandler(runner, HandleDashboard))
	server.Register("dashboard.all", makeHandler(runner, HandleDashboardAll))
	server.Register("doctor", makeHandler(runner, HandleDoctor))
	server.Register("doctor.blob_block", makeHandler(runner, HandleDoctorBlobBlock))
	server.Register("join.verify", makeHandler(runner, HandleJoinVerify))
	server.Register("why", makeHandler(runner, HandleWhy))
	server.Register("whose", makeHandler(runner, HandleWhose))
	server.Register("list.runs", makeHandler(runner, HandleListRuns))
	server.Register("list.sessions", makeHandler(runner, HandleListSessions))
	server.Register("list.jobs", makeHandler(runner, HandleListJobs))
	server.Register("list.artifacts", makeHandler(runner, HandleListArtifacts))
	server.Register("list.workflows", makeHandler(runner, HandleListWorkflows))
	server.Register("worktree.list", makeHandler(runner, HandleWorktreeList))
	server.Register("work.packet_show", makeHandler(runner, HandleWorkPacketShow))
	server.Register("run.summary", makeHandler(runner, HandleRunSummary))
	server.Register("run.graph", makeHandler(runner, HandleRunGraph))
	server.Register("run.detail", makeHandler(runner, HandleRunDetail))
	server.Register("job.detail", makeHandler(runner, HandleJobDetail))
	server.Register("run.events", makeHandler(runner, HandleRunEvents))
	server.Register("run.posture_verdicts", makeHandler(runner, HandleRunPostureVerdicts))
	server.Register("artifact.show", makeHandler(runner, HandleArtifactShow))
	server.Register("artifact.get_content", makeHandler(runner, HandleArtifactGetContent))
	server.Register("artifact.list_for_run", makeHandler(runner, HandleArtifactListForRun))
	server.Register("records.docket", makeHandler(runner, HandleRecordsDocket))
	server.Register("records.migration.materialize", makeHandler(runner, HandleRecordsMigrationMaterialize))
	server.Register("records.migration.verify", makeHandler(runner, HandleRecordsMigrationVerify))
	server.Register("git.snapshot", makeHandler(runner, HandleGitSnapshot))
	server.Register("corpus.list_historical_dogfoods", makeHandler(runner, HandleCorpusListHistoricalDogfoods))
	server.Register("corpus.list_historical_dogfood_files", makeHandler(runner, HandleCorpusListHistoricalDogfoodFiles))
	server.Register("corpus.fetch_historical_dogfood_file", makeHandler(runner, HandleCorpusFetchHistoricalDogfoodFile))
	server.Register("archive.create", makeHandler(runner, HandleArchiveCreate))
	server.Register("workflow.validate", makeHandler(runner, HandleWorkflowValidate))
	server.Register("workflow.lint", makeHandler(runner, HandleWorkflowLint))
	server.Register("workflow.plan", makeHandler(runner, HandleWorkflowPlan))
	server.Register("workflow.graph", makeHandler(runner, HandleWorkflowGraph))
	server.Register("workflow.accepted_risks.list", makeHandler(runner, HandleWorkflowAcceptedRisksList))
	server.Register("workflow.templates.list", makeHandler(runner, HandleWorkflowTemplatesList))
	server.Register("workflow.templates.show", makeHandler(runner, HandleWorkflowTemplatesShow))
	server.Register("workflow.generate.preview", makeHandler(runner, HandleWorkflowGeneratePreview))
	server.Register("evidence.export", makeHandler(runner, HandleEvidenceExport))
	server.Register("corpus.export", makeHandler(runner, HandleCorpusExport))
	server.Register("recall.search", makeHandler(runner, HandleRecallSearch))
	server.Register("escalation.list", makeHandler(runner, HandleEscalationList))
	server.Register("escalation.show", makeHandler(runner, HandleEscalationShow))
	server.Register("escalation.resolve", makeHandler(runner, HandleEscalationResolve))
	server.Register("supervise.status", makeHandler(runner, HandleSuperviseStatus))
	server.Register("supervise.list", makeHandler(runner, HandleSuperviseList))
	server.Register("supervise.trajectory", makeHandler(runner, HandleSuperviseTrajectory))
	server.Register("supervise.reattach_status", makeHandler(runner, HandleSuperviseReattachStatus))
	server.Register("trajectory.export", makeHandler(runner, HandleTrajectoryExport))
	server.Register("trajectory.watch", makeHandler(runner, HandleTrajectoryWatch))
	server.Register("interrogation.list", makeHandler(runner, HandleInterrogationList))
	server.Register("interrogation.show", makeHandler(runner, HandleInterrogationShow))
}

// handlerFn is the per-method signature: (ctx, runner, envelope) → response.
type handlerFn func(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error)

func makeHandler(runner db.Runner, fn handlerFn) rpc.Handler {
	return func(ctx context.Context, envelope rpc.Envelope) (map[string]any, error) {
		return fn(ctx, runner, envelope)
	}
}
