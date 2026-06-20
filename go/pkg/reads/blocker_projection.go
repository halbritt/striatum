package reads

import (
	"context"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/db"
)

// #477 — needs_operator recovery UX. When the autonomous recovery loop gives up
// on a job it opens a blocker (e.g. blocker_kind='recovery_exhausted') and flips
// the run to needs_operator. The blocker's id (`blk_…`) IS the escalation_id the
// operator must hand to the right resolve verb — but the standard inspection
// surfaces (dashboard, run summary, why) only ever reported a TYPE COUNT, never
// the id, and never the literal command. This projection turns an open blocker
// row into an actionable entry that names the id AND the exact verb that clears
// it, so dashboard/why/run-summary can stop sending the operator to grep raw
// event payloads or guess between three near-identical resolve verbs.
//
// The three resolution surfaces mirror the daemon's own routing (the error
// messages quoted in #477):
//   - human_checkpoint severity  -> `checkpoint resolve <id> continue`
//   - escalation-class kinds      -> `escalation resolve <id>`
//   - everything else (process)   -> `recovery resolve-blocker <id>`
//
// blockerEscalationKinds is kept in lockstep with escalationPredicate() (the
// single source of truth HandleEscalationResolve uses to find a blocker), so the
// surface this projection advertises is exactly the verb that will accept the id.

// openRunBlockerActions returns the actionable open blockers for one run, used by
// the dashboard projection so `dashboard --once` can show the id + resolve
// command instead of only a {kind: count} tally.
func openRunBlockerActions(ctx context.Context, runner db.Runner, repositoryID, runID string) ([]map[string]any, error) {
	if repositoryID == "" || runID == "" {
		return []map[string]any{}, nil
	}
	rows, err := collectRows(ctx, runner, `
		SELECT b.blocker_id, b.run_id, b.job_id, j.workflow_job_id,
		       b.session_id, b.severity, b.blocker_kind, b.state, b.created_at
		  FROM striatumd.blockers b
		  LEFT JOIN striatumd.jobs j
		    ON j.repository_id = b.repository_id AND j.job_id = b.job_id
		 WHERE b.repository_id = $1
		   AND b.run_id = $2
		   AND b.state = 'open'
		 ORDER BY b.created_at, b.blocker_id`,
		repositoryID, runID)
	if err != nil {
		return nil, err
	}
	return shapeBlockerActionRows(rows), nil
}

// targetBlockerActions returns the actionable open blockers reachable from a
// single target id (run/job/session/blocker), used by `why` so a needs_operator
// run's "what do I do" surface is non-empty even when no event payload happens to
// ILIKE-match the run id.
func targetBlockerActions(ctx context.Context, runner db.Runner, repositoryID, targetID string) ([]map[string]any, error) {
	if repositoryID == "" || targetID == "" {
		return []map[string]any{}, nil
	}
	rows, err := collectRows(ctx, runner, `
		SELECT b.blocker_id, b.run_id, b.job_id, j.workflow_job_id,
		       b.session_id, b.severity, b.blocker_kind, b.state, b.created_at
		  FROM striatumd.blockers b
		  LEFT JOIN striatumd.jobs j
		    ON j.repository_id = b.repository_id AND j.job_id = b.job_id
		 WHERE b.repository_id = $1
		   AND b.state = 'open'
		   AND (
		        b.blocker_id = $2
		     OR b.run_id = $2
		     OR b.job_id = $2
		     OR b.session_id = $2
		   )
		 ORDER BY b.created_at, b.blocker_id`,
		repositoryID, targetID)
	if err != nil {
		return nil, err
	}
	return shapeBlockerActionRows(rows), nil
}

func shapeBlockerActionRows(rows []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		blockerID := stringFrom(row, "blocker_id")
		entry := map[string]any{
			"blocker_id":         blockerID,
			"run_id":             row["run_id"],
			"job_id":             row["job_id"],
			"workflow_job_id":    row["workflow_job_id"],
			"session_id":         row["session_id"],
			"severity":           row["severity"],
			"blocker_kind":       row["blocker_kind"],
			"state":              row["state"],
			"created_at":         row["created_at"],
			"resolve_surface":    blockerResolveSurface(row),
			"resolution_command": blockerResolutionCommand(row),
			"resolve_command":    blockerResolveCommand(row),
			"inspection_hint":    fmt.Sprintf("striatum why %s --json", blockerID),
			"escalation_id":      escalationIDForBlocker(row),
			"checkpoint_id":      checkpointIDForBlocker(row),
		}
		out = append(out, entry)
	}
	return out
}

// blockerResolveSurface names which resolve verb family accepts this blocker,
// mirroring the daemon's own routing so the advertised command is the one that
// will succeed (not one of the two dead ends #477 hit).
func blockerResolveSurface(row map[string]any) string {
	switch {
	case stringFrom(row, "severity") == "human_checkpoint":
		return "checkpoint.resolve"
	case blockerKindUsesEscalationResolve(stringFrom(row, "blocker_kind")):
		return "escalation.resolve"
	default:
		return "recovery.resolve_blocker"
	}
}

// blockerResolutionCommand is the literal CLI an operator runs to clear the
// blocker. It is the load-bearing output of #477: the id was findable, but the
// VERB was a maze.
func blockerResolutionCommand(row map[string]any) string {
	blockerID := stringFrom(row, "blocker_id")
	switch blockerResolveSurface(row) {
	case "checkpoint.resolve":
		return "striatum checkpoint resolve " + blockerID + " continue"
	case "escalation.resolve":
		return "striatum escalation resolve " + blockerID
	default:
		return "striatum recovery resolve-blocker " + blockerID
	}
}

// blockerResolveCommand is the JSON-emitting form for scripted operators.
func blockerResolveCommand(row map[string]any) string {
	return blockerResolutionCommand(row) + " --json"
}

func escalationIDForBlocker(row map[string]any) any {
	if blockerResolveSurface(row) == "escalation.resolve" {
		return row["blocker_id"]
	}
	return nil
}

func checkpointIDForBlocker(row map[string]any) any {
	if blockerResolveSurface(row) == "checkpoint.resolve" {
		return row["blocker_id"]
	}
	return nil
}

// blockerKindUsesEscalationResolve mirrors the blocker_kind branch of
// escalationPredicate() (detail.go) — the single source of truth for which
// blockers `escalation resolve` accepts. Keep these two in sync: the surface this
// projection advertises must be the verb that will actually take the id.
func blockerKindUsesEscalationResolve(kind string) bool {
	switch kind {
	case "ai_self_declared",
		"ambiguous_goal",
		"committee_stalemate",
		"contradicting_decisions",
		"missing_authority",
		"no_available_reviewer_lane",
		"override_required",
		"recovery_exhausted",
		"provenance_gate_failed":
		return true
	default:
		return false
	}
}
