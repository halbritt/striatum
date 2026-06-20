package reads

import (
	"context"
	"fmt"
	"sort"
)

// RFC 0132 P4b (#342, D214) — the panel-quorum / advisory-dissent doctor invariant
// and the dissent-ledger completeness check folded in from #339.
//
// The RFC 0135 P4 quorum core (mutations/barrier_quorum.go) evaluates the panel
// quorum over a FROZEN declared-seat denominator keyed on the stable workflow_job_id,
// and forward-writes a dissent_ledger row co-transactionally with every blocking
// verdict. This doctor block is the integrity invariant over that machinery: it
// detects the failure modes the RFC names plus the forward-write hole #339 guards,
// following the existing doctor problem-classifier shape (problem code + message +
// verbose problem_records) used by doctorBarrierIntegrity / doctorWorktreeRefSafety.
//
// The checks (all ok-reddening, keyed on the stable gate / seat workflow_job_id):
//
//   - quorum_seat_unresolvable: a downstream gate job depends on a declared review
//     seat (workflow_job_id) for which NO jobs row exists in the run. The seat can
//     never resolve to a live attempt, so the quorum predicate's
//     staged.attempt = live.attempt can never be satisfied for it — a permanent
//     fail-closed deadlock (the RFC's load-bearing risk: a seat not mapped back to a
//     live job). Fires only for a gate that is NOT yet terminal (a completed gate
//     already cleared its denominator).
//   - quorum_denominator_mismatch: the gate's LIVE gating-seat dependency set
//     (job_dependencies → review jobs declared panel_role gating) disagrees with the
//     gate's FROZEN declared denominator in the run's workflow snapshot. The frozen
//     denominator and the live job graph must agree; a mismatch means the denominator
//     the predicate would compute drifted from the snapshot it was frozen at (the
//     TOCTOU / re-derived-denominator hazard the RFC's single load-bearing risk
//     names).
//   - finalize_ignored_advisory_dissent: a COMPLETED gate job whose panel carries a
//     live (current-seal) advisory dissent (needs_revision / reject) that was never
//     surfaced as an advisory-guard blocker — the gate finalized while ignoring an
//     advisory voice (the "never silent" Layer C invariant, checked after the fact).
//   - dissent_ledger_incomplete (#339): a non-accepting, non-superseded verdict
//     (needs_revision / reject) on a review seat at its LIVE attempt that has NO live
//     dissent_ledger row at that seat+attempt. A future code path that records a
//     blocking verdict WITHOUT forward-writing the ledger token is caught here, not
//     silently re-opening the dropped-dissent hole one layer down. Only fires when
//     the dissent_ledger table exists (a daemon behind migration 0032 skips it).
//
// A healthy in-flight run with an unfilled-but-live gating seat is NOT a problem —
// an outstanding seat is normal until it resolves; only a structurally unresolvable
// seat, a frozen/live denominator disagreement, an ignored advisory dissent on a
// finalized gate, or a missing forward-write reddens the doctor. This keeps the
// green baseline green (the fires-on-bad / quiet-on-good discipline of D204/D205).

// quorumGateView is a downstream gate job that carries a frozen quorum denominator
// (one or more review-seat dependencies), resolved for the doctor checks.
type quorumGateView struct {
	GateJobID         string
	GateWorkflowJobID string
	RunID             string
	GateState         string
	// DeclaredSeats are the gate's FROZEN declared review seats (from the run's
	// snapshot edges), each with its panel_role (gating|advisory) and whether it
	// resolves to a live job row.
	DeclaredSeats []quorumSeatView
	// LiveGatingSeats is the gate's LIVE gating review-seat dependency set (from the
	// job_dependencies graph), for the denominator-mismatch comparison.
	LiveGatingSeats []string
}

type quorumSeatView struct {
	WorkflowJobID string
	PanelRole     string
	// HasJobRow is true when at least one jobs row exists for the seat in the run
	// (the seat resolves to a live attempt). False = structurally unresolvable.
	HasJobRow bool
}

// doctorQuorumIntegrity is the panel-quorum / advisory-dissent doctor invariant. It
// returns the quorum_integrity doctor block, the ok-reddening problems, their verbose
// records, and the (unused) warnings/warning-records — mirroring doctorBarrierIntegrity
// so HandleDoctor can fan it in uniformly.
func doctorQuorumIntegrity(ctx context.Context, runner any, repositoryID string) (map[string]any, []string, []map[string]any, []string, []map[string]any) {
	block := map[string]any{
		"checked": false,
		"gates":   []map[string]any{},
	}
	if repositoryID == "" {
		return block, nil, nil, nil, nil
	}

	gates, err := loadQuorumGates(ctx, runner, repositoryID)
	if err != nil {
		block["error"] = err.Error()
		return block, nil, nil, nil, nil
	}
	block["checked"] = true

	problems := []string{}
	records := []map[string]any{}
	gateViews := make([]map[string]any, 0, len(gates))

	for _, gate := range gates {
		gatingSeats := 0
		advisorySeats := 0
		for _, seat := range gate.DeclaredSeats {
			switch seat.PanelRole {
			case "advisory":
				advisorySeats++
			default:
				gatingSeats++
			}
		}
		gateViews = append(gateViews, map[string]any{
			"gate_workflow_job_id": gate.GateWorkflowJobID,
			"run_id":               gate.RunID,
			"gate_state":           gate.GateState,
			"gating_seats":         gatingSeats,
			"advisory_seats":       advisorySeats,
		})

		// quorum_seat_unresolvable: a declared seat with no jobs row, on a non-terminal
		// gate (a completed gate already cleared its denominator).
		if !quorumGateTerminal(gate.GateState) {
			for _, seat := range gate.DeclaredSeats {
				if seat.HasJobRow {
					continue
				}
				problems = append(problems, fmt.Sprintf(
					"quorum_seat_unresolvable.%s: gate %s depends on declared review seat %s which has no job row in run %s; the seat can never resolve to a live attempt (permanent fail-closed quorum deadlock)",
					gate.GateWorkflowJobID, gate.GateWorkflowJobID, seat.WorkflowJobID, gate.RunID,
				))
				records = append(records, quorumProblemRecord("quorum_seat_unresolvable", gate, map[string]any{
					"unresolvable_seat": seat.WorkflowJobID,
					"panel_role":        seat.PanelRole,
				}))
			}
		}
	}

	// quorum_denominator_mismatch: the live gating-seat dependency set disagrees with
	// the frozen declared denominator for the gate. This is a per-gate scan over the
	// snapshot vs the live job_dependencies graph.
	mismatchProblems, mismatchRecords := doctorQuorumDenominatorMismatch(ctx, runner, repositoryID, gates)
	problems = append(problems, mismatchProblems...)
	records = append(records, mismatchRecords...)

	// finalize_ignored_advisory_dissent: a completed gate that finalized while a live
	// advisory dissent existed on one of its advisory seats with no guard blocker.
	ignoredProblems, ignoredRecords := doctorFinalizeIgnoredAdvisoryDissent(ctx, runner, repositoryID, gates)
	problems = append(problems, ignoredProblems...)
	records = append(records, ignoredRecords...)

	// dissent_ledger_incomplete (#339): a non-accepting, non-superseded verdict at a
	// seat's live attempt with no live dissent_ledger row.
	incompleteProblems, incompleteRecords := doctorDissentLedgerCompleteness(ctx, runner, repositoryID)
	problems = append(problems, incompleteProblems...)
	records = append(records, incompleteRecords...)

	block["gates"] = gateViews
	return block, problems, records, nil, nil
}

// quorumGateTerminal reports whether a gate job state is terminal (the denominator
// is already cleared / the gate finalized).
func quorumGateTerminal(state string) bool {
	switch state {
	case "completed", "skipped", "canceled", "quarantined":
		return true
	}
	return false
}

// loadQuorumGates loads every PANEL GATE — a job the run snapshot declares as the
// downstream of one or more review seats (via the snapshot's `on: completed` edges) —
// resolving its FROZEN declared seats (with panel_role + live-job-row presence) and
// its LIVE gating-seat dependency set (from the job_dependencies graph). Gates whose
// snapshot declares no review-seat edge (a phase-materialized or non-panel gate) are
// skipped, so the checks are scoped to explicitly-declared panels where the frozen vs
// live comparison is unambiguous.
func loadQuorumGates(ctx context.Context, runner any, repositoryID string) ([]quorumGateView, error) {
	// Every non-terminal-or-recently-finalized panel gate that has a live job row,
	// with its run + state + live job-graph review-seat dependencies.
	gateRows, err := collectRows(ctx, runner, `
		SELECT g.job_id          AS gate_job_id,
		       g.workflow_job_id AS gate_workflow_job_id,
		       g.run_id          AS run_id,
		       g.state           AS gate_state
		  FROM striatumd.jobs g
		 WHERE g.repository_id = $1
		   AND EXISTS (
		     SELECT 1 FROM striatumd.job_dependencies jd
		       JOIN striatumd.jobs u
		         ON u.repository_id = jd.repository_id AND u.job_id = jd.depends_on_job_id
		      WHERE jd.repository_id = g.repository_id AND jd.job_id = g.job_id
		        AND u.job_type = 'review'
		   )
		 ORDER BY g.run_id, g.workflow_job_id`,
		repositoryID)
	if err != nil {
		return nil, err
	}
	// Cache per-run snapshot derivations (declared seats + roles) so we parse each
	// snapshot once.
	declaredByRun := map[string]map[string][]string{} // gate_wfid -> declared review seats
	rolesByRun := map[string]map[string]string{}      // seat_wfid -> panel_role
	out := make([]quorumGateView, 0, len(gateRows))
	for _, row := range gateRows {
		runID := stringFrom(row, "run_id")
		gateWF := stringFrom(row, "gate_workflow_job_id")
		gateJobID := stringFrom(row, "gate_job_id")
		if _, ok := declaredByRun[runID]; !ok {
			declaredByRun[runID] = snapshotPanelDependencies(ctx, runner, repositoryID, runID)
			rolesByRun[runID] = panelRolesForRun(ctx, runner, repositoryID, runID)
		}
		declaredSeats, ok := declaredByRun[runID][gateWF]
		if !ok || len(declaredSeats) == 0 {
			// No explicitly-declared review-seat edge for this gate in the snapshot;
			// skip (phase-materialized / non-panel gate).
			continue
		}
		gate := quorumGateView{
			GateJobID:         gateJobID,
			GateWorkflowJobID: gateWF,
			RunID:             runID,
			GateState:         stringFrom(row, "gate_state"),
		}
		for _, seatWF := range declaredSeats {
			role := rolesByRun[runID][seatWF]
			if role == "" {
				role = "gating"
			}
			hasJob, err := seatHasJobRow(ctx, runner, repositoryID, runID, seatWF)
			if err != nil {
				return nil, err
			}
			gate.DeclaredSeats = append(gate.DeclaredSeats, quorumSeatView{
				WorkflowJobID: seatWF,
				PanelRole:     role,
				HasJobRow:     hasJob,
			})
		}
		sort.Slice(gate.DeclaredSeats, func(i, j int) bool {
			return gate.DeclaredSeats[i].WorkflowJobID < gate.DeclaredSeats[j].WorkflowJobID
		})
		liveGating, err := liveGatingDependencySeats(ctx, runner, repositoryID, runID, gateJobID, rolesByRun[runID])
		if err != nil {
			return nil, err
		}
		gate.LiveGatingSeats = liveGating
		out = append(out, gate)
	}
	return out, nil
}

// seatHasJobRow reports whether any jobs row exists for a seat workflow_job_id in the
// run (the seat resolves to a live attempt).
func seatHasJobRow(ctx context.Context, runner any, repositoryID, runID, seatWF string) (bool, error) {
	rows, err := collectRows(ctx, runner, `
		SELECT 1 FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2 AND workflow_job_id = $3 LIMIT 1`,
		repositoryID, runID, seatWF)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// liveGatingDependencySeats returns the gate's LIVE gating review-seat dependency set
// (job_dependencies → review jobs declared panel_role gating), sorted/de-duplicated by
// stable workflow_job_id.
func liveGatingDependencySeats(ctx context.Context, runner any, repositoryID, runID, gateJobID string, roles map[string]string) ([]string, error) {
	rows, err := collectRows(ctx, runner, `
		SELECT DISTINCT u.workflow_job_id AS seat
		  FROM striatumd.job_dependencies jd
		  JOIN striatumd.jobs u
		    ON u.repository_id = jd.repository_id AND u.job_id = jd.depends_on_job_id
		 WHERE jd.repository_id = $1 AND jd.job_id = $2 AND u.job_type = 'review'`,
		repositoryID, gateJobID)
	if err != nil {
		return nil, err
	}
	seats := []string{}
	for _, row := range rows {
		seat := stringFrom(row, "seat")
		role := roles[seat]
		if role == "" {
			role = "gating"
		}
		if role == "gating" {
			seats = append(seats, seat)
		}
	}
	sort.Strings(seats)
	return seats, nil
}

// panelRolesForRun parses a run's workflow snapshot and returns each review job's
// declared panel_role keyed by workflow_job_id (gating|advisory; default gating).
func panelRolesForRun(ctx context.Context, runner any, repositoryID, runID string) map[string]string {
	roles := map[string]string{}
	rows, err := collectRows(ctx, runner, `
		SELECT w.workflow_json
		  FROM striatumd.runs r
		  JOIN striatumd.workflow_snapshots w
		    ON w.repository_id = r.repository_id AND w.workflow_snapshot_id = r.workflow_snapshot_id
		 WHERE r.repository_id = $1 AND r.run_id = $2
		 LIMIT 1`, repositoryID, runID)
	if err != nil || len(rows) == 0 {
		return roles
	}
	workflow := objectFromJSONish(rows[0]["workflow_json"])
	for _, def := range jobDefList(workflow["jobs"]) {
		if fmt.Sprint(def["type"]) != "review" {
			continue
		}
		id := fmt.Sprint(def["id"])
		if id == "" {
			continue
		}
		role := "gating"
		if r, ok := def["panel_role"].(string); ok && r == "advisory" {
			role = "advisory"
		}
		roles[id] = role
	}
	return roles
}

// doctorQuorumDenominatorMismatch flags a gate whose LIVE gating-seat dependency set
// (job_dependencies → review jobs panel_role gating) disagrees with the gate's FROZEN
// gating denominator in the run snapshot (the snapshot's review→gate edges, panel_role
// gating). A drift means the denominator the quorum predicate would compute over the
// live graph is not the one the workflow froze (the re-derived-denominator hazard the
// RFC's single load-bearing risk names).
func doctorQuorumDenominatorMismatch(_ context.Context, _ any, _ string, gates []quorumGateView) ([]string, []map[string]any) {
	problems := []string{}
	records := []map[string]any{}
	for _, gate := range gates {
		if quorumGateTerminal(gate.GateState) {
			continue
		}
		// The FROZEN gating denominator is the gating subset of the gate's snapshot-
		// declared seats.
		frozen := []string{}
		for _, seat := range gate.DeclaredSeats {
			if seat.PanelRole != "advisory" {
				frozen = append(frozen, seat.WorkflowJobID)
			}
		}
		sort.Strings(frozen)
		live := append([]string{}, gate.LiveGatingSeats...)
		sort.Strings(live)
		if equalStringSets(live, frozen) {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"quorum_denominator_mismatch.%s: gate %s live gating denominator %v disagrees with the frozen snapshot denominator %v (run %s); the denominator drifted from what the workflow froze",
			gate.GateWorkflowJobID, gate.GateWorkflowJobID, live, frozen, gate.RunID,
		))
		records = append(records, quorumProblemRecord("quorum_denominator_mismatch", gate, map[string]any{
			"live_denominator":   live,
			"frozen_denominator": frozen,
		}))
	}
	return problems, records
}

// snapshotPanelDependencies parses a run snapshot and returns, per gate
// workflow_job_id, ALL review seats (gating + advisory) the snapshot's EXPLICIT
// `edges` block declares as `on: completed` dependencies feeding that gate. The
// snapshot's edges are the FROZEN denominator source the run.prepare job_dependencies
// were built from; the doctor compares the live job graph against them to detect a
// post-prepare denominator drift and an unresolvable declared seat.
//
// Only review→gate edges are recorded, so a workflow that relies on phase-materialized
// synthesis edges (which reads cannot reproduce without the phase index) yields no
// declared panel for that gate and is skipped rather than flagged with a false
// mismatch — the check is scoped to explicitly-declared panel edges.
func snapshotPanelDependencies(ctx context.Context, runner any, repositoryID, runID string) map[string][]string {
	result := map[string][]string{}
	rows, err := collectRows(ctx, runner, `
		SELECT w.workflow_json
		  FROM striatumd.runs r
		  JOIN striatumd.workflow_snapshots w
		    ON w.repository_id = r.repository_id AND w.workflow_snapshot_id = r.workflow_snapshot_id
		 WHERE r.repository_id = $1 AND r.run_id = $2
		 LIMIT 1`, repositoryID, runID)
	if err != nil || len(rows) == 0 {
		return result
	}
	workflow := objectFromJSONish(rows[0]["workflow_json"])
	reviewJobs := map[string]bool{}
	for _, def := range jobDefList(workflow["jobs"]) {
		if fmt.Sprint(def["type"]) == "review" {
			reviewJobs[fmt.Sprint(def["id"])] = true
		}
	}
	seen := map[string]map[string]bool{}
	for _, edgeValue := range jobDefList(workflow["edges"]) {
		fromID := fmt.Sprint(edgeValue["from"])
		toID := fmt.Sprint(edgeValue["to"])
		if fromID == "" || toID == "" {
			continue
		}
		if on, ok := edgeValue["on"]; ok && fmt.Sprint(on) != "completed" {
			continue
		}
		if !reviewJobs[fromID] {
			continue
		}
		if seen[toID] == nil {
			seen[toID] = map[string]bool{}
		}
		if seen[toID][fromID] {
			continue
		}
		seen[toID][fromID] = true
		result[toID] = append(result[toID], fromID)
	}
	for gate := range result {
		sort.Strings(result[gate])
	}
	return result
}

// jobDefList coerces a workflow_json jobs/edges value (which pgx may decode as
// []any of map[string]any) into a []map[string]any.
func jobDefList(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if def, ok := item.(map[string]any); ok {
				out = append(out, def)
			}
		}
		return out
	}
	return nil
}

// doctorFinalizeIgnoredAdvisoryDissent flags a COMPLETED gate whose panel carried a
// live (current-seal) advisory dissent (needs_revision / reject) that was never
// surfaced as an advisory-guard blocker — a finalize that ignored an advisory voice.
func doctorFinalizeIgnoredAdvisoryDissent(ctx context.Context, runner any, repositoryID string, gates []quorumGateView) ([]string, []map[string]any) {
	problems := []string{}
	records := []map[string]any{}
	for _, gate := range gates {
		if gate.GateState != "completed" {
			continue
		}
		advisorySeats := []string{}
		for _, seat := range gate.DeclaredSeats {
			if seat.PanelRole == "advisory" {
				advisorySeats = append(advisorySeats, seat.WorkflowJobID)
			}
		}
		if len(advisorySeats) == 0 {
			continue
		}
		dissentingSeats := []string{}
		for _, seatWF := range advisorySeats {
			dissented, err := seatHasCurrentSealDissentRead(ctx, runner, repositoryID, gate.RunID, seatWF)
			if err == nil && dissented {
				dissentingSeats = append(dissentingSeats, seatWF)
			}
		}
		if len(dissentingSeats) == 0 {
			continue
		}
		// The finalize is only "ignored" if no advisory-guard blocker was ever raised
		// for this gate (an open OR resolved one means the line was stopped and the
		// operator acted — that is legible, not ignored).
		hadGuard, err := gateEverHadAdvisoryGuardBlocker(ctx, runner, repositoryID, gate.GateJobID)
		if err == nil && hadGuard {
			continue
		}
		sort.Strings(dissentingSeats)
		problems = append(problems, fmt.Sprintf(
			"finalize_ignored_advisory_dissent.%s: completed gate %s finalized while advisory seat(s) %v carried a live dissent and no advisory guard ever fired (run %s); an advisory voice was silently dropped",
			gate.GateWorkflowJobID, gate.GateWorkflowJobID, dissentingSeats, gate.RunID,
		))
		records = append(records, quorumProblemRecord("finalize_ignored_advisory_dissent", gate, map[string]any{
			"dissenting_advisory_seats": dissentingSeats,
		}))
	}
	return problems, records
}

// seatHasCurrentSealDissentRead reports whether a review seat carries a non-superseded
// blocking verdict (needs_revision / reject) at its LIVE attempt.
func seatHasCurrentSealDissentRead(ctx context.Context, runner any, repositoryID, runID, seatWF string) (bool, error) {
	rows, err := collectRows(ctx, runner, `
		WITH live AS (
		  SELECT MAX(attempt) AS attempt FROM striatumd.jobs
		   WHERE repository_id = $1 AND run_id = $2 AND workflow_job_id = $3
		)
		SELECT 1
		  FROM striatumd.verdicts v
		  JOIN striatumd.jobs j
		    ON j.repository_id = v.repository_id AND j.job_id = v.job_id
		  JOIN live ON j.attempt = live.attempt
		 WHERE v.repository_id = $1 AND v.run_id = $2 AND j.workflow_job_id = $3
		   AND v.verdict IN ('needs_revision','reject')
		   AND v.superseded_by_decision_id IS NULL
		 LIMIT 1`, repositoryID, runID, seatWF)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// gateEverHadAdvisoryGuardBlocker reports whether the gate ever carried an
// advisory-guard blocker (any state) — meaning the line was stopped and recorded.
func gateEverHadAdvisoryGuardBlocker(ctx context.Context, runner any, repositoryID, gateJobID string) (bool, error) {
	rows, err := collectRows(ctx, runner, `
		SELECT 1 FROM striatumd.blockers
		 WHERE repository_id = $1 AND job_id = $2
		   AND blocker_kind IN ('advisory_corroborated_abstention',
		                        'unanimous_advisory_reject',
		                        'advisory_only_panel_ungrounded')
		 LIMIT 1`, repositoryID, gateJobID)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// doctorDissentLedgerCompleteness (#339) flags a non-accepting, non-superseded
// verdict whose forward-written dissent token is absent. New rows are keyed by
// verdict_id so later recovery/revision attempt drift does not create a false
// positive; older/null-verdict_id rows fall back to the live seat+attempt check.
// Skipped when the dissent_ledger table is absent (a daemon behind migration 0032).
func doctorDissentLedgerCompleteness(ctx context.Context, runner any, repositoryID string) ([]string, []map[string]any) {
	problems := []string{}
	records := []map[string]any{}
	if !dissentLedgerTableExists(ctx, runner) {
		return problems, records
	}
	rows, err := collectRows(ctx, runner, `
		SELECT v.run_id, j.workflow_job_id, j.attempt, v.verdict, v.verdict_id
		  FROM striatumd.verdicts v
		  JOIN striatumd.jobs j
		    ON j.repository_id = v.repository_id AND j.job_id = v.job_id
		  JOIN striatumd.runs r
		    ON r.repository_id = v.repository_id AND r.run_id = v.run_id
		 WHERE v.repository_id = $1
		   -- #443: a TERMINAL run has no live seat to recover or transfer, so a
		   -- missing dissent_ledger row on it is moot, not an actionable integrity
		   -- gap. The dissent ledger only arrived in migration 0032 (RFC 0135 P4),
		   -- so every run that reached needs_revision/reject before that has a
		   -- verdict but no ledger row by construction; without this scope the check
		   -- reds doctor on ~25 historical/pre-0032 terminal runs. Mirrors the
		   -- terminal-run scoping the artifact (D204) and status-blocker (#419)
		   -- checks already apply. The check stays load-bearing for LIVE barriers.
		   AND r.state NOT IN `+statusTerminalRunStatesSQL+`
		   AND v.verdict IN ('needs_revision','reject')
		   AND v.superseded_by_decision_id IS NULL
		   AND j.attempt = (
		     SELECT MAX(j2.attempt) FROM striatumd.jobs j2
		      WHERE j2.repository_id = j.repository_id AND j2.run_id = j.run_id
		        AND j2.workflow_job_id = j.workflow_job_id
		   )
		   AND NOT EXISTS (
		     SELECT 1 FROM striatumd.dissent_ledger d
		      WHERE d.repository_id = v.repository_id AND d.run_id = v.run_id
		        AND d.workflow_job_id = j.workflow_job_id
		        AND (
		          (d.verdict_id IS NOT NULL AND d.verdict_id = v.verdict_id)
		          OR ((d.verdict_id IS NULL OR v.verdict_id IS NULL) AND d.attempt = j.attempt)
		        )
		   )
		 ORDER BY v.run_id, j.workflow_job_id`,
		repositoryID)
	if err != nil {
		return []string{"dissent_ledger_incomplete.read_failed: " + err.Error()}, []map[string]any{{
			"check": "dissent_ledger_incomplete_read_failed",
			"error": err.Error(),
		}}
	}
	for _, row := range rows {
		runID := stringFrom(row, "run_id")
		seatWF := stringFrom(row, "workflow_job_id")
		attempt := intFrom(row, "attempt")
		verdict := stringFrom(row, "verdict")
		problems = append(problems, fmt.Sprintf(
			"dissent_ledger_incomplete.%s: seat %s has a live %s verdict at attempt %d with no dissent_ledger row (run %s); the forward-write token was not burned — a recovered/transferred seat could read this dissent as absent",
			seatWF, seatWF, verdict, attempt, runID,
		))
		records = append(records, map[string]any{
			"check": "dissent_ledger_incomplete",
			"id":    seatWF,
			"context": map[string]any{
				"run_id":          runID,
				"workflow_job_id": seatWF,
				"attempt":         attempt,
				"verdict":         verdict,
				"verdict_id":      stringFrom(row, "verdict_id"),
			},
		})
	}
	return problems, records
}

// dissentLedgerTableExists reports whether striatumd.dissent_ledger exists (migration
// 0032). A daemon behind the migration skips the completeness check rather than
// crashing the doctor pass.
func dissentLedgerTableExists(ctx context.Context, runner any) bool {
	rows, err := collectRows(ctx, runner, `
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.tables
		   WHERE table_schema = 'striatumd' AND table_name = 'dissent_ledger'
		) AS present`)
	if err != nil || len(rows) == 0 {
		return false
	}
	return boolValue(rows[0]["present"])
}

// quorumProblemRecord builds a verbose record for a quorum/advisory doctor problem,
// mirroring barrierProblemRecord (check + id + context).
func quorumProblemRecord(check string, gate quorumGateView, extra map[string]any) map[string]any {
	ctxMap := map[string]any{
		"gate_workflow_job_id": gate.GateWorkflowJobID,
		"gate_job_id":          gate.GateJobID,
		"run_id":               gate.RunID,
		"gate_state":           gate.GateState,
	}
	for k, v := range extra {
		ctxMap[k] = v
	}
	return map[string]any{
		"check":   check,
		"id":      gate.GateWorkflowJobID,
		"context": ctxMap,
	}
}

// quorumDissentSummary is the RFC 0132 P4b (#342) finalize-decision legibility block
// for run.summary / dashboard: the live (current-seal) dissent_ledger rows that block
// a clean finalize, and any open advisory-guard holds on a gate. It makes a quorum
// hold or an advisory park self-explaining before an operator runs checkpoint resolve.
func quorumDissentSummary(ctx context.Context, runner any, repositoryID, runID string) map[string]any {
	summary := map[string]any{
		"live_dissent":    []map[string]any{},
		"advisory_holds":  []map[string]any{},
		"blocks_finalize": false,
	}
	// Live dissent: a dissent_ledger row at the seat's LIVE attempt (the seal-durable
	// blocking witness). A dissent at a superseded seal no longer blocks. Tolerate an
	// absent dissent_ledger table (a daemon behind migration 0032).
	if dissentLedgerTableExists(ctx, runner) {
		rows, err := collectRows(ctx, runner, `
			SELECT d.workflow_job_id, d.attempt, d.verdict, d.job_id, d.recorded_at
			  FROM striatumd.dissent_ledger d
			 WHERE d.repository_id = $1 AND d.run_id = $2
			   AND d.attempt = (
			     SELECT MAX(j.attempt) FROM striatumd.jobs j
			      WHERE j.repository_id = d.repository_id AND j.run_id = d.run_id
			        AND j.workflow_job_id = d.workflow_job_id
			   )
			 ORDER BY d.workflow_job_id, d.attempt`,
			repositoryID, runID)
		if err == nil {
			live := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				live = append(live, map[string]any{
					"workflow_job_id": stringFrom(row, "workflow_job_id"),
					"attempt":         intFrom(row, "attempt"),
					"verdict":         stringFrom(row, "verdict"),
				})
			}
			summary["live_dissent"] = live
			if len(live) > 0 {
				summary["blocks_finalize"] = true
			}
		}
	}
	// Open advisory-guard holds (the three Layer C guard kinds) on a gate in this run.
	holdRows, err := collectRows(ctx, runner, `
		SELECT b.job_id, j.workflow_job_id AS gate_workflow_job_id, b.blocker_kind, b.blocker_id
		  FROM striatumd.blockers b
		  JOIN striatumd.jobs j
		    ON j.repository_id = b.repository_id AND j.job_id = b.job_id
		 WHERE b.repository_id = $1 AND b.run_id = $2 AND b.state = 'open'
		   AND b.blocker_kind IN ('advisory_corroborated_abstention',
		                          'unanimous_advisory_reject',
		                          'advisory_only_panel_ungrounded')
		 ORDER BY j.workflow_job_id, b.blocker_kind`,
		repositoryID, runID)
	if err == nil {
		holds := make([]map[string]any, 0, len(holdRows))
		for _, row := range holdRows {
			holds = append(holds, map[string]any{
				"gate_workflow_job_id": stringFrom(row, "gate_workflow_job_id"),
				"guard_fired":          stringFrom(row, "blocker_kind"),
				"advisory_outcome":     "must_escalate",
				"blocker_id":           stringFrom(row, "blocker_id"),
			})
		}
		summary["advisory_holds"] = holds
		if len(holds) > 0 {
			summary["blocks_finalize"] = true
		}
	}
	return summary
}

// equalStringSets reports whether two SORTED string slices are equal.
func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
