# FALSIFIER - RFC 0168 P0 C1 scrub-postcondition re-attack

author: falsifier-reviewer-003

## Result

C1 is still blocked by one narrow proof defect. I credit the v2 holder for fixing the original v1 state-machine hole: `active/scrubbing/quarantined/returned` is durable; `scrubbing` and `quarantined` are excluded from the free set by the held-uid predicate; `scrub-begin` removes the uid from allocation before side effects; failed proof goes to `quarantined`; the reaper handles leaked-active and stuck-scrubbing rows; restart preserves quarantine; and exhaustion counts dirty states as consumed (`HOLDER.md:133-199`, `HOLDER.md:241-309`). That closes the old "returned-but-dirty has no state" challenge.

The remaining issue is the process-domain proof itself. P1 says a return is blocked only when a `pool_uid` process remains in `R`/`S`/`D`, while tolerating `Z` zombies (`HOLDER.md:222-227`). Linux also reports stopped and traced non-zombie tasks (`T` and `t`). Those tasks have not exited, still belong to the uid, can hold file descriptors and same-uid residue, and can be resumed. A proof that permits them is not the requested proof that the per-uid kill domain is empty.

## Precise Claim Challenged

The seed and v1 ledger require a scrub postcondition that proves no dirty uid can be returned and re-leased. The v1 ledger states the required process predicate as zero non-zombie uid-owned processes before `returned`; v2 narrows the executable predicate to "no `R`/`S`/`D`" (`HOLDER.md:222-227`, `HOLDER.md:280-285`, v1 ledger `COLLABORATION_LEDGER_cycle_1.md:44-45`). Because `returned` is reached whenever P1-P5 all hold (`HOLDER.md:237-239`), the exact definition of P1 is load-bearing.

Existing source shape makes the ambiguity material rather than wordsmithing: the current `/proc` helper in `tmux_liveness.go` treats process state as a single-character zombie check (`processZombie`, `tmux_liveness.go:576-590`). If the build follows the v2 text literally, a non-zombie state outside `R/S/D` can be treated as clean even though it is still a live uid-owned task.

## Concrete Failing Case

1. Session S1 leases uid U and leaves a U-owned process stopped or ptrace-stopped (`T`/`t`). It may still hold descriptors into HOME scratch, credential paths, tmux-adjacent files, or other same-uid residue.
2. Teardown transitions the lease to `scrubbing`, correctly excluding U from the free set while scrub runs.
3. The scrub command returns, or the bounded proof observes the survivor. This is exactly the class C1 says command exit status cannot prove away.
4. P1 enumerates the U-owned PID but, as written, only fails on `R`/`S`/`D`. The process is not `Z`, but it is also not in the listed blocking set. P2-P5 can pass: tmux socket gone, credentials absent, HOME scratch reset, and per-lease ACLs removed.
5. `tx_scrub_finalize` records a clean proof and moves U to `returned`. A later S2 allocation leases U while S1's non-zombie same-uid process still exists. That reopens the same cross-lease residue class C1 was supposed to close, despite the durable quarantine state being otherwise correct.

This is a dirty re-lease through an under-specified proof predicate, not through the old missing-quarantine state.

## Strongest Rebuttal

The strongest rebuttal is that `kill -KILL -1` should normally terminate stopped/traced tasks and that the holder probably meant `R/S/D` as shorthand for all runnable or resource-bearing non-zombies, with `Z` as the only tolerated exception. The A17 wording also says a "surviving" process should make P1 fail (`HOLDER.md:293-297`).

That intent is not enough for the build spec. C1 exists because command success and happy-path intent are insufficient. The implementable rule in P1 names `R/S/D`, distinguishes only `Z` from that set, and does not say `T`, `t`, or unknown states quarantine the uid. A falsifiable spec needs the blocking predicate to be explicit.

## Required Revision

Tighten P1 to the actual gate predicate: after bounded re-kill and re-probe, there are zero `pool_uid`-owned tasks except zombies or dead tasks that cannot execute and hold no resources. Any observed non-zombie state, including `T`, `t`, or an unknown state, must fail P1, finalize as `quarantined`, emit `lane_uid_scrub_failed`, and keep the uid out of allocation across restart.

Extend A9'/A17, or add a named test, for this edge:

- `TestStoppedOrTracedUIDProcessBlocksReturn`: inject a stopped/traced U-owned survivor during scrub; assert P1 fails, the lease becomes `quarantined`, the uid is not re-leased, restart preserves quarantine, and only the same P1-P5 proof can clear it.

Also require `scrub_proof` / `scrub_failure` to record observed PIDs and `/proc` states. Doctor/operator output must be able to distinguish tolerated zombie residue from a non-zombie survivor that caused quarantine.

## Carry-Forward Check

I found no regression in the carried hard core HC-A1..A5 or in OQ1/OQ3/OQ5/OQ6/narrowing. The four-state lease model, held-uid allocation rule, crash-safe `scrubbing` state, quarantine/reaper surface, dirty-state exhaustion accounting, launch-as-only provisioning boundary, generation token, and per-uid hydration shape all move in the right direction. The challenge is confined to C1's scrub postcondition proof.

## Bottom Line

The v2 state machine is structurally much better than v1, but C1 does not clear while the proof can return a uid with a stopped/traced non-zombie same-uid task still present. The needed fix is small and load-bearing: make P1 "zero non-zombie uid-owned tasks," test the stopped/traced survivor, and record the observed process states in the proof.