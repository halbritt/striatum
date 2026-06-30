package mutations

import (
	"context"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// HandleRecoveryReseal is RFC 0125 P1-2 (D192; GH #271 + #273): the
// same-attempt completion path for a job whose work.complete was refused by
// the worktree-durability gate.
//
// Background. When a required artifact body cannot be reconstructed from the
// run branch HEAD, ensurePerJobPublishedArtifactsDurable
// (artifact_durability.go) refuses work.complete with a
// "published_artifact_not_durable" condition, and the worktree GC skips the
// worktree for the same reason. Neither writes a `blockers` row, so this verb
// keys on (run_id, job_id[, attempt]) rather than a blocker_id. The only
// pre-existing operator recovery was `run retry-job`, which bumps the job to a
// NEW attempt — past max_attempts (#273) and duplicating provenance, because no
// verb completed the SAME attempt once the body was made durable (#271).
//
// recovery.reseal closes that gap. It invokes the daemon-as-porter to commit any
// published artifact bodies still present in the per-job worktree, then anchors
// that worktree. If the job has already opened a human checkpoint, reseal keeps
// that checkpoint open and restores the job to waiting_human; otherwise it
// requeues the SAME attempt via requeueJobSameAttempt — no attempt bump, no
// downstream reset. If the body is absent or still not durable it refuses with
// the same invalid_transition listing the still-undurable artifacts, so the
// operator knows the remediation has not landed (it never silently completes).
//
// Contrast: reopenJobForAttempt / resetJobToBlockedWithReason
// (revision_routing.go) and HandleRunRetryJob (run.go) all BUMP attempt. This
// handler must not, under any path.
func HandleRecoveryReseal(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	jobID := stringParam(envelope, "job_id")
	if runID == "" || jobID == "" {
		return nil, rpc.NewError("schema_invalid", "recovery.reseal requires run_id and job_id", nil)
	}
	// attempt is optional: when supplied it pins the probe to a specific
	// historical attempt; when omitted the job's current attempt is used.
	expectedAttempt := stringParam(envelope, "attempt")
	recoveryAuthor := stringParam(envelope, "recovery_author")

	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first, scoped via the job's immutable
		// run_id, mirroring HandleRecoveryInvalidateJob.
		if err := lockRunForJob(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		}
		// Load the job FOR UPDATE with exactly the columns requeueJobSameAttempt
		// (and its insertPendingMessageForJob fallback) needs.
		rows, err := queryRows(ctx, tx, `
			SELECT j.job_id, j.run_id, j.workflow_job_id, j.state, j.attempt,
			       j.role_id, j.lane_selector_json, j.max_attempts,
			       j.write_scope_json, j.current_message_id, j.current_lease_id
			  FROM striatumd.jobs j
			 WHERE j.repository_id = $1 AND j.job_id = $2
			 LIMIT 1
			 FOR UPDATE OF j`, repositoryID, jobID)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, rpc.NewError("not_found", fmt.Sprintf("could not find jobs row for %q", jobID), nil)
		}
		job := rows[0]
		if fmt.Sprint(job["run_id"]) != runID {
			return nil, rpc.NewError("invalid_transition", "job does not belong to the given run_id", nil)
		}
		// The durability gate only fires for repo-write jobs that require an
		// active per-job worktree; reseal is meaningless for any other job.
		if !isRepoWrite(job) {
			return nil, rpc.NewError("invalid_transition", "recovery.reseal applies only to repo-write jobs subject to the worktree-durability gate", nil)
		}
		if expectedAttempt != "" && fmt.Sprint(job["attempt"]) != expectedAttempt {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
				"recovery.reseal attempt %s does not match the job's current attempt %v; reseal completes the live attempt only",
				expectedAttempt, job["attempt"]), nil)
		}

		// Re-probe durability and let the daemon-as-porter commit artifact files
		// that still exist in the per-job worktree. ensurePublishedArtifactsDurableWithPorter
		// returns:
		//   - worktree_required error  → the per-job worktree was already released;
		//     the operator must recreate it (and re-commit the body) before reseal.
		//   - invalid_transition listing artifacts → the body is STILL not durable
		//     (this is publishedArtifactsNotDurableError); surface it verbatim so
		//     the operator knows the remediation has not landed.
		//   - nil → every required artifact is reconstructable from HEAD; proceed.
		if err := ensurePublishedArtifactsDurableWithPorter(ctx, tx, repositoryID, job, "recovery.reseal"); err != nil {
			return nil, err
		}
		anchorPayload, err := anchorActiveWorktreeForJob(ctx, tx, repositoryID, job)
		if err != nil {
			return nil, err
		}
		if anchorPayload != nil {
			if _, err := appendEvent(ctx, tx, repositoryID, runID, "job.commits_anchored", nil, jobID, nullable(job["current_message_id"]), nil, nullable(job["current_lease_id"]), anchorPayload); err != nil {
				return nil, err
			}
		}

		checkpoint, err := openHumanCheckpointBlockerForJob(ctx, tx, repositoryID, jobID)
		if err != nil {
			return nil, err
		}
		if checkpoint != nil {
			return restoreResealedHumanCheckpoint(ctx, tx, repositoryID, job, checkpoint, recoveryAuthor)
		}

		// Durable: requeue the SAME attempt. No attempt bump, no downstream reset.
		opts := requeueSameAttemptOptions{author: recoveryAuthor}
		result, err := requeueJobSameAttempt(ctx, tx, repositoryID, job, opts)
		if err != nil {
			return nil, err
		}

		resealedPayload := map[string]any{
			"attempt":             job["attempt"],
			"already_reclaimable": result.alreadyReclaimable,
		}
		if recoveryAuthor != "" {
			resealedPayload["author"] = recoveryAuthor
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.resealed", nil, jobID, result.messageID, nil, result.leaseID, resealedPayload); err != nil {
			return nil, err
		}

		status := "resealed"
		if result.alreadyReclaimable {
			status = "already_reclaimable"
		}
		return map[string]any{
			"status":          status,
			"run_id":          runID,
			"job_id":          jobID,
			"workflow_job_id": job["workflow_job_id"],
			"attempt":         job["attempt"],
			"new_state":       "queued",
			"message_id":      result.messageID,
			"next_actions":    []string{"register_or_select_session", "claim_available_work"},
		}, nil
	})
}

func openHumanCheckpointBlockerForJob(ctx context.Context, runner any, repositoryID, jobID string) (map[string]any, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT blocker_id, run_id, job_id, session_id, blocker_kind, severity
		  FROM striatumd.blockers
		 WHERE repository_id = $1
		   AND job_id = $2
		   AND state = 'open'
		   AND severity = 'human_checkpoint'
		 ORDER BY created_at DESC, blocker_id DESC
		 LIMIT 1
		 FOR UPDATE`, repositoryID, jobID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func restoreResealedHumanCheckpoint(ctx context.Context, runner any, repositoryID string, job map[string]any, checkpoint map[string]any, recoveryAuthor string) (map[string]any, error) {
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return nil, fmt.Errorf("runner does not support exec")
	}
	runID := fmt.Sprint(job["run_id"])
	jobID := fmt.Sprint(job["job_id"])
	now := nowString()

	if err := exec.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'released', released_at = $1, release_reason = 'recovery_reseal_checkpoint'
		 WHERE repository_id = $2
		   AND resource_type = 'job'
		   AND resource_id = $3
		   AND state = 'active'`,
		now, repositoryID, jobID); err != nil {
		return nil, err
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.queue_messages
		   SET state = 'blocked', updated_at = $1, current_lease_id = NULL
		 WHERE repository_id = $2
		   AND job_id = $3
		   AND kind = 'work'
		   AND state IN ('pending','claimed','acked')`,
		now, repositoryID, jobID); err != nil {
		return nil, err
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'waiting_human', current_lease_id = NULL
		 WHERE repository_id = $1 AND job_id = $2`,
		repositoryID, jobID); err != nil {
		return nil, err
	}

	conflictRows, err := queryRows(ctx, runner, `
		SELECT blocker_id
		  FROM striatumd.blockers
		 WHERE repository_id = $1
		   AND job_id = $2
		   AND state = 'open'
		   AND severity <> 'human_checkpoint'
		   AND blocker_kind = 'immutable_artifact_conflict'
		 ORDER BY created_at, blocker_id
		 FOR UPDATE`, repositoryID, jobID)
	if err != nil {
		return nil, err
	}
	resolvedBlockers := []string{}
	for _, row := range conflictRows {
		blockerID := fmt.Sprint(row["blocker_id"])
		if err := exec.Exec(ctx, `
			UPDATE striatumd.blockers
			   SET state = 'resolved', resolved_at = $1
			 WHERE repository_id = $2 AND blocker_id = $3`,
			now, repositoryID, blockerID); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, runner, repositoryID, runID, "blocker.resolved", nil, jobID, nil, nil, nil, map[string]any{
			"blocker_id": blockerID,
			"reason":     "recovery_reseal_checkpoint",
		}); err != nil {
			return nil, err
		}
		resolvedBlockers = append(resolvedBlockers, blockerID)
	}

	payload := map[string]any{
		"attempt":             job["attempt"],
		"already_reclaimable": false,
		"checkpoint_restored": true,
		"blocker_id":          checkpoint["blocker_id"],
	}
	if recoveryAuthor != "" {
		payload["author"] = recoveryAuthor
	}
	if len(resolvedBlockers) > 0 {
		payload["resolved_blockers"] = resolvedBlockers
	}
	if _, err := appendEvent(ctx, runner, repositoryID, runID, "recovery.resealed", nil, jobID, nullable(job["current_message_id"]), nil, nullable(job["current_lease_id"]), payload); err != nil {
		return nil, err
	}
	if err := markJobTerminal(ctx, runner, repositoryID, runID, jobID); err != nil {
		return nil, err
	}

	return map[string]any{
		"status":            "resealed_checkpoint",
		"run_id":            runID,
		"job_id":            jobID,
		"workflow_job_id":   job["workflow_job_id"],
		"attempt":           job["attempt"],
		"new_state":         "waiting_human",
		"blocker_id":        checkpoint["blocker_id"],
		"resolved_blockers": resolvedBlockers,
		"next_actions":      []string{"fix_findings_then_checkpoint_continue_or_record_decision"},
	}, nil
}
