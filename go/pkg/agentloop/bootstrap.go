package agentloop

import (
	"fmt"
	"sort"
	"strings"
)

const (
	EnvMCPToken     = "STRIATUM_MCP_TOKEN"
	EnvMCPTokenFile = "STRIATUM_MCP_TOKEN_FILE"
	EnvRepositoryID = "STRIATUM_REPOSITORY_ID"
	EnvDaemonSocket = "STRIATUM_DAEMON_SOCKET"
	// EnvMCPBootEpoch carries the boot epoch of the daemon process run the lane
	// was launched against (#316). The supervisor injects it; injectLaneMCPConfig
	// folds it into each adapter's MCP client as the mcp.HeaderBootEpoch request
	// header so every MCP request echoes it. The daemon rejects a request whose
	// presented epoch differs from its own live epoch (a recycled-port hit).
	EnvMCPBootEpoch = "STRIATUM_MCP_BOOT_EPOCH"
)

type BootstrapContext struct {
	SocketPath   string
	RepoRoot     string
	RepositoryID string
	RunID        string
	SessionID    string
	Endpoint     string
	Token        TokenMaterial
}

func BuildBootstrapPrompt(ctx BootstrapContext) string {
	tokenInstruction := "Authenticate with STRIATUM_MCP_TOKEN if it is set; do not print capability tokens."
	if ctx.Token.Source != "" && ctx.Token.Source != EnvMCPToken {
		tokenInstruction = fmt.Sprintf("Authenticate with the bearer token referenced by STRIATUM_MCP_TOKEN_FILE (%s); do not print capability tokens.", ctx.Token.Source)
	}
	repositoryInstruction := "Use the registered repository_id for this target repository when MCP tools require repository_id."
	if ctx.RepositoryID != "" {
		repositoryInstruction = fmt.Sprintf("Use repository_id %s when MCP tools require repository_id.", ctx.RepositoryID)
	}
	repositoryExample := ctx.RepositoryID
	if repositoryExample == "" {
		repositoryExample = "<repository-id>"
	}
	sessionExample := ctx.SessionID
	if sessionExample == "" {
		sessionExample = "<session-id>"
	}

	return fmt.Sprintf(`You are a Striatum lane agent for run %s, session %s.
Target repository root: %s
Use the local Striatum MCP server at %s.
The same endpoint is available in STRIATUM_MCP_URL. %s
%s
Use MCP protocol methods only through tools/list and tools/call. Do not call daemon methods such as work.await_packet as top-level MCP JSON-RPC methods; put the daemon method in params.name.
First discover tools with {"method":"tools/list","params":{"repository_id":"%s","session_id":"%s"}}.
Then await work with {"method":"tools/call","params":{"name":"work.await_packet","repository_id":"%s","arguments":{"session_id":"%s","lease_seconds":1800}}}.
Do NOT spawn a background task to probe, curl, or "discover" the MCP endpoint before working: it is already configured above and reachable now. Use tools/list, then tools/call for work.await_packet directly in the foreground — a background discovery probe leaves the lane idle and trips the MCP-discovery stall before any work is claimed.
When work.await_packet returns a work packet, process it to completion yourself, synchronously and inline, before awaiting again: acknowledge it, then do exactly what the packet describes (read its context, edit files, run its commands), publish every expected artifact, and call tools/call for work.complete (use review.submit for review jobs). Do NOT just save the packet, spawn a background poller, or treat receiving a packet as a substitute for doing its work — each packet must be fully executed and completed before the next work.await_packet.
This is a durable receive loop while the lane is active: after every work.complete, work.release, interrogation.answer, or conversation turn, call tools/call for work.await_packet again exactly once to see whether more work is already ready.
While working a single packet, if you spend more than a few minutes on local work (reading source, editing files, running commands) between MCP calls, call tools/call for work.heartbeat with local_work=true periodically — at least every few minutes, honoring the packet's lease.heartbeat_after_seconds — to keep your lease and tool-progress clock alive: the daemon classifies a lease that stops heartbeating or making local-work progress as stalled even while you are actively working locally.
If work.await_packet returns an interrogation_question, answer it with tools/call for interrogation.answer, then immediately return to tools/call for work.await_packet.
If work.await_packet returns no_work, do not poll. Treat idle_behavior=exit_session as a normal terminal idle signal: stop this lane process cleanly so run drive or a future wake mechanism can start a fresh session when work is queued.
If you need input or are blocked before work.await_packet, call tools/call for session.report with report_kind question or escalate instead of waiting silently in terminal text.
Use MCP tools to acknowledge work, publish artifacts, report blockers, complete work, or release work. This PTY supervisor will not claim, complete, release, or spoon-feed packet JSON for you.
A working Striatum client is already provided: the MCP server above and the striatum CLI on your PATH both reach this daemon. Use them directly. Do NOT author a JSON-RPC/MCP client (e.g. scripts/striatum_client.py) or any other control-plane helper inside the target repository — that is control-plane scratch, not project source, and writing it pollutes the work tree.
Stay inside the active work packet write scope, treat .striatum/ as operational scratch, and follow the packet commands exactly.
`, ctx.RunID, ctx.SessionID, ctx.RepoRoot, ctx.Endpoint, tokenInstruction, repositoryInstruction, repositoryExample, sessionExample, repositoryExample, sessionExample)
}

func AgentEnvironment(base []string, ctx BootstrapContext) []string {
	updates := map[string]string{
		EnvMCPURL:             ctx.Endpoint,
		"STRIATUM_REPO":       ctx.RepoRoot,
		"STRIATUM_REPO_ROOT":  ctx.RepoRoot,
		"STRIATUM_RUN_ID":     ctx.RunID,
		"STRIATUM_SESSION_ID": ctx.SessionID,
	}
	if ctx.RepositoryID != "" {
		updates[EnvRepositoryID] = ctx.RepositoryID
	}
	if ctx.SocketPath != "" {
		updates[EnvDaemonSocket] = ctx.SocketPath
	}
	// Always give the literal token via STRIATUM_MCP_TOKEN when we have it —
	// codex reads the bearer from this env var (its config says
	// `bearer_token_env_var = "STRIATUM_MCP_TOKEN"`) and gets nothing if only
	// the file pointer is set. The file pointer is still exposed for adapters
	// that prefer to read the token from a 0600 file (claude's plugin config
	// can reference it).
	if ctx.Token.Token != "" {
		updates[EnvMCPToken] = ctx.Token.Token
	}
	if ctx.Token.Source != "" && ctx.Token.Source != EnvMCPToken {
		updates[EnvMCPTokenFile] = ctx.Token.Source
	}
	normalizeInteractiveTerminalEnv(base, updates)
	return mergeEnv(base, updates)
}

func normalizeInteractiveTerminalEnv(base []string, updates map[string]string) {
	term, ok := envLookup(base, "TERM")
	if !ok || strings.TrimSpace(term) == "" || strings.TrimSpace(term) == "dumb" {
		updates["TERM"] = "xterm-256color"
	}
}

func mergeEnv(base []string, updates map[string]string) []string {
	out := append([]string(nil), base...)
	positions := map[string]int{}
	for i, entry := range out {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			positions[key] = i
		}
	}

	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		entry := key + "=" + updates[key]
		if pos, ok := positions[key]; ok {
			out[pos] = entry
			continue
		}
		positions[key] = len(out)
		out = append(out, entry)
	}
	return out
}

func envLookup(env []string, key string) (string, bool) {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return env[i][len(prefix):], true
		}
	}
	return "", false
}
