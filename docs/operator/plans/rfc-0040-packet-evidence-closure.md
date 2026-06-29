---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_rfc-0040-packet-evidence-closure"
scope_kind: "rfc"
scope_ref: "docs/rfcs/0040-mcp-driven-dogfood-harness.md"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Workflow scaffolded, packet-evidence residuals classified, and the bounded PostgreSQL artifact-author projection gap closed."
supersedes: null
retrieval_priority: "high"
---

# RFC 0040 Packet Evidence Closure
author: coordinator-codex-gpt-5-001

## Scope

Close the RFC 0040 V1.6 residual named by TODO item 28: packet evidence and
provenance debt left after dogfood-044's cycle-exhaustion override. This is
not a request to restore the retired SQLite-era dogfood composite tools.

## Evidence Base

- `docs/TODO.md` item 28
- `docs/ROADMAP.md` section 6
- `docs/DECISION_LOG.md` D098 and D110
- `docs/rfcs/0040-mcp-driven-dogfood-harness.md`
- `docs/SPEC.md` artifact, work-packet, evidence-export, and MCP boundary
  sections
- Current MCP/provenance tests around MCP structured errors, packet author
  lines, artifact publication, artifact detail, and evidence redaction

## Workflow

Runnable scaffold:
`docs/operator/workflows/rfc-0040-packet-evidence-closure.json`.

Validation:

```bash
PYTHONPATH=src .venv/bin/python -m striatum.cli workflow validate docs/operator/workflows/rfc-0040-packet-evidence-closure.json --json
```

## Closure

The residual was narrowed to one bounded source gap: PostgreSQL artifact
summaries preserved the recorded byline under compatibility keys, but did not
also project it as the packet/evidence identity fields consumed by evidence
export and run-summary renderers. The fix now exposes recorded artifact
bylines as `author.line` and `author.actual_author_line` while retaining the
existing `author.author_line` compatibility field for list clients.

No PostgreSQL-native `dogfood.publish_on_behalf` or
`dogfood.surgical_recovery` composite was added. D110 keeps the historical
SQLite-bound composite names out of the production daemon contract until a
separate accepted PostgreSQL-native design exists.

## Needed Shared-Doc Updates

Do not make these updates in this scoped worker turn:

- Mark TODO item 28's remaining packet-evidence debt as closed once this
  patch is accepted.
- Update ROADMAP section 6 for RFC 0040 V1.6 from "remaining packet-evidence
  debt" to closed.
- Add a pointer from the next operator brief only if the operator wants this
  closure visible in current-state retrieval.
