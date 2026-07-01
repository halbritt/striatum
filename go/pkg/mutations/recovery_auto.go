package mutations

import (
	"context"
	"fmt"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	"time"
)

func HandleRecoveryAuto(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "recovery.auto_publish_stale_artifacts requires run_id", nil)
	}
	dryRun := boolParam(envelope, "dry_run")
	// #198: pre-probe every supervised agent's liveness OUTSIDE the transaction.
	// The masked-dead-lane probe (supervisedAgentConfirmedDead) shells out to
	// `tmux list-panes` / reads /proc; doing it inside the lock-holding sweep tx
	// was the root of the minutes-long convoy that starved every other write
	// (global SQLSTATE 57014). The probe is a pure read whose result only gates
	// the in-tx decision, so taking the snapshot first and injecting it preserves
	// the decision logic exactly while removing all subprocess IO from the lock
	// window. Skipped on dry_run (it never requeues, so it never probes).
	if !dryRun {
		ctx = withLivenessOracle(ctx, buildRunLivenessOracle(ctx, runner, repositoryID, runID))
	}

	// #198: drain each supervisor's helper-event FIFO in its OWN short
	// transaction BEFORE the main sweep transaction, instead of inside it. The
	// drain reads files and, per event, takes `FOR UPDATE OF ps` on a supervisor
	// row plus 3 heartbeat UPDATEs (the `process_supervisor_pointers SET updated_at`
	// holder observed in #198). Doing that inside the lock-holding sweep tx stacked
	// the supervisor row locks behind the per-run advisory lock for the whole
	// sweep. Draining first, per-supervisor, keeps the writes short and OUT of the
	// advisory-lock window — consistent with the supervise.report path, which
	// already drains supervisor events without taking lockRun. It runs before the
	// main tx so refreshRunLiveness still classifies on freshly-drained activity
	// (the prior drain-before-liveness ordering is preserved). dry_run does not
	// drain (mutating), matching the prior in-tx behavior.
	helperEvents, err := sweepDrainHelperEvents(ctx, runner, repositoryID, runID, dryRun)
	if err != nil {
		return nil, err
	}
	laneUIDRecovery := recoverLaneUIDLeases(ctx, runner, repositoryID, runID, dryRun, boolParam(envelope, "retry_quarantined_lane_uids"))
	if !dryRun {
		// Worktree anchoring shells out to git; compute it before lockRun so the
		// sweep transaction only records the already-durable anchor payload.
		worktreeAnchors, err := buildRunWorktreeAnchorOracle(ctx, runner, repositoryID, runID)
		if err != nil {
			return nil, err
		}
		ctx = withWorktreeAnchorOracle(ctx, worktreeAnchors)
	}

	var notificationEscalations []map[string]any
	result, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// Mark every call below as executing inside the sweep transaction (#198
		// observability seam). Liveness probes must NOT run while this is set —
		// they were pre-probed above and read from the injected oracle.
		ctx := withinSweepTx(ctx)
		// RFC 0104: per-run advisory lock first — the recovery sweep is the third
		// concurrent party on a run's rows (alongside claim and verdict-completion),
		// so serializing it on the same per-run lock prevents the {sessions, runs}
		// cycle. Taken per-run (never one lock spanning all runs).
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, false)
		if err != nil {
			return nil, err
		}
		abandonedRun := map[string]any{"status": "skipped", "reason": "dry_run"}
		if !dryRun {
			if _, err := expireLeases(ctx, tx, repositoryID, runID); err != nil {
				return nil, err
			}
			abandonedRun, err = autoCancelAbandonedRunIfEligible(ctx, tx, repositoryID, runID, abandonedRunAutoCancelAfter)
			if err != nil {
				return nil, err
			}
			if abandonedRun["status"] == "auto_canceled" {
				return map[string]any{
					"run_id":            runID,
					"dry_run":           dryRun,
					"published_count":   0,
					"published":         []map[string]any{},
					"skipped_count":     0,
					"skipped":           []map[string]any{},
					"helper_events":     helperEvents,
					"lane_uid_recovery": laneUIDRecovery,
					"liveness":          map[string]any{"skipped": true, "reason": abandonedRunAutoCancelReason},
					"abandoned_run":     abandonedRun,
					"recovery_actions": map[string]any{
						"acted_count":              0,
						"actions":                  []map[string]any{},
						"escalation_pending_count": 0,
					},
					"escalations": map[string]any{
						"raised_count": 0,
						"raised":       []map[string]any{},
					},
				}, nil
			}
		}
		// #203: the expired-lease disjuncts (both the join and the WHERE) must
		// exclude leases that recovery already transferred or requeued away —
		// otherwise a lease whose job has since moved to a fresh attempt is
		// re-matched here and the auto-publish pass credits the (now re-opened)
		// job from the PRIOR attempt's on-disk artifact, silently discarding a
		// needs_revision verdict. This is the same NULL-safe exclusion #179 added
		// to HandleRecoveryStaleLeases / the dashboard projection; all three share
		// db.ExpiredLeaseStillStalePredicate so a fourth copy cannot drift.
		rows, err := queryRows(ctx, tx, `
			SELECT j.*, l.lease_id, l.owner_session_id,
			       qm.message_id, qm.state AS message_state,
			       l.state AS lease_state
			  FROM striatumd.jobs j
			  LEFT JOIN striatumd.leases l
			    ON l.repository_id = j.repository_id
			   AND (l.lease_id = j.current_lease_id
			        OR (l.resource_id = j.job_id AND l.state = 'expired'
			            AND `+db.ExpiredLeaseStillStalePredicate+`))
			  LEFT JOIN striatumd.queue_messages qm
			    ON qm.repository_id = j.repository_id
			   AND qm.message_id = j.current_message_id
			 WHERE j.repository_id = $1
			   AND j.run_id = $2
			   AND j.state IN ('claimed','running','stale_lease')
			   AND ((l.state = 'expired' AND `+db.ExpiredLeaseStillStalePredicate+`)
			        OR (l.state = 'active' AND l.expires_at < $3::timestamptz))
			 ORDER BY j.workflow_job_id`, repositoryID, runID, nowString())
		if err != nil {
			return nil, err
		}
		skipped := []map[string]any{}
		published := []map[string]any{}
		seen := map[string]bool{}
		for _, row := range rows {
			key := fmt.Sprintf("%v/%v", row["job_id"], row["lease_id"])
			if seen[key] {
				continue
			}
			seen[key] = true
			jobID := fmt.Sprint(row["job_id"])
			workflowJobID := fmt.Sprint(row["workflow_job_id"])
			sessionID := fmt.Sprint(nullable(row["owner_session_id"]))
			leaseID := fmt.Sprint(nullable(row["lease_id"]))
			messageID := fmt.Sprint(nullable(row["message_id"]))
			expected := asList(row["expected_artifacts_json"])
			if len(expected) == 0 {
				skipped = append(skipped, map[string]any{
					"workflow_job_id": workflowJobID,
					"reason":          "no expected_artifacts declared",
				})
				continue
			}
			if sessionID == "" || sessionID == "<nil>" || leaseID == "" || leaseID == "<nil>" || messageID == "" || messageID == "<nil>" {
				skipped = append(skipped, map[string]any{
					"workflow_job_id": workflowJobID,
					"reason":          "no recoverable session/lease/message triple",
				})
				continue
			}
			job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
			if err != nil {
				return nil, err
			}
			expectedByline, err := expectedAuthorLine(ctx, tx, repositoryID, job, sessionID)
			if err != nil {
				skipped = append(skipped, map[string]any{
					"workflow_job_id": workflowJobID,
					"reason":          "could not derive expected byline: " + err.Error(),
				})
				continue
			}
			publishable, attemptSkipped, err := autoPublishableArtifacts(ctx, tx, repositoryID, fmt.Sprint(run["repo_root"]), job, sessionID, expectedByline)
			if err != nil {
				return nil, err
			}
			// #203: surface the attempt-gate refusals (stale prior-attempt
			// artifacts) as legible skip records and emit an event so the silent
			// auto-publish-of-pre-revision-doc is observable in the run history.
			for _, rec := range attemptSkipped {
				skipped = append(skipped, rec)
				if _, eerr := appendEvent(ctx, tx, repositoryID, runID, "recovery.auto_publish_refused", sessionID, jobID, nil, nil, leaseID, rec); eerr != nil {
					return nil, eerr
				}
			}
			if len(publishable) == 0 {
				skipped = append(skipped, map[string]any{
					"workflow_job_id": workflowJobID,
					"reason":          "no required expected_artifact found on disk with matching byline",
				})
				continue
			}
			if dryRun {
				items := []map[string]any{}
				for _, declared := range publishable {
					items = append(items, map[string]any{
						"path":         declared["path"],
						"kind":         declared["kind"],
						"logical_name": declared["logical_name"],
					})
				}
				published = append(published, map[string]any{
					"workflow_job_id": workflowJobID,
					"session_id":      sessionID,
					"would_publish":   items,
					"would_expire":    fmt.Sprint(row["lease_state"]) == "active",
				})
				continue
			}
			artifacts := []map[string]any{}
			for _, declared := range publishable {
				// #530: publish FROM the root the file was found in (main repo_root or
				// the per-job worktree), so a worktree-salvaged deliverable is read back
				// from the worktree. The write-scope path check still uses the declared
				// repo-relative path, so the artifact's recorded repo_path is unchanged.
				sourceRoot := fmt.Sprint(run["repo_root"])
				if r, ok := declared["source_root"].(string); ok && r != "" {
					sourceRoot = r
				}
				artifact, err := publishRecoveredArtifact(ctx, tx, repositoryID, job, sessionID, leaseID, sourceRoot, declared)
				if err != nil {
					return nil, err
				}
				artifacts = append(artifacts, artifact)
			}
			var complete map[string]any
			if isVerdictCapableJobType(fmt.Sprint(job["job_type"])) {
				// #144: a verdict-bearing job (review / phase_synthesis) that stalled
				// after writing its finding artifact but before recording the verdict
				// must have that verdict recorded on recovery. Otherwise the job
				// completes with no verdict row and the verdict-gated --accepted
				// review--> edge never fires, wedging the run with every job green.
				// Recover the verdict from the on-disk finding's verdict_intent and run
				// the same completion / cycle / downstream routing recordVerdict does
				// (applyVerdict is the shared core; it tolerates the stale lease).
				verdict, findingArtifactID, found, verr := recoveredReviewVerdict(fmt.Sprint(run["repo_root"]), publishable, artifacts)
				if verr != nil {
					return nil, verr
				}
				// Route only the verdicts the autonomous path can cleanly apply
				// (accept / accept_with_findings clear the gate; needs_revision routes
				// the bounded cycle or opens a checkpoint). A `reject` carries an
				// interactive self-correction guard that returns an error when the
				// workflow declares a revision cycle — which here would roll back the
				// whole sweep — so it falls back to plain completion (pre-#144 behavior)
				// rather than wedging recovery for the run.
				if found && autonomouslyApplicableVerdict(verdict) {
					complete, err = applyVerdict(ctx, tx, repositoryID, sessionID, jobID, leaseID, verdict, job, findingArtifactID,
						"autonomous recovery: verdict auto-recorded from on-disk finding", nil, nil, "recovery_auto_recorded_from_finding")
				} else {
					complete, err = completeAutoRecoveredJob(ctx, tx, repositoryID, jobID, sessionID, leaseID, messageID)
				}
			} else {
				complete, err = completeAutoRecoveredJob(ctx, tx, repositoryID, jobID, sessionID, leaseID, messageID)
			}
			if err != nil {
				return nil, err
			}
			paths := []any{}
			for _, declared := range publishable {
				paths = append(paths, declared["path"])
			}
			if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.auto_published", sessionID, jobID, nil, nil, leaseID, map[string]any{
				"workflow_job_id": workflowJobID,
				"artifacts":       paths,
				"byline":          expectedByline,
			}); err != nil {
				return nil, err
			}
			published = append(published, map[string]any{
				"workflow_job_id": workflowJobID,
				"session_id":      sessionID,
				"artifacts":       artifacts,
				"complete":        complete,
			})
		}
		if !dryRun {
			if err := maybeCompleteRun(ctx, tx, repositoryID, runID); err != nil {
				return nil, err
			}
		}
		// RFC 0101 Phase 3 Slice 2: the autonomous in-daemon recovery decision tree
		// (OQ4 resolved in-daemon, D094). Runs after the auto-publish pass (so any
		// recoverable job already completed and is excluded) and before
		// refreshRunLiveness (which then classifies whatever the tree left
		// untouched). It reclaims genuinely-stuck jobs on the same attempt within
		// per-job budgets. Dry-run skips it (it mutates state).
		recoveryActions := []map[string]any{}
		escalationsRaised := []map[string]any{}
		if !dryRun {
			workflow, werr := workflowForRun(ctx, tx, repositoryID, run)
			if werr != nil {
				return nil, werr
			}
			policy := recoveryPolicyFromWorkflow(workflow)
			acted, rerr := recoverStuckJobs(ctx, tx, repositoryID, runID, policy)
			if rerr != nil {
				return nil, rerr
			}
			recoveryActions = acted
			// #579: session-driven complement to the job-driven recoverStuckJobs.
			// Reap an idle-stalled lane that owns NO job (a builder that finished its
			// slice then went agent_protocol_idle_stall with job=None) — recoverStuckJobs
			// scans only unfinished jobs and cannot reach it, so it would otherwise keep
			// MCP-heartbeating forever, hold its lane "occupied" in run-reconcile, and
			// starve queued downstream jobs with no needs_operator. Run AFTER
			// recoverStuckJobs so any session the job-driven paths just closed is already
			// 'closed' here and skipped.
			reaped, oerr := reapIdleOrphanSessions(ctx, tx, repositoryID, runID)
			if oerr != nil {
				return nil, oerr
			}
			recoveryActions = append(recoveryActions, reaped...)
			// A requeue may have completed the run's last unfinished job's removal
			// from limbo; re-check completion so a fully-recovered run can settle.
			if err := maybeCompleteRun(ctx, tx, repositoryID, runID); err != nil {
				return nil, err
			}
			// RFC 0101 Phase 4: consume escalation_pending. recoverStuckJobs flags a
			// budget-exhausted job (Phase 3) but nothing acted on the flag. Here we
			// turn each newly-exhausted job into a structured, operator-actionable
			// escalation (blocker + escalation_inbox, RFC 0062) and flip the run to
			// needs_operator so it never sits silently 'running'. Idempotent via the
			// job_recovery_state.run_escalated_at guard; the sweep's running/paused
			// active-run filter then excludes the escalated run from re-sweeping.
			raised, notify, eerr := escalateExhaustedJobs(ctx, tx, repositoryID, runID, policy)
			if eerr != nil {
				return nil, eerr
			}
			escalationsRaised = raised
			notificationEscalations = notify
		}
		liveness, err := refreshRunLiveness(ctx, tx, repositoryID, runID, dryRun)
		if err != nil {
			return nil, err
		}
		escalationPending := 0
		for _, item := range recoveryActions {
			if item["escalation_pending"] == true {
				escalationPending++
			}
		}
		return map[string]any{
			"run_id":            runID,
			"dry_run":           dryRun,
			"published_count":   len(published),
			"published":         published,
			"skipped_count":     len(skipped),
			"skipped":           skipped,
			"helper_events":     helperEvents,
			"lane_uid_recovery": laneUIDRecovery,
			"liveness":          liveness,
			"abandoned_run":     abandonedRun,
			"recovery_actions": map[string]any{
				"acted_count":              len(recoveryActions),
				"actions":                  recoveryActions,
				"escalation_pending_count": escalationPending,
			},
			"escalations": map[string]any{
				"raised_count": len(escalationsRaised),
				"raised":       escalationsRaised,
			},
		}, nil
	})
	if err != nil {
		return nil, err
	}
	if !dryRun {
		notifyRecoveryEscalationsBestEffort(ctx, repositoryID, runID, notificationEscalations)
	}
	return result, nil
}

func autoCancelAbandonedRunIfEligible(ctx context.Context, tx db.TxRunner, repositoryID, runID string, threshold time.Duration) (map[string]any, error) {
	decision, err := abandonedRunAutoCancelDecision(ctx, tx, repositoryID, runID, threshold, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if decision["eligible"] != true {
		return decision, nil
	}
	result, err := cancelRunInTx(ctx, tx, repositoryID, runID, abandonedRunAutoCancelReason)
	if err != nil {
		return nil, err
	}
	result["status"] = "auto_canceled"
	result["reason"] = abandonedRunAutoCancelReason
	result["threshold_seconds"] = int(threshold.Seconds())
	result["last_activity_at"] = decision["last_activity_at"]
	if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.abandoned_run_auto_canceled", nil, nil, nil, nil, nil, map[string]any{
		"reason":            abandonedRunAutoCancelReason,
		"threshold_seconds": int(threshold.Seconds()),
		"last_activity_at":  decision["last_activity_at"],
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func abandonedRunAutoCancelDecision(ctx context.Context, tx db.TxRunner, repositoryID, runID string, threshold time.Duration, now time.Time) (map[string]any, error) {
	run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true)
	if err != nil {
		return nil, err
	}
	if fmt.Sprint(run["state"]) != "running" {
		return map[string]any{"status": "not_candidate", "eligible": false, "reason": "run_not_running"}, nil
	}
	checks := []struct {
		reason string
		sql    string
	}{
		{
			reason: "live_session",
			sql: `SELECT 1 FROM striatumd.sessions
			       WHERE repository_id = $1 AND run_id = $2 AND state = 'active'
			       LIMIT 1`,
		},
		{
			reason: "active_lease",
			sql: `SELECT 1 FROM striatumd.leases
			       WHERE repository_id = $1 AND run_id = $2 AND state = 'active'
			       LIMIT 1`,
		},
		{
			reason: "live_supervisor",
			sql: `SELECT 1 FROM striatumd.process_supervisors
			       WHERE repository_id = $1 AND run_id = $2 AND state IN ('starting','attached','detached')
			       LIMIT 1`,
		},
		{
			reason: "live_supervisor_pointer",
			sql: `SELECT 1 FROM striatumd.process_supervisor_pointers
			       WHERE repository_id = $1 AND run_id = $2 AND state IN ('starting','attached','detached')
			       LIMIT 1`,
		},
		{
			reason: "live_daemon_supervisor",
			sql: `SELECT 1 FROM striatumd.daemon_supervisors
			       WHERE repository_id = $1 AND run_id = $2 AND state IN ('starting','attached','detached')
			       LIMIT 1`,
		},
		{
			reason: "live_process_execution",
			sql: `SELECT 1 FROM striatumd.process_executions
			       WHERE repository_id = $1 AND run_id = $2 AND state IN ('starting','running')
			       LIMIT 1`,
		},
		{
			reason: "live_worktree",
			sql: `SELECT 1 FROM striatumd.job_worktrees
			       WHERE repository_id = $1 AND run_id = $2 AND state IN ('active','abandoned')
			       LIMIT 1`,
		},
	}
	for _, check := range checks {
		found, err := existsRow(ctx, tx, check.sql, repositoryID, runID)
		if err != nil {
			return nil, err
		}
		if found {
			return map[string]any{"status": "not_candidate", "eligible": false, "reason": check.reason}, nil
		}
	}
	lastActivity, ok, err := abandonedRunLastActivity(ctx, tx, repositoryID, runID, run)
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[string]any{"status": "not_candidate", "eligible": false, "reason": "missing_activity_baseline"}, nil
	}
	if !lastActivity.Before(now.Add(-threshold)) {
		return map[string]any{
			"status":            "not_candidate",
			"eligible":          false,
			"reason":            "recent_activity",
			"threshold_seconds": int(threshold.Seconds()),
			"last_activity_at":  lastActivity.UTC().Format(time.RFC3339),
		}, nil
	}
	return map[string]any{
		"status":            "candidate",
		"eligible":          true,
		"reason":            "abandoned_run_idle",
		"threshold_seconds": int(threshold.Seconds()),
		"last_activity_at":  lastActivity.UTC().Format(time.RFC3339),
	}, nil
}

func abandonedRunLastActivity(ctx context.Context, tx db.TxRunner, repositoryID, runID string, run map[string]any) (time.Time, bool, error) {
	var latest time.Time
	setLatest := func(value any) {
		if ts, ok := asTime(value); ok && (latest.IsZero() || ts.After(latest)) {
			latest = ts
		}
	}
	setLatest(run["created_at"])
	setLatest(run["started_at"])
	row, err := oneRow(ctx, tx, `
		SELECT MAX(created_at) AS last_event_at
		  FROM striatumd.events
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND event_type NOT IN (
		     'daemon.recovery_sweep',
		     'lease.expired',
		     'session.liveness_deadline_missed',
		     'session.liveness_recovered',
		     'worktree.abandoned'
		   )`, repositoryID, runID)
	if err != nil {
		return time.Time{}, false, err
	}
	setLatest(row["last_event_at"])
	sessionRow, err := oneRow(ctx, tx, `
		SELECT MAX(activity_at) AS last_session_activity_at
		  FROM (
		    SELECT registered_at AS activity_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT closed_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_mcp_request_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_tools_list_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_await_packet_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_packet_delivered_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_ack_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_work_block_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_work_release_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_work_complete_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_work_heartbeat_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_session_ready_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_session_heartbeat_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_session_question_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		    UNION ALL SELECT last_session_escalate_at FROM striatumd.sessions WHERE repository_id = $1 AND run_id = $2
		  ) activity
		 WHERE activity_at IS NOT NULL`, repositoryID, runID)
	if err != nil {
		return time.Time{}, false, err
	}
	setLatest(sessionRow["last_session_activity_at"])
	if latest.IsZero() {
		return time.Time{}, false, nil
	}
	return latest.UTC(), true, nil
}

func drainRunHelperEvents(ctx context.Context, tx db.TxRunner, repositoryID string, runID string, dryRun bool) (map[string]any, error) {
	rows, err := queryRows(ctx, tx,
		`SELECT supervisor_id
		   FROM striatumd.process_supervisor_pointers
		  WHERE repository_id = $1
		    AND run_id = $2
		    AND state IN ('starting','attached')
		  ORDER BY supervisor_id`,
		repositoryID,
		runID,
	)
	if err != nil {
		return nil, err
	}
	drained := []string{}
	for _, row := range rows {
		supervisorID := fmt.Sprint(nullable(row["supervisor_id"]))
		if supervisorID == "" || supervisorID == "<nil>" {
			continue
		}
		if dryRun {
			continue
		}
		if err := recoveryDrainHelperEvents(ctx, tx, repositoryID, supervisorID, 0); err != nil {
			return nil, err
		}
		drained = append(drained, supervisorID)
	}
	return map[string]any{
		"checked_count": len(rows),
		"drained_count": len(drained),
		"drained":       drained,
		"dry_run":       dryRun,
	}, nil
}

// sweepDrainHelperEvents is the #198 out-of-sweep-tx form of drainRunHelperEvents:
// it enumerates the run's active supervisors with a plain read (no tx, no
// FOR UPDATE) and drains each supervisor's helper FIFO in its OWN short
// transaction, so the per-supervisor `FOR UPDATE OF ps` + heartbeat UPDATEs are
// never held inside the main sweep's per-run advisory-lock window.
//
// A single supervisor's drain failing is isolated (recorded as a soft error) and
// does NOT fail the sweep — one wedged supervisor row can no longer serialize the
// whole run's recovery. dryRun is a pure read of the supervisor list (no drain),
// matching the prior in-tx dryRun behavior.
func sweepDrainHelperEvents(ctx context.Context, runner db.Runner, repositoryID string, runID string, dryRun bool) (map[string]any, error) {
	rows, err := queryRows(ctx, runner,
		`SELECT supervisor_id
		   FROM striatumd.process_supervisor_pointers
		  WHERE repository_id = $1
		    AND run_id = $2
		    AND state IN ('starting','attached')
		  ORDER BY supervisor_id`,
		repositoryID,
		runID,
	)
	if err != nil {
		return nil, err
	}
	drained := []string{}
	drainErrors := []map[string]any{}
	for _, row := range rows {
		supervisorID := fmt.Sprint(nullable(row["supervisor_id"]))
		if supervisorID == "" || supervisorID == "<nil>" {
			continue
		}
		if dryRun {
			continue
		}
		if _, derr := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
			return nil, recoveryDrainHelperEvents(ctx, tx, repositoryID, supervisorID, 0)
		}); derr != nil {
			drainErrors = append(drainErrors, map[string]any{
				"supervisor_id": supervisorID,
				"error":         derr.Error(),
			})
			continue
		}
		drained = append(drained, supervisorID)
	}
	result := map[string]any{
		"checked_count": len(rows),
		"drained_count": len(drained),
		"drained":       drained,
		"dry_run":       dryRun,
	}
	if len(drainErrors) > 0 {
		result["drain_errors"] = drainErrors
	}
	return result, nil
}

func refreshRunLiveness(ctx context.Context, tx db.TxRunner, repositoryID string, runID string, dryRun bool) (map[string]any, error) {
	rows, err := queryRows(ctx, tx,
		`SELECT s.session_id, s.run_id, s.role_id, s.lane_id, s.state, s.registered_at,
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
		        s.liveness_stall_class,
		        s.liveness_stall_since,
		        active_lease.lease_id AS active_lease_id,
		        active_lease.acquired_at AS active_lease_acquired_at,
		        active_lease.expires_at AS active_lease_expires_at,
		        active_lease.last_heartbeat_at AS active_lease_last_heartbeat_at
		   FROM striatumd.sessions s
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
		    AND s.run_id = $2
		    AND s.state = 'active'
		  ORDER BY s.registered_at, s.session_id
		  FOR UPDATE OF s`,
		repositoryID,
		runID,
	)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	missed := []map[string]any{}
	recovered := []map[string]any{}
	for _, row := range rows {
		activity := sessionliveness.ActivityFromRow(row)
		result := sessionliveness.Classify(activity, sessionliveness.DefaultPolicy(), now)
		previous := fmt.Sprint(nullable(row[sessionliveness.LivenessStallClass]))
		if previous == "<nil>" {
			previous = ""
		}
		if previous == result.StallClass {
			continue
		}
		sessionID := fmt.Sprint(row["session_id"])
		var stallSince any
		if result.StallSince != nil {
			stallSince = *result.StallSince
		}
		if !dryRun {
			if err := tx.Exec(ctx, `
				UPDATE striatumd.sessions
				   SET liveness_stall_class = $1,
				       liveness_stall_since = $2
				 WHERE repository_id = $3 AND session_id = $4`,
				nullable(result.StallClass),
				stallSince,
				repositoryID,
				sessionID,
			); err != nil {
				return nil, err
			}
			if result.StallClass != "" {
				if _, err := appendEvent(ctx, tx, repositoryID, runID, "session.liveness_deadline_missed", sessionID, nil, nil, nil, nil, map[string]any{
					"repository_id":           repositoryID,
					"run_id":                  runID,
					"session_id":              sessionID,
					"lane_id":                 row["lane_id"],
					"role_id":                 row["role_id"],
					"stall_class":             result.StallClass,
					"deadline_name":           result.DeadlineName,
					"deadline_seconds":        result.DeadlineSeconds,
					"observed_at":             now.Format(time.RFC3339),
					"last_activity_timestamp": relevantActivityTimestamp(activity, result.DeadlineName),
				}); err != nil {
					return nil, err
				}
			} else if previous != "" {
				if _, err := appendEvent(ctx, tx, repositoryID, runID, "session.liveness_recovered", sessionID, nil, nil, nil, nil, map[string]any{
					"repository_id":        repositoryID,
					"run_id":               runID,
					"session_id":           sessionID,
					"lane_id":              row["lane_id"],
					"role_id":              row["role_id"],
					"previous_stall_class": previous,
					"observed_at":          now.Format(time.RFC3339),
					"signal":               "liveness_sweep",
				}); err != nil {
					return nil, err
				}
			}
		}
		item := map[string]any{
			"session_id": sessionID,
			"previous":   nullable(previous),
			"current":    nullable(result.StallClass),
		}
		if result.StallClass != "" {
			missed = append(missed, item)
		} else {
			recovered = append(recovered, item)
		}
	}
	return map[string]any{
		"checked_count":   len(rows),
		"missed_count":    len(missed),
		"missed":          missed,
		"recovered_count": len(recovered),
		"recovered":       recovered,
		"dry_run":         dryRun,
	}, nil
}

func relevantActivityTimestamp(activity sessionliveness.Activity, deadline string) any {
	var value *time.Time
	switch deadline {
	case sessionliveness.DeadlineDiscovery:
		value = activity.RegisteredAt
	case sessionliveness.DeadlineAwaitPacket:
		value = activity.LastToolsListAt
	case sessionliveness.DeadlineAck:
		value = activity.LastPacketDeliveredAt
	case sessionliveness.DeadlineLeaseHeartbeat:
		value = latestMutationTime(activity.LastWorkHeartbeatAt, activity.ActiveLeaseHeartbeatAt, activity.ActiveLeaseAcquiredAt)
	case sessionliveness.DeadlineQuestionPending:
		value = activity.LastSessionQuestionAt
	case sessionliveness.DeadlineEscalation:
		value = activity.LastSessionEscalateAt
	case sessionliveness.DeadlineToolProgress:
		// #324: the wedge rung ages against the tool-call timeline only (never PTY
		// spinner activity), so the operator-visible relevant timestamp is the most
		// recent tool-call start/finish.
		value = latestMutationTime(activity.LastToolCallStartedAt, activity.LastToolCallFinishedAt)
	default:
		value = latestMutationTime(
			activity.LastMCPRequestAt,
			activity.LastToolsListAt,
			activity.LastAwaitPacketAt,
			activity.LastPacketDeliveredAt,
			activity.LastAckAt,
			activity.LastWorkBlockAt,
			activity.LastWorkReleaseAt,
			activity.LastWorkCompleteAt,
			activity.LastWorkHeartbeatAt,
			activity.LastSessionReadyAt,
			activity.LastSessionHeartbeatAt,
			activity.LastSessionQuestionAt,
			activity.LastSessionEscalateAt,
			activity.RegisteredAt,
		)
	}
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

func latestMutationTime(values ...*time.Time) *time.Time {
	var latest *time.Time
	for _, value := range values {
		if value == nil {
			continue
		}
		if latest == nil || value.After(*latest) {
			latest = value
		}
	}
	return latest
}
