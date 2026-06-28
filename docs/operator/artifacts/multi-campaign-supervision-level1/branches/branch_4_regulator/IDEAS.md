---
artifact_kind: handoff
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: multi-campaign-supervision-level1
author: diverger-author-001
created_at: 2026-06-28
updated_at: 2026-06-28
---

# Branch 4 Regulator Ideas

Frame: audit what must be provable, traceable, or refusable when one accepted arc can drive many Striatum stages.

## Authority Receipt Expiration

Each campaign transition carries a short authority receipt naming the accepted arc hash, the verbs allowed for the next stage, the evidence required before use, and the stop clauses that revoke it. The receipt expires at every design/build/verify boundary, so a fresh agent must re-prove authority instead of inheriting ambient permission.

## Deferral Quarantine Codes

A deferral is not a note; it is a quarantine code with owner, trigger, blocked proof surface, and release condition. If a later ticket depends on that surface, the campaign must show the quarantine in-band and refuse "done" until a human accepts, narrows, or retires it.

## Fresh-Context Replay Test

Before the meta-agent advances a campaign, it must produce a compact restart packet that a new lane can use to reconstruct the current arc without chat history. If the packet cannot replay the accepted plan, open stops, latest receipts, and evidence handles, the arc is not governable and progression is refused.

## Contradiction-First Status

Campaign status starts from potential contradiction, not optimism. Every green state must name the daemon row, artifact, git ref, ticket, and verifier receipt it reconciled; if any source moves afterward, the status downgrades to "unreconciled" until proof is refreshed.

## Irreversible Action Lockbox

Actions that close issues, integrate branches, widen authority, mark roadmap rows shipped, or delete historical bodies sit behind a lockbox. The meta-agent may prepare the action and cite evidence, but execution requires a matching human-confirmed receipt plus a preflight that no quarantine code is still open.

## Scope-Drift Refusal Ledger

Every discovered slice enters a refusal ledger before it becomes work. The ledger records why the slice is in or out of the accepted arc, which authority receipt can launch it, and which human decision would be needed to widen scope, so useful new work cannot silently smuggle itself into the campaign.