# Design — RFC 0050 HTTP/SSE MCP (one of three parallel lanes)

You are one of three design lanes running in parallel for RFC 0050:
native HTTP/SSE MCP server in `striatumd`, plus the agentloop becoming a
pure PTY supervisor.

**Read first:** `docs/rfcs/0130-go-daemon-http-sse-mcp.md`. Skim
`docs/operator/BRIEF.md` for the current state. Inspect
`go/pkg/mcp/tools.go`, `go/pkg/mcp/capabilities.go`,
`go/pkg/rpc/server.go`, and `go/pkg/agentloop/loop.go` to understand the
existing shape — your design should reuse `mcp.Service.ToolsList` /
`mcp.Service.ToolsCall` rather than reimplement them.

Do **not** coordinate with the other lanes — independent perspectives
are the point.

Produce a single `DESIGN.md` inside your lane's allowed write path
(`docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/design/<lane>/DESIGN.md`). Cover at minimum:

- **HTTP listener** — bind address, port choice/config (env var, flag,
  `.striatum/` registry), localhost-only enforcement (per `serve`
  precedent), TLS posture.
- **SSE framing** — endpoint shape (`/mcp/sse`), session/connection
  lifecycle, keep-alive, framing for `tools/list` vs `tools/call`
  responses, streaming-vs-unary trade-off for tools/call.
- **Auth path** — how the capability token is presented (header? query?
  initial SSE message?), how it flows into `rpc.Authorizer`.
- **Tools mapping** — how `tools/list` and `tools/call` are wired to the
  existing `mcp.Service`; error shape on `method_unknown` /
  `capability_denied`.
- **Agentloop bootstrap prompt shape** — what the new PTY supervisor
  injects into the agent's stdin so the agent can auto-connect (URL,
  token, repository id).
- **Two or three alternatives considered** and why your choice wins.
- **Risks, unknowns, and a "what could go wrong" section** — slow-client
  backpressure, port collisions, token leakage in process listings,
  regressions in the existing Unix-socket RPC path.
- **Rollout sketch** — what lands first (the HTTP/SSE endpoint and a
  test), what defers (agentloop PTY refactor, `src/striatum/mcp.py`
  deletion).

Keep the document focused. Do not write code. Do not edit files outside
your lane's allowed paths.

When the design is complete, emit the `submit-handoff` packet that the
runner provided in your work packet's `commands` block.
