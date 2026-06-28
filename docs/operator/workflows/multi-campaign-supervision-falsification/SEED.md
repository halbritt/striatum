---
type: record
status: working
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: falsification-gate-seed
author: operator-codex-gpt-5-004
created_at: 2026-06-28
updated_at: 2026-06-28
---

# MULTI_CAMPAIGN_SUPERVISION Falsification Seed

## Purpose

This workflow tests whether the completed Level-1 ideation synthesis is ready to
advance to a product-decision / RFC drafting gate. It does not accept a product
architecture and does not authorize implementation.

The claim under test is the Level-1 shortlist in
`docs/operator/artifacts/multi-campaign-supervision-level1/IDEATION_SYNTHESIS.md`:

- authority receipt expiration
- fresh-context replay test
- deferral quarantine and scope-drift refusal
- cross-surface contradiction gate

## Inputs

- Level-0 bootstrap artifacts under
  `docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/`
- Level-1 ideation artifacts under
  `docs/operator/artifacts/multi-campaign-supervision-level1/`
- current `docs/operator/BRIEF.md`
- `docs/how-to/how-to-agent.md`

## Hard Boundary

Do not produce source code, schema changes, route maps, UI plans, implementation
tickets, or design-to-build readiness. A clearing verdict means only that the
operator may draft an RFC/product decision or run a narrower follow-up design
gate.

## Falsification Focus

Falsifiers should try to break these points:

- authority receipts become ambient permission rather than stage-scoped proof
- replay packets are over-curated and hide contradictions
- quarantine records become accepted work by being visible
- ticket or dashboard surfaces become a second workflow state machine
- done claims do not reconcile daemon state, artifacts, Git/docs, tickets or
  issue mirrors, verifier receipts, and known deferrals
- the design depends on live chat history despite the fresh-context requirement
- the next gate skips a human/product decision before build planning

## Lane Choice

Use Codex-only lanes for this run. Claude lanes remain unavailable until
2026-06-30 15:59 UTC unless credentials or credits are explicitly restored.
This same-family pairing is a known limitation; independence comes from fresh
contexts, disjoint write scopes, explicit falsifier lenses, and the adjudicator
ledger, not from model-family diversity.

## Run Checks

Before starting the run:

```bash
./go/bin/striatum operator bootstrap --markdown
./go/bin/striatum status --json --run-limit 0
./go/bin/striatum workflow validate docs/operator/workflows/multi-campaign-supervision-falsification/workflow.json --json
```

Stop on a red doctor, active blocker, dirty unrelated checkout, or any pressure
to treat this as implementation authorization.
