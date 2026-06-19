# verification-gate-flow

A reusable gate that makes **completion** a checked property instead of a word a
producer types. It specializes the [falsification-gate](../falsification-gate-flow/)
pattern from *claims about a proposal* to *claims about what was built*.

```
builder ──▶ verifier ──▶ adjudicate ──▶ commit_verified
   ▲                          │
   └──────── needs_revision ──┘   (cycle, bounded by max_iterations)
```

- **builder** ships the slice **and** a `CLAIM_LEDGER.md`: every capability gets a
  status (`VERIFIED` / `ASSERTED` / `DESIGNED`) and, above `DESIGNED`, a runnable
  witness (a test, a `grep`, a CLI command, a `mypy` run).
- **verifier** (fresh session) *runs* each witness itself and publishes a
  `VERIFICATION_REPORT.md` of exit codes and PASS/FAIL — ground truth, not prose.
- **adjudicate** (fresh session) publishes a `collaboration_ledger`; any claim
  stated above the status its witness earns → `needs_revision`, which the cycle
  routes back to the builder.
- **commit_verified** publishes only after `accept`, stamping each claim with its
  earned status.

## Why

Lane runs recurrently let ideation/doc stages mark features "accepted/implemented"
that the build stages never delivered (see the cam-analyzer dogfood: a decision
log claiming a "bracketed verdict-agreement" feature absent from code). The lane
has gates, but they adjudicate *claims*, not *ground truth* — and an LLM
adjudicator can be talked into "looks implemented" the same way the producer was.

## Today vs. the RFC

This example works on **existing primitives** (build/review/synthesis jobs +
`cycles` + the `collaboration_ledger` verdict gate): the determinism lives in the
**verifier agent's** own command execution, and the engine gates on the resulting
verdict. The engine itself still cannot run a command and gate on its exit code —
so a verifier that *skips* running a witness is only caught socially.

[RFC 0134](../../docs/rfcs/0134-executable-verification-gate-and-claim-status-provenance.md)
proposes closing that: a first-class `verify` job type the **engine** executes,
deriving the verdict mechanically from exit codes, plus a `claim_ledger` artifact
with the `VERIFIED > ASSERTED > DESIGNED` status lattice as a first-class contract.

## Generating the shape

This pattern is now a **generatable workflow shape** (RFC 0141, `experimental`):

```
striatum workflow generate --shape verification_gate --write
```

The generated workflow uses a **real `type: verify` job** that runs
`striatum verifier run` against the in-binary builtin checks
(`builtin:go-build` / `builtin:go-test` / `builtin:go-vet`) with **zero operator
JSON**, minting receipts the adjudicator gates on. It also scaffolds a hashless
`verification/allowlist.intent.json` (in the verify lane's `forbidden_paths`, so a
verified lane can never sanction its own checks) and a `.gitignore` for the
per-host `allowlist.pins.*` files. Builtin receipts cap at **ASSERTED**; reaching
**VERIFIED** requires an external check the operator pins
(`striatum verifier pin --host-here`) and attests.

This directory is retained as the **portable today-primitives demonstration**
(no host-specific pins, runnable anywhere): it shows the same build → verify →
adjudicate → commit gate built entirely on existing primitives, where the
verifier's *agent* runs the witnesses rather than the engine.
