---
schema_version: "striatum.finding.v1"
artifact_kind: "finding"
verdict_intent: "accept_with_findings"
severity: "medium"
tags:
  - "rfc-0123"
  - "kind-routing"
author: "scorekeeper-local-fixture-005"
---

# Scorecard C

Finding: Proposal C should not be selected because it only extends the current
kind-only routing model. RFC 0123 explicitly needs role-based placement for
cases such as final publication synthesis in git and intermediate synthesis in
blob storage.
