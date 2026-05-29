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

func prepareLaneCommandForBootstrap(command []string, repoRoot, endpoint string, token TokenMaterial, prompt string) ([]string, func(), bootstrapDeliveryMode, error) {
	laneCommand, cleanupMCP, err := injectLaneMCPConfig(command, repoRoot, endpoint, token)
	if err != nil {
		return nil, cleanupMCP, "", err
	}
	mode := bootstrapDeliveryModeFor(laneCommand)
	if mode == bootstrapDeliveryArgv {
		return appendBootstrapArgv(laneCommand, prompt), cleanupMCP, mode, nil
	}
	return laneCommand, cleanupMCP, mode, nil
}

func bootstrapDeliveryModeFor(command []string) bootstrapDeliveryMode {
	if len(command) == 0 {
		return bootstrapDeliveryPTYSubmit
	}
	switch laneAdapterName(command[0]) {
	case "codex", "agy":
		// Codex and agy accept an initial prompt via argv and submit it
		// themselves. Typing the multi-line bootstrap into their TUI leaves the
		// text buffered in the input editor, even when followed by CR/double-CR.
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
	if len(command) > 0 && laneAdapterName(command[0]) == "agy" {
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
	laneCommand, cleanupMCP, bootstrapDelivery, err := prepareLaneCommandForBootstrap(cfg.Command, cfg.RepoRoot, cfg.Endpoint, cfg.Token, prompt)
	if err != nil {
		return fmt.Errorf("agent-loop command preparation: %w", err)
	}
	defer cleanupMCP()

	cmd := exec.CommandContext(ctx, laneCommand[0], laneCommand[1:]...)
	cmd.Dir = cfg.RepoRoot
	cmd.Env = childEnv

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("agent-loop pty start: %w", err)
	}
	defer ptmx.Close()

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
	var sink io.Writer = stdout
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
			defer f.Close()
			fmt.Fprintf(f, "\n===== agent-loop session %s @ %s, command=%v =====\n", cfg.SessionID, cfg.RunID, laneCommand)
			sink = io.MultiWriter(stdout, f)
		}
	}

	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		_, _ = io.Copy(sink, ptmx)
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

	startDaemonReceiverLoop(ctx, cfg, ptmx, stderr)

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
	if err != nil {
		return fmt.Errorf("agent command exited: %w", err)
	}
	return nil
}

func startDaemonReceiverLoop(ctx context.Context, cfg runConfig, ptmx io.Writer, stderr io.Writer) {
	if daemonReceiverDisabled(cfg.Env) || cfg.RepositoryID == "" || cfg.SessionID == "" {
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

func daemonReceiverDisabled(env []string) bool {
	value, ok := envLookup(env, "STRIATUM_AGENT_LOOP_DAEMON_RECEIVER")
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "off", "disabled":
		return true
	default:
		return false
	}
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
