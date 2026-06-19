package lanehealth

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"syscall"
	"time"

	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
	"github.com/jackc/pgx/v5"
)

type LaneReason string

const (
	ReasonNone                    LaneReason = ""
	ReasonNoAttachedSupervisor    LaneReason = "no_attached_supervisor"
	ReasonPIDMissing              LaneReason = "pid_missing"
	ReasonDaemonSupervisorMissing LaneReason = "daemon_supervisor_missing"
	ReasonPointerStateMismatch    LaneReason = "pointer_state_mismatch"
	ReasonDaemonStateMismatch     LaneReason = "daemon_state_mismatch"
	ReasonPointerPIDMismatch      LaneReason = "pointer_pid_mismatch"
	ReasonTmuxMetadataCorrupt     LaneReason = "tmux_metadata_corrupt"
	ReasonStartTokenUnverified    LaneReason = "start_token_unverified"
	ReasonPIDGone                 LaneReason = "pid_gone"
	ReasonSupervisorStalled       LaneReason = "supervisor_stalled"
	ReasonPIDIdentityMismatch     LaneReason = "pid_identity_mismatch"
	ReasonPIDIdentityUnavailable  LaneReason = "pid_identity_unavailable"
)

// LivenessAliveButSilent is the projection-only protocol state reported on
// Health.LivenessClass for a lane doing honest long local work (RFC 0140): its
// active PID probe is positively alive and identity-matched, but it has made no
// tool-call progress for ToolProgressSeconds, so sessionliveness reclassifies it
// wedged_no_tool_progress (#324). It is a first-class, attestation-PRESERVING
// state — the supervised process is provably alive, so its byline is not forged
// (RFC 0026 / D080). It is NOT a persisted liveness_stall_class enum value (it
// never widens the migration-0012 CHECK constraint); it is computed on the fly by
// Classify and surfaced only on the in-memory Health.
const LivenessAliveButSilent = "alive_but_silent"

// deliveryReasonAttachClientExited is the delivery_liveness reason emitted when
// the `tmux attach-session` observer client exits. Per RFC 0089 that client is
// an OBSERVER only and is not the delivery transport, so its exit must not
// degrade packet delivery while the pane is alive and the real transport
// (persistent FIFO / pty helper) is healthy. See #63 F7.
const deliveryReasonAttachClientExited = "attach_client_exited"

type Health struct {
	Bound, Alive, Attested, Deliverable bool
	Stall                               sessionliveness.Result
	Reason                              LaneReason
	LivenessClass                       string
	SupervisorID                        string
	PID                                 int
	DeliveryReason                      string
}

func (h Health) LiveTarget() bool { return h.Attested && h.Alive }
func (h Health) CanDeliver() bool { return h.Alive && h.Deliverable }
func (h Health) IsStalled() bool  { return h.Stall.StallClass != "" }

// Probe is the injected process probe port.
type Probe interface {
	ProbeLane(ctx context.Context, meta gosupervisor.TmuxMeta, pid int, startToken string) gosupervisor.LaneLiveness
}

// ProdProbe is the production adapter wrapping gosupervisor.ProbeLaneLiveness.
type ProdProbe struct {
	Runner      gosupervisor.TmuxRunner
	RunAsRunner func(runAsUser string) gosupervisor.TmuxRunner
}

func (p ProdProbe) ProbeLane(ctx context.Context, meta gosupervisor.TmuxMeta, pid int, startToken string) gosupervisor.LaneLiveness {
	// Reconstruct map shape for legacy compatibility under supervisor package.
	metadata := map[string]any{
		"agent_loop_mode": meta.AgentLoopMode,
	}
	tmux := map[string]any{
		"state":                   meta.Tmux.State,
		"session_name":            meta.Tmux.SessionName,
		"window_id":               meta.Tmux.WindowID,
		"pane_id":                 meta.Tmux.PaneID,
		"pane_start_token":        meta.Tmux.PaneStartToken,
		"attach_command":          meta.Tmux.AttachCommand,
		"unavailable_reason":      meta.Tmux.UnavailableReason,
		"captured_at":             meta.Tmux.CapturedAt,
		"probe_skipped_at":        meta.Tmux.ProbeSkippedAt,
		"last_ok_at":              meta.Tmux.LastOkAt,
		"last_unavailable_detail": meta.Tmux.LastUnavailableDetail,
		"liveness_state":          meta.Tmux.LivenessState,
		"pane_pid":                meta.Tmux.PanePID,
		"attach_client_pid":       meta.Tmux.AttachClientPID,
		"probe_unavailable_count": meta.Tmux.ProbeUnavailableCount,
	}
	if meta.Tmux.RunAsUser != "" {
		tmux["run_as_user"] = meta.Tmux.RunAsUser
	}
	if meta.Tmux.DeliveryLiveness != nil {
		tmux["delivery_liveness"] = map[string]any{
			"class":       meta.Tmux.DeliveryLiveness.Class,
			"reason":      meta.Tmux.DeliveryLiveness.Reason,
			"observed_at": meta.Tmux.DeliveryLiveness.ObservedAt,
			"reported_at": meta.Tmux.DeliveryLiveness.ReportedAt,
			"remediation": meta.Tmux.DeliveryLiveness.Remediation,
			"healthy":     meta.Tmux.DeliveryLiveness.Healthy,
		}
	}
	if meta.Tmux.AttachClientLastExit != nil {
		tmux["attach_client_last_exit"] = meta.Tmux.AttachClientLastExit
	}
	if meta.Tmux.Liveness != nil {
		tmux["liveness"] = meta.Tmux.Liveness
	}
	metadata["tmux"] = tmux

	if meta.DeliveryLiveness != nil {
		metadata["delivery_liveness"] = map[string]any{
			"class":       meta.DeliveryLiveness.Class,
			"reason":      meta.DeliveryLiveness.Reason,
			"observed_at": meta.DeliveryLiveness.ObservedAt,
			"reported_at": meta.DeliveryLiveness.ReportedAt,
			"remediation": meta.DeliveryLiveness.Remediation,
			"healthy":     meta.DeliveryLiveness.Healthy,
		}
	}

	return gosupervisor.ProbeLaneLiveness(ctx, p.runnerForMeta(meta), metadata, pid, startToken)
}

func (p ProdProbe) runnerForMeta(meta gosupervisor.TmuxMeta) gosupervisor.TmuxRunner {
	runAsUser := strings.TrimSpace(meta.Tmux.RunAsUser)
	if runAsUser != "" {
		if p.RunAsRunner != nil {
			return p.RunAsRunner(runAsUser)
		}
		return gosupervisor.RunAsTmuxRunner(runAsUser, nil)
	}
	if p.Runner != nil {
		return p.Runner
	}
	return gosupervisor.DefaultTmuxRunner()
}

type Facts struct {
	SupervisorRecorded bool
	SupervisorID       string
	PID                int
	PIDStartTime       string
	SupervisorState    string

	HasPointer                bool
	PointerDaemonSupervisorID string
	PointerPID                int
	PointerPIDStartTime       string
	PointerState              string
	PointerTmuxMeta           gosupervisor.TmuxMeta
	HelperPID                 int
	HelperPIDStartTime        string

	HasDaemonSupervisor bool
	DaemonSupervisorID  string
	DaemonState         string

	ProbePerformed bool
	ProbeResult    gosupervisor.LaneLiveness

	DeliveryDegraded bool
	DeliveryReason   string

	SessionActivity sessionliveness.Activity
	LivenessPolicy  sessionliveness.Policy
}

// Classify computes composite lane health using a pure state machine.
func Classify(f Facts, now time.Time) Health {
	// #63 F7: the exit of the `tmux attach-session` observer client is not a
	// delivery failure. When the only recorded delivery degradation is
	// attach_client_exited and the pane is alive (the active probe reports the
	// lane alive), the real delivery transport (persistent FIFO / pty helper)
	// is intact, so the lane is still deliverable. Genuine transport failures
	// (helper_process_gone, stdin_reader_missing) are recorded under a
	// different reason — the upstream reader in Check overrides the reason to
	// helper_process_gone when the helper PID is dead — so they are preserved
	// here untouched.
	deliveryDegraded := f.DeliveryDegraded
	deliveryReason := f.DeliveryReason
	if deliveryDegraded && deliveryReason == deliveryReasonAttachClientExited && paneAlive(f) {
		deliveryDegraded = false
		deliveryReason = ""
	}

	h := Health{
		SupervisorID:   f.SupervisorID,
		PID:            f.PID,
		Deliverable:    !deliveryDegraded,
		DeliveryReason: deliveryReason,
	}

	// 1. Basic attachment checks
	if !f.SupervisorRecorded || f.SupervisorState != "attached" {
		h.Reason = ReasonNoAttachedSupervisor
		return h
	}

	// 2. Process check
	if f.PID <= 0 {
		h.Reason = ReasonPIDMissing
		return h
	}

	// 3. Pointer & Daemon structure validation
	if !f.HasPointer || f.PointerDaemonSupervisorID == "" {
		h.Reason = ReasonDaemonSupervisorMissing
		return h
	}
	if f.PointerState != "attached" {
		h.Reason = ReasonPointerStateMismatch
		return h
	}
	if !f.HasDaemonSupervisor || f.DaemonSupervisorID == "" {
		h.Reason = ReasonDaemonSupervisorMissing
		return h
	}
	if f.DaemonState != "attached" {
		h.Reason = ReasonDaemonStateMismatch
		return h
	}
	if f.PointerPID > 0 && f.PointerPID != f.PID {
		h.Reason = ReasonPointerPIDMismatch
		return h
	}

	// 4. Metadata integrity
	if f.PointerTmuxMeta.Tmux.State == "backed" && !f.PointerTmuxMeta.IsValidTmux() {
		h.Reason = ReasonTmuxMetadataCorrupt
		return h
	}

	h.Bound = true

	// 5. Active probe evaluation
	if f.ProbePerformed {
		h.Alive = f.ProbeResult.Alive
		h.LivenessClass = f.ProbeResult.Class
		if !f.ProbeResult.Alive {
			reason := f.ProbeResult.Class
			if reason == "" {
				reason = "pid_gone"
			}
			h.Reason = LaneReason(reason)
			return h
		}
		if f.ProbeResult.Backed == "tmux" && f.ProbeResult.Detail == "start_token_unverified" {
			h.Reason = ReasonStartTokenUnverified
			return h
		}
	}

	// 6. Stall classification
	h.Stall = sessionliveness.Classify(f.SessionActivity, f.LivenessPolicy, now)
	if h.Stall.StallClass != "" {
		// RFC 0140 (part B): a wedged_no_tool_progress stall is a statement about
		// MCP/tool-call PROGRESS, not about process death. A lane doing honest long
		// local work (a full test suite, a browser-acceptance profile, a large repo
		// scan) issues zero tool calls by design, so after ToolProgressSeconds it
		// trips the #324 rung exactly as a dead-endpoint lane would. When that lane's
		// ACTIVE PID probe is positively alive AND identity-matched, it is
		// "alive but tool-call-silent" — attest it, do not drop the byline mid-work.
		//
		// This keys on the SAME forgery-resistant PID oracle the recovery decision
		// tree uses to confirm death (pty_confirmed_dead). Reaching this point with
		// f.ProbePerformed == true GUARANTEES f.ProbeResult.Alive == true and the
		// start token verified: step 5 returns early for a dead PID, a pid-identity
		// mismatch, or start_token_unverified. So f.ProbePerformed && f.ProbeResult.Alive
		// here means a provably-live, identity-matched process — never a dead,
		// hijacked, or closed-session lane (those never reach step 6 attested).
		//
		// A lane with NO positive PID oracle (a pipe / TransportPipe lane, or any
		// lane whose probe was not performed) does NOT qualify and keeps today's
		// behavior — ReasonSupervisorStalled, Attested=false — so the exemption can
		// never rest on PTY freshness or the liveness verdict alone (RFC 0131
		// "default to the lower-confidence classification on ambiguity"). Every
		// OTHER stall class (discovery / await / ack / lease-heartbeat / protocol-
		// idle / attention) still drops attestation unchanged.
		if h.Stall.StallClass == sessionliveness.StallToolProgress && f.ProbePerformed && f.ProbeResult.Alive {
			h.LivenessClass = LivenessAliveButSilent
			h.Attested = true
			return h
		}
		h.Reason = ReasonSupervisorStalled
		return h
	}

	h.Attested = true
	return h
}

// paneAlive reports whether the active probe gives positive evidence the lane's
// pane/process is alive. It is the gate for treating an attach-observer exit as
// benign (#63 F7): without a probe that says the lane is alive we keep the
// recorded delivery degradation rather than assume the transport is healthy.
func paneAlive(f Facts) bool {
	return f.ProbePerformed && f.ProbeResult.Alive
}

// Checker aggregates database loading with liveness probing.
type Checker struct {
	Probe Probe
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (c Checker) Check(ctx context.Context, runner any, repositoryID, sessionID string) (Health, error) {
	q, ok := runner.(queryer)
	if !ok {
		return Health{}, fmt.Errorf("runner does not support queries")
	}
	rows, err := q.Query(ctx, `
		SELECT ps.supervisor_id,
		       ps.pid,
		       COALESCE(ps.pid_start_time, '') AS pid_start_time,
		       ps.state AS supervisor_state,
		       p.daemon_supervisor_id AS pointer_daemon_supervisor_id,
		       p.pid AS pointer_pid,
		       COALESCE(p.pid_start_time, '') AS pointer_pid_start_time,
		       p.state AS pointer_state,
		       COALESCE(p.metadata_json, '{}'::jsonb) AS pointer_metadata_json,
		       ds.daemon_supervisor_id AS daemon_supervisor_id,
		       ds.state AS daemon_state,
		       s.state AS state,
		       s.registered_at AS registered_at,
		       s.last_mcp_request_at AS last_mcp_request_at,
		       s.last_tools_list_at AS last_tools_list_at,
		       s.last_await_packet_at AS last_await_packet_at,
		       s.last_packet_delivered_at AS last_packet_delivered_at,
		       s.last_ack_at AS last_ack_at,
		       s.last_work_block_at AS last_work_block_at,
		       s.last_work_release_at AS last_work_release_at,
		       s.last_work_complete_at AS last_work_complete_at,
		       s.last_work_heartbeat_at AS last_work_heartbeat_at,
		       s.last_session_ready_at AS last_session_ready_at,
		       s.last_session_heartbeat_at AS last_session_heartbeat_at,
		       s.last_session_question_at AS last_session_question_at,
		       s.last_session_escalate_at AS last_session_escalate_at,
		       s.last_pty_activity_at AS last_pty_activity_at,
		       s.last_tool_call_started_at AS last_tool_call_started_at,
		       s.last_tool_call_finished_at AS last_tool_call_finished_at,
		       s.liveness_stall_class AS liveness_stall_class,
		       s.liveness_stall_since AS liveness_stall_since,
		       active_lease.lease_id AS active_lease_id,
		       active_lease.acquired_at AS active_lease_acquired_at,
		       active_lease.expires_at AS active_lease_expires_at,
		       active_lease.last_heartbeat_at AS active_lease_last_heartbeat_at
		  FROM striatumd.sessions s
		  LEFT JOIN striatumd.process_supervisors ps
		    ON ps.repository_id = s.repository_id
		   AND ps.session_id = s.session_id
		   AND ps.state = 'attached'
		  LEFT JOIN striatumd.process_supervisor_pointers p
		    ON p.repository_id = ps.repository_id
		   AND p.supervisor_id = ps.supervisor_id
		  LEFT JOIN striatumd.daemon_supervisors ds
		    ON ds.repository_id = ps.repository_id
		   AND ds.daemon_supervisor_id = p.daemon_supervisor_id
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
		 ORDER BY ps.started_at DESC, ps.supervisor_id DESC
		 LIMIT 1`, repositoryID, sessionID)
	if err != nil {
		return Health{}, err
	}
	defer rows.Close()

	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		return Health{}, err
	}
	if len(items) == 0 {
		return Health{}, pgx.ErrNoRows
	}
	row := items[0]

	f := Facts{
		LivenessPolicy: sessionliveness.DefaultPolicy(),
	}

	f.SessionActivity = sessionliveness.ActivityFromRow(row)

	if val, ok := row["supervisor_id"].(string); ok && val != "" {
		f.SupervisorRecorded = true
		f.SupervisorID = val
		f.PID, _ = intValue(row["pid"])
		f.PIDStartTime, _ = row["pid_start_time"].(string)
		if sState, ok := row["supervisor_state"].(string); ok && sState != "" {
			f.SupervisorState = sState
		} else {
			f.SupervisorState, _ = row["state"].(string)
		}
	}

	if val, ok := row["pointer_daemon_supervisor_id"].(string); ok && val != "" {
		f.HasPointer = true
		f.PointerDaemonSupervisorID = val
		f.PointerPID, _ = intValue(row["pointer_pid"])
		f.PointerPIDStartTime, _ = row["pointer_pid_start_time"].(string)
		f.PointerState, _ = row["pointer_state"].(string)
		f.PointerTmuxMeta = parseTmuxMeta(row["pointer_metadata_json"])
	}

	if val, ok := row["daemon_supervisor_id"].(string); ok && val != "" {
		f.HasDaemonSupervisor = true
		f.DaemonSupervisorID = val
		f.DaemonState, _ = row["daemon_state"].(string)
	}

	// Read delivery liveness degraded state from tmux metadata block
	if f.HasPointer {
		if f.PointerTmuxMeta.DeliveryLiveness != nil && !f.PointerTmuxMeta.DeliveryLiveness.Healthy {
			f.DeliveryDegraded = true
			f.DeliveryReason = f.PointerTmuxMeta.DeliveryLiveness.Reason
		}
		if f.PointerTmuxMeta.Tmux.DeliveryLiveness != nil && !f.PointerTmuxMeta.Tmux.DeliveryLiveness.Healthy {
			f.DeliveryDegraded = true
			f.DeliveryReason = f.PointerTmuxMeta.Tmux.DeliveryLiveness.Reason
		}
		f.HelperPID, f.HelperPIDStartTime = parseHelperFields(row["pointer_metadata_json"])
		if f.HelperPID > 0 {
			if !IsHelperAlive(f.HelperPID, f.HelperPIDStartTime) {
				f.DeliveryDegraded = true
				f.DeliveryReason = "helper_process_gone"
			}
		}
	}

	// Trigger active probe if process supervisor attached and has positive PID
	if f.SupervisorRecorded && f.PID > 0 {
		f.ProbePerformed = true
		f.ProbeResult = c.Probe.ProbeLane(ctx, f.PointerTmuxMeta, f.PID, f.PIDStartTime)
	}

	return Classify(f, time.Now().UTC()), nil
}

func parseTmuxMeta(val any) gosupervisor.TmuxMeta {
	var meta gosupervisor.TmuxMeta
	if val == nil {
		return meta
	}
	switch typed := val.(type) {
	case []byte:
		_ = json.Unmarshal(typed, &meta)
	case string:
		_ = json.Unmarshal([]byte(typed), &meta)
	case map[string]any:
		if data, err := json.Marshal(typed); err == nil {
			_ = json.Unmarshal(data, &meta)
		}
	}
	return meta
}

func intValue(v any) (int, bool) {
	switch typed := v.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		if parsed, err := time.ParseDuration(typed); err == nil {
			return int(parsed.Seconds()), true
		}
		var parsed int
		if _, err := fmt.Sscan(typed, &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// LegacyMap maps unified health properties into the compatible legacy map.
func LegacyMap(h Health) map[string]any {
	if h.Attested {
		return map[string]any{
			"attested":      true,
			"state":         "attested",
			"supervisor_id": h.SupervisorID,
			"pid":           h.PID,
			"reason":        nil,
			"liveness":      h.LivenessClass,
		}
	}
	var reasonVal any = string(h.Reason)
	if h.Reason == ReasonNone {
		reasonVal = nil
	}
	var supervisorIDVal any = h.SupervisorID
	if h.SupervisorID == "" {
		supervisorIDVal = nil
	}
	var pidVal any = h.PID
	if h.PID <= 0 {
		pidVal = nil
	}
	return map[string]any{
		"attested":      false,
		"state":         "unattested",
		"supervisor_id": nullable(supervisorIDVal),
		"pid":           nullable(pidVal),
		"reason":        reasonVal,
	}
}

func nullable(v any) any {
	if v == nil {
		return nil
	}
	return v
}

func IsHelperAlive(pid int, expectedStartTime string) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err != nil {
		return false
	}
	if expectedStartTime != "" {
		currentStart, ok := gosupervisor.ProcessStartToken(pid)
		if !ok || strings.TrimSpace(currentStart) != strings.TrimSpace(expectedStartTime) {
			return false
		}
	}
	return true
}

func parseHelperFields(val any) (int, string) {
	if val == nil {
		return 0, ""
	}
	var m map[string]any
	switch typed := val.(type) {
	case []byte:
		_ = json.Unmarshal(typed, &m)
	case string:
		_ = json.Unmarshal([]byte(typed), &m)
	case map[string]any:
		m = typed
	}
	if m == nil {
		return 0, ""
	}
	pid, _ := intValue(m["helper_pid"])
	startTime, _ := m["helper_pid_start_time"].(string)
	return pid, startTime
}
