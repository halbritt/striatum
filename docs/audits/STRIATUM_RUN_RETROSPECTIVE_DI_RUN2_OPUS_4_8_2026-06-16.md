---
type: record
status: frozen
owner: OPUS
expires: null
---

# Striatum Run Retrospective — `di-run2` (divergent_ideation)

Auditor: claude-opus-4.8 · Date: 2026-06-16 · Prompt: `~/git/prompts/RUN_RETROSPECTIVE.md`

## 0. Provenance Inspected

- **Target:** run `run_d5c7ab3d3ac7200cbd1dbb384fa1b8c4`, workflow `di-run2`
  (shape `divergent_ideation`, RFC 0087 + RFC 0129), provenance on branch
  `striatum/di-run2` under `striatum/di-run2/`.
- **Repo:** `~/git/striatum`, audited from `main` via
  `git show striatum/di-run2:<path>` (read-only; no checkout switch needed).
- **Authority used:** `committed` + `history` (read-only git) only. **No** live
  daemon, corpus, or blob reads were authorized, so terminal run-state and
  blob-routed artifacts are graded as `unknowable`/`claimed` where they cannot
  be reached from git.
- **Artifact inventory (committed, branch `striatum/di-run2`):**
  `PROBLEM_BRIEF.md`; `branches/branch_2_liability_chain_auditor/IDEAS.md`;
  `branches/branch_4_liquidity_provider/IDEAS.md`; `CONVERGENCE.md`
  (`findings_ledger.v1`); `deepened/deepen_1/DEEPENED.md`,
  `deepened/deepen_2/DEEPENED.md` (`synthesis.v1`); `IDEATION_SYNTHESIS.md`
  (`synthesis.v1`). Porter commits `5dc37430`→`d282bc4d` (history).
- **Expected-but-missing from committed tree:** `branch_1_naive_ten_year_old/IDEAS.md`
  and `branch_3_logistics/IDEAS.md` (2 of 4 diverge branches) — routed to blob
  exhaust per RFC 0123, not git. No committed terminal/completion/decision
  marker.
- **Related run (trend context only, not deep-audited):**
  `striatum/divergent-ideation-inaugural` (`run_e178d358…`) — committed tree holds
  only `PROBLEM_BRIEF.md` + one branch IDEAS; **partial**, wedged on a bare-`codex`
  lane and operator-canceled before convergence. Cited below only for trend.

## 1. Process Verdict

**`PROCESS_FRAGILE` · confidence `medium`.**

| Dimension | Grade |
|---|---|
| review substance | `not_applicable` (shape has no review job/verdict) |
| revision convergence | `not_applicable` (no `needs_revision` cycle) |
| lane independence | `adequate` (strong for the 2 committed branches; 2 unknowable) |
| synthesis fidelity | `strong` |
| decision & escalation hygiene | `weak` |
| provenance completeness | `weak` |
| recommendation quality | `strong` |

The generative chain itself worked well: four diverge branches under distinct
frames fed a convergence critic that scored/clustered/trap-flagged 24 ideas, a
deepen pair developed the top picks, and a final synthesis assembled a shortlist
with an explicit non-obvious pick — and the committed artifacts are mutually
faithful (`synthesis fidelity = strong`). The run is graded `PROCESS_FRAGILE`, not
sound, for two provenance-confidence gaps: (a) the committed tree reads like a
clean completion but the run did **not** reach `completed` — it was operator-canceled
after an `artifact_conflict` on `final_synthesis`, and **nothing in committed
provenance discloses that**; and (b) the two diverge branches that produced the
*winning, deepened, synthesized* ideas (B3.x from `branch_3_logistics`) are absent
from committed provenance, so the load-bearing inputs are unauditable from git.
Confidence is capped at `medium` because live/blob enrichment that would resolve
both gaps was not authorized.

## 2. Reconstructed Run Shape

From `workflow.json` (committed) and the porter commit log (history):

- `frame_problem` (`synthesis`, role `problem_framer`, lane `claude`) →
- diverge fan-out (all `build`, `fresh_session_required: true`):
  `branch_1_naive_ten_year_old` (claude), `branch_2_liability_chain_auditor`
  (gemini), `branch_3_logistics` (gpt), `branch_4_liquidity_provider` (claude) →
- `converge` (`synthesis`, role `convergence_critic`, lane `gpt`) →
- deepen fan-out: `deepen_1` (claude), `deepen_2` (gemini) →
- `final_synthesis` (`synthesis`, role `final_synthesizer`, lane `gpt`).

A double fan-out/join, no cycles, no review/verdict jobs. Three model families
carry frames (claude/gemini/gpt). Porter commits (history) show frame_problem,
branch_2, branch_4, converge, final_synthesis published in order, then a
deepening-artifacts commit (`d282bc4d`). **Terminal state is not represented in
committed git** (no completion record file); out-of-band it is `canceled`
(operator), after an `artifact_conflict` blocker on `final_synthesis` whose body
nonetheless committed at `c5e96cb0`.

## 3. Process Scorecard

- **review substance — `not_applicable`.** `divergent_ideation` declares no
  `review`-type job and no verdict; there is no review gate to grade
  (`committed`: `workflow.json` job list).
- **revision convergence — `not_applicable`.** No cycle / `needs_revision` edge;
  nothing to converge (`committed`: `workflow.json` `cycles: []`).
- **lane independence — `adequate`.** The two committed branches are genuinely
  distinct after subtracting the shared brief skeleton: `branch_2` (gemini,
  *liability_chain_auditor*) frames every idea as accountability/ownership (lease
  attestation, chain-of-custody, liability transfer at the schema boundary);
  `branch_4` (claude, *liquidity_provider*) frames every idea as priced
  volatility/hidden subsidy (escalation as an unpriced option, budget as a
  bid-ask spread, the stub as the liquidity provider). No shared blind spot, no
  paraphrase — the RFC 0129 distortion-axis selection visibly produced
  structurally different output on different models (`committed`:
  `branches/branch_2…/IDEAS.md`, `branches/branch_4…/IDEAS.md`). Capped at
  `adequate` because `branch_1` and `branch_3` IDEAS are **not committed** — half
  the fan-out's independence is `unknowable` from git.
- **synthesis fidelity — `strong`.** `CONVERGENCE.md` names its inputs (the four
  branches), scores 24 ideas on weighted novelty/viability/fit, clusters by
  angle, and flags traps with reasons (B4.2 `yes`, B4.1 `watch`). Spot-check
  (≤3): convergence rows B2.1/B2.4/B2.5/B2.3 map faithfully to `branch_2` ideas
  #1/#4/#5/#3, and B4.1/B4.2/B4.3 map faithfully to `branch_4` ideas #1/#2/#3 —
  no laundering, no invented attribution (`committed`). `IDEATION_SYNTHESIS.md`
  draws its shortlist (Last-mile receipts = B3.6, Exception-lane cross-dock =
  B3.2, warehouse scan = B3.4, ★ adversarial stubs = B4.3) from the convergence
  ranking and names CONVERGENCE + the deepen pair as inputs; `deepen_1` (B3.6)
  and `deepen_2` (B3.2) each cite CONVERGENCE and deepen the declared rank.
- **decision & escalation hygiene — `weak`.** The run hit an `artifact_conflict`
  on `final_synthesis` and was **operator-canceled**, but no committed artifact —
  no decision record, no escalation, no terminal marker — discloses either the
  conflict or the cancel. From committed evidence the disposition is invisible
  (`committed`: absence; the cancel/conflict are `claimed, uncheckable` here).
- **provenance completeness — `weak`.** The synthesis chain is complete and
  classifiable, but (a) no committed terminal-state record and (b) 2 of 4 diverge
  inputs are blob-routed out of git — including the source branch of the
  top-ranked, deepened, ★-picked ideas.
- **recommendation quality — `strong`.** §10 ties each next-run change to an
  observed gap.

## 4. Findings

1. **(High) Committed provenance overstates terminal success.** *Mechanism:*
   `divergent_ideation` publishes no terminal/decision artifact to the run
   branch, and RFC 0125's `run_completion_record` lives in PG, not the committed
   tree. *Evidence:* all seven artifacts including `IDEATION_SYNTHESIS.md` are
   porter-committed (`history`: `5dc37430`…`d282bc4d`), yet the run's actual
   terminal state was `canceled` after an `artifact_conflict` on `final_synthesis`
   (known out-of-band; not in git). *Consequence:* a reader — or this auditor on
   git-only — reasonably reads the tree as a clean completion; the gate that the
   shape's own dogfood is meant to demonstrate (run reaches a legible terminal
   state) is not legible from durable provenance. *Smallest fix:* have the porter
   (or the shape) write a terminal disposition marker to the run branch on every
   terminal transition — completed vs canceled vs conflicted — so committed
   evidence cannot imply success the run did not reach.

2. **(High) The winning ideas' source branch is absent from committed
   provenance.** *Mechanism:* RFC 0123 routes ordinary lane outputs to blob
   exhaust; only synthesis/ledgers stay in git. *Evidence:* `branch_1` and
   `branch_3` `IDEAS.md` are not committed, but `CONVERGENCE.md` scores B1.x/B3.x
   ideas and the eventual shortlist + ★ pick (B3.6, B3.2, B3.4) all originate in
   `branch_3_logistics` (`committed`: convergence table + `IDEATION_SYNTHESIS.md`).
   *Consequence:* the load-bearing inputs to the run's headline output cannot be
   verified from committed evidence — convergence's faithfulness to `branch_3` is
   `unknowable` from git, and half the fan-out's lane independence is unauditable.
   *Smallest fix:* for `divergent_ideation`, treat diverge-branch IDEAS as
   git-retained gated inputs (not throwaway exhaust), **or** require the
   convergence ledger to quote each scored idea's source text verbatim so the
   synthesis is self-auditing without blob/live access.

3. **(Low) Deepen front-matter is inconsistent across lanes.** *Evidence:*
   `deepen_1` (claude) carries `author:` and `inputs:` (CONVERGENCE + BRIEF) in
   front matter; `deepen_2` (gemini) omits `author:` from front matter (body
   only) and lists only CONVERGENCE in `inputs` (`committed`). *Consequence:*
   cosmetic provenance non-uniformity across model lanes; passed validation, but
   weakens machine-classification. *Fix:* tighten the deepen prompt/role to
   require `author` + full `inputs` in front matter.

## 5. Review Substance

`Not applicable.` `divergent_ideation` declares no `review`-type job and no
verdict gate, so the mandatory review table is empty by construction:

| review artifact | verdict | reviewed target | cited specifics | falsifying scenario | severity consistency | grade |
|---|---|---|---|---|---|---|
| — (no review jobs in shape) | — | — | — | — | — | `not_applicable` |

The shape's "critic" is the **convergence** job, audited under synthesis fidelity
(§3), not as a review: it scores and prunes generative ideas; it does not gate a
peer artifact with accept/reject. This is a legitimate shape design, not a miss.

## 6. Revision Convergence

`Not applicable.` No `needs_revision` cycle exists (`committed`: `workflow.json`
`cycles: []`); there is no finding→change→re-review loop to map. No unresolved
loops.

## 7. Lane Independence

Strong where committed. After subtracting the shared brief skeleton, `branch_2`
(gemini) and `branch_4` (claude) share no idea, no framing, and no blind spot:
the gemini branch reasons exclusively about *who is accountable when progress
evaporates*, the claude branch exclusively about *who silently bears unpriced
volatility*. Both are recognizably the frames RFC 0129's selector assigned
(`liability_chain_auditor`, `liquidity_provider`), and both run on different model
families, so the run demonstrates genuine multi-model divergence — the property
the shape exists to provide. Unverifiable for `branch_1`/`branch_3` (blob-only):
convergence shows their ideas existed and differed (B1.x vs B3.x clusters), but
their raw text is not in committed provenance. Synthesis handled no explicit
disagreement (the shape converges by scoring, not debate); the convergence critic
recorded traps rather than resolving contradictions, which is correct for this
shape.

## 8. Synthesis And Decisions

Synthesis is the strongest dimension. `CONVERGENCE.md` (gpt) is a substantive
critic pass — 24 scored rows, explicit weights (novelty 0.35 / viability 0.40 /
fit 0.25), angle clusters, and two trap flags with reasons — and its attributions
to `branch_2`/`branch_4` check out verbatim. `IDEATION_SYNTHESIS.md` (gpt)
preserves the convergence ranking, marks the non-obvious pick (★ adversarial
structural stubs, B4.3), and lists its inputs. The deepen pair faithfully expand
the declared top-2. **Decisions/escalations:** none recorded in committed
provenance — and that is itself the §4.1 finding: the operator cancel + the
`artifact_conflict` (and, out-of-band, a `recovery reseal` + co-driver recovery
misstep) are nowhere in the durable tree. All artifact bylines are consistent
with their declared lanes (`problem-framer-claude-opus-4.8-001`,
`diverger-gemini-001`, `diverger-claude-opus-4.8-002`, `convergence-critic-gpt-5.5-001`,
`deepener-claude-opus-4.8-001`, `deepener-gemini-001`, `final-synthesizer-gpt-5.5-001`)
— `claimed`, corroborated by the workflow's lane assignment, but live attestation
is `unknowable` from git.

## 9. Unknowable From This Provenance

- The run's **terminal state** (canceled vs completed) and the `artifact_conflict`
  / recovery history — not in committed git; would need `live` daemon
  (`run_completion_record`, run-state) or the operator's cancel reason.
- `branch_1`/`branch_3` raw IDEAS and therefore convergence's fidelity to them —
  blob-routed; would need `exported`/`live` blob access.
- Session freshness, lease/heartbeat timing, attestation truth behind the bylines.
- Whether the conflict was a benign duplicate-publish race (the body did commit)
  or a genuine durability loss — `unknowable` from committed evidence.

## 10. Recommendations For The Next Run

1. **Commit a terminal disposition marker** (tied to §4.1): on every terminal
   transition the porter should write a small `RUN_LEDGER`/completion record to
   the run branch naming the terminal state, so committed provenance cannot imply
   a completion the run did not reach. Gate/porter change, not a product fix.
2. **Retain diverge-branch IDEAS in git for `divergent_ideation`, or make the
   convergence ledger quote source idea text** (tied to §4.2): the branches are
   the shape's gated inputs, not disposable exhaust; an auditor must be able to
   confirm convergence represented them faithfully without blob/live access.
3. **Normalize deepen front matter** (tied to §4.3): require `author` + full
   `inputs` in the deepen role/prompt so every lane's deepen artifact is uniformly
   machine-classifiable.
4. **Operator-process note** (trend, with the inaugural run): the bare-`codex`
   wedge in `striatum/divergent-ideation-inaugural` and the `reseal`+co-driver
   recovery misstep in this run were operator/lane-config errors, not shape
   defects — fold the `codex --yolo` requirement and the "one driver; never reseal
   an already-committed body" rule into the operator runbook so the next run does
   not repeat them.
