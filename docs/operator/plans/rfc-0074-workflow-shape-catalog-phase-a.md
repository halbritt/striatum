---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_rfc-0074-workflow-shape-catalog-phase-a"
scope_kind: "rfc"
scope_ref: "docs/rfcs/0074-workflow-shape-and-adversary-pack-catalog.md"
state: "closed"
opened_at: "2026-05-22"
closed_at: "2026-05-23"
closure_summary: "Phase A catalog metadata, read-only discovery, and implementation-panel example validation landed; Phase B generator/UI behavior remains deferred."
supersedes: null
retrieval_priority: "high"
---

# RFC 0074 Workflow Shape Catalog Phase A Plan
author: coordinator-codex-gpt-5-001

## Outcome

RFC 0074 Phase A is closed. Metadata-first catalog entries, role/adversary
pack discovery, one implementation-panel example validation, read-only
discovery review, and closure have landed. Generator shape implementation,
role/adversary generation flags, cost estimation, RFC 0052 committee
artifacts, and web chooser pack selection remain deferred to Phase B or later.

## Inputs

- [`RFC 0074`](../../rfcs/0074-workflow-shape-and-adversary-pack-catalog.md)
- [`RFC 0034`](../../rfcs/0034-workflow-generator-and-template-catalog.md)
- [`RFC 0076`](../../rfcs/0076-three-lane-code-and-doc-audit-workflow.md)
- Historical generated-record source path: `docs/operator/artifacts/active-runway-1-5/phase4/SYNTHESIS.md`
- Historical generated-record source path: `docs/operator/artifacts/rfc-0076-audit-remediation/catalog-followup/PLAN.md`
- [`docs/operator/workflows/rfc-0074-phase-a-catalog.json`](../workflows/rfc-0074-phase-a-catalog.json)

## Workstreams

| Workstream | State |
|---|---|
| Discover role/adversary pack names, overlaps, and RFC 0076 fit | landed |
| Add metadata-first catalog entries and read-only discovery surfaces | landed |
| Validate one hand-authored implementation-panel example | landed |
| Review discovery surfaces for Phase A/Phase B boundary leaks | landed |
| Publish closure with validation evidence and remaining deferred work | landed |

## Workflow Scaffold

Validate the workflow:

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate --allow-same-model-pairing docs/operator/workflows/rfc-0074-phase-a-catalog.json
```

Closure evidence is recorded in
`docs/operator/artifacts/rfc-0074-phase-a-catalog/CLOSURE.md`.

## Guardrails

- Phase A is local package-data/catalog metadata plus one validating example.
- Do not make role packs or adversary packs runtime state, daemon state, model
  identity, or workflow-schema requirements.
- Do not add `workflow generate --shape implementation_panel`, `--role-pack`,
  `--adversary-pack`, or chooser pack-selection behavior in this phase.
- Do not add new artifact kinds or RFC 0052 debate/panel schemas.
- Keep catalog discovery read-only: listing, showing, rendering, and service
  read responses may expose packs, but write/generation flows must not pretend
  to honor pack choices yet.
