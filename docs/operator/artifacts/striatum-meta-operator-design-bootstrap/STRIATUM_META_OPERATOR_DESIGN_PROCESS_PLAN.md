---
type: record
status: working
feature_slug: STRIATUM_META_OPERATOR
source: feature-design-bootstrap
author: operator-codex-gpt-5-001
created_at: 2026-06-28
---

# STRIATUM_META_OPERATOR Design Process Plan

## Purpose

This plan hands the meta-operator concept to the next design phase without
selecting architecture. It defines the evidence, workflow, reviewer lenses,
artifacts, and stop conditions needed before any implementation begins.

## Inputs

- `STRIATUM_META_OPERATOR_DESIGN_SCOPE_PRD.md`
- `STRIATUM_META_OPERATOR_REPOSITORY_RECON.md`
- `STRIATUM_META_OPERATOR_WORKFLOW_SELECTION.md`
- `STRIATUM_META_OPERATOR_IDEATION_BRIEF.md`
- Current `./go/bin/striatum operator bootstrap --markdown`
- `docs/reference/spec.md`
- `docs/reference/ubiquitous-language.md`
- `docs/reference/workflow-types.md`
- `docs/reference/workflow-catalog.md`
- `docs/how-to/how-to-agent.md`
- RFCs 0099, 0102, 0116, 0122, 0124, 0128, and 0167

## Phase 0: Refresh State

Goal: ensure the design operator is working from current repo and daemon state.

Required first action:

```bash
./go/bin/striatum operator bootstrap --markdown
```

Stop if bootstrap or doctor reports integrity problems that affect design
authority, live state, or the target repo. A red doctor is a stop-and-fix
condition under `AGENTS.md:149-165`.

## Phase 1: Level-1 Divergent Ideation

Goal: produce materially different candidate designs before narrowing.

Recommended workflow: Striatum-native `divergent_ideation`.

Minimum branch count: 5.

Seed frames:

- Read-only attention view.
- Negative-authority supervisor.
- Daemon scheduler extension.
- Workflow-shape campaign coordinator.
- Proof courier / contradiction hunter.
- Campaign logistics / airlock.
- Dashboard/control surface.

Each branch must answer:

- definition of campaign,
- live-state surfaces read,
- authority granted,
- authority explicitly refused,
- stale-evidence handling,
- checkout/integration handling,
- recovery behavior,
- completion proof,
- human checkpoint model,
- required RFC or decision-log changes.

## Phase 2: Candidate Synthesis

Goal: combine overlapping ideas into three to five candidate families and score
them without declaring a winner too early.

Required synthesis artifacts:

- candidate summary,
- authority matrix,
- proof matrix,
- failure-mode table,
- stale-evidence policy,
- checkout and integration coordination policy,
- docs/RFC impact table,
- no-build risks.

The synthesis must explicitly name any design that creates a new durable object,
new daemon authority, new workflow shape, new CLI command, or new dashboard
surface.

## Phase 3: Falsification And Review

Goal: try to break the leading design before accepting it.

Recommended workflow: `falsification_gate`, with `multi_review_synthesis` if
review lanes produce separate ledgers.

Required reviewer lenses:

- provenance integrity,
- daemon authority and capability tokens,
- scheduler/recovery correctness,
- stale success claim detection,
- checkout and Git hygiene,
- operator attention economy,
- product boundary and local-first constraints,
- documentation and decision-record obligations.

Minimum falsification questions:

- Can the meta-operator mark a campaign complete when daemon state, Git ancestry,
  issue state, and docs disagree?
- Can it act on stale evidence?
- Can it silently create a second state machine?
- Can it bypass daemon recovery after a lane failure?
- Can it create checkout contention between campaigns?
- Can it overload the human with low-value anomalies?
- Can it acquire write authority without an explicit product decision?

## Phase 4: Product Decision Gate

Goal: decide whether the design is ready for an RFC or should stop.

Possible outcomes:

- no-build decision: existing run drive, dashboard, and operator workflows are
  enough after smaller improvements;
- read-only design accepted: proceed with a bounded attention/proof surface;
- negative-authority design accepted: proceed only with explicit pause,
  quarantine, refuse, and resume semantics;
- daemon authority expansion accepted: draft RFC and update command authority
  matrix/test plan;
- cross-repo or multi-daemon coordination requested: draft a fresh product
  decision before implementation because current product surface is
  single-repository per run.

## Phase 5: Design-To-Build Readiness

Only after a design is accepted, prepare implementation issues or a build
workflow.

Build readiness requires:

- accepted design or RFC,
- authority matrix,
- command-authority matrix update plan if new methods exist,
- migration/storage plan if new durable state exists,
- test plan,
- docs update plan,
- recovery and rollback plan,
- operator UX acceptance criteria,
- explicit non-goals and forbidden shortcuts.

## Artifact Index For Next Phase

Expected Level-1 artifacts:

- `STRIATUM_META_OPERATOR_PROBLEM_BRIEF.md`
- `STRIATUM_META_OPERATOR_DIVERGENCE_LEDGER.md`
- `STRIATUM_META_OPERATOR_CANDIDATE_SYNTHESIS.md`
- `STRIATUM_META_OPERATOR_AUTHORITY_MATRIX.md`
- `STRIATUM_META_OPERATOR_PROOF_MATRIX.md`
- `STRIATUM_META_OPERATOR_FALSIFICATION_LEDGER.md`
- `STRIATUM_META_OPERATOR_DESIGN_RECOMMENDATION.md`
- optional RFC draft under `docs/rfcs/` only if the accepted design changes
  product behavior.

## Stop Conditions

- Bootstrap or doctor is red.
- The design requires hosted services, telemetry, external persistence, durable
  transcript capture, or provider SDKs.
- The design treats repo files, issue labels, terminal output, or PTY logs as
  live control-plane state.
- The design requires cross-repo atomic writes without first reopening the
  product boundary.
- The design needs new daemon authority but has no command-authority update and
  guardrail-test plan.
- The design can hand-finish failed runner work outside daemon recovery.

## Handoff

Start the next invocation with Phase 0. Do not begin by editing source or
drafting an implementation issue. The next meaningful work is Level-1 design
ideation over the candidate space seeded here.
