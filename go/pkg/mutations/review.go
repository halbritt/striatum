package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

func HandleRecordVerdict(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	jobID := stringParam(envelope, "job_id")
	leaseID := stringParam(envelope, "lease_id")
	verdict := normalizeVerdict(stringParam(envelope, "verdict"))
	findingsArtifactID := nullable(stringParam(envelope, "findings_artifact_id"))
	rationale := nullable(stringParam(envelope, "rationale"))
	reviewProvenanceDecisionID := reviewProvenanceDecisionParam(envelope)
	if sessionID == "" || jobID == "" || leaseID == "" || verdict == "" {
		return nil, rpc.NewError("schema_invalid", "review.verdict requires session_id, job_id, lease_id, and verdict", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		if _, err := enforceSessionBindingForSession(ctx, tx, repositoryID, sessionID, "review.verdict"); err != nil {
			return nil, err
		}
		// RFC 0104: take the per-run advisory lock first so the verdict-completion
		// path (recordVerdict -> maybeCompleteRun -> closeRemainingSessions) shares
		// the claim/sweep serialization point and the {sessions, runs} cycle cannot
		// form.
		if err := lockRunForJob(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		}
		return recordVerdict(ctx, tx, repositoryID, sessionID, jobID, leaseID, verdict, findingsArtifactID, rationale, recordVerdictOptions{
			ReviewProvenanceDecisionID: reviewProvenanceDecisionID,
			RequireWorkSessionBackend:  true,
			WorkSessionBackendPhase:    "record-verdict",
		})
	})
}

func HandleSubmitReview(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	jobID := stringParam(envelope, "job_id")
	leaseID := stringParam(envelope, "lease_id")
	pathText := stringParam(envelope, "path")
	verdict := normalizeVerdict(stringParam(envelope, "verdict"))
	// #96: capture whether the caller passed explicit identity flags BEFORE
	// applying defaults. When the verdict-capable job has exactly one required
	// expected artifact, we infer the missing logical_name/kind from it (below,
	// inside the transaction once the job row is loaded) so a job expecting e.g.
	// logical_name=test_gate_report does not silently fail the precheck under the
	// "review" default.
	logicalName := stringParam(envelope, "logical_name")
	kind := stringParam(envelope, "kind")
	logicalNameExplicit := logicalName != ""
	kindExplicit := kind != ""
	rationale := nullable(stringParam(envelope, "rationale"))
	reviewProvenanceDecisionID := reviewProvenanceDecisionParam(envelope)
	if sessionID == "" || jobID == "" || leaseID == "" || pathText == "" || verdict == "" {
		return nil, rpc.NewError("schema_invalid", "review.submit requires session_id, job_id, lease_id, path, and verdict", nil)
	}
	// #98: two concurrent submit-review calls completing sibling reviews under
	// the same run can have Postgres abort one with a deadlock (SQLSTATE 40P01).
	// Retry the whole transaction a bounded number of times; the body is
	// idempotent (publishArtifact already no-ops an already-published artifact).
	return withTxRetryOnDeadlock(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		if _, err := enforceSessionBindingForSession(ctx, tx, repositoryID, sessionID, "review.submit"); err != nil {
			return nil, err
		}
		// RFC 0104: per-run advisory lock first, before the jobs FOR UPDATE below.
		if err := lockRunForJob(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		}
		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
		if err != nil {
			return nil, err
		}
		applyFrozenAttemptWriteScope(ctx, tx, repositoryID, job, leaseID)
		// #96: when the caller omitted logical_name and/or kind, infer them from
		// the job's sole required expected artifact (resolving cycle placeholders
		// against the attempt so the inferred tuple matches the work packet and
		// verifyRequiredArtifacts). Inference only fires when there is exactly one
		// required expected artifact; otherwise we fall back to the historical
		// defaults below and the precheck error names the explicit tuple(s) the
		// caller must pass.
		logicalName, kind = inferSubmitReviewArtifactIdentity(
			job, pathText, logicalName, kind, logicalNameExplicit, kindExplicit,
		)
		if logicalName == "" {
			logicalName = "review"
		}
		if kind == "" {
			kind = "finding"
		}
		reviewProvenance, err := prevalidateSubmitReview(ctx, tx, repositoryID, job, sessionID, leaseID, logicalName, kind, pathText, verdict, reviewProvenanceDecisionID)
		if err != nil {
			return nil, err
		}
		// RFC 0095 Goal 8 / #206: refuse a finding byte-identical to this job's
		// prior-attempt finding. A re-claimed review job (attempt > 1) that adopts
		// the prior round's REVIEW.md verbatim re-asserts a stale verdict against a
		// target that was already revised — process theater that poisons the bounded
		// revision cycle. A fresh review of a CHANGED target producing a
		// byte-identical document is not plausible.
		if err := enforceFreshReviewNotByteIdentical(ctx, tx, repositoryID, job, kind, pathText); err != nil {
			return nil, err
		}
		if fmt.Sprint(job["state"]) == "claimed" && nullable(job["current_message_id"]) != nil {
			if err := ackInline(ctx, tx, repositoryID, sessionID, fmt.Sprint(job["current_message_id"]), leaseID); err != nil {
				return nil, err
			}
		}
		// RFC 0095 §7 / GH #58: publishArtifact now detects an
		// already-published artifact on either unique constraint
		// (logical-name tuple or repo_path+content_sha256 tuple) and returns
		// a {status: already_published} no-op instead of letting the INSERT
		// crash with a raw pgx 23505. That keeps publish-then-submit-review
		// idempotent inside this single transaction; the previous post-hoc
		// 23505 retry that re-ran the whole submit in a fresh transaction is
		// no longer needed (and its logical-name lookup could not even find
		// the row when the conflict was on the repo_path constraint).
		artifact, err := publishArtifactWithOptions(ctx, tx, repositoryID, sessionID, jobID, leaseID, kind, logicalName, pathText, publishArtifactOptions{
			ReviewProvenanceOverride: reviewProvenance != nil,
		})
		if err != nil {
			return nil, err
		}
		verdictResult, err := recordVerdict(ctx, tx, repositoryID, sessionID, jobID, leaseID, verdict, artifact["artifact_id"], rationale, recordVerdictOptions{
			ReviewProvenanceDecisionID: reviewProvenanceDecisionID,
		})
		if err != nil {
			return nil, err
		}
		finalJob, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, false)
		if err != nil {
			return nil, err
		}
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", fmt.Sprint(finalJob["run_id"]), false)
		if err != nil {
			return nil, err
		}
		downstream, err := downstreamJobs(ctx, tx, repositoryID, jobID)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"artifact":        artifact,
			"verdict":         verdictResult,
			"job_state":       finalJob["state"],
			"run_state":       run["state"],
			"blocker_id":      verdictResult["blocker_id"],
			"downstream_jobs": downstream,
		}, nil
	})
}

func HandleOverrideVerdict(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	jobID := stringParam(envelope, "job_id")
	verdict := stringParam(envelope, "verdict")
	rationale := stringParam(envelope, "rationale")
	findingsArtifactID := nullable(stringParam(envelope, "findings_artifact_id"))
	autoFreshSession := boolParam(envelope, "auto_fresh_session")
	if sessionID == "" || jobID == "" || verdict == "" || strings.TrimSpace(rationale) == "" {
		return nil, rpc.NewError("schema_invalid", "review.override requires session_id, job_id, verdict, and rationale", nil)
	}
	if verdict != "accept" && verdict != "accept_with_findings" {
		return nil, rpc.NewError("invalid_transition", "override verdict must be 'accept' or 'accept_with_findings'", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first, before the jobs FOR UPDATE below.
		if err := lockRunForJob(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		}
		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(job["job_type"]) != "review" {
			return nil, rpc.NewError("invalid_transition", "verdict override is valid only for review jobs", nil)
		}
		jobState := fmt.Sprint(job["state"])
		if jobState != "completed" && jobState != "waiting_human" {
			return nil, rpc.NewError("invalid_transition", "verdict override requires a completed or waiting_human review job", nil)
		}
		if autoFreshSession {
			resolved, err := resolveOverrideSession(ctx, tx, repositoryID, sessionID, jobID)
			if err != nil {
				return nil, err
			}
			sessionID = resolved
		}
		session, err := rowByID(ctx, tx, repositoryID, "sessions", "session_id", sessionID, false)
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(session["run_id"]) != fmt.Sprint(job["run_id"]) {
			return nil, rpc.NewError("invalid_transition", "override session does not belong to the job run", nil)
		}
		if fmt.Sprint(session["state"]) != "active" {
			return nil, rpc.NewError("invalid_transition", "override session must be active", nil)
		}
		hasOwnVerdict, err := existsRow(ctx, tx, `
			SELECT 1 FROM striatumd.verdicts
			 WHERE repository_id = $1 AND job_id = $2 AND session_id = $3
			 LIMIT 1`, repositoryID, jobID, sessionID)
		if err != nil {
			return nil, err
		}
		if hasOwnVerdict {
			return nil, rpc.NewError("invalid_transition", "override session already has a verdict for this job; register a fresh session", nil)
		}
		previous, err := oneRow(ctx, tx, `
			SELECT * FROM striatumd.verdicts
			 WHERE repository_id = $1 AND job_id = $2
			 ORDER BY created_at DESC, verdict_id DESC
			 LIMIT 1`, repositoryID, jobID)
		previousVerdictID := any(nil)
		previousVerdictValue := any(nil)
		previousVerdict := ""
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			previous = nil
		case err != nil:
			return nil, err
		default:
			previousVerdict = fmt.Sprint(previous["verdict"])
			previousVerdictID = previous["verdict_id"]
			previousVerdictValue = previousVerdict
		}
		if previousVerdict == "accept" || previousVerdict == "accept_with_findings" {
			return map[string]any{"status": "already_accepting", "job_id": jobID, "previous_verdict": previousVerdict}, nil
		}
		effectiveArtifactID := findingsArtifactID
		if effectiveArtifactID == nil && previous != nil {
			effectiveArtifactID = nullable(previous["findings_artifact_id"])
		}
		if effectiveArtifactID != nil {
			artifact, err := rowByID(ctx, tx, repositoryID, "artifacts", "artifact_id", fmt.Sprint(effectiveArtifactID), false)
			if err != nil {
				return nil, err
			}
			if fmt.Sprint(artifact["run_id"]) != fmt.Sprint(job["run_id"]) {
				return nil, rpc.NewError("invalid_transition", "findings artifact belongs to a different run", nil)
			}
			if fmt.Sprint(artifact["job_id"]) != jobID {
				return nil, rpc.NewError("invalid_transition", "findings artifact belongs to a different job", nil)
			}
		}
		verdictID, err := newID("verdict")
		if err != nil {
			return nil, err
		}
		// RFC 0118 P0-2: an operator override is never the workflow-resolved
		// posture — the row must read back as operator-cleared, and the frozen
		// stamp (P0-1) records the lane state the override was issued against.
		attestation := sessionLaneAttestation(ctx, tx, repositoryID, sessionID)
		now := nowString()
		// RFC 0126 P0 (D194 / GH #282): the operator-override INSERT intentionally
		// does NOT name review_generation. In production (owner bundle 0009 applied)
		// the column DEFAULTs to 1; stamping the live build generation onto an
		// override is deferred to P1/P2, when the generation-scoped completion gate
		// actually consults the column (this operator path is rare, and leaving it
		// unstamped here keeps both the runtime-only harness and production correct
		// without an adaptive branch on a non-hot path).
		if err := tx.Exec(ctx, `
			INSERT INTO striatumd.verdicts (
			  repository_id, verdict_id, run_id, job_id, session_id,
			  verdict, rationale, findings_artifact_id, created_at, posture,
			  lane_attestation_at_record, review_provenance_override,
			  review_provenance_decision_id, supervisor_id_at_record
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'override',$10,true,NULL,$11)`,
			repositoryID,
			verdictID,
			job["run_id"],
			jobID,
			sessionID,
			verdict,
			strings.TrimSpace(rationale),
			effectiveArtifactID,
			now,
			attestation["state"],
			attestation["supervisor_id"],
		); err != nil {
			return nil, err
		}
		resolvedBlockers := 0
		if jobState == "waiting_human" {
			messageID := nullable(job["current_message_id"])
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
			if _, err := appendEvent(ctx, tx, repositoryID, job["run_id"], "job.completed", sessionID, jobID, messageID, nil, nil, map[string]any{
				"summary": "review override accepted by operator",
				"source":  "review.override",
			}); err != nil {
				return nil, err
			}
			if err := markJobTerminal(ctx, tx, repositoryID, fmt.Sprint(job["run_id"]), jobID); err != nil {
				return nil, err
			}
			rows, err := queryRows(ctx, tx, `
					SELECT blocker_id FROM striatumd.blockers
					 WHERE repository_id = $1
				   AND job_id = $2
				   AND state = 'open'
				   AND severity = 'human_checkpoint'
				   AND blocker_kind = 'revision_routing'
				 FOR UPDATE`, repositoryID, jobID)
			if err != nil {
				return nil, err
			}
			resolvedBlockers = len(rows)
			if err := tx.Exec(ctx, `
				UPDATE striatumd.blockers
				   SET state = 'resolved', resolved_at = $1
				 WHERE repository_id = $2
				   AND job_id = $3
				   AND state = 'open'
				   AND severity = 'human_checkpoint'
				   AND blocker_kind = 'revision_routing'`, now, repositoryID, jobID); err != nil {
				return nil, err
			}
		}
		if _, err := appendEvent(ctx, tx, repositoryID, job["run_id"], "verdict.overridden", sessionID, jobID, nil, effectiveArtifactID, nil, map[string]any{
			"previous_verdict":    previousVerdictValue,
			"verdict":             verdict,
			"previous_verdict_id": previousVerdictID,
			"verdict_id":          verdictID,
			"resolved_blockers":   resolvedBlockers,
			"posture":             "override",
			"lane_attestation":    attestation["state"],
		}); err != nil {
			return nil, err
		}
		// #65 P1: an operator override that completes a parked review can be the
		// last reviewer of an interrogable target; retire the panel-owned window.
		if err := releaseInterrogationTargetForCompletedReview(ctx, tx, repositoryID, fmt.Sprint(job["run_id"]), jobID); err != nil {
			return nil, err
		}
		if err := maybeEnqueueDownstream(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		}
		if err := maybeCompleteRun(ctx, tx, repositoryID, fmt.Sprint(job["run_id"])); err != nil {
			return nil, err
		}
		status := "overridden"
		if previous == nil {
			status = "recovered"
		}
		return map[string]any{
			"status":               status,
			"job_id":               jobID,
			"previous_verdict":     previousVerdictValue,
			"verdict":              verdict,
			"verdict_id":           verdictID,
			"findings_artifact_id": effectiveArtifactID,
			"resolved_blockers":    resolvedBlockers,
		}, nil
	})
}

func resolveOverrideSession(ctx context.Context, runner any, repositoryID, requestedSessionID, jobID string) (string, error) {
	hasOwnVerdict, err := existsRow(ctx, runner, `
		SELECT 1 FROM striatumd.verdicts
		 WHERE repository_id = $1 AND job_id = $2 AND session_id = $3
		 LIMIT 1`, repositoryID, jobID, requestedSessionID)
	if err != nil {
		return "", err
	}
	if !hasOwnVerdict {
		return requestedSessionID, nil
	}
	requested, err := rowByID(ctx, runner, repositoryID, "sessions", "session_id", requestedSessionID, false)
	if err != nil {
		return requestedSessionID, nil
	}
	runID := fmt.Sprint(requested["run_id"])
	laneID := fmt.Sprint(requested["lane_id"])
	rows, err := queryRows(ctx, runner, `
		SELECT ordinal
		  FROM striatumd.sessions
		 WHERE repository_id = $1 AND run_id = $2
		   AND role_id = 'reviewer' AND lane_id = $3
		 FOR UPDATE`, repositoryID, runID, laneID)
	if err != nil {
		return "", err
	}
	ordinal := 1
	for _, row := range rows {
		if value := intValue(row["ordinal"]); value >= ordinal {
			ordinal = value + 1
		}
	}
	sessionID, err := newID("sess")
	if err != nil {
		return "", err
	}
	labelSuffix := jobID
	if len(labelSuffix) > 12 {
		labelSuffix = labelSuffix[len(labelSuffix)-12:]
	}
	operatorLabel := "operator-override-" + strings.ToLower(labelSuffix)
	slug := fmt.Sprintf("reviewer-%s-%d", laneID, ordinal)
	now := nowString()
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return "", fmt.Errorf("runner does not support exec")
	}
	capabilitiesArg, err := db.JSONBArg(runner, []string{})
	if err != nil {
		return "", err
	}
	if err := exec.Exec(ctx, `
		INSERT INTO striatumd.sessions (
		  repository_id, session_id, run_id, role_id, lane_id, slug, ordinal,
		  capabilities_json, parent_session_id, fresh_context, state,
		  registered_at, last_heartbeat_at, non_fresh_reason, operator_label
		)
		VALUES ($1,$2,$3,'reviewer',$4,$5,$6,$7::jsonb,NULL,true,'active',$8,$9,NULL,$10)`,
		repositoryID, sessionID, runID, laneID, slug, ordinal, capabilitiesArg, now, now, operatorLabel); err != nil {
		return "", err
	}
	if _, err := appendEvent(ctx, runner, repositoryID, runID, "session.registered", sessionID, nil, nil, nil, nil, map[string]any{
		"role":           "reviewer",
		"lane":           laneID,
		"slug":           slug,
		"operator_label": operatorLabel,
		"source":         "override_auto_fresh_session",
	}); err != nil {
		return "", err
	}
	return sessionID, nil
}

// normalizeVerdict maps reviewer-natural verdict synonyms onto the canonical
// daemon verdict vocabulary {accept, accept_with_findings, needs_revision,
// reject} (#132). Supervised reviewers and adapters reach for tokens like
// "accepted_with_follow_up", "approve", or "request_changes" that are not the
// canonical spelling; accepting the common synonyms avoids spurious submit
// failures and duplicate artifacts (#132) and steers change-request language to
// the recoverable needs_revision rather than terminal reject (#140). Unknown
// tokens pass through unchanged so the existing "unknown verdict" path still
// reports them.
func normalizeVerdict(verdict string) string {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "accept", "approve", "approved", "accepted":
		return "accept"
	case "accept_with_findings", "accepted_with_findings",
		"accept_with_follow_up", "accepted_with_follow_up",
		"accept_with_followup", "accepted_with_followup":
		return "accept_with_findings"
	case "needs_revision", "needs_revisions", "request_changes",
		"changes_requested", "request_change", "needs_changes", "revise":
		return "needs_revision"
	case "reject", "rejected":
		return "reject"
	default:
		return verdict
	}
}

func recordVerdict(
	ctx context.Context,
	runner any,
	repositoryID string,
	sessionID string,
	jobID string,
	leaseID string,
	verdict string,
	findingsArtifactID any,
	rationale any,
	options recordVerdictOptions,
) (map[string]any, error) {
	job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		return nil, err
	}
	if fmt.Sprint(job["job_type"]) != "review" && fmt.Sprint(job["job_type"]) != "phase_synthesis" {
		return nil, rpc.NewError("invalid_transition", "verdict is valid only for verdict-capable jobs", nil)
	}
	if _, err := activeLeaseFor(ctx, runner, repositoryID, leaseID, sessionID, jobID); err != nil {
		return nil, err
	}
	if fmt.Sprint(job["state"]) != "running" {
		label := "review"
		if fmt.Sprint(job["job_type"]) == "phase_synthesis" {
			label = "phase_synthesis"
		}
		return nil, rpc.NewError("invalid_transition", fmt.Sprintf("%s job must be running before verdict", label), nil)
	}
	reviewProvenance, gateAttestation, err := enforceReviewProvenancePolicy(ctx, runner, repositoryID, job, sessionID, verdict, "recording a verdict", options.ReviewProvenanceDecisionID)
	if err != nil {
		return nil, err
	}
	if options.ProvenanceOverrideBasis != "" && reviewProvenance["review_provenance_override"] != true {
		merged := map[string]any{
			"review_provenance_override": true,
			"review_provenance_basis":    options.ProvenanceOverrideBasis,
		}
		for key, value := range reviewProvenance {
			merged[key] = value
		}
		reviewProvenance = merged
	}
	if options.RequireWorkSessionBackend && reviewProvenance == nil {
		tx, ok := runner.(db.TxRunner)
		if !ok {
			return nil, fmt.Errorf("runner does not support review backend gate")
		}
		phase := options.WorkSessionBackendPhase
		if phase == "" {
			phase = "record-verdict"
		}
		if err := ensureWorkSessionBackend(ctx, tx, repositoryID, sessionID, phase); err != nil {
			return nil, err
		}
	}
	if err := verifyRequiredArtifacts(ctx, runner, repositoryID, jobID); err != nil {
		return nil, err
	}
	// Build finding 2: enforce collaboration_ledger verdict consistency on the
	// primitive verdict path too. Without this, an adjudicator could publish a
	// `verdict: needs_revision` ledger then record `accept` to clear the gate,
	// bypassing RFC 0093's substance gate. The submit-review path already guards
	// this in prevalidateSubmitReview; this closes the publish_artifact +
	// review.verdict path.
	if err := enforceCollaborationLedgerVerdict(ctx, runner, repositoryID, job, verdict); err != nil {
		return nil, err
	}
	if findingsArtifactID != nil {
		artifact, err := rowByID(ctx, runner, repositoryID, "artifacts", "artifact_id", fmt.Sprint(findingsArtifactID), false)
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(artifact["run_id"]) != fmt.Sprint(job["run_id"]) || fmt.Sprint(artifact["job_id"]) != jobID {
			return nil, rpc.NewError("invalid_transition", "findings artifact belongs to a different job", nil)
		}
	}
	return applyVerdict(ctx, runner, repositoryID, sessionID, jobID, leaseID, verdict, job, findingsArtifactID, rationale, reviewProvenance, gateAttestation)
}

type recordVerdictOptions struct {
	ReviewProvenanceDecisionID string
	RequireWorkSessionBackend  bool
	WorkSessionBackendPhase    string
	// ProvenanceOverrideBasis is set by operator/recovery surfaces that record
	// a verdict without an attested-lane admission gate of their own (RFC 0118
	// P0-2 mechanism 3, e.g. recovery auto-finalize). A non-empty basis stamps
	// review_provenance_override=true on the verdict row and names the basis in
	// the verdict.recorded event, so the verdict can never read back as a clean
	// lane-attested clear.
	ProvenanceOverrideBasis string
}

// applyVerdict records the verdict row and runs the completion / cycle / downstream
// routing for a verdict-bearing job. It is the post-precondition core of
// recordVerdict, factored out (#144) so the autonomous stale-lease recovery path
// can record a verdict (and route its cycle / downstream edge) for a review whose
// lane stalled after writing its finding artifact but before recording the verdict
// — without re-imposing recordVerdict's active-lease / attestation / running-state
// preconditions, which by construction do not hold for a stale lane. Every state
// mutation it performs (completeReviewJob / failReviewJob / routeRevisionCycle) is
// lease-state-tolerant (unconditional UPDATEs keyed by id), so it is safe on a
// stale lane.
func applyVerdict(ctx context.Context, runner any, repositoryID, sessionID, jobID, leaseID, verdict string, job map[string]any, findingsArtifactID, rationale any, reviewProvenance, gateAttestation map[string]any) (map[string]any, error) {
	verdictID, err := newID("verdict")
	if err != nil {
		return nil, err
	}
	now := nowString()
	posture, err := resolveReviewPosture(ctx, runner, repositoryID, job)
	if err != nil {
		return nil, err
	}
	// RFC 0118 P0-1: the frozen stamp records the SAME attestation map the
	// admission gate decided on. Only the recovery path (#144), which by
	// construction runs without an admission gate, probes here — once, before
	// the INSERT, so the stamp and the verdict.recorded event cannot disagree.
	attestation := gateAttestation
	if attestation == nil {
		attestation = sessionLaneAttestation(ctx, runner, repositoryID, sessionID)
	}
	// RFC 0118 reconciliation note 1: a session that claimed this job via the
	// admin work.claim_override escape (#222) is a non-independent claimant by
	// operator decision; carry the claim's authorizing decision onto the frozen
	// stamp even when the lane is attested at record time. A verdict-time gate
	// decision already in reviewProvenance takes precedence for the decision_id.
	if nullable(reviewProvenance["review_provenance_decision_id"]) == nil {
		claim, err := oneRow(ctx, runner, `
			SELECT payload_json->>'decision_id' AS decision_id
			  FROM striatumd.events
			 WHERE repository_id = $1
			   AND job_id = $2
			   AND actor_session_id = $3
			   AND event_type = 'work.claim_overridden'
			 ORDER BY event_id DESC
			 LIMIT 1`, repositoryID, jobID, sessionID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			merged := map[string]any{
				"review_provenance_override":    true,
				"review_provenance_decision_id": claim["decision_id"],
			}
			for key, value := range reviewProvenance {
				merged[key] = value
			}
			reviewProvenance = merged
		}
	}
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return nil, fmt.Errorf("runner does not support exec")
	}
	// RFC 0126 P0 (D194 / GH #282): stamp the verdict with the reviewed build's
	// CURRENT review_generation at record time so a later build revision (which
	// bumps the build's generation in reopenJobForAttempt) renders this verdict
	// non-current by generation mismatch — it is never deleted. The column is
	// owner-bundle DDL (owner bundle 0009): when it is absent (a
	// runtime-migrations-only database such as the default pgtest harness) the
	// INSERT omits it, exactly as db.appendArtifactRowDirect omits placement.
	//
	// RFC 0135 P5 (D216) — this stamp is the sealed expectation barrier
	// primitive's "embed the seal in the contribution" operation
	// (db.BarrierReadySQL with seal := review_generation). The verdict is the
	// staged contribution; review_generation is the seal it is sealed at. The
	// finalization gate then JOINs each required obligation's accepting verdict on
	// the build's LIVE seal (staged.review_generation = live.review_generation), so
	// a verdict stamped below the live seal is structurally absent from the readiness
	// count — the exact trap-killer the primitive guarantees. No P5 behavior change.
	if reviewGenerationEnabled(ctx, runner) {
		generation, gerr := reviewedBuildGeneration(ctx, runner, repositoryID, job)
		if gerr != nil {
			return nil, gerr
		}
		if err := exec.Exec(ctx, `
			INSERT INTO striatumd.verdicts (
			  repository_id, verdict_id, run_id, job_id, session_id, verdict,
			  rationale, findings_artifact_id, created_at, posture,
			  lane_attestation_at_record, review_provenance_override,
			  review_provenance_decision_id, supervisor_id_at_record, review_generation
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
			repositoryID, verdictID, job["run_id"], jobID, sessionID, verdict,
			rationale, findingsArtifactID, now, posture,
			attestation["state"],
			reviewProvenance["review_provenance_override"] == true,
			nullable(reviewProvenance["review_provenance_decision_id"]),
			attestation["supervisor_id"], generation,
		); err != nil {
			return nil, err
		}
	} else if err := exec.Exec(ctx, `
		INSERT INTO striatumd.verdicts (
		  repository_id, verdict_id, run_id, job_id, session_id, verdict,
		  rationale, findings_artifact_id, created_at, posture,
		  lane_attestation_at_record, review_provenance_override,
		  review_provenance_decision_id, supervisor_id_at_record
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		repositoryID, verdictID, job["run_id"], jobID, sessionID, verdict,
		rationale, findingsArtifactID, now, posture,
		attestation["state"],
		reviewProvenance["review_provenance_override"] == true,
		nullable(reviewProvenance["review_provenance_decision_id"]),
		attestation["supervisor_id"],
	); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"verdict":          verdict,
		"posture":          posture,
		"lane_attestation": attestation["state"],
	}
	for key, value := range reviewProvenance {
		payload[key] = value
	}
	if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "verdict.recorded", sessionID, jobID, nil, nil, leaseID, payload); err != nil {
		return nil, err
	}
	// RFC 0135 P4 (#339, D214): forward-write the dissent ledger row CO-TRANSACTIONALLY
	// with a blocking verdict (needs_revision / reject), under the per-run advisory
	// lock this path already holds. The row is keyed on the STABLE workflow_job_id and
	// sealed at the seat's live attempt, so a live dissent blocks the panel quorum
	// wherever recovery later moves the seat's lineage (the seal-durable witness). It
	// no-ops for accepting verdicts and when the table is absent (runtime-only test DB).
	if tx, ok := runner.(db.TxRunner); ok {
		if err := recordDissent(ctx, tx, repositoryID, job, sessionID, verdictID, verdict); err != nil {
			return nil, err
		}
		// RFC 0132 Layer C / P4b (#341): evaluate the advisory guards over the panel's
		// gating-abstention + advisory tally co-transactionally with this verdict. A
		// fired guard (advisory_corroborated_abstention / unanimous_advisory_reject /
		// advisory_only_panel_ungrounded) opens an operator-resolvable BLOCKED blocker on
		// the downstream gate (it never auto-flips the implementer and never silently
		// wedges). A no-op when no advisory guard fires (the silent-accept case).
		if err := applyAdvisoryGuards(ctx, tx, repositoryID, jobID); err != nil {
			return nil, err
		}
	}
	switch verdict {
	case "accept", "accept_with_findings":
		if err := completeReviewJob(ctx, runner, repositoryID, job, sessionID, leaseID, verdict); err != nil {
			return nil, err
		}
		// #65 P1: if this was the last reviewer of an interrogable upstream
		// target, retire the panel-owned interrogation window now that no
		// reviewer remains to interrogate the live target.
		if err := releaseInterrogationTargetForCompletedReview(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), jobID); err != nil {
			return nil, err
		}
		if err := maybeEnqueueDownstream(ctx, runner, repositoryID, jobID); err != nil {
			return nil, err
		}
		if err := maybeCompleteRun(ctx, runner, repositoryID, fmt.Sprint(job["run_id"])); err != nil {
			return nil, err
		}
		return map[string]any{"status": "completed", "job_id": jobID, "verdict": verdict, "verdict_id": verdictID}, nil
	case "needs_revision":
		// If the workflow declares a matching `cycle` (from this review job,
		// on_verdict needs_revision), route back to the cycle target and
		// re-open it for revision instead of stalling on a human checkpoint
		// (RFC 0083 §2 bounded revision cycle). The checkpoint is only opened
		// when there is genuinely no matching cycle, or the cycle's
		// max_iterations budget is exhausted, or the cycle target is absent.
		cycles, err := workflowCyclesForJob(ctx, runner, repositoryID, job)
		if err != nil {
			return nil, err
		}
		cycle, matched := matchRevisionCycle(cycles, verdict)
		if matched {
			routed, fired, err := routeRevisionCycle(ctx, runner, repositoryID, job, sessionID, leaseID, cycle, verdict)
			if err != nil {
				return nil, err
			}
			if fired {
				routed["verdict_id"] = verdictID
				return routed, nil
			}
		}
		// #77: a review job that feeds an adjudicator must not open its own
		// revision checkpoint when it has no matching cycle — the adjudicator
		// owns the revision decision. When this reviewer's only path forward is a
		// downstream phase_synthesis (adjudicator) whose dependency gate accepts
		// this verdict (no requires_verdict, or one that includes it), record the
		// dissent, complete the review, and enqueue the adjudicator to weigh it,
		// instead of stalling the live panel on an operator override.
		if !matched {
			feeds, err := reviewFeedsAbsorbingAdjudicator(ctx, runner, repositoryID, jobID, verdict)
			if err != nil {
				return nil, err
			}
			if feeds {
				if err := completeReviewJob(ctx, runner, repositoryID, job, sessionID, leaseID, "needs_revision absorbed by adjudicator"); err != nil {
					return nil, err
				}
				if err := releaseInterrogationTargetForCompletedReview(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), jobID); err != nil {
					return nil, err
				}
				if err := maybeEnqueueDownstream(ctx, runner, repositoryID, jobID); err != nil {
					return nil, err
				}
				if err := maybeCompleteRun(ctx, runner, repositoryID, fmt.Sprint(job["run_id"])); err != nil {
					return nil, err
				}
				return map[string]any{"status": "completed", "job_id": jobID, "verdict": verdict, "verdict_id": verdictID, "absorbed_by_adjudicator": true}, nil
			}
		}
		description := "needs_revision verdict has no matching workflow cycle. Resolve with `continue` to re-review the revised branch (fix the findings first, or it re-reproduces the verdict and re-opens this checkpoint), or `override --decision-id <id>` to proceed past the verdict. Workflow authors can declare a bounded revision `cycle` so a needs_revision verdict routes back to the implementer automatically instead of parking here."
		if matched {
			description = "needs_revision verdict matched a workflow cycle but its iteration budget is exhausted or its target is unavailable. Resolve with `continue` to re-review the revised branch (fix the findings first), or `override --decision-id <id>` to proceed past the verdict. Workflow authors can raise the revision `cycle` iteration budget for this review job."
		}
		blockerID, err := openHumanCheckpoint(ctx, runner, repositoryID, job, sessionID, leaseID, description)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": "waiting_human", "job_id": jobID, "verdict": verdict, "blocker_id": blockerID, "verdict_id": verdictID}, nil
	case "reject":
		// #140: `reject` fails the review job and the whole run with no
		// recovery path. Supervised reviewers (especially codex) conflate
		// "request changes" with reject and wedge the run non-recoverably. When
		// the workflow declares a bounded revision cycle for this review job, a
		// reject is almost always a misfired change-request: refuse it with a
		// clear correction so the lane self-corrects to the recoverable
		// needs_revision, and keep override-verdict as the explicit force path
		// for a genuine terminal rejection. Workflows with no revision cycle keep
		// the historical terminal-reject behavior.
		rejectCycles, err := workflowCyclesForJob(ctx, runner, repositoryID, job)
		if err != nil {
			return nil, err
		}
		if _, hasRevisionCycle := matchRevisionCycle(rejectCycles, "needs_revision"); hasRevisionCycle {
			return nil, rpc.NewError(
				"invalid_transition",
				"reject is terminal and this workflow declares a bounded revision cycle for this review; submit needs_revision to request changes (recoverable), or use override-verdict to force a terminal rejection",
				map[string]any{"recoverable_verdict": "needs_revision", "submitted_verdict": "reject"},
			)
		}
		if err := failReviewJob(ctx, runner, repositoryID, job, sessionID, leaseID); err != nil {
			return nil, err
		}
		if err := maybeCompleteRun(ctx, runner, repositoryID, fmt.Sprint(job["run_id"])); err != nil {
			return nil, err
		}
		return map[string]any{"status": "failed", "job_id": jobID, "verdict": verdict, "verdict_id": verdictID}, nil
	default:
		return nil, rpc.NewError("invalid_transition", fmt.Sprintf("unknown verdict %q", verdict), nil)
	}
}

func isVerdictCapableJobType(jobType string) bool {
	return jobType == "review" || jobType == "phase_synthesis"
}

func verdictCapableJobLabel(jobType string) string {
	if jobType == "phase_synthesis" {
		return "phase_synthesis"
	}
	return "review"
}

// submitReviewRequiredExpectedArtifacts returns the job's required expected
// artifacts with cycle placeholders resolved against the current attempt, so
// callers compare against the attempt-scoped logical name + path the
// adjudicator was told to publish (matching the work packet and
// verifyRequiredArtifacts).
func submitReviewRequiredExpectedArtifacts(job map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, item := range resolveExpectedArtifactCycles(asList(job["expected_artifacts_json"]), intValue(job["attempt"])) {
		expected := asMap(item)
		if expected["required"] != true {
			continue
		}
		out = append(out, expected)
	}
	return out
}

// inferSubmitReviewArtifactIdentity implements #96 part 1. When the caller did
// not pass an explicit logical_name and/or kind, and the verdict-capable job
// has exactly ONE required expected artifact, infer the missing field(s) from
// that sole expected artifact — most usefully when the submitted path matches
// the expected artifact's path. Inference is only applied when unambiguous
// (exactly one required expected artifact); with zero or several required
// expected artifacts the caller's values pass through unchanged and the
// historical "review"/"finding" defaults still apply.
func inferSubmitReviewArtifactIdentity(
	job map[string]any,
	pathText, logicalName, kind string,
	logicalNameExplicit, kindExplicit bool,
) (string, string) {
	if logicalNameExplicit && kindExplicit {
		return logicalName, kind
	}
	required := submitReviewRequiredExpectedArtifacts(job)
	if len(required) != 1 {
		return logicalName, kind
	}
	sole := required[0]
	expectedLogical, _ := sole["logical_name"].(string)
	expectedKind, _ := sole["kind"].(string)
	if !logicalNameExplicit && expectedLogical != "" {
		logicalName = expectedLogical
	}
	if !kindExplicit && expectedKind != "" {
		kind = expectedKind
	}
	return logicalName, kind
}

func prevalidateSubmitReview(ctx context.Context, runner db.TxRunner, repositoryID string, job map[string]any, sessionID, leaseID, logicalName, kind, pathText, verdict, reviewProvenanceDecisionID string) (map[string]any, error) {
	if !isVerdictCapableJobType(fmt.Sprint(job["job_type"])) {
		return nil, rpc.NewError("invalid_transition", "submit-review is valid only for verdict-capable jobs", nil)
	}
	state := fmt.Sprint(job["state"])
	if state != "claimed" && state != "running" {
		return nil, rpc.NewError("invalid_transition", "verdict-capable job must be claimed or running before submit-review", nil)
	}
	if state == "claimed" && nullable(job["current_message_id"]) == nil {
		return nil, rpc.NewError("invalid_transition", "claimed verdict-capable job is missing its current message", nil)
	}
	if _, err := activeLeaseFor(ctx, runner, repositoryID, leaseID, sessionID, fmt.Sprint(job["job_id"])); err != nil {
		return nil, err
	}
	reviewProvenance, _, err := enforceReviewProvenancePolicy(ctx, runner, repositoryID, job, sessionID, verdict, "publishing review artifacts and recording an accepting verdict", reviewProvenanceDecisionID)
	if err != nil {
		return nil, err
	}
	if reviewProvenance == nil {
		if err := ensureWorkSessionBackend(ctx, runner, repositoryID, sessionID, "submit-review"); err != nil {
			return nil, err
		}
	}
	if err := prevalidateSubmitReviewArtifactVerdict(ctx, runner, repositoryID, job, kind, pathText, verdict); err != nil {
		return nil, err
	}
	// Build finding 1: resolve cycle placeholders against the job attempt so the
	// submit-review pre-check compares against the attempt-scoped logical name +
	// path the adjudicator published to (cycle_<attempt>), matching the work
	// packet and verifyRequiredArtifacts.
	for _, item := range resolveExpectedArtifactCycles(asList(job["expected_artifacts_json"]), intValue(job["attempt"])) {
		expected := asMap(item)
		if expected["required"] != true {
			continue
		}
		if expected["logical_name"] == logicalName && expected["kind"] == kind && expected["path"] == pathText {
			continue
		}
		found, err := existsRow(ctx, runner, `
			SELECT 1 FROM striatumd.artifacts
			 WHERE repository_id = $1 AND job_id = $2 AND logical_name = $3
			   AND artifact_kind = $4 AND repo_path = $5
			 LIMIT 1`,
			repositoryID,
			job["job_id"],
			expected["logical_name"],
			expected["kind"],
			expected["path"],
		)
		if err != nil {
			return nil, err
		}
		if !found {
			// #96 part 2: name BOTH the submitted tuple and the expected tuple so
			// the operator can see exactly what was submitted vs required and pass
			// the right --logical-name/--kind (the submitted identity defaults to
			// "review"/"finding" when not inferred from a sole expected artifact).
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
				"required artifact would still be missing after submit-review: submitted (logical_name=%q, kind=%q, path=%q) does not satisfy expected (logical_name=%q, kind=%q, path=%q); pass --logical-name and --kind matching the expected artifact",
				logicalName,
				kind,
				pathText,
				expected["logical_name"],
				expected["kind"],
				expected["path"],
			), nil)
		}
	}
	return reviewProvenance, nil
}

func prevalidateSubmitReviewArtifactVerdict(ctx context.Context, runner any, repositoryID string, job map[string]any, kind, pathText, submittedVerdict string) error {
	if kind != "collaboration_ledger" {
		return nil
	}
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return err
	}
	repoRoot := fmt.Sprint(run["repo_root"])
	if !pathAllowed(repoRoot, pathText, asMap(job["write_scope_json"])) {
		return rpc.NewError("artifact_error", "artifact path is outside the job write scope", nil)
	}
	// #484: resolve through the job's active worktree (mirrors the worktree-aware
	// publish + fresh-review re-read). review.submit of a worktree-isolated
	// collaboration_ledger job otherwise fails with "artifact file does not exist"
	// because the ledger lives in the per-job worktree, not repoRoot.
	activeWorktree, err := activeWorktreeForJob(ctx, runner, repositoryID, fmt.Sprint(job["job_id"]))
	if err != nil {
		return err
	}
	path, err := artifactSourcePath(repoRoot, pathText, activeWorktree)
	if err != nil {
		return rpc.NewError("artifact_error", err.Error(), nil)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return rpc.NewError("artifact_error", "artifact file does not exist", nil)
	}
	frontMatter, err := artifactcontracts.ParseAndValidateFrontMatter(kind, path, payload)
	if err != nil {
		return rpc.NewError("artifact_error", err.Error(), nil)
	}
	ledgerVerdict := fmt.Sprint(frontMatter["verdict"])
	if ledgerVerdict != submittedVerdict {
		return rpc.NewError("artifact_error", fmt.Sprintf("review.submit verdict %q must match collaboration_ledger front matter verdict %q", submittedVerdict, ledgerVerdict), nil)
	}
	return nil
}

// enforceFreshReviewNotByteIdentical refuses a review.submit whose finding body
// is byte-identical (content_sha256) to a finding THIS job already published at
// a lower attempt (#206 / RFC 0095 Goal 8). A re-opened review job runs a fresh
// round against a revised target; adopting the prior round's REVIEW.md verbatim
// re-asserts a stale verdict and burns the bounded revision budget on phantom
// rejections. The guard is scoped to verdict-capable (review-class) jobs at
// attempt > 1; attempt 1 has no prior round to collide with.
func enforceFreshReviewNotByteIdentical(ctx context.Context, runner any, repositoryID string, job map[string]any, kind, pathText string) error {
	if !isVerdictCapableJobType(fmt.Sprint(job["job_type"])) {
		return nil
	}
	currentAttempt := jobAttemptValue(job["attempt"])
	if currentAttempt <= 1 {
		return nil
	}
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return err
	}
	repoRoot := fmt.Sprint(run["repo_root"])
	if !pathAllowed(repoRoot, pathText, asMap(job["write_scope_json"])) {
		// Path scoping is the publishArtifact contract's job; defer the precise
		// error to it rather than masking it here.
		return nil
	}
	activeWorktree, err := activeWorktreeForJob(ctx, runner, repositoryID, fmt.Sprint(job["job_id"]))
	if err != nil {
		return err
	}
	path, err := artifactSourcePath(repoRoot, pathText, activeWorktree)
	if err != nil {
		// Let publishArtifact surface the path resolution error.
		return nil
	}
	info, statErr := os.Stat(path)
	if statErr != nil || info.IsDir() {
		// The file is missing; publishArtifact reports the precise artifact_error.
		return nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return rpc.NewError("artifact_error", "artifact file does not exist", nil)
	}
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	prior, err := oneRow(ctx, runner, `
		SELECT attempt, artifact_id, repo_path FROM striatumd.artifacts
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND content_sha256 = $4 AND attempt < $5
		 ORDER BY attempt DESC
		 LIMIT 1`, repositoryID, job["run_id"], job["job_id"], digest, currentAttempt)
	if err != nil {
		if errorsIsNoRows(err) {
			return nil
		}
		return err
	}
	priorAttempt := jobAttemptValue(prior["attempt"])
	return rpc.NewError("fresh_review_byte_identical", fmt.Sprintf(
		"review finding at %s is byte-identical (content_sha256=%s) to this job's attempt-%d finding (artifact %v) while the job is at attempt %d. A re-opened review job must evaluate the CURRENT revision; republishing the prior round's finding re-asserts a stale verdict. Delete the stale finding file the prior round left at %s, read the revised target, and write your own finding before resubmitting.",
		pathText, digest, priorAttempt, prior["artifact_id"], currentAttempt, fmt.Sprint(prior["repo_path"]),
	), nil)
}

func ackInline(ctx context.Context, runner any, repositoryID, sessionID, messageID, leaseID string) error {
	message, err := rowByID(ctx, runner, repositoryID, "queue_messages", "message_id", messageID, true)
	if err != nil {
		return err
	}
	job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", fmt.Sprint(message["job_id"]), true)
	if err != nil {
		return err
	}
	if _, err := activeLeaseFor(ctx, runner, repositoryID, leaseID, sessionID, fmt.Sprint(job["job_id"])); err != nil {
		return err
	}
	if fmt.Sprint(message["state"]) == "acked" {
		return nil
	}
	if fmt.Sprint(message["state"]) != "claimed" || fmt.Sprint(job["state"]) != "claimed" {
		return rpc.NewError("invalid_transition", "work must be claimed before ack", nil)
	}
	now := nowString()
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.queue_messages
		   SET state = 'acked', acked_at = $1, updated_at = $2
		 WHERE repository_id = $3 AND message_id = $4`, now, now, repositoryID, messageID); err != nil {
		return err
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'running', started_at = $1
		 WHERE repository_id = $2 AND job_id = $3`, now, repositoryID, job["job_id"]); err != nil {
		return err
	}
	_, err = appendEvent(ctx, runner, repositoryID, job["run_id"], "queue.acked", sessionID, job["job_id"], messageID, nil, leaseID, nil)
	return err
}

func completeReviewJob(ctx context.Context, runner any, repositoryID string, job map[string]any, sessionID, leaseID, summary string) error {
	now := nowString()
	messageID := nullable(job["current_message_id"])
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	// #551: a repo-write review / phase_synthesis job (e.g. a falsification_gate
	// adjudicate that published a collaboration_ledger via artifact.publish, or a
	// verification_gate adjudicate) must have its git_publication artifacts
	// porter-committed AND anchored to the run branch on completion — exactly as
	// the work.complete path does (lifecycle.go). Otherwise the published body is
	// registered in the DB (the verdict reads it, the gate clears) but left
	// untracked in the per-job worktree, so the downstream gated job forks a run
	// branch that lacks it and is blind to the ledger. Both helpers no-op for a
	// non-repo-write / no-worktree-required job (a review_only reviewer publishing
	// a blob_exhaust finding is unaffected), so this is safe for every review job.
	if err := ensurePublishedArtifactsDurableWithPorter(ctx, runner, repositoryID, job, "review.verdict"); err != nil {
		return err
	}
	anchorPayload, err := anchorActiveWorktreeForJob(ctx, runner, repositoryID, job)
	if err != nil {
		return err
	}
	if anchorPayload != nil {
		if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "job.commits_anchored", sessionID, job["job_id"], messageID, nil, leaseID, anchorPayload); err != nil {
			return err
		}
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'completed', completed_at = $1, current_lease_id = NULL
		 WHERE repository_id = $2 AND job_id = $3`, now, repositoryID, job["job_id"]); err != nil {
		return err
	}
	if messageID != nil {
		if err := exec.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'completed', completed_at = $1, updated_at = $2,
			       current_lease_id = NULL
			 WHERE repository_id = $3 AND message_id = $4`, now, now, repositoryID, messageID); err != nil {
			return err
		}
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'released', released_at = $1, release_reason = 'verdict'
		 WHERE repository_id = $2 AND lease_id = $3`, now, repositoryID, leaseID); err != nil {
		return err
	}
	if _, err = appendEvent(ctx, runner, repositoryID, job["run_id"], "job.completed", sessionID, job["job_id"], messageID, nil, leaseID, map[string]any{"summary": summary}); err != nil {
		return err
	}
	return markJobTerminal(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), fmt.Sprint(job["job_id"]))
}

func failReviewJob(ctx context.Context, runner any, repositoryID string, job map[string]any, sessionID, leaseID string) error {
	now := nowString()
	messageID := nullable(job["current_message_id"])
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'failed', completed_at = $1, current_lease_id = NULL
		 WHERE repository_id = $2 AND job_id = $3`, now, repositoryID, job["job_id"]); err != nil {
		return err
	}
	if messageID != nil {
		if err := exec.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'completed', completed_at = $1, updated_at = $2,
			       current_lease_id = NULL
			 WHERE repository_id = $3 AND message_id = $4`, now, now, repositoryID, messageID); err != nil {
			return err
		}
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'released', released_at = $1, release_reason = 'reject'
		 WHERE repository_id = $2 AND lease_id = $3`, now, repositoryID, leaseID); err != nil {
		return err
	}
	_, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "job.failed", sessionID, job["job_id"], messageID, nil, leaseID, map[string]any{"reason": "reject"})
	if err != nil {
		return err
	}
	return markJobTerminal(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), fmt.Sprint(job["job_id"]))
}

func openHumanCheckpoint(ctx context.Context, runner any, repositoryID string, job map[string]any, sessionID, leaseID, description string) (string, error) {
	blockerID, err := newID("blk")
	if err != nil {
		return "", err
	}
	now := nowString()
	messageID := nullable(job["current_message_id"])
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return "", fmt.Errorf("runner does not support exec")
	}
	if err := exec.Exec(ctx, `
		INSERT INTO striatumd.blockers (
		  repository_id, blocker_id, run_id, job_id, session_id, severity,
		  blocker_kind, description, state, created_at
		)
		VALUES ($1,$2,$3,$4,$5,'human_checkpoint','revision_routing',$6,'open',$7)`,
		repositoryID, blockerID, job["run_id"], job["job_id"], sessionID, description, now); err != nil {
		return "", err
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'waiting_human', current_lease_id = NULL
		 WHERE repository_id = $1 AND job_id = $2`, repositoryID, job["job_id"]); err != nil {
		return "", err
	}
	if messageID != nil {
		if err := exec.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'blocked', current_lease_id = NULL, updated_at = $1
			 WHERE repository_id = $2 AND message_id = $3`, now, repositoryID, messageID); err != nil {
			return "", err
		}
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'released', released_at = $1, release_reason = 'needs_revision'
		 WHERE repository_id = $2 AND lease_id = $3`, now, repositoryID, leaseID); err != nil {
		return "", err
	}
	if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "human_checkpoint.opened", sessionID, job["job_id"], nil, nil, leaseID, map[string]any{
		"blocker_id":  blockerID,
		"description": description,
	}); err != nil {
		return "", err
	}
	if err := markJobTerminal(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), fmt.Sprint(job["job_id"])); err != nil {
		return "", err
	}
	return blockerID, nil
}

func resolveReviewPosture(ctx context.Context, runner any, repositoryID string, job map[string]any) (string, error) {
	if fmt.Sprint(job["job_type"]) == "phase_synthesis" {
		return "neutral", nil
	}
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return "", err
	}
	snapshot, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(run["workflow_snapshot_id"]), false)
	if err != nil {
		return "", err
	}
	for _, item := range asList(asMap(snapshot["workflow_json"])["jobs"]) {
		def := asMap(item)
		if def["id"] == job["workflow_job_id"] && def["type"] == "review" {
			if posture, ok := def["review_posture"].(string); ok && posture != "" {
				return posture, nil
			}
			return "neutral", nil
		}
	}
	return "neutral", nil
}

func enforceRequiredAttestationForArtifactPublish(ctx context.Context, runner any, repositoryID string, job map[string]any, sessionID string) error {
	return enforceRequiredAttestationForArtifactPublishWithOverride(ctx, runner, repositoryID, job, sessionID, false)
}

func enforceRequiredAttestationForArtifactPublishWithOverride(ctx context.Context, runner any, repositoryID string, job map[string]any, sessionID string, reviewProvenanceOverride bool) error {
	def, err := workflowJobDefinitionForRow(ctx, runner, repositoryID, job)
	if err != nil {
		return err
	}
	if def["require_attested_lane"] != true {
		return nil
	}
	attestation := sessionLaneAttestation(ctx, runner, repositoryID, sessionID)
	if attestation["attested"] == true || reviewProvenanceOverride {
		return nil
	}
	return reviewAttestationError(sessionID, "publishing review artifacts", attestation, false)
}

// enforceReviewProvenancePolicy returns the override evidence (nil when no
// override is in play) AND the lane-attestation map it probed to decide. The
// caller threads that attestation map down to applyVerdict so the frozen
// verdict stamp records the SAME probe value the gate decided on — a second
// probe could disagree with the gate if the lane flips in between (RFC 0118
// P0-1 TOCTOU).
func enforceReviewProvenancePolicy(ctx context.Context, runner any, repositoryID string, job map[string]any, sessionID, verdict, action, decisionID string) (map[string]any, map[string]any, error) {
	def, err := workflowJobDefinitionForRow(ctx, runner, repositoryID, job)
	if err != nil {
		return nil, nil, err
	}
	attestation := sessionLaneAttestation(ctx, runner, repositoryID, sessionID)
	if attestation["attested"] == true {
		return nil, attestation, nil
	}
	requiresAttestedLane := def["require_attested_lane"] == true
	requiresOverride := requiresAttestedLane || (isAcceptingVerdict(verdict) && reviewRequiresIndependentProvenance(job, def))
	if !requiresOverride {
		return nil, attestation, nil
	}
	if strings.TrimSpace(decisionID) == "" {
		if requiresAttestedLane {
			return nil, attestation, reviewAttestationError(sessionID, action, attestation, true)
		}
		return nil, attestation, rpc.NewError(
			"review_provenance_override_required",
			"unattested/operator-authored accepting verdict for a fresh review job requires --review-provenance-decision-id referencing an accepting run-level decision with escape_surface=review_provenance",
			map[string]any{
				"job_id":       job["job_id"],
				"workflow_job": job["workflow_job_id"],
				"session_id":   sessionID,
			},
		)
	}
	evidence, err := verifyReviewProvenanceDecision(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), decisionID)
	if err != nil {
		return nil, attestation, err
	}
	return evidence, attestation, nil
}

func workflowJobDefinitionForRow(ctx context.Context, runner any, repositoryID string, job map[string]any) (map[string]any, error) {
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return nil, err
	}
	snapshot, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(run["workflow_snapshot_id"]), false)
	if err != nil {
		return nil, err
	}
	for _, item := range asList(asMap(snapshot["workflow_json"])["jobs"]) {
		def := asMap(item)
		if def["id"] != job["workflow_job_id"] {
			continue
		}
		return def, nil
	}
	return map[string]any{}, nil
}

func reviewAttestationError(sessionID, action string, attestation map[string]any, includeDecisionPath bool) error {
	reason := ""
	if attestation["reason"] != nil {
		reason = fmt.Sprintf(" (%v)", attestation["reason"])
	}
	message := "review job requires an attached lane supervisor before " + action + reason + "; recovery: striatum supervise start --session-id " + sessionID
	if includeDecisionPath {
		message += " or record an accepting review_provenance decision and pass --review-provenance-decision-id"
	}
	return rpc.NewError("invalid_transition", message, nil)
}

func reviewRequiresIndependentProvenance(job, def map[string]any) bool {
	if fmt.Sprint(job["job_type"]) != "review" {
		return false
	}
	if defType := fmt.Sprint(def["type"]); defType != "" && defType != "review" {
		return false
	}
	return boolValue(job["fresh_session_required"]) || def["fresh_session_required"] == true || def["reviewer_context_policy"] == "fresh"
}

func isAcceptingVerdict(verdict string) bool {
	return verdict == "accept" || verdict == "accept_with_findings"
}

func reviewProvenanceDecisionParam(envelope rpc.Envelope) string {
	if value := strings.TrimSpace(stringParam(envelope, "review_provenance_decision_id")); value != "" {
		return value
	}
	return strings.TrimSpace(stringParam(envelope, "provenance_decision_id"))
}

func verifyReviewProvenanceDecision(ctx context.Context, runner any, repositoryID, runID, decisionID string) (map[string]any, error) {
	artifact, err := oneRow(ctx, runner, `
		SELECT artifact_id, job_id, session_id
		  FROM striatumd.artifacts
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND artifact_kind = 'decision'
		   AND logical_name = $3
		 LIMIT 1`, repositoryID, runID, decisionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rpc.NewError("invalid_transition", "review provenance decision was not found for this run", map[string]any{"decision_id": decisionID})
		}
		return nil, err
	}
	if nullable(artifact["job_id"]) != nil || nullable(artifact["session_id"]) != nil {
		return nil, rpc.NewError("invalid_transition", "review provenance decision must be a run-level decision artifact", map[string]any{"decision_id": decisionID})
	}
	event, err := oneRow(ctx, runner, `
		SELECT payload_json
		  FROM striatumd.events
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND event_type = 'decision.recorded'
		   AND payload_json->>'decision_id' = $3
		 ORDER BY event_id DESC
		 LIMIT 1`, repositoryID, runID, decisionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rpc.NewError("invalid_transition", "review provenance decision is missing its decision.recorded event", map[string]any{"decision_id": decisionID})
		}
		return nil, err
	}
	payload := asMap(event["payload_json"])
	outcome := fmt.Sprint(payload["outcome"])
	if outcome != "accepted" && outcome != "accepted_with_follow_up" {
		return nil, rpc.NewError("invalid_transition", fmt.Sprintf("review provenance decision must have an accepting outcome (got %q)", outcome), map[string]any{"decision_id": decisionID})
	}
	if fmt.Sprint(payload["escape_surface"]) != "review_provenance" {
		return nil, rpc.NewError("invalid_transition", "review provenance decision must declare escape_surface=review_provenance", map[string]any{"decision_id": decisionID})
	}
	return map[string]any{
		"review_provenance_override":             true,
		"review_provenance_decision_id":          decisionID,
		"review_provenance_decision_artifact_id": artifact["artifact_id"],
	}, nil
}

// reviewFeedsAbsorbingAdjudicator reports whether every downstream consumer of
// this review job absorbs its verdict (#77) — i.e. the dependency-edge gate does
// not require a clearing verdict from this reviewer (empty `requires_verdict`, or
// one that includes the verdict). run.prepare leaves edges INTO an adjudicator
// (a phase_synthesis job) ungated for exactly this reason, while it gates every
// other review→downstream edge with `requires_verdict:[accept,
// accept_with_findings]`. So in a prepared workflow a tolerant downstream IS an
// adjudicator: the reviewer's needs_revision is a finding for the adjudicator to
// weigh, not a trigger for the reviewer's own revision checkpoint. A reviewer
// with no downstream, or any downstream that hard-gates on a clearing verdict,
// is not absorbed (it still opens a checkpoint).
func reviewFeedsAbsorbingAdjudicator(ctx context.Context, runner any, repositoryID, jobID, verdict string) (bool, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT gate_json
		  FROM striatumd.job_dependencies
		 WHERE repository_id = $1 AND depends_on_job_id = $2`, repositoryID, jobID)
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	for _, row := range rows {
		required := requiredVerdicts(asMap(row["gate_json"])["requires_verdict"])
		if len(required) > 0 && !required[verdict] {
			return false, nil
		}
	}
	return true, nil
}

func downstreamJobs(ctx context.Context, runner any, repositoryID, jobID string) ([]map[string]any, error) {
	return queryRows(ctx, runner, `
		SELECT j.job_id, j.workflow_job_id, j.state
		  FROM striatumd.job_dependencies dep
		  JOIN striatumd.jobs j
		    ON j.repository_id = dep.repository_id
		   AND j.job_id = dep.job_id
		 WHERE dep.repository_id = $1 AND dep.depends_on_job_id = $2
		 ORDER BY j.created_at, j.job_id`, repositoryID, jobID)
}
