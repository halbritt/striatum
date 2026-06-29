---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
---

# RFC 0103 W3/W4 — Floor Evidence

author: presenter-claude-opus-4.8-002

Acceptance evidence for the RFC 0103 floor. Each item below is a closed,
dogfood-proven behavior exercised through the production daemon handlers.

- **W4 (#131) — interrogation against a retired target.** `interrogation.open`
  now returns a structured `interrogation_unavailable` signal for a retired
  panel target, so a replacement reviewer proceeds on the already-published
  artifact instead of wedging on `target_unavailable`.
- **W3 (#125) — `work.ack` is non-substitutable.** A `session.report` issued
  while a packet is claimed-but-unacked is flagged, not accepted; the
  acknowledgement step cannot be silently bypassed by a report.
- **W3 (#141) — daemon-restart survival.** A supervised lane survives a
  `systemctl restart striatumd` mid-run (`KillMode=process` plus
  `context.WithoutCancel`); the repo-write job completes through the production
  handlers rather than dying with the daemon.

Together these establish the RFC 0103 floor: interrogation degrades gracefully,
acknowledgement is mandatory, and supervised work is restart-durable.

## Limitations

- **#134 remains open** as a distinct interrogator-session issue; W4 closes the
  retired-*target* degradation path, not the broader interrogator-session lifecycle.
- **codex-lane MCP config is endpoint-stale across restarts** (`doctor` reports the
  codex config as stale even while the live endpoint is reachable), so codex lanes
  may need a config refresh after a daemon restart.
- This is the **floor**, not the production-grade fault matrix: it proves a handful
  of closed behaviors end-to-end, not exhaustive coverage of every failure mode.
