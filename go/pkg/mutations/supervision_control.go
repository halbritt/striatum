package mutations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
	"github.com/jackc/pgx/v5"
)

const (
	supervisionTransportPipe      = "pipe"
	supervisionTransportPTYHelper = "pty_helper"

	stdinDeliveryPersistentFIFO = "persistent_fifo"
	stdinDeliveryOneShotEOF     = "one_shot_eof"

	agentLoopModeSelfDriving = "self_driving"
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
	Transport          string
	StdinDelivery      string
	RequireTmux        bool
}

type supervisorControlRow struct {
	SupervisorID       string
	RunID              string
	SessionID          string
	State              string
	StdinPipePath      string
	PID                int
	HasPID             bool
	PIDStartTime       string
	DaemonSupervisorID string
	Metadata           map[string]any
}

type supervisionPacketRow struct {
	PacketID  string
	RunID     string
	JobID     string
	LeaseID   string
	SessionID string
	Packet    map[string]any
}

type supervisionLaunchResult struct {
	PID                 int
	PIDStartTime        string
	HelperPID           int
	HelperPIDStartTime  string
	Metadata            map[string]any
	InitialHelperEvents []map[string]any
	InitialHelperOffset int
}

var (
	supervisionMkfifo = func(path string) error {
		return syscall.Mkfifo(path, 0o600)
	}
	supervisionLaunch         = launchSupervisedProcess
	supervisionWrite          = writeSupervisorPayload
	supervisionTmuxRunner     = gosupervisor.DefaultTmuxRunner()
	errSupervisorPipeNoReader = errors.New("supervisor pipe has no reader")
)

type supervisorPipeNoReaderDeliveryError struct {
	supervisorID string
	metadata     map[string]any
	reason       string
}

func (e *supervisorPipeNoReaderDeliveryError) Error() string {
	return "supervisor delivery is degraded: " + e.reason
}

func (e *supervisorPipeNoReaderDeliveryError) Unwrap() error {
	return errSupervisorPipeNoReader
}

func HandleSuperviseStart(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredControlTextParam(envelope, "session_id", "supervise.start requires session_id")
	if err != nil {
		return nil, err
	}
	config, err := loadSupervisionStartConfig(ctx, runner, repositoryID, sessionID)
	if err != nil {
		return nil, err
	}
	supervisorID, err := newID("sup")
	if err != nil {
		return nil, err
	}
	daemonSupervisorID, err := newID("dsup")
	if err != nil {
		return nil, err
	}
	scratch := filepath.Join(config.RepoRoot, ".striatum", "scratch", supervisorID)
	pipePath := filepath.Join(scratch, "stdin.pipe")
	eventPath := filepath.Join(scratch, "helper-events.jsonl")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		return nil, err
	}
	_ = os.Remove(pipePath)
	if err := supervisionMkfifo(pipePath); err != nil {
		return nil, err
	}
	cleanupPipe := true
	defer func() {
		if cleanupPipe {
			_ = os.Remove(pipePath)
		}
	}()

	startedAt := nowString()
	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		if err := lockSuperviseStart(ctx, tx, repositoryID, sessionID); err != nil {
			return nil, err
		}
		if err := ensureNoActiveSupervisor(ctx, tx, repositoryID, sessionID); err != nil {
			return nil, err
		}
		if err := insertStartingSupervisorRows(ctx, tx, repositoryID, config, supervisorID, daemonSupervisorID, scratch, pipePath, eventPath, startedAt); err != nil {
			return nil, err
		}
		payload := map[string]any{
			"supervisor_id":        supervisorID,
			"daemon_supervisor_id": daemonSupervisorID,
			"adapter":              "process",
			"transport":            config.Transport,
			"stdin_delivery":       config.StdinDelivery,
			"require_tmux":         config.RequireTmux,
			"agent_loop_mode":      config.AgentLoopMode,
			"stdin_pipe_path":      pipePath,
		}
		if config.Transport == supervisionTransportPTYHelper {
			payload["helper_events_path"] = eventPath
		}
		_, err := appendEvent(ctx, tx, repositoryID, config.RunID, "supervisor.starting", sessionID, nil, nil, nil, nil, payload)
		return nil, err
	}); err != nil {
		return nil, err
	}

	launch, err := supervisionLaunch(ctx, config, supervisorID, scratch, pipePath, eventPath)
	if err != nil {
		_ = markSupervisorLost(ctx, runner, repositoryID, supervisorID, config.RunID, sessionID, "start failed: "+err.Error(), 0, map[string]any{"phase": "start", "error": err.Error()})
		return nil, rpc.NewError("invalid_transition", "supervisor could not launch lane command: "+err.Error(), nil)
	}
	if launch.PIDStartTime == "" {
		launch.PIDStartTime, _ = processStartToken(launch.PID)
	}
	if launch.PIDStartTime == "" {
		launch.PIDStartTime = tmuxPaneStartTokenFromMetadata(launch.Metadata)
	}
	if !pidAliveLocal(launch.PID) {
		_ = markSupervisorLost(ctx, runner, repositoryID, supervisorID, config.RunID, sessionID, "child exited before attach", launch.PID, map[string]any{"phase": "start"})
		return nil, rpc.NewError("invalid_transition", "supervisor child exited before it could be attached", nil)
	}

	attachedAt := nowString()
	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		if len(launch.InitialHelperEvents) > 0 {
			for _, event := range launch.InitialHelperEvents {
				normalized, normErr := normalizeSuperviseReportEvent(event, "", supervisorID, 0)
				if normErr != nil {
					return nil, normErr
				}
				if _, recErr := recordSuperviseReportEvent(ctx, tx, repositoryID, normalized); recErr != nil {
					return nil, recErr
				}
			}
		}
		if len(launch.Metadata) > 0 || launch.InitialHelperOffset > 0 {
			metadata := copyMap(launch.Metadata)
			if launch.InitialHelperOffset > 0 {
				metadata["helper_events_offset"] = launch.InitialHelperOffset
			}
			if err := mergePointerMetadata(ctx, tx, repositoryID, supervisorID, metadata); err != nil {
				return nil, err
			}
		}
		if err := updateSupervisorState(ctx, tx, repositoryID, supervisorID, daemonSupervisorID, "attached", attachedAt, launch.PID, launch.PIDStartTime, attachedAt, nil, nil); err != nil {
			return nil, err
		}
		payload := map[string]any{
			"supervisor_id":        supervisorID,
			"daemon_supervisor_id": daemonSupervisorID,
			"pid":                  launch.PID,
			"transport":            config.Transport,
			"stdin_delivery":       config.StdinDelivery,
			"require_tmux":         config.RequireTmux,
			"agent_loop_mode":      config.AgentLoopMode,
			"stdin_pipe_path":      pipePath,
		}
		if config.Transport == supervisionTransportPTYHelper {
			payload["helper_pid"] = optionalPositiveInt(launch.HelperPID)
			payload["helper_events_path"] = eventPath
		}
		if tmux := objectOrNil(launch.Metadata["tmux"]); tmux != nil {
			payload["tmux"] = tmux
		}
		_, err := appendEvent(ctx, tx, repositoryID, config.RunID, "supervisor.started", sessionID, nil, nil, nil, nil, payload)
		return nil, err
	}); err != nil {
		return nil, err
	}
	cleanupPipe = false
	return map[string]any{
		"supervisor_id":        supervisorID,
		"daemon_supervisor_id": daemonSupervisorID,
		"session_id":           sessionID,
		"run_id":               config.RunID,
		"pid":                  launch.PID,
		"pid_start_time":       nullableString(launch.PIDStartTime),
		"stdin_pipe_path":      pipePath,
		"state":                "attached",
		"transport":            config.Transport,
		"stdin_delivery":       config.StdinDelivery,
		"require_tmux":         config.RequireTmux,
		"agent_loop_mode":      config.AgentLoopMode,
		"helper_process":       helperProcessPayload(config.Transport, launch.HelperPID, launch.HelperPIDStartTime, eventPath),
		"lane_attestation":     laneAttestation(launch.PIDStartTime),
		"lane_id":              config.LaneID,
		"tmux":                 objectOrNil(launch.Metadata["tmux"]),
	}, nil
}

func HandleSuperviseSend(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredControlTextParam(envelope, "session_id", "supervise.send requires session_id")
	if err != nil {
		return nil, err
	}
	packetID, err := requiredControlTextParam(envelope, "packet_id", "supervise.send requires packet_id")
	if err != nil {
		return nil, err
	}

	result, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		supervisor, err := requireActiveControlSupervisor(ctx, tx, repositoryID, sessionID, true)
		if err != nil {
			return nil, err
		}
		if err := drainHelperEvents(ctx, tx, repositoryID, supervisor.SupervisorID, 0); err != nil {
			return nil, err
		}
		supervisor, err = requireActiveControlSupervisor(ctx, tx, repositoryID, sessionID, true)
		if err != nil {
			return nil, err
		}
		if supervisor.State != "attached" {
			message := fmt.Sprintf("supervise send requires an attached supervisor (supervisor_id=%s, state=%s)", supervisor.SupervisorID, supervisor.State)
			if supervisor.State == "detached" {
				message = fmt.Sprintf("supervisor is detached; stop this supervisor and restart/reclaim before delivery (supervisor_id=%s)", supervisor.SupervisorID)
			}
			return nil, rpc.NewError("invalid_transition", message, nil)
		}
		packet, err := loadWorkPacket(ctx, tx, repositoryID, packetID)
		if err != nil {
			return nil, err
		}
		if packet.SessionID != sessionID {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("work packet does not belong to this session: packet_session=%q, requested_session=%q", packet.SessionID, sessionID), nil)
		}
		if packet.RunID != supervisor.RunID {
			return nil, rpc.NewError("invalid_transition", "work packet run does not match supervisor run", nil)
		}
		if err := ensureActivePacketLease(ctx, tx, repositoryID, packet, sessionID); err != nil {
			return nil, err
		}
		if err := reconcileSupervisorForDelivery(ctx, tx, repositoryID, supervisor, "supervise.send"); err != nil {
			return nil, err
		}
		if supervisor.StdinPipePath == "" {
			return nil, rpc.NewError("invalid_transition", "supervisor stdin pipe is missing: <unset>", nil)
		}
		if _, err := os.Stat(supervisor.StdinPipePath); err != nil {
			return nil, rpc.NewError("invalid_transition", "supervisor stdin pipe is missing: "+supervisor.StdinPipePath, nil)
		}
		payload, err := json.Marshal(packet.Packet)
		if err != nil {
			return nil, err
		}
		payload = append(payload, '\n')
		delivery, err := supervisionWrite(ctx, tx, repositoryID, supervisor.SupervisorID, supervisor.StdinPipePath, payload)
		if err != nil {
			return nil, err
		}
		deliveredAt := nowString()
		if err := refreshSupervisorHeartbeat(ctx, tx, repositoryID, supervisor.SupervisorID, supervisor.DaemonSupervisorID, deliveredAt); err != nil {
			return nil, err
		}
		if err := drainHelperEvents(ctx, tx, repositoryID, supervisor.SupervisorID, 250*time.Millisecond); err != nil {
			return nil, err
		}
		_, err = appendEvent(ctx, tx, repositoryID, supervisor.RunID, "supervisor.packet_delivered", sessionID, nil, nil, nil, nil, map[string]any{
			"supervisor_id":            supervisor.SupervisorID,
			"packet_id":                packetID,
			"bytes_written":            delivery.BytesWritten,
			"stdin_delivery":           delivery.StdinDelivery,
			"stdin_closed_after_write": delivery.StdinClosedAfterWrite,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"supervisor_id":            supervisor.SupervisorID,
			"packet_id":                packetID,
			"delivered_at":             deliveredAt,
			"bytes":                    delivery.BytesWritten,
			"stdin_delivery":           delivery.StdinDelivery,
			"stdin_closed_after_write": delivery.StdinClosedAfterWrite,
			"delivery_state":           "delivered_unacknowledged",
			"control_ack_expected":     true,
		}, nil
	})
	if err == nil {
		return result, nil
	}
	var noReader *supervisorPipeNoReaderDeliveryError
	if !errors.As(err, &noReader) {
		return nil, err
	}
	if _, markErr := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		return map[string]any{}, markPointerDeliveryDegraded(ctx, tx, repositoryID, noReader.supervisorID, noReader.metadata, noReader.reason)
	}); markErr != nil {
		return nil, markErr
	}
	return nil, rpc.NewError("invalid_transition", noReader.Error(), nil)
}

func HandleSuperviseStop(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredControlTextParam(envelope, "session_id", "supervise.stop requires session_id")
	if err != nil {
		return nil, err
	}
	reason, err := requiredControlTextParam(envelope, "reason", "supervise.stop requires reason")
	if err != nil {
		return nil, err
	}
	if err := requireSessionExists(ctx, runner, repositoryID, sessionID); err != nil {
		return nil, err
	}
	terminal, err := latestTerminalSupervisor(ctx, runner, repositoryID, sessionID)
	if err != nil {
		return nil, err
	}
	if terminal != nil {
		return map[string]any{
			"supervisor_id": terminal.SupervisorID,
			"session_id":    sessionID,
			"pid":           optionalIntValue(terminal.PID, terminal.HasPID),
			"state":         "stopped",
			"ended_at":      terminal.Metadata["ended_at"],
			"stop_reason":   terminal.Metadata["stop_reason"],
			"signal":        nil,
			"note":          "supervisor was already " + terminal.State,
		}, nil
	}

	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		supervisor, err := requireActiveControlSupervisor(ctx, tx, repositoryID, sessionID, true)
		if err != nil {
			return nil, err
		}
		_ = drainHelperEvents(ctx, tx, repositoryID, supervisor.SupervisorID, 0)
		var signaled any
		eventExtra := map[string]any{}
		stopNote := any(nil)
		if tmuxIdentity, ok := gosupervisor.TmuxIdentityFromMetadata(supervisor.Metadata); ok {
			signal, note, fallbackReason, cleanupSkip := stopTmuxBackedLane(ctx, tmuxIdentity, supervisor.PID, supervisor.PIDStartTime)
			signaled = signal
			if note != "" {
				stopNote = note
			}
			if fallbackReason != "" {
				eventExtra["tmux_kill_fallback_reason"] = fallbackReason
			}
			if cleanupSkip != "" {
				eventExtra["pane_pid_cleanup_skipped_reason"] = cleanupSkip
			}
		} else if supervisor.HasPID {
			signal, cleanupSkip := terminateProcessWithStartToken(supervisor.PID, supervisor.PIDStartTime)
			signaled = signal
			if cleanupSkip != "" {
				eventExtra["pid_cleanup_skipped_reason"] = cleanupSkip
			}
		}
		if helperPID, ok := intValueOptional(supervisor.Metadata["helper_pid"]); ok && (!supervisor.HasPID || helperPID != supervisor.PID) {
			helperSignal, cleanupSkip := terminateProcessWithStartToken(helperPID, metadataString(supervisor.Metadata["helper_pid_start_time"]))
			if helperSignal != nil {
				eventExtra["helper_signal"] = helperSignal
			}
			if cleanupSkip != "" {
				eventExtra["helper_pid_cleanup_skipped_reason"] = cleanupSkip
			}
		}
		if supervisor.StdinPipePath != "" {
			_ = os.Remove(supervisor.StdinPipePath)
		}
		endedAt := nowString()
		if err := updateSupervisorState(ctx, tx, repositoryID, supervisor.SupervisorID, supervisor.DaemonSupervisorID, "stopped", endedAt, 0, "", "", &endedAt, &reason); err != nil {
			return nil, err
		}
		// #50: a stopped supervisor must not leave its session reading as
		// `active` — that pollutes "find the latest active <role>/<lane> session"
		// lookups (interrogation targeting, reviewer prompts). Close the session
		// in one guarded UPDATE: only when it is still `active` AND holds no
		// active lease (mid-work sessions are left for explicit recovery). Done
		// as a single conditional statement so no extra row read is required.
		if err := tx.Exec(ctx, `
			UPDATE striatumd.sessions
			   SET state = 'closed', closed_at = $1, close_reason = $2
			 WHERE repository_id = $3 AND session_id = $4 AND state = 'active'
			   AND NOT EXISTS (
				 SELECT 1 FROM striatumd.leases l
				  WHERE l.repository_id = $3 AND l.owner_session_id = $4 AND l.state = 'active')`,
			endedAt, "supervisor stopped: "+reason, repositoryID, sessionID); err != nil {
			return nil, err
		}
		eventPayload := map[string]any{
			"supervisor_id":        supervisor.SupervisorID,
			"daemon_supervisor_id": nullableString(supervisor.DaemonSupervisorID),
			"pid":                  optionalIntValue(supervisor.PID, supervisor.HasPID),
			"reason":               reason,
			"signal":               signaled,
		}
		for key, value := range eventExtra {
			eventPayload[key] = value
		}
		_, err = appendEvent(ctx, tx, repositoryID, supervisor.RunID, "supervisor.stopped", sessionID, nil, nil, nil, nil, eventPayload)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"supervisor_id":        supervisor.SupervisorID,
			"daemon_supervisor_id": nullableString(supervisor.DaemonSupervisorID),
			"session_id":           sessionID,
			"pid":                  optionalIntValue(supervisor.PID, supervisor.HasPID),
			"state":                "stopped",
			"ended_at":             endedAt,
			"stop_reason":          reason,
			"signal":               signaled,
			"note":                 stopNote,
		}, nil
	})
}

func stopTmuxBackedLane(ctx context.Context, identity gosupervisor.TmuxIdentity, panePID int, paneStartToken string) (signal any, note string, fallbackReason string, cleanupSkip string) {
	if strings.TrimSpace(identity.SessionName) == "" {
		if panePID > 0 {
			signal, cleanupSkip = terminateProcessWithStartToken(panePID, paneStartToken)
			return signal, "", "tmux_session_missing", cleanupSkip
		}
		return nil, "tmux_session_missing", "", ""
	}
	_, err := supervisionTmuxRunner.Run(ctx, "kill-session", "-t", identity.SessionName)
	if err == nil || tmuxSessionAlreadyGone(err) {
		if err != nil {
			note = string(gosupervisor.TmuxLivenessSessionMissing)
		}
		return "tmux_kill_session", note, "", ""
	}
	if panePID > 0 {
		signal, cleanupSkip = terminateProcessWithStartToken(panePID, paneStartToken)
		return signal, "", string(gosupervisor.TmuxLivenessUnavailable), cleanupSkip
	}
	return nil, "", string(gosupervisor.TmuxLivenessUnavailable), ""
}

func tmuxSessionAlreadyGone(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "can't find session") ||
		strings.Contains(text, "can't find window") ||
		strings.Contains(text, "no server running") ||
		strings.Contains(text, "session not found")
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
	command, err := commandArray(lane)
	if err != nil {
		return config, err
	}
	config.OriginalCommand = append([]string(nil), command...)
	config.AgentLoopMode = agentLoopModeSelfDriving
	if laneUsesAgentLoop(lane) {
		// RFC 0088: wrap the raw lane command in `striatumd -agent-loop -- …`
		// so the agent-loop executor delivers the bootstrap prompt and submits
		// it over a PTY (interactive lanes), instead of launching the bare
		// command which blocks waiting for input it never receives.
		command, err = selfDrivingAgentLoopCommand(command)
		if err != nil {
			return config, err
		}
	}
	// RFC 0088: resolve argv0 against the augmented supervised PATH so a lane
	// binary that lives only in ~/.local/bin (codex, claude, agy) launches even
	// when the daemon's own PATH lacks it. exec.Command resolves argv0 against
	// the launching process's PATH at construction time, before cmd.Env is
	// applied, so setting the child PATH alone is insufficient (the F44
	// path.conf-retirement regression).
	command = resolveSupervisedCommandBinary(command)
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
	if err := ensureNoActiveSupervisor(ctx, runner, repositoryID, sessionID); err != nil {
		return config, err
	}
	config.Command = command
	config.Transport = transport
	config.StdinDelivery = delivery
	config.RequireTmux = requireTmux
	return config, nil
}

func insertStartingSupervisorRows(ctx context.Context, runner db.TxRunner, repositoryID string, config supervisionStartConfig, supervisorID, daemonSupervisorID, scratch, pipePath, eventPath, startedAt string) error {
	commandJSON, err := json.Marshal(config.Command)
	if err != nil {
		return err
	}
	metadata := map[string]any{
		"source":             "go_supervision_control_handler",
		"daemon_instance_id": currentDaemonInstanceID(),
		"transport":          config.Transport,
		"stdin_delivery":     config.StdinDelivery,
		"require_tmux":       config.RequireTmux,
		"agent_loop_mode":    config.AgentLoopMode,
	}
	if config.Transport == supervisionTransportPTYHelper {
		metadata["helper_events_path"] = eventPath
		metadata["helper_events_offset"] = 0
	}
	commandArg, err := db.JSONBArg(runner, config.Command)
	if err != nil {
		return err
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.process_supervisors (
		  repository_id, supervisor_id, run_id, session_id, adapter,
		  command_json, cwd, scratch_path, stdin_pipe_path, state, started_at
		)
		VALUES ($1,$2,$3,$4,'process',$5::jsonb,$6,$7,$8,'starting',$9)`,
		repositoryID, supervisorID, config.RunID, config.SessionID, commandArg, config.RepoRoot, scratch, pipePath, startedAt,
	); err != nil {
		return err
	}
	metadataArg, err := db.JSONBArg(runner, metadata)
	if err != nil {
		return err
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.process_supervisor_pointers (
		  repository_id, supervisor_id, daemon_supervisor_id, run_id,
		  session_id, state, updated_at, metadata_json
		)
		VALUES ($1,$2,$3,$4,$5,'starting',$6,$7::jsonb)`,
		repositoryID, supervisorID, daemonSupervisorID, config.RunID, config.SessionID, startedAt, metadataArg,
	); err != nil {
		return err
	}
	return runner.Exec(ctx, `
		INSERT INTO striatumd.daemon_supervisors (
		  daemon_supervisor_id, repository_id, run_id, session_id,
		  repo_supervisor_id, daemon_instance_id, adapter, command_json,
		  command_sha256, cwd, stdin_pipe_path, state, started_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,'process',$7::jsonb,$8,$9,$10,'starting',$11)`,
		daemonSupervisorID, repositoryID, config.RunID, config.SessionID,
		supervisorID, currentDaemonInstanceID(), commandArg, sha256Hex(commandJSON),
		config.RepoRoot, pipePath, startedAt,
	)
}

func lockSuperviseStart(ctx context.Context, runner db.TxRunner, repositoryID, sessionID string) error {
	key := "striatum:supervise_start:" + repositoryID + ":" + sessionID
	return runner.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key)
}

func ensureNoActiveSupervisor(ctx context.Context, runner any, repositoryID, sessionID string) error {
	rower, ok := runner.(interface {
		QueryRow(context.Context, string, ...any) db.Row
	})
	if !ok {
		return fmt.Errorf("runner does not support query row")
	}
	var supervisorID, state string
	err := rower.QueryRow(ctx, `
		SELECT supervisor_id, state
		  FROM striatumd.process_supervisors
		 WHERE repository_id = $1 AND session_id = $2
		   AND state = ANY($3)
		 ORDER BY started_at DESC, supervisor_id DESC
		 LIMIT 1`,
		repositoryID, sessionID, []string{"starting", "attached", "detached"},
	).Scan(&supervisorID, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return rpc.NewError("invalid_transition", fmt.Sprintf("session already has an active supervisor: %s (state=%s)", supervisorID, state), nil)
}

func requireActiveControlSupervisor(ctx context.Context, runner any, repositoryID, sessionID string, forUpdate bool) (supervisorControlRow, error) {
	suffix := ""
	if forUpdate {
		suffix = " FOR UPDATE OF ps"
	}
	sql := `
		SELECT ps.supervisor_id, ps.run_id, ps.session_id, ps.state,
		       COALESCE(ps.stdin_pipe_path, ''), ps.pid, COALESCE(ps.pid_start_time, ''),
		       COALESCE(p.daemon_supervisor_id, ''), COALESCE(p.metadata_json, '{}'::jsonb)
		  FROM striatumd.process_supervisors ps
		  LEFT JOIN striatumd.process_supervisor_pointers p
		    ON p.repository_id = ps.repository_id AND p.supervisor_id = ps.supervisor_id
		 WHERE ps.repository_id = $1 AND ps.session_id = $2
		   AND ps.state = ANY($3)
		 ORDER BY ps.started_at DESC, ps.supervisor_id DESC
		 LIMIT 1` + suffix
	rower, ok := runner.(interface {
		QueryRow(context.Context, string, ...any) db.Row
	})
	if !ok {
		return supervisorControlRow{}, fmt.Errorf("runner does not support query row")
	}
	var row supervisorControlRow
	var pid *int
	var metadata any
	err := rower.QueryRow(ctx, sql, repositoryID, sessionID, []string{"starting", "attached", "detached"}).Scan(
		&row.SupervisorID, &row.RunID, &row.SessionID, &row.State,
		&row.StdinPipePath, &pid, &row.PIDStartTime,
		&row.DaemonSupervisorID, &metadata,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, rpc.NewError("invalid_transition", fmt.Sprintf("no active supervisor for session_id=%q", sessionID), nil)
	}
	if err != nil {
		return row, err
	}
	if pid != nil {
		row.PID = *pid
		row.HasPID = true
	}
	row.Metadata = asMap(metadata)
	return row, nil
}

func latestTerminalSupervisor(ctx context.Context, runner any, repositoryID, sessionID string) (*supervisorControlRow, error) {
	if _, err := requireActiveControlSupervisor(ctx, runner, repositoryID, sessionID, false); err == nil {
		return nil, nil
	} else {
		var rpcErr *rpc.Error
		if !errors.As(err, &rpcErr) || rpcErr.Code != "invalid_transition" {
			return nil, err
		}
	}
	rower, ok := runner.(interface {
		QueryRow(context.Context, string, ...any) db.Row
	})
	if !ok {
		return nil, fmt.Errorf("runner does not support query row")
	}
	var row supervisorControlRow
	var pid *int
	var endedAt, stopReason any
	err := rower.QueryRow(ctx, `
		SELECT supervisor_id, run_id, session_id, state, COALESCE(stdin_pipe_path, ''),
		       pid, COALESCE(pid_start_time, ''), ended_at, stop_reason
		  FROM striatumd.process_supervisors
		 WHERE repository_id = $1 AND session_id = $2
		   AND state = ANY($3)
		 ORDER BY started_at DESC, supervisor_id DESC
		 LIMIT 1`,
		repositoryID, sessionID, []string{"lost", "stopped"},
	).Scan(&row.SupervisorID, &row.RunID, &row.SessionID, &row.State, &row.StdinPipePath, &pid, &row.PIDStartTime, &endedAt, &stopReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if pid != nil {
		row.PID = *pid
		row.HasPID = true
	}
	row.Metadata = map[string]any{"ended_at": timestampOrNil(endedAt), "stop_reason": stopReason}
	return &row, nil
}

func loadWorkPacket(ctx context.Context, runner db.TxRunner, repositoryID, packetID string) (supervisionPacketRow, error) {
	var row supervisionPacketRow
	if wrongKind, ok := wrongKindPacketID(packetID); ok {
		return row, rpc.NewError("not_found", fmt.Sprintf("%s is a %s id, not a work packet id; use data.packet_id (or data.packet.packet_id) from claim-next JSON for supervise send", packetID, wrongKind), nil)
	}
	var packetRaw any
	err := runner.QueryRow(ctx, `
		SELECT packet_id, run_id, job_id, lease_id, session_id, packet_json
		  FROM striatumd.work_packets
		 WHERE repository_id = $1 AND packet_id = $2`,
		repositoryID, packetID,
	).Scan(&row.PacketID, &row.RunID, &row.JobID, &row.LeaseID, &row.SessionID, &packetRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, rpc.NewError("not_found", fmt.Sprintf("could not find work packet for packet_id=%q", packetID), nil)
	}
	if err != nil {
		return row, err
	}
	row.Packet = asMap(packetRaw)
	return row, nil
}

func wrongKindPacketID(packetID string) (string, bool) {
	switch {
	case strings.HasPrefix(packetID, "msg_"):
		return "message", true
	case strings.HasPrefix(packetID, "lease_"):
		return "lease", true
	case strings.HasPrefix(packetID, "job_"):
		return "job", true
	case strings.HasPrefix(packetID, "sess_"):
		return "session", true
	case strings.HasPrefix(packetID, "sup_"):
		return "supervisor", true
	default:
		return "", false
	}
}

func ensureActivePacketLease(ctx context.Context, runner db.TxRunner, repositoryID string, packet supervisionPacketRow, sessionID string) error {
	var state, ownerSessionID, resourceID string
	var expiresAt any
	err := runner.QueryRow(ctx, `
		SELECT state, owner_session_id, resource_id, expires_at
		  FROM striatumd.leases
		 WHERE repository_id = $1 AND lease_id = $2
		 FOR UPDATE`,
		repositoryID, packet.LeaseID,
	).Scan(&state, &ownerSessionID, &resourceID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return rpc.NewError("lease_error", "lease not found", nil)
	}
	if err != nil {
		return err
	}
	if state != "active" {
		return rpc.NewError("lease_error", "lease is not active", nil)
	}
	if ownerSessionID != sessionID {
		return rpc.NewError("lease_error", "lease is owned by another session", nil)
	}
	if resourceID != packet.JobID {
		return rpc.NewError("lease_error", "lease does not belong to the job", nil)
	}
	if expires, ok := asTime(expiresAt); ok && expires.UTC().Before(time.Now().UTC()) {
		return rpc.NewError("lease_error", "lease is expired", nil)
	}
	return nil
}

func reconcileSupervisorForDelivery(ctx context.Context, runner db.TxRunner, repositoryID string, supervisor supervisorControlRow, phase string) error {
	if !supervisor.HasPID {
		if err := markSupervisorLostInTx(ctx, runner, repositoryID, supervisor.SupervisorID, supervisor.RunID, supervisor.SessionID, "pid_missing observed by "+phase, 0, nil); err != nil {
			return err
		}
		return rpc.NewError("invalid_transition", "supervisor cannot accept delivery: pid_missing", nil)
	}
	if reason, degraded := supervisorDeliveryDegraded(supervisor.Metadata); degraded {
		return rpc.NewError("invalid_transition", "supervisor delivery is degraded: "+reason, nil)
	}
	live := gosupervisor.ProbeLaneLiveness(ctx, supervisionTmuxRunner, supervisor.Metadata, supervisor.PID, supervisor.PIDStartTime)
	if live.Class == string(gosupervisor.TmuxLivenessUnavailable) {
		return rpc.NewError("invalid_transition", "tmux probe unavailable; cannot verify lane: "+live.Detail, nil)
	}
	if !live.Alive {
		reason := live.Class
		if reason == "" {
			reason = "pid_gone"
		}
		lostPayload := map[string]any{"phase": phase, "reattach_reason": reason}
		if strings.HasPrefix(reason, "tmux_") {
			lostPayload["tmux_liveness"] = reason
		}
		if err := markSupervisorLostInTx(ctx, runner, repositoryID, supervisor.SupervisorID, supervisor.RunID, supervisor.SessionID, reason, supervisor.PID, lostPayload); err != nil {
			return err
		}
		if strings.HasPrefix(reason, "tmux_") {
			return rpc.NewError("invalid_transition", "supervisor cannot accept delivery: "+reason, nil)
		}
		return rpc.NewError("invalid_transition", fmt.Sprintf("supervisor pid is gone: %s", supervisor.SupervisorID), nil)
	}
	var pointerState string
	var daemonSupervisorID *string
	err := runner.QueryRow(ctx, `
		SELECT state, daemon_supervisor_id
		  FROM striatumd.process_supervisor_pointers
		 WHERE repository_id = $1 AND supervisor_id = $2
		 FOR UPDATE`,
		repositoryID, supervisor.SupervisorID,
	).Scan(&pointerState, &daemonSupervisorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return rpc.NewError("invalid_transition", "supervisor requires operator reconciliation before delivery: pointer_missing", nil)
	}
	if err != nil {
		return err
	}
	if pointerState != supervisor.State {
		return rpc.NewError("invalid_transition", "supervisor requires operator reconciliation before delivery: pointer_state_mismatch", nil)
	}
	if daemonSupervisorID == nil || *daemonSupervisorID == "" {
		return rpc.NewError("invalid_transition", "supervisor requires operator reconciliation before delivery: daemon_supervisor_missing", nil)
	}
	var daemonState string
	if err := runner.QueryRow(ctx, `
		SELECT state
		  FROM striatumd.daemon_supervisors
		 WHERE repository_id = $1 AND daemon_supervisor_id = $2
		 FOR UPDATE`,
		repositoryID, *daemonSupervisorID,
	).Scan(&daemonState); errors.Is(err, pgx.ErrNoRows) {
		return rpc.NewError("invalid_transition", "supervisor requires operator reconciliation before delivery: daemon_supervisor_missing", nil)
	} else if err != nil {
		return err
	}
	if daemonState != supervisor.State {
		return rpc.NewError("invalid_transition", "supervisor requires operator reconciliation before delivery: daemon_state_mismatch", nil)
	}
	return nil
}

func supervisorDeliveryDegraded(metadata map[string]any) (string, bool) {
	tmux := asMap(metadata["tmux"])
	delivery := asMap(tmux["delivery_liveness"])
	if len(delivery) == 0 {
		delivery = asMap(metadata["delivery_liveness"])
	}
	if len(delivery) == 0 {
		return "", false
	}
	if healthy, ok := delivery["healthy"].(bool); ok && healthy {
		return "", false
	}
	class, _ := delivery["class"].(string)
	class = strings.TrimSpace(class)
	if class == "" {
		return "", false
	}
	reason, _ := delivery["reason"].(string)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = class
	}
	return reason, true
}

type supervisorDeliveryResult struct {
	BytesWritten          int
	StdinDelivery         string
	StdinClosedAfterWrite bool
}

func writeSupervisorPayload(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID, pipePath string, payload []byte) (supervisorDeliveryResult, error) {
	metadata, err := pointerMetadata(ctx, runner, repositoryID, supervisorID)
	if err != nil {
		return supervisorDeliveryResult{}, err
	}
	stdinDelivery := metadataStdinDelivery(metadata)
	if stdinDelivery == stdinDeliveryOneShotEOF && metadata["stdin_delivery_consumed"] == true {
		return supervisorDeliveryResult{}, rpc.NewError("invalid_transition", "one-shot supervisor stdin has already been consumed", nil)
	}
	bytesWritten, err := writeToPipe(ctx, pipePath, payload)
	if err != nil {
		if errors.Is(err, errSupervisorPipeNoReader) {
			return supervisorDeliveryResult{}, &supervisorPipeNoReaderDeliveryError{
				supervisorID: supervisorID,
				metadata:     metadata,
				reason:       "stdin_reader_missing",
			}
		}
		return supervisorDeliveryResult{}, err
	}
	closed := stdinDelivery == stdinDeliveryOneShotEOF
	if closed {
		_ = os.Remove(pipePath)
		if err := mergePointerMetadata(ctx, runner, repositoryID, supervisorID, map[string]any{"stdin_delivery_consumed": true}); err != nil {
			return supervisorDeliveryResult{}, err
		}
	}
	return supervisorDeliveryResult{BytesWritten: bytesWritten, StdinDelivery: stdinDelivery, StdinClosedAfterWrite: closed}, nil
}

func writeToPipe(ctx context.Context, pipePath string, payload []byte) (int, error) {
	fd, err := syscall.Open(pipePath, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ENXIO) {
			return 0, errSupervisorPipeNoReader
		}
		return 0, err
	}
	file := os.NewFile(uintptr(fd), pipePath)
	defer file.Close()
	total := 0
	for total < len(payload) {
		n, err := file.Write(payload[total:])
		if n > 0 {
			total += n
		}
		if err != nil {
			if errors.Is(err, syscall.EPIPE) {
				return total, rpc.NewError("invalid_transition", "supervisor pipe is broken; child has closed stdin", nil)
			}
			if errors.Is(err, syscall.EAGAIN) {
				select {
				case <-ctx.Done():
					return total, ctx.Err()
				case <-time.After(20 * time.Millisecond):
					continue
				}
			}
			return total, err
		}
		if n == 0 {
			return total, rpc.NewError("invalid_transition", "supervisor pipe write returned zero bytes", nil)
		}
	}
	return total, nil
}

func markPointerDeliveryDegraded(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID string, metadata map[string]any, reason string) error {
	updated := map[string]any{}
	for key, value := range metadata {
		updated[key] = value
	}
	delivery := map[string]any{
		"class":       "degraded",
		"healthy":     false,
		"reason":      reason,
		"observed_at": nowString(),
	}
	if tmux := asMap(updated["tmux"]); len(tmux) > 0 {
		tmux["delivery_liveness"] = delivery
		updated["tmux"] = tmux
	} else {
		updated["delivery_liveness"] = delivery
	}
	return mergePointerMetadata(ctx, runner, repositoryID, supervisorID, updated)
}

func launchSupervisedProcess(ctx context.Context, config supervisionStartConfig, supervisorID, scratch, pipePath, eventPath string) (supervisionLaunchResult, error) {
	if config.Transport == supervisionTransportPTYHelper {
		return launchPTYHelper(ctx, config, supervisorID, scratch, pipePath, eventPath)
	}
	return launchPipeProcess(ctx, config, supervisorID, pipePath)
}

func launchPipeProcess(ctx context.Context, config supervisionStartConfig, supervisorID, pipePath string) (supervisionLaunchResult, error) {
	fd, err := syscall.Open(pipePath, syscall.O_RDWR, 0)
	if err != nil {
		return supervisionLaunchResult{}, fmt.Errorf("open stdin fifo: %w", err)
	}
	stdin := os.NewFile(uintptr(fd), "stdin.pipe")
	defer stdin.Close()
	cmd := exec.CommandContext(ctx, config.Command[0], config.Command[1:]...)
	cmd.Dir = config.RepoRoot
	cmd.Env = supervisedEnv(config.RepoRoot, config.RepositoryID, config.RunID, config.SessionID, supervisorID, config.LaneID)
	cmd.Stdin = stdin
	stdout, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return supervisionLaunchResult{}, err
	}
	defer stdout.Close()
	stderr, err := openSupervisedStderr()
	if err != nil {
		return supervisionLaunchResult{}, err
	}
	defer stderr.Close()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if err := cmd.Start(); err != nil {
		return supervisionLaunchResult{}, fmt.Errorf("cmd.Start: %w", err)
	}
	start, _ := processStartToken(cmd.Process.Pid)
	go func() {
		_ = cmd.Wait()
	}()
	return supervisionLaunchResult{PID: cmd.Process.Pid, PIDStartTime: start}, nil
}

func launchPTYHelper(ctx context.Context, config supervisionStartConfig, supervisorID, scratch, pipePath, eventPath string) (supervisionLaunchResult, error) {
	helper, err := resolveSupervisorHelper()
	if err != nil {
		return supervisionLaunchResult{}, err
	}
	if err := os.WriteFile(eventPath, nil, 0o600); err != nil {
		return supervisionLaunchResult{}, err
	}
	launchSpec := gosupervisor.HelperLaunchSpec{
		SchemaVersion:   gosupervisor.HelperLaunchSchemaVersion,
		SupervisorID:    supervisorID,
		ScratchDir:      filepath.Dir(scratch),
		Command:         config.Command,
		Env:             supervisedEnvEntries(config.RepoRoot, config.RepositoryID, config.RunID, config.SessionID, supervisorID, config.LaneID),
		WorkingDir:      config.RepoRoot,
		PacketInputPath: pipePath,
		RequireTmux:     config.RequireTmux,
	}
	specBody, err := json.Marshal(launchSpec)
	if err != nil {
		return supervisionLaunchResult{}, err
	}
	specPath := filepath.Join(scratch, "helper-launch.json")
	if err := os.WriteFile(specPath, append(specBody, '\n'), 0o600); err != nil {
		return supervisionLaunchResult{}, err
	}
	eventFile, err := os.OpenFile(eventPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return supervisionLaunchResult{}, err
	}
	defer eventFile.Close()
	cmd := exec.CommandContext(ctx, helper)
	cmd.Dir = config.RepoRoot
	cmd.Stdout = eventFile
	stderr, err := openSupervisedStderr()
	if err != nil {
		return supervisionLaunchResult{}, err
	}
	defer stderr.Close()
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return supervisionLaunchResult{}, err
	}
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if err := cmd.Start(); err != nil {
		return supervisionLaunchResult{}, err
	}
	if _, err := stdin.Write(append(specBody, '\n')); err != nil {
		_ = terminateProcess(cmd.Process.Pid)
		return supervisionLaunchResult{}, err
	}
	_ = stdin.Close()
	events, offset, err := waitForHelperAgentStart(cmd, eventPath, helperStartTimeout())
	if err != nil {
		_ = terminateProcess(cmd.Process.Pid)
		return supervisionLaunchResult{}, err
	}
	agentPID, err := agentPIDFromEvents(events)
	if err != nil {
		_ = terminateProcess(cmd.Process.Pid)
		return supervisionLaunchResult{}, err
	}
	agentStart, _ := processStartToken(agentPID)
	helperStart, _ := processStartToken(cmd.Process.Pid)
	metadata := map[string]any{
		"transport":               supervisionTransportPTYHelper,
		"helper_binary":           helper,
		"helper_pid":              cmd.Process.Pid,
		"helper_pid_start_time":   helperStart,
		"helper_launch_spec_path": specPath,
		"helper_events_path":      eventPath,
	}
	if tmux := tmuxMetadataFromHelperEvents(events); tmux != nil {
		metadata["tmux"] = tmux
		if agentStart == "" {
			if token, _ := tmux["pane_start_token"].(string); token != "" {
				agentStart = token
			}
		}
	}
	return supervisionLaunchResult{
		PID:                 agentPID,
		PIDStartTime:        agentStart,
		HelperPID:           cmd.Process.Pid,
		HelperPIDStartTime:  helperStart,
		InitialHelperEvents: events,
		InitialHelperOffset: offset,
		Metadata:            metadata,
	}, nil
}

func tmuxMetadataFromHelperEvents(events []map[string]any) map[string]any {
	for _, event := range events {
		if event["event_type"] != gosupervisor.HelperEventAgentStarted {
			continue
		}
		payload := asMap(event["payload"])
		metadata := asMap(payload["metadata"])
		tmux := asMap(metadata["tmux"])
		if len(tmux) > 0 {
			if lastExit := attachClientExitMetadataFromHelperEvents(events); len(lastExit) > 0 {
				tmux["attach_client_last_exit"] = lastExit
				if delivery := asMap(lastExit["delivery_liveness"]); len(delivery) > 0 {
					tmux["delivery_liveness"] = delivery
				}
			}
			return tmux
		}
	}
	return nil
}

func attachClientExitMetadataFromHelperEvents(events []map[string]any) map[string]any {
	var last map[string]any
	for _, event := range events {
		if event["event_type"] != gosupervisor.HelperEventAttachExited {
			continue
		}
		payload := asMap(event["payload"])
		if len(payload) == 0 {
			continue
		}
		out := map[string]any{}
		if observedAt := metadataString(event["timestamp"]); observedAt != "" {
			out["observed_at"] = observedAt
		}
		if observedAt := metadataString(payload["observed_at"]); observedAt != "" {
			out["observed_at"] = observedAt
		}
		if tmuxLiveness := metadataString(payload["tmux_liveness"]); tmuxLiveness != "" {
			out["tmux_liveness"] = tmuxLiveness
		}
		if pid, ok := intValueOptional(payload["attach_client_pid"]); ok {
			out["attach_pid"] = pid
		} else if pid, ok := intValueOptional(payload["attach_pid"]); ok {
			out["attach_pid"] = pid
		}
		if exitCode, ok := intValueOptional(payload["attach_exit_code"]); ok {
			out["attach_exit_code"] = exitCode
		} else if exitCode, ok := intValueOptional(payload["exit_code"]); ok {
			out["attach_exit_code"] = exitCode
		}
		if panePID, ok := intValueOptional(payload["pid"]); ok {
			out["pane_pid"] = panePID
		}
		delivery := asMap(payload["delivery_liveness"])
		if len(delivery) == 0 && payload["delivery_degraded"] == true {
			observedAt := metadataString(out["observed_at"])
			delivery = map[string]any{
				"class":       "degraded",
				"healthy":     false,
				"reason":      "attach_client_exited",
				"observed_at": observedAt,
			}
		}
		if len(delivery) > 0 {
			out["delivery_liveness"] = delivery
		}
		last = out
	}
	if last == nil {
		return nil
	}
	return last
}

func tmuxPaneStartTokenFromMetadata(metadata map[string]any) string {
	tmux := asMap(metadata["tmux"])
	token, _ := tmux["pane_start_token"].(string)
	return token
}

func objectOrNil(value any) map[string]any {
	object := asMap(value)
	if len(object) == 0 {
		return nil
	}
	return object
}

func waitForHelperAgentStart(cmd *exec.Cmd, eventPath string, timeout time.Duration) ([]map[string]any, int, error) {
	deadline := time.Now().Add(timeout)
	var lastEvents []map[string]any
	lastOffset := 0
	for time.Now().Before(deadline) {
		events, offset, err := readHelperEventsFromFile(eventPath, 0)
		if err != nil {
			return nil, 0, err
		}
		if len(events) > 0 {
			lastEvents = events
			lastOffset = offset
			for _, event := range events {
				switch event["event_type"] {
				case gosupervisor.HelperEventAgentStarted:
					return events, offset, nil
				case gosupervisor.HelperEventError:
					return nil, 0, fmt.Errorf("PTY helper failed before attach: %v", event["payload"])
				case gosupervisor.HelperEventAgentExited:
					return nil, 0, fmt.Errorf("PTY helper agent exited before attach")
				}
			}
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, 0, fmt.Errorf("PTY helper did not report agent_started before timeout (events=%d, offset=%d)", len(lastEvents), lastOffset)
}

func agentPIDFromEvents(events []map[string]any) (int, error) {
	for _, event := range events {
		if event["event_type"] != gosupervisor.HelperEventAgentStarted {
			continue
		}
		pid, ok := intValueOptional(asMap(event["payload"])["pid"])
		if !ok {
			return 0, fmt.Errorf("PTY helper did not report agent pid")
		}
		return pid, nil
	}
	return 0, fmt.Errorf("PTY helper did not report agent_started")
}

func drainHelperEvents(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID string, wait time.Duration) error {
	metadata, err := pointerMetadata(ctx, runner, repositoryID, supervisorID)
	if err != nil {
		return err
	}
	if metadata["transport"] != supervisionTransportPTYHelper {
		return nil
	}
	path, _ := metadata["helper_events_path"].(string)
	if path == "" {
		return nil
	}
	offset, _ := intValueOptional(metadata["helper_events_offset"])
	deadline := time.Now().Add(wait)
	var events []map[string]any
	newOffset := offset
	for {
		events, newOffset, err = readHelperEventsFromFile(path, offset)
		if err != nil {
			return err
		}
		if len(events) > 0 || time.Now().After(deadline) || wait <= 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(events) > 0 {
		for _, event := range events {
			normalized, normErr := normalizeSuperviseReportEvent(event, "", supervisorID, 0)
			if normErr != nil {
				return normErr
			}
			if _, recErr := recordSuperviseReportEvent(ctx, runner, repositoryID, normalized); recErr != nil {
				return recErr
			}
		}
	}
	if newOffset != offset {
		return mergePointerMetadata(ctx, runner, repositoryID, supervisorID, map[string]any{"helper_events_offset": newOffset})
	}
	return nil
}

func readHelperEventsFromFile(path string, offset int) ([]map[string]any, int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, offset, nil
	}
	if err != nil {
		return nil, offset, err
	}
	if offset >= len(data) {
		return nil, len(data), nil
	}
	chunk := data[offset:]
	if len(chunk) == 0 {
		return nil, offset, nil
	}
	complete := chunk
	newOffset := len(data)
	if chunk[len(chunk)-1] != '\n' {
		last := strings.LastIndexByte(string(chunk), '\n')
		if last < 0 {
			return nil, offset, nil
		}
		complete = chunk[:last+1]
		newOffset = offset + last + 1
	}
	if strings.TrimSpace(string(complete)) == "" {
		return nil, newOffset, nil
	}
	events, err := parseHelperJSONL(string(complete))
	return events, newOffset, err
}

func pointerMetadata(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID string) (map[string]any, error) {
	var raw any
	err := runner.QueryRow(ctx, `
		SELECT metadata_json
		  FROM striatumd.process_supervisor_pointers
		 WHERE repository_id = $1 AND supervisor_id = $2`,
		repositoryID, supervisorID,
	).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	return asMap(raw), nil
}

func mergePointerMetadata(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID string, metadata map[string]any) error {
	current, err := pointerMetadata(ctx, runner, repositoryID, supervisorID)
	if err != nil {
		return err
	}
	for key, value := range metadata {
		if key == "tmux" {
			currentTmux := asMap(current["tmux"])
			nextTmux := asMap(value)
			if len(currentTmux) > 0 && len(nextTmux) > 0 {
				merged := copyMap(currentTmux)
				for tmuxKey, tmuxValue := range nextTmux {
					merged[tmuxKey] = tmuxValue
				}
				current[key] = merged
				continue
			}
		}
		current[key] = value
	}
	metadataArg, err := db.JSONBArg(runner, current)
	if err != nil {
		return err
	}
	return runner.Exec(ctx, `
		UPDATE striatumd.process_supervisor_pointers
		   SET metadata_json = $1::jsonb
		 WHERE repository_id = $2 AND supervisor_id = $3`,
		metadataArg, repositoryID, supervisorID,
	)
}

func updateSupervisorState(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID, daemonSupervisorID, state, updatedAt string, pid int, pidStartTime, heartbeatAt string, endedAt *string, stopReason *string) error {
	pidArg := any(nil)
	if pid > 0 {
		pidArg = pid
	}
	pidStartArg := nullableString(pidStartTime)
	heartbeatArg := nullableString(heartbeatAt)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.process_supervisors
		   SET state = $1,
		       pid = COALESCE($2, pid),
		       pid_start_time = COALESCE($3, pid_start_time),
		       heartbeat_at = COALESCE($4, heartbeat_at),
		       ended_at = $5,
		       stop_reason = $6
		 WHERE repository_id = $7 AND supervisor_id = $8`,
		state, pidArg, pidStartArg, heartbeatArg, nullableStringPointer(endedAt), nullableStringPointer(stopReason), repositoryID, supervisorID,
	); err != nil {
		return err
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.process_supervisor_pointers
		   SET state = $1,
		       pid = COALESCE($2, pid),
		       pid_start_time = COALESCE($3, pid_start_time),
		       updated_at = $4
		 WHERE repository_id = $5 AND supervisor_id = $6`,
		state, pidArg, pidStartArg, updatedAt, repositoryID, supervisorID,
	); err != nil {
		return err
	}
	if daemonSupervisorID == "" {
		return nil
	}
	return runner.Exec(ctx, `
		UPDATE striatumd.daemon_supervisors
		   SET state = $1,
		       pid = COALESCE($2, pid),
		       pid_start_time = COALESCE($3, pid_start_time),
		       heartbeat_at = COALESCE($4, heartbeat_at),
		       ended_at = $5,
		       stop_reason = $6
		 WHERE repository_id = $7 AND daemon_supervisor_id = $8`,
		state, pidArg, pidStartArg, heartbeatArg, nullableStringPointer(endedAt), nullableStringPointer(stopReason), repositoryID, daemonSupervisorID,
	)
}

func refreshSupervisorHeartbeat(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID, daemonSupervisorID, updatedAt string) error {
	if err := runner.Exec(ctx, `
		UPDATE striatumd.process_supervisors
		   SET heartbeat_at = $1
		 WHERE repository_id = $2 AND supervisor_id = $3`,
		updatedAt, repositoryID, supervisorID,
	); err != nil {
		return err
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.process_supervisor_pointers
		   SET updated_at = $1
		 WHERE repository_id = $2 AND supervisor_id = $3`,
		updatedAt, repositoryID, supervisorID,
	); err != nil {
		return err
	}
	if daemonSupervisorID == "" {
		return nil
	}
	return runner.Exec(ctx, `
		UPDATE striatumd.daemon_supervisors
		   SET heartbeat_at = $1
		 WHERE repository_id = $2 AND daemon_supervisor_id = $3`,
		updatedAt, repositoryID, daemonSupervisorID,
	)
}

func markSupervisorLost(ctx context.Context, runner db.Runner, repositoryID, supervisorID, runID, sessionID, reason string, pid int, payload map[string]any) error {
	_, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		return nil, markSupervisorLostInTx(ctx, tx, repositoryID, supervisorID, runID, sessionID, reason, pid, payload)
	})
	return err
}

func markSupervisorLostInTx(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID, runID, sessionID, reason string, pid int, payload map[string]any) error {
	now := nowString()
	daemonSupervisorID := ""
	_ = runner.QueryRow(ctx, `
		SELECT daemon_supervisor_id
		  FROM striatumd.process_supervisor_pointers
		 WHERE repository_id = $1 AND supervisor_id = $2`,
		repositoryID, supervisorID,
	).Scan(&daemonSupervisorID)
	if err := updateSupervisorState(ctx, runner, repositoryID, supervisorID, daemonSupervisorID, "lost", now, pid, "", "", &now, &reason); err != nil {
		return err
	}
	eventPayload := map[string]any{
		"supervisor_id":        supervisorID,
		"daemon_supervisor_id": nullableString(daemonSupervisorID),
		"pid":                  optionalPositiveInt(pid),
		"reason":               reason,
	}
	for key, value := range payload {
		eventPayload[key] = value
	}
	_, err := appendEvent(ctx, runner, repositoryID, runID, "supervisor.lost", sessionID, nil, nil, nil, nil, eventPayload)
	return err
}

func requireSessionExists(ctx context.Context, runner db.Runner, repositoryID, sessionID string) error {
	var found string
	err := runner.QueryRow(ctx, `
		SELECT session_id
		  FROM striatumd.sessions
		 WHERE repository_id = $1 AND session_id = $2
		 LIMIT 1`,
		repositoryID, sessionID,
	).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return rpc.NewError("not_found", "session not found: "+sessionID, nil)
	}
	return err
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
	if value, ok := boolLaneValue(lane, "agent_loop"); ok {
		return value
	}
	capabilities := asMap(lane["adapter_capabilities"])
	if value, ok := boolLaneValue(capabilities, "agent_loop"); ok {
		return value
	}
	return false
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
// on the augmented supervised PATH, so the lane binary resolves regardless of
// the daemon's own PATH. A no-op when argv0 is already a path or cannot be
// resolved (the launch will then surface the original not-found error).
func resolveSupervisedCommandBinary(command []string) []string {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return command
	}
	bin := command[0]
	if strings.ContainsRune(bin, os.PathSeparator) {
		return command
	}
	for _, dir := range filepath.SplitList(supervisedPath()) {
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

func metadataStdinDelivery(metadata map[string]any) string {
	value, _ := metadata["stdin_delivery"].(string)
	if value == stdinDeliveryOneShotEOF || value == stdinDeliveryPersistentFIFO {
		return value
	}
	return stdinDeliveryPersistentFIFO
}

func currentDaemonInstanceID() string {
	if value := os.Getenv("STRIATUM_DAEMON_INSTANCE_ID"); value != "" {
		return value
	}
	return "go-pg-handler"
}

func supervisedEnv(repoRoot, repositoryID, runID, sessionID, supervisorID, laneID string) []string {
	return mergeEnvReplacing(os.Environ(), supervisedEnvEntries(repoRoot, repositoryID, runID, sessionID, supervisorID, laneID))
}

func supervisedEnvEntries(repoRoot, repositoryID, runID, sessionID, supervisorID, laneID string) []string {
	return []string{
		"PATH=" + supervisedPath(),
		"STRIATUM_REPOSITORY_ID=" + repositoryID,
		"STRIATUM_RUN_ID=" + runID,
		"STRIATUM_SESSION_ID=" + sessionID,
		"STRIATUM_SUPERVISOR_ID=" + supervisorID,
		"STRIATUM_REPO=" + repoRoot,
		"STRIATUM_LANE_ID=" + laneID,
	}
}

func mergeEnvReplacing(base []string, updates []string) []string {
	keys := map[string]bool{}
	for _, entry := range updates {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			keys[key] = true
		}
	}
	out := make([]string, 0, len(base)+len(updates))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" || keys[key] {
			continue
		}
		out = append(out, entry)
	}
	return append(out, updates...)
}

// openSupervisedStderr returns the stderr sink for a supervised lane: by
// default /dev/null (D028 no-capture), but if STRIATUM_SUPERVISED_STDERR_LOG
// is set, it's appended to that path. Used to surface agent-loop / lane
// failures that would otherwise be silent — debug only.
func openSupervisedStderr() (*os.File, error) {
	if path := strings.TrimSpace(os.Getenv("STRIATUM_SUPERVISED_STDERR_LOG")); path != "" {
		return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	}
	return os.OpenFile(os.DevNull, os.O_WRONLY, 0)
}

func supervisedPath() string {
	current := os.Getenv("PATH")
	entries := filepath.SplitList(current)
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry != "" {
			seen[entry] = true
		}
	}
	for _, dir := range supervisedPathDirs() {
		if seen[dir] {
			continue
		}
		entries = append(entries, dir)
		seen[dir] = true
	}
	return strings.Join(entries, string(os.PathListSeparator))
}

func supervisedPathDirs() []string {
	rawDirs := []string{}
	if override := strings.TrimSpace(os.Getenv("STRIATUM_SUPERVISED_PATH_DIRS")); override != "" {
		rawDirs = filepath.SplitList(override)
	} else if home := supervisedHomeDir(); home != "" {
		rawDirs = []string{
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
		}
	}
	dirs := make([]string, 0, len(rawDirs))
	seen := map[string]bool{}
	for _, raw := range rawDirs {
		dir := filepath.Clean(strings.TrimSpace(raw))
		if dir == "." || dir == "" || !filepath.IsAbs(dir) || seen[dir] {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		dirs = append(dirs, dir)
		seen[dir] = true
	}
	return dirs
}

func supervisedHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return home
	}
	return strings.TrimSpace(os.Getenv("HOME"))
}

func resolveSupervisorHelper() (string, error) {
	if override := os.Getenv("STRIATUM_SUPERVISOR_HELPER"); override != "" {
		return override, nil
	}
	if found, err := exec.LookPath("striatum-supervisor-helper"); err == nil {
		return found, nil
	}
	repoHelper := filepath.Join("go", "bin", "striatum-supervisor-helper")
	if _, err := os.Stat(repoHelper); err == nil {
		abs, _ := filepath.Abs(repoHelper)
		return abs, nil
	}
	return "", fmt.Errorf("striatum-supervisor-helper not found; set STRIATUM_SUPERVISOR_HELPER or build go/bin/striatum-supervisor-helper")
}

func helperStartTimeout() time.Duration {
	raw := os.Getenv("STRIATUM_SUPERVISOR_HELPER_START_TIMEOUT")
	if raw == "" {
		return 5 * time.Second
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds < 0.1 {
		return 5 * time.Second
	}
	return time.Duration(seconds * float64(time.Second))
}

func terminateProcess(pid int) any {
	if pid <= 0 || !pidAliveLocal(pid) {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	signaled := "SIGTERM"
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return nil
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		reapIfChild(pid)
		if !pidAliveLocal(pid) {
			return signaled
		}
		time.Sleep(50 * time.Millisecond)
	}
	signaled = "SIGKILL"
	_ = proc.Signal(syscall.SIGKILL)
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reapIfChild(pid)
		if !pidAliveLocal(pid) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return signaled
}

func terminateProcessWithStartToken(pid int, expectedStartToken string) (any, string) {
	if pid <= 0 {
		return nil, ""
	}
	expectedStartToken = strings.TrimSpace(expectedStartToken)
	if expectedStartToken == "" {
		return nil, "start_token_missing"
	}
	currentStartToken, ok := processStartToken(pid)
	if !ok || strings.TrimSpace(currentStartToken) == "" {
		return nil, "start_token_unavailable"
	}
	if currentStartToken != expectedStartToken {
		return nil, "start_token_mismatch"
	}
	return terminateProcess(pid), ""
}

func metadataString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func pidAliveLocal(pid int) bool {
	if pid <= 0 {
		return false
	}
	if processZombieLocal(pid) {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func processZombieLocal(pid int) bool {
	if runtime.GOOS != "linux" || pid <= 0 {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	return linuxProcStatZombie(data)
}

func linuxProcStatZombie(data []byte) bool {
	text := string(data)
	idx := strings.LastIndex(text, ")")
	if idx < 0 || idx+1 >= len(text) {
		return false
	}
	fields := strings.Fields(text[idx+1:])
	return len(fields) > 0 && fields[0] == "Z"
}

func reapIfChild(pid int) {
	var status syscall.WaitStatus
	_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
}

func requiredControlTextParam(envelope rpc.Envelope, key string, message string) (string, error) {
	value, ok := envelope.Params[key]
	if !ok || value == nil {
		return "", rpc.NewError("schema_invalid", message, nil)
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return "", rpc.NewError("schema_invalid", message, nil)
	}
	return text, nil
}

func helperProcessPayload(transport string, helperPID int, helperStart, eventPath string) any {
	if transport != supervisionTransportPTYHelper {
		return nil
	}
	return map[string]any{
		"pid":            optionalPositiveInt(helperPID),
		"pid_start_time": nullableString(helperStart),
		"events_path":    eventPath,
	}
}

func laneAttestation(pidStartTime string) string {
	if pidStartTime == "" {
		return "unattested"
	}
	return "attested"
}

func timestampOrNil(value any) any {
	if value == nil {
		return nil
	}
	if text := timestampString(value); text != "<nil>" {
		return text
	}
	return nil
}

func nullableStringPointer(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func optionalPositiveInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func optionalIntValue(value int, ok bool) any {
	if !ok {
		return nil
	}
	return value
}

func intValueOptional(value any) (int, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, false
	case int:
		return typed, true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		if typed == float64(int(typed)) {
			return int(typed), true
		}
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err == nil
	case string:
		if typed == "" {
			return 0, false
		}
		parsed, err := strconv.Atoi(typed)
		return parsed, err == nil
	}
	return 0, false
}

func copyMap(input map[string]any) map[string]any {
	output := map[string]any{}
	for key, value := range input {
		output[key] = value
	}
	return output
}
