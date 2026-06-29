---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_rfc-0078-go-only-packaging-release"
scope_kind: "rfc"
scope_ref: "docs/rfcs/0078-go-only-runtime-and-python-removal.md"
state: "open"
opened_at: "2026-05-25"
closed_at: null
closure_summary: null
supersedes: null
retrieval_priority: "high"
---

# RFC 0078 Go-Only Packaging Release Plan
author: coordinator-codex-gpt-5-001

## Outcome

Execute the RFC 0078 packaging, release, and installation cutover as a
standalone max-parallel workflow. The prior RFC 0078 run established the
cutover ledger and identified packaging as a next gate; this plan turns that
gate into executable work packets for the version source, Go module layout,
Makefile/CI/release archives, embedded assets, smoke scripts, install docs,
and PyPI retirement.

## Inputs

- [`RFC 0078`](../../rfcs/0078-go-only-runtime-and-python-removal.md)
- Historical generated-record source path: `docs/operator/artifacts/rfc-0078-go-only-runtime-and-python-removal/packaging/HANDOFF.md`
- Historical generated-record source path: `docs/operator/artifacts/rfc-0078-go-only-runtime-and-python-removal/final/SUMMARY.md`
- Historical generated-record source path: `docs/operator/artifacts/rfc-0078-go-only-runtime-and-python-removal/inventory/CUTOVER_LEDGER.md`
- [`docs/operator/workflows/rfc-0078-go-only-packaging-release.json`](../workflows/rfc-0078-go-only-packaging-release.json)

## Workstreams

| Workstream | State | Primary write scope |
|---|---|---|
| Decide root `VERSION` and root Go module policy | scaffolded | decision artifact only |
| Replace Makefile, CI, and release archive mechanics | scaffolded | `VERSION`, Makefiles, CI workflows, release scripts |
| Embed Go-owned runtime assets | scaffolded | Go asset packages and embedding tests |
| Replace package and fresh-clone smoke scripts | scaffolded | smoke scripts and smoke docs |
| Rewrite active install/release docs to Go-only guidance | scaffolded | active install and release docs |
| Define and publish PyPI retirement sequence | scaffolded | retirement artifact first, optional docs after review |
| Synthesize packaging gate status and next deletion gate | scaffolded | final synthesis artifact |

## Execution Shape

The workflow is intentionally high parallelism:

- `parallelism.max_active_jobs` is `12`;
- the first six jobs can run independently;
- initial implementation jobs have disjoint product write scopes;
- decision-heavy jobs publish artifacts before product edits that depend on
  those decisions;
- the final synthesis waits for every parallel stream and records whether the
  packaging gate is ready to unblock Python packaging deletion.

## Boundaries

- Do not reintroduce Python as a release, install, smoke, or package bridge.
- Do not publish new Python runtime artifacts as part of the Go-only cutover
  unless a later accepted decision explicitly chooses a transitional
  deprecation artifact.
- Keep daemon-owned PostgreSQL and Go daemon/MCP authority unchanged.
- Keep Node/Vite contributor tooling out of scope unless a retained web asset
  packaging path needs a deterministic frontend build check.
- Historical RFC and decision mentions are provenance; active install,
  release, operator, and agent guidance must become Go-only after the cutover.

## First Gate

Run validation before preparing the workflow:

```bash
striatum workflow validate --allow-same-model-pairing docs/operator/workflows/rfc-0078-go-only-packaging-release.json
```

The first implementation pass should prefer additive Go packaging scaffolds
and replacement smoke checks over deleting `pyproject.toml` immediately.
Deletion belongs behind the final synthesis gate once the release archive,
embedded assets, install docs, and PyPI retirement plan agree.
