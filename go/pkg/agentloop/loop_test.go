package agentloop

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentLoopSubmitSequenceDefault(t *testing.T) {
	os.Unsetenv("STRIATUM_AGENT_LOOP_SUBMIT_SEQUENCE")
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
	cmd, cleanup, mode, err := prepareLaneCommandForBootstrap(
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
	prompt := "bootstrap prompt\nwith multiple lines"
	cmd, cleanup, mode, err := prepareLaneCommandForBootstrap(
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
	if _, err := os.Stat(repo + "/.gemini/settings.json"); err != nil {
		t.Fatalf("agy MCP config should be written to .gemini/settings.json: %v", err)
	}
}

func TestPrepareLaneCommandForBootstrapKeepsClaudePTYSubmit(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(repo+"/.striatum/scratch", 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := "bootstrap prompt"
	cmd, cleanup, mode, err := prepareLaneCommandForBootstrap(
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
	if mode != bootstrapDeliveryPTYSubmit {
		t.Fatalf("mode = %q, want %q", mode, bootstrapDeliveryPTYSubmit)
	}
	if cmd[len(cmd)-1] == prompt {
		t.Fatalf("claude command should not receive bootstrap prompt argv: %#v", cmd)
	}
	if cmd[len(cmd)-1] != "--strict-mcp-config" {
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

func TestDaemonReceiverDisabledEnv(t *testing.T) {
	if daemonReceiverDisabled([]string{"STRIATUM_AGENT_LOOP_DAEMON_RECEIVER=on"}) {
		t.Fatalf("receiver should stay enabled for on")
	}
	if !daemonReceiverDisabled([]string{"STRIATUM_AGENT_LOOP_DAEMON_RECEIVER=off"}) {
		t.Fatalf("receiver should disable for off")
	}
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
