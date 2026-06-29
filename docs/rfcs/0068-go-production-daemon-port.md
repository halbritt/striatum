# RFC 0068: Go Production Daemon Port

Status: accepted
Date: 2026-05-17
Context: [RFC 0030](0030-daemon-rpc-server-and-version-skew-protocol.md), [RFC 0033](0033-storage-substrate-rewrite-for-daemon-v2.md), [RFC 0039](0039-go-daemon-core.md), [RFC 0043](0043-postgres-as-sole-substrate-and-daemon-required-runtime.md), [RFC 0059](../records/_frozen/rfcs/0059-eradicate-legacy-sqlite-fallbacks.md), [DECISION_LOG.md](../decisions/decision-log.md)

Successor note:
[`RFC 0078`](0078-go-only-runtime-and-python-removal.md), if accepted and
completed, supersedes this RFC's Python CLI/web-client carve-out. Until that
final deletion gate passes, this RFC remains the live rule for the production
daemon cutover only: Go is the only daemon core, while active Python
client/web/package/test surfaces are still tracked as RFC 0078 blockers.

## Problem

D105 encoded a Python-primary production daemon constraint during the
remediation sprint. That kept the product focused while the Python/PostgreSQL
path stabilized, but it is not the desired product direction.

The operator decision was explicit: move production daemon and daemon runtime
responsibilities to Go, retire the Python daemon after active contract-method
parity, keep the Python CLI/web layers where they remain useful, and eliminate
SQLite from production and compatibility paths. As of 2026-05-24, Go is the
production/default daemon, active contract-method parity is landed, the Python
daemon and Python MCP wrapper are deleted, and the legacy local-state
package/facades/fixtures are gone.

## Goals

- Supersede D105 with D107: Go becomes the intended production daemon core.
- Keep Python only as CLI/web client code; the Python daemon is no longer
  selectable and `striatumd` no longer points at the legacy Python daemon
  module.
- Keep the Python CLI acceptable as a client of the Go daemon.
- Preserve the RFC 0030 envelope, capability, request-id, version-skew, audit,
  and method-registry semantics.
- Preserve the RFC 0033/RFC 0043 PostgreSQL substrate and daemon-required
  runtime.
- Remove SQLite from all production, service, MCP, dogfood, operator-helper,
  and fixture paths.

## Non-Goals

- Rewrite the Python CLI in this RFC.
- Introduce hosted services, telemetry, external persistence, or cloud APIs.
- Keep a permanent dual-core product where Python and Go diverge in behavior.
- Keep repo-local SQLite as a supported compatibility mode.

## Proposal

The Go daemon port lands through independent, testable slices:

1. **Core gate and freshness.** The Go daemon must refuse to serve when its
   embedded migrations, method contract, generated registry, or packaged binary
   lag the Python/source contract. `--describe` and `daemon.hello` expose the
   core, version, supported schema, methods etag, and migration hash set.
2. **Go daemon method parity.** Replace `not_implemented` placeholders for all
   production daemon methods with Go handlers or explicitly remove the method
   from production surfaces until it is implemented.
3. **Go-owned global surfaces.** Move daemon startup, health, audit, sweep,
   dashboard-all, daemon MCP resources, and repository registration to Go over
   PostgreSQL.
4. **Client and service boundary.** Keep the Python CLI/web service as clients:
   no direct PostgreSQL repo resolution, no `striatum.api.invoke` production
   run authority for daemon-mapped reads or mutations, and no Python daemon
   fallback. The `striatumd` console script is a Go-daemon launcher shim.
5. **SQLite eradication.** Delete or port remaining SQLite-backed service,
   dogfood, adapter, byline, inbox, recovery, corpus, local API helpers, and
   migration fixtures.
6. **Retirement.** Keep the Go conformance suite passing, remove the legacy
   Python daemon module, and remove Python-daemon-only production code.

## Acceptance Criteria

- D107 is recorded and D105 is superseded.
- `striatum daemon start` launches the Go daemon by default after active
  contract-method parity; D111 retires the Python core selector, so
  `--core python` and `STRIATUM_DAEMON_CORE=python` are no longer supported.
- The Go daemon supports the current PostgreSQL schema version and refuses stale
  packaged binaries with a rebuild/remediation hint.
- The Go daemon serves every production method in
  `contracts/daemon_methods.json` or hides unsupported methods from production
  clients.
- Production MCP discovery hides local workflow-file authoring methods. Removed
  dogfood composite and reviewed-patch mutation names audit as
  `method_unknown`; any hidden registered methods still reauthorize and fail
  closed when called directly.
- CLI, web, MCP, and service tests pass against the Go daemon without direct
  SQLite opens.
- Production daemon/client paths do not open retired repo-local state or the
  legacy daemon registry; guardrails block imports of retired implementation
  modules from returning.
- The Python daemon can be deleted without losing production behavior once the
  remaining legacy harness and fixture-conversion tasks are done.

## Implementation Notes

- `make daemon-go-conformance` is now the Go daemon CI/release gate. It builds
  and tests the Go daemon, then runs the PostgreSQL multi-repo harness with
  `CORE=go`, including Go daemon smoke, audit, mutation-registry, and
  supervisor smoke coverage. CI runs that gate on Linux where the PostgreSQL
  service is available.
- The multi-repo harness participant runner writes prepare/start/cancel and
  human-checkpoint state to daemon-owned PostgreSQL tables instead of creating
  or querying `.striatum/retired-local-state` in target repositories.
- Go `run.prepare` uses the Go workflow-authoring loader for source-path
  resolution before inserting workflow snapshot rows, so traversal refusal and
  JSON-only workflow-source validation no longer depend on Python-daemon
  behavior.
- Go `workflow.upgrade --add-phases` now ports the Python V1-to-V1.1
  phase-inference path, including preview/apply behavior, synthesis-job
  insertion, cross-phase edge rewriting, and the PostgreSQL non-terminal-run
  guard.
- Go `workflow.generate --shape multi_phase` now ports the Python generator's
  V1.1 output path, including ordered phases, per-track job remapping,
  `phase_synthesis` gates, and cross-phase synthesis-to-entry edges.
- As of 2026-05-18, Go handler coverage reports zero missing or generic
  `not_implemented` handlers for active contract methods. D110 removed
  `daemon.migrate_repo_local`, `dogfood.publish_on_behalf`, and
  `dogfood.surgical_recovery` from the production method contract. D112
  removed `apply.reviewed_patch` from the production method contract; stale
  direct calls return and audit as `method_unknown`.
- Python/Go production MCP `tools/list` now hides local workflow-file
  authoring methods. Daemon MCP `resources/list` and `resources/read` use
  PostgreSQL-backed repository visibility and read projections whenever a
  daemon PostgreSQL connection is present.
- The remaining legacy local-state implementation residue is deleted. Production
  daemon, service, MCP, and operator-helper paths must not reopen repo-local
  SQLite or the legacy daemon registry.
- `striatum daemon start` now always launches the Go daemon. `--core go`
  remains a deprecated no-op compatibility flag; the Python daemon is not
  selectable by CLI flag or environment variable. The `striatumd` console
  script now routes through a launcher shim that delegates to the same Go
  startup path without importing `striatum.daemon`.
- Runtime path/token helpers have moved to `striatum.daemon_runtime`, and
  PostgreSQL repository-registration helpers used by day-zero and daemon RPC
  live in `striatum.daemon_pg.repositories`. Production daemon CLI/admin
  dispatch uses `striatum.daemon_pg.client_admin`, and the old CLI-side
  daemon registry wrapper is removed. Guardrails keep direct imports from the
  retired `striatum.daemon` module out of production and test source.
- SQLite-era repository identity and deterministic migration fixtures are
  deleted; refusal/reporting code may still recognize the retired
  `.striatum/retired-local-state` file name without opening it as live state.
- D114 retires the no-PostgreSQL daemon MCP resource fallback. MCP
  `resources/list` and `resources/read` now require a daemon PostgreSQL
  connection and no longer import the legacy Python daemon for SQLite registry
  resources.

## Retirement Gate

The production daemon RPC retirement ledger is empty. Removed names return and
audit as `method_unknown`: D110 removed `daemon.migrate_repo_local`,
`dogfood.publish_on_behalf`, and `dogfood.surgical_recovery`; D112 removed
`apply.reviewed_patch`. D113 closes the writable SQLite import window:
`striatum daemon migrate` and `striatum daemon migrate-repo-local` are retired
compatibility spellings that refuse before opening SQLite. The historical
migration importer and deterministic repo-local fixtures are deleted. D114 also
removes the no-`pg_conn` daemon MCP resource fallback from the Python daemon
cleanup ledger.

`make daemon-go-conformance`, `go test ./cmd/striatumd`, and
`tests/architecture/test_authority_guardrails.py` are the executable cutover
checks for the remaining Go production contract and method-removal behavior.

## Resolved Questions

- D109 resolved the daemon-core default, and D111 completed the selector
  retirement: `striatum daemon start` launches Go, `--core go` is a
  deprecated no-op, and Python is no longer selectable as a daemon core.
- D112 resolved reviewed-patch apply mutation handling for this checkpoint:
  `apply.reviewed_patch` is not a production daemon RPC until a future
  sealed-apply decision defines the full mutation contract.
- D113 resolved SQLite import-window handling for this checkpoint: writable
  SQLite imports are no longer operator/product surfaces.

## Open Questions

- None for the current cutover. Any future archival recovery tool would require
  a separate product decision and must not restore SQLite as live state.

## Domain Modeling

This RFC changes an implementation boundary, not the workflow model. The daemon
core is a runtime implementation of the existing daemon aggregate authority.
Workflow state, method semantics, and audit events remain the same model
described in [`docs/DDD.md § Adding to the model`](../reference/domain-driven-design.md#adding-to-the-model).
