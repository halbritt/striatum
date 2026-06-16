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
	RecallDigest     RecallDigestOptions
}

// packageBlobClient is the daemon's blob client, set by Register and
// read by publishArtifact. Package-level so that publishArtifact's
// transitive callers (review.submit, recovery.auto_finalize) do not
// need their own threading. nil = blob storage disabled / not
// configured; publishArtifact then skips the S3 upload step and the
// artifact body stays in the working tree.
var packageBlobClient *blob.Client
var packageDaemonSocketPath string
var packageRecallDigestOptions RecallDigestOptions

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
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	packageBlobClient = o.BlobClient
	packageDaemonSocketPath = strings.TrimSpace(o.DaemonSocketPath)
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
	rpc.MethodRegistry["supervise.rebridge"] = rpc.NewMethod("supervise.rebridge", rpc.CapPtr(rpc.CapabilityClaim), true, rpc.ScopeSingleRepo)
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
	server.Register("recovery.resolve_blocker", makeHandler(runner, HandleRecoveryResolveBlocker))
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
func isTransientDaemonLoadError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case statementTimeoutSQLState, "57P01", "57P02", "57P03":
		return true
	default:
		return false
	}
}

// withTxRetryOnDeadlock runs fn inside a transaction, retrying up to a small
// bounded number of times when the transaction aborts with a Postgres deadlock
// (SQLSTATE 40P01). The retry is intentionally tight — only the deadlock
// SQLSTATE is retried, with a short backoff — so a genuine livelock surfaces a
// clear error recommending a serial retry rather than spinning. Any non-
// deadlock error (validation, transition refusal, etc.) is returned
// immediately.
func withTxRetryOnDeadlock(ctx context.Context, runner db.Runner, fn func(db.TxRunner) (map[string]any, error)) (map[string]any, error) {
	const maxAttempts = 3
	const baseBackoff = 5 * time.Millisecond
	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err := withTx(ctx, runner, fn)
		if err == nil {
			return result, nil
		}
		if !isDeadlockError(err) {
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
		map[string]any{"sqlstate": deadlockSQLState, "attempts": maxAttempts},
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
		latest, err := latestVerdict(ctx, runner, repositoryID, fmt.Sprint(upstream["job_id"]))
		if err != nil {
			return false, err
		}
		if !required[latest] {
			return false, nil
		}
	}
	return true, nil
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
	remaining, err := existsRow(ctx, runner, `
		SELECT 1 FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2
		   AND state NOT IN ('completed','skipped','canceled')
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
	state := "canceled"
	eventType := "run.canceled"
	source := "run_canceled"
	reason := "run_canceled"
	payload := map[string]any{"reason": "all_jobs_canceled"}
	if hasCompleted {
		state = "completed"
		eventType = "run.completed"
		source = "run_completed"
		reason = "run_completed"
		payload = nil
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
		payload = map[string]any{
			"completion_mode": completionMode,
			"provenance_gate": ledger,
		}
		payload, err = freezeRunCompletionRecord(ctx, runner, repositoryID, runID, "completed", "run_completed",
			map[string]any{"completion_mode": completionMode, "provenance_gate": ledger}, payload)
		if err != nil {
			return err
		}
		// stop_reason may carry 'provenance_gate_failed' from an earlier
		// escalation of this run; a clean (re-driven) completion clears it.
		if err := exec.Exec(ctx, `
			UPDATE striatumd.runs
			   SET state = $1, completed_at = $2, stop_reason = NULL,
			       completion_mode = $3
			 WHERE repository_id = $4 AND run_id = $5`, state, now, completionMode, repositoryID, runID); err != nil {
			return err
		}
	} else {
		payload, err = freezeRunCompletionRecord(ctx, runner, repositoryID, runID, state, reason,
			map[string]any{"stop_reason": "all_jobs_canceled"}, payload)
		if err != nil {
			return err
		}
		if err := exec.Exec(ctx, `
			UPDATE striatumd.runs
			   SET state = $1, completed_at = $2, stop_reason = 'all_jobs_canceled'
			 WHERE repository_id = $3 AND run_id = $4`, state, now, repositoryID, runID); err != nil {
			return err
		}
	}
	if _, err := appendEvent(ctx, runner, repositoryID, runID, eventType, nil, nil, nil, nil, nil, payload); err != nil {
		return err
	}
	return closeRemainingSessions(ctx, runner, repositoryID, runID, source, reason)
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
