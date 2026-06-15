---
schema_version: striatum.synthesis.v1
artifact_kind: synthesis
author: deepener-claude-opus-4.8-001
inputs:
  - striatum/di-run2/artifacts/CONVERGENCE.md
  - striatum/di-run2/artifacts/PROBLEM_BRIEF.md
---

# Deepened Pick 1 — Last-mile delivery receipts (B3.6)

author: deepener-claude-opus-4.8-001

## The pick

Convergence ledger rank #1 (weighted 9.25): **the RFC 0105 graduation fixture
must judge `divergent_ideation` by the daemon-recorded artifact receipts emitted
at every upstream stop, never by ambient files in a worktree, and never by the
idea prose itself.**

## How it would actually work (sketch)

The fixture drives a real `divergent_ideation` run with deterministic lane stubs,
but its pass/fail oracle reads only one thing: the daemon's artifact-receipt
ledger — the records `artifact.publish` and `work.complete` already write at
state-transition time (the content-addressed `RUN_LEDGER` from #286 is the seed
of this surface). For every node the compiled v1 fan-out declares
(`frame_problem`, the N diverge branches, `convergence`, the K `deepen` jobs,
`final_synthesis`) the oracle asserts there is exactly one terminal receipt whose
tuple `{logical_name, kind, path, author_line, placement, attempt,
terminal_transition}` matches what the graph predicts for that node. Fan-in is
proven structurally: the `convergence` and `final_synthesis` jobs must have
consumed *exactly* the declared upstream receipt set — no missing stops, no
phantom extras. Because receipts are emitted by the daemon and are
content-addressed, the entire oracle is a deterministic function of the ledger
and never inspects a single generated sentence, which is precisely how it
survives the non-determinism constraint. Under the fault matrix a killed lane,
churned transport, or replaced reviewer simply changes *which* receipts appear
and on *which* attempt; the gate then asserts the terminal receipt set still
*closes the graph* — a replacement-attempt receipt or a named escalation receipt
at the affected node — within the lease/budget window. The check reproduces
identically across CI runs regardless of model drift, because model output is
opaque cargo and only the delivery record is read.

## Load-bearing risk

The receipt ledger must be **faithful and atomic with durable artifact
persistence**. The whole gate's validity rests on a receipt existing *if and only
if* the artifact actually landed and is valid — if the daemon can record a
`complete`/`publish` receipt for a job whose artifact never durably wrote (or
wrote invalid front matter), the oracle certifies a phantom delivery and ships a
false green. Any window between "receipt written" and "artifact exists/validates"
is the hole that sinks the design. (Secondary risk: the fixture must *derive* the
expected receipt set from the compiled graph, not hand-list it, or the oracle
silently rots the first time the shape's fan width changes.)

## First concrete step a builder would take

Dump the receipt ledger for **this very dogfood run** and diff it against the
compiled graph's expected receipt tuples. Concretely: read the existing
`RUN_LEDGER` / artifact-receipt schema (#286, commit `d9f428ce`) and confirm it
already carries `{logical_name, kind, path, author_line, placement, attempt,
terminal_transition}` per job. If it does, the receipt source already exists and
the fixture is *only* an oracle over existing data — satisfying "no new daemon
method." If a field is missing, *that gap* (not a new RPC) is the first thing to
close, and you have found it before writing a line of fixture code.

## Child ideas

1. **Receipt-set diff as the entire oracle (variation).** Collapse the gate to a
   single assertion: `terminal_receipt_set(run) == expected_receipt_set(compiled_graph)`.
   Build the expected set by walking the v1 fan-out the workflow compiles to, so
   the oracle auto-tracks fan width and never hard-codes "6 branches." Pass = exact
   match; fail = the symmetric difference is non-empty, and that diff *is* the
   error message — missing stops and unexpected extras both named loud.

2. **Escalation receipts as first-class graph stops (hybrid with pick #2, B3.2).**
   Treat a named escalation as a *legal* terminal receipt for a node. The same
   receipt-set oracle then covers the fault matrix for free: a killed lane closes
   either with a replacement-attempt receipt or an escalation receipt at that node,
   and the gate asserts the node reached *some* allowed terminal receipt within
   budget. This fuses "last-mile receipts" with "exception-lane cross-dock" into
   one assertion surface instead of two test families.

3. **Chain-of-custody annotation on every receipt (unlock, hybrid with B2.1).**
   Have each receipt also carry `lease_id`, `session_id`, `supervisor_id`, and
   `attempt`. The green condition stays pure set-equality, but on *failure* the
   custody fields convert a red CI into an immediately debuggable trace ("node
   `deepen_2` has no terminal receipt; last lease X on session Y expired at T").
   Custody is diagnostic-only, never part of the pass condition — so it adds
   debuggability without crossing the content boundary.

4. **Adversarial receipt fixtures — the test of the test (hybrid with B4.3).**
   Drive deterministic lane stubs that emit *worst-legal* shapes: duplicate
   publish, oversized artifact, publish-then-die-before-complete. Assert the
   receipt oracle still returns the correct verdict for each. This is the direct
   countermeasure to the load-bearing risk: it proves the ledger distinguishes
   "artifact present but job never reached terminal" from "delivered," closing the
   phantom-delivery hole on purpose rather than hoping it never opens.

5. **Freshness / attempt-monotonicity seal (variation, hybrid with B3.3).** Assert
   each node's terminal receipt comes from a fresh session and that attempt numbers
   are monotonic with no reused warm-session receipt. This catches a subtle
   false-green where a retried lane "delivers" by quietly reusing a prior attempt's
   artifact, and it makes both fan-out parallel-diversity (fresh-session branches)
   and fault recovery (clean retries) structurally checkable — still without
   reading a word of content.
