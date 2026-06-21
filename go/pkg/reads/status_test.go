package reads

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

type statusFakeRunner struct {
	repoRoot string
	runFound bool
}

func (statusFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("status must be read-only")
}

func (statusFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return dashboardAllFakeRow{}
}

func (statusFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected scalar query")
}

func (statusFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("status must not open a transaction")
}

func (r statusFakeRunner) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	now := time.Now().UTC().Truncate(time.Second)
	switch {
	case strings.Contains(sql, "SELECT r.run_id") && strings.Contains(sql, "LIMIT 1"):
		if r.runFound {
			return dashboardAllRowsFromMaps([]map[string]any{{"run_id": "run_a"}}), nil
		}
		return dashboardAllRowsFromMaps(nil), nil
	case strings.Contains(sql, "jsonb_agg(j.write_scope_json)"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"run_id": "run_a", "branch_name": "main", "state": "running", "paused_at": nil,
			"write_scopes": []any{map[string]any{"mode": "repo_write", "repo_write": true, "allowed_paths": []any{"docs"}}},
		}}), nil
	case strings.Contains(sql, "FROM striatumd.sessions s") && strings.Contains(sql, "r.state = 'running'"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"run_id": "run_a", "role_id": "author", "lane_id": "lane_a", "slug": "author-1",
			"state": "active", "operator_label": "codex",
		}}), nil
	case strings.Contains(sql, "SELECT r.run_id, r.state, r.branch_name"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"run_id": "run_a", "state": "running", "branch_name": "main",
			"workflow_id": "refactoring-campaign", "workflow_version": "v3",
		}}), nil
	case strings.Contains(sql, "SELECT j.state, COUNT(*) AS count"):
		return dashboardAllRowsFromMaps([]map[string]any{
			{"state": "completed", "count": int64(2)},
			{"state": "running", "count": int64(1)},
		}), nil
	case strings.Contains(sql, "LEFT JOIN striatumd.process_supervisors ps") && strings.Contains(sql, "FROM striatumd.sessions s"):
		now := time.Now().UTC()
		merged := map[string]any{
			"session_id":                   "sess_a",
			"run_id":                       "run_a",
			"role_id":                      "author",
			"lane_id":                      "lane_a",
			"slug":                         "author-1",
			"ordinal":                      int64(1),
			"state":                        "active",
			"operator_label":               "codex",
			"supervisor_id":                "sup_a",
			"pid":                          int64(os.Getpid()),
			"pid_start_time":               "",
			"supervisor_state":             "attached",
			"pointer_daemon_supervisor_id": "dsup_a",
			"pointer_pid":                  int64(os.Getpid()),
			"pointer_pid_start_time":       "",
			"pointer_state":                "attached",
			"pointer_metadata_json": map[string]any{
				"tmux": map[string]any{
					"session_name":   "striatum-run_a-lane_a-sup_a",
					"attach_command": "tmux attach-session -t striatum-run_a-lane_a-sup_a",
				},
			},
			"supervisor_metadata_json": map[string]any{
				"tmux": map[string]any{
					"session_name":   "striatum-run_a-lane_a-sup_a",
					"attach_command": "tmux attach-session -t striatum-run_a-lane_a-sup_a",
				},
			},
			"daemon_supervisor_id": "dsup_a",
			"daemon_state":         "attached",
			"registered_at":        now.Add(-10 * time.Minute),
			"last_tools_list_at":   now.Add(-9 * time.Minute),
			"last_await_packet_at": now.Add(-8 * time.Minute),
			"last_mcp_request_at":  now.Add(-1 * time.Minute),
			"liveness_stall_class": nil,
			"liveness_stall_since": nil,
		}
		return dashboardAllRowsFromMaps([]map[string]any{merged}), nil
	case strings.Contains(sql, "SELECT s.session_id") && strings.Contains(sql, "ps.pid AS pid"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"session_id": "sess_a", "run_id": "run_a", "role_id": "author",
			"lane_id": "lane_a", "slug": "author-1", "ordinal": int64(1),
			"state": "active", "operator_label": "codex",
			"supervisor_id": "sup_a", "pid": int64(os.Getpid()),
			"supervisor_metadata_json": map[string]any{
				"tmux": map[string]any{
					"session_name":   "striatum-run_a-lane_a-sup_a",
					"attach_command": "tmux attach-session -t striatum-run_a-lane_a-sup_a",
				},
			},
		}}), nil
	case strings.Contains(sql, "FROM striatumd.blockers b"):
		row := map[string]any{
			"blocker_id": "blk_a", "run_id": "run_a", "job_id": "job_review",
			"session_id": "sess_a", "severity": "human_checkpoint",
			"blocker_kind": "operator_decision", "description": "needs decision",
			"state": "open", "created_at": now, "payload_json": map[string]any{"kind": "decision"},
			"workflow_job_id": "review", "job_state": "blocked",
		}
		return dashboardAllRowsFromMaps([]map[string]any{row}), nil
	case strings.Contains(sql, "v.verdict NOT IN ('accept', 'accept_with_findings')"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"verdict_id": "verdict_a", "run_id": "run_a", "job_id": "job_review",
			"workflow_job_id": "review", "verdict": "needs_revision",
			"posture": "blocking", "created_at": now,
		}}), nil
	case strings.Contains(sql, "GROUP BY v.posture, v.verdict"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"posture": "blocking", "verdict": "needs_revision", "count": int64(1),
		}}), nil
	case strings.Contains(sql, "FROM striatumd.queue_messages q"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"run_id": "run_a", "job_id": "job_draft", "workflow_job_id": "draft",
			"role_id": "author", "lane_id": "lane_a", "count": int64(1),
		}}), nil
	case strings.Contains(sql, "j.state = 'blocked'") && strings.Contains(sql, "FROM striatumd.jobs j"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"run_id": "run_a", "job_id": "job_review", "workflow_job_id": "review", "state": "blocked",
		}}), nil
	case strings.Contains(sql, "FROM striatumd.process_executions p"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"running_count": int64(2), "stale_running_count": int64(1),
			"lost_count": int64(1), "timed_out_count": int64(1),
		}}), nil
	case strings.Contains(sql, "FROM striatumd.process_supervisors ps"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"supervisor_id": "sup_a", "run_id": "run_a", "session_id": "sess_a", "pid": int64(os.Getpid()),
			"supervisor_state": "attached", "supervisor_heartbeat_at": now.Add(-10 * time.Minute),
			"session_last_heartbeat_at": now.Add(-10 * time.Minute),
			"lease_id":                  "lease_a", "job_id": "job_run", "acquired_at": now.Add(-10 * time.Minute),
			"expires_at": now.Add(10 * time.Minute), "lease_last_heartbeat_at": now.Add(-10 * time.Minute),
			"workflow_job_id": "run", "job_state": "running",
			"message_id": "msg_run", "message_state": "acked",
		}}), nil
	case strings.Contains(sql, "FROM striatumd.process_supervisors s"):
		return dashboardAllRowsFromMaps([]map[string]any{{"?column?": int64(1)}}), nil
	case strings.Contains(sql, "j.expected_artifacts_json"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"repo_root": r.repoRoot,
			"expected_artifacts_json": []any{
				map[string]any{"path": "artifacts/result.md", "required": true},
			},
		}}), nil
	case strings.Contains(sql, "SELECT w.workflow_json"):
		return dashboardAllRowsFromMaps([]map[string]any{{"workflow_json": map[string]any{
			"provenance_mode": "strict",
			"operator_mode":   "constrained",
			"recovery":        map[string]any{"auto_finalize": map[string]any{"enabled": true}},
			"phases": []any{
				map[string]any{"id": "phase_build", "name": "Build", "synthesis_job_id": "review"},
			},
			"jobs": []any{
				map[string]any{"id": "draft", "phase_id": "phase_build"},
				map[string]any{"id": "review", "phase_id": "phase_build"},
			},
		}}}), nil
	case strings.Contains(sql, "COUNT(*) AS candidate_count"):
		return dashboardAllRowsFromMaps([]map[string]any{{"candidate_count": int64(1)}}), nil
	case strings.Contains(sql, "SELECT j.job_id, j.workflow_job_id, j.state, j.attempt"):
		return dashboardAllRowsFromMaps([]map[string]any{
			{"job_id": "job_draft", "workflow_job_id": "draft", "state": "completed", "attempt": int64(1)},
			{"job_id": "job_review", "workflow_job_id": "review", "state": "running", "attempt": int64(1)},
		}), nil
	case strings.Contains(sql, "v.job_id, v.verdict_id, v.verdict"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"job_id": "job_review", "verdict_id": "verdict_latest",
			"verdict": "needs_revision", "posture": "blocking", "created_at": now,
		}}), nil
	case strings.Contains(sql, "SELECT state FROM striatumd.runs") && strings.Contains(sql, "LIMIT 1"):
		// RFC 0157 state_projection's run-state read.
		return dashboardAllRowsFromMaps([]map[string]any{{"state": "running"}}), nil
	case strings.Contains(sql, "SELECT workflow_job_id, state") && strings.Contains(sql, "FROM striatumd.jobs"):
		// RFC 0157 state_projection's per-job (id, state) read.
		return dashboardAllRowsFromMaps([]map[string]any{
			{"workflow_job_id": "draft", "state": "completed"},
			{"workflow_job_id": "review", "state": "running"},
		}), nil
	default:
		return nil, errors.New("unexpected query: " + sql)
	}
}

func TestHandleStatusBuildsPythonShapedRunProjection(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "artifacts", "result.md"), []byte("---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := HandleStatus(context.Background(), statusFakeRunner{repoRoot: repoRoot, runFound: true}, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_a", "run_id": "run_a"},
	})
	if err != nil {
		t.Fatalf("HandleStatus: %v", err)
	}

	// RFC 0042: each run row carries a human-facing workflow_name folded from
	// the joined workflow_snapshots identity (workflow_id [@ workflow_version]).
	runs := result["runs"].([]map[string]any)
	if len(runs) != 1 {
		t.Fatalf("runs = %#v", runs)
	}
	if runs[0]["workflow_name"] != "refactoring-campaign @ v3" {
		t.Fatalf("run workflow_name = %#v", runs[0]["workflow_name"])
	}
	// The raw snapshot columns are folded away, leaving only workflow_name.
	if _, leaked := runs[0]["workflow_id"]; leaked {
		t.Fatalf("run row leaked raw workflow_id column: %#v", runs[0])
	}

	jobs := result["jobs"].(map[string]int)
	if jobs["completed"] != 2 || jobs["running"] != 1 {
		t.Fatalf("jobs = %#v", jobs)
	}
	verdicts := result["verdicts_by_posture"].(map[string]map[string]int)
	if verdicts["blocking"]["needs_revision"] != 1 {
		t.Fatalf("verdicts_by_posture = %#v", verdicts)
	}
	claimable := result["claimable_jobs"].([]map[string]any)
	if len(claimable) != 1 || claimable[0]["workflow_job_id"] != "draft" {
		t.Fatalf("claimable_jobs = %#v", claimable)
	}
	sessions := result["sessions"].([]map[string]any)
	if len(sessions) != 1 || sessions[0]["lane_attestation"] != "attested" || sessions[0]["pid"] == nil {
		t.Fatalf("sessions = %#v", sessions)
	}
	tmux := sessions[0]["tmux"].(map[string]any)
	if tmux["session_name"] != "striatum-run_a-lane_a-sup_a" {
		t.Fatalf("session tmux metadata = %#v", tmux)
	}
	processHealth := result["process_health"].(map[string]any)
	if processHealth["stale_running_count"] != 1 {
		t.Fatalf("process_health = %#v", processHealth)
	}
	supervisorStalls := result["supervisor_stalls"].(map[string]any)
	if supervisorStalls["stalled_count"] != 1 || supervisorStalls["warning_count"] != 1 {
		t.Fatalf("supervisor_stalls = %#v", supervisorStalls)
	}
	autoFinalize := result["auto_finalize_dry_run"].(map[string]any)
	if autoFinalize["candidate_count"] != 1 {
		t.Fatalf("auto_finalize_dry_run = %#v", autoFinalize)
	}
	laneSummary := autoFinalize["lane_finalization_summary"].(map[string]any)
	if laneSummary["pending"] != 1 || laneSummary["auto_from_artifact"] != 0 {
		t.Fatalf("lane_finalization_summary = %#v", laneSummary)
	}
	if result["provenance_mode"] != "strict" || result["operator_mode"] != "constrained" || result["current_phase_id"] != "phase_build" {
		t.Fatalf("run progress fields = %#v", result)
	}
	actions := result["next_actions"].([]string)
	for _, expected := range []string{
		"claim_available_work",
		"inspect_packet_with_inbox",
		"recover_orphan_supervisor",
		"recovery_auto_publish",
		"resolve_human_checkpoint",
		"derive_expected_byline",
		"revise_workflow_cycle",
		"recovery_process_reconcile",
		"supervisor_stall_investigate",
	} {
		if !containsString(actions, expected) {
			t.Fatalf("next_actions missing %s in %#v", expected, actions)
		}
	}
	// #124: the dashboard summary only populates candidate_count (SQL count of
	// running jobs); eligible_count (artifacts on disk) is 0 here, so
	// recovery_auto_finalize must NOT be recommended.
	if containsString(actions, "recovery_auto_finalize") {
		t.Fatalf("#124: recovery_auto_finalize must not appear when eligible_count==0: %#v", actions)
	}
	if stringCount(actions, "derive_expected_byline") != 1 {
		t.Fatalf("next_actions should de-duplicate derive_expected_byline: %#v", actions)
	}
	nonAccepting := result["latest_non_accepting_review_verdicts"].([]map[string]any)
	if len(nonAccepting) != 1 || nonAccepting[0]["verdict"] != "needs_revision" {
		t.Fatalf("non-accepting verdicts = %#v", nonAccepting)
	}
}

func TestStatusNextActionsRespectAutoFinalizeLivePolicy(t *testing.T) {
	optOutActions := statusNextActions(
		nil,
		nil,
		nil,
		nil,
		false,
		false,
		map[string]any{},
		map[string]any{},
		map[string]any{
			"eligible_count": int64(1),
			"policy":         map[string]any{"live_allowed": false},
		},
	)
	if containsString(optOutActions, "recovery_auto_finalize") {
		t.Fatalf("opt-out actions include recovery_auto_finalize: %#v", optOutActions)
	}

	liveActions := statusNextActions(
		nil,
		nil,
		nil,
		nil,
		false,
		false,
		map[string]any{},
		map[string]any{},
		map[string]any{
			"eligible_count": int64(1),
			"policy":         map[string]any{"live_allowed": true},
		},
	)
	if !containsString(liveActions, "recovery_auto_finalize") {
		t.Fatalf("live actions missing recovery_auto_finalize: %#v", liveActions)
	}
}

// TestStatusNextActionsAutoFinalizeRequiresEligibleCount guards #124: a
// summary with candidates (running jobs that might be finalizable) but no
// eligible artifacts (eligible_count == 0) must NOT recommend
// recovery_auto_finalize. Only eligible_count > 0 is actionable.
func TestStatusNextActionsAutoFinalizeRequiresEligibleCount(t *testing.T) {
	// candidate_count > 0 but eligible_count == 0 → not recommended.
	candidatesOnly := statusNextActions(
		nil, nil, nil, nil, false, false,
		map[string]any{}, map[string]any{},
		map[string]any{
			"candidate_count": int64(3),
			"eligible_count":  int64(0),
			"policy":          map[string]any{"live_allowed": true},
		},
	)
	if containsString(candidatesOnly, "recovery_auto_finalize") {
		t.Fatalf("#124: candidate_count-only must not recommend recovery_auto_finalize: %#v", candidatesOnly)
	}

	// eligible_count > 0 → recommended.
	withEligible := statusNextActions(
		nil, nil, nil, nil, false, false,
		map[string]any{}, map[string]any{},
		map[string]any{
			"candidate_count": int64(3),
			"eligible_count":  int64(1),
			"policy":          map[string]any{"live_allowed": true},
		},
	)
	if !containsString(withEligible, "recovery_auto_finalize") {
		t.Fatalf("#124: eligible_count > 0 must recommend recovery_auto_finalize: %#v", withEligible)
	}

	// Neither candidate nor eligible → not recommended.
	noneEligible := statusNextActions(
		nil, nil, nil, nil, false, false,
		map[string]any{}, map[string]any{},
		map[string]any{
			"candidate_count": int64(0),
			"eligible_count":  int64(0),
			"policy":          map[string]any{"live_allowed": true},
		},
	)
	if containsString(noneEligible, "recovery_auto_finalize") {
		t.Fatalf("#124: zero eligible must not recommend recovery_auto_finalize: %#v", noneEligible)
	}
}

func TestHandleStatusRejectsUnknownRunID(t *testing.T) {
	_, err := HandleStatus(context.Background(), statusFakeRunner{runFound: false}, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_a", "run_id": "missing"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "not_found" {
		t.Fatalf("err = %#v", err)
	}
}

func containsString(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}

func stringCount(items []string, expected string) int {
	count := 0
	for _, item := range items {
		if item == expected {
			count++
		}
	}
	return count
}
