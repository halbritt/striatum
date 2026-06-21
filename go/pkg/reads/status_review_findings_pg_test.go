package reads

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// #506: a blocking review's actual objections (its REVIEW.md finding, commonly
// published placement=blob_exhaust — off the run branch and absent from
// `evidence export` bodies) were not surfaced by `striatum status`. The operator
// adjudicating a needs_revision (override vs. another cycle) had to reconstruct the
// reviewer's reasoning from the PTY trajectory. status now projects the verdict's
// rationale, a bounded rationale_excerpt, the linked findings_artifact_id, and a
// findings_hint naming the exact `artifact get-content` read, and inlines the
// reviewer's reasoning into the recovery_action — so the reviewer's objection is
// readable from status itself.
func TestStatusSurfacesReviewFindingsForNonAcceptingVerdict(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_review_findings_506"
	now := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)
	crSeedRepo(t, ctx, runner, repoID, now)
	runID := "run_review_findings_506"
	crSeedRun(t, ctx, runner, repoID, runID, "striatum/review-findings", now, false, nil)

	// The build job the review covers is the one crSeedRun already seeded
	// (workflow_job_id "build", job id job_<runID>). Seed a findings artifact the
	// verdict links to (the verdict FK requires the artifact to exist); it stands in
	// for the blob_exhaust REVIEW.md the operator could not previously read.
	buildJobID := "job_" + runID
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.artifacts (
		  repository_id, artifact_id, run_id, job_id, logical_name, artifact_kind,
		  repo_path, content_sha256, size_bytes, publish_mode, created_at, attempt
		) VALUES ($1,$2,$3,$4,'review','finding','docs/REVIEW.md',$5,42,'create',$6,1)`,
		repoID, "art_finding_506", runID, buildJobID, "sha_finding_506", now); err != nil {
		t.Fatalf("seed finding artifact: %v", err)
	}

	longRationale := "The slice does not handle the empty-input case, the new " +
		"predicate races the recovery sweep on the per-run advisory lock, and the " +
		"added test asserts the wrong column so it passes vacuously. " +
		strings.Repeat("Additional detail that pushes this rationale past the inline excerpt limit. ", 6)

	seedReviewJobWithVerdict(t, ctx, runner, statusVerdictFixture{
		repoID: repoID, runID: runID, jobID: "job_review_506", workflowJobID: "review_codex",
		sessionID: "sess_review_506", verdictID: "verdict_506", verdict: "needs_revision",
		rationale: longRationale, findingsArtifactID: "art_finding_506",
		createdAt: now, ordinal: 1,
	})
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.job_dependencies (repository_id, job_id, depends_on_job_id)
		VALUES ($1,$2,$3)`, repoID, "job_review_506", buildJobID); err != nil {
		t.Fatalf("seed dependency: %v", err)
	}

	result, err := HandleStatus(ctx, runner, rpc.Envelope{Params: map[string]any{"repository_id": repoID, "run_id": runID}})
	if err != nil {
		t.Fatalf("HandleStatus: %v", err)
	}
	rows, ok := result["latest_non_accepting_review_verdicts"].([]map[string]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("latest_non_accepting_review_verdicts = %#v", result["latest_non_accepting_review_verdicts"])
	}
	var row map[string]any
	for _, r := range rows {
		if fmt.Sprint(r["job_id"]) == "job_review_506" {
			row = r
			break
		}
	}
	if row == nil {
		t.Fatalf("review verdict not surfaced; rows=%#v", rows)
	}

	// The linked finding id is projected so a programmatic consumer (dashboard / web
	// UI) can route to the body.
	if got := fmt.Sprint(row["findings_artifact_id"]); got != "art_finding_506" {
		t.Fatalf("findings_artifact_id = %q, want art_finding_506", got)
	}
	// The findings_hint names the exact read that fetches the body.
	hint := fmt.Sprint(row["findings_hint"])
	if !strings.Contains(hint, "artifact get-content art_finding_506") {
		t.Fatalf("findings_hint = %q, want it to name `artifact get-content art_finding_506`", hint)
	}
	// The rationale excerpt is the bounded, single-line head of the rationale.
	excerpt, _ := row["rationale_excerpt"].(string)
	if excerpt == "" {
		t.Fatalf("rationale_excerpt empty; row=%#v", row)
	}
	if len(excerpt) > reviewRationaleExcerptLimit+len("…") {
		t.Fatalf("rationale_excerpt len=%d exceeds bound %d: %q", len(excerpt), reviewRationaleExcerptLimit, excerpt)
	}
	if !strings.HasSuffix(excerpt, "…") {
		t.Fatalf("rationale_excerpt should be truncated with an ellipsis: %q", excerpt)
	}
	if strings.Contains(excerpt, "\n") {
		t.Fatalf("rationale_excerpt should be single-line: %q", excerpt)
	}
	if !strings.HasPrefix(excerpt, "The slice does not handle the empty-input case") {
		t.Fatalf("rationale_excerpt should be the head of the rationale: %q", excerpt)
	}
	// The recovery_action inlines both the reviewer's reasoning and the fetch hint,
	// so the operator sees WHY in the same line that tells them how to recover.
	action := fmt.Sprint(row["recovery_action"])
	if !strings.Contains(action, "Reviewer said:") || !strings.Contains(action, "artifact get-content art_finding_506") {
		t.Fatalf("recovery_action = %q, want it to inline the reviewer reasoning + fetch hint", action)
	}
}

// Negative control: a non-accepting verdict with no linked finding and an empty
// rationale gets NO findings_hint and NO rationale_excerpt — the legibility fields
// are not vacuously present.
func TestStatusReviewFindingsAbsentWhenNoRationaleOrFinding(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_review_findings_506_neg"
	now := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	crSeedRepo(t, ctx, runner, repoID, now)
	runID := "run_review_findings_506_neg"
	crSeedRun(t, ctx, runner, repoID, runID, "striatum/review-findings-neg", now, false, nil)

	seedReviewJobWithVerdict(t, ctx, runner, statusVerdictFixture{
		repoID: repoID, runID: runID, jobID: "job_review_neg", workflowJobID: "review_codex",
		sessionID: "sess_review_neg", verdictID: "verdict_neg", verdict: "needs_revision",
		rationale: "   ", // whitespace-only → no excerpt
		createdAt: now, ordinal: 1,
	})

	result, err := HandleStatus(ctx, runner, rpc.Envelope{Params: map[string]any{"repository_id": repoID, "run_id": runID}})
	if err != nil {
		t.Fatalf("HandleStatus: %v", err)
	}
	rows, _ := result["latest_non_accepting_review_verdicts"].([]map[string]any)
	if len(rows) == 0 {
		t.Fatalf("expected the non-accepting verdict to surface; rows=%#v", rows)
	}
	row := rows[0]
	if _, present := row["findings_hint"]; present {
		t.Fatalf("findings_hint should be absent with no linked finding; row=%#v", row)
	}
	if _, present := row["rationale_excerpt"]; present {
		t.Fatalf("rationale_excerpt should be absent with a whitespace-only rationale; row=%#v", row)
	}
	// The recovery_action still carries the ordinary non-accepting guidance, with no
	// dangling "Reviewer said:" clause.
	action := fmt.Sprint(row["recovery_action"])
	if strings.Contains(action, "Reviewer said:") || strings.Contains(action, "artifact get-content") {
		t.Fatalf("recovery_action = %q, want no findings clause when none is available", action)
	}
}
