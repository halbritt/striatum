package recovery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/reads"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type sweepCursorFakeRunner struct {
	queries  []string
	latch    map[string]any
	queryErr error
	execSQL  string
	execArgs []any
}

func (r *sweepCursorFakeRunner) Exec(_ context.Context, sql string, args ...any) error {
	r.execSQL = sql
	r.execArgs = args
	return nil
}

func (r *sweepCursorFakeRunner) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	r.queries = append(r.queries, sql)
	if r.queryErr != nil {
		return nil, r.queryErr
	}
	return sweepCursorRowsFromMaps([]map[string]any{r.latch}), nil
}

func (r *sweepCursorFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return sweepBreakerFakeRow{err: pgx.ErrNoRows}
}

func (r *sweepCursorFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("not implemented")
}

func (r *sweepCursorFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("not implemented")
}

func TestUpsertSchedulerCursorAddsReadSideLatch(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_recovery_cursor_latch"
	runID := "run_recovery_cursor_latch"
	toolAdvancedAt := time.Date(2026, 6, 17, 22, 30, 0, 0, time.UTC)
	ptyOnlyAt := toolAdvancedAt.Add(10 * time.Minute)
	seedRecoveryCursorLatchFixture(t, ctx, runner, repoID, runID, toolAdvancedAt, ptyOnlyAt)

	if _, err := upsertSchedulerCursor(ctx, runner, repoID, runID, map[string]any{"status": "ok"}, "active"); err != nil {
		t.Fatalf("upsertSchedulerCursor: %v", err)
	}

	var status string
	var claimableJobCount int
	var lastLaneAdvancedAt string
	var hasLatchError bool
	if err := runner.QueryRow(ctx, `
		SELECT last_result_json->>'status',
		       (last_result_json->>'claimable_job_count')::integer,
		       last_result_json->>'last_lane_advanced_at',
		       last_result_json ? 'recovery_cursor_latch_error'
		  FROM striatumd.scheduler_cursors
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND cursor_kind = 'recovery'`,
		repoID, runID).Scan(&status, &claimableJobCount, &lastLaneAdvancedAt, &hasLatchError); err != nil {
		t.Fatalf("read scheduler cursor: %v", err)
	}
	if status != "ok" {
		t.Fatalf("status = %q, want ok", status)
	}
	if claimableJobCount != 1 {
		t.Fatalf("claimable_job_count = %d, want 1", claimableJobCount)
	}
	if lastLaneAdvancedAt != toolAdvancedAt.Format(time.RFC3339) {
		t.Fatalf("last_lane_advanced_at = %q, want tool timestamp %s, not fresher PTY timestamp %s", lastLaneAdvancedAt, toolAdvancedAt.Format(time.RFC3339), ptyOnlyAt.Format(time.RFC3339))
	}
	if hasLatchError {
		t.Fatalf("cursor result should not contain recovery_cursor_latch_error")
	}
}

func seedRecoveryCursorLatchFixture(t *testing.T, ctx context.Context, runner db.Runner, repoID string, runID string, toolAdvancedAt time.Time, ptyOnlyAt time.Time) {
	t.Helper()
	now := toolAdvancedAt.Add(-time.Hour)
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.repositories (
		  repository_id, repo_identity, repo_root, state_db_path, display_name,
		  registered_at, last_schema_version, state
		) VALUES ($1,$2,$3,$4,'repo',$5,28,'active')`,
		repoID, "ident_"+repoID, "/tmp/"+repoID, "/tmp/"+repoID+"/.striatum", now); err != nil {
		t.Fatalf("insert repository: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.workflow_snapshots (
		  repository_id, workflow_snapshot_id, workflow_id, content_sha256, workflow_json, loaded_at
		) VALUES ($1,$2,'wf','sha','{}'::jsonb,$3)`, repoID, "snap_"+runID, now); err != nil {
		t.Fatalf("insert workflow snapshot: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.runs (
		  repository_id, run_id, workflow_snapshot_id, repo_root, state, created_at, started_at
		) VALUES ($1,$2,$3,$4,'running',$5,$5)`,
		repoID, runID, "snap_"+runID, "/tmp/"+repoID, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.sessions (
		  repository_id, session_id, run_id, role_id, lane_id, slug, ordinal, state,
		  registered_at, last_tool_call_started_at, last_pty_activity_at
		) VALUES ($1,'sess_1',$2,'builder','local','builder-local-1',1,'active',$3,$4,$5)`,
		repoID, runID, now, toolAdvancedAt, ptyOnlyAt); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, title, job_type, role_id,
		  state, attempt, max_attempts, idempotency_key, created_at
		) VALUES ($1,'job_1',$2,'build','Build','build','builder','running',1,1,'idem_1',$3)`,
		repoID, runID, now); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.queue_messages (
		  repository_id, message_id, run_id, job_id, kind, state, priority,
		  target_role_id, target_lane_id, created_at, updated_at
		) VALUES ($1,'msg_1',$2,'job_1','work','pending',0,'builder','local',$3,$3)`,
		repoID, runID, now); err != nil {
		t.Fatalf("insert queue message: %v", err)
	}
}

func TestUpsertSchedulerCursorRecordsLatchReadFailure(t *testing.T) {
	runner := &sweepCursorFakeRunner{queryErr: errors.New("latch read failed")}

	if _, err := upsertSchedulerCursor(context.Background(), runner, "repo_1", "run_1", map[string]any{"status": "ok"}, "sweep_degraded"); err != nil {
		t.Fatalf("upsertSchedulerCursor should not fail on latch read error: %v", err)
	}
	result, ok := runner.execArgs[2].(map[string]any)
	if !ok {
		t.Fatalf("cursor result arg = %#v, want map", runner.execArgs[2])
	}
	if result["status"] != "ok" {
		t.Fatalf("existing result field lost: %#v", result)
	}
	if result["claimable_job_count"] != 0 {
		t.Fatalf("claimable_job_count = %#v, want default 0 on read failure", result["claimable_job_count"])
	}
	if result["last_lane_advanced_at"] != nil {
		t.Fatalf("last_lane_advanced_at = %#v, want nil on read failure", result["last_lane_advanced_at"])
	}
	if got := result["recovery_cursor_latch_error"]; got != "latch read failed" {
		t.Fatalf("recovery_cursor_latch_error = %#v, want latch read failed", got)
	}
}

type sweepCursorFakeRows struct {
	fields []string
	rows   [][]any
	index  int
}

func sweepCursorRowsFromMaps(items []map[string]any) pgx.Rows {
	fields := []string{}
	if len(items) > 0 {
		fields = append(fields, "claimable_job_count", "last_lane_advanced_at")
	}
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		row := make([]any, 0, len(fields))
		for _, field := range fields {
			row = append(row, item[field])
		}
		rows = append(rows, row)
	}
	return &sweepCursorFakeRows{fields: fields, rows: rows, index: -1}
}

func (r *sweepCursorFakeRows) Close() {}

func (r *sweepCursorFakeRows) Err() error { return nil }

func (r *sweepCursorFakeRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }

func (r *sweepCursorFakeRows) FieldDescriptions() []pgconn.FieldDescription {
	out := make([]pgconn.FieldDescription, 0, len(r.fields))
	for _, field := range r.fields {
		out = append(out, pgconn.FieldDescription{Name: field})
	}
	return out
}

func (r *sweepCursorFakeRows) Next() bool {
	r.index++
	return r.index < len(r.rows)
}

func (r *sweepCursorFakeRows) Scan(dest ...any) error {
	values, err := r.Values()
	if err != nil {
		return err
	}
	for i := range dest {
		if i >= len(values) {
			break
		}
		switch target := dest[i].(type) {
		case *int64:
			switch value := values[i].(type) {
			case int:
				*target = int64(value)
			case int64:
				*target = value
			}
		case **time.Time:
			if value, ok := values[i].(time.Time); ok {
				copied := value
				*target = &copied
			} else {
				*target = nil
			}
		default:
			return errors.New("unsupported fake rows scan target")
		}
	}
	return nil
}

func (r *sweepCursorFakeRows) Values() ([]any, error) {
	if r.index < 0 || r.index >= len(r.rows) {
		return nil, errors.New("no current row")
	}
	return r.rows[r.index], nil
}

func (r *sweepCursorFakeRows) RawValues() [][]byte { return nil }

func (r *sweepCursorFakeRows) Conn() *pgx.Conn { return nil }

type sweepBreakerFakeRow struct {
	count     int
	tripState string
	runState  string
	err       error
}

func (r sweepBreakerFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *int:
			*target = r.count
		case *string:
			switch i {
			case 1:
				*target = r.tripState
			case 2:
				*target = r.runState
			default:
				*target = ""
			}
		default:
			return errors.New("unsupported breaker row scan target")
		}
	}
	return nil
}

func TestActiveRunSweepTripsPoisonRunAndRecoversAfterResolve(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_recovery_sweep_trip"
	poisonRunID := "run_recovery_sweep_trip_poison"
	healthyRunID := "run_recovery_sweep_trip_healthy"
	seedRecoverySweepTripFixture(t, ctx, runner, repoID, poisonRunID, healthyRunID)

	visited := map[string]int{}
	sweep := ActiveRunSweep{
		Runner: runner,
		Author: "test",
		sweepRun: func(_ context.Context, _ db.Runner, _ string, runID string, _ string) (map[string]any, error) {
			visited[runID]++
			if runID == poisonRunID {
				return nil, errors.New("synthetic per-run recovery panic/error")
			}
			return map[string]any{"status": "ok"}, nil
		},
	}

	for i := 0; i < recoverySweepTripThreshold; i++ {
		if _, err := sweep.SweepOnce(ctx); err != nil {
			t.Fatalf("sweep tick %d: %v", i+1, err)
		}
	}
	if visited[poisonRunID] != recoverySweepTripThreshold {
		t.Fatalf("poison run visits before trip = %d, want %d", visited[poisonRunID], recoverySweepTripThreshold)
	}
	if visited[healthyRunID] != recoverySweepTripThreshold {
		t.Fatalf("healthy run visits before trip = %d, want %d", visited[healthyRunID], recoverySweepTripThreshold)
	}
	if got := recoverySweepRunState(t, ctx, runner, repoID, poisonRunID); got != "needs_operator" {
		t.Fatalf("poison run state = %q, want needs_operator", got)
	}
	escalationID := recoverySweepTripEscalationID(t, ctx, runner, repoID, poisonRunID)
	if escalationID == "" {
		t.Fatal("trip did not create a recovery_exhausted escalation")
	}

	doctor, err := reads.HandleDoctor(ctx, runner, rpc.Envelope{
		Params: map[string]any{"repository_id": repoID, "verbose": true},
	})
	if err != nil {
		t.Fatalf("doctor after trip: %v", err)
	}
	problems := strings.Join(doctor["problems"].([]string), "\n")
	if !strings.Contains(problems, "recovery_sweep_cursor_tripped."+poisonRunID) {
		t.Fatalf("doctor problems missing recovery_sweep_cursor_tripped:\n%s", problems)
	}
	if !strings.Contains(problems, "striatum escalation resolve "+escalationID) {
		t.Fatalf("doctor problems missing recovery command for %s:\n%s", escalationID, problems)
	}

	if _, err := sweep.SweepOnce(ctx); err != nil {
		t.Fatalf("post-trip sweep: %v", err)
	}
	if visited[poisonRunID] != recoverySweepTripThreshold {
		t.Fatalf("poison run was swept after trip; visits=%d want still %d", visited[poisonRunID], recoverySweepTripThreshold)
	}
	if visited[healthyRunID] != recoverySweepTripThreshold+1 {
		t.Fatalf("healthy run did not continue after poison trip; visits=%d", visited[healthyRunID])
	}

	resolved, err := reads.HandleEscalationResolve(ctx, runner, rpc.Envelope{
		Params: map[string]any{
			"repository_id":   repoID,
			"escalation_id":   escalationID,
			"resolution_note": "fixed the sweep poison input",
		},
	})
	if err != nil {
		t.Fatalf("resolve trip escalation: %v", err)
	}
	if resolved["status"] != "resolved" {
		t.Fatalf("resolve status = %#v, want resolved", resolved["status"])
	}
	if got := recoverySweepRunState(t, ctx, runner, repoID, poisonRunID); got != "running" {
		t.Fatalf("poison run state after resolve = %q, want running", got)
	}

	if _, err := sweep.SweepOnce(ctx); err != nil {
		t.Fatalf("post-resolve sweep: %v", err)
	}
	if visited[poisonRunID] != recoverySweepTripThreshold+1 {
		t.Fatalf("poison run was not swept after resolve; visits=%d", visited[poisonRunID])
	}
	if got := recoverySweepRunState(t, ctx, runner, repoID, poisonRunID); got != "running" {
		t.Fatalf("poison run re-tripped immediately after resolve; state=%q", got)
	}
	count, state := recoverySweepCursorBreaker(t, ctx, runner, repoID, poisonRunID)
	if count != 1 || state != "armed" {
		t.Fatalf("breaker after resolve+one failure = count %d state %q, want count 1 state armed", count, state)
	}
}

func seedRecoverySweepTripFixture(t *testing.T, ctx context.Context, runner db.Runner, repoID string, runIDs ...string) {
	t.Helper()
	now := time.Date(2026, 7, 1, 20, 0, 0, 0, time.UTC)
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.repositories (
		  repository_id, repo_identity, repo_root, state_db_path, display_name,
		  registered_at, last_schema_version, state
		) VALUES ($1,$2,$3,$4,'repo',$5,28,'active')`,
		repoID, "ident_"+repoID, "/tmp/"+repoID, "/tmp/"+repoID+"/.striatum", now); err != nil {
		t.Fatalf("insert repository: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.workflow_snapshots (
		  repository_id, workflow_snapshot_id, workflow_id, content_sha256, workflow_json, loaded_at
		) VALUES ($1,$2,'wf','sha','{}'::jsonb,$3)`, repoID, "snap_"+repoID, now); err != nil {
		t.Fatalf("insert workflow snapshot: %v", err)
	}
	for i, runID := range runIDs {
		createdAt := now.Add(time.Duration(i) * time.Second)
		if err := runner.Exec(ctx, `
			INSERT INTO striatumd.runs (
			  repository_id, run_id, workflow_snapshot_id, repo_root, state, created_at, started_at
			) VALUES ($1,$2,$3,$4,'running',$5,$5)`,
			repoID, runID, "snap_"+repoID, "/tmp/"+repoID, createdAt); err != nil {
			t.Fatalf("insert run %s: %v", runID, err)
		}
	}
}

func recoverySweepRunState(t *testing.T, ctx context.Context, runner db.Runner, repoID string, runID string) string {
	t.Helper()
	var state string
	if err := runner.QueryRow(ctx, `
		SELECT state FROM striatumd.runs
		 WHERE repository_id = $1 AND run_id = $2`,
		repoID, runID).Scan(&state); err != nil {
		t.Fatalf("read run state %s: %v", runID, err)
	}
	return state
}

func recoverySweepTripEscalationID(t *testing.T, ctx context.Context, runner db.Runner, repoID string, runID string) string {
	t.Helper()
	var escalationID string
	err := runner.QueryRow(ctx, `
		SELECT escalation_id
		  FROM striatumd.escalation_inbox
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND blocker_kind = 'recovery_exhausted'
		   AND payload_json->>'source' = $3
		   AND state = 'pending'
		 ORDER BY created_at, escalation_id
		 LIMIT 1`,
		repoID, runID, recoverySweepTripSource).Scan(&escalationID)
	if err != nil {
		t.Fatalf("read trip escalation: %v", err)
	}
	return escalationID
}

func recoverySweepCursorBreaker(t *testing.T, ctx context.Context, runner db.Runner, repoID string, runID string) (int, string) {
	t.Helper()
	var count int
	var state string
	if err := runner.QueryRow(ctx, `
		SELECT (last_result_json->>'recovery_degraded_sweep_count')::integer,
		       last_result_json->>'recovery_sweep_trip_state'
		  FROM striatumd.scheduler_cursors
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND cursor_kind = 'recovery'`,
		repoID, runID).Scan(&count, &state); err != nil {
		t.Fatalf("read breaker cursor: %v", err)
	}
	return count, state
}
