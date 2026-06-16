package mutations

import (
	"context"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/db"
)

// recoveryExhaustedBlockerKind is the daemon-authored blocker kind for a job the
// autonomous recovery loop could not reclaim within its per-job budget. It is
// added to validBlockerKind (it already matches the ^[a-z0-9._-]$ shape),
// isEscalation, and isEscalationClassBlocker / escalationPredicate so it
// validates and is treated as an escalation everywhere (RFC 0062).
const recoveryExhaustedBlockerKind = "recovery_exhausted"

// escalateExhaustedJobs is the RFC 0101 Phase 4 run-health pass. It runs INSIDE
// HandleRecoveryAuto's existing withTx, AFTER recoverStuckJobs (the Phase 3
// decision tree that sets escalation_pending) and is skipped on dry_run.
//
// For each job_recovery_state row in this run with escalation_pending=true AND
// run_escalated_at IS NULL it:
//   - creates a blockers row (state 'open', kind 'recovery_exhausted',
//     severity 'blocked') and an escalation_inbox row (state 'pending', SAME id)
//     carrying a STRUCTURED payload_json (the daemon authors no markdown
//     artifact — this payload IS the escalation, RFC 0062),
//   - stamps run_escalated_at=now() on the budget row (idempotency guard),
//   - appends a run.escalated event naming the job + blocker.
//
// After processing all such rows, if at least one escalation was raised AND the
// run is still 'running', it flips the run to 'needs_operator' and appends a
// run.needs_operator event so the run never sits silently 'running'. Once
// needs_operator, the sweep's running/paused active-run filter (recovery/
// sweep.go) excludes the run, so this never re-runs; the run_escalated_at guard
// is belt-and-suspenders for the same pass and any concurrent re-entry.
//
// It is idempotent + convergent: a budget row whose run_escalated_at is already
// set is skipped, so re-running raises no duplicate blocker/escalation rows.
func escalateExhaustedJobs(ctx context.Context, tx db.TxRunner, repositoryID, runID string) ([]map[string]any, error) {
	// The job's lane is read straight off jobs.lane_selector_json->>'lane_id'
	// (the canonical lane-name source — same projection runreconcile,
	// dashboard_all, and the recovery decision tree use). NULLIF coerces an
	// empty / missing selector to SQL NULL so an unresolvable lane degrades to
	// "" downstream rather than erroring (#311 carve-out).
	rows, err := queryRows(ctx, tx, `
		SELECT jrs.job_id, jrs.requeue_count, jrs.transfer_count, jrs.respawn_count,
		       jrs.last_recovery_action, jrs.last_stall_class,
		       j.workflow_job_id,
		       NULLIF(j.lane_selector_json->>'lane_id','') AS lane
		  FROM striatumd.job_recovery_state jrs
		  LEFT JOIN striatumd.jobs j
		    ON j.repository_id = jrs.repository_id AND j.job_id = jrs.job_id
		 WHERE jrs.repository_id = $1
		   AND jrs.run_id = $2
		   AND jrs.escalation_pending = true
		   AND jrs.run_escalated_at IS NULL
		 ORDER BY jrs.job_id
		 FOR UPDATE OF jrs`, repositoryID, runID)
	if err != nil {
		return nil, err
	}

	raised := []map[string]any{}
	for _, row := range rows {
		jobID := fmt.Sprint(row["job_id"])
		workflowJobID := fmt.Sprint(nullable(row["workflow_job_id"]))
		// lane may be unresolvable (NULL selector, job row missing) — degrade to
		// "" and omit it everywhere rather than erroring (#311 carve-out).
		lane := ""
		if v := nullable(row["lane"]); v != nil {
			lane = fmt.Sprint(v)
		}
		stallClass := fmt.Sprint(nullable(row["last_stall_class"]))
		lastAction := fmt.Sprint(nullable(row["last_recovery_action"]))
		requeueCount := intFromAny(row["requeue_count"], 0)
		transferCount := intFromAny(row["transfer_count"], 0)
		respawnCount := intFromAny(row["respawn_count"], 0)
		recoveryAttempts := requeueCount + transferCount + respawnCount

		blockerID, err := newID("blk")
		if err != nil {
			return nil, err
		}
		now := nowString()

		laneClause := ""
		if lane != "" {
			laneClause = fmt.Sprintf(" lane=%s", lane)
		}
		description := fmt.Sprintf(
			"autonomous recovery exhausted for job %s (%s):%s stall_class=%s, last_recovery_action=%s, recovery_attempts=%d; operator action required",
			workflowJobID, jobID, laneClause, stallClass, lastAction, recoveryAttempts,
		)

		// The structured escalation payload IS the operator-actionable artifact
		// (RFC 0062 — the daemon does not author markdown). It names the stuck
		// job, the stall class, the recovery counters, and concrete next actions.
		payload := map[string]any{
			"schema_version":             "striatum.recovery_escalation.v1",
			"source":                     "recovery.escalate_exhausted",
			"is_escalation":              true,
			"blocker_kind":               recoveryExhaustedBlockerKind,
			"severity":                   "blocked",
			"stuck_job":                  workflowJobID,
			"job_id":                     jobID,
			"stall_class":                stallClass,
			"last_recovery_action":       lastAction,
			"requeue_count":              requeueCount,
			"transfer_count":             transferCount,
			"respawn_count":              respawnCount,
			"recovery_attempts":          recoveryAttempts,
			"suggested_operator_actions": suggestedOperatorActions(stallClass),
		}
		// Name the offending lane in the structured escalation when resolvable
		// (#311 carve-out). Omitted entirely if the lane could not be resolved.
		if lane != "" {
			payload["lane"] = lane
		}
		payloadArg, err := db.JSONBArg(tx, payload)
		if err != nil {
			return nil, err
		}

		if err := tx.Exec(ctx, `
			INSERT INTO striatumd.blockers (
			  repository_id, blocker_id, run_id, job_id, session_id, severity,
			  blocker_kind, description, state, created_at, payload_json
			)
			VALUES ($1,$2,$3,$4,NULL,'blocked',$5,$6,'open',$7,$8::jsonb)`,
			repositoryID, blockerID, runID, jobID,
			recoveryExhaustedBlockerKind, description, now, payloadArg,
		); err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			INSERT INTO striatumd.escalation_inbox (
			  repository_id, escalation_id, run_id, job_id, session_id,
			  blocker_id, blocker_kind, severity, state, created_at, payload_json
			)
			VALUES ($1,$2,$3,$4,NULL,$5,$6,'blocked','pending',$7,$8::jsonb)`,
			repositoryID, blockerID, runID, jobID,
			blockerID, recoveryExhaustedBlockerKind, now, payloadArg,
		); err != nil {
			return nil, err
		}

		// Idempotency guard: stamp run_escalated_at so a re-run does not duplicate
		// the escalation even if the run somehow remains sweepable.
		if err := tx.Exec(ctx, `
			UPDATE striatumd.job_recovery_state
			   SET run_escalated_at = $1, updated_at = $1
			 WHERE repository_id = $2 AND job_id = $3`,
			now, repositoryID, jobID); err != nil {
			return nil, err
		}

		escalatedEvent := map[string]any{
			"workflow_job_id":   workflowJobID,
			"job_id":            jobID,
			"blocker_id":        blockerID,
			"blocker_kind":      recoveryExhaustedBlockerKind,
			"stall_class":       stallClass,
			"recovery_attempts": recoveryAttempts,
		}
		if lane != "" {
			escalatedEvent["lane"] = lane
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "run.escalated", nil, jobID, nil, nil, nil, escalatedEvent); err != nil {
			return nil, err
		}

		raised = append(raised, map[string]any{
			"workflow_job_id":   workflowJobID,
			"job_id":            jobID,
			"lane":              lane,
			"blocker_id":        blockerID,
			"blocker_kind":      recoveryExhaustedBlockerKind,
			"stall_class":       stallClass,
			"recovery_attempts": recoveryAttempts,
		})
	}

	if len(raised) == 0 {
		return raised, nil
	}

	// Flip the run to needs_operator (guarded on state='running' so a
	// concurrent transition cannot clobber a terminal/already-escalated run).
	// striatumd.runs has no updated_at column (see 0005_repo_local_workflow_state),
	// so the state flip is the only mutation here — matching HandleRunCancel.
	if err := tx.Exec(ctx, `
		UPDATE striatumd.runs
		   SET state = 'needs_operator'
		 WHERE repository_id = $1 AND run_id = $2 AND state = 'running'`,
		repositoryID, runID); err != nil {
		return nil, err
	}
	if _, err := appendEvent(ctx, tx, repositoryID, runID, "run.needs_operator", nil, nil, nil, nil, nil, map[string]any{
		// KEEP the stable "recovery_exhausted" reason code: existing consumers /
		// tests match it. ADD a structured stuck_jobs array so the offending
		// job + lane is legible in the durable event chain (#311 carve-out).
		"reason":            "recovery_exhausted",
		"escalated_job_ids": escalatedJobIDs(raised),
		"escalation_count":  len(raised),
		"stuck_jobs":        stuckJobs(raised),
	}); err != nil {
		return nil, err
	}
	return raised, nil
}

// stuckJobs projects the raised escalations into the run.needs_operator event's
// structured legibility array — one {workflow_job_id, lane, stall_class} object
// per escalated job, so the offending job + lane is named in the durable event
// chain (not just the bare "recovery_exhausted" reason). lane is "" when it
// could not be resolved (#311 carve-out).
func stuckJobs(raised []map[string]any) []any {
	out := make([]any, 0, len(raised))
	for _, r := range raised {
		out = append(out, map[string]any{
			"workflow_job_id": r["workflow_job_id"],
			"lane":            r["lane"],
			"stall_class":     r["stall_class"],
		})
	}
	return out
}

func escalatedJobIDs(raised []map[string]any) []any {
	ids := make([]any, 0, len(raised))
	for _, r := range raised {
		ids = append(ids, r["job_id"])
	}
	return ids
}

// suggestedOperatorActions returns the operator-actionable next steps for an
// exhausted-recovery escalation, specialized by stall class. The unsealed-exit
// class (#289) gets distinct advice because the deliverable may already be
// present in the per-job worktree — the agent produced it but never sealed it —
// so the operator should inspect before assuming the work is lost. Every other
// class (including an empty class) gets the generic remediation set.
func suggestedOperatorActions(stallClass string) []any {
	if stallClass == stallClassAgentExitedUnsealed {
		return []any{
			"inspect the per-job worktree / published artifacts: the agent emitted output but exited before work.complete, so the work may be complete-but-unsealed",
			"re-drive the run to respawn the lane so it can re-author and seal (work.complete) the deliverable",
			"capture the worktree diff if the deliverable is already present, then cancel the run",
		}
	}
	return []any{
		"re-prepare with corrected write_scope",
		"transfer to a fresh session (session close --requeue-job / recovery requeue-stale --force)",
		"cancel the run",
	}
}
