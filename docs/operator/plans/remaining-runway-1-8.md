---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_remaining-runway-1-8"
scope_kind: "initiative"
scope_ref: "docs/operator/BRIEF.md"
state: "open"
opened_at: "2026-05-23"
closed_at: null
closure_summary: null
supersedes: null
retrieval_priority: "high"
---

# Remaining Runway 1-8 Plan
author: coordinator-codex-gpt-5-001

## Outcome

Create ordered scaffold artifacts for the remaining eight tracks while keeping
implementation work out of this workflow:

1. TODO 55 accepted-risk CLI/UI client polish.
2. TODO 56 D125 live auto-finalize dogfood gate evidence and default dry-run.
3. RFC 0075 fail-closed tmux requirements and UI polish.
4. CLI retirement UI/MCP parity.
5. TODO 59 Corpus Contract V2 watermark and archive follow-through.
6. TODO 60 Git/PR request artifacts and local commit confirmation.
7. RFC 0074 Phase A catalog/generator metadata, discovery, and example.
8. TODO 61 legacy cleanup.

The workflow serializes only the eight scaffold jobs. Each scaffold unlocks an
independent validation job and an independent boundary review job, which may
run in parallel with later scaffold jobs.

## Inputs

- [`docs/operator/BRIEF.md`](../BRIEF.md)
- [`docs/TODO.md`](../../reference/todo.md)
- [`docs/DECISION_LOG.md`](../../decisions/decision-log.md)
- Historical generated-record source path: `docs/operator/artifacts/active-runway-1-5/FINAL.md`
- [`RFC 0130`](../../rfcs/0130-go-daemon-http-sse-mcp.md)
- [`RFC 0057`](../../rfcs/0057-corpus-contract-v2.md)
- [`RFC 0064`](../../rfcs/0064-review-diversity-enforcement.md)
- [`RFC 0066`](../../rfcs/0066-replay-archive-corpus-v2-foundations.md)
- [`RFC 0067`](../../rfcs/0067-optional-git-pr-integration.md)
- [`RFC 0068`](../../rfcs/0068-go-production-daemon-port.md)
- [`RFC 0074`](../../rfcs/0074-workflow-shape-and-adversary-pack-catalog.md)
- [`RFC 0075`](../../rfcs/0075-tmux-observable-mcp-agent-sessions.md)
- [`RFC 0077`](../../rfcs/0077-mcp-activity-liveness-deadlines.md)
- [`docs/operator/workflows/remaining-runway-1-8.json`](../workflows/remaining-runway-1-8.json)

## Ordering And Parallelism

| Order | Track | Scaffold dependency | Parallel follow-up |
|---:|---|---|---|
| 1 | TODO 55 accepted-risk CLI/UI | none | validation + review after scaffold 1 |
| 2 | TODO 56 dogfood gate/default dry-run | scaffold 1 | validation + review after scaffold 2 |
| 3 | RFC 0075 fail-closed tmux/UI | scaffold 2 | validation + review after scaffold 3 |
| 4 | CLI retirement UI/MCP parity | scaffold 3 | validation + review after scaffold 4 |
| 5 | TODO 59 corpus watermark/archive | scaffold 4 | validation + review after scaffold 5 |
| 6 | TODO 60 Git/PR requests/local commit | scaffold 5 | validation + review after scaffold 6 |
| 7 | RFC 0074 Phase A catalog/generator | scaffold 6 | validation + review after scaffold 7 |
| 8 | TODO 61 legacy cleanup | scaffold 7 | validation + review after scaffold 8 |

The final closer waits for all scaffold, validation, and review artifacts. It
does not block later scaffold work on earlier validation/review unless a future
operator explicitly changes the workflow.

## Workflow Scaffold

Validate the workflow:

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate --allow-same-model-pairing docs/operator/workflows/remaining-runway-1-8.json
```

Prepare and start it through the daemon-backed runner when ready:

```bash
PYTHONPATH=src python3 -m striatum.cli run prepare --workflow docs/operator/workflows/remaining-runway-1-8.json --json
PYTHONPATH=src python3 -m striatum.cli run start --run-id <run_id> --json
```

## Guardrails

- Produce scaffold artifacts only; do not edit product source, tests, roadmap,
  TODO, or the current operator brief in this workflow.
- Keep live workflow state daemon/PostgreSQL-owned. Repository artifacts are
  provenance, not the message bus.
- Do not retire CLI workflow-control verbs before MCP/UI parity exists and is
  covered by tests.
- Do not make tmux panes, pane text, or transcripts authoritative workflow
  state.
- Do not reopen repo-local SQLite or legacy daemon registry paths in
  production.
- Do not add hosted services, telemetry, transcript capture, provider SDKs, or
  external persistence.
