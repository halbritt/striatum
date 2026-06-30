# Example Workflows

This directory contains workflow fixtures for choosing, studying, and adapting
Striatum run shapes. They are not runtime defaults: every run still starts from
an explicit `workflow.json` passed to `striatum run prepare` or selected through
the local workflow browser.

Treat the checked-in examples as starting material, not as a promise that the
lane commands and write scopes fit your target repository. Before preparing a
real run:

1. Pick the example whose failure mode matches the work.
2. Replace fixture lanes such as `local` and placeholder command arrays with the
   agent-loop lanes you actually use.
3. Move artifact paths and write scopes out of `examples/...` or `src/example/`
   and into a task-specific root in the target repository.
4. Run `striatum workflow validate <path> --json` and resolve current validator
   output before `run prepare`.

For the broader selection guide, diagrams, and generator roadmap, see
[`docs/reference/workflow-types.md`](../docs/reference/workflow-types.md).

## Quick Selection

| If the work is... | Start with |
|---|---|
| A small documentation review | [`docs-review-flow/`](docs-review-flow/) |
| A small code or docs change with one revision path | [`code-change-flow/`](code-change-flow/) |
| A run that must pause for an owner decision | [`human-checkpoint-flow/`](human-checkpoint-flow/) |
| A claim-heavy synthesis that needs evidence rows | [`support-ledger-flow/`](support-ledger-flow/) |
| An RFC or process cleanup needing independent reviews | [`rfc-ledger-cleanup/`](rfc-ledger-cleanup/) |
| Choosing between implementation approaches before coding | [`implementation-panel-flow/`](implementation-panel-flow/) |
| Multi-lane design, build, and review | [`three-lane-design-build-review/`](three-lane-design-build-review/) |
| Multi-lane design/build with live reviewer interrogation | [`iterated-interrogating-panel/`](iterated-interrogating-panel/) |
| Adversarial proposal challenge before publication | [`falsification-gate-flow/`](falsification-gate-flow/) |
| One focused challenge against a draft finding or proposal | [`cross-examination-flow/`](cross-examination-flow/) |
| Objections that must become binding constraints | [`adjudicated-constraint-extraction-flow/`](adjudicated-constraint-extraction-flow/) |
| Completion claims that need witness evidence | [`verification-gate-flow/`](verification-gate-flow/) |
| Widening an option space before choosing | [`divergent-ideation-flow/`](divergent-ideation-flow/) |
| Recovering hidden constraints before proposal writing | [`fog-of-war-review-flow/`](fog-of-war-review-flow/) |
| Retiring repeated claims after a forum | [`synaptic-prune-flow/`](synaptic-prune-flow/) |
| One behavior-preserving refactoring campaign | [`refactoring-campaign/`](refactoring-campaign/) |
| Harness-profile packet projection tests | [`harness-profiles/`](harness-profiles/) |
| Validator negative-path behavior | [`adapter-unavailable-flow/`](adapter-unavailable-flow/) |
| Bounded revision exhaustion behavior | [`failed-review-revision-cycle/`](failed-review-revision-cycle/) |
| Historical RFC replay or provenance | [`rfc-0014-operational-artifact-home/`](rfc-0014-operational-artifact-home/), [`rfc-0050-http-sse-mcp/`](rfc-0050-http-sse-mcp/) |

## Small Starter Flows

### `docs-review-flow/`

Use this when the output is documentation and the desired path is draft, review,
then summary or apply note. It is the smallest readable review-and-synthesis
fixture: `draft_docs` writes `DOCS_DRAFT.md`, `review_docs` writes
`review/DOCS_REVIEW.md`, and `apply_docs` writes `DOCS_SUMMARY.md`.

The checked-in lane is a `local` fixture lane, so adapt it before expecting a
model to author artifacts. Current validation also treats same-model
author/reviewer pairing as a lint refusal unless you pass
`--allow-same-model-pairing`, record an override in the workflow, or split
author and reviewer lanes. Choose `code-change-flow/` when the work touches
source, and choose `human-checkpoint-flow/` when an owner decision must stop the
run.

### `code-change-flow/`

Use this for a narrow implementation or docs patch that should get one bounded
review loop before final application. The graph is
`draft_change -> review_change -> apply_change`; a `needs_revision` verdict from
the review can route once back to the draft job. The fixture writes to
`src/example/` and `docs/code-change/`, so real use starts by replacing those
paths with the task's actual write scope.

The lane is a single Codex-flavored supervised process lane and is not an
independent review setup by itself. Modern validation also requires autonomous
repo-write agent-loop lanes to use `worktree_isolation: "per_job"` or to carry
an explicit shared-checkout rationale. Use a multi-lane workflow when reviewer
independence matters, or `failed-review-revision-cycle/` when you specifically
need to exercise revision exhaustion.

### `human-checkpoint-flow/`

Use this when the runner must pause after analysis and review so the operator
can decide whether to continue. The jobs produce `ANALYSIS.md`, a review
finding, and a `human_checkpoint` decision artifact. The checkpoint is live
runner state, not a sentence in a Markdown file.

This fixture is useful for risky write scopes, accept/reject decisions, and
policy questions that should not be resolved by the lane alone. It requires an
operator to unblock the run, so prefer `docs-review-flow/` or
`code-change-flow/` when no decision point is needed.

### `failed-review-revision-cycle/`

Use this as a runner-behavior fixture for the `needs_revision` route and the
bounded-cycle failure path. It has the same draft/review shape as the small code
change example but omits the final apply job: after one revision loop, another
blocking review verdict demonstrates how the run escalates instead of looping
forever.

This is not the normal implementation starter. Use `code-change-flow/` when you
expect the work to land after review.

### `adapter-unavailable-flow/`

Use this only as a negative validation fixture. It declares a `local` process
lane with `network = forbidden` and asks for enforced network isolation, while
the process adapter can only provide advisory-strict handling for that
constraint. The validator is expected to reject it.

Do not copy this as an offline-work template. If you need actual
network-isolated work, use an adapter or lane substrate whose enforcement level
matches the workflow constraints.

## Evidence And Review Flows

### `support-ledger-flow/`

Use this when the main risk is unsupported prose. The author first publishes a
synthesis, then writes a support ledger that maps each claim to evidence. An
auditor checks those rows, and a final reviewer accepts or rejects the package.

The artifact path is `docs/support-ledger-flow/`: `SYNTHESIS.md`,
`SUPPORT_LEDGER.md`, `audit/AUDIT.md`, and `final/FINAL_REVIEW.md`. This is a
single-lane fixture, so it is about evidence discipline rather than model-family
independence. Choose `rfc-ledger-cleanup/` when you need several reviewers to
disagree before synthesis.

### `rfc-ledger-cleanup/`

Use this when a proposal, RFC cleanup, or process record needs independent
review, a normalized findings ledger, synthesis, and a final readiness gate.
The graph is draft, parallel Codex/Gemini reviews, findings ledger, Claude
synthesis, and Claude final review. A final `needs_revision` can route once back
to synthesis.

The fixture writes under `docs/reviews/rfc-ledger/` and assumes its configured
agent commands are available. Use it as the current generic pattern for
multi-review synthesis. Choose `support-ledger-flow/` when every claim needs a
support row but broad reviewer disagreement is not the point.

### `rfc-0014-operational-artifact-home/`

This is historical provenance for the RFC 0014 operational-artifact-home design
thread. It is useful when you want to inspect how an older multi-review package
was reviewed: Claude and Codex independent reviews, a findings ledger, a
synthesis, and a final review.

Keep the historical boundary explicit. The fixture mentions old RFC context and
process paths, and one Gemini-titled review is wired through the Codex lane in
the preserved JSON. Do not present it as the current generic RFC example; use
`rfc-ledger-cleanup/` for that.

## Design And Build Flows

### `implementation-panel-flow/`

Use this before implementation when the expensive mistake is choosing the wrong
approach. The panel frames the problem, fans out three proposals, scores each
against fixed criteria, compiles a tradeoff ledger, arbitrates a path, runs a
dissent review, and records a decision. It produces `PROBLEM_BRIEF.md`,
`PROPOSAL_A.md` through `PROPOSAL_C.md`, three scorecards,
`TRADEOFF_LEDGER.md`, `ARBITRATOR_SYNTHESIS.md`, `DISSENT_REVIEW.md`, and
`DECISION.md`.

The fixture is provider-neutral and does not write source code. Use it to decide
what to build, then hand the decision to a build/review workflow.

### `three-lane-design-build-review/`

Use this for a generic design/build/review run where three independent design
lanes should propose options before one implementation proceeds. The shape is
three design artifacts, one synthesis, an ergonomics design review, one
implementation handoff, and three build reviews with `threat_model`,
`ergonomics_dx`, and `devils_advocate` postures. Review verdicts can route back
to synthesis or implementation up to two times.

The default artifacts live under `docs/three-lane-design-build-review/`, with
source write scopes under `src/` and `tests/`. Replace `{{TASK}}`, lane
commands, artifact roots, and write scopes before running it in another repo.
Choose `iterated-interrogating-panel/` when reviewers need preserved-context
interrogation instead of artifact-only review.

### `iterated-interrogating-panel/`

Use this for higher-risk design/build work where reviewers need to question the
author's live context before judging the artifact. The design loop fans out
Codex, Claude, and Gemini design artifacts, synthesizes them, then runs a
three-posture review panel against an interrogable synthesis session. The build
loop then has one implementer and the same three-posture review panel against an
interrogable implementation session.

This shape requires live agent-loop lanes with preserved context and reviewer
sessions that hold the `interrogate` capability. One-shot commands are the
wrong substrate. The role prompts cap interrogation rounds; that cap is prompt
discipline, not an engine limit. Use the simpler three-lane fixture when
artifact review is enough.

### `rfc-0050-http-sse-mcp/`

This is a preserved dogfood fixture for the HTTP/SSE MCP server work. The
original Phase A-C and follow-on cleanup have landed, so use it for replay,
audit, or regression-design exercises rather than as a current product promise.

Its graph mirrors the design/build/review family with RFC-specific artifact
paths under `docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/`. The
fixture scope covers daemon HTTP/SSE MCP behavior such as initialize,
`tools/list`, `tools/call`, capability-token authorization, port configuration,
and tests; it explicitly deferred other work in the original run. Use
`three-lane-design-build-review/` for a generic implementation workflow.

## Collaboration Gates

### `falsification-gate-flow/`

Use this when a leading proposal should face adversarial pressure before it can
be committed downstream. The holder publishes `HOLDER.md`, two falsifiers
publish challenges, and an adjudicator publishes
`COLLABORATION_LEDGER_${cycle}.md`. If the ledger clears, the workflow publishes
`PROPOSAL.md` and `FINAL_SUMMARY.md`; if it does not, one bounded
`needs_revision` cycle routes back to the falsifier phase.

Choose this when proposal quality is the question. Use `verification-gate-flow/`
when the disputed object is completion evidence, or `cross-examination-flow/`
for a smaller one-challenge gate.

### `cross-examination-flow/`

Use this when a draft finding or proposal needs one material challenge before
publication. The author publishes `DRAFT.md`, the cross-examiner publishes
`CROSS_EXAM.md`, the adjudicator gates with a collaboration ledger, and a
cleared run publishes `PROPOSAL.md` plus `FINAL_SUMMARY.md`.

This is narrower than the falsification gate: one challenger instead of two. Use
`adjudicated-constraint-extraction-flow/` when objections should become binding
constraints that drive revision and spec publication.

### `adjudicated-constraint-extraction-flow/`

Use this for productive refusal. Instead of letting a `needs_revision` verdict
remain a generic block, the adjudicator converts load-bearing objections into a
constraint table that the next revision must explicitly discharge.

The fixture has eight phases: survey, convener synthesis, posture-specific
cross-examination, adjudication, revision synthesis, discharge review, spec
publication, and final review. Five cross-examiner postures cover product,
implementation, privacy, eval, and operations. The final review typechecks
constraint discharge before publication.

This is intentionally heavy. Choose `cross-examination-flow/` or
`falsification-gate-flow/` when a simpler publication gate is enough.

### `verification-gate-flow/`

Use this when completion claims need witnesses. The builder publishes a
`CLAIM_LEDGER.md`, the verifier runs the listed witnesses and publishes a
`VERIFICATION_REPORT.md`, the adjudicator records a collaboration ledger, and a
cleared run publishes `VERIFIED_RELEASE.md` plus `FINAL_SUMMARY.md`. Claims use
the `VERIFIED`, `ASSERTED`, and `DESIGNED` status lattice; anything above
`DESIGNED` needs evidence.

This directory demonstrates the portable "today primitives" version, where the
verifier agent runs the witnesses and the engine gates on the adjudicated
verdict. The newer generated `verification_gate` shape adds first-class verify
job behavior and verifier receipts.

## Exploration And Memory-Pressure Flows

### `divergent-ideation-flow/`

Use this when the goal is breadth before judgment: architecture options, API
shape, naming, migration strategy, or fuzzy bug hypotheses. The workflow frames
the problem, fans out five independent branches under different cognitive
frames, converges by scoring novelty, viability, fit, traps, and cross-model
signals, deepens the top three ideas, and writes a final ideation synthesis.

Artifacts include `PROBLEM_BRIEF.md`, one `IDEAS.md` per branch,
`CONVERGENCE.md`, three `DEEPENED.md` files, and
`IDEATION_SYNTHESIS.md`. Do not use this as an implementation workflow; use it
to pick or sharpen the next implementation workflow.

### `fog-of-war-review-flow/`

Use this when the risk is false confidence from partial context. The coordinator
partitions a spec into disjoint fragments, reconstructors receive only their own
fragment and interrogate peers to recover missing constraints, a rollup records
what was recovered or missed, and a judge with the full spec gates proposal
writing. The proposal phase is withheld until the coverage gate clears.

This fixture needs strict information hiding and a real ground-truth holder.
The judge scores constraints as reconstructed, hallucinated, or missed; a
`needs_revision` verdict can route once back to reconstruction. Use divergent
ideation when the problem is option generation rather than hidden constraint
recovery.

### `synaptic-prune-flow/`

Use this after a forum has produced claims that future runs should stop
re-litigating. A coordinator opens and closes a conversation with a
`post_dialog_hook`, still-live participants nominate one claim each for
retirement, a rollup gathers nominations, and an adjudicator retires claims that
receive at least two coherent nominations into a collaboration ledger.

The retired set is provenance for future work, not a reputation score for
participants. If a participant is gone, the workflow records that fact and keeps
moving. Use this to shorten future debates, not to choose new work.

## Refactoring Campaign

### `refactoring-campaign/`

Use this for one named, behavior-preserving refactoring. It is three chained
runs, not one large workflow.

Stage 0, `stage-0-goal-selection/`, is an implementation-panel graph that
surveys candidates, produces three goal proposals, scores them, arbitrates, runs
dissent review, and records `GOAL_DECISION.md`.

Stage 1, `stage-1-plan-gate/`, is a falsification gate for the plan. It writes
`REFACTORING_PLAN.md`, runs two falsifier passes, adjudicates binding
constraints, and publishes `COMMITTED_PLAN.md` plus `GATE_SUMMARY.md`.

Stage 2, `stage-2-execution/`, is the execution loop. The author executes the
committed plan slice by slice in an isolated worktree, the reviewer checks
behavior preservation, and the final job writes `FINAL_REPORT.md`.

The campaign should stop on a red baseline, unverifiable preservation claim,
behavior change, frozen-surface conflict, oversized slice, or non-reproducible
verification. Landing the executed worktree is an operator integration step
after the run clears.

## Fixtures For Runner Behavior

### `harness-profiles/`

Use this to test RFC 0010 harness-profile projection into work packets. It is
not a substantive product workflow. Three independent jobs exercise `generic`,
`codex`, and `claude_code` lane profiles and write one `OUT.md` per lane under
`examples/harness-profiles/out/`.

Choose it when you are checking runner/profile exposure behavior. For RFC,
evidence, or implementation work, pick a workflow above.

## Historical Provenance

The examples directory still carries historical fixtures because they explain
why current Striatum workflow shapes exist. Historical examples are useful for
replay, audit, and regression-design work, but they should not silently set
policy for new workflows. When a historical fixture disagrees with current
source behavior, current source and `docs/reference/spec.md` win.
