---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
author: deepener-author-001
title: "Authority receipt expiration deepening"
run_id: "run_7899e132bf7996d49c9b81d0df905962"
inputs:
  - "docs/operator/artifacts/multi-campaign-supervision-level1/CONVERGENCE.md"
  - "docs/operator/artifacts/multi-campaign-supervision-level1/PROBLEM_BRIEF.md"
---

# Authority Receipt Expiration Deepening

author: deepener-author-001

## Sketch

Authority receipt expiration treats the human-accepted arc plan as a time- and stage-scoped grant, not as blanket permission for a coordinating meta-agent to keep launching work. Each design, build, verify, recovery, or discovered-slice transition must present a receipt that names the current scope, allowed next actions, forbidden actions, evidence consumed, unresolved deferrals, stop conditions, and expiry point. A fresh-context lane can then decide whether the receipt is still admissible by checking durable handles from the campaign artifacts, daemon run state, ticket or issue mirrors, Git/docs evidence, and verifier receipts, without trusting inherited chat history. If the receipt has expired or its evidence is incomplete, the meta-agent may only ask for renewal, narrowing, quarantine, or stop, rather than silently continuing the arc. Supporting ideas collapse into this shape: stage hall passes become per-stage receipts, spend-down budgets become receipt limits, customs-broker gates become admissibility checks, and deductible stop conditions become explicit renewal thresholds. The result is a portfolio supervisor whose default state is "no current authority unless the receipt proves it," while the daemon remains the live workflow state machine.

## Load-Bearing Risk

The hard risk is evidence laundering: a plausible receipt could renew authority from stale, partial, or prose-only claims unless admissibility requires concrete daemon, artifact, Git, doc, ticket, and verifier handles and treats missing or contradictory handles as a stop condition.

## First Concrete Step

Draft an `AuthorityReceiptV1` artifact contract and one worked transition example for a single RFC arc, with fields for scope, expiry, allowed actions, forbidden actions, evidence handles, deferrals, discovered slices, renewal criteria, and stop triggers. A later product build that makes receipts daemon-enforced rather than design provenance would need source, schema, authority-matrix, and UI/doc paths outside this Level-1 synthesis-only write scope.

## Child Ideas

- Stage receipts: separate receipt types for arc acceptance, design launch, build launch, verifier launch, recovery, and stop report.
- Child-slice receipts: discovered slices can only move from quarantine to work when a parent receipt grants bounded child authority.
- Receipt freshness tests: a blank lane must reconstruct the active authority and stop conditions from durable handles before renewal.
- Admissibility classes: mark each receipt as admissible, insufficient, expired, contradictory, or human-renewal-required.
- Dashboard pressure row: surface receipts by nearest expiry, missing evidence, unresolved deferrals, and authority-renewal requests.
