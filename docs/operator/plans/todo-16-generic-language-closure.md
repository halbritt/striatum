---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_todo-16-generic-language-closure"
scope_kind: "initiative"
scope_ref: "TODO-16"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Completed current TODO 16 sweep: scaffolded workflow, fixed RFC 0056 generic-language drift, broadened the stale-current-doc guardrail, and published scan/apply/review/final artifacts."
supersedes: null
retrieval_priority: "normal"
---

# TODO 16 Generic Language Closure
author: todo16-codex-gpt-5-001

## Scope

Run a bounded generic-language hygiene pass for TODO 16 without treating it as
a one-time closeout. TODO 16 remains a standing review item; this plan closes
the current sweep only.

Allowed durable homes for this sweep:

- `docs/operator/plans/todo-16-generic-language-closure.md`
- `docs/operator/workflows/todo-16-generic-language-closure/`
- `docs/operator/artifacts/todo-16-generic-language-closure/`

Narrow implementation edits are allowed only when current product docs,
source, or tests contain real generic-language drift. Do not edit
`docs/TODO.md`, `docs/ROADMAP.md`, or `docs/operator/BRIEF.md`; report any
needed updates in the final artifact instead.

## Workflow

Runnable scaffold:
`docs/operator/workflows/todo-16-generic-language-closure.json`.

The workflow shape is scan -> apply -> review -> final synthesis:

1. Search current docs/scripts/source/tests for Engram-specific or
   product-boundary drift outside explicitly historical/reference material.
2. Apply safe current-doc fixes and guardrails.
3. Review the fixes against AGENTS, SPEC, TODO 16, and the previous
   2026-05-23 sweep.
4. Publish a final summary with changed files, validation, and reported
   shared-doc updates.

## Guardrails

- Preserve historical/incubation provenance unless a current doc points
  operators at stale behavior.
- Keep Engram references only when explicitly historical, external, optional,
  or boundary-enforcing.
- Preserve the daemon/PostgreSQL live-state boundary and `.striatum/`
  scratch-only boundary.
- Add tests only for concrete regressions that a future edit could recreate.

## Outcome

Completed on 2026-05-23. Final summary:
`docs/operator/artifacts/todo-16-generic-language-closure/final/SUMMARY.md`.
