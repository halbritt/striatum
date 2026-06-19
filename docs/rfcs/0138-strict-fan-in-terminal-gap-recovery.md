# RFC 0138: A finite terminal-gap recovery exit for a STRICT fan-in barrier with a permanently-unrecoverable required seat — the missing analog of the quorum abstention budget, without silently forging completeness

Status: proposed
Date: 2026-06-19
author: proposer-claude-opus-4-8-001
Context:
- GitHub [#453](https://github.com/halbritt/striatum/issues/453) — "barrier: a
  strict fan-in with a permanently-unrecoverable required seat has no automatic
  terminal-gap exit (parks run in needs_operator)." Source: the failure-mode audit
  (Claude Opus 4.8, 2026-06-19, static-traced, medium confidence) finding
  **FMA-003** in
  [`STRIATUM_FAILURE_MODE_AUDIT_OPUS_4_8_2026-06-19.md`](../../STRIATUM_FAILURE_MODE_AUDIT_OPUS_4_8_2026-06-19.md)
  (§3). Labeled `rfc-0133` — this is in the fan-in / sealed-barrier family.
- [RFC 0133](0133-fan-in-deferred-join-barrier-and-manifest.md) (accepted, D213) —
  the fan-in deferred join barrier + join manifest. This RFC's terminal-gap edge is
  RFC 0133's `is_terminal_gap` in-edge applied to a *strict* (all-siblings-required)
  fan-in, where today the only terminal-gap source is an operator-driven quarantine.
- [RFC 0135](0135-sealed-barrier-primitive.md) (accepted / implemented, D216) — the
  shared `(entity, seal)` sealed-barrier primitive. The readiness predicate
  (`go/pkg/db/barrier_predicate.go`, `BarrierReadySQL`) fires iff every declared
  in-edge `is_terminal_gap OR staged.seal = live.seal`. **The terminal-gap disjunct
  is the entire mechanism by which a barrier fires with a seat absent** — so this
  RFC's question is "what may set `is_terminal_gap` for a strict-fan-in required
  seat, and under what evidence." It composes with the existing predicate, not
  around it.
- [RFC 0132](0132-gating-advisory-reviews-quorum-dissent-protection.md) (accepted,
  D212) / D214 — the quorum **abstention budget** that already solves the analogous
  problem for *panel* barriers: at most `max_gating_abstentions` provably-dead seats
  may be skipped (`resolvePanelSeats` /
  `seatStructurallyUnrecoverable` → `supervisedAgentConfirmedDead`,
  `go/pkg/mutations/barrier_quorum.go`). A *strict* fan-in has no such budget — that
  asymmetry is exactly the gap #453 names.
- Grounded reads at `main` (HEAD `5c904f60`):
  - `go/pkg/mutations/barrier_fanin.go` — the fan-in readiness JOIN. The only
    `is_terminal_gap` source for a fan-in seat today is
    `j.state = 'canceled' AND write_scope_json->>'quarantined' = 'true'`
    (the quarantine marker, `barrier_in_edges` CTE). An outstanding seat with no
    completed job and no quarantine is neither live-sealed nor terminal, so
    `bool_and` cannot be TRUE while it is outstanding — the barrier waits forever.
  - `go/pkg/mutations/recovery_quarantine_lane.go:95-98` — `recovery.quarantine_lane`
    **hard-refuses a non-terminal run** (`ArtifactDebrisRunStateTerminal(runState)`
    gate). It snapshots a *canceled/failed* run's dirty lane worktree; it cannot
    seal a *live* run's seat. So the one mechanism that produces a quarantine marker
    is unavailable on the live run that is stuck.
  - `go/pkg/mutations/barrier_quorum.go` — `resolvePanelSeats` /
    the `StructurallyUnrecoverable` abstention path (D214b).
  - `go/pkg/db/barrier_predicate.go` — `BarrierReadySQL`, `BarrierIsTerminalGapColumn`.
  - `go/pkg/reads/doctor_barrier.go:43-51` — the doctor surfaces
    `barrier_orphaned_staging_ref` and the `barrier_blocked` /
    `blocked_manifest` named condition (RFC 0135 P3, migration 0031
    `barrier_status` view). A strict fan-in stuck on a dead required seat is
    *detectable* here; it is *automatic recovery* that is missing.
- Decisions [D213](../decisions/decision-log.md) (RFC 0133),
  [D214](../decisions/decision-log.md) (the quorum abstention-budget ratifications),
  [D216](../decisions/decision-log.md) (RFC 0135 sealed-barrier primitive).

> **Self-applied discipline.** The load-bearing claim — "a strict fan-in with a
> permanently-dead required seat has NO automatic terminal-gap exit and parks in
> `needs_operator`, while a quorum panel degrades via the abstention budget" — was
> `ASSERTED` (FMA-003), then **`VERIFIED` against source.** The fan-in
> `barrier_in_edges` CTE (`barrier_fanin.go:124-135`) derives `is_terminal_gap`
> *only* from the quarantine marker; there is no abstention disjunct.
> `recovery_quarantine_lane.go:95` refuses any non-terminal run. The quorum path
> (`resolvePanelSeats`, `barrier_quorum.go:366-412`) admits up to
> `MaxGatingAbstentions` provably-dead seats as terminal gaps. So the asymmetry is
> real and verified: **the terminal-gap exit exists for one barrier shape (quorum,
> via a budget) and not the other (strict fan-in).** The audit did NOT fault-inject
> this (no disposable-environment grant), so the *exact* `needs_operator` framing is
> static-traced; the structural gap is verified.

## Problem

### The two barrier shapes degrade differently

Both barrier shapes are instances of the RFC 0135 sealed-barrier primitive: a
barrier fires iff `bool_and(is_terminal_gap OR staged.seal = live.seal)` over its
declared in-edges. They differ only in what may set `is_terminal_gap`:

| Barrier shape | declared in-edges | `is_terminal_gap` source today | degrades when a required seat dies? |
| --- | --- | --- | --- |
| **Panel quorum** (RFC 0132) | the frozen GATING review seats | a `structurally_unrecoverable` seat (provably dead, `supervisedAgentConfirmedDead`) **within `max_gating_abstentions`** | **YES** — the abstention budget admits up to N dead seats as terminal gaps and the barrier fires degraded |
| **Strict fan-in** (RFC 0133) | every declared sibling job (all required) | **only** a quarantine marker (`canceled` + `write_scope_json->>'quarantined'='true'`) | **NO** — no abstention budget; an outstanding required seat with no quarantine keeps `bool_and` FALSE forever |

A panel that loses one provably-dead gating seat (within budget) fires with a
recorded abstention. A strict fan-in that loses one provably-dead *required*
sibling cannot fire — the seat is neither live-sealed nor terminal, and the only
thing that would mark it terminal (a quarantine marker) is produced by
`recovery.quarantine_lane`, which **refuses to run on a non-terminal run**
(`recovery_quarantine_lane.go:95`). So the live run that is stuck cannot reach the
verb that would unstick it.

### What actually happens to a dead required sibling

The dead seat is **not** silently lost. It is first handled by the normal recovery
path: the sweep requeues the same attempt to a fresh lane (RFC 0095 attempt
lifecycle), and on budget exhaustion (`max_requeues`) the run **escalates to
`needs_operator`**. This is loud and bounded: the run stalls, the escalation fires,
and doctor surfaces the condition (`barrier_blocked` / `blocked_manifest`,
`doctor_barrier.go:47`). An operator can cancel the run, or cancel the run and then
`recovery.quarantine_lane` the now-terminal seat.

So the failure is **availability, not corruption**: one run parks in
`needs_operator` until a human acts, with no *automatic* exit. That is genuinely
better than a silent wedge — but it is the one strict-fan-in liveness gap the
audit flagged, and it is the asymmetry with the quorum path that makes it worth a
decision.

### Why this is not simply "add an abstention budget to fan-in"

The quorum abstention budget is sound *because of what a quorum is*: a quorum is a
**ceiling on tolerated silence** over a denominator — the panel was *designed* to
finalize without every seat, and the budget makes "tolerate N absent seats"
explicit and bounded (RFC 0132 framing). A **strict fan-in is the opposite**: every
declared sibling is *required* precisely because the join is *incomplete* without
that sibling's contribution. The whole point of "required" is that the assembled
result must fold in that contribution.

This is the **safety tension** at the heart of #453, and the RFC must surface it
honestly rather than paper over it:

> **Auto-sealing a REQUIRED fan-in seat lets the run complete while the assembled
> output is MISSING a contribution the barrier declared required.** Unlike a
> quorum (where "N absent seats is acceptable" was the declared contract), a strict
> fan-in's contract is "all of these, folded together." Auto-sealing a required
> seat as a terminal gap silently changes the answer the join produces — it forges
> *completeness*. A reviewer or downstream gate reading the assembled artifact has
> no way to know the join is short a required leg unless the gap is made explicit
> and durable.

The quorum path is safe to degrade because its denominator *already* encoded the
tolerance. A strict fan-in encoded the opposite. So any terminal-gap exit for a
strict fan-in must be **opt-in at the barrier level** and must **never silently
forge completeness** — the degraded join must be loudly, durably marked as
degraded, and a downstream gate must be able to refuse it.

### Detectability is already strong; the gap is automatic recovery

`striatum doctor` already detects this: the stuck barrier surfaces as
`barrier_blocked` with a `blocked_manifest` enumerating the blocking in-edge, and
`recovery.quarantine_lane` exists as the operator's manual exit (after the run is
canceled). The missing pieces are (a) a clear, actionable message at the
`needs_operator` escalation and (b) a *decision* about whether an automatic
degraded-fire exit should exist at all — and if so, under what evidence and with
what completeness-honesty guarantees.

## Options

### Option A — operator-gated by design + legibility (smallest change)

Declare the current behavior **intended**: a strict fan-in's required seats are
required, so a permanently-dead one *must* stop and ask a human. Do not add any
automatic degraded-fire path. Instead improve legibility so the operator's exit is
obvious and fast:

- Sharpen the `needs_operator` escalation message for a strict-fan-in barrier
  blocked on a dead required seat: name the seat, name the barrier, and emit the
  exact bounded recovery commands (cancel the run, or cancel + `recovery
  quarantine-lane` the seat — though note the quarantine path needs the run
  terminal first, which is itself a friction worth surfacing).
- Add (or confirm) a doctor problem class that is specific to this case — a
  `barrier_blocked` sub-reason like `strict_fanin_required_seat_unrecoverable` — so
  the operator sees "this is the FMA-003 case" rather than a generic block.
- Document the contract in the RFC 0133 / RFC 0135 design and in
  `docs/reference/spec.md`: **a strict fan-in does not auto-degrade; a dead required
  seat is an operator decision by design.**

**Trade-offs.** Cheapest and safest — it cannot forge completeness because it never
fires a short join. But it leaves the availability gap: a run still parks until a
human acts, even when the seat is *provably* dead with no possibility of recovery.
It also leaves the asymmetry with the quorum path unexplained except as "fan-in is
stricter on purpose," which is defensible but is a *policy* answer, not a mechanism.

### Option B — opt-in terminal-gap seal after recovery exhaustion, with a structured degraded-run artifact (recommended)

Allow a strict fan-in to fire degraded **only when the barrier explicitly declares
it tolerates a sealed gap**, and **only after recovery is exhausted on a provably
dead seat**, and **only by emitting a structured `terminal_gap` record so
completeness is never silently forged.** Concretely:

1. **A per-barrier opt-in flag** in the workflow definition (no DDL — this is
   workflow config, the D215 pattern), e.g. `fanin_tolerates_sealed_gap: true`
   (default `false`, so behavior is unchanged for every existing strict fan-in).
   The absence of the flag is Option A's behavior exactly.
2. **The gap is admitted ONLY for a provably-dead seat**, reusing the quorum path's
   `structurally_unrecoverable` oracle (`seatStructurallyUnrecoverable` →
   `supervisedAgentConfirmedDead`, `barrier_quorum.go:502`). A *slow* seat is never
   a gap (silence from a live lane is not consent — the D214b axiom carries over).
   Optionally bound it like the quorum budget (`max_sealed_gaps`, default 0) so the
   author declares *how many* required legs the join may be short.
3. **The seal sets `is_terminal_gap` for that in-edge** — the same disjunct the
   quarantine marker already sets in `barrier_in_edges` — so the existing
   `BarrierReadySQL` predicate fires unchanged. No new predicate; the terminal-gap
   edge is the composition point RFC 0135 designed.
4. **The fire emits a durable, structured degraded record.** The seat appears in the
   `join_manifest.v1` with `status: "terminal_gap"` (not `staged_live`) and a
   `damage_code` (e.g. `required_seat_unrecoverable`), exactly the way a quarantined
   seat already appears (`faninManifestEdgeForSeat`,
   `barrier_fanin.go:809-863`). Additionally, mark the **run** itself degraded (a
   `degraded_run` / `terminal_gap` signal in run state or an emitted artifact) so a
   downstream gate and the operator both see "this run completed with a sealed gap —
   it is NOT a complete join."
5. **A downstream gate can refuse a degraded join.** The completeness honesty is
   enforced, not advisory: a gate that requires a full join refuses a manifest
   carrying a `terminal_gap` edge unless it *also* declares it tolerates the gap.
   This is what makes Option B "not silently forge completeness" rather than just
   "fire and hope nobody notices."

**Trade-offs.** This is the mechanism that closes the availability gap *and* keeps
the safety property: the only way a required seat is sealed is (a) the author
explicitly opted in, (b) the seat is provably dead, and (c) the result is loudly
marked degraded with a downstream refusal path. It mirrors the quorum abstention
budget one-for-one (provably-dead-only, bounded, durable record), which is the
strongest argument that it is the right shape: the analogous problem is *already*
solved this way for panels. The cost is real new surface — the flag, the
manifest/run degraded signal, the downstream-refusal gate, and the doctor
invariants that fire when a degraded run's `terminal_gap` record is missing or
inconsistent. It is more than a patch; it is a small feature behind an opt-in.

### Option C — hybrid: auto-escalate with a bounded operator-decision window, then a declared default

Keep the human in the loop but bound the wait. On recovery exhaustion of a strict
fan-in required seat, escalate to `needs_operator` **with a deadline and a
pre-declared default**: the barrier declares `on_unrecoverable_required_seat:
{action: seal_gap | cancel_run, after: <window>}`. Within the window an operator may
choose (cancel, requeue with a fresh lane, or seal); if the window elapses with no
operator action, the daemon applies the declared default automatically.

**Trade-offs.** This gives the operator first refusal (so a recoverable situation is
not auto-sealed prematurely) while still guaranteeing a *finite* exit even fully
unattended — which is the autonomy property the project values. But it adds a
*time-based* recovery transition (a new scheduler concern: a deadline timer per
blocked barrier), which is more moving parts than B, and the "declared default =
seal" branch has *exactly* B's completeness-forging risk, so it must carry all of
B's degraded-run honesty machinery anyway. C is essentially "B plus a timed operator
window." It is the right answer only if the unattended-finite-exit guarantee is a
hard requirement; otherwise the window is complexity B does not need.

## Recommendation

**Adopt Option B, structured as RFC 0135's terminal-gap edge with an explicit
per-barrier opt-in and a durable degraded-run record — and ship Option A's
legibility improvements unconditionally as the first slice (they are valuable even
if B never lands and they are the right default behavior when the opt-in is off).**

The decisive argument is the **symmetry with the already-shipped quorum abstention
budget**: the project already accepted (D214) that a barrier may fire with a
*provably-dead* seat skipped, bounded by an explicit budget, recorded durably — for
panels. Option B is that exact contract applied to strict fan-in, reusing the same
`supervisedAgentConfirmedDead` oracle and the same `is_terminal_gap` predicate
disjunct, with the *additional* honesty requirement (a degraded-run signal + a
downstream refusal path) that fan-in needs and quorum did not, *because a strict
fan-in's "required" means the join is genuinely incomplete without the seat* — where
a quorum's denominator already encoded the tolerance.

This keeps the safety property the audit cares about: completeness is **never
silently forged**. A required seat is sealed only when the author opted in, the seat
is provably dead, and the result is loudly degraded with a refusal path — so a
downstream reader can always tell a complete join from a degraded one. The default
(opt-in off) is Option A, which is itself a sound, documented contract.

Reject Option C as the *default* shape (it is B plus a timer; ship the timer only if
a hard unattended-finite-exit requirement emerges), and reject a bare unconditional
abstention budget on fan-in (it would degrade a strict join with no author opt-in
and no completeness record — the silent-forge failure).

## Acceptance criteria (testable)

These fence the recommended Option B (and its Option-A first slice). Each is a
behavior the implementation must satisfy and a fixture must assert:

1. **Default unchanged (opt-in off).** A strict fan-in barrier with no
   `fanin_tolerates_sealed_gap` flag, with one declared sibling permanently dead
   (`supervisedAgentConfirmedDead` true), after recovery exhaustion, **stays
   blocked** and escalates to `needs_operator` — `BarrierReadySQL` returns FALSE.
   *No automatic fire.* (Fixture: drive the fan-in, kill one required seat, assert
   the barrier never fires and the run parks. This is the FMA-003 reproduction the
   audit listed under "Gated Verification §7.4.")
2. **Legibility (Option A, ships unconditionally).** The blocked barrier surfaces a
   *specific* doctor reason (e.g. `barrier_blocked` with a
   `strict_fanin_required_seat_unrecoverable` blocked_manifest entry naming the dead
   seat), and the `needs_operator` escalation message names the seat, the barrier,
   and the exact bounded recovery commands. (Fixture: assert the doctor problem
   record and the escalation message content.)
3. **Opt-in fires only for a provably-dead seat.** With
   `fanin_tolerates_sealed_gap: true` and `max_sealed_gaps: 1`: a *provably dead*
   required seat is admitted as a terminal gap and the barrier fires; a merely
   *slow* (not provably dead) required seat does **not** fire (silence != consent).
   (Fixture: parameterize the seat's liveness oracle; assert fire iff dead.)
4. **Budget bounds the gap.** With `max_sealed_gaps: 1` and *two* provably-dead
   required seats, the barrier does **not** fire (the second dead seat exceeds the
   budget and stays blocking) — mirroring `resolvePanelSeats`' beyond-budget rule.
5. **Completeness is never silently forged.** When the barrier fires with a sealed
   gap, the `join_manifest.v1` records the seat with `status: terminal_gap` and a
   `damage_code`, **and** the run carries a durable degraded/`terminal_gap` signal.
   A downstream gate that requires a full join **refuses** the degraded manifest
   unless it also declares it tolerates the gap. (Fixture: assert the manifest edge,
   the run signal, and the downstream refusal.)
6. **Doctor catches an inconsistent degraded record.** A fired-with-gap barrier
   whose manifest claims a `terminal_gap` but whose run lacks the degraded signal
   (or vice-versa) reddens doctor — completeness honesty is an *invariant*, not a
   convention. (Fixture: seed the inconsistency; assert a doctor problem.)
7. **Composes with the RFC 0135 predicate (no predicate fork).** The gap is set via
   the existing `is_terminal_gap` in-edge column; `TestBarrierPredicateHasNoRefCount`
   stays green (no new `COUNT(*)`-of-refs shape), and the fan-in readiness SQL still
   routes through `db.BarrierReadySQL`. (Fixture: the static guard plus a readiness
   test over the new terminal-gap source.)

## Composition with the sealed-barrier family

This RFC deliberately does **not** mint a new barrier mechanism. It composes with
existing, accepted work:

- **RFC 0135's `is_terminal_gap` disjunct** (`barrier_predicate.go`) is the single
  composition point — the sealed gap sets the same edge column the quarantine marker
  already sets, so the readiness predicate is unchanged and the trap-killer property
  (`staged.seal = live.seal`, never a ref `COUNT(*)`) is preserved.
- **The quorum abstention budget** (`barrier_quorum.go`, D214b) is the proven
  template: same `supervisedAgentConfirmedDead` oracle, same provably-dead-only rule,
  same bounded budget, same durable record. Option B is that contract applied to
  strict fan-in with the added completeness-honesty machinery fan-in requires.
- **The shared-primitive direction in #354 / RFC 0133.** Because the gap is an
  `is_terminal_gap` edge in the shared `(entity, seal)` primitive, the
  unrecoverable-required-seat policy is expressible *once* across callers; if a
  future caller needs "fire degraded on a provably-dead required in-edge," it inherits
  this contract rather than re-discovering it (the same way #354 unified the four
  barrier callers under one predicate).
- **`recovery.quarantine_lane`** stays the operator's manual exit and stays
  terminal-run-only by design (it snapshots a *canceled* run's dirty worktree — a
  distinct concern from sealing a *live* run's barrier seat). Option B's sealed gap
  is the *automatic, opt-in, provably-dead-only* path that the manual quarantine is
  not; the two do not overlap. (A clarifying note in `recovery_quarantine_lane.go`
  pointing at the sealed-gap path for the live-run case is a cheap legibility win.)

## Open questions

1. **`max_sealed_gaps` budget, or boolean opt-in only?** A boolean
   `fanin_tolerates_sealed_gap` is the simplest opt-in (tolerate *any* number of
   provably-dead required seats); a `max_sealed_gaps: N` budget mirrors the quorum's
   `max_gating_abstentions` and bounds *how incomplete* a join may be. Recommendation:
   the budget form (default 0), for exact symmetry with quorum and a tighter contract
   — pinned before implementation.
2. **Where does the run-level degraded signal live?** A new run-state value
   (`completed_degraded`?) is the most legible but touches the run-state CHECK (an
   owner-bundle concern per D187/D215); an emitted `degraded_run` / `terminal_gap`
   *artifact* with front matter is no-DDL and composes with the existing publisher
   schema guard. Recommendation: the artifact form first (no DDL), with a run-state
   value deferred unless a gate needs to filter on it cheaply. Pin before the
   completeness-honesty slice.
3. **Should the downstream-refusal be the default or opt-in?** Safest is *refuse a
   degraded manifest by default* (a gate must explicitly declare it tolerates the
   gap to consume it), so completeness honesty is fail-closed. Confirm this is the
   default direction (it should be).
4. **Is Option C's timed window ever required?** Only if a hard unattended-finite-exit
   guarantee is declared (the autonomy-mission property). If so, C is "B plus a
   per-barrier deadline timer with a declared default"; the timer is the only extra
   surface. Defer unless the requirement is explicit.

## Domain Modeling

This is a **boundary clarification plus a new value object**. The new value object is
the **sealed gap**: a declared-required in-edge that the barrier fires *without*,
recorded as a first-class `terminal_gap` contribution carrying a damage code — the
fan-in analog of the quorum's `abstain` classification, but with the explicit
completeness-honesty obligation that a strict join's "required" demands. The boundary
clarification is that **a strict fan-in's completeness is a domain invariant that may
only be relaxed by an explicit author declaration, a provably-dead seat, and a
durable degraded record** — never silently. The terminal-gap edge is evaluated under
the same per-run advisory lock (RFC 0104) and the same `(entity, seal)` predicate
(RFC 0135). Cites
[`docs/explanation/domain-driven-design.md § "Adding to the model"`](../explanation/domain-driven-design.md#adding-to-the-model);
RFC 0019 is the precedent.
