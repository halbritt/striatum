package mutations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/halbritt/striatum/go/pkg/agentloop"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/lanehealth"
	"github.com/halbritt/striatum/go/pkg/rpc"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	supervisionTransportPipe      = "pipe"
	supervisionTransportPTYHelper = "pty_helper"

	stdinDeliveryPersistentFIFO = "persistent_fifo"
	stdinDeliveryOneShotEOF     = "one_shot_eof"

	agentLoopModeSelfDriving = "self_driving"
	supervisedLaneOSUserEnv  = "STRIATUM_LANE_OS_USER"
	// agentLoopModePush labels a supervised lane that does NOT use the agent loop:
	// a stdin-FIFO/push consumer that reads a delivered packet then runs the agent,
	// rather than a true self-driver that calls work.await_packet. Recording the
	// honest mode (#146) keeps sessionHasSelfDrivingSupervisor — and therefore the
	// claim-next hint — accurate: a push lane needs `supervise send`, so it must not
	// receive the self_driving "do not run supervise send" note.
	agentLoopModePush = "supervised_push"
)

type supervisorControlRow struct {
	SupervisorID       string
	RunID              string
	SessionID          string
	State              string
	ScratchPath        string
	StdinPipePath      string
	PID                int
	HasPID             bool
	PIDStartTime       string
	DaemonSupervisorID string
	Metadata           map[string]any
	EndedAt            any
	StopReason         any
}

type supervisionPacketRow struct {
	PacketID  string
	RunID     string
	JobID     string
	LeaseID   string
	SessionID string
	Packet    map[string]any
}

var (
	supervisionMkfifo = func(path string) error {
		return syscall.Mkfifo(path, 0o600)
	}
	supervisionLaunch         = launchSupervisedProcess
	supervisionRebridgeLaunch = launchRebridgeHelper
	supervisionWrite          = writeSupervisorPayload
	supervisionTmuxRunner     = gosupervisor.DefaultTmuxRunner()
	signalProcessZeroLocal    = signalProcessZero
	errSupervisorPipeNoReader = errors.New("supervisor pipe has no reader")
)

func HandleSuperviseStart(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredControlTextParam(envelope, "session_id", "supervise.start requires session_id")
	if err != nil {
		return nil, err
	}
	replace := boolParam(envelope, "replace")
	providerAuthGate, err := providerAuthGateMode(envelope)
	if err != nil {
		return nil, err
	}
	config, err := loadSupervisionStartConfig(ctx, runner, repositoryID, sessionID)
	if err != nil {
		return nil, err
	}
	if err := runSuperviseProviderAuthGate(ctx, config, providerAuthGate); err != nil {
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
	// #279: when the lane runs as a non-owner OS user (config.RunAsUser set —
	// configuredLaneRunAsUser already collapses the owner case to ""), it can
	// traverse `.striatum` but cannot create its ephemeral MCP config under
	// `.striatum/scratch` (agentloop.writeEphemeralMCPConfig does os.CreateTemp
	// there) without a writable-scratch ACL. Prepare that grant BEFORE launch so
	// non-Codex lanes don't fail to start with "create ephemeral mcp config: ...
	// permission denied". No-op for owner-run lanes.
	if err := prepareScratchACLsForLaneUser(config.RepoRoot, config.RunAsUser); err != nil {
		return nil, rpc.NewError("invalid_transition", "could not prepare lane scratch ACLs: "+err.Error(), nil)
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
		if err := supersedeStaleSupervisorIfRequested(ctx, tx, repositoryID, sessionID, replace, startedAt); err != nil {
			return nil, err
		}
		// RFC 0096 V2 / #135: mint a session-BOUND capability token and inject it
		// into the lane env (below) so the lane authenticates as its own session,
		// not the shared operator override. Done inside the start transaction so the
		// token is committed atomically with the supervisor rows; the plaintext is
		// captured into config.CapabilityToken (in-memory → lane env only).
		boundToken, err := mintSessionBoundToken(ctx, tx, repositoryID, sessionID)
		if err != nil {
			return nil, err
		}
		if token, ok := boundToken["token"].(string); ok {
			config.CapabilityToken = token
		}
		if err := insertStartingsSupervisorRowsWithCleanError(ctx, tx, repositoryID, config, supervisorID, daemonSupervisorID, scratch, pipePath, eventPath, startedAt, sessionID); err != nil {
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
		if config.AgentLoopAutoPromoted {
			// #431: make the daemon's push→self-driving promotion legible in the run
			// timeline so the operator can see WHY a bare agent-CLI lane is being
			// driven as an agent loop (instead of a silent flip).
			payload["agent_loop_auto_promoted"] = true
		}
		if config.Transport == supervisionTransportPTYHelper {
			payload["helper_events_path"] = eventPath
		}
		_, err = appendEvent(ctx, tx, repositoryID, config.RunID, "supervisor.starting", sessionID, nil, nil, nil, nil, payload)
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
		// #201: the daemon could not confirm the launched agent PID. That does NOT
		// always mean the lane died — for a sandboxed lane whose helper runs as the
		// lane OS user, the pane/process can be perfectly alive while only the
		// daemon's attach leg failed (e.g. the lane cannot reach the daemon socket
		// to attest). Probe the pane FIRST: a provably-alive pane is recorded
		// detached (recoverable via rebridge), never a misleading lost /
		// "child exited", and we must not run the dead-lane cleanup (which kills the
		// tmux session) against a live pane.
		live := probeLaneLivenessAtStart(ctx, launch.Metadata, launch.PID, launch.PIDStartTime)
		state, reason, message := failedAttachOutcome(live.Alive)
		if state == "detached" {
			// Keep the stdin pipe so a rebridge can reuse it, do NOT tear down the
			// live pane, and leave the session recoverable rather than lost.
			cleanupPipe = false
			_ = markSupervisorDetachedAfterFailedAttach(ctx, runner, repositoryID, supervisorID, daemonSupervisorID, config.RunID, sessionID, reason, launch.PID, launch.PIDStartTime, launch.Metadata, map[string]any{
				"phase":               "start",
				"pane_liveness_class": live.Class,
				"pane_alive":          live.Alive,
			})
			return nil, rpc.NewError("invalid_transition", message, nil)
		}
		// Genuine dead child: tear down any tmux/process remnants, then mark lost.
		payload := failedAttachCleanupPayload(ctx, launch)
		payload["pane_liveness_class"] = live.Class
		payload["pane_alive"] = live.Alive
		_ = markSupervisorLostWithMetadata(ctx, runner, repositoryID, supervisorID, config.RunID, sessionID, reason, launch.PID, launch.Metadata, payload)
		return nil, rpc.NewError("invalid_transition", message, nil)
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
		if config.AgentLoopAutoPromoted {
			payload["agent_loop_auto_promoted"] = true
		}
		if config.Transport == supervisionTransportPTYHelper {
			payload["helper_pid"] = optionalPositiveInt(launch.HelperPID)
			payload["helper_events_path"] = eventPath
		}
		if tmux := objectOrNil(launch.Metadata["tmux"]); tmux != nil {
			payload["tmux"] = tmux
		}
		if runAsUser := metadataString(launch.Metadata["run_as_user"]); runAsUser != "" {
			payload["run_as_user"] = runAsUser
		}
		_, err := appendEvent(ctx, tx, repositoryID, config.RunID, "supervisor.started", sessionID, nil, nil, nil, nil, payload)
		return nil, err
	}); err != nil {
		return nil, err
	}
	cleanupPipe = false
	result := map[string]any{
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
	}
	if config.AgentLoopAutoPromoted {
		result["agent_loop_auto_promoted"] = true
	}
	if runAsUser := metadataString(launch.Metadata["run_as_user"]); runAsUser != "" {
		result["run_as_user"] = runAsUser
	}
	// #115: a prepared/running run uses its FROZEN workflow snapshot, so on-disk
	// workflow.json edits are inert. Surface a warning when the lane just launched
	// from a snapshot that diverges from the current file, so the operator does not
	// burn time on a silent no-op (the fix is to prepare a new run).
	if w := snapshotDivergenceWarningForRun(ctx, runner, repositoryID, config.RepoRoot, config.WorkflowSnapshotID); w != "" {
		result["snapshot_divergence_warning"] = w
	}
	if config.AgentLoopMode == agentLoopModePush {
		result["auto_dispatch"] = autoDispatchPushSupervisor(ctx, runner, repositoryID, sessionID)
	}
	return result, nil
}

func autoDispatchPushSupervisor(ctx context.Context, runner db.Runner, repositoryID, sessionID string) map[string]any {
	result, err := withTxRetryOnDeadlock(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		claim, err := claimNextInTx(ctx, tx, repositoryID, sessionID, 3600)
		if err != nil {
			return nil, err
		}
		status, _ := claim["status"].(string)
		if status != "claimed" {
			out := map[string]any{"status": status}
			if status == "" {
				out["status"] = "no_work"
			}
			for _, key := range []string{"paused", "ineligible_reason", "workflow_job_id", "hint"} {
				if value, ok := claim[key]; ok {
					out[key] = value
				}
			}
			return out, nil
		}
		packetID, _ := claim["packet_id"].(string)
		if packetID == "" {
			return nil, rpc.NewError("invalid_transition", "claim-next did not return a packet_id for push auto-dispatch", nil)
		}
		delivery, err := deliverClaimedPacketToSupervisorInTx(ctx, tx, repositoryID, sessionID, packetID, "supervise.start.auto_dispatch")
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"status":    "delivered",
			"packet_id": packetID,
			"delivery":  delivery,
		}, nil
	})
	if err == nil {
		return result
	}
	var noReader *supervisorPipeNoReaderDeliveryError
	if errors.As(err, &noReader) {
		if _, markErr := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
			return map[string]any{}, markPointerDeliveryDegraded(ctx, tx, repositoryID, noReader.supervisorID, noReader.metadata, noReader.reason)
		}); markErr != nil {
			err = markErr
		}
	}
	out := map[string]any{
		"status": "failed",
		"error":  err.Error(),
	}
	var rpcErr *rpc.Error
	if errors.As(err, &rpcErr) {
		out["error_code"] = rpcErr.Code
		out["error"] = rpcErr.Message
		if len(rpcErr.Details) > 0 {
			out["error_details"] = rpcErr.Details
		}
	}
	return out
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
		return deliverClaimedPacketToSupervisorInTx(ctx, tx, repositoryID, sessionID, packetID, "supervise.send")
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

func deliverClaimedPacketToSupervisorInTx(ctx context.Context, tx db.TxRunner, repositoryID, sessionID, packetID, phase string) (map[string]any, error) {
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
	if err := reconcileSupervisorForDelivery(ctx, tx, repositoryID, supervisor, phase); err != nil {
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
	// A buffered (no-reader) write is NOT durably delivered: the payload sits in
	// the process-global in-memory pipeBuffers and a daemon restart drops it. Do
	// not record supervisor.packet_delivered for it — that would land a "delivered"
	// event for a packet that may never reach the lane, corrupting the provenance
	// the runner depends on. Record a distinct supervisor.packet_buffered event and
	// return a degraded delivery_state so the caller/operator sees the truth (#358).
	eventName := "supervisor.packet_delivered"
	deliveryState := "delivered_unacknowledged"
	if delivery.Buffered {
		eventName = "supervisor.packet_buffered"
		deliveryState = "buffered_no_reader"
	}
	_, err = appendEvent(ctx, tx, repositoryID, supervisor.RunID, eventName, sessionID, nil, nil, nil, nil, map[string]any{
		"supervisor_id":            supervisor.SupervisorID,
		"packet_id":                packetID,
		"bytes_written":            delivery.BytesWritten,
		"stdin_delivery":           delivery.StdinDelivery,
		"stdin_closed_after_write": delivery.StdinClosedAfterWrite,
		"buffered":                 delivery.Buffered,
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
		"delivery_state":           deliveryState,
		"buffered":                 delivery.Buffered,
		"durable":                  !delivery.Buffered,
		"control_ack_expected":     true,
	}, nil
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
		if terminal.State == "lost" {
			return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
				return stopSupervisorInTx(ctx, tx, repositoryID, sessionID, reason, *terminal)
			})
		}
		return map[string]any{
			"supervisor_id": terminal.SupervisorID,
			"session_id":    sessionID,
			"pid":           optionalIntValue(terminal.PID, terminal.HasPID),
			"state":         "stopped",
			"ended_at":      terminal.EndedAt,
			"stop_reason":   terminal.StopReason,
			"signal":        nil,
			"note":          "supervisor was already " + terminal.State,
		}, nil
	}

	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		supervisor, err := requireActiveControlSupervisor(ctx, tx, repositoryID, sessionID, true)
		if err != nil {
			return nil, err
		}
		return stopSupervisorInTx(ctx, tx, repositoryID, sessionID, reason, supervisor)
	})
}

func stopSupervisorInTx(ctx context.Context, tx db.TxRunner, repositoryID, sessionID, reason string, supervisor supervisorControlRow) (map[string]any, error) {
	_ = drainHelperEvents(ctx, tx, repositoryID, supervisor.SupervisorID, 0)
	var signaled any
	eventExtra := map[string]any{}
	stopNote := any(nil)
	if tmuxIdentity, ok := gosupervisor.TmuxIdentityFromMetadata(supervisor.Metadata); ok {
		signal, note, fallbackReason, cleanupSkip := stopTmuxBackedLane(ctx, supervisor.Metadata, tmuxIdentity, supervisor.PID, supervisor.PIDStartTime)
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
	// #50: a stopped supervisor must not leave its session reading as `active` —
	// that pollutes "find the latest active <role>/<lane> session" lookups
	// (interrogation targeting, reviewer prompts). Close the session in one
	// guarded UPDATE: only when it is still `active` AND holds no active lease
	// (mid-work sessions are left for explicit recovery). Done as a single
	// conditional statement so no extra row read is required.
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
	if err := markActiveSessionTerminal(ctx, tx, activeSessionTerminalUpdate{
		RepositoryID: repositoryID,
		SessionID:    sessionID,
		State:        "stopped",
		Reason:       "supervisor stopped: " + reason,
	}); err != nil {
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
	_, err := appendEvent(ctx, tx, repositoryID, supervisor.RunID, "supervisor.stopped", sessionID, nil, nil, nil, nil, eventPayload)
	if err != nil {
		return nil, err
	}
	agentloop.CleanupGeminiSettings(supervisorWorkingDir(supervisor), supervisor.SupervisorID)
	agentloop.CleanupClaudeScheduledTasksLock(supervisorWorkingDir(supervisor))
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
}

func failedAttachCleanupPayload(ctx context.Context, launch supervisionLaunchResult) map[string]any {
	payload := map[string]any{"phase": "start"}
	if tmuxIdentity, ok := gosupervisor.TmuxIdentityFromMetadata(launch.Metadata); ok {
		signal, note, fallbackReason, cleanupSkip := stopTmuxBackedLane(ctx, launch.Metadata, tmuxIdentity, launch.PID, launch.PIDStartTime)
		if signal != nil {
			payload["signal"] = signal
		}
		if note != "" {
			payload["cleanup_note"] = note
		}
		if fallbackReason != "" {
			payload["tmux_kill_fallback_reason"] = fallbackReason
		}
		if cleanupSkip != "" {
			payload["pane_pid_cleanup_skipped_reason"] = cleanupSkip
		}
		return payload
	}
	if launch.PID > 0 {
		signal, cleanupSkip := terminateProcessWithStartToken(launch.PID, launch.PIDStartTime)
		if signal != nil {
			payload["signal"] = signal
		}
		if cleanupSkip != "" {
			payload["pid_cleanup_skipped_reason"] = cleanupSkip
		}
	}
	return payload
}

func HandleSuperviseRebridge(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredControlTextParam(envelope, "session_id", "supervise.rebridge requires session_id")
	if err != nil {
		return nil, err
	}
	var supervisor supervisorControlRow
	var identity gosupervisor.TmuxIdentity
	var eventPath string
	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		var err error
		supervisor, err = requireActiveControlSupervisor(ctx, tx, repositoryID, sessionID, true)
		if err != nil {
			return nil, err
		}
		if supervisor.State != "attached" {
			return nil, rpc.NewError("invalid_transition", "supervise.rebridge requires an attached supervisor", nil)
		}
		identity, err = requireRebridgeableTmuxPane(ctx, supervisor)
		if err != nil {
			return nil, err
		}
		eventPath = metadataString(supervisor.Metadata["helper_events_path"])
		if eventPath == "" {
			eventPath = filepath.Join(supervisor.ScratchPath, "helper-events.jsonl")
		}
		if supervisor.StdinPipePath == "" {
			return nil, rpc.NewError("invalid_transition", "supervise.rebridge requires a supervisor stdin pipe path", nil)
		}
		if err := ensureSupervisorFIFO(supervisor.StdinPipePath); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	}); err != nil {
		return nil, err
	}

	launch, err := supervisionRebridgeLaunch(ctx, supervisor, identity, eventPath)
	if err != nil {
		return nil, rpc.NewError("invalid_transition", "supervise.rebridge could not attach delivery bridge: "+err.Error(), nil)
	}
	rebridgedAt := nowString()
	result, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// #67: a successful rebridge rebuilds the delivery transport (fresh helper
		// + persistent FIFO). The re-attached tmux attach-observer then re-emits a
		// benign attach_client_exited (#63 F7) — that is NOT a delivery failure, so
		// it must not keep the lane reported as degraded. Only a real transport
		// failure the helper reports on relaunch (helper_error / agent_exited)
		// preserves the degraded delivery_liveness block.
		hasRealDeliveryFailure := false
		if len(launch.InitialHelperEvents) > 0 {
			for _, event := range launch.InitialHelperEvents {
				normalized, normErr := normalizeSuperviseReportEvent(event, "", supervisor.SupervisorID, 0)
				if normErr != nil {
					return nil, normErr
				}
				switch normalized.EventType {
				case string(gosupervisor.HelperEventError), string(gosupervisor.HelperEventAgentExited):
					hasRealDeliveryFailure = true
				}
				if _, recErr := recordSuperviseReportEvent(ctx, tx, repositoryID, normalized); recErr != nil {
					return nil, recErr
				}
			}
		}
		current, err := pointerMetadata(ctx, tx, repositoryID, supervisor.SupervisorID)
		if err != nil {
			return nil, err
		}
		updated := copyMap(current)
		updated["helper_pid"] = launch.HelperPID
		updated["helper_pid_start_time"] = launch.HelperPIDStartTime
		updated["helper_events_path"] = eventPath
		updated["helper_events_offset"] = launch.InitialHelperOffset
		if !hasRealDeliveryFailure {
			delete(updated, "delivery_liveness")
		}
		if tmux := asMap(updated["tmux"]); len(tmux) > 0 {
			if !hasRealDeliveryFailure {
				delete(tmux, "delivery_liveness")
			}
			tmux["attach_client_pid"] = launch.Metadata["attach_client_pid"]
			tmux["last_rebridged_at"] = rebridgedAt
			if launchTmux := asMap(launch.Metadata["tmux"]); len(launchTmux) > 0 {
				for key, value := range launchTmux {
					tmux[key] = value
				}
				if !hasRealDeliveryFailure {
					delete(tmux, "delivery_liveness")
				}
			}
			updated["tmux"] = tmux
		}
		if err := replacePointerMetadata(ctx, tx, repositoryID, supervisor.SupervisorID, updated); err != nil {
			return nil, err
		}
		if err := refreshSupervisorHeartbeat(ctx, tx, repositoryID, supervisor.SupervisorID, supervisor.DaemonSupervisorID, rebridgedAt); err != nil {
			return nil, err
		}
		payload := map[string]any{
			"supervisor_id":     supervisor.SupervisorID,
			"session_id":        sessionID,
			"helper_pid":        launch.HelperPID,
			"attach_client_pid": launch.Metadata["attach_client_pid"],
			"tmux_liveness":     string(gosupervisor.TmuxLivenessOK),
		}
		_, err = appendEvent(ctx, tx, repositoryID, supervisor.RunID, "supervisor.rebridged", sessionID, nil, nil, nil, nil, payload)
		if err != nil {
			return nil, err
		}
		deliveryStateVal := "healthy"
		if hasRealDeliveryFailure {
			deliveryStateVal = "degraded"
		}
		return map[string]any{
			"supervisor_id":     supervisor.SupervisorID,
			"session_id":        sessionID,
			"run_id":            supervisor.RunID,
			"state":             "attached",
			"delivery_state":    deliveryStateVal,
			"helper_pid":        launch.HelperPID,
			"attach_client_pid": launch.Metadata["attach_client_pid"],
			"rebridged_at":      rebridgedAt,
			"tmux":              asMap(updated["tmux"]),
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func requireRebridgeableTmuxPane(ctx context.Context, supervisor supervisorControlRow) (gosupervisor.TmuxIdentity, error) {
	identity, ok := gosupervisor.TmuxIdentityFromMetadata(supervisor.Metadata)
	if !ok {
		return gosupervisor.TmuxIdentity{}, rpc.NewError("invalid_transition", "supervise.rebridge requires a tmux-backed supervisor", nil)
	}
	live := gosupervisor.ProbeLaneLiveness(ctx, tmuxRunnerForSupervisorMetadata(supervisor.Metadata), supervisor.Metadata, supervisor.PID, supervisor.PIDStartTime)
	if live.Class == string(gosupervisor.TmuxLivenessUnavailable) {
		return gosupervisor.TmuxIdentity{}, rpc.NewError("invalid_transition", "supervise.rebridge cannot verify tmux pane liveness: "+live.Detail, nil)
	}
	if !live.Alive {
		return gosupervisor.TmuxIdentity{}, rpc.NewError("invalid_transition", "supervise.rebridge refused because pane liveness is "+live.Class+"; stop and restart or reclaim the lane", nil)
	}
	if live.Tmux != nil && live.Tmux.ObservedPanePID > 0 {
		identity.PanePID = live.Tmux.ObservedPanePID
	}
	if live.Tmux != nil && live.Tmux.ObservedStartTok != "" {
		identity.PaneStartToken = live.Tmux.ObservedStartTok
	}
	return identity, nil
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
	if config.RunAsUser != "" {
		metadata["run_as_user"] = config.RunAsUser
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

// supersedeStaleSupervisorIfRequested is called inside the advisory-locked
// transaction for supervise.start. When replace=true and a stale active
// supervisor exists for the session, it is superseded (marked lost) so the
// subsequent INSERT does not collide with the unique partial index
// uq_active_daemon_supervisor_pointer_per_session. When replace=false and a
// stale supervisor exists, a clean actionable error is returned directing the
// operator to retry with --replace. When no stale supervisor exists in either
// case the call is a no-op.
func supersedeStaleSupervisorIfRequested(ctx context.Context, runner db.TxRunner, repositoryID, sessionID string, replace bool, now string) error {
	var supervisorID, runID, state string
	err := runner.QueryRow(ctx, `
		SELECT supervisor_id, run_id, state
		  FROM striatumd.process_supervisors
		 WHERE repository_id = $1 AND session_id = $2
		   AND state = ANY($3)
		 ORDER BY started_at DESC, supervisor_id DESC
		 LIMIT 1
		 FOR UPDATE`,
		repositoryID, sessionID, []string{"starting", "attached", "detached"},
	).Scan(&supervisorID, &runID, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		// No active supervisor — proceed with INSERT.
		return nil
	}
	if err != nil {
		return err
	}
	// A stale supervisor exists.
	if !replace {
		return rpc.NewError("invalid_transition", fmt.Sprintf(
			"session already has an active supervisor: %s (state=%s); retry with --replace to supersede it",
			supervisorID, state,
		), nil)
	}
	// replace=true: supersede the stale supervisor by marking it lost so the
	// unique partial index allows the incoming INSERT.
	reason := "superseded by supervise.start --replace"
	return markSupervisorLostInTx(ctx, runner, repositoryID, supervisorID, runID, sessionID, reason, 0, map[string]any{"superseded_at": now})
}

// insertStartingsSupervisorRowsWithCleanError is a wrapper around
// insertStartingSupervisorRows that detects a Postgres unique-constraint
// violation (SQLSTATE 23505) on the process_supervisor_pointers partial index
// and converts it into an actionable rpc.Error instead of surfacing the raw
// database error to the operator. This guards the narrow race window between
// the advisory-locked SELECT and the INSERT.
func insertStartingsSupervisorRowsWithCleanError(ctx context.Context, runner db.TxRunner, repositoryID string, config supervisionStartConfig, supervisorID, daemonSupervisorID, scratch, pipePath, eventPath, startedAt, sessionID string) error {
	err := insertStartingSupervisorRows(ctx, runner, repositoryID, config, supervisorID, daemonSupervisorID, scratch, pipePath, eventPath, startedAt)
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return rpc.NewError("invalid_transition", fmt.Sprintf(
			"session already has an active supervisor for session_id=%q; retry with --replace to supersede it",
			sessionID,
		), nil)
	}
	return err
}

func requireActiveControlSupervisor(ctx context.Context, runner any, repositoryID, sessionID string, forUpdate bool) (supervisorControlRow, error) {
	suffix := ""
	if forUpdate {
		suffix = " FOR UPDATE OF ps"
	}
	sql := `
		SELECT ps.supervisor_id, ps.run_id, ps.session_id, ps.state,
		       COALESCE(ps.scratch_path, ''), COALESCE(ps.stdin_pipe_path, ''), ps.pid, COALESCE(ps.pid_start_time, ''),
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
		&row.ScratchPath, &row.StdinPipePath, &pid, &row.PIDStartTime,
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
	var metadata any
	err := rower.QueryRow(ctx, `
		SELECT ps.supervisor_id, ps.run_id, ps.session_id, ps.state,
		       COALESCE(ps.scratch_path, ''), COALESCE(ps.stdin_pipe_path, ''), ps.pid, COALESCE(ps.pid_start_time, ''),
		       COALESCE(p.daemon_supervisor_id, ''), COALESCE(p.metadata_json, '{}'::jsonb),
		       ps.ended_at, ps.stop_reason
		  FROM striatumd.process_supervisors ps
		  LEFT JOIN striatumd.process_supervisor_pointers p
		    ON p.repository_id = ps.repository_id AND p.supervisor_id = ps.supervisor_id
		 WHERE ps.repository_id = $1 AND ps.session_id = $2
		   AND ps.state = ANY($3)
		 ORDER BY ps.started_at DESC, ps.supervisor_id DESC
		 LIMIT 1`,
		repositoryID, sessionID, []string{"lost", "stopped"},
	).Scan(
		&row.SupervisorID, &row.RunID, &row.SessionID, &row.State,
		&row.ScratchPath, &row.StdinPipePath, &pid, &row.PIDStartTime,
		&row.DaemonSupervisorID, &metadata, &row.EndedAt, &row.StopReason,
	)
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
	row.Metadata = asMap(metadata)
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
	// 1. Transactional FOR UPDATE locks inside the mutation block prior to checker call
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
	if daemonSupervisorID != nil && *daemonSupervisorID != "" {
		var daemonState string
		if err := runner.QueryRow(ctx, `
			SELECT state
			  FROM striatumd.daemon_supervisors
			 WHERE repository_id = $1 AND daemon_supervisor_id = $2
			 FOR UPDATE`,
			repositoryID, *daemonSupervisorID,
		).Scan(&daemonState); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}

	// 2. Checker call
	checker := lanehealth.Checker{
		Probe: lanehealth.ProdProbe{Runner: supervisionTmuxRunner},
	}
	health, err := checker.Check(ctx, runner, repositoryID, supervisor.SessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return rpc.NewError("invalid_transition", "supervisor requires operator reconciliation before delivery: pointer_missing", nil)
		}
		return err
	}

	if !supervisor.HasPID || health.Reason == lanehealth.ReasonPIDMissing {
		if err := markSupervisorLostInTx(ctx, runner, repositoryID, supervisor.SupervisorID, supervisor.RunID, supervisor.SessionID, "pid_missing observed by "+phase, 0, nil); err != nil {
			return err
		}
		return rpc.NewError("invalid_transition", "supervisor cannot accept delivery: pid_missing", nil)
	}

	// #63 F10: key the delivery gate purely on the reconciled LIVE probe
	// (health.Deliverable), not on stale supervisor metadata. The lanehealth
	// checker already reconciles the benign #63 F7 case — an exited tmux
	// attach-session OBSERVER client whose pane is alive and whose real
	// transport (persistent FIFO / pty helper) is healthy stays
	// health.Deliverable == true — while genuine transport failures
	// (helper_process_gone, stdin_reader_missing) keep health.Deliverable
	// false. Previously this gate ALSO required supervisorDeliveryDegraded to
	// observe a delivery_liveness metadata record, so a helper/transport that
	// died abruptly WITHOUT writing that record (main lane PID still alive)
	// slipped the gate and dispatched a packet to a dead FIFO. We now reject
	// whenever the live probe is not deliverable. supervisorDeliveryDegraded
	// survives only as a fallback reason source for the rare case where the
	// probe is non-deliverable but did not carry a DeliveryReason string.
	if !health.Deliverable {
		reason := health.DeliveryReason
		if reason == "" {
			if metaReason, _ := supervisorDeliveryDegraded(supervisor.Metadata); metaReason != "" {
				reason = metaReason
			}
		}
		if reason == "" {
			reason = "not_deliverable"
		}
		return rpc.NewError("invalid_transition", "supervisor delivery is degraded: "+reason, nil)
	}

	if !health.Alive {
		reason := health.LivenessClass
		if reason == "" {
			reason = "pid_gone"
		}
		live := gosupervisor.ProbeLaneLiveness(ctx, tmuxRunnerForSupervisorMetadata(supervisor.Metadata), supervisor.Metadata, supervisor.PID, supervisor.PIDStartTime)
		if reason == string(gosupervisor.TmuxLivenessUnavailable) {
			count := tmuxUnavailableCount(supervisor.Metadata) + 1
			metadata := tmuxProbeDegradedMetadata(supervisor.Metadata, live, count)
			if count >= gosupervisor.TmuxUnavailableLostThreshold() {
				payload := map[string]any{"phase": phase, "tmux_liveness": live.Class, "probe_unavailable_count": count}
				if live.Tmux != nil && live.Tmux.Failure != nil {
					payload["probe_failure"] = gosupervisor.TmuxProbeFailurePayload(*live.Tmux.Failure)
				}
				if err := markSupervisorLostInTx(ctx, runner, repositoryID, supervisor.SupervisorID, supervisor.RunID, supervisor.SessionID, "tmux_unavailable_persistent", supervisor.PID, payload); err != nil {
					return err
				}
				return rpc.NewError("invalid_transition", "supervisor cannot accept delivery: tmux_unavailable_persistent", nil)
			}
			if err := replacePointerMetadata(ctx, runner, repositoryID, supervisor.SupervisorID, metadata); err != nil {
				return err
			}
			return rpc.NewError("invalid_transition", "supervisor liveness is degraded: tmux_unavailable; "+live.Detail, nil)
		}

		lostPayload := map[string]any{"phase": phase, "reattach_reason": reason}
		if strings.HasPrefix(reason, "tmux_") {
			lostPayload["tmux_liveness"] = reason
			if live.Tmux != nil && live.Tmux.Failure != nil {
				lostPayload["probe_failure"] = gosupervisor.TmuxProbeFailurePayload(*live.Tmux.Failure)
			}
		}
		if err := markSupervisorLostInTx(ctx, runner, repositoryID, supervisor.SupervisorID, supervisor.RunID, supervisor.SessionID, reason, supervisor.PID, lostPayload); err != nil {
			return err
		}
		if strings.HasPrefix(reason, "tmux_") {
			return rpc.NewError("invalid_transition", "supervisor cannot accept delivery: "+reason, nil)
		}
		return rpc.NewError("invalid_transition", fmt.Sprintf("supervisor pid is gone: %s", supervisor.SupervisorID), nil)
	}

	// 3. Structural checks from checker results
	if health.Reason == lanehealth.ReasonPointerStateMismatch {
		return rpc.NewError("invalid_transition", "supervisor requires operator reconciliation before delivery: pointer_state_mismatch", nil)
	}
	if health.Reason == lanehealth.ReasonDaemonStateMismatch {
		return rpc.NewError("invalid_transition", "supervisor requires operator reconciliation before delivery: daemon_state_mismatch", nil)
	}
	if health.Reason == lanehealth.ReasonDaemonSupervisorMissing {
		return rpc.NewError("invalid_transition", "supervisor requires operator reconciliation before delivery: daemon_supervisor_missing", nil)
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

func tmuxUnavailableCount(metadata map[string]any) int {
	tmux := asMap(metadata["tmux"])
	count, ok := intValueOptional(tmux["probe_unavailable_count"])
	if !ok || count < 0 {
		return 0
	}
	return count
}

func tmuxProbeDegradedMetadata(metadata map[string]any, live gosupervisor.LaneLiveness, count int) map[string]any {
	updated := copyMap(metadata)
	tmux := asMap(updated["tmux"])
	if len(tmux) == 0 {
		tmux = map[string]any{}
	}
	tmux["liveness_state"] = "degraded"
	tmux["probe_skipped_at"] = nowString()
	tmux["probe_unavailable_count"] = count
	if live.Detail != "" {
		tmux["last_unavailable_detail"] = live.Detail
	}
	if live.Tmux != nil {
		tmux["liveness"] = gosupervisor.TmuxLivenessPayload(*live.Tmux)
	}
	updated["tmux"] = tmux
	return updated
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

func replacePointerMetadata(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID string, metadata map[string]any) error {
	metadataArg, err := db.JSONBArg(runner, metadata)
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
	// Issue #62: any terminal supervisor transition — supervise.stop / tmux kill,
	// signal, lost, failed — must remove the per-launch .gemini/settings.json
	// (rotating MCP bearer token) the lane wrote. Centralizing this at the
	// state-transition choke point keeps teardown cleanup path-independent.
	// CleanupGeminiSettings is idempotent (no-ops once its scratch markers are
	// gone), so a redundant call from another path is harmless.
	if supervisorTerminalStates[state] {
		cleanupSupervisorLaneMCPConfig(ctx, runner, repositoryID, supervisorID)
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

// probeLaneLivenessAtStart probes a just-launched lane's pane/process liveness
// on the supervise-start failure path. Overridable in tests.
var probeLaneLivenessAtStart = func(ctx context.Context, metadata map[string]any, pid int, startToken string) gosupervisor.LaneLiveness {
	return gosupervisor.ProbeLaneLiveness(ctx, tmuxRunnerForSupervisorMetadata(metadata), metadata, pid, startToken)
}

// failedAttachOutcome classifies a supervise-start attach failure (#201). When
// the daemon could not confirm the launched agent PID, the lane may genuinely
// have died OR its pane/process may be alive while only the daemon's attach leg
// failed — the common sandboxed-lane case (a helper running as the lane OS user
// that cannot reach the daemon socket to attest). A provably-alive pane is
// recorded detached (recoverable via rebridge), never lost, with an accurate
// message; otherwise the genuine "child exited before attach" path stands.
func failedAttachOutcome(paneAlive bool) (state, reason, message string) {
	if paneAlive {
		return "detached", "attach_failed_lane_alive",
			"supervisor could not attach the lane, but its pane/process is alive; the lane likely cannot reach the daemon to attest. Rebridge the supervisor and verify lane-user provisioning (HOME, provider auth, repo ACLs) — see docs/how-to/lane-sandbox.md"
	}
	return "lost", "child exited before attach", "supervisor child exited before it could be attached"
}

// markSupervisorDetachedAfterFailedAttach records a launched-but-unattached
// supervisor whose pane is alive as detached (#201). Unlike the lost path it
// does NOT mark the session terminal: the lane is alive, so the session stays
// recoverable and an operator can rebridge.
func markSupervisorDetachedAfterFailedAttach(ctx context.Context, runner db.Runner, repositoryID, supervisorID, daemonSupervisorID, runID, sessionID, reason string, pid int, pidStartTime string, metadata, payload map[string]any) error {
	_, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		if len(metadata) > 0 {
			if mergeErr := mergePointerMetadata(ctx, tx, repositoryID, supervisorID, metadata); mergeErr != nil {
				if payload == nil {
					payload = map[string]any{}
				}
				payload["metadata_persist_error"] = mergeErr.Error()
			}
		}
		now := nowString()
		if err := updateSupervisorState(ctx, tx, repositoryID, supervisorID, daemonSupervisorID, "detached", now, pid, pidStartTime, now, nil, nil); err != nil {
			return nil, err
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
		_, err := appendEvent(ctx, tx, repositoryID, runID, "supervisor.detached", sessionID, nil, nil, nil, nil, eventPayload)
		return nil, err
	})
	return err
}

func markSupervisorLostWithMetadata(ctx context.Context, runner db.Runner, repositoryID, supervisorID, runID, sessionID, reason string, pid int, metadata map[string]any, payload map[string]any) error {
	_, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		if len(metadata) > 0 {
			if mergeErr := mergePointerMetadata(ctx, tx, repositoryID, supervisorID, metadata); mergeErr != nil {
				if payload == nil {
					payload = map[string]any{}
				}
				payload["metadata_persist_error"] = mergeErr.Error()
			}
		}
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
	if err := markActiveSessionTerminal(ctx, runner, activeSessionTerminalUpdate{
		RepositoryID: repositoryID,
		SessionID:    sessionID,
		State:        "lost",
		Reason:       "supervisor lost: " + reason,
	}); err != nil {
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

func currentDaemonInstanceID() string {
	if value := os.Getenv("STRIATUM_DAEMON_INSTANCE_ID"); value != "" {
		return value
	}
	return "go-pg-handler"
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
