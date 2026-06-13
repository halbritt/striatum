package reads

import (
	"context"
	"strconv"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

func limitClause(envelope rpc.Envelope, defaultLimit int) (string, int) {
	limit := defaultLimit
	switch v := envelope.Params["limit"].(type) {
	case float64:
		limit = int(v)
	case int:
		limit = v
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			limit = parsed
		}
	}
	if limit <= 0 || limit > 1000 {
		limit = defaultLimit
	}
	return " LIMIT " + strconv.Itoa(limit), limit
}

// HandleListRuns mirrors reads/list_runs.py.
func HandleListRuns(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	state := stringParam(envelope, "state")
	args := []any{repositoryID}
	where := "WHERE repository_id = $1"
	if state != "" {
		where += " AND state = $2"
		args = append(args, state)
	}
	limit, count := limitClause(envelope, 200)
	items, err := collectRows(ctx, runner,
		`SELECT run_id, workflow_snapshot_id, repo_root, state, branch_name,
		        created_at, started_at, completed_at, stop_reason
		   FROM striatumd.runs `+where+
			` ORDER BY created_at DESC`+limit,
		args...,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(items), "limit": count, "items": items}, nil
}

// HandleListSessions mirrors reads/list_sessions.py.
func HandleListSessions(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	role := stringParam(envelope, "role")
	lane := stringParam(envelope, "lane")
	args := []any{repositoryID}
	where := "WHERE s.repository_id = $1"
	if runID != "" {
		args = append(args, runID)
		where += " AND s.run_id = $" + strconv.Itoa(len(args))
	}
	if role != "" {
		args = append(args, role)
		where += " AND s.role_id = $" + strconv.Itoa(len(args))
	}
	if lane != "" {
		args = append(args, lane)
		where += " AND s.lane_id = $" + strconv.Itoa(len(args))
	}
	limit, count := limitClause(envelope, 200)
	items, err := collectRows(ctx, runner,
		`SELECT s.session_id, s.run_id, s.role_id, s.lane_id, s.state, s.slug,
		        s.capabilities_json,
		        CASE WHEN ps.supervisor_id IS NOT NULL THEN 'attested' ELSE 'unattested' END AS lane_attestation,
		        CASE WHEN ps.supervisor_id IS NOT NULL THEN NULL ELSE 'no_attached_supervisor' END AS lane_attestation_reason,
		        s.registered_at, s.closed_at, s.close_reason
		   FROM striatumd.sessions s
		   LEFT JOIN striatumd.process_supervisors ps
		     ON ps.repository_id = s.repository_id
		    AND ps.session_id = s.session_id
		    AND ps.state = 'attached' `+where+
			` ORDER BY s.registered_at DESC`+limit,
		args...,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(items), "limit": count, "items": items}, nil
}

// HandleListJobs mirrors reads/list_jobs.py.
func HandleListJobs(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	state := stringParam(envelope, "state")
	workflowJobID := stringParam(envelope, "workflow_job_id")
	args := []any{repositoryID}
	where := "WHERE repository_id = $1"
	if runID != "" {
		args = append(args, runID)
		where += " AND run_id = $" + strconv.Itoa(len(args))
	}
	if state != "" {
		args = append(args, state)
		where += " AND state = $" + strconv.Itoa(len(args))
	}
	if workflowJobID != "" {
		args = append(args, workflowJobID)
		where += " AND workflow_job_id = $" + strconv.Itoa(len(args))
	}
	limit, count := limitClause(envelope, 500)
	items, err := collectRows(ctx, runner,
		`SELECT job_id, run_id, workflow_job_id, title, job_type, role_id,
		        state, attempt, max_attempts, current_message_id,
		        current_lease_id, ready_at, started_at, completed_at
		   FROM striatumd.jobs `+where+
			` ORDER BY created_at DESC`+limit,
		args...,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(items), "limit": count, "items": items}, nil
}

// HandleListArtifacts mirrors reads/list_artifacts.py.
func HandleListArtifacts(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	kind := stringParam(envelope, "kind")
	args := []any{repositoryID}
	where := "WHERE a.repository_id = $1"
	if runID != "" {
		args = append(args, runID)
		where += " AND a.run_id = $" + strconv.Itoa(len(args))
	}
	if kind != "" {
		args = append(args, kind)
		where += " AND a.artifact_kind = $" + strconv.Itoa(len(args))
	}
	limit, count := limitClause(envelope, 500)
	items, err := collectRows(ctx, runner,
		`SELECT a.artifact_id, a.run_id, a.job_id, a.session_id,
		        a.artifact_kind AS kind, a.logical_name,
		        a.repo_path AS path, a.content_sha256, a.author_line AS byline,
		        a.created_at AS published_at`+artifactPlacementProjection(ctx, runner, "a")+artifactProvenanceColumns+`
		   FROM striatumd.artifacts a`+artifactProvenanceJoins+
			where+
			` ORDER BY a.created_at DESC`+limit,
		args...,
	)
	if err != nil {
		return nil, err
	}
	decorateArtifactPlacements(items)
	decorateArtifactProvenance(items)
	return map[string]any{"count": len(items), "limit": count, "items": items}, nil
}

// HandleListWorkflows mirrors reads/list_workflows.py.
func HandleListWorkflows(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	limit, count := limitClause(envelope, 200)
	items, err := collectRows(ctx, runner,
		`SELECT workflow_snapshot_id, workflow_id, workflow_version,
		        content_sha256 AS snapshot_sha256, loaded_at AS captured_at
		   FROM striatumd.workflow_snapshots
		  WHERE repository_id = $1
		  ORDER BY loaded_at DESC`+limit,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(items), "limit": count, "items": items}, nil
}

// HandleWorktreeList mirrors daemon_pg/handlers/worktree.py::worktree_list.
func HandleWorktreeList(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID, err := optionalNonEmptyString(envelope, "run_id")
	if err != nil {
		return nil, err
	}
	args := []any{repositoryID}
	where := "WHERE w.repository_id = $1"
	if runID != "" {
		args = append(args, runID)
		where += " AND w.run_id = $" + strconv.Itoa(len(args))
	}
	rows, err := collectRows(ctx, runner,
		`SELECT w.worktree_id, w.run_id, w.job_id, w.lease_id,
		        w.base_branch, w.worktree_path, w.state, w.created_at,
		        w.released_at, w.removed_at, j.workflow_job_id,
		        j.state AS job_state, r.repo_root, r.branch_name
		   FROM striatumd.job_worktrees w
		   JOIN striatumd.jobs j
		     ON j.repository_id = w.repository_id
		    AND j.job_id = w.job_id
		   JOIN striatumd.runs r
		     ON r.repository_id = w.repository_id
		    AND r.run_id = w.run_id
		  `+where+`
		  ORDER BY w.created_at, w.worktree_id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		addWorktreeAnchorProjection(ctx, row)
	}
	return map[string]any{"worktrees": rows}, nil
}

func optionalNonEmptyString(envelope rpc.Envelope, key string) (string, error) {
	value, exists := envelope.Params[key]
	if !exists || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return "", rpc.NewError("schema_invalid", key+" must be a non-empty string when present", nil)
	}
	return text, nil
}
