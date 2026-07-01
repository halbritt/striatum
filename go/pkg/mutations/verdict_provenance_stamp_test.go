package mutations

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// RFC 0118 P0-1 (GH #240): every verdict row freezes its provenance basis at
// record time — the lane-attestation state the admission gate decided on, the
// supervising process identity, and any operator override decision — so the
// run-completion gate and post-incident audit can read a verdict's provenance
// from the frozen row alone. Assertions read the verdicts row back via SQL;
// no live lanehealth probe in the assertion path.

func readVerdictProvenanceStamp(t *testing.T, ctx context.Context, runner any, repoID, jobID string) map[string]any {
	t.Helper()
	row, err := oneRow(ctx, runner, `
		SELECT lane_attestation_at_record, review_provenance_override,
		       review_provenance_decision_id, supervisor_id_at_record
		  FROM striatumd.verdicts
		 WHERE repository_id = $1 AND job_id = $2`, repoID, jobID)
	if err != nil {
		t.Fatalf("read verdict provenance stamp: %v", err)
	}
	return row
}

func readVerdictModelIdentityStamp(t *testing.T, ctx context.Context, runner any, repoID, jobID, verdict string) map[string]any {
	t.Helper()
	row, err := oneRow(ctx, runner, `
		SELECT model_identity_declared, model_family_at_record,
		       model_identity_basis, model_co_blindness_at_record
		  FROM striatumd.verdicts
		 WHERE repository_id = $1 AND job_id = $2
		   AND ($3 = '' OR verdict = $3)
		 ORDER BY created_at DESC, verdict_id DESC
		 LIMIT 1`, repoID, jobID, verdict)
	if err != nil {
		t.Fatalf("read verdict model identity stamp: %v", err)
	}
	return row
}

func applyOwnerBundlesForVerdictModelIdentity(t *testing.T, ctx context.Context, runner db.Runner) {
	t.Helper()
	if _, _, err := db.ApplyOwnerBundles(ctx, runner, "test"); err != nil {
		t.Fatalf("apply owner bundles: %v", err)
	}
}

func TestSubmitReviewStampsDeclaredModelIdentity(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	applyOwnerBundlesForVerdictModelIdentity(t, ctx, runner)
	repoID, sessionID, jobID, leaseID := seedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("accept"))

	if _, err := HandleSubmitReview(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"path":       "artifacts/review/FINDING.md",
		"verdict":    "accept",
		"rationale":  "attested reviewer accepts",
	})); err != nil {
		t.Fatalf("submit review: %v", err)
	}

	stamp := readVerdictModelIdentityStamp(t, ctx, runner, repoID, jobID, "accept")
	if got := fmt.Sprint(stamp["model_identity_declared"]); got != "Claude" {
		t.Fatalf("model_identity_declared = %q, want Claude", got)
	}
	if got := fmt.Sprint(stamp["model_family_at_record"]); got != "claude" {
		t.Fatalf("model_family_at_record = %q, want claude", got)
	}
	if got := fmt.Sprint(stamp["model_identity_basis"]); got != verdictModelBasisWorkflowDisplay {
		t.Fatalf("model_identity_basis = %q, want %s", got, verdictModelBasisWorkflowDisplay)
	}
	if got := fmt.Sprint(stamp["model_co_blindness_at_record"]); got != verdictCoBlindnessUnknown {
		t.Fatalf("model_co_blindness_at_record = %q, want unknown without upstream dependency", got)
	}
}

func TestOperatorOverrideStampsUnknownModelIdentity(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	applyOwnerBundlesForVerdictModelIdentity(t, ctx, runner)
	repoID, sessionID, jobID, leaseID := seedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("needs_revision"))

	if _, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"verdict":    "needs_revision",
		"rationale":  "reviewer requests changes",
	})); err != nil {
		t.Fatalf("record needs_revision: %v", err)
	}
	if _, err := HandleOverrideVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":         sessionID,
		"job_id":             jobID,
		"verdict":            "accept",
		"rationale":          "operator accepts despite parked revision checkpoint",
		"auto_fresh_session": true,
	})); err != nil {
		t.Fatalf("override verdict: %v", err)
	}

	stamp := readVerdictModelIdentityStamp(t, ctx, runner, repoID, jobID, "accept")
	if got := fmt.Sprint(stamp["model_identity_declared"]); got != verdictModelUnknown {
		t.Fatalf("model_identity_declared = %q, want unknown", got)
	}
	if got := fmt.Sprint(stamp["model_family_at_record"]); got != verdictModelUnknown {
		t.Fatalf("model_family_at_record = %q, want unknown", got)
	}
	if got := fmt.Sprint(stamp["model_identity_basis"]); got != "operator_override" {
		t.Fatalf("model_identity_basis = %q, want operator_override", got)
	}
}

func TestRecordVerdictRecoveryBasisStampsUnknownModelIdentity(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	applyOwnerBundlesForVerdictModelIdentity(t, ctx, runner)
	repoID, sessionID, jobID, leaseID := seedUnattestedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("accept"))

	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		return recordVerdict(ctx, tx, repoID, sessionID, jobID, leaseID, "accept", nil,
			"auto-finalized from stable expected artifact", recordVerdictOptions{
				ProvenanceOverrideBasis: "daemon_auto_finalized_from_artifact",
			})
	}); err != nil {
		t.Fatalf("record verdict with recovery basis: %v", err)
	}

	stamp := readVerdictModelIdentityStamp(t, ctx, runner, repoID, jobID, "accept")
	if got := fmt.Sprint(stamp["model_identity_declared"]); got != verdictModelUnknown {
		t.Fatalf("model_identity_declared = %q, want unknown", got)
	}
	if got := fmt.Sprint(stamp["model_identity_basis"]); got != "daemon_auto_finalized_from_artifact" {
		t.Fatalf("model_identity_basis = %q, want recovery basis", got)
	}
}

// RFC 0118 P0-2: review.override is an operator surface — its verdict row
// must carry posture='override' (literal, never the workflow-resolved
// posture) and a review_provenance_override stamp, so an operator clear can
// never read back as a clean neutral lane verdict.
func TestOverrideVerdictForcesOverridePostureAndStamp(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("needs_revision"))

	if _, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"verdict":    "needs_revision",
		"rationale":  "reviewer requests changes",
	})); err != nil {
		t.Fatalf("record needs_revision: %v", err)
	}

	if _, err := HandleOverrideVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":         sessionID,
		"job_id":             jobID,
		"verdict":            "accept",
		"rationale":          "operator accepts despite parked revision checkpoint",
		"auto_fresh_session": true,
	})); err != nil {
		t.Fatalf("override verdict: %v", err)
	}

	row, err := oneRow(ctx, runner, `
		SELECT posture, lane_attestation_at_record, review_provenance_override,
		       review_provenance_decision_id, supervisor_id_at_record
		  FROM striatumd.verdicts
		 WHERE repository_id = $1 AND job_id = $2 AND verdict = 'accept'`, repoID, jobID)
	if err != nil {
		t.Fatalf("read override verdict row: %v", err)
	}
	if got := fmt.Sprint(row["posture"]); got != "override" {
		t.Fatalf("posture = %q, want override (literal, not workflow-resolved)", got)
	}
	if row["review_provenance_override"] != true {
		t.Fatalf("review_provenance_override = %v, want true", row["review_provenance_override"])
	}
	if got := fmt.Sprint(row["lane_attestation_at_record"]); got != "unattested" {
		t.Fatalf("lane_attestation_at_record = %q, want unattested (minted override session)", got)
	}
	event, err := oneRow(ctx, runner, `
		SELECT payload_json FROM striatumd.events
		 WHERE repository_id = $1 AND event_type = 'verdict.overridden'
		   AND job_id = $2
		 ORDER BY event_id DESC LIMIT 1`, repoID, jobID)
	if err != nil {
		t.Fatalf("read verdict.overridden event: %v", err)
	}
	if payload := asMap(event["payload_json"]); payload["posture"] != "override" {
		t.Fatalf("verdict.overridden payload posture = %v, want override", payload["posture"])
	}
}

// RFC 0118 P0-2 (mechanism 3): an operator/recovery surface that records a
// verdict with no admission gate of its own declares its override basis via
// recordVerdictOptions, so the auto-finalize bypass can never mint a verdict
// that reads back as a clean lane clear. This is the chokepoint contract the
// recovery.auto_finalize caller relies on.
func TestRecordVerdictProvenanceOverrideBasisStampsOverride(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedUnattestedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("accept"))

	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		return recordVerdict(ctx, tx, repoID, sessionID, jobID, leaseID, "accept", nil,
			"auto-finalized from stable expected artifact", recordVerdictOptions{
				ProvenanceOverrideBasis: "daemon_auto_finalized_from_artifact",
			})
	}); err != nil {
		t.Fatalf("record verdict with override basis: %v", err)
	}

	stamp := readVerdictProvenanceStamp(t, ctx, runner, repoID, jobID)
	if stamp["review_provenance_override"] != true {
		t.Fatalf("review_provenance_override = %v, want true (declared basis)", stamp["review_provenance_override"])
	}
	if got := fmt.Sprint(stamp["lane_attestation_at_record"]); got != "unattested" {
		t.Fatalf("lane_attestation_at_record = %q, want unattested", got)
	}
	event, err := oneRow(ctx, runner, `
		SELECT payload_json FROM striatumd.events
		 WHERE repository_id = $1 AND event_type = 'verdict.recorded' AND job_id = $2
		 ORDER BY event_id DESC LIMIT 1`, repoID, jobID)
	if err != nil {
		t.Fatalf("read verdict.recorded event: %v", err)
	}
	if payload := asMap(event["payload_json"]); payload["review_provenance_basis"] != "daemon_auto_finalized_from_artifact" {
		t.Fatalf("verdict.recorded payload basis = %v, want daemon_auto_finalized_from_artifact", payload["review_provenance_basis"])
	}
}

// RFC 0118 P0-2: the checkpoint.override clearing verdict (already
// posture='override') must also carry the frozen provenance stamp, bound to
// the resolving decision.
func TestCheckpointOverrideStampsClearingVerdict(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	applyOwnerBundlesForVerdictModelIdentity(t, ctx, runner)
	repoID := "repo_ckpt_stamp"
	runID, reviewJobID, _, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, []any{})
	blockerID := openCheckpointViaVerdict(t, ctx, runner, repoID, reviewJobID, sessionID, leaseID)
	decisionID := "dec_ckpt_stamp"
	seedDecisionArtifact(t, ctx, runner, repoID, runID, decisionID, "accepted")

	if _, err := HandleCheckpointResolve(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id":  blockerID,
		"action":      "override",
		"decision_id": decisionID,
	})); err != nil {
		t.Fatalf("checkpoint override: %v", err)
	}

	row, err := oneRow(ctx, runner, `
		SELECT lane_attestation_at_record, review_provenance_override,
		       review_provenance_decision_id, supervisor_id_at_record,
		       model_identity_declared, model_family_at_record,
		       model_identity_basis, model_co_blindness_at_record
		  FROM striatumd.verdicts
		 WHERE repository_id = $1 AND job_id = $2 AND posture = 'override'`, repoID, reviewJobID)
	if err != nil {
		t.Fatalf("read clearing verdict stamp: %v", err)
	}
	if row["review_provenance_override"] != true {
		t.Fatalf("review_provenance_override = %v, want true", row["review_provenance_override"])
	}
	if got := fmt.Sprint(row["review_provenance_decision_id"]); got != decisionID {
		t.Fatalf("review_provenance_decision_id = %q, want %q", got, decisionID)
	}
	if got := fmt.Sprint(row["lane_attestation_at_record"]); got != "unattested" {
		t.Fatalf("lane_attestation_at_record = %q, want unattested (minted clearing session)", got)
	}
	if got := fmt.Sprint(row["model_identity_declared"]); got != verdictModelUnknown {
		t.Fatalf("model_identity_declared = %q, want unknown", got)
	}
	if got := fmt.Sprint(row["model_identity_basis"]); got != "checkpoint_override" {
		t.Fatalf("model_identity_basis = %q, want checkpoint_override", got)
	}
}

// RFC 0118 reconciliation note 1: a session that claimed its job via the
// admin work.claim_override escape (#222) is by construction an
// operator-escaped, non-independent claimant. Any verdict it later records
// must carry the claim's authorizing decision onto the frozen stamp — even
// when the lane is attested at record time — so the run-completion gate can
// never count it toward a clean lanes_attested completion.
func TestRecordVerdictCarriesClaimOverrideDecisionOntoStamp(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("accept"))
	runID := "run_" + repoID
	claimDecisionID := "dec_claim_override_" + strings.ReplaceAll(t.Name(), "/", "_")
	if _, err := appendEvent(ctx, runner, repoID, runID, "work.claim_overridden", sessionID, jobID, nil, nil, nil, map[string]any{
		"decision_id": claimDecisionID,
		"override":    "fresh_review_process_lineage",
	}); err != nil {
		t.Fatalf("seed claim_overridden event: %v", err)
	}

	if _, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"verdict":    "accept",
		"rationale":  "override-claimed reviewer accepts",
	})); err != nil {
		t.Fatalf("record accept verdict: %v", err)
	}

	stamp := readVerdictProvenanceStamp(t, ctx, runner, repoID, jobID)
	if got := fmt.Sprint(stamp["lane_attestation_at_record"]); got != "attested" {
		t.Fatalf("lane_attestation_at_record = %q, want attested", got)
	}
	if stamp["review_provenance_override"] != true {
		t.Fatalf("review_provenance_override = %v, want true (claim_override carried forward)", stamp["review_provenance_override"])
	}
	if got := fmt.Sprint(stamp["review_provenance_decision_id"]); got != claimDecisionID {
		t.Fatalf("review_provenance_decision_id = %q, want claim decision %q", got, claimDecisionID)
	}
}

// An unattested fresh review accepted under a verdict-time provenance
// decision stamps the override and the authorizing decision, and freezes the
// unattested lane state the gate actually saw.
func TestRecordVerdictStampsOverrideProvenance(t *testing.T) {
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
	decisionID := "dec_stamp_override_" + strings.ReplaceAll(t.Name(), "/", "_")
	seedReviewProvenanceDecision(t, ctx, runner, repoID, runID, decisionID)

	if _, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id":                    sessionID,
		"job_id":                        jobID,
		"lease_id":                      leaseID,
		"verdict":                       "accept",
		"findings_artifact_id":          published["artifact_id"],
		"rationale":                     "operator accepted unattested fresh review",
		"review_provenance_decision_id": decisionID,
	})); err != nil {
		t.Fatalf("record verdict with provenance decision: %v", err)
	}

	stamp := readVerdictProvenanceStamp(t, ctx, runner, repoID, jobID)
	if got := fmt.Sprint(stamp["lane_attestation_at_record"]); got != "unattested" {
		t.Fatalf("lane_attestation_at_record = %q, want unattested", got)
	}
	if stamp["review_provenance_override"] != true {
		t.Fatalf("review_provenance_override = %v, want true", stamp["review_provenance_override"])
	}
	if got := fmt.Sprint(stamp["review_provenance_decision_id"]); got != decisionID {
		t.Fatalf("review_provenance_decision_id = %q, want %q", got, decisionID)
	}
	if stamp["supervisor_id_at_record"] != nil {
		t.Fatalf("supervisor_id_at_record = %v, want NULL", stamp["supervisor_id_at_record"])
	}
}

func TestRecordVerdictStampsAttestedProvenance(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID, sessionID, jobID, leaseID := seedReviewFindingFixture(t, ctx, runner, findingArtifactPayload("accept"))

	if _, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     jobID,
		"lease_id":   leaseID,
		"verdict":    "accept",
		"rationale":  "attested reviewer accepts",
	})); err != nil {
		t.Fatalf("record accept verdict: %v", err)
	}

	stamp := readVerdictProvenanceStamp(t, ctx, runner, repoID, jobID)
	if got := fmt.Sprint(stamp["lane_attestation_at_record"]); got != "attested" {
		t.Fatalf("lane_attestation_at_record = %q, want attested", got)
	}
	if stamp["review_provenance_override"] != false {
		t.Fatalf("review_provenance_override = %v, want false", stamp["review_provenance_override"])
	}
	if stamp["review_provenance_decision_id"] != nil {
		t.Fatalf("review_provenance_decision_id = %v, want NULL", stamp["review_provenance_decision_id"])
	}
	if got := fmt.Sprint(stamp["supervisor_id_at_record"]); got != "sup_"+sessionID {
		t.Fatalf("supervisor_id_at_record = %q, want %q", got, "sup_"+sessionID)
	}
}

// RFC 0118 P0-2: the regression guard over EVERY verdict-write surface in
// this package. Each surface is classified either as a lane surface (stamps
// the admission gate's attestation truthfully, may stay posture=neutral) or
// an operator/recovery surface (must force posture='override' or declare a
// review_provenance override basis). A new INSERT INTO striatumd.verdicts,
// applyVerdict caller, or recordVerdict caller fails this test until it is
// classified below AND covered by a behavioral stamp test like the ones in
// this file.
func TestVerdictWriteSurfacesAreClassified(t *testing.T) {
	type surfaceCounts struct {
		verdictInserts     int
		applyVerdictCalls  int
		recordVerdictCalls int
	}
	classified := map[string]surfaceCounts{
		// applyVerdict INSERT — the chokepoint: lane verdicts stamp the threaded
		// gate attestation; operator decisions arrive via review_provenance;
		// work.claim_override decisions are carried forward; recovery surfaces
		// declare recordVerdictOptions.ProvenanceOverrideBasis. RFC 0126 P0 (D194)
		// is now adaptive inside insertVerdictRow (review_generation when owner
		// bundle 0009 exists, model identity when owner bundle 0024 exists). The
		// override path also routes through that helper and keeps posture='override'
		// (TestOverrideVerdictForcesOverridePostureAndStamp).
		// recordVerdict callers: HandleRecordVerdict, HandleSubmitReview (lane
		// surfaces behind the admission gate); applyVerdict caller: recordVerdict.
		"review.go": {applyVerdictCalls: 1, recordVerdictCalls: 2},
		// checkpoint.override clearing INSERT — operator surface,
		// posture='override' + decision-bound stamp
		// (TestCheckpointOverrideStampsClearingVerdict).
		"operator.go": {},
		// insertVerdictRow — the only direct verdict INSERT. All write surfaces
		// reach it with their already-classified provenance and declared/unknown
		// model identity stamp.
		"verdict_model_identity.go": {verdictInserts: 1},
		// #144 stale-lease sweep — lane-evidence transcription: truthful
		// unattested stamp, NO override basis; the completion gate disposes of it
		// (TestSweepAutoPublishReviewRecordsVerdictAndFiresDownstream).
		"recovery_auto.go": {applyVerdictCalls: 1},
		// recovery auto-finalize — recovery surface; MUST declare
		// ProvenanceOverrideBasis (TestRecordVerdictProvenanceOverrideBasisStampsOverride;
		// basis declaration asserted below).
		"recovery_auto_finalize.go": {recordVerdictCalls: 1},
		// durable verdict-artifact recovery — recovery surface; MUST declare an
		// explicit review_provenance override basis before calling applyVerdict
		// (covered by TestRecoveryCompleteStalledForceRecordsVerdictFromDurableSynthesisArtifact).
		"recovery_verdict_artifact.go": {applyVerdictCalls: 1},
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src := string(raw)
		got := surfaceCounts{
			verdictInserts:     strings.Count(src, "INSERT INTO striatumd.verdicts"),
			applyVerdictCalls:  strings.Count(src, "applyVerdict(") - strings.Count(src, "func applyVerdict("),
			recordVerdictCalls: strings.Count(src, "recordVerdict(") - strings.Count(src, "func recordVerdict("),
		}
		want := classified[name]
		if got != want {
			t.Errorf("%s: verdict-write surfaces %+v, classified %+v — a new/changed verdict writer must be classified in this test and stamp its provenance (RFC 0118 P0-2)", name, got, want)
		}
	}

	// The auto-finalize caller must keep declaring its override basis: without
	// it an auto-finalized verdict reads back as a clean neutral lane clear
	// (the run_45aa8852 bypass).
	autoFinalize, err := os.ReadFile("recovery_auto_finalize.go")
	if err != nil {
		t.Fatalf("read recovery_auto_finalize.go: %v", err)
	}
	if !strings.Contains(string(autoFinalize), "ProvenanceOverrideBasis:") {
		t.Errorf("recovery_auto_finalize.go no longer declares ProvenanceOverrideBasis on its recordVerdict call (RFC 0118 P0-2 mechanism 3)")
	}
	verdictArtifact, err := os.ReadFile("recovery_verdict_artifact.go")
	if err != nil {
		t.Fatalf("read recovery_verdict_artifact.go: %v", err)
	}
	if !strings.Contains(string(verdictArtifact), "\"review_provenance_override\": true") ||
		!strings.Contains(string(verdictArtifact), "\"review_provenance_basis\":") {
		t.Errorf("recovery_verdict_artifact.go must stamp recovery verdicts with review_provenance override basis (RFC 0118 P0-2 mechanism 3)")
	}
}
