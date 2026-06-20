package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/rpc"
)

func TestWorkflowValidateJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeWorkflow(t, dir, basicWorkflow())
	var stdout, stderr bytes.Buffer
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldwd)
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	exitCode := run([]string{"workflow", "validate", "--json", filepath.Base(path)}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true {
		t.Fatalf("payload ok = %#v", payload["ok"])
	}
	data := payload["data"].(map[string]any)
	if data["workflow_id"] != "go-cli-test" {
		t.Fatalf("workflow_id = %#v", data["workflow_id"])
	}
}

func TestWorkflowValidateAllowsLeadingGlobalOptions(t *testing.T) {
	dir := t.TempDir()
	path := writeWorkflow(t, dir, basicWorkflow())
	cwd := t.TempDir()
	var stdout, stderr bytes.Buffer
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldwd)
	})
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	exitCode := run([]string{"--repo", dir, "--json", "workflow", "validate", filepath.Base(path), "--allow-same-model-pairing"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true {
		t.Fatalf("payload ok = %#v", payload["ok"])
	}
}

func TestTopLevelHelpAndUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exitCode := run([]string{"--help"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("help exit = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "usage: striatum") {
		t.Fatalf("help output = %q", stdout.String())
	}
	// #104: help lists the control surface (the work loop + key commands) so a
	// self-driving lane does not have to fall back to raw MCP tools/list.
	help := stdout.String()
	for _, want := range []string{"work-packet loop", "work.await_packet", "claim-next", "publish-artifact", "complete", "run ", "drive", "operator", "bootstrap", "recovery", "register-session", "--help"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help output missing %q; got:\n%s", want, help)
		}
	}
	// #122: top-level help must also list the local workflow authoring
	// subcommands so they are discoverable alongside the daemon-routed
	// workflow accept-risk | accepted-risks subcommands.
	for _, want := range []string{"validate", "generate", "templates"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help output missing workflow authoring subcommand %q; got:\n%s", want, help)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := run([]string{"self-update"}, &stdout, &stderr); exitCode != 2 {
		t.Fatalf("unknown command exit = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown command: self-update") {
		t.Fatalf("unknown command stderr = %q", stderr.String())
	}
}

func TestRunDriveHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"run", "drive", "--help"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	for _, want := range []string{"usage: striatum run drive", "--run-id", "--interval", "--provider-auth-gate", "auto|required|off", "--once", "--json", "existing daemon RPC methods"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help output missing %q; got:\n%s", want, stdout.String())
		}
	}
}

func TestRunDriveRejectsBadProviderAuthGate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"run", "drive", "--run-id", "run_1", "--provider-auth-gate", "maybe"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "provider_auth_gate must be one of auto, required, off") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestOperatorBootstrapHelpAndDocsStayInSync(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"operator", "bootstrap", "--help"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	for _, want := range []string{"usage: striatum operator bootstrap", "--json", "--markdown", "--operator-docs-root", "--limit"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help output missing %q; got:\n%s", want, stdout.String())
		}
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "reference", "cli-reference.md"))
	if err != nil {
		t.Fatalf("read cli-reference: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "striatum operator bootstrap") {
		t.Fatalf("cli-reference must document operator bootstrap")
	}
	if strings.Contains(text, "striatum operator current-brief") {
		t.Fatalf("cli-reference still documents retired operator current-brief")
	}
}

func TestOperatorBootstrapRejectsDisabledMarkdownOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"operator", "bootstrap", "--markdown=false"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exit = %d, stdout = %s, stderr = %s", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--markdown=false is not supported") {
		t.Fatalf("stderr missing markdown guidance: %s", stderr.String())
	}
}

func TestOperatorBootstrapJSONSummarizesAndCapsDaemonReads(t *testing.T) {
	t.Setenv("STRIATUM_REPOSITORY_ID", "")
	repoRoot := writeBootstrapRepo(t, "2.31.0", true)
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	server := rpc.NewServer()
	server.Register("repo.resolve", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if envelope.Params["path"] == "" {
			t.Fatalf("repo.resolve path missing: %#v", envelope.Params)
		}
		return map[string]any{"repository_id": "repo_1", "repo_root": repoRoot}, nil
	})
	server.Register("status", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if envelope.Params["repository_id"] != "repo_1" || stringAny(envelope.Params["run_limit"]) != "0" {
			t.Fatalf("status params = %#v", envelope.Params)
		}
		runs := []any{}
		claimable := []any{}
		blockers := []any{}
		checkpoints := []any{}
		for i := 0; i < 8; i++ {
			runs = append(runs, map[string]any{"run_id": "run_" + string(rune('a'+i)), "state": "running", "branch_name": "main"})
			claimable = append(claimable, map[string]any{"run_id": "run_a", "job_id": "job_claim_" + string(rune('a'+i)), "state": "queued", "role_id": "author", "lane_id": "codex"})
			blockers = append(blockers, map[string]any{"blocker_id": "blk_" + string(rune('a'+i)), "run_id": "run_a", "job_id": "job_x", "severity": "blocked", "blocker_kind": "missing_input", "description": strings.Repeat("x", 240)})
			checkpoints = append(checkpoints, map[string]any{"blocker_id": "hcp_" + string(rune('a'+i)), "run_id": "run_a", "severity": "human_checkpoint", "blocker_kind": "operator_decision", "description": "needs decision"})
		}
		runs = append(runs, map[string]any{"run_id": "run_done", "state": "completed"})
		return map[string]any{
			"runs":              runs,
			"claimable_jobs":    claimable,
			"open_blockers":     blockers,
			"human_checkpoints": checkpoints,
			"next_actions":      []any{"a", "b", "c", "d", "e", "f"},
		}, nil
	})
	server.Register("doctor", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		problems := []any{"p1", "p2", "p3", "p4", "p5", "p6"}
		warnings := []any{"skills_outdated: project codex bundle was generated by an older runner", "w2", "w3", "w4", "w5", "w6"}
		return map[string]any{
			"ok":                  false,
			"schema_version":      float64(22),
			"problems":            problems,
			"warnings":            warnings,
			"stale_leases":        float64(2),
			"waiting_human":       float64(1),
			"needs_operator":      float64(1),
			"needs_operator_runs": []any{"run_a", "run_b", "run_c", "run_d"},
			"skills":              map[string]any{"checked": true, "current_version": "2.31.0"},
		}, nil
	})
	startTestRPCServer(t, socket, server)
	tokenFile := filepath.Join(t.TempDir(), "client-token")
	if err := os.WriteFile(tokenFile, []byte("tok.secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--daemon-socket", socket, "--capability-token-file", tokenFile, "--repo", repoRoot, "operator", "bootstrap", "--json", "--limit", "3"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	var packet operatorBootstrapPacket
	if err := json.Unmarshal(stdout.Bytes(), &packet); err != nil {
		t.Fatalf("decode bootstrap JSON: %v; body=%s", err, stdout.String())
	}
	if packet.SchemaVersion != operatorBootstrapSchemaVersion {
		t.Fatalf("schema = %q", packet.SchemaVersion)
	}
	if !packet.Daemon.Reachable || !packet.Daemon.Authorized {
		t.Fatalf("daemon summary = %#v", packet.Daemon)
	}
	if packet.Repository.RepositoryID != "repo_1" {
		t.Fatalf("repository = %#v", packet.Repository)
	}
	if packet.OperatorBrief.Status != "current" {
		t.Fatalf("operator brief = %#v", packet.OperatorBrief)
	}
	if packet.Frontier.ClaimableCount != 8 || len(packet.Frontier.ClaimableJobs) != 3 {
		t.Fatalf("claimable summary = %#v", packet.Frontier)
	}
	if len(packet.Frontier.ActiveRuns) != 3 || packet.Frontier.RunCounts["completed"] != 1 {
		t.Fatalf("run summary = %#v", packet.Frontier)
	}
	if packet.Doctor.ProblemCount != 6 || len(packet.Doctor.Problems) != 3 {
		t.Fatalf("doctor summary = %#v", packet.Doctor)
	}
	if packet.Skills.Status != "attention_needed" || len(packet.Skills.RecoveryCommands) == 0 {
		t.Fatalf("skills summary = %#v", packet.Skills)
	}
}

func TestOperatorBootstrapJSONDegradesWhenDaemonUnavailable(t *testing.T) {
	t.Setenv("STRIATUM_REPOSITORY_ID", "")
	repoRoot := writeBootstrapRepo(t, "2.31.0", false)
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--daemon-socket", filepath.Join(t.TempDir(), "missing.sock"), "--capability-token", "tok.secret", "--repo", repoRoot, "operator", "bootstrap", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	var packet operatorBootstrapPacket
	if err := json.Unmarshal(stdout.Bytes(), &packet); err != nil {
		t.Fatalf("decode bootstrap JSON: %v; body=%s", err, stdout.String())
	}
	if packet.OK || packet.Daemon.Reachable {
		t.Fatalf("daemon should be degraded: %#v", packet.Daemon)
	}
	if !strings.Contains(strings.Join(packet.NextActions, "\n"), "striatum daemon status") {
		t.Fatalf("next actions should include daemon recovery: %#v", packet.NextActions)
	}
	if packet.OperatorBrief.Status != "stale" {
		t.Fatalf("brief should be stale against VERSION: %#v", packet.OperatorBrief)
	}
}

func TestRunDriveDispatchesThroughRPC(t *testing.T) {
	t.Setenv("STRIATUM_REPOSITORY_ID", "")
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	server := rpc.NewServer()
	wantRepoPath, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	server.Register("repo.resolve", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if envelope.Params["path"] != wantRepoPath {
			t.Fatalf("repo.resolve path = %#v, want %q", envelope.Params["path"], wantRepoPath)
		}
		return map[string]any{"repository_id": "repo_1", "repo_root": t.TempDir()}, nil
	})
	server.Register("run.detail", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if envelope.Params["repository_id"] != "repo_1" || envelope.Params["run_id"] != "run_1" {
			t.Fatalf("params = %#v", envelope.Params)
		}
		return map[string]any{
			"run":  map[string]any{"run_id": "run_1", "state": "completed"},
			"jobs": []any{},
		}, nil
	})
	startTestRPCServer(t, socket, server)
	tokenFile := filepath.Join(t.TempDir(), "client-token")
	if err := os.WriteFile(tokenFile, []byte("tok.secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--daemon-socket", socket, "--capability-token-file", tokenFile, "--repo", ".", "run", "drive", "--run-id", "run_1", "--once", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"action":"terminal"`) || !strings.Contains(stdout.String(), `"result":"completed"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestRetiredCLICompatibilityCommandsStayUnavailable(t *testing.T) {
	tests := [][]string{
		{"--no-daemon", "status"},
		{"daemon", "migrate"},
		{"daemon", "migrate-repo-local"},
		{"scaffold", "ddd"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exitCode := run(args, &stdout, &stderr)
			if exitCode == 0 {
				t.Fatalf("expected nonzero exit, stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
			if exitCode != 2 && exitCode != 11 && exitCode != 12 {
				t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
			}
		})
	}
}

func TestBareLocalInstallCommandsShowUsage(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"plugin"}, want: "usage: striatum plugin {install|uninstall}"},
		{args: []string{"plugin", "--help"}, want: "usage: striatum plugin {install|uninstall}"},
		{args: []string{"skills"}, want: "usage: striatum skills install"},
		{args: []string{"skills", "--help"}, want: "usage: striatum skills install"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exitCode := run(tt.args, &stdout, &stderr)
			if len(tt.args) == 1 && exitCode != 2 {
				t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
			}
			if len(tt.args) == 2 && exitCode != 0 {
				t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
			}
			if !strings.Contains(stdout.String()+stderr.String(), tt.want) {
				t.Fatalf("usage missing %q; stdout=%q stderr=%q", tt.want, stdout.String(), stderr.String())
			}
		})
	}
}

func TestWorkflowValidateJSONErrorEnvelope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"striatum.workflow.v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldwd)
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	exitCode := run([]string{"workflow", "validate", "--json", filepath.Base(path)}, &stdout, &stderr)
	if exitCode != 8 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != false {
		t.Fatalf("payload ok = %#v", payload["ok"])
	}
	errPayload := payload["error"].(map[string]any)
	if errPayload["code"] != "workflow_invalid" {
		t.Fatalf("error = %#v", errPayload)
	}
}

func TestWorkflowValidateRefusesSameModelPairingUnlessAllowed(t *testing.T) {
	dir := t.TempDir()
	path := writeWorkflow(t, dir, sameModelWorkflow())
	var stdout, stderr bytes.Buffer
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldwd)
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	exitCode := run([]string{"workflow", "validate", filepath.Base(path)}, &stdout, &stderr)
	if exitCode != 8 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "same model family") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = run([]string{"workflow", "validate", "--allow-same-model-pairing", filepath.Base(path)}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("allowed exit = %d, stderr = %s", exitCode, stderr.String())
	}
}

func TestWorkflowValidateRefusesSameModelAdjudicatorPairUnlessAllowed(t *testing.T) {
	dir := t.TempDir()
	path := writeWorkflow(t, dir, sameModelAdjudicatorWorkflow())
	var stdout, stderr bytes.Buffer
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldwd)
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	// Default refusal: an adjudicator sharing a model family with the upstream
	// holder it adjudicates is refused (RFC 0093 + RFC 0064 same-model gate).
	exitCode := run([]string{"workflow", "validate", filepath.Base(path)}, &stdout, &stderr)
	if exitCode != 8 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "adjudicator job") || !strings.Contains(stderr.String(), "same model family") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	// Override path: the audited --allow-same-model-pairing flag clears it.
	stdout.Reset()
	stderr.Reset()
	exitCode = run([]string{"workflow", "validate", "--allow-same-model-pairing", filepath.Base(path)}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("allowed exit = %d, stderr = %s", exitCode, stderr.String())
	}
}

// TestWorkflowValidateRefusesClaudePrintLane proves `workflow validate` refuses
// a `claude --print` lane (exit 8) with a message naming the 2026-06-15
// cost consequence, and that the inline `allow_claude_print: true` override
// clears it. (#199)
func TestWorkflowValidateRefusesClaudePrintLane(t *testing.T) {
	dir := t.TempDir()
	path := writeWorkflow(t, dir, claudePrintWorkflow(false))
	var stdout, stderr bytes.Buffer
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldwd)
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	exitCode := run([]string{"workflow", "validate", filepath.Base(path)}, &stdout, &stderr)
	if exitCode != 8 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "2026-06-15") {
		t.Fatalf("refusal must name the 2026-06-15 cost consequence; stderr = %s", stderr.String())
	}
	// Override path: the inline allow_claude_print: true clears the refusal.
	stdout.Reset()
	stderr.Reset()
	overridePath := writeWorkflow(t, dir, claudePrintWorkflow(true))
	exitCode = run([]string{"workflow", "validate", filepath.Base(overridePath)}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("override exit = %d, stderr = %s", exitCode, stderr.String())
	}
}

func TestWorkflowValidateRefusesCodexExecLane(t *testing.T) {
	dir := t.TempDir()
	path := writeWorkflow(t, dir, codexExecWorkflow())
	var stdout, stderr bytes.Buffer
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldwd)
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	exitCode := run([]string{"workflow", "validate", filepath.Base(path)}, &stdout, &stderr)
	if exitCode != 8 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "codex exec") || !strings.Contains(stderr.String(), "#267") {
		t.Fatalf("refusal should name codex exec and #267; stderr = %s", stderr.String())
	}
}

func TestWorkflowValidateRefusesAutonomousSharedCheckoutRepoWrite(t *testing.T) {
	dir := t.TempDir()
	path := writeWorkflow(t, dir, autonomousSharedCheckoutWorkflow(false))
	var stdout, stderr bytes.Buffer
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldwd)
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	exitCode := run([]string{"workflow", "validate", filepath.Base(path)}, &stdout, &stderr)
	if exitCode != 8 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "autonomous repo-write lanes must use per-job worktrees") {
		t.Fatalf("stderr = %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	overridePath := writeWorkflow(t, dir, autonomousSharedCheckoutWorkflow(true))
	exitCode = run([]string{"workflow", "validate", filepath.Base(overridePath)}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("override exit = %d, stderr = %s", exitCode, stderr.String())
	}
}

func autonomousSharedCheckoutWorkflow(override bool) string {
	overrideFields := ""
	if override {
		overrideFields = `, "allow_shared_checkout_repo_write": true, "shared_checkout_repo_write_rationale": "interactive-human compatibility fixture"`
	}
	return `{
  "schema_version": "striatum.workflow.v1",
  "workflow_id": "go-cli-test",
  "workflow_version": "test",
  "name": "Go CLI Test",
  "context_docs": [],
  "coordinator": {"role_id": "coordinator", "lane_id": "codex"},
  "parallelism": {"mode": "declared", "max_active_jobs": 1},
  "branch": {"mode": "confirm", "suggested_name": "main"},
  "lanes": {"codex": {"adapter": "process", "command": ["codex"], "model": "codex", "adapter_capabilities": {"agent_loop": true}` + overrideFields + `}},
  "roles": {"coordinator": {"description": "Coordinator"}, "worker": {"description": "Worker"}},
  "jobs": [{
    "id": "build",
    "type": "build",
    "role_id": "worker",
    "lane_id": "codex",
    "task_prompt": {"inline": "do work"},
    "write_scope": {"mode": "repo_write", "repo_write": true, "allowed_paths": ["out/"], "forbidden_paths": []},
    "expected_artifacts": []
  }],
  "edges": [],
  "cycles": []
}`
}

func claudePrintWorkflow(override bool) string {
	allow := ""
	if override {
		allow = `, "allow_claude_print": true`
	}
	return `{
  "schema_version": "striatum.workflow.v1",
  "workflow_id": "go-cli-test",
  "workflow_version": "test",
  "name": "Go CLI Test",
  "context_docs": [],
  "coordinator": {"role_id": "coordinator", "lane_id": "claude"},
  "parallelism": {"mode": "declared", "max_active_jobs": 1},
  "branch": {"mode": "confirm", "suggested_name": "main"},
  "lanes": {"claude": {"adapter": "process", "command": ["claude", "--print"], "model": "claude"` + allow + `}},
  "roles": {"coordinator": {"description": "Coordinator"}, "worker": {"description": "Worker"}},
  "jobs": [{
    "id": "build",
    "type": "build",
    "role_id": "worker",
    "lane_id": "claude",
    "task_prompt": {"inline": "do work"},
    "write_scope": {"mode": "repo_write", "repo_write": true, "allowed_paths": ["out/"], "forbidden_paths": []},
    "expected_artifacts": []
  }],
  "edges": [],
  "cycles": []
}`
}

func codexExecWorkflow() string {
	return `{
  "schema_version": "striatum.workflow.v1",
  "workflow_id": "go-cli-test",
  "workflow_version": "test",
  "name": "Go CLI Test",
  "context_docs": [],
  "coordinator": {"role_id": "coordinator", "lane_id": "codex"},
  "parallelism": {"mode": "declared", "max_active_jobs": 1},
  "branch": {"mode": "confirm", "suggested_name": "main"},
  "lanes": {"codex": {"adapter": "process", "command": ["codex", "exec", "-"], "model": "codex"}},
  "roles": {"coordinator": {"description": "Coordinator"}, "worker": {"description": "Worker"}},
  "jobs": [{
    "id": "build",
    "type": "build",
    "role_id": "worker",
    "lane_id": "codex",
    "task_prompt": {"inline": "do work"},
    "write_scope": {"mode": "review_only_artifact", "repo_write": false, "allowed_paths": ["out/"], "forbidden_paths": []},
    "expected_artifacts": [{"logical_name": "result", "kind": "finding", "path": "out/result.md", "required": true}]
  }],
  "edges": [],
  "cycles": []
}`
}

func sameModelAdjudicatorWorkflow() string {
	return `{
  "schema_version": "striatum.workflow.v1.1",
  "workflow_id": "go-cli-adjudicator-test",
  "workflow_version": "test",
  "name": "Go CLI Adjudicator Test",
  "context_docs": [],
  "coordinator": {"role_id": "holder", "lane_id": "codex"},
  "parallelism": {"mode": "declared", "max_active_jobs": 1},
  "branch": {"mode": "confirm", "suggested_name": "main"},
  "lanes": {"codex": {"adapter": "process", "command": ["true"], "model": "codex", "display_model": "Codex"}},
  "roles": {"holder": {"description": "Holder"}, "adjudicator": {"description": "Adjudicator"}},
  "jobs": [
    {
      "id": "holder",
      "type": "build",
      "role_id": "holder",
      "lane_id": "codex",
      "task_prompt": {"inline": "hold"},
      "write_scope": {"mode": "repo_write", "repo_write": true, "allowed_paths": ["dialogue/holder/"], "forbidden_paths": []},
      "expected_artifacts": []
    },
    {
      "id": "adjudicate",
      "type": "build",
      "role_id": "adjudicator",
      "lane_id": "codex",
      "task_prompt": {"inline": "adjudicate"},
      "write_scope": {"mode": "repo_write", "repo_write": true, "allowed_paths": ["dialogue/adjudicator/"], "forbidden_paths": []},
      "expected_artifacts": [{"logical_name": "collaboration_ledger_${cycle}", "kind": "collaboration_ledger", "path": "dialogue/adjudicator/COLLABORATION_LEDGER_${cycle}.md", "required": true}]
    }
  ],
  "edges": [{"from": "holder", "to": "adjudicate", "on": "completed"}],
  "cycles": []
}`
}

func TestDaemonRouteDispatchesThroughRPC(t *testing.T) {
	t.Setenv("STRIATUM_REPOSITORY_ID", "")
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	server := rpc.NewServer()
	server.Register("repo.resolve", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		return map[string]any{"repository_id": "repo_1"}, nil
	})
	server.Register("status", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if envelope.Params["repository_id"] != "repo_1" || envelope.Params["run_id"] != "run_1" {
			t.Fatalf("params = %#v", envelope.Params)
		}
		return map[string]any{"state": "ok"}, nil
	})
	startTestRPCServer(t, socket, server)
	tokenFile := filepath.Join(t.TempDir(), "client-token")
	if err := os.WriteFile(tokenFile, []byte("tok.secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--daemon-socket", socket, "--capability-token-file", tokenFile, "--repo", ".", "status", "--run-id", "run_1"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"state":"ok"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestDaemonErrorExitCode(t *testing.T) {
	t.Setenv("STRIATUM_REPOSITORY_ID", "")
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	server := rpc.NewServer()
	server.Register("repo.resolve", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		return nil, rpc.NewError("repo_not_registered", "missing repo", nil)
	})
	startTestRPCServer(t, socket, server)
	tokenFile := filepath.Join(t.TempDir(), "client-token")
	if err := os.WriteFile(tokenFile, []byte("tok.secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--daemon-socket", socket, "--capability-token-file", tokenFile, "status"}, &stdout, &stderr)
	if exitCode != 12 {
		t.Fatalf("exit = %d stderr=%s", exitCode, stderr.String())
	}
}

func TestUnknownCommandStillReturnsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"does-not-exist"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exit = %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func startTestRPCServer(t *testing.T, socket string, server *rpc.Server) {
	t.Helper()
	listener, err := rpc.ListenUnix(socket)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				_ = server.ServeConn(context.Background(), conn, conn.RemoteAddr().String())
			}(conn)
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
}

func writeWorkflow(t *testing.T, dir string, body string) string {
	t.Helper()
	path := filepath.Join(dir, "workflow.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeBootstrapRepo(t *testing.T, version string, currentBrief bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte(version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	operatorDir := filepath.Join(dir, "docs", "operator")
	if err := os.MkdirAll(operatorDir, 0o700); err != nil {
		t.Fatal(err)
	}
	mentioned := "v0.0.1"
	if currentBrief {
		mentioned = "v" + strings.TrimPrefix(version, "v")
	}
	brief := `---
schema_version: "striatum.operator_brief.v1"
artifact_kind: "operator_brief"
brief_id: "brief_test"
supersedes: null
scope_links: ["docs/reference/spec.md"]
context_budget_lines: 20
retrieval_priority: "high"
status: "current"
---

# Operator Brief
author: operator-codex-001

Latest release is ` + mentioned + `.
`
	if err := os.WriteFile(filepath.Join(operatorDir, "BRIEF.md"), []byte(brief), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func basicWorkflow() string {
	return `{
  "schema_version": "striatum.workflow.v1",
  "workflow_id": "go-cli-test",
  "workflow_version": "test",
  "name": "Go CLI Test",
  "context_docs": [],
  "coordinator": {"role_id": "coordinator", "lane_id": "codex"},
  "parallelism": {"mode": "declared", "max_active_jobs": 1},
  "branch": {"mode": "confirm", "suggested_name": "main"},
  "lanes": {"codex": {"adapter": "process", "command": ["true"], "model": "codex"}},
  "roles": {"coordinator": {"description": "Coordinator"}, "worker": {"description": "Worker"}},
  "jobs": [{
    "id": "build",
    "type": "build",
    "role_id": "worker",
    "lane_id": "codex",
    "task_prompt": {"inline": "do work"},
    "write_scope": {"mode": "repo_write", "repo_write": true, "allowed_paths": ["out/"], "forbidden_paths": []},
    "expected_artifacts": []
  }],
  "edges": [],
  "cycles": []
}`
}

func sameModelWorkflow() string {
	return `{
  "schema_version": "striatum.workflow.v1",
  "workflow_id": "go-cli-test",
  "workflow_version": "test",
  "name": "Go CLI Test",
  "context_docs": [],
  "coordinator": {"role_id": "coordinator", "lane_id": "codex"},
  "parallelism": {"mode": "declared", "max_active_jobs": 1},
  "branch": {"mode": "confirm", "suggested_name": "main"},
  "lanes": {"codex": {"adapter": "process", "command": ["true"], "model": "codex", "display_model": "Codex"}},
  "roles": {"coordinator": {"description": "Coordinator"}, "worker": {"description": "Worker"}, "reviewer": {"description": "Reviewer"}},
  "jobs": [
    {
      "id": "build",
      "type": "build",
      "role_id": "worker",
      "lane_id": "codex",
      "task_prompt": {"inline": "do work"},
      "write_scope": {"mode": "repo_write", "repo_write": true, "allowed_paths": ["out/"], "forbidden_paths": []},
      "expected_artifacts": []
    },
    {
      "id": "review",
      "type": "review",
      "role_id": "reviewer",
      "lane_id": "codex",
      "task_prompt": {"inline": "review"},
      "write_scope": {"mode": "review_only_artifact", "repo_write": false, "allowed_paths": ["reviews/"], "forbidden_paths": []},
      "expected_artifacts": []
    }
  ],
  "edges": [{"from": "build", "to": "review", "on": "completed"}],
  "cycles": []
}`
}

func TestWorkflowGenerateConversationPreview(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--repo", dir, "--json", "workflow", "generate",
		"--shape", "conversation", "--option", "topic=trajectory design",
		"--workflow-id", "conv-cli-test"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v; out=%s", err, stdout.String())
	}
	if payload["ok"] != true {
		t.Fatalf("ok = %#v", payload["ok"])
	}
	data := payload["data"].(map[string]any)
	if data["shape"] != "conversation" || data["workflow_id"] != "conv-cli-test" {
		t.Fatalf("data = %#v", data)
	}
	planned, ok := data["planned"].([]any)
	if !ok || len(planned) == 0 {
		t.Fatalf("expected planned files, got %#v", data["planned"])
	}
	// Preview must not write anything to the repo.
	if _, err := os.Stat(filepath.Join(dir, "docs", "operator", "workflows", "conv-cli-test", "workflow.json")); !os.IsNotExist(err) {
		t.Fatalf("preview wrote workflow.json or stat failed: %v", err)
	}
}

// #187: the lane sets the catalog recommends for code-change work
// (author_reviewer, multi_review, single_agent) advertise
// required_options like "lanes.author.command". Those keys must be
// settable via `--option lanes.<id>.command=<JSON array>` so the
// advertised lane sets are actually generatable from the CLI.
func TestWorkflowGenerateRoutesLaneCommandOption(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--repo", dir, "--json", "workflow", "generate",
		"--shape", "code_change", "--lane-set", "author_reviewer",
		"--option", `lanes.author.command=["claude","--dangerously-skip-permissions"]`,
		"--option", `lanes.reviewer.command=["codex"]`,
		"--workflow-id", "lane-cmd-test", "--write"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v; out=%s", err, stdout.String())
	}
	if payload["ok"] != true {
		t.Fatalf("ok = %#v; stderr=%s", payload["ok"], stderr.String())
	}
	data := payload["data"].(map[string]any)
	if data["workflow_id"] != "lane-cmd-test" {
		t.Fatalf("data = %#v", data)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "docs", "operator", "workflows", "lane-cmd-test", "workflow.json"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow map[string]any
	if err := json.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	lanes := workflow["lanes"].(map[string]any)
	reviewer := lanes["reviewer"].(map[string]any)
	capabilities := reviewer["adapter_capabilities"].(map[string]any)
	if capabilities["agent_loop"] != true {
		t.Fatalf("reviewer adapter_capabilities = %#v", capabilities)
	}
	supervision := reviewer["supervision"].(map[string]any)
	if supervision["transport"] != "pty_helper" {
		t.Fatalf("reviewer supervision = %#v", supervision)
	}
}

func TestWorkflowTemplatesListIncludesConversation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--json", "workflow", "templates", "list", "--kind", "shape"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d, stderr = %s", exitCode, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v; out=%s", err, stdout.String())
	}
	data := payload["data"].(map[string]any)
	templates := data["templates"].([]any)
	found := false
	for _, entry := range templates {
		if m, ok := entry.(map[string]any); ok && m["template_id"] == "conversation" {
			found = true
		}
	}
	if !found {
		t.Fatalf("conversation shape not listed in templates: %s", stdout.String())
	}
}
