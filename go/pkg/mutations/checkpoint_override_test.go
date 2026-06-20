package mutations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// rpcErrorCode extracts the rpc.Error code from err, or "" if err is not an
// rpc.Error.
func rpcErrorCode(err error) string {
	var rpcErr *rpc.Error
	if errors.As(err, &rpcErr) {
		return rpcErr.Code
	}
	return ""
}

// seedDecisionArtifact inserts a run-level decision artifact plus the matching
// `decision.recorded` event (which carries the outcome the checkpoint override
// consults). decision.record normally writes both; tests seed them directly so
// no on-disk markdown is required.
func seedDecisionArtifact(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, decisionID, outcome string) (artifactID string) {
	t.Helper()
	now := time.Now().UTC()
	artifactID = "art_" + decisionID
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.artifacts (
		  repository_id, artifact_id, run_id, job_id, session_id,
		  logical_name, artifact_kind, repo_path, content_sha256,
		  size_bytes, publish_mode, created_at
		)
		VALUES ($1,$2,$3,NULL,NULL,$4,'decision',$5,'sha',1,'create',$6)`,
		repoID, artifactID, runID, decisionID, "decisions/"+decisionID+".md", now); err != nil {
		t.Fatalf("insert decision artifact: %v", err)
	}
	if _, err := appendEvent(ctx, runner, repoID, runID, "decision.recorded", nil, nil, nil, artifactID, nil, map[string]any{
		"decision_id": decisionID,
		"outcome":     outcome,
		"path":        "decisions/" + decisionID + ".md",
	}); err != nil {
		t.Fatalf("append decision.recorded event: %v", err)
	}
	return artifactID
}

// openCheckpointViaVerdict drives the no-cycle needs_revision path through the
// real verdict handler so the resulting revision_routing checkpoint and the
// stale needs_revision verdict row match production exactly. It returns the
// blocker id.
func openCheckpointViaVerdict(t *testing.T, ctx context.Context, runner db.Runner, repoID, reviewJobID, sessionID, leaseID string) string {
	t.Helper()
	result, err := HandleRecordVerdict(ctx, runner, intgEnv(repoID, map[string]any{
		"session_id": sessionID,
		"job_id":     reviewJobID,
		"lease_id":   leaseID,
		"verdict":    "needs_revision",
	}))
	if err != nil {
		t.Fatalf("record needs_revision: %v", err)
	}
	if fmt.Sprint(result["status"]) != "waiting_human" {
		t.Fatalf("status = %v, want waiting_human; result=%#v", result["status"], result)
	}
	blockerID := fmt.Sprint(result["blocker_id"])
	if blockerID == "" || result["blocker_id"] == nil {
		t.Fatalf("expected blocker_id, got %#v", result)
	}
	return blockerID
}

// F2 (issue #63): checkpoint.resolve override with a valid run-level accepting
// decision resolves the revision_routing checkpoint, completes the review job,
// records a superseding clearing verdict (latestVerdict == accept_with_findings)
// and makes the previously-blocked downstream job reachable.
func TestCheckpointOverrideClearsRevisionRoutingAndUnblocksDownstream(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_ckpt_override_ok"
	runID, reviewJobID, _, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, []any{})

	// Wire an implement job gated on the review accepting.
	implementJobID := "job_impl_" + repoID
	seedBlockedJob(t, ctx, runner, repoID, runID, implementJobID, "implement", "generic", "synthesizer")
	seedDependency(t, ctx, runner, repoID, implementJobID, reviewJobID, map[string]any{
		"on": "completed", "from": "review_design_codex", "to": "implement",
		"requires_verdict": []any{"accept", "accept_with_findings"},
	})

	blockerID := openCheckpointViaVerdict(t, ctx, runner, repoID, reviewJobID, sessionID, leaseID)

	// Sanity: the stale needs_revision is the latest verdict and the gate is shut.
	if latest, _ := latestVerdict(ctx, runner, repoID, reviewJobID); latest != "needs_revision" {
		t.Fatalf("pre-override latest verdict = %q, want needs_revision", latest)
	}

	decisionID := "dec_accept_override"
	artifactID := seedDecisionArtifact(t, ctx, runner, repoID, runID, decisionID, "accepted")

	result, err := HandleCheckpointResolve(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id":  blockerID,
		"action":      "override",
		"decision_id": decisionID,
	}))
	if err != nil {
		t.Fatalf("checkpoint override: %v", err)
	}
	if fmt.Sprint(result["action"]) != "override" {
		t.Fatalf("action = %v, want override", result["action"])
	}
	if fmt.Sprint(result["decision_id"]) != decisionID {
		t.Fatalf("decision_id = %v, want %s", result["decision_id"], decisionID)
	}
	if fmt.Sprint(result["decision_artifact_id"]) != artifactID {
		t.Fatalf("decision_artifact_id = %v, want %s", result["decision_artifact_id"], artifactID)
	}

	// The checkpoint is resolved.
	if n := openRevisionBlockers(t, ctx, runner, repoID, reviewJobID); n != 0 {
		t.Fatalf("expected checkpoint resolved (0 open), got %d", n)
	}
	// The review job is completed (settled by decision, not re-run).
	if got := jobState(t, ctx, runner, repoID, reviewJobID); got != "completed" {
		t.Fatalf("review state = %q, want completed", got)
	}
	// A superseding clearing verdict is now the latest verdict.
	latest, err := latestVerdict(ctx, runner, repoID, reviewJobID)
	if err != nil {
		t.Fatalf("latest verdict: %v", err)
	}
	if latest != "accept_with_findings" {
		t.Fatalf("latest verdict = %q, want accept_with_findings (superseding clear)", latest)
	}
	// Both verdict rows persist (stale needs_revision preserved for audit).
	if n := countVerdicts(t, ctx, runner, repoID, reviewJobID); n != 2 {
		t.Fatalf("verdict count = %d, want 2 (stale needs_revision + superseding clear)", n)
	}
	// The clearing verdict carries posture=override.
	postureRow, err := oneRow(ctx, runner, `
		SELECT posture FROM striatumd.verdicts
		 WHERE repository_id = $1 AND job_id = $2
		 ORDER BY created_at DESC, verdict_id DESC LIMIT 1`, repoID, reviewJobID)
	if err != nil {
		t.Fatalf("select latest posture: %v", err)
	}
	if fmt.Sprint(postureRow["posture"]) != "override" {
		t.Fatalf("latest posture = %q, want override", postureRow["posture"])
	}
	// The previously-blocked downstream becomes reachable (queued).
	if got := jobState(t, ctx, runner, repoID, implementJobID); got != "queued" {
		t.Fatalf("implement state = %q, want queued (downstream unblocked)", got)
	}
	// downstream_jobs in the response lists the implement job.
	downstream, ok := result["downstream_jobs"].([]map[string]any)
	if !ok || len(downstream) != 1 {
		t.Fatalf("downstream_jobs = %#v, want 1 entry", result["downstream_jobs"])
	}
	if fmt.Sprint(downstream[0]["job_id"]) != implementJobID {
		t.Fatalf("downstream job = %v, want %s", downstream[0]["job_id"], implementJobID)
	}
	// A checkpoint.resolved event records the supersession.
	evtRow, err := oneRow(ctx, runner, `
		SELECT count(*) AS n FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND event_type = 'checkpoint.resolved'
		   AND payload_json->>'action' = 'override'
		   AND payload_json->>'superseded_verdict' = 'needs_revision'`, repoID, runID)
	if err != nil {
		t.Fatalf("count override events: %v", err)
	}
	if intValue(evtRow["n"]) != 1 {
		t.Fatalf("expected 1 override checkpoint.resolved event, got %v", evtRow["n"])
	}
	// #505: resolving the checkpoint left the run `running` with a now-claimable
	// downstream job, but the detached `run drive` unit exited when the run hit
	// the waiting_human checkpoint (IsTerminalRunState treats waiting_human as
	// drive-terminal) and is not re-armed here. next_actions must name the
	// re-drive verb so the run does not silently stall until a manual re-drive.
	if got := fmt.Sprint(result["run_state"]); got != "running" {
		t.Fatalf("run_state = %q, want running (checkpoint left claimable work)", got)
	}
	if !nextActionsContainRunDrive(result) {
		t.Fatalf("next_actions = %#v, want a run-drive re-arm hint (#505)", result["next_actions"])
	}
}

// nextActionsContainRunDrive reports whether the handler's next_actions advise
// re-arming the detached driver (`run drive`). #505: without this the operator
// gets no signal that the resolved run needs a driver and it silently stalls.
func nextActionsContainRunDrive(result map[string]any) bool {
	actions, ok := result["next_actions"].([]string)
	if !ok {
		return false
	}
	for _, a := range actions {
		if strings.Contains(a, "run drive") {
			return true
		}
	}
	return false
}

// override without decision_id is a schema_invalid error.
func TestCheckpointOverrideRequiresDecisionID(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_ckpt_override_no_decision"
	_, reviewJobID, _, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, []any{})
	blockerID := openCheckpointViaVerdict(t, ctx, runner, repoID, reviewJobID, sessionID, leaseID)

	_, err := HandleCheckpointResolve(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id": blockerID,
		"action":     "override",
	}))
	if err == nil {
		t.Fatal("expected override without decision_id to be rejected")
	}
	if code := rpcErrorCode(err); code != "schema_invalid" {
		t.Fatalf("error code = %q, want schema_invalid", code)
	}
	// The checkpoint is untouched.
	if n := openRevisionBlockers(t, ctx, runner, repoID, reviewJobID); n != 1 {
		t.Fatalf("expected checkpoint still open, got %d", n)
	}
}

// override with a non-accepting decision outcome is rejected (invalid_transition).
func TestCheckpointOverrideRejectsNonAcceptingDecision(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_ckpt_override_rejected_decision"
	runID, reviewJobID, _, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, []any{})
	blockerID := openCheckpointViaVerdict(t, ctx, runner, repoID, reviewJobID, sessionID, leaseID)

	decisionID := "dec_rejected"
	seedDecisionArtifact(t, ctx, runner, repoID, runID, decisionID, "rejected")

	_, err := HandleCheckpointResolve(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id":  blockerID,
		"action":      "override",
		"decision_id": decisionID,
	}))
	if err == nil {
		t.Fatal("expected override with rejected decision to be rejected")
	}
	if code := rpcErrorCode(err); code != "invalid_transition" {
		t.Fatalf("error code = %q, want invalid_transition", code)
	}
	if got := jobState(t, ctx, runner, repoID, reviewJobID); got != "waiting_human" {
		t.Fatalf("review state = %q, want waiting_human (unchanged)", got)
	}
}

// override referencing a job-bound (non run-level) decision is rejected.
func TestCheckpointOverrideRejectsJobBoundDecision(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_ckpt_override_jobbound"
	runID, reviewJobID, _, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, []any{})
	blockerID := openCheckpointViaVerdict(t, ctx, runner, repoID, reviewJobID, sessionID, leaseID)

	// A decision artifact bound to the review job (not run-level).
	now := time.Now().UTC()
	decisionID := "dec_jobbound"
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.artifacts (
		  repository_id, artifact_id, run_id, job_id, session_id,
		  logical_name, artifact_kind, repo_path, content_sha256,
		  size_bytes, publish_mode, created_at
		)
		VALUES ($1,$2,$3,$4,NULL,$5,'decision',$6,'sha',1,'create',$7)`,
		repoID, "art_"+decisionID, runID, reviewJobID, decisionID, "decisions/"+decisionID+".md", now); err != nil {
		t.Fatalf("insert job-bound decision artifact: %v", err)
	}

	_, err := HandleCheckpointResolve(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id":  blockerID,
		"action":      "override",
		"decision_id": decisionID,
	}))
	if err == nil {
		t.Fatal("expected override with job-bound decision to be rejected")
	}
	if code := rpcErrorCode(err); code != "invalid_transition" {
		t.Fatalf("error code = %q, want invalid_transition", code)
	}
}

// override referencing a missing decision is rejected (not found).
func TestCheckpointOverrideRejectsMissingDecision(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_ckpt_override_missing"
	_, reviewJobID, _, sessionID, leaseID := revisionFixture(t, ctx, runner, repoID, []any{})
	blockerID := openCheckpointViaVerdict(t, ctx, runner, repoID, reviewJobID, sessionID, leaseID)

	_, err := HandleCheckpointResolve(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id":  blockerID,
		"action":      "override",
		"decision_id": "dec_does_not_exist",
	}))
	if err == nil {
		t.Fatal("expected override with missing decision to be rejected")
	}
}

// override is only valid on a revision_routing checkpoint; a human_checkpoint
// blocker of a different kind is rejected.
func TestCheckpointOverrideRejectsNonRevisionRoutingCheckpoint(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_ckpt_override_wrong_kind"
	runID, reviewJobID, _, _, _ := revisionFixture(t, ctx, runner, repoID, []any{})

	// Seed a human_checkpoint blocker of a different kind (operator_decision)
	// on the review job, leaving the job in waiting_human.
	now := time.Now().UTC()
	blockerID := "blk_operator_decision_" + repoID
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.blockers (
		  repository_id, blocker_id, run_id, job_id, session_id, severity,
		  blocker_kind, description, state, created_at
		)
		VALUES ($1,$2,$3,$4,NULL,'human_checkpoint','operator_decision','needs a call','open',$5)`,
		repoID, blockerID, runID, reviewJobID, now); err != nil {
		t.Fatalf("insert operator_decision blocker: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET state = 'waiting_human', current_lease_id = NULL
		 WHERE repository_id = $1 AND job_id = $2`, repoID, reviewJobID); err != nil {
		t.Fatalf("set review waiting_human: %v", err)
	}

	decisionID := "dec_accept_wrong_kind"
	seedDecisionArtifact(t, ctx, runner, repoID, runID, decisionID, "accepted")

	_, err := HandleCheckpointResolve(ctx, runner, intgEnv(repoID, map[string]any{
		"blocker_id":  blockerID,
		"action":      "override",
		"decision_id": decisionID,
	}))
	if err == nil {
		t.Fatal("expected override on non-revision_routing checkpoint to be rejected")
	}
	if code := rpcErrorCode(err); code != "invalid_transition" {
		t.Fatalf("error code = %q, want invalid_transition", code)
	}
}
