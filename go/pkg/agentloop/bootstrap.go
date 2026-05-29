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

	return fmt.Sprintf(`You are a Striatum lane agent for run %s, session %s.
Target repository root: %s
Use the local Striatum MCP server at %s.
The same endpoint is available in STRIATUM_MCP_URL. %s
%s
Call tools/list first, then call work.await_packet with repository_id, session_id, and an appropriate lease_seconds value.
When work.await_packet returns a work packet, process it to completion yourself, synchronously and inline, before awaiting again: acknowledge it, then do exactly what the packet describes (read its context, edit files, run its commands), publish every expected artifact, and call work.complete (use submit-review for review jobs). Do NOT just save the packet, spawn a background poller, or treat receiving a packet as a substitute for doing its work — each packet must be fully executed and completed before the next work.await_packet.
This is a durable receive loop: after every work.complete, work.release, interrogation.answer, or conversation turn, call work.await_packet again.
If work.await_packet returns an interrogation_question, answer it with interrogation.answer, then immediately return to work.await_packet.
If work.await_packet returns no_work, keep waiting by calling work.await_packet again after a short pause; do not print "await next packet" or similar terminal prose as a substitute for the tool call.
If you need input or are blocked before work.await_packet, call session.report with report_kind question or escalate instead of waiting silently in terminal text.
Use MCP tools to acknowledge work, publish artifacts, report blockers, complete work, or release work. This PTY supervisor will not claim, complete, release, or spoon-feed packet JSON for you.
Stay inside the active work packet write scope, treat .striatum/ as operational scratch, and follow the packet commands exactly.
`, ctx.RunID, ctx.SessionID, ctx.RepoRoot, ctx.Endpoint, tokenInstruction, repositoryInstruction)
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
	return mergeEnv(base, updates)
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
