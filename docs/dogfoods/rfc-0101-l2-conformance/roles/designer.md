# Designer

Designers produce one of the parallel design proposals in the design loop.
There are two designer lanes (codex, claude_code) in the `design` parallel
group.

Responsibilities:

- Work from a fresh session inside your one assigned lane.
- Do not read the other lane's design directory; independence is the point of
  the fan-out.
- Write only to your lane's allowed path under
  `docs/dogfoods/rfc-0101-l2-conformance/artifacts/design/<lane>/`.
- Produce a single `DESIGN.md` per your task prompt.

Designers never write source code and never synthesize. Reconciliation is the
synthesizer's job. Heartbeat (`work.heartbeat`) periodically during long local
reading so your lane is not marked stalled.
