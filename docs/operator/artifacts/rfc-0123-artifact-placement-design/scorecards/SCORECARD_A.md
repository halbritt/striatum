---
schema_version: "striatum.finding.v1"
artifact_kind: "finding"
verdict_intent: "accept"
severity: "low"
tags:
  - "rfc-0123"
  - "placement"
author: "scorekeeper-local-fixture-001"
---

# Scorecard A

Proposal A is the preferred implementation shape. It preserves compatibility,
keeps placement attached to artifact provenance, and keeps publish/read/doctor
logic simple enough to verify in focused tests.

Residual risk: owner bundle and Go append signature must move together.
