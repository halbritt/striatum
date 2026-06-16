package mutations

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/halbritt/striatum/go/pkg/agentloop"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	"github.com/jackc/pgx/v5"
)

// slotHasUnclaimedParallelWork reports whether the (run, role, lane) slot has
// more live work messages than the given number of active sessions — i.e. a
// declared-parallel queued job remains that a new distinct session could claim
// (#100). Work messages in pending/claimed/acked are "live" (a terminal message
// is done). When the count does not exceed the active sessions, a second
// registration on the slot is an accidental duplicate and is refused.
func slotHasUnclaimedParallelWork(ctx context.Context, runner any, repositoryID, runID, role, lane string, activeSessions int) (bool, error) {
	row, err := oneRow(ctx, runner, `
		SELECT count(*) AS n
		  FROM striatumd.queue_messages
		 WHERE repository_id = $1 AND run_id = $2 AND kind = 'work'
		   AND target_role_id = $3 AND target_lane_id = $4
		   AND state IN ('pending', 'claimed', 'acked')`,
		repositoryID, runID, role, lane)
	if err != nil {
		return false, err
	}
	return intValue(row["n"]) > activeSessions, nil
}

func HandleRegisterSession(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	role := stringParam(envelope, "role")
	lane := stringParam(envelope, "lane")
	if runID == "" || role == "" || lane == "" {
		return nil, rpc.NewError("schema_invalid", "session.register requires run_id, role, and lane", nil)
	}
	requestedCapabilities := stringSliceParam(envelope, "capabilities", "capability")
	fresh := boolParam(envelope, "fresh")
	// RFC 0095 §5 (#60/#75): --replace (alias --force) opts in to atomically
	// closing any active session on the same (run, role, lane) slot before
	// registering the new one. Without it, a duplicate active session is no
	// longer silently superseded (#75) — the handler returns the exact
	// remediation naming which session to close (#60).
	replace := boolParam(envelope, "replace") || boolParam(envelope, "force")
	parentSessionID := nullable(stringParam(envelope, "parent_session_id"))
	forceNonFresh := boolParam(envelope, "force_non_fresh")
	nonFreshReason := stringParam(envelope, "non_fresh_reason")
	if nonFreshReason == "" {
		nonFreshReason = stringParam(envelope, "reason")
	}
	// #189/#174: --replace must not silently yank a session that is still live
	// (heartbeating within the lease's advertised window) and possibly driving
	// the same work packet. --force-live --reason "..." is the explicit escape
	// hatch for a genuinely-wedged-but-heartbeating lane; it mirrors the
	// --force-non-fresh --reason pattern and records the justification.
	forceLive := boolParam(envelope, "force_live")
	forceLiveReason := strings.TrimSpace(stringParam(envelope, "force_live_reason"))
	if forceLiveReason == "" {
		forceLiveReason = strings.TrimSpace(stringParam(envelope, "reason"))
	}
	operatorLabel := stringParam(envelope, "operator_label")

	// #133: register-session --replace can race a concurrent claim/recovery
	// transaction and abort with a 40P01 deadlock during stale-reviewer
	// recovery. Retry the same bounded way the RFC 0101 Phase 0a handlers do
	// (claim/review/interrogation) so the recovery path tolerates the deadlock
	// internally instead of surfacing it to the operator. The body fully rolls
	// back on abort, so re-running from scratch is safe.
	return withTxRetryOnDeadlock(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first — session.register mutates
		// the run aggregate and races with run.cancel / run drive cleanup.
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true)
		if err != nil {
			return nil, err
		}
		if isTerminalRunState(fmt.Sprint(run["state"])) {
			return nil, rpc.NewError(
				"invalid_transition",
				fmt.Sprintf("run %s is in terminal state %q; session.register refuses terminal runs", runID, run["state"]),
				map[string]any{"run_id": runID, "run_state": run["state"]},
			)
		}
		snapshot, err := rowByID(ctx, tx, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(run["workflow_snapshot_id"]), false)
		if err != nil {
			return nil, err
		}
		workflow := asMap(snapshot["workflow_json"])
		roles := asMap(workflow["roles"])
		if _, ok := roles[role]; !ok {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("unknown role %q for run", role), nil)
		}
		lanes := asMap(workflow["lanes"])
		laneConfig, ok := lanes[lane]
		if !ok {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("unknown lane %q for run", lane), nil)
		}
		capabilities := normalizeSessionCapabilities(requestedCapabilities)
		if len(capabilities) == 0 {
			capabilities = workflowLaneCapabilities(asMap(laneConfig))
		}
		var recordedLabel any
		if operatorLabel != "" {
			cleaned, err := validateOperatorLabel(operatorLabel, workflow)
			if err != nil {
				return nil, err
			}
			recordedLabel = cleaned
		}
		var recordedReason any
		if role == "reviewer" && workflowDeclaresFreshReviewer(workflow) {
			authorRow, err := oneRow(ctx, tx, `
				SELECT s.session_id
				  FROM striatumd.sessions s
				 WHERE s.repository_id = $1 AND s.run_id = $2
				   AND s.role_id = 'author' AND s.state = 'active'
				   AND (
				     EXISTS (
				       SELECT 1
				         FROM striatumd.leases l
				        WHERE l.repository_id = s.repository_id
				          AND l.run_id = s.run_id
				          AND l.owner_session_id = s.session_id
				          AND l.resource_type = 'job'
				          AND l.state = 'active'
				     )
				     OR EXISTS (
				       SELECT 1
				         FROM striatumd.queue_messages q
				        WHERE q.repository_id = s.repository_id
				          AND q.run_id = s.run_id
				          AND q.kind = 'work'
				          AND q.target_role_id = s.role_id
				          AND q.state = 'pending'
				     )
				   )
				 ORDER BY s.registered_at
				 LIMIT 1`, repositoryID, runID)
			if err == nil {
				if !forceNonFresh {
					// #188/#209: name the contaminable author session and suggest
					// session.close as the cleaner remedy, alongside the explicit
					// --force-non-fresh escape hatch.
					authorSession := strings.TrimSpace(fmt.Sprint(authorRow["session_id"]))
					return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
						"workflow declares reviewer_context_policy: fresh and an active author session (%s) is still holding or eligible for work on this run; release it with `striatum session close %s --reason \"...\"` to register a fresh reviewer, or pass --force-non-fresh --reason \"...\" to register a non-fresh reviewer explicitly",
						authorSession, authorSession), nil)
				}
				if strings.TrimSpace(nonFreshReason) == "" {
					return nil, rpc.NewError("invalid_transition", "--force-non-fresh requires a non-empty --reason", nil)
				}
				recordedReason = strings.TrimSpace(nonFreshReason)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}
		activeSessions, err := queryRows(ctx, tx, `
			SELECT session_id, role_id, lane_id, operator_label,
			       GREATEST(
			         EXTRACT(EPOCH FROM (now() - COALESCE(last_heartbeat_at, registered_at)))::bigint,
			         0
			       ) AS heartbeat_age_seconds
			  FROM striatumd.sessions
			 WHERE repository_id = $1 AND run_id = $2
			   AND role_id = $3 AND lane_id = $4
			   AND state = 'active'
			 FOR UPDATE`, repositoryID, runID, role, lane)
		if err != nil {
			return nil, err
		}
		// RFC 0095 §5: without --replace, do NOT implicitly supersede an existing
		// active session on this slot. Parallel same-(role, lane) jobs (#75) need
		// distinct active sessions, so a second fresh registration must succeed
		// without closing the first. When the operator genuinely wants to replace
		// a stranded session (#60), --replace closes the prior session(s) and
		// transfers their leases; otherwise return the exact remediation.
		if len(activeSessions) > 0 && !replace {
			// #100: the documented parallel fanout — multiple queued jobs on the
			// same (run, role, lane) with disjoint scopes — needs a distinct active
			// session per job. Allow a second active session ONLY when the slot has
			// genuinely more live parallel work than active sessions to claim it;
			// the new session gets a fresh ordinal below and claims the extra job.
			// Otherwise the duplicate is an accidental double-register (#60) and is
			// refused with the same remediation.
			parallel, err := slotHasUnclaimedParallelWork(ctx, tx, repositoryID, runID, role, lane, len(activeSessions))
			if err != nil {
				return nil, err
			}
			if !parallel {
				ids := make([]string, 0, len(activeSessions))
				for _, s := range activeSessions {
					ids = append(ids, fmt.Sprint(s["session_id"]))
				}
				return nil, rpc.NewError(
					"invalid_transition",
					fmt.Sprintf(
						"an active session already exists on this (run, role, lane): %s, and there is no additional queued parallel job for it to claim. To replace it, pass --replace (or close it first with `striatum session close %s`).",
						strings.Join(ids, ", "),
						ids[0],
					),
					map[string]any{"active_session_ids": ids},
				)
			}
			// Parallel work remains: fall through to register a distinct sibling
			// session WITHOUT superseding the existing one(s).
		}
		supersedePriorSessions := replace
		if !supersedePriorSessions {
			activeSessions = nil
		}
		// #189/#174: --replace transferred leases off the displaced session(s)
		// with no liveness check, so a wrapper could yank a session that was
		// still heartbeating and actively driving the same packet — the silent
		// double-drive that requeued live work in run_cbd6d74b. Before
		// superseding, refuse when any displaced session heartbeated within the
		// lease's advertised heartbeat window (the same window the liveness sweep
		// uses to classify a lease as live, derived from the canonical liveness
		// policy — not a magic literal). --force-live --reason "..." is the
		// explicit escape hatch for a genuinely-wedged-but-heartbeating lane.
		if supersedePriorSessions && !forceLive {
			heartbeatWindow := sessionliveness.DefaultPolicy().LeaseHeartbeatSeconds
			for _, oldSess := range activeSessions {
				ageSeconds := intValue(oldSess["heartbeat_age_seconds"])
				if ageSeconds > heartbeatWindow {
					continue
				}
				oldSessID := fmt.Sprint(oldSess["session_id"])
				label := ""
				if value := nullable(oldSess["operator_label"]); value != nil {
					if cleaned := strings.TrimSpace(fmt.Sprint(value)); cleaned != "" {
						label = fmt.Sprintf(" (operator_label: %s)", cleaned)
					}
				}
				return nil, rpc.NewError(
					"displaced_session_live",
					fmt.Sprintf(
						"--replace would displace session %s%s, which last heartbeated %ds ago — within the %ds heartbeat window, so it is still live and may be driving this packet. Confirm it is genuinely wedged (`striatum list sessions`), then retry with --force-live --reason \"...\", or close it first with `striatum session close %s`.",
						oldSessID, label, ageSeconds, heartbeatWindow, oldSessID,
					),
					map[string]any{
						"displaced_session_id":  oldSessID,
						"heartbeat_age_seconds": ageSeconds,
						"heartbeat_window":      heartbeatWindow,
						"operator_label":        nullable(oldSess["operator_label"]),
					},
				)
			}
		}
		// --force-live, like --force-non-fresh, requires a recorded justification.
		if supersedePriorSessions && forceLive && len(activeSessions) > 0 && forceLiveReason == "" {
			return nil, rpc.NewError("invalid_transition", "--force-live requires a non-empty --reason", nil)
		}
		for _, oldSess := range activeSessions {
			oldSessID := fmt.Sprint(oldSess["session_id"])
			now := nowString()
			err = tx.Exec(ctx, `
				UPDATE striatumd.sessions
				   SET state = 'closed', closed_at = $1, close_reason = 'superseded'
				 WHERE repository_id = $2 AND session_id = $3`, now, repositoryID, oldSessID)
			if err != nil {
				return nil, err
			}
			supersessionPayload := map[string]any{
				"session_id": oldSessID,
				"role_id":    oldSess["role_id"],
				"lane_id":    oldSess["lane_id"],
				"reason":     "superseded",
				"source":     "supersession",
			}
			// #189: when the operator force-displaced a still-live lane, record
			// the justification on the audit event so the override is legible.
			if forceLive && forceLiveReason != "" {
				supersessionPayload["force_live"] = true
				supersessionPayload["force_live_reason"] = forceLiveReason
			}
			_, err = appendEvent(ctx, tx, repositoryID, runID, "session.closed", oldSessID, nil, nil, nil, nil, supersessionPayload)
			if err != nil {
				return nil, err
			}

			leases, err := queryRows(ctx, tx, `
				SELECT lease_id, resource_id
				  FROM striatumd.leases
				 WHERE repository_id = $1 AND owner_session_id = $2 AND state = 'active'`, repositoryID, oldSessID)
			if err != nil {
				return nil, err
			}
			for _, lease := range leases {
				leaseID := fmt.Sprint(lease["lease_id"])
				jobID := fmt.Sprint(lease["resource_id"])

				err = tx.Exec(ctx, `
					UPDATE striatumd.leases
					   SET state = 'released', released_at = $1
					 WHERE repository_id = $2 AND lease_id = $3`, now, repositoryID, leaseID)
				if err != nil {
					return nil, err
				}
				_, err = appendEvent(ctx, tx, repositoryID, runID, "lease.released", oldSessID, jobID, nil, nil, leaseID, map[string]any{
					"reason": "superseded",
				})
				if err != nil {
					return nil, err
				}

				err = tx.Exec(ctx, `
					UPDATE striatumd.jobs
					   SET state = 'queued', current_message_id = NULL, current_lease_id = NULL
					 WHERE repository_id = $1 AND job_id = $2`, repositoryID, jobID)
				if err != nil {
					return nil, err
				}
				_, err = appendEvent(ctx, tx, repositoryID, runID, "job.queued", oldSessID, jobID, nil, nil, nil, map[string]any{
					"reason": "superseded",
				})
				if err != nil {
					return nil, err
				}

				err = tx.Exec(ctx, `
					UPDATE striatumd.queue_messages
					   SET state = 'pending', current_lease_id = NULL, updated_at = $1
					 WHERE repository_id = $2 AND job_id = $3 AND state IN ('claimed', 'acked')`, now, repositoryID, jobID)
				if err != nil {
					return nil, err
				}
			}
		}

		rows, err := queryRows(ctx, tx, `
			SELECT ordinal
			  FROM striatumd.sessions
			 WHERE repository_id = $1 AND run_id = $2
			   AND role_id = $3 AND lane_id = $4
			 FOR UPDATE`, repositoryID, runID, role, lane)
		if err != nil {
			return nil, err
		}
		ordinal := 1
		for _, row := range rows {
			switch value := row["ordinal"].(type) {
			case int:
				if value >= ordinal {
					ordinal = value + 1
				}
			case int32:
				if int(value) >= ordinal {
					ordinal = int(value) + 1
				}
			case int64:
				if int(value) >= ordinal {
					ordinal = int(value) + 1
				}
			}
		}
		sessionID, err := newID("sess")
		if err != nil {
			return nil, err
		}
		slug := fmt.Sprintf("%s-%s-%d", role, lane, ordinal)
		now := nowString()
		capabilitiesArg, err := db.JSONBArg(tx, capabilities)
		if err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			INSERT INTO striatumd.sessions (
			  repository_id, session_id, run_id, role_id, lane_id, slug, ordinal,
			  capabilities_json, parent_session_id, fresh_context, state,
			  registered_at, last_heartbeat_at, non_fresh_reason, operator_label
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,'active',$11,$12,$13,$14)`,
			repositoryID,
			sessionID,
			runID,
			role,
			lane,
			slug,
			ordinal,
			capabilitiesArg,
			parentSessionID,
			fresh,
			now,
			now,
			recordedReason,
			recordedLabel,
		); err != nil {
			return nil, err
		}
		payload := map[string]any{"role": role, "lane": lane, "slug": slug}
		if recordedReason != nil {
			payload["non_fresh_reason"] = recordedReason
		}
		if recordedLabel != nil {
			payload["operator_label"] = recordedLabel
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "session.registered", sessionID, nil, nil, nil, nil, payload); err != nil {
			return nil, err
		}
		attestation := sessionLaneAttestation(ctx, tx, repositoryID, sessionID)
		// RFC 0096 V2 / #135: mint a capability token BOUND to this session so a
		// supervised lane can become session-bound and the cross-session
		// impersonation enforcement fully bites. Returned to the caller; lane-env
		// wiring to actually USE it is the remaining follow-up.
		boundToken, err := mintSessionBoundToken(ctx, tx, repositoryID, sessionID)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"session_id":               sessionID,
			"slug":                     slug,
			"lane_attestation":         attestation["state"],
			"lane_attestation_reason":  attestation["reason"],
			"operator_label":           recordedLabel,
			"supervise_hint":           "striatum supervise start --session-id " + sessionID,
			"session_capability_token": boundToken,
		}, nil
	})
}

func workflowLaneCapabilities(lane map[string]any) []string {
	capabilities := []string{}
	for _, item := range asList(lane["capabilities"]) {
		capabilities = append(capabilities, fmt.Sprint(item))
	}
	return normalizeSessionCapabilities(capabilities)
}

func normalizeSessionCapabilities(capabilities []string) []string {
	result := make([]string, 0, len(capabilities))
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		cleaned := strings.TrimSpace(capability)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}
	return result
}

type activeSessionTerminalUpdate struct {
	RepositoryID string
	SessionID    string
	State        string
	Reason       string
}

func markActiveSessionTerminal(ctx context.Context, runner any, update activeSessionTerminalUpdate) error {
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support session terminal update")
	}
	now := nowString()
	if err := exec.Exec(ctx, `
		UPDATE striatumd.sessions
		   SET state = $1, closed_at = $2, close_reason = $3
		 WHERE repository_id = $4 AND session_id = $5 AND state = 'active'`,
		update.State, now, update.Reason, update.RepositoryID, update.SessionID); err != nil {
		return err
	}
	return nil
}

func HandleCloseSession(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	reason := strings.TrimSpace(stringParam(envelope, "reason"))
	// RFC 0101 Phase 3 Slice 1 (#121 ask #1): opt-in flag to return the closed
	// session's in-flight (running-limbo) job to the queue on the SAME attempt so
	// a fresh lane can pick it up, instead of leaving the job stranded.
	requeueJob := boolParam(envelope, "requeue_job")
	if sessionID == "" {
		return nil, rpc.NewError("schema_invalid", "session.close requires session_id", nil)
	}
	if reason == "" {
		return nil, rpc.NewError("invalid_transition", "session close reason must not be empty", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		session, err := rowByID(ctx, tx, repositoryID, "sessions", "session_id", sessionID, true)
		if err != nil {
			return nil, err
		}
		state := fmt.Sprint(session["state"])
		if state == "closed" || state == "expired" || state == "stopped" || state == "lost" {
			return map[string]any{
				"session_id":   sessionID,
				"run_id":       session["run_id"],
				"role_id":      session["role_id"],
				"lane_id":      session["lane_id"],
				"state":        state,
				"closed_at":    session["closed_at"],
				"close_reason": session["close_reason"],
				"note":         "session was already " + state,
			}, nil
		}
		active, err := oneRow(ctx, tx, `
			SELECT lease_id FROM striatumd.leases
			 WHERE repository_id = $1 AND owner_session_id = $2 AND state = 'active'
			 LIMIT 1`, repositoryID, sessionID)
		if err == nil {
			return nil, rpc.NewError("lease_error", fmt.Sprintf("session has an active lease (%s); release the lease (striatum release) before closing the session", active["lease_id"]), nil)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		now := nowString()
		if err := stopSupervisorsForTerminalSession(ctx, tx, terminalSessionSupervisorStop{
			RepositoryID:     repositoryID,
			RunID:            fmt.Sprint(session["run_id"]),
			SessionID:        sessionID,
			EndedAt:          now,
			Reason:           reason,
			StopReasonPrefix: "session close",
			EventSource:      "explicit_session_close",
		}); err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.sessions
			   SET state = 'closed', closed_at = $1, close_reason = $2
			 WHERE repository_id = $3 AND session_id = $4`, now, reason, repositoryID, sessionID); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, session["run_id"], "session.closed", sessionID, nil, nil, nil, nil, map[string]any{
			"session_id": sessionID,
			"role_id":    session["role_id"],
			"lane_id":    session["lane_id"],
			"reason":     reason,
			"source":     "explicit",
		}); err != nil {
			return nil, err
		}

		var repoRoot string
		if err := tx.QueryRow(ctx, `SELECT repo_root FROM striatumd.repositories WHERE repository_id = $1`, repositoryID).Scan(&repoRoot); err == nil && repoRoot != "" {
			if rows, err := queryRows(ctx, tx, `SELECT DISTINCT supervisor_id FROM striatumd.process_supervisor_pointers WHERE repository_id = $1 AND session_id = $2`, repositoryID, sessionID); err == nil {
				for _, row := range rows {
					if sID, ok := row["supervisor_id"].(string); ok && sID != "" {
						agentloop.CleanupGeminiSettings(repoRoot, sID)
					}
				}
			}
			// #129: remove the Claude lane's ephemeral .claude/scheduled_tasks.lock
			// on session close too (mirrors the #62 .gemini/settings.json cleanup).
			agentloop.CleanupClaudeScheduledTasksLock(repoRoot)
		}

		result := map[string]any{
			"session_id":   sessionID,
			"run_id":       session["run_id"],
			"role_id":      session["role_id"],
			"lane_id":      session["lane_id"],
			"state":        "closed",
			"closed_at":    now,
			"close_reason": reason,
		}

		// #121 ask #1: optionally return the session's in-flight job to the queue
		// on the same attempt. The active-lease check above guarantees the lease is
		// already released, so this is exactly the dead-lane running-limbo case.
		if requeueJob {
			inflight, ierr := queryRows(ctx, tx, `
				SELECT j.job_id, j.run_id, j.workflow_job_id, j.state,
				       j.role_id, j.lane_selector_json, j.max_attempts,
				       j.write_scope_json, j.current_message_id, j.current_lease_id
				  FROM striatumd.jobs j
				  JOIN striatumd.leases l
				    ON l.repository_id = j.repository_id
				   AND l.resource_type = 'job'
				   AND l.resource_id = j.job_id
				 WHERE j.repository_id = $1
				   AND l.owner_session_id = $2
				   AND j.state IN ('claimed', 'running', 'stale_lease')
				 ORDER BY l.acquired_at DESC
				 LIMIT 1
				 FOR UPDATE OF j`, repositoryID, sessionID)
			if ierr != nil {
				return nil, ierr
			}
			if len(inflight) == 0 {
				result["requeued_job"] = nil
				result["requeue_job_note"] = "no in-flight job found for this session to requeue"
			} else {
				job := inflight[0]
				opts := requeueSameAttemptOptions{
					operatorOverride: isRepoWrite(job),
					justification:    reason,
				}
				rq, rqerr := requeueJobSameAttempt(ctx, tx, repositoryID, job, opts)
				if rqerr != nil {
					return nil, rqerr
				}
				result["requeued_job"] = map[string]any{
					"job_id":              job["job_id"],
					"workflow_job_id":     job["workflow_job_id"],
					"message_id":          rq.messageID,
					"repo_write":          isRepoWrite(job),
					"already_reclaimable": rq.alreadyReclaimable,
				}
			}
		}

		return result, nil
	})
}

func HandleSessionReport(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	reportKind := strings.TrimSpace(stringParam(envelope, "report_kind"))
	phase := strings.TrimSpace(stringParam(envelope, "phase"))
	message := strings.TrimSpace(stringParam(envelope, "message"))
	blockerKind := strings.TrimSpace(stringParam(envelope, "blocker_kind"))
	if sessionID == "" || reportKind == "" || phase == "" {
		return nil, rpc.NewError("schema_invalid", "session.report requires session_id, report_kind, and phase", nil)
	}
	if !validSessionReportKind(reportKind) {
		return nil, rpc.NewError("schema_invalid", "session.report report_kind must be one of ready, heartbeat, question, escalate", nil)
	}
	if !validSessionReportPhase(phase) {
		return nil, rpc.NewError("schema_invalid", "session.report phase must be one of discovery, await_packet, lease_held, between_packets", nil)
	}
	if len(message) > 280 {
		return nil, rpc.NewError("schema_invalid", "session.report message must be at most 280 characters", nil)
	}
	if blockerKind != "" && !validSessionReportBlockerKind(blockerKind) {
		return nil, rpc.NewError("schema_invalid", "session.report blocker_kind must be one of auth_prompt, model_timeout, missing_input, other", nil)
	}
	if (reportKind == "ready" || reportKind == "heartbeat") && blockerKind != "" {
		return nil, rpc.NewError("schema_invalid", "session.report blocker_kind is only valid for question or escalate reports", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		session, err := rowByID(ctx, tx, repositoryID, "sessions", "session_id", sessionID, true)
		if err != nil {
			return nil, err
		}
		runID := session["run_id"]
		payload := map[string]any{
			"session_id":  sessionID,
			"report_kind": reportKind,
			"phase":       phase,
		}
		if message != "" {
			payload["message"] = message
		}
		if blockerKind != "" {
			payload["blocker_kind"] = blockerKind
		}
		if err := sessionliveness.Record(ctx, tx, repositoryID, sessionID, sessionReportActivityColumn(reportKind)); err != nil {
			return nil, err
		}
		// W3 (#125): work.ack is non-substitutable. If the lane reports while it
		// still holds a claimed-but-unacked work packet, flag the outstanding ack so
		// the control plane and the pane agree — a session.report does not advance a
		// claimed packet to running (only work.ack does). Recording the report
		// preserves liveness; the flag prevents the report from silently masking the
		// stall a lane causes by reporting ready instead of acking.
		unacked, err := unackedClaimedPacketForSession(ctx, tx, repositoryID, sessionID)
		if err != nil {
			return nil, err
		}
		if unacked != nil {
			payload["unacked_packet"] = unacked
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "session.reported", sessionID, nil, nil, nil, nil, payload); err != nil {
			return nil, err
		}
		result := map[string]any{
			"status":      "reported",
			"session_id":  sessionID,
			"run_id":      runID,
			"report_kind": reportKind,
			"phase":       phase,
		}
		if unacked != nil {
			result["unacked_packet"] = unacked
		}
		return result, nil
	})
}

// unackedClaimedPacketForSession returns a flag describing a work packet the
// session has claimed (delivered) but not yet work.ack'd, or nil if none. W3
// (#125): a session.report must not substitute for work.ack — surfacing the
// outstanding packet keeps the control plane and the pane in agreement so a lane
// that reports readiness instead of acking does not stall silently.
func unackedClaimedPacketForSession(ctx context.Context, runner any, repositoryID, sessionID string) (map[string]any, error) {
	row, err := oneRow(ctx, runner, `
		SELECT qm.message_id, qm.job_id, l.lease_id
		  FROM striatumd.queue_messages qm
		  JOIN striatumd.leases l
		    ON l.repository_id = qm.repository_id AND l.resource_type = 'job'
		   AND l.resource_id = qm.job_id AND l.state = 'active'
		 WHERE qm.repository_id = $1 AND l.owner_session_id = $2
		   AND qm.kind = 'work' AND qm.state = 'claimed'
		 ORDER BY qm.created_at ASC, qm.message_id ASC
		 LIMIT 1`, repositoryID, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message_id": fmt.Sprint(row["message_id"]),
		"job_id":     fmt.Sprint(row["job_id"]),
		"lease_id":   fmt.Sprint(row["lease_id"]),
		"guidance":   "you hold a claimed work packet that is not yet acknowledged; call work.ack(message_id, lease_id) before reporting ready — session.report does not acknowledge work",
	}, nil
}

func sessionReportActivityColumn(reportKind string) string {
	switch reportKind {
	case "ready":
		return sessionliveness.LastSessionReadyAt
	case "heartbeat":
		return sessionliveness.LastSessionHeartbeatAt
	case "question":
		return sessionliveness.LastSessionQuestionAt
	case "escalate":
		return sessionliveness.LastSessionEscalateAt
	default:
		return sessionliveness.LastMCPRequestAt
	}
}

func validSessionReportKind(value string) bool {
	switch value {
	case "ready", "heartbeat", "question", "escalate":
		return true
	default:
		return false
	}
}

func validSessionReportPhase(value string) bool {
	switch value {
	case "discovery", "await_packet", "lease_held", "between_packets":
		return true
	default:
		return false
	}
}

func validSessionReportBlockerKind(value string) bool {
	switch value {
	case "auth_prompt", "model_timeout", "missing_input", "other":
		return true
	default:
		return false
	}
}

func HandleAckWork(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	messageID := stringParam(envelope, "message_id")
	leaseID := stringParam(envelope, "lease_id")
	if sessionID == "" || messageID == "" || leaseID == "" {
		return nil, rpc.NewError("schema_invalid", "work.ack requires session_id, message_id, and lease_id", nil)
	}
	// RFC 0096 V2 / #135: a bound token may only ack its own session's work.
	if _, err := enforceSessionBinding(ctx, sessionID); err != nil {
		return nil, err
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		message, err := rowByID(ctx, tx, repositoryID, "queue_messages", "message_id", messageID, true)
		if err != nil {
			return nil, err
		}
		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", fmt.Sprint(message["job_id"]), true)
		if err != nil {
			return nil, err
		}
		if _, err := activeLeaseFor(ctx, tx, repositoryID, leaseID, sessionID, fmt.Sprint(job["job_id"])); err != nil {
			return nil, err
		}
		if fmt.Sprint(message["state"]) == "acked" {
			return map[string]any{"status": "acked", "job_id": job["job_id"]}, nil
		}
		if fmt.Sprint(message["state"]) != "claimed" || fmt.Sprint(job["state"]) != "claimed" {
			return nil, rpc.NewError("invalid_transition", "work must be claimed before ack", nil)
		}
		now := nowString()
		if err := tx.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'acked', acked_at = $1, updated_at = $2
			 WHERE repository_id = $3 AND message_id = $4`, now, now, repositoryID, messageID); err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.jobs
			   SET state = 'running', started_at = $1
			 WHERE repository_id = $2 AND job_id = $3`, now, repositoryID, job["job_id"]); err != nil {
			return nil, err
		}
		if err := sessionliveness.Record(ctx, tx, repositoryID, sessionID, sessionliveness.LastAckAt); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, job["run_id"], "queue.acked", sessionID, job["job_id"], messageID, nil, leaseID, nil); err != nil {
			return nil, err
		}
		return map[string]any{"status": "acked", "job_id": job["job_id"]}, nil
	})
}

func HandleHeartbeat(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	leaseID := stringParam(envelope, "lease_id")
	extendSeconds := intParam(envelope, "extend_seconds", 1800)
	if sessionID == "" || leaseID == "" {
		return nil, rpc.NewError("schema_invalid", "work.heartbeat requires session_id and lease_id", nil)
	}
	// RFC 0096 V2 / #135: a bound token may only heartbeat its own session's lease.
	if _, err := enforceSessionBinding(ctx, sessionID); err != nil {
		return nil, err
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		lease, err := activeLeaseFor(ctx, tx, repositoryID, leaseID, sessionID, "")
		if err != nil {
			return nil, err
		}
		now := nowString()
		expiresAt := expiresAfter(extendSeconds)
		if err := tx.Exec(ctx, `
			UPDATE striatumd.sessions
			   SET last_heartbeat_at = $1
			 WHERE repository_id = $2 AND session_id = $3`, now, repositoryID, sessionID); err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.leases
			   SET last_heartbeat_at = $1, expires_at = $2
			 WHERE repository_id = $3 AND lease_id = $4`, now, expiresAt, repositoryID, leaseID); err != nil {
			return nil, err
		}
		if err := sessionliveness.Record(ctx, tx, repositoryID, sessionID, sessionliveness.LastWorkHeartbeatAt); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, lease["run_id"], "lease.heartbeat", sessionID, lease["resource_id"], nil, nil, leaseID, map[string]any{"expires_at": expiresAt}); err != nil {
			return nil, err
		}
		return map[string]any{"status": "heartbeat", "expires_at": expiresAt}, nil
	})
}

func HandleReleaseWork(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	messageID := stringParam(envelope, "message_id")
	leaseID := stringParam(envelope, "lease_id")
	reason := stringParam(envelope, "reason")
	if reason == "" {
		reason = "released"
	}
	requeue := boolParam(envelope, "requeue")
	// #108: an operator-inspected transfer of a live repo-write claim to a fresh
	// session. Unlike a plain release (which blocks a repo-write job) or
	// run.retry_job (which bumps the attempt), --transfer returns the job to the
	// queue with the SAME attempt so a fresh session can claim it without the work
	// looking like a retry/revision in run history.
	transfer := boolParam(envelope, "transfer")
	if sessionID == "" || leaseID == "" {
		return nil, rpc.NewError("schema_invalid", "work.release requires session_id and lease_id", nil)
	}
	// RFC 0096 V2 / #135: a bound token may only release its own session's work.
	if _, err := enforceSessionBinding(ctx, sessionID); err != nil {
		return nil, err
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		var message map[string]any
		var err error
		if messageID != "" {
			message, err = rowByID(ctx, tx, repositoryID, "queue_messages", "message_id", messageID, true)
			if err != nil {
				return nil, err
			}
		} else {
			message, err = oneRow(ctx, tx, `
				SELECT *
				  FROM striatumd.queue_messages
				 WHERE repository_id = $1 AND current_lease_id = $2
				 LIMIT 1
				 FOR UPDATE`, repositoryID, leaseID)
			if err != nil {
				return nil, err
			}
			messageID = fmt.Sprint(message["message_id"])
		}
		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", fmt.Sprint(message["job_id"]), true)
		if err != nil {
			return nil, err
		}
		if _, err := activeLeaseFor(ctx, tx, repositoryID, leaseID, sessionID, fmt.Sprint(job["job_id"])); err != nil {
			return nil, err
		}
		repoWrite := isRepoWrite(job)
		if requeue && repoWrite && !transfer {
			return nil, rpc.NewError("invalid_transition", "release --requeue is not supported for repo_write jobs; use release --transfer (an operator-inspected transfer that preserves the attempt) or recovery requeue-stale --force", nil)
		}
		now := nowString()
		jobState := "blocked"
		messageState := "blocked"
		// A non-repo-write requeue, or an operator-inspected repo-write --transfer,
		// returns the work to the queue with the same attempt (#108).
		if (requeue && !repoWrite) || transfer {
			jobState = "queued"
			messageState = "pending"
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.leases
			   SET state = 'released', released_at = $1, release_reason = $2
			 WHERE repository_id = $3 AND lease_id = $4`, now, reason, repositoryID, leaseID); err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.jobs
			   SET state = $1, current_lease_id = NULL
			 WHERE repository_id = $2 AND job_id = $3`, jobState, repositoryID, job["job_id"]); err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = $1, current_lease_id = NULL, updated_at = $2
			 WHERE repository_id = $3 AND message_id = $4`, messageState, now, repositoryID, messageID); err != nil {
			return nil, err
		}
		if err := sessionliveness.Record(ctx, tx, repositoryID, sessionID, sessionliveness.LastWorkReleaseAt); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, job["run_id"], "lease.released", sessionID, job["job_id"], messageID, nil, leaseID, map[string]any{"reason": reason, "job_state": jobState, "transfer": transfer}); err != nil {
			return nil, err
		}
		result := map[string]any{"status": "released", "job_state": jobState}
		if jobState == "queued" {
			result["transferred"] = true
			result["attempt_preserved"] = true
			result["attempt"] = job["attempt"]
			result["next_actions"] = []string{"register_or_select_fresh_session", "claim_available_work"}
		} else if repoWrite {
			// #108: a plain repo-write release blocks the job, which reads like a
			// failure/revision. Point the operator at the attempt-preserving
			// transfer path so they don't reach for run.retry_job (which bumps the
			// attempt and pollutes run history).
			result["note"] = "repo-write job released to blocked. For a clean operator transfer that returns it to the queue with the SAME attempt, re-run release with --transfer (or use `recovery requeue-stale --run-id <run> --job-id <job> --force --justification \"...\"`) rather than `run retry-job`."
			result["next_actions"] = []string{"release_with_transfer_preserves_attempt"}
		}
		return result, nil
	})
}

func HandleBlockWork(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	request, err := normalizeBlockWork(envelope)
	if err != nil {
		return nil, err
	}
	// RFC 0096 V2 / #135: a bound token may only block its own session's work.
	if _, err := enforceSessionBinding(ctx, request.sessionID); err != nil {
		return nil, err
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", request.jobID, true)
		if err != nil {
			return nil, err
		}
		if _, err := activeLeaseFor(ctx, tx, repositoryID, request.leaseID, request.sessionID, request.jobID); err != nil {
			return nil, err
		}
		blockerID, err := newID("blk")
		if err != nil {
			return nil, err
		}
		now := nowString()
		state := "blocked"
		if request.severity == "human_checkpoint" {
			state = "waiting_human"
		}
		payload := blockerPayload(request.severity, request.kind, request.description)
		payloadArg, err := db.JSONBArg(tx, payload)
		if err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			INSERT INTO striatumd.blockers (
			  repository_id, blocker_id, run_id, job_id, session_id, severity,
			  blocker_kind, description, state, created_at, payload_json
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'open',$9,$10::jsonb)`,
			repositoryID, blockerID, job["run_id"], request.jobID, request.sessionID,
			request.severity, request.kind, request.description, now, payloadArg,
		); err != nil {
			return nil, err
		}
		if isEscalation(request.severity, request.kind) {
			if err := tx.Exec(ctx, `
				INSERT INTO striatumd.escalation_inbox (
				  repository_id, escalation_id, run_id, job_id, session_id,
				  blocker_id, blocker_kind, severity, state, created_at, payload_json
				)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'pending',$9,$10::jsonb)`,
				repositoryID, blockerID, job["run_id"], request.jobID, request.sessionID,
				blockerID, request.kind, request.severity, now, payloadArg,
			); err != nil {
				return nil, err
			}
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.jobs
			   SET state = $1, current_lease_id = NULL
			 WHERE repository_id = $2 AND job_id = $3`, state, repositoryID, request.jobID); err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.leases
			   SET state = 'released', released_at = $1, release_reason = 'blocked'
			 WHERE repository_id = $2 AND lease_id = $3`, now, repositoryID, request.leaseID); err != nil {
			return nil, err
		}
		messageID := nullable(job["current_message_id"])
		if messageID != nil {
			if err := tx.Exec(ctx, `
				UPDATE striatumd.queue_messages
				   SET state = 'blocked', current_lease_id = NULL
				 WHERE repository_id = $1 AND message_id = $2`, repositoryID, messageID); err != nil {
				return nil, err
			}
		}
		if _, err := appendEvent(ctx, tx, repositoryID, job["run_id"], "job.blocked", request.sessionID, request.jobID, nil, nil, request.leaseID, map[string]any{
			"blocker_id":             blockerID,
			"severity":               request.severity,
			"blocker_kind":           request.kind,
			"is_escalation":          payload["is_escalation"],
			"payload_schema_version": payload["schema_version"],
		}); err != nil {
			return nil, err
		}
		if err := sessionliveness.Record(ctx, tx, repositoryID, request.sessionID, sessionliveness.LastWorkBlockAt); err != nil {
			return nil, err
		}
		if state == "waiting_human" {
			if err := markJobTerminal(ctx, tx, repositoryID, fmt.Sprint(job["run_id"]), request.jobID); err != nil {
				return nil, err
			}
		}
		return map[string]any{"status": "blocked", "blocker_id": blockerID}, nil
	})
}

// completedByVerdict reports whether the job already reached its completed
// state via its verdict-capable path — exactly the case the #127
// already_completed no-op in HandleCompleteWork answers. The peek runs before
// the RFC 0104 per-run advisory lock, so it must not take a row lock; it is
// lenient — a missing job row reports false so the inactive-session guidance
// (and downstream not-found handling) still applies.
func completedByVerdict(ctx context.Context, runner any, repositoryID string, jobID string) bool {
	job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, false)
	if err != nil {
		return false
	}
	return isVerdictCapableJobType(fmt.Sprint(job["job_type"])) && fmt.Sprint(job["state"]) == "completed"
}

func HandleCompleteWork(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	jobID := stringParam(envelope, "job_id")
	leaseID := stringParam(envelope, "lease_id")
	summary := stringParam(envelope, "summary")
	if sessionID == "" || jobID == "" || leaseID == "" {
		return nil, rpc.NewError("schema_invalid", "work.complete requires session_id, job_id, and lease_id", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0096 V2 / #135: a bound token may only complete its own session's work.
		if _, err := enforceSessionBindingForSession(ctx, tx, repositoryID, sessionID, "work.complete"); err != nil {
			return nil, err
		}
		// A verdict that completes this job can close this very session in the
		// same stroke (maybeCompleteRun -> closeRemainingSessions), so the
		// lane's follow-up work.complete legitimately arrives from a
		// just-closed session. The #127 idempotent already_completed answer
		// below must win over the inactive-session refusal; the gate only
		// applies to a complete that would still change state.
		if !completedByVerdict(ctx, tx, repositoryID, jobID) {
			if err := enforceActiveActingSession(ctx, tx, repositoryID, sessionID, jobID, "work.complete"); err != nil {
				return nil, err
			}
		}
		// RFC 0104: per-run advisory lock first — work.complete can complete the run
		// (maybeCompleteRun -> closeRemainingSessions: runs -> sessions), which
		// inverts against the claim path; serialize on the per-run lock.
		if err := lockRunForJob(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		}
		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
		if err != nil {
			return nil, err
		}
		jobType := fmt.Sprint(job["job_type"])
		// #127: review/phase_synthesis jobs complete via review.submit/verdict,
		// which records the verdict and releases the lease. A lane that then
		// follows the generic packet instruction and calls work.complete would
		// otherwise hit a misleading "lease is not active" (the lease was just
		// released by the verdict). Return an idempotent no-op for an
		// already-completed verdict job instead — the job is done, complete is a
		// no-op here, and this is reported clearly rather than as a lease failure.
		if isVerdictCapableJobType(jobType) && fmt.Sprint(job["state"]) == "completed" {
			label := verdictCapableJobLabel(jobType)
			return map[string]any{
				"status":       "already_completed",
				"job_id":       jobID,
				"completed_by": "verdict",
				"note":         fmt.Sprintf("%s jobs complete via submit-review; work.complete is a no-op once the verdict is recorded", label),
			}, nil
		}
		if _, err := activeLeaseFor(ctx, tx, repositoryID, leaseID, sessionID, jobID); err != nil {
			return nil, err
		}
		if err := ensureWorkSessionBackend(ctx, tx, repositoryID, sessionID, "complete"); err != nil {
			return nil, err
		}
		if isVerdictCapableJobType(jobType) {
			label := verdictCapableJobLabel(jobType)
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("%s jobs must use submit-review instead of complete", label), nil)
		}
		if fmt.Sprint(job["state"]) != "running" {
			return nil, rpc.NewError("invalid_transition", "job must be running before completion", nil)
		}
		applyFrozenAttemptWriteScope(ctx, tx, repositoryID, job, leaseID)
		if err := enforceWriteScopeClean(ctx, tx, repositoryID, job); err != nil {
			return nil, err
		}
		if err := verifyRequiredArtifacts(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		}
		if err := ensurePublishedArtifactsDurableWithPorter(ctx, tx, repositoryID, job, "work.complete"); err != nil {
			return nil, err
		}
		// #287: opt-in source-change publish. When the job's write_scope sets
		// publish_source_changes=true, commit the lane's in-scope source edits to the
		// run branch alongside its declared artifacts, before the anchor below
		// advances the run ref over both.
		publishedSourcePaths, err := publishWorktreeSourceChanges(ctx, tx, repositoryID, job)
		if err != nil {
			return nil, err
		}
		// #297 (D201): make the silent-drop loud. Files the lane wrote inside its
		// write_scope but neither declared as expected_artifacts nor published via the
		// source-publish path above would otherwise be committed only if the lane
		// happened to `git add` them — and stranded untracked (lost to worktree gc)
		// otherwise. Compute the residual and surface it prominently in the completion
		// result + a provenance event. This is non-breaking by design: it warns, it
		// never refuses, so existing flows still complete.
		strandedInScopePaths, err := detectStrandedInScopePaths(ctx, tx, repositoryID, job, publishedSourcePaths)
		if err != nil {
			return nil, err
		}
		now := nowString()
		messageID := nullable(job["current_message_id"])
		if len(publishedSourcePaths) > 0 {
			// Bound the event payload (the events table caps row size); the count
			// stays exact and a truncation flag is set when the list is clipped.
			const maxEventPaths = 500
			eventPaths := publishedSourcePaths
			payload := map[string]any{"count": len(publishedSourcePaths)}
			if len(eventPaths) > maxEventPaths {
				eventPaths = append([]string(nil), eventPaths[:maxEventPaths]...)
				payload["paths_truncated"] = true
			}
			payload["paths"] = eventPaths
			if _, err := appendEvent(ctx, tx, repositoryID, job["run_id"], "job.source_changes_published", sessionID, jobID, messageID, nil, leaseID, payload); err != nil {
				return nil, err
			}
		}
		// #297 (D201): record the stranded in-scope paths as durable provenance so the
		// drop is legible in the event chain (not just the transient RPC result).
		if len(strandedInScopePaths) > 0 {
			const maxEventPaths = 500
			eventPaths := strandedInScopePaths
			payload := map[string]any{"count": len(strandedInScopePaths)}
			if len(eventPaths) > maxEventPaths {
				eventPaths = append([]string(nil), eventPaths[:maxEventPaths]...)
				payload["paths_truncated"] = true
			}
			payload["paths"] = eventPaths
			if _, err := appendEvent(ctx, tx, repositoryID, job["run_id"], "job.in_scope_paths_stranded", sessionID, jobID, messageID, nil, leaseID, payload); err != nil {
				return nil, err
			}
		}
		anchorPayload, err := anchorActiveWorktreeForJob(ctx, tx, repositoryID, job)
		if err != nil {
			return nil, err
		}
		if anchorPayload != nil {
			if _, err := appendEvent(ctx, tx, repositoryID, job["run_id"], "job.commits_anchored", sessionID, jobID, messageID, nil, leaseID, anchorPayload); err != nil {
				return nil, err
			}
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.jobs
			   SET state = 'completed', completed_at = $1, current_lease_id = NULL
			 WHERE repository_id = $2 AND job_id = $3`, now, repositoryID, jobID); err != nil {
			return nil, err
		}
		if messageID != nil {
			if err := tx.Exec(ctx, `
				UPDATE striatumd.queue_messages
				   SET state = 'completed', completed_at = $1, updated_at = $2,
				       current_lease_id = NULL
				 WHERE repository_id = $3 AND message_id = $4`, now, now, repositoryID, messageID); err != nil {
				return nil, err
			}
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.leases
			   SET state = 'released', released_at = $1, release_reason = 'completed'
			 WHERE repository_id = $2 AND lease_id = $3`, now, repositoryID, leaseID); err != nil {
			return nil, err
		}
		if err := sessionliveness.Record(ctx, tx, repositoryID, sessionID, sessionliveness.LastWorkCompleteAt); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, job["run_id"], "job.completed", sessionID, jobID, messageID, nil, leaseID, map[string]any{"summary": summary}); err != nil {
			return nil, err
		}
		// #175: a job that earlier reported an autonomous blocker (e.g. a
		// write_scope_guard_conflict that was preserved/restored out-of-band) and
		// then completed left the blocker open with no resolution path —
		// checkpoint.resolve is human-only and recovery.resume is
		// process-adapter-only — so a completed run kept presenting it as an
		// active open_blocker/next_action. Resolve the completing job's open
		// autonomous (non-human, non-escalation) blockers here, inside the same
		// transaction. Human-checkpoint and escalation-class blockers are left
		// untouched: they require explicit human adjudication.
		if err := resolveAutonomousBlockersOnCompletion(ctx, tx, repositoryID, fmt.Sprint(job["run_id"]), jobID, sessionID, now); err != nil {
			return nil, err
		}
		// RFC 0082 §5: an interrogable job's session does NOT close on
		// completion. It enters the awaiting_interrogation phase — staying live
		// with its context preserved so a reviewer can interrogate it — until
		// interrogation.close or a bounded idle timeout closes the session.
		interrogable, err := jobIsInterrogable(ctx, tx, repositoryID, job)
		if err != nil {
			return nil, err
		}
		if interrogable {
			if _, err := appendEvent(ctx, tx, repositoryID, job["run_id"], "session.awaiting_interrogation", sessionID, jobID, nil, nil, nil, map[string]any{
				"session_id":      sessionID,
				"workflow_job_id": job["workflow_job_id"],
				"attempt":         job["attempt"],
			}); err != nil {
				return nil, err
			}
		}
		if err := markJobTerminal(ctx, tx, repositoryID, fmt.Sprint(job["run_id"]), jobID); err != nil {
			return nil, err
		}
		if err := maybeEnqueueDownstream(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		}
		if err := maybeCompleteRun(ctx, tx, repositoryID, fmt.Sprint(job["run_id"])); err != nil {
			return nil, err
		}
		result := map[string]any{"status": "completed", "job_id": jobID}
		if anchorPayload != nil {
			result["commit_anchor"] = anchorPayload
		}
		if interrogable {
			result["interrogable"] = true
			result["session_phase"] = "awaiting_interrogation"
		}
		// #297 (D201): surface stranded in-scope authored paths prominently. The job
		// still completes (non-breaking warning), but the operator/agent gets the exact
		// list of files written inside write_scope that did NOT reach the run branch —
		// declare them as expected_artifacts or set write_scope.publish_source_changes
		// so they are committed, or remove them if they were not meant to ship.
		if len(strandedInScopePaths) > 0 {
			result["stranded_in_scope_paths"] = strandedInScopePaths
			result["warnings"] = append(asStringSlice(result["warnings"]), fmt.Sprintf(
				"%d in-scope file(s) the lane wrote were neither declared as expected_artifacts nor published to the run branch and will be lost on worktree gc: %s. Declare them as expected_artifacts, set write_scope.publish_source_changes=true to commit them, or remove them if unintended.",
				len(strandedInScopePaths), strings.Join(strandedInScopePaths, ", "),
			))
		}
		return result, nil
	})
}

// resolveAutonomousBlockersOnCompletion resolves the completing job's open
// autonomous blockers (#175). An "autonomous" blocker is one that is neither a
// human_checkpoint nor an escalation-class blocker (those require explicit human
// adjudication and are left open). Each resolved blocker emits a
// blocker.resolved_on_completion event so the resolution is legible in the
// durable event chain, and any matching escalation_inbox row is resolved too so
// status/dashboard stop presenting a completed run's stale blocker as an active
// next action.
//
// The runner is accepted as `any` (not db.TxRunner) so every job.completed
// path can reuse it: HandleCompleteWork passes the live tx, while the recovery
// completion paths (#304: completeAutoRecoveredJob, completeRecoveredJob,
// completeAutoFinalizedJob) pass the same tx through their `runner any`
// parameter. Without this, a blocked-severity blocker raised on an earlier
// attempt that a recovery/retry path later completed stayed open forever with
// no resolution event — the dangling-blocker bug in #304.
func resolveAutonomousBlockersOnCompletion(ctx context.Context, runner any, repositoryID, runID, jobID, sessionID, now string) error {
	tx, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	openBlockers, err := queryRows(ctx, runner, `
		SELECT blocker_id, severity, blocker_kind
		  FROM striatumd.blockers
		 WHERE repository_id = $1 AND job_id = $2 AND state = 'open'`, repositoryID, jobID)
	if err != nil {
		return err
	}
	resolvedRecoveryExhausted := false
	for _, blocker := range openBlockers {
		reason := "job_completed"
		if isEscalationClassBlocker(blocker) {
			// #207 (part C): a recovery_exhausted blocker means "autonomous recovery
			// could not reclaim THIS job". When the same job subsequently completes a
			// genuine attempt, that premise is moot — the recovery exhaustion was a
			// false alarm the job itself just disproved — so auto-close ONLY this
			// escalation-by-exhaustion blocker against the completing job. Every other
			// escalation-class kind (human_checkpoint and the genuinely-human kinds:
			// ambiguous_goal, missing_authority, contradicting_decisions, etc.) still
			// needs human adjudication and is left untouched.
			if fmt.Sprint(blocker["blocker_kind"]) != recoveryExhaustedBlockerKind {
				continue
			}
			reason = "recovery_exhausted_moot_after_job_completed"
			resolvedRecoveryExhausted = true
		}
		blockerID := fmt.Sprint(blocker["blocker_id"])
		if err := tx.Exec(ctx, `
			UPDATE striatumd.blockers
			   SET state = 'resolved', resolved_at = $1
			 WHERE repository_id = $2 AND blocker_id = $3`, now, repositoryID, blockerID); err != nil {
			return err
		}
		// Resolve a matching escalation_inbox row. Plain autonomous blockers
		// normally have none (defensive), but a recovery_exhausted blocker DOES
		// have one (escalateExhaustedJobs inserts it with the same id), so this is
		// load-bearing for the #207 case.
		if err := tx.Exec(ctx, `
			UPDATE striatumd.escalation_inbox
			   SET state = 'resolved', resolved_at = $1
			 WHERE repository_id = $2 AND escalation_id = $3 AND state <> 'resolved'`, now, repositoryID, blockerID); err != nil {
			return err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "blocker.resolved_on_completion", sessionID, jobID, nil, nil, nil, map[string]any{
			"blocker_id":   blockerID,
			"severity":     blocker["severity"],
			"blocker_kind": blocker["blocker_kind"],
			"reason":       reason,
		}); err != nil {
			return err
		}
	}

	// #207 (part C): if we just cleared the completing job's moot recovery_exhausted
	// escalation, restore the run from needs_operator to running — but ONLY when no
	// OTHER open escalation-class blocker remains anywhere in the run. A genuine
	// human_checkpoint or human escalation on a sibling job must keep the run
	// frozen; this only unfreezes a run whose sole reason for being needs_operator
	// was the now-disproven recovery exhaustion. Mirrors escalation.resolve's
	// needs_operator -> running restore (reads/escalation_resolve.go).
	if !resolvedRecoveryExhausted {
		return nil
	}
	remaining, err := queryRows(ctx, tx, `
		SELECT blocker_id, severity, blocker_kind
		  FROM striatumd.blockers
		 WHERE repository_id = $1 AND run_id = $2 AND state = 'open'`, repositoryID, runID)
	if err != nil {
		return err
	}
	for _, blocker := range remaining {
		if isEscalationClassBlocker(blocker) {
			// Another escalation still needs a human; leave the run needs_operator.
			return nil
		}
	}
	// Only flip + emit run.resumed when the run is actually needs_operator (a run
	// already running, or terminal/canceled, must not be revived). striatumd.runs
	// has no updated_at column, so the state flip is the only mutation.
	run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, false)
	if err != nil {
		return err
	}
	if fmt.Sprint(run["state"]) != "needs_operator" {
		return nil
	}
	if err := tx.Exec(ctx, `
		UPDATE striatumd.runs
		   SET state = 'running'
		 WHERE repository_id = $1 AND run_id = $2 AND state = 'needs_operator'`,
		repositoryID, runID); err != nil {
		return err
	}
	if _, err := appendEvent(ctx, tx, repositoryID, runID, "run.resumed", sessionID, jobID, nil, nil, nil, map[string]any{
		"reason":     "recovery_exhausted_moot_after_job_completed",
		"from_state": "needs_operator",
	}); err != nil {
		return err
	}
	return nil
}

// jobIsInterrogable reports whether the completed job's workflow definition
// declares interrogable: true. The flag lives in the run's workflow snapshot
// (no jobs-table column is added — owner-table ALTERs are forbidden per RFC
// 0079 §5), keyed by workflow_job_id.
func jobIsInterrogable(ctx context.Context, runner any, repositoryID string, job map[string]any) (bool, error) {
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return false, err
	}
	snapshot, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(run["workflow_snapshot_id"]), false)
	if err != nil {
		return false, err
	}
	workflow := asMap(snapshot["workflow_json"])
	workflowJobID := fmt.Sprint(job["workflow_job_id"])
	for _, item := range asList(workflow["jobs"]) {
		def := asMap(item)
		if fmt.Sprint(def["id"]) == workflowJobID {
			return def["interrogable"] == true, nil
		}
	}
	return false, nil
}

func isEscalation(severity string, kind string) bool {
	if severity == "human_checkpoint" {
		return true
	}
	switch kind {
	case "ambiguous_goal", "missing_authority", "contradicting_decisions",
		"no_available_reviewer_lane", "committee_stalemate", "override_required",
		"ai_self_declared",
		// RFC 0101 Phase 4: a daemon-authored escalation for a job the
		// autonomous recovery loop could not reclaim within its per-job budget.
		recoveryExhaustedBlockerKind:
		return true
	}
	return false
}

type blockWorkRequest struct {
	sessionID   string
	jobID       string
	leaseID     string
	kind        string
	severity    string
	description string
}

func normalizeBlockWork(envelope rpc.Envelope) (blockWorkRequest, error) {
	request := blockWorkRequest{
		sessionID:   strings.TrimSpace(stringParam(envelope, "session_id")),
		jobID:       strings.TrimSpace(stringParam(envelope, "job_id")),
		leaseID:     strings.TrimSpace(stringParam(envelope, "lease_id")),
		kind:        strings.TrimSpace(stringParam(envelope, "kind")),
		severity:    strings.TrimSpace(stringParam(envelope, "severity")),
		description: strings.TrimSpace(stringParam(envelope, "description")),
	}
	required := map[string]string{
		"session_id":  request.sessionID,
		"job_id":      request.jobID,
		"lease_id":    request.leaseID,
		"kind":        request.kind,
		"severity":    request.severity,
		"description": request.description,
	}
	for key, value := range required {
		if value == "" {
			return blockWorkRequest{}, rpc.NewError("schema_invalid", fmt.Sprintf("work.block field %s must be non-empty", key), nil)
		}
	}
	if request.severity != "blocked" && request.severity != "human_checkpoint" {
		return blockWorkRequest{}, rpc.NewError("schema_invalid", "work.block severity must be one of blocked, human_checkpoint", nil)
	}
	if !validBlockerKind(request.kind) {
		return blockWorkRequest{}, rpc.NewError("schema_invalid", "work.block kind must match ^[a-z0-9._-]{1,64}$", nil)
	}
	if len(request.description) > 8000 {
		return blockWorkRequest{}, rpc.NewError("schema_invalid", "work.block description must be at most 8000 characters", nil)
	}
	return request, nil
}

func validBlockerKind(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '.' || ch == '_' || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func blockerPayload(severity string, kind string, description string) map[string]any {
	trigger := any(nil)
	if severity == "human_checkpoint" {
		trigger = "human_checkpoint"
	} else if isEscalation(severity, kind) {
		trigger = "escalation_blocker_kind"
	}
	return map[string]any{
		"schema_version":     "striatum.blocker_payload.v1",
		"source":             "work.block",
		"severity":           severity,
		"blocker_kind":       kind,
		"description_format": "plain_text",
		"description_length": len(description),
		"is_escalation":      isEscalation(severity, kind),
		"escalation_trigger": trigger,
	}
}
