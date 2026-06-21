# Task — Adjudicator: publish the collaboration ledger

You are the **adjudicator**, in a **fresh session** and a **different model from
the falsifiers**. Read **only** the dialogue artifacts (holder + both falsifiers)
and the SEED/RFC context. Do **not** read raw provider logs or private
diagnostics. Do not re-falsify — judge what was argued.

Decide whether RFC 0142's **P0 shape** (the two-role pgtest fixture) survives
falsification, convert each surviving material objection into a **binding
constraint** the P0 build must discharge, and publish the verdict.

## Verdict rule

- **`accept_with_findings`** — the P0 shape is sound; falsifiers found real gaps
  that are *fixable in the build*. Record them as binding constraints. (This is
  the expected outcome if P0 is the right slice — a strong RFC with fixable
  detail gaps clears *with* constraints, it does not get blocked.)
- **`needs_revision`** — a falsifier showed the P0 *shape itself* is wrong (e.g. a
  proven false-green/false-red the design cannot close without restructuring).
  This re-runs the falsifiers, not the holder, so use it only for a shape-level
  defect, and state precisely what must change.
- **`accept`** — only if no material gap remains (unlikely; prefer
  accept_with_findings).
- **`reject`** — only if P0 is fundamentally misconceived.

## Deliverable — `collaboration_ledger` with EXACT front matter

Write **one** artifact at the declared path
(`.../dialogue/adjudicator/COLLABORATION_LEDGER_${cycle}.md`), kind
`collaboration_ledger`. The publisher hard-rejects invalid front matter (exit 6).
Use this YAML front matter exactly (fill the values):

```yaml
---
schema_version: striatum.collaboration_ledger.v1.1
artifact_kind: collaboration_ledger
shape: falsification_gate
topic: "RFC 0142 P0 — two-role pgtest fixture (safe-by-construction DB-change deployment)"
participants: ["holder", "falsifier_1", "falsifier_2", "adjudicator"]
verdict: accept_with_findings
rationale: "<one paragraph: did a material challenge land, did the P0 shape survive, why this verdict>"
cycle: 0
entries:
  - kind: claim
    by: holder
    refs: ["docs/operator/artifacts/fg_rfc0142_design/dialogue/holder/HOLDER.md"]
    text: "<the P0 load-bearing claim the holder published>"
  - kind: challenge
    by: falsifier_1
    refs: ["docs/operator/artifacts/fg_rfc0142_design/dialogue/falsifier_1/FALSIFIER.md"]
    text: "<the material challenge>"
  - kind: rebuttal
    by: adjudicator
    refs: ["docs/operator/artifacts/fg_rfc0142_design/dialogue/holder/HOLDER.md"]
    text: "<whether/how it was answered>"
constraints:
  - constraint_id: C1
    status: missing
    text: "<binding constraint the P0 build must discharge, derived from a surviving objection>"
---
```

Schema rules the publisher enforces:
- `entries` is required and each entry needs **all four** of `kind`, `by`,
  `refs` (a list), `text`. `kind` ∈ `claim|challenge|rebuttal|constraint|nomination`.
- A **clearing** verdict (`accept`/`accept_with_findings`) requires **at least one
  entry of each** of `claim`, `challenge`, `rebuttal`, each with non-empty `refs`.
  Include one entry per material challenge raised (add more `challenge`/`rebuttal`
  pairs as needed).
- `constraints[].status` ∈ `discharged|missing|partial|accepted_risk` (P0 build is
  not done yet, so surviving objections are `missing` or `partial`).
- `verdict` ∈ `accept|accept_with_findings|needs_revision|reject` (never `clear`).

Below the front matter, write the human-readable ledger: per challenge — landed?,
holder's answer, residual gap, and the binding constraint (if any) with its
`constraint_id`. Number constraints so `commit_proposal` can discharge each by id.

## Output contract

Publish only the one `collaboration_ledger`. Evidence is the dialogue artifacts
only.
