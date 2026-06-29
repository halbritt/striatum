# RFC 0117: Per-job worktree & branch ref-safety

Status: accepted (D176)
Date: 2026-06-06
author: rfc-author-claude-opus-4.8-001
Context: RFC 0008 (opt-in per-job git worktree isolation — the unfulfilled
"automatic cleanup / collect artifacts back to provenance" promise this RFC
finishes), RFC 0108 (parallel independent runs + serialized gated
`run.integrate`, whose `merge-tree`/`commit-tree`/CAS-`update-ref` plumbing this
RFC reuses), RFC 0104 (per-run advisory lock taken first in every per-run
mutation), GH #186 (data-loss: released per-job worktree leaves the completed
job's commit stack dangling and gc-vulnerable), GH #184 (stage-2 per_job
publication needed a manual branch fast-forward repair and left the operator's
primary checkout on the run branch), GH #183 (worktree create fails with
`invalid reference` because confirmed-but-uncreated branch refs don't exist —
the subsumed stopgap), GH #171 (closed; earlier per_job publish dead-end, fixed
only the artifact-body read path); `go/pkg/mutations/worktree.go`,
`go/pkg/mutations/lifecycle.go`, `go/pkg/mutations/integrate.go`,
`go/pkg/mutations/run.go`, `go/pkg/mutations/recovery.go`,
`go/pkg/mutations/artifact.go`, `go/pkg/db/sql/0005_repo_local_workflow_state.sql`.

## Problem

This section describes the pre-implementation behavior that motivated the RFC.

A `code_change` (or any repo-write) job that runs under
`worktree_isolation: per_job` does its work — slice commits, the published
artifact's source commit — on the **detached HEAD** of a per-job git worktree.
The runner creates that worktree with `git worktree add --detach`
(`worktree.go:84`), so the commit stack the job builds is reachable from **no
named ref**: only the worktree's transient HEAD points at it.

Nothing in the lifecycle ever attaches that stack to a durable ref:

- **`work.complete` does no worktree or git integration at all.**
  `HandleCompleteWork` (`lifecycle.go:949-1073`) flips job/queue/lease state,
  records liveness, appends `job.completed`, and runs the downstream/run-complete
  cascade. It never touches the worktree, the run branch, or any git ref. The
  commit stack remains dangling at the instant the job is marked `completed`.
- **`artifact.publish` reads the body, not the commits.** The #171 fix taught
  `artifact.publish` to read the artifact *file* from the active worktree
  (`artifact.go:105-117` → `artifactSourcePath`, `artifact.go:332-336`) so the
  `content_sha256` is computed from the right bytes. But that is a **blob read**.
  It establishes no commit reachability: the slice commits that contain the work
  are still reachable from nothing.
- **Release is a bare, manual, force-remove.** `HandleWorktreeRelease`
  (`worktree.go:134-221`) runs `git worktree remove --force` (`worktree.go:171`)
  with no check that the worktree's HEAD is reachable from any ref. Release is a
  **manual verb only** — `worktree.release` has no automatic caller anywhere
  (the only callers of the method are the CLI route and the MCP registry;
  `lifecycle.go` and `recovery.go` never invoke it). So *whether* and *when* a
  completed job's worktree gets force-removed is operator-timing-dependent, which
  is exactly why #186 saw the lifecycle behave nondeterministically across
  identically-shaped jobs in one campaign.

The consequence is **silent, severe data loss (#186).** No blob store was
configured, so the daemon retained only the artifact's `content_sha256`; git's
object store held the *only* copy of the completed, reviewed-against work. When
the worktree was force-removed, its detached HEAD vanished and the whole slice
stack became dangling objects (`git fsck --lost-found` recovered tips `7795ac4`
/ `8a949a4`). A routine `git gc` aggressive prune would have destroyed them
permanently. Recovery required forensic `fsck` + date filtering +
`merge-base --is-ancestor` ancestry checks + a `content_sha256` re-authentication
of the recovered tip — improvised git surgery for work the runner was supposed
to be the custodian of.

`#184` is the milder sibling on the same fault line: the worktree was **not**
removed (it stayed alive at its detached HEAD), so the operator could
fast-forward the run branch from it by hand — but (a) the daemon never did that
fast-forward, so a full `needs_revision` cycle was burned on *publication
mechanics* rather than substance, and (b) the manual repair used `git checkout`
/ `git merge --ff-only` **in the primary checkout**, which left the operator's
primary working tree parked on the run branch instead of `main`, silently, with
the operator discovering it only when tracked-file reads returned branch-tip
content.

There is a **third place** commits orphan: stale-lease recovery
(`recovery.go:1731-1757`) marks a transferred job's worktree `abandoned` in the
DB and emits `worktree.abandoned`, but performs **no git operation** — the
detached worktree (and its commit stack) is left on disk with no ref and no
custodian, the #184 shape produced by recovery rather than by completion.

Finally, `#183` was the *create-side* corollary of the same ref-unsafety:
before this RFC, `branch.confirm` in default confirm mode recorded the confirmed
branch name but **never created the git ref**; only `--create` created a ref,
and it did so with checkout-based branch creation that moved the operator's
primary HEAD. So when the author lane ran `worktree create`,
`validatedWorktreeCreateInputs` accepted the recorded branch name, but
`git worktree add --detach <target> <base_branch>` failed with `invalid
reference` because the base ref did not exist. The lane had to create the branch
by hand at the run's recorded base. **`#183` was subsumed by this RFC's ref-only
branch creation path** (see §"Subsuming #183").

The through-line: **the daemon creates detached worktree state and ref-naming
records, but never closes the loop to a durable git ref — neither at create
(#183) nor at complete/release (#186, #184) nor at recovery — and the one path
that does move refs (`branch.confirm --create`, the #184 manual repair) does it
by moving the operator's primary `HEAD`.** RFC 0008's accepted text promised
"automatic cleanup" and "the runner manages the collection of artifacts back to
the main repository provenance area"; that half of RFC 0008 was never delivered.

## The core invariant

This RFC is the design around a single invariant, stated in English:

> **A completed repo-write job's commit stack is always reachable from a durable
> git ref before its worktree can be released, and the daemon never moves the
> operator's primary checkout HEAD.**

Two clauses, each load-bearing:

1. **Reachability-before-release.** No path — `work.complete`,
   `worktree.release`, stale-lease recovery, or run completion — may leave a
   completed (or in-flight-but-committed) job's HEAD reachable only from a
   transient worktree HEAD. A durable ref (the run branch, or a per-job pin
   under `refs/striatum/`) must contain that tip first.
2. **Hands-off the operator's checkout.** Every ref the daemon moves is moved
   by ref plumbing (`update-ref` with compare-and-swap, `git branch`,
   `commit-tree`), never by `git checkout` / `git checkout -b` / `git merge` in
   a working tree. The daemon owns *refs*, not *the operator's HEAD*. This is
   already the discipline `run.integrate` follows (`integrate.go:20-26`); this
   RFC extends it to the create and complete/release surfaces.

Everything below is mechanism in service of that invariant.

## Goals

1. At `work.complete` for a repo-write job that has an active worktree, make the
   job's worktree HEAD reachable from a durable ref: **fast-forward the run
   branch when ancestry permits, else pin `refs/striatum/<run_id>/<job_id>`** —
   atomically with the completion transaction's git side, before the job is
   reported completed.
2. Refuse a non-`--force` `worktree.release` whose HEAD is **not** an ancestor
   of any durable ref, naming the remedy; keep `--force` as the explicit
   data-losing escape, audited.
3. Make stale-lease recovery's worktree-abandon path ref-safe: pin the
   abandoned worktree's HEAD under `refs/striatum/` before (or instead of)
   abandoning it, so a transferred job's commits never orphan.
4. Make ref creation **ref-only everywhere**: `branch.confirm` (all modes that
   need a ref) and `worktree create`'s base-branch precondition use
   `git branch` / `update-ref` against the run's recorded base, never
   `git checkout -b`. Subsume #183.
5. Surface worktree ref-safety in `daemon doctor` / `worktree list`: a
   per-worktree reachability + ownership probe, extending the existing
   `worktree_path_missing_on_disk` problem family.
6. **Reuse, not reinvent.** The CAS `update-ref` at `integrate.go:136`, the
   `merge-tree`/`commit-tree` plumbing, `integrateRevParse`, `isFullGitSHA`,
   and `runGitWorktreeCommand` already exist and are proven by RFC 0108. This
   RFC adds ref-plumbing helpers in the same idiom; it introduces no new git
   strategy.

## Non-Goals

- **No durable transcript / blob mandate.** This RFC does not require a blob
  store. It makes git the *reliable* custodian of completed commits (the status
  quo intent), not a *new* persistence tier. (Per the product boundary, the
  daemon does not introduce external persistence.) A configured RFC 0072 blob
  store remains an orthogonal, additive copy of artifact *bodies*; it does not
  satisfy commit reachability and is not a substitute for this invariant.
- **No auto-merge to mainline.** Reachability is to the **run branch** (or a
  per-job pin), never to `main`/an integration target. Integration into a
  mainline stays RFC 0108's serialized, gated, human-surfaced `run.integrate`.
  This RFC strictly *feeds* `run.integrate` a complete, ref-reachable run
  branch; it never bypasses it.
- **No new core daemon method.** All invariant behavior attaches to existing verbs
  (`work.complete`, `worktree.release`, `branch.confirm`, recovery sweep,
  `worktree create`) plus doctor/list read surfaces. The follow-up cleanup
  companion (#213) adds `worktree.gc` as a guarded operator verb after the
  invariant is in place.
- **No change to write-scope / artifact-contract enforcement.** The fast-forward
  runs *after* the existing `enforceWriteScopeClean` / `verifyRequiredArtifacts`
  gates in `work.complete`; it is a strictly additional, last step.
- **No commit authorship by the daemon for the FF case.** Fast-forwarding the run
  branch moves a ref to an *existing* lane commit; the daemon authors no commit.
  The only `commit-tree` the daemon ever authors is RFC 0108's integration merge,
  which is out of scope here.

## Design

### The decision at `work.complete`

Add a final step to `HandleCompleteWork` (`lifecycle.go`), after
`verifyRequiredArtifacts` succeeds and before/around the state-flip, gated on the
job having an **active worktree** (`activeWorktreeForJob`, the same lookup
`artifact.publish` uses): **anchor the worktree HEAD to a durable ref.** Pure git
plumbing in `repoRoot`, never touching the worktree's index or any checkout:

1. Resolve `head = git rev-parse --verify <worktree_path>/HEAD` (the worktree's
   detached tip). If the worktree HEAD equals the run branch tip (no commits
   were made — e.g. an artifact-only job that wrote nothing git-tracked, or a
   no-op), there is nothing to anchor; record `anchor: none` and continue.
2. Resolve `runTip = git rev-parse --verify refs/heads/<run_branch>` (the run's
   confirmed branch; guaranteed to exist post-§"Ref-only branch creation").
3. **Fast-forward case** — `git merge-base --is-ancestor <runTip> <head>`
   succeeds (the run branch tip is an ancestor of the job HEAD; the job built
   strictly on top of the current run branch): advance the run branch by a
   **compare-and-swap** `update-ref refs/heads/<run_branch> <head> <runTip>`
   (the exact pattern at `integrate.go:136`). The CAS expected-old-value is
   `runTip`; if a concurrent job moved the run branch between the rev-parse and
   the update, the CAS fails and we fall through to the pin case (below) rather
   than clobbering. On success: `anchor: run_branch_ff`, payload carries
   `{run_branch, from: runTip, to: head}`.
4. **Pin case** — the run branch is *not* an ancestor of the job HEAD (diverged:
   a concurrent job already advanced the run branch, or the job's base was an
   older run-branch tip, or the FF-CAS lost a race): create/overwrite
   `refs/striatum/<run_id>/<job_id>` → `head` via
   `update-ref refs/striatum/<run_id>/<job_id> <head>`. This is a durable,
   namespaced ref outside `refs/heads/` (so it never appears as a branch, never
   collides with operator branches, and is trivially enumerable/sweepable). On
   success: `anchor: job_pin`, payload `{pin_ref, head}`. The divergence is
   recorded so a later synthesis/integration job (or `run.integrate`) can reach
   the pinned tip.
5. Append a `job.commits_anchored` event with the anchor kind + refs inside the
   completion transaction, **before** the lease/state flip commits — so the
   anchor is part of the same atomic completion record the rest of
   `HandleCompleteWork` writes. Git is not transactional with PG, so the order
   is **anchor-git-then-append-event-then-commit-tx**, mirroring
   `integrate.go:118-131`'s "append event before the irreversible git move…"
   reasoning, adapted: here the git move (`update-ref`) is itself idempotent and
   CAS-guarded, so a tx rollback after a successful `update-ref` leaves a
   *harmless extra durable ref* (the worst case is an un-eventful pin, which the
   doctor probe and the idempotent re-run reconcile), never lost commits.

This makes the invariant's clause 1 hold **the moment a repo-write job
completes**, independent of when/whether the worktree is later released. #186's
data-loss window closes here: by the time `work.complete` returns, the slice
stack is on the run branch (FF) or under a `refs/striatum/` pin (divergence).

> Why anchor at `work.complete` and not at `worktree.release`? Because release is
> a *manual, optional, possibly-never* verb (the #186 nondeterminism). The
> earliest deterministic moment the daemon *knows* the work is done and
> reviewed-against is completion. Anchoring there means a worktree that is never
> released, force-removed, or gc'd has still already surrendered its commits to a
> durable ref. Release becomes a pure cleanup of an *already-anchored* worktree.

### Release becomes reachability-gated

`HandleWorktreeRelease` (`worktree.go:134-221`) gains a guard before the
`git worktree remove --force`:

- Resolve the worktree HEAD. Compute reachability: HEAD is reachable iff
  `git merge-base --is-ancestor <head> <ref>` for **some** durable ref — the run
  branch, or `refs/striatum/<run_id>/<job_id>`, or (defensively) any
  `refs/striatum/<run_id>/*`. (After the `work.complete` anchor this is
  essentially always true for a completed job; the guard catches the
  *un-anchored* cases: a worktree released before completion, an abandoned
  worktree whose recovery anchor was skipped on an old binary, a future caller.)
- If reachable: force-remove as today; mark `removed`; emit `worktree.released`
  (unchanged).
- If **not** reachable and the request is not `--force`: refuse with a typed
  error (`worktree_head_unreachable`) naming the dangling HEAD sha and the
  remedy ("re-run `work.complete` to anchor, or pass `--force` to discard"). The
  worktree and its commits are left intact.
- `--force` (new optional param on `worktree.release`) keeps today's behavior:
  force-remove regardless, emitting `worktree.force_released` with the
  un-anchored HEAD recorded in the audit chain so the discard is attributable.

Reachability is computed with the same CAS/ancestor plumbing; no working tree is
touched.

### Stale-lease recovery anchors before abandoning

`recovery.go:1731-1757` (the lease-expiry worktree-abandon loop) currently marks
the worktree `abandoned` with no git side. Extend it to **anchor first**: before
the `state = 'abandoned'` UPDATE, run the same anchor step as `work.complete`
(FF-or-pin the worktree HEAD) so a transferred/abandoned job's commits are
reachable from `refs/striatum/<run_id>/<job_id>` even though the job did not
complete. The `worktree.abandoned` event payload gains the anchor ref. This
closes the recovery-path orphan (the #184 shape produced by recovery). Because
recovery runs inside the sweep transaction under the per-run lock (RFC 0104),
the same anchor-then-mark ordering and idempotency apply.

> Abandoned-worktree git *removal* (the on-disk worktree dir) stays out of scope
> here: recovery only changes DB state today, and a follow-up worktree-gc verb
> (Open Question 3) can force-remove anchored-abandoned worktrees. The invariant
> only requires *reachability*, which this provides.

### Ref-only branch creation (subsuming #183)

The daemon must be able to guarantee the run branch ref exists (so `work.complete`
can FF it and `worktree create` can base off it) **without ever moving the
operator's primary HEAD.** Replace the checkout-based paths:

- **`branch.confirm`**: pre-implementation, default `confirm` mode was
  `records_only` and created no ref (#183's root), while `--create` mode used
  checkout-based branch creation that moved primary HEAD (#184's root). Replace
  both ref-creating effects with **`git branch <name> <base>`** (or `update-ref
  refs/heads/<name> <base>` when `<name>` may already exist), where `<base>` is
  the run's recorded starting commit. `git branch` creates the ref **without
  checking it out** — primary HEAD is untouched. The decision:
  - `confirm` mode (the default, #183): create the ref at the run's recorded base
    if it does not yet exist; if it exists, validate it (or accept it). The
    response's `records_only` flag becomes **honest**: `records_only` is `true`
    only when no ref was created, `false` (with `created: true`) when this call
    created the ref. (#184 noted the older hardcoded `records_only:true` masked
    ref creation.)
  - `--create` mode: keep the create-if-absent semantics but via `git branch`,
    not `checkout -b`. The `use_current` / `strict` modes (which validate
    against the *operator's* current branch and intentionally read HEAD) are
    unchanged — they never *move* HEAD.
- **`worktree create`** (`validatedWorktreeCreateInputs`, `worktree.go:267-273`):
  if the confirmed base branch ref is missing at create time (a run confirmed
  before this RFC, or a campaign that confirmed the same name across runs without
  creating it — #183's exact reproduction), create it at the run's recorded base
  with `git branch` before the `git worktree add`, instead of failing with
  `invalid reference`. The run already records its starting commit; this is the
  remediation #183 asks for, done daemon-side and ref-only.

This makes the run branch a **real, durable ref from confirmation onward**, which
is the precondition the `work.complete` fast-forward depends on, and it removes
every daemon code path that runs `git checkout` against the operator's tree.

### Why fast-forward-or-pin (alternatives considered)

- **Always pin under `refs/striatum/`, never touch the run branch.** Rejected as
  the *primary* anchor: it keeps the run branch empty (the #186 symptom: "run
  branch still at the scaffold commit"), so every consumer — the next slice job,
  a synthesis job, `run.integrate`, the operator — must know to look under
  `refs/striatum/<run>/<job>` instead of the branch they confirmed. The natural,
  least-surprising place for a job's commits is the run branch the operator named.
  FF-when-possible keeps the run branch the single obvious head; the pin is the
  *fallback* for genuine divergence, not the default.
- **Always fast-forward, fail completion if FF is impossible.** Rejected: under
  legitimate concurrency (RFC 0108: parallel jobs on one run branch, or a
  synthesis job that already advanced the branch) FF is *expected* to be
  impossible for the later job, and failing `work.complete` would wedge the run
  on a non-error. The pin makes divergence a recorded, reachable, non-fatal
  state that a downstream synthesis/integration step resolves — which is already
  RFC 0008's "synthesis jobs remain responsible for merging outputs" division of
  labor, now with the diverged commits *reachable* instead of dangling.
- **Auto-merge diverged stacks onto the run branch with `merge-tree`.** Rejected
  for `work.complete`: that is exactly RFC 0108's `run.integrate` job —
  conflict-detecting, serialized, human-surfaced on conflict. Doing it implicitly
  at every job completion would silently auto-resolve or wedge. The pin defers
  the merge to the explicit integration step. (A future enhancement could let a
  *synthesis* job opt into a `merge-tree` of its phase's pins; Open Question 5.)
- **Anchor by checking out the run branch in the worktree and merging.**
  Rejected: violates invariant clause 2 (no checkout/merge in any working tree)
  and reintroduces #184. Ref plumbing only.

### Interaction with `run.integrate` and RFC 0108 multi-run

`run.integrate` (`integrate.go`) merges a **completed run's branch** into a
mainline target. This RFC's contract guarantees that by the time a run is
`completed`, the run branch is a real ref whose tip transitively reaches every
repo-write job's commits — directly (FF jobs) or via the pins that a
synthesis/integration phase folds in. So `integrateRevParse(runBranch)`
(`integrate.go:82`) resolves to a tip that actually contains the work, instead
of the scaffold commit #186 observed. The reused `update-ref` CAS pattern is
literally shared code (§Implementation factors the ref helpers out of
`integrate.go`).

For RFC 0108 **parallel independent runs**: per-run branches and per-job
worktrees already compose; this RFC strengthens the composition by (a) anchoring
each run's commits to *its own* run branch under the per-run lock (RFC 0104), so
two runs' completions never race on one ref, and (b) namespacing pins by
`run_id` (`refs/striatum/<run_id>/<job_id>`), so concurrent runs' pins never
collide. The doctor reachability probe (below) is naturally per-run.

### Failure modes (enumerated)

| Failure mode | Behavior under this RFC |
|---|---|
| **Diverged run branch** (concurrent job advanced it; job built on an older tip) | FF-CAS fails the `--is-ancestor` test → **pin** `refs/striatum/<run>/<job>`; commits reachable; a synthesis/integration step reconciles. No wedge, no loss. |
| **Concurrent jobs on one run branch** (RFC 0108) | First completer FFs the branch; later completers see the branch is no longer their ancestor → pin. Each tip reachable; per-run lock serializes the CAS so no lost update. |
| **FF-CAS lost a race** (branch moved between rev-parse and update-ref) | CAS exit≠0 → fall through to **pin** rather than clobber. Idempotent re-run of `work.complete` (already idempotent for completed jobs) re-evaluates. |
| **Operator deleted the run branch** before completion | `rev-parse refs/heads/<run_branch>` fails → **pin** the job HEAD (commits still saved) and emit `run_branch_missing` in the anchor event so the operator is told the branch they confirmed is gone. Completion is not wedged; the work is not lost. |
| **Operator deleted/garbage-collected a pin** | Doctor reachability probe reports `unreachable` for that worktree/job; the remedy is re-run `work.complete` (re-anchors) while the worktree exists, or `git fsck` recovery if it doesn't. The pin under `refs/striatum/` is itself gc-protected (it *is* a ref), so this only happens on an explicit operator `update-ref -d`. |
| **Worktree HEAD == run branch tip** (no commits) | `anchor: none`; nothing to do; release is unconditionally reachable. |
| **Old binary, new database** | Pre-RFC binaries don't anchor; the doctor probe flags un-anchored completed jobs; re-running `work.complete` on the new binary anchors them (idempotent). |
| **`--force` release of un-anchored work** | Allowed, but emits `worktree.force_released` recording the discarded HEAD — the only sanctioned data-loss path, fully attributable. |

### Doctor / observability surface

Extend `worktree list` and `daemon doctor` with a per-worktree **ref-safety**
block, in the idiom of the existing `worktree_path_missing_on_disk` problem code
(spec.md:1362):

- For each `active`/`abandoned` worktree row: probe `head` reachability against
  durable refs and report `{worktree_id, job_id, run_id, head, anchor:
  run_branch | job_pin | none | unreachable, anchored_ref}`.
- New doctor problem code **`worktree_head_unreachable`** when a worktree's HEAD
  is reachable from no durable ref (the #186 precondition), with remedy text
  ("re-run `work.complete` to anchor while the worktree exists; un-anchored
  commits are gc-vulnerable").
- New problem code **`job_completed_without_anchor`** when a `completed`
  repo-write job has a worktree (or a recorded worktree) whose HEAD is
  unreachable — catches the exact #186 state retroactively on adopted databases.
- `worktree list` gains an `anchor` column so the operator can see, at a glance,
  that every completed job's work is on a ref.

These are pure reads (no new persistence); they make the invariant *inspectable*,
the same way RFC 0114's `pg_read_scope` posture is probe-derived rather than
asserted.

### Subsuming #183

`#183` is being fixed in a parallel batch as a **narrow stopgap** — make
`worktree create` (or `branch.confirm`) create the missing confirmed branch so
the `invalid reference` failure stops. That stopgap is correct and should land;
this RFC **subsumes** it by making ref-only branch creation a *systemic*
property rather than a point-patch:

- The stopgap fixes the *create-time* symptom. This RFC additionally requires the
  ref be created **ref-only** (`git branch`, never `checkout -b`), so the #183
  fix does not reintroduce #184's primary-HEAD move.
- This RFC makes the same ref-creation the **precondition the `work.complete`
  fast-forward depends on**, so #183 and #186 are fixed by one coherent
  branch-ref lifecycle rather than two unrelated patches.
- At acceptance, the standalone #183 fix should be reconciled with §"Ref-only
  branch creation": if the stopgap used `git checkout -b`, it is replaced by
  `git branch`; if it already used `git branch`/`update-ref`, this RFC simply
  adopts it. Either way #183 closes as part of this lifecycle, not beside it.

## Implementation plan (gate-first, phased)

Implementation status: phases 1-5 are now implemented in source. `work.complete`
anchors active per-job worktree commits with fast-forward-or-pin and emits
`job.commits_anchored`; `worktree.release` refuses unreachable HEADs unless
`--force` records `worktree.force_released`; `branch.confirm` / `worktree
create` use ref-only branch creation; and stale-lease recovery anchors before
marking worktrees `abandoned`. `daemon doctor` and `worktree list` now surface
the ref-safety projection and flag `worktree_head_unreachable` /
`job_completed_without_anchor`. The #213 companion `worktree.gc` verb removes
only anchored on-disk worktrees for terminal jobs and reports skipped rows with
reasons.

Each phase lands alone, behind a named regression gate. **The RED gate is
written first and must fail on `origin/main` before the fix.**

**Phase 0 — ref-helper extraction + the RED gate.** Factor the ref plumbing out
of `integrate.go` into a small `mutations` helper surface
(`refReachable(ctx, repoRoot, sha, refs...)`,
`casUpdateRef(ctx, repoRoot, ref, new, expectedOld)`,
`createBranchRef(ctx, repoRoot, name, base)`, reusing `integrateGit`,
`integrateRevParse`, `isFullGitSHA`). No behavior change.
- **RED gate `TestWorktreeCompleteAnchorsCommitStack`** (pg-gated, in
  `go/pkg/mutations`): create a per-job worktree, commit a stack on its detached
  HEAD, `work.complete`, then **release** it, then assert the stack tip is an
  ancestor of *some* durable ref (`refs/heads/<run_branch>` or
  `refs/striatum/<run>/<job>`). On `origin/main` this fails (tip dangles) — the
  executable statement of #186.

**Phase 1 — anchor at `work.complete`.** Implement the FF-or-pin step
(§"The decision at `work.complete`") + `job.commits_anchored` event. Turns the
Phase-0 gate GREEN.
- Gates: `TestWorktreeCompleteFastForwardsRunBranch` (FF case advances the
  branch via CAS), `TestWorktreeCompletePinsOnDivergence` (a pre-advanced run
  branch → job HEAD pinned, branch untouched), `TestWorktreeCompleteNoopWhenNoCommits`.

**Phase 2 — reachability-gated release + `--force`.** Implement the release
guard and `worktree.force_released` audit.
- Gates: `TestReleaseRefusesUnreachableHead` (un-anchored worktree, non-force →
  `worktree_head_unreachable`, worktree intact),
  `TestForceReleaseDiscardsAndAudits`, `TestReleaseAfterCompleteSucceeds`.

**Phase 3 — ref-only branch creation (subsumes #183) + primary-HEAD safety.**
Replace `checkout -b` with `git branch`/`update-ref` in `branch.confirm` and the
`worktree create` base-branch precondition; make `records_only` honest.
- Gates: `TestBranchConfirmCreatesRefWithoutMovingHead` (confirm mode creates the
  ref at the recorded base; `git branch --show-current` of the primary checkout
  is unchanged — the #184 invariant), `TestWorktreeCreateCreatesMissingConfirmedBranch`
  (the #183 reproduction now succeeds), `TestBranchConfirmRecordsOnlyHonest`.

**Phase 4 — recovery anchors before abandon.** Anchor in the
`recovery.go:1731-1757` abandon loop.
- Gate: `TestRecoveryAnchorsBeforeAbandon` (lease-expiry transfer of a job with
  committed worktree work → tip reachable from `refs/striatum/<run>/<job>`).

**Phase 5 — doctor / `worktree list` ref-safety surface.** The probe, the two
new problem codes, the `anchor` column.
- Gates: `TestDoctorFlagsUnreachableWorktreeHead`,
  `TestWorktreeListShowsAnchor`.

Dependency order: Phase 0 → Phase 1 → (Phase 2, Phase 3, Phase 4 parallel) →
Phase 5. Each ships independently; Phases 2/3/4 each independently reduce orphan
surface.

## Acceptance Criteria

- After `work.complete` for any repo-write job that committed in its worktree,
  the worktree HEAD is an ancestor of `refs/heads/<run_branch>` (FF case) **or**
  equals `refs/striatum/<run_id>/<job_id>` (pin case); a `job.commits_anchored`
  event records which. (`TestWorktreeCompleteAnchorsCommitStack` GREEN.)
- A non-`--force` `worktree.release` of a worktree whose HEAD is reachable from
  no durable ref is refused with `worktree_head_unreachable`, leaving the
  worktree intact; `--force` discards it and records the discarded HEAD.
- No daemon code path runs `git checkout` / `git checkout -b` / `git merge`
  against the operator's primary checkout. `branch.confirm` and `worktree create`
  create refs via `git branch`/`update-ref`; the operator's
  `git branch --show-current` is invariant across both. (#184/#183 gates GREEN.)
- The #183 confirmed-but-uncreated-branch `worktree create` failure no longer
  occurs; the missing run branch ref is created at the recorded base, ref-only.
- Stale-lease recovery anchors an abandoned worktree's HEAD under
  `refs/striatum/` before marking it `abandoned`.
- `run.integrate` of a completed run resolves a run branch tip that transitively
  reaches every repo-write job's commits (FF jobs directly; diverged jobs via the
  synthesis/integration step that folds their pins).
- `daemon doctor` / `worktree list` report per-worktree anchor state and flag
  `worktree_head_unreachable` / `job_completed_without_anchor`.
- `docs/reference/spec.md` (worktree lifecycle + the new problem codes),
  `docs/rfcs/0008-...` (cross-reference: the "automatic cleanup / collect back
  to provenance" promise is now fulfilled by RFC 0117), and the decision log
  update only when the behavior lands.

## Proposed decision-log entry

> **DXXX — Per-job worktree & branch ref-safety (RFC 0117, accepted).** A
> completed repo-write job's commit stack is always reachable from a durable git
> ref before its worktree can be released, and the daemon never moves the
> operator's primary checkout HEAD. `work.complete` fast-forwards the run branch
> when ancestry permits (compare-and-swap `update-ref`, the RFC 0108
> `integrate.go` pattern) and otherwise pins `refs/striatum/<run_id>/<job_id>`;
> non-`--force` `worktree.release` refuses an unreachable HEAD; stale-lease
> recovery anchors before abandoning; `branch.confirm` and `worktree create`
> create branch refs with `git branch`/`update-ref` (never `checkout -b`),
> subsuming the standalone #183 stopgap. Closes #186 (dangling-commit data loss),
> #184 (manual FF repair + primary-checkout move), #183 (confirmed-but-uncreated
> branch ref). Fulfills RFC 0008's unimplemented "automatic cleanup / collect
> artifacts back to provenance" clause. No new daemon method, no new persistence,
> no auto-merge to mainline (integration stays RFC 0108's gated `run.integrate`).

## Open Questions

1. **Pin retention / gc of `refs/striatum/` pins.** A pinned job tip stays a
   durable ref forever unless something deletes it. Should pins be swept after
   their commits are folded into the run branch (by a synthesis step or
   `run.integrate`), or retained as permanent per-job provenance? Proposal:
   retain until run termination, then a `run.archive`/closeout step prunes pins
   whose commits are reachable from the (integrated) run branch; leave
   unreachable pins (genuinely divergent, never-integrated work) in place as the
   last copy. Decide at acceptance.
2. **Should `work.complete` anchor be best-effort or fail-closed on git error?**
   If the `update-ref` itself errors (disk full, repo corruption — not a CAS
   miss, which falls through to pin), should `work.complete` fail (job stays
   running, lease held, operator alerted) or complete with a
   `commit_anchor_failed` event and let the doctor probe + release guard catch
   it? Proposal: **fail-closed** — a job that cannot guarantee its commits are
   saved has not safely completed; the invariant is the point. Confirm.
3. **On-disk gc of anchored-abandoned worktrees.** Resolved by the #213
   companion `worktree.gc` verb, which force-removes only terminal-job
   worktrees whose HEAD is reachable from the run branch or `refs/striatum/`
   pins and reports skipped rows with reasons.
4. **Pin namespace collisions across run retries.** `run.retry_job` bumps the job
   attempt (RFC 0095) but the `job_id` is stable; should the pin be
   `refs/striatum/<run_id>/<job_id>/<attempt>` so a retried attempt's commits
   don't overwrite the prior attempt's pin? Proposal: yes, namespace by attempt
   to preserve per-attempt provenance (matching RFC 0095's attempt-scoped
   artifacts). Confirm the ref-name shape.
5. **Synthesis-job opt-in `merge-tree` of phase pins.** Should a synthesis job be
   able to declaratively fold all its phase's `refs/striatum/<run>/*` pins into
   the run branch via RFC 0108's `merge-tree` plumbing (conflict-surfacing), so a
   diverged parallel build doesn't require a manual integration? Natural future
   extension; not required for the invariant. Defer.
6. **Interaction with RFC 0072 blob store.** When a blob store *is* configured,
   the artifact body is doubly durable (blob + git). The invariant is unchanged
   (git still custodies *commits*, blobs only bodies), but doctor could
   cross-check that an anchored commit's artifact blob matches `content_sha256`.
   Worth a doctor cross-check? Low priority; note for the implementer.

## Companion issues

1. **`worktree gc` verb** — implemented by #213. It force-removes on-disk
   worktree directories whose HEAD
   is anchored and whose job is terminal, so anchored-abandoned worktrees from
   recovery and never-released completed worktrees don't accumulate over long
   campaigns (Open Question 3).
2. **Pin lifecycle / sweep policy** — implement the Open Question 1 retention
   decision (`run.archive`/closeout prunes integrated pins, retains divergent
   ones). Without it, `refs/striatum/` grows unbounded over a repo's lifetime.
3. **Attempt-namespaced pins** (Open Question 4) — if `run.retry_job` semantics
   require per-attempt pin provenance, change the pin ref shape to include
   `<attempt>` and add the regression that a retried attempt doesn't clobber the
   prior attempt's tip.
4. **Guard against `git checkout`/`merge` in daemon mutation code** — a lint or
   guard test that fails CI if a `go/pkg/mutations` path shells out to
   `git checkout`/`git checkout -b`/`git merge` (working-tree-moving commands),
   making invariant clause 2 structurally unhittable rather than reviewer-enforced
   (mirrors RFC 0114 Open Question 7's "make the trap structurally unhittable").
5. **Doctor cross-check anchored commit ↔ artifact `content_sha256`** when an
   RFC 0072 blob store is configured (Open Question 6) — confirm the anchored
   commit actually contains the artifact body the daemon recorded.

## Domain Modeling

This RFC clarifies a **boundary** (per `docs/reference/domain-driven-design.md
§ "Adding to the model"`) rather than adding an aggregate: it makes explicit
which git refs the **Run** aggregate (and its **Job** entities) is the custodian
of, and asserts that the operator's working checkout is *outside* the daemon's
write boundary. The new durable artifacts are **domain events** —
`job.commits_anchored`, `worktree.force_released`, and the anchor fields on the
existing `worktree.abandoned` / `worktree.released` events — which record, in the
hash-chained event log, exactly where each job's commits became reachable. The
core invariant adds no new aggregate root or value object; the run branch ref
and the `refs/striatum/<run>/<job>` pin are the **Run** aggregate's
externally-visible git projection, the same way `job_worktrees` rows are its
internal one. The companion `worktree.gc` method is an operator cleanup surface
over that projection, not a new aggregate.
