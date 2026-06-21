package mutations

import (
	"context"
	"fmt"
)

// revisionCycle is the runtime view of a workflow `cycles[]` entry that the
// engine consults when a review job records a `needs_revision` verdict.
// See RFC 0083 §2 (bounded revision cycle) and RFC 0045 for the schema shape.
type revisionCycle struct {
	from          string
	to            string
	onVerdict     string
	maxIterations int
	allowSameLane bool
}

// workflowCyclesForJob loads the run's workflow snapshot and returns the
// declared cycles whose `from` matches the given review job's workflow_job_id.
func workflowCyclesForJob(ctx context.Context, runner any, repositoryID string, job map[string]any) ([]revisionCycle, error) {
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return nil, err
	}
	snapshot, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(run["workflow_snapshot_id"]), false)
	if err != nil {
		return nil, err
	}
	fromID := fmt.Sprint(job["workflow_job_id"])
	cycles := []revisionCycle{}
	for _, item := range asList(asMap(snapshot["workflow_json"])["cycles"]) {
		def := asMap(item)
		if fmt.Sprint(def["from"]) != fromID {
			continue
		}
		cycles = append(cycles, revisionCycle{
			from:          fromID,
			to:            fmt.Sprint(def["to"]),
			onVerdict:     fmt.Sprint(def["on_verdict"]),
			maxIterations: intValue(def["max_iterations"]),
			allowSameLane: boolValue(def["allow_same_lane"]),
		})
	}
	return cycles, nil
}

// matchRevisionCycle returns the cycle whose `from` is the review job and whose
// `on_verdict` matches the recorded verdict. An absent/empty on_verdict defaults
// to "needs_revision" per RFC 0083 (the only verdict that drives a revision
// cycle today).
func matchRevisionCycle(cycles []revisionCycle, verdict string) (revisionCycle, bool) {
	for _, cycle := range cycles {
		on := cycle.onVerdict
		if on == "" {
			on = "needs_revision"
		}
		if on == verdict {
			return cycle, true
		}
	}
	return revisionCycle{}, false
}

// countRevisionRoutings returns how many times this cycle (review job ->
// target) has already been fired for the run, so max_iterations can be
// enforced. It counts emitted revision.cycle_routed events for the review job's
// workflow_job_id, which is monotonic and crash-safe (events are append-only).
func countRevisionRoutings(ctx context.Context, runner any, repositoryID string, job map[string]any, cycle revisionCycle) (int, error) {
	row, err := oneRow(ctx, runner, `
		SELECT count(*) AS n
		  FROM striatumd.events
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND event_type = 'revision.cycle_routed'
		   AND payload_json->>'from_workflow_job_id' = $3
		   AND payload_json->>'to_workflow_job_id' = $4`,
		repositoryID, fmt.Sprint(job["run_id"]), cycle.from, cycle.to)
	if err != nil {
		return 0, err
	}
	return intValue(row["n"]), nil
}

// routeRevisionCycle handles a `needs_revision` verdict that matches a declared
// workflow cycle: it completes the review job, then re-opens the cycle target
// job for revision. Returns false (and no error) when the cycle's iteration
// budget is exhausted or the target job is absent, so the caller can fall back
// to a human checkpoint.
func routeRevisionCycle(
	ctx context.Context,
	runner any,
	repositoryID string,
	job map[string]any,
	sessionID, leaseID string,
	cycle revisionCycle,
	verdict string,
) (map[string]any, bool, error) {
	priorRoutings, err := countRevisionRoutings(ctx, runner, repositoryID, job, cycle)
	if err != nil {
		return nil, false, err
	}
	// max_iterations bounds how many revision re-works the cycle may drive.
	// Enforce it only when a positive budget is declared; once exhausted the
	// caller opens a human checkpoint instead of looping forever.
	if cycle.maxIterations > 0 && priorRoutings >= cycle.maxIterations {
		return nil, false, nil
	}

	target, err := latestJobForWorkflowID(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), cycle.to)
	if err != nil {
		return nil, false, err
	}
	if target == nil {
		// The declared cycle target does not exist in this run's job graph;
		// fall back to a human checkpoint rather than silently dropping it.
		return nil, false, nil
	}

	// Complete the review job (the verdict is recorded; the review is done).
	if err := completeReviewJob(ctx, runner, repositoryID, job, sessionID, leaseID, "needs_revision routed to revision cycle"); err != nil {
		return nil, false, err
	}

	iteration := priorRoutings + 1
	// #476 legibility (RFC 0154 alternative A): record how many SIBLING gating review
	// seats — other reviewers feeding the same downstream gate — were still in flight
	// (non-terminal) at the instant this needs_revision routed to the cycle target.
	// Today's behavior routes on the FIRST blocking dissent regardless of siblings
	// (the debounce that would WAIT for them is the RFC 0154 / D250 opt-in, not yet
	// implemented). Surfacing the count makes the short-circuit visible in
	// `striatum why <run_id>` and the run summary so an operator can apply the
	// "wait for the cohort" best-practice by hand. This is pure observability — it
	// does NOT change the routing decision (gate semantics are unchanged).
	inFlightSiblings, sibErr := inFlightSiblingGatingSeats(ctx, runner, repositoryID, job)
	if sibErr != nil {
		return nil, false, sibErr
	}
	if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "revision.cycle_routed", sessionID, job["job_id"], nil, nil, leaseID, map[string]any{
		"from_workflow_job_id":           cycle.from,
		"to_workflow_job_id":             cycle.to,
		"target_job_id":                  target["job_id"],
		"verdict":                        verdict,
		"iteration":                      iteration,
		"max_iterations":                 cycle.maxIterations,
		"allow_same_lane":                cycle.allowSameLane,
		"in_flight_sibling_gating_seats": inFlightSiblings,
	}); err != nil {
		return nil, false, err
	}

	// Re-open the cycle target for revision. The target may be `completed`
	// (the usual case: an upstream synthesis/implement job that already
	// finished); re-opening it as part of an accepted revision cycle is a
	// legitimate transition (F3). Reset it to a clean state and re-enqueue.
	//
	// If the target is already non-terminal (queued/claimed/running) — e.g. an
	// operator manually retried it concurrently — the verdict and cycle-routed
	// event are still recorded, but the re-open is skipped rather than erroring
	// the whole verdict transaction (Bug B).
	targetState := fmt.Sprint(target["state"])
	if !isTerminalJobState(targetState) {
		if err := maybeCompleteRun(ctx, runner, repositoryID, fmt.Sprint(job["run_id"])); err != nil {
			return nil, false, err
		}
		return map[string]any{
			"status":                         "revision_routed",
			"job_id":                         job["job_id"],
			"verdict":                        verdict,
			"cycle_target_job_id":            target["job_id"],
			"cycle_to":                       cycle.to,
			"cycle_iteration":                iteration,
			"cycle_max_iterations":           cycle.maxIterations,
			"target_message_id":              nil,
			"target_already_open":            true,
			"in_flight_sibling_gating_seats": inFlightSiblings,
		}, true, nil
	}

	// Re-open the cycle target atomically (RFC 0095 Phase 2 / #65 P3): release
	// the prior lease, cancel the prior message, reset the target's transitive
	// downstream terminal jobs back to `blocked` and clear their stale verdicts
	// (so the reviews that reviewed it — and anything further downstream —
	// re-run after the revision per the RFC 0083 review-after-revision
	// contract), bump the attempt, and re-enqueue a fresh message. Consolidating
	// the prior separate reset/reopen/enqueue steps guarantees no dangling lease
	// or message survives to wedge the re-claim.
	messageID, err := reopenJobForAttempt(ctx, runner, repositoryID, target, "revision_cycle")
	if err != nil {
		return nil, false, err
	}

	if err := maybeCompleteRun(ctx, runner, repositoryID, fmt.Sprint(job["run_id"])); err != nil {
		return nil, false, err
	}

	return map[string]any{
		"status":                         "revision_routed",
		"job_id":                         job["job_id"],
		"verdict":                        verdict,
		"cycle_target_job_id":            target["job_id"],
		"cycle_to":                       cycle.to,
		"cycle_iteration":                iteration,
		"cycle_max_iterations":           cycle.maxIterations,
		"target_message_id":              messageID,
		"in_flight_sibling_gating_seats": inFlightSiblings,
	}, true, nil
}

// inFlightSiblingGatingSeats counts how many OTHER gating review seats — reviewers
// feeding the same downstream gate as the routing review job — are still in flight
// (a non-terminal latest-attempt job state) at the moment this review's
// needs_revision routes to its revision cycle (#476 legibility / RFC 0154
// alternative A). It is read-only: it never alters the route. The cohort is the
// frozen gating-seat denominator the accept-path quorum already freezes
// (resolveQuorumDeclaration), so the count is consistent with the downstream gate's
// declared panel. The routing seat itself is excluded. When the review feeds no gate
// with a declared gating panel, the count is 0 (a single-reviewer cycle has no
// siblings to wait on).
func inFlightSiblingGatingSeats(ctx context.Context, runner any, repositoryID string, job map[string]any) (int, error) {
	runID := fmt.Sprint(job["run_id"])
	selfSeat := fmt.Sprint(job["workflow_job_id"])
	gates, err := downstreamJobs(ctx, runner, repositoryID, fmt.Sprint(job["job_id"]))
	if err != nil {
		return 0, err
	}
	// Deduplicate sibling seats across all downstream gates the review feeds; a seat
	// is "in flight" if its latest-attempt job is in a non-terminal state. Count each
	// distinct sibling seat at most once.
	counted := map[string]bool{}
	inFlight := 0
	for _, gate := range gates {
		decl, derr := resolveQuorumDeclaration(ctx, runner, repositoryID, fmt.Sprint(gate["job_id"]))
		if derr != nil {
			return 0, derr
		}
		for _, seatID := range decl.Seats {
			if seatID == selfSeat || counted[seatID] {
				continue
			}
			counted[seatID] = true
			seatJob, jerr := latestJobForWorkflowID(ctx, runner, repositoryID, runID, seatID)
			if jerr != nil {
				return 0, jerr
			}
			if seatJob == nil {
				continue
			}
			if !isTerminalJobState(fmt.Sprint(seatJob["state"])) {
				inFlight++
			}
		}
	}
	return inFlight, nil
}

// isDeclaredCycleTarget reports whether the given job's workflow_job_id is the
// `to` target of any declared revision cycle in the run's workflow. This lets
// the operator retry path (run.retry_job) re-open a completed cycle target for
// a manual revision (F3) without opening retries on arbitrary completed jobs.
func isDeclaredCycleTarget(ctx context.Context, runner any, repositoryID string, job map[string]any) (bool, error) {
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return false, err
	}
	snapshot, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(run["workflow_snapshot_id"]), false)
	if err != nil {
		return false, err
	}
	workflowJobID := fmt.Sprint(job["workflow_job_id"])
	for _, item := range asList(asMap(snapshot["workflow_json"])["cycles"]) {
		def := asMap(item)
		if fmt.Sprint(def["to"]) != workflowJobID {
			continue
		}
		// Only `needs_revision` cycles declare a genuine revision target. A
		// cycle keyed on any other verdict does not unlock the F3 retry path,
		// so the completed job remains non-retriable (Bug C). An absent or
		// empty on_verdict defaults to needs_revision per RFC 0083, matching
		// matchRevisionCycle.
		on := fmt.Sprint(def["on_verdict"])
		if def["on_verdict"] == nil || on == "" {
			on = "needs_revision"
		}
		if on == "needs_revision" {
			return true, nil
		}
	}
	return false, nil
}

// latestJobForWorkflowID returns the highest-attempt job row for a given
// workflow_job_id in a run, or nil when no such job exists.
func latestJobForWorkflowID(ctx context.Context, runner any, repositoryID, runID, workflowJobID string) (map[string]any, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT * FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2 AND workflow_job_id = $3
		 ORDER BY attempt DESC, created_at DESC
		 LIMIT 1
		 FOR UPDATE`, repositoryID, runID, workflowJobID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// reviewedBuildGeneration returns the current review_generation of the
// build/synthesis job this verdict-capable job reviews — its revision-cycle
// target (cycle.to). RFC 0126 P0 (D194): applyVerdict stamps each verdict with
// this value at record time so a later build revision (which bumps the build's
// review_generation) renders the verdict non-current by GENERATION MISMATCH
// rather than by DELETE. A job with no declared needs_revision cycle has no
// build whose revision could invalidate it, so it defaults to generation 1.
// The read is intentionally non-locking: it runs under the per-run advisory
// lock that already serializes the verdict-record and reopen paths, so the
// committed generation it sees is authoritative.
func reviewedBuildGeneration(ctx context.Context, runner any, repositoryID string, job map[string]any) (int, error) {
	cycles, err := workflowCyclesForJob(ctx, runner, repositoryID, job)
	if err != nil {
		return 0, err
	}
	cycle, matched := matchRevisionCycle(cycles, "needs_revision")
	if !matched {
		return 1, nil
	}
	row, err := oneRow(ctx, runner, `
		SELECT review_generation FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2 AND workflow_job_id = $3
		 ORDER BY attempt DESC, created_at DESC
		 LIMIT 1`, repositoryID, fmt.Sprint(job["run_id"]), cycle.to)
	if err != nil {
		if errorsIsNoRows(err) {
			return 1, nil
		}
		return 0, err
	}
	gen := intValue(row["review_generation"])
	if gen < 1 {
		gen = 1
	}
	return gen, nil
}

// isTerminalJobState reports whether a job state is one a revision cycle may
// re-open: a finished state (completed/failed/canceled/skipped/waiting_human)
// or `blocked` (not yet enqueued). Live states (queued/claimed/running) are
// non-terminal and must not be clobbered by an in-flight revision routing.
func isTerminalJobState(state string) bool {
	switch state {
	case "completed", "failed", "canceled", "skipped", "waiting_human", "blocked":
		return true
	default:
		return false
	}
}

// reopenJobForAttempt is the RFC 0095 Phase 2 (#65 P3) single atomic re-open
// helper. Every re-open path (the revision-cycle router, run.retry_job, and
// checkpoint.resolve continue) funnels through it so a re-opened job can never
// be left with a dangling active lease or a stale pending work message that
// would later make `work.claim_next` / `work.await_packet` fail the
// `uq_active_resource_lease` / `uq_active_work_message_per_job` partial unique
// indexes (the #65 P3 wedge: `duplicate active job lease` on re-claim).
//
// In a single transaction it: (a) releases the prior active lease for the job
// (release_reason carries the caller's re-open reason), (b) cancels the prior
// pending/claimed/acked work message, (c) cancels open blockers, (d) resets the
// job's transitive downstream terminal jobs back to `blocked` and clears their
// stale verdicts (the F1 router's existing reset, so the reviews that reviewed
// this job re-run after the revision), (e) clears the job's stale verdicts,
// (f) resets the job to `blocked` and bumps `attempt`, and (g) re-enqueues a
// fresh `pending` work message. It returns the new message id.
//
// The reset is idempotent: re-running it against an already-reset job (no active
// lease, no pending message, attempt already bumped) simply bumps the attempt
// again and re-enqueues, which is the same contract the legacy paths had.
func reopenJobForAttempt(ctx context.Context, runner any, repositoryID string, job map[string]any, reason string) (string, error) {
	if reason == "" {
		reason = "reopened"
	}
	jobID := fmt.Sprint(job["job_id"])
	now := nowString()
	// #65 P1: with the panel-owned interrogation window, the prior attempt's
	// interrogable target session stays live through the whole review panel, so a
	// revision re-open must explicitly retire that superseded session before the
	// fresh attempt spawns a new one. Done first, while the old session is still
	// resolvable via its session.awaiting_interrogation event.
	if err := closeInterrogationTargetForReopen(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), jobID); err != nil {
		return "", err
	}
	// Re-block the transitive downstream terminal jobs BEFORE re-opening the
	// target so the recursive walk still sees the target's current downstream
	// edges. This reuses the F1 router's reset so the reviews (and anything
	// further downstream) re-run after the revision.
	if err := resetDownstreamForRevision(ctx, runner, repositoryID, jobID, now); err != nil {
		return "", err
	}
	if err := resetJobToBlockedWithReason(ctx, runner, repositoryID, jobID, now, reason); err != nil {
		return "", err
	}
	// RFC 0126 P0 (D194 / GH #282): advance the reopened build/synthesis job's
	// review epoch in the SAME transaction as its attempt bump. Verdicts recorded
	// against the prior content were stamped with the prior generation, so this
	// bump renders them non-current by GENERATION MISMATCH (no DELETE) — closing
	// the stale-verdict window the verdict clear used to race. Only the directly
	// reopened target advances; the transitively re-blocked downstream reviewers
	// (resetDownstreamForRevision, above) keep their generation and are stamped
	// with this new build generation when they re-review.
	if err := bumpReviewGeneration(ctx, runner, repositoryID, jobID); err != nil {
		return "", err
	}
	return enqueueJob(ctx, runner, repositoryID, jobID)
}

// bumpReviewGeneration advances a build/synthesis job's monotonic
// review_generation by one (RFC 0126 P0 / D194). It is invoked from
// reopenJobForAttempt so the increment is atomic with the attempt bump: a
// verdict can never be stamped against a half-updated generation. The
// generation is the build's "review epoch" — verdicts stamped with a prior
// generation are non-current without being deleted.
//
// RFC 0135 P5 (D216) — review_generation IS THE SEAL. This is the sealed
// expectation barrier primitive's "advance the entity's seal" operation
// (db.BarrierReadySQL with seal := review_generation): the review obligation is
// the entity, its monotonic review_generation is the seal, and bumping it here
// renders every prior-seal verdict structurally non-current (a generation
// mismatch, never a DELETE) — exactly the primitive's trap-killer where a
// contribution from a superseded seal is structurally absent from the readiness
// JOIN. The primitive does NOT re-key this onto the churning `attempt` (RFC 0126
// rejected that because recovery churns attempt); it ADOPTS the monotonic
// generation as the seal. No behavior changes at P5 — this is the naming of an
// already-shipped mechanism as the canonical seal instance, fenced by
// TestRevisionCoherenceIsTheSealInstance.
func bumpReviewGeneration(ctx context.Context, runner any, repositoryID, jobID string) error {
	// review_generation is owner-bundle DDL (owner bundle 0009): present once the
	// daemon has applied owner bundles, absent in a runtime-migrations-only DB
	// (e.g. the default pgtest harness). When absent the build epoch is a no-op —
	// the column cannot be stamped either, so there is nothing to keep current.
	if !reviewGenerationEnabled(ctx, runner) {
		return nil
	}
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	return exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET review_generation = review_generation + 1
		 WHERE repository_id = $1 AND job_id = $2`, repositoryID, jobID)
}

// reviewGenerationEnabled reports whether the owner-bundle-0009 review_generation
// columns are present on BOTH striatumd.jobs and striatumd.verdicts. The columns
// are owner-table DDL (RFC 0126 P0 / D194), so production has them once owner
// bundles are applied, but a runtime-migrations-only database (the default
// pgtest harness) does not. The verdict write path and the build-epoch bump
// branch on this, mirroring db.ArtifactPlacementColumnPresent for the artifact
// placement column — a single catalog lookup, cheap on the verdict path.
func reviewGenerationEnabled(ctx context.Context, runner any) bool {
	row, err := oneRow(ctx, runner, `
		SELECT (
		  EXISTS (SELECT 1 FROM information_schema.columns
		           WHERE table_schema = 'striatumd' AND table_name = 'jobs'
		             AND column_name = 'review_generation')
		  AND
		  EXISTS (SELECT 1 FROM information_schema.columns
		           WHERE table_schema = 'striatumd' AND table_name = 'verdicts'
		             AND column_name = 'review_generation')
		) AS enabled`)
	return err == nil && boolValue(row["enabled"])
}

// resetJobToBlocked is the shared reset core used both to re-open the cycle
// target and to re-block its transitive downstream jobs. It releases any active
// lease, cancels in-flight work messages and open blockers, clears terminal
// timestamps, and bumps the attempt. RFC 0126 P0 (D194): it no longer clears
// verdicts — history is append-only and staleness is a build-generation
// non-match.
func resetJobToBlocked(ctx context.Context, runner any, repositoryID, jobID, now string) error {
	return resetJobToBlockedWithReason(ctx, runner, repositoryID, jobID, now, "revision_cycle")
}

// resetJobToBlockedWithReason is resetJobToBlocked parameterized by the lease
// release_reason. The reason is purely diagnostic (it lands in
// leases.release_reason); the reset itself is identical regardless of which
// re-open path invoked it.
func resetJobToBlockedWithReason(ctx context.Context, runner any, repositoryID, jobID, now, releaseReason string) error {
	return resetJobToBlockedCore(ctx, runner, repositoryID, jobID, now, releaseReason)
}

// resetJobToBlockedCore is the shared reset body. RFC 0126 P0 (D194 / GH #282):
// it no longer DELETEs the job's verdicts — verdict history is append-only and
// staleness is a build-generation non-match (reopenJobForAttempt bumps the
// build's review_generation). The (repository_id, job_id, session_id) verdict
// uniqueness still bars the same session from re-recording on the fresh attempt,
// and the fresh-review lineage gate (#206) requires a different claimant anyway,
// so preserving the rows neither wedges a re-claim nor lets a stale verdict win
// a latest-row tiebreak. (The RFC 0118 P1-6 invalidate path,
// recovery.invalidate_job, supersedes the rows as a durable receipt before
// calling the shared reset; superseded rows stay invisible to latestVerdict and
// the run-completion gate.)
func resetJobToBlockedCore(ctx context.Context, runner any, repositoryID, jobID, now, releaseReason string) error {
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	if releaseReason == "" {
		releaseReason = "revision_cycle"
	}
	// Release any active lease still pinned to the job. Without this, a later
	// re-claim's INSERT into striatumd.leases (state='active') would violate the
	// uq_active_resource_lease partial unique index and wedge the re-open.
	if err := exec.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'released', released_at = $1, release_reason = $4
		 WHERE repository_id = $2 AND resource_id = $3 AND state = 'active'`,
		now, repositoryID, jobID, releaseReason); err != nil {
		return err
	}
	// Cancel any in-flight work messages so enqueueJob can publish a fresh one
	// (the uq_active_work_message_per_job partial unique index would otherwise
	// reject a new pending message). `blocked` is also cancelled: a job that was
	// parked on a human checkpoint leaves its current message in `blocked`, and a
	// re-open via checkpoint.resolve must retire it rather than orphan it.
	if err := exec.Exec(ctx, `
		UPDATE striatumd.queue_messages
		   SET state = 'canceled', updated_at = $1, current_lease_id = NULL
		 WHERE repository_id = $2 AND job_id = $3
		   AND state IN ('pending','claimed','acked','blocked')`,
		now, repositoryID, jobID); err != nil {
		return err
	}
	// Cancel any open blockers on the job.
	if err := exec.Exec(ctx, `
		UPDATE striatumd.blockers
		   SET state = 'canceled', resolved_at = $1
		 WHERE repository_id = $2 AND job_id = $3 AND resolved_at IS NULL`,
		now, repositoryID, jobID); err != nil {
		return err
	}
	// RFC 0126 P0 (D194): verdict history is NOT cleared here — see the function
	// doc comment. A revision renders prior-round verdicts non-current by
	// generation mismatch, preserving the full per-round audit trail.
	// Reset the job to a clean blocked state and bump the attempt. enqueueJob
	// (or the normal downstream gating) transitions blocked->queued and attaches
	// a fresh message.
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'blocked',
		       started_at = NULL,
		       completed_at = NULL,
		       current_lease_id = NULL,
		       current_message_id = NULL,
		       attempt = attempt + 1
		 WHERE repository_id = $1 AND job_id = $2`,
		repositoryID, jobID); err != nil {
		return err
	}
	return nil
}

// resetDownstreamForRevision walks the job_dependencies graph from the cycle
// target and re-blocks every transitive downstream job that is currently in a
// terminal state (completed/failed/canceled/skipped/waiting_human). Each such
// job — the reviews that reviewed the target, plus anything further downstream
// such as implement/build-review — is returned to `blocked` so the normal
// dependency gating re-enqueues it as the target (and then each predecessor)
// re-completes. Jobs already non-terminal (queued/claimed/running) are left
// alone so an in-flight worker is not clobbered.
func resetDownstreamForRevision(ctx context.Context, runner any, repositoryID, targetJobID, now string) error {
	rows, err := queryRows(ctx, runner, `
		WITH RECURSIVE downstream(job_id) AS (
		    SELECT dep.job_id
		      FROM striatumd.job_dependencies dep
		     WHERE dep.repository_id = $1 AND dep.depends_on_job_id = $2
		  UNION
		    SELECT dep.job_id
		      FROM striatumd.job_dependencies dep
		      JOIN downstream d
		        ON dep.depends_on_job_id = d.job_id
		     WHERE dep.repository_id = $1
		)
		SELECT j.job_id, j.state
		  FROM downstream d
		  JOIN striatumd.jobs j
		    ON j.repository_id = $1 AND j.job_id = d.job_id
		 WHERE j.state IN ('completed','failed','canceled','skipped','waiting_human')
		 ORDER BY j.created_at, j.job_id
		 FOR UPDATE OF j`, repositoryID, targetJobID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := resetJobToBlocked(ctx, runner, repositoryID, fmt.Sprint(row["job_id"]), now); err != nil {
			return err
		}
	}
	return nil
}
