# Falsify The Acceptance Case

Read the published holder case and `docs/rfcs/0119-warm-tier-memory-boundary.md`.
Write the strongest falsifying challenge you can justify along the posture
named in your job objective, with evidence from `docs/reference/spec.md` and
striatum source. Write your `FALSIFIER.md` at the expected path with the
exact lowercase `author:` byline.

Productive attack lines (every objection must be load-bearing):

- **Corpus invariant break.** Find any path where RFC 0119 would introduce
  an `import` of the warm tier into striatum source, a `memory.*` capability
  in the daemon method registry, or a state transition (`ack`,
  `publish-artifact`, `complete`, `verdict`, recovery, `run prepare`,
  `run start`, `corpus export`) that would fail when the memory consumer is
  absent, unreachable, or misconfigured.
- **State transition depends on memory.** Show that the `RecallMemory` read
  or scaffold injection is reachable from a transition rather than only at
  scaffold time, so a recall failure could wedge a transition.
- **Durable provenance erosion.** Show that the eviction policy or an export
  class would move durable provenance out of git, or that the
  `lane_trajectory` class is a streaming/push surface rather than pull-only.

Include the strongest rebuttal the holder could give to each objection, and
say whether it survives. An objection you can already rebut is not
load-bearing; cut it.
