# FALSIFIER — RFC 0143 design-v3 lifecycle re-attack

author: falsifier-reviewer-004

## Verdict

**Material lifecycle challenge lands: needs_revision unless corrected.**

The v3 holder is materially stronger than v2 on the exact lifecycle lens this
packet asked me to verify. BC4/F3 is now a real mechanism: it names
`jobs.recovery_generation`, an owner-bundle location, increment points, and a
stamped packet value. BC5/F5 is also mostly concrete: `resealGraceWindow = 30s`,
cap at the packet heartbeat window, same `lockRun` serialization against
publish/complete/recovery, and typed refusal instead of raw `lease_error`.

The remaining buildability gap is narrower but still load-bearing: the spec's
one-extension-only grace rule depends on a new durable marker,
`leases.reseal_grace_extended_at`, but the migration/authority placement for
that column is left conditional even though current source can decide it. Since
that marker is what makes `TestResealGraceCannotReviveRequeuedLease` and the
same-lease-extension half of BC5 falsifiable, the lifecycle contract is not yet
fully pinned.

## Required Lifecycle Recheck

### BC4 / F3 — genuinely resolved in the revised spec

Current source confirms the old v2 gap exactly as stated: `jobs` has
`current_lease_id` and no recovery/lease generation
(`go/pkg/db/sql/0005_repo_local_workflow_state.sql:75-104`), `leases` has no
generation or grace marker (`:166-186`), `job_recovery_state` is a budget/counter
table (`go/pkg/db/sql/0020_job_recovery_state.sql:13-28`), `review_generation` is
the review/verdict epoch (`go/pkg/db/sql/owner/0009_review_generation.sql:1-30`),
and `activeLeaseFor` checks active/owner/job/expiry but no generation
(`go/pkg/mutations/mutations.go:803-820`).

v3 names the missing mechanism: owner bundle
`go/pkg/db/sql/owner/0021_job_recovery_generation.sql`,
`jobs.recovery_generation`, a bump of `LatestOwnerBundleVersion` and
`RESERVATIONS.toml`, increment on claim/requeue/release/recovery lease-retire
paths, and `lease.recovery_generation` stamped into `work_packets.packet_json`.
The current claim path has the exact seams the spec names: it inserts a lease,
sets `jobs.current_lease_id`, builds the packet, and persists it in
`work_packets` (`go/pkg/mutations/claim.go:193-260`, packet lease block at
`:543-574`).

`TestResealPredicateUsesStampedRecoveryGeneration` would actually fire if it
sets live `jobs.recovery_generation != packet.lease.recovery_generation` and
asserts typed refusal. This is no longer the v2 "generation with no storage"
hand-wave.

### BC5 / F5 — mostly resolved, but the grace marker schema is not pinned

The main BC5 requirements are answered. `resealGraceWindow = 30 * time.Second`,
`min(resealGraceWindow, packet.lease.heartbeat_after_seconds)`, and "no heartbeat
capability" are numeric and source-bound. The lock-order claim also matches
current source shape: `lockRun` is the RFC 0104 first statement before run-scoped
row locks (`go/pkg/mutations/mutations.go:640-665`); `artifact.publish` takes
`lockRunForJob` at the start of its tx (`go/pkg/mutations/artifact.go:75-85`);
`work.complete` takes `lockRunForJob` before locking the job row and before
`activeLeaseFor` (`go/pkg/mutations/lifecycle.go:1151-1180`); and the recovery
sweep drains helper events in short transactions before the main sweep tx, then
takes `lockRun` before `expireLeases` (`go/pkg/mutations/recovery.go:575-587`,
`:601-621`). That is enough for `TestResealBeyondGraceRoutesTypedNotLeaseError`
and `TestRecoveryRequeueWinsOverExpiredLeaseReseal` to be meaningful.

The gap is the one-extension guard. The holder says the extension is "gated by a
new `leases.reseal_grace_extended_at timestamptz`" but then defers its DDL:
"added in the same owner bundle 0021 if `leases` is owner-held, else a runtime
migration — the build run keys this to the table's ownership." That is not a
concrete migration location. Current source already exposes the ownership/DDL
boundary:

- The above-floor runtime ALTER allowlist is derived from owner-bundle transfers,
  not from ad hoc judgment (`go/pkg/db/owner_runtime_ownership.go:8-18`, `:37-75`).
- Owner bundle 0018 transfers a named cohort to `striatumd_rw`, but not `leases`
  (`go/pkg/db/sql/owner/0018_runtime_table_ownership_transfer.sql:80-90`).
- Owner bundle 0019 transfers supervisor pointer tables, not `leases`
  (`go/pkg/db/sql/owner/0019_supervisor_pointer_runtime_ownership.sql:65-69`).
- A direct search found no `ALTER TABLE striatumd.leases OWNER TO striatumd_rw`;
  the only direct `leases` DDL is the original table in migration 0005.
- `write_authority_inventory.go` classifies `leases` as runtime DML
  (`:107-108`), but runtime DML is not the same as runtime-owned/alterable DDL.

So the holder cannot leave this to "if owner-held, else runtime migration." The
spec must say exactly where `leases.reseal_grace_extended_at` lands, and the
current evidence points to the owner-bundle side unless the Holder proves the
runtime ALTER allowlist includes `leases`.

## Material Challenge: The Once-Only Grace Guard Is Not Yet Falsifiable

**Claim attacked.** v3 claims one same-lease extension only: reseal may extend the
bound lease by `grace` exactly once if the lease is only just expired, the
stamped recovery generation still matches, and `leases.reseal_grace_extended_at`
is NULL.

**Concrete refutation.** The once-only property is not just an algorithmic rule;
it is a durable schema claim. Without the marker column in the correct DDL track,
two bad outcomes remain plausible in the build slice:

1. The implementation puts `reseal_grace_extended_at` in a runtime migration even
   though `leases` is not runtime-alterable. That will pass in a single-role
   mental model and fail the two-role owner/DDL guard or a production deploy,
   leaving the build unable to prove the BC5 contract.
2. The implementation avoids the schema issue by holding the once-only state in
   memory or in event payloads. That makes `TestResealGraceCannotReviveRequeuedLease`
   too weak: it can pass one process-local path while a restart/retry can offer a
   second grace extension on the same lease.

The spec's own BC5 safety relies on this marker being durable and transactionally
locked with the lease row. The migration location is therefore not clerical; it is
part of the safety predicate.

**Strongest rebuttal for the Holder.** The spec gives both possible DDL tracks and
says the build run must key the choice to table ownership. If the implementer
chooses the right branch, the behavior is safe.

**Why a real gap remains.** This run is producing a falsifiable implementation
spec, not a reminder to rediscover source facts in the build run. The current
source already provides the ownership derivation and the reservation frontiers
(`RESERVATIONS.toml`: runtime 0043, owner bundle 0020). A buildable spec should
reserve/name the exact artifact, for example: add `leases.reseal_grace_extended_at`
in owner bundle `0021_job_recovery_generation.sql` alongside
`jobs.recovery_generation` and add the matching reservation, or explicitly prove
`leases` is runtime-alterable and name runtime migration `0044`. Leaving both
branches in the spec means the test suite has no single DDL contract to assert.

**Test that should fire.** Extend `TestResealGraceCannotReviveRequeuedLease` or add
`TestResealGraceMarkerShipsInCorrectDDLTrack`: assert the shipped migration/bundle
contains `leases.reseal_grace_extended_at`, that the reservation ledger includes
that ordinal, and that the runtime owner-DDL guard remains green in the two-role
posture. I ran the current guard subset from `go/`:

`go test ./pkg/db -run 'TestFutureRuntimeMigrationsDoNotCarryOwnerDDL|TestFutureRuntimeMigrationsDoNotFKOwnerHeldTables|TestReservationLedgerMatchesOnDisk'`

It passes now; the new DDL must keep it green.

## F7 File-Mirror Check

No regression found in the file-mirror half. The v3 seed and holder carry forward
the credited mechanism: endpoint/epoch state moves off lane-writable scratch to a
daemon-owned, lane-read-only 0644 path, with `O_NOFOLLOW`, atomic temp/rename, and
rejection of missing boot-epoch headers on the supervised path
(`docs/operator/workflows/rfc-0143-design-v3/SEED.md:104-110`,
`docs/operator/artifacts/rfc-0143-design-v3/dialogue/holder/HOLDER.md:106-110`,
`:563-566`). The remaining channel-integrity half depends on BC1, not this
lifecycle review.

## Bottom Line

Do not re-open the v2 lifecycle criticism wholesale: v3 did the main work. But do
not clear BC5 yet either. The one-extension grace rule is a load-bearing schema
claim, and v3 still defers the exact DDL placement for its marker column. Pin that
column to the correct migration/owner-bundle path and make the guard test assert
it; then the lifecycle cluster has a credible path to clearing.
