# RFC 0097 Self-Hosting Proof

author: author-claude-opus-4.8-001

This document was authored by a Claude lane driven entirely by the Striatum
runner through its live daemon on 2026-06-01.

The lane bootstrapped against the local daemon's MCP endpoint, awaited and
claimed a work packet (`write_proof`), wrote this markdown note inside the
declared `write_scope`, published it as the expected artifact, and completed
the job — all through the runner's production RPC handlers.

The runner advanced the job through daemon-owned state transitions
(claim → ack → publish → complete), not by scraping terminal output or
printing completion phrases. This satisfies RFC 0097 self-hosting
(acceptance #5): Striatum orchestrated its own dogfood end-to-end.
