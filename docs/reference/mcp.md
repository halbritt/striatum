# Striatum MCP

Status: native Go daemon HTTP MCP is the production tool surface
Updated: 2026-05-21

## Overview

Striatum's MCP surface is served by the local Go `striatumd` daemon. The
primary transport endpoint is Streamable-HTTP-style `POST /mcp` on loopback
only, with `GET /mcp` and the legacy `/mcp/sse` alias available for SSE
clients. Tool discovery comes from the daemon method registry, is filtered by
the caller's capability token, and every `tools/call` re-enters daemon RPC with
normal authorization, request logging, and audit behavior.

The retired Python `striatum.mcp` stdio wrapper is no longer part of the
product surface. Agents should connect to the running daemon instead of
spawning a proxy process.

## Endpoint

`striatumd` starts the MCP HTTP listener by default on an ephemeral loopback
port. The daemon writes the active SSE endpoint to the owner-only runtime file:

```text
$STRIATUM_DAEMON_RUNTIME_DIR/mcp-http-endpoint
```

If `STRIATUM_DAEMON_RUNTIME_DIR` is unset, the runtime directory follows the
same daemon token/socket rules documented in `docs/how-to/postgres-transition.md`.
The file contains a single URL such as:

```text
http://127.0.0.1:43127/mcp
```

The listener can be configured with:

```bash
striatumd --mcp-http-addr 127.0.0.1:8765
STRIATUM_DAEMON_MCP_HTTP_ADDR=127.0.0.1:8765 striatumd
```

Use `--mcp-http-addr off` to disable the listener. Non-loopback bind addresses
are refused.

## Protocol

Diagnostic and Streamable HTTP clients can send JSON-RPC requests directly to:

```http
POST /mcp
Authorization: Bearer <capability-token>
Content-Type: application/json
```

The response is returned as one JSON-RPC response body. The supported JSON-RPC
methods are:

- `initialize`
- `notifications/initialized`
- `tools/list`
- `tools/call`

SSE clients can open a stream at either endpoint:

```http
GET /mcp
Authorization: Bearer <capability-token>
```

or the compatibility alias:

```http
GET /mcp/sse
Authorization: Bearer <capability-token>
```

The first event is `endpoint`; its data is a relative message URL:

```text
event: endpoint
data: /mcp/messages?session_id=<session>
```

Clients then send JSON-RPC requests to that URL:

```http
POST /mcp/messages?session_id=<session>
Authorization: Bearer <capability-token>
Content-Type: application/json
```

Responses are delivered on the SSE stream as `message` events. The
compatibility alias also accepts direct diagnostic `POST /mcp/sse`, but new
clients should use `POST /mcp`.

## Authentication

Use a daemon capability token in the HTTP `Authorization` header:

```http
Authorization: Bearer dtok_...
```

Tokens are the same daemon tokens used by Unix-socket RPC. The daemon runtime
`client-token` is not automatically applied to arbitrary clients; a supervisor
or operator must pass token material explicitly. Tokens in query strings or
JSON-RPC params are not accepted by the daemon HTTP transport.

`tools/list` accepts an optional `repository_id`. Single-repository tools are
listed only when the token is authorized for that repository. `tools/call`
also accepts `repository_id` at the method params level and copies it into the
tool `arguments` object when the caller did not already provide one.

## Tool Calls

`tools/list` returns daemon methods that are all of:

- present in `contracts/daemon_methods.json`,
- non-deprecated,
- not internal `daemon.*` handshake methods,
- not hidden local workflow-authoring methods,
- authorized by the supplied token and repository scope.

Example direct diagnostic request:

```json
{"jsonrpc":"2.0","id":"tools","method":"tools/list","params":{"repository_id":"repo_123"}}
```

`tools/call` dispatches through daemon RPC only for production-supported MCP
tools. Hidden local workflow-authoring methods fail closed at the MCP layer
with `structuredContent.error == "tool_hidden"` even when the caller has a
write-capable token. Production-supported calls use MCP tool result shape with
Striatum details in `structuredContent`:

```json
{
  "content": [{"type": "text", "text": "status"}],
  "structuredContent": {
    "ok": true,
    "method": "status",
    "audit_id": "audit_..."
  },
  "isError": false
}
```

Denied calls fail closed. Calls that reach daemon RPC audit under
`transport = "mcp"`; hidden local workflow-authoring methods are refused by
MCP before daemon dispatch. Missing bearer auth, malformed JSON, bad local
browser Origin/Host checks, and unknown JSON-RPC methods return stable
JSON-RPC error objects with `error.data.code`. Hidden-tool, daemon-method, and
authorization denials through `tools/call` return MCP tool results with
`isError: true` and the denial code in `structuredContent.error`. Common
denial codes include `tool_hidden`, `token_missing`, `token_malformed`,
`token_invalid`, `token_revoked`, `token_expired`, `capability_missing`,
`capability_scope_mismatch`, `capability_expired`, `repo_not_registered`, and
`method_unknown`.

The HTTP listener refuses non-loopback bind addresses at startup. Requests must
also carry a loopback `Host`, and any `Origin` header must be loopback
(`http://localhost`, `http://127.0.0.1`, or equivalent loopback IP forms).

## Agent Loop

The Go `--agent-loop` mode is a PTY supervisor only. It starts the configured
agent command, exports the daemon MCP endpoint in `STRIATUM_MCP_URL`, passes
token material through `STRIATUM_MCP_TOKEN` or `STRIATUM_MCP_TOKEN_FILE`, and
injects a bootstrap prompt.

The supervisor does not call `work.await_packet`, claim work, complete work,
release work, or write packet JSON. The agent is responsible for using MCP:

1. call `tools/list`,
2. call `work.await_packet` with `repository_id`, `session_id`, and
   `lease_seconds`,
3. use the packet's identifiers, expected artifacts, and write scope,
4. report state with MCP tools such as `work.ack`, `artifact.publish`,
   `review.verdict`, `work.complete`, or `work.release`.

Before a packet exists, agents use `session.report` for structured
`ready`, `heartbeat`, `question`, or `escalate` reports. This is the
pre-work path for startup blockers; terminal text and pane contents remain
observability only.

## Boundary

The MCP server is local-only and daemon-owned. It does not introduce hosted
services, telemetry, transcript capture, external persistence, direct database
writes outside daemon RPC, marker-file state, or terminal-output state.

Repository files remain provenance; PostgreSQL remains live workflow state.
`.striatum/` beside a target repository is operational scratch, not an MCP
message bus.
