---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_current-todo-1-5-2026-05-24"
scope_kind: "initiative"
scope_ref: "docs/operator/BRIEF.md"
state: "in_progress"
opened_at: "2026-05-24"
closed_at: null
closure_summary: null
supersedes: "brief_2026-05-23_rfc0075_polish_closure"
retrieval_priority: "high"
---

# Current TODO 1-5 Plan
author: coordinator-codex-gpt-5-001

## Objective

Drive the five remaining actionable workstreams from the current operator
brief to closure or honest blocker evidence:

1. D125 / TODO 56 auto-finalize evidence gate.
2. TODO 49 / TODO 61 legacy SQLite cleanup.
3. TODO 52 service split cleanup.
4. TODO 53 escalation blocker payload hardening.
5. RFC 0074 Phase B workflow generator pack support.

## Runbook

Validate the scaffold:

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate --allow-same-model-pairing docs/operator/workflows/current-todo-1-5-2026-05-24.json
```

Drive implementation in order, committing and pushing after coherent slices.
If D125 cannot honestly satisfy its gate, leave global auto-finalize dry-run
and record the blocker instead of changing default policy.

## Guardrails

- Do not reintroduce repo-local SQLite authority.
- Do not add hosted providers, telemetry, transcript capture, or external
  persistence.
- Keep service splits behavior-preserving.
- Keep RFC 0074 packs as authoring/generator inputs only; generated workflows
  remain ordinary validated `workflow.json` trees.
- Keep the RFC 0050 / RFC 0075 workflow-control cutover closed.
