// Package mutations is the Go-core port of the daemon-side PostgreSQL
// workflow mutation handlers.
package mutations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/blob"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/lanehealth"
	"github.com/halbritt/striatum/go/pkg/reads"
	"github.com/halbritt/striatum/go/pkg/rpc"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type handlerFn func(context.Context, db.Runner, rpc.Envelope) (map[string]any, error)

// Options carries handler-construction dependencies that not all
// mutation handlers share. BlobClient may be nil when the daemon is
// started without STRIATUM_BLOB_ENDPOINT.
type Options struct {
	BlobClient       *blob.Client
	DaemonSocketPath string
	// MCPBootEpoch is THIS daemon process run's MCP boot epoch (#316). The
	// supervisor folds it into the lane capability material (env + per-launch
	// MCP client headers) so every MCP request a lane makes carries the epoch of
	// the daemon it was launched against; the daemon rejects a request whose
	// presented epoch differs from its live epoch (a recycled-port hit). Empty
	// disables injection (the daemon then accepts requests with no epoch, the
	// backward-compatible posture).
	MCPBootEpoch string
	RecallDigest RecallDigestOptions
}

// packageBlobClient is the daemon's blob client, set by Register and
// read by publishArtifact. Package-level so that publishArtifact's
// transitive callers (review.submit, recovery.auto_finalize) do not
// need their own threading. nil = blob storage disabled / not
// configured; publishArtifact then skips the S3 upload step and the
// artifact body stays in the working tree.
var packageBlobClient *blob.Client
var packageDaemonSocketPath string

// packageMCPBootEpoch is THIS daemon process run's MCP boot epoch (#316), set
// by Register and read when building a supervised lane env. Package-level so
// supervisedEnvEntries (and its run-as sibling) can inject it without threading
// the value through every supervise call site, mirroring packageDaemonSocketPath.
var packageMCPBootEpoch string
var packageRecallDigestOptions RecallDigestOptions

var deadlockRetryExhaustedCounter = expvar.NewInt("deadlock.retry_exhausted")

// Register wires the mutation RPC handlers onto server. Optional opts
// inject extra dependencies; today only Options.BlobClient is
// consumed, via packageBlobClient at handler invocation time.
func Register(server *rpc.Server, runner db.Runner, opts ...Options) {
	if runner == nil {
		return
	}
	reads.DrainHelperEventsHook = func(ctx context.Context, tx db.TxRunner, repositoryID string, supervisorID string) error {
		return drainHelperEvents(ctx, tx, repositoryID, supervisorID, 0)
	}
	reads.RunCompletionRedriveHook = func(ctx context.Context, tx db.TxRunner, repositoryID string, runID string) error {
		return maybeCompleteRun(ctx, tx, repositoryID, runID)
	}
	reads.RunLockHook = func(ctx context.Context, tx db.TxRunner, repositoryID string, runID string) error {
		return LockRun(ctx, tx, repositoryID, runID)
	}
	reads.RecoveryExhaustedRedispatchHook = func(ctx context.Context, tx db.TxRunner, repositoryID, runID, jobID string) (bool, error) {
		return redispatchRecoveryExhaustedJob(ctx, tx, repositoryID, runID, jobID)
	}
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	packageBlobClient = o.BlobClient
	packageDaemonSocketPath = strings.TrimSpace(o.DaemonSocketPath)
	packageMCPBootEpoch = strings.TrimSpace(o.MCPBootEpoch)
	packageRecallDigestOptions = normalizeRecallDigestOptions(o.RecallDigest)
	server.Register("session.register", makeHandler(runner, HandleRegisterSession))
	server.Register("session.close", makeHandler(runner, HandleCloseSession))
	server.Register("session.report", makeHandler(runner, HandleSessionReport))
	server.Register("wake.wait", makeHandler(runner, HandleWakeWait))
	server.Register("work.claim_next", makeHandler(runner, HandleClaimNext))
	server.Register("claim_next", makeHandler(runner, HandleClaimNext))
	server.Register("work.claim_override", makeHandler(runner, HandleClaimOverride))
	server.Register("work.await_packet", makeHandler(runner, HandleAwaitPacket))
	server.Register("work.ack", makeHandler(runner, HandleAckWork))
	server.Register("ack", makeHandler(runner, HandleAckWork))
	server.Register("work.heartbeat", makeHandler(runner, HandleHeartbeat))
	server.Register("heartbeat", makeHandler(runner, HandleHeartbeat))
	server.Register("work.release", makeHandler(runner, HandleReleaseWork))
	server.Register("release", makeHandler(runner, HandleReleaseWork))
	server.Register("work.send_message", makeHandler(runner, HandleSendMessage))
	server.Register("work.block", makeHandler(runner, HandleBlockWork))
	server.Register("block", makeHandler(runner, HandleBlockWork))
	server.Register("work.complete", makeHandler(runner, HandleCompleteWork))
	server.Register("complete", makeHandler(runner, HandleCompleteWork))
	server.Register("artifact.publish", makeHandler(runner, HandlePublishArtifact))
	server.Register("publish_artifact", makeHandler(runner, HandlePublishArtifact))
	server.Register("repo.write", makeHandler(runner, HandleRepoWrite))
	server.Register("repo.patch_preview", makeHandler(runner, HandleRepoPatchPreview))
	server.Register("repo.patch_apply", makeHandler(runner, HandleRepoPatchApply))
	server.Register("process.run", makeHandler(runner, HandleProcessRun))
	server.Register("git.commit_apply", makeHandler(runner, HandleGitCommitApply))
	server.Register("worktree.create", makeHandler(runner, HandleWorktreeCreate))
	server.Register("worktree.release", makeHandler(runner, HandleWorktreeRelease))
	server.Register("worktree.anchor", makeHandler(runner, HandleWorktreeAnchor))
	server.Register("worktree.gc", makeHandler(runner, HandleWorktreeGC))
	server.Register("supervise.start", makeHandler(runner, HandleSuperviseStart))
	server.Register("supervise.send", makeHandler(runner, HandleSuperviseSend))
	server.Register("supervise.rebridge", makeHandler(runner, HandleSuperviseRebridge))
	server.Register("supervise.stop", makeHandler(runner, HandleSuperviseStop))
	server.Register("workflow.init", makeHandler(runner, HandleWorkflowInit))
	server.Register("workflow.generate", makeHandler(runner, HandleWorkflowGenerate))
	server.Register("workflow.upgrade", makeHandler(runner, HandleWorkflowUpgrade))
	server.Register("workflow.accept_risk", makeHandler(runner, HandleWorkflowAcceptRisk))
	server.Register("review.verdict", makeHandler(runner, HandleRecordVerdict))
	server.Register("verdict", makeHandler(runner, HandleRecordVerdict))
	server.Register("review.submit", makeHandler(runner, HandleSubmitReview))
	server.Register("submit_review", makeHandler(runner, HandleSubmitReview))
	server.Register("review.override", makeHandler(runner, HandleOverrideVerdict))
	server.Register("run.prepare", makeHandler(runner, HandleRunPrepare))
	server.Register("run.start", makeHandler(runner, HandleRunStart))
	server.Register("run.pause", makeHandler(runner, HandleRunPause))
	server.Register("run.resume", makeHandler(runner, HandleRunResume))
	server.Register("run.cancel", makeHandler(runner, HandleRunCancel))
	server.Register("run.retry_job", makeHandler(runner, HandleRunRetryJob))
	server.Register("run.integrate", makeHandler(runner, HandleRunIntegrate))
	server.Register("branch.confirm", makeHandler(runner, HandleBranchConfirm))
	server.Register("decision.record", makeHandler(runner, HandleDecisionRecord))
	server.Register("checkpoint.resolve", makeHandler(runner, HandleCheckpointResolve))
	server.Register("verifier.attest", makeHandler(runner, HandleVerifierAttest))
	server.Register("recovery.stale_leases", makeHandler(runner, HandleRecoveryStaleLeases))
	server.Register("recovery.requeue_stale", makeHandler(runner, HandleRecoveryRequeueStale))
	server.Register("recovery.cancel_job", makeHandler(runner, HandleRecoveryCancelJob))
	server.Register("recovery.process_reconcile", makeHandler(runner, HandleRecoveryProcessReconcile))
	server.Register("recovery.resume", makeHandler(runner, HandleRecoveryResume))
	server.Register("recovery.sweep", makeHandler(runner, HandleRecoveryAuto))
	server.Register("recovery.auto_publish_stale_artifacts", makeHandler(runner, HandleRecoveryAuto))
	server.Register("recovery.auto", makeHandler(runner, HandleRecoveryAuto))
	server.Register("recovery.auto_finalize", makeHandler(runner, HandleRecoveryAutoFinalize))
	server.Register("recovery.invalidate_job", makeHandler(runner, HandleRecoveryInvalidateJob))
	server.Register("recovery.reseal", makeHandler(runner, HandleRecoveryReseal))
	server.Register("recovery.complete_stalled", makeHandler(runner, HandleRecoveryCompleteStalled))
	server.Register("recovery.accept_quarantined", makeHandler(runner, HandleRecoveryAcceptQuarantined))
	server.Register("recovery.resolve_blocker", makeHandler(runner, HandleRecoveryResolveBlocker))
	server.Register("recovery.prune_debris", makeHandler(runner, HandleRecoveryPruneDebris))
	server.Register("recovery.quarantine_lane", makeHandler(runner, HandleRecoveryQuarantineLane))
	server.Register("supervise.report", makeHandler(runner, HandleSuperviseReport))
	server.Register("interrogation.open", makeHandler(runner, HandleInterrogationOpen))
	server.Register("interrogation.ask", makeHandler(runner, HandleInterrogationAsk))
	server.Register("interrogation.answer", makeHandler(runner, HandleInterrogationAnswer))
	server.Register("interrogation.close", makeHandler(runner, HandleInterrogationClose))
	server.Register("conversation.open", makeHandler(runner, HandleConversationOpen))
	server.Register("conversation.say", makeHandler(runner, HandleConversationSay))
	server.Register("conversation.close", makeHandler(runner, HandleConversationClose))
	server.Register("conversation.list", makeHandler(runner, HandleConversationList))
	server.Register("conversation.show", makeHandler(runner, HandleConversationShow))
	server.Register("corpus.migrate_historical_dogfood_file", makeHandler(runner, HandleCorpusMigrateHistoricalDogfoodFile))
	server.Register("artifact.backfill_blob", makeHandler(runner, HandleArtifactBackfillBlob))
}

func makeHandler(runner db.Runner, fn handlerFn) rpc.Handler {
	return func(ctx context.Context, envelope rpc.Envelope) (map[string]any, error) {
		return fn(ctx, runner, envelope)
	}
}

func requireRepositoryID(envelope rpc.Envelope) (string, error) {
	value, _ := envelope.Params["repository_id"].(string)
	if value == "" {
		return "", rpc.NewError("repo_not_registered", "daemon RPC route requires repository_id", nil)
	}
	return value, nil
}

// sessionBinding is the result of evaluating the caller's capability-token
// session binding against the act-as session a session-scoped handler is about
// to act for (RFC 0096 V2 / #135).
type sessionBinding struct {
	// OperatorOverride is true when the caller's token is session-UNBOUND (the
	// operator/coordinator repo-scoped token) acting as a lane. The action is
	// allowed but must be recorded with honest operator provenance, never
	// attributed to the target lane's own PTY.
	OperatorOverride bool
}

const staleSessionTokenSuggestion = "Stop the stale lane and run supervise.start for the active successor session so it receives its own session-bound token; use supervise.rebridge only to repair that successor's existing supervisor, then retry from the successor lane."
const inactiveSessionSuggestion = "Recover through daemon state: requeue the job on the same attempt (`striatum recovery requeue-stale --run-id <run_id> --job-id <job_id> --force --justification \"...\"`), then register or supervise.start a fresh session and retry from that fresh session. If the session is still active but about to be closed, use `striatum session close --session-id <session_id> --reason \"...\" --requeue-job`."

// enforceSessionBinding is the per-session capability-token guard that closes
// the cross-session impersonation vector (GH #135).
//
//   - If the caller's token is BOUND to a session, the act-as session
//     (actSessionID, the session_id param) MUST equal the bound session, else
//     capability_denied. This is what stops a compromised lane from answering
//     an interrogation as another lane.
//   - If the caller's token is UNBOUND (operator/coordinator), the action is
//     allowed but flagged OperatorOverride so the handler records honest
//     provenance (responder=operator), satisfying #135's explicit override
//     mode.
//   - When no AuthContext was threaded onto ctx at all (direct unit tests, or a
//     transport that does not authorize) the caller is treated as UNBOUND —
//     never as a bound impersonation bypass. Combined with
//     AllowAllAuthorizer yielding an unbound context, dev/test wiring keeps
//     working under operator-override semantics.
func enforceSessionBinding(ctx context.Context, actSessionID string) (sessionBinding, error) {
	auth, ok := rpc.AuthFromContext(ctx)
	if !ok || !auth.IsSessionBound() {
		return sessionBinding{OperatorOverride: true}, nil
	}
	if auth.SessionID != actSessionID {
		return sessionBinding{}, rpc.NewError(
			"capability_denied",
			fmt.Sprintf("capability token is bound to session %s; cannot act as session %s", auth.SessionID, actSessionID),
			nil,
		)
	}
	return sessionBinding{OperatorOverride: false}, nil
}

// enforceSessionBindingForSession is the DB-aware variant for session-scoped
// writes. It keeps ordinary cross-session impersonation as capability_denied,
// but when the caller is using a closed predecessor's token against the active
// successor in the same run/role/lane slot, it returns a typed stale-token
// remediation without exposing token material (#254).
func enforceSessionBindingForSession(ctx context.Context, runner any, repositoryID string, actSessionID string, method string) (sessionBinding, error) {
	auth, ok := rpc.AuthFromContext(ctx)
	if !ok || !auth.IsSessionBound() {
		return sessionBinding{OperatorOverride: true}, nil
	}
	if auth.SessionID == actSessionID {
		return sessionBinding{OperatorOverride: false}, nil
	}
	if err := staleSuccessorSessionTokenError(ctx, runner, repositoryID, auth.SessionID, actSessionID, method); err != nil {
		return sessionBinding{}, err
	}
	return enforceSessionBinding(ctx, actSessionID)
}

func enforceActiveActingSession(ctx context.Context, runner any, repositoryID string, sessionID string, jobID string, method string) error {
	session, err := sessionBindingLookup(ctx, runner, repositoryID, sessionID)
	if err != nil {
		return err
	}
	if fmt.Sprint(session["state"]) == "active" {
		return nil
	}
	staleErr := rpc.NewError(
		"session_inactive",
		fmt.Sprintf("session %s is %s and cannot perform %s; requeue job %s on the same attempt, then retry from a fresh active session", sessionID, session["state"], method, jobID),
		map[string]any{
			"session_id":    sessionID,
			"session_state": fmt.Sprint(session["state"]),
			"run_id":        fmt.Sprint(session["run_id"]),
			"role_id":       fmt.Sprint(session["role_id"]),
			"lane_id":       fmt.Sprint(session["lane_id"]),
			"job_id":        jobID,
			"method":        method,
			"recovery":      "requeue_same_attempt_then_fresh_session",
		},
	)
	staleErr.Suggestion = inactiveSessionSuggestion
	return staleErr
}

func staleSuccessorSessionTokenError(ctx context.Context, runner any, repositoryID string, boundSessionID string, requestedSessionID string, method string) error {
	bound, err := sessionBindingLookup(ctx, runner, repositoryID, boundSessionID)
	if err != nil {
		return nil
	}
	requested, err := sessionBindingLookup(ctx, runner, repositoryID, requestedSessionID)
	if err != nil {
		return nil
	}
	sameSlot := fmt.Sprint(bound["run_id"]) == fmt.Sprint(requested["run_id"]) &&
		fmt.Sprint(bound["role_id"]) == fmt.Sprint(requested["role_id"]) &&
		fmt.Sprint(bound["lane_id"]) == fmt.Sprint(requested["lane_id"])
	if !sameSlot || fmt.Sprint(bound["state"]) == "active" || fmt.Sprint(requested["state"]) != "active" {
		return nil
	}
	if intValue(bound["ordinal"]) >= intValue(requested["ordinal"]) {
		return nil
	}
	staleErr := rpc.NewError(
		"session_token_stale",
		fmt.Sprintf("session-bound capability token belongs to closed predecessor session %s; it cannot act as successor session %s for %s. Restart or rebridge the successor lane so it receives its own session-bound token, then retry from that successor session.", boundSessionID, requestedSessionID, method),
		map[string]any{
			"bound_session_id":        boundSessionID,
			"requested_session_id":    requestedSessionID,
			"run_id":                  fmt.Sprint(requested["run_id"]),
			"role_id":                 fmt.Sprint(requested["role_id"]),
			"lane_id":                 fmt.Sprint(requested["lane_id"]),
			"bound_session_state":     fmt.Sprint(bound["state"]),
			"bound_session_reason":    nullable(bound["close_reason"]),
			"requested_session_state": fmt.Sprint(requested["state"]),
			"method":                  method,
		},
	)
	staleErr.Suggestion = staleSessionTokenSuggestion
	return staleErr
}

func sessionBindingLookup(ctx context.Context, runner any, repositoryID string, sessionID string) (map[string]any, error) {
	return oneRow(ctx, runner, `
		SELECT session_id, run_id, role_id, lane_id, ordinal, state, close_reason
		  FROM striatumd.sessions
		 WHERE repository_id = $1 AND session_id = $2
		 LIMIT 1`, repositoryID, sessionID)
}

func stringParam(envelope rpc.Envelope, key string) string {
	if value, ok := envelope.Params[key].(string); ok {
		return value
	}
	return ""
}

func boolParam(envelope rpc.Envelope, key string) bool {
	if value, ok := envelope.Params[key].(bool); ok {
		return value
	}
	return false
}

func intParam(envelope rpc.Envelope, key string, fallback int) int {
	switch value := envelope.Params[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func stringSliceParam(envelope rpc.Envelope, keys ...string) []string {
	for _, key := range keys {
		value, ok := envelope.Params[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case []string:
			return append([]string(nil), typed...)
		case []any:
			result := []string{}
			for _, item := range typed {
				result = append(result, fmt.Sprint(item))
			}
			return result
		case string:
			if typed != "" {
				return []string{typed}
			}
		}
	}
	return []string{}
}

// withTx runs fn inside an authorized mutation transaction. Every mutating
// handler routes through here (or withTxRetryOnDeadlock), so the RFC 0110
// authority/attribution prelude is installed as the transaction's first
// statement at a single chokepoint (C-AUTH-TX-WRAPPER) rather than per handler.
func withTx(ctx context.Context, runner db.Runner, fn func(db.TxRunner) (map[string]any, error)) (map[string]any, error) {
	rawTx, err := db.BeginAuthorizedMutation(ctx, runner, db.AuthorityFromContext(ctx))
	if err != nil {
		return nil, err
	}
	tx := newWakeTx(rawTx)
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()
	result, err := fn(tx)
	if err != nil {
		// #222/dbf2013b follow-up: the session backend gate must EXPIRE/mark-lost a
		// dead session as a durable cleanup, but it runs inside this work tx and
		// then returns a refusal error — which rolls the tx back and would undo the
		// transition. Roll back first (releasing this tx's FOR UPDATE locks on the
		// session row), then apply the cleanup on a fresh pooled connection so it
		// commits independently, and surface the plain refusal to the caller.
		var gate *sessionBackendGateError
		if errors.As(err, &gate) {
			_ = tx.Rollback(context.Background())
			committed = true // finalize: skip the deferred rollback
			if gate.cleanup != nil {
				_ = gate.cleanup(ctx, runner)
			}
			return nil, gate.rpcErr
		}
		return nil, err
	}
	// RFC 0110 §4.4: append the success audit row as the final write INSIDE this
	// transaction so it commits atomically with the mutation; an append failure
	// fails closed (aborts the whole mutation).
	auditID, audited, err := appendMutationAudit(ctx, tx)
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
	tx.publishWakeEvents()
	return result, nil
}

// appendMutationAudit appends the success audit row for the current RPC inside
// the mutation's own transaction. It returns audited=false (no error) when the
// context lacks dispatch threading (a direct handler unit test or a recorder-
// less server), leaving auditing to the standalone dispatch path. An append
// failure is surfaced as an error so withTx fails the whole mutation closed.
func appendMutationAudit(ctx context.Context, tx db.TxRunner) (string, bool, error) {
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

// deadlockSQLState is the Postgres SQLSTATE for a detected deadlock.
const deadlockSQLState = "40P01"

// isDeadlockError reports whether err is a Postgres deadlock (SQLSTATE 40P01).
// Two concurrent mutations that touch the same rows in opposite order (e.g.
// parallel review.submit calls completing sibling reviews) can have the
// deadlock detector abort one of them; that loser is safe to retry.
func isDeadlockError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == deadlockSQLState
}

// statementTimeoutSQLState is the Postgres SQLSTATE raised when a statement is
// canceled for exceeding statement_timeout.
const statementTimeoutSQLState = "57014"

// lockTimeoutSQLState is the Postgres SQLSTATE raised when a statement is
// canceled for exceeding lock_timeout — it waited on a row/table lock longer than
// the configured bound and was aborted before the lock was granted.
const lockTimeoutSQLState = "55P03"

// isTransientDaemonLoadError reports whether err is a transient, retry-safe
// control-plane error caused by daemon/database load rather than a real failure
// of the requested transition (#197). The canonical case is a statement timeout
// (SQLSTATE 57014): under concurrent multi-run claim contention a claim query can
// be canceled for exceeding statement_timeout. That is "the daemon is busy, poll
// again", NOT "no work" or "broken" — but it surfaced to lanes as a raw
// `ERROR: canceling statement due to statement timeout (SQLSTATE 57014)`, which
// models reasonably read as a hard failure and stopped retrying, orphaning the
// job. Class 57 (operator_intervention) admin shutdown/crash codes (57P01
// admin_shutdown, 57P02 crash_shutdown, 57P03 cannot_connect_now) are likewise
// transient — the connection was torn down under us, and the lane's await loop
// should poll again rather than park.
//
// lock_timeout (SQLSTATE 55P03, #389 gap 1 / #383 item 3) joins them: under a
// multi-run state-DB storm a lifecycle/event-write tx can wait on a contended
// run-aggregate or event-chain-head lock longer than lock_timeout and be aborted
// before the lock is granted. That is the same victim-side back-pressure as a
// statement timeout — the requested transition was never refused, the writer just
// lost a race for a busy lock — so it surfaced raw as `append_event_row (sd):
// 55P03` and wedged the job (e.g. it consumed a max_attempts:1 job's only attempt,
// or hard-failed run.prepare/start). Classifying it transient lets the bounded
// retry wrappers (and the await/claim poll loop) re-attempt instead, self-healing
// once the convoy clears and surfacing a legible "daemon under load" error only if
// the bounded budget is exhausted.
func isTransientDaemonLoadError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case statementTimeoutSQLState, lockTimeoutSQLState, "57P01", "57P02", "57P03":
		return true
	default:
		return false
	}
}

// withTxRetryOnDeadlock runs fn inside a transaction, retrying up to a small
// bounded number of times when the transaction aborts with a Postgres deadlock
// (SQLSTATE 40P01) or a transient daemon-load error (isTransientDaemonLoadError:
// statement_timeout 57014, lock_timeout 55P03, or a class-57 connection
// teardown). The retry is intentionally tight — only those retry-safe SQLSTATEs
// are retried, with a short backoff — so a genuine livelock or sustained
// back-pressure surfaces a clear error rather than spinning. Any other error
// (validation, transition refusal, etc.) is returned immediately.
//
// The transient-load class is folded in here (#389 gap 1 / #383 item 3) so the
// per-run lifecycle/event-write verbs routed through this wrapper —
// artifact.publish/seal, review.submit, run.retry_job, the interrogation and
// supervision-control mutations — self-heal a transient lock_timeout/statement
// timeout the same way run.prepare does via withTxRetryOnTransientLoad, instead of
// surfacing the raw SQLSTATE and wedging the job (e.g. consuming a max_attempts:1
// job's only attempt). On exhaustion the surfaced error reflects the last cause:
// a deadlock stays the serial-retry `invalid_transition`, a transient load
// becomes the legible `daemon_under_load`.
func withTxRetryOnDeadlock(ctx context.Context, runner db.Runner, fn func(db.TxRunner) (map[string]any, error)) (map[string]any, error) {
	const maxAttempts = 3
	const baseBackoff = 5 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err := withTx(ctx, runner, fn)
		if err == nil {
			return result, nil
		}
		if !isDeadlockError(err) && !isTransientDaemonLoadError(err) {
			return nil, err
		}
		lastErr = err
		if attempt == maxAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(baseBackoff * time.Duration(attempt+1)):
		}
	}
	// Surface the error appropriate to the last cause: a transient daemon-load
	// (lock/statement timeout, connection teardown) gets the legible
	// `daemon_under_load`; a deadlock keeps the serial-retry guidance.
	if isTransientDaemonLoadError(lastErr) {
		var sqlstate string
		var pgErr *pgconn.PgError
		if errors.As(lastErr, &pgErr) {
			sqlstate = pgErr.Code
		}
		return nil, rpc.NewError(
			"daemon_under_load",
			"daemon is under load and the operation timed out after retrying; retry shortly",
			map[string]any{"sqlstate": sqlstate, "attempts": maxAttempts},
		)
	}
	deadlockRetryExhaustedCounter.Add(1)
	return nil, rpc.NewError(
		"invalid_transition",
		"transaction aborted by a database deadlock after retrying; retry serially",
		map[string]any{"sqlstate": deadlockSQLState, "attempts": maxAttempts},
	)
}

// withTxRetryOnTransientLoad runs fn inside a transaction, retrying up to a small
// bounded number of times when the transaction fails with a transient
// daemon-load error (isTransientDaemonLoadError: a statement_timeout 57014 or a
// class-57 admin/crash/cannot-connect shutdown). This is the victim-side
// mitigation for the #198/#355 event-append convoy: under a multi-run supervise
// storm a lock-holding sweep/reconcile transaction can starve a plain
// control-plane append (e.g. HandleRunPrepare's `run.created`) until it hits the
// global statement_timeout and surfaces the raw `append_event_row (sd): 57014`.
// That is transient back-pressure, not a real refusal of the transition, so the
// control-plane verb retries with a short bounded backoff rather than hard-
// failing the operator. Any non-transient error (validation, transition refusal,
// etc.) is returned immediately. After the bounded attempts it surfaces a legible
// "daemon under load, retry" error instead of the raw SQLSTATE.
//
// Modeled on withTxRetryOnDeadlock — same shape, classified via the existing
// isTransientDaemonLoadError. The primary #355 fix removes the convoy's source
// (the in-tx subprocess probe); this bounds the blast radius if any other
// lock-holder regresses.
func withTxRetryOnTransientLoad(ctx context.Context, runner db.Runner, fn func(db.TxRunner) (map[string]any, error)) (map[string]any, error) {
	const maxAttempts = 4
	const baseBackoff = 25 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err := withTx(ctx, runner, fn)
		if err == nil {
			return result, nil
		}
		if !isTransientDaemonLoadError(err) {
			return nil, err
		}
		lastErr = err
		if attempt == maxAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(baseBackoff * time.Duration(attempt+1)):
		}
	}
	var sqlstate string
	var pgErr *pgconn.PgError
	if errors.As(lastErr, &pgErr) {
		sqlstate = pgErr.Code
	}
	return nil, rpc.NewError(
		"daemon_under_load",
		"daemon is under load and the operation timed out after retrying; retry shortly",
		map[string]any{"sqlstate": sqlstate, "attempts": maxAttempts},
	)
}

// lockRun acquires the per-run transaction-scoped advisory lock that serializes
// every mutation of a single run's aggregate (sessions, runs, jobs, leases,
// queue_messages, verdicts, blockers, interrogations for that run). Per the
// RFC 0104 per-run serialization invariant it MUST be taken as the FIRST
// statement of every per-run mutation transaction, BEFORE any FOR UPDATE on a
// run-scoped row.
//
// The deadlock it retires (RFC 0104): the claim path
// (work.await_packet -> HandleClaimNext) locks `sessions` then `runs` then
// `jobs`, while the verdict-completion path
// (review.submit/verdict -> recordVerdict -> maybeCompleteRun ->
// closeRemainingSessions) locks `jobs` then `runs` then `sessions`, and the 60s
// recovery sweep is a third concurrent party on the same rows. The
// {sessions, runs} pair is acquired in opposite order, so without a shared,
// earlier serialization point Postgres aborts one transaction with SQLSTATE
// 40P01 — which under yolo/minimal-human-intervention surfaces to a lane with no
// operator to retry it by hand. Taking this lock first gives every per-run tx an
// identical earlier serialization point, so the cycle cannot form;
// withTxRetryOnDeadlock remains only as a defense-in-depth backstop.
//
// The key is per (repository_id, run_id) — the NARROWEST scope that serializes
// only the (small, lane-bounded) write concurrency WITHIN one run. Unrelated
// runs and unrelated repositories never serialize against each other, and the
// claim queue's `FOR UPDATE OF qm SKIP LOCKED` is unaffected. This generalizes
// the RFC 0101 Phase 0a per-run interrogation lock (formerly
// lockRunInterrogation) to the whole run aggregate.
func lockRun(ctx context.Context, runner db.TxRunner, repositoryID, runID string) error {
	key := "striatum:run:" + repositoryID + ":" + runID
	return runner.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key)
}

// LockRun is the exported entry to the per-run advisory lock for the one
// per-run write verb that lives outside this package — escalation.resolve,
// registered in pkg/reads (which cannot import pkg/mutations: mutations imports
// reads). It delegates to the unexported lockRun so the advisory-lock KEY is
// computed in exactly one place and always matches every in-package per-run
// mutation; callers must NOT re-derive the key by hand. Wired into reads via
// reads.RunLockHook in Register, mirroring the RunCompletionRedriveHook
// indirection. Like lockRun, it MUST be the first statement of the per-run
// transaction, before any blocking FOR UPDATE on a run-scoped row (RFC 0104).
func LockRun(ctx context.Context, runner db.TxRunner, repositoryID, runID string) error {
	return lockRun(ctx, runner, repositoryID, runID)
}

// lockRepo serializes a mutation that must reason about ALL runs on a repository
// at once — the RFC 0108 Phase 2 run.start isolation precondition, which reads
// "is a sibling run already active?" then mutates run state, and must not let two
// concurrent run.starts both observe a stale "no sibling" snapshot and race onto
// the shared main checkout. The key is per (repository_id) — strictly WIDER than
// lockRun's per-(repository_id, run_id). No mutation takes BOTH lockRepo and
// lockRun, so the two advisory locks cannot form a lock-ordering cycle; the only
// shared resource a lockRepo holder and a lockRun holder both contend on is the
// per-repo event-chain head row, which every appendEvent takes last, so no cycle
// forms there either.
func lockRepo(ctx context.Context, runner db.TxRunner, repositoryID string) error {
	key := "striatum:repo:" + repositoryID
	return runner.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key)
}

// lockRunForSession resolves a session's (immutable) run_id with a non-locking
// read and takes lockRun before any run-scoped FOR UPDATE (RFC 0104). A missing
// session takes no lock and lets the downstream FOR UPDATE surface the canonical
// not-found error, preserving prior behavior.
func lockRunForSession(ctx context.Context, runner db.TxRunner, repositoryID, sessionID string) error {
	runID, err := sessionRunID(ctx, runner, repositoryID, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	return lockRun(ctx, runner, repositoryID, runID)
}

// lockRunForJob resolves a job's (immutable) run_id with a non-locking read and
// takes lockRun before any run-scoped FOR UPDATE (RFC 0104). A missing job takes
// no lock and lets the downstream FOR UPDATE surface the canonical not-found
// error, preserving prior behavior.
func lockRunForJob(ctx context.Context, runner db.TxRunner, repositoryID, jobID string) error {
	row, err := oneRow(ctx, runner, `SELECT run_id FROM striatumd.jobs WHERE repository_id = $1 AND job_id = $2`, repositoryID, jobID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	return lockRun(ctx, runner, repositoryID, fmt.Sprint(row["run_id"]))
}

// lockRunForBlocker resolves a blocker's (immutable) run_id with a non-locking
// read and takes lockRun before any run-scoped FOR UPDATE (RFC 0104). A missing
// blocker takes no lock and lets the downstream FOR UPDATE surface the canonical
// not-found error.
func lockRunForBlocker(ctx context.Context, runner db.TxRunner, repositoryID, blockerID string) error {
	row, err := oneRow(ctx, runner, `SELECT run_id FROM striatumd.blockers WHERE repository_id = $1 AND blocker_id = $2`, repositoryID, blockerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	return lockRun(ctx, runner, repositoryID, fmt.Sprint(row["run_id"]))
}

func rowByID(ctx context.Context, runner any, repositoryID, table, column, value string, forUpdate bool) (map[string]any, error) {
	suffix := ""
	if forUpdate {
		suffix = " FOR UPDATE"
	}
	return oneRow(ctx, runner, fmt.Sprintf(
		"SELECT * FROM striatumd.%s WHERE repository_id = $1 AND %s = $2%s",
		table,
		column,
		suffix,
	), repositoryID, value)
}

func oneRow(ctx context.Context, runner any, sql string, args ...any) (map[string]any, error) {
	q, ok := runner.(queryer)
	if !ok {
		return nil, fmt.Errorf("runner does not support row queries")
	}
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, pgx.ErrNoRows
	}
	return items[0], nil
}

func queryRows(ctx context.Context, runner any, sql string, args ...any) ([]map[string]any, error) {
	q, ok := runner.(queryer)
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

func nowString() string {
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func expiresAfter(seconds int) string {
	return time.Now().UTC().Add(time.Duration(seconds) * time.Second).Truncate(time.Second).Format(time.RFC3339)
}

func newID(prefix string) (string, error) {
	var body [16]byte
	if _, err := rand.Read(body[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(body[:]), nil
}

func activeLeaseFor(ctx context.Context, runner any, repositoryID, leaseID, sessionID, jobID string) (map[string]any, error) {
	lease, err := rowByID(ctx, runner, repositoryID, "leases", "lease_id", leaseID, true)
	if err != nil {
		return nil, err
	}
	if fmt.Sprint(lease["state"]) != "active" {
		return nil, rpc.NewError("lease_error", "lease is not active", nil)
	}
	if fmt.Sprint(lease["owner_session_id"]) != sessionID {
		return nil, rpc.NewError("lease_error", "lease is owned by another session", nil)
	}
	if jobID != "" && fmt.Sprint(lease["resource_id"]) != jobID {
		return nil, rpc.NewError("lease_error", "lease does not belong to the job", nil)
	}
	if expires, ok := asTime(lease["expires_at"]); ok && expires.UTC().Before(time.Now().UTC()) {
		return nil, rpc.NewError("lease_error", "lease is expired", nil)
	}
	return lease, nil
}

func isRepoWrite(job map[string]any) bool {
	scope := asMap(job["write_scope_json"])
	return scope["repo_write"] == true || scope["mode"] == "repo_write"
}

func verifyRequiredArtifacts(ctx context.Context, runner any, repositoryID, jobID string) error {
	job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, false)
	if err != nil {
		return err
	}
	// Build finding 1: resolve cycle placeholders against the job attempt so a
	// revision-cycle re-run verifies the attempt-scoped logical name + path the
	// adjudicator actually published to (cycle_<attempt>), not the raw template.
	attempt := jobAttemptValue(job["attempt"])
	expected := resolveExpectedArtifactCycles(asList(job["expected_artifacts_json"]), attempt)
	if len(expected) == 0 {
		return nil
	}
	for _, item := range expected {
		artifact := asMap(item)
		if artifact["required"] != true {
			continue
		}
		// RFC 0095 §1 / #84: a required artifact is satisfied ONLY by an artifact
		// produced at the job's CURRENT attempt. A prior attempt's artifact must
		// not satisfy a re-opened attempt's gate, so the revision gate cannot be
		// cleared by stale output — the fresh attempt must (re)publish its own
		// attempt-scoped artifact.
		_, err := oneRow(ctx, runner, `
			SELECT 1 FROM striatumd.artifacts
			 WHERE repository_id = $1 AND job_id = $2 AND logical_name = $3
			   AND artifact_kind = $4 AND repo_path = $5 AND attempt = $6
			 LIMIT 1`,
			repositoryID,
			jobID,
			artifact["logical_name"],
			artifact["kind"],
			artifact["path"],
			attempt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return rpc.NewError("invalid_transition", fmt.Sprintf(
				"required artifact is missing: logical_name=%q, kind=%q, path=%q",
				artifact["logical_name"],
				artifact["kind"],
				artifact["path"],
			), nil)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func maybeEnqueueDownstream(ctx context.Context, runner any, repositoryID, completedJobID string) error {
	completed, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", completedJobID, false)
	if err != nil {
		return err
	}
	rows, err := queryRows(ctx, runner, `
		SELECT job_id FROM striatumd.job_dependencies
		 WHERE repository_id = $1 AND depends_on_job_id = $2`, repositoryID, completedJobID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		jobID := fmt.Sprint(row["job_id"])
		job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, true)
		if err != nil {
			return err
		}
		if fmt.Sprint(job["state"]) != "blocked" {
			continue
		}
		satisfied, err := dependenciesSatisfied(ctx, runner, repositoryID, jobID)
		if err != nil {
			return err
		}
		if satisfied {
			if _, err := enqueueJob(ctx, runner, repositoryID, jobID); err != nil {
				return err
			}
		}
	}
	if err := enqueueReadyBlockedJobs(ctx, runner, repositoryID, fmt.Sprint(completed["run_id"])); err != nil {
		return err
	}
	return nil
}

func enqueueReadyBlockedJobs(ctx context.Context, runner any, repositoryID, runID string) error {
	rows, err := queryRows(ctx, runner, `
		SELECT job_id
		  FROM striatumd.jobs
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND state = 'blocked'
		 ORDER BY created_at, job_id
		 FOR UPDATE`, repositoryID, runID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		jobID := fmt.Sprint(row["job_id"])
		job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, true)
		if err != nil {
			return err
		}
		if fmt.Sprint(job["state"]) != "blocked" {
			continue
		}
		satisfied, err := dependenciesSatisfied(ctx, runner, repositoryID, jobID)
		if err != nil {
			return err
		}
		if satisfied {
			if _, err := enqueueJob(ctx, runner, repositoryID, jobID); err != nil {
				return err
			}
		}
	}
	return nil
}

func dependenciesSatisfied(ctx context.Context, runner any, repositoryID, jobID string) (bool, error) {
	// RFC 0132 Layer C / P4b (#341): an open advisory-guard blocker on this gate job
	// HOLDS the line — the gate cannot enqueue (and so the run cannot finalize) until
	// an operator resolves it (checkpoint resolve). This is how an advisory reviewer
	// stops the line without ever auto-flipping a verdict. A gate with no advisory
	// seats never carries such a blocker, so default behavior is unchanged.
	heldByAdvisory, err := gateHeldByOpenAdvisoryGuard(ctx, runner, repositoryID, jobID)
	if err != nil {
		return false, err
	}
	if heldByAdvisory {
		return false, nil
	}
	// RFC 0135 P4 cutover (#354, D216): when the gate declares a GATING review-seat
	// panel, the panel-quorum sealed barrier (entity=review_seat, seal=attempt)
	// COMPOSES WITH the edge-by-edge gate over the FROZEN declared-seat denominator. The
	// barrier is keyed on the stable workflow_job_id (recovery churns job_id), classifies
	// each seat by the live seal, honors the live dissent_ledger, and skips only a
	// provably-dead seat within the abstention budget (D214).
	//
	// COMPOSE, do NOT replace (the cutover-regression fix): the barrier MUST NOT block a
	// review edge the legacy edge-by-edge gate UNBLOCKS — that regressed the
	// revision-routing / checkpoint-override / blocked-sibling-requeue scenarios where a
	// settled seat carries a superseding clearing verdict, no required verdict at all, or
	// an append-only dissent the override cleared by supersession. So each governed seat
	// is resolved to a per-seat state and the edge-by-edge `latestVerdict` check is kept
	// as the floor; the barrier only ever ADDS the ONE strictness the seal-blind legacy
	// gate misses — refusing a STALE-SEAL accept (the trap, panelSeat.staleSealTrap) —
	// and only ever RELAXES via the explicit abstention budget (a provably-dead seat
	// within budget, panelSeat.terminalGap). With the DEFAULT budget 0 and all-gating
	// seats this is IDENTICAL to the edge-by-edge gate except it kills the stale-seal
	// trap — proven by TestPanelQuorumCutoverEqualsEdgeByEdge.
	//
	// Fallback: STRIATUM_BARRIER_QUORUM=0 forces the legacy edge-by-edge path for every
	// gate (the recoverable kill switch), so a quorum regression is reversible without a
	// redeploy of older code.
	governedSeats := map[string]panelSeat{}
	if barrierQuorumEnabled() {
		resolved, err := resolveGovernedSeats(ctx, runner, repositoryID, jobID)
		if err != nil {
			return false, err
		}
		governedSeats = resolved
	}

	deps, err := queryRows(ctx, runner, `
		SELECT * FROM striatumd.job_dependencies
		 WHERE repository_id = $1 AND job_id = $2`, repositoryID, jobID)
	if err != nil {
		return false, err
	}
	for _, dep := range deps {
		upstream, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", fmt.Sprint(dep["depends_on_job_id"]), false)
		if err != nil {
			return false, err
		}
		if fmt.Sprint(upstream["state"]) != "completed" {
			return false, nil
		}
		required := requiredVerdicts(asMap(dep["gate_json"])["requires_verdict"])
		if len(required) == 0 {
			continue
		}
		seatID := fmt.Sprint(upstream["workflow_job_id"])
		seat, governed := governedSeats[seatID]
		// A seat the barrier governs (a GATING review seat in the frozen denominator)
		// whose edge requires exactly the standard accepting set is COMPOSED, not
		// short-circuited: a provably-dead seat the abstention budget admits passes (the
		// only relaxation), then the edge-by-edge floor below runs unchanged, and the
		// barrier's only added strictness — refusing a STALE-SEAL accept — is applied
		// AFTER the floor admits. A non-standard requires_verdict still gates purely
		// edge-by-edge below, so the barrier can never disagree with an edge's declared
		// requirement.
		barrierGoverns := governed && isStandardAcceptingGate(required)
		if barrierGoverns && seat.terminalGap() {
			// Provably-dead seat within the abstention budget (D214b): the barrier
			// RELAXES the gate so the run is not deadlocked on a dead lane.
			continue
		}
		latest, err := latestVerdict(ctx, runner, repositoryID, fmt.Sprint(upstream["job_id"]))
		if err != nil {
			return false, err
		}
		if !required[latest] {
			return false, nil
		}
		// The edge-by-edge floor admitted this seat. The barrier ADDS exactly one
		// strictness the seal-blind legacy gate misses: if the seat's accepting verdict
		// is STALE (its live attempt advanced past the accept), refuse — the trap-killer.
		// For an override (accept at the live seal) or an absorb (no required verdict, not
		// reached here) the seat is not a stale-seal trap, so the floor's admit stands and
		// the barrier never regresses the legacy unblock.
		if barrierGoverns && seat.staleSealTrap() {
			return false, nil
		}
	}
	return true, nil
}

// isStandardAcceptingGate reports whether a gate's requires_verdict set is exactly
// the accepting set {accept, accept_with_findings} the quorum barrier treats as
// satisfying. Only such an edge may be governed by the barrier without any risk of
// disagreeing with the edge-by-edge requirement; a non-standard requires_verdict
// keeps the edge on the legacy edge-by-edge path.
func isStandardAcceptingGate(required map[string]bool) bool {
	return len(required) == 2 && required["accept"] && required["accept_with_findings"]
}

// barrierQuorumEnabled reports whether the RFC 0135 P4 panel-quorum barrier governs
// paneled review gates (the default). STRIATUM_BARRIER_QUORUM=0 forces the legacy
// edge-by-edge path — the recoverable kill switch the cutover keeps so a quorum
// regression is reversible without redeploying older code. Any value other than the
// explicit "0" leaves the barrier ON.
func barrierQuorumEnabled() bool {
	return os.Getenv("STRIATUM_BARRIER_QUORUM") != "0"
}

func requiredVerdicts(value any) map[string]bool {
	result := map[string]bool{}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			result[fmt.Sprint(item)] = true
		}
	case []string:
		for _, item := range typed {
			result[item] = true
		}
	}
	return result
}

func asList(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	case []byte:
		var result []any
		_ = json.Unmarshal(typed, &result)
		return result
	case string:
		var result []any
		_ = json.Unmarshal([]byte(typed), &result)
		return result
	}
	return nil
}

func latestVerdict(ctx context.Context, runner any, repositoryID, jobID string) (string, error) {
	// Superseded rows (checkpoint.override supersession, RFC 0118 P1-6
	// invalidation receipts) never gate anything: the active verdict is the
	// newest non-superseded row.
	row, err := oneRow(ctx, runner, `
		SELECT verdict FROM striatumd.verdicts
		 WHERE repository_id = $1 AND job_id = $2
		   AND superseded_by_decision_id IS NULL
		 ORDER BY created_at DESC, verdict_id DESC
		 LIMIT 1`, repositoryID, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprint(row["verdict"]), nil
}

func enqueueJob(ctx context.Context, runner any, repositoryID, jobID string) (string, error) {
	job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		return "", err
	}
	state := fmt.Sprint(job["state"])
	if state != "blocked" && state != "queued" {
		return "", rpc.NewError("invalid_transition", "job is not enqueueable", nil)
	}
	messageID, err := newID("msg")
	if err != nil {
		return "", err
	}
	now := nowString()
	selector := asMap(job["lane_selector_json"])
	targetLane, _ := selector["lane_id"].(string)
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return "", fmt.Errorf("runner does not support exec")
	}
	payloadArg, err := db.JSONBArg(runner, map[string]any{})
	if err != nil {
		return "", err
	}
	if err := exec.Exec(ctx, `
		INSERT INTO striatumd.queue_messages (
		  repository_id, message_id, run_id, job_id, kind, state, priority,
		  target_role_id, target_lane_id, payload_json, claim_count,
		  max_claims, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,'work','pending',0,$5,$6,$7::jsonb,0,$8,$9,$10)`,
		repositoryID,
		messageID,
		job["run_id"],
		jobID,
		job["role_id"],
		nullable(targetLane),
		payloadArg,
		job["max_attempts"],
		now,
		now,
	); err != nil {
		return "", err
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'queued', current_message_id = $1, ready_at = $2
		 WHERE repository_id = $3 AND job_id = $4`, messageID, now, repositoryID, jobID); err != nil {
		return "", err
	}
	if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "queue.message_enqueued", nil, jobID, messageID, nil, nil, map[string]any{
		"workflow_job_id": job["workflow_job_id"],
	}); err != nil {
		return "", err
	}
	recordWake(runner, WakeEvent{
		RepositoryID: repositoryID,
		RunID:        fmt.Sprint(job["run_id"]),
		Kind:         "work_available",
		MessageID:    messageID,
	})
	return messageID, nil
}

func maybeCompleteRun(ctx context.Context, runner any, repositoryID, runID string) error {
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", runID, true)
	if err != nil {
		return err
	}
	failed, err := existsRow(ctx, runner, `
		SELECT 1 FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2 AND state = 'failed'
		 LIMIT 1`, repositoryID, runID)
	if err != nil {
		return err
	}
	// #311 P0: 'quarantined' is excluded from the non-terminal `remaining` set so
	// the run can finalize-the-majority on its completed deliverables while a
	// single recovery-exhausted, downstream-clear job sits quarantined (NEVER
	// completed — no false completion). The quarantined job is the one narrow
	// thing surfaced to the operator (recovery accept-quarantined terminalizes
	// it). It is intentionally NOT in 'completed'/'skipped'/'canceled' so the run
	// records a quarantine manifest and stop_reason='quarantined_jobs'.
	remaining, err := existsRow(ctx, runner, `
		SELECT 1 FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2
		   AND state NOT IN ('completed','skipped','canceled','quarantined')
		 LIMIT 1`, repositoryID, runID)
	if err != nil {
		return err
	}
	now := nowString()
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	if failed && fmt.Sprint(run["state"]) == "running" {
		failedPayload, err := freezeRunCompletionRecord(ctx, runner, repositoryID, runID, "failed", "run_failed",
			map[string]any{"stop_reason": "job_failed"}, map[string]any{"reason": "job_failed"})
		if err != nil {
			return err
		}
		if err := exec.Exec(ctx, `
			UPDATE striatumd.runs
			   SET state = 'failed', completed_at = $1, stop_reason = 'job_failed'
			 WHERE repository_id = $2 AND run_id = $3`, now, repositoryID, runID); err != nil {
			return err
		}
		if _, err := appendEvent(ctx, runner, repositoryID, runID, "run.failed", nil, nil, nil, nil, nil, failedPayload); err != nil {
			return err
		}
		return closeRemainingSessions(ctx, runner, repositoryID, runID, "run_failed", "run_failed")
	}
	if remaining || fmt.Sprint(run["state"]) != "running" {
		return nil
	}
	hasCompleted, err := existsRow(ctx, runner, `
		SELECT 1 FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2 AND state = 'completed'
		 LIMIT 1`, repositoryID, runID)
	if err != nil {
		return err
	}
	// #311 P0: collect the quarantine manifest (the single offending job(s) +
	// lane + stall_class + blocker) so the terminal run.completed/run.canceled
	// event and the durable run_completion_record name exactly what flaked.
	quarantineManifest, err := buildQuarantineManifest(ctx, runner, repositoryID, runID)
	if err != nil {
		return err
	}
	state := "canceled"
	eventType := "run.canceled"
	source := "run_canceled"
	reason := "run_canceled"
	// payload is assigned in both branches below (completionPayload or
	// canceledPayload) before its single use at appendEvent, so it has no
	// meaningful initial value here.
	var payload map[string]any
	if hasCompleted {
		state = "completed"
		eventType = "run.completed"
		source = "run_completed"
		reason = "run_completed"
	}
	if state == "completed" {
		// RFC 0118 P0-3: re-verify every provenance-required review gate
		// against the frozen verdict stamp before choosing a clean completion.
		// A failing gate routes the run to needs_operator instead of completed;
		// the ledger of passing gates is recorded on the run.completed event.
		ledger, failingGates, err := verifyRunCompletionProvenance(ctx, runner, repositoryID, runID)
		if err != nil {
			return err
		}
		if len(failingGates) > 0 {
			return escalateProvenanceGateFailure(ctx, runner, repositoryID, runID, failingGates)
		}
		completionMode := "lanes_attested"
		for _, entry := range ledger {
			if entry["basis"] == "override" {
				completionMode = "operator_override"
				break
			}
		}
		completionPayload := map[string]any{
			"completion_mode": completionMode,
			"provenance_gate": ledger,
		}
		recordExtra := map[string]any{"completion_mode": completionMode, "provenance_gate": ledger}
		// RFC 0134 / D227: the executable-half gate READ. Load the run's
		// claim_ledger + the receipts its VERIFIED claims name and record the
		// EFFECTIVE claim status (two-signal sealed receipt → VERIFIED; missing /
		// wedged / non-strict / single-signal → ASSERTED). This is a PURE READ:
		// it executes nothing, adds NO failing gate, and never blocks completion
		// on engine liveness — it only makes the verified-vs-asserted ledger
		// durable on the completion event.
		claimVerification, cverr := evaluateRunClaimVerification(ctx, runner, repositoryID, runID)
		if cverr != nil {
			return cverr
		}
		if len(claimVerification) > 0 {
			completionPayload["claim_verification"] = claimVerification
			recordExtra["claim_verification"] = claimVerification
		}
		// #311 P0: a clean completion that left a single recovery-exhausted,
		// downstream-clear job quarantined finalizes-the-majority — carry the
		// manifest and set stop_reason='quarantined_jobs' (instead of NULL) so the
		// run records exactly what was set aside. The state stays 'completed' (no
		// new run state); the manifest is the legibility surface.
		stopReasonClause := "stop_reason = NULL"
		if len(quarantineManifest) > 0 {
			completionPayload["quarantine_manifest"] = quarantineManifest
			recordExtra["quarantine_manifest"] = quarantineManifest
			recordExtra["stop_reason"] = "quarantined_jobs"
			stopReasonClause = "stop_reason = 'quarantined_jobs'"
		}
		payload = completionPayload
		payload, err = freezeRunCompletionRecord(ctx, runner, repositoryID, runID, "completed", "run_completed",
			recordExtra, payload)
		if err != nil {
			return err
		}
		// stop_reason may carry 'provenance_gate_failed' from an earlier
		// escalation of this run; a clean (re-driven) completion clears it unless
		// a quarantine manifest replaces it (#311).
		if err := exec.Exec(ctx, `
			UPDATE striatumd.runs
			   SET state = $1, completed_at = $2, `+stopReasonClause+`,
			       completion_mode = $3
			 WHERE repository_id = $4 AND run_id = $5`, state, now, completionMode, repositoryID, runID); err != nil {
			return err
		}
	} else {
		canceledStopReason := "all_jobs_canceled"
		canceledPayload := map[string]any{"reason": "all_jobs_canceled"}
		canceledExtra := map[string]any{"stop_reason": "all_jobs_canceled"}
		// #311 P0: if there are quarantined jobs but no completed deliverables, the
		// run still cancels (nothing to finalize-the-majority on) but the manifest
		// + quarantined_jobs stop_reason name what flaked.
		if len(quarantineManifest) > 0 {
			canceledStopReason = "quarantined_jobs"
			canceledPayload["quarantine_manifest"] = quarantineManifest
			canceledExtra["stop_reason"] = "quarantined_jobs"
		}
		payload = canceledPayload
		payload, err = freezeRunCompletionRecord(ctx, runner, repositoryID, runID, state, reason,
			canceledExtra, payload)
		if err != nil {
			return err
		}
		if err := exec.Exec(ctx, `
			UPDATE striatumd.runs
			   SET state = $1, completed_at = $2, stop_reason = $3
			 WHERE repository_id = $4 AND run_id = $5`, state, now, canceledStopReason, repositoryID, runID); err != nil {
			return err
		}
	}
	if _, err := appendEvent(ctx, runner, repositoryID, runID, eventType, nil, nil, nil, nil, nil, payload); err != nil {
		return err
	}
	return closeRemainingSessions(ctx, runner, repositoryID, runID, source, reason)
}

// buildQuarantineManifest assembles the #311 P0 quarantine manifest: one entry
// per job left in the 'quarantined' state for the run, naming the single
// offending job + lane + stall_class + recovery counters + its open
// recovery_exhausted blocker. It is folded into the terminal
// run.completed/run.canceled event payload and the durable run_completion_record
// so a finalize-the-majority run records exactly which job(s) were set aside and
// why. Returns an empty slice when no job is quarantined (the common case),
// which the caller treats as "no manifest".
func buildQuarantineManifest(ctx context.Context, runner any, repositoryID, runID string) ([]map[string]any, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT j.job_id, j.workflow_job_id,
		       NULLIF(j.lane_selector_json->>'lane_id','') AS lane,
		       jrs.last_stall_class, jrs.requeue_count, jrs.transfer_count,
		       jrs.respawn_count,
		       b.blocker_id, b.blocker_kind
		  FROM striatumd.jobs j
		  LEFT JOIN striatumd.job_recovery_state jrs
		    ON jrs.repository_id = j.repository_id AND jrs.job_id = j.job_id
		  LEFT JOIN LATERAL (
		    SELECT bz.blocker_id, bz.blocker_kind
		      FROM striatumd.blockers bz
		     WHERE bz.repository_id = j.repository_id AND bz.job_id = j.job_id
		       AND bz.state = 'open'
		     ORDER BY (bz.blocker_kind = 'recovery_exhausted') DESC, bz.created_at
		     LIMIT 1
		  ) b ON true
		 WHERE j.repository_id = $1 AND j.run_id = $2 AND j.state = 'quarantined'
		 ORDER BY j.workflow_job_id`, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	manifest := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"job_id":          fmt.Sprint(row["job_id"]),
			"workflow_job_id": fmt.Sprint(row["workflow_job_id"]),
			"state":           "quarantined",
		}
		if v := nullable(row["lane"]); v != nil {
			entry["lane"] = fmt.Sprint(v)
		}
		if v := nullable(row["last_stall_class"]); v != nil {
			entry["stall_class"] = fmt.Sprint(v)
		}
		entry["recovery_attempts"] = intFromAny(row["requeue_count"], 0) +
			intFromAny(row["transfer_count"], 0) + intFromAny(row["respawn_count"], 0)
		if v := nullable(row["blocker_id"]); v != nil {
			entry["blocker_id"] = fmt.Sprint(v)
		}
		if v := nullable(row["blocker_kind"]); v != nil {
			entry["blocker_kind"] = fmt.Sprint(v)
		}
		manifest = append(manifest, entry)
	}
	return manifest, nil
}

func existsRow(ctx context.Context, runner any, sql string, args ...any) (bool, error) {
	_, err := oneRow(ctx, runner, sql, args...)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func closeRemainingSessions(ctx context.Context, runner any, repositoryID, runID, source, reason string) error {
	rows, err := queryRows(ctx, runner, `
		SELECT * FROM striatumd.sessions
		 WHERE repository_id = $1 AND run_id = $2 AND state = 'active'
		 ORDER BY registered_at
		 FOR UPDATE`, repositoryID, runID)
	if err != nil {
		return err
	}
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	now := nowString()
	for _, row := range rows {
		sessionID := fmt.Sprint(row["session_id"])
		activeLease, err := existsRow(ctx, runner, `
			SELECT 1 FROM striatumd.leases
			 WHERE repository_id = $1 AND owner_session_id = $2 AND state = 'active'
			 LIMIT 1`, repositoryID, sessionID)
		if err != nil {
			return err
		}
		if activeLease {
			continue
		}
		if err := stopSupervisorsForTerminalSession(ctx, runner, terminalSessionSupervisorStop{
			RepositoryID:     repositoryID,
			RunID:            runID,
			SessionID:        sessionID,
			EndedAt:          now,
			Reason:           reason,
			StopReasonPrefix: "terminal run cleanup",
			EventSource:      "terminal_run_cleanup",
		}); err != nil {
			return err
		}
		if err := exec.Exec(ctx, `
			UPDATE striatumd.sessions
			   SET state = 'closed', closed_at = $1, close_reason = $2
			 WHERE repository_id = $3 AND session_id = $4`, now, reason, repositoryID, sessionID); err != nil {
			return err
		}
		if _, err := appendEvent(ctx, runner, repositoryID, runID, "session.closed", sessionID, nil, nil, nil, nil, map[string]any{
			"session_id": sessionID,
			"role_id":    row["role_id"],
			"lane_id":    row["lane_id"],
			"reason":     reason,
			"source":     source,
		}); err != nil {
			return err
		}
	}
	// #417 backstop: the loop above only reaps supervisors for sessions still in
	// state='active' (and skips active-lease holders). A lane that dies abnormally
	// has its session 'closed' before the run terminates, so its supervisor is
	// never reached and lingers in a live state ('starting'/'attached'/'detached')
	// — the dominant leak (511 of 562 stranded supervisors had an already-closed
	// session in the #417 incident). The status/dashboard read path LEFT-JOINs
	// process_supervisors ON state='attached' and sudo+tmux ProbeLaneLiveness each
	// one, so the stranded rows storm a repo-wide read until it blows the CLI read
	// deadline. Reap every supervisor still live for this now-terminal run,
	// regardless of its session's state or any lingering lease.
	orphanSessions, err := queryRows(ctx, runner, `
		SELECT DISTINCT session_id
		  FROM striatumd.process_supervisors
		 WHERE repository_id = $1 AND run_id = $2
		   AND state IN ('starting','attached','detached')
		 ORDER BY session_id`, repositoryID, runID)
	if err != nil {
		return err
	}
	for _, row := range orphanSessions {
		sessionID := fmt.Sprint(row["session_id"])
		if sessionID == "" {
			continue
		}
		if err := stopSupervisorsForTerminalSession(ctx, runner, terminalSessionSupervisorStop{
			RepositoryID:     repositoryID,
			RunID:            runID,
			SessionID:        sessionID,
			EndedAt:          now,
			Reason:           reason,
			StopReasonPrefix: "terminal run cleanup (orphan supervisor)",
			EventSource:      "terminal_run_cleanup_orphan",
		}); err != nil {
			return err
		}
	}
	// #420: resolve every still-open blocker for this now-terminal run. The work a
	// blocker gated is over once the run is terminal, so leaving it 'open' makes a
	// dead run read as pending operator work (the #419 read-side scoping hides it,
	// but the row itself stayed 'open' forever).
	if err := resolveTerminalRunOpenBlockers(ctx, runner, repositoryID, runID, now, reason); err != nil {
		return err
	}
	return nil
}

// resolveTerminalRunOpenBlockers resolves every still-open blocker for a run that
// has reached a terminal state (#420). Unlike `recovery resolve-blocker`, which
// REFUSES human_checkpoint / escalation-class blockers because a LIVE run must
// adjudicate them through checkpoint.resolve / escalation.resolve (those record an
// adjudicating decision), the terminal transition resolves ALL kinds: once the run
// is dead the adjudication obligation is moot, so the honest record is "resolved by
// the terminal cause", NOT a forged decision. It only ever runs from the terminal
// cleanup path, so it can never short-circuit a live run's adjudication. The
// escalation_inbox mirror is kept in lock-step so the two surfaces never disagree
// (the same invariant recovery.resolve_blocker maintains).
func resolveTerminalRunOpenBlockers(ctx context.Context, runner any, repositoryID, runID, now, reason string) error {
	rows, err := queryRows(ctx, runner, `
		UPDATE striatumd.blockers
		   SET state = 'resolved', resolved_at = $1
		 WHERE repository_id = $2 AND run_id = $3 AND state = 'open'
		 RETURNING blocker_id, severity, blocker_kind, job_id`,
		now, repositoryID, runID)
	if err != nil {
		return err
	}
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	for _, row := range rows {
		blockerID := fmt.Sprint(row["blocker_id"])
		if blockerID == "" {
			continue
		}
		// A plain autonomous blocker has no escalation_inbox row; an escalation-class
		// one does. Resolve any matching row so the two surfaces stay consistent.
		if err := exec.Exec(ctx, `
			UPDATE striatumd.escalation_inbox
			   SET state = 'resolved', resolved_at = $1
			 WHERE repository_id = $2 AND escalation_id = $3 AND state <> 'resolved'`,
			now, repositoryID, blockerID); err != nil {
			return err
		}
		if _, err := appendEvent(ctx, runner, repositoryID, runID, "blocker.resolved", nil, nullable(row["job_id"]), nil, nil, nil, map[string]any{
			"blocker_id":   blockerID,
			"severity":     row["severity"],
			"blocker_kind": row["blocker_kind"],
			"reason":       reason,
			"source":       "terminal_run_cleanup",
		}); err != nil {
			return err
		}
	}
	return nil
}

type terminalSessionSupervisorStop struct {
	RepositoryID     string
	RunID            string
	SessionID        string
	EndedAt          string
	Reason           string
	StopReasonPrefix string
	EventSource      string
}

func stopSupervisorsForTerminalSession(ctx context.Context, runner any, stop terminalSessionSupervisorStop) error {
	tx, ok := runner.(db.TxRunner)
	if !ok {
		return fmt.Errorf("runner does not support supervisor terminal cleanup")
	}
	rows, err := queryRows(ctx, runner, `
		SELECT ps.supervisor_id,
		       ps.pid,
		       ps.pid_start_time,
		       ps.stdin_pipe_path,
		       p.daemon_supervisor_id,
		       p.metadata_json AS pointer_metadata_json
		  FROM striatumd.process_supervisors ps
		  LEFT JOIN striatumd.process_supervisor_pointers p
		    ON p.repository_id = ps.repository_id
		   AND p.supervisor_id = ps.supervisor_id
		 WHERE ps.repository_id = $1
		   AND ps.run_id = $2
		   AND ps.session_id = $3
		   AND ps.state IN ('starting','attached','detached')
		 ORDER BY ps.started_at DESC, ps.supervisor_id DESC
		 FOR UPDATE OF ps`,
		stop.RepositoryID, stop.RunID, stop.SessionID,
	)
	if err != nil {
		return err
	}
	stopReason := stop.StopReasonPrefix + ": " + stop.Reason
	for _, row := range rows {
		supervisorID := metadataString(row["supervisor_id"])
		if supervisorID == "" {
			continue
		}
		daemonSupervisorID := metadataString(row["daemon_supervisor_id"])
		pid, hasPID := intValueOptional(row["pid"])
		pidStart := metadataString(row["pid_start_time"])
		metadata := asMap(row["pointer_metadata_json"])
		eventExtra := map[string]any{}
		var signaled any
		if tmuxIdentity, ok := gosupervisor.TmuxIdentityFromMetadata(metadata); ok {
			signal, note, fallbackReason, cleanupSkip := stopTmuxBackedLane(ctx, metadata, tmuxIdentity, pid, pidStart)
			signaled = signal
			if note != "" {
				eventExtra["tmux_stop_note"] = note
			}
			if fallbackReason != "" {
				eventExtra["tmux_kill_fallback_reason"] = fallbackReason
			}
			if cleanupSkip != "" {
				eventExtra["pane_pid_cleanup_skipped_reason"] = cleanupSkip
			}
		} else if hasPID {
			signal, cleanupSkip := terminateProcessWithStartToken(pid, pidStart)
			signaled = signal
			if cleanupSkip != "" {
				eventExtra["pid_cleanup_skipped_reason"] = cleanupSkip
			}
		}
		if helperPID, ok := intValueOptional(metadata["helper_pid"]); ok && (!hasPID || helperPID != pid) {
			helperSignal, cleanupSkip := terminateProcessWithStartToken(helperPID, metadataString(metadata["helper_pid_start_time"]))
			if helperSignal != nil {
				eventExtra["helper_signal"] = helperSignal
			}
			if cleanupSkip != "" {
				eventExtra["helper_pid_cleanup_skipped_reason"] = cleanupSkip
			}
		}
		if stdinPipePath := metadataString(row["stdin_pipe_path"]); stdinPipePath != "" {
			_ = os.Remove(stdinPipePath)
		}
		if err := updateSupervisorState(ctx, tx, stop.RepositoryID, supervisorID, daemonSupervisorID, "stopped", stop.EndedAt, 0, "", "", &stop.EndedAt, &stopReason); err != nil {
			return err
		}
		payload := map[string]any{
			"supervisor_id":        supervisorID,
			"daemon_supervisor_id": nullableString(daemonSupervisorID),
			"session_id":           stop.SessionID,
			"reason":               stop.Reason,
			"source":               stop.EventSource,
		}
		if signaled != nil {
			payload["signal"] = signaled
		}
		for key, value := range eventExtra {
			payload[key] = value
		}
		if _, err := appendEvent(ctx, runner, stop.RepositoryID, stop.RunID, "supervisor.stopped", stop.SessionID, nil, nil, nil, nil, payload); err != nil {
			return err
		}
	}
	return nil
}

func sessionLaneAttestation(ctx context.Context, runner any, repositoryID, sessionID string) map[string]any {
	checker := lanehealth.Checker{
		Probe: lanehealth.ProdProbe{Runner: supervisionTmuxRunner},
	}
	health, err := checker.Check(ctx, runner, repositoryID, sessionID)
	if err != nil {
		return map[string]any{
			"attested":      false,
			"state":         "unattested",
			"supervisor_id": nil,
			"pid":           nil,
			"reason":        "no_attached_supervisor",
		}
	}
	return lanehealth.LegacyMap(health)
}

func appendEvent(
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
	// RFC 0110 §7 P2 (full): once the operator declares the full write boundary
	// the event append routes through the owner-owned SECURITY DEFINER
	// append_event_row, which asserts daemon authority, enforces transcript
	// exclusion, computes the v3 chain hash in-DB, and advances the chain head —
	// atomic with this mutation. Behavior-neutral until P2 is adopted (the direct
	// path below is unchanged). Foreign keys are null-coerced here so the SD
	// path's stored row matches the direct INSERT.
	if db.ActiveWriteBoundary().AtLeast(db.PhaseFull) {
		return db.AppendEventRowSD(ctx, runner, db.EventRow{
			RepositoryID:   repositoryID,
			RunID:          nullable(runID),
			EventType:      eventType,
			ActorSessionID: nullable(actorSessionID),
			JobID:          nullable(jobID),
			MessageID:      nullable(messageID),
			ArtifactID:     nullable(artifactID),
			LeaseID:        nullable(leaseID),
			Payload:        payload,
		})
	}
	// Schema v6: read previous_hash from striatumd.repo_event_chain_heads
	// (FOR UPDATE) instead of scanning the events table. The head row is
	// upserted after the event insert so concurrent appenders serialize on
	// the singleton-per-repository head and the chain stays contiguous.
	previousHash, err := previousChainHead(ctx, runner, repositoryID)
	if err != nil {
		return 0, err
	}
	eventID, err := nextEventID(ctx, runner)
	if err != nil {
		return 0, err
	}
	createdAt := nowString()
	rowMaterial := map[string]any{
		"repository_id":    repositoryID,
		"event_id":         eventID,
		"run_id":           nullable(runID),
		"event_type":       eventType,
		"actor_session_id": nullable(actorSessionID),
		"job_id":           nullable(jobID),
		"message_id":       nullable(messageID),
		"artifact_id":      nullable(artifactID),
		"lease_id":         nullable(leaseID),
		"payload_json":     payload,
		"created_at":       createdAt,
	}
	rowHash, err := canonicalEventHash(rowMaterial, previousHash)
	if err != nil {
		return 0, err
	}
	payloadArg, err := eventPayloadInsertArg(runner, payload)
	if err != nil {
		return 0, err
	}
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return 0, fmt.Errorf("runner does not support exec")
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
		nullable(runID),
		eventType,
		nullable(actorSessionID),
		nullable(jobID),
		nullable(messageID),
		nullable(artifactID),
		nullable(leaseID),
		payloadArg,
		createdAt,
		nullable(previousHash),
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

func eventPayloadInsertArg(runner any, payload map[string]any) (any, error) {
	return db.JSONBArg(runner, payload)
}

func previousChainHead(ctx context.Context, runner any, repositoryID string) (any, error) {
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

func nextEventID(ctx context.Context, runner any) (int64, error) {
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

func canonicalEventHash(row map[string]any, previousHash any) (string, error) {
	payload := map[string]any{
		"previous_hash":    nullable(previousHash),
		"repository_id":    row["repository_id"],
		"event_id":         row["event_id"],
		"run_id":           nullable(row["run_id"]),
		"event_type":       row["event_type"],
		"actor_session_id": nullable(row["actor_session_id"]),
		"job_id":           nullable(row["job_id"]),
		"message_id":       nullable(row["message_id"]),
		"artifact_id":      nullable(row["artifact_id"]),
		"lease_id":         nullable(row["lease_id"]),
		"payload_json":     eventPayload(row["payload_json"]),
		"created_at":       timestampString(row["created_at"]),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func eventPayload(value any) map[string]any {
	payload := asMap(value)
	result := map[string]any{}
	for key, item := range payload {
		if key == "_event_chain" {
			continue
		}
		result[key] = item
	}
	return result
}

func asMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case []byte:
		var result map[string]any
		_ = json.Unmarshal(typed, &result)
		if result != nil {
			return result
		}
	case string:
		var result map[string]any
		_ = json.Unmarshal([]byte(typed), &result)
		if result != nil {
			return result
		}
	}
	return map[string]any{}
}

func nullable(value any) any {
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

func asTime(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case string:
		parsed, err := time.Parse(time.RFC3339, typed)
		return parsed, err == nil
	case []byte:
		parsed, err := time.Parse(time.RFC3339, string(typed))
		return parsed, err == nil
	}
	return time.Time{}, false
}

func timestampString(value any) string {
	if ts, ok := asTime(value); ok {
		return ts.UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	return fmt.Sprint(value)
}

var operatorLabelPattern = regexp.MustCompile(`^[a-z0-9._-]{1,64}$`)

func validateOperatorLabel(label string, workflow map[string]any) (string, error) {
	cleaned := strings.ToLower(strings.TrimSpace(label))
	if !operatorLabelPattern.MatchString(cleaned) {
		return "", rpc.NewError("invalid_transition", "operator label must match ^[a-z0-9._-]{1,64}$", nil)
	}
	reserved := map[string]struct{}{
		"operator": {}, "attested": {}, "unattested": {}, "unknown": {},
	}
	if lanes := asMap(workflow["lanes"]); lanes != nil {
		for lane := range lanes {
			reserved[strings.ToLower(lane)] = struct{}{}
		}
	}
	if _, found := reserved[cleaned]; found {
		return "", rpc.NewError("invalid_transition", fmt.Sprintf("operator label %q is reserved", cleaned), nil)
	}
	return cleaned, nil
}

func workflowDeclaresFreshReviewer(workflow map[string]any) bool {
	jobs := asList(workflow["jobs"])
	if len(jobs) == 0 {
		return false
	}
	for _, item := range jobs {
		job := asMap(item)
		if job["type"] != "review" {
			continue
		}
		if job["reviewer_context_policy"] == "fresh" || job["fresh_session_required"] == true {
			return true
		}
	}
	return false
}
