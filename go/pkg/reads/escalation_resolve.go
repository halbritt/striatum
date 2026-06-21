package reads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RunCompletionRedriveHook re-drives the RFC 0118 P0-3 run-completion
// re-verification after an escalation resolve flips a needs_operator run
// back to running. With every job already terminal nothing else will ever
// call the completion path again, so the resolve must re-invoke it or the
// run sits running-but-inert forever. Wired by mutations.Register (the reads
// package cannot import mutations — mutations imports reads).
var RunCompletionRedriveHook func(ctx context.Context, tx db.TxRunner, repositoryID, runID string) error

// RecoveryExhaustedRedispatchHook re-dispatches a sessionless, budget-exhausted
// job after its recovery_exhausted escalation is resolved (#388). The repro: an
// agent_exited_unsealed job's requeue budget ran out, the daemon escalated
// recovery_exhausted, and the driver exited; after `escalation resolve` the job
// stayed `running` with a dead session and a spent budget, so retry-job rejected
// it, complete-stalled had nothing durable, and the re-armed sweep re-escalated
// (the budget was never reset) — a permanent wedge. The hook (registered by
// mutations.Register, since reads cannot import mutations) resets a
// running/claimed/stale_lease job WITH NO LIVE SESSION back to queued (releasing
// the lease, re-pending the work message) AND clears its job_recovery_state
// (escalation_pending=false, requeue_count=0) so the re-armed run/sweep
// re-dispatches with a fresh budget. It is a no-op for a job that already
// completed, was quarantined, or still has a live session. nil only in a unit
// test that drives HandleEscalationResolve without mutations.Register.
var RecoveryExhaustedRedispatchHook func(ctx context.Context, tx db.TxRunner, repositoryID, runID, jobID string) (bool, error)

// RunLockHook takes the RFC 0104 per-run advisory lock (mutations.LockRun) on
// escalation.resolve's transaction. escalation.resolve is a per-run write verb
// (it FOR UPDATEs blockers, then maybeCompleteRun FOR UPDATEs runs) but lives in
// pkg/reads, which cannot import pkg/mutations (mutations imports reads), so the
// lock primitive is injected as a hook by mutations.Register rather than called
// directly — sharing the EXACT advisory-lock key with every in-package per-run
// mutation instead of re-deriving it. Like every other per-run mutation, the
// lock MUST be taken as the first statement of the transaction, before any
// blocking FOR UPDATE on a run-scoped row, or the {blockers/runs} pair re-opens
// the inverted lock-order cycle the advisory lock exists to retire. nil only in
// a unit test that drives HandleEscalationResolve without mutations.Register
// (where there is no concurrent run mutation to serialize against anyway).
var RunLockHook func(ctx context.Context, tx db.TxRunner, repositoryID, runID string) error

func HandleEscalationResolve(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	escalationID := stringParam(envelope, "escalation_id")
	if escalationID == "" {
		escalationID = stringParam(envelope, "blocker_id")
	}
	if escalationID == "" {
		return nil, rpc.NewError("schema_invalid", "escalation_id must be a non-empty string", nil)
	}
	decisionID, decisionSet, err := optionalStrictText(envelope, "decision_id")
	if err != nil {
		return nil, err
	}
	resolutionNote, noteSet, err := optionalStrictText(envelope, "resolution_note")
	if err != nil {
		return nil, err
	}

	txResult, err := withResolveRetryOnDeadlock(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104 (#356): escalation.resolve is a per-run write — it FOR UPDATEs
		// the blocker below and then maybeCompleteRun FOR UPDATEs the run — so it
		// must take the per-run advisory lock as the transaction's FIRST statement,
		// before the blocking FOR UPDATE. Resolve the blocker's (immutable) run_id
		// with a NON-locking read, then take the lock; a missing/non-escalation
		// blocker takes no lock and lets the FOR UPDATE below surface the canonical
		// not-found error, preserving prior behavior. The lock primitive is injected
		// by mutations.Register (reads cannot import mutations) so the advisory-lock
		// key matches every in-package per-run mutation exactly.
		if RunLockHook != nil {
			lockRows, err := queryAnyRows(ctx, tx, `
				SELECT run_id
				  FROM striatumd.blockers b
				 WHERE b.repository_id = $1
				   AND b.blocker_id = $2
				   AND `+escalationPredicate(), repositoryID, escalationID)
			if err != nil {
				return nil, err
			}
			if len(lockRows) > 0 {
				if runID := fmt.Sprint(lockRows[0]["run_id"]); runID != "" && runID != "<nil>" {
					if err := RunLockHook(ctx, tx, repositoryID, runID); err != nil {
						return nil, err
					}
				}
			}
		}
		blockers, err := queryAnyRows(ctx, tx, `
			SELECT *
			  FROM striatumd.blockers b
			 WHERE b.repository_id = $1
			   AND b.blocker_id = $2
			   AND `+escalationPredicate()+`
			 FOR UPDATE`, repositoryID, escalationID)
		if err != nil {
			return nil, err
		}
		if len(blockers) == 0 {
			return nil, rpc.NewError("not_found", "escalation not found: "+escalationID, nil)
		}
		blocker := blockers[0]
		if fmt.Sprint(blocker["state"]) != "open" {
			return nil, rpc.NewError("invalid_transition", "escalation is not open", nil)
		}

		payload := copyMap(objectOrEmpty(blocker["payload_json"]))
		resolutionPayload := map[string]any{}
		if decisionSet {
			resolutionPayload["decision_id"] = decisionID
		}
		if noteSet {
			resolutionPayload["resolution_note"] = resolutionNote
		}
		if len(resolutionPayload) > 0 {
			payload["escalation_resolution"] = resolutionPayload
		}
		var decisionArtifactID any = nil
		if decisionSet && decisionID != "" {
			artifacts, err := collectRows(ctx, tx, `
				SELECT artifact_id, run_id, job_id, session_id, logical_name
				  FROM striatumd.artifacts
				 WHERE repository_id = $1
				   AND artifact_kind = 'decision'
				   AND run_id = $2
				   AND logical_name = $3
				 LIMIT 1`, repositoryID, blocker["run_id"], decisionID)
			if err != nil {
				return nil, err
			}
			if len(artifacts) == 0 {
				return nil, rpc.NewError("not_found", "decision artifact for decision_id not found in run", nil)
			}
			artifact := artifacts[0]
			if nullableResolve(artifact["job_id"]) != nil || nullableResolve(artifact["session_id"]) != nil {
				return nil, rpc.NewError("invalid_transition", "decision artifact must be run-level (no job or session binding)", nil)
			}
			decisionArtifactID = artifact["artifact_id"]
		}
		now := resolveNowString()
		payloadArg, err := db.JSONBArg(tx, payload)
		if err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.blockers
			   SET state = 'resolved',
			       resolved_at = $1,
			       payload_json = $2::jsonb
			 WHERE repository_id = $3
			   AND blocker_id = $4`, now, payloadArg, repositoryID, escalationID); err != nil {
			return nil, err
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.escalation_inbox
			   SET state = 'resolved',
			       resolved_at = $1,
			       decision_artifact_id = $2,
			       resolution_note = $3,
			       payload_json = $4::jsonb
			 WHERE repository_id = $5
			   AND escalation_id = $6`, now, decisionArtifactID, nullableResolve(resolutionNote), payloadArg, repositoryID, escalationID); err != nil {
			return nil, err
		}
		eventPayload := map[string]any{"escalation_id": escalationID}
		for key, value := range resolutionPayload {
			eventPayload[key] = value
		}
		if _, err := appendResolveEvent(ctx, tx, repositoryID, fmt.Sprint(blocker["run_id"]), "escalation.resolved", nil, nullableResolve(blocker["job_id"]), nil, nil, nil, eventPayload); err != nil {
			return nil, err
		}
		// RFC 0101 Phase 4: resolving an escalation must clear the run's
		// needs_operator state so the recovery sweep (running/paused filter) and
		// new claims (claim.go gates on run.state='running') resume. The job was
		// already requeued by Phase 3, or the operator re-prepares it; either way
		// the run is no longer silently stuck. Guarded on state='needs_operator' so
		// resolving an unrelated escalation on a still-running run is a no-op, and a
		// terminal/canceled run is never revived. UPDATE ... RETURNING reports
		// whether the flip happened so run.resumed is emitted only when it did.
		runID := fmt.Sprint(blocker["run_id"])
		// #388: re-dispatch a budget-exhausted, sessionless job whose
		// recovery_exhausted escalation is being resolved, BEFORE the
		// needs_operator -> running flip and completion re-drive below. If the job
		// is reset to queued here, the run is NOT actually complete, so the flip
		// must happen (a fresh lane can claim the re-pended work) and the completion
		// re-drive must NOT prematurely finalize it. Resetting first keeps that
		// ordering honest. The hook is a no-op for any other escalation kind, an
		// already-terminal/quarantined job, or a job that still has a live session.
		redispatched := false
		if RecoveryExhaustedRedispatchHook != nil &&
			fmt.Sprint(blocker["blocker_kind"]) == "recovery_exhausted" {
			jobID := fmt.Sprint(nullableResolve(blocker["job_id"]))
			if jobID != "" {
				done, err := RecoveryExhaustedRedispatchHook(ctx, tx, repositoryID, runID, jobID)
				if err != nil {
					return nil, err
				}
				redispatched = done
			}
		}
		clearedRows, err := queryAnyRows(ctx, tx, `
			UPDATE striatumd.runs
			   SET state = 'running'
			 WHERE repository_id = $1 AND run_id = $2 AND state = 'needs_operator'
			RETURNING run_id`, repositoryID, runID)
		if err != nil {
			return nil, err
		}
		if len(clearedRows) > 0 {
			if _, err := appendResolveEvent(ctx, tx, repositoryID, runID, "run.resumed", nil, nullableResolve(blocker["job_id"]), nil, nil, nil, map[string]any{
				"reason":        "escalation_resolved",
				"escalation_id": escalationID,
				"from_state":    "needs_operator",
			}); err != nil {
				return nil, err
			}
			// #388: when the job was re-dispatched, do NOT re-drive run completion —
			// the run now has a live (queued, re-pended) job to finish, and the
			// completion gate would otherwise see no claimable session and could
			// prematurely settle/escalate. The re-pended work message wakes a fresh
			// lane through the normal claim path. The completion re-drive stays for
			// the unchanged case (every job already terminal).
			if RunCompletionRedriveHook != nil && !redispatched {
				if err := RunCompletionRedriveHook(ctx, tx, repositoryID, runID); err != nil {
					return nil, err
				}
			}
		}
		// #505: re-read the run's final state and whether it now has claimable,
		// non-paused work, so the response can advertise the re-drive verb. The
		// detached auto-driver (`run drive`, unit `striatum-drive-<run>`) exits when
		// the run hits needs_operator (runreconcile.IsTerminalRunState treats it as
		// drive-terminal) and is not re-armed by this handler; without the hint the
		// run silently stalls after the operator resolves the escalation until a
		// manual re-drive. Mirrors checkpoint.resolve's re-arm hint.
		runStateRows, err := queryAnyRows(ctx, tx, `
			SELECT state, paused_at FROM striatumd.runs
			 WHERE repository_id = $1 AND run_id = $2`, repositoryID, runID)
		if err != nil {
			return nil, err
		}
		finalRunState := ""
		runPaused := false
		if len(runStateRows) > 0 {
			finalRunState = fmt.Sprint(runStateRows[0]["state"])
			runPaused = nullableResolve(runStateRows[0]["paused_at"]) != nil
		}
		claimableRows, err := queryAnyRows(ctx, tx, `
			SELECT 1 FROM striatumd.jobs
			 WHERE repository_id = $1 AND run_id = $2
			   AND state IN ('queued','blocked')
			 LIMIT 1`, repositoryID, runID)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"run_id":             runID,
			"run_state":          finalRunState,
			"run_paused":         runPaused,
			"has_claimable_work": len(claimableRows) > 0,
		}, nil
	})
	if err != nil {
		return nil, err
	}

	escalation, err := loadEscalationProjection(ctx, runner, repositoryID, escalationID)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"status":     "resolved",
		"escalation": escalation,
	}
	if txResult != nil {
		runID := fmt.Sprint(txResult["run_id"])
		runState := fmt.Sprint(txResult["run_state"])
		out["run_state"] = runState
		nextActions := []string{}
		paused, _ := txResult["run_paused"].(bool)
		claimable, _ := txResult["has_claimable_work"].(bool)
		// Only re-arm when the run is genuinely drivable: running, not a deliberate
		// pause (RFC 0124 C5), with claimable work. A still-terminal or paused run
		// gets no re-drive hint.
		if runState == "running" && !paused && claimable && runID != "" && runID != "<nil>" {
			nextActions = append(nextActions, fmt.Sprintf("run drive --run-id %s", runID))
		}
		if len(nextActions) > 0 {
			out["next_actions"] = nextActions
		}
	}
	return out, nil
}

func optionalStrictText(envelope rpc.Envelope, key string) (string, bool, error) {
	value, exists := envelope.Params[key]
	if !exists || value == nil {
		return "", false, nil
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return "", false, rpc.NewError("schema_invalid", key+" must be a non-empty string when present", nil)
	}
	return text, true, nil
}

func loadEscalationProjection(ctx context.Context, runner any, repositoryID string, escalationID string) (map[string]any, error) {
	rows, err := queryAnyRows(ctx, runner, `
		SELECT b.blocker_id AS escalation_id, b.blocker_id, b.run_id,
		        b.job_id, j.workflow_job_id, b.session_id,
		        s.role_id AS session_role_id, s.lane_id AS session_lane_id,
		        b.severity, b.blocker_kind AS class, b.description, b.state,
		        b.created_at, b.resolved_at, b.payload_json,
		        a.artifact_id AS linked_artifact_id,
		        a.repo_path AS linked_repo_path,
		        a.content_sha256 AS linked_content_sha256
		   FROM striatumd.blockers b
		   LEFT JOIN striatumd.jobs j
		     ON j.repository_id = b.repository_id AND j.job_id = b.job_id
		   LEFT JOIN striatumd.sessions s
		     ON s.repository_id = b.repository_id AND s.session_id = b.session_id
		   LEFT JOIN striatumd.artifacts a
		     ON a.repository_id = b.repository_id
		    AND a.artifact_id = b.payload_json #>> '{escalation_artifact,artifact_id}'
		  WHERE b.repository_id = $1
		    AND b.blocker_id = $2
		    AND `+escalationPredicate(), repositoryID, escalationID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, rpc.NewError("not_found", "escalation not found: "+escalationID, nil)
	}
	return shapeEscalations(rows)[0], nil
}

// resolveDeadlockSQLState is the Postgres SQLSTATE for a detected deadlock,
// mirroring mutations.deadlockSQLState. escalation.resolve cannot reuse the
// mutations helper directly (reads cannot import mutations), so the tight,
// deadlock-only retry is reproduced here.
const resolveDeadlockSQLState = "40P01"

func isResolveDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == resolveDeadlockSQLState
}

// withResolveRetryOnDeadlock wraps withResolveTx in the same tight,
// deadlock-only retry as mutations.withTxRetryOnDeadlock (#356). escalation.
// resolve takes the per-run advisory lock first (RunLockHook), so a 40P01 here
// is a backstop, not the common case; only SQLSTATE 40P01 is retried, with a
// short bounded backoff, so any other error (not_found, invalid_transition,
// daemon_auth_lost, etc.) is returned immediately and a genuine livelock
// surfaces a clear error rather than spinning.
func withResolveRetryOnDeadlock(ctx context.Context, runner db.Runner, fn func(db.TxRunner) (map[string]any, error)) (map[string]any, error) {
	const maxAttempts = 3
	const baseBackoff = 5 * time.Millisecond
	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err := withResolveTx(ctx, runner, fn)
		if err == nil {
			return result, nil
		}
		if !isResolveDeadlock(err) {
			return nil, err
		}
		if attempt == maxAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(baseBackoff * time.Duration(attempt+1)):
		}
	}
	return nil, rpc.NewError(
		"invalid_transition",
		"transaction aborted by a database deadlock after retrying; retry serially",
		map[string]any{"sqlstate": resolveDeadlockSQLState, "attempts": maxAttempts},
	)
}

// withResolveTx runs escalation.resolve's body inside an authorized mutation
// transaction. Although escalation.resolve is registered in the reads package
// (its Python parity handler lives beside the escalation projection), it is a
// write verb: it must open its transaction the same way every mutation verb does
// (db.BeginAuthorizedMutation, via the mutations withTx chokepoint) so the RFC
// 0110 authority/attribution prelude is the transaction's first statement. Under
// pg_write_boundary=full the event append routes through the owner-owned SECURITY
// DEFINER append_event_row, which asserts daemon authority from the
// prelude-installed striatum.daemon_auth GUC; a bare runner.BeginTx leaves that
// GUC unset, so the SD function raises SQLSTATE 28000 and the handler returns
// daemon_auth_lost while every other write verb succeeds (#176). The mutation
// audit row is appended as the final write inside the same transaction (RFC 0110
// §4.4), atomic with the resolve, mirroring the mutations chokepoint; an append
// failure fails the whole resolve closed.
func withResolveTx(ctx context.Context, runner db.Runner, fn func(db.TxRunner) (map[string]any, error)) (map[string]any, error) {
	tx, err := db.BeginAuthorizedMutation(ctx, runner, db.AuthorityFromContext(ctx))
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()
	result, err := fn(tx)
	if err != nil {
		return nil, err
	}
	auditID, audited, err := appendResolveMutationAudit(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	if audited {
		if dispatch, ok := rpc.AuditDispatchFromContext(ctx); ok {
			dispatch.AuditID = auditID
			dispatch.Appended = true
		}
	}
	return result, nil
}

// appendResolveMutationAudit appends the success audit row for escalation.resolve
// inside the resolve transaction, mirroring the mutations package's
// appendMutationAudit (RFC 0110 §4.4). It returns audited=false (no error) when
// the context lacks dispatch threading (a direct handler unit test or a
// recorder-less server), leaving auditing to the standalone dispatch path; an
// append failure is surfaced as an error so the resolve fails closed.
func appendResolveMutationAudit(ctx context.Context, tx db.TxRunner) (string, bool, error) {
	meta, ok, err := db.BuildAuditMetaFromContext(ctx, true)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	auditID, err := db.AppendAuditInTx(ctx, tx, meta)
	if err != nil {
		return "", false, rpc.NewError(
			"audit_append_failed",
			"daemon could not append the mutation audit row; rolling the mutation back",
			map[string]any{"cause": err.Error()},
		)
	}
	return auditID, true, nil
}

func queryAnyRows(ctx context.Context, runner any, sql string, args ...any) ([]map[string]any, error) {
	q, ok := runner.(Queryer)
	if !ok {
		return nil, fmt.Errorf("runner does not support row queries")
	}
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToMap)
}

func appendResolveEvent(
	ctx context.Context,
	runner any,
	repositoryID string,
	runID any,
	eventType string,
	actorSessionID any,
	jobID any,
	messageID any,
	artifactID any,
	leaseID any,
	payload map[string]any,
) (int64, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	// RFC 0110 §7 P2 (full): route the event append through the owner-owned
	// SECURITY DEFINER append_event_row (in-DB authority + transcript exclusion +
	// v3 chain hash), atomic with this mutation. Behavior-neutral until P2 is
	// adopted. Foreign keys are null-coerced here so the SD path's stored row
	// matches the direct INSERT below.
	if db.ActiveWriteBoundary().AtLeast(db.PhaseFull) {
		return db.AppendEventRowSD(ctx, runner, db.EventRow{
			RepositoryID:   repositoryID,
			RunID:          nullableResolve(runID),
			EventType:      eventType,
			ActorSessionID: nullableResolve(actorSessionID),
			JobID:          nullableResolve(jobID),
			MessageID:      nullableResolve(messageID),
			ArtifactID:     nullableResolve(artifactID),
			LeaseID:        nullableResolve(leaseID),
			Payload:        payload,
		})
	}
	previousHash, err := previousResolveChainHead(ctx, runner, repositoryID)
	if err != nil {
		return 0, err
	}
	eventID, err := nextResolveEventID(ctx, runner)
	if err != nil {
		return 0, err
	}
	createdAt := resolveNowString()
	rowMaterial := map[string]any{
		"repository_id":    repositoryID,
		"event_id":         eventID,
		"run_id":           nullableResolve(runID),
		"event_type":       eventType,
		"actor_session_id": nullableResolve(actorSessionID),
		"job_id":           nullableResolve(jobID),
		"message_id":       nullableResolve(messageID),
		"artifact_id":      nullableResolve(artifactID),
		"lease_id":         nullableResolve(leaseID),
		"payload_json":     payload,
		"created_at":       createdAt,
	}
	rowHash, err := canonicalResolveEventHash(rowMaterial, previousHash)
	if err != nil {
		return 0, err
	}
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return 0, fmt.Errorf("runner does not support exec")
	}
	payloadArg, err := db.JSONBArg(runner, payload)
	if err != nil {
		return 0, err
	}
	if err := exec.Exec(ctx, `
		INSERT INTO striatumd.events (
		  repository_id, event_id, run_id, event_type, actor_session_id, job_id,
		  message_id, artifact_id, lease_id, payload_json, created_at,
		  previous_hash, row_hash
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13)`,
		repositoryID,
		eventID,
		nullableResolve(runID),
		eventType,
		nullableResolve(actorSessionID),
		nullableResolve(jobID),
		nullableResolve(messageID),
		nullableResolve(artifactID),
		nullableResolve(leaseID),
		payloadArg,
		createdAt,
		nullableResolve(previousHash),
		rowHash,
	); err != nil {
		return 0, err
	}
	if err := exec.Exec(ctx, `
		INSERT INTO striatumd.repo_event_chain_heads(
		  repository_id, last_event_id, last_hash, updated_at
		) VALUES ($1, $2, $3, now())
		ON CONFLICT (repository_id)
		DO UPDATE SET last_event_id = EXCLUDED.last_event_id,
		              last_hash = EXCLUDED.last_hash,
		              updated_at = now()`,
		repositoryID,
		eventID,
		rowHash,
	); err != nil {
		return 0, err
	}
	return eventID, nil
}

func previousResolveChainHead(ctx context.Context, runner any, repositoryID string) (any, error) {
	rower, ok := runner.(interface {
		QueryRow(context.Context, string, ...any) db.Row
	})
	if !ok {
		return nil, fmt.Errorf("runner does not support query row")
	}
	var hash *string
	err := rower.QueryRow(ctx,
		"SELECT last_hash FROM striatumd.repo_event_chain_heads WHERE repository_id = $1 FOR UPDATE",
		repositoryID,
	).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if hash == nil {
		return nil, nil
	}
	return *hash, nil
}

func nextResolveEventID(ctx context.Context, runner any) (int64, error) {
	rower, ok := runner.(interface {
		QueryRow(context.Context, string, ...any) db.Row
	})
	if !ok {
		return 0, fmt.Errorf("runner does not support query row")
	}
	var eventID int64
	err := rower.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('striatumd.events', 'event_id'))").Scan(&eventID)
	return eventID, err
}

func canonicalResolveEventHash(row map[string]any, previousHash any) (string, error) {
	payload := map[string]any{
		"previous_hash":    nullableResolve(previousHash),
		"repository_id":    row["repository_id"],
		"event_id":         row["event_id"],
		"run_id":           nullableResolve(row["run_id"]),
		"event_type":       row["event_type"],
		"actor_session_id": nullableResolve(row["actor_session_id"]),
		"job_id":           nullableResolve(row["job_id"]),
		"message_id":       nullableResolve(row["message_id"]),
		"artifact_id":      nullableResolve(row["artifact_id"]),
		"lease_id":         nullableResolve(row["lease_id"]),
		"payload_json":     resolveEventPayload(row["payload_json"]),
		"created_at":       fmt.Sprint(row["created_at"]),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func resolveEventPayload(value any) map[string]any {
	payload := objectOrEmpty(value)
	result := map[string]any{}
	for key, item := range payload {
		if key == "_event_chain" {
			continue
		}
		result[key] = item
	}
	return result
}

func nullableResolve(value any) any {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return nil
		}
	case *string:
		if typed == nil || *typed == "" {
			return nil
		}
		return *typed
	}
	return value
}

func resolveNowString() string {
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}
