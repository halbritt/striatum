package mutations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/metrics"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// recoveryPolicy holds the per-job autonomous-recovery budgets read from the
// workflow's optional top-level `recovery_policy` block (RFC 0020 §Step 2),
// extended here for RFC 0101 Phase 3 Slice 2 with `max_requeues` /
// `max_transfers`. The block is documented but was never parsed in the Go
// daemon, so reading it here is the first consumer; we extend the existing
// block rather than inventing a parallel one. A workflow that omits the block
// gets the slice defaults.
type recoveryPolicy struct {
	maxRequeues int
	// maxUnsealedRequeues bounds requeues for the agent_exited_unsealed class
	// (#289): a confirmed-dead supervised agent that engaged the work protocol and
	// emitted output but never called work.complete. A respawn can re-author and
	// seal the work, so one retry is worthwhile, but a systematic unsealed exit
	// (turn-end / context-budget / rate-limit death mid-task) rarely self-heals on
	// repeat — so this class escalates to the operator faster than a hard crash,
	// burning less of the requeue budget than the symptom the issue reported.
	// This is the STATEFUL repo-write lane bound; a read-only reviewer lane uses
	// the larger maxReviewerUnsealedRequeues (#478 / D249).
	maxUnsealedRequeues int
	// maxReviewerUnsealedRequeues is the agent_exited_unsealed requeue bound for a
	// READ-ONLY reviewer lane (a job that does not repo-write — e.g. a claude_code
	// review_final / review_design lane). D249 (RFC 0152) resolved #478 with a
	// lane-kind-differentiated budget: for a stateless reviewer lane a transient
	// unsealed exit (the common cause is transient Anthropic-API unavailability
	// during end-of-session wind-down, after the review was already produced) is a
	// clean fresh-session retry, so escalate-after-one-respawn made a single API
	// blip the most frequent committee needs_operator cause. A stateful repo-write
	// lane's repeated unsealed exit still signals systematic failure, so it keeps
	// the tight maxUnsealedRequeues default. This larger bound is therefore applied
	// only to non-repo-write lanes; the global defaultMaxUnsealedRequeues constant
	// (and the pinned defaultMaxUnsealedRequeues < defaultMaxRequeues invariant)
	// does not move.
	maxReviewerUnsealedRequeues int
	maxTransfers                int
	// maxQuarantinableJobs bounds how many jobs a single run may move to the
	// 'quarantined' state and still finalize-the-majority (#311 P0). The default
	// is 1: a run with a SINGLE recovery-exhausted, downstream-clear job
	// quarantines it and finalizes on the completed deliverables, but a run with
	// multiple simultaneous flaky jobs escalates to needs_operator rather than
	// silently swallowing large-scale failure. An operator can raise the cap via
	// recovery_policy.max_quarantinable_jobs; 0 disables quarantine entirely
	// (every exhaustion escalates the whole run, the pre-#311 behavior).
	maxQuarantinableJobs int
	// maxSilentSweeps is the RFC 0131 Layer 4 escape-valve cap: the number of
	// CONSECUTIVE deadline_elapsed_only silent sweeps (no forgery-resistant
	// progress) a pipe / no-oracle lane may accrue before escalation fires
	// regardless of the confidence (misfire_evidence_score) reduction. It is a hard
	// integer ceiling, not a ratchet: once consecutive_silent_sweeps reaches it,
	// silence itself is the oracle the pipe lane otherwise lacks, so the lane is
	// escalatable in bounded time by construction (never un-escalatable, Goal 3).
	// 0 means "derive the RFC default (maxRequeues*2)+3 at use time"; an operator
	// may set it explicitly via recovery_policy.max_silent_sweeps. The cap must be
	// chosen so cap × sweep_interval exceeds the longest plausible silent
	// generation turn (Risks: long legitimate silent generation).
	maxSilentSweeps int
}

const (
	defaultMaxRequeues         = 2
	defaultMaxUnsealedRequeues = 1
	// defaultMaxReviewerUnsealedRequeues is the larger #478 / D249 unsealed-requeue
	// bound for a read-only reviewer lane. It deliberately matches the hard-crash
	// requeue default (defaultMaxRequeues) rather than exceeding it: a transient
	// reviewer unsealed exit is no worse than a hard crash and a fresh session
	// almost always succeeds, so it earns the same room — never MORE — as a hard
	// crash. The selector caps it at maxRequeues at use time, so an operator who
	// lowers max_requeues never gets a reviewer bound that exceeds their own
	// requeue budget.
	defaultMaxReviewerUnsealedRequeues = 2
	defaultMaxTransfers                = 3
	defaultMaxQuarantinableJobs        = 1
)

// silentSweepCap returns the RFC 0131 Layer 4 escape-valve cap FLOOR for this
// policy: the operator override (recovery_policy.max_silent_sweeps) when set, else
// the RFC default (maxRequeues*2)+3. The +3 floor keeps the cap finite and useful
// even when maxRequeues is 0 (a workflow that disabled requeues): a pipe lane is
// still escalatable in bounded silent sweeps. This is the FLOOR — the tightest cap
// any job gets — applied to a critical-path job whose downstream is blocked
// (topologyAdaptiveSilentSweepCap loosens it for a leaf job).
func (p recoveryPolicy) silentSweepCap() int {
	if p.maxSilentSweeps > 0 {
		return p.maxSilentSweeps
	}
	silentCap := (p.maxRequeues * 2) + 3
	if silentCap < 3 {
		silentCap = 3
	}
	return silentCap
}

// leafSilentSweepSlack is how many EXTRA silent sweeps a leaf job (no blocked
// downstream dependent) is granted on top of the single-cap floor (RFC 0131 #349
// topology-adaptive cap). A leaf job's prolonged silence wedges nothing downstream,
// so it is given generous room for a long legitimate silent generation turn before
// the escape valve fires; a critical-path job whose downstream is blocked stays at
// the tight floor cap (it must escalate sooner because its wedge is costly). The
// extra room scales with the floor so a workflow with a larger requeue budget gets a
// proportionally larger leaf allowance.
func (p recoveryPolicy) topologyAdaptiveSilentSweepCap(downstreamBlocked bool) int {
	floor := p.silentSweepCap()
	if downstreamBlocked {
		// Critical path: a blocked downstream dependent is waiting on this job, so
		// escalate at the tight single-cap floor (the established, backward-compatible
		// behavior — no job ever escalates SOONER than the pre-#349 single cap).
		return floor
	}
	// Leaf / non-critical: nothing is blocked behind this job, so loosen the cap —
	// double the floor — to tolerate a long legitimate silent generation turn before
	// the escape valve fires. Still finite, so the lane remains escalatable in bounded
	// time by construction (the never-un-escalatable invariant holds for every cap).
	return floor * 2
}

// topologyAdaptiveEscalationThreshold returns how many CONSECUTIVE silent sweeps a
// pipe lane must accrue before the Layer-3 bar escalates it (RFC 0131 #349). A
// critical-path job whose downstream is blocked escalates at the tight 2-sweep
// floor (the established, backward-compatible two-consecutive-sweep bar — no job
// escalates SOONER than pre-#349). A leaf job whose silence wedges nothing
// downstream is given a generous threshold (the floor cap) so a long legitimate
// silent generation turn is tolerated before the bar fires; the loosened escape-
// valve cap remains its absolute ceiling. The threshold is always >= 2 and is
// clamped to <= the cap by applyConfidenceGate, so the lane is always escalatable.
func (p recoveryPolicy) topologyAdaptiveEscalationThreshold(downstreamBlocked bool) int {
	if downstreamBlocked {
		return 2
	}
	threshold := p.silentSweepCap()
	if threshold < 2 {
		threshold = 2
	}
	return threshold
}

// jobHasBlockedDownstream reports whether any NON-TERMINAL sibling job depends on
// this job (it is downstream of jobID via job_dependencies) — i.e. this job is on a
// critical path whose downstream is blocked waiting for it (RFC 0131 #349). A job
// with a blocked downstream gets the tight single-cap floor; a leaf job (no
// dependents, or all dependents already terminal) gets the generous loosened cap.
// A terminal dependent (completed/failed/canceled/quarantined/compromised) is no
// longer blocked on this job, so it does not make the job critical-path.
func jobHasBlockedDownstream(ctx context.Context, tx db.TxRunner, repositoryID, jobID string) (bool, error) {
	return existsRow(ctx, tx, `
		SELECT 1
		  FROM striatumd.job_dependencies dep
		  JOIN striatumd.jobs d
		    ON d.repository_id = dep.repository_id AND d.job_id = dep.job_id
		 WHERE dep.repository_id = $1
		   AND dep.depends_on_job_id = $2
		   AND d.state NOT IN ('completed','failed','canceled','quarantined','compromised')
		 LIMIT 1`, repositoryID, jobID)
}

// Recovery-specific stall classifications for a confirmed-dead supervised agent.
// These are distinct from the sessionliveness.* protocol stall classes (which
// describe a still-present session that is not making progress): here the agent
// PROCESS is gone.
const (
	// stallClassAgentPIDDead — the supervised agent process/pane is dead and the
	// session showed no evidence of having engaged the work protocol with output.
	stallClassAgentPIDDead = "agent_pid_dead"
	// stallClassAgentExitedUnsealed — the agent engaged the work protocol (claimed
	// / made MCP tool calls) and emitted PTY output, then its process died WITHOUT
	// calling work.complete. The work may be complete-but-unsealed; recovery cannot
	// seal on its behalf (attestation), so it respawns once then escalates with a
	// distinct, inspect-the-worktree remediation (#289).
	stallClassAgentExitedUnsealed = "agent_exited_unsealed"
)

// isNecrosisStallClass reports whether a recovery stall class is a CONFIRMED-DEAD
// class — the necrosis set the RFC 0137 Phase B exporter counts. It is the single
// source of truth for "necrosis at the site": closeStalledOwningSession uses it
// to tag the durable session.closed event, and the metrics necrosis domain is
// pinned to exactly {agent_pid_dead, agent_exited_unsealed, recovery_exhausted}
// by TestNecrosisDomainMatchesConfirmedDeadConstants. recovery_exhausted is a
// blocker kind (not a stall class), so it is necrosis-tagged at its own
// escalation site (run.escalated / recovery.job_quarantined carry blocker_kind),
// not here.
func isNecrosisStallClass(stallClass string) bool {
	return stallClass == stallClassAgentPIDDead || stallClass == stallClassAgentExitedUnsealed
}

// recoveryPolicyFromWorkflow reads the recovery budgets from a workflow JSON
// map. Honors the documented RFC 0020 `max_total_requeues_per_job` as the
// requeue fallback when the slice-2 `max_requeues` key is absent, so an
// existing recovery_policy block keeps meaning what it says.
func recoveryPolicyFromWorkflow(workflow map[string]any) recoveryPolicy {
	policy := recoveryPolicy{
		maxRequeues:                 defaultMaxRequeues,
		maxUnsealedRequeues:         defaultMaxUnsealedRequeues,
		maxReviewerUnsealedRequeues: defaultMaxReviewerUnsealedRequeues,
		maxTransfers:                defaultMaxTransfers,
		maxQuarantinableJobs:        defaultMaxQuarantinableJobs,
	}
	block := asMap(workflow["recovery_policy"])
	if len(block) == 0 {
		return policy
	}
	if v, ok := block["max_requeues"]; ok {
		policy.maxRequeues = intFromAny(v, policy.maxRequeues)
	} else if v, ok := block["max_total_requeues_per_job"]; ok {
		// Backward-compatible: the documented RFC 0020 field bounds total
		// requeues per job, which is exactly this budget.
		policy.maxRequeues = intFromAny(v, policy.maxRequeues)
	}
	if v, ok := block["max_unsealed_requeues"]; ok {
		policy.maxUnsealedRequeues = intFromAny(v, policy.maxUnsealedRequeues)
	}
	if v, ok := block["max_reviewer_unsealed_requeues"]; ok {
		// #478 / D249 operator override for the read-only reviewer-lane unsealed
		// bound. Absent, the larger default applies; clamped >= the stateful bound
		// and <= max_requeues below so it is always at least as forgiving as the
		// stateful lane and never exceeds the run's own requeue budget.
		policy.maxReviewerUnsealedRequeues = intFromAny(v, policy.maxReviewerUnsealedRequeues)
	}
	if v, ok := block["max_transfers"]; ok {
		policy.maxTransfers = intFromAny(v, policy.maxTransfers)
	}
	if v, ok := block["max_quarantinable_jobs"]; ok {
		policy.maxQuarantinableJobs = intFromAny(v, policy.maxQuarantinableJobs)
	}
	if v, ok := block["max_silent_sweeps"]; ok {
		// RFC 0131 Layer 4 escape-valve cap override. 0/absent derives the RFC
		// default (maxRequeues*2)+3 at use time; a negative value is clamped to 0
		// (derive default) below.
		policy.maxSilentSweeps = intFromAny(v, policy.maxSilentSweeps)
	}
	if policy.maxSilentSweeps < 0 {
		policy.maxSilentSweeps = 0
	}
	if policy.maxRequeues < 0 {
		policy.maxRequeues = 0
	}
	if policy.maxUnsealedRequeues < 0 {
		policy.maxUnsealedRequeues = 0
	}
	// The unsealed budget is meant to escalate no later than a hard crash, so cap
	// it at maxRequeues — a smaller-or-equal bound holds even under an operator
	// override that sets max_requeues below the unsealed default.
	if policy.maxUnsealedRequeues > policy.maxRequeues {
		policy.maxUnsealedRequeues = policy.maxRequeues
	}
	if policy.maxReviewerUnsealedRequeues < 0 {
		policy.maxReviewerUnsealedRequeues = 0
	}
	// #478 / D249: the reviewer-lane unsealed bound is at LEAST as forgiving as the
	// stateful bound (it is the looser of the two, never tighter) and never exceeds
	// the run's own requeue budget. The first clamp keeps a too-low operator
	// override from accidentally making a reviewer lane escalate sooner than a
	// stateful one; the second keeps it bounded by max_requeues (so a hard crash is
	// never out-budgeted by a reviewer unsealed exit).
	if policy.maxReviewerUnsealedRequeues < policy.maxUnsealedRequeues {
		policy.maxReviewerUnsealedRequeues = policy.maxUnsealedRequeues
	}
	if policy.maxReviewerUnsealedRequeues > policy.maxRequeues {
		policy.maxReviewerUnsealedRequeues = policy.maxRequeues
	}
	if policy.maxTransfers < 0 {
		policy.maxTransfers = 0
	}
	if policy.maxQuarantinableJobs < 0 {
		policy.maxQuarantinableJobs = 0
	}
	return policy
}

// unsealedRequeueBudget returns the agent_exited_unsealed requeue bound for a
// lane of the given kind (#478 / D249). A READ-ONLY reviewer lane (repoWrite ==
// false) gets the larger maxReviewerUnsealedRequeues — a transient unsealed exit
// there (transient API unavailability during end-of-session wind-down, after the
// review was already produced) is a clean fresh-session retry, so it earns the
// same room as a hard crash. A STATEFUL repo-write lane (repoWrite == true) keeps
// the tight maxUnsealedRequeues: a repeated unsealed exit there still signals
// systematic failure and must escalate sooner than a hard crash. The global
// defaultMaxUnsealedRequeues constant (and the pinned
// defaultMaxUnsealedRequeues < defaultMaxRequeues invariant) is untouched — only
// the lane-scoped selection differs.
func (p recoveryPolicy) unsealedRequeueBudget(repoWrite bool) int {
	if repoWrite {
		return p.maxUnsealedRequeues
	}
	return p.maxReviewerUnsealedRequeues
}

// jobRecoveryBudget mirrors a striatumd.job_recovery_state row.
type jobRecoveryBudget struct {
	requeueCount       int
	transferCount      int
	respawnCount       int
	escalationPending  bool
	lastRecoveryAction string
	// RFC 0131 131-C confidence-gate columns (migration 0035). misfireEvidenceScore
	// and consecutiveSilentSweeps are monotone-compounding signals that gate a
	// deadline_elapsed_only pipe-lane escalation; lastProbeBasis is the probe_basis
	// the previous sweep recorded, used to require the evidence strictly increased
	// across two consecutive deadline_elapsed_only sweeps. confidenceColumns reports
	// whether migration 0035 is applied: a deployment behind on it leaves these at
	// their zero values and the gate degrades to today's ungated escalation.
	misfireEvidenceScore    int
	consecutiveSilentSweeps int
	lastProbeBasis          string
	confidenceColumns       bool
	// lastRecoveryAt is the timestamp the previous sweep stamped on this budget
	// row. The confidence gate uses it as the "since the last sweep" boundary for
	// the forgery-resistant progress check: sealed work created after it counts as
	// progress made since the prior sweep. The zero time.Time when the row is new.
	lastRecoveryAt time.Time
}

// readJobRecoveryBudget reads (without upserting) the current budget row for a
// job. A missing row is reported as a zeroed budget — the upsert happens only
// when an action is actually recorded. The RFC 0131 131-C confidence-gate columns
// (migration 0035) are read when present; a deployment behind on 0035 falls back
// to the pre-131-C column set (degrade-safe: the gate then sees zeroed confidence
// state and behaves as the ungated path), matching the bundle-0012 degrade-safe
// pattern P0 uses.
func readJobRecoveryBudget(ctx context.Context, tx db.TxRunner, repositoryID, jobID string) (jobRecoveryBudget, error) {
	row, err := oneRow(ctx, tx, `
		SELECT requeue_count, transfer_count, respawn_count, escalation_pending,
		       last_recovery_action, last_recovery_at, misfire_evidence_score,
		       consecutive_silent_sweeps, last_probe_basis
		  FROM striatumd.job_recovery_state
		 WHERE repository_id = $1 AND job_id = $2`, repositoryID, jobID)
	if err != nil {
		if isNoRows(err) {
			return jobRecoveryBudget{confidenceColumns: true}, nil
		}
		if isUndefinedColumn(err) {
			// Deployment behind on migration 0035: read the legacy column set so the
			// gate degrades to ungated single-sweep escalation (the pre-131-C path).
			return readJobRecoveryBudgetLegacy(ctx, tx, repositoryID, jobID)
		}
		return jobRecoveryBudget{}, err
	}
	lastRecoveryAt, _ := timeFromAny(row["last_recovery_at"])
	return jobRecoveryBudget{
		requeueCount:            intFromAny(row["requeue_count"], 0),
		transferCount:           intFromAny(row["transfer_count"], 0),
		respawnCount:            intFromAny(row["respawn_count"], 0),
		escalationPending:       row["escalation_pending"] == true,
		lastRecoveryAction:      fmt.Sprint(nullable(row["last_recovery_action"])),
		misfireEvidenceScore:    intFromAny(row["misfire_evidence_score"], 0),
		consecutiveSilentSweeps: intFromAny(row["consecutive_silent_sweeps"], 0),
		lastProbeBasis:          fmt.Sprint(nullable(row["last_probe_basis"])),
		confidenceColumns:       true,
		lastRecoveryAt:          lastRecoveryAt.UTC(),
	}, nil
}

// readJobRecoveryBudgetLegacy reads the pre-131-C column set for a deployment
// behind on migration 0035. confidenceColumns stays false so the confidence gate
// knows the new state cannot be persisted and falls back to ungated escalation.
func readJobRecoveryBudgetLegacy(ctx context.Context, tx db.TxRunner, repositoryID, jobID string) (jobRecoveryBudget, error) {
	row, err := oneRow(ctx, tx, `
		SELECT requeue_count, transfer_count, respawn_count, escalation_pending,
		       last_recovery_action
		  FROM striatumd.job_recovery_state
		 WHERE repository_id = $1 AND job_id = $2`, repositoryID, jobID)
	if err != nil {
		if isNoRows(err) {
			return jobRecoveryBudget{}, nil
		}
		return jobRecoveryBudget{}, err
	}
	return jobRecoveryBudget{
		requeueCount:       intFromAny(row["requeue_count"], 0),
		transferCount:      intFromAny(row["transfer_count"], 0),
		respawnCount:       intFromAny(row["respawn_count"], 0),
		escalationPending:  row["escalation_pending"] == true,
		lastRecoveryAction: fmt.Sprint(nullable(row["last_recovery_action"])),
	}, nil
}

// recordRecoveryAction increments the named budget counter and stamps the last
// action metadata. counterColumn is one of requeue_count / transfer_count /
// respawn_count. The row is created on first action (idempotent upsert).
func recordRecoveryAction(ctx context.Context, tx db.TxRunner, repositoryID, runID, jobID, counterColumn, action, stallClass string) error {
	now := nowString()
	// counterColumn is a fixed internal constant (never user input), so it is
	// safe to interpolate into the SQL text.
	sql := fmt.Sprintf(`
		INSERT INTO striatumd.job_recovery_state (
		  repository_id, run_id, job_id, %[1]s,
		  last_recovery_action, last_recovery_at, last_stall_class,
		  created_at, updated_at
		) VALUES ($1, $2, $3, 1, $4, $5, $6, $5, $5)
		ON CONFLICT (repository_id, job_id) DO UPDATE
		   SET %[1]s = striatumd.job_recovery_state.%[1]s + 1,
		       last_recovery_action = EXCLUDED.last_recovery_action,
		       last_recovery_at = EXCLUDED.last_recovery_at,
		       last_stall_class = EXCLUDED.last_stall_class,
		       updated_at = EXCLUDED.updated_at`, counterColumn)
	return tx.Exec(ctx, sql, repositoryID, runID, jobID, action, now, nullable(stallClass))
}

// markRecoveryEscalation flags the budget row as escalation_pending so Phase 4
// can flip the run to needs_operator. It does NOT increment a counter — the
// budget was already exhausted. It records the action and stall class for
// audit. Idempotent: re-flagging an already-pending row only refreshes the
// metadata, not escalated_at (set once).
func markRecoveryEscalation(ctx context.Context, tx db.TxRunner, repositoryID, runID, jobID, action, stallClass string) error {
	now := nowString()
	return tx.Exec(ctx, `
		INSERT INTO striatumd.job_recovery_state (
		  repository_id, run_id, job_id,
		  last_recovery_action, last_recovery_at, last_stall_class,
		  escalation_pending, escalated_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, true, $5, $5, $5)
		ON CONFLICT (repository_id, job_id) DO UPDATE
		   SET escalation_pending = true,
		       escalated_at = COALESCE(striatumd.job_recovery_state.escalated_at, EXCLUDED.escalated_at),
		       last_recovery_action = EXCLUDED.last_recovery_action,
		       last_recovery_at = EXCLUDED.last_recovery_at,
		       last_stall_class = EXCLUDED.last_stall_class,
		       updated_at = EXCLUDED.updated_at`,
		repositoryID, runID, jobID, action, now, nullable(stallClass))
}

// jobSealedProgressAt returns the most recent FORGERY-RESISTANT sealed-work
// progress timestamp for a job: the later of its latest published artifact's
// created_at (an artifact anchor moved) and its latest sealed verdict's
// created_at (a verdict was sealed). Both rows are written only by the daemon's
// publish / verdict handlers — a dead-but-spinning agent (the #324 spinner class)
// can keep emitting raw PTY/stderr frames and trivial protocol chatter, but it
// cannot manufacture an artifacts or verdicts row, so this is the exact
// un-forgeable progress signal RFC 0131 Layer 4 requires for the silent-sweep
// counter reset. Returns the zero time.Time (and false) when the job has no such
// sealed progress yet.
func jobSealedProgressAt(ctx context.Context, tx db.TxRunner, repositoryID, jobID string) (time.Time, bool, error) {
	row, err := oneRow(ctx, tx, `
		SELECT GREATEST(
		         (SELECT max(created_at) FROM striatumd.artifacts
		           WHERE repository_id = $1 AND job_id = $2),
		         (SELECT max(created_at) FROM striatumd.verdicts
		           WHERE repository_id = $1 AND job_id = $2)
		       ) AS sealed_at`, repositoryID, jobID)
	if err != nil {
		if isNoRows(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	ts, ok := timeFromAny(row["sealed_at"])
	if !ok {
		return time.Time{}, false, nil
	}
	return ts.UTC(), true, nil
}

// cohortHasFresherLiveness implements RFC 0131 Layer 3 cross-lane corroboration:
// it reports whether ANY SIBLING job in the same in-memory recovery cohort (the
// rows already scanned under recoverStuckJobs' single FOR UPDATE OF j) has an
// owning session whose liveness is fresher than `windowStart`. A solo
// deadline_elapsed_only pipe deadline alongside a demonstrably-live sibling is a
// likely misfire (a host pause / sweep-timing artifact, not a real hang), so the
// gate defers rather than burning budget. This is FREE — the cohort is already in
// hand — and is an optimization for the multi-lane case only: a single-pipe-lane
// run has no siblings, so the Layer-4 cap (not corroboration) is its safety
// guarantee. selfJobID excludes the job being decided from its own cohort. The
// freshness signal is the same last_mcp_request_at / activity columns the
// classifier consumes, so a sibling that is genuinely working corroborates; a
// cohort that is uniformly silent does not (and must not — a fleet-wide wedge
// must still escalate, RFC 0131 Alternatives "naive global-quiescence").
func cohortHasFresherLiveness(rows []map[string]any, selfJobID string, windowStart time.Time, now time.Time) bool {
	windowStart = windowStart.UTC()
	for _, row := range rows {
		if fmt.Sprint(row["job_id"]) == selfJobID {
			continue
		}
		sibling := sessionliveness.ActivityFromRow(row)
		// A sibling counts as "fresher than the window" when its most recent
		// recorded MCP/protocol/PTY/lease-heartbeat activity is after windowStart.
		// We reuse the classifier's freshest-signal set indirectly: any of the
		// activity timestamps after windowStart is conclusive recent life.
		latest := latestSiblingActivity(sibling)
		if latest != nil && latest.UTC().After(windowStart) {
			return true
		}
	}
	return false
}

// latestSiblingActivity returns the most recent activity timestamp across a
// sibling session's liveness columns — the signal cross-lane corroboration ages
// against. It deliberately includes the forgery-resistant work/lease columns and
// the protocol columns so any genuine recent life in the cohort corroborates.
func latestSiblingActivity(a sessionliveness.Activity) *time.Time {
	return latestTimePtr(
		a.LastMCPRequestAt, a.LastToolsListAt, a.LastAwaitPacketAt,
		a.LastPacketDeliveredAt, a.LastAckAt, a.LastWorkBlockAt,
		a.LastWorkReleaseAt, a.LastWorkCompleteAt, a.LastWorkHeartbeatAt,
		a.LastSessionReadyAt, a.LastSessionHeartbeatAt, a.LastPTYActivityAt,
		a.LastPipeReadAt, a.LastToolCallStartedAt, a.LastToolCallFinishedAt,
		a.ActiveLeaseHeartbeatAt,
	)
}

func latestTimePtr(values ...*time.Time) *time.Time {
	var latest *time.Time
	for _, v := range values {
		if v == nil {
			continue
		}
		if latest == nil || v.UTC().After(latest.UTC()) {
			latest = v
		}
	}
	return latest
}

// windowStartForGate returns the "since the last sweep" boundary the confidence
// gate compares forgery-resistant sealed progress against (RFC 0131 Layer 3/4).
// It is the timestamp the previous sweep stamped on the budget row
// (budget.lastRecoveryAt). For a job with no prior recovery touch (a fresh
// budget row) it falls back to a bounded look-back window (one protocol-idle
// interval before `now`), so a lane that sealed real work very recently — but
// has no prior sweep yet — is still credited with progress on its first
// debounce.
func windowStartForGate(budget jobRecoveryBudget, now time.Time) time.Time {
	if !budget.lastRecoveryAt.IsZero() {
		return budget.lastRecoveryAt.UTC()
	}
	lookback := time.Duration(sessionliveness.DefaultPolicy().ProtocolIdleSeconds) * time.Second
	return now.UTC().Add(-lookback)
}

// confidenceGateDecision is the outcome of the RFC 0131 Layer 3/4 confidence gate
// for a deadline_elapsed_only pipe-lane escalation candidate.
type confidenceGateDecision struct {
	// escalate is true when the gate permits this sweep to commit the escalation
	// (the evidence cleared the two-consecutive-sweep bar, OR the Layer-4 silent-
	// sweep cap fired). When false, the escalation is DEBOUNCED this sweep.
	escalate bool
	// capFired is true when escalation was forced by the Layer-4 escape-valve cap
	// regardless of the confidence reduction (recorded for legibility).
	capFired bool
	// reset is true when forgery-resistant sealed progress advanced since the last
	// sweep, so the gate reset the counters and deferred (the lane is working).
	reset bool
	// silentSweeps / misfireScore are the post-update counter values (for the
	// debounced/escalation event payloads and the persisted row). silentCap is the
	// Layer-4 escape-valve cap in force for this decision.
	silentSweeps int
	misfireScore int
	silentCap    int
}

// applyConfidenceGate decides whether a deadline_elapsed_only escalation may
// commit this sweep (RFC 0131 Layer 3 + Layer 4) and persists the updated gate
// state on the job_recovery_state row. It is called ONLY for the pipe / no-oracle
// case (probe_basis == deadline_elapsed_only); a pty_confirmed_dead basis skips
// the gate entirely (it has a real oracle and escalates immediately). The rules:
//
//   - Forgery-resistant progress advanced since the last sweep (sealed artifact /
//     verdict, or a corroborating live sibling) => reset both counters to 0,
//     persist, and DEFER (the lane is demonstrably working).
//   - Else compound misfire_evidence_score, increment consecutive_silent_sweeps,
//     persist, and check the bars:
//   - Escape-valve cap (Layer 4): consecutive_silent_sweeps >= cap => ESCALATE
//     regardless of the confidence reduction (silence is the oracle).
//   - Layer-3 consecutive-silent-sweep bar: the PRIOR sweep was also a
//     deadline_elapsed_only stall (last_probe_basis == deadline_elapsed_only) AND
//     the lane has been silent for at least `escalateThreshold` consecutive sweeps
//     => ESCALATE. A single sweep never escalates a pipe lane (escalateThreshold is
//     never below 2). RFC 0131 #349 makes the threshold topology-adaptive: a
//     critical-path job (blocked downstream) escalates at the tight 2-sweep floor
//     exactly as before; a leaf job whose silence wedges nothing downstream is given
//     a generous threshold (the floor cap) before the bar fires — so topology
//     governs the OBSERVABLE escalation timing, not just the rarely-reached ceiling.
//   - Otherwise DEBOUNCE (do not escalate this sweep).
//
// When migration 0035 is not applied (budget.confidenceColumns == false) the gate
// cannot persist its state, so it degrades to the pre-131-C ungated path:
// escalate immediately. progressAdvanced folds the sealed-progress and cohort
// signals; silentCap is the topology-adaptive Layer-4 ceiling and escalateThreshold
// is the topology-adaptive Layer-3 bar minimum (always >= 2, always <= silentCap).
func applyConfidenceGate(
	ctx context.Context, tx db.TxRunner,
	repositoryID, runID, jobID string,
	budget jobRecoveryBudget, policy recoveryPolicy,
	progressAdvanced bool, escalateThreshold, silentCap int,
) (confidenceGateDecision, error) {
	// RFC 0131 #349: silentCap is the topology-adaptive escape-valve ceiling the
	// caller resolved for this job (policy.silentSweepCap() floor, loosened for a
	// leaf job); escalateThreshold is the topology-adaptive Layer-3 bar minimum (2
	// for a critical-path job, generous for a leaf). A non-positive value (an
	// unspecified caller, or a legacy call) falls back to the single-cap floor / the
	// 2-sweep bar so the cap is always finite and a single sweep never escalates.
	if silentCap <= 0 {
		silentCap = policy.silentSweepCap()
	}
	if escalateThreshold < 2 {
		escalateThreshold = 2
	}
	if escalateThreshold > silentCap {
		// The Layer-3 bar can never demand more sweeps than the Layer-4 ceiling — the
		// ceiling always fires first, keeping the lane escalatable in bounded time.
		escalateThreshold = silentCap
	}
	if !budget.confidenceColumns {
		// Degrade-safe: a deployment behind on migration 0035 cannot persist the
		// gate state, so it falls back to today's ungated single-sweep escalation.
		return confidenceGateDecision{escalate: true, silentCap: silentCap}, nil
	}

	if progressAdvanced {
		// Forgery-resistant progress: reset both counters and defer.
		if err := writeConfidenceGateState(ctx, tx, repositoryID, runID, jobID, 0, 0, sessionliveness.ProbeBasisDeadlineElapsedOnly); err != nil {
			return confidenceGateDecision{}, err
		}
		return confidenceGateDecision{escalate: false, reset: true, silentSweeps: 0, misfireScore: 0, silentCap: silentCap}, nil
	}

	silent := budget.consecutiveSilentSweeps + 1
	misfire := budget.misfireEvidenceScore + 1
	if err := writeConfidenceGateState(ctx, tx, repositoryID, runID, jobID, misfire, silent, sessionliveness.ProbeBasisDeadlineElapsedOnly); err != nil {
		return confidenceGateDecision{}, err
	}

	// Layer 4 escape-valve cap: a hard integer ceiling. Once the lane has been
	// provably silent for `silentCap` consecutive sweeps, escalate regardless of
	// the confidence reduction — silence itself is the oracle the pipe lane lacks.
	if silent >= silentCap {
		return confidenceGateDecision{escalate: true, capFired: true, silentSweeps: silent, misfireScore: misfire, silentCap: silentCap}, nil
	}

	// Layer 3 consecutive-silent-sweep bar: escalation commits only after at least
	// `escalateThreshold` consecutive deadline_elapsed_only sweeps (the prior
	// recorded basis was also deadline_elapsed_only). A single sweep never escalates
	// a pipe lane; a leaf job's threshold is generous, a critical-path job's is 2.
	priorSilent := budget.lastProbeBasis == string(sessionliveness.ProbeBasisDeadlineElapsedOnly)
	if priorSilent && silent >= escalateThreshold {
		return confidenceGateDecision{escalate: true, silentSweeps: silent, misfireScore: misfire, silentCap: silentCap}, nil
	}

	// Debounce: do not escalate this sweep.
	return confidenceGateDecision{escalate: false, silentSweeps: silent, misfireScore: misfire, silentCap: silentCap}, nil
}

// writeConfidenceGateState upserts the RFC 0131 131-C confidence-gate columns on
// the job_recovery_state row (migration 0035). It does NOT touch escalation_pending
// / escalated_at (the escalation flag is set separately by markRecoveryEscalation
// once the gate permits escalation) and does NOT increment a budget counter.
func writeConfidenceGateState(ctx context.Context, tx db.TxRunner, repositoryID, runID, jobID string, misfireScore, silentSweeps int, basis sessionliveness.ProbeBasis) error {
	now := nowString()
	return tx.Exec(ctx, `
		INSERT INTO striatumd.job_recovery_state (
		  repository_id, run_id, job_id,
		  misfire_evidence_score, consecutive_silent_sweeps, last_probe_basis,
		  last_recovery_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $7)
		ON CONFLICT (repository_id, job_id) DO UPDATE
		   SET misfire_evidence_score = EXCLUDED.misfire_evidence_score,
		       consecutive_silent_sweeps = EXCLUDED.consecutive_silent_sweeps,
		       last_probe_basis = EXCLUDED.last_probe_basis,
		       updated_at = EXCLUDED.updated_at`,
		repositoryID, runID, jobID, misfireScore, silentSweeps, string(basis), now)
}

const recoveryDecisionAuthor = "striatumd-recovery"

// recoverStuckJobs is the RFC 0101 Phase 3 Slice 2 autonomous in-daemon
// recovery decision tree (OQ4 resolved in-daemon, D094). It runs INSIDE
// HandleRecoveryAuto's existing withTx, after the auto-publish pass and before
// refreshRunLiveness, so it sees the same snapshot the auto-publish loop did and
// the liveness refresh classifies any sessions it leaves untouched.
//
// For each UNFINISHED job (state in claimed/running/stale_lease) it classifies
// the owning session (if any) via sessionliveness.Classify and decides:
//
//   - owning session dead/closed/absent AND lease released/expired/absent AND
//     no recoverable artifact (auto-publish already skipped it) ->
//     requeueJobSameAttempt (requeue_count budget). repo-write jobs are
//     reclaimed with operatorOverride: the bounded daemon loop IS the inspection
//     (D036's manual gate is for the interactive CLI, not autonomous recovery).
//   - stalled past its deadline with a still-active-but-expired lease ->
//     force-expire + requeueJobSameAttempt (transfer_count budget).
//   - working_* / quiet (pre-deadline) sessions are left untouched.
//
// Leaked interrogation windows (an awaiting-interrogation target session with no
// pending panel consumers) are closed via releaseInterrogationTargetForCompletedReview
// / maybeCloseInterrogationTarget (no budget).
//
// It is idempotent + convergent: re-running on an already-requeued (now
// queued+pending) job is a no-op (requeueJobSameAttempt detects
// already_reclaimable and we skip the budget increment in that case).
func recoverStuckJobs(ctx context.Context, tx db.TxRunner, repositoryID, runID string, policy recoveryPolicy) ([]map[string]any, error) {
	now := time.Now().UTC().Truncate(time.Second)
	actions := []map[string]any{}

	// Scan unfinished jobs with their most-recent lease (any state) and that
	// lease's owning session's full liveness-activity columns. The lease is
	// resolved as the latest lease on the job resource so a job whose
	// current_lease_id was cleared (running-limbo) still resolves its dead
	// owner. expected_artifacts_json lets us re-check the auto-publish skip.
	rows, err := queryRows(ctx, tx, `
		SELECT j.job_id, j.run_id, j.workflow_job_id, j.state AS job_state,
		       j.role_id, j.lane_selector_json, j.max_attempts,
		       j.write_scope_json, j.current_message_id, j.current_lease_id,
		       j.expected_artifacts_json,
		       l.lease_id, l.state AS lease_state, l.expires_at AS lease_expires_at,
		       l.owner_session_id,
		       s.session_id, s.state AS session_state, s.registered_at,
		       s.last_mcp_request_at, s.last_tools_list_at, s.last_await_packet_at,
		       s.last_packet_delivered_at, s.last_ack_at, s.last_work_block_at,
		       s.last_work_release_at, s.last_work_complete_at, s.last_work_heartbeat_at,
		       s.last_session_ready_at, s.last_session_heartbeat_at,
		       s.last_session_question_at, s.last_session_escalate_at,
		       s.last_pty_activity_at, `+db.SessionPipeReadProjection(ctx, tx, "s")+`,
		       s.last_tool_call_started_at,
		       s.last_tool_call_finished_at,
		       s.liveness_stall_class, s.liveness_stall_since,
		       al.lease_id AS active_lease_id, al.acquired_at AS active_lease_acquired_at,
		       al.expires_at AS active_lease_expires_at,
		       al.last_heartbeat_at AS active_lease_last_heartbeat_at,
		       sp.pid AS supervisor_pointer_pid,
		       sp.pid_start_time AS supervisor_pointer_pid_start_time,
		       sp.state AS supervisor_pointer_state,
		       sp.metadata_json AS supervisor_pointer_metadata_json
		  FROM striatumd.jobs j
		  LEFT JOIN LATERAL (
		    SELECT lz.lease_id, lz.state, lz.expires_at, lz.owner_session_id
		      FROM striatumd.leases lz
		     WHERE lz.repository_id = j.repository_id
		       AND lz.resource_id = j.job_id
		     -- Prefer the ACTIVE lease first (#145): a same-second acquired_at tie
		     -- between a prior attempt's released lease and the live attempt's active
		     -- lease must resolve the live one, not let the random lease_id tiebreak
		     -- pick the released (closed-session) lease and falsely requeue while the
		     -- live lease still holds the job.
		     -- Then prefer the job's own current_lease_id (the inverse #145 shape,
		     -- caught by the ACE explicit-consumer fault fixture): when BOTH the
		     -- prior attempt's lease and the current attempt's lease are released
		     -- within the same second, the random lease_id tiebreak can resolve the
		     -- PRIOR attempt's lease, whose owner session is still active, masking
		     -- the genuinely dead current owner so the dead lane is never requeued.
		     -- IS NOT DISTINCT FROM keeps the predicate false (not NULL) when the
		     -- job's current_lease_id was already cleared (running-limbo).
		     ORDER BY (lz.state = 'active') DESC,
		              (lz.lease_id IS NOT DISTINCT FROM j.current_lease_id) DESC,
		              lz.acquired_at DESC, lz.lease_id DESC
		     LIMIT 1
		  ) l ON true
		  -- #291: bind the supervised session for a never-claimed (leaseless) queued
		  -- job. A hung supervised lane (dead tmux pane, or alive but never claiming
		  -- its packet) leaves its job in 'queued' with NO lease, so the lease-anchored
		  -- session join above resolves nothing and the whole job is invisible to
		  -- recovery. The pointer carries only run_id+session_id (no job_id), so we
		  -- bind by the SAME eligibility the claim path uses (claim.go: target_role_id
		  -- = session.role_id AND (lane unset OR session.lane_id match)) so recovery and
		  -- claim agree on which session would have taken the job. Gated on a
		  -- NON-TERMINAL pointer (starting/attached/detached) — a lost/stopped pointer
		  -- means the lane already tore down and there is no live bound session to act
		  -- on. To avoid mis-binding job A to session B in a multi-lane run (the repro
		  -- is two parallel build lanes), we bind ONLY when exactly one such session
		  -- matches (COUNT(*) OVER () = 1). Restricted to leaseless queued jobs so the
		  -- claimed/running/stale_lease + running-limbo paths keep their lease-latest
		  -- session resolution untouched.
		  LEFT JOIN LATERAL (
		    SELECT bound_session_id FROM (
		      SELECT bs.session_id AS bound_session_id,
		             COUNT(*) OVER () AS match_count
		        FROM striatumd.sessions bs
		        JOIN striatumd.process_supervisor_pointers bp
		          ON bp.repository_id = bs.repository_id
		         AND bp.session_id = bs.session_id
		         AND bp.state IN ('starting','attached','detached')
		       WHERE j.state = 'queued'
		         AND l.owner_session_id IS NULL
		         AND bs.repository_id = j.repository_id
		         AND bs.run_id = j.run_id
		         AND bs.state = 'active'
		         AND bs.role_id = j.role_id
		         AND (
		           NULLIF(j.lane_selector_json->>'lane_id','') IS NULL
		           OR bs.lane_id = j.lane_selector_json->>'lane_id'
		         )
		    ) bound_candidates
		     WHERE match_count = 1
		     LIMIT 1
		  ) bs ON true
		  LEFT JOIN striatumd.sessions s
		    ON s.repository_id = j.repository_id
		   AND s.session_id = COALESCE(l.owner_session_id, bs.bound_session_id)
		  LEFT JOIN LATERAL (
		    SELECT az.lease_id, az.acquired_at, az.expires_at, az.last_heartbeat_at
		      FROM striatumd.leases az
		     WHERE az.repository_id = s.repository_id
		       AND az.owner_session_id = s.session_id
		       AND az.state = 'active'
		     ORDER BY az.acquired_at DESC, az.lease_id DESC
		     LIMIT 1
		  ) al ON true
		  LEFT JOIN LATERAL (
		    SELECT pp.pid, pp.pid_start_time, pp.state, pp.metadata_json
		      FROM striatumd.process_supervisor_pointers pp
		     WHERE pp.repository_id = s.repository_id
		       AND pp.session_id = s.session_id
		     ORDER BY pp.updated_at DESC, pp.supervisor_id DESC
		     LIMIT 1
		  ) sp ON true
		 WHERE j.repository_id = $1
		   AND j.run_id = $2
		   -- #291: 'queued' is scanned so a never-claimed job with a hung bound
		   -- supervised session is recoverable. The hasBoundSupervisedSession guard
		   -- below ensures a normal queued job with NO bound session is never acted on.
		   AND j.state IN ('queued','claimed','running','stale_lease')
		 ORDER BY j.workflow_job_id
		 FOR UPDATE OF j`, repositoryID, runID)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		jobID := fmt.Sprint(row["job_id"])
		workflowJobID := fmt.Sprint(row["workflow_job_id"])
		jobState := fmt.Sprint(nullable(row["job_state"]))

		// #291 guard (MANDATORY): a 'queued' job is only ever acted on when its bound
		// supervised session is still ACTIVE — i.e. the #291 hung-but-active case (a
		// dead tmux pane still marked active, or an alive session that never claimed
		// its packet). Two healthy/converged populations MUST be skipped here, or the
		// newly-scanned 'queued' state would re-requeue them on every sweep:
		//   1. A normal freshly-queued job with NO resolvable bound session (waiting
		//      for a lane to spawn, or just enqueued) — empty session_id.
		//   2. A job ALREADY requeued by a prior sweep / dead-lane path, whose only
		//      resolvable session is a closed/absent one reachable via its stale
		//      released lease. It is already claimable and just awaiting a fresh lane;
		//      acting again would break the convergence invariant (the dead-lane
		//      requeue case keeps its own scan via the claimed/running/stale_lease
		//      states and is NOT re-entered through 'queued').
		// Only an active bound session represents an unhandled hung lane wedging the
		// run, which the switch below then classifies (CASE 2 transfer for an honest
		// stall, or the confirmedDead default for a dead pane/PID) and recovers by
		// closing the hung owner so a fresh lane can claim the already-pending job.
		if jobState == "queued" {
			boundSession := fmt.Sprint(nullable(row["session_id"]))
			boundSessionState := fmt.Sprint(nullable(row["session_state"]))
			if boundSession == "" || boundSession == "<nil>" || boundSessionState != "active" {
				continue
			}
		}

		// Classify the owning session. A job with no resolvable owning session
		// row is treated as absent (dead lane / closed session) — Classify on an
		// empty activity reports the inactive/dead protocol.
		activity := sessionliveness.ActivityFromRow(row)
		result := sessionliveness.Classify(activity, sessionliveness.DefaultPolicy(), now)
		protocol := result.Protocol
		stallClass := result.StallClass
		if stallClass == "" {
			stallClass = protocol
		}
		// RFC 0131 Layer 1: transport is a liveness-evidence OUTPUT this slice plumbs
		// onto recovery.* events. result.ProbeBasis (see probeBasis() below) is the
		// typed evidence basis; transport records which lane class produced it.
		transport := activity.Transport

		sessionID := fmt.Sprint(nullable(row["session_id"]))
		sessionState := fmt.Sprint(nullable(row["session_state"]))
		sessionAbsent := sessionID == "" || sessionID == "<nil>"
		sessionDead := sessionAbsent ||
			protocol == sessionliveness.ProtocolDead ||
			(sessionState != "" && sessionState != "active" && sessionState != "<nil>")

		leaseState := fmt.Sprint(nullable(row["lease_state"]))
		hasActiveLease := leaseState == "active"
		// "released/expired/absent" — the dead-lane requeue precondition.
		leaseClearedOrGone := !hasActiveLease

		// CASE 3: a leaked interrogation window. An awaiting-interrogation target
		// session with no pending panel consumers blocks the run. Handle it by
		// releasing the interrogable target for any completed review depending on
		// this job; maybeCloseInterrogationTarget is the idempotent guard.
		if sessionID != "" && sessionID != "<nil>" {
			closed, ierr := closeLeakedInterrogationWindow(ctx, tx, repositoryID, runID, jobID, sessionID)
			if ierr != nil {
				return nil, ierr
			}
			if closed {
				actions = append(actions, map[string]any{
					"workflow_job_id": workflowJobID,
					"job_id":          jobID,
					"action":          "interrogation_window_closed",
					"session_id":      sessionID,
				})
				// Re-fetch nothing; the window close does not requeue the job. Fall
				// through so a genuinely dead job is still reclaimed below.
			}
		}

		// confirmedDead memoizes the supervised-agent liveness probe (oracle-backed,
		// so this is a cache read on the production sweep). It is consulted lazily by
		// CASE 2 and the default branch, so a job that hits CASE 1 first never probes.
		deadProbed := false
		deadResult := false
		confirmedDead := func() bool {
			if !deadProbed {
				deadResult = supervisedAgentConfirmedDead(ctx, row)
				deadProbed = true
			}
			return deadResult
		}

		// RFC 0131 Layer 1: resolve the typed probe_basis at event-emission time.
		// Classify() stamps every stall deadline_elapsed_only (it has no oracle);
		// a pty_helper lane whose supervised process the confirmedDead oracle
		// positively judges dead is UPGRADED to pty_confirmed_dead. A pipe (or
		// unknown-transport) lane has no PTY oracle, so its basis stays
		// deadline_elapsed_only regardless of the probe. This is OUTPUT only — the
		// confidence gate / escape-valve cap is RFC 0131 131-C, not this slice.
		probeBasis := func() sessionliveness.ProbeBasis {
			basis := result.ProbeBasis
			// A dead/absent session (CASE 1) classifies as inactive, so Classify
			// leaves ProbeBasis empty. Reaching a recovery ACTION means a
			// deadline-based liveness decision was nonetheless made (the session is
			// gone / its lease lapsed), so default an empty basis to
			// deadline_elapsed_only before any oracle upgrade — keeping the recorded
			// basis always meaningful rather than blank.
			if basis == sessionliveness.ProbeBasisNone {
				basis = sessionliveness.ProbeBasisDeadlineElapsedOnly
			}
			if confirmedDead() {
				basis = sessionliveness.UpgradeProbeBasisConfirmedDead(transport, basis)
			}
			return basis
		}

		// #308(A): before choosing a requeue, an agent that engaged the work protocol
		// and emitted output but never sealed (deadAgentExitedUnsealed) — whose lane
		// is now confirmed dead OR dead-by-session-state — should be AUTO-FINALIZED
		// from its already-published, body-reconstructable required artifacts instead
		// of requeued. The deliverable is durable; re-running a (often max_attempts=1)
		// final job both wastes a model invocation and cannot succeed once the
		// requeue budget is spent, wedging the run at needs_operator. This reuses the
		// proven D200 finalize-from-durable-artifact path (finalizeStalledJob) and all
		// its safety gates (verdict-capable refusal, presence + reconstructability),
		// so it only fires when the work is genuinely complete-but-unsealed. A dead
		// lane with NO durable artifact still falls through to the requeue path below.
		deadLaneUnsealed := (sessionDead && leaseClearedOrGone && !leaseStaleActive(row)) ||
			(confirmedDead() && deadAgentExitedUnsealed(activity))
		if deadLaneUnsealed && deadAgentExitedUnsealed(activity) {
			finalized, ferr := tryFinalizeUnsealedFromDurableArtifact(ctx, tx, repositoryID, jobID)
			if ferr != nil {
				return nil, ferr
			}
			if finalized {
				// Close the (possibly still-active) owning session so it cannot wake to
				// double-work the job that is now terminally completed. Idempotent.
				ownerClosed := false
				if !sessionAbsent {
					closed, cerr := closeStalledOwningSession(ctx, tx, repositoryID, runID, jobID, sessionID, stallClassAgentExitedUnsealed)
					if cerr != nil {
						return nil, cerr
					}
					ownerClosed = closed
				}
				actions = append(actions, map[string]any{
					"workflow_job_id":       workflowJobID,
					"job_id":                jobID,
					"action":                "auto_finalize_unsealed",
					"stall_class":           stallClassAgentExitedUnsealed,
					"acted":                 true,
					"stalled_owner_closed":  ownerClosed,
					"stalled_owner_session": nullable(sessionID),
				})
				continue
			}
			// #317: #308 finalize did not fire. If a required artifact ROW was
			// already published THIS attempt but its body is NOT durable, a
			// same-attempt requeue would wedge: the (logical_name, attempt) row is
			// immutable, so the re-run can neither republish nor complete (sealing a
			// stale, non-durable artifact) and blocks on
			// artifact_immutable_byline_mismatch with no automated recovery. Reopen
			// on a FRESH attempt instead, so the re-run publishes into a clean
			// namespace; the prior attempt's append-only row is retained.
			reopened, fromAttempt, toAttempt, rerr := tryReopenFreshAttemptForStaleArtifact(ctx, tx, repositoryID, row)
			if rerr != nil {
				return nil, rerr
			}
			if reopened {
				ownerClosed := false
				if !sessionAbsent {
					closed, cerr := closeStalledOwningSession(ctx, tx, repositoryID, runID, jobID, sessionID, stallClassAgentExitedUnsealed)
					if cerr != nil {
						return nil, cerr
					}
					ownerClosed = closed
				}
				// #373A: clear any open NON-escalation autonomous blocker so the
				// reopened attempt can proceed (same selection as the requeue path).
				blockersResolved, brerr := resolveDanglingAutonomousBlockersOnRequeue(ctx, tx, repositoryID, runID, jobID, sessionID, nowString())
				if brerr != nil {
					return nil, brerr
				}
				actions = append(actions, map[string]any{
					"workflow_job_id":            workflowJobID,
					"job_id":                     jobID,
					"action":                     "reopen_fresh_attempt_stale_artifact",
					"stall_class":                stallClassAgentExitedUnsealed,
					"from_attempt":               fromAttempt,
					"to_attempt":                 toAttempt,
					"acted":                      true,
					"stalled_owner_closed":       ownerClosed,
					"stalled_owner_session":      nullable(sessionID),
					"dangling_blockers_resolved": blockersResolved,
				})
				continue
			}
		}

		// #373B guardrail: a still-active session in the ProtocolAttention posture
		// (agent_escalation_pending) is presumed "awaiting human" — but ONLY when a
		// genuine human-attention blocker was actually raised. session.escalate stamps
		// last_session_escalate_at (driving ProtocolAttention) WITHOUT necessarily
		// raising a human_checkpoint/escalation-class blocker, so an attention session
		// with no such blocker is a dead/parked lane wedging the run forever. We probe
		// the open-blocker frontier per-job so the reap path below leaves a real
		// human gate for the operator and only reaps the false-attention wedge.
		hasHumanAttention, haerr := jobHasOpenHumanAttentionBlocker(ctx, tx, repositoryID, jobID)
		if haerr != nil {
			return nil, haerr
		}

		// Decide the operational recovery action.
		var action, counterColumn string
		var forceExpire bool
		var closeStalledOwner bool
		switch {
		case sessionDead && leaseClearedOrGone && !leaseStaleActive(row):
			// CASE 1: dead/closed/absent owner, lease released/expired/absent, and
			// the auto-publish pass already ran (any recoverable artifact would have
			// completed the job, so a job still unfinished here has none). Requeue
			// on the same attempt. CASE 1 takes precedence for a dead/absent session;
			// the !sessionDead guard on CASE 2 makes the precedence unambiguous (a
			// closed/absent session is sessionDead and never broadens into CASE 2).
			action = "requeue_same_attempt"
			counterColumn = "requeue_count"
		case !sessionDead && protocol == sessionliveness.ProtocolStalled && !hasHumanAttention && !confirmedDead():
			// CASE 2 (RFC 0101 Phase 3 Slice 2b, #121 parked agent): the owning
			// session is still present and state='active' but HONESTLY stalled — and
			// its supervised agent PROCESS is NOT confirmed dead. The confirmed-dead
			// exclusion (#289) matters because a dead agent's activity timestamps
			// freeze at the moment of death, so a sweep that runs after they age past
			// the liveness deadlines would otherwise misread a dead lane as a live
			// parked one and transfer it on the transfer_count budget. A confirmed-dead
			// agent must instead fall through to the dead-lane requeue path below, where
			// the unsealed-exit classification and its smaller budget apply. The
			// Phase 1 honest-liveness contract guarantees protocol==stalled means NO
			// Phase 1 honest-liveness contract guarantees protocol==stalled means NO
			// protocol + NO PTY + NO tool-call progress past the deadline — i.e.
			// genuinely stuck — so acting on it is safe regardless of lease state.
			// This fires whether the lease is expired, released, or absent: by the
			// time the decision tree runs, HandleRecoveryAuto's expireLeases pass has
			// already flipped a past-expiry active lease to 'expired', so the old
			// `hasActiveLease && leaseExpired` precondition could never observe the
			// live #121 case. It is a transfer to a fresh session, on the
			// transfer_count budget. requeueJobSameAttempt already force-expires any
			// residual active lease, so dropping the hasActiveLease precondition is
			// safe; forceExpire stays true as belt-and-suspenders. Because the
			// session never closed itself, also close the superseded stalled owner
			// below so the parked lane cannot wake up to double-work or reclaim.
			action = "transfer_requeue"
			counterColumn = "transfer_count"
			forceExpire = true
			closeStalledOwner = true
		case !sessionDead && protocol == sessionliveness.ProtocolAttention &&
			leaseClearedOrGone && !leaseStaleActive(row) && !hasHumanAttention && !confirmedDead():
			// CASE 2b (#373B no-lease attention reap): the owning session is still
			// state='active' and in the ProtocolAttention posture
			// (agent_escalation_pending) past its deadline, holds NO active lease, has
			// NO genuine open human-attention blocker (the guardrail above), and its
			// supervised agent PROCESS is not confirmed dead (indeterminate). The repro
			// (#373) is a relaunched codex lane that posted session.escalate but never
			// raised a human_checkpoint, then froze: the job sat behind a dead-but-
			// active session forever, healed only by a manual `session close`. This is
			// exactly what CASE 2 does for the protocol-idle stall — reap (close) the
			// stalled owner and transfer the attempt to a fresh lane — so we treat the
			// no-blocker attention session identically. The hasHumanAttention guard
			// (an open human_checkpoint / escalation-class blocker) keeps a REAL human
			// gate untouched: it falls through to default and, absent a confirmed-dead
			// probe, is left for the operator. Restricted to no_lease so an attention
			// session that still holds a live lease is never reaped mid-gate.
			action = "transfer_requeue"
			counterColumn = "transfer_count"
			forceExpire = true
			closeStalledOwner = true
		default:
			// working_* / quiet (pre-deadline), or a live lease that has not yet
			// expired: not genuinely stuck BY the session/lease liveness signal.
			// But lease/heartbeat freshness is forgeable by an out-of-band operator
			// `striatum heartbeat` loop (the documented long-command lease-expiry
			// mitigation), while an actually-dead agent PROCESS is not. If the
			// supervised agent is confirmed dead, this is a masked dead lane (#147
			// Symptom B) that would otherwise sit `running` forever — requeue it on
			// the same attempt and close the falsely-active owning session. The
			// probe is positive (a dead PID / dead pane), so a genuinely-working
			// lane — including one whose supervisor bridge merely detached while the
			// agent runs (#147 Symptom A) — is never requeued, and an indeterminate
			// probe leaves the job untouched.
			if !confirmedDead() {
				continue
			}
			action = "requeue_same_attempt"
			counterColumn = "requeue_count"
			forceExpire = true
			closeStalledOwner = true
			// #289: distinguish an agent that engaged the work protocol and produced
			// output but never sealed (no work.complete) from a hard early crash that
			// never got going. Both respawn (we cannot seal on the agent's behalf), but
			// the unsealed variant gets a distinct class, a smaller requeue budget, and
			// an inspect-the-worktree escalation remediation. The signal is coarse on
			// purpose: it fires for any confirmed-dead agent that made an MCP tool call
			// and emitted PTY output — whether it finished a deliverable then exited at
			// turn-end / context-budget / rate-limit, or merely got going then died —
			// and both populations are well served by "inspect the worktree, retry
			// once, then escalate". Only the never-engaged early crash stays
			// agent_pid_dead.
			if deadAgentExitedUnsealed(activity) {
				stallClass = stallClassAgentExitedUnsealed
			} else {
				stallClass = stallClassAgentPIDDead
			}
		}

		budget, berr := readJobRecoveryBudget(ctx, tx, repositoryID, jobID)
		if berr != nil {
			return nil, berr
		}
		current := budget.requeueCount
		limit := policy.maxRequeues
		if counterColumn == "transfer_count" {
			current = budget.transferCount
			limit = policy.maxTransfers
		} else if stallClass == stallClassAgentExitedUnsealed {
			// #289: the unsealed-exit class shares the requeue_count counter but
			// escalates after a smaller budget — a respawn rarely heals a systematic
			// unsealed exit, so hand it to the operator sooner with a precise reason.
			// #478 / D249: the bound is lane-kind-differentiated — a read-only reviewer
			// lane gets the larger reviewer bound (a transient unsealed exit there is a
			// clean fresh-session retry), while a stateful repo-write lane keeps the
			// tight default (a repeated unsealed exit signals systematic failure).
			limit = policy.unsealedRequeueBudget(isRepoWrite(row))
		}
		// gateEscalation is set when the RFC 0131 131-C confidence gate PERMITTED a
		// deadline_elapsed_only escalation to commit this sweep (the two-sweep bar
		// cleared, or the Layer-4 cap fired). It enriches the budget_exhausted event
		// with WHY it finally escalated; nil for a pty_confirmed_dead (oracle) basis
		// or a deployment behind on migration 0035 (degrade-safe ungated path).
		var gateEscalation *confidenceGateDecision
		if current >= limit {
			// Budget exhausted: the pre-131-C behavior escalates immediately. RFC
			// 0131 Layer 3/4 (131-C) interposes a CONFIDENCE GATE for the pipe /
			// no-oracle case: a job whose stall basis is deadline_elapsed_only (a
			// deadline merely elapsed on a lane with no confirmed-dead oracle) must
			// clear a higher bar — two consecutive silent sweeps with no
			// forgery-resistant progress AND no corroborating live sibling — OR the
			// Layer-4 escape-valve cap — before it may flip the run toward
			// needs_operator. A pty_confirmed_dead basis (the PTY oracle fired) skips
			// the gate and escalates immediately, exactly as today.
			//
			// The gate is scoped to the NO-ORACLE ambiguous case — a still-present
			// pipe lane that MIGHT be quietly working (RFC 0131 Problem gap 2/3). Two
			// exclusions keep it from suppressing a lane that actually has a death
			// signal:
			//   - !sessionDead: a CLOSED / absent / protocol-dead session is itself a
			//     forgery-resistant death signal (the session row is terminal,
			//     independent of any PTY oracle), so it escalates immediately, exactly
			//     as before (preserves the dead-lane CASE-1 timing).
			//   - !confirmedDead(): the supervised-agent process probe positively
			//     judged the agent dead (e.g. the #289 agent_exited_unsealed class on an
			//     active session). That IS a real oracle regardless of transport — even
			//     when the basis did not upgrade to pty_confirmed_dead (a pipe/unknown
			//     transport) — so a confirmed-dead lane escalates immediately and is
			//     NOT debounced. Only a lane with NO confirmed-dead oracle and only an
			//     elapsed deadline is the genuinely-ambiguous case the gate exists for.
			gateApplies := !sessionDead && !confirmedDead() &&
				probeBasis() == sessionliveness.ProbeBasisDeadlineElapsedOnly
			if gateApplies {
				// Forgery-resistant progress since the last sweep: a sealed artifact
				// anchor / sealed verdict advanced (daemon-recorded sealed-work the
				// #324 spinner cannot manufacture), OR a sibling in the in-memory
				// cohort shows liveness fresher than this job's last recovery touch.
				sealedAt, hasSealed, serr := jobSealedProgressAt(ctx, tx, repositoryID, jobID)
				if serr != nil {
					return nil, serr
				}
				windowStart := windowStartForGate(budget, now)
				progressAdvanced := (hasSealed && sealedAt.After(windowStart)) ||
					cohortHasFresherLiveness(rows, jobID, windowStart, now)

				// RFC 0131 #349: resolve the topology-adaptive escape-valve cap. A job
				// whose downstream dependent is still blocked (a critical path waiting on
				// it) escalates at the tight single-cap floor; a leaf job whose silence
				// wedges nothing downstream gets a loosened cap (more room for a long
				// legitimate silent generation turn). The single cap stays the floor, so
				// no job ever escalates sooner than the pre-#349 behavior.
				downstreamBlocked, dberr := jobHasBlockedDownstream(ctx, tx, repositoryID, jobID)
				if dberr != nil {
					return nil, dberr
				}
				silentCap := policy.topologyAdaptiveSilentSweepCap(downstreamBlocked)
				escalateThreshold := policy.topologyAdaptiveEscalationThreshold(downstreamBlocked)

				gate, gerr := applyConfidenceGate(ctx, tx, repositoryID, runID, jobID, budget, policy, progressAdvanced, escalateThreshold, silentCap)
				if gerr != nil {
					return nil, gerr
				}
				if !gate.escalate {
					// Debounced: do NOT escalate this sweep. Record the gate state so
					// an operator (and doctor/dashboard) sees "deferred, N/cap silent
					// sweeps" rather than silence.
					if _, eerr := appendEvent(ctx, tx, repositoryID, runID, "recovery.escalation_debounced", nullable(sessionID), jobID, nil, nil, nil, map[string]any{
						"workflow_job_id":           workflowJobID,
						"action":                    action,
						"budget":                    counterColumn,
						"count":                     current,
						"limit":                     limit,
						"stall_class":               stallClass,
						"transport":                 transportPayloadValue(transport),
						"probe_basis":               string(probeBasis()),
						"consecutive_silent_sweeps": gate.silentSweeps,
						"misfire_evidence_score":    gate.misfireScore,
						"silent_sweep_cap":          gate.silentCap,
						"progress_reset":            gate.reset,
						"escalation_pending":        false,
					}); eerr != nil {
						return nil, eerr
					}
					actions = append(actions, map[string]any{
						"workflow_job_id":           workflowJobID,
						"job_id":                    jobID,
						"action":                    action,
						"budget":                    counterColumn,
						"count":                     current,
						"limit":                     limit,
						"escalation_debounced":      true,
						"escalation_pending":        false,
						"acted":                     false,
						"transport":                 transportPayloadValue(transport),
						"probe_basis":               string(probeBasis()),
						"consecutive_silent_sweeps": gate.silentSweeps,
						"misfire_evidence_score":    gate.misfireScore,
						"silent_sweep_cap":          gate.silentCap,
						"progress_reset":            gate.reset,
					})
					continue
				}
				// gate.escalate: fall through to the escalation path below. The
				// recovery.budget_exhausted event records WHY it finally fired (the
				// Layer-4 cap, or the two-consecutive-sweep bar) via the gate fields.
				gateEscalation = &gate
			}
			// Budget exhausted: do NOT act. Flag escalation_pending (Phase 4
			// consumes it) and record it once. Idempotent.
			if eerr := markRecoveryEscalation(ctx, tx, repositoryID, runID, jobID, action+"_budget_exhausted", stallClass); eerr != nil {
				return nil, eerr
			}
			budgetExhaustedPayload := map[string]any{
				"workflow_job_id":    workflowJobID,
				"action":             action,
				"budget":             counterColumn,
				"count":              current,
				"limit":              limit,
				"stall_class":        stallClass,
				"escalation_pending": true,
				// RFC 0131 Layer 1: transport + typed probe_basis make every
				// escalation record WHAT KIND of evidence it acted on — a genuine
				// pty_confirmed_dead vs a deadline_elapsed_only that merely elapsed
				// on a no-oracle (pipe) lane.
				"transport":   transportPayloadValue(transport),
				"probe_basis": string(probeBasis()),
			}
			if gateEscalation != nil {
				// RFC 0131 131-C: record WHY the confidence gate finally permitted this
				// deadline_elapsed_only escalation — the Layer-4 escape-valve cap fired,
				// or the two-consecutive-sweep bar cleared — so the escalation is
				// auditable provenance, never silent daemon behavior (Goal 5).
				budgetExhaustedPayload["confidence_gated"] = true
				budgetExhaustedPayload["cap_fired"] = gateEscalation.capFired
				budgetExhaustedPayload["consecutive_silent_sweeps"] = gateEscalation.silentSweeps
				budgetExhaustedPayload["misfire_evidence_score"] = gateEscalation.misfireScore
				budgetExhaustedPayload["silent_sweep_cap"] = gateEscalation.silentCap
			}
			if _, eerr := appendEvent(ctx, tx, repositoryID, runID, "recovery.budget_exhausted", nullable(sessionID), jobID, nil, nil, nil, budgetExhaustedPayload); eerr != nil {
				return nil, eerr
			}
			budgetExhaustedAction := map[string]any{
				"workflow_job_id":    workflowJobID,
				"job_id":             jobID,
				"action":             action,
				"budget":             counterColumn,
				"count":              current,
				"limit":              limit,
				"escalation_pending": true,
				"acted":              false,
				"transport":          transportPayloadValue(transport),
				"probe_basis":        string(probeBasis()),
			}
			if gateEscalation != nil {
				budgetExhaustedAction["confidence_gated"] = true
				budgetExhaustedAction["cap_fired"] = gateEscalation.capFired
				budgetExhaustedAction["consecutive_silent_sweeps"] = gateEscalation.silentSweeps
				budgetExhaustedAction["misfire_evidence_score"] = gateEscalation.misfireScore
				budgetExhaustedAction["silent_sweep_cap"] = gateEscalation.silentCap
			}
			actions = append(actions, budgetExhaustedAction)
			continue
		}

		// Force-expire a residual active-but-expired lease before requeue (the
		// transfer case). requeueJobSameAttempt also force-expires any active
		// lease pinned to the job, so this is belt-and-suspenders for clarity.
		if forceExpire {
			if err := tx.Exec(ctx, `
				UPDATE striatumd.leases
				   SET state = 'expired', released_at = COALESCE(released_at, $1),
				       release_reason = 'recovery_transfer'
				 WHERE repository_id = $2 AND resource_id = $3 AND state = 'active'`,
				nowString(), repositoryID, jobID); err != nil {
				return nil, err
			}
		}

		repoWrite := isRepoWrite(row)
		opts := requeueSameAttemptOptions{
			author:        recoveryDecisionAuthor,
			justification: fmt.Sprintf("autonomous recovery: owning session %s (%s); %s", sessionStateLabel(sessionID, sessionState), stallClass, action),
		}
		if repoWrite {
			// The bounded daemon sweep IS the inspection D036 requires of an
			// interactive operator, so autonomous recovery overrides the repo-write
			// manual gate.
			opts.operatorOverride = true
		}
		res, rqerr := requeueJobSameAttempt(ctx, tx, repositoryID, row, opts)
		if rqerr != nil {
			return nil, rqerr
		}
		// RFC 0101 Phase 3 Slice 2b: when CASE 2 transferred a job away from a
		// still-active stalled owning session, close that session so the parked
		// lane cannot wake up to double-work or reclaim the job a fresh lane now
		// owns. Mirrors the #121 manual flow (the operator did `session close`).
		// Only the session that OWNS this job is touched; interrogation-target
		// sessions are handled by the panel-window logic (closeLeakedInterrogationWindow),
		// not here. The close is guarded on still-active (idempotent).
		//
		// #291: for a never-claimed 'queued' job whose bound supervised session is
		// hung (dead pane, or alive-but-leaseless), the job's work message is ALREADY
		// pending so requeueJobSameAttempt is a no-op (alreadyReclaimable=true) — but
		// the run is still wedged because the hung session is what blocks progress.
		// Closing that owning session IS the recovery action here (the manual
		// `supervise stop` the issue reporter had to run by hand), so it must happen
		// even on the already-reclaimable path. We therefore close the stalled owner
		// before deciding how to report the action.
		ownerClosed := false
		if closeStalledOwner && !sessionAbsent {
			closed, cerr := closeStalledOwningSession(ctx, tx, repositoryID, runID, jobID, sessionID, stallClass)
			if cerr != nil {
				return nil, cerr
			}
			ownerClosed = closed
		}
		// #373A: a requeue/transfer must also clear the job's open NON-escalation
		// autonomous blockers (e.g. an agent-raised branch_contamination), or the
		// requeued attempt cannot proceed behind its own dangling blocker. Reuses the
		// EXACT #304 completion-time selection so a genuine human-attention blocker
		// (human_checkpoint / waiting_human / any escalation-class kind, including a
		// recovery_exhausted escalation) is NEVER auto-resolved here. Done even on the
		// already_reclaimable path: a prior sweep may have requeued the message while
		// leaving the blocker open, and a still-open blocker is exactly the wedge.
		blockersResolved, brerr := resolveDanglingAutonomousBlockersOnRequeue(ctx, tx, repositoryID, runID, jobID, sessionID, nowString())
		if brerr != nil {
			return nil, brerr
		}
		if res.alreadyReclaimable && !ownerClosed && blockersResolved == 0 {
			// Convergent no-op: the job was already queued+pending (a prior sweep
			// requeued it), there was no live owning session to close, and no dangling
			// blocker to clear. Do NOT increment the budget; just note it.
			actions = append(actions, map[string]any{
				"workflow_job_id": workflowJobID,
				"job_id":          jobID,
				"action":          action,
				"acted":           false,
				"reason":          "already_reclaimable",
			})
			continue
		}
		if err := recordRecoveryAction(ctx, tx, repositoryID, runID, jobID, counterColumn, action, stallClass); err != nil {
			return nil, err
		}
		actions = append(actions, map[string]any{
			"workflow_job_id":            workflowJobID,
			"job_id":                     jobID,
			"action":                     action,
			"budget":                     counterColumn,
			"count":                      current + 1,
			"limit":                      limit,
			"repo_write":                 repoWrite,
			"stall_class":                stallClass,
			"acted":                      true,
			"stalled_owner_closed":       ownerClosed,
			"stalled_owner_session":      nullable(sessionID),
			"dangling_blockers_resolved": blockersResolved,
			// RFC 0131 Layer 1: a requeue/transfer action records the same
			// transport + typed probe_basis it acted on, so the action audit trail
			// is as legible as the budget_exhausted event.
			"transport":   transportPayloadValue(transport),
			"probe_basis": string(probeBasis()),
		})
	}
	return actions, nil
}

// jobHasOpenHumanAttentionBlocker reports whether the job has ANY open blocker
// that genuinely requires a human (#373B guardrail). This is the precise inverse
// of the autonomous-blocker set: a human_checkpoint (its job sits waiting_human)
// or any escalation-class kind (ambiguous_goal, missing_authority,
// recovery_exhausted, etc., per isEscalationClassBlocker) must keep the job for
// the operator and must NEVER be reaped by recovery. It is evaluated per-job on
// the open-blocker frontier so a session that posted session.escalate AND raised
// a real human_checkpoint is protected, while one that merely went attention with
// NO genuine human blocker (the #373B wedge) is reapable.
func jobHasOpenHumanAttentionBlocker(ctx context.Context, tx db.TxRunner, repositoryID, jobID string) (bool, error) {
	openBlockers, err := queryRows(ctx, tx, `
		SELECT severity, blocker_kind
		  FROM striatumd.blockers
		 WHERE repository_id = $1 AND job_id = $2 AND state = 'open'`, repositoryID, jobID)
	if err != nil {
		return false, err
	}
	for _, blocker := range openBlockers {
		if isEscalationClassBlocker(blocker) {
			return true, nil
		}
	}
	return false, nil
}

// closeStalledOwningSession closes a still-active stalled session that OWNS a job
// the decision tree just transferred (CASE 2). It is guarded on state='active'
// so a session another actor already closed (or that was never active) is left
// untouched — making it idempotent and safe to re-run. It records a
// session.closed event with the recovery reason so the audit trail is honest.
// It deliberately does NOT touch interrogation-target sessions: those are the
// panel-window logic's responsibility (closeLeakedInterrogationWindow).
func closeStalledOwningSession(ctx context.Context, tx db.TxRunner, repositoryID, runID, jobID, sessionID, stallClass string) (bool, error) {
	if sessionID == "" || sessionID == "<nil>" {
		return false, nil
	}
	// Guard: only close a session that is STILL active (idempotent — a session
	// some other actor already closed, or that was never active, is left alone).
	// The decision tree already holds FOR UPDATE on the owning job; the session
	// row itself is read fresh here so a concurrent close is observed.
	stillActive, err := existsRow(ctx, tx, `
		SELECT 1 FROM striatumd.sessions
		 WHERE repository_id = $1 AND session_id = $2 AND state = 'active'
		 LIMIT 1`, repositoryID, sessionID)
	if err != nil {
		return false, err
	}
	if !stillActive {
		return false, nil
	}
	now := nowString()
	const closeReason = "recovery_stalled_transfer"
	if err := tx.Exec(ctx, `
		UPDATE striatumd.sessions
		   SET state = 'closed', closed_at = $1, close_reason = $2
		 WHERE repository_id = $3 AND session_id = $4 AND state = 'active'`,
		now, closeReason, repositoryID, sessionID); err != nil {
		return false, err
	}
	closePayload := map[string]any{
		"session_id":  sessionID,
		"reason":      closeReason,
		"source":      "recovery_decision_tree",
		"stall_class": stallClass,
	}
	// RFC 0137 Phase B (deliverable #4): tag the lifecycle-metric class AT THE
	// SITE so the exporter's sweep-tick fold can split apoptosis (clean programmed
	// closes) from necrosis (confirmed-dead closes) without re-deriving intent
	// downstream. A confirmed-dead stall class is necrosis; any other recovery
	// close here is an honest-stall transfer — neither a clean apoptosis nor a
	// death — so it is tagged distinctly and the fold excludes it from both
	// counters (it surfaces in lease_transitions instead). This additive payload
	// key changes no recovery behavior.
	if isNecrosisStallClass(stallClass) {
		closePayload[metrics.LifecycleTagKey] = metrics.LifecycleTagNecrosis
	} else {
		closePayload[metrics.LifecycleTagKey] = metrics.LifecycleTagRecoveryTransfer
	}
	if _, err := appendEvent(ctx, tx, repositoryID, runID, "session.closed", sessionID, jobID, nil, nil, nil, closePayload); err != nil {
		return false, err
	}
	return true, nil
}

// closeLeakedInterrogationWindow closes a leaked interrogation window owned by
// sessionID for this job: if the session is an interrogable target whose panel
// has no pending consumers, maybeCloseInterrogationTarget retires it. Returns
// whether a window was actually closed. Best-effort + idempotent.
func closeLeakedInterrogationWindow(ctx context.Context, tx db.TxRunner, repositoryID, runID, jobID, sessionID string) (bool, error) {
	// Only attempt a close when the session is currently sitting in an
	// awaiting-interrogation / interrogation target posture; maybeCloseInterrogationTarget
	// itself guards on active state, no open interrogations, no active lease, and
	// no pending panel consumers, so calling it unconditionally is safe but we
	// avoid the work when there is plainly no interrogation row for the session.
	hasInterrogation, err := existsRow(ctx, tx, `
		SELECT 1 FROM striatumd.interrogations
		 WHERE repository_id = $1 AND target_session_id = $2
		 LIMIT 1`, repositoryID, sessionID)
	if err != nil {
		return false, err
	}
	if !hasInterrogation {
		return false, nil
	}
	return maybeCloseInterrogationTarget(ctx, tx, repositoryID, runID, sessionID)
}

// tryFinalizeUnsealedFromDurableArtifact is the #308(A) autonomous finalize: for
// a dead-lane job that engaged the work protocol but never sealed (the caller has
// already established deadAgentExitedUnsealed), it auto-finalizes the job from its
// already-published, body-reconstructable required artifacts — the same D200
// finalize-from-durable-artifact path the manual `recovery complete-stalled` verb
// runs — INSTEAD of requeueing it. It returns finalized=false (and leaves the job
// untouched, so the caller falls through to the requeue path) whenever the
// deliverable is NOT safely finalizable:
//
//   - verdict-capable jobs (review / phase_synthesis) complete via a recorded,
//     attested verdict; finalizing from an artifact would bypass RFC 0118
//     verdict attestation, so they are never auto-finalized here.
//   - a job with NO required expected_artifact has no durable deliverable to
//     finalize from — re-running it is the correct recovery, so this returns false.
//   - if any required artifact ROW is missing (publish never happened) or its
//     BODY is not reconstructable from its declared placement (RFC 0125 P0-3,
//     worktree-independent), the work is not actually durable, so this returns
//     false and the requeue path handles it.
//
// Reusing finalizeStalledJob keeps a single completion code path (job → completed,
// lease released, autonomous-blocker resolution, downstream enqueue, run
// completion) so the autonomous finalize and the operator verb cannot drift.
func tryFinalizeUnsealedFromDurableArtifact(ctx context.Context, tx db.TxRunner, repositoryID, jobID string) (bool, error) {
	job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		return false, err
	}
	// Verdict-capable jobs must complete via attested verdict (RFC 0118): never
	// auto-finalize them from an artifact here.
	if isVerdictCapableJobType(fmt.Sprint(job["job_type"])) {
		return false, nil
	}
	// Require at least one REQUIRED expected_artifact: a job with no durable
	// deliverable to finalize from must be re-run, not silently completed.
	attempt := jobAttemptValue(job["attempt"])
	expected := resolveExpectedArtifactCycles(asList(job["expected_artifacts_json"]), attempt)
	hasRequired := false
	for _, item := range expected {
		if asMap(item)["required"] == true {
			hasRequired = true
			break
		}
	}
	if !hasRequired {
		return false, nil
	}
	// #530 salvage: a per-job lane can write its deliverable to its isolated
	// worktree and then exit unsealed BEFORE artifact.publish (e.g. a transient
	// API outage during end-of-session wind-down), leaving no artifact row. Before
	// Gate 1 refuses on the missing row, scan the main checkout and the job's
	// per-job worktree for the written-but-unpublished file and publish the missing
	// required artifact ROW(s); the body, anchored via the worktree commit the
	// sweep pinned, then satisfies the reconstructability gate. A no-op when the
	// rows already exist or no matching file is on disk (then Gate 1 falls through
	// to the requeue path as before).
	if _, serr := salvagePublishMissingRequiredArtifacts(ctx, tx, repositoryID, job); serr != nil {
		if _, ok := serr.(*rpc.Error); ok {
			return false, nil
		}
		return false, serr
	}
	// Gate 1: every required artifact ROW exists (publish happened). A missing row
	// returns an rpc invalid_transition error from verifyRequiredArtifacts; treat
	// it as "not finalizable" (fall through to requeue), not a sweep failure.
	if err := verifyRequiredArtifacts(ctx, tx, repositoryID, jobID); err != nil {
		if _, ok := err.(*rpc.Error); ok {
			return false, nil
		}
		return false, err
	}
	// Gate 2: every required artifact BODY is reconstructable from its declared
	// placement (RFC 0125 P0-3) — worktree-independent, so it holds even after the
	// per-job worktree was torn down, as long as the body is on the run branch /
	// blob store. Any positive-failure reconstruction means the deliverable is not
	// durable -> do not finalize.
	recon, err := verifyRequiredArtifactReconstructable(ctx, tx, repositoryID, job)
	if err != nil {
		return false, err
	}
	if len(failedReconstructions(recon)) > 0 {
		return false, nil
	}
	if _, err := finalizeStalledJob(ctx, tx, repositoryID, job, "autonomous recovery: auto-finalized published-but-unsealed job from durable artifact (#308)"); err != nil {
		return false, err
	}
	return true, nil
}

// tryReopenFreshAttemptForStaleArtifact handles the #317 trap: a dead-lane job
// whose required artifact ROW was already published this attempt but whose body
// is NOT durable (so the #308 finalize above could not fire). A same-attempt
// requeue would trap the re-run — the (logical_name, attempt) row is immutable
// (0018_artifact_attempt_scope.sql) and the artifacts table is append-only
// (0005 triggers), so the re-run can neither republish (immutable, and a
// per-session byline guarantees a mismatch) nor complete (that would seal a
// stale, non-durable artifact); it blocks on artifact_immutable_byline_mismatch
// with no automated recovery. Reopen on a FRESH attempt instead: the re-run
// publishes into a clean (logical_name, attempt) namespace, leaving the prior
// attempt's append-only row intact for provenance. max_attempts is bumped in
// lockstep because this is recovery clearing its own prior partial work, not a
// content revision consuming the author's retry budget. Returns whether it acted
// and the (from, to) attempt numbers. Verdict-capable jobs are never reopened
// here (they route through attested verdict / review override, RFC 0118).
func tryReopenFreshAttemptForStaleArtifact(ctx context.Context, tx db.TxRunner, repositoryID string, row map[string]any) (bool, int, int, error) {
	jobID := fmt.Sprint(row["job_id"])
	runID := row["run_id"]
	if isVerdictCapableJobType(fmt.Sprint(row["job_type"])) {
		return false, 0, 0, nil
	}
	attempt := jobAttemptValue(row["attempt"])
	expected := resolveExpectedArtifactCycles(asList(row["expected_artifacts_json"]), attempt)
	hasRequired := false
	for _, item := range expected {
		if asMap(item)["required"] == true {
			hasRequired = true
			break
		}
	}
	if !hasRequired {
		return false, 0, 0, nil
	}
	// Required artifact ROWS must already exist (publish happened this attempt) —
	// otherwise a same-attempt requeue can simply republish and there is no trap.
	// A missing row surfaces as an rpc invalid_transition error; treat it as
	// "no trap" (fall through to the normal requeue), not a sweep failure.
	if err := verifyRequiredArtifacts(ctx, tx, repositoryID, jobID); err != nil {
		if _, ok := err.(*rpc.Error); ok {
			return false, 0, 0, nil
		}
		return false, 0, 0, err
	}
	// The body must be NON-durable. If it is reconstructable the #308 path above
	// finalizes it; reopening would needlessly re-run a complete deliverable.
	recon, err := verifyRequiredArtifactReconstructable(ctx, tx, repositoryID, row)
	if err != nil {
		return false, 0, 0, err
	}
	if len(failedReconstructions(recon)) == 0 {
		return false, 0, 0, nil
	}
	next := attempt + 1
	if err := tx.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET attempt = $3, max_attempts = GREATEST(max_attempts, $3)
		 WHERE repository_id = $1 AND job_id = $2`, repositoryID, jobID, next); err != nil {
		return false, 0, 0, err
	}
	updated, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		return false, 0, 0, err
	}
	opts := requeueSameAttemptOptions{
		author: recoveryDecisionAuthor,
		justification: fmt.Sprintf(
			"autonomous recovery (#317): prior attempt %d published a required artifact whose body is not durable; reopened on fresh attempt %d so the re-run can republish into a clean namespace",
			attempt, next),
	}
	if isRepoWrite(updated) {
		opts.operatorOverride = true
	}
	if _, err := requeueJobSameAttempt(ctx, tx, repositoryID, updated, opts); err != nil {
		return false, 0, 0, err
	}
	if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.reopened_fresh_attempt", nil, jobID, nil, nil, nil, map[string]any{
		"reason":       "stale_published_artifact_not_durable",
		"from_attempt": attempt,
		"to_attempt":   next,
	}); err != nil {
		return false, 0, 0, err
	}
	return true, attempt, next, nil
}

// deadAgentExitedUnsealed reports whether a confirmed-dead supervised agent had
// engaged the work protocol and produced output but never sealed it with
// work.complete (#289). The signal is: no work.complete for the owning session,
// AND it both made at least one MCP tool call (LastToolCall* — stamped by any
// tools/call: work.claim_next / await_packet / ack / heartbeat / publish) and
// emitted PTY output (LastPTYActivityAt). This is deliberately coarse — it does
// NOT prove a finished deliverable; it means "claimed a packet and printed
// something, then the process died unsealed." That covers both the
// finished-but-unsealed self-driving exit (turn-end / context-budget / rate-limit
// death) and the got-going-then-crashed case; recovery cannot tell them apart
// (the issue's own premise) and both warrant the same response — a smaller budget
// and an inspect-the-worktree escalation, never sealing on the agent's behalf
// (attestation forbids it). Only the never-engaged early crash (no tool call or
// no output) stays the generic agent_pid_dead class.
func deadAgentExitedUnsealed(activity sessionliveness.Activity) bool {
	if activity.LastWorkCompleteAt != nil {
		return false
	}
	engagedProtocol := activity.LastToolCallStartedAt != nil || activity.LastToolCallFinishedAt != nil
	producedOutput := activity.LastPTYActivityAt != nil
	return engagedProtocol && producedOutput
}

// supervisedAgentConfirmedDead reports whether the owning session's supervised
// agent PROCESS is positively dead — the #147 Symptom B signal that, unlike
// lease/heartbeat freshness, an out-of-band operator `striatum heartbeat` loop
// cannot forge. It requires a recorded supervisor pointer: a pointer already
// marked lost/stopped is conclusive; otherwise the recorded agent is probed with
// the same ProbeLaneLiveness the process reconcile uses (the tmux pane for tmux
// lanes, the PID + start-token for plain lanes, so a reused PID is not mistaken
// for the original). An UNAVAILABLE probe (e.g. tmux not reachable) returns
// false: a lane we cannot positively judge dead is left alone, never requeued.
func supervisedAgentConfirmedDead(ctx context.Context, row map[string]any) bool {
	state := fmt.Sprint(nullable(row["supervisor_pointer_state"]))
	if state == "lost" || state == "stopped" {
		return true
	}
	pid := intFromAny(row["supervisor_pointer_pid"], 0)
	if pid <= 0 {
		return false // no recorded agent process to judge
	}
	metadata := asMap(row["supervisor_pointer_metadata_json"])
	expectedStart := fmt.Sprint(nullable(row["supervisor_pointer_pid_start_time"]))
	if expectedStart == "<nil>" {
		expectedStart = ""
	}
	// #198: in the periodic sweep this reads the pre-tx liveness snapshot
	// (probeLaneLivenessCached), so the `tmux list-panes` / /proc probe never runs
	// while the sweep transaction holds the per-run advisory lock + FOR UPDATE row
	// locks. Operator RPCs and unit tests carry no oracle and probe live.
	live := probeLaneLivenessCached(ctx, metadata, pid, expectedStart)
	switch live.Class {
	case string(gosupervisor.TmuxLivenessUnavailable), gosupervisor.PIDLivenessIdentityUnavailable:
		// Cannot determine; do not requeue a possibly-live lane.
		// pid_identity_unavailable means kill(pid,0) SUCCEEDED (the process is
		// signalable, i.e. alive) but /proc/<pid>/stat was momentarily
		// unreadable — treating that as dead force-expired a LIVE agent's lease
		// (the #145/#147 false-requeue class), and the #198 pre-tx oracle would
		// cache the transient failure for the whole sweep window.
		return false
	}
	return !live.Alive
}

// leaseStaleActive reports whether the job's resolved lease is still 'active'
// (not yet expired) — used to exclude live claimants from the dead-lane requeue
// case even when the owning session looks dead (defensive: a fresh claimant
// would have a brand-new active lease).
func leaseStaleActive(row map[string]any) bool {
	return fmt.Sprint(nullable(row["lease_state"])) == "active"
}

// transportPayloadValue renders a TransportType for a recovery.* event payload
// (RFC 0131 Layer 1). An unknown transport (no supervisor pointer metadata)
// renders as a nil JSON field rather than an empty string, so a row whose
// transport could not be determined carries an explicit null rather than a
// misleading "".
func transportPayloadValue(transport sessionliveness.TransportType) any {
	if transport == sessionliveness.TransportUnknown {
		return nil
	}
	return string(transport)
}

func sessionStateLabel(sessionID, sessionState string) string {
	if sessionID == "" || sessionID == "<nil>" {
		return "absent"
	}
	if sessionState == "" || sessionState == "<nil>" {
		return "unknown"
	}
	return sessionState
}

// isNoRows reports a pgx no-rows error.
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// isUndefinedColumn reports a PostgreSQL undefined_column error (SQLSTATE 42703).
// The RFC 0131 131-C confidence-gate columns (migration 0035) read path uses this
// to fall back to the legacy column set on a deployment behind on 0035, so the
// gate degrades safely to today's ungated escalation rather than erroring the
// whole recovery sweep.
func isUndefinedColumn(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42703"
	}
	return false
}
