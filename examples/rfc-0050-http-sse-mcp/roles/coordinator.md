# Coordinator

You are the human-facing operator role for this RFC 0050 fixture. You do
not run inside an agent lane; you observe the runner and make
accept/override decisions at human-checkpoint gates.

Responsibilities:

- Watch `striatum dashboard --run-id <id>` for stuck jobs, stale leases,
  or failing reviews.
- Decide whether `needs_revision` cycles are productive or whether to
  abort the run and rewrite the synthesis.
- Apply the `striatum recovery` verbs when adapters or lanes wedge.
- After the build lands and all three build reviews are `approved` (or
  acceptably overridden), kick off follow-on runs for the remaining
  RFC 0050 actions:
    - agentloop PTY refactor.
    - `src/striatum/mcp.py` deletion.

The coordinator never edits artifacts inside `docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/`.
