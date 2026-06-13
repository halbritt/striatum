---
schema_version: "striatum.findings_ledger.v1"
artifact_kind: "findings_ledger"
summary_count: 3
author: "tradeoff-ledger-local-fixture-001"
---

# RFC 0123 Placement Tradeoff Ledger

## Result

Use Proposal A: additive artifact placement column plus workflow validation and
legacy defaulting.

## Findings

- A: accepted. Best provenance and migration posture.
- B: needs revision. Avoids migration but makes placement too implicit.
- C: rejected. Keeps the kind-only model RFC 0123 is meant to retire.

## Implementation Boundary

Deliver the first implementation as a compatibility-preserving slice:

- `blob_exhaust`, `git_publication`, and `git_pointer_manifest` constants.
- `expected_artifacts[].placement` validation.
- artifact-row persistence with legacy defaulting.
- publish routing by resolved placement.
- read/list/export placement projection.
- doctor git-anchor narrowing for blob exhaust.
- docs and generated workflow updates.
