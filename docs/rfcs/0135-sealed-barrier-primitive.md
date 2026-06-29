# RFC 0135: The sealed expectation barrier primitive — one (entity, seal) barrier shared by fan-in, quorum, revision-coherence, and run.integrate

Status: accepted / implemented (D216; P0-P6 shipped v2.34.0; P1/P2 fan-in live by default with `STRIATUM_BARRIER_FANIN=0` kill switch per D269/#527/#354; P4 quorum + P6 run.integrate live, P5 confirmed live)
Date: 2026-06-17
author: proposer-claude-opus-4-8-001
Context:
- GitHub [#354](https://github.com/halbritt/striatum/issues/354) — "migrate
  RFC 0132 / 0095 / 0108 onto the shared attempt-sealed barrier primitive." The
  maintainer **ratified the full-span option** (D216): build the shared primitive
  **first**, spanning all four callers, rather than ship four ad-hoc predicates
  that each re-discover the stale-attempt trap. This RFC is that primitive.
- [RFC 0133](0133-fan-in-deferred-join-barrier-and-manifest.md) (accepted, D213)
  — the fan-in deferred join barrier and join manifest. Its Open Question 3 asked
  whether #319 is "the right scope, or the first caller of a primitive that should
  be designed once." This RFC answers: the first caller. RFC 0133 stays the
  authoritative *fan-in* design; 0135 generalizes its predicate. RFC 0133's
  load-bearing JOIN — `staging.attempt = jobs.attempt` — is the special case of
  this primitive's predicate where `entity = job` and `seal = attempt`.
- [RFC 0132](0132-gating-advisory-reviews-quorum-dissent-protection.md) (accepted,
  D212) — gating/advisory reviews + quorum with dissent protection. Its quorum
  predicate (`panelQuorumSatisfied`, branching off `dependenciesSatisfied`) is the
  same barrier keyed on `entity = review job`, `seal = attempt`, with a
  declared-seat denominator. **D214 (this session)** ratified RFC 0132's open
  questions strictly — the verdict-less seat-holding stub and the
  skip-only-provably-dead-seat rule — and those ratifications shape the predicate's
  `abstain` / `blocking` classification here.
- [RFC 0126](0126-multi-reviewer-revision-coherence.md) (accepted, D194) — the
  build-owned monotonic `review_generation`. RFC 0126 **deliberately rejected
  attempt-keying** for review coherence ("recovery churns `attempt`") and chose a
  monotonic generation that does *not* churn under recovery. This RFC's central bet
  is that `review_generation` **is the seal** for review entities — so RFC 0095's
  revision coherence folds into the primitive *without regressing off the
  generation*, because the primitive is keyed on a monotonic seal, never on raw
  `attempt`.
- [RFC 0108](0108-parallel-independent-runs.md) — `run.integrate`'s
  `merge-tree → commit-tree → CAS update-ref` plumbing (`HandleRunIntegrate` in
  `go/pkg/mutations/integrate.go:27`), and its run-level, run_id-keyed integration
  gate at a higher layer than the per-job barrier.
- [RFC 0118](0118-gate-run-completion-on-attested-provenance.md) — the
  run-completion provenance gate and the doctor integrity invariants the barrier
  must keep green.
- Decisions [D213](../decisions/decision-log.md) (RFC 0133),
  [D212](../decisions/decision-log.md) (RFC 0132),
  [D194](../decisions/decision-log.md) (RFC 0126 review generation),
  [D206](../decisions/decision-log.md) (the shipped per-completion fan-in merge),
  [D211](../decisions/decision-log.md) (RFC 0131 forgery-resistant sealed-work
  progress), and [D187](../decisions/decision-log.md) (the #244 migration-ownership
  boundary that forces the owner-bundle decision below).
- Prior art in source, read at `main` to ground every claim:
  `go/pkg/mutations/mutations.go` (`dependenciesSatisfied:828`,
  `latestVerdict:895`), `go/pkg/mutations/worktree.go`
  (`fanInIntegrateRunBranch:1091`, the attempt-namespaced pin refs),
  `go/pkg/mutations/integrate.go` (`HandleRunIntegrate:27`, the merge-tree/CAS
  plumbing), `go/pkg/mutations/revision_routing.go` (`bumpReviewGeneration:361`,
  its same-transaction call from `reopenJobForAttempt:349`),
  `go/pkg/mutations/review.go` (the verdict `review_generation` stamp at `:653`),
  `go/pkg/mutations/recovery_decision_tree.go` (`supervisedAgentConfirmedDead:983`,
  the forgery-resistant dead-PID oracle), `go/pkg/db/migrations_test.go`
  (`TestFutureRuntimeMigrationsDoNotCarryOwnerDDL:423`, the owner-DDL build guard),
  `go/pkg/db/sql/owner/0012_job_quarantine_state.sql` (the owner-bundle precedent),
  `go/pkg/db/sql/0005_repo_local_workflow_state.sql` (the `jobs` table, its
  `job_type` CHECK, and `UNIQUE (repository_id, run_id, workflow_job_id, attempt)`).

> **Self-applied discipline.** The single load-bearing claim of this RFC — "the
> four callers share one barrier" — was `ASSERTED`, then **`VERIFIED` against
> source, and the verdict was that the mechanism is *not natively shared*.** RFC
> 0095/0126 already uses `review_generation` (a monotonic epoch, RFC 0126/D194)
> that *deliberately rejected* attempt-keying; RFC 0108 is run_id-keyed at a
> higher layer than the per-job staging barrier. The unification is therefore a
> **design bet**, not a refactor of existing shared code. The Risks section
> records that honestly: 0095/0108 folding in depends *entirely* on the
> seal-not-attempt keying holding up. The maintainer chose the full-span option
> (D216) with that finding in hand.

## Problem

Four places in the runtime each evaluate "is the expected set of contributions
present and current?" and each has independently re-discovered the same trap —
that **counting by a churning key (raw `attempt`, or `job_id`) silently admits a
stale contribution from a superseded round**:

1. **Fan-in** (RFC 0133 / D213). N siblings stage to attempt-addressed refs; the
   barrier must JOIN `staging.attempt = jobs.attempt` (the live attempt) or a
   requeued attempt's stale ref re-strands the real output behind a successful
   join — *"the original stranding bug reborn behind a successful join"*
   (RFC 0133 synthesis trap #1).
2. **Panel quorum** (RFC 0132 / D212). The downstream gate is satisfied
   edge-by-edge in `dependenciesSatisfied` (`mutations.go:828`); there is no
   aggregate quorum node and no declared-seat denominator. RFC 0132 adds
   `panelQuorumSatisfied` over a *frozen* denominator, classifying each declared
   seat's latest active verdict — and the classification must be keyed on the
   stable seat (`workflow_job_id`), never on the churning `job_id` recovery
   reassigns.
3. **Revision coherence** (RFC 0095 / RFC 0126 / D194). A revision bumps the
   build's `review_generation` (`bumpReviewGeneration:361`, called in the same
   transaction as the attempt bump from `reopenJobForAttempt:349`); a verdict
   stamped with a prior generation (`review.go:653`) is non-current by mismatch.
   The finalization gate is a set-difference: every required reviewer must have a
   **current-generation** accepting verdict.
4. **Run.integrate** (RFC 0108). `HandleRunIntegrate` (`integrate.go:27`)
   advances a mainline ref to a *completed* run's branch via merge-tree/CAS,
   serialized per-repo, idempotent on a prior integration — a run-level barrier
   keyed on `run_id` and the run's terminal state.

These are four predicates, four trap-rediscoveries, four doctor surfaces, four
test suites. The cross-cutting observation (RFC 0133 OQ3; RFC 0132 OQ2; the #354
capstone) is that **they are the same barrier counting the same thing badly**: a
barrier that counts by a key that *churns under recovery* (`attempt`, `job_id`)
instead of by the stable identity that was *sealed*. The maintainer ratified
(#354 / D216) that the right move is to design the barrier **once** — and to do
it **before** the four callers each ship their own predicate, so they land *as
instances of the primitive* rather than as four predicates to be reconciled later.

## The thesis: key on (stable entity id, monotonic SEAL counter)

A **sealed expectation barrier** is parameterized by two things:

- a **stable entity id** — an identity that does *not* churn under recovery,
  requeue, resume, transfer, or revision; and
- a **monotonic seal counter** — an integer per entity that advances strictly on
  every event that *invalidates a prior round's contribution* (a requeue, a
  revision, a re-open). A seal **never decreases** and a superseded seal's
  contributions are structurally invisible to the current seal.

The barrier readiness predicate is, for every declared in-edge:

> there exists a staged contribution whose embedded seal **equals** the entity's
> **live seal** (`staging.seal = entity.live_seal`), and the entity is in a
> terminal-acceptable state.

It **never counts staged refs per entity** and **never reads "the latest
contribution" by recency** — it JOINs each in-edge's staging ref against the
entity's *current* seal, so a contribution from a superseded seal is
*structurally* absent from the count, not filtered out after the fact.

The load-bearing claim is that the four callers' keys are all **projections of
the same (entity, seal) abstraction**:

| Caller | entity | seal | "stale-round invisibility" property |
| --- | --- | --- | --- |
| Fan-in (0133) | `job` (a fan-in sibling) | `attempt` | `staging.attempt = jobs.attempt` (RFC 0133); a requeued attempt's ref no longer matches |
| Panel quorum (0132) | `review job` keyed by stable `workflow_job_id` | `attempt` (the seat's live attempt) | a recovered/transferred seat maps back to the same `workflow_job_id`; the forward-written `dissent_ledger` is the *seal-durable* dissent witness |
| Revision coherence (0095/0126) | `review obligation` for a build | **`review_generation`** | a prior-generation verdict is non-current by generation mismatch (RFC 0126), *exactly* `staging.seal = entity.live_seal` |
| Run.integrate (0108) | `run` | run-completion epoch (terminal state + integration idempotency) | a run already integrated into a target is a no-op; the run is the entity, job-level barriers compose into it |

Two of these already key on a *raw attempt* (fan-in, quorum); one keys on a
*monotonic generation that deliberately is not attempt* (revision coherence); one
keys at a *higher run layer* (integrate). The primitive's job is to make all four
the same shape **without forcing the generation-keyed caller back onto attempt**
— which is why the abstraction is `seal`, not `attempt`.

### Why `seal`, not `attempt` (the spine of the whole RFC)

`attempt` (`striatumd.jobs.attempt`) **churns under recovery**: a requeue /
resume / `complete-stalled` bumps it, and a stalled-transfer reassigns `job_id`
and `session_id` entirely (RFC 0132's flaw analysis). RFC 0126 (D194) saw exactly
this and refused to key review coherence on `attempt`, introducing
`review_generation` — a monotonic counter that advances *only on a revision*, not
on every recovery event, and that survives the `job_id` churn because it lives on
the stable build job's seat.

The seal is the generalization of "the thing RFC 0126 introduced so that recovery
churn does not erase a round's identity." Concretely:

- For **fan-in** and **quorum**, the seal is the live `attempt`. (This *does* churn
  on recovery — but for these callers churn is *desirable*: a requeue should
  invalidate the prior staged ref, which is precisely RFC 0133's trap-#1 kill.
  The seal abstraction permits attempt as a seal because attempt is monotonic and
  non-decreasing within a `(repository_id, run_id, workflow_job_id)` seat.)
- For **revision coherence**, the seal is **`review_generation`**. The primitive
  does **not** re-key it onto attempt — it *adopts the generation as the seal*.
  This is the entire reason 0095/0126 fold in without regressing: the primitive's
  predicate `staging.seal = entity.live_seal` is *byte-identical in shape* to RFC
  0126's "non-superseded accepting verdict stamped with the build's current
  generation," with `seal := review_generation`.
- For **run.integrate**, the entity is the **run**, and a job-level sealed barrier
  composes into a run-level one: a run's integration epoch is satisfied iff every
  in-edge job-level barrier has fired and the run is terminal. The primitive is
  generic over `entity ∈ {job, run}`; job barriers nest inside the run barrier.

A monotonic seal that does not decrease is the only property the predicate needs.
Whether a given caller's seal *churns on recovery* (attempt) or *only on a
content-invalidating event* (generation) is a per-entity policy, not a property
of the barrier — and that is what lets one barrier serve all four.

## Per-caller reconciliation

### Fan-in (RFC 0133) — `entity = job`, `seal = attempt`

The primitive *is* RFC 0133's Slice 2 barrier, lifted to be entity-generic.
- Each sibling stages to `refs/striatum/staged/<run>/<job>/<attempt>` (RFC 0133);
  the attempt suffix is the seal embedded in the ref name.
- Readiness: per in-edge, take the live seal (`MAX(attempt) WHERE
  state='completed'` for the seat) and require a staging row whose embedded seal
  equals it — i.e. the primitive predicate with `seal := attempt`.
- The staging write is co-transactional with the attempt/state update (RFC 0133);
  the primitive preserves this.
- RFC 0133's quarantine-as-terminal-in-edge, requeue-tombstone, and
  `recovery/`-prefix exclusion become *the primitive's* terminal-state and
  stale-ref rules, applied identically to every caller.

### Panel quorum (RFC 0132) — `entity = review job` (stable `workflow_job_id`), `seal = attempt`

`panelQuorumSatisfied` becomes the primitive evaluated over a **declared-seat
denominator** with three additions D214 ratified:
- The entity id is the **stable seat key** (`workflow_job_id`), so a
  recovered/transferred seat maps back to the same entity — never `job_id`
  (RFC 0132 Risks; the primitive enforces this by construction since the entity id
  is the JOIN key).
- A **daemon-authored abstention stub holds a seat / raises the frozen
  denominator but carries NO verdict value** (D214(a) / RFC 0132 OQ1 ratified
  strictly): in the primitive's classification an unfilled seat is `abstain`, never
  `accept`/`reject`. The stub is *seat-occupancy state*, not a contribution with a
  seal-bearing value — it cannot satisfy `staging.seal = entity.live_seal` because
  it has no contribution, only a held seat counted against the denominator.
- **Skip only a provably-dead seat** (D214(b) / RFC 0132 OQ2 ratified): the
  primitive may treat an in-edge as terminally-absent (counting against the
  abstention budget) **only** when the seat is `structurally_unrecoverable`, bound
  to the existing forgery-resistant `supervisedAgentConfirmedDead`
  (`recovery_decision_tree.go:983`) oracle. A *live* gating seat blocks the barrier
  — quorum may skip a dead seat, never a slow one.
- The forward-written `dissent_ledger` (RFC 0132 Layer B) is the **seal-durable
  dissent witness**: a live dissent row blocks the barrier wherever recovery later
  moved the seat's lineage, which is the quorum analog of "a superseded-seal
  contribution is invisible" applied to *blocking* rather than *satisfying*
  contributions.
- **Quorum-shape generalization (`k_of_n`) is deferred to lint** (D214 /
  RFC 0132 OQ3) — the primitive carries only `all_seats | budget:N`; richer shapes
  desugar in `workflowauthoring/lint.go`, **no schema change**.

### Revision coherence (RFC 0095 / RFC 0126) — `entity = review obligation`, `seal = review_generation`

This is the fold that justifies the whole "seal not attempt" framing.
- `review_generation` **becomes the seal** for review entities. No re-keying onto
  attempt; the primitive *adopts* the monotonic generation. `bumpReviewGeneration`
  (`revision_routing.go:361`, called same-tx from `reopenJobForAttempt:349`) is the
  primitive's "advance the entity's seal" operation.
- The verdict stamp at `review.go:653` is the primitive's "embed the seal in the
  contribution" operation; the write-boundary rejection of a superseded-generation
  verdict (RFC 0126 P1) is the primitive's "a contribution sealed below the live
  seal cannot be recorded" rule.
- The finalization gate (RFC 0126's per-build set-difference: required obligations
  MINUS current-generation accepting verdicts) is *exactly* the primitive's
  readiness predicate with `seal := review_generation`. The primitive does not
  change RFC 0126's behavior; it **renames** RFC 0126's mechanism as the
  canonical instance of the shared predicate, and any future review-coherence work
  inherits the primitive's doctor/test surface for free.
- **No regression off `review_generation`** is the explicit, accepted bet
  (Risks). RFC 0126 rejected attempt-keying for a reason; the primitive honors that
  by keying on the seal, and `review_generation` *is* the seal — so adopting the
  primitive cannot drag review coherence back onto a churning attempt.
- **Landed at P5 (docs + tests only, NO behavior change, NO migration).** The
  three operations above are named as seal operations directly in source comments
  (`bumpReviewGeneration`, the `applyVerdict` verdict stamp, and the
  `verifyRunCompletionProvenance` finalization gate), and the equivalence is
  fenced by `TestRevisionCoherenceIsTheSealInstance`
  (`go/pkg/mutations/revision_coherence_seal_test.go`): it re-expresses the
  RFC 0126 #282 regression (a revised build at generation 2; reviewer A accepts at
  gen 2; reviewer B's gen-1 `needs_revision` survives) and evaluates the primitive
  readiness predicate `bool_and(is_terminal_gap OR staged.seal = live.seal)` with
  `seal := review_generation` directly over the live verdict/obligation rows,
  asserting it REFUSES naming reviewer B — byte-identical to RFC 0126's set-difference
  outcome. The default finalization path is unchanged; the seal predicate is a pure,
  un-wired equivalence witness, never a new production code path.

### Run.integrate (RFC 0108) — `entity = run`

- The entity is the **run**; the run barrier is satisfied iff (a) the run is in a
  terminal-acceptable state (`completed`, matching `integrate.go:55`) and (b) every
  declared job-level sealed barrier inside the run has fired. **Job-level barriers
  compose into the run-level barrier.**
- The merge-tree/CAS/idempotency plumbing in `HandleRunIntegrate`
  (`integrate.go:27`, the `runIntegratedInto` no-op, the per-repo `lockRepo`) is
  the run-entity's "assembly," exactly as RFC 0133's `barrier_assembly` is the
  job-entity's assembly. The two share the merge-tree/CAS code path.
- Run.integrate keys on `run_id` *at a higher layer* than the per-job staging
  barrier (the source finding). Folding it in means demoting "run is completed and
  not yet integrated" to "the run-entity's seal is live and its in-edge job
  barriers are all fired" — a structural recasting, not a behavior change, and the
  highest-risk fold (see Risks).

## The generalized JOIN barrier predicate and the trap-killer property

The predicate is minted **once** as an exported SQL fragment + a named SQL
function (mirroring the `ExpiredLeaseStillStalePredicate` named-fragment precedent
RFC 0133 cited), generic over the entity table and the seal column:

```sql
-- barrier_ready(entity_kind, run_id, barrier_id): every declared in-edge has a
-- staged contribution at the entity's LIVE seal, or is a terminal-acceptable gap.
-- NEVER a COUNT(*) of staged refs per entity; ALWAYS a JOIN on the live seal.
SELECT bool_and(
         edge.is_terminal_gap                       -- quarantined / provably-dead seat
      OR staged.seal = live.seal                     -- the load-bearing JOIN
       )
  FROM barrier_in_edges      edge
  JOIN entity_live_seal      live   USING (entity_kind, entity_id)
  LEFT JOIN staged_contrib   staged USING (entity_kind, entity_id)
 WHERE edge.barrier_id = $1;
```

- For **fan-in**: `entity_kind='job'`, `seal=attempt`, `staged_contrib` = the
  `refs/striatum/staged/<run>/<job>/<attempt>` rows.
- For **quorum**: `entity_kind='review_seat'` keyed by `workflow_job_id`,
  `seal=attempt`; `staged_contrib` = a non-superseded current-seal verdict;
  `is_terminal_gap` = a `structurally_unrecoverable` stub within the abstention
  budget; a live `dissent_ledger` row forces the edge non-terminal-and-blocking.
- For **revision**: `entity_kind='review_obligation'`, `seal=review_generation`;
  `staged_contrib` = an accepting verdict at the current generation.
- For **run**: `entity_kind='run'`, the in-edges are the run's job barriers; the
  seal is the run's integration epoch.

**The trap-killer property, generalized.** A stale ref / stale verdict / stale
staged contribution from a *superseded seal* never appears in the JOIN, because
the JOIN demands `staged.seal = live.seal`. There is no filtering step that a
forgotten code path can skip, no ref count that a stale ref can inflate, no "latest
by recency" read that a slow lane can poison. This is RFC 0133's synthesis-trap-#1
killer (`staging.attempt = jobs.attempt`) **generalized to every seal** — and the
reason a *static guard* (below) can fail the build on any `COUNT(*)`-of-refs shape
across **all four** callers at once.

## Schema (per-object split — copied verbatim from the #333 / D215 ratification)

The maintainer ratified (D215, #333) a **per-object** schema-ownership split.
These are the load-bearing constraints; they apply to the primitive's tables
exactly as RFC 0133 framed them for fan-in:

- **`barrier_assembly` job_type CHECK widening => OWNER BUNDLE 0013 (mandatory).**
  `jobs` is owner-held; a runtime `ALTER` of its `job_type` CHECK **fails the build
  guard** `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL`
  (`go/pkg/db/migrations_test.go:423`) **and** crash-loops a two-role production
  daemon per D187 / #244. The widening ships as an owner bundle, mirroring
  `go/pkg/db/sql/owner/0012_job_quarantine_state.sql`. No exceptions.
- **New tables (freeze / staging / `barrier_state`) => RUNTIME migrations with NO
  SQL FOREIGN KEY to `striatumd.jobs`.** This is the FK-to-owner-table trap: a
  runtime table carrying a real `FOREIGN KEY` into an owner-held table re-creates
  the cross-role ownership problem. Keep the `(repository_id, run_id,
  workflow_job_id, attempt/seal)` key as **bare columns**; enforce referential
  integrity **in Go**, not as a SQL FK. **Each new table needs its own explicit
  `GRANT`** — pgtest masks an omitted grant (the default harness runs
  runtime-migrations-only as a single role), so a missing grant passes tests and
  fails in two-role production.
- **`join_manifest.v1` => no DDL.** It is an `artifactcontracts` registration with
  the publisher's exit-6 schema guard (RFC 0133 Slice 1), not a table.

**Owner-bundle number coordination (resolved after #333 / D215).** The
`barrier_assembly` widening landed first as owner bundle `0013`. The earlier RFC
0132 optional `dissent_quarantine` run-state reservation did not land before
#372/#379 consumed owner bundle `0014`, so `dissent_quarantine` must use the next
available owner bundle number if it ships.

## Slice plan — re-cast #344–#347 as the primitive's first callers

The maintainer ratified building the primitive **first** (D216). The existing
RFC 0133 slice issues #344–#347 are **re-cast as the primitive's job-entity
callers** rather than fan-in-only work; the RFC 0132 slice issues #338–#343
**consume the same readiness predicate** rather than minting their own.

Build/migration order (each slice independently landable; later slices depend on
earlier):

| Slice | Re-cast of | Scope | Schema |
| --- | --- | --- | --- |
| **P0 — the predicate + the manifest** | #344 (133-A) | Mint `barrier_ready` once as an exported fragment + named SQL function, entity/seal-generic; ship the static build guard against any bare ref-`COUNT(*)` barrier shape; register `join_manifest.v1`. Lands on today's D206 merge for audit value before any new barrier fires. | `join_manifest.v1` **no DDL** |
| **P1 — staging + the live-seal JOIN barrier (fan-in instance)** *(IMPLEMENTED — runtime migration `0029`; live default after D269 with `STRIATUM_BARRIER_FANIN=0` kill switch)* | #345 (133-B) | Attempt-addressed staging refs; the immutable freeze record; the JOIN barrier with `entity=job, seal=attempt`; requeue tombstone, `recovery/`-prefix exclusion, merge-base contamination check, advisory-lock fire serialization, quarantine-as-terminal-in-edge. The first *live* instance of the primitive. | freeze/staging tables: **runtime, no FK to jobs, explicit GRANT each** |
| **P2 — assembly + N=1 unification (job-entity)** | #346 (133-C) | `runBarrierAssembly` backed by a `barrier_state` row (`sealed→assembling→committed\|failed`) with two-phase journaling; route N=1 through the one path. The downstream gate runs this path inline before queueing the join; the explicit `barrier_assembly` dispatcher remains a compatibility surface. | `barrier_assembly` CHECK => **owner bundle 0013** for the explicit dispatcher; `barrier_state` => **runtime, no FK, explicit GRANT** |
| **P3 — doctor + refusal + verify** *(IMPLEMENTED — `barrier_integrity` doctor invariant + `BARRIER_BLOCKED`/`blocked_manifest` (a DERIVED runtime/doctor condition, NOT a `barrier_state` CHECK value) + `striatum join verify` (`join.verify`) + runtime migration `0031` `barrier_status` view; D206 remains the `STRIATUM_BARRIER_FANIN=0` fallback)* | #347 (133-D) | The generalized barrier doctor invariant (subsumes `fanin_sibling_unintegrated` and the per-integration checks), the `BARRIER_BLOCKED` named state + `blocked_manifest`, `barrier_status` view, `striatum join verify`. | none |
| **P4 — quorum consumes the predicate** *(LIVE — flipped to default by the D233/#354 cutover: `dependenciesSatisfied` routes a GATING review panel through `panelQuorumSatisfied`, retiring the edge-by-edge `latestVerdict` default for paneled gates; kill switch `STRIATUM_BARRIER_QUORUM=0`; equivalence `TestPanelQuorumCutoverEqualsEdgeByEdge`)* | #338–#343 (132-A…F) | `panelQuorumSatisfied` is **the primitive** with `entity=review_seat (workflow_job_id), seal=attempt`, declared-seat denominator, verdict-less abstention stub (D214a), skip-only-provably-dead (D214b), forward-written dissent. `k_of_n` desugars in lint (D214). #343's optional `dissent_quarantine` run-state now needs the next available owner bundle if it ships. | per RFC 0132 / D215 (no-DDL workflow_json + runtime `dissent_ledger`; optional run-state CHECK => owner bundle) |
| **P5 — revision coherence is named as the seal instance** *(IMPLEMENTED — docs + tests only, NO behavior change, NO migration; `bumpReviewGeneration` = "advance the entity's seal", the `applyVerdict` review_generation stamp = "embed the seal in the contribution", and the RFC 0126 finalization set-difference = the primitive predicate with `seal := review_generation`, all named in source comments; fenced by `TestRevisionCoherenceIsTheSealInstance`)* | RFC 0095/0126 follow-up | `review_generation` is documented and tested as `seal := review_generation`; the RFC 0126 finalization gate is asserted to be the primitive predicate. **No behavior change**, no migration — a renaming + a shared-doctor/shared-test fold. | none |
| **P6 — run.integrate folds in (entity=run)** *(LIVE — flipped to default by the D233/#354 cutover: `HandleRunIntegrate` gates on `runEntityBarrierReady`, retiring the bare `state=='completed'` default; the shared assembly `assembleRunEntityIntegration` was already factored in; kill switch `STRIATUM_BARRIER_RUN_ENTITY=0`; equivalence `TestRunIntegrateRunEntityBarrierGate` + `TestRunIntegrateIsTheRunEntityBarrier`. After D269, runs with declared fan-in job barriers compose through committed `barrier_state`; non-fan-in and kill-switch runs still reduce to the terminal-state check.)* | RFC 0108 follow-up | The run-completion integration gate is recast as the run-entity barrier whose in-edges are job-level barriers; the merge-tree/CAS path is shared with `barrier_assembly`. The highest-risk fold (Risks). | none (uses existing run/integration state) |

The four-caller span is **P1+P4 (attempt-sealed), P5 (generation-sealed),
P6 (run-sealed)** — the full breadth #354/D216 ratified. P0–P3 land the primitive
through the fan-in caller first (lowest risk, RFC 0133's already-accepted design);
P4 reuses the predicate; P5/P6 are the *bet* folds and ship last, each behind the
existing equivalence-fixture discipline (assert identical outcome to today's path
before flipping any workflow).

**Cutover outcome (D233, #354 FULL cutover).** Three of the four callers are now
the LIVE default, each behind a recoverable env kill switch and a proven equivalence
fixture: **P4 quorum** (`dependenciesSatisfied` → `panelQuorumSatisfied`,
`STRIATUM_BARRIER_QUORUM=0` reverts; byte-identical to edge-by-edge at the default
budget 0, STRICTER only where it kills the stale-seal-accept trap), **P5 revision
coherence** (already the live gate — `review_generation` IS the seal, enforced at the
verdict stamp + `resetDownstreamForRevision`), and **P6 run.integrate**
(`HandleRunIntegrate` → `runEntityBarrierReady`, `STRIATUM_BARRIER_RUN_ENTITY=0`
reverts; equivalent because no live-path run declares a job-level barrier, so the
gate reduces to the terminal-state check today and composes for the future).
**P1/P2 fan-in are now live by default (D269/#527)** — confirmed fan-in runs stage
and pin declared siblings instead of merging them at completion, then the downstream
gate waits for the live-seal barrier and assembles through `runBarrierAssembly`
before queueing the join. `STRIATUM_BARRIER_FANIN=0` restores the D206
per-completion path; unconfirmed-branch runs also stay on that fallback because no
safe frozen tip exists. The same-final-tree fixtures prove the assembled *tree*
matches D206, and `TestFaninCutoverStagesPinsAndAssemblesBeforeDownstreamQueues`
proves the live wiring: no premature run-branch movement, committed barrier state,
and downstream enqueue only after assembly. Live deployment equivalence remains
deferred until `striatum doctor` is green.

## Risks (honest record of the source finding and the accepted bet)

- **The mechanism is NOT natively shared — this is a design bet, not a refactor.**
  Source analysis (verified at `main`) found: RFC 0095/0126 already uses
  `review_generation`, a monotonic epoch (RFC 0126/D194) that **deliberately
  rejected attempt-keying because recovery churns `attempt`**; RFC 0108 is
  **run_id-keyed at a higher layer** than the per-job staging barrier. There is no
  existing shared predicate to extract — the four callers are genuinely different
  mechanisms today. **The entire unification rests on the (entity, seal) keying
  being the right abstraction for all four.** The maintainer chose the full-span
  option (D216) with this finding explicitly in hand. If the abstraction is wrong,
  P5/P6 do not fold and the primitive degrades to "fan-in + quorum share a
  predicate" (still a win, but not the four-way win ratified).
- **0095/0108 folding depends entirely on seal-not-attempt holding up.** P5 is
  safe *only if* `review_generation` is a faithful seal — i.e. it advances exactly
  when a prior round must be invalidated and never decreases. If a future change
  makes the generation churn on recovery (the thing RFC 0126 forbade), the fold
  silently re-keys review coherence onto a churning counter and re-opens the
  RFC 0095/0101 stale-verdict reopen wedge. P6 is riskier still: run.integrate's
  run_id key lives above the staging layer, so recasting it as a run-entity barrier
  whose in-edges are job barriers must preserve the per-repo serialization
  (`lockRepo`, `integrate.go:48`) and the integration idempotency
  (`runIntegratedInto`) exactly, or it regresses RFC 0108's gate.
- **Two-store consistency (inherited from RFC 0133).** The PG state transition and
  the git CAS cannot share one transaction. The primitive inherits RFC 0133's
  two-phase journaling (write `target_commit_sha`+`tree_sha` to PG *before* the git
  CAS) for the job-entity assembly, and the run-entity assembly inherits
  `HandleRunIntegrate`'s existing idempotency.
- **Merge-base proves ancestry, not content (inherited from RFC 0133).** A staged
  ref of `frozen_tip + cherry-picked evolved content` passes the merge-base test;
  the child-idea mitigation (record `frozen_tip_tree_sha`, assert per-commit tree
  provenance) carries over unchanged.
- **A wrong seat-key mapping fails closed (inherited from RFC 0132).** If a
  recovered/revised entity is not mapped back to its stable entity id, the barrier
  deadlocks (a worse failure than false-fire). The predicate must resolve to the
  stable entity id (`workflow_job_id` for review seats), never `job_id`; the
  four-end-state fixture (RFC 0132) and the live-seal fixture (below) fence this.
- **One primitive = one blast radius.** A defect in the shared predicate breaks all
  four callers at once. Mitigated by: minting it once as a guarded named fragment;
  a single audited chaos suite; and landing the callers incrementally (fan-in
  first, the bet-folds last) so a regression surfaces on the lowest-risk caller
  before the high-risk ones adopt it.

## Test plan

- `TestSealedBarrierJoinsOnLiveSeal` — the generalized form of RFC 0133's
  `TestFaninBarrierJoinsOnLiveAttempt`: a contribution sealed at seal=1, then the
  entity's seal advances to 2; the barrier must **not** fire on the seal-1
  contribution and must fire only once a seal-2 contribution exists. Parameterized
  over `seal ∈ {attempt, review_generation}` so it covers fan-in **and** revision
  coherence with one body.
- `TestSealedBarrierFreezePointIsImmutable` — `UPDATE`/`DELETE` of a freeze record
  as `striatumd_rw` is rejected (RFC 0133's `TestFaninFreezePointIsImmutable`,
  generalized): an immutability trigger plus the SELECT/INSERT-only grant.
- `TestBarrierPredicateHasNoRefCount` — a **static-source guard** failing the build
  on any bare `COUNT(*)`-of-staged-refs (or "latest verdict by recency as the gate
  coherence source") barrier shape across all callers — the generalization of
  RFC 0133's static guard, now protecting four call sites at once.
- `TestLatestVerdictIsSealAware` — `latestVerdict` (`mutations.go:895`) /
  `panelQuorumSatisfied` resolve the *current-seal* contribution, never the
  recency-latest row: a slow lane recording a stale-seal verdict does not satisfy
  the barrier (the RFC 0126 write-boundary + the quorum seat classification, made
  one assertion).
- `TestQuorumStubHoldsSeatWithoutVerdict` (D214a) — a daemon-authored abstention
  stub raises the frozen denominator and is classified `abstain`, carrying **no**
  verdict value; it cannot satisfy `staged.seal = live.seal`.
- `TestQuorumSkipsOnlyProvablyDeadSeat` (D214b) — a `structurally_unrecoverable`
  seat (bound to `supervisedAgentConfirmedDead`,
  `recovery_decision_tree.go:983`) is quorum-skippable within budget; a *live*
  gating seat blocks the barrier (silence-from-a-live-lane is not consent).
- `TestRevisionCoherenceIsTheSealInstance` (P5) *(IMPLEMENTED —
  `go/pkg/mutations/revision_coherence_seal_test.go`)* — the RFC 0126 #282
  regression fence re-expressed against the primitive: a revised build at generation
  2, reviewer A accepts at gen 2, reviewer B's gen-1 `needs_revision` remains ⇒ the
  barrier refuses naming B, proving `seal := review_generation` yields RFC 0126's
  exact behavior.
- `TestRunIntegrateIsTheRunEntityBarrier` (P6) — the run-entity barrier produces
  the *same* integrated tree and the same idempotency as today's
  `HandleRunIntegrate`, asserted before any caller flips.
- A crash-mid-assembly recovery test (inherited from RFC 0133): recovery recognizes
  its own commit (or compares to the journaled intent) rather than wedging.
- A barrier-vs-per-completion **same-final-tree** equivalence fixture per caller
  before flipping any workflow over (the RFC 0133 cutover discipline).
- `make -C go vet lint check-tests` **uncontended**.

## Open questions

1. **Should `seal` be a single typed column, or per-caller?** The cleanest form is
   a `seal BIGINT` the predicate reads uniformly; the per-caller form keeps
   `attempt` and `review_generation` as the native columns and the predicate maps
   each to `seal` in the fragment. Recommendation: per-caller native columns mapped
   in one place (avoids a backfill and keeps RFC 0126's column authoritative),
   pinned before P5.
2. **Run-branch-as-projection-of-the-barrier-chain (RFC 0133 OQ2, pushed up).** If
   every run-branch advance is a committed `barrier_state` row whose first parent is
   the prior barrier's commit, the run branch becomes a chain doctor can walk
   newest-first and an out-of-band hand-commit becomes a structural chain break.
   With the run-entity barrier (P6) this is now a *cross-caller* wildcard, not a
   fan-in-only one. Defer until P6 lands.
3. **Does the quorum caller need a per-edge seal at all, or only seat-occupancy?**
   The abstention stub (D214a) holds a seat *without a contribution*; that is
   seat-occupancy, not a sealed contribution. Pin whether the quorum instance reads
   the seal for *satisfying* edges only and treats occupancy as a separate
   denominator axis (recommended) before P4 reuses the predicate.

## Domain Modeling

The sealed expectation barrier is a **boundary clarification + a new value
object**: `Seal` is a value object (a monotonic, non-decreasing integer per
entity); the barrier readiness predicate is a **domain invariant** over the
`(entity, seal)` value, evaluated transactionally under the per-run advisory lock
(RFC 0104). The four callers are the same aggregate invariant projected onto four
aggregate roots (fan-in job, review seat, review obligation, run). Cites
[`docs/DDD.md § "Adding to the model"`](../reference/domain-driven-design.md#adding-to-the-model);
RFC 0019 is the precedent.

## Appendix — design provenance

The (entity, seal) thesis, the "four callers are projections of one barrier"
recasting, and the seal-not-attempt fold for review coherence were the convergent
finding of two independent divergent-ideation branches across RFC 0133 OQ3 and
RFC 0132 OQ2 — the same `(entity, attempt)`-barrier insight reached from the
fan-in TOCTOU and the quorum stale-verdict angle independently (RFC 0133 Appendix;
RFC 0132 Appendix). The capstone observation (#354) that these are "the same
(entity, attempt)-barrier bug → a candidate unifying primitive" was surfaced as a
scoping decision rather than an aside *because* the cross-branch convergence is the
strongest signal that the unification is real and not a forced abstraction. The
maintainer ratified the full-span build (#354 / D216) with the explicit source
finding — recorded honestly in Risks — that the mechanism is not natively shared.
