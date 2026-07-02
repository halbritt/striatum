package recovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"runtime/debug"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/mutations"
	"github.com/jackc/pgx/v5"
)

const (
	recoverySweepTripThreshold = 3
	recoverySweepTripSource    = "recovery.sweep_trip_latch"
	recoveryExhaustedKind      = "recovery_exhausted"
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
			sweepErr := err
			result = map[string]any{"error": sweepErr.Error()}
			cursorUpdate := recoveryCursorUpdate(ctx, s.Runner, run.repositoryID, run.runID, result, "sweep_degraded")
			var trip recoverySweepTrip
			if cursorUpdate.tripped {
				var tripErr error
				trip, tripErr = tripRecoverySweepBreaker(ctx, s.Runner, run.repositoryID, run.runID, sweepErr.Error(), cursorUpdate.degradedCount, cursorUpdate.tripThreshold, author)
				if tripErr != nil {
					return nil, tripErr
				}
				cursorUpdate.result["recovery_sweep_trip_escalation_id"] = trip.escalationID
				cursorUpdate.result["recovery_sweep_trip_recovery_command"] = trip.recoveryCommand
			}
			if cursorErr := writeSchedulerCursor(ctx, s.Runner, run.repositoryID, run.runID, cursorUpdate.result, "sweep_degraded"); cursorErr != nil {
				return nil, cursorErr
			}
			entry := map[string]any{
				"repository_id": run.repositoryID,
				"run_id":        run.runID,
				"error":         sweepErr.Error(),
			}
			if cursorUpdate.degradedCount > 0 {
				entry["degraded_sweep_count"] = cursorUpdate.degradedCount
			}
			if cursorUpdate.tripped {
				entry["tripped"] = true
				entry["recovery_command"] = trip.recoveryCommand
				entry["escalation_id"] = trip.escalationID
			}
			sweeps = append(sweeps, entry)
			continue
		}
		if _, err := upsertSchedulerCursor(ctx, s.Runner, run.repositoryID, run.runID, result, "active"); err != nil {
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

type schedulerCursorUpdate struct {
	result        map[string]any
	degradedCount int
	tripped       bool
	tripThreshold int
}

type recoverySweepBreakerState struct {
	degradedCount int
	tripState     string
	runState      string
}

type recoverySweepTrip struct {
	escalationID    string
	recoveryCommand string
}

func upsertSchedulerCursor(ctx context.Context, runner db.Runner, repositoryID string, runID string, result map[string]any, state string) (schedulerCursorUpdate, error) {
	update := recoveryCursorUpdate(ctx, runner, repositoryID, runID, result, state)
	return update, writeSchedulerCursor(ctx, runner, repositoryID, runID, update.result, state)
}

func writeSchedulerCursor(ctx context.Context, runner db.Runner, repositoryID string, runID string, result map[string]any, state string) error {
	resultArg, err := db.JSONBArg(runner, result)
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

func recoveryCursorUpdate(ctx context.Context, runner db.Runner, repositoryID string, runID string, result map[string]any, state string) schedulerCursorUpdate {
	latched := map[string]any{}
	for key, value := range result {
		latched[key] = value
	}
	update := schedulerCursorUpdate{
		result:        latched,
		tripThreshold: recoverySweepTripThreshold,
	}
	latch, err := readRecoveryCursorLatch(ctx, runner, repositoryID, runID)
	if err != nil {
		latched["claimable_job_count"] = 0
		latched["last_lane_advanced_at"] = nil
		latched["recovery_cursor_latch_error"] = err.Error()
	} else {
		for key, value := range latch {
			latched[key] = value
		}
	}
	if state != "sweep_degraded" {
		return update
	}
	breaker, err := readRecoverySweepBreakerState(ctx, runner, repositoryID, runID)
	if err != nil {
		latched["recovery_sweep_breaker_error"] = err.Error()
	}
	degradedCount := breaker.degradedCount + 1
	if degradedCount < 1 {
		degradedCount = 1
	}
	update.degradedCount = degradedCount
	latched["recovery_degraded_sweep_count"] = degradedCount
	latched["recovery_sweep_trip_threshold"] = recoverySweepTripThreshold
	latched["recovery_sweep_trip_state"] = "armed"
	if errorText, ok := result["error"].(string); ok && errorText != "" {
		latched["recovery_sweep_trip_error"] = truncateRecoverySweepText(errorText, 1000)
	}
	if degradedCount >= recoverySweepTripThreshold {
		update.tripped = true
		latched["recovery_sweep_trip_state"] = "tripped"
		latched["recovery_sweep_tripped_at"] = recoverySweepNow()
	}
	return update
}

func readRecoverySweepBreakerState(ctx context.Context, runner db.Runner, repositoryID string, runID string) (recoverySweepBreakerState, error) {
	var count int
	var tripState string
	var runState string
	err := runner.QueryRow(ctx, `
		SELECT CASE
		         WHEN COALESCE(c.last_result_json->>'recovery_degraded_sweep_count', '') ~ '^[0-9]+$'
		         THEN (c.last_result_json->>'recovery_degraded_sweep_count')::integer
		         ELSE 0
		       END AS recovery_degraded_sweep_count,
		       COALESCE(c.last_result_json->>'recovery_sweep_trip_state', '') AS recovery_sweep_trip_state,
		       COALESCE(r.state, '') AS run_state
		  FROM striatumd.scheduler_cursors c
		  LEFT JOIN striatumd.runs r
		    ON r.repository_id = c.repository_id
		   AND r.run_id = c.run_id
		 WHERE c.repository_id = $1
		   AND c.run_id = $2
		   AND c.cursor_kind = 'recovery'`,
		repositoryID, runID).Scan(&count, &tripState, &runState)
	if err != nil {
		if err == pgx.ErrNoRows {
			return recoverySweepBreakerState{}, nil
		}
		return recoverySweepBreakerState{}, err
	}
	if count < 0 {
		count = 0
	}
	if tripState == "tripped" && (runState == "running" || runState == "paused") {
		count = 0
	}
	return recoverySweepBreakerState{
		degradedCount: count,
		tripState:     tripState,
		runState:      runState,
	}, nil
}

func tripRecoverySweepBreaker(ctx context.Context, runner db.Runner, repositoryID string, runID string, errorText string, degradedCount int, threshold int, author string) (recoverySweepTrip, error) {
	tx, err := db.BeginAuthorizedMutation(ctx, runner, db.AuthorityFromContext(ctx))
	if err != nil {
		return recoverySweepTrip{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()
	if err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "striatum:run:"+repositoryID+":"+runID); err != nil {
		return recoverySweepTrip{}, err
	}

	var existingBlockerID string
	err = tx.QueryRow(ctx, `
		SELECT blocker_id
		  FROM striatumd.blockers
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND blocker_kind = $3
		   AND state = 'open'
		   AND payload_json->>'source' = $4
		 ORDER BY created_at, blocker_id
		 LIMIT 1`,
		repositoryID, runID, recoveryExhaustedKind, recoverySweepTripSource).Scan(&existingBlockerID)
	if err != nil && err != pgx.ErrNoRows {
		return recoverySweepTrip{}, err
	}
	if existingBlockerID != "" {
		if err := tx.Exec(ctx, `
			UPDATE striatumd.runs
			   SET state = 'needs_operator'
			 WHERE repository_id = $1
			   AND run_id = $2
			   AND state IN ('running','paused')`,
			repositoryID, runID); err != nil {
			return recoverySweepTrip{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return recoverySweepTrip{}, err
		}
		committed = true
		return recoverySweepTrip{
			escalationID:    existingBlockerID,
			recoveryCommand: "striatum escalation resolve " + existingBlockerID,
		}, nil
	}

	var runState string
	if err := tx.QueryRow(ctx, `
		SELECT state
		  FROM striatumd.runs
		 WHERE repository_id = $1 AND run_id = $2
		 FOR UPDATE`,
		repositoryID, runID).Scan(&runState); err != nil {
		return recoverySweepTrip{}, err
	}
	if runState != "running" && runState != "paused" && runState != "needs_operator" {
		if err := tx.Commit(ctx); err != nil {
			return recoverySweepTrip{}, err
		}
		committed = true
		return recoverySweepTrip{}, nil
	}

	blockerID, err := newRecoverySweepID("blk")
	if err != nil {
		return recoverySweepTrip{}, err
	}
	recoveryCommand := "striatum escalation resolve " + blockerID
	shortError := truncateRecoverySweepText(errorText, 1000)
	now := recoverySweepNow()
	payload := map[string]any{
		"schema_version":                "striatum.recovery_escalation.v1",
		"source":                        recoverySweepTripSource,
		"is_escalation":                 true,
		"blocker_kind":                  recoveryExhaustedKind,
		"severity":                      "blocked",
		"disposition":                   "sweep_trip_latch",
		"consecutive_degraded_sweeps":   degradedCount,
		"recovery_sweep_trip_threshold": threshold,
		"last_error":                    shortError,
		"author":                        author,
		"suggested_operator_actions": []any{
			"inspect and fix the recovery sweep error for this run",
			"after the fault is corrected, run `" + recoveryCommand + "` to clear needs_operator and re-arm the sweep",
			"cancel the run if it cannot be recovered",
		},
	}
	payloadArg, err := db.JSONBArg(tx, payload)
	if err != nil {
		return recoverySweepTrip{}, err
	}
	description := fmt.Sprintf(
		"recovery sweep breaker tripped after %d consecutive degraded sweeps: %s; recover with `%s` after fixing the sweep error, or cancel the run",
		degradedCount, shortError, recoveryCommand,
	)
	if err := tx.Exec(ctx, `
		INSERT INTO striatumd.blockers (
		  repository_id, blocker_id, run_id, job_id, session_id, severity,
		  blocker_kind, description, state, created_at, payload_json
		)
		VALUES ($1,$2,$3,NULL,NULL,'blocked',$4,$5,'open',$6,$7::jsonb)`,
		repositoryID, blockerID, runID, recoveryExhaustedKind, description, now, payloadArg,
	); err != nil {
		return recoverySweepTrip{}, err
	}
	if err := tx.Exec(ctx, `
		INSERT INTO striatumd.escalation_inbox (
		  repository_id, escalation_id, run_id, job_id, session_id,
		  blocker_id, blocker_kind, severity, state, created_at, payload_json
		)
		VALUES ($1,$2,$3,NULL,NULL,$4,$5,'blocked','pending',$6,$7::jsonb)`,
		repositoryID, blockerID, runID, blockerID, recoveryExhaustedKind, now, payloadArg,
	); err != nil {
		return recoverySweepTrip{}, err
	}
	if err := tx.Exec(ctx, `
		UPDATE striatumd.runs
		   SET state = 'needs_operator'
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND state IN ('running','paused')`,
		repositoryID, runID); err != nil {
		return recoverySweepTrip{}, err
	}
	if db.ActiveWriteBoundary().AtLeast(db.PhaseFull) {
		if _, err := db.AppendEventRowSD(ctx, tx, db.EventRow{
			RepositoryID: repositoryID,
			RunID:        runID,
			EventType:    "run.escalated",
			Payload: map[string]any{
				"reason":                      recoveryExhaustedKind,
				"source":                      recoverySweepTripSource,
				"blocker_id":                  blockerID,
				"blocker_kind":                recoveryExhaustedKind,
				"consecutive_degraded_sweeps": degradedCount,
				"last_error":                  shortError,
			},
		}); err != nil {
			return recoverySweepTrip{}, err
		}
		if _, err := db.AppendEventRowSD(ctx, tx, db.EventRow{
			RepositoryID: repositoryID,
			RunID:        runID,
			EventType:    "run.needs_operator",
			Payload: map[string]any{
				"reason":        recoveryExhaustedKind,
				"source":        recoverySweepTripSource,
				"escalation_id": blockerID,
			},
		}); err != nil {
			return recoverySweepTrip{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return recoverySweepTrip{}, err
	}
	committed = true
	// Wake the opt-in escalation notifier strictly AFTER the trip transaction
	// committed — the exact seam the breaker exists to page an AFK operator
	// about (an otherwise-unattended wedge). Best-effort: the notifier swallows
	// errors and is bounded by its own 3s client timeout, so a failing or
	// hanging endpoint can never roll back the trip or wedge the sweep. Only
	// this fresh-insert path notifies; the existing-blocker re-flip path above
	// creates no new escalation row (its pending row was already announced).
	mutations.NotifyEscalationsBestEffort(ctx, repositoryID, runID, []map[string]any{{
		"source":       recoverySweepTripSource,
		"blocker_id":   blockerID,
		"blocker_kind": recoveryExhaustedKind,
		"disposition":  "sweep_trip_latch",
	}})
	return recoverySweepTrip{
		escalationID:    blockerID,
		recoveryCommand: recoveryCommand,
	}, nil
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

func recoverySweepNow() string {
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func newRecoverySweepID(prefix string) (string, error) {
	var body [16]byte
	if _, err := rand.Read(body[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(body[:]), nil
}

func truncateRecoverySweepText(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}
