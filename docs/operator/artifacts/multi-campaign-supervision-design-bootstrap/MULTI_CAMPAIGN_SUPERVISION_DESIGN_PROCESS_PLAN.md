---
type: record
status: working
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: feature-design-bootstrap
author: operator-codex-gpt-5-002
created_at: 2026-06-28
updated_at: 2026-06-28
---

# MULTI_CAMPAIGN_SUPERVISION Design Process Plan

## Purpose

This plan hands the multi-campaign supervision concept to a fresh Level-1 design
operator. It describes the process for designing the capability, not the
capability's architecture. The load-bearing human requirement is that each major
stage should be able to start in a fresh context window, with durable artifacts
and tickets carrying context forward.

## Inputs

- `MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md`: live-human Stage 1 notes,
  problem statement, objectives, constraints, decision points, and acceptance
  criteria for the design effort.
- `MULTI_CAMPAIGN_SUPERVISION_REPOSITORY_RECON.md`: current repo facts and
  architectural constraints.
- `MULTI_CAMPAIGN_SUPERVISION_WORKFLOW_SELECTION.md`: chosen Level-1 design
  workflow, gates, context handoffs, and rejected shortcuts.
- `MULTI_CAMPAIGN_SUPERVISION_IDEATION_BRIEF.md`: problem framing, questions,
  evaluation criteria, reviewer perspectives, and non-recommendation.

## Phase 0: Refresh State

Goal: ensure the design operator is working from current repo and daemon state.

Required first action:

```bash
./go/bin/striatum operator bootstrap --markdown
```

Follow the returned `next_actions` and bounded `reading_plan`. Stop if bootstrap
or doctor reports integrity problems that affect design authority, live state,
or the target repo. A red doctor is a stop-and-fix condition under
`AGENTS.md:149`.

Inputs:

- current checkout
- current daemon state
- these Level-0 artifacts

Outputs:

- refreshed state note in the Level-1 working artifact
- decision to proceed, pause, or repair the environment first

Gate: bootstrap is current enough and `doctor` is not red for this design work.

## Phase 1: Fresh-Context Divergent Ideation

Workflow: Striatum-native `divergent_ideation`.

Goal: produce materially different candidate designs before narrowing.

Inputs:

- all four Level-0 input artifacts
- current bootstrap state
- accepted `MULTI_CAMPAIGN_SUPERVISION` slug

Activities:

- Start the ideation workflow in a fresh context window.
- Generate distinct candidate families for ticketing substrate, UI/dashboard
  surface, meta-agent authority, fresh-context handoff, deferral policy, and
  proof model.
- Keep candidates at design level; do not assign source-file edits or
  implementation tickets.

Outputs:

- `MULTI_CAMPAIGN_SUPERVISION_PROBLEM_BRIEF.md`
- `MULTI_CAMPAIGN_SUPERVISION_DIVERGENCE_LEDGER.md`

Gate: at least three materially different candidate families exist, and each
answers the questions in the ideation brief.

## Phase 2: Candidate Synthesis

Workflow: synthesis stage from `divergent_ideation`, with a fresh context window.

Goal: converge the candidate set into a small number of reviewed options without
choosing prematurely.

Inputs:

- divergence ledger
- problem brief
- repository recon
- workflow selection

Activities:

- Score candidates against fresh-context robustness, deferral accountability,
  authority fidelity, local-first ticketing fit, portfolio visibility,
  completion proof, workflow compatibility, and implementation realism.
- Produce matrices for ticketing, UI, authority, context handoff, proof, and
  stop conditions.
- Record dissent and no-build risks.

Outputs:

- `MULTI_CAMPAIGN_SUPERVISION_CANDIDATE_SYNTHESIS.md`
- `MULTI_CAMPAIGN_SUPERVISION_AUTHORITY_MATRIX.md`
- `MULTI_CAMPAIGN_SUPERVISION_PROOF_AND_CONTEXT_MATRIX.md`

Gate: synthesis names a lead candidate or explicitly says the space must widen,
and it preserves unresolved objections for review.

## Phase 3: Falsification And Review

Workflow: `falsification_gate`, optionally followed by `multi_review_synthesis`.

Goal: try to break the leading design before acceptance.

Inputs:

- candidate synthesis
- authority matrix
- proof/context matrix
- Stage 1 failure modes

Activities:

- Attack whether deferrals can silently become accepted.
- Attack whether stale daemon/Git/docs/ticket evidence can produce false done
  claims.
- Attack whether the meta-agent can hide blocked RFCs or overflow its own
  context.
- Attack whether the design bypasses daemon authority or creates a second
  workflow state machine.
- Attack whether irreversible actions can happen without human confirmation.

Outputs:

- `MULTI_CAMPAIGN_SUPERVISION_FALSIFICATION_LEDGER.md`
- optional `MULTI_CAMPAIGN_SUPERVISION_REVIEW_SYNTHESIS.md`

Gate: verdict is accept, accept-with-findings, revise, or reject, with explicit
reasons and carried constraints.

## Phase 4: Product Decision Gate

Workflow: human checkpoint plus decision/RFC drafting if needed.

Goal: decide whether the accepted design requires a new RFC, decision-log entry,
or no-build conclusion.

Inputs:

- falsification ledger
- review synthesis, if present
- candidate synthesis

Activities:

- Decide whether local-first ticketing needs daemon state, repository artifacts,
  GitHub issue integration, or a hybrid.
- Decide whether the UI/dashboard needs new read routes or only existing status
  projections.
- Decide whether the meta-agent has recommendation-only, sequencing,
  pause/quarantine/resume, ticket-update, or docs-update authority.
- Decide which actions remain human-confirmed.
- Record accepted non-goals and stop conditions.

Outputs:

- `MULTI_CAMPAIGN_SUPERVISION_DESIGN_RECOMMENDATION.md`
- optional RFC draft under `docs/rfcs/`
- optional decision-log update recommendation

Gate: a human/product authority accepts the design direction or explicitly stops
the feature.

## Phase 5: Design-To-Build Readiness

Workflow: design-readiness review, not implementation.

Goal: prepare a later build planner without starting implementation here.

Inputs:

- accepted design recommendation or RFC
- product decision gate result
- matrices and falsification findings

Activities:

- Confirm authority changes and command-authority matrix obligations.
- Confirm storage/migration implications if any durable ticket or campaign state
  is selected.
- Confirm UI/dashboard acceptance criteria.
- Confirm fresh-context packet/ticket requirements.
- Confirm deferral, stop-condition, recovery, and proof obligations.
- Only then authorize a later issue/ticket slicing process.

Outputs:

- `MULTI_CAMPAIGN_SUPERVISION_DESIGN_TO_BUILD_READINESS.md`
- later implementation tickets only after this gate, not during Level 0

Gate: the build planner can start cold from accepted design artifacts and does
not need this chat session.

## Reviewer Perspectives

- authority and security - can the meta-agent act only within accepted
  capabilities and human-confirmed boundaries?
- provenance integrity - can plans, handoffs, deferrals, launches, and done
  claims be audited without transcripts?
- fresh-context operations - can every stage start from bounded durable context?
- operator attention and UX - does the UI/dashboard show the right state without
  hiding blocked work?
- ticketing/local-first - does the ticket substrate preserve local-first
  operation and avoid making GitHub mandatory?
- recovery and stop conditions - does the design pause safely when evidence or
  runner state conflicts?
- product boundary - does the design avoid hosted coordination, cross-repo
  mutation, and an unofficial second state machine?

## Artifacts Index

| Artifact | Status | Location |
| --- | --- | --- |
| `MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md` | produced | `docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/` |
| `MULTI_CAMPAIGN_SUPERVISION_REPOSITORY_RECON.md` | produced | `docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/` |
| `MULTI_CAMPAIGN_SUPERVISION_WORKFLOW_SELECTION.md` | produced | `docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/` |
| `MULTI_CAMPAIGN_SUPERVISION_IDEATION_BRIEF.md` | produced | `docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/` |
| `MULTI_CAMPAIGN_SUPERVISION_DESIGN_PROCESS_PLAN.md` | produced | `docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/` |
| `MULTI_CAMPAIGN_SUPERVISION_PROBLEM_BRIEF.md` | pending | Level-1 design workflow artifact root |
| `MULTI_CAMPAIGN_SUPERVISION_DIVERGENCE_LEDGER.md` | pending | Level-1 design workflow artifact root |
| `MULTI_CAMPAIGN_SUPERVISION_CANDIDATE_SYNTHESIS.md` | pending | Level-1 design workflow artifact root |
| `MULTI_CAMPAIGN_SUPERVISION_AUTHORITY_MATRIX.md` | pending | Level-1 design workflow artifact root |
| `MULTI_CAMPAIGN_SUPERVISION_PROOF_AND_CONTEXT_MATRIX.md` | pending | Level-1 design workflow artifact root |
| `MULTI_CAMPAIGN_SUPERVISION_FALSIFICATION_LEDGER.md` | pending | Level-1 review workflow artifact root |
| `MULTI_CAMPAIGN_SUPERVISION_DESIGN_RECOMMENDATION.md` | pending | Level-1 design workflow artifact root |
| `MULTI_CAMPAIGN_SUPERVISION_DESIGN_TO_BUILD_READINESS.md` | pending | after accepted design |

## Handoff

To role: Level-1 design operator.

Entry point:

```bash
./go/bin/striatum operator bootstrap --markdown
```

Clean-context instructions:

1. Run the entry point first and follow `next_actions` and the bounded
   `reading_plan`.
2. Read this plan.
3. Read the four prior Level-0 artifacts in this directory.
4. Start a fresh-context Level-1 divergent ideation run for
   `MULTI_CAMPAIGN_SUPERVISION`.
5. Do not rely on chat history from the Stage 1 interview; the interview input
   is captured in `MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md`.

Provenance: produced by `operator-codex-gpt-5-002` on 2026-06-28 from live
Stage 1 human interrogation, Striatum bootstrap state, repository recon, and the
feature-design-bootstrap skill.

## Stop Boundary

This plan is Level 0: it designs the design process. It does not select the
feature's architecture, data model, API, UI implementation, daemon storage,
ticketing backend, workflow role, or build sequence. Those are Level-1 design
work and must not appear here.

## Inherited Open Questions

- What is the durable coordination unit?
- What local-first ticketing substrate should carry arc work and handoff bodies?
- Which actions may the meta-agent take after arc acceptance?
- What exact context packet crosses each fresh-window boundary?
- How are deferrals represented, challenged, and accepted or rejected?
- What UI/dashboard status is required for a human to oversee a dozen RFCs?
- What stop conditions pause the meta-agent, and who resolves each?
- Does the accepted design require an RFC or decision-log update?
