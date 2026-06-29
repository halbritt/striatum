---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_todo-2-adapter-constraints-closure"
scope_kind: "todo"
scope_ref: "docs/TODO.md#2-adapter-constraint-enforcement"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Process-adapter constraint enforcement is complete for current scope; enforced network/filesystem isolation requires a future sandbox adapter RFC."
supersedes: null
retrieval_priority: "high"
---

# TODO 2 Adapter Constraints Closure
author: coordinator-codex-gpt-5-001

## Outcome

Close TODO 2 for the current process-adapter scope without overstating
sandbox guarantees. The current product can validate required enforcement,
surface requested and actual enforcement in work packets, enforce
`transcripts=off`, and honestly report `network=forbidden` plus
`repo_scope=local_only` as `advisory_strict`.

The remaining jump from `advisory_strict` to `enforced` for network or
repository/filesystem isolation is not a missing process-adapter patch. It
requires a new accepted sandbox/worktree adapter design that specifies OS
containment, filesystem namespace behavior, network namespacing, portability,
recovery, and operator UX.

## Inputs

- [`AGENTS.md`](../../../AGENTS.md)
- [`docs/SPEC.md`](../../reference/spec.md)
- [`docs/DECISION_LOG.md`](../../decisions/decision-log.md)
- [`docs/UBIQUITOUS_LANGUAGE.md`](../../reference/ubiquitous-language.md)
- [`docs/TODO.md`](../../reference/todo.md)
- [`docs/operator/BRIEF.md`](../BRIEF.md)
- Retired Python source path: `src/striatum/repo_policy.py`
- Retired Python source path: `src/striatum/workflow.py`
- Retired Python source path: `src/striatum/daemon_pg/handlers/context.py`
- Retired Python test path: `tests/test_cli_mvp.py`

## Workflow

Runnable scaffold:
`docs/operator/workflows/todo-2-adapter-constraints-closure.json`.

Validation:

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate docs/operator/workflows/todo-2-adapter-constraints-closure.json --json
```

## Guardrails

- Do not edit shared `docs/TODO.md`, `docs/ROADMAP.md`, or
  `docs/operator/BRIEF.md` from this closure packet.
- Do not claim `network=forbidden` or `repo_scope=local_only` are enforced by
  the current `process` adapter.
- Do not add hosted services, telemetry, transcript capture, provider SDKs,
  or external persistence.
- Do not introduce a sandbox/worktree adapter without a new product decision
  or RFC.
- Keep `.striatum/` as operational scratch only.

## Reported Shared-Doc Follow-Up

`docs/TODO.md` should be updated by the operator after review: TODO 2 can move
from "most done" to "done for current process-adapter scope", with a separate
future RFC item for enforced network/filesystem isolation.
