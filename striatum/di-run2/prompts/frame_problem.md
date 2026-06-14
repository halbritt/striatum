Publish a concise problem brief at the declared artifact path (PROBLEM_BRIEF.md).
Frame the open-ended design question below. Do NOT propose solutions — only frame
the space the divergence branches will explore: state the goal, the hard
constraints, the non-goals, and the decision criteria for a good answer.

QUESTION TO FRAME:

How should Striatum graduate the new `divergent_ideation` workflow shape from
`experimental` to `supported`?

Background the branches need: `divergent_ideation` compiles to a flat
`striatum.workflow.v1` fan-out — a `frame_problem` brief, N fresh-session diverge
branches (each under one cognitive "frame" — a vantage that distorts how the
problem is re-asked, generator-only, no evaluation), a convergence critic that
scores ideas on novelty/viability/fit + clusters + flags traps + picks the
top-K, K deepen jobs, and a final synthesis. Frames are a curated library
selected with an anti-redundancy gate (no two frames sharing >=2 distortion
axes) and a min-structure gate; branches round-robin across lanes so different
models carry different frames. Per RFC 0106, a shape graduates to `supported`
only with a green RFC 0105 unattended-reliability fixture that proves the run
completes-or-escalates-loud without a human, under a fault matrix (lane death,
transport churn, reviewer replacement).

The hard part to frame clearly: branch and convergence outputs are
**model-generated ideas, not deterministic**, so the fixture cannot assert exact
content. Hard constraints to record in the brief: no new daemon method; no model
call in any state transition; the fixture must run unattended in CI and prove
self-recover-or-escalate within budget (never a silent wedge); outputs are
non-deterministic so assertions must be structural/behavioral, not content-exact.
