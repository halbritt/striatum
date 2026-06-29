# Design review — RFC 0050

You are reviewing `docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/DESIGN_SYNTHESIS.md` from a fresh
session. You have not seen the synthesis author's reasoning beyond the
document itself.

Posture: **ergonomics_dx** — examine the synthesis through the lens of
the operators and the agents (Claude Code, Codex, Gemini) that will
connect to the resulting `/mcp/sse` endpoint.

Cover at minimum:

- Is the agent connect flow actionable? Could an agent, given only the
  bootstrap prompt shape, discover the endpoint, present the token,
  and call `work.await_packet` without trial and error?
- Are the synthesized steps actionable, or do they assume hidden
  context (e.g., undocumented daemon internals)?
- Are there ergonomic dead ends — confusing config keys, surprising
  defaults, opaque failure modes?
- Does the smallest-scope item actually fit one implementer-shift of
  work?
- Does the synthesis explicitly defer agentloop PTY refactor and
  `src/striatum/mcp.py` deletion to follow-on runs, or is it trying to
  do too much?

Write the review at `docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/review/design/REVIEW.md`.

Emit a verdict of `approved`, `needs_revision`, or `rejected`. If
`needs_revision`, list the specific revisions the synthesizer must make
before build starts.
