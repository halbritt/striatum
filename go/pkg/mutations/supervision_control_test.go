package mutations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/laneproviderauth"
	"github.com/halbritt/striatum/go/pkg/rpc"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
	"github.com/jackc/pgx/v5"
)

func TestSuperviseStartInsertsAndAttachesProcessSupervisor(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
	}()
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	supervisionLaunch = func(context.Context, supervisionStartConfig, string, string, string, string) (supervisionLaunchResult, error) {
		return supervisionLaunchResult{
			PID:          os.Getpid(),
			PIDStartTime: "start-token",
			Metadata: map[string]any{
				"tmux": map[string]any{
					"session_name":   "striatum-run_1-lane_1-sup_1",
					"attach_command": "tmux attach-session -t striatum-run_1-lane_1-sup_1",
				},
			},
		}, nil
	}

	repoRoot := t.TempDir()
	tx1 := &superviseControlFakeTx{}
	tx2 := &superviseControlFakeTx{}
	runner := &superviseControlFakeRunner{
		repoRoot: repoRoot,
		txs:      []*superviseControlFakeTx{tx1, tx2},
	}
	result, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStart: %v", err)
	}
	if result["state"] != "attached" || result["session_id"] != "sess_1" || result["run_id"] != "run_1" {
		t.Fatalf("start result = %#v", result)
	}
	if result["lane_attestation"] != "attested" {
		t.Fatalf("lane_attestation = %v", result["lane_attestation"])
	}
	tmux := result["tmux"].(map[string]any)
	if tmux["attach_command"] != "tmux attach-session -t striatum-run_1-lane_1-sup_1" {
		t.Fatalf("tmux metadata = %#v", tmux)
	}
	if !tx1.sawExec("INSERT INTO striatumd.process_supervisors") {
		t.Fatalf("missing process_supervisors insert: %#v", tx1.execs)
	}
	if !tx1.sawExec("pg_advisory_xact_lock") {
		t.Fatalf("missing supervise-start advisory lock: %#v", tx1.execs)
	}
	if !tx1.sawExec("INSERT INTO striatumd.daemon_supervisors") {
		t.Fatalf("missing daemon_supervisors insert: %#v", tx1.execs)
	}
	if !tx2.sawExec("UPDATE striatumd.process_supervisors", "state = $1") {
		t.Fatalf("missing attached process update: %#v", tx2.execs)
	}
	if len(tx1.eventInserts()) != 1 || len(tx2.eventInserts()) != 1 {
		t.Fatalf("event inserts tx1/tx2 = %d/%d", len(tx1.eventInserts()), len(tx2.eventInserts()))
	}
	payload := tx2.lastEventInsert().args[9].(map[string]any)
	eventTmux := payload["tmux"].(map[string]any)
	if eventTmux["session_name"] != "striatum-run_1-lane_1-sup_1" {
		t.Fatalf("started event tmux metadata = %#v", eventTmux)
	}
}

func TestSuperviseStartTreatsPermissionDeniedSignalProbeAsLive(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	origSignal := signalProcessZeroLocal
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
		signalProcessZeroLocal = origSignal
	}()
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	supervisionLaunch = func(context.Context, supervisionStartConfig, string, string, string, string) (supervisionLaunchResult, error) {
		return supervisionLaunchResult{
			PID:          999999,
			PIDStartTime: "cross-user-start-token",
			Metadata: map[string]any{
				"tmux": map[string]any{
					"session_name": "striatum-run_1-lane_1-sup_1",
					"run_as_user":  "lane-user",
				},
			},
		}, nil
	}
	signalProcessZeroLocal = func(pid int) error {
		if pid != 999999 {
			t.Fatalf("signal probe pid = %d, want launched pid", pid)
		}
		return syscall.EPERM
	}

	tx1 := &superviseControlFakeTx{}
	tx2 := &superviseControlFakeTx{}
	runner := &superviseControlFakeRunner{
		repoRoot: t.TempDir(),
		txs:      []*superviseControlFakeTx{tx1, tx2},
	}
	result, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_cross_user",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err != nil {
		t.Fatalf("permission-denied signal probe should not mark child exited: %v", err)
	}
	if result["state"] != "attached" {
		t.Fatalf("start result = %#v", result)
	}
	if tx2.sawEventType("supervisor.lost") {
		t.Fatalf("permission-denied signal probe must not record supervisor.lost: %#v", tx2.execs)
	}
}

func TestSuperviseStartCleansUpTmuxLaunchWhenChildDeadBeforeAttach(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	origSignal := signalProcessZeroLocal
	origRunner := supervisionTmuxRunner
	origProbe := probeLaneLivenessAtStart
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
		signalProcessZeroLocal = origSignal
		supervisionTmuxRunner = origRunner
		probeLaneLivenessAtStart = origProbe
	}()
	// #201: this exercises the genuine dead-child path, so the pane probe reports
	// not-alive; the supervisor is marked lost and the tmux launch is cleaned up.
	probeLaneLivenessAtStart = func(context.Context, map[string]any, int, string) gosupervisor.LaneLiveness {
		return gosupervisor.LaneLiveness{Alive: false, Class: "tmux_pane_dead"}
	}
	tmuxRunner := &mutationFakeTmuxRunner{}
	supervisionTmuxRunner = tmuxRunner
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	supervisionLaunch = func(context.Context, supervisionStartConfig, string, string, string, string) (supervisionLaunchResult, error) {
		return supervisionLaunchResult{
			PID:          999998,
			PIDStartTime: "dead-start-token",
			Metadata: map[string]any{
				"tmux": map[string]any{
					"state":        "backed",
					"session_name": "striatum-run-dead-before-attach",
					"pane_id":      "%4",
					"pane_pid":     999998,
				},
			},
		}, nil
	}
	signalProcessZeroLocal = func(pid int) error {
		if pid != 999998 {
			t.Fatalf("signal probe pid = %d, want launched pid", pid)
		}
		return syscall.ESRCH
	}

	tx1 := &superviseControlFakeTx{}
	tx2 := &superviseControlFakeTx{}
	runner := &superviseControlFakeRunner{
		repoRoot: t.TempDir(),
		txs:      []*superviseControlFakeTx{tx1, tx2},
	}
	_, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_dead_before_attach",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err == nil {
		t.Fatalf("expected child-dead attach gate to fail")
	}
	if len(tmuxRunner.calls) != 1 || strings.Join(tmuxRunner.calls[0], " ") != "kill-session -t striatum-run-dead-before-attach" {
		t.Fatalf("tmux calls = %#v", tmuxRunner.calls)
	}
	event := tx2.lastEventInsert()
	if event == nil || event.args[3] != "supervisor.lost" {
		t.Fatalf("lost event insert = %#v", event)
	}
	payload := event.args[9].(map[string]any)
	if payload["signal"] != "tmux_kill_session" {
		t.Fatalf("lost event payload = %#v", payload)
	}
	if !tx2.sawExec("UPDATE striatumd.process_supervisor_pointers", "metadata_json") {
		t.Fatalf("failed attach should persist launch metadata for later cleanup: %#v", tx2.execs)
	}
}

// TestSuperviseStartRecordsDetachedWhenPaneAliveButAttachFailed verifies #201:
// when the daemon cannot confirm the agent PID but the lane pane is alive,
// supervise start records a recoverable detached supervisor (NOT a misleading
// lost / child-exited), does NOT tear the live pane down, and leaves the session
// recoverable for a rebridge.
func TestSuperviseStartRecordsDetachedWhenPaneAliveButAttachFailed(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	origSignal := signalProcessZeroLocal
	origRunner := supervisionTmuxRunner
	origProbe := probeLaneLivenessAtStart
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
		signalProcessZeroLocal = origSignal
		supervisionTmuxRunner = origRunner
		probeLaneLivenessAtStart = origProbe
	}()
	probeLaneLivenessAtStart = func(context.Context, map[string]any, int, string) gosupervisor.LaneLiveness {
		return gosupervisor.LaneLiveness{Alive: true, Class: "tmux_ok"}
	}
	tmuxRunner := &mutationFakeTmuxRunner{}
	supervisionTmuxRunner = tmuxRunner
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	supervisionLaunch = func(context.Context, supervisionStartConfig, string, string, string, string) (supervisionLaunchResult, error) {
		return supervisionLaunchResult{
			PID:          999998,
			PIDStartTime: "alive-start-token",
			Metadata: map[string]any{
				"tmux": map[string]any{
					"state":        "backed",
					"session_name": "striatum-run-alive-attach-failed",
					"pane_id":      "%4",
					"pane_pid":     999998,
				},
			},
		}, nil
	}
	// The daemon's PID signal probe says dead, but the pane probe (above) says
	// alive — the attach-failed-lane-alive case.
	signalProcessZeroLocal = func(int) error { return syscall.ESRCH }

	tx1 := &superviseControlFakeTx{}
	tx2 := &superviseControlFakeTx{}
	runner := &superviseControlFakeRunner{
		repoRoot: t.TempDir(),
		txs:      []*superviseControlFakeTx{tx1, tx2},
	}
	_, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_alive_attach_failed",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err == nil {
		t.Fatalf("expected attach failure to surface an error")
	}
	if !strings.Contains(err.Error(), "alive") || !strings.Contains(strings.ToLower(err.Error()), "rebridge") {
		t.Fatalf("error not legible for attach-failed-lane-alive: %v", err)
	}
	// The live pane must NOT be torn down (no kill-session against an alive pane).
	if len(tmuxRunner.calls) != 0 {
		t.Fatalf("live pane should not be cleaned up, tmux calls = %#v", tmuxRunner.calls)
	}
	event := tx2.lastEventInsert()
	if event == nil || event.args[3] != "supervisor.detached" {
		t.Fatalf("expected supervisor.detached event, got %#v", event)
	}
	if !tx2.sawExecArg("UPDATE striatumd.process_supervisors", "detached") {
		t.Fatalf("supervisor should be set detached: %#v", tx2.execs)
	}
	// The session stays recoverable — it must NOT be marked lost.
	if tx2.sawExecArg("UPDATE striatumd.active_sessions", "lost") || tx2.sawExecArg("UPDATE striatumd.sessions", "lost") {
		t.Fatalf("detached attach-failure must not mark the session lost: %#v", tx2.execs)
	}
}

// TestSuperviseStartReplaceSupersedessStaleActiveSupervisor guards #116: when
// replace=true and a stale active supervisor row already exists for the
// session, supervise.start must supersede (mark lost) the stale row and
// succeed — never surface a raw Postgres 23505. The stale supervisor is
// detected inside the advisory-locked transaction, marked lost via
// markSupervisorLostInTx, and the new INSERT proceeds cleanly.
func TestSuperviseStartReplaceSupersedessStaleActiveSupervisor(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
	}()
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	supervisionLaunch = func(_ context.Context, _ supervisionStartConfig, _ string, _ string, _ string, _ string) (supervisionLaunchResult, error) {
		return supervisionLaunchResult{PID: os.Getpid(), PIDStartTime: "start-token"}, nil
	}

	repoRoot := t.TempDir()
	// tx1 simulates the first transaction: advisory lock + stale supervisor found
	// + supersede + INSERT. tx2 handles the attach update.
	tx1 := &superviseControlFakeTx{staleSupervisorID: "sup_stale"}
	tx2 := &superviseControlFakeTx{}
	runner := &superviseControlFakeRunner{
		repoRoot: repoRoot,
		txs:      []*superviseControlFakeTx{tx1, tx2},
	}
	result, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_replace",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"replace":       true,
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStart with replace=true: %v", err)
	}
	if result["state"] != "attached" || result["session_id"] != "sess_1" {
		t.Fatalf("replace=true start result = %#v", result)
	}
	// The stale supervisor must have been superseded: an UPDATE to
	// process_supervisors involving the stale supervisor_id must appear
	// in tx1's execs (from markSupervisorLostInTx).
	if !tx1.sawExecArg("UPDATE striatumd.process_supervisors", "sup_stale") {
		t.Fatalf("#116: replace=true must mark stale supervisor lost: %#v", tx1.execs)
	}
	// The new supervisor INSERT must also appear.
	if !tx1.sawExec("INSERT INTO striatumd.process_supervisors") {
		t.Fatalf("#116: replace=true must insert new supervisor: %#v", tx1.execs)
	}
}

// TestSuperviseStartNoReplaceReturnsCleanErrorOnActiveSupervisor guards #116:
// when replace is NOT set and a stale active supervisor exists, the handler
// must return a clean actionable rpc.Error that names the session and advises
// --replace — never a raw Postgres 23505.
func TestSuperviseStartNoReplaceReturnsCleanErrorOnActiveSupervisor(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
	}()
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	supervisionLaunch = func(_ context.Context, _ supervisionStartConfig, _ string, _ string, _ string, _ string) (supervisionLaunchResult, error) {
		return supervisionLaunchResult{PID: os.Getpid(), PIDStartTime: "start-token"}, nil
	}

	repoRoot := t.TempDir()
	tx1 := &superviseControlFakeTx{staleSupervisorID: "sup_stale"}
	runner := &superviseControlFakeRunner{
		repoRoot: repoRoot,
		txs:      []*superviseControlFakeTx{tx1},
	}
	_, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_no_replace",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			// replace is NOT set
		},
	})
	if err == nil {
		t.Fatalf("#116: expected clean error when stale supervisor exists without --replace")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" {
		t.Fatalf("#116: expected rpc invalid_transition error, got: %#v", err)
	}
	// Must name the stale supervisor and direct operator to --replace.
	if !strings.Contains(rpcErr.Message, "sup_stale") {
		t.Fatalf("#116: clean error must name stale supervisor id: %q", rpcErr.Message)
	}
	if !strings.Contains(rpcErr.Message, "--replace") {
		t.Fatalf("#116: clean error must mention --replace: %q", rpcErr.Message)
	}
	// Must NOT contain raw Postgres error text.
	if strings.Contains(rpcErr.Message, "23505") || strings.Contains(rpcErr.Message, "duplicate key") {
		t.Fatalf("#116: raw Postgres 23505 text must not escape: %q", rpcErr.Message)
	}
}

func TestSuperviseStartPropagatesRequireTmuxFromLaneSupervision(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
	}()
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	var launchedConfig supervisionStartConfig
	supervisionLaunch = func(_ context.Context, config supervisionStartConfig, _ string, _ string, _ string, _ string) (supervisionLaunchResult, error) {
		launchedConfig = config
		return supervisionLaunchResult{PID: os.Getpid(), PIDStartTime: "start-token"}, nil
	}

	runner := &superviseControlFakeRunner{
		repoRoot: t.TempDir(),
		workflowSupervision: map[string]any{
			"transport":    supervisionTransportPTYHelper,
			"require_tmux": true,
		},
		txs: []*superviseControlFakeTx{{}, {}},
	}
	result, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_require_tmux",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStart: %v", err)
	}
	if !launchedConfig.RequireTmux || launchedConfig.Transport != supervisionTransportPTYHelper {
		t.Fatalf("launched config = %#v", launchedConfig)
	}
	if result["require_tmux"] != true {
		t.Fatalf("result require_tmux = %#v", result["require_tmux"])
	}
}

func TestSuperviseStartWrapsAgentLoopLaneInAgentLoop(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
	}()
	t.Setenv("STRIATUM_AGENT_LOOP_BINARY", "/bin/striatumd")
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	var launchedConfig supervisionStartConfig
	supervisionLaunch = func(_ context.Context, config supervisionStartConfig, _ string, _ string, _ string, _ string) (supervisionLaunchResult, error) {
		launchedConfig = config
		return supervisionLaunchResult{PID: os.Getpid(), PIDStartTime: "start-token"}, nil
	}

	runner := &superviseControlFakeRunner{
		repoRoot: t.TempDir(),
		workflowLane: map[string]any{
			"adapter_capabilities": map[string]any{"agent_loop": true},
			// #181: an agent-loop lane must name a self-driving-capable adapter
			// (codex / agy / claude). Use an absolute path so PATH resolution is a
			// no-op and the wrapped-command assertion below stays stable.
			"command": []any{"/bin/codex"},
		},
		txs: []*superviseControlFakeTx{{}, {}},
	}
	result, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_agent_loop",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStart: %v", err)
	}
	want := []string{"/bin/striatumd", "-agent-loop", "--", "/bin/codex"}
	if strings.Join(launchedConfig.Command, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("launched command = %#v, want %#v", launchedConfig.Command, want)
	}
	if launchedConfig.AgentLoopMode != agentLoopModeSelfDriving || result["agent_loop_mode"] != agentLoopModeSelfDriving {
		t.Fatalf("self-driving agent-loop mode not surfaced: config=%#v result=%#v", launchedConfig.AgentLoopMode, result)
	}
	if launchedConfig.Transport != supervisionTransportPTYHelper || result["transport"] != supervisionTransportPTYHelper {
		t.Fatalf("agent-loop default transport = config:%q result:%#v, want pty_helper", launchedConfig.Transport, result["transport"])
	}
	if strings.Join(launchedConfig.OriginalCommand, "\x00") != "/bin/codex" {
		t.Fatalf("original command = %#v", launchedConfig.OriginalCommand)
	}
}

// TestRequireSupportedAgentLoopAdapter is the #181 unit guard: only codex / agy
// / claude (the adapters whose self-driving bootstrap delivery is wired) are
// accepted for an agent-loop lane; any other argv0 is refused with a legible
// error naming the offending adapter and the supported set.
func TestRequireSupportedAgentLoopAdapter(t *testing.T) {
	for _, ok := range [][]string{
		{"codex", "--dangerously-bypass-approvals-and-sandbox"},
		{"/home/u/.local/bin/claude", "--dangerously-skip-permissions"},
		{"agy"},
	} {
		if err := requireSupportedAgentLoopAdapter(ok); err != nil {
			t.Fatalf("supported adapter %v refused: %v", ok, err)
		}
	}
	for _, bad := range [][]string{
		{"/usr/bin/python3", "loop.py"},
		{"bash", "-c", "true"},
		{"my-home-grown-agent"},
	} {
		err := requireSupportedAgentLoopAdapter(bad)
		rpcErr, isRPC := err.(*rpc.Error)
		if !isRPC || rpcErr.Code != "invalid_transition" {
			t.Fatalf("argv %v: err = %#v, want invalid_transition rpc.Error", bad, err)
		}
		argv0 := bad[0]
		if !strings.Contains(rpcErr.Message, argv0) {
			t.Fatalf("argv %v: message must name argv0 %q: %q", bad, argv0, rpcErr.Message)
		}
		for _, adapter := range []string{"codex", "agy", "claude"} {
			if !strings.Contains(rpcErr.Message, adapter) {
				t.Fatalf("argv %v: message must name supported adapter %q: %q", bad, adapter, rpcErr.Message)
			}
		}
	}
	if err := requireSupportedAgentLoopAdapter(nil); err == nil {
		t.Fatalf("empty command must be refused")
	}
}

// TestSuperviseStartRefusesUnsupportedAgentLoopAdapter is the #181 end-to-end
// guard: supervise start refuses an agent-loop lane whose argv0 cannot run the
// self-driving loop, BEFORE inserting any process_supervisors row (so the
// operator gets a legible refusal instead of a wedged, healthy-looking lane).
func TestSuperviseStartRefusesUnsupportedAgentLoopAdapter(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
	}()
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	supervisionLaunch = func(_ context.Context, _ supervisionStartConfig, _ string, _ string, _ string, _ string) (supervisionLaunchResult, error) {
		t.Fatalf("#181: supervisionLaunch must not be reached for an unsupported agent-loop adapter")
		return supervisionLaunchResult{}, nil
	}

	tx1 := &superviseControlFakeTx{}
	runner := &superviseControlFakeRunner{
		repoRoot: t.TempDir(),
		workflowLane: map[string]any{
			"adapter_capabilities": map[string]any{"agent_loop": true},
			"command":              []any{"/usr/bin/python3", "loop.py"},
		},
		txs: []*superviseControlFakeTx{tx1},
	}
	_, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_bad_agent_loop",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" {
		t.Fatalf("#181: expected invalid_transition rpc error, got %#v", err)
	}
	if !strings.Contains(rpcErr.Message, "python3") {
		t.Fatalf("#181: refusal must name the offending argv0: %q", rpcErr.Message)
	}
	if tx1.sawExec("INSERT INTO striatumd.process_supervisors") {
		t.Fatalf("#181: refusal must happen BEFORE the supervisor row insert: %#v", tx1.execs)
	}
}

func TestSuperviseStartProviderAuthRefusalHasNoLaunchSideEffects(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	origProviderAuth := supervisionProviderAuthCheck
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
		supervisionProviderAuthCheck = origProviderAuth
	}()
	supervisionMkfifo = func(path string) error {
		t.Fatalf("provider-auth refusal must happen before FIFO creation: %s", path)
		return nil
	}
	supervisionLaunch = func(_ context.Context, _ supervisionStartConfig, _ string, _ string, _ string, _ string) (supervisionLaunchResult, error) {
		t.Fatalf("provider-auth refusal must happen before launching the provider lane")
		return supervisionLaunchResult{}, nil
	}
	supervisionProviderAuthCheck = func(_ context.Context, params laneproviderauth.Params) laneproviderauth.Result {
		if params.RunID != "run_1" || params.LaneID != "lane_1" || params.Provider != laneproviderauth.ProviderCodex {
			t.Fatalf("preflight params = %#v", params)
		}
		renderedEnv := strings.Join(params.Env, "\n")
		for _, forbidden := range []string{"STRIATUM_MCP_TOKEN", "DATABASE_URL", "OPENAI_API_KEY"} {
			if strings.Contains(renderedEnv, forbidden) {
				t.Fatalf("preflight env leaked %s: %s", forbidden, renderedEnv)
			}
		}
		return laneproviderauth.Result{
			Checked:           true,
			Provider:          laneproviderauth.ProviderCodex,
			RunID:             params.RunID,
			LaneID:            params.LaneID,
			RunAsUser:         params.RunAsUser,
			Status:            laneproviderauth.StatusFailed,
			FailureClass:      laneproviderauth.FailureAuthFailed,
			RawOutputReturned: false,
			Network:           "provider_cli_may_use_network",
			Costing:           "provider_tokens_may_be_spent",
			Remediation:       "refresh the provider login for the lane OS user, then retry supervise.start",
		}
	}
	t.Setenv("STRIATUM_MCP_TOKEN", "must-not-leak")
	t.Setenv("DATABASE_URL", "postgres://must-not-leak")
	t.Setenv("OPENAI_API_KEY", "must-not-leak")

	repoRoot := t.TempDir()
	tx1 := &superviseControlFakeTx{}
	runner := &superviseControlFakeRunner{
		repoRoot: repoRoot,
		workflowLane: map[string]any{
			"adapter_capabilities": map[string]any{"agent_loop": true},
			"command":              []any{"codex"},
		},
		txs: []*superviseControlFakeTx{tx1},
	}
	_, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_provider_auth_refused",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id":      "repo_1",
			"session_id":         "sess_1",
			"provider_auth_gate": "required",
		},
	})
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != laneproviderauth.FailureAuthFailed {
		t.Fatalf("expected %s rpc error, got %#v", laneproviderauth.FailureAuthFailed, err)
	}
	if tx1.sawExec("INSERT INTO striatumd.process_supervisors") ||
		tx1.sawExec("INSERT INTO striatumd.daemon_supervisors") ||
		tx1.sawEventType("supervisor.starting") {
		t.Fatalf("provider-auth refusal created supervisor state: %#v", tx1.execs)
	}
	if _, statErr := os.Stat(filepath.Join(repoRoot, ".striatum", "scratch")); !os.IsNotExist(statErr) {
		t.Fatalf("provider-auth refusal must not create supervisor scratch; stat err=%v", statErr)
	}
	details := rpcErr.Details["lane_provider_auth"].(map[string]any)
	if details["raw_output_returned"] != false || details["failure_class"] != laneproviderauth.FailureAuthFailed {
		t.Fatalf("safe details = %#v", details)
	}
	renderedDetails, _ := json.Marshal(details)
	if strings.Contains(string(renderedDetails), "must-not-leak") {
		t.Fatalf("provider-auth details leaked secret material: %s", string(renderedDetails))
	}
}

// TestSuperviseStartLabelsNonAgentLoopLaneAsPush guards #146: a lane that does
// NOT use the agent loop is a stdin-FIFO/push consumer (it reads a delivered
// packet then runs the agent), not a true self-driver that calls
// work.await_packet. It must be recorded as supervised_push, NOT self_driving, so
// claim-next surfaces the supervise_send hint instead of the misleading
// self_claim_note ("do not run supervise send") that sent operators down a dead
// path. Before the fix every supervised lane was hardcoded self_driving.
func TestSuperviseStartLabelsNonAgentLoopLaneAsPush(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
	}()
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	var launchedConfig supervisionStartConfig
	supervisionLaunch = func(_ context.Context, config supervisionStartConfig, _ string, _ string, _ string, _ string) (supervisionLaunchResult, error) {
		launchedConfig = config
		return supervisionLaunchResult{PID: os.Getpid(), PIDStartTime: "start-token"}, nil
	}

	runner := &superviseControlFakeRunner{
		repoRoot:     t.TempDir(),
		workflowLane: map[string]any{}, // a plain lane: no agent_loop capability => push consumer
		txs:          []*superviseControlFakeTx{{}, {}},
	}
	result, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_push",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStart: %v", err)
	}
	if launchedConfig.AgentLoopMode != agentLoopModePush || result["agent_loop_mode"] != agentLoopModePush {
		t.Fatalf("non-agent-loop lane must be labeled %q (push), got config=%q result=%#v (the misleading self_claim_note bug)",
			agentLoopModePush, launchedConfig.AgentLoopMode, result["agent_loop_mode"])
	}
}

// TestSuperviseStartAutoPromotesBareAgentCLILaneToSelfDriving guards #431 (the
// self-hosting crux): a lane whose command is a bare interactive agent CLI
// (claude/codex/agy) but which does NOT declare agent_loop must be auto-promoted
// to the self-driving agent loop. Such a command cannot consume stdin-FIFO push
// packets — launched in supervised_push mode it reads the pushed packet as
// conversational input, never drives work.await_packet/heartbeat/complete, and
// dies at the liveness deadline with no artifact. The daemon promotes it
// (mirroring workflowgenerate.defaultAgentLoopLane), wraps it in
// `striatumd -agent-loop`, defaults the transport to pty_helper, and surfaces the
// promotion legibly. This covers hand-edited / copied / pre-generate snapshots
// that the authoring-time default never touched.
func TestSuperviseStartAutoPromotesBareAgentCLILaneToSelfDriving(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
	}()
	t.Setenv("STRIATUM_AGENT_LOOP_BINARY", "/bin/striatumd")
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	var launchedConfig supervisionStartConfig
	supervisionLaunch = func(_ context.Context, config supervisionStartConfig, _ string, _ string, _ string, _ string) (supervisionLaunchResult, error) {
		launchedConfig = config
		return supervisionLaunchResult{PID: os.Getpid(), PIDStartTime: "start-token"}, nil
	}

	runner := &superviseControlFakeRunner{
		repoRoot: t.TempDir(),
		workflowLane: map[string]any{
			// A real agent command pasted over a fixture, WITHOUT agent_loop — the
			// exact #431 misconfiguration. Absolute path so PATH resolution is a no-op
			// and the wrapped-command assertion stays stable.
			"command": []any{"/bin/claude", "--model", "claude-opus-4-8", "--permission-mode", "bypassPermissions"},
		},
		txs: []*superviseControlFakeTx{{}, {}},
	}
	result, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_autopromote",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStart: %v", err)
	}
	want := []string{"/bin/striatumd", "-agent-loop", "--", "/bin/claude", "--model", "claude-opus-4-8", "--permission-mode", "bypassPermissions"}
	if strings.Join(launchedConfig.Command, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("auto-promoted command = %#v, want %#v", launchedConfig.Command, want)
	}
	if launchedConfig.AgentLoopMode != agentLoopModeSelfDriving || result["agent_loop_mode"] != agentLoopModeSelfDriving {
		t.Fatalf("auto-promoted lane mode = config:%q result:%#v, want %q (self_driving)",
			launchedConfig.AgentLoopMode, result["agent_loop_mode"], agentLoopModeSelfDriving)
	}
	if !launchedConfig.AgentLoopAutoPromoted || result["agent_loop_auto_promoted"] != true {
		t.Fatalf("auto-promotion not surfaced: config=%v result=%#v",
			launchedConfig.AgentLoopAutoPromoted, result["agent_loop_auto_promoted"])
	}
	if launchedConfig.Transport != supervisionTransportPTYHelper || result["transport"] != supervisionTransportPTYHelper {
		t.Fatalf("auto-promoted lane transport = config:%q result:%#v, want pty_helper",
			launchedConfig.Transport, result["transport"])
	}
}

// TestSuperviseStartRefusesExplicitlyDisabledAgentLoopOnAgentCLI guards #431: a
// bare interactive agent CLI with agent_loop EXPLICITLY false cannot run in push
// mode (it never drives work.*), so supervise start refuses it loudly BEFORE
// inserting any supervisor row instead of launching a lane that silently misses
// the liveness deadline (RFC 0111 legibility).
func TestSuperviseStartRefusesExplicitlyDisabledAgentLoopOnAgentCLI(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
	}()
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	supervisionLaunch = func(_ context.Context, _ supervisionStartConfig, _ string, _ string, _ string, _ string) (supervisionLaunchResult, error) {
		t.Fatal("lane must not launch when agent_loop is explicitly disabled on an interactive agent CLI")
		return supervisionLaunchResult{}, nil
	}

	runner := &superviseControlFakeRunner{
		repoRoot: t.TempDir(),
		workflowLane: map[string]any{
			"adapter_capabilities": map[string]any{"agent_loop": false},
			"command":              []any{"/bin/claude", "--permission-mode", "bypassPermissions"},
		},
		txs: []*superviseControlFakeTx{{}, {}},
	}
	_, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_refuse_pushagentcli",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" {
		t.Fatalf("expected invalid_transition rpc error, got %#v", err)
	}
	if !strings.Contains(rpcErr.Message, "agent_loop") {
		t.Fatalf("refusal message should name agent_loop, got %q", rpcErr.Message)
	}
}

func TestSuperviseStartAutoDispatchesPushLane(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	origWrite := supervisionWrite
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
		supervisionWrite = origWrite
	}()
	startToken := currentStartTokenForMutationTest(t)
	var runner *superviseControlFakeRunner
	supervisionMkfifo = func(path string) error {
		runner.pipePath = path
		return os.WriteFile(path, nil, 0o600)
	}
	supervisionLaunch = func(_ context.Context, config supervisionStartConfig, _ string, _ string, _ string, _ string) (supervisionLaunchResult, error) {
		if config.AgentLoopMode != agentLoopModePush {
			t.Fatalf("plain lane launched with agent_loop_mode=%q, want push", config.AgentLoopMode)
		}
		return supervisionLaunchResult{PID: os.Getpid(), PIDStartTime: startToken}, nil
	}
	var deliveredPayload []byte
	supervisionWrite = func(_ context.Context, _ db.TxRunner, _ string, _ string, _ string, payload []byte) (supervisorDeliveryResult, error) {
		deliveredPayload = append([]byte(nil), payload...)
		return supervisorDeliveryResult{BytesWritten: len(payload), StdinDelivery: stdinDeliveryPersistentFIFO}, nil
	}

	repoRoot := t.TempDir()
	tx1 := &superviseControlFakeTx{repoRoot: repoRoot}
	tx2 := &superviseControlFakeTx{repoRoot: repoRoot}
	tx3 := &superviseControlFakeTx{repoRoot: repoRoot, pid: os.Getpid(), pidStart: startToken, claimable: true}
	runner = &superviseControlFakeRunner{
		repoRoot:     repoRoot,
		workflowLane: map[string]any{}, // plain lane: no agent_loop capability => push consumer
		txs:          []*superviseControlFakeTx{tx1, tx2, tx3},
	}

	result, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_push_auto_dispatch",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStart: %v", err)
	}
	auto := asMap(result["auto_dispatch"])
	if auto["status"] != "delivered" {
		t.Fatalf("auto_dispatch = %#v, want delivered", auto)
	}
	packetID, _ := auto["packet_id"].(string)
	if !strings.HasPrefix(packetID, "wp_") {
		t.Fatalf("auto_dispatch packet_id = %q, want generated work packet id", packetID)
	}
	delivery := asMap(auto["delivery"])
	if delivery["packet_id"] != packetID || delivery["delivery_state"] != "delivered_unacknowledged" {
		t.Fatalf("delivery = %#v", delivery)
	}
	if len(deliveredPayload) == 0 || !strings.Contains(string(deliveredPayload), `"packet":"body"`) {
		t.Fatalf("delivered payload = %q", string(deliveredPayload))
	}
	if !tx3.sawExec("INSERT INTO striatumd.leases") || !tx3.sawExec("INSERT INTO striatumd.work_packets") {
		t.Fatalf("auto-dispatch did not claim work in tx3: %#v", tx3.execs)
	}
	if !tx3.sawEventType("queue.claimed") || !tx3.sawEventType("supervisor.packet_delivered") {
		t.Fatalf("auto-dispatch events missing: %#v", tx3.eventInserts())
	}
}

func TestMergePointerMetadataPreservesExistingTmuxFields(t *testing.T) {
	tx := &superviseControlFakeTx{
		metadata: map[string]any{
			"transport": supervisionTransportPTYHelper,
			"tmux": map[string]any{
				"state": "backed",
				"delivery_liveness": map[string]any{
					"class":   "degraded",
					"healthy": false,
					"reason":  "attach_client_exited",
				},
				"attach_client_last_exit": map[string]any{"attach_exit_code": 1},
			},
		},
	}
	err := mergePointerMetadata(context.Background(), tx, "repo_1", "sup_1", map[string]any{
		"helper_events_offset": 512,
		"tmux": map[string]any{
			"pane_id":          "%4",
			"pane_start_token": "123",
		},
	})
	if err != nil {
		t.Fatalf("mergePointerMetadata: %v", err)
	}
	if len(tx.execs) == 0 {
		t.Fatal("expected metadata update")
	}
	metadata, ok := tx.execs[len(tx.execs)-1].args[0].(map[string]any)
	if !ok {
		t.Fatalf("metadata arg = %#v", tx.execs[len(tx.execs)-1].args[0])
	}
	tmux := asMap(metadata["tmux"])
	if tmux["pane_id"] != "%4" || tmux["pane_start_token"] != "123" {
		t.Fatalf("incoming tmux fields missing: %#v", tmux)
	}
	delivery := asMap(tmux["delivery_liveness"])
	if delivery["reason"] != "attach_client_exited" {
		t.Fatalf("delivery liveness was not preserved: %#v", tmux)
	}
	if asMap(tmux["attach_client_last_exit"])["attach_exit_code"] != 1 {
		t.Fatalf("attach_client_last_exit was not preserved: %#v", tmux)
	}
	if metadata["helper_events_offset"] != 512 {
		t.Fatalf("helper_events_offset = %#v", metadata["helper_events_offset"])
	}
}

func TestSuperviseStartAgentLoopAllowsExplicitPipeTransportOverride(t *testing.T) {
	origMkfifo := supervisionMkfifo
	origLaunch := supervisionLaunch
	defer func() {
		supervisionMkfifo = origMkfifo
		supervisionLaunch = origLaunch
	}()
	t.Setenv("STRIATUM_AGENT_LOOP_BINARY", "/bin/striatumd")
	supervisionMkfifo = func(path string) error {
		return os.WriteFile(path, nil, 0o600)
	}
	var launchedConfig supervisionStartConfig
	supervisionLaunch = func(_ context.Context, config supervisionStartConfig, _ string, _ string, _ string, _ string) (supervisionLaunchResult, error) {
		launchedConfig = config
		return supervisionLaunchResult{PID: os.Getpid(), PIDStartTime: "start-token"}, nil
	}

	runner := &superviseControlFakeRunner{
		repoRoot: t.TempDir(),
		workflowLane: map[string]any{
			"adapter_capabilities": map[string]any{"agent_loop": true},
			"command":              []any{"/bin/codex"}, // #181: self-driving-capable adapter
		},
		workflowSupervision: map[string]any{
			"transport": supervisionTransportPipe,
		},
		txs: []*superviseControlFakeTx{{}, {}},
	}
	result, err := HandleSuperviseStart(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_start_agent_loop_pipe_override",
		Method:        "supervise.start",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStart: %v", err)
	}
	if launchedConfig.Transport != supervisionTransportPipe || result["transport"] != supervisionTransportPipe {
		t.Fatalf("agent-loop explicit transport override = config:%q result:%#v, want pipe", launchedConfig.Transport, result["transport"])
	}
}

func TestResolveSupervisedCommandBinaryResolvesOnAugmentedPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "faketool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STRIATUM_SUPERVISED_PATH_DIRS", dir)

	got := resolveSupervisedCommandBinary([]string{"faketool", "arg"}, nil)
	want := []string{bin, "arg"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("resolved = %#v, want %#v", got, want)
	}

	// An absolute / path-bearing argv0 is left untouched.
	abs := resolveSupervisedCommandBinary([]string{"/bin/cat", "-"}, nil)
	if strings.Join(abs, "\x00") != strings.Join([]string{"/bin/cat", "-"}, "\x00") {
		t.Fatalf("absolute argv0 rewritten: %#v", abs)
	}
}

// TestFailedAttachOutcome verifies #201: a supervise-start attach failure with a
// provably-alive pane is classified detached (recoverable), never a misleading
// lost / "child exited"; a dead pane keeps the genuine child-exited path.
func TestFailedAttachOutcome(t *testing.T) {
	state, reason, message := failedAttachOutcome(true)
	if state != "detached" {
		t.Fatalf("pane-alive state = %q, want detached", state)
	}
	if reason != "attach_failed_lane_alive" {
		t.Fatalf("pane-alive reason = %q", reason)
	}
	if !strings.Contains(message, "alive") || !strings.Contains(message, "rebridge") && !strings.Contains(message, "Rebridge") {
		t.Fatalf("pane-alive message not legible: %q", message)
	}

	state, reason, message = failedAttachOutcome(false)
	if state != "lost" {
		t.Fatalf("pane-dead state = %q, want lost", state)
	}
	if reason != "child exited before attach" {
		t.Fatalf("pane-dead reason = %q", reason)
	}
	if !strings.Contains(message, "child exited") {
		t.Fatalf("pane-dead message = %q", message)
	}
}

// TestResolveSupervisedCommandBinaryUsesPathPrefix verifies #223: a lane binary
// that lives only in the lane's path_prefix resolves without a wrapper, even
// when it is not on the daemon's augmented PATH.
func TestResolveSupervisedCommandBinaryUsesPathPrefix(t *testing.T) {
	prefixDir := t.TempDir()
	bin := filepath.Join(prefixDir, "agy")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Daemon PATH dirs deliberately do not contain agy.
	t.Setenv("STRIATUM_SUPERVISED_PATH_DIRS", t.TempDir())

	got := resolveSupervisedCommandBinary([]string{"agy", "--dangerously-skip-permissions"}, []string{prefixDir})
	want := []string{bin, "--dangerously-skip-permissions"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("resolved = %#v, want %#v", got, want)
	}
}

func TestLanePathPrefixParsing(t *testing.T) {
	got, err := lanePathPrefix(map[string]any{"path_prefix": []any{"/opt/agy/bin", "/usr/local/bin", "/opt/agy/bin"}})
	if err != nil {
		t.Fatalf("lanePathPrefix: %v", err)
	}
	want := []string{"/opt/agy/bin", "/usr/local/bin"} // deduped, order preserved
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("path_prefix = %#v, want %#v", got, want)
	}
	if _, err := lanePathPrefix(map[string]any{"path_prefix": []any{"relative/bin"}}); err == nil {
		t.Fatalf("expected error for relative path_prefix entry")
	}
	if _, err := lanePathPrefix(map[string]any{"path_prefix": "not-an-array"}); err == nil {
		t.Fatalf("expected error for non-array path_prefix")
	}
	if got, err := lanePathPrefix(map[string]any{}); err != nil || got != nil {
		t.Fatalf("absent path_prefix = (%#v, %v), want (nil, nil)", got, err)
	}
}

func TestLaneCommandEnvParsing(t *testing.T) {
	got, err := laneCommandEnv(map[string]any{"command_env": map[string]any{"AGY_HOME": "/opt/agy", "FOO": "bar"}})
	if err != nil {
		t.Fatalf("laneCommandEnv: %v", err)
	}
	if got["AGY_HOME"] != "/opt/agy" || got["FOO"] != "bar" {
		t.Fatalf("command_env = %#v", got)
	}
	if _, err := laneCommandEnv(map[string]any{"command_env": map[string]any{"PATH": "/x"}}); err == nil {
		t.Fatalf("expected error for command_env PATH")
	}
	if _, err := laneCommandEnv(map[string]any{"command_env": map[string]any{"STRIATUM_MCP_TOKEN": "x"}}); err == nil {
		t.Fatalf("expected error for STRIATUM_-namespaced command_env key")
	}
	if _, err := laneCommandEnv(map[string]any{"command_env": map[string]any{"FOO": 1}}); err == nil {
		t.Fatalf("expected error for non-string command_env value")
	}
}

func TestSupervisedPushCommandInjectsCodexMCPConfigBeforeExec(t *testing.T) {
	config := supervisionStartConfig{
		AgentLoopMode:   agentLoopModePush,
		Command:         []string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "-"},
		OriginalCommand: []string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "-"},
		RepoRoot:        t.TempDir(),
		CapabilityToken: "stok_session_secret",
	}
	got, err := supervisedPushCommand(config, []string{"STRIATUM_MCP_URL=http://127.0.0.1:42727/mcp"})
	if err != nil {
		t.Fatalf("supervisedPushCommand: %v", err)
	}
	wantPrefix := []string{
		"codex",
		"-c", `mcp_servers.striatum.url="http://127.0.0.1:42727/mcp"`,
		"-c", `mcp_servers.striatum.bearer_token_env_var="STRIATUM_MCP_TOKEN"`,
	}
	if len(got) < len(wantPrefix) || strings.Join(got[:len(wantPrefix)], "\x00") != strings.Join(wantPrefix, "\x00") {
		t.Fatalf("codex push command = %#v, want prefix %#v", got, wantPrefix)
	}
	if got[len(got)-1] != "-" {
		t.Fatalf("codex stdin marker moved: %#v", got)
	}
}

// TestSupervisedPushCommandRefusesCodexWithoutEndpoint is the #296 silent-fallback
// guard: a codex push lane that cannot resolve a live MCP endpoint must FAIL the
// launch (loud + recoverable) instead of degrading to a bare codex that silently
// no-ops against a dead/absent work-packet control plane.
func TestSupervisedPushCommandRefusesCodexWithoutEndpoint(t *testing.T) {
	config := supervisionStartConfig{
		AgentLoopMode:   agentLoopModePush,
		Command:         []string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "-"},
		OriginalCommand: []string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "-"},
		RepoRoot:        t.TempDir(), // no .striatum metadata → no endpoint
		CapabilityToken: "stok_session_secret",
	}
	// Empty env (and an isolated HOME) so ResolveMCPEndpoint finds no live endpoint.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	got, err := supervisedPushCommand(config, []string{})
	if err == nil {
		t.Fatalf("expected codex push launch to refuse without an endpoint, got command %#v", got)
	}
	if !strings.Contains(err.Error(), "codex push lane") {
		t.Fatalf("refusal message should name the codex push lane, got: %v", err)
	}
}

// TestSupervisedPushCommandRefusesCodexWithoutToken: even with a resolvable
// endpoint, a codex push lane with no session capability token cannot authenticate
// to MCP — refuse rather than launch a bare codex that can never claim work.
func TestSupervisedPushCommandRefusesCodexWithoutToken(t *testing.T) {
	config := supervisionStartConfig{
		AgentLoopMode:   agentLoopModePush,
		Command:         []string{"codex", "exec", "-"},
		OriginalCommand: []string{"codex", "exec", "-"},
		RepoRoot:        t.TempDir(),
		CapabilityToken: "", // no session-bound token
	}
	got, err := supervisedPushCommand(config, []string{"STRIATUM_MCP_URL=http://127.0.0.1:42727/mcp"})
	if err == nil {
		t.Fatalf("expected codex push launch to refuse without a capability token, got command %#v", got)
	}
	if !strings.Contains(err.Error(), "capability token") {
		t.Fatalf("refusal message should name the missing capability token, got: %v", err)
	}
}

// TestSupervisedPushCommandPassesNonCodexThrough confirms the refusal is scoped to
// codex push lanes: a non-codex push lane (or a self-driving lane) is returned
// verbatim with no error even when no endpoint resolves (its injection, if any,
// happens on a different path).
func TestSupervisedPushCommandPassesNonCodexThrough(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	config := supervisionStartConfig{
		AgentLoopMode:   agentLoopModePush,
		Command:         []string{"claude", "-"},
		OriginalCommand: []string{"claude", "-"},
		RepoRoot:        t.TempDir(),
	}
	got, err := supervisedPushCommand(config, []string{})
	if err != nil {
		t.Fatalf("non-codex push lane should pass through without error, got: %v", err)
	}
	if strings.Join(got, "\x00") != strings.Join(config.Command, "\x00") {
		t.Fatalf("non-codex push command should be verbatim, got %#v", got)
	}
}

func TestSupervisedRunAsExecEnvFiltersSensitiveFallbackEnv(t *testing.T) {
	got := supervisedRunAsExecEnv([]string{
		"PATH=/usr/bin",
		"STRIATUM_MCP_URL=http://127.0.0.1:1/mcp",
		"STRIATUM_MCP_TOKEN=stok_session_secret",
		"PROVIDER_API_KEY=provider_secret",
	})
	joined := strings.Join(got, "\x00")
	for _, needle := range []string{"STRIATUM_MCP_TOKEN=", "stok_session_secret", "PROVIDER_API_KEY=", "provider_secret"} {
		if strings.Contains(joined, needle) {
			t.Fatalf("sensitive env leaked through run-as argv fallback: found %q in %#v", needle, got)
		}
	}
	if !strings.Contains(joined, "STRIATUM_MCP_URL=http://127.0.0.1:1/mcp") {
		t.Fatalf("non-sensitive MCP URL missing from run-as env fallback: %#v", got)
	}
}

// TestSupervisedLaneEnvAppliesLaunchEnv verifies #223 end to end: the lane env
// prepends path_prefix to PATH and includes command_env, while the daemon's
// session-bound token stays authoritative and the adapter identity stays the
// declared lane adapter (not a wrapper).
func TestSupervisedLaneEnvAppliesLaunchEnv(t *testing.T) {
	config := supervisionStartConfig{
		RepoRoot:         t.TempDir(),
		RepositoryID:     "repo",
		RunID:            "run",
		SessionID:        "sess",
		LaneID:           "agy",
		OriginalCommand:  []string{"agy", "--dangerously-skip-permissions"},
		CapabilityToken:  "tok-123",
		LaunchPathPrefix: []string{"/opt/agy/bin"},
		LaunchEnv:        map[string]string{"AGY_HOME": "/opt/agy", "FOO": "bar"},
	}
	if config.adapterName() != "agy" {
		t.Fatalf("adapter = %q, want agy (declared lane adapter, not a wrapper)", config.adapterName())
	}
	env := supervisedLaneEnv(config, "sup-1")
	values := map[string]string{}
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok {
			values[k] = v
		}
	}
	if !strings.HasPrefix(values["PATH"], "/opt/agy/bin"+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want /opt/agy/bin prepended", values["PATH"])
	}
	if values["AGY_HOME"] != "/opt/agy" || values["FOO"] != "bar" {
		t.Fatalf("command_env not applied: AGY_HOME=%q FOO=%q", values["AGY_HOME"], values["FOO"])
	}
	if values["STRIATUM_MCP_TOKEN"] != "tok-123" {
		t.Fatalf("STRIATUM_MCP_TOKEN = %q, want the injected bound token", values["STRIATUM_MCP_TOKEN"])
	}
}

func TestSupervisedLaneEnvNormalizesMissingOrDumbTerminal(t *testing.T) {
	for _, base := range [][]string{
		{"PATH=/usr/bin"},
		{"PATH=/usr/bin", "TERM=dumb"},
	} {
		env := normalizeSupervisedTerminalEnv(base)
		if got := envValue(t, env, "TERM"); got != "xterm-256color" {
			t.Fatalf("TERM = %q for base %#v, want xterm-256color", got, base)
		}
	}
	env := normalizeSupervisedTerminalEnv([]string{"PATH=/usr/bin", "TERM=screen-256color"})
	if got := envValue(t, env, "TERM"); got != "screen-256color" {
		t.Fatalf("TERM = %q, want existing usable terminal preserved", got)
	}
}

func TestProviderAuthPreflightEnvKeepsSafeLaunchEnvOnly(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("STRIATUM_MCP_TOKEN", "shared-operator-token")
	t.Setenv("DATABASE_URL", "postgres://secret")
	config := supervisionStartConfig{
		LaunchPathPrefix: []string{"/opt/codex/bin"},
		LaunchEnv: map[string]string{
			"CODEX_HOME":     "/home/lane/.codex",
			"OPENAI_API_KEY": "must-not-leak",
		},
	}

	values := envValues(providerAuthPreflightEnv(config))
	if values["CODEX_HOME"] != "/home/lane/.codex" {
		t.Fatalf("CODEX_HOME = %q, want launch env value", values["CODEX_HOME"])
	}
	if !strings.HasPrefix(values["PATH"], "/opt/codex/bin"+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want path_prefix prepended", values["PATH"])
	}
	for _, forbidden := range []string{"STRIATUM_MCP_TOKEN", "DATABASE_URL", "OPENAI_API_KEY"} {
		if _, ok := values[forbidden]; ok {
			t.Fatalf("provider auth preflight env leaked %s: %#v", forbidden, values)
		}
	}
}

func TestProviderAuthPreflightEnvRunAsUsesLaneAuthHome(t *testing.T) {
	origLaneHome := laneOSUserHome
	t.Cleanup(func() { laneOSUserHome = origLaneHome })
	laneOSUserHome = func(name string) string {
		if name == "striatum-lane" {
			return "/home/striatum-lane"
		}
		return ""
	}
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("CODEX_HOME", "/home/operator/.codex")
	t.Setenv("XDG_CONFIG_HOME", "/home/operator/.config")
	t.Setenv("XDG_CACHE_HOME", "/home/operator/.cache")

	values := envValues(providerAuthPreflightEnv(supervisionStartConfig{RunAsUser: "striatum-lane"}))
	if values["HOME"] != "/home/striatum-lane" {
		t.Fatalf("HOME = %q, want lane user home", values["HOME"])
	}
	for _, forbidden := range []string{"CODEX_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		if _, ok := values[forbidden]; ok {
			t.Fatalf("run-as provider auth env inherited operator %s: %#v", forbidden, values)
		}
	}
	if authHome := laneproviderauth.ResolveAuthHome(laneproviderauth.ProviderCodex, providerAuthPreflightEnv(supervisionStartConfig{RunAsUser: "striatum-lane"})); authHome != "/home/striatum-lane/.codex" {
		t.Fatalf("auth home = %q, want lane HOME/.codex", authHome)
	}

	withLaunchHome := envValues(providerAuthPreflightEnv(supervisionStartConfig{
		RunAsUser: "striatum-lane",
		LaunchEnv: map[string]string{
			"CODEX_HOME": "/srv/lane-codex",
		},
	}))
	if withLaunchHome["CODEX_HOME"] != "/srv/lane-codex" {
		t.Fatalf("LaunchEnv CODEX_HOME = %q, want explicit lane value", withLaunchHome["CODEX_HOME"])
	}
}

func TestSupervisedEnvAddsOperatorLocalBinsToPath(t *testing.T) {
	home := t.TempDir()
	localBin := filepath.Join(home, ".local", "bin")
	npmBin := filepath.Join(home, ".npm-global", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatalf("mkdir local bin: %v", err)
	}
	if err := os.MkdirAll(npmBin, 0o755); err != nil {
		t.Fatalf("mkdir npm bin: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", strings.Join([]string{"/usr/local/bin", "/usr/bin"}, string(os.PathListSeparator)))
	t.Setenv("STRIATUM_SUPERVISED_PATH_DIRS", "")

	entries := supervisedEnvEntries("claude", "/repo", "repo_1", "run_1", "sess_1", "sup_1", "lane_1", "")
	path := envValue(t, entries, "PATH")
	want := strings.Join([]string{"/usr/local/bin", "/usr/bin", localBin, npmBin}, string(os.PathListSeparator))
	if path != want {
		t.Fatalf("PATH = %q, want %q", path, want)
	}
	if countEnv(entries, "PATH") != 1 {
		t.Fatalf("supervisedEnvEntries PATH count = %d, want 1", countEnv(entries, "PATH"))
	}
	if countEnv(supervisedEnv("claude", "/repo", "repo_1", "run_1", "sess_1", "sup_1", "lane_1", ""), "PATH") != 1 {
		t.Fatalf("supervisedEnv should emit exactly one effective PATH")
	}
	for _, key := range []string{
		"STRIATUM_REPOSITORY_ID",
		"STRIATUM_RUN_ID",
		"STRIATUM_SESSION_ID",
		"STRIATUM_SUPERVISOR_ID",
		"STRIATUM_REPO",
		"STRIATUM_LANE_ID",
	} {
		if envValue(t, entries, key) == "" {
			t.Fatalf("%s missing from supervised env entries: %#v", key, entries)
		}
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry, "STRIATUM_SUPERVISED_PATH_DIRS=") {
			t.Fatalf("override trust-boundary env leaked through supervisedEnvEntries: %#v", entries)
		}
	}
}

func TestSupervisedEnvPathOverrideAppendsExistingAbsoluteDirs(t *testing.T) {
	baseDir := t.TempDir()
	customOne := filepath.Join(baseDir, "custom-one")
	customTwo := filepath.Join(baseDir, "custom-two")
	if err := os.MkdirAll(customOne, 0o755); err != nil {
		t.Fatalf("mkdir custom one: %v", err)
	}
	if err := os.MkdirAll(customTwo, 0o755); err != nil {
		t.Fatalf("mkdir custom two: %v", err)
	}
	missing := filepath.Join(baseDir, "missing")
	t.Setenv("PATH", strings.Join([]string{"/usr/bin", customOne}, string(os.PathListSeparator)))
	t.Setenv("STRIATUM_SUPERVISED_PATH_DIRS", strings.Join([]string{
		customOne,
		missing,
		"relative/bin",
		customTwo,
	}, string(os.PathListSeparator)))

	entries := supervisedEnvEntries("claude", "/repo", "repo_1", "run_1", "sess_1", "sup_1", "lane_1", "")
	path := envValue(t, entries, "PATH")
	want := strings.Join([]string{"/usr/bin", customOne, customTwo}, string(os.PathListSeparator))
	if path != want {
		t.Fatalf("PATH = %q, want %q", path, want)
	}
}

// TestSupervisedEnvExcludesDaemonSecrets is the #87 / RFC 0096 §2 regression:
// the supervised lane env must be built from an explicit allowlist, NOT from
// the daemon's os.Environ(), so a Postgres DSN / DATABASE_URL / any
// secret-looking daemon var never leaks into the lane (and its pane). It also
// pins that the required STRIATUM_* run/session vars and the agent-loop MCP
// bootstrap vars DO pass through, and that an unexpected pass-through var widens
// the allowlist only on purpose (this test fails loudly if it does).
func TestSupervisedEnvExcludesDaemonSecrets(t *testing.T) {
	// Plant secret-looking daemon vars + an arbitrary non-allowlisted var.
	for k, v := range map[string]string{
		"STRIATUM_POSTGRES_DSN": "postgres://striatumd_rw:hunter2@/striatum?host=/var/run/postgresql",
		"DATABASE_URL":          "postgres:///striatum",
		"PGPASSWORD":            "hunter2",
		"PGHOST":                "/var/run/postgresql",
		"AWS_SECRET_ACCESS_KEY": "should-not-leak",
		"SOME_RANDOM_VAR":       "irrelevant",
	} {
		t.Setenv(k, v)
	}
	// Plant the bootstrap vars the lane genuinely needs so we can assert they
	// pass through. STRIATUM_MCP_TOKEN here is the daemon's SHARED operator
	// override; #135 requires it be DROPPED (the lane gets its injected bound
	// token instead, not this one).
	t.Setenv("STRIATUM_MCP_URL", "http://127.0.0.1:9999/mcp/sse")
	t.Setenv("STRIATUM_MCP_TOKEN", "shared-override-bearer")
	t.Setenv("HOME", "/home/lane")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("LC_ALL", "en_US.UTF-8")

	const boundToken = "stok_bound.session-secret-not-real"
	env := supervisedEnv("claude", "/repo", "repo_1", "run_1", "sess_1", "sup_1", "lane_1", boundToken)

	// 0. #101: the claude lane carries the welcome/update-nag suppression keys.
	for key, want := range map[string]string{
		"DISABLE_AUTOUPDATER":                      "1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	} {
		if got := envValue(t, env, key); got != want {
			t.Fatalf("#101 claude suppression %s = %q, want %q", key, got, want)
		}
	}

	// 1. No daemon secret leaks through.
	for _, banned := range []string{
		"STRIATUM_POSTGRES_DSN",
		"DATABASE_URL",
		"PGPASSWORD",
		"PGHOST",
		"AWS_SECRET_ACCESS_KEY",
		"SOME_RANDOM_VAR",
	} {
		if hasEnvKey(env, banned) {
			t.Fatalf("daemon secret %s leaked into supervised lane env: %#v", banned, env)
		}
	}
	// Belt-and-suspenders: no entry should contain the secret value, regardless
	// of key.
	for _, entry := range env {
		if strings.Contains(entry, "hunter2") || strings.Contains(entry, "should-not-leak") {
			t.Fatalf("secret value leaked into supervised lane env entry: %q", entry)
		}
	}

	// 2. Required STRIATUM_* run/session vars are present and correct.
	for key, want := range map[string]string{
		"STRIATUM_REPOSITORY_ID": "repo_1",
		"STRIATUM_RUN_ID":        "run_1",
		"STRIATUM_SESSION_ID":    "sess_1",
		"STRIATUM_SUPERVISOR_ID": "sup_1",
		"STRIATUM_REPO":          "/repo",
		"STRIATUM_LANE_ID":       "lane_1",
	} {
		if got := envValue(t, env, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}

	// 3. Agent-loop MCP endpoint + OS basics pass through.
	for key, want := range map[string]string{
		"STRIATUM_MCP_URL": "http://127.0.0.1:9999/mcp/sse",
		"HOME":             "/home/lane",
		"TERM":             "xterm-256color",
		"LC_ALL":           "en_US.UTF-8",
	} {
		if got := envValue(t, env, key); got != want {
			t.Fatalf("expected pass-through %s = %q, got %q", key, want, got)
		}
	}

	// 3b. #135: the lane's bearer is the injected session-bound token, and the
	// daemon's SHARED override token is dropped (not passed through).
	if got := envValue(t, env, "STRIATUM_MCP_TOKEN"); got != boundToken {
		t.Fatalf("STRIATUM_MCP_TOKEN = %q, want the injected bound token %q (shared override must be dropped)", got, boundToken)
	}
	for _, entry := range env {
		if strings.Contains(entry, "shared-override-bearer") {
			t.Fatalf("daemon shared override token leaked into supervised lane env: %q", entry)
		}
	}

	// 4. PATH is present exactly once.
	if countEnv(env, "PATH") != 1 {
		t.Fatalf("supervisedEnv should emit exactly one PATH, got %d", countEnv(env, "PATH"))
	}
}

func TestSupervisedLaneEnvRunAsDropsDaemonIdentity(t *testing.T) {
	origHome := laneOSUserHome
	t.Cleanup(func() { laneOSUserHome = origHome })
	laneOSUserHome = func(name string) string {
		if name == "striatum-lane" {
			return "/var/lib/striatum-lane"
		}
		return ""
	}
	pathDir := t.TempDir()
	for k, v := range map[string]string{
		"HOME":                           "/home/daemon",
		"USER":                           "daemonuser",
		"LOGNAME":                        "daemonuser",
		"PATH":                           "/usr/bin",
		"XDG_CONFIG_HOME":                "/home/daemon/.config",
		"XDG_CACHE_HOME":                 "/home/daemon/.cache",
		"SSH_AUTH_SOCK":                  "/run/user/1000/ssh-agent",
		"STRIATUM_MCP_URL":               "http://127.0.0.1:9999/mcp",
		"STRIATUM_MCP_TOKEN":             "shared-override-bearer",
		"DATABASE_URL":                   "postgres:///striatumd",
		"PGHOST":                         "/var/run/postgresql",
		"TERM":                           "xterm-256color",
		"LC_ALL":                         "en_US.UTF-8",
		"STRIATUM_SUPERVISED_PATH_DIRS":  pathDir,
		"STRIATUM_SUPERVISED_STDERR_LOG": "/tmp/daemon-debug.log",
		"STRIATUM_DAEMON_SOCKET":         "/run/user/1000/striatum/daemon-go.sock",
	} {
		t.Setenv(k, v)
	}
	const boundToken = "stok_bound.session-secret-not-real"
	env := supervisedLaneEnv(supervisionStartConfig{
		RepositoryID:    "repo_1",
		RunID:           "run_1",
		SessionID:       "sess_1",
		LaneID:          "lane_1",
		RepoRoot:        "/repo",
		Command:         []string{"/bin/true"},
		OriginalCommand: []string{"claude"},
		RunAsUser:       "striatum-lane",
		CapabilityToken: boundToken,
	}, "sup_1")

	for _, banned := range []string{
		"DATABASE_URL",
		"PGHOST",
		"XDG_CONFIG_HOME",
		"XDG_CACHE_HOME",
		"SSH_AUTH_SOCK",
		"STRIATUM_SUPERVISED_STDERR_LOG",
	} {
		if hasEnvKey(env, banned) {
			t.Fatalf("run-as lane env leaked %s: %#v", banned, env)
		}
	}
	for key, want := range map[string]string{
		"HOME":                   "/var/lib/striatum-lane",
		"USER":                   "striatum-lane",
		"LOGNAME":                "striatum-lane",
		"STRIATUM_MCP_URL":       "http://127.0.0.1:9999/mcp",
		"STRIATUM_MCP_TOKEN":     boundToken,
		"STRIATUM_RUN_ID":        "run_1",
		"STRIATUM_DAEMON_SOCKET": "/run/user/1000/striatum/daemon-go.sock",
		"TERM":                   "xterm-256color",
		"LC_ALL":                 "en_US.UTF-8",
	} {
		if got := envValue(t, env, key); got != want {
			t.Fatalf("%s = %q, want %q in run-as env: %#v", key, got, want, env)
		}
	}
	for _, entry := range env {
		if strings.Contains(entry, "shared-override-bearer") || strings.Contains(entry, "/home/daemon") {
			t.Fatalf("daemon identity/secret leaked into run-as env: %q", entry)
		}
	}
}

func TestSupervisedLaneEnvRunAsDerivesDaemonSocketFromXDGRuntimeDir(t *testing.T) {
	origHome := laneOSUserHome
	t.Cleanup(func() { laneOSUserHome = origHome })
	laneOSUserHome = func(name string) string {
		if name == "striatum-lane" {
			return "/var/lib/striatum-lane"
		}
		return ""
	}
	for k, v := range map[string]string{
		"HOME":                          "/home/daemon",
		"USER":                          "daemonuser",
		"LOGNAME":                       "daemonuser",
		"PATH":                          "/usr/bin",
		"XDG_RUNTIME_DIR":               "/run/user/1000",
		"STRIATUM_DAEMON_SOCKET":        "",
		"STRIATUM_DAEMON_RUNTIME_DIR":   "",
		"STRIATUM_MCP_URL":              "http://127.0.0.1:9999/mcp",
		"STRIATUM_SUPERVISED_PATH_DIRS": t.TempDir(),
	} {
		t.Setenv(k, v)
	}

	env := supervisedLaneEnv(supervisionStartConfig{
		RepositoryID:    "repo_1",
		RunID:           "run_1",
		SessionID:       "sess_1",
		LaneID:          "lane_1",
		RepoRoot:        "/repo",
		Command:         []string{"/bin/true"},
		OriginalCommand: []string{"codex"},
		RunAsUser:       "striatum-lane",
		CapabilityToken: "stok_bound.session-secret-not-real",
	}, "sup_1")

	want := "/run/user/1000/striatum/daemon-go.sock"
	if got := envValue(t, env, "STRIATUM_DAEMON_SOCKET"); got != want {
		t.Fatalf("STRIATUM_DAEMON_SOCKET = %q, want %q in run-as env: %#v", got, want, env)
	}
}

func TestSupervisedLaneEnvUsesRegisteredDaemonSocketPath(t *testing.T) {
	origSocket := packageDaemonSocketPath
	t.Cleanup(func() { packageDaemonSocketPath = origSocket })
	packageDaemonSocketPath = "/run/user/1000/striatum/daemon-go.sock"

	t.Setenv("XDG_RUNTIME_DIR", "/run/user/9999")
	t.Setenv("STRIATUM_DAEMON_SOCKET", "/tmp/stale.sock")
	env := supervisedEnvEntries(
		"codex",
		"/repo",
		"repo_1",
		"run_1",
		"sess_1",
		"sup_1",
		"codex",
		"stok_bound.session-secret-not-real",
	)

	if got := envValue(t, env, "STRIATUM_DAEMON_SOCKET"); got != "/run/user/1000/striatum/daemon-go.sock" {
		t.Fatalf("STRIATUM_DAEMON_SOCKET = %q, want registered daemon socket in env: %#v", got, env)
	}
}

func hasEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

// TestUpdateSupervisorStateCleansLaneMCPConfigOnEveryTeardown is the #70 / RFC
// 0096 §3 regression: the agy lane writes its MCP bearer token into the repo's
// .gemini/settings.json, and that file must be removed on EVERY teardown path.
// Cleanup is centralized at the supervisor terminal-state transition
// (updateSupervisorState → cleanupSupervisorLaneMCPConfig), so this drives that
// transition for each terminal state (stopped / lost / failed — the paths hit by
// graceful exit, supervise stop, and tmux kill/lost) and asserts the token file
// is gone, plus the non-terminal "attached" case which must NOT clean.
func TestUpdateSupervisorStateCleansLaneMCPConfigOnEveryTeardown(t *testing.T) {
	// plant seeds the per-launch lane operational files a teardown must clean:
	// the agy .gemini/settings.json token file (#62) and the Claude
	// .claude/scheduled_tasks.lock (#129), plus an unrelated operator-owned
	// .claude file that must be preserved.
	plant := func(repo string) (settingsPath, lockPath, operatorClaude string) {
		t.Helper()
		settingsPath = filepath.Join(repo, ".gemini", "settings.json")
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
			t.Fatalf("mkdir .gemini: %v", err)
		}
		// Simulate the per-launch token file the agy adapter wrote.
		if err := os.WriteFile(settingsPath, []byte(`{"mcpServers":{"striatum":{"headers":{"Authorization":"Bearer secret"}}}}`), 0o600); err != nil {
			t.Fatalf("write settings: %v", err)
		}
		// Simulate the "created (no pre-existing)" scratch marker so cleanup
		// removes rather than restores.
		scratch := filepath.Join(repo, ".striatum", "scratch", "sup_1")
		if err := os.MkdirAll(scratch, 0o755); err != nil {
			t.Fatalf("mkdir scratch: %v", err)
		}
		if err := os.WriteFile(filepath.Join(scratch, "settings.json.created"), nil, 0o600); err != nil {
			t.Fatalf("write created marker: %v", err)
		}
		// Simulate the Claude lane's ephemeral lock + an operator .claude file.
		claudeDir := filepath.Join(repo, ".claude")
		if err := os.MkdirAll(claudeDir, 0o755); err != nil {
			t.Fatalf("mkdir .claude: %v", err)
		}
		lockPath = filepath.Join(claudeDir, "scheduled_tasks.lock")
		if err := os.WriteFile(lockPath, []byte("lock"), 0o600); err != nil {
			t.Fatalf("write lock: %v", err)
		}
		operatorClaude = filepath.Join(claudeDir, "settings.json")
		if err := os.WriteFile(operatorClaude, []byte(`{"operator":true}`), 0o600); err != nil {
			t.Fatalf("write operator .claude file: %v", err)
		}
		return settingsPath, lockPath, operatorClaude
	}

	for _, state := range []string{"stopped", "lost", "failed"} {
		t.Run("terminal_"+state, func(t *testing.T) {
			repo := t.TempDir()
			settingsPath, lockPath, operatorClaude := plant(repo)
			tx := &superviseControlFakeTx{repoRoot: repo}
			endedAt := "2026-05-30T00:00:00Z"
			reason := "teardown via " + state
			if err := updateSupervisorState(context.Background(), tx, "repo_1", "sup_1", "dsup_1", state, endedAt, 0, "", "", &endedAt, &reason); err != nil {
				t.Fatalf("updateSupervisorState(%s): %v", state, err)
			}
			if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
				t.Fatalf("state %s left the token file on disk: stat err = %v", state, err)
			}
			if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
				t.Fatalf("state %s left .claude/scheduled_tasks.lock on disk: stat err = %v", state, err)
			}
			if _, err := os.Stat(operatorClaude); err != nil {
				t.Fatalf("state %s removed unrelated operator .claude file: %v", state, err)
			}
		})
	}

	t.Run("non_terminal_attached_keeps_file", func(t *testing.T) {
		repo := t.TempDir()
		settingsPath, lockPath, _ := plant(repo)
		tx := &superviseControlFakeTx{repoRoot: repo}
		if err := updateSupervisorState(context.Background(), tx, "repo_1", "sup_1", "dsup_1", "attached", "2026-05-30T00:00:00Z", 123, "tok", "", nil, nil); err != nil {
			t.Fatalf("updateSupervisorState(attached): %v", err)
		}
		if _, err := os.Stat(settingsPath); err != nil {
			t.Fatalf("non-terminal transition removed the live token file: %v", err)
		}
		if _, err := os.Stat(lockPath); err != nil {
			t.Fatalf("non-terminal transition removed the live .claude lock: %v", err)
		}
	})
}

func TestSuperviseSendDeliversPacketUnacknowledged(t *testing.T) {
	dir := t.TempDir()
	pipePath := dir + "/stdin.pipe"
	if err := syscall.Mkfifo(pipePath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	readerFD, err := syscall.Open(pipePath, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open FIFO reader: %v", err)
	}
	reader := os.NewFile(uintptr(readerFD), "stdin.pipe.reader")
	defer func() { _ = reader.Close() }()

	tx := &superviseControlFakeTx{pipePath: pipePath, pid: os.Getpid()}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}}
	result, err := HandleSuperviseSend(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_send",
		Method:        "supervise.send",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"packet_id":     "packet_1",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseSend: %v", err)
	}
	if result["delivery_state"] != "delivered_unacknowledged" || result["control_ack_expected"] != true {
		t.Fatalf("send result = %#v", result)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read FIFO payload: %v", err)
	}
	var packet map[string]any
	if err := json.Unmarshal(body, &packet); err != nil {
		t.Fatalf("packet json = %q: %v", string(body), err)
	}
	if packet["packet"] != "body" {
		t.Fatalf("delivered packet = %#v", packet)
	}
	if !tx.sawExec("UPDATE striatumd.process_supervisors", "heartbeat_at") {
		t.Fatalf("missing heartbeat update: %#v", tx.execs)
	}
	event := tx.lastEventInsert()
	if event == nil || event.args[3] != "supervisor.packet_delivered" {
		t.Fatalf("event insert = %#v", event)
	}
	payload := event.args[9].(map[string]any)
	if payload["packet_id"] != "packet_1" || payload["stdin_delivery"] != stdinDeliveryPersistentFIFO {
		t.Fatalf("event payload = %#v", payload)
	}
}

func TestOneShotPipeStdinSeesEOFAfterHoldReleased(t *testing.T) {
	dir := t.TempDir()
	pipePath := filepath.Join(dir, "stdin.pipe")
	if err := syscall.Mkfifo(pipePath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	reader, cleanup, err := openOneShotPipeStdin(pipePath)
	if err != nil {
		t.Fatalf("openOneShotPipeStdin: %v", err)
	}
	defer cleanup()

	readDone := make(chan struct {
		body []byte
		err  error
	}, 1)
	go func() {
		body, err := io.ReadAll(reader)
		readDone <- struct {
			body []byte
			err  error
		}{body: body, err: err}
	}()

	if n, buffered, err := writeToPipe(context.Background(), pipePath, []byte("packet\n")); err != nil || n != len("packet\n") || buffered {
		t.Fatalf("writeToPipe n=%d buffered=%v err=%v", n, buffered, err)
	}
	select {
	case got := <-readDone:
		t.Fatalf("reader saw EOF before one-shot hold release: body=%q err=%v", got.body, got.err)
	case <-time.After(50 * time.Millisecond):
	}

	releaseOneShotFIFOHold(pipePath)
	select {
	case got := <-readDone:
		if got.err != nil {
			t.Fatalf("readAll: %v", got.err)
		}
		if string(got.body) != "packet\n" {
			t.Fatalf("body = %q", string(got.body))
		}
	case <-time.After(time.Second):
		t.Fatalf("reader did not see EOF after one-shot hold release")
	}
}

func TestSuperviseSendWrongKindPacketIDPointsAtClaimNextPacketID(t *testing.T) {
	tx := &superviseControlFakeTx{}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}}
	_, err := HandleSuperviseSend(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_send_wrong_id",
		Method:        "supervise.send",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"packet_id":     "msg_123",
		},
	})
	if err == nil {
		t.Fatalf("expected supervise send to reject wrong-kind packet id")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "not_found" {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(rpcErr.Message, "msg_123 is a message id, not a work packet id") ||
		!strings.Contains(rpcErr.Message, "data.packet_id") ||
		!strings.Contains(rpcErr.Message, "data.packet.packet_id") {
		t.Fatalf("message = %q", rpcErr.Message)
	}
}

// TestSuperviseSendIgnoresBenignAttachClientExited guards #63 F7: a lane whose
// only recorded delivery degradation is attach_client_exited (the tmux
// attach-session OBSERVER client exiting) must NOT block delivery while the
// pane is alive and the real transport is healthy. Here the live PID backs the
// probe as alive, so the benign attach-observer exit is reconciled away and the
// send is allowed to proceed past the delivery-degraded gate (it then fails at
// the missing pipe, which is the next stage — proving the gate did not reject).
func TestSuperviseSendIgnoresBenignAttachClientExited(t *testing.T) {
	tx := &superviseControlFakeTx{
		pipePath: "/tmp/no-write-expected",
		pid:      os.Getpid(),
		metadata: map[string]any{
			"stdin_delivery": stdinDeliveryPersistentFIFO,
			"tmux": map[string]any{
				"delivery_liveness": map[string]any{
					"class":   "degraded",
					"healthy": false,
					"reason":  "attach_client_exited",
				},
			},
		},
	}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}}
	_, err := HandleSuperviseSend(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_send_benign_attach_exit",
		Method:        "supervise.send",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"packet_id":     "packet_1",
		},
	})
	if err == nil {
		t.Fatalf("expected a downstream error (missing pipe), not nil")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok {
		t.Fatalf("err = %#v", err)
	}
	if strings.Contains(rpcErr.Message, "delivery is degraded") {
		t.Fatalf("benign attach_client_exited must not block delivery: %q", rpcErr.Message)
	}
	if !strings.Contains(rpcErr.Message, "stdin pipe is missing") {
		t.Fatalf("expected delivery to proceed to pipe write, got %q", rpcErr.Message)
	}
}

// TestSuperviseSendRejectsGenuineTransportFailureWithAttachExit guards the
// converse of #63 F7: even when the persisted delivery_liveness reason is the
// benign attach_client_exited, a genuinely broken transport (here a dead helper
// PID) must STILL block delivery. The lanehealth checker overrides the reason
// to helper_process_gone when the helper process is gone, so the gate rejects.
func TestSuperviseSendRejectsGenuineTransportFailureWithAttachExit(t *testing.T) {
	tx := &superviseControlFakeTx{
		pipePath: "/tmp/no-write-expected",
		pid:      os.Getpid(),
		metadata: map[string]any{
			"stdin_delivery":        stdinDeliveryPersistentFIFO,
			"helper_pid":            999999999, // not a live process
			"helper_pid_start_time": "",
			"tmux": map[string]any{
				"delivery_liveness": map[string]any{
					"class":   "degraded",
					"healthy": false,
					"reason":  "attach_client_exited",
				},
			},
		},
	}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}}
	_, err := HandleSuperviseSend(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_send_helper_gone",
		Method:        "supervise.send",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"packet_id":     "packet_1",
		},
	})
	if err == nil {
		t.Fatalf("expected supervise send to reject genuinely degraded supervisor")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" || !strings.Contains(rpcErr.Message, "delivery is degraded: helper_process_gone") {
		t.Fatalf("err = %#v", err)
	}
	if len(tx.eventInserts()) != 0 {
		t.Fatalf("degraded supervisor should not record delivery: %#v", tx.execs)
	}
}

func TestSuperviseSendRejectsRootDeliveryDegradedSupervisor(t *testing.T) {
	tx := &superviseControlFakeTx{
		pipePath: "/tmp/no-write-expected",
		pid:      os.Getpid(),
		metadata: map[string]any{
			"stdin_delivery": stdinDeliveryPersistentFIFO,
			"delivery_liveness": map[string]any{
				"class":   "degraded",
				"healthy": false,
				"reason":  "stdin_reader_missing",
			},
		},
	}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}}
	_, err := HandleSuperviseSend(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_send_root_degraded",
		Method:        "supervise.send",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"packet_id":     "packet_1",
		},
	})
	if err == nil {
		t.Fatalf("expected supervise send to reject root delivery-degraded supervisor")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" || !strings.Contains(rpcErr.Message, "delivery is degraded: stdin_reader_missing") {
		t.Fatalf("err = %#v", err)
	}
	if len(tx.eventInserts()) != 0 {
		t.Fatalf("degraded supervisor should not record delivery: %#v", tx.execs)
	}
}

// TestSuperviseSendRejectsAbruptHelperDeathWithoutMetadataRecord guards the
// #63 F10 fix. A helper/transport that dies abruptly WITHOUT writing a
// delivery_liveness metadata record (the main lane PID is still alive, so the
// live probe reports the lane alive) used to slip the old metadata-keyed gate
// and dispatch a packet to a dead FIFO. The lanehealth checker still detects
// the dead helper PID (DeliveryReason=helper_process_gone, Deliverable=false),
// so the gate — now keyed purely on the live probe — must reject. There is NO
// tmux.delivery_liveness or root delivery_liveness record here on purpose.
func TestSuperviseSendRejectsAbruptHelperDeathWithoutMetadataRecord(t *testing.T) {
	tx := &superviseControlFakeTx{
		pipePath: "/tmp/no-write-expected",
		pid:      os.Getpid(),
		metadata: map[string]any{
			"stdin_delivery":        stdinDeliveryPersistentFIFO,
			"helper_pid":            999999999, // not a live process
			"helper_pid_start_time": "",
			// Deliberately NO delivery_liveness record (tmux or root): this is
			// the abrupt-death case the old metadata-keyed gate missed.
		},
	}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}}
	_, err := HandleSuperviseSend(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_send_abrupt_helper_death",
		Method:        "supervise.send",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"packet_id":     "packet_1",
		},
	})
	if err == nil {
		t.Fatalf("expected supervise send to reject lane with a dead helper PID")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" || !strings.Contains(rpcErr.Message, "delivery is degraded: helper_process_gone") {
		t.Fatalf("err = %#v", err)
	}
	if len(tx.eventInserts()) != 0 {
		t.Fatalf("rejected delivery must not record a packet event: %#v", tx.execs)
	}
}

// TestSuperviseSendDeliversWhenMetadataDegradedButProbeDeliverable guards the
// #63 F7 direction and the F10 over-rejection risk: when the supervisor
// metadata still carries a benign attach_client_exited delivery_liveness record
// (degraded by the stale-metadata view) but the live probe reconciles the lane
// to health.Deliverable == true, the live probe wins and delivery SUCCEEDS. The
// real FIFO has a reader so the packet is actually written end-to-end, proving
// the gate did not over-reject.
func TestSuperviseSendDeliversWhenMetadataDegradedButProbeDeliverable(t *testing.T) {
	dir := t.TempDir()
	pipePath := dir + "/stdin.pipe"
	if err := syscall.Mkfifo(pipePath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	readerFD, err := syscall.Open(pipePath, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open FIFO reader: %v", err)
	}
	reader := os.NewFile(uintptr(readerFD), "stdin.pipe.reader")
	defer func() { _ = reader.Close() }()

	tx := &superviseControlFakeTx{
		pipePath: pipePath,
		pid:      os.Getpid(),
		metadata: map[string]any{
			"stdin_delivery": stdinDeliveryPersistentFIFO,
			"tmux": map[string]any{
				"delivery_liveness": map[string]any{
					"class":   "degraded",
					"healthy": false,
					"reason":  "attach_client_exited",
				},
			},
		},
	}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}}
	result, err := HandleSuperviseSend(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_send_metadata_degraded_probe_live",
		Method:        "supervise.send",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"packet_id":     "packet_1",
		},
	})
	if err != nil {
		t.Fatalf("benign attach_client_exited with a live probe must deliver: %v", err)
	}
	if result["delivery_state"] != "delivered_unacknowledged" {
		t.Fatalf("send result = %#v", result)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read FIFO payload: %v", err)
	}
	var packet map[string]any
	if err := json.Unmarshal(body, &packet); err != nil {
		t.Fatalf("packet json = %q: %v", string(body), err)
	}
	if packet["packet"] != "body" {
		t.Fatalf("delivered packet = %#v", packet)
	}
}

func TestTmuxMetadataFromHelperEventsPreservesLaunchAttachExit(t *testing.T) {
	tmux := tmuxMetadataFromHelperEvents([]map[string]any{
		{
			"event_type": gosupervisor.HelperEventAgentStarted,
			"timestamp":  "2026-05-28T21:03:21Z",
			"payload": map[string]any{
				"pid": 1001,
				"metadata": map[string]any{
					"tmux": map[string]any{
						"state":            "backed",
						"session_name":     "striatum-run",
						"pane_id":          "%1",
						"pane_pid":         1001,
						"pane_start_token": "start-1",
					},
				},
			},
		},
		{
			"event_type": gosupervisor.HelperEventAttachExited,
			"timestamp":  "2026-05-28T21:03:22Z",
			"payload": map[string]any{
				"attach_client_pid": 2002,
				"attach_exit_code":  1,
				"delivery_degraded": true,
				"observed_at":       "2026-05-28T21:03:22Z",
				"pid":               1001,
				"tmux_liveness":     "tmux_ok",
			},
		},
	})
	if tmux == nil {
		t.Fatalf("missing tmux metadata")
	}
	delivery := tmux["delivery_liveness"].(map[string]any)
	if delivery["class"] != "degraded" || delivery["healthy"] != false || delivery["reason"] != "attach_client_exited" {
		t.Fatalf("delivery liveness = %#v", delivery)
	}
	lastExit := tmux["attach_client_last_exit"].(map[string]any)
	if lastExit["attach_pid"] != 2002 || lastExit["attach_exit_code"] != 1 || lastExit["pane_pid"] != 1001 {
		t.Fatalf("last exit = %#v", lastExit)
	}
}

func TestSuperviseSendMarksDeliveryDegradedWhenPipeHasNoReader(t *testing.T) {
	origRunner := supervisionTmuxRunner
	defer func() { supervisionTmuxRunner = origRunner }()
	supervisionTmuxRunner = superviseReportFakeTmuxRunner{
		display: "%4|" + strconv.Itoa(os.Getpid()) + "|0|",
	}

	dir := t.TempDir()
	pipePath := filepath.Join(dir, "stdin.pipe")
	if err := syscall.Mkfifo(pipePath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	metadata := map[string]any{
		"stdin_delivery": stdinDeliveryPersistentFIFO,
		"tmux": map[string]any{
			"state":        "backed",
			"session_name": "striatum-run",
			"pane_id":      "%4",
			"pane_pid":     os.Getpid(),
		},
	}

	// 10 successful buffered sends, plus 1 failing overflow send, plus 1 degradation update tx
	var txs []*superviseControlFakeTx
	for i := 0; i < 11; i++ {
		txs = append(txs, &superviseControlFakeTx{pipePath: pipePath, pid: os.Getpid(), metadata: metadata})
	}
	degradeTx := &superviseControlFakeTx{metadata: metadata}
	txs = append(txs, degradeTx)

	runner := &superviseControlFakeRunner{txs: txs}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Perform 10 successful sends (which are buffered)
	for i := 1; i <= 10; i++ {
		_, err := HandleSuperviseSend(ctx, runner, rpc.Envelope{
			SchemaVersion: rpc.SupportedEnvelopeVersion,
			RequestID:     "req_send_no_reader_" + strconv.Itoa(i),
			Method:        "supervise.send",
			Params: map[string]any{
				"repository_id": "repo_1",
				"session_id":    "sess_1",
				"packet_id":     "packet_" + strconv.Itoa(i),
			},
		})
		if err != nil {
			t.Fatalf("send %d failed unexpectedly: %v", i, err)
		}
	}

	// The 11th send must fail with queue overflow / degraded status
	_, err := HandleSuperviseSend(ctx, runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_send_no_reader_11",
		Method:        "supervise.send",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"packet_id":     "packet_11",
		},
	})
	if err == nil {
		t.Fatalf("expected 11th supervise send to fail due to buffer overflow")
	}

	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" || !strings.Contains(rpcErr.Message, "delivery is degraded: stdin_reader_missing") {
		t.Fatalf("err = %#v", err)
	}

	// Verify that the failing tx (the 11th) rolled back, and the degrade update tx (the 12th) committed
	if !txs[10].rolledBack || !degradeTx.committed {
		t.Fatalf("transactions rollback/commit = tx11:%v degradeTx:%v", txs[10].rolledBack, degradeTx.committed)
	}

	update := degradeTx.pointerMetadataUpdate()
	if update == nil {
		t.Fatalf("missing persisted delivery degradation metadata update: %#v", degradeTx.execs)
	}
	updated := update.args[0].(map[string]any)
	tmux := updated["tmux"].(map[string]any)
	delivery := tmux["delivery_liveness"].(map[string]any)
	if delivery["class"] != "degraded" || delivery["healthy"] != false || delivery["reason"] != "stdin_reader_missing" {
		t.Fatalf("delivery liveness = %#v", delivery)
	}
	if len(txs[10].eventInserts()) != 0 {
		t.Fatalf("missing-reader send should not record packet delivery: %#v", txs[10].execs)
	}
}

func TestSuperviseRebridgeRefusesDeadPane(t *testing.T) {
	origRunner := supervisionTmuxRunner
	defer func() { supervisionTmuxRunner = origRunner }()
	panePID := os.Getpid()
	supervisionTmuxRunner = superviseReportFakeTmuxRunner{
		display: "%4|" + strconv.Itoa(panePID) + "|1|",
	}

	dir := t.TempDir()
	pipePath := filepath.Join(dir, "stdin.pipe")
	tx := &superviseControlFakeTx{
		pipePath: pipePath,
		pid:      panePID,
		metadata: map[string]any{
			"stdin_delivery": stdinDeliveryPersistentFIFO,
			"tmux": map[string]any{
				"state":        "backed",
				"session_name": "striatum-run",
				"pane_id":      "%4",
				"pane_pid":     panePID,
			},
		},
	}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}}

	_, err := HandleSuperviseRebridge(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_rebridge_dead_pane",
		Method:        "supervise.rebridge",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err == nil {
		t.Fatalf("expected rebridge to refuse a dead tmux pane")
	}
	rpcErr, ok := err.(*rpc.Error)
	if !ok || rpcErr.Code != "invalid_transition" || !strings.Contains(rpcErr.Message, "pane liveness is tmux_pane_dead") {
		t.Fatalf("err = %#v", err)
	}
	if len(tx.execs) != 0 {
		t.Fatalf("rebridge refusal should not mutate rows: %#v", tx.execs)
	}
}

func TestSuperviseStopMarksSupervisorStoppedAndUnlinksPipe(t *testing.T) {
	dir := t.TempDir()
	pipePath := dir + "/stdin.pipe"
	if err := os.WriteFile(pipePath, nil, 0o600); err != nil {
		t.Fatalf("write pipe placeholder: %v", err)
	}
	tx := &superviseControlFakeTx{pipePath: pipePath}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}, pipePath: pipePath}
	result, err := HandleSuperviseStop(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_stop",
		Method:        "supervise.stop",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"reason":        "operator_requested",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStop: %v", err)
	}
	if result["state"] != "stopped" || result["stop_reason"] != "operator_requested" {
		t.Fatalf("stop result = %#v", result)
	}
	if _, err := os.Stat(pipePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pipe still exists or unexpected stat err: %v", err)
	}
	if !tx.sawExec("UPDATE striatumd.process_supervisors", "ended_at") {
		t.Fatalf("missing stopped update: %#v", tx.execs)
	}
	// #50: the stopped supervisor's session must be closed (guarded on no active
	// lease) so it stops reading as `active` in live-session lookups.
	if !tx.sawExec("UPDATE striatumd.sessions", "state = 'closed'") {
		t.Fatalf("missing guarded session-close update: %#v", tx.execs)
	}
	event := tx.lastEventInsert()
	if event == nil || event.args[3] != "supervisor.stopped" {
		t.Fatalf("event insert = %#v", event)
	}
}

func TestSuperviseStopUsesTmuxKillSessionForBackedLane(t *testing.T) {
	origRunner := supervisionTmuxRunner
	defer func() { supervisionTmuxRunner = origRunner }()
	tmuxRunner := &mutationFakeTmuxRunner{}
	supervisionTmuxRunner = tmuxRunner

	dir := t.TempDir()
	pipePath := dir + "/stdin.pipe"
	if err := os.WriteFile(pipePath, nil, 0o600); err != nil {
		t.Fatalf("write pipe placeholder: %v", err)
	}
	tx := &superviseControlFakeTx{
		pipePath: pipePath,
		pid:      os.Getpid(),
		metadata: map[string]any{
			"stdin_delivery": stdinDeliveryPersistentFIFO,
			"tmux": map[string]any{
				"state":             "backed",
				"session_name":      "striatum-run",
				"pane_id":           "%4",
				"pane_pid":          os.Getpid(),
				"attach_client_pid": 0,
			},
		},
	}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}, pipePath: pipePath}
	result, err := HandleSuperviseStop(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_stop_tmux",
		Method:        "supervise.stop",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"reason":        "operator_requested",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStop: %v", err)
	}
	if result["signal"] != "tmux_kill_session" {
		t.Fatalf("stop signal = %#v", result["signal"])
	}
	if len(tmuxRunner.calls) != 1 || strings.Join(tmuxRunner.calls[0], " ") != "kill-session -t striatum-run" {
		t.Fatalf("tmux calls = %#v", tmuxRunner.calls)
	}
}

func TestSuperviseStopCleansUpLostTmuxBackedSupervisor(t *testing.T) {
	origRunner := supervisionTmuxRunner
	defer func() { supervisionTmuxRunner = origRunner }()
	tmuxRunner := &mutationFakeTmuxRunner{}
	supervisionTmuxRunner = tmuxRunner

	dir := t.TempDir()
	pipePath := dir + "/stdin.pipe"
	if err := os.WriteFile(pipePath, nil, 0o600); err != nil {
		t.Fatalf("write pipe placeholder: %v", err)
	}
	metadata := map[string]any{
		"stdin_delivery": stdinDeliveryPersistentFIFO,
		"tmux": map[string]any{
			"state":             "backed",
			"session_name":      "striatum-run-lost",
			"pane_id":           "%4",
			"pane_pid":          os.Getpid(),
			"attach_client_pid": 0,
		},
	}
	terminal := &supervisorControlRow{
		SupervisorID:       "sup_lost",
		RunID:              "run_1",
		SessionID:          "sess_1",
		State:              "lost",
		ScratchPath:        filepath.Join(dir, ".striatum", "scratch", "sup_lost"),
		StdinPipePath:      pipePath,
		PID:                os.Getpid(),
		HasPID:             true,
		DaemonSupervisorID: "dsup_1",
		Metadata:           metadata,
		EndedAt:            "2026-06-07T00:00:00Z",
		StopReason:         "child exited before attach",
	}
	tx := &superviseControlFakeTx{
		pipePath: pipePath,
		pid:      os.Getpid(),
		metadata: metadata,
	}
	runner := &superviseControlFakeRunner{
		activeSupervisorMissing: true,
		terminalSupervisor:      terminal,
		pipePath:                pipePath,
		txs:                     []*superviseControlFakeTx{tx},
	}
	result, err := HandleSuperviseStop(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_stop_lost_tmux",
		Method:        "supervise.stop",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"reason":        "operator_cleanup",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStop: %v", err)
	}
	if result["signal"] != "tmux_kill_session" {
		t.Fatalf("stop signal = %#v", result["signal"])
	}
	if result["note"] == "supervisor was already lost" {
		t.Fatalf("lost supervisor must be cleaned up, not treated as already terminal: %#v", result)
	}
	if len(tmuxRunner.calls) != 1 || strings.Join(tmuxRunner.calls[0], " ") != "kill-session -t striatum-run-lost" {
		t.Fatalf("tmux calls = %#v", tmuxRunner.calls)
	}
	if _, err := os.Stat(pipePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pipe still exists or unexpected stat err: %v", err)
	}
	if !tx.sawExec("UPDATE striatumd.process_supervisors", "ended_at") {
		t.Fatalf("missing stopped update: %#v", tx.execs)
	}
	event := tx.lastEventInsert()
	if event == nil || event.args[3] != "supervisor.stopped" {
		t.Fatalf("event insert = %#v", event)
	}
}

// TestSuperviseStopRemovesEphemeralGeminiSettings is the issue #62 regression:
// the tmux-backed teardown path (supervise.stop -> tmux kill-session) must
// remove the per-launch .gemini/settings.json (rotating MCP bearer token) the
// agy lane wrote, restoring any prior contents. Cleanup is driven from the
// terminal supervisor-state transition, not the agent-loop's own cleanupMCP, so
// it fires regardless of exit path.
func TestSuperviseStopRemovesEphemeralGeminiSettings(t *testing.T) {
	origRunner := supervisionTmuxRunner
	defer func() { supervisionTmuxRunner = origRunner }()
	supervisionTmuxRunner = &mutationFakeTmuxRunner{}

	repo := t.TempDir()
	// The agy launch wrote a token-bearing project settings file plus a scratch
	// "created" marker (no prior settings existed at launch).
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

	dir := t.TempDir()
	pipePath := dir + "/stdin.pipe"
	if err := os.WriteFile(pipePath, nil, 0o600); err != nil {
		t.Fatalf("write pipe placeholder: %v", err)
	}
	tx := &superviseControlFakeTx{
		pipePath: pipePath,
		repoRoot: repo,
		pid:      os.Getpid(),
		metadata: map[string]any{
			"stdin_delivery": stdinDeliveryPersistentFIFO,
			"tmux": map[string]any{
				"state":             "backed",
				"session_name":      "striatum-run",
				"pane_id":           "%4",
				"pane_pid":          os.Getpid(),
				"attach_client_pid": 0,
			},
		},
	}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}, pipePath: pipePath}
	if _, err := HandleSuperviseStop(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_stop_gemini",
		Method:        "supervise.stop",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"reason":        "operator_requested",
		},
	}); err != nil {
		t.Fatalf("HandleSuperviseStop: %v", err)
	}

	// The token-bearing settings file (created-at-launch) must be gone after a
	// tmux-backed teardown, and its scratch marker cleaned up.
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		body, _ := os.ReadFile(settingsPath)
		t.Fatalf("ephemeral .gemini/settings.json not removed on tmux teardown: %s", body)
	}
	if _, err := os.Stat(filepath.Join(scratchDir, "settings.json.created")); !os.IsNotExist(err) {
		t.Fatalf("scratch created-marker not cleaned up on teardown")
	}
}

// TestUpdateSupervisorStateTerminalRestoresPriorGeminiSettings asserts the
// state-transition choke point restores pre-existing .gemini/settings.json
// contents (not just deletes) on any terminal transition — the issue #62
// "restore to prior contents" requirement, exercised independent of the
// supervise.stop handler.
func TestUpdateSupervisorStateTerminalRestoresPriorGeminiSettings(t *testing.T) {
	repo := t.TempDir()
	geminiDir := filepath.Join(repo, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(geminiDir, "settings.json")
	original := []byte(`{"security":{"auth":{"selectedType":"oauth-personal"}}}`)
	if err := os.WriteFile(settingsPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	scratchDir := filepath.Join(repo, ".striatum", "scratch", "sup_1")
	if err := os.MkdirAll(scratchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Launch backed up the prior settings and overwrote the live file with the
	// rotating-token version.
	if err := os.WriteFile(filepath.Join(scratchDir, "settings.json.backup"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"mcpServers":{"striatum":{"httpUrl":"http://127.0.0.1:1/mcp","headers":{"Authorization":"Bearer dtok_secret"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	tx := &superviseControlFakeTx{repoRoot: repo}
	if err := updateSupervisorState(context.Background(), tx, "repo_1", "sup_1", "", "stopped", nowString(), 0, "", "", nil, nil); err != nil {
		t.Fatalf("updateSupervisorState: %v", err)
	}

	restored, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read restored settings: %v", err)
	}
	if string(restored) != string(original) {
		t.Fatalf("terminal transition should restore prior settings, got: %s", restored)
	}
	if _, err := os.Stat(filepath.Join(scratchDir, "settings.json.backup")); !os.IsNotExist(err) {
		t.Fatalf("scratch backup marker not cleaned up on terminal transition")
	}
}

func TestSuperviseStopSkipsStaleHelperPIDCleanup(t *testing.T) {
	_ = currentStartTokenForMutationTest(t)
	tx := &superviseControlFakeTx{
		pid:      999999999,
		pidStart: "stale-supervisor-start-token",
		metadata: map[string]any{
			"stdin_delivery":        stdinDeliveryPersistentFIFO,
			"helper_pid":            os.Getpid(),
			"helper_pid_start_time": "stale-start-token",
		},
	}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}}
	result, err := HandleSuperviseStop(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_stop_stale_helper",
		Method:        "supervise.stop",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"reason":        "operator_requested",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStop: %v", err)
	}
	if result["state"] != "stopped" {
		t.Fatalf("stop result = %#v", result)
	}
	event := tx.lastEventInsert()
	if event == nil {
		t.Fatal("missing stopped event")
	}
	payload := event.args[9].(map[string]any)
	if payload["helper_pid_cleanup_skipped_reason"] != "start_token_mismatch" {
		t.Fatalf("event payload = %#v", payload)
	}
}

func TestSuperviseStopSkipsTmuxFallbackPanePIDOnStartTokenMismatch(t *testing.T) {
	_ = currentStartTokenForMutationTest(t)
	origRunner := supervisionTmuxRunner
	defer func() { supervisionTmuxRunner = origRunner }()
	supervisionTmuxRunner = &mutationFakeTmuxRunner{err: errors.New("tmux server wedged")}

	tx := &superviseControlFakeTx{
		pid:      os.Getpid(),
		pidStart: "stale-start-token",
		metadata: map[string]any{
			"stdin_delivery": stdinDeliveryPersistentFIFO,
			"tmux": map[string]any{
				"state":        "backed",
				"session_name": "striatum-run",
				"pane_id":      "%4",
				"pane_pid":     os.Getpid(),
			},
		},
	}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx}}
	result, err := HandleSuperviseStop(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_stop_stale_tmux_fallback",
		Method:        "supervise.stop",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
			"reason":        "operator_requested",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseStop: %v", err)
	}
	if result["signal"] != nil {
		t.Fatalf("stop signal = %#v, want skipped fallback cleanup", result["signal"])
	}
	event := tx.lastEventInsert()
	if event == nil {
		t.Fatal("missing stopped event")
	}
	payload := event.args[9].(map[string]any)
	if payload["tmux_kill_fallback_reason"] != string(gosupervisor.TmuxLivenessUnavailable) ||
		payload["pane_pid_cleanup_skipped_reason"] != "start_token_mismatch" {
		t.Fatalf("event payload = %#v", payload)
	}
}

type mutationFakeTmuxRunner struct {
	calls [][]string
	err   error
}

func (r *mutationFakeTmuxRunner) Run(_ context.Context, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	return "", r.err
}

func TestLinuxProcStatZombieDetectsDefunctProcessState(t *testing.T) {
	if !linuxProcStatZombie([]byte("1234 (agent command) Z 1 2 3")) {
		t.Fatal("expected Z process state to be treated as zombie")
	}
	if linuxProcStatZombie([]byte("1234 (agent command) S 1 2 3")) {
		t.Fatal("sleeping process state should not be treated as zombie")
	}
}

type superviseControlFakeRunner struct {
	mu                      sync.Mutex
	repoRoot                string
	pipePath                string
	workflowSupervision     map[string]any
	workflowLane            map[string]any
	activeSupervisorMissing bool
	terminalSupervisor      *supervisorControlRow
	txs                     []*superviseControlFakeTx
}

func (r *superviseControlFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("unexpected runner exec outside tx")
}

func (r *superviseControlFakeRunner) QueryRow(_ context.Context, sql string, args ...any) db.Row {
	return r.fakeRow(sql, args...)
}

func (r *superviseControlFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected query scalar")
}

func (r *superviseControlFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.txs) == 0 {
		return nil, errors.New("unexpected BeginTx")
	}
	tx := r.txs[0]
	r.txs = r.txs[1:]
	if tx.pipePath == "" {
		tx.pipePath = r.pipePath
	}
	return tx, nil
}

func (r *superviseControlFakeRunner) fakeRow(sql string, args ...any) db.Row {
	switch {
	case strings.Contains(sql, "SELECT s.session_id, s.run_id"):
		return superviseControlFakeRow{values: []any{"sess_1", "run_1", "lane_1", "active", "snap_1", r.repoRoot}}
	case strings.Contains(sql, "SELECT state FROM striatumd.sessions"):
		return superviseControlFakeRow{values: []any{"active"}}
	case strings.Contains(sql, "SELECT workflow_json"):
		lane := map[string]any{
			"adapter": "process",
			"command": []any{"/bin/cat"},
		}
		if r.workflowSupervision != nil {
			lane["supervision"] = r.workflowSupervision
		}
		for key, value := range r.workflowLane {
			lane[key] = value
		}
		return superviseControlFakeRow{values: []any{map[string]any{
			"lanes": map[string]any{
				"lane_1": lane,
			},
		}}}
	case strings.Contains(sql, "SELECT supervisor_id, state") && strings.Contains(sql, "state = ANY"):
		return superviseControlFakeRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "SELECT session_id") && strings.Contains(sql, "FROM striatumd.sessions"):
		return superviseControlFakeRow{values: []any{"sess_1"}}
	case strings.Contains(sql, "FROM striatumd.process_supervisors ps") && strings.Contains(sql, "ps.ended_at"):
		if r.terminalSupervisor == nil {
			return superviseControlFakeRow{err: pgx.ErrNoRows}
		}
		supervisor := r.terminalSupervisor
		var pid any
		if supervisor.HasPID {
			pid = supervisor.PID
		}
		return superviseControlFakeRow{values: []any{
			supervisor.SupervisorID, supervisor.RunID, supervisor.SessionID, supervisor.State,
			supervisor.ScratchPath, supervisor.StdinPipePath, pid, supervisor.PIDStartTime,
			supervisor.DaemonSupervisorID, supervisor.Metadata,
			supervisor.EndedAt, supervisor.StopReason,
		}}
	case strings.Contains(sql, "SELECT ps.supervisor_id"):
		if r.activeSupervisorMissing {
			return superviseControlFakeRow{err: pgx.ErrNoRows}
		}
		return superviseControlFakeRow{values: []any{"sup_1", "run_1", "sess_1", "attached", filepath.Join(r.repoRoot, ".striatum", "scratch", "sup_1"), r.pipePath, nil, "", "dsup_1", map[string]any{"stdin_delivery": stdinDeliveryPersistentFIFO}}}
	default:
		return superviseControlFakeRow{err: errors.New("unexpected runner query: " + sql)}
	}
}

type superviseControlFakeTx struct {
	pipePath          string
	repoRoot          string
	pid               int
	pidStart          string
	metadata          map[string]any
	claimable         bool
	nextEvent         int64
	execs             []superviseControlExec
	committed         bool
	rolledBack        bool
	staleSupervisorID string // non-empty → supersedeStaleSupervisorIfRequested finds a stale supervisor
}

type superviseControlExec struct {
	sql  string
	args []any
}

func (tx *superviseControlFakeTx) Exec(_ context.Context, sql string, args ...any) error {
	tx.execs = append(tx.execs, superviseControlExec{sql: sql, args: append([]any(nil), args...)})
	return nil
}

func (tx *superviseControlFakeTx) QueryRow(_ context.Context, sql string, args ...any) db.Row {
	switch {
	case strings.Contains(sql, "ps.cwd, ps.scratch_path"):
		// Issue #62: lane-MCP teardown resolves the supervisor's working dir.
		cwd := filepath.Dir(tx.pipePath)
		if tx.repoRoot != "" {
			cwd = tx.repoRoot
		}
		runAsUser := ""
		if tx.metadata != nil {
			runAsUser, _ = tx.metadata["run_as_user"].(string)
		}
		return superviseControlFakeRow{values: []any{cwd, filepath.Join(cwd, ".striatum", "scratch", "sup_1"), runAsUser}}
	case strings.Contains(sql, "SELECT supervisor_id, run_id, state") && strings.Contains(sql, "state = ANY"):
		// supersedeStaleSupervisorIfRequested: return stale supervisor if configured.
		if tx.staleSupervisorID != "" {
			return superviseControlFakeRow{values: []any{tx.staleSupervisorID, "run_1", "attached"}}
		}
		return superviseControlFakeRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "SELECT daemon_supervisor_id") && strings.Contains(sql, "process_supervisor_pointers"):
		// markSupervisorLostInTx looks up the daemon supervisor id.
		return superviseControlFakeRow{values: []any{"dsup_stale"}}
	case strings.Contains(sql, "SELECT supervisor_id, state") && strings.Contains(sql, "state = ANY"):
		return superviseControlFakeRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "SELECT ps.supervisor_id") && strings.Contains(sql, "ps.scratch_path"):
		var pid any
		if tx.pid > 0 {
			pid = tx.pid
		}
		metadata := tx.metadata
		if metadata == nil {
			metadata = map[string]any{"stdin_delivery": stdinDeliveryPersistentFIFO}
		}
		return superviseControlFakeRow{values: []any{"sup_1", "run_1", "sess_1", "attached", filepath.Dir(tx.pipePath), tx.pipePath, pid, tx.pidStart, "dsup_1", metadata}}
	case strings.Contains(sql, "SELECT ps.supervisor_id"):
		var pid any
		if tx.pid > 0 {
			pid = tx.pid
		}
		metadata := tx.metadata
		if metadata == nil {
			metadata = map[string]any{"stdin_delivery": stdinDeliveryPersistentFIFO}
		}
		return superviseControlFakeRow{values: []any{"sup_1", "run_1", "sess_1", "attached", pid, tx.pidStart, "dsup_1", metadata}}
	case strings.Contains(sql, "FROM striatumd.work_packets"):
		return superviseControlFakeRow{values: []any{"packet_1", "run_1", "job_1", "lease_1", "sess_1", map[string]any{"packet": "body"}}}
	case strings.Contains(sql, "FROM striatumd.leases"):
		return superviseControlFakeRow{values: []any{"active", "sess_1", "job_1", "2999-01-01T00:00:00Z"}}
	case strings.Contains(sql, "SELECT state, daemon_supervisor_id"):
		dsup := "dsup_1"
		return superviseControlFakeRow{values: []any{"attached", &dsup}}
	case strings.Contains(sql, "FROM striatumd.daemon_supervisors") && strings.Contains(sql, "SELECT state"):
		return superviseControlFakeRow{values: []any{"attached"}}
	case strings.Contains(sql, "SELECT metadata_json"):
		metadata := tx.metadata
		if metadata == nil {
			metadata = map[string]any{"stdin_delivery": stdinDeliveryPersistentFIFO}
		}
		return superviseControlFakeRow{values: []any{metadata}}
	case strings.Contains(sql, "repo_event_chain_heads"):
		return superviseControlFakeRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "nextval"):
		tx.nextEvent++
		return superviseControlFakeRow{values: []any{tx.nextEvent}}
	default:
		return superviseControlFakeRow{err: errors.New("unexpected tx query: " + sql)}
	}
}

func (tx *superviseControlFakeTx) QueryScalar(_ context.Context, sql string, _ ...any) (string, error) {
	if strings.Contains(sql, "FROM striatumd.supervisor_buffered_packets") {
		// #456: the persisted-buffer depth check. Empty in this in-memory fake, so
		// persistBufferedPacket may proceed to its (fake-recorded) INSERT.
		return "0", nil
	}
	return "", errors.New("unexpected query scalar")
}

func (tx *superviseControlFakeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "FROM striatumd.supervisor_buffered_packets") {
		// #456: the durable no-reader push buffer. This fake exercises the
		// in-memory delivery path, so the durable store is empty (hydrate is a
		// no-op) — return zero rows.
		return runPrepareRowsFromMaps(nil), nil
	}
	if strings.Contains(sql, "SELECT run_id FROM striatumd.sessions") {
		return runPrepareRowsFromMaps([]map[string]any{{"run_id": "run_1"}}), nil
	}
	if strings.Contains(sql, "SELECT * FROM striatumd.sessions") {
		return runPrepareRowsFromMaps([]map[string]any{{
			"session_id":        "sess_1",
			"run_id":            "run_1",
			"role_id":           "worker",
			"lane_id":           "lane_1",
			"slug":              "worker-lane-1",
			"capabilities_json": []any{"write"},
			"ordinal":           1,
			"operator_label":    "codex",
			"state":             "active",
		}}), nil
	}
	if strings.Contains(sql, "SELECT * FROM striatumd.runs") {
		return runPrepareRowsFromMaps([]map[string]any{{
			"run_id":               "run_1",
			"state":                "running",
			"paused_at":            nil,
			"workflow_snapshot_id": "snap_1",
			"repo_root":            tx.repoRoot,
			"branch_name":          "main",
			"branch_confirmed_at":  "2026-01-01T00:00:00Z",
		}}), nil
	}
	if strings.Contains(sql, "SELECT * FROM striatumd.leases") {
		return runPrepareRowsFromMaps(nil), nil
	}
	if strings.Contains(sql, "SELECT qm.*") {
		if !tx.claimable {
			return runPrepareRowsFromMaps(nil), nil
		}
		return runPrepareRowsFromMaps([]map[string]any{{
			"message_id":     "msg_1",
			"run_id":         "run_1",
			"job_id":         "job_1",
			"target_role_id": "worker",
			"target_lane_id": "lane_1",
			"priority":       10,
		}}), nil
	}
	if strings.Contains(sql, "SELECT j.workflow_job_id AS workflow_job_id") {
		return runPrepareRowsFromMaps(nil), nil
	}
	if strings.Contains(sql, "SELECT * FROM striatumd.jobs") {
		return runPrepareRowsFromMaps([]map[string]any{{
			"job_id":                  "job_1",
			"run_id":                  "run_1",
			"workflow_job_id":         "draft",
			"attempt":                 1,
			"state":                   "queued",
			"role_id":                 "worker",
			"title":                   "Draft artifact",
			"job_type":                "draft",
			"fresh_session_required":  false,
			"write_scope_json":        map[string]any{"allowed_paths": []any{"docs/"}, "forbidden_paths": []any{".striatum/"}},
			"expected_artifacts_json": []any{},
			"lane_selector_json":      map[string]any{"lane_id": "lane_1"},
			"capability_requirements_json": map[string]any{
				"objective":   "draft the artifact",
				"task_prompt": map[string]any{"inline": "draft"},
				"inputs":      []any{},
			},
		}}), nil
	}
	if strings.Contains(sql, "SELECT * FROM striatumd.workflow_snapshots") {
		return runPrepareRowsFromMaps([]map[string]any{{
			"workflow_snapshot_id": "snap_1",
			"source_path":          "workflows/demo/workflow.json",
			"workflow_json": map[string]any{
				"workflow_id":  "wf",
				"context_docs": []any{},
				"roles": map[string]any{
					"worker": map[string]any{"definition_path": "roles/worker.md", "summary": "Worker"},
				},
				"lanes": map[string]any{
					"lane_1": map[string]any{"display_model": "Codex"},
				},
				"jobs": []any{map[string]any{"id": "draft", "type": "draft"}},
			},
		}}), nil
	}
	if strings.Contains(sql, "SELECT COALESCE(p.metadata_json") {
		metadata := tx.metadata
		if metadata == nil {
			metadata = map[string]any{"stdin_delivery": stdinDeliveryPersistentFIFO, "agent_loop_mode": agentLoopModePush}
		}
		return runPrepareRowsFromMaps([]map[string]any{{"metadata_json": metadata}}), nil
	}
	if strings.Contains(sql, "LEFT JOIN striatumd.process_supervisors ps") && strings.Contains(sql, "FROM striatumd.sessions s") {
		var pid any
		if tx.pid > 0 {
			pid = tx.pid
		}
		metadata := tx.metadata
		if metadata == nil {
			metadata = map[string]any{"stdin_delivery": stdinDeliveryPersistentFIFO}
		}
		now := time.Now().UTC()
		merged := map[string]any{
			"supervisor_id":                "sup_1",
			"pid":                          pid,
			"pid_start_time":               tx.pidStart,
			"supervisor_state":             "attached",
			"pointer_daemon_supervisor_id": "dsup_1",
			"pointer_pid":                  pid,
			"pointer_pid_start_time":       tx.pidStart,
			"pointer_state":                "attached",
			"pointer_metadata_json":        metadata,
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
		return runPrepareRowsFromMaps([]map[string]any{merged}), nil
	}
	return nil, errors.New("unexpected tx query: " + sql)
}

func (tx *superviseControlFakeTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *superviseControlFakeTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

func (tx *superviseControlFakeTx) sawExec(parts ...string) bool {
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

// sawExecArg returns true when any exec has sqlPart in its SQL and argValue
// among its args (checked via fmt.Sprint for flexible matching).
func (tx *superviseControlFakeTx) sawExecArg(sqlPart, argValue string) bool {
	for _, exec := range tx.execs {
		if !strings.Contains(exec.sql, sqlPart) {
			continue
		}
		for _, arg := range exec.args {
			if fmt.Sprint(arg) == argValue {
				return true
			}
		}
	}
	return false
}

func (tx *superviseControlFakeTx) eventInserts() []superviseControlExec {
	var events []superviseControlExec
	for _, exec := range tx.execs {
		if strings.Contains(exec.sql, "INSERT INTO striatumd.events") {
			events = append(events, exec)
		}
	}
	return events
}

func (tx *superviseControlFakeTx) lastEventInsert() *superviseControlExec {
	events := tx.eventInserts()
	if len(events) == 0 {
		return nil
	}
	return &events[len(events)-1]
}

func (tx *superviseControlFakeTx) sawEventType(eventType string) bool {
	for _, event := range tx.eventInserts() {
		if len(event.args) > 3 && fmt.Sprint(event.args[3]) == eventType {
			return true
		}
	}
	return false
}

func (tx *superviseControlFakeTx) pointerMetadataUpdate() *superviseControlExec {
	for _, exec := range tx.execs {
		if strings.Contains(exec.sql, "UPDATE striatumd.process_supervisor_pointers") && strings.Contains(exec.sql, "metadata_json") {
			return &exec
		}
	}
	return nil
}

func envValue(t *testing.T, env []string, key string) string {
	t.Helper()
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	t.Fatalf("missing env key %s in %#v", key, env)
	return ""
}

func countEnv(env []string, key string) int {
	prefix := key + "="
	count := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			count++
		}
	}
	return count
}

func currentStartTokenForMutationTest(t *testing.T) string {
	t.Helper()
	token, ok := processStartToken(os.Getpid())
	if !ok || token == "" {
		t.Skip("process start token unavailable on this platform")
	}
	return token
}

type superviseControlFakeRow struct {
	values []any
	err    error
}

func (r superviseControlFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) > len(r.values) {
		return errors.New("not enough fake row values")
	}
	for i, value := range r.values[:len(dest)] {
		switch target := dest[i].(type) {
		case *string:
			if value == nil {
				*target = ""
			} else {
				*target = value.(string)
			}
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
			} else {
				typed := value.(int)
				*target = &typed
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

func TestSuperviseRebridgeClearsBenignAttachExitDelivery(t *testing.T) {
	origRunner := supervisionTmuxRunner
	origRebridgeLaunch := supervisionRebridgeLaunch
	defer func() {
		supervisionTmuxRunner = origRunner
		supervisionRebridgeLaunch = origRebridgeLaunch
	}()

	panePID := os.Getpid()
	token := "1748452211"
	supervisionTmuxRunner = superviseReportFakeTmuxRunner{
		display: "%4|" + strconv.Itoa(panePID) + "|0|" + token,
	}

	dir := t.TempDir()
	pipePath := filepath.Join(dir, "stdin.pipe")
	tx1 := &superviseControlFakeTx{
		pipePath: pipePath,
		pid:      panePID,
		pidStart: token,
		metadata: map[string]any{
			"stdin_delivery": stdinDeliveryPersistentFIFO,
			"tmux": map[string]any{
				"state":            "backed",
				"session_name":     "striatum-run",
				"pane_id":          "%4",
				"pane_pid":         panePID,
				"pane_start_token": token,
			},
		},
	}
	tx2 := &superviseControlFakeTx{
		pipePath: pipePath,
		pid:      panePID,
		pidStart: token,
		metadata: map[string]any{
			"stdin_delivery": stdinDeliveryPersistentFIFO,
			"tmux": map[string]any{
				"state":            "backed",
				"session_name":     "striatum-run",
				"pane_id":          "%4",
				"pane_pid":         panePID,
				"pane_start_token": token,
			},
		},
	}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx1, tx2}}

	// Mock rebridge launch to return an attach_client_exited event in its initial events batch!
	supervisionRebridgeLaunch = func(ctx context.Context, supervisor supervisorControlRow, identity gosupervisor.TmuxIdentity, eventPath string) (supervisionLaunchResult, error) {
		return supervisionLaunchResult{
			PID:          panePID,
			PIDStartTime: token,
			HelperPID:    1234,
			InitialHelperEvents: []map[string]any{
				{
					"schema_version": gosupervisor.HelperEventSchemaVersion,
					"event_type":     gosupervisor.HelperEventAgentStarted,
					"supervisor_id":  "sup_1",
					"session_id":     "sess_1",
					"payload": map[string]any{
						"pid":               panePID,
						"attach_pid":        1234,
						"attach_client_pid": 1234,
					},
				},
				{
					"schema_version": gosupervisor.HelperEventSchemaVersion,
					"event_type":     gosupervisor.HelperEventAttachExited,
					"supervisor_id":  "sup_1",
					"session_id":     "sess_1",
					"payload": map[string]any{
						"attach_exit_code": 1,
						"tmux_liveness":    string(gosupervisor.TmuxLivenessOK),
					},
				},
			},
			InitialHelperOffset: 120,
			Metadata: map[string]any{
				"tmux": map[string]any{
					"state":        "backed",
					"session_name": "striatum-run",
					"pane_id":      "%4",
					"pane_pid":     panePID,
				},
			},
		}, nil
	}

	result, err := HandleSuperviseRebridge(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_rebridge_preserve_degraded",
		Method:        "supervise.rebridge",
		Params: map[string]any{
			"repository_id": "repo_1",
			"session_id":    "sess_1",
		},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseRebridge: %v", err)
	}

	// #67: a benign attach-observer exit (#63 F7) on a freshly rebuilt bridge is
	// NOT a delivery failure — rebridge must report healthy, not degraded.
	if result["delivery_state"] != "healthy" {
		t.Fatalf("#67: benign attach_client_exited must leave rebridge healthy, got %v", result["delivery_state"])
	}

	// The degraded delivery_liveness block must be cleared from the replaced
	// pointer metadata (top-level and under tmux).
	var replaceUpdate map[string]any
	for _, exec := range tx2.execs {
		if strings.Contains(exec.sql, "UPDATE striatumd.process_supervisor_pointers") {
			if m, ok := exec.args[0].(map[string]any); ok {
				replaceUpdate = m
			}
		}
	}
	if replaceUpdate == nil {
		t.Fatalf("missing process_supervisor_pointers metadata update")
	}
	if _, ok := replaceUpdate["delivery_liveness"]; ok {
		t.Fatalf("#67: top-level delivery_liveness must be cleared on benign attach exit, got: %#v", replaceUpdate)
	}
	if tmux := asMap(replaceUpdate["tmux"]); len(tmux) > 0 {
		if _, ok := tmux["delivery_liveness"]; ok {
			t.Fatalf("#67: tmux.delivery_liveness must be cleared on benign attach exit, got: %#v", tmux)
		}
	}
}

// TestSuperviseRebridgePreservesRealDeliveryFailure is the #67 guard's other
// side: a genuine transport failure the helper reports on relaunch
// (helper_error) must still leave the lane degraded after rebridge.
func TestSuperviseRebridgePreservesRealDeliveryFailure(t *testing.T) {
	origRunner := supervisionTmuxRunner
	origRebridgeLaunch := supervisionRebridgeLaunch
	defer func() {
		supervisionTmuxRunner = origRunner
		supervisionRebridgeLaunch = origRebridgeLaunch
	}()

	panePID := os.Getpid()
	token := "1748452211"
	supervisionTmuxRunner = superviseReportFakeTmuxRunner{
		display: "%4|" + strconv.Itoa(panePID) + "|0|" + token,
	}
	dir := t.TempDir()
	pipePath := filepath.Join(dir, "stdin.pipe")
	meta := map[string]any{
		"stdin_delivery": stdinDeliveryPersistentFIFO,
		"tmux": map[string]any{
			"state":            "backed",
			"session_name":     "striatum-run",
			"pane_id":          "%4",
			"pane_pid":         panePID,
			"pane_start_token": token,
		},
	}
	tx1 := &superviseControlFakeTx{pipePath: pipePath, pid: panePID, pidStart: token, metadata: copyMap(meta)}
	tx2 := &superviseControlFakeTx{pipePath: pipePath, pid: panePID, pidStart: token, metadata: copyMap(meta)}
	runner := &superviseControlFakeRunner{txs: []*superviseControlFakeTx{tx1, tx2}}

	supervisionRebridgeLaunch = func(ctx context.Context, supervisor supervisorControlRow, identity gosupervisor.TmuxIdentity, eventPath string) (supervisionLaunchResult, error) {
		return supervisionLaunchResult{
			PID:          panePID,
			PIDStartTime: token,
			HelperPID:    1234,
			InitialHelperEvents: []map[string]any{
				{
					"schema_version": gosupervisor.HelperEventSchemaVersion,
					"event_type":     gosupervisor.HelperEventError,
					"supervisor_id":  "sup_1",
					"session_id":     "sess_1",
					"payload":        map[string]any{"message": "helper transport dead"},
				},
			},
			InitialHelperOffset: 120,
			Metadata: map[string]any{
				"tmux": map[string]any{"state": "backed", "session_name": "striatum-run", "pane_id": "%4", "pane_pid": panePID},
			},
		}, nil
	}

	result, err := HandleSuperviseRebridge(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_rebridge_real_failure",
		Method:        "supervise.rebridge",
		Params:        map[string]any{"repository_id": "repo_1", "session_id": "sess_1"},
	})
	if err != nil {
		t.Fatalf("HandleSuperviseRebridge: %v", err)
	}
	if result["delivery_state"] != "degraded" {
		t.Fatalf("#67: a real helper_error must keep rebridge degraded, got %v", result["delivery_state"])
	}
}
