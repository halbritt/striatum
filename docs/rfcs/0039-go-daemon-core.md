# RFC 0039: Go Daemon Core

Status: superseded by D107 / D109 / D111 / RFC 0068 (Go production daemon)
Date: 2026-05-13
Supersession note: D105 temporarily narrowed Go to support/runtime work.
D107/RFC 0068 supersedes that constraint and restores the Go production daemon
port as the target architecture. D109 later flips `striatum daemon start` to
the Go daemon by default, and D111 retires the Python daemon selector. Older
sections in this RFC that describe Python as the default, dual-core CI, or
operator-selectable Python are historical phase notes.
Context:
[`RFC 0028`](0028-long-running-daemon-and-multi-repository-control-plane.md),
[`RFC 0030`](0030-daemon-rpc-server-and-version-skew-protocol.md),
[`RFC 0031`](0031-daemon-owned-supervision-and-sealed-apply-boundary.md),
[`RFC 0032`](0032-cross-repo-workflows-and-mcp-mutation-capabilities.md),
[`RFC 0033`](0033-storage-substrate-rewrite-for-daemon-v2.md),
[`RFC 0035`](0035-multi-repo-test-harness-for-cross-repo-workflows.md),
[`docs/DECISION_LOG.md`](../decisions/decision-log.md) (D082, D084, D086, D087, D088, D107, D109, D111),
`src/striatum/daemon.py`,
`src/striatum/daemon_rpc/`,
`src/striatum/daemon_apply/`,
`src/striatum/daemon_supervisor/`,
`src/striatum/daemon_pg/`

## Problem

D084 planned a Go-language core for the daemon. The reasoning at the
time of D084 was: "Daemon-first product positioning makes long-running
process supervision, signal handling, packaging, and single-binary
distribution first-class concerns. Go is a better fit for that
operational surface than Python, but a rewrite has cost; designing the
protocol to be language-agnostic now avoids relitigating the wire
format later."

The conditions D084 listed as worth revisiting the rewrite cost have
arrived:

1. **RFC 0030 has shipped** (dogfood-034, v1.23.0). The daemon RPC
   envelope-v1, version-skew handshake, capability-bound method
   registry, and audit chain helpers are all language-agnostic from
   day one — exactly as D084 required.
2. **RFC 0033 has shipped** (dogfood-033, v1.22.0). The PostgreSQL
   substrate is the daemon's authoritative store; the Go daemon does
   not need to reimplement SQLite handling. The daemon DB schema is
   defined by SQL migrations under `src/striatum/daemon_pg/sql/` which
   can run unchanged against the same PostgreSQL instance.
3. **RFC 0031 has shipped** (dogfood-034 + dogfood-035, v1.23.0 +
   v1.24.0). Daemon-owned supervisor metadata, apply receipt schema,
   and sealed-apply authority semantics are nailed down.
4. **RFC 0032 has shipped** (dogfood-035, v1.24.0). MCP capability-
   gated `tools/call` + `tools/list` filtering + audit row append are
   complete.
5. **RFC 0035 has shipped** (dogfood-037, v1.27.0). The multi-repo
   test harness exercises every RFC 0032 V2 threat surface end-to-end.
   The Go daemon can be validated against the same harness.
6. **Daemon V2 surface area has stabilized.** The Python daemon
   currently spans `src/striatum/daemon.py` (foreground supervision,
   process boot, signal handling), `src/striatum/daemon_rpc/` (RPC
   server + method registry + capability + envelope), `src/striatum/
   daemon_apply/` (apply service), `src/striatum/daemon_supervisor/`
   (supervisor pointers), and `src/striatum/daemon_pg/` (Postgres
   substrate). Python is currently fighting itself on three of these
   surfaces:
   - **Long-running process supervision.** Python's signal handling
     interacts poorly with threaded subprocess management. Several
     dogfoods (036, 037, 038) hit minor friction patterns around
     supervisor cleanup, stale leases under active load, and
     PID-recycling correctness checks. Go's goroutine + signal-channel
     model is the well-trodden path for this shape.
   - **Single-binary distribution.** The daemon currently ships as
     a `striatumd` console script that depends on the full
     `striatum-orchestrator` wheel + `psycopg[binary]` + the Python
     runtime version compatibility matrix. A Go daemon can ship as
     a single statically-linked binary per platform, addressable via
     `go install` or a downloadable artifact.
   - **PTY handling for supervised lanes.** Python's `pty` module and
     `subprocess.Popen` interaction is platform-specific and has
     surprised us in cross-platform tests. Go's `os/exec` + `pty`
     packages are well-tested across Linux/macOS.

The CLI client (the `striatum` command) stays in Python. It is the
operator's surface and benefits from Python's REPL-debuggability,
docstring tooling, and pip distribution. CLI ↔ daemon communication
goes over the RFC 0030 envelope-v1 protocol (Unix socket, JSON), which
is already language-agnostic by D084 design.

RFC 0039 scopes the actual Go rewrite: layout, build, distribution,
test parity, migration path, retirement of the Python daemon.

## Goals

- Rewrite the daemon core in Go: process supervision, RPC server,
  apply service, supervisor metadata management, Postgres database
  access layer.
- Preserve the RFC 0030 envelope-v1 wire protocol unchanged. CLI
  clients (the Python `striatum` command and any future Go test
  client) talk to the daemon via the same Unix socket + JSON
  framing.
- Preserve the RFC 0033 Postgres substrate unchanged. Same schema,
  same migrations, same audit chain helper semantics. The Go daemon
  reads/writes the same DB.
- Ship the Go daemon as a single statically-linked binary per
  platform (Linux x86_64, Linux arm64, macOS x86_64, macOS arm64).
- Maintain test parity: the RFC 0035 multi-repo test harness must
  cover the Go daemon end-to-end before the Python daemon is retired from
  production. The harness already boots a daemon subprocess; the binary just
  needs to be the Go one.
- Provide a historical migration path: Python daemon and Go daemon coexist
  during transition, then D109/D111 move production startup to Go and retire
  Python daemon selection. Remaining Python-daemon code is deletion/fixture
  debt, not an operator runtime choice.

## Non-Goals

- Rewriting the CLI in Go. The operator-facing CLI stays Python.
- Rewriting the agent SDK (`striatum.skills`, `striatum.plugin`,
  `striatum.workflow_generator`, `striatum.web`). These are
  CLI/web-side and stay Python.
- Multi-machine / hosted-mode daemon (D083 out of scope).
- Windows daemon support. Per RFC 0030/0031 V2 scope, Windows
  daemon is deferred. Linux + macOS only for the Go rewrite too.
- Changing the wire protocol, schema, or audit-chain semantics.
- Changing the operator-facing semantics of `striatum daemon
  start`, `repo add/list/remove`, `recovery *`, dashboard, MCP, etc.
- Performance optimization beyond what's required for correctness.
  The Go rewrite's motivation is operational surface (signals,
  packaging, PTY) not raw throughput.
- Sealing the apply path behind cryptographic non-repudiation. RFC
  0031's threat model (AI guardrail, not malicious-local-root) is
  preserved.

## External Prior Art

Daemon-and-CLI-in-different-languages is a common pattern:

- **Docker** — daemon in Go (`dockerd`), CLI in Go (also). The
  Go-daemon-Go-CLI pattern is the most-trodden, but Striatum's
  Python CLI is intentionally kept because the operator surface
  benefits from Python tooling.
- **Kubernetes** — control plane in Go (`kube-apiserver`,
  `kube-controller-manager`), various CLIs in Go but also Python
  (`kubectl-helm-python`, etc.). The pattern of "Go control plane
  + per-language CLIs over a stable wire protocol" matches
  Striatum's intent.
- **containerd** — Go daemon, gRPC API, multiple language clients.
  Wire protocol stability is the key constraint; Striatum's RFC
  0030 envelope-v1 fills the same role.
- **Buildkit** — Go daemon, gRPC API. Similar shape.
- **PostgreSQL** — server in C, clients in everything. The
  precedent of "server in operationally-strong language, clients
  everywhere" is decades old.

## Proposal

### 1. Repository layout

A new top-level directory `go/` houses the Go daemon. The existing
Python package tree under `src/striatum/` stays.

```
striatum/
  src/striatum/           # Python CLI + agent SDK + web UI (unchanged)
  go/
    cmd/
      striatumd/          # daemon main package
        main.go
    pkg/
      rpc/                # RFC 0030 envelope-v1 implementation
        envelope.go
        registry.go
        capability.go
        server.go
      apply/              # RFC 0031 apply service
        receipt.go
        service.go
      supervisor/         # supervised process owner
        pointer.go
        liveness.go
        pty.go
      db/                 # daemon Postgres substrate
        connection.go
        migrations.go
        audit.go
      mcp/                # RFC 0032 MCP tools/call + tools/list
        capabilities.go
        tools.go
      crossrepo/          # RFC 0032 cross-repo run lifecycle
        prepare.go
        lifecycle.go
    go.mod
    go.sum
    Makefile              # contributor-side build for the go daemon
  Makefile                # top-level; gains daemon-go-* targets
  docs/
  tests/                  # Python tests stay; new Go tests under go/
```

The Go daemon's source is **separate** from the Python source. The
RFC 0030 wire protocol is the shared contract; neither side imports
the other.

### 2. Wire protocol contract

RFC 0030 envelope-v1 is the unchanged contract. The Go daemon implements
the same:

- Unix socket transport with owner-only permissions.
- Newline-delimited JSON envelope per request.
- `daemon.hello` / `daemon.welcome` version handshake.
- `daemon.describe` method registry exposition.
- Capability-bound method registry (seven capabilities: `read`,
  `write`, `review`, `claim`, `apply`, `admin`, `recovery`).
- Audit row append for every mutating method call.

The Python CLI client code under `src/striatum/daemon_rpc/client.py`
(or equivalent) talks to either daemon implementation interchangeably;
no client-side change.

### 3. Database contract

RFC 0033 Postgres substrate is the unchanged contract. The Go daemon:

- Reads connection details from the same `STRIATUM_DAEMON_DB_URL` env
  var or `~/.config/striatum/daemon.conf`.
- Runs migrations from `src/striatum/daemon_pg/sql/*.sql` (or a Go
  embedding of those same SQL files). Migrations are Postgres SQL;
  they don't care which language applies them.
- Uses the same schema (cross_repo_runs, audit_chain, capability
  tokens, supervisor pointers, etc.).
- Uses the same audit-chain hash helper (defined as SQL/Postgres
  function or replicated in Go).

During the historical coexistence window, the Python daemon and Go daemon were
**mutually exclusive** in a given run: only one daemon owned the Postgres
database at a time. Current production startup uses the Go daemon; the pidfile
+ socket-path lock still prevents concurrent daemon processes.

### 4. Selection mechanism

Superseded by D111 / RFC 0068 follow-through: the selection mechanism below
was transitional and no longer selects a production daemon. Current production
startup uses the Go daemon; `--core go` is a deprecated no-op compatibility
flag, while `--core python` and `STRIATUM_DAEMON_CORE=python` no longer select
a daemon.

- `striatum daemon start` (Python CLI) defaults to the Go daemon after D109.
- `striatum daemon start --core python` is retired by D111 and does not boot a
  Python daemon.
- `STRIATUM_DAEMON_CORE=python` is retired by D111 and does not boot a Python
  daemon.
- RFC 0068 tracks deletion of the legacy Python daemon module after fixture
  cleanup.

The Python CLI launches the Go daemon as a subprocess via the packaged
`striatum._daemongo` binary when present, then falls back to
`STRIATUMD_GO_BIN`, then `go/bin/striatumd` for contributor checkouts.
The Python CLI client speaks envelope-v1 over the Unix socket
regardless of daemon language.

### 5. Distribution

**Per-platform binaries:**

- `make -C go build` from the source tree produces `go/bin/striatumd`.
- Release tooling cross-compiles linux-amd64, linux-arm64,
  darwin-amd64, and darwin-arm64 binaries, then stages them into the
  `striatum._daemongo` package-data tree with `make daemon-go-release`.
- Contributor checkouts can stage the host binary for local wheel or
  editable testing with `make daemon-go-install`; custom binaries use
  `STRIATUMD_GO_BIN=/path/to/striatumd`.

**Current operator install:**

```bash
pip install striatum-orchestrator
striatum daemon start
```

`--core go` is accepted only as a deprecated no-op compatibility flag.
Contributor checkouts can use `make -C go build`; custom binaries use
`STRIATUMD_GO_BIN=/path/to/striatumd`. The Python daemon is no longer an
optional production core.

### 6. Process supervision

The Go daemon owns the supervised-lane subprocesses (agent CLIs in
`bash -lc '... | exec <wrapper>.sh'` shape). Per RFC 0031, supervisor
metadata lives in the daemon DB. The Go implementation:

- Spawns supervised processes with `os/exec` + `creack/pty` (the
  well-trodden Go PTY library).
- Writes packet JSON to the supervised wrapper's stdin via the FIFO
  pipe (same shape as the Python daemon).
- Heartbeats the lease while the supervised process is making forward
  progress (per the friction note from dogfood-038 OPERATOR_REPORT
  intervention #5: supervised-progress lease heartbeat).
- Cleans up subprocesses on SIGTERM via deterministic signal channel
  + waitgroup drain (Go's well-trodden pattern).

### 7. Test parity

The RFC 0035 multi-repo test harness boots a daemon subprocess via
`tests/_harness/daemon.py`. Extending the harness:

- Historical V1/V1.5 harness work added a `daemon_core` parameter
  (`"python"` or `"go"`) so the Go daemon could be compared against the
  Python daemon before the cutover.
- Current conformance uses the Go daemon as the production gate
  (`make daemon-go-conformance` / `CORE=go`). The Python-core CI lane is
  retired by D111; any remaining Python-daemon exercise is legacy fixture
  cleanup.
- The acceptance bar for production is now Go conformance plus explicit
  removal of unsupported method names from the production contract.

Per-language unit tests:

- Go tests under `go/pkg/*/test_*.go` exercise the Go daemon's
  internal behaviors (envelope parsing, capability matching,
  PTY interaction, signal handling).
- Python tests under `tests/*` stay unchanged.

### 8. Audit-chain semantics

The audit-chain hash is a SQL function in `src/striatum/daemon_pg/sql/`.
The Go daemon calls the same function. The chain is verified
end-to-end by the same harness assertions (RFC 0035). No language-
specific audit-chain code; the chain is a property of the daemon DB
schema.

### 9. Migration path

Three phases:

**Phase 1 — historical coexistence (RFC 0039 V1 scope):**
- Both daemons existed; operators could choose via `--core` flag.
- The default stayed Python during this historical phase.
- Multi-repo test harness coverage compared both cores before cutover.
- CI ran both daemon test matrices before D111 retired the Python-core lane.
- Documentation labelled each daemon's tradeoffs before Go became the
  production core.

**Phase 2 / Phase 3 superseded by D107 / RFC 0068 / D111:**
D107 restored the path where Go becomes the production daemon, D109 flipped
the default, and D111 retired Python daemon selection. The Python CLI may
remain the operator client, but daemon ownership, PostgreSQL migrations,
audit, authorization, MCP, recovery, and supervision are now Go production
responsibilities.

The original RFC 0039 acceptance covered Phase 1 only; RFC 0068 owns the
production cutover and Python-daemon retirement follow-through.

### 10. Historical CI matrix

The original CI matrix gained:

- Go 1.23 (or current Go LTS) toolchain setup.
- `go build ./...` step for Go daemon binaries.
- `go test ./...` for Go-side unit tests.
- `make test-multi-repo CORE=go` runs the harness against the Go
  daemon.
- Cross-compilation for linux-arm64, darwin-amd64, darwin-arm64
  artifacts (release-time only; not every PR).
- Wheel/package smoke covers the packaged Go daemon binary when present;
  source and sdist installs may still rely on `STRIATUMD_GO_BIN` or
  `go/bin/striatumd`.

## Acceptance Criteria

- `go/` directory layout exists with `cmd/striatumd/`, `pkg/{rpc,
  apply,supervisor,db,mcp,crossrepo}/`, `go.mod`, and
  `go/Makefile`.
- Go daemon implements the full RFC 0030 envelope-v1 + version
  handshake + method registry + capability gating.
- Go daemon reads/writes the RFC 0033/RFC 0043 Postgres substrate using the
  same daemon-owned schema and migrations.
- Go daemon owns supervised processes per RFC 0031, including PTY
  + signal handling + supervised-progress lease heartbeat.
- Go daemon implements the RFC 0032 cross-repo run lifecycle +
  MCP `tools/call` + `tools/list` + audit append.
- `striatum daemon start` launches the Go daemon binary via the Python CLI;
  `--core go` is retained only as a deprecated no-op compatibility flag.
- The multi-repo harness boots the Go daemon and runs the daemon conformance
  suite green.
- CI runs the Go daemon conformance gate; the Python-core lane is retired.
- Cross-compile produces linux-amd64 + linux-arm64 + darwin-amd64
  + darwin-arm64 binaries on release.
- Distribution: a release ships the wheel (Python) + four Go binaries
  per platform.
- Documentation: `docs/SPEC.md` daemon section names Go as the production
  daemon, `docs/HOW_TO_HUMAN.md` documents normal `striatum daemon start`
  usage, and `CHANGELOG.md` records the cutover.
- No regression in any existing Python test.

## Implementation Plan

This is a large rewrite. Six phases land independently with green
test parity at each step.

> Historical status (dogfood-042): Steps 1+2 landed per the
> Track A synthesis (`docs/dogfood/042/track_a/DESIGN_SYNTHESIS.md`). The
> Go daemon now exposes the read-only RPC envelope-v1 method registry
> (`daemon.hello`, `daemon.welcome`, `daemon.describe`, `daemon.status`,
> `daemon.version`, `audit.show`, `repo.list`) on top of the RFC 0033
> PostgreSQL substrate, with a cross-language v2 audit-row hash that
> the Python verifier accepts. The RFC 0035 multi-repo test harness
> gained a `daemon_core` parameter (`"python"` default; `"go"` opts in)
> so e2e fixtures could target either core before D111. Later slices added
> `striatum daemon start --core go`, packaged binary lookup,
> conformance gates, generated registry metadata, and broad Go handler
> coverage. D109/D111 have since flipped production startup to Go and retired
> Python core selection; RFC 0068 / TODO 61 now track fixture cleanup and
> Python-daemon module deletion.

### Step 1. Skeleton + envelope-v1

Land `go/cmd/striatumd/`, `go/pkg/rpc/{envelope,registry,capability,server}.go`,
`go.mod`. Implement: socket listen, accept, envelope parse/serialize,
hello/welcome handshake, describe, capability table, ack/refuse paths.
Tests: `go test go/pkg/rpc/...` covers envelope round-trip and
capability matching. The daemon at this stage handles read-only RPC
verbs only.

### Step 2. Postgres substrate

Land `go/pkg/db/{connection,migrations,audit}.go`. Implement
connection pool, migration loader (reading from
`src/striatum/daemon_pg/sql/*.sql`), and the audit-chain wrapper.
Tests: `go test go/pkg/db/...` against ephemeral Postgres (matching
the RFC 0035 harness path).

### Step 3. Read-only daemon (CLI integration)

Wire the Python CLI's historical `striatum daemon start --core go` path to
launch the Go binary. Current startup uses `striatum daemon start`; `--core
go` remains a deprecated no-op. The Go daemon handles read-only verbs (status,
dashboard, audit show) from the Python CLI. First end-to-end test:
`MultiRepoHarness(daemon_core="go")` smoke + `test_cross_repo_prepare_e2e`
read-only assertions.

### Step 4. Mutating verbs + apply

Land `go/pkg/apply/`, `go/pkg/mcp/`, `go/pkg/crossrepo/`. Implement
the full mutation verb table: cross-repo prepare/start/cancel,
`tools/call`, capability token issuance/revocation/expiry. Run the
full RFC 0035 harness against `daemon_core="go"`; iterate until
green.

### Step 5. Supervised processes

Land `go/pkg/supervisor/{pointer,liveness,pty}.go`. Implement
supervised-lane spawning with PTY, packet delivery via FIFO,
heartbeat from supervised-progress signal, deterministic cleanup
on SIGTERM. Smoke test with a real codex/claude/gemini supervised
lane.

### Step 6. Distribution + docs

Cross-compile the four platform binaries in release tooling and stage
them into `striatum._daemongo` package data. Land `docs/SPEC.md`
daemon section update, `docs/HOW_TO_HUMAN.md` flag documentation, and
`CHANGELOG.md` entry. Tag a release whose wheel can carry the matching
Go daemon binary while source and sdist installs keep the
`STRIATUMD_GO_BIN` / `go/bin/striatumd` fallbacks.

## Open Questions

- Should the Go daemon's source live in this repo or a separate
  repository? Decision: same repo for V1 so wire protocol, migration,
  and conformance changes stay co-located; reconsider only if release
  cadences diverge after the Python daemon retires.
- Should the Go daemon use a Go gRPC + protobuf stack instead of the
  RFC 0030 envelope-v1 JSON-over-Unix-socket protocol? Recommendation:
  no — RFC 0030 is the contract; switching to protobuf would force
  the Python CLI client to also switch and break compatibility. JSON
  envelope is intentionally simple.
- Should the Go daemon ship through Python package data? Decision:
  yes for the main package when a matching platform binary is staged
  by release tooling. Source and sdist installs remain supported by
  `STRIATUMD_GO_BIN` and contributor-checkout `go/bin/striatumd`.
- Should the Go daemon take over the apply-receipt signing path
  from RFC 0031? Recommendation: yes — apply-receipts are daemon-
  owned per D088; the Go daemon implements the same fail-closed
  authority semantics.
- Should the Go daemon expose Prometheus metrics? Recommendation:
  no — local-first ethos says no telemetry surface. Operators
  who want metrics can scrape the audit chain.
- ~~Should the existing Python daemon `striatum daemon start` keep
  working forever, or be removed in a future RFC?~~ Resolved by D111:
  `striatum daemon start` launches Go, `--core python` no longer selects a
  daemon, and the remaining Python daemon module is legacy deletion work.

## Domain Modeling

This RFC adds an alternative implementation of the existing daemon
domain, not new aggregates. The daemon's domain (RPC method registry,
capability tokens, audit chain, cross-repo runs, supervised processes,
apply receipts) is preserved verbatim across languages. The wire
protocol (RFC 0030 envelope-v1) and the storage substrate (RFC 0033
Postgres) are the language-independent contracts.

The single relevant concept was **daemon core**: a value object on
the operator-facing configuration enumerating which language implementation
was running during the transition. D111 retires that operator choice. Current
production core is Go; Python may remain only in CLI/web client code and
legacy fixture paths.

## V1.5 Deltas (correctness slice)

V1 shipped a Go daemon that bound the envelope-v1 socket and applied
migrations but had five correctness gaps that blocked promotion of the
Go core to operator workloads. V1.5 closes those gaps and is the merge
slice before mutating routes land in Step 4.

The findings are pinned to dogfood-047 designs and the implementation
order is locked by `docs/dogfood/047/DESIGN_SYNTHESIS.md`. F5 lands
before F4 and F1 because those two correctness fixes need the
parameter-binding and transaction support of the new driver; F2 and F3
land after the daemon can authorize and audit requests correctly.

### F5 — Pure-Go PostgreSQL driver

`go/pkg/db/connection.go` no longer shells out to `psql`. The connection
pool is `pgx/v5` (the first third-party Go runtime dependency for this
repository — `go/go.mod` now requires `github.com/jackc/pgx/v5 v5.7.2`,
with five indirect modules). The pool is configured with
`application_name = "striatumd-go/<daemon_version>"`, a default
`statement_timeout`, and the PostgreSQL simple protocol so the embedded
multi-statement migration files keep working unchanged while parameters
are still bound through the driver. `db.Runner` is the
parameter-aware database surface used by the rest of the daemon, and
`db.TxRunner` is its transactional sibling.

### F4 — Transactional audit append

`go/pkg/db/audit.go` no longer races. `AuditRecorder.RecordRPC` opens
one `READ COMMITTED` transaction, locks the singleton
`striatumd.audit_chain_head` row with `FOR UPDATE`, computes the row
hash from the locked `previous_hash`, inserts the new audit row with
`RETURNING audit_id`, updates the chain head, and commits. The returned
audit id flows back into the RFC 0030 response so the chain remains
linear under concurrent RPC traffic. The opt-in Go race test in
`go/pkg/db/audit_race_test.go` exercises this against an ephemeral
Postgres URL (`STRIATUM_PG_TEST_URL`); the Python cross-core regression
lives in `tests/test_daemon_go_audit.py` and runs under
`make test-multi-repo CORE=go`.

### F1 — PostgreSQL-backed RPC authorization

`go/pkg/rpc/auth_pg.go` introduces `PostgresAuthorizer`, which validates
Python-issued capability tokens against the same `striatumd.clients` /
`striatumd.client_capabilities` rows the Python authorizer uses. Token
secrets are HMAC-SHA256 compared with `subtle.ConstantTimeCompare`
against the stored salt+hash, and the capability lookup mirrors the
Python query (including the `repository_id IS NULL OR repository_id =
$3` wildcard rule and the scope-mismatch fallback). Denial reasons line
up one-for-one with `src/striatum/daemon_rpc/capability.py` so clients
cannot tell the two cores apart from the refusal envelope. The serving
daemon in `go/cmd/striatumd/main.go` wires this authorizer when a
PostgreSQL URL is configured; `AllowAllAuthorizer` is now test-only.

### F2 — Go harness launch contract

The Go binary is the canonical CLI: it accepts `--socket`,
`--postgres-url`, `--migrate`, `--describe`, and the new optional
`--migrations-sha-source`. The SHA-source flag compares the embedded
migration file hashes against the SQL files on disk before serving and
exits non-zero on drift — that replaces V1's `--migrations-dir`
re-loader without giving up the drift signal. `go/Makefile` writes
`go/bin/striatumd` so `tests/_harness/daemon.py` can locate the binary
without an environment override; `STRIATUMD_GO_BIN` remains a trusted
developer-environment override.

### F3 — `make test-multi-repo CORE=go`

`Makefile` accepts `CORE ?= python` and forwards it through
`STRIATUM_MULTI_REPO_DAEMON_CORE`. The class-scoped `daemon_core`
fixture in `tests/conftest.py` reads that variable and passes it to
`MultiRepoHarness`; the test list now includes
`tests/test_daemon_go_smoke.py` and `tests/test_daemon_go_audit.py` so
the Go-core matrix exercises a real boot, a read-only RPC, and the F4
audit chain. The CI shape is intentionally two explicit jobs
(`CORE=python`, `CORE=go`) rather than in-process parametrization, so
the Go-core evidence was intentional rather than implied. Current CI uses the
Go daemon conformance gate; Python-core execution is retired outside legacy
fixtures.

## V1.6 Deltas (supervisor + CI hardening)

V1.6 closes the five named follow-ups from v1.39.0 against the Go
supervisor surface and the CI matrix. Implementation lives in
dogfood-051; the design synthesis at
`docs/dogfood/051/DESIGN_SYNTHESIS.md` is the binding spec.

### F-pty — `creack/pty` master fd wiring

`go/pkg/supervisor/pty.go::launchPTY` replaces the V1.5 not-wired
sentinel with `pty.Start(cmd)` from `github.com/creack/pty v1.1.24`.
The PTY master `*os.File` becomes the daemon's `StdinWriter`; the
slave side is wired automatically by creack/pty as the child's
stdin/stdout/stderr. The `UsePTY=false` branch is unchanged. The new
dependency adds no further transitive Go modules.

### F-pid-recycling — start-time pairing

`go/pkg/supervisor/liveness.go::processAliveAtStartTime` pairs the
signal-0 probe with a kernel-reported start time. The Linux reader
parses `/proc/<pid>/stat` field 22 (clock-ticks-since-boot) and
converts via `/proc/stat` `btime` + 100Hz `CLK_TCK`. A 2-second
tolerance absorbs clock-resolution jitter against
`PointerRow.StartedAt`. On non-Linux the reader returns `(_, false)`
and the probe falls back to signal-0 only — the V1.6 acceptance gate
is Linux explicitly per §6 above. The heartbeat goroutine passes
`row.StartedAt` to the probe on each tick. Closes dogfood-049 gemini
F1.

### F-perms — `0700` / `0600` on scratch state

`go/pkg/supervisor/pointer.go::WritePidfile` and
`go/pkg/supervisor/pty.go::ensureFIFO` now `MkdirAll` with `0o700`;
the pidfile is written `0o600`; `openDevNullOr` opens stdout/stderr
fallback files at `0o600`. Closes dogfood-049 claude F-perms.

### F-store — Postgres-backed `PointerStore`

`go/pkg/db/supervisor_pointers.go` exports
`SupervisorPointerStore{pool *pgxpool.Pool}` with three methods —
`UpsertSupervisorPointer`, `MarkSupervisorLost`,
`GetSupervisorPointer` — backed by UPSERT against
`striatumd.process_supervisor_pointers`. A locally-defined
`PointerRow` mirrors `supervisor.PointerRow` to keep `go/pkg/db`
free of the supervisor import (avoids the obvious cycle). Typed
`ErrSupervisorNotFound` flows out of `Get` and `MarkLost` when no
row matches. Boot-time wire-up into `cmd/striatumd/main.go` is a
V1.7 follow-up.

### F-ci — verify Go binary present

`.github/workflows/ci.yml` adds a "Verify Go binary present" step
on the `daemon-core == 'go'` matrix axis that runs
`test -x go/bin/striatumd` immediately after `make daemon-go-build`.
A missing binary now fails with an `::error::` annotation and a
clear remediation message rather than silently passing through to a
no-op test run. Closes dogfood-049 gemini F6 (CI matrix bypass
risk).

### Known follow-ups (V1.7)

- **macOS process start-time reader.** Replace the Linux-only
  `/proc/<pid>/stat` path with `proc_pidinfo` /
  `sysctl kern.proc.pid.<pid>` so darwin gets the same PID-recycling
  guarantee.
- **Wire Postgres-backed `SupervisorPointerStore` into `cmd/striatumd/main.go`.**
  The store is implemented but not yet dependency-injected into the
  daemon boot path; lands when the daemon switches from in-memory
  fakes to PG-backed in V1.7.
