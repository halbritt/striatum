---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_deferred-25-cross-repo-live-closure"
scope_kind: "phase"
scope_ref: "deferred-25-full-live-cross-repo-scheduling"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Workflow scaffolded and validated; landed RFC 0032/0035/Go slices cover schema, PostgreSQL metadata, MCP capability gating, harness coverage, and cross-repo read/cancel, while full live cross-repo scheduling needs a new bounded RFC."
supersedes: "docs/operator/workflows/residual-deferred-closure-2026-05-23/prompts/deferred_25_cross_repo_live.md"
retrieval_priority: "normal"
---

# Deferred 25 Cross-Repo Live Closure
author: deferred25-cross-repo-codex-gpt-5-001

## Objective

Classify deferred item 25: full live cross-repo scheduling beyond the landed
RFC 0032 schema/capability slice, RFC 0035 multi-repo harness, and Go daemon
read/cancel slices.

## Scope

Owned paths for this closure:

- `docs/operator/plans/deferred-25-cross-repo-live-closure.md`
- `docs/operator/workflows/deferred-25-cross-repo-live-closure/`
- `docs/operator/artifacts/deferred-25-cross-repo-live-closure/`

Narrow status-only documentation fixes are allowed where current docs
overclaim implementation status. Shared TODO, roadmap, and operator brief
files stay out of scope.

## Outcome

Deferred item 25 is closed as classification work, not implementation work.
The current implementation is not stale for the landed slices: cross-repo
workflow validation, daemon PostgreSQL cross-repo tables, MCP capability
gating, the multi-repo harness, and production cross-repo read/cancel routes
exist and have focused tests.

Full live cross-repo scheduling is still not a production surface. It needs a
new bounded RFC that decides whether to extend `run.prepare`/`run.start` for
cross-repo workflows or add explicit `cross_repo.prepare`/`cross_repo.start`
methods, then specifies scheduler fan-out, cross-repo dependency unblocking,
cycle accounting, packet/session scope, recovery, and operator UI/MCP/CLI
parity.

## Workflow

Runnable scaffold:
`docs/operator/workflows/deferred-25-cross-repo-live-closure.json`.

Final classification:
`docs/operator/artifacts/deferred-25-cross-repo-live-closure/RESULT.md`.

## Validation

Run and record:

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate docs/operator/workflows/deferred-25-cross-repo-live-closure.json --json
PYTHONPATH=src python3 -m striatum.cli workflow plan docs/operator/workflows/deferred-25-cross-repo-live-closure.json --json
PYTHONPATH=src python3 -m striatum.cli workflow lint docs/operator/workflows/deferred-25-cross-repo-live-closure.json --json
PYTHONPATH=src .venv/bin/python -m pytest -q tests/test_workflow_cross_repo.py tests/test_cross_repo_lifecycle.py tests/test_mcp_mutation_capabilities.py::test_mcp_tools_call_exact_workflow_control_methods_route_to_daemon_rpc
(cd go && go test ./pkg/crossrepo)
make test-multi-repo
PYTHONPATH=src python3 - <<'PY'
from pathlib import Path
from striatum.artifact_contracts import validate_artifact_front_matter

for kind, raw_path in [
    ("work_plan", "docs/operator/plans/deferred-25-cross-repo-live-closure.md"),
    ("synthesis", "docs/operator/artifacts/deferred-25-cross-repo-live-closure/RESULT.md"),
]:
    path = Path(raw_path)
    validate_artifact_front_matter(kind=kind, path=path, payload=path.read_bytes())
print("front matter valid")
PY
git diff --check -- docs/rfcs/0032-cross-repo-workflows-and-mcp-mutation-capabilities.md docs/rfcs/0035-multi-repo-test-harness-for-cross-repo-workflows.md docs/operator/plans/deferred-25-cross-repo-live-closure.md docs/operator/workflows/deferred-25-cross-repo-live-closure docs/operator/artifacts/deferred-25-cross-repo-live-closure
```
