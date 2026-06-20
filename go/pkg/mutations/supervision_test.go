package mutations

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
	"github.com/jackc/pgx/v5"
)

type laneAttestationRunner struct {
	rows []map[string]any
}

func (r laneAttestationRunner) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return runPrepareRowsFromMaps(r.rows), nil
}

func TestSessionLaneAttestationRequiresLivePointerConsistency(t *testing.T) {
	attestation := sessionLaneAttestation(context.Background(), laneAttestationRunner{rows: []map[string]any{
		{
			"supervisor_id":                "sup_1",
			"pid":                          os.Getpid(),
			"pid_start_time":               "",
			"state":                        "attached",
			"pointer_daemon_supervisor_id": "dsup_1",
			"pointer_pid":                  os.Getpid(),
			"pointer_pid_start_time":       "",
			"pointer_state":                "attached",
			"pointer_metadata_json":        map[string]any{},
			"daemon_supervisor_id":         "dsup_1",
			"daemon_state":                 "attached",
		},
	}}, "repo_1", "sess_1")
	if attestation["attested"] != true || attestation["state"] != "attested" {
		t.Fatalf("attestation = %#v", attestation)
	}

	attestation = sessionLaneAttestation(context.Background(), laneAttestationRunner{rows: []map[string]any{
		{
			"supervisor_id":                "sup_1",
			"pid":                          os.Getpid(),
			"pid_start_time":               "",
			"state":                        "attached",
			"pointer_daemon_supervisor_id": "dsup_1",
			"pointer_pid":                  os.Getpid() + 1,
			"pointer_pid_start_time":       "",
			"pointer_state":                "attached",
			"pointer_metadata_json":        map[string]any{},
			"daemon_supervisor_id":         "dsup_1",
			"daemon_state":                 "attached",
		},
	}}, "repo_1", "sess_1")
	if attestation["attested"] != false || attestation["reason"] != "pointer_pid_mismatch" {
		t.Fatalf("attestation = %#v", attestation)
	}
}

func TestSessionLaneAttestationRejectsCorruptTmuxMetadata(t *testing.T) {
	attestation := sessionLaneAttestation(context.Background(), laneAttestationRunner{rows: []map[string]any{
		{
			"supervisor_id":                "sup_1",
			"pid":                          os.Getpid(),
			"pid_start_time":               "",
			"state":                        "attached",
			"pointer_daemon_supervisor_id": "dsup_1",
			"pointer_pid":                  os.Getpid(),
			"pointer_pid_start_time":       "",
			"pointer_state":                "attached",
			"pointer_metadata_json":        map[string]any{"tmux": map[string]any{"state": "backed", "session_name": "striatum-run"}},
			"daemon_supervisor_id":         "dsup_1",
			"daemon_state":                 "attached",
		},
	}}, "repo_1", "sess_1")
	if attestation["attested"] != false || attestation["reason"] != "tmux_metadata_corrupt" {
		t.Fatalf("attestation = %#v", attestation)
	}
}

func TestSessionLaneAttestationRejectsLiteralTmuxStartToken(t *testing.T) {
	origRunner := supervisionTmuxRunner
	defer func() { supervisionTmuxRunner = origRunner }()
	pid := os.Getpid()
	supervisionTmuxRunner = superviseReportFakeTmuxRunner{
		display: "%4|" + strconv.Itoa(pid) + "|0|#{pane_start_time}",
	}

	attestation := sessionLaneAttestation(context.Background(), laneAttestationRunner{rows: []map[string]any{
		{
			"supervisor_id":                "sup_1",
			"pid":                          pid,
			"pid_start_time":               "",
			"state":                        "attached",
			"pointer_daemon_supervisor_id": "dsup_1",
			"pointer_pid":                  pid,
			"pointer_pid_start_time":       "",
			"pointer_state":                "attached",
			"pointer_metadata_json": map[string]any{"tmux": map[string]any{
				"state":            "backed",
				"session_name":     "striatum-run",
				"pane_id":          "%4",
				"pane_pid":         pid,
				"pane_start_token": "#{pane_start_time}",
			}},
			"daemon_supervisor_id": "dsup_1",
			"daemon_state":         "attached",
		},
	}}, "repo_1", "sess_1")
	if attestation["attested"] != false || attestation["reason"] != "start_token_unverified" {
		t.Fatalf("attestation = %#v", attestation)
	}
}

func TestSuperviseReportRecordsAgentExit(t *testing.T) {
	tx := &superviseReportFakeTx{
		supervisor: supervisorReportRow{
			SupervisorID:       "sup_1",
			RunID:              "run_1",
			SessionID:          "sess_1",
			State:              "attached",
			DaemonSupervisorID: "dsup_1",
		},
	}
	runner := &superviseReportFakeRunner{tx: tx}

	result, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_supervise_report",
		Method:        "supervise.report",
		Params: map[string]any{
			"repository_id": "repo_1",
			"supervisor_id": "sup_1",
			"session_id":    "sess_1",
			"event_type":    "agent_exited",
			"payload": map[string]any{
				"exit_code":     7,
				"pty_log_path":  "/tmp/striatum/sup_1/pty.log",
				"pty_log_bytes": 55,
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseReport: %v", err)
	}
	if result["state"] != "stopped" {
		t.Fatalf("state = %v, want stopped", result["state"])
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction commit/rollback = %v/%v", tx.committed, tx.rolledBack)
	}
	if !tx.sawExec("UPDATE striatumd.process_supervisors", "state = 'stopped'") {
		t.Fatalf("process supervisor stop update was not executed: %#v", tx.execs)
	}
	if !tx.sawExec("UPDATE striatumd.process_supervisor_pointers", "state = 'stopped'") {
		t.Fatalf("pointer stop update was not executed: %#v", tx.execs)
	}
	if !tx.sawExec("UPDATE striatumd.daemon_supervisors", "state = 'stopped'") {
		t.Fatalf("daemon supervisor stop update was not executed: %#v", tx.execs)
	}

	event := tx.eventInsert()
	if event == nil {
		t.Fatalf("event insert was not executed: %#v", tx.execs)
	}
	if got := event.args[3]; got != "supervisor.agent_exited" {
		t.Fatalf("event_type arg = %v, want supervisor.agent_exited", got)
	}
	payload, ok := event.args[9].(map[string]any)
	if !ok {
		t.Fatalf("event payload arg = %#v", event.args[9])
	}
	if payload["daemon_supervisor_id"] != "dsup_1" {
		t.Fatalf("daemon_supervisor_id payload = %#v", payload)
	}
	nested, ok := payload["payload"].(map[string]any)
	if !ok || nested["exit_code"] != 7 {
		t.Fatalf("nested payload = %#v", payload["payload"])
	}
	if nested["pty_log_path"] != "/tmp/striatum/sup_1/pty.log" || nested["pty_log_bytes"] != 55 {
		t.Fatalf("agent_exited did not retain pty log diagnostic fields: %#v", nested)
	}
}

// TestSuperviseReportMeaningfulProgressRefreshesActiveLease guards RFC 0101
// Phase 1 and #378: a progress event the helper flagged meaningful refreshes
// the session's active lease (last_heartbeat_at + extended expiry) and stamps
// liveness fields so honest local work between MCP calls does not trip
// agent_lease_heartbeat_stall (#80 / #136), but does not append
// supervisor.progress to the durable event chain.
func TestSuperviseReportMeaningfulProgressRefreshesActiveLease(t *testing.T) {
	tx := &superviseReportFakeTx{
		supervisor: supervisorReportRow{
			SupervisorID: "sup_1",
			RunID:        "run_1",
			SessionID:    "sess_1",
			State:        "attached",
		},
		activeLeaseID:       "lease_1",
		activeLeaseResource: "job_1",
	}
	runner := &superviseReportFakeRunner{tx: tx}

	_, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_supervise_progress_meaningful",
		Method:        "supervise.report",
		Params: map[string]any{
			"repository_id": "repo_1",
			"supervisor_id": "sup_1",
			"session_id":    "sess_1",
			"event_type":    "progress",
			"payload":       map[string]any{"bytes": 4096, "total_bytes": 4096, "meaningful": true},
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseReport: %v", err)
	}
	if !tx.sawExec("UPDATE striatumd.leases", "last_heartbeat_at", "expires_at") {
		t.Fatalf("active lease was not heartbeat-refreshed: %#v", tx.execs)
	}
	if !tx.sawExec("UPDATE striatumd.sessions", "last_work_heartbeat_at") {
		t.Fatalf("last_work_heartbeat_at was not stamped: %#v", tx.execs)
	}
	events := tx.eventInserts()
	if len(events) != 1 {
		t.Fatalf("expected only the derived lease.heartbeat event, got %d: %#v", len(events), tx.execs)
	}
	if events[0].args[3] != "lease.heartbeat" {
		t.Fatalf("event_type = %v, want lease.heartbeat", events[0].args[3])
	}
}

// TestSuperviseReportNonMeaningfulProgressLeavesLeaseAlone guards the rejection
// side: a plain (spinner/redraw) progress event the helper did NOT flag must
// not refresh the lease — we only keep a lane alive on real output evidence.
func TestSuperviseReportNonMeaningfulProgressLeavesLeaseAlone(t *testing.T) {
	tx := &superviseReportFakeTx{
		supervisor: supervisorReportRow{
			SupervisorID: "sup_1",
			RunID:        "run_1",
			SessionID:    "sess_1",
			State:        "attached",
		},
		activeLeaseID: "lease_1",
	}
	runner := &superviseReportFakeRunner{tx: tx}

	_, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_supervise_progress_plain",
		Method:        "supervise.report",
		Params: map[string]any{
			"repository_id": "repo_1",
			"supervisor_id": "sup_1",
			"session_id":    "sess_1",
			"event_type":    "progress",
			"payload":       map[string]any{"bytes": 12, "total_bytes": 12},
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseReport: %v", err)
	}
	if tx.sawExec("UPDATE striatumd.leases", "last_heartbeat_at", "expires_at") {
		t.Fatalf("plain progress must not refresh the lease: %#v", tx.execs)
	}
	if tx.sawExec("UPDATE striatumd.sessions", "last_work_heartbeat_at") {
		t.Fatalf("plain progress must not stamp last_work_heartbeat_at: %#v", tx.execs)
	}
	if tx.sawExec("UPDATE striatumd.sessions", "last_pty_activity_at") {
		t.Fatalf("plain progress must not stamp last_pty_activity_at: %#v", tx.execs)
	}
	if events := tx.eventInserts(); len(events) != 0 {
		t.Fatalf("plain progress must not append supervisor.progress: %#v", events)
	}
}

// TestSuperviseReportMeaningfulProgressWithoutLeaseUpdatesLivenessOnly guards
// that meaningful progress without an active lease updates session liveness but
// does not issue a lease refresh or append supervisor.progress.
func TestSuperviseReportMeaningfulProgressWithoutLeaseUpdatesLivenessOnly(t *testing.T) {
	tx := &superviseReportFakeTx{
		supervisor: supervisorReportRow{
			SupervisorID: "sup_1",
			RunID:        "run_1",
			SessionID:    "sess_1",
			State:        "attached",
		},
		// activeLeaseID left empty => pgx.ErrNoRows
	}
	runner := &superviseReportFakeRunner{tx: tx}

	_, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_supervise_progress_no_lease",
		Method:        "supervise.report",
		Params: map[string]any{
			"repository_id": "repo_1",
			"supervisor_id": "sup_1",
			"session_id":    "sess_1",
			"event_type":    "progress",
			"payload":       map[string]any{"bytes": 4096, "total_bytes": 4096, "meaningful": true},
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseReport: %v", err)
	}
	if tx.sawExec("UPDATE striatumd.leases", "last_heartbeat_at", "expires_at") {
		t.Fatalf("no active lease => no lease update expected: %#v", tx.execs)
	}
	if tx.sawExec("UPDATE striatumd.sessions", "last_work_heartbeat_at") {
		t.Fatalf("no active lease => no work-heartbeat stamp expected: %#v", tx.execs)
	}
	if !tx.sawExec("UPDATE striatumd.sessions", "last_pty_activity_at") {
		t.Fatalf("meaningful progress should still stamp last_pty_activity_at: %#v", tx.execs)
	}
	if events := tx.eventInserts(); len(events) != 0 {
		t.Fatalf("meaningful progress without a lease must not append supervisor.progress: %#v", events)
	}
}

// TestSuperviseReportPipeLaneStampsPipeRead asserts the RFC 0131 #350 synthetic
// pipe-read signal: a meaningful progress event for a PIPE-transport lane (its
// supervisor pointer metadata records transport=pipe) stamps last_pipe_read_at —
// the pipe analogue of last_pty_activity_at — rather than last_pty_activity_at, so
// a genuinely-working pipe lane reads working_local during long local work. A
// pty_helper lane still stamps last_pty_activity_at (unchanged).
func TestSuperviseReportPipeLaneStampsPipeRead(t *testing.T) {
	t.Run("pipe lane stamps last_pipe_read_at", func(t *testing.T) {
		tx := &superviseReportFakeTx{
			supervisor: supervisorReportRow{
				SupervisorID: "sup_pipe",
				RunID:        "run_1",
				SessionID:    "sess_pipe",
				State:        "attached",
				Metadata:     map[string]any{"transport": "pipe"},
			},
			activeLeaseID:       "lease_1",
			activeLeaseResource: "job_1",
		}
		runner := &superviseReportFakeRunner{tx: tx}
		_, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
			SchemaVersion: rpc.SupportedEnvelopeVersion,
			RequestID:     "req_pipe_progress",
			Method:        "supervise.report",
			Params: map[string]any{
				"repository_id": "repo_1",
				"supervisor_id": "sup_pipe",
				"session_id":    "sess_pipe",
				"event_type":    "progress",
				"payload":       map[string]any{"bytes": 4096, "total_bytes": 4096, "meaningful": true},
			},
		})
		if err != nil {
			t.Fatalf("HandleSuperviseReport: %v", err)
		}
		if !tx.sawExec("UPDATE striatumd.sessions", "last_pipe_read_at") {
			t.Fatalf("pipe lane meaningful progress must stamp last_pipe_read_at: %#v", tx.execs)
		}
		if tx.sawExec("UPDATE striatumd.sessions", "last_pty_activity_at") {
			t.Fatalf("a pipe lane must NOT stamp last_pty_activity_at: %#v", tx.execs)
		}
	})

	t.Run("pty_helper lane still stamps last_pty_activity_at", func(t *testing.T) {
		tx := &superviseReportFakeTx{
			supervisor: supervisorReportRow{
				SupervisorID: "sup_pty",
				RunID:        "run_1",
				SessionID:    "sess_pty",
				State:        "attached",
				Metadata:     map[string]any{"transport": "pty_helper"},
			},
			activeLeaseID:       "lease_2",
			activeLeaseResource: "job_2",
		}
		runner := &superviseReportFakeRunner{tx: tx}
		_, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
			SchemaVersion: rpc.SupportedEnvelopeVersion,
			RequestID:     "req_pty_progress",
			Method:        "supervise.report",
			Params: map[string]any{
				"repository_id": "repo_1",
				"supervisor_id": "sup_pty",
				"session_id":    "sess_pty",
				"event_type":    "progress",
				"payload":       map[string]any{"bytes": 4096, "total_bytes": 4096, "meaningful": true},
			},
		})
		if err != nil {
			t.Fatalf("HandleSuperviseReport: %v", err)
		}
		if !tx.sawExec("UPDATE striatumd.sessions", "last_pty_activity_at") {
			t.Fatalf("pty_helper lane meaningful progress must stamp last_pty_activity_at: %#v", tx.execs)
		}
		if tx.sawExec("UPDATE striatumd.sessions", "last_pipe_read_at") {
			t.Fatalf("a pty_helper lane must NOT stamp last_pipe_read_at: %#v", tx.execs)
		}
	})

	t.Run("pipe lane behind owner bundle 0017 degrades to last_pty_activity_at", func(t *testing.T) {
		tx := &superviseReportFakeTx{
			supervisor: supervisorReportRow{
				SupervisorID: "sup_pipe_legacy",
				RunID:        "run_1",
				SessionID:    "sess_pipe_legacy",
				State:        "attached",
				Metadata:     map[string]any{"transport": "pipe"},
			},
			activeLeaseID:        "lease_3",
			activeLeaseResource:  "job_3",
			pipeReadColumnAbsent: true, // simulate a daemon behind owner bundle 0017
		}
		runner := &superviseReportFakeRunner{tx: tx}
		_, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
			SchemaVersion: rpc.SupportedEnvelopeVersion,
			RequestID:     "req_pipe_legacy",
			Method:        "supervise.report",
			Params: map[string]any{
				"repository_id": "repo_1",
				"supervisor_id": "sup_pipe_legacy",
				"session_id":    "sess_pipe_legacy",
				"event_type":    "progress",
				"payload":       map[string]any{"bytes": 4096, "total_bytes": 4096, "meaningful": true},
			},
		})
		if err != nil {
			t.Fatalf("HandleSuperviseReport: %v", err)
		}
		// Degrade-safe: no last_pipe_read_at column, so the pipe lane's output is
		// recorded under the established last_pty_activity_at column instead.
		if !tx.sawExec("UPDATE striatumd.sessions", "last_pty_activity_at") {
			t.Fatalf("pipe lane behind the bundle must fall back to last_pty_activity_at: %#v", tx.execs)
		}
		if tx.sawExec("UPDATE striatumd.sessions", "last_pipe_read_at") {
			t.Fatalf("a daemon behind owner bundle 0017 must NOT stamp last_pipe_read_at: %#v", tx.execs)
		}
	})
}

func TestSuperviseReportRecordsAttachClientExitAsDetached(t *testing.T) {
	tx := &superviseReportFakeTx{
		supervisor: supervisorReportRow{
			SupervisorID:       "sup_1",
			RunID:              "run_1",
			SessionID:          "sess_1",
			State:              "attached",
			DaemonSupervisorID: "dsup_1",
		},
	}
	runner := &superviseReportFakeRunner{tx: tx}

	result, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_supervise_attach_exit",
		Method:        "supervise.report",
		Params: map[string]any{
			"repository_id": "repo_1",
			"supervisor_id": "sup_1",
			"session_id":    "sess_1",
			"event_type":    "attach_client_exited",
			"payload":       map[string]any{"attach_client_pid": 123, "pid": 456},
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseReport: %v", err)
	}
	if result["state"] != "detached" {
		t.Fatalf("state = %v, want detached", result["state"])
	}
	if !tx.sawExec("UPDATE striatumd.process_supervisors", "state = 'detached'") {
		t.Fatalf("process supervisor detach update was not executed: %#v", tx.execs)
	}
	if !tx.sawExec("UPDATE striatumd.process_supervisor_pointers", "state = 'detached'") {
		t.Fatalf("pointer detach update was not executed: %#v", tx.execs)
	}
	if !tx.sawExec("UPDATE striatumd.daemon_supervisors", "state = 'detached'") {
		t.Fatalf("daemon supervisor detach update was not executed: %#v", tx.execs)
	}
	event := tx.eventInsert()
	if event == nil || event.args[3] != "supervisor.attach_client_exited" {
		t.Fatalf("event insert = %#v", event)
	}
}

func TestSuperviseReportAttachClientExitTmuxOKKeepsAttached(t *testing.T) {
	origRunner := supervisionTmuxRunner
	defer func() { supervisionTmuxRunner = origRunner }()
	supervisionTmuxRunner = superviseReportFakeTmuxRunner{
		display: "%4|456|0|1748452211",
	}

	tx := &superviseReportFakeTx{
		supervisor: supervisorReportRow{
			SupervisorID:       "sup_1",
			RunID:              "run_1",
			SessionID:          "sess_1",
			State:              "attached",
			DaemonSupervisorID: "dsup_1",
			Metadata: map[string]any{
				"tmux": map[string]any{
					"state":            "backed",
					"session_name":     "striatum-run",
					"pane_id":          "%4",
					"pane_pid":         456,
					"pane_start_token": "1748452211",
				},
			},
		},
	}
	runner := &superviseReportFakeRunner{tx: tx}

	result, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_supervise_attach_exit_live_tmux",
		Method:        "supervise.report",
		Params: map[string]any{
			"repository_id": "repo_1",
			"supervisor_id": "sup_1",
			"session_id":    "sess_1",
			"event_type":    "attach_client_exited",
			"payload": map[string]any{
				"attach_client_pid": 123,
				"pid":               456,
				"tmux_liveness":     "tmux_ok",
				"delivery_liveness": map[string]any{
					"class":   "ok",
					"healthy": true,
					"reason":  "recovered",
				},
				"delivery_reason": "recovered",
				"pane_text":       "raw pane bytes must not persist",
				"stdout":          "provider stdout must not persist",
				"stderr":          "provider stderr must not persist",
				"transcript":      "transcript must not persist",
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseReport: %v", err)
	}
	if result["state"] != "attached" {
		t.Fatalf("state = %v, want attached", result["state"])
	}
	if tx.sawExec("UPDATE striatumd.process_supervisors", "state = 'detached'") {
		t.Fatalf("live tmux attach exit detached supervisor: %#v", tx.execs)
	}
	if !tx.sawExec("UPDATE striatumd.process_supervisor_pointers", "metadata_json") {
		t.Fatalf("attach exit metadata merge was not executed: %#v", tx.execs)
	}
	metadataUpdate := tx.pointerMetadataUpdate()
	if metadataUpdate == nil {
		t.Fatalf("missing pointer metadata update: %#v", tx.execs)
	}
	metadata, ok := metadataUpdate.args[0].(map[string]any)
	if !ok {
		t.Fatalf("metadata arg = %#v", metadataUpdate.args[0])
	}
	tmux := metadata["tmux"].(map[string]any)
	delivery := tmux["delivery_liveness"].(map[string]any)
	if delivery["class"] != "degraded" || delivery["healthy"] != false || delivery["reason"] != "attach_client_exited" {
		t.Fatalf("delivery liveness = %#v", delivery)
	}
	lastExit := tmux["attach_client_last_exit"].(map[string]any)
	if lastExit["delivery_liveness"] == nil || lastExit["tmux_liveness"] != "tmux_ok" {
		t.Fatalf("attach last exit = %#v", lastExit)
	}
	event := tx.eventInsert()
	if event == nil || event.args[3] != "supervisor.attach_client_exited" {
		t.Fatalf("event insert = %#v", event)
	}
	eventPayload, ok := event.args[9].(map[string]any)
	if !ok {
		t.Fatalf("event payload = %#v", event.args[9])
	}
	nested, ok := eventPayload["payload"].(map[string]any)
	if !ok {
		t.Fatalf("nested event payload = %#v", eventPayload["payload"])
	}
	for _, forbidden := range []string{"pane_text", "stdout", "stderr", "transcript", "delivery_reason"} {
		if _, ok := nested[forbidden]; ok {
			t.Fatalf("nested event payload leaked %q: %#v", forbidden, nested)
		}
	}
	eventDelivery := nested["delivery_liveness"].(map[string]any)
	if eventDelivery["class"] != "degraded" || eventDelivery["healthy"] != false || eventDelivery["reason"] != "attach_client_exited" {
		t.Fatalf("event delivery liveness = %#v", eventDelivery)
	}
}

func TestSuperviseReportAttachClientExitUsesDaemonObservedTmuxLiveness(t *testing.T) {
	origRunner := supervisionTmuxRunner
	defer func() { supervisionTmuxRunner = origRunner }()
	supervisionTmuxRunner = superviseReportFakeTmuxRunner{
		display: "%4|456|1|1748452211",
	}

	tx := &superviseReportFakeTx{
		supervisor: supervisorReportRow{
			SupervisorID:       "sup_1",
			RunID:              "run_1",
			SessionID:          "sess_1",
			State:              "attached",
			DaemonSupervisorID: "dsup_1",
			Metadata: map[string]any{
				"tmux": map[string]any{
					"state":            "backed",
					"session_name":     "striatum-run",
					"pane_id":          "%4",
					"pane_pid":         456,
					"pane_start_token": "1748452211",
				},
			},
		},
	}
	runner := &superviseReportFakeRunner{tx: tx}

	result, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_supervise_attach_exit_dead_tmux",
		Method:        "supervise.report",
		Params: map[string]any{
			"repository_id": "repo_1",
			"supervisor_id": "sup_1",
			"session_id":    "sess_1",
			"event_type":    "attach_client_exited",
			"payload": map[string]any{
				"attach_client_pid": 123,
				"tmux_liveness":     "tmux_ok",
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseReport: %v", err)
	}
	if result["state"] != "detached" {
		t.Fatalf("state = %v, want detached", result["state"])
	}
	if !tx.sawExec("UPDATE striatumd.process_supervisors", "state = 'detached'") {
		t.Fatalf("dead daemon-observed tmux pane did not detach supervisor: %#v", tx.execs)
	}
	event := tx.eventInsert()
	if event == nil {
		t.Fatal("missing attach exit event")
	}
	eventPayload, ok := event.args[9].(map[string]any)
	if !ok {
		t.Fatalf("event payload = %#v", event.args[9])
	}
	nested, ok := eventPayload["payload"].(map[string]any)
	if !ok || nested["tmux_liveness"] != string(gosupervisor.TmuxLivenessPaneDead) {
		t.Fatalf("nested payload = %#v", eventPayload["payload"])
	}
}

func TestSuperviseReportValidatesHelperBatchBeforeTransaction(t *testing.T) {
	runner := &superviseReportFakeRunner{tx: &superviseReportFakeTx{}}
	_, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_supervise_report_bad_batch",
		Method:        "supervise.report",
		Params: map[string]any{
			"repository_id": "repo_1",
			"supervisor_id": "sup_1",
			"events_jsonl":  "{\"schema_version\":\"striatum.supervisor_helper.event.v1\",\"event_type\":\"progress\",\"supervisor_id\":\"sup_1\"}\n{\"event_type\":",
		},
	})
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "schema_invalid" {
		t.Fatalf("error = %v, want schema_invalid", err)
	}
	if runner.beginCount != 0 {
		t.Fatalf("BeginTx called for malformed batch: %d", runner.beginCount)
	}
}

func TestSuperviseReportRecordsHelperBatch(t *testing.T) {
	tx := &superviseReportFakeTx{
		supervisor: supervisorReportRow{
			SupervisorID:       "sup_1",
			RunID:              "run_1",
			SessionID:          "sess_1",
			State:              "attached",
			DaemonSupervisorID: "dsup_1",
		},
	}
	runner := &superviseReportFakeRunner{tx: tx}

	result, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_supervise_report_batch",
		Method:        "supervise.report",
		Params: map[string]any{
			"repository_id": "repo_1",
			"supervisor_id": "sup_1",
			"events": []any{
				map[string]any{
					"schema_version": "striatum.supervisor_helper.event.v1",
					"event_type":     "packet_accepted",
					"packet_id":      "packet_1",
					"timestamp":      "2026-05-17T00:00:00Z",
					"payload":        map[string]any{"bytes_read": 123},
				},
				map[string]any{
					"schema_version": "striatum.supervisor_helper.event.v1",
					"event_type":     "progress",
					"payload":        map[string]any{"kind": "heartbeat"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseReport batch: %v", err)
	}
	if result["events_recorded"] != 1 {
		t.Fatalf("events_recorded = %v, want 1 durable lifecycle event", result["events_recorded"])
	}
	events := tx.eventInserts()
	if len(events) != 1 {
		t.Fatalf("event inserts = %d, want 1 chained lifecycle event", len(events))
	}
	if events[0].args[3] != "supervisor."+gosupervisor.HelperEventPacketAccepted {
		t.Fatalf("event_type = %v, want supervisor.packet_accepted", events[0].args[3])
	}
	if result["state"] != "attached" {
		t.Fatalf("batch state = %v, want attached", result["state"])
	}
}

func TestSuperviseReportRecordsProcessTerminationDiagnostic(t *testing.T) {
	tx := &superviseReportFakeTx{
		supervisor: supervisorReportRow{
			SupervisorID:       "sup_1",
			RunID:              "run_1",
			SessionID:          "sess_1",
			State:              "attached",
			DaemonSupervisorID: "dsup_1",
			Metadata:           map[string]any{"transport": "pty_helper"},
		},
	}
	runner := &superviseReportFakeRunner{tx: tx}

	result, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_supervise_report_process_terminated",
		Method:        "supervise.report",
		Params: map[string]any{
			"repository_id": "repo_1",
			"supervisor_id": "sup_1",
			"event_type":    gosupervisor.HelperEventProcessTerminated,
			"timestamp":     "2026-06-04T18:00:00Z",
			"payload": map[string]any{
				"phase":      "context",
				"reason":     "context canceled",
				"signal":     "SIGTERM",
				"method":     "process_signal",
				"pid":        1234,
				"attach_pid": 5678,
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseReport process_terminated: %v", err)
	}
	if result["event_type"] != gosupervisor.HelperEventProcessTerminated || result["state"] != "attached" {
		t.Fatalf("result = %#v", result)
	}
	update := tx.pointerMetadataUpdate()
	if update == nil {
		t.Fatalf("missing pointer metadata update: %#v", tx.execs)
	}
	metadata, ok := update.args[0].(map[string]any)
	if !ok {
		t.Fatalf("metadata arg = %#v", update.args[0])
	}
	termination := metadata["last_process_termination"].(map[string]any)
	if termination["phase"] != "context" || termination["signal"] != "SIGTERM" || termination["reason"] != "context canceled" {
		t.Fatalf("last_process_termination = %#v", termination)
	}
	if termination["reported_at"] != "2026-06-04T18:00:00Z" {
		t.Fatalf("reported_at = %#v", termination["reported_at"])
	}
	event := tx.eventInsert()
	if event == nil || event.args[3] != "supervisor."+gosupervisor.HelperEventProcessTerminated {
		t.Fatalf("missing supervisor.process_terminated event: %#v", tx.execs)
	}
}

type superviseReportFakeRunner struct {
	tx         *superviseReportFakeTx
	beginCount int
}

func (r *superviseReportFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("unexpected runner exec outside tx")
}

func (r *superviseReportFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return superviseReportFakeRow{err: errors.New("unexpected runner query outside tx")}
}

func (r *superviseReportFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected runner query scalar outside tx")
}

func (r *superviseReportFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	r.beginCount++
	return r.tx, nil
}

type superviseReportFakeTx struct {
	supervisor supervisorReportRow
	// activeLeaseID, when set, is returned by the active-lease query that the
	// RFC 0101 Phase 1 meaningful-progress path issues; activeLeaseResource is
	// the resource_id for that lease. An empty activeLeaseID means the session
	// holds no active lease (pgx.ErrNoRows).
	activeLeaseID       string
	activeLeaseResource string
	nextEvent           int64
	execs               []superviseReportExec
	committed           bool
	rolledBack          bool
	// pipeReadColumnAbsent simulates a daemon behind owner bundle 0017 (RFC 0131
	// #350): the last_pipe_read_at column-present probe returns false, so a pipe
	// lane degrades to stamping last_pty_activity_at.
	pipeReadColumnAbsent bool
}

type superviseReportExec struct {
	sql  string
	args []any
}

func (tx *superviseReportFakeTx) Exec(_ context.Context, sql string, args ...any) error {
	tx.execs = append(tx.execs, superviseReportExec{sql: sql, args: append([]any(nil), args...)})
	return nil
}

func (tx *superviseReportFakeTx) QueryRow(_ context.Context, sql string, _ ...any) db.Row {
	switch {
	case strings.Contains(sql, "FROM striatumd.leases") && strings.Contains(sql, "owner_session_id"):
		if tx.activeLeaseID == "" {
			return superviseReportFakeRow{err: pgx.ErrNoRows}
		}
		var resource any
		if tx.activeLeaseResource != "" {
			res := tx.activeLeaseResource
			resource = &res
		}
		return superviseReportFakeRow{values: []any{tx.activeLeaseID, resource}}
	case strings.Contains(sql, "FROM striatumd.process_supervisor_pointers"):
		return superviseReportFakeRow{values: []any{tx.supervisor.Metadata}}
	case strings.Contains(sql, "FROM striatumd.process_supervisors"):
		dsup := tx.supervisor.DaemonSupervisorID
		var pid any
		if tx.supervisor.HasPID || tx.supervisor.PID > 0 {
			pid = tx.supervisor.PID
		}
		// RFC 0139: findReportSupervisor now also reads p.updated_at (the prior
		// pointer timestamp) so refreshReportSupervisorHeartbeat can compute the
		// coalesce write-skip from the already-read row. Surface PointerUpdatedAt
		// as that column; nil (empty) means "no stored timestamp" → always writes.
		var pointerUpdatedAt any
		if tx.supervisor.PointerUpdatedAt != "" {
			pointerUpdatedAt = tx.supervisor.PointerUpdatedAt
		}
		return superviseReportFakeRow{values: []any{
			tx.supervisor.SupervisorID,
			tx.supervisor.RunID,
			tx.supervisor.SessionID,
			tx.supervisor.State,
			pid,
			tx.supervisor.PIDStartTime,
			&dsup,
			tx.supervisor.Metadata,
			pointerUpdatedAt,
		}}
	case strings.Contains(sql, "repo_event_chain_heads"):
		return superviseReportFakeRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "nextval"):
		tx.nextEvent++
		return superviseReportFakeRow{values: []any{tx.nextEvent}}
	default:
		return superviseReportFakeRow{err: errors.New("unexpected query: " + sql)}
	}
}

func (tx *superviseReportFakeTx) QueryScalar(_ context.Context, sql string, _ ...any) (string, error) {
	// RFC 0131 #350: the pipe-read stamp probes for the last_pipe_read_at column
	// (owner bundle 0017). The fake reports it present (true) so the pipe-transport
	// path is exercised; pipeReadColumnAbsent flips it to simulate a daemon behind
	// the bundle (degrade-safe fallback to last_pty_activity_at).
	if strings.Contains(sql, "last_pipe_read_at") {
		if tx.pipeReadColumnAbsent {
			return "false", nil
		}
		return "true", nil
	}
	return "", errors.New("unexpected query scalar")
}

func (tx *superviseReportFakeTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *superviseReportFakeTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

func (tx *superviseReportFakeTx) sawExec(parts ...string) bool {
	for _, exec := range tx.execs {
		ok := true
		for _, part := range parts {
			if !strings.Contains(exec.sql, part) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func (tx *superviseReportFakeTx) eventInsert() *superviseReportExec {
	events := tx.eventInserts()
	if len(events) == 0 {
		return nil
	}
	return &events[0]
}

func (tx *superviseReportFakeTx) pointerMetadataUpdate() *superviseReportExec {
	for _, exec := range tx.execs {
		if strings.Contains(exec.sql, "UPDATE striatumd.process_supervisor_pointers") && strings.Contains(exec.sql, "metadata_json") {
			return &exec
		}
	}
	return nil
}

func (tx *superviseReportFakeTx) eventInserts() []superviseReportExec {
	events := []superviseReportExec{}
	for _, exec := range tx.execs {
		if strings.Contains(exec.sql, "INSERT INTO striatumd.events") {
			events = append(events, exec)
		}
	}
	return events
}

type superviseReportFakeRow struct {
	values []any
	err    error
}

func (r superviseReportFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, value := range r.values {
		switch target := dest[i].(type) {
		case *string:
			*target = value.(string)
		case **string:
			if value == nil {
				*target = nil
			} else if ptr, ok := value.(*string); ok {
				*target = ptr
			} else {
				text := value.(string)
				*target = &text
			}
		case **int:
			if value == nil {
				*target = nil
			} else if ptr, ok := value.(*int); ok {
				*target = ptr
			} else {
				number := value.(int)
				*target = &number
			}
		case *int64:
			*target = value.(int64)
		case *any:
			*target = value
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

type superviseReportFakeTmuxRunner struct {
	display string
	err     error
}

func (r superviseReportFakeTmuxRunner) Run(_ context.Context, args ...string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if len(args) > 0 && args[0] == "display-message" {
		return r.display, nil
	}
	return "", nil
}
