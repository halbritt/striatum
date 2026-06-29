# RFC 0078 Go Web Service Cutover Plan

Status: active
Date: 2026-05-25
author: operator-codex-gpt-5.5-002

## Scope

Execute the RFC 0078 local web/service slice as an executable Striatum
workflow. The outcome is either a Go replacement for the remaining Python
local service/web behavior or an explicit route-retirement decision for routes
that should not survive the Go-only runtime cutover.

This plan is deliberately narrower than the umbrella RFC 0078 plan. It owns the
local HTTP service, local web UI routes, static/template serving, SSE event
stream behavior, route-level tests, security headers/mutation gates, and
retirement guardrails. It does not own the full Go CLI router, packaging
cutover, workflow-generator parity, pytest migration outside service-route
coverage, or wholesale Python deletion.

## Executable Workflow

Workflow:
`docs/operator/workflows/rfc-0078-go-web-service-cutover.json`

The workflow declares `parallelism.max_active_jobs: 12`. Six independent root
jobs can run immediately:

- route retention and retirement decision ledger;
- Go service/security layer implementation;
- static asset and template embedding;
- SSE event stream replacement;
- route tests and parity fixtures;
- retirement guardrails.

All root jobs converge into a final synthesis artifact that records shipped
behavior, routes retired by decision, remaining blockers, validation commands,
and the next executable slice if the Python web/service surface is not yet
fully removable.

## Route Decision Rules

Keep a route only when it is still part of the current operator surface, can be
backed by daemon RPC/PostgreSQL authority, and has route-level coverage in Go.

Retire a route when it is historical dogfood convenience, duplicate
compatibility surface, Python-only scaffolding, or would preserve a second
state authority. Retirement must be named in the route ledger and guarded by a
test or static check that prevents accidental reintroduction.

Do not keep routes that depend on Python module imports, repo-local SQLite,
terminal output scraping, transcripts, hosted services, telemetry, or external
persistence.

## Expected Evidence

The run should publish durable artifacts under
`docs/operator/artifacts/rfc-0078-go-web-service-cutover/`:

- `routes/ROUTE_LEDGER.md` for keep/port/retire decisions;
- `service/HANDOFF.md` for service/security implementation evidence;
- `static/HANDOFF.md` for embedding and CSP/static behavior;
- `sse/HANDOFF.md` for event-stream behavior;
- `tests/HANDOFF.md` for route coverage and command results;
- `guardrails/HANDOFF.md` for retirement guards;
- `final/SUMMARY.md` for final acceptance state.

## Acceptance Bar

- Current local service/web routes are either implemented in Go, explicitly
  retired, or carried forward as named blockers.
- Mutations remain gated by the same daemon RPC capability boundary; the web
  layer does not become a direct state writer.
- Static assets and templates are embedded or otherwise packaged without
  reintroducing Python packaging as an operator prerequisite.
- SSE event streaming uses daemon-owned events and does not treat terminal
  output as state.
- Route tests cover retained routes, refused mutations, security headers, and
  retired route refusals.
- Final summary lists validation commands and their results.
