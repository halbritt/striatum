# RFC 0130 — Native Go Daemon HTTP/SSE MCP and Agent Loop

**Status:** accepted (native MCP slices implemented)
**Scope:** Architecture alignment / Python MCP and CLI control-plane deprecation

> **Provenance:** Formerly numbered `0050-mcp` (it was indexed under that label in
> `docs/rfcs/README.md`). Renumbered to **RFC 0130** on 2026-06-16 to resolve the
> duplicate `0050` prefix it shared with
> [`RFC 0050 — Operator UI rework and provenance honesty`](0050-operator-ui-rework-and-provenance-honesty.md)
> (GitHub issue #320). The operator-UI RFC keeps `0050` and the historical
> `rfc-0050` label; its old design proposals now live under
> `docs/records/_frozen/rfcs/rfc-0050/design/`. Historical operator run records
> and the example fixture under `docs/operator/{plans,workflows,artifacts}/rfc-0050-*`
> and `examples/rfc-0050-http-sse-mcp/` retain their original `rfc-0050-*` slugs as
> frozen run-record provenance (`.check-docs-ignore`).

## Background

Under the original MCP integration (RFC 0036 / RFC 0040), MCP support was provided via a Python-based `stdio` wrapper (`src/striatum/mcp.py`) which proxied requests to the daemon's Unix socket. For interactive agents (like Claude Code) inside the `claude` lane (RFC 0049), a supervisor script was proposed to either proxy JSON directly to `stdin` or use the Python wrapper.

As Striatum moves to a single native Go binary (deprecating the Python CLI and wrappers), the `-agent-loop` was initially ported as a JSON `stdin` proxy. However, this strips the agent of its autonomy as an MCP client. Furthermore, the standard `stdio` MCP transport requires the agent to spawn the MCP server as a subprocess, which adds unnecessary indirection when the daemon is already running.

## Goals

- **Deprecate Python MCP**: Completely remove `src/striatum/mcp.py`.
- **Native Daemon HTTP/SSE**: Build an HTTP MCP server natively into the Go `striatumd` daemon, with Streamable-HTTP-style `POST /mcp` as the primary endpoint and `/mcp/sse` retained as a compatibility alias for SSE clients. This allows agents to connect directly to the running daemon without spawning proxy processes.
- **Autonomous Agents**: Refactor the `-agent-loop` supervisor to act strictly as a PTY manager. It will spawn the agent process, inject a bootstrap prompt containing the daemon's HTTP/SSE endpoint, and let the agent natively use its own MCP client to discover tools (like `work.await_packet`), execute work, and report completions.
- **Retire the CLI control plane**: Stop requiring a human operator or an
  AI operator to drive live workflow state through `striatum` CLI verbs. The
  daemon MCP surface and operator UI become the primary control planes; any
  remaining CLI entry points are temporary bootstrap, diagnostics, or
  compatibility shims until equivalent MCP/UI paths exist.

## Design Sketch

### 1. Go Daemon HTTP/SSE Server
The Go daemon (`striatumd`) will expose an HTTP server (e.g., on a configured local port or a specific socket).
- **Endpoint**: `/mcp` primary, `/mcp/sse` compatibility alias.
- **Protocol**: MCP over loopback HTTP, with SSE stream support for clients that require it.
- **Behavior**: It will natively serve `tools/list` (using `mcp.VisibleTools()`) and `tools/call`. Incoming MCP tool calls will be mapped to daemon JSON-RPC methods, authenticated via the standard capability tokens, and executed in-process.

### 2. `-agent-loop` Redesign
The Go `-agent-loop` subcommand (or the generic lane supervisor) will:
1. Allocate a PTY for the agent.
2. Spawn the agent (e.g., `claude`).
3. Send an initial bootstrap prompt:
   ```
   You are a Striatum lane agent. Connect to the MCP server at http://localhost:<daemon-port>/mcp. Call 'work.await_packet' to register for work.
   ```
4. Monitor the agent process until termination.

It will **no longer** long-poll `await_packet` itself or pipe raw JSON.

## Roadmap

The current implementation work is split into gates so the native daemon MCP
transport can land without waiting for every agent-loop and CLI-retirement
dependency. Each phase should leave the tree shippable and should add tests
that fail closed when the daemon cannot authorize or serve the requested
method.

### Phase A: Native MCP Smoke

- Add the Go daemon HTTP listener and MCP endpoint behind the existing
  local-only daemon boundary. `POST /mcp` is the primary direct request path;
  `/mcp/sse` remains available as an SSE/backcompat alias.
- Support MCP initialization and `tools/list` using the production-visible
  tool set from `go/pkg/mcp`.
- Add a deterministic test MCP client that exercises the endpoint without
  launching a real terminal agent.
- Keep Python MCP and CLI behavior unchanged during this phase.

### Phase B: Read-Only Tool Calls

- Implement `tools/call` for a narrow read method such as `daemon.hello`,
  `daemon.welcome`, `repo.resolve`, or an equivalent status/describe method.
- Enforce capability tokens and return the same denial vocabulary as daemon
  JSON-RPC.
- Record request/audit evidence so MCP calls are observable alongside other
  daemon calls.

### Phase C: First Mutating Tool Call

- Route one low-risk mutation through MCP, preferably a workflow-loop method
  already present in `contracts/daemon_methods.json`.
- Cover success, missing-token, wrong-capability, wrong-repository, and
  unsupported-method cases.
- Prove that `tools/list` hides unsupported production methods instead of
  advertising a call that will fail at runtime.

### Phase D: Work Packet Loop

- Expose the minimal lane loop over MCP: `work.await_packet`, `work.ack`,
  `work.heartbeat`, artifact publication, verdict/complete, and close/release
  behavior as applicable to the current daemon method contract.
- Preserve lease semantics, stale-lease refusal, write-scope policy, and
  author/byline validation exactly as the daemon RPC path does.
- Add an end-to-end fake-agent harness that completes a small workflow entirely
  through MCP `tools/call`.

**Implementation status:** landed for the current minimal packet loop. The
daemon-backed fake-agent harness prepares and starts a one-job workflow,
registers a session, claims with `work.await_packet`, acknowledges and
heartbeats the lease, publishes the required handoff artifact, completes the
job/run, and verifies stale leases refuse later lifecycle mutation.

### Phase E: Agent-Loop Bootstrap

- Refactor `go/pkg/agentloop` into a PTY supervisor that launches the agent
  process and injects only the MCP endpoint, token material, target repository
  identity, and lane instructions.
- The supervisor must not claim work, poll `work.await_packet`, or spoon-feed
  JSON packets to the agent.
- Prove the loop first with a scripted MCP-capable fake agent, then with one
  real interactive agent profile.

### Phase F: CLI Retirement Cutover

- Define CLI retirement as: no live workflow control operation requires a
  human or AI operator to invoke `striatum` CLI verbs.
- Move operator-facing workflow setup, lane selection, run observation,
  recovery, escalation, artifact review, and workflow selection to daemon MCP
  and/or the operator UI.
- Classify remaining CLI commands as bootstrap, diagnostics, or compatibility
  clients of daemon MCP/RPC. Hiding or deleting commands is a later
  deprecation/release decision, not required for the Phase F cutover.
- Update docs, examples, and skills so they teach MCP/UI operation first and
  stop presenting CLI loops as the normal path.

### Phase G: Python MCP Deletion

- Delete `src/striatum/mcp.py` after native Go MCP supports the production
  lane loop and the operator path no longer depends on the Python wrapper.
- Remove Python MCP launch docs, tests, and aliases.
- Keep any Python client code that remains useful for web/UI compatibility
  separate from MCP server authority.

## Dependencies And Non-Blockers

Hard dependencies for the active Operator track:

- Native Go HTTP/SSE MCP transport.
- Capability-token handoff from daemon/supervisor to MCP clients.
- Contract-derived MCP tool visibility.
- Read and mutation dispatch through the daemon method registry.
- A deterministic MCP test client and fake-agent harness.
- PTY bootstrap that tells real agents how to connect without making the
  supervisor the workflow brain.

Do not block the first MCP daemon slices on:

- Full committee-deliberation workflow semantics.
- Full workflow-shape catalog expansion.
- Optional Git/PR authority.
- Sealed apply implementation.
- Deleting every CLI command before MCP/UI parity exists.
- Support for every possible external agent before one real lane proves the
  loop.

## Acceptance Criteria

- Phase A-C accepted: the Go daemon exposes a functional `/mcp` endpoint with
  `/mcp/sse` compatibility,
  supports initialization, `tools/list`, one read call, and one authorized
  mutation through MCP with fail-closed tests.
- Phase D accepted: a fake MCP agent can complete a workflow packet loop
  without invoking the CLI or Python MCP wrapper; stale lease refusal is
  covered through the same MCP path.
- Phase E accepted: an interactive agent can be spawned by the PTY supervisor,
  connect to the daemon SSE endpoint, discover tools, and complete a work
  packet via MCP `tools/call`.
- Phase F accepted: documented operator and agent workflows no longer require
  CLI verbs for live workflow control; remaining CLI commands are explicitly
  classified as bootstrap, diagnostics, or daemon-backed compatibility.
- Phase G accepted: `src/striatum/mcp.py` and its docs/tests/aliases are
  removed after native Go MCP and replacement operator surfaces have parity.
