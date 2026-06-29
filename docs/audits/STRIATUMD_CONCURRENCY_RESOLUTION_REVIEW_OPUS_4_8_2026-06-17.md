# striatumd — Adversarial Concurrency-Resolution Review

Lead reviewer: concurrency-engineer-opus-4-8-001
Date: 2026-06-17
Target: `striatumd` Go daemon @ HEAD `d88518a5` (review authored against `v2.33.0-47-gc2213393`)
Backing store: loopback PostgreSQL 16, DB `striatum_daemon`, schema `striatumd`
Review under triage: `docs/operator/daemon-perf-analysis/REPORT.md`
Rubric satisfied: `/home/halbritt/git/prompts/CONCURRENCY_RESOLUTION_REVIEW.md`

Voices used throughout, never blurred: **stated** (what the review/detector claims) ·
**actual** (what the source lines / live read-only counters demonstrably show — file opened
and quoted) · **mine** (my judgment, as judgment). Every load-bearing claim is tagged
`VERIFIED-from-source` (I opened the line), `VERIFIED-live` (read-only SELECT against
`striatum_daemon`), `PREDICTED-FROM-SOURCE` (structural fact true, blast-radius needs a live
load), or `ASSERTED` (trusted, not opened).

---

## A. THESIS — the forced verdict

**FINDINGS: MIXED.** Of the ten concurrency claims (F1–F7, R1–R3): three are REAL structural
defects worth a change (F1, F2, F3), three are MISDIAGNOSED as live bottlenecks though
structurally real-but-latent (F4, F5, R3), three are BENIGN-by-design (F6, R1, R2), and one
is REAL-but-low (F7, a policy/legibility gap, not a contention hazard). The review is
**unusually source-accurate and honest** — it self-caught an investigator-injected GUC false
fact (`lock_timeout=3s`), reset the live role to baseline, and recorded the catch (REPORT
§0). I credit that and do **not** rubber-stamp it: my value is the per-finding FINAL verdict
and the skepticism toward the review's own fixes (Section D).

**THREADING: RIGHT-SIZED.** `OVER-` / `UNDER-PARALLELIZED` is the wrong axis here and saying
so is itself a finding. `striatumd` has **no CPU-parallel compute span to tune** — it is
lock/IO/subprocess-bound over loopback Postgres. The "concurrency" is one RPC-dispatch
goroutine per accepted connection (rpc/server.go:176, serial within a connection), a ~60 s
recovery-sweep scheduler, and a flag-gated auto-spawn scheduler; `GOMAXPROCS` is the box
default 12, no errgroup fan-out. The Go-level shared mutable state is tiny and correctly
synchronized (`go vet` clean; `-race` suite green per Makefile/ci). The unit of contention is
a **PostgreSQL row/advisory lock**, not a cache line, so PMU/HITM/MESI counters and the
classic Amdahl/noise-floor ground-truths are **inapplicable in textbook form** (Section B).
The daemon is correctly sized; the defects are *hold-time hygiene inside an already-correct
lock discipline*, not a wrong number of threads.

**Confidence: HIGH** on the structural verdicts (every cited line opened and quoted; the
lock-ordering ledger reproduced from source) — **MEDIUM** on the production-convoy
blast-radius (the headline #355 convoy has **never been reproduced**: 0/1252 `work.complete`
and 0/N `run.integrate`/`run.prepare` audit rows carry a 57014/convoy denial in live history;
Repro B has no scaffolding).

**Single biggest risk, in one clause:** shipping the structural fix (the git-hoist) *as if it
were a behavior-preserving perf change* when on the correctness-sensitive paths it is a
TOCTOU that moves git ref side-effects outside the lock that today serializes them.

**The forced F1 adjudication:** F1 (the chain-head `FOR UPDATE`) is the review's headline, but
it is **a REAL substrate MISDIAGNOSED as the bottleneck primitive.** The lock itself drained
**98,008 acquire→hash→insert→advance→release cycles in one wall-clock second** (VERIFIED-live)
— a ~10.2 µs/holder floor, a sub-millisecond row lock sustaining ~98k ops/s, which is the
*opposite* of a lock contended to death. The real defect is the **git-in-txn HOLDER** (F2/F3)
that parks the chain head across an unbounded `git` subprocess and converts the flood into a
57014 convoy. **F1 is the amplifier; F2/F3 are the root.** The fix the review already points
at (hoist git out of the lock-holding txn, the landed #198 `recovery.go` pre-compute pattern)
is subtraction-correct *in direction*, but see Section D for the TOCTOU caveat that splits the
hoist into a free half and a correctness-sensitive half.

**Load-bearing assumption the verdict rests on:** that the single-operator workload captured
in live history (one hot `repository_id`, the 98k/s burst, p50≈250 s between same-run
completes) is representative of the regime the findings describe. **Evidence that would flip
it:** a captured 57014 convoy row on `work.complete`/`run.prepare`/`run.integrate` under a
real multi-lane fan-in load, or a multi-holder same-repo burst — either would promote
F2/F3/F5 from PREDICTED-FROM-SOURCE to MEASURED and could justify the lock_timeout lever the
retry-storm objection currently blocks.

**The assumption I would least like to be wrong about:** that the git ref-advances in the
hoist targets are already compare-and-swap (`update-ref <new> <expected>`). If that CAS were
*not* present, the git-hoist would be a silent last-writer-wins corruption of a run branch,
not a loud refusal. I VERIFIED it *is* present for `run.integrate` (integrate.go:147) and the
`work.complete` anchor (worktree.go:1037/1088) — but it is **absent** for `run.prepare`'s
branch-create (run.go:841), which is why that one hoist is the most dangerous.

---

## B. INPUTS INGESTED / EVIDENCE GAPS

**Review ingested:** `docs/operator/daemon-perf-analysis/REPORT.md` (status: evidenced
analysis; author `operator-claude-opus-4-8-001`), authored against `striatumd`
`v2.33.0-47-gc2213393`, pid 82183.

**Build under triage:** the per-finding agents read HEAD `d88518a5` (5 commits past the report
sha `c2213393`). Per the ground-truth reproduction agent: `go build` + `go vet ./...` pass with
no output; every cited `file:line` resolved at `d88518a5` (one citation-drift note below). The
live daemon was restarted between report authoring and this triage — **pid 82183 → 479772** —
so every live count is a light-load snapshot on 2026-06-17, not the convoy regime the findings
describe. **Mid-review the shared tree advanced again — `d88518a5 → f2d35c4e`** — when a
concurrent session merged `fix/355-recovery-reconcile-convoy` (`6638cc26`, "move reconcile
liveness probe out of the event-append tx (#355)"). That merge lands the **`run.prepare`
victim-side mitigation** F3/§6 point at: `runPrepare` is now wrapped in
`withTxRetryOnTransientLoad` (run.go:34; 4 attempts, 25 ms base backoff, surfacing a clean
`daemon_under_load` on exhaustion instead of a raw `exit_code=10` 57014). This **corroborates
the review's direction** (bound the victim's blast radius) and strengthens the RIGHT-SIZED
verdict; it does not change F3's structural verdict (the git-in-txn holder F2 is untouched).
Findings are anchored to `d88518a5` (what the agents read), with this `f2d35c4e` delta noted
wherever it supersedes a claim. **This provenance drift is a real gap, not a footnote:** the live witnesses confirm
the *structural* claims and *falsify* the "this is a live bottleneck" framing of F4/F5/F6, but
they cannot confirm or deny the *worst-case* convoy because the box was never under convoy
load during capture.

**Regime (ground-truth 3, VERIFIED-live + VERIFIED-from-source):** `nproc=12` (Intel i5-11400F,
6c/12t, single socket, **no NUMA**); x86-64 / x86-TSO hardware ordering but the governing
contract is the Go memory model; concurrent-GC Go allocator. pgxpool default `MaxConns =
max(4, NumCPU) = 12`; live pool holds 4 backends (1 active) under light load — **pool
saturation is not the current bottleneck**. Effective per-statement timeout is **60 s** (pool
RuntimeParam at connection.go:287-288 injected because the DSN omits it), which *shadows* the
role-level `statement_timeout=600s` — this corrects REPORT line 26/115 which reason about the
600 s value. Role baseline is `{statement_timeout=600s}` with **no `lock_timeout`** (inherits
server default 0/disabled) — the report's §0 self-correction is VERIFIED.

**Ground-truths the rubric demands that are INAPPLICABLE here, stated as a finding:**
- **Noise floor (GT1):** there is no re-runnable representative CPU workload; the honest noise
  floor is run-to-run variance of append latency under the `supervisor.progress` chatter, and
  it is **unmeasurable today** — there is no lock-acquire/commit event pair (REPORT blind-spot
  a). Any "under-the-noise" verdict in this regime is UNDECIDABLE against a measured threshold
  until the proposed `events.lock_wait_us` column lands.
- **Amdahl ceiling (GT2):** no parallel compute span exists to compute a ceiling from. The real
  ceiling is the throughput of one per-repo serialization point ≈ **98k appends/s when
  unstalled** (VERIFIED-live). The only pathological breach is a holder doing slow non-DB work
  (git) under the chain head — which is F2/F3, not a thread-count problem.
- **PMU / HITM / MESI (GT3):** the wrong instrument. The daemon is lock/IO/subprocess-bound,
  not cache-line-bound; the report correctly demotes allocation hooks (REPORT §4 "Demoted").

**Reproduced vs deferred (ground-truth repro custody):**
- REPRODUCED (read-only): the 98.6% `supervisor.progress` dominance (13,452,303 / 13,637,567 =
  98.64%); the **98,008-appends-in-one-second** peak to the digit; the blob size distribution
  (p50=8072, p99=19666, max=20206 bytes — falsifies F6 "unbounded"); `run.integrate` = 5 calls
  / 11 days, **all denied before withTx** (falsifies F5 "live bottleneck"); `worktree.gc` = 8
  calls / 4 days (falsifies F4 "highest blast radius"); 0 deadlock-classed denials
  (corroborates R2); R-1's `NRestarts=0` / zero panics in journal.
- DEFERRED SAFELY: **Repro A** (40P01) and **Repro B** (57014 convoy) were *not* run — Repro B
  has no scaffolding, and the HARD SAFETY rule forbids writing to the live `striatum_daemon` or
  running PG-gated tests against it. The convoy firing stays **PREDICTED-FROM-SOURCE /
  UNDECIDABLE-until-reproduced** for F1/F2/F3/F4/F5.

**Citation-drift notes (do not falsify; will mislead a literal reader):** (1) every
`recovery.go:NNN` in REPORT.md means `go/pkg/mutations/recovery.go`, **not**
`go/pkg/recovery/recovery.go` (the latter does not exist; `pkg/recovery` holds only
`scheduler.go` + `sweep.go`). Line numbers resolve once the `mutations/` prefix is supplied.
(2) **`mutations.go` line numbers in this review are anchored to `d88518a5`**; the mid-review
`f2d35c4e` merge inserted `withTxRetryOnTransientLoad` (~37 lines) around line 550, so cites
below it shift on current main (e.g. the RFC 0104 `lockRun`-first invariant comment moved
`:556 → :587`; the `withTxRetryOnDeadlock` body `:503-523 → ~:540-560`). Same for `run.go`
(the non-CAS branch-create exec is at `:848`, not the `:841` cited in places). The constructs
are unchanged; only the offsets drifted.

---

## C. THE CONCURRENCY FINDINGS LEDGER

Verdicts are the FINAL (post-skeptic) calls — where the verifier corrected the triager, the
correction stands. Class uses the rubric's determinism gate. Every cell is mechanically
derivable; the verdict follows from them.

| Finding (symbol / line / lock) | Class | Provenance complete? | Benign-by-design? | Discriminating witness (invariant violated) | FINAL verdict | Severity | Cheapest disproof attempted |
|---|---|---|---|---|---|---|---|
| **F1** chain-head `FOR UPDATE` · `0004_phase2_events.sql:121-124` · per-repo singleton `repo_event_chain_heads` | PREDICTED-FROM-SOURCE (convoy); MEASURED (flood) | Yes (struct); convoy unreproduced | The **lock-acquire side** yes (hash-chain single-writer); the convoy substrate no | A txn must NOT hold the chain head / `lockRunForJob` across an unbounded off-CPU git subprocess. Flood path **honors** it (DB-only µs hold, supervision.go is git-free); git-in-txn holders **violate** it | **REAL** (substrate VERIFIED; convoy UNDECIDABLE-until-reproduced) | high | Hold-time × frequency split: 98,008 acq/s ⇒ ~10.2 µs/holder ⇒ lock is uncontended at steady state. Flood does **not** convoy itself → F1 is amplifier, F2/F3 are root |
| **F2** `work.complete` per-run-lock convoy · `lifecycle.go:1118/1137`; git `:1181/:1188/:1235` · `lockRunForJob` | PREDICTED-FROM-SOURCE | Yes (struct); convoy unreproduced | No | Same #198 no-IO-under-lock invariant; `work.complete` holds `lockRunForJob` across 3 unbounded git side-effects (porter add+commit, source-publish, anchor CAS) | **REAL** | medium | Live: 0/1252 `work.complete` rows carry a 57014/convoy denial; same-run complete gaps p50≈250 s. Structure real; convoy MISDIAGNOSED-as-MEASURED |
| **F3** `run.prepare` git-in-txn · `run.go:27` (**no advisory lock**), git `:921/:931`, append `:1056/:1063` | PREDICTED-FROM-SOURCE | Yes (struct); convoy unreproduced | No | git (`rev-parse`/`branch`) runs inside the run.prepare txn holding runs/jobs row locks; its tail chain-head appends queue on a foreign-held head until a 57014. At the build my agents read (`d88518a5`) it was an unwrapped `withTx` → unswallowed → `exit_code=10`; **the tree advanced to `f2d35c4e` mid-review** and run.prepare now wraps `runPrepare` in `withTxRetryOnTransientLoad` (run.go:34, the #355 victim-side mitigation), so it bounded-retries the 57014 (4 attempts) and surfaces a clean `daemon_under_load` only on exhaustion | **REAL** (victim, not holder; victim-side now mitigated on main) | medium | Ordering proof: git (`:921/:931`) strictly precedes both appends (`:1056/:1063`) → run.prepare is a victim, not the convoy source |
| **F4** `worktree.gc` repo-wide `lockRepo` × N worktrees · `worktree.go:548`, git `:580/:615/:634` | PREDICTED-FROM-SOURCE | Yes | No (latency only) | "Highest structural blast radius, blocks EVERY run" is **FALSE**: `lockRepo` absent from event_write.go/claim.go/liveness.go, so gc cannot serialize the chain-head flood; contends only with run.start + run.integrate | **MISDIAGNOSED** | low | Live: 8 invocations / 4 days, operator-only, no auto-caller. Cannot block the flood (different lock object) |
| **F5** `run.integrate` repo-wide `lockRepo` across git plumbing · `integrate.go:48`, git `:87/:117/:147` | PREDICTED-FROM-SOURCE | Yes | No (the serialization is by-design; the unbounded hold is the latent defect) | Repo-wide advisory lock held across unbounded off-PG git with no child timeout; "overlaps the busiest moment and contends" is **FALSE** | **MISDIAGNOSED** (latent structural defect, freq=0) | low | Live: 5 calls / 11 days, **all denied before withTx**; 0 `run.integrated` events ever → lock acquired by integrate **0 times** in production |
| **F6** `artifact.publish` S3 `PutBytes` under `lockRunForJob` · `artifact.go:75/76/314` | PREDICTED-FROM-SOURCE (conditional) | Yes | Yes | none — `lockRunForJob`-first is the intended RFC 0104 order; only the *width* of the section is the structural fact, not a correctness defect | **BENIGN** | low | Live: blob bodies p99=19.6 KB / max=20.2 KB (not "unbounded"); ZERO same-run blob-publish pairs within 5 s ever → the only contention scope observed ~zero times |
| **F7** #322 cap planner-only · `runreconcile.go:138-140` vs `claim.go:193` (`claimChosenJob`, no cap) | PREDICTED-FROM-SOURCE (policy gap) | Yes | No | "no run exceeds `max_active_jobs`" enforced only at planner launch, not as a claim-time/DB invariant; AND "operator can tell wedged from quiet" unmet (no `claimable>0 ∧ advance_gap>N` discriminator) | **REAL** (policy + legibility gap, not a race) | low | grep: claim path shells no git/IO; `lockRunForSession` is µs per-(repo,run) over pure row transitions → not on the convoy substrate. BENIGN as contention |
| **R1** sweep-suicide P0 — error path RULED OUT; panic residual (no `recover()` in go/) · `scheduler.go:56-74`; `rpc/server.go:176` | PREDICTED-FROM-SOURCE | Yes | Yes | none — a panic crash-restart violates no invariant `recover()` would preserve: PG is authoritative+append-only (panicked txn rolls back via deferred unwind), `KillMode=process` keeps lanes alive across restart | **BENIGN** | low | systemd unit = deliberate let-it-crash (`Restart=on-failure`, `RestartSec=2`, documented); live `NRestarts=0`, zero panics in journal over ~13.6M events |
| **R2** #325 deadlock 40P01 RULED OUT · `mutations.go:456/503-528/556-558`; guard test `run_lock_guard_test.go` | DETERMINISTIC (struct invariant); repro PG-gated, not run | Yes (struct) | Yes | none — `{sessions,runs}` cycle upheld-against: `lockRun` is the first statement of every per-run tx, giving an identical earlier serialization point; the only residual is an **observability gap** (no `deadlock.retry_exhausted` counter) | **BENIGN** | low | grep `retry_exhausted` = 0; live: 0 deadlock-classed denials; exhaustion bucketed silently as `invalid_transition` |
| **R3** queued→claimed / claimed→completed tails RULED OUT as daemon latency · `claim.go:264`, `lifecycle.go:1268` | MEASURED | Yes | Yes | none — both interval endpoints are state-transition events with **no daemon lock held across the gap**; the 40 h max is a parked job, not a held lock | **MISDIAGNOSED-as-daemon** (correct ruling) | none | Live reproduce: max=145,618 s (40.4 h) between two events cannot be a held lock → agent think-time, "do not instrument" stands |

---

## D. THE INVERSE CHECK — what the review MISSED

High bar, only real risks. Three classes: a serialization surface the headline omits, hazards
latent in the review's own fixes, and Go-level hazards the PG-centric review cannot see.

**D1 — The GLOBAL `audit_chain_head` singleton: a second, cross-repo serialization point the
headline omits.** `VERIFIED-from-source` (citation corrected on the final pass: the mechanism
is **not** a `SELECT … FOR UPDATE` in a Go `audit.go` — that file does not exist; it is an
`UPDATE striatumd.audit_chain_head … WHERE singleton = true` inside the SD audit function at
`go/pkg/db/sql/owner/0001_authority_phase0.sql:212`, reached via `appendMutationAudit`
(mutations.go:438), called as the final write of `withTx` at mutations.go:415). **stated:**
REPORT §1 row 1 pins the convoy on `repo_event_chain_heads`, a *per-repository* head.
**actual:** every successful mutation, as its **final** in-tx write before COMMIT, runs that
`UPDATE` against `audit_chain_head`, a **global** singleton — one row for the whole daemon
across all repos. The `UPDATE` takes a row-exclusive lock on the singleton, held from that
final write to COMMIT. **mine:** because every mutation across every repo funnels its commit
through this one row, `audit_chain_head` caps the **global** mutation-commit rate and couples
otherwise-independent repos: a single repo's flood drives the singleton at the same rate as
its per-repo head, so a second repo's mutation queues behind it even though their per-repo
event heads are disjoint. **Correction to my own first draft:** the lock is the *last* write,
so it is **not** held across the holder's full git-shell duration (that overstatement is
withdrawn) — the git-in-txn convoy (F2/F3) pins `repo_event_chain_heads` across git, while
`audit_chain_head` is pinned only across the short post-git commit tail. The real, narrower
point stands: this is a **global** serialization surface the per-repo headline never names,
and the review's proposed `lock_wait_us` column instruments only the *per-repo* chain head
(C1), **not** C2 — it would not measure this wait. No deadlock (consistently last in the
canonical order → cannot cycle). Severity: medium. A material omission, though not the
catastrophe a "held across git" framing would imply.

**D2 — The git-hoist TOCTOU (the review's headline structural fix is not behavior-preserving on
the correctness-sensitive paths).** `VERIFIED-from-source`. **stated:** REPORT §6 markets the
hoist (rows 2–6) as a pure perf subtraction mirroring the landed #198 pattern
(`recovery.go:559-565`). **actual:** the #198 precedent precomputes anchor git for **expired /
dead leases** (quiescent worktrees where last-resort reconciliation *tolerates* git-ahead-of-
DB); `work.complete` and `run.prepare` run a **live** lane on the happy path, and the in-txn
git there is not a read — it COMMITS onto the worktree (`git add -f` + commit,
artifact_durability.go:138) and advances the run-branch ref via CAS `update-ref <new>
<expected>` (worktree.go:1037/1088, integrate.go:147). **mine, split by path:**
- *Saved by CAS (shippable, SAFE-WITH-CAVEAT):* `run.integrate` (F5) and the `work.complete`
  anchor (F2) — the ref advance is already a compare-and-swap, so hoisting converts a
  silently-serialized success into a **loud** `git_commit_apply_failed` ("did mainline move
  concurrently?"), not silent corruption. But the hoist *removes* the `lockRun`/`lockRepo`
  serialization that today keeps sibling ref-advances from racing, pushing them onto the
  bounded 6-retry CAS loop (worktree.go:1092) — a **new** `git_commit_apply_failed` surface
  under the rare near-simultaneous fan-in. This is an OBSERVABLE behavior change; the review's
  "behavior-equivalent" claim is wrong.
- *Not saved by CAS (the dangerous one):* `run.prepare` branch-create (F3) — `gitEnsureBranchRef`
  is idempotent only if the branch exists; on first create it runs `git branch <name> <base>`
  (run.go:841) with **no CAS and no commit-time HEAD re-read**, and records `branch_base =
  currentGitHead` captured pre-txn. If HEAD moves between read and commit, the txn durably
  records a stale `branch_base` (consumed downstream: run.start, worktree base-branch, #299
  base-drift). **Crucial correction to the prompt premise:** `run.prepare` takes **no advisory
  lock at all** (run.go:27, VERIFIED), so this TOCTOU **already exists** in the current in-txn
  code — the hoist does not create it, but the review never declares `branch_base` is a
  blind-trust value with no validation-at-commit on either side of the hoist. **Acceptance
  criterion the review must add:** *every hoisted git ref-advance must be a CAS `update-ref`*;
  a future hoist of a non-CAS git op ships the silent corruption.

**D3 — `worktree.gc` git-hoist is over-subtraction (the prior triager's proposed fix is
UNSAFE).** `VERIFIED-from-source`. `lockRepo` is the *same* advisory lock `run.integrate`
holds across its `update-ref` and pin deletion (worktree.go:775). gc's safety gate
`worktreeHeadReachability` reads `refs/striatum/<run>/<job>` to decide whether removing a
worktree silently discards commits. Hoisting that probe **out** of `lockRepo` opens the window:
a concurrent `run.integrate`/pin-sweep deletes the durable ref between the pre-lock
"reachable=true" probe and the post-lock `worktree remove --force`, force-removing a
now-unreachable worktree — the `worktree_head_unreachable` silent-data-loss class AGENTS.md
flags as **stop-and-fix**. The `FOR UPDATE OF w` row lock does **not** protect the git refs,
and "terminal job" ≠ "terminal run". **mine:** F4 is MISDIAGNOSED as a bottleneck *and* its
proposed fix is unsafe — the cheap safe alternative if latency ever matters is a child-context
timeout on the per-worktree git **inside** `lockRepo`, or an N-worktree cap per call, both of
which keep the serialization invariant and add no TOCTOU.

**D4 — The `lock_timeout` lever is a retry-storm + a #197 re-orphan, not a clean "fail fast and
legibly".** `VERIFIED-from-source` + `VERIFIED-live`. PostgreSQL surfaces `lock_timeout`,
`statement_timeout`, AND a chain-head `FOR UPDATE` wait exceeding the pool's 60 s **all as the
same SQLSTATE 57014** with no structured field distinguishing them. (i) `isTransientDaemonLoad
Error` (mutations.go:483-494) matches 57014 unconditionally, so a finite role-level
`lock_timeout` is **reclassified benign** on the claim/await path (claim.go:1685) — fail-fast
becomes "spin the poll loop faster and hotter", not legible. (ii) On the loud paths, fail-fast
under a real convoy makes every contender fail and retry **simultaneously** → a thundering herd
re-contending the same chain head, **amplifying** the convoy. **mine:** the report's §6.4 lever
is **un-shippable as a blanket role-level GUC** until lock-wait-57014 is separable from
statement-57014 (which Postgres does not give you at the SQLSTATE level — needs a `SET LOCAL
lock_timeout` around the FOR UPDATE acquire so *its* 57014 is caught distinctly). This directly
answers the rubric's "the role-level lock_timeout IF the retry-storm objection is answered"
gate: **it is not answered** — drop the lever for now.

**D5 — Narrowing `isTransientDaemonLoadError` (REPORT §6.3) directly REGRESSES #197.**
`VERIFIED-from-source`. The swallow at claim.go:1685 exists because a load-57014 surfaced to a
lane "which models read as broken and stopped retrying, orphaning the job" (#197). The same
mechanism that keeps #355 invisible (a lock-wait 57014 reclassified benign) is the one that
keeps #197 fixed (a load 57014 reclassified benign so the lane polls again). You cannot narrow
the predicate to un-swallow lock-wait 57014 without **also** un-swallowing some genuine load
57014s (false-negative on a discriminator that does not exist at the PgError level), and each
re-orphans a job exactly as #197 described. **mine:** §6.3 is un-shippable as a one-line
predicate edit; it needs a real lock-wait discriminator first. The prior triagers correctly
**excluded §6.3 from every per-finding smallest_fix** — keep it excluded.

**D6 — `recover()`-and-continue (REPORT R-1 fix) is a correctness regression; only
latch-then-re-panic is acceptable.** `VERIFIED-from-source` (0 `recover()` in non-test go/). A
panic mid-mutation may leave in-memory state (e.g. the `rpc.Server.seenRequests`/`handshakeSeen`
dedup maps, server.go:33) poisoned; the pool already *destroys* connections with leftover txn
state (connection.go:296-300), so a recover-and-continue resumes a **sick** daemon with doctor
green — strictly worse than today's loud, state-consistent supervised restart. The only
defensible form is the rubric's latch-a-durable-`daemon_health`-row-then-re-panic (and even
that must write on a fresh connection, best-effort, and re-panic regardless, or the latch
deadlocks the panic path). **mine:** R1's panic residual is REAL-but-BENIGN; the *fix* is the
hazard.

**D7 — Go-level hazards invisible to `-race` and to the PG-centric review.** `VERIFIED-from-
source` (D7b) / `PREDICTED` (D7c):
- **D7a `rpc.Server.seenRequests` memory leak — INVESTIGATED AND WITHDRAWN (false finding,
  caught on the final verification pass).** stated (by a triage agent): `seenRequests`/
  `handshakeSeen` are unbounded maps that never delete → a monotonic leak. **actual:** FALSE.
  Both fields are `*boundedSeen` (server.go:36-37, `newBoundedSeen(defaultDedupeMaxEntries,
  defaultDedupeTTL)` at :51-52); `boundedSeen` is, by its own doc, a "size-capped, TTL-evicting
  set" (dedupe.go:20) — `defaultDedupeMaxEntries = 50000`, `defaultDedupeTTL = 10m`
  (dedupe.go:16-17) — and `Add` calls `sweepLocked` (TTL) + `enforceCapLocked` (cap)
  (dedupe.go:57/91/104). The dedup set is already bounded by design. **mine:** there is no
  leak and no recommendation; the structural comment at server.go:32 ("bounded (size cap + TTL
  eviction)") is accurate. This row is retained as an audit trail of a finding the adversarial
  pass killed rather than a hazard.
- **D7b #322 is a missing-invariant under a HELD lock, not the "check-then-act across two
  transactions" data race the report describes.** `claimNextInTx` takes `lockRunForSession`
  first (claim.go:63) keyed per-(repo,run), so concurrent claims on one run **fully serialize**;
  the candidate SELECT is `FOR UPDATE OF qm SKIP LOCKED` (claim.go:141). There is **no TOCTOU
  within a run.** The true defect: `claimChosenJob` (claim.go:193) contains **zero**
  `max_active_jobs` reference — the cap is bypassed even under the held lock. **mine:** this is
  *better* news for fixability (a deterministic missing-check reproduces every time, not a
  probabilistic race) and the safe fix is a `COUNT(in-flight)` guard **inside the already-held
  `lockRun` txn** (no new lock, no new ordering edge), gated default-allow-on-error.
- **D7c per-lane PTY master is written by ≥3 live goroutines with non-atomic 2-write prompt
  sequences** (agentloop/loop.go: io.Copy at :346, receiver `writePromptThenSubmit` at :430,
  #323 rotation watcher at :525). `os.File.Write` is atomic per syscall so `-race` is
  structurally blind (separate `write()` syscalls, no shared Go object), but prompt-A/prompt-B/
  submit-A/submit-B can interleave and corrupt what the interactive CLI ingests. Blast radius:
  one lane subprocess, never the daemon; requires a mid-run daemon restart (rotation) to
  coincide with packet delivery. Severity: low. Cheap fix: a per-lane `sync.Mutex` serializing
  the whole `writePromptThenSubmit` on `ptmx`.

**Lock-ordering ledger (declared once, reused for every fix's absence-proof) — VERIFIED-from-
source, NO CYCLE found.** Canonical total order, derived from `withTx` structure:

> **`[ lockRepo XOR lockRun* XOR lockSuperviseStart ]` → row `FOR UPDATE` locks → C1
> `repo_event_chain_heads` (inside `appendEvent`) → C2 `audit_chain_head` (inside
> `appendMutationAudit`, last) → COMMIT.**

Four adversarial cycle questions, all NEGATIVE: (1) `lockRepo` and `lockRun` **never co-occur**
in one tx — the three `lockRepo` handlers are `run.start`/`run.integrate`/`worktree.gc`, none
of which take `lockRun`; the ~30 `lockRun` handlers take no `lockRepo` (mutations.go:566-567
documents this as the actual invariant, VERIFIED). (2) C1 is acquired **only** inside
`appendEvent`, always after the advisory lock → uniformly last-but-one. (3) `lockRun`-first
breaks the `{sessions,runs}` cycle (claim takes `lockRunForSession` before sessions/runs FOR
UPDATE; complete takes `lockRunForJob` before runs→sessions). (4) C2 is the **global** singleton
(D1), consistently last → cannot cycle but is the cross-repo convoy surface. **Any fix below
that adds sync must place its lock in this order; none of my recommendations add a lock, so none
introduces a cycle.**

---

## THE SPINE

### Phase 1 — DISPROVE FIRST (cheapest falsifier wins)

This is DB-lock contention over loopback PG, not Go-goroutine/CPU-core scaling, so the rubric's
single-thread/scaling-sweep falsifier is N/A (the i5's 12 threads do not change a single-row
`FOR UPDATE`). The applicable falsifier is **hold-time × acquisition-frequency as a fraction of
wall-clock**, plus the on-CPU/off-CPU split.

- **F1 survived as REAL-but-amplifier.** Cheapest disproof *ran and succeeded* for the flood:
  the `supervisor.progress` emit path (supervision.go:79-356) holds the chain head only across
  an in-DB hash + 2 row writes — **no git/S3/subprocess in the txn** (VERIFIED) — so the
  98,008-acq/s flood is high-frequency on a briefly-held lock that **does not convoy itself**.
  The disproof *failed* for the git-in-txn holders → the convoy substrate is F2/F3.
- **F2 convoy MISDIAGNOSED-as-MEASURED; structure REAL.** Same-run consecutive complete gaps
  p50≈250 s, only 3.9% land <1 s apart; 0/1252 `work.complete` audit rows carry a 57014. The
  off-CPU cost in the lock window is git subprocess (fork/exec + object-db write), not on-CPU
  spin and not the advisory acquire.
- **F3 victim, not holder.** Ordering proof: git precedes the appends.
- **F4 / F5 MISDIAGNOSED as live bottlenecks** by the single cheapest query: `worktree.gc` 8
  calls/4 days; `run.integrate` reached `lockRepo` **0 times** in production history.
- **F6 MISDIAGNOSED**; blob bodies bounded ≤20 KB, zero same-run overlap.
- **F7 BENIGN as contention** (claim path off the git/IO substrate); REAL as a policy gap.
- **R1 / R2 / R3 are not contention findings**; the benign-by-design check (the systemd
  let-it-crash unit for R1; the lockRun-first ordering invariant for R2; the
  endpoints-are-state-transitions boundary for R3) is the discriminating witness.

**Counter-falsifier guard (against over-subtraction):** for each survivor I name the cheapest
experiment that *would* have flipped it to BENIGN and confirm it was run. F1: "show the flood
holds the lock only briefly" — RAN, succeeded for the flood (so F1's *lock* is benign), failed
for the holders (so the convoy survives as PREDICTED). F2: "find one live 57014 charged to a
sibling on the same run during a `work.complete` git window" — RAN against live audit, came back
**empty** → convoy stays PREDICTED, never laundered to MEASURED.

### Phase 2 — EVALUATE (provenance / determinism gate)

Provenance header per surviving finding is the build/regime in Section B (HEAD `d88518a5`,
i5-11400F 12t, loopback PG16, run-count: structural claims opened once each; live counts a
single 2026-06-17 snapshot at pid 479772). **Determinism gate, enforced:** the convoy firing for
F1/F2/F3/F4/F5 is **PREDICTED-FROM-SOURCE**, run-count of the convoy itself = **0**. Per the
rubric, *a permanent narrowing fix may not ship against UNDECIDABLE or a sample size of one* —
so the `lock_timeout` lever (a permanent narrowing GUC) and the §6.3 swallow-narrowing are
**blocked by the gate**, and the convoy-firing claim stays quarantined. The fixes I *do*
recommend are **lock-scope SUBTRACTIONS / observability adds**, not narrowings, justified by the
VERIFIED *structural* fact alone (git-in-lock; missing deadlock counter) — those clear the
gate. The discriminating witness for each is the lock-ordering ledger invariant in Section D;
none lacks an invariant, so none is left `ASSERTED`.

### Phase 3 — RESOLVE (subtraction-first; every change declares its new failure mode)

Forced resolution order applied per surviving finding; each sync row would have to fill every
cheaper rung with a VERIFIED reason it failed before adding sync — and **none of my
recommendations reach the add-sync rung.**

- **F1 amplifier → rung 2 (remove shared state from the hot path):** take `supervisor.progress`
  off the hash chain. *Why lower rungs fail:* rung 1 (delete the writer/thread) fails — the
  chain single-writer is load-bearing for events that must be chained; rung 3 (shard the head)
  fails — a hash chain is inherently single-writer per repo, sharding breaks contiguity. Rung 2
  *succeeds specifically for `supervisor.progress`* because it is **never read as provenance**
  (VERIFIED: emitted as `"supervisor."+event.EventType` at supervision.go:354, gated by
  `progressIsMeaningful` at :320; **independently re-confirmed on the final pass that no reader
  consumes it** — the dashboard's `enrichSupervisorProgress` reads the `process_supervisors`
  liveness/pointer tables, not the event chain, and no read query in `go/pkg/reads` filters
  `event_type='supervisor.progress'`) and `assertEventChainLinear`
  ignores `event_type` (0004:22-23). **New failure mode:** liveness chatter loses
  tamper-evidence + contiguity. **Absence-of-harm proof:** D028 already classes it as
  volume/timing evidence, not durable provenance; the verifier checks linkage, not which
  event_types are present, so removing a never-read type cannot break linearity. **Consistency
  cost declared:** liveness chatter becomes "observed, not chain-anchored."
- **F1 convoy root / F2 / F3 → rung 5 (restructure: shrink the txn to hold no subprocess) via
  the #198 pre-compute-oracle pattern.** *Why lower rungs fail:* no second thread to delete
  (one synchronous RPC); the chain-head singleton and the run/repo locks are load-bearing
  (removing them re-opens #325/#198/run-isolation). **New failure mode (the load-bearing
  caveat):** the git-hoist removes the lock that today serializes sibling git ref-advances →
  TOCTOU (Section D2). **Absence-of-silent-corruption proof:** the ref advance is already a CAS
  `update-ref <new> <expected>` for integrate/anchor (a stale precompute fails the CAS *loudly*
  as `git_commit_apply_failed`, not last-writer-wins). **This proof DOES NOT hold for
  `run.prepare` branch-create** (no CAS, run.go:841) → that hoist is gated behind keeping the
  branch-create in-txn or adding a commit-time HEAD re-validation.
- **F4 → no fix; reject the proposed hoist (UNSAFE per D3).** If latency ever bites: child-
  context timeout on per-worktree git *inside* `lockRepo` (keeps serialization, no TOCTOU).
- **F5 → drop (freq=0).** If integrate ever becomes hot: hoist the read-only `rev-parse`/
  `merge-tree`/`commit-tree` before `lockRepo`, keep only the CAS `update-ref` inside; new
  observable failure = a loud retryable `git_commit_apply_failed` on a genuine concurrent-
  integrate race; absence-proof = the existing 3-arg CAS.
- **F6 → drop;** fold opportunistically into the F2/F3 hoist campaign if it lands (pre-tx
  `PutBytes`). New failure mode = a harmless content-addressed blob orphan (idempotent re-put,
  invisible to every reader). No standalone change.
- **F7 → rung "read-side latch" (observation, not a lock):** extend the sweep cursor
  `last_result_json` (sweep.go:201-218, written **outside** any lock) with
  `{claimable_job_count, last_lane_advanced_at}` → the `claimable>0 ∧ advance_gap>N` discriminator.
  **New failure mode: none** (read-only, no predicate added to `claimChosenJob`, no lock scope).
  The optional in-txn cap guard is the add-sync rung and is **parked** behind D208's
  revisit-trigger; if ever shipped it must default-allow-on-error.
- **R1 → rung 1 (do nothing; the sync is not load-bearing).** systemd is already the
  supervisor; `recover()`-and-continue is a correctness regression (D6).
- **R2 → no sync change** (the lock is correct + guard-tested); residual is an observability
  counter (`deadlock.retry_exhausted`), implemented **in-process (expvar/log), NOT a durable
  appendEvent** — a durable event would open a fresh txn taking the chain head on the cold
  post-failure path, re-introducing the very contention #355 is about. New failure mode of the
  in-process counter: ~nil (cold path, post-rollback, no lock held).
- **R3 → delete the instrument (rung 0).** No fix; "do not instrument the gross span" stands.

---

## RECOMMENDATIONS

Only changes I would personally make, ordered by **safety / reversibility** (not impact, not
elegance). The cheapest reversible wins ship first; the structural correctness-sensitive change
is gated last. BENIGN/MISDIAGNOSED/UNDECIDABLE findings get a one-line drop, not a row.

| # | Finding | Smallest safe change | Why subtraction failed (if adding sync) | New risk + absence-proof | Verification plan | Blast radius | Effort |
|---|---|---|---|---|---|---|---|
| **1** | R2 residual | Emit `deadlock.retry_exhausted` at mutations.go:523 **as an in-process expvar/log counter, not a durable event**; extend `TestPerRunHandlersTakeLockRunFirst` to cover any new per-run handler | n/a — pure observability, no sync added | Risk: a durable-event impl would take the chain head on the cold path (re-#355). Absence-proof: implement as expvar/log only; the failing tx has already rolled back at :523, so the counter holds no lock | Confirm counter increments under Repro A; static guard test compiles + flags a synthetic mis-ordered handler | nil (cold post-rollback path) | XS |
| **2** | R1 panic residual | Observability-only deferred `recover()` on the rpc/server.go:176 dispatch goroutine that logs panic+stack then **immediately re-panics** (never swallows) | n/a — no control-flow change, no sync; let-it-crash preserved | Risk: ~nil (byte-identical behavior + one journal line naming the failed RPC). Absence-proof: `recover()` is immediately followed by `panic(r)` — no execution continues, no invariant newly trusted | Inject a handler panic in a scratch daemon; assert journal names the method and the process still exits non-78 → systemd restarts | nil (re-panics) | XS |
| **3** | F7 (legibility half) | Extend sweep cursor `last_result_json` (sweep.go:201-218, outside the lock) with `{claimable_job_count, last_lane_advanced_at}`; add the `claimable>0 ∧ advance_gap>N` doctor predicate | n/a — read-side latch, no lock, no predicate added to claim | Risk: none (read-only, changes no control flow). Absence-proof: no lock scope, no `claimChosenJob` edit | Drive Repro D; assert the latch reads WEDGED with a parked lane and quiet with claimable=0 | nil | S |
| **4** | F1 amplifier | Route `supervisor.progress` (98.6% of events) to an unchained liveness store / chain-exempt class | rung 2 (remove shared state from hot path); rung 1/3 fail (single-writer load-bearing; sharding breaks contiguity) | Risk: chatter loses tamper-evidence + contiguity. Absence-proof: no production consumer reads it (VERIFIED emit-only); verifier ignores `event_type`; D028 classes it non-durable | Assert chain still verifies after the type is unchained; assert chain-head acquisitions/sec drop by ~98% in a replay | medium (removes 98% of chain-head churn) | M |
| **5** | F2 / F3 (git-hoist) — **GATED** | Hoist git out of the lock-holding txn (#198 pre-compute oracle) — **for F2 anchor: keep the CAS `update-ref` and add a behavioral witness; for F3 branch-create: keep it in-txn OR add commit-time HEAD re-validation** | rung 5 (restructure); no thread to delete, locks load-bearing (#325/#198/isolation) | Risk: TOCTOU — removes the serialization that today keeps sibling ref-advances from racing → new `git_commit_apply_failed` surface; F3 records stale `branch_base` with no CAS. Absence-proof holds ONLY where the ref-advance is CAS (integrate/anchor), NOT for run.prepare branch-create | **Build Repro B + land `events.lock_wait_us` FIRST.** Then a witness: ≥3 simultaneous sibling completes on one run all reach the run branch (#290 reachability) without spuriously exhausting CAS. Do NOT advertise as observable-equivalent | high (run-branch provenance) | L |

**Dropped (one-line reasons, no recommendation):**
- **F1 (as "the lock is the bottleneck"):** MISDIAGNOSED — the lock sustains 98k/s; the holder
  is the defect. Real fix lives in #4/#5 above.
- **F4 `worktree.gc` hoist:** MISDIAGNOSED bottleneck **and** the proposed hoist is UNSAFE
  (D3 silent-data-loss TOCTOU). If latency ever bites: child-ctx timeout *inside* `lockRepo`.
- **F5 `run.integrate`:** MISDIAGNOSED — `lockRepo` acquired by integrate **0 times** in
  production. Drop; revisit only if it becomes hot.
- **F6 `artifact.publish`:** BENIGN — bounded ≤20 KB, zero same-run overlap. Fold into #5
  opportunistically, no standalone change.
- **D7a `seenRequests` "leak":** WITHDRAWN — the field is already a `boundedSeen` (50k cap +
  10 min TTL, dedupe.go); the leak was a triage-agent misread, killed on the verification pass.
- **R3 tails:** MISDIAGNOSED-as-daemon (correct ruling) — agent think-time, do not instrument.
- **REPORT §6.3 (narrow the 57014 swallow):** **REJECTED** — re-orphans jobs (#197 regression,
  D5). No PgError-level lock-vs-statement discriminator exists.
- **REPORT §6.4 (role-level `lock_timeout`):** **REJECTED for now** — retry-storm + benign-
  reclassification, the retry-storm objection is **not** answered (D4). Blocked by the
  determinism gate (permanent narrowing vs an unreproduced convoy).
- **REPORT §6.1/§6.2 (`lock_wait_us` column + doctor scan):** the **column** is SAFE and is the
  right *prerequisite* for #5 (times the wait, not the hold — label it a per-waiter wait gauge,
  not "hold-time", and note it misses the C2 global head per D1). The **doctor scan** is NOT
  free as written — `lock_wait_us` is unindexed, §5 forbids a new index, so it seq/range-scans
  ~13.3M live rows inside `HandleDoctor` against the append flood; ship only if hard-scoped to
  a recent `created_at` window within one `run_id` using the existing index, with an explicit
  `LIMIT`. Not a standalone recommendation until #5 is in flight.

---

## VERIFICATION PLAN (mandatory per fix)

- **#1 / #2 (counters):** scratch daemon only; assert the signal fires and the process still
  exits non-78 → systemd restarts. No PG-gated test against `striatum_daemon`. Holds at the
  next order of magnitude trivially (cold paths, O(1)).
- **#3 (latch):** Repro D for the latch; assert it reads WEDGED with a parked lane and quiet
  with claimable=0. Holds at scale — a read-side operation untouched by lane count.
- **#4 (supervisor.progress off-chain):** replay the 98k/s burst against a test cluster; assert
  (a) the event chain still verifies (`assertEventChainLinear` green) and (b) chain-head
  acquisitions/sec drop ~98%. **Argue at the next order of magnitude:** a multi-repo concurrent
  burst contends on *different* chain-head rows (no cross-repo lock), so removing the dominant
  single-repo flood is strictly monotone-good; the residual risk is multiple *slow holders* on
  the *same* repo, which #5 addresses.
- **#5 (git-hoist) — the only fix that must clear the full bar before merge:** (a) **build Repro
  B** (the #355 convoy) — it has no scaffolding today; (b) land `events.lock_wait_us` to make
  the convoy a measured quantity and to set the doctor threshold; (c) a behavioral-equivalence
  witness for N≥3 simultaneous fan-in completes reaching the run branch without spurious CAS
  exhaustion; (d) explicitly verify the F3 branch-create path keeps its in-txn placement or
  re-validates HEAD at commit. **Per the rubric, this permanent-restructure fix may not ship on
  a sample size of one** — Repro B is the gate. Verify at the next order of magnitude by driving
  the fan-in width past the worst observed value and confirming the 6-retry CAS ceiling holds (or
  make `maxAttempts` adaptive).

---

## OPEN QUESTIONS

1. **Production load + topology.** All live counts are a single light-load snapshot at pid
   479772 on 2026-06-17, not the convoy regime. The worst-case chain-head hold time when a
   git-in-txn holder stalls is **unmeasured** — the 98k/s burst proves the lock drains fast at
   *steady state*, not under a parked holder.
2. **Is the parallelism load-bearing?** Yes for the *lanes* (fan-in completes on one run are the
   product), but there is **no CPU-parallel span** to tune — so "right-sized" is a statement
   about lock-hold hygiene, not thread count. If a future workload needs >1 job per lane (D208's
   revisit-trigger), the F7 cap-guard moves from parked to required.
3. **The deferred PG repros.** Repro A (40P01) and Repro B (57014 convoy) were **not** run
   (Repro B unbuilt; HARD SAFETY forbids writing to the live DB). The convoy firing is
   UNDECIDABLE-until-reproduced; F1/F2/F3/F4/F5 blast-radius stays PREDICTED-FROM-SOURCE.
4. **Effective lock-wait timeout (60 s vs 600 s).** VERIFIED-from-source by precedence (pool
   RuntimeParam shadows the role floor), but not from a captured cross-backend GUC read
   (PostgreSQL has no cross-backend GUC read). One rung below a captured reading; the proposed
   `lock_wait_us` column would settle it empirically.
5. **C2 `audit_chain_head` cross-repo convoy (D1).** Its blast radius under a real git-in-txn
   hold is unmeasured and the proposed `lock_wait_us` column does **not** instrument it. Needs
   either a second column on the audit append or the read-side `pg_stat_activity` sampler.
6. **An elegant rewrite deliberately parked:** a PgError-level lock-wait-vs-statement-timeout
   discriminator (a `SET LOCAL lock_timeout` wrapping each `FOR UPDATE` acquire so its 57014 is
   caught distinctly) — this is the prerequisite that would *unblock* both the §6.3 swallow-
   narrowing and the §6.4 `lock_timeout` lever. Parked as an Open Question, not a recommendation,
   until daylight and a human are watching.

— concurrency-engineer-opus-4-8-001, 2026-06-17
