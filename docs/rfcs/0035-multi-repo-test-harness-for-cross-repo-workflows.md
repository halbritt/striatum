# RFC 0035: Multi-Repo Test Harness for Cross-Repo Workflows

Status: accepted (V1)
Date: 2026-05-12
Context:
[`RFC 0032`](0032-cross-repo-workflows-and-mcp-mutation-capabilities.md),
[`RFC 0030`](0030-daemon-rpc-server-and-version-skew-protocol.md),
[`RFC 0031`](0031-daemon-owned-supervision-and-sealed-apply-boundary.md),
[`RFC 0033`](0033-storage-substrate-rewrite-for-daemon-v2.md),
[`docs/decisions/decision-log.md`](../decisions/decision-log.md) (D086, D087, D088, D090),
historical dogfood source paths `docs/dogfood/035/DESIGN_SYNTHESIS.md` and
`docs/dogfood/035/BUILD_HANDOFF.md`,
`tests/conftest.py`,
`tests/test_cross_repo_lifecycle.py`,
`tests/test_workflow_cross_repo.py`

Implementation status: dogfood-037 shipped the developer-only
`tests/_harness/MultiRepoHarness` coverage described here. The harness now
targets the Go daemon and daemon-owned PostgreSQL, with no repo-local SQLite
authority. It exercises RFC 0032 schema, prepare/lifecycle helper paths,
crash-recovery, MCP capability scope, and per-repo write-scope behavior; it
does not by itself ship the production live cross-repo scheduler fan-out
surface.

## Problem

RFC 0032 V2 shipped cross-repo workflow schema validation, daemon-mediated
run lifecycle helpers (mock-friendly), per-repo write-scope enforcement,
and MCP mutation capability gating with audit. The implementation in
dogfood-035 shipped unit + mock-based + schema/validator/capability-gate
coverage; **multi-repo / cross-repo end-to-end integration tests against a
real two-repo daemon harness were explicitly deferred** to a follow-up
RFC. That follow-up is this RFC.

The earlier test fixtures were single-repo. Current harnesses boot a daemon
with ephemeral Postgres and registered target repositories; `.striatum/` is
scratch only and no SQLite file is created or inspected except migration
fixtures. Cross-repo behavior — the `repositories` workflow block, two repos
sharing a daemon, cross-repo run lifecycle with daemon-side coordination,
per-repo failure isolation, daemon-crash reconciliation across repos, MCP
token scope enforcement against a non-primary registered repo, cross-repo
cycle iteration accounting — needs harness coverage end-to-end. The
mock-based coverage in `tests/test_cross_repo_lifecycle.py` and the
validator coverage in `tests/test_workflow_cross_repo.py` together
verify shape and contracts, but they do not exercise the daemon RPC
server, the daemon DB writes, or the cross-repo coordination paths in
their integrated form.

Without that harness:

- behavior changes that affect cross-repo coordination cannot be tested
  without ad-hoc multi-repo scaffolding per test file;
- the daemon's startup reconciliation logic for `preparing` cross-repo
  rows has no integration coverage;
- the MCP capability scope mismatch path (token scoped to repo A used
  against repo B) is verified only at the capability check layer, not
  end-to-end through the MCP server, daemon RPC, and per-repo write-
  scope refusal;
- the daemon/Postgres transaction ordering for participating repository
  scopes cannot be exercised end-to-end;
- regressions that show up only when two repos share a daemon will
  surface in production rather than in CI.

Scope is **developer/test infrastructure**, not product code. The
harness lives under `tests/` (and a small reusable helper module).
Nothing in the harness ships as a public API or operator-facing surface.

## Goals

- Provide a reusable test fixture that boots a daemon instance with N
  registered target repositories, each with daemon/Postgres state under a
  `repository_id` scope plus a worktree.
- Cover the cross-repo workflow execution paths end-to-end: prepare,
  start, run summary, cancel, dashboard across participating repos,
  daemon-crash reconciliation, per-repo unregistration mid-run, per-
  repo write-scope enforcement when a job targets a non-primary repo,
  cross-repo cycle iteration accounting (max_iterations global to the
  cycle).
- Cover the MCP capability scope enforcement path end-to-end: token
  issuance against repo A, attempted use against repo B, refusal with
  the documented `capability_missing` denial vocabulary, audit row
  appended with the documented denial reason.
- Cover daemon/Postgres transaction ordering on the cross-repo prepare path:
  cross-repo metadata plus per-repository rows write inside the same daemon
  transaction, with rollback on partial failure.
- Surface daemon RPC client testing patterns so future RFCs (Go core,
  cross-platform daemon, hosted mode) can adopt the same shape.
- Keep tests fast enough to run in CI on the existing matrix.

## Non-Goals

- Public API or operator-facing surface for multi-repo testing. The
  harness is `tests/` only.
- Cross-machine / hosted-mode testing. Per D083, daemon V2 is single-
  user single-machine; multi-machine semantics are out of scope.
- Multi-tenant testing. One OS user per harness instance.
- Replacement of `tests/conftest.py`'s single-repo fixtures. The
  single-repo flow is still the right shape for the majority of tests.
- Bundled / Dockerized Postgres for the harness. The harness uses the
  existing system Postgres requirement from RFC 0033; Docker-based
  ephemeral Postgres is a separate hardening RFC.
- Windows daemon support. Per RFC 0030 and the V2 dogfoods, Windows
  daemon mode is deferred.
- Performance / load testing. The harness's goal is functional
  end-to-end coverage; load tests are a separate effort.

## External Prior Art

Test harnesses that boot a real daemon plus N satellite instances are
common in distributed-systems testing. Striatum's harness borrows the
useful patterns and trims anything that violates the local-first
boundary:

- **Kubernetes e2e tests** (Kind, k3d): the test boots a real local
  cluster with N nodes and exercises the API against it. The useful idea
  is "boot a real instance per test class, share it across tests in the
  class, tear down deterministically". The non-goal for Striatum is
  ephemeral container orchestration; subprocess + temp-dir is enough.
- **PostgreSQL `pg_tap` and similar**: per-test database creation +
  fixture loading + deterministic cleanup. The useful idea is "fast
  schema reset between tests via TRUNCATE rather than reinit". The
  Striatum harness adopts this pattern for the daemon Postgres
  substrate.
- **Argo Workflows e2e tests**: per-test workflow submission against a
  shared controller. The useful idea is "share the daemon, isolate the
  per-test state". Striatum's equivalent is per-test cross-repo run id
  against a class-scoped daemon instance.
- **etcd test clusters**: in-process test cluster with N nodes. The
  useful idea is "subprocess per node, deterministic teardown, port
  collision avoidance". Striatum's harness uses subprocess + Unix
  socket so port collision is not a concern.

## Proposal

### 1. Harness module layout

Add a reusable test helper module:

```
tests/
  _harness/
    __init__.py
    multi_repo.py          # MultiRepoHarness fixture
    daemon.py              # ephemeral daemon helpers
    repos.py               # per-repo init + register helpers
    pg.py                  # per-test Postgres reset helpers
    mcp.py                 # MCP client helpers for capability tests
  test_multi_repo_harness.py   # smoke test for the harness itself
  test_cross_repo_prepare_e2e.py
  test_cross_repo_lifecycle_e2e.py
  test_cross_repo_crash_recovery_e2e.py
  test_mcp_capability_scope_e2e.py
  test_per_repo_write_scope_e2e.py
```

The harness is plain pytest fixtures + helper classes. No new
production dependencies; reuses `psycopg[binary]` already declared as
optional `daemon-pg`.

### 2. `MultiRepoHarness` fixture

The core fixture boots one daemon instance and N registered target
repositories. Per-class scope by default; per-function for tests that
mutate daemon state in ways that don't reset cleanly.

```python
@pytest.fixture(scope="class")
def multi_repo_harness(tmp_path_factory, postgres_url):
    harness = MultiRepoHarness(
        daemon_pg_url=postgres_url,
        repo_count=2,
        scratch_dir=tmp_path_factory.mktemp("multi_repo"),
    )
    try:
        harness.start()
        yield harness
    finally:
        harness.stop()
```

`MultiRepoHarness.start()`:

- creates an ephemeral Postgres database on the existing system PG
  instance (CI provides the connection; local dev uses `make pg-test`);
- runs the daemon PG migrations against that database;
- boots a daemon instance with an ephemeral Unix socket under
  `scratch_dir/daemon.sock`;
- initializes N target repositories under `scratch_dir/repo-{0..N-1}/`,
  each with `.striatum/` scratch only;
- registers each repository with the daemon (`striatum repo add
  --init`);
- exposes helpers to issue capability tokens, prepare cross-repo runs,
  inspect daemon DB rows, and inspect per-repo SQLite rows.

`MultiRepoHarness.stop()`:

- sends SIGTERM to the daemon, waits for clean exit;
- drops the ephemeral Postgres database;
- removes the scratch directory.

### 3. Per-test database reset

For tests that need a clean daemon DB per function (rather than per
class), the harness exposes:

```python
@pytest.fixture
def clean_daemon_db(multi_repo_harness):
    multi_repo_harness.reset_daemon_db()
    yield
```

`reset_daemon_db()` truncates every table in the daemon DB except the
schema-version row. It does NOT re-register repositories; tests that
need fresh repo registration call `harness.register_all()` explicitly.

The per-class daemon survives reset; subprocess teardown is per class.

### 4. Cross-repo prepare end-to-end test

`tests/test_cross_repo_prepare_e2e.py` covers:

- well-formed cross-repo workflow → daemon Postgres row in `preparing`, then
  `prepared`; per-repository rows created under participating
  `repository_id` scopes with matching `cross_repo_run_id`;
- malformed cross-repo workflow (unknown repo_id in `repositories`
  block) → daemon refuses, no DB writes anywhere;
- one participating repository fails during prepare (simulated via a scoped
  repository-state failure) → full rollback in the daemon transaction, no
  orphan daemon DB row, no partial per-repo state;
- workflow validator caught it at submit time, not run time
  (regression test for the validator).

### 5. Cross-repo lifecycle end-to-end test

`tests/test_cross_repo_lifecycle_e2e.py` covers:

- prepare → start → run summary → cancel cleanly across two repos;
- prepare → start → one repo's run completes → other repo continues →
  cross-repo run summary reflects mixed state;
- prepare → start → cancel mid-run → both repos transition to canceled,
  daemon DB row to canceled, no orphans;
- dashboard --run-id against the cross-repo run shows both
  participating repos;
- cross-repo cycle iteration accounting (max_iterations global to the
  cycle): force a needs_revision verdict; verify the cycle counter
  increments at the daemon Postgres layer.

### 6. Crash recovery end-to-end test

`tests/test_cross_repo_crash_recovery_e2e.py` covers:

- daemon killed mid-prepare → daemon restart's startup reconciliation
  observes incomplete daemon Postgres rows → rolls back the transition to
  `aborted`;
- daemon killed mid-start (after prepare, before all per-repo runs
  transitioned to running) → daemon restart reconciliation completes
  the transition or fails with a structured error;
- daemon killed mid-cancel → daemon restart finishes the cascade;
- one participating repo's filesystem becomes unreachable mid-run → daemon
  pauses the run with a human checkpoint; verify the checkpoint is recorded
  in daemon Postgres under the affected repository scopes.

### 7. MCP capability scope end-to-end test

`tests/test_mcp_capability_scope_e2e.py` covers:

- token issued with `write` capability scoped to repo A → MCP
  `tools/call` for a write-path against repo A succeeds, audit row
  appended with `decision=allowed`;
- same token used against repo B → MCP `tools/call` refused with
  `capability_missing` denial reason, audit row appended with
  `decision=denied`;
- token with only `read` capability → `tools/list` filters to
  read-only tools; `tools/call` against a write tool refused;
- unknown method → standard MCP unknown-method error, audit row with
  `decision=denied` and `denial_reason=method_unknown`;
- revoked token used → refused with `token_revoked`, audit row;
- expired token used → refused with `token_expired`, audit row;
- audit chain continuity across all of the above (sha256-chained
  prev-hash on each audit row).

### 8. Per-repo write-scope end-to-end test

`tests/test_per_repo_write_scope_e2e.py` covers:

- cross-repo workflow where job J targets repo B → job publishes
  artifact to a path inside repo B's allowed paths → success;
- same workflow where job J's expected_artifacts include a path that
  resolves into repo A's worktree → workflow validator catches it at
  submit time;
- runtime attempt to write a path inside repo A from a job targeting
  repo B → publish-artifact refuses with `write_scope_violation`;
- a `repo_write` job targeting a repo not declared in `repositories`
  → validator refuses; runtime path is unreachable (validator catches
  it earlier).

### 9. Harness smoke test

`tests/test_multi_repo_harness.py` smoke-tests the harness itself:

- start + register 2 repos + stop;
- `harness.reset_daemon_db()` actually clears every table except the
  schema-version row;
- ephemeral Postgres DB is dropped on stop;
- scratch directory is removed on stop;
- the daemon's Unix socket is deleted on stop;
- starting the harness twice in a row works (no port/socket
  collisions).

### 10. CI integration

The harness depends on the existing system Postgres requirement from
RFC 0033. CI already has the `daemon-pg` extras installed for the
single-repo Postgres tests; the multi-repo harness reuses the same
connection.

Local dev:

- `make test-multi-repo` runs only the harness-backed tests;
- `make pg-test` (already exists per RFC 0033) ensures the local PG is
  available;
- the existing `make test` includes the harness tests by default if PG
  is available; skips them with a clear message if PG is not.

CI matrix:

- Linux + PG: full coverage (existing job).
- macOS + PG: full coverage (existing job).
- macOS no-PG: harness tests skipped with a clear message.

The harness's per-class scope keeps the daemon startup cost amortized
across multiple tests; per-class teardown drops the ephemeral DB. Total
added wall-clock for the new harness tests should be under 60 seconds
on a developer laptop.

## Acceptance Criteria

- `tests/_harness/` module exists with `MultiRepoHarness`, daemon
  helpers, repo helpers, PG reset helpers, and MCP client helpers.
- `MultiRepoHarness` boots a daemon + N registered repos; per-class
  fixture scope works; per-function reset works.
- Each end-to-end test file from §4-§8 lands with at least the listed
  cases passing.
- `tests/test_multi_repo_harness.py` smoke test passes.
- `make test-multi-repo` runs the harness tests against the existing
  system PG.
- CI matrix runs the harness tests on Linux+PG and macOS+PG; cleanly
  skips on macOS-no-PG.
- The harness adds < 60 seconds to the local `make test` wall-clock.
- Existing single-repo tests continue to pass unmodified.
- `tests/conftest.py` single-repo fixtures remain the recommended shape
  for non-cross-repo behavior.
- Docs update `docs/TODO.md` (Open item 19 moves to most-done with this
  RFC as the landing point), `docs/SPEC.md` (if any user-visible
  contract changes), and `CHANGELOG.md` (Decided + Added entries).

## Implementation Plan

### Step 1. Harness module skeleton

Land `tests/_harness/` with `MultiRepoHarness` skeleton, daemon
boot/teardown, ephemeral PG reset, and the smoke test
(`tests/test_multi_repo_harness.py`). No e2e tests yet.

### Step 2. Prepare + lifecycle e2e

Land `tests/test_cross_repo_prepare_e2e.py` and
`tests/test_cross_repo_lifecycle_e2e.py`. Surface any harness gaps
(MCP client construction, audit-row inspection helpers) and fold them
back into the harness module.

### Step 3. Crash recovery e2e

Land `tests/test_cross_repo_crash_recovery_e2e.py`. This is the
trickiest tier because it needs deterministic daemon-kill simulation;
expect to add SIGKILL helpers + restart helpers to the harness.

### Step 4. MCP capability scope e2e

Land `tests/test_mcp_capability_scope_e2e.py`. The MCP client helper
needs to support both `tools/list` and `tools/call` with explicit
capability-token headers.

### Step 5. Per-repo write-scope e2e

Land `tests/test_per_repo_write_scope_e2e.py`. Mostly uses existing
harness helpers; verifies the validator + runtime refusal paths.

### Step 6. CI wiring + docs

Add `make test-multi-repo`, update `tests/conftest.py` to wire the
harness fixture, update `docs/TODO.md` Open item 19 to most-done with
this RFC as the landing point, and update `CHANGELOG.md`.

## Open Questions

- Should the harness run a daemon per test class or a daemon per test
  function by default? Per-class amortizes boot cost; per-function
  isolates state perfectly. Recommendation: per-class with explicit
  per-function escape hatch.
- Should `MultiRepoHarness.reset_daemon_db()` also re-register the
  participating repositories automatically, or require explicit re-
  registration? Auto-register is convenient; explicit is honest about
  what state survives reset. Recommendation: explicit
  `harness.register_all()`.
- Should crash-recovery tests use SIGKILL or `daemon.shutdown()`?
  SIGKILL simulates real crashes; `shutdown()` is deterministic but
  may mask races. Recommendation: SIGKILL for the daemon-side crash
  cases; document the determinism caveat.
- Should the harness expose a Go-client testing surface in addition to
  the Python client? D084 names Go as the future daemon language;
  exposing a small Go test client now is cheap. Recommendation: defer
  until a Go core RFC names a concrete client; the wire protocol from
  RFC 0030 is already language-neutral.
- Should the harness ship a "two-repos with worktree-isolated lanes"
  example workflow under `examples/`? It would be a useful onboarding
  artifact for operators starting their first cross-repo workflow.
  Recommendation: yes, but as a follow-up — keep this RFC scoped to
  test infrastructure.

## Domain Modeling

This RFC adds test infrastructure, not new domain concepts. The
existing cross-repo run aggregate (`cross_repo_run_id`, per-repo
`runs.cross_repo_run_id` pointer), the MCP capability vocabulary, the
audit chain, the daemon RPC method registry, and the per-repo write-
scope contract are exercised end-to-end; the harness itself adds no
new value objects.
