---
schema_version: "striatum.handoff.v1"
artifact_kind: "handoff"
---

author: designer-claude-code-001

# RFC 0050 — HTTP/SSE MCP Server: Claude-Code Lane Design

This is the `claude_code` lane's independent design proposal for RFC 0050.
The other two lanes (`codex`, `gemini`) are producing parallel proposals; a
later synthesis job will reconcile them.

The thesis of this design is: **lean on what we already have.** The Go
daemon already has a working in-process MCP service (`mcp.Service` in
`go/pkg/mcp/`) and a working RPC server with capability-token
authorization. The job is to expose them over HTTP/SSE on loopback,
discoverably, without inventing parallel auth or parallel handler paths.

## 1. HTTP listener

### Bind address and transport

- **Bind**: `127.0.0.1` only. Hard-coded — not configurable. The daemon
  is a local-first single-user process per `docs/SPEC.md`; binding any
  other interface (including `0.0.0.0` or a non-loopback IPv6 address)
  is rejected at startup with `daemon_config_invalid`. This mirrors the
  Unix-socket precedent: the existing transport is already restricted
  to a per-user runtime dir under `XDG_RUNTIME_DIR`.
- **No TLS.** Loopback-only HTTP, no certs, no proxy in front. Adding
  TLS would invite users to terminate it remotely and break the
  local-first invariant; if a future use case needs remote access it
  should land as an explicit `serve --tunnel` story, not as silently
  available TLS.
- **Coexists with Unix socket.** The HTTP listener is additive. The
  Unix-socket RPC at `XDG_RUNTIME_DIR/striatum/daemon-go.sock` keeps
  serving the CLI; nothing about its shape changes. The HTTP listener
  is a second goroutine off the same `*rpc.Server`.

### Port choice

Three options were considered:

1. **Fixed default port (e.g. 4870).** Simple, but collides with other
   striatum instances on the same machine (multiple repos, multiple
   users, dev/prod side-by-side). A second `striatumd` fails to start
   for an opaque reason. Rejected.
2. **Configurable port via flag / env var (`--mcp-port`,
   `STRIATUM_MCP_PORT`).** Familiar shape, but pushes a port-management
   problem onto the operator and onto every spawning supervisor.
   Rejected as the *primary* mechanism.
3. **Ephemeral port chosen by the OS, advertised via
   `.striatum/daemon-mcp.json`.** No collisions, no operator config in
   the common case, supervisor reads the file to bootstrap the agent.
   **Chosen.**

So the design is:

- By default, listen on `127.0.0.1:0`. After `Listen` returns, capture
  the bound port and write a small JSON document to
  `.striatum/daemon-mcp.json`:

  ```json
  {
    "schema_version": 1,
    "url": "http://127.0.0.1:54871/mcp",
    "sse_url": "http://127.0.0.1:54871/mcp/sse",
    "pid": 4166705,
    "started_at": "2026-05-20T14:42:11Z"
  }
  ```

  The file is written atomically (write to `.tmp`, `rename`) and
  removed on graceful shutdown (alongside the existing daemon pidfile
  cleanup in `go/cmd/striatumd/main.go`).
- `STRIATUM_MCP_PORT` / `--mcp-port` is honored as an escape hatch and
  short-circuits the ephemeral choice. Set it for tests or for
  pinned-port deployments. We never *publish* a default port value in
  docs to discourage operators from typing one into a config file.
- `STRIATUM_MCP_DISABLE=1` or `--mcp-disable` skips the listener
  entirely. This is the kill switch if the HTTP path regresses; the
  Unix socket stays up.

### Lifecycle inside `main.go`

The HTTP listener slots in between `listener, err := rpc.ListenUnix(...)`
and `server.Serve(...)`. Both listeners share the same `context.Context`
and the same shutdown path. The unified shutdown hook
(`shutdownOnce.Do(cancel)`) closes both listeners. We get `Cancel`
semantics for free.

The MCP HTTP listener lives in a new package: `go/pkg/mcphttp/`. The
existing `go/pkg/mcp/` package stays a pure protocol package — no
networking. That separation matters because `mcp` is already covered by
`capabilities_test.go` for tool-visibility logic; we should not pollute
it with HTTP framing.

## 2. SSE framing

There are two MCP HTTP transport variants currently in the spec:

1. **Legacy "HTTP+SSE" transport**: two endpoints — a `GET /mcp/sse`
   that holds an open server-sent-events stream, and a `POST
   /mcp/messages?session_id=...` for client-to-server JSON-RPC frames.
   The server's first SSE event is an `endpoint` event that tells the
   client where to POST to.
2. **Newer "Streamable HTTP" transport**: a single endpoint, `POST
   /mcp`. The client posts a single JSON-RPC frame; the server may
   reply with a unary JSON response *or* an SSE stream, depending on
   `Accept` headers and whether the call wants streaming (notifications,
   progress).

**Choice: implement the legacy HTTP+SSE transport first**, with the
newer Streamable HTTP transport as a deferred follow-up. Reasons:

- Claude Code, Codex CLI, and other current MCP clients still default
  to legacy HTTP+SSE; streamable HTTP support is patchier across the
  client matrix that Striatum cares about as of 2026-05.
- The legacy transport's two-endpoint shape maps cleanly onto our
  existing handler model: SSE is server-out (mostly heartbeats and
  responses keyed by request id), POST is client-in and unary. We don't
  need to invent a streaming response path for `tools/call` on day one.
- Streamable HTTP can be added later as `POST /mcp` without disturbing
  the legacy endpoints — they coexist by URL.

### Endpoints

```
GET  /mcp/sse                      # SSE stream, server -> client
POST /mcp/messages?session_id=...  # client -> server JSON-RPC frames
GET  /mcp/health                   # liveness, returns {"ok": true, "schema": N}
```

### Session lifecycle

- `GET /mcp/sse` opens an SSE stream. The server immediately sends an
  `event: endpoint` frame with `data: /mcp/messages?session_id=<id>`.
  The session id is a fresh ULID minted per connection; it's *not* a
  Striatum session id. (We rename internally to avoid the collision.
  Call it `mcp_connection_id` everywhere except on the wire where MCP
  spec mandates `session_id`.)
- Subsequent `POST /mcp/messages?session_id=<id>` requests carry a
  single JSON-RPC frame in the body. The server processes it and emits
  the response as a `message` SSE event on the matching stream. Posts
  return `202 Accepted` once the response has been enqueued.
- Keep-alive: SSE comment frames (`: keepalive\n\n`) every 15s. This
  matches the conservative end of the MCP client reconnect timers we've
  seen in the wild.
- Reconnect: the server does not retain undelivered messages across
  reconnects in V1. If a client disconnects, in-flight `tools/call`
  responses are dropped and the operation is treated as best-effort.
  (Striatum tool calls go through the existing RPC server, which audits
  the call regardless — the audit trail does not depend on the client
  staying connected.) A deferred follow-up may add `Last-Event-ID`
  replay.
- Connection cap: 8 concurrent SSE connections per repository scope by
  default, configurable via `STRIATUM_MCP_MAX_CONNS`. New connections
  past the cap get `503 Service Unavailable`.

### Framing for `tools/list` vs `tools/call`

- `tools/list` is unary. The response is a single `message` event with
  a JSON-RPC `result` payload. We do not stream the tool list — it's
  small (low hundreds of methods at most) and clients want it whole.
- `tools/call` is unary in V1. We do not stream progress notifications
  for in-flight calls; if a future tool needs progress, that is the
  trigger to add the Streamable HTTP transport. Single response message
  with `structuredContent`, matching the existing `mcp.Service.ToolsCall`
  return shape verbatim.

This makes the SSE stream genuinely simple: it carries `endpoint`,
`message`, and `: keepalive` frames. Nothing else.

## 3. Auth path

The daemon already has capability tokens. We do not invent a second
auth scheme.

### Token presentation

- **`GET /mcp/sse`**: `Authorization: Bearer <token>` header required.
  No token, or invalid token, returns `401 Unauthorized` with a
  JSON-RPC-shaped error body so clients with poor SSE error handling
  still see a structured failure.
- **`POST /mcp/messages`**: same `Authorization: Bearer <token>`
  header, on every POST. The header is the source of truth on each
  request — we re-authorize per JSON-RPC frame against
  `rpc.Authorizer`, identical to the existing Unix-socket path.
- **Token in URL? No.** Tokens in `?token=` query strings leak into
  proxy logs, browser histories, and `ps`-style snapshots. The
  `Authorization` header is the only accepted location.

The token value is the same capability token already issued by
`admin.BootstrapRuntimeTokenIfNeeded`. We do not mint MCP-specific
tokens; the existing scope (repository id binding, capability check)
already covers what we need.

### Flow into `rpc.Authorizer`

The HTTP handler:

1. Parses the `Authorization` header, strips the `Bearer ` prefix.
2. Decodes the JSON-RPC frame from the POST body.
3. Builds an `rpc.Envelope` with `CapabilityToken: <bearer>`,
   `Method: tool.Name`, `Params: tool.arguments`,
   `RequestID: jsonrpc.id`.
4. Calls `mcp.Service.ToolsCall(ctx, name, arguments, token, requestID)`.

`mcp.Service.ToolsCall` already does the rest: it calls
`s.RPC.HandleWithoutHandshake(ctx, envelope, "mcp")`, which runs the
authorizer (`PostgresAuthorizer` in production), audits the call, and
returns a structured response with `ok / error / data` keys.

### Tokens never appear in process listings

The supervisor never passes the token as an argv flag. The agent reads
it from one of:

- `STRIATUM_MCP_TOKEN` environment variable (set by the supervisor for
  the child), or
- a file path advertised via `STRIATUM_MCP_TOKEN_FILE`.

Both are documented; the supervisor uses the env var by default because
it doesn't survive a `ps` snapshot. (We tolerate the small ambient-env
risk because `/proc/<pid>/environ` is already readable only by the
same user, identical to how the existing runtime token file is
protected.)

## 4. Tools mapping

This is the cheapest part of the design because the work is already
done.

### `tools/list`

```go
func (h *Handler) handleToolsList(ctx context.Context, req jsonrpcRequest, token string) jsonrpcResponse {
    params := req.Params
    if _, ok := params["repository_id"]; !ok {
        params["repository_id"] = h.repositoryID(token) // see "Repository scope"
    }
    out := h.mcp.ToolsList(ctx, params, token)
    return jsonrpcResponse{ID: req.ID, Result: out}
}
```

The `mcp.Service.ToolsList` call already filters by capability and
already hides internal/handshake methods. We pass through.

### `tools/call`

```go
func (h *Handler) handleToolsCall(ctx context.Context, req jsonrpcRequest, token string) jsonrpcResponse {
    name, _ := req.Params["name"].(string)
    arguments, _ := req.Params["arguments"].(map[string]any)
    out := h.mcp.ToolsCall(ctx, name, arguments, token, req.ID)
    return jsonrpcResponse{ID: req.ID, Result: out}
}
```

The MCP-shaped `out` already contains `isError: true/false`,
`structuredContent`, and `content`. No remapping is needed.

### Error shape

We adopt JSON-RPC error codes for transport-layer failures and reserve
the MCP `isError` flag for tool-layer failures:

| Failure | HTTP | JSON-RPC code | MCP isError |
|---|---|---|---|
| Missing/invalid bearer token | 401 | -32001 (custom: auth_required) | n/a |
| Unknown method (`tools/foo`) | 200 | -32601 (method not found) | n/a |
| Daemon-side `method_unknown` for the called tool | 200 | n/a | true (code=`method_unknown`) |
| Daemon-side `capability_denied` | 200 | n/a | true (code=`capability_denied`) |
| Daemon-side `not_implemented` | 200 | n/a | true (code=`not_implemented`) |
| Malformed JSON body | 400 | -32700 (parse error) | n/a |
| Missing `session_id` query param | 400 | -32602 (invalid params) | n/a |

The split is important. MCP clients distinguish between "the transport
failed" (retry the connection) and "the tool returned an error"
(surface it to the user). Mapping everything to HTTP 5xx, or
everything to MCP `isError`, would confuse clients in opposite
directions.

### Repository scope handling

Most daemon methods are repository-scoped. The HTTP transport handles
this in two layers:

1. **Header-bound default**: When the SSE connection opens, the
   `Authorization` header is paired with a single `repository_id` —
   whichever repository the capability token was minted for. The handler
   caches that `repository_id` on the connection and uses it as the
   default for any `tools/call` that omits `repository_id`.
2. **Per-call override**: If the agent passes `repository_id` in
   `arguments`, the explicit value wins. (Useful for tools like
   `cross_repo.list` that legitimately span repositories.)

This makes the common case ergonomic (the agent doesn't have to thread
`repository_id` through every call), without losing the ability to
target a non-default repository when needed.

## 5. Agentloop bootstrap prompt shape

The new `-agent-loop` (or its successor, the lane supervisor) becomes
a PTY manager. It:

1. Reads `.striatum/daemon-mcp.json` to learn `sse_url`.
2. Mints (or re-reads) the capability token, asserts it's still valid.
3. Allocates a PTY and spawns the agent (`claude`, `codex`, `gemini`,
   etc).
4. Sets these environment variables on the child:
   - `STRIATUM_MCP_URL` → e.g. `http://127.0.0.1:54871/mcp/sse`
   - `STRIATUM_MCP_TOKEN` → the capability token
   - `STRIATUM_REPOSITORY_ID` → the registered repo id
   - `STRIATUM_RUN_ID` → the active run id (already supported)
   - `STRIATUM_SESSION_ID` → the agent session id (already supported)
5. Writes a bootstrap prompt to the PTY before relinquishing control to
   the user / agent loop.

### Bootstrap prompt template

```
You are a Striatum lane agent on the {lane_id} lane in run {run_id}.

The Striatum daemon exposes an MCP server at the URL in the
STRIATUM_MCP_URL environment variable, authenticated with the bearer
token in STRIATUM_MCP_TOKEN.

To start, connect to that MCP server, list the available tools, and
call `work.await_packet` to register for work. The repository id for
this run is in STRIATUM_REPOSITORY_ID.

Stay inside your work packet's allowed_paths. Use the CLI verbs in the
packet's `commands` block verbatim. Do not advance state by printing
phrases — advance it by calling the tools the daemon exposes.
```

Three design choices in the prompt:

- **URL and token go in env vars, not the prompt body.** The prompt is
  visible to anything that reads PTY scrollback (tmux capture-pane,
  log capture); env vars are not. This is the same threat model that
  drove the existing `STRIATUM_REPO`/`STRIATUM_SESSION_ID` env-var
  contract in `agentloop.Run`.
- **The prompt names tools by name, not by behavior.** The agent should
  call `tools/list` to discover the real surface, not believe the
  prompt's description. Anchoring on a specific tool (`work.await_packet`)
  is a starting point, not a contract.
- **The prompt repeats the AGENTS.md guardrails** about staying in
  `allowed_paths` and using CLI verbs verbatim. Agents shouldn't have to
  rediscover those from cold context.

The supervisor does not log the prompt body, does not log the env, and
does not parse the agent's PTY output for workflow state — per
AGENTS.md and the supervisor contract (DEVNULL on supervisors).

## 6. Alternatives considered

### Alternative A: stay on `stdio` MCP, spawn the daemon as a subprocess of each agent

This is the standard MCP `stdio` transport. The agent (Claude Code,
Codex, etc) launches an `mcp-server` binary as a child process and
pipes JSON-RPC over its stdio.

- ✅ Standard MCP transport, every client supports it.
- ✅ No HTTP listener, no port management, no auth layer to design.
- ❌ Each agent gets its own daemon subprocess, or each agent talks to
   a thin proxy that proxies to the daemon. Either way we get a fork
   of the JSON-RPC path (Python wrapper redux, just in Go).
- ❌ The daemon is already running. Adding a subprocess proxy *for the
   client to spawn* is exactly the indirection RFC 0050's Goals
   section calls out as wasteful.

Rejected. It's a step sideways from the current Python wrapper, not
forward.

### Alternative B: Streamable HTTP transport as the V1 endpoint

Land the newer MCP transport (`POST /mcp` with optional SSE response)
directly, skip the legacy SSE+POST split.

- ✅ Newer, simpler in shape (one endpoint).
- ✅ Lays the groundwork for progress notifications on long-running
  tool calls.
- ❌ Client support is patchier; we'd risk shipping a server that the
  agents we actually run in Striatum (Claude Code's MCP client, Codex
  CLI's MCP client as of early 2026) can't speak.
- ❌ Doesn't reduce code complexity meaningfully — we'd still need
  request demuxing, just on a single endpoint instead of two.

Defer. We'll add it once at least two of the agent clients in the lane
matrix can use it.

### Alternative C (chosen): legacy HTTP+SSE transport, native in the Go daemon

The shape this design recommends. Smallest piece of new code, broadest
client compatibility, no parallel auth path, no parallel handler path.
The trade-off is the legacy transport's slightly awkward two-endpoint
shape — but that is paid in 200 lines of HTTP handler, not in a
recurring tax on the rest of the daemon.

## 7. Risks, unknowns, what could go wrong

### Slow-client backpressure

If an MCP client opens an SSE stream and then stops reading, the
server's `http.ResponseWriter` blocks on `Write`. A blocked SSE
goroutine pins memory and one of our 8 connection slots.

Mitigation:

- Each SSE connection runs on its own goroutine with a buffered
  outbound channel (capacity 32 messages). Writes to the channel are
  non-blocking from the request-handling side; if the channel is full
  the connection is force-closed with a `connection_too_slow` event.
- Per-connection write timeout: 5s on the underlying `Write` call. If
  the OS-level write doesn't drain in 5s the connection is closed.
- The audit trail for the tool call is unaffected — `mcp.Service.ToolsCall`
  has already recorded it via the RPC server's `AuditRecorder` by the
  time we try to write the response.

### Port collisions and leftover files

`.striatum/daemon-mcp.json` is the discovery file. If the daemon dies
without cleanup, a stale file points at a port that nothing is bound
to.

Mitigation:

- The file includes the daemon PID. The supervisor checks
  `kill -0 <pid>` (or platform equivalent) before trusting the URL.
  If the PID is dead, the supervisor refuses to spawn the agent and
  surfaces a clear error: "stale .striatum/daemon-mcp.json — restart
  striatumd".
- The daemon's defer cleanup removes the file alongside the pidfile.
- A boot-time check: if the file exists at startup and the PID is alive
  and belongs to another striatumd, refuse to start. If the PID is
  dead, take over and rewrite the file.

### Token leakage in process listings

Discussed in §3. The token never appears in argv. It appears in:

- `Authorization` headers (server-side, never logged at info level)
- `STRIATUM_MCP_TOKEN` env var (`/proc/<pid>/environ`, user-readable
  only)
- The existing runtime-token file at `~/.local/share/striatum/.runtime-token`

We do not log request bodies, ever. We log method names and outcomes
only — matching the existing `rpc.AuditRecorder` posture.

### Regression in the existing Unix-socket RPC path

The HTTP transport is purely additive: a new package
(`go/pkg/mcphttp/`), a new listener, no edits to handlers. The risk
isn't behavioral drift in handlers; it's resource contention.

Mitigation:

- A `go test` integration test that exercises both transports
  concurrently against the same `*rpc.Server` instance: 20 concurrent
  Unix-socket `daemon.hello` round-trips against 20 concurrent
  `tools/call` invocations, asserting both progress and the audit
  recorder sees 40 distinct records.
- The HTTP listener does not call `Handle` (which requires handshake),
  it calls `HandleWithoutHandshake`. The handshake state machine
  remains untouched. The MCP path is explicitly *not* a Unix-socket
  client.

### `repository_id` ambiguity

If the capability token isn't repository-scoped (which is the case for
some admin tokens), the default `repository_id` is empty, and any
tool that needs it errors with `repo_not_registered`.

Mitigation:

- `tools/list` filters by capability; admin-token clients see
  cross-repo tools but get a clear `repo_not_registered` error if they
  try to call a repo-scoped tool without arguments.
- The bootstrap prompt is explicit: "the repository id for this run is
  in STRIATUM_REPOSITORY_ID". Agents that follow the prompt won't hit
  this edge.

### MCP protocol version drift

The MCP spec is moving. The Streamable HTTP transport is the future;
the legacy SSE transport is supported but deprecated. We may have to
implement Streamable HTTP within a year.

Mitigation:

- Add a single `protocol_version` field to `.striatum/daemon-mcp.json`
  so clients can detect the transport. Today: `"legacy_sse_2024_11"`.
- Keep `mcphttp` small and avoid leaking transport assumptions into the
  rest of `striatumd`. When Streamable HTTP lands, we add a new handler
  on `POST /mcp` and bump the version field.

### Connection cap is opinionated

8 concurrent SSE connections may be too many or too few depending on
how aggressively orchestration tools open extras. We should expose the
cap as a config but pick a number that errs low.

Mitigation:

- `STRIATUM_MCP_MAX_CONNS` documented in the operator brief and the
  doctor output.
- `doctor` reports the current connection count so operators can spot
  exhaustion.

## 8. Rollout sketch

The work is sliced into three landings.

### Slice 1: HTTP/SSE endpoint live, agentloop unchanged

What lands:

- New package `go/pkg/mcphttp/` with handlers, session table,
  keep-alive, JSON-RPC framing, and the legacy SSE transport.
- New goroutine in `go/cmd/striatumd/main.go` that binds an
  `httptest.NewServer`-style HTTP server on `127.0.0.1:0`, writes
  `.striatum/daemon-mcp.json`, and shuts down with the rest of the
  daemon.
- An integration test under `tests/` (or `go/pkg/mcphttp/`) that
  drives a real `tools/list` and `tools/call` round trip with a
  capability token.
- Operator-visible: `striatum doctor` reports the SSE URL and
  connection count.
- The existing `agentloop.Run` is untouched and still works.

This slice is enough to satisfy two of the three RFC 0050 acceptance
criteria: a functional `/mcp/sse` endpoint, and an interactive agent
can theoretically connect to it.

### Slice 2: PTY-based agentloop refactor

What lands:

- `agentloop.Run` becomes a PTY supervisor. Allocates a PTY, spawns the
  agent command, writes the bootstrap prompt, sets env, waits for
  termination.
- It no longer calls `work.await_packet`, no longer pipes JSON to
  stdin, no longer marshals packets.
- New `go/pkg/agentloop/pty.go` (or similar) with PTY allocation.
- Integration test: spawn a fake agent script that connects to the SSE
  endpoint and acknowledges a packet, assert the workflow completes.

This slice covers acceptance criterion 3 (interactive agent connects
and completes a packet via MCP tools/call).

### Slice 3: delete `src/striatum/mcp.py` and decouple Python

What lands:

- Remove `src/striatum/mcp.py` and any callers.
- Remove Python-side MCP wrapper assets and skills entries.
- Update docs/POSTGRES_TRANSITION.md and operator brief to remove
  references.
- Verify `make test` and `make smoke` are green without the Python
  MCP wrapper.

This slice closes acceptance criterion 1. It is straightforward but
high-touch, hence last — we want slices 1 and 2 to bake first.

### What defers explicitly

- **Streamable HTTP transport.** Add later as `POST /mcp` once client
  support justifies it.
- **`Last-Event-ID` SSE replay.** Add later if real-world reconnect
  behavior demands it.
- **Per-tool input schemas in `tools/list`.** Today every tool reports
  `{"type": "object", "additionalProperties": true}`. Real schemas
  would help clients with type-aware completion but are a separate
  body of work tied to the daemon's RPC method registry.
- **MCP progress notifications.** Held until the first tool genuinely
  needs them.

## 9. What this design does not change

- The Unix-socket RPC server is unchanged. The CLI keeps talking to it.
- `rpc.Authorizer`, the capability-token storage, and audit-recording
  are reused without modification.
- The Python `striatum` CLI is *not* removed by this RFC — only
  `src/striatum/mcp.py` and the MCP wrapper bits. Other Python CLI work
  is RFC 0068's runway.
- Workflow validation, generation, and the workflow templates list
  remain hidden from MCP `tools/list` (see `isHiddenProductionTool`).
  Agents inside lanes call the CLI for those — they're authoring
  surfaces, not runtime tools.

## 10. Open questions for synthesis

These are the points where I expect the three lane proposals to
diverge and where the synthesis will have to pick:

1. **Legacy SSE vs Streamable HTTP for V1.** I argue legacy SSE first;
   other lanes may pick Streamable HTTP. The trade-off is client
   compatibility today vs spec-future-proofing.
2. **Ephemeral port + discovery file vs configured port.** I argue
   ephemeral by default with a discovery file. Other lanes may favor a
   fixed configurable port for simplicity.
3. **`repository_id` defaulting from the token.** I argue we default
   it from the connection-bound token. Other lanes may prefer to
   require it explicitly on every call for clarity.
4. **Where the bootstrap prompt lives.** I argue env vars + minimal
   prompt body. Other lanes may prefer a fuller in-prompt explanation
   or a file the agent reads.
5. **Connection cap as a hard limit vs soft warning.** I argue a hard
   limit (503) at 8. Other lanes may prefer to log-and-allow.

These are also the points worth probing in the design review.
