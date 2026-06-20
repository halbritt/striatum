package agentloop

import (
	"strings"
	"testing"
)

func TestAgentEnvironmentExportsEndpointTokenAndRepositoryID(t *testing.T) {
	env := AgentEnvironment([]string{"PATH=/bin", "STRIATUM_MCP_URL=http://old"}, BootstrapContext{
		SocketPath:   "/tmp/striatum.sock",
		RepoRoot:     "/repo",
		RepositoryID: "repo_1",
		RunID:        "run_1",
		SessionID:    "sess_1",
		Endpoint:     "http://127.0.0.1:1234/mcp/sse",
		Token:        TokenMaterial{Token: "tok", Source: EnvMCPToken},
	})

	assertEnvValue(t, env, EnvMCPURL, "http://127.0.0.1:1234/mcp/sse")
	assertEnvValue(t, env, EnvRepositoryID, "repo_1")
	assertEnvValue(t, env, "STRIATUM_REPO", "/repo")
	assertEnvValue(t, env, "STRIATUM_REPO_ROOT", "/repo")
	assertEnvValue(t, env, "STRIATUM_RUN_ID", "run_1")
	assertEnvValue(t, env, "STRIATUM_SESSION_ID", "sess_1")
	assertEnvValue(t, env, EnvDaemonSocket, "/tmp/striatum.sock")
	assertEnvValue(t, env, EnvMCPToken, "tok")
}

func TestAgentEnvironmentDoesNotInventRepositoryIDFromRepoRoot(t *testing.T) {
	env := AgentEnvironment(nil, BootstrapContext{
		RepoRoot: "/repo",
		Endpoint: "http://127.0.0.1:1234/mcp/sse",
	})
	if value, ok := envLookup(env, EnvRepositoryID); ok {
		t.Fatalf("%s = %q, want unset without a registered repository id", EnvRepositoryID, value)
	}
}

func TestAgentEnvironmentExposesLiteralTokenAndFilePointer(t *testing.T) {
	// RFC 0088 P3: both STRIATUM_MCP_TOKEN (literal) and STRIATUM_MCP_TOKEN_FILE
	// (pointer) must be set when we have a token loaded from a file. Codex
	// reads bearer from the literal env var; claude can use either. Setting
	// both is the only shape that lets every adapter authenticate.
	env := AgentEnvironment(nil, BootstrapContext{
		Endpoint: "http://127.0.0.1:1234/mcp/sse",
		Token:    TokenMaterial{Token: "tok", Source: "/runtime/client-token"},
	})
	assertEnvValue(t, env, EnvMCPToken, "tok")
	assertEnvValue(t, env, EnvMCPTokenFile, "/runtime/client-token")
}

func TestAgentEnvironmentNormalizesDumbTerminal(t *testing.T) {
	env := AgentEnvironment([]string{"TERM=dumb"}, BootstrapContext{
		Endpoint: "http://127.0.0.1:1234/mcp/sse",
	})
	assertEnvValue(t, env, "TERM", "xterm-256color")
}

func TestAgentEnvironmentPreservesUsableTerminal(t *testing.T) {
	env := AgentEnvironment([]string{"TERM=screen-256color"}, BootstrapContext{
		Endpoint: "http://127.0.0.1:1234/mcp/sse",
	})
	assertEnvValue(t, env, "TERM", "screen-256color")
}

func TestBuildBootstrapPromptNamesNativeMCPBoundary(t *testing.T) {
	prompt := BuildBootstrapPrompt(BootstrapContext{
		RepoRoot:     "/repo",
		RepositoryID: "repo_1",
		RunID:        "run_1",
		SessionID:    "sess_1",
		Endpoint:     "http://127.0.0.1:1234/mcp/sse",
		Token:        TokenMaterial{Source: "/runtime/client-token"},
	})
	for _, want := range []string{
		"Use the local Striatum MCP server at http://127.0.0.1:1234/mcp/sse.",
		"Use repository_id repo_1",
		"work.await_packet",
		"durable receive loop",
		// #80: explicit heartbeat-cadence guidance for long local work so a
		// healthy-but-slow lane is not classified stalled.
		"work.heartbeat",
		"local_work=true",
		"lease.heartbeat_after_seconds",
		// #85: steer lanes away from background MCP-discovery probes that idle the
		// lane and trip the discovery stall before any work is claimed.
		"Do NOT spawn a background task to probe",
		"interrogation_question",
		"idle_behavior=exit_session",
		"If work.await_packet returns no_work, do not poll.",
		"session.report",
		"instead of waiting silently in terminal text",
		"will not claim, complete, release, or spoon-feed packet JSON",
		"STRIATUM_MCP_TOKEN_FILE",
		// #86 / RFC 0096 §3: tell lanes to use the provided client and never
		// author a control-plane helper script in the target repo.
		"striatum CLI on your PATH",
		"Do NOT author a JSON-RPC/MCP client",
		"scripts/striatum_client.py",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("bootstrap prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "keep waiting by calling tools/call for work.await_packet again after a short pause") {
		t.Fatalf("bootstrap prompt still teaches no_work polling:\n%s", prompt)
	}
}

func TestBuildBootstrapPromptShowsToolsCallProtocolShape(t *testing.T) {
	prompt := BuildBootstrapPrompt(BootstrapContext{
		RepoRoot:     "/repo",
		RepositoryID: "repo_1",
		RunID:        "run_1",
		SessionID:    "sess_1",
		Endpoint:     "http://127.0.0.1:1234/mcp/sse",
		Token:        TokenMaterial{Source: "/runtime/client-token"},
	})

	for _, want := range []string{
		`"method":"tools/list"`,
		`"repository_id":"repo_1"`,
		`"session_id":"sess_1"`,
		`"method":"tools/call"`,
		`"name":"work.await_packet"`,
		`"arguments":{"session_id":"sess_1","lease_seconds":1800}`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("bootstrap prompt missing MCP protocol shape %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "then call work.await_packet with") {
		t.Fatalf("bootstrap prompt implies direct MCP daemon-method calls:\n%s", prompt)
	}
}

func assertEnvValue(t *testing.T, env []string, key string, want string) {
	t.Helper()
	got, ok := envLookup(env, key)
	if !ok {
		t.Fatalf("%s not found in environment", key)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}
