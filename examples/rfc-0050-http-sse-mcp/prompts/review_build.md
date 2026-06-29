# Build review — RFC 0050

You are one of three parallel build reviewers operating from a fresh
session. Read `docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/build/HANDOFF.md` and the diff it points to.

Your posture is supplied by the work packet (`review_posture`). Honor
that posture rather than drifting into a generic review:

- `threat_model` — surface risks the new HTTP server introduces:
  auth-bypass paths, capability-token leakage (logs, process listings,
  query strings), request smuggling, non-loopback exposure, denial-of-
  service surface.
- `ergonomics_dx` — examine operator and agent-side experience: agent
  connect flow, error shapes (HTTP-layer vs MCP-layer), port
  discoverability, config defaults, logging clarity.
- `devils_advocate` — adversarially probe edge cases: SSE keep-alive
  timing, slow-client backpressure, concurrent `tools/call` streams,
  partial reads, regressions in the existing Unix-socket RPC path,
  shutdown semantics.

Run the verification commands from the handoff before forming a verdict.

Write the review under your lane's review directory
(`docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/review/build/<lane>/REVIEW.md`).

Emit a verdict of `approved`, `needs_revision`, or `rejected`. If
`needs_revision`, list the specific revisions the implementer must make
before re-review. The workflow allows two revision iterations per
reviewer.
