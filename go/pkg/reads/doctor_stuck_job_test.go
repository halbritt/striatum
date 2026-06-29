package reads

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

type doctorStuckJobFakeRunner struct {
	doctorFakeRunner
	stuckRows []map[string]any
}

func (r *doctorStuckJobFakeRunner) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "WITH stuck AS"):
		return dashboardAllRowsFromMaps(r.stuckRows), nil
	case strings.Contains(sql, "COUNT(*) AS c"):
		return dashboardAllRowsFromMaps([]map[string]any{{"c": int64(0)}}), nil
	default:
		return dashboardAllRowsFromMaps(nil), nil
	}
}

// TestDoctorFlagsBlockedJobWithNoLiveSession (#389): a blocked job with no live
// session and no recent progress is surfaced as a WARNING naming `recovery
// reseal` — but doctor `ok` stays true (it is advisory, not a hard red).
func TestDoctorFlagsBlockedJobWithNoLiveSession(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * time.Minute)
	runner := &doctorStuckJobFakeRunner{stuckRows: []map[string]any{{
		"job_id":                   "job_design_codex",
		"run_id":                   "run_wedged",
		"workflow_job_id":          "design_codex",
		"role_id":                  "designer",
		"lane_id":                  "codex",
		"job_state":                "blocked",
		"lease_last_heartbeat_at":  nil,
		"started_at":               old.Format(time.RFC3339),
		"ready_at":                 old.Format(time.RFC3339),
		"live_session_count":       int64(0),
		"last_session_progress_at": nil,
	}}}

	block, warnings, records := doctorStuckJobsNoLiveSession(context.Background(), runner, "repo_1", now)
	if block["stuck_count"] != 1 {
		t.Fatalf("stuck_count = %#v, want 1; block=%#v", block["stuck_count"], block)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "job_stuck_no_live_session.job_design_codex") || !strings.Contains(warnings[0], "recovery reseal") {
		t.Fatalf("warnings = %#v, want one blocked-job warning naming recovery reseal", warnings)
	}
	if len(records) != 1 || records[0]["check"] != "job_stuck_no_live_session" || records[0]["job_state"] != "blocked" {
		t.Fatalf("records = %#v, want one blocked stuck-job record", records)
	}
}

// TestDoctorStuckJobNoFalsePositiveOnLiveSession: a job with a live (active)
// session on its lane is NEVER flagged, even if quiet — the lane is alive. This
// is the critical guard against false positives on healthy actively-running jobs.
func TestDoctorStuckJobNoFalsePositiveOnLiveSession(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * time.Minute)
	runner := &doctorStuckJobFakeRunner{stuckRows: []map[string]any{{
		"job_id":                   "job_running",
		"run_id":                   "run_healthy",
		"workflow_job_id":          "author_draft",
		"role_id":                  "author",
		"lane_id":                  "codex",
		"job_state":                "running",
		"lease_last_heartbeat_at":  old.Format(time.RFC3339),
		"started_at":               old.Format(time.RFC3339),
		"ready_at":                 old.Format(time.RFC3339),
		"live_session_count":       int64(1), // live session => healthy
		"last_session_progress_at": old.Format(time.RFC3339),
	}}}

	block, warnings, records := doctorStuckJobsNoLiveSession(context.Background(), runner, "repo_1", now)
	if block["stuck_count"] != 0 || len(warnings) != 0 || len(records) != 0 {
		t.Fatalf("a job with a live session must not be flagged: block=%#v warnings=%#v records=%#v", block, warnings, records)
	}
}

// TestDoctorStuckJobNoFalsePositiveOnRecentProgress: a sessionless running job
// that made progress WITHIN the bound is not flagged — only a genuinely quiet
// job trips the check.
func TestDoctorStuckJobNoFalsePositiveOnRecentProgress(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-2 * time.Minute) // within the 15m bound
	runner := &doctorStuckJobFakeRunner{stuckRows: []map[string]any{{
		"job_id":                   "job_running",
		"run_id":                   "run_healthy",
		"workflow_job_id":          "author_draft",
		"role_id":                  "author",
		"lane_id":                  "codex",
		"job_state":                "running",
		"lease_last_heartbeat_at":  fresh.Format(time.RFC3339),
		"started_at":               now.Add(-time.Hour).Format(time.RFC3339),
		"ready_at":                 nil,
		"live_session_count":       int64(0),
		"last_session_progress_at": nil,
	}}}

	block, warnings, records := doctorStuckJobsNoLiveSession(context.Background(), runner, "repo_1", now)
	if block["stuck_count"] != 0 || len(warnings) != 0 || len(records) != 0 {
		t.Fatalf("a job with recent lease heartbeat must not be flagged: block=%#v warnings=%#v records=%#v", block, warnings, records)
	}
}

func TestDoctorStuckJobSkipsLegiblyBlockedJobs(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * time.Minute)
	runner := &doctorStuckJobFakeRunner{stuckRows: []map[string]any{
		{
			"job_id":                      "job_open_blocker",
			"run_id":                      "run_legible",
			"workflow_job_id":             "draft",
			"role_id":                     "author",
			"lane_id":                     "codex",
			"job_state":                   "blocked",
			"lease_last_heartbeat_at":     nil,
			"started_at":                  old.Format(time.RFC3339),
			"ready_at":                    old.Format(time.RFC3339),
			"live_session_count":          int64(0),
			"last_session_progress_at":    nil,
			"active_blocker_count":        int64(1),
			"unfinished_dependency_count": int64(0),
		},
		{
			"job_id":                      "job_waiting_dependency",
			"run_id":                      "run_legible",
			"workflow_job_id":             "apply",
			"role_id":                     "author",
			"lane_id":                     "codex",
			"job_state":                   "blocked",
			"lease_last_heartbeat_at":     nil,
			"started_at":                  nil,
			"ready_at":                    old.Format(time.RFC3339),
			"live_session_count":          int64(0),
			"last_session_progress_at":    nil,
			"active_blocker_count":        int64(0),
			"unfinished_dependency_count": int64(1),
		},
		{
			"job_id":                      "job_silent_blocked",
			"run_id":                      "run_wedged",
			"workflow_job_id":             "review",
			"role_id":                     "reviewer",
			"lane_id":                     "codex",
			"job_state":                   "blocked",
			"lease_last_heartbeat_at":     nil,
			"started_at":                  old.Format(time.RFC3339),
			"ready_at":                    old.Format(time.RFC3339),
			"live_session_count":          int64(0),
			"last_session_progress_at":    nil,
			"active_blocker_count":        int64(0),
			"unfinished_dependency_count": int64(0),
		},
	}}

	block, warnings, records := doctorStuckJobsNoLiveSession(context.Background(), runner, "repo_1", now)
	if block["stuck_count"] != 1 || len(warnings) != 1 || len(records) != 1 {
		t.Fatalf("expected only silent blocked job to warn: block=%#v warnings=%#v records=%#v", block, warnings, records)
	}
	if records[0]["job_id"] != "job_silent_blocked" {
		t.Fatalf("records = %#v, want only job_silent_blocked", records)
	}
}

// TestDoctorStuckJobIsWarningNotHardRed: surfaced through HandleDoctor, the stuck
// job lands in `warnings` (and verbose `warning_records`) but `ok` stays true.
func TestDoctorStuckJobIsWarningNotHardRed(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-30 * time.Minute)
	runner := &doctorStuckJobFakeRunner{stuckRows: []map[string]any{{
		"job_id":                   "job_blocked",
		"run_id":                   "run_wedged",
		"workflow_job_id":          "design_codex",
		"role_id":                  "designer",
		"lane_id":                  "codex",
		"job_state":                "blocked",
		"lease_last_heartbeat_at":  nil,
		"started_at":               old.Format(time.RFC3339),
		"ready_at":                 nil,
		"live_session_count":       int64(0),
		"last_session_progress_at": nil,
	}}}

	result, err := HandleDoctor(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "verbose": true},
	})
	if err != nil {
		t.Fatalf("HandleDoctor: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("doctor ok = %v, want true (stuck-job is a warning, not a hard red); problems=%v", result["ok"], result["problems"])
	}
	warnings := strings.Join(result["warnings"].([]string), "\n")
	if !strings.Contains(warnings, "job_stuck_no_live_session.job_blocked") {
		t.Fatalf("warnings missing stuck-job warning:\n%s", warnings)
	}
	block, ok := result["job_stuck_no_live_session"].(map[string]any)
	if !ok || block["stuck_count"] != 1 {
		t.Fatalf("job_stuck_no_live_session block = %#v, want one stuck job", result["job_stuck_no_live_session"])
	}
	records := result["warning_records"].([]map[string]any)
	found := false
	for _, record := range records {
		if record["check"] == "job_stuck_no_live_session" && record["job_id"] == "job_blocked" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warning_records missing job_stuck_no_live_session: %#v", records)
	}
}

// TestDoctorStuckJobAgainstLiveSchema exercises the real query end-to-end and
// proves the two halves of the invariant against PostgreSQL: a sessionless,
// quiet, non-terminal job IS flagged, while a healthy job with a live session is
// NOT — even with identical quiet timestamps.
func TestDoctorStuckJobAgainstLiveSchema(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	now := time.Now().UTC()
	old := now.Add(-30 * time.Minute)
	repoID := "repo_doctor_stuck_job_pg"
	seedDoctorRecoveryCursorRepo(t, ctx, runner, repoID, now)
	runID := "run_stuck_job_pg"
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.runs (
		  repository_id, run_id, workflow_snapshot_id, repo_root, state, created_at, started_at
		) VALUES ($1,$2,$3,$4,'running',$5,$5)`,
		repoID, runID, "snap_"+repoID, "/tmp/"+repoID, old); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	// A blocked job with no session at all => stuck.
	seedStuckJob(t, ctx, runner, repoID, runID, "job_blocked", "design_codex", "designer", "codex", "blocked", old, "")
	// A running job whose lane HAS a live (active) session => healthy, not flagged.
	seedSession(t, ctx, runner, repoID, runID, "sess_live", "author", "claude", "active", old)
	seedStuckJob(t, ctx, runner, repoID, runID, "job_running_live", "author_draft", "author", "claude", "running", old, "")
	// A running job whose lane's only session is CLOSED => stuck.
	seedSession(t, ctx, runner, repoID, runID, "sess_dead", "reviewer", "agy", "closed", old)
	seedStuckJob(t, ctx, runner, repoID, runID, "job_running_dead", "review", "reviewer", "agy", "running", old, "")
	// A completed job => never considered.
	seedStuckJob(t, ctx, runner, repoID, runID, "job_done", "ledger", "scribe", "codex", "completed", old, "")

	block, warnings, records := doctorStuckJobsNoLiveSession(ctx, runner, repoID, now)
	flagged := map[string]bool{}
	for _, record := range records {
		flagged[stringFrom(record, "job_id")] = true
	}
	if !flagged["job_blocked"] {
		t.Fatalf("blocked sessionless job not flagged; warnings=%#v", warnings)
	}
	if !flagged["job_running_dead"] {
		t.Fatalf("running job with only a closed session not flagged; warnings=%#v", warnings)
	}
	if flagged["job_running_live"] {
		t.Fatalf("running job with a LIVE session was wrongly flagged (false positive); warnings=%#v", warnings)
	}
	if flagged["job_done"] {
		t.Fatalf("completed job was wrongly flagged; warnings=%#v", warnings)
	}
	if block["stuck_count"] != len(records) {
		t.Fatalf("stuck_count %#v != records %d", block["stuck_count"], len(records))
	}
}

func seedStuckJob(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, jobID, workflowJobID, role, lane, state string, ts time.Time, leaseID string) {
	t.Helper()
	laneSelector, err := db.JSONBArg(runner, map[string]any{"lane_id": lane})
	if err != nil {
		t.Fatalf("lane selector jsonb: %v", err)
	}
	var leaseArg any
	if leaseID != "" {
		leaseArg = leaseID
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, title, job_type, role_id,
		  lane_selector_json, state, attempt, max_attempts, idempotency_key,
		  created_at, ready_at, started_at, current_lease_id
		) VALUES ($1,$2,$3,$4,$5,'generic',$6,$7::jsonb,$8,1,1,$9,$10,$10,$10,$11)`,
		repoID, jobID, runID, workflowJobID, jobID, role, laneSelector, state, "idem_"+jobID, ts, leaseArg); err != nil {
		t.Fatalf("insert job %s: %v", jobID, err)
	}
}

func seedSession(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, sessionID, role, lane, state string, ts time.Time) {
	t.Helper()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.sessions (
		  repository_id, session_id, run_id, role_id, lane_id, slug, ordinal,
		  state, registered_at, last_heartbeat_at
		) VALUES ($1,$2,$3,$4,$5,$6,1,$7,$8,$8)`,
		repoID, sessionID, runID, role, lane, sessionID, state, ts); err != nil {
		t.Fatalf("insert session %s: %v", sessionID, err)
	}
}
