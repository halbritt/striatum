# FALSIFIER - replay freshness and mutable-surface drift

author: falsifier-reviewer-005

## Claim Challenged

The holder's Claim 2 and Claim 4 say fresh-context replay plus the cross-surface contradiction gate can prove major-stage advancement and done claims without inherited chat history. That claim is only sound if the proof is fresh at the moment of advancement, not merely true for a replay packet or evidence bundle assembled earlier.

## Concrete Evidence

The shortlisted replay shape is stage-boundary proof: before advancement, the daemon assembles a bounded replay packet from durable handles, and the fresh lane passes if it can restate the next state and identify missing or contradictory evidence (`IDEATION_SYNTHESIS.md:48-52`; `deepened/deepen_2/DEEPENED.md:14-24`). The same synthesis says the contradiction gate must reconcile daemon run/job/verdict state, artifact publication, Git/docs state, ticket or issue state, verifier receipts, and known deferrals, with missing or conflicting handles treated as stop pressure (`IDEATION_SYNTHESIS.md:73-76`). The holder repeats that the gate must cover those surfaces and that stale, missing, contradictory, or omitted handles refute the claim (`HOLDER.md:112-135`).

The upstream scope makes those surfaces mutable and broad. The live-human PRD includes roadmap/TODO state, GitHub or local tickets, live Striatum run state, timelines, accepted arc constraints, issue/ticket updates, operator reports, daemon run links, commit/doc evidence, and deferral justifications (`MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md:40-44`). The problem brief says daemon-owned PostgreSQL remains live state, tickets/issues are at most mirrors or references without a later decision, red doctor/provenance conflicts/stale evidence are stop-and-fix, and done claims must reconcile daemon state, artifacts, Git, docs, tickets or issues, and verifier evidence (`PROBLEM_BRIEF.md:53-69`, `PROBLEM_BRIEF.md:104-105`). Striatum's operator rules reinforce the same boundary: the daemon is the single writer for workflow state, while prose, terminal output, marker files, and direct database access do not advance state (`docs/how-to/how-to-agent.md:52-73`).

The gap is that the holder requires reconciliation but does not require a stage-local freshness baseline: no explicit source epoch, Git/doc ref set, ticket/issue mirror snapshot, verifier receipt inventory timestamp, deferral-custody revision, doctor/status timestamp, or rule saying the final advancement/done seal must re-query those surfaces after replay and before success. The deepening artifact correctly warns that replay packets can be over-curated and should require source-handle checks (`deepened/deepen_2/DEEPENED.md:31-43`), but source-handle checks are not the same as proving the handles are still current when the coordinating agent advances the campaign.

## Counterexample

At T1, a fresh replay packet is assembled for build-to-verify advancement. It includes the accepted arc, authority receipt, current artifact path, current Git docs ref, one green verifier receipt, an issue/ticket status, and a deferral ledger entry marked waiting. The fresh lane checks every handle in that packet and passes.

Between T1 and T2, before the coordinating agent seals the advancement or calls the slice done, three ordinary surfaces move: a newer verifier receipt fails against the same slice, docs change on the run branch, and the ticket mirror gets a comment that converts the waiting deferral into a proposed child slice. At T2, the done claim cites the T1 replay pass and the original handles. If the design has no mandatory revalidation epoch or mutation fence, the stage can pass while all local artifacts are internally consistent and while the fresh lane never relied on live chat history. The contradiction was not hidden by chat; it appeared after replay and before done.

That breaks the proof layer without requiring any architecture, UI, route map, or implementation ticket. The issue is a product-gate invariant: a replay pass cannot be treated as reusable proof unless the next design specifies how long it remains valid, which mutable surfaces it freezes, and what must be rechecked immediately before advancement.

## Strongest Rebuttal

The holder has a strong defense: it explicitly declares the current gate is not architecture, design-to-build readiness, source work, daemon schema, route maps, UI, ticket backend, implementation tickets, or approval to run outside ordinary Striatum daemon authority (`HOLDER.md:5-15`, `HOLDER.md:150-164`). It also says Claim 4 fails if required surfaces are missing, stale, or contradictory, and the workflow selection still requires a product decision/RFC and design-to-build readiness before implementation (`HOLDER.md:131-135`; `MULTI_CAMPAIGN_SUPERVISION_WORKFLOW_SELECTION.md:76-105`).

That rebuttal prevents premature build work, but it does not close the falsifier gap. The next product decision or RFC is exactly where the admissibility proof must be made load-bearing. If it can say "fresh replay passed" without a same-boundary freshness contract over mutable surfaces, it can still convert an old proof into a current done claim.

## Unanswered Gap

The synthesis should not clear on the fresh-context/proof/deferral lens unless the next gate requires a replay-freshness and mutation-drift floor:

- every replay pass and done proof must name the source epoch or ref for daemon state, artifacts, Git/docs, ticket or issue mirrors, verifier receipts, operator reports, doctor/status, and deferral custody;
- every advancement or done seal must either revalidate those sources at seal time or prove no in-scope source changed since the replay pass;
- any newer in-scope verifier receipt, Git/doc mutation, ticket/issue mirror mutation, deferral-custody mutation, red doctor, or unreachable required source after replay is stop pressure, not a warning;
- replay pass results expire at a stated boundary and cannot be reused as done proof for later mutable state.

Without that floor, bounded fresh replay can satisfy the no-chat-history requirement while still letting old proof silently age into a false green stage.