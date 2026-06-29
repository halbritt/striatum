# RFC 0083: Iterated Panel Review with Interrogation

Status: accepted (D139)
Date: 2026-05-25
author: proposer-claude-opus-4-7-001
Context:
[`RFC 0002`](0002-reviewer-independence-policy.md) (reviewer access scope / context policy),
[`RFC 0018`](0018-focused-adversarial-review-postures.md) (review postures),
[`RFC 0068`](0068-go-production-daemon-port.md) (MCP agent-loop, `--print` wrapper),
[`RFC 0074`](0074-workflow-shape-and-adversary-pack-catalog.md) (`implementation_panel` shape),
[`RFC 0081`](0081-conversation-trajectories.md) (`dialogue` trajectory),
[`RFC 0082`](0082-interrogation-sessions.md) (interrogation sessions, `interrogable`, `awaiting_interrogation`),
historical generated-record source path `docs/operator/workflows/interrogating-panel-2026-05-25/PATTERN_SPEC.md` (ground-truth pattern spec),
[`docs/reference/workflow-types.md` § "Review By Interrogation"](../reference/workflow-types.md),
[`go/pkg/workflowtemplates/catalog.json`](../../go/pkg/workflowtemplates/catalog.json) (`iterated_interrogating_panel` catalog entry),
[`examples/three-lane-design-build-review/workflow.json`](../../examples/three-lane-design-build-review/workflow.json),
[`examples/implementation-panel-flow/workflow.json`](../../examples/implementation-panel-flow/workflow.json).

## Problem

RFC 0082 gave us interrogation sessions — a reviewer can question a builder's
*preserved* context instead of cold-reading its artifact. But that capability is
not yet composed into a reusable workflow shape. The two prior-art examples each
hold half of what a real design+build loop wants and neither interrogates:

- `examples/three-lane-design-build-review/workflow.json` chains a 3-lane design
  fan-out → synthesis → review, then a single implement → 3-wide build review,
  with bounded `needs_revision` cycles. Reviews are artifact-mediated only; the
  reviewed agent's reasoning is gone by the time a reviewer reads it.
- `examples/implementation-panel-flow/workflow.json` (RFC 0074
  `implementation_panel`) fans options out to a scoring panel with one revision
  cycle, but again the panel scores documents on cold context.

Defects that live in the *reasoning behind* an artifact — an unstated
assumption, a discarded alternative, a "we'll handle that later" — survive
artifact-only review. They surface only when a reviewer can ask the author *why*
and get an answer from the author's live working memory. We want a single,
reusable shape that catches those defects through genuine interrogation, with
both iteration budgets bounded so a run cannot loop forever.

## Goals

- A reusable workflow shape with **two structurally identical loops** (design,
  build) chained design → build, each ending in an **interrogating panel
  review** against the reviewed agent's preserved context.
- Two clearly separated bounded-iteration budgets: **interrogation rounds**
  (within one review, ≤ 3, early exit on resolved findings) and the **revision
  cycle** (re-work when the panel verdict is `needs_revision`, bounded
  `max_iterations`). Conflating these is the easy mistake; this RFC keeps them
  distinct.
- Run the pattern on the **MCP agent-loop** executor, the only substrate that
  preserves context for truthful interrogation.
- Express the whole shape in `striatum.workflow.v1` with **no engine changes**.
- A conditional deprecation of the `--print` supervised wrapper for new
  workflows, gated on the agent-loop functioning per adapter.

## Non-Goals

- New engine/schema features. Every field used here already exists (RFC
  0002/0018/0074/0082); this RFC composes, it does not extend.
- Replacing artifact-based verdicts; interrogation augments them (RFC 0082).
- Capturing transcripts or terminal output (D028 stands).
- Forcing duplicate *builds* — build fan-out is on the review side, not three
  competing diffs (see Proposal §1).

## Proposal

The reusable example is now represented as catalog shape
`iterated_interrogating_panel`, with
`examples/iterated-interrogating-panel/workflow.json` as the checked-in
fixture. Treat it as the larger design/build sibling of RFC 0093's
substance-gate collaboration shapes: both rely on preserved-context
interrogation and bounded revision, but RFC 0083 uses a panel review pattern
instead of an adjudicator-owned `collaboration_ledger`.

### 1. The pattern shape

Two structurally identical loops, chained design → build. Each loop is:

```
fan-out (3 independent lanes)  →  synthesis  →  interrogating panel review
        ^                                              |
        |___________ revision cycle (needs_revision) __|
```

- **Fan-out (3 lanes).** Three independent agents produce independent artifacts
  for one objective with no cross-talk (`parallel_group`, disjoint write
  scopes). In the **design loop** this is three design proposals. In the
  **build loop** the fan-out is on the *review* side — the implementation stays
  single-author and a 3-wide panel reviews it — so the loop never produces three
  conflicting diffs.
- **Synthesis.** In the design loop, one synthesizer reconciles the three
  proposals into one buildable synthesis. In the build loop, the "synthesis"
  node is the implementer producing the actual change plus a HANDOFF. Either way
  this node is the **reviewed** node.
- **Interrogating panel review.** A `parallel_group` of **3 reviewers** with
  distinct postures (`threat_model`, `ergonomics_dx`, `devils_advocate`). The
  reviewed node is `interrogable: true` and stays live
  (`awaiting_interrogation`) after `work.complete`, so each panel reviewer
  interrogates its preserved context before rendering a verdict.

### 2. Two bounded-iteration concepts (kept distinct)

These are different budgets at different scopes. Do not conflate them.

1. **Interrogation rounds — ≤ 3, early exit on resolved findings.** *Inside a
   single review.* Each panel reviewer runs one interrogation thread against the
   live reviewed session: `interrogation.open` → up to **3** `ask`/`answer`
   rounds → `close`. The reviewer **stops early** and closes the moment its open
   findings are resolved. The cap and the early exit are enforced by the
   **reviewer role prompt**, not the engine (RFC 0082's `interrogation.*` does
   not bound ask/answer). The reviewer must state in its finding how many rounds
   it used and why it stopped.

2. **Revision cycle — bounded re-work.** *Across review → reviewed node.* If the
   panel's aggregate verdict is `needs_revision`, the loop returns to the
   synthesis/implement node. Encoded as a `cycle` with `on_verdict:
   needs_revision`, `max_iterations: 2`. Early exit is automatic: if no reviewer
   returns `needs_revision`, the cycle does not fire.

### 3. Execution substrate — agent-loop-first

All lanes run via the **MCP agent-loop** (`striatumd -agent-loop <cmd>`), not
the `--print` supervised wrapper.

Interrogation requires **preserved context** so the reviewed agent answers from
its own working memory (RFC 0082 §5). The `--print` wrapper spawns a *fresh
process per packet* — no memory carries between packets — so a reviewer
interrogating a `--print` author would receive answers reconstructed from the
published artifact, i.e. exactly the cold-context review interrogation is meant
to replace. A `--print` author cannot be interrogated *truthfully* and therefore
cannot be the interrogation target.

**Conditional `--print` deprecation.** This RFC proposes deprecating `--print`
for *new* workflows, **conditional on** the agent-loop functioning for each lane
adapter (claude / codex / gemini). That condition is being validated
empirically (Phase A of the live run). The deprecation takes effect only for the
adapters where the agent-loop is proven; the RFC does not declare it
unconditionally.

**Fallback for failed adapters.** If a given adapter's agent-loop does not
function in Phase A, that lane falls back to `--print` for *fan-out / review
authoring only* and MUST NOT be the interrogation target. The interrogation
target is always an agent-loop lane (claude is the known-good baseline).

### 4. Schema mapping (`striatum.workflow.v1` — no engine changes)

Every field below already exists. Names are taken verbatim from PATTERN_SPEC §4.

- **3-wide fan-out / panel** → `parallel_group` +
  `parallelism.max_active_jobs: 3` + `require_disjoint_write_scopes: true`.
- **Reviewed node interrogable** → job `interrogable: true`;
  `expected_artifacts` may be `[]` so `complete` needs no separately-published
  artifact on the reviewed node when its artifact *is* the synthesis/handoff.
- **Panel reviewers** → `type: review`, `reviewer_context_policy: fresh`,
  `reviewer_access_scope: document_only` (NOT `artifact_only`, which is invalid),
  distinct `review_posture`, each holding the `interrogate` capability at
  register-session time (`--capability interrogate`).
- **Revision** → `cycles: [{from: <review>, to: <synth/impl>, on_verdict:
  needs_revision, max_iterations: 2, allow_same_lane: true}]`.
- **Verdict vocabulary** (artifact contracts): `accept`,
  `accept_with_findings`, `needs_revision`, `reject`.

The shape is the union of the two prior-art examples — the design→synthesis→
review→revision-cycle chain of `three-lane-design-build-review` and the panel
fan-out of `implementation-panel-flow` — with the reviewed node marked
`interrogable` and reviewers granted `interrogate`. No new job type, edge kind,
or validator rule is required.

### 5. Reusable example + first run

- Reusable pattern lands at `examples/iterated-interrogating-panel/`.
- The live run at `docs/operator/workflows/interrogating-panel-2026-05-25/`
  exercises it on a concrete object task: design + build the **interrogation-log
  UI** feature (render a run's interrogation Q&A as a chat-style transcript in
  the workflow-history web UI). The design loop produces the feature RFC; the
  build loop implements it. The payoff is viewing *this run's own* interrogation
  thread as chat — closing the loop on the data the pattern itself generates.

## Drawbacks

- The reviewer-prompt-enforced ≤3 / early-exit cap is a *prompt* discipline, not
  an engine guard. A misbehaving reviewer prompt could run more rounds; the
  enforcement story is the role prompt + the required round-count statement in
  the finding, not a daemon limit. Hardening ask/answer with an engine cap is
  possible future work but out of scope (RFC 0082 deliberately left it
  unbounded).
- Keeping reviewed sessions live through `awaiting_interrogation` widens the
  resource/lease window (RFC 0082 risk); the idle-timeout + recovery sweep (RFC
  0020/0077) bounds it.
- The agent-loop dependency means the pattern's interrogation value is only as
  good as the per-adapter agent-loop; until Phase A proves codex/gemini, the
  full three-posture panel may interrogate only the claude-authored baseline.

## Alternatives Considered

- **Artifact-only panel review** (the status quo in both prior-art examples).
  Rejected: it cannot reach the reasoning-level defects this pattern targets;
  that is the entire motivation (Problem).
- **Three competing builds in the build loop.** Rejected: produces three
  conflicting diffs and a merge problem; the spec puts build fan-out on the
  review side instead (§1).
- **Engine-enforced interrogation-round cap.** Rejected for this RFC: RFC 0082
  left ask/answer unbounded by design, and adding a cap is an engine change this
  RFC explicitly avoids. Left as future work.
- **Unconditional `--print` deprecation.** Rejected: deprecating before the
  agent-loop is proven per adapter would strand any adapter whose loop does not
  function; hence the conditional deprecation + authoring-only fallback (§3).

## Rollout

1. Land `examples/iterated-interrogating-panel/` (workflow + role prompts) using
   only existing schema fields (§4) and expose it as catalog shape
   `iterated_interrogating_panel`.
2. Run Phase A of the live run to validate the agent-loop per adapter
   (claude / codex / gemini) and the interrogation round / revision cycle
   behavior end to end.
3. On the Phase A result, record the conditional `--print` deprecation decision
   for the adapters proven; failed adapters keep the authoring-only fallback.
4. Document the pattern in `docs/WORKFLOW_TYPES.md` as a composition of the
   existing "Review By Interrogation" type with panel fan-out and bounded
   revision; cross-link the example.

The two `docs/DECISION_LOG.md` entries this RFC proposes (one accepting the
pattern, one accepting the conditional `--print` deprecation) are staged in
`docs/operator/workflows/interrogating-panel-2026-05-25/DECISION_LOG_SNIPPET.md`
for the operator to merge, avoiding a concurrent-write conflict on the log.

## Open Questions

- Should the ≤3 interrogation-round cap eventually become an engine-enforced
  bound on `interrogation.ask`, or stay a role-prompt discipline?
- For the build loop, should each panel reviewer interrogate the implementer
  serially (one live session, three interrogations) or is concurrent
  interrogation of one target safe under the message-bus targeting model?
- After Phase A, do codex/gemini graduate to interrogation *targets*, or remain
  authoring-only fallback lanes until their agent-loops are independently
  hardened?

## Domain Modeling

This RFC adds no new aggregate. It is a **boundary clarification** in the sense
of [`docs/DDD.md` § "Adding to the model"](../reference/domain-driven-design.md#adding-to-the-model): it
names a reusable composition of existing concepts — fan-out (`parallel_group`),
synthesis, the RFC 0082 *interrogation* aggregate, review postures (RFC 0018),
and bounded `cycles` — and pins the two distinct bounded-iteration value
concepts (interrogation rounds vs revision cycle) so they are not conflated in
authoring or tooling. The daemon remains the single writer; no new authority is
introduced.
