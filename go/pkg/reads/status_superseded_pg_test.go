package reads

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// #283: after recovery supersedes a stale non-accepting review verdict (the
// `recovery invalidate-job ... <decision_id>` path sets
// verdicts.superseded_by_decision_id), status' latest_non_accepting_review_verdicts
// must drop the superseded row. Otherwise a completed run with a later
// accepting verdict still looks like it needs a revision cycle. A companion
// assertion verifies a NON-superseded needs_revision verdict still appears, so
// the filter is not vacuous.
func TestStatusExcludesSupersededNonAcceptingVerdicts(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_superseded_status"
	now := time.Date(2026, 6, 14, 9, 0, 0, 0, time.UTC)
	crSeedRepo(t, ctx, runner, repoID, now)

	// Run A: a review job whose attempt-1 needs_revision verdict was superseded
	// by a recovery decision, with a later attempt-2 accept verdict. This run is
	// resolved and must NOT surface as still-needs-revision.
	supersededRunID := "run_superseded"
	crSeedRun(t, ctx, runner, repoID, supersededRunID, "striatum/superseded", now, false, nil)
	seedReviewJobWithVerdict(t, ctx, runner, statusVerdictFixture{
		repoID:                 repoID,
		runID:                  supersededRunID,
		jobID:                  "job_review_superseded",
		workflowJobID:          "review",
		sessionID:              "sess_review_superseded_1",
		verdictID:              "verdict_superseded_a1",
		verdict:                "needs_revision",
		supersededByDecisionID: "dec_recovery_283",
		createdAt:              now,
	})
	seedReviewJobVerdictOnly(t, ctx, runner, statusVerdictFixture{
		repoID:    repoID,
		runID:     supersededRunID,
		jobID:     "job_review_superseded",
		sessionID: "sess_review_superseded_2",
		ordinal:   2,
		verdictID: "verdict_superseded_a2_accept",
		verdict:   "accept",
		createdAt: now.Add(time.Hour),
	})

	// Run B: a review job with a live, non-superseded needs_revision verdict.
	// This run genuinely still needs a revision cycle and MUST surface.
	liveRunID := "run_live_revision"
	crSeedRun(t, ctx, runner, repoID, liveRunID, "striatum/live-revision", now, false, nil)
	seedReviewJobWithVerdict(t, ctx, runner, statusVerdictFixture{
		repoID:        repoID,
		runID:         liveRunID,
		jobID:         "job_review_live",
		workflowJobID: "review",
		sessionID:     "sess_review_live_1",
		verdictID:     "verdict_live_b1",
		verdict:       "needs_revision",
		createdAt:     now,
	})

	assertNonAccepting := func(t *testing.T, runID string, wantJobIDs map[string]bool) {
		t.Helper()
		params := map[string]any{"repository_id": repoID}
		if runID != "" {
			params["run_id"] = runID
		}
		result, err := HandleStatus(ctx, runner, rpc.Envelope{Params: params})
		if err != nil {
			t.Fatalf("HandleStatus(run=%q): %v", runID, err)
		}
		rows, ok := result["latest_non_accepting_review_verdicts"].([]map[string]any)
		if !ok {
			t.Fatalf("latest_non_accepting_review_verdicts = %#v, want []map[string]any",
				result["latest_non_accepting_review_verdicts"])
		}
		got := map[string]bool{}
		for _, row := range rows {
			got[fmt.Sprint(row["job_id"])] = true
		}
		for jobID, want := range wantJobIDs {
			if got[jobID] != want {
				t.Fatalf("run=%q: job %q present=%v, want %v (rows=%#v)",
					runID, jobID, got[jobID], want, rows)
			}
		}
	}

	// Run-scoped: the superseded run shows no non-accepting verdict.
	assertNonAccepting(t, supersededRunID, map[string]bool{
		"job_review_superseded": false,
	})
	// Run-scoped: the live run still shows its needs_revision verdict.
	assertNonAccepting(t, liveRunID, map[string]bool{
		"job_review_live": true,
	})
	// Repo-wide: superseded excluded, live included (filter is not vacuous).
	assertNonAccepting(t, "", map[string]bool{
		"job_review_superseded": false,
		"job_review_live":       true,
	})
}

type statusVerdictFixture struct {
	repoID                 string
	runID                  string
	jobID                  string
	workflowJobID          string
	sessionID              string
	ordinal                int
	verdictID              string
	verdict                string
	supersededByDecisionID string
	// rationale defaults to "rev" when empty (preserving existing callers); set it
	// to exercise the #506 rationale_excerpt legibility projection.
	rationale string
	// findingsArtifactID, when set, links the verdict to a findings artifact so the
	// #506 findings_hint surfaces (the verdict's FK requires the artifact to exist;
	// callers that set this must seed the artifact row first).
	findingsArtifactID string
	createdAt          time.Time
}

// seedReviewJobWithVerdict inserts a review job (its own job row + author-distinct
// review session) and one verdict against it.
func seedReviewJobWithVerdict(t *testing.T, ctx context.Context, runner db.Runner, f statusVerdictFixture) {
	t.Helper()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, idempotency_key, write_scope_json, expected_artifacts_json,
		  lane_selector_json, created_at
		) VALUES ($1,$2,$3,$4,1,'completed','reviewer','Review','review',$5,
		         '{"mode":"document_only","repo_write":false,"allowed_paths":[]}'::jsonb,
		         '[]'::jsonb,'{"lane_id":"agy"}'::jsonb,$6)`,
		f.repoID, f.jobID, f.runID, f.workflowJobID, "idem_"+f.jobID, f.createdAt); err != nil {
		t.Fatalf("seed review job %s: %v", f.jobID, err)
	}
	seedReviewJobVerdictOnly(t, ctx, runner, f)
}

// seedReviewJobVerdictOnly inserts a review session and one verdict against an
// already-seeded job (used for a second verdict on the same job_id, which the
// verdicts UNIQUE(repository_id, job_id, session_id) constraint requires to use
// a distinct session).
func seedReviewJobVerdictOnly(t *testing.T, ctx context.Context, runner db.Runner, f statusVerdictFixture) {
	t.Helper()
	ordinal := f.ordinal
	if ordinal == 0 {
		ordinal = 1
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.sessions (
		  repository_id, session_id, run_id, role_id, lane_id, slug, ordinal,
		  capabilities_json, state, operator_label, registered_at
		) VALUES ($1,$2,$3,'reviewer','agy',$4,$5,'["claim","review"]'::jsonb,'active','codex',$6)`,
		f.repoID, f.sessionID, f.runID, f.sessionID, ordinal, f.createdAt); err != nil {
		t.Fatalf("seed review session %s: %v", f.sessionID, err)
	}
	var superseded any
	if f.supersededByDecisionID != "" {
		superseded = f.supersededByDecisionID
	}
	rationale := f.rationale
	if rationale == "" {
		rationale = "rev"
	}
	var findingsArtifactID any
	if f.findingsArtifactID != "" {
		findingsArtifactID = f.findingsArtifactID
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.verdicts (
		  repository_id, verdict_id, run_id, job_id, session_id, verdict,
		  rationale, created_at, posture, superseded_by_decision_id, findings_artifact_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'neutral',$9,$10)`,
		f.repoID, f.verdictID, f.runID, f.jobID, f.sessionID, f.verdict,
		rationale, f.createdAt, superseded, findingsArtifactID); err != nil {
		t.Fatalf("seed verdict %s: %v", f.verdictID, err)
	}
}
