package reads

import (
	"strings"
	"testing"
)

// #477: an escalation-class blocker (e.g. recovery_exhausted, the kind the
// recovery sweep opens before flipping a run to needs_operator) must project the
// blocker_id as escalation_id AND the literal `escalation resolve <id>` command —
// the verb that actually clears it (not the two dead ends the issue hit).
func TestShapeBlockerActionRowsEscalationClassNamesEscalationResolve(t *testing.T) {
	rows := []map[string]any{{
		"blocker_id":      "blk_recov",
		"run_id":          "run_x",
		"job_id":          "job_x",
		"workflow_job_id": "review_final",
		"session_id":      nil,
		"severity":        "blocked",
		"blocker_kind":    "recovery_exhausted",
		"state":           "open",
		"created_at":      "2026-06-20T00:00:00Z",
	}}
	out := shapeBlockerActionRows(rows)
	if len(out) != 1 {
		t.Fatalf("want one shaped blocker, got %d", len(out))
	}
	entry := out[0]
	if entry["blocker_id"] != "blk_recov" {
		t.Fatalf("blocker_id = %#v", entry["blocker_id"])
	}
	if entry["escalation_id"] != "blk_recov" {
		t.Fatalf("escalation_id = %#v, want blk_recov (escalation-class blocker)", entry["escalation_id"])
	}
	if entry["checkpoint_id"] != nil {
		t.Fatalf("checkpoint_id = %#v, want nil for an escalation-class blocker", entry["checkpoint_id"])
	}
	if entry["resolve_surface"] != "escalation.resolve" {
		t.Fatalf("resolve_surface = %#v", entry["resolve_surface"])
	}
	if entry["resolution_command"] != "striatum escalation resolve blk_recov" {
		t.Fatalf("resolution_command = %#v", entry["resolution_command"])
	}
	if entry["resolve_command"] != "striatum escalation resolve blk_recov --json" {
		t.Fatalf("resolve_command = %#v", entry["resolve_command"])
	}
	if entry["inspection_hint"] != "striatum why blk_recov --json" {
		t.Fatalf("inspection_hint = %#v", entry["inspection_hint"])
	}
}

// A human_checkpoint blocker routes to `checkpoint resolve`, NOT escalation —
// proving the projection mirrors the daemon's own resolve routing.
func TestShapeBlockerActionRowsHumanCheckpointNamesCheckpointResolve(t *testing.T) {
	rows := []map[string]any{{
		"blocker_id":   "blk_chk",
		"severity":     "human_checkpoint",
		"blocker_kind": "operator_review",
		"state":        "open",
	}}
	entry := shapeBlockerActionRows(rows)[0]
	if entry["resolve_surface"] != "checkpoint.resolve" {
		t.Fatalf("resolve_surface = %#v", entry["resolve_surface"])
	}
	if entry["checkpoint_id"] != "blk_chk" {
		t.Fatalf("checkpoint_id = %#v, want blk_chk", entry["checkpoint_id"])
	}
	if entry["escalation_id"] != nil {
		t.Fatalf("escalation_id = %#v, want nil for a human_checkpoint", entry["escalation_id"])
	}
	if entry["resolution_command"] != "striatum checkpoint resolve blk_chk continue" {
		t.Fatalf("resolution_command = %#v", entry["resolution_command"])
	}
}

// A non-escalation, non-checkpoint blocker (a process-adapter blocker) routes to
// `recovery resolve-blocker` — the default surface.
func TestShapeBlockerActionRowsProcessBlockerNamesRecoveryResolve(t *testing.T) {
	rows := []map[string]any{{
		"blocker_id":   "blk_proc",
		"severity":     "blocked",
		"blocker_kind": "retry_budget_pending",
		"state":        "open",
	}}
	entry := shapeBlockerActionRows(rows)[0]
	if entry["resolve_surface"] != "recovery.resolve_blocker" {
		t.Fatalf("resolve_surface = %#v", entry["resolve_surface"])
	}
	if entry["escalation_id"] != nil || entry["checkpoint_id"] != nil {
		t.Fatalf("escalation_id/checkpoint_id = %#v/%#v, want both nil", entry["escalation_id"], entry["checkpoint_id"])
	}
	if entry["resolution_command"] != "striatum recovery resolve-blocker blk_proc" {
		t.Fatalf("resolution_command = %#v", entry["resolution_command"])
	}
}

// The escalation-class enumeration MUST stay in lockstep with the kinds in
// escalationPredicate() (detail.go) — the single source of truth the resolve
// handler uses to find a blocker. If they drift, the projection would advertise a
// verb that rejects the id (exactly the #477 maze). This pins the agreement.
func TestBlockerEscalationKindsMatchEscalationPredicate(t *testing.T) {
	predicate := escalationPredicate()
	for _, kind := range []string{
		"ai_self_declared",
		"ambiguous_goal",
		"committee_stalemate",
		"contradicting_decisions",
		"missing_authority",
		"no_available_reviewer_lane",
		"override_required",
		"recovery_exhausted",
		"provenance_gate_failed",
	} {
		if !blockerKindUsesEscalationResolve(kind) {
			t.Fatalf("blockerKindUsesEscalationResolve(%q) = false, want true", kind)
		}
		if !strings.Contains(predicate, "'"+kind+"'") {
			t.Fatalf("escalationPredicate() is missing %q — projection and resolve handler drifted", kind)
		}
	}
}
