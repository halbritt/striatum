# RFC 0154: Debounce multi-reviewer final-review fan-in before revision-cycle routing — should a single `needs_revision` route to author while sibling final reviewers are still in flight?

Status: accepted (D250; option B -- opt-in, default-preserving final-review debounce)
Date: 2026-06-20
author: triager-claude-opus-4-8-001

## Affected issue

- GitHub [#476](https://github.com/halbritt/striatum/issues/476) — auto-driver
  routes to the author lane on the first final-review `needs_revision` verdict
  without waiting for the other concurrent final verdicts (quorum). Filed against
  striatum 2.34.1.
- Sibling [#505](https://github.com/halbritt/striatum/issues/505) — the detached
  auto-driver (`run drive`) is not re-armed after a checkpoint resolve. Distinct
  defect, but it shares the run-drive / final-review-fan-in surface; this stub is a
  policy proposal and lands **no** code, so there is no file overlap with the #505
  fix.

## The unresolved decision

A multi-reviewer final-review panel fans out N concurrent reviewers (e.g. the
prompt-committee topology: 3 parallel final reviewers → author, with a bounded
revision cycle). Today the daemon routes the author revision cycle on the **first**
seat's `needs_revision` verdict, regardless of whether the other N-1 final-review
seats have reported. Should the engine instead **debounce** the fan-in — wait for
all N seats (or a configurable quorum) to report before routing one consolidated
revision pass — and if so, under what exact policy?

Note on blast radius: the N-final-reviewers-each-cycling-to-one-author topology is
**not** a default shipped generator shape. The basic/panel generator
(`shapes_panel.go`) emits a single `from` cycle; only `shapes_collaboration.go` and
the per-track composition in `shapes_multiphase.go` can produce multiple cycles, and
the #476 repro was a **hand-authored / multiphase-composed** prompt-committee
workflow. So the friction affects hand-authored and multiphase-composed
multi-reviewer workflows, not every default shape — which bounds the "changes every
existing multi-reviewer workflow" cost in the FIX-rejection rationale and
strengthens the "default unchanged" safety of an opt-in policy.

This is a product decision because there is no single correct answer and several
sub-policies must be settled together:

1. **Cohort identity** — which sibling jobs form the "final-review cohort" a
   `needs_revision` must wait on? The natural denominator is the frozen set of
   gating review seats that feed the same downstream gate (the existing
   panel-quorum denominator), but the revision-cycle declaration (`cycles[]`) is
   keyed per review-job `from`, not per cohort, so the two are not currently
   joined.
2. **Wait shape** — all-N vs configurable quorum vs dissent-threshold early route.
   "Wait for every seat" delays a clearly-doomed artifact; "route on first dissent"
   (today) burns bounded cycles on a moving target.
3. **Gating vs advisory** — RFC 0132 / D214 already split reviewers into `gating`
   and `advisory` panel roles. Should advisory dissent ever trigger or delay a
   revision route? (Today advisory guards open a downstream blocker; they do not
   route revisions.)
4. **Bounded-cycle composition** — a consolidated debounced route should consume
   **one** `max_iterations` slot, not N. The current per-verdict routing risks two
   stragglers each consuming a slot (max 2 by default), so a second straggler
   `needs_revision` can prematurely exhaust the budget and escalate.
5. **Stale-verdict supersession** — in-flight reviewers that report *after* the
   consolidated revision starts are reviewing a soon-to-be-stale artifact. RFC 0126
   / D194 already renders prior-generation verdicts non-current by a build-generation
   mismatch; the debounce policy must define how a late straggler verdict composes
   with the new build generation (cancel the in-flight seat, let it land and be
   superseded, or hold the route until it lands).

## Current evidence and claim boundaries

Grounded reads at `origin/main` (HEAD `d5d3cd86`):

- `go/pkg/mutations/review.go` — `applyVerdict`, `case "needs_revision"` (≈ lines
  757-778): the moment a single seat records `needs_revision`, the engine loads the
  review job's declared `cycles[]`, and if one matches it calls
  `routeRevisionCycle` **immediately**. There is no read of sibling-seat state, no
  cohort/quorum check, and no debounce window on this path.
- `go/pkg/mutations/revision_routing.go` — `routeRevisionCycle` completes the
  review job, emits `revision.cycle_routed`, and re-opens the cycle target
  (`reopenJobForAttempt`) the same transaction. Its only gate is the cycle's own
  `max_iterations` budget (`countRevisionRoutings`); it does not consider sibling
  final reviewers.
- `go/pkg/mutations/barrier_quorum.go` — the **accept** path already debounces, but
  via a *different and independent* code path from revision routing.
  `panelQuorumSatisfied` (and its `staleSealTrap` helper, ≈ lines 234-262) live
  entirely inside `dependenciesSatisfied` (`mutations.go` ≈ lines 1021-1042) — the
  **downstream-gate enqueue-readiness** predicate that asks "may the panel's
  *consumer* run yet?" over the frozen declared gating-seat denominator (RFC
  0132/0135, D214/D216). **Revision routing never consults this barrier at all.**
  `review.go`'s `needs_revision` case and `routeRevisionCycle` (`revision_routing.go`)
  are a separate synchronous mutation that never calls `panelQuorumSatisfied`. The
  `staleSealTrap` comment's "revision-routing … the barrier must fall through"
  language is **not** a carve-out of the dissent route: it names the narrow seat
  state `AcceptingAttempt == LiveAttempt` — a seat whose *accept is already at the
  live seal* (i.e. a previously-revised seat now carrying a fresh clearing accept)
  — which the readiness predicate must not regress. It is about a settled
  accept-at-live-seal in the downstream-gate readiness check, **not** about the
  `needs_revision` path that triggers a route. So the real reason revision routing
  has no quorum/cohort check is simply that **the two paths are orthogonal**: the
  accept-side debounce is in the downstream-gate predicate, the dissent-side route
  is a separate mutation with no cohort awareness. The asymmetry is an *absence of
  any cohort check on the route path*, not a deliberate dissent-short-circuit
  encoded in the barrier.
- `go/pkg/workflowauthoring/workflow.go` (≈ lines 895-899) — the `cycles[]` schema
  today carries only `from`, `to`, `on_verdict` (must be `needs_revision`),
  `max_iterations`, `allow_same_lane`. There is **no** debounce / quorum / cohort
  field to express "wait for all N before routing".

Live characterization: `go test ./pkg/mutations -run
TestNeedsRevisionRoutesToMatchingCycle -count=1` PASSES against a live PostgreSQL
(`STRIATUM_PG_TEST_URL` set) — a single `needs_revision` with a matching cycle
routes to the target. This is a **single-seat** fixture: it confirms immediate
routing on one verdict but does not itself exercise the multi-reviewer "seat A
`needs_revision` while seat B is still `claimed`" straggler scenario. The
multi-reviewer straggler behavior follows from the *absence* of any sibling /
cohort read on the route path (`review.go`'s `needs_revision` case and
`routeRevisionCycle` never read sibling-seat state — verified by inspection), and
would be pinned by a new multi-reviewer pgtest shipped with whichever option is
ratified.

Claim boundaries:

- This stub does **not** claim the present behavior is a bug against any ratified
  invariant. The operator playbook quoted in #476 ("when one final reviewer returns
  needs_revision while others are still reviewing, do NOT route to author yet") is a
  human best-practice heuristic, not a documented or test-enforced invariant. There
  is therefore no PRE-FIX failing proof tied to an accepted invariant — only a
  characterization test confirming the current intended behavior.
- The repro run in #476 (`run_a90127785d9baaf1478551f050551358`) was not replayable
  in this triage; the source path is dispositive on its own.

## Why a direct FIX was rejected

A narrow source patch would have to **invent** the policy enumerated above —
cohort identity, wait shape, gating/advisory handling, bounded-cycle composition,
and stale-verdict supersession — and encode it in a new `cycles[]` schema field
that every workflow author and the workflow generator must understand. That is a
contract expansion, not a behavior narrowing to a cited invariant, so the FIX
escape ("strictly narrows to an accepted invariant, expands no contract") does not
apply. Patching `routeRevisionCycle` to silently wait for the whole panel would
also change the observable behavior of every existing multi-reviewer workflow with
a revision cycle, including ones that deliberately want an early route on the first
blocking dissent.

## Hot blast-radius dimensions that forced RFC

- **wire_format** — encoding the debounce/quorum policy requires a new workflow
  `cycles[]` schema field (and possibly a cohort/denominator reference), changing
  the workflow JSON contract that authors, the generator, and the lint depend on.
- **cross_team_contract** — revision-routing timing is a behavior that the dissent
  ledger (RFC 0135 P4/#339), the panel-quorum barrier (RFC 0132/D214), and the
  bounded-cycle budget all coordinate on; changing *when* routing fires changes
  observable run behavior across all multi-reviewer workflows.

## Alternatives / rejected direct patches

- **A. Status quo + legibility only** — keep first-dissent routing; sharpen
  operator guidance and surface "N-1 final reviewers still in flight" on the run
  summary so the human can choose to wait. Cheapest; leaves the load-bearing
  heuristic on the operator (the exact friction #476 names).
- **B (suggested by #476; CHOSEN — see Resolution). All-N or quorum debounce** —
  gate `routeRevisionCycle` on the same frozen gating-seat denominator the accept
  path already uses (`panelQuorumSatisfied`'s declaration), so a `needs_revision`
  from one seat is buffered until the cohort reaches quorum, then a single
  consolidated revision packet is issued consuming one `max_iterations` slot.
  Reuses the existing denominator and dissent-ledger primitives; needs a new opt-in
  `cycles[]` field (default = today's first-dissent behavior so existing workflows
  are unchanged) and a defined late-straggler supersession rule.
- **C. Dissent-threshold early route** — debounce, but route early once a declared
  blocking-dissent threshold (e.g. ≥ k gating dissents) is reached, to avoid waiting
  on a slow seat when the artifact is already clearly doomed. B plus an escape
  hatch; more policy surface.
- **Rejected direct patch** — unconditionally make `routeRevisionCycle` wait for the
  whole panel with no opt-in. Rejected: it silently changes every existing
  revision-cycle workflow and removes the legitimate "route on first blocking
  dissent" behavior some workflows deliberately want.
- **Rejected: driver-side debounce** — have the auto-driver (`run drive`) withhold
  the route until N verdicts land, rather than debouncing in the daemon mutation.
  Rejected: revision routing is a **synchronous daemon mutation** fired inside
  `applyVerdict`, not a driver decision; a driver-side heuristic would be advisory,
  out-of-band of the durable state transition, and would not hold for non-driven or
  externally-driven runs. The debounce must live in the daemon mutation to be an
  enforced invariant.

## Resolution (revised 2026-06-20 — RFC_REVIEW findings addressed)

RFC_REVIEW returned **ACCEPT_WITH_FINDINGS** (confidence high; 0 blockers, 2
serious, 3 minor). The decision-relevant conclusion — that revision-cycle routing
fires on the first `needs_revision` with no sibling/cohort/quorum check, and that
settling this is a product decision (not a narrow FIX) — was verified true. Two
serious findings are addressed by this revision: the `staleSealTrap` carve-out was
**mischaracterized** (corrected in *Current evidence and claim boundaries* above —
the route path is independent of the barrier, not deliberately carved out of it),
and the stub was **not yet a ratifiable design** because no option was chosen. This
section closes the second gap by selecting an option and pinning all five
sub-policies. The three minor findings (single-seat characterization framing,
hand-authored topology, driver-side alternative) are addressed inline above.

**Chosen option: B — opt-in, default-preserving all-N / quorum debounce.** B is the
strongest candidate because it reuses primitives already in force (the frozen
gating-seat denominator from RFC 0132/0135 D214/D216 and the `review_generation`
supersession from RFC 0126/D194) and, being opt-in with a default equal to today's
first-dissent behavior, changes **no** existing workflow silently. C (B plus a
dissent-threshold early route) is deferred: it adds policy surface (the early-route
threshold) without a demonstrated need, and B can be extended to C later behind the
same opt-in field. A (legibility-only) is retained as an independently-shippable
operability win (surface "N-1 final reviewers still in flight" on the run summary)
that composes with B; it is not a substitute for B.

### Pinned sub-policies (the five settled together)

1. **Cohort identity** — the cohort a `needs_revision` waits on is the **frozen
   declared gating review-seat denominator that feeds the same downstream gate** —
   the exact denominator `panelQuorumSatisfied` / `resolveGovernedSeats` already
   freeze for the accept path. The per-`from` `cycles[]` declaration references this
   cohort by the downstream gate / barrier it is governed by, so the route path and
   the accept path share one denominator (closing the "two are not currently
   joined" gap). Advisory seats are **not** in the cohort.
2. **Wait shape** — **configurable quorum over the gating cohort**, defaulting to
   **all-N** when the new field is enabled. Expressed as the new `cycles[]` field
   (below); when the field is absent or set to the default sentinel, behavior is
   **today's first-dissent immediate route** (no change).
3. **Gating vs advisory** — only **gating** dissent participates in the debounce
   cohort and can trigger/delay a route. **Advisory** dissent never routes a
   revision and never delays one (it continues to open a downstream advisory
   blocker exactly as today, per RFC 0132/D214). This preserves the existing
   gating/advisory split.
4. **Bounded-cycle composition** — a debounced route consumes **exactly one**
   `max_iterations` slot per consolidated revision pass, regardless of how many
   gating seats dissented. Buffered straggler dissents collapse into the single
   route; they do not each consume a slot (closing the "second straggler exhausts
   the budget" friction). `countRevisionRoutings` counts consolidated routes, not
   per-verdict dissents.
5. **Late-straggler supersession** — a gating seat that reports `needs_revision`
   *after* the consolidated route has fired is **let to land and is then superseded
   by build generation**: the route bumps `review_generation` / the build seal (RFC
   0126/D194, recast as the seal in RFC 0135/D216), so the late verdict is recorded
   against the now-stale generation and is non-current — it does **not** trigger a
   second route on the superseded artifact and does **not** consume another slot.
   The in-flight seat is not force-cancelled (its verdict is durable provenance);
   it is rendered non-current by the generation mismatch the next cycle already
   carries.

### New `cycles[]` field (workflow JSON contract)

A single **opt-in** field on the per-`from` cycle declaration controls the wait
shape, e.g. `debounce_cohort` (exact name to be finalized in the impl issue):

- **absent / default sentinel** → today's behavior: route on the first matching
  `needs_revision` (no debounce). Every existing workflow is unchanged.
- **`all`** → wait for every gating seat in the cohort to report before routing one
  consolidated revision pass.
- **a quorum value** (e.g. an integer or fraction of the gating cohort) → wait until
  that many gating dissents-or-completions are reached, then route once.

The field is validated in `workflowauthoring/workflow.go` `cycles[]` validation, is
understood by the generator and lint, and requires **no DDL/migration** (it is a
`workflow_json` field, per the D212 workflow-json-field pattern). The cohort
denominator is **not** re-declared on the cycle; it is **referenced** from the
existing gating-seat denominator the downstream gate already freezes, so authors do
not restate the cohort.

### Status of this RFC and next steps

This revision selects the design and pins the sub-policies, but **ratification is a
later step**: the RFC remains `proposed (revised; ratification pending)` and #476
stays **open** until a maintainer ratifies the decision and allocates a decision
number (next free at the time of this revision was **D249** — confirm the next-free
number at ratification time). On ratification, file implementation issues for: (1)
the new opt-in `cycles[]` `debounce_cohort` field (generator + lint +
`workflowauthoring` validation), (2) the cohort-denominator reference wiring the
route path to the frozen gating-seat denominator, (3) the late-straggler
supersession rule composing with `review_generation` / the build seal, and (4) a
multi-reviewer straggler pgtest that pins the debounced route (and a negative-control
test confirming the default sentinel preserves first-dissent routing). No code,
schema field, or migration is landed by this RFC.
