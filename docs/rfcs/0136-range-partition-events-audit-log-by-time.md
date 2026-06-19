# RFC 0136: Range-partition `events` and `audit_log` by `created_at` — the PK/unique-key reshape on two owner-held append-only chained tables, and partition DROP as the retention path that subsumes #386

Status: proposed
Date: 2026-06-18
author: proposer-claude-opus-4-8-001
Context:
- GitHub [#387](https://github.com/halbritt/striatum/issues/387) — "range-partition
  `events` / `audit_log` by `created_at`." The maintainer chose **draft the RFC now,
  implement later**: this document is the design of record; no code lands with it.
- GitHub [#386](https://github.com/halbritt/striatum/issues/386) — FK covering
  indexes on `events` / `audit_log`, **already implemented** as owner bundle
  [`0015_fk_covering_indexes_events_audit.sql`](../../go/pkg/db/sql/owner/0015_fk_covering_indexes_events_audit.sql).
  #386 is **interim insurance** against the seq-scan-on-parent-delete cliff; this RFC
  is the structural fix that, via partition `DETACH`/`DROP`, makes the delete-time
  RI scan #386 covers **not happen at all** for the retention path (the clean
  subsumption — §"Subsuming #386").
- [RFC 0110](0110-daemon-postgres-authentication-and-database-enforced-write-boundary.md)
  / D164 — both tables are **owner-held**: their only durable write path is a
  `SECURITY DEFINER` function (`striatumd.append_event_row`,
  `striatumd.append_audit_row`), runtime `INSERT` is revoked, and the row hash is
  computed in-DB. The partition reshape therefore touches owner DDL and the SD write
  path, not a runtime migration.
- [D187](../decisions/decision-log.md) (the #244 migration-ownership boundary) /
  [D215](../decisions/decision-log.md) (the per-object schema-ownership split), and the
  [PostgreSQL transition runbook](../how-to/postgres-transition.md) — owner-table DDL
  (`ALTER TABLE`, `DROP TABLE`, partition-by reshape) belongs in an owner/admin bundle,
  never a runtime migration. The build guard
  `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL`
  (`go/pkg/db/migrations_test.go:423`) fails otherwise, and a two-role production daemon
  crash-loops (D187 / #244).
- [D218](../decisions/decision-log.md) — owner bundle `0014` (chain-head lock-wait
  gauges) is the most recent owner-bundle precedent on these exact two tables; with
  `0015` (#386) now taken, the next free owner bundle for this reshape is **`0016`**.
- Grounded reads at `main`:
  `go/pkg/db/sql/0005_repo_local_workflow_state.sql` (the `events` table,
  `PRIMARY KEY (repository_id, event_id)`, the six FKs, the `events_no_update` /
  `events_no_delete` append-only triggers at `:447`/`:452`),
  `go/pkg/db/sql/0001_baseline.sql` (`audit_log`, `PRIMARY KEY (audit_id)`,
  `row_hash text NOT NULL UNIQUE`, `segment_id bigint NOT NULL REFERENCES
  audit_segments`, the `audit_log_no_update` / `audit_log_no_delete` triggers at
  `:158`/`:163`, and `audit_chain_head` / `audit_segments`),
  `go/pkg/db/sql/0006_events_chain_anchors.sql` (`repo_event_chain_heads`, its
  **`DEFERRABLE INITIALLY DEFERRED` FOREIGN KEY (repository_id, last_event_id)
  REFERENCES events(repository_id, event_id)** at `:72`),
  `go/pkg/db/sql/owner/0004_phase2_events.sql` (`append_event_row`, the in-DB
  `event_v3_row_hash`, the `repo_event_chain_heads` head advance),
  `go/pkg/db/sql/owner/0001_authority_phase0.sql` (`append_audit_row`, the in-DB
  `audit_v3_row_hash`, the open-segment select + `audit_chain_head` advance),
  `go/pkg/db/sql/owner/0015_fk_covering_indexes_events_audit.sql` (#386, the rows-on-
  proximal figure: `events ~13.5M rows`).

> **Self-applied discipline.** The single load-bearing claim of this RFC — "declarative
> range partitioning forces a PK/unique-key reshape on these two tables" — was
> `ASSERTED`, then **`VERIFIED` against source and against the PostgreSQL constraint
> that a partitioned table's PRIMARY KEY and every UNIQUE constraint must include the
> partition key column.** The verdict: `events.PRIMARY KEY (repository_id, event_id)`
> does **not** contain `created_at`, and `audit_log` has **both** a single-column
> `PRIMARY KEY (audit_id)` and a single-column `UNIQUE (row_hash)`, neither containing
> `ts`. So partitioning is **not** a transparent `ALTER`; it is a key reshape on
> append-only, hash-chained, owner-held tables — and the chain-head FK from
> `repo_event_chain_heads` into the events PK breaks under that reshape. The hardest
> parts of this RFC (the reshape, the chain-head FK, the backfill) are consequences of
> that verified constraint, not speculative risk.

## Problem

### The latent cliff

`striatumd.events` and `striatumd.audit_log` are unbounded append-only logs. `events`
records every run/job/lease state transition; `audit_log` records every RPC decision.
On `proximal` `events` already holds **~13.5M rows** (recorded in owner bundle 0015),
and both grow monotonically for the life of a registered target repository. Three
costs compound as they grow, none yet acute, all latent:

1. **Seq-scan / index-bloat on time-range reads.** Operator and doctor reads that
   want "events in the last hour" or "the audit tail since a timestamp" have no
   time-leading index that prunes old data; they either scan a growing b-tree or scan
   the table. The existing indexes lead with `run_id` / `job_id` / FK columns
   (`events_pkey (repository_id, event_id)`, `idx_events_run_time`, `idx_events_job`,
   and the #386 FK-covering indexes), none time-leading, so a time-bounded sweep over
   a 13.5M-row table touches the whole index.
2. **VACUUM cost on one monolithic heap.** Autovacuum and any future retention DELETE
   run against one ever-growing heap; there is no way to vacuum or freeze "only recent
   data" or to cheaply reclaim "everything older than the horizon."
3. **The retention cliff (the acute one #386 papers over).** There is today **no**
   retention mechanism for these tables — they only grow. The moment a retention or GC
   policy *does* start deleting old `events` / `audit_log` rows (or deleting the parent
   rows they FK to — `runs`, `sessions`, `artifacts`, `leases`, `queue_messages`,
   `audit_segments`), PostgreSQL must seq-scan the child table for dependent rows on
   each parent delete. #386 added FK covering indexes so that delete-time RI check is
   an index lookup, not a 13.5M-row seq-scan — **interim insurance**. But a row-by-row
   `DELETE` of the retention horizon is *itself* O(rows-deleted) heap churn plus index
   maintenance plus a VACUUM to reclaim, on append-only tables whose `*_no_delete`
   triggers (below) currently **refuse** DELETE outright.

### Why retention is not a `DELETE` you can just enable

Both tables carry append-only enforcement triggers that *refuse* row mutation:
`events_no_update` / `events_no_delete`
(`0005_repo_local_workflow_state.sql:447`/`:452`) and `audit_log_no_update` /
`audit_log_no_delete` (`0001_baseline.sql:158`/`:163`). They exist precisely because
these are tamper-evident logs — the audit chain and the per-repo event chain depend on
no row ever being altered or removed in place. So "just `DELETE` old rows" is not only
slow, it is *structurally forbidden* by the current schema, and rightly so: a row
`DELETE` would silently break the hash chain it sits in.

**Range partitioning is the retention mechanism that respects all three constraints.**
Dropping or detaching an entire old partition is a metadata operation — no per-row
heap churn, no per-row RI scan, no VACUUM of the surviving data, and (because it is DDL
on the partition, not row DML) it does **not** trip the `*_no_delete` row triggers. The
hash chain is preserved across the boundary by treating partition retirement as a
*sealed-segment* operation (§"Preserving the hash-chain contracts"), exactly the way
`audit_segments` already frames purge as a segment-level, chain-aware act
(`audit_segments.state IN ('open','closed','purged')`, `retention_state`).

### Why RFC 0110 / D164 make this an owner-bundle problem

Both tables are owner-held under RFC 0110: the runtime role `striatumd_rw` cannot
`INSERT` directly (it is revoked); the sole durable write path is the `SECURITY
DEFINER` functions `append_event_row` (owner bundle 0004) and `append_audit_row`
(owner bundle 0001), which compute the row hash **in-DB** and advance the chain head.
A partitioning reshape is `ALTER TABLE` / table-rebuild DDL against owner-owned tables,
which D187 / D215 place squarely in an owner/admin bundle. The SD append functions must
also be re-validated against the partitioned parent (they `INSERT INTO
striatumd.events` / `striatumd.audit_log` — routing to the right partition is
transparent to the function body, but the reshape touches the function's table and must
be re-stamped as a capability-parity bundle, §"The owner bundle and capability parity").

## The hard constraint: the partition key must join every PK and UNIQUE constraint

PostgreSQL declarative range partitioning requires that **the partition key column be a
member of the table's PRIMARY KEY and of every UNIQUE constraint** (a partitioned table
cannot enforce a uniqueness constraint that does not include the partition key, because
each partition is an independent index and global uniqueness over only non-partition
columns is not enforceable). Both tables violate this today:

| Table | Partition key | Current PK | Current UNIQUE | Reshape forced |
| --- | --- | --- | --- | --- |
| `events` | `created_at` | `(repository_id, event_id)` | — (none beyond PK) | PK → `(repository_id, event_id, created_at)` |
| `audit_log` | `ts` | `(audit_id)` | `(row_hash)` | PK → `(audit_id, ts)` **and** UNIQUE → `(row_hash, ts)` |

This is the spine of the RFC. Two consequences, each load-bearing:

- **The reshape changes the *enforced uniqueness*, not the *logical* uniqueness.** A
  composite PK `(repository_id, event_id, created_at)` no longer database-enforces that
  `event_id` is unique *per repository independent of time* — uniqueness is now enforced
  only over the full triple. Likewise `(row_hash, ts)` no longer database-enforces that
  `row_hash` is globally unique. In practice both are *still* logically unique
  (`event_id` is an IDENTITY sequence; `row_hash` is a sha256 over inputs including the
  prior hash, so a collision is a hash break), and the append functions are the only
  writers — but the RFC must be explicit that **the database stops enforcing the
  narrower uniqueness**, and decide whether to re-assert it (e.g. a partial/BRIN check,
  or accept the SD-function-is-sole-writer invariant as sufficient — Open Question 3).
- **`created_at` / `ts` become part of the identity tuple.** They are already
  `NOT NULL` and already written by the append functions (`v_created_at :=
  date_trunc('second', now())` in `append_event_row`; `v_ts := date_trunc('second',
  now())` in `append_audit_row`), so no new not-null backfill is required for new rows.
  But every foreign key *into* these tables, and every read that joins on the old key,
  must be re-examined (§"The chain-head FK is the sharp edge").

## The chain-head FK is the sharp edge

`repo_event_chain_heads` carries a **real SQL foreign key into the events PK**:

```sql
-- 0006_events_chain_anchors.sql:72
FOREIGN KEY (repository_id, last_event_id)
  REFERENCES striatumd.events(repository_id, event_id) DEFERRABLE INITIALLY DEFERRED
```

A foreign key must reference a PK or UNIQUE constraint of the parent. Once the events
PK becomes `(repository_id, event_id, created_at)`, **`(repository_id, event_id)` is no
longer a key**, so this FK can no longer be declared — PostgreSQL will refuse it. This
is the same shape as the D215 "FK-to-owner-table trap," reached from the partition
side: the integrity that was a SQL FK must move into Go.

Resolution (mirrors D215's runtime-table rule, applied to the owner table's own
satellite): **drop the SQL FK and enforce `repo_event_chain_heads.last_event_id`
referential integrity in Go**, inside the same `append_event_row` transaction that
already advances the head. The append function already (a) inserts the event and (b)
upserts the chain head in one transaction with the head's `FOR UPDATE` lock held, so
the head can only ever point at an event the same transaction just wrote — the FK was
always belt-and-suspenders over a function-local invariant. Dropping it loses no real
guarantee; the SD function is the sole writer of both rows. The `DEFERRABLE` clause
existed only to let the head UPDATE and the event INSERT co-commit without an FK flap;
once the FK is gone, so is the flap. (`audit_chain_head` has **no** FK into `audit_log`
— it is a bare singleton pointer — so the audit side needs no FK surgery here.)

Other FK considerations:
- The six FKs *out of* `events` (to `runs`, `sessions`, `jobs`, `queue_messages`,
  `artifacts`, `leases`) and the one FK *out of* `audit_log` (`segment_id` →
  `audit_segments`) are **unaffected** — a partitioned table may still *have* outbound
  FKs; the partition-key-in-key rule constrains only *inbound* references and the
  table's own PK/UNIQUE. `audit_segments` stays a normal (unpartitioned) table; the
  `audit_log.segment_id` FK into it survives unchanged.
- No other table FKs into `events` or `audit_log` (verified: `repo_event_chain_heads`
  is the only inbound FK to `events`; nothing FKs into `audit_log`). So the chain-head
  FK is the *only* inbound-FK casualty.

## Preserving the hash-chain contracts across the partition boundary

The two chains must be byte-for-byte invariant across partitioning — partitioning is a
*physical storage* change that must be *semantically invisible* to the chain verifier.

### Event chain
- The per-repo chain is `previous_hash == prior row_hash`, linked through
  `repo_event_chain_heads.last_hash`; the v3 hash is computed **entirely in-DB**
  (`event_v3_row_hash`, owner bundle 0004) and **nothing in Go recomputes an event row
  hash** (the chain verifier `assertEventChainLinear` checks *linkage*, not hash
  content — owner bundle 0004's load-bearing note). Partitioning changes **none** of the
  hash inputs: `created_at` is already a hash input, `event_id` is already a hash input,
  and routing a row to a partition does not alter any column value. So the chain is
  invariant by construction — the reshape adds `created_at` to the *PK tuple*, but
  `created_at` was already in the *hash tuple*.
- **Chain continuity across a dropped partition.** When an old partition is detached/
  dropped for retention, the chain develops a *gap* (the dropped events' hashes are
  gone). This is acceptable **iff** retirement is modeled as a sealed boundary the
  verifier knows about — the same way `assertEventChainLinear` already tolerates a
  legacy-unanchored start by treating `previous_hash` as NULL and starting a fresh
  segment. The recommended form mirrors the audit segment model (below): record the
  retired partition's first/last `event_id` + first/last `row_hash` in a small
  **`event_chain_segments`** ledger (or reuse a generalized segment table) so a
  retrospective can prove "the chain was linear up to the seam, the seam hash is X,
  the chain resumed at hash X" without the dropped rows. Without such a ledger,
  dropping a partition is indistinguishable from tampering — so the retention path
  **must** seal the segment before it drops it (Open Question 2).

### Audit chain
- `audit_log` already has the right model: every row carries `segment_id`, and
  `audit_segments` already records `first_audit_id` / `last_audit_id` / `first_hash` /
  `last_hash` / `previous_segment_last_hash` / `next_segment_first_previous_hash` and a
  `retention_state` plus `state IN ('open','closed','purged')`. **Partition boundaries
  should be aligned to (or made to imply) segment boundaries**, so dropping a partition
  == purging a *closed, sealed* segment: flip `audit_segments.state` to `purged` /
  `retention_state` accordingly, and the cross-segment hash witnesses
  (`previous_segment_last_hash`, `next_segment_first_previous_hash`) already let a
  verifier prove chain continuity across the purged gap. The audit chain's retention
  story is therefore *already designed* at the segment layer; partitioning just makes
  the physical purge a partition `DROP` instead of a (forbidden) row `DELETE`. The
  `append_audit_row` open-segment selection and `audit_chain_head` advance are
  unchanged by partitioning (it `INSERT`s into the parent; routing is transparent).

The unifying rule: **a partition may only be dropped after the chain segment it
contains is sealed and its boundary hashes are durably recorded.** Retention operates
on *sealed segments*, and partitions are aligned to segment boundaries so the two are
the same act. This is what makes partition-DROP a legitimate retention primitive on a
tamper-evident log rather than a chain break.

## The owner bundle and capability parity

Per D187 / D215 the reshape ships as **owner bundle `0016`** (0015 is the highest taken;
§D218). The bundle is owner-applied out-of-band, additive/cumulative on 0001–0015, and
**capability-parity gated** exactly like owner bundle 0004 (RFC 0110 §8.2): a binary
that does not understand partition-aware append/retention must refuse to serve once the
bundle is stamped, and a binary configured for partitioned retention must refuse to
serve until it is stamped — so a binary-before-bundle or bundle-before-binary deploy
fails closed, not silently. Record a `schema_authority` capability stamp (e.g.
`events_audit_partitioned`, mirroring the `event_sd_append` stamp pattern) the startup
parity check verifies.

The bundle must be expressible as ordinary owner DDL — which forces the **backfill**
question, because you cannot `ALTER TABLE ... PARTITION BY` an existing non-empty table
in place; PostgreSQL has no in-place "convert to partitioned." The two standard forms:

- **(A) New partitioned table + attach-as-historical + copy + swap (online-ish).** Create
  `events_partitioned` with the reshaped PK, attach the existing `events` heap as a
  single historical partition (or copy the legacy rows into one historical partition),
  cut new writes over by renaming, and let retention carve future partitions. Lets the
  13.5M legacy rows ride as one sealed historical partition that retention can later drop
  wholesale.
- **(B) Rebuild via copy into time-bucketed partitions.** Create the partitioned table,
  create partitions per the chosen granularity, `INSERT ... SELECT` the legacy rows into
  their time buckets, swap. Heavier (full table rewrite) but leaves history already
  bucketed.

Recommended: **(A)** — minimal rewrite, the legacy heap becomes one immutable historical
partition, and the cutover is a rename under the owner role. The exact online-safety of
the swap (lock window, dual-write, or maintenance-window cutover) is an implementation
detail of the bundle; on a single-node local-first deployment a brief maintenance-window
cutover is acceptable and far simpler than dual-write. This is the "coordinated backfill,
capability-parity style" the issue calls for (cf. owner bundle 0004's parity gate / D187
/ D215).

## Subsuming #386

#386 (owner bundle 0015) added FK covering indexes so that *if* a parent row is deleted,
the child-side RI check is an index lookup rather than a 13.5M-row seq-scan. It is
interim insurance for a delete path that, today, **cannot even run** (the `*_no_delete`
triggers refuse it) and that has no retention policy behind it.

Range partitioning **subsumes** the case #386 insures against, for the retention path:

- **Partition `DETACH` / `DROP` does no child-side RI scan at all.** Dropping a whole
  old partition is a catalog operation; there is no per-row delete, so there is no
  per-row FK check to be slow about — the very scan #386's covering indexes accelerate
  simply does not occur. Retention stops being "delete N million rows (slow, and
  forbidden by the trigger)" and becomes "drop one partition (metadata, instant,
  trigger-irrelevant because it is DDL not DML)."
- The #386 indexes remain useful for the *other* direction — point lookups by
  `actor_session_id` / `artifact_id` / `lease_id` / `message_id` / `segment_id` — so the
  bundle is **not** reverted; it keeps its read value. The subsumption is specifically of
  the *delete-time RI scan* rationale, which partition-DROP eliminates.

So #386 is correctly framed as the cheap stop-gap and this RFC as the structural fix:
once partitioning + segment-aligned retention land, the retention cliff #386 guards is
no longer reachable via row deletes.

## Recommended phased plan

| Phase | Scope | Schema / ownership |
| --- | --- | --- |
| **P0 — decide the policy knobs** ✅ **DONE (2026-06-19, D241)** | ~~Pin the granularity and retention horizon with the maintainer.~~ **Resolved: weekly granularity; `events` 3-month retention, `audit_log` infinite (partitioned-but-never-dropped); Q3=(a) sole-writer, Q4=sibling slices, Q5=generalize segment abstraction.** See "P0 RESOLVED" above. | none (decision) |
| **P1 — chain-segment sealing for events** | Generalize the audit-segment "seal + boundary-hash + retention_state" model to the **event** chain (an `event_chain_segments` ledger or a shared segment abstraction), so an event partition can be sealed and proven-continuous before any drop. Land this **before** any partitioning, so retention has a chain-safe boundary from day one. | runtime table (no FK into owner `events`, integrity in Go, explicit GRANT) per D215 |
| **P2 — the reshape + partitioned `events`** | Owner bundle 0016: events PK → `(repository_id, event_id, created_at)`; drop the `repo_event_chain_heads` SQL FK and move its RI into the `append_event_row` transaction (Go/SD-local); create the partitioned table, attach the legacy heap as the historical partition (backfill form A), re-validate `append_event_row` against the parent, capability-parity stamp. | **owner bundle 0016** |
| **P3 — partitioned `audit_log`** | Owner bundle 0016 (same bundle or a sibling): audit PK → `(audit_id, ts)`, UNIQUE → `(row_hash, ts)`; align partition boundaries to `audit_segments`; re-validate `append_audit_row`; partition-DROP wired to segment `purged`/`retention_state`. | **owner bundle 0016** |
| **P4 — retention executor + doctor** | A daemon-owned, segment-aware retention sweep that seals → records boundary hashes → detaches/drops partitions past the horizon, plus doctor invariants: `event_chain_segment_seam_unproven`, `audit_segment_purged_without_boundary_hash`, `partition_dropped_without_sealed_segment`. The retention act is daemon-mediated, never a hand `DROP`. | owner partition-management privilege; doctor reads runtime |
| **P5 — re-validate / prune indexes** | Confirm the #386 FK-covering indexes are still earning their keep against the partitioned shape (per-partition local indexes vs. the global ones), and add time-leading local indexes the partition pruner can use for the §Problem-1 time-range reads. | index tuning (owner, no grant — 0015 precedent) |

P1 lands first and is independently valuable (it gives the event chain the same
retention-readiness the audit chain already has). P2/P3 are the reshape proper and the
highest-risk owner DDL. P4 makes retention real; P5 cashes in the read-performance win.

## Risks

- **The reshape is irreversible-ish owner DDL on the two tamper-evident logs.** A botched
  cutover (backfill form A's swap) on the audit log is the worst case — it is the
  compliance/forensic record. Mitigate with the capability-parity gate (fail closed),
  a dry-run on a copy, and a maintenance-window cutover with a verified chain-linear
  check (`assertEventChainLinear` / the audit verifier) **before and after** the swap.
- **Uniqueness narrowing (the §"hard constraint" consequence).** Dropping
  database-enforced narrow uniqueness on `event_id`-per-repo and global `row_hash`
  relies on the SD-function-is-sole-writer invariant. If that invariant is ever weakened
  (a second writer, a manual fix-up), a duplicate could slip in undetected. Open
  Question 3 decides whether to re-assert a guard. The audit `row_hash` collision case is
  also a *hash break* (sha256 over the prior hash), so a duplicate `row_hash` already
  signals tampering independent of the uniqueness constraint — the constraint was a
  backstop, not the primary witness.
- **Chain seam = a real gap.** A dropped partition is genuinely gone; only the sealed
  boundary hashes survive. This is the intended retention semantics, but it means the
  full row-level transcript before the horizon is *not* reconstructable — only the
  sealed-segment proof is. This must be an explicit, documented retention property, not
  a surprise (it is consistent with the product boundary: the DB is a curated record,
  not a transcript store — owner bundle 0004's `C-EVENT-NO-TRANSCRIPTS`).
- **Backfill of 13.5M+ rows.** Form A keeps it cheap (attach, no rewrite); form B
  rewrites. The lock window of the cutover is the operational risk; on a single-node
  local-first node a maintenance window is acceptable.
- **Partition-pruning depends on `created_at` being in the query predicate.** Reads that
  filter only by `run_id` / `job_id` (no time bound) cannot prune and will fan across
  partitions; the existing/#386 indexes (now per-partition local) still serve them, but
  the planner touches every partition's index. Pin which hot reads carry a time bound
  (Open Question 1 informs granularity).

## Test obligations (before any P2/P3 owner DDL lands)

1. **Chain-linear before/after the swap (pgtest).** `assertEventChainLinear` and the
   audit chain verifier pass byte-identically against the partitioned table as against
   the monolithic one, for a fixture with rows spanning ≥2 partitions.
2. **Append still routes + hashes unchanged (pgtest).** `append_event_row` /
   `append_audit_row` insert into the correct partition, the in-DB row hash equals the
   pre-partition hash for identical inputs, and the chain head advances.
3. **Chain-head RI in Go (pgtest).** With the SQL FK dropped, an attempt to advance
   `repo_event_chain_heads.last_event_id` to a non-existent event is rejected by the
   Go/SD-local check, not a SQL FK.
4. **Partition DROP is trigger-clean and RI-scan-free (pgtest).** Dropping a sealed
   historical partition does **not** fire `events_no_delete` / `audit_log_no_delete`,
   does not require the parent FKs to be scanned, and the chain verifier still proves
   continuity across the seam from the sealed segment record alone.
5. **Capability parity (pgtest/harness).** A binary without the
   `events_audit_partitioned` capability refuses to serve once the bundle is stamped;
   a partition-aware binary refuses until it is stamped.
6. **Runtime-DDL guard stays green.** `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL`
   confirms none of the reshape DDL leaked into a runtime migration.

## P0 RESOLVED (2026-06-19, D241) — the policy knobs are pinned

The maintainer pinned Open Questions 1–5 against **measured prod velocity** (not the
RFC's original lower-velocity assumption). Grounding read on `striatum_daemon`
2026-06-19: `events` = 14.0M rows / 20 GB and `audit_log` = 17.3M rows / 8.8 GB, both
accumulated over a **~5-week** span (2026-05-14 → 2026-06-19) — i.e. ~468k events/day
and ~576k audit rows/day, **~14 GB/month combined and accelerating** (June daily rate
~4× May's). The figures reframed Q1: a *monthly* chunk at this velocity is ~9 GB
(events) / ~5 GB (audit) — as large as the entire current table — which barely improves
the per-chunk VACUUM story, so the RFC's reflexive "monthly default" was overridden.

- **Q1 granularity → WEEKLY.** ~3.3M rows/~2.0 GB per `events` chunk, ~4.0M/~1.2 GB per
  `audit_log` chunk, ~52 chunks/yr/table. Even per-chunk VACUUM and tight (1-week) drop
  resolution at the measured rate.
- **Q2 retention horizon → `events` 3 months (drop older), `audit_log` ∞ (never drop).**
  `events` is operational telemetry whose debugging value decays in weeks; capping it at
  ~3 months bounds it near ~27 GB steady-state. `audit_log` is the forensic/compliance
  record: **partitioned-but-never-dropped** — it still wins the VACUUM/read benefits, but
  the retention executor (P4) never carves it. (P4's `events` sweep is the only DROP path
  that goes live; the `audit_log` DROP path stays disabled-by-policy until/unless a future
  decision sets a finite audit horizon.)
- **Q3 narrowed uniqueness → (a)** accept the SD-function-is-sole-writer invariant as
  sufficient, documented explicitly; no per-partition/BRIN guard, no constraint-trigger.
- **Q4 bundle layout → sibling slices within the owner-bundle line:** land `events` first,
  let it prove the pattern, then `audit_log` on its own verified cutover.
- **Q5 segment abstraction → generalize** the `audit_segments` seal/boundary-hash model
  into a shared chain-segment abstraction reused by `events` (P1), rather than minting a
  divergent parallel table.

P1+ implementation stays **ready-for-human** (the P2/P3 owner-DDL reshape is the
highest-risk slice and must land on a deliberate owner-bundle cutover). **Owner-bundle
numbers in P2–P4 below say `0016`; that number is now TAKEN (the latest owner bundle on
`main` is `0019`). Use the next free owner-bundle number at implementation time** (re-fetch
before claiming — same renumber discipline as D236/D239).

## Open Questions (resolved — see "P0 RESOLVED" above)

1. ~~**Partition granularity — monthly?**~~ **RESOLVED → weekly** (D241). The original
   monthly default assumed lower velocity; measured ~0.5M rows/day/table makes a monthly
   chunk ≈ the whole current table, so weekly is the grounded pick.
2. ~~**Retention horizon — how long before a partition is droppable?**~~ **RESOLVED →
   `events` 3 months / `audit_log` infinite** (D241). `events` operational (bounded ~27 GB);
   `audit_log` the forensic record, partitioned-but-never-dropped.
3. **Re-assert the narrowed uniqueness, or rely on the sole-writer invariant?** After the
   reshape the DB no longer enforces `event_id`-unique-per-repo or globally-unique
   `row_hash`. Options: (a) accept the SD-function-is-sole-writer invariant as sufficient
   (simplest; the append functions are the only writers and `row_hash` collision is a
   hash break anyway); (b) add a per-partition or BRIN-backed guard; (c) a deferred
   constraint-trigger re-check. Recommendation: **(a)**, documented explicitly, pinned
   before P2.
4. **One bundle or two for the two tables?** Ship `events` and `audit_log` in a single
   owner bundle 0016 (one capability stamp, one cutover) or sibling bundles (independent
   risk, independent rollback)? Recommendation: sibling slices within the 0016 line so
   `audit_log` (the higher-stakes record) can land on its own verified cutover after
   `events` proves the pattern.
5. **Generalize `audit_segments` into a shared chain-segment abstraction for events
   (P1), or mint a parallel `event_chain_segments` table?** The audit segment model is
   exactly what events needs for retention-safe sealing; sharing one abstraction avoids
   drift, but `audit_segments` is currently audit-specific and owner-shaped. Pin in P1.

## Domain Modeling

This is a **boundary clarification plus a new value object**. The new value object is the
**chain segment** as the unit of retention: a sealed, hash-witnessed range of a
tamper-evident log that can be physically retired (partition DROP) without breaking the
chain's provable continuity — already realized for audit (`audit_segments`), generalized
to events here. The boundary clarification is that **retention operates on sealed
segments, never on rows**, and that *physical* storage layout (range partitions) is made
*semantically invisible* to the chain invariant (the row hash and chain linkage are
unchanged by partitioning). The partition key joining the identity tuple is an
aggregate-key change on the two log aggregates. Cites
[`docs/DDD.md § "Adding to the model"`](../explanation/domain-driven-design.md#adding-to-the-model);
RFC 0019 is the precedent.
