---
schema_version: striatum.decision.v1
decision_id: "rfc-0123-apply-recovery"
run_id: "run_2a278c3161d484fea10260dfd60a291e"
artifact_kind: decision
owner: human
outcome: accepted
follow_up_required: false
title: "Recover RFC 0123 apply after stopped session"
created_at: "2026-06-13T17:42:55Z"
---

# Recover RFC 0123 apply after stopped session

Decision ID: `rfc-0123-apply-recovery`
Run ID: `run_2a278c3161d484fea10260dfd60a291e`
Outcome: `accepted`

## Rationale

The stopped apply session held a claimed packet after autonomous recovery exhaustion. The apply job was requeued with an operator override after local implementation and verification completed, and a fresh session will publish the final summary.
