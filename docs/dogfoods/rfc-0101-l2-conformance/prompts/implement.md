# Implement — the cleared RFC 0101 Layer 2 design

The design is synthesized and cleared (with required changes) by the
adversarial design panel. Build against it. You stay live (interrogable) — the
build-review panel will cross-examine you.

## Inputs

- `docs/dogfoods/rfc-0101-l2-conformance/artifacts/DESIGN_SYNTHESIS.md` — build to this.
- `docs/dogfoods/rfc-0101-l2-conformance/artifacts/review/design/*/REVIEW.md` — the
  panel's required changes. Address every one or justify why not.

## What to build (first concrete cut)

1. The conformance **contract** expressed as testable Go (the ordered clauses
   as a typed structure) — likely a new `go/pkg/adapterconformance` package.
2. A **single-adapter fixture runner** that drives the known-good `claude_code`
   adapter green end-to-end against a `pgtest`-provisioned daemon — this pins
   the contract.
3. The **lane-env hardening** changes in the lane-spawn path (deterministic,
   not prompt-steering) for #76/#85/#70.
4. The **`make` target** + CI wiring, with `agy` handled per the design's
   non-rotting skip.

## Constraints

- Stay inside `write_scope.allowed_paths`
  (`docs/dogfoods/rfc-0101-l2-conformance/artifacts/build/`, `go/`, `docs/rfcs/`).
  Never write `.striatum/`.
- Go-only. Build must pass `make -C go build`. Lint is
  `golangci-lint --default=none --enable=govet,staticcheck,errcheck,ineffassign`.
  Tests need PostgreSQL via `pgtest`
  (`STRIATUM_PG_TEST_URL=postgres:///postgres?host=/var/run/postgresql`).
- Add/update tests for behaviour you add (the conformance harness IS the test;
  make sure it actually runs and asserts).
- If a part is too large for one pass, land the load-bearing core and record
  the remainder as explicit follow-ups in your HANDOFF — do not fake
  completion. Reviewers verify against the real on-disk state.

## HANDOFF.md

Write `docs/dogfoods/rfc-0101-l2-conformance/artifacts/build/HANDOFF.md`: exactly
what you built (file paths + the test/`make` target a reviewer runs to
verify), which design-panel required changes you addressed and how, what you
deferred and why, and the exact validation commands (build, lint, the
conformance target).

Heartbeat (`work.heartbeat`) periodically during long local edits/builds. After
completing, remain available to answer build-panel interrogation questions.
