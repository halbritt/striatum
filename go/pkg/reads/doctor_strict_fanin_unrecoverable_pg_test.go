package reads

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// RFC 0138 (#453) Option A — the legibility slice (ships UNCONDITIONALLY, valuable
// even when the Option B opt-in is off). A strict fan-in barrier blocked on a
// PROVABLY-DEAD required seat (no completed job, no quarantine, owning lane lost) that
// does NOT tolerate a sealed gap surfaces a SPECIFIC doctor reason
// (strict_fanin_required_seat_unrecoverable) naming the seat, the barrier, and the
// exact bounded recovery commands — the FMA-003 case, made legible rather than a
// generic block.

// seedDeadOwningSession binds a job to a session whose supervisor pointer is in the
// conclusive-dead state `lost` (the same durable signal supervisedAgentConfirmedDead
// treats as conclusive), so the seat's owning lane is provably dead.
func seedDeadOwningSession(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID, jobID, sessionID, leaseID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.sessions (
		  repository_id, session_id, run_id, role_id, lane_id, slug, ordinal,
		  capabilities_json, state, registered_at
		) VALUES ($1,$2,$3,'implementer','lane',$2||'-slug',1,'[]'::jsonb,'active',$4)`,
		repoID, sessionID, runID, now); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.leases (
		  repository_id, lease_id, run_id, resource_type, resource_id, owner_session_id,
		  state, acquired_at, expires_at
		) VALUES ($1,$2,$3,'job',$4,$5,'active',$6,$7)`,
		repoID, leaseID, runID, jobID, sessionID, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("seed lease: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET current_lease_id = $1
		 WHERE repository_id = $2 AND job_id = $3`, leaseID, repoID, jobID); err != nil {
		t.Fatalf("bind lease to job: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.process_supervisor_pointers (
		  repository_id, supervisor_id, daemon_supervisor_id, run_id, session_id,
		  pid, pid_start_time, state, updated_at, metadata_json
		) VALUES ($1,$2,$3,$4,$5,0,'','lost',$6,'{}'::jsonb)`,
		repoID, "sup_"+sessionID, "dsup_"+sessionID, runID, sessionID, now); err != nil {
		t.Fatalf("seed lost pointer: %v", err)
	}
}

// TestDoctorFiresOnStrictFaninDeadRequiredSeat is RFC 0138 AC#2 (Option A): a strict
// fan-in (opt-in OFF) with a provably-dead required seat surfaces the specific
// strict_fanin_required_seat_unrecoverable reason, naming the dead seat + the barrier
// + the bounded recovery commands.
func TestDoctorFiresOnStrictFaninDeadRequiredSeat(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_bdoc_fanin_dead"
	runID := "run_bdoc_fanin_dead"
	barrierID := "barrier_bdoc_fanin_dead"
	repoRoot := t.TempDir()

	seedBarrierRepo(t, ctx, runner, repoID, repoRoot)
	seedBarrierRun(t, ctx, runner, repoID, runID, repoRoot)
	// author_a completed + staged. author_dead is outstanding (running, no completed
	// attempt, no quarantine) and its owning lane is provably dead.
	seedBarrierJob(t, ctx, runner, repoID, runID, "job_a1", "author_a", "completed", 1)
	seedBarrierJob(t, ctx, runner, repoID, runID, "job_dead1", "author_dead", "running", 1)
	seedBarrierFreeze(t, ctx, runner, repoID, barrierID, runID, "synth", []string{"author_a", "author_dead"}, "0000000000000000000000000000000000000000")
	seedBarrierStaged(t, ctx, runner, repoID, barrierID, runID, "author_a", "job_a1", 1)
	seedDeadOwningSession(t, ctx, runner, repoID, runID, "job_dead1", "sess_dead", "lease_dead")

	_, problems, records, _, _ := doctorBarrierIntegrity(ctx, runner, repoID)

	var problem string
	for _, p := range problems {
		if strings.HasPrefix(p, "strict_fanin_required_seat_unrecoverable.") {
			problem = p
		}
	}
	if problem == "" {
		t.Fatalf("expected strict_fanin_required_seat_unrecoverable problem (FMA-003), got: %v", problems)
	}
	// The message names the dead seat and the barrier.
	if !strings.Contains(problem, "author_dead") {
		t.Fatalf("problem must name the dead seat author_dead: %q", problem)
	}
	if !strings.Contains(problem, barrierID) {
		t.Fatalf("problem must name the barrier %q: %q", barrierID, problem)
	}
	// The verbose record carries the bounded recovery commands and the seat/barrier.
	var rec map[string]any
	for _, r := range records {
		if r["check"] == "strict_fanin_required_seat_unrecoverable" {
			rec = r
		}
	}
	if rec == nil {
		t.Fatalf("expected a strict_fanin_required_seat_unrecoverable record; records=%v", records)
	}
	ctxMap, _ := rec["context"].(map[string]any)
	if stringFrom(ctxMap, "barrier_id") != barrierID {
		t.Fatalf("record context must name the barrier; got %v", ctxMap)
	}
	deadSeats, _ := ctxMap["unrecoverable_seats"].([]string)
	found := false
	for _, s := range deadSeats {
		if s == "author_dead" {
			found = true
		}
	}
	if !found {
		t.Fatalf("record must list author_dead in unrecoverable_seats; got %v", ctxMap["unrecoverable_seats"])
	}
	actions, _ := ctxMap["recovery_commands"].([]string)
	if len(actions) == 0 {
		t.Fatalf("record must carry bounded recovery_commands; got %v", ctxMap)
	}
	joined := strings.Join(actions, " | ")
	if !strings.Contains(joined, "cancel") {
		t.Fatalf("recovery_commands must include the bounded cancel/quarantine path; got %v", actions)
	}
}

// TestDoctorQuietOnStrictFaninSlowSeat asserts the reason does NOT fire for a merely-
// SLOW (not provably dead) outstanding required seat — silence from a live lane is not
// the FMA-003 case (a slow seat is a healthy in-flight barrier).
func TestDoctorQuietOnStrictFaninSlowSeat(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	runner := pool.Runner

	repoID := "repo_bdoc_fanin_slow"
	runID := "run_bdoc_fanin_slow"
	barrierID := "barrier_bdoc_fanin_slow"
	repoRoot := t.TempDir()

	seedBarrierRepo(t, ctx, runner, repoID, repoRoot)
	seedBarrierRun(t, ctx, runner, repoID, runID, repoRoot)
	seedBarrierJob(t, ctx, runner, repoID, runID, "job_a1", "author_a", "completed", 1)
	// author_slow is outstanding (running) but its lane is ATTACHED — alive, not dead.
	seedBarrierJob(t, ctx, runner, repoID, runID, "job_slow1", "author_slow", "running", 1)
	seedBarrierFreeze(t, ctx, runner, repoID, barrierID, runID, "synth", []string{"author_a", "author_slow"}, "0000000000000000000000000000000000000000")
	seedBarrierStaged(t, ctx, runner, repoID, barrierID, runID, "author_a", "job_a1", 1)
	// Bind a session whose pointer is ATTACHED (alive).
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.sessions (
		  repository_id, session_id, run_id, role_id, lane_id, slug, ordinal,
		  capabilities_json, state, registered_at
		) VALUES ($1,'sess_slow',$2,'implementer','lane','sess_slow-slug',1,'[]'::jsonb,'active',$3)`,
		repoID, runID, now); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.leases (
		  repository_id, lease_id, run_id, resource_type, resource_id, owner_session_id,
		  state, acquired_at, expires_at
		) VALUES ($1,'lease_slow',$2,'job','job_slow1','sess_slow','active',$3,$4)`,
		repoID, runID, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("seed lease: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET current_lease_id = 'lease_slow'
		 WHERE repository_id = $1 AND job_id = 'job_slow1'`, repoID); err != nil {
		t.Fatalf("bind lease: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.process_supervisor_pointers (
		  repository_id, supervisor_id, daemon_supervisor_id, run_id, session_id,
		  pid, pid_start_time, state, updated_at, metadata_json
		) VALUES ($1,'sup_slow','dsup_slow',$2,'sess_slow',4242,'','attached',$3,'{}'::jsonb)`,
		repoID, runID, now); err != nil {
		t.Fatalf("seed attached pointer: %v", err)
	}

	_, problems, _, _, _ := doctorBarrierIntegrity(ctx, runner, repoID)
	for _, p := range problems {
		if strings.HasPrefix(p, "strict_fanin_required_seat_unrecoverable.") {
			t.Fatalf("a merely-SLOW (alive) outstanding seat must NOT trip the FMA-003 reason: %q", p)
		}
	}
}
