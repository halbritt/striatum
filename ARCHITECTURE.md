# Striatum Architecture Map

One navigable map of the substrate (GH #161, RFC 0111 companion). It orients a
fresh agent or human without the seven-doc tour; it does **not** replace the
product boundary in [`docs/reference/spec.md`](docs/reference/spec.md), the
[decision log](docs/decisions/decision-log.md), or the
[RFC index](docs/rfcs/README.md) — when this map disagrees with them, they win.

Striatum is a standalone, **local-first workflow runner for terminal-based AI
coding agents**: multi-model lanes (Claude / Codex / Gemini) execute reviewed,
provenance-tracked work against target repositories, with anti-hallucination
coming from multi-model review plus an append-only audit trail. Self-hosted
server, multi-repo, possibly multi-user (RFC 0107) — never SaaS, no telemetry,
no Striatum-managed cloud APIs (D094). Operator-provided S3-compatible blob
endpoints are configured durability backends, not a hosted Striatum service
(RFC 0072 / RFC 0123).

## Components (three binaries, one Makefile)

| Binary | Role |
|---|---|
| `striatumd` | The daemon — the **single writer** and only authority over live state. Owns PostgreSQL, serves every surface below, runs recovery sweeps. Installed as a systemd user unit (`striatum daemon install`; runbook: [`docs/how-to/daemon-runbook.md`](docs/how-to/daemon-runbook.md)). |
| `striatum` | The CLI — a thin client of the daemon over its unix-socket RPC. Every verb maps 1:1 to a daemon method ([`docs/reference/command-authority-matrix.md`](docs/reference/command-authority-matrix.md)); there is no daemon-less mode (D094 / RFC 0043). |
| `striatum-supervisor-helper` | Per-lane PTY supervisor — bridges a daemon-owned lane to a real agent CLI process under tmux, survives daemon restarts (`KillMode=process`, RFC 0103). |

Source lives under `go/` (`go/cmd/<binary>`, `go/pkg/<area>`). Key packages:
`rpc` (envelope, method registry, capability authority), `mcp` (the MCP HTTP
boundary), `mutations` / `reads` (write/read handlers), `db` (PG, migrations,
hash-chained audit), `supervisor` + `agentloop` (lane runtime),
`workflowauthoring` / `workflowgenerate` / `workflowtemplates` (workflow.json),
`recovery`, `webservice` / `websse` (web UI), `adapterconformance` (the
installed-CLI seat gates, RFC 0109).

## State: who owns what

- **PostgreSQL, owned by the daemon, scoped per `repository_id`** (RFC 0033 /
  D094 / RFC 0043) is the *only* authoritative live state: runs, jobs, leases,
  sessions, artifacts records, events, and a **hash-chained append-only
  `audit_log`**. Two roles: the OS owner applies migrations
  (RFC 0079 §5); the runtime role (`striatumd_rw`) gets narrower authority —
  being hardened toward DB-*enforced* writes per RFC 0110 (D164).
- **`.striatum/` next to each target repo is operational scratch only**: PTY
  FIFOs, supervisor scratch, pidfiles, plugin/token caches. Never workflow
  state, never committed.
- **Repository files are durable provenance, not the message bus.** Artifacts
  land in the target repo (front-matter–validated; publisher refuses invalid
  front matter with exit 6), but markers/tmux/terminal output never advance
  state.
- **Runtime discovery dir** `/run/user/<uid>/striatum/`: `daemon-go.sock`
  (RPC), `client-token` (operator capability token), `mcp-http-endpoint` (URL
  file), `web-ui.sock`, `striatumd.pid`. Config: `~/.config/striatum/daemon.toml`
  (`postgres_url`; runbook: [`docs/how-to/postgres-transition.md`](docs/how-to/postgres-transition.md)).

## Surfaces (all served by the daemon)

| Surface | Transport | Use |
|---|---|---|
| RPC | unix socket `daemon-go.sock`, JSON envelope (RFC 0030: `schema_version`, `request_id`, `method`, `params`, `capability_token`) | The CLI's transport. Typed errors: `rpc.Error{Code, Message, Details}` (+ `Suggestion` and a closed, guard-tested code catalog per RFC 0111 P2/P3). |
| MCP | HTTP `127.0.0.1:<port>/mcp` (+ `/mcp/sse`), bearer capability token | What agent lanes drive: `tools/list` is visibility-filtered per token; `tools/call` re-authorizes every call. Failures carry code+message(+suggestion) in the content text an agent reads (RFC 0111 P1). |
| Web UI | `web-ui.sock` / mounted web service | Operator browser surface (runs, jobs, recovery actions). |
| Dashboard | `striatum dashboard --run-id <id>` (`--once` for one frame) | Compact terminal view for humans/scripts watching a run. |

**Method authority** is a single registry (`go/pkg/rpc/registry_methods.go`):
each method declares its required capability (`read` / `write` / `review` /
`claim` / `admin`), repository scope, and audit class — pinned by guard tests
and documented in the [command-authority-matrix](docs/reference/command-authority-matrix.md).

## Write boundary

Capability tokens gate every method: the operator token
(`/run/user/<uid>/striatum/client-token`) is broad; **session-bound lane
tokens** (issued at `register-session`, wired into the lane env at
`supervise start`) are narrow — repo-scoped, capability-scoped, expiring
(RFC 0096 / 0103 W1). Work packets carry `write_scope.allowed_paths` /
`forbidden_paths`; the daemon's write-scope guard rejects out-of-scope
`work.complete`. RFC 0110 (D164) extends enforcement into PostgreSQL itself
(ephemeral runtime credential, SECURITY-DEFINER-only writes, lane OS-user
isolation) so a leaked credential cannot forge artifacts or rewrite the audit
chain.

## Run model (the vocabulary an agent needs)

- **Workflow** (`workflow.json`, `striatum.workflow.v1`): lanes (adapter +
  command + capabilities + optional `worktree_isolation: per_job`), roles,
  jobs (typed: `synthesis`, `review`, …; shapes are support-tiered and frozen
  per RFC 0106), edges, and bounded `cycles` (e.g. review → implement on
  `needs_revision`). `run prepare` **snapshots** the workflow — editing the
  file does not change a prepared/running run.
- **Run / jobs / leases**: a run moves jobs through queued → claimed (lease) →
  completed; leases expire lazily and have heartbeats (`work.heartbeat`).
  Per-run locking serializes run mutations (RFC 0104).
- **Sessions / lanes / attestation**: a lane session becomes **attested** when
  its supervised PTY process is the thing doing the work (`supervise start`);
  attestation decays without heartbeats and gates `artifact.publish` bylines
  (lowercase privacy-safe: `author: <role>-<model>-<ordinal>`).
- **The loop** (skill bundle: `striatum skills install`): `work.await_packet`
  (MCP) or `claim-next` → `ack` → do the work → `publish-artifact` /
  `submit-review` → `complete`. Verdicts: `accept` /
  `accept_with_findings` / `needs_revision` / `reject`. A `needs_revision`
  on an interrogable job **closes the prior lane session** and spawns a
  next-ordinal lane for attempt 2 (auto-re-supervised).
- **Interrogation**: reviewers may open Q&A windows against interrogable jobs
  (`interrogation.open/ask/answer`); unavailability is a legible non-wedging
  signal (`interrogation_unavailable`, RFC 0103 W4).
- **Recovery**: the background sweep + `striatum recovery …` verbs requeue
  stale leases, probe lane-process liveness (dead agent ⇒ requeue, #147),
  auto-publish recorded verdicts, and auto-finalize completed runs — designed
  for unattended operation (RFC 0020/0105; standing reliability harness).

## Failure legibility

Errors are part of the contract (RFC 0111, D165): stable codes
(`invalid_transition`, `capability_missing`, `token_expired`, …) reach the MCP
content text an agent reads (P1, landed), with default remediation suggestions
and a closed guard-tested catalog in `pkg/rpc` +
[the matrix](docs/reference/command-authority-matrix.md) (P2/P3) — so an agent
dispatches on failures in-band instead of re-running CLI verbs.

## Where to go deeper

1. [`docs/reference/spec.md`](docs/reference/spec.md) — the product boundary (source of truth).
2. [`docs/how-to/how-to-agent.md`](docs/how-to/how-to-agent.md) — driving the runner as an agent.
3. [`docs/decisions/decision-log.md`](docs/decisions/decision-log.md) + [`docs/rfcs/README.md`](docs/rfcs/README.md) — why it is this way.
4. [`docs/index.md`](docs/index.md) — every doc, one line each.
