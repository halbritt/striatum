# FALSIFIER - RFC 0136 retention-state and P5 execution gap

author: falsifier-reviewer-002

## Gate result

Material challenge: the holder SPEC does not yet make the retention path executable
or auditable enough to clear the owner-DDL / retention / P5 gate.

The holder correctly says the live reshape must wait for RFC 0142 P5, and it
correctly narrows `audit_log` to infinite retention under D241. The gap is that
H6/H7 still treat "sealed segment -> partition DROP -> purged/retired chain gap"
as a pinned contract for `events`, while current source proves the required event
segment state transition is impossible: a sealed `event_chain_segments` row is
frozen, and the existing pgtest asserts that updating its `retention_state` to
`purged` must fail.

That leaves the P4/P5 executor with three bad options: drop an event partition
without recording a purge transition, change the runtime segment schema/trigger
before the owner partition DROP, or put that runtime-table change in the owner DDL
bundle. The SPEC names none of those as a required expand/contract step, and the
last two are exactly where the owner/runtime DDL boundary has bitten this repo.
This should stop the gate from clearing until the retention state machine and its
DDL placement are specified.

## Claim attacked

H6 says a dropped partition is acceptable only if retirement is modeled as a
sealed segment boundary, that dropping a partition is "purging a closed, sealed,
boundary-hashed segment," and that the shipped `event_chain_segments` ledger plus
doctor invariant "give events the same seal-before-drop capability the audit
chain has" (`HOLDER.md:197-227`).

H7 then says partition `DETACH`/`DROP` subsumes #386 for the retention path
because retiring old `events`/`audit_log` data becomes catalog DDL rather than row
`DELETE` (`HOLDER.md:229-257`).

H9 says P0 pins backfill form A and leaves only online-safety/rehearsal to RFC
0142 P5 (`HOLDER.md:293-316`).

Those claims skip the state transition between "sealed and still retained" and
"purged because the backing partition was dropped." Current source has a
`purged` state, but the only shipped trigger semantics make it unreachable after
a segment is sealed.

## Concrete refutation

Current migration 0041 freezes every non-open event segment:

```sql
IF OLD.state <> 'open' THEN
  RAISE EXCEPTION 'sealed daemon event chain segments are append-only';
END IF;
```

That is not just an implementation detail. The migration comments say only an
open segment may be updated, and the trigger is installed on every UPDATE
(`go/pkg/db/sql/0041_event_chain_segments.sql:88-106`). The existing test drives
the exact retention transition a future executor would need and expects it to be
rejected:

```sql
UPDATE striatumd.event_chain_segments SET retention_state='purged'
 WHERE repository_id=$1 AND segment_id=1
```

`TestSealedEventSegmentIsAppendOnly` treats success as a failure
(`go/pkg/mutations/event_chain_segments_pg_test.go:317-323`). I reran that test
from the Go module and it passed:

```sh
go test ./pkg/mutations -run TestSealedEventSegmentIsAppendOnly -count=1
```

So the concrete failing retention run is:

1. Seal the event segment that aligns to an old weekly partition.
2. Try to mark the sealed segment `state='purged'` or
   `retention_state='purged'` as the durable witness that the partition is about
   to be detached/dropped.
3. The trigger rejects the update before the partition can be retired.

If the executor drops first and leaves the segment in `state='sealed',
retention_state='active'`, it has not recorded the retention act. The existing
doctor check only proves sealed/purged segment seam completeness and continuity;
it explicitly says P1 has no dependency on the not-yet-built retention executor
(`go/pkg/reads/doctor_event_chain_segment.go:18-24`). It has no partition-drop
ledger to compare against, so it cannot implement H6's promised
`partition_dropped_without_sealed_segment` check from the current source alone.

This is a retention safety failure, not a cosmetic mismatch. A dropped partition
is a real transcript gap. Without a reachable "this sealed segment was purged by
this partition DROP" state, the durable evidence cannot distinguish:

- a sealed segment still backed by rows,
- a sealed segment whose rows were intentionally dropped under retention policy,
- a wrong partition dropped by hand or by a buggy executor after some unrelated
  segment was sealed.

The chain seam can still be mathematically witnessed, but the retention act is
not auditable as the daemon-mediated operation H6/H7 require.

## Owner-DDL and P5 dependency failure

The required repair crosses ownership planes. `event_chain_segments` is a runtime
table from migration 0041; the partitioned `events` table and partition
management are owner DDL. If P4/P5 fixes this by altering
`event_chain_segments` triggers or adding a separate purge ledger, that schema
change must be sequenced with the owner partition bundle and capability stamp.
If it is placed in the owner bundle, it is modifying the runtime side of the
per-object split. If it is placed in a runtime migration, the owner DROP must
refuse until that runtime state machine exists. H8/H9 do not specify either
ordering or the fail-closed check.

The P5 rehearsal therefore needs more than "attach + rename within a lock
budget." It must rehearse this full sequence:

1. runtime retention-state schema/trigger available,
2. sealed segment marked as purged or recorded in a purge ledger,
3. owner `DETACH`/`DROP` executes,
4. doctor verifies both seam continuity and that the dropped partition has a
   matching purged segment record.

The holder SPEC currently pins only step 3's shape and step 4's desired words,
not the state machine that makes them executable.

## Audit-log overclaim

D241 resolved `audit_log` to infinite retention: partitioned but never dropped,
and P4's live DROP path is events-only
(`docs/rfcs/0136-range-partition-events-audit-log-by-time.md:392-398`). The
holder repeats that in H6, but H7 still says "retiring old
`events`/`audit_log` data becomes a partition `DETACH`/`DROP`" and frames #386
subsumption as `events/audit_log` retention-delete coverage.

For `audit_log`, there is no live retention DROP under this RFC. The strongest
defense is that this is intentional policy and H7's "honesty boundary" keeps
#386's indexes. That defense means the SPEC should narrow the subsumption claim:
events partition DROP removes the events retention-delete cliff; audit_log gets
partitioning/read/VACUUM benefits and keeps infinite retention. It does not get
an audit retention-DROP path unless a future decision changes the horizon and
reopens the audit purge design.

## Strongest rebuttal

The strongest holder rebuttal is that a sealed segment record alone is enough to
prove hash continuity after a drop, and P4 is allowed to add the retention
executor, a purge ledger, or an allowed sealed-to-purged transition later. H9
also honestly says the live cutover cannot execute until RFC 0142 P5.

That rebuttal does not clear this gate. The adjudicator is supposed to score
whether P0 pins the retention contract that the build run will execute after P5.
Right now the contract depends on a state transition that current source forbids
and on a cross-plane DDL sequencing rule the SPEC has not named. "P5 will figure
out how to make purged reachable" is not a falsifiable build-ready
specification.

## Required repair

Before this clears, the SPEC needs to add one explicit retention-state design and
test set:

1. Define whether event partition retirement updates
   `event_chain_segments.state`, updates `retention_state`, or writes a separate
   append-only partition-purge ledger.
2. Make that transition reachable for sealed segments without reopening arbitrary
   sealed-segment mutation.
3. State whether the schema change is a runtime migration, owner bundle step, or
   paired expand/contract step, and require a fail-closed capability check so the
   owner DROP cannot run before the runtime purge evidence path exists.
4. Add pgtests that a sealed segment can be retired exactly once, a non-sealed or
   straddling segment cannot be retired, and doctor reds on a dropped partition
   with no matching purged segment evidence.
5. Narrow H7's #386 subsumption language to the events retention-DROP path unless
   the design intentionally reopens finite audit retention.

## Verdict

Real gap remains. H6/H7 correctly identify that partition DROP must be
segment-aware, but the current event segment ledger cannot record the purge act
after sealing, and H9 does not assign that missing state machine to P5's
expand/contract rehearsal. The gate should not clear until the retention
transition is specified and tested.
