# Engram Reference Fixture Context

Status: historical
Date: 2026-05-07

> **Current direction.** For Striatum's live relationship to Engram, see
> [`docs/reference/spec.md` § Corpus Export And Augmentation Boundary](../../reference/spec.md),
> [RFC 0041](../../rfcs/0041-engram-memory-layer-for-striatum-operators.md),
> [RFC 0044](../../rfcs/0044-engram-phase-1-implementation-spec.md), and the
> proposed contract scaffold in
> [RFC 0057](../../rfcs/0057-corpus-contract-v2.md). Engram is positioned as an
> **optional local augmentation consumer** of `striatum corpus export`
> bundles, not a runtime dependency. This document is retained for
> *incubation provenance* only.

Striatum was incubated inside the Engram repository so its design and MVP build
could use the real context that exposed the need for it. The project has now
split into a standalone repository. Engram remains the first external reference
fixture and validation history.

## Why Incubate Inside Engram

Engram produced the motivating workflow:

- multi-model design and review using Claude, Codex, and Gemini;
- exact model identity mattered to confidence;
- tmux panes gave useful visibility but poor introspection;
- marker files were useful durable artifacts but too weak as the live message
  bus;
- reject/re-review paths needed explicit state;
- prompt chains, findings, syntheses, and decisions needed durable artifacts;
- branch and commit authority needed to remain human-controlled.

The incubation period let the design team inspect the actual rough process
rather than designing from a sanitized abstraction.

## Boundaries

- `striatum` remains a generic local terminal-agent orchestrator, not an
  Engram-only tool.
- Engram is the reference customer and first fixture.
- Engram's local-first/no-unapproved-cloud-dependency posture should inform
  safety and privacy defaults.
- Engram-specific paths, prompt ordinals, and marker names belong in examples
  or workflow fixtures, not core product logic.
- The standalone Striatum repository is the product boundary.

## Engram Context To Read

From the Engram repo root:

1. `README.md`
2. `docs/process/multi-agent-review-loop.md`
3. `docs/process/project-judgment.md`
4. `docs/process/phase-3-agent-runbook.md`
5. `scripts/phase3_tmux_agents.sh`
6. `prompts/P021_generate_phase_3_claims_beliefs_spec.md` through
   `prompts/P031_begin_phase_3_pipeline.md`

Treat these as reference material for `striatum` requirements, not as
product architecture to copy blindly.
