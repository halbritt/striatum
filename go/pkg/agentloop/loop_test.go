package agentloop

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAgentLoopSubmitSequenceDefault(t *testing.T) {
	if err := os.Unsetenv("STRIATUM_AGENT_LOOP_SUBMIT_SEQUENCE"); err != nil {
		t.Fatalf("unset submit sequence env: %v", err)
	}
	if got := agentLoopSubmitSequence(); got != "\r" {
		t.Fatalf("default submit sequence = %q, want carriage return", got)
	}
}

func TestAgentLoopSubmitSequenceOverride(t *testing.T) {
	cases := map[string]string{
		`\n`:   "\n",
		`\r\n`: "\r\n",
		"":     "", // explicit empty disables the submit (headless EOF adapters)
		`\r`:   "\r",
		`x\ty`: "x\ty",
	}
	for raw, want := range cases {
		t.Setenv("STRIATUM_AGENT_LOOP_SUBMIT_SEQUENCE", raw)
		if got := agentLoopSubmitSequence(); got != want {
			t.Fatalf("submit sequence for %q = %q, want %q", raw, got, want)
		}
	}
}

func TestPrepareLaneCommandForBootstrapUsesCodexInitialPromptArg(t *testing.T) {
	prompt := "bootstrap prompt\nwith multiple lines"
	cmd, cleanup, mode, _, err := prepareLaneCommandForBootstrap(
		[]string{"/home/x/.local/bin/codex", "--model", "gpt-5.5"},
		t.TempDir(),
		"http://127.0.0.1:42727/mcp",
		TokenMaterial{Token: "dtok_secret"},
		prompt,
	)
	if err != nil {
		t.Fatalf("prepare codex: %v", err)
	}
	defer cleanup()
	if mode != bootstrapDeliveryArgv {
		t.Fatalf("mode = %q, want %q", mode, bootstrapDeliveryArgv)
	}
	if got := cmd[len(cmd)-1]; got != prompt {
		t.Fatalf("last arg = %q, want bootstrap prompt", got)
	}
	joined := strings.Join(cmd, "\x00")
	if !strings.Contains(joined, `mcp_servers.striatum.url="http://127.0.0.1:42727/mcp"`) {
		t.Fatalf("prepared command missing codex MCP override: %#v", cmd)
	}
}

func TestPrepareLaneCommandForBootstrapUsesAgyInitialPromptArg(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("STRIATUM_SUPERVISOR_ID", "sup_loop_agy")
	prompt := "bootstrap prompt\nwith multiple lines"
	cmd, cleanup, mode, _, err := prepareLaneCommandForBootstrap(
		[]string{"/home/x/.local/bin/agy", "--dangerously-skip-permissions"},
		repo,
		"http://127.0.0.1:42727/mcp",
		TokenMaterial{Token: "dtok_secret"},
		prompt,
	)
	if err != nil {
		t.Fatalf("prepare agy: %v", err)
	}
	defer cleanup()
	if mode != bootstrapDeliveryArgv {
		t.Fatalf("mode = %q, want %q (agy must not use the PTY-submit path; its TUI buffers the prompt unsubmitted)", mode, bootstrapDeliveryArgv)
	}
	// agy takes the initial prompt as the VALUE of --prompt-interactive, not a
	// trailing positional like codex.
	if got := cmd[len(cmd)-1]; got != prompt {
		t.Fatalf("last arg = %q, want bootstrap prompt", got)
	}
	if got := cmd[len(cmd)-2]; got != "--prompt-interactive" {
		t.Fatalf("arg before prompt = %q, want --prompt-interactive", got)
	}
	// agy has NO --mcp-config flag (those make it print usage and exit); its
	// MCP config goes through .gemini/settings.json instead.
	joined := strings.Join(cmd, "\x00")
	if strings.Contains(joined, "--mcp-config") || strings.Contains(joined, "--strict-mcp-config") {
		t.Fatalf("agy command must not carry claude-shaped MCP flags: %#v", cmd)
	}
	if _, err := os.Stat(filepath.Join(repo, ".gemini", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("agy MCP config must not be written to target .gemini/settings.json, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, agyUserSettingsRelPath)); err != nil {
		t.Fatalf("agy MCP config should be written to user-scoped settings: %v", err)
	}
}

// #101: Claude Code v2.1.x buffers a typed multi-line bootstrap in its TUI and a
// trailing CR no longer submits it, so claude_code now takes the bootstrap as the
// initial positional prompt (argv) like codex, while keeping its MCP flags.
func TestPrepareLaneCommandForBootstrapUsesClaudeInitialPromptArg(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(repo+"/.striatum/scratch", 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := "bootstrap prompt\nwith multiple lines"
	cmd, cleanup, mode, _, err := prepareLaneCommandForBootstrap(
		[]string{"/home/x/.local/bin/claude", "--model", "claude-opus-4-7"},
		repo,
		"http://127.0.0.1:42727/mcp",
		TokenMaterial{Token: "dtok_secret"},
		prompt,
	)
	if err != nil {
		t.Fatalf("prepare claude: %v", err)
	}
	defer cleanup()
	if mode != bootstrapDeliveryArgv {
		t.Fatalf("mode = %q, want %q (claude TUI buffers the prompt unsubmitted; #101)", mode, bootstrapDeliveryArgv)
	}
	// The bootstrap is the trailing positional prompt (claude [options] <prompt>).
	if got := cmd[len(cmd)-1]; got != prompt {
		t.Fatalf("last arg = %q, want bootstrap prompt", got)
	}
	// MCP flags are still injected (just no longer last).
	joined := strings.Join(cmd, "\x00")
	if !strings.Contains(joined, "--strict-mcp-config") {
		t.Fatalf("claude command missing strict MCP config: %#v", cmd)
	}
}

func TestPromptForDaemonEnvelopeFormatsInterrogationQuestion(t *testing.T) {
	prompt := promptForDaemonEnvelope(map[string]any{
		"type":             "interrogation_question",
		"interrogation_id": "intg_1",
		"message_id":       "msg_1",
		"body":             "why this design?",
	})
	for _, want := range []string{"interrogation_question", "intg_1", "msg_1", "why this design?", "interrogation.answer"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if !strings.HasSuffix(prompt, "\n") {
		t.Fatalf("prompt should end with a newline before separate submit: %q", prompt)
	}
	if strings.HasSuffix(prompt, "\r") {
		t.Fatalf("prompt should not embed submit carriage return: %q", prompt)
	}
}

type writeRecorder struct {
	writes []string
}

func (r *writeRecorder) Write(p []byte) (int, error) {
	r.writes = append(r.writes, string(p))
	return len(p), nil
}

func TestWritePromptThenSubmitSeparatesPromptAndSubmit(t *testing.T) {
	rec := &writeRecorder{}
	if err := writePromptThenSubmit(rec, "daemon packet prompt", 0, "\r"); err != nil {
		t.Fatalf("writePromptThenSubmit: %v", err)
	}
	if got, want := len(rec.writes), 2; got != want {
		t.Fatalf("writes = %d, want %d: %#v", got, want, rec.writes)
	}
	if rec.writes[0] != "daemon packet prompt" {
		t.Fatalf("first write = %q, want prompt", rec.writes[0])
	}
	if rec.writes[1] != "\r" {
		t.Fatalf("second write = %q, want submit", rec.writes[1])
	}
}

func TestWritePromptThenSubmitCanDisableSubmit(t *testing.T) {
	rec := &writeRecorder{}
	if err := writePromptThenSubmit(rec, "daemon packet prompt", 0, ""); err != nil {
		t.Fatalf("writePromptThenSubmit: %v", err)
	}
	if got, want := len(rec.writes), 1; got != want {
		t.Fatalf("writes = %d, want %d: %#v", got, want, rec.writes)
	}
	if rec.writes[0] != "daemon packet prompt" {
		t.Fatalf("first write = %q, want prompt", rec.writes[0])
	}
}

func TestWritePromptThenSubmitAllowsZeroDelay(t *testing.T) {
	rec := &writeRecorder{}
	start := time.Now()
	if err := writePromptThenSubmit(rec, "prompt", 0, "\n"); err != nil {
		t.Fatalf("writePromptThenSubmit: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("zero delay should not sleep noticeably, elapsed %s", elapsed)
	}
}

func TestActiveLeaseIDFromStatus(t *testing.T) {
	cases := []struct {
		name   string
		status map[string]any
		want   string
	}{
		{
			name:   "active lease present",
			status: map[string]any{"protocol_liveness": map[string]any{"active_lease_id": "lease_abc"}},
			want:   "lease_abc",
		},
		{
			name:   "trims whitespace",
			status: map[string]any{"protocol_liveness": map[string]any{"active_lease_id": "  lease_xyz  "}},
			want:   "lease_xyz",
		},
		{
			name:   "nil active lease => empty (no lease to keep alive)",
			status: map[string]any{"protocol_liveness": map[string]any{"active_lease_id": nil}},
			want:   "",
		},
		{
			name:   "missing protocol_liveness => empty",
			status: map[string]any{},
			want:   "",
		},
		{
			name:   "nil status => empty",
			status: nil,
			want:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := activeLeaseIDFromStatus(tc.status); got != tc.want {
				t.Fatalf("activeLeaseIDFromStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLocalWorkKeepaliveInterval(t *testing.T) {
	t.Run("default is ToolProgressSeconds/3", func(t *testing.T) {
		if err := os.Unsetenv("STRIATUM_AGENT_LOOP_KEEPALIVE_MS"); err != nil {
			t.Fatalf("unset keepalive env: %v", err)
		}
		if got := localWorkKeepaliveInterval(); got != 200*time.Second {
			t.Fatalf("default keepalive interval = %s, want 200s (well under the 600s wedge deadline)", got)
		}
	})
	t.Run("override", func(t *testing.T) {
		t.Setenv("STRIATUM_AGENT_LOOP_KEEPALIVE_MS", "5000")
		if got := localWorkKeepaliveInterval(); got != 5*time.Second {
			t.Fatalf("override keepalive interval = %s, want 5s", got)
		}
	})
	t.Run("zero disables", func(t *testing.T) {
		t.Setenv("STRIATUM_AGENT_LOOP_KEEPALIVE_MS", "0")
		if got := localWorkKeepaliveInterval(); got != 0 {
			t.Fatalf("zero keepalive interval = %s, want 0 (disabled)", got)
		}
	})
}

func TestLocalWorkKeepaliveStaysEnabledForCodexReceiverCarveout(t *testing.T) {
	t.Setenv("STRIATUM_AGENT_LOOP_KEEPALIVE_MS", "5000")
	if !daemonReceiverDisabled(nil, "codex") {
		t.Fatalf("codex should keep the PTY-side daemon receiver disabled by default")
	}
	cfg := runConfig{RepositoryID: "repo_1", SessionID: "sess_1"}
	if localWorkKeepaliveDisabled(cfg) {
		t.Fatalf("local_work keepalive must stay enabled for codex lanes even when the daemon receiver is disabled")
	}
}

func TestDaemonReceiverDisabledEnv(t *testing.T) {
	if daemonReceiverDisabled([]string{"STRIATUM_AGENT_LOOP_DAEMON_RECEIVER=on"}, "codex") {
		t.Fatalf("receiver should stay enabled for on")
	}
	if !daemonReceiverDisabled([]string{"STRIATUM_AGENT_LOOP_DAEMON_RECEIVER=off"}, "agy") {
		t.Fatalf("receiver should disable for off")
	}
	if !daemonReceiverDisabled(nil, "codex") {
		t.Fatalf("codex should default to its own MCP receive loop, without the PTY-side daemon receiver")
	}
	if daemonReceiverDisabled(nil, "agy") {
		t.Fatalf("non-codex adapters should keep the PTY-side daemon receiver by default")
	}
}

func TestDaemonEnvelopeRequestsIdleExitFailsClosedOnIdleBehavior(t *testing.T) {
	// Any non-empty idle_behavior on a no_work envelope requests an exit —
	// including values this lane build does not recognize (fail closed).
	for _, envelope := range []map[string]any{
		{"status": "no_work", "idle_behavior": "exit_session"},
		{"status": "no_work", "idle_behavior": "future_value"},
	} {
		if !EnvelopeRequestsIdleExit(envelope) {
			t.Fatalf("expected idle exit for %#v", envelope)
		}
	}
	// Absent or empty idle_behavior (an older daemon) keeps the legacy
	// polling behavior; non-no_work envelopes never request an exit.
	for _, envelope := range []map[string]any{
		{"status": "no_work"},
		{"status": "no_work", "idle_behavior": ""},
		{"status": "claimed", "idle_behavior": "exit_session"},
		{"type": "work_packet"},
	} {
		if EnvelopeRequestsIdleExit(envelope) {
			t.Fatalf("unexpected idle exit for %#v", envelope)
		}
	}
}

func TestNormalizeAgentExitErrorTreatsRequestedIdleExitAsClean(t *testing.T) {
	exitErr := errors.New("signal: terminated")
	if err := normalizeAgentExitError(exitErr, true); err != nil {
		t.Fatalf("idle-requested exit err = %v, want nil", err)
	}
	if err := normalizeAgentExitError(exitErr, false); err == nil || !strings.Contains(err.Error(), "agent command exited") {
		t.Fatalf("non-idle exit err = %v, want wrapped error", err)
	}
}

// TestApplyMCPEndpointRotationRewritesClaudeConfigAndPrompts is the #323
// rotation-recovery core: given a rotated runtime endpoint, the claude path
// rewrites the ephemeral --mcp-config in place AND sends a /mcp reconnect prompt
// into the PTY. The re-resolve uses the on-disk runtime file, not the launch
// literal.
func TestApplyMCPEndpointRotationRewritesClaudeConfigAndPrompts(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".striatum", "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath, cleanup, err := writeEphemeralMCPConfig(repo, "http://127.0.0.1:37637/mcp/sse", "dtok_launch")
	if err != nil {
		t.Fatalf("write ephemeral config: %v", err)
	}
	defer cleanup()

	rec := &writeRecorder{}
	var stderr strings.Builder
	rotated := "http://127.0.0.1:41283/mcp/sse"
	if err := applyMCPEndpointRotation("/home/x/.local/bin/claude", cfgPath, rotated, TokenMaterial{Token: "dtok_rotated"}, rec, &stderr); err != nil {
		t.Fatalf("applyMCPEndpointRotation: %v", err)
	}

	body, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(body), rotated) || !strings.Contains(string(body), "Bearer dtok_rotated") {
		t.Fatalf("config not rewritten to rotated endpoint/token:\n%s", body)
	}
	joined := strings.Join(rec.writes, "")
	if !strings.Contains(joined, "/mcp") || !strings.Contains(joined, rotated) {
		t.Fatalf("claude reconnect prompt missing /mcp + rotated endpoint:\n%s", joined)
	}
}

// TestApplyMCPEndpointRotationNoopAdapterFallsBack proves an adapter with no
// reconnect affordance (agy) does NOT crash and writes no PTY prompt — the
// config-rewrite-only no-op fallback.
func TestApplyMCPEndpointRotationNoopAdapterFallsBack(t *testing.T) {
	rec := &writeRecorder{}
	var stderr strings.Builder
	// No ephemeral config path for agy (its config is the gemini settings file);
	// applyMCPEndpointRotation must still succeed and emit no PTY prompt.
	if err := applyMCPEndpointRotation("/home/x/.local/bin/agy", "", "http://127.0.0.1:41283/mcp/sse", TokenMaterial{Token: "dtok"}, rec, &stderr); err != nil {
		t.Fatalf("applyMCPEndpointRotation agy: %v", err)
	}
	if len(rec.writes) != 0 {
		t.Fatalf("agy should get no reconnect prompt, got %#v", rec.writes)
	}
	if !strings.Contains(stderr.String(), "no reconnect prompt") {
		t.Fatalf("expected no-op fallback log, got: %s", stderr.String())
	}
}

// TestStartMCPEndpointRotationWatcherRewritesOnRotation is the focused watcher
// test: with a short poll interval and a fake runtime dir, rotating the runtime
// endpoint file mid-run causes the watcher to rewrite the ephemeral claude config
// to the new endpoint — without any live CLI or daemon.
func TestStartMCPEndpointRotationWatcherRewritesOnRotation(t *testing.T) {
	repo := t.TempDir()
	runtimeDir := t.TempDir()
	t.Setenv(EnvDaemonRuntimeDir, runtimeDir)
	if err := os.MkdirAll(filepath.Join(repo, ".striatum", "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	endpointFile := filepath.Join(runtimeDir, "mcp-http-endpoint")
	if err := os.WriteFile(endpointFile, []byte("127.0.0.1:37637\n"), 0o600); err != nil {
		t.Fatalf("write initial runtime endpoint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "client-token"), []byte("dtok_runtime\n"), 0o600); err != nil {
		t.Fatalf("write runtime token: %v", err)
	}

	launchEndpoint := "http://127.0.0.1:37637/mcp/sse"
	cfgPath, cleanup, err := writeEphemeralMCPConfig(repo, launchEndpoint, "dtok_launch")
	if err != nil {
		t.Fatalf("write ephemeral config: %v", err)
	}
	defer cleanup()

	t.Setenv("STRIATUM_AGENT_LOOP_MCP_ROTATION_POLL_MS", "20")
	t.Setenv("STRIATUM_AGENT_LOOP_SUBMIT_DELAY_MS", "0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := runConfig{
		RepoRoot:  repo,
		RunID:     "run_rot",
		SessionID: "sess_rot",
		Endpoint:  launchEndpoint,
		Token:     TokenMaterial{Token: "dtok_launch"},
		Command:   []string{"/home/x/.local/bin/claude"},
		Env:       os.Environ(),
	}
	rec := &syncWriteRecorder{}
	startMCPEndpointRotationWatcher(ctx, cfg, "/home/x/.local/bin/claude", cfgPath, rec, io.Discard)

	// Rotate the runtime endpoint file as a mid-run daemon restart would.
	rotated := "http://127.0.0.1:41283/mcp/sse"
	if err := os.WriteFile(endpointFile, []byte("127.0.0.1:41283\n"), 0o600); err != nil {
		t.Fatalf("rotate runtime endpoint: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		body, _ := os.ReadFile(cfgPath)
		if strings.Contains(string(body), rotated) {
			return // success: watcher rewrote the ephemeral config to the new endpoint
		}
		time.Sleep(20 * time.Millisecond)
	}
	body, _ := os.ReadFile(cfgPath)
	t.Fatalf("watcher did not rewrite config to rotated endpoint within deadline:\n%s", body)
}

// syncWriteRecorder is a goroutine-safe io.Writer for the watcher test (the
// watcher writes the reconnect prompt from a background goroutine).
type syncWriteRecorder struct {
	mu     sync.Mutex
	writes []string
}

func (r *syncWriteRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, string(p))
	return len(p), nil
}

func TestRunWithIOCodexReceivesBootstrapAsInitialPromptArg(t *testing.T) {
	dir := t.TempDir()
	repo := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	fakeCodex := filepath.Join(dir, "codex")
	if err := os.WriteFile(fakeCodex, []byte(`#!/bin/sh
: > "$STRIATUM_TEST_ARGS"
for arg in "$@"; do
  printf '%s\n---ARG---\n' "$arg" >> "$STRIATUM_TEST_ARGS"
done
`), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	cfg := runConfig{
		RepoRoot:   repo,
		RunID:      "run_codex_arg_prompt",
		SessionID:  "sess_codex_arg_prompt",
		Endpoint:   "http://127.0.0.1:42727/mcp",
		Token:      TokenMaterial{Token: "dtok_secret"},
		Command:    []string{fakeCodex, "--model", "gpt-5.5"},
		Env:        append(os.Environ(), "STRIATUM_TEST_ARGS="+argsPath),
		SocketPath: "/tmp/striatum-test.sock",
	}
	if err := runWithIO(context.Background(), cfg, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("runWithIO fake codex: %v", err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if !strings.Contains(string(args), "You are a Striatum lane agent for run run_codex_arg_prompt") {
		t.Fatalf("codex argv missing bootstrap prompt:\n%s", args)
	}
	if !strings.Contains(string(args), `mcp_servers.striatum.url="http://127.0.0.1:42727/mcp"`) {
		t.Fatalf("codex argv missing MCP URL override:\n%s", args)
	}
}
