# Author the RFC 0103 Floor-Evidence Synthesis

You are a Claude presenter lane driven by the Striatum runner through its live
daemon. Author one short markdown synthesis note and publish it through your
Striatum tools (advance state through the daemon — never by printing phrases).

Artifact path (exact, from your work packet `expected_artifacts`):
`docs/dogfoods/rfc-0103-floor/artifacts/FLOOR_SYNTHESIS.md`.

## First attempt

Write a short note (~12–20 lines) titled "RFC 0103 W3/W4 — Floor Evidence". Near
the top include the lowercase byline `author: presenter-claude-opus-4-8-1`.
Summarize, factually and briefly:

- **W4 (#131):** `interrogation.open` now returns a structured
  `interrogation_unavailable` signal for a retired panel target, so a replacement
  reviewer proceeds on the published artifact instead of wedging on
  `target_unavailable`.
- **W3 (#125):** `work.ack` is non-substitutable — a `session.report` while a
  packet is claimed-but-unacked is flagged, not accepted.
- **W3 (#141):** a supervised lane survives a `systemctl restart striatumd`
  mid-run (`KillMode=process` + `context.WithoutCancel`); the repo-write job
  completes through the production handlers.

Do NOT include a "Limitations" section on this first attempt. Stay inside your
`write_scope.allowed_paths`. Publish the artifact and complete the job.

## Revision attempt (only if you receive a `needs_revision` review)

If the runner gives you a fresh packet for a revision, add a short
`## Limitations` section (2–4 bullets — e.g. that #134 remains open as a distinct
interrogator-session issue, that codex-lane MCP config is endpoint-stale across
restarts, and that this is the floor, not the production-grade fault matrix), then
re-publish and complete. Keep everything else intact.
