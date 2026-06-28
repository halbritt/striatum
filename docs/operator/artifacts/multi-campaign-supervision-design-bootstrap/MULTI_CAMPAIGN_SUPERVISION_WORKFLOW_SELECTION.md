---
type: record
status: working
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: feature-design-bootstrap
author: operator-codex-gpt-5-002
created_at: 2026-06-28
updated_at: 2026-06-28
---

# MULTI_CAMPAIGN_SUPERVISION Workflow Selection

## Decision Boundary

This artifact selects the design workflow for multi-campaign supervision. It
does not choose the product architecture, ticketing backend, daemon schema,
dashboard design, or automation authority.

## Workflow Inventory Considered

| workflow_or_skill | status | evidence_or_source | why_reused_or_rejected | authority_needed_to_run |
| --- | --- | --- | --- | --- |
| `feature-design-bootstrap` | available / used | Loaded local skill; five Level-0 artifacts in this directory | Correct Level-0 process after live Stage 1 interrogation | Read/write access to this artifact directory |
| `divergent_ideation` | available | `docs/reference/workflow-catalog.md:105`; `docs/reference/workflow-types.md:489` | Recommended for Level-1 candidate generation because the problem spans authority, tickets, UI, context refresh, provenance, and workflow sequencing | Striatum design workflow authority after bootstrap |
| `falsification_gate` | available | `docs/reference/workflow-catalog.md:147`; `docs/reference/workflow-types.md:456` | Recommended after candidate synthesis to attack silent deferrals, stale success, context overflow, and daemon-authority bypass | Striatum workflow authority and independent review lanes |
| `multi_review_synthesis` | available | `docs/reference/workflow-catalog.md:315` | Conditional consolidation step if several reviewer ledgers need one decision surface | Striatum workflow authority |
| `implementation_panel` | available later | `docs/reference/workflow-catalog.md:207` | Not a Level-1 default; useful after design acceptance if implementation alternatives are contested | Build/design-review authority after product decision |
| `human_checkpoint` | available | `docs/reference/workflow-catalog.md:189` | Required as a gate for arc acceptance, irreversible actions, and unresolved authority choices | Human-principal decision authority |
| `$handoff` / `$striatum-handoff` style payloads | available as local skills / user-supplied input | Stage 1 interview supplied the desired pattern | Use as a design input for ticket bodies and fresh-context continuation; do not assume storage or schema | None for design; later ticket authoring authority if selected |
| GitHub issues | available as tracker, not merge path | `AGENTS.md:131` | Candidate communication medium only; cannot be merge authority or live workflow truth | Tracker access only |
| Local-first ticketing | needs setup / design | Stage 1 human preference | Must be investigated because the human prefers local ticketing and UI introspection | Product decision if new daemon/repo state is needed |
| Direct implementation | rejected | Level-0 boundary and Stage 1 uncertainty | Too much authority and blast radius before ticketing, provenance, UI, and automation boundaries are designed | Not applicable |
| Cross-repo orchestration | rejected for default | `docs/reference/spec.md:21` | Current product surface retired cross-repo workflow mutation; needs fresh product decision before becoming a candidate | Product decision required |

## Divergent Ideation

Needed: yes.

Rationale: the solution space is broad and easy to collapse too early. The
capability might be expressed as a ticket-led arc planner, daemon-backed campaign
ledger, UI/dashboard portfolio view, workflow coordinator role, negative
authority supervisor, or a hybrid. The design also needs to preserve fresh
context per stage, local-first operation, and deferral accountability.

Suggested tool: Striatum-native `divergent_ideation`.

## Committee / Multi-Perspective Review

Needed: yes.

Rationale: the feature would coordinate many work streams and may start,
sequence, pause, scaffold, or escalate work after a single human-accepted arc.
That crosses authority, provenance, recovery, UI/attention, ticketing, and
workflow semantics. A single-pass design review would miss failure modes the
human explicitly called out, especially silent deferrals and context overflow.

Suggested mechanism: `falsification_gate`, optionally followed by
`multi_review_synthesis`.

## Recommended Sequence

1. Start the next design invocation with current Striatum bootstrap:
   `./go/bin/striatum operator bootstrap --markdown`.
2. Read the five `MULTI_CAMPAIGN_SUPERVISION_*.md` Level-0 artifacts in this
   directory.
3. Run a fresh-context Level-1 `divergent_ideation` workflow. Each branch should
   start from the artifact set, not from this chat session.
4. Require candidate families that materially differ on ticketing substrate,
   authority model, UI/status surface, context-refresh contract, and provenance
   model.
5. Synthesize candidates into an authority/proof/context matrix before choosing
   any lead.
6. Run a falsification gate against the leading design, with explicit attacks on
   silent deferrals, stale success claims, hidden blocked RFCs, context-window
   overflow, daemon bypass, and irreversible unconfirmed actions.
7. If the design changes product behavior, draft an RFC or product decision
   record after review acceptance.
8. Stop at design-to-build readiness. Do not create implementation tickets until
   the design is accepted.

## Fresh-Context Handoffs

Every major phase should begin in a fresh context window. The handoff between
phases is durable artifact and ticket content, not inherited chat memory.

| From | To | Carried |
| --- | --- | --- |
| Level-0 bootstrap | Level-1 ideation | These five artifacts, especially the Stage 1 notes in `MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md`. |
| Ideation branches | Candidate synthesis | Branch artifacts, scored ideas, trap lists, and any explicit unknowns; no raw transcripts. |
| Candidate synthesis | Falsification gate | Candidate recommendation, authority/proof/context matrix, deferral policy draft, and UI/ticketing assumptions. |
| Falsification gate | Product decision | Collaboration ledger, unresolved objections, accepted constraints, and revise/abort/proceed verdict. |
| Product decision | Design-to-build readiness | Accepted design/RFC, ticketing decision, authority matrix, fresh-context packet schema, and stop-condition list. |

## Gating Artifacts

| Artifact | Gate |
| --- | --- |
| `MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md` | Must include live-human Stage 1 notes and the accepted slug. |
| `MULTI_CAMPAIGN_SUPERVISION_REPOSITORY_RECON.md` | Must be current enough for the design operator; rerun bootstrap if stale. |
| `MULTI_CAMPAIGN_SUPERVISION_IDEATION_BRIEF.md` | Must frame multiple strategy families without recommending a winner. |
| Level-1 divergence ledger | Must contain distinct candidates, not cosmetic variants. |
| Candidate synthesis matrix | Must cover authority, proof, tickets, UI, fresh context, deferrals, and stop conditions. |
| Falsification/collaboration ledger | Must either clear the leading design or force revision/abort. |
| Product decision / RFC recommendation | Must decide whether new product authority is required. |
| Design-to-build readiness checklist | Must exist before implementation tickets or build workflows are created. |

## Review Checkpoints

| At | Who | Decides |
| --- | --- | --- |
| Arc problem restatement | Human principal | Proceed, correct scope, or abort before Level 1. |
| Candidate synthesis | Design operator plus reviewer perspectives | Proceed with a lead candidate, revise synthesis, or widen ideation. |
| Falsification gate | Independent falsifier/adjudicator lanes | Accept, accept with findings, needs revision, or reject. |
| Product decision | Human principal / product decision authority | RFC required, decision-log entry sufficient, or no-build. |
| Design-to-build readiness | Operator/reviewer | Ready for implementation planning or blocked on unresolved authority/provenance gaps. |

## Candidate Level-1 Lanes

- Arc and ticketing lane: defines campaign/RFC/slice/ticket vocabulary and local
  vs GitHub ticket substrate options.
- Fresh-context lane: defines per-stage context packets, handoff payloads, and
  continuation rules.
- Authority lane: defines what the meta-agent may do after arc acceptance and
  what must remain human-confirmed.
- Deferral/provenance lane: defines visible deferral justification, completion
  proof, and contradiction detection.
- UI/status lane: defines the ticket board, overall dashboard, and attention
  rules without hiding blocked work.
- Recovery/safety lane: defines pause, quarantine, resume, stop conditions, and
  daemon recovery boundaries.

## Rejected Shortcuts

- Build the ticket UI first: the UI is desired, but storage, authority, and
  deferral semantics must be designed before implementation.
- Treat GitHub issues as the coordination truth by default: useful as a tracker,
  but not local-first and not live workflow state.
- Keep the previous slug: the human replaced it with
  `MULTI_CAMPAIGN_SUPERVISION`; retaining the old slug would orphan artifacts.
- Let a long-lived meta-agent chat window carry the whole arc: this contradicts
  the explicit fresh-context-per-stage requirement.
- Allow the meta-agent to "paper over" failed runs or silent deferrals: this is
  one of the motivating failures.

## Output Gate For Level 1

Level 1 should produce:

- at least three distinct candidate designs,
- a scored ticketing/UI/authority/context/proof matrix,
- falsification findings,
- a recommendation or explicit no-build decision,
- an RFC-or-no-RFC recommendation,
- a design-to-build readiness checklist after acceptance only.
