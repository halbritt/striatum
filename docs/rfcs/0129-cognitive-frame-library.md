# RFC 0129: Cognitive frame library — categories, anti-redundancy selection, and the multi-model convergence signal

Status: accepted / implemented (D199)
Date: 2026-06-14
author: proposer-claude-opus-4-8-001
Context:
- [RFC 0087](0087-divergent-ideation-workflow-shape.md) — the `divergent_ideation`
  graph shape (diverge → converge → deepen). **This RFC is its companion/successor
  for the frame layer**: RFC 0087 owns the graph/phase mechanics and ships the
  frame library as a flat ten-row table copied from ADHD's `SKILL.md`; this RFC
  supplies the real frame **data model**, a **curated, categorized library**, an
  **anti-redundancy selection policy**, and the **multi-model convergence signal**.
  RFC 0087's default-pack table should gain a current-status note pointing here.
- Prior art: [`UditAkhourii/adhd`](https://github.com/UditAkhourii/adhd) (MIT) and
  its spec site <https://adhdstack.github.io/>. Per RFC 0087 Non-Goals and the
  Go-only runtime ([RFC 0078](0078-go-only-runtime-and-python-removal.md)), this
  borrows ADHD's **prompt/frame design**, never its code; no vendor SDK, no
  Node/TS import.
- [RFC 0074](0074-workflow-shape-and-adversary-pack-catalog.md) — role packs and
  adversary packs; a **frame** is the sibling value object and a frame pack the
  sibling authoring input.
- [RFC 0034](0034-workflow-generator-and-template-catalog.md) — the generator +
  template catalog this lives in.
- [RFC 0064](0064-review-diversity-enforcement.md) — review-diversity enforcement
  (same-model refusal, diversity warnings). Precedent: this RFC adds the
  **generative-frame analog** — anti-redundancy by *distortion axis* rather than
  by model identity.
- [RFC 0018](0018-focused-adversarial-review-postures.md) — review postures
  (adversarial *reading* coverage), contrasted with frames (generative *vantages*).
- [RFC 0105](0105-unattended-reliability-harness.md) / [RFC 0106](0106-workflow-shape-support-tiers.md)
  — the behavioral-gate and catalog-governance precedents the diversity-regression
  test and the frame-growth governance question lean on.
- **Empirical grounding.** Every frame proposed here was produced by a *live
  multi-model divergent run of the ADHD method on itself* — six mutually-blind
  frames across four models in two families (Claude Sonnet/Opus/Haiku via the Agent
  tool + local Qwen3.6-35B over `llama.cpp`), six ideas each, then three deepen
  agents. The run, its scores, and the convergence evidence are **Appendix A**.
  This RFC is therefore a dogfood: the method designing its own frame library.

## Problem

RFC 0087 proposes the `divergent_ideation` shape and a "frame pack" authoring input,
but ships the frame library as a **flat table copied verbatim from ADHD's
`SKILL.md`**, with no analysis of whether those are the right vantages, no mechanism
to keep one run's frames non-redundant, and no use of Striatum's defining advantage
over the single-agent ADHD skill — genuinely different models. Three concrete gaps:

1. **The library is unexamined and persona-only.** Every frame in ADHD's table and
   in RFC 0087's default pack is a human persona or profession (hardware-engineer,
   regulator, 10-year-old, competitor, speedrunner, 3am-on-call, …). An entire space
   of frames that are **not personas** — frames that are *operations on the problem
   statement itself* — is absent, as are whole classes of temporal and
   risk-pricing vantages. The set was never audited for coverage; it was inherited.

2. **Frames can fail silently as paraphrases of each other.** ADHD's own write-up
   admits frames can "fail silently — producing paraphrases of another frame's ideas
   — without the harness catching it. Frame-quality evaluation is future work." RFC
   0087 inherits this: it selects frames by coarse `code`/`design`/`general`/`wild`
   tags with **no guard** against two selected frames inducing the same structural
   distortion. A run can therefore spend N branches' worth of model calls for fewer
   than N branches' worth of divergence — the exact failure the method exists to
   prevent.

3. **The multi-model advantage is unused as a signal.** RFC 0087 makes branches
   provider-portable (a strict superset of ADHD's Claude-only behavior) but treats
   multi-model purely as *portability*. It never exploits the fact that when two
   **different model families** independently produce the same frame or idea, that
   convergence is a quality signal a single-model run structurally cannot generate.

## What the divergent run found (the evidence)

The run (Appendix A) produced a sharp, reproducible result:

- **All 15 current frames are personas.** The single biggest structural gap is a
  missing *kind* of frame, not a missing topic.
- **Three missing categories**, each surfaced independently by multiple branches
  and multiple models:
  - **Operation / transform frames** — a frame that is a *verb applied to the
    problem statement* before generation, not a character speaking. Distinct output
    shape: it produces a *re-coordinatization* of the problem (a kernel, a dual
    objective, a basis, an invariant, a setpoint) rather than a worldview-colored
    answer, and it is **composable** in a way personas are not (you can stack
    "compress" then "invert"; you cannot meaningfully stack "10-year-old" then
    "regulator").
  - **Temporal-forensic frames** — fix a *point in time* (past or future) as ground
    truth and reason from it. Fills the gap between the existing **acute**
    `3am-on-call` and an absent **chronic/forensic** stance.
  - **Risk-pricing / accountability frames** — quantify and *price the downside*, or
    locate *who is structurally on the hook*. Distinct from `regulator`'s binary
    pass/fail and `competitor`'s single-exploit search.
- **A live multi-model convergence signal.** A `regulator` branch on Sonnet and a
  `markets` branch on Haiku — different vantages on different models — *independently*
  produced an "actuary / price-the-tail-risk" frame. Two unrelated starting points
  falling toward the same attractor is strong evidence the gap is real, and is a
  signal a Claude-only run cannot produce. This is the concrete argument for Goal 4.

## Goals

1. **Categorize and curate the library.** Add a `frame_kind` discriminator
   (`persona` | `operation`), a `category` (`persona` | `operation` |
   `temporal_forensic` | `risk_pricing`), and **distortion-axis dimensions** to every
   frame; add the three new categories on top of the existing personas.
2. **Give operation-frames a real generator path** — a *transform-then-generate*
   step (the frame rewrites the problem statement before the branch generates) plus a
   **required checkable intermediate artifact**, so the convergence critic can score
   whether the transform did real work instead of dressing the input in jargon.
3. **Anti-redundancy selection.** The generator refuses to select two frames sharing
   ≥2 distortion-axis dimensions in one run, and skips operation-frames on
   low-structure / pure-preference problems where they collapse to vacuity. A run's
   branches become *provably distinct*.
4. **Multi-model convergence as a first-class signal.** When branches are spread
   across ≥2 model families, the convergence job records cross-family agreement as a
   confidence boost; a **diversity-regression test** asserts the policy actually
   yields non-converging survivors. This operationalizes ADHD's deferred
   "frame-quality evaluation."
5. **Stay inside the generator + catalog (+ optional artifact-schema) bounded
   context**, exactly per RFC 0087: no daemon method, no runtime engine, no external
   call in any state transition, no vendor import.

## Non-Goals

- Inherit all of RFC 0087's Non-Goals: no daemon method, no runtime "divergence
  engine," no Node/TS or Claude Agent SDK import, no replacement for RFC 0052
  committee deliberation, no change to how lanes execute.
- **No claim the library is complete.** The anti-redundancy gate makes a single
  run's frames distinct; it does **not** prove the library spans all useful
  distortions. Frame quality stays an empirical, dogfood-measured property — this
  RFC supplies the *measurement gate*, not a proof.
- **No model-pinning requirement.** Multi-model convergence is a *bonus signal when
  available*, not a precondition; single-family runs still work (the signal is simply
  empty).
- No daemon-side automatic frame *authoring*. Frames are curated authoring data,
  like role/adversary packs.

## Proposal

### 1. Frame data model (extends the RFC 0087 frame pack)

Each catalog frame gains (purely authoring metadata — never a persisted live-state
field):

- `frame_kind`: `persona` | `operation`. Existing frames default to `persona`.
- `category`: `persona` | `operation` | `temporal_forensic` | `risk_pricing`. The
  existing `code`/`design`/`general`/`wild` tags are retained for backward-compatible
  selection.
- `dimensions`: a small map of distortion axes, used only by the anti-redundancy
  gate. Per category:
  - temporal-forensic — `{ time_anchor: past|present|future|deep_future,
    agency: active|custodial|forensic|reflective|zero,
    evidence_type: residue|inherited_debt|fossil_record|contingency|live_signal }`
  - risk-pricing — `{ axis: magnitude|ownership|fragility|hidden_subsidy }`
  - operation — `{ operates_on: objective|constraints|representation|causal_order|invariant,
    min_structure: low|medium|high }`
- Operation-frames additionally carry:
  - `transform_prompt` — the rewrite applied to the problem statement *before*
    generation.
  - `required_output_artifact` — the checkable intermediate the branch must emit
    (kernel / dual objective / basis / invariant / setpoint).

### 2. The curated library (the answer to "are these the best frames?")

**Keep all 15 personas.** Add the three categories below (vantages and dimensions are
the deepened survivors from Appendix A; the generator selects a subset per run, never
all of them).

**Operation frames** (`frame_kind: operation`):

| id | transform / vantage | operates_on | min_structure |
|---|---|---|---|
| `lossy_compression` | Strip the problem to the smallest version that preserves its essential difficulty; solve only that kernel. | representation | medium |
| `dual_problem` | Invert the optimization target — maximize what's being avoided; describe what you'd build to achieve it reliably. | objective | high |
| `load_bearing_wall` | Name the single assumption that, if removed, dissolves the problem; design as if you must remove it. | constraints | low |
| `conservation_law` | Name the quantity that must stay constant (attention, trust, effort, headcount); forbid any solution that secretly violates it. | invariant | low |
| `resolution_sweep` | Re-state the problem at three zoom levels (sentence / paragraph / page); keep only constraints that survive all three. | representation | low |
| `time_reverse` | Start from the finished, obviously-correct outcome and run the problem backward; narrate the last decision first. | causal_order | medium |
| `adversarial_boundary` | Find the input that satisfies every stated requirement while violating its spirit; redesign so that input is impossible. | constraints | medium |

**Temporal-forensic frames** (`category: temporal_forensic`, all `frame_kind: persona`
but tagged on the time/agency/evidence axes):

| id | vantage | time_anchor / agency / evidence_type |
|---|---|---|
| `pre_mortem` | It is 18 months out and this shipped and quietly died — write the cause-of-death report, then design backward from the morgue. | future / forensic / residue |
| `decade_inheritor` | You didn't build this and must keep it alive 10 years after the author quit, no docs — design the version you wouldn't curse. | future / custodial / inherited_debt |
| `returning_exile` | You left this domain 20 years ago and just returned — name everything now taken for granted that was once a live, contested choice. | past / reflective / contingency |
| `archaeologist_2200` | Excavate this artifact 174 years from now with no living witnesses — reconstruct what problem it solved from physical evidence alone. | deep_future / zero / fossil_record |
| `fossil_record_auditor` | Collect every workaround and duct-tape patch; treat them as the fossil record of what the spec got wrong; write the corrected spec. | present / forensic / inherited_debt |

**Risk-pricing frames** (`category: risk_pricing`) — split by *distortion axis*, not
by finance costume (see the consolidation trap in Appendix A):

| id | vantage | axis |
|---|---|---|
| `actuary` | Build the loss distribution — probability band × impact multiplier per failure; price the <0.1%, >10× tail. | magnitude |
| `liability_chain_auditor` | Trace every decision to its last accountable owner — at which handoff does responsibility evaporate, and who is named when it fails? | ownership |
| `short_seller` | State the bull thesis in one sentence; name the single assumption whose falsity voids it; bet against it cheaply before consensus notices. | fragility |
| `liquidity_provider` | Who silently bears the volatility and what do they implicitly charge — which hidden subsidy makes the system look cheaper than it is? | hidden_subsidy |

### 3. Operation-frame generator path (transform-then-generate)

For `frame_kind: operation`, the generator emits a branch whose prompt (a) runs
`transform_prompt` against the raw problem, (b) feeds **both** the original and the
transformed statement to the generator, and (c) requires the branch artifact to
include the `required_output_artifact` so the convergence job can score whether the
transform did real work versus restated the input. Persona frames keep RFC 0087's
single-pass path unchanged. The generator is still the only component that knows about
frames; the validator, scheduler, and daemon see ordinary jobs.

### 4. Anti-redundancy selection + min-structure gate

RFC 0087's selection (code/design/wild heuristic + "≥1 wild frame" + vary across runs)
gains two rules, both at generate/prepare time:

- **Distortion-axis disjointness.** Reject any candidate pair that shares ≥2
  `dimensions` values. This is the generative-frame analog of RFC 0064's same-model
  review refusal: there, two reviewers must not be the same *model*; here, two
  branches must not be the same *distortion*.
- **Min-structure precondition.** A generate-time `problem_shape` hint
  (`low` | `medium` | `high`, default `medium`) gates operation-frames: on a
  `low`-structure / pure-preference problem ("name the launch party"), operation
  frames are skipped because they degrade to jargon-as-content (`"the eigenstate of
  the party is fun-vs-cost"`).

### 5. Multi-model convergence signal

When `lane_set` spreads branches across ≥2 distinct **model families**, the
convergence prompt — and the optional `striatum.ideation_synthesis.v1` schema RFC 0087
defers to its Phase B — gain a `cross_model_agreement[]` section: any idea or cluster
independently produced by ≥2 families is flagged higher-confidence. This is the signal
ADHD (Claude-only) cannot produce. Single-family runs leave the section empty without
error. Model family is read from an explicit lane `model_family` tag (defaulting to a
heuristic over the existing lane `display_model`).

### 6. Diversity-regression test (the frame-quality gate ADHD lacked)

A fixture runs `divergent_ideation` on a known problem and asserts the top survivors
do **not** all collapse into one cluster — the concrete operationalization of ADHD's
deferred "frame-quality evaluation," and an RFC 0105-style behavioral gate for the
frame library (wired alongside RFC 0087 AC5's shape fixture).

## Acceptance Criteria

1. The catalog frame schema is extended with `frame_kind` / `category` /
   `dimensions` (+ `transform_prompt` / `required_output_artifact` for operation
   frames); existing frames migrate to `persona` + a category with dimensions;
   `workflow templates list/show` surface the new fields.
2. The three new categories and their frames land in the bundled library and are
   selectable by the generator.
3. An operation-frame branch emits the transform + the checkable intermediate
   artifact; a fixture exercises one operation frame end-to-end.
4. The anti-redundancy selector refuses ≥2-shared-dimension pairs (unit test); the
   min-structure gate skips operation frames on `low`-structure problems (unit test).
5. A fixture with a 2-family `lane_set` produces a convergence artifact with a
   populated `cross_model_agreement` section; a single-family run leaves it empty
   without error.
6. The diversity-regression fixture asserts non-converging survivors and is wired
   into the RFC 0105 harness alongside the `divergent_ideation` shape fixture.
7. Docs updated: `docs/reference/spec.md` (frame-library note in the workflow-shape
   list), `docs/reference/ubiquitous-language.md` (`frame_kind`, frame **category**,
   **distortion axis**, **anti-redundancy selection**, **cross-model agreement**), the
   generated workflow-catalog reference, and a current-status note on RFC 0087's
   default-pack table pointing here. MIT attribution for ADHD retained.
8. No new daemon method, no `memory.*`/vendor capability, no Node/TS or Claude Agent
   SDK import; the RFC 0078 Python-trace and corpus augmentation guardrails stay green.

## Open Questions

1. **Fold into RFC 0087 or keep separate?** Recommend **separate successor**: RFC 0087
   owns graph mechanics; this owns the library, the selection policy, and the
   multi-model signal — distinct concerns, and 0087 is already large. Both ship before
   the shape graduates a support tier (RFC 0106).
2. **How to declare model family** for Goal 4 — an explicit lane `model_family` tag,
   a heuristic over `display_model`, or a catalog map? Recommend an explicit tag with
   a `display_model` heuristic default.
3. **Operation-frame artifact schema.** Does the checkable intermediate need its own
   front matter, or is a structured section inside the existing `finding` enough?
   Recommend reuse `finding` for V1 (matches RFC 0087's "reuse existing kinds").
4. **Min-structure auto-detection.** V1 takes a generate-time `problem_shape` hint.
   Should convergence later *learn* it from how vacuous the operation-branch outputs
   were? Defer.
5. **Library-growth governance.** RFC 0106 froze new *shapes* pending graduation. Do
   new *frames* need a similar gate, or is the diversity-regression test enough?
   Recommend frames are *data*, gated by the diversity test, not the shape-freeze.

## Domain Modeling

Per [`docs/reference/domain-driven-design.md` § "Adding to the model"](../reference/domain-driven-design.md#adding-to-the-model)
(precedent: RFC 0019, RFC 0087):

- A **frame** and a **frame category** are **value objects** and *authoring inputs* —
  identical in kind to RFC 0087 frame packs and RFC 0074 role/adversary packs.
  `frame_kind`, `category`, `dimensions`, `transform_prompt`, and
  `required_output_artifact` are authoring metadata consumed by the generator; they
  leave **no persisted live-state field**. Only ordinary jobs with frame-seeded
  prompt context survive into the workflow snapshot.
- **Cross-model agreement** is an **annotation on the convergence artifact** (a value
  recorded as durable provenance via the optional `ideation_synthesis` schema), not a
  new aggregate root and not a domain event.
- **Anti-redundancy selection** and the **min-structure gate** are **generator
  policy**, executed at generate/prepare time. The scheduler and daemon see ordinary
  jobs — the boundary is identical to RFC 0087.
- **No new domain event.** Divergence branches emit the existing
  `artifact.published` / job-lifecycle events like any other build job.

## Appendix A — the multi-model divergent run that produced this RFC

This RFC's frames were generated by running the ADHD method *on itself* — the dogfood
that motivates Goal 4.

**Run configuration.** Problem `P`: *"What cognitive frames should a divergent-ideation
engine offer; are the current ~15 the best, and what is missing?"* Six mutually-blind
divergence branches (isolation invariant held — no branch saw another), each a
different frame **and** a different model:

| frame (vantage) | model | family |
|---|---|---|
| regulator (audit coverage gaps) | Claude Sonnet | Claude |
| biology (transplant living-system mechanisms) | Claude Opus | Claude |
| markets (scarce/under-supplied vantages) | Claude Haiku | Claude |
| inversion (negate the anti-frames) | Claude Opus | Claude |
| remove-load-bearing (drop "frame = persona") | Claude Sonnet | Claude |
| ant-colony (emergent / decentralized) | Qwen3.6-35B-A3B (`llama.cpp`) | **Qwen (cross-family)** |

Then a critic pass (score / cluster / trap-detect) and three deepen agents (Opus /
Sonnet / Opus). ~36 candidate frames in, three deepened categories out.

**Converge — clusters (representative score chips, novelty / viability / fit):**

- **Problem-as-operation** (frame = a verb, not a persona) `[N9 V7 F9]` — *the
  highest-leverage finding; defines a whole missing category.* ★
- **Temporal-forensic** (fix a point in time, reason from it) `[N7 V9 F9]` — pre-mortem
  is a validated real technique; fills acute→chronic gap.
- **Risk-pricing / accountability** (price the downside / who's on the hook)
  `[N7 V8 F8]` — strongest cross-model convergence.
- **Emergent-topology** (no central planner; answer is a coordination shape)
  `[N8 V6 F6]` — on-brand for a multi-agent runner; runner-up.
- **Subtract / parasite** (deletion or host-dependency reshapes the answer)
  `[N8 V6 F7]` — runner-up (apoptosis-budget, symbiont, smuggler, loser's-bracket).

**Traps (attractive but flagged):**

- **Five finance frames as five frames** (actuary / creditor / short-seller /
  liquidity-provider / liability-auditor) — four collapse to one "price the downside"
  distortion; *fix*: split by axis (magnitude / ownership / fragility / hidden-subsidy),
  fold `creditor` into actuary+auditor. Kept as the risk-pricing category.
- **Over-mathematical operation frames on fuzzy problems** (eigenstate / entropy-budget
  / dual-problem) — high novelty, low viability where the problem has no structure to
  bite; *fix*: the `min_structure` gate (Proposal §4).
- **Three near-duplicate forensic frames in one run** (archaeologist / exile /
  paleo-anthropologist all "reconstruct intent from residue") — convergence disguised
  as divergence; *fix*: the distortion-axis disjointness gate (Proposal §4).

**Focus — the three deepened categories** are exactly Proposal §2's three tables
(operation / temporal-forensic / risk-pricing), each with the load-bearing risk that
became a Proposal mechanism: operation→checkable-artifact + min-structure;
temporal-forensic→dimension tags + anti-redundancy; risk-pricing→split-by-axis.

**Provocation (the wildcard left open).** The emergent-topology cluster
(`mycelial` / `murmuration` / `microbiome` / `protocol-designer`) was a strong runner-up
and is the most *on-brand* for Striatum specifically — a frame whose answer is a
*decentralized coordination structure* rather than an artifact. A future RFC could add
a fourth category, **topology frames**, and even let such a frame's output seed a
*new workflow shape* — divergent ideation that proposes its own orchestration graph.
