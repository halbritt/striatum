---
schema_version: "striatum.finding.v1"
artifact_kind: "finding"
verdict_intent: "accept_with_findings"
severity: "low"
tags:
  - "rfc-0123"
  - "implementation-review"
author: reviewer-local-fixture-001
---

# Implementation Draft Review

The draft is acceptable for the first compatible RFC 0123 slice.

## Findings

- Use one shared resolver for legacy defaulting. Null or omitted placement
  should not become a separate semantic from the compatibility default.
- Keep the owner/admin append function in lockstep with direct artifact writes.
  Environments with direct table INSERT revoked must still be able to publish
  artifacts with placement.
- Keep doctor posture split by placement. Git anchor checks belong to
  git-retained artifacts; blob-exhaust rows should report missing blob metadata
  without requiring a repository file anchor.

## Verdict

`accept_with_findings`. Apply the findings during implementation and include
focused tests for the resolver, workflow validation, publish routing, read
projection, and doctor behavior.
