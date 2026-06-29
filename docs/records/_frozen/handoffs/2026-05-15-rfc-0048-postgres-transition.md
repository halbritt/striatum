# Handoff — RFC 0048 Postgres transition session (2026-05-14 → 2026-05-15)

author: operator

Goal of the session: move Striatum from "daemon-required CLI in name only,
SQLite in practice" to "Postgres is the live substrate end-to-end."
Six version bumps shipped during the original session; the follow-up
implementation completed the Go-core mutation port and the mapped CLI
fail-closed flip. The substrate flip is now mechanically complete for the
Python core reads + mutations, Go-core reads + mutations, and CLI routing for
the mapped daemon verbs. Current status note (2026-05-17): the V1.5 hardening
bundle referenced here completed in v1.55.0; use `CHANGELOG.md`,
`docs/TODO.md`, and `docs/ROADMAP.md` for current open work.

## What landed

| Tag | RFC scope | Notes |
|---|---|---|
| v1.49.0 | RFC 0048 V1 Phase A | 16 mutation PG handlers under `src/striatum/daemon_pg/handlers/{workflow_loop,recovery_evidence}/`. `DaemonRpcRouter._route` resolves PG before falling through to legacy `CLI_ROUTES`. |
| v1.50.0 | RFC 0048 V1.5 | Unix-socket accept loop in `run_daemon_foreground`. Daemon-required CLI verbs actually reach the daemon now. `POSTGRES_TRANSITION.md` role-provisioning runbook (`striatumd_rw`). |
| v1.51.0 | RFC 0048 Phase C scaffold | CLI dispatch hook (`src/striatum/cli/daemon_rpc_route.py`) routes ~30 CLI verbs through Unix socket. PG admin client + runtime token bootstrapped on daemon startup. systemd user unit at `~/.config/systemd/user/striatumd.service`. Multi-repo router fix. Token + auth wiring. |
| v1.52.0 | RFC 0048 Phase C complete (Python reads) | 12 read-surface PG handlers under `src/striatum/daemon_pg/handlers/reads/` (status, dashboard, list.\*, run.summary, why, doctor, evidence.export, corpus.export). |
| v1.53.0 | GH #19 + #21 + doctor --explain | `recovery requeue-stale --force --justification "<reason>"` for repo_write stale jobs (audit-chained). `_verify_state_health()` at serve startup refuses to bind over a corrupted retired-local-state + flushes WAL. `daemon doctor --explain` shows per-method PG-backed vs SQLite-fallback routing. |
| v1.54.0 | RFC 0048 Phase B (Go reads) | `go/pkg/reads/` ports the same 12 read handlers to Go-core parity. `go/cmd/striatumd/main.go` registers them before the not-implemented stub loop. Go daemon now serves reads instead of returning `not_implemented`. |
| follow-up | RFC 0048 Phase B/C completion | `go/pkg/mutations/` ports the repo-local workflow mutation surface to Go, Go embeds repo-local schema migration 0005, `run.prepare` materializes v1.1 phase dependencies, `recovery.auto` live mode publishes and completes recoverable stale work, `branch.confirm` honors git modes, and mapped CLI RPC verbs fail closed instead of falling back to SQLite. |

main at `3d85802`. All branches deleted; tag history clean.

## Current operator workspace state

- Daemon: **active** under systemd (`systemctl --user status striatumd.service`). Bootstrap admin client in `striatumd.clients`; runtime token at `/run/user/1000/striatum/client-token`.
- CLI: `striatum 1.54.0` (`pip install -e . --force-reinstall --user --break-system-packages` to pick up newer).
- retired-local-state: minimal — only dogfood-058's old `run_02ebec63…` row (the rest was lost when serve clobbered state, fix landed in v1.53.0 but the lost runs aren't recoverable).
- Postgres `striatum_daemon` DB: has data from the earlier `daemon migrate-repo-local` attempt — `repo_a89ecd1664764f039a127c62ab7da3f3` registered, 1 run / 90 events from dogfood-058. The schema is at version 5; `striatumd_rw` role + grants in place.
- Operator UI: `striatum serve --web --allow-mutations` running on `127.0.0.1:8088`; Tailscale-bridged at `https://proximal.tail0ecc2e.ts.net:8443/`.
- Quarantined: `.striatum/retired-local-state.corrupt` from earlier corruption — safe to delete.

## Current status after follow-up

**Complete — Phase B mutation port**

Follow-up implementation added `go/pkg/mutations/` and registered the Phase B
mutation surface before the Go daemon's generic `not_implemented` fallback.
The Go daemon now has handlers for: `session.register`, `session.close`,
`work.claim_next`, `work.ack`, `work.heartbeat`, `work.release`, `work.block`,
`work.complete`, `artifact.publish`, `review.submit`, `review.verdict`,
`review.override`, `decision.record`, `checkpoint.resolve`, `branch.confirm`,
`run.{prepare,start,pause,resume,cancel,retry_job}`,
`recovery.{stale_leases,requeue_stale,cancel_job,process_reconcile,resume,auto}`.
The follow-up also copied repo-local schema migration 0005 into the Go embed,
made `run.prepare` materialize v1.1 phase-synthesis dependencies, made
`recovery.auto` publish matching stale expected artifacts and complete the
recovered job, added artifact front-matter validation, ported
`branch.confirm` git modes (`--create`, `--use-current`, `--strict`), and
flipped mapped CLI RPC verbs to fail closed instead of falling back to SQLite
when daemon routing cannot proceed.

Verification run in the follow-up:

- `go test ./pkg/mutations ./cmd/striatumd`
- `go test ./...`
- `.venv/bin/pytest tests/test_cli_daemon_rpc_route.py tests/daemon_rpc/test_registry_rfc0043_coverage.py tests/test_daemon_rpc.py::test_daemon_pg_migrations_name_rpc_supervisor_apply_and_repo_local_tables tests/daemon_pg/test_repo_local_migration.py::test_repo_local_migration_registered_as_daemon_pg_v5 -q`
- `.venv/bin/ruff check src/striatum/cli/daemon_rpc_route.py tests/test_cli_daemon_rpc_route.py`
- `make daemon-go-build`

**Remaining — P1 V1.5 hardening bundle**

- **codex F2** capability-denial test matrix: PG-backed handlers × denial cases (missing/revoked/expired/wrong-cap/wrong-repo/replay). Each test monkeypatches `authorize()` to deny + asserts no SQL execution + denied audit row. Mechanical; the exact count should now include the Go mutation surface as well as the existing Python PG handlers.
- **codex F3** audit-chain SERIALIZABLE / row-lock per write handler. Touches every PG write handler. High blast radius; warrants a dogfood.
- **codex F4** append-only role-grant tests: connect as `striatumd_rw`, attempt `UPDATE`/`DELETE` on `events`/`artifacts`, assert permission denied. ~20 min cowboy.
- **claude HIGH#1** byte-equivalence parity rig: wire conftest's `ReadSeed` / `pg_ctx` / `sqlite_conn` into handler tests, add `assert_payload_parity` with per-key diff helper. ~60 min cowboy.
- **claude HIGH#2** dead code cleanup: `complete_inline`, `ack_inline`, and `recovery.resume --complete` still warrant discrete decisions. `recovery.auto` live mode has been implemented in Go follow-up work, so this item no longer blocks the Go mutation path.
- **Schema migration 0006**: ALTER `striatumd.events` ADD `previous_hash` + `row_hash`; CREATE `striatumd.repo_event_chain_heads`; re-anchor from `payload_json._event_chain`. ~60 min cowboy.

**Complete — P2 mapped CLI fail-closed flip**

The CLI dispatch hook now refuses to fall back to legacy SQLite for mapped,
registered RPC methods when the daemon is unreachable or the target repository
is not registered. CLI-local bootstrap and admin surfaces remain explicit
out-of-band helpers, not SQLite fallback routes.

## Pre-flight checks before next workflow / commit

1. `striatum --version` → expect `1.54.0`.
2. `systemctl --user status striatumd.service` → expect `active (running)`.
3. `striatum daemon doctor --explain --json | jq '.data.explain | {method_count, pg_backed_count}'` → expect the PG-backed count to include reads plus mapped mutations after installing the follow-up working tree. (Note: the `--json` envelope wraps the explain payload under `.data`, not at the top level.)
4. `python3 -c "import sqlite3; print(sqlite3.connect('.striatum/retired-local-state').execute('PRAGMA integrity_check').fetchone())"` → expect `('ok',)`.
5. `ps -ef | grep -E "(codex|claude|gemini).*wrapper" | grep -v grep | wc -l` → expect 0.
6. `ls /run/user/1000/striatum/` → expect `client-token`, `striatumd.pid`, `striatumd.sock`.
7. `git status --short --branch` → expect `## main`, clean tree.

## Smoke-test the v1.53.0 fixes before launching another workflow

The fixes shipped but were not load-tested under real conditions. Verify:

**GH #21 (serve startup state-loss)**:
```bash
# 1. Create a fake run row in retired-local-state so we can detect clobber.
python3 -c "
import sqlite3, uuid
c = sqlite3.connect('.striatum/retired-local-state')
c.execute('INSERT INTO runs(run_id, workflow_snapshot_id, repo_root, state, created_at) VALUES (?, ?, ?, ?, ?)',
          ('run_smoke_' + uuid.uuid4().hex[:8], 'ws_smoke', '/home/halbritt/git/striatum', 'completed', '2026-05-15T00:00:00Z'))
c.commit()
"
SIZE_BEFORE=$(stat -c%s .striatum/retired-local-state)
# 2. Restart serve.
kill -TERM $(pgrep -f "striatum.*serve.*web")
sleep 2
striatum --repo . serve --web --allow-mutations --host 127.0.0.1 --port 8088 &
sleep 3
SIZE_AFTER=$(stat -c%s .striatum/retired-local-state)
# 3. Sizes should match. Row should still be there.
echo "before=$SIZE_BEFORE after=$SIZE_AFTER"
```

**GH #19 (stale-lease repo_write recovery)**:
```bash
# Create a stale_lease repo_write job and exercise --force --justification.
# Easier: ensure the new CLI surface is reachable.
striatum recovery requeue-stale --help | grep -- '--force'
# Then in a real run with a real stale lease, verify:
# striatum recovery requeue-stale --run-id <r> --job-id <j> --force --justification "smoke test"
# → expect "status": "requeued", "operator_override": true.
```

## Known hazards

1. **Don't restart `striatum serve` while a dogfood run is active.** v1.53.0's `_verify_state_health` makes startup safer (refuses if corrupted, flushes WAL) but the cleanest path is still "don't bounce serve mid-run." If the operator UI is unhealthy, diagnose without `kill -TERM`.
2. **Cowboy mode is allowed for V1.5 hardening + bug fixes.** Dogfood mode is required for new RFC implementations (Phase B writes is RFC-class work; F2-F4 / HIGH#1-2 cleanup work could be cowboyed). The memory `feedback_operator_never_implements` says operators don't implement; the user override during the v1.53.0 work made that conditional — when the user explicitly says "cowboy it," it's fine for small bounded fixes.
3. **`STRIATUM_DAEMON_REQUIRED=0 STRIATUM_TEST_HARNESS=1` is no longer the default operator escape.** v1.51.0+ wired the daemon-required path end-to-end for reachable methods. Operators in test-harness mode should expect that path to bypass the daemon entirely; only useful for legacy SQLite-only operations.
4. **Postgres has leftover state for `repo_a89ecd1664764f039a127c62ab7da3f3`** from the earlier migration attempt. If re-running `daemon migrate-repo-local` on this repo: pre-clear via `sudo -u postgres psql -d striatum_daemon -c "DELETE FROM striatumd.repo_migrations WHERE repository_id='repo_a89ecd…'; TRUNCATE striatumd.{events, artifacts, runs, jobs, sessions, queue_messages, leases, work_packets, verdicts, blockers, command_requests, process_executions, job_worktrees, process_supervisors, process_supervisor_pointers, job_dependencies, workflow_snapshots} CASCADE;"`.
5. **GH #19 and #21 fixes were not load-tested.** Run the smoke tests above before relying on them.

## Memory notes that shaped this session

- `feedback_operator_never_implements` — saved during dogfood-060. Override allowed by user for V1.5 fix-up + small bug fixes.
- `feedback_autonomous_run_decisions` — don't block at human-acceptance gates; record the decision and continue.
- `feedback_finalize_without_asking` — after a meaningful unit of work ships, automatically commit, push, FF main, push main, delete the working branch.

## Reference docs

- `docs/dogfood/FRICTION_LOG.md` — dogfood-060 F1 + F2 (stale-lease recovery gap + serve startup state-loss). Both fixed in v1.53.0 / v1.54.0 era.
- `docs/dogfood/058/OPERATOR_REPORT.md` — V1.5 fix-up scope + cycle-exhaustion lessons.
- `docs/dogfood/060/OPERATOR_REPORT.md` — Phase C read-handler landing notes + the operator-driven completion footnote.
- `docs/POSTGRES_TRANSITION.md` — operator runbook including the `striatumd_rw` role-provisioning section (added in v1.50.0).
- `docs/rfcs/0048-daemon-side-substrate-migration.md` — V1 Phase A landing summary + V1.5 follow-up list.

## Sessions worth examining if patterns repeat

- dogfood-057 (RFC 0048 V1 Phase A): cycle exhaustion on codex `reject` with substantive findings; operator chose to land V1 + defer findings to V1.5.
- dogfood-058 (RFC 0048 V1.5 fix-up): track-boundary conflicts in synth caused cycle exhaustion. Lesson: prefer single implement track when methods are similar in structure.
- dogfood-060 (RFC 0048 Phase C reads): supervisor death + stale-lease unrecoverable; required operator pivot to direct implementation + filing of GH #19 + #21. Lesson: lazy-lease semantics for `repo_write` jobs need an operator escape hatch (now shipped as `--force --justification`).
