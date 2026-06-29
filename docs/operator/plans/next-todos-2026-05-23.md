---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_next-todos-2026-05-23"
scope_kind: "initiative"
scope_ref: "docs/operator/BRIEF.md"
state: "open"
opened_at: "2026-05-23"
closed_at: null
closure_summary: null
supersedes: "plan_ordered-backlog-2026-05-23"
retrieval_priority: "high"
---

# Next TODO Runway
author: coordinator-codex-gpt-5-001

## Scope

Run the next actions named by `docs/operator/BRIEF.md` after the ordered
backlog workflow:

1. TODO 56 / D125 live auto-finalize evidence gate.
2. TODO 67 / RFC 0050 + RFC 0075 parity gaps before CLI retirement.
3. Bounded cleanup/service follow-through:
   TODO 61/49/62/63 cleanup, TODO 52 route splits, TODO 53 escalation
   schema/docs hardening, and RFC 0075 tmux authority guardrails.

## Guardrails

- Do not mark the D125 default-live gate satisfied unless there are three
  live successes across at least two lane shapes with zero contested
  audit-chain events.
- Keep global auto-finalize dry-run by default.
- Do not hide or delete workflow-control CLI verbs in this runway.
- Treat tmux panes, pane text, terminal output, and transcripts as
  inspection-only operational metadata, never workflow authority.
- Keep escalation artifact publication link-only per D130.
- Do not reintroduce repo-local SQLite authority or broad transcript capture.

## Workflow

Runnable scaffold:
`docs/operator/workflows/next-todos-2026-05-23.json`.

Validation:

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate --allow-same-model-pairing docs/operator/workflows/next-todos-2026-05-23.json
```
