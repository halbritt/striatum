# RFC 0139: Supervisor reconcile/heartbeat loop write-amplification — stop the indexed-`state` churn and per-pass timestamp bumps from bloating `process_supervisor_pointers`

Status: implemented (D240, 2026-06-19 — #421, PR #489; migration 0040 + owner bundle 0019)
Date: 2026-06-19
author: proposer-claude-opus-4-8-001
Context:
- GitHub [#421](https://github.com/halbritt/striatum/issues/421) — "supervisor
  reconcile-loop write amplification bloats `process_supervisor_pointers` (HOT-defeating
  state churn)." The DB side is already contained by an automated monthly `pg_repack`;
  this RFC is the **app/schema root-cause fix**: cut the write volume and the
  HOT-defeating index churn so the bloat does not re-accrue. No infra change.
- Measured on host `proximal` (PG 17.10, `pg_stat_statements` ~16 h window): the reconcile
  loop runs **~2.7M times** in the window, each pass doing one read + three single-column
  timestamp writes (~8.1M writes). `process_supervisor_pointers` is the bloat hotspot and
  the slowest write (**2.13 ms**, ~2.4× its siblings); `pg_stat_user_tables` shows
  `n_tup_upd = 3,448,208`, `n_tup_hot_upd = 2,756,063` → **79.9 % HOT** (vs ~92 % on the
  sibling tables), `n_tup_newpage_upd = 691,885` (**~20 % to new pages**). The ~746k
  non-heartbeat updates (~21.6 %) ≈ the new-page rate; those are the updates that change
  the **indexed `state`** column. Under a ~510 stmt/s burst the heap grew **2.5 MB →
  149 MB in ~90 min**, plateauing ~150–255 MB; autovacuum keeps `n_dead_tup`≈0 but cannot
  truncate interior free space, so only `pg_repack`/`VACUUM FULL` reclaims it.
- GitHub [#417](https://github.com/halbritt/striatum/issues/417) — the phantom-supervisor
  reconcile-storm fix (migration
  [`0033_reap_terminal_run_supervisors.sql`](../../go/pkg/db/sql/0033_reap_terminal_run_supervisors.sql)
  + the `closeRemainingSessions` closed-session reap + the status-probe guard). 562
  supervisors were stranded `attached` across 128 terminal runs; the `status` read
  LEFT-JOINs `process_supervisors ON state='attached'` and runs a `sudo+tmux`
  `ProbeLaneLiveness` per attached row, so the stranded rows fan out hundreds of failing
  probes and blow the CLI read deadline. **Any change here must not weaken that
  stabilization** (the partial-unique and run indexes that key on
  `state IN ('starting','attached','detached')` are load-bearing for the reap and for the
  `attached`-scoped status join).
- GitHub [#198](https://github.com/halbritt/striatum/issues/198) /
  [#355](https://github.com/halbritt/striatum/issues/355) — the reconcile-loop
  lock-contention family (the `57014` / `append_event_row (sd): 57014` incidents, ~10
  deadlocks/2 days, ~106–113 s transactions near `transaction_timeout=120 s`). #198 hoisted
  the helper-event drain and liveness probes **out** of the lock-holding sweep transaction;
  #355 mirrored that for `recovery.process_reconcile` and added `SET LOCAL
  statement_timeout = '15s'` inside the lock window. **Any change here must not regress that
  contention story** — it must not add work inside the per-run advisory-lock transaction.
- Grounded reads at `main`:
  - [`go/pkg/db/sql/0005_repo_local_workflow_state.sql:402`](../../go/pkg/db/sql/0005_repo_local_workflow_state.sql)
    — `CREATE TABLE striatumd.process_supervisor_pointers`
    (PK `(repository_id, supervisor_id)`, `state text CHECK (state IN
    ('starting','attached','detached','lost','stopped'))`, `updated_at timestamptz NOT
    NULL`), and its two non-PK indexes at `:418`/`:421`:
    `uq_active_daemon_supervisor_pointer_per_session (repository_id, session_id) WHERE state
    IN ('starting','attached','detached')` and `idx_process_supervisor_pointers_run
    (repository_id, run_id, state)`. **`updated_at` is in no index; `state` is in two of the
    three.** The sibling `process_supervisors` (`:374`) and
    [`go/pkg/db/sql/0002_rpc_supervision_apply.sql:15`](../../go/pkg/db/sql/0002_rpc_supervision_apply.sql)
    `daemon_supervisors` (`:15`) have the identical shape: `heartbeat_at` un-indexed, `state`
    in the partial-unique-per-session and the run index.
  - [`go/pkg/mutations/supervision_control.go:1419`](../../go/pkg/mutations/supervision_control.go)
    — `refreshSupervisorHeartbeat`, the **three single-column timestamp UPDATEs** the issue
    measured: `process_supervisors SET heartbeat_at`, `process_supervisor_pointers SET
    updated_at`, `daemon_supervisors SET heartbeat_at` (the last only when a
    `daemon_supervisor_id` is present). Each is an **unconditional** bump — no
    `WHERE … < threshold` guard, so every drained helper event writes all three rows.
  - [`go/pkg/mutations/supervision_control.go:1370`](../../go/pkg/mutations/supervision_control.go)
    — `updateSupervisorState`, the in-place `state` transition
    (`starting → attached → detached → stopped/lost`) on both `process_supervisors` and
    `process_supervisor_pointers`. Each `state` write is non-HOT (it is in two indexes).
  - [`go/pkg/mutations/recovery.go:64`](../../go/pkg/mutations/recovery.go)
    `HandleRecoveryProcessReconcile` + the `sweepDrainHelperEvents` /`drainHelperEvents` path
    that calls `refreshSupervisorHeartbeat` once per drained helper event (the #198 hoist:
    each drain is its **own** short transaction *before* the main sweep), and the reconcile
    read SELECT with the `process_supervisor_pointers` `LEFT JOIN LATERAL … ORDER BY
    ptr.updated_at DESC` (the read that consumes `updated_at`).
  - [`go/pkg/mutations/mutations.go:662`](../../go/pkg/mutations/mutations.go) — `lockRun`
    (`SELECT pg_advisory_xact_lock(hashtext('striatum:run:'||repository_id||':'||run_id))`),
    the per-run advisory lock #198/#355 hardened.
  - [`go/cmd/striatumd/main.go:80`](../../go/cmd/striatumd/main.go) +
    [`go/pkg/recovery/scheduler.go:10`](../../go/pkg/recovery/scheduler.go) — the resident
    recovery scheduler, `DefaultSweepInterval = 60s`, `--sweep-interval-seconds` /
    `STRIATUM_SWEEP_INTERVAL_SECONDS`. The supervisor-helper progress heartbeat min-interval
    is `defaultHeartbeatMinInterval = 20s`
    ([`go/pkg/supervisor/progress_meter.go:43`](../../go/pkg/supervisor/progress_meter.go),
    `STRIATUM_HELPER_HEARTBEAT_INTERVAL`) — the cadence that produces the ~20s-spaced helper
    events whose drain fires the three UPDATEs.
  - [`go/pkg/db/migrations_test.go:42`](../../go/pkg/db/migrations_test.go) — the floor-27
    runtime-DDL guard (`runtimeMigrationOwnerDDLViolations`). All three supervisor tables are
    **runtime-owned** (`striatumd_rw` created them in runtime migrations 0002/0005 and bumps
    them on every heartbeat — see the 0033 ownership note), and the guard regex matches only
    `ALTER TABLE` / `DROP TABLE`, **not** `CREATE INDEX` / `DROP INDEX`. So index reshaping on
    these tables ships as an ordinary runtime migration; a column add or a side table that
    needs `ALTER TABLE` adds the touched table to `runtimeOwnedTablesAlterable`.

> **Self-applied discipline.** The single load-bearing mechanism claim of this RFC —
> "the heartbeat timestamp bumps are HOT-eligible (un-indexed columns) but the `state`
> transitions are necessarily non-HOT because `state` is in two indexes, and *that* is the
> ~20 % new-page churn that bloats the heap" — was `ASSERTED`, then **`VERIFIED` against
> source**: `updated_at`/`heartbeat_at` appear in **no** index definition in
> `0005_repo_local_workflow_state.sql`/`0002_rpc_supervision_apply.sql`, while `state`
> appears in `uq_active_daemon_supervisor_pointer_per_session` (partial) and
> `idx_process_supervisor_pointers_run`. The verdict: a pure timestamp bump can be HOT and
> never touches an index entry; a `state` UPDATE always writes new index tuples and a new
> heap tuple. The issue's 79.9 % HOT / 20 % new-page split, and the ~746k `state`-bearing
> updates ≈ the ~692k new-page updates, are explained by exactly this, not by the timestamp
> writes. The recommendation follows from the verified mechanism, not from speculation.

## Problem

The supervisor reconcile/heartbeat loop is the daemon's **dominant write source**, and
`process_supervisor_pointers` is its bloat hotspot. Three measured facts:

1. **Volume.** ~2.7M reconcile passes / 16 h, three single-column writes per pass (~8.1M
   timestamp writes). For a liveness signal whose only consumers are the reconcile read's
   `ORDER BY ptr.updated_at DESC` tiebreak and stale-lease detection, that is a large write
   budget spent on data nobody reads at sub-minute resolution.
2. **HOT defeat.** 79.9 % HOT on `process_supervisor_pointers` (vs ~92 % on siblings),
   `n_tup_newpage_upd = 691,885` (~20 %). The ~746k non-heartbeat updates are the in-place
   **`state`** transitions, and because `state` is in two of the three indexes, **every one
   is a non-HOT update**: it writes a new heap tuple *and* two new index entries. Non-HOT
   tuples are what extend the heap and what VACUUM-but-not-truncate cannot reclaim.
3. **Bloat that only `pg_repack` reclaims.** The heap grew 2.5 MB → 149 MB in ~90 min under
   load and plateaus ~150–255 MB; autovacuum holds `n_dead_tup`≈0 but cannot truncate
   interior free space. The DB-side answer is a monthly repack — a symptom treatment.

Operationally low-impact today (bounded, fully cached, <1 % of `shared_buffers`), but it
wastes buffer cache + CPU on the daemon's hottest read path and forces periodic repacks,
and it is the same reconcile-loop transaction family behind the #198/#355 lock/timeout
incidents — cutting its write volume relieves that pressure too.

### Why the two write classes behave differently

| Write class | Touches | Index entries written | HOT-eligible? | Effect on bloat |
| --- | --- | --- | --- | --- |
| `refreshSupervisorHeartbeat` timestamp bump (`updated_at`/`heartbeat_at`) | un-indexed column only | none | **yes** (if the page has free space) | page-fill pressure only; goes to a new page once the page's dead-tuple slack fills |
| `updateSupervisorState` (`state` change) | column in 2 of 3 indexes | 2 (new tuple in each index) | **no, ever** | a new heap tuple + 2 index entries every time; the ~20 % new-page churn |

So the two issue-listed levers are *complementary, not redundant*: the timestamp lever cuts
how fast pages fill (and the raw write IO / buffer-cache thrash), and the `state` lever
removes the unavoidably-non-HOT churn. Either alone helps; together they are the structural
fix.

## The three directions, with trade-offs

The issue lists three directions. Each is analyzed against the #417 and #198/#355 constraints.

### Direction 1 — throttle / coalesce the heartbeat timestamp bumps

`refreshSupervisorHeartbeat` bumps all three rows unconditionally on every drained helper
event (~every 20 s per live supervisor). The timestamp's only sub-minute consumer is the
reconcile read's `ORDER BY ptr.updated_at DESC` tiebreak; liveness/staleness decisions are
made at the 60 s sweep cadence (and recovery's stale-lease thresholds are minutes). So the
*resolution* of `updated_at`/`heartbeat_at` can be coarsened without changing any decision:

- **Coalesce with a write-skip guard.** Add `WHERE … AND <ts_col> < $now - interval 'Ns'`
  (or compute in Go from the row already read) so a bump only writes when the stored
  timestamp is older than a floor (e.g. 30–60 s). This caps writes per row at ~1 per floor
  window regardless of helper-event burst rate. **Trade-off:** the stored timestamp is now
  accurate only to ±floor; fine for liveness (the staleness thresholds are minutes) but the
  RFC must confirm no consumer needs second-resolution `updated_at` (the reconcile tiebreak
  only needs *ordering*, which a coarse timestamp + the `supervisor_id DESC` secondary key
  preserve).
- **Drop the `process_supervisor_pointers.updated_at` bump from the heartbeat path
  entirely** and let `updated_at` track only real state transitions (it is already set by
  `updateSupervisorState`). The reconcile read's `ORDER BY ptr.updated_at DESC,
  supervisor_id DESC` still resolves a deterministic "most recent pointer per session"
  because `supervisor_id` is monotonic per session; **but** this changes the meaning of the
  ordering key, so it needs the read verified (Open Question 2).
- **Lower the helper heartbeat cadence** (`STRIATUM_HELPER_HEARTBEAT_INTERVAL`, default
  20 s) — blunt; it cuts all three writes proportionally but also coarsens progress
  liveness, which is load-bearing for the supervised-progress watcher. Weakest option; not
  recommended as the primary lever.

**#417 interaction:** none — this touches only the un-indexed timestamp columns, not `state`
or the indexes the reap/status-join depend on. **#198/#355 interaction:** strictly
*reduces* work; the drain transactions write less. Net: fewer writes, more HOT (pages fill
slower).

### Direction 2 — lighten the indexes / stop churning `state` in-place

The ~20 % new-page churn is the `state` column being in `idx_process_supervisor_pointers_run
(repository_id, run_id, state)` and `uq_active_daemon_supervisor_pointer_per_session
(repository_id, session_id) WHERE state IN (…)`. Options:

- **Drop `state` from `idx_..._run`** if the run-scoped reads do not need it as an index
  column. The run index exists to find a run's pointers; whether `state` earns its place as
  the third key column (vs. being a filtered-on payload the heap supplies) is verifiable
  against the actual run-scoped query shapes. If a state transition no longer rewrites *this*
  index, half the per-`state`-change index churn disappears and more `state` updates become
  HOT. **Trade-off:** any read that does an index-only scan filtering on `state` would lose
  it; must confirm none does (Open Question 1). **This index is `state`-bearing but NOT
  partial**, so it is *not* the #417 load-bearing one (that is the `attached`-scoped status
  join, which uses the partial-unique).
- **Do NOT touch `uq_active_daemon_supervisor_pointer_per_session`.** This partial-unique
  (`WHERE state IN ('starting','attached','detached')`) is the #417-stabilizing constraint:
  it enforces one live pointer per session and is exactly what the reap and the
  `attached`-scoped status join rely on. A `state` transition that crosses the
  `('starting','attached','detached')` ↔ `('lost','stopped')` boundary *must* update this
  partial index (the row enters/leaves it) — that is correct and load-bearing. We keep it as
  is; the win is only from the non-partial run index.
- **Net effect:** removing `state` from one of the two `state`-bearing indexes turns the
  intra-live-state transitions (`starting → attached → detached`, which do *not* change
  partial-index membership) into single-index (or HOT, if the run index drops `state`)
  updates, cutting the new-page rate. The terminal transitions (→ `stopped`/`lost`) still
  correctly leave the partial index.

**#417 interaction:** preserved — the partial-unique stays; only the non-partial run index
loses a column it may not need. **#198/#355 interaction:** none (index DDL is a one-time
runtime migration; runtime behavior unchanged). This is the lever that directly attacks the
20 % new-page churn.

### Direction 3 — split the hot timestamps into a narrow side table

The classic pattern: move the frequently-bumped `updated_at`/`heartbeat_at` out of the wide,
multi-index `process_supervisor_pointers` row into a narrow side table
(`supervisor_pointer_heartbeats(repository_id, supervisor_id PK, updated_at, heartbeat_at)`)
with **no secondary indexes**. Then a heartbeat bump churns a tiny, single-index row, and
the wide pointer row is only touched on real `state`/metadata change.

- **Pro:** the heartbeat writes leave the bloat-prone table entirely; the wide row's churn
  drops to just the `state`-transition rate, which is ~27 % of current volume. The side table
  is narrow, so its own bloat is trivially `pg_repack`-able and its HOT rate near 100 %.
- **Con:** it is a schema change touching reads — the reconcile read's `ORDER BY
  ptr.updated_at DESC` and any staleness query must JOIN the side table;
  `refreshSupervisorHeartbeat` rewrites to UPSERT the side table; `ALTER`/new-table DDL means
  adding the table to `runtimeOwnedTablesAlterable` and a runtime migration with backfill.
  Heavier than Directions 1+2, with more read-path surface to re-verify. Justified only if
  Directions 1+2 prove insufficient.

**#417 interaction:** none if the side table carries only timestamps (state stays in the
main table, indexes unchanged). **#198/#355 interaction:** the side-table UPSERT replaces one
of the three UPDATEs; net write count is similar but the bloat moves off the indexed table.

## Recommended combination

**Adopt Directions 1 + 2 together; hold Direction 3 in reserve.** Concretely:

1. **Coalesce the heartbeat bumps (Direction 1).** Add a write-skip floor to
   `refreshSupervisorHeartbeat`: only issue each of the three single-column UPDATEs when the
   stored timestamp is older than a configurable floor (default ~30 s, env
   `STRIATUM_SUPERVISOR_HEARTBEAT_COALESCE`), computed from the row the caller already holds
   so no extra read is added inside the lock window. This caps the ~8.1M writes at roughly
   one per row per floor window — a large multiplicative cut — while keeping `updated_at`
   monotonic and the reconcile-read ordering intact (coarsened resolution does not change any
   minutes-scale liveness decision). Keep the `process_supervisor_pointers.updated_at` column
   (do not remove the bump entirely) so the reconcile read's existing `ORDER BY` is
   semantically unchanged.
2. **Drop `state` from the non-partial run index (Direction 2)** — ship
   `idx_process_supervisor_pointers_run` as `(repository_id, run_id)` (and the matching
   `idx_process_supervisors_run` / `idx_daemon_supervisors_repo_run`) **iff** Open Question 1
   confirms no read needs `state` as an index column there. **Leave both partial-unique
   `…_per_session` indexes exactly as they are** (the #417 stabilization). This turns the
   common intra-live `state` transitions into fewer/HOT updates and cuts the ~20 % new-page
   rate.
3. **Ship as one runtime migration 0038** (the next free runtime version; current highest is
   0037): the index reshape (`DROP INDEX … ; CREATE INDEX …`, ideally `CREATE INDEX
   CONCURRENTLY` out of a transaction if the migration runner allows, else a brief in-line
   rebuild on these small tables) **plus** no schema change for the coalesce (it is pure Go in
   `refreshSupervisorHeartbeat`). Because the tables are runtime-owned and the migration is
   `CREATE INDEX`/`DROP INDEX` only, it clears the floor-27 owner-DDL guard without listing
   the tables in `runtimeOwnedTablesAlterable`.

Direction 3 (the side table) is the structural escape hatch if, after 1+2, the residual
`state`-transition churn still bloats the table past an acceptable repack cadence — but 1+2
should remove the dominant write source and the dominant non-HOT source, which is the whole
of the measured problem.

## Interactions (must-not-break)

- **#417 phantom-supervisor stabilization:** the `attached`-scoped status join and the
  closed-session reap depend on the **partial-unique** `…_per_session` indexes and on `state`
  remaining a correct, queryable column. We do not touch the partial-unique indexes, do not
  drop the `state` column, and do not change the `CHECK (state IN (…))` domain or the reap
  DML (migration 0033). The status read still LEFT-JOINs on `state='attached'` (a heap
  filter, or via the partial index), unaffected by dropping `state` from the *non-partial*
  run index.
- **#198/#355 lock contention:** the coalesce only *removes* writes; it adds **no** statement
  inside the per-run advisory-lock (`lockRun`) transaction, and the skip-floor is evaluated
  in Go from an already-read row, so no extra SELECT enters the lock window. The index reshape
  is one-time DDL. Net: strictly less reconcile-loop write work, which relieves the timeout/
  deadlock pressure rather than adding to it.
- **Liveness correctness:** coarsening `updated_at`/`heartbeat_at` to a ~30 s floor is safe
  because every staleness/liveness threshold that consumes them operates at minutes scale and
  the 60 s sweep cadence; the only sub-minute consumer is the reconcile read's *ordering*
  tiebreak, which a monotonic (if coarse) timestamp + the `supervisor_id` secondary key
  preserve. The RFC's P0 test obligation pins this with the actual consumers enumerated.

## Measurable acceptance criteria

Measured on a representative load (or a synthetic burst reproducing the ~510 stmt/s
reconcile rate) over a comparable window, against the same `pg_stat_statements` /
`pg_stat_user_tables` counters as the issue:

1. **Write volume:** the three reconcile timestamp UPDATEs drop by **≥ 80 %** vs the
   ~8.1M/16 h baseline (a 30 s coalesce floor against ~20 s helper events caps each row at
   ~1 write/floor; the floor sets the exact factor).
2. **New-page updates:** `n_tup_newpage_upd / n_tup_upd` on `process_supervisor_pointers`
   drops from ~20 % to **≤ 5 %** (the run-index `state` drop removes the index-entry rewrite
   for intra-live transitions; only the partial-index boundary crossings remain non-HOT).
3. **HOT ratio:** `n_tup_hot_upd / n_tup_upd` on `process_supervisor_pointers` rises from
   79.9 % to **≥ 92 %** (parity with the current siblings) — i.e. the table behaves like its
   un-state-churned peers.
4. **Bloat / repack:** under the same burst, steady-state heap size for
   `process_supervisor_pointers` stays **≤ 25 %** of the ~150–255 MB plateau, and the
   automated `pg_repack` cadence can be relaxed (the bloat no longer re-accrues at the
   monthly rate) — verified by `pgstattuple`/`pg_repack --dry-run` free-space delta over the
   window.
5. **No regression:** #417 status-read latency (the `attached`-scoped probe fan-out) is
   unchanged or better; the reconcile read plan still uses an index for the run/session
   lookups (confirm via `EXPLAIN` that dropping `state` from the run index did not force a
   seq scan on any hot read); no new statement inside the `lockRun` transaction (the #198/#355
   guard).

## Risks

- **Coarse `updated_at` breaks a hidden consumer.** If any read relies on second-resolution
  `updated_at`/`heartbeat_at` (beyond ordering), the coalesce floor silently degrades it.
  Mitigated by the P0 enumeration of every consumer (Open Question 2) and a regression that
  asserts the reconcile read returns the same "most-recent pointer per session" with coarse
  timestamps.
- **Dropping `state` from the run index slows a run-scoped read.** If some hot query does an
  index-only scan filtering on `state` via `idx_..._run`, removing the column forces a heap
  fetch or a seq scan. Mitigated by Open Question 1 (audit the run-scoped query shapes +
  `EXPLAIN` before/after) — if any read needs it, keep the index and lean harder on
  Directions 1 and 3.
- **`CREATE INDEX` rebuild window.** On these small, hot tables a non-`CONCURRENTLY` rebuild
  briefly blocks writes. Mitigated by `CREATE INDEX CONCURRENTLY` (if the migration runner
  supports out-of-transaction DDL) or by the rebuild being sub-second at the observed table
  size; either way it is a one-time cost.
- **Coalesce masks a genuinely stalled supervisor for up to one floor window.** A 30 s floor
  means a heartbeat can be up to 30 s stale before it is written; this is well inside the
  minutes-scale staleness thresholds, but the floor must stay below the smallest liveness
  threshold (pin in P0).

## Test obligations (before the migration lands)

1. **Coalesce correctness (pgtest).** With the skip-floor active,
   `refreshSupervisorHeartbeat` writes at most once per row per floor window under a burst of
   helper events, and `updated_at`/`heartbeat_at` remain monotonic; the reconcile read
   (`ORDER BY ptr.updated_at DESC, supervisor_id DESC`) still selects the same
   most-recent-per-session pointer as with per-event bumps.
2. **State-transition churn (pgtest + `pg_stat_user_tables`).** A scripted
   `starting → attached → detached → stopped` sequence produces no `n_tup_newpage_upd` on the
   intra-live transitions after `state` leaves the run index (only the terminal
   partial-index boundary crossing is non-HOT), and `n_tup_hot_upd` ratio meets criterion 3.
3. **#417 invariants intact (pgtest).** The partial-unique `…_per_session` indexes still
   enforce one live pointer per session; migration 0033's reap DML still finds and stops
   stranded live supervisors; the `attached`-scoped status read returns the same rows.
4. **No new lock-window statement (#198/#355 regression).** Assert
   `HandleRecoveryProcessReconcile` issues no additional SELECT/UPDATE inside the `lockRun`
   transaction; the coalesce skip is evaluated outside it / from the already-read row.
5. **Run-index read plan (pgtest/`EXPLAIN`).** The run-scoped reads that used
   `idx_..._run (… , state)` still use an index after the column drop (criterion 5).
6. **Runtime-DDL guard stays green.** `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL`
   confirms migration 0038 carries only `CREATE INDEX`/`DROP INDEX` (no `ALTER TABLE`/`DROP
   TABLE`) on the runtime-owned supervisor tables.

## Open Questions

1. **Does any read need `state` in `idx_process_supervisor_pointers_run`?** Audit the
   run-scoped query shapes (the reconcile read, status/dashboard run views, recovery) and
   `EXPLAIN` them before/after dropping `state`. If none filters on `state` *via this index*
   (the partial-unique already covers the live-state set per session), drop it; otherwise keep
   it and lean on Directions 1 and 3. **Verify before P-migration.**
2. **Coarse-timestamp consumers.** Enumerate every consumer of
   `process_supervisor_pointers.updated_at` and the two `heartbeat_at` columns. If all are
   ordering/staleness at minutes scale, the coalesce floor is free; if any needs fine
   resolution, scope the floor to only the columns that can take it. **Pin the floor value
   below the smallest liveness threshold.**
3. **Coalesce floor value + knob.** 30 s is the recommended starting floor (≥ the 20 s helper
   cadence, so most consecutive events skip; ≪ the minutes-scale staleness windows). Confirm
   against the actual staleness/lease thresholds and expose
   `STRIATUM_SUPERVISOR_HEARTBEAT_COALESCE` so operators can tune or disable it. **Policy/ops
   call.**
4. **Do the sibling tables need the same reshape?** `process_supervisors` and
   `daemon_supervisors` have the identical index shape and the same heartbeat path; they bloat
   less (the issue measures pointers as the hotspot) but the coalesce and run-index drop apply
   uniformly. Recommendation: apply 1+2 to all three in the one migration for consistency, but
   gate the run-index `state` drop on the same per-table read audit (Q1).
5. **Is Direction 3 ever needed?** If, after 1+2, the residual `state`-transition churn still
   forces a sub-quarterly repack, split the timestamps into the narrow side table. Decide the
   repack-cadence threshold that triggers Direction 3 so it is a measured follow-up, not a
   speculative pre-build.

## Domain Modeling

This is a **boundary clarification plus a write-amplification fix on an existing aggregate**,
not a new aggregate. The supervisor pointer remains the value object that projects "which
daemon-owned supervisor is live for this session/run." The clarification is that **the
pointer's `state` (a real lifecycle transition, indexed, queryable, load-bearing for #417) is
a distinct concern from its `updated_at`/`heartbeat_at` (a high-frequency liveness pulse with
no fine-resolution consumer)** — the former earns indexes and durable transitions, the latter
is a coalesced, un-indexed, repack-irrelevant heartbeat. Conflating the two in one wide,
multi-index, per-event-rewritten row is the source of the amplification; separating their
write cadences (coalesce) and their index participation (drop `state` from the non-partial
run index; reserve a side table for the timestamps) realigns physical write behavior with the
domain meaning. Cites
[`docs/DDD.md § "Adding to the model"`](../explanation/domain-driven-design.md#adding-to-the-model);
RFC 0031 (daemon-owned supervision pointers) and RFC 0009 (long-lived process supervision)
are the precedents for the aggregate.
