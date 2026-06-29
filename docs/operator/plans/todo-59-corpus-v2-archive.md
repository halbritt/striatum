---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_todo-59-corpus-v2-archive"
scope_kind: "todo"
scope_ref: "docs/TODO.md#59-phase-11-replay-archive-and-corpus-v2-foundations"
state: "open"
opened_at: "2026-05-22"
closed_at: null
closure_summary: null
supersedes: null
retrieval_priority: "high"
---

# TODO 59 Corpus V2 Archive Follow-Up
author: coordinator-codex-gpt-5-001

## Outcome

Close the remaining TODO 59 follow-up around Corpus Contract V2 and run
archives: archive-default enforcement, deep verification behavior,
read-only semantic inspection, and the remaining local-only augmentation
reference boundary if it is still needed after mapping current source state.

This scaffold does not change product policy. D126 is the decision boundary:
composite corpus identity, graduated redaction, hybrid archive bundles,
verification replay by default, deep-chain verification, read-only semantic
inspection, no comparative replay, and optional daemon audit-chain cross-check.

## Inputs

- [`docs/TODO.md`](../../reference/todo.md)
- [`D126`](../../decisions/decision-log.md)
- [`docs/SPEC.md`](../../reference/spec.md)
- [`RFC 0057`](../../rfcs/0057-corpus-contract-v2.md)
- [`RFC 0066`](../../rfcs/0066-replay-archive-corpus-v2-foundations.md)
- [`docs/operator/BRIEF.md`](../BRIEF.md)
- [`docs/operator/workflows/todo-59-corpus-v2-archive.json`](../workflows/todo-59-corpus-v2-archive.json)

## Workstreams

| Workstream | State |
|---|---|
| Map current TODO 59 source, test, and doc state against D126 | scaffolded |
| Implement the smallest archive-default, deep-verification, and semantic-inspection follow-up | scaffolded |
| Review authority, privacy, and augmentation-not-dependency boundaries | scaffolded |
| Publish closure evidence and remaining deferrals | scaffolded |

## Workflow Scaffold

Validate the workflow:

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate --allow-same-model-pairing docs/operator/workflows/todo-59-corpus-v2-archive.json
```

Prepare and start it through the daemon-backed runner:

```bash
PYTHONPATH=src python3 -m striatum.cli run prepare --workflow docs/operator/workflows/todo-59-corpus-v2-archive.json --json
PYTHONPATH=src python3 -m striatum.cli run start --run-id <run_id> --json
```

## Guardrails

- Keep all live workflow state daemon/PostgreSQL-owned.
- Keep archive and corpus verification local, read-only, and deterministic.
- Do not add hosted services, telemetry, transcript capture, external
  persistence, or runtime retrieval dependencies.
- Do not make augmentation references a prerequisite for workflow progress.
- Do not reopen repo-local SQLite or `.striatum/` as live state.
