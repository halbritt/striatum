# FALSIFIER - multi-campaign supervision authority boundary

author: falsifier-reviewer-003

## Claim Challenged

The holder's Claim 1, Claim 3, and combined gate claim say the Level-1 shortlist can advance to a product-decision or RFC-drafting gate because authority receipts, quarantine, fresh replay, and contradiction checks will keep a coordinating agent from inheriting ambient permission. That claim is only sound if the next gate cannot treat an `AuthorityReceiptV1` artifact, replay pass, quarantine row, ticket field, or dashboard status as current authority to start, sequence, scaffold, promote, or mark Striatum work done.

The material risk is narrower than immediate build creep: the proposal can stay inside the holder's non-claims and still let provenance rows become the practical permission source while the daemon only sees an already-authorized client call.

## Concrete Evidence

Striatum's current boundary is daemon-first. `docs/how-to/how-to-agent.md` says authoritative live workflow state lives in daemon-owned PostgreSQL and that the runner, not prose, terminal output, marker files, or direct database access, advances state (`docs/how-to/how-to-agent.md:50-60`). It also says the daemon is the single writer and `work.complete` / review submission are the state transitions (`docs/how-to/how-to-agent.md:64-73`, `docs/how-to/how-to-agent.md:78-101`). Artifacts and bylines are required deliverables (`docs/how-to/how-to-agent.md:213-235`), not live permission checks.

The motivating product scope asks for more than advisory reporting. The live-human PRD says that after the human accepts an arc, the meta-agent should run design, build, verifier, and discovered-slice work until a stop condition, and outputs may include tickets/work packets, status dashboards, issue or ticket updates, daemon run links, and evidence links (`MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md:36-46`). The problem brief asks Level 1 to define what the coordinating agent may start, sequence, scaffold, pause, quarantine, update, or escalate (`PROBLEM_BRIEF.md:42-47`). The ideation brief asks which actions remain human-confirmed, especially irreversible or authority-expanding actions (`MULTI_CAMPAIGN_SUPERVISION_IDEATION_BRIEF.md:104-115`).

The shortlist and deepening artifacts leave the enforcement boundary split. `IDEATION_SYNTHESIS.md` says the human-accepted arc emits a stage-scoped authority receipt and continuation renews from durable handles against daemon state, artifacts, Git/docs, ticket or issue mirrors, and verifier receipts (`IDEATION_SYNTHESIS.md:34-39`). But the first concrete step in the authority deepening is to draft an `AuthorityReceiptV1` artifact contract and worked example; daemon-enforced receipts are explicitly a later product build outside Level-1 scope (`deepened/deepen_1/DEEPENED.md:24-26`). The deferral deepening similarly proposes quarantine promotion receipts and dashboard pressure rows while naming the risk that the quarantine ledger becomes an unofficial state machine if it is not tied to daemon evidence and human-accepted authority (`deepened/deepen_3/DEEPENED.md:107-121`).

The holder recognizes the risks but does not make the proof-only floor mandatory. It says this gate is not architecture, build readiness, schema, route, UI, ticket backend, implementation ticket, or approval to run outside ordinary daemon authority (`HOLDER.md:5-15`, `HOLDER.md:150-164`). It also says visibility is not acceptance and promotion requires bounded authority, fresh-context payload, and contradiction-proof evidence (`HOLDER.md:85-110`). What it does not require is that the next RFC/product decision either remain proof-only with no workflow-launch authority, or define a current daemon-state authority check before any stage action is executed. The synthesis's own wildcard proposes exactly that proof-only v1 as the safer first floor, but frames it as a provocation rather than a clearing condition (`IDEATION_SYNTHESIS.md:95-102`).

## Counterexample

A next RFC can honestly obey the holder's literal boundary and still cross the daemon-authority line:

1. It defines `AuthorityReceiptV1` as a repo artifact or ticket section with scope, expiry, allowed actions, forbidden actions, evidence handles, deferrals, discovered slices, renewal criteria, and stop triggers. It does not add daemon schema, route maps, UI, implementation tickets, or build readiness.
2. It defines a fresh replay lane that validates that artifact against selected daemon, artifact, Git/docs, ticket, deferral, and verifier handles, then writes a replay pass result.
3. It lets a coordinating agent with ordinary Striatum daemon credentials use that replay pass plus the receipt to start the next workflow, scaffold a discovered slice, promote a quarantine row, update ticket status, or mark the campaign stage done.
4. The daemon accepts the client call because the session or operator has ordinary daemon authority. At the moment of action, the daemon has no stage-scoped receipt object to reject an expired receipt, a quarantine row that was never promoted, a dashboard row that is only advisory, or a missing human confirmation. The product-stage permission was decided in artifact/ticket prose before the daemon call.

That is not direct Postgres access, terminal scraping, or an implementation ticket. It is a daemon-respecting design on paper. But it still makes the artifact/ticket/dashboard layer the practical workflow state machine: the daemon records the launch or completion after the coordinating agent has already interpreted provenance as permission.

## Strongest Rebuttal

The holder has a real defense. It narrowly limits this falsification result to a product-decision or RFC-drafting gate, not source changes or design-to-build readiness (`HOLDER.md:5-15`). It says the authority claim fails if receipts renew from stale or missing handles, if a stage advances after expiry, or if a ticket/dashboard/prose claim becomes inherited authority (`HOLDER.md:53-59`). It says quarantine visibility is not acceptance and promotion requires bounded authority, fresh-context payload, and contradiction proof (`HOLDER.md:85-110`). The seed also says this run does not authorize implementation and must stop before design-to-build readiness (`SEED.md:36-41`), and workflow selection requires a product decision plus design-to-build readiness checklist before implementation tickets (`MULTI_CAMPAIGN_SUPERVISION_WORKFLOW_SELECTION.md:94-115`).

That rebuttal prevents immediate build authorization. It does not close the permission-source gap, because the next gate is exactly where the permission model will be chosen. "Ordinary Striatum daemon authority" authenticates the client and protects the runner boundary; it does not by itself prove that the multi-campaign stage action has a current human/product-approved receipt, unexpired scope, reconciled daemon state, and explicit stop-condition handling at call time.

## Unanswered Gap

The holder should not clear on the authority / daemon-boundary lens unless the next gate carries this stop condition:

A receipt, replay pass, quarantine row, ticket field, dashboard row, or contradiction report is provenance only. It may request renewal, repair, quarantine, or stop. It may not authorize a coordinating agent to start, sequence, scaffold, promote, update acceptance state, or mark work done unless, at the moment of action, either:

- a current daemon-state authority check matches the exact stage, action, scope, expiry, required evidence, deferral state, and stop conditions; or
- an explicit human/product decision authorizes that exact transition while preserving daemon-state reconciliation.

Until that floor exists, the first acceptable product/RFC direction is proof-only or recommendation-only. Without it, the proposal can advance while still allowing stage receipts and quarantine rows to become ambient permission under a daemon-shaped wrapper.
