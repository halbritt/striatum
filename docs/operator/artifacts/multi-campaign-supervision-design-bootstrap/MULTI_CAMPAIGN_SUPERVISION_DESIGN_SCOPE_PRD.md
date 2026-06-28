---
type: record
status: working
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: feature-design-bootstrap
author: operator-codex-gpt-5-002
created_at: 2026-06-28
updated_at: 2026-06-28
---

# MULTI_CAMPAIGN_SUPERVISION Design Scope PRD

## Level-0 Boundary

This PRD scopes the design effort for multi-campaign supervision. It does not
choose an architecture, data model, ticketing backend, daemon method, UI shape,
or implementation plan.

## Stage 1 Source

Stage 1 used a live-human interview on 2026-06-28. The earlier headless draft
under the previous slug is superseded by this live-human-sourced scope.

- `feature_slug`: `MULTI_CAMPAIGN_SUPERVISION`
- `slug_rationale`: names the capability the human wants: supervising and
  coordinating several Striatum campaign arcs at once. It deliberately does not
  preselect a meta-agent architecture, ticketing substrate, daemon scheduler, or
  dashboard implementation.
- `source_mode`: `live_human`
- `source_precedence_used`: live interview in this operator session.

## Structured Interview Notes

| Area | Status | Notes |
| --- | --- | --- |
| Desired capability | answered | Once the arc of a development effort is understood, the human should not have to initiate every atomic design, build, verifier, or slice workflow by hand. A human should be able to oversee roughly a dozen RFC proposals at once. |
| Motivating pain | answered | Current pain is manual handoff writing, copy/paste process management, keeping context windows fresh after long sessions, lack of visibility across RFCs, and arbitrary deferrals that silently become accepted. |
| Current workaround | answered | The human creates ad hoc process docs, roadmaps, TODOs, and GitHub issues. GitHub issues or a local ticketing system may become a coordination medium, but that is not decided. |
| Intended users | answered | Primary user is a meta-agent guided by a human principal. The human remains the accountable overseer rather than the routine initiator for every work unit. |
| Expected workflow | answered | Given a set of RFC proposals, the human asks the meta-agent for a plan that sequences design, build, and verify workflows. The human accepts the arc, then the meta-agent runs it until a stop condition, scaffolding newly discovered slices and coordinating fresh-agent handoffs as context windows fill. |
| Inputs and outputs | answered | Inputs include RFC proposal docs, roadmap/TODO state, GitHub or local tickets, live Striatum run state, timelines, and accepted arc constraints. Outputs include an accepted arc plan, tickets/work packets, handoff payloads, status dashboard, issue/ticket updates, operator reports, and evidence links. |
| UI / CLI expectations | answered | A UI for work ticketing and introspection is expected. A dashboard showing overall status is desirable. CLI/API shape remains an investigation. |
| Automation expectations | answered | After the human accepts an arc, the meta-agent should run fully until a stop condition. Its control surface should remain minimal enough that coordination does not itself overflow the context window. |
| Persistence / provenance | answered | Persist accepted arc plan, ticket history, workflow launches, handoffs, deferrals, stop-condition decisions, timeline changes, issue/ticket updates, daemon run links, commit/doc evidence, and deferral justifications. Deferrals must be human-visible and justified. |
| Constraints | answered | Preserve Striatum's local-first product boundary, daemon/PostgreSQL live-state authority, privacy boundaries, no hosted services/cloud APIs, no PR merge flow, and current workflow authority. Investigate local-first ticketing rather than assuming GitHub issues. |
| Failure modes | answered | Must not silently accept deferrals, lose provenance, start the wrong workflow arc, hide blocked RFCs, overflow the meta-agent context, bypass daemon authority, or make irreversible changes without human confirmation. |
| Non-goals | answered | Do not choose final architecture in this bootstrap, replace Striatum's daemon state machine, implement cross-repo orchestration, add hosted/cloud coordination, or remove human oversight. |
| Success criteria | answered | The RFC roadmap can be scaffolded and completed in one coordinated instance instead of requiring a human to restart and stitch together each stage. |

## Human Constraints And Preferences

- Each major stage should get a fresh context window. Durable artifacts, tickets,
  and handoff payloads carry context forward; live conversational history should
  not be the load-bearing medium.
- Handoff payloads similar to `$handoff` or `$striatum-handoff` may be useful as
  ticket bodies or continuation payloads, but that is a design input, not a
  chosen implementation.
- Local-first ticketing is preferred if viable. GitHub issues remain an option,
  but should not be treated as the default live coordination substrate.
- Deferrals must have explicit, inspectable justification.

## Solution-Shaped Inputs Reframed

- "Meta-agent" is recorded as the desired coordinating actor, but Level 1 must
  decide whether that actor is a workflow role, daemon-backed operator surface,
  UI-guided agent loop, ticket coordinator, or another shape.
- "UI for ticketing" is recorded as an introspection and control requirement,
  not a decision to build a specific frontend first.
- "Use handoff skills in tickets" is recorded as a context-transfer and
  fresh-window requirement, not a decision about ticket schema or storage.

## Problem Statement

Striatum can drive design, build, and verifier workflows for a single arc of
work, but the human principal still has to initiate and stitch together many
atomic bodies of work. When an effort spans several RFCs, staged builds, and
newly discovered slices, the human carries the portfolio state by creating
handoffs, cutting and pasting context, refreshing agent windows, checking
roadmaps, and noticing silent deferrals.

The design question is how Striatum should let a human accept an arc-level plan
once, then let a constrained meta-agent coordinate the design/build/verify
sequence across many RFCs or feature slices until an explicit stop condition,
while preserving Striatum's local-first daemon authority and provenance model.

## Design Objectives

- Define the unit of coordination: campaign, RFC arc, roadmap item, slice, work
  ticket, workflow run, or another existing Striatum concept.
- Define how an arc-level plan is proposed, accepted, revised, and completed
  without making the live chat context authoritative.
- Define how every major stage starts with a fresh context window and receives
  only durable, bounded context from artifacts, tickets, and handoff payloads.
- Define the ticketing and introspection model, including whether the first
  version uses local tickets, GitHub issues, daemon rows, repository artifacts,
  or a hybrid.
- Define the meta-agent authority model after arc acceptance: what it may start,
  sequence, pause, quarantine, resume, scaffold, update, or escalate.
- Define a deferral accountability model that prevents arbitrary deferrals from
  silently becoming accepted process state.
- Define proof surfaces for "done" across daemon state, workflow artifacts, Git
  state, docs, tickets/issues, and verifier receipts.
- Define a dashboard or UI review model that gives the human portfolio-level
  status without flooding attention.

## Non-Goals

- Do not choose the final architecture in this Level-0 bootstrap.
- Do not implement the multi-campaign supervisor.
- Do not add daemon RPC methods, CLI commands, schema tables, web UI routes,
  workflow shapes, tickets, docs, or tests in this bootstrap.
- Do not replace Striatum's daemon-owned state machine.
- Do not implement cross-repository orchestration or multi-daemon coordination
  without a fresh product decision.
- Do not add hosted services, cloud APIs, telemetry, durable transcript capture,
  external persistence, or provider SDK dependencies.
- Do not remove human oversight; the human accepts the arc and resolves stop
  conditions or explicit checkpoints.

## Investigations For Level 1

| Question | Why it matters | How to investigate |
| --- | --- | --- |
| What is the durable unit of coordination? | The design must not create a second workflow state machine accidentally. | Compare existing terms in `docs/reference/ubiquitous-language.md`, roadmap rows, runs/jobs, and artifact conventions. |
| What ticketing substrate should carry work and handoff payloads? | The human prefers local-first ticketing, but GitHub issues already exist as a tracker. | Compare local daemon/repo artifact options, GitHub issues, and possible local ticket boards against authority, privacy, and UI needs. |
| Which actions can happen after arc acceptance without human confirmation? | The feature must run fully until stop conditions, but irreversible changes still need guardrails. | Inventory existing daemon capabilities, recovery verbs, workflow generators, and state-doc/update rules. |
| What does "fresh context per stage" require? | Long sessions currently fail because live context fills and handoffs are manual. | Design context packets for design, build, verify, slice discovery, and continuation. |
| How should deferrals be represented and reviewed? | Silent arbitrary deferrals are a named failure mode. | Inspect existing action-item ledgers, verdicts, blockers, human checkpoints, and operator reports for reusable shapes. |
| What UI/dashboard status is required? | Human needs overall visibility across many RFCs. | Inventory current dashboard/web surfaces and define what portfolio state must be visible. |
| How does the meta-agent scaffold newly discovered slices? | RFCs may split into slices that are not known when the arc is accepted. | Map current workflow generation and issue/ticket slicing practices without selecting a build plan. |

## Constraints

- Striatum is standalone, local-first, and generic, not Engram-specific
  (`AGENTS.md:3`, `README.md:3`).
- Daemon-owned PostgreSQL is authoritative live state; repository artifacts are
  durable provenance (`AGENTS.md:43`, `README.md:59`,
  `docs/reference/spec.md:31`).
- Marker files, tmux panes, terminal output, provider hooks, and scratch logs
  are not workflow state (`AGENTS.md:50`, `docs/reference/spec.md:38`).
- New hosted services, cloud APIs, telemetry, durable transcript capture, or
  external persistence require an explicit product decision (`AGENTS.md:53`,
  `docs/reference/spec.md:16`).
- State transitions go through daemon MCP/RPC or exact CLI compatibility
  fallbacks, not direct PostgreSQL edits or prose markers (`AGENTS.md:62`,
  `docs/how-to/how-to-agent.md:50`).
- Source reaches `main` through daemon run-integration or direct sync-guarded
  operator commits, not GitHub pull requests (`AGENTS.md:131`).
- A red `doctor` or provenance integrity problem is a stop-and-fix condition,
  not something to route around (`AGENTS.md:149`).
- Fresh design/build/verify context windows are a human requirement from Stage
  1, not a repo-discovered fact.

## Unknowns

- Whether the first viable ticketing surface should be daemon-owned, repo-file
  backed, GitHub-backed, or local UI backed.
- The expected maximum number of concurrently supervised RFCs, runs, slices, and
  fresh-agent contexts.
- Whether the first iteration should be one repository/one daemon only, or
  whether multi-repository visibility is needed before cross-repo mutation.
- The exact stop-condition vocabulary the human will accept for a fully running
  meta-agent.
- Whether existing workflow shapes can model the arc cleanly or a new product
  concept is required.

## Decision Points For Level 1

| Decision | Candidate options | Blocking |
| --- | --- | --- |
| Coordination unit | campaign, RFC arc, roadmap item, ticket, workflow group, run set | yes |
| Ticketing substrate | local daemon tickets, repo artifacts, GitHub issues, hybrid, new local board | yes |
| Authority after arc acceptance | recommend-only, start/sequence workflows, pause/quarantine/resume, scaffold slices, update tickets/docs | yes |
| Fresh-context contract | stage-per-window, slice-per-window, attempt-per-window, checkpoint-triggered refresh | yes |
| UI surface | dashboard extension, ticket board, CLI status, web UI page, combined surface | no |
| Deferral policy | hard human checkpoint, visible deferral ledger, time-boxed deferrals, reviewer challenge gate | yes |
| Product decision path | RFC required, decision-log entry sufficient, no product change | yes |

## Deliverables

- `MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md`
- `MULTI_CAMPAIGN_SUPERVISION_REPOSITORY_RECON.md`
- `MULTI_CAMPAIGN_SUPERVISION_WORKFLOW_SELECTION.md`
- `MULTI_CAMPAIGN_SUPERVISION_IDEATION_BRIEF.md`
- `MULTI_CAMPAIGN_SUPERVISION_DESIGN_PROCESS_PLAN.md`

Expected Level-1 deliverables are defined in the design process plan; this
bootstrap does not create implementation issues.

## Design Acceptance Criteria

- The next design phase starts from live-human-sourced Stage 1 notes, not the
  earlier headless inference.
- The design process requires fresh context windows at major stage boundaries
  and names exactly what artifacts/tickets cross those boundaries.
- The design process produces materially different candidate designs before
  narrowing.
- Candidate evaluation covers authority, local-first ticketing, UI
  introspection, fresh-context handoff, deferral accountability, provenance,
  recovery, and completion proof.
- The review gate explicitly tries to falsify silent deferrals, stale success
  claims, daemon-authority bypass, context overflow, and hidden blocked RFCs.
- The design stops at a design-to-build readiness gate and does not start source
  implementation.
