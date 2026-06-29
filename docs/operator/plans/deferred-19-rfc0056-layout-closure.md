---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_deferred-19-rfc0056-layout-closure"
scope_kind: "rfc"
scope_ref: "docs/rfcs/0056-consumer-repo-directory-structure-opinions.md#phase-c-optional"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Workflow scaffolded and validated; RFC 0056 workflow-file generation and artifact-root .gitignore policy are closed as explicit non-changes for the layout scaffold, with a narrow regression test."
supersedes: null
retrieval_priority: "high"
---

# Deferred 19 RFC 0056 Layout Closure Plan
author: coordinator-codex-gpt-5-001

## Objective

Close deferred item 19 without broadening RFC 0056 beyond its accepted
consumer-repo layout boundary. The question is whether the
`init --with-striatum-layout` scaffold should also create workflow files or
edit artifact-root `.gitignore` policy.

## Scope

Owned paths:

- `docs/operator/plans/deferred-19-rfc0056-layout-closure.md`
- `docs/operator/workflows/deferred-19-rfc0056-layout-closure/`
- `docs/operator/artifacts/deferred-19-rfc0056-layout-closure/`

Narrow source/test guardrail:

- `tests/test_scaffold_ddd_layout.py`

Shared status docs are intentionally not edited in this pass:
`docs/TODO.md`, `docs/ROADMAP.md`, and `docs/operator/BRIEF.md`.

## Workflow

The scaffolded workflow maps the current RFC 0056 implementation boundary,
classifies the optional follow-up, and writes a final closure summary.

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate docs/operator/workflows/deferred-19-rfc0056-layout-closure.json --json
PYTHONPATH=src python3 -m striatum.cli workflow plan docs/operator/workflows/deferred-19-rfc0056-layout-closure.json --json
```

## Closure

The layout scaffold remains directory-only. Workflow files are generated only
through the explicit workflow generator path, and artifact-root commit or
ignore policy stays operator-owned. The only source change in this packet is a
focused scaffold-layout regression test that locks in those non-writes.
