# RFC 0043: PostgreSQL as the Sole Substrate and Daemon-Required Runtime

Status: accepted / implemented (D094, writable import surface narrowed by D113)
Date: 2026-05-12
Implementation note: bare `STRIATUM_DAEMON_REQUIRED=0` is no longer a
production opt-out. Legacy SQLite paths are reachable only through
explicitly paired test-harness compatibility (`STRIATUM_TEST_HARNESS=1`)
or guarded migration fixtures; the operator-facing writable
`daemon migrate-repo-local` import command is retired by D113.
Context:
[`docs/DECISION_LOG.md`](../decisions/decision-log.md) (D094 supersedes D006/D007/D036
and the SQLite half of D009; D082, D084, D086, D087, D088 cite the daemon-first
trajectory; D028 unaffected),
[`RFC 0028`](0028-long-running-daemon-and-multi-repository-control-plane.md),
[`RFC 0030`](0030-daemon-rpc-server-and-version-skew-protocol.md),
[`RFC 0031`](0031-daemon-owned-supervision-and-sealed-apply-boundary.md),
[`RFC 0032`](0032-cross-repo-workflows-and-mcp-mutation-capabilities.md),
[`RFC 0033`](0033-storage-substrate-rewrite-for-daemon-v2.md),
[`RFC 0035`](0035-multi-repo-test-harness-for-cross-repo-workflows.md),
[`RFC 0039`](0039-go-daemon-core.md) (scope delta required, see §6),
`src/striatum/db.py`,
`src/striatum/schema.py`,
`src/striatum/migrations.py`,
`src/striatum/daemon_pg/`,
`src/striatum/daemon_rpc/`,
`src/striatum/cli/dispatch.py`,
`src/striatum/cli/mutations.py`,
`tests/_harness/MultiRepoHarness`

## Problem

Striatum currently runs two substrates:

- `.striatum/retired-local-state` — repo-local, owns *workflow truth*: runs, jobs,
  sessions, queue messages, leases, work packets, artifact records, verdicts,
  blockers, command requests, process executions, events, worktree rows, and
  supervisor pointers. Mutated directly by the CLI process.
- Daemon-owned PostgreSQL (RFC 0033) — owns *daemon-global state*: registry,
  capability tokens, audit chain, scheduler cursors, RPC request log, daemon
  supervisor metadata, apply receipts, cross-repo run coordination.

D086 deferred the question of repo-local state. RFC 0033's non-goal §1 made
the carve-out explicit: "Replacing repo-local `.striatum/retired-local-state`. That
stays SQLite under D006/D007 unless a future RFC explicitly proposes change."
This is that RFC.

The carve-out has aged badly. Every daemon-V2 RFC since RFC 0028 has had to
bridge two substrates:

- RFC 0031 added `process_supervisor_pointers` to repo-local SQLite as a
  *mirror* of daemon-owned supervisor metadata in Postgres, purely to keep
  workflow-state queries on one side and supervisor-process queries on the
  other.
- RFC 0032 added `runs.cross_repo_run_id` to repo-local SQLite as a pointer
  back to a daemon-owned `cross_repo_run_id` in Postgres. Cross-repo lifecycle
  spans both substrates; crash semantics require reconciling them.
- RFC 0033 §5 had to define "byte-equivalent audit chain" precisely because
  the audit chain crosses the boundary.
- RFC 0030 had to define "compatible repo-local supervisor pointers" because
  daemon-mediated supervision still has to update repo-local workflow state.
- RFC 0035's multi-repo test harness spins up ephemeral Postgres for the
  daemon and creates per-repo SQLite files for workflow state. Every
  integration test pays for two substrates.

Practitioner cost is showing in dogfoods 036, 037, 038, 039 as friction
patterns: cross-substrate consistency questions, two migration systems
(SQLite `PRAGMA user_version` plus Postgres versioned migrations), two test
fixtures, two reconciliation paths. RFC 0039 (Go daemon, proposed) currently
plans to keep SQLite handling for repo-local state — meaning the Go core
must reimplement Python's SQLite layer with no future product reason.

D082 made the daemon the long-term product surface. D086 picked the
substrate. The remaining inertia is the assumption — D006 (SQLite as the v1
coordination layer), D007 (state inside `.striatum/`), the SQLite half of
D009 (binary owns SQLite writes/invariants/leases), and D036 (lazy lease
expiry instead of a background daemon) — that workflow truth has to live
where the CLI process can write it without help.

D094 supersedes that assumption. This RFC specifies the change.

## Goals

- Move every authoritative repo-local table to the daemon-owned PostgreSQL
  schema introduced by RFC 0033, under a per-repo namespace.
- Retire `.striatum/retired-local-state`. Define `.striatum/` as operational
  scratch (FIFOs, pidfiles, supervisor stdout) with no durable workflow
  state.
- Retire direct repo-local CLI mode (the `--no-daemon` path). Every Striatum
  CLI verb routes through the daemon RPC envelope (RFC 0030). CLI without a
  reachable daemon refuses with a documented exit code and platform-specific
  remediation; there is no silent SQLite fallback.
- Specify the original per-repo migration path for the D094 cutover. D113 later
  retired the operator-facing writable import command; current production
  registration paths refuse legacy SQLite and tell operators to archive/remove
  it before registering.
- Expand RFC 0030's method registry to cover every existing repo-local
  mutation (the verbs currently in `src/striatum/cli/mutations.py`) so the
  daemon is a complete RPC server, not a partial one.
- Revise RFC 0039 (Go daemon) scope so it owns the full repo-local mutation
  surface from day one. The language-agnostic envelope, schema, and audit
  chain from RFC 0030/0033 already support it.
- Update `docs/SPEC.md` § "State Store" and § "CLI" to describe the new
  substrate boundary and the daemon-required CLI behavior.
- Make multi-tenancy (`tenant_id` column on every table) a clean follow-up
  RFC by leaving Postgres schemas already keyed by `repository_id` /
  `tenant_id`-ready.

## Non-Goals

- Bundling PostgreSQL with the Striatum distribution. RFC 0033 §8 deferred
  bundled / Dockerized distribution to a follow-up RFC; this RFC inherits
  that deferral. Operators provide a system Postgres exactly as RFC 0033
  required.
- Implementing multi-tenancy. The schema lands ready for a `tenant_id`
  column add, but enforcement, role mapping, and per-tenant capability
  scopes are deferred to a dedicated follow-up RFC.
- Implementing hosted mode (network-accessible daemon, internet-facing
  auth). The daemon stays owner-only per D083. Hosted mode is incremental
  from this base but out of scope here.
- Rewriting `docs/dogfood/<NNN>/` historical scaffolds. They describe the
  V1 substrate and remain frozen.
- Changing D028 (no-transcript artifact policy), D009's "agents call CLI"
  half, or any review / verdict / artifact-publishing semantics. The
  change is substrate-only; behavior at the API surface is preserved.
- Replacing the Python daemon. RFC 0039 (Go daemon) handles that
  transition. RFC 0043 only revises its scope.

## Proposal

### 1. New substrate boundary

The daemon-owned PostgreSQL instance (`daemon_db` per RFC 0033) becomes the
authoritative store for all Striatum state. The schema gains a repo-local
namespace.

Per-repo workflow tables (mirrors of today's `.striatum/retired-local-state`
content, modulo type adjustments for Postgres):

- `runs`, `sessions`, `jobs`, `job_dependencies`, `queue_messages`,
  `leases`, `work_packets`, `artifacts`, `verdicts`, `blockers`,
  `command_requests`, `process_executions`, `events`, `job_worktrees`,
  `process_supervisors`, `process_supervisor_pointers`.

Each table gains a `repository_id UUID NOT NULL REFERENCES repositories
(repository_id)` column and is indexed on `(repository_id, ...)` for the
existing access patterns. The Postgres roles model from RFC 0033 §5
extends to these tables: daemon read-write role for normal operation,
append-only enforcement on `events` and `artifacts` via revoked UPDATE /
DELETE grants, migration role for schema changes.

Schema-version tracking is unified. RFC 0033's daemon-DB migration set
absorbs the repo-local migrations now in `src/striatum/migrations.py`.
The Python SQLite migration list retires.

### 2. `.striatum/` as operational scratch

`.striatum/` survives, intentionally, because lane wrappers, supervised
FIFOs, and pidfiles need a filesystem location that lives next to the
target repository's working tree. After this RFC:

```text
.striatum/
├── scratch/
│   └── <supervisor_id>/
│       ├── stdin.pipe          # supervised wrapper FIFO (RFC 0009)
│       ├── stdout.log          # transient; deleted on supervisor stop
│       └── pid                 # process identity file
├── worktrees/
│   └── <worktree_id>/          # git worktree (RFC 0008); no metadata file
└── bin/                       # optional supervised lane wrappers
```

The daemon runtime token is outside the target repository, under the daemon
runtime directory as `client-token`.

Nothing in `.striatum/` is durable workflow truth. `striatum doctor`
treats files outside this allowlist as warnings, and the `.gitignore`
written by `striatum init` covers the whole directory unchanged.

The `retired-local-state` file is not created on new init. If present from a
prior version it is left alone until the operator archives or removes it.
D113 later retired the operator-facing SQLite import command.

### 3. Daemon-required CLI behavior

Every `striatum` CLI verb routes through the daemon RPC envelope per RFC
0030. There is no direct repo-local path. The pre-RFC-0043 `--no-daemon`
flag is removed (parsing it returns the standard "unknown option" error;
documentation marks the flag as retired).

Verbs that mutate state require a capability-token grant for the verb's
declared capability (`read`, `write`, `review`, `claim`, `apply`,
`admin`, `recovery` per RFC 0032). The daemon is the single writer.

CLI behavior when the daemon is unreachable:

- Exit code **11** (new; reserved here): `daemon_unreachable`.
- Stderr message names the daemon socket path the client tried, names
  the most likely remediation (`systemctl --user start striatumd` on
  Linux with the systemd unit; `launchctl bootstrap` hint on macOS; an
  explicit `striatumd` reminder for foreground operation; Postgres install
  hints if the daemon was never started), and exits.
  No SQLite fallback is attempted, no SQLite file is created or read.
- `daemon doctor` runs even when the daemon is down (it reads
  configuration only) and emits the same remediation list.
- `striatum --help` and `striatum --version` work without a daemon
  because they touch no state.

The CLI is otherwise unchanged. Agents continue to call `claim-next`,
`ack`, `complete`, etc., per D009's "agents call CLI, not raw SQL" half
(preserved). The change is invisible at the verb surface; the daemon
mediates underneath.

### 4. Per-repo migration

D113 later retired this operator-facing writable import surface. The command
shapes below document the original D094 cutover design; current Striatum keeps
the spellings parseable only to return a clear exit-code-12 refusal before
importing SQLite migration code.

The original cutover command was the per-repo analogue of RFC 0033 §4:

```text
striatum daemon migrate-repo-local --repo <path> --dry-run
striatum daemon migrate-repo-local --repo <path>
striatum daemon migrate-repo-local --repo <path> --keep-sqlite-readonly
```

The command's behavior:

1. Authorizes the daemon admin token before opening anything. Refuses
   on missing or weak authorization (exit code 4).
2. Verifies the daemon DB is reachable and has the repo registered
   (per RFC 0028 `repo add`). If the repo is not registered, the
   command runs `repo add` implicitly with `--init=false` and prints
   what it did.
3. Verifies the on-disk `.striatum/retired-local-state` schema version is
   the highest the runner supports. If not, refuses and points the
   operator at an older Striatum release that can bring the legacy SQLite
   source forward first.
   Migration across both substrates simultaneously is not supported
   in this command.
4. With `--dry-run`: enumerates row counts per table, hash anchors
   per artifact, and the existing event-log head. Writes nothing.
   Exits 0 on success, exit code 8 if the SQLite is structurally
   bad.
5. Without `--dry-run`: opens a single Postgres transaction at
   `SERIALIZABLE` isolation, inserts every row preserving original
   `created_at` and `event_id` ordering, replays the event log to
   recompute audit-chain anchors and verifies they match the SQLite
   originals byte-for-byte, writes a checkpoint marker row in the
   `repo_migrations` daemon-DB table, then commits.
6. With `--keep-sqlite-readonly` (default): renames
   `.striatum/retired-local-state` to `.striatum/retired-local-state.tombstone`
   and sets the file mode to 0444. The file is no longer opened by
   any Striatum verb; it is preserved for operator inspection. The
   tombstone is what the operator deletes when they no longer need
   the local mirror.
7. Without `--keep-sqlite-readonly`: deletes the file after a
   `--confirm-delete` flag (which is required when
   `--keep-sqlite-readonly` is explicitly disabled). The operator
   chooses irreversible cleanup explicitly; there is no default
   destructive path.
8. Idempotent re-runs against a fully-migrated repo report
   "already migrated" with the checkpoint marker timestamp and exit
   0.

The migration command preserves D028: the `.striatum/retired-local-state`
file never contained transcripts, and the migration does not record
any new data that was not already in the SQLite.

If the daemon is unreachable, the migration command refuses with the
standard exit code 11 (§3).

### 5. RFC 0030 method registry expansion

Every mutation currently exposed by `src/striatum/cli/mutations.py`
gains a registered RPC method. Capability mapping (read / write /
review / claim / apply / admin / recovery) and repo-scope mode
(single_repo / cross_repo / daemon_global) follow the verb's existing
semantics:

| Verb (illustrative — exact list lives in implementation) | Method | Capability | Scope |
|---|---|---|---|
| `register-session` | `session.register` | `claim` | single_repo |
| `claim-next` | `work.claim_next` | `claim` | single_repo |
| `ack` | `work.ack` | `claim` | single_repo |
| `heartbeat` | `work.heartbeat` | `claim` | single_repo |
| `complete` | `work.complete` | `write` | single_repo |
| `block` | `work.block` | `write` | single_repo |
| `release` | `work.release` | `claim` | single_repo |
| `publish-artifact` | `artifact.publish` | `write` | single_repo |
| `submit-review` | `review.submit` | `review` | single_repo |
| `verdict` | `review.verdict` | `review` | single_repo |
| `decision record` | `decision.record` | `admin` | single_repo |
| `checkpoint resolve` | `checkpoint.resolve` | `admin` | single_repo |
| `recovery requeue-stale` | `recovery.requeue_stale` | `recovery` | single_repo |
| `recovery cancel-job` | `recovery.cancel_job` | `recovery` | single_repo |
| `recovery resume` | `recovery.resume` | `recovery` | single_repo |
| `worktree create` | `worktree.create` | `write` | single_repo |
| `branch confirm` | `branch.confirm` | `admin` | single_repo |
| `run prepare` | `run.prepare` | `admin` | single_repo |
| `run start` | `run.start` | `admin` | single_repo |
| `run pause` / `run resume` / `run cancel` | `run.pause` / `run.resume` / `run.cancel` | `admin` | single_repo |
| `supervise start` / `send` / `stop` | `supervise.*` (already present per RFC 0031) | `claim` | single_repo |
| `workflow validate` | `workflow.validate` | `read` | single_repo |
| `workflow generate` | `workflow.generate` | `write` | single_repo |

Read verbs (`status`, `why`, `dashboard`, `doctor`, `run summary`,
`run graph`, `evidence export`) get matching `read`-capability methods
under the existing dotted vocabulary.

The full method list is enumerated in the RFC 0030 method registry at
implementation time. The point here is the principle: every CLI
mutation has a registry entry; the daemon refuses unrecognized methods
with exit code 10 (per RFC 0030).

### 6. Go daemon scope delta

RFC 0039 originally planned to inherit the daemon's storage surface as
RFC 0033 left it: Postgres for daemon-owned state, SQLite handling for
repo-local state. That plan is obsolete under D094. D107 and RFC 0068
supersede the old Python-primary constraint and make the Go production
daemon port an accepted target on the same Postgres-only substrate.

The Go daemon scope must:

- Drop SQLite from the Go core's scope entirely. The Go daemon owns
  one substrate (Postgres) and one wire protocol (RFC 0030 envelope).
- Cover the full method registry from §5, not a daemon-only subset.
  Every CLI mutation routes through the Go daemon when the Go core
  is selected.
- Reuse RFC 0035's multi-repo test harness (which already pays the
  ephemeral-Postgres cost) to validate the Go core against the same
  acceptance criteria as the Python daemon.
- Make Phase 1 of the Go core the "single-binary single-language
  daemon" — no Python-daemon-mediated fallback for unsupported
  methods, because there is no unsupported method.

The revision is mechanical; the language-agnostic envelope, schema,
and audit chain were designed for exactly this. The risk of *not*
revising is that RFC 0039 ships with a permanent SQLite obligation
that has no product reason.

### 7. Test infrastructure

`tests/_harness/MultiRepoHarness` (RFC 0035) already spins ephemeral
Postgres for the daemon DB. Two changes:

- The harness creates per-repo workflow schemas in the same Postgres
  instance instead of per-repo SQLite files. Schema teardown reuses
  the daemon-DB teardown path.
- Tests that exercise direct CLI mode (the `--no-daemon` path) are
  retired or rewritten to use the daemon-mediated path. The CLI's
  retirement of `--no-daemon` removes the test surface they target.

CI cost stays similar: tests already pay for one Postgres; they no
longer pay for per-test SQLite setup. Net change is neutral-to-favorable.

Tests of the historical migration helper itself (`migrate-repo-local`) use a
golden SQLite fixture. That fixture lives at
`tests/fixtures/v1_repo_local_sqlite/retired-local-state` as the highest
runner-supported V1 schema; after D113 those tests must opt in with
`STRIATUM_LEGACY_SQLITE_IMPORT=1`.

### 8. Multi-tenancy and hosted mode (hooks, not implementation)

The new substrate is ready for two follow-up RFCs that the V1 hybrid
substrate could not support cleanly:

- **Multi-tenancy** is a column add: `tenant_id UUID NOT NULL` on every
  per-tenant table, with `(tenant_id, repository_id, ...)` index
  rewrites and the capability-token model extended to scope tokens to
  a tenant. RFC 0043 does not ship the column; it ships the schema
  pattern that makes the column-add trivial.
- **Hosted mode** (network-accessible daemon, internet-facing auth)
  becomes an incremental change to the daemon's transport surface
  (RFC 0030's owner-only socket grows a TLS endpoint with proper
  auth) and an explicit decision about cross-network capability
  scopes. RFC 0043 does not ship the transport change; it removes the
  architectural blocker.

Neither follow-up is in this RFC's acceptance criteria. They are
called out so reviewers understand the shape of the future the
substrate flip enables.

## Compatibility and Migration

- **Existing target repositories** with `.striatum/retired-local-state` continue
  to require operator cleanup before registration. D113 retired the writable
  SQLite import path; current CLI verbs refuse with exit code 12 and point at
  archive/remove plus `striatum adopt` or `striatum repo add --init`.
- **Existing dogfood scaffolds** under `docs/dogfood/<NNN>/` are frozen
  historical artifacts. Their workflow JSON references and any
  embedded SQLite path strings document the V1 substrate. They are not
  rewritten; running an old scaffold's workflow today produces a
  current-substrate run.
- **Examples** under `examples/` get a one-line check: any reference to
  `.striatum/retired-local-state` is replaced with a substrate-neutral phrase
  ("Striatum's authoritative state") or removed entirely.
- **Test fixtures** for the migration command itself (§7) preserve a
  V1 SQLite snapshot for the migration's regression suite. That fixture
  is generated by the last V1 release of `striatum init` against an
  empty repo plus a small run, and committed under
  `tests/fixtures/v1_repo_local_sqlite/`.
- **Skill bundles** (RFC 0015) and **plugin bundles** (RFC 0025) regenerate
  with new "Do not write to `.striatum/retired-local-state`" language replaced
  by "Do not bypass the daemon; use the supplied runner client commands."
  The underlying invariant — agents do not touch the substrate directly —
  is preserved.
- **`STRIATUM_DAEMON_DB_URL`** remains the configuration entry point.
  Per-repo workflow tables live in the same database; the daemon owns
  the schema mapping. Operators who already configured Postgres for
  RFC 0033 pay no incremental setup cost.
- **Direct CLI mode** is retired immediately at RFC 0043 acceptance.
  The deprecation window is the time between the RFC's acceptance
  and the next minor release; documentation and CHANGELOG announce
  the retirement. There is no "soft retirement" period where both
  modes work, because two-substrate maintenance is the cost this RFC
  exists to remove.

## Downsides and risks

- **Postgres install becomes a hard prerequisite for every Striatum
  user, not only daemon users.** RFC 0033 already paid most of this
  cost (daemon mode required system Postgres), but RFC 0043 makes the
  cost universal. The bundled-distribution follow-up RFC remains the
  honest answer to "first-time operator friction"; this RFC does not
  pretend the cost is zero. `daemon doctor` and `striatum init` both
  emit platform-specific install hints.
- **Direct CLI mode disappears.** Operators who used `--no-daemon` for
  quick experiments lose that path. D113 retired the writable SQLite
  migration commands, so the current operator path is a one-time
  archive/remove of legacy SQLite followed by `striatum adopt` or
  `striatum repo add --init`. The daemon's foreground-mode startup is
  cheap, but the friction is real.
- **CI matrix grows by one Postgres instance per test run that
  previously used SQLite.** RFC 0035 already pays this; the marginal
  cost is zero for tests already on the harness. Tests that have not
  adopted the harness yet need to move. Estimated change: ~20 test
  files.
- **The daemon must own the full method registry from §5 before this
  RFC's acceptance criteria can be met.** This was originally framed
  as Python-daemon implementation work. D107/D111 supersede that
  framing: Go is the production daemon core and Python may remain as
  CLI/web client code. Current conformance work is therefore measured
  against the Go daemon contract, with unsupported retired methods
  removed from the production registry rather than kept as Python-era
  placeholders.
- **Schema-version coupling tightens.** Today, repo-local SQLite and
  daemon-DB Postgres evolve on independent migration tracks. After
  this RFC, every schema change is one place — which is the point,
  but it removes a relief valve.
- **Audit chain rewrites must be flawless.** The migration command
  re-anchors the audit chain in Postgres. Bugs in the re-anchor
  preserve the SQLite chain (because `--keep-sqlite-readonly` is the
  default) but corrupt the new chain. Acceptance criteria require
  byte-equivalent hash verification end-to-end.
- **Recovery semantics shift.** D036's "lazy lease expiry" was a
  V1-era choice predicated on no background daemon. With a mandatory
  daemon, lease expiry can be daemon-managed (background sweeper)
  rather than lazy. This RFC does not redesign recovery — RFC 0020
  remains the recovery design — but the door opens.
- **Schema rewrite is a single large change.** Even though the row
  shapes are mostly mechanical translations from SQLite to Postgres,
  the volume is substantial: 17 tables, indexes, append-only enforcement
  via grants, and migration scripts. Implementation can split across
  multiple PRs but the substrate flip happens all-at-once at the user's
  first migrate invocation.

## Benefits

- One substrate. One migration system. One test harness. One Go-core
  scope. The maintenance debt the hybrid model imposes — bridging
  tables, dual schema versions, dual reconciliation paths — disappears.
- Multi-tenancy is a column add, not a rewrite.
- Hosted mode becomes accessible (with separate auth work).
- RFC 0039 (Go daemon) gets a clean scope: one substrate, one wire
  protocol. Phase 1 is "single-binary Go daemon," not a second
  implementation of the retired repo-local compatibility layer.
- Cross-repo workflows (RFC 0032) stop having to reconcile two
  substrates on every transition.
- MVCC eliminates the WAL contention that the daemon-mediated mutation
  traffic was about to surface (RFC 0033 named this for daemon-owned
  state; it applies equally to high-frequency repo-local writes like
  supervisor heartbeats once they route through the daemon).
- The audit chain is one chain. RFC 0033 §5's hash-anchor matching
  becomes superfluous because there is nothing to match against.
- `daemon doctor` reports one schema version, one substrate health,
  one audit chain — operator clarity wins.
- The daemon-first product story (D082) becomes the only product story.
  Documentation collapses; new-operator onboarding has one path.

## Acceptance Criteria

- `striatum init` against a fresh target repo creates `.striatum/`,
  registers the repo with the daemon, and produces a Postgres schema
  state ready for `run prepare`. No SQLite file is created.
- The original `striatum daemon migrate-repo-local --repo <path>` cutover
  behavior is retained only as historical RFC context; current Striatum returns
  a retired-command refusal before opening SQLite migration code.
- Current registration paths tell operators to archive/remove legacy SQLite
  files before registering with `striatum adopt` or `striatum repo add --init`.
- Every existing CLI verb works end-to-end against the new substrate.
  Per-verb integration tests are added to `tests/_harness/
  MultiRepoHarness` coverage.
- The `--no-daemon` flag is removed; parsing it returns the standard
  "unknown option" error.
- CLI verbs with no reachable daemon exit code 11
  (`daemon_unreachable`) with platform-specific remediation in stderr.
  Documented in `docs/CLI_REFERENCE.md`.
- CLI verbs against an unregistered repo or a repo with legacy SQLite state
  exit code 12 (`repo_not_migrated`) with archive/remove remediation in stderr.
  Documented in
  `docs/CLI_REFERENCE.md`.
- `daemon doctor` reports one substrate (Postgres), one schema version,
  one audit chain, and refuses to start if the daemon role lacks the
  expected privileges (per RFC 0033 §3, extended to the new tables).
- `tests/_harness/MultiRepoHarness` exercises the migration command,
  the daemon-unreachable refusal, the unmigrated-repo refusal, and
  the cross-repo workflow path on the unified substrate.
- `src/striatum/db.py`, `src/striatum/schema.py`, and
  `src/striatum/migrations.py` are retired or reduced to the
  daemon-client schema layer. The Python module surface for direct
  SQLite access is removed.
- `docs/SPEC.md` § "State Store" describes the new substrate. § "CLI"
  documents the daemon-required behavior and the new exit codes.
  `docs/HOW_TO_HUMAN.md` and `docs/HOW_TO_AGENT.md` update their
  bootstrap sections. `docs/UBIQUITOUS_LANGUAGE.md` gains the terms
  in § Domain Modeling below. `docs/MCP.md` documents the expanded
  method registry from §5.
- The superseding Go daemon decision/RFC records the Postgres-only scope
  and is verified against the acceptance criteria of this RFC.

## Open Questions

- **Schema namespacing.** Do per-repo workflow tables live in a single
  Postgres schema (`striatum_repos.runs`, `striatum_repos.jobs`, …)
  keyed by `repository_id`, or in per-repo schemas (`repo_<uuid>.runs`,
  `repo_<uuid>.jobs`)? Recommendation: single schema with
  `repository_id` columns, because cross-repo workflows (RFC 0032)
  already join across repos and per-schema isolation would complicate
  those queries. Reviewers should challenge the recommendation.
- **Connection-pool sizing.** The daemon connects to Postgres on
  behalf of every CLI invocation now. Does the daemon need a per-repo
  connection pool, a global pool with row-level repository scoping, or
  both? Recommendation: global `pgxpool.Pool`-style pool (Python `psycopg`
  pool today), since the daemon is the only Postgres client. Concurrency
  comes from MVCC, not connection multiplicity.
- **Migration command authorization.** Should `migrate-repo-local`
  require an explicit daemon admin token, or accept the daemon's
  startup auto-grant if the operator is on the daemon's machine?
  Recommendation: explicit admin token, matching the RFC 0033 daemon
  migration's auth posture.
- **Exit code numbering.** Codes 11 (`daemon_unreachable`) and 12
  (`repo_not_migrated`) are reserved by this RFC. The current high
  reserved code is 10 (RFC 0030 version-skew refusal). If another RFC
  has claimed 11 or 12 in flight, this RFC takes whatever the next two
  free codes are; the numbering is not load-bearing.
- **Recovery sweeper relocation.** The daemon now owns the one-shot
  `recovery.sweep` mutation surface. `recovery watch` remains foreground
  CLI scheduler glue over that RPC so its pidfile, signal handling, and
  JSONL stream stay process-local while mutation authority stays in the
  daemon.
- **Web UI workflow file references.** RFC 0024's workflow browser
  references files in the target repo's working tree. The daemon now
  mediates state, but the workflow JSON is still a file the agent
  authors. Confirm the boundary is "files in the repo are the agent's;
  state in the daemon is the runner's." Probably already true, worth
  explicit confirmation in the SPEC update.
- **Direct-mode retirement timing.** RFC 0043's text retires
  `--no-daemon` immediately at acceptance. Is one deprecation cycle
  worth the maintenance cost? Recommendation: no. The supersession is
  the point; a parallel-mode period reintroduces the hybrid this RFC
  removes.
- **Bundled-distribution follow-up.** RFC 0033 §8 deferred bundled /
  Dockerized distribution. This RFC inherits the deferral but makes
  the bundled question more urgent (every operator hits the install
  bar now, not only daemon adopters). Should the bundled-distribution
  RFC be scheduled in the next acceptance window?

## Domain Modeling

Terms to add to `docs/UBIQUITOUS_LANGUAGE.md` after acceptance:

- **Repo-local state (post-D094)** — the per-repository workflow tables
  (`runs`, `jobs`, `sessions`, `queue_messages`, `leases`,
  `work_packets`, `artifacts`, `verdicts`, `blockers`,
  `command_requests`, `process_executions`, `events`, `job_worktrees`,
  `process_supervisors`, `process_supervisor_pointers`) hosted in the
  daemon-owned PostgreSQL instance under a `repository_id` scope.
  Replaces the V1 sense of `.striatum/retired-local-state`.
- **Operational scratch** — the post-D094 role of `.striatum/`: a
  filesystem location next to the target repo for supervised wrapper
  FIFOs, pidfiles, transient supervisor stdout, optional lane wrappers, and
  plugin scratch. The daemon runtime token lives under the daemon runtime
  directory as `client-token`. Nothing here is durable workflow truth.
- **Daemon-required CLI** — the post-D094 default and only CLI behavior.
  Every verb routes through the daemon RPC envelope; the daemon is the
  single writer. There is no SQLite fallback.
- **Tombstone SQLite** — the read-only `.striatum/retired-local-state.tombstone`
  file created by historical migration fixtures or pre-D113 cutovers. It is
  not opened by any Striatum verb; it is preserved for operator inspection
  until the operator removes it.
- **Repo-local migration checkpoint** — the marker row in
  `repo_migrations` (daemon-DB table) that recorded historical successful
  per-repo migrations. It remains for bounded verification/fixture paths, not
  as a current operator import workflow.
- **Repo-local schema version** — retired. The daemon-DB schema
  version (RFC 0033 substrate version) now covers all schemas.

DDD framing (per [`docs/DDD.md`](../reference/domain-driven-design.md)): the change is a
**boundary clarification**, not a new aggregate. The "live state"
bounded context expands from "repo-local SQLite plus daemon Postgres"
to "daemon Postgres, namespaced by repository." Aggregate roots
(Run, Session, Job, Lease, Artifact, Verdict, Blocker) are unchanged
in their identity and invariants; their storage substrate is the
implementation detail this RFC moves. The original CLI-only write-boundary
invariant (D009 first half) becomes a daemon-method write-boundary
invariant under D104: CLI is one approved local client, not the sole
production interface.

## V1.5 deltas

V1.5 closes the four follow-up findings folded under decision D102
(cycle-exhaustion override on dogfood-048). The shape and product
boundary of V1 are unchanged — V1.5 is purely the gap closure list.
See `docs/dogfood/050/DESIGN_SYNTHESIS.md` for the design notes.

### F-parser — wire `daemon migrate-repo-local`

D113 later narrowed this parser surface to compatibility-only refusal. The
V1.5 details below are retained as historical gap-closure context.

V1 shipped the migration body (`migrate_repo_local()`) and the
`src/striatum/cli/daemon.py` helper but did not register the
subparser in `src/striatum/cli/parser.py`, so the operator command
`striatum daemon migrate-repo-local --help` returned an unknown
subcommand. V1.5 adds the subparser under `daemon` and a dispatch
arm in `dispatch._dispatch_daemon` that routes the verb into
`striatum.cli.daemon:dispatch_daemon`. The subparser exposes
`--from {sqlite}`, `--to {pg}`, `--repo` (falls back to the
top-level `--repo`), `--postgres-url`, `--dry-run`,
`--keep-sqlite-readonly` (default true), `--no-keep-sqlite-readonly`,
`--confirm-delete`, and `--json`.

### F-escape — daemon-required is the default

V1 left `resolve_requirement` in `src/striatum/cli/daemon_required.py`
env-gated on `STRIATUM_DAEMON_REQUIRED=1`, so the default CLI
behavior silently fell through to the SQLite path — directly
contradicting §3 of this RFC. V1.5 flips the resolver:

* Bare `STRIATUM_DAEMON_REQUIRED=0` is no longer a production opt-out.
  Legacy SQLite-backed fixtures require the explicit paired test-harness
  context (`STRIATUM_DAEMON_REQUIRED=0 STRIATUM_TEST_HARNESS=1`) or a
  migration fixture.
* Any other value, including the env var being unset, returns a
  populated `DaemonRequirement` and the dispatcher enforces the
  exit-code-11/12 refusals before any SQLite-backed code runs.

The CLI escape-path audit (`src/striatum/cli/`) confirmed the
top-level `enforce_daemon_required()` gate is the only silent-
fallback gate; the per-verb mutation, introspection, recovery, and
worktree slices are reached only after the dispatcher calls
`enforce_daemon_required(args.command, repo)`.

### F-test — end-to-end exit-code-12 coverage

V1 had no end-to-end test that proved a real `striatum --repo
<unmigrated> status` invocation exits with code 12 and the
then-current `striatum daemon migrate-repo-local ...` remediation hint. V1.5 adds
two cases in `tests/exit_codes/test_rfc0043_refusals.py`:

* `test_dispatch_returns_exit_12_for_unmigrated_repo` — binds a
  Unix socket so the daemon-reachability check passes, drops an
  empty `.striatum/retired-local-state` to present the pre-cutover disk
  signal, then asserts `dispatch_mod.main(["--repo", str(tmp_path),
  "status"])` returns `12`, that stderr contains
  `repo_not_migrated`, and that the remediation line names
  `striatum daemon migrate-repo-local --from sqlite --to pg --repo`
  with the resolved repo path.
* `test_dispatch_exit_12_json_envelope` — same fixture, asserts the
  `--json` envelope `{"ok": false, "error": {"code": 12, ...}}`
  carries the structured `hint` naming the migrate command.

The existing `test_resolve_requirement_returns_none_without_env`
case is replaced by
`test_resolve_requirement_enforces_by_default_when_env_unset` plus
`test_resolve_requirement_opt_out_with_env_zero` to lock in the
flipped default.

### F-crash — sentinel-based crash-resume

V1 committed Postgres state then performed the SQLite tombstone or
delete outside the transaction. A crash between those two steps
left the operator with a migrated repo plus a still-writable
`.striatum/retired-local-state` — a silent split-brain. V1.5 hardens this
with a checkpointed-resume design (transactional rollback was
rejected because the SQLite filesystem rename cannot participate
in the Postgres transaction):

1. After the Postgres `SERIALIZABLE` transaction commits, the
   migration writes the sentinel
   `.striatum/retired-local-state.migrated` atomically — JSON body
   containing `repository_id`, `source_state_db_sha256`,
   `keep_sqlite_readonly`, `confirm_delete`, and `written_at`.
   The write uses a `*.tmp` file + `os.fsync` + `replace` so a
   crash between the Postgres commit and the SQLite finalization
   leaves either the old state or the fully-written sentinel.
2. The original `_tombstone_or_delete_state_db` call runs.
3. On success, the sentinel is removed.

Both `already_migrated` early-return paths (the SQLite-missing
branch in `migrate_repo_local` and the in-transaction branch in
`_migrate_full`) call the new helper
`_resume_sqlite_finalization_after_checkpoint()` before returning:

* If `retired-local-state` is still on disk, the helper verifies its
  SHA against `checkpoint["source_state_db_sha256"]` (refusing
  with exit code 8 on mismatch — non-destructive), then resumes
  the original tombstone/delete action recorded in the sentinel,
  clears the sentinel, and returns the `sqlite_finalization`
  result with `resumed_from_checkpoint: true`.
* If only the sentinel remains, it clears the orphan and reports
  `cleared_orphan_sentinel`.
* If neither exists, it returns `None` (fully finalized).

The regression coverage is at
`tests/daemon_pg/test_repo_local_migration_crash_resume.py` —
pure-Python helper tests plus a Postgres-backed end-to-end test
that monkeypatches `_tombstone_or_delete_state_db` to simulate the
mid-finalization crash and asserts that the rerun lands the
`0444` tombstone idempotently.

No new SQL file is required under `src/striatum/daemon_pg/sql/`;
V1.5 is schema-additive but this fix needs no schema change. The
existing `--keep-sqlite-readonly` tombstone semantics are preserved
in normal migration, resumed migration, dry-run, and
already-migrated paths.
