# Implementation — RFC 0050 HTTP/SSE MCP

Build the smallest-scope item from
`docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/DESIGN_SYNTHESIS.md`, incorporating any required
revisions from the design review.

Scope (in this run only):

- HTTP listener and `/mcp/sse` endpoint inside `striatumd` (Go).
- `tools/list` and `tools/call` wired through to the existing
  `mcp.Service` — do not reimplement tool discovery or method dispatch.
- Capability-token authentication via `rpc.Authorizer`. Reuse the
  existing token contract; the HTTP layer extracts the token and
  forwards it, it does not validate independently.
- Port configuration: a clean knob (env var or flag) with a localhost
  default, refusing non-loopback binds (precedent: `striatum serve`).
- At least one Go test that exercises the HTTP/SSE endpoint with a real
  authorizer.
- Update `go/cmd/striatumd/main.go` (or appropriate startup glue) to
  start the HTTP server alongside the existing Unix-socket RPC server.

Out of scope (defer to follow-on runs — these are actions 2 and 3 in
the operator brief):

- Agentloop PTY refactor.
- Deleting `src/striatum/mcp.py`.

Touch only the paths in `write_scope.allowed_paths`. Keep the change
reviewable. Resist scope creep.

Hand off:

- Write `docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/build/HANDOFF.md` summarizing what landed, what
  was deferred, the exact verification commands the reviewers should
  run (e.g., `cd go && go test ./...`, plus a curl-against-SSE recipe
  or a Go test invocation), and the port-config flag/env-var name.

When the handoff is complete, emit the `submit-handoff` packet from the
runner's work packet.
