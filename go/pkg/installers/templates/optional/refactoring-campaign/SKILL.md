---
name: refactoring-campaign
description: Drive a behavior-preserving refactoring campaign end-to-end through striatum — instantiate the three-stage workflow chain (goal selection, falsified plan gate, sliced execution), prepare and start each run, hand artifacts between stages, and stop at refusal gates. Use when the user asks to run a refactoring campaign, invokes /refactoring-campaign, or wants a goal-adjudicated refactoring with provenance against a registered target repository.
---

# Refactoring Campaign

Orchestrate the three-stage campaign preserved at
`examples/refactoring-campaign/` in the striatum repo: stage 0 selects one
named goal (implementation-panel graph), stage 1 gates a falsified plan
(falsification-gate graph), stage 2 executes bounded slices with a
preservation review (code-change graph). Each stage is a separate striatum
run; each terminal artifact is the next stage's input contract.

This skill is the campaign driver. For run mechanics defer to the
generated skills: `striatum-scaffold` (prepare/branch), `striatum-supervise`
(lane driving), `striatum-recover` (wedges), `striatum-claim-loop` (packet
work). The daemon is a hard prerequisite for every verb.

## Inputs

- **target repo** — a registered repository (default: current repo).
- **campaign slug** — short kebab-case name for this campaign.
- **goal** (optional) — if the user names a concrete refactoring goal,
  skip stage 0 and author `GOAL_DECISION.md` directly as an
  operator-owned decision (`owner: human` — honest: the human named it).
- **lanes** (optional) — default multi-family: claude authors/synthesizes,
  codex + agy on panels and reviews. See [REFERENCE.md](REFERENCE.md).

## Quick start

```sh
# 1. Instantiate campaign workflows into the target repo (rewrites
#    artifact roots to striatum/refactoring/<slug>/, unique workflow ids).
scripts/instantiate.sh --slug <slug> --target-repo <path>

# 2. Bind real lanes in each stage workflow.json (REFERENCE.md §Lanes),
#    then per stage, in order 0 → 1 → 2:
striatum --repo <path> workflow validate --allow-same-model-pairing \
  striatum/workflows/refactoring-campaign-<slug>/stage-N-*/workflow.json
striatum --repo <path> run prepare --workflow <that path>   # + branch confirm
striatum --repo <path> run start --run-id <id>
# run start auto-drives the run (#212): it registers + supervises a lane per
# role/lane as the DAG unblocks, no operator process in the loop. Just wait for
# terminal (pass --no-drive on run start if you want to drive it yourself):
scripts/wait-run.sh <run-id> 60 <path>
```

## The campaign loop

1. **Stage 0 — goal selection.** Run it; read
   `striatum/refactoring/<slug>/00-goal/GOAL_DECISION.md`. If the
   arbitrator refused all candidates → **campaign stop**, report why.
   Skipped entirely when the user supplied the goal.
2. **Stage 1 — plan gate.** Run it; read `01-plan/GATE_SUMMARY.md` and
   the adjudicator ledger. Cleared → continue. `needs_revision` exhausted
   or gate **refused** → campaign stop. At most ONE fresh stage-1 rerun,
   and only for substance failures, never for a refused gate.
3. **Stage 2 — execution.** Before `run prepare`, set
   `execute_slices.write_scope.allowed_paths` from the committed plan's
   blast radius and add path-shaped frozen surfaces to `forbidden_paths`
   (workflows are frozen at prepare — edit first). Run it; read
   `02-execution/PRESERVATION_REVIEW.md` and `FINAL_REPORT.md`.
4. **Integrate.** On an `accept` verdict, land the run worktree via the
   serialized `run.integrate` (never hand-merge; `merge_conflict` is an
   operator decision). Report the final report path and commit map.

## Stop matrix

| Signal | Action |
|---|---|
| Stage 0 arbitrator refuses all goals | Stop; surface the refusal rationale |
| Stage 1 gate refused (undischargeable constraint) | Stop; this is the gate working |
| Stage 1 substance `needs_revision` exhausted | One fresh stage-1 run max, then stop |
| Stage 2 ledger records an honest stop condition | Report it; the operator decides; do not stretch slices |
| Run wedges (stale lease, dead lane) | `striatum-recover`; never advance state by hand |

An honest stop with a truthful ledger is a successful campaign outcome,
not a failure. Drive autonomously between gates — do not insert human
confirmation prompts the gates don't require — and report outcomes
faithfully from daemon reads, never from assumptions.

Details, lane bindings, and driving discipline: [REFERENCE.md](REFERENCE.md).
