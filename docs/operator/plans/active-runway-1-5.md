---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_active-runway-1-5"
scope_kind: "operator_runway"
scope_ref: "docs/operator/BRIEF.md"
state: "closed"
opened_at: "2026-05-22"
closed_at: "2026-05-22"
closure_summary: "Workflow run_b2e013582e0aeba267dd7a47cc66ccf1 completed 14 jobs and produced FINAL.md with ordered implementation batches."
supersedes: null
retrieval_priority: "high"
---

# Active Runway 1-5 Plan
author: coordinator-codex-gpt-5-001

## Outcome

Drive the next five operator workstreams in order while using maximum
parallelism inside each ordered phase:

1. TODO 55/56/59/60 implementation follow-ups.
2. CLI retirement and MCP/UI parity.
3. RFC 0075 tmux-observable session metadata.
4. RFC 0074 Phase A catalog/generator scaffold, including deferred RFC 0076
   catalog entries.
5. Residual TODO 61/62/63 cleanup.

This plan does not turn terminal output into workflow authority, does not add
hosted services, does not re-open repo-local SQLite authority, and does not
retire CLI workflow-control verbs before MCP/UI parity is covered.

## Inputs

- [`docs/operator/BRIEF.md`](../BRIEF.md)
- [`docs/ROADMAP.md`](../../reference/roadmap.md)
- [`docs/TODO.md`](../../reference/todo.md)
- [`D124-D129`](../../decisions/decision-log.md)
- [`RFC 0130 MCP`](../../rfcs/0130-go-daemon-http-sse-mcp.md)
- [`RFC 0074`](../../rfcs/0074-workflow-shape-and-adversary-pack-catalog.md)
- [`RFC 0075`](../../rfcs/0075-tmux-observable-mcp-agent-sessions.md)
- [`RFC 0077`](../../rfcs/0077-mcp-activity-liveness-deadlines.md)
- [`docs/operator/workflows/active-runway-1-5.json`](../workflows/active-runway-1-5.json)

## Ordered Phases

| Phase | Parallelism | Output |
|---|---:|---|
| 1. TODO 55/56/59/60 | 4 lanes | Four implementation packets plus a synthesis that chooses a safe order. |
| 2. CLI retirement | 1 lane | CLI cutover ledger and survivor classification. |
| 3. RFC 0075 tmux | 1 lane | Smallest tmux metadata implementation packet. |
| 4. RFC 0074 Phase A | 2 lanes | Catalog metadata and runnable-example packets plus synthesis. |
| 5. TODO 61/62/63 residuals | 3 lanes | Bounded cleanup packets and final closure. |

## Workflow Scaffold

Validate the workflow:

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate --allow-same-model-pairing docs/operator/workflows/active-runway-1-5.json
```

Prepare and start it through the daemon-backed runner:

```bash
PYTHONPATH=src python3 -m striatum.cli run prepare --workflow docs/operator/workflows/active-runway-1-5.json --json
PYTHONPATH=src python3 -m striatum.cli run start --run-id <run_id> --json
```

## Guardrails

- Keep implementation batches disjoint when they move from planning packets
  into source patches.
- Use daemon-owned PostgreSQL and MCP/RPC surfaces as live authority.
- Keep RFC 0076 generator/catalog work in RFC 0074 Phase A unless a later
  decision changes the priority.
- Treat TODO 61/62/63 residuals as cleanup and guardrail maintenance, not as
  permission to restore retired compatibility paths.
