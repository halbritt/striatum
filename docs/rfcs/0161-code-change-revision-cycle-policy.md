# RFC 0161: code_change revision-cycle policy (cycle width + in-loop author rebuttal)

Status: proposed (no decision recorded; no code lands with this proposal)
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

What the repository state supports today (origin/main @ `d5d3cd86`):

- Default `code_change` cycle width is exactly 1
  (`maxCycles` -> `defaultAny(spec.Options["max_revision_cycles"], 1)`).
- The compiled cycle is `{from: review, to: draft, on_verdict: needs_revision,
  max_iterations: <max>}` with no intermediate rebuttal node.
- Cycle exhaustion escalates to a human gate; the documented operator exits are
  `override` and `retry-job`.

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

1. **Bump the default `max_revision_cycles` from 1 to N (one-line change in
   `maxCycles`).** Rejected as a direct FIX: it silently re-weights the
   acceptance contract for every `code_change` run with no decision and no
   evidence that N is better than 1 for a miscalibrated reviewer.
2. **Always auto-`retry-job` on cycle exhaustion.** Rejected: removes the human
   gate that the escalation exists to provide; can loop indefinitely against a
   reviewer that will never concede.
3. **Reviewer-posture/prompt calibration only (no shape change).** A viable
   subset that may resolve much of the friction without a contract change; this
   RFC should evaluate it as the cheapest alternative before adding a node.
4. **Operator-side legibility only (issue #506 part b).** Necessary but not
   sufficient: it makes adjudication easier but does not address the structural
   "override or full re-attempt" dichotomy.

## Open questions for review

- Is the answer reviewer-calibration, cycle width, an author-rebuttal turn, or
  a combination — and what is the default for each?
- If an author-rebuttal turn is added: where does it sit in the graph, what
  artifact does it emit, how does the reviewer concede, and how do
  attempt/lease/fresh-session semantics change?
- Should any width change be a new default or an opt-in `spec.options` knob
  with the default unchanged?

## Handoff

This is a non-implementing stub. Route to RFC_REVIEW for a product decision on
the two coupled questions above. The legibility half of issue #506 ships
separately as a direct FIX and does not depend on this RFC. No default, contract,
or migration changes until a decision is recorded in
`docs/decisions/decision-log.md`.
