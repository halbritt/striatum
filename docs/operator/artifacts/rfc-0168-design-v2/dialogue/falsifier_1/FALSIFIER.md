# FALSIFIER - RFC 0168 P0 C1 scrub postcondition challenge

author: falsifier-reviewer-001

## Result

**C1 still has one gate-blocking proof gap.** I credit the v2 holder for fixing the old v1 failure boundary in the durable state machine: `active/scrubbing/quarantined/returned` is real, the held-unique index excludes dirty uids from allocation, scrub-begin removes the uid from the free set before any side effect, failed proof goes to `quarantined`, quarantine survives restart, the reaper re-drives leaked/stuck rows, and exhaustion excludes `scrubbing` + `quarantined` (`HOLDER.md:138-168`, `HOLDER.md:178-199`, `HOLDER.md:248-262`, `HOLDER.md:293-309`). That closes the original "no non-free dirty state" challenge.

The remaining material issue is narrower: the new P1 postcondition does not actually prove the per-uid kill domain is empty. It only fails when a `pool_uid` process is in `R`/`S`/`D`, while Linux `/proc/<pid>/status` also has non-zombie stopped/traced states (`T` stopped, `t` tracing stop). A stopped/traced task is not a zombie and has not exited; it cannot be silently treated as clean residue in a proof whose required target is an empty uid process domain.

## Precise Claim Attacked

The v2 spec says the C1 fix includes a scrub **postcondition proof** rather than scrub-command exit codes (`HOLDER.md:28`, `HOLDER.md:218-239`). P1 is the load-bearing process-domain proof:

- enumerate `pool_uid`-owned PIDs via `/proc/<pid>/status`;
- assert no `pool_uid` process in `R`/`S`/`D` remains;
- record `Z` zombies without blocking because they cannot run code (`HOLDER.md:222-227`);
- allow `returned` only when P1-P5 all hold (`HOLDER.md:237-239`).

That is close, but it is not the same as the C1 requirement. The v2 seed required proof that the per-uid kill domain is empty, not merely that `kill -KILL -1` returned 0 (`SEED.md:58-68`, `SEED.md:94-99`). The v1 adjudicator prescribed a postcondition of **zero non-zombie uid-owned processes** before `returned` (`COLLABORATION_LEDGER_cycle_1.md:45`). P1 names the zombie exception but does not say every other observed state blocks return.

## Concrete Failing Case

1. Session S1 leases uid U and leaves a U-owned process in a stopped or ptrace-stopped state (`T`/`t`) with same-uid memory, file descriptors, or HOME/credential reachability.
2. Teardown enters `scrubbing`; S1's uid is correctly held out of the free set.
3. The scrub command path runs, but the process survives the command or is observed during the bounded proof window. This is exactly the class C1 says command success cannot prove away.
4. P1 enumerates the U-owned PID, sees state `T` or `t`, and the spec as written only declares `R`/`S`/`D` blocking. P2-P5 can all pass: no tmux socket, credential path absent, HOME scratch reset, ACLs removed.
5. `tx_scrub_finalize` can record clean proof and move U to `returned` even though a non-zombie U-owned process still exists. A later S2 lease of U now shares an OS uid domain with S1 residue, reopening the same cross-lease residue class C1 exists to close.

The damage does not require the old missing-quarantine bug. The state machine can be correct and still return a dirty uid if the proof predicate under-classifies a surviving non-zombie process.

## Strongest Rebuttal

The best rebuttal is that `kill -KILL -1` should normally terminate stopped/traced processes, and the holder probably intended `R`/`S`/`D` as shorthand for "processes that can still run code," with `Z` as the only safe exception. But C1 was explicitly about not trusting command success or happy-path expectation. The build spec must define the postcondition an implementation can test. As written, it does not say `T`/`t` or any unknown non-zombie state fails the proof, and A17 only says a "surviving process" makes P1 fail without covering the stopped/traced survivor variant (`HOLDER.md:293-297`).

## Required Revision

Tighten P1 to the predicate the gate asked for: after bounded re-kill/re-probe, there are **zero `pool_uid`-owned tasks except zombies/dead tasks that cannot execute and hold no resources**. Any observed non-zombie state, including `T`/`t` and any unknown state, must block `returned` and finalize as `quarantined` with `lane_uid_scrub_failed`.

Add a named negative test, or extend A9'/A17, for the missing edge:

- `TestStoppedOrTracedUIDProcessBlocksReturn`: inject a U-owned stopped/traced survivor after scrub; assert P1 fails, the row becomes `quarantined`, the uid is not allocated, restart preserves quarantine, and only the same P1-P5 proof can clear it.

Also require `scrub_proof` to record the observed PIDs and `/proc` states, so doctor/operator output can distinguish `Z` tolerated residue from a non-zombie quarantine cause.

## Carry-Forward Check

I found no regression in the v1-proven hard core HC-A1..A5 or the credited OQ1/OQ3/OQ5/OQ6/narrowing set. The structural per-lane uid model remains right; the OQ1 free predicate, OQ3 launch-as-only boundary, OQ5 generation token, and OQ6 credential-hydration shape are carried forward coherently (`HOLDER.md:44-87`, `HOLDER.md:93-115`, `HOLDER.md:311-320`, `HOLDER.md:426-451`, `HOLDER.md:460-491`). The challenge is confined to C1's scrub postcondition proof.

## Bottom Line

v2 fixes the durable quarantine/free-set hole, but C1 does not clear while P1 can interpret a stopped/traced non-zombie uid-owned process as clean. The correction is small and precise: make the process proof "zero non-zombie uid-owned tasks" and test the stopped/traced survivor case.
