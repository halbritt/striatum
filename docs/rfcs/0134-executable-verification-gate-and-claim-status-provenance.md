# RFC 0134: Executable verification gate and claim status-provenance

Status: accepted-with-revisions / implemented (D227 accept; verified-and-graduated D237; both build halves shipped, #394/#395 closed) — see "Accepted form" below
Date: 2026-06-17
author: proposer-claude-opus-4-8

## Accepted form (D227)

RFC 0134 is accepted **with revisions**. The accepted architecture is
**validate-not-execute**, and daemon-gate-path execution that inherits the
lane's `process.run` / `write_scope` posture is explicitly **rejected** (it is
a daemon-reach RCE/exfil surface against the PG socket, the runtime token, and
other repos). The accepted shape, which re-scopes build issue #395:

1. **Claim-status lattice first, no execution.** Ship `claim_ledger` with
   `VERIFIED > ASSERTED > DESIGNED` as a first-class artifact — monotonic and
   append-only at the daemon writer, demotable but never self-promotable, and
   auto-decaying `VERIFIED → ASSERTED` when bound input hashes change. This half
   is pure validation.
2. **Verification runs off the gate path, in a disposable sandboxed lane.** Any
   executable verification is its own job in a disposable sandboxed verifier
   lane (reusing the lane-sandbox + supervisor-helper machinery); the completion
   gate merely **reads** its durable receipt. A missing or wedged verify degrades
   the claim to `ASSERTED` — it never blocks completion on engine liveness.
3. **The daemon validates, never executes.** `checks[]` are content-addressed
   against an operator-curated, git-tracked allowlist (a lane *names* but never
   *authors* the executed bytes); the daemon only validates a tamper-evident
   transcript receipt (argv + resolved binary hash + exit code + stdout digest +
   cwd tree-sha) bound to the worktree tree-sha. `VERIFIED` (top rung) requires
   **two** signals (sealed receipt + independent re-execution agreement); a lone
   exit-0 earns only `ASSERTED`; timeout / envelope-violation / network-touch →
   `INDETERMINATE`, never `VERIFIED`, with cgroup-enforced caps so a runaway
   check kills its scope, not the daemon.

Build order: lattice + ledger + provenance-lint first; sandboxed off-gate-path
verifier second — **not** a daemon-gate-path executor.

**Implementation status (lattice slice, #395 — landed).** The first,
validation-only half is implemented: the `claim_ledger.v1` and `receipt.v1`
artifact contracts (`go/pkg/artifactcontracts/claim_ledger.go`,
registered in `contracts.go` with the publisher's exit-code-6 schema guard),
the claim lattice `VERIFIED > ASSERTED > DESIGNED`, the provenance lint
(`LintClaimLedger` — a claim's status may not exceed its evidence; `VERIFIED`
requires a bound receipt and a matching input digest), VERIFIED→ASSERTED
auto-decay on bound-input change (`EffectiveClaimStatus`), and the
daemon-writer cross-seal rules (`go/pkg/mutations/claim_ledger.go`,
`enforceClaimLedgerLattice` — monotonic append-only `ledger_seal`, demotable
but never self-promotable).

**Implementation status (executable slice, #395 — landed, validate-not-execute).**
The off-gate-path executable half is now implemented per the Accepted form
above (NOT the deferred §2–§3 daemon-gate-path executor, which D227 rejects):

- **`verify` job type** — widened onto the owner-held `jobs_job_type_check`
  via **owner bundle 0016** (`go/pkg/db/sql/owner/0016_verify_job_type.sql`,
  `LatestOwnerBundleVersion = 16`). Per D215 the job_type CHECK is owner-held,
  so this is an owner bundle (mirroring 0013), never a runtime migration.
- **Disposable sandboxed verifier LANE** (`go/pkg/verifier`) — the lane-side
  `striatum verifier run` command (`go/cmd/striatum/verifier.go`) resolves a
  named check against an operator-curated, git-tracked, content-addressed
  **allowlist** (`allowlist.go`: a workflow NAMES a check; it never AUTHORS the
  bytes — an unknown id or a binary whose sha256 drifted from the pinned hash is
  refused), runs it under the strictest available **sandbox envelope**
  (`sandbox.go`: bubblewrap → systemd-run → unshare+ulimit → none, reporting an
  honest resolved posture: no-network, no-new-privileges, read-only-except-scratch,
  and cgroup/cpu/mem/wall-clock caps so a runaway check kills its own scope, not
  the daemon), and mints a tamper-evident **`receipt.v1`** (`receipt.go`).
- **Two-signal VERIFIED** — the check runs TWICE; VERIFIED requires the sealed
  receipt PLUS the independent re-execution agreement under a **strict** sandbox
  (`classifyResult` / `EffectiveStatusFromReceipt`). A lone exit-0 earns only
  ASSERTED; a timeout / envelope-violation / non-strict posture / unstable
  read-only tree is INDETERMINATE → ASSERTED, never VERIFIED.
- **Gate READ (non-blocking)** — the run-completion gate
  (`go/pkg/mutations/claim_verifier_gate.go`, wired into `maybeCompleteRun`)
  loads the run's `claim_ledger` + the receipts its VERIFIED claims name and
  records the EFFECTIVE claim status on the `run.completed` event. It is a PURE
  READ: it executes nothing, adds NO failing gate, and a missing / wedged /
  timed-out verify degrades the claim to ASSERTED — it NEVER blocks completion
  on engine liveness.

**The daemon NEVER executes a check.** The only command execution in the whole
feature is in the verifier LANE (`striatum verifier run`), off the gate path;
the daemon validates sealed receipts and curates the executed bytes, exactly as
D227 binds. The §2–§3 daemon-run-`checks[]` design below is the REJECTED form
and is retained only as the problem statement the Accepted form supersedes.

**Graduation status (D237 — verified-and-graduated).** Both build halves are on
`main`, owner bundle 0016 is live (the owner DB carries `verify` in
`jobs_job_type_check`), and every accepted-form path is exercised end-to-end:

- *Live mint.* `striatum verifier run` against an operator-curated allowlist runs
  the check under a strict bubblewrap envelope and mints a sealed `receipt.v1` —
  a passing check classifies `verified_eligible` (strict + two-signal agreement +
  exit-0), a deliberately-failing check classifies `asserted`.
- *Connected mint→gate→decay proof.* The real sandboxed mint
  (`verifier.ExecuteCheck`) flows through the real daemon-side gate read
  (`evaluateRunClaimVerification`) in one regression
  (`TestRunClaimVerificationEndToEndRealReceiptMint`): on a strict host the
  two-signal receipt reads back VERIFIED (`two_signal_sealed_receipt`); re-minting
  over a CHANGED worktree tree yields a different seal, so the claim still naming
  the old seal auto-decays to ASSERTED (`receipt_seal_mismatch`) — and on a
  degraded (non-strict) host the same path asserts the fail-safe (never VERIFIED).
- *Validation half.* The provenance lint refuses a hand-authored VERIFIED without
  a bound receipt / matching digest (publisher exit 6); the daemon writer is
  monotonic (demotable, never self-promotable); the receipt round-trips its
  schema and seal (tamper-evident).
- *Operator legibility.* The verified-vs-asserted ledger the gate derives is
  frozen on the `run_completion_record` (`run.summary` projects it) and rendered
  as a deterministic `## Claim Verification` section in the evidence export.

The accepted form is **non-blocking by design**, so there is no run-blocking
behavior to dogfood; the executable lane mint + the daemon gate read are the
whole 0134-specific surface, and both are proven above (a full supervised
`verify`-job dogfood would only re-exercise the generic lane plumbing every other
dogfood already covers). The interim [`examples/verification-gate-flow/`](../../examples/verification-gate-flow/)
stays the **portable** today-primitives demonstration: a runnable real-`verify`
example cannot be a committed fixture because the allowlist content-addresses
exact binary bytes (host/distro-specific by design); a *generatable*
`verification_gate` shape that scaffolds the workflow + a template allowlist is
tracked as a follow-up (it does not block graduation).

Context:
- The **cam-analyzer dogfood** (`~/git/cam-analyzer`, a lane-produced product;
  review `CAM_ANALYZER_DEEP_ARCHITECTURE_REVIEW_CLAUDE_OPUS_4_8_2026-06-17.md`).
  An adversarial post-hoc audit found the lane's *documentation* outran its
  *implementation*: a decision-log entry (D013, "bracketed verdict-agreement")
  marked accepted/implemented but **absent from code**; an RFC pillar described as
  load-bearing yet unimplemented; "unconstructable"/"enforced" guarantees the code
  does not earn; and a **fabricated frontier-model attribution** the run's own
  retrospective caught but never patched. The defects cluster on one axis —
  *honesty of completion claims* — and none was caught **inside** the lane.
- [`examples/falsification-gate-flow/`](../../examples/falsification-gate-flow/) —
  the `holder → falsifier → adjudicate → commit` topology this RFC specializes
  from *claims about a proposal* to *claims about what was built*.
- [`examples/verification-gate-flow/`](../../examples/verification-gate-flow/) —
  the interim, **today-primitives** realization shipped alongside this RFC
  (`striatum workflow validate` → `valid`). It demonstrates the pattern but
  inherits the gap below.
- [RFC 0118](0118-gate-run-completion-on-attested-provenance.md) — the
  run-completion provenance gate (`go/pkg/mutations/run_completion_gate.go`,
  `verifyRunCompletionProvenance` / `escalateProvenanceGateFailure`) this RFC
  feeds with a *mechanically attested* verdict.
- [RFC 0125](0125-durable-gate-artifact-provenance.md) — durable gate-artifact
  provenance; the witness record proposed here is an extension of that durability
  to *executable* evidence.
- Prior art in source: `go/pkg/mutations/revision_routing.go`
  (`matchRevisionCycle`, `routeRevisionCycle`, `countRevisionRoutings` — the
  `on_verdict: needs_revision` backward edge); `go/pkg/mutations/collaboration_ledger.go`
  (`enforceCollaborationLedgerVerdict` — front-matter verdict binding);
  `go/pkg/workflowauthoring/workflow.go` (job-type validation, `roles` map,
  `cycles`, `edges`); `go/pkg/artifactcontracts/contracts.go` (the `test_report`
  artifact kind).

> **Self-applied discipline (this RFC eats its own dog food).** The single
> load-bearing claim was `ASSERTED`, then **`VERIFIED` against source — and
> refined when the witness contradicted the first wording.** The engine *does*
> shell out and gate on exit codes, but only for its own **git/worktree
> plumbing**: `go/pkg/mutations/worktree.go:638` gates on `result.ExitCode` from
> `runGitWorktreeCommand("worktree","remove",…)`, and `go/pkg/mutations/run.go:815`
> runs `git rev-parse --verify`. What is genuinely absent is a
> **workflow-declarable** job type that runs an *arbitrary check command* and
> derives a *job verdict* from its exit code — confirmed by the job-type handling
> in `go/pkg/workflowauthoring/lint.go` (`type` defaults to `generic`; the
> verdict-bearing types are `review` / `phase_synthesis`, neither of which
> executes a declared check). So the execution-and-gate primitive **already exists
> internally and would be *exposed*, not built from nothing.** Remaining "current
> behavior" claims are `ASSERTED` from a structured read of the cited files.

## Problem

Lane runs recurrently ship products whose **completion claims are asserted by a
producer and never checked against running code**. Striatum already has gates —
but every gate adjudicates *prose*:

- The job-type set (`build`, `draft`, `review`, `synthesis`, `phase_synthesis`,
  `generic`) is **LLM-prose-in, artifact-out**. The engine shells out and gates
  on exit codes for its own git/worktree plumbing (`worktree.go:638`,
  `run.go:815`), but **no *workflow-declarable* job type runs an arbitrary check
  command and derives a job verdict from it.** A `test_report` artifact *kind*
  exists (`artifactcontracts/contracts.go`), but the engine never *produces* it by
  executing a test — an agent must run the test and publish the report, and
  nothing forces the agent to actually run it.
- Consequently the "verifier" in any gate (`adjudicate` in the falsification flow)
  is an LLM reading evidence and forming a verdict. **An LLM adjudicator can be
  talked into "looks implemented" the same way the producer was** — the failure
  mode the cam-analyzer dogfood exhibits.

The decision log conflates *decision status* with *capability status*: a row marked
`accepted` ("we decided to") is read as "it exists." There is no machine-checkable
notion of *whether the thing is built*, so "designed" silently passes as "done."
This is the same error the dogfood product exists to forbid one level down —
**asserted-as-verified is inferred-as-measured.**

## Goals

1. A first-class **claim status lattice** `VERIFIED > ASSERTED > DESIGNED` carried
   by a durable `claim_ledger` artifact, where `VERIFIED` *requires* a runnable
   witness (a test id, a `grep`, a CLI command + expected output, or a `mypy`
   invocation). No artifact may carry completion language above the status its
   witness earns.
2. A **`verify` job type the engine executes**: it declares `checks[]` (a command
   + a pass condition), the daemon runs each in the lane worktree, captures exit
   code and an output hash, and **derives the verdict mechanically** — `pass` iff
   every check passes — rather than asking an LLM to judge.
3. **Reuse the existing gate machinery**: a failing check emits
   `verdict: needs_revision`, routing backward through the existing `cycles` /
   `revision_routing.go` edge; the mechanically-attested verdict satisfies the
   RFC 0118 run-completion provenance gate, so a run **cannot complete** with an
   unmet executable check.

## Non-Goals

- Replacing LLM/human adjudication for *taste, design, and trade-off* questions —
  those stay `review`/`phase_synthesis` jobs. This gate is for *completion*, which
  is binary and checkable, not for *quality*, which is not.
- Building CI infrastructure or a hosted runner. Checks run in the same lane
  worktree and adapter that already execute agent tool calls; this respects the
  single-operator, local-first constraint.
- A general sandbox/permission model beyond the existing lane `write_scope`. Check
  commands inherit the lane's existing execution surface (see Open Questions on
  timeout/network).
- Auto-generating witnesses. The builder authors them; the gate only runs them.

## Proposal

**1. `claim_ledger` artifact kind.** A new artifact kind (extending the
`test_report`/`collaboration_ledger` family in `artifactcontracts/contracts.go`)
whose body is a table of rows `{claim, status, witness}` with YAML front matter
carrying the rolled-up `verdict`. `status ∈ {DESIGNED, ASSERTED, VERIFIED}`;
`witness` is required for `status > DESIGNED` and is one of `{test, grep, command,
file_line, mypy}` with the literal invocation and its pass condition. The existing
`enforceCollaborationLedgerVerdict` front-matter binding extends to gate on the
rolled-up verdict.

**2. `verify` job type.** A job `{"type": "verify", "checks": [...]}` where each
check is `{id, claim_ref, command, pass_when: "exit_zero" | "stdout_contains" |
"artifact_present", expect}`. The daemon (a new handler beside
`go/pkg/mutations/`) executes each check in the job's lane worktree, records
`{exit_code, stdout_sha256, passed}` per check into the `claim_ledger`, and sets
the job verdict to `pass` iff all checks pass, else `needs_revision`. The verdict
is **derived, not authored** — no model in the loop for the pass/fail decision.
The command-execution-and-gate plumbing already exists for internal git
operations (`runGitWorktreeCommand` → `result.ExitCode`, `worktree.go`); V1
*exposes* it as a job-level primitive rather than inventing it.

**3. Wiring (no new gate machinery).** Author flows as
`builder → verify → adjudicate? → commit`:
- `verify`'s `needs_revision` routes back to `builder` via an existing `cycle`
  (`matchRevisionCycle`/`routeRevisionCycle`, bounded by `max_iterations`).
- The mechanically-attested `verify` verdict is a provenance-required review for
  RFC 0118, so `verifyRunCompletionProvenance` blocks run completion (state
  `needs_operator`, `stop_reason='provenance_gate_failed'`) until every executable
  check is met or an operator records an explicit override decision.
- An optional `adjudicate` (LLM) stage remains for *interpreting* a failure
  ("is this witness even testing the claim?"), but it can only *lower* a `pass`,
  never raise a `fail` — the executable result is the floor.

**4. Status-provenance lint (the cheap mechanical guard).** A doctor check
(beside `go/pkg/reads/`) that scans published docs for completion language
("implemented", "enforced", "done", "✓") and cross-references the `claim_ledger`:
any completion word not backed by a `VERIFIED` row with a passing witness raises a
warning. This is the lane analogue of the dogfood product's own dependency-guard
test — *the lane held its product to a rule it never applied to its own output.*

**5. Interim path (ships now).** Until the `verify` job type lands,
[`examples/verification-gate-flow/`](../../examples/verification-gate-flow/)
realizes the pattern on **today's primitives**: a `verifier` *build* job whose
agent runs the witnesses with its own tools and publishes a `test_report`, gated
by `collaboration_ledger` + `cycle`. Its honest limitation, stated in its README:
the determinism lives in the verifier *agent*, so a verifier that *skips* running
a witness is only caught socially. RFC V1 moves that determinism into the engine.

## Acceptance Criteria

- A `verify` job runs its declared `checks[]`, records per-check exit codes and
  output hashes into a `claim_ledger`, and sets `pass` iff all pass — proven by a
  dogfood run with one passing and one deliberately-failing check.
- A failing check routes `needs_revision` to the prior job (bounded) and, if
  unresolved, blocks run completion through the RFC 0118 gate (state
  `needs_operator`).
- `striatum workflow validate` rejects a `claim_ledger` whose row is `VERIFIED`
  with an absent or malformed witness.
- The status-provenance doctor flags a published doc that says "implemented" for a
  claim whose ledger status is `ASSERTED`/`DESIGNED`.
- Re-running the cam-analyzer build through this gate would have caught D013
  (claimed implemented; no test names it) **inside** the lane, before release.

## Open Questions

- **Execution surface for engine-run checks.** Timeout, network, and resource
  bounds for a daemon-run `command`; does it reuse the lane adapter's process
  surface or a constrained one? (The `process` lane adapter already shells out;
  the question is policy, not mechanism.)
- **Determinism of witnesses.** A flaky or environment-dependent witness makes the
  gate flaky. Do we pin a witness to a recorded output hash and treat drift as a
  distinct `stale_witness` state rather than `fail`?
- **Verdict vocabulary.** `revision_routing.go` currently supports only
  `needs_revision`. Does `verify` need a distinct terminal `blocked_failing` for a
  check that can't pass without new work (vs. one a revision can fix)?
- **Who owns the witness command's trust.** A builder could author a witness that
  passes vacuously. The `adjudicate`-lowers-only stage and the "witness must
  exercise the claim" rule mitigate socially; is a structural check possible?

## Domain Modeling

Per [`docs/DDD.md § "Adding to the model"`](../explanation/domain-driven-design.md#adding-to-the-model)
(RFC 0019 precedent):

- **`ClaimLedger`** — a **value object** carried as a gate artifact: an immutable
  set of `Claim{text, status, witness}` rows with a rolled-up verdict. Equality by
  value; it is published, not mutated.
- **`Claim.status`** — a **value object** over the `VERIFIED > ASSERTED >
  DESIGNED` lattice, exactly mirroring the dogfood product's `MEASURED > INFERRED
  > EXTRAPOLATED` provenance lattice. The ordering is the point: a stage may only
  ever *lower* a claim's effective status (an LLM adjudicator cannot raise an
  executable `fail`), never raise it — the lane's "sealed mint."
- **`verify` job** — a new `job_type` on the **jobs aggregate**
  (`go/pkg/workflowauthoring/workflow.go` validation + the jobs table
  `job_type` CHECK).
- **`CheckEvaluated`** — a **domain event** (per check: command, exit code,
  output hash, passed) appended to the run's event log, the executable analogue of
  the existing `revision.cycle_routed` event.
- **Boundary clarification** — this RFC draws the line between *completion*
  (executable, mechanically gated here) and *quality* (interpretive, left to
  review jobs). Gates that today blur the two are the recurrence vector.
