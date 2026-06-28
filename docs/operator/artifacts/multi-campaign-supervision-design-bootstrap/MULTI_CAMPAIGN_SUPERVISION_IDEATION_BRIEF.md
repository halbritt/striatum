---
type: record
status: working
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: feature-design-bootstrap
author: operator-codex-gpt-5-002
created_at: 2026-06-28
updated_at: 2026-06-28
---

# MULTI_CAMPAIGN_SUPERVISION Ideation Brief

## Brief

Design a process and product shape for supervising many Striatum development
arcs at once. The human wants to accept an arc-level plan across RFCs, then let
a meta-agent coordinate design, build, verifier, and newly discovered slice
work until explicit stop conditions, with each major stage able to start in a
fresh context window.

The design space must remain open. Plausible strategies include, but are not
limited to, ticket-led arc planning, a local-first ticket board, daemon-backed
campaign state, dashboard/status projections, a workflow coordinator role,
negative-authority supervision, or a hybrid. This brief does not choose among
them.

## Problem Space

Current Striatum operation does not give the human one portfolio-level way to
plan and inspect a dozen RFC arcs. The human creates handoffs, cuts and pastes
context, restarts agents, checks roadmaps, and notices deferrals manually. The
feature should make the accepted arc, current stage, slice discovery, handoffs,
deferrals, stop conditions, and evidence visible and durable enough that fresh
agents can continue without relying on a bloated chat window.

## Repository Constraints To Carry Into Ideation

| Constraint | Source |
| --- | --- |
| Striatum is standalone, local-first, and generic. | `AGENTS.md:3`; `README.md:3` |
| Daemon-owned PostgreSQL is authoritative live state; repo files are durable provenance. | `docs/reference/spec.md:31`; `ARCHITECTURE.md:34`; `ARCHITECTURE.md:43` |
| `.striatum/`, terminal output, tmux panes, and provider hooks are not workflow state. | `docs/reference/spec.md:38`; `ARCHITECTURE.md:40` |
| CLI, MCP, web UI, and dashboard are daemon-served or daemon-backed surfaces. | `README.md:19`; `ARCHITECTURE.md:52` |
| New RPC methods or route maps require authority matrix and guardrail test updates. | `AGENTS.md:116` |
| Source landing uses daemon integration or direct sync-guarded operator commit, not GitHub PRs. | `AGENTS.md:131` |
| Red doctor and integrity problems are stop-and-fix conditions. | `AGENTS.md:149` |
| The RFC roadmap already encodes Design -> Build -> Verify as the ship path. | `docs/operator/rfc-roadmap.md:14` |
| Supported workflow shapes include divergent ideation and falsification gates. | `docs/reference/workflow-catalog.md:105`; `docs/reference/workflow-catalog.md:147` |

## Questions Every Candidate Must Answer

- What is the durable unit of coordination: campaign, RFC arc, roadmap item,
  slice, ticket, workflow group, run set, or another concept?
- How does the human accept an arc plan once, and how are later arc changes
  surfaced for confirmation?
- What exact context crosses each fresh-window boundary between planning,
  design, build, verify, slice discovery, and continuation?
- What ticketing substrate carries work units, handoff bodies, deferral
  justifications, and timeline changes?
- What is local-first ticketing in Striatum terms, and when, if ever, are
  GitHub issues appropriate?
- What UI/dashboard surface lets the human introspect the whole portfolio
  without hiding blocked work or flooding attention?
- What can the meta-agent do after arc acceptance: recommend, start workflows,
  sequence stages, scaffold slices, pause, quarantine, resume, update tickets,
  update docs, or escalate?
- Which actions remain human-confirmed, especially irreversible or authority
  expanding actions?
- How are deferrals represented so arbitrary deferrals cannot silently become
  accepted?
- What stop conditions pause the meta-agent, and how are they proven?
- What proof is required before a roadmap item, slice, campaign, or arc is
  "done"?
- How does the design prevent a second workflow state machine from emerging
  beside the daemon?
- How does it keep coordination effort from overflowing the meta-agent's own
  context window?

## Evaluation Criteria

| Criterion | Weight | What good looks like |
| --- | ---: | --- |
| Fresh-context robustness | 0.16 | Every stage can start from bounded durable artifacts/tickets without live-chat inheritance. |
| Deferral accountability | 0.15 | Deferrals are explicit, justified, reviewable, and cannot silently become success. |
| Authority fidelity | 0.15 | Actions map to daemon capabilities and human-confirmed decisions; no ambient authority creep. |
| Local-first ticketing fit | 0.13 | Coordination works without hosted state and distinguishes live state from durable provenance. |
| Portfolio visibility | 0.13 | Human sees the arc, stages, blockers, stop conditions, and evidence across many RFCs. |
| Completion proof | 0.12 | Done claims reconcile daemon state, artifacts, Git, docs, tickets/issues, and verifier receipts. |
| Workflow compatibility | 0.10 | Reuses roadmap and workflow concepts before inventing new state. |
| Implementation realism | 0.06 | Can ship incrementally without fragile terminal scraping or unbounded autonomous loops. |

## Reviewer Perspectives

| Perspective | Lens | Checks |
| --- | --- | --- |
| Authority and security | Can the meta-agent act only within accepted authority? | Capability mapping, irreversible action gates, human-confirmed decisions, command-authority obligations. |
| Provenance integrity | Can every plan, handoff, deferral, and done claim be audited later? | Artifact/ticket durability, daemon links, evidence completeness, no transcript leakage. |
| Fresh-context operations | Can a fresh agent continue each stage without session memory? | Context packet size, required artifacts, stage handoffs, stale-context expiry. |
| Operator attention and UX | Does the UI/dashboard reduce attention load without hiding important state? | Portfolio scan, blockers, deferrals, timeline drift, next human actions. |
| Ticketing/local-first | Does the ticketing model preserve local-first boundaries? | Local storage, GitHub fallback limits, privacy, offline behavior, sync conflicts. |
| Recovery and stop conditions | Does the system stop safely when workflows wedge or evidence conflicts? | Red doctor behavior, quarantine, pause/resume, runner defect routing. |
| Product boundary | Does the design avoid hosted coordination, cross-repo mutation, or a second state machine? | Spec/RFC alignment, non-goals, extension-point discipline. |

## Required Outputs From Level 1

- A problem brief restating the live-human scope and fresh-context requirement.
- At least three distinct candidate designs.
- A candidate matrix covering ticketing substrate, UI surface, authority,
  context handoffs, deferral policy, proof, and stop conditions.
- A falsification ledger focused on silent deferrals, stale success claims,
  hidden blocked RFCs, context overflow, daemon bypass, and irreversible actions.
- A recommendation or explicit no-build decision.
- An RFC-or-no-RFC recommendation.
- A design-to-build readiness checklist only after design acceptance.

## Prior ADHD Seed Set

The earlier ADHD-style seed output remains useful as raw ideation material, but
the live interview narrows the center of gravity toward arc planning, local-first
ticketing, fresh context, UI introspection, and deferral accountability.

Candidate families to explore without choosing a winner:

- Ticket-led arc planner: accepted roadmap arcs decompose into tickets carrying
  stage context, handoff payloads, deferrals, and evidence.
- Local-first campaign ledger: daemon-backed or repo-backed campaign state
  tracks RFC arcs and discovered slices.
- Portfolio dashboard: read/status surface over accepted arcs, tickets, runs,
  blockers, deferrals, and proof.
- Negative-authority supervisor: meta-agent may pause/quarantine/refuse unsafe
  states but cannot silently repair or complete work.
- Workflow coordinator role: a Striatum workflow/session coordinates stage
  transitions and fresh-context packets.
- Proof courier / contradiction hunter: reconciles daemon, Git, docs, ticket,
  and verifier evidence before done claims.

## Traps To Avoid

- A nice dashboard that hides unresolved authority and ticket-state questions.
- GitHub issue labels becoming stale social proof.
- A meta-agent chat session becoming the real source of truth.
- Deferrals encoded as ordinary comments rather than reviewable process state.
- A local ticket store that duplicates daemon live state without clear
  authority.
- Auto-scaffolding slices without a human-accepted arc or stop-condition model.
- Treating handoff payloads as transcript capture.

## Explicit Non-Recommendation

This brief frames the design search for multi-campaign supervision. It does not
select a ticketing backend, UI surface, daemon object, workflow role, or
automation authority. Choosing among those options is Level-1 design work after
divergent ideation and review.
