---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_rfc-0078-docs-guardrails-final-deletion"
scope_kind: "rfc"
scope_ref: "docs/rfcs/0078-go-only-runtime-and-python-removal.md"
state: "open"
opened_at: "2026-05-25"
closed_at: null
closure_summary: null
supersedes: "plan_rfc-0078-go-only-runtime-and-python-removal"
retrieval_priority: "high"
---

# RFC 0078 Docs, Guardrails, And Final Deletion Plan
author: operator-codex-gpt-5.5-001

## Outcome

Finish the remaining RFC 0078 work after the first Go-only runtime slices:
supersede obsolete Python-era decisions and RFC guidance, rewrite current
operator and adopter docs, update skill/plugin templates, install Python-trace
guardrails, run the final deletion gate, and publish acceptance closure.

This plan is intentionally the final gate, not a discovery workflow. Earlier
RFC 0078 work owns broad Go parity and cutover ledgers. This workflow should
only proceed when those ledgers show that each Python behavior is replaced,
retired, or preserved solely as historical provenance.

## Inputs

- `docs/operator/BRIEF.md`
- `docs/operator/plans/rfc-0078-go-only-runtime-and-python-removal.md`
- `docs/operator/workflows/rfc-0078-go-only-runtime-and-python-removal.json`
- `docs/rfcs/0078-go-only-runtime-and-python-removal.md`
- `docs/rfcs/0068-go-production-daemon-port.md`
- `docs/rfcs/0070-daemon-client-service-boundary.md`
- `docs/SPEC.md`
- `docs/DECISION_LOG.md`
- `docs/TODO.md`
- `docs/ROADMAP.md`
- RFC 0078 cutover artifacts under `docs/operator/artifacts/rfc-0078-go-only-runtime-and-python-removal/`

## Workflow Scaffold

Run or validate the workflow here:

```bash
striatum workflow validate --allow-same-model-pairing docs/operator/workflows/rfc-0078-docs-guardrails-final-deletion.json
```

The workflow declares high parallelism (`max_active_jobs: 12`) but keeps the
first implementation wave write scopes disjoint:

| Workstream | State | Primary output |
|---|---|---|
| Decision and RFC supersession | scaffolded | Supersession handoff |
| Active documentation rewrite | scaffolded | Docs rewrite handoff |
| Skill and plugin template rewrite | scaffolded | Template rewrite handoff |
| Python-trace guardrail implementation | scaffolded | Guardrail handoff |
| Final deletion gate | scaffolded | Deletion gate report |
| Independent review | scaffolded | Review finding |
| Acceptance closure | scaffolded | Final summary |

## Gate Rules

- Do not delete active Python source, tests, packaging, or templates unless
  the finalizer can name replacement, explicit retirement, or accepted
  historical-provenance status for that path class.
- Do not remove historical dogfood, old prompts, or provenance artifacts merely
  because they mention Python. Mark them historical when needed.
- Do not restore SQLite, Python daemon, Python MCP wrapper, direct Python CLI
  authority, Python packaging as the distribution path, or Python-only operator
  setup instructions.
- Do not add hosted services, telemetry, transcript capture, external
  persistence, or provider SDK imports as part of the cutover.
- Keep every live workflow-control claim aligned with daemon-owned PostgreSQL,
  Go daemon authority, daemon MCP/RPC, and local web UI operator actions.

## Acceptance Criteria

- Replaced decisions and RFCs use `superseded` or successor links where the
  live rule changed, without deleting provenance.
- Current docs and templates no longer instruct new operators to install,
  invoke, or depend on Python Striatum runtime surfaces.
- Python-trace guardrails fail on active Striatum Python source, tests,
  packaging, or operator instructions, while allowing target repositories to
  run Python as their own workload and allowing marked historical provenance.
- The final deletion gate reports zero unclassified active Python traces.
- Review accepts the gate evidence, or records exact blockers before closure.
