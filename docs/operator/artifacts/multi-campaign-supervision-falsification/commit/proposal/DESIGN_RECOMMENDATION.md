---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
author: committer-author-001
title: "MULTI_CAMPAIGN_SUPERVISION Falsification-Cleared Recommendation"
run_id: "run_5cc6429f588bed52d29e950aeefe9b81"
inputs:
  - "artifact:art_31c6e86dd1f8736942f2db3c5bd30617 collaboration_ledger_cycle_3 sha256:9009fbf2afe77091c4180f3902347cb3aef20167be0dbe1cc652f7619d4cd754"
  - "docs/operator/workflows/multi-campaign-supervision-falsification/SEED.md"
  - "docs/operator/artifacts/multi-campaign-supervision-level1/IDEATION_SYNTHESIS.md"
  - "docs/operator/artifacts/multi-campaign-supervision-level1/PROBLEM_BRIEF.md"
  - "docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md"
  - "docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/MULTI_CAMPAIGN_SUPERVISION_WORKFLOW_SELECTION.md"
---

# MULTI_CAMPAIGN_SUPERVISION Design Recommendation

author: committer-author-001

## Recommendation

Proceed with findings to a human product-decision or RFC-drafting gate for the
Level-1 shortlist:

- authority receipt expiration
- fresh-context replay test
- deferral quarantine and scope-drift refusal
- cross-surface contradiction gate

The clearing basis is the cycle-3 collaboration ledger,
`collaboration_ledger_cycle_3` (`art_31c6e86dd1f8736942f2db3c5bd30617`), which
records `verdict: accept_with_findings`. The ledger clears only the narrow next
step: product decision or RFC drafting with the accepted constraints preserved.
It does not approve architecture acceptance, implementation, daemon schema,
route maps, UI, ticket backend selection, build planning, workflow launch
authority, or design-to-build readiness.

## Accepted Findings

The falsification gate accepted the shortlist with two high-severity findings
answered by binding floors, plus a carried no-build boundary.

`AUTHORITY-PROVENANCE-FLOOR-ANSWERED`: provenance and status surfaces can become
ambient permission if a coordinating agent treats receipts, replay results,
quarantine rows, ticket sections, dashboard rows, or contradiction reports as
action authority. The acceptable answer is that those surfaces remain provenance
and stop-pressure only until current daemon-scoped authority or an explicit
human/product decision authorizes the exact transition with daemon-state
reconciliation.

`REPLAY-FRESHNESS-FLOOR-ANSWERED`: replay can become stale selected proof if
mutable surfaces change between replay and advancement or done sealing. The
acceptable answer is source-checkable evidence inventory plus same-boundary
revalidation, or proof that no in-scope source changed since replay.

`NO-BUILD-BOUNDARY-CARRIES`: clearing this gate is not architecture acceptance or
implementation authority. The next artifact remains a product-decision/RFC gate,
not a build gate.

## Binding Constraints

`C1-PROVENANCE-NOT-PERMISSION`: the product decision or RFC must state that
receipts, replay passes, quarantine rows, ticket fields, dashboard rows, and
contradiction reports are provenance and stop-pressure surfaces only. They may
not authorize start, sequence, scaffold, promote, acceptance-state update, or
done seal unless a current daemon-scoped authority object or explicit
human/product decision authorizes that exact transition with daemon-state
reconciliation.

`C2-SAME-BOUNDARY-REPLAY-FRESHNESS`: the product decision or RFC must require
replay packets, replay pass results, advancement proofs, and done proofs to name
source handles and freshness refs for all in-scope mutable surfaces, then
revalidate those surfaces at advancement or done seal time, or prove no in-scope
source changed since replay. New conflicting, red, unreachable, or omitted
in-scope evidence is stop pressure.

These constraints are load-bearing. They should remain explicit and reviewable
in the next product decision or RFC, not softened into advisory notes.

## Recommended Next Gate

The next admissible artifact should decide whether the shortlist becomes a
product decision, an RFC direction, a narrowed follow-up design gate, or a
no-build result. If it proceeds, it should keep the first acceptable direction
proof-only or recommendation-only unless it explicitly defines the current
authority source for exact actions and the same-boundary freshness proof for
advancement or completion claims.

If the next artifact cannot preserve `C1-PROVENANCE-NOT-PERMISSION` and
`C2-SAME-BOUNDARY-REPLAY-FRESHNESS`, the correct outcome is revision or no-build,
not progression by weakening the falsification findings.