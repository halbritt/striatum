package reads

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/installers"
	"github.com/halbritt/striatum/go/pkg/rpc"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
)

const doctorSupervisorProbeTimeout = 5 * time.Second
const doctorRecoveryCursorWedgedAfter = 5 * time.Minute

// HandleDoctor mirrors reads/doctor.py. Returns a flat health report of
// the running daemon-pg state: schema version, append-only invariants
// detected via probe queries, stale-lease + waiting-human counts.
func HandleDoctor(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	if strings.TrimSpace(stringParam(envelope, "lane_provider_auth")) != "" {
		return HandleDoctorLaneProviderAuth(ctx, runner, envelope)
	}
	repositoryID, _ := envelope.Params["repository_id"].(string)
	verbose := boolValue(envelope.Params["verbose"])
	problems := []string{}
	problemRecords := []map[string]any{}
	warningRecords := []map[string]any{}

	schemaVersion, err := db.ReadSchemaVersion(ctx, runner)
	if err != nil {
		problems = append(problems, "schema_meta.read_failed: "+err.Error())
	}

	staleLeases := 0
	if repositoryID != "" {
		// #45: an expired lease row persists forever, so counting every
		// `state = 'expired'` lease keeps reporting stale leases long after the
		// work was recovered/completed. A lease is only genuinely stale when it
		// is still a job's CURRENT lease AND that job is still actionable
		// (claimed/running/stale_lease) — matching the authoritative predicate in
		// status.go. Recovery or completion swaps current_lease_id / advances the
		// job state, which makes the count drop as expected.
		rows, err := collectRows(ctx, runner,
			`SELECT COUNT(*) AS c
			   FROM striatumd.jobs j
			   JOIN striatumd.leases l
			     ON l.repository_id = j.repository_id
			    AND l.lease_id = j.current_lease_id
			  WHERE j.repository_id = $1
			    AND l.state = 'expired'
			    AND j.state IN ('claimed', 'running', 'stale_lease')`,
			repositoryID,
		)
		if err == nil && len(rows) > 0 {
			if c, ok := rows[0]["c"]; ok {
				switch v := c.(type) {
				case int64:
					staleLeases = int(v)
				case int:
					staleLeases = v
				case float64:
					staleLeases = int(v)
				}
			}
		}
	}

	waitingHuman := 0
	if repositoryID != "" {
		rows, err := collectRows(ctx, runner,
			`SELECT COUNT(*) AS c FROM striatumd.runs
			  WHERE repository_id = $1 AND state = 'waiting_human'`,
			repositoryID,
		)
		if err == nil && len(rows) > 0 {
			if c, ok := rows[0]["c"]; ok {
				switch v := c.(type) {
				case int64:
					waitingHuman = int(v)
				case int:
					waitingHuman = v
				case float64:
					waitingHuman = int(v)
				}
			}
		}
	}
	// RFC 0101 Phase 4: a run flipped to needs_operator by the recovery sweep
	// (autonomous recovery exhausted its per-job budget) is a LOUD, actionable
	// problem — not a warning. Surface the count + the specific run ids and add
	// each to `problems` with the escalation reason so `ok` goes false.
	needsOperatorRuns := []string{}
	if repositoryID != "" {
		rows, err := collectRows(ctx, runner,
			`SELECT run_id FROM striatumd.runs
			  WHERE repository_id = $1 AND state = 'needs_operator'
			  ORDER BY run_id`,
			repositoryID,
		)
		if err == nil {
			for _, row := range rows {
				runID := superviseString(row["run_id"])
				if runID == "" {
					continue
				}
				needsOperatorRuns = append(needsOperatorRuns, runID)
				problems = append(problems, "run_needs_operator."+runID+": autonomous recovery exhausted; resolve the recovery_exhausted escalation (escalation resolve) or cancel the run")
			}
		}
	}

	supervisorLiveness := []map[string]any{}
	if repositoryID != "" {
		probeCtx, cancel := context.WithTimeout(ctx, doctorSupervisorProbeTimeout)
		defer cancel()
		if rows, err := reattachStatusRowsWithOptions(probeCtx, runner, repositoryID, "", "", "", reattachStatusRowsOptions{nonTerminalRunsOnly: true}); err == nil {
			for _, row := range rows {
				view := reattachStatusView(probeCtx, row)
				class := superviseString(view["lane_liveness_class"])
				if !strings.HasPrefix(class, "tmux_") && class != string(gosupervisor.TmuxLivenessHelperDetachedProcessAlive) {
					continue
				}
				deliveryLiveness := superviseObject(view["delivery_liveness"])
				deliveryClass := superviseString(deliveryLiveness["class"])
				deliveryReason := superviseString(deliveryLiveness["reason"])

				item := map[string]any{
					"supervisor_id":  view["supervisor_id"],
					"session_id":     view["session_id"],
					"class":          class,
					"state":          view["reattach_state"],
					"reason":         view["reattach_reason"],
					"lane_backend":   view["lane_backend"],
					"trajectory_log": view["trajectory_log"],
				}
				if deliveryClass == "degraded" {
					if remediation := deliveryRemediation(deliveryReason, superviseString(view["session_id"])); remediation != "" {
						item["remediation"] = remediation
					}
				} else if remediation := tmuxLivenessRemediation(class, superviseString(view["reattach_reason"]), superviseString(view["session_id"])); remediation != "" {
					item["remediation"] = remediation
				}
				supervisorLiveness = append(supervisorLiveness, item)
				if view["reattach_state"] != "terminal" && class != string(gosupervisor.TmuxLivenessOK) && class != string(gosupervisor.TmuxLivenessUnavailable) {
					problems = append(problems, "supervisor_liveness."+superviseString(view["supervisor_id"])+": "+class)
				}
				if deliveryClass == "degraded" {
					problems = append(problems, "supervisor_delivery_degraded."+superviseString(view["supervisor_id"])+": "+deliveryReason)
				}
			}
		}
	}

	// #64: advise (warn, never hard-fail) when ~/.codex/config.toml points at a
	// stale MCP endpoint or codex would start without a bearer. The token VALUE
	// is never read or returned.
	codexBlock, codexWarnings := codexDoctorBlock()
	warnings := append([]string{}, codexWarnings...)

	// #87 / RFC 0096 §2: surface when supervised lanes are not isolated from the
	// daemon's PostgreSQL by a dedicated PG-less lane OS user. This is advisory
	// by default and blocking under the RFC 0110 secure profile. Configuration
	// posture proxy only; no DSN/token value is read.
	laneSandboxBlock, laneSandboxWarnings, laneSandboxProblems := laneSandboxDoctorBlock()
	warnings = append(warnings, laneSandboxWarnings...)
	problems = append(problems, laneSandboxProblems...)

	// RFC 0107: surface the configured principals and each one's capability/repo
	// scope so the operator can see who can do what, on which repositories, on
	// this self-hosted daemon. Daemon-global (independent of repository_id);
	// never reads or returns token material.
	principalsBlock := principalsDoctorBlock(ctx, runner)

	// RFC 0110: report the daemon->PostgreSQL write-boundary posture and the
	// bounded-discard reconnect signal. Posture is "none" in release N (no phase
	// has closed a surface); never reads or returns any secret.
	pgWriteBoundaryBlock, pgWriteBoundaryWarnings := pgWriteBoundaryDoctorBlock()
	warnings = append(warnings, pgWriteBoundaryWarnings...)

	// RFC 0110 / #164: report the separate read-scope posture. This is not part
	// of pg_write_boundary because current phases intentionally do not claim
	// private-read denial for a live runtime credential. Since RFC 0114 the
	// posture is derived from schema_authority stamps + live privilege and
	// ownership probes rather than hard-coded.
	pgReadScopeBlock := pgReadScopeDoctorBlock(ctx, runner)

	eventLockWaitBlock, auditLockWaitBlock, lockWaitWarnings, lockWaitWarningRecords := doctorLockWaitConvoys(ctx, runner, repositoryID, time.Now().UTC())
	warnings = append(warnings, lockWaitWarnings...)
	warningRecords = append(warningRecords, lockWaitWarningRecords...)

	worktreeRefSafetyBlock, worktreeProblems, worktreeProblemRecords, worktreeWarnings, worktreeWarningRecords := doctorWorktreeRefSafety(ctx, runner, repositoryID)
	problems = append(problems, worktreeProblems...)
	problemRecords = append(problemRecords, worktreeProblemRecords...)
	warnings = append(warnings, worktreeWarnings...)
	warningRecords = append(warningRecords, worktreeWarningRecords...)

	skillsBlock := map[string]any{"checked": false}
	if repoRoot := doctorRepoRoot(ctx, runner, repositoryID); repoRoot != "" {
		home, _ := os.UserHomeDir()
		skillsHealth, err := installers.CheckSkillsHealth(installers.SkillsHealthParams{
			Target:         repoRoot,
			Home:           home,
			CurrentVersion: packageStriatumVersion,
		})
		if err == nil {
			skillsBlock = map[string]any{
				"checked":         skillsHealth.Checked,
				"current_version": skillsHealth.CurrentVersion,
				"items":           skillsHealth.Items,
			}
			warnings = append(warnings, installers.SkillsHealthWarnings(skillsHealth)...)
		} else {
			warnings = append(warnings, "skills_check_failed: "+err.Error())
		}
	}

	blobBlock := blobDoctorBlock(ctx, runner, repositoryID)
	artifactAnchorBlock, artifactAnchorProblems, artifactAnchorRecords, artifactAnchorWarnings, artifactAnchorWarningRecords := doctorArtifactAnchorIntegrity(ctx, runner, repositoryID, blobBlock)
	problems = append(problems, artifactAnchorProblems...)
	problemRecords = append(problemRecords, artifactAnchorRecords...)
	warnings = append(warnings, artifactAnchorWarnings...)
	warningRecords = append(warningRecords, artifactAnchorWarningRecords...)

	// RFC 0135 P3 (#347): the generalized barrier integrity invariant over the
	// sealed expectation barrier tables (freeze / staging / barrier_state), surfaced
	// through the migration-0031 striatumd.barrier_status view. It detects a
	// BARRIER_BLOCKED condition (a live blocking in-edge), an 'assembling' barrier
	// whose journaled target commit is unreachable, a 'committed' barrier whose
	// manifest disagrees with the staged refs at the live seal, and orphaned staging
	// refs. It subsumes the per-integration fanin_sibling_unintegrated warning at the
	// barrier level (that per-worktree warning remains the worktree-scoped view).
	barrierBlock, barrierProblems, barrierRecords, barrierWarnings, barrierWarningRecords := doctorBarrierIntegrity(ctx, runner, repositoryID)
	problems = append(problems, barrierProblems...)
	problemRecords = append(problemRecords, barrierRecords...)
	warnings = append(warnings, barrierWarnings...)
	warningRecords = append(warningRecords, barrierWarningRecords...)

	// RFC 0132 P4b (#342): the panel-quorum / advisory-dissent integrity invariant.
	// It detects a structurally unresolvable quorum seat (a declared seat with no job
	// row — a permanent fail-closed deadlock), a frozen/live denominator mismatch, a
	// completed gate that finalized while ignoring a live advisory dissent, and the
	// #339 dissent-ledger completeness hole (a live blocking verdict with no
	// forward-written dissent_ledger row).
	quorumBlock, quorumProblems, quorumRecords, quorumWarnings, quorumWarningRecords := doctorQuorumIntegrity(ctx, runner, repositoryID)
	problems = append(problems, quorumProblems...)
	problemRecords = append(problemRecords, quorumRecords...)
	warnings = append(warnings, quorumWarnings...)
	warningRecords = append(warningRecords, quorumWarningRecords...)

	recoveryCursorBlock, recoveryCursorProblems, recoveryCursorRecords := doctorRecoverySweepCursor(ctx, runner, repositoryID, time.Now().UTC())
	problems = append(problems, recoveryCursorProblems...)
	problemRecords = append(problemRecords, recoveryCursorRecords...)

	// RFC 0131 131-D (#337): the confidence-gate escape-valve safety-floor invariant.
	// A job whose consecutive_silent_sweeps reached its escape-valve cap but is NOT
	// escalation_pending on a still-actionable run is a never-un-escalatable breach
	// (Layer 4 Goal 3): the cap must always eventually fire. Hard RED — the safety
	// floor RFC 0131 forbids letting fail is itself observable here.
	recoveryGateBlock, recoveryGateProblems, recoveryGateRecords := doctorRecoveryGateIntegrity(ctx, runner, repositoryID)
	problems = append(problems, recoveryGateProblems...)
	problemRecords = append(problemRecords, recoveryGateRecords...)

	// #373/#388/#389: surface the wedge family the recovery_sweep_cursor check
	// could not see — a non-terminal job with NO live session and no recent
	// progress yields 0 claimable jobs (a sessionless running job, or a blocked
	// job) so doctor stayed green while the run deadlocked. This is a WARNING (not
	// a hard red): it must tolerate normal in-flight latency and never re-red a
	// green baseline on a healthy actively-running job.
	stuckJobBlock, stuckJobWarnings, stuckJobWarningRecords := doctorStuckJobsNoLiveSession(ctx, runner, repositoryID, time.Now().UTC())
	warnings = append(warnings, stuckJobWarnings...)
	warningRecords = append(warningRecords, stuckJobWarningRecords...)

	// RFC 0136 P1 (D242 / #387): the event_chain_segment_seam_unproven invariant.
	// A sealed event-chain segment (migration 0041) is the unit of retention; a
	// future partition DROP may only retire a sealed, hash-witnessed range. This
	// check reds when a sealed segment lacks its boundary hashes or its
	// cross-segment seam witnesses do not link to its predecessor — i.e. when the
	// chain's provable continuity across the (future) dropped-rows seam is broken.
	// It skips cleanly on a DB where migration 0041 has not yet applied.
	eventSegmentBlock, eventSegmentProblems, eventSegmentRecords := doctorEventChainSegmentSeams(ctx, runner, repositoryID)
	problems = append(problems, eventSegmentProblems...)
	problemRecords = append(problemRecords, eventSegmentRecords...)

	result := map[string]any{
		"ok":                           len(problems) == 0,
		"schema_version":               schemaVersion,
		"stale_leases":                 staleLeases,
		"waiting_human":                waitingHuman,
		"needs_operator":               len(needsOperatorRuns),
		"needs_operator_runs":          needsOperatorRuns,
		"supervisors":                  supervisorLiveness,
		"problems":                     problems,
		"warnings":                     warnings,
		"codex":                        codexBlock,
		"lane_sandbox":                 laneSandboxBlock,
		"principals":                   principalsBlock,
		"pg_write_boundary":            pgWriteBoundaryBlock,
		"pg_read_scope":                pgReadScopeBlock,
		"event_chain_head_lock_convoy": eventLockWaitBlock,
		"audit_chain_head_lock_convoy": auditLockWaitBlock,
		"worktree_ref_safety":          worktreeRefSafetyBlock,
		"artifact_anchor_integrity":    artifactAnchorBlock,
		"barrier_integrity":            barrierBlock,
		"quorum_integrity":             quorumBlock,
		"recovery_sweep_cursor":        recoveryCursorBlock,
		"recovery_escape_valve":        recoveryGateBlock,
		"job_stuck_no_live_session":    stuckJobBlock,
		"event_chain_segment_seams":    eventSegmentBlock,
		"skills":                       skillsBlock,
		"blob":                         blobBlock,
	}
	if verbose {
		result["problem_records"] = problemRecords
		result["warning_records"] = warningRecords
	}
	return result, nil
}

func doctorRecoverySweepCursor(ctx context.Context, runner db.Runner, repositoryID string, now time.Time) (map[string]any, []string, []map[string]any) {
	block := map[string]any{
		"checked": false,
		"skipped": "repository_id required",
	}
	if repositoryID == "" {
		return block, nil, nil
	}
	quietBefore := now.UTC().Add(-doctorRecoveryCursorWedgedAfter)
	rows, err := collectRows(ctx, runner, `
		SELECT r.run_id,
		       r.state AS run_state,
		       c.state AS cursor_state,
		       c.last_sweep_at,
		       COALESCE(c.last_result_json->>'claimable_job_count', '0') AS claimable_job_count,
		       c.last_result_json->>'last_lane_advanced_at' AS last_lane_advanced_at,
		       c.last_result_json->>'recovery_cursor_latch_error' AS recovery_cursor_latch_error,
		       r.started_at,
		       r.created_at
		  FROM striatumd.runs r
		  JOIN striatumd.scheduler_cursors c
		    ON c.repository_id = r.repository_id
		   AND c.run_id = r.run_id
		   AND c.cursor_kind = 'recovery'
		 WHERE r.repository_id = $1
		   AND r.state = 'running'
		   AND c.state <> 'removed'
		 ORDER BY COALESCE(r.started_at, r.created_at), r.run_id`,
		repositoryID)
	if err != nil {
		block["checked"] = true
		block["skipped"] = nil
		block["error"] = err.Error()
		return block, []string{"recovery_sweep_cursor.read_failed: " + err.Error()}, []map[string]any{{
			"check": "recovery_sweep_cursor_read_failed",
			"error": err.Error(),
		}}
	}
	block["checked"] = true
	block["skipped"] = nil

	wedged := []map[string]any{}
	latchErrors := []map[string]any{}
	problems := []string{}
	records := []map[string]any{}
	for _, row := range rows {
		if stringFrom(row, "run_state") != "running" {
			continue
		}
		runID := stringFrom(row, "run_id")
		if latchError := stringFrom(row, "recovery_cursor_latch_error"); latchError != "" {
			record := map[string]any{
				"check":         "recovery_sweep_cursor_latch_error",
				"run_id":        runID,
				"error":         latchError,
				"last_sweep_at": row["last_sweep_at"],
				"cursor_state":  row["cursor_state"],
			}
			latchErrors = append(latchErrors, record)
			records = append(records, record)
			problems = append(problems, "recovery_sweep_cursor_latch_error."+runID+": "+latchError)
			continue
		}
		claimableJobCount := intFrom(row, "claimable_job_count")
		if claimableJobCount <= 0 {
			continue
		}
		quietSince, ok := recoveryCursorQuietSince(row)
		if !ok || !quietSince.Before(quietBefore) {
			continue
		}
		lastLaneAdvancedAt := stringFrom(row, "last_lane_advanced_at")
		record := map[string]any{
			"check":                 "recovery_sweep_cursor_wedged",
			"run_id":                runID,
			"claimable_job_count":   claimableJobCount,
			"last_lane_advanced_at": lastLaneAdvancedAt,
			"quiet_since":           quietSince.UTC().Format(time.RFC3339),
			"threshold_seconds":     int(doctorRecoveryCursorWedgedAfter.Seconds()),
			"last_sweep_at":         row["last_sweep_at"],
			"cursor_state":          row["cursor_state"],
			"latch_error":           row["recovery_cursor_latch_error"],
		}
		wedged = append(wedged, record)
		records = append(records, record)
		problems = append(problems, "recovery_sweep_cursor_wedged."+runID+": claimable_job_count="+intPlaceholder(claimableJobCount)+" and no lane advance since "+record["quiet_since"].(string))
	}
	block["latch_error_count"] = len(latchErrors)
	block["latch_errors"] = latchErrors
	block["wedged_count"] = len(wedged)
	block["wedged_runs"] = wedged
	return block, problems, records
}

func recoveryCursorQuietSince(row map[string]any) (time.Time, bool) {
	if quietSince, ok := parseTimeValue(row["last_lane_advanced_at"]); ok {
		return quietSince, true
	}
	if quietSince, ok := parseTimeValue(row["started_at"]); ok {
		return quietSince, true
	}
	if quietSince, ok := parseTimeValue(row["created_at"]); ok {
		return quietSince, true
	}
	return parseTimeValue(row["quiet_since"])
}

func doctorRepoRoot(ctx context.Context, runner any, repositoryID string) string {
	if repositoryID == "" {
		return ""
	}
	rows, err := collectRows(ctx, runner,
		`SELECT repo_root FROM striatumd.repositories
		  WHERE repository_id = $1 AND state != 'removed'
		  ORDER BY registered_at DESC LIMIT 1`,
		repositoryID,
	)
	if err != nil || len(rows) == 0 {
		return ""
	}
	return strings.TrimSpace(stringFrom(rows[0], "repo_root"))
}
