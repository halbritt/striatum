# HOLDER (v1) — RFC 0136: range-partition `events` and `audit_log` by `created_at`/`ts`, partition DROP as the retention path that subsumes #386 — the implementation SPEC

author: holder-author-001

> **Fresh v1 leading proposal for the `rfc-0136-design` `falsification_gate` run.** This is
> the published claim Falsifier 1 (reshape / chain-integrity lens) and Falsifier 2 (owner-DDL
> / retention / P5-dependency lens) re-attack. The deliverable the cleared cycle commits is the
> falsifiable spec the `rfc-0136-build` `code_change` run executes **once RFC 0142 P5 lands**
> (the explicit blocker — §9). Required context read in full first:
> `docs/operator/workflows/rfc-0136-design/SEED.md` and
> `docs/rfcs/0136-range-partition-events-audit-log-by-time.md`.
>
> **Every source anchor below is re-verified line-by-line against the worktree at `main`.** That
> re-verification surfaced **three corrections to the RFC's own anchors/claims**, each load-bearing
> and each baked into the assertions below so the falsifiers can confirm them: (C-1) the owner
> bundle is **NOT `0020`** — `0020_owner_bundle_watermark_read.sql` already occupies it on disk, and
> `0021` is design-reserved by RFC 0142 P4 — so the reshape's ordinal must be re-fetched and is
> `≥ 0022` (§H8); (C-2) the chain-head FK is at **`0006:80-81`**, not `0006:72` as the RFC text says
> (§H2/H3); (C-3) the runtime-DDL guard is `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` at
> **`migrations_test.go:643`**, not `:423` (§H8). A fourth, subtler correction (C-4): the RFC's
> "the SD append function is the **sole** writer of `repo_event_chain_heads`" is **imprecise** —
> there are **three** structurally-identical writers; the safe-to-drop-the-FK argument survives on
> **co-transactionality**, not singleness (§H3).

---

## 0. The single load-bearing claim (the hard core)

> **CORE.** Declarative range partitioning of the two owner-held, append-only, hash-chained tables
> `striatumd.events` (by `created_at`) and `striatumd.audit_log` (by `ts`) **FORCES a PRIMARY-KEY /
> UNIQUE-constraint reshape** on both tables — because PostgreSQL requires the partition-key column
> to be a member of the table's PK and of every UNIQUE constraint — **and that reshape preserves
> every existing integrity guarantee** (the six `events` FKs, the `audit_log.segment_id` FK,
> `audit_segments` / `audit_chain_head`, the cross-table `DEFERRABLE INITIALLY DEFERRED` chain-head
> FK, the two in-DB hash chains, and the append-only triggers), **provided** the one inbound FK that
> the reshape structurally invalidates (`repo_event_chain_heads → events`) moves its referential
> integrity into the (already co-transactional) Go/SD write path, and **provided** the reshape ships
> as owner DDL whose execution on the live ~14M-row `events` table is rehearsed on RFC 0142 P5's
> ephemeral two-role clone before it touches production.

The CORE decomposes into nine falsifiable assertions (§H1–§H9). Each states the claim, the source
anchor that grounds it, the evidence that supports it, and **the concrete observation/test that
would refute it** — the falsifiers' attack surface. §9 then draws the honest P5-dependency boundary
the adjudicator gates on: what P0 can pin now, and what genuinely cannot be built until 0142 P5.

---

## 1. The falsifiable assertion ledger

### H1 — The reshape is FORCED and partition-legal (the spine)

**Claim.** To range-partition by `created_at`/`ts`, the keys MUST change to:

| Table | Partition key | Current PK | Current UNIQUE | Forced reshape |
| --- | --- | --- | --- | --- |
| `events` | `created_at` | `(repository_id, event_id)` | none | PK → `(repository_id, event_id, created_at)` |
| `audit_log` | `ts` | `(audit_id)` | `(row_hash)` | PK → `(audit_id, ts)` **and** UNIQUE → `(row_hash, ts)` |

Neither current key contains its partition column, and PostgreSQL refuses a partitioned table whose
PK or any UNIQUE constraint omits the partition key (each partition is an independent index; global
uniqueness over only non-partition columns is unenforceable). So partitioning is **not** a
transparent `ALTER` — it is a key reshape.

**Source.** `events`: `0005_repo_local_workflow_state.sql:324` (`CREATE TABLE … events`), PK
`(repository_id, event_id)` and `created_at timestamptz NOT NULL` in that block. `audit_log`:
`0001_baseline.sql:84` (`audit_id … PRIMARY KEY`), `:85` (`ts timestamptz NOT NULL`), `:99`
(`row_hash text NOT NULL UNIQUE`). Both partition columns are already `NOT NULL` and already written
by the append functions (`v_created_at`/`v_ts := date_trunc('second', now())` —
`owner/0004_phase2_events.sql`, `owner/0001_authority_phase0.sql:172`), so **no new not-null
backfill is required for new rows** and the partition key is never NULL.

**Refuting observation.** A pgtest that creates `events_partitioned … PARTITION BY RANGE (created_at)`
with PK `(repository_id, event_id)` only (or `audit_log_partitioned PARTITION BY RANGE (ts)` with
PK `(audit_id)` / UNIQUE `(row_hash)`) — PostgreSQL must raise *"unique constraint on partitioned
table must include all partitioning columns"*. If it does **not** error, "the reshape is forced" is
refuted. Conversely the reshaped keys must create cleanly. (Pins the PG-version floor for declarative
range partitioning + partition-key-in-key, ≥ PG 11/12; the deploy already targets modern PG.)

### H2 — The reshape preserves every other constraint; the chain-head FK is the ONLY inbound-FK casualty

**Claim.** The reshape touches **only** the two tables' own PK/UNIQUE and **inbound** references.
Specifically:
- The **six FKs OUT of `events`** — `(repository_id, run_id|actor_session_id|job_id|message_id|
  artifact_id|lease_id)` → `runs|sessions|jobs|queue_messages|artifacts|leases` — are **unaffected**;
  a partitioned table may freely carry outbound FKs.
- The **`audit_log.segment_id` FK OUT** to `audit_segments` is **unaffected**; `audit_segments` stays
  an ordinary (unpartitioned) table.
- `audit_chain_head` carries **no FK into `audit_log`** (it is a bare singleton pointer), so the
  audit side needs **no inbound-FK surgery**.
- The **only** inbound FK that the reshape structurally invalidates is the cross-table
  `repo_event_chain_heads (repository_id, last_event_id) → events(repository_id, event_id) DEFERRABLE
  INITIALLY DEFERRED`: once the events PK becomes `(repository_id, event_id, created_at)`,
  `(repository_id, event_id)` is no longer a key, and a FK may only reference a PK/UNIQUE, so
  PostgreSQL refuses the declaration. (Resolution in §H3.)

**Source.** Six outbound `events` FKs: `0005:…` (the `events` block, lines `FOREIGN KEY (repository_id,
run_id)…` through `… lease_id`). `audit_log.segment_id` FK: `0001:100` (`segment_id bigint NOT NULL
REFERENCES striatumd.audit_segments(segment_id)`). `audit_chain_head` (no FK): `0001:103-106`
(`singleton boolean PRIMARY KEY …, last_audit_id bigint, last_hash text` — no `REFERENCES`).
Chain-head FK: **`0006_events_chain_anchors.sql:80-81`** (`FOREIGN KEY (repository_id, last_event_id)
REFERENCES striatumd.events(repository_id, event_id) DEFERRABLE INITIALLY DEFERRED`) — **correction
C-2: the RFC text says `0006:72`; the actual FK is at `:80-81`.**

**Refuting observation.** A catalog query at build time —
`SELECT conname, conrelid::regclass FROM pg_constraint WHERE contype='f' AND confrelid =
'striatumd.events'::regclass` — must return **exactly** `repo_event_chain_heads`'s FK; the same
query with `confrelid = 'striatumd.audit_log'::regclass` must return **nothing**. If either returns
an additional inbound FK, the "only casualty" claim — and the §H7 retention-DROP-is-RI-scan-free
argument that depends on it — is refuted, and that FK must be added to the reshape plan. A pgtest
must also confirm all six outbound `events` FKs and the `audit_log.segment_id` FK still validate
(RI-check on parent delete) against the partitioned shape.

### H3 — Dropping the chain-head FK and moving its RI into Go is integrity-preserving (the sharp edge, corrected)

**Claim.** The chain-head FK (§H2) is dropped, and its referential integrity is enforced in Go/SD,
inside the same transaction that advances the head. This loses **no real guarantee**, because
**every** writer of `repo_event_chain_heads.last_event_id` **inserts the referenced event row in the
same transaction, immediately before upserting the head (with the head row locked `FOR UPDATE`)** —
so `last_event_id` provably references an event the same transaction just wrote. The SQL FK was
belt-and-suspenders over this transaction-local invariant; the `DEFERRABLE INITIALLY DEFERRED`
clause existed only to let the event INSERT and the head UPSERT co-commit without an FK flap, which
disappears with the FK.

**Correction C-4 (held openly, against the RFC's "sole writer" wording).** The RFC says the SD
`append_event_row` is the *sole* writer. Source re-verification shows **three** structurally-identical
writers, not one: (1) the SD `append_event_row` — `owner/0004_phase2_events.sql` (locks the head
`FOR UPDATE`, computes the in-DB hash, INSERTs the event, advances the head, `:122-124`/`:143`); (2)
`go/pkg/mutations/mutations.go:1782-1818` (INSERT `events` → INSERT … `repo_event_chain_heads … ON
CONFLICT … DO UPDATE`, with `previousChainHead` taking `FOR UPDATE` at `:1835`); (3)
`go/pkg/reads/escalation_resolve.go:552-588` (the identical INSERT-event-then-upsert-head shape,
`FOR UPDATE` at `:601`). The safe-to-drop argument therefore rests on **co-transactionality across
the closed writer set**, not on singleness — which is the *stronger* and *correct* basis. (`audit_chain_head`
has no FK and is a bare singleton — `0001:103-106` — so the audit side needs no equivalent surgery.)

**Refuting observation (three distinct attacks for Falsifier 1).**
1. **A fourth, non-co-transactional writer.** A build-time guard test (grep + AST) must assert the
   writer set of `repo_event_chain_heads.last_event_id` is exactly the three sites above (plus tests)
   and that each upserts the head in the same statement-batch/tx as the event INSERT. If a writer
   exists that advances `last_event_id` **without** inserting the referenced event in the same tx,
   the claim is refuted and that path needs the explicit Go RI check.
2. **Drive the RI breach directly.** With the SQL FK dropped, a pgtest that attempts to advance
   `repo_event_chain_heads.last_event_id` to a non-existent `event_id` through each writer path must
   be rejected (structurally, because the event is inserted first; or by an explicit guard if any
   path is refactored to decouple). If the head can come to point at a non-existent event, refuted.
3. **Cross-partition resolution.** Because the head and the event it references may land in different
   partitions, a pgtest with events spanning ≥ 2 partitions must show the head advance still
   co-commits and the (now Go-enforced) RI holds across the partition boundary.

### H4 — The two hash chains are byte-invariant across partitioning (partitioning is semantically invisible)

**Claim.** Partitioning is a *physical storage* change that alters **no column value and no hash
input**, so both row hashes and both chain linkages are invariant by construction. `created_at` and
`event_id` are **already** inputs to the event hash; `ts` is **already** an input to the audit hash;
adding `created_at`/`ts` to the *PK tuple* does not change the *hash tuple*. The chain verifier
checks **linkage** (`previous_hash == prior row_hash`), not physical layout.

**Source.** `event_v3_row_hash` (`owner/0004_phase2_events.sql:37-76`) digests, in fixed order,
`previous_hash, repository_id, event_id, run_id, event_type, actor_session_id, job_id, message_id,
artifact_id, lease_id, payload, created_at` — i.e. **`event_id` and `created_at` are both already
hashed**. `audit_v3_row_hash` (`owner/0001_authority_phase0.sql:107-148`) digests `…, v_ts, …,
segment_id` — **`ts` already hashed**. The Go-side `canonicalEventHash` (`mutations.go:1768`) hashes
the same row material including `created_at` (`:1766`). The verifier `assertEventChainLinear`
(`go/pkg/adapterconformance/multirun_test.go:552`) asserts a single linear per-repo chain by
*linkage*, not by recomputing hash content.

**Refuting observation.** (a) A pgtest computing the row hash for identical inputs on the
partitioned table vs the monolithic table — the bytes must be identical; if they differ, refuted.
(b) `assertEventChainLinear` and the audit verifier must pass **byte-identically** against a fixture
whose rows span ≥ 2 partitions, with consecutive chained rows deliberately straddling a partition
boundary. If the verifier diverges between the partitioned and monolithic fixtures, refuted.

### H5 — Append-only triggers and append routing survive partitioning; partition DROP is trigger-clean

**Claim.** (a) The row-level `BEFORE UPDATE/DELETE` triggers `events_no_update`/`events_no_delete`
(`0005:447`/`:452`) and `audit_log_no_update`/`audit_log_no_delete` (`0001:158`/`:163`) continue to
refuse row mutation on **every** partition — PostgreSQL propagates a partitioned table's row triggers
to its partitions, and the owner bundle re-declares them on the new partitioned parent. (b) The
append functions `INSERT INTO striatumd.events`/`…audit_log` unchanged; tuple routing to the correct
partition by `created_at`/`ts` is transparent to the function body (re-validation/re-stamp only, no
body rewrite). (c) **Partition `DETACH`/`DROP` is catalog DDL, not row DML, so it does NOT fire the
`*_no_delete` row triggers** — which is precisely what makes retention possible on a table whose
triggers otherwise *forbid* row deletion.

**Source.** Triggers: `0005:447-453`, `0001:158-164`, both `EXECUTE FUNCTION
striatumd.refuse_repo_append_only_change()` (`0005:440-446`). Append routing: `append_event_row`
/`append_audit_row` `INSERT INTO` the parent (`owner/0004`, `owner/0001:200-209`). The
`*_no_delete` triggers exist because these are tamper-evident logs (RFC §"Why retention is not a
`DELETE`").

**Refuting observation.** (a) A pgtest issuing `UPDATE`/`DELETE` against a row *inside a partition*
must still raise `repo-local append-only rows cannot be updated or deleted`; if a partition escapes
the trigger, refuted. (b) A pgtest that `DETACH`+`DROP`s a partition must **not** raise the
`*_no_delete` exception and must not require a row scan; if the drop trips the trigger, the retention
primitive is refuted. (c) A pgtest appending events whose `created_at` spans ≥ 2 partitions must show
each row routed to the correct partition and the chain head advanced (the §H4 fixture doubles here).

### H6 — Partition DROP respects the chain: a partition is droppable ONLY after the segment it contains is sealed and its boundary hashes are recorded

**Claim.** A dropped partition is a real gap in the row-level transcript; that is acceptable **iff**
retirement is modeled as a *sealed segment boundary the verifier knows about*. The unifying rule:
**retention operates on sealed segments, never on rows, and partition boundaries are aligned to (or
made to imply) chain-segment boundaries**, so dropping a partition == purging a closed, sealed,
boundary-hashed segment. Concretely:
- **Audit side (already modeled).** `audit_segments` records `first_audit_id`/`last_audit_id`/
  `first_hash`/`last_hash`/`previous_segment_last_hash`/`next_segment_first_previous_hash`/
  `retention_state` and `state ∈ {open,closed,purged}` (`0001:68-81`). Aligning weekly partition
  boundaries to segment seals makes a partition DROP a `state → purged` flip whose cross-segment
  hash witnesses already prove continuity across the gap.
- **Event side (foundation already shipped, P1/D242).** The runtime `event_chain_segments` ledger
  (`go/pkg/db/sql/0041_event_chain_segments.sql`), the Go sealing path
  `pkg/mutations.SealEventChainSegment` (integrity-in-Go, no FK into owner `events`), and the
  `event_chain_segment_seam_unproven` doctor invariant (`go/pkg/reads/doctor_event_chain_segment.go`)
  give events the same seal-before-drop capability the audit chain has.
- **The straddle precondition (the sharp retention edge).** A partition is droppable **iff every
  chain segment overlapping it is sealed/closed with recorded boundary hashes and no live segment
  straddles its upper boundary.** The retention executor seals the event/audit segment at each
  partition boundary so partition ≙ sealed segment 1:1; a segment that straddles a boundary blocks
  the drop until it closes.

**Refuting observation.** (a) A pgtest that drops a partition **without** a recorded sealed-segment
boundary must be rejected by a doctor invariant (`partition_dropped_without_sealed_segment` /
`event_chain_segment_seam_unproven` / `audit_segment_purged_without_boundary_hash`), and the chain
verifier must still prove continuity across the seam **from the sealed-segment record alone** (the
dropped rows gone). If a partition can be dropped while a segment straddles its boundary, or if the
verifier cannot prove the seam post-drop, refuted. (b) Per the pinned policy (D241), the **audit_log
DROP path stays disabled-by-policy** (partitioned-but-never-dropped, infinite retention); a test must
confirm the retention executor never carves `audit_log`. Only `events` (3-month horizon) drops.

### H7 — Partition DROP subsumes #386's delete-time RI seq-scan — for the retention path, narrowly and honestly

**Claim.** #386 (owner bundle `0015`) added FK covering indexes so that *if* a parent row is deleted,
the child-side RI check is an index lookup rather than a ~13.5M-row seq-scan. For the **retention
path**, range partitioning makes that scan **not happen at all**: retiring old `events`/`audit_log`
data becomes a partition `DETACH`/`DROP` (catalog DDL, O(1) metadata) instead of a row `DELETE`
(O(rows), and forbidden by the `*_no_delete` trigger anyway), and a partition DROP performs **zero
child-side RI scan** because — by §H2 — there is **no inbound FK** into `events` (after the chain-head
FK moves to Go) or `audit_log` to re-check.

**Honesty boundary (held, not over-claimed).** The subsumption is **specifically of the
events/audit_log *retention-delete* RI-scan rationale**, NOT all of #386. The #386 covering indexes
**remain earning their keep** and the bundle is **not reverted**: they still serve (i) point lookups
by `actor_session_id`/`artifact_id`/`lease_id`/`message_id`/`segment_id`, and (ii) the *other*
direction — deleting a **parent** row (`runs`/`sessions`/…) still RI-scans `events` for children
*unless* the events partitions covering that horizon were dropped first. So #386 is the cheap
stop-gap; this RFC is the structural fix that removes the specific retention-delete cliff.

**Source.** `owner/0015_fk_covering_indexes_events_audit.sql:1-43` (#386, the `events ~13.5M rows`
figure at `:15`, the covering indexes at `:39-43`); the seq-scan-on-parent-delete rationale at
`:15-21`. Append-only `*_no_delete` triggers (§H5) forbid the row-DELETE alternative.

**Refuting observation.** A pgtest dropping a sealed historical partition must show, via
`EXPLAIN`/`pg_stat`, **no child-side RI scan and no `*_no_delete` trigger fire** (§H5b doubles
here). Falsifier 2's strongest attack: exhibit an inbound FK into `events`/`audit_log` (refuting
§H2's catalog query) that *would* be scanned on partition drop — if one exists, this subsumption is
refuted. A second attack: show a retention scenario that still requires a row `DELETE` (e.g. a
partition whose contents are not fully past the horizon) — answered by §H6's segment-aligned
droppability precondition (only fully-sealed, fully-past-horizon partitions drop).

### H8 — The reshape is owner DDL on a re-fetched ordinal (≥ 0022), capability-parity gated, never a runtime migration

**Claim.** The reshape (`ALTER`/table-rebuild + `PARTITION BY` on owner-held `events`/`audit_log`) is
**owner DDL** and ships in an **owner bundle**, never a runtime migration (D187/D215; a runtime
migration carrying owner DDL trips the build guard and crash-loops a two-role production daemon —
D187/#244). The bundle is **capability-parity gated** like owner bundle `0004` (RFC 0110 §8.2): a
binary without partition-aware append/retention refuses to serve once the bundle is stamped, and a
partition-aware binary refuses until it is stamped — fail-closed both ways. Record a
`schema_authority` capability stamp (e.g. `events_audit_partitioned`).

**Ordinal discipline (correction C-1, load-bearing for Falsifier 2).** The RFC names owner bundle
**`0020`** — **stale**: `go/pkg/db/sql/owner/0020_owner_bundle_watermark_read.sql` already occupies
`0020` on disk, and **`0021` is design-reserved by RFC 0142 P4's terminal DDL-revoke bundle**
(`DDLRevokeOwnerBundleVersion = 21`, not yet authored on disk). So the reshape's ordinal must be
**re-fetched at claim time per D236/D239** and is **`≥ 0022`** — and because 0136's reshape is gated
on 0142 P5 (§9), which lands after 0142's full bundle line (incl. 0021), the actual ordinal is
**whatever is next-free after 0142's bundles land**. The SPEC pins the *discipline* (next-free,
re-fetch, do not collide with 0142's reserved 0021), not a fixed number.

**Source.** `ls go/pkg/db/sql/owner/` → highest on disk is `0020_owner_bundle_watermark_read.sql`.
Guard: `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` at **`go/pkg/db/migrations_test.go:643`**
(**correction C-3**: the RFC says `:423`). D187/D215 owner-DDL placement rule; owner bundle `0004`
the capability-parity precedent; owner bundle `0014` (`owner/0014_chain_lock_wait_gauges.sql`) the
owner-bundle precedent on these exact two tables.

**Refuting observation.** (a) `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` must stay green —
placing any reshape DDL (`ALTER TABLE`, `DROP TABLE`, `PARTITION BY`, the new partitioned parent) in
a runtime migration (`go/pkg/db/sql/00xx_*.sql`) must make it RED; if the guard tolerates owner DDL
in a runtime migration, the discipline is unenforced and refuted. (b) A capability-parity pgtest: a
binary without `events_audit_partitioned` must refuse once the bundle is stamped, and a
partition-aware binary must refuse until it is stamped. (c) An ordinal-collision check: if the
reshape is authored at `0020` or `0021`, it collides with the watermark-read bundle or 0142 P4's
DDL-revoke bundle — the build must reject the duplicate ordinal.

### H9 — Execution on ~14M rows uses backfill form A and is rehearsable ONLY via RFC 0142 P5 (the dependency)

**Claim.** You cannot `ALTER TABLE … PARTITION BY` a non-empty table in place (PostgreSQL has no
in-place convert-to-partitioned). The bundle uses **backfill form A**: create the partitioned parent
with the reshaped PK; **attach the existing ~14M-row `events` heap as one sealed historical
partition**; cut new writes over by rename under the owner role; let retention carve future weekly
partitions. Form A is minimal-rewrite (attach, not copy) and the legacy heap becomes one immutable
historical partition retention can later drop wholesale. **However**, the *online-safety of the
cutover* (lock window vs. expand/contract vs. maintenance window) on the live ~14M-row table is the
operational risk, and **the highest-risk owner-DDL reshape is un-rehearsable until RFC 0142 P5
exists** — see the boundary in §9. P0 pins the *plan*; it does **not** claim build-readiness for the
*execution*.

**Source.** RFC §"The owner bundle and capability parity" (form A/B, recommend A); current row
counts `events ~14.0M / 20 GB`, `audit_log ~17.3M / 8.8 GB` (RFC §"P0 RESOLVED", D241). RFC 0142 P5:
`docs/rfcs/0142-…:235` — *"P5 — rehearsal receipt + expand/contract … **Unblocks RFC 0136 P2/P3
safely.** Highest-risk owner DDL; lands last"*; accepted D258, **P5 not yet built** (SEED §"Dependency").

**Refuting observation.** (a) A dry-run on a clone must show form A's attach + rename completes within
the pinned lock budget; if the attach/swap requires an unbounded `ACCESS EXCLUSIVE` window on ~14M
rows with no expand/contract seam, the "executable without an unbounded lock outage" claim is refuted
**and** that is precisely the gap §9 assigns to 0142 P5. (b) Chain-linear before **and** after the
swap: `assertEventChainLinear` + the audit verifier must pass byte-identically pre- and
post-cutover; if the swap perturbs the chain, refuted.

---

## 2. (intentionally folded into §1) — every load-bearing claim above is a falsifiable assertion

---

## 9. The RFC 0142 P5 dependency boundary (the honest gate the adjudicator scores)

RFC 0136's P2/P3 reshape is **blocked-by RFC 0142 P5** (`rehearse` + `rehearsal_receipt.v1` +
expand/contract on an ephemeral two-role clone; designed D258, **not yet built**). The maintainer
authorized **designing 0136 in parallel** with the 0142 P5 build. This SPEC is therefore split
explicitly:

**What P0 of THIS design can — and does — pin now (build-ready specification, independent of P5):**
- The **reshape shape** — the exact PK/UNIQUE changes on both tables (§H1).
- The **constraint-preservation proof obligations** — outbound FKs, `audit_segments`/`audit_chain_head`,
  the single inbound-FK casualty, the catalog-query test (§H2).
- The **chain-head FK → Go RI** resolution and its co-transactional safety basis across the verified
  three-writer set (§H3).
- The **hash-chain invariance** argument and its byte-identical refutation test (§H4).
- The **trigger/append-routing survival** and **trigger-clean partition DROP** assertions (§H5).
- The **retention contract** — seal-before-drop, partition≙segment alignment, the straddle
  precondition, the audit-never-dropped policy (§H6); the event-side foundation already shipped (P1/D242).
- The **#386 subsumption** scope and honesty boundary (§H7).
- The **owner-DDL plumbing** — owner bundle, re-fetched ordinal `≥ 0022` (NOT 0020/0021),
  capability-parity stamp pattern, the runtime-DDL guard (§H8).
- The **backfill form** (A) and the full **test-obligation set** below.

**What genuinely CANNOT be built/validated until 0142 P5 lands (no over-claim):**
- The **actual execution** of the reshape cutover on the live ~14M-row `events` table — its
  **rehearsal** on an ephemeral two-role clone (`CREATE DATABASE …_rehearsal_<planhash>` with the
  real owner+runtime roles) and the signed **`rehearsal_receipt.v1`** that proves the plan against a
  prod-shaped clone before it touches production.
- The **expand/contract primitive** (`expand_rehearsal` → `contract_swap`) that makes the form-A
  attach+swap online-safe without an unbounded lock outage (§H9a), and the **lock-budget** enforcement.
- Because the reshape is "highest-risk owner DDL, no rehearsal and no rollback under the current
  model" (RFC 0142 §Failures), the P2/P3 owner-bundle cutover **must not land** until P5's rehearsal
  harness exists. P0 specifies the plan to *be* rehearsed; P5 provides the *means* to rehearse it.

The adjudicator clears the gate on: §H1–§H9 held against both falsifiers **and** this P5-dependency
boundary stated honestly (the SPEC pins the shape, not build-readiness ahead of P5).

---

## 3. Test obligations (consolidated — all must exist before any P2/P3 owner DDL lands)

1. **Reshape-legality (H1).** Partition attempt with the un-reshaped key errors; reshaped key
   succeeds (pgtest).
2. **Constraint preservation (H2).** Catalog query proves exactly one inbound FK to `events`, none to
   `audit_log`; all six outbound `events` FKs + the `audit_log.segment_id` FK validate against the
   partitioned shape (pgtest).
3. **Chain-head RI in Go (H3).** Writer-set guard test (closed set, each co-transactional); with the
   SQL FK dropped, advancing `last_event_id` to a non-existent event is rejected on every path
   (build-guard + pgtest), including cross-partition.
4. **Hash + chain invariance (H4).** In-DB row hash byte-identical partitioned vs monolithic for
   identical inputs; `assertEventChainLinear` + audit verifier pass byte-identically over a ≥2-partition,
   boundary-straddling fixture (pgtest).
5. **Trigger + routing survival; trigger-clean DROP (H5).** `UPDATE`/`DELETE` inside a partition still
   refused; append routes to the correct partition + head advances; `DETACH`/`DROP` fires no
   `*_no_delete` trigger and needs no row scan (pgtest).
6. **Seal-before-drop retention (H6).** Drop without a sealed-segment boundary rejected by doctor;
   verifier proves continuity across the seam from the sealed record alone; `audit_log` drop path
   confirmed disabled-by-policy (pgtest + doctor).
7. **#386 subsumption (H7).** Partition DROP shows no child-side RI scan (`EXPLAIN`/`pg_stat`); #386
   indexes retained for point lookups + parent-delete direction (pgtest).
8. **Owner-DDL discipline (H8).** `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` (`migrations_test.go:643`)
   stays green; reshape DDL in a runtime migration turns it RED; capability-parity refuse-both-ways
   pgtest; ordinal-collision rejection.
9. **Cutover chain-linear before/after (H9).** `assertEventChainLinear` + audit verifier byte-identical
   pre- and post-swap — **executed under 0142 P5's rehearsal harness** (this is the P5-gated obligation).

---

## 4. Local-first boundary (held)

Nothing here introduces hosted services, cloud APIs, telemetry, durable transcript capture/export, or
external persistence (AGENTS.md / D094). The reshape is owner DDL on the daemon-owned PostgreSQL
instance; the rehearsal clone (0142 P5) is an **ephemeral local** `CREATE DATABASE` on the same node
(operator-local, within the boundary — RFC 0142 §Boundary). A dropped partition is a real,
documented retention gap, not a transcript store — consistent with owner bundle 0004's
`C-EVENT-NO-TRANSCRIPTS`: the DB is a curated, hash-witnessed record, not a row-level transcript
archive.

---

## 5. Source-anchor index (re-verified at `main`; ⚠ marks corrections vs. the RFC)

| Anchor | What it grounds |
| --- | --- |
| `0005_repo_local_workflow_state.sql:324` (`events` block), PK `(repository_id, event_id)`, six outbound FKs | H1, H2 |
| `0005:447`/`:452` `events_no_update`/`events_no_delete`; `:440-446` `refuse_repo_append_only_change` | H5 |
| `0001_baseline.sql:84` `audit_id … PRIMARY KEY`; `:85` `ts NOT NULL`; `:99` `row_hash … UNIQUE`; `:100` `segment_id` FK | H1, H2 |
| `0001:68-81` `audit_segments` (boundary-hash + `state {open,closed,purged}` + `retention_state`) | H6 |
| `0001:103-106` `audit_chain_head` (singleton, **no inbound FK**) | H2, H3 |
| `0001:158`/`:163` `audit_log_no_update`/`audit_log_no_delete` | H5 |
| ⚠ `0006_events_chain_anchors.sql:80-81` chain-head FK `DEFERRABLE INITIALLY DEFERRED` (**RFC says `:72`**) | H2, H3 |
| `owner/0004_phase2_events.sql:37-76` `event_v3_row_hash` (`event_id`,`created_at` hashed); `:122-124`/`:143` append+head | H3, H4 |
| `owner/0001_authority_phase0.sql:107-148` `audit_v3_row_hash` (`ts` hashed); `:172` `v_ts`; `:200-209` append | H4, H1 |
| `go/pkg/mutations/mutations.go:1768` `canonicalEventHash`; `:1782-1818` event+head writer; `:1835` `FOR UPDATE` | H3, H4 |
| `go/pkg/reads/escalation_resolve.go:552-588` event+head writer; `:601` `FOR UPDATE` | H3 |
| `go/pkg/adapterconformance/multirun_test.go:552` `assertEventChainLinear` (linkage verifier) | H4, H9 |
| `0041_event_chain_segments.sql`, `pkg/mutations.SealEventChainSegment`, `doctor_event_chain_segment.go` (P1/D242) | H6 |
| `owner/0015_fk_covering_indexes_events_audit.sql:1-43` (#386, `~13.5M` at `:15`, indexes `:39-43`) | H7 |
| ⚠ `go/pkg/db/migrations_test.go:643` `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` (**RFC says `:423`**) | H8 |
| ⚠ `go/pkg/db/sql/owner/0020_owner_bundle_watermark_read.sql` (owner `0020` TAKEN; `0021` reserved by 0142 P4) | H8 |
| `docs/rfcs/0142-…:235` "P5 … Unblocks RFC 0136 P2/P3 safely"; accepted D258, P5 not yet built | H9, §9 |
