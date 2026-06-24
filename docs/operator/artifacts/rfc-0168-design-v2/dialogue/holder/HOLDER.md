# HOLDER — RFC 0168 P0 falsifiable implementation spec (REVISION v2: discharge C1 lease lifecycle + C2 ACL exactness, carry the v1 hard core forward unregressed)

author: holder-author-001

> This is the **v2 revision** of the RFC 0168 P0 `falsification_gate` proposal. The
> base is the v1 spec
> (`docs/operator/artifacts/rfc-0168-design/dialogue/holder/HOLDER.md`); this is a
> revision of it, **not a rewrite**. v1 **proved the structural hard core** (a per-lane
> uid dissolves `BC1-W1-ORACLE` on this host under Yama `ptrace_scope=1`) and resolved
> OQ1/OQ3/OQ5/OQ6 — both falsifiers credited those and the adjudicator independently
> re-verified them. The v1 cycle-1 adjudicator ledger
> (`docs/operator/artifacts/rfc-0168-design/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md`)
> returned `needs_revision` on **two** source-anchored, verdict-driving gate holes — **C1**
> (OQ2 lease lifecycle cannot represent a returned-but-dirty uid) and **C2** (OQ4 recursive
> group ACL over-grants `.striatum/` control-plane). This revision **discharges C1 and C2**
> with build-bearing, source-anchored mechanisms + named tests, and **carries forward,
> unregressed, everything v1 cleared.** The direction (per-lane pooled OS uid) is ratified
> (D261, 2026-06-24) and is **not** relitigated. Every source citation below was
> re-verified against the current worktree HEAD while authoring this revision (see §Source
> re-verification). This is the published claim the two falsifiers re-attack.

---

# Addressing the v1 constraints (the auditable revision map)

| v1 verdict ground | This revision | Where |
| --- | --- | --- |
| **C1 / `OQ2-LEASE-LIFECYCLE`** — schema is `state active|returned`; prose relies on an unrepresented `quarantined`; free predicate = "no active row"; "mark returned after scrub 1–3" rests on `kill … == 0`, which does not prove an empty kill domain → a scrub failure either re-leases a dirty uid or strands a zombie-active lease (no recovery, no doctor, false exhaustion). | **RESOLVED** — a durable **4-state** machine `active / scrubbing / quarantined / returned`; the free predicate is *"no row in `active|scrubbing|quarantined`"* enforced by a partial **unique held-index**; an **allocate / scrub-begin / scrub-finalize transaction boundary** with the side-effecting scrub strictly **between** transactions (crash-safe); a **scrub postcondition PROOF** by `/proc` + socket + `stat` observation (P1–P5), not exit codes; a **reaper** for leaked-active and stuck-scrubbing rows; a **doctor surface**; and **exhaustion accounting that excludes `scrubbing`+`quarantined`** from the free set. | **§OQ2 (rewritten)**, tests **A8′/A9′/A10′/A11′ + A17–A20** |
| **C2 / `OQ4-ACL-SCRATCH-OVERGRANT`** — `setfacl -R -m g:striatum-lanes:rX <repo>` (+ default) recurses through `.striatum/`, granting **every** pool uid (incl. unleased) group read on another lane's `0600` MCP bearer (`mcpconfig.go`), PTY logs (`loop.go`), token cache, and foreign worktrees; `-R …:rX` adds group `r` even to existing `0600` files. | **RESOLVED** — a **hard boundary at `.striatum/`**: the group `rX` grant covers **shared source/artifact only and is stripped from `.striatum/` and `.git/`** (auditable end-state: *no `g:striatum-lanes` access-or-default entry anywhere under `.striatum/`*); `.striatum/` reachability is **per-leased-uid only** (`--x` traverse, re-keyed from `scratch_acl.go`), the per-supervisor scratch dir is `rwx` to the **leased** uid alone, the per-job worktree is **chowned** to the leasing uid, and **all per-lease ACLs are removed on scrub** (ties to OQ2 proof P5). | **§OQ4 (rewritten)**, test **A16** + extended `make lane-isolation-check`/`doctor` |
| **Carry-forwards (v1 PROVEN — do NOT reopen / regress)** | **INTACT, restated, re-verified** | below |
| Structural hard core **HC-A1..A5** (per-uid tmux socket; cross-uid `0600` DAC; cross-uid `ptrace`/`setns`//proc denial — the exact axis namespace-inode failed; `SO_PEERCRED` uid discriminator; every residual same-uid surface closed) | carried **unregressed**; re-verified against `pty.go`/`tmux_liveness.go` HEAD | **§Part 1 (carried)** |
| **OQ1** host-global pool ceiling + typed fail-closed `lane_uid_pool_exhausted` refuse-and-requeue | carried; its only v1 caveat (exhaustion reads the quarantine state) is now **closed** by C1 | **§OQ1 (carried)** |
| **OQ3** static host-runbook pool; daemon holds only launch-as `(%striatum-lanes)`, **no** `useradd`/`userdel` | carried unregressed; the new scrub uses only the launch-as grant the daemon already holds | **§OQ3 (carried)** |
| **OQ5** leased-uid + monotonic per-uid **generation** token as the anti-recycle primitive | carried unregressed; the generation is minted in the C1 allocate transaction | **§OQ5 (carried)** |
| **OQ6** per-spawn per-uid hydration into the leased uid's `0600` store, scrubbed on return | carried; its v1 contingency (a failed scrub leaves a store) is now **closed** by C1 proof P3 | **§OQ6 (carried)** |
| **The NARROWING invariant** — no admin-token widening, no lane-readable shared reseal bearer, no new authority | carried unregressed; both C1 and C2 only **remove** surface | throughout |

The two rewrites (OQ2, OQ4) below are the new load-bearing claims. The carried sections
restate the v1 proof faithfully (the falsifiers have v1 as required context) and flag that
nothing in them changed; a regression in any carried claim is itself a gate failure.

---

# Part 1 — THE HARD CORE (CARRIED FORWARD UNREGRESSED from v1 §Part 1; re-verified, not reopened)

The whole RFC leans on one assertion: **a per-lane uid dissolves `BC1-W1-ORACLE` on this
host.** v1 proved it as four structural sub-claims + a residual-surface closure; both
falsifiers credited it and the adjudicator independently re-verified the launch path. It is
carried here **unchanged**. The exact attack: target lane runs as `U_t`, sibling as
`U_s ≠ U_t`; the launch path is `sudo -n -u <runAsUser> -- env -i … tmux …`
(`commandInvocationWithEnvFile`, `pty.go:98-112`), tmux invoked **bare** through the same
run-as path (`tmuxRunnerForSpec`→`RunAsTmuxRunner`, `pty.go:310-314`;
`tmux_liveness.go:125-149`) with a deterministic session name (`pty.go:620-633`) and **no**
`-S`/`TMUX_TMPDIR` (`sanitizedRunAsEnv`, `pty.go:120-155`).

- **HC-A1** — each lane's bare tmux lands on tmux's **default per-uid socket**
  `$TMUX_TMPDIR/tmux-<uid>/default` (`/tmp/tmux-<uid>`, dir `0700` owned by that uid,
  socket `srwx------`). `U_s` has no `--x` on `/tmp/tmux-<U_t>` ⟹ `connect(2)` fails
  `EACCES` before any tmux command parses; the same-uid-mutable oracle is gone. **Refuter
  / test A1:** `TestSiblingPoolUIDCannotRespawnTargetPane` (real-path) — `U_s`'s
  `respawn-pane -k` against `U_t`'s pane must fail and the pane pid must be unchanged.
- **HC-A2** — a `U_t`-owned `0600` file in a `0700` HOME is unreadable by `U_s` under POSIX
  DAC (the RFC 0143 Slice B reseal-token surface). **Test A2:**
  `TestSiblingPoolUIDCannotReadLaneOwnedResealToken`.
- **HC-A3** — cross-uid `ptrace`/`setns`/`open("/proc/<U_t>/…")` are denied by
  `ptrace_may_access` (matching-uid or `CAP_SYS_PTRACE` required), **independent of Yama**.
  This is the exact axis on which **namespace-inode binding failed** (D261 — a shared-uid
  sibling passes `ptrace_may_access` and `setns`s in; a different uid cannot). **Test A3:**
  `TestSiblingPoolUIDCannotPtraceOrSetnsOrReadProcSecrets` (asserts `ptrace_scope=1`
  precondition).
- **HC-A4** — the daemon control socket reads kernel-attested `SO_PEERCRED {pid,uid}`; with
  a per-lane uid the accept predicate gains a **meaningful uid discriminator**
  (`peer.uid == U_t`), so a `U_s` peer is rejected before any pid/oracle reasoning. **Test
  A4:** `TestControlFrameAcceptsOnlyLeasedLaneUID`.
- **HC-A5** — every residual same-uid surface is named and closed: the only common ancestor
  of two lanes is the trusted **daemon** (not a lane); the shared tmux server is now per-uid
  (HC-A1); world/group-readable paths are closed on every **private** surface
  (`0600`/`0700`, no group ACL — **see the revised OQ4 for the exact `.striatum/`
  boundary**); daemon-bridging is lane-vs-daemon, not lane-to-lane. **Test A5:**
  `TestNoSharedSameUIDSurfaceBetweenPoolLanes`.

**Hard-core conclusion (carried):** the BC1-W1-ORACLE mechanism requires `U_s` to address
`U_t`'s tmux server (A1: denied), `ptrace`/`setns` into `U_t` (A3: denied), or read a
`U_t`-owned file (A2: denied) — all structurally closed by the different uid (A5: no residual
bridge). The daemon no longer infers identity from a mutable oracle; the kernel **attests**
the connecting uid (A4). This is a **narrowing**. *Nothing in this revision changes Part 1;
the full per-claim proof and source citations are in v1 §Part 1.*

---

# Part 2 — THE SIX OPEN QUESTIONS

## OQ1 — Pool size + exhaustion (CARRIED, with the v1 caveat now CLOSED)

**Carried unregressed.** There is no host-global concurrent-lane ceiling today
(`max_active_jobs` is per-workflow, default `0`=unlimited;
`runreconcile_test.go:395`), so a finite uid pool **introduces the first host-global
ceiling** — a deliberate, named consequence. Sizing: `N` = the host's max concurrent live
**sessions with an attached supervisor across all runs** (a lane holds a uid for its session
lifetime, OQ2), operator-chosen, runbook-documented (`N ≥ Σ_runs(expected concurrent
distinct-lane supervisors)`). Exhaustion policy = **REFUSE, fail-closed, typed**: when a
launch needs a uid and none is free, the daemon refuses with a typed
`lane_uid_pool_exhausted` floor and leaves the job **queued/recoverable**; it never falls
back to the shared `striatum-lane` uid, never blocks-and-waits holding a lock, never
auto-`useradd`s. Tests **A6** (`TestLaneUIDPoolCeilingIsHostGlobalAcrossRuns`) and **A7**
(`TestLaneUIDPoolExhaustionRefusesTyped`) carry forward.

**The v1 caveat, now closed by C1.** The adjudicator credited OQ1 but flagged one residue:
*exhaustion accounting must read the OQ2 quarantine state, which v1 left unspecified.* The
revised OQ2 below defines the free predicate as *"no row in `active|scrubbing|quarantined`"*,
so the free-set size the exhaustion check reads is
`free = N − count(active) − count(scrubbing) − count(quarantined)` — a `quarantined` uid is
counted **consumed**, the ceiling is honest, and a new negative test (**A20**) asserts
exhaustion fires at the reduced ceiling rather than re-leasing a dirty uid. The caveat is
discharged.

## OQ2 — Lease/scrub/reaper lifecycle (REWRITTEN to discharge C1)

**The C1 hole (restated):** v1's `lane_uid_leases` schema was `state active|returned`; the
prose relied on an unrepresented `quarantined`; the free predicate was *"no active lease
row"*; and *"mark returned after scrub steps 1–3 succeed"* rested on command exit codes —
`sudo -n -u <pool_uid> -- kill -KILL -1` returning `0` does **not** prove an empty per-uid
kill domain (a process can be in `D`/`Z`, survive, reappear, or recreate HOME files after the
command returns). A scrub failure forced a state the table could not express → either
`returned`+`scrub_status=failed` re-leases the **dirty** uid (same-uid cross-lease residue
leak — the exact class RFC 0168 must eliminate), or `active` strands a **zombie** lease (no
recovery, no doctor, false exhaustion). The fix is an executable state machine that makes a
dirty/leaked uid **impossible to re-lease**.

### OQ2.1 — A durable FOUR-state lease, with the free set excluding every non-clean state

P0 adds the daemon-owned table **`striatumd.lane_uid_leases`** (a new owner-bundle version,
the same additive mechanism that added `jobs.recovery_generation` /
`leases.reseal_grace_extended_at`). It lives in daemon-owned PostgreSQL, so it **survives a
`striatumd` restart** (D094 / RFC 0043). The schema (the v1 fields **plus** the C1 states and
scrub-proof columns):

```sql
CREATE TABLE striatumd.lane_uid_leases (
  lease_id       text PRIMARY KEY,         -- host-global (the pool is a HOST resource, OQ1)
  pool_uid       integer NOT NULL,         -- the leased OS uid
  generation     bigint  NOT NULL,         -- monotonic per pool_uid (OQ5 anti-recycle token)
  repository_id  text    NOT NULL,         -- which repo this lease currently serves (ACL scope, OQ4)
  session_id     text    NOT NULL,
  supervisor_id  text    NOT NULL,
  state          text    NOT NULL
                 CHECK (state IN ('active','scrubbing','quarantined','returned')),
  scrub_status   text    CHECK (scrub_status IN ('clean','failed')), -- null until finalize
  scrub_proof    jsonb,                    -- P1..P5 observations recorded at finalize
  scrub_failure  text,                     -- which proof failed + detail (quarantined rows)
  leased_at      timestamptz NOT NULL,
  scrub_started_at timestamptz,
  returned_at    timestamptz
);

-- A uid HELD (in any non-clean state) is exclusive HOST-WIDE: the structural guard
-- that no second session can be allocated a uid that is active, mid-scrub, or dirty.
CREATE UNIQUE INDEX uq_lane_uid_held
  ON striatumd.lane_uid_leases(pool_uid)
  WHERE state IN ('active','scrubbing','quarantined');
```

**Free predicate (the C1 fix).** A pool uid is allocatable iff it has **no** row in
`active|scrubbing|quarantined` (i.e. its latest row is `returned`, or it has never been
leased) — **never** v1's "no active row". The free set is **derived from the table**, never
held in memory (restart-survival, below). `quarantined` and `scrubbing` are first-class
non-free states; the partial unique index makes any of the three mutually exclusive per uid,
so two transactions cannot both hold one uid.

### OQ2.2 — The allocate / scrub-begin / scrub-finalize TRANSACTION boundary (crash-safe)

The side-effecting scrub (external `sudo … kill/rm` + `/proc` probes) **cannot** run inside a
DB transaction (it blocks; holding a tx/advisory lock during it would stall the daemon — the
same `#198` reasoning that moved liveness probes **out** of the sweep tx,
`recovery.go:565-587`). So the lifecycle is **three transactions** with the scrub strictly
**between** them:

1. **Allocate** (`tx_alloc`, at `supervise.start` before token-mint+launch): insert
   `{state='active', generation = (max generation for pool_uid)+1, …}` selecting a uid whose
   latest state is `returned`/absent. The `uq_lane_uid_held` index makes a concurrent
   double-allocate of the same uid fail atomically (serialization). Launch as
   `RunAsUser = pool_uid`. *No scrub here.*
2. **Scrub-begin** (`tx_scrub_begin`, guarded `state='active'→'scrubbing'`,
   `scrub_started_at=now`): runs on `session.close` (hooked into `stopSupervisorInTx`,
   `supervision_control.go:557-637`, immediately after the existing tmux-kill /
   `terminateProcessWithStartToken` and the conditional session-close at `:603-612`) **or**
   when the reaper claims a dead/leaked lease. This **atomically removes the uid from the
   free set BEFORE any scrub command runs** — closing the v1 race where allocation could pick
   a uid mid-scrub.
3. **Scrub + postcondition proof** (OUT of any DB tx): the steps + proof in OQ2.3.
4. **Scrub-finalize** (`tx_scrub_finalize`, guarded `state='scrubbing'`): if the proof
   passed, `→'returned'`, `scrub_status='clean'`, `scrub_proof=<observations>`,
   `returned_at=now`. If the proof failed, `→'quarantined'`, `scrub_status='failed'`,
   `scrub_failure=<detail>`, and emit the typed `lane_uid_scrub_failed` floor.

**Crash-safety.** A crash between (2) and (4) leaves the row durably `scrubbing` in
PostgreSQL — **not free** (free predicate excludes it), **not re-leasable** (unique held
index). The recovery sweep re-drives it (OQ2.4). So a crash never strands a dirty uid as
free and never strands a `scrubbing` row forever.

### OQ2.3 — The scrub steps AND the postcondition PROOF (observation, not exit codes)

**Scrub commands** (all `sudo -n -u <pool_uid> --`, within the launch-as grant the daemon
already holds — OQ3; the live teardown at `supervision_control.go:557-637` today does **none**
of these beyond `CleanupGeminiSettings`/`CleanupClaudeScheduledTasksLock`, so P0 adds this as
a new scrub primitive):

- **S1 — per-uid kill domain:** `kill -KILL -1` (every process owned by `pool_uid`), reaping
  the uid-owned tmux **server** (HC-A1) and any stray/daemonized lane processes. Safe only
  because the uid is private to this lease.
- **S2 — credential store:** delete `~<pool_uid>/.claude/.credentials.json` and the resolved
  `CLAUDE_CONFIG_DIR` store (OQ6).
- **S3 — HOME scratch + per-lease ACLs:** remove the lane's writable HOME contents (provider
  caches/config) and remove the per-leased-uid `.striatum/` ACL grants (OQ4 — `.striatum`
  `--x`, `.striatum/scratch` `--x`, `.striatum/scratch/<supervisor_id>` `rwx`) and release/
  chown-back the per-job worktree.

**The POSTCONDITION proof (the heart of C1 — NOT `exit==0`).** After S1–S3 the daemon
**proves** the uid domain is empty by *observation as the daemon/owner*, with a bounded retry
(re-`kill`+re-probe, M attempts, short backoff) before declaring failure:

- **P1 — empty kill domain:** enumerate `pool_uid`-owned PIDs by reading `/proc/<pid>/status`
  (`Uid:` real+effective fields — the same `/proc` mechanism the PID start-token probe uses,
  `tmux_liveness.go:392-408`) and assert **no** `pool_uid`-owned process in a code-runnable
  state (`R`/`S`/`D`) remains. A `Z` zombie (which `kill -KILL -1`==0 does **not** reap) is
  recorded but does not block — it holds no resources and cannot run code; the proof
  **distinguishes `Z` from `R/S/D`**, which is exactly the gap `exit==0` could not see.
- **P2 — no uid-owned tmux server:** assert `connect(2)` to `/tmp/tmux-<pool_uid>/default`
  fails `ECONNREFUSED`/`ENOENT` (server gone, not merely signaled).
- **P3 — credential store absent:** `stat(2)` the resolved per-uid credential path(s) ⟹
  `ENOENT`.
- **P4 — HOME scratch reset + reseal-token absent:** assert the per-uid writable HOME scratch
  is gone and the reseal-token path is absent.
- **P5 — per-lease ACLs removed:** assert no `.striatum/scratch/<supervisor_id>` ACL entry for
  `pool_uid` remains (ties to OQ4).

`returned` is reached **only** when P1–P5 all hold (recorded in `scrub_proof`). **Any** failed
proof ⟹ `quarantined`. This is the source-anchored difference from v1: the negative path is
**defined and proven**, not asserted positively.

### OQ2.4 — The reaper (leaked-active + stuck-scrubbing) and quarantine remediation

The recovery sweep is the reaper host (`HandleRecoveryAuto`, `recovery.go:553`, every 60s,
`main.go:81`; it already expires leases — `expireLeases`, `recovery.go:2516` — and reaps
idle orphans — `reapIdleOrphanSessions`, `recovery_decision_tree.go:1474` — using the
liveness oracle `buildRunLivenessOracle`, `recovery_liveness_oracle.go:117`). P0 extends it:

- **Leaked-active reaper:** an `active` `lane_uid_lease` whose owning session is dead (the
  oracle reports the lane gone) and that never ran `session.close` (daemon died mid-lease) is
  transitioned `active→scrubbing` (tx_scrub_begin) and driven through scrub+proof
  (→`returned`/`quarantined`). **No uid leaks active** — every path that ends a session also
  ends its uid lease.
- **Stuck-scrubbing reaper:** a `scrubbing` row (crash between begin and finalize) is
  re-driven **idempotently** each sweep (re-kill an already-dead domain, re-rm absent files,
  re-prove) until it finalizes. A `scrubbing` row that outlives `M` sweeps surfaces a typed
  `lane_uid_lease_stuck_scrubbing` doctor finding (a scrub that never converges is a real
  defect to surface, not route around).
- **Quarantine remediation:** a `quarantined` row is **never** auto-returned. It stays out of
  the free set across restarts (a durable row; the free predicate excludes it). It clears only
  via an explicit operator/recovery retry (a `recovery` verb / MCP recovery method) that
  re-runs the **same** scrub+proof and transitions `quarantined→returned` **only on a clean
  proof** — the single `quarantined→returned` edge, never a blind clear.

### OQ2.5 — Restart-survival (CARRIED, extended for the quarantine sub-case)

The boot epoch is fresh-per-process and not persisted (`randomBootEpoch`,
`main.go:722/731`); `daemonInstanceID` is restart-stable (`main.go:665-690`). On restart **no
in-memory binding survives** — but the `lane_uid_leases` rows do (PostgreSQL). The daemon
**derives** pool state from the table: a live `active` lease whose lane is still alive (the
sweep oracle) keeps its uid (the OQ5 generation re-binds attestation); a dead `active` is
reaped; a `scrubbing` row is re-driven; **a `quarantined` row stays `quarantined`** (non-free
across restart — the sub-case the adjudicator flagged as riding on OQ2, now explicit). The
free set is derived, never memory-held, so a restart cannot double-lease or strand a uid.

### OQ2 falsifiable assertions (A8′/A9′/A10′/A11′ revised + A17–A20 new negatives)

- **A8′** `TestUIDLeaseBindsSessionAndPersistsAcrossRestart` — a leased uid is recorded;
  after a simulated restart (fresh boot epoch, reload from DB) the live lane keeps the **same**
  uid. **Refuter:** binding lost or a second uid leased to the same live session.
- **A9′** `TestUIDReturnScrubsAndProvesEmptyDomain` — return a uid whose lease left a stray
  daemonized process, a credential file, and HOME scratch; assert S1–S3 ran **and** P1–P5 are
  observed clean (`/proc` shows no `R/S/D` `pool_uid` process; tmux socket refused; cred
  absent; HOME reset), and the row reaches `returned` only with `scrub_status='clean'` +
  recorded `scrub_proof`. **Refuter:** the row returns with any P-observation unmet, or
  returns on `exit==0` alone.
- **A10′** `TestLeakedActiveUIDReapedToScrubbingThenProven` — kill the daemon mid-lease (no
  `session.close`); the next sweep with the lane dead transitions `active→scrubbing` and
  finalizes (`returned`/`quarantined`), never left leaked-active. **Refuter:** a uid stays
  `active` with no live session.
- **A11′** `TestUIDLeaseReconstructedAfterBootEpochRotation` — rotate the boot epoch mid-work;
  the uid↔session binding rebuilds from the DB and the lane still owns its `0600` reseal token
  as the same uid. **Refuter:** binding cannot be reconstructed without an in-memory value.
- **A17** `TestScrubFailureQuarantinesAndIsNeverReLeased` (the C1 negative path) — inject a
  **surviving** `pool_uid` process (so P1 fails) **or** a deletion failure (P3/P4 fails);
  assert the uid → `quarantined`, a typed `lane_uid_scrub_failed` is emitted, and a subsequent
  allocation **does not** pick it (excluded from the free set) — **no dirty re-lease, no
  shared-uid fallback**. **Refuter:** the dirty uid is allocated to a later session.
- **A18** `TestCrashDuringScrubLeavesUIDHeldNotFree` — crash between `tx_scrub_begin` and
  `tx_scrub_finalize`; assert the row is durably `scrubbing`, excluded from the free set, and
  the next sweep re-drives it to a terminal state. **Refuter:** the uid is allocatable while
  `scrubbing`, or the `scrubbing` row is never re-driven.
- **A19** `TestQuarantineSurvivesRestartAndIsNonFree` — quarantine a uid, rotate the boot
  epoch; assert it is still `quarantined` and not allocatable; an operator retry against a
  now-clean domain transitions `quarantined→returned` **only after** the P1–P5 proof.
  **Refuter:** quarantine is lost on restart, or clears without the proof.
- **A20** `TestExhaustionExcludesScrubbingAndQuarantined` — with pool `N`, one `quarantined`
  + one `scrubbing`, assert `lane_uid_pool_exhausted` fires at `N−2` (not `N`) and never
  re-leases a dirty uid or falls back to `striatum-lane`. **Refuter:** a launch succeeds on a
  `quarantined`/`scrubbing` uid.

## OQ3 — Provisioning ownership (CARRIED UNREGRESSED)

Host-setup runbook artifact (static pool); the daemon gets **no** uid-lifecycle authority.
The pool is pre-created by the operator runbook (extending `docs/how-to/lane-sandbox.md`),
exactly as the single lane user is today. The daemon **leases** from a static set; it never
`useradd`/`userdel`s. The sudoers grant widens from one user to a runas **group**
(`striatumd ALL=(%striatum-lanes) NOPASSWD: ALL`, still **launch-only**;
`lane-sandbox.md:94`). The daemon's only new exercise of that grant is the **scrub** (`sudo
-n -u <pool_uid> -- kill/rm` — within the launch-as authority it already holds, no new
privilege class). This keeps RFC 0168 a **narrowing** (`lane-sandbox.md` non-goal "not new
daemon authority"). **Test A12** (`TestDaemonHoldsNoUIDLifecycleAuthority`) carries forward:
the pool sudoers grant is runas-only with no `useradd`/`usermod`/`visudo`, and no daemon code
path calls `useradd`/`userdel`; `doctor` reports `daemon_creates_uids: false`.

## OQ4 — ACL interaction (REWRITTEN to discharge C2)

**The C2 hole (restated):** v1's `setfacl -R -m g:striatum-lanes:rX <repo>` (+ default group
ACL) over the repo **root** recurses through `.striatum/` — daemon/operator-private
control-plane: the `0600` MCP bearer at `.striatum/scratch/<supervisor_id>/`
(`mcpconfig.go:241,266`), PTY logs (`loop.go:145,300`), the token cache, and foreign per-job
worktrees. That grants **every** pool uid (including one **not leased to this repo**) group
read on another lane's session-bound bearer, and `setfacl -R …:rX` adds group `r` even to
existing `0600` files (so "mode is `0600`" is not a rebuttal). It breaks the repo's own
`.striatum/` carve-out (`scratch_acl.go:42-49`: `.striatum`→`u:<lane>:--x` traverse-only,
under the explicit "never broaden read access to private operator state" invariant;
`lane-sandbox.md:348-355`: `.striatum/` is daemon-private, the *only* lane exception is the
per-job worktree). The fix is a **hard ACL boundary at `.striatum/`**.

### OQ4.1 — Two ACL domains with a hard boundary at `.striatum/`

**(a) Shared source/artifact tree → group `rX`, EXCLUDING control-plane.** The pool group
`striatum-lanes` (OQ3) gets recursive access + default read/traverse on the repo's **shared
work product only**, never on `.striatum/` or `.git/`. This preserves v1's virtue (one grant
covers every pool uid and every future source file — no N× per-uid churn) while removing the
hole. Build-bearing mechanism (a minimal change to `repo_acl.go:97-140`, whose current
single-lane helper applies `setfacl -R -m u:<lane>:rwx` over `repoRoot` **and** the worktrees
root — itself a latent recursion into `.striatum/` that the pool/group version must **not**
replicate):

- Apply `setfacl -R -m g:striatum-lanes:rX -m d:g:striatum-lanes:rX <repoRoot>`, then a
  **mandatory carve-out step** that strips the group entry + default from the control-plane
  subtrees: `setfacl -R -x g:striatum-lanes -k <repoRoot>/.striatum` and `… <repoRoot>/.git`.
  (`-x g:striatum-lanes` removes the named-group **access** entry from existing files —
  including ones the first `-R` had added group `r` to; `-k` removes the **default** ACL so
  new files do not inherit it.)
- **Equivalent allowlist form** (the falsifier's stated preference) is permitted: apply the
  group grant only to an allowlist of source/artifact top-level entries (everything except
  `.striatum`, `.git`). The spec gates on the **auditable end-state**, not the procedure:

  > **OQ4 invariant (the load-bearing assertion):** *No path under `<repoRoot>/.striatum/`
  > (nor `<repoRoot>/.git/`) carries a `g:striatum-lanes` access **or** default ACL entry,
  > before or after provisioning, for existing or future files.*

  A non-leased pool uid that is a group member can read/traverse shared **source** — this is
  **not** a new exposure: the lane principal class already reads shared work product today (the
  single shared uid does), and the security boundary that matters (control-plane + private
  secrets) is **never** group-granted. This is the v1-credited reasoning, now confined to the
  source tree by the invariant above.

**(b) Control-plane / private / worktree → per LEASED uid only, removed on scrub.** No group
ACL touches any of these; every grant is keyed to the **currently leased** uid and applied at
lease/launch, removed at scrub (OQ2 S3 / proof P5):

- `.striatum/` → `u:<leased-uid>:--x` (traverse only) — re-keys `scratch_acl.go:46` from a
  fixed `laneUser` to the **leased** uid. An **unleased** pool uid gets **no entry** ⟹ cannot
  even traverse `.striatum/`.
- `.striatum/scratch/` → `u:<leased-uid>:--x` (traverse only — **not** `rwx`; pushed down from
  v1/current `rwx`-on-`scratch` at `scratch_acl.go:47`) so a leased uid cannot list or read
  **sibling** supervisors' scratch dirs.
- `.striatum/scratch/<supervisor_id>/` → `u:<leased-uid>:rwx` + default ACL — the lane's
  **own** ephemeral MCP config dir (where `mcpconfig.go` writes the `0600` bearer and
  `loop.go` writes `pty.log`). Per-supervisor, so another leased uid has no entry on it.
- `.striatum/worktrees/<id>/` → **chowned to the leased uid** at worktree creation (the daemon
  owns the worktree lifecycle and knows the lease's uid). Only the leasing uid reads/writes
  its worktree; **no group ACL, no group default** on the worktrees root (replacing the
  current `repo_acl.go:130-138` default-ACL-on-worktrees-root grant for the pool case). This
  is the *only* lane exception under `.striatum/`, matching `lane-sandbox.md:352-355`.
- **PG isolation** covers every pool uid via a group reject rule
  (`local all %striatum-lanes reject` + the loopback forms, the pool analogue of
  `lane-sandbox.md:77-79`).

### OQ4.2 — Why the exact failing case is closed

The unleased-uid-reads-another-lane's-bearer case (CHALLENGE 2): an **unleased** pool uid
`U_x` has **no** ACL entry on `.striatum/` (only the *currently leased* uid does, and only
`--x`), so it cannot traverse into `.striatum/scratch/<S1>/` — `open(2)` of S1's
`lane-mcp-config-*.json` bearer fails `EACCES` at the `.striatum` traverse. A **different
leased** uid `U_2` (which holds `.striatum`:`--x` + `.striatum/scratch`:`--x`) still cannot
read S1's bearer: `.striatum/scratch` is `--x` (traverse-only, no `r`/list), and
`.striatum/scratch/<S1>/` carries an ACL entry only for S1's leased uid ⟹ `EACCES` on the
subdir. Because no group ACL ever touches `.striatum/`, the `-R …:rX`-adds-group-`r`-to-`0600`
problem cannot arise. The cross-lane control-plane replay surface is removed.

### OQ4 falsifiable assertions (A13 carried + A16 new non-exposure test)

- **A13** (carried) `TestPoolACLGrantsSharedReadNotPrivateOrCrossWrite` — every pool uid can
  traverse+read shared **source** via the group ACL; a pool uid not leased to a worktree
  cannot write it (worktree owned by the leasing uid); no pool uid reads another lane's `0600`
  reseal token / HOME (HC-A2); `make lane-isolation-check` shows every pool uid denied
  PostgreSQL over the UNIX socket **and** loopback TCP.
- **A16** (new — the C2 control) `TestPoolACLDoesNotExposeOperationalScratchOrForeignWorktrees`
  — provision the pool ACL; seed lane S1's `0600` MCP bearer
  (`.striatum/scratch/<S1>/lane-mcp-config-*.json`), its `pty.log`, the token cache, and a
  foreign per-job worktree owned by S1's leased uid. Then, as **(i)** an **unleased** pool uid
  `U_x` and **(ii)** a **different leased** uid `U_2`, assert: `open(2)` of the bearer →
  `EACCES`; listing `.striatum/scratch/` → `EACCES`; reading `pty.log` → `EACCES`; reading any
  file in S1's foreign worktree → `EACCES`; **and** assert the OQ4 invariant directly — no path
  under `.striatum/` carries a `g:striatum-lanes` access-or-default entry. Run over **both**
  seeded-existing scratch and **future** scratch (create a new bearer after provisioning and
  re-assert unreadable). Extend `make lane-isolation-check` / `doctor` to **fail** when any
  `.striatum/` (or `.git/`/provider-auth/token-cache) path carries `striatum-lanes` group read,
  or when a `returned`/`quarantined` uid still holds a `.striatum/scratch/<supervisor_id>` ACL
  (scrub didn't remove it — ties to OQ2 P5). **Refuter:** any read succeeds for `U_x`/`U_2`, or
  any `.striatum/` path carries the group entry.

## OQ5 — Attestation + recycle-confusion generation token (CARRIED UNREGRESSED)

Attestation records the **leased uid** (from the `lane_uid_leases` row) so it answers *"is
this the lane we leased `U_t` to?"* — the PID start-token (`ProcessStartToken`, `/proc` field
22, `tmux_liveness.go:392-408`) discriminates the **process**, the leased uid the
**principal**. The `lane_uid_leases.generation` (monotonic per uid, minted in `tx_alloc`,
OQ2.2) is folded into the lane's capability material exactly as `MCPBootEpoch` is
(`mutations.go:41-48`); the daemon **refuses to attest, and refuses a control frame, when the
presented generation ≠ the live generation for that uid** — closing the cross-lease window a
process surviving the scrub would otherwise have. (The condition the adjudicator attached:
**every** attestation **and** control-frame path compares the live generation — held.) **Test
A14** (`TestRecycledUIDGenerationPreventsCrossLeaseConfusion`) carries forward.

## OQ6 — Per-uid credential store (CARRIED, contingency now CLOSED by C1)

The RFC 0165 spawn-time hydrator (#583) targets the **leased** uid's HOME via temp-file+rename
(verifying destination owner==leased-uid, mode `0600`, parseable OAuth, expiry lead, source
generation stability). Hydration is **per-spawn** (fresh from the operator source each launch)
and the **scrub deletes** the per-uid store on return (OQ2 S2, proven absent by P3) — so a
uid's store exists only for the lease's lifetime: fresh in, deleted out, never N stale copies;
`0600` owned by the leased uid inside its `0700` HOME, unreadable by any other pool uid
(HC-A2). The v1 contingency the adjudicator flagged (*a failed scrub leaves a credential store
for the next lease*) is now **closed**: a failed P3 ⟹ `quarantined`, so the uid is never
re-leased with a surviving store. **Test A15** (`TestPerUIDCredentialHydrateNoStaleNoLeak`)
carries forward, extended to assert that an injected P3 failure quarantines rather than
re-leasing (overlaps A17).

---

# Part 3 — THE P0 SLICE (updated for the discharged OQ2/OQ4)

P0 is the minimum for a lane to run as its own pooled uid and safely own a `0600` reseal
token:

1. **Static pool, host-provisioned** (OQ3): N pool uids, `striatum-lanes` group, widened
   runas-group sudoers, per-uid PG reject, **and the revised OQ4 ACL** (group `rX` on shared
   source **with the `.striatum/`/`.git/` carve-out**; per-leased-uid `.striatum/` traverse +
   per-supervisor scratch + chowned worktree); daemon holds no uid-lifecycle authority.
2. **Daemon-owned `lane_uid_leases` table** (OQ2): the **four-state** machine
   (`active/scrubbing/quarantined/returned`), the partial **held-unique** index, the
   generation token, **persisted** (restart-survival).
3. **Allocation + host-global admission ceiling** (OQ1): lease a free uid (free = no
   `active|scrubbing|quarantined` row) at `supervise.start`; refuse `lane_uid_pool_exhausted`
   (typed, queued, no shared-uid fallback) when none is free.
4. **Return + scrub + PROOF + reaper** (OQ2): the allocate/scrub-begin/scrub-finalize
   transaction boundary; S1–S3 scrub + P1–P5 postcondition proof on `session.close`; the
   recovery sweep reaps leaked-active and re-drives stuck-scrubbing; quarantine-on-failed-proof
   with a doctor surface and operator retry.
5. **Attestation binds uid + generation** (OQ5): the recycle-confusion token.
6. **Per-uid credential hydration** (OQ6): RFC 0165 hydrator targets the leased uid; scrub
   deletes on return (proven by P3).

With (1)–(6), RFC 0143 Slice B reduces to *"write a session-scoped reseal token owned by the
leased uid, `0600`"* — safe by HC-A2.

**Seams deferred to later slices (named, not dropped):** daemon-managed dynamic uid
create/destroy (OQ3 blast radius); automated pool autogrow/resizing (P0 is fixed `N`); the
reseal-token write itself (RFC 0143 Slice B); cross-host/multi-host pooling; non-tmux adapter
parity for the per-uid kill domain (P0 covers the tmux + pipe-backed lanes the supervisor
launches today; a future adapter inherits the same `lane_uid_leases` + scrub contract).

**Local-first boundary preserved.** One host, one PostgreSQL (the single writer, D094 / RFC
0043), one daemon; every scrub/kill/probe is local `sudo -n -u <pool_uid>` within the
launch-as grant; the pool is OS users; no hosted service, cloud API, telemetry, or external
persistence. The change is a **narrowing** — both C1 (more durable state, a proof, a reaper)
and C2 (a tighter ACL) only **remove** surface; no new authority is granted.

---

# Source re-verification (every load-bearing site CONFIRMED against current worktree HEAD; new C1/C2 anchors added)

| Claim | Site | Status |
| --- | --- | --- |
| Run-as launch = `sudo -n -u <runAsUser> -- env -i …`; bare tmux; minimal env; deterministic session name | `pty.go:98-112,:120-155,:310-314,:620-633`; `tmux_liveness.go:125-149` | **CONFIRMED** (carried Part 1) |
| `leases` is a **job** lease (`resource_type CHECK IN ('job')`, `state CHECK IN ('active','released','expired')`); `uq_active_resource_lease` partial-unique on active | `0005_repo_local_workflow_state.sql:166-186` | **CONFIRMED** — the new `lane_uid_leases` mirrors this shape + the four states + the held-unique index |
| **Live teardown does tmux-kill / `terminateProcessWithStartToken` + stdin-pipe rm + `CleanupGeminiSettings`/`CleanupClaudeScheduledTasksLock`, and closes the session only when it holds no active lease — NO per-uid kill / cred / HOME scrub** | `supervision_control.go:557-637` (esp. session-close guard `:603-612`, gemini/claude cleanup `:635-636`) | **CONFIRMED** — P0 hooks `tx_scrub_begin` + the scrub here |
| Recovery sweep is the reaper host (60s); expires leases; reaps idle orphans; builds a liveness oracle | `recovery.go:553` (`HandleRecoveryAuto`), `:2516` (`expireLeases`); `recovery_decision_tree.go:1474` (`reapIdleOrphanSessions`); `recovery_liveness_oracle.go:117` (`buildRunLivenessOracle`); `main.go:81` (`-sweep-interval-seconds` default 60) | **CONFIRMED** — P0 extends it with the leaked-active + stuck-scrubbing reaper |
| Probes run OUT of the sweep tx (the #198 reasoning the scrub reuses to keep external commands out of any DB tx) | `recovery.go:565-587` | **CONFIRMED** |
| Boot epoch fresh-per-process, NOT persisted; `daemonInstanceID` restart-stable ⟹ derive the free set from the DB, not memory | `main.go:722,:731` (`randomBootEpoch`); `main.go:665-690` (`daemonInstanceID`) | **CONFIRMED** (restart-survival) |
| PID start-token via `/proc` (the mechanism P1 reuses to enumerate `pool_uid`-owned PIDs and distinguish `Z` from `R/S/D`) | `tmux_liveness.go:392-408` | **CONFIRMED** |
| `MCPBootEpoch` folded into capability material + rejected on mismatch (the model the OQ5 generation token reuses) | `mutations.go:41-48` | **CONFIRMED** |
| **`.striatum` carve-out: `u:<lane>:--x` traverse only, `.striatum/scratch`:`u:<lane>:rwx`+default, under "never broaden read access to private operator state"** | `scratch_acl.go:42-49` | **CONFIRMED** — P0 re-keys to the leased uid + pushes `rwx` down to `.striatum/scratch/<supervisor_id>` |
| **Current repo ACL applies `setfacl -R -m u:<lane>:rwx -m d:u:<lane>:rwx [-m d:u:<owner>:rwx]` over `repoRoot` AND `.striatum/worktrees` (a recursion into `.striatum/` the pool/group version must NOT replicate)** | `repo_acl.go:25-32,:97-140` (specs `:118-124`, targets `:130-138`) | **CONFIRMED** — P0 replaces with the group-on-source + carve-out + per-leased-uid worktree chown |
| **`0600` MCP bearer + PTY log live under `.striatum/scratch/<supervisor_id>/`** (the exposed control-plane) | `mcpconfig.go:241` (`WriteFile … 0o600`), `:266` (`scratch/<supervisorID>`); `loop.go:145` (`pty.log` path), `:300` (`0o600`) | **CONFIRMED** — the exact files A16 plants and asserts unreadable |
| Runbook: `.striatum/` is daemon-private (only lane exception = the per-job worktree); single-lane ACL/worktree/pg-reject/sudoers | `lane-sandbox.md:348-355,:52,:77-79,:94,:256-267` | **CONFIRMED** — P0 widens to the pool/group analogues with the carve-out preserved |
| Per-uid HOME + per-uid Claude credential store; RFC 0165 hydrator contract | `supervision_env.go:205-226`; `laneproviderauth/resolver.go:78-92`; `docs/rfcs/0165-claude-provider-credential-freshness.md` | **CONFIRMED** (OQ6) |
| No host-global concurrent-lane ceiling today (`max_active_jobs` per-workflow, default unlimited) | `runreconcile_test.go:395` | **CONFIRMED** (OQ1 — P0's pool is the first ceiling) |
| D261 ratifies the per-lane-uid direction; rejects namespace-inode/AppArmor-hat/private-socket-alone; blocker #585 | `docs/decisions/decision-log.md` (D261); RFC 0168 | **CONFIRMED** |

---

# Falsifiable-assertion index (the claims the falsifiers re-attack)

| ID | Claim | Refuting test/check |
| --- | --- | --- |
| **A1–A5** | the hard core (per-uid tmux socket; `0600` DAC; cross-uid ptrace/setns//proc denial; `SO_PEERCRED` uid discriminator; no residual same-uid surface) — **CARRIED** | `TestSiblingPoolUIDCannotRespawnTargetPane` / `…ReadLaneOwnedResealToken` / `…PtraceOrSetnsOrReadProcSecrets`; `TestControlFrameAcceptsOnlyLeasedLaneUID`; `TestNoSharedSameUIDSurfaceBetweenPoolLanes` |
| **A6** | pool ceiling host-global across all runs; no double-lease — **CARRIED** | `TestLaneUIDPoolCeilingIsHostGlobalAcrossRuns` |
| **A7** | exhaustion refuses typed, queues, never shares/grows; frees on return — **CARRIED** | `TestLaneUIDPoolExhaustionRefusesTyped` |
| **A8′** | uid binds to the session and persists across restart (reconstructed from PostgreSQL) | `TestUIDLeaseBindsSessionAndPersistsAcrossRestart` |
| **A9′** | return scrubs (S1–S3) **and proves** an empty domain (P1–P5); `returned` only on a clean proof | `TestUIDReturnScrubsAndProvesEmptyDomain` |
| **A10′** | leaked-active uid reaped `active→scrubbing→returned/quarantined` by the sweep | `TestLeakedActiveUIDReapedToScrubbingThenProven` |
| **A11′** | binding reconstructed after a boot-epoch rotation (the RFC 0143 case) | `TestUIDLeaseReconstructedAfterBootEpochRotation` |
| **A12** | daemon holds no uid-lifecycle authority (launch-as only) — **CARRIED** | `TestDaemonHoldsNoUIDLifecycleAuthority` |
| **A13** | group ACL grants shared **source** read only; private secrets + worktree write per-uid; all pool uids PG-denied — **CARRIED** | `TestPoolACLGrantsSharedReadNotPrivateOrCrossWrite` + `make lane-isolation-check` |
| **A14** | recycled-uid generation token refuses a stale-lease actor (every attestation **and** control-frame path) — **CARRIED** | `TestRecycledUIDGenerationPreventsCrossLeaseConfusion` |
| **A15** | per-uid credential hydration leaves no stale copy / no cross-uid leak; a failed P3 quarantines — **CARRIED + extended** | `TestPerUIDCredentialHydrateNoStaleNoLeak` |
| **A16** | **(C2)** the pool ACL exposes no `.striatum/` control-plane (MCP bearer / PTY log / token cache / foreign worktree) to an unleased **or** different-leased pool uid; no `g:striatum-lanes` entry under `.striatum/` (existing + future) | `TestPoolACLDoesNotExposeOperationalScratchOrForeignWorktrees` + extended `make lane-isolation-check`/`doctor` |
| **A17** | **(C1)** a failed scrub postcondition quarantines and the dirty uid is **never** re-leased (no shared-uid fallback) | `TestScrubFailureQuarantinesAndIsNeverReLeased` |
| **A18** | **(C1)** a crash during scrub leaves the uid durably `scrubbing` (held, not free); the sweep re-drives it | `TestCrashDuringScrubLeavesUIDHeldNotFree` |
| **A19** | **(C1)** quarantine survives a restart and is non-free; clears only via the proof-gated `quarantined→returned` retry | `TestQuarantineSurvivesRestartAndIsNonFree` |
| **A20** | **(C1)** exhaustion accounting excludes `scrubbing`+`quarantined`; fires typed at the reduced ceiling, never dirty reuse | `TestExhaustionExcludesScrubbingAndQuarantined` |

**Negative control:** the BC1-W1-ORACLE replay itself (A1) — a sibling pool uid's
`respawn-pane -k` must be refused at the kernel boundary. **C1 control:** A17/A18 (a failed
or crashed scrub provably yields a held/quarantined uid, never a dirty re-lease). **C2
control:** A16 (an unleased/other-leased pool uid provably cannot read another lane's
control-plane bearer). **Restart control:** A8′/A11′/A19 (the binding + quarantine rebuilt
from PostgreSQL after a fresh boot epoch). A clearing verdict requires the hard core proven
(A1–A5, carried), the lease/scrub/reaper **complete with the postcondition proof and a
non-free dirty/quarantined uid** (A8′–A11′, A17–A20), the ACL **exact** (A13, A16), and no
standing falsifier challenge.

---
<sub>Holder leading proposal — RFC 0168 P0 `falsification_gate` design run, **REVISION v2**.
Discharges the two binding cycle-1 constraints: **C1** (OQ2 lease lifecycle) with a durable
four-state machine (`active/scrubbing/quarantined/returned`), a partial held-unique index, an
allocate/scrub-begin/scrub-finalize transaction boundary (scrub strictly between txns,
crash-safe), a scrub **postcondition proof** by `/proc`+socket+`stat` observation (P1–P5, not
exit codes), a leaked-active + stuck-scrubbing reaper, a doctor surface, and exhaustion
accounting excluding `scrubbing`+`quarantined`; and **C2** (OQ4 ACL) with a hard `.striatum/`
boundary — group `rX` on shared source only (carved out of `.striatum/`/`.git/`), per-leased-uid
`.striatum/` traverse + per-supervisor scratch + chowned worktree, all removed on scrub, plus
the `TestPoolACLDoesNotExposeOperationalScratchOrForeignWorktrees` non-exposure test. Carries
the v1-proven hard core (HC-A1..A5) and the credited OQ1/OQ3/OQ5/OQ6 + the narrowing invariant
**unregressed**; the v1 OQ1 exhaustion-accounting caveat and OQ6 stale-store contingency are
both closed by the C1 fix. Local-first boundary intact: one host, one PostgreSQL, one daemon
as single writer; no hosted services. This is the published claim the falsifiers re-attack.</sub>
