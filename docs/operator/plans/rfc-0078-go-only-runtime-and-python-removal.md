# RFC 0078 Go-Only Runtime And Python Removal Plan

Status: active
Date: 2026-05-24
author: operator-codex-gpt-5.5-001

## Scope

Execute RFC 0078 by turning the proposed Go-only runtime cutover into a
max-parallel workflow, then drive the implementation in bounded slices until
Python is no longer an active Striatum product surface.

## Execution Shape

The scaffolded workflow is
`docs/operator/workflows/rfc-0078-go-only-runtime-and-python-removal.json`.
It sets `parallelism.max_active_jobs` to `20`, matching the highest existing
operator workflow concurrency in this repository. Current Codex sub-agent
capacity accepted six live agents; those are assigned to the first six
independent audit/implementation slices.

## Workstreams

- Inventory every Python trace and classify it as port, retire, rewrite_doc,
  historical_provenance, or delete_after_gate.
- Define the Go CLI replacement for the Python `striatum` executable.
- Map Python web/service routes to Go replacements or explicit retirements.
- Close workflow-authoring, workflow-generation, and artifact schema parity
  gaps.
- Migrate pytest coverage to Go, shell, or browser checks.
- Replace Python packaging, release, and install surfaces.
- Update current docs and supersession notes after the implementation shape is
  accepted.
- Add deletion-gate guardrails so Python product traces cannot return.

## First Gate

Do not mechanically delete `src/` or `tests/` until the cutover ledger names
the replacement, retirement decision, or accepted historical exception for
each behavior class. The first implementation slice should make Python removal
measurably closer while preserving the Go daemon/PostgreSQL/MCP authority
boundary.
