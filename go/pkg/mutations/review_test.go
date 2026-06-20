package mutations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// #132: review.submit/verdict accepts reviewer-natural synonyms and maps them
// to the canonical vocabulary; unknown tokens pass through unchanged.
func TestNormalizeVerdictSynonyms(t *testing.T) {
	cases := map[string]string{
		"accept":                  "accept",
		"approve":                 "accept",
		"Approved":                "accept",
		"accept_with_findings":    "accept_with_findings",
		"accepted_with_follow_up": "accept_with_findings",
		"accept_with_followup":    "accept_with_findings",
		"needs_revision":          "needs_revision",
		"request_changes":         "needs_revision",
		"changes_requested":       "needs_revision",
		" Revise ":                "needs_revision",
		"reject":                  "reject",
		"rejected":                "reject",
		"something_unrecognized":  "something_unrecognized",
	}
	for in, want := range cases {
		if got := normalizeVerdict(in); got != want {
			t.Errorf("normalizeVerdict(%q) = %q, want %q", in, got, want)
		}
	}
}

// #140: a `reject` verdict on a review job whose workflow declares a bounded
// revision cycle is refused (recoverable steer to needs_revision), not recorded
// as a non-recoverable run failure. The review job stays running so the lane can
// re-submit needs_revision.
func TestRejectWithRevisionCycleRefused(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_reject_cycle"
	cycles := []any{
		map[string]any{
			"from":            "review_design_codex",
			"to":              "synth",
			"on_verdict":      "needs_revision",
			"max_iterations":  float64(2),
			"allow_same_lane": true,
		},
	}
	_, reviewJobID, _, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, cycles)

	_, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     reviewJobID,
		"lease_id":   leaseID,
		"verdict":    "reject",
	}))
	if err == nil {
		t.Fatalf("expected reject to be refused when a revision cycle is declared")
	}
	if !strings.Contains(err.Error(), "needs_revision") {
		t.Fatalf("refusal should steer to needs_revision; got %v", err)
	}
	if got := jobState(t, ctx, runner, repoID, reviewJobID); got != "running" {
		t.Fatalf("review job state = %q, want running (reject refused, not failed)", got)
	}
}

// #140: with NO revision cycle declared, `reject` keeps its historical terminal
// behavior (fails the review job).
func TestRejectWithoutCycleStillFails(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_reject_no_cycle"
	_, reviewJobID, _, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, []any{})

	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     reviewJobID,
		"lease_id":   leaseID,
		"verdict":    "reject",
	}))
	if err != nil {
		t.Fatalf("record reject verdict: %v", err)
	}
	if fmt.Sprint(result["status"]) != "failed" {
		t.Fatalf("status = %v, want failed", result["status"])
	}
}

// #127: after a verdict completes a review job, a follow-up work.complete is an
// idempotent no-op (already_completed), not a misleading "lease is not active".
func TestCompleteAfterVerdictIsIdempotent(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_complete_after_verdict"
	_, reviewJobID, _, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, []any{})

	if _, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     reviewJobID,
		"lease_id":   leaseID,
		"verdict":    "accept",
	})); err != nil {
		t.Fatalf("record accept verdict: %v", err)
	}
	if got := jobState(t, ctx, runner, repoID, reviewJobID); got != "completed" {
		t.Fatalf("review job state = %q, want completed after accept", got)
	}

	result, err := HandleCompleteWork(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     reviewJobID,
		"lease_id":   leaseID,
		"summary":    "lane followed the generic complete instruction",
	}))
	if err != nil {
		t.Fatalf("complete after verdict should not error; got %v", err)
	}
	if fmt.Sprint(result["status"]) != "already_completed" {
		t.Fatalf("status = %v, want already_completed", result["status"])
	}
}

func TestOverrideVerdictRecoversCompletedReviewWithoutPriorVerdict(t *testing.T) {
	tx := newReviewOverrideFakeTx()
	tx.reviewState = "completed"
	runner := &reviewOverrideFakeRunner{tx: tx}

	result, err := HandleOverrideVerdict(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_override_recover",
		Method:        "review.override",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_override",
			"job_id":        "job_review",
			"verdict":       "accept",
			"rationale":     "published artifact was reviewed manually",
		},
	})
	if err != nil {
		t.Fatalf("HandleOverrideVerdict: %v", err)
	}
	if result["status"] != "recovered" {
		t.Fatalf("status = %#v, want recovered", result["status"])
	}
	if result["previous_verdict"] != nil {
		t.Fatalf("previous_verdict = %#v, want nil", result["previous_verdict"])
	}
	if tx.latestVerdict != "accept" {
		t.Fatalf("latest verdict = %q, want accept", tx.latestVerdict)
	}
	if tx.downstreamState != "queued" {
		t.Fatalf("downstream state = %q, want queued", tx.downstreamState)
	}
	if !tx.sawEvent("verdict.overridden") {
		t.Fatalf("verdict.overridden event not appended: %#v", tx.events)
	}
	if !tx.sawEvent("queue.message_enqueued") {
		t.Fatalf("downstream enqueue event not appended: %#v", tx.events)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction commit/rollback = %v/%v", tx.committed, tx.rolledBack)
	}
}

func TestOverrideVerdictCompletesWaitingHumanAndEnqueuesDownstream(t *testing.T) {
	tx := newReviewOverrideFakeTx()
	tx.reviewState = "waiting_human"
	tx.previousVerdict = "needs_revision"
	runner := &reviewOverrideFakeRunner{tx: tx}

	result, err := HandleOverrideVerdict(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_override_waiting",
		Method:        "review.override",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_override",
			"job_id":        "job_review",
			"verdict":       "accept_with_findings",
			"rationale":     "operator accepted the revision path",
		},
	})
	if err != nil {
		t.Fatalf("HandleOverrideVerdict: %v", err)
	}
	if result["status"] != "overridden" {
		t.Fatalf("status = %#v, want overridden", result["status"])
	}
	if result["previous_verdict"] != "needs_revision" {
		t.Fatalf("previous_verdict = %#v", result["previous_verdict"])
	}
	if result["resolved_blockers"] != 1 {
		t.Fatalf("resolved_blockers = %#v, want 1", result["resolved_blockers"])
	}
	if tx.reviewState != "completed" {
		t.Fatalf("review state = %q, want completed", tx.reviewState)
	}
	if tx.downstreamState != "queued" {
		t.Fatalf("downstream state = %q, want queued", tx.downstreamState)
	}
	if !tx.sawEvent("job.completed") {
		t.Fatalf("job.completed event not appended for waiting_human override: %#v", tx.events)
	}
	if !tx.sawEvent("queue.message_enqueued") {
		t.Fatalf("downstream enqueue event not appended: %#v", tx.events)
	}
}

func TestOverrideVerdictAlreadyAcceptingDoesNotMutate(t *testing.T) {
	tx := newReviewOverrideFakeTx()
	tx.reviewState = "completed"
	tx.previousVerdict = "accept"
	runner := &reviewOverrideFakeRunner{tx: tx}

	result, err := HandleOverrideVerdict(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_override_already",
		Method:        "review.override",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_override",
			"job_id":        "job_review",
			"verdict":       "accept",
			"rationale":     "no change needed",
		},
	})
	if err != nil {
		t.Fatalf("HandleOverrideVerdict: %v", err)
	}
	if result["status"] != "already_accepting" {
		t.Fatalf("status = %#v, want already_accepting", result["status"])
	}
	if tx.insertedVerdicts != 0 {
		t.Fatalf("inserted verdicts = %d, want 0", tx.insertedVerdicts)
	}
	if tx.downstreamState != "blocked" {
		t.Fatalf("downstream state = %q, want blocked", tx.downstreamState)
	}
	if tx.sawEvent("queue.message_enqueued") {
		t.Fatalf("unexpected downstream enqueue event: %#v", tx.events)
	}
}

func TestSubmitReviewRejectsUnattestedFreshReviewWithoutProvenanceDecision(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedUnattestedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("accept"))
	markReviewJobFresh(t, ctx, runner, repoID, jobID)

	_, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"path":       "artifacts/review/FINDING.md",
		"verdict":    "accept",
		"rationale":  "operator-authored final review recovery",
	}))
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "review_provenance_override_required" {
		t.Fatalf("submit-review error = %v, want review_provenance_override_required", err)
	}
	if got := jobState(t, ctx, runner, repoID, jobID); got != "running" {
		t.Fatalf("review job state = %q, want running", got)
	}
	runID := "run_" + repoID
	run, err := oneRow(ctx, runner, `
		SELECT state FROM striatumd.runs
		 WHERE repository_id = $1 AND run_id = $2`, repoID, runID)
	if err != nil {
		t.Fatalf("read run: %v", err)
	}
	if run["state"] != "running" {
		t.Fatalf("run state = %v, want running", run["state"])
	}
	if n := countVerdicts(t, ctx, runner, repoID, jobID); n != 0 {
		t.Fatalf("verdict count = %d, want 0", n)
	}
}

func TestSubmitReviewExpiresUnattestedSessionWithoutProvenanceOverride(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedUnattestedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("reject"))

	_, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"path":       "artifacts/review/FINDING.md",
		"verdict":    "reject",
		"rationale":  "operator-authored rejected review without lane backend",
	}))
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "invalid_transition" {
		t.Fatalf("submit-review error = %v, want invalid_transition", err)
	}
	if got := intgSessionState(t, ctx, runner, repoID, sessionID); got != "expired" {
		t.Fatalf("session state = %q, want expired", got)
	}
	if got := jobState(t, ctx, runner, repoID, jobID); got != "running" {
		t.Fatalf("review job state = %q, want running", got)
	}
	if n := activeLeaseCount(t, ctx, runner, repoID, jobID); n != 1 {
		t.Fatalf("active lease count = %d, want 1", n)
	}
	if n := countVerdicts(t, ctx, runner, repoID, jobID); n != 0 {
		t.Fatalf("verdict count = %d, want 0", n)
	}
	if n := countArtifactsForJob(t, ctx, runner, repoID, jobID); n != 0 {
		t.Fatalf("artifact count = %d, want 0", n)
	}
}

func TestRecordVerdictExpiresUnattestedSessionWithoutProvenanceOverride(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedUnattestedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("reject"))

	_, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"verdict":    "reject",
		"rationale":  "operator-authored rejected verdict without lane backend",
	}))
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "invalid_transition" {
		t.Fatalf("record verdict error = %v, want invalid_transition", err)
	}
	if got := intgSessionState(t, ctx, runner, repoID, sessionID); got != "expired" {
		t.Fatalf("session state = %q, want expired", got)
	}
	if got := jobState(t, ctx, runner, repoID, jobID); got != "running" {
		t.Fatalf("review job state = %q, want running", got)
	}
	if n := activeLeaseCount(t, ctx, runner, repoID, jobID); n != 1 {
		t.Fatalf("active lease count = %d, want 1", n)
	}
	if n := countVerdicts(t, ctx, runner, repoID, jobID); n != 0 {
		t.Fatalf("verdict count = %d, want 0", n)
	}
}

func TestSubmitReviewAllowsUnattestedFreshReviewWithProvenanceDecision(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedUnattestedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("accept"))
	markReviewJobFresh(t, ctx, runner, repoID, jobID)
	runID := "run_" + repoID
	decisionID := "dec_review_provenance_" + strings.ReplaceAll(t.Name(), "/", "_")
	decisionArtifactID := seedReviewProvenanceDecision(t, ctx, runner, repoID, runID, decisionID)

	result, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":                    sessionID,
		"job_id":                        jobID,
		"lease_id":                      leaseID,
		"path":                          "artifacts/review/FINDING.md",
		"verdict":                       "accept",
		"rationale":                     "human accepted operator-authored review recovery risk",
		"review_provenance_decision_id": decisionID,
	}))
	if err != nil {
		t.Fatalf("submit-review with provenance decision: %v", err)
	}
	if result["job_state"] != "completed" || result["run_state"] != "completed" {
		t.Fatalf("result states = job:%v run:%v, want completed/completed", result["job_state"], result["run_state"])
	}
	event, err := oneRow(ctx, runner, `
		SELECT payload_json
		  FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND event_type = 'verdict.recorded'
		 ORDER BY event_id DESC
		 LIMIT 1`, repoID, runID)
	if err != nil {
		t.Fatalf("read verdict event: %v", err)
	}
	payload := asMap(event["payload_json"])
	if payload["review_provenance_override"] != true ||
		payload["review_provenance_decision_id"] != decisionID ||
		payload["review_provenance_decision_artifact_id"] != decisionArtifactID {
		t.Fatalf("verdict event payload = %#v, want review provenance evidence", payload)
	}
}

func TestSubmitReviewAllowsRequireAttestedLaneWithProvenanceDecision(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedUnattestedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("accept"))
	runID := "run_" + repoID
	setReviewWorkflowJobPolicy(t, ctx, runner, repoID, runID, map[string]any{"require_attested_lane": true})
	decisionID := "dec_review_attested_lane_" + strings.ReplaceAll(t.Name(), "/", "_")
	seedReviewProvenanceDecision(t, ctx, runner, repoID, runID, decisionID)

	result, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":                    sessionID,
		"job_id":                        jobID,
		"lease_id":                      leaseID,
		"path":                          "artifacts/review/FINDING.md",
		"verdict":                       "accept",
		"rationale":                     "human accepted unattested required-lane review recovery",
		"review_provenance_decision_id": decisionID,
	}))
	if err != nil {
		t.Fatalf("submit-review with required-lane provenance decision: %v", err)
	}
	if result["job_state"] != "completed" {
		t.Fatalf("job_state = %v, want completed", result["job_state"])
	}
	artifact := asMap(result["artifact"])
	if artifact["status"] != "published" {
		t.Fatalf("artifact result = %#v, want published", artifact)
	}
}

func TestRecordVerdictAllowsUnattestedFreshReviewWithProvenanceDecision(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedUnattestedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("accept"))
	published, err := HandlePublishArtifact(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":   sessionID,
		"job_id":       jobID,
		"lease_id":     leaseID,
		"kind":         "finding",
		"logical_name": "review",
		"path":         "artifacts/review/FINDING.md",
	}))
	if err != nil {
		t.Fatalf("publish finding: %v", err)
	}
	markReviewJobFresh(t, ctx, runner, repoID, jobID)
	runID := "run_" + repoID
	decisionID := "dec_review_verdict_" + strings.ReplaceAll(t.Name(), "/", "_")
	seedReviewProvenanceDecision(t, ctx, runner, repoID, runID, decisionID)

	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":                    sessionID,
		"job_id":                        jobID,
		"lease_id":                      leaseID,
		"verdict":                       "accept",
		"findings_artifact_id":          published["artifact_id"],
		"rationale":                     "human accepted already-published operator-authored review recovery",
		"review_provenance_decision_id": decisionID,
	}))
	if err != nil {
		t.Fatalf("record verdict with provenance decision: %v", err)
	}
	if result["status"] != "completed" {
		t.Fatalf("status = %v, want completed", result["status"])
	}
}

func TestSubmitReviewRejectsCollaborationLedgerVerdictMismatch(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedCollaborationLedgerSubmitFixture(t, ctx, runner, "needs_revision")

	_, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":   sessionID,
		"job_id":       jobID,
		"lease_id":     leaseID,
		"path":         "artifacts/gate/LEDGER.md",
		"kind":         "collaboration_ledger",
		"logical_name": "collaboration_ledger",
		"verdict":      "accept",
	}))
	if err == nil || !strings.Contains(err.Error(), "must match collaboration_ledger front matter verdict") {
		t.Fatalf("error = %v", err)
	}
}

func TestSubmitReviewAcceptsMatchingCollaborationLedgerVerdict(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedCollaborationLedgerSubmitFixture(t, ctx, runner, "accept")

	result, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":   sessionID,
		"job_id":       jobID,
		"lease_id":     leaseID,
		"path":         "artifacts/gate/LEDGER.md",
		"kind":         "collaboration_ledger",
		"logical_name": "collaboration_ledger",
		"verdict":      "accept",
	}))
	if err != nil {
		t.Fatalf("submit review: %v", err)
	}
	if result["job_state"] != "completed" {
		t.Fatalf("job_state = %#v; result=%#v", result["job_state"], result)
	}
}

func TestPublishArtifactRejectsACEProductiveRefusal(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedCollaborationLedgerSubmitFixtureWithPayload(t, ctx, runner, aceCollaborationLedgerSubmitPayload("needs_revision", "constraints: []\n", ""))

	_, err := HandlePublishArtifact(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":   sessionID,
		"job_id":       jobID,
		"lease_id":     leaseID,
		"path":         "artifacts/gate/LEDGER.md",
		"kind":         "collaboration_ledger",
		"logical_name": "collaboration_ledger",
	}))
	if err == nil || !strings.Contains(err.Error(), "needs_revision requires a non-empty constraints[]") {
		t.Fatalf("error = %v", err)
	}
}

func TestSubmitReviewRejectsACEProductiveRefusal(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedCollaborationLedgerSubmitFixtureWithPayload(t, ctx, runner, aceCollaborationLedgerSubmitPayload("needs_revision", "constraints: []\n", ""))

	_, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":   sessionID,
		"job_id":       jobID,
		"lease_id":     leaseID,
		"path":         "artifacts/gate/LEDGER.md",
		"kind":         "collaboration_ledger",
		"logical_name": "collaboration_ledger",
		"verdict":      "needs_revision",
	}))
	if err == nil || !strings.Contains(err.Error(), "needs_revision requires a non-empty constraints[]") {
		t.Fatalf("error = %v", err)
	}
}

func TestSubmitReviewIdempotentPathRejectsACEProductiveRefusal(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedCollaborationLedgerSubmitFixtureWithPayload(t, ctx, runner, aceCollaborationLedgerSubmitPayload("needs_revision", "constraints: []\n", ""))
	seedPublishedCollaborationLedgerArtifact(t, ctx, runner, repoID, sessionID, jobID)

	_, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":   sessionID,
		"job_id":       jobID,
		"lease_id":     leaseID,
		"path":         "artifacts/gate/LEDGER.md",
		"kind":         "collaboration_ledger",
		"logical_name": "collaboration_ledger",
		"verdict":      "needs_revision",
	}))
	if err == nil || !strings.Contains(err.Error(), "needs_revision requires a non-empty constraints[]") {
		t.Fatalf("error = %v", err)
	}
}

func TestRecordVerdictRejectsACEProductiveRefusal(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedCollaborationLedgerSubmitFixtureWithPayload(t, ctx, runner, aceCollaborationLedgerSubmitPayload("needs_revision", "constraints: []\n", ""))
	seedPublishedCollaborationLedgerArtifact(t, ctx, runner, repoID, sessionID, jobID)

	_, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"verdict":    "needs_revision",
	}))
	if err == nil || !strings.Contains(err.Error(), "needs_revision requires a non-empty constraints[]") {
		t.Fatalf("error = %v", err)
	}
}

func TestSubmitReviewAcceptsACEProductiveRefusalWithBindingConstraint(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedCollaborationLedgerSubmitFixtureWithPayload(t, ctx, runner, aceCollaborationLedgerSubmitPayload("needs_revision", aceSubmitBindingConstraint(), aceSubmitFinding()))

	result, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":   sessionID,
		"job_id":       jobID,
		"lease_id":     leaseID,
		"path":         "artifacts/gate/LEDGER.md",
		"kind":         "collaboration_ledger",
		"logical_name": "collaboration_ledger",
		"verdict":      "needs_revision",
	}))
	if err != nil {
		t.Fatalf("submit review: %v", err)
	}
	if result["job_state"] != "waiting_human" {
		t.Fatalf("job_state = %#v; result=%#v", result["job_state"], result)
	}
}

// TestRecordVerdictRejectsCollaborationLedgerVerdictMismatch covers build
// finding 2: the primitive path (publish_artifact then review.verdict /
// recordVerdict) must enforce the same collaboration_ledger verdict
// consistency the submit path does. The adjudicator here published a
// `verdict: needs_revision` ledger and tries to record `accept` to clear the
// gate; the daemon must refuse.
func TestRecordVerdictRejectsCollaborationLedgerVerdictMismatch(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedCollaborationLedgerSubmitFixture(t, ctx, runner, "needs_revision")
	seedPublishedCollaborationLedgerArtifact(t, ctx, runner, repoID, sessionID, jobID)

	_, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"verdict":    "accept",
	}))
	if err == nil || !strings.Contains(err.Error(), "must match collaboration_ledger front matter verdict") {
		t.Fatalf("error = %v", err)
	}
}

// TestRecordVerdictAcceptsMatchingCollaborationLedgerVerdict is the green
// counterpart: a matching verdict on the primitive path is accepted and
// completes the gate job.
func TestRecordVerdictAcceptsMatchingCollaborationLedgerVerdict(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedCollaborationLedgerSubmitFixture(t, ctx, runner, "accept")
	seedPublishedCollaborationLedgerArtifact(t, ctx, runner, repoID, sessionID, jobID)

	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"verdict":    "accept",
	}))
	if err != nil {
		t.Fatalf("record verdict: %v", err)
	}
	if result["status"] != "completed" {
		t.Fatalf("status = %#v; result=%#v", result["status"], result)
	}
}

// TestRecordVerdictReadsCollaborationLedgerFromActiveWorktree is the #484
// regression: a worktree-isolated adjudicate job publishes its collaboration
// ledger into the per-job worktree, not repo_root (which is checked out on the
// default branch). The verdict-match re-read must resolve the ledger through the
// active worktree (artifactSourcePath), mirroring the worktree-aware publish
// path. Before the fix, enforceCollaborationLedgerVerdict read bare
// repoRelativePath(repo_root, ...) and failed with "collaboration_ledger
// artifact file does not exist", so the verdict could never be recorded and the
// falsification_gate parked at the adjudicate gate.
func TestRecordVerdictReadsCollaborationLedgerFromActiveWorktree(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedCollaborationLedgerSubmitFixture(t, ctx, runner, "accept")
	seedPublishedCollaborationLedgerArtifact(t, ctx, runner, repoID, sessionID, jobID)
	// Reproduce worktree_isolation: per_job — the ledger lives ONLY in the active
	// worktree, absent from repo_root.
	moveLedgerToActiveWorktree(t, ctx, runner, repoID, jobID, leaseID, "artifacts/gate/LEDGER.md")

	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"verdict":    "accept",
	}))
	if err != nil {
		t.Fatalf("record verdict from worktree-resident ledger: %v", err)
	}
	if result["status"] != "completed" {
		t.Fatalf("status = %#v; result=%#v", result["status"], result)
	}
}

// moveLedgerToActiveWorktree relocates the seeded ledger from repo_root into a
// per-job worktree and registers the active worktree row, reproducing the #484
// worktree-isolation condition (ledger absent from repo_root).
func moveLedgerToActiveWorktree(t *testing.T, ctx context.Context, runner db.Runner, repoID, jobID, leaseID, relPath string) {
	t.Helper()
	runID := "run_" + repoID
	run, err := rowByID(ctx, runner, repoID, "runs", "run_id", runID, false)
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	repoRoot := fmt.Sprint(run["repo_root"])
	worktreeRel := filepath.ToSlash(filepath.Join(".striatum", "worktrees", "wt_484"))
	dst := filepath.Join(repoRoot, filepath.FromSlash(worktreeRel), filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(repoRoot, filepath.FromSlash(relPath))
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read seeded ledger: %v", err)
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatalf("remove repo-root ledger: %v", err)
	}
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.job_worktrees (
		  repository_id, worktree_id, run_id, job_id, lease_id,
		  base_branch, worktree_path, state, created_at
		) VALUES ($1,$2,$3,$4,$5,'main',$6,'active',$7)`,
		repoID, "wt_484_"+jobID, runID, jobID, leaseID, worktreeRel, now); err != nil {
		t.Fatalf("insert job worktree: %v", err)
	}
}

// seedPublishedCollaborationLedgerArtifact inserts the artifacts row that the
// adjudicator's publish_artifact call would have created, so the primitive
// verdict path's verifyRequiredArtifacts finds the expected ledger and
// enforceCollaborationLedgerVerdict can read it from disk.
func seedPublishedCollaborationLedgerArtifact(t *testing.T, ctx context.Context, runner db.Runner, repoID, sessionID, jobID string) {
	t.Helper()
	runID := "run_" + repoID
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.artifacts (
		  repository_id, artifact_id, run_id, job_id, session_id, logical_name,
		  artifact_kind, repo_path, content_sha256, size_bytes, publish_mode, created_at
		) VALUES ($1,$2,$3,$4,$5,'collaboration_ledger','collaboration_ledger',
		  'artifacts/gate/LEDGER.md','sha_'||$2,1,'create',$6)`,
		repoID, "art_ledger_"+repoID, runID, jobID, sessionID, now); err != nil {
		t.Fatalf("insert collaboration ledger artifact: %v", err)
	}
}

func seedCollaborationLedgerSubmitFixture(t *testing.T, ctx context.Context, runner db.Runner, verdict string) (repoID, sessionID, jobID, leaseID string) {
	return seedCollaborationLedgerSubmitFixtureWithPayload(t, ctx, runner, collaborationLedgerSubmitPayload(verdict))
}

func seedCollaborationLedgerSubmitFixtureWithPayload(t *testing.T, ctx context.Context, runner db.Runner, payload string) (repoID, sessionID, jobID, leaseID string) {
	t.Helper()
	repoID = "repo_collab_" + strings.NewReplacer("/", "_", " ", "_", "-", "_").Replace(t.Name())
	runID := "run_" + repoID
	sessionID = "sess_adjudicator_" + repoID
	jobID = "job_adjudicate_" + repoID
	leaseID = "lease_" + repoID
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "artifacts", "gate"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "artifacts", "gate", "LEDGER.md"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	intgSeedRepo(t, ctx, runner, repoID)
	workflow := map[string]any{
		"workflow_id": "collaboration_gate",
		"lanes": map[string]any{
			"adjudicator": map[string]any{"display_model": "Codex GPT-5.5"},
		},
		"jobs": []any{
			map[string]any{"id": "adjudicate", "type": "phase_synthesis", "role_id": "adjudicator"},
		},
	}
	intgSeedRun(t, ctx, runner, repoID, runID, workflow)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.runs SET repo_root = $1
		 WHERE repository_id = $2 AND run_id = $3`, repoRoot, repoID, runID); err != nil {
		t.Fatalf("update run repo root: %v", err)
	}
	intgSeedSession(t, ctx, runner, repoID, runID, sessionID, "adjudicator", "adjudicator", []string{"review"}, "active")
	intgAttest(t, ctx, runner, repoID, runID, sessionID, "adjudicator")
	now := time.Now().UTC()
	laneSelectorArg, err := db.JSONBArg(runner, map[string]any{"lane_id": "adjudicator"})
	if err != nil {
		t.Fatal(err)
	}
	writeScopeArg, err := db.JSONBArg(runner, map[string]any{
		"mode":            "review_only_artifact",
		"repo_write":      false,
		"allowed_paths":   []string{"artifacts/gate/"},
		"forbidden_paths": []string{".striatum/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedArg, err := db.JSONBArg(runner, []map[string]any{{
		"logical_name": "collaboration_ledger",
		"kind":         "collaboration_ledger",
		"path":         "artifacts/gate/LEDGER.md",
		"required":     true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	// run.prepare maps a `phase_synthesis` WORKFLOW job to the stored
	// job_type='review' (see run.go: the jobs_job_type_check constraint does not
	// permit 'phase_synthesis'). Seed the live stored shape so the test exercises
	// the real row and the collaboration-ledger verdict guard fires on the same
	// path production does. The workflow_json snapshot still carries
	// type=phase_synthesis for the `adjudicate` job def, matching production.
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, lane_selector_json, write_scope_json,
		  expected_artifacts_json, idempotency_key, created_at, started_at
		) VALUES ($1,$2,$3,'adjudicate',1,'running','adjudicator','Adjudicate','review',
		  $4::jsonb,$5::jsonb,$6::jsonb,'idem_'||$2,$7,$7)`,
		repoID, jobID, runID, laneSelectorArg, writeScopeArg, expectedArg, now); err != nil {
		t.Fatalf("insert adjudication job: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.leases (
		  repository_id, lease_id, run_id, resource_type, resource_id,
		  owner_session_id, state, acquired_at, expires_at
		) VALUES ($1,$2,$3,'job',$4,$5,'active',$6,$7)`,
		repoID, leaseID, runID, jobID, sessionID, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("insert lease: %v", err)
	}
	return repoID, sessionID, jobID, leaseID
}

func collaborationLedgerSubmitPayload(verdict string) string {
	entries := `  - kind: claim
    by: sess_holder
    refs: ["dialogue:1"]
    text: "The proposal is ready."
  - kind: challenge
    by: sess_falsifier
    refs: ["dialogue:2"]
    text: "The proposal lacks migration evidence."`
	rationale := "A challenge landed but has not been rebutted."
	if verdict == "accept" {
		entries += `
  - kind: rebuttal
    by: sess_holder
    refs: ["dialogue:3"]
    text: "The migration evidence is in the linked fixture."`
		rationale = "A material challenge landed and was rebutted on the record."
	}
	return `---
schema_version: "striatum.collaboration_ledger.v1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
topic: "substance gate"
participants: ["sess_holder", "sess_falsifier"]
entries:
` + entries + `
verdict: "` + verdict + `"
rationale: "` + rationale + `"
---

# Ledger
`
}

func aceCollaborationLedgerSubmitPayload(verdict string, constraints string, findings string) string {
	return `---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "adjudicated_constraint_extraction"
topic: "productive refusal gate"
participants: ["sess_holder", "sess_falsifier"]
entries:
  - kind: claim
    by: sess_holder
    refs: ["dialogue:1"]
    text: "The proposal is ready."
  - kind: challenge
    by: sess_falsifier
    refs: ["dialogue:2"]
    text: "The proposal lacks an enforceable validation gate."
cycle: 1
verdict: "` + verdict + `"
rationale: "A challenge landed and must be carried as a constraint."
` + findings + constraints + `branches:
  implementation: blocked_pending_answer
---

# Ledger
`
}

func aceSubmitFinding() string {
	return `findings:
  - id: F1
    severity: high
    posture: implementation
    status: converted_to_constraint
    challenge: "The proposal lacks an enforceable validation gate."
    requested_constraint_shape:
      kind: gate
    source_refs: ["dialogue:2"]
`
}

func aceSubmitBindingConstraint() string {
	return `constraints:
  - id: C1
    source_finding: F1
    posture: implementation
    severity: high
    kind: gate
    binding: true
    text: "The next revision must add an executable validation gate."
    verification:
      gate: "go -C go test ./pkg/artifactcontracts/..."
`
}

// TestSubmitReviewIdempotentAfterPublishArtifact is the RFC 0095 §7 / GH #58
// live-PG regression: an operator publishes a finding with publish-artifact,
// then runs submit-review against the same finding body at the same path. The
// submit re-publishes under the default "review" logical name, which collides
// with the (repository_id, run_id, repo_path, content_sha256) unique
// constraint. Before the fix this crashed with a raw pgx 23505 on
// artifacts_repository_id_run_id_repo_path_content_sha256_key. After the fix
// the publish folds into an idempotent no-op, the verdict is recorded, and the
// job completes.
func TestSubmitReviewIdempotentAfterPublishArtifact(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("accept"))

	publishResult, err := HandlePublishArtifact(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":   sessionID,
		"job_id":       jobID,
		"lease_id":     leaseID,
		"kind":         "finding",
		"logical_name": "finding",
		"path":         "artifacts/review/FINDING.md",
	}))
	if err != nil {
		t.Fatalf("publish-artifact: %v", err)
	}
	if publishResult["status"] != "published" {
		t.Fatalf("publish status = %#v, want published", publishResult["status"])
	}
	publishedID := fmt.Sprint(publishResult["artifact_id"])

	// submit-review against the same body at the same path under the default
	// "review" logical name. This is the exact #58 collision.
	result, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"path":       "artifacts/review/FINDING.md",
		"verdict":    "accept",
		"rationale":  "finding already verified via publish-artifact",
	}))
	if err != nil {
		t.Fatalf("submit-review must be an idempotent no-op, got error: %v", err)
	}
	artifact, ok := result["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("artifact missing from result: %#v", result)
	}
	if artifact["status"] != "already_published" {
		t.Fatalf("artifact status = %#v, want already_published", artifact["status"])
	}
	if fmt.Sprint(artifact["artifact_id"]) != publishedID {
		t.Fatalf("idempotent publish reused artifact_id %v, want %v", artifact["artifact_id"], publishedID)
	}
	if artifact["notice"] == nil || !strings.Contains(fmt.Sprint(artifact["notice"]), "idempotent no-op") {
		t.Fatalf("expected an idempotent-no-op notice, got %#v", artifact["notice"])
	}
	if result["job_state"] != "completed" {
		t.Fatalf("job_state = %#v, want completed; result=%#v", result["job_state"], result)
	}
	verdict, ok := result["verdict"].(map[string]any)
	if !ok || verdict["verdict"] != "accept" {
		t.Fatalf("verdict not recorded: %#v", result["verdict"])
	}

	// Exactly one artifact row should exist at that path: the idempotent path
	// reused the existing row instead of inserting a second one.
	count, err := runner.QueryScalar(ctx, `
		SELECT count(*) FROM striatumd.artifacts
		 WHERE repository_id = $1 AND repo_path = 'artifacts/review/FINDING.md'`, repoID)
	if err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	if count != "1" {
		t.Fatalf("artifact rows at path = %s, want 1 (no duplicate insert)", count)
	}
}

// TestSubmitReviewRejectsDifferentContentSamePathSameLogicalName covers the
// real-conflict half of RFC 0095 §7 / GH #58: re-publishing *different* content
// under the same logical name + path must surface a clean rpc.Error (the
// content-mismatch guard), never a raw pgx constraint crash. The fixture seeds
// an existing artifact under logical name "review" and then submits a review
// whose on-disk body differs, so the content_sha256 no longer matches.
func TestSubmitReviewRejectsDifferentContentSamePathSameLogicalName(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("accept"))

	// Pre-seed an artifact under the default "review" logical name at the same
	// path but with a *different* content_sha256 than the on-disk body.
	now := time.Now().UTC()
	runID := "run_" + repoID
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.artifacts (
		  repository_id, artifact_id, run_id, job_id, session_id, logical_name,
		  artifact_kind, repo_path, content_sha256, size_bytes, publish_mode, created_at
		) VALUES ($1,'art_prior',$2,$3,$4,'review','finding',
		  'artifacts/review/FINDING.md','sha_does_not_match_disk',1,'create',$5)`,
		repoID, runID, jobID, sessionID, now); err != nil {
		t.Fatalf("seed prior artifact: %v", err)
	}

	_, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"path":       "artifacts/review/FINDING.md",
		"verdict":    "accept",
		"rationale":  "different body at the same path",
	}))
	if err == nil {
		t.Fatal("expected a clean rpc.Error for different content at the same logical name + path, got nil")
	}
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error is not an rpc.Error (raw constraint crash leaked): %T %v", err, err)
	}
	if rpcErr.Code != "artifact_error" || !strings.Contains(rpcErr.Message, "already exists with different content") {
		t.Fatalf("rpc.Error = %+v, want artifact_error / different content", rpcErr)
	}
}

// findingArtifactPayload returns a minimal valid finding artifact body. It
// carries no author line so the markdown author-line check is skipped, keeping
// the fixture independent of lane attestation.
func findingArtifactPayload(verdictIntent string) string {
	return `---
schema_version: "striatum.finding.v1"
artifact_kind: "finding"
verdict_intent: "` + verdictIntent + `"
---

# Finding

The change is correct.
`
}

// seedReviewFindingFixture seeds a repo + run (with an on-disk repo root) + an
// active, attested reviewer session + a running review job (no required
// artifacts, no attestation requirement) + an active lease, and writes the
// finding body to artifacts/review/FINDING.md. It returns the ids the review
// handlers need.
func seedReviewFindingFixture(t *testing.T, ctx context.Context, runner db.Runner, payload string) (repoID, sessionID, jobID, leaseID string) {
	return seedReviewFindingFixtureWithAttestation(t, ctx, runner, payload, true)
}

func seedUnattestedReviewFindingFixture(t *testing.T, ctx context.Context, runner db.Runner, payload string) (repoID, sessionID, jobID, leaseID string) {
	return seedReviewFindingFixtureWithAttestation(t, ctx, runner, payload, false)
}

func seedReviewFindingFixtureWithAttestation(t *testing.T, ctx context.Context, runner db.Runner, payload string, attested bool) (repoID, sessionID, jobID, leaseID string) {
	t.Helper()
	repoID = "repo_idem58_" + strings.ReplaceAll(t.Name(), "/", "_")
	runID := "run_" + repoID
	sessionID = "sess_reviewer_" + repoID
	jobID = "job_review_" + repoID
	leaseID = "lease_" + repoID
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "artifacts", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "artifacts", "review", "FINDING.md"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	intgSeedRepo(t, ctx, runner, repoID)
	workflow := map[string]any{
		"workflow_id": "review_wf",
		"lanes": map[string]any{
			"reviewer": map[string]any{"display_model": "Claude"},
		},
		"jobs": []any{
			map[string]any{"id": "review", "type": "review"},
		},
	}
	intgSeedRun(t, ctx, runner, repoID, runID, workflow)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.runs SET repo_root = $1
		 WHERE repository_id = $2 AND run_id = $3`, repoRoot, repoID, runID); err != nil {
		t.Fatalf("update run repo root: %v", err)
	}
	intgSeedSession(t, ctx, runner, repoID, runID, sessionID, "reviewer", "reviewer", []string{"review"}, "active")
	if attested {
		intgAttest(t, ctx, runner, repoID, runID, sessionID, "reviewer")
	}
	now := time.Now().UTC()
	laneSelectorArg, err := db.JSONBArg(runner, map[string]any{"lane_id": "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	writeScopeArg, err := db.JSONBArg(runner, map[string]any{
		"mode":            "review_only_artifact",
		"repo_write":      false,
		"allowed_paths":   []string{"artifacts/review/"},
		"forbidden_paths": []string{".striatum/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedArg, err := db.JSONBArg(runner, []map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, lane_selector_json, write_scope_json,
		  expected_artifacts_json, idempotency_key, created_at, started_at
		) VALUES ($1,$2,$3,'review',1,'running','reviewer','Review','review',
		  $4::jsonb,$5::jsonb,$6::jsonb,'idem_'||$2,$7,$7)`,
		repoID, jobID, runID, laneSelectorArg, writeScopeArg, expectedArg, now); err != nil {
		t.Fatalf("insert review job: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.leases (
		  repository_id, lease_id, run_id, resource_type, resource_id,
		  owner_session_id, state, acquired_at, expires_at
		) VALUES ($1,$2,$3,'job',$4,$5,'active',$6,$7)`,
		repoID, leaseID, runID, jobID, sessionID, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("insert lease: %v", err)
	}
	return repoID, sessionID, jobID, leaseID
}

func markReviewJobFresh(t *testing.T, ctx context.Context, runner db.Runner, repoID, jobID string) {
	t.Helper()
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET fresh_session_required = true
		 WHERE repository_id = $1 AND job_id = $2`, repoID, jobID); err != nil {
		t.Fatalf("mark review fresh: %v", err)
	}
}

func setReviewWorkflowJobPolicy(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID string, policy map[string]any) {
	t.Helper()
	reviewJob := map[string]any{"id": "review", "type": "review"}
	for key, value := range policy {
		reviewJob[key] = value
	}
	workflow := map[string]any{
		"workflow_id": "review_wf",
		"lanes": map[string]any{
			"reviewer": map[string]any{"display_model": "Claude"},
		},
		"jobs": []any{reviewJob},
	}
	workflowArg, err := db.JSONBArg(runner, workflow)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.workflow_snapshots
		   SET workflow_json = $1::jsonb
		 WHERE repository_id = $2 AND workflow_snapshot_id = $3`,
		workflowArg, repoID, "snap_"+runID); err != nil {
		t.Fatalf("set review workflow policy: %v", err)
	}
}

func countArtifactsForJob(t *testing.T, ctx context.Context, runner any, repoID, jobID string) int {
	t.Helper()
	row, err := oneRow(ctx, runner, `
		SELECT count(*) AS n
		  FROM striatumd.artifacts
		 WHERE repository_id = $1 AND job_id = $2`,
		repoID, jobID)
	if err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	return intValue(row["n"])
}

func seedReviewProvenanceDecision(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, decisionID string) string {
	t.Helper()
	now := time.Now().UTC()
	artifactID := "art_" + decisionID
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.artifacts (
		  repository_id, artifact_id, run_id, job_id, session_id,
		  logical_name, artifact_kind, repo_path, content_sha256,
		  size_bytes, publish_mode, created_at
		)
		VALUES ($1,$2,$3,NULL,NULL,$4,'decision',$5,'sha_'||$2,1,'create',$6)`,
		repoID, artifactID, runID, decisionID, "decisions/"+decisionID+".md", now); err != nil {
		t.Fatalf("insert review provenance decision artifact: %v", err)
	}
	if _, err := appendEvent(ctx, runner, repoID, runID, "decision.recorded", nil, nil, nil, artifactID, nil, map[string]any{
		"decision_id":     decisionID,
		"outcome":         "accepted",
		"path":            "decisions/" + decisionID + ".md",
		"escape_decision": true,
		"escape_surface":  "review_provenance",
		"escape_action":   "allow unattested accepting review recovery for " + runID,
	}); err != nil {
		t.Fatalf("append review provenance decision event: %v", err)
	}
	return artifactID
}

type reviewOverrideFakeRunner struct {
	tx *reviewOverrideFakeTx
}

func (r *reviewOverrideFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("unexpected runner exec outside tx")
}

func (r *reviewOverrideFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return reviewOverrideFakeRow{err: errors.New("unexpected runner query row outside tx")}
}

func (r *reviewOverrideFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected runner query scalar outside tx")
}

func (r *reviewOverrideFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return r.tx, nil
}

type reviewOverrideFakeTx struct {
	reviewState      string
	downstreamState  string
	previousVerdict  string
	latestVerdict    string
	insertedVerdicts int
	events           []string
	nextEvent        int64
	committed        bool
	rolledBack       bool
}

func newReviewOverrideFakeTx() *reviewOverrideFakeTx {
	return &reviewOverrideFakeTx{
		reviewState:     "completed",
		downstreamState: "blocked",
	}
}

func (tx *reviewOverrideFakeTx) Exec(_ context.Context, sql string, args ...any) error {
	switch {
	case strings.Contains(sql, "INSERT INTO striatumd.verdicts"):
		tx.insertedVerdicts++
		tx.latestVerdict = args[5].(string)
	case strings.Contains(sql, "UPDATE striatumd.jobs") && strings.Contains(sql, "SET state = 'completed'"):
		tx.reviewState = "completed"
	case strings.Contains(sql, "UPDATE striatumd.jobs") && strings.Contains(sql, "SET state = 'queued'"):
		tx.downstreamState = "queued"
	case strings.Contains(sql, "INSERT INTO striatumd.events"):
		tx.events = append(tx.events, args[3].(string))
	}
	return nil
}

func (tx *reviewOverrideFakeTx) QueryRow(_ context.Context, sql string, _ ...any) db.Row {
	switch {
	case strings.Contains(sql, "repo_event_chain_heads"):
		return reviewOverrideFakeRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "nextval"):
		tx.nextEvent++
		return reviewOverrideFakeRow{values: []any{tx.nextEvent}}
	default:
		return reviewOverrideFakeRow{err: errors.New("unexpected query row: " + sql)}
	}
}

func (tx *reviewOverrideFakeTx) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected query scalar")
}

func (tx *reviewOverrideFakeTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "FROM striatumd.jobs"):
		return tx.queryJobs(sql, args...), nil
	case strings.Contains(sql, "FROM striatumd.sessions"):
		return runPrepareRowsFromMaps([]map[string]any{{
			"session_id": "sess_override",
			"run_id":     "run_1",
			"state":      "active",
			"role_id":    "reviewer",
			"lane_id":    "codex",
		}}), nil
	case strings.Contains(sql, "SELECT 1 FROM striatumd.verdicts"):
		return runPrepareRowsFromMaps(nil), nil
	case strings.Contains(sql, "SELECT * FROM striatumd.verdicts"):
		if tx.previousVerdict == "" {
			return runPrepareRowsFromMaps(nil), nil
		}
		return runPrepareRowsFromMaps([]map[string]any{{
			"verdict_id":           "verdict_previous",
			"verdict":              tx.previousVerdict,
			"findings_artifact_id": nil,
		}}), nil
	case strings.Contains(sql, "SELECT verdict FROM striatumd.verdicts"):
		if tx.latestVerdict == "" {
			return runPrepareRowsFromMaps(nil), nil
		}
		return runPrepareRowsFromMaps([]map[string]any{{"verdict": tx.latestVerdict}}), nil
	case strings.Contains(sql, "FROM striatumd.runs"):
		return runPrepareRowsFromMaps([]map[string]any{{
			"run_id":               "run_1",
			"state":                "running",
			"workflow_snapshot_id": "snap_1",
		}}), nil
	case strings.Contains(sql, "FROM striatumd.workflow_snapshots"):
		return runPrepareRowsFromMaps([]map[string]any{{
			"workflow_snapshot_id": "snap_1",
			"workflow_json": map[string]any{
				"jobs": []any{
					map[string]any{"id": "review", "type": "review"},
				},
			},
		}}), nil
	case strings.Contains(sql, "FROM striatumd.blockers"):
		// RFC 0132 P4b (#341): the advisory-guard hold check on the downstream gate
		// (dependenciesSatisfied -> gateHeldByOpenAdvisoryGuard) has no advisory blocker
		// in these override fixtures, so it must return no rows; otherwise the gate would
		// read as held and never enqueue.
		if strings.Contains(sql, "blocker_kind = ANY($3)") {
			return runPrepareRowsFromMaps(nil), nil
		}
		return runPrepareRowsFromMaps([]map[string]any{{"blocker_id": "blk_1"}}), nil
	case strings.Contains(sql, "FROM striatumd.job_dependencies"):
		return tx.queryDependencies(sql, args...), nil
	default:
		return nil, errors.New("unexpected query: " + sql)
	}
}

func (tx *reviewOverrideFakeTx) queryJobs(sql string, args ...any) pgx.Rows {
	if strings.Contains(sql, "state = 'failed'") {
		return runPrepareRowsFromMaps(nil)
	}
	if strings.Contains(sql, "state NOT IN") {
		if tx.downstreamState == "completed" || tx.downstreamState == "skipped" || tx.downstreamState == "canceled" {
			return runPrepareRowsFromMaps(nil)
		}
		return runPrepareRowsFromMaps([]map[string]any{{"job_id": "job_apply"}})
	}
	if strings.Contains(sql, "state = 'completed'") {
		return runPrepareRowsFromMaps([]map[string]any{{"job_id": "job_review"}})
	}
	jobID := args[1].(string)
	if jobID == "job_review" {
		return runPrepareRowsFromMaps([]map[string]any{{
			"job_id":             "job_review",
			"run_id":             "run_1",
			"workflow_job_id":    "review",
			"job_type":           "review",
			"state":              tx.reviewState,
			"current_message_id": "msg_review",
		}})
	}
	return runPrepareRowsFromMaps([]map[string]any{{
		"job_id":             "job_apply",
		"run_id":             "run_1",
		"workflow_job_id":    "apply",
		"job_type":           "synthesis",
		"state":              tx.downstreamState,
		"role_id":            "author",
		"lane_selector_json": map[string]any{},
		"max_attempts":       1,
	}})
}

func (tx *reviewOverrideFakeTx) queryDependencies(sql string, args ...any) pgx.Rows {
	if strings.Contains(sql, "depends_on_job_id") {
		if args[1] == "job_review" {
			return runPrepareRowsFromMaps([]map[string]any{{"job_id": "job_apply"}})
		}
		return runPrepareRowsFromMaps(nil)
	}
	if args[1] == "job_apply" {
		return runPrepareRowsFromMaps([]map[string]any{{
			"job_id":            "job_apply",
			"depends_on_job_id": "job_review",
			"gate_json": map[string]any{
				"requires_verdict": []any{"accept", "accept_with_findings"},
			},
		}})
	}
	return runPrepareRowsFromMaps(nil)
}

func (tx *reviewOverrideFakeTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *reviewOverrideFakeTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

func (tx *reviewOverrideFakeTx) sawEvent(eventType string) bool {
	for _, event := range tx.events {
		if event == eventType {
			return true
		}
	}
	return false
}

type reviewOverrideFakeRow struct {
	values []any
	err    error
}

func (r reviewOverrideFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, value := range r.values {
		switch target := dest[i].(type) {
		case *string:
			if value == nil {
				*target = ""
			} else {
				*target = fmt.Sprint(value)
			}
		case *int64:
			*target = value.(int64)
		case **int:
			if value == nil {
				*target = nil
			} else {
				v := intValue(value)
				*target = &v
			}
		case **string:
			if value == nil {
				*target = nil
			} else {
				text := value.(string)
				*target = &text
			}
		case *any:
			*target = value
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

// TestSubmitReviewDuplicateArtifactHandling exercises RFC 0095 §7 / GH #58 at
// the handler seam with a fake runner: when an artifact with the same body is
// already published at the submit path (the repo_path + content_sha256
// constraint, not the logical-name constraint), publishArtifact returns a
// {status: already_published} no-op and the whole submit completes in a single
// committed transaction. The previous behavior let the INSERT crash with a raw
// 23505 and re-ran the submit in a second transaction; that post-hoc retry has
// been removed in favor of the proactive pre-check.
func TestSubmitReviewDuplicateArtifactHandling(t *testing.T) {
	tmpDir := t.TempDir()
	artPath := filepath.Join(tmpDir, "finding.md")
	mustWrite(t, artPath, `---
schema_version: striatum.finding.v1
artifact_kind: finding
verdict_intent: accept
---
author: author-unknown-model-001

Everything looks perfect.`)

	runner := &submitReviewFakeRunner{
		repoRoot: tmpDir,
	}

	result, err := HandleSubmitReview(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_submit_duplicate",
		Method:        "review.submit",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_review",
			"job_id":        "job_review",
			"lease_id":      "lease_1",
			"path":          "finding.md",
			"verdict":       "accept",
			"logical_name":  "review_art",
			"kind":          "finding",
			"rationale":     "looks great",
		},
	})
	if err != nil {
		t.Fatalf("HandleSubmitReview failed: %v", err)
	}

	if result == nil {
		t.Fatalf("expected result, got nil")
	}

	artifact := result["artifact"].(map[string]any)
	if artifact["artifact_id"] != "art_existing" {
		t.Fatalf("expected artifact_id to be 'art_existing', got %v", artifact["artifact_id"])
	}
	if artifact["status"] != "already_published" {
		t.Fatalf("expected status to be 'already_published', got %v", artifact["status"])
	}

	if runner.txCount != 1 {
		t.Fatalf("expected a single transaction, got %d", runner.txCount)
	}
	if runner.tx1 == nil || !runner.tx1.committed {
		t.Fatalf("expected the single transaction to be committed")
	}
	if runner.tx1.rolledBack {
		t.Fatalf("did not expect the transaction to be rolled back")
	}
	if runner.tx1.artifactInserts != 0 {
		t.Fatalf("expected no artifact INSERT on the idempotent path, got %d", runner.tx1.artifactInserts)
	}
}

type submitReviewFakeRunner struct {
	repoRoot string
	tx1      *submitReviewFakeTx
	txCount  int
}

func (r *submitReviewFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("unexpected runner exec outside tx")
}

func (r *submitReviewFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return reviewOverrideFakeRow{err: errors.New("unexpected runner query row outside tx")}
}

func (r *submitReviewFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected runner query scalar outside tx")
}

func (r *submitReviewFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	r.txCount++
	r.tx1 = &submitReviewFakeTx{repoRoot: r.repoRoot}
	return r.tx1, nil
}

type submitReviewFakeTx struct {
	repoRoot        string
	committed       bool
	rolledBack      bool
	artifactInserts int
}

func (tx *submitReviewFakeTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *submitReviewFakeTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

func (tx *submitReviewFakeTx) Exec(_ context.Context, sql string, args ...any) error {
	if strings.Contains(sql, "INSERT INTO striatumd.artifacts") {
		// The proactive pre-check should short-circuit before any artifact
		// INSERT on the idempotent path; record the attempt so the test can
		// assert it never happens.
		tx.artifactInserts++
	}
	return nil
}

func (tx *submitReviewFakeTx) QueryRow(_ context.Context, sql string, _ ...any) db.Row {
	switch {
	case strings.Contains(sql, "FROM striatumd.process_supervisors ps"):
		return reviewOverrideFakeRow{values: []any{
			"sup_review",
			"run_1",
			"sess_review",
			"attached",
			tx.repoRoot,
			"",
			os.Getpid(),
			"",
			"dsup_review",
			map[string]any{},
		}}
	case strings.Contains(sql, "repo_event_chain_heads"):
		return reviewOverrideFakeRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "nextval"):
		return reviewOverrideFakeRow{values: []any{int64(1)}}
	default:
		return reviewOverrideFakeRow{err: pgx.ErrNoRows}
	}
}

func (tx *submitReviewFakeTx) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", nil
}

func (tx *submitReviewFakeTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "LEFT JOIN striatumd.process_supervisors ps") &&
		strings.Contains(sql, "LEFT JOIN striatumd.process_supervisor_pointers p"):
		pid := os.Getpid()
		return runPrepareRowsFromMaps([]map[string]any{{
			"supervisor_id":                "sup_review",
			"pid":                          pid,
			"pid_start_time":               "",
			"supervisor_state":             "attached",
			"pointer_daemon_supervisor_id": "dsup_review",
			"pointer_pid":                  pid,
			"pointer_pid_start_time":       "",
			"pointer_state":                "attached",
			"pointer_metadata_json":        map[string]any{},
			"daemon_supervisor_id":         "dsup_review",
			"daemon_state":                 "attached",
			"state":                        "active",
			"registered_at":                time.Now().UTC(),
		}}), nil
	case strings.Contains(sql, "FROM striatumd.jobs"):
		return runPrepareRowsFromMaps([]map[string]any{{
			"job_id":             "job_review",
			"run_id":             "run_1",
			"job_type":           "review",
			"state":              "running",
			"current_message_id": "msg_review",
			"role_id":            "author",
			"workflow_job_id":    "review",
			"lane_selector_json": map[string]any{},
			"write_scope_json": map[string]any{
				"allowed_paths": []any{"."},
			},
		}}), nil
	case strings.Contains(sql, "FROM striatumd.leases"):
		return runPrepareRowsFromMaps([]map[string]any{{
			"lease_id":         "lease_1",
			"owner_session_id": "sess_review",
			"resource_id":      "job_review",
			"state":            "active",
		}}), nil
	case strings.Contains(sql, "FROM striatumd.sessions"):
		return runPrepareRowsFromMaps([]map[string]any{{
			"session_id": "sess_review",
			"run_id":     "run_1",
			"state":      "active",
			"ordinal":    1,
		}}), nil
	case strings.Contains(sql, "FROM striatumd.runs"):
		return runPrepareRowsFromMaps([]map[string]any{{
			"run_id":               "run_1",
			"repo_root":            tx.repoRoot,
			"workflow_snapshot_id": "snap_1",
			"state":                "running",
		}}), nil
	case strings.Contains(sql, "FROM striatumd.workflow_snapshots"):
		return runPrepareRowsFromMaps([]map[string]any{{
			"workflow_snapshot_id": "snap_1",
			"workflow_json": map[string]any{
				"jobs": []any{
					map[string]any{"id": "review", "type": "review"},
				},
			},
		}}), nil
	case strings.Contains(sql, "FROM striatumd.artifacts") && strings.Contains(sql, "content_sha256 = $4"):
		// RFC 0095 §7 / GH #58 proactive pre-check on the
		// (repository_id, run_id, repo_path, content_sha256) constraint:
		// the same body is already published at this path, so return the
		// existing artifact and let publishArtifact fold it into an
		// already_published no-op.
		return runPrepareRowsFromMaps([]map[string]any{{
			"artifact_id":  "art_existing",
			"logical_name": "finding",
		}}), nil
	case strings.Contains(sql, "SELECT artifact_id FROM striatumd.artifacts"):
		return runPrepareRowsFromMaps([]map[string]any{{
			"artifact_id": "art_existing",
		}}), nil
	case strings.Contains(sql, "SELECT * FROM striatumd.artifacts"):
		if strings.Contains(sql, "logical_name") {
			return runPrepareRowsFromMaps(nil), nil
		}
		return runPrepareRowsFromMaps([]map[string]any{{
			"artifact_id":    "art_existing",
			"run_id":         "run_1",
			"job_id":         "job_review",
			"logical_name":   "review_art",
			"artifact_kind":  "finding",
			"repo_path":      "finding.md",
			"content_sha256": "some-sha",
		}}), nil
	case strings.Contains(sql, "FROM striatumd.job_dependencies"):
		return runPrepareRowsFromMaps(nil), nil
	default:
		return runPrepareRowsFromMaps(nil), nil
	}
}

// #98: classify a Postgres deadlock (SQLSTATE 40P01) so review.submit can
// retry it, while leaving every other error untouched.
func TestIsDeadlockError(t *testing.T) {
	if !isDeadlockError(&pgconn.PgError{Code: "40P01", Message: "deadlock detected"}) {
		t.Fatalf("40P01 should classify as a deadlock")
	}
	if !isDeadlockError(fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "40P01"})) {
		t.Fatalf("wrapped 40P01 should classify as a deadlock")
	}
	if isDeadlockError(&pgconn.PgError{Code: "23505"}) {
		t.Fatalf("unique-violation (23505) must not classify as a deadlock")
	}
	if isDeadlockError(errors.New("plain error")) {
		t.Fatalf("plain error must not classify as a deadlock")
	}
	if isDeadlockError(nil) {
		t.Fatalf("nil must not classify as a deadlock")
	}
}

// #98: withTxRetryOnDeadlock retries a transaction that aborts with a deadlock
// and succeeds on a later attempt; a non-deadlock error is returned at once.
func TestWithTxRetryOnDeadlock(t *testing.T) {
	ctx := context.Background()
	initialRetryExhausted := deadlockRetryExhaustedCounter.Value()

	// Deadlock on the first two attempts, then succeed.
	runner := &deadlockRetryRunner{failWith: &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}, failTimes: 2}
	result, err := withTxRetryOnDeadlock(ctx, runner, func(db.TxRunner) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("result = %#v", result)
	}
	if runner.attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (two deadlocks then success)", runner.attempts)
	}
	if got := deadlockRetryExhaustedCounter.Value(); got != initialRetryExhausted {
		t.Fatalf("deadlock.retry_exhausted = %d, want unchanged at %d after successful retry", got, initialRetryExhausted)
	}

	// Deadlock on every attempt -> bounded failure with a serial-retry hint.
	always := &deadlockRetryRunner{failWith: &pgconn.PgError{Code: "40P01"}, failTimes: 99}
	beforeExhausted := deadlockRetryExhaustedCounter.Value()
	_, err = withTxRetryOnDeadlock(ctx, always, func(db.TxRunner) (map[string]any, error) {
		return nil, nil
	})
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected rpc error after exhausting retries, got %v", err)
	}
	if !strings.Contains(rpcErr.Message, "retry serially") {
		t.Fatalf("error message = %q, want it to mention serial retry", rpcErr.Message)
	}
	if always.attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (bounded)", always.attempts)
	}
	if got := deadlockRetryExhaustedCounter.Value(); got != beforeExhausted+1 {
		t.Fatalf("deadlock.retry_exhausted = %d, want %d after retry exhaustion", got, beforeExhausted+1)
	}

	// A non-deadlock error is returned immediately, no retry.
	nonDeadlock := &deadlockRetryRunner{failWith: &pgconn.PgError{Code: "23505"}, failTimes: 99}
	beforeNonDeadlock := deadlockRetryExhaustedCounter.Value()
	_, err = withTxRetryOnDeadlock(ctx, nonDeadlock, func(db.TxRunner) (map[string]any, error) {
		return nil, nil
	})
	if !isDeadlockError(err) && !errors.As(err, new(*pgconn.PgError)) {
		t.Fatalf("expected the underlying pg error back, got %v", err)
	}
	if nonDeadlock.attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on non-deadlock)", nonDeadlock.attempts)
	}
	if got := deadlockRetryExhaustedCounter.Value(); got != beforeNonDeadlock {
		t.Fatalf("deadlock.retry_exhausted = %d, want unchanged at %d after non-deadlock", got, beforeNonDeadlock)
	}
}

// deadlockRetryRunner fails BeginTx with failWith for the first failTimes
// attempts, then returns a committable no-op transaction. withTx surfaces the
// BeginTx error directly, which is what the retry wrapper classifies.
type deadlockRetryRunner struct {
	failWith  error
	failTimes int
	attempts  int
}

func (r *deadlockRetryRunner) Exec(context.Context, string, ...any) error { return nil }
func (r *deadlockRetryRunner) QueryRow(context.Context, string, ...any) db.Row {
	return reviewOverrideFakeRow{err: pgx.ErrNoRows}
}
func (r *deadlockRetryRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", nil
}
func (r *deadlockRetryRunner) BeginTx(context.Context) (db.TxRunner, error) {
	r.attempts++
	if r.attempts <= r.failTimes {
		return nil, r.failWith
	}
	return &deadlockRetryTx{}, nil
}

type deadlockRetryTx struct{}

func (deadlockRetryTx) Exec(context.Context, string, ...any) error { return nil }
func (deadlockRetryTx) QueryRow(context.Context, string, ...any) db.Row {
	return reviewOverrideFakeRow{err: pgx.ErrNoRows}
}
func (deadlockRetryTx) QueryScalar(context.Context, string, ...any) (string, error) { return "", nil }
func (deadlockRetryTx) Commit(context.Context) error                                { return nil }
func (deadlockRetryTx) Rollback(context.Context) error                              { return nil }
