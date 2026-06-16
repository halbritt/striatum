package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/halbritt/striatum/go/pkg/agentloop"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
	"github.com/jackc/pgx/v5"
)

var cancelableJobStates = map[string]bool{
	"blocked":       true,
	"queued":        true,
	"claimed":       true,
	"running":       true,
	"stale_lease":   true,
	"waiting_human": true,
}

var processAdapterBlockerKinds = map[string]bool{
	"process_outputs_missing":           true,
	"process_review_verdict_missing":    true,
	"process_exit_nonzero":              true,
	"process_timeout_exceeded":          true,
	"process_lost_with_outputs_missing": true,
}

var processExitBlockerKinds = map[string]bool{
	"process_exit_nonzero":     true,
	"process_timeout_exceeded": true,
}

var writeScopeResumeBlockerKinds = map[string]bool{
	"write_scope.out_of_scope_dirty": true,
	"write_scope_guard_conflict":     true,
}

var recoveryDrainHelperEvents = drainHelperEvents

var terminalJobStates = map[string]bool{
	"completed": true,
	"failed":    true,
	"canceled":  true,
	"skipped":   true,
}

const abandonedRunAutoCancelReason = "abandoned_auto_canceled"

var abandonedRunAutoCancelAfter = 24 * time.Hour

func HandleRecoveryProcessReconcile(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "recovery.process_reconcile requires run_id", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first.
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, false)
		if err != nil {
			return nil, err
		}
		repoRoot := fmt.Sprint(run["repo_root"])
		rows, err := queryRows(ctx, tx, `
			SELECT pe.*,
			       p.metadata_json AS supervisor_metadata_json,
			       p.pid_start_time AS supervisor_pid_start_time,
			       p.pid AS supervisor_pid,
			       p.supervisor_id,
			       p.daemon_supervisor_id
			  FROM striatumd.process_executions pe
			  LEFT JOIN LATERAL (
			    SELECT ptr.metadata_json, ptr.pid_start_time, ptr.pid, ptr.supervisor_id, ptr.daemon_supervisor_id
			      FROM striatumd.process_supervisor_pointers ptr
			     WHERE ptr.repository_id = pe.repository_id
			       AND ptr.run_id = pe.run_id
			       AND ptr.session_id = pe.session_id
			     ORDER BY ptr.updated_at DESC, ptr.supervisor_id DESC
			     LIMIT 1
			  ) p ON true
			 WHERE pe.repository_id = $1
			   AND pe.run_id = $2
			   AND pe.state = 'running'
			 ORDER BY pe.started_at
			 FOR UPDATE OF pe`, repositoryID, runID)
		if err != nil {
			return nil, err
		}
		stillRunning := []map[string]any{}
		lost := []map[string]any{}
		now := nowString()
		for _, row := range rows {
			pid := intValue(row["pid"])
			metadata := asMap(row["supervisor_metadata_json"])
			probePID := pid
			if supervisorPID := intValue(row["supervisor_pid"]); supervisorPID > 0 {
				probePID = supervisorPID
			}
			expectedStart, _ := row["supervisor_pid_start_time"].(string)
			live := gosupervisor.ProbeLaneLiveness(ctx, tmuxRunnerForSupervisorMetadata(metadata), metadata, probePID, expectedStart)
			alive := live.Alive
			if live.Backed != "tmux" && pid > 0 {
				alive = pidAlive(pid)
			}
			if live.Class == string(gosupervisor.TmuxLivenessUnavailable) {
				stillRunning = append(stillRunning, map[string]any{
					"process_id": row["process_id"],
					"job_id":     row["job_id"],
					"pid":        pid,
					"started_at": row["started_at"],
					"liveness":   live.Class,
				})
				continue
			}
			if alive {
				stillRunning = append(stillRunning, map[string]any{
					"process_id": row["process_id"],
					"job_id":     row["job_id"],
					"pid":        pid,
					"started_at": row["started_at"],
					"liveness":   live.Class,
				})
				continue
			}
			processID := fmt.Sprint(row["process_id"])
			if err := tx.Exec(ctx, `
				UPDATE striatumd.process_executions
				   SET state = 'lost', ended_at = $1
				 WHERE repository_id = $2 AND process_id = $3`, now, repositoryID, processID); err != nil {
				return nil, err
			}
			var supervisorID string
			if s, ok := row["supervisor_id"].(string); ok {
				supervisorID = s
			}
			var daemonSupervisorID string
			if ds, ok := row["daemon_supervisor_id"].(string); ok {
				daemonSupervisorID = ds
			}
			if supervisorID != "" {
				stopReason := "unexpected child exit (lost)"
				if err := updateSupervisorState(ctx, tx, repositoryID, supervisorID, daemonSupervisorID, "stopped", now, 0, "", "", &now, &stopReason); err != nil {
					return nil, err
				}
				if err := markActiveSessionTerminal(ctx, tx, activeSessionTerminalUpdate{
					RepositoryID: repositoryID,
					SessionID:    fmt.Sprint(row["session_id"]),
					State:        "lost",
					Reason:       "process lost: " + stopReason,
				}); err != nil {
					return nil, err
				}
				agentloop.CleanupGeminiSettings(repoRoot, supervisorID)
				agentloop.CleanupClaudeScheduledTasksLock(repoRoot)
			}
			if _, err := appendEvent(ctx, tx, repositoryID, runID, "process.lost", row["session_id"], row["job_id"], nil, nil, row["lease_id"], map[string]any{
				"process_id": processID,
				"pid":        row["pid"],
				"reason":     live.Class,
			}); err != nil {
				return nil, err
			}
			job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", fmt.Sprint(row["job_id"]), true)
			if err != nil {
				return nil, err
			}
			blockerKind, err := evaluateAndBlockLostProcess(ctx, tx, repositoryID, job, fmt.Sprint(row["session_id"]), processID, row["command_json"])
			if err != nil {
				return nil, err
			}
			lost = append(lost, map[string]any{
				"process_id":   processID,
				"job_id":       row["job_id"],
				"pid":          row["pid"],
				"blocker_kind": blockerKind,
			})
		}
		return map[string]any{
			"run_id":               runID,
			"checked_count":        len(rows),
			"still_running":        stillRunning,
			"transitioned_to_lost": lost,
			"next_actions":         []string{"inspect_process_blockers", "resume_or_requeue_affected_work"},
		}, nil
	})
}

func HandleRecoveryResume(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	blockerID := stringParam(envelope, "blocker_id")
	if blockerID == "" {
		return nil, rpc.NewError("schema_invalid", "recovery.resume requires blocker_id", nil)
	}
	complete := boolParam(envelope, "complete")
	sessionID := stringParam(envelope, "session_id")
	summary := stringParam(envelope, "summary")
	force := boolParam(envelope, "force")
	extendSeconds := intParam(envelope, "extend_seconds", 900)
	if extendSeconds <= 0 {
		return nil, rpc.NewError("invalid_transition", "--extend-seconds must be positive", nil)
	}
	if complete && sessionID == "" {
		return nil, rpc.NewError("invalid_transition", "--complete requires --session-id", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first.
		if err := lockRunForBlocker(ctx, tx, repositoryID, blockerID); err != nil {
			return nil, err
		}
		blocker, err := rowByID(ctx, tx, repositoryID, "blockers", "blocker_id", blockerID, true)
		if err != nil {
			return nil, err
		}
		blockerKind := fmt.Sprint(blocker["blocker_kind"])
		if !processAdapterBlockerKinds[blockerKind] {
			if writeScopeResumeBlockerKinds[blockerKind] {
				return resumeWriteScopeBlocker(ctx, tx, writeScopeResumeRequest{
					RepositoryID:      repositoryID,
					Blocker:           blocker,
					BlockerKind:       blockerKind,
					SessionID:         sessionID,
					Summary:           summary,
					CompleteRequested: complete,
				})
			}
			return nil, rpc.NewError("invalid_transition", "recovery resume supports only process-adapter blockers", nil)
		}
		if nullable(blocker["job_id"]) == nil {
			return nil, rpc.NewError("invalid_transition", "process-adapter blocker is not job-bound", nil)
		}
		jobID := fmt.Sprint(blocker["job_id"])
		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
		if err != nil {
			return nil, err
		}
		runID := fmt.Sprint(job["run_id"])
		if fmt.Sprint(blocker["run_id"]) != runID {
			return nil, rpc.NewError("invalid_transition", "blocker does not belong to the job run", nil)
		}
		if fmt.Sprint(blocker["state"]) != "open" {
			return map[string]any{
				"status":           "already_resolved",
				"run_id":           runID,
				"job_id":           jobID,
				"workflow_job_id":  job["workflow_job_id"],
				"blocker_id":       blockerID,
				"blocker_kind":     blockerKind,
				"completed_inline": false,
				"next_actions":     []string{"inspect_job_state"},
			}, nil
		}
		jobState := fmt.Sprint(job["state"])
		if terminalJobStates[jobState] && force {
			now := nowString()
			if err := tx.Exec(ctx, `
				UPDATE striatumd.blockers
				   SET state = 'resolved', resolved_at = $1
				 WHERE repository_id = $2 AND blocker_id = $3`, now, repositoryID, blockerID); err != nil {
				return nil, err
			}
			if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.blocker_dismissed_terminal", nullable(sessionID), jobID, nil, nil, nil, map[string]any{
				"blocker_id":   blockerID,
				"blocker_kind": blockerKind,
				"job_state":    jobState,
				"reason":       "process-adapter blocker dismissed against terminal job (GH #7 legacy state)",
			}); err != nil {
				return nil, err
			}
			return map[string]any{
				"status":           "resolved_terminal_no_op",
				"run_id":           runID,
				"job_id":           jobID,
				"workflow_job_id":  job["workflow_job_id"],
				"blocker_id":       blockerID,
				"blocker_kind":     blockerKind,
				"completed_inline": false,
				"job_state":        jobState,
				"next_actions":     []string{"inspect_job_state"},
			}, nil
		}
		if jobState != "blocked" {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("job must be blocked before recovery resume (state=%q); pass --force for GH #7 legacy process-adapter blockers on terminal jobs", jobState), nil)
		}
		if processExitBlockerKinds[blockerKind] && !force {
			return nil, rpc.NewError("invalid_transition", blockerKind+" requires --force after operator inspection", nil)
		}
		missingPaths, reviewVerdictMissing, err := validateProcessOutputs(ctx, tx, repositoryID, job)
		if err != nil {
			return nil, err
		}
		if len(missingPaths) > 0 {
			return nil, rpc.NewError("invalid_transition", "process-adapter blocker still has missing required artifacts: "+strings.Join(missingPaths, ", "), nil)
		}
		if complete && reviewVerdictMissing {
			return nil, rpc.NewError("invalid_transition", "process-adapter blocker cannot complete while review verdict is missing", nil)
		}
		if reviewVerdictMissing && fmt.Sprint(job["job_type"]) != "review" {
			return nil, rpc.NewError("invalid_transition", "process-adapter blocker still has a missing review verdict", nil)
		}
		leaseID := nullable(job["current_lease_id"])
		if leaseID == nil {
			return nil, rpc.NewError("invalid_transition", "process-adapter blocker job has no current lease to resume", nil)
		}
		lease, err := rowByID(ctx, tx, repositoryID, "leases", "lease_id", fmt.Sprint(leaseID), true)
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(lease["state"]) != "active" {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("process-adapter blocker lease is not active (state=%q)", lease["state"]), nil)
		}
		leaseOwner := fmt.Sprint(lease["owner_session_id"])
		if sessionID != "" && sessionID != leaseOwner {
			return nil, rpc.NewError("invalid_transition", "session does not own the process-adapter lease", nil)
		}
		actorSessionID := sessionID
		if actorSessionID == "" {
			actorSessionID = fmt.Sprint(nullable(blocker["session_id"]))
		}
		if actorSessionID == "" || actorSessionID == "<nil>" {
			actorSessionID = leaseOwner
		}
		now := nowString()
		expiresAt := expiresAfter(extendSeconds)
		if err := tx.Exec(ctx, `
			UPDATE striatumd.leases
			   SET last_heartbeat_at = $1, expires_at = $2
			 WHERE repository_id = $3 AND lease_id = $4`, now, expiresAt, repositoryID, leaseID); err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.blockers
			   SET state = 'resolved', resolved_at = $1
			 WHERE repository_id = $2 AND blocker_id = $3`, now, repositoryID, blockerID); err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.jobs
			   SET state = 'running'
			 WHERE repository_id = $1 AND job_id = $2`, repositoryID, jobID); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.process_blocker_resolved", actorSessionID, jobID, nil, nil, leaseID, map[string]any{
			"blocker_id":             blockerID,
			"blocker_kind":           blockerKind,
			"verb":                   "recovery resume",
			"force":                  force,
			"completed_inline":       complete,
			"missing_artifact_paths": missingPaths,
			"review_verdict_missing": reviewVerdictMissing,
			"lease_extended_until":   expiresAt,
			"original_envelope":      asMap(blocker["payload_json"]),
		}); err != nil {
			return nil, err
		}
		result := map[string]any{
			"status":                 "resumed",
			"run_id":                 runID,
			"job_id":                 jobID,
			"workflow_job_id":        job["workflow_job_id"],
			"blocker_id":             blockerID,
			"blocker_kind":           blockerKind,
			"lease_id":               leaseID,
			"lease_extended_until":   expiresAt,
			"force":                  force,
			"completed_inline":       false,
			"review_verdict_missing": reviewVerdictMissing,
			"next_actions":           []string{"complete_job", "monitor_run_progress"},
		}
		if reviewVerdictMissing {
			result["next_actions"] = []string{"record_review_verdict"}
		}
		if !complete {
			return result, nil
		}
		completion, err := completeRecoveredJob(ctx, tx, repositoryID, jobID, actorSessionID, fmt.Sprint(leaseID), summary)
		if err != nil {
			return nil, err
		}
		result["status"] = "resumed_completed"
		result["completed_inline"] = true
		result["completion"] = completion
		result["next_actions"] = []string{"monitor_run_progress", "export_run_evidence"}
		return result, nil
	})
}

type writeScopeResumeRequest struct {
	RepositoryID      string
	Blocker           map[string]any
	BlockerKind       string
	SessionID         string
	Summary           string
	CompleteRequested bool
}

type writeScopeResumeTarget struct {
	JobID string
	RunID string
	Job   map[string]any
}

func resumeWriteScopeBlocker(ctx context.Context, tx db.TxRunner, request writeScopeResumeRequest) (map[string]any, error) {
	if nullable(request.Blocker["job_id"]) == nil {
		return nil, rpc.NewError("invalid_transition", "write-scope blocker is not job-bound", nil)
	}
	if fmt.Sprint(request.Blocker["state"]) != "open" {
		return alreadyResolvedWriteScopeResumeResult(request), nil
	}
	target, err := validateWriteScopeResumeTarget(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	if err := enforceWriteScopeClean(ctx, tx, request.RepositoryID, target.Job); err != nil {
		return nil, err
	}
	if err := resolveWriteScopeBlocker(ctx, tx, request, target); err != nil {
		return nil, err
	}
	requeue, err := requeueJobSameAttempt(ctx, tx, request.RepositoryID, target.Job, requeueSameAttemptOptions{
		operatorOverride: true,
		justification:    request.Summary,
		author:           request.SessionID,
	})
	if err != nil {
		return nil, err
	}
	return writeScopeResumeResult(request, target, requeue), nil
}

func alreadyResolvedWriteScopeResumeResult(request writeScopeResumeRequest) map[string]any {
	return map[string]any{
		"status":           "already_resolved",
		"run_id":           request.Blocker["run_id"],
		"job_id":           request.Blocker["job_id"],
		"blocker_id":       request.Blocker["blocker_id"],
		"blocker_kind":     request.BlockerKind,
		"completed_inline": false,
		"next_actions":     []string{"inspect_job_state"},
	}
}

func validateWriteScopeResumeTarget(ctx context.Context, tx db.TxRunner, request writeScopeResumeRequest) (writeScopeResumeTarget, error) {
	jobID := fmt.Sprint(request.Blocker["job_id"])
	job, err := rowByID(ctx, tx, request.RepositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		return writeScopeResumeTarget{}, err
	}
	runID := fmt.Sprint(job["run_id"])
	if fmt.Sprint(request.Blocker["run_id"]) != runID {
		return writeScopeResumeTarget{}, rpc.NewError("invalid_transition", "blocker does not belong to the job run", nil)
	}
	if fmt.Sprint(job["state"]) != "blocked" {
		return writeScopeResumeTarget{}, rpc.NewError("invalid_transition", fmt.Sprintf("job must be blocked before write-scope recovery resume (state=%q)", job["state"]), nil)
	}
	return writeScopeResumeTarget{JobID: jobID, RunID: runID, Job: job}, nil
}

func resolveWriteScopeBlocker(ctx context.Context, tx db.TxRunner, request writeScopeResumeRequest, target writeScopeResumeTarget) error {
	now := nowString()
	if err := tx.Exec(ctx, `
		UPDATE striatumd.blockers
		   SET state = 'resolved', resolved_at = $1
		 WHERE repository_id = $2 AND blocker_id = $3`, now, request.RepositoryID, request.Blocker["blocker_id"]); err != nil {
		return err
	}
	_, err := appendEvent(ctx, tx, request.RepositoryID, target.RunID, "recovery.write_scope_blocker_resolved", nullable(request.SessionID), target.JobID, nil, nil, nil, map[string]any{
		"blocker_id":         request.Blocker["blocker_id"],
		"blocker_kind":       request.BlockerKind,
		"verb":               "recovery resume",
		"complete_requested": request.CompleteRequested,
		"summary":            request.Summary,
		"original_envelope":  asMap(request.Blocker["payload_json"]),
	})
	return err
}

func writeScopeResumeResult(request writeScopeResumeRequest, target writeScopeResumeTarget, requeue requeueSameAttemptResult) map[string]any {
	nextActions := []string{"claim_available_work", "complete_job"}
	if request.CompleteRequested {
		nextActions = []string{"claim_available_work", "complete_job_after_claim"}
	}
	return map[string]any{
		"status":              "resumed_requeued",
		"run_id":              target.RunID,
		"job_id":              target.JobID,
		"workflow_job_id":     target.Job["workflow_job_id"],
		"blocker_id":          request.Blocker["blocker_id"],
		"blocker_kind":        request.BlockerKind,
		"message_id":          requeue.messageID,
		"already_reclaimable": requeue.alreadyReclaimable,
		"completed_inline":    false,
		"complete_requested":  request.CompleteRequested,
		"note":                "write-scope blockers release their lease when blocked; recovery.resume resolves the clean blocker and requeues the same attempt for a fresh claim before completion",
		"next_actions":        nextActions,
	}
}

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
	if !dryRun {
		// Worktree anchoring shells out to git; compute it before lockRun so the
		// sweep transaction only records the already-durable anchor payload.
		worktreeAnchors, err := buildRunWorktreeAnchorOracle(ctx, runner, repositoryID, runID)
		if err != nil {
			return nil, err
		}
		ctx = withWorktreeAnchorOracle(ctx, worktreeAnchors)
	}

	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
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
					"run_id":          runID,
					"dry_run":         dryRun,
					"published_count": 0,
					"published":       []map[string]any{},
					"skipped_count":   0,
					"skipped":         []map[string]any{},
					"helper_events":   helperEvents,
					"liveness":        map[string]any{"skipped": true, "reason": abandonedRunAutoCancelReason},
					"abandoned_run":   abandonedRun,
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
				artifact, err := publishRecoveredArtifact(ctx, tx, repositoryID, job, sessionID, leaseID, fmt.Sprint(run["repo_root"]), declared)
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
						"autonomous recovery: verdict auto-recorded from on-disk finding", nil, nil)
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
			raised, eerr := escalateExhaustedJobs(ctx, tx, repositoryID, runID)
			if eerr != nil {
				return nil, eerr
			}
			escalationsRaised = raised
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
			"run_id":          runID,
			"dry_run":         dryRun,
			"published_count": len(published),
			"published":       published,
			"skipped_count":   len(skipped),
			"skipped":         skipped,
			"helper_events":   helperEvents,
			"liveness":        liveness,
			"abandoned_run":   abandonedRun,
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

func SweepRun(ctx context.Context, runner db.Runner, repositoryID string, runID string, author string) (map[string]any, error) {
	if author == "" {
		author = "striatumd-go"
	}
	result, err := HandleRecoveryAuto(ctx, runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "daemon_sweep_" + runID,
		Method:        "recovery.sweep",
		Params: map[string]any{
			"repository_id": repositoryID,
			"run_id":        runID,
		},
	})
	if err != nil {
		return nil, err
	}
	_, err = withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first — this records a run-scoped sweep
		// event concurrently with claim/verdict-completion on the same run.
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		_, err := appendEvent(ctx, tx, repositoryID, runID, "daemon.recovery_sweep", nil, nil, nil, nil, nil, map[string]any{
			"author":        author,
			"repository_id": repositoryID,
			"result":        result,
		})
		return map[string]any{}, err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func autoPublishableArtifacts(ctx context.Context, runner any, repositoryID string, repoRoot string, job map[string]any, sessionID string, expectedByline string) (publishable []map[string]any, skipped []map[string]any, err error) {
	publishable = []map[string]any{}
	skipped = []map[string]any{}
	currentAttempt := jobAttemptValue(job["attempt"])
	workflowJobID := fmt.Sprint(job["workflow_job_id"])
	for _, item := range asList(job["expected_artifacts_json"]) {
		declared := asMap(item)
		if declared["required"] == false {
			continue
		}
		pathText, _ := declared["path"].(string)
		kind, _ := declared["kind"].(string)
		logicalName, _ := declared["logical_name"].(string)
		if pathText == "" || kind == "" || logicalName == "" {
			continue
		}
		path, err := repoRelativePath(repoRoot, pathText, false)
		if err != nil {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if !utf8.Valid(payload) {
			continue
		}
		matched := false
		for _, line := range markdownTitleBlockAuthorLines(string(payload)) {
			if canonicalBylineForm(line) == expectedByline {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		// RFC 0095 Goal 2 / #203: recovery auto-publish must only complete a job
		// from artifacts produced by the CURRENT attempt. A re-opened (revision-
		// cycle) job whose on-disk file is byte-identical to a PRIOR attempt's
		// published artifact is the pre-revision document the reviewer rejected —
		// crediting it converts the needs_revision verdict into a silent no-op.
		// Detect it by content_sha256: a row for THIS job at a lower attempt with
		// the same content means the on-disk file is stale prior-attempt output.
		sum := sha256.Sum256(payload)
		digest := hex.EncodeToString(sum[:])
		priorAttempt, found, perr := priorAttemptArtifactByContent(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), fmt.Sprint(job["job_id"]), digest, currentAttempt)
		if perr != nil {
			return nil, nil, perr
		}
		if found {
			skipped = append(skipped, map[string]any{
				"workflow_job_id": workflowJobID,
				"path":            pathText,
				"reason":          "stale_prior_attempt_artifact",
				"detail": fmt.Sprintf(
					"on-disk %s is byte-identical (content_sha256=%s) to this job's attempt-%d artifact while the job is at attempt %d; a re-opened (revision-cycle) attempt is never satisfied by a prior attempt's output (RFC 0095 Goal 2 / #203). A fresh lane must perform the revision.",
					pathText, digest, priorAttempt, currentAttempt,
				),
			})
			continue
		}
		publishable = append(publishable, map[string]any{
			"path":         pathText,
			"kind":         kind,
			"logical_name": logicalName,
		})
	}
	return publishable, skipped, nil
}

// priorAttemptArtifactByContent reports whether THIS job already published an
// artifact with the given content_sha256 at an attempt strictly lower than the
// job's current attempt. Used by the recovery auto-publish attempt-gate
// (RFC 0095 Goal 2 / #203) to refuse crediting a re-opened job from its
// pre-revision on-disk document. Returns the matched prior attempt for a legible
// skip reason.
func priorAttemptArtifactByContent(ctx context.Context, runner any, repositoryID, runID, jobID, contentSha256 string, currentAttempt int) (priorAttempt int, found bool, err error) {
	row, err := oneRow(ctx, runner, `
		SELECT attempt FROM striatumd.artifacts
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND content_sha256 = $4 AND attempt < $5
		 ORDER BY attempt DESC
		 LIMIT 1`, repositoryID, runID, jobID, contentSha256, currentAttempt)
	if err != nil {
		if errorsIsNoRows(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return jobAttemptValue(row["attempt"]), true, nil
}

// recoveredReviewVerdict extracts a verdict-bearing job's verdict from the
// just-published finding artifact's verdict_intent front matter, pairing it with
// that finding's published artifact_id (publishable[i] <-> artifacts[i]). #144:
// the stale-lease auto-publish path uses this to record the review's ACTUAL
// verdict on recovery (honoring what the reviewer decided on disk), not a blanket
// accept. Returns found=false when no finding with a verdict_intent is among the
// published artifacts, so the caller falls back to a plain completion.
func recoveredReviewVerdict(repoRoot string, publishable, artifacts []map[string]any) (verdict string, findingArtifactID any, found bool, err error) {
	for i, declared := range publishable {
		kind := fmt.Sprint(declared["kind"])
		if _, ok := frontMatterSchemas[kind]; !ok {
			continue
		}
		pathText := fmt.Sprint(declared["path"])
		path, perr := repoRelativePath(repoRoot, pathText, false)
		if perr != nil {
			continue
		}
		payload, rerr := os.ReadFile(filepath.Clean(path))
		if rerr != nil {
			continue
		}
		fm, ferr := autoFinalizeRequiredFrontMatter(kind, path, payload)
		if ferr != nil {
			return "", nil, false, ferr
		}
		v, ok := fm["verdict_intent"].(string)
		if !ok || v == "" {
			continue
		}
		if i < len(artifacts) {
			findingArtifactID = artifacts[i]["artifact_id"]
		}
		return v, findingArtifactID, true, nil
	}
	return "", nil, false, nil
}

// autonomouslyApplicableVerdict reports whether the autonomous stale-lease
// recovery path can cleanly apply a recovered verdict. accept / accept_with_findings
// complete the gate and needs_revision routes the bounded cycle (or opens a
// checkpoint) — none of which error. reject is excluded: its revision-cycle
// self-correction guard returns an error that would roll back the whole sweep, so
// a recovered reject falls back to plain completion instead.
func autonomouslyApplicableVerdict(verdict string) bool {
	switch verdict {
	case "accept", "accept_with_findings", "needs_revision":
		return true
	default:
		return false
	}
}

func publishRecoveredArtifact(ctx context.Context, runner any, repositoryID string, job map[string]any, sessionID string, leaseID string, repoRoot string, declared map[string]any) (map[string]any, error) {
	kind := fmt.Sprint(declared["kind"])
	logicalName := fmt.Sprint(declared["logical_name"])
	pathText := fmt.Sprint(declared["path"])
	if kind == "transcript" {
		return nil, rpc.NewError("artifact_error", "transcript artifacts are not allowed by default", nil)
	}
	if !allowedArtifactKinds[kind] {
		return nil, rpc.NewError("artifact_error", fmt.Sprintf("artifact kind %q is not in the allowed kinds list", kind), nil)
	}
	applyFrozenAttemptWriteScope(ctx, runner, repositoryID, job, leaseID)
	writeScope := asMap(job["write_scope_json"])
	if !pathAllowed(repoRoot, pathText, writeScope) {
		return nil, writeScopePathError(job, pathText, stringListFromAny(writeScope["allowed_paths"]), stringListFromAny(writeScope["forbidden_paths"]))
	}
	path, err := repoRelativePath(repoRoot, pathText, false)
	if err != nil {
		return nil, rpc.NewError("artifact_error", err.Error(), nil)
	}
	payload, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, rpc.NewError("artifact_error", "artifact file does not exist", nil)
	}
	payload, err = ensureRequiredFrontMatter(kind, path, payload)
	if err != nil {
		return nil, err
	}
	if err := validateMarkdownAuthorLine(ctx, runner, repositoryID, job, sessionID, path, payload); err != nil {
		return nil, err
	}
	if err := validateArtifactFrontMatter(kind, path, payload); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	// RFC 0095 §1 / #84: artifacts are attempt-scoped. Surgical recovery must key
	// its collision check (and the INSERT below) on the job's CURRENT attempt, so
	// a re-opened job's prior-attempt row neither mis-fires the conflict nor
	// mis-attributes the recovered artifact to attempt 1.
	attempt := jobAttemptValue(job["attempt"])
	existing, err := oneRow(ctx, runner, `
		SELECT * FROM striatumd.artifacts
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3 AND logical_name = $4
		   AND attempt = $5
		 LIMIT 1`, repositoryID, job["run_id"], job["job_id"], logicalName, attempt)
	if err == nil {
		if fmt.Sprint(existing["content_sha256"]) == digest && fmt.Sprint(existing["repo_path"]) == pathText {
			return map[string]any{"status": "already_published", "artifact_id": existing["artifact_id"]}, nil
		}
		return nil, rpc.NewError("artifact_error", artifactLogicalNameConflictMessage, nil)
	}
	if !errorsIsNoRows(err) {
		return nil, err
	}
	artifactID, err := newID("art")
	if err != nil {
		return nil, err
	}
	now := nowString()
	tx, ok := runner.(db.TxRunner)
	if !ok {
		return nil, fmt.Errorf("runner does not support transactional artifact append")
	}
	// RFC 0110 §7: SD-routed at phase audit_artifacts, direct INSERT before P1.
	if err := db.AppendArtifactInTx(ctx, tx, db.ArtifactRow{
		RepositoryID:  repositoryID,
		ArtifactID:    artifactID,
		RunID:         job["run_id"],
		JobID:         job["job_id"],
		SessionID:     sessionID,
		LogicalName:   logicalName,
		ArtifactKind:  kind,
		RepoPath:      pathText,
		ContentSHA256: digest,
		SizeBytes:     len(payload),
		CreatedAt:     now,
		AuthorLine:    nullable(firstAuthorLine(payload)),
		Attempt:       attempt,
	}); err != nil {
		return nil, err
	}
	if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "artifact.published", sessionID, job["job_id"], nil, artifactID, leaseID, map[string]any{
		"logical_name": logicalName,
		"path":         pathText,
		"sha256":       digest,
	}); err != nil {
		return nil, err
	}
	return map[string]any{"status": "published", "artifact_id": artifactID, "sha256": digest}, nil
}

func completeAutoRecoveredJob(ctx context.Context, runner any, repositoryID, jobID, sessionID, leaseID, messageID string) (map[string]any, error) {
	job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		return nil, err
	}
	if fmt.Sprint(job["state"]) != "stale_lease" && fmt.Sprint(job["state"]) != "running" && fmt.Sprint(job["state"]) != "claimed" {
		return nil, rpc.NewError("invalid_transition", "stale job is no longer auto-recoverable", nil)
	}
	if err := verifyRequiredArtifacts(ctx, runner, repositoryID, jobID); err != nil {
		return nil, err
	}
	now := nowString()
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return nil, fmt.Errorf("runner does not support exec")
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'completed', completed_at = $1, current_lease_id = NULL
		 WHERE repository_id = $2 AND job_id = $3`, now, repositoryID, jobID); err != nil {
		return nil, err
	}
	if messageID != "" && messageID != "<nil>" {
		if err := exec.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'completed', completed_at = $1, updated_at = $2,
			       current_lease_id = NULL
			 WHERE repository_id = $3 AND message_id = $4`, now, now, repositoryID, messageID); err != nil {
			return nil, err
		}
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.leases
		   SET released_at = COALESCE(released_at, $1),
		       release_reason = COALESCE(release_reason, 'auto_published')
		 WHERE repository_id = $2 AND lease_id = $3`, now, repositoryID, leaseID); err != nil {
		return nil, err
	}
	if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "job.completed", sessionID, jobID, nullable(messageID), nil, leaseID, map[string]any{
		"summary": "auto-published on stale lease",
	}); err != nil {
		return nil, err
	}
	// #304: a blocked-severity blocker raised on an earlier attempt of this job
	// must not dangle once the auto-recovery path completes it. Resolve the
	// completing job's open autonomous blockers exactly as the normal
	// HandleCompleteWork path does.
	if err := resolveAutonomousBlockersOnCompletion(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), jobID, sessionID, now); err != nil {
		return nil, err
	}
	if err := markJobTerminal(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), jobID); err != nil {
		return nil, err
	}
	if err := maybeEnqueueDownstream(ctx, runner, repositoryID, jobID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "completed", "job_id": jobID}, nil
}

func HandleRecoveryStaleLeases(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "recovery.stale_leases requires run_id", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first.
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		if _, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true); err != nil {
			return nil, err
		}
		if _, err := expireLeases(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		// #179: the expired-lease disjunct must only surface leases that still
		// represent an UNRESOLVED stale condition. A lease that recovery already
		// transferred or requeued away is historical provenance, not an actionable
		// stale lease — its job has since moved to a fresh lease/attempt — yet the
		// bare `l.state = 'expired'` join kept matching it, so every later
		// recovery.stale_leases call re-reported the same already-released lease as
		// stale. expireLeases (run just above) stamps a genuinely stale lease
		// state='expired', released_at=now, release_reason='expired', so released_at
		// alone cannot discriminate; the recovery transfer/requeue release reasons
		// are what mark a lease as already handled. Exclude those.
		rows, err := queryRows(ctx, tx, `
			SELECT j.job_id, j.workflow_job_id, j.state AS job_state,
			       j.write_scope_json,
			       l.lease_id, l.owner_session_id, l.acquired_at,
			       l.expires_at, l.released_at, l.release_reason,
			       l.state AS lease_state,
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
			            AND `+db.ExpiredLeaseStillStalePredicate+`))
			 ORDER BY j.workflow_job_id, l.expires_at`, repositoryID, runID)
		if err != nil {
			return nil, err
		}
		entries := []map[string]any{}
		seen := map[string]bool{}
		for _, row := range rows {
			key := fmt.Sprintf("%s/%v", row["job_id"], row["lease_id"])
			if seen[key] {
				continue
			}
			seen[key] = true
			repoWrite := isRepoWrite(row)
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
	})
}

// requeueSameAttemptOptions carries the provenance recorded on the
// recovery.requeued_same_attempt event for a same-attempt requeue.
type requeueSameAttemptOptions struct {
	operatorOverride bool
	justification    string
	author           string
}

// requeueSameAttemptResult reports what the helper did so callers can shape
// their RPC result and detect the idempotent no-op path.
type requeueSameAttemptResult struct {
	messageID          any
	alreadyReclaimable bool
	leaseID            any
}

// terminalMessageStates are the queue_message states that are NOT live — a job
// whose current message is in one of these (or NULL) needs a fresh pending one
// to become claimable. Mirrors slotHasUnclaimedParallelWork's "live" set
// (pending/claimed/acked) by complement.
var terminalMessageStates = map[string]bool{
	"completed": true,
	"canceled":  true,
	"failed":    true,
	"expired":   true,
	"dead":      true,
}

// requeueJobSameAttempt returns a dead-lane unfinished job to claimable WITHOUT
// bumping the attempt and WITHOUT resetting downstream. It is the RFC 0101
// Phase 3 Slice 1 primitive for the "running-limbo" failure: a supervised lane
// died (operator close, dead pane, missed heartbeat) leaving jobs.state in
// claimed/running/stale_lease, current_lease_id NULL, the lease released, and
// zero artifacts — so neither the auto-publish recovery nor the expired-lease
// requeue path (HandleRecoveryRequeueStale's JOIN to an expired lease) can
// reclaim it.
//
// Unlike reopenJobForAttempt (revision_routing.go) this is an OPERATIONAL, not
// content, recovery: attempt/max_attempts and downstream jobs are untouched.
//
// It is idempotent: an already-queued job whose current message is pending is a
// no-op success (result.alreadyReclaimable=true), mirroring the
// already_reclaimable pattern in HandleRecoveryRequeueStale.
//
// The job map must carry at least job_id, run_id, state, current_lease_id,
// current_message_id, and the columns insertPendingMessageForJob needs
// (role_id, lane_selector_json or target lane, max_attempts).
func requeueJobSameAttempt(ctx context.Context, tx db.TxRunner, repositoryID string, job map[string]any, opts requeueSameAttemptOptions) (requeueSameAttemptResult, error) {
	jobID := fmt.Sprint(job["job_id"])
	runID := job["run_id"]
	now := nowString()

	// Force-expire any residual ACTIVE lease still pinned to the job so a fresh
	// claim cannot trip the uq_active_resource_lease partial unique index. A
	// lease already in 'released'/'expired' is harmless and left as-is.
	if err := tx.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'expired', released_at = COALESCE(released_at, $1),
		       release_reason = 'recovery_requeue'
		 WHERE repository_id = $2 AND resource_id = $3 AND state = 'active'`,
		now, repositoryID, jobID); err != nil {
		return requeueSameAttemptResult{}, err
	}

	// Resolve the job's current message (if any) and whether it is still live.
	messageID := nullable(job["current_message_id"])
	currentMessageState := ""
	currentMessageLive := false
	if messageID != nil {
		row, err := oneRow(ctx, tx, `
			SELECT state FROM striatumd.queue_messages
			 WHERE repository_id = $1 AND message_id = $2`, repositoryID, messageID)
		if errors.Is(err, pgx.ErrNoRows) {
			messageID = nil
		} else if err != nil {
			return requeueSameAttemptResult{}, err
		} else {
			currentMessageState = fmt.Sprint(row["state"])
			currentMessageLive = !terminalMessageStates[currentMessageState]
		}
	}
	// RFC 0101 Phase 5: the live foreground claim path (HandleClaimNext) binds the
	// work message to a lease but does NOT stamp jobs.current_message_id, so a
	// genuinely live-claimed job that then dies arrives here with
	// current_message_id NULL while its work message is still in a non-terminal
	// (claimed/acked) state. Minting a fresh pending message in that case would
	// trip the uq_active_work_message_per_job partial unique index (one
	// non-terminal work message per job). Resolve the job's still-live work
	// message directly so we REUSE it rather than duplicate it. This is keyed on
	// the same (pending/claimed/acked) set the unique index covers, so it finds
	// exactly the message the index would collide with. (Surfaced by the
	// fault-injection chaos suite, which drives the REAL claim path rather than a
	// hand-seeded current_message_id.)
	if messageID == nil {
		row, err := oneRow(ctx, tx, `
			SELECT message_id, state FROM striatumd.queue_messages
			 WHERE repository_id = $1 AND job_id = $2 AND kind = 'work'
			   AND state IN ('pending','claimed','acked')
			 ORDER BY created_at DESC, message_id DESC
			 LIMIT 1`, repositoryID, jobID)
		if errors.Is(err, pgx.ErrNoRows) {
			messageID = nil
		} else if err != nil {
			return requeueSameAttemptResult{}, err
		} else {
			messageID = row["message_id"]
			currentMessageState = fmt.Sprint(row["state"])
			currentMessageLive = !terminalMessageStates[currentMessageState]
		}
	}
	leaseID := nullable(job["current_lease_id"])

	// Idempotency: an already-queued job whose live message is already pending is
	// reclaimable; report a no-op success without mutating the job/message state.
	if currentMessageLive && fmt.Sprint(job["state"]) == "queued" && currentMessageState == "pending" {
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.requeued_same_attempt", nil, jobID, messageID, nil, leaseID, requeueSameAttemptEventPayload(opts, true)); err != nil {
			return requeueSameAttemptResult{}, err
		}
		recordWake(tx, WakeEvent{
			RepositoryID: repositoryID,
			RunID:        fmt.Sprint(runID),
			Kind:         "work_available",
			MessageID:    fmt.Sprint(messageID),
		})
		return requeueSameAttemptResult{messageID: messageID, alreadyReclaimable: true, leaseID: leaseID}, nil
	}

	if !currentMessageLive {
		// No live message (NULL or terminal): mint a fresh pending one. This also
		// flips the job to queued + current_lease_id NULL + current_message_id.
		created, err := insertPendingMessageForJob(ctx, tx, repositoryID, job, now)
		if err != nil {
			return requeueSameAttemptResult{}, err
		}
		messageID = created
	} else {
		// Reuse the live message: flip the job to queued and the message back to
		// pending so a fresh session can claim the SAME attempt.
		if err := tx.Exec(ctx, `
			UPDATE striatumd.jobs
			   SET state = 'queued', current_lease_id = NULL
			 WHERE repository_id = $1 AND job_id = $2`, repositoryID, jobID); err != nil {
			return requeueSameAttemptResult{}, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'pending', current_lease_id = NULL, updated_at = $1
			 WHERE repository_id = $2 AND message_id = $3`, now, repositoryID, messageID); err != nil {
			return requeueSameAttemptResult{}, err
		}
	}

	if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.requeued_same_attempt", nil, jobID, messageID, nil, leaseID, requeueSameAttemptEventPayload(opts, false)); err != nil {
		return requeueSameAttemptResult{}, err
	}
	recordWake(tx, WakeEvent{
		RepositoryID: repositoryID,
		RunID:        fmt.Sprint(runID),
		Kind:         "work_available",
		MessageID:    fmt.Sprint(messageID),
	})
	return requeueSameAttemptResult{messageID: messageID, leaseID: leaseID}, nil
}

func requeueSameAttemptEventPayload(opts requeueSameAttemptOptions, alreadyReclaimable bool) map[string]any {
	payload := map[string]any{
		"already_reclaimable": alreadyReclaimable,
	}
	if opts.operatorOverride {
		payload["operator_override"] = true
		payload["repo_write"] = true
	}
	if opts.justification != "" {
		payload["justification"] = opts.justification
	}
	if opts.author != "" {
		payload["author"] = opts.author
	}
	return payload
}

func HandleRecoveryRequeueStale(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	jobID := stringParam(envelope, "job_id")
	if runID == "" || jobID == "" {
		return nil, rpc.NewError("schema_invalid", "recovery.requeue_stale requires run_id and job_id", nil)
	}
	force := boolParam(envelope, "force")
	justification := strings.TrimSpace(stringParam(envelope, "justification"))
	if force && justification == "" {
		return nil, rpc.NewError("invalid_transition", "--force requeue requires --justification", nil)
	}
	recoveryAuthor := stringParam(envelope, "recovery_author")

	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first.
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		if _, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true); err != nil {
			return nil, err
		}
		if _, err := expireLeases(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		now := nowString()
		// #82: an operator-inspected transfer (`--force`) of a live-but-wrong
		// repo-write claim. Force-expire the job's still-active lease and mark a
		// claimed/running job stale so the same attempt + queue message can be
		// requeued to a fresh session below — a lease-ownership correction that,
		// unlike run.retry_job, does NOT bump the attempt counter or reset
		// downstream. `--force` already requires `--justification`.
		if force {
			if err := tx.Exec(ctx, `
				UPDATE striatumd.leases
				   SET state = 'expired', released_at = $1, release_reason = 'operator_transfer'
				 WHERE repository_id = $2 AND resource_id = $3 AND state = 'active'`,
				now, repositoryID, jobID); err != nil {
				return nil, err
			}
			if err := tx.Exec(ctx, `
				UPDATE striatumd.jobs
				   SET state = 'stale_lease'
				 WHERE repository_id = $1 AND job_id = $2 AND state IN ('claimed', 'running')`,
				repositoryID, jobID); err != nil {
				return nil, err
			}
		}
		rows, err := queryRows(ctx, tx, `
			SELECT j.job_id, j.run_id, j.workflow_job_id, j.state,
			       j.role_id, j.lane_selector_json, j.max_attempts,
			       j.write_scope_json, j.current_message_id, j.current_lease_id,
			       l.lease_id, l.owner_session_id, l.expires_at,
			       qm.message_id, qm.state AS message_state
			  FROM striatumd.jobs j
			  JOIN striatumd.leases l
			    ON l.repository_id = j.repository_id
			   AND l.resource_id = j.job_id
			   AND l.state = 'expired'
			  LEFT JOIN striatumd.queue_messages qm
			    ON qm.repository_id = j.repository_id
			   AND qm.message_id = j.current_message_id
			 WHERE j.repository_id = $1
			   AND j.run_id = $2
			   AND j.job_id = $3
			   AND j.state IN ('queued', 'blocked', 'stale_lease')
			 ORDER BY l.expires_at DESC
			 LIMIT 1
			 FOR UPDATE OF j, l`, repositoryID, runID, jobID)
		if err != nil {
			return nil, err
		}
		var row map[string]any
		if len(rows) > 0 {
			row = rows[0]
		} else {
			// #82: when the job is held by a LIVE claimant (active lease) rather
			// than a stale/expired one, guide the operator to the transfer path
			// instead of the bare "no stale lease" error.
			hasActive, lerr := existsRow(ctx, tx, `
				SELECT 1 FROM striatumd.leases
				 WHERE repository_id = $1 AND resource_id = $2 AND state = 'active' LIMIT 1`,
				repositoryID, jobID)
			if lerr != nil {
				return nil, lerr
			}
			if hasActive {
				return nil, rpc.NewError("invalid_transition", "job is held by a live claimant (active lease); after stopping the wrong session and inspecting, transfer it with `--force --justification \"<reason>\"` (preserves the attempt; does not retry the job)", nil)
			}
			// RFC 0101 Phase 3 Slice 1 (#121): a dead-lane repo-write job is left in
			// "running-limbo" — jobs.state in claimed/running/stale_lease,
			// current_lease_id NULL, the lease already released (NOT expired), and
			// zero artifacts. There is no expired-lease row for the JOIN above to
			// find, so today this errored "no stale expired lease". Reclaim it on the
			// SAME attempt via requeueJobSameAttempt (no attempt bump, no downstream
			// reset). The D036 repo-write inspection gate is preserved: repo-write
			// still requires --force --justification. 'queued' is included so a
			// repeated requeue of an already-reclaimed job is an idempotent no-op
			// (the helper detects already_reclaimable); 'blocked' is excluded — a
			// blocked job with no lease is legitimately waiting on dependencies.
			limbo, lerr := queryRows(ctx, tx, `
				SELECT j.job_id, j.run_id, j.workflow_job_id, j.state,
				       j.role_id, j.lane_selector_json, j.max_attempts,
				       j.write_scope_json, j.current_message_id, j.current_lease_id
				  FROM striatumd.jobs j
				 WHERE j.repository_id = $1
				   AND j.run_id = $2
				   AND j.job_id = $3
				   AND j.state IN ('claimed', 'running', 'stale_lease', 'queued')
				 LIMIT 1
				 FOR UPDATE OF j`, repositoryID, runID, jobID)
			if lerr != nil {
				return nil, lerr
			}
			if len(limbo) == 0 {
				return nil, rpc.NewError("invalid_transition", "job has no stale expired lease to requeue", nil)
			}
			row = limbo[0]
		}

		repoWrite := isRepoWrite(row)
		if repoWrite && !force {
			return nil, rpc.NewError("invalid_transition", "repo-write stale jobs require manual inspection; rerun with `--force --justification \"<reason>\"` to override after inspection", nil)
		}
		opts := requeueSameAttemptOptions{author: recoveryAuthor}
		if force && repoWrite {
			opts.operatorOverride = true
			opts.justification = justification
		}
		result, err := requeueJobSameAttempt(ctx, tx, repositoryID, row, opts)
		if err != nil {
			return nil, err
		}
		// Preserve the verb's legacy `recovery.stale_requeued` audit event (the
		// helper also appends the canonical `recovery.requeued_same_attempt`); keep
		// the original payload shape so existing audit consumers are unchanged.
		legacyPayload := map[string]any{
			"already_reclaimable": result.alreadyReclaimable,
			"repo_write":          repoWrite,
		}
		if recoveryAuthor != "" {
			legacyPayload["author"] = recoveryAuthor
		}
		if force && repoWrite {
			legacyPayload["operator_override"] = true
			legacyPayload["justification"] = justification
		}
		// On the expired-lease JOIN path row["lease_id"] is the expired lease that
		// was found; on the dead-lane limbo path there is no such row, so it is nil.
		responseLeaseID := nullable(row["lease_id"])
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.stale_requeued", nil, jobID, result.messageID, nil, responseLeaseID, legacyPayload); err != nil {
			return nil, err
		}
		status := "requeued"
		if result.alreadyReclaimable {
			status = "already_reclaimable"
		}
		return map[string]any{
			"status":            status,
			"run_id":            runID,
			"job_id":            jobID,
			"workflow_job_id":   row["workflow_job_id"],
			"lease_id":          responseLeaseID,
			"message_id":        result.messageID,
			"repo_write":        repoWrite,
			"operator_override": force && repoWrite,
			"next_actions":      []string{"register_or_select_session", "claim_available_work"},
		}, nil
	})
}

func HandleRecoveryCancelJob(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	jobID := stringParam(envelope, "job_id")
	reason := strings.TrimSpace(stringParam(envelope, "reason"))
	cascade := boolParam(envelope, "cascade")
	if runID == "" || jobID == "" {
		return nil, rpc.NewError("schema_invalid", "recovery.cancel_job requires run_id and job_id", nil)
	}
	if reason == "" {
		return nil, rpc.NewError("invalid_transition", "cancel reason must not be empty", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first.
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		if _, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true); err != nil {
			return nil, err
		}
		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(job["run_id"]) != runID {
			return nil, rpc.NewError("invalid_transition", "job does not belong to the requested run", nil)
		}
		if !cancelableJobStates[fmt.Sprint(job["state"])] {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("job state %q is terminal and cannot be canceled", job["state"]), nil)
		}
		if _, err := expireLeases(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		job, err = rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
		if err != nil {
			return nil, err
		}
		if !cancelableJobStates[fmt.Sprint(job["state"])] {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("job state %q is terminal and cannot be canceled", job["state"]), nil)
		}
		dependents, err := dependentsBlockedOnlyThrough(ctx, tx, repositoryID, jobID)
		if err != nil {
			return nil, err
		}
		if len(dependents) > 0 && !cascade {
			names := []string{}
			for _, row := range dependents {
				names = append(names, fmt.Sprint(row["workflow_job_id"]))
			}
			return nil, rpc.NewError("invalid_transition", "job has blocked dependents whose only path is through this job; rerun with --cascade or cancel them explicitly: "+strings.Join(names, ", "), nil)
		}
		now := nowString()
		canceled, err := cancelSingleJob(ctx, tx, repositoryID, job, reason, now)
		if err != nil {
			return nil, err
		}
		downstream := []map[string]any{}
		if cascade {
			queue := append([]map[string]any(nil), dependents...)
			visited := map[string]bool{jobID: true}
			for len(queue) > 0 {
				next := []map[string]any{}
				for _, item := range queue {
					depID := fmt.Sprint(item["job_id"])
					if visited[depID] {
						continue
					}
					visited[depID] = true
					fresh, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", depID, true)
					if err != nil {
						return nil, err
					}
					if !cancelableJobStates[fmt.Sprint(fresh["state"])] {
						continue
					}
					summary, err := cancelSingleJob(ctx, tx, repositoryID, fresh, "cascade:"+reason, now)
					if err != nil {
						return nil, err
					}
					downstream = append(downstream, summary)
					more, err := dependentsBlockedOnlyThrough(ctx, tx, repositoryID, depID)
					if err != nil {
						return nil, err
					}
					next = append(next, more...)
				}
				queue = next
			}
		}
		if err := maybeCompleteRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		runAfter, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, false)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"status":              "canceled",
			"run_id":              runID,
			"job_id":              jobID,
			"workflow_job_id":     canceled["workflow_job_id"],
			"previous_state":      canceled["previous_state"],
			"reason":              reason,
			"cascade":             cascade,
			"downstream_canceled": downstream,
			"run_state":           runAfter["state"],
			"next_actions":        []string{"inspect_run_state", "export_run_evidence"},
		}, nil
	})
}

type worktreeAnchorOracleKey struct{}

type worktreeAnchorOracle struct {
	byWorktreeID map[string]map[string]any
}

func withWorktreeAnchorOracle(ctx context.Context, oracle *worktreeAnchorOracle) context.Context {
	return context.WithValue(ctx, worktreeAnchorOracleKey{}, oracle)
}

func worktreeAnchorOracleFromContext(ctx context.Context) *worktreeAnchorOracle {
	oracle, _ := ctx.Value(worktreeAnchorOracleKey{}).(*worktreeAnchorOracle)
	return oracle
}

func (o *worktreeAnchorOracle) lookup(worktreeID string) (map[string]any, bool) {
	if o == nil || o.byWorktreeID == nil {
		return nil, false
	}
	payload, ok := o.byWorktreeID[worktreeID]
	return payload, ok
}

// buildRunWorktreeAnchorOracle anchors expired repo-write worktrees before the
// sweep transaction opens. The in-transaction path may then abandon the worktree
// row and emit provenance without running git under lockRun.
func buildRunWorktreeAnchorOracle(ctx context.Context, runner db.Runner, repositoryID, runID string) (*worktreeAnchorOracle, error) {
	oracle := &worktreeAnchorOracle{byWorktreeID: map[string]map[string]any{}}
	rows, err := queryRows(ctx, runner, `
		SELECT j.job_id, j.attempt,
		       wt.worktree_id, wt.worktree_path, wt.base_branch
		  FROM striatumd.leases l
		  JOIN striatumd.jobs j
		    ON j.repository_id = l.repository_id
		   AND j.job_id = l.resource_id
		  JOIN striatumd.job_worktrees wt
		    ON wt.repository_id = j.repository_id
		   AND wt.job_id = j.job_id
		   AND wt.lease_id = l.lease_id
		   AND wt.state = 'active'
		 WHERE l.repository_id = $1
		   AND l.run_id = $2
		   AND l.state = 'active'
		   AND l.expires_at < $3::timestamptz
		 ORDER BY wt.worktree_id`,
		repositoryID,
		runID,
		nowString(),
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return oracle, nil
	}
	repoRoot, err := activeRepositoryRoot(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		worktreeID := fmt.Sprint(row["worktree_id"])
		worktree := map[string]any{
			"worktree_id":   worktreeID,
			"worktree_path": row["worktree_path"],
			"base_branch":   row["base_branch"],
		}
		payload, err := anchorWorktreeCommitStack(ctx, repoRoot, runID, fmt.Sprint(row["job_id"]), fmt.Sprint(row["base_branch"]), worktree, intValue(row["attempt"]))
		if err != nil {
			return nil, err
		}
		oracle.byWorktreeID[worktreeID] = payload
	}
	return oracle, nil
}

func expireLeases(ctx context.Context, runner any, repositoryID, runID string) ([]map[string]any, error) {
	now := nowString()
	rows, err := queryRows(ctx, runner, `
		SELECT * FROM striatumd.leases
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND state = 'active'
		   AND expires_at < $3::timestamptz
		 FOR UPDATE`, repositoryID, runID, now)
	if err != nil {
		return nil, err
	}
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return nil, fmt.Errorf("runner does not support exec")
	}
	summaries := []map[string]any{}
	for _, lease := range rows {
		job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", fmt.Sprint(lease["resource_id"]), true)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		messageID := nullable(job["current_message_id"])
		repoWrite := isRepoWrite(job)
		jobState := "queued"
		messageState := "pending"
		if repoWrite {
			jobState = "stale_lease"
			messageState = "blocked"
		}
		if err := exec.Exec(ctx, `
			UPDATE striatumd.leases
			   SET state = 'expired', released_at = $1, release_reason = 'expired'
			 WHERE repository_id = $2 AND lease_id = $3`, now, repositoryID, lease["lease_id"]); err != nil {
			return nil, err
		}
		if err := exec.Exec(ctx, `
			UPDATE striatumd.jobs
			   SET state = $1, current_lease_id = NULL
			 WHERE repository_id = $2 AND job_id = $3`, jobState, repositoryID, job["job_id"]); err != nil {
			return nil, err
		}
		if messageID != nil {
			if err := exec.Exec(ctx, `
				UPDATE striatumd.queue_messages
				   SET state = $1, current_lease_id = NULL, updated_at = $2
				 WHERE repository_id = $3 AND message_id = $4`, messageState, now, repositoryID, messageID); err != nil {
				return nil, err
			}
		}
		if _, err := appendEvent(ctx, runner, repositoryID, runID, "lease.expired", nil, job["job_id"], messageID, nil, lease["lease_id"], map[string]any{
			"job_state":     jobState,
			"message_state": messageState,
		}); err != nil {
			return nil, err
		}
		worktrees, err := queryRows(ctx, runner, `
			SELECT *
			  FROM striatumd.job_worktrees
			 WHERE repository_id = $1
			   AND job_id = $2
			   AND state = 'active'
			 FOR UPDATE`, repositoryID, job["job_id"])
		if err != nil {
			return nil, err
		}
		for _, worktree := range worktrees {
			if fmt.Sprint(worktree["lease_id"]) != fmt.Sprint(lease["lease_id"]) {
				continue
			}
			anchorOracle := worktreeAnchorOracleFromContext(ctx)
			anchorPayload, ok := anchorOracle.lookup(fmt.Sprint(worktree["worktree_id"]))
			if !ok {
				if anchorOracle != nil {
					return nil, fmt.Errorf("missing precomputed worktree anchor for %s", worktree["worktree_id"])
				}
				repoRoot, err := activeRepositoryRoot(ctx, runner, repositoryID)
				if err != nil {
					return nil, err
				}
				anchorPayload, err = anchorWorktreeCommitStack(ctx, repoRoot, runID, fmt.Sprint(job["job_id"]), fmt.Sprint(worktree["base_branch"]), worktree, intValue(job["attempt"]))
				if err != nil {
					return nil, err
				}
			}
			if err := exec.Exec(ctx, `
				UPDATE striatumd.job_worktrees
				   SET state = 'abandoned'
				 WHERE repository_id = $1 AND worktree_id = $2`, repositoryID, worktree["worktree_id"]); err != nil {
				return nil, err
			}
			if _, err := appendEvent(ctx, runner, repositoryID, runID, "worktree.abandoned", nil, job["job_id"], nil, nil, lease["lease_id"], map[string]any{
				"worktree_id": fmt.Sprint(worktree["worktree_id"]),
				"base_branch": worktree["base_branch"],
				"anchor":      anchorPayload,
			}); err != nil {
				return nil, err
			}
		}
		summaries = append(summaries, map[string]any{
			"lease_id":      fmt.Sprint(lease["lease_id"]),
			"job_id":        fmt.Sprint(job["job_id"]),
			"message_id":    nullable(messageID),
			"job_state":     jobState,
			"message_state": messageState,
			"repo_write":    repoWrite,
		})
	}
	return summaries, nil
}

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func evaluateAndBlockLostProcess(ctx context.Context, runner any, repositoryID string, job map[string]any, sessionID string, processID string, command any) (any, error) {
	missingPaths, verdictMissing, err := validateProcessOutputs(ctx, runner, repositoryID, job)
	if err != nil {
		return nil, err
	}
	if len(missingPaths) == 0 && !verdictMissing {
		return nil, nil
	}
	existing, err := existsRow(ctx, runner, `
		SELECT 1 FROM striatumd.blockers
		 WHERE repository_id = $1
		   AND job_id = $2
		   AND state = 'open'
		 LIMIT 1`, repositoryID, job["job_id"])
	if err != nil {
		return nil, err
	}
	if existing {
		return nil, nil
	}
	blockerID, err := newID("blk")
	if err != nil {
		return nil, err
	}
	now := nowString()
	blockerKind := "process_lost_with_outputs_missing"
	description := fmt.Sprintf("process %s was lost (external kill or runner exit); required outputs missing: %d artifact(s), verdict missing=%v", processID, len(missingPaths), verdictMissing)
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return nil, fmt.Errorf("runner does not support exec")
	}
	blockerPayload := map[string]any{
		"process_id":             processID,
		"command":                command,
		"missing_artifact_paths": missingPaths,
		"review_verdict_missing": verdictMissing,
	}
	blockerPayloadArg, err := db.JSONBArg(runner, blockerPayload)
	if err != nil {
		return nil, err
	}
	if err := exec.Exec(ctx, `
		INSERT INTO striatumd.blockers (
		  repository_id, blocker_id, run_id, job_id, session_id,
		  severity, blocker_kind, description, state, created_at, payload_json
		)
		VALUES ($1,$2,$3,$4,$5,'blocked',$6,$7,'open',$8,$9::jsonb)`,
		repositoryID,
		blockerID,
		job["run_id"],
		job["job_id"],
		sessionID,
		blockerKind,
		description,
		now,
		blockerPayloadArg,
	); err != nil {
		return nil, err
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'blocked'
		 WHERE repository_id = $1 AND job_id = $2`, repositoryID, job["job_id"]); err != nil {
		return nil, err
	}
	if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "job.blocked", sessionID, job["job_id"], nil, nil, nil, map[string]any{
		"blocker_id":   blockerID,
		"blocker_kind": blockerKind,
	}); err != nil {
		return nil, err
	}
	return blockerKind, nil
}

func validateProcessOutputs(ctx context.Context, runner any, repositoryID string, job map[string]any) ([]string, bool, error) {
	requiredPaths := []string{}
	for _, item := range asList(job["expected_artifacts_json"]) {
		expected := asMap(item)
		if expected["required"] == false {
			continue
		}
		path, _ := expected["path"].(string)
		if path != "" {
			requiredPaths = append(requiredPaths, path)
		}
	}
	published := map[string]bool{}
	if len(requiredPaths) > 0 {
		rows, err := queryRows(ctx, runner, `
			SELECT repo_path FROM striatumd.artifacts
			 WHERE repository_id = $1 AND job_id = $2`, repositoryID, job["job_id"])
		if err != nil {
			return nil, false, err
		}
		for _, row := range rows {
			published[fmt.Sprint(row["repo_path"])] = true
		}
	}
	missing := []string{}
	for _, path := range requiredPaths {
		if !published[path] {
			missing = append(missing, path)
		}
	}
	verdictMissing := false
	if fmt.Sprint(job["job_type"]) == "review" {
		found, err := existsRow(ctx, runner, `
			SELECT 1 FROM striatumd.verdicts
			 WHERE repository_id = $1 AND job_id = $2
			 LIMIT 1`, repositoryID, job["job_id"])
		if err != nil {
			return nil, false, err
		}
		verdictMissing = !found
	}
	return missing, verdictMissing, nil
}

func completeRecoveredJob(ctx context.Context, runner any, repositoryID, jobID, sessionID, leaseID, summary string) (map[string]any, error) {
	job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		return nil, err
	}
	if _, err := activeLeaseFor(ctx, runner, repositoryID, leaseID, sessionID, jobID); err != nil {
		return nil, err
	}
	if fmt.Sprint(job["state"]) != "running" {
		return nil, rpc.NewError("invalid_transition", "job must be running before completion", nil)
	}
	applyFrozenAttemptWriteScope(ctx, runner, repositoryID, job, leaseID)
	if err := enforceWriteScopeClean(ctx, runner, repositoryID, job); err != nil {
		return nil, err
	}
	if err := verifyRequiredArtifacts(ctx, runner, repositoryID, jobID); err != nil {
		return nil, err
	}
	if err := ensurePerJobPublishedArtifactsDurable(ctx, runner, repositoryID, job, "recovery.resume --complete"); err != nil {
		return nil, err
	}
	now := nowString()
	messageID := nullable(job["current_message_id"])
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return nil, fmt.Errorf("runner does not support exec")
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'completed', completed_at = $1, current_lease_id = NULL
		 WHERE repository_id = $2 AND job_id = $3`, now, repositoryID, jobID); err != nil {
		return nil, err
	}
	if messageID != nil {
		if err := exec.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'completed', completed_at = $1, updated_at = $2,
			       current_lease_id = NULL
			 WHERE repository_id = $3 AND message_id = $4`, now, now, repositoryID, messageID); err != nil {
			return nil, err
		}
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'released', released_at = $1, release_reason = 'completed'
		 WHERE repository_id = $2 AND lease_id = $3`, now, repositoryID, leaseID); err != nil {
		return nil, err
	}
	if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "job.completed", sessionID, jobID, messageID, nil, leaseID, map[string]any{"summary": summary}); err != nil {
		return nil, err
	}
	// #304: resolve the completing job's open autonomous blockers (e.g. a
	// blocked-severity write_scope conflict raised on a prior attempt) so a
	// recovery.resume --complete does not leave them dangling open.
	if err := resolveAutonomousBlockersOnCompletion(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), jobID, sessionID, now); err != nil {
		return nil, err
	}
	if err := markJobTerminal(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), jobID); err != nil {
		return nil, err
	}
	if err := maybeEnqueueDownstream(ctx, runner, repositoryID, jobID); err != nil {
		return nil, err
	}
	if err := maybeCompleteRun(ctx, runner, repositoryID, fmt.Sprint(job["run_id"])); err != nil {
		return nil, err
	}
	return map[string]any{"status": "completed", "job_id": jobID}, nil
}

func insertPendingMessageForJob(ctx context.Context, runner any, repositoryID string, job map[string]any, now string) (string, error) {
	messageID, err := newID("msg")
	if err != nil {
		return "", err
	}
	selector := asMap(job["lane_selector_json"])
	targetLane, _ := selector["lane_id"].(string)
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return "", fmt.Errorf("runner does not support exec")
	}
	payloadArg, err := db.JSONBArg(runner, map[string]any{})
	if err != nil {
		return "", err
	}
	if err := exec.Exec(ctx, `
		INSERT INTO striatumd.queue_messages (
		  repository_id, message_id, run_id, job_id, kind, state, priority,
		  target_role_id, target_lane_id, payload_json, claim_count,
		  max_claims, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,'work','pending',0,$5,$6,$7::jsonb,0,$8,$9,$10)`,
		repositoryID,
		messageID,
		job["run_id"],
		job["job_id"],
		job["role_id"],
		nullable(targetLane),
		payloadArg,
		job["max_attempts"],
		now,
		now,
	); err != nil {
		return "", err
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'queued', current_lease_id = NULL, current_message_id = $1
		 WHERE repository_id = $2 AND job_id = $3`, messageID, repositoryID, job["job_id"]); err != nil {
		return "", err
	}
	return messageID, nil
}

func dependentsBlockedOnlyThrough(ctx context.Context, runner any, repositoryID, jobID string) ([]map[string]any, error) {
	candidates, err := queryRows(ctx, runner, `
		SELECT j.job_id, j.workflow_job_id, j.state
		  FROM striatumd.job_dependencies dep
		  JOIN striatumd.jobs j
		    ON j.repository_id = dep.repository_id
		   AND j.job_id = dep.job_id
		 WHERE dep.repository_id = $1
		   AND dep.depends_on_job_id = $2
		   AND j.state = 'blocked'
		 ORDER BY j.workflow_job_id`, repositoryID, jobID)
	if err != nil {
		return nil, err
	}
	qualifying := []map[string]any{}
	for _, candidate := range candidates {
		otherDeps, err := queryRows(ctx, runner, `
			SELECT up.state
			  FROM striatumd.job_dependencies dep
			  JOIN striatumd.jobs up
			    ON up.repository_id = dep.repository_id
			   AND up.job_id = dep.depends_on_job_id
			 WHERE dep.repository_id = $1
			   AND dep.job_id = $2
			   AND dep.depends_on_job_id != $3`, repositoryID, candidate["job_id"], jobID)
		if err != nil {
			return nil, err
		}
		onlyThrough := true
		for _, row := range otherDeps {
			state := fmt.Sprint(row["state"])
			if state != "completed" && state != "canceled" {
				onlyThrough = false
				break
			}
		}
		if onlyThrough {
			qualifying = append(qualifying, candidate)
		}
	}
	return qualifying, nil
}

func cancelSingleJob(ctx context.Context, runner any, repositoryID string, job map[string]any, reason, now string) (map[string]any, error) {
	jobID := fmt.Sprint(job["job_id"])
	leaseID := nullable(job["current_lease_id"])
	messageID := nullable(job["current_message_id"])
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return nil, fmt.Errorf("runner does not support exec")
	}
	if leaseID != nil {
		if err := exec.Exec(ctx, `
			UPDATE striatumd.leases
			   SET state = 'released', released_at = $1, release_reason = 'canceled'
			 WHERE repository_id = $2
			   AND lease_id = $3
			   AND state = 'active'`, now, repositoryID, leaseID); err != nil {
			return nil, err
		}
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.leases
		   SET release_reason = COALESCE(release_reason, 'canceled')
		 WHERE repository_id = $1
		   AND resource_id = $2
		   AND state = 'expired'`, repositoryID, jobID); err != nil {
		return nil, err
	}
	if messageID != nil {
		if err := exec.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'canceled', current_lease_id = NULL, updated_at = $1
			 WHERE repository_id = $2 AND message_id = $3`, now, repositoryID, messageID); err != nil {
			return nil, err
		}
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'canceled', current_lease_id = NULL,
		       current_message_id = NULL, completed_at = $1
		 WHERE repository_id = $2 AND job_id = $3`, now, repositoryID, jobID); err != nil {
		return nil, err
	}
	if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "job.canceled", nil, jobID, messageID, nil, leaseID, map[string]any{
		"reason":          reason,
		"workflow_job_id": job["workflow_job_id"],
	}); err != nil {
		return nil, err
	}
	if err := markJobTerminal(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), jobID); err != nil {
		return nil, err
	}
	return map[string]any{
		"job_id":          jobID,
		"workflow_job_id": job["workflow_job_id"],
		"previous_state":  job["state"],
	}, nil
}
