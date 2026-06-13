---
schema_version: "striatum.decision.v1"
artifact_kind: "decision"
decision_id: "D190"
run_id: "rfc-0123-artifact-placement-design"
owner: "human"
outcome: "accepted_with_follow_up"
follow_up_required: true
title: "Implement RFC 0123 with additive artifact placement"
created_at: "2026-06-13T17:20:00Z"
author: principal-decider-local-fixture-001
---

# RFC 0123 Design Decision

Adopt explicit artifact placement as an additive artifact contract:
`blob_exhaust`, `git_publication`, and `git_pointer_manifest`.

The first implementation slice must persist resolved placement on artifact rows,
validate optional `expected_artifacts[].placement`, route publish by placement,
project placement in read/export surfaces, and make doctor placement-aware. Old
workflows without placement remain valid and use the current RFC 0072
kind-based default.

Follow-up remains for deeper pointer-manifest body validation and broad
historical migration of unschemaed lane-exhaust kinds.
