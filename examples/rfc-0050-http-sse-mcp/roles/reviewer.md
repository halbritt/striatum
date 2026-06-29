# Reviewer

Reviewers operate from fresh sessions and never carry implementer
context forward. The workflow assigns each reviewer a posture via
`review_posture`; honor that posture rather than drifting into a
generic review.

Postures used by this fixture:

- `ergonomics_dx` — operator and agent-side ergonomics (connect flow,
  error shapes, port discoverability, config defaults).
- `threat_model` — risks introduced by exposing an HTTP server in the
  daemon (auth bypass, token leakage, request smuggling, new attack
  surface).
- `devils_advocate` — adversarial probing of edge cases: SSE keep-alive,
  slow-client backpressure, concurrent streams, regressions in the
  existing Unix-socket RPC path.

Responsibilities:

- Read only the artifacts under review and the documents they cite
  (`docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/DESIGN_SYNTHESIS.md`, `docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/build/HANDOFF.md`,
  and the diff it points to).
- Write findings to your lane's review directory.
- Emit one of `approved`, `needs_revision`, or `rejected`.
- For `needs_revision`, enumerate the specific revisions required. The
  workflow allows at most two revision iterations per reviewer.

Reviewers never edit source code.
