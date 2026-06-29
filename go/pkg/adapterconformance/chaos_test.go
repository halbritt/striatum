package adapterconformance

// RFC 0101 Phase 5 — fault-injection chaos suite (the robustness regression
// gate). These tests drive the REAL fake-agent lifecycle through the in-process
// daemon (NewHarness + the production mutations/reads handlers), INJECT a lane
// failure (a dead pane / a stalled-past-deadline lane), run the production
// recovery sweep (mutations.SweepRun -> HandleRecoveryAuto -> recoverStuckJobs
// -> escalateExhaustedJobs), and assert the run either SELF-RECOVERS (a fresh
// lane completes the requeued packet on the same attempt, no operator) OR
// ESCALATES loudly (needs_operator + a recovery_exhausted escalation_inbox row)
// within a bounded budget — never a silent wedge.
//
// ALL fault injection lives in this test layer: the time-warp helpers
// (injectStalledLane / injectDeadLane) UPDATE the harness DB's session + lease
// timestamps into the past to simulate elapsed time so sessionliveness.Classify
// sees a dead/stalled lane; the runSweep wrapper drives the production sweep;
// and the fresh-replacement-agent helper registers a NEW session and runs the
// REAL testagent ModeHappy lifecycle through the production claim/ack/publish/
// complete handlers. NO shipped daemon code gains a fault hook.
//
// Gating: these are PG-gated exactly like the existing conformance tests —
// NewHarness calls pgtest.Pool which t.Skip()s cleanly when STRIATUM_PG_TEST_URL
// is unset, so they ride the existing CI PostgreSQL tier and add ZERO cost to
// the hermetic `make -C go test` (which runs without the URL and skips them).
//
// RFC 0103 W3 (#141) — KNOWN APPROXIMATION. This in-process chaos suite is the
// hermetic robustness gate, but it is deliberately NOT the primary gate for the
// daemon-restart class (#141): it time-warps DB timestamps to simulate a
// dead/stalled lane and never orphans an OS helper process or recreates the
// daemon socket. The real surface — a `systemctl restart striatumd` that
// SIGKILLs the helper cgroup / cancels its spawn context and recreates the
// socket mid-run — is covered by the [live-corroborated] gate
// (docs/dogfoods/rfc-0103-w3-141-restart). The escalation-within-budget assertions
// below are the success criterion only for an explicitly UNRECOVERABLE injected
// fault, with a paired assertion that the run never silently wedges; a
// recoverable fault (the systemd restart) must COMPLETE, not escalate.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/adapterconformance/testagent"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/mutations"
	recoverypkg "github.com/halbritt/striatum/go/pkg/recovery"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	"github.com/jackc/pgx/v5/pgconn"
)

// chaosSweepAuthor is the audit author the chaos sweeps record on their
// daemon.recovery_sweep event.
const chaosSweepAuthor = "striatumd-chaos"

// --- fault-injection seam (TEST LAYER ONLY) --------------------------------

// sweepSummary is the recovery_actions / escalations summary extracted from a
// SweepRun result, plus the raw result map for deeper assertions.
type sweepSummary struct {
	result          map[string]any
	actedCount      int
	escalationCount int
}

// runSweep drives one full production recovery sweep against the harness DB and
// returns its recovery_actions / escalations summary. It is the chaos test's
// trigger: the SAME mutations.SweepRun the live daemon background loop calls,
// against the isolated pgtest database — no fault hook is added to it.
func runSweep(t *testing.T, ctx context.Context, h *Harness, runID string) sweepSummary {
	t.Helper()
	result, err := mutations.SweepRun(ctx, h.Runner, h.RepositoryID, runID, chaosSweepAuthor)
	if err != nil {
		t.Fatalf("sweep run %s: %v", runID, err)
	}
	summary := sweepSummary{result: result}
	if ra, ok := result["recovery_actions"].(map[string]any); ok {
		summary.actedCount = intFromMap(ra, "acted_count")
	}
	if es, ok := result["escalations"].(map[string]any); ok {
		summary.escalationCount = intFromMap(es, "raised_count")
	}
	return summary
}

func TestRecoverySchedulerSurvivesPGConnectionKill(t *testing.T) {
	ctx := context.Background()
	maxSweeps := 2
	calls := 0
	adminShutdown := &pgconn.PgError{Code: "57P01", Message: "terminating connection due to administrator command"}
	waited := 0

	result, err := recoverypkg.RunScheduler(ctx, recoverypkg.SchedulerOptions{
		Interval:    time.Millisecond,
		MinInterval: time.Millisecond,
		MaxSweeps:   &maxSweeps,
		SweepOnce: func(context.Context) (map[string]any, error) {
			calls++
			if calls == 1 {
				return nil, adminShutdown
			}
			return map[string]any{"status": "sweep_after_pg_reconnect"}, nil
		},
		Wait: func(context.Context, time.Duration) bool {
			waited++
			return true
		},
	})
	if err != nil {
		t.Fatalf("scheduler returned PG admin-shutdown as daemon-fatal error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("sweep calls = %d, want injected failure plus retry", calls)
	}
	if waited != 1 {
		t.Fatalf("waits = %d, want one backoff between failed sweep and retry", waited)
	}
	if result.FailedSweeps != 1 || result.Sweeps != 2 {
		t.Fatalf("scheduler result = %#v, want one contained failed sweep across two attempts", result)
	}
}

// injectDeadLane simulates a HARD dead lane: the supervised pane died and its
// session closed, releasing its lease, leaving the job in running-limbo. It
// closes the session (state='closed') and releases any active lease on the job.
// sessionliveness.Classify then treats the owning session as dead, so the
// recovery decision tree's CASE 1 (dead/closed owner + lease cleared) requeues
// the job on the same attempt (requeue_count budget). It is the #121 shape.
func injectDeadLane(t *testing.T, ctx context.Context, runner db.Runner, repositoryID, sessionID, jobID string) {
	t.Helper()
	past := time.Now().UTC().Add(-2 * time.Hour)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.sessions
		   SET state = 'closed', closed_at = $1, close_reason = 'chaos_dead_pane'
		 WHERE repository_id = $2 AND session_id = $3`,
		past, repositoryID, sessionID); err != nil {
		t.Fatalf("inject dead lane (close session %s): %v", sessionID, err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'released', released_at = COALESCE(released_at, $1),
		       release_reason = 'chaos_dead_pane'
		 WHERE repository_id = $2 AND resource_id = $3 AND state = 'active'`,
		past, repositoryID, jobID); err != nil {
		t.Fatalf("inject dead lane (release lease for %s): %v", jobID, err)
	}
}

// injectMaskedDeadAgent simulates #147 Symptom B (the masked-dead-agent silent
// wedge): the supervised AGENT process died, but an operator heartbeat loop
// (`striatum heartbeat --session-id --lease-id`, the documented long-command
// lease-expiry mitigation) keeps the lease and session WARM, so the
// lease-freshness liveness signal masks the death and the job sits `running`
// forever, never requeued. Unlike injectDeadLane it does NOT close the session
// or release the lease — instead it refreshes the lease expiry + heartbeat (the
// operator keeping it alive) and records the supervised agent as an `attached`
// pointer whose PID is not alive (a PID above any real Linux pid). State stays
// `attached` because nothing marked it lost: the background sweep never probes
// the agent PID — that IS the bug. The recovery sweep must detect the dead agent
// and requeue despite the warm lease.
func injectMaskedDeadAgent(t *testing.T, ctx context.Context, runner db.Runner, repositoryID, runID, sessionID, jobID string) {
	t.Helper()
	now := time.Now().UTC()
	const deadPID = 2147483646 // above any real Linux PID -> syscall.Kill(deadPID,0)==ESRCH (dead)
	// The operator heartbeat keeps the lease warm: future expiry + fresh heartbeat.
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases
		   SET expires_at = $1, last_heartbeat_at = $2
		 WHERE repository_id = $3 AND resource_id = $4 AND state = 'active'`,
		now.Add(time.Hour), now, repositoryID, jobID); err != nil {
		t.Fatalf("inject masked dead agent (warm lease %s): %v", jobID, err)
	}
	// Age any prior (released/expired) lease on the job into the past so the
	// recovery lease resolver unambiguously picks the live att2 lease. In a real
	// run the prior attempt's lease was released minutes ago; the harness creates
	// both within the same second, and recoverStuckJobs orders leases by
	// (acquired_at DESC, lease_id DESC), so a same-second tie would let the random
	// lease_id resolve the closed-session released lease and mask the test's
	// intent. (The same-second tie is a latent false-requeue risk in its own
	// right — tracked separately.)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases
		   SET acquired_at = $1
		 WHERE repository_id = $2 AND resource_id = $3 AND state <> 'active'`,
		now.Add(-10*time.Minute), repositoryID, jobID); err != nil {
		t.Fatalf("inject masked dead agent (age prior leases %s): %v", jobID, err)
	}
	// Record the supervised agent as a (secretly dead) attached pointer. Reuse
	// the attest supervisor id ("sup_"+session) so this flips the session's
	// existing attached pointer (seedFixtureSession now attests every scenario
	// session, #243/dbf2013b) to the dead pid via ON CONFLICT — inserting a
	// second attached pointer would violate uq_active_daemon_supervisor_pointer_per_session.
	supID := "sup_" + sessionID
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.process_supervisor_pointers (
		  repository_id, supervisor_id, daemon_supervisor_id, run_id, session_id,
		  pid, pid_start_time, state, updated_at, metadata_json
		) VALUES ($1,$2,$3,$4,$5,$6,'','attached',$7,'{}'::jsonb)
		ON CONFLICT (repository_id, supervisor_id) DO UPDATE
		   SET pid = EXCLUDED.pid, state = EXCLUDED.state, updated_at = EXCLUDED.updated_at`,
		repositoryID, supID, "dsup_"+sessionID, runID, sessionID, deadPID, now); err != nil {
		t.Fatalf("inject masked dead agent (pointer %s): %v", sessionID, err)
	}
}

// injectStalledLane simulates a STALLED lane: the session is still active (the
// pane never died, it is wedged), but it has produced NO protocol / PTY / tool
// progress past every liveness deadline. It ages every last_* protocol column,
// registered_at, and the owning active lease's acquired_at / expires_at /
// last_heartbeat_at deep into the past (the time-warp) so sessionliveness.Classify
// reports ProtocolStalled with a still-active session — the honest #121 parked
// agent. The recovery decision tree's CASE 2 then transfer-requeues the job
// (transfer_count budget) and closes the stalled owner.
//
// This is the core chaos primitive: elapsed time is simulated by warping
// timestamps, not by sleeping, so the scenario is fully deterministic.
func injectStalledLane(t *testing.T, ctx context.Context, runner db.Runner, repositoryID, sessionID, jobID string) {
	t.Helper()
	past := time.Now().UTC().Add(-2 * time.Hour)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.sessions
		   SET registered_at = $1,
		       last_mcp_request_at = $1, last_tools_list_at = $1,
		       last_await_packet_at = $1, last_packet_delivered_at = $1,
		       last_ack_at = $1, last_work_block_at = $1,
		       last_work_release_at = $1, last_work_heartbeat_at = $1,
		       last_session_ready_at = $1, last_session_heartbeat_at = $1,
		       last_pty_activity_at = $1,
		       last_tool_call_started_at = NULL, last_tool_call_finished_at = NULL
		 WHERE repository_id = $2 AND session_id = $3 AND state = 'active'`,
		past, repositoryID, sessionID); err != nil {
		t.Fatalf("inject stalled lane (age session %s): %v", sessionID, err)
	}
	// Age the lease too: an active lease with a deep-past heartbeat trips the
	// lease-heartbeat stall (the terminal liveness rung for a lease holder), so a
	// still-active but wedged lease holder classifies stalled. Leave the lease
	// 'active' + past-expiry so the sweep's expireLeases flips it to expired,
	// matching the live path the decision tree sees.
	if err := runner.Exec(ctx, `
		UPDATE striatumd.leases
		   SET acquired_at = $1, expires_at = $1, last_heartbeat_at = $1
		 WHERE repository_id = $2 AND resource_id = $3 AND state = 'active'`,
		past, repositoryID, jobID); err != nil {
		t.Fatalf("inject stalled lane (age lease for %s): %v", jobID, err)
	}
}

// freshReplacementAgent registers a NEW session for the same role/lane and runs
// the REAL testagent ModeHappy lifecycle against the production handlers to
// claim the REQUEUED packet and complete it. This is the self-recovery proof:
// it exercises the genuine session.register / work.await_packet / work.ack /
// work.heartbeat / artifact.publish / work.complete state machine — NOT a
// shortcut — so a passing test means a fresh lane truly completed the work the
// dead lane abandoned. It returns the agent's terminal Result.
func freshReplacementAgent(t *testing.T, ctx context.Context, h *Harness, fx *Fixture) testagent.Result {
	t.Helper()
	cfg := testagent.Config{
		Endpoint:     h.MCPEndpoint(),
		Token:        h.Token,
		RepositoryID: fx.RepositoryID,
		RunID:        fx.RunID,
		Role:         fx.Role,
		Lane:         fx.Lane,
		Mode:         testagent.ModeHappy,
		Artifact: testagent.ArtifactSpec{
			Kind:        fx.ArtifactKind,
			LogicalName: fx.ArtifactLogicalName,
			RepoPath:    fx.ArtifactRepoPath,
		},
		InterrogationWait: 2 * time.Second,
		AfterRegister: func(sessionID string) error {
			return attestFixtureSessionErr(ctx, h.Runner, fx.RepositoryID, fx.RunID, sessionID, fx.Lane)
		},
	}
	res, err := testagent.New(cfg).Run(ctx)
	if err != nil {
		t.Fatalf("fresh replacement agent run: %v", err)
	}
	return res
}

func intFromMap(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// --- chaos fixtures ---------------------------------------------------------

// chaosFixture seeds a repo-write `implement` job whose owning fake-agent
// session has been driven up to (but not through) completion, then returns the
// dead lane's session id and the active lease the fault-injection helper warps.
type chaosFixture struct {
	fx            *Fixture
	deadSessionID string
	jobID         string
}

// seedChaosRepoWriteRun seeds a repo-write implement job (NOT document_only) and
// drives fake-agent-1 to claim + ack it (a lane that then dies). It returns the
// fixture plus the dead session id. The agent does NOT publish or complete — it
// is interrupted by the injected fault, leaving the job in running-limbo with
// zero recoverable artifacts so the auto-publish pass cannot finish it.
func seedChaosRepoWriteRun(t *testing.T, ctx context.Context, h *Harness, repoID string) chaosFixture {
	t.Helper()
	fx := seedRepoWriteFixture(t, ctx, h.Runner, repoID)
	res := driveAgentToClaim(t, ctx, h, fx)
	return chaosFixture{fx: fx, deadSessionID: res.SessionID, jobID: fx.JobID}
}

// seedRepoWriteFixture seeds a validation-passing single-job run whose job is a
// repo_write `implement` job (mode=repo_write + repo_write:true), with a real
// git repo at repoRoot so the work.complete write_scope-clean gate passes. It
// mirrors SeedFixture but for the #121 repo-write acceptance case.
func seedRepoWriteFixture(t *testing.T, ctx context.Context, runner db.Runner, repositoryID string) *Fixture {
	t.Helper()
	repoRoot := t.TempDir()
	initGitRepo(t, repoRoot)

	role := "implementer"
	lane := "claude_code"
	runID := "run_" + repositoryID
	jobID := "job_" + repositoryID

	// Write the expected artifact into the git repo and commit it, so the tree is
	// clean: the fresh replacement agent publishes this same already-tracked,
	// already-committed file, leaving `git status` clean at work.complete.
	artifactRepoPath := "docs/conformance-note.md"
	writeRepoFile(t, repoRoot, artifactRepoPath, fixtureArtifactBody())
	gitCommitAll(t, repoRoot, "seed conformance note")
	artifactAbs := filepath.Join(repoRoot, artifactRepoPath)

	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.repositories (
		  repository_id, repo_identity, repo_root, state_db_path, display_name,
		  registered_at, last_schema_version, state
		) VALUES ($1,$2,$3,$4,'chaos',$5,16,'active')`,
		repositoryID, fixtureRepoIdentity, repoRoot, filepath.Join(repoRoot, ".striatum"), now,
	); err != nil {
		t.Fatalf("seed repository: %v", err)
	}

	// A repo_write write_scope with allowed_paths under docs — the path the
	// artifact lives in, so artifact.publish path checks pass and the
	// write_scope-clean gate sees a clean tree.
	writeScope := map[string]any{
		"mode":          "repo_write",
		"repo_write":    true,
		"allowed_paths": []any{"docs"},
	}
	expectedArtifacts := []any{
		map[string]any{
			"logical_name": "conformance_note",
			"kind":         "progress_note",
			"path":         artifactRepoPath,
			"required":     true,
		},
	}
	workflow := map[string]any{
		"workflow_id": fixtureWorkflowID,
		"roles": map[string]any{
			role: map[string]any{"summary": "implement a small change"},
		},
		"lanes": map[string]any{
			lane: map[string]any{
				"display_model": "Claude",
				"capabilities":  []any{"claim", "write", "interrogate"},
			},
		},
		"jobs": []any{
			map[string]any{
				"id":           fixtureWorkflowJob,
				"interrogable": false,
			},
		},
	}

	seedFixtureRun(t, ctx, runner, repositoryID, runID, workflow)
	seedFixtureJob(t, ctx, runner, repositoryID, runID, jobID, role, lane, writeScope, expectedArtifacts, now)

	return &Fixture{
		RepositoryID:        repositoryID,
		RunID:               runID,
		Role:                role,
		Lane:                lane,
		JobID:               jobID,
		RepoRoot:            repoRoot,
		ArtifactKind:        "progress_note",
		ArtifactLogicalName: "conformance_note",
		ArtifactRepoPath:    artifactRepoPath,
		ArtifactAbsPath:     artifactAbs,
	}
}

// driveAgentToClaim runs a fake agent that claims + acks the job then stops
// before publishing/completing — a genuine live claimant that then dies,
// leaving running-limbo with zero artifacts. ModeNoHeartbeat claims + acks but
// stops before the heartbeat, which is exactly the live-claimant-then-dead shape
// the chaos fault-injection turns into a dead/stalled lane. (C6 HeartbeatMissed
// is not under assertion here; we only need a live claimant.)
func driveAgentToClaim(t *testing.T, ctx context.Context, h *Harness, fx *Fixture) testagent.Result {
	t.Helper()
	res := RunLive(t, ctx, h, fx, "fake_agent", testagent.ModeNoHeartbeat, LiveScope{})
	if res.AgentErr != nil {
		t.Fatalf("drive agent to claim/ack: %v", res.AgentErr)
	}
	if res.AgentResult.JobID == "" || res.AgentResult.LeaseID == "" {
		t.Fatalf("agent did not claim the job: %+v", res.AgentResult)
	}
	return res.AgentResult
}

// --- git helpers (TEST LAYER) ----------------------------------------------

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "chaos@example.test")
	runGit(t, dir, "config", "user.name", "chaos")
	runGit(t, dir, "config", "commit.gpgsign", "false")
}

func gitCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", msg)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeRepoFile(t *testing.T, repoRoot, relPath, body string) {
	t.Helper()
	full := filepath.Join(repoRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

// --- helpers to read DB state ----------------------------------------------

func chaosJobState(t *testing.T, ctx context.Context, h *Harness, jobID string) string {
	t.Helper()
	state, err := h.Observer().JobState(ctx, h.RepositoryID, jobID)
	if err != nil {
		t.Fatalf("read job state %s: %v", jobID, err)
	}
	return state
}

func chaosJobAttempt(t *testing.T, ctx context.Context, h *Harness, jobID string) int {
	t.Helper()
	rows := chaosQuery(t, ctx, h, `SELECT attempt FROM striatumd.jobs WHERE repository_id=$1 AND job_id=$2`,
		h.RepositoryID, jobID)
	if len(rows) != 1 {
		t.Fatalf("expected 1 job row for %s, got %d", jobID, len(rows))
	}
	return intFromMap(rows[0], "attempt")
}

func chaosRunState(t *testing.T, ctx context.Context, h *Harness, runID string) string {
	t.Helper()
	rows := chaosQuery(t, ctx, h, `SELECT state FROM striatumd.runs WHERE repository_id=$1 AND run_id=$2`,
		h.RepositoryID, runID)
	if len(rows) != 1 {
		t.Fatalf("expected 1 run row for %s, got %d", runID, len(rows))
	}
	return fmt.Sprint(rows[0]["state"])
}

func chaosRecoveryExhaustedEscalations(t *testing.T, ctx context.Context, h *Harness, runID string) []map[string]any {
	t.Helper()
	return chaosQuery(t, ctx, h, `
		SELECT ei.escalation_id, ei.state AS ei_state, ei.blocker_kind, ei.payload_json
		  FROM striatumd.escalation_inbox ei
		 WHERE ei.repository_id=$1 AND ei.run_id=$2 AND ei.blocker_kind='recovery_exhausted'`,
		h.RepositoryID, runID)
}

func chaosQuery(t *testing.T, ctx context.Context, h *Harness, sql string, args ...any) []map[string]any {
	t.Helper()
	q, ok := h.Runner.(queryer)
	if !ok {
		t.Fatalf("runner is not a queryer")
	}
	obs := DaemonObserver{Runner: q}
	rows, err := obs.rows(ctx, sql, args...)
	if err != nil {
		t.Fatalf("query: %v\nsql=%s", err, sql)
	}
	return rows
}

// --- chaos scenarios --------------------------------------------------------

// TestChaosDeadRepoWriteLaneSelfRecovers is RFC 0101 acceptance #2 (the #121
// case): a repo-write implement lane claims its job then dies (hard dead pane —
// session closed, lease released, NO artifact). The recovery sweep requeues the
// job on the SAME attempt (no needs_operator, no escalation), and a FRESH fake
// agent claims the requeued packet and completes it through the production
// handlers — the run reaches a terminal job with NO operator action.
func TestChaosDeadRepoWriteLaneSelfRecovers(t *testing.T) {
	repoID := "repo_chaos_dead_repowrite"
	ctx := context.Background()
	h := NewHarness(t, repoID) // SKIPs without STRIATUM_PG_TEST_URL.
	chaos := seedChaosRepoWriteRun(t, ctx, h, repoID)

	priorAttempt := chaosJobAttempt(t, ctx, h, chaos.jobID)

	// INJECT the fault: the lane's pane died and its session closed.
	injectDeadLane(t, ctx, h.Runner, repoID, chaos.deadSessionID, chaos.jobID)

	// Pre-condition: the job is wedged (running-limbo) before the sweep.
	if got := chaosJobState(t, ctx, h, chaos.jobID); got != "running" {
		t.Fatalf("pre-sweep job state = %q, want running (limbo)", got)
	}

	// Trigger the production recovery sweep.
	summary := runSweep(t, ctx, h, chaos.fx.RunID)
	if summary.actedCount != 1 {
		t.Fatalf("sweep acted_count = %d, want 1 (the requeue); result=%#v", summary.actedCount, summary.result)
	}
	if summary.escalationCount != 0 {
		t.Fatalf("sweep raised %d escalations on a FIRST dead-lane recovery, want 0 (self-recover, not escalate)", summary.escalationCount)
	}

	// Self-recovery assertions: requeued on the SAME attempt, no operator.
	if got := chaosJobState(t, ctx, h, chaos.jobID); got != "queued" {
		t.Fatalf("post-sweep job state = %q, want queued (requeued)", got)
	}
	if got := chaosJobAttempt(t, ctx, h, chaos.jobID); got != priorAttempt {
		t.Fatalf("attempt = %d, want %d (autonomous requeue must NOT bump attempt)", got, priorAttempt)
	}
	if got := chaosRunState(t, ctx, h, chaos.fx.RunID); got != "running" {
		t.Fatalf("run state = %q, want running (no operator needed)", got)
	}
	if escs := chaosRecoveryExhaustedEscalations(t, ctx, h, chaos.fx.RunID); len(escs) != 0 {
		t.Fatalf("a self-recovering run raised %d recovery_exhausted escalations, want 0", len(escs))
	}

	// A FRESH agent claims the requeued packet and completes it through the
	// production claim/complete handlers — the self-recovery proof.
	res := freshReplacementAgent(t, ctx, h, chaos.fx)
	if !res.Completed {
		t.Fatalf("fresh replacement agent did not complete the requeued job: %+v", res)
	}
	if got := chaosJobState(t, ctx, h, chaos.jobID); got != "completed" {
		t.Fatalf("post-recovery job state = %q, want completed", got)
	}
	if got := chaosRunState(t, ctx, h, chaos.fx.RunID); got != "completed" {
		t.Fatalf("post-recovery run state = %q, want completed (self-recovered, no operator)", got)
	}
}

// TestChaosStalledLaneSelfRecovers: a lane claims its job then STALLS (the pane
// is alive, the session is still active, but it makes no protocol/PTY/tool
// progress past every deadline — the honest #121 parked agent). The recovery
// sweep transfer-requeues the job AND closes the stalled owning session; a fresh
// agent then completes the requeued packet. Self-recovery, no operator.
func TestChaosStalledLaneSelfRecovers(t *testing.T) {
	repoID := "repo_chaos_stalled"
	ctx := context.Background()
	h := NewHarness(t, repoID)
	chaos := seedChaosRepoWriteRun(t, ctx, h, repoID)

	priorAttempt := chaosJobAttempt(t, ctx, h, chaos.jobID)

	// INJECT the fault: time-warp the still-active session + lease past every
	// liveness deadline so Classify reports it stalled (not dead).
	injectStalledLane(t, ctx, h.Runner, repoID, chaos.deadSessionID, chaos.jobID)

	// Honest-liveness pre-condition: the still-active lane classifies stalled.
	assertSessionClassified(t, ctx, h, chaos.deadSessionID, sessionliveness.ProtocolStalled)

	summary := runSweep(t, ctx, h, chaos.fx.RunID)
	if summary.actedCount != 1 {
		t.Fatalf("sweep acted_count = %d, want 1 (the transfer requeue); result=%#v", summary.actedCount, summary.result)
	}
	if summary.escalationCount != 0 {
		t.Fatalf("first stalled-lane recovery raised %d escalations, want 0", summary.escalationCount)
	}

	// The job is requeued on the SAME attempt and the stalled owner is closed so
	// it cannot wake to double-work.
	if got := chaosJobState(t, ctx, h, chaos.jobID); got != "queued" {
		t.Fatalf("post-sweep job state = %q, want queued (transfer requeued)", got)
	}
	if got := chaosJobAttempt(t, ctx, h, chaos.jobID); got != priorAttempt {
		t.Fatalf("attempt = %d, want %d (transfer requeue must NOT bump attempt)", got, priorAttempt)
	}
	assertSessionState(t, ctx, h, chaos.deadSessionID, "closed")
	if got := chaosRunState(t, ctx, h, chaos.fx.RunID); got != "running" {
		t.Fatalf("run state = %q, want running (no operator needed)", got)
	}

	// A fresh agent completes the transferred job.
	res := freshReplacementAgent(t, ctx, h, chaos.fx)
	if !res.Completed {
		t.Fatalf("fresh replacement agent did not complete the transferred job: %+v", res)
	}
	if got := chaosJobState(t, ctx, h, chaos.jobID); got != "completed" {
		t.Fatalf("post-recovery job state = %q, want completed", got)
	}
	if got := chaosRunState(t, ctx, h, chaos.fx.RunID); got != "completed" {
		t.Fatalf("post-recovery run state = %q, want completed (self-recovered)", got)
	}
}

// TestChaosUnrecoverableLaneEscalatesLoudly is RFC 0101 acceptance #4: a lane
// that dies repeatedly past the per-job requeue budget (default max_requeues=2)
// must, within a bounded number of sweeps, flip the run to needs_operator with a
// pending recovery_exhausted escalation_inbox row carrying a structured
// payload — NEVER a silent 'running'. Each cycle dies a fresh claimant then
// sweeps; after the budget is exhausted the next sweep escalates.
func TestChaosUnrecoverableLaneEscalatesLoudly(t *testing.T) {
	repoID := "repo_chaos_unrecoverable"
	ctx := context.Background()
	h := NewHarness(t, repoID)
	chaos := seedChaosRepoWriteRun(t, ctx, h, repoID)
	runID := chaos.fx.RunID

	// Cycle 1: the seeded lane is already claimed+acked; kill it and sweep ->
	// requeue_count 1. Then for each subsequent cycle a fresh lane claims the
	// requeued job, dies, and the sweep requeues again — until the budget (2) is
	// exhausted and the sweep escalates. We bound the loop so a NON-escalating
	// (silent-wedge) regression fails loudly via the iteration cap rather than
	// hanging.
	const maxCycles = 6 // default max_requeues=2 -> escalate by cycle 3; cap guards a silent wedge.
	escalated := false
	deadSession := chaos.deadSessionID
	for cycle := 1; cycle <= maxCycles; cycle++ {
		injectDeadLane(t, ctx, h.Runner, repoID, deadSession, chaos.jobID)
		summary := runSweep(t, ctx, h, runID)
		if summary.escalationCount > 0 || chaosRunState(t, ctx, h, runID) == "needs_operator" {
			escalated = true
			break
		}
		// Not yet escalated: the job must have been requeued so a fresh lane can
		// re-claim it and die again on the next cycle.
		if got := chaosJobState(t, ctx, h, chaos.jobID); got != "queued" {
			t.Fatalf("cycle %d: job state = %q, want queued (still self-recovering, pre-budget)", cycle, got)
		}
		// A fresh claimant claims + acks then dies before completing.
		res := driveAgentToClaim(t, ctx, h, chaos.fx)
		deadSession = res.SessionID
	}

	if !escalated {
		t.Fatalf("run never escalated after %d dead-lane cycles: a budget-exhausted run sat silently without needs_operator (the silent-wedge regression)", maxCycles)
	}

	// Loud escalation assertions (acceptance #4): run flipped to needs_operator.
	if got := chaosRunState(t, ctx, h, runID); got != "needs_operator" {
		t.Fatalf("run state = %q, want needs_operator after budget exhaustion", got)
	}
	// Exactly one pending recovery_exhausted escalation with a structured payload.
	escs := chaosRecoveryExhaustedEscalations(t, ctx, h, runID)
	if len(escs) != 1 {
		t.Fatalf("recovery_exhausted escalation count = %d, want 1", len(escs))
	}
	esc := escs[0]
	if got := fmt.Sprint(esc["ei_state"]); got != "pending" {
		t.Fatalf("escalation_inbox state = %q, want pending", got)
	}
	payload, ok := esc["payload_json"].(map[string]any)
	if !ok {
		t.Fatalf("escalation payload not a JSON object: %#v", esc["payload_json"])
	}
	if got := fmt.Sprint(payload["schema_version"]); got != "striatum.recovery_escalation.v1" {
		t.Fatalf("payload schema_version = %q, want striatum.recovery_escalation.v1", got)
	}
	if payload["is_escalation"] != true {
		t.Fatalf("payload is_escalation = %v, want true", payload["is_escalation"])
	}
	if got := fmt.Sprint(payload["job_id"]); got != chaos.jobID {
		t.Fatalf("payload job_id = %q, want %s", got, chaos.jobID)
	}
}

// TestChaosHonestLivenessDuringFault is RFC 0101 acceptance #3: the liveness
// classifier reports the TRUE state of an injected fault. A dead pane (closed
// session) is never 'healthy'/working; a stalled-but-active lane classifies
// 'stalled'; and an actively-working lane (just claimed, fresh timestamps) is
// NEVER reported stalled. This guards the honest-liveness contract the whole
// recovery arc keys on — a lie here silently wedges recovery.
func TestChaosHonestLivenessDuringFault(t *testing.T) {
	repoID := "repo_chaos_honest_liveness"
	ctx := context.Background()
	h := NewHarness(t, repoID)
	chaos := seedChaosRepoWriteRun(t, ctx, h, repoID)

	// (1) A freshly-claimed, still-active lane must NOT be stalled or dead — it is
	// genuinely working (active lease + fresh protocol timestamps).
	working := classifySession(t, ctx, h, chaos.deadSessionID)
	if working.Protocol == sessionliveness.ProtocolStalled || working.Protocol == sessionliveness.ProtocolDead {
		t.Fatalf("a freshly-active working lane classified %q; must never be stalled/dead (false-positive wedge)", working.Protocol)
	}

	// (2) Inject a STALL (still active, aged past deadline) -> honestly stalled.
	injectStalledLane(t, ctx, h.Runner, repoID, chaos.deadSessionID, chaos.jobID)
	stalled := classifySession(t, ctx, h, chaos.deadSessionID)
	if stalled.Protocol != sessionliveness.ProtocolStalled {
		t.Fatalf("a stalled-past-deadline active lane classified %q, want %q (a wedge reported as healthy hides the fault)", stalled.Protocol, sessionliveness.ProtocolStalled)
	}

	// (3) A DEAD pane (closed session) is never healthy/working: a closed session
	// classifies inactive (the honest terminal state — never working_*).
	injectDeadLane(t, ctx, h.Runner, repoID, chaos.deadSessionID, chaos.jobID)
	dead := classifySession(t, ctx, h, chaos.deadSessionID)
	if isWorkingProtocol(dead.Protocol) {
		t.Fatalf("a dead/closed pane classified %q (a working_* state); a dead pane must never read healthy", dead.Protocol)
	}
}

// --- liveness assertion helpers --------------------------------------------

func classifySession(t *testing.T, ctx context.Context, h *Harness, sessionID string) sessionliveness.Result {
	t.Helper()
	snap, err := h.Observer().Session(ctx, h.RepositoryID, sessionID, time.Now().UTC())
	if err != nil {
		t.Fatalf("observe session %s: %v", sessionID, err)
	}
	return snap.Classification
}

func assertSessionClassified(t *testing.T, ctx context.Context, h *Harness, sessionID, want string) {
	t.Helper()
	got := classifySession(t, ctx, h, sessionID).Protocol
	if got != want {
		t.Fatalf("session %s classified %q, want %q", sessionID, got, want)
	}
}

func assertSessionState(t *testing.T, ctx context.Context, h *Harness, sessionID, want string) {
	t.Helper()
	rows := chaosQuery(t, ctx, h, `SELECT state FROM striatumd.sessions WHERE repository_id=$1 AND session_id=$2`,
		h.RepositoryID, sessionID)
	if len(rows) != 1 {
		t.Fatalf("expected 1 session row for %s, got %d", sessionID, len(rows))
	}
	if got := fmt.Sprint(rows[0]["state"]); got != want {
		t.Fatalf("session %s state = %q, want %q", sessionID, got, want)
	}
}

func isWorkingProtocol(protocol string) bool {
	switch protocol {
	case sessionliveness.ProtocolWorkingProtocol,
		sessionliveness.ProtocolWorkingLocal,
		sessionliveness.ProtocolWorkingTool,
		sessionliveness.ProtocolLive,
		sessionliveness.ProtocolQuiet:
		return true
	default:
		return false
	}
}
