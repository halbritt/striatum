# RFC 0166: completion deadline for an alive-but-never-completing lane (sealed-progress silence budget)

Status: proposed
Date: 2026-06-23
author: proposer-claude-opus-4-8

## Summary

A supervised lane can stay **alive but never complete**. A flailing agent (e.g.
a Claude reviewer looping) keeps emitting PTY output, so its supervisor renews
the work-lease's `expires_at` every ~3.3 min (`lease.heartbeat`, source
`supervisor_pty_progress`). The stalled-lease classifier treats PTY output as
progress, so this lane is **indistinguishable from a healthy slow lane** — one
observed run held a `review_design_claude` lane `running` for **1h42m** without
ever calling `work.complete`/`review.submit`. `recovery requeue-stale` refuses
(the lease is warm), and the only exits are an operator `supervise stop` or a
`run cancel` that kills the whole run (issue #576).

Prior liveness work (#147 / #309 / #311 / RFC 0101) hardened detection of
**dead / no-heartbeat** lanes; the #324 wedge guard (`StallToolProgress` /
`wedged_no_tool_progress`) catches a lane that has tool-call history but stops
making **tool-call** progress. Neither covers the **alive-and-loud but never
forward-sealing** case: the lane heartbeats, may even keep calling tools, yet
produces no daemon-recorded sealed work and never converges.

This RFC asks for a product decision on a **completion deadline that is
independent of lease heartbeats**, that escalates (`needs_operator`) or
auto-requeues a fresh session, **without false-killing a legitimately
slow-but-healthy lane** and **without a lane being able to game it by emitting
fake progress**. No code, contract, default, or migration lands with this
proposal.

## Why this is a decision, not a direct FIX

The investigation behind #576 confirmed the **mechanism** is undecided, not just
unimplemented:

- There is **no max-duration field** anywhere today (`jobs` / `sessions` /
  `leases` carry `started_at` / `acquired_at`, never a cap), so any deadline is
  net-new persisted/derived state.
- At least three viable deadline **models** exist with materially different
  trade-offs (a per-job-type wall-clock cap; a no-sealed-progress stall class; a
  no-progress-velocity cap), and the cheap-looking one (a plain wall-clock cap)
  is a **trap** that false-kills healthy slow jobs.
- False-killing a healthy lane mid-work is the exact **CASE-2 artifact-loss**
  failure the #145 lease-heartbeat reprieve was built to prevent — a
  **product-safety** regression, which routes to RFC under the triage
  blast-radius rule.

Hand to `RFC_REVIEW.md` before acceptance.

## Design exploration (divergent ideation)

This direction was produced with a divergent-ideation pass (the ADHD skill: five
cognitive frames — regulator, 3am-on-call, hostile-competitor, logistics,
biology — each generating in isolation, then scored / clustered / trap-pruned /
top-3 deepened). Five independent frames **converged** on the same spine: a
deadline clock fed **only** by forgery-resistant sealed events, never by PTY
heartbeats. The full explored set and the rejected traps are in the appendix —
they are the falsification surface a design review should attack.

## Proposed direction

A **sealed-progress silence budget**: four composable parts, each grounded in
machinery that already exists in the daemon.

### Part 1 — the clock (detector)

A per-job deadline **derived**, not stored: the elapsed wall-clock since the
job's last **forgery-resistant sealed event**, computed by the existing
`jobSealedProgressAt(ctx, tx, repositoryID, jobID)`
(`go/pkg/mutations/recovery_decision_tree.go`, today
`GREATEST(max(artifacts.created_at), max(verdicts.created_at))`), with the
job's `started_at` / lease `acquired_at` as the floor when nothing has sealed
yet. PTY heartbeats keep the **lease** warm (the lane is alive) but cannot touch
this independent clock.

A new stall class `wedged_no_sealed_progress` is added next to
`StallToolProgress` in `go/pkg/sessionliveness/liveness.go`. Unlike the #324
rung it **cannot** be decided by the pure `Classify()` function (which sees only
the session activity columns, never the per-job `artifacts` / `verdicts`
tables); evaluation lives in `recoverStuckJobs`, which already holds the tx,
already SELECTs the lease/session/job rows, and already calls
`jobSealedProgressAt`. The seam is exactly the one that signal was built for —
it is currently consumed only as a *post-hoc debounce* inside the confidence
gate (`progressAdvanced`); this promotes it to a **first-class deadline that
originates a stall**.

### Part 2 — the correctness constraint (no false-kill): AND, not OR

The load-bearing risk is the **single-final-seal lane**: a legitimately healthy
job whose *only* deliverable is one terminal `work.complete` with no
intermediate artifacts/verdicts produces **zero** sealed events for its entire
lifetime, so a naive clock counting from `started_at` would cross any finite
budget while the lane is doing real work — re-introducing the #145 mid-work-kill
failure.

Resolution: the rung fires **only when BOTH** signals are stale — the sealed
clock is past budget **AND** the #324 tool-progress timeline is past
`ToolProgressSeconds`. A lane that is silent on sealed work but still actively
issuing tool calls (the single-final-seal job genuinely working) is **not**
trapped; only a lane that has gone quiet on *every* forgery-resistant axis and
is coasting on raw PTY noise trips. This `AND` is the central safety decision the
review must ratify.

### Part 3 — anti-gaming hardening (the non-obvious one): reset on novelty

A naive sealed clock is still gameable: a flailing lane could re-call
`artifact.publish` on an **unchanged** blob (same `content_sha256`) to reset the
clock without doing work. So a reset requires a strict increase in a
daemon-derived **monotonic progress position** — e.g.
`{count(distinct content_sha256), count(sealed verdicts), highest satisfied
expected_artifacts milestone index}` — recorded on the existing
`striatumd.job_recovery_state` row. The deadline **consumes novelty, not
events**: a re-published identical hash does not grow the distinct-hash
cardinality, so the cursor (and the clock) do not advance. The de-dup grain must
be `content_sha256` (so a single artifact revised with genuinely new bytes still
counts, while an idempotent re-anchor of identical bytes correctly does not).

### Part 4 — the action (self-heal ladder, telomere-bounded)

On the first breach, **do not page** — auto-requeue **one** fresh session
(cheap, invisible), reusing the existing `transfer_requeue` /
`requeueJobSameAttempt` path. The requeue budget (`recoveryPolicy.maxRequeues` /
`maxUnsealedRequeues` / `maxSilentSweeps`) only **resets on genuine sealed
progress**: a job that burns fresh sessions without ever sealing **shortens
toward a floor** (a telomere), at which point it can no longer auto-requeue and
escalates to `needs_operator` — carrying a single copy-pasteable recovery verb
(`recovery complete-stalled` if a durable artifact exists, else
`recovery requeue-stale --override`). A deterministically-flailing lane converges
to the floor in bounded sweeps instead of looping forever; the existing
`topologyAdaptiveSilentSweepCap` / lease-heartbeat reprieves are inherited, not
bypassed.

### Part 5 — the known-slow exception (operator grant as sealed record)

An operator grants a known-long job more time via a verb (`recovery
grant-silence <run-id> <job-id> --seconds N`) that writes a **forgery-resistant
daemon-recorded** event; the clock's floor reads
`GREATEST(sealedProgressAt, max(grant)…)`. The grant is auditable provenance, a
lane cannot self-grant, and one slow job is whitelisted without weakening the
default for everyone else.

### Rollout — shadow-first

Matching the codebase convention for risky boot/recovery gates (RFC 0135 barrier
cutover; #570/#578 schema-drift shadow), ship `wedged_no_sealed_progress` as an
**advisory** dashboard/doctor signal first, with `SealedSilenceSeconds`
defaulting to `0` (= disabled, the `ToolProgressSeconds` convention). Gather
real per-shape sealed-silence distributions from production runs, choose the
thresholds empirically (there is no historical corpus to set them a priori —
turn the absence into a corpus before the rung is allowed to escalate), then arm
the action. A false-positive deadline must never convert a healthy run into an
outage just by landing to `main`.

## Decision questions to pin (for RFC_REVIEW)

1. **Composition.** Ratify `wedged_no_sealed_progress` = sealed-clock-stale
   **AND** #324-tool-progress-stale (vs OR, vs sealed-only). This is the
   no-false-kill core.
2. **Thresholds + bake.** The default `SealedSilenceSeconds` (per-shape vs
   global), the shadow-bake duration, and the arming gate (env opt-in vs in-code
   default).
3. **Action order.** Auto-requeue-first-then-escalate (self-heal) vs
   escalate-first; and the telomere floor (how many unsealed requeues before
   `needs_operator`).
4. **Novelty grain.** Confirm `content_sha256` as the monotonic-progress unit and
   whether the milestone-index dimension ships in v1 or later.
5. **Persistence.** Whether Parts 3–5 need a migration on `job_recovery_state`
   (novelty cursor + grant floor) and the doctor block for an un-recorded grant.

## Alternatives / rejected direct patches

- **Plain wall-clock max-duration cap** (the issue's Option A, and the banned
  baseline) — *trap*: false-kills healthy slow jobs, an arbitrary constant, and
  a lane that sealed early then stalled is killed alongside a genuinely-slow
  one. The sealed clock dominates it.
- **Full p99 / peer-percentile statistical deadline as the *primary* model** —
  *trap*: cold-start; the self-host per-`job_type` corpus is tiny, so a velocity
  model now is premature sophistication. Kept as an optional later tuning layer
  over a simple configurable default.
- **Active liveness-challenge protocol** (daemon → lane `checkpoint.attest`
  request/response) — high integration cost: a new surface every agent harness
  must implement; the passive sealed clock achieves the goal without agent
  cooperation. Future option.
- **Per-shape declared milestone schedule as the deadline source** — pushes
  deadline policy onto every workflow author; the lighter "first-seal deadline +
  between-seal max-gap" captures most of the value.

## Blast radius

| dimension | hot? | note |
|---|---|---|
| product_safety_claim | **yes** | a deadline that false-kills a healthy lane regresses the #145 reprieve guarantee |
| persisted_schema / migration | **yes (Parts 3–5)** | novelty cursor + grant floor on `job_recovery_state` |
| security_or_authz | no | operator grant is daemon-recorded, lane cannot self-grant |
| public_api | minor | one new read-only/operator recovery verb surface |
| wire_format / cross_team | no | — |

## Appendix — divergent-ideation set (falsification surface)

Clusters that emerged across the five isolated frames (chips = novelty / viability / fit, 0–10):

- **Clock fed only by forgery-resistant sealed events, distinct from the lease**
  `[N6 V9 F10]` — the spine; every frame independently reached it.
- **Reset must consume monotonic novelty, not any event** `[N8 V7 F8]` — only the
  hostile-competitor frame caught the re-publish/replay hole.
- **Threshold = per-job-type baseline from history/peers, not a constant**
  `[N7 V6 F8]` — viable only as a later tuning layer (cold-start).
- **Per-gate / milestone deadlines between sealed transitions** `[N8 V6 F8]`.
- **Self-heal ladder: auto-requeue fresh, escalate only on repeat** `[N7 V9 F9]`.
- **Operator grant = forgery-resistant sealed extension token** `[N5 V9 F8]`.
- **Lane self-attests an ETA/plan, defends it via sealed RPC** `[N8 V5 F7]`.
- **Run-scoped sibling-fairness preemption** `[N8 V6 F7]` — a warm-but-sealless
  lane on the critical path that starves sealed siblings shortens faster (reuse
  `cohortHasFresherLiveness`); a strong v2 child idea.

Provocation left open for review: should the deadline be **whole-run-scoped**
rather than per-lane — escalate when the *run* makes no sealed advance while one
lane stays warm-but-sealless past its peers — folding #576 and the sibling-
starvation half of #579 into one run-progress invariant?

## References

- Issue #576 (surfacing run `run_01dd7111…`, iterated-interrogating-panel).
- `go/pkg/mutations/recovery_decision_tree.go` (`recoverStuckJobs`,
  `jobSealedProgressAt`, `applyConfidenceGate`, `recoveryPolicy`,
  `topologyAdaptiveSilentSweepCap`, `cohortHasFresherLiveness`).
- `go/pkg/sessionliveness/liveness.go` (`StallToolProgress` / #324 wedge guard,
  `Policy.ToolProgressSeconds`).
- `striatumd.job_recovery_state` (migration 0035 confidence columns).
- Lineage: #147 / #309 / #311 / RFC 0101 (dead-lane liveness); #324 (tool-progress
  wedge); #145 (lease-heartbeat reprieve, the false-kill guard this must not
  regress); #579 (idle-orphan reaping, the sibling-starvation cousin).
