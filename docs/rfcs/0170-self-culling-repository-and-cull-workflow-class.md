# RFC 0170: Self-culling repository — a CULL workflow class (detect and shed dead artifacts)

Status: proposed / P0 implemented (D271; runtime schema 45)
Date: 2026-06-25
Context: deep architecture review 2026-06-24 (`docs/audits/STRIATUM_DEEP_ARCHITECTURE_REVIEW_CLAUDE_OPUS_4_8_2026-06-24.md`, "machine-speed accretion with no brake"); RFC 0020 (autonomous stalled-run recovery / the sweep), RFC 0122 (daemon auto-spawn scheduler), RFC 0106 (shape graduation + `falsification_gate`/`verification_gate`), RFC 0134/0141 (verifier sealed receipts), RFC 0136 (hash-chained `event_chain_segment`), RFC 0047 (verdict supersession columns), RFC 0117 (worktree/branch ref safety), RFC 0167 (operator identity & run attribution)
author: proposer-claude-opus-4-8

> **Provenance.** Scoped with a divergent-ideation pass (5 cognitive frames —
> apoptosis/autophagy/immune, regulator, ant-colony, markets, 3am-on-call — × 6
> ideas → scored/clustered → top-3 deepened, each deepening verified against the
> live tree). The converged result is **three coupled mechanisms**, recorded
> below. This is a freshly-proposed sketch; if it is accepted it should be
> hardened through Striatum's own design→build→verify pipeline (a `cull_gate`
> design run is the natural first dogfood), and the authoritative spec then lives
> in the committed design proposal, superseding this sketch where they differ.
>
> **P0 implementation.** D271 ratified the observe-only P0 slice, and
> `run_992bd797fc136f1e3d782f443f9fb2ad` built and verified it on `main`
> (`02a15e83`): runtime migration 0045 adds `cullable_entity`, and
> `DecayTickSweep` now nominates/withdraws Tier-1 candidates without deletion,
> paging, or run-admission effects. The #618 citation-exactness hardening is
> closed: `status: frozen` Markdown records outside `docs/records/_frozen/` are
> non-live citation sources. The #619 cull-slot liveness fence is also closed:
> expired scan generations can no longer starve later scans or commit late
> candidacy deltas. Broader P1+ tombstone, doctor, `cull_gate`, and reaper work
> remains future work.

## Problem

Striatum has three workflow classes — **DESIGN** (`falsification_gate` hardens an
RFC spec into criteria), **BUILD** (`code_change` implements it in reviewed
slices), **VERIFY** (a sandboxed verifier mints a sealed receipt; the executor
cannot verify itself). **All three add. Nothing subtracts.** The deep
architecture review of 2026-06-24 measured the consequence: **+85.5K Go LOC
against −2.8K deleted in 13 days**; 41 unmerged `rfc-…-design-vN` scaffold
branches; dead subsystems (a circuit breaker, the `crossrepo` package) that
survived ~four reviews before a human finally named them; 169 RFCs and 65
Postgres tables; a README that drifted stale.

The root cause is not a missing tool. Subtraction is **optional, lower-status,
and human-triggered**: it happens only when a reviewer points at a specific
corpse, and it loses every scheduling contest to the next feature. An
accretion-biased agent fleet running an add-only pipeline will accrete
indefinitely. The runner that drives the building cannot drive the shedding.

**Root reframe (mirroring RFC 0162's "alert on absence of success").** Deletion
must become a *first-class, continuous, runner-driven, provenance-disciplined*
mutation — with the same evidence bar the project already demands of every other
state change: an audited record, a second key, refusability, and reversibility.
A deletion is the **most dangerous** mutation there is (irreversible loss; it can
silently remove a safety check; it can mis-fire on a caller static analysis
missed), so the second key here must certify a *negative* — the **absence of
use** — and a wrong cull must page nobody.

## Goals

- The repo **continuously sheds** dead code, stale branches, superseded
  RFCs/decisions, and drifted docs, without a human first naming each corpse.
- Every cull is an **audited, refusable, reversible** daemon mutation carrying a
  **second key** that independently attests absence-of-use (executor ≠ verifier).
- A **wrong cull never pages the operator**: quarantine-before-delete, a soak
  window, and a receipt that auto-voids and resurrects from the audit chain.
- A **counterforce** makes subtraction win scheduling contests instead of losing
  them — accretion that is not being shed becomes a throttle on further building.
- Everything reuses machinery that already exists (the recovery sweep, doctor,
  the verifier, run-admission, supersession columns, the auto-spawn scheduler) so
  the net new surface is small and shadow-first.

## Non-Goals

- Hosted/cloud/telemetry anything (D094 boundary unchanged).
- LLM-opinion-based deletion. Deadness is proven by graph/execution evidence, not
  by a reviewer's vibe; an LLM lane may *propose* a candidate but may not be the
  evidence.
- A new internal credit/currency economy. The counterforce settles through plain
  run outcomes, the audit chain, and the existing release gate — not minted
  tokens (see Rejected alternatives).
- Hard deletion of anything in the protected **root set** (shipped behavior, open
  issues, the spec, live runs) — ever.

## Proposal

Three coupled mechanisms — a **trigger**, a **reversible cull**, and a
**counterforce**. None alone suffices: the trigger without the counterforce still
loses to BUILD; the counterforce without the reversible cull is unsafe; the cull
without the trigger is still human-driven. The dependency between them is the
point.

### Pillar 1 — Decay-and-reachability trigger (continuous candidacy)

A runtime migration (next free runtime slot, ~`0045`; **runtime**, not owner —
the runtime role must DML it, so it MUST carry read/write authority-inventory
rows or it red-mains CI under `TestWriteAuthorityInventoryComplete` and the new
non-PG `authority_inventory_static_test.go`) adds a `cullable_entity` ledger:
`(kind, ref, last_reinforced_at, decay_score, reachable_from_root, candidacy_state)`
for `kind ∈ {code_symbol, file, package, branch, rfc, decision, doc, table}`.

A new `DecayTickSweep` in `go/pkg/recovery` implements `SweepOnce` and piggybacks
the existing recovery-sweep timer (`scheduler.go`/`sweep.go`). On each tick it
half-life-decays every entity's `decay_score`, **resetting the clock only on a
real inbound edge**. Reference edges are indexed in **cost tiers, cheapest
first** so value lands before the expensive graph is built:

- **Tier 1 — supersession (free today).** `verdicts.superseded_by_decision_id` /
  `superseded_at` already live in Postgres (RFC 0047 / migration 0007); the
  `Status: superseded by …` markdown convention and `rfc-…-design-vN` → ratified
  `vN+1` are read directly. A superseded artifact with a live successor is the
  **cheapest possible candidate — the human already pointed, just at the
  replacement.** No AST, no LLM.
- **Tier 2 — route/contract edges.** RPC-method and CLI-route references read
  from `contracts/daemon_methods.json` (codegen, not AST).
- **Tier 3 — Go import/call/coverage graph** (incrementally maintained).
- **Tier 4 — doc cross-links** for the drifted-docs class.

An entity whose decayed score crosses a midden threshold **and** is unreachable
from the protected root set auto-enqueues a CULL candidacy. Deadness is thus
**provable by traversal**, not asserted.

### Pillar 2 — The reversible cull with an absence-of-use second key (`cull_gate`)

CULL ships as a **generatable workflow shape, `cull_gate`** (sibling to
`verification_gate`/`falsification_gate` in `workflowtemplates`), so a cull is
never a bare daemon-side delete — it runs through the existing lane/verdict/seal
machinery: a **holder** lane proposes the candidate + a death certificate
(cause-of-death = the Pillar-1 evidence); an independent **falsifier** lane is
tasked *only* with finding one live caller; the sealed **verifier receipt is the
second key, attesting absence** (full `go build`/`vet`/`test`/route-gen pass with
the bytes **shadow-removed**).

Culling is **two-phase and written as events on the RFC 0136 hash chain**, so an
un-cull is a backward chain replay — nothing leaves the audit log:

- **Phase 1 — tombstone (reversible).** Per kind: code → moved to a `_graveyard/`
  tree as a **real, greppable** symbol (deliberately *not* `//go:build`-tagged
  out — see the load-bearing risk); branch → `tags/graveyard/<branch>` with the
  ref deleted; RFC/decision → `status='tombstoned'` + `death_certificate_event_id`
  (extending the doctor suppression already used for `recovery.debris_pruned`
  tombstones); table → renamed `zz_tombstoned_*` and revoked, **never `DROP`ped**.
  The receipt is **staked**: each subsequent sweep re-checks it, and if the build
  no longer passes shadow-removed or any reference reappeared, the receipt
  **auto-voids and the entity silently resurrects from the chain — paging
  nobody**, and the sweep's blast-radius shrinks.
- **Phase 2 — reap (irreversible).** A timed reaper hard-deletes only a tombstone
  that survived the full soak window with a still-valid receipt, requiring a
  **fresh** end-of-soak second attestation (not just the surviving original),
  capped at N-per-sweep with no cascade, auto-paused on any doctor-red.

Refusal taxonomy (fail-closed): `removes-an-unguarded-invariant` blocks culling
any symbol whose shadow-removal flips **no** test red — an untested symbol's
absence is unprovable, so it is *ineligible*, not eligible.

### Pillar 3 — The counterforce that makes subtraction win

Three controls invert the incentive that the review identified (cleanup is
low-status and always loses to building):

- **Doctor state "unrefuted accretion."** A new read-only block in
  `go/pkg/reads/doctor.go` (template: `doctorRecoveryGateIntegrity`), backed by an
  append-only `accretion_ledger` the daemon writes at run-integration from
  `git diff --numstat -M -C origin/main..run-branch` (so renames/moves/churn net
  to zero; generated/vendored paths excluded via a `.check-docs-ignore`-style
  pathspec — only **genuine net removal** counts). When net surface over a rolling
  per-repo window exceeds budget **with no open CULL run**, doctor goes **amber**.
- **Run-admission throttle (shadow-first).** Behind `STRIATUM_ACCRETION_REFUSE`
  (default off), `HandleRunStart` (`go/pkg/mutations/run.go`, already the
  run-admission chokepoint that refuses concurrent running-on-branch) refuses new
  BUILD/DESIGN starts while accretion is amber — **CULL becomes the only class
  allowed to start** until the books rebalance. Scarcity-of-culling is itself the
  red.
- **Clearing-house at release.** Reusing the `check_release_version.py` CI slot,
  the VERSION bump is refused unless the period's net surface ≤ 0 **or** a signed
  **overdraft artifact** (a front-matter decision attributed to the
  operator-principal via RFC 0167) is present. The overdraft is durable, dated,
  attributable, **auto-expires into cull-only** if not netted next period, and is
  visible in `striatum whose` — an honest, self-closing escape valve, not a
  rubber stamp.

The ledger should ultimately meter **coupling/surface** (net new exported
symbols, RPC methods, tables, RFC files — the authority-inventory CI guard
already counts new tables) rather than raw LOC, so a refactor that removes an
interface pays down debt while a 50-line feature that adds a table accrues it,
and "deletion theater" (ripping out code nobody ran) does not earn credit.

### Phasing (shadow-first throughout)

- **P0** — `cullable_entity` ledger + `DecayTickSweep`, **Tier-1 supersession
  only**, read-only candidacy. Observe; no deletion.
- **P1** — `cull_tombstone` ledger + doctor integrity block (RED on a voided or
  soak-expired receipt). The invariant before any reaper (the RFC 0136 P1 pattern:
  ship the doctor seam with no executor behind it).
- **P2** — `cull_gate` shape (holder + falsifier + sealed absence-receipt);
  two-phase tombstone; **manual** reap only.
- **P3** — timed reaper behind the soak window + resurrection-rate governor +
  blast-radius cap + auto-pause on doctor-red.
- **P4** — `accretion_ledger` + doctor "unrefuted accretion" **amber** (observe
  only), calibrated against the real +85.5K/−2.8K history before any enforcement.
- **P5** — wire the throttle into `HandleRunStart` (shadow
  `STRIATUM_ACCRETION_REFUSE`) + clearing-house at release; switch the meter from
  LOC to coupling/surface.

## Rejected alternatives (traps)

- **RFC/code prediction market on "will it be cited."** Requires a betting/credit
  economy, is gameable, and citation is a weak proxy for "should exist."
  Over-engineered for one operator. Keep the adversarial *role* (the falsifier),
  drop the market.
- **Hard cap-and-trade at the migration boundary** ("cannot add table #66 without
  retiring one"). A hard admission law wedges legitimately-needed growth and risks
  a #431-class "the daemon won't let me fix the daemon" deadlock. Use **soft**
  back-pressure (doctor amber + the signed overdraft), never a hard gate on adds.
- **An internal minted-credit currency** (short-seller "earns credits"). Inventing
  a token economy is overbuild; settle through plain run outcomes and the audit
  chain.
- **`//go:build cull_tombstone` that compiles the symbol out.** It then goes
  invisible to the very build/test signal meant to falsify its death receipt — a
  guaranteed false reap after the soak. Tombstones must stay **real-but-shadowed,
  greppable** symbols.
- **LLM-judged deadness.** Static/execution reachability is the evidence bar; an
  LLM may propose, never certify.

## Acceptance Criteria

- A superseded RFC/decision and an `rfc-…-design-vN` branch with a ratified
  successor are auto-nominated by P0 with cause-of-death pre-filled, **with zero
  false positives across the existing supersession corpus** (Tier-1 is exact).
- A `cull_gate` run cannot reach a reap without (a) a falsifier lane that failed
  to find a live caller and (b) a sealed verifier receipt proving build+tests pass
  shadow-removed; either absent ⇒ the run refuses, not proceeds.
- Touching a tombstoned symbol during the soak (a new import, a run reference, a
  human edit) **voids the receipt and resurrects the bytes from the audit chain in
  the next sweep**, with a regression test proving no page and no data loss.
- Culling a symbol whose shadow-removal flips no test red is **refused**
  (`removes-an-unguarded-invariant`), proven by a negative-control test.
- With `STRIATUM_ACCRETION_REFUSE=1` and accretion amber, a BUILD `run start`
  refuses with a typed error naming "open a cull run or sign an overdraft"; with
  the flag unset, behavior is byte-identical to today.
- Every cull and un-cull is a hash-chained event; `striatum doctor` stays green
  across a full tombstone→soak→reap cycle and RED on any voided receipt.

## Open Questions

1. **Peer or phase? (the load-bearing design question.)** Is CULL a *thing you do*
   (a fourth peer workflow) or a *toll you pay* (the closing phase of every build:
   `design→build→verify→cull`, where a run cannot `complete` until it has
   tombstoned what it superseded or posted an overdraft)? The review's evidence —
   subtraction only ever happened under external pressure — argues for the toll;
   the standing backlog (41 branches, dead subsystems) still needs the peer/sweep.
   Likely both: sweep for the backlog, phase for the flow.
2. **Soak length.** The window must exceed the longest dormant-caller cycle (a
   quarterly cron, an RFC reopened weeks later, a failure-only recovery path) —
   plausibly **weeks** on a single-operator cadence. A fixed constant, per-kind, or
   adaptive from observed resurrection latency?
3. **Counterforce: amber-only, or ever a hard block?** Blocking `run start` is the
   highest-blast-radius point in the system for a solo operator. Does the hard
   throttle ever earn its keep, or should the daemon stay amber-only and let the
   release clearing-house be the sole hard gate?
4. **Reachability completeness.** Tier-3 must account for reflection, struct tags,
   SQL string literals, codegen templates, and MCP route maps that static analysis
   misses — which is why Tiers 1–2 (exact, DB/contract-backed) lead and Tier 3
   stays advisory until proven complete.
5. **Meter design.** LOC for the MVP, coupling/surface (symbols, methods, tables,
   RFC files) for the real counterforce — what is the exact gameable-resistant
   formula, and does it need a per-kind weight?

## Domain Modeling

New aggregates: **`cullable_entity`** (candidacy ledger, runtime-owned),
**`cull_tombstone`** (quarantine ledger: `target_kind`, `target_ref`,
`death_certificate_event_id`, `staked_receipt_id`, `soak_until`, `state ∈
{quarantined, resurrected, reaped}`), **`accretion_ledger`** (append-only
per-integration surface delta). New workflow shape **`cull_gate`**. New events:
`cull.candidacy_enqueued`, `cull.tombstoned`, `cull.receipt_voided`,
`cull.resurrected`, `cull.reaped`, `accretion.window_recorded` — all on the
existing per-repo hash chain. New doctor classes: `cull_tombstone_receipt_voided`
(RED), `unrefuted_accretion` (amber). New capability: a cull mutation requires the
existing `recovery`/`admin` capability tier; the reap requires the fresh second
attestation. No new principal kind (RFC 0167 attribution reused for overdrafts).

## Wider opportunity (optional follow-up, out of scope)

The same decay/reachability index that finds dead code can surface a
**self-maintaining `docs/index.md` and README-status** (the drifted-docs class as
a continuously-repaired artifact, closing the meta-layer half of the review), and
a **`striatum cull why <ref>`** read that explains, for any artifact, what edges
keep it alive — the inverse of `striatum why`. Both are read-only and ride the
same graph.

## Pointers

- `docs/audits/STRIATUM_DEEP_ARCHITECTURE_REVIEW_CLAUDE_OPUS_4_8_2026-06-24.md` (the accretion finding this RFC answers)
- RFC 0020 (recovery sweep), RFC 0122 (auto-spawn scheduler), RFC 0106 (shape graduation / `falsification_gate`)
- RFC 0134 / 0141 (verifier sealed receipt — the second-key template), RFC 0136 (hash-chained `event_chain_segment` — the un-cull substrate)
- RFC 0047 (verdict supersession columns — the free Tier-1 edge), RFC 0117 (worktree/branch ref safety), RFC 0167 (operator identity — overdraft attribution)
- `go/pkg/recovery/scheduler.go` + `sweep.go`, `go/pkg/mutations/run.go` (`HandleRunStart`), `go/pkg/reads/doctor.go` (+ `doctor_recovery_gate.go` / `doctor_artifact_debris.go` templates), `go/pkg/verifier`, `go/pkg/db/{read,write}_authority_inventory.go`, `contracts/daemon_methods.json`, `scripts/check_release_version.py`
