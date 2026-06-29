# RFC 0050 design synthesis

You have read all three design proposals under
`docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/design/`. Produce a single buildable synthesis at
`docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/DESIGN_SYNTHESIS.md`.

The synthesis should:

- Pick **concrete** approaches, not a menu of options:
  - HTTP listener: bind, port-config knob, localhost enforcement.
  - SSE framing: endpoint path, lifecycle, keep-alive interval.
  - Capability-token transport: header name and value shape.
  - tools/list and tools/call wiring to `mcp.Service`.
  - Error/audit envelope for HTTP errors vs MCP-layer errors.
  - Agentloop bootstrap prompt template (action 2 follow-on; record the
    shape even if implementation defers).
- Cite each input design by lane (codex / claude_code / gemini) for the
  ideas you carry forward and the ones you reject.
- Call out any unresolved contradictions and explain how the build
  phase should handle them.
- List the smallest implementable scope a single implementer can land
  next. Anchor this to: a working `/mcp/sse` endpoint + tools/list +
  tools/call + capability auth + one Go test, port config wired
  through. Defer agentloop PTY refactor and `src/striatum/mcp.py`
  deletion to follow-on runs.

Do not edit files under `docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/design/`. Do not write source
code.

When the synthesis is complete, emit the `submit-handoff` packet that
the runner provided.
