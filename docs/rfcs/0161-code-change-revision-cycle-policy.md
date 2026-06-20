# RFC 0161: code_change revision-cycle policy (cycle width + in-loop author rebuttal)

Status: accepted (D253; cheapest-first + bounded concession-only rebuttal alt; default cycles unchanged)
Date: 2026-06-20
author: proposer-claude-opus-4-8

## Summary

The generated `code_change` workflow shape compiles a single
`review -> draft` revision cycle whose default `max_iterations` is **1**
(`go/pkg/workflowgenerate/generate.go` `maxCycles`, defaulting
`spec.options.max_revision_cycles` to 1; consumed by
`go/pkg/workflowgenerate/shapes_basic.go` for the `code_change` branch). When a
thorough reviewer returns `needs_revision`, the author gets exactly one more
full attempt; a second `needs_revision` exhausts the cycle and escalates the run
to `waiting_human`. The only in-product exits from that escalation are an
operator `override` (recorded decision) or another full author attempt via
`retry-job`. There is **no in-loop "author rebuts / reviewer concedes" turn**:
the author cannot answer a reviewer objection without re-spending an entire
attempt, and the reviewer has no structured way to withdraw a finding it now
agrees was addressed.

This RFC asks for a product decision on two coupled questions:

1. **Cycle width.** Should the *default* `code_change` revision-cycle
   `max_iterations` be wider than 1 (and if so, what value / what gate), or
   should width stay 1 and the escalation path be improved instead?
2. **In-loop rebuttal.** Should `code_change` gain a structured
   author-rebuttal / reviewer-concession turn *before* the cycle escalates to
   `waiting_human`, so a verified-complete slice with a reviewer who is wrong on
   a point can resolve in-loop without an operator override?

No code, contract, or default change lands with this proposal. See issue #506
(part a) for the surfacing context.

## Context

- The shape and its cycle policy are an authored workflow-graph contract that
  every `code_change` run, lane, and reviewer depends on. `code_change` is a
  first-class generatable shape (see `docs/reference/ubiquitous-language.md`,
  "workflow shape").
- The friction was surfaced repeatedly across the RFC 0137 A->D implementation
  campaign (decision-log **D247**, which records that the dogfood "surfaced
  runner defects (recorded as issues)"). On meaty phases the reviewer returned
  `needs_revision` two-to-three times even when the slice was independently
  verified complete (`go build` / `go test` green, all acceptance criteria met,
  the cited gap already fixed). One phase required an operator
  `checkpoint resolve override` on a verified-complete slice; others needed
  `retry-job` plus sharpened prompts.
- Adjudicating each `needs_revision` (override vs. another cycle) is harder than
  it should be because the reviewer `finding` body is not operator-readable from
  the CLI. That **legibility** half of issue #506 (part b) is handled
  independently as a direct FIX (a read-only `striatum artifact get-content
  <artifact-id>` verb exposing the existing `artifact.get_content` RPC) and is
  *not* part of this RFC. This RFC is only the cycle-policy half.

## Evidence and claim boundaries

What the repository state supports today (origin/main @ `9e1b6475`;
the cycle-policy source files are unchanged from the prior anchor `d5d3cd86`,
an ancestor of `9e1b6475`):

- Default `code_change` cycle width is exactly 1
  (`maxCycles` -> `defaultAny(spec.Options["max_revision_cycles"], 1)`).
- The compiled cycle is `{from: review, to: draft, on_verdict: needs_revision,
  max_iterations: <max>}` with no intermediate rebuttal node.
- Cycle exhaustion escalates to a human gate; the documented operator exits are
  `checkpoint resolve override` (the human run-gate; D158 distinguishes this from
  the verdict-surface `override-verdict`, which force-terminals a reject) and
  `retry-job`.

What is *not* yet established and is the open product question:

- Whether wider default width improves outcomes or merely lets a
  miscalibrated reviewer burn more attempts (a reviewer that over-rejects at
  width 1 may also over-reject at width 3).
- Whether the right intervention is reviewer-calibration (prompt/posture),
  cycle width, an author-rebuttal turn, or a combination — and how an
  author-rebuttal turn interacts with the verdict gate, lease/attempt
  accounting, the supervisor, and the fresh-session-on-revision behavior.

## Why a direct FIX was rejected

- **It is not a strictly-narrowing change to a cited accepted invariant.**
  Widening the default cycle or inserting a rebuttal turn *loosens* the review
  acceptance contract and changes how reviewed work reaches a run branch. That
  is a product/quality-bar judgment, not a bug with one correct answer.
- **No failing proof exists for "the default is wrong."** The observed
  over-rejection is a calibration/UX signal across a specific dogfood, not a
  determinate defect with a red test that a code change makes green. Fabricating
  a "fix" here would encode a contested policy as if it were settled.
- **An author-rebuttal turn is a new state-machine node**, touching the verdict
  gate, the cycle compiler, attempt/lease accounting, the supervisor, and the
  revision-spawns-fresh-session behavior — a contract change across multiple
  components, not a local edit.

## Hot blast-radius dimensions that forced RFC

- `cross_team_contract` — the `code_change` workflow-shape graph is a contract
  consumed by every run/lane/reviewer; changing default cycle width or adding a
  rebuttal node changes that contract.
- `product_safety_claim` — review-acceptance width is part of the provenance /
  quality guarantee that reviewed work reaching a run branch was actually
  accepted; loosening it without a decision weakens that claim.

## Alternatives / rejected direct patches

These are ordered cheapest-first by blast radius. The decision should evaluate
them in order and adopt the cheapest that resolves the friction: reviewer-posture
/ calibration (no graph change) before any structural change, and — if a node is
added — the asymmetric concession-only rebuttal (alternative 5), which is the only
structural option that does **not** loosen the acceptance contract.

1. **Reviewer-posture/prompt calibration only (no shape change).** The cheapest
   intervention: it touches no workflow-graph contract and no default, and may
   resolve much of the over-rejection friction directly (a thorough reviewer that
   is recalibrated to hold findings only when genuinely unaddressed never exhausts
   the cycle). Evaluate this first; only add a graph change if calibration alone
   is insufficient.
2. **Operator-side legibility only (issue #506 part b).** Already shipped
   separately (the read-only `striatum artifact get-content` verb). Necessary but
   not sufficient: it makes adjudication easier but does not address the structural
   "override or full re-attempt" dichotomy. Listed here for completeness; it is not
   a substitute for a cycle-policy decision.
3. **Bump the default `max_revision_cycles` from 1 to N (one-line change in
   `maxCycles`).** Rejected as a direct FIX: it silently re-weights the
   acceptance contract for every `code_change` run with no decision and no
   evidence that N is better than 1 for a miscalibrated reviewer. Widening the
   default *loosens* the RFC 0034 V1 contract (`max_revision_cycles: 1`); the
   default must stay 1 until a decision is recorded.
4. **Always auto-`retry-job` on cycle exhaustion.** Rejected: removes the human
   gate that the escalation exists to provide; can loop indefinitely against a
   reviewer that will never concede.
5. **Asymmetric, concession-only in-loop author-rebuttal turn (RECOMMENDED
   structural shape if any node is added).** This is the structural option that
   keeps the default cycle width at **1** and is **contract-neutral**: the author
   may post exactly **one** rebuttal per `needs_revision`, and that rebuttal costs
   a revision iteration **only when the reviewer declines to concede**. The
   asymmetry is load-bearing:
   - **Reviewer concedes** (it agrees the finding was already addressed): the
     concession resolves the cycle in-loop and clears it as accepted — a no-op on
     the iteration budget. No extra full attempt is spent; the human gate is not
     touched; default width stays 1 for the common case.
   - **Reviewer holds** (it does not concede): the rebuttal falls straight through
     to the existing bounded cycle and, on exhaustion, the existing
     `waiting_human` gate — exactly as today. The author cannot re-enter review
     for free; a reviewer that never concedes can never loop the author↔reviewer
     pair below the human gate. This is precisely the failure mode of alternative
     4 (auto-retry) and a naive "rebuttal re-enters review without consuming an
     iteration" node, and the asymmetry forecloses it.

   Because it only ever *shortens* the path (concession) or leaves it unchanged
   (hold), it does not widen the default and does not loosen the
   `product_safety_claim` (work reaching a run branch was still actually accepted).
   It is therefore separable from the cycle-width question along the axis "does
   this change the default width / loosen the contract?" — and the answer for this
   node is *no*.

   **Bounding requirements (must be specified before any code or default lands).**
   A rebuttal node sits directly on top of the in-force cycle router and the
   attempt/lease/fresh-session machinery; without explicit bounds a naive node is
   an unbounded extra cycle that *does* weaken provenance. The decision must spell
   out:
   - **D158 cycle-router compatibility.** D158 (accepted) refuses an in-cycle
     `reject` inside a declared `on_verdict: needs_revision` cycle and steers it to
     `needs_revision`/`override-verdict`; the router consults only declared cycles
     (D155 added the cycle-aware routing). The rebuttal node and its concession
     verdict must be declared so the router recognizes them; a concession must not
     be expressible as a path around the D158 refusal (i.e. it must not reopen the
     in-cycle `reject`→`needs_revision` wedge class of #127/#132/#140).
   - **Iteration / attempt accounting.** Exactly one rebuttal is permitted per
     `needs_revision`; a held rebuttal consumes one revision iteration of the
     existing bounded budget (it does not mint a fresh budget). Concession does not
     consume an iteration. The `waiting_human` escalation on budget exhaustion is
     unchanged.
   - **Lease and fresh-session-on-revision semantics.** Today a revision spawns a
     fresh-session next-ordinal lane. The decision must state whether a rebuttal is
     authored within the current session/lease or spawns its own lane, how the
     reviewer-concession turn is leased, and how a stale lease during a rebuttal is
     recovered — so the node cannot strand a lane or create an un-leased turn.

   Adopting this shape lets the decision improve the in-loop experience without
   changing the default width or the acceptance contract; widening width (and the
   contract loosening it entails) remains a separate, gated question.

## Open questions for review

- Evaluated cheapest-first: does reviewer-calibration (alternative 1) alone
  resolve the over-rejection, or is a structural change needed at all?
- If a structural node is added, the recommended shape is the asymmetric
  concession-only rebuttal (alternative 5): where exactly does it sit in the
  graph, what artifact does the rebuttal emit, how does the reviewer concede,
  and how are the bounding requirements above (D158 router compatibility,
  iteration/attempt accounting, lease/fresh-session semantics) made concrete?
- Should any width change be a new default or an opt-in `spec.options` knob with
  the default unchanged? The default `max_revision_cycles` stays **1** (the
  RFC 0034 V1 contract) unless and until a decision explicitly records a wider
  default — note the recommended rebuttal node does not require widening it.

## Handoff

This is a non-implementing stub. Route to RFC_REVIEW for a product decision on
the two coupled questions above. The legibility half of issue #506 ships
separately as a direct FIX and does not depend on this RFC. No default, contract,
or migration changes until a decision is recorded in
`docs/decisions/decision-log.md`; the default `max_revision_cycles` stays 1 and
`wontfix` (keep width 1, no node) remains a legitimate outcome.

## Revision history

- 2026-06-20 (post-RFC_REVIEW, run `2026-06-20_9e1b6475`): addressed the
  ACCEPT_WITH_FINDINGS report. Added the asymmetric, concession-only
  author-rebuttal turn as alternative 5 (the contract-preserving structural
  option that keeps default width at 1) with explicit bounding against
  attempt/lease/fresh-session accounting and the D158 cycle router; reordered the
  alternatives cheapest-first (calibration before any graph change); named
  `checkpoint resolve override` precisely vs. `override-verdict`; refreshed the
  evidence anchor to `9e1b6475`. The `max_revision_cycles` default is unchanged
  (stays 1, the RFC 0034 V1 contract). Ratification still pending.
