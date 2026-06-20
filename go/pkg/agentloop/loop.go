package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/halbritt/striatum/go/pkg/cli/rpcclient"
)

func Run(socketPath, repoRoot, runID, sessionID string, command []string) error {
	if sessionID == "" || runID == "" || repoRoot == "" {
		return fmt.Errorf("STRIATUM_SESSION_ID, STRIATUM_RUN_ID, and STRIATUM_REPO must be in environment")
	}
	if len(command) == 0 {
		return fmt.Errorf("agent command is required")
	}

	endpoint, err := ResolveMCPEndpoint(repoRoot, os.Environ())
	if err != nil {
		return err
	}
	repositoryID := os.Getenv(EnvRepositoryID)
	token, err := ResolveTokenMaterial(repoRoot, os.Environ())
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := runConfig{
		SocketPath:   socketPath,
		RepoRoot:     repoRoot,
		RepositoryID: repositoryID,
		RunID:        runID,
		SessionID:    sessionID,
		Endpoint:     endpoint,
		Token:        token,
		Command:      command,
		Env:          os.Environ(),
	}
	return runWithIO(ctx, cfg, os.Stdin, os.Stdout, os.Stderr)
}

// RunContext is Run with a caller-supplied context and explicit IO, for
// in-process conformance harnesses (RFC 0109 P3) that drive the real lane CLI
// against a test endpoint instead of the live daemon. Unlike Run it does not
// install a SIGINT/SIGTERM handler or read os.Stdin — the caller's ctx is the
// sole stop signal, so an interactive lane (which never self-exits) is bounded
// by ctx cancellation (exec.CommandContext kills the child on ctx done). The
// MCP endpoint, repository_id, and token are resolved from the environment
// exactly as Run does, so callers set them with t.Setenv before invoking.
func RunContext(ctx context.Context, socketPath, repoRoot, runID, sessionID string, command []string, stdout, stderr io.Writer) error {
	if sessionID == "" || runID == "" || repoRoot == "" {
		return fmt.Errorf("STRIATUM_SESSION_ID, STRIATUM_RUN_ID, and STRIATUM_REPO must be in environment")
	}
	if len(command) == 0 {
		return fmt.Errorf("agent command is required")
	}
	endpoint, err := ResolveMCPEndpoint(repoRoot, os.Environ())
	if err != nil {
		return err
	}
	token, err := ResolveTokenMaterial(repoRoot, os.Environ())
	if err != nil {
		return err
	}
	cfg := runConfig{
		SocketPath:   socketPath,
		RepoRoot:     repoRoot,
		RepositoryID: os.Getenv(EnvRepositoryID),
		RunID:        runID,
		SessionID:    sessionID,
		Endpoint:     endpoint,
		Token:        token,
		Command:      command,
		Env:          os.Environ(),
	}
	return runWithIO(ctx, cfg, nil, stdout, stderr)
}

type runConfig struct {
	SocketPath   string
	RepoRoot     string
	RepositoryID string
	RunID        string
	SessionID    string
	Endpoint     string
	Token        TokenMaterial
	Command      []string
	Env          []string
}

type bootstrapDeliveryMode string

const (
	bootstrapDeliveryPTYSubmit bootstrapDeliveryMode = "pty_submit"
	bootstrapDeliveryArgv      bootstrapDeliveryMode = "argv"
)

const daemonReceiverDefaultLeaseSeconds = 1800

// agentLoopSubmitSequence returns the key-sequence written after the bootstrap
// prompt to submit it to an interactive agent. Defaults to a single carriage
// return (Enter), which submits the input line in the TUIs we drive; override
// via STRIATUM_AGENT_LOOP_SUBMIT_SEQUENCE using Go-style escapes (\r, \n) for
// adapters that need a different submit (e.g. bracketed paste). An explicitly
// empty override disables the submit (for headless adapters that EOF instead).
func agentLoopSubmitSequence() string {
	raw, ok := os.LookupEnv("STRIATUM_AGENT_LOOP_SUBMIT_SEQUENCE")
	if !ok {
		return "\r"
	}
	return decodeSubmitSequence(raw)
}

func decodeSubmitSequence(raw string) string {
	replacer := strings.NewReplacer(`\r`, "\r", `\n`, "\n", `\t`, "\t", `\\`, `\`)
	return replacer.Replace(raw)
}

// resolveTrajectoryLogPath returns the default per-supervisor trajectory log
// path. Returns empty when the supervisor id or repo root are unavailable
// (e.g., agent-loop invoked outside the daemon supervisor for unit testing).
func resolveTrajectoryLogPath(repoRoot, supervisorID string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	supervisorID = strings.TrimSpace(supervisorID)
	if repoRoot == "" || supervisorID == "" {
		return ""
	}
	return filepath.Join(repoRoot, ".striatum", "scratch", supervisorID, "pty.log")
}

// agentLoopSubmitDelay is how long to wait after writing the bootstrap prompt
// before sending the submit key-sequence, so a TUI line editor finishes
// ingesting the multi-line paste. Override via STRIATUM_AGENT_LOOP_SUBMIT_DELAY_MS.
func agentLoopSubmitDelay() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("STRIATUM_AGENT_LOOP_SUBMIT_DELAY_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 750 * time.Millisecond
}

func writePromptThenSubmit(w io.Writer, prompt string, delay time.Duration, submit string) error {
	if _, err := io.WriteString(w, prompt); err != nil {
		return err
	}
	if submit == "" {
		return nil
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	_, err := io.WriteString(w, submit)
	return err
}

func prepareLaneCommandForBootstrap(command []string, repoRoot, endpoint string, token TokenMaterial, prompt string) ([]string, func(), bootstrapDeliveryMode, string, error) {
	laneCommand, cleanupMCP, mcpConfigPath, err := injectLaneMCPConfigWithRewritePath(command, repoRoot, endpoint, token)
	if err != nil {
		return nil, cleanupMCP, "", "", err
	}
	mode := bootstrapDeliveryModeFor(laneCommand)
	if mode == bootstrapDeliveryArgv {
		return appendBootstrapArgv(laneCommand, prompt), cleanupMCP, mode, mcpConfigPath, nil
	}
	return laneCommand, cleanupMCP, mode, mcpConfigPath, nil
}

func bootstrapDeliveryModeFor(command []string) bootstrapDeliveryMode {
	if len(command) == 0 {
		return bootstrapDeliveryPTYSubmit
	}
	switch LaneAdapterName(command[0]) {
	case "codex", "agy", "claude":
		// Codex, agy, and Claude Code accept an initial prompt via argv and submit
		// it themselves. Typing the multi-line bootstrap into their TUI leaves the
		// text buffered in the input editor, even when followed by CR/double-CR.
		// #101: Claude Code v2.1.x regressed into this TUI-buffering behavior (a
		// trailing CR no longer submits the typed bootstrap, and even a manual
		// `tmux send-keys Enter` did not), so the two claude_code lanes sat idle at
		// the prompt while the control surface read healthy. `claude [options]
		// <prompt>` takes the bootstrap as the initial positional prompt and starts
		// an interactive session by default (no --print), which Claude submits
		// itself; the agent-loop receive loop then drives subsequent turns.
		return bootstrapDeliveryArgv
	default:
		return bootstrapDeliveryPTYSubmit
	}
}

// appendBootstrapArgv attaches the bootstrap prompt to a lane command that
// accepts an initial prompt via argv. Codex takes the prompt as a trailing
// positional; agy takes it as the value of --prompt-interactive (-i), after
// which it continues the session as a long-lived interactive agent-loop.
func appendBootstrapArgv(command []string, prompt string) []string {
	out := append([]string(nil), command...)
	if len(command) > 0 && LaneAdapterName(command[0]) == "agy" {
		return append(out, "--prompt-interactive", prompt)
	}
	return append(out, prompt)
}

func runWithIO(ctx context.Context, cfg runConfig, stdin io.Reader, stdout, stderr io.Writer) error {
	log.Printf("Starting Striatum agent PTY for session %s on run %s", cfg.SessionID, cfg.RunID)
	log.Printf("Agent command: %v", cfg.Command)
	log.Printf("MCP endpoint: %s", cfg.Endpoint)

	childEnv := AgentEnvironment(cfg.Env, BootstrapContext{
		SocketPath:   cfg.SocketPath,
		RepoRoot:     cfg.RepoRoot,
		RepositoryID: cfg.RepositoryID,
		RunID:        cfg.RunID,
		SessionID:    cfg.SessionID,
		Endpoint:     cfg.Endpoint,
		Token:        cfg.Token,
	})
	prompt := BuildBootstrapPrompt(BootstrapContext{
		SocketPath:   cfg.SocketPath,
		RepoRoot:     cfg.RepoRoot,
		RepositoryID: cfg.RepositoryID,
		RunID:        cfg.RunID,
		SessionID:    cfg.SessionID,
		Endpoint:     cfg.Endpoint,
		Token:        cfg.Token,
	})

	// RFC 0088 Decision 5: give the lane CLI a striatum MCP server pointed at
	// the live endpoint + token, generated fresh into ephemeral scratch and
	// removed on exit (never persist the rotating port).
	laneCommand, cleanupMCP, bootstrapDelivery, mcpConfigPath, err := prepareLaneCommandForBootstrap(cfg.Command, cfg.RepoRoot, cfg.Endpoint, cfg.Token, prompt)
	if err != nil {
		return fmt.Errorf("agent-loop command preparation: %w", err)
	}
	defer cleanupMCP()

	// #163: a claude lane runs interactively in a PTY with cwd == repo_root, where
	// claude 2.1.x parks on its workspace-trust dialog the first time it sees the
	// repo (and --dangerously-skip-permissions does not bypass it). Pre-accept the
	// trust for this workspace so the lane does not silently wedge before claiming.
	// Best-effort + idempotent; never fail the launch over it.
	if len(cfg.Command) > 0 && LaneAdapterName(cfg.Command[0]) == "claude" {
		if note, terr := ensureClaudeWorkspaceTrusted(cfg.RepoRoot); terr != nil {
			log.Printf("claude workspace-trust pre-seed (#163) best-effort failed: %v", terr)
		} else if note != "" {
			log.Printf("claude workspace-trust (#163): %s", note)
		}
	}

	cmd := exec.CommandContext(ctx, laneCommand[0], laneCommand[1:]...)
	cmd.Dir = cfg.RepoRoot
	cmd.Env = childEnv

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return fmt.Errorf("agent-loop pty start: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	if inputFile, ok := stdin.(*os.File); ok {
		_ = pty.InheritSize(inputFile, ptmx)
	}

	// RFC 0088 P3 follow-up: tee the PTY output to a per-supervisor 0600 file
	// under .striatum/scratch so the operator can `tail -f` the lane trajectory
	// for live inspection or interactive debugging. This is LOCAL-ONLY
	// (operator scratch); D028's "no upstream transcript capture" intent is
	// preserved — nothing is sent to the daemon, MCP, or repo artifacts. Set
	// STRIATUM_AGENT_LOOP_DEBUG_LOG explicitly to override the path (e.g. for
	// debugging from a fixed location); set it to "off" / "/dev/null" to
	// disable.
	sink := stdout
	trajectoryPath := resolveTrajectoryLogPath(cfg.RepoRoot, os.Getenv("STRIATUM_SUPERVISOR_ID"))
	if explicit := strings.TrimSpace(os.Getenv("STRIATUM_AGENT_LOOP_DEBUG_LOG")); explicit != "" {
		switch strings.ToLower(explicit) {
		case "off", "false", "0", "/dev/null":
			trajectoryPath = ""
		default:
			trajectoryPath = explicit
		}
	}
	if trajectoryPath != "" {
		_ = os.MkdirAll(filepath.Dir(trajectoryPath), 0o700)
		if f, ferr := os.OpenFile(trajectoryPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); ferr == nil {
			defer func() { _ = f.Close() }()
			_, _ = fmt.Fprintf(f, "\n===== agent-loop session %s @ %s, command=%v =====\n", cfg.SessionID, cfg.RunID, laneCommand)
			sink = io.MultiWriter(stdout, f)
		}
	}

	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		_ = copyPTYOutputWithTerminalReplies(ptmx, sink, ptmx, laneCommand[0])
	}()

	// Write the bootstrap prompt, then — after a short delay so the TUI line
	// editor finishes ingesting the (multi-line) paste — send the submit
	// key-sequence as a SEPARATE write so an interactive TUI agent actually
	// submits it instead of leaving it buffered in the input line (the RFC 0088
	// / D140 "buffers unsubmitted" blocker). Concatenating the CR to the prompt
	// does not submit: the editor absorbs it into the multi-line input. Headless
	// agents read the prompt as input and the later CR is harmless.
	if bootstrapDelivery == bootstrapDeliveryPTYSubmit {
		if err := writePromptThenSubmit(ptmx, prompt, agentLoopSubmitDelay(), agentLoopSubmitSequence()); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("agent-loop bootstrap submit: %w", err)
		}
	}

	var idleExitRequested atomic.Bool
	startDaemonReceiverLoop(ctx, cfg, laneCommand[0], ptmx, stderr, func() {
		idleExitRequested.Store(true)
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
	})

	// RFC 0140 (part A): keep an honest long-local-work lane out of the #324
	// wedged_no_tool_progress stall by emitting a periodic local_work keepalive
	// from THIS supervised process while it holds an active lease. The keepalive
	// is a work.heartbeat with local_work=true, which the daemon also folds into
	// the tool-progress timeline — so a lane running a full test suite / browser
	// profile / large repo scan (zero tool calls for minutes) never crosses
	// ToolProgressSeconds and keeps its attested byline. It is forgery-resistant
	// (only this live process holds the session-bound token), so a dead agent
	// cannot fire it and recovery still reaps a genuinely dead lane.
	startLocalWorkKeepalive(ctx, cfg, laneCommand[0], stderr)

	// #323: a mid-run daemon restart rotates the MCP HTTP endpoint (dynamic
	// port) and rewrites the runtime endpoint file, but this supervised lane
	// (which survived the restart, #141) still holds the dead launch-time port,
	// losing the only repo_write path for a striatum-lane lane against a
	// halbritt-owned worktree. Watch the runtime endpoint file; when it rotates
	// away from the launch value, rewrite the ephemeral --mcp-config and prompt
	// the CLI to reconnect its striatum MCP server.
	startMCPEndpointRotationWatcher(ctx, cfg, laneCommand[0], mcpConfigPath, ptmx, stderr)

	if stdin != nil {
		go func() {
			_, err := io.Copy(ptmx, stdin)
			if err != nil && !errors.Is(err, os.ErrClosed) {
				_, _ = fmt.Fprintf(stderr, "agent-loop stdin copy failed: %v\n", err)
			}
		}()
	}

	err = cmd.Wait()
	_ = ptmx.Close()
	<-outputDone
	return normalizeAgentExitError(err, idleExitRequested.Load())
}

func normalizeAgentExitError(err error, idleExitRequested bool) error {
	if err != nil {
		if idleExitRequested {
			return nil
		}
		return fmt.Errorf("agent command exited: %w", err)
	}
	return nil
}

func startDaemonReceiverLoop(ctx context.Context, cfg runConfig, adapter string, ptmx io.Writer, stderr io.Writer, requestIdleExit func()) {
	if daemonReceiverDisabled(cfg.Env, adapter) || cfg.RepositoryID == "" || cfg.SessionID == "" {
		return
	}
	clientCfg := rpcclient.Config{
		SocketPath: cfg.SocketPath,
		Token:      cfg.Token.Token,
		DeadlineMS: 45000,
	}
	if cfg.Token.Source != "" && cfg.Token.Source != EnvMCPToken {
		clientCfg.TokenFile = cfg.Token.Source
	}
	client := rpcclient.Client{Config: clientCfg}

	go func() {
		backoff := 2 * time.Second
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			ready, err := daemonReceiverReady(ctx, client, cfg.RepositoryID, cfg.SessionID)
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "agent-loop daemon receiver status failed: %v\n", err)
				sleepOrDone(ctx, backoff)
				continue
			}
			if !ready {
				sleepOrDone(ctx, 5*time.Second)
				continue
			}

			callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			envelope, err := client.Invoke(callCtx, "work.await_packet", map[string]any{
				"repository_id": cfg.RepositoryID,
				"session_id":    cfg.SessionID,
				"lease_seconds": daemonReceiverDefaultLeaseSeconds,
			})
			cancel()
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "agent-loop daemon receiver await failed: %v\n", err)
				sleepOrDone(ctx, backoff)
				continue
			}
			backoff = 2 * time.Second

			if EnvelopeRequestsIdleExit(envelope) {
				_, _ = fmt.Fprintln(stderr, "agent-loop daemon receiver idle: exiting lane after no_work")
				if requestIdleExit != nil {
					requestIdleExit()
				}
				return
			}

			prompt := promptForDaemonEnvelope(envelope)
			if prompt == "" {
				sleepOrDone(ctx, 2*time.Second)
				continue
			}
			if err := writePromptThenSubmit(ptmx, prompt, agentLoopSubmitDelay(), agentLoopSubmitSequence()); err != nil {
				_, _ = fmt.Fprintf(stderr, "agent-loop daemon receiver prompt failed: %v\n", err)
				return
			}
		}
	}()
}

// localWorkKeepaliveInterval is the cadence of the RFC 0140 part-A local_work
// keepalive. It defaults to ToolProgressSeconds/3 (~200s) so a lane is refreshed
// well before it could cross the 600s wedged_no_tool_progress deadline, while not
// hammering the daemon. Override via STRIATUM_AGENT_LOOP_KEEPALIVE_MS (>=0; 0
// disables the keepalive, e.g. for tests or a build that relies solely on the
// server-side liveness-truthful classifier, RFC 0140 part B).
func localWorkKeepaliveInterval() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("STRIATUM_AGENT_LOOP_KEEPALIVE_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 200 * time.Second
}

// activeLeaseIDFromStatus extracts the active lease id from a supervise.status
// envelope's protocol_liveness block. Returns "" when there is no active lease
// (the keepalive only fires for a lease holder — a lane with no claimed work has
// nothing to keep alive). Pure, so it is unit-testable without a live daemon.
func activeLeaseIDFromStatus(status map[string]any) string {
	liveness, _ := status["protocol_liveness"].(map[string]any)
	if liveness == nil {
		return ""
	}
	id := liveness["active_lease_id"]
	if id == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(id))
}

// startLocalWorkKeepalive launches the RFC 0140 part-A keepalive loop. On a
// periodic tick it reads supervise.status; if the session holds an active lease
// it issues a work.heartbeat with local_work=true, which the daemon folds into
// BOTH the lease-heartbeat and the tool-progress timelines — so a lane doing long
// tool-call-less local work never ages into wedged_no_tool_progress and keeps its
// attested byline. No active lease => no keepalive (nothing to keep alive).
//
// Unlike the PTY-side daemon receiver, this stays enabled for Codex lanes: the
// supervised process owns the session token and can safely send lease-scoped
// heartbeats without racing Codex's foreground work.await_packet loop.
// STRIATUM_AGENT_LOOP_KEEPALIVE_MS=0 is the explicit off switch.
func startLocalWorkKeepalive(ctx context.Context, cfg runConfig, _ string, stderr io.Writer) {
	if localWorkKeepaliveDisabled(cfg) {
		return
	}
	interval := localWorkKeepaliveInterval()
	clientCfg := rpcclient.Config{
		SocketPath: cfg.SocketPath,
		Token:      cfg.Token.Token,
		DeadlineMS: 15000,
	}
	if cfg.Token.Source != "" && cfg.Token.Source != EnvMCPToken {
		clientCfg.TokenFile = cfg.Token.Source
	}
	client := rpcclient.Client{Config: clientCfg}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
			statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			status, err := client.Invoke(statusCtx, "supervise.status", map[string]any{
				"repository_id": cfg.RepositoryID,
				"session_id":    cfg.SessionID,
			})
			cancel()
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "agent-loop local_work keepalive status failed: %v\n", err)
				continue
			}
			leaseID := activeLeaseIDFromStatus(status)
			if leaseID == "" {
				continue
			}
			hbCtx, cancelHB := context.WithTimeout(ctx, 15*time.Second)
			_, err = client.Invoke(hbCtx, "work.heartbeat", map[string]any{
				"repository_id": cfg.RepositoryID,
				"session_id":    cfg.SessionID,
				"lease_id":      leaseID,
				"local_work":    true,
			})
			cancelHB()
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "agent-loop local_work keepalive heartbeat failed: %v\n", err)
			}
		}
	}()
}

func localWorkKeepaliveDisabled(cfg runConfig) bool {
	return cfg.RepositoryID == "" || cfg.SessionID == "" || localWorkKeepaliveInterval() <= 0
}

// mcpRotationPollInterval is how often the #323 watcher re-reads the runtime
// MCP endpoint file to detect a mid-run daemon-restart port rotation. Override
// via STRIATUM_AGENT_LOOP_MCP_ROTATION_POLL_MS (>=0; 0 disables the watcher).
func mcpRotationPollInterval() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("STRIATUM_AGENT_LOOP_MCP_ROTATION_POLL_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 10 * time.Second
}

// startMCPEndpointRotationWatcher launches the #323 rotation-recovery loop. On a
// periodic tick it re-resolves the daemon's runtime MCP endpoint+token directly
// from disk (ResolveMCPEndpointFresh, bypassing the cached launch literal). When
// the fresh endpoint DIFFERS from the launch value it (a) rewrites the lane's
// ephemeral --mcp-config file in place with the new endpoint+token, and (b)
// sends an adapter-appropriate reconnect prompt into the PTY so the lane CLI
// re-establishes its striatum MCP server. The daemon-receiver loop itself uses
// the stable unix socket and already survives the restart (#141); this restores
// the CLI's repo_write path.
//
// Deterministic core = the fresh re-resolution + ephemeral-config rewrite. The
// PTY reconnect prompt is best-effort and adapter-gated: only adapters with a
// known reconnect affordance (claude, via /mcp) are prompted; others fall back
// to a no-op (logged), never a crash.
func startMCPEndpointRotationWatcher(ctx context.Context, cfg runConfig, adapter, mcpConfigPath string, ptmx io.Writer, stderr io.Writer) {
	interval := mcpRotationPollInterval()
	if interval <= 0 {
		return
	}
	launchEndpoint := strings.TrimSpace(cfg.Endpoint)
	if launchEndpoint == "" {
		return
	}
	go func() {
		appliedEndpoint := launchEndpoint
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
			fresh, err := ResolveMCPEndpointFresh(cfg.RepoRoot)
			if err != nil {
				// No runtime endpoint file (or unreadable) — nothing to compare
				// against yet; the launch endpoint stays authoritative.
				continue
			}
			fresh = strings.TrimSpace(fresh)
			if fresh == "" || fresh == appliedEndpoint {
				continue
			}
			// The endpoint rotated. Re-read the token too (it normally does not
			// rotate, but re-mint is possible); fall back to the launch token.
			token := cfg.Token
			if freshTok, terr := ResolveTokenMaterialFresh(cfg.RepoRoot); terr == nil && strings.TrimSpace(freshTok.Token) != "" {
				token = freshTok
			}
			if err := applyMCPEndpointRotation(adapter, mcpConfigPath, fresh, token, ptmx, stderr); err != nil {
				_, _ = fmt.Fprintf(stderr, "agent-loop MCP rotation recovery failed (#323): %v\n", err)
				// Do not advance appliedEndpoint: retry on the next tick.
				continue
			}
			_, _ = fmt.Fprintf(stderr, "agent-loop MCP endpoint rotated (#323): %s -> %s; rewrote lane config and prompted reconnect\n", appliedEndpoint, fresh)
			appliedEndpoint = fresh
		}
	}()
}

// applyMCPEndpointRotation rewrites the ephemeral --mcp-config (when present)
// and sends the adapter-appropriate reconnect prompt. The config rewrite is the
// deterministic core; the PTY reconnect prompt is best-effort and adapter-gated.
func applyMCPEndpointRotation(adapter, mcpConfigPath, endpoint string, token TokenMaterial, ptmx io.Writer, stderr io.Writer) error {
	if mcpConfigPath != "" {
		if err := RewriteEphemeralMCPConfig(mcpConfigPath, endpoint, token.Token); err != nil {
			return fmt.Errorf("rewrite ephemeral mcp config: %w", err)
		}
	}
	prompt := mcpReconnectPrompt(adapter, endpoint)
	if prompt == "" {
		// Adapter has no known reconnect affordance — no-op fallback (the config
		// was rewritten if it has an ephemeral file; codex/agy re-read their own
		// config or env, and a future build may hot-reload). Never crash.
		_, _ = fmt.Fprintf(stderr, "agent-loop MCP rotation (#323): adapter %q has no reconnect prompt; relying on config rewrite only\n", LaneAdapterName(adapter))
		return nil
	}
	if err := writePromptThenSubmit(ptmx, prompt, agentLoopSubmitDelay(), agentLoopSubmitSequence()); err != nil {
		return fmt.Errorf("send reconnect prompt: %w", err)
	}
	return nil
}

// mcpReconnectPrompt returns the PTY text that tells an interactive lane CLI to
// re-establish its striatum MCP server after the ephemeral config was rewritten
// to a new endpoint (#323). Only adapters with a known reconnect affordance get
// a prompt; others return "" so the caller falls back to a config-rewrite-only
// no-op rather than typing a command the CLI does not understand.
func mcpReconnectPrompt(adapter, endpoint string) string {
	switch LaneAdapterName(adapter) {
	case "claude":
		// claude exposes `/mcp` to manage MCP servers; after rewriting the
		// --mcp-config file, ask claude to reconnect the striatum server so it
		// picks up the rotated endpoint. The third-party binary may not hot-reload
		// --mcp-config on its own, so this prompt is the reconnect trigger.
		return fmt.Sprintf("\n\nThe Striatum MCP daemon restarted and its endpoint rotated to %s; your --mcp-config was refreshed. Run /mcp and reconnect the \"striatum\" server (or restart it) so MCP tools work again, then resume the active work packet. Do not answer in terminal prose only.\n", endpoint)
	default:
		// agy/codex/others: no reliable PTY reconnect affordance. The config
		// rewrite (claude) / env+config (codex) is the best-effort path; return
		// empty so the caller logs a no-op rather than mistyping into the TUI.
		return ""
	}
}

// EnvelopeRequestsIdleExit reports whether a work.await_packet envelope is the
// terminal-idle signal the agent-loop lane receiver must stop on. It is the
// daemon↔receiver exit contract (RFC 0120 Phase 1): the daemon returns this
// envelope shape for a no-work or terminal-session await, and the receiver
// exits the lane cleanly instead of polling. Exported so the contract can be
// asserted against real daemon output, not only hand-built envelopes — a daemon
// envelope that fails this check would silently leave the receiver error- or
// idle-looping against a finished session.
func EnvelopeRequestsIdleExit(envelope map[string]any) bool {
	if fmt.Sprint(envelope["status"]) != "no_work" {
		return false
	}
	// Fail closed: any non-empty idle_behavior on a no_work envelope is an
	// exit instruction, even when the value is unrecognized — an unknown
	// future value must not silently fall back to the resident repoll loop.
	// Only an absent idle_behavior (an older daemon) keeps the legacy polling
	// behavior.
	behavior, ok := envelope["idle_behavior"]
	if !ok || behavior == nil {
		return false
	}
	return fmt.Sprint(behavior) != ""
}

func daemonReceiverReady(ctx context.Context, client rpcclient.Client, repositoryID, sessionID string) (bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, err := client.Invoke(callCtx, "supervise.status", map[string]any{
		"repository_id": repositoryID,
		"session_id":    sessionID,
	})
	if err != nil {
		return false, err
	}
	liveness, _ := status["protocol_liveness"].(map[string]any)
	if fmt.Sprint(liveness["active_lease_id"]) != "" && liveness["active_lease_id"] != nil {
		return false, nil
	}
	return liveness["last_work_complete_at"] != nil ||
		liveness["last_work_release_at"] != nil ||
		liveness["last_work_block_at"] != nil, nil
}

func daemonReceiverDisabled(env []string, adapter string) bool {
	value, ok := envLookup(env, "STRIATUM_AGENT_LOOP_DAEMON_RECEIVER")
	if ok {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "0", "false", "off", "disabled":
			return true
		default:
			return false
		}
	}
	// Codex already runs the Striatum MCP receive loop from its bootstrap prompt.
	// Letting the PTY-side daemon receiver call work.await_packet races that loop:
	// it can pre-claim the next packet under the same session and leave codex's
	// own await call seeing no_work.
	return LaneAdapterName(adapter) == "codex"
}

func promptForDaemonEnvelope(envelope map[string]any) string {
	switch fmt.Sprint(envelope["type"]) {
	case "interrogation_question":
		return fmt.Sprintf("\n\nStriatum delivered an interrogation_question for this session.\nInterrogation ID: %s\nMessage ID: %s\n\nQuestion:\n%s\n\nAnswer with the interrogation.answer tool. After answering, wait for the next item; do not answer in terminal prose only.\n",
			envelope["interrogation_id"],
			envelope["message_id"],
			envelope["body"],
		)
	case "conversation_message":
		body, _ := json.MarshalIndent(envelope, "", "  ")
		return "\n\nStriatum delivered a conversation turn for this session. Respond using the conversation tool required by this envelope; do not answer in terminal prose only.\n\n```json\n" + string(body) + "\n```\n"
	case "work_packet":
		body, _ := json.MarshalIndent(envelope["packet"], "", "  ")
		return "\n\nStriatum delivered a work packet for this session. Follow the packet commands exactly: ack first, then complete, release, block, publish artifacts, or record verdict through Striatum tools as appropriate.\n\n```json\n" + string(body) + "\n```\n"
	default:
		return ""
	}
}

func sleepOrDone(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
