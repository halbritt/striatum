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
//
// The first returned slice preserves the existing public "raised" semantics:
// jobs that force the whole run to needs_operator. The second returned slice is
// every newly-created escalation row that should wake an opt-in notifier after
// the transaction commits, including quarantined-job escalations.
func escalateExhaustedJobs(ctx context.Context, tx db.TxRunner, repositoryID, runID string, policy recoveryPolicy) ([]map[string]any, []map[string]any, error) {
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
		return nil, nil, err
	}

	// #311 P0: count jobs already quarantined in this run so the
	// max_quarantinable_jobs cap holds across sweeps (a prior sweep may have
	// quarantined a different job). This is the running total the per-job
	// eligibility check increments as it quarantines within this pass.
	quarantinedSoFar, err := countQuarantinedJobs(ctx, tx, repositoryID, runID)
	if err != nil {
		return nil, nil, err
	}
	// #311 P0 deployment-safety: probe ONCE whether the 'quarantined' job state is
	// permitted by the live jobs_state_check (owner bundle 0012 applied). On a
	// deployment behind on owner bundles this is false and quarantine is disabled
	// for the whole pass — every exhaustion falls back to the unchanged whole-run
	// needs_operator escalation rather than crashing the sweep on a CHECK
	// violation.
	quarantinePermitted, err := jobQuarantineStatePermitted(ctx, tx)
	if err != nil {
		return nil, nil, err
	}

	raised := []map[string]any{}
	notify := []map[string]any{}
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

		// #311 P0: BEFORE flipping the whole run, decide quarantine-eligibility for
		// THIS exhausted job. It is eligible to quarantine (finalize-the-majority)
		// only when ALL of: (a) no unfinished job transitively depends on it,
		// (b) it is not a provenance-required reviewer the RFC 0118 run-completion
		// gate would refuse, and (c) the per-run quarantine cap is not exceeded.
		// When eligible we quarantine ONLY this job (never seal/complete it),
		// record the blocker + escalation on it, and let the run finalize on its
		// completed deliverables — instead of flipping the entire run to
		// needs_operator and discarding the durable work of every sibling.
		eligible, ineligibleReason, qerr := quarantineEligible(ctx, tx, repositoryID, runID, jobID, policy, quarantinedSoFar, quarantinePermitted)
		if qerr != nil {
			return nil, nil, qerr
		}
		if eligible {
			notification, err := quarantineExhaustedJob(ctx, tx, repositoryID, runID, row, lane, stallClass, lastAction, requeueCount, transferCount, respawnCount)
			if err != nil {
				return nil, nil, err
			}
			notify = append(notify, notification)
			quarantinedSoFar++
			continue
		}
		_ = ineligibleReason // recorded on the run.needs_operator event below via the raised set

		blockerID, err := newID("blk")
		if err != nil {
			return nil, nil, err
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
			return nil, nil, err
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
			return nil, nil, err
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
			return nil, nil, err
		}

		// Idempotency guard: stamp run_escalated_at so a re-run does not duplicate
		// the escalation even if the run somehow remains sweepable.
		if err := tx.Exec(ctx, `
			UPDATE striatumd.job_recovery_state
			   SET run_escalated_at = $1, updated_at = $1
			 WHERE repository_id = $2 AND job_id = $3`,
			now, repositoryID, jobID); err != nil {
			return nil, nil, err
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
			return nil, nil, err
		}

		notification := map[string]any{
			"workflow_job_id":   workflowJobID,
			"job_id":            jobID,
			"lane":              lane,
			"blocker_id":        blockerID,
			"blocker_kind":      recoveryExhaustedBlockerKind,
			"stall_class":       stallClass,
			"recovery_attempts": recoveryAttempts,
		}
		raised = append(raised, notification)
		notify = append(notify, notification)
	}

	if len(raised) == 0 {
		// #311 P0: no job needed a whole-run escalation — either nothing was
		// exhausted, or every exhausted job was quarantine-eligible and set aside.
		// In the quarantine case the run can now finalize-the-majority on its
		// completed deliverables, so re-check completion (maybeCompleteRun excludes
		// 'quarantined' from the non-terminal set and records the manifest).
		if err := maybeCompleteRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, nil, err
		}
		return raised, notify, nil
	}

	// Flip the run to needs_operator (guarded on state='running' so a
	// concurrent transition cannot clobber a terminal/already-escalated run).
	// striatumd.runs has no updated_at column (see 0005_repo_local_workflow_state),
	// so the state flip is the only mutation here — matching HandleRunCancel.
	// A job that was quarantined this pass is NOT in `raised`, so a run with one
	// quarantined job and one genuinely-needed escalation still escalates (the
	// needed job keeps the run frozen) while the manifest still records the
	// quarantined one at the eventual completion.
	if err := tx.Exec(ctx, `
		UPDATE striatumd.runs
		   SET state = 'needs_operator'
		 WHERE repository_id = $1 AND run_id = $2 AND state = 'running'`,
		repositoryID, runID); err != nil {
		return nil, nil, err
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
		return nil, nil, err
	}
	return raised, notify, nil
}

// redispatchRecoveryExhaustedJob is the #388 fix, invoked by escalation.resolve
// (via reads.RecoveryExhaustedRedispatchHook) when an operator resolves a
// recovery_exhausted escalation. The repro: an agent_exited_unsealed job ran out
// of requeue budget, the daemon escalated recovery_exhausted, the driver exited,
// and after `escalation resolve` the job was left in 'running' with a dead session
// and a SPENT budget — so retry-job rejected the running state, complete-stalled
// had nothing durable, and the re-armed sweep simply re-escalated (the budget was
// never reset). A permanent wedge that doctor never flagged.
//
// This resets a sessionless running/claimed/stale_lease job back to queued
// (releasing its lease and re-pending its work message via requeueJobSameAttempt)
// AND clears its job_recovery_state — escalation_pending=false, requeue_count=0,
// transfer_count=0, respawn_count=0, run_escalated_at=NULL — so the re-armed run /
// next sweep re-dispatches it with a FRESH budget instead of re-escalating. It is
// a guarded no-op (returns false) when the job:
//   - already completed / was quarantined / is otherwise terminal (nothing to
//     re-dispatch), or
//   - still has a LIVE owning session (active lease + active session) — the lane
//     is alive, so escalation.resolve must not yank the job out from under it.
//
// Returns whether it actually re-dispatched the job so the caller can suppress
// the run-completion re-drive (the run now has a live queued job to finish).
func redispatchRecoveryExhaustedJob(ctx context.Context, tx db.TxRunner, repositoryID, runID, jobID string) (bool, error) {
	job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, err
	}
	state := fmt.Sprint(job["state"])
	switch state {
	case "running", "claimed", "stale_lease":
		// re-dispatchable states
	default:
		// queued (already re-dispatched), waiting_human/blocked (a human gate),
		// completed/failed/canceled/quarantined (terminal): leave untouched.
		return false, nil
	}

	// Refuse to yank a job out from under a genuinely-live lane: an active lease
	// whose owning session is still active means the lane is alive (mirrors the
	// complete-stalled liveness guard). A spent-budget wedge has a DEAD session, so
	// this only blocks the harmless case.
	if leaseID := nullable(job["current_lease_id"]); leaseID != nil {
		live, err := existsRow(ctx, tx, `
			SELECT 1
			  FROM striatumd.leases l
			  JOIN striatumd.sessions s
			    ON s.repository_id = l.repository_id
			   AND s.session_id = l.owner_session_id
			 WHERE l.repository_id = $1 AND l.lease_id = $2
			   AND l.state = 'active' AND l.expires_at > $3::timestamptz
			   AND s.state = 'active'
			 LIMIT 1`, repositoryID, leaseID, nowString())
		if err != nil {
			return false, err
		}
		if live {
			return false, nil
		}
	}

	// Reset the job to queued + re-pend its work message on the SAME attempt. The
	// repo-write override matches the autonomous recovery decision tree: the
	// operator's explicit escalation resolve IS the inspection D036 requires.
	opts := requeueSameAttemptOptions{
		author:        recoveryDecisionAuthor,
		justification: "escalation resolve (#388): re-dispatched budget-exhausted sessionless job with a fresh recovery budget",
	}
	if isRepoWrite(job) {
		opts.operatorOverride = true
	}
	if _, err := requeueJobSameAttempt(ctx, tx, repositoryID, job, opts); err != nil {
		return false, err
	}

	// Clear the recovery budget so the re-armed run/sweep re-dispatches with a
	// fresh budget instead of immediately re-escalating. Resetting run_escalated_at
	// to NULL lets a genuinely-recurring stall re-escalate later (after a fresh full
	// budget), rather than being silently swallowed.
	now := nowString()
	if err := tx.Exec(ctx, `
		UPDATE striatumd.job_recovery_state
		   SET escalation_pending = false,
		       escalated_at = NULL,
		       run_escalated_at = NULL,
		       requeue_count = 0,
		       transfer_count = 0,
		       respawn_count = 0,
		       last_recovery_action = 'escalation_resolved_redispatch',
		       last_recovery_at = $3,
		       updated_at = $3
		 WHERE repository_id = $1 AND job_id = $2`, repositoryID, jobID, now); err != nil {
		return false, err
	}

	if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.redispatched_after_escalation_resolve", nil, jobID, nil, nil, nil, map[string]any{
		"job_id":     jobID,
		"from_state": state,
		"reason":     "recovery_exhausted_resolved",
	}); err != nil {
		return false, err
	}
	return true, nil
}

// jobQuarantineStatePermitted reports whether the live striatumd.jobs
// jobs_state_check CHECK constraint permits the 'quarantined' state — i.e. owner
// bundle 0012 has been applied (#311 P0). It probes pg_get_constraintdef rather
// than attempting the UPDATE, so a behind-deployment is detected without raising
// a CHECK violation that would abort the sweep transaction. A missing constraint
// (renamed/dropped out of band) is treated as NOT permitted (fail-safe: prefer
// the proven escalation path over an unguarded write).
func jobQuarantineStatePermitted(ctx context.Context, tx db.TxRunner) (bool, error) {
	row, err := oneRow(ctx, tx, `
		SELECT COALESCE(bool_or(pg_get_constraintdef(oid) LIKE '%quarantined%'), false) AS ok
		  FROM pg_constraint
		 WHERE conname = 'jobs_state_check'
		   AND conrelid = 'striatumd.jobs'::regclass`)
	if err != nil {
		return false, err
	}
	return row["ok"] == true, nil
}

// countQuarantinedJobs returns the number of jobs currently in the 'quarantined'
// state for a run — the running total the max_quarantinable_jobs cap is checked
// against (a prior sweep may have quarantined a different job, #311 P0).
func countQuarantinedJobs(ctx context.Context, tx db.TxRunner, repositoryID, runID string) (int, error) {
	row, err := oneRow(ctx, tx, `
		SELECT COUNT(*)::int AS n FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2 AND state = 'quarantined'`,
		repositoryID, runID)
	if err != nil {
		return 0, err
	}
	return intFromAny(row["n"], 0), nil
}

// quarantineEligible decides whether a single recovery-exhausted job may be
// quarantined so the run finalizes-the-majority (#311 P0), instead of flipping
// the whole run to needs_operator. ALL three load-bearing guards must hold:
//
//	(a) NO unfinished job transitively (multi-hop) depends on the exhausted job —
//	    a non-terminal reachable dependent is a hard dependency the deliverables
//	    cannot finalize without.
//	(b) The exhausted job is NOT a provenance-required reviewer whose absence
//	    RFC 0118's verifyRunCompletionProvenance would refuse — i.e. completing
//	    the run would not be blocked by this job's verdict gate.
//	(c) Quarantining this job would not exceed the per-run cap
//	    (policy.maxQuarantinableJobs, default 1).
//
// If any guard fails the job is NOT eligible and the caller falls through to the
// unchanged whole-run needs_operator escalation. The returned reason names the
// first failing guard for legibility.
func quarantineEligible(ctx context.Context, tx db.TxRunner, repositoryID, runID, jobID string, policy recoveryPolicy, quarantinedSoFar int, quarantinePermitted bool) (bool, string, error) {
	// (d) deployment-safety: the 'quarantined' job state must be permitted by the
	// live jobs_state_check (owner bundle 0012 applied). On a deployment behind on
	// owner bundles the state cannot persist — the UPDATE would raise a CHECK
	// violation and crash the whole sweep transaction — so quarantine is DISABLED
	// and we fall back to the unchanged whole-run needs_operator escalation. This
	// mirrors the artifact-placement / review-generation adaptive column pattern
	// (a feature gated on its owner-bundle DDL having landed). Checked first so a
	// behind-deployment never even evaluates the other guards.
	if !quarantinePermitted {
		return false, "quarantine_state_unavailable", nil
	}
	// (c) cap. A run already at the cap escalates the rest rather than silently
	// swallowing large-scale failure.
	if quarantinedSoFar >= policy.maxQuarantinableJobs {
		return false, "quarantine_cap_exceeded", nil
	}
	// (a) transitive downstream dependency: walk job_dependencies downstream from
	// the exhausted job; if ANY reachable job is non-terminal it is a hard
	// dependent and the job is not quarantine-eligible.
	blocking, err := unfinishedTransitiveDependent(ctx, tx, repositoryID, jobID)
	if err != nil {
		return false, "", err
	}
	if blocking {
		return false, "unfinished_downstream_dependent", nil
	}
	// (b) RFC 0118 provenance gate: a provenance-required reviewer the
	// run-completion gate would refuse must NOT be quarantine-finalized.
	required, err := jobIsProvenanceRequiredReviewer(ctx, tx, repositoryID, jobID)
	if err != nil {
		return false, "", err
	}
	if required {
		return false, "provenance_required_reviewer", nil
	}
	return true, "", nil
}

// unfinishedTransitiveDependent reports whether any job that transitively
// (multi-hop) depends on jobID is still non-terminal. It walks the
// job_dependencies downstream edges (depends_on_job_id -> job_id) breadth-first.
// A reachable job in any non-terminal state (blocked / queued / claimed /
// running / stale_lease / waiting_human / quarantined) means the deliverables
// cannot finalize without this job, so it must NOT be quarantined (#311 P0
// guard a). Terminal dependents (completed / failed / canceled / skipped) do
// not block. A `blocked` dependent counts as unfinished: it is waiting on this
// exact job and would never become runnable if the job is set aside.
func unfinishedTransitiveDependent(ctx context.Context, tx db.TxRunner, repositoryID, jobID string) (bool, error) {
	visited := map[string]bool{jobID: true}
	frontier := []string{jobID}
	for len(frontier) > 0 {
		next := []string{}
		for _, current := range frontier {
			rows, err := queryRows(ctx, tx, `
				SELECT j.job_id, j.state
				  FROM striatumd.job_dependencies dep
				  JOIN striatumd.jobs j
				    ON j.repository_id = dep.repository_id AND j.job_id = dep.job_id
				 WHERE dep.repository_id = $1 AND dep.depends_on_job_id = $2`,
				repositoryID, current)
			if err != nil {
				return false, err
			}
			for _, row := range rows {
				depID := fmt.Sprint(row["job_id"])
				if !terminalJobStates[fmt.Sprint(row["state"])] {
					// A non-terminal downstream dependent (at any depth) is a hard
					// dependency — short-circuit.
					return true, nil
				}
				if !visited[depID] {
					visited[depID] = true
					next = append(next, depID)
				}
			}
		}
		frontier = next
	}
	return false, nil
}

// jobIsProvenanceRequiredReviewer reports whether the job is a verdict-capable
// (review / phase_synthesis) job the RFC 0118 run-completion provenance gate
// would treat as provenance-required — i.e. one whose absence/missing-verdict
// would refuse a clean completion. Reuses workflowJobDefinitionForRow +
// reviewRequiresIndependentProvenance so the quarantine guard and the completion
// gate agree on which reviewers are load-bearing (#311 P0 guard b). A
// non-verdict-capable job is never a provenance gate, so it returns false fast.
func jobIsProvenanceRequiredReviewer(ctx context.Context, tx db.TxRunner, repositoryID, jobID string) (bool, error) {
	job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, false)
	if err != nil {
		return false, err
	}
	if !isVerdictCapableJobType(fmt.Sprint(job["job_type"])) {
		return false, nil
	}
	def, err := workflowJobDefinitionForRow(ctx, tx, repositoryID, job)
	if err != nil {
		return false, err
	}
	return def["require_attested_lane"] == true || reviewRequiresIndependentProvenance(job, def), nil
}

// quarantineExhaustedJob moves a single recovery-exhausted, downstream-clear job
// to the 'quarantined' state (#311 P0). It NEVER marks the job completed and
// NEVER seals an artifact — the quarantined job is the one narrow thing surfaced
// to the operator. It writes the recovery_exhausted blocker + escalation_inbox
// row on THIS job (so the operator has a resolvable handle and
// recovery.accept-quarantined can find it), stamps run_escalated_at (idempotency
// guard), appends a recovery.job_quarantined event naming
// {workflow_job_id, job_id, lane, stall_class}, and releases any residual active
// lease so the dead lane cannot wake. The caller then re-checks run completion.
func quarantineExhaustedJob(ctx context.Context, tx db.TxRunner, repositoryID, runID string, row map[string]any, lane, stallClass, lastAction string, requeueCount, transferCount, respawnCount int) (map[string]any, error) {
	jobID := fmt.Sprint(row["job_id"])
	workflowJobID := fmt.Sprint(nullable(row["workflow_job_id"]))
	recoveryAttempts := requeueCount + transferCount + respawnCount
	now := nowString()

	blockerID, err := newID("blk")
	if err != nil {
		return nil, err
	}
	laneClause := ""
	if lane != "" {
		laneClause = fmt.Sprintf(" lane=%s", lane)
	}
	description := fmt.Sprintf(
		"job quarantined: autonomous recovery exhausted for job %s (%s):%s stall_class=%s, last_recovery_action=%s, recovery_attempts=%d. The run finalized on its completed deliverables; this single job is set aside. Resolve with `recovery accept-quarantined <run-id> <job-id>` (marks it canceled-by-operator).",
		workflowJobID, jobID, laneClause, stallClass, lastAction, recoveryAttempts,
	)

	payload := map[string]any{
		"schema_version":             "striatum.recovery_escalation.v1",
		"source":                     "recovery.job_quarantined",
		"is_escalation":              true,
		"blocker_kind":               recoveryExhaustedBlockerKind,
		"severity":                   "blocked",
		"disposition":                "quarantined",
		"stuck_job":                  workflowJobID,
		"job_id":                     jobID,
		"stall_class":                stallClass,
		"last_recovery_action":       lastAction,
		"requeue_count":              requeueCount,
		"transfer_count":             transferCount,
		"respawn_count":              respawnCount,
		"recovery_attempts":          recoveryAttempts,
		"suggested_operator_actions": suggestedQuarantineOperatorActions(),
	}
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

	// Release any residual active lease so the dead lane cannot wake to
	// double-work the now-quarantined job. Belt-and-suspenders: a budget-exhausted
	// job's lane is already dead, but the quarantine state is terminal-for-finalize
	// so leave nothing live pointed at it.
	if err := tx.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'released', released_at = COALESCE(released_at, $1),
		       release_reason = COALESCE(release_reason, 'recovery_quarantine')
		 WHERE repository_id = $2 AND resource_id = $3 AND state = 'active'`,
		now, repositoryID, jobID); err != nil {
		return nil, err
	}

	// Move ONLY this job to 'quarantined'. NEVER 'completed' — no false
	// completion, no sealed artifact. current_lease_id is cleared so nothing
	// points a live lease at it.
	if err := tx.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'quarantined', current_lease_id = NULL
		 WHERE repository_id = $1 AND job_id = $2`,
		repositoryID, jobID); err != nil {
		return nil, err
	}

	// Idempotency guard: stamp run_escalated_at so a re-run does not re-process
	// this budget row (it is no longer escalation-eligible once quarantined, but
	// the stamp keeps the row out of the next pass's SELECT regardless).
	if err := tx.Exec(ctx, `
		UPDATE striatumd.job_recovery_state
		   SET run_escalated_at = $1, updated_at = $1
		 WHERE repository_id = $2 AND job_id = $3`,
		now, repositoryID, jobID); err != nil {
		return nil, err
	}

	quarantinedEvent := map[string]any{
		"workflow_job_id":   workflowJobID,
		"job_id":            jobID,
		"blocker_id":        blockerID,
		"blocker_kind":      recoveryExhaustedBlockerKind,
		"stall_class":       stallClass,
		"recovery_attempts": recoveryAttempts,
		"disposition":       "quarantined",
	}
	if lane != "" {
		quarantinedEvent["lane"] = lane
	}
	if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.job_quarantined", nil, jobID, nil, nil, nil, quarantinedEvent); err != nil {
		return nil, err
	}
	return quarantinedEvent, nil
}

// suggestedQuarantineOperatorActions returns the operator-actionable next steps
// for a quarantined job: the run already finalized on its deliverables, so the
// only action is to dispose of the single set-aside job.
func suggestedQuarantineOperatorActions() []any {
	return []any{
		"inspect the quarantined job's recovery history and any partial output",
		"accept the quarantine to mark the job canceled-by-operator: recovery accept-quarantined <run-id> <job-id>",
		"if the work is still needed, re-prepare it as a fresh run/job",
	}
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
	if stallClass == stallClassSessionUnrecoverableAcrossRotation {
		// RFC 0143 Slice A (#512): the rotation-lockout refinement — the lane was
		// locked out of resealing across a daemon boot-epoch rotation. Same
		// complete-but-unsealed remediation shape as agent_exited_unsealed (the
		// deliverable may be durable), but with a distinct, legible rotation reason so
		// the operator knows WHY it stopped. The supported #512 recovery is an operator
		// requeue (Slice B's automatic reseal is RFC-0168-bounded / out of scope).
		return []any{
			"session unrecoverable across a daemon boot-epoch rotation (#512): the lane lost its resealing path when the daemon restarted; its deliverable may be complete-but-unsealed on the run branch",
			"if the deliverable IS already durable on the run branch: `recovery complete-stalled --job-id <job>` to finalize from the published artifact",
			"if the deliverable is NOT durably present: `escalation resolve <id>` to re-dispatch the job with a fresh recovery budget, then let the run drive a fresh lane (launched against the CURRENT daemon) to re-author and seal (work.complete)",
			"otherwise capture the worktree diff and `run cancel`",
		}
	}
	if stallClass == stallClassAgentExitedUnsealed {
		return []any{
			"inspect the per-job worktree / published artifacts: the agent emitted output but exited before work.complete, so the work may be complete-but-unsealed",
			// #388: `escalation resolve` now re-dispatches a sessionless, budget-
			// exhausted job back to queued with a fresh recovery budget, so a fresh
			// lane re-authors and seals it. (The old "re-drive the run to respawn the
			// lane" guidance did NOT work from the running+spent-budget state.)
			"if the deliverable is NOT durably present: `escalation resolve <id>` to re-dispatch the job with a fresh recovery budget, then let the run drive a fresh lane to re-author and seal (work.complete)",
			"if the deliverable IS already durable on the run branch: `recovery complete-stalled --job-id <job>` to finalize from the published artifact",
			"otherwise capture the worktree diff and `run cancel`",
		}
	}
	return []any{
		"re-prepare with corrected write_scope",
		"transfer to a fresh session (session close --requeue-job / recovery requeue-stale --force)",
		// #388: for a sessionless budget-exhausted job, `escalation resolve` resets
		// it to queued with a fresh budget so the run re-dispatches it.
		"`escalation resolve <id>` to re-dispatch a sessionless budget-exhausted job with a fresh recovery budget",
		"cancel the run",
	}
}
