author: designer-gemini-001

# DESIGN — RFC 0050: Native Go Daemon HTTP/SSE MCP and Agent Loop

## 1. Overview
This design implements RFC 0050, which moves Striatum towards a fully native Go implementation by:
- Deprecating the Python MCP wrapper.
- Adding a native HTTP/SSE MCP server to the `striatumd` Go daemon.
- Refactoring the `-agent-loop` into a PTY-based supervisor that allows agents to be autonomous MCP clients.

## 2. Go Daemon HTTP/SSE Server

### 2.1 HTTP Server Initialization
The `striatumd` daemon currently only listens on a Unix socket for line-framed JSON-RPC. We will add a standard Go `http.Server`.

- **New Flag**: `--http-addr` (e.g., `localhost:8080`). If unset, the HTTP server will not start.
- **Port Discovery**: If set to `localhost:0`, the daemon will pick a random available port and write it to a well-known location (e.g., `~/.striatum/daemon-http-port`).

### 2.2 Endpoint: `/mcp/sse`
The daemon will expose a single endpoint for MCP SSE.

- **GET /mcp/sse**: Establishes the SSE connection.
  - Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`.
  - Event `endpoint`: Sends the POST URI for client-to-server messages (e.g., `/mcp/message?id=<session_id>`).
- **POST /mcp/message**: Receives JSON-RPC messages from the client.

### 2.3 MCP Protocol Mapping
The `go/pkg/mcp` package will be extended to handle SSE transport.

- **Tools Discovery**: Uses `mcp.VisibleTools(ctx, authorizer, token, repositoryID)` to list available daemon methods as MCP tools.
- **Tool Execution**: Uses `mcp.Service.ToolsCall(...)` which routes the call to the daemon's internal `rpc.Server.HandleWithoutHandshake`.
- **Authentication**: Clients must provide the `striatum` capability token. For SSE, this can be passed via:
  - `Authorization: Bearer <token>` header.
  - Query parameter `?token=<token>`.

## 3. Agent Loop Refactor (`-agent-loop`)

### 3.1 PTY Management
The `agentloop.Run` function in `go/pkg/agentloop/loop.go` will be refactored to:
1. Allocate a PTY using a library like `github.com/creack/pty`.
2. Spawn the agent process (e.g., `claude`) with its `stdin`, `stdout`, and `stderr` connected to the PTY.
3. Manage window size (winsize) propagation.

### 3.2 Bootstrap Prompt
Immediately upon spawning the agent, the supervisor will write a bootstrap prompt to the PTY's master side:

```text
You are a Striatum lane agent.
Connect to the MCP server at http://localhost:<port>/mcp/sse?token=<token>.
Call 'work.await_packet' to register for work.
```

The supervisor will no longer perform `work.await_packet` or `work.complete` itself. It remains active only to monitor the agent's process lifecycle and proxy I/O.

### 3.3 Token Handling
The supervisor reads the runtime token from `.striatum/capability_token` and includes it in the bootstrap URI to ensure the agent has immediate authenticated access.

## 4. Deprecation and Cleanup

- **Remove**: `src/striatum/mcp.py`.
- **Remove**: Any remaining Python-based MCP tests.
- **Update**: `go/pkg/agentloop/loop.go` to remove the JSON `stdin` piping logic.

## 5. Security Considerations

- **Localhost Binding**: The HTTP server must default to `localhost` to prevent unauthorized remote access.
- **Capability Tokens**: Full capability enforcement is maintained by routing all MCP calls through the daemon's existing `Authorizer`.
- **Audit Logging**: Every MCP tool call is recorded via the daemon's `AuditRecorder`, just like standard RPC calls.
