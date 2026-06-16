package mutations

import (
	"context"
	"errors"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

// HandleRecoveryResolveBlocker is GH #304: the operator verb that closes a
// dangling non-escalation, non-checkpoint blocker by id.
//
// A severity='blocked' (is_escalation=false) work.block blocker raised on an
// earlier job attempt could be left open forever once the job later completed
// via the recovery/retry completion paths (the #304 root cause, now fixed at
// every job.completed site). Until those re-completed runs are swept, and for
// any blocked-severity blocker that legitimately outlives its gate, no operator
// verb could close it: checkpoint.resolve only acts on waiting_human, and
// escalation.resolve only acts on escalation-class blockers. This verb fills
// that gap.
//
// It deliberately REFUSES the blockers that already have a dedicated verb:
//   - waiting_human / human_checkpoint blockers -> checkpoint.resolve.
//   - escalation-class blockers (ambiguous_goal, recovery_exhausted, etc.)
//     -> escalation.resolve, which records the adjudicating decision.
//
// Those refusals keep this from becoming a back door that silently discharges a
// human-adjudication gate. It resolves the blocker, resolves any matching
// escalation_inbox row defensively, and emits a blocker.resolved event so the
// resolution is legible in the durable chain. It does NOT touch run/job state:
// the dangling blocker is by definition no longer gating a live job, so there is
// nothing to unblock — it only reconciles the open_blockers frontier.
func HandleRecoveryResolveBlocker(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	blockerID := stringParam(envelope, "blocker_id")
	if blockerID == "" {
		return nil, rpc.NewError("schema_invalid", "recovery.resolve_blocker requires blocker_id", nil)
	}
	note := stringParam(envelope, "note")

	result, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		blocker, err := rowByID(ctx, tx, repositoryID, "blockers", "blocker_id", blockerID, true)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rpc.NewError("not_found", fmt.Sprintf("could not find blockers row for %q", blockerID), nil)
		}
		if err != nil {
			return nil, err
		}
		runID := fmt.Sprint(blocker["run_id"])
		// RFC 0104: per-run advisory lock, scoped via the blocker's run, so a
		// concurrent claim/complete/recovery sweep on the same run serializes
		// against this resolve.
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}

		state := fmt.Sprint(blocker["state"])
		if state != "open" {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
				"blocker is %s; resolve-blocker only closes an open blocker", state), nil)
		}
		// A human_checkpoint blocker is a human-adjudication gate (its job sits in
		// waiting_human); it is resolved through checkpoint.resolve, not here.
		if fmt.Sprint(blocker["severity"]) == "human_checkpoint" {
			return nil, rpc.NewError("invalid_transition",
				"blocker is a human checkpoint; resolve it with `checkpoint resolve` instead", nil)
		}
		// An escalation-class blocker carries a decision obligation; it is resolved
		// through escalation.resolve, which records the adjudicating decision.
		if isEscalationClassBlocker(blocker) {
			return nil, rpc.NewError("invalid_transition",
				"blocker is escalation-class; resolve it with `escalation resolve` instead", nil)
		}

		now := nowString()
		if err := tx.Exec(ctx, `
			UPDATE striatumd.blockers
			   SET state = 'resolved', resolved_at = $1
			 WHERE repository_id = $2 AND blocker_id = $3`, now, repositoryID, blockerID); err != nil {
			return nil, err
		}
		// Defensive: a plain autonomous blocker has no escalation_inbox row, but
		// resolve any matching one anyway so the two surfaces never disagree.
		if err := tx.Exec(ctx, `
			UPDATE striatumd.escalation_inbox
			   SET state = 'resolved', resolved_at = $1
			 WHERE repository_id = $2 AND escalation_id = $3 AND state <> 'resolved'`, now, repositoryID, blockerID); err != nil {
			return nil, err
		}
		eventPayload := map[string]any{
			"blocker_id":   blockerID,
			"severity":     blocker["severity"],
			"blocker_kind": blocker["blocker_kind"],
			"reason":       "operator_resolve_blocker",
		}
		if note != "" {
			eventPayload["note"] = note
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "blocker.resolved", nil, nullable(blocker["job_id"]), nil, nil, nil, eventPayload); err != nil {
			return nil, err
		}
		return map[string]any{
			"status":       "resolved",
			"blocker_id":   blockerID,
			"run_id":       runID,
			"job_id":       blocker["job_id"],
			"blocker_kind": blocker["blocker_kind"],
			"severity":     blocker["severity"],
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
