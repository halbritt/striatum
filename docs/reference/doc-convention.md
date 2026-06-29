---
type: reference
status: canonical
owner: halbritt
expires: null
---

# Documentation convention (striatum)

striatum follows the **shared** documentation convention single-sourced in
[`doc-convention-lint`](https://github.com/halbritt/doc-convention-lint) and
vendored here, pinned by SHA, via `.pre-commit-config.yaml`. There is one
convention across striatum + engram; this repo supplies only an extend-only
overlay (`./doc-convention.yaml`).

This convention is the **layout + enforcement** companion to
[`doc-map.md`](doc-map.md), which remains the **concept-ownership** contract
("one home per concept, every other doc cites it"). doc-map says *which doc owns
a concept*; this convention says *which shelf a doc lives on and how that is
machine-checked*.

## The model

Two axes. **Curated vs provenance:** curated docs are human-intent, mutable,
edited in place; provenance is historical run output, research snapshots,
audits, and retired scaffolds that should remain discoverable without reading
as current guidance. Current curated docs live under `docs/how-to/`,
`docs/reference/`, `docs/agents/`, `docs/decisions/`, and `docs/rfcs/`.
`docs/operator/`, `docs/campaigns/`, and `docs/dogfoods/` are sanctioned
runtime or workflow-fixture regions. `docs/audits/` is the browseable audit
corpus. `docs/records/_frozen/` is the frozen archival tail.

## TL;DR for an agent about to write a doc

1. Is this current operator/runtime material?
   → use the existing sanctioned region (`docs/operator/`, `docs/campaigns/`,
   or `docs/dogfoods/`) and keep the directory README accurate.
2. Is this a whole-repo audit, hygiene report, review, or reconcile report?
   → `docs/audits/`.
3. Is this frozen provenance, old research, or an archived run packet?
   → `docs/records/_frozen/`.
4. Else it is curated: task guides go in `docs/how-to/`, lookup contracts in
   `docs/reference/`, agent-facing guidance in `docs/agents/`, designs in
   `docs/rfcs/`, and decisions in `docs/decisions/`.

## Status

**Migration Phase 1 — warn-only.** The linter reports but does not block.
Striatum keeps a repo-specific overlay because several runtime surfaces are
path contracts, not generic Diataxis shelves. Run
`doc-lint lint --all --warn-only` to see current drift.
