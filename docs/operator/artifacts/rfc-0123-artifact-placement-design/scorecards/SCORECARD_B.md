---
schema_version: "striatum.finding.v1"
artifact_kind: "finding"
verdict_intent: "accept_with_findings"
severity: "medium"
tags:
  - "rfc-0123"
  - "provenance"
author: "scorekeeper-local-fixture-004"
---

# Scorecard B

Proposal B avoids migration work but makes artifact placement an inferred value
instead of artifact-row provenance. That is the wrong tradeoff for corpus
export, doctor output, and future historical migration.

Finding: persist placement on artifact rows or provide an equally stable
provenance surface.
