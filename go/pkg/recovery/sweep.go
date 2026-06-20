package recovery

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/mutations"
	"github.com/jackc/pgx/v5"
)

type queryRunner interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// perRunSweepFunc is the per-run unit of work both background sweeps invoke
// (mutations.SweepRun / mutations.SchedulerSpawnOnce share this signature). It
// is a field on the sweep structs so a test can inject a panicking unit, and so
// runPerRunSweep can wrap it in a recover.
type perRunSweepFunc func(ctx context.Context, runner db.Runner, repositoryID, runID, author string) (map[string]any, error)

// runPerRunSweep invokes one per-run sweep unit and converts a recovered panic
// into an error so a single poison run degrades that run's cursor (the existing
// error path) instead of unwinding past the sweep loop and crashing the single-
// writer daemon (FMA-001 / issue #451). The recover is scoped to ONE run: a
// panic degrades that run and the caller continues to the next run. The panic +
// stack are logged loud at error level so the fault stays visible, mirroring the
// daemon's prior goroutine-level panic log without the fatal re-raise.
func runPerRunSweep(ctx context.Context, fn perRunSweepFunc, runner db.Runner, repositoryID, runID, author string) (result map[string]any, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovery sweep panic recovered (repository_id=%s run_id=%s); degrading this run and continuing: panic=%v\n%s", repositoryID, runID, r, debug.Stack())
			result = nil
			err = fmt.Errorf("sweep panic recovered: %v", r)
		}
	}()
	return fn(ctx, runner, repositoryID, runID, author)
}

type ActiveRunSweep struct {
	Runner db.Runner
	Author string

	// sweepRun is the per-run unit; nil means use mutations.SweepRun. It is set
	// only in tests to inject a panicking or failing unit.
	sweepRun perRunSweepFunc
}

func (s ActiveRunSweep) SweepOnce(ctx context.Context) (map[string]any, error) {
	if s.Runner == nil {
		return nil, fmt.Errorf("recovery sweep requires daemon PostgreSQL")
	}
	queryer, ok := s.Runner.(queryRunner)
	if !ok {
		return nil, fmt.Errorf("recovery sweep runner does not support queries")
	}
	author := s.Author
	if author == "" {
		author = "striatumd-go"
	}

	rows, err := queryer.Query(ctx, `
		SELECT r.repository_id, runs.run_id
		  FROM striatumd.repositories r
		  JOIN striatumd.runs runs
		    ON runs.repository_id = r.repository_id
		 WHERE r.state = 'active'
		   AND runs.state IN ('running', 'paused')
		 ORDER BY r.repository_id, runs.created_at, runs.run_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type activeRun struct {
		repositoryID string
		runID        string
	}
	activeRuns := []activeRun{}
	for rows.Next() {
		var row activeRun
		if err := rows.Scan(&row.repositoryID, &row.runID); err != nil {
			return nil, err
		}
		activeRuns = append(activeRuns, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sweepRun := s.sweepRun
	if sweepRun == nil {
		sweepRun = mutations.SweepRun
	}

	sweeps := []map[string]any{}
	for _, run := range activeRuns {
		result, err := runPerRunSweep(ctx, sweepRun, s.Runner, run.repositoryID, run.runID, author)
		if err != nil {
			result = map[string]any{"error": err.Error()}
			if cursorErr := upsertSchedulerCursor(ctx, s.Runner, run.repositoryID, run.runID, result, "sweep_degraded"); cursorErr != nil {
				return nil, cursorErr
			}
			sweeps = append(sweeps, map[string]any{
				"repository_id": run.repositoryID,
				"run_id":        run.runID,
				"error":         err.Error(),
			})
			continue
		}
		if err := upsertSchedulerCursor(ctx, s.Runner, run.repositoryID, run.runID, result, "active"); err != nil {
			return nil, err
		}
		sweeps = append(sweeps, map[string]any{
			"repository_id": run.repositoryID,
			"run_id":        run.runID,
			"result":        result,
		})
	}
	return map[string]any{"mode": "daemon", "sweeps": sweeps}, nil
}

// AutoSpawnSweep is the daemon-side supervision.auto_spawn scheduler (RFC 0122):
// each tick it reconciles every running run that holds an active spawn-
// authorization grant, registering + supervising the queued auto_spawn lanes
// under the captured run owner. It is the standing process that finally removes
// the operator (model AND credential) from the spawn loop — the residual
// operator-side `run drive` could not close (RFC 0122 §Problem). Like
// ActiveRunSweep it runs on the resident scheduler interval and records a per-run
// scheduler cursor so a degraded run is visible, not silent.
type AutoSpawnSweep struct {
	Runner db.Runner
	Author string

	// spawnRun is the per-run unit; nil means use mutations.SchedulerSpawnOnce.
	// It is set only in tests to inject a panicking or failing unit.
	spawnRun perRunSweepFunc
}

func (s AutoSpawnSweep) SweepOnce(ctx context.Context) (map[string]any, error) {
	if s.Runner == nil {
		return nil, fmt.Errorf("auto_spawn sweep requires daemon PostgreSQL")
	}
	queryer, ok := s.Runner.(queryRunner)
	if !ok {
		return nil, fmt.Errorf("auto_spawn sweep runner does not support queries")
	}
	author := s.Author
	if author == "" {
		author = "striatumd-go"
	}

	// Only runs with an ACTIVE grant are candidates — a run with no grant is not
	// auto_spawn-authorized and must never be touched by the scheduler (C2/C6).
	rows, err := queryer.Query(ctx, `
		SELECT DISTINCT r.repository_id, runs.run_id, runs.created_at
		  FROM striatumd.repositories r
		  JOIN striatumd.runs runs
		    ON runs.repository_id = r.repository_id
		  JOIN striatumd.spawn_authorization_grants g
		    ON g.repository_id = runs.repository_id
		   AND g.run_id = runs.run_id
		   AND g.revoked_at IS NULL
		 WHERE r.state = 'active'
		   AND runs.state = 'running'
		 ORDER BY r.repository_id, runs.created_at, runs.run_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		repositoryID string
		runID        string
	}
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		var createdAt any
		if err := rows.Scan(&c.repositoryID, &c.runID, &createdAt); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	spawnRun := s.spawnRun
	if spawnRun == nil {
		spawnRun = mutations.SchedulerSpawnOnce
	}

	sweeps := []map[string]any{}
	for _, c := range candidates {
		result, err := runPerRunSweep(ctx, spawnRun, s.Runner, c.repositoryID, c.runID, author)
		if err != nil {
			// A poisoned spawn (expired/missing grant, run-as failure) is recorded
			// loud as a degraded cursor + surfaced in the sweep result; the scheduler
			// never silently falls back (C2).
			result = map[string]any{"error": err.Error()}
			if cursorErr := upsertAutoSpawnCursor(ctx, s.Runner, c.repositoryID, c.runID, result, "spawn_degraded"); cursorErr != nil {
				return nil, cursorErr
			}
			sweeps = append(sweeps, map[string]any{
				"repository_id": c.repositoryID,
				"run_id":        c.runID,
				"error":         err.Error(),
			})
			continue
		}
		if err := upsertAutoSpawnCursor(ctx, s.Runner, c.repositoryID, c.runID, result, "active"); err != nil {
			return nil, err
		}
		sweeps = append(sweeps, map[string]any{
			"repository_id": c.repositoryID,
			"run_id":        c.runID,
			"result":        result,
		})
	}
	return map[string]any{"mode": "auto_spawn", "sweeps": sweeps}, nil
}

func upsertAutoSpawnCursor(ctx context.Context, runner db.Runner, repositoryID string, runID string, result map[string]any, state string) error {
	resultArg, err := db.JSONBArg(runner, result)
	if err != nil {
		return err
	}
	return runner.Exec(ctx, `
		INSERT INTO striatumd.scheduler_cursors(
		  repository_id, run_id, cursor_kind, last_sweep_at,
		  next_sweep_after, last_result_json, state
		)
		VALUES ($1, $2, 'auto_spawn', now(), NULL, $3::jsonb, $4)
		ON CONFLICT (repository_id, run_id, cursor_kind)
		DO UPDATE SET last_sweep_at = now(),
		              next_sweep_after = NULL,
		              last_result_json = EXCLUDED.last_result_json,
		              state = EXCLUDED.state`,
		repositoryID, runID, resultArg, state)
}

func upsertSchedulerCursor(ctx context.Context, runner db.Runner, repositoryID string, runID string, result map[string]any, state string) error {
	latched := recoveryCursorResultWithLatch(ctx, runner, repositoryID, runID, result)
	resultArg, err := db.JSONBArg(runner, latched)
	if err != nil {
		return err
	}
	return runner.Exec(ctx, `
		INSERT INTO striatumd.scheduler_cursors(
		  repository_id, run_id, cursor_kind, last_sweep_at,
		  next_sweep_after, last_result_json, state
		)
		VALUES ($1, $2, 'recovery', now(), NULL, $3::jsonb, $4)
		ON CONFLICT (repository_id, run_id, cursor_kind)
		DO UPDATE SET last_sweep_at = now(),
		              next_sweep_after = NULL,
		              last_result_json = EXCLUDED.last_result_json,
		              state = EXCLUDED.state`,
		repositoryID, runID, resultArg, state)
}

func recoveryCursorResultWithLatch(ctx context.Context, runner db.Runner, repositoryID string, runID string, result map[string]any) map[string]any {
	latched := map[string]any{}
	for key, value := range result {
		latched[key] = value
	}
	latch, err := readRecoveryCursorLatch(ctx, runner, repositoryID, runID)
	if err != nil {
		latched["claimable_job_count"] = 0
		latched["last_lane_advanced_at"] = nil
		latched["recovery_cursor_latch_error"] = err.Error()
		return latched
	}
	for key, value := range latch {
		latched[key] = value
	}
	return latched
}

func readRecoveryCursorLatch(ctx context.Context, runner db.Runner, repositoryID string, runID string) (map[string]any, error) {
	queryer, ok := runner.(queryRunner)
	if !ok {
		return nil, fmt.Errorf("recovery cursor latch runner does not support queries")
	}
	rows, err := queryer.Query(ctx, `
		SELECT (
		         SELECT COUNT(DISTINCT q.job_id)
		           FROM striatumd.queue_messages q
		           JOIN striatumd.jobs j
		             ON j.repository_id = q.repository_id
		            AND j.job_id = q.job_id
		          WHERE q.repository_id = $1
		            AND q.run_id = $2
		            AND q.kind = 'work'
		            AND q.state = 'pending'
		            AND (q.visible_after IS NULL OR q.visible_after <= now())
		       ) AS claimable_job_count,
		       (
		         SELECT MAX(activity_at)
		           FROM (
		             SELECT e.created_at AS activity_at
		               FROM striatumd.events e
		              WHERE e.repository_id = $1
		                AND e.run_id = $2
		                AND e.actor_session_id IS NOT NULL
		                AND e.event_type IN (
		                  'queue.claimed',
		                  'artifact.published',
		                  'verdict.recorded',
		                  'job.completed'
		                )
		             UNION ALL SELECT s.last_mcp_request_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_tools_list_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_await_packet_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_packet_delivered_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_ack_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_work_block_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_work_release_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_work_complete_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_session_ready_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_session_question_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_session_escalate_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_tool_call_started_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		             UNION ALL SELECT s.last_tool_call_finished_at FROM striatumd.sessions s WHERE s.repository_id = $1 AND s.run_id = $2
		           ) activity
		          WHERE activity_at IS NOT NULL
		       ) AS last_lane_advanced_at`,
		repositoryID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return map[string]any{
			"claimable_job_count":   0,
			"last_lane_advanced_at": nil,
		}, rows.Err()
	}
	var claimableJobCount int64
	var lastLaneAdvancedAt *time.Time
	if err := rows.Scan(&claimableJobCount, &lastLaneAdvancedAt); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := map[string]any{
		"claimable_job_count":   int(claimableJobCount),
		"last_lane_advanced_at": nil,
	}
	if lastLaneAdvancedAt != nil {
		result["last_lane_advanced_at"] = lastLaneAdvancedAt.UTC().Format(time.RFC3339)
	}
	return result, nil
}
