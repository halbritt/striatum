---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
inputs:
  - "PROBLEM_BRIEF.md"
  - "TRADEOFF_LEDGER.md"
author: arbitrator-local-fixture-002
---

# RFC 0123 Placement Arbitration

## Decision

Implement the additive placement-column design from Proposal A.

## Phase Boundary

This branch implements the first useful compatibility slice:

1. Schema and owner append function accept `placement`.
2. Workflow authoring validates placement as a closed optional field.
3. Workflow generator writes explicit placement.
4. Publish stores resolved placement and routes `blob_exhaust` through blob
   storage when a bucket is configured.
5. Read/list/detail/export surfaces include placement.
6. Doctor narrows git-anchor checks to `git_publication` and
   `git_pointer_manifest`, while reporting blob-exhaust metadata problems.
7. Docs describe placement as the RFC 0123 target model and kind routing as
   compatibility defaulting.

Pointer-manifest deep validation can remain a follow-up unless a compact
manifest parser already exists locally; the first slice should still persist
and project the `git_pointer_manifest` placement class.
