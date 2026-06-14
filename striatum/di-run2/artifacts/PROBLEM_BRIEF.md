# Problem Brief — Graduating `divergent_ideation` from experimental to supported

author: problem-framer-claude-opus-4.8-001

## The Question

How should Striatum graduate the new `divergent_ideation` workflow shape from
`experimental` to `supported`?

This is the open-ended space the divergence branches explore. It is *not* a
request to redesign the shape, and *not* a request to improve idea quality — it
is a request for the path and the evidence that earn the promotion.

## Why this is open now

`divergent_ideation` already compiles and runs — this very run is a dogfood of
it. What is missing is the *evidence and process* that lets the project promote
the shape past the existing graduation gate. The genuinely open part: that gate
demands an automated proof of reliability, but the workflow's own outputs are
non-deterministic model text. How you prove reliability without being able to
assert on content is the crux the branches must engage.

## What the shape is (background every branch needs)

`divergent_ideation` compiles to a flat `striatum.workflow.v1` fan-out:

- a `frame_problem` brief (this job),
- N **fresh-session diverge branches**, each under one cognitive *frame* — a
  vantage that distorts how the problem is re-asked; generator-only, no
  evaluation; branches cannot see each other,
- a **convergence critic** that scores ideas on novelty/viability/fit, clusters
  them, flags traps, and picks the top-K,
- K **deepen** jobs that develop the survivors,
- a **final synthesis**.

Frames come from a curated library selected by an anti-redundancy gate (no two
frames share ≥2 distortion axes) plus a min-structure gate; branches round-robin
across lanes so different models carry different frames.

## The graduation gate (fixed — do not relitigate)

Per **RFC 0106**, a workflow shape graduates `experimental → supported` only when
it carries a **green RFC 0105 unattended-reliability fixture** that proves the run
**completes-or-escalates-loud without a human**, under a fault matrix:

- **lane death**,
- **transport churn**,
- **reviewer replacement**.

## The hard part to frame

Branch and convergence outputs are **model-generated ideas, not deterministic
data**. The fixture therefore *cannot* assert exact content. The tension every
branch must wrestle with: **how do you prove a non-deterministic, multi-model,
fan-out/fan-in workflow is reliable enough to be "supported" using a
deterministic CI fixture?**

## Goal — what a good answer delivers

A credible graduation path, and concretely a **fixture design**, that earns
`divergent_ideation` its `supported` status: it makes the unattended-reliability
claim checkable in CI, survives the fault matrix, and stays loud — escalating,
never silently wedging — whenever it cannot make progress.

## Hard constraints (record verbatim; any answer that violates these is invalid)

1. **No new daemon method.** The graduation must ride the existing RPC/MCP
   surface.
2. **No model call in any state transition.** Inference happens only inside lane
   work, never inside the daemon's advance logic.
3. **Unattended in CI.** The fixture runs with no human in the loop and must
   prove self-recover-or-escalate **within a budget** — never a silent wedge or
   unbounded wait.
4. **Non-deterministic outputs.** Assertions must be **structural/behavioral**
   (run-graph shape, artifact presence/kind, terminal state reached, escalation
   emitted, budget honored) — never content-exact ("branch 2 said X").
5. **Fault-matrix coverage.** Must demonstrate completes-or-escalates-loud under
   lane death, transport churn, and reviewer replacement.
6. **Product boundary holds.** No hosted services, cloud APIs, telemetry, or
   durable transcript capture/export introduced to make the fixture work.

## Non-goals (out of scope for this ideation)

- Redesigning the frame library, the anti-redundancy gate, or the min-structure
  gate.
- Changing the compiled v1 fan-out shape — its edges, job kinds, or fan width.
- Improving the *quality* of the ideas the workflow produces. Graduation gates on
  reliability, not cleverness.
- Adding a new daemon method or a new workflow-DSL primitive.
- Any human-in-the-loop or hand-graded acceptance step. The whole point is
  unattended.

## Decision criteria (how convergence will score a good answer)

- **Unattended-provable:** the reliability claim is verified by the fixture with
  zero human intervention.
- **Clean determinism boundary:** non-deterministic content is isolated behind
  structural/behavioral assertions; the harness itself is deterministic and
  reproducible across runs.
- **Fault-matrix complete:** lane death, transport churn, and reviewer
  replacement are each exercised, and each ends in complete-or-loud-escalate.
- **Bounded and loud:** every failure path terminates within budget with a
  visible escalation — no silent wedge, no unbounded wait.
- **Minimal, boundary-safe surface:** no new daemon method, no model call in
  transitions, no new external dependency.
- **CI-maintainable:** stable and fast enough to live in CI without flaking on
  model variance.

## Inputs branches may assume

- The shape already compiles and runs; this run is itself a live instance of it.
- **RFC 0105** (unattended-reliability fixture pattern) and **RFC 0106**
  (experimental→supported graduation gate) are the governing decisions.
- The RFC 0105 fixture pattern is the established mechanism for this gate, and
  previously-graduated shapes are fair precedent to learn from and reuse.
