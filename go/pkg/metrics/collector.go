package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/reads"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	"github.com/jackc/pgx/v5"
)

// Querier is the minimal multi-row DB surface the sweep-tick fold needs. It is
// satisfied by *db.PgxRunner — the same concrete type the recovery sweep
// type-asserts to (go/pkg/recovery/sweep.go). It is used ONLY by
// Collector.Refresh, never by the scrape path, which the zero-DB-query test
// enforces by handing the collector a Querier that panics on use and asserting a
// scrape never trips it.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Collector folds and publishes the metrics snapshot from the recovery-sweep
// tick and serves it on /metrics. It holds the daemon runner solely for the
// tick-time fold (Refresh); the scrape path (Handler) reads the published atomic
// pointer and never touches the runner. surrogate maps each repository_id to its
// salted bucket at fold time (Phase D) so a raw repository_id never reaches the
// wire.
type Collector struct {
	runner    Querier
	surrogate *Surrogate
}

// NewCollector builds a collector over the daemon runner. A nil runner yields a
// collector whose Refresh is a no-op error and whose Handler serves an empty but
// valid surface — used by call sites (and tests) that only exercise the scrape
// path. The surrogate defaults to an empty-secret (unsalted-but-deterministic)
// mapping; production wiring uses NewCollectorWithSurrogate with the per-daemon
// authority secret.
func NewCollector(runner Querier) *Collector {
	return &Collector{runner: runner}
}

// NewCollectorWithSurrogate builds a collector that maps repository_ids to salted
// surrogate buckets with the supplied Surrogate (the per-daemon RFC 0110 authority
// secret in production). This is the Phase D constructor the daemon uses so the
// per-repo families and the consent gauge carry an unlinkable bucket rather than a
// raw id.
func NewCollectorWithSurrogate(runner Querier, surrogate *Surrogate) *Collector {
	return &Collector{runner: runner, surrogate: surrogate}
}

// ensureSurrogate lazily installs an empty-secret surrogate if none was supplied,
// so the per-repo fold always produces deterministic distinct buckets even when a
// collector is built without one (tests, pre-authority boot). Called only from the
// serial fold path, so no locking is needed.
func (c *Collector) ensureSurrogate() *Surrogate {
	if c.surrogate == nil {
		c.surrogate = NewSurrogate("")
	}
	return c.surrogate
}

// Refresh folds a fresh snapshot from the daemon DB and publishes it. It runs at
// the recovery-sweep cadence (default 60s), NOT on the scrape path: it issues a
// small fixed number of aggregate queries regardless of run/job count, so it can
// never become the per-scrape self-DoS the RFC warns against. The caller folds
// once per tick and treats any error as non-fatal (metrics are observational —
// the last-good snapshot keeps serving).
//
// The Phase A folds (run-state counts, stranded supervisors) are load-bearing
// and abort the refresh on error. The Phase B folds (the failure-mode families)
// are BEST-EFFORT: a Phase B query error degrades that one family to empty for
// this snapshot rather than blocking the whole surface, so a taxonomy fold bug
// can never take down the Phase A operational gauges.
func (c *Collector) Refresh(ctx context.Context, now time.Time) error {
	if c.runner == nil {
		return fmt.Errorf("metrics fold requires a daemon runner")
	}
	at := now.UTC()
	// Phase D / OQ1 publish-on-errored-tick: a load-bearing Phase A fold failure
	// must not silently keep serving last-good numbers. Republish the carried-
	// forward last-good snapshot stamped tick_status=error so the failed tick is
	// directly visible (snapshot_age keeps climbing, tick_status flips to error).
	rawCounts, err := c.runStateCounts(ctx)
	if err != nil {
		Publish(erroredTickSnapshot(Load()))
		return fmt.Errorf("fold run-state counts: %w", err)
	}
	stranded, err := c.strandedSupervisorCount(ctx)
	if err != nil {
		Publish(erroredTickSnapshot(Load()))
		return fmt.Errorf("fold stranded-supervisor count: %w", err)
	}

	in := SnapshotInput{
		BuiltAt:             at,
		RawRunStateCounts:   rawCounts,
		StrandedSupervisors: stranded,
	}
	// Phase B/D: best-effort folds. An error leaves the corresponding family empty
	// AND degrades the tick to partial, so a single failing taxonomy/per-repo query
	// is visible via tick_status without failing the whole surface.
	partial := false
	var ferr error
	if in.EventCounts, ferr = c.lifecycleEventCounts(ctx); ferr != nil {
		partial = true
	}
	if in.LeaseTransitionCounts, ferr = c.leaseTransitionCounts(ctx); ferr != nil {
		partial = true
	}
	if in.WedgeAges, ferr = c.runWedgeAges(ctx, at); ferr != nil {
		partial = true
	}
	if in.LivenessMargins, ferr = c.livenessMargins(ctx, at); ferr != nil {
		partial = true
	}
	// Phase D: per-repo consent + run-state fold (the consent gauge and the
	// consent-gated Provenance family). Best-effort like the taxonomy folds.
	repoMetrics, rerr := c.repoMetrics(ctx)
	if rerr != nil {
		partial = true
	} else {
		in.RepoMetrics = repoMetrics
	}

	// Phase C: doctor_problems is folded HERE, on the bounded sweep cadence, never
	// on the scrape path. It is best-effort like the Phase B families: a doctor
	// error degrades the gauge to empty for this snapshot rather than failing the
	// whole surface.
	in.DoctorProblemRecords = c.doctorProblemRecords(ctx)

	if partial {
		in.TickStatus = TickPartial
	} else {
		in.TickStatus = TickOK
	}

	Publish(Build(in))
	return nil
}

// repoMetrics folds the Phase D per-repository observations: the active
// repositories with their per-repo consent flag and their run-state counts. The
// repository_id is mapped to its salted surrogate bucket here, so Build (and
// everything downstream of it) sees only the bucket — never the raw id. It is
// best-effort: an error degrades the per-repo families to empty for this tick and
// flips tick_status to partial.
func (c *Collector) repoMetrics(ctx context.Context) ([]RepoMetric, error) {
	surrogate := c.ensureSurrogate()
	consent, order, err := c.repoConsentFlags(ctx)
	if err != nil {
		return nil, err
	}
	runStates, err := c.repoRunStateCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RepoMetric, 0, len(order))
	for _, repoID := range order {
		out = append(out, RepoMetric{
			RepoID:    repoID,
			Bucket:    surrogate.Bucket(repoID),
			Consented: consent[repoID],
			RunStates: runStates[repoID],
		})
	}
	return out, nil
}

// metricsConsentSettingKey is the repositories.settings_json key that records the
// per-repo product-decision consent for Provenance-classified metric families
// (RFC 0137 Phase D deliverable #2). Persisting it in the existing per-repo
// settings column keeps the consent durable in the daemon-owned DB with the
// smallest possible schema footprint (no new table/migration). A repo defaults to
// NO consent (Provenance families default OFF per repo) when the key is absent.
const metricsConsentSettingKey = "metrics_provenance_consent"

// repoConsentFlags reads the active repositories and their per-repo Provenance
// consent flag from striatumd.repositories.settings_json. It returns the consent
// map plus the repositories in a stable id order so the fold is deterministic. It
// selects ONLY repository_id and the single consent setting — no repo path,
// branch, or other provenance column reaches the fold.
func (c *Collector) repoConsentFlags(ctx context.Context) (map[string]bool, []string, error) {
	rows, err := c.runner.Query(ctx, `
		SELECT repository_id,
		       COALESCE(settings_json->>'`+metricsConsentSettingKey+`', '') AS consent
		  FROM striatumd.repositories
		 WHERE state <> 'removed'
		 ORDER BY repository_id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	consent := map[string]bool{}
	order := []string{}
	for rows.Next() {
		var repoID, flag string
		if err := rows.Scan(&repoID, &flag); err != nil {
			return nil, nil, err
		}
		if repoID == "" {
			continue
		}
		consent[repoID] = flag == "true" || flag == "1"
		order = append(order, repoID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return consent, order, nil
}

// repoRunStateCounts aggregates runs by (repository_id, state) for the per-repo
// Provenance run gauge. It selects only the repository_id and the closed-enum
// state — never a repo path, branch, sha, prompt, or byline — so the only
// repo-linkable value is the surrogate bucket the caller derives from
// repository_id. Cardinality is bounded by repos * states, independent of run
// history.
func (c *Collector) repoRunStateCounts(ctx context.Context) (map[string]map[string]int, error) {
	rows, err := c.runner.Query(ctx, `
		SELECT repository_id, state, COUNT(*)::bigint
		  FROM striatumd.runs
		 GROUP BY repository_id, state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]int{}
	for rows.Next() {
		var repoID, state string
		var n int64
		if err := rows.Scan(&repoID, &state, &n); err != nil {
			return nil, err
		}
		if repoID == "" {
			continue
		}
		byState := out[repoID]
		if byState == nil {
			byState = map[string]int{}
			out[repoID] = byState
		}
		byState[state] += int(n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// doctorFoldTimeout bounds the per-repository doctor evaluation folded into a
// sweep tick so a slow check (supervisor probe, skills filesystem scan) can never
// stall the recovery sweep that drives the fold.
const doctorFoldTimeout = 15 * time.Second

// doctorProblemRecords runs the doctor checks per active repository on the sweep
// cadence and returns the aggregated STATIC problem records for the
// doctor_problems gauge. It is best-effort: a missing db.Runner surface, a repo
// enumeration error, or a per-repo doctor error degrades to fewer (or no) records
// rather than failing the snapshot. Only result["problem_records"] is read — the
// dynamic-id `problems` strings are never consulted (F-A8); the `class` derivation
// itself lives in foldDoctorProblemRecords.
func (c *Collector) doctorProblemRecords(ctx context.Context) []map[string]any {
	dr, ok := c.runner.(db.Runner)
	if !ok {
		return nil
	}
	repos, err := c.activeRepositoryIDs(ctx)
	if err != nil {
		return nil
	}
	var records []map[string]any
	for _, repoID := range repos {
		repoCtx, cancel := context.WithTimeout(ctx, doctorFoldTimeout)
		result, derr := reads.HandleDoctor(repoCtx, dr, rpc.Envelope{
			Params: map[string]any{
				"repository_id": repoID,
				// verbose=true is what populates result["problem_records"]; the
				// non-verbose `problems` summary strings are deliberately ignored.
				"verbose": true,
			},
		})
		// Capture the deadline state BEFORE cancel() (which would itself set repoCtx.Err()).
		// foldableDoctorRecords drops a DEGRADED run (hard error, or doctorFoldTimeout
		// exceeded) so a partial, possibly-false result never reaches the gauge.
		ctxErr := repoCtx.Err()
		cancel()
		records = append(records, foldableDoctorRecords(result, derr, ctxErr)...)
	}
	return records
}

// activeRepositoryIDs lists the distinct non-removed repositories the daemon
// serves, so the doctor fold evaluates each one. It selects only the
// repository_id key — no repo path, branch, or other provenance column.
func (c *Collector) activeRepositoryIDs(ctx context.Context) ([]string, error) {
	rows, err := c.runner.Query(ctx, `
		SELECT DISTINCT repository_id
		  FROM striatumd.repositories
		 WHERE state <> 'removed'
		 ORDER BY repository_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var repoID string
		if err := rows.Scan(&repoID); err != nil {
			return nil, err
		}
		if repoID != "" {
			out = append(out, repoID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// runStateCounts aggregates runs by lifecycle state across the daemon-owned DB.
// It selects only the closed-enum state column — never a repo path, branch, sha,
// prompt, or byline — so there is nothing sensitive to leak into a label.
func (c *Collector) runStateCounts(ctx context.Context) (map[string]int, error) {
	rows, err := c.runner.Query(ctx, `
		SELECT state, COUNT(*)::bigint
		  FROM striatumd.runs
		 GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var state string
		var n int64
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		counts[state] = int(n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

// strandedSupervisorCount counts process_supervisors still 'attached' to a
// terminal run — the RFC 0137 #417 phantom-supervisor signal, the exact shape
// the status read path LEFT-JOINs and then probes (see
// go/pkg/db/sql/0033_reap_terminal_run_supervisors.sql).
func (c *Collector) strandedSupervisorCount(ctx context.Context) (int, error) {
	rows, err := c.runner.Query(ctx, `
		SELECT COUNT(*)::bigint
		  FROM striatumd.process_supervisors ps
		  JOIN striatumd.runs r
		    ON r.repository_id = ps.repository_id
		   AND r.run_id = ps.run_id
		 WHERE ps.state = 'attached'
		   AND r.state IN ('completed', 'failed', 'canceled')`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var count int64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return int(count), nil
}

// lifecycleEventCounts folds the apoptosis / necrosis / liveness counters from
// the DURABLE striatumd.events ledger. It selects ONLY the closed-enum
// classification fields (event_type and the stall_class / blocker_kind /
// lifecycle_metric payload tags) — never a run/job/session id, lane, role, path,
// or any free text — and GROUP BYs so it transfers one row per distinct
// classification, not one per event. Folding from the immutable append-only
// ledger is what makes the counters tx-safe (a rolled-back lifecycle transaction
// never wrote the event) and restart-consistent (the counter is re-derived from
// durable history, not reset to zero) — RFC 0137 §"Design guidance".
func (c *Collector) lifecycleEventCounts(ctx context.Context) ([]EventCount, error) {
	// lease.released and supervisor.stopped feed the previously-hollow apoptosis
	// reasons (lease_handoff / supervisor_drained). The lease handoff decision needs
	// the lease.released payload reason/transfer, so they are projected — but ONLY
	// for lease.released (a CASE guards them) so a free-text supervisor.stopped
	// reason never widens the GROUP BY into per-supervisor cardinality. None of
	// these columns reach the wire: render emits only the closed origin/reason enum.
	rows, err := c.runner.Query(ctx, `
		SELECT event_type,
		       COALESCE(payload_json->>'stall_class','')      AS stall_class,
		       COALESCE(payload_json->>'blocker_kind','')     AS blocker_kind,
		       COALESCE(payload_json->>'lifecycle_metric','') AS lifecycle_metric,
		       COALESCE(CASE WHEN event_type = 'lease.released'
		                     THEN payload_json->>'reason' END, '')   AS lease_reason,
		       COALESCE(CASE WHEN event_type = 'lease.released'
		                     THEN payload_json->>'transfer' END, '') AS lease_transfer,
		       COUNT(*)::bigint AS n
		  FROM striatumd.events
		 WHERE event_type IN (
		       'run.completed', 'job.completed', 'session.closed',
		       'session.liveness_deadline_missed', 'session.liveness_recovered',
		       'run.escalated', 'recovery.job_quarantined',
		       'lease.released', 'supervisor.stopped')
		 GROUP BY 1, 2, 3, 4, 5, 6`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventCount{}
	for rows.Next() {
		var eventType, stallClass, blockerKind, lifecycleTag, leaseReason, leaseTransfer string
		var n int64
		if err := rows.Scan(&eventType, &stallClass, &blockerKind, &lifecycleTag, &leaseReason, &leaseTransfer, &n); err != nil {
			return nil, err
		}
		out = append(out, EventCount{
			Event: LifecycleEvent{
				EventType:     eventType,
				StallClass:    stallClass,
				BlockerKind:   blockerKind,
				LifecycleTag:  lifecycleTag,
				LeaseReason:   leaseReason,
				LeaseTransfer: leaseTransfer == "true",
			},
			Count: int(n),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// leaseTransitionCounts folds the lease_transitions counter from the durable
// lease.released / lease.expired events. Leases transition out of 'active', so
// from is fixed to active and the (to, reason) pair is derived by
// leaseTransitionTarget; the raw reason is then bucketed to the closed category
// enum by the fold. Only the bucketed reason reaches the wire.
//
// job_state is projected ONLY for lease.expired (a CASE guards it) so the fold can
// distinguish a repo-write stale-lease expiry (job parked in stale_lease — the RFC
// 0137 primary stale-lease storm signal) from an ordinary expiry; without it the
// stale signal collapsed into to="expired" and was unobservable (prior-review F1).
// The CASE keeps the closed-enum job_state off the lease.released rows so it never
// widens cardinality there, and job_state is itself a closed lifecycle-state enum,
// never an id.
func (c *Collector) leaseTransitionCounts(ctx context.Context) ([]LeaseTransitionCount, error) {
	rows, err := c.runner.Query(ctx, `
		SELECT event_type,
		       COALESCE(payload_json->>'reason','') AS reason,
		       COALESCE(CASE WHEN event_type = 'lease.expired'
		                     THEN payload_json->>'job_state' END, '') AS job_state,
		       COUNT(*)::bigint AS n
		  FROM striatumd.events
		 WHERE event_type IN ('lease.released', 'lease.expired')
		 GROUP BY 1, 2, 3`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LeaseTransitionCount{}
	for rows.Next() {
		var eventType, reason, jobState string
		var n int64
		if err := rows.Scan(&eventType, &reason, &jobState, &n); err != nil {
			return nil, err
		}
		to, transitionReason := leaseTransitionTarget(eventType, reason, jobState)
		out = append(out, LeaseTransitionCount{
			Transition: LeaseTransition{From: "active", To: to, Reason: transitionReason},
			Count:      int(n),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// nonTerminalRunStates is the set of run states that can be "wedged" — a run that
// has not advanced a job state in a while. Terminal runs (completed/failed/
// canceled/compromised) cannot wedge, so they are excluded. It is a []string so
// pgx encodes it as text[] for the ANY($) filter (the codebase idiom; a []any
// does not encode to a Postgres array).
var nonTerminalRunStates = []string{"running", "ready", "blocked", "needs_operator", "needs_branch_confirmation"}

// jobStateAdvanceEventTypes is the CLOSED set of durable event types that mark a
// job changing lifecycle state — the only evidence of forward progress the
// wedge-age signal is allowed to reset on. Run-scoped or keepalive events that
// keep arriving while jobs stay stuck are DELIBERATELY excluded: most importantly
// daemon.recovery_sweep, which the recovery sweep records on the run every tick,
// and lease.heartbeat. Counting any event (the prior behavior) let those make a
// wedged run look freshly active, so striatum_run_wedge_age_seconds stayed low
// exactly when it should grow (prior-review F1). A new job event type must be
// added here deliberately; isJobStateAdvanceEvent and the SQL ANY($) filter share
// this single source of truth.
var jobStateAdvanceEventTypes = []string{
	"job.created",
	"job.queued",
	"job.completed",
	"job.failed",
	"job.blocked",
	"job.canceled",
	"job.retried",
	"job.auto_finalized",
	"job.commits_anchored",
	"job.source_changes_published",
	"job.in_scope_paths_stranded",
}

// isJobStateAdvanceEvent reports whether an event type counts as a job-state
// advance for the wedge-age signal. It is the in-process mirror of the SQL filter
// and the regression guard (a non-job event such as daemon.recovery_sweep must
// return false).
func isJobStateAdvanceEvent(eventType string) bool {
	for _, t := range jobStateAdvanceEventTypes {
		if t == eventType {
			return true
		}
	}
	return false
}

// runWedgeAges observes, per non-terminal run, the seconds since that run last
// advanced a JOB state — the wedge-age signal. The age is measured from the most
// recent job-state-advance event only (jobStateAdvanceEventTypes); run-scoped and
// keepalive events are excluded so a wedged run's age keeps growing instead of
// being reset by the next sweep tick (prior-review F1). Origin is daemon-core
// (runs are the daemon's own aggregate). It is bounded by the number of
// non-terminal runs, never the event history.
func (c *Collector) runWedgeAges(ctx context.Context, now time.Time) ([]WedgeObservation, error) {
	rows, err := c.runner.Query(ctx, `
		SELECT EXTRACT(EPOCH FROM ($1::timestamptz - MAX(e.created_at)))::float8 AS wedge_seconds
		  FROM striatumd.runs r
		  JOIN striatumd.events e
		    ON e.repository_id = r.repository_id AND e.run_id = r.run_id
		 WHERE r.state = ANY($2)
		   AND e.event_type = ANY($3)
		 GROUP BY r.repository_id, r.run_id`, now, nonTerminalRunStates, jobStateAdvanceEventTypes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WedgeObservation{}
	for rows.Next() {
		var ageSeconds float64
		if err := rows.Scan(&ageSeconds); err != nil {
			return nil, err
		}
		if ageSeconds < 0 {
			ageSeconds = 0
		}
		out = append(out, WedgeObservation{Origin: OriginDaemonCore, AgeSeconds: ageSeconds})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// livenessMargins observes, per active lane session holding an active lease, the
// seconds of margin to the lease-heartbeat deadline — the operationally central
// liveness deadline for a working lane. Margin = (LeaseHeartbeatSeconds +
// LeaseHeartbeatSlack) - elapsed-since-last-heartbeat; it goes negative once the
// deadline has elapsed (a reversible pre-death state, F-A6). The deadline is read
// from sessionliveness.DefaultPolicy so the margin stays anchored to the real
// liveness policy rather than a hardcoded constant. Origin is lane.
func (c *Collector) livenessMargins(ctx context.Context, now time.Time) ([]MarginObservation, error) {
	policy := sessionliveness.DefaultPolicy()
	deadline := float64(policy.LeaseHeartbeatSeconds + policy.LeaseHeartbeatSlack)
	rows, err := c.runner.Query(ctx, `
		SELECT EXTRACT(EPOCH FROM ($1::timestamptz - GREATEST(
		         s.last_work_heartbeat_at, al.last_heartbeat_at, al.acquired_at)))::float8 AS elapsed
		  FROM striatumd.sessions s
		  JOIN LATERAL (
		    SELECT al.last_heartbeat_at, al.acquired_at
		      FROM striatumd.leases al
		     WHERE al.repository_id = s.repository_id
		       AND al.owner_session_id = s.session_id
		       AND al.state = 'active'
		     ORDER BY al.acquired_at DESC
		     LIMIT 1
		  ) al ON true
		 WHERE s.state = 'active'
		   AND GREATEST(s.last_work_heartbeat_at, al.last_heartbeat_at, al.acquired_at) IS NOT NULL`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MarginObservation{}
	for rows.Next() {
		var elapsed float64
		if err := rows.Scan(&elapsed); err != nil {
			return nil, err
		}
		out = append(out, MarginObservation{Origin: OriginLane, MarginSeconds: deadline - elapsed})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Handler returns the /metrics scrape handler. It does exactly Load -> render ->
// write: no PG round-trip, no shared mutex. It is a method on Collector so the
// daemon mounts the exporter from the very collector that owns the runner and
// folds the snapshot — yet the body provably reads only the published atomic
// pointer and never c.runner. The zero-DB-query test pins that boundary by
// building this handler from a collector whose runner panics on use and
// asserting a scrape never trips it. The surface is therefore served from the
// http.Server's own goroutines, lock-domain-disjoint from the
// reconcile/recovery/status mutators.
func (c *Collector) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := Load()
		if snap == nil {
			// Before the first fold, serve a valid empty surface (age 0).
			snap = &Snapshot{}
		}
		w.Header().Set("Content-Type", scrapeContentType)
		_ = snap.WriteText(w, time.Now().UTC())
	})
}
