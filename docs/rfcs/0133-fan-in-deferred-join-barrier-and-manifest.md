# RFC 0133: Fan-in deferred join barrier and join manifest

Status: accepted / implemented (D213; folded into RFC 0135 P1; barrier_assembly dispatcher + staging-at-completion wired in shadow behind `STRIATUM_BARRIER_FANIN`, D246/#354)
Date: 2026-06-17
author: proposer-claude-opus-4-8-001
Context:
- GitHub [#319](https://github.com/halbritt/striatum/issues/319) — "graduate
  fan-in to a deferred post-completion join barrier + join manifest", kept open
  as a tracked enhancement (not a bug fix). This RFC is that graduation.
- GitHub [#290](https://github.com/halbritt/striatum/issues/290) (closed) and
  Decision [D206](../decisions/decision-log.md) — the **already-shipped**
  per-completion fan-in integration. A non-fast-forwardable sibling is merged into
  the run branch via an object-DB content merge (`fanInIntegrateRunBranch` in
  `go/pkg/mutations/worktree.go`), guarded by the `fanin_sibling_unintegrated`
  doctor warning (`go/pkg/reads/worktree_refs.go:85`). **The stranding bug is
  fixed.** This RFC graduates the per-completion merge to the more robust barrier
  form; it does not re-fix a live bug.
- Decisions [D208](../decisions/decision-log.md) / [D210](../decisions/decision-log.md)
  — the triage waves that named this graduation in their "Revisit Trigger".
- The #290 design synthesis under
  `docs/campaigns/issue-290-parallel-fanin-design/` (prompts/roles/workflow.json),
  which named the join barrier (its *spine*) and the join manifest (its
  *legibility surface*) as the deferred pieces.
- [RFC 0108](0108-parallel-independent-runs.md) — `run.integrate`'s
  `merge-tree → commit-tree → CAS update-ref` plumbing
  (`go/pkg/mutations/integrate.go`), reused here.
- [RFC 0117](0117-worktree-branch-ref-safety.md) — the attempt-namespaced
  `refs/striatum/<run>/<job>/<attempt>` pin-ref shape this RFC stages on top of.
- [RFC 0118](0118-gate-run-completion-on-attested-provenance.md) — the provenance
  gate and the doctor integrity invariants (`job_completed_without_anchor`,
  `worktree_head_unreachable`) the barrier must keep green.
- Prior art in source: `go/pkg/mutations/worktree.go`
  (`fanInIntegrateRunBranch`, `anchorWorktreeCommitStack`, attempt pin refs),
  `go/pkg/mutations/integrate.go` (`mergeTreeWriteTree`, CAS `update-ref`),
  `go/pkg/reads/run_graph.go` (`graphDependencyEdges`, `graphLatestJobs`),
  `go/pkg/reads/worktree_refs.go` (`doctorWorktreeRefSafety`),
  `go/pkg/artifactcontracts/placement.go`,
  `go/pkg/db/sql/0005_repo_local_workflow_state.sql` (the `jobs` table, its
  `job_type` CHECK, and the `UNIQUE (repository_id, run_id, workflow_job_id,
  attempt)` constraint).

## Problem (framed as a graduation, not a bug)

Today each fan-in sibling integrates into the run branch **at its own
completion** (D206). This is correct and deployed. But it has three costs the
#290 synthesis flagged as worth retiring:

1. **History is order-dependent.** N siblings produce N merges into the run
   branch, in whatever completion order happened, instead of one deterministic
   assembly. The integration story is harder to read and harder for doctor to
   assert as a single invariant.
2. **Linear and fan-in are two code paths.** The fast-forward fast-path
   (`worktree.go` FF branch) and the fan-in content merge
   (`fanInIntegrateRunBranch`) are separate, each with its own edge cases.
3. **No machine-readable record of what was joined.** When several siblings land,
   there is no single artifact stating *which* siblings, *which* commits, and —
   critically — *which attempt* of each contributed.

The synthesis's spine was: freeze the run-branch tip at the fan-out point,
complete each sibling to a daemon-owned **staging ref**, block on a DAG-derived
**join barrier** until every declared sibling is complete-and-staged, then do
**one** deterministic assembly in canonical `workflow_job_id` order. Its wildcard:
"delete fast-forward entirely; make fan-in the only way the run branch advances"
— so the N=1 linear case becomes the trivial 1-sibling join and both paths
collapse into one mechanism with one doctor invariant.

**The load-bearing trap (synthesis trap #1).** The barrier predicate must
evaluate each in-edge's **live attempt**, never count staged refs per job. Under
requeue / resume / `complete-stalled`, a stale attempt's ref must not satisfy the
barrier and re-strand the requeued real output — *"the original stranding bug
reborn behind a successful join."*

## The structural fact that kills the trap

Grounding against the schema: there is **no `current_attempt_id` column**. The
live attempt is the inline `striatumd.jobs.attempt` integer, and the seat is keyed
by `UNIQUE (repository_id, run_id, workflow_job_id, attempt)`. Staging refs are
already attempt-namespaced (RFC 0117). Therefore the barrier predicate is a
**JOIN on the live attempt**:

```
staging.attempt = jobs.attempt  AND  jobs.state = 'completed'
```

A requeue/resume/complete-stalled bumps `jobs.attempt` to a strictly-new value
(the UNIQUE key makes the prior attempt a distinct historical row), so a stale
attempt's staging ref no longer satisfies `staging.attempt = jobs.attempt` and is
**structurally invisible** to the barrier — no filtering, no ref counting, no
cron. The trap is the predicate expressed as a checkable query shape.

## Goals

1. One deterministic assembly per fan-in group, in canonical `workflow_job_id`
   order, off a frozen base — replacing N order-dependent merges.
2. A barrier predicate that is **provably** keyed on the live attempt, not on
   ref existence/count.
3. A first-class, machine-readable **join manifest** recording siblings,
   commits, paths, and **attempt** of each.
4. Unify N=1 (linear) and N>1 (fan-in) into a single code path with a single
   doctor invariant.
5. A **named refusal state** so a permanently-absent in-edge produces a legible
   blocked manifest instead of an infinite silent wait.
6. Crash-recoverable assembly that re-runs as an ordinary recovery, never a
   hand-finished merge (honoring the "do not paste over a broken runner"
   boundary).

## Non-Goals

- **Semantic netting** of the joined content. The manifest is provenance only —
  what was folded, never an understanding of it. Semantic merge is a downstream
  job's concern (explicit synthesis non-goal).
- Overlapping write scopes. Parallel fan-in lanes write disjoint subtrees
  (`parallelism.require_disjoint_write_scopes`); an overlap is surfaced LOUDLY
  (the D206 `git_commit_apply_failed` behavior), never silently last-writer-wins.
- Replacing PostgreSQL as authoritative live state with git refs. Refs are
  daemon-owned scratch and a *witness*; the barrier predicate runs against PG.

## Proposed design (three slices; manifest-first is the safe entry)

### Slice 1 — The join manifest (lowest risk, can land first)

A `join_manifest.v1` front-matter artifact, registered in
`artifactcontracts/placement.go` with `git_publication` placement and the
publisher's exit-6 schema guard. It is emitted as the **sole** output of a
barrier fire (provenance only): for each sibling
`{ workflow_job_id, attempt, staging_ref, commit_sha, paths_touched[], status,
superseded_attempts[], damage_code }`, plus the frozen `base_oid`, the assembled
`tree_sha`, and the barrier-evaluation snapshot/XID. It can be emitted on top of
the **existing** per-completion merge (D206) for audit value before the barrier
itself exists, which is why it is the recommended first landing.

### Slice 2 — Attempt-addressed staging refs + the live-attempt JOIN barrier

- At fan-out, write the frozen run-branch tip **once** into an append-only,
  owner-held freeze record (`fanin_freeze_points`: `frozen_tip_sha`,
  `declared_sibling_job_ids[]`), with `SELECT, INSERT` only — no UPDATE/DELETE
  grant, plus a `BEFORE UPDATE OR DELETE` trigger — so the base is structurally
  immutable and auditable.
- Each sibling completes through the existing `work.complete` path but, instead
  of merging into the run branch, cuts a staging ref
  `refs/striatum/staged/<run>/<job>/<attempt>` at its worktree HEAD. The `staged/`
  verb prefix keeps these out of the integrate sweep's `refs/striatum/<run>/`
  glob, and the attempt suffix makes a stale attempt's ref structurally distinct.
  The staging write is **co-transactional** with the attempt/state update.
- The barrier is one downstream job whose in-edges are existing
  `job_dependencies` rows. Readiness is the single set-difference query above:
  per in-edge take the live attempt (`MAX(attempt) WHERE state='completed'`) and
  require a staging row whose embedded attempt **equals** it. Mint the predicate
  **once** as an exported fragment + named SQL function
  (mirroring the `ExpiredLeaseStillStalePredicate` named-fragment precedent), with
  a build-failing static guard against any bare `COUNT(*)`-of-refs shape.

**Defenses for the trap and the two-store seam** (each from the adversarial
exploration):

- **Attempt in the ref name** (structural) — a stale attempt's ref is unreachable
  under the current attempt token with no PG lookup.
- **Attempt + job id embedded in the staged commit message/note** — the barrier
  cross-validates the ref against PG, closing the git/PG divergence after a
  crash-restart where a ref exists but PG does not recognize that attempt.
- **Requeue tombstones the prior staging ref** into `refs/striatum/voided/…` (ties
  to #306 git-retain), so recovery can *explain* why a ref was excluded.
- **`recovery/`-prefixed refs are excluded by the predicate** — a
  `complete-stalled` recovery ref cannot silently enter the canonical set.
- **A `barrier_epoch` counter** on the join job (incremented on every
  requeue/resume) embedded in the staged-ref path makes stale-epoch refs invisible
  by path-prefix mismatch — composable with attempt-in-name.
- **Merge-base contamination check** — assert `git merge-base <frozen> <staged> ==
  <frozen>`; a staged ref descended from the *evolved* branch (base drift) was
  originally refused outright. (Caveat: this proves topological ancestry, not content
  provenance — see Risks.) **Implemented (#352 + #353):** a non-descendant staged ref
  is now *classified* (`classifyContributionBase`) into a **recoverable base drift**
  (recovered, see Risks "Base drift") and a **contaminated base** (still refused with
  `barrier_smuggled_content`); the content-provenance caveat is closed by the
  per-commit tree-provenance walk, anchored at the frozen tip for a direct descendant
  and at the merge-base for a recovered drift.
- **`pg_advisory_xact_lock(run_id, barrier_id)`** held across barrier eval +
  downstream job creation serializes concurrent fires; the second fire sees the
  created job and exits idempotently.
- **Quarantine (#311 P0) as a first-class TERMINAL in-edge state** in the barrier
  predicate: "every in-edge is complete+staged **or** quarantined" fires the
  barrier with a recorded gap in the manifest, so a quarantined sibling does not
  deadlock the barrier forever.

### Slice 3 — Assembly as a first-class recoverable job (the wildcard collapse)

Promote assembly to a `barrier_assembly` job type backed by a `barrier_state`
row (`sealed → assembling → committed | failed`). Assembly =
`merge-tree --write-tree` to fold staged shas in `workflow_job_id` order,
`commit-tree` onto the frozen tip, CAS `update-ref` (the exact plumbing at
`integrate.go:147` / `worktree.go`). A mid-assembly crash leaves the row
`assembling`; the recovery sweep reclaims it as an ordinary job failure —
idempotent because re-running over unchanged staged refs + frozen tip is
deterministic.

**The load-bearing risk of this slice:** the PG state transition and the git CAS
cannot share one transaction (two stores, no 2PC). If the daemon dies *after*
`update-ref` but *before* `state → committed`, the branch is physically advanced
while PG reads `assembling`; recovery re-runs and the second CAS is **rejected**
(the old-sha no longer matches) → wedge to `needs_operator`, unless the assembly
job treats *"CAS rejected because the tip already equals my intended commit"* as
success (read the live tip back, prove its tree equals the recomputed merge-tree
output, mark `committed`). That recompute-and-recognize-your-own-commit
determinism leans on `merge-tree` being byte-stable across git versions.

**Mitigation that removes the byte-stability dependency: two-phase journaling.**
Write `target_commit_sha` + `tree_sha` to PG **before** the git CAS, so recovery
compares to *recorded intent* rather than recomputing. This is the recommended
form.

Routing **N=1** through the same `barrier_assembly` path (a one-in-edge barrier
whose merge-tree is a trivial fast-forward) delivers the wildcard — one code
path, one doctor invariant — and makes the freeze/merge-base/CAS machinery run on
every common run so it can never rot from disuse.

## Alternatives considered (and why they are traps)

- **Countdown latch / `barrier_weight` per-job decrement.** A per-job counter
  cannot tell a live attempt from a stale one — it *is* the "count staged refs per
  job" mechanism the load-bearing trap forbids. Elegant-looking; reintroduces the
  bug. The JOIN on `jobs.attempt` is the only safe predicate.
- **TTL / scheduled GC of stale refs.** Makes barrier correctness depend on a cron
  having run; a delayed sweep plus a same-attempt-number collision after a requeue
  gap can match stale freight. Correctness must not be timing-dependent.
- **Topological-sort assembly order.** The graph can admit multiple valid topo
  orders, breaking "one deterministic assembly". Canonical `workflow_job_id` order
  is load-bearing.
- **Partial-fire / "tolerated absence" quorum.** Contradicts the all-siblings
  invariant and silently corrupts downstream inputs. (The legitimate gap is a
  *quarantined* in-edge, recorded in the manifest — not a tolerated absence.)
- **LISTEN/NOTIFY self-assembly.** NOTIFY is best-effort, not durable across a
  restart; the barrier must be a transactional predicate, not an event.
- **Verifying the git commit *author* matches the lane.** The git author is
  forgeable; enforce at the daemon-as-sole-writer layer, not by inspecting the
  object.

## Risks

- **Two-store consistency.** Covered by co-transactional staging writes, two-phase
  journaling, the manifest-row-before-ref-write ordering, and a doctor check that
  flags any live-completed in-edge lacking a live-attempt staging ref.
- **Merge-base proves ancestry, not content.** A staged ref of `frozen_tip +
  linear commits` passes the merge-base test even if those commits cherry-picked
  evolved-tip content. Mitigation (child idea): record `frozen_tip_tree_sha` and
  assert per-commit tree provenance to close the smuggled-content hole.
  **Implemented (#352):** `assertContributionTreeProvenance` walks `frozen..staged`
  and refuses any chain commit not descending from the frozen tip
  (`barrier_smuggled_content`), sealing the recorded `frozen_tip_tree_sha` against
  the live tree first.
- **Base drift** (#299/#306): the frozen tip diverging from the live branch.
  Handle as a recorded, recoverable extra `commit-tree` parent leg, not a CAS
  wedge. **Implemented (#353, opt-in/shadow):** `classifyContributionBase`
  distinguishes a **recoverable base drift** — the staged commit does not descend
  from the frozen tip but shares a real merge-base with it and folds in no foreign
  root (the run branch legitimately evolved under a sibling's feet) — from a
  **contaminated base** (disjoint history, or an off-base foreign root the frozen
  base does not share, the #352 shape reached via a non-descendant base, still
  refused). A recoverable drift records the rebase leg
  (`base_drift_onto_sha`/`base_drift_reason` on `barrier_staged_contributions`,
  migration 0036 = the merge-base it folds against) and `assembleFaninBarrier`
  3-way-merges it onto the frozen tip with the staged commit as the **extra
  `commit-tree` parent leg**, preserving both the frozen line and the evolved-base
  line (the #299 invariant). The drift path reuses the #352 tree-provenance walk
  anchored at the merge-base, so no foreign graft can enter the recovered range.
  Regression: `TestFaninStagingRecoversBaseDrift` +
  `TestFaninStagingRefusesContaminatedBaseDrift`.
- **Job-type CHECK ownership** (see Open Questions): adding `barrier_assembly`
  touches the `jobs` table CHECK, which may be owner-held in production.

## Migration and rollout

- `join_manifest.v1` (Slice 1): `artifactcontracts` registration, **no DDL** —
  lands first.
- `fanin_freeze_points` + `barrier_state` + the staging row table (Slice 2/3): new
  runtime tables with no SQL FK to owner-held `jobs`, per D215. The freeze
  record's append-only immutability and the staging table's live-attempt identity
  are the load-bearing schema constraints; referential integrity is enforced in
  Go, and each table carries its own explicit runtime grant.
- Ship behind a workflow opt-in; existing runs keep the D206 per-completion merge
  until they declare a barrier. The barrier and the per-completion path produce
  the same final tree for any completion order — assert that equivalence in a
  fixture before flipping any workflow over.

## Doctor and legibility

- A new invariant `fanin_barrier_stale_attempt` / `barrier_tree_divergence` that
  **subsumes** `fanin_sibling_unintegrated` and the per-integration
  `worktree_head_unreachable` / `job_completed_without_anchor` checks: a positive
  hit is a runner defect, never silent corruption.
- A `BARRIER_BLOCKED` named state + `blocked_manifest` artifact when an in-edge is
  terminally unsatisfiable (`recovery_exhausted` / `needs_operator` / canceled),
  plus a read-only `barrier_status` view classifying each in-edge
  (staged-live / staged-stale / in-flight / quarantined) and the barrier age —
  the single biggest paging win, diagnosable in one query.
- `striatum join verify --run-id` re-folds the live manifest lines onto the
  recorded base in a scratch worktree and asserts the produced tree OID equals the
  run-branch tip (doctor-class).

## Test plan

- `TestFaninBarrierJoinsOnLiveAttempt` — a sibling staged at attempt 1 then
  requeued to attempt 2; the barrier must **not** fire on attempt 1's ref, and
  must fire only once attempt 2 stages. The trap, made a red test.
- `TestFaninFreezePointIsImmutable` — `UPDATE`/`DELETE` as `striatumd_rw` is
  rejected.
- A crash-mid-assembly recovery test — daemon dies between `update-ref` and
  `state → committed`; recovery recognizes its own commit (or compares to the
  journaled intent) and marks `committed` rather than wedging.
- N-sibling-all-reachable-any-order, N=1-through-the-same-path, and a barrier vs
  per-completion **same-final-tree** equivalence test.
- A static-source guard failing the build on a bare ref-`COUNT(*)` barrier shape.
- `make -C go vet lint check-tests` **uncontended**.

## Open questions

1. **Ownership of the new schema (resolved by D215/D216).** The `jobs.job_type`
   CHECK is owner-held in production, so `barrier_assembly` ships as owner
   bundle 0013. The new freeze/staging/`barrier_state` tables ship as runtime
   migrations with no SQL FK to owner-held `jobs` and with explicit grants,
   avoiding the grants-vs-bundle `25P02`-on-terminal-revoke gotcha.
2. **Run branch as a projection (the wildcard, pushed one step).** If every
   run-branch advance is a committed `barrier_state` row whose first parent is the
   prior barrier's commit, the run branch becomes a linear chain doctor can walk
   newest-first — and an out-of-band hand-commit (the cherry-pick-to-main mistake
   the boundary forbids) becomes a *chain break*, doctor-visible by construction.
   Should the run branch be demoted to a materialized **projection** of the
   barrier chain that the daemon can rebuild from PG + staging refs, so
   hand-edited state is structurally impossible to confuse for real state?
   **Resolved (D233, #351): NO — the run branch stays an AUTHORITATIVE git ref
   advanced by the barrier CAS.** The projection form buys no integrity property
   the shipped `barrier_state` two-phase journal + the `barrier_integrity` doctor
   invariant + the existing worktree-ref/artifact-anchor doctor checks do not
   already provide, while adding a second source of truth that must be kept
   consistent with the ref. The authoritative-ref form is the simpler invariant
   and is what the P1/P2/P6 CAS plumbing already builds on.
3. **Scope: one barrier, or a general primitive?** Two design branches converged
   on the observation that #319's fan-in TOCTOU, RFC 0095's stale-verdict reopen
   wedge, and RFC 0108's integrate gate are *the same bug* — a barrier that counts
   by `job_id` instead of by the `(entity, attempt)` that was sealed. Should this
   ship as one **attempt-sealed expectation barrier** primitive (sealed set +
   re-seal-on-requeue contract + sealed-set doctor diff + `BARRIER_BLOCKED`
   refusal) of which fan-in, panel-review quorum
   ([RFC 0132](0132-gating-advisory-reviews-quorum-dissent-protection.md)), RFC
   0108 integrate, and recovery quorum are all instances — one audited primitive,
   one doctor check, one chaos suite — rather than four ad-hoc predicates each
   re-discovering the stale-attempt trap? Is #319 the right scope, or the first
   caller of a primitive that should be designed once?

## Appendix — design provenance

The live-attempt-JOIN predicate, the immutable freeze record, the
assembly-as-recoverable-job + two-phase journaling, the manifest schema, the
`BARRIER_BLOCKED` refusal state, and the trap catalog were produced by a
parallel-frame divergent-ideation run (logistics, regulator, 3am-on-call,
hostile-competitor, and remove-the-load-bearing-assumption frames over isolated
branches, then convergence) grounded against the real `worktree.go` /
`integrate.go` / `run_graph.go` source and the `jobs` UNIQUE constraint. Two
independent branches reached the same `(entity, attempt)` insight and the same
unifying-primitive provocation (Open Question 3) — that cross-branch convergence
is the strongest signal in the run and the reason it is surfaced as a scoping
decision rather than an aside.
