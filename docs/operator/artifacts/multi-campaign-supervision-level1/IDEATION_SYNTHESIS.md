---
type: record
status: working
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: adhd-level1-scout
author: operator-codex-gpt-5-003
created_at: 2026-06-28
updated_at: 2026-06-28
---

# MULTI_CAMPAIGN_SUPERVISION ADHD Ideation Synthesis

## Shortlist

### Star: Expiring Authority Envelopes

A campaign begins when the human accepts an arc plan that emits the first
expiring authority envelope as durable repo provenance, with scope, allowed RFC
set, budget, stop conditions, and required evidence named explicitly. The daemon
remains the live authority: the meta-agent can only request or coordinate
existing design, build, and verify workflows that fit the active envelope. At
each stage boundary, the meta-agent produces an admissibility packet saying what
changed, what evidence exists, what remains uncertain, and whether the next
envelope should narrow, renew, fork, or stop. Continuation requires durable
evidence such as accepted designs, review verdicts, issue updates, operator
reports, or verified artifacts satisfying the previous envelope's exit criteria.

Load-bearing risk: weak or ambiguous evidence could be laundered into renewed
authority, so the admissibility test must be stricter than plausible prose.

First concrete step: draft the Level-1 envelope template and transition
checklist with fields for scope, expiry, allowed actions, forbidden actions,
evidence required to continue, stop conditions, and exact durable artifacts
passed to the next context.

Child ideas:

- envelope renewal as a campaign-level analogue of lease renewal
- stage-specific envelopes such as design-only, verification-only,
  reconciliation, and stop-report
- evidence ledger entries classed as admissible, insufficient, or blocking
- child envelopes for discovered slices with independent stop conditions
- failed admissibility as an operator-facing decision point

### Campaign Tickets As Stage Passports

An arc starts with a human-accepted campaign passport stating parent arc, scope
boundary, authorized stages, explicit non-authority, and stop clauses. The
meta-agent reads passports and receipts from repo provenance, then asks the
daemon to launch ordinary workflows whose live state remains in PostgreSQL.
Each stage produces a new stamped passport with evidence receipts, unresolved
deferrals, handoff payload, and the exact context seed for the next fresh-window
agent. Discovered slices can be proposed only as child passports under the
parent arc, so scope growth is recorded before any workflow is allowed to run.

Load-bearing risk: the passport can become a shadow state machine competing
with daemon-owned PostgreSQL instead of describing authority and provenance.

First concrete step: draft a Campaign Passport V1 artifact contract with
allowed fields, forbidden powers, and mapping to existing workflow artifacts.

Child ideas:

- stage stamps: accepted, launched, verified, deferred, stopped, superseded
- stop clauses as explicit brakes for the meta-agent
- child passport proposals for bounded scope expansion
- proof receipts attached to every passport
- passport diffs as the human review surface for campaign drift

### Failure-First Dashboard And Quarantine Ledger

The portfolio dashboard is read-only and ranks campaigns by failure pressure:
missing proof, stale ambiguity, rejected or deferred claims, authority requests,
human checkpoints, doctor integrity problems, and evidence handles that cannot
be resolved. A discovered slice cannot silently become work; it lands in a
quarantine or deferral ledger with its claim, source evidence, why it is out of
current scope, what authority would promote it, and what stop condition it may
trigger. The meta-agent proposes the next workflow only when required proofs and
scope gates are satisfied; otherwise it emits an escalation or narrowed handoff
artifact. Stage boundaries are hard resets that pass accepted claims, unresolved
risks, deferred slices, and exact daemon/artifact handles, not chat history.

Load-bearing risk: the dashboard could become a hidden control plane if agents
treat projected rows or deferral entries as permission to expand scope or start
build work.

First concrete step: write a Level-1 artifact-contract mock using one real
multi-RFC arc: dashboard row taxonomy, deferral-ledger fields, stop-pressure
signals, promotion rules, and stage handoff payload shape.

Child ideas:

- stop-pressure score instead of progress percentage
- claim quarantine ledger for every discovered slice
- stage restart packet with accepted claims, rejected claims, pending proofs,
  authority boundaries, stop conditions, and retrieval handles
- evidence-first dashboard rows where unresolved handles are compliance
  failures
- arc-level escalation protocol for ambiguity above a stop threshold

## Candidate Matrix

| Candidate | Ticketing substrate | UI surface | Authority model | Context handoff | Deferral policy | Proof model | Stop conditions |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Expiring envelopes | Envelope artifact plus later ticket/passport mapping | Envelope state and admissibility pressure | Stage-scoped, expiring, evidence-renewed | Envelope plus admissibility packet | Out-of-envelope work cannot continue | Evidence ledger satisfies exit criteria | Expiry, missing evidence, authority escalation, red doctor, stale arc |
| Stage passports | Local passport/provenance envelope | Passport diffs and stamp status | Passport grants no live authority; daemon still launches workflows | Next-stage passport seed | Child passport proposals before scope growth | Stamp receipts and artifact hashes | Stop clauses and unstamped transitions |
| Failure dashboard | Quarantine/deferral ledger plus evidence rows | Read-only failure/compliance console | Read surface only; no control-plane writes | Stage restart packet | Claims quarantined until promoted by accepted authority | Resolvable daemon/artifact/ticket handles | Stop-pressure threshold, unresolved handle, human checkpoint |

## Non-Decision

This synthesis does not accept an architecture. It nominates candidate families
for the next Striatum-native divergent ideation and falsification pass.

