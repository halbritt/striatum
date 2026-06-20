package reads

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// RFC 0132 P4b (#342) — PG-backed tests for the panel-quorum / advisory-dissent
// doctor invariant + the #339 dissent-ledger completeness check. They exercise the
// fires-on-bad / quiet-on-good discipline: silent on a healthy panel, loud on an
// unresolvable seat, a denominator mismatch, an ignored advisory dissent, or a
// missing forward-write.

// seedQuorumDoctorRepoRun seeds a repo + run whose snapshot carries the panel: a
// jobs[] with panel_role per review seat and an edges[] declaring review→gate edges.
func seedQuorumDoctorRepoRun(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID string, snapshotJSON string) {
	t.Helper()
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.repositories (
		  repository_id, repo_identity, repo_root, state_db_path, display_name,
		  registered_at, last_schema_version, state
		) VALUES ($1,$2,$3,$4,'repo',$5,16,'active')`,
		repoID, "ident_"+repoID, "/tmp/"+repoID, "/tmp/"+repoID+"/.striatum", now); err != nil {
		t.Fatalf("insert repository: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.workflow_snapshots (
		  repository_id, workflow_snapshot_id, workflow_id, content_sha256, workflow_json, loaded_at
		) VALUES ($1,$2,'wf','sha',$3::jsonb,$4)`,
		repoID, "snap_"+repoID, snapshotJSON, now); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.runs (
		  repository_id, run_id, workflow_snapshot_id, repo_root, state, created_at
		) VALUES ($1,$2,$3,$4,'running',$5)`,
		repoID, runID, "snap_"+repoID, "/tmp/"+repoID, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

func seedQuorumDoctorJob(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, jobID, workflowJobID, jobType, state string, attempt int) {
	t.Helper()
	now := time.Now().UTC()
	role := "reviewer"
	if jobType != "review" {
		role = "adjudicator"
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, title, job_type, role_id,
		  state, attempt, idempotency_key, created_at
		) VALUES ($1,$2,$3,$4,'J',$5,$6,$7,$8,$9,$10)`,
		repoID, jobID, runID, workflowJobID, jobType, role, state, attempt, "idem_"+jobID, now); err != nil {
		t.Fatalf("insert job %s: %v", jobID, err)
	}
}

func seedQuorumDoctorDep(t *testing.T, ctx context.Context, runner db.Runner, repoID, gateJobID, seatJobID string) {
	t.Helper()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.job_dependencies (repository_id, job_id, depends_on_job_id, gate_json)
		VALUES ($1,$2,$3,'{"on":"completed"}'::jsonb)
		ON CONFLICT (repository_id, job_id, depends_on_job_id) DO NOTHING`,
		repoID, gateJobID, seatJobID); err != nil {
		t.Fatalf("insert dep: %v", err)
	}
}

func seedQuorumDoctorSession(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, sessionID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.sessions (
		  repository_id, session_id, run_id, role_id, lane_id, slug, ordinal,
		  capabilities_json, fresh_context, state, registered_at
		) VALUES ($1,$2,$3,'reviewer','lane',$4,1,'[]'::jsonb,true,'active',$5)`,
		repoID, sessionID, runID, "slug_"+sessionID, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func seedQuorumDoctorVerdict(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, verdictID, jobID, sessionID, verdict string) {
	t.Helper()
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.verdicts (
		  repository_id, verdict_id, run_id, job_id, session_id, verdict, created_at, posture
		) VALUES ($1,$2,$3,$4,$5,$6,$7,'neutral')`,
		repoID, verdictID, runID, jobID, sessionID, verdict, now); err != nil {
		t.Fatalf("insert verdict: %v", err)
	}
}

func seedQuorumDoctorDissent(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, seatWF, jobID string, attempt int, verdict string) {
	seedQuorumDoctorDissentForVerdict(t, ctx, runner, repoID, runID, seatWF, jobID, attempt, verdict, "")
}

func seedQuorumDoctorDissentForVerdict(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, seatWF, jobID string, attempt int, verdict, verdictID string) {
	t.Helper()
	now := time.Now().UTC()
	var verdictRef any
	if verdictID != "" {
		verdictRef = verdictID
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.dissent_ledger (
		  repository_id, dissent_id, run_id, workflow_job_id, attempt, job_id, verdict_id, verdict, recorded_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		repoID, "d_"+jobID, runID, seatWF, attempt, jobID, verdictRef, verdict, now); err != nil {
		t.Fatalf("insert dissent: %v", err)
	}
}

// panelSnapshot builds a workflow_json with the given review seats (id→panel_role)
// and a single gate with explicit review→gate edges.
func panelSnapshot(gateID string, seatRoles map[string]string) string {
	var jobs, edges []string
	jobs = append(jobs, `{"id":"`+gateID+`","type":"synthesis"}`)
	for seat, role := range seatRoles {
		jobs = append(jobs, `{"id":"`+seat+`","type":"review","panel_role":"`+role+`"}`)
		edges = append(edges, `{"from":"`+seat+`","to":"`+gateID+`","on":"completed"}`)
	}
	return `{"jobs":[` + strings.Join(jobs, ",") + `],"edges":[` + strings.Join(edges, ",") + `]}`
}

func hasQuorumProblem(problems []string, prefix string) bool {
	for _, p := range problems {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// --- tests -------------------------------------------------------------------

// TestDoctorQuorumQuietOnHealthyPanel asserts the invariant stays SILENT on a healthy
// panel: every gating seat has a job row, all accept, the denominator matches.
func TestDoctorQuorumQuietOnHealthyPanel(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_qdoc_quiet"
	runID := "run_qdoc_quiet"
	seedQuorumDoctorRepoRun(t, ctx, runner, repoID, runID, panelSnapshot("gate", map[string]string{
		"rev_a": "gating", "rev_b": "gating",
	}))
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_gate", "gate", "synthesis", "running", 1)
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_a", "rev_a", "review", "completed", 1)
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_b", "rev_b", "review", "completed", 1)
	seedQuorumDoctorDep(t, ctx, runner, repoID, "job_gate", "job_a")
	seedQuorumDoctorDep(t, ctx, runner, repoID, "job_gate", "job_b")

	_, problems, _, _, _ := doctorQuorumIntegrity(ctx, runner, repoID)
	for _, p := range problems {
		if strings.HasPrefix(p, "quorum_") || strings.HasPrefix(p, "finalize_") || strings.HasPrefix(p, "dissent_ledger_") {
			t.Fatalf("healthy panel produced a problem: %q", p)
		}
	}
}

// TestDoctorQuorumFiresOnUnresolvableSeat asserts quorum_seat_unresolvable fires when
// the snapshot declares a gating seat (rev_ghost) feeding the gate but NO job row
// exists for it — a permanent fail-closed deadlock (the seat can never resolve to a
// live attempt). The gate IS a panel (it has a live review-seat dependency on rev_a),
// so loadQuorumGates includes it and resolves the frozen denominator from the snapshot.
func TestDoctorQuorumFiresOnUnresolvableSeat(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_qdoc_unresolvable"
	runID := "run_qdoc_unresolvable"
	// The snapshot declares two gating seats feeding the gate: rev_a and rev_ghost.
	seedQuorumDoctorRepoRun(t, ctx, runner, repoID, runID, panelSnapshot("gate", map[string]string{
		"rev_a": "gating", "rev_ghost": "gating",
	}))
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_gate", "gate", "synthesis", "blocked", 1)
	// rev_a has a live job row + dependency edge (makes the gate a recognised panel) ...
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_a", "rev_a", "review", "completed", 1)
	seedQuorumDoctorDep(t, ctx, runner, repoID, "job_gate", "job_a")
	// ... but rev_ghost (declared in the snapshot) has NO job row at all — unresolvable.

	_, problems, records, _, _ := doctorQuorumIntegrity(ctx, runner, repoID)
	if !hasQuorumProblem(problems, "quorum_seat_unresolvable.gate") {
		t.Fatalf("expected quorum_seat_unresolvable, got: %v", problems)
	}
	// The verbose record names the unresolvable seat.
	found := false
	for _, rec := range records {
		if rec["check"] != "quorum_seat_unresolvable" {
			continue
		}
		ctxMap, _ := rec["context"].(map[string]any)
		if stringFrom(ctxMap, "unresolvable_seat") == "rev_ghost" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unresolvable record did not name rev_ghost; records=%v", records)
	}
}

// TestDoctorQuorumUnresolvableSeatSilentOnTerminalGate asserts a completed gate does
// NOT fire quorum_seat_unresolvable (a finalized gate already cleared its denominator).
func TestDoctorQuorumUnresolvableSeatSilentOnTerminalGate(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_qdoc_unres_terminal"
	runID := "run_qdoc_unres_terminal"
	seedQuorumDoctorRepoRun(t, ctx, runner, repoID, runID, panelSnapshot("gate", map[string]string{
		"rev_a": "gating", "rev_ghost": "gating",
	}))
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_gate", "gate", "synthesis", "completed", 1)
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_a", "rev_a", "review", "completed", 1)
	seedQuorumDoctorDep(t, ctx, runner, repoID, "job_gate", "job_a")

	_, problems, _, _, _ := doctorQuorumIntegrity(ctx, runner, repoID)
	if hasQuorumProblem(problems, "quorum_seat_unresolvable.gate") {
		t.Fatalf("a completed gate must NOT fire quorum_seat_unresolvable; got: %v", problems)
	}
}

// TestDoctorQuorumFiresOnDenominatorMismatch asserts quorum_denominator_mismatch fires
// when the live gating-seat dependency set disagrees with the frozen snapshot edges.
func TestDoctorQuorumFiresOnDenominatorMismatch(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_qdoc_mismatch"
	runID := "run_qdoc_mismatch"
	// The snapshot freezes TWO gating seats (rev_a, rev_b) feeding the gate ...
	seedQuorumDoctorRepoRun(t, ctx, runner, repoID, runID, panelSnapshot("gate", map[string]string{
		"rev_a": "gating", "rev_b": "gating",
	}))
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_gate", "gate", "synthesis", "blocked", 1)
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_a", "rev_a", "review", "completed", 1)
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_b", "rev_b", "review", "completed", 1)
	// ... but the LIVE job graph only wires rev_a → gate (rev_b's edge was dropped).
	seedQuorumDoctorDep(t, ctx, runner, repoID, "job_gate", "job_a")

	_, problems, _, _, _ := doctorQuorumIntegrity(ctx, runner, repoID)
	if !hasQuorumProblem(problems, "quorum_denominator_mismatch.gate") {
		t.Fatalf("expected quorum_denominator_mismatch, got: %v", problems)
	}
}

// TestDoctorFinalizeIgnoredAdvisoryDissent asserts finalize_ignored_advisory_dissent
// fires when a completed gate finalized while an advisory seat carried a live dissent
// and no advisory-guard blocker ever fired.
func TestDoctorFinalizeIgnoredAdvisoryDissent(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_qdoc_ignored"
	runID := "run_qdoc_ignored"
	seedQuorumDoctorRepoRun(t, ctx, runner, repoID, runID, panelSnapshot("gate", map[string]string{
		"rev_g":   "gating",
		"rev_adv": "advisory",
	}))
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_gate", "gate", "synthesis", "completed", 1)
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_g", "rev_g", "review", "completed", 1)
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_adv", "rev_adv", "review", "completed", 1)
	seedQuorumDoctorDep(t, ctx, runner, repoID, "job_gate", "job_g")
	seedQuorumDoctorDep(t, ctx, runner, repoID, "job_gate", "job_adv")
	seedQuorumDoctorSession(t, ctx, runner, repoID, runID, "sess_adv")
	// The advisory seat rejected at its live attempt 1, but the gate completed anyway.
	seedQuorumDoctorVerdict(t, ctx, runner, repoID, runID, "v_adv", "job_adv", "sess_adv", "reject")

	_, problems, _, _, _ := doctorQuorumIntegrity(ctx, runner, repoID)
	if !hasQuorumProblem(problems, "finalize_ignored_advisory_dissent.gate") {
		t.Fatalf("expected finalize_ignored_advisory_dissent, got: %v", problems)
	}
}

// TestDoctorDissentLedgerCompletenessFires asserts dissent_ledger_incomplete fires
// when a live blocking verdict has NO dissent_ledger row (the forward-write hole),
// and is SILENT once the ledger row exists.
func TestDoctorDissentLedgerCompletenessFires(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_qdoc_ledger"
	runID := "run_qdoc_ledger"
	seedQuorumDoctorRepoRun(t, ctx, runner, repoID, runID, panelSnapshot("gate", map[string]string{
		"rev_g": "gating",
	}))
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_gate", "gate", "synthesis", "blocked", 1)
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_g", "rev_g", "review", "completed", 1)
	seedQuorumDoctorDep(t, ctx, runner, repoID, "job_gate", "job_g")
	seedQuorumDoctorSession(t, ctx, runner, repoID, runID, "sess_g")
	// A needs_revision verdict at the seat's live attempt 1 with NO dissent_ledger row.
	seedQuorumDoctorVerdict(t, ctx, runner, repoID, runID, "v_g", "job_g", "sess_g", "needs_revision")

	_, problems, _, _, _ := doctorQuorumIntegrity(ctx, runner, repoID)
	if !hasQuorumProblem(problems, "dissent_ledger_incomplete.rev_g") {
		t.Fatalf("expected dissent_ledger_incomplete, got: %v", problems)
	}

	// Now write the forward-write token; the check goes silent.
	seedQuorumDoctorDissent(t, ctx, runner, repoID, runID, "rev_g", "job_g", 1, "needs_revision")
	_, problems2, _, _, _ := doctorQuorumIntegrity(ctx, runner, repoID)
	if hasQuorumProblem(problems2, "dissent_ledger_incomplete.rev_g") {
		t.Fatalf("dissent_ledger_incomplete must be silent once the ledger row exists, got: %v", problems2)
	}
}

// TestDoctorDissentLedgerCompletenessUsesVerdictIDAcrossAttemptDrift is the #493
// trap: a blocking verdict can write its dissent token, then revision/recovery can
// advance the seat's live attempt. The forward-write is still valid because it is
// tied to the recorded verdict_id; doctor must not misread the later attempt bump
// as a missing token.
func TestDoctorDissentLedgerCompletenessUsesVerdictIDAcrossAttemptDrift(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_qdoc_ledger_drift"
	runID := "run_qdoc_ledger_drift"
	seedQuorumDoctorRepoRun(t, ctx, runner, repoID, runID, panelSnapshot("gate", map[string]string{
		"rev_g": "gating",
	}))
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_gate", "gate", "synthesis", "blocked", 1)
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_g", "rev_g", "review", "completed", 1)
	seedQuorumDoctorDep(t, ctx, runner, repoID, "job_gate", "job_g")
	seedQuorumDoctorSession(t, ctx, runner, repoID, runID, "sess_g")
	seedQuorumDoctorVerdict(t, ctx, runner, repoID, runID, "v_g", "job_g", "sess_g", "needs_revision")
	seedQuorumDoctorDissentForVerdict(t, ctx, runner, repoID, runID, "rev_g", "job_g", 1, "needs_revision", "v_g")

	if err := runner.Exec(ctx, `UPDATE striatumd.jobs SET attempt=2 WHERE repository_id=$1 AND job_id=$2`, repoID, "job_g"); err != nil {
		t.Fatalf("advance job attempt: %v", err)
	}

	_, problems, _, _, _ := doctorQuorumIntegrity(ctx, runner, repoID)
	if hasQuorumProblem(problems, "dissent_ledger_incomplete.rev_g") {
		t.Fatalf("dissent_ledger_incomplete must be silent when the verdict_id token exists across attempt drift, got: %v", problems)
	}
}

// TestDoctorDissentLedgerCompletenessSilentOnTerminalRun is the #443 fix: the check
// fires on a LIVE run with a missing dissent_ledger row, but goes SILENT once the run
// is terminal. A terminal run has no live seat to recover/transfer (so the absent
// forward-write token is moot), and every run that reached needs_revision before
// migration 0032 has a verdict but no ledger row by construction.
func TestDoctorDissentLedgerCompletenessSilentOnTerminalRun(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_qdoc_ledger_term"
	runID := "run_qdoc_ledger_term"
	seedQuorumDoctorRepoRun(t, ctx, runner, repoID, runID, panelSnapshot("gate", map[string]string{
		"rev_g": "gating",
	}))
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_gate", "gate", "synthesis", "blocked", 1)
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_g", "rev_g", "review", "completed", 1)
	seedQuorumDoctorDep(t, ctx, runner, repoID, "job_gate", "job_g")
	seedQuorumDoctorSession(t, ctx, runner, repoID, runID, "sess_g")
	seedQuorumDoctorVerdict(t, ctx, runner, repoID, runID, "v_g", "job_g", "sess_g", "needs_revision")

	// Live run: the check fires (the setup is a genuine positive, so the test is
	// not vacuous).
	_, problems, _, _, _ := doctorQuorumIntegrity(ctx, runner, repoID)
	if !hasQuorumProblem(problems, "dissent_ledger_incomplete.rev_g") {
		t.Fatalf("expected dissent_ledger_incomplete on the LIVE run, got: %v", problems)
	}

	// Terminal run: the check goes silent (#443).
	if err := runner.Exec(ctx, `UPDATE striatumd.runs SET state='canceled' WHERE repository_id=$1 AND run_id=$2`, repoID, runID); err != nil {
		t.Fatalf("cancel run: %v", err)
	}
	_, problems2, _, _, _ := doctorQuorumIntegrity(ctx, runner, repoID)
	if hasQuorumProblem(problems2, "dissent_ledger_incomplete.rev_g") {
		t.Fatalf("dissent_ledger_incomplete must be silent on a terminal run, got: %v", problems2)
	}
}

// TestRunSummaryQuorumDissentLegibility asserts run.summary surfaces the live dissent
// rows and open advisory holds so a quorum/advisory park is self-explaining before
// checkpoint resolve (#342 finalize-decision legibility).
func TestRunSummaryQuorumDissentLegibility(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_qdoc_legibility"
	runID := "run_qdoc_legibility"
	seedQuorumDoctorRepoRun(t, ctx, runner, repoID, runID, panelSnapshot("gate", map[string]string{
		"rev_g": "gating",
	}))
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_gate", "gate", "synthesis", "blocked", 1)
	seedQuorumDoctorJob(t, ctx, runner, repoID, runID, "job_g", "rev_g", "review", "completed", 1)
	seedQuorumDoctorDep(t, ctx, runner, repoID, "job_gate", "job_g")
	// A live dissent at the seat's live attempt 1.
	seedQuorumDoctorDissent(t, ctx, runner, repoID, runID, "rev_g", "job_g", 1, "needs_revision")
	// An open advisory-guard hold on the gate.
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.blockers (
		  repository_id, blocker_id, run_id, job_id, severity, blocker_kind, description, state, created_at
		) VALUES ($1,'blk_adv',$2,'job_gate','blocked','unanimous_advisory_reject','held','open',now())`,
		repoID, runID); err != nil {
		t.Fatalf("insert advisory hold: %v", err)
	}

	summary := quorumDissentSummary(ctx, runner, repoID, runID)
	if summary["blocks_finalize"] != true {
		t.Fatalf("blocks_finalize must be true with a live dissent + advisory hold; got %v", summary["blocks_finalize"])
	}
	live, _ := summary["live_dissent"].([]map[string]any)
	if len(live) != 1 || stringFrom(live[0], "workflow_job_id") != "rev_g" {
		t.Fatalf("live_dissent must name rev_g; got %v", summary["live_dissent"])
	}
	holds, _ := summary["advisory_holds"].([]map[string]any)
	if len(holds) != 1 || stringFrom(holds[0], "guard_fired") != "unanimous_advisory_reject" {
		t.Fatalf("advisory_holds must name the unanimous_advisory_reject hold; got %v", summary["advisory_holds"])
	}
}
