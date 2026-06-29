# RFC 0172: Proof-only multi-campaign supervision

Status: proposed
Date: 2026-06-29
Context:
`docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md`
(Level-0 scope),
`docs/operator/artifacts/multi-campaign-supervision-level1/IDEATION_SYNTHESIS.md`
(Level-1 synthesis),
[`docs/operator/workflows/multi-campaign-supervision-falsification.json`](../operator/workflows/multi-campaign-supervision-falsification.json)
(falsification workflow, whose context names `SEED.md`),
`docs/operator/artifacts/multi-campaign-supervision-falsification/commit/proposal/DESIGN_RECOMMENDATION.md`
(falsification-cleared recommendation),
`artifact:art_663da6c7b01a3150e5a17fcaf6758c7d`
(falsification final summary), and
`artifact:art_31c6e86dd1f8736942f2db3c5bd30617`
(cycle-3 collaboration ledger).

## Problem

Striatum can run one design, build, review, or verification arc with strong
daemon-owned state and durable provenance, but the human operator still stitches
together larger efforts manually. A single roadmap item can split into RFC
design, implementation slices, verification receipts, recovery follow-up,
documentation updates, and newly discovered work. Today the operator carries
that portfolio state through handoffs, chat context, roadmap scans, tracker
updates, and repeated fresh-agent restarts.

The `MULTI_CAMPAIGN_SUPERVISION` design work asked how a human can accept a
bounded arc-level plan and then let a constrained coordinating agent keep many
Striatum development arcs legible until an explicit stop condition. The
falsification gate cleared only the RFC/product-decision step. It did not accept
an architecture and did not authorize implementation.

The hard risk is authority laundering. A ticket row, dashboard row, replay pass,
receipt, or contradiction report can look like permission to act even when the
daemon has not authorized the exact transition. A stale replay packet can also
age into false done proof after daemon state, artifacts, Git/docs, tracker
mirrors, or verifier evidence have changed.

## Goals

- Define `campaign arc` as the v1 coordination unit for a bounded development
  effort spanning one target repository and one Striatum daemon.
- Preserve Striatum's daemon/PostgreSQL live-state boundary; repository files,
  tracker rows, and dashboard projections remain provenance and read surfaces.
- Make v1 proof-only or recommendation-only: it may emit and inspect authority
  receipts, replay failures, quarantine rows, contradiction reports, and
  recommended next actions, but it may not launch, sequence, promote, or seal
  work by itself.
- Require fresh-context replay before stage advancement and done claims.
- Make deferrals and discovered slices explicit, justified, reviewable, and
  unable to become accepted work by neglect.
- Provide a compact portfolio status surface for roughly a dozen RFC arcs
  without turning status into permission.
- Keep local/private trackers such as Plane as mirrors or operator planning
  surfaces. Existing GitHub issues may remain issue-tracker references, but any
  hosted/external writeback for campaign state requires a later explicit product
  decision and a daemon-reconciled mutation contract.

## Non-Goals

- Do not implement daemon schema, RPC methods, CLI commands, dashboard routes,
  ticket storage, or workflow templates in this RFC.
- Do not authorize a build workflow, implementation tickets, route-map changes,
  schema migrations, or UI work while this RFC is only proposed.
- Do not introduce hosted services, cloud APIs, telemetry, durable transcript
  capture/export, provider SDK integration, or external persistence.
- Do not implement cross-repository mutation or multi-daemon orchestration.
- Do not replace ordinary Striatum runs, jobs, leases, verdicts, blockers,
  artifacts, or verifier receipts with a second workflow state machine.
- Do not let a long-lived coordinating chat window become the source of truth.
- Do not treat GitHub, Plane, or any other tracker as merge authority or live
  workflow authority.

## Binding Constraints

The falsification gate accepted these as mandatory floors for this RFC. They are
part of the proposal, not advisory notes.

`C1-PROVENANCE-NOT-PERMISSION`: receipts, replay passes, quarantine rows, ticket
fields, dashboard rows, and contradiction reports are provenance and
stop-pressure only. They do not authorize start, sequence, scaffold, promotion,
acceptance-state updates, or done seals unless a current daemon-scoped authority
object or explicit human/product decision authorizes that exact transition with
daemon-state reconciliation.

`C2-SAME-BOUNDARY-REPLAY-FRESHNESS`: replay packets, replay pass results,
advancement proofs, and done proofs must name source handles and freshness refs
for all in-scope mutable surfaces, then revalidate those surfaces at advancement
or done-seal time, or prove no in-scope source changed since replay. New
conflicting, red, unreachable, or omitted in-scope evidence is stop pressure.

If an implementation slice cannot preserve both constraints, the correct result
is revision or no-build, not a weakened build.

## Proposal

### Campaign arc

A campaign arc is a daemon-indexed coordination record for one bounded body of
work in one target repository. It names:

- the accepted product/RFC handles that define the arc;
- the target repository and daemon repository id;
- the current phase, such as plan, design, build, verify, repair, or done
  candidate;
- source handles and freshness refs for daemon state, artifacts, Git/docs,
  tracker mirrors, verifier receipts, and quarantine records;
- the current authority receipt, if any;
- explicit stop conditions and required human-confirmed transitions.

A campaign arc is not a run, not a job, not a workflow graph, and not a ticket
board. It is a read/check/report aggregate over existing Striatum authority plus
small proof records. Existing workflow state remains in the ordinary run/job
tables and ordinary daemon methods remain the only way to mutate workflow state.

### Authority receipts

An authority receipt is a stage-scoped proof object that records what the
operator or accepted product decision has authorized for this arc. It should
name:

- authorized phase and exact action class;
- allowed and forbidden actions;
- expiry or renewal criteria;
- evidence handles required before renewal;
- deferrals and discovered slices that are in scope;
- stop triggers and human-confirmed transitions.

The receipt is not permission by itself. A client that wants to start, sequence,
scaffold, promote, update acceptance state, or seal done still needs the current
daemon-scoped authority object or explicit human/product decision for that exact
transition, and the daemon must reconcile the receipt against current state.

### Fresh-context replay

Before an arc advances between major phases, the daemon should be able to build
a bounded replay packet from durable handles. A fresh agent must be able to
reconstruct:

- accepted arc scope;
- current authority receipt and expiry;
- in-scope mutable surfaces;
- known deferrals and quarantines;
- required evidence handles;
- next admissible action, or the stop reason.

Replay failure is a repair item. A replay pass is not durable done proof unless
the same boundary is revalidated at advancement or done-seal time, or the system
proves that no in-scope source changed since replay.

### Quarantine and scope-drift refusal

Every discovered slice or deferred item must be recorded as either refused or
quarantined. A quarantine row should include:

- item title and stable id;
- reason code;
- owner or responsible role;
- source evidence handle;
- wake-up condition;
- re-entry gate;
- current status.

Quarantine is visibility, not permission. Promotion out of quarantine requires
current authority plus replay/freshness proof. A tracker card or dashboard row
showing a quarantined item must not make it eligible for work.

### Cross-surface contradiction report

Before an arc stage advances or a done claim is accepted, a contradiction report
must reconcile at least these surfaces:

- daemon run/job/verdict/blocker/checkpoint state;
- artifact publication and readback state;
- Git/docs state;
- tracker mirror state, if a tracker is selected;
- verifier receipts and claim ledgers;
- known quarantine and deferral rows.

Missing, stale, conflicting, red, unreachable, or omitted in-scope handles are
stop pressure. The report should explain the conflict and recommend a repair or
human decision; it should not perform the repair unless another accepted
authority path explicitly allows that action.

### Portfolio status

The first human-facing status surface should be read-only. It should show:

- campaign arcs and current phase;
- authority receipt status and expiry;
- replay freshness status;
- quarantine count and most important stop reasons;
- contradiction status;
- recommended next action and whether it requires human confirmation.

The UI/CLI copy should use words that preserve the authority boundary. Avoid
labels that imply "ready" means "authorized." Use wording such as
`recommended`, `blocked`, `stop pressure`, `needs authority`, and
`replay stale`.

### Tracker mirrors

Local/private Plane work items or local ticket-board records may mirror arc
state for operator planning, search, assignment, and discussion. Existing GitHub
issues may be referenced as the issue tracker, but this RFC does not authorize
new hosted/external campaign-state writeback. Tracker rows do not become live
workflow state. A local/private tracker mutation may be useful after an accepted
build, but any v1 tracker integration must preserve these rules:

- daemon/PostgreSQL remains the authoritative live state;
- tracker rows are mirrors or durable planning surfaces;
- tracker labels and fields do not authorize Striatum state transitions;
- tracker drift is a contradiction-report input, not permission to continue;
- hosted/external tracker writeback requires a separate accepted product
  decision before implementation.

## Human-Confirmed Actions

These actions remain human-confirmed or product-decision-confirmed in v1:

- accept, revise, pause, or cancel a campaign arc;
- widen arc scope or add a new RFC/slice family;
- promote a quarantined item into active work;
- launch a design/build/verify workflow unless an accepted later RFC defines the
  exact daemon authority object that may do it;
- scaffold implementation tickets or tracker work from a proposed RFC;
- update acceptance state, mark an arc done, or seal a done claim;
- perform irreversible source, schema, data-deletion, deployment, or release
  actions.

## Build Slices After Acceptance

If this RFC is accepted, build should proceed in small slices. These are not
authorized while the RFC is proposed:

1. Terminology and contract inventory over existing run/job/verdict/artifact,
   blocker, checkpoint, claim-ledger, receipt, and tracker-mirror surfaces.
2. Minimal daemon-owned campaign-arc model plus authority-receipt records.
3. Read-only campaign inspection CLI.
4. Replay packet build/check with same-boundary freshness refs.
5. Quarantine/refusal records for deferrals and discovered slices.
6. Cross-surface contradiction report generation.
7. Read-only portfolio status projection for CLI/dashboard.
8. Optional local/private tracker mirror writeback, still provenance-only.
9. Dogfood on one RFC-roadmap subset with proof-only supervision.

New RPC methods or handwritten route maps must update
`docs/reference/command-authority-matrix.md` and the authority guardrail tests.
Any schema-bearing slice must follow the current migration/owner-bundle rules
and the safe database-change deployment policy in
[RFC 0142](0142-safe-by-construction-database-change-deployment.md).

## Acceptance Criteria

- The RFC or its accepted successor defines `campaign arc` and clearly states
  what it is not.
- `C1-PROVENANCE-NOT-PERMISSION` and
  `C2-SAME-BOUNDARY-REPLAY-FRESHNESS` remain explicit, reviewable, and testable.
- v1 is proof-only or recommendation-only unless a later accepted product
  decision defines an exact daemon authority object for a specific action.
- The live-state substrate is daemon-owned PostgreSQL; repository files,
  tracker rows, and dashboard rows remain provenance/read surfaces.
- Fresh-context replay names source handles and freshness refs for every
  in-scope mutable surface.
- Replay failure, contradiction, red doctor, unreachable evidence, and omitted
  in-scope evidence block advancement and done claims.
- Quarantine/refusal records do not grant work permission.
- Portfolio status is read-only in the first surface and does not present rows
  as authorization.
- Any tracker integration is a mirror and is included in contradiction checks.
- The first dogfood can supervise one bounded RFC-roadmap subset without
  launching, sequencing, promoting, or sealing work outside accepted authority.

## Open Questions

1. Is `campaign arc` a new aggregate root with its own table, or a projection
   over accepted RFC/product handles plus proof records?
2. What is the smallest authority receipt that is useful without creating a new
   permission token vocabulary?
3. Should tracker mirrors be read-only in v1, or may an accepted build write
   mirror comments/cards after daemon state changes?
4. Which exact source handles are required for replay freshness: commit SHA,
   artifact ids and hashes, run/job state version, tracker item version, verifier
   receipt digest, and/or decision-log row?
5. How should portfolio status stay compact enough for a dozen arcs without
   hiding stop pressure?
6. Does a later version ever get workflow-launch authority, or should this
   product remain a negative-authority supervisor indefinitely?

## Domain Modeling

`campaign arc` is a repository-scoped coordination aggregate or projection,
depending on the accepted implementation path. It must never own ordinary
workflow transitions. `authority receipt`, `replay packet`, `quarantine row`,
and `contradiction report` are value objects or read models: they are
deterministic, source-handle-bound, and useful for review, but they do not grant
permission. The proposal is a boundary clarification around operator
coordination authority, not a replacement for run/job/artifact aggregate state.
