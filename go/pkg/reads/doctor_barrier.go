package reads

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// RFC 0135 P3 (D216) — the generalized barrier doctor invariant + the
// BARRIER_BLOCKED named condition + blocked_manifest (#347).
//
// P0/P1/P2 minted the sealed expectation barrier primitive (the entity/seal-generic
// readiness predicate db.BarrierReadySQL), the first live fan-in instance (freeze +
// attempt-addressed staging tables, migration 0029), and the recoverable
// barrier_assembly job (the barrier_state journal, migration 0030). P3 closes the
// loop with a DOCTOR invariant that detects barrier integrity problems and a
// `striatum join verify` verb (reads.HandleJoinVerify) that audits one barrier.
//
// This doctor block reads the runtime barrier tables through the migration-0031
// striatumd.barrier_status view and the durable rows, classifying four integrity
// problems plus the BARRIER_BLOCKED condition. It follows the existing doctor
// problem-classifier shape (problem code + message + verbose problem_records) used
// by doctorWorktreeRefSafety / doctorArtifactAnchorIntegrity, and it SUBSUMES the
// per-integration `fanin_sibling_unintegrated` warning: that warning fires on a
// completed sibling worktree HEAD reachable only via a refs/striatum pin, which is
// exactly the stranded-author case a never-fired / blocked barrier surfaces here
// at the barrier level. The worktree-ref warning stays as the per-worktree view;
// this barrier block is the barrier-level invariant the worktree warning composes
// into.
//
// The integrity problems (ok-reddening), keyed on the stable barrier_id:
//
//   - barrier_assembling_target_unreachable: an 'assembling' barrier whose journaled
//     target_commit_sha is not a reachable git commit in the run's repo. The
//     two-phase journal wrote an intent the git store cannot honor — a corruption /
//     base-loss signal (the assembler must be recovered, never silently retried).
//   - barrier_committed_manifest_mismatch: a 'committed' barrier whose journaled
//     assembly tree disagrees with the staged refs at the live seal — the durable
//     commit and the contributions it claims to have folded no longer agree (a
//     tampered ref / out-of-band re-point after commit).
//   - barrier_orphaned_staging_ref: a staged contribution row whose barrier has NO
//     freeze record — a staging write that escaped its barrier (a freeze record was
//     never written, or was meant to be but the staging happened first). The
//     contribution can never be assembled; it is orphaned debris.
//   - barrier_blocked: a barrier with a live BLOCKING in-edge (a blocked /
//     waiting_human / failed seat, or an open blocking/human_checkpoint blocker on a
//     seat) that cannot fire without intervention — NOT a clean terminal gap. This
//     is the named BARRIER_BLOCKED condition; the blocked_manifest enumerates the
//     blocking in-edges (which seat, why) in the verbose problem record.
//
// A sealed-but-not-yet-assembled barrier in a healthy in-flight run is NOT a
// problem — sealing is normal and the assembly job runs asynchronously; only a
// genuine integrity gap (an unreachable journaled target, a committed mismatch, an
// orphan, or a hard block) reddens the doctor. This keeps the green baseline green
// (the fires-on-bad / quiet-on-good discipline of D204/D205).

// barrierStatusRow is one row of the migration-0031 striatumd.barrier_status view.
type barrierStatusRow struct {
	BarrierID               string
	RunID                   string
	DownstreamWorkflowJobID string
	FrozenTipSHA            string
	DeclaredSeats           int
	StagedSeats             int
	TerminalGapSeats        int
	BlockingSeats           int
	OutstandingSeats        int
	AssemblyState           string
	AssemblyTargetCommitSHA string
	AssemblyTreeSHA         string
	AssemblyFailReason      string
	Condition               string
}

// blockedInEdge is one blocking in-edge of a BARRIER_BLOCKED barrier: which seat,
// and why it blocks (the seat's job state / open blocker). It is the row shape of
// the `blocked_manifest` the doctor record and `striatum join verify` surface.
type blockedInEdge struct {
	WorkflowJobID string `json:"workflow_job_id"`
	JobState      string `json:"job_state,omitempty"`
	Reason        string `json:"reason"`
	BlockerKind   string `json:"blocker_kind,omitempty"`
}

// doctorBarrierIntegrity is the barrier doctor invariant. It returns the
// barrier_integrity doctor block, the ok-reddening problems, their verbose records,
// the (currently unused) warnings/warning-records — mirroring the shape of
// doctorWorktreeRefSafety so HandleDoctor can fan it in uniformly.
func doctorBarrierIntegrity(ctx context.Context, runner any, repositoryID string) (map[string]any, []string, []map[string]any, []string, []map[string]any) {
	block := map[string]any{
		"checked":  false,
		"barriers": []map[string]any{},
	}
	if repositoryID == "" {
		return block, nil, nil, nil, nil
	}

	// The view is created by migration 0031; a daemon BEHIND on migrations (the view
	// absent) must not crash the whole doctor pass. Probe the view, and if the query
	// errors (e.g. relation does not exist), record it and skip — the rest of doctor
	// stays green.
	rows, err := collectRows(ctx, runner, `
		SELECT barrier_id, run_id, downstream_workflow_job_id, frozen_tip_sha,
		       declared_seats, staged_seats, terminal_gap_seats, blocking_seats,
		       outstanding_seats,
		       COALESCE(assembly_state, '')              AS assembly_state,
		       COALESCE(assembly_target_commit_sha, '')  AS assembly_target_commit_sha,
		       COALESCE(assembly_tree_sha, '')           AS assembly_tree_sha,
		       COALESCE(assembly_fail_reason, '')        AS assembly_fail_reason,
		       condition
		  FROM striatumd.barrier_status
		 WHERE repository_id = $1
		 ORDER BY barrier_id`,
		repositoryID,
	)
	if err != nil {
		block["error"] = err.Error()
		return block, nil, nil, nil, nil
	}
	block["checked"] = true

	repoRoot := doctorRepoRoot(ctx, runner, repositoryID)

	problems := []string{}
	records := []map[string]any{}
	barrierViews := make([]map[string]any, 0, len(rows))

	for _, raw := range rows {
		row := barrierStatusRow{
			BarrierID:               stringFrom(raw, "barrier_id"),
			RunID:                   stringFrom(raw, "run_id"),
			DownstreamWorkflowJobID: stringFrom(raw, "downstream_workflow_job_id"),
			FrozenTipSHA:            stringFrom(raw, "frozen_tip_sha"),
			DeclaredSeats:           intFromAny(raw["declared_seats"]),
			StagedSeats:             intFromAny(raw["staged_seats"]),
			TerminalGapSeats:        intFromAny(raw["terminal_gap_seats"]),
			BlockingSeats:           intFromAny(raw["blocking_seats"]),
			OutstandingSeats:        intFromAny(raw["outstanding_seats"]),
			AssemblyState:           stringFrom(raw, "assembly_state"),
			AssemblyTargetCommitSHA: stringFrom(raw, "assembly_target_commit_sha"),
			AssemblyTreeSHA:         stringFrom(raw, "assembly_tree_sha"),
			AssemblyFailReason:      stringFrom(raw, "assembly_fail_reason"),
			Condition:               stringFrom(raw, "condition"),
		}

		view := map[string]any{
			"barrier_id":     row.BarrierID,
			"run_id":         row.RunID,
			"condition":      row.Condition,
			"declared_seats": row.DeclaredSeats,
			"staged_seats":   row.StagedSeats,
			"blocking_seats": row.BlockingSeats,
			"assembly_state": row.AssemblyState,
		}

		// BARRIER_BLOCKED: a live blocking in-edge that cannot fire without
		// intervention. Surface it loudly with a blocked_manifest of the offending
		// seats.
		if row.BlockingSeats > 0 || row.Condition == barrierConditionBlocked {
			manifest := loadBlockedManifest(ctx, runner, repositoryID, row.RunID, row.BarrierID)
			problems = append(problems, fmt.Sprintf(
				"barrier_blocked.%s: barrier has %d live blocking in-edge(s) that cannot fire without intervention (BARRIER_BLOCKED); resolve the blocked seat(s) or recover the run",
				row.BarrierID, row.BlockingSeats,
			))
			records = append(records, barrierProblemRecord("barrier_blocked", row, map[string]any{
				"blocked_manifest": manifestToAny(manifest),
			}))
		}

		switch row.AssemblyState {
		case "assembling":
			// The journaled target must be a reachable git commit. An unreachable
			// journaled intent is a corruption / base-loss signal the assembler must be
			// recovered from, never silently retried.
			if row.AssemblyTargetCommitSHA != "" && repoRoot != "" &&
				!gitCommitExists(ctx, repoRoot, row.AssemblyTargetCommitSHA) {
				problems = append(problems, fmt.Sprintf(
					"barrier_assembling_target_unreachable.%s: barrier_state journaled assembly target %s is not a reachable commit in the run repo; the two-phase journal intent cannot be honored — recover the assembly (do not retry blindly)",
					row.BarrierID, row.AssemblyTargetCommitSHA,
				))
				records = append(records, barrierProblemRecord("barrier_assembling_target_unreachable", row, map[string]any{
					"journaled_target": row.AssemblyTargetCommitSHA,
				}))
			}
		case "committed":
			// A committed barrier's journaled tree must agree with the staged refs at
			// the live seal. The staged-seat count must still cover every non-gap
			// declared seat; if a staged ref was tampered/voided out from under a
			// committed barrier, the manifest no longer matches the commit it claims.
			expectedStaged := row.DeclaredSeats - row.TerminalGapSeats
			if expectedStaged < 0 {
				expectedStaged = 0
			}
			if row.StagedSeats < expectedStaged {
				problems = append(problems, fmt.Sprintf(
					"barrier_committed_manifest_mismatch.%s: committed barrier folded %d staged seat(s) but only %d of %d non-gap declared seat(s) are still staged at the live seal; the durable commit disagrees with its contributions (tampered/voided staged ref)",
					row.BarrierID, expectedStaged, row.StagedSeats, expectedStaged,
				))
				records = append(records, barrierProblemRecord("barrier_committed_manifest_mismatch", row, map[string]any{
					"expected_staged": expectedStaged,
					"actual_staged":   row.StagedSeats,
					"assembly_tree":   row.AssemblyTreeSHA,
				}))
			}
		}

		barrierViews = append(barrierViews, view)
	}

	// Orphaned staging refs: a live staged contribution whose barrier has NO freeze
	// record. This is a separate scan over the staging table (the view only projects
	// barriers that have a freeze record), so an orphan that the view cannot see is
	// still caught. We select the orphan staging ROWS (no COUNT(*) aggregate — the
	// RFC 0135 anti-ref-count guard forbids a COUNT-of-staged-refs shape anywhere in
	// a barrier file) and tally per barrier in Go.
	orphanCounts, orphanRuns := loadOrphanedStagingBarriers(ctx, runner, repositoryID)
	orphanIDs := make([]string, 0, len(orphanCounts))
	for barrierID := range orphanCounts {
		orphanIDs = append(orphanIDs, barrierID)
	}
	sort.Strings(orphanIDs)
	for _, barrierID := range orphanIDs {
		stagedRows := orphanCounts[barrierID]
		runID := orphanRuns[barrierID]
		problems = append(problems, fmt.Sprintf(
			"barrier_orphaned_staging_ref.%s: %d staged contribution(s) reference a barrier with no freeze record; the contribution can never be assembled (orphaned staging debris)",
			barrierID, stagedRows,
		))
		records = append(records, map[string]any{
			"check":   "barrier_orphaned_staging_ref",
			"id":      barrierID,
			"context": map[string]any{"barrier_id": barrierID, "run_id": runID, "staged_rows": stagedRows},
		})
	}

	// RFC 0138 (#453) Option A — the FMA-003 case made legible. A strict fan-in
	// barrier blocked on a PROVABLY-DEAD required seat that does NOT tolerate a sealed
	// gap has no automatic exit and parks the run in needs_operator; surface it as a
	// SPECIFIC reason (not a generic block) naming the seat, the barrier, and the
	// bounded recovery commands, so the operator sees "this is the dead-required-seat
	// case" immediately. This scan is separate from the BARRIER_BLOCKED case above: a
	// provably-dead-but-still-`running` outstanding seat is `outstanding`, not
	// `blocking`, in the view's narrow classification (a slow `running` seat is merely
	// pending), so without this scan it would surface as nothing at all.
	unrecoverable := loadStrictFaninUnrecoverableBarriers(ctx, runner, repositoryID)
	for _, u := range unrecoverable {
		problems = append(problems, fmt.Sprintf(
			"strict_fanin_required_seat_unrecoverable.%s: strict fan-in barrier has %d provably-dead REQUIRED seat(s) (%s) with no automatic exit; the run parks in needs_operator. The barrier does not tolerate a sealed gap (RFC 0138 opt-in off). Recover via: %s",
			u.BarrierID, len(u.Seats), strings.Join(u.Seats, ", "), strings.Join(u.RecoveryCommands, "; "),
		))
		records = append(records, map[string]any{
			"check": "strict_fanin_required_seat_unrecoverable",
			"id":    u.BarrierID,
			"context": map[string]any{
				"barrier_id":          u.BarrierID,
				"run_id":              u.RunID,
				"unrecoverable_seats": u.Seats,
				"recovery_commands":   u.RecoveryCommands,
				"fma":                 "FMA-003",
			},
		})
	}

	block["barriers"] = barrierViews
	return block, problems, records, nil, nil
}

// strictFaninUnrecoverable is one strict fan-in barrier blocked on one or more
// provably-dead REQUIRED seats with no automatic terminal-gap exit (RFC 0138 Option A
// / FMA-003).
type strictFaninUnrecoverable struct {
	BarrierID        string
	RunID            string
	Seats            []string
	RecoveryCommands []string
}

// loadStrictFaninUnrecoverableBarriers finds strict fan-in barriers blocked on a
// provably-dead REQUIRED seat that the barrier does NOT tolerate as a sealed gap
// (RFC 0138 Option A). A seat is reported only when ALL hold:
//
//   - the barrier does NOT opt into sealed-gap tolerance (fanin_tolerates_sealed_gap
//     false / absent / budget 0) — an opted-in barrier auto-degrades (Option B) and is
//     NOT this case;
//   - the seat is OUTSTANDING: it has no completed job (no live seal) and no quarantine
//     marker — it is neither live-staged nor a clean terminal gap;
//   - the seat's owning lane is PROVABLY DEAD: its live job row's owning session's
//     supervisor pointer is in the conclusive-dead state ('lost' / 'stopped'), the SAME
//     durable signal supervisedAgentConfirmedDead treats as conclusive. This stays
//     CONSERVATIVE — a seat whose deadness needs a live PID probe (an 'attached' /
//     'detached' pointer) is NOT reported here, so a merely-slow seat never reddens the
//     doctor (fires-on-bad / quiet-on-good).
//
// It reads the freeze record's sealed-gap columns adaptively: a deployment behind on
// migration 0039 lacks them, so the opt-in is treated as off (the FMA-003 case is then
// reportable for every dead required seat, which is correct for that deployment).
func loadStrictFaninUnrecoverableBarriers(ctx context.Context, runner any, repositoryID string) []strictFaninUnrecoverable {
	// The sealed-gap tolerance columns may be absent on a deployment behind on 0039;
	// project them adaptively so this read never errors on an older schema.
	tolerates := "false"
	budget := "0"
	if faninFreezeSealedGapColumnsPresent(ctx, runner) {
		tolerates = "COALESCE(fp.fanin_tolerates_sealed_gap, false)"
		budget = "COALESCE(fp.max_sealed_gaps, 0)"
	}
	rows, err := collectRows(ctx, runner, `
		WITH declared AS (
		    SELECT fp.repository_id, fp.barrier_id, fp.run_id,
		           sib.value::text AS workflow_job_id,
		           `+tolerates+` AS tolerates_gap,
		           `+budget+`    AS max_gaps
		      FROM striatumd.fanin_freeze_points fp,
		           jsonb_array_elements_text(fp.declared_sibling_job_ids) AS sib(value)
		     WHERE fp.repository_id = $1
		)
		SELECT d.barrier_id, d.run_id, d.workflow_job_id
		  FROM declared d
		 WHERE (d.tolerates_gap = false OR d.max_gaps <= 0)
		   -- OUTSTANDING: no completed job (no live seal).
		   AND NOT EXISTS (
		     SELECT 1 FROM striatumd.jobs j
		      WHERE j.repository_id = d.repository_id AND j.run_id = d.run_id
		        AND j.workflow_job_id = d.workflow_job_id AND j.state = 'completed'
		   )
		   -- NOT a quarantine terminal gap.
		   AND NOT EXISTS (
		     SELECT 1 FROM striatumd.jobs j
		      WHERE j.repository_id = d.repository_id AND j.run_id = d.run_id
		        AND j.workflow_job_id = d.workflow_job_id
		        AND j.state = 'canceled'
		        AND COALESCE(j.write_scope_json->>'quarantined','') = 'true'
		   )
		   -- PROVABLY DEAD: the seat's live job row's owning session pointer is
		   -- conclusively dead ('lost'/'stopped'). A merely-slow ('attached'/'detached')
		   -- or unprobeable seat is NOT reported (conservative — quiet-on-good).
		   AND EXISTS (
		     SELECT 1
		       FROM striatumd.jobs j
		       JOIN striatumd.leases l
		         ON l.repository_id = j.repository_id AND l.lease_id = j.current_lease_id
		       JOIN striatumd.process_supervisor_pointers sp
		         ON sp.repository_id = j.repository_id AND sp.session_id = l.owner_session_id
		      WHERE j.repository_id = d.repository_id AND j.run_id = d.run_id
		        AND j.workflow_job_id = d.workflow_job_id
		        AND sp.state IN ('lost','stopped')
		   )
		 ORDER BY d.barrier_id, d.workflow_job_id`,
		repositoryID,
	)
	if err != nil {
		return nil
	}
	byBarrier := map[string]*strictFaninUnrecoverable{}
	order := []string{}
	for _, r := range rows {
		barrierID := stringFrom(r, "barrier_id")
		seat := stringFrom(r, "workflow_job_id")
		u, ok := byBarrier[barrierID]
		if !ok {
			u = &strictFaninUnrecoverable{
				BarrierID:        barrierID,
				RunID:            stringFrom(r, "run_id"),
				RecoveryCommands: strictFaninUnrecoverableRecoveryCommands(stringFrom(r, "run_id")),
			}
			byBarrier[barrierID] = u
			order = append(order, barrierID)
		}
		u.Seats = append(u.Seats, seat)
	}
	out := make([]strictFaninUnrecoverable, 0, len(order))
	for _, barrierID := range order {
		out = append(out, *byBarrier[barrierID])
	}
	return out
}

// strictFaninUnrecoverableRecoveryCommands returns the bounded operator recovery
// commands for a strict fan-in stuck on a dead required seat (RFC 0138 Option A): the
// run must be canceled (a strict fan-in's required seat is required by design), or the
// author may re-prepare the join with an opt-in sealed-gap tolerance if a degraded
// join is acceptable. Note: recovery.quarantine_lane requires the run terminal first
// (it snapshots a canceled/failed run's worktree), so the bounded path is cancel —
// then optionally quarantine the seat — or opt into a sealed gap.
func strictFaninUnrecoverableRecoveryCommands(runID string) []string {
	return []string{
		fmt.Sprintf("striatum run cancel %s  (a strict fan-in's required seat is required by design; cancel the run)", runID),
		fmt.Sprintf("then optionally: striatum recovery quarantine-lane --run-id %s --job-id <dead-seat-job>  (snapshots the now-canceled run's dead lane worktree)", runID),
		"or re-prepare the join with fanin_tolerates_sealed_gap + max_sealed_gaps if a degraded (short-a-leg) join is acceptable (RFC 0138 Option B)",
	}
}

// faninFreezeSealedGapColumnsPresent reports whether the RFC 0138 (#453) sealed-gap
// tolerance columns (migration 0039) exist on striatumd.fanin_freeze_points. Mirrors
// the mutations-side faninSealedGapColumnsPresent: a deployment behind on 0039 lacks
// them, so the doctor treats the opt-in as off rather than erroring on an older schema.
func faninFreezeSealedGapColumnsPresent(ctx context.Context, runner any) bool {
	rows, err := collectRows(ctx, runner, `
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.columns
		   WHERE table_schema = 'striatumd'
		     AND table_name = 'fanin_freeze_points'
		     AND column_name = 'fanin_tolerates_sealed_gap'
		) AS present`)
	if err != nil || len(rows) == 0 {
		return false
	}
	return boolValue(rows[0]["present"])
}

// loadOrphanedStagingBarriers returns, per barrier_id, how many live staged
// contributions reference a barrier that has NO freeze record (orphaned debris),
// and the run_id of one such row. It deliberately selects the staging ROWS and
// tallies in Go rather than issuing a COUNT(*) aggregate: the RFC 0135 anti-ref-
// count static guard forbids a COUNT-of-staged-refs SQL shape anywhere in a barrier
// file, and an orphan tally is a debris count, not a readiness gate — keeping it in
// Go avoids reintroducing the forbidden shape.
func loadOrphanedStagingBarriers(ctx context.Context, runner any, repositoryID string) (map[string]int, map[string]string) {
	counts := map[string]int{}
	runs := map[string]string{}
	rows, err := collectRows(ctx, runner, `
		SELECT s.barrier_id, s.run_id
		  FROM striatumd.barrier_staged_contributions s
		  LEFT JOIN striatumd.fanin_freeze_points fp
		    ON fp.repository_id = s.repository_id AND fp.barrier_id = s.barrier_id
		 WHERE s.repository_id = $1
		   AND s.status = 'staged'
		   AND fp.barrier_id IS NULL`,
		repositoryID,
	)
	if err != nil {
		return counts, runs
	}
	for _, r := range rows {
		barrierID := stringFrom(r, "barrier_id")
		counts[barrierID]++
		if _, ok := runs[barrierID]; !ok {
			runs[barrierID] = stringFrom(r, "run_id")
		}
	}
	return counts, runs
}

// loadBlockedManifest enumerates the blocking in-edges of a barrier: each declared
// seat whose job is in a blocking state (blocked / waiting_human / failed) or
// carries an open blocking/human_checkpoint blocker. The classification is keyed on
// the stable seat (workflow_job_id), never on job_id (RFC 0135).
func loadBlockedManifest(ctx context.Context, runner any, repositoryID, runID, barrierID string) []blockedInEdge {
	rows, err := collectRows(ctx, runner, `
		WITH declared AS (
		    SELECT sib.value::text AS workflow_job_id
		      FROM striatumd.fanin_freeze_points fp,
		           jsonb_array_elements_text(fp.declared_sibling_job_ids) AS sib(value)
		     WHERE fp.repository_id = $1 AND fp.barrier_id = $2
		), seat_state AS (
		    SELECT d.workflow_job_id,
		           (SELECT j.state
		              FROM striatumd.jobs j
		             WHERE j.repository_id = $1 AND j.run_id = $3
		               AND j.workflow_job_id = d.workflow_job_id
		             ORDER BY j.attempt DESC
		             LIMIT 1) AS job_state
		      FROM declared d
		)
		SELECT ss.workflow_job_id,
		       COALESCE(ss.job_state, '') AS job_state,
		       COALESCE((
		         SELECT b.blocker_kind
		           FROM striatumd.blockers b
		           JOIN striatumd.jobs j
		             ON j.repository_id = b.repository_id AND j.job_id = b.job_id
		          WHERE b.repository_id = $1 AND b.run_id = $3
		            AND j.workflow_job_id = ss.workflow_job_id
		            AND b.state = 'open'
		            AND b.severity IN ('blocked','human_checkpoint')
		          ORDER BY b.created_at DESC
		          LIMIT 1
		       ), '') AS blocker_kind
		  FROM seat_state ss
		 WHERE ss.job_state IN ('blocked','waiting_human','failed')
		    OR EXISTS (
		         SELECT 1 FROM striatumd.blockers b
		           JOIN striatumd.jobs j
		             ON j.repository_id = b.repository_id AND j.job_id = b.job_id
		          WHERE b.repository_id = $1 AND b.run_id = $3
		            AND j.workflow_job_id = ss.workflow_job_id
		            AND b.state = 'open'
		            AND b.severity IN ('blocked','human_checkpoint')
		       )
		 ORDER BY ss.workflow_job_id`,
		repositoryID, barrierID, runID,
	)
	if err != nil {
		return nil
	}
	manifest := make([]blockedInEdge, 0, len(rows))
	for _, r := range rows {
		seat := stringFrom(r, "workflow_job_id")
		jobState := stringFrom(r, "job_state")
		blockerKind := stringFrom(r, "blocker_kind")
		reason := blockedReason(jobState, blockerKind)
		manifest = append(manifest, blockedInEdge{
			WorkflowJobID: seat,
			JobState:      jobState,
			Reason:        reason,
			BlockerKind:   blockerKind,
		})
	}
	sort.Slice(manifest, func(i, j int) bool { return manifest[i].WorkflowJobID < manifest[j].WorkflowJobID })
	return manifest
}

// blockedReason renders a human-legible reason a seat blocks the barrier.
func blockedReason(jobState, blockerKind string) string {
	switch jobState {
	case "blocked":
		if blockerKind != "" {
			return "seat job is blocked (" + blockerKind + ")"
		}
		return "seat job is blocked"
	case "waiting_human":
		return "seat job is waiting on a human checkpoint"
	case "failed":
		return "seat job has failed and was not recovered"
	}
	if blockerKind != "" {
		return "seat has an open blocking blocker (" + blockerKind + ")"
	}
	return "seat is an unresolvable in-edge"
}

// barrierProblemRecord builds a verbose record for a barrier integrity problem,
// mirroring worktreeProblemRecord (check + id + context).
func barrierProblemRecord(check string, row barrierStatusRow, extra map[string]any) map[string]any {
	ctxMap := map[string]any{
		"barrier_id":     row.BarrierID,
		"run_id":         row.RunID,
		"condition":      row.Condition,
		"assembly_state": row.AssemblyState,
		"declared_seats": row.DeclaredSeats,
		"staged_seats":   row.StagedSeats,
		"blocking_seats": row.BlockingSeats,
	}
	for k, v := range extra {
		ctxMap[k] = v
	}
	return map[string]any{
		"check":   check,
		"id":      row.BarrierID,
		"context": ctxMap,
	}
}

// manifestToAny converts the typed blocked_manifest to the []map[string]any shape
// the verbose doctor record / RPC result returns.
func manifestToAny(manifest []blockedInEdge) []map[string]any {
	out := make([]map[string]any, 0, len(manifest))
	for _, e := range manifest {
		out = append(out, map[string]any{
			"workflow_job_id": e.WorkflowJobID,
			"job_state":       e.JobState,
			"reason":          e.Reason,
			"blocker_kind":    e.BlockerKind,
		})
	}
	return out
}

// gitCommitExists reports whether ref resolves to a commit object in repoRoot.
func gitCommitExists(ctx context.Context, repoRoot, ref string) bool {
	if strings.TrimSpace(repoRoot) == "" || strings.TrimSpace(ref) == "" {
		return false
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return cmd.Run() == nil
}

// intFromAny coerces a PG numeric scan result to int.
func intFromAny(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case int32:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
