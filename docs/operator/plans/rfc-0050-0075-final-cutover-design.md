---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_rfc-0050-0075-final-cutover-design"
scope_kind: "rfc"
scope_ref: "docs/rfcs/0050-go-daemon-http-sse-mcp.md + docs/rfcs/0075-tmux-observable-mcp-agent-sessions.md"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Final cutover design accepted: MCP is the lane-agent control plane, local web UI is the human/operator control plane, and CLI remains compatibility/bootstrap/diagnostics only."
supersedes: "docs/operator/plans/rfc-0050-cli-retirement-cutover.md"
retrieval_priority: "high"
---

# RFC 0050 / RFC 0075 Final Cutover Design
author: coordinator-codex-gpt-5-001

## Outcome

The cutover design is complete and recorded in
`docs/operator/artifacts/rfc-0050-0075-final-cutover-design/design/CUTOVER_DESIGN.md`.

The terminal cutover rule is:

- lane-agent live workflow control is MCP-first and does not require CLI;
- human/operator live workflow control is local-web-UI-first and does not
  require CLI;
- CLI verbs may remain as bootstrap, diagnostics, compatibility, and
  scriptable escape hatches, but docs and skills must not teach them as the
  normal live-control path.

## Workflow

Runnable scaffold:
`docs/operator/workflows/rfc-0050-0075-final-cutover-design.json`.
The design workflow completed as
`run_4a5eb33b0d6b037e9f62a0335d04b349`.

## Guardrails

- Do not reintroduce Python MCP or stdio wrappers.
- Do not parse tmux pane text or terminal output as workflow state.
- Do not delete bootstrap or diagnostics CLI commands.
- Do not add hosted services, provider SDKs, telemetry, transcript capture, or
  external persistence.
