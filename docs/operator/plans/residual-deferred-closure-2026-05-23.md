---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_residual-deferred-closure-2026-05-23"
scope_kind: "initiative"
scope_ref: "docs/operator/BRIEF.md"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "All requested active and deferred backlog items were scaffolded, driven through closure workflows, and classified as implemented, no-action/out-of-core, or requiring a new bounded RFC before implementation."
supersedes: "brief_2026-05-23_next-todos"
retrieval_priority: "high"
---

# Residual Deferred Closure Plan
author: coordinator-codex-gpt-5-001

## Objective

Close or explicitly classify every remaining item from the operator backlog
without weakening Striatum's product boundary. The work is split into
artifact-only Striatum workflow lanes so each item can produce independent
closure evidence before shared status docs are updated.

## Tracks

1. TODO 62 / RFC 0069 PostgreSQL-only daemon-global residuals.
2. TODO 63 / RFC 0070 daemon client/service boundary residuals.
3. TODO 16 generic language hygiene.
4. TODO 2 adapter constraint enforcement.
5. Artifact schemas and redaction future-surface closure.
6. RFC 0040 packet-evidence/provenance residual.
7. Deferred items 14-27 from the current operator list.

## Rules

- If an item has executable product work, implement or scaffold it in a
  bounded follow-up.
- If an item is deliberately out of core, close it as an explicit non-product
  or optional-plugin boundary rather than leaving it as vague deferred work.
- Do not reintroduce repo-local SQLite authority, hosted providers,
  telemetry, transcript capture, or external memory dependencies.
- Do not retire workflow-control CLI verbs until MCP/UI/docs/skill parity is
  complete and tested.
- Keep all workflow outputs under
  `docs/operator/artifacts/residual-deferred-closure-2026-05-23/`.

## Outcome

The closure artifacts are under
`docs/operator/artifacts/residual-deferred-closure-2026-05-23/` and the
per-item workflows live beside this aggregate workflow. Shared status docs
now distinguish active gates from closed optional/non-product items and from
future work that needs a new accepted RFC.

## Validation

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate --allow-same-model-pairing docs/operator/workflows/residual-deferred-closure-2026-05-23.json
```
