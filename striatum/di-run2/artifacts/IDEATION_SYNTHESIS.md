---
schema_version: striatum.synthesis.v1
artifact_kind: synthesis
author: final-synthesizer-gpt-5.5-001
inputs:
  - striatum/di-run2/artifacts/CONVERGENCE.md
  - striatum/di-run2/artifacts/deepened/deepen_1/DEEPENED.md
  - striatum/di-run2/artifacts/deepened/deepen_2/DEEPENED.md
---

# Ideation Synthesis

author: final-synthesizer-gpt-5.5-001

## Operator Result

Graduate `divergent_ideation` by proving the workflow's delivery mechanics,
not the model prose. The supported-status fixture should derive expected
workflow stops from the compiled graph, drive deterministic lane stubs through
the RFC 0105 fault matrix, and accept only two terminal outcomes for every
node: a valid daemon artifact receipt that closes the graph, or a named
escalation that preserves the failed lease/session context within budget.

## Shortlist

### 1. Last-mile delivery receipts

This is the main path. It gives CI a deterministic oracle for a
non-deterministic fan-out/fan-in workflow: compare daemon-recorded artifact
receipts against the compiled graph's expected `{logical_name, kind, path,
author_line, placement, attempt, terminal_transition}` tuples. The fixture
passes because the graph closed through declared receipts, not because any
generated idea text matched an expectation.

### 2. Exception-lane cross-dock

This supplies the required fault-matrix proof. Inject lane death, transport
churn, and reviewer replacement on a seeded schedule, then require each affected
packet to move to exactly one allowed terminal path: replacement completion
with valid receipts, or structured escalation with the original lease/session
context before the budget closes. This turns "completes-or-escalates-loud" into
an observable PostgreSQL state transition instead of a terminal convention.

### 3. Deterministic warehouse scan

This is the acceptance scanner for the receipt design. It should walk the run
graph, artifact receipts, artifact kind/path/byline, attempts, leases, terminal
states, and timestamps without reading generated prose. Its job is to make the
pass/fail boundary small enough to live in CI and loud enough that a red run
names the missing stop, duplicate receipt, stale lease, or late escalation.

### 4. * Adversarial structural lane stubs

This is the non-obvious but viable pick. Deterministic stubs can hide the very
variance the workflow will face in real use, so the fixture should make them
emit worst-legal structural outputs: empty-but-valid branches, duplicate
top-K selections, oversized artifacts, publish-then-die sequences, and reviewer
disagreement. That tests the receipt oracle and recovery routes against the
awkward legal edge cases rather than certifying only the convenient stub path.

## Trap List

- **Cryptographic mock signing:** signatures add accountability ceremony but do
  not prove the workflow completes, recovers, or escalates.
- **Transport proxy digest:** a proxy risks making the test certify a test-only
  transport dependency instead of the product RPC/MCP surface.
- **Budget price discovery, as stated:** "occasionally trips" is a CI flake
  unless tied to deterministic fault cases with expected escalations.
- **Cluster-count invariant:** cluster balance pressures semantic quality and
  violates the structural-only graduation boundary.
- **Flake tolerance:** tolerance thresholds can normalize regressions; the
  fixture should fail loud on deterministic structural variance instead.
- **Escalation premium as the gate:** escalation frequency is useful operating
  data, but promotion first needs proof that each controlled fault terminates
  complete-or-loud within budget.

## Wildcard Provocation

Make the graduation fixture include a "test of the test": feed the oracle a
small set of synthetic bad receipt ledgers, such as missing terminal receipt,
phantom artifact receipt, duplicate terminal receipt, and late escalation, and
require the oracle to reject each one with a precise graph diff. That shifts
the next direction from "can the workflow pass?" to "can the proof fail for the
right reasons?", which is the better confidence test before marking the shape
supported.
