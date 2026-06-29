---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_todo-63-daemon-client-boundary-closure"
scope_kind: "initiative"
scope_ref: "docs/TODO.md#63-rfc-0070-daemon-clientservice-boundary-completion"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "RFC 0070 residuals close with primitive daemon methods as the supported path; no source change was required."
supersedes: null
retrieval_priority: "high"
---

# TODO 63 Daemon Client Boundary Closure
author: coordinator-codex-gpt-5-001

## Outcome

Close TODO 63 / RFC 0070 by validating the current daemon client/service
boundary and recording that the removed dogfood composites should stay absent
from the production daemon contract. Operators should continue using primitive
daemon methods unless a later accepted RFC or decision defines a
PostgreSQL-native operator-composite surface.

## Evidence Scope

- `docs/rfcs/0070-daemon-client-service-boundary.md`
- `docs/TODO.md` item 63
- `docs/operator/BRIEF.md`
- `docs/ROADMAP.md`
- `docs/DECISION_LOG.md` D110 and D112
- `tests/test_cli_daemon_rpc_route.py`
- `tests/test_service.py`
- `tests/test_chat_tools_daemon_boundary.py`
- `tests/test_mcp_mutation_capabilities.py`
- `tests/test_mcp_dogfood_e2e.py`
- `go/pkg/mcp/http_test.go`

## Workflow

The bounded workflow scaffold is:

```bash
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=src .venv/bin/python -m striatum.cli workflow validate docs/operator/workflows/todo-63-daemon-client-boundary-closure.json
```

If a later operator wants to rerun the closure through live Striatum state:

```bash
PYTHONPATH=src python3 -m striatum.cli run prepare --workflow docs/operator/workflows/todo-63-daemon-client-boundary-closure.json --json
PYTHONPATH=src python3 -m striatum.cli run start --run-id <run_id> --json
```

## Guardrails

- Do not edit shared TODO, roadmap, brief, RFC, decision, or architecture
  ledgers from this closure slice.
- Do not reintroduce `dogfood.publish_on_behalf`,
  `dogfood.surgical_recovery`, or `apply.reviewed_patch` into the production
  daemon contract without a new accepted decision.
- Keep local `striatum.api.invoke` limited to explicit compatibility,
  local-authoring, and test-fixture paths.
- Keep daemon MCP `tools/list` and `tools/call` filtered through the supported
  production method set.

## Closure Artifact

The no-action closure artifact is
`docs/operator/artifacts/todo-63-daemon-client-boundary-closure/closure/NO_ACTION.md`.

## Shared Updates To Queue

Do not apply these in this slice. A later operator update should:

- mark TODO 63 as done or closed in `docs/TODO.md`;
- update `docs/ROADMAP.md` to remove TODO 63 from residual cleanup;
- refresh `docs/operator/BRIEF.md` once the next current-state brief is
  authored;
- optionally change RFC 0070 status from `mostly implemented` to an
  implemented/closed status if the maintainer wants RFC status to mirror this
  artifact.
