package reads

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

func HandleRunEvents(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "run.events requires run_id", nil)
	}
	sinceEventID, ok := nonNegativeIntParam(envelope, "since_event_id", 0)
	if !ok {
		return nil, rpc.NewError("schema_invalid", "since_event_id must be a non-negative integer", nil)
	}
	limit := boundedLimit(envelope, 100, 500)
	runs, err := collectRows(ctx, runner,
		`SELECT run_id, state
		   FROM striatumd.runs
		  WHERE repository_id = $1 AND run_id = $2`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, rpc.NewError("not_found", "run not found: "+runID, nil)
	}
	events, err := collectRows(ctx, runner,
		`SELECT event_id, run_id, event_type, actor_session_id, job_id,
		        payload_json, created_at
		   FROM striatumd.events
		  WHERE repository_id = $1
		    AND run_id = $2
		    AND event_id > $3
		  ORDER BY event_id
		  LIMIT $4`,
		repositoryID, runID, sinceEventID, limit,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"run":            runs[0],
		"events":         shapeEvents(events),
		"count":          len(events),
		"since_event_id": sinceEventID,
		"terminal":       terminalRun(runs[0]),
	}, nil
}

func HandleRunDetail(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "run.detail requires run_id", nil)
	}
	runs, err := collectRows(ctx, runner,
		`SELECT *
		   FROM striatumd.runs
		  WHERE repository_id = $1 AND run_id = $2`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, rpc.NewError("not_found", "run not found: "+runID, nil)
	}
	jobs, err := collectRows(ctx, runner,
		`SELECT *
		   FROM striatumd.jobs
		  WHERE repository_id = $1 AND run_id = $2
		  ORDER BY workflow_job_id, attempt`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	workflow := loadWorkflowSnapshot(ctx, runner, repositoryID, runs[0])
	// #71: the detail run row carries repo_root + branch_name (SELECT *), so
	// surface the advisory branch-divergence warning here too; the run.status
	// surface decorates its own run rows in statusRuns.
	decorateBranchDivergence(runs[0])
	status, err := HandleStatus(ctx, runner, envelope)
	if err != nil {
		status = map[string]any{"next_actions": []any{}}
	}
	return map[string]any{
		"run":                   runs[0],
		"workflow":              workflow,
		"jobs":                  jobs,
		"next_actions":          status["next_actions"],
		"recovery_panel":        map[string]any{},
		"verdicts_by_posture":   status["verdicts_by_posture"],
		"sessions":              status["sessions"],
		"phase_progress":        []any{},
		"current_phase_id":      nil,
		"suggested_branch_name": "",
		"allow_dirty":           false,
	}, nil
}

// loadWorkflowSnapshot returns the parsed workflow_json body for the
// run's recorded workflow_snapshot_id, or an empty map on any
// failure. The run-detail render path validates the returned map; an
// empty map renders as "(no jobs)" rather than crashing the page.
func loadWorkflowSnapshot(
	ctx context.Context,
	runner db.Runner,
	repositoryID string,
	run map[string]any,
) map[string]any {
	snapshotID, _ := run["workflow_snapshot_id"].(string)
	if snapshotID == "" {
		return map[string]any{}
	}
	rows, err := collectRows(ctx, runner,
		`SELECT workflow_json
		   FROM striatumd.workflow_snapshots
		  WHERE repository_id = $1 AND workflow_snapshot_id = $2
		  LIMIT 1`,
		repositoryID, snapshotID,
	)
	if err != nil || len(rows) == 0 {
		return map[string]any{}
	}
	body := rows[0]["workflow_json"]
	if doc, ok := body.(map[string]any); ok {
		return doc
	}
	// pgx can return jsonb as []byte when the row-scan path does not
	// auto-decode; fall back to JSON-unmarshalling that shape.
	if raw, ok := body.([]byte); ok {
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err == nil {
			return doc
		}
	}
	if raw, ok := body.(string); ok {
		var doc map[string]any
		if err := json.Unmarshal([]byte(raw), &doc); err == nil {
			return doc
		}
	}
	return map[string]any{}
}

func HandleJobDetail(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	jobRef := stringParam(envelope, "job_id")
	if runID == "" || jobRef == "" {
		return nil, rpc.NewError("schema_invalid", "job.detail requires run_id and job_id", nil)
	}
	runs, err := collectRows(ctx, runner,
		`SELECT *
		   FROM striatumd.runs
		  WHERE repository_id = $1 AND run_id = $2`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, rpc.NewError("not_found", "not found", nil)
	}
	jobs, err := collectRows(ctx, runner,
		`SELECT *
		   FROM striatumd.jobs
		  WHERE repository_id = $1
		    AND run_id = $2
		    AND (job_id = $3 OR workflow_job_id = $3)
		  LIMIT 1`,
		repositoryID, runID, jobRef,
	)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, rpc.NewError("not_found", "not found", nil)
	}
	jobID := stringFrom(jobs[0], "job_id")
	artifacts, err := collectRows(ctx, runner,
		`SELECT a.*`+artifactProvenanceColumns+`
		   FROM striatumd.artifacts a`+artifactProvenanceJoins+`
		  WHERE a.repository_id = $1 AND a.job_id = $2
		  ORDER BY a.created_at`,
		repositoryID, jobID,
	)
	if err != nil {
		return nil, err
	}
	decorateArtifactPlacements(artifacts)
	decorateArtifactProvenance(artifacts)
	verdicts, err := collectRows(ctx, runner,
		`SELECT *
		   FROM striatumd.verdicts
		  WHERE repository_id = $1 AND job_id = $2
		  ORDER BY created_at DESC, verdict_id DESC`,
		repositoryID, jobID,
	)
	if err != nil {
		return nil, err
	}
	var latestVerdict any
	if len(verdicts) > 0 {
		latestVerdict = verdicts[0]
	}
	return map[string]any{
		"run":                    runs[0],
		"job":                    jobs[0],
		"artifacts":              artifacts,
		"latest_verdict":         latestVerdict,
		"verdicts":               verdicts,
		"expected_artifact_rows": []any{},
		"process_evidence":       map[string]any{},
	}, nil
}

func HandleArtifactShow(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	artifactID := stringParam(envelope, "artifact_id")
	if artifactID == "" {
		return nil, rpc.NewError("schema_invalid", "artifact.show requires artifact_id", nil)
	}
	artifacts, err := collectRows(ctx, runner,
		`SELECT a.artifact_id, a.run_id, a.job_id, a.session_id, a.logical_name,
		        a.artifact_kind, a.repo_path, a.content_sha256, a.size_bytes,
		        a.publish_mode, a.created_at, a.author_line,
		        a.attestation_override_rationale`+artifactPlacementProjection(ctx, runner, "a")+artifactProvenanceColumns+`
		   FROM striatumd.artifacts a`+artifactProvenanceJoins+`
		  WHERE a.repository_id = $1 AND a.artifact_id = $2`,
		repositoryID, artifactID,
	)
	if err != nil {
		return nil, err
	}
	if len(artifacts) == 0 {
		return nil, rpc.NewError("not_found", "artifact not found: "+artifactID, nil)
	}
	decorateArtifactPlacements(artifacts)
	artifact := artifacts[0]
	decorateArtifactProvenance(artifacts)
	if runID := stringParam(envelope, "run_id"); runID != "" && stringFrom(artifact, "run_id") != runID {
		return nil, rpc.NewError("not_found", "artifact not found in run: "+artifactID, nil)
	}
	if include, _ := envelope.Params["include_web_context"].(bool); !include {
		return map[string]any{"artifact": artifact}, nil
	}
	runID := stringFrom(artifact, "run_id")
	runs, err := collectRows(ctx, runner,
		`SELECT run_id, state, branch_name
		   FROM striatumd.runs
		  WHERE repository_id = $1 AND run_id = $2`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, rpc.NewError("not_found", "run not found: "+runID, nil)
	}
	provenance, err := collectRows(ctx, runner,
		`SELECT event_id, event_type, actor_session_id, job_id, artifact_id,
		        lease_id, payload_json, created_at
		   FROM striatumd.events
		  WHERE repository_id = $1
		    AND run_id = $2
		    AND event_type IN (
		      'recovery.auto_published',
		      'provenance.publish_without_process_execution'
		    )
		    AND (artifact_id = $3 OR payload_json::text LIKE $4)
		  ORDER BY event_id`,
		repositoryID, runID, artifactID, "%"+artifactID+"%",
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"artifact":              artifact,
		"run":                   runs[0],
		"expected_author_line":  nil,
		"provenance_trail":      provenance,
		"web_context_complete":  false,
		"web_context_port_note": "expected author line parity remains RFC 0068 work",
	}, nil
}

func HandleRunPostureVerdicts(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	posture := stringParam(envelope, "posture")
	if runID == "" || posture == "" {
		return nil, rpc.NewError("schema_invalid", "run.posture_verdicts requires run_id and posture", nil)
	}
	if strings.Contains(posture, "/") || strings.Contains(posture, "..") || strings.ContainsRune(posture, 0) {
		return nil, rpc.NewError("schema_invalid", "posture contains invalid path characters", nil)
	}
	runs, err := collectRows(ctx, runner,
		`SELECT run_id, state, branch_name
		   FROM striatumd.runs
		  WHERE repository_id = $1 AND run_id = $2`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, rpc.NewError("not_found", "run not found: "+runID, nil)
	}
	verdicts, err := collectRows(ctx, runner,
		`SELECT v.verdict_id, v.verdict, v.rationale, v.created_at,
		        v.job_id, v.findings_artifact_id, v.session_id, v.posture,
		        j.workflow_job_id, j.role_id, j.lane_selector_json,
		        s.slug AS session_slug
		   FROM striatumd.verdicts v
		   JOIN striatumd.jobs j
		     ON j.repository_id = v.repository_id
		    AND j.job_id = v.job_id
		   JOIN striatumd.sessions s
		     ON s.repository_id = v.repository_id
		    AND s.session_id = v.session_id
		  WHERE v.repository_id = $1 AND v.run_id = $2 AND v.posture = $3
		  ORDER BY v.created_at DESC, v.verdict_id DESC`,
		repositoryID, runID, posture,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"run":      runs[0],
		"posture":  posture,
		"verdicts": shapeVerdictSources(verdicts),
		"count":    len(verdicts),
	}, nil
}

func HandleEscalationList(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	state := stringParam(envelope, "state")
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "resolved" && state != "canceled" && state != "all" {
		return nil, rpc.NewError("schema_invalid", "state must be open, resolved, canceled, or all", nil)
	}
	limit := boundedLimit(envelope, 100, 1000)
	args := []any{repositoryID}
	where := `WHERE b.repository_id = $1 AND ` + escalationPredicate()
	if runID := stringParam(envelope, "run_id"); runID != "" {
		args = append(args, runID)
		where += " AND b.run_id = $2"
	}
	if state != "all" {
		args = append(args, state)
		where += " AND b.state = " + placeholder(len(args))
	}
	args = append(args, limit)
	rows, err := collectRows(ctx, runner,
		`SELECT b.blocker_id AS escalation_id, b.blocker_id, b.run_id,
		        b.job_id, j.workflow_job_id, b.session_id,
		        s.role_id AS session_role_id, s.lane_id AS session_lane_id,
		        b.severity, b.blocker_kind AS class, b.description, b.state,
		        b.created_at, b.resolved_at, b.payload_json,
		        a.artifact_id AS linked_artifact_id,
		        a.repo_path AS linked_repo_path,
		        a.content_sha256 AS linked_content_sha256,
		        e.state AS inbox_state,
		        e.viewed_at,
		        e.decision_artifact_id,
		        e.resolution_note
		   FROM striatumd.blockers b
		   LEFT JOIN striatumd.jobs j
		     ON j.repository_id = b.repository_id AND j.job_id = b.job_id
		   LEFT JOIN striatumd.sessions s
		     ON s.repository_id = b.repository_id AND s.session_id = b.session_id
		   LEFT JOIN striatumd.artifacts a
		     ON a.repository_id = b.repository_id
		    AND a.artifact_id = b.payload_json #>> '{escalation_artifact,artifact_id}'
		   LEFT JOIN striatumd.escalation_inbox e
		     ON e.repository_id = b.repository_id AND e.escalation_id = b.blocker_id
		  `+where+
			` ORDER BY b.created_at ASC, b.blocker_id ASC
		    LIMIT `+placeholder(len(args)),
		args...,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"repository_id": repositoryID,
		"run_id":        stringParam(envelope, "run_id"),
		"state":         state,
		"count":         len(rows),
		"escalations":   shapeEscalations(rows),
	}, nil
}

func HandleEscalationShow(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	escalationID := stringParam(envelope, "escalation_id")
	if escalationID == "" {
		escalationID = stringParam(envelope, "blocker_id")
	}
	if escalationID == "" {
		return nil, rpc.NewError("schema_invalid", "escalation_id must be a non-empty string", nil)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.escalation_inbox
		   SET state = 'viewed', viewed_at = $1
		 WHERE repository_id = $2
		   AND escalation_id = $3
		   AND state = 'pending'`,
		now, repositoryID, escalationID); err != nil {
		return nil, err
	}
	rows, err := collectRows(ctx, runner,
		`SELECT b.blocker_id AS escalation_id, b.blocker_id, b.run_id,
		        b.job_id, j.workflow_job_id, b.session_id,
		        s.role_id AS session_role_id, s.lane_id AS session_lane_id,
		        b.severity, b.blocker_kind AS class, b.description, b.state,
		        b.created_at, b.resolved_at, b.payload_json,
		        a.artifact_id AS linked_artifact_id,
		        a.repo_path AS linked_repo_path,
		        a.content_sha256 AS linked_content_sha256,
		        e.state AS inbox_state,
		        e.viewed_at,
		        e.decision_artifact_id,
		        e.resolution_note
		   FROM striatumd.blockers b
		   LEFT JOIN striatumd.jobs j
		     ON j.repository_id = b.repository_id AND j.job_id = b.job_id
		   LEFT JOIN striatumd.sessions s
		     ON s.repository_id = b.repository_id AND s.session_id = b.session_id
		   LEFT JOIN striatumd.artifacts a
		     ON a.repository_id = b.repository_id
		    AND a.artifact_id = b.payload_json #>> '{escalation_artifact,artifact_id}'
		   LEFT JOIN striatumd.escalation_inbox e
		     ON e.repository_id = b.repository_id AND e.escalation_id = b.blocker_id
		  WHERE b.repository_id = $1
		    AND b.blocker_id = $2
		    AND `+escalationPredicate(),
		repositoryID, escalationID,
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, rpc.NewError("not_found", "escalation not found: "+escalationID, nil)
	}
	return map[string]any{"escalation": shapeEscalations(rows)[0]}, nil
}

func nonNegativeIntParam(envelope rpc.Envelope, key string, fallback int) (int, bool) {
	value, exists := envelope.Params[key]
	if !exists || value == nil {
		return fallback, true
	}
	switch typed := value.(type) {
	case int:
		return typed, typed >= 0
	case int64:
		return int(typed), typed >= 0
	case float64:
		return int(typed), typed >= 0 && typed == float64(int(typed))
	default:
		return fallback, false
	}
}

func boundedLimit(envelope rpc.Envelope, fallback int, max int) int {
	limit, ok := nonNegativeIntParam(envelope, "limit", fallback)
	if !ok || limit <= 0 {
		return fallback
	}
	if limit > max {
		return max
	}
	return limit
}

func shapeEvents(rows []map[string]any) []map[string]any {
	events := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		events = append(events, map[string]any{
			"event_id":         row["event_id"],
			"run_id":           row["run_id"],
			"type":             row["event_type"],
			"actor_session_id": row["actor_session_id"],
			"job_id":           row["job_id"],
			"payload":          objectOrEmpty(row["payload_json"]),
			"created_at":       row["created_at"],
		})
	}
	return events
}

func shapeVerdictSources(rows []map[string]any) []map[string]any {
	shaped := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := copyMap(row)
		item["source"] = "natural"
		item["provenance"] = "natural"
		item["override_rationale"] = nil
		shaped = append(shaped, item)
	}
	return shaped
}

func shapeEscalations(rows []map[string]any) []map[string]any {
	shaped := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		source := "blocker"
		if stringFrom(row, "severity") == "human_checkpoint" {
			source = "human_checkpoint"
		}
		shaped = append(shaped, map[string]any{
			"escalation_id":        row["escalation_id"],
			"blocker_id":           row["blocker_id"],
			"run_id":               row["run_id"],
			"job_id":               row["job_id"],
			"workflow_job_id":      row["workflow_job_id"],
			"session_id":           row["session_id"],
			"session_role_id":      row["session_role_id"],
			"session_lane_id":      row["session_lane_id"],
			"source":               source,
			"class":                row["class"],
			"severity":             row["severity"],
			"description":          row["description"],
			"state":                row["state"],
			"inbox_state":          row["inbox_state"],
			"viewed_at":            row["viewed_at"],
			"created_at":           row["created_at"],
			"resolved_at":          row["resolved_at"],
			"decision_artifact_id": row["decision_artifact_id"],
			"resolution_note":      row["resolution_note"],
			"escalation_artifact":  escalationArtifactSummary(row),
			"payload":              objectOrEmpty(row["payload_json"]),
		})
	}
	return shaped
}

func escalationArtifactSummary(row map[string]any) any {
	payload := objectOrEmpty(row["payload_json"])
	value, ok := payload["escalation_artifact"].(map[string]any)
	if !ok {
		return nil
	}
	required := []string{"artifact_id", "repo_path", "content_sha256", "linked_at", "link_source"}
	for _, key := range required {
		if stringFrom(value, key) == "" {
			return nil
		}
	}
	if stringFrom(value, "artifact_id") != stringFrom(row, "linked_artifact_id") ||
		stringFrom(value, "repo_path") != stringFrom(row, "linked_repo_path") ||
		stringFrom(value, "content_sha256") != stringFrom(row, "linked_content_sha256") {
		return nil
	}
	return map[string]any{
		"artifact_id":    stringFrom(value, "artifact_id"),
		"repo_path":      stringFrom(value, "repo_path"),
		"content_sha256": stringFrom(value, "content_sha256"),
		"linked_at":      stringFrom(value, "linked_at"),
		"link_source":    stringFrom(value, "link_source"),
	}
}

func objectOrEmpty(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	if raw, ok := value.([]byte); ok {
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err == nil && object != nil {
			return object
		}
	}
	if raw, ok := value.(string); ok {
		var object map[string]any
		if err := json.Unmarshal([]byte(raw), &object); err == nil && object != nil {
			return object
		}
	}
	return map[string]any{}
}

func copyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func terminalRun(run map[string]any) bool {
	state := stringFrom(run, "state")
	return state == "completed" || state == "failed" || state == "canceled"
}

func escalationPredicate() string {
	// 'recovery_exhausted' is the RFC 0101 Phase 4 daemon-authored escalation for
	// a job the autonomous recovery loop could not reclaim within its per-job
	// budget; including it here makes HandleEscalationResolve able to find and
	// resolve it (which clears the run's needs_operator state).
	return "(b.severity = 'human_checkpoint' OR b.blocker_kind IN ('ai_self_declared', 'ambiguous_goal', 'committee_stalemate', 'contradicting_decisions', 'missing_authority', 'no_available_reviewer_lane', 'override_required', 'recovery_exhausted', 'provenance_gate_failed'))"
}

func placeholder(index int) string {
	return "$" + strconv.Itoa(index)
}
