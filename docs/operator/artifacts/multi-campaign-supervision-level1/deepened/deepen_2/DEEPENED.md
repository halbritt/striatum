---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
inputs:
  - "CONVERGENCE.md"
  - "PROBLEM_BRIEF.md"
author: deepener-reviewer-1-001
---

# Fresh-Context Replay Test

## Sketch

The fresh-context replay test is a stage-boundary gate that proves a blank lane
can reconstruct the accepted campaign arc without inheriting chat history. Before
a campaign advances from planning to design, design to build, build to verify, or
slice discovery to promotion, the daemon assembles a bounded replay packet from
durable sources: accepted arc plan, current authority receipt, stop conditions,
deferrals, discovered-slice custody, relevant run/job/artifact handles, Git/doc
evidence, and the intended next action. A fresh session receives only that packet
and must restate the admissible next state, list missing or contradictory
evidence, and identify any human-confirmed boundary that would be crossed. The
campaign may advance only when the replay result agrees with daemon state and
does not surface unresolved stop pressure; otherwise advancement pauses and the
gap becomes a visible repair item rather than a silent handoff failure. This is
not a second workflow state machine: it is an acceptance proof over daemon state
and durable provenance before the existing workflow machinery is allowed to
continue. It composes naturally with expiring authority receipts, because every
receipt can require a fresh replay proof before renewal or stage transfer.

## Load-Bearing Risk

The replay packet can become too large or too curated, letting it either overflow
fresh context or hide the very contradictions it is supposed to expose. The
design has to define a minimal, inspectable packet contract and require the
fresh lane to check source handles rather than trusting a polished summary.

## First Concrete Step

Specify the replay packet fields and pass/fail receipt for one boundary first:
candidate synthesis to falsification gate, including the exact daemon handles,
artifact paths, deferral entries, stop conditions, and human decisions a fresh
lane must be able to verify.

## Child Ideas

- Replay receipts as renewable authority preconditions: an authority receipt
  cannot extend into the next stage unless the latest replay proof passed.
- Negative replay mode: the fresh lane is rewarded for finding one contradiction
  or missing handle, making failure a useful outcome instead of wasted overhead.
- Context budget linting: replay packet generation fails if required context
  exceeds a declared line or token budget, forcing sharper durable artifacts.
- Deferral custody replay: every deferred or discovered slice must survive the
  blank-lane reconstruction with reason, owner, wake-up condition, and promotion
  authority intact.
- Dashboard replay badge: portfolio status shows the last replay result and age,
  so a green campaign is green because a fresh context could actually continue.
