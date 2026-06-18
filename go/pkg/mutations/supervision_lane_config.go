package mutations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/halbritt/striatum/go/pkg/agentloop"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/workflowauthoring"
	"github.com/jackc/pgx/v5"
)

type supervisionStartConfig struct {
	SessionID          string
	RepositoryID       string
	RunID              string
	LaneID             string
	RepoRoot           string
	WorkflowSnapshotID string
	Command            []string
	OriginalCommand    []string
	AgentLoopMode      string
	// AgentLoopAutoPromoted records that the daemon flipped this lane from the
	// default push mode to self-driving because its command is a bare interactive
	// agent CLI that cannot consume stdin-FIFO push packets (#431). Surfaced in the
	// supervisor.starting event + start result so the promotion is legible rather
	// than silent.
	AgentLoopAutoPromoted bool
	Transport             string
	StdinDelivery         string
	RequireTmux           bool
	RunAsUser             string
	// CapabilityToken is the session-BOUND MCP capability token minted for this
	// lane at supervise start (RFC 0096 V2 / #135). It is injected into the lane
	// env as STRIATUM_MCP_TOKEN so the lane authenticates as its OWN session and
	// the cross-session impersonation guard (enforceSessionBinding) bites in live
	// runs. The plaintext lives only in memory → lane env; it is never returned to
	// any RPC caller. Empty only in dev/test paths that do not mint (the lane then
	// gets no injected token, which fails loudly rather than silently inheriting
	// the daemon's shared operator override).
	CapabilityToken string
	// LaunchPathPrefix is the lane's optional path_prefix (#223): absolute
	// directories prepended to the supervised lane PATH so a workflow can launch a
	// lane binary that lives outside the daemon PATH WITHOUT a machine-local
	// wrapper script. It is workflow-authored (lives in the snapshot), so it stays
	// portable and auditable.
	LaunchPathPrefix []string
	// LaunchEnv is the lane's optional command_env (#223): extra non-secret env
	// for the lane process. It cannot set PATH (use path_prefix) or any
	// STRIATUM_-namespaced control var, so it never touches the control plane.
	LaunchEnv map[string]string
}

// adapterName returns the bare CLI adapter name of the lane (e.g. "claude"),
// derived from the RAW lane argv0 (OriginalCommand) rather than the possibly
// agent-loop–wrapped Command — so per-adapter env hardening
// (supervisedAdapterEnvEntries / #101) keys off the real child CLI, not the
// "striatumd -agent-loop" wrapper. Uses the same canonical argv→adapter mapping
// as the agent-loop wiring (agentloop.LaneAdapterName).
func (c supervisionStartConfig) adapterName() string {
	argv := c.OriginalCommand
	if len(argv) == 0 {
		argv = c.Command
	}
	if len(argv) == 0 {
		return ""
	}
	return agentloop.LaneAdapterName(argv[0])
}

func loadSupervisionStartConfig(ctx context.Context, runner db.Runner, repositoryID string, sessionID string) (supervisionStartConfig, error) {
	var config supervisionStartConfig
	config.RepositoryID = repositoryID
	var sessionState string
	err := runner.QueryRow(ctx, `
		SELECT s.session_id, s.run_id, s.lane_id, s.state,
		       r.workflow_snapshot_id, repo.repo_root
		  FROM striatumd.sessions s
		  JOIN striatumd.runs r
		    ON r.repository_id = s.repository_id AND r.run_id = s.run_id
		  JOIN striatumd.repositories repo
		    ON repo.repository_id = s.repository_id
		 WHERE s.repository_id = $1 AND s.session_id = $2`,
		repositoryID, sessionID,
	).Scan(&config.SessionID, &config.RunID, &config.LaneID, &sessionState, &config.WorkflowSnapshotID, &config.RepoRoot)
	if errors.Is(err, pgx.ErrNoRows) {
		return config, rpc.NewError("not_found", "session not found: "+sessionID, nil)
	}
	if err != nil {
		return config, err
	}
	if sessionState != "active" {
		return config, rpc.NewError("invalid_transition", "supervise start requires an active session", nil)
	}
	var workflowRaw any
	if err := runner.QueryRow(ctx, `
		SELECT workflow_json
		  FROM striatumd.workflow_snapshots
		 WHERE repository_id = $1 AND workflow_snapshot_id = $2`,
		repositoryID, config.WorkflowSnapshotID,
	).Scan(&workflowRaw); err != nil {
		return config, err
	}
	workflow := asMap(workflowRaw)
	lane := laneConfig(workflow, config.LaneID)
	// Last line of defense before the subprocess launches. validate/prepare
	// already refuse retired one-shot agent commands, but the lane here comes
	// from a frozen snapshot; refuse here too.
	if err := workflowauthoring.RefuseRetiredOneShotLane(config.LaneID, lane); err != nil {
		return config, rpc.NewError("invalid_transition", err.Error(), nil)
	}
	command, err := commandArray(lane)
	if err != nil {
		return config, err
	}
	config.OriginalCommand = append([]string(nil), command...)
	// #223: first-class lane launch env. Parse the workflow-authored path_prefix
	// and command_env so a lane (e.g. agy) can be launched with a controlled PATH
	// and provider env WITHOUT a machine-local /usr/bin/env wrapper — which would
	// also have changed the adapter identity to "env" and been refused below.
	config.LaunchPathPrefix, err = lanePathPrefix(lane)
	if err != nil {
		return config, err
	}
	config.LaunchEnv, err = laneCommandEnv(lane)
	if err != nil {
		return config, err
	}
	usesAgentLoop, explicit := laneAgentLoopSetting(lane)
	if commandSelfDrivesViaArgv(command) {
		// #431: a bare interactive agent CLI (claude/codex/agy) can ONLY self-drive.
		// In supervised_push mode the daemon hands it a newline-delimited packet on
		// its stdin FIFO and waits for it to ack/heartbeat/complete; but a bare
		// interactive agent reads that packet as conversational input, has no MCP
		// config and no bootstrap prompt, never calls work.await_packet/heartbeat/
		// complete, and dies at the liveness deadline with no artifact. Push mode is
		// only viable for an OS-level FIFO consumer (argv0 sh/bash/python/…, never a
		// bare agent CLI).
		switch {
		case !explicit:
			// agent_loop unset: an authoring slip — e.g. a real command pasted over
			// the `local` parking fixture, or a hand-edited / copied / pre-generate
			// snapshot that never passed through workflowgenerate.defaultAgentLoopLane.
			// Promote it to the self-driving loop (which injects the lane MCP config +
			// bootstrap prompt) — the only mode in which it drives work.*. Mirrors
			// defaultAgentLoopLane so authoring (generate) and launch (supervise start)
			// agree. Mutate the decoded snapshot lane (local to this call) so the
			// downstream transport / stdin-delivery defaults follow the promoted mode
			// (supervisionTransport defaults an agent-loop lane to pty_helper).
			usesAgentLoop = true
			config.AgentLoopAutoPromoted = true
			lane["agent_loop"] = true
		case !usesAgentLoop:
			// agent_loop explicitly false on a command that cannot consume push
			// packets: refuse loudly BEFORE inserting the supervisor row rather than
			// launch a lane that will silently miss the liveness deadline. Symmetric
			// with requireSupportedAgentLoopAdapter (RFC 0111 legibility).
			return config, rpc.NewError("invalid_transition", fmt.Sprintf(
				"supervise start refuses lane %q: command %q is an interactive agent CLI that cannot consume stdin-FIFO push packets, but agent_loop is set false. A bare agent CLI never drives work.await_packet/heartbeat/complete in push mode and will miss the liveness deadline with no artifact. Remove the agent_loop=false override so the lane self-drives, or wrap the command in an explicit stdin-FIFO consumer.",
				config.LaneID, command[0]), nil)
		}
	}
	if usesAgentLoop {
		// #181: an agent-loop lane is driven by the self-driving bootstrap +
		// PTY-submit wiring, which only knows how to deliver the initial prompt
		// to codex, agy, and claude (agentloop.BootstrapDeliveryModeFor). Any
		// other argv0 would be wrapped by selfDrivingAgentLoopCommand and launched
		// as a self-driver that can never receive its bootstrap, so the lane sits
		// idle behind a healthy-looking supervisor. Refuse BEFORE inserting the
		// supervisor row so the operator gets a legible error instead of a wedged
		// lane (RFC 0111). (Auto-promoted lanes always pass: commandSelfDrivesViaArgv
		// is exactly the admitted set.)
		if err := requireSupportedAgentLoopAdapter(command); err != nil {
			return config, err
		}
		config.AgentLoopMode = agentLoopModeSelfDriving
		// RFC 0088: wrap the raw lane command in `striatumd -agent-loop -- …`
		// so the agent-loop executor delivers the bootstrap prompt and submits
		// it over a PTY (interactive lanes), instead of launching the bare
		// command which blocks waiting for input it never receives.
		command, err = selfDrivingAgentLoopCommand(command)
		if err != nil {
			return config, err
		}
	} else {
		// #146: a lane that does NOT use the agent loop is a stdin-FIFO/push
		// consumer (it reads a delivered packet then runs the agent), not a true
		// self-driver that calls work.await_packet. Record the honest push mode so
		// sessionHasSelfDrivingSupervisor — and therefore claim-next — surfaces the
		// supervise_send hint instead of the misleading self_driving self_claim_note
		// ("do not run supervise send"), which sent operators down a dead path.
		config.AgentLoopMode = agentLoopModePush
	}
	// RFC 0088: resolve argv0 against the augmented supervised PATH so a lane
	// binary that lives only in ~/.local/bin (codex, claude, agy) launches even
	// when the daemon's own PATH lacks it. exec.Command resolves argv0 against
	// the launching process's PATH at construction time, before cmd.Env is
	// applied, so setting the child PATH alone is insufficient (the F44
	// path.conf-retirement regression).
	command = resolveSupervisedCommandBinary(command, config.LaunchPathPrefix)
	transport, err := supervisionTransport(lane)
	if err != nil {
		return config, err
	}
	delivery, err := supervisionStdinDelivery(lane, transport)
	if err != nil {
		return config, err
	}
	requireTmux, err := supervisionRequireTmux(lane, transport)
	if err != nil {
		return config, err
	}
	config.Command = command
	config.Transport = transport
	config.StdinDelivery = delivery
	config.RequireTmux = requireTmux
	config.RunAsUser = configuredLaneRunAsUser()
	if config.RunAsUser != "" && runtime.GOOS == "windows" {
		return config, rpc.NewError("invalid_transition", supervisedLaneOSUserEnv+" is not supported on windows", nil)
	}
	return config, nil
}

func laneConfig(workflow map[string]any, laneID string) map[string]any {
	lanes := asMap(workflow["lanes"])
	return asMap(lanes[laneID])
}

func commandArray(lane map[string]any) ([]string, error) {
	raw, ok := lane["command"]
	if !ok {
		return nil, rpc.NewError("invalid_transition", "process lane command must be a non-empty array", nil)
	}
	items := asList(raw)
	if len(items) == 0 {
		return nil, rpc.NewError("invalid_transition", "process lane command must be a non-empty array", nil)
	}
	command := make([]string, 0, len(items))
	for _, item := range items {
		part, ok := item.(string)
		if !ok || part == "" {
			return nil, rpc.NewError("invalid_transition", "process lane command entries must be non-empty strings", nil)
		}
		command = append(command, part)
	}
	if lane["adapter"] != "process" {
		return nil, rpc.NewError("invalid_transition", "supervise start requires a process-adapter lane", nil)
	}
	return command, nil
}

func boolLaneValue(values map[string]any, key string) (bool, bool) {
	value, exists := values[key]
	if !exists {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		if normalized == "true" || normalized == "1" {
			return true, true
		}
		if normalized == "false" || normalized == "0" {
			return false, true
		}
	}
	return false, false
}

// laneUsesAgentLoop reports whether a lane opts into the daemon-owned
// agent-loop PTY session (RFC 0088): the command is wrapped in
// `striatumd -agent-loop -- …` and driven over a PTY with a submitted
// bootstrap prompt. Opt-in via lane `agent_loop: true` or
// `adapter_capabilities.agent_loop: true`; default false preserves the
// raw-launch / one-shot-delivery behavior for existing lanes.
func laneUsesAgentLoop(lane map[string]any) bool {
	value, _ := laneAgentLoopSetting(lane)
	return value
}

// laneAgentLoopSetting returns the lane's explicit agent-loop opt-in/out and
// whether it was set at all. agent_loop may live at the top level or under
// adapter_capabilities; the top level wins. When neither is present the lane has
// expressed no intent (explicit=false), which lets the daemon apply the #431
// bare-agent-CLI self-driving default in loadSupervisionStartConfig.
func laneAgentLoopSetting(lane map[string]any) (value bool, explicit bool) {
	if v, ok := boolLaneValue(lane, "agent_loop"); ok {
		return v, true
	}
	capabilities := asMap(lane["adapter_capabilities"])
	if v, ok := boolLaneValue(capabilities, "agent_loop"); ok {
		return v, true
	}
	return false, false
}

// commandSelfDrivesViaArgv reports whether the lane command is a bare interactive
// agent CLI (claude / codex / agy) that the agent-loop bootstrap drives by passing
// the prompt on argv. This is exactly the set requireSupportedAgentLoopAdapter
// admits, so a lane promoted on this predicate is guaranteed to pass that guard.
// Such a command cannot consume stdin-FIFO push packets, so it is only functional
// in the self-driving agent loop; a shell/python wrapper (argv0 sh/bash/python/…)
// or the `local` parking fixture (sh -c 'cat >/dev/null') falls through to false
// and stays a push consumer. Codex one-shot (`codex exec`) lanes are refused
// upstream by RefuseRetiredOneShotLane, so they never reach this predicate.
func commandSelfDrivesViaArgv(command []string) bool {
	return agentloop.BootstrapDeliveryModeFor(command) == agentloop.BootstrapDeliveryArgv
}

// requireSupportedAgentLoopAdapter refuses an agent-loop lane whose argv0 is not
// a self-driving-capable adapter (codex / agy / claude). The predicate is
// agentloop.BootstrapDeliveryModeFor — the canonical bootstrap-delivery contract,
// pinned by the conformance C0 golden — so this guard cannot drift from the
// agent-loop wiring when an adapter is added or removed. The refusal names the
// offending argv0 and the supported adapters so the operator can fix the lane
// command (#181, RFC 0111 legibility).
func requireSupportedAgentLoopAdapter(command []string) error {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return rpc.NewError("invalid_transition", "agent-loop lane command must be non-empty", nil)
	}
	if agentloop.BootstrapDeliveryModeFor(command) == agentloop.BootstrapDeliveryArgv {
		return nil
	}
	adapter := agentloop.LaneAdapterName(command[0])
	return rpc.NewError("invalid_transition", fmt.Sprintf(
		"supervise start refuses agent-loop lane: adapter %q (argv0 %q) cannot run the self-driving loop; supported agent-loop adapters are codex, agy, claude. Set the lane command to one of those, or drop adapter_capabilities.agent_loop so the lane runs as a stdin-FIFO push consumer.",
		adapter, command[0]), nil)
}

// selfDrivingAgentLoopCommand wraps a raw lane command in the agent-loop
// executor so the daemon-owned PTY session delivers the bootstrap prompt.
func selfDrivingAgentLoopCommand(command []string) ([]string, error) {
	if len(command) == 0 {
		return nil, rpc.NewError("invalid_transition", "self-driving lane command must be non-empty", nil)
	}
	if agentLoopFlagIndex(command) >= 0 {
		return append([]string(nil), command...), nil
	}
	return append([]string{agentLoopExecutable(), "-agent-loop", "--"}, command...), nil
}

// resolveSupervisedCommandBinary rewrites command[0] to an absolute path found
// on the lane's path_prefix (#223) then the augmented supervised PATH, so the
// lane binary resolves regardless of the daemon's own PATH. A no-op when argv0
// is already a path or cannot be resolved (the launch will then surface the
// original not-found error).
func resolveSupervisedCommandBinary(command []string, pathPrefix []string) []string {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return command
	}
	bin := command[0]
	if strings.ContainsRune(bin, os.PathSeparator) {
		return command
	}
	searchDirs := append(append([]string(nil), pathPrefix...), filepath.SplitList(supervisedPath())...)
	for _, dir := range searchDirs {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, bin)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			out := append([]string(nil), command...)
			out[0] = candidate
			return out
		}
	}
	return command
}

// lanePathPrefix parses the optional lane path_prefix (#223): absolute
// directories prepended to the supervised lane PATH. Entries must be non-empty
// absolute paths; the daemon-side existence of a dir is not required so the
// snapshot stays portable.
func lanePathPrefix(lane map[string]any) ([]string, error) {
	raw, ok := lane["path_prefix"]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, rpc.NewError("invalid_transition", "lane path_prefix must be an array of absolute directory strings", nil)
	}
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		dir, ok := item.(string)
		if !ok || strings.TrimSpace(dir) == "" {
			return nil, rpc.NewError("invalid_transition", "lane path_prefix entries must be non-empty strings", nil)
		}
		dir = filepath.Clean(strings.TrimSpace(dir))
		if !filepath.IsAbs(dir) {
			return nil, rpc.NewError("invalid_transition", "lane path_prefix entries must be absolute paths: "+dir, nil)
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out, nil
}

// laneCommandEnv parses the optional lane command_env (#223): extra non-secret
// environment entries for the lane process. It cannot set PATH (use path_prefix)
// or any STRIATUM_-namespaced control var (daemon-owned identity/token/endpoint),
// so a workflow can give a lane provider-specific env without touching the
// control plane or PATH.
func laneCommandEnv(lane map[string]any) (map[string]string, error) {
	raw, ok := lane["command_env"]
	if !ok || raw == nil {
		return nil, nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, rpc.NewError("invalid_transition", "lane command_env must be an object of string values", nil)
	}
	out := make(map[string]string, len(obj))
	for key, value := range obj {
		key = strings.TrimSpace(key)
		if err := validateLaneCommandEnvKey(key); err != nil {
			return nil, err
		}
		text, ok := value.(string)
		if !ok {
			return nil, rpc.NewError("invalid_transition", "lane command_env value for "+key+" must be a string", nil)
		}
		out[key] = text
	}
	return out, nil
}

func validateLaneCommandEnvKey(key string) error {
	if key == "" {
		return rpc.NewError("invalid_transition", "lane command_env keys must be non-empty", nil)
	}
	if key == "PATH" {
		return rpc.NewError("invalid_transition", "lane command_env must not set PATH; use path_prefix instead", nil)
	}
	if strings.HasPrefix(key, "STRIATUM_") {
		return rpc.NewError("invalid_transition", "lane command_env must not set STRIATUM_-namespaced control vars: "+key, nil)
	}
	return nil
}

func agentLoopFlagIndex(command []string) int {
	limit := len(command)
	if limit > 4 {
		limit = 4
	}
	for i := 0; i < limit; i++ {
		if command[i] == "-agent-loop" || command[i] == "--agent-loop" {
			return i
		}
	}
	return -1
}

func agentLoopExecutable() string {
	if override := strings.TrimSpace(os.Getenv("STRIATUM_AGENT_LOOP_BINARY")); override != "" {
		return override
	}
	if executable, err := os.Executable(); err == nil && executable != "" {
		return executable
	}
	return "striatumd"
}

func supervisionTransport(lane map[string]any) (string, error) {
	supervision, err := laneSupervision(lane)
	if err != nil {
		return "", err
	}
	if supervision == nil {
		if laneUsesAgentLoop(lane) {
			return supervisionTransportPTYHelper, nil
		}
		return supervisionTransportPipe, nil
	}
	transport, _ := supervision["transport"].(string)
	if transport == "" {
		if laneUsesAgentLoop(lane) {
			return supervisionTransportPTYHelper, nil
		}
		return supervisionTransportPipe, nil
	}
	if transport == supervisionTransportPipe {
		return supervisionTransportPipe, nil
	}
	if transport == supervisionTransportPTYHelper {
		return supervisionTransportPTYHelper, nil
	}
	return "", rpc.NewError("invalid_transition", "lane supervision.transport must be 'pipe' or 'pty_helper'", nil)
}

func supervisionStdinDelivery(lane map[string]any, transport string) (string, error) {
	supervision, err := laneSupervision(lane)
	if err != nil {
		return "", err
	}
	if supervision == nil {
		return stdinDeliveryPersistentFIFO, nil
	}
	mode, _ := supervision["stdin_delivery"].(string)
	if mode == "" {
		return stdinDeliveryPersistentFIFO, nil
	}
	if mode != stdinDeliveryPersistentFIFO && mode != stdinDeliveryOneShotEOF {
		return "", rpc.NewError("invalid_transition", "lane supervision.stdin_delivery must be 'persistent_fifo' or 'one_shot_eof'", nil)
	}
	if mode == stdinDeliveryOneShotEOF && transport != supervisionTransportPipe {
		return "", rpc.NewError("invalid_transition", "lane supervision.stdin_delivery='one_shot_eof' requires supervision.transport='pipe'", nil)
	}
	return mode, nil
}

func supervisionRequireTmux(lane map[string]any, transport string) (bool, error) {
	supervision, err := laneSupervision(lane)
	if err != nil {
		return false, err
	}
	if supervision == nil {
		return false, nil
	}
	raw, ok := supervision["require_tmux"]
	if !ok {
		return false, nil
	}
	requireTmux, ok := raw.(bool)
	if !ok {
		return false, rpc.NewError("invalid_transition", "lane supervision.require_tmux must be a boolean", nil)
	}
	if requireTmux && transport != supervisionTransportPTYHelper {
		return false, rpc.NewError("invalid_transition", "lane supervision.require_tmux=true requires supervision.transport='pty_helper'", nil)
	}
	return requireTmux, nil
}

func laneSupervision(lane map[string]any) (map[string]any, error) {
	raw, ok := lane["supervision"]
	if !ok || raw == nil {
		return nil, nil
	}
	supervision, ok := raw.(map[string]any)
	if ok {
		return supervision, nil
	}
	return nil, rpc.NewError("invalid_transition", "lane supervision must be an object when provided", nil)
}
