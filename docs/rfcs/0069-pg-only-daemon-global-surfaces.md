# RFC 0069: PostgreSQL-Only Daemon Global Surfaces

Status: implemented (residual: optional polish) — PG-backed daemon-global reads/sweeps are in place (startup bootstrap, health, audit, doctor, `dashboard.all`, Go `status`, daemon MCP resources, the Go resident recovery scheduler) and the SQLite registry is fail-closed; the residual is the optional Open-Question polish below (generating registry-probe/diagnostic paths from the method contract). Currency-promoted in D245 (2026-06-20, RSA-007) after a verifying grep found no live registry/Python fallback in error paths.
Date: 2026-05-17
Context: [RFC 0043](0043-postgres-as-sole-substrate-and-daemon-required-runtime.md), [RFC 0059](../records/_frozen/rfcs/0059-eradicate-legacy-sqlite-fallbacks.md), [RFC 0060](0060-single-daemon-method-contract-source.md), [RFC 0068](0068-go-production-daemon-port.md), [REMEDIATION_SYNTHESIS_2026-05-17](../architecture/REMEDIATION_SYNTHESIS_2026-05-17.md)

## Problem

The repo registrar has moved to PostgreSQL, but some daemon-global surfaces in
the current Python daemon still call the legacy SQLite registry helpers. That
keeps a second state authority reachable from production daemon code and
weakens both RFC 0043 and the RFC 0068 Go-port target.

Known surfaces included daemon startup bootstrap, `dashboard.all`, the Python
`daemon_sweep_once` registry path, daemon MCP resource list/read helpers,
daemon lifecycle helpers, daemon health, and daemon audit/doctor probes.
Startup bootstrap, dashboard-all subset, daemon MCP resources,
PostgreSQL-backed daemon lifecycle/health/audit/doctor reads, and the Go
resident recovery scheduler have landed. The load-bearing PG-only daemon-global
surface has shipped; the only residual is optional polish (the Open Question
below on generating registry-probe/diagnostic paths from the method contract).

## Goals

- Make production daemon-global reads and sweeps PostgreSQL-only.
- Make the Go daemon the target owner for these global surfaces.
- Permit legacy SQLite registry access only in migration and explicitly gated
  test-harness compatibility paths.
- Fail closed when a PostgreSQL DTO or handler is missing instead of opening
  SQLite.
- Keep `repo.add`, `repo.list`, and `repo.remove` PG-native.

## Non-Goals

- Delete every historical SQLite migration fixture.
- Change the daemon RPC envelope or capability vocabulary.
- Flip the default daemon core; RFC 0068 owns the full port and retirement
  sequence.

## Proposal

Add a production registry tripwire and port daemon-global surfaces in order:

1. Guard `connect_registry()` so production calls are impossible outside
   migration/test compatibility.
2. Move daemon startup bootstrap, health, audit, and doctor data to
   `striatumd.*` tables.
3. Replace the Python `daemon_sweep_once()` registry path with a
   PostgreSQL-backed scheduler cursor that invokes the existing PG
   `recovery.sweep` handler per active run.
4. Rebuild `dashboard.all` and daemon MCP resources from daemon PostgreSQL rows
   and repository-scoped PG read handlers.
5. Mark missing global DTOs as `not_implemented` until implemented; do not
   fall back to SQLite.

## Acceptance Criteria

- With `STRIATUM_SQLITE_CONNECT_TRIPWIRE=1` and no test/migration escape,
  production daemon-global commands either use PostgreSQL or fail before
  `connect_registry()`.
- `dashboard.all`, `striatum://daemon/repos`,
  `striatum://daemon/dashboard`, and repository MCP resources work against a
  PG-only fixture.
- The daemon recovery scheduler records PostgreSQL recovery events/cursors and
  never opens or creates `.striatum/retired-local-state`.
- Regression tests cover daemon start, dashboard-all, daemon sweep, MCP
  resources, health, and audit/doctor paths.

## Implementation Notes

- Production `connect_registry()` is now gated behind the paired test-harness
  escape (`STRIATUM_TEST_HARNESS=1` and `STRIATUM_DAEMON_REQUIRED=0`);
  the old standalone legacy-registry opt-in is no longer exported or reported
  by daemon diagnostics.
- Python daemon startup with PostgreSQL configured now uses
  `striatumd.daemon_meta` for the daemon instance id and passes the
  PostgreSQL connection through daemon sweep execution instead of opening the
  legacy SQLite registry.
- Go owns `repo.add`, `repo.list`, `repo.remove`, `repo.resolve`, and a
  read-only `dashboard.all` projection over daemon PostgreSQL. The
  dashboard-all projection now includes per-active-run `run_progress` with
  phase progress, auto-finalize dry-run visibility, and supervisor-stall
  detail in both Go and Python/PostgreSQL paths.
- Daemon MCP `resources/list` and `resources/read` are PostgreSQL-backed when
  the MCP server has a daemon PostgreSQL connection. Resource visibility honors
  global and repo-scoped read tokens, uses PostgreSQL read projections for
  status/doctor/run/why/blocker/dashboard data, and keeps stale-lease resources
  read-only by projecting current expired/stale rows instead of invoking lazy
  recovery mutation handlers.
- Architecture guardrails now classify every remaining direct
  `striatum.daemon.connect_registry()` caller and assert production daemon MCP
  resource reads fail before reaching the legacy SQLite registry when the
  tripwire is active.
- `striatum daemon audit` now authorizes with the PostgreSQL capability table,
  appends the audit read to the PostgreSQL audit chain, and returns the
  compatibility field names expected by existing CLI consumers. The legacy
  SQLite audit read remains only for unconfigured fixture/migration paths.
- `striatum daemon health` now appends an unauthenticated allowed health row
  to the PostgreSQL audit chain and returns the existing compact health
  response when a daemon DB is configured. The legacy SQLite health probe
  remains only for unconfigured fixture/migration paths.
- `daemon doctor` now treats the legacy SQLite registry as post-cutover unused
  after a successful PostgreSQL doctor check instead of probing it. The direct
  `read_doctor` helper uses PostgreSQL for global and repo-scoped diagnostics
  when a daemon DB is configured and leaves the old registry path only for
  unconfigured fixture/migration compatibility.
- `striatum daemon status` and `striatum daemon stop` now use PostgreSQL
  capability authorization and PostgreSQL audit rows when a daemon DB is
  configured. Runtime pidfile behavior remains local to the daemon runtime
  directory.
- Production daemon CLI/admin dispatch now imports PostgreSQL-only helpers from
  `striatum.daemon_pg.client_admin`; the legacy Python daemon registry wrapper
  and direct `--daemon` read fallback are removed from CLI dispatch.
- `workflow upgrade` no longer falls back to repo-local SQLite running-run
  checks in production or under the legacy test-harness escape. PostgreSQL
  verification failures fail closed even when legacy SQLite files exist.
- The compact terminal dashboard renders production single-run text frames
  from the daemon/PostgreSQL `dashboard` DTO. The repo-local SQLite payload
  gatherer has been deleted; paired test-harness dashboard assertions now use
  renderer fixtures instead of a second live reader.
- Go `status` now returns the PostgreSQL/Python read-model shape, including
  job counts, nested verdict posture counts, queue-based claimable jobs,
  blockers and human checkpoints, process health, supervisor stalls, phase
  progress, provenance mode, auto-finalize dry-run visibility, and
  deterministic next actions.
- The Go daemon now starts a resident recovery scheduler loop after socket
  bind. The loop runs an immediate PostgreSQL active-run sweep, calls the Go
  `recovery.sweep` path per active run, records `daemon.recovery_sweep`
  events, and upserts `striatumd.scheduler_cursors`. Operators can tune it
  with `--sweep-interval-seconds`; tests can bound it with `--max-sweeps`.

## Open Questions

- Should any remaining registry-probe/global diagnostic paths be generated
  from the daemon method contract instead of curated by guardrail tests?

## Domain Modeling

This RFC is a boundary clarification. The daemon-owned PostgreSQL instance is
the aggregate authority for registered target repositories and daemon-global
metadata. SQLite survives only as migration source material and fixture data.
