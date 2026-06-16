package mutations

import (
	"context"
	"errors"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

// completeStalledSummary is the durable job.completed summary for an
// operator-initiated complete-stalled. It names the recovery action so the
// provenance chain records that the job was finalized from its already-durable
// published artifacts rather than sealed by a live lane.
const completeStalledSummary = "operator complete-stalled: finalized from durable published artifacts (recovery_exhausted dead-end)"

// HandleRecoveryCompleteStalled is GH #292: the non-destructive completion path
// for a job whose agent published its required artifacts (durably, on the run
// branch) and then died in the gap before work.complete. Autonomous recovery
// requeues the same attempt, the agent re-dies the same way, recovery exhausts,
// and the run goes needs_operator behind a recovery_exhausted blocker.
//
// From that dead-end the pre-existing verbs all fail:
//   - recovery.auto_finalize requires run.state='running' AND a live active
//     lease+session (it re-publishes from a file an idling lane left on disk);
//     this run is needs_operator and the lane is dead.
//   - recovery.resume handles only process-adapter / write-scope blockers, not
//     recovery_exhausted.
//   - recovery.reseal requeues the SAME attempt for a fresh claim — but the
//     agent re-dies every claim, so it never completes.
//   - recovery.cancel_job marks a SUCCESSFUL job canceled (misrepresenting
//     provenance) and leaves the blocker open.
//
// complete-stalled closes that gap. It is the operator inverse of the dead
// lane's missing work.complete: it verifies the job's required artifacts are
// present (verifyRequiredArtifacts) AND durable/reconstructable from their
// declared placement (verifyRequiredArtifactReconstructable, RFC 0125 P0-3 —
// worktree-independent), then drives the same server-side completion work.complete
// would have, reusing resolveAutonomousBlockersOnCompletion (#207) to resolve the
// now-moot recovery_exhausted blocker + escalation and restore the run from
// needs_operator to running, then enqueues downstream + re-checks run completion.
//
// It deliberately REFUSES verdict-capable jobs (review / phase_synthesis): those
// complete via a recorded verdict, and finalizing them from an artifact would
// bypass the RFC 0118 verdict-attestation gate. The operator routes those
// through review override instead.
func HandleRecoveryCompleteStalled(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	jobID := stringParam(envelope, "job_id")
	if runID == "" || jobID == "" {
		return nil, rpc.NewError("schema_invalid", "recovery.complete_stalled requires run_id and job_id", nil)
	}
	dryRun := boolParam(envelope, "dry_run")
	force := boolParam(envelope, "force")
	summary := stringParam(envelope, "summary")

	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first, scoped via the job's immutable
		// run_id (mirrors HandleRecoveryReseal). complete-stalled completes a job
		// and may complete the run, inverting against claim/verdict-completion.
		if err := lockRunForJob(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		}
		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rpc.NewError("not_found", fmt.Sprintf("could not find jobs row for %q", jobID), nil)
		}
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(job["run_id"]) != runID {
			return nil, rpc.NewError("invalid_transition", "job does not belong to the given run_id", nil)
		}
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, false)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rpc.NewError("not_found", fmt.Sprintf("could not find runs row for %q", runID), nil)
		}
		if err != nil {
			return nil, err
		}
		runState := fmt.Sprint(run["state"])
		if runState != "running" && runState != "needs_operator" {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
				"run is %s; complete-stalled applies to a running or needs_operator run", runState), nil)
		}

		jobState := fmt.Sprint(job["state"])
		if terminalJobStates[jobState] {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
				"job is already terminal (%s); nothing to finalize", jobState), nil)
		}
		if jobState != "running" && jobState != "claimed" && jobState != "stale_lease" {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
				"complete-stalled finalizes a running/claimed/stale_lease job; state=%q", jobState), nil)
		}

		// RFC 0118: verdict-capable jobs complete via a recorded, attested verdict.
		// Finalizing one from its artifact would bypass verdict attestation, so it
		// is refused unconditionally (no --force escape) — the operator uses review
		// override / recovery reseal for those.
		if isVerdictCapableJobType(fmt.Sprint(job["job_type"])) {
			label := verdictCapableJobLabel(fmt.Sprint(job["job_type"]))
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
				"%s jobs complete via a recorded verdict; complete-stalled must not bypass verdict attestation (RFC 0118) — use review override instead", label), nil)
		}

		// Liveness guard: complete-stalled finalizes a DEAD lane. If the job still
		// holds a live (active, unexpired) lease whose OWNING SESSION is also still
		// active, the lane is alive — yanking the job out from under it would corrupt
		// provenance. Refuse in both the default and --force paths; the live lane
		// completes via the normal work.complete path.
		//
		// #309: the live test is SESSION liveness, not the lease TIME deadline alone.
		// A recovery `requeue_same_attempt` renews the lease's expires_at on a lane
		// that is already gone, so a time-only guard read a confirmed-dead session's
		// renewed lease as "alive" and forced the operator to wait out a fiction
		// (~31 min observed). A lease whose owning session is stopped/closed/absent is
		// NOT live regardless of how recently a requeue pushed its deadline forward,
		// so it is finalizable immediately. The guard still refuses a genuinely live
		// lane (active lease AND active owning session).
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
				return nil, err
			}
			if live {
				return nil, rpc.NewError("invalid_transition",
					"job still holds a live active lease whose owning session is still active (the lane is alive); complete-stalled finalizes a dead lane — close/expire the session first, or let the lane complete via work.complete", nil)
			}
		}

		// Dead-end signature: an open recovery_exhausted blocker on the job. This
		// is the exact state #292 dead-ends in; restricting the verb to it keeps it
		// from finalizing a healthy in-flight job. Keyed on the EXISTENCE of an
		// open recovery_exhausted blocker (not merely the newest open blocker), so a
		// later autonomous blocker stacked on top does not mask the dead-end.
		// --force relaxes this so a job stalled for an adjacent reason (with durable
		// artifacts) can still be finalized after operator inspection.
		blocker, err := openRecoveryExhaustedBlockerForJob(ctx, tx, repositoryID, jobID)
		if err != nil {
			return nil, err
		}
		hasExhausted := blocker != nil
		if !hasExhausted && !force {
			return nil, rpc.NewError("invalid_transition",
				"complete-stalled requires an open recovery_exhausted blocker on the job (the autonomous-recovery dead-end); pass --force to finalize a job stalled for another reason after inspection", nil)
		}

		// Gate 1: every required expected_artifact row exists (publish happened).
		if err := verifyRequiredArtifacts(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		}
		// Gate 2: every required artifact BODY is reconstructable from its declared
		// placement (RFC 0125 P0-3) — the whole premise of #292 ("the deliverable
		// is durable"). Worktree-independent, so it passes even after the per-job
		// worktree was GC'd, as long as the body is on the run branch / blob store.
		recon, err := verifyRequiredArtifactReconstructable(ctx, tx, repositoryID, job)
		if err != nil {
			return nil, err
		}
		if failed := failedReconstructions(recon); len(failed) > 0 {
			return nil, rpc.NewError("invalid_transition",
				"complete-stalled refused: a required artifact body is not reconstructable from its declared placement; restore the body (then recovery reseal) before finalizing",
				map[string]any{
					"run_id":            runID,
					"job_id":            jobID,
					"unreconstructable": reconstructionLedgerEntries(failed),
				})
		}

		base := map[string]any{
			"run_id":          runID,
			"job_id":          jobID,
			"workflow_job_id": job["workflow_job_id"],
			"job_state":       jobState,
			"run_state":       runState,
		}
		if blocker != nil {
			base["blocker_id"] = blocker["blocker_id"]
			base["blocker_kind"] = blocker["blocker_kind"]
		}
		if len(recon) > 0 {
			base["artifacts"] = reconstructionLedgerEntries(recon)
		}

		if dryRun {
			base["status"] = "dry_run"
			base["would_complete"] = true
			base["next_actions"] = []string{"recovery complete-stalled (omit --dry-run to finalize)"}
			return base, nil
		}

		completion, err := finalizeStalledJob(ctx, tx, repositoryID, job, summary)
		if err != nil {
			return nil, err
		}
		runAfter, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, false)
		if err != nil {
			return nil, err
		}
		base["status"] = "completed"
		base["run_state_after"] = runAfter["state"]
		base["completion"] = completion
		base["next_actions"] = []string{"monitor_run_progress", "export_run_evidence"}
		return base, nil
	})
}

// openRecoveryExhaustedBlockerForJob returns the oldest open recovery_exhausted
// blocker row for a job, or nil. Unlike openBlockerForJob (newest of ANY kind),
// it keys on the kind so a later autonomous blocker stacked on top of the
// exhaustion escalation cannot mask the #292 dead-end.
func openRecoveryExhaustedBlockerForJob(ctx context.Context, runner any, repositoryID, jobID string) (map[string]any, error) {
	row, err := oneRow(ctx, runner, `
		SELECT blocker_id, severity, blocker_kind, description, state, created_at
		  FROM striatumd.blockers
		 WHERE repository_id = $1
		   AND job_id = $2
		   AND state = 'open'
		   AND blocker_kind = $3
		 ORDER BY created_at, blocker_id
		 LIMIT 1`, repositoryID, jobID, recoveryExhaustedBlockerKind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// finalizeStalledJob performs the server-side completion of a stalled job whose
// required artifacts are already durable. It mirrors work.complete's terminal
// half — minus the live-lane preconditions (active lease, write-scope clean,
// porter re-commit) the dead lane can no longer satisfy — and reuses
// resolveAutonomousBlockersOnCompletion so the moot recovery_exhausted blocker +
// escalation are resolved and a needs_operator run is restored to running, in
// the same transaction. The caller has already verified artifact presence +
// reconstructability and the verdict-capable / dead-end preconditions.
func finalizeStalledJob(ctx context.Context, tx db.TxRunner, repositoryID string, job map[string]any, summary string) (map[string]any, error) {
	jobID := fmt.Sprint(job["job_id"])
	runID := fmt.Sprint(job["run_id"])
	now := nowString()
	messageID := nullable(job["current_message_id"])
	leaseID := nullable(job["current_lease_id"])
	completeSummary := completeStalledSummary
	if summary != "" {
		completeSummary = completeStalledSummary + ": " + summary
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
	if leaseID != nil {
		if err := tx.Exec(ctx, `
			UPDATE striatumd.leases
			   SET state = 'released', released_at = COALESCE(released_at, $1),
			       release_reason = COALESCE(release_reason, 'operator_complete_stalled')
			 WHERE repository_id = $2 AND lease_id = $3`, now, repositoryID, leaseID); err != nil {
			return nil, err
		}
	}
	if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.stalled_job_completed", nil, jobID, messageID, nil, leaseID, map[string]any{
		"workflow_job_id": job["workflow_job_id"],
		"attempt":         job["attempt"],
		"summary":         completeSummary,
		"source":          "recovery.complete_stalled",
	}); err != nil {
		return nil, err
	}
	if _, err := appendEvent(ctx, tx, repositoryID, runID, "job.completed", nil, jobID, messageID, nil, leaseID, map[string]any{
		"summary": completeSummary,
	}); err != nil {
		return nil, err
	}
	// #207 part C: resolve the completing job's moot recovery_exhausted blocker +
	// escalation_inbox row and, when it was the run's sole escalation, restore the
	// run from needs_operator to running. This is the same routine work.complete
	// runs — completion must be ordered before maybeCompleteRun, which only acts
	// on a running run.
	if err := resolveAutonomousBlockersOnCompletion(ctx, tx, repositoryID, runID, jobID, "", now); err != nil {
		return nil, err
	}
	if err := markJobTerminal(ctx, tx, repositoryID, runID, jobID); err != nil {
		return nil, err
	}
	if err := maybeEnqueueDownstream(ctx, tx, repositoryID, jobID); err != nil {
		return nil, err
	}
	if err := maybeCompleteRun(ctx, tx, repositoryID, runID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "completed", "job_id": jobID}, nil
}
