# RFC 0078 Python Test Migration Plan

Status: active
Date: 2026-05-25
author: operator-codex-gpt-5.5-001

## Scope

Finish the RFC 0078 test-suite migration by replacing pytest coverage with
Go, shell, and browser checks before Python source and tests are deleted from
active Striatum head.

This plan is intentionally narrower than the whole Go-only runtime cutover.
It owns the executable workflow scaffold at
`docs/operator/workflows/rfc-0078-python-test-migration.json` and
focuses on test coverage only: coverage ledger refinement, PostgreSQL harness
parity, CLI route tests, local web/browser tests, workflow and artifact
contract tests, corpus/archive tests, packaging smoke, and final deletion
readiness.

## Inputs

- `docs/rfcs/0078-go-only-runtime-and-python-removal.md`
- `docs/operator/artifacts/rfc-0078-go-only-runtime-and-python-removal/tests/HANDOFF.md`
- `docs/operator/artifacts/rfc-0078-go-only-runtime-and-python-removal/inventory/CUTOVER_LEDGER.md`
- current `tests/` pytest coverage
- current Go tests under `go/`
- frontend tests under `src/striatum/web/frontend/src/__tests__/`
- smoke scripts under `scripts/`

## Execution Shape

The workflow declares `parallelism.max_active_jobs` as `20` to preserve the
RFC 0078 high-parallelism posture. The coverage ledger refinement runs first
and publishes the test migration ledger. After that, six migration slices run
in parallel with disjoint write scopes, followed by a final deletion-readiness
gate.

The workflow should not delete Python tests solely because a replacement file
exists. Each deletion must cite a ledger row and one of:

- Go replacement coverage with command evidence.
- Shell/browser replacement coverage with command evidence.
- Explicit retirement reason tied to an accepted RFC/decision or current
  product boundary.
- Historical-provenance exception that is outside active runtime coverage.

## Workstreams

1. Refine the pytest coverage ledger into row-level migration decisions.
2. Build reusable Go PostgreSQL test harness coverage for daemon/RPC stateful
   behavior.
3. Port CLI pytest coverage to Go command/RPC tests and focused shell smoke.
4. Port local web and browser-facing pytest coverage to Go route tests and
   frontend browser/component tests.
5. Port workflow validation/generation and artifact/front-matter tests to Go.
6. Port corpus export, archive, redaction, and replay verification tests to
   Go.
7. Replace Python package/fresh-clone smoke with Go distribution smoke.
8. Publish final deletion readiness: remaining blockers, allowed exceptions,
   and the replacement aggregate validation command.

## Gates

- `coverage_ledger_refinement` must complete before any migration slice.
- Migration slices may remove or retire Python tests only when their artifact
  names the ledger row and replacement evidence.
- `final_deletion_readiness` must not declare Python deletion ready while any
  pytest row remains `unmapped`, `blocked`, or `replacement_missing`.
- Final readiness must name the aggregate command that replaces `make test`
  once pytest is gone.

## Non-Goals

- Porting all Python source in this workflow.
- Rewriting current documentation outside the test-migration evidence needed
  by the final readiness gate.
- Reopening Python daemon, Python MCP, repo-local SQLite, or legacy local-state
  behavior as transitional test harnesses.
- Adding hosted services, telemetry, cloud APIs, or external persistence.
