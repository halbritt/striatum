---
type: record
status: working
feature_slug: STRIATUM_META_OPERATOR
source: feature-design-bootstrap
author: operator-codex-gpt-5-001
created_at: 2026-06-28
---

# STRIATUM_META_OPERATOR Ideation Brief

## Brief

Design a Striatum meta-operator that supervises and coordinates several
campaigns/operators at once so the human principal does not babysit each run by
hand.

The design must preserve the Striatum product boundary: daemon-owned PostgreSQL
is authoritative live state, repository files are durable provenance,
`.striatum/` is scratch, and terminals/provider output are not workflow truth.
It must also respect session-bound capabilities, write scopes, recovery
discipline, checkout isolation, and local-first operation.

This brief is for ideation only. It should not choose an architecture.

## Questions Every Candidate Must Answer

- What exactly is a "campaign" in Striatum terms?
- What does the meta-operator read, and from which authoritative surfaces?
- What does it mutate, if anything?
- How does it avoid becoming a second workflow state machine?
- How does stale evidence expire?
- How does it coordinate several operators without causing checkout or branch
  contention?
- How does it preserve single-repository run invariants while still helping a
  human supervise many efforts?
- What proof is required before it can call a campaign done?
- What conditions force quarantine, refusal, recovery, or human escalation?
- Which decisions require an RFC or command-authority update?

## Repository Constraints To Carry Into Ideation

- Local-first, no hosted service, no telemetry, no external persistence, no
  durable transcript export.
- Daemon MCP/RPC is the legitimate state transition surface.
- CLI commands are compatibility fallbacks and parameter references.
- Direct PostgreSQL mutation is out of bounds.
- GitHub is an issue tracker, not a merge mechanism.
- Red doctor and integrity defects are stop-and-fix conditions.
- Current cross-repo product surface is retired; cross-repo outcomes use
  decomposition and typed handoffs unless reopened by product decision.
- New durable Markdown artifacts use lowercase privacy-safe bylines.

## Evaluation Criteria

| Criterion | Weight | What good looks like |
| --- | ---: | --- |
| Authority fidelity | 0.20 | No ambient authority creep; actions map to daemon capabilities and audits. |
| Proof completeness | 0.20 | Health, completion, and refusal claims can be checked against authoritative surfaces. |
| Operator-load reduction | 0.15 | Human sees fewer, better next actions, not a larger dashboard of partial signals. |
| Blast radius and reversibility | 0.15 | Mistakes pause/quarantine rather than integrate, erase, or silently reroute state. |
| Workflow compatibility | 0.15 | Reuses existing run, lane, artifact, recovery, and review concepts where possible. |
| Implementation realism | 0.15 | Can ship incrementally without a second store or fragile terminal scraping. |

## Reviewer Perspectives

- Provenance integrity reviewer.
- Authority and security reviewer.
- Operator attention and UX reviewer.
- Scheduler and recovery reviewer.
- Git, branch, and checkout hygiene reviewer.
- Product-boundary reviewer.
- Documentation and RFC reviewer.

## ADHD Wide Set

These ideas were generated through the requested ADHD divergent ideation pass.
Scores are qualitative `N/V/F`: novelty, value, feasibility. They are seed
material, not selected architecture.

### Authority And Refusal

- Refusal certificates for every skipped or blocked operator action. `[7/8/9]`
- Per-campaign authority passports signed by daemon state, checkout freshness,
  and issue scope. `[8/7/9]`
- Machine-readable negative authority manifest. `[8/8/9]`
- Freshness-expiring evidence budgets per campaign. `[7/9/9]`
- Counterfactual demand log for actions the meta-operator wanted but lacked
  authority to perform. `[8/7/8]`
- Campaign-published machine-checkable operating constraints. `[7/8/9]`

### Quarantine And Safety Gates

- Quarantine mode for campaigns whose proof chain touches stale branches, dirty
  trees, or red doctor output. `[7/9/10]`
- Read-only supervisor with pause, quarantine, and resume permissions, never
  artifact or git write permissions. `[8/8/9]`
- Per-campaign blast-radius budgets that freeze runs after bounded stale leases,
  red doctor findings, or checkout contention. `[7/8/9]`
- Red-doctor alarm signals that locally stop new work on the same repository
  until the condition decays or is cleared. `[8/7/8]`
- Fresh daemon-scoped trail crumbs for the last invariant proved. `[8/7/8]`
- Recovery error cemeteries keyed by integrity symptom. `[8/6/8]`

### Checkout And Main Capacity

- Daemon-owned campaign airlock that grants one integration token at a time
  across active operators. `[8/7/9]`
- Checkout ownership receipts recorded before any worktree or branch is touched.
  `[7/8/9]`
- Just-in-time lane kitting with fresh `origin/main` worktree, capability token,
  write scope, and health check only when an operator pulls work. `[7/8/8]`
- Dock-door capacity locks requiring clean-tree inspection before a load leaves.
  `[7/8/8]`
- Checkout heat maps that repel new operators from recently touched branches,
  worktrees, and paths. `[8/6/8]`
- Last-mile proof courier that verifies ancestry, doctor green, issue closure,
  and state-doc updates before completion. `[8/9/10]`

### Campaign Logistics And Handoffs

- Cross-dock depot transferring only daemon-signed work packets between campaign
  hubs, never repository edits. `[8/7/8]`
- Returns desk routing failed/refused work to daemon recovery verbs or runner
  defect issues instead of hand-finishing. `[7/9/9]`
- Milk-run collector that gathers compact next-action cards from each campaign,
  not terminal transcripts. `[7/8/9]`
- Quiescence contract requiring each campaign to publish its next safe idle
  point and latest stop deadline. `[8/7/8]`
- Tandem rescue where a stuck lane can recruit one neighbor through a narrow
  rescue signal. `[8/5/7]`
- Issue labels as food trails whose strength rises only after clean integration
  to main. `[7/5/6]`

### Proof And Intent Alignment

- Daemon-recorded intent fingerprints to reject duplicate-but-different campaign
  intents. `[8/8/9]`
- Contradiction hunter comparing run state, anchors, issue state, docs, and
  main ancestry. `[8/9/10]`
- Local escalation queue of smallest reversible unblocks for coordination
  deadlocks. `[8/7/8]`
- Anomaly suppression unless the anomaly has a provenance-corruption path.
  `[7/8/8]`

## Converge

Shortlist these idea families for Level-1 deepening:

1. Contradiction hunter plus last-mile proof courier.
   This makes completion proof multi-witness without granting repair authority.
2. Read-only or negative-authority meta-operator.
   This reduces operator load while minimizing the risk of creating an
   unreviewed second state machine.
3. Campaign logistics and airlock.
   This frames coordination around controlled movement through gates, handoffs,
   quarantine, and integration capacity.
4. Freshness-expiring evidence budgets and authority passports.
   This forces stale observations to expire before they drive decisions.

## Focus

### Candidate A: Contradiction Hunter / Last-Mile Proof Courier

Sketch: a read-only completion gate compares daemon run state, artifact
anchors/hashes, GitHub issue state where applicable, `origin/main` ancestry,
doctor health, and required state-doc updates. A clean result produces a compact
completion proof packet. A dirty result names the failed invariant and stops
completion.

Key risk: the checker could drift into repair, merge, issue-close, or doc-write
authority.

First step: inventory existing machine-readable proof surfaces for one
campaign-complete scenario.

Sub-ideas: completion contradiction matrix, operator-facing proof packet,
recovery router, state-doc freshness check, main-ancestry courier.

### Candidate B: Read-Only Meta-Operator With Negative Authority

Sketch: a daemon-scoped supervisor acts only on invariant failures. Negative
authority means pause, quarantine, refuse, and resume only after daemon recovery
proves the invariant restored. It reads daemon APIs and treats repo files as
provenance, not live state. It has no artifact, commit, issue-close, or report
write authority unless a later design proves a narrow need.

Key risk: authority creep into a writer that can silently alter campaign
outcomes.

First step: inventory existing daemon verbs and states that correspond to
pause, recovery, quarantine, resume, and refusal.

Sub-ideas: invariant catalog, negative authority capability, two-key resume,
doctor-linked campaign block, campaign refusal state.

### Candidate C: Campaign Logistics / Airlock

Sketch: campaigns move like deliveries through intake, depot, quarantine,
capacity, and last-mile gates. The meta-operator reads daemon state and emits
durable provenance explaining movement, pause, and handoff. Dangerous phases are
serialized through gates. Cross-run or cross-repo coordination, if needed, uses
typed handoffs rather than shared mutable state.

Key risk: the logistics layer becomes an unofficial workflow state machine.

First step: draft Level-1 RFC questions for gate vocabulary, typed handoffs,
and invariants proving the gates are views or daemon-backed commands only.

Sub-ideas: capacity board, quarantine-first intake, shipping-manifest artifact,
last-mile gate, depot hazard classification.

## Traps To Avoid

- Issue-label food trails can become stale social proof unless backed by daemon
  and integration proof.
- Tandem rescue can become covert co-driving unless narrowly scoped and audited.
- A global airlock token can deadlock or become an unsupported scheduler if
  release and quiescence proof are unclear.
- Pheromone-style coordination is useful as metaphor but unsafe unless every
  signal is machine-checkable and authority-scoped.
- A dashboard can hide unresolved authority questions behind a nicer surface.

## Provocation

What if the first meta-operator is not allowed to start, finish, merge, close,
or repair anything, and may only prove contradictions, quarantine unsafe states,
and present the smallest reversible next action? If that is too weak, the design
should show exactly which additional authority is required and why existing
daemon verbs cannot supply it.
