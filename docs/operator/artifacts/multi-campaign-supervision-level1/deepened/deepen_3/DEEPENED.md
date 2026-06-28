---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
author: deepener-reviewer-2-001
inputs:
  - CONVERGENCE.md
  - PROBLEM_BRIEF.md
---

# Deferral Quarantine And Scope-Drift Refusal

## Sketch

This pick treats every deferred or newly discovered slice as custody-bearing work, not as a comment, loose issue label, or dashboard footnote. When a coordinating agent encounters work outside the accepted arc, it must either refuse the drift or place the item in quarantine with a code, reason, owner, evidence handle, wake-up condition, and allowed re-entry path. The quarantine row is visible in the campaign status surface, but it is not itself accepted work: promotion back into the arc requires the same kind of authority receipt, fresh-context payload, and proof gate that ordinary planned stages use. This keeps the daemon-owned workflow state authoritative because quarantine records explain why work is paused or excluded while ordinary Striatum runs, leases, artifacts, and verdicts still decide execution. Fresh agents can restart from the quarantine ledger by seeing which slices are deliberately out of scope, which are waiting on human confirmation, and which have enough proof to become new accepted work. The design makes deferral a public mutation with custody and expiry, so silence no longer decays into acceptance.

## Load-Bearing Risk

The risk is that the quarantine ledger becomes a second unofficial state machine: if codes, expiry, and promotion rules are not tied to daemon evidence and human-accepted authority, agents will start treating ledger rows as permission to ignore or complete work.

## First Concrete Step

A builder should inventory the current Striatum surfaces that already express paused or non-final work -- blockers, human checkpoints, verdicts, action-item ledgers, issue references, recovery states, and operator reports -- then draft the smallest quarantine vocabulary that maps each code to an existing evidence surface or an explicit product decision gap.

## Child Ideas

- Deferral custody card: a structured ticket section with reason, owner, expiry trigger, evidence handle, and re-entry gate.
- Scope-drift refusal ledger: a visible list of discovered slices the meta-agent refused to absorb because they exceed the accepted arc.
- Quarantine promotion receipt: a small artifact proving the item has human acceptance, bounded context, and a target workflow before it re-enters the arc.
- Expiry pressure dashboard: a status view that sorts quarantined items by stale wake-up dates, missing proof, and blocked owner decisions.
- Deferral falsifier pass: a reviewer role that samples quarantined items and tries to prove one has silently become accepted or completed without authority.