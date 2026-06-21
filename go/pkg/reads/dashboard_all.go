package reads

import (
	"context"
	"encoding/json"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/lanehealth"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
)

const protocolVersion = 1

// HandleDashboardAll returns the daemon-global dashboard projection over the
// daemon-owned PostgreSQL registry and per-repository workflow tables.
//
// This is intentionally SELECT-only. Unlike the legacy Python/SQLite path, it
// does not lazily expire leases while rendering a dashboard frame.
func HandleDashboardAll(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	if runner == nil {
		return nil, rpc.NewError("daemon_db_missing", "dashboard.all requires daemon PostgreSQL", nil)
	}
	repos, err := collectRows(ctx, runner,
		`SELECT repository_id, display_name, repo_root, state
		   FROM striatumd.repositories
		  WHERE state != 'removed'
		  ORDER BY repository_id`)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		repositoryID := stringFrom(repo, "repository_id")
		entry := map[string]any{
			"repository_id": repositoryID,
			"display_name":  repo["display_name"],
			"repo_root":     repo["repo_root"],
			"state":         repo["state"],
		}
		status, err := dashboardAllStatus(ctx, runner, repositoryID)
		if err != nil {
			entry["state"] = "degraded"
			entry["error"] = err.Error()
			result = append(result, entry)
			continue
		}
		staleLeases, err := dashboardAllStaleLeases(ctx, runner, repositoryID)
		if err != nil {
			entry["state"] = "degraded"
			entry["error"] = err.Error()
			result = append(result, entry)
			continue
		}
		runProgress, err := dashboardAllRunProgress(ctx, runner, repositoryID)
		if err != nil {
			entry["state"] = "degraded"
			entry["error"] = err.Error()
			result = append(result, entry)
			continue
		}
		// RFC 0108 Phase 5: the repo-scoped concurrent-runs view (parallel fan-out
		// + live collisions), the read complement to the Phase 2/3 run.start gates.
		concurrentRuns, err := repoConcurrentRuns(ctx, runner, repositoryID)
		if err != nil {
			entry["state"] = "degraded"
			entry["error"] = err.Error()
			result = append(result, entry)
			continue
		}
		entry["status"] = status
		entry["stale_leases"] = staleLeases
		entry["run_progress"] = runProgress
		entry["concurrent_runs"] = concurrentRuns
		result = append(result, entry)
	}
	return map[string]any{
		"mode":             "daemon",
		"protocol_version": protocolVersion,
		"repositories":     result,
	}, nil
}

func dashboardAllStatus(ctx context.Context, runner db.Runner, repositoryID string) (map[string]any, error) {
	runs, err := collectRows(ctx, runner,
		`SELECT r.run_id, r.state, r.branch_name
		   FROM striatumd.runs r
		  WHERE r.repository_id = $1
		  ORDER BY r.created_at, r.run_id`,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	jobRows, err := collectRows(ctx, runner,
		`SELECT j.state, COUNT(*) AS count
		   FROM striatumd.jobs j
		  WHERE j.repository_id = $1
		  GROUP BY j.state
		  ORDER BY j.state`,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	jobs := map[string]int{}
	for _, row := range jobRows {
		jobs[stringFrom(row, "state")] = intFrom(row, "count")
	}
	openBlockers, err := dashboardAllBlockers(ctx, runner, repositoryID, "")
	if err != nil {
		return nil, err
	}
	humanCheckpoints, err := dashboardAllBlockers(ctx, runner, repositoryID, "human_checkpoint")
	if err != nil {
		return nil, err
	}
	nonAccepting, err := collectRows(ctx, runner,
		// #506: carry the rationale + linked findings artifact so the cross-run
		// dashboard surfaces WHY a review blocked, not just THAT it did. Enriched
		// below with a rationale excerpt and an `artifact get-content` hint, matching
		// the per-run status projection.
		`SELECT DISTINCT ON (v.job_id)
		        v.verdict_id, v.run_id, v.job_id, j.workflow_job_id,
		        v.verdict, v.posture, v.created_at,
		        v.rationale, v.findings_artifact_id
		   FROM striatumd.verdicts v
		   JOIN striatumd.jobs j
		     ON j.repository_id = v.repository_id
		    AND j.job_id = v.job_id
		  WHERE v.repository_id = $1 AND v.verdict NOT IN ('accept', 'accept_with_findings')
		    AND v.superseded_by_decision_id IS NULL
		  ORDER BY v.job_id, v.created_at DESC, v.verdict_id DESC`,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	for _, row := range nonAccepting {
		decorateReviewFindings(row)
	}
	verdictPostures, err := collectRows(ctx, runner,
		`SELECT v.posture, v.verdict, COUNT(*) AS count
		   FROM striatumd.verdicts v
		  WHERE v.repository_id = $1
		  GROUP BY v.posture, v.verdict
		  ORDER BY v.posture, v.verdict`,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	verdictsByPosture := map[string]map[string]int{}
	for _, row := range verdictPostures {
		posture := stringFrom(row, "posture")
		verdict := stringFrom(row, "verdict")
		if _, ok := verdictsByPosture[posture]; !ok {
			verdictsByPosture[posture] = map[string]int{}
		}
		verdictsByPosture[posture][verdict] = intFrom(row, "count")
	}
	claimable, err := collectRows(ctx, runner,
		`SELECT q.run_id, q.job_id, j.workflow_job_id,
		        q.target_role_id AS role_id, q.target_lane_id AS lane_id,
		        COUNT(*) AS count
		   FROM striatumd.queue_messages q
		   JOIN striatumd.jobs j
		     ON j.repository_id = q.repository_id
		    AND j.job_id = q.job_id
		  WHERE q.repository_id = $1
		    AND q.state = 'pending'
		    AND (q.visible_after IS NULL OR q.visible_after <= now())
		  GROUP BY q.run_id, q.job_id, j.workflow_job_id,
		           q.target_role_id, q.target_lane_id
		  ORDER BY q.target_role_id, q.target_lane_id, j.workflow_job_id`,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	blockedDownstream, err := collectRows(ctx, runner,
		`SELECT j.run_id, j.job_id, j.workflow_job_id, j.state
		   FROM striatumd.jobs j
		  WHERE j.repository_id = $1 AND j.state = 'blocked'
		  ORDER BY j.created_at, j.workflow_job_id`,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	sessions, err := collectRows(ctx, runner,
		`SELECT s.session_id, s.run_id, s.role_id, s.lane_id, s.slug,
		        s.ordinal, s.state, s.operator_label,
		        s.registered_at,
		        s.last_mcp_request_at,
		        s.last_tools_list_at,
		        s.last_await_packet_at,
		        s.last_packet_delivered_at,
		        s.last_ack_at,
		        s.last_work_block_at,
		        s.last_work_release_at,
		        s.last_work_complete_at,
		        s.last_work_heartbeat_at,
		        s.last_session_ready_at,
		        s.last_session_heartbeat_at,
		        s.last_session_question_at,
		        s.last_session_escalate_at,
		        s.last_pty_activity_at,
		        s.last_tool_call_started_at,
		        s.last_tool_call_finished_at,
		        s.liveness_stall_class,
		        s.liveness_stall_since,
		        ps.supervisor_id AS supervisor_id,
		        ps.pid AS pid,
		        ps.pid_start_time AS pid_start_time,
		        ptr.metadata_json AS supervisor_metadata_json,
		        active_lease.lease_id AS active_lease_id,
		        active_lease.acquired_at AS active_lease_acquired_at,
		        active_lease.expires_at AS active_lease_expires_at,
		        active_lease.last_heartbeat_at AS active_lease_last_heartbeat_at
		   FROM striatumd.sessions s
		   LEFT JOIN striatumd.process_supervisors ps
		    ON ps.repository_id = s.repository_id
		    AND ps.session_id = s.session_id
		    AND ps.state = 'attached'
		   LEFT JOIN striatumd.process_supervisor_pointers ptr
		     ON ptr.repository_id = ps.repository_id
		    AND ptr.supervisor_id = ps.supervisor_id
		   LEFT JOIN LATERAL (
		     SELECT l.lease_id, l.acquired_at, l.expires_at, l.last_heartbeat_at
		       FROM striatumd.leases l
		      WHERE l.repository_id = s.repository_id
		        AND l.owner_session_id = s.session_id
		        AND l.state = 'active'
		      ORDER BY l.acquired_at DESC, l.lease_id DESC
		      LIMIT 1
		   ) active_lease ON true
		  WHERE s.repository_id = $1
		  ORDER BY s.registered_at, s.session_id`,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, session := range sessions {
		session["liveness"] = sessionliveness.ProjectionFromRow(session, now)
		sessionliveness.RemoveProjectionSourceFields(session)
		supervisorID := stringFrom(session, "supervisor_id")
		if supervisorID != "" && DrainHelperEventsHook != nil {
			if tx, err := beginHelperEventDrainTx(ctx, runner); err == nil {
				_ = DrainHelperEventsHook(ctx, tx, repositoryID, supervisorID)
				_ = tx.Commit(ctx)
				metaRows, err := collectRows(ctx, runner,
					`SELECT metadata_json FROM striatumd.process_supervisor_pointers
					  WHERE repository_id = $1 AND supervisor_id = $2`,
					repositoryID, supervisorID,
				)
				if err == nil && len(metaRows) > 0 {
					session["supervisor_metadata_json"] = metaRows[0]["metadata_json"]
				}
			}
		}
		metadata := superviseObject(session["supervisor_metadata_json"])
		attachSupervisorTmux(session, "supervisor_metadata_json")
		if stringFrom(session, "supervisor_id") == "" {
			session["lane_attestation"] = "unattested"
			session["lane_attestation_reason"] = "no_attached_supervisor"
			session["pid"] = nil
			session["lane_backend"] = "none"
			session["delivery_state"] = "unknown"
			continue
		}
		pid, _ := intValueOptional(session["pid"])
		_ = attachTmuxLivenessFromMetadata(ctx, session, metadata, pid, superviseString(session["pid_start_time"]))
		checker := lanehealth.Checker{
			Probe: lanehealth.ProdProbe{Runner: superviseTmuxRunner},
		}
		health, err := checker.Check(ctx, runner, repositoryID, superviseString(session["session_id"]))
		if err == nil {
			legMap := lanehealth.LegacyMap(health)
			session["lane_attestation"] = legMap["state"]
			session["lane_attestation_reason"] = legMap["reason"]
			reconcileBenignAttachExit(session, health)
		} else {
			session["lane_attestation"] = "unattested"
			session["lane_attestation_reason"] = "no_attached_supervisor"
		}
	}
	processHealth, err := dashboardAllProcessHealth(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"runs":                                 runs,
		"provenance_mode":                      nil,
		"sessions":                             sessions,
		"jobs":                                 jobs,
		"open_blockers":                        openBlockers,
		"human_checkpoints":                    humanCheckpoints,
		"latest_non_accepting_review_verdicts": nonAccepting,
		"verdicts_by_posture":                  verdictsByPosture,
		"claimable_jobs":                       claimable,
		"blocked_downstream_jobs":              blockedDownstream,
		"process_health":                       processHealth,
		"next_actions":                         statusNextActions(claimable, openBlockers, humanCheckpoints, nonAccepting, false, false, processHealth, map[string]any{"next_actions": []string{}}, nil),
	}, nil
}

func dashboardAllBlockers(ctx context.Context, runner db.Runner, repositoryID string, severity string) ([]map[string]any, error) {
	args := []any{repositoryID}
	where := "b.repository_id = $1 AND b.state = 'open'"
	if severity != "" {
		where += " AND b.severity = $2"
		args = append(args, severity)
	}
	return collectRows(ctx, runner,
		`SELECT b.blocker_id, b.run_id, b.job_id, b.session_id, b.severity,
		        b.blocker_kind, b.description, b.state,
		        j.workflow_job_id, j.state AS job_state
		   FROM striatumd.blockers b
		   LEFT JOIN striatumd.jobs j
		     ON j.repository_id = b.repository_id
		    AND j.job_id = b.job_id
		  WHERE `+where+`
		  ORDER BY b.created_at, b.blocker_id`,
		args...,
	)
}

func dashboardAllProcessHealth(ctx context.Context, runner db.Runner, repositoryID string) (map[string]any, error) {
	rows, err := collectRows(ctx, runner,
		`SELECT
		    COUNT(*) FILTER (WHERE p.state = 'running') AS running_count,
		    COUNT(*) FILTER (
		      WHERE p.state = 'running' AND l.state = 'expired'
		    ) AS stale_running_count,
		    COUNT(*) FILTER (WHERE p.state = 'lost') AS lost_count,
		    COUNT(*) FILTER (WHERE p.state = 'timed_out') AS timed_out_count
		   FROM striatumd.process_executions p
		   LEFT JOIN striatumd.leases l
		     ON l.repository_id = p.repository_id
		    AND l.lease_id = p.lease_id
		  WHERE p.repository_id = $1`,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	row := map[string]any{}
	if len(rows) > 0 {
		row = rows[0]
	}
	nextActions := []string{}
	if intFrom(row, "stale_running_count") > 0 {
		nextActions = append(nextActions, "recovery_process_reconcile")
	}
	return map[string]any{
		"running_count":       intFrom(row, "running_count"),
		"stale_running_count": intFrom(row, "stale_running_count"),
		"lost_count":          intFrom(row, "lost_count"),
		"timed_out_count":     intFrom(row, "timed_out_count"),
		"next_actions":        nextActions,
	}, nil
}

func dashboardAllStaleLeases(ctx context.Context, runner db.Runner, repositoryID string) ([]map[string]any, error) {
	runRows, err := collectRows(ctx, runner,
		`SELECT run_id
		   FROM striatumd.runs
		  WHERE repository_id = $1 AND state IN ('running', 'blocked')
		  ORDER BY created_at, run_id`,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(runRows))
	for _, run := range runRows {
		payload, err := dashboardAllStaleLeasesForRun(ctx, runner, repositoryID, stringFrom(run, "run_id"))
		if err != nil {
			return nil, err
		}
		out = append(out, payload)
	}
	return out, nil
}

func dashboardAllStaleLeasesForRun(ctx context.Context, runner db.Runner, repositoryID string, runID string) (map[string]any, error) {
	rows, err := collectRows(ctx, runner,
		`SELECT j.job_id, j.workflow_job_id, j.state AS job_state,
		        j.write_scope_json,
		        l.lease_id, l.owner_session_id, l.acquired_at,
		        l.expires_at, l.released_at, l.release_reason,
		        qm.message_id, qm.state AS message_state
		   FROM striatumd.jobs j
		   LEFT JOIN striatumd.leases l
		     ON l.repository_id = j.repository_id
		    AND (l.lease_id = j.current_lease_id
		         OR (l.resource_id = j.job_id
		             AND l.state = 'expired'
		             AND `+db.ExpiredLeaseStillStalePredicate+`))
		   LEFT JOIN striatumd.queue_messages qm
		     ON qm.repository_id = j.repository_id
		    AND qm.message_id = j.current_message_id
		  WHERE j.repository_id = $1
		    AND j.run_id = $2
		    AND (j.state = 'stale_lease'
		         OR (l.state = 'expired'
		             AND `+db.ExpiredLeaseStillStalePredicate+`
		             AND j.state IN ('claimed', 'running')))
		  ORDER BY j.workflow_job_id, l.expires_at`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	entries := []map[string]any{}
	seen := map[string]bool{}
	for _, row := range rows {
		key := stringFrom(row, "job_id") + "/" + stringFrom(row, "lease_id")
		if seen[key] {
			continue
		}
		seen[key] = true
		repoWrite := isRepoWriteScope(row["write_scope_json"])
		policy := "safe_to_reclaim_when_pending"
		actions := []string{"register_or_select_session", "claim_available_work"}
		if repoWrite {
			policy = "manual_inspection_required"
			actions = []string{"inspect_worktree_and_artifacts", "decide_requeue_or_cancel"}
		}
		entries = append(entries, map[string]any{
			"job_id":           row["job_id"],
			"workflow_job_id":  row["workflow_job_id"],
			"job_state":        row["job_state"],
			"lease_id":         row["lease_id"],
			"owner_session_id": row["owner_session_id"],
			"expires_at":       row["expires_at"],
			"released_at":      row["released_at"],
			"release_reason":   row["release_reason"],
			"message_id":       row["message_id"],
			"message_state":    row["message_state"],
			"repo_write":       repoWrite,
			"recovery_policy":  policy,
			"next_actions":     actions,
		})
	}
	nextActions := []string{}
	if len(entries) > 0 {
		nextActions = []string{"inspect_worktree_and_artifacts", "decide_requeue_or_cancel"}
	}
	return map[string]any{
		"run_id":       runID,
		"stale_count":  len(entries),
		"stale_leases": entries,
		"next_actions": nextActions,
	}, nil
}

func dashboardAllRunProgress(ctx context.Context, runner db.Runner, repositoryID string) ([]map[string]any, error) {
	rows, err := collectRows(ctx, runner,
		`SELECT r.run_id, r.state, r.paused_at, r.workflow_snapshot_id,
		        r.repo_root, w.workflow_json
		   FROM striatumd.runs r
		   JOIN striatumd.workflow_snapshots w
		     ON w.repository_id = r.repository_id
		    AND w.workflow_snapshot_id = r.workflow_snapshot_id
		  WHERE r.repository_id = $1
		    AND (r.state IN ('needs_branch_confirmation','ready','running','blocked')
		         OR r.paused_at IS NOT NULL)
		  ORDER BY r.created_at, r.run_id`,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		runID := stringFrom(row, "run_id")
		workflow := objectFromJSONish(row["workflow_json"])
		phase, err := dashboardAllPhaseProgress(ctx, runner, repositoryID, runID, workflow)
		if err != nil {
			return nil, err
		}
		autoFinalize, err := dashboardAllAutoFinalizeSummary(ctx, runner, repositoryID, runID, workflow)
		if err != nil {
			return nil, err
		}
		supervisorStalls, err := dashboardAllSupervisorStalls(ctx, runner, repositoryID, runID)
		if err != nil {
			return nil, err
		}
		item := map[string]any{
			"run_id":                runID,
			"state":                 row["state"],
			"current_phase_id":      phase["current_phase_id"],
			"phases":                phase["phases"],
			"auto_finalize_dry_run": autoFinalize,
			"supervisor_stalls":     supervisorStalls,
		}
		out = append(out, item)
	}
	return out, nil
}

func dashboardAllPhaseProgress(ctx context.Context, runner db.Runner, repositoryID, runID string, workflow map[string]any) (map[string]any, error) {
	rawPhases := anySlice(workflow["phases"])
	if len(rawPhases) == 0 {
		return map[string]any{"phases": []map[string]any{}, "current_phase_id": nil}, nil
	}
	jobs, err := collectRows(ctx, runner,
		`SELECT j.job_id, j.workflow_job_id, j.state, j.attempt
		   FROM striatumd.jobs j
		  WHERE j.repository_id = $1 AND j.run_id = $2
		  ORDER BY j.workflow_job_id, j.attempt`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	latestJobs := map[string]map[string]any{}
	for _, job := range jobs {
		workflowJobID := stringFrom(job, "workflow_job_id")
		if workflowJobID == "" {
			continue
		}
		current, ok := latestJobs[workflowJobID]
		if !ok || intFrom(job, "attempt") >= intFrom(current, "attempt") {
			latestJobs[workflowJobID] = job
		}
	}
	latestVerdicts, err := dashboardAllLatestVerdicts(ctx, runner, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	phaseOrder := []string{}
	phaseByID := map[string]map[string]any{}
	synthesisByPhase := map[string]string{}
	jobsByPhase := map[string][]string{}
	for _, raw := range rawPhases {
		phase := objectFromJSONish(raw)
		phaseID := stringValue(phase["id"])
		if phaseID == "" {
			continue
		}
		phaseOrder = append(phaseOrder, phaseID)
		phaseByID[phaseID] = phase
		if synthesis := stringValue(phase["synthesis_job_id"]); synthesis != "" {
			synthesisByPhase[phaseID] = synthesis
		}
		jobsByPhase[phaseID] = []string{}
	}
	for _, rawJob := range anySlice(workflow["jobs"]) {
		job := objectFromJSONish(rawJob)
		workflowJobID := stringValue(job["id"])
		if workflowJobID == "" {
			continue
		}
		phaseID := stringValue(job["phase_id"])
		if phaseID == "" {
			phaseID = stringValue(job["phase"])
		}
		if phaseID == "" {
			continue
		}
		jobsByPhase[phaseID] = append(jobsByPhase[phaseID], workflowJobID)
	}
	phases := make([]map[string]any, 0, len(phaseOrder))
	for index, phaseID := range phaseOrder {
		phase := phaseByID[phaseID]
		workflowJobIDs := jobsByPhase[phaseID]
		jobsByState := map[string]int{}
		jobStates := []string{}
		for _, workflowJobID := range workflowJobIDs {
			state := "pending"
			if row, ok := latestJobs[workflowJobID]; ok {
				state = stringFrom(row, "state")
				if state == "" {
					state = "pending"
				}
			}
			jobStates = append(jobStates, state)
			jobsByState[state]++
		}
		synthesisJobID := synthesisByPhase[phaseID]
		var synthesisState any
		var synthesisVerdict any
		if synthesisRow, ok := latestJobs[synthesisJobID]; ok {
			synthesisState = synthesisRow["state"]
			synthesisVerdict = latestVerdicts[stringFrom(synthesisRow, "job_id")]
		}
		payload := map[string]any{
			"id":                phaseID,
			"name":              defaultString(phase["name"], phaseID),
			"index":             index,
			"state":             dashboardAllPhaseState(jobStates),
			"jobs_total":        len(workflowJobIDs),
			"jobs_completed":    jobsByState["completed"],
			"jobs_by_state":     jobsByState,
			"synthesis_job_id":  nullableStringValue(synthesisJobID),
			"synthesis_state":   synthesisState,
			"synthesis_verdict": synthesisVerdict,
		}
		for _, key := range []string{"description", "color"} {
			if value := stringValue(phase[key]); value != "" {
				payload[key] = value
			}
		}
		phases = append(phases, payload)
	}
	if len(phases) == 0 {
		return map[string]any{"phases": []map[string]any{}, "current_phase_id": nil}, nil
	}
	currentPhaseID := phases[len(phases)-1]["id"]
	for _, phase := range phases {
		if phase["state"] != "completed" {
			currentPhaseID = phase["id"]
			break
		}
	}
	return map[string]any{"phases": phases, "current_phase_id": currentPhaseID}, nil
}

func dashboardAllLatestVerdicts(ctx context.Context, runner db.Runner, repositoryID, runID string) (map[string]map[string]any, error) {
	rows, err := collectRows(ctx, runner,
		`SELECT DISTINCT ON (v.job_id)
		        v.job_id, v.verdict_id, v.verdict, v.posture, v.created_at
		   FROM striatumd.verdicts v
		  WHERE v.repository_id = $1 AND v.run_id = $2
		  ORDER BY v.job_id, v.created_at DESC, v.verdict_id DESC`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, row := range rows {
		out[stringFrom(row, "job_id")] = row
	}
	return out, nil
}

func dashboardAllPhaseState(states []string) string {
	if len(states) > 0 {
		allCompleted := true
		for _, state := range states {
			if state != "completed" {
				allCompleted = false
				break
			}
		}
		if allCompleted {
			return "completed"
		}
	}
	for _, state := range states {
		if state == "queued" || state == "claimed" || state == "running" || state == "stale_lease" || state == "waiting_human" || state == "blocked" {
			return "active"
		}
	}
	for _, state := range states {
		if state == "failed" {
			return "failed"
		}
	}
	incomplete := []string{}
	for _, state := range states {
		if state != "completed" {
			incomplete = append(incomplete, state)
		}
	}
	if len(incomplete) > 0 {
		allCanceled := true
		for _, state := range incomplete {
			if state != "canceled" && state != "skipped" {
				allCanceled = false
				break
			}
		}
		if allCanceled {
			return "canceled"
		}
	}
	return "pending"
}

func dashboardAllAutoFinalizeSummary(ctx context.Context, runner db.Runner, repositoryID, runID string, workflow map[string]any) (map[string]any, error) {
	rows, err := collectRows(ctx, runner,
		`SELECT COUNT(*) AS candidate_count
		   FROM striatumd.jobs j
		   JOIN striatumd.leases l
		     ON l.repository_id = j.repository_id
		    AND l.lease_id = j.current_lease_id
		   JOIN striatumd.sessions s
		     ON s.repository_id = j.repository_id
		    AND s.session_id = l.owner_session_id
		   LEFT JOIN striatumd.queue_messages qm
		     ON qm.repository_id = j.repository_id
		    AND qm.message_id = j.current_message_id
		  WHERE j.repository_id = $1
		    AND j.run_id = $2
		    AND j.state IN ('claimed', 'running')
		    AND l.state = 'active'
		    AND l.expires_at >= now()
		    AND s.state = 'active'
		    AND (qm.message_id IS NULL OR qm.state IN ('claimed', 'acked'))`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	candidateCount := 0
	if len(rows) > 0 {
		candidateCount = intFrom(rows[0], "candidate_count")
	}
	return map[string]any{
		"run_id":                    runID,
		"dry_run":                   true,
		"projection":                "dashboard_all_sql_summary",
		"policy":                    dashboardAllAutoFinalizePolicy(workflow),
		"candidate_count":           candidateCount,
		"lane_finalization_summary": dashboardAllLaneFinalizationSummary(0, 0, candidateCount),
	}, nil
}

func dashboardAllLaneFinalizationSummary(autoFromArtifact, manualPublish, pending int) map[string]any {
	return map[string]any{
		"auto_from_artifact": autoFromArtifact,
		"manual_publish":     manualPublish,
		"pending":            pending,
	}
}

func dashboardAllAutoFinalizePolicy(workflow map[string]any) map[string]any {
	policy := workflow["auto_finalize"]
	if recovery := objectFromJSONish(workflow["recovery"]); len(recovery) > 0 {
		if value, ok := recovery["auto_finalize"]; ok {
			policy = value
		}
	}
	enabled, configured, optOut := dashboardAllAutoFinalizePolicyState(policy)
	return map[string]any{
		"workflow_enabled":           enabled,
		"workflow_configured":        configured,
		"workflow_opt_out":           optOut,
		"force":                      false,
		"live_allowed":               enabled,
		"global_default_mode":        "live",
		"default_live_gate":          dashboardAllAutoFinalizeDefaultLiveGate(),
		"mtime_grace_seconds":        30,
		"allow_no_process_execution": false,
	}
}

func dashboardAllAutoFinalizePolicyState(policy any) (enabled bool, configured bool, optOut bool) {
	configured = policy != nil
	switch typed := policy.(type) {
	case bool:
		optOut = !typed
	case map[string]any:
		optOut = typed["enabled"] == false
	default:
		if policyMap := objectFromJSONish(policy); len(policyMap) > 0 {
			optOut = policyMap["enabled"] == false
		}
	}
	return !optOut, configured, optOut
}

func dashboardAllAutoFinalizeDefaultLiveGate() map[string]any {
	return map[string]any{
		"decision_id":                      "D125",
		"status":                           "satisfied",
		"required_live_successes":          3,
		"required_lane_shapes":             2,
		"max_contested_audit_chain_events": 0,
		"evidence_artifact_kind":           "auto_finalize_gate_evidence",
		"live_default_enabled":             true,
		"enabled_by_decision_id":           "D133",
	}
}

func dashboardAllSupervisorStalls(ctx context.Context, runner db.Runner, repositoryID, runID string) (map[string]any, error) {
	// #291: the leases join is LEFT (not INNER) and the job-state filter includes
	// 'queued' so an active-but-LEASELESS bound supervised session — a hung lane
	// that never claimed its packet, or a dead tmux pane still marked active —
	// surfaces instead of reporting stalled_count:0 forever. The leaseless job is
	// bound to the supervisor's session by the SAME role+lane eligibility the claim
	// path uses (so the projection and the claim path agree on which job the session
	// would have taken), and only its still-pending work message counts (a genuinely
	// claimable job). The original leased claimed/running path is unchanged: those
	// rows still resolve their job via the active lease and stall on lease age.
	where := `ps.repository_id = $1
		    AND ps.state = 'attached'
		    AND s.state = 'active'
		    AND (
		      (l.state = 'active' AND l.resource_type = 'job'
		         AND j.state IN ('claimed', 'running')
		         AND j.current_lease_id = l.lease_id)
		      OR (l.lease_id IS NULL AND j.state = 'queued')
		    )`
	args := []any{repositoryID}
	if runID != "" {
		where += " AND ps.run_id = $2"
		args = append(args, runID)
	}
	rows, err := collectRows(ctx, runner,
		`SELECT ps.supervisor_id, ps.run_id, ps.session_id, ps.pid,
		        ps.state AS supervisor_state,
		        COALESCE(ptr.updated_at, ps.heartbeat_at) AS supervisor_heartbeat_at,
		        s.last_heartbeat_at AS session_last_heartbeat_at,
		        l.lease_id, COALESCE(l.resource_id, j.job_id) AS job_id, l.acquired_at,
		        l.expires_at, l.last_heartbeat_at AS lease_last_heartbeat_at,
		        j.workflow_job_id, j.state AS job_state,
		        j.current_message_id AS message_id,
		        qm.state AS message_state,
		        (l.lease_id IS NULL) AS leaseless
		   FROM striatumd.process_supervisors ps
		   JOIN striatumd.sessions s
		     ON s.repository_id = ps.repository_id
		    AND s.session_id = ps.session_id
		   LEFT JOIN striatumd.leases l
		     ON l.repository_id = ps.repository_id
		    AND l.run_id = ps.run_id
		    AND l.owner_session_id = ps.session_id
		    AND l.state = 'active'
		    AND l.resource_type = 'job'
		   LEFT JOIN striatumd.jobs j
		     ON j.repository_id = ps.repository_id
		    AND (
		      (l.lease_id IS NOT NULL AND j.job_id = l.resource_id
		         AND j.current_lease_id = l.lease_id)
		      OR (l.lease_id IS NULL
		         AND j.run_id = ps.run_id
		         AND j.state = 'queued'
		         AND j.role_id = s.role_id
		         AND (
		           NULLIF(j.lane_selector_json->>'lane_id','') IS NULL
		           OR j.lane_selector_json->>'lane_id' = s.lane_id
		         )
		         AND EXISTS (
		           SELECT 1 FROM striatumd.queue_messages wm
		            WHERE wm.repository_id = j.repository_id
		              AND wm.message_id = j.current_message_id
		              AND wm.kind = 'work'
		              AND wm.state = 'pending'
		         ))
		    )
		   LEFT JOIN striatumd.queue_messages qm
		     ON qm.repository_id = j.repository_id
		    AND qm.message_id = j.current_message_id
		   LEFT JOIN striatumd.process_supervisor_pointers ptr
		     ON ptr.repository_id = ps.repository_id
		    AND ptr.supervisor_id = ps.supervisor_id
		  WHERE `+where+`
		  ORDER BY ps.started_at DESC, ps.supervisor_id DESC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	supervisors := []map[string]any{}
	expiredCount := 0
	warningCount := 0
	leaselessCount := 0
	for _, row := range rows {
		leaseless := row["leaseless"] == true
		var progress map[string]any
		stalled := false
		if leaseless {
			// A leaseless bound session has no lease clock; its progress is the
			// freshest of the session/supervisor heartbeats. Flag it stalled once
			// that progress is older than the stall threshold — a freshly spawned
			// session that has simply not claimed yet stays within the window and is
			// NOT flagged, so a healthy queued job is never reported.
			progress = enrichSupervisorProgress(row, now, defaultSupervisorStallAfterSeconds)
			if age, ok := progress["last_progress_age_seconds"].(int); ok && age >= defaultSupervisorStallAfterSeconds {
				stalled = true
			}
		} else {
			progress = enrichSupervisorProgress(row, now, defaultSupervisorStallAfterSeconds)
			stalled = progress["stalled"] == true
		}
		if !stalled {
			continue
		}
		if leaseless {
			leaselessCount++
			warningCount++
		} else if progress["lease_expired"] == true {
			expiredCount++
		} else {
			warningCount++
		}
		supervisors = append(supervisors, map[string]any{
			"supervisor_id":             progress["supervisor_id"],
			"run_id":                    progress["run_id"],
			"session_id":                progress["session_id"],
			"job_id":                    progress["job_id"],
			"workflow_job_id":           progress["workflow_job_id"],
			"lease_id":                  progress["lease_id"],
			"message_id":                progress["message_id"],
			"last_progress_at":          progress["last_progress_at"],
			"last_progress_age_seconds": progress["last_progress_age_seconds"],
			"lease_expires_at":          progress["lease_expires_at"],
			"lease_expired":             progress["lease_expired"],
			"leaseless":                 leaseless,
		})
	}
	nextActions := []string{}
	if len(supervisors) > 0 {
		nextActions = []string{"supervisor_stall_investigate"}
	}
	return map[string]any{
		"stalled_count":       len(supervisors),
		"warning_count":       warningCount,
		"expired_count":       expiredCount,
		"leaseless_count":     leaselessCount,
		"stall_after_seconds": defaultSupervisorStallAfterSeconds,
		"supervisors":         supervisors,
		"next_actions":        nextActions,
	}, nil
}

func isRepoWriteScope(value any) bool {
	scope := map[string]any{}
	switch v := value.(type) {
	case map[string]any:
		scope = v
	case []byte:
		_ = json.Unmarshal(v, &scope)
	case string:
		_ = json.Unmarshal([]byte(v), &scope)
	default:
		return false
	}
	if mode, _ := scope["mode"].(string); mode == "repo_write" {
		return true
	}
	flag, _ := scope["repo_write"].(bool)
	return flag
}
