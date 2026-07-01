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

type doctorRecoveryCursorFakeRunner struct {
	doctorFakeRunner
	recoveryRows []map[string]any
	queries      []string
}

func (r *doctorRecoveryCursorFakeRunner) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	r.queries = append(r.queries, sql)
	switch {
	case strings.Contains(sql, "striatumd.scheduler_cursors"):
		return dashboardAllRowsFromMaps(r.recoveryRows), nil
	case strings.Contains(sql, "COUNT(*) AS c"):
		return dashboardAllRowsFromMaps([]map[string]any{{"c": int64(0)}}), nil
	default:
		return dashboardAllRowsFromMaps(nil), nil
	}
}

func TestHandleDoctorFlagsRecoverySweepCursorWedgedClaimableRun(t *testing.T) {
	now := time.Date(2026, 6, 17, 22, 50, 0, 0, time.UTC)
	quietSince := now.Add(-10 * time.Minute)
	runner := &doctorRecoveryCursorFakeRunner{recoveryRows: []map[string]any{{
		"run_id":                "run_wedged",
		"run_state":             "running",
		"cursor_state":          "active",
		"last_sweep_at":         now.Add(-time.Minute),
		"claimable_job_count":   int64(2),
		"last_lane_advanced_at": quietSince.Format(time.RFC3339),
		"quiet_since":           quietSince,
	}}}

	result, err := HandleDoctor(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "verbose": true},
	})
	if err != nil {
		t.Fatalf("HandleDoctor: %v", err)
	}
	if result["ok"] == true {
		t.Fatalf("doctor ok = true, want false")
	}
	if result["availability_ok"] == true {
		t.Fatalf("availability_ok = true, want false")
	}
	if result["provenance_ok"] != true {
		t.Fatalf("provenance_ok = %v, want true", result["provenance_ok"])
	}
	problems := strings.Join(result["problems"].([]string), "\n")
	if !strings.Contains(problems, "recovery_sweep_cursor_wedged.run_wedged") {
		t.Fatalf("problems missing recovery cursor wedge:\n%s", problems)
	}
	block := result["recovery_sweep_cursor"].(map[string]any)
	if block["wedged_count"] != 1 {
		t.Fatalf("recovery_sweep_cursor block = %#v, want one wedged run", block)
	}
	records := result["problem_records"].([]map[string]any)
	found := false
	for _, record := range records {
		if record["check"] == "recovery_sweep_cursor_wedged" && record["run_id"] == "run_wedged" {
			if record["plane"] != doctorProblemPlaneAvailability {
				t.Fatalf("recovery_sweep_cursor_wedged plane = %v, want %s", record["plane"], doctorProblemPlaneAvailability)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("problem_records missing recovery_sweep_cursor_wedged: %#v", records)
	}
}

func TestDoctorRecoverySweepCursorQuietWhenNotActionablyWedged(t *testing.T) {
	now := time.Date(2026, 6, 17, 22, 50, 0, 0, time.UTC)
	old := now.Add(-10 * time.Minute)
	fresh := now.Add(-time.Minute)
	runner := &doctorRecoveryCursorFakeRunner{recoveryRows: []map[string]any{
		{
			"run_id":                "run_no_claimable",
			"run_state":             "running",
			"cursor_state":          "active",
			"last_sweep_at":         now,
			"claimable_job_count":   int64(0),
			"last_lane_advanced_at": old.Format(time.RFC3339),
			"quiet_since":           old,
		},
		{
			"run_id":                "run_paused",
			"run_state":             "paused",
			"cursor_state":          "active",
			"last_sweep_at":         now,
			"claimable_job_count":   int64(3),
			"last_lane_advanced_at": old.Format(time.RFC3339),
			"quiet_since":           old,
		},
		{
			"run_id":                "run_fresh",
			"run_state":             "running",
			"cursor_state":          "active",
			"last_sweep_at":         now,
			"claimable_job_count":   int64(1),
			"last_lane_advanced_at": fresh.Format(time.RFC3339),
			"quiet_since":           fresh,
		},
	}}

	block, problems, records := doctorRecoverySweepCursor(context.Background(), runner, "repo_1", now)
	if len(problems) != 0 || len(records) != 0 {
		t.Fatalf("quiet rows produced problems=%#v records=%#v", problems, records)
	}
	if block["wedged_count"] != 0 {
		t.Fatalf("block = %#v, want no wedged runs", block)
	}
}

func TestDoctorRecoverySweepCursorFlagsLatchReadFailure(t *testing.T) {
	now := time.Date(2026, 6, 17, 22, 50, 0, 0, time.UTC)
	runner := &doctorRecoveryCursorFakeRunner{recoveryRows: []map[string]any{{
		"run_id":                      "run_latch_error",
		"run_state":                   "running",
		"cursor_state":                "sweep_degraded",
		"last_sweep_at":               now,
		"claimable_job_count":         int64(0),
		"recovery_cursor_latch_error": "latch read failed",
		"started_at":                  now,
		"created_at":                  now,
	}}}

	block, problems, records := doctorRecoverySweepCursor(context.Background(), runner, "repo_1", now)
	if len(problems) != 1 || !strings.Contains(problems[0], "recovery_sweep_cursor_latch_error.run_latch_error") {
		t.Fatalf("problems = %#v, want latch error problem", problems)
	}
	if block["latch_error_count"] != 1 {
		t.Fatalf("block = %#v, want one latch error", block)
	}
	if len(records) != 1 || records[0]["check"] != "recovery_sweep_cursor_latch_error" {
		t.Fatalf("records = %#v, want latch error record", records)
	}
}

func TestDoctorRecoverySweepCursorFlagsTrippedRunWithRecoveryCommand(t *testing.T) {
	now := time.Date(2026, 7, 1, 22, 50, 0, 0, time.UTC)
	runner := &doctorRecoveryCursorFakeRunner{recoveryRows: []map[string]any{{
		"run_id":                               "run_trip",
		"run_state":                            "needs_operator",
		"cursor_state":                         "sweep_degraded",
		"last_sweep_at":                        now,
		"recovery_sweep_trip_state":            "tripped",
		"recovery_degraded_sweep_count":        int64(3),
		"recovery_sweep_trip_threshold":        int64(3),
		"recovery_sweep_trip_error":            "synthetic sweep failure",
		"recovery_sweep_trip_blocker_id":       "blk_trip",
		"recovery_sweep_trip_recovery_command": "striatum escalation resolve blk_trip",
		"started_at":                           now,
		"created_at":                           now,
	}}}

	block, problems, records := doctorRecoverySweepCursor(context.Background(), runner, "repo_1", now)
	if len(problems) != 1 || !strings.Contains(problems[0], "recovery_sweep_cursor_tripped.run_trip") {
		t.Fatalf("problems = %#v, want tripped cursor problem", problems)
	}
	if !strings.Contains(problems[0], "striatum escalation resolve blk_trip") {
		t.Fatalf("problem missing recovery command: %#v", problems)
	}
	if block["tripped_count"] != 1 {
		t.Fatalf("block = %#v, want one tripped run", block)
	}
	if len(records) != 1 || records[0]["check"] != "recovery_sweep_cursor_tripped" {
		t.Fatalf("records = %#v, want tripped record", records)
	}
}

func TestDoctorRecoverySweepCursorUsesPersistedCursorJSON(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	now := time.Date(2026, 6, 17, 22, 50, 0, 0, time.UTC)
	repoID := "repo_doctor_recovery_cursor_pg"
	otherRepoID := "repo_doctor_recovery_cursor_other"
	seedDoctorRecoveryCursorRepo(t, ctx, runner, repoID, now)
	seedDoctorRecoveryCursorRepo(t, ctx, runner, otherRepoID, now)
	old := now.Add(-10 * time.Minute)
	fresh := now.Add(-1 * time.Minute)
	seedDoctorRecoveryCursorRun(t, ctx, runner, repoID, "run_wedged", "running", "active", old, map[string]any{
		"claimable_job_count":   2,
		"last_lane_advanced_at": old.Format(time.RFC3339),
	})
	seedDoctorRecoveryCursorRun(t, ctx, runner, repoID, "run_no_claimable", "running", "active", old, map[string]any{
		"claimable_job_count":   0,
		"last_lane_advanced_at": old.Format(time.RFC3339),
	})
	seedDoctorRecoveryCursorRun(t, ctx, runner, repoID, "run_removed_cursor", "running", "removed", old, map[string]any{
		"claimable_job_count":   3,
		"last_lane_advanced_at": old.Format(time.RFC3339),
	})
	seedDoctorRecoveryCursorRun(t, ctx, runner, repoID, "run_completed", "completed", "active", old, map[string]any{
		"claimable_job_count":   4,
		"last_lane_advanced_at": old.Format(time.RFC3339),
	})
	seedDoctorRecoveryCursorRun(t, ctx, runner, repoID, "run_fresh", "running", "active", old, map[string]any{
		"claimable_job_count":   1,
		"last_lane_advanced_at": fresh.Format(time.RFC3339),
	})
	seedDoctorRecoveryCursorRun(t, ctx, runner, repoID, "run_malformed_timestamp", "running", "active", old, map[string]any{
		"claimable_job_count":   1,
		"last_lane_advanced_at": "not-a-timestamp",
	})
	seedDoctorRecoveryCursorRun(t, ctx, runner, otherRepoID, "run_other_repo", "running", "active", old, map[string]any{
		"claimable_job_count":   5,
		"last_lane_advanced_at": old.Format(time.RFC3339),
	})

	block, problems, records := doctorRecoverySweepCursor(ctx, runner, repoID, now)
	if block["wedged_count"] != 2 {
		t.Fatalf("block = %#v, want two wedged runs", block)
	}
	problemText := strings.Join(problems, "\n")
	for _, want := range []string{"recovery_sweep_cursor_wedged.run_wedged", "recovery_sweep_cursor_wedged.run_malformed_timestamp"} {
		if !strings.Contains(problemText, want) {
			t.Fatalf("problems missing %s:\n%s", want, problemText)
		}
	}
	for _, notWant := range []string{"run_no_claimable", "run_removed_cursor", "run_completed", "run_fresh", "run_other_repo"} {
		if strings.Contains(problemText, notWant) {
			t.Fatalf("problems unexpectedly mention %s:\n%s", notWant, problemText)
		}
	}
	if len(records) != 2 {
		t.Fatalf("records = %#v, want two wedged records", records)
	}
}

func seedDoctorRecoveryCursorRepo(t *testing.T, ctx context.Context, runner db.Runner, repoID string, now time.Time) {
	t.Helper()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.repositories (
		  repository_id, repo_identity, repo_root, state_db_path, display_name,
		  registered_at, last_schema_version, state
		) VALUES ($1,$2,$3,$4,'repo',$5,28,'active')`,
		repoID, "ident_"+repoID, "/tmp/"+repoID, "/tmp/"+repoID+"/.striatum", now); err != nil {
		t.Fatalf("insert repository %s: %v", repoID, err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.workflow_snapshots (
		  repository_id, workflow_snapshot_id, workflow_id, content_sha256, workflow_json, loaded_at
		) VALUES ($1,$2,'wf','sha','{}'::jsonb,$3)`, repoID, "snap_"+repoID, now); err != nil {
		t.Fatalf("insert workflow snapshot %s: %v", repoID, err)
	}
}

func seedDoctorRecoveryCursorRun(t *testing.T, ctx context.Context, runner db.Runner, repoID string, runID string, runState string, cursorState string, startedAt time.Time, result map[string]any) {
	t.Helper()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.runs (
		  repository_id, run_id, workflow_snapshot_id, repo_root, state, created_at, started_at
		) VALUES ($1,$2,$3,$4,$5,$6,$6)`,
		repoID, runID, "snap_"+repoID, "/tmp/"+repoID, runState, startedAt); err != nil {
		t.Fatalf("insert run %s: %v", runID, err)
	}
	resultArg, err := db.JSONBArg(runner, result)
	if err != nil {
		t.Fatalf("jsonb arg for %s: %v", runID, err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.scheduler_cursors (
		  repository_id, run_id, cursor_kind, last_sweep_at, last_result_json, state
		) VALUES ($1,$2,'recovery',$3,$4::jsonb,$5)`,
		repoID, runID, startedAt, resultArg, cursorState); err != nil {
		t.Fatalf("insert scheduler cursor %s: %v", runID, err)
	}
}
