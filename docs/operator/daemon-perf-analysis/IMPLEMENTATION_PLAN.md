# striatumd Concurrency — Implementation Plan (priority-sorted)

Status: plan
Date: 2026-06-17
Anchored build: `f2d35c4e` (line numbers are symbol-anchored; offsets drift under
concurrent merges — trust the symbol over the line).
Source findings: `../../audits/STRIATUMD_CONCURRENCY_RESOLUTION_REVIEW_OPUS_4_8_2026-06-17.md`
(ledger §C, inverse check §D, recommendations) over `REPORT.md`.

> Priority = value × safety × unblock-order, **not** elegance or raw impact. Cheap,
> reversible, observability-only changes ship first; the one correctness-sensitive
> structural change (the git-hoist) ships last, gated behind a reproduction and a
> measurement. MISDIAGNOSED/BENIGN findings (F4/F5/F6, R3, D7a) get **no work item** —
> they are disproven, listed under "Not doing" with the disproof.

## What is already fixed / already tracked (don't re-do)

- **#355 (CLOSED, PR #370)** removed the worst convoy *holder*: the recovery-reconcile
  sudo/tmux liveness probe no longer runs inside the lock-holding event-append tx
  (pre-tx oracle hoist), + a bounded transient-load retry on `run.prepare`
  (`withTxRetryOnTransientLoad`) + `SET LOCAL statement_timeout` in the reconcile tx.
  This is **not** the same holder as F2 (`work.complete` git) / F4 / F5 / F6 — those remain.
- **#372 (OPEN, ready-for-agent)** is the `events.lock_wait_us` column + doctor convoy
  check. That is plan item **P1.1** — refine it, do not duplicate it.
- **#322 (CLOSED)** landed the *planner* cap for `run drive`. The DB-side claim-txn
  over-grant + the wedged-vs-quiet discriminator (F7) are net-new follow-ups (P0.3/P2.2).

## Priority summary

| # | Item | Finding | Effort | Reversible? | New lock? | Issue |
|---|---|---|---|---|---|---|
| **P0.1** | `deadlock.retry_exhausted` in-process counter | R2 residual | XS | yes | no | **#375** (ready-for-agent) |
| **P0.2** | panic observability: `recover()`→log→**re-panic** on dispatch+sweep goroutines | R1 residual | XS | yes | no | **#376** (ready-for-agent) |
| **P0.3** | F7 read-side wedged/quiet latch in the sweep cursor + doctor predicate | F7 (legibility half) | S | yes | no | **#377** (ready-for-agent) |
| **P1.1** | `events.lock_wait_us` per-waiter wait gauge + **scoped** doctor check | F1 / blind-spot (a) | M | column additive | no | **#372** (refined — see comment) |
| **P1.2** | route `supervisor.progress` off the hash chain | F1 amplifier | M | mostly | no | **#378** (ready-for-human / decision) |
| **P1.3** | instrument the **global** `audit_chain_head` wait | D1 | S–M | yes | no | **#379** (ready-for-agent) |
| **P2.1** | git-hoist the remaining lock-holding holders — **GATED** | F2 / F3 | L | partial | no (removes one) | **#380** (ready-for-human) |
| **P2.2** | in-txn `max_active_jobs` cap guard in `claimChosenJob` — **PARKED** | F7 (cap half) | M | yes | no (reuses held lock) | **#381** (ready-for-human) |

> Tracked: P0 = #375/#376/#377 · P1 = #372 (refined)/#378/#379 · P2 = #380/#381.
> Critical path to the structural fix: **Repro B (new test) + #372 → #380**.

---

## P0 — ship first (cheap, reversible, observability-only; no new lock, no behavior change)

### P0.1 — `deadlock.retry_exhausted` counter (R2 residual)
- **Finding/verdict:** R2 BENIGN — RFC 0104 `lockRun`-first + `withTxRetryOnDeadlock` (×3)
  is correct and guard-tested. The only residual is an **observability gap**: when the
  bounded retry is exhausted it is bucketed silently as `invalid_transition`, so a *new*
  path that re-inverts the lock order would be invisible.
- **Change:** at the retry-exhaustion return of `withTxRetryOnDeadlock`
  (`go/pkg/mutations/mutations.go`, the `rpc.NewError("invalid_transition", "… deadlock …")`
  tail), increment an **in-process** `expvar`/`log` counter `deadlock.retry_exhausted`.
  Extend `TestPerRunHandlersTakeLockRunFirst` to assert any new per-run handler is covered.
- **New risk + absence-proof:** a *durable* event impl would open a fresh tx and take the
  chain head on the cold post-failure path — re-introducing the exact contention this whole
  effort is about. **Absence-proof:** implement as `expvar`/`log` ONLY; the tx has already
  rolled back at the return point, so the counter holds no lock.
- **Verification:** drive Repro A (`run_lock_deadlock_test.go`); assert the counter moves;
  the static lock-order guard test flags a synthetic mis-ordered handler. Blast radius nil.

### P0.2 — panic observability (R1 residual)
- **Finding/verdict:** R1 BENIGN — systemd is the supervisor (`Restart=on-failure`,
  `RestartSec=2`, `KillMode=process`); a `recover()`-and-**continue** would resume a sick
  daemon and is a *correctness regression* (§D6). But a bare panic today gives no breadcrumb
  of which RPC/goroutine died.
- **Change:** a deferred `recover()` on the RPC dispatch goroutine (`go/pkg/rpc/server.go`,
  the `go func()` per-connection handler) and the sweep goroutine that logs `panic+stack`
  (and the method, for the RPC case) then **immediately `panic(r)`** — never swallows.
- **New risk + absence-proof:** ~nil; behavior is byte-identical (process still aborts → exit
  non-78 → systemd restarts) plus one journal line. **Absence-proof:** the `recover()` is
  immediately followed by `panic(r)`; no execution continues, no invariant newly trusted.
- **Verification:** inject a handler panic in a **scratch** daemon; assert the journal names
  the method and the process still exits and restarts. Blast radius nil.

### P0.3 — F7 read-side wedged/quiet latch (legibility half)
- **Finding/verdict:** F7 REAL (legibility gap, not a race). Nothing distinguishes a
  "correctly quiet" run from a "wedged" one.
- **Change:** extend the sweep cursor `last_result_json` (`go/pkg/recovery/sweep.go`, written
  **outside** any lock) with `{claimable_job_count, last_lane_advanced_at}`; add a doctor
  predicate `claimable>0 ∧ now()-last_lane_advanced_at>N ⇒ WEDGED`, never page when
  `claimable==0`.
- **New risk + absence-proof:** none — read-only, changes no control flow, adds no predicate
  to `claimChosenJob`, no lock scope.
- **Verification:** Repro D (`runreconcile_test.go`/`rundrive_test.go`); assert the latch
  reads WEDGED with a parked lane and quiet with `claimable==0`. Blast radius nil.

---

## P1 — measurement + the single biggest throughput reduction

### P1.1 — `events.lock_wait_us` + scoped doctor check  →  **#372 (refine, don't dup)**
- **Finding/verdict:** F1 substrate REAL; blind-spot (a) — the chain-head lock-wait is
  unmeasurable today. #372 already specs the column + an `event_chain_head_lock_convoy`
  doctor check via an owner bundle. **Refinements this review adds to #372:**
  1. Label the column a **per-waiter WAIT gauge**, not "hold-time" (it times the
     `FOR UPDATE` acquire wait, not the post-acquire hold).
  2. It instruments only the **per-repo** `repo_event_chain_heads` (C1) — it does **not** see
     the **global** `audit_chain_head` (C2); see P1.3.
  3. The doctor scan is **not free**: `lock_wait_us` is unindexed and §5 forbids a new index
     on `events`, so an unscoped scan seq/range-scans ~13.6M rows inside `HandleDoctor`
     against the append flood. Ship only **hard-scoped** to a recent `created_at` window
     within one `run_id` using the existing index, with an explicit `LIMIT`.
- **Action:** add these as a comment on #372; no new issue.

### P1.2 — route `supervisor.progress` off the hash chain (F1 amplifier)
- **Finding/verdict:** F1 is a REAL substrate **MISDIAGNOSED as the bottleneck primitive** —
  the chain-head lock sustained **98.65%** of events (re-verified live: 13,594,444 /
  13,780,281) at ~98k acq/s; it is *not* contended to death. But `supervisor.progress` is
  98.6% pure liveness chatter funneled onto the provenance chain. Taking it off removes ~98%
  of chain-head churn — the largest single reduction available.
- **Change:** emit `supervisor.progress` (constructed at `supervision.go:354` as
  `"supervisor."+event.EventType`, gated by `progressIsMeaningful` at :320) to an **unchained
  liveness store / chain-exempt class** instead of `appendEvent` on the hash chain.
- **New risk + absence-proof (consistency cost declared):** liveness chatter loses
  tamper-evidence + contiguity → "observed, not chain-anchored." **Absence-proof:** no
  production consumer reads the *events* (re-verified: the dashboard's
  `enrichSupervisorProgress` reads the `process_supervisors`/pointer tables, not the chain;
  no `go/pkg/reads` query filters `event_type='supervisor.progress'`); `assertEventChainLinear`
  ignores `event_type` (so removing a never-read type cannot break linearity); D028 already
  classes it volume/timing evidence, not durable provenance.
- **Why ready-for-human:** changing *what is chained* is a provenance-semantics product
  decision — needs a decision-log entry. **Verification:** replay the 98k/s burst against a
  test cluster; assert the chain still verifies AND chain-head acq/s drops ~98%.

### P1.3 — instrument the global `audit_chain_head` wait (D1)
- **Finding/verdict:** D1 (what the review missed) — every successful mutation's **final**
  write is `UPDATE striatumd.audit_chain_head … WHERE singleton=true`
  (`0001_authority_phase0.sql:212`, via `appendMutationAudit`, `mutations.go:438`/`:415`), a
  **global** singleton row-lock held to COMMIT. It caps the global mutation-commit rate and
  couples otherwise-independent repos; the #372 per-repo column does **not** measure it.
- **Change:** a second wait gauge on the audit append (mirroring #372's per-repo column) OR
  fold C2 into the instrumentation-hook-2 read-side `pg_locks ⋈ pg_stat_activity` sampler.
- **New risk + absence-proof:** an in-tx clock_timestamp pair on the *last* write adds a
  sub-µs bracket on a write the tx already commits — no new lock scope. **Absence-proof:**
  same safe-exception argument as #372's column.
- **Dependency:** design alongside #372 so the two gauges share shape. **Verification:** a
  multi-repo concurrent burst shows non-zero C2 wait while C1 (per-repo) waits stay flat.

---

## P2 — structural & correctness-sensitive (gated / parked; ship last)

### P2.1 — git-hoist the remaining lock-holding holders (F2/F3) — **GATED**
- **Finding/verdict:** F2/F3 REAL — `work.complete` holds `lockRunForJob` across 3 unbounded
  git side-effects (porter add+commit, source-publish, anchor CAS); `run.prepare` runs git
  inside its tx. These are the convoy **root** (#355 fixed only the recovery-reconcile holder).
- **Change:** apply the landed #198 / #355 pre-compute-oracle pattern — do the git **before**
  the tx, let the tx record only resolved values.
- **The hard constraint (§D2 TOCTOU — the single biggest risk in the whole review):** the
  hoist removes the lock that today serializes sibling git ref-advances. It is safe ONLY where
  the ref-advance is already a **CAS** `update-ref <new> <expected>`:
  - SAFE-with-caveat: `run.integrate` (`integrate.go:147`) and the `work.complete` anchor
    (`worktree.go:1037`/`:1088`) are CAS → a stale precompute fails **loudly** as
    `git_commit_apply_failed`, not silent corruption. Hoisting them is an **observable
    behavior change** (new failure surface under rare near-simultaneous fan-in) — do **not**
    advertise as behavior-equivalent.
  - **UNSAFE:** `run.prepare` branch-create — `gitEnsureBranchRef` runs a plain
    `git branch <name> <base>` (`run.go:848`) with **no CAS** and records `branch_base`
    captured pre-tx. Keep this op **in-tx**, OR add a commit-time HEAD re-validation.
  - **Acceptance criterion (must be enforced):** *every hoisted git ref-advance is a CAS
    `update-ref`.* A future hoist of a non-CAS git op ships silent corruption.
- **Gate (must clear before merge — permanent restructure may not ship on a sample of one):**
  1. **Build Repro B** (the #355 convoy reproduction) — no scaffolding exists today.
  2. Land **#372** (`events.lock_wait_us`) so the convoy is a measured quantity and the doctor
     threshold is set against data.
  3. A behavioral-equivalence witness: ≥3 simultaneous sibling completes on one run all reach
     the run branch (#290 reachability) without spuriously exhausting the 6-retry CAS loop.
- **Effort L. Blast radius high (run-branch provenance).** Ready-for-human (correctness).

### P2.2 — in-txn `max_active_jobs` cap guard in `claimChosenJob` (F7 cap half) — **PARKED**
- **Finding/verdict:** F7 — the cap is enforced only in `runreconcile.PlanLaunch`
  (`runreconcile.go:138`), not in `claimChosenJob` (`claim.go`), so the DB will over-grant a
  direct `work.await_packet`. D7b: this is a **missing check under the already-held
  `lockRunForSession`**, not a TOCTOU race — so it reproduces deterministically.
- **Change:** a `COUNT(in-flight) < max_active_jobs` guard **inside** the already-held
  `lockRun` tx in `claimChosenJob` — no new lock, no new ordering edge — **default-allow-on-
  error**.
- **Why parked:** today every workflow runs ≤1 job per lane, so the planner cap suffices; the
  DB-side guard only matters once a >1-job-per-lane workload exists (D208 revisit-trigger).
  File it so it is tracked, not lost. Ready-for-human (policy).

---

## Not doing (disproven or rejected — no work item)

- **F4 `worktree.gc` hoist — REJECTED (unsafe).** MISDIAGNOSED bottleneck (8 calls/4 days,
  cannot serialize the flood) **and** the proposed hoist is a `worktree_head_unreachable`
  silent-data-loss TOCTOU (§D3). If latency ever bites: a child-context timeout on the
  per-worktree git *inside* `lockRepo` (keeps serialization, adds no TOCTOU).
- **F5 `run.integrate`.** MISDIAGNOSED — `lockRepo` acquired by integrate **0 times** in
  production (re-verified: 0 `run.integrated` events; 5 audit rows, all denied pre-tx). Drop.
- **F6 `artifact.publish`.** BENIGN — blobs bounded ≤20 KB, zero same-run overlap. Fold a
  pre-tx `PutBytes` into P2.1 opportunistically if that campaign lands; no standalone change.
- **R3 latency tails.** MISDIAGNOSED-as-daemon (correct ruling) — agent think-time; a 40 h gap
  cannot be a held lock. Do **not** instrument.
- **REPORT §6.3 (narrow the 57014 swallow) — REJECTED.** Re-orphans jobs (#197 regression,
  §D5); no PgError-level lock-vs-statement discriminator exists.
- **REPORT §6.4 (role-level `lock_timeout`) — REJECTED for now.** Retry-storm + benign-
  reclassification; the objection is unanswered (§D4). Blocked by the determinism gate.
- **D7a `seenRequests` "leak" — WITHDRAWN (false finding).** Already a bounded `boundedSeen`
  (50k cap + 10 min TTL, `dedupe.go`).

## Sequencing / dependency graph

```
P0.1  P0.2  P0.3   ── independent, parallelizable, ship any order ──┐
P1.1 (#372) ──┬─ prerequisite ─→ P2.1 (git-hoist gate)             │
P1.3 (D1) ────┘  (share gauge shape)                               │
P1.2 (off-chain) ── independent decision, big win, do early ───────┘
P2.2 (cap guard) ── parked until a >1-job/lane workload exists
```
Critical path to the structural fix: **Repro B (new test) + #372 (P1.1) → P2.1**.
Everything in P0 and P1.2/P1.3 is independent and can land first with no gate.
