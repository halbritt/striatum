package reads

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
	"github.com/jackc/pgx/v5"
)

type superviseReadFakeRunner struct {
	listQuery      string
	listArgs       []any
	execCount      int
	statusRow      map[string]any
	escalationRows []map[string]any
}

func (r *superviseReadFakeRunner) Exec(context.Context, string, ...any) error {
	r.execCount++
	return errors.New("supervisor read handlers must be read-only")
}

func (r *superviseReadFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return superviseReadFakeRow{}
}

func (r *superviseReadFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected scalar query")
}

func TestOptionalNonNegativeIntParamAcceptsJSONNumber(t *testing.T) {
	got, err := optionalNonNegativeIntParam(rpc.Envelope{
		Params: map[string]any{"tail_lines": json.Number("120")},
	}, "tail_lines")
	if err != nil {
		t.Fatalf("optionalNonNegativeIntParam: %v", err)
	}
	if got != 120 {
		t.Fatalf("tail_lines = %d, want 120", got)
	}
}

func (r *superviseReadFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("supervisor read handlers must not open transactions")
}

func (r *superviseReadFakeRunner) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "LEFT JOIN striatumd.process_supervisors ps") && strings.Contains(sql, "FROM striatumd.sessions s"):
		sup := r.statusRow
		if sup == nil {
			sup = superviseBaseRow("sup_gone", 0, "stale-start-token")
		}
		now := time.Now().UTC()
		merged := map[string]any{
			"supervisor_id":                sup["supervisor_id"],
			"pid":                          sup["pid"],
			"pid_start_time":               sup["pid_start_time"],
			"supervisor_state":             sup["state"],
			"pointer_daemon_supervisor_id": "dsup_1",
			"pointer_pid":                  sup["pid"],
			"pointer_pid_start_time":       sup["pid_start_time"],
			"pointer_state":                "attached",
			"pointer_metadata_json":        sup["pointer_metadata_json"],
			"daemon_supervisor_id":         "dsup_1",
			"daemon_state":                 "attached",
			"state":                        "active",
			"registered_at":                now.Add(-10 * time.Minute),
			"last_tools_list_at":           now.Add(-9 * time.Minute),
			"last_await_packet_at":         now.Add(-8 * time.Minute),
			"last_mcp_request_at":          now.Add(-1 * time.Minute),
			"liveness_stall_class":         nil,
			"liveness_stall_since":         nil,
		}
		if meta, ok := sup["pointer_metadata_json"].(map[string]any); ok {
			merged["pointer_metadata_json"] = meta
			merged["supervisor_metadata_json"] = meta
		} else if metaBytes, ok := sup["pointer_metadata_json"].([]byte); ok {
			merged["pointer_metadata_json"] = metaBytes
			merged["supervisor_metadata_json"] = metaBytes
		}
		return dashboardAllRowsFromMaps([]map[string]any{merged}), nil
	case strings.Contains(sql, "FROM striatumd.runs"):
		return dashboardAllRowsFromMaps([]map[string]any{{"run_id": "run_1"}}), nil
	case strings.Contains(sql, "FROM striatumd.sessions") && strings.Contains(sql, "last_mcp_request_at"):
		now := time.Now().UTC()
		return dashboardAllRowsFromMaps([]map[string]any{
			{
				"session_id":              "sess_1",
				"state":                   "active",
				"registered_at":           now.Add(-10 * time.Minute),
				"last_tools_list_at":      now.Add(-9 * time.Minute),
				"last_await_packet_at":    now.Add(-8 * time.Minute),
				"last_mcp_request_at":     now.Add(-6 * time.Minute),
				"liveness_stall_class":    nil,
				"liveness_stall_since":    nil,
				"active_lease_id":         nil,
				"active_lease_expires_at": nil,
			},
		}), nil
	case strings.Contains(sql, "FROM striatumd.sessions") && strings.Contains(sql, "LIMIT 1"):
		return dashboardAllRowsFromMaps([]map[string]any{{"session_id": "sess_1"}}), nil
	case strings.Contains(sql, "LEFT JOIN striatumd.daemon_supervisors ds"):
		rows := superviseReattachRows()
		if len(args) > 1 {
			if supervisorID, ok := args[len(args)-1].(string); ok && strings.HasPrefix(supervisorID, "sup_") {
				rows = filterSuperviseRows(rows, supervisorID)
			}
		}
		return dashboardAllRowsFromMaps(rows), nil
	case strings.Contains(sql, "FROM striatumd.events") && strings.Contains(sql, "session.reported"):
		return dashboardAllRowsFromMaps(r.escalationRows), nil
	case strings.Contains(sql, "FROM striatumd.process_supervisors") && strings.Contains(sql, "ORDER BY ps.started_at DESC"):
		r.listQuery = sql
		r.listArgs = args
		if strings.Contains(sql, "LIMIT 1") {
			if r.statusRow != nil {
				return dashboardAllRowsFromMaps([]map[string]any{r.statusRow}), nil
			}
			return dashboardAllRowsFromMaps([]map[string]any{superviseBaseRow("sup_gone", 0, "stale-start-token")}), nil
		}
		return dashboardAllRowsFromMaps([]map[string]any{superviseBaseRow("sup_reattachable", os.Getpid(), currentStartTokenForTest())}), nil
	default:
		return nil, errors.New("unexpected query: " + sql)
	}
}

type superviseReadFakeRow struct{}

func (superviseReadFakeRow) Scan(...any) error {
	return errors.New("unexpected row scan")
}

type readFakeTmuxRunner struct {
	responses []readFakeTmuxResponse
	calls     [][]string
}

type readFakeTmuxResponse struct {
	prefix []string
	out    string
	err    error
}

func (r *readFakeTmuxRunner) Run(_ context.Context, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	for _, response := range r.responses {
		if len(args) < len(response.prefix) {
			continue
		}
		matches := true
		for i := range response.prefix {
			if args[i] != response.prefix[i] {
				matches = false
				break
			}
		}
		if matches {
			return response.out, response.err
		}
	}
	return "", errors.New("unexpected tmux command")
}

func TestHandleSuperviseListScopesByRepositoryRunAndState(t *testing.T) {
	runner := &superviseReadFakeRunner{}
	result, err := HandleSuperviseList(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "run_id": "run_1", "state": "attached"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseList: %v", err)
	}
	if !strings.Contains(runner.listQuery, "ps.repository_id = $1 AND ps.run_id = $2") || !strings.Contains(runner.listQuery, "ps.state = $3") {
		t.Fatalf("query is not repo/run/state scoped: %s", runner.listQuery)
	}
	if len(runner.listArgs) != 3 || runner.listArgs[0] != "repo_1" || runner.listArgs[1] != "run_1" || runner.listArgs[2] != "attached" {
		t.Fatalf("args = %#v", runner.listArgs)
	}
	if result["state"] != "attached" {
		t.Fatalf("state = %#v", result["state"])
	}
	supervisors := result["supervisors"].([]map[string]any)
	if len(supervisors) != 1 || supervisors[0]["supervisor_id"] != "sup_reattachable" {
		t.Fatalf("supervisors = %#v", supervisors)
	}
	tmux := supervisors[0]["tmux"].(map[string]any)
	if tmux["session_name"] != "striatum-run_1-lane_1-sup_reattachable" {
		t.Fatalf("supervisor tmux metadata = %#v", tmux)
	}
	if runner.execCount != 0 {
		t.Fatalf("unexpected exec count %d", runner.execCount)
	}
}

func TestHandleSuperviseReattachStatusSummarizesLivenessIdentityAndRepair(t *testing.T) {
	runner := &superviseReadFakeRunner{}
	result, err := HandleSuperviseReattachStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "run_id": "run_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseReattachStatus: %v", err)
	}
	summary := result["summary"].(map[string]int)
	if summary["total"] != 3 || summary["reattachable"] != 1 || summary["lost_candidate"] != 1 || summary["needs_repair"] != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	byID := map[string]map[string]any{}
	for _, row := range result["supervisors"].([]map[string]any) {
		byID[row["supervisor_id"].(string)] = row
	}
	if byID["sup_reattachable"]["pid_identity"] != "matched" || byID["sup_reattachable"]["reattach_state"] != "reattachable" {
		t.Fatalf("reattachable row = %#v", byID["sup_reattachable"])
	}
	if byID["sup_gone"]["reattach_reason"] != "pid_gone" {
		t.Fatalf("gone row = %#v", byID["sup_gone"])
	}
	if byID["sup_repair"]["reattach_reason"] != "pointer_missing" {
		t.Fatalf("repair row = %#v", byID["sup_repair"])
	}
	if runner.execCount != 0 {
		t.Fatalf("unexpected exec count %d", runner.execCount)
	}
}

func TestHandleSuperviseStatusKeepsGonePIDAsReadProjection(t *testing.T) {
	runner := &superviseReadFakeRunner{}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	if result["state"] != "attached" || result["liveness"] != "gone" {
		t.Fatalf("status projection = %#v", result)
	}
	tmux := result["tmux"].(map[string]any)
	if tmux["attach_command"] != "tmux attach-session -t striatum-run_1-lane_1-sup_gone" {
		t.Fatalf("status tmux metadata = %#v", tmux)
	}
	protocolLiveness, ok := result["protocol_liveness"].(map[string]any)
	if !ok || protocolLiveness["stall_class"] != "agent_protocol_idle_stall" {
		t.Fatalf("protocol liveness = %#v", result["protocol_liveness"])
	}
	if result["lane_attestation"] != "unattested" || result["lane_attestation_reason"] != "no_live_attached_supervisor" {
		t.Fatalf("lane attestation = %#v", result)
	}
	if runner.execCount != 0 {
		t.Fatalf("unexpected exec count %d", runner.execCount)
	}
}

// TestHandleSuperviseStatusExposesDistinctLivenessSignals guards #147 Symptom A:
// the single `liveness` field conflates the agent-process liveness, the
// supervisor-bridge state, and the lease freshness, so an operator cannot tell a
// detached/stopped bridge whose agent child is still alive from a genuinely dead
// lane. The status projection must expose them as distinct fields. Here the lane
// is gone (no live PID) so agent_pid_alive is false, but supervisor_state still
// surfaces the bridge state and lease_fresh is false.
func TestHandleSuperviseStatusExposesDistinctLivenessSignals(t *testing.T) {
	runner := &superviseReadFakeRunner{}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	if result["agent_pid_alive"] != false {
		t.Fatalf("agent_pid_alive = %#v, want false (gone lane)", result["agent_pid_alive"])
	}
	if result["supervisor_state"] != "attached" {
		t.Fatalf("supervisor_state = %#v, want attached (the bridge state, distinct from liveness)", result["supervisor_state"])
	}
	if result["lease_fresh"] != false {
		t.Fatalf("lease_fresh = %#v, want false (no live lease)", result["lease_fresh"])
	}
}

func TestHandleSuperviseStatusTreatsStartTokenMismatchAsGone(t *testing.T) {
	row := superviseBaseRow("sup_mismatch", os.Getpid(), "different-start-token")
	runner := &superviseReadFakeRunner{statusRow: row}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	if result["liveness"] != "gone" || result["pid_identity"] != "mismatch" {
		t.Fatalf("status projection = %#v", result)
	}
	if result["lane_attestation"] != "unattested" || result["lane_attestation_reason"] != "pid_identity_mismatch" {
		t.Fatalf("lane attestation = %#v", result)
	}
}

func TestHandleSuperviseStatusReportsTmuxLivenessClass(t *testing.T) {
	restore := SetTmuxRunnerForTest(&readFakeTmuxRunner{responses: []readFakeTmuxResponse{
		{prefix: []string{"has-session"}},
		{prefix: []string{"display-message"}, out: "%4|48211|0|1748452211\n"},
	}})
	defer restore()

	row := superviseBaseRow("sup_tmux", 0, "")
	row["pid"] = 48211
	row["pid_start_time"] = "1748452211"
	row["pointer_metadata_json"] = map[string]any{
		"tmux": map[string]any{
			"state":            "backed",
			"session_name":     "striatum-run_1-lane_1-sup_tmux",
			"pane_id":          "%4",
			"pane_pid":         48211,
			"pane_start_token": "1748452211",
			"attach_command":   "tmux attach-session -t striatum-run_1-lane_1-sup_tmux",
		},
	}
	runner := &superviseReadFakeRunner{statusRow: row}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	if result["liveness"] != "alive" || result["lane_attestation"] != "attested" {
		t.Fatalf("status projection = %#v", result)
	}
	// #147 Symptom A: a live agent surfaces agent_pid_alive=true distinctly.
	if result["agent_pid_alive"] != true {
		t.Fatalf("agent_pid_alive = %#v, want true (probe alive)", result["agent_pid_alive"])
	}
	tmux := result["tmux"].(map[string]any)
	liveness := tmux["liveness"].(map[string]any)
	if liveness["class"] != "tmux_ok" || liveness["healthy"] != true {
		t.Fatalf("tmux liveness = %#v", liveness)
	}
}

func TestHandleSuperviseStatusTreatsTmuxStartTokenUnverifiedAsUnattested(t *testing.T) {
	restore := SetTmuxRunnerForTest(&readFakeTmuxRunner{responses: []readFakeTmuxResponse{
		{prefix: []string{"has-session"}},
		{prefix: []string{"display-message"}, out: "%4|48211|0|\n"},
	}})
	defer restore()

	row := superviseBaseRow("sup_tmux", 48211, "")
	row["pointer_metadata_json"] = map[string]any{
		"tmux": map[string]any{
			"state":          "backed",
			"session_name":   "striatum-run_1-lane_1-sup_tmux",
			"pane_id":        "%4",
			"pane_pid":       48211,
			"attach_command": "tmux attach-session -t striatum-run_1-lane_1-sup_tmux",
		},
	}
	runner := &superviseReadFakeRunner{statusRow: row}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	if result["liveness"] != "alive" {
		t.Fatalf("status projection = %#v", result)
	}
	if result["lane_attestation"] != "unattested" || result["lane_attestation_reason"] != "start_token_unverified" {
		t.Fatalf("lane attestation = %#v", result)
	}
	tmux := result["tmux"].(map[string]any)
	liveness := tmux["liveness"].(map[string]any)
	if liveness["class"] != "tmux_ok" || liveness["detail"] != "start_token_unverified" {
		t.Fatalf("tmux liveness = %#v", liveness)
	}
}

func TestReattachStatusViewUsesTmuxObservedStartToken(t *testing.T) {
	restore := SetTmuxRunnerForTest(&readFakeTmuxRunner{responses: []readFakeTmuxResponse{
		{prefix: []string{"has-session"}},
		{prefix: []string{"display-message"}, out: "%4|48211|0|1748452211\n"},
	}})
	defer restore()

	view := reattachStatusView(context.Background(), map[string]any{
		"supervisor_id":                "sup_tmux",
		"run_id":                       "run_1",
		"session_id":                   "sess_1",
		"state":                        "attached",
		"pid":                          48211,
		"pid_start_time":               "1748452211",
		"pointer_daemon_supervisor_id": "dsup_1",
		"pointer_state":                "attached",
		"pointer_metadata_json": map[string]any{
			"tmux": map[string]any{
				"state":            "backed",
				"session_name":     "striatum-run_1-lane_1-sup_tmux",
				"pane_id":          "%4",
				"pane_pid":         48211,
				"pane_start_token": "1748452211",
			},
		},
		"daemon_supervisor_id": "dsup_1",
		"daemon_state":         "attached",
	})

	if view["reattach_state"] != "reattachable" || view["pid_identity"] != "matched" || view["current_pid_start_time"] != "1748452211" {
		t.Fatalf("reattach view = %#v", view)
	}
}

// TestHandleSuperviseStatusReconcilesBenignAttachExit guards #63 F7 on the read
// surface: a tmux-backed lane whose pane is alive (tmux_ok) and whose only
// recorded delivery degradation is the OBSERVER attach client exiting must NOT
// surface a top-level degraded delivery_liveness or a rebridge prompt — that
// noise prompted unnecessary rebridges. The raw observation (tmux.delivery_liveness
// and tmux.attach_client_last_exit) is preserved as provenance.
func TestHandleSuperviseStatusReconcilesBenignAttachExit(t *testing.T) {
	restore := SetTmuxRunnerForTest(&readFakeTmuxRunner{responses: []readFakeTmuxResponse{
		{prefix: []string{"has-session"}},
		{prefix: []string{"display-message"}, out: "%4|48211|0|1748452211\n"},
	}})
	defer restore()

	row := superviseBaseRow("sup_tmux", 48211, "1748452211")
	row["pointer_metadata_json"] = map[string]any{
		"tmux": map[string]any{
			"state":            "backed",
			"session_name":     "striatum-run_1-lane_1-sup_tmux",
			"pane_id":          "%4",
			"pane_pid":         48211,
			"pane_start_token": "1748452211",
			"attach_command":   "tmux attach-session -t striatum-run_1-lane_1-sup_tmux",
			"delivery_liveness": map[string]any{
				"class":       "degraded",
				"healthy":     false,
				"reason":      "attach_client_exited",
				"observed_at": "2026-05-28T17:00:00Z",
			},
			"attach_client_last_exit": map[string]any{
				"attach_pid":       123,
				"tmux_liveness":    "tmux_ok",
				"attach_exit_code": 0,
				"delivery_liveness": map[string]any{
					"class":   "degraded",
					"healthy": false,
					"reason":  "attach_client_exited",
				},
			},
		},
	}
	runner := &superviseReadFakeRunner{statusRow: row}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	if result["liveness"] != "alive" || result["lane_attestation"] != "attested" {
		t.Fatalf("pane liveness projection = %#v", result)
	}
	if result["lane_backend"] != "tmux" || result["pane_liveness"] != string(gosupervisor.TmuxLivenessOK) {
		t.Fatalf("distinct lane signals = %#v", result)
	}
	// F7: top-level delivery view is reconciled to healthy; no rebridge prompt.
	if result["delivery_state"] != "healthy" {
		t.Fatalf("delivery_state = %#v, want healthy", result["delivery_state"])
	}
	if _, present := result["delivery_liveness"]; present {
		t.Fatalf("benign attach exit must not surface top-level delivery_liveness: %#v", result["delivery_liveness"])
	}
	// Raw observation is preserved as provenance for auditability.
	tmux := result["tmux"].(map[string]any)
	if tmux["delivery_liveness"] == nil || tmux["attach_client_last_exit"] == nil {
		t.Fatalf("tmux delivery metadata = %#v", tmux)
	}
}

func TestHandleSuperviseStatusSurfacesHelperProcessGone(t *testing.T) {
	restore := SetTmuxRunnerForTest(&readFakeTmuxRunner{responses: []readFakeTmuxResponse{
		{prefix: []string{"has-session"}},
		{prefix: []string{"display-message"}, out: "%4|48211|0|1748452211\n"},
	}})
	defer restore()

	row := superviseBaseRow("sup_tmux", 48211, "1748452211")
	// Seed a dead helper PID inside metadata
	row["pointer_metadata_json"] = map[string]any{
		"helper_pid":            999999,
		"helper_pid_start_time": "some-start-time",
		"tmux": map[string]any{
			"state":            "backed",
			"session_name":     "striatum-run_1-lane_1-sup_tmux",
			"pane_id":          "%4",
			"pane_pid":         48211,
			"pane_start_token": "1748452211",
			"attach_command":   "tmux attach-session -t striatum-run_1-lane_1-sup_tmux",
		},
	}
	runner := &superviseReadFakeRunner{statusRow: row}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	if result["delivery_state"] != "helper_process_gone" {
		t.Fatalf("expected delivery_state to be 'helper_process_gone', got %v", result["delivery_state"])
	}
	delivery := result["delivery_liveness"].(map[string]any)
	if delivery["healthy"] != false || delivery["reason"] != "helper_process_gone" || delivery["class"] != "helper_process_gone" {
		t.Fatalf("expected delivery_liveness to reflect helper process gone, got %#v", delivery)
	}
}

func TestHandleSuperviseStatusSurfacesLastProcessTermination(t *testing.T) {
	row := superviseBaseRow("sup_terminated", os.Getpid(), currentStartTokenForTest())
	row["pointer_metadata_json"] = map[string]any{
		"transport": "pty_helper",
		"last_process_termination": map[string]any{
			"phase":       "context",
			"reason":      "context canceled",
			"signal":      "SIGTERM",
			"method":      "process_signal",
			"pid":         1234,
			"reported_at": "2026-06-04T18:00:00Z",
		},
	}
	runner := &superviseReadFakeRunner{statusRow: row}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	termination, ok := result["last_process_termination"].(map[string]any)
	if !ok {
		t.Fatalf("missing last_process_termination: %#v", result)
	}
	if termination["phase"] != "context" || termination["signal"] != "SIGTERM" || termination["reason"] != "context canceled" {
		t.Fatalf("last_process_termination = %#v", termination)
	}
	if termination["pid"] != 1234 {
		t.Fatalf("termination pid = %#v", termination["pid"])
	}
}

func TestHandleSuperviseStatusSurfacesRootDeliveryDegradedWithoutTmuxMetadata(t *testing.T) {
	row := superviseBaseRow("sup_plain", os.Getpid(), currentStartTokenForTest())
	row["pointer_metadata_json"] = map[string]any{
		"delivery_liveness": map[string]any{
			"class":       "degraded",
			"healthy":     false,
			"reason":      "stdin_reader_missing",
			"observed_at": "2026-05-28T17:00:00Z",
		},
	}
	runner := &superviseReadFakeRunner{statusRow: row}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	if _, ok := result["tmux"]; ok {
		t.Fatalf("plain supervisor unexpectedly has tmux metadata: %#v", result)
	}
	if result["lane_backend"] != "plain_pty" || result["delivery_state"] != "degraded" {
		t.Fatalf("plain delivery signals = %#v", result)
	}
	delivery := result["delivery_liveness"].(map[string]any)
	if delivery["class"] != "degraded" || delivery["healthy"] != false || delivery["reason"] != "stdin_reader_missing" {
		t.Fatalf("delivery liveness = %#v", delivery)
	}
	if !strings.Contains(superviseString(delivery["remediation"]), "striatum supervise rebridge --session-id sess_1") {
		t.Fatalf("delivery remediation = %#v", delivery["remediation"])
	}
}

func TestHandleSuperviseStatusNeverIncludesPaneTextInTmuxMetadata(t *testing.T) {
	restore := SetTmuxRunnerForTest(&readFakeTmuxRunner{responses: []readFakeTmuxResponse{
		{prefix: []string{"has-session"}},
		{prefix: []string{"display-message"}, out: "%4|48211|0|1748452211\n"},
	}})
	defer restore()

	row := superviseBaseRow("sup_tmux", 48211, "1748452211")
	row["pointer_metadata_json"] = map[string]any{
		"tmux": map[string]any{
			"state":            "backed",
			"session_name":     "striatum-run_1-lane_1-sup_tmux",
			"pane_id":          "%4",
			"pane_pid":         48211,
			"pane_start_token": "1748452211",
			"attach_command":   "tmux attach-session -t striatum-run_1-lane_1-sup_tmux",
			"pane_text":        "must not leak",
			"stdout":           "must not leak",
		},
	}
	runner := &superviseReadFakeRunner{statusRow: row}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	tmux := result["tmux"].(map[string]any)
	for _, forbidden := range []string{"pane_text", "capture", "buffer", "output", "stdout", "stderr", "transcript"} {
		if _, ok := tmux[forbidden]; ok {
			t.Fatalf("tmux metadata leaked %s: %#v", forbidden, tmux)
		}
	}
}

func TestHandleSuperviseStatusSurfacesLatestEscalationReport(t *testing.T) {
	now := time.Date(2026, 5, 27, 4, 0, 0, 0, time.UTC)
	runner := &superviseReadFakeRunner{
		statusRow: superviseBaseRow("sup_gone", 0, "stale-start-token"),
		escalationRows: []map[string]any{
			{
				"event_id":   int64(42),
				"run_id":     "run_1",
				"session_id": "sess_1",
				"created_at": now,
				"payload_json": map[string]any{
					"report_kind":  "escalate",
					"phase":        "await_packet",
					"blocker_kind": "other",
					"message":      "lane parked floor for conv_1 after 2 attempt(s)",
				},
			},
		},
	}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	if result["liveness"] != "gone" || result["liveness_reason"] != "escalated" {
		t.Fatalf("liveness projection = %#v", result)
	}
	report, ok := result["latest_escalation_report"].(map[string]any)
	if !ok {
		t.Fatalf("missing latest escalation report: %#v", result)
	}
	if report["event_id"] != int64(42) || report["blocker_kind"] != "other" {
		t.Fatalf("escalation report = %#v", report)
	}
	if !strings.Contains(superviseString(report["message"]), "parked floor") {
		t.Fatalf("escalation report message = %#v", report)
	}
}

func TestHandleSuperviseStatusSurfacesTrajectoryLogMetadata(t *testing.T) {
	repo := t.TempDir()
	scratch := filepath.Join(repo, ".striatum", "scratch", "sup_log")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(scratch, "pty.log")
	if err := os.WriteFile(logPath, []byte("provider booted\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	row := superviseBaseRow("sup_log", 0, "stale-start-token")
	row["scratch_path"] = scratch
	row["pointer_metadata_json"] = map[string]any{
		"agent_loop_mode": "self_driving",
		"tmux": map[string]any{
			"session_name":   "striatum-run_1-lane_1-sup_log",
			"attach_command": "tmux attach-session -t striatum-run_1-lane_1-sup_log",
		},
	}
	runner := &superviseReadFakeRunner{statusRow: row}
	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	trajectory := result["trajectory_log"].(map[string]any)
	if trajectory["status"] != "available" || trajectory["enabled"] != true || trajectory["path"] != logPath {
		t.Fatalf("trajectory_log = %#v", trajectory)
	}
	if trajectory["size_bytes"] != int64(len("provider booted\n")) {
		t.Fatalf("trajectory size = %#v", trajectory["size_bytes"])
	}
	if _, leaked := result["content"]; leaked {
		t.Fatalf("status must not include trajectory content: %#v", result)
	}
}

func TestHandleSuperviseTrajectoryReadsTailFromScratchLog(t *testing.T) {
	repo := t.TempDir()
	scratch := filepath.Join(repo, ".striatum", "scratch", "sup_log")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "one\ntwo\nthree\n"
	if err := os.WriteFile(filepath.Join(scratch, "pty.log"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	row := superviseBaseRow("sup_log", os.Getpid(), currentStartTokenForTest())
	row["repo_root"] = repo
	row["scratch_path"] = scratch
	row["pointer_metadata_json"] = map[string]any{"agent_loop_mode": "self_driving"}
	runner := &superviseReadFakeRunner{statusRow: row}
	result, err := HandleSuperviseTrajectory(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1", "tail_lines": 2},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseTrajectory: %v", err)
	}
	if result["content"] != "two\nthree\n" || result["truncated"] != true || result["tail_lines"] != 2 {
		t.Fatalf("trajectory result = %#v", result)
	}
	trajectory := result["trajectory_log"].(map[string]any)
	if trajectory["status"] != "available" || trajectory["exists"] != true || trajectory["size_bytes"] != len(body) {
		t.Fatalf("trajectory metadata = %#v", trajectory)
	}
}

func TestHandleSuperviseTrajectoryRejectsScratchPathOutsideRepo(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	row := superviseBaseRow("sup_escape", os.Getpid(), currentStartTokenForTest())
	row["repo_root"] = repo
	row["scratch_path"] = outside
	row["pointer_metadata_json"] = map[string]any{"agent_loop_mode": "self_driving"}
	runner := &superviseReadFakeRunner{statusRow: row}
	result, err := HandleSuperviseTrajectory(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseTrajectory: %v", err)
	}
	trajectory := result["trajectory_log"].(map[string]any)
	if trajectory["status"] != "invalid_path" || result["content"] != "" {
		t.Fatalf("trajectory result = %#v", result)
	}
}

func TestLinuxProcStatZombieDetectsDefunctProcessStateForReads(t *testing.T) {
	if !linuxProcStatZombie([]byte("1234 (agent command) Z 1 2 3")) {
		t.Fatal("expected Z process state to be treated as zombie")
	}
	if linuxProcStatZombie([]byte("1234 (agent command) S 1 2 3")) {
		t.Fatal("sleeping process state should not be treated as zombie")
	}
}

func TestTmuxMetadataIsAllowlistedForOperatorStatus(t *testing.T) {
	tmux := tmuxMetadata(map[string]any{
		"tmux": map[string]any{
			"session_name":            "striatum-run_1-lane_1-sup_1",
			"window_id":               "@1",
			"pane_id":                 "%2",
			"attach_command":          "tmux attach-session -t striatum-run_1-lane_1-sup_1",
			"last_ok_at":              "2026-05-28T18:00:00Z",
			"probe_skipped_at":        "2026-05-28T18:01:00Z",
			"probe_unavailable_count": 2,
			"last_unavailable_detail": "probe_timeout",
			"pane_text":               "terminal bytes must stay out",
			"raw_output":              "terminal bytes must stay out",
		},
	})
	if tmux["state"] != "attachable" || tmux["pane_id"] != "%2" {
		t.Fatalf("tmux metadata = %#v", tmux)
	}
	if tmux["probe_unavailable_count"] != 2 || tmux["last_unavailable_detail"] != "probe_timeout" {
		t.Fatalf("tmux probe metadata = %#v", tmux)
	}
	for _, forbidden := range []string{"pane_text", "raw_output"} {
		if _, ok := tmux[forbidden]; ok {
			t.Fatalf("tmux metadata leaked %s: %#v", forbidden, tmux)
		}
	}
}

func TestTmuxUnavailableMetadataCarriesStaticRemediation(t *testing.T) {
	tmux := tmuxMetadata(map[string]any{
		"tmux": map[string]any{
			"unavailable_reason": "tmux_not_found",
		},
	})
	if tmux["state"] != "unavailable" {
		t.Fatalf("tmux metadata = %#v", tmux)
	}
	if !strings.Contains(superviseString(tmux["remediation"]), "install tmux") {
		t.Fatalf("tmux remediation = %#v", tmux["remediation"])
	}
}

func TestSuperviseReadHandlersValidateParamsBeforeQuery(t *testing.T) {
	tests := []struct {
		name    string
		handler handlerFn
		params  map[string]any
	}{
		{
			name:    "status session",
			handler: HandleSuperviseStatus,
			params:  map[string]any{"repository_id": "repo_1"},
		},
		{
			name:    "list run",
			handler: HandleSuperviseList,
			params:  map[string]any{"repository_id": "repo_1"},
		},
		{
			name:    "trajectory session",
			handler: HandleSuperviseTrajectory,
			params:  map[string]any{"repository_id": "repo_1"},
		},
		{
			name:    "reattach run id",
			handler: HandleSuperviseReattachStatus,
			params:  map[string]any{"repository_id": "repo_1", "run_id": 42},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.handler(context.Background(), nil, rpc.Envelope{Params: tc.params})
			var rpcErr *rpc.Error
			if !errors.As(err, &rpcErr) || rpcErr.Code != "schema_invalid" {
				t.Fatalf("error = %v, want schema_invalid", err)
			}
		})
	}
}

func superviseReattachRows() []map[string]any {
	token := currentStartTokenForTest()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	row := func(supervisorID string, pid int, pidStart string) map[string]any {
		base := superviseBaseRow(supervisorID, pid, pidStart)
		base["started_at"] = now
		base["pointer_daemon_supervisor_id"] = "dsup_" + supervisorID
		base["pointer_pid"] = pid
		base["pointer_pid_start_time"] = pidStart
		base["pointer_state"] = "attached"
		base["pointer_updated_at"] = now
		base["pointer_metadata_json"] = map[string]any{"source": "test"}
		base["daemon_supervisor_id"] = "dsup_" + supervisorID
		base["daemon_instance_id"] = "daemon_1"
		base["daemon_pid"] = pid
		base["daemon_pid_start_time"] = pidStart
		base["daemon_state"] = "attached"
		base["daemon_heartbeat_at"] = now
		base["daemon_ended_at"] = nil
		base["daemon_stop_reason"] = nil
		return base
	}
	reattachable := row("sup_reattachable", os.Getpid(), token)
	gone := row("sup_gone", 0, "stale-start-token")
	repair := row("sup_repair", os.Getpid(), token)
	repair["pointer_daemon_supervisor_id"] = nil
	repair["daemon_supervisor_id"] = nil
	return []map[string]any{reattachable, gone, repair}
}

func filterSuperviseRows(rows []map[string]any, supervisorID string) []map[string]any {
	filtered := []map[string]any{}
	for _, row := range rows {
		if row["supervisor_id"] == supervisorID {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func superviseBaseRow(supervisorID string, pid int, pidStart string) map[string]any {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	return map[string]any{
		"repository_id":   "repo_1",
		"supervisor_id":   supervisorID,
		"run_id":          "run_1",
		"session_id":      "sess_1",
		"adapter":         "process",
		"command_json":    []any{"sleep", "600"},
		"cwd":             "/tmp/repo",
		"scratch_path":    "/tmp/repo/.striatum/scratch/" + supervisorID,
		"stdin_pipe_path": "/tmp/repo/.striatum/scratch/" + supervisorID + "/stdin.pipe",
		"pid":             pid,
		"pid_start_time":  pidStart,
		"state":           "attached",
		"started_at":      now,
		"heartbeat_at":    now,
		"ended_at":        nil,
		"stop_reason":     nil,
		"pointer_metadata_json": map[string]any{
			"tmux": map[string]any{
				"session_name":   "striatum-run_1-lane_1-" + supervisorID,
				"attach_command": "tmux attach-session -t striatum-run_1-lane_1-" + supervisorID,
			},
		},
	}
}

func currentStartTokenForTest() string {
	token, ok := processStartToken(os.Getpid())
	if !ok {
		return ""
	}
	return token
}

type doctorDeliveryDegradedFakeRunner struct {
	execCount int
}

func (r *doctorDeliveryDegradedFakeRunner) Exec(context.Context, string, ...any) error {
	r.execCount++
	return nil
}

func (r *doctorDeliveryDegradedFakeRunner) QueryRow(ctx context.Context, sql string, args ...any) db.Row {
	return superviseReadFakeRow{}
}

func (r *doctorDeliveryDegradedFakeRunner) QueryScalar(ctx context.Context, sql string, args ...any) (string, error) {
	if strings.Contains(sql, "substrate_version") {
		return "8", nil
	}
	return "", nil
}

func (r *doctorDeliveryDegradedFakeRunner) BeginTx(ctx context.Context) (db.TxRunner, error) {
	return nil, errors.New("not implemented")
}

func (r *doctorDeliveryDegradedFakeRunner) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "striatumd.process_supervisors ps") {
		token := currentStartTokenForTest()
		now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
		row := superviseBaseRow("sup_degraded", os.Getpid(), token)
		row["started_at"] = now
		row["pointer_daemon_supervisor_id"] = "dsup_sup_degraded"
		row["pointer_pid"] = os.Getpid()
		row["pointer_pid_start_time"] = token
		row["pointer_state"] = "attached"
		row["pointer_updated_at"] = now
		row["pointer_metadata_json"] = map[string]any{
			"tmux": map[string]any{
				"state":        "backed",
				"session_name": "striatum-run_1-lane_1-sup_degraded",
				"pane_id":      "%2",
				"pane_pid":     os.Getpid(),
				"delivery_liveness": map[string]any{
					"class":  "degraded",
					"reason": "attach_client_exited",
				},
			},
		}
		row["daemon_supervisor_id"] = "dsup_sup_degraded"
		row["daemon_instance_id"] = "daemon_1"
		row["daemon_pid"] = os.Getpid()
		row["daemon_pid_start_time"] = token
		row["daemon_state"] = "attached"
		row["daemon_heartbeat_at"] = now
		row["daemon_ended_at"] = nil
		row["daemon_stop_reason"] = nil
		return dashboardAllRowsFromMaps([]map[string]any{row}), nil
	}
	return dashboardAllRowsFromMaps([]map[string]any{}), nil
}

func TestHandleDoctorDeliveryDegradedSurfacesProblem(t *testing.T) {
	runner := &doctorDeliveryDegradedFakeRunner{}
	result, err := HandleDoctor(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1"},
	})
	if err != nil {
		t.Fatalf("HandleDoctor: %v", err)
	}
	if result["ok"] == true {
		t.Fatalf("expected doctor ok to be false, got true")
	}
	problems := result["problems"].([]string)
	found := false
	for _, p := range problems {
		if strings.HasPrefix(p, "supervisor_delivery_degraded.sup_degraded") {
			found = true
			if !strings.Contains(p, "attach_client_exited") {
				t.Fatalf("expected problem to mention attach_client_exited, got %q", p)
			}
		}
	}
	if !found {
		t.Fatalf("expected supervisor_delivery_degraded problem to be listed, got: %v", problems)
	}
	supervisors := result["supervisors"].([]map[string]any)
	if len(supervisors) != 1 {
		t.Fatalf("expected 1 supervisor, got: %#v", supervisors)
	}
	sup := supervisors[0]
	rem := superviseString(sup["remediation"])
	if !strings.Contains(rem, "striatum supervise rebridge") || !strings.Contains(rem, "--session-id sess_1") {
		t.Fatalf("expected rebridge remediation, got: %q", rem)
	}
}

type doctorTerminalSupervisorFakeRunner struct {
	supervisorQuery string
}

func (r *doctorTerminalSupervisorFakeRunner) Exec(context.Context, string, ...any) error {
	return nil
}

func (r *doctorTerminalSupervisorFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return superviseReadFakeRow{}
}

func (r *doctorTerminalSupervisorFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "8", nil
}

func (r *doctorTerminalSupervisorFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("not implemented")
}

func (r *doctorTerminalSupervisorFakeRunner) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "striatumd.process_supervisors ps") {
		r.supervisorQuery = sql
		if strings.Contains(sql, "JOIN striatumd.runs r") && strings.Contains(sql, "r.state NOT IN ('completed','failed','canceled')") {
			return dashboardAllRowsFromMaps([]map[string]any{}), nil
		}
		row := superviseBaseRow("sup_terminal", os.Getpid(), currentStartTokenForTest())
		row["pointer_metadata_json"] = map[string]any{
			"tmux": map[string]any{
				"state":        "backed",
				"session_name": "striatum-terminal-run",
				"pane_id":      "%1",
				"pane_pid":     os.Getpid(),
			},
		}
		return dashboardAllRowsFromMaps([]map[string]any{row}), nil
	}
	return dashboardAllRowsFromMaps([]map[string]any{}), nil
}

type doctorPanicTmuxRunner struct {
	called bool
}

func (r *doctorPanicTmuxRunner) Run(ctx context.Context, args ...string) (string, error) {
	r.called = true
	return "", errors.New("terminal-run supervisor should not be probed by doctor")
}

func TestHandleDoctorSkipsTerminalRunSupervisorLiveness(t *testing.T) {
	runner := &doctorTerminalSupervisorFakeRunner{}
	tmux := &doctorPanicTmuxRunner{}
	restore := SetTmuxRunnerForTest(tmux)
	defer restore()

	result, err := HandleDoctor(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1"},
	})
	if err != nil {
		t.Fatalf("HandleDoctor: %v", err)
	}
	if runner.supervisorQuery == "" {
		t.Fatal("expected doctor to query supervisors")
	}
	if !strings.Contains(runner.supervisorQuery, "JOIN striatumd.runs r") {
		t.Fatalf("doctor supervisor query did not join runs: %s", runner.supervisorQuery)
	}
	if tmux.called {
		t.Fatal("doctor probed tmux liveness for a terminal-run supervisor")
	}
	supervisors := result["supervisors"].([]map[string]any)
	if len(supervisors) != 0 {
		t.Fatalf("terminal-run supervisors should be omitted, got: %#v", supervisors)
	}
}

type noSupervisorFakeRunner struct{}

func (r *noSupervisorFakeRunner) Exec(ctx context.Context, sql string, args ...any) error { return nil }
func (r *noSupervisorFakeRunner) QueryRow(ctx context.Context, sql string, args ...any) db.Row {
	return superviseReadFakeRow{}
}
func (r *noSupervisorFakeRunner) QueryScalar(ctx context.Context, sql string, args ...any) (string, error) {
	return "", nil
}
func (r *noSupervisorFakeRunner) BeginTx(ctx context.Context) (db.TxRunner, error) {
	return nil, errors.New("not implemented")
}
func (r *noSupervisorFakeRunner) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "FROM striatumd.sessions") {
		return dashboardAllRowsFromMaps([]map[string]any{
			{
				"session_id":               "sess_no_sup",
				"run_id":                   "run_1",
				"role_id":                  "implementer",
				"lane_id":                  "lane_1",
				"slug":                     "sess_no_sup",
				"ordinal":                  1,
				"state":                    "active",
				"registered_at":            time.Now().UTC(),
				"supervisor_id":            "",
				"pid":                      nil,
				"supervisor_metadata_json": nil,
			},
		}), nil
	}
	return dashboardAllRowsFromMaps([]map[string]any{}), nil
}

func TestNoSupervisorRowDefaultsToNoneAndUnknown(t *testing.T) {
	runner := &noSupervisorFakeRunner{}
	sessions, err := statusSessions(context.Background(), runner, "repo_1", statusRunScope{runID: "run_1"})
	if err != nil {
		t.Fatalf("statusSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got: %v", sessions)
	}
	sess := sessions[0]
	if sess["lane_backend"] != "none" {
		t.Errorf("expected lane_backend to be 'none', got: %q", sess["lane_backend"])
	}
	if sess["delivery_state"] != "unknown" {
		t.Errorf("expected delivery_state to be 'unknown', got: %q", sess["delivery_state"])
	}
	if sess["lane_attestation"] != "unattested" {
		t.Errorf("expected lane_attestation to be 'unattested', got: %q", sess["lane_attestation"])
	}
	if sess["lane_attestation_reason"] != "no_attached_supervisor" {
		t.Errorf("expected lane_attestation_reason to be 'no_attached_supervisor', got: %q", sess["lane_attestation_reason"])
	}
}

func TestRemediationCoverageTable(t *testing.T) {
	tmuxClasses := []gosupervisor.TmuxLivenessClass{
		gosupervisor.TmuxLivenessSessionMissing,
		gosupervisor.TmuxLivenessPaneMissing,
		gosupervisor.TmuxLivenessPaneDead,
		gosupervisor.TmuxLivenessPanePIDMismatch,
		gosupervisor.TmuxLivenessUnavailable,
		gosupervisor.TmuxLivenessHelperDetachedProcessAlive,
	}
	for _, class := range tmuxClasses {
		rem := tmuxLivenessRemediation(string(class), "reason", "sess_1")
		if rem == "" {
			t.Errorf("expected non-empty remediation for tmux class %s", class)
		}
	}

	deliveryReasons := []string{
		"attach_client_exited",
		"stdin_reader_missing",
	}
	for _, reason := range deliveryReasons {
		rem := deliveryRemediation(reason, "sess_1")
		if rem == "" {
			t.Errorf("expected non-empty remediation for delivery reason %s", reason)
		}
	}
}

type statusTxFakeRunner struct {
	execs    []string
	repoRoot string
}

func (tx *statusTxFakeRunner) Exec(ctx context.Context, sql string, args ...any) error {
	tx.execs = append(tx.execs, sql)
	return nil
}

func (tx *statusTxFakeRunner) QueryRow(ctx context.Context, sql string, args ...any) db.Row {
	if strings.Contains(sql, "ps.cwd, ps.scratch_path") {
		// Issue #62: terminal transition resolves the supervisor working dir to
		// clean up the per-launch .gemini/settings.json.
		return statusCwdFakeRow{cwd: tx.repoRoot, scratchPath: filepath.Join(tx.repoRoot, ".striatum", "scratch", "sup_1")}
	}
	return superviseReadFakeRow{}
}

type statusCwdFakeRow struct {
	cwd         string
	scratchPath string
}

func (r statusCwdFakeRow) Scan(dest ...any) error {
	if len(dest) != 3 {
		return errors.New("statusCwdFakeRow expects 3 destinations")
	}
	if p, ok := dest[0].(*string); ok {
		*p = r.cwd
	}
	if p, ok := dest[1].(*string); ok {
		*p = r.scratchPath
	}
	if p, ok := dest[2].(*string); ok {
		*p = ""
	}
	return nil
}

func (tx *statusTxFakeRunner) QueryScalar(ctx context.Context, sql string, args ...any) (string, error) {
	return "", nil
}

func (tx *statusTxFakeRunner) Commit(ctx context.Context) error {
	return nil
}

func (tx *statusTxFakeRunner) Rollback(ctx context.Context) error {
	return nil
}

type statusRunnerWithTxFake struct {
	superviseReadFakeRunner
	tx *statusTxFakeRunner
}

func (r *statusRunnerWithTxFake) BeginTx(ctx context.Context) (db.TxRunner, error) {
	return r.tx, nil
}

func TestHandleSuperviseStatusTransitionsToStoppedOnUnexpectedExit(t *testing.T) {
	txFake := &statusTxFakeRunner{}
	runner := &statusRunnerWithTxFake{
		tx: txFake,
	}
	// Row has pid > 0 but with mismatch start token, making ProbeLaneLiveness return Alive=false
	row := superviseBaseRow("sup_mismatch", os.Getpid(), "different-start-token")
	runner.statusRow = row

	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}

	// Verify the returned state is immediately stopped
	if result["state"] != "stopped" || result["liveness"] != "gone" {
		t.Fatalf("expected state stopped, got: %#v", result)
	}

	// Verify database updates were executed in the transaction
	if len(txFake.execs) == 0 {
		t.Fatalf("expected Exec calls in transaction, got 0")
	}
	hasUpdate := false
	for _, sql := range txFake.execs {
		if strings.Contains(sql, "UPDATE striatumd.process_supervisors") {
			hasUpdate = true
		}
	}
	if !hasUpdate {
		t.Fatalf("expected UPDATE striatumd.process_supervisors in execs, got: %v", txFake.execs)
	}
}

// TestHandleSuperviseStatusGracefulExitRemovesEphemeralGeminiSettings is the
// issue #62 regression for the graceful-completion path: when supervise.status
// detects the lane has exited (probed gone) and transitions the supervisor
// attached->stopped, it must remove the per-launch .gemini/settings.json bearer
// token the agy lane left behind.
func TestHandleSuperviseStatusGracefulExitRemovesEphemeralGeminiSettings(t *testing.T) {
	repo := t.TempDir()
	geminiDir := filepath.Join(repo, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(geminiDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"mcpServers":{"striatum":{"httpUrl":"http://127.0.0.1:34135/mcp","headers":{"Authorization":"Bearer dtok_secret"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	scratchDir := filepath.Join(repo, ".striatum", "scratch", "sup_1")
	if err := os.MkdirAll(scratchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratchDir, "settings.json.created"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	txFake := &statusTxFakeRunner{repoRoot: repo}
	runner := &statusRunnerWithTxFake{tx: txFake}
	// pid > 0 but with a mismatched start token makes ProbeLaneLiveness report
	// Alive=false, driving the attached->stopped transition.
	runner.statusRow = superviseBaseRow("sup_1", os.Getpid(), "different-start-token")

	result, err := HandleSuperviseStatus(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStatus: %v", err)
	}
	if result["state"] != "stopped" {
		t.Fatalf("expected stopped transition, got: %#v", result["state"])
	}

	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		body, _ := os.ReadFile(settingsPath)
		t.Fatalf("graceful-exit teardown left token-bearing .gemini/settings.json: %s", body)
	}
	if _, err := os.Stat(filepath.Join(scratchDir, "settings.json.created")); !os.IsNotExist(err) {
		t.Fatalf("scratch created-marker not cleaned up on graceful exit")
	}
}
