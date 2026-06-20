# Striatum — Failure-Mode Audit

- **Auditor model:** Claude Opus 4.8 (1M context)
- **Date:** 2026-06-19
- **Target:** repository root `/home/halbritt/git/striatum` (whole-repo survey + deep traces)
- **Method:** read-only static tracing; five parallel deep-trace passes, then auditor verification of every promoted high-severity claim against source.

---

## 0. Audit Basis

**Repo state.** Branch `main` @ `36f7700c`. Working tree carries one uncommitted edit:
`go/pkg/reads/doctor_quorum.go` (scopes the `dissent_ledger_incomplete` doctor
check to non-terminal runs, D236 — a detectability refinement, not a behavior
change to a state-bearing path). No other dirty files.

**Authority used.** Read-only only: file reads, static search, `git status/log/diff`.
No build, no tests, no daemon, no fault injection were run (none authorized).

**Commands.**
- `git status --short`, `git log --oneline`, `git diff go/pkg/reads/doctor_quorum.go` — **ran — passed** (read-only).
- `make test` / `make -C go check-tests` (race + coverage, PG-backed) — **not run — not authorized**.
- `make smoke` / `make race` — **not run — not authorized**.
- Fault injection (kill daemon mid-sweep, truncate a blob, kill a migration, partition the blob store) — **not run — not authorized** (no named disposable-environment grant).

**Runtime shape.** Three Go binaries: `striatumd` (the single PostgreSQL writer +
recovery/auto-spawn schedulers + all surfaces), `striatum` (thin RPC client),
`striatum-supervisor-helper` (per-lane PTY bridge under tmux, survives daemon
restart). Durable state: PostgreSQL (`striatumd` schema; 37 runtime migrations
`0001-0037` + 17 owner-bundle DDL files), a hash-chained `audit_log`, a
content-addressed blob store (local FS or Garage S3), and per-job git worktrees
with attempt-namespaced run pins (`refs/striatum/<run>/<job>/<attempt>`). Background
loops: recovery sweep (60s) and auto-spawn sweep, both on `pkg/recovery`'s scheduler.
Transactions open at **`pgx.ReadCommitted`** (`pkg/db/connection.go:198`); cross-cutting
serialization is the **RFC 0104 per-run advisory lock** (`lockRun`).

**Depth ledger (section-0 evidence).**

| Boundary | Pass | Why selected / skipped | Files read | Strongest tier | Residual risk |
|---|---|---|---|---|---|
| Job/lease state machine, claim, work.complete | deep-trace | core durable transition, highest write volume | `mutations/claim.go`, `lifecycle.go`, `mutations.go`, `run.go`, `write_scope_guard.go`, `db/connection.go`, `db/authority.go` | static-traced | concurrency claims rest on lock+isolation reasoning, not executed |
| Recovery sweep / requeue / auto-finalize / escalation | deep-trace | must run unattended; false-success & stuck-job blast radius | `recovery/sweep.go`, `scheduler.go`, `mutations/recovery*.go`, `runreconcile.go`, `cmd/striatumd/main.go` | static-traced | loop-termination under poison state not fault-injected |
| Artifact provenance: worktree / integrate / git-apply / blob | deep-trace | F1 surface (lost work, torn artifact, forged anchor) | `mutations/worktree.go`, `integrate.go`, `git_commit_apply.go`, `artifact*.go`, `blob/*.go`, `apply/*.go`, `run_completion_gate.go` | static-traced | blob partial-upload behavior not fault-injected |
| Supervisor / PTY / tmux / daemon-restart survival | deep-trace | cross-process boundary; dead-agent + restart | `supervisor/pty.go`, `helper.go`, `tmux_liveness.go`, `agentloop/loop.go`, `sessionliveness/liveness.go`, `mutations/supervision*.go` | static-traced | FIFO/PTY torn-write timing not executed |
| Audit hash chain + migrations + owner bundles | deep-trace | append-chain integrity; schema-transition atomicity | `db/audit.go`, `event_write.go`, `migrations.go`, `owner.go`, `authority_bootstrap.go` | static-traced | mid-migration crash window not fault-injected |
| Barrier / fan-in / quorum assembly | deep-trace | stuck-forever & diverged-sibling risk | `mutations/barrier_*.go`, `db/barrier_predicate.go` | static-traced | live participant-death recovery exit not executed |
| MCP/RPC token expiry, interrogation, web UI | survey | lower durable-state blast radius | `rpc/auth_pg.go`, `capability.go`, `mutations/interrogation.go` | static-traced | token-expiry-mid-work wedge surveyed only |
| Doctor integrity catalog (detectability surface) | survey | the standing detection net | `reads/doctor_*.go` (grep), `docs/operator/doctor-acknowledged-loss.json` | static-traced | not the failure source; the detector |

---

## 1. Verdict

**`MIXED_RECOVERY` — confidence `medium`.**

No `BLOCKER` survives tracing. Every blocker-grade claim the deep-trace pass
produced — torn lease state on crash, auto-finalize forging provenance, a
concurrent audit-chain fork, a dead-lease requeue race, a git-ref/DB-anchor tear —
**collapses under verification**: the per-run advisory lock serializes the
"racing" paths, `work.complete` is a single transaction, the audit chain holds a
`FOR UPDATE` on its singleton head row, and the run-completion reconstructability
gate independently blocks a run whose required artifact body is gone. The system
is genuinely **recovery-aware**: per-run locking, an idempotent hash chain,
attempt-namespaced pins, a ~50-class doctor catalog with an sha-bound
acknowledged-loss baseline, and a sealed-barrier "trap-killer" predicate are all
load-bearing and worth preserving.

What remains is a cluster of `SERIOUS` **availability / unattended-liveness** gaps,
not data-corruption gaps. The sharpest: a *panic* (not an error) inside either
background sweep crashes the single-writer daemon and, because the sweep re-reads
durable state every restart, a deterministic poison row produces a restart
crash-loop with no automatic exit. Confidence is `medium` because execution and
fault injection were unauthorized — the lock/isolation reasoning is established
statically, but the real-world exposure of the migration-atomicity and blob
partial-write findings depends on dynamic behavior not exercised here.

Counts: **0 BLOCKER · 4 SERIOUS · 5 MINOR** (ranked) + 6 NOTE (contextual).

---

## 2. Failure Boundary Inventory

**State-bearing surfaces found.** (a) PostgreSQL job/lease/run state machine
(single writer, ReadCommitted + per-run advisory lock). (b) Hash-chained
`audit_log` + event chain. (c) Schema migrations (37 runtime) + owner-bundle DDL
(17). (d) Content-addressed blob store (local FS / Garage S3). (e) Per-job git
worktrees + attempt-namespaced run pins + run-branch integration. (f) Barrier /
fan-in / quorum assembly with a two-phase journal. (g) PTY/FIFO lane delivery +
tmux supervisor + supervisor-helper subprocess. (h) Background recovery & auto-spawn
sweeps. (i) MCP/RPC capability tokens (expiring, session-bound).

**Selected deep dives:** boundaries (a)–(h). **Surveyed, not deep-traced:** (i)
token-expiry-mid-work (legible refusal, low durable blast radius), the web UI
service, and interrogation windows (legible `interrogation_unavailable`,
non-wedging by design, RFC 0103 W4).

**Detectability net (strong, preserve).** `striatum doctor` ships ~50 integrity
problem classes covering exactly these boundaries —
`job_completed_without_anchor`, `worktree_head_unreachable`,
`artifact_anchor_hash_mismatch`, `artifact_blob_metadata_missing`,
`barrier_orphan`, `recovery_sweep_cursor_wedged`, `recover_orphan_supervisor`,
etc. — plus an operator-acknowledged, content-sha-bound loss baseline
(`docs/operator/doctor-acknowledged-loss.json`). Most failure modes below are
**detected** by this net; the gaps are in *automatic recovery*, not *visibility*.

---

## 3. Ranked Failure-Mode Ledger

> Each row is auditor-verified against source. Severity reflects the verified
> behavior, not the deep-trace pass's first guess (several were lowered).

| id | sev | subsystem / trigger | invariant | failure point | current behavior (verified) | blast radius | detect | recovery | tier / status |
|---|---|---|---|---|---|---|---|---|---|
| **FMA-001** | SERIOUS | recovery+auto-spawn sweep / dependency failure + malformed durable row → panic | a sweep fault must not take down the single writer; unattended liveness | `cmd/striatumd/main.go:760-765` (recovery: recover→log→`panic(r)` re-raise) and `:800-816` (auto-spawn: **no** recover) over `recovery/sweep.go` → `mutations.SweepRun` | A sweep **error** is handled gracefully (per-run `sweep_degraded` cursor, `OnSweepError` logs, backs off, continues). A sweep **panic** (e.g. nil-deref classifying a malformed job/lease row) is re-panicked / unrecovered → **whole daemon process dies**. systemd restarts it; the next sweep re-reads the same durable rows → if the panic is deterministic, **restart crash-loop**, no automatic exit. | All runs frozen (single writer down); unattended operation halts repo-wide. | loud (process exit + stack in journal; systemd churn) | systemd restart heals a *transient* panic; a *deterministic* poison row needs manual DB repair — no automatic exit traced | static-traced / static only |
| **FMA-002** | SERIOUS | migrations / crash between DDL-commit and version-stamp | migrations apply atomically and idempotently | `pkg/db/migrations.go:254-277` `applyOne` — three separate `runner.Exec` (DDL; stamp `schema_migrations`; stamp `schema_meta`), no enclosing tx | The DDL string commits in its own implicit tx; the version stamps are **separate** autocommit writes. Crash in the gap = DDL applied, version unstamped. Restart reads the stale version and **re-runs the same migration**. Safety depends entirely on every migration being idempotent — an **unenforced convention** (no guard, no `verifyRecordedHash` for the in-progress version). A non-idempotent statement (`CREATE TYPE`, bare `ADD COLUMN`, a data backfill) re-errors → `ApplyMigrations` fails → **daemon refuses to start**. | daemon down until manual reconciliation; potential half-applied schema | loud (won't start) — but a *silent* half-apply of an idempotent migration is invisible until a query hits the missing object | manual (drop partial objects / stamp version); fail-closed | static-traced / static only |
| **FMA-003** | SERIOUS | barrier / a required fan-in participant is permanently unrecoverable | a sealed barrier must have a finite recovery exit | `mutations/barrier_fanin.go` readiness `bool_and` over declared siblings; `recovery_quarantine_lane.go:97` is **terminal-run-only** + operator-RPC-invoked | A dead participant is first handled by the normal recovery path (requeue same attempt → fresh lane). On budget exhaustion it **escalates to `needs_operator`**. For a **quorum/panel** barrier an abstention budget (`barrier_quorum.go` `resolvePanelSeats`/`StructurallyUnrecoverable`) can still fire. For a **strict fan-in** (all siblings required) there is **no abstention** — the run sits in `needs_operator` until a human cancels or quarantines. `recovery.quarantine_lane` cannot seal a live run's seat (it gates on a *terminal* run). | one run stalls until operator action | loud (run stalls; escalation/`needs_operator`; doctor `barrier_orphan`) | bounded operator recovery (cancel / manual quarantine); no automatic gap-seal for strict fan-in | static-traced / static only |
| **FMA-004** | SERIOUS | blob store / truncated or partial upload | a stored blob is byte-identical to its recorded sha | `pkg/blob/client.go` `PutBytes` (verifies **size** via `StatObject`, not a content readback) → `mutations/artifact.go` publish | `PutBytes` returns the *pre-upload* sha and checks object **size**, not stored content. A truncated PUT that satisfies the size check stores a body whose readback hash ≠ recorded sha. The mismatch is **not** caught at publish. | a published artifact whose body is corrupt | **loud, but late**: caught at the run-completion reconstructability gate (`run_completion_gate.go:96,246` → `required_artifact_unreconstructable`), which blocks run completion | manual: delete corrupt blob + republish, or `recovery reseal`; exposure limited to blob-placement (S3/Garage), not local-FS-tracked or git-anchored artifacts | static-traced / static only |
| **FMA-005** | MINOR | recovery / auto-finalize completion path | the two recovery completion paths enforce the same durability floor | `mutations/recovery_auto_finalize.go:1300` (`completeAutoFinalizedJob` calls only `verifyRequiredArtifacts`) vs `mutations/recovery.go:2573-2576` (`completeRecoveredJob` calls **both** `verifyRequiredArtifacts` **and** `ensurePerJobPublishedArtifactsDurable`) | Auto-finalize seals a job `completed` after a row-presence check but **skips the per-job durability probe** its sibling runs. For **required** artifacts the run-completion reconstructability gate backstops it loudly (so no run is forged). For **non-required** published artifacts the durability probe is simply absent — a job can seal `completed` with a non-durable optional artifact that `completeRecoveredJob` would have refused. | optional artifact body may be silently non-durable on an auto-finalized job | delayed (doctor; run gate only covers *required* artifacts) | `recovery reseal`; align the two paths | static-traced / static only |
| **FMA-006** | MINOR | supervisor / daemon restart with a buffered packet | a queued packet is durable or replayable | `mutations/supervision_delivery.go` no-reader path buffers into an in-process `pipeBuffers` map | When a FIFO has no reader, `supervise.send` buffers the packet **in memory** and records a degraded `supervisor.packet_buffered` event. A daemon restart drops the map. Self-driving (pull) lanes re-fetch via `work.await_packet`, but a legacy `supervised_push` lane never receives it. | one push-lane packet lost across a restart | structured signal (`packet_buffered`) exists, but the drop itself is silent | operator re-send (`supervise send`); pull-lanes self-heal | static-traced / static only |
| **FMA-007** | MINOR | owner bundles / partial apply with cross-bundle dependency | a bundle applies atomically; a re-run is safe | `pkg/db/owner.go:244-274` `applyOneOwnerBundle` (bundle SQL + stamp in one tx — good) | The bundle/stamp pair **is** transactional (rollback-safe). Residual: a re-run after an earlier bundle's objects went missing can fail on a cross-bundle dependency (`relation does not exist`) → daemon **fail-closed** won't start. Bounded by idempotent DDL + the `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` guard. | daemon down until manual fix | loud (won't start) | manual; fail-closed is a recovery property | static-traced / static only |
| **FMA-008** | MINOR | migrations / hash verify skipped for in-progress version | applied SQL is verified against embedded SQL | `pkg/db/migrations.go:143-149` + `verifyRecordedHash:243-252` | After a stale-version restart (FMA-002), the partially-applied migration's version is *not* `≤ current`, so `verifyRecordedHash` is **skipped** for exactly the migration most likely to have drifted. Disk/embedded drift on that version goes unverified until an explicit `VerifyMigrationsSHASource`/doctor run. | undetected SQL drift on one migration | delayed | manual verify | static-traced / static only |
| **FMA-009** | MINOR | attestation / long tool-call-less local work | honest slow work keeps a publishable byline | `pkg/sessionliveness/liveness.go` `wedged_no_tool_progress` (≥600s, history non-nil) → `claim.go` attestation gate | A lane that issued ≥1 tool call then does 10+ min of silent local work (test suite, repo scan) is reclassified `stalled_wedged` → attestation drops → `artifact.publish` byline refused mid-work. | publish refused on honest work; operator override needed | loud (publish refused) | `work.heartbeat` during local work; operator override | static-traced / static only (known DX friction) |

### Top failure modes — prose

**FMA-001 (the one to harden next).** The recovery and auto-spawn sweeps are the
daemon's unattended heart, and they read malformed-by-history durable state. The
scheduler correctly treats a returned **error** as non-fatal (degraded cursor +
backoff). But a **panic** is a different path: `main.go:762` logs and then
`panic(r)` re-raises, and `main.go:800` has no recover at all — either way the
single-writer process dies. systemd restarting it is fine for a transient fault,
but the sweep re-reads the *same* rows on boot, so a deterministic nil-deref
(e.g. an unexpected-null column in a recovery-classification query) becomes a
crash-loop with no automatic exit. Smallest next step: wrap each `SweepOnce` in a
recover that converts a panic into the same degraded-cursor + backoff path the
error branch already uses, so one poison run degrades instead of downing the
daemon. (Validation: a fault-injection grant to feed one synthetic malformed
job row to a scratch daemon's sweep.)

**FMA-002 / FMA-008 (migration atomicity).** `applyOne` is three separate
autocommit writes. The DDL itself is atomic (one Exec → one implicit tx), but the
**version stamp is a separate commit**, so the crash window leaves DDL-applied /
version-unstamped, and recovery depends on every migration being re-runnable.
The shipped migrations largely use `IF NOT EXISTS`, which is why this has not
bitten — but nothing *enforces* it, and the hash-verify that would catch drift is
skipped for precisely the in-progress version. Smallest next step: stamp the
version inside the same Exec batch as the DDL (one tx), or add a deploy guard that
asserts every migration body is idempotent.

**FMA-003 (strict fan-in dead seat).** Quorum panels degrade gracefully via the
abstention budget; strict fan-in barriers do not, so a permanently-dead *required*
sibling parks the run in `needs_operator`. This is loud and bounded (an operator
can cancel or quarantine), and the dead seat is first retried — so it is not
"stuck forever silently." But there is no automatic terminal-gap seal for a live
run, and `recovery.quarantine_lane` deliberately refuses non-terminal runs.
Smallest next step: decide whether a strict fan-in seat should be auto-sealable as
a terminal gap after recovery exhaustion, or remain operator-gated by design
(document it either way).

---

## 4. Recovery And Idempotency

**Strong, traced recovery.** `work.complete` is fully transactional
(`lifecycle.go:1244-1268`: job + queue-message + lease + liveness + event in one
tx). The recovery sweep degrades per-run (records a `sweep_degraded` scheduler
cursor) and continues rather than aborting on a single run's error. Run pins are
attempt-namespaced, so a retry never clobbers a prior attempt's provenance. The
barrier two-phase journal (`state='assembling'` + target sha persisted before the
git CAS) makes assembly crash-resumable with an idempotent CAS. `recovery reseal`,
`recovery resume --complete`, `recovery complete-stalled`, `recovery requeue-stale`,
and `recovery quarantine-lane` are all real, daemon-owned, and their invocation
paths (recovery verbs / sweep) are traced.

**Where recovery is absent or partial.** (1) No panic→degrade conversion in the
sweep goroutines (FMA-001). (2) No atomic DDL+stamp in the migration runner
(FMA-002). (3) No automatic terminal-gap seal for a strict fan-in dead required
seat (FMA-003). (4) No content readback verification on blob put (FMA-004). (5)
No per-job durability floor on the auto-finalize completion twin (FMA-005). (6) No
replay of an in-memory-buffered push packet across restart (FMA-006).

---

## 5. Concurrency And Partial-Write Notes

- **The RFC 0104 per-run advisory lock is the linchpin.** `claim`, `work.complete`,
  the verdict/`maybeCompleteRun` path, and `SweepRun` all take `lockRun`
  (`recovery.go:1355`, `lifecycle.go:1137`, `claim.go`, `run.go`). This serializes
  the "races" the deep-trace pass flagged (REC-1 dead-lease-vs-complete, REC-5
  requeue-vs-claim): they cannot interleave within a run. **Preserve the per-run
  granularity** — relaxing it to per-repo or global would reintroduce the
  lock-ordering deadlocks RFC 0104 closed.
- **Audit chain is fork-safe under ReadCommitted** because a `FOR UPDATE` on the
  singleton `audit_chain_head` row serializes append, and the V3 path computes the
  hash inside a SECURITY-DEFINER function co-committed with the mutation. ReadCommitted
  does not weaken this — the row lock, not MVCC, is the serializer.
- **Partial writes that are benign:** an orphan attempt-pin if the daemon dies
  after `git update-ref` but before the `work.complete` tx commits — the DB rolls
  back, the job re-drives, and re-anchoring is idempotent (the orphan ref is
  reachable and reaped). An orphan content-addressed blob if a crash lands after
  `PutBytes` but before the artifact row — harmless dedup on retry. Both are NOTE-tier.
- **Lease expiry is lazy by design** (runs on claim + sweep). A lane that misses
  its heartbeat window can have its job requeued under it; the next `work.*` call
  is loudly refused (expired lease). Work is not silently double-applied — the
  refused lane fails closed.

---

## 6. What Fails Loudly Or Safely (preserve)

- **Per-run advisory lock (RFC 0104)** — serializes all run-scoped mutations incl.
  the recovery sweep; the deadlock retry (`withTxRetryOnDeadlock`, SQLSTATE 40P01)
  is the backstop.
- **Hash-chained audit append** — `FOR UPDATE` singleton head + V3 SECURITY-DEFINER
  atomic append (`db/audit.go`); `VerifyRows` chain-walk catches any gap.
- **Run-completion reconstructability gate** (`run_completion_gate.go:96,246`) —
  orthogonal to the verdict path; refuses to complete a run whose *required*
  artifact body is unreconstructable, with an actionable `recovery reseal` message.
  This is what neutralizes the "auto-finalize forges provenance" concern.
- **Attempt-namespaced run pins** (`worktree.go` `pinWorktreeCommitStack`) — a
  retry cannot clobber an earlier attempt's commit provenance.
- **Sealed-barrier trap-killer** (`db/barrier_predicate.go`, `staged.seal = live.seal`,
  guard `TestBarrierPredicateHasNoRefCount`) — stale-seal contributions are
  *structurally invisible*, not filtered after the fact, across all four callers.
- **Recovery sweep degrades, not wedges** (`recovery/sweep.go:68-86`) — a single
  run's sweep error becomes a visible `sweep_degraded` cursor and the loop continues.
- **Daemon-restart lane survival** — `context.WithoutCancel` + systemd
  `KillMode=process` (RFC 0103) keep supervised lanes alive across daemon restart.
- **Doctor catalog + acknowledged-loss baseline** — ~50 integrity classes give this
  audit's findings a standing detector; losses are sha-bound and operator-acknowledged.

---

## 7. Gated Verification (would raise confidence)

These were **not run — not authorized**; each would convert a `static only` row to
`fault injected - observed`:

1. **FMA-001:** on a scratch daemon (own `STRIATUM_DAEMON_RUNTIME_DIR`), feed the
   recovery sweep one synthetic malformed job/lease row and confirm whether it
   panics the process and whether restart crash-loops.
2. **FMA-002:** `kill -9` a scratch daemon mid-`applyOne` (after DDL, before stamp),
   restart, observe re-apply behavior with an intentionally non-idempotent test
   migration.
3. **FMA-004:** truncate a blob in a disposable bucket and confirm the mismatch is
   caught only at the run-completion gate, not at publish.
4. **FMA-003:** drive a strict fan-in barrier, kill one required participant
   permanently, confirm the run parks in `needs_operator` with no auto-exit.
5. Run `make -C go check-tests` (race + coverage) to confirm none of the above is
   already covered by an existing concurrency/recovery test.

---

## 8. Residual Risk And Unread Areas

- **Surveyed, not deep-traced:** MCP/RPC token-expiry-mid-work (legible refusal;
  a session token expiring mid-job loudly refuses `work.complete` — wedge risk is
  operator-recoverable, not corrupting), the web UI service, and interrogation
  windows (non-wedging by design).
- **Dynamic exposure unmeasured:** FMA-002's real bite depends on whether any
  shipped migration is non-idempotent (most use `IF NOT EXISTS`); FMA-004 depends
  on blob placement being S3/Garage rather than local-FS.
- **Known/largely-mitigated:** the #417 phantom-supervisor reconcile storm is
  fixed (migration 0033 + closed-session reap backstop, `mutations.go:1486-1500`);
  treated here as a resolved class with a residual closed-session reap path, not a
  fresh finding.

### Rejected Candidates (deep-trace claims that did not survive verification)

- **JOB-1 (torn lease: job=completed, lease=active on crash)** — refuted.
  `lifecycle.go:1244-1268` updates job, queue-message, lease, liveness, and event
  on the **same** `tx`; a crash before COMMIT rolls all back. No torn durable state.
- **REC-1 (dead-lease requeue races `work.complete` → ghost requeue)** — refuted.
  `SweepRun` (`recovery.go:1355`) and `work.complete` (`lifecycle.go:1137`) both
  hold the per-run advisory lock, so they cannot interleave; and a queued job with
  `current_lease_id = NULL` is the *normal* claimable state (claim mints a fresh
  lease), not a wedge.
- **REC-2 (auto-finalize forges provenance, run advances)** — downgraded to FMA-005.
  `completeAutoFinalizedJob` calls `maybeCompleteRun`, and the run-completion
  reconstructability gate blocks any run with an unreconstructable required body.
- **AUD-1 (concurrent appenders fork the audit chain)** — refuted (the pass's own
  body agreed): the `FOR UPDATE` singleton head row serializes appenders under
  ReadCommitted; the V3 SD function is the single hash writer.
- **ART-1 (git-ref ↔ DB-anchor tear, BLOCKER)** — downgraded to NOTE: DB rollback
  on crash + idempotent re-anchor on re-drive; orphan ref is reachable and reaped.
- **ART-3 (artifact-row ↔ blob orphan, SERIOUS)** — downgraded to NOTE:
  content-addressed key makes the orphan blob a harmless dedup on retry.
- **REC-3 (sweep crash → recovery-bookkeeping drift)** — **trigger not traced to a
  cross-transaction window.** `recordRecoveryAction` and the event append appear to
  execute within the `SweepRun` `lockRun` transaction; the described counter/event
  drift would require them to be in separate transactions, which was not established.
  Omitted rather than promoted on speculation.
- **REC-4 / REC-5 / REC-6 / BAR-2 / SUP-2 / SUP-5 / SUP-7** — real but NOTE-tier:
  bounded by `max_requeues`→escalation (REC-4), the `uq_active_work_message_per_job`
  unique constraint + per-run lock (REC-5), operator-bounded cancel (REC-6),
  co-transactional void + seal-equality predicate (BAR-2), and daemon-restart reap
  of orphaned PIDs (SUP-5/SUP-7); SUP-2 dead-agent detection lags a warm lease only
  up to the heartbeat deadline.
