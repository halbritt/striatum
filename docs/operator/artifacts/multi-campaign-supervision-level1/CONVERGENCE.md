---
schema_version: "striatum.findings_ledger.v1"
artifact_kind: "findings_ledger"
summary_count: 30
---

# MULTI_CAMPAIGN_SUPERVISION Convergence Ledger

author: convergence-critic-reviewer-2-001

## Basis

Read inputs:

- `docs/operator/artifacts/multi-campaign-supervision-level1/branches/branch_1_naive_ten_year_old/IDEAS.md`
- `docs/operator/artifacts/multi-campaign-supervision-level1/branches/branch_2_logistics/IDEAS.md`
- `docs/operator/artifacts/multi-campaign-supervision-level1/branches/branch_3_returning_exile/IDEAS.md`
- `docs/operator/artifacts/multi-campaign-supervision-level1/branches/branch_4_regulator/IDEAS.md`
- `docs/operator/artifacts/multi-campaign-supervision-level1/branches/branch_5_actuary/IDEAS.md`
- Level-0 bootstrap artifacts, `PROBLEM_BRIEF.md`, `DIVERGENCE_LEDGER.md`, `IDEATION_SYNTHESIS.md`, workflow metadata, and lane instructions.

Scores use novelty / viability / fit from 0 to 10. Weighted score is `0.35 * novelty + 0.40 * viability + 0.25 * fit`.

## Cluster Map

| Cluster | Underlying angle | Branch evidence |
| --- | --- | --- |
| A | Expiring authority and admissibility receipts | B1.5, B2.6, B3.2, B4.1, B5.6 |
| B | Fresh-context restart packets, passports, crates, and replay tests | B1.3, B2.1, B2.3, B3.1, B4.3 |
| C | Deferral and discovered-slice quarantine | B1.4, B2.2, B2.5, B3.3, B4.2, B4.6 |
| D | Cross-surface proof and contradiction gates | B1.6, B3.4, B4.4, B5.2 |
| E | Failure-first portfolio status and attention surfaces | B1.2, B2.4, B3.5, B5.5 |
| F | Recovery and tail-risk reserves | B3.6, B5.1, B5.4 |
| G | Irreversible-action lockbox | B4.5 |
| H | Provenance-carrying ticket/passport substrate | B1.1 plus support from A/B/C |

## Scored Ledger

| ID | Idea | Cluster | N | V | F | Weighted | Trap |
| --- | --- | --- | ---: | ---: | ---: | ---: | --- |
| B1.1 | Homework folder with plan/build/check/skipped pockets and proof card | H | 7 | 9 | 9 | 8.30 | no |
| B1.2 | Stoplight shelf where red cards float to the front | E | 7 | 8 | 8 | 7.65 | no |
| B1.3 | Five-note lunchbox for a fresh agent | B | 7 | 9 | 10 | 8.55 | no |
| B1.4 | Named deferral promise token with reason, owner, wake-up, proof | C | 7 | 9 | 10 | 8.55 | no |
| B1.5 | One-door stage hall passes and per-slice passes | A | 9 | 9 | 10 | 9.25 | no |
| B1.6 | Receipt table matching daemon, Git, docs, and ticket state | D | 8 | 8 | 10 | 8.50 | no |
| B2.1 | Cold-chain context crates with freshness labels and quarantine | B | 8 | 9 | 10 | 8.90 | no |
| B2.2 | Returns desk for deferrals with expiration and re-entry dock | C | 8 | 9 | 10 | 8.90 | no |
| B2.3 | Cross-dock handoff bay with standardized manifests | B | 8 | 8 | 9 | 8.25 | no |
| B2.4 | Yard-control map for active arcs | E | 7 | 7 | 8 | 7.25 | control-plane risk |
| B2.5 | Milk-run slice pickup with scheduled stops and batches | C | 7 | 7 | 7 | 7.00 | scheduling-first risk |
| B2.6 | Customs broker for authority crossings | A | 8 | 8 | 9 | 8.25 | no |
| B3.1 | Forgetting as expiry-bound context passport | B | 9 | 9 | 10 | 9.25 | no |
| B3.2 | Authority as a spend-down budget | A | 9 | 8 | 10 | 8.85 | no |
| B3.3 | Deferral as a public mutation | C | 8 | 9 | 10 | 8.90 | no |
| B3.4 | Done as a cross-surface ceremony | D | 8 | 8 | 10 | 8.50 | no |
| B3.5 | Dashboard as an attention ledger | E | 8 | 8 | 9 | 8.25 | no |
| B3.6 | Recovery named in the arc plan | F | 8 | 8 | 9 | 8.25 | no |
| B4.1 | Authority receipt expiration at every stage boundary | A | 9 | 9 | 10 | 9.25 | no |
| B4.2 | Deferral quarantine codes | C | 8 | 9 | 10 | 8.90 | no |
| B4.3 | Fresh-context replay test before advancement | B | 9 | 9 | 10 | 9.25 | no |
| B4.4 | Contradiction-first status | D | 9 | 8 | 9 | 8.60 | no |
| B4.5 | Irreversible action lockbox | G | 8 | 9 | 9 | 8.65 | no |
| B4.6 | Scope-drift refusal ledger for discovered slices | C | 8 | 9 | 10 | 8.90 | no |
| B5.1 | Reserve tickets for expected rework and overflow | F | 7 | 7 | 7 | 7.00 | yes |
| B5.2 | Catastrophe exclusions that force stop instead of pricing | D | 8 | 9 | 10 | 8.90 | no |
| B5.3 | Experience-rated autonomy by RFC family | A | 9 | 5 | 6 | 6.65 | yes |
| B5.4 | Tail-event reinsurance lanes | F | 8 | 6 | 7 | 6.95 | yes |
| B5.5 | Premium shock dashboard for marginal risk | E | 8 | 7 | 8 | 7.60 | quantification risk |
| B5.6 | Deductible stop conditions | A | 7 | 8 | 9 | 7.90 | no |

## Top Picks For Deepening

The raw row scores create several ties inside the same authority/fresh-context family. For downstream deepening, I select the top three non-trap picks after collapsing near-duplicates by cluster. This keeps the next stage from spending all three deepen lanes on one shape with different metaphors.

1. **Authority receipt expiration** (`B4.1`, cluster A, 9.25). This is the strongest lead because it makes meta-agent authority explicit, stage-scoped, evidence-gated, and naturally fresh-context safe. It should absorb supporting variants from B1.5 stage hall passes, B3.2 spend-down authority budgets, B2.6 customs broker gates, and B5.6 deductible stop conditions.
2. **Fresh-context replay test** (`B4.3`, cluster B, 9.25). This is the best acceptance proof for any design: before a campaign advances, a blank lane must reconstruct accepted plan, stops, receipts, deferrals, and evidence handles without chat history. It should absorb B3.1 context passports, B2.1 cold-chain crates, B2.3 cross-dock manifests, and B1.3 five-note lunchboxes.
3. **Deferral quarantine codes / scope-drift refusal ledger** (`B4.2` / `B4.6`, cluster C, 8.90). This is the strongest distinct answer to the human's named failure mode: arbitrary deferrals becoming accepted. It should combine B2.2 return labels, B3.3 public deferral mutations, and B1.4 promise tokens into one visible custody model.

Near miss: **Catastrophe exclusions** (`B5.2`, 8.90) is important, but it fits better as a falsification and stop-condition rule attached to the three picks than as a separate product shape.

## Trap List

- **Generic ticket or yard board as control plane**: attractive because it is familiar, but it can duplicate daemon state and turn projected status into unofficial authority.
- **Scheduling-first slice pickup**: batching discovered slices by appointment risks hiding urgent proof conflicts behind calendar mechanics.
- **Reserve tickets for expected rework**: useful as accounting, but dangerous if it normalizes stale context and verifier churn as pre-paid waste instead of forcing repair.
- **Experience-rated autonomy**: novel, but it creates a hidden policy engine from sparse history and can loosen authority after exactly the wrong kind of lucky success.
- **Tail-event reinsurance lanes**: reserving scarce lanes does not itself detect provenance corruption, hidden blockers, or silent deferrals, and may starve normal verification.
- **Premium shock dashboard as a decision engine**: useful as advisory attention pressure, but false precision can turn risk pricing into an unreviewed concurrency cap.
- **Negative proof checklist alone**: good hygiene, but weak if not backed by actual diff, artifact-scope, daemon-state, and publication checks.

## Cross-Model Signal

No confirmed cross-model-family signal is available from the packet evidence. The workflow metadata shows all active lanes launched through the `codex` command, and the branch artifacts expose role/lane authors rather than provider model families. I therefore do not claim a cross-model confidence boost.

There is strong cross-frame convergence across independent branch frames:

- Expiring authority surfaced in naive, logistics, returning-exile, regulator, and actuary branches.
- Fresh-context payloads surfaced in naive, logistics, returning-exile, and regulator branches.
- Deferral custody surfaced in naive, logistics, returning-exile, and regulator branches.
- Cross-surface proof surfaced in naive, returning-exile, regulator, and actuary branches.
- Failure-first status surfaced in naive, logistics, returning-exile, and actuary branches.

That convergence is useful, but it should be treated as cross-frame agreement, not cross-model-family validation.