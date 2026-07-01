package mutations

import (
	"context"
	"errors"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

// RFC 0118 P0-3 (GH #240): the run-completion provenance gate. The
// run_45aa8852 incident was a run that reached `completed` although no
// independent reviewer process ever ran — every per-job admission gate had
// been individually satisfied or bypassed, and nothing re-checked the whole
// run at its completion boundary. This gate re-verifies every
// provenance-required review gate against the FROZEN verdict stamp (P0-1)
// before maybeCompleteRun may choose a clean completion.
//
// RFC 0135 P5 (D216) — the revision-coherence half of this gate IS the sealed
// expectation barrier primitive (db.BarrierReadySQL) with seal :=
// review_generation. RFC 0126's finalization rule ("for each build, every
// required reviewer obligation has a non-superseded ACCEPTING verdict stamped
// with the build's CURRENT review_generation") is the per-build set-difference
// `required_obligations MINUS current-generation accepting verdicts` — exactly
// the primitive's readiness predicate `bool_and(is_terminal_gap OR
// staged.seal = live.seal)` projected onto entity=review_obligation,
// seal=review_generation, where a verdict stamped below the build's live
// generation is structurally absent from the satisfying set (never a COUNT, never
// a recency read). On a build revision, bumpReviewGeneration advances the entity's
// live seal and resetDownstreamForRevision returns the reviewer to `blocked` (no
// current-seal verdict), so a stale prior-generation needs_revision can no longer
// be named as a satisfying obligation. P5 RENAMES this already-shipped mechanism
// as the canonical seal instance — it does NOT change behavior; the equivalence is
// proven by TestRevisionCoherenceIsTheSealInstance.

const provenanceGateFailedBlockerKind = "provenance_gate_failed"

// verifyRunCompletionProvenance evaluates the completion invariant over every
// completed verdict-capable job of the run and returns the per-gate ledger
// (recorded into the run.completed event) plus the failing gates.
//
// Scope rule (normative, RFC 0118 Design): only gates the workflow declares
// as requiring independent provenance (require_attested_lane=true OR
// fresh/reviewer_context_policy=fresh) must show a frozen
// lane_attestation_at_record='attested' stamp or an explicit override basis;
// every other gate is held to the shipped admission rule (accepting +
// present), which legitimately admits posture=neutral verdicts from
// unattested sessions. A NULL stamp (pre-migration row) is fail-closed per
// operator decision #1. A completed job whose latest non-superseded verdict
// is non-accepting cleared no gate itself (dissent absorbed by a downstream
// adjudicator, #77) — the clearing gate carries its own ledger entry.
func verifyRunCompletionProvenance(ctx context.Context, runner any, repositoryID, runID string) (ledger []map[string]any, failing []map[string]any, err error) {
	jobs, err := queryRows(ctx, runner, `
		SELECT * FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2
		   AND state = 'completed'
		   AND job_type IN ('review','phase_synthesis')
		 ORDER BY created_at, job_id`, repositoryID, runID)
	if err != nil {
		return nil, nil, err
	}
	escapeDecisionID := ""
	escapeLooked := false
	for _, job := range jobs {
		def, err := workflowJobDefinitionForRow(ctx, runner, repositoryID, job)
		if err != nil {
			return nil, nil, err
		}
		provenanceRequired := def["require_attested_lane"] == true || reviewRequiresIndependentProvenance(job, def)
		jobID := fmt.Sprint(job["job_id"])
		entry := map[string]any{
			"workflow_job_id":     job["workflow_job_id"],
			"job_id":              jobID,
			"provenance_required": provenanceRequired,
		}
		ledger = append(ledger, entry)
		if provenanceRequired {
			if artErr := verifyRequiredArtifacts(ctx, runner, repositoryID, jobID); artErr != nil {
				var rpcErr *rpc.Error
				if !errors.As(artErr, &rpcErr) {
					return nil, nil, artErr
				}
				entry["failure"] = "required_artifacts_missing"
				entry["failure_detail"] = rpcErr.Message
				failing = append(failing, entry)
				continue
			}
			// RFC 0125 P0-3 (#285): the row exists — now re-verify every required
			// artifact BODY is reconstructable from its declared placement, not
			// merely that the row + size_bytes survive. The per-artifact results
			// are recorded into the gate ledger (and the RUN_LEDGER, #286);
			// positive evidence of a gone/corrupt body fails the gate with key
			// required_artifact_unreconstructable, orthogonal to the verdict path.
			recon, rerr := verifyRequiredArtifactReconstructable(ctx, runner, repositoryID, job)
			if rerr != nil {
				return nil, nil, rerr
			}
			if len(recon) > 0 {
				entry["artifacts"] = reconstructionLedgerEntries(recon)
			}
			if failed := failedReconstructions(recon); len(failed) > 0 {
				entry["failure"] = "required_artifact_unreconstructable"
				entry["unreconstructable"] = reconstructionLedgerEntries(failed)
				failing = append(failing, entry)
				continue
			}
		}
		verdictRow, err := oneRow(ctx, runner, `
			SELECT * FROM striatumd.verdicts
			 WHERE repository_id = $1 AND job_id = $2
			   AND superseded_by_decision_id IS NULL
			 ORDER BY created_at DESC, verdict_id DESC
			 LIMIT 1`, repositoryID, jobID)
		if errors.Is(err, pgx.ErrNoRows) {
			entry["basis"] = "no_verdict"
			if provenanceRequired {
				entry["failure"] = "missing_verdict"
				failing = append(failing, entry)
			}
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		verdict := fmt.Sprint(verdictRow["verdict"])
		entry["verdict"] = verdict
		entry["verdict_id"] = verdictRow["verdict_id"]
		attachVerdictModelIdentityFields(entry, verdictRow)
		if !isAcceptingVerdict(verdict) {
			entry["basis"] = "non_accepting"
			continue
		}
		attested := fmt.Sprint(nullable(verdictRow["lane_attestation_at_record"])) == "attested"
		override := verdictRow["review_provenance_override"] == true || fmt.Sprint(verdictRow["posture"]) == "override"
		switch {
		case override:
			entry["basis"] = "override"
			entry["posture"] = verdictRow["posture"]
			if id := nullable(verdictRow["review_provenance_decision_id"]); id != nil {
				entry["override_decision_id"] = id
			}
		case attested:
			entry["basis"] = "attested"
			if id := nullable(verdictRow["supervisor_id_at_record"]); id != nil {
				entry["supervisor_id_at_record"] = id
			}
		case !provenanceRequired:
			entry["basis"] = "admitted_unattested"
		default:
			// One run-level accepting escape_surface=review_provenance decision
			// (the same evidence the admission gate accepts) is an operator
			// override basis for the run's failing gates — this is the resume
			// path: record the decision, resolve the escalation, re-drive.
			if !escapeLooked {
				escapeLooked = true
				escapeDecisionID, err = runLevelReviewProvenanceEscapeDecision(ctx, runner, repositoryID, runID)
				if err != nil {
					return nil, nil, err
				}
			}
			if escapeDecisionID != "" {
				entry["basis"] = "override"
				entry["override_decision_id"] = escapeDecisionID
				entry["override_source"] = "run_level_escape_decision"
				continue
			}
			entry["basis"] = "unattested"
			entry["failure"] = "unattested_without_override"
			failing = append(failing, entry)
		}
	}
	return ledger, failing, nil
}

// runLevelReviewProvenanceEscapeDecision returns the newest run-level
// accepting escape_surface=review_provenance decision for the run, verified
// with the same predicate the admission gate uses, or "" when none exists.
func runLevelReviewProvenanceEscapeDecision(ctx context.Context, runner any, repositoryID, runID string) (string, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT payload_json
		  FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2
		   AND event_type = 'decision.recorded'
		   AND payload_json->>'escape_surface' = 'review_provenance'
		 ORDER BY event_id DESC`, repositoryID, runID)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		decisionID := fmt.Sprint(asMap(row["payload_json"])["decision_id"])
		if decisionID == "" || decisionID == "<nil>" {
			continue
		}
		if _, err := verifyReviewProvenanceDecision(ctx, runner, repositoryID, runID, decisionID); err == nil {
			return decisionID, nil
		}
	}
	return "", nil
}

// escalateProvenanceGateFailure routes a failing completion gate to
// needs_operator/provenance_gate_failed with a resolvable escalation (RFC
// 0062 blockers + escalation_inbox), enumerating the failing gate ids. The
// run is NOT terminal: sessions stay open and the operator resumes by
// recording a review_provenance escape decision (or overriding the verdict)
// and resolving the escalation, which re-drives completion.
func escalateProvenanceGateFailure(ctx context.Context, runner any, repositoryID, runID string, failing []map[string]any) error {
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	now := nowString()
	alreadyOpen, err := existsRow(ctx, runner, `
		SELECT 1 FROM striatumd.blockers
		 WHERE repository_id = $1 AND run_id = $2
		   AND blocker_kind = $3 AND state = 'open'
		 LIMIT 1`, repositoryID, runID, provenanceGateFailedBlockerKind)
	if err != nil {
		return err
	}
	if !alreadyOpen {
		blockerID, err := newID("blk")
		if err != nil {
			return err
		}
		failingGateIDs := make([]any, 0, len(failing))
		unreconstructable := 0
		for _, gate := range failing {
			failingGateIDs = append(failingGateIDs, gate["workflow_job_id"])
			if gate["failure"] == "required_artifact_unreconstructable" {
				unreconstructable++
			}
		}
		description := fmt.Sprintf(
			"run completion blocked: %d provenance-required review gate(s) lack an attested or override provenance basis; record an accepting review_provenance escape decision (decision record --escape-surface review_provenance) or override the verdict, then resolve this escalation to re-drive completion",
			len(failing),
		)
		suggestedActions := []any{
			"record an accepting run-level decision with escape_surface=review_provenance, then escalation resolve --decision-id <id>",
			"review override --auto-fresh-session to force a terminal operator verdict on the gate",
			"cancel the run",
		}
		if unreconstructable > 0 {
			// A missing/corrupt body is not fixed by a verdict override; the body
			// must be restored and re-probed (RFC 0125 same-attempt reseal).
			description = fmt.Sprintf(
				"run completion blocked: %d gate(s) have an unreconstructable required artifact body (the row survives but the body is gone or corrupt at its declared placement); restore the body, then recovery reseal the job to re-probe durability and re-drive completion",
				unreconstructable,
			)
			suggestedActions = append([]any{
				"restore the artifact body at its declared placement, then recovery reseal <blocker-id> to re-probe durability on the same attempt",
			}, suggestedActions...)
		}
		payload := map[string]any{
			"schema_version":             "striatumd.provenance_gate_escalation.v1",
			"source":                     "run.completion_gate",
			"is_escalation":              true,
			"blocker_kind":               provenanceGateFailedBlockerKind,
			"severity":                   "blocked",
			"failing_gates":              failing,
			"unreconstructable_count":    unreconstructable,
			"suggested_operator_actions": suggestedActions,
		}
		payloadArg, err := db.JSONBArg(runner, payload)
		if err != nil {
			return err
		}
		if err := exec.Exec(ctx, `
			INSERT INTO striatumd.blockers (
			  repository_id, blocker_id, run_id, job_id, session_id, severity,
			  blocker_kind, description, state, created_at, payload_json
			)
			VALUES ($1,$2,$3,NULL,NULL,'blocked',$4,$5,'open',$6,$7::jsonb)`,
			repositoryID, blockerID, runID,
			provenanceGateFailedBlockerKind, description, now, payloadArg,
		); err != nil {
			return err
		}
		if err := exec.Exec(ctx, `
			INSERT INTO striatumd.escalation_inbox (
			  repository_id, escalation_id, run_id, job_id, session_id,
			  blocker_id, blocker_kind, severity, state, created_at, payload_json
			)
			VALUES ($1,$2,$3,NULL,NULL,$4,$5,'blocked','pending',$6,$7::jsonb)`,
			repositoryID, blockerID, runID,
			blockerID, provenanceGateFailedBlockerKind, now, payloadArg,
		); err != nil {
			return err
		}
		if _, err := appendEvent(ctx, runner, repositoryID, runID, "run.escalated", nil, nil, nil, nil, nil, map[string]any{
			"blocker_id":       blockerID,
			"blocker_kind":     provenanceGateFailedBlockerKind,
			"failing_gate_ids": failingGateIDs,
		}); err != nil {
			return err
		}
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.runs
		   SET state = 'needs_operator', stop_reason = 'provenance_gate_failed'
		 WHERE repository_id = $1 AND run_id = $2 AND state = 'running'`,
		repositoryID, runID); err != nil {
		return err
	}
	if _, err := appendEvent(ctx, runner, repositoryID, runID, "run.needs_operator", nil, nil, nil, nil, nil, map[string]any{
		"reason":           "provenance_gate_failed",
		"escalation_count": len(failing),
	}); err != nil {
		return err
	}
	return nil
}
