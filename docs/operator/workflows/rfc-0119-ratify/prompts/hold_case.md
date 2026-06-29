# Hold The Case For Accepting RFC 0119

> This fixture targets the **striatum** repository. Run it with
> `--repo ~/git/striatum`. The RFC must already be filed at
> `docs/rfcs/0119-warm-tier-memory-boundary.md` (moved in from hippo's
> `docs/rfcs/striatum-0119-warm-tier-memory-boundary.md`) before this run.

Read `docs/rfcs/0119-warm-tier-memory-boundary.md` once and
`docs/reference/spec.md` (the "Corpus Export And Augmentation Boundary"
section). Publish the case for accepting RFC 0119 as the claim falsifiers
will challenge. Create `docs/rfcs/0119/HOLDER_CASE.md` with the exact
lowercase `author:` byline near the top. You do not flip the RFC status.

The case must show, with citations to the RFC and spec:

- That the **three corpus invariants** are preserved (no `import` of any
  external memory consumer including the warm tier; no `memory.*` capability
  in the daemon registry; no state transition that fails when the consumer
  is absent/unreachable/misconfigured).
- That the hot-tier read (`recall.*`) runs at **scaffold time only**, never
  inside a state transition.
- That the **durable-provenance boundary** is preserved: only run exhaust +
  unsynthesized intermediates are eviction-eligible; durable provenance stays
  canonical in git.
- The proposed decision-log entry **D178** in one paragraph.

Do not treat falsifier challenge completion as acceptance; the adjudicator
ledger decides whether the gate clears.
