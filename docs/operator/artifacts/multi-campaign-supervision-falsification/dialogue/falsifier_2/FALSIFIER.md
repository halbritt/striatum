# FALSIFIER - replay completeness and deferral proof gap

author: falsifier-reviewer-002

## Claim Challenged

The holder's Claim 2, Claim 3, Claim 4, and combined gate claim say the Level-1 shortlist is ready for a product-decision/RFC drafting gate because fresh-context replay, deferral quarantine, and cross-surface contradiction proof can prevent inherited chat, silent deferrals, and false done claims. That only holds if the replay packet and done proof can prove they are complete over the relevant evidence universe, not merely consistent over the handles selected for the packet.

## Concrete Evidence

The motivating scope is broader than a single artifact bundle. The Level-0 PRD says inputs include RFC proposal docs, roadmap/TODO state, GitHub or local tickets, live Striatum run state, timelines, and accepted arc constraints, while outputs include tickets/work packets, handoff payloads, status dashboard, issue/ticket updates, operator reports, and evidence links (`MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md:40-45`). The problem brief makes the same proof obligation explicit: done claims must reconcile daemon state, workflow artifacts, Git, docs, tickets or issues, and verifier receipts (`PROBLEM_BRIEF.md:46-47`, `PROBLEM_BRIEF.md:104-105`).

The selected fresh-context mechanism gives the fresh lane only a bounded packet. The deepening artifact says the daemon assembles that packet from durable sources and the fresh session receives only that packet, then the campaign may advance when the replay result agrees with daemon state and surfaces no unresolved stop pressure (`deepened/deepen_2/DEEPENED.md:14-24`). It correctly names the load-bearing risk: the packet can be too curated and hide contradictions, so the design must define a minimal inspectable contract and require source-handle checks (`deepened/deepen_2/DEEPENED.md:31-36`). The synthesis repeats the same shape: a bounded replay packet from durable handles, pass if the lane can restate next state and identify missing or contradictory evidence, and failure becomes repair (`IDEATION_SYNTHESIS.md:48-52`).

The contradiction gate is supposed to close the gap by reconciling daemon run/job/verdict state, artifact publication, Git/docs state, ticket or issue state, verifier receipts, and known deferrals, with missing or conflicting handles treated as stop pressure (`IDEATION_SYNTHESIS.md:73-76`; `HOLDER.md:112-135`). But neither the holder nor the deepened replay artifact requires a completeness witness: a manifest of which daemon queries, artifact lists, Git/docs refs, ticket/issue mirrors, verifier receipt stores, operator reports, and deferral custody entries were searched, when they were searched, which expected surfaces were unreachable, and why omitted handles are out of scope.

## Counterexample

A campaign reaches a build-to-verify boundary. The accepted arc has one green verifier receipt in the current ticket body and a matching artifact path, but an earlier verifier receipt on the same slice failed against a newer Git doc state, and a discovered-slice deferral is recorded in an issue mirror rather than the local ticket section. The coordinating agent hands the daemon the current accepted arc, authority receipt, green verifier handle, current artifact path, and ticket status. The daemon assembles a bounded replay packet from those durable handles. A fresh lane receives only that packet, checks every handle inside it, restates the next admissible state, and finds no contradiction. The done gate then reconciles daemon/artifact/Git/ticket/verifier/deferral surfaces as represented inside the packet and marks the stage green.

That satisfies the holder's local mechanics but still hides the contradiction. The fresh lane was forbidden from inheriting chat history, but it also had no independent proof that the packet enumerated all relevant verifier receipts, issue/ticket mirrors, deferral rows, operator reports, and Git/docs revisions. The omitted failing verifier receipt and issue-mirror deferral were not contradicted; they were never made visible. This is not a UI or implementation detail. It breaks the proof claim at the product-decision boundary because bounded replay can become selected-evidence replay.

## Strongest Rebuttal

The holder has a real defense. It explicitly says Claim 2 fails if the replay packet hides contradictory handles, Claim 3 fails if quarantined work is invisible to a fresh lane, and Claim 4 fails if known deferrals are omitted or missing handles are downgraded to warnings (`HOLDER.md:78-83`, `HOLDER.md:104-110`, `HOLDER.md:131-135`). The deepening artifact also calls over-curation the load-bearing risk and says the first concrete step is to specify exact daemon handles, artifact paths, deferral entries, stop conditions, and human decisions for one boundary (`deepened/deepen_2/DEEPENED.md:31-43`). The current gate is also not build readiness; it only permits a product decision or narrower RFC draft (`SEED.md:36-41`; `HOLDER.md:150-164`).

That rebuttal shows the authors see the hazard, but it does not make the shortlist ready. The gap is not that the future design lacks a schema or route map. The gap is that the clearing claim still permits the next RFC to define replay packet fields over selected durable handles, while treating omission detection as ordinary contradiction checking. A fresh-context lane cannot detect omitted evidence if the packet is both its evidence source and its search boundary.

## Unanswered Gap

The synthesis should not clear on the fresh-context/proof/deferral lens unless the next gate is required to include a completeness floor:

- every replay packet and done proof must carry an evidence-inventory manifest that lists the required surfaces searched, their query/source handles, freshness timestamps or refs, and unreachable/unknown surfaces;
- omission of an in-scope daemon row, artifact, Git/docs revision, ticket/issue mirror, verifier receipt, operator report, or deferral custody entry is itself stop pressure;
- the fresh lane must either verify that manifest against source handles or receive a daemon-scoped proof that the manifest is complete for the boundary;
- quarantined and discovered slices need negative proof too: not just "listed if known", but proof that no in-scope deferral surface was skipped before any done or advancement claim turns green.

Without that floor, the proposal can honestly avoid inherited chat history while still letting curated replay packets and selected contradiction checks hide the evidence that would have stopped the campaign.