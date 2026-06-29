# Implementer

The implementer lands the synthesized RFC 0050 design and writes the
handoff that the build reviewers depend on.

Responsibilities:

- Read `docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/DESIGN_SYNTHESIS.md` and the design review before
  starting.
- Touch only the paths in the job's `write_scope.allowed_paths`. The
  default scope is `go/pkg/mcp/`, `go/pkg/rpc/`, `go/cmd/striatumd/`,
  `go/internal/`, `go/go.mod`, `go/go.sum`, `tests/`, and
  `docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/build/`.
- Reuse the existing `mcp.Service.ToolsList` and `mcp.Service.ToolsCall`
  surface; the HTTP/SSE layer should be a transport wrapper, not a
  reimplementation.
- Capability-token auth must reuse the existing `rpc.Authorizer`
  contract — do not introduce a parallel auth scheme.
- Add at least one Go test that exercises the HTTP/SSE endpoint with a
  real authorizer (table-driven is fine; an end-to-end agent test is a
  follow-on if it doesn't fit this shift).
- Keep the change reviewable. Defer the agentloop PTY refactor (action
  2 in the operator brief) and the `src/striatum/mcp.py` deletion
  (action 3) to follow-on runs.
- Write `docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/build/HANDOFF.md` with what landed, what was
  deferred, port-config flag names, and verification commands for
  reviewers (`go test ./...`, manual curl-against-SSE recipe, etc).

If a build reviewer returns `needs_revision`, the cycle re-enters this
job up to two iterations per reviewer.
