# Task — Committer: publish the build-ready P0 spec

You are the **committer**. Run only after the adjudicator's `collaboration_ledger`
records a **clearing verdict** (`accept` or `accept_with_findings`). Read the
holder artifact, both falsifier artifacts, and the collaboration ledger.

Your artifact is the **deliverable of this whole run**: a **build-ready P0 spec**
the downstream implementation run will build test-first. It is the holder's P0
spec, sharpened so that **every binding constraint from the ledger is discharged
inline** — do not weaken or drop any constraint the adjudicator recorded.

## Deliverable — `synthesis` with EXACT front matter

Write **one** artifact at the declared path
(`docs/operator/artifacts/fg_rfc0142_design/commit/proposal/PROPOSAL.md`), kind
`synthesis`. The publisher hard-rejects invalid front matter (exit 6). Front
matter exactly:

```yaml
---
schema_version: striatum.synthesis.v1
artifact_kind: synthesis
inputs:
  - "docs/operator/artifacts/fg_rfc0142_design/dialogue/holder/HOLDER.md"
  - "docs/operator/artifacts/fg_rfc0142_design/dialogue/falsifier_1/FALSIFIER.md"
  - "docs/operator/artifacts/fg_rfc0142_design/dialogue/falsifier_2/FALSIFIER.md"
  - "docs/operator/artifacts/fg_rfc0142_design/dialogue/adjudicator/COLLABORATION_LEDGER_0.md"
---
```

Body — the build-ready P0 spec:

1. **Objective.** One paragraph: what P0 delivers and the load-bearing claim it
   makes true (the two-role fixture as the CI oracle for the `42501` trap).
2. **Files to change**, with the exact role each plays
   (`go/pkg/pgtest/pgtest.go`, `go/pkg/db/migrations_test.go` / new `*_pg_test.go`).
3. **The two-role fixture design** — owner role vs `striatumd_rw`, ownership of
   authority/append-only tables, and the **exact mechanism** the migration suite
   uses to run as the privilege-constrained runtime role so `42501` fires
   (resolving the fixture-fidelity constraint from the ledger).
4. **The red regression test** — name, owner table touched, DDL attempted,
   expected `42501`, and how it asserts the *right* failure reason.
5. **The green control** — the legal runtime migration that must pass.
6. **Constraint discharge table.** One row per ledger `constraint_id`: how the P0
   spec discharges it (or, if it cannot be discharged in P0, an explicit
   `accepted_risk` with rationale — do not silently drop it).
7. **Out of scope / non-goals** — P1–P5, the proposed new symbols (`schema_state`,
   `deploy`, `rehearse`, …) are NOT in P0. P0 = test harness + test code, no
   runtime/owner migration, no daemon behavior change.
8. **Build plan for the implementation run** — an ordered, test-first checklist
   (write the red test first; provision the fixture; make it green for the control
   and red for the violation), plus how to verify (which `go test` package; note
   the PG-backed suites need a real cluster, so the builtin no-network verifier
   covers build/vet but not the PG test — call this out).

## Output contract

Publish only the one `synthesis`. Do not weaken any adjudicator constraint. Do not
edit source or touch `.striatum/`.
