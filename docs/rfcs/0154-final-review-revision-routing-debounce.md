# RFC 0154: Debounce multi-reviewer final-review fan-in before revision-cycle routing — should a single `needs_revision` route to author while sibling final reviewers are still in flight?

Status: proposed (triage stub — needs RFC_REVIEW)
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
prompt-committee shape: 3 parallel final reviewers → author, with a bounded
revision cycle). Today the daemon routes the author revision cycle on the **first**
seat's `needs_revision` verdict, regardless of whether the other N-1 final-review
seats have reported. Should the engine instead **debounce** the fan-in — wait for
all N seats (or a configurable quorum) to report before routing one consolidated
revision pass — and if so, under what exact policy?

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
- `go/pkg/mutations/barrier_quorum.go` — the **accept** path already debounces:
  `panelQuorumSatisfied` gates the *downstream* consumer over the frozen declared
  gating-seat denominator (RFC 0132/0135, D214/D216). The `needs_revision` /
  revision-routing path is explicitly carved **out** of this barrier: the
  `staleSealTrap` doc (≈ lines 234-262) names "revision-routing … scenarios where
  the legacy gate UNBLOCKS and the barrier must fall through" — i.e. the asymmetry
  (accept waits for the whole panel, dissent short-circuits) is the present
  intended behavior, not an oversight in the predicate.
- `go/pkg/workflowauthoring/workflow.go` (≈ lines 895-899) — the `cycles[]` schema
  today carries only `from`, `to`, `on_verdict` (must be `needs_revision`),
  `max_iterations`, `allow_same_lane`. There is **no** debounce / quorum / cohort
  field to express "wait for all N before routing".

Live characterization (the immediate-route behavior is the live behavior, not a
test gap): `go test ./pkg/mutations -run
TestNeedsRevisionRoutesToMatchingCycle -count=1` PASSES against a live PostgreSQL
(`STRIATUM_PG_TEST_URL` set) — a single `needs_revision` with a matching cycle
routes to the target with no sibling check.

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
- **B (suggested by #476). All-N or quorum debounce** — gate `routeRevisionCycle`
  on the same frozen gating-seat denominator the accept path already uses
  (`panelQuorumSatisfied`'s declaration), so a `needs_revision` from one seat is
  buffered until the cohort reaches quorum, then a single consolidated revision
  packet is issued consuming one `max_iterations` slot. Reuses the existing
  denominator and dissent-ledger primitives; needs a new opt-in `cycles[]` field
  (default = today's behavior so existing workflows are unchanged) and a defined
  late-straggler supersession rule.
- **C. Dissent-threshold early route** — debounce, but route early once a declared
  blocking-dissent threshold (e.g. ≥ k gating dissents) is reached, to avoid waiting
  on a slow seat when the artifact is already clearly doomed. B plus an escape
  hatch; more policy surface.
- **Rejected direct patch** — unconditionally make `routeRevisionCycle` wait for the
  whole panel with no opt-in. Rejected: it silently changes every existing
  revision-cycle workflow and removes the legitimate "route on first blocking
  dissent" behavior the quorum barrier deliberately preserves.

## Handoff to RFC_REVIEW

This is a triage stub, not an implementation. The reviewer should:

1. Choose between alternatives A / B / C (or a hybrid) and ratify the five
   sub-policies (cohort identity, wait shape, gating/advisory, bounded-cycle
   composition, late-straggler supersession).
2. If B or C: specify the new opt-in `cycles[]` field name + default (must default
   to today's first-dissent behavior so no existing workflow changes silently),
   and how the cohort denominator is referenced from the per-`from` cycle
   declaration.
3. Allocate a decision number (next free is **D249**) and the implementation
   issues. No code, schema field, or migration is proposed here.
