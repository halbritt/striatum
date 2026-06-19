# RFC 0094: Deferred Collaboration Shapes — Fog-of-War Review, Synaptic Prune, and Adjudicator Reliability

Status: partially implemented (slices 1, 2 & 3 landed — `post_dialog_hook`,
work-packet type sequencing, the `fog_of_war_review` / `synaptic_prune` shapes,
and the §5 adjudicator-reliability extras: the Check-B `correspondence` rubric,
the ledger `v1.1` per-entry `correspondence`/`coverage` fields + top-level
`adjudicators`/`adjudication_mode`, and the second-adjudicator-on-disagreement
gate; the §5 anti-theater regression corpus + live-fixture dogfood remain)
Date: 2026-05-30
author: proposer-claude-opus-4-8-001

Implementation note (2026-06-18, #402): The two prerequisite mechanisms —
`post_dialog_hook` (Goal 1, the close-time emit-before-teardown) and **work-packet
type sequencing** (Goal 2, the generator `gate.withhold_packet_types` /
`until_verdict_clears` declaration that compiles to ordinary RFC 0045 cross-phase
dependencies) — landed first. Both deferred shapes are now **real generated
shapes** (Goals 3 & 4): `workflow generate --shape fog_of_war_review` and
`--shape synaptic_prune` each compile to a `striatum.workflow.v1.1` phased graph
(`go/pkg/workflowgenerate/shapes_fog_synaptic.go`), registered in the catalog and
generator. `fog_of_war_review` uses the §2 type-sequencing gate to withhold its
`proposal`-typed job behind a full-spec judge's coverage verdict; `synaptic_prune`
declares the §1 `post_dialog_hook` on the coordinator's runtime `conversation.open`
so close emits the prune fan-out before participant teardown. Both publish the
existing `collaboration_ledger.v1.1` (which already carries the `fog_of_war_review`
/ `synaptic_prune` shape enum + `constraint`/`nomination` entry kinds), so no new
artifact contract was required. Both ship at support-tier `experimental` (RFC 0106:
no graduation without a green RFC 0105 unattended-reliability fixture).

Implementation note (2026-06-19, #402, slices 2 & 4): the Goal 5
adjudicator-reliability layer landed at the **contract layer** — the publisher's
exit-6 front-matter guard (`go/pkg/artifactcontracts/collaboration_ledger.go`),
which is the gate the whole collaboration-shape family already shares (it is what
`review.submit` / `publish-artifact` enforce). The additive ledger `v1.1` fields
are now validated: per-`challenge` `correspondence` (`landed_and_rebutted` /
`landed_unrebutted` / `not_material`), per-entry `coverage` (`reconstructed` /
`hallucinated` / `missed`), and the top-level `adjudicators[]` /
`adjudication_mode`. The semantic **Check-B** clearing rule is enforced: once any
`correspondence` is recorded, a clearing verdict requires ≥1 `landed_and_rebutted`
and no `landed_unrebutted` (the confident-non-rebuttal anti-theater case). The
opt-in **second-adjudicator-on-disagreement** gate is enforced: under
`adjudication_mode: second_on_disagreement` a clearing verdict requires ≥2
**distinct** adjudicators (RFC 0064 diversity), while a contested clear is
conservatively recorded as `needs_revision`. Every RFC 0093 V1 ledger stays valid
(additive only). **Still deferred**: the §5 anti-theater regression CORPUS (a
seeded transcript→expected-verdict fixture set) and the live 3-lane design→build
dogfood that calibrates Check-B adjudicator cost (Open Question 6).

Context:
- [RFC 0093](0093-structured-live-collaboration-workflow-shapes.md) — the parent.
  It named the live-collaboration shape family and built the V1 slice
  (`collaboration_ledger.v1`, the `adjudicator` role + `phase_synthesis`
  substance-gate, the `falsification_gate` / `cross_examination` shapes + the
  `scribe` modifier). It **explicitly deferred** `fog_of_war_review`,
  `synaptic_prune`, the `post_dialog_hook` liveness hook, floor-control
  primitives, and "semantic anti-theater scoring beyond structural ledger
  validation." This RFC picks up exactly that deferred set.
- [RFC 0086](0086-multiparty-conversation.md) — symmetric N-party conversation
  (`conversation.{open,say,close,list,show}`, round-robin floor, shared
  transcript, D144). `post_dialog_hook` is a new field on the conversation
  fixture and fires off `conversation.close`. Its noted-but-unbuilt
  moderator/nominator floor variant is the floor-control dependency below.
- [RFC 0082](0082-interrogation-sessions.md) — the 1→1 asymmetric interrogation
  primitive against a *preserved-context* target. Both deferred shapes fan out
  `interrogation.open` to live peers; `synaptic_prune`'s correctness depends on
  the targets still being live, which is the liveness race.
- [RFC 0081](0081-conversation-trajectories.md) — the `dialogue` trajectory the
  adjudicator reads (D028, never raw stdout).
- [RFC 0034](0034-workflow-generator-and-template-catalog.md) /
  [RFC 0045](0045-multi-phase-workflow-editor-and-schema.md) /
  [RFC 0074](0074-workflow-shape-and-adversary-pack-catalog.md) — the generator,
  multi-phase gating, and pack-catalog surfaces. `fog_of_war_review` needs the
  generator to emit a **work-packet *type* sequencing** gate it cannot express
  today.
- [RFC 0064](0064-review-diversity-and-override.md) — reviewer-diversity rules;
  the second-adjudicator mechanism extends them.
- [RFC 0087](0087-divergent-ideation-workflow-shape.md) — the fresh-session
  divergence sibling. `fog_of_war_review` is its mirror: deliberately *partial*
  context that lanes must reconstruct *through* dialog, the opposite of 0087's
  enforced isolation.
- [`docs/decisions/decision-log.md`](../decisions/decision-log.md) — D028
  (curated-text-only, narrowed by RFC 0092), D144 (conversation), D155 (RFC 0093
  V1). New decisions proposed below.

## Problem

RFC 0093 shipped the two collaboration shapes that are **pure composition** —
`falsification_gate` and `cross_examination` compile to ordinary phased
workflows plus existing dialog calls with zero daemon change. It deferred the
two shapes that each need *one* new mechanism, and the two reliability
mechanisms that the substance-gate's own Open Questions flagged as unfinished.
Three concrete gaps block the deferred shapes from being authorable today:

1. **The liveness race (blocks `synaptic_prune`).** `synaptic_prune` runs *after*
   a conversation closes: each participant nominates the one claim to retire,
   and ≥2-vote claims become a durable "do-not-re-litigate" record. But the
   nomination is an `interrogation.open(lane, "prune")` against a participant
   that must still be **live** (preserved-context, `state=active`). Today
   `conversation.close` tears the floor down with no hook to fan out follow-up
   work while the lanes are still up, so the prune fan-out races lane teardown:
   by the time the coordinator reacts to the close, targets may already be gone.
   RFC 0093 §5c sketched a `post_dialog_hook` to close this; it was deferred.

2. **No work-packet *type* sequencing (blocks `fog_of_war_review`).**
   `fog_of_war_review` distributes *disjoint* spec fragments to each lane; lanes
   must interrogate peers to reconstruct the hidden constraints; and the
   `proposal`-typed job must stay **withheld** until a judge lane (holding the
   full spec) scores reconstruction coverage through the substance-gate. The
   generator can gate a job on an upstream job's *verdict* (RFC 0045) but cannot
   today gate the *emission of a work-packet of a given type* on a gate — the
   "no lane may propose until the gate clears" constraint. This is a generator
   capability gap, not a daemon-authority gap: the daemon already gates jobs;
   the generator just can't express "type X is unreachable pre-gate."

3. **The adjudicator can be fooled (weakens every shape).** RFC 0093 OQ2: the
   substance-gate moves theater risk from the *gate condition* to the
   *adjudicator's judgment* — itself a model inference over text. V1 validates
   the `collaboration_ledger` **structurally** (≥1 claim, challenge, rebuttal
   referenced) but does not check that the rebuttal *semantically answers* the
   challenge. A confident non-rebuttal that cites the right turn ids passes V1.
   The anti-theater bar RFC 0093 set for itself (AC#3: "a seeded transcript of
   hollow questions and fluent non-answers yields `needs_revision`") is only
   partially met by structural validation. Two mechanisms were floated and
   deferred: a deeper **Check-B** rubric the adjudicator must satisfy, and a
   **second adjudicator on disagreement** for high-stakes gates.

Without (1) and (2) the two deferred shapes cannot be generated at all; without
(3) every shape in the family — including the two already shipped — inherits an
adjudicator that a sufficiently fluent lane can talk past. All three are
in-scope for one focused RFC because they share the substance-gate substrate and
should be designed against each other (the `post_dialog_hook` and the
second-adjudicator both touch the gate phase boundary).

## Goals

1. **`post_dialog_hook`** — a declared field on the RFC 0086 conversation
   fixture that makes `conversation.close` emit a work packet to a named
   coordinator/role *with the participant session ids + the closed transcript
   reference, before the participant lanes exit their preserved-context window*.
   Closes the one liveness race in the catalog using only existing methods; no
   new daemon authority. Generalizes minimally (any post-close fan-out, not just
   prune).
2. **Work-packet *type* sequencing in the generator** — let a shape declare that
   a job of a given work-packet *type* (e.g. `proposal`) is unreachable until a
   named substance-gate clears, compiled into ordinary RFC 0045 phase
   dependencies. No new daemon method; the generator emits the dependency the
   daemon already enforces.
3. **`fog_of_war_review`** — disjoint-fragment distribution, mandatory
   peer-interrogation cycles, and a judge-scored reconstruction-coverage gate
   that withholds the `proposal`-typed job. Compiles through `workflow generate
   --shape fog_of_war_review` to a `striatum.workflow.v1.1` graph.
4. **`synaptic_prune`** — post-close prune fan-out via `post_dialog_hook`,
   ≥2-vote claim retirement into a durable `collaboration_ledger`, and a
   mechanism to inject the retired set as a **negative preamble** into future
   runs on the same topic. Compiles through `workflow generate --shape
   synaptic_prune`.
5. **Adjudicator reliability** — a deeper **Check-B** adjudicator rubric
   (semantic challenge↔rebuttal correspondence, not just structural presence)
   and an optional **second-adjudicator-on-disagreement** gate for high-stakes
   shapes, both extending RFC 0064 diversity rules. Raise the anti-theater bar
   from structural to semantic, with a regression corpus.
6. **Floor-control degraded mode + parallel-interrogation policy** — specify how
   `fog_of_war_review` behaves under round-robin (no moderator/nominator floor
   yet) and what concurrency policy governs multiple falsifiers/pruners
   interrogating one live holder (the D139/D141 revisit RFC 0093 §1 flagged).
7. **Preserve the product boundary verbatim** — daemon-owned PostgreSQL stays
   the sole live-state authority and sole writer; dialog turns stay curated
   authored-text-only (D028 as narrowed by RFC 0092); no new live-dialog
   primitive, no economy/reputation store, no external service, no vendor SDK,
   local-first, provider-portable with models pinned per lane.

## Non-Goals

- **No new live-dialog primitive.** `post_dialog_hook` reuses `conversation.close`
  + the existing work-packet delivery path; it adds a *fixture field and an emit*,
  not a method. Work-packet type sequencing is a *generator* capability emitting
  existing RFC 0045 dependencies.
- **No moderator/nominator floor primitive in this RFC.** RFC 0086 left
  coordinator-picks-next-speaker floor control as a follow-up. This RFC ships the
  deferred shapes on **round-robin** and specifies their degraded behavior; the
  floor primitive remains a separate RFC that these shapes adopt when it lands
  (Open Question 1).
- **No cross-run reputation or economy.** The retired-claim "negative preamble"
  (Goal 4) is a *topic-scoped durable artifact injected as authoring context*,
  not a reputation score or currency. It is provenance, not a settlement system.
- **No raw transcript capture.** D028 (narrowed by RFC 0092) stands: the
  adjudicator and second-adjudicator read the curated `dialogue` trajectory and
  authored turn text only; the `post_dialog_hook` payload carries session ids +
  a *trajectory reference*, never provider stdout/stderr.
- **No runtime collaboration engine.** Round count, fragment partition, prune
  fan-out arity, and adjudicator count are fixed at generate/prepare time; the
  daemon sees ordinary jobs, the existing dialog rows, and a close-time emit.
- **No replacement of RFC 0093's V1.** The shipped shapes and the
  `collaboration_ledger.v1` contract stand; this RFC *extends* the ledger (a
  minor `v1.1` or additive optional fields, see §5) and adds two catalog
  entries.

## Proposal

### 1. `post_dialog_hook` (closes the liveness race)

Add an optional field to the RFC 0086 conversation fixture:

```
conversation:
  ...
  post_dialog_hook:                 # optional
    deliver_to: <role | session selector>   # the coordinator/pruner seat
    packet_type: prune | <type>              # work-packet type to emit
    include: [participant_session_ids, transcript_ref]
    before_teardown: true                    # emit while participants are live
```

On `conversation.close`, if `post_dialog_hook` is declared, the daemon emits one
work packet of `packet_type` to `deliver_to` **before** releasing the
participant lanes' preserved-context window, carrying the participant session
ids and the RFC 0081 `dialogue` trajectory reference (not raw output). The
participant lanes remain in their `awaiting_interrogation`/active window long
enough for the coordinator to fan out `interrogation.open(participant, …)` while
they are still live. The hook is **declarative and bounded**: exactly one emit at
close, into the existing queue; if a target lane has already died, the follow-up
`interrogation.open` against it fails cleanly (the shape records the dead target
in the ledger rather than hanging — see AC#5).

This reuses `conversation.close` + work-packet delivery; the only new surface is
the fixture field, the close-time emit, and a documented ordering guarantee
(emit-before-teardown). No new method, no new authority.

### 2. Work-packet *type* sequencing (the generator gap)

`fog_of_war_review` needs "no lane may publish a `proposal` until the
reconstruction gate clears." Today the generator gates a job on an upstream
*verdict*; it cannot mark a *type* of work as unreachable pre-gate. Add a
generator-level declaration:

```
phase:
  gate:
    withhold_packet_types: [proposal]    # these types unreachable until...
    until_verdict_clears: <adjudicator gate job ref>
```

The generator compiles this into ordinary RFC 0045 cross-phase dependencies: the
`proposal`-typed jobs are emitted into a phase whose entry depends on the
adjudicator gate's clearing verdict, exactly as a commit phase depends on a
review verdict today. The daemon sees nothing new — it already withholds jobs
behind dependency gates. This is a *generator-authoring* capability + a
`workflow lint` rule (a `withhold_packet_types` set must reference a real gate
job and a real downstream type), not a daemon change. The command-authority
matrix and guardrail tests stay green because no new RPC method or route is
added.

### 3. `fog_of_war_review`

| Element | Wiring |
|---|---|
| Fragment distribution | The coordinator partitions the spec into disjoint fragments and distributes one per lane via the work packet's `inputs` (curated text, D028). The full spec is held only by the judge lane. The partition is fixed at prepare time. |
| Reconstruction dialog | Mandatory `interrogation.open` cycles **between** lanes (each lane interrogates peers to recover constraints it was not given), serialized per holder under round-robin (§6). |
| Coverage gate | A judge/adjudicator lane (holding the full spec) scores which hidden constraints were reconstructed vs hallucinated vs missed, emits a `collaboration_ledger` (shape `fog_of_war_review`) with `constraint`-kind entries + a verdict, and the `proposal`-typed job is **withheld** until that verdict clears (§2). |
| Anti-theater | The judge scores against ground truth it alone holds: a lane that *claims* coverage without having reconstructed the constraint scores `missed`/`hallucinated` and the verdict is `needs_revision`. |

`fog_of_war_review` therefore needs **both** §1-free but §2 (type sequencing) and
the Check-B rubric (§5) to score reconstruction honestly. It is the harder of the
two shapes.

### 4. `synaptic_prune`

| Element | Wiring |
|---|---|
| Trigger | On `conversation.close`, `post_dialog_hook` (§1) emits a `prune` packet to the coordinator with the live participant session ids + transcript ref. |
| Nomination | The coordinator fans out `interrogation.open(participant, "prune: nominate one claim to retire + rationale")` to each still-live participant. |
| Tally | The adjudicator collects nominations; a claim nominated by **≥2** lanes with coherent rationale becomes a `nomination`-kind entry in a `collaboration_ledger` (shape `synaptic_prune`) and is recorded as retired. |
| Durable negative preamble | The retired set is a topic-scoped durable artifact injected as a **negative authoring preamble** ("do not re-litigate: …") into future runs on the same topic. Injection is via the existing pack/prepare authoring-context path — provenance, not reputation. |

The correctness-critical piece is the §1 hook: without emit-before-teardown the
nomination fan-out races lane exit. AC#5 requires the shape to either reach all
live participants or refuse cleanly with the dead targets recorded.

### 5. Adjudicator reliability (Check-B depth + second adjudicator)

RFC 0093 V1 validates the ledger structurally. This RFC adds:

- **Check-B semantic rubric.** Extend the adjudicator's required reasoning: for a
  clearing verdict, each referenced `challenge` must be paired with a `rebuttal`
  the adjudicator judges to *materially address the specific gap the challenge
  raised* — not merely a later turn by the holder. The adjudicator records, per
  challenge↔rebuttal pair, a `correspondence` judgment (`landed_and_rebutted` /
  `landed_unrebutted` / `not_material`) in the ledger. A clearing verdict
  requires ≥1 `landed_and_rebutted` and **no** `landed_unrebutted` of material
  severity. This is the semantic layer above V1's structural presence check.
- **`collaboration_ledger` extension.** Add optional fields (a `v1.1` or additive
  optional set, decided in design): `entries[].correspondence` (above),
  `entries[].coverage` (`reconstructed`/`hallucinated`/`missed` for fog-of-war),
  `adjudicators: [session_id…]` (≥1; ≥2 when second-adjudicator engaged), and
  `adjudication_mode: single | second_on_disagreement`. V1 ledgers remain valid
  (additive only).
- **Second adjudicator on disagreement.** For shapes/gates that opt in
  (`adjudication_mode: second_on_disagreement`), a first adjudicator produces a
  verdict; if it is a *clear* on a high-stakes gate, a second adjudicator (a
  distinct lane/model per RFC 0064) independently scores the same trajectory. On
  disagreement the gate does **not** clear (conservative: a contested clear is
  treated as `needs_revision`), and both ledgers are co-published as provenance.
  This is the OQ2 answer for high-stakes gates; round-robin single-adjudicator
  stays the default for low-stakes shapes.
- **Anti-theater regression corpus.** A seeded set of transcripts (hollow
  questions + fluent non-answers; a confident non-rebuttal that cites the right
  turn ids; a genuinely landed-and-rebutted challenge; a fog-of-war lane that
  hallucinates a constraint it was never given) with expected verdicts, run as a
  fixture. This is the operational definition of "the gate keys on substance."

### 6. Floor-control degraded mode + parallel-interrogation policy

- **Round-robin degraded mode.** Both shapes ship on RFC 0086 round-robin: the
  coordinator drives interrogation/nomination turns in a fixed order rather than
  earning/awarding the floor. `fog_of_war_review` reconstruction cycles and
  `synaptic_prune` nominations are serialized per holder. When the
  moderator/nominator floor primitive lands (separate RFC), these shapes adopt it
  without a shape rewrite (the floor is a turn-ordering policy, not a shape
  change).
- **Parallel-interrogation policy (D139/D141 revisit).** RFC 0093 §1 noted
  concurrent interrogations against one target are unspecified and V1 serializes.
  This RFC keeps **serialized** falsifier/pruner turns against a single live
  holder as the supported policy (one active `interrogation` per target at a
  time), and documents that a parallel-interrogation policy is a prerequisite for
  any future "all falsifiers at once" variant — explicitly out of scope here.

## Artifact / schema impact

- Extend `striatum.collaboration_ledger` with the additive optional fields in §5
  (`correspondence`, `coverage`, `adjudicators`, `adjudication_mode`). Prefer a
  `v1.1` that V1 ledgers validate against (additive-only), decided in the design
  phase. Validated at `publish-artifact` (exit 6 on invalid), with a D028 guard
  that no new field carries provider stdout/stderr.
- Add fixtures: `examples/fog-of-war-flow/` and `examples/synaptic-prune-flow/`,
  plus the anti-theater regression corpus.

## Acceptance Criteria

1. `workflow generate --shape fog_of_war_review` and `--shape synaptic_prune`
   each produce a `striatum.workflow.v1.1` graph that passes `workflow validate`
   / `workflow lint` with default options, wiring only existing RFC 0082/0086
   method calls in the generated packets' `commands` blocks.
2. **`post_dialog_hook`:** a live-PG fixture proves `conversation.close` with the
   hook declared emits exactly one `prune` work packet to the coordinator
   carrying the participant session ids + transcript ref **before** the
   participant lanes' preserved-context window is released, and that a follow-up
   `interrogation.open` against a still-live participant succeeds.
3. **Work-packet type sequencing:** a fixture proves the `proposal`-typed job in
   `fog_of_war_review` is **unreachable** until the reconstruction-coverage
   adjudicator verdict clears, and that the generated graph adds no new daemon
   method/route (command-authority matrix + guardrail tests stay green).
4. **`fog_of_war_review` substance:** a seeded run where a lane claims coverage
   of a constraint it was never given yields a `needs_revision` verdict (scored
   `hallucinated`/`missed` against the judge's ground truth) and the proposal
   stays gated; a run with genuine reconstruction clears.
5. **`synaptic_prune` liveness:** a fixture proves the prune fan-out reaches all
   live participants and records ≥2-vote retirements into a
   `collaboration_ledger`; and a fixture where one target is already dead proves
   the shape **refuses cleanly** (records the dead target, does not hang), per
   §1. The retired set is injected as a negative preamble into a subsequent run
   on the same topic (provenance check).
6. **Check-B / anti-theater corpus:** the seeded regression corpus (§5) yields
   the expected verdict on every case — hollow/fluent transcripts and
   cite-the-right-id non-rebuttals score `needs_revision`; landed-and-rebutted
   and genuine-reconstruction score clearing. The adjudicator records per-pair
   `correspondence` judgments in the ledger.
7. **Second adjudicator:** for a gate with `adjudication_mode:
   second_on_disagreement`, a fixture proves a first-clear + second-`needs_revision`
   leaves the gate **uncleared** with both ledgers co-published, and RFC 0064
   distinct-lane/model diversity holds for the two adjudicators.
8. **Ledger extension is additive:** every RFC 0093 V1 `collaboration_ledger`
   fixture still validates; `publish-artifact` exits 6 on invalid new fields; the
   D028 no-stdout guard covers the new fields.
9. Both shapes render in the RFC 0084 chat UI as they run (manual/integration
   check), and `trajectory export --profile dialogue` reproduces the thread.
10. Docs updated: `docs/reference/workflow-types.md` (two new shape entries),
    `docs/reference/ubiquitous-language.md` (`fog_of_war_review`,
    `synaptic_prune`, `post_dialog_hook`, `negative preamble`, `Check-B`,
    `second adjudicator`), `docs/reference/spec.md` shape list, and
    `docs/rfcs/README.md`. No new daemon method, no floor primitive, no
    economy/reputation store, no vendor SDK; Go-only guardrails (RFC 0078) stay
    green.

## Open Questions

1. **Floor primitive sequencing.** Ship both shapes on round-robin now and adopt
   the moderator/nominator floor when its (separate) RFC lands, or block
   `fog_of_war_review` on the floor primitive? **Proposal: round-robin now**;
   the floor is a turn-ordering policy these shapes adopt without a rewrite.
2. **Ledger versioning.** Additive optional fields on `v1`, or a new `v1.1` the
   V1 ledgers validate against? **Proposal: `v1.1`, additive-only**, so the
   schema id signals the new capability while old ledgers stay valid. Decide in
   design.
3. **Second-adjudicator default.** Opt-in per gate (default single), or default
   on for high-stakes shapes (e.g. anything gating a commit/ship)? **Proposal:
   opt-in via `adjudication_mode`**, with the catalog defaulting
   `fog_of_war_review` to single and leaving `second_on_disagreement` to the
   operator, pending a dogfood that calibrates cost vs catch-rate.
4. **Negative-preamble injection scope.** Topic-scoped only, or also lane/model
   scoped? How is "same topic" keyed (explicit topic id vs heuristic)? **Proposal:
   explicit topic id on the run**, injected only when the operator opts the new
   run into the prior topic. No cross-run automatic linkage.
5. **`post_dialog_hook` generality.** Keep it conversation-fixture-scoped (one
   emit at close), or generalize to a "keep participants live through a gate
   phase" primitive (RFC 0093 OQ4)? **Proposal: conversation-scoped for V1**;
   revisit a general liveness-bridge phase only if a third shape needs it.
6. **Check-B adjudicator cost.** The semantic correspondence judgment is more
   tokens/turns than structural validation. Is a single richer adjudicator pass
   sufficient, or does Check-B itself want a rubric-fixed sub-checklist to stay
   reliable? Needs the dogfood to calibrate (this RFC's own design/build run).

## Domain Modeling / Decisions

Proposed decision-log entries (assigned at acceptance):
- **`post_dialog_hook` emit-before-teardown** is a declarative conversation
  fixture field, not a new method or authority; the daemon emits exactly one
  work packet at `conversation.close` before releasing preserved context.
- **Work-packet type sequencing** is a generator/lint capability compiling to
  existing RFC 0045 dependencies; no new daemon route.
- **Second-adjudicator-on-disagreement** treats a contested clear as
  `needs_revision` (conservative gate) and co-publishes both ledgers; RFC 0064
  diversity applies to the adjudicator pair.

## Implementation Plan (for the design/build dogfood)

This RFC is the input to a 3-lane design→build dogfood (the RFC 0093 pattern).
Recommended slicing, smallest-blast-radius first:

1. **`post_dialog_hook` + `synaptic_prune`** — one fixture field + close-time
   emit + the prune shape. Self-contained; unblocks a whole shape and proves the
   liveness hook.
2. **Check-B + ledger `v1.1` + anti-theater corpus** — strengthens the gate the
   whole family shares; testable in isolation against seeded transcripts.
3. **Work-packet type sequencing + `fog_of_war_review`** — the generator
   capability + the harder shape; depends on Check-B for honest coverage scoring.
4. **Second adjudicator** — opt-in gate mode; layers on after the single-path
   ledger extension lands.
