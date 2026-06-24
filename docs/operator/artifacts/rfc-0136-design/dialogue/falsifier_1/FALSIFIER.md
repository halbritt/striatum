# FALSIFIER - RFC 0136 chain-head RI is not preserved by Go-only writer discipline

author: falsifier-reviewer-003

## Claim challenged

The holder's core claim is that the forced partition-key reshape preserves every existing integrity guarantee while moving the invalidated `repo_event_chain_heads -> events` SQL FK into the Go/SD write path. H3 says this loses no real guarantee because the known writers insert the event row and advance `repo_event_chain_heads.last_event_id` in the same transaction.

That is not equivalent to the current schema guarantee. Today, `events` has `PRIMARY KEY (repository_id, event_id)` and the chain head has a real FK to that pair. The same source also grants `striatumd_rw` direct `SELECT, INSERT, UPDATE` on `repo_event_chain_heads`, and the write-authority inventory classifies that table as `ClassRuntimeDML`, not SD-gated. Dropping the FK without changing that SQL privilege surface leaves a direct database path that H3's Go writer-set guard cannot police.

## Concrete counterexample

After the proposed P2 reshape, `events` becomes keyed by `(repository_id, event_id, created_at)`, so the existing FK from `repo_event_chain_heads(repository_id, last_event_id)` to `events(repository_id, event_id)` cannot be re-declared. If the holder's proposed replacement is only the known co-transactional Go/SD writers, the runtime role can still execute the table-level DML shape that source currently grants:

```sql
UPDATE striatumd.repo_event_chain_heads
   SET last_event_id = 999999999999,
       last_hash = 'not-an-event-row-hash',
       updated_at = now()
 WHERE repository_id = 'repo_1';
```

With the current FK, commit fails unless `events(repo_1, 999999999999)` exists. Under H3 as written, the FK is gone and there is no replacement trigger, SD-only wrapper, composite FK, or privilege revocation that rejects the update. The next append path then reads `last_hash` from `repo_event_chain_heads ... FOR UPDATE`, computes the next event's `previous_hash` from that bogus value, inserts the event, and advances the head. The table can now report a head whose `last_event_id` never existed, and the subsequent row can be chained to a hash that was never an event row hash.

Source anchors checked:

- `go/pkg/db/sql/0005_repo_local_workflow_state.sql:324-342`: current `events` PK is `(repository_id, event_id)` with the six outbound FKs.
- `go/pkg/db/sql/0006_events_chain_anchors.sql:72-81`: current `repo_event_chain_heads` FK references `events(repository_id, event_id)` and is `DEFERRABLE INITIALLY DEFERRED`.
- `go/pkg/db/sql/0006_events_chain_anchors.sql:98-112`: the head table deliberately has no no-update trigger and grants runtime `SELECT, INSERT, UPDATE`.
- `go/pkg/db/write_authority_inventory.go:50-55,115`: `repo_event_chain_heads` is runtime DML.
- `go/pkg/db/sql/owner/0004_phase2_events.sql:121-149`: `append_event_row` reads the head hash, inserts the event, and upserts the head.
- `go/pkg/mutations/mutations.go:1782-1818,1833-1836` and `go/pkg/reads/escalation_resolve.go:552-588,599-602`: the Go writers have the same event-insert then head-upsert pattern, but they do not constrain raw SQL updates to the head table.

## Failing cases

1. FK preservation fails: after the SQL FK is removed, `repo_event_chain_heads.last_event_id` can point at no `events` row through the retained runtime DML surface.
2. Chain integrity fails: a bogus `last_hash` becomes the `previous_hash` input for the next append, so the chain can be advanced from a non-event hash even if the append function itself routes across partitions correctly.
3. Append-only protection is incomplete: `events_no_update` and `events_no_delete` may still protect partition rows, but the mutable head pointer is the chain serialization point and has no comparable DB-level integrity check once the FK is gone.
4. Row identity remains under-specified: after `events` is keyed by `(repository_id, event_id, created_at)`, the head still stores only `(repository_id, last_event_id)`. If any duplicate logical event id is introduced by owner backfill, sequence reset, fixture insert, or other non-append DML, the head no longer names one unique parent row.
5. The same integrity-narrowing pattern exists on `audit_log`: current source has `row_hash text NOT NULL UNIQUE`; the proposed partition-legal `UNIQUE(row_hash, ts)` no longer database-enforces global row-hash uniqueness. The holder may accept that as an application/hash invariant, but that is a narrowed guarantee, not preservation of the existing one.

## Strongest rebuttal

The strongest holder rebuttal is that legitimate daemon append paths are co-transactional: they take the head lock, allocate the event id, insert the event row, compute the hash, and upsert the head in one transaction. On the happy path, the runtime role is not supposed to issue arbitrary SQL against `repo_event_chain_heads`; duplicate event ids should not come from the identity sequence; and duplicate audit hashes are cryptographically implausible unless the hash function or owner backfill is already broken.

That rebuttal lowers the guarantee from database-enforced integrity to discipline around the expected application paths. The packet's hard claim is stronger: the reshape should preserve every existing integrity guarantee. Current source gives the runtime role an explicit SQL write privilege on the head table, and the current FK is the database mechanism that makes that privilege safe. A grep/AST guard over Go writer sites does not cover a table that remains `ClassRuntimeDML`.

## Unanswered gap

H3 needs a database-level replacement before it can claim preservation. Either:

1. add `last_created_at` to `repo_event_chain_heads` and keep a real composite FK to `events(repository_id, event_id, created_at)`, or
2. revoke direct runtime `INSERT/UPDATE` on `repo_event_chain_heads` and expose an SD function or trigger that verifies the target event row under lock before the head changes.

If the design intentionally accepts losing the old SQL-enforced row identity, global audit row-hash uniqueness, and chain-head FK guarantee, the core claim must say that explicitly and name the replacing invariant. The required pgtests should include: direct runtime-DML head advance to a non-existent event is rejected after the FK removal; duplicate `(repository_id, event_id)` across partitions is rejected or explicitly accepted by the narrowed contract; duplicate `audit_log.row_hash` across partitions is rejected or explicitly accepted by the narrowed contract.
