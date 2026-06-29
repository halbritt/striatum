# Commit The Acceptance + D178

Publish the acceptance only after the adjudicator ledger records a clearing
verdict. Create `docs/rfcs/0119/ACCEPTANCE.md` with `striatum.synthesis.v1`
front matter and the exact lowercase `author:` byline:

```yaml
---
schema_version: striatum.synthesis.v1
artifact_kind: synthesis
---
```

If the ledger **refused** the gate, write a short refusal note instead,
naming the undischarged constraints — do not publish an acceptance.

On a clearing verdict, the acceptance records:

- The RFC 0119 status flip to `accepted` and each `binding` constraint from
  the ledger with where the RFC text discharges it.
- The proposed decision-log entry **D178** (ready to paste into
  `docs/decisions/decision-log.md`): the decision, rationale, consequence,
  and revisit trigger, in the table-row style of the existing log. Write it
  to `docs/decisions/D178.md` for the operator to merge into the log.
- The explicit reminder that **no Go code lands in this run**; the
  `RecallMemory` + scaffold injection are the operator's follow-up, gated on
  the corpus guardrails staying green.

Stay inside the declared write scope; never write to `.striatum/` or `.git/`.
