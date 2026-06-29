---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_rfc-0078-workflow-artifact-parity"
scope_kind: "initiative"
scope_ref: "docs/operator/BRIEF.md"
state: "planned"
opened_at: "2026-05-25"
closed_at: null
closure_summary: null
supersedes: "docs/operator/artifacts/rfc-0078-go-only-runtime-and-python-removal/workflow-authoring/HANDOFF.md"
retrieval_priority: "high"
---

# RFC 0078 Workflow Artifact Parity Plan
author: operator-codex-gpt-5-001

## Objective

Close the RFC 0078 workflow-authoring parity gap without broad Python deletion.
This workflow targets the specific remaining chain named by the prior
workflow-authoring handoff:

1. Move artifact/front-matter contracts into a dedicated Go package shared by
   artifact publish, Git mutation artifacts, workflow validation, and tests.
2. Expand Go workflow validation parity for the behavior still owned by Python.
3. Align workflow lint semantics with Python lint and accepted-risk authority.
4. Make workflow generation reuse validation and lint instead of drifting.
5. Decide and implement or explicitly retire `templates render-md` parity.
6. Convert the relevant Python workflow/generator/artifact tests to Go or
   shell-backed checks.
7. Summarize validation evidence, residuals, and the next RFC 0078 deletion
   gate.

## Execution Shape

The workflow is
`docs/operator/workflows/rfc-0078-workflow-artifact-parity.json`.
It declares `max_active_jobs: 12` so the runner can exploit concurrency where
write scopes are disjoint. The artifact-contract package is the first gate.
Validation, lint, generator reuse, and render-md decision work can then proceed
as independent slices. The test migration waits for those implementation
slices so it can consolidate coverage without racing source edits.

## Guardrails

- Keep the daemon/PostgreSQL authority boundary intact.
- Do not reintroduce Python as an active runtime dependency.
- Do not delete Python source or tests just because a replacement is started;
  deletion still needs named parity evidence or a retirement decision.
- Keep workflow artifacts generic and provenance-oriented.
- Do not write `.striatum/`, `.venv/`, caches, transcripts, or private
  diagnostics.
- Update durable artifacts with the exact author lines declared by the
  workflow.

## Validation Gate

At minimum, the workflow closer should report:

```bash
go test ./...
PYTHONPATH=src python3 -m striatum.cli workflow validate --allow-same-model-pairing docs/operator/workflows/rfc-0078-workflow-artifact-parity.json
```

If the Python validator is unavailable because RFC 0078 has advanced, replace
the second command with the Go `striatum workflow validate` equivalent and
state the reason in the final summary.
