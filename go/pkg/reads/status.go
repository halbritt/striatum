package reads

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/lanehealth"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
)

// HandleStatus mirrors src/striatum/daemon_pg/handlers/reads/status.py.
// Returns a snapshot of run, jobs, sessions, queue messages, leases,
// blockers and verdict counts scoped to the caller's repository_id and
// optional run_id filter.
func HandleStatus(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID != "" {
		exists, err := rowExists(ctx, runner,
			`SELECT r.run_id
			   FROM striatumd.runs r
			  WHERE r.repository_id = $1 AND r.run_id = $2
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
	// #193: repo-wide status used to assemble every historical run, returning a
	// 353KB payload no operator (human or agent) can scan and costing the daemon a
	// full-history walk on the obvious first diagnostic command. Default the
	// repo-wide view to the runs an operator actually acts on — every non-terminal
	// run plus the most recent N terminal runs — and let --all-runs / --run-limit
	// override. A run_id-scoped call is already bounded to that one run and ignores
	// the window.
	scope := resolveStatusRunScope(envelope, runID)

	runs, err := statusRuns(ctx, runner, repositoryID, scope)
	if err != nil {
		return nil, err
	}
	jobs, err := statusJobCounts(ctx, runner, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	sessions, err := statusSessions(ctx, runner, repositoryID, scope)
	if err != nil {
		return nil, err
	}
	openBlockers, err := statusBlockers(ctx, runner, repositoryID, runID, "")
	if err != nil {
		return nil, err
	}
	humanCheckpoints, err := statusBlockers(ctx, runner, repositoryID, runID, "human_checkpoint")
	if err != nil {
		return nil, err
	}
	decorateCheckpointResolveActions(humanCheckpoints)
	nonAccepting, err := statusLatestNonAccepting(ctx, runner, repositoryID, scope)
	if err != nil {
		return nil, err
	}
	verdictsByPosture, err := statusVerdictsByPosture(ctx, runner, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	claimable, err := statusClaimableJobs(ctx, runner, repositoryID, scope)
	if err != nil {
		return nil, err
	}
	blockedDownstream, err := statusBlockedDownstreamJobs(ctx, runner, repositoryID, scope)
	if err != nil {
		return nil, err
	}
	processHealth, err := statusProcessHealth(ctx, runner, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	supervisorStalls, err := statusSupervisorStalls(ctx, runner, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	hasOrphanSupervisor, err := statusHasLostSupervisorHeldLease(ctx, runner, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	hasStaleLeases, err := statusHasStaleLeasesWithOnDiskArtifacts(ctx, runner, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	// RFC 0108 Phase 5: the repo-scoped concurrent-runs view is always repo-level
	// (every `running` run on the repo + their live collisions), independent of an
	// optional run_id filter, so an operator viewing one run still sees the whole
	// parallel fan-out and any collision (the RFC 0102 attention principle).
	concurrentRuns, err := repoConcurrentRuns(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	var autoFinalize any
	var provenanceMode any
	var operatorMode any
	result := map[string]any{
		"runs":                                 runs,
		"jobs":                                 jobs,
		"sessions":                             sessions,
		"verdicts_by_posture":                  verdictsByPosture,
		"latest_non_accepting_review_verdicts": nonAccepting,
		"open_blockers":                        openBlockers,
		"claimable_jobs":                       claimable,
		"blocked_downstream_jobs":              blockedDownstream,
		"human_checkpoints":                    humanCheckpoints,
		"process_health":                       processHealth,
		"supervisor_stalls":                    supervisorStalls,
		"auto_finalize_dry_run":                autoFinalize,
		"concurrent_runs":                      concurrentRuns,
		"next_actions":                         statusNextActions(claimable, openBlockers, humanCheckpoints, nonAccepting, hasOrphanSupervisor, hasStaleLeases, processHealth, supervisorStalls, autoFinalize),
		"provenance_mode":                      provenanceMode,
		"operator_mode":                        operatorMode,
	}
	if runID != "" {
		workflow, err := statusWorkflowForRun(ctx, runner, repositoryID, runID)
		if err != nil {
			return nil, err
		}
		provenanceMode = nullableStringValue(stringValue(workflow["provenance_mode"]))
		operatorMode = workflowOperatorMode(workflow)
		autoFinalize, err = dashboardAllAutoFinalizeSummary(ctx, runner, repositoryID, runID, workflow)
		if err != nil {
			return nil, err
		}
		phase, err := dashboardAllPhaseProgress(ctx, runner, repositoryID, runID, workflow)
		if err != nil {
			return nil, err
		}
		result["provenance_mode"] = provenanceMode
		result["operator_mode"] = operatorMode
		result["auto_finalize_dry_run"] = autoFinalize
		result["current_phase_id"] = phase["current_phase_id"]
		result["phases"] = phase["phases"]
		result["selected_run_id"] = runID
		result["next_actions"] = statusNextActions(claimable, openBlockers, humanCheckpoints, nonAccepting, hasOrphanSupervisor, hasStaleLeases, processHealth, supervisorStalls, autoFinalize)
	}
	return result, nil
}

// statusTerminalRunStatesSQL is the SQL list literal of terminal run states.
// A run in one of these can never grow new claimable/blocked work, so it is
// excluded from claimable_jobs / blocked_downstream_jobs and from the repo-wide
// recent-runs default (#193). run.cancel refuses to cancel a completed/failed
// run and produces 'canceled'; pause/resume refuse on completed/failed. Kept as
// a single source of truth so the run-window and the claimable/blocked filters
// agree.
const statusTerminalRunStatesSQL = "('completed', 'failed', 'canceled')"

// statusRunStateIsTerminal reports whether a run state is terminal, mirroring
// statusTerminalRunStatesSQL. A terminal run can have no live lane, so its
// sessions are never liveness-probed (#417).
func statusRunStateIsTerminal(state string) bool {
	switch state {
	case "completed", "failed", "canceled":
		return true
	default:
		return false
	}
}

// defaultStatusTerminalRunLimit bounds how many recent TERMINAL runs the
// repo-wide default surfaces (non-terminal runs are always included in full).
const defaultStatusTerminalRunLimit = 20

// statusRunScope describes which runs a repo-wide status read should cover.
type statusRunScope struct {
	runID    string // non-empty: scoped to exactly this run (window ignored)
	allRuns  bool   // --all-runs: no bounding (the legacy full-history behavior)
	runLimit int    // recent-terminal-run cap for the repo-wide default
}

func resolveStatusRunScope(envelope rpc.Envelope, runID string) statusRunScope {
	scope := statusRunScope{runID: runID, runLimit: defaultStatusTerminalRunLimit}
	if statusBoolFlag(envelope, "all_runs") {
		scope.allRuns = true
	}
	if v, ok := statusIntFlag(envelope, "run_limit"); ok && v >= 0 {
		scope.runLimit = v
	}
	return scope
}

// statusBoolFlag reads an advisory boolean status flag. Status is a read path,
// so a malformed flag falls back to the default rather than failing the whole
// snapshot.
func statusBoolFlag(envelope rpc.Envelope, key string) bool {
	value, _ := envelope.Params[key].(bool)
	return value
}

// statusIntFlag reads an advisory integer status flag, tolerating the int /
// int64 / float64 forms the JSON/CLI param plumbing can produce. ok=false when
// the flag is absent or not a number (the caller keeps its default).
func statusIntFlag(envelope rpc.Envelope, key string) (int, bool) {
	raw, ok := envelope.Params[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		parsed, err := v.Int64()
		if err == nil {
			return int(parsed), true
		}
	case string:
		parsed, err := strconv.Atoi(v)
		if err == nil {
			return parsed, true
		}
	default:
		return 0, false
	}
	return 0, false
}

// repoWideBounded reports whether the scope is the bounded repo-wide default
// (no run_id, not --all-runs).
func (s statusRunScope) repoWideBounded() bool {
	return s.runID == "" && !s.allRuns
}

func statusRuns(ctx context.Context, runner db.Runner, repositoryID string, scope statusRunScope) ([]map[string]any, error) {
	args := []any{repositoryID}
	var query string
	switch {
	case scope.runID != "":
		args = append(args, scope.runID)
		query = `SELECT r.run_id, r.state, r.branch_name, r.repo_root
		           FROM striatumd.runs r
		          WHERE r.repository_id = $1 AND r.run_id = $2
		          ORDER BY r.created_at, r.run_id`
	case scope.repoWideBounded():
		// Every non-terminal run, plus the most recent N terminal runs. The LIMIT
		// is scoped to the terminal arm (its own subquery) so it never truncates
		// the always-included non-terminal runs.
		args = append(args, scope.runLimit)
		query = `WITH bounded AS (
		            SELECT r.run_id, r.state, r.branch_name, r.repo_root, r.created_at
		              FROM striatumd.runs r
		             WHERE r.repository_id = $1
		               AND r.state NOT IN ` + statusTerminalRunStatesSQL + `
		             UNION ALL
		            SELECT t.run_id, t.state, t.branch_name, t.repo_root, t.created_at
		              FROM (
		                SELECT r.run_id, r.state, r.branch_name, r.repo_root, r.created_at
		                  FROM striatumd.runs r
		                 WHERE r.repository_id = $1
		                   AND r.state IN ` + statusTerminalRunStatesSQL + `
		                 ORDER BY r.created_at DESC, r.run_id DESC
		                 LIMIT $2
		              ) t
		          )
		          SELECT run_id, state, branch_name, repo_root
		            FROM bounded
		           ORDER BY created_at, run_id`
	default: // --all-runs: legacy full-history behavior
		query = `SELECT r.run_id, r.state, r.branch_name, r.repo_root
		           FROM striatumd.runs r
		          WHERE r.repository_id = $1
		          ORDER BY r.created_at, r.run_id`
	}
	rows, err := collectRows(ctx, runner, query, args...)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		decorateBranchDivergence(row)
	}
	return rows, nil
}

// decorateBranchDivergence implements #71: when branch.mode=auto sets
// runs.branch_name (run metadata) but the target repo working tree stays on a
// different branch (e.g. master), generated work can silently land on the wrong
// branch. This is advisory only — it never fails the read. When both the
// stored branch_name and the actual checkout branch are known and differ, the
// run gets a `branch_divergence` warning naming both branches; otherwise the
// field is omitted.
func decorateBranchDivergence(row map[string]any) {
	stored := strings.TrimSpace(stringFrom(row, "branch_name"))
	repoRoot := strings.TrimSpace(stringFrom(row, "repo_root"))
	if stored == "" || repoRoot == "" {
		return
	}
	actual := strings.TrimSpace(currentGitBranch(repoRoot))
	if actual == "" || actual == stored {
		return
	}
	row["branch_divergence"] = map[string]any{
		"expected_branch": stored,
		"actual_branch":   actual,
		"warning": fmt.Sprintf(
			"run branch_name %q does not match the target repository's current checkout branch %q; generated work may land on the wrong branch",
			stored, actual,
		),
	}
}

// currentGitBranch returns the target repository's current checkout branch via
// a read-only `git -C <repoRoot> branch --show-current`. It returns "" on any
// failure (not a git repo, detached HEAD, git missing) so the divergence check
// stays advisory and never fails the read. `--show-current` mirrors the
// pkg/mutations helper and correctly reports the branch even on an unborn
// branch with no commits yet (where `rev-parse HEAD` would fail). This is a
// local read-only helper rather than reaching into pkg/mutations.
func currentGitBranch(repoRoot string) string {
	cmd := exec.Command("git", "-C", repoRoot, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func statusJobCounts(ctx context.Context, runner db.Runner, repositoryID, runID string) (map[string]int, error) {
	where := "j.repository_id = $1"
	args := []any{repositoryID}
	if runID != "" {
		where += " AND j.run_id = $2"
		args = append(args, runID)
	}
	rows, err := collectRows(ctx, runner,
		`SELECT j.state, COUNT(*) AS count
		   FROM striatumd.jobs j
		  WHERE `+where+`
		  GROUP BY j.state
		  ORDER BY j.state`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, row := range rows {
		out[stringFrom(row, "state")] = intFrom(row, "count")
	}
	return out, nil
}

func statusSessions(ctx context.Context, runner db.Runner, repositoryID string, scope statusRunScope) ([]map[string]any, error) {
	where := "s.repository_id = $1"
	args := []any{repositoryID}
	if scope.runID != "" {
		where += " AND s.run_id = $2"
		args = append(args, scope.runID)
	} else if scope.repoWideBounded() {
		// #480: repo-wide status should expose the current operator frontier, not
		// every historical session ever registered for the repository. Keep
		// run-scoped and --all-runs detail intact; bounded repo-wide status tracks
		// only active sessions on non-terminal runs.
		where += " AND r.state NOT IN " + statusTerminalRunStatesSQL
		where += " AND s.state = 'active'"
	}
	rows, err := collectRows(ctx, runner,
		`SELECT s.session_id, s.run_id, s.role_id, s.lane_id, s.slug,
		        s.ordinal, s.state, s.operator_label,
		        s.registered_at,
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
		        r.state AS run_state,
		        ps.supervisor_id AS supervisor_id, ps.pid AS pid,
		        ptr.metadata_json AS supervisor_metadata_json,
		        active_lease.lease_id AS active_lease_id,
		        active_lease.acquired_at AS active_lease_acquired_at,
		        active_lease.expires_at AS active_lease_expires_at,
		        active_lease.last_heartbeat_at AS active_lease_last_heartbeat_at
		   FROM striatumd.sessions s
		   LEFT JOIN striatumd.runs r
		     ON r.repository_id = s.repository_id
		    AND r.run_id = s.run_id
		   LEFT JOIN striatumd.process_supervisors ps
		     ON ps.repository_id = s.repository_id
		    AND ps.session_id = s.session_id
		    AND ps.state = 'attached'
		   LEFT JOIN striatumd.process_supervisor_pointers ptr
		     ON ptr.repository_id = ps.repository_id
		    AND ptr.supervisor_id = ps.supervisor_id
		   LEFT JOIN LATERAL (
		     SELECT l.lease_id, l.acquired_at, l.expires_at, l.last_heartbeat_at
		       FROM striatumd.leases l
		      WHERE l.repository_id = s.repository_id
		        AND l.owner_session_id = s.session_id
		        AND l.state = 'active'
		      ORDER BY l.acquired_at DESC, l.lease_id DESC
		      LIMIT 1
		   ) active_lease ON true
		  WHERE `+where+`
		  ORDER BY s.registered_at, s.session_id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, row := range rows {
		// #417: a session on a terminal run is not live. Blanking its supervisor
		// columns routes it through the no-attached-supervisor path below, so the
		// loop neither drains its helper FIFO nor sudo+tmux ProbeLaneLiveness it.
		// Supervisors are reaped to a terminal state on run completion; this also
		// guards any terminal path that leaks a still-'attached' row from
		// re-storming the repo-wide status read (the #417 30s-timeout incident).
		runStateTerminal := statusRunStateIsTerminal(stringFrom(row, "run_state"))
		delete(row, "run_state")
		if runStateTerminal {
			row["supervisor_id"] = ""
			row["pid"] = nil
		}
		row["liveness"] = sessionliveness.ProjectionFromRow(row, now)
		sessionliveness.RemoveProjectionSourceFields(row)
		supervisorID := stringFrom(row, "supervisor_id")
		if supervisorID != "" && DrainHelperEventsHook != nil {
			drainStatusHelperEvents(ctx, runner, repositoryID, supervisorID, row)
		}
		metadata := superviseObject(row["supervisor_metadata_json"])
		attachSupervisorTmux(row, "supervisor_metadata_json")
		if stringFrom(row, "supervisor_id") == "" {
			row["lane_attestation"] = "unattested"
			row["lane_attestation_reason"] = "no_attached_supervisor"
			row["pid"] = nil
			row["lane_backend"] = "none"
			row["delivery_state"] = "unknown"
			continue
		}
		pid, _ := intValueOptional(row["pid"])
		_ = attachTmuxLivenessFromMetadata(ctx, row, metadata, pid, "")
		checker := lanehealth.Checker{
			Probe: lanehealth.ProdProbe{Runner: superviseTmuxRunner},
		}
		health, err := checker.Check(ctx, runner, repositoryID, superviseString(row["session_id"]))
		if err == nil {
			legMap := lanehealth.LegacyMap(health)
			row["lane_attestation"] = legMap["state"]
			row["lane_attestation_reason"] = legMap["reason"]
			reconcileBenignAttachExit(row, health)
		} else {
			row["lane_attestation"] = "unattested"
			row["lane_attestation_reason"] = "no_attached_supervisor"
		}
	}
	return rows, nil
}

// statusHelperDrainLockTimeout bounds how long the status read path will wait on
// a supervisor row lock while opportunistically draining helper events (#193).
const statusHelperDrainLockTimeout = "500ms"

// drainStatusHelperEvents opportunistically drains a supervisor's helper FIFO
// from the status READ path so the freshest PTY metadata shows up in the
// snapshot. The drain takes `FOR UPDATE OF ps` on the supervisor row and writes
// heartbeats, so under recovery/report contention it could otherwise block the
// (read-only) status call for the full statement timeout — the >30s spike in
// #193. A short SET LOCAL lock_timeout makes a contended lock fail fast (55P03);
// the drain is then skipped, the transaction rolled back, and a
// `helper_drain_skipped` note recorded on the session row. Status is advisory —
// stale-by-one-cycle helper metadata is fine, a 30s+ stall is not — so degrading
// gracefully here is strictly better than blocking.
func drainStatusHelperEvents(ctx context.Context, runner db.Runner, repositoryID, supervisorID string, row map[string]any) {
	tx, err := beginHelperEventDrainTx(ctx, runner)
	if err != nil {
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	// Bound the lock wait so a contended supervisor row cannot stall the read.
	if err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+statusHelperDrainLockTimeout+`'`); err != nil {
		return
	}
	if err := DrainHelperEventsHook(ctx, tx, repositoryID, supervisorID); err != nil {
		// Lock-timeout (or any drain error): skip without failing the read, and
		// note it so a poller can tell the metadata may be one cycle stale.
		row["helper_drain_skipped"] = true
		return
	}
	if err := tx.Commit(ctx); err != nil {
		return
	}
	committed = true
	metaRows, err := collectRows(ctx, runner,
		`SELECT metadata_json FROM striatumd.process_supervisor_pointers
		  WHERE repository_id = $1 AND supervisor_id = $2`,
		repositoryID, supervisorID,
	)
	if err == nil && len(metaRows) > 0 {
		row["supervisor_metadata_json"] = metaRows[0]["metadata_json"]
	}
}

// decorateCheckpointResolveActions advertises the checkpoint.resolve actions
// available for each open human_checkpoint blocker. revision_routing
// checkpoints additionally accept `override` (issue #63 F2), which accepts the
// checkpoint as superseded by a recorded run-level decision and makes the
// downstream gate reachable without re-queueing the review (requires
// --decision-id).
func decorateCheckpointResolveActions(checkpoints []map[string]any) {
	for _, cp := range checkpoints {
		actions := []any{"continue", "cancel"}
		hints := map[string]any{
			"continue": "re-run the reviewer on the current branch (a revision cycle); fix the findings first, or it re-reproduces the verdict and re-opens this checkpoint",
			"cancel":   "cancel the gated work",
		}
		if stringFrom(cp, "blocker_kind") == "revision_routing" {
			actions = append(actions, "override")
			hints["continue"] = "re-review the revised branch (a revision cycle); fix the findings first, or it re-reproduces needs_revision and re-opens this checkpoint. Use override to proceed past the verdict instead of re-reviewing"
			hints["override"] = "proceed past the verdict, accepting it as superseded by a recorded decision; requires --decision-id"
		}
		cp["resolve_action_hints"] = hints
		cp["resolve_actions"] = actions
	}
}

func statusBlockers(ctx context.Context, runner db.Runner, repositoryID, runID, severity string) ([]map[string]any, error) {
	where := "b.repository_id = $1 AND b.state = 'open'"
	args := []any{repositoryID}
	runJoin := ""
	if runID != "" {
		where += " AND b.run_id = $" + intPlaceholder(len(args)+1)
		args = append(args, runID)
	} else {
		// #193 (extended): an open blocker / human_checkpoint on a TERMINAL run is
		// not actionable — the run can never grow new work, so resolving it changes
		// nothing. The repo-wide claimable_jobs / blocked_downstream_jobs views
		// already exclude terminal runs for exactly this reason; statusBlockers was
		// missed, so a canceled/completed run's stale blockers surfaced forever as
		// phantom open_blockers / human_checkpoints in the operator frontier (e.g. a
		// 21-day-old revision_routing checkpoint on a canceled run reading as pending
		// human work). Join runs and drop terminal-run blockers from the repo-wide
		// default. A run_id-scoped call is an explicit ask for one run (terminal or
		// not) and is left untouched. The blocker rows themselves are preserved as
		// durable provenance of why the run was blocked when it terminated.
		runJoin = `
		   JOIN striatumd.runs r
		     ON r.repository_id = b.repository_id
		    AND r.run_id = b.run_id
		    AND r.state NOT IN ` + statusTerminalRunStatesSQL
	}
	if severity != "" {
		where += " AND b.severity = $" + intPlaceholder(len(args)+1)
		args = append(args, severity)
	}
	return collectRows(ctx, runner,
		`SELECT b.blocker_id, b.run_id, b.job_id, b.session_id,
		        b.severity, b.blocker_kind, b.description, b.state,
		        b.created_at, b.payload_json, j.workflow_job_id,
		        j.state AS job_state
		   FROM striatumd.blockers b`+runJoin+`
		   LEFT JOIN striatumd.jobs j
		     ON j.repository_id = b.repository_id
		    AND j.job_id = b.job_id
		  WHERE `+where+`
		  ORDER BY b.created_at, b.blocker_id`,
		args...,
	)
}

func statusLatestNonAccepting(ctx context.Context, runner db.Runner, repositoryID string, scope statusRunScope) ([]map[string]any, error) {
	// #283: a non-accepting verdict that was superseded via
	// `recovery invalidate-job ... <decision_id>` (sets
	// superseded_by_decision_id) is no longer operator-actionable; a later
	// accepting verdict resolved the revision cycle. Exclude superseded rows so
	// status does not report a completed run as still needing a revision.
	where := "v.repository_id = $1 AND v.verdict NOT IN ('accept', 'accept_with_findings') AND v.superseded_by_decision_id IS NULL"
	args := []any{repositoryID}
	runJoin := ""
	if scope.runID != "" {
		where += " AND v.run_id = $2"
		args = append(args, scope.runID)
	} else if scope.repoWideBounded() {
		// #480: repo-wide status is an operator frontier, not an archival verdict
		// export. `--run-limit 0` used to hide runs while still returning every
		// historical non-accepting review verdict, producing an unbounded cold-start
		// payload. Keep run-scoped and --all-runs behavior intact, but make the
		// bounded repo-wide default report only verdicts on non-terminal runs.
		runJoin = `
		   JOIN striatumd.runs r
		     ON r.repository_id = v.repository_id
		    AND r.run_id = v.run_id
		    AND r.state NOT IN ` + statusTerminalRunStatesSQL
	}
	rows, err := collectRows(ctx, runner,
		// #282: also report whether the work this review covers was REVISED after
		// the verdict was recorded — an upstream job the review depends on published
		// an artifact newer than the verdict. That is the stale-review case (a build
		// re-implemented after a needs_revision, but this reviewer's verdict was
		// never refreshed) that silently blocks finalization with an
		// otherwise-complete job graph. Surfacing it lets status hand the operator a
		// precise recovery action instead of a buried verdict row.
		`SELECT DISTINCT ON (v.job_id)
		        v.verdict_id, v.run_id, v.job_id, j.workflow_job_id,
		        v.verdict, v.posture, v.created_at,
		        EXISTS (
		          SELECT 1
		            FROM striatumd.job_dependencies dep
		            JOIN striatumd.artifacts a
		              ON a.repository_id = dep.repository_id
		             AND a.job_id = dep.depends_on_job_id
		           WHERE dep.repository_id = v.repository_id
		             AND dep.job_id = v.job_id
		             AND a.created_at > v.created_at
		        ) AS upstream_revised_after_verdict
		   FROM striatumd.verdicts v`+runJoin+`
		   JOIN striatumd.jobs j
		     ON j.repository_id = v.repository_id
		    AND j.job_id = v.job_id
		  WHERE `+where+`
		  ORDER BY v.job_id, v.created_at DESC, v.verdict_id DESC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		jobID := stringFrom(row, "job_id")
		workflowJobID := stringFrom(row, "workflow_job_id")
		verdict := stringFrom(row, "verdict")
		if row["upstream_revised_after_verdict"] == true {
			row["recovery_action"] = fmt.Sprintf(
				"stale review: %s was revised after this %s verdict — rerun review job %s (run.retry_job), or supersede the verdict via `recovery invalidate-job %s <decision_id>` after recording the decision. Finalization stays blocked until this review's latest verdict is accepting.",
				workflowJobID, verdict, jobID, jobID)
		} else {
			row["recovery_action"] = fmt.Sprintf(
				"non-accepting review %s (%s): address the findings and rerun, or accept-with-override via `override-verdict`.",
				workflowJobID, verdict)
		}
	}
	return rows, nil
}

func statusVerdictsByPosture(ctx context.Context, runner db.Runner, repositoryID, runID string) (map[string]map[string]int, error) {
	where := "v.repository_id = $1"
	args := []any{repositoryID}
	if runID != "" {
		where += " AND v.run_id = $2"
		args = append(args, runID)
	}
	rows, err := collectRows(ctx, runner,
		`SELECT v.posture, v.verdict, COUNT(*) AS count
		   FROM striatumd.verdicts v
		  WHERE `+where+`
		  GROUP BY v.posture, v.verdict
		  ORDER BY v.posture, v.verdict`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]int{}
	for _, row := range rows {
		posture := stringFrom(row, "posture")
		verdict := stringFrom(row, "verdict")
		if _, ok := out[posture]; !ok {
			out[posture] = map[string]int{}
		}
		out[posture][verdict] = intFrom(row, "count")
	}
	return out, nil
}

func statusClaimableJobs(ctx context.Context, runner db.Runner, repositoryID string, scope statusRunScope) ([]map[string]any, error) {
	where := "q.repository_id = $1"
	args := []any{repositoryID}
	if scope.runID != "" {
		where += " AND q.run_id = $2"
		args = append(args, scope.runID)
	} else {
		// #193: a pending queue_message on a TERMINAL run (e.g. an orphan message
		// left when run.cancel canceled the jobs but did not cancel the message) is
		// not actionable work — no one should claim work for a completed/failed/
		// canceled run. Exclude terminal runs so claimable_jobs never enumerates
		// historical runs forever. The run_id-scoped call is left as-is (an explicit
		// ask for one run, terminal or not).
		where += " AND r.state NOT IN " + statusTerminalRunStatesSQL
	}
	return collectRows(ctx, runner,
		`SELECT q.run_id, q.job_id, j.workflow_job_id,
		        q.target_role_id AS role_id, q.target_lane_id AS lane_id,
		        COUNT(*) AS count
		   FROM striatumd.queue_messages q
		   JOIN striatumd.jobs j
		     ON j.repository_id = q.repository_id
		    AND j.job_id = q.job_id
		   JOIN striatumd.runs r
		     ON r.repository_id = q.repository_id
		    AND r.run_id = q.run_id
		  WHERE `+where+`
		    AND q.state = 'pending'
		    AND (q.visible_after IS NULL OR q.visible_after <= now())
		  GROUP BY q.run_id, q.job_id, j.workflow_job_id,
		           q.target_role_id, q.target_lane_id
		  ORDER BY q.target_role_id, q.target_lane_id, j.workflow_job_id`,
		args...,
	)
}

func statusBlockedDownstreamJobs(ctx context.Context, runner db.Runner, repositoryID string, scope statusRunScope) ([]map[string]any, error) {
	where := "j.repository_id = $1 AND j.state = 'blocked'"
	args := []any{repositoryID}
	if scope.runID != "" {
		where += " AND j.run_id = $2"
		args = append(args, scope.runID)
	} else {
		// #193: a blocked job on a terminal run cannot become actionable; exclude
		// terminal runs from the repo-wide enumeration.
		where += " AND r.state NOT IN " + statusTerminalRunStatesSQL
	}
	return collectRows(ctx, runner,
		`SELECT j.run_id, j.job_id, j.workflow_job_id, j.state
		   FROM striatumd.jobs j
		   JOIN striatumd.runs r
		     ON r.repository_id = j.repository_id
		    AND r.run_id = j.run_id
		  WHERE `+where+`
		  ORDER BY j.created_at, j.workflow_job_id`,
		args...,
	)
}

func statusProcessHealth(ctx context.Context, runner db.Runner, repositoryID, runID string) (map[string]any, error) {
	if runID == "" {
		return map[string]any{
			"running_count":       0,
			"stale_running_count": 0,
			"lost_count":          0,
			"timed_out_count":     0,
			"next_actions":        []string{},
		}, nil
	}
	where := "p.repository_id = $1"
	args := []any{repositoryID}
	where += " AND p.run_id = $2"
	args = append(args, runID)
	rows, err := collectRows(ctx, runner,
		`SELECT
		    COUNT(*) FILTER (WHERE p.state = 'running') AS running_count,
		    COUNT(*) FILTER (
		      WHERE p.state = 'running' AND l.state = 'expired'
		    ) AS stale_running_count,
		    COUNT(*) FILTER (WHERE p.state = 'lost') AS lost_count,
		    COUNT(*) FILTER (WHERE p.state = 'timed_out') AS timed_out_count
		   FROM striatumd.process_executions p
		   LEFT JOIN striatumd.leases l
		     ON l.repository_id = p.repository_id
		    AND l.lease_id = p.lease_id
		  WHERE `+where,
		args...,
	)
	if err != nil {
		return nil, err
	}
	row := map[string]any{}
	if len(rows) > 0 {
		row = rows[0]
	}
	nextActions := []string{}
	if intFrom(row, "stale_running_count") > 0 {
		nextActions = append(nextActions, "recovery_process_reconcile")
	}
	return map[string]any{
		"running_count":       intFrom(row, "running_count"),
		"stale_running_count": intFrom(row, "stale_running_count"),
		"lost_count":          intFrom(row, "lost_count"),
		"timed_out_count":     intFrom(row, "timed_out_count"),
		"next_actions":        nextActions,
	}, nil
}

func statusSupervisorStalls(ctx context.Context, runner db.Runner, repositoryID, runID string) (map[string]any, error) {
	return dashboardAllSupervisorStalls(ctx, runner, repositoryID, runID)
}

func statusWorkflowForRun(ctx context.Context, runner db.Runner, repositoryID, runID string) (map[string]any, error) {
	rows, err := collectRows(ctx, runner,
		`SELECT w.workflow_json
		   FROM striatumd.runs r
		   JOIN striatumd.workflow_snapshots w
		     ON w.repository_id = r.repository_id
		    AND w.workflow_snapshot_id = r.workflow_snapshot_id
		  WHERE r.repository_id = $1 AND r.run_id = $2`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return map[string]any{}, nil
	}
	return objectFromJSONish(rows[0]["workflow_json"]), nil
}

func statusHasLostSupervisorHeldLease(ctx context.Context, runner db.Runner, repositoryID, runID string) (bool, error) {
	where := "s.repository_id = $1 AND s.state = 'lost'"
	args := []any{repositoryID}
	if runID != "" {
		where += " AND s.run_id = $2"
		args = append(args, runID)
	}
	return rowExists(ctx, runner,
		`SELECT 1
		   FROM striatumd.process_supervisors s
		   JOIN striatumd.leases l
		     ON l.repository_id = s.repository_id
		    AND l.owner_session_id = s.session_id
		    AND l.state = 'active'
		    AND l.expires_at > now()
		  WHERE `+where+`
		  LIMIT 1`,
		args...,
	)
}

func statusHasStaleLeasesWithOnDiskArtifacts(ctx context.Context, runner db.Runner, repositoryID, runID string) (bool, error) {
	where := "j.repository_id = $1 AND l.state = 'expired' AND j.state IN ('claimed', 'running', 'stale_lease')"
	args := []any{repositoryID}
	if runID != "" {
		where += " AND j.run_id = $2"
		args = append(args, runID)
	}
	rows, err := collectRows(ctx, runner,
		`SELECT r.repo_root, j.expected_artifacts_json
		   FROM striatumd.jobs j
		   JOIN striatumd.runs r
		     ON r.repository_id = j.repository_id
		    AND r.run_id = j.run_id
		   JOIN striatumd.leases l
		     ON l.repository_id = j.repository_id
		    AND l.lease_id = j.current_lease_id
		  WHERE `+where,
		args...,
	)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		repoRoot := stringFrom(row, "repo_root")
		for _, artifact := range statusExpectedArtifacts(row["expected_artifacts_json"]) {
			required, ok := artifact["required"].(bool)
			if ok && !required {
				continue
			}
			path := stringValue(artifact["path"])
			if path == "" {
				continue
			}
			fullPath := path
			if !filepath.IsAbs(fullPath) {
				fullPath = filepath.Join(repoRoot, path)
			}
			if _, err := os.Stat(fullPath); err == nil {
				return true, nil
			}
		}
	}
	return false, nil
}

func statusExpectedArtifacts(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := []map[string]any{}
		for _, item := range typed {
			if artifact := objectFromJSONish(item); len(artifact) > 0 {
				out = append(out, artifact)
			}
		}
		return out
	case []byte:
		var decoded []map[string]any
		_ = json.Unmarshal(typed, &decoded)
		return decoded
	case string:
		var decoded []map[string]any
		_ = json.Unmarshal([]byte(typed), &decoded)
		return decoded
	default:
		return []map[string]any{}
	}
}

func statusNextActions(
	claimable []map[string]any,
	openBlockers []map[string]any,
	humanCheckpoints []map[string]any,
	nonAcceptingVerdicts []map[string]any,
	hasOrphanSupervisor bool,
	hasStaleLeases bool,
	processHealth map[string]any,
	supervisorStalls map[string]any,
	autoFinalize any,
) []string {
	out := []string{}
	if len(claimable) > 0 {
		out = append(out, "claim_available_work", "inspect_packet_with_inbox")
	}
	if hasOrphanSupervisor {
		out = append(out, "recover_orphan_supervisor")
	}
	if hasStaleLeases {
		out = append(out, "recovery_auto_publish")
	}
	if len(openBlockers) > 0 {
		out = append(out, "inspect_blocker", "export_run_evidence")
	}
	if len(humanCheckpoints) > 0 {
		out = append(out, "resolve_human_checkpoint", "derive_expected_byline")
	}
	if len(nonAcceptingVerdicts) > 0 {
		out = append(out, "revise_workflow_cycle", "derive_expected_byline")
	}
	// Only recommend recovery_auto_finalize when the dry-run confirms at least
	// one job/artifact is actually eligible (artifacts present on disk and
	// ready for finalization). candidate_count alone means jobs are running and
	// *might* be finalizable; eligible_count means they *are*. Recommending on
	// candidate_count alone produces a misleading next-action when no artifacts
	// have landed yet. (#124)
	if summary, ok := autoFinalize.(map[string]any); ok && statusAutoFinalizeLiveAllowed(summary) && intFrom(summary, "eligible_count") > 0 {
		out = appendUnique(out, "recovery_auto_finalize")
	}
	for _, action := range statusStringList(processHealth["next_actions"]) {
		if action != "" {
			out = appendUnique(out, action)
		}
	}
	for _, action := range statusStringList(supervisorStalls["next_actions"]) {
		if action != "" {
			out = appendUnique(out, action)
		}
	}
	return uniqueStrings(out)
}

func statusStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := []string{}
		for _, item := range typed {
			if text := stringValue(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return []string{}
	}
}

func appendUnique(items []string, item string) []string {
	for _, existing := range items {
		if existing == item {
			return items
		}
	}
	return append(items, item)
}

func uniqueStrings(items []string) []string {
	out := []string{}
	for _, item := range items {
		out = appendUnique(out, item)
	}
	return out
}

func statusAutoFinalizeLiveAllowed(summary map[string]any) bool {
	policy, ok := summary["policy"].(map[string]any)
	return ok && policy["live_allowed"] == true
}

func intPlaceholder(value int) string {
	return strconv.Itoa(value)
}

func stringFrom(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
