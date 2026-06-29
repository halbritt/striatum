---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_deferred-14-rfc0049-closure"
scope_kind: "rfc"
scope_ref: "docs/rfcs/0049-interactive-claude-lane-mcp-control-plane.md"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Deferred item 14 / RFC 0049 remains shelved under D106; current MCP and tmux work landed generic prerequisites but did not reopen the Claude-specific capability experiment."
supersedes: "plan_residual-deferred-closure-2026-05-23"
retrieval_priority: "high"
---

# Deferred 14 RFC 0049 Closure Plan
author: deferred14-rfc0049-codex-gpt-5-001

## Objective

Classify deferred item 14, RFC 0049 interactive Claude lane via MCP, as
reopened, closed, or shelved using current product decisions, source behavior,
and the active RFC 0050 / RFC 0075 MCP-session work.

## Scope

Owned writable paths:

- `docs/operator/plans/deferred-14-rfc0049-closure.md`
- `docs/operator/workflows/deferred-14-rfc0049-closure/`
- `docs/operator/artifacts/deferred-14-rfc0049-closure/`

Protected shared status docs are intentionally not edited:

- `docs/TODO.md`
- `docs/ROADMAP.md`
- `docs/operator/BRIEF.md`

## Closure Rule

Reopen RFC 0049 only if current evidence shows one of the D106 revisit
conditions is satisfied: materially changed billing terms, a supported answer
for PTY-supervised interactive Claude Code billing, or an explicitly funded
PTY/MCP spike with measurable success criteria.

Otherwise, preserve the status as shelved and record which landed MCP/tmux
pieces are generic prerequisites rather than Claude-specific implementation.

## Workflow

The runnable workflow lives at
`docs/operator/workflows/deferred-14-rfc0049-closure.json` and has two
jobs:

1. `classify_rfc0049`: audit decisions, RFCs, source, tests, and current
   external billing documentation.
2. `write_final_summary`: summarize the classification, validation, and any
   protected-doc follow-up.

## Result

Classification: shelved. RFC 0050 and RFC 0075 have landed important generic
MCP/tmux infrastructure, but the source still lacks RFC 0049's
Claude-specific `long_lived` lifecycle, `fresh_strategy`, real interactive
Claude spike evidence, and billing attribution proof. D106 remains current.
