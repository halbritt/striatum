---
type: record
status: working
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: multi-campaign-supervision-level1-frame_problem
author: problem-framer-author-001
created_at: 2026-06-28
updated_at: 2026-06-28
---

# MULTI_CAMPAIGN_SUPERVISION Problem Brief

## Open Question

How should Striatum let a human accept a bounded arc-level plan across many RFCs
or feature slices, then let a constrained coordinating agent carry ordinary
Striatum design, build, verifier, and discovered-slice work forward until an
explicit stop condition, while every major stage can restart from durable
context instead of inherited chat history?

## Starting Context

Striatum already runs structured workflows with daemon-owned live state,
leases, artifacts, verdicts, and recovery paths. The gap is portfolio
supervision: the human principal still has to stitch together many atomic
workflow launches, handoffs, roadmap checks, deferral decisions, and fresh-agent
restarts when an effort spans several RFCs and newly discovered slices.

The desired capability is not just scheduling. It must make the accepted arc,
current stage, authority boundary, handoff payload, discovered work, deferrals,
stop conditions, and evidence handles visible enough that a fresh context can
continue without treating live conversation as state.

## Goals

- Define the durable unit of coordination for a supervised arc.
- Define how a human accepts, revises, pauses, or stops that arc.
- Define what durable context crosses each fresh-window boundary between
  planning, design, build, verify, slice discovery, continuation, and stop.
- Define the ticketing and introspection model, including what must remain
  local-first and when an external tracker may be only a mirror or reference.
- Define the coordinating agent's authority after arc acceptance, including what
  it may start, sequence, scaffold, pause, quarantine, update, or escalate.
- Define how discovered slices and deferred work remain explicit, justified,
  inspectable, and revisitable.
- Define proof for done claims across daemon state, workflow artifacts, Git,
  docs, tickets or issues, and verifier receipts.
- Define the human-facing status surface for supervising roughly a dozen active
  RFC arcs without hiding blocked, deferred, or ambiguous work.

## Constraints

- Striatum remains standalone, local-first, and generic to target repositories.
- Daemon-owned PostgreSQL remains authoritative live state; repository files are
  durable provenance, not the live message bus.
- State transitions must flow through daemon MCP/RPC or exact CLI compatibility
  clients, not direct database writes, marker files, terminal scraping, or prose
  claims.
- The design must not create a second workflow state machine beside the daemon.
- Hosted services, cloud APIs, telemetry, durable transcript capture, external
  persistence, provider-specific SDKs, and GitHub PR landing flow are outside
  the product boundary without a new product decision.
- GitHub issues may be considered only as a tracker or mirror unless a later
  accepted decision says otherwise; they are not merge authority or live
  workflow truth.
- A red doctor, provenance integrity conflict, hidden blocked work, or stale
  evidence conflict is a stop-and-fix condition.
- Each major stage must be able to start in a fresh context window from bounded
  durable artifacts, tickets, and handoff payloads.
- Downstream work recommendations must respect the frozen Level-1 write scopes;
  if the correct later implementation needs paths outside them, the design must
  say so explicitly.

## Non-Goals

- Do not choose the final architecture, data model, ticketing backend, UI shape,
  daemon method, CLI command, route map, or workflow implementation in this
  problem brief.
- Do not create implementation tickets, source edits, schema changes, UI routes,
  daemon methods, tests, or build plans during this Level-1 framing pass.
- Do not remove human oversight. The human accepts the arc and resolves stop
  conditions, authority expansion, and irreversible decisions.
- Do not depend on a long-lived coordinating chat window as the source of truth.
- Do not expand into cross-repository or multi-daemon coordination without a
  separate product decision.
- Do not treat deferrals, comments, labels, or dashboard rows as accepted work
  unless the accepted design defines the authority and proof that makes them
  admissible.

## Decision Criteria

Candidates should be evaluated on:

- Fresh-context robustness: a blank agent can restart each stage from bounded
  durable context without session memory.
- Deferral accountability: deferred or discovered work is justified,
  reviewable, and unable to become silent success.
- Authority fidelity: every action maps to accepted human authority and daemon
  capabilities, with irreversible actions gated.
- Local-first ticketing fit: coordination works without hosted state and keeps
  live state distinct from durable provenance.
- Portfolio visibility: the human can scan stages, blockers, stop pressure,
  deferrals, and evidence across many arcs.
- Completion proof: done claims reconcile daemon state, artifacts, Git, docs,
  ticket or issue state, and verifier evidence.
- Workflow compatibility: the design reuses existing roadmap and workflow
  concepts before inventing new state.
- Implementation realism: the eventual build can ship incrementally without
  terminal scraping, ambient authority creep, or unbounded autonomous loops.

## Questions For Divergence

- What is the durable coordination unit, and how does it relate to existing
  Striatum runs, jobs, artifacts, roadmap rows, RFCs, slices, and tickets?
- What exactly does the human accept at arc start, and what later changes force
  a human checkpoint?
- What is the smallest durable handoff payload that lets a fresh agent continue
  each stage accurately?
- What ticketing substrate can carry work units, handoffs, deferral
  justifications, timeline changes, and evidence without becoming live workflow
  truth by accident?
- What should the dashboard or UI surface emphasize first: progress, blocked
  work, deferrals, stop conditions, evidence gaps, or human decisions?
- What may the coordinating agent do automatically after arc acceptance, and
  which actions must remain recommendation-only or human-confirmed?
- How are discovered slices quarantined, promoted, revised, or rejected?
- What proof is required before an RFC arc, stage, slice, or campaign is called
  done?
- What stop conditions prevent stale context, hidden blockers, silent deferral,
  daemon-authority bypass, or false completion from continuing the arc?
