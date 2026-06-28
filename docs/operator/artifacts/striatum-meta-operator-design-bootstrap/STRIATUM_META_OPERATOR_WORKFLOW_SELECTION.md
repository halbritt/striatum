---
type: record
status: working
feature_slug: STRIATUM_META_OPERATOR
source: feature-design-bootstrap
author: operator-codex-gpt-5-001
created_at: 2026-06-28
---

# STRIATUM_META_OPERATOR Workflow Selection

## Decision Boundary

This artifact selects a design process, not a product architecture. The next
phase should widen and test candidate designs before choosing daemon, CLI,
dashboard, workflow-shape, or hybrid implementation.

## Workflow Inventory

| Workflow or method | Status | Use for this effort | Rationale |
| --- | --- | --- | --- |
| feature-design-bootstrap | Used now | Level-0 only | Produces scope, recon, workflow selection, ideation brief, and process plan without selecting architecture. |
| ADHD skill | Used now | Seed material only | The user explicitly requested divergent ideation. Its output should seed repo-native design work, not replace it. |
| `divergent_ideation` | Recommended next | Level-1 candidate generation | Supported workflow for high-stakes open-ended design; widens before narrowing and deepens selected options. See `docs/reference/workflow-types.md:489-541`. |
| `falsification_gate` | Recommended after synthesis | Challenge leading design | Supported collaboration gate for proposal falsification without raw PTY/provider output. See `docs/reference/workflow-types.md:450-481`. |
| `multi_review_synthesis` | Conditional | Consolidate reviewer findings | Supported synthesis if multiple review lanes produce separate ledgers. See `docs/reference/workflow-catalog.md:315-333`. |
| `implementation_panel` | Later only | Build after accepted design | Supported, but premature before authority and state-model decisions. See `docs/reference/workflow-catalog.md:207-235`. |
| `iterated_interrogating_panel` | Not default | Optional product grilling | Experimental/example-only, so it should not be the primary path. See `docs/reference/workflow-catalog.md:237-246`. |
| Direct implementation | Rejected | None in this phase | The capability touches daemon authority, workflow state, git hygiene, and operator trust. |
| New cross-repo scheduler | Rejected for Level 1 default | Candidate only by explicit decision | Current product boundary keeps workflows single-repository per run. See `docs/rfcs/0128-cross-repo-run-boundary.md:20-35`. |

## Divergent Ideation Required

Yes.

The feature is vague, high-impact, and cross-cutting. It could plausibly be a
read-only attention view, a negative-authority supervisor, a daemon scheduler
extension, a workflow-level coordinator, a campaign ledger, a dashboard/control
mode, or a set of proof gates. Selecting one too early would hide the most
important design risks.

## Committee Review Required

Yes.

Any design that coordinates several campaigns/operators must be reviewed against:

- provenance integrity,
- daemon authority and capability tokens,
- stale evidence,
- checkout and integration races,
- operator attention economics,
- recovery correctness,
- product-boundary drift,
- state-doc and issue-state consistency.

## Recommended Sequence

1. Start the next design run with the current Striatum bootstrap:
   `./go/bin/striatum operator bootstrap --markdown`.
2. Read these five Level-0 artifacts and the cited source docs.
3. Generate a Striatum-native `divergent_ideation` workflow for Level-1 design.
4. Seed that workflow with the ADHD clusters in
   `STRIATUM_META_OPERATOR_IDEATION_BRIEF.md`.
5. Require at least five candidate branches and at least three deepened
   candidate families.
6. Synthesize candidates into an authority/proof matrix before choosing a lead.
7. Run a `falsification_gate` or equivalent multi-review step against the
   leading design.
8. If the design changes product behavior, draft an RFC and update the decision
   log only after the design is accepted.
9. Stop at a design-to-build readiness gate. Do not create implementation
   issues or touch source until the accepted design has authority, proof,
   recovery, and docs-update obligations defined.

## Candidate Level-1 Lanes

- Authority and security lane: defines what the meta-operator may read, refuse,
  pause, quarantine, resume, spawn, integrate, or delegate.
- Provenance lane: defines proof surfaces, artifact anchors, state-doc
  freshness, issue-state evidence, and completion contradiction detection.
- Operator UX lane: defines the attention surface, next-action cards,
  suppression rules, and human checkpoint model.
- Scheduler and recovery lane: evaluates whether existing driver/scheduler
  concepts are enough or whether a new daemon surface is required.
- Git and checkout lane: evaluates integration serialization, branch freshness,
  worktree ownership, and clean-tree preconditions.
- Product-boundary lane: rejects hidden hosted dependencies, transcript stores,
  direct DB reads, or cross-repo atomicity.

## First Command Shape For The Next Designer

The next designer should not execute this automatically from this bootstrap
artifact, but this is the intended starting pattern:

```bash
./go/bin/striatum operator bootstrap --markdown
```

After that, create or claim a design workflow using the current supported
Striatum workflow generator and the local operator guidance active at that time.
The first action is bootstrap, not artifact writing or implementation.

## Rejected Shortcuts

- Building a dashboard widget first: useful later, but it would prematurely
  choose UI over authority/proof design.
- Adding a daemon scheduler first: too much authority without proving whether a
  read-only or negative-authority design suffices.
- Treating GitHub issues or labels as campaign truth: issue state may be useful
  evidence, but Striatum live state remains daemon-owned PostgreSQL.
- Scraping terminals or PTY logs: explicitly outside the product boundary.
- Allowing the meta-operator to "fix up" failed runs by hand: this violates the
  recovery and provenance discipline in `AGENTS.md:149-165`.

## Output Gate For Level 1

Level 1 should produce:

- a candidate set with materially different designs,
- a scored authority/proof matrix,
- falsification findings,
- a recommended lead design or explicit no-build decision,
- an RFC-or-no-RFC recommendation,
- a build-readiness checklist only after design acceptance.
