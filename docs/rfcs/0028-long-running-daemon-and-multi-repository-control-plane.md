# RFC 0028: Long-Running Daemon and Multi-Repository Control Plane

Status: superseded V1 foundation
Date: 2026-05-10
Context:
[`docs/SPEC.md`](../reference/spec.md),
[`docs/MCP.md`](../reference/mcp.md),
[`RFC 0012`](0012-local-service-api.md),
[`RFC 0013`](0013-local-web-ui.md),
[`RFC 0020`](0020-autonomous-stalled-run-recovery.md),
[`RFC 0023`](0023-web-chat-and-browse.md),
[`RFC 0024`](0024-workflow-browser-and-builder.md),
[`RFC 0026`](0026-lane-attestation-and-operator-byline-honesty.md),
[`RFC 0027`](0027-sealed-patch-provenance-mode.md),
[`docs/DECISION_LOG.md`](../decisions/decision-log.md) (D006, D007, D009,
D020, D028, D036, D049, D058, D059, D066, D068, D074, D075,
D076-D080)

Superseded by: RFC 0030, RFC 0033, RFC 0032, D087, D094, and D104. This
RFC is retained as the historical V1 daemon foundation: registry +
foreground sweep, resources-only daemon MCP, metadata-only audit, no RPC
server. Current production behavior is daemon RPC, daemon-owned
PostgreSQL, daemon-required CLI/MCP/web clients, and mutation-capable MCP.

## Problem

Striatum's current center of gravity is a short-lived CLI process over a
repo-local SQLite file. That shape is intentionally conservative and has
served the V1 product well: easy to inspect, easy to delete, no hidden
coordinator, no hosted service, no terminal-output state. It also creates a
ceiling.

Every workflow operation is episodic. A command starts, opens the state
database, performs one mutation or read, and exits. Long-running concerns
are therefore implemented as separate, partial mechanisms:

- `striatum serve` exposed a local HTTP/Unix-socket service. In V1 it was an
  adapter over `striatum.api.invoke`; current production service calls
  daemon RPC for daemon-owned state.
- `python -m striatum.mcp` exposes a stdio MCP-like wrapper, but it is scoped
  to one target repository and one child-process session.
- `striatum supervise` records long-lived agent processes, but supervision
  is still operated through CLI calls and named pipes.
- `recovery watch` loops around one run, but each watcher is separate and
  does not become a global scheduler.
- The web UI can introspect and mutate a run, but it is still backed by
  request-time CLI/API dispatch and per-repo state.

This is fine for one run in one repository. It becomes awkward when a human
or operator surrogate wants to watch ten repositories, compare all active
blockers, keep recovery alive overnight, maintain several supervised agents,
serve MCP tools to multiple clients, and enforce provenance policy from a
single local control plane.

The deeper product question is whether Striatum should remain "a repo-local
CLI that happens to have services" or grow a first-class daemon: a local,
long-running, multi-repository orchestration process whose clients are the
CLI, MCP, web UI, and agent plugins.

## Goals

- Design `striatumd`, an optional long-running local control plane that can
  manage multiple target repositories and multiple active workflows at once.
- Treat CLI, MCP, local HTTP, web UI, and future desktop/chat surfaces as
  clients over the same command model rather than as separate control planes.
- Preserve Striatum's principles, not necessarily its current
  implementation: local-first operation, structured commands as the only
  workflow mutation surface, no marker-file state, no terminal-output state,
  no broad transcript capture by default, and explicit provenance boundaries.
- Make recovery and supervision resident capabilities instead of
  user-launched loops.
- Provide a global dashboard and MCP resource surface across all registered
  repositories, runs, jobs, sessions, blockers, supervisors, receipts, and
  human checkpoints.
- Open storage and language choices honestly, including the possibility that
  SQLite and Python are not the final daemon substrate.
- Keep a compatibility path for existing repo-local CLI workflows during the
  transition.

## Non-Goals

- No hosted Striatum service in this RFC. The daemon is local by default.
- No automatic replacement of the current CLI in one step.
- No remote multi-user SaaS semantics in V1.
- No weakening of RFC 0026/0027 provenance boundaries. This RFC is sequenced
  after the provenance-failure work because a daemon concentrates authority.
- No commitment to SQLite, Python, Go, JavaScript, or any other implementation
  language in the proposal itself.
- No transcript capture by default. Long-lived agent sessions remain
  artifact/verdict/receipt driven unless a future product decision changes
  D028.
- No assumption that MCP clients are trusted. MCP is an operator surface with
  explicit authorization, not an implicit root capability.

## Proposal

### 1. Product shape

Introduce an optional `striatumd` process. The daemon is the resident local
control plane. Existing CLI verbs become client calls into it when a daemon
is active; without a daemon they continue to use today's direct local mode
until compatibility is explicitly retired.

The daemon owns:

- registered target repositories;
- workflow validation and snapshots;
- run lifecycle;
- job scheduling, dependency gates, leases, and claimability;
- session registration and lane attestation;
- supervised agent process lifecycle;
- recovery policies and timers;
- event streaming;
- artifact validation and receipt generation;
- MCP tools/resources;
- web UI state;
- local auth and client capability policy.

The daemon does **not** decide review truth, synthesize artifacts on its own,
parse terminal output as state, or treat itself as a hidden model agent. It
is a deterministic orchestration service.

### 2. Tenancy model

"Multi-tenant" in the first daemon design should mean local tenants, not
hosted organizations. V1 candidates:

- **Repository tenant:** each registered target repository is a tenant with
  its own policy, workflows, runs, state, and artifacts.
- **Operator tenant:** each local human/operator identity has capabilities
  over one or more repositories.
- **Client tenant:** each connected MCP/web/CLI client receives a capability
  set for read, claim, review, apply, recovery, and admin actions.

The daemon must make the unit explicit. A single-user laptop can run with
one operator tenant and many repository tenants. A shared workstation can
use OS identity, Unix-socket permissions, or local tokens to prevent one
operator client from mutating another repository's runs.

The term "tenant" must not imply hosted persistence, remote accounts, or
central telemetry.

### 3. Client surfaces

The daemon exposes a small set of local transports:

- Unix-domain socket by default.
- Loopback HTTP for the web UI and simple clients.
- MCP server over stdio and/or socket for agent tools.
- Optional WebSocket/SSE event stream for live dashboards.

The CLI becomes a client:

```text
striatum daemon start
striatum daemon status
striatum repo add /path/to/repo
striatum repo list
striatum run prepare --repo <repo-id> --workflow <path>
striatum dashboard --all
```

The MCP surface becomes first-class rather than an adapter over one repo:

- resources: `striatum://repos`, `striatum://runs`, `striatum://run/<id>`,
  `striatum://job/<id>`, `striatum://blockers`, `striatum://receipts`;
- tools: `repo_add`, `workflow_validate`, `run_prepare`, `run_start`,
  `register_session`, `claim_next`, `ack`, `heartbeat`, `publish_artifact`,
  `submit_review`, `complete_job`, `apply_reviewed_patch`, `doctor`,
  `recovery_sweep`;
- prompts: operator boundary, claim loop, reviewer posture prompts,
  implementation handoff prompts.

MCP mutations should default to read-only unless the client connection has a
mutation capability. This mirrors the current `striatum serve
--allow-mutations` caution, but the daemon should model it as capability
authorization rather than one global flag.

### 4. Scheduling and resident recovery

The daemon can replace today's lazy/episodic recovery with a real scheduler:

- periodic stale-lease sweeps across all active runs;
- supervised process liveness probes;
- recovery policy execution without per-run watcher commands;
- checkpoint timeout escalation;
- auto-requeue for review-only work where policy allows it;
- global "stuck runs" queue;
- event notifications to web/MCP/CLI clients.

This does not need to make recovery more aggressive. It can preserve D036's
safety policy while removing the need for a human to keep poking the CLI.

### 5. Agent supervision

Long-running agent processes become native daemon children rather than
rows operated by CLI commands. The daemon owns:

- process start/stop/restart;
- PTY vs pipe selection;
- packet delivery;
- heartbeat timers;
- liveness probes;
- scratch directories;
- lane attestation state;
- provenance mode constraints.

The daemon still must not parse stdout/stderr as workflow state. Agent
outputs remain durable artifacts, verdicts, blockers, receipts, and
structured command calls.

### 6. Storage architecture options

This RFC does not assume the current SQLite design must remain. It presents
four options for design review.

#### Option A: Per-repository SQLite, daemon multiplexing

Each target repository keeps `.striatum/retired-local-state`. The daemon registers
many repos and opens the relevant DB per operation.

Benefits:

- best backward compatibility;
- repo-local cleanup/export story stays intact;
- existing migrations and tests remain useful;
- failure blast radius is one repo.

Downsides:

- global queries require fan-out across many DBs;
- daemon-global identity/capability/process state needs another store;
- multi-repo workflows are awkward;
- per-repo SQLite files remain writable unless paired with sealed-mode
  authority changes.

#### Option B: Central daemon SQLite

The daemon stores all repository/run/job/session/process state in one local
database, for example under `~/.local/share/striatum/striatumd.sqlite3`.

Benefits:

- simple global dashboard and query model;
- one scheduler store;
- easier capability and client-session management;
- no multi-DB transaction problem for cross-repo workflows.

Downsides:

- weakens the repo-local state promise;
- harder fresh-clone audit story;
- repository deletion no longer deletes all live state;
- central DB corruption affects all runs.

#### Option C: Hybrid registry plus per-repository run stores

The daemon has a central registry for repos, clients, capabilities,
supervisors, and global scheduling, while each repository keeps its run
history and artifacts in repo-local state.

Benefits:

- preserves much of the repo-local model;
- enables global visibility and resident scheduler;
- lets high-value daemon concerns live centrally;
- migration can be gradual.

Downsides:

- more conceptual complexity;
- split-brain risk if central registry and repo DB disagree;
- cross-repo transactions remain hard;
- tests must cover both layers.

#### Option D: Replace SQLite

Use a different durable substrate for daemon mode. Candidates include:

- embedded PostgreSQL-like engine;
- external local PostgreSQL;
- RocksDB/Badger-style key-value store;
- event-sourced append log plus indexed projections;
- libSQL/Turso-style embedded replication later, if local-first remains
  intact.

Benefits:

- could support stronger concurrency, streaming projections, and
  multi-tenant queries;
- may better fit daemon-owned event sourcing;
- can separate command log from read models cleanly.

Downsides:

- larger operational footprint;
- distribution pain;
- more ways to violate local-first simplicity;
- migration cost may dwarf the value before the daemon product is proven.

Recommended V1 path: Option C. Keep repo-local run state while adding a
central daemon registry. Revisit Option D only after daemon mode has real
load and pain.

### 7. Implementation language options

This RFC deliberately permits a rewrite, but it should not romanticize one.

#### Stay in Python

Benefits:

- reuses current validator, migrations, CLI, web UI, service, MCP wrapper,
  and tests;
- fastest path to a daemon prototype;
- least disruptive while RFC 0026/0027 are still settling.

Downsides:

- packaging a robust long-running service is clunkier;
- process supervision and PTY behavior are more fiddly;
- concurrency model is adequate but not inspiring;
- static single-binary distribution is hard.

#### Rewrite daemon core in Go

Benefits:

- strong fit for resident process supervision, sockets, signals, and
  concurrency;
- single binary distribution;
- easier launchd/systemd/Homebrew story;
- crisp daemon/client split.

Downsides:

- rewrite risk;
- duplicated workflow validation semantics;
- Python package compatibility break;
- two-language repository unless everything moves.

#### Build daemon/web in TypeScript or JavaScript

Benefits:

- excellent web/MCP ecosystem velocity;
- easy sharing between web UI and daemon client models;
- good developer UX for plugins and UI.

Downsides:

- less natural for local process supervision;
- Node runtime dependency changes the installation feel;
- durable local database and signal handling need careful discipline.

Recommended V1 path: prototype in Python, design the daemon/client protocol
so a Go daemon can replace the Python daemon later. If the daemon becomes the
primary product, write a follow-up RFC evaluating a Go core before any
large-scale rewrite.

### 8. Compatibility and migration

Daemon mode must not strand existing users. The migration should be phased:

1. Add daemon registry and read-only multi-repo dashboard.
2. Add daemon-backed CLI client mode while direct CLI mode remains default
   (V1 shipped registry-backed read mode; RPC client mode is deferred).
3. Move recovery scheduling and global doctor into the daemon.
4. Move supervision into the daemon.
5. Add daemon-backed MCP with read-only default capabilities (V1 shipped
   registry-backed resources-only MCP; RPC transport is deferred).
6. Add mutation capabilities for trusted clients.
7. Decide whether direct SQLite CLI mode remains a supported fallback or
   becomes a compatibility mode.

Existing `.striatum/retired-local-state` runs should be importable/registered
without data rewrite where possible.

### 9. Provenance and trust implications

A daemon concentrates authority. That is a benefit only if the authority is
explicit and defended.

RFC 0026 and RFC 0027 should land first because daemon mode otherwise makes
the operator bypass cleaner to automate. Once sealed provenance exists, the
daemon can become the natural home for:

- lane attestation;
- patch capture;
- protected apply;
- signing keys;
- provenance receipts;
- capability tokens;
- MCP mutation authorization.

The daemon should never claim more provenance than the active mode provides.
In `advisory` mode, it is still advisory. In `attested_bylines`, it improves
identity claims but not source-byte provenance. In `sealed_patch`, it can own
the apply service and signing material.

### 10. Security model

Minimum local daemon security:

- Unix socket default with owner-only permissions.
- Loopback HTTP only unless a separate RFC accepts remote serving.
- Per-client capabilities rather than a single global `--allow-mutations`.
- Constant-time token compare for HTTP bearer tokens.
- Audit log for every mutating client call.
- Explicit repository registration.
- Refuse symlink/path traversal across repository boundaries.
- Never expose `.striatum/retired-local-state` raw writes.
- Keep transcripts off by default.

Future multi-user local mode needs:

- OS-user identity mapping;
- repo-level ACLs;
- capability revocation;
- per-client session expiry;
- lockout and compromised-client recovery.

### 11. Downsides and risks

- A daemon is a larger trust boundary than a CLI.
- A daemon bug can affect many repositories at once.
- Central registry state can become another thing to back up, migrate, and
  repair.
- Users may confuse local daemon with hosted service expectations.
- Long-running processes need lifecycle management: launchd, systemd,
  login items, pidfiles, logs, upgrades, and crash recovery.
- Version skew appears between daemon and CLI clients.
- MCP mutation tools increase the blast radius of prompt injection unless
  capability gates are conservative.
- Optional daemon mode can create two product paths unless the docs are
  ruthless about what is primary.

### 12. Benefits

- One place to see every active run and blocker.
- Real multi-repository operator ergonomics.
- Resident recovery without per-run watcher commands.
- Native long-lived agent supervision.
- Cleaner MCP: agents call tools against a live control plane instead of
  shelling out or spawning per-repo stdio wrappers.
- Better web UI and notifications.
- Stronger sealed provenance because apply/signing authority can live in
  the daemon instead of a short-lived operator process.
- Clearer future for desktop apps, menu-bar status, editor integration, and
  local automation.

## Acceptance Criteria

This RFC is intentionally architectural. A first accepted implementation
slice should demonstrate:

- A daemon process can register at least two target repositories.
- A global read-only dashboard lists runs, blockers, claimable jobs, and
  stale leases across registered repositories.
- The daemon exposes an MCP resource list spanning multiple repositories.
- The CLI can call the daemon for read-only `status`, `doctor`, and
  dashboard operations.
- Direct CLI mode still works for existing workflows.
- The daemon records every client request with client id, repository id,
  command, authorization result, and timestamp.
- The daemon refuses mutation tools unless a client capability explicitly
  permits them.
- The daemon can run a recovery sweep across all active runs without a
  separate `recovery watch` process per run.
- Tests cover daemon restart with a pre-existing registry and at least one
  registered repo-local state store.
- Docs clearly state that daemon mode is local-first and optional, and that
  provenance guarantees still depend on the selected provenance mode.

## V1 Implementation Notes

Dogfood-031 accepted and implemented the V1 acceptance-criteria slice.
The shipped surface is intentionally narrower than the full proposal:
`striatumd` / `striatum daemon start`, daemon registry, `repo
add/list/remove`, explicit `--daemon` read routing for `status`,
`doctor`, `why`, global `dashboard --all`, resources-only daemon MCP,
metadata-only hash-chained audit, and a foreground daemon sweep over
active registered runs.

Revision round 3 further narrowed the honesty boundary: V1 is
registry-backed multi-repository coordination plus a foreground sweep loop,
not a daemon RPC server. CLI and MCP clients open the owner-only registry
SQLite directly under token/capability checks; the `striatumd` Unix socket
is a lifecycle marker in this implementation, not a request router. The
full daemon-mediated socket/HTTP protocol described by this RFC is deferred
to a follow-up RFC.

Revision round 2 tightened the V1 implementation boundary: unsupported
forced-daemon verbs now refuse instead of falling back to direct mode,
`repo add` authorizes before repo-local access and requires explicit
`--init` for absent state databases, daemon MCP resource requests carry an
explicit token and filter repo-scoped reads, closed audit segment manifests
are guarded and checked by doctor, and foreground sweeps write repo-local
`daemon.recovery_sweep` events with `author: striatumd-<instance-id>`.
Sweep cursors can enter `sweep_degraded`, and doctor surfaces both degraded
sweeps and duplicate `recovery watch` schedulers for registered runs.

Deferred from V1: cross-repository workflows, ordinary workflow
mutations through the daemon, a daemon RPC server, daemon-owned
supervision, MCP mutation tools, sealed apply, signing keys,
service-manager installation, audit retention/rotation, Windows daemon
support, hosted semantics, and local multi-user operator tenancy.

## Open Questions

Status after V1 (2026-05-11): four product-level questions are resolved as
plain decisions; four become the spine of follow-up RFCs; one stays an open
TODO.

Resolved as decisions:

- ~~Is the long-term product "CLI with optional daemon" or "daemon with CLI
  client"?~~ **Resolved by D082**: daemon with CLI client. Daemon mode is the
  primary product surface for sealed provenance, cross-repo coordination,
  supervised agent ownership, and MCP mutation. Direct CLI mode is a
  compatibility shim retired by a future RFC.
- ~~Does local multi-user mode matter, or is one human/operator per machine
  sufficient?~~ **Resolved by D083**: single OS user per machine for daemon
  V2. Multi-user is deferred to a dedicated future RFC.
- ~~Is Python acceptable for a long-running control plane, or should a Go core
  be designed before implementation starts?~~ **Resolved by D084**: plan a Go
  core. RFC 0030 designs a language-agnostic protocol so a Go daemon can
  replace the Python daemon cleanly; the first daemon-first RPC server may
  still ship in Python.
- ~~How should daemon logs be stored without becoming transcript-like
  sensitive material?~~ **Resolved by D085**: metadata-only audit by default;
  opt-in `--verbose-log` flag for operator debugging with a session-scoped
  rotated file and automatic deletion timer.

Becomes the follow-up RFC trio:

- Should daemon mode become required for sealed provenance? → **RFC 0031**
  (daemon-owned supervision + sealed-apply boundary). Daemon-first product
  positioning makes daemon-required-for-sealed the natural alignment.
- What is the upgrade story when CLI and daemon versions differ? → **RFC
  0030** (daemon RPC server + version skew protocol). Wire-protocol version
  handshake, capability binding to RPC routes, and explicit refusal /
  downgrade semantics across daemon-CLI version pairs.
- Should cross-repository workflows be in scope, or is multi-repository
  introspection enough for V1? → **RFC 0032** (cross-repo workflows + MCP
  mutation capabilities). Daemon-first transactions make cross-repo possible;
  scope and semantics need their own design + dogfood cycle.
- Which capabilities are safe to expose through MCP by default? → **RFC
  0032**, alongside cross-repo. MCP mutation defaults depend on daemon RPC
  capability vocabulary and audit guarantees from RFC 0030.

Storage substrate joins the follow-up RFC set:

- ~~Is Option C storage enough, or should daemon mode move immediately to a
  central event log?~~ **Resolved by D086**: daemon V2 will not stay on
  SQLite. **RFC 0033** evaluates non-SQLite substrates (event-sourced log,
  libSQL/Turso, embedded Postgres, RocksDB/BoltDB, ...) and picks one. RFC
  0033 lands first because RFC 0030's wire protocol, schema migrations, and
  audit-chain format key off the substrate choice. Repo-local
  `.striatum/retired-local-state` may stay SQLite indefinitely; the rewrite is for
  daemon-owned state, not repo-owned state.

## Domain Modeling

This RFC adds terms that should land in `docs/UBIQUITOUS_LANGUAGE.md` only
after acceptance.

- **Striatum daemon** - long-term term for a local deterministic control
  plane process. In the shipped V1 implementation this term means a shared
  owner-only registry SQLite plus foreground recovery sweep loop; scheduling
  over RPC, supervision, event streaming, and daemon-mediated client routing
  are deferred.
- **Repository tenant** - a registered target repository managed by the
  daemon, with its own workflows, runs, policies, state, and artifacts.
- **Operator tenant** - a local operator identity or trust zone with
  capabilities over one or more repository tenants.
- **Client capability** - a value object granting a connected CLI, MCP, web,
  or plugin client permission to perform a bounded set of read or mutation
  commands.
- **Daemon registry** - central daemon state for registered repositories,
  clients, capabilities, active supervisors, and global scheduling metadata.
- **Global dashboard** - a registry-backed view over all registered
  repositories and active runs.

The daemon is not a new domain authority that invents workflow truth. It is a
resident implementation of the same bounded context: structured commands,
explicit state transitions, durable artifacts, and review/acceptance gates.
