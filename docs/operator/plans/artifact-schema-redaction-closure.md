---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_artifact-schema-redaction-closure"
scope_kind: "initiative"
scope_ref: "docs/TODO.md#items-6-7"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Artifact schemas and evidence redaction were audited; one session-reason redaction gap was closed; future artifact schemas remain per-RFC additions."
supersedes: null
retrieval_priority: "normal"
---

# Artifact Schema And Redaction Closure Plan
author: artifact-schemas-redaction-codex-001

## Outcome

TODO 6 and TODO 7 are already marked complete. This plan records the bounded
closure pass requested for the remaining/future coverage question: confirm the
current artifact schema registry and SPEC/test drift guard, close any concrete
redaction gap in existing evidence fields, and leave future artifact schemas as
per-RFC work.

## Inputs

- [`docs/TODO.md`](../../reference/todo.md), items 6 and 7.
- [`docs/SPEC.md`](../../reference/spec.md), Artifacts and Artifact Front Matter Schemas.
- Retired Python source path: `src/striatum/artifact_contracts.py`.
- Retired Python source path: `src/striatum/evidence_presentation.py`.
- Retired Python source path: `src/striatum/corpus/redaction.py`.
- Retired Python test path: `tests/test_artifact_schemas.py`.
- Retired Python test path: `tests/test_corpus_redaction.py`.
- [`docs/operator/workflows/artifact-schema-redaction-closure.json`](../workflows/artifact-schema-redaction-closure.json).

## Workstreams

| Workstream | State |
|---|---|
| Audit registered artifact schemas against SPEC/test drift coverage | closed |
| Audit evidence redaction for existing artifact/evidence fields | closed |
| Patch any concrete existing-field coverage gap | closed |
| Publish closure and classify future schema additions | closed |

## Workflow Scaffold

Validate the workflow:

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate docs/operator/workflows/artifact-schema-redaction-closure.json
```

Focused verification:

```bash
PYTHONPATH=src python3 -m pytest tests/test_corpus_redaction.py tests/test_artifact_schemas.py -q
```

Closure evidence is recorded under
`docs/operator/artifacts/artifact-schema-redaction-closure/`.

## Guardrails

- Do not edit shared TODO, ROADMAP, or BRIEF in this pass.
- Do not add speculative artifact schemas without a current RFC or decision.
- Keep redaction fail-closed: new evidence fields require an explicit policy
  entry and synthetic-injection coverage.
- Keep source/test edits narrow to existing artifact/evidence fields.
