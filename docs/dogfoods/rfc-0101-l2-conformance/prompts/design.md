# Design — RFC 0101 Layer 2 adapter-conformance harness + lane-env hardening

Produce an **independent** design proposal. Write a single `DESIGN.md` to your
lane's allowed path. Do not coordinate with the other design lane — divergence
is the point.

## The task

Striatum drives terminal AI coding CLIs (`claude_code`, `codex`, `agy`/gemini)
through PTYs, supervised by a daemon that owns authoritative state in
PostgreSQL. Today each adapter fails in its own *silent* way and a CLI version
bump can regress bootstrap/turn behaviour with **no test catching it** — it is
discovered live, mid-run. RFC 0101 Layer 2 makes the lane boundary a
**contract every adapter must satisfy, verified in CI**.

Design **both** halves:

1. **Adapter-conformance harness** — a fixture each adapter must pass
   end-to-end against the *actually installed* CLI: bootstrap submits →
   `tools/list` → `work.claim` → ≥1 `work.heartbeat` → a multi-turn job with
   one interrogation round → artifact `publish` → `work.complete` → loop to
   `no_work`. A CLI bump that breaks bootstrap or the turn loop then **fails
   CI** instead of being discovered live. Promote the landed `claude_code`
   argv-bootstrap fix (#101) to a contract clause.
2. **Lane-env hardening** — deterministically (not by prompt-steering) close:
   - **#76** — `agy` blocks on an interactive CLI survey/feedback prompt.
   - **#85** — `agy` spawns a background MCP-discovery probe and idles past the
     deadline.
   - **#70** — `agy` writes a bearer token into the target repo
     `.gemini/settings.json` (token must stay out of the work tree).

## Hard constraints

- Daemon stays authoritative (D094); lanes stay scratch (D005). No new
  cloud/hosted/external dependency, no telemetry, no durable transcript
  capture.
- The harness runs against the **installed** CLI binary, in CI, via a `make`
  target. Tests need PostgreSQL; the `pgtest` harness auto-provisions
  (`STRIATUM_PG_TEST_URL=postgres:///postgres?host=/var/run/postgresql`).
- `agy`/gemini cannot hold a multi-turn seat yet (#95 turn-driver deferred).
  Say explicitly how `agy` is handled in the conformance matrix (single-shot
  subset + a **non-rotting** skip) without the skip silently going green.
- Go-only. Relevant existing code to ground your design (read it):
  `go/pkg/supervisor/helper.go` (PTY helper, argv bootstrap, emits
  `agent_started`/`progress`/`agent_exited`), the adapter submit-driver code,
  `go/pkg/sessionliveness`, `go/pkg/agentloop`. The conformance harness likely
  lives in a new `go/pkg/adapterconformance` package + a CI binary; pin real
  paths.

## Open questions you must take a position on (defend each)

- **"Bootstrap delivered" detection** — distinguish a real submission from TUI
  redraw/spinner noise. (Strong candidate: define it as the first *protocol*
  event observed at the daemon — e.g. `last_tools_list_at` becoming non-null —
  a daemon-side signal, not a PTY scrape.)
- **Real daemon vs. stub** — does the fixture drive a live/pgtest daemon or a
  stub? What does a stub hide (the #101-class integration regression)? What
  flakiness does a live daemon risk?
- **Failure taxonomy** — name the failure classes the harness emits per failed
  clause (e.g. `BootstrapStall`, `DiscoveryProbeStall`, `AwaitPacketStall`,
  `HeartbeatMissed`, `TurnExitEarly`, `InterrogationIgnored`,
  `TokenLeakInWorkTree`). Make them precise enough to later route an RFC 0101
  Layer 3 recovery action.
- **Preventing the permanent `xfail`** — keep the `agy` multi-turn skip from
  becoming a permanently-green lie (e.g. a version gate, an issue-referenced
  skip message, a promotion mechanism when #95 lands).

## DESIGN.md structure

1. Problem restatement. 2. Proposed design (ordered conformance contract
clauses; harness architecture with real `go/` paths and how each clause is
asserted; failure taxonomy; lane-env hardening mechanism for #76/#85/#70;
`agy` handling). 3. Key decisions (your answers to the open questions + why).
4. Risks (the load-bearing one + mitigation). 5. Test plan (how the harness is
validated and wired into `make`/CI).

Be concrete and reviewable — you will be interrogated on this. Heartbeat
(`work.heartbeat`) periodically during long local reading.
