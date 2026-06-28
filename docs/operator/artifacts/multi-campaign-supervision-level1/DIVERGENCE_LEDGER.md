---
type: record
status: working
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: adhd-level1-scout
author: operator-codex-gpt-5-003
created_at: 2026-06-28
updated_at: 2026-06-28
---

# MULTI_CAMPAIGN_SUPERVISION ADHD Divergence Ledger

## Brief

This ledger records the explicit ADHD pass for Level-1 ideation. Five isolated
branches ran under regulator, logistics, competitor, remove-the-load-bearing-
assumption, and 10-year-old frames.

Scores use novelty, viability, and fit from 0 to 10: `[N V F]`.

## Wide Set

### Evidence-Carrying Ticket Substrate

- Local provenance envelopes, not tasks: scope, authority, evidence,
  deferrals, and handoff payloads. `[N8 V9 F9]`
- Stage passports stamped by design, build, verify, and stop-condition
  artifacts. `[N8 V8 F9]`
- Bill-of-lading bundles for RFC slices with authority, proof receipts,
  deferrals, and delivery-stop clauses. `[N8 V8 F9]`
- Last-mile proof bags: ticket handle, artifact hash, verification command, and
  next fresh-start payload. `[N7 V8 F9]`
- Backpack cards saying who may touch each campaign item, what proof is needed,
  and when it returns. `[N7 V8 F8]`
- Lunch-tray slots for active, waiting, blocked, and proved slices in daemon
  state. `[N6 V7 F8]`

### Expiring Authority And Admissibility

- Chain of expiring capability envelopes instead of a persistent supervisor
  plan. `[N9 V8 F10]`
- Authority warrant that expires at each stage boundary. `[N9 V8 F9]`
- Docketed admissibility decisions for each arc transition, not progress
  updates. `[N8 V8 F9]`
- Stop as the default state; continuation must prove why stopping is worse.
  `[N9 V8 F9]`
- Executable stop contracts: budget exhaustion, proof gap, authority
  escalation, contradiction, or stale arc. `[N8 V8 F9]`
- Hall monitor meta-agent: point, ask, and queue chores, but not build, merge,
  or invent rules. `[N7 V8 F8]`
- Hostile clerk meta-agent: bounded work packets, objections, and stop
  recommendations only. `[N9 V8 F8]`
- Local treaty between bounded agents, renewed only by repo-visible evidence.
  `[N9 V6 F8]`
- Hallway map where rooms are RFC arcs, doors are stop conditions, and stamped
  artifacts unlock doors. `[N7 V8 F8]`

### Deferral Custody And Quarantine

- Deferral registry with custody status, revisit trigger, and poison-pill stop
  rule. `[N8 V9 F10]`
- Every discovered slice carries poison-pill reason, expiry condition, and
  parent-arc link before re-entering planning. `[N8 V8 F10]`
- Discovered slices enter a quarantine ledger until a later arc checkpoint
  grants scope. `[N8 V9 F10]`
- Exception freight tags for damaged, oversized, hazardous, or backordered
  discoveries before repacking. `[N8 V8 F9]`
- Promise board receipt for what changed, what was deferred, and what would
  stop the arc. `[N7 V8 F8]`
- Dock appointment windows for fresh-context pickup, with missed appointments
  routed to deferral. `[N8 V6 F7]`

### Failure-First Portfolio Surface

- Dashboard as compliance surface for blocked, deferred, and refused work, not
  a productivity board. `[N8 V8 F9]`
- Failure console showing ambiguity, authority requests, blocked proofs, and
  stop pressure first. `[N8 V8 F9]`
- Dashboard projection of artifact claims instead of a control surface.
  `[N8 V8 F9]`
- Yard-control dashboard showing staged RFC bundles, blocked bays, returned
  slices, proof bags, and remaining authority envelope. `[N8 V7 F8]`

### Fresh-Context Proof

- Fresh-context restart drills as the proof model: a blank agent must recover
  stage intent from artifacts alone. `[N9 V9 F10]`
- Evidence packets with reconstruction checksums and forbidden-memory
  declarations. `[N8 V7 F8]`
- Sealed envelope for each fresh start: current map, allowed moves, stop signs,
  and missing questions. `[N7 V9 F9]`
- Cross-dock handoff bays for sorting completed design packets into build,
  verify, defer, or return lanes. `[N8 V8 F8]`

### Restraint And Negative Proof

- Negative proof requirements for Level 1 showing no implementation, schema,
  route, daemon method, or build ticket was created. `[N8 V10 F8]`

## Converge

1. `Expiring capability envelopes` is the strongest lead because it binds
   meta-agent authority to stage-specific evidence and gives fresh contexts a
   bounded authority object instead of inherited conversation.
2. `Stage passports / local provenance envelopes` is the most concrete
   ticketing candidate because it turns work units into auditable context and
   proof carriers without making GitHub or a UI authoritative.
3. `Failure-first dashboard plus quarantine ledger` is the best UI and
   deferral candidate because it makes blocked, refused, ambiguous, and
   deferred work more visible than happy-path progress.
4. `Fresh-context restart drills` should become an acceptance test for any
   design candidate, not a separate architecture.

Non-obvious-but-viable pick: star `Expiring capability envelopes`. It is less
obvious than a ticket board, and it directly attacks the dangerous part of the
feature: ambient authority creep across many workflows.

## Traps

- Generic ticket board: likely hides authority, proof, and deferral semantics
  behind familiar task labels.
- Dashboard as control plane: duplicates daemon authority and creates a second
  workflow state machine.
- Daemon lunch-tray state as the first move: risks premature schema design
  before the ticket/proof contract is understood.
- Dock appointment scheduler: turns a design problem into time automation
  before stop and proof semantics exist.
- Local treaty metaphor: useful vocabulary, but too abstract without a concrete
  artifact or daemon mapping.
- Negative proof theater: valuable as a guard, but weak if reduced to a
  checklist rather than validated by diff and artifact scope.

## Focus Seeds

The three branches deepened for synthesis are:

- campaign tickets as stage passports
- campaigns as expiring authority envelopes
- failure/compliance dashboard with quarantine deferral ledger

## Provocation

What if the first shippable meta-agent is only allowed to stop, refuse,
quarantine, and prepare the next authority envelope, while ordinary Striatum
workflow launches stay human-confirmed until the proof model survives review?

