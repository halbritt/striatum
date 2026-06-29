# Web UI Development

Status: current Go web surface; RFC 0038 React/Vite guide retired
Date: 2026-06-09

Striatum's current local web UI is served by `striatumd` from Go code. The
old contributor-side React/Vite island tree under `src/striatum/web/frontend/`
was removed with the Python runtime. There is no current `make ui-install`,
`make ui-build`, `make ui-dev`, or `make ui-test` loop.

Use this guide when changing the current Go-served web surface.

## Current Layout

```text
go/cmd/striatumd/web_service.go       # mounts web service beside MCP
go/pkg/webservice/                    # HTTP routes, auth, mutation gate, SSE
go/pkg/webassets/                     # embedded templates and static assets
go/pkg/webassets/templates/*.html     # server-rendered pages
go/pkg/webassets/static/base.css      # bundled stylesheet
go/pkg/webassets/static/app.js        # bundled browser script
go/pkg/websse/                        # SSE helpers
```

`go/pkg/webassets/assets.go` embeds templates and static files into the Go
binary. Operators install Go release archives and do not need Node.

## Local Development Loop

```bash
make -C go build
./go/bin/striatumd \
  -socket "${XDG_RUNTIME_DIR}/striatum/daemon-go.sock" \
  -postgres-url "$STRIATUM_DAEMON_DB_URL"
```

In another shell, discover the web base URL from the MCP endpoint and strip
the `/mcp` suffix:

```bash
BASE_URL=$(sed 's#/mcp$##' "${XDG_RUNTIME_DIR}/striatum/mcp-http-endpoint")
TOKEN=$(cat "${XDG_RUNTIME_DIR}/striatum/client-token")
curl -H "Authorization: Bearer ${TOKEN}" "${BASE_URL}/v1/health"
```

For systemd-managed installs, use:

```bash
systemctl --user restart striatumd
striatum daemon status
```

## Route Boundary

The loopback web service uses the same runtime bearer token as daemon MCP. It
rejects non-loopback hosts. Mutating web routes fail closed unless
`STRIATUM_DAEMON_WEB_ALLOW_MUTATIONS=1` is set on the daemon process before
startup.

Current routes are documented in [SPEC.md - Local Web UI](../reference/spec.md#local-web-ui).
The important implementation surfaces are:

- `GET /` and `GET /run?run_id=<id>` - server-rendered status pages.
- `GET /static/<path>` - embedded static assets.
- `GET /v1/health` - service health.
- `GET /v1/runs...` - daemon-backed JSON/SSE reads.
- `GET /v1/artifacts/<artifact_id>/raw` - raw artifact bytes.
- `GET /workflow-templates...` - workflow template reads.
- `POST /v1/invoke`, `POST /workflows/generate/preview`, and
  `POST /workflows/generate` - mutation-capable endpoints gated by
  `STRIATUM_DAEMON_WEB_ALLOW_MUTATIONS`.

Retired Python-era pages such as `/view/`, `/workflows/new`,
`/workflows/edit/<path>`, `/chat`, and `/doctor` are not current Go routes
unless a future change reintroduces and documents them.

## Tests

Focused Go tests live near the web code:

```bash
go test ./pkg/webassets ./pkg/webservice ./pkg/websse ./cmd/striatumd
```

The repo-wide verification target remains:

```bash
make test
```

Do not add a Node toolchain, CDN dependency, external runtime fetch, or SPA
bundle without an accepted product decision.

## See Also

- [daemon-runbook.md](daemon-runbook.md) - daemon lifecycle and runtime layout.
- [mcp.md](../reference/mcp.md) - MCP endpoint discovery and authentication.
- [SPEC.md - Local Web UI](../reference/spec.md#local-web-ui) - product
  boundary for the web surface.
- [RFC 0085](../rfcs/0085-tailnet-identity-ui-auth.md) - optional read-only
  tailnet identity listener.
