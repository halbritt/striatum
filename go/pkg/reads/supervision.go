package reads

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/halbritt/striatum/go/pkg/agentloop"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/lanehealth"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
)

const defaultSupervisorStallAfterSeconds = 300
const defaultSupervisorTrajectoryTailLines = 200
const supervisorTrajectoryLogFilename = "pty.log"

// DrainHelperEventsHook is registered by the mutations package at startup.
// It allows read-layer projections to lazily drain PTY helper events without
// introducing package circular dependencies.
var DrainHelperEventsHook func(ctx context.Context, runner db.TxRunner, repositoryID string, supervisorID string) error

func beginHelperEventDrainTx(ctx context.Context, runner db.Runner) (db.TxRunner, error) {
	return db.BeginAuthorizedMutation(ctx, runner, db.AuthorityFromContext(ctx))
}

var supervisorTerminalStates = map[string]bool{
	"lost":    true,
	"stopped": true,
}

var supervisorActiveStatesRead = map[string]bool{
	"starting": true,
	"attached": true,
	"detached": true,
}

var superviseTmuxRunner = gosupervisor.DefaultTmuxRunner()

func SetTmuxRunnerForTest(runner gosupervisor.TmuxRunner) func() {
	previous := superviseTmuxRunner
	if runner == nil {
		superviseTmuxRunner = gosupervisor.DefaultTmuxRunner()
	} else {
		superviseTmuxRunner = runner
	}
	return func() {
		superviseTmuxRunner = previous
	}
}

// HandleSuperviseStatus returns the read projection for supervise.status. It
// deliberately does not drain helper files, reattach rows, or mark missing PIDs
// lost; those are mutation surfaces.
func HandleSuperviseStatus(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredTextParam(envelope, "session_id", "supervise.status requires session_id")
	if err != nil {
		return nil, err
	}
	exists, err := rowExists(ctx, runner,
		`SELECT session_id FROM striatumd.sessions
		  WHERE repository_id = $1 AND session_id = $2
		  LIMIT 1`,
		repositoryID, sessionID,
	)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, rpc.NewError("not_found", "session not found: "+sessionID, nil)
	}
	rows, err := collectRows(ctx, runner,
		`SELECT ps.*, p.metadata_json AS pointer_metadata_json
		   FROM striatumd.process_supervisors ps
		   LEFT JOIN striatumd.process_supervisor_pointers p
		     ON p.repository_id = ps.repository_id
		    AND p.supervisor_id = ps.supervisor_id
		  WHERE ps.repository_id = $1 AND ps.session_id = $2
		  ORDER BY ps.started_at DESC, ps.supervisor_id DESC
		  LIMIT 1`,
		repositoryID, sessionID,
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, rpc.NewError("not_found", fmt.Sprintf("no supervisor recorded for session_id=%q", sessionID), nil)
	}

	supervisorID := stringFrom(rows[0], "supervisor_id")
	if supervisorID != "" && DrainHelperEventsHook != nil {
		if tx, err := beginHelperEventDrainTx(ctx, runner); err == nil {
			_ = DrainHelperEventsHook(ctx, tx, repositoryID, supervisorID)
			_ = tx.Commit(ctx)
			// Re-fetch rows so that rows[0]["pointer_metadata_json"] reflects the
			// drained pointer metadata. #180/#200: assigning the re-fetch result
			// unconditionally clobbered the previously good `rows` with an empty
			// slice whenever the re-fetch errored or returned no rows (e.g. the
			// supervisor row was concurrently removed, or the daemon is stopping and
			// rotating the MCP port out from under the read), so the rows[0] index
			// below panicked with "index out of range [0] with length 0". Mirror the
			// reattachStatusRows pattern: only adopt the re-fetch when it succeeded
			// and returned a row; otherwise keep the original rows.
			if refetched, err := collectRows(ctx, runner,
				`SELECT ps.*, p.metadata_json AS pointer_metadata_json
				   FROM striatumd.process_supervisors ps
				   LEFT JOIN striatumd.process_supervisor_pointers p
				     ON p.repository_id = ps.repository_id
				    AND p.supervisor_id = ps.supervisor_id
				  WHERE ps.repository_id = $1 AND ps.session_id = $2
				  ORDER BY ps.started_at DESC, ps.supervisor_id DESC
				  LIMIT 1`,
				repositoryID, sessionID,
			); err == nil && len(refetched) > 0 {
				rows = refetched
			}
		}
	}

	supervisor := supervisorView(rows[0])
	protocolLiveness, err := sessionProtocolLiveness(ctx, runner, repositoryID, sessionID)
	if err != nil {
		return nil, err
	}
	supervisor["protocol_liveness"] = protocolLiveness
	state := superviseString(supervisor["state"])
	pid, hasPID := intValueOptional(supervisor["pid"])
	pointerMetadata := superviseObject(rows[0]["pointer_metadata_json"])
	liveness := "gone"
	agentPIDAlive := false
	var progress map[string]any
	if hasPID && supervisorActiveStatesRead[state] {
		live := gosupervisor.ProbeLaneLiveness(ctx, superviseTmuxRunnerForMetadata(pointerMetadata), pointerMetadata, pid, superviseString(supervisor["pid_start_time"]))
		attachTmuxLiveness(supervisor, live)
		if live.Backed == "plain_pty" {
			pidIdentityReason := live.Class
			if live.Alive {
				pidIdentityReason = ""
			}
			supervisor["current_pid_start_time"] = nullableText(live.Detail)
			supervisor["pid_identity"] = pidIdentityFromReason(live.Alive, pidIdentityReason)
		}
		if live.Alive {
			liveness = "alive"
			agentPIDAlive = true
			progress, err = supervisorProgressForSession(ctx, runner, repositoryID, sessionID, defaultSupervisorStallAfterSeconds)
			if err != nil {
				return nil, err
			}
			if progress != nil && boolValue(progress["stalled"]) {
				liveness = "stalled"
			}
		} else {
			if state == "attached" {
				if tx, txErr := runner.BeginTx(ctx); txErr == nil {
					now := nowString()
					stopReason := "unexpected exit (probed gone)"
					daemonSupervisorID := superviseString(supervisor["daemon_supervisor_id"])
					_ = updateSupervisorState(ctx, tx, repositoryID, supervisorID, daemonSupervisorID, "stopped", now, 0, "", "", &now, &stopReason)
					_ = tx.Commit(ctx)
					supervisor["state"] = "stopped"
					supervisor["ended_at"] = now
					supervisor["stop_reason"] = stopReason
				}
			}
		}
	} else if hasPID && state == "stopped" {
		alive, currentStart, reason := pidLiveWithStartToken(pid, superviseString(supervisor["pid_start_time"]))
		supervisor["current_pid_start_time"] = nullableText(currentStart)
		supervisor["pid_identity"] = pidIdentityFromReason(alive, reason)
		if alive {
			liveness = "alive"
			agentPIDAlive = true
		}
	}
	supervisor["liveness"] = liveness
	// #147 Symptom A: the single `liveness` field conflates three independent
	// signals, so an operator cannot tell a detached/stopped supervisor bridge
	// whose agent child is still alive (the false-`gone` case) from a genuinely
	// dead lane. Expose them distinctly: agent_pid_alive is the probed liveness of
	// the supervised process; supervisor_state is the bridge state (which may have
	// just been reconciled to `stopped` above); lease_fresh is whether the work
	// lease is still live.
	supervisor["agent_pid_alive"] = agentPIDAlive
	supervisor["supervisor_state"] = superviseString(supervisor["state"])
	supervisor["lease_fresh"] = progress != nil && progress["lease_id"] != nil && !boolValue(progress["lease_expired"])
	if progress != nil {
		supervisor["active_lease_id"] = progress["lease_id"]
		supervisor["active_lease_expires_at"] = progress["lease_expires_at"]
		supervisor["active_lease_last_heartbeat_at"] = progress["lease_last_heartbeat_at"]
		supervisor["last_progress_at"] = progress["last_progress_at"]
		supervisor["last_progress_age_seconds"] = progress["last_progress_age_seconds"]
		supervisor["stall_after_seconds"] = progress["stall_after_seconds"]
		supervisor["lease_expired"] = progress["lease_expired"]
	}
	escalationReport, err := latestSessionEscalationReport(ctx, runner, repositoryID, superviseString(supervisor["run_id"]), sessionID)
	if err != nil {
		return nil, err
	}
	if escalationReport != nil {
		supervisor["latest_escalation_report"] = escalationReport
		if liveness != "alive" {
			supervisor["liveness_reason"] = "escalated"
		}
	}

	reattachRows, err := reattachStatusRows(ctx, runner, repositoryID, "", "", superviseString(supervisor["supervisor_id"]))
	if err != nil {
		return nil, err
	}
	reattachState := ""
	reattachReason := ""
	if len(reattachRows) > 0 {
		reattachView := reattachStatusView(ctx, reattachRows[0])
		reattachState = superviseString(reattachView["reattach_state"])
		reattachReason = superviseString(reattachView["reattach_reason"])
	}
	// Call unified checker to resolve lane attestation
	checker := lanehealth.Checker{
		Probe: lanehealth.ProdProbe{Runner: superviseTmuxRunner},
	}
	health, err := checker.Check(ctx, runner, repositoryID, sessionID)
	if err == nil {
		legMap := lanehealth.LegacyMap(health)
		supervisor["lane_attestation"] = legMap["state"]
		supervisor["lane_attestation_reason"] = legMap["reason"]
		if !health.Deliverable && health.DeliveryReason == "helper_process_gone" {
			delivery := map[string]any{
				"healthy":     false,
				"reason":      health.DeliveryReason,
				"class":       health.DeliveryReason,
				"remediation": deliveryRemediation(health.DeliveryReason, sessionID),
			}
			supervisor["delivery_liveness"] = delivery
			supervisor["delivery_state"] = health.DeliveryReason
		} else {
			reconcileBenignAttachExit(supervisor, health)
		}
	} else {
		supervisor["lane_attestation"] = "unattested"
		supervisor["lane_attestation_reason"] = "no_attached_supervisor"
	}

	if reattachState == "needs_repair" || reattachState == "needs_verification" {
		supervisor["lane_attestation"] = "unattested"
		supervisor["lane_attestation_reason"] = nullableText(reattachReason)
	} else if supervisor["lane_attestation"] == "unattested" {
		reason := superviseString(supervisor["lane_attestation_reason"])
		if reason == "pid_missing" || reason == "no_attached_supervisor" || reason == "" {
			supervisor["lane_attestation_reason"] = "no_live_attached_supervisor"
		}
	}

	return supervisor, nil
}

func sessionProtocolLiveness(ctx context.Context, runner db.Runner, repositoryID string, sessionID string) (map[string]any, error) {
	rows, err := collectRows(ctx, runner,
		`SELECT s.session_id, s.state, s.registered_at,
		        s.last_mcp_request_at,
		        s.last_tools_list_at,
		        s.last_await_packet_at,
		        s.last_packet_delivered_at,
		        s.last_ack_at,
		        s.last_work_block_at,
		        s.last_work_release_at,
		        s.last_work_complete_at,
		        s.last_work_heartbeat_at,
		        s.last_session_ready_at,
		        s.last_session_heartbeat_at,
		        s.last_session_question_at,
		        s.last_session_escalate_at,
		        s.last_pty_activity_at,
		        `+db.SessionPipeReadProjection(ctx, runner, "s")+`,
		        s.last_tool_call_started_at,
		        s.last_tool_call_finished_at,
		        s.liveness_stall_class,
		        s.liveness_stall_since,
		        active_lease.lease_id AS active_lease_id,
		        active_lease.acquired_at AS active_lease_acquired_at,
		        active_lease.expires_at AS active_lease_expires_at,
		        active_lease.last_heartbeat_at AS active_lease_last_heartbeat_at
		   FROM striatumd.sessions s
		   LEFT JOIN LATERAL (
		     SELECT l.lease_id, l.acquired_at, l.expires_at, l.last_heartbeat_at
		       FROM striatumd.leases l
		      WHERE l.repository_id = s.repository_id
		        AND l.owner_session_id = s.session_id
		        AND l.state = 'active'
		      ORDER BY l.acquired_at DESC, l.lease_id DESC
		      LIMIT 1
		   ) active_lease ON true
		  WHERE s.repository_id = $1 AND s.session_id = $2
		  LIMIT 1`,
		repositoryID,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return map[string]any{}, nil
	}
	return sessionliveness.ProjectionFromRow(rows[0], time.Now().UTC()), nil
}

// HandleSuperviseList returns process supervisor rows for a run, optionally
// filtered by state. It is a PostgreSQL-only read projection.
func HandleSuperviseList(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID, err := requiredTextParam(envelope, "run_id", "supervise.list requires run_id")
	if err != nil {
		return nil, err
	}
	state, err := optionalTextParam(envelope, "state")
	if err != nil {
		return nil, err
	}
	exists, err := rowExists(ctx, runner,
		`SELECT run_id FROM striatumd.runs
		  WHERE repository_id = $1 AND run_id = $2
		  LIMIT 1`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, rpc.NewError("not_found", "run not found: "+runID, nil)
	}

	args := []any{repositoryID, runID}
	where := "WHERE ps.repository_id = $1 AND ps.run_id = $2"
	if state != "" {
		args = append(args, state)
		where += " AND ps.state = $3"
	}
	rows, err := collectRows(ctx, runner,
		`SELECT ps.*, p.metadata_json AS pointer_metadata_json
		   FROM striatumd.process_supervisors ps
		   LEFT JOIN striatumd.process_supervisor_pointers p
		     ON p.repository_id = ps.repository_id
		    AND p.supervisor_id = ps.supervisor_id
		  `+where+`
		  ORDER BY ps.started_at DESC, ps.supervisor_id DESC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	supervisors := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		supervisors = append(supervisors, supervisorView(row))
	}
	return map[string]any{
		"run_id":      runID,
		"state":       nullableText(state),
		"supervisors": supervisors,
	}, nil
}

// HandleSuperviseTrajectory returns the operator-local PTY diagnostic log for
// the latest supervisor attached to a session. This is intentionally an
// explicit read verb: ordinary status/list projections expose only log metadata
// and never include terminal bytes.
func HandleSuperviseTrajectory(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredTextParam(envelope, "session_id", "supervise.trajectory requires session_id")
	if err != nil {
		return nil, err
	}
	tail, err := optionalBoolParam(envelope, "tail")
	if err != nil {
		return nil, err
	}
	tailLines, err := optionalNonNegativeIntParam(envelope, "tail_lines")
	if err != nil {
		return nil, err
	}
	if tail && tailLines == 0 {
		tailLines = defaultSupervisorTrajectoryTailLines
	}

	rows, err := collectRows(ctx, runner,
		`SELECT ps.*, p.metadata_json AS pointer_metadata_json, repo.repo_root
		   FROM striatumd.process_supervisors ps
		   JOIN striatumd.repositories repo
		     ON repo.repository_id = ps.repository_id
		   LEFT JOIN striatumd.process_supervisor_pointers p
		     ON p.repository_id = ps.repository_id
		    AND p.supervisor_id = ps.supervisor_id
		  WHERE ps.repository_id = $1 AND ps.session_id = $2
		  ORDER BY ps.started_at DESC, ps.supervisor_id DESC
		  LIMIT 1`,
		repositoryID, sessionID,
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, rpc.NewError("not_found", fmt.Sprintf("no supervisor recorded for session_id=%q", sessionID), nil)
	}

	row := rows[0]
	path, pathErr := trajectoryLogPathForRow(row)
	info := trajectoryLogMetadataForPath(path, superviseString(row["supervisor_id"]), superviseString(superviseObject(row["pointer_metadata_json"])["agent_loop_mode"]))
	result := map[string]any{
		"repository_id":  repositoryID,
		"run_id":         row["run_id"],
		"session_id":     sessionID,
		"supervisor_id":  row["supervisor_id"],
		"trajectory_log": info,
		"tail_lines":     nullableInt(tailLines),
		"truncated":      false,
		"content":        "",
	}
	if pathErr != nil {
		info["status"] = "invalid_path"
		info["error"] = pathErr.Error()
		return result, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			info["status"] = "missing"
			info["exists"] = false
			return result, nil
		}
		info["status"] = "unreadable"
		info["exists"] = true
		info["error"] = err.Error()
		return result, nil
	}
	info["status"] = "available"
	info["exists"] = true
	info["size_bytes"] = len(data)
	if tailLines > 0 {
		data, result["truncated"] = tailBytesByLines(data, tailLines)
	}
	result["content"] = string(data)
	return result, nil
}

// HandleSuperviseReattachStatus returns the daemon reattach health DTO
// without repairing pointer rows or writing events.
func HandleSuperviseReattachStatus(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID, err := optionalTextParam(envelope, "run_id")
	if err != nil {
		return nil, err
	}
	sessionID, err := optionalTextParam(envelope, "session_id")
	if err != nil {
		return nil, err
	}
	supervisorID, err := optionalTextParam(envelope, "supervisor_id")
	if err != nil {
		return nil, err
	}
	if runID != "" {
		exists, err := rowExists(ctx, runner,
			`SELECT run_id FROM striatumd.runs
			  WHERE repository_id = $1 AND run_id = $2
			  LIMIT 1`,
			repositoryID, runID,
		)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, rpc.NewError("not_found", "run not found: "+runID, nil)
		}
	}
	if sessionID != "" {
		exists, err := rowExists(ctx, runner,
			`SELECT session_id FROM striatumd.sessions
			  WHERE repository_id = $1 AND session_id = $2
			  LIMIT 1`,
			repositoryID, sessionID,
		)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, rpc.NewError("not_found", "session not found: "+sessionID, nil)
		}
	}

	rows, err := reattachStatusRows(ctx, runner, repositoryID, runID, sessionID, supervisorID)
	if err != nil {
		return nil, err
	}
	if supervisorID != "" && len(rows) == 0 {
		return nil, rpc.NewError("not_found", fmt.Sprintf("no supervisor recorded for supervisor_id=%q", supervisorID), nil)
	}
	supervisors := make([]map[string]any, 0, len(rows))
	summary := map[string]int{
		"total":              0,
		"reattachable":       0,
		"lost_candidate":     0,
		"needs_repair":       0,
		"needs_verification": 0,
		"terminal":           0,
	}
	for _, row := range rows {
		view := reattachStatusView(ctx, row)
		supervisors = append(supervisors, view)
		summary["total"]++
		state := superviseString(view["reattach_state"])
		summary[state] = summary[state] + 1
	}
	return map[string]any{
		"run_id":        nullableText(runID),
		"session_id":    nullableText(sessionID),
		"supervisor_id": nullableText(supervisorID),
		"summary":       summary,
		"supervisors":   supervisors,
	}, nil
}

type reattachStatusRowsOptions struct {
	nonTerminalRunsOnly bool
}

func reattachStatusRows(ctx context.Context, runner db.Runner, repositoryID, runID, sessionID, supervisorID string) ([]map[string]any, error) {
	return reattachStatusRowsWithOptions(ctx, runner, repositoryID, runID, sessionID, supervisorID, reattachStatusRowsOptions{})
}

func reattachStatusRowsWithOptions(ctx context.Context, runner db.Runner, repositoryID, runID, sessionID, supervisorID string, opts reattachStatusRowsOptions) ([]map[string]any, error) {
	args := []any{repositoryID}
	where := "ps.repository_id = $1"
	runJoin := ""
	if opts.nonTerminalRunsOnly {
		runJoin = `JOIN striatumd.runs r
		     ON r.repository_id = ps.repository_id
		    AND r.run_id = ps.run_id`
		where += " AND r.state NOT IN ('completed','failed','canceled')"
	}
	if runID != "" {
		args = append(args, runID)
		where += " AND ps.run_id = $" + strconv.Itoa(len(args))
	}
	if sessionID != "" {
		args = append(args, sessionID)
		where += " AND ps.session_id = $" + strconv.Itoa(len(args))
	}
	if supervisorID != "" {
		args = append(args, supervisorID)
		where += " AND ps.supervisor_id = $" + strconv.Itoa(len(args))
	}
	rows, err := collectRows(ctx, runner,
		`SELECT
		      ps.supervisor_id,
		      ps.run_id,
		      ps.session_id,
		      ps.adapter,
		      ps.cwd,
		      ps.scratch_path,
		      ps.stdin_pipe_path,
		      ps.pid,
		      ps.pid_start_time,
		      ps.state,
		      ps.started_at,
		      ps.heartbeat_at,
		      ps.ended_at,
		      ps.stop_reason,
		      p.daemon_supervisor_id AS pointer_daemon_supervisor_id,
		      p.pid AS pointer_pid,
		      p.pid_start_time AS pointer_pid_start_time,
		      p.state AS pointer_state,
		      p.updated_at AS pointer_updated_at,
		      p.metadata_json AS pointer_metadata_json,
		      ds.daemon_supervisor_id AS daemon_supervisor_id,
		      ds.daemon_instance_id,
		      ds.pid AS daemon_pid,
		      ds.pid_start_time AS daemon_pid_start_time,
		      ds.state AS daemon_state,
		      ds.heartbeat_at AS daemon_heartbeat_at,
		      ds.ended_at AS daemon_ended_at,
		      ds.stop_reason AS daemon_stop_reason
		   FROM striatumd.process_supervisors ps
		   `+runJoin+`
		   LEFT JOIN striatumd.process_supervisor_pointers p
		     ON p.repository_id = ps.repository_id
		    AND p.supervisor_id = ps.supervisor_id
		   LEFT JOIN striatumd.daemon_supervisors ds
		     ON ds.repository_id = ps.repository_id
		    AND ds.daemon_supervisor_id = p.daemon_supervisor_id
		  WHERE `+where+`
		  ORDER BY ps.started_at DESC, ps.supervisor_id DESC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	if DrainHelperEventsHook != nil {
		for _, row := range rows {
			supID := superviseString(row["supervisor_id"])
			if supID != "" {
				if tx, err := beginHelperEventDrainTx(ctx, runner); err == nil {
					_ = DrainHelperEventsHook(ctx, tx, repositoryID, supID)
					_ = tx.Commit(ctx)
					metaRows, err := collectRows(ctx, runner,
						`SELECT metadata_json FROM striatumd.process_supervisor_pointers
						  WHERE repository_id = $1 AND supervisor_id = $2`,
						repositoryID, supID,
					)
					if err == nil && len(metaRows) > 0 {
						row["pointer_metadata_json"] = metaRows[0]["metadata_json"]
					}
				}
			}
		}
	}
	return rows, nil
}

func reattachStatusView(ctx context.Context, row map[string]any) map[string]any {
	pid, hasPID := intValueOptional(row["pid"])
	pointerMetadata := superviseObject(row["pointer_metadata_json"])
	live := gosupervisor.ProbeLaneLiveness(ctx, superviseTmuxRunnerForMetadata(pointerMetadata), pointerMetadata, pid, superviseString(row["pid_start_time"]))
	pidAlive := hasPID && live.Alive
	currentStart := ""
	if live.Backed == "tmux" && live.Tmux != nil && live.Tmux.ObservedStartTok != "" {
		currentStart = live.Tmux.ObservedStartTok
	} else if live.Backed == "plain_pty" && pidAlive {
		currentStart, _ = processStartToken(pid)
	} else if live.Backed == "plain_pty" && live.Detail != "" {
		currentStart = live.Detail
	}
	state, reason, action := reattachState(row, pidAlive, currentStart, live)
	remediation := ""
	if live.Backed == "tmux" {
		remediation = tmuxLivenessRemediation(live.Class, live.Detail, superviseString(row["session_id"]))
	}
	var deliveryLiveness map[string]any
	if tmux := superviseObject(pointerMetadata["tmux"]); len(tmux) > 0 {
		deliveryLiveness = superviseObject(tmux["delivery_liveness"])
	}
	if len(deliveryLiveness) == 0 {
		deliveryLiveness = superviseObject(pointerMetadata["delivery_liveness"])
	}
	helperPID, hasHelperPID := intValueOptional(pointerMetadata["helper_pid"])
	helperStart := superviseString(pointerMetadata["helper_pid_start_time"])
	if hasHelperPID && helperPID > 0 && !lanehealth.IsHelperAlive(helperPID, helperStart) {
		deliveryLiveness = map[string]any{
			"healthy":     false,
			"reason":      "helper_process_gone",
			"class":       "helper_process_gone",
			"remediation": deliveryRemediation("helper_process_gone", superviseString(row["session_id"])),
		}
	}
	return map[string]any{
		"supervisor_id":          row["supervisor_id"],
		"run_id":                 row["run_id"],
		"session_id":             row["session_id"],
		"state":                  row["state"],
		"lane_backend":           laneBackend(pointerMetadata),
		"trajectory_log":         trajectoryLogMetadataFromRow(row, pointerMetadata),
		"delivery_liveness":      deliveryLiveness,
		"pid":                    optionalIntValue(pid, hasPID),
		"pid_liveness":           pidLiveness(pidAlive),
		"lane_liveness_class":    live.Class,
		"pid_start_time":         row["pid_start_time"],
		"current_pid_start_time": nullableText(currentStart),
		"pid_identity":           pidIdentity(row, pidAlive, currentStart),
		"reattach_state":         state,
		"reattach_reason":        nullableText(reason),
		"recommended_action":     action,
		"remediation":            nullableText(remediation),
		"started_at":             timestampValue(row["started_at"]),
		"heartbeat_at":           timestampValue(row["heartbeat_at"]),
		"ended_at":               timestampValue(row["ended_at"]),
		"stop_reason":            row["stop_reason"],
		"stdin_pipe_path":        row["stdin_pipe_path"],
		"pointer": map[string]any{
			"daemon_supervisor_id": row["pointer_daemon_supervisor_id"],
			"state":                row["pointer_state"],
			"pid":                  optionalIntFromAny(row["pointer_pid"]),
			"pid_start_time":       row["pointer_pid_start_time"],
			"updated_at":           timestampValue(row["pointer_updated_at"]),
			"metadata":             superviseObject(row["pointer_metadata_json"]),
		},
		"daemon_supervisor": map[string]any{
			"daemon_supervisor_id": row["daemon_supervisor_id"],
			"daemon_instance_id":   row["daemon_instance_id"],
			"state":                row["daemon_state"],
			"pid":                  optionalIntFromAny(row["daemon_pid"]),
			"pid_start_time":       row["daemon_pid_start_time"],
			"heartbeat_at":         timestampValue(row["daemon_heartbeat_at"]),
			"ended_at":             timestampValue(row["daemon_ended_at"]),
			"stop_reason":          row["daemon_stop_reason"],
		},
	}
}

func latestSessionEscalationReport(ctx context.Context, runner db.Runner, repositoryID, runID, sessionID string) (map[string]any, error) {
	if runID == "" || sessionID == "" {
		return nil, nil
	}
	rows, err := collectRows(ctx, runner,
		`SELECT event_id, run_id, actor_session_id AS session_id, created_at, payload_json
		   FROM striatumd.events
		  WHERE repository_id = $1
		    AND run_id = $2
		    AND actor_session_id = $3
		    AND event_type = 'session.reported'
		    AND payload_json->>'report_kind' = 'escalate'
		  ORDER BY created_at DESC, event_id DESC
		  LIMIT 1`,
		repositoryID, runID, sessionID,
	)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return sessionEscalationReportView(rows[0]), nil
}

func latestSessionEscalationReportsForRun(ctx context.Context, runner db.Runner, repositoryID, runID string) (map[string]map[string]any, error) {
	if runID == "" {
		return map[string]map[string]any{}, nil
	}
	rows, err := collectRows(ctx, runner,
		`SELECT DISTINCT ON (actor_session_id)
		        event_id, run_id, actor_session_id AS session_id, created_at, payload_json
		   FROM striatumd.events
		  WHERE repository_id = $1
		    AND run_id = $2
		    AND actor_session_id IS NOT NULL
		    AND event_type = 'session.reported'
		    AND payload_json->>'report_kind' = 'escalate'
		  ORDER BY actor_session_id, created_at DESC, event_id DESC`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	reports := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		sessionID := superviseString(row["session_id"])
		if sessionID == "" {
			continue
		}
		reports[sessionID] = sessionEscalationReportView(row)
	}
	return reports, nil
}

func sessionEscalationReportView(row map[string]any) map[string]any {
	payload := superviseObject(row["payload_json"])
	return map[string]any{
		"event_id":     row["event_id"],
		"run_id":       row["run_id"],
		"session_id":   row["session_id"],
		"created_at":   timestampValue(row["created_at"]),
		"report_kind":  "escalate",
		"phase":        nullableText(superviseString(payload["phase"])),
		"blocker_kind": nullableText(superviseString(payload["blocker_kind"])),
		"message":      nullableText(superviseString(payload["message"])),
	}
}

func reattachState(row map[string]any, pidAlive bool, currentStart string, live gosupervisor.LaneLiveness) (string, string, string) {
	state := superviseString(row["state"])
	if supervisorTerminalStates[state] {
		return "terminal", state, "no_action"
	}
	if live.Backed == "tmux" {
		switch live.Class {
		case string(gosupervisor.TmuxLivenessOK):
			if live.Backed == "tmux" && live.Alive && live.Detail == "start_token_unverified" {
				return "needs_verification", "start_token_unverified", "verify_before_reattach"
			}
			// Keep checking pointer and daemon consistency below.
		case string(gosupervisor.TmuxLivenessUnavailable):
			return "needs_verification", live.Class, "verify_before_reattach"
		case string(gosupervisor.TmuxLivenessHelperDetachedProcessAlive):
			return "needs_verification", live.Class, "rebridge_helper"
		default:
			return "lost_candidate", live.Class, "mark_lost_or_reconcile"
		}
	}
	_, hasPID := intValueOptional(row["pid"])
	if !hasPID {
		return "lost_candidate", "pid_missing", "mark_lost_or_reconcile"
	}
	if !pidAlive {
		return "lost_candidate", "pid_gone", "mark_lost_or_reconcile"
	}
	expectedStart := superviseString(row["pid_start_time"])
	if expectedStart == "" || currentStart == "" {
		return "needs_verification", "pid_identity_unavailable", "verify_before_reattach"
	}
	if currentStart != expectedStart {
		return "lost_candidate", "pid_identity_mismatch", "mark_lost_or_reconcile"
	}
	pointerState := superviseString(row["pointer_state"])
	if row["pointer_daemon_supervisor_id"] == nil {
		return "needs_repair", "pointer_missing", "repair_supervisor_pointer"
	}
	if pointerState != state {
		return "needs_repair", "pointer_state_mismatch", "repair_supervisor_pointer"
	}
	if row["daemon_supervisor_id"] == nil {
		return "needs_repair", "daemon_supervisor_missing", "repair_daemon_supervisor"
	}
	daemonState := superviseString(row["daemon_state"])
	if daemonState != state {
		return "needs_repair", "daemon_state_mismatch", "repair_daemon_supervisor"
	}
	return "reattachable", "", "reattach"
}

func pidIdentity(row map[string]any, pidAlive bool, currentStart string) string {
	expectedStart := superviseString(row["pid_start_time"])
	if !pidAlive {
		return "pid_gone"
	}
	if expectedStart == "" || currentStart == "" {
		return "unverified"
	}
	if currentStart != expectedStart {
		return "mismatch"
	}
	return "matched"
}

func pidLiveWithStartToken(pid int, expectedStart string) (bool, string, string) {
	if !pidAlive(pid) {
		return false, "", "pid_gone"
	}
	currentStart := ""
	if expectedStart != "" {
		var ok bool
		currentStart, ok = processStartToken(pid)
		if !ok || currentStart == "" {
			return false, currentStart, "pid_identity_unavailable"
		}
		if currentStart != expectedStart {
			return false, currentStart, "pid_identity_mismatch"
		}
	}
	return true, currentStart, ""
}

func pidIdentityFromReason(alive bool, reason string) string {
	if alive {
		return "matched"
	}
	switch reason {
	case "pid_identity_mismatch":
		return "mismatch"
	case "pid_identity_unavailable":
		return "unverified"
	default:
		return "pid_gone"
	}
}

func supervisorProgressForSession(ctx context.Context, runner db.Runner, repositoryID, sessionID string, stallAfterSeconds int) (map[string]any, error) {
	rows, err := collectRows(ctx, runner,
		`SELECT ps.supervisor_id, ps.run_id, ps.session_id, ps.pid,
		        ps.state AS supervisor_state,
		        COALESCE(ptr.updated_at, ps.heartbeat_at) AS supervisor_heartbeat_at,
		        s.last_heartbeat_at AS session_last_heartbeat_at,
		        l.lease_id, l.resource_id AS job_id, l.acquired_at,
		        l.expires_at, l.last_heartbeat_at AS lease_last_heartbeat_at,
		        j.workflow_job_id, j.state AS job_state,
		        j.current_message_id AS message_id,
		        qm.state AS message_state
		   FROM striatumd.process_supervisors ps
		   JOIN striatumd.sessions s
		     ON s.repository_id = ps.repository_id
		    AND s.session_id = ps.session_id
		   JOIN striatumd.leases l
		     ON l.repository_id = ps.repository_id
		    AND l.run_id = ps.run_id
		    AND l.owner_session_id = ps.session_id
		   JOIN striatumd.jobs j
		     ON j.repository_id = l.repository_id
		    AND j.job_id = l.resource_id
		    AND j.current_lease_id = l.lease_id
		   LEFT JOIN striatumd.queue_messages qm
		     ON qm.repository_id = j.repository_id
		    AND qm.message_id = j.current_message_id
		   LEFT JOIN striatumd.process_supervisor_pointers ptr
		     ON ptr.repository_id = ps.repository_id
		    AND ptr.supervisor_id = ps.supervisor_id
		  WHERE ps.repository_id = $1
		    AND ps.session_id = $2
		    AND ps.state = 'attached'
		    AND l.state = 'active'
		    AND l.resource_type = 'job'
		    AND j.state IN ('claimed', 'running')
		  ORDER BY ps.started_at DESC, ps.supervisor_id DESC
		  LIMIT 1`,
		repositoryID, sessionID,
	)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return enrichSupervisorProgress(rows[0], time.Now().UTC().Truncate(time.Second), stallAfterSeconds), nil
}

func enrichSupervisorProgress(row map[string]any, now time.Time, stallAfterSeconds int) map[string]any {
	candidates := []any{
		row["lease_last_heartbeat_at"],
		row["session_last_heartbeat_at"],
		row["supervisor_heartbeat_at"],
		row["acquired_at"],
	}
	var lastProgress time.Time
	for _, candidate := range candidates {
		if parsed, ok := parseTimeValue(candidate); ok && (lastProgress.IsZero() || parsed.After(lastProgress)) {
			lastProgress = parsed
		}
	}
	var ageSeconds any
	stalledByAge := false
	if !lastProgress.IsZero() {
		age := int(now.Sub(lastProgress).Seconds())
		if age < 0 {
			age = 0
		}
		ageSeconds = age
		stalledByAge = age >= stallAfterSeconds
	}
	expiresAt, hasExpiresAt := parseTimeValue(row["expires_at"])
	acquiredAt, hasAcquiredAt := parseTimeValue(row["acquired_at"])
	leaseExpired := hasExpiresAt && expiresAt.Before(now)
	var leaseSeconds any
	preExpiryTracked := true
	if hasExpiresAt && hasAcquiredAt {
		seconds := int(expiresAt.Sub(acquiredAt).Seconds())
		if seconds < 0 {
			seconds = 0
		}
		leaseSeconds = seconds
		preExpiryTracked = seconds <= 24*60*60
	}
	item := copyMap(row)
	item["last_progress_at"] = timestampValue(lastProgress)
	item["last_progress_age_seconds"] = ageSeconds
	item["stall_after_seconds"] = stallAfterSeconds
	item["lease_duration_seconds"] = leaseSeconds
	item["lease_expired"] = leaseExpired
	item["stalled"] = leaseExpired || (preExpiryTracked && stalledByAge)
	item["lease_expires_at"] = timestampValue(row["expires_at"])
	item["lease_last_heartbeat_at"] = timestampValue(row["lease_last_heartbeat_at"])
	item["session_last_heartbeat_at"] = timestampValue(row["session_last_heartbeat_at"])
	item["supervisor_heartbeat_at"] = timestampValue(row["supervisor_heartbeat_at"])
	return item
}

func supervisorView(row map[string]any) map[string]any {
	view := copyMap(row)
	for _, key := range []string{"started_at", "heartbeat_at", "ended_at"} {
		view[key] = timestampValue(view[key])
	}
	attachSupervisorTmux(view, "pointer_metadata_json")
	return view
}

func attachSupervisorTmux(view map[string]any, metadataKey string) {
	metadata := superviseObject(view[metadataKey])
	delete(view, metadataKey)
	if mode := superviseString(metadata["agent_loop_mode"]); mode != "" {
		view["agent_loop_mode"] = mode
	}
	if runAsUser := superviseRunAsUser(metadata); runAsUser != "" {
		view["run_as_user"] = runAsUser
	}
	if termination := processTerminationMetadata(superviseObject(metadata["last_process_termination"])); len(termination) > 0 {
		view["last_process_termination"] = termination
	}
	view["lane_backend"] = laneBackend(metadata)
	if tmux := tmuxMetadata(metadata); tmux != nil {
		view["tmux"] = tmux
		if delivery := superviseObject(tmux["delivery_liveness"]); len(delivery) > 0 {
			view["delivery_liveness"] = delivery
		}
	}
	if _, ok := view["delivery_liveness"]; !ok {
		if delivery := deliveryLivenessMetadata(superviseObject(metadata["delivery_liveness"])); len(delivery) > 0 {
			view["delivery_liveness"] = delivery
		}
	}
	if delivery := superviseObject(view["delivery_liveness"]); len(delivery) > 0 && (superviseString(delivery["remediation"]) == "" || strings.Contains(superviseString(delivery["remediation"]), "<session_id>")) {
		if remediation := deliveryRemediation(superviseString(delivery["reason"]), superviseString(view["session_id"])); remediation != "" {
			delivery["remediation"] = remediation
			view["delivery_liveness"] = delivery
		}
	}
	helperPID, hasHelperPID := intValueOptional(metadata["helper_pid"])
	helperStart := superviseString(metadata["helper_pid_start_time"])
	if hasHelperPID && helperPID > 0 && !lanehealth.IsHelperAlive(helperPID, helperStart) {
		view["delivery_liveness"] = map[string]any{
			"healthy":     false,
			"reason":      "helper_process_gone",
			"class":       "helper_process_gone",
			"remediation": deliveryRemediation("helper_process_gone", superviseString(view["session_id"])),
		}
	}
	view["delivery_state"] = deliveryState(view["delivery_liveness"])
	view["trajectory_log"] = trajectoryLogMetadataFromRow(view, metadata)
}

func trajectoryLogMetadataFromRow(row map[string]any, metadata map[string]any) map[string]any {
	path, err := trajectoryLogPathForRow(row)
	agentLoopMode := superviseString(metadata["agent_loop_mode"])
	info := trajectoryLogMetadataForPath(path, superviseString(row["supervisor_id"]), agentLoopMode)
	if err != nil {
		info["status"] = "invalid_path"
		info["error"] = err.Error()
		return info
	}
	return info
}

func trajectoryLogMetadataForPath(path string, supervisorID string, agentLoopMode string) map[string]any {
	info := map[string]any{
		"enabled":         agentLoopMode != "",
		"expected":        agentLoopMode != "",
		"supervisor_id":   nullableText(supervisorID),
		"agent_loop_mode": nullableText(agentLoopMode),
	}
	if path == "" {
		info["status"] = "unconfigured"
		info["exists"] = false
		return info
	}
	info["path"] = path
	stat, err := os.Stat(path)
	if err == nil && !stat.IsDir() {
		info["status"] = "available"
		info["exists"] = true
		info["enabled"] = true
		info["size_bytes"] = stat.Size()
		info["updated_at"] = timestampValue(stat.ModTime())
		return info
	}
	if err == nil && stat.IsDir() {
		info["status"] = "unreadable"
		info["exists"] = true
		info["error"] = "path is a directory"
		return info
	}
	if os.IsNotExist(err) {
		if agentLoopMode != "" {
			info["status"] = "missing"
		} else {
			info["status"] = "not_configured"
		}
		info["exists"] = false
		return info
	}
	info["status"] = "unreadable"
	info["exists"] = true
	info["error"] = err.Error()
	return info
}

func trajectoryLogPathForRow(row map[string]any) (string, error) {
	return trajectoryLogPath(superviseString(row["repo_root"]), superviseString(row["scratch_path"]), superviseString(row["supervisor_id"]))
}

func trajectoryLogPath(repoRoot string, scratchPath string, supervisorID string) (string, error) {
	if scratchPath == "" {
		if repoRoot == "" || supervisorID == "" {
			return "", nil
		}
		scratchPath = filepath.Join(repoRoot, ".striatum", "scratch", supervisorID)
	}
	absScratch, err := filepath.Abs(scratchPath)
	if err != nil {
		return "", err
	}
	if repoRoot != "" {
		absRepoRoot, err := filepath.Abs(repoRoot)
		if err != nil {
			return "", err
		}
		scratchRoot := filepath.Join(absRepoRoot, ".striatum", "scratch")
		if !pathWithin(absScratch, scratchRoot) {
			return "", fmt.Errorf("trajectory log path escapes repository scratch directory")
		}
	}
	return filepath.Join(absScratch, supervisorTrajectoryLogFilename), nil
}

func pathWithin(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel))
}

func tailBytesByLines(data []byte, lines int) ([]byte, bool) {
	if lines <= 0 {
		return data, false
	}
	newlines := 0
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] != '\n' {
			continue
		}
		newlines++
		if newlines > lines {
			return data[i+1:], true
		}
	}
	return data, false
}

func processTerminationMetadata(raw map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"phase", "reason", "signal", "method", "tmux_session_name", "reported_at"} {
		if value := superviseString(raw[key]); value != "" {
			out[key] = value
		}
	}
	for _, key := range []string{"pid", "attach_pid"} {
		if value, ok := intValueOptional(raw[key]); ok {
			out[key] = value
		}
	}
	return out
}

func attachTmuxLiveness(view map[string]any, live gosupervisor.LaneLiveness) {
	if live.Class != "" {
		view["pane_liveness"] = live.Class
	}
	if live.Backed != "tmux" || live.Tmux == nil {
		return
	}
	tmux := superviseObject(view["tmux"])
	if len(tmux) == 0 {
		tmux = map[string]any{"state": "backed"}
	}
	tmux["liveness"] = gosupervisor.TmuxLivenessPayload(*live.Tmux)
	if remediation := tmuxLivenessRemediation(live.Class, live.Detail, superviseString(view["session_id"])); remediation != "" {
		tmux["remediation"] = remediation
	}
	view["tmux"] = tmux
}

func attachTmuxLivenessFromMetadata(ctx context.Context, view map[string]any, metadata map[string]any, pid int, expectedStart string) gosupervisor.LaneLiveness {
	live := gosupervisor.ProbeLaneLiveness(ctx, superviseTmuxRunnerForMetadata(metadata), metadata, pid, expectedStart)
	attachTmuxLiveness(view, live)
	return live
}

func superviseTmuxRunnerForMetadata(metadata map[string]any) gosupervisor.TmuxRunner {
	runAsUser := superviseRunAsUser(metadata)
	if runAsUser == "" {
		return superviseTmuxRunner
	}
	env := []string{"PATH=" + supervisePath()}
	env = append(env, superviseLaneUserIdentityEnv(runAsUser)...)
	return gosupervisor.RunAsTmuxRunner(runAsUser, env)
}

func superviseRunAsUser(metadata map[string]any) string {
	if runAsUser := strings.TrimSpace(superviseString(metadata["run_as_user"])); runAsUser != "" {
		return runAsUser
	}
	if tmux := superviseObject(metadata["tmux"]); len(tmux) > 0 {
		return strings.TrimSpace(superviseString(tmux["run_as_user"]))
	}
	return ""
}

func superviseLaneUserIdentityEnv(runAsUser string) []string {
	runAsUser = strings.TrimSpace(runAsUser)
	if runAsUser == "" {
		return nil
	}
	entries := []string{"USER=" + runAsUser, "LOGNAME=" + runAsUser}
	if home := superviseLaneUserHome(runAsUser); home != "" {
		entries = append(entries, "HOME="+home)
	}
	return entries
}

func superviseLaneUserHome(runAsUser string) string {
	if u, err := user.Lookup(strings.TrimSpace(runAsUser)); err == nil {
		return strings.TrimSpace(u.HomeDir)
	}
	return ""
}

func supervisePath() string {
	if path := strings.TrimSpace(os.Getenv("PATH")); path != "" {
		return path
	}
	return "/usr/local/bin:/usr/bin:/bin"
}

func tmuxMetadata(metadata map[string]any) map[string]any {
	raw := superviseObject(metadata["tmux"])
	if len(raw) == 0 {
		return nil
	}
	tmux := map[string]any{}
	for _, key := range []string{"state", "session_name", "window_id", "pane_id", "pane_start_token", "attach_command", "unavailable_reason", "captured_at", "probe_skipped_at", "last_ok_at", "last_unavailable_detail", "liveness_state", "run_as_user"} {
		if value := superviseString(raw[key]); value != "" {
			tmux[key] = value
		}
	}
	for _, key := range []string{"pane_pid", "attach_client_pid", "probe_unavailable_count"} {
		if value, ok := intValueOptional(raw[key]); ok {
			tmux[key] = value
		}
	}
	if delivery := deliveryLivenessMetadata(superviseObject(raw["delivery_liveness"])); len(delivery) > 0 {
		tmux["delivery_liveness"] = delivery
	}
	if lastExit := attachClientLastExitMetadata(superviseObject(raw["attach_client_last_exit"])); len(lastExit) > 0 {
		tmux["attach_client_last_exit"] = lastExit
	}
	if liveness := superviseObject(raw["liveness"]); len(liveness) > 0 {
		tmux["liveness"] = tmuxLivenessMetadata(liveness)
	}
	if reason := superviseString(tmux["unavailable_reason"]); reason != "" {
		tmux["state"] = "unavailable"
		if remediation := tmuxUnavailableRemediation(reason); remediation != "" {
			tmux["remediation"] = remediation
		}
	} else if superviseString(tmux["state"]) == "" && (superviseString(tmux["attach_command"]) != "" || superviseString(tmux["session_name"]) != "") {
		tmux["state"] = "attachable"
	}
	if len(tmux) == 0 {
		return nil
	}
	return tmux
}

func laneBackend(metadata map[string]any) string {
	tmux := superviseObject(metadata["tmux"])
	switch {
	case superviseString(tmux["state"]) == "backed":
		return "tmux"
	case superviseString(tmux["unavailable_reason"]) != "":
		return "plain_pty_fallback"
	default:
		return "plain_pty"
	}
}

// reconcileBenignAttachExit clears the noisy top-level delivery_liveness block
// when the only recorded delivery degradation is the tmux attach-session
// OBSERVER client exiting and the lanehealth checker — having reconciled the
// live probe and the real transport (helper PID) — deems the lane deliverable
// (#63 F7). The raw observation under tmux.delivery_liveness /
// tmux.attach_client_last_exit is left intact as provenance. Genuine transport
// failures keep health.Deliverable false and are surfaced by the caller.
func reconcileBenignAttachExit(view map[string]any, health lanehealth.Health) {
	if !health.Deliverable {
		return
	}
	delivery := superviseObject(view["delivery_liveness"])
	if len(delivery) == 0 || superviseString(delivery["reason"]) != "attach_client_exited" {
		return
	}
	delete(view, "delivery_liveness")
	view["delivery_state"] = "healthy"
}

func deliveryState(value any) string {
	delivery := superviseObject(value)
	class := superviseString(delivery["class"])
	if class != "" {
		return class
	}
	if healthy, ok := delivery["healthy"].(bool); ok && healthy {
		return "healthy"
	}
	return "healthy"
}

func deliveryLivenessMetadata(raw map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"class", "reason", "observed_at", "reported_at", "remediation"} {
		if value := superviseString(raw[key]); value != "" {
			out[key] = value
		}
	}
	if healthy, ok := raw["healthy"].(bool); ok {
		out["healthy"] = healthy
	}
	if len(out) == 0 {
		return nil
	}
	if superviseString(out["remediation"]) == "" {
		if remediation := deliveryRemediation(superviseString(out["reason"]), ""); remediation != "" {
			out["remediation"] = remediation
		}
	}
	return out
}

func attachClientLastExitMetadata(raw map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"observed_at", "reported_at", "tmux_liveness"} {
		if value := superviseString(raw[key]); value != "" {
			out[key] = value
		}
	}
	for _, key := range []string{"attach_pid", "attach_exit_code", "pane_pid"} {
		if value, ok := intValueOptional(raw[key]); ok {
			out[key] = value
		}
	}
	if delivery := deliveryLivenessMetadata(superviseObject(raw["delivery_liveness"])); len(delivery) > 0 {
		out["delivery_liveness"] = delivery
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func tmuxLivenessMetadata(raw map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"class", "state", "detail", "remediation"} {
		if value := superviseString(raw[key]); value != "" {
			out[key] = value
		}
	}
	if value, ok := raw["healthy"].(bool); ok {
		out["healthy"] = value
	}
	for _, key := range []string{"observed_pane_pid"} {
		if value, ok := intValueOptional(raw[key]); ok {
			out[key] = value
		}
	}
	if value := superviseString(raw["observed_pane_start"]); value != "" {
		out["observed_pane_start"] = value
	}
	if failure := tmuxProbeFailureMetadata(superviseObject(raw["probe_failure"])); len(failure) > 0 {
		out["probe_failure"] = failure
	}
	return out
}

func tmuxProbeFailureMetadata(raw map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"failure_class", "detail", "errno"} {
		if value := superviseString(raw[key]); value != "" {
			out[key] = value
		}
	}
	for _, key := range []string{"exit_code", "observed_pane_pid"} {
		if value, ok := intValueOptional(raw[key]); ok {
			out[key] = value
		}
	}
	if value, ok := raw["pane_process_alive"].(bool); ok {
		out["pane_process_alive"] = value
	}
	return out
}

func tmuxLivenessRemediation(class string, detail string, sessionID string) string {
	switch class {
	case string(gosupervisor.TmuxLivenessUnavailable):
		if strings.Contains(detail, "command not found") {
			return "tmux is not available to the daemon; install tmux or repair the daemon PATH before retrying"
		}
		if strings.Contains(detail, "timeout") {
			return "tmux did not answer before the probe timeout; inspect the local tmux server and retry"
		}
		return "tmux could not be probed; repair tmux availability before trusting lane liveness"
	case string(gosupervisor.TmuxLivenessSessionMissing):
		return "tmux session is missing; stop this supervisor and start or reclaim a replacement lane"
	case string(gosupervisor.TmuxLivenessPaneMissing):
		return "tmux pane is missing; stop this supervisor and start or reclaim a replacement lane"
	case string(gosupervisor.TmuxLivenessPaneDead):
		return "tmux pane process exited; inspect the retained pane if needed, then stop and restart or reclaim the lane"
	case string(gosupervisor.TmuxLivenessPanePIDMismatch):
		return "tmux pane identity changed; stop this supervisor and start or reclaim a replacement lane"
	case string(gosupervisor.TmuxLivenessHelperDetachedProcessAlive):
		target := "--session-id <session_id>"
		if sessionID != "" {
			target = "--session-id " + sessionID
		}
		return "helper detached while the tmux pane process is alive; run striatum supervise rebridge " + target
	default:
		return ""
	}
}

func deliveryRemediation(reason string, sessionID string) string {
	target := "--session-id <session_id>"
	if sessionID != "" {
		target = "--session-id " + sessionID
	}
	switch reason {
	case "attach_client_exited":
		return "delivery client exited; run striatum supervise rebridge " + target
	case "stdin_reader_missing":
		return "supervisor stdin reader is missing; run striatum supervise rebridge " + target + " for tmux-backed lanes or restart the lane"
	default:
		return ""
	}
}

func tmuxUnavailableRemediation(reason string) string {
	switch reason {
	case "tmux_not_found":
		return "install tmux or unset supervision.require_tmux for non-interactive lanes"
	case "missing_run_or_lane":
		return "restart the PTY supervisor with STRIATUM_RUN_ID and STRIATUM_LANE_ID"
	default:
		return ""
	}
}

func rowExists(ctx context.Context, runner db.Runner, sql string, args ...any) (bool, error) {
	rows, err := collectRows(ctx, runner, sql, args...)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

func requiredTextParam(envelope rpc.Envelope, key string, message string) (string, error) {
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

func optionalTextParam(envelope rpc.Envelope, key string) (string, error) {
	value, ok := envelope.Params[key]
	if !ok || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", rpc.NewError("schema_invalid", key+" must be a string", nil)
	}
	return text, nil
}

func optionalBoolParam(envelope rpc.Envelope, key string) (bool, error) {
	value, ok := envelope.Params[key]
	if !ok || value == nil {
		return false, nil
	}
	if typed, ok := value.(bool); ok {
		return typed, nil
	}
	if typed, ok := value.(string); ok {
		parsed, err := strconv.ParseBool(typed)
		if err == nil {
			return parsed, nil
		}
	}
	return false, rpc.NewError("schema_invalid", key+" must be a boolean", nil)
}

func optionalNonNegativeIntParam(envelope rpc.Envelope, key string) (int, error) {
	value, ok := envelope.Params[key]
	if !ok || value == nil {
		return 0, nil
	}
	parsed, ok := intValueOptional(value)
	if !ok || parsed < 0 {
		return 0, rpc.NewError("schema_invalid", key+" must be a non-negative integer", nil)
	}
	return parsed, nil
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if processZombie(pid) {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
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

func pidLiveness(alive bool) string {
	if alive {
		return "alive"
	}
	return "gone"
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt(value int) any {
	if value == 0 {
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

func optionalIntFromAny(value any) any {
	if parsed, ok := intValueOptional(value); ok {
		return parsed
	}
	return nil
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
		parsed, err := strconv.Atoi(typed.String())
		return parsed, err == nil
	case string:
		if typed == "" {
			return 0, false
		}
		parsed, err := strconv.Atoi(typed)
		return parsed, err == nil
	}
	return 0, false
}

func boolValue(value any) bool {
	typed, _ := value.(bool)
	return typed
}

func superviseString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func superviseObject(value any) map[string]any {
	result := objectFromJSONish(value)
	if result == nil {
		return map[string]any{}
	}
	return result
}

func timestampValue(value any) any {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return nil
		}
		return typed.UTC().Truncate(time.Second).Format(time.RFC3339)
	case *time.Time:
		if typed == nil || typed.IsZero() {
			return nil
		}
		return typed.UTC().Truncate(time.Second).Format(time.RFC3339)
	default:
		return value
	}
}

func parseTimeValue(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), true
	case string:
		if typed == "" {
			return time.Time{}, false
		}
		parsed, err := time.Parse(time.RFC3339, typed)
		if err != nil {
			return time.Time{}, false
		}
		return parsed.UTC(), true
	}
	return time.Time{}, false
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
	// Issue #62: any terminal supervisor transition — graceful probed-gone exit,
	// supervise.stop / tmux kill, signal / lost — must remove the per-launch
	// .gemini/settings.json (rotating MCP bearer token) the lane wrote. Doing it
	// here, at the single state-transition choke point, makes teardown cleanup
	// path-independent. CleanupGeminiSettings is idempotent (no-ops once its
	// scratch markers are gone), so calling it on every terminal transition is
	// safe even when another path already cleaned up.
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

// cleanupSupervisorLaneMCPConfig resolves the supervisor's working directory and
// removes/restores any per-launch lane operational files the lane wrote into the
// target work tree: the agy .gemini/settings.json bearer-token file (#62) and
// the Claude .claude/scheduled_tasks.lock (#129). Best-effort: a missing repo
// root or absent files leave nothing to do.
func cleanupSupervisorLaneMCPConfig(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID string) {
	repoRoot, runAsUser := supervisorCleanupContext(ctx, runner, repositoryID, supervisorID)
	if repoRoot == "" {
		return
	}
	agentloop.CleanupGeminiSettings(repoRoot, supervisorID)
	if home := superviseLaneUserHome(runAsUser); home != "" {
		agentloop.CleanupAgyUserSettings(home, supervisorID)
	}
	agentloop.CleanupClaudeScheduledTasksLock(repoRoot)
}

// supervisorCleanupContext reads the supervisor's recorded working directory
// (cwd), falling back to deriving it from the scratch path layout
// (<repo>/.striatum/scratch/<supervisor_id>), plus the optional run-as user used
// for lane-user scoped cleanup.
func supervisorCleanupContext(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID string) (string, string) {
	var cwd, scratchPath, runAsUser string
	if err := runner.QueryRow(ctx, `
		SELECT ps.cwd, ps.scratch_path, COALESCE(p.metadata_json->>'run_as_user', '')
		  FROM striatumd.process_supervisors ps
		  LEFT JOIN striatumd.process_supervisor_pointers p
		    ON p.repository_id = ps.repository_id AND p.supervisor_id = ps.supervisor_id
		 WHERE ps.repository_id = $1 AND ps.supervisor_id = $2`,
		repositoryID, supervisorID,
	).Scan(&cwd, &scratchPath, &runAsUser); err != nil {
		return "", ""
	}
	if strings.TrimSpace(cwd) != "" {
		return cwd, runAsUser
	}
	if strings.TrimSpace(scratchPath) != "" {
		// scratch_path is <repo>/.striatum/scratch/<supervisor_id>.
		return filepath.Dir(filepath.Dir(filepath.Dir(scratchPath))), runAsUser
	}
	return "", runAsUser
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableStringPointer(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func nowString() string {
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}
