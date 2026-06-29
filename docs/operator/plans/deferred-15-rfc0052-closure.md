---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_deferred-15-rfc0052-closure"
scope_kind: "rfc"
scope_ref: "docs/rfcs/0052-committee-deliberation-workflow.md"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Deferred item 15 requires a bounded RFC 0052 Phase A implementation RFC/design before production implementation; it is not closed by RFC 0074 primitives."
supersedes: null
retrieval_priority: "high"
---

# Deferred 15 RFC 0052 Closure Plan
author: coordinator-codex-gpt-5-001

## Outcome

Deferred item 15 is classified as requiring a new bounded implementation
RFC/design before production implementation is scheduled.

RFC 0052 remains useful and unblocked, but it is still a proposal with schema,
method, validation, recovery, and cost questions left open. RFC 0074 provides a
lighter implementation-panel catalog path on current primitives, but it
explicitly does not implement RFC 0052's typed debate artifacts, committee
phase, debate/panel daemon methods, or committee-specific validator behavior.

## Inputs

- [TODO item 43](../../reference/todo.md)
- [ROADMAP section 5.8](../../reference/roadmap.md)
- [Operator brief](../BRIEF.md)
- [RFC 0052](../../rfcs/0052-committee-deliberation-workflow.md)
- [RFC 0074](../../rfcs/0074-workflow-shape-and-adversary-pack-catalog.md)
- Historical generated-record source path: `docs/operator/artifacts/rfc-0074-phase-a-catalog/CLOSURE.md`
- [Workflow scaffold](../workflows/deferred-15-rfc0052-closure.json)
- Historical generated-record source path: `docs/operator/artifacts/deferred-15-rfc0052-closure/final/SUMMARY.md`

## Workflow

Validate the scaffold:

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate docs/operator/workflows/deferred-15-rfc0052-closure.json --json
```

The workflow is artifact-only and docs-scoped. It is designed to answer one
question: should RFC 0052 be scheduled now, closed as RFC 0074-covered, or
carried into a bounded implementation RFC?

## Guardrails

- Do not edit shared `docs/TODO.md`, `docs/ROADMAP.md`, or
  `docs/operator/BRIEF.md` from this closure packet.
- Do not claim RFC 0074 Phase A implements committee deliberation. It exposes
  catalog metadata and an implementation-panel example using existing
  artifact kinds.
- Do not add RFC 0052 artifact schemas, daemon methods, workflow phase types,
  or validator behavior without a separate accepted implementation design.
- Do not add hosted services, telemetry, transcript capture, external
  persistence, or provider SDK coupling.
- Keep `.striatum/` as operational scratch only.

## Follow-Up To Queue

When RFC 0052 becomes product priority, schedule a bounded implementation
RFC/workflow that fixes:

- final front-matter schemas for `debate_turn`, `arbitration_ruling`,
  `panel_vote`, `panel_verdict`, and `debate_synthesis`;
- whether debate/panel behavior is new daemon RPC methods or a stricter
  composition over existing artifact publication and verdict paths;
- workflow schema and validator rules for committee phases;
- verdict, decision, recovery, auto-finalize, and corpus-export behavior;
- A/B benchmark instrumentation and acceptance criteria before broad catalog
  integration.
