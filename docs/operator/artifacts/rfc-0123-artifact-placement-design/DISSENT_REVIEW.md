---
schema_version: "striatum.finding.v1"
artifact_kind: "finding"
verdict_intent: "accept_with_findings"
severity: "low"
tags:
  - "rfc-0123"
  - "dissent"
author: dissent-reviewer-local-fixture-001
---

# Dissent Review

The selected design is acceptable, with one finding: the implementation must
avoid turning null placement into a second hidden semantic. A single resolver
should own legacy defaulting and both publish and read surfaces should use it.

The owner bundle/function signature update is mandatory because the current
deployment may run with direct artifact INSERT revoked.
