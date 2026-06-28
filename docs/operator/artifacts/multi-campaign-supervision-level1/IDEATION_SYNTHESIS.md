---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
author: final-synthesizer-reviewer-2-001
title: "MULTI_CAMPAIGN_SUPERVISION Ideation Synthesis"
run_id: "run_7899e132bf7996d49c9b81d0df905962"
inputs:
  - "docs/operator/artifacts/multi-campaign-supervision-level1/CONVERGENCE.md"
  - "docs/operator/artifacts/multi-campaign-supervision-level1/deepened/deepen_1/DEEPENED.md"
  - "docs/operator/artifacts/multi-campaign-supervision-level1/deepened/deepen_2/DEEPENED.md"
  - "docs/operator/artifacts/multi-campaign-supervision-level1/deepened/deepen_3/DEEPENED.md"
---

# MULTI_CAMPAIGN_SUPERVISION Ideation Synthesis

author: final-synthesizer-reviewer-2-001

## Basis

This narrows the Level-1 ideation result for operator review. It does not accept
an architecture, ticketing backend, daemon schema, UI shape, or implementation
plan. The shortlist is assembled from the convergence ledger and the three
deepened picks.

## Shortlist

### ★ Authority Receipt Expiration

Why it is on the shortlist: it is the strongest converged pick and the
non-obvious-but-viable lead. It attacks the hardest failure mode: a
meta-agent's authority silently expanding as a long arc crosses design, build,
verify, recovery, and discovered-slice boundaries.

Shape: the human-accepted arc emits a stage-scoped authority receipt naming
scope, expiry, allowed actions, forbidden actions, required evidence,
deferrals, discovered slices, renewal criteria, and stop triggers. Continuation
is not inherited from chat or from a dashboard row; it is renewed only when the
receipt's durable handles remain admissible against daemon state, artifacts,
Git/docs evidence, ticket or issue mirrors, and verifier receipts.

### Fresh-Context Replay Test

Why it is on the shortlist: it turns the human's fresh-window requirement into a
testable gate instead of an aspiration. A blank lane must be able to reconstruct
the accepted arc, current authority, stop conditions, deferrals, evidence, and
next admissible action from durable inputs alone.

Shape: before a campaign advances between major stages, the daemon assembles a
bounded replay packet from durable handles. The fresh lane passes only if it can
restate the admissible next state and identify missing or contradictory
evidence without inherited conversation. Failure becomes a visible repair item,
not a silent handoff loss.

### Deferral Quarantine And Scope-Drift Refusal

Why it is on the shortlist: it directly answers the named pain that arbitrary
deferrals become accepted by neglect. It also gives discovered slices a custody
model before they can grow the arc.

Shape: every out-of-scope discovery is either refused or quarantined with a
code, reason, owner, evidence handle, wake-up condition, and re-entry gate.
Quarantine is visible but not permission: promotion into work requires bounded
authority, a fresh-context payload, and proof that the item belongs in the
accepted arc.

### Cross-Surface Contradiction Gate

Why it is on the shortlist: it is the proof layer the other three picks need.
The convergence ledger's cross-surface proof, contradiction-first status, and
catastrophe-exclusion ideas should be carried forward as a gate, even though
they are not a standalone product shape.

Shape: before a stage, slice, or campaign is called done, a contradiction check
must reconcile daemon run/job/verdict state, artifact publication, Git/docs
state, ticket or issue state, verifier receipts, and known deferrals. Missing or
conflicting handles are stop pressure, not advisory warnings.

## Trap List

- Generic ticket board as control plane: familiar task labels can launder
  authority and duplicate daemon live state.
- Dashboard as decision engine: a read/status projection can become a shadow
  workflow state machine if agents treat rows as permission.
- Scheduling-first slice pickup: batching by time can hide urgent proof
  conflicts, stale context, and scope drift.
- Experience-rated autonomy: sparse success history can become an opaque policy
  engine that loosens authority after lucky runs.
- Quarantine ledger as permission: deferral rows must not become accepted work
  merely because they are visible.
- Negative proof checklist alone: useful hygiene, but weak without actual diff,
  artifact-scope, daemon-state, and publication checks.
- Over-curated replay packets: a polished summary can hide contradictions or
  exceed the fresh lane's context budget.

## Wildcard Provocation

What if v1 is a shadow supervisor with no workflow-launch authority at all? It
would follow one accepted RFC arc and emit only authority receipts, replay-test
failures, quarantine rows, and contradiction reports while humans still launch
ordinary Striatum workflows. If this proof-only supervisor cannot keep a dozen
arcs legible, adding launch authority is premature; if it can, its outputs
become the artifact contract for a later product decision.
