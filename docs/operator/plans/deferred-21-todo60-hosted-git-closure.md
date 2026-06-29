---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_deferred-21-todo60-hosted-git-closure"
scope_kind: "phase"
scope_ref: "docs/TODO.md#60"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "TODO 60 hosted Git/PR provider actions are closed for Striatum core as future optional-plugin work; no D127 core source violation was found."
supersedes: null
retrieval_priority: "normal"
---

# Deferred 21 TODO 60 Hosted Git Closure
author: deferred21-todo60-codex-gpt-5-001

## Scope

Close deferred item 21 for TODO 60's hosted Git/PR provider action tail.
The bounded question is whether there is remaining Striatum core work after
D127 and the landed local TODO 60 slice, or whether hosted provider actions
must stay optional-plugin/out-of-core until a later human-principal-confirmed
decision accepts that surface.

## Evidence Base

- `docs/TODO.md` item 60
- `docs/ROADMAP.md` TODO 60 and blocked sections
- `docs/operator/BRIEF.md`
- `docs/DECISION_LOG.md` D127
- `docs/SPEC.md` Git snapshot, commit-apply, and artifact schema sections
- `docs/rfcs/0067-optional-git-pr-integration.md`
- `contracts/daemon_methods.json`
- `src/striatum/cli/daemon_rpc_route.py`
- `go/pkg/reads/git_snapshot.go`
- `go/pkg/mutations/git_commit_apply.go`
- `go/pkg/reads/git_snapshot_test.go`
- `go/pkg/mutations/git_commit_apply_test.go`
- `tests/test_cli_daemon_rpc_route.py`
- `tests/test_artifact_schemas.py`
- `tests/test_mcp_mutation_capabilities.py`
- `go/pkg/mcp/http_test.go`

## Workflow

Runnable scaffold:
`docs/operator/workflows/deferred-21-todo60-hosted-git-closure.json`.

The workflow has three bounded synthesis jobs:

- audit the current core Git/PR boundary against D127;
- classify hosted provider actions as optional-plugin/out-of-core or identify
  any actual D127 violation;
- publish a final closure summary with validation evidence.

Writes are limited to:

- `docs/operator/artifacts/deferred-21-todo60-hosted-git-closure/`

Shared `TODO`, `ROADMAP`, and `BRIEF` files are intentionally not edited from
this closure packet.

## Outcome

No D127 source violation was found. Core Striatum has the read-only local
`git.snapshot` method and the explicit-confirm local `git.commit_apply`
method. It does not expose hosted provider actions, provider SDK imports,
push/fetch behavior, hosted PR mutation methods, or credential loading in the
TODO 60 core slice.

Hosted provider actions are therefore closed for core and classified as future
optional-plugin work. Reopening them requires a later accepted RFC or decision
that defines provider behavior, credential handling, confirmation semantics,
and exact hosted operations.

Final artifact:
`docs/operator/artifacts/deferred-21-todo60-hosted-git-closure/final/SUMMARY.md`.
