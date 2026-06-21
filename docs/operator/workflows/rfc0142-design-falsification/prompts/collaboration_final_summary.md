# Task — Final summary

You are the **final summary** author. The collaboration gate has cleared and the
build-ready P0 spec is published. Read the collaboration ledger and the
`commit/proposal/PROPOSAL.md` synthesis.

## Deliverable — `synthesis` with EXACT front matter

Write **one** artifact at the declared path
(`docs/operator/artifacts/fg_rfc0142_design/commit/final/FINAL_SUMMARY.md`), kind
`synthesis`. Front matter exactly:

```yaml
---
schema_version: striatum.synthesis.v1
artifact_kind: synthesis
inputs:
  - "docs/operator/artifacts/fg_rfc0142_design/dialogue/adjudicator/COLLABORATION_LEDGER_0.md"
  - "docs/operator/artifacts/fg_rfc0142_design/commit/proposal/PROPOSAL.md"
---
```

Body — keep it tight:

1. **Verdict** — the ledger verdict and the one-line reason.
2. **What survived falsification** — the P0 load-bearing claim, intact or amended.
3. **Binding constraints the P0 build must honor** — list each `constraint_id` and
   how the published spec discharges it.
4. **Hand-off to the implementation run** — the single sentence the downstream
   build lane needs: build RFC 0142 P0 (two-role pgtest fixture + one red
   regression test + green control) per `commit/proposal/PROPOSAL.md`, test-first.
5. **Residual follow-ups** — anything deferred to P1–P5 or flagged but out of P0
   scope.

## Output contract

Publish only the one `synthesis`. Do not re-open the gate.
