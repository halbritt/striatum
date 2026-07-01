package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/jackc/pgx/v5"
)

// RFC 0118 P1-5 (GH #240): the durable run_completion_record. Snapshot the
// run's last-live state INSIDE the terminal transaction, BEFORE
// closeRemainingSessions transitions supervisor rows and before read-side
// terminal filters would hide them — the record answers "what was alive,
// attested, leased, and override-cleared when this run ended?" without any
// live probe or daemon archaeology. The record is written exactly once
// (operator decision #3) and its sha256 is anchored in the append-only
// terminal event payload.

// buildRunCompletionRecord assembles the record. extra keys (e.g. the P0-3
// provenance ledger and completion_mode on the completed path) are merged
// into the top level. Active sessions are probed while their lanes are still
// live; closingReason records the imminent close reason the terminal
// transition is about to apply to them.
func buildRunCompletionRecord(ctx context.Context, runner any, repositoryID, runID, terminalState, closingReason string, extra map[string]any) (map[string]any, error) {
	record := map[string]any{
		"schema_version": "striatumd.run_completion_record.v1",
		"run_id":         runID,
		"terminal_state": terminalState,
		"recorded_at":    nowString(),
	}
	for key, value := range extra {
		record[key] = value
	}

	jobs, err := queryRows(ctx, runner, `
		SELECT workflow_job_id, job_id, job_type, state, attempt
		  FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2
		 ORDER BY created_at, job_id`, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	record["jobs"] = jobs

	verdicts, err := queryRows(ctx, runner, `
		SELECT v.job_id, v.verdict_id, v.verdict, v.posture,
		       v.lane_attestation_at_record, v.review_provenance_override,
		       v.review_provenance_decision_id, v.supervisor_id_at_record,
		       v.created_at`+verdictModelIdentitySelectProjection(ctx, runner, "v")+`
		  FROM striatumd.verdicts v
		 WHERE v.repository_id = $1 AND v.run_id = $2
		   AND v.superseded_by_decision_id IS NULL
		 ORDER BY v.created_at, v.verdict_id`, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	record["verdicts"] = verdicts

	// RFC 0125 P1-1 (#286): the content-addressed RUN_LEDGER — a self-contained,
	// per-gate record of every required artifact body's reconstructability
	// provenance (placement, content/blob sha256, git anchor ref/commit,
	// readback_verified) plus the gate's verdict + frozen attestation. The
	// completion record's own sha256 is anchored in the append-only terminal
	// event, so a retrospective reconstructs every gate/verdict/SHA OFFLINE from
	// the ledger hash alone — no live daemon archaeology. Assembled only for a
	// completed run (the audit case); a canceled/failed run's bodies may
	// legitimately be absent.
	if terminalState == "completed" {
		runLedger, err := buildRunArtifactLedger(ctx, runner, repositoryID, runID)
		if err != nil {
			return nil, err
		}
		record["run_ledger"] = runLedger
	}

	sessionRows, err := queryRows(ctx, runner, `
		SELECT session_id, role_id, lane_id, state, close_reason,
		       registered_at, closed_at, last_pty_activity_at
		  FROM striatumd.sessions
		 WHERE repository_id = $1 AND run_id = $2
		 ORDER BY registered_at, session_id`, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	sessions := make([]map[string]any, 0, len(sessionRows))
	for _, row := range sessionRows {
		sessionID := fmt.Sprint(row["session_id"])
		entry := map[string]any{
			"session_id":        sessionID,
			"role_id":           row["role_id"],
			"lane_id":           row["lane_id"],
			"state":             row["state"],
			"registered_at":     row["registered_at"],
			"closed_at":         nullable(row["closed_at"]),
			"pty_activity_seen": nullable(row["last_pty_activity_at"]) != nil,
		}
		if fmt.Sprint(row["state"]) == "active" {
			// Probed while the lane is still live — this is the last-live
			// liveness/attestation class the retrospective needs.
			entry["attestation"] = sessionLaneAttestation(ctx, runner, repositoryID, sessionID)
			entry["close_reason"] = closingReason
		} else {
			entry["close_reason"] = nullable(row["close_reason"])
		}
		sessions = append(sessions, entry)
	}
	record["sessions"] = sessions

	supervisors, err := queryRows(ctx, runner, `
		SELECT supervisor_id, session_id, state, pid
		  FROM striatumd.process_supervisors
		 WHERE repository_id = $1 AND run_id = $2
		 ORDER BY supervisor_id`, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	record["supervisors"] = supervisors

	leases, err := queryRows(ctx, runner, `
		SELECT lease_id, owner_session_id, resource_id, state, expires_at,
		       (state = 'active' AND expires_at < now()) AS expired_while_active
		  FROM striatumd.leases
		 WHERE repository_id = $1 AND run_id = $2
		 ORDER BY acquired_at, lease_id`, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	record["leases"] = leases

	trajectoryRow, err := oneRow(ctx, runner, `
		SELECT count(*) AS n FROM striatumd.trajectory_segments
		 WHERE repository_id = $1 AND run_id = $2`, repositoryID, runID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		record["trajectory_segment_count"] = 0
	case err != nil:
		return nil, err
	default:
		record["trajectory_segment_count"] = intValue(trajectoryRow["n"])
	}

	recoveryEvents, err := queryRows(ctx, runner, `
		SELECT event_type, count(*) AS n
		  FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2
		   AND (event_type LIKE 'recovery.%'
		     OR event_type IN ('job.auto_finalized','artifact.auto_finalized','work.claim_overridden'))
		 GROUP BY event_type
		 ORDER BY event_type`, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	record["recovery_events"] = recoveryEvents

	return record, nil
}

// persistRunCompletionRecord writes the record (write-once) and returns the
// sha256 over its canonical JSON for anchoring in the terminal event. When a
// record already exists (decision #3 overwrite guard) it returns "", false —
// the terminal event then omits the anchor rather than anchoring a hash that
// does not match the stored record.
func persistRunCompletionRecord(ctx context.Context, runner any, repositoryID, runID string, record map[string]any) (string, bool, error) {
	recordArg, err := db.JSONBArg(runner, record)
	if err != nil {
		return "", false, err
	}
	rows, err := queryRows(ctx, runner, `
		UPDATE striatumd.runs
		   SET completion_record_json = $1::jsonb
		 WHERE repository_id = $2 AND run_id = $3
		   AND completion_record_json IS NULL
		RETURNING run_id`, recordArg, repositoryID, runID)
	if err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	// Hash what a reader will get back from PostgreSQL (jsonb round-trip), not
	// the Go map's own serialization: encoding/json sorts map keys, and the
	// stored jsonb re-marshalled through the same path is what a retrospective
	// re-hashes against the anchored event.
	stored, err := oneRow(ctx, runner, `
		SELECT completion_record_json FROM striatumd.runs
		 WHERE repository_id = $1 AND run_id = $2`, repositoryID, runID)
	if err != nil {
		return "", false, err
	}
	canonical, err := json.Marshal(asMap(stored["completion_record_json"]))
	if err != nil {
		return "", false, err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), true, nil
}

// freezeRunCompletionRecord builds, persists, and (when newly written) folds
// the sha256 anchor into the terminal event payload. Call it INSIDE the
// terminal transaction, before the runs.state UPDATE and before
// closeRemainingSessions.
func freezeRunCompletionRecord(ctx context.Context, runner any, repositoryID, runID, terminalState, closingReason string, extra map[string]any, eventPayload map[string]any) (map[string]any, error) {
	// RFC 0122 §6: a terminal run can never be auto-spawned again. Revoke its
	// spawn-authorization grant at the single finalization chokepoint so every
	// terminal path (complete/fail/cancel/compromise) drops the grant — defense in
	// depth beside the scheduler's own terminal-state predicate.
	if err := revokeSpawnAuthorizationGrant(ctx, runner, repositoryID, runID, "run_"+terminalState); err != nil {
		return nil, err
	}
	record, err := buildRunCompletionRecord(ctx, runner, repositoryID, runID, terminalState, closingReason, extra)
	if err != nil {
		return nil, err
	}
	digest, written, err := persistRunCompletionRecord(ctx, runner, repositoryID, runID, record)
	if err != nil {
		return nil, err
	}
	if !written {
		return eventPayload, nil
	}
	if eventPayload == nil {
		eventPayload = map[string]any{}
	}
	eventPayload["completion_record_sha256"] = digest
	return eventPayload, nil
}

// buildRunArtifactLedger assembles the RFC 0125 P1-1 RUN_LEDGER (#286): one
// self-contained entry per completed verdict-capable job (review /
// phase_synthesis), carrying its latest non-superseded verdict + frozen
// attestation and the reconstructability provenance of every required artifact
// body (placement, content_sha256, blob_sha256, git_anchor_ref/commit,
// readback_verified). It reuses verifyRequiredArtifactReconstructable (#285) so
// the gate decision and the durable ledger walk the same required-artifact set.
func buildRunArtifactLedger(ctx context.Context, runner any, repositoryID, runID string) ([]map[string]any, error) {
	jobs, err := queryRows(ctx, runner, `
		SELECT job_id, workflow_job_id, job_type, attempt
		  FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2
		   AND state = 'completed'
		   AND job_type IN ('review','phase_synthesis')
		 ORDER BY created_at, job_id`, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	ledger := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		jobID := fmt.Sprint(j["job_id"])
		entry := map[string]any{
			"workflow_job_id": j["workflow_job_id"],
			"job_id":          jobID,
			"job_type":        j["job_type"],
			"attempt":         j["attempt"],
		}
		// The ledger is provenance RECORDING, not a gate — the gate already ran in
		// verifyRunCompletionProvenance. A per-job probe fault here must never roll
		// back a completion the gate approved, so record it and continue rather
		// than propagating the error up the terminal-transaction path.
		if job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, false); err != nil {
			entry["ledger_probe_error"] = err.Error()
		} else if recon, err := verifyRequiredArtifactReconstructable(ctx, runner, repositoryID, job); err != nil {
			entry["ledger_probe_error"] = err.Error()
		} else if len(recon) > 0 {
			entry["artifacts"] = reconstructionLedgerEntries(recon)
		}
		verdict, vErr := oneRow(ctx, runner, `
			SELECT v.verdict_id, v.verdict, v.posture, v.lane_attestation_at_record`+verdictModelIdentitySelectProjection(ctx, runner, "v")+`
			  FROM striatumd.verdicts v
			 WHERE v.repository_id = $1 AND v.job_id = $2
			   AND v.superseded_by_decision_id IS NULL
			 ORDER BY v.created_at DESC, v.verdict_id DESC
			 LIMIT 1`, repositoryID, jobID)
		switch {
		case errors.Is(vErr, pgx.ErrNoRows):
			// no verdict (e.g. dissent absorbed downstream) — leave verdict fields unset
		case vErr != nil:
			return nil, vErr
		default:
			entry["verdict_id"] = verdict["verdict_id"]
			entry["verdict"] = verdict["verdict"]
			entry["posture"] = verdict["posture"]
			entry["lane_attestation_at_record"] = nullable(verdict["lane_attestation_at_record"])
			attachVerdictModelIdentityFields(entry, verdict)
		}
		ledger = append(ledger, entry)
	}
	return ledger, nil
}
