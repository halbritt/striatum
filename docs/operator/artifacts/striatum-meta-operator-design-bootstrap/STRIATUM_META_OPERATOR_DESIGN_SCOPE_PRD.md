---
type: record
status: working
feature_slug: STRIATUM_META_OPERATOR
source: feature-design-bootstrap
author: operator-codex-gpt-5-001
created_at: 2026-06-28
---

# STRIATUM_META_OPERATOR Design Scope PRD

## Stage 1 Source

This Level-0 artifact was created from the user's headless prompt:

> "we want a Striatum meta-operator - something that supervises and coordinates several Striatum campaigns/operators at once instead of us babysitting each run by hand."

The prompt is sufficient to define the design target and pain, but not enough to
select architecture. Unknowns below are carried forward as design inputs rather
than resolved here.

## Capability Slug

`STRIATUM_META_OPERATOR`

The slug names the desired capability, not a mechanism. It intentionally does
not imply daemon-side scheduler, CLI sidecar, dashboard view, workflow shape, or
operator-report generator.

## Problem Statement

Striatum can run structured local-first workflows, but a human operator still
has to supervise several campaigns, runs, and operators by hand. The current
burden is not only noticing activity. The operator must also decide whether each
run is healthy, whether evidence is fresh, whether a checkout is safe to touch,
whether a stuck lane should be recovered or escalated, and whether completion is
actually proven across daemon state, artifacts, Git state, docs, and issue
state.

The design question is how Striatum should provide a meta-operator that reduces
this babysitting while preserving the existing product boundary: daemon-owned
PostgreSQL is authoritative live state, repository files are durable provenance,
and terminal output, marker files, provider hooks, and scratch logs are not
control-plane truth. See `AGENTS.md:41-58`, `docs/reference/spec.md:31-40`,
and `ARCHITECTURE.md:32-50`.

## Primary Users

- Human principal: the person accountable for several campaigns and for any
  accepted authority escalation.
- AI operator: a constrained local agent acting on behalf of the human principal
  through session-bound capabilities.
- Campaign operator: an operator responsible for one campaign, run, RFC, issue
  slice, or workflow group.
- Reviewer or verifier lane: an agent producing evidence that a campaign state
  or completion claim is valid.

The existing language distinguishes coordinator, operator, human principal,
lane, target repository, live state, and durable provenance in
`docs/reference/ubiquitous-language.md:21-35`, `:58-75`, and `:205-228`.

## Desired User Outcome

A human should be able to ask "what needs my attention across all active
campaigns?" and receive a small, current, evidence-backed set of actions instead
of watching terminals or manually reconciling run state.

A campaign should not be marked healthy, integrated, or complete unless the
meta-operator can prove that status using authoritative Striatum surfaces.

## Current Workaround

The human manually runs bootstrap/status/dashboard/doctor commands, reads
operator briefs, watches multiple terminals, checks Git ancestry and cleanliness,
and reconciles workflow artifacts by hand. This is consistent with RFC 0102's
attention-economy problem statement: operators currently face many heterogeneous
surfaces, identifiers, and partial signals. See `docs/rfcs/0102-operator-attention-economy.md:71-115`.

## Objectives For The Design Process

- Define what a "campaign" means in this capability: one run, several runs, an
  RFC ship path, an issue, a roadmap wave, or another existing Striatum concept.
- Define the meta-operator authority model: read-only observer, negative
  authority supervisor, workflow coordinator, daemon scheduler extension, or a
  sequence of existing workflows.
- Define how stale evidence expires and which observations are safe enough to
  drive a coordination action.
- Define how several campaigns coordinate without violating single-repository
  run invariants, checkout hygiene, branch integration rules, or the daemon as
  the only live-state authority.
- Define a completion proof model that checks daemon run state, artifact
  anchors, Git ancestry, doctor state, required state-doc updates, and issue
  state where applicable.
- Define the review path and artifacts needed before implementation, including
  falsification, authority review, and product decision records.

## Non-Goals

- This bootstrap does not choose the architecture.
- This bootstrap does not implement a meta-operator.
- This bootstrap does not add daemon RPC methods, CLI commands, schema tables,
  dashboard screens, docs, or tests.
- This bootstrap does not authorize hosted services, cloud APIs, telemetry,
  durable transcript capture/export, external persistence, or provider SDKs.
- This bootstrap does not reopen cross-repository orchestration as a product
  surface. Current source says workflows are single-repository per run unless a
  fresh product decision reopens that boundary. See
  `docs/rfcs/0128-cross-repo-run-boundary.md:20-35` and
  `docs/reference/cli-reference.md:481-487`.
- This bootstrap does not bypass daemon recovery. A red doctor or integrity
  defect remains a stop-and-fix condition. See `AGENTS.md:149-165`.
- This bootstrap does not hand-finish stranded lane work, cherry-pick around the
  runner, or treat repo files as live workflow state.

## Hard Constraints

- Striatum is standalone, local-first, and generic, not Engram-specific. See
  `AGENTS.md:3-7` and `README.md:3-22`.
- The daemon-owned PostgreSQL store is authoritative live state. Repository
  artifacts are durable provenance. See `docs/reference/spec.md:31-40` and
  `README.md:59-63`.
- State transitions must go through daemon MCP/RPC or exact CLI compatibility
  fallbacks. Do not scrape terminal output, print magic phrases, or mutate
  PostgreSQL directly. See `docs/how-to/how-to-agent.md:50-93`.
- The meta-operator must respect session-bound capabilities, write scopes,
  checkout isolation, and audit boundaries. See `ARCHITECTURE.md:52-77`.
- New authority surfaces must update the command authority matrix and authority
  guardrail tests before they ship. See `AGENTS.md:112-120`.
- Source reaches `main` through daemon run-integration or a direct
  sync-guarded operator commit, not GitHub pull requests. See
  `AGENTS.md:131-138`.
- State docs must move with state changes. See `AGENTS.md:139-148`.
- New durable Markdown artifacts should use lowercase privacy-safe bylines. See
  `AGENTS.md:124-128`.

## Unknowns To Resolve In Level 1

- Is the first design target one repository, one daemon, or several registered
  target repositories?
- Is "campaign" already a durable Striatum object, a naming convention over
  existing runs, or a new concept that needs an RFC?
- Which actions may the meta-operator take without human confirmation: observe,
  classify, pause, quarantine, resume, requeue, spawn, assign, integrate, close
  issues, or update state docs?
- Can the first version be entirely read-only plus recommendations, or does it
  need negative authority such as pause/quarantine/refuse?
- Should coordination live as a Striatum workflow shape, a daemon subsystem, a
  CLI operator command, a dashboard/control view, or a composition of existing
  surfaces?
- What proof is required before the meta-operator may say "done"?
- How should the meta-operator expose uncertainty, stale observations, and
  refused actions to the human without flooding attention?
- Which existing RFCs are prerequisites, especially RFC 0102, RFC 0116,
  RFC 0122, RFC 0124, RFC 0128, RFC 0167, and constrained operator mode?

## Success Criteria For The Design Process

- Produces at least three materially different candidate designs.
- For each candidate, names authority boundaries, live-state dependencies,
  recovery behavior, completion proof, and failure modes.
- Uses existing Striatum concepts where possible before inventing new durable
  objects.
- Explains how the design avoids becoming a second workflow state machine.
- Includes a falsification review focused on provenance corruption, checkout
  races, stale success claims, authority creep, and operator-attention overload.
- Ends with either a design-ready RFC/proposal or a documented refusal to build
  until a prerequisite product decision is made.

## Level-0 Output Boundary

The design process may proceed from this artifact into Level-1 ideation and
review. It must not proceed directly into implementation.
