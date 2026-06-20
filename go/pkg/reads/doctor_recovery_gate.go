package reads

import (
	"context"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
)

// RFC 0131 131-D (#337) — the confidence-gate / escape-valve doctor invariant.
//
// RFC 0131 Layer 4 (131-C) is built on a load-bearing safety guarantee: the
// escape-valve cap MUST always eventually fire, so a genuinely-hung pipe / no-
// oracle lane is escalatable in bounded time by construction (Goal 3, "never
// create an un-escalatable lane"). The gate increments consecutive_silent_sweeps
// every deadline_elapsed_only sweep with no forgery-resistant progress, and the
// SAME sweep that drives the counter to the cap must escalate (set
// escalation_pending). So in a HEALTHY daemon a job whose
// consecutive_silent_sweeps has reached its escape-valve cap is ALWAYS also
// escalation_pending.
//
// This doctor block surfaces the breach of that invariant — the RFC's own
// "A doctor warning when a pipe lane has exceeded the cap but the run did not
// escalate (an invariant breach), so the safety floor is itself observable."
// A job_recovery_state row whose consecutive_silent_sweeps >= its cap but which
// is NOT escalation_pending, on a run that is still actionable (running /
// waiting_human), means the cap failed to fire — a never-un-escalatable
// violation. It is a hard RED (problem), not a warning: it is exactly the
// safety-floor failure RFC 0131 forbids, and it cannot be a benign transient
// (the gate sets escalation_pending co-transactionally with reaching the cap, so
// the two are never legitimately out of step on a non-terminal run).
//
// The per-job cap is computed the same way mutations.recoveryPolicy.silentSweepCap
// does — the operator override recovery_policy.max_silent_sweeps when set, else
// (max_requeues*2)+3 with a +3 floor — read from each job's run workflow snapshot
// so the check uses the cap that actually governed the row.
//
// Degrade-safe: a deployment behind migration 0035 has no confidence-gate columns,
// so the information_schema guard short-circuits and the block reports
// checked=false (the pre-131-C ungated path never accrues silent sweeps).
const doctorRecoveryGateCheck = "recovery_escape_valve_uncapped"

// doctorRecoveryGateIntegrity flags the RFC 0131 Layer-4 escape-valve invariant
// breach: a job at/over its silent-sweep cap that is not escalation_pending on a
// still-actionable run. Returns the block, hard problems, and verbose records.
func doctorRecoveryGateIntegrity(ctx context.Context, runner db.Runner, repositoryID string) (map[string]any, []string, []map[string]any) {
	block := map[string]any{
		"checked":      false,
		"check":        doctorRecoveryGateCheck,
		"skipped":      "repository_id required",
		"breach_count": 0,
		"breaches":     []map[string]any{},
	}
	if repositoryID == "" {
		return block, nil, nil
	}
	if !jobRecoveryConfidenceColumnsExist(ctx, runner) {
		// A daemon behind migration 0035 runs the pre-131-C ungated path, which
		// never accrues consecutive_silent_sweeps, so there is nothing to check.
		block["skipped"] = "migration 0035 (confidence-gate columns) not applied"
		return block, nil, nil
	}

	// Per-job cap derivation in SQL, mirroring recoveryPolicy.silentSweepCap():
	//   - recovery_policy.max_silent_sweeps when set (> 0) wins;
	//   - else (max_requeues*2)+3, where max_requeues falls back to the documented
	//     RFC 0020 max_total_requeues_per_job, then the default 2;
	//   - floored at 3 so a requeue-disabled workflow is still bounded.
	// The cap governs only the deadline_elapsed_only pipe / no-oracle gate, but the
	// breach (cap reached, not escalated) is transport-independent: any row that
	// accrued silent sweeps to the cap without escalating is a safety-floor failure.
	rows, err := collectRows(ctx, runner, `
		WITH gate AS (
			SELECT jrs.repository_id, jrs.run_id, jrs.job_id,
			       jrs.consecutive_silent_sweeps,
			       jrs.misfire_evidence_score,
			       jrs.last_probe_basis,
			       jrs.escalation_pending,
			       jrs.last_recovery_at,
			       j.workflow_job_id,
			       r.state AS run_state,
			       COALESCE(
			         NULLIF((w.workflow_json #>> '{recovery_policy,max_silent_sweeps}'), '')::int,
			         GREATEST(
			           (COALESCE(
			              NULLIF((w.workflow_json #>> '{recovery_policy,max_requeues}'), '')::int,
			              NULLIF((w.workflow_json #>> '{recovery_policy,max_total_requeues_per_job}'), '')::int,
			              2
			            ) * 2) + 3,
			           3
			         )
			       ) AS silent_sweep_cap
			  FROM striatumd.job_recovery_state jrs
			  JOIN striatumd.jobs j
			    ON j.repository_id = jrs.repository_id AND j.job_id = jrs.job_id
			  JOIN striatumd.runs r
			    ON r.repository_id = jrs.repository_id AND r.run_id = jrs.run_id
			  LEFT JOIN striatumd.workflow_snapshots w
			    ON w.repository_id = r.repository_id
			   AND w.workflow_snapshot_id = r.workflow_snapshot_id
			 WHERE jrs.repository_id = $1
			   AND jrs.consecutive_silent_sweeps > 0
			   AND jrs.escalation_pending = false
			   -- A terminal run (completed/failed/canceled/compromised) is past the
			   -- recovery sweep; only a still-actionable run must keep escalating.
			   AND r.state IN ('running','waiting_human')
		)
		SELECT run_id, job_id, workflow_job_id, run_state,
		       consecutive_silent_sweeps, misfire_evidence_score, last_probe_basis,
		       silent_sweep_cap, last_recovery_at
		  FROM gate
		 WHERE consecutive_silent_sweeps >= silent_sweep_cap
		 ORDER BY run_id, workflow_job_id, job_id`,
		repositoryID)
	if err != nil {
		block["checked"] = true
		block["skipped"] = nil
		block["error"] = err.Error()
		return block, []string{doctorRecoveryGateCheck + ".read_failed: " + err.Error()}, []map[string]any{{
			"check": doctorRecoveryGateCheck + "_read_failed",
			"error": err.Error(),
		}}
	}
	block["checked"] = true
	block["skipped"] = nil

	breaches := []map[string]any{}
	problems := []string{}
	records := []map[string]any{}
	for _, row := range rows {
		jobID := stringFrom(row, "job_id")
		runID := stringFrom(row, "run_id")
		silent := intFrom(row, "consecutive_silent_sweeps")
		silentCap := intFrom(row, "silent_sweep_cap")
		record := map[string]any{
			"check":                     doctorRecoveryGateCheck,
			"job_id":                    jobID,
			"run_id":                    runID,
			"workflow_job_id":           stringFrom(row, "workflow_job_id"),
			"run_state":                 stringFrom(row, "run_state"),
			"consecutive_silent_sweeps": silent,
			"silent_sweep_cap":          silentCap,
			"misfire_evidence_score":    intFrom(row, "misfire_evidence_score"),
			"last_probe_basis":          stringFrom(row, "last_probe_basis"),
			"escalation_pending":        false,
			"remediation":               recoveryGateBreachRemediation(runID, jobID),
		}
		if at, ok := parseTimeValue(row["last_recovery_at"]); ok {
			record["last_recovery_at"] = at.UTC().Format(time.RFC3339)
		}
		breaches = append(breaches, record)
		records = append(records, record)
		problems = append(problems, doctorRecoveryGateCheck+"."+jobID+
			": consecutive_silent_sweeps="+intPlaceholder(silent)+
			" >= cap="+intPlaceholder(silentCap)+" but escalation_pending=false (the escape valve must always eventually fire); "+
			recoveryGateBreachRemediation(runID, jobID))
	}
	block["breach_count"] = len(breaches)
	block["breaches"] = breaches
	return block, problems, records
}

// recoveryGateBreachRemediation names how an operator clears a never-un-escalatable
// escape-valve breach: route recovery back through the daemon's own state machine
// (the AGENTS.md "do not paste over a broken runner" rule). The breach is a runner
// defect — the gate failed to escalate at the cap — so the right move is to force the
// over-budget job to its escalation/quarantine outcome via the daemon, then file it.
func recoveryGateBreachRemediation(runID, jobID string) string {
	return "the silent-sweep escape valve did not fire — run `striatum recovery complete-stalled --run-id " + runID +
		" --job-id " + jobID + "` if the work is durable, else `striatum recovery requeue-stale`; this is a runner defect (file/update an issue) — do not hand-finish the job"
}

// recoveryGateSummary is the RFC 0131 131-D (#337) escalation-decision legibility
// block for run.summary. It projects the confidence-gate state of every job in the
// run that has accrued silent sweeps or is escalation_pending, so an operator can
// see WHY a lane is — or is NOT yet — escalated: how many consecutive silent
// sweeps it has against its escape-valve cap, its compounding misfire score, the
// last probe_basis recorded, and whether the gate has flipped it to
// escalation_pending. A lane being DEBOUNCED (silent_sweeps > 0, not yet
// escalation_pending) reads "deferred, N/cap silent sweeps" rather than silence.
//
// Degrade-safe: a daemon behind migration 0035 has no confidence-gate columns, so
// the block reports checked=false and an empty gates list.
func recoveryGateSummary(ctx context.Context, runner db.Runner, repositoryID, runID string) map[string]any {
	summary := map[string]any{
		"checked":         false,
		"debounced_count": 0,
		"escalated_count": 0,
		"gates":           []map[string]any{},
	}
	if repositoryID == "" || runID == "" {
		return summary
	}
	if !jobRecoveryConfidenceColumnsExist(ctx, runner) {
		summary["skipped"] = "migration 0035 (confidence-gate columns) not applied"
		return summary
	}
	rows, err := collectRows(ctx, runner, `
		SELECT jrs.job_id, j.workflow_job_id,
		       jrs.consecutive_silent_sweeps,
		       jrs.misfire_evidence_score,
		       jrs.last_probe_basis,
		       jrs.escalation_pending,
		       jrs.last_recovery_action,
		       jrs.last_recovery_at,
		       b.blocker_id,
		       b.blocker_kind,
		       b.severity,
		       COALESCE(
		         NULLIF((w.workflow_json #>> '{recovery_policy,max_silent_sweeps}'), '')::int,
		         GREATEST(
		           (COALESCE(
		              NULLIF((w.workflow_json #>> '{recovery_policy,max_requeues}'), '')::int,
		              NULLIF((w.workflow_json #>> '{recovery_policy,max_total_requeues_per_job}'), '')::int,
		              2
		            ) * 2) + 3,
		           3
		         )
		       ) AS silent_sweep_cap
		  FROM striatumd.job_recovery_state jrs
		  JOIN striatumd.jobs j
		    ON j.repository_id = jrs.repository_id AND j.job_id = jrs.job_id
		  JOIN striatumd.runs r
		    ON r.repository_id = jrs.repository_id AND r.run_id = jrs.run_id
		  LEFT JOIN striatumd.workflow_snapshots w
		    ON w.repository_id = r.repository_id
		   AND w.workflow_snapshot_id = r.workflow_snapshot_id
		  -- #477: surface the open blocker (and thus the escalation_id) backing an
		  -- escalated gate, preferring the recovery_exhausted escalation the sweep
		  -- opens, so run.summary names the id + resolve verb instead of just job_id.
		  LEFT JOIN LATERAL (
		    SELECT bz.blocker_id, bz.blocker_kind, bz.severity
		      FROM striatumd.blockers bz
		     WHERE bz.repository_id = jrs.repository_id
		       AND bz.job_id = jrs.job_id
		       AND bz.state = 'open'
		     ORDER BY (bz.blocker_kind = 'recovery_exhausted') DESC, bz.created_at, bz.blocker_id
		     LIMIT 1
		  ) b ON true
		 WHERE jrs.repository_id = $1 AND jrs.run_id = $2
		   AND (jrs.consecutive_silent_sweeps > 0 OR jrs.escalation_pending = true)
		 ORDER BY j.workflow_job_id, jrs.job_id`,
		repositoryID, runID)
	if err != nil {
		summary["checked"] = true
		summary["error"] = err.Error()
		return summary
	}
	summary["checked"] = true
	gates := make([]map[string]any, 0, len(rows))
	debounced := 0
	escalated := 0
	for _, row := range rows {
		escalationPending := row["escalation_pending"] == true
		gate := map[string]any{
			"workflow_job_id":           stringFrom(row, "workflow_job_id"),
			"job_id":                    stringFrom(row, "job_id"),
			"consecutive_silent_sweeps": intFrom(row, "consecutive_silent_sweeps"),
			"silent_sweep_cap":          intFrom(row, "silent_sweep_cap"),
			"misfire_evidence_score":    intFrom(row, "misfire_evidence_score"),
			"last_probe_basis":          stringFrom(row, "last_probe_basis"),
			"escalation_pending":        escalationPending,
			"last_recovery_action":      stringFrom(row, "last_recovery_action"),
		}
		// #477: when an open blocker backs this gate, name the blocker_id, the
		// resolve surface, and the literal resolve command. For escalation-class
		// blockers (e.g. recovery_exhausted) escalation_id == blocker_id, so an
		// operator reading `run summary` can copy `escalation resolve <id>` instead
		// of grepping raw event payloads for the id and guessing the verb.
		if blockerID := stringFrom(row, "blocker_id"); blockerID != "" {
			gate["blocker_id"] = blockerID
			gate["blocker_kind"] = stringFrom(row, "blocker_kind")
			gate["resolve_surface"] = blockerResolveSurface(row)
			gate["resolution_command"] = blockerResolutionCommand(row)
			gate["resolve_command"] = blockerResolveCommand(row)
			if escID := escalationIDForBlocker(row); escID != nil {
				gate["escalation_id"] = escID
			}
		}
		if at, ok := parseTimeValue(row["last_recovery_at"]); ok {
			gate["last_recovery_at"] = at.UTC().Format(time.RFC3339)
		}
		if escalationPending {
			gate["gate_state"] = "escalated"
			escalated++
		} else {
			gate["gate_state"] = "debounced"
			debounced++
		}
		gates = append(gates, gate)
	}
	summary["gates"] = gates
	summary["debounced_count"] = debounced
	summary["escalated_count"] = escalated
	return summary
}

// jobRecoveryConfidenceColumnsExist reports whether migration 0035 (the RFC 0131
// 131-C confidence-gate columns) is applied. A deployment behind on it has no
// consecutive_silent_sweeps column, so the escape-valve invariant cannot be — and
// need not be — checked (the pre-131-C path never accrues silent sweeps).
func jobRecoveryConfidenceColumnsExist(ctx context.Context, runner db.Runner) bool {
	rows, err := collectRows(ctx, runner, `
		SELECT 1
		  FROM information_schema.columns
		 WHERE table_schema = 'striatumd'
		   AND table_name = 'job_recovery_state'
		   AND column_name = 'consecutive_silent_sweeps'
		 LIMIT 1`)
	return err == nil && len(rows) > 0
}
