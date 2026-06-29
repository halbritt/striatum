---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_deferred-27-engram-side-closure"
scope_kind: "phase"
scope_ref: "docs/TODO.md#32-queue-engram-side-tenant-aware-rfc-0044-phase-1"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Engram-side RFC 0044 Phase 1 ingester, MCP server, retrieval tools, and memory capabilities are classified as external Engram work, not remaining Striatum core work."
supersedes: null
retrieval_priority: "normal"
---

# Deferred 27 Engram-Side Memory Tools Closure
author: deferred27-engram-side-codex-gpt-5-001

## Scope

Close deferred item 27 for Engram-side memory ingester, MCP server, and
retrieval tools. The bounded question is whether Striatum still has core work
to do for RFC 0044 Phase 1 after the Striatum corpus export and Corpus V2
reference-only augmentation boundary landed, or whether the remaining work
belongs entirely in the external Engram repository.

## Evidence Base

- `docs/TODO.md` items 23 and 32
- `docs/ROADMAP.md` section 5.7 and blocked table item 32
- `docs/operator/BRIEF.md`
- `docs/SPEC.md` Corpus Export And Augmentation Boundary
- `docs/UBIQUITOUS_LANGUAGE.md` augmentation terms
- `docs/rfcs/0041-engram-memory-layer-for-striatum-operators.md`
- `docs/rfcs/0044-engram-phase-1-implementation-spec.md`
- `docs/rfcs/0057-corpus-contract-v2.md`
- `tests/test_cli_corpus_export.py`
- `tests/test_corpus_verify.py`

## Workflow

Runnable scaffold:
`docs/operator/workflows/deferred-27-engram-side-closure.json`.

The workflow has one bounded synthesis job that classifies the remaining
Engram-side RFC 0044 Phase 1 implementation surface and records validation
evidence for the Striatum boundary invariants.

Writes are limited to:

- `docs/operator/artifacts/deferred-27-engram-side-closure/`

Shared `TODO`, `ROADMAP`, `BRIEF`, RFC, source, and test files are
intentionally not edited from this closure packet.

## Outcome

No Striatum source, test, or shared-document change is required. Current
Striatum docs already classify the Engram ingester, `engram-mcp-stdio`, the
read-only retrieval tools, and Engram-local `memory.*` capabilities as
external consumer work under `~/git/engram/`.

Final artifact:
`docs/operator/artifacts/deferred-27-engram-side-closure/closure/EXTERNAL_ENGRAM_WORK.md`.
