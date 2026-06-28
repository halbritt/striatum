# MULTI_CAMPAIGN_SUPERVISION Holder Claim: Level-1 Falsification Gate

author: holder-author-001

This is the holder claim for the `MULTI_CAMPAIGN_SUPERVISION` falsification
run. The claim is deliberately narrow: the Level-1 synthesis is ready to be
challenged for a product-decision or RFC-drafting gate. It is not an accepted
architecture, not design-to-build readiness, and not authorization to create
source changes, daemon schema, route maps, UI surfaces, ticket backends, or
implementation tickets.

If this artifact clears falsification, the next admissible action is a human
product decision or an RFC/design record that decides whether these concepts
should become a product design. If any claim below is refuted, the synthesis
needs revision before that gate.

## Boundary Of The Published Claim

The proposal under test is the four-item Level-1 shortlist in
`docs/operator/artifacts/multi-campaign-supervision-level1/IDEATION_SYNTHESIS.md`:

- authority receipt expiration
- fresh-context replay test
- deferral quarantine and scope-drift refusal
- cross-surface contradiction gate

The shared product boundary is taken from the Level-0 PRD, the Level-1 problem
brief, the current operator brief, and the falsification seed: Striatum remains
local-first; daemon-owned PostgreSQL remains authoritative live workflow state;
repo files are durable provenance; tickets, dashboards, docs, and issue mirrors
must not become a second workflow state machine; and live chat history must not
carry the arc.

## Cycle 2 Revision Floor

This holder claim is revised in response to the cycle-2 adjudicator findings:
`AUTHORITY-PROVENANCE-NOT-PERMISSION` and `REPLAY-COMPLETENESS-WITNESS`. The
four-item shortlist is preserved, but the next product-decision/RFC gate must
carry two mandatory floors before it can clear.

**Floor 1: provenance is not permission.** Authority receipts, replay results,
quarantine rows, ticket sections, dashboard rows, and contradiction reports are
provenance and stop-pressure surfaces only. They may request renewal, repair,
quarantine, rejection, or human review. They do not authorize a coordinating
agent to start, sequence, scaffold, promote, update acceptance state, or mark
work done unless the exact stage action is authorized at the moment of action by
one of these current authorities:

- a daemon-scoped authority object or daemon-state check that matches the exact
  stage, action, scope, expiry, required evidence, deferral state, and stop
  conditions; or
- an explicit human/product decision for that exact transition, with daemon-state
  reconciliation preserved.

The first acceptable RFC is therefore proof-only or recommendation-only unless it
specifies daemon-state enforcement, current receipt reconciliation, expiry
refusal, and human-confirmed authority expansion before any coordinator acts.

**Floor 2: replay and done proofs need an evidence inventory.** Each replay
packet, replay pass, advancement proof, and done proof must include a
source-checkable evidence inventory. The inventory must list required surfaces
searched, query/source handles, freshness timestamps or refs, unreachable or
unknown surfaces, and omitted-handle rationale. Required surfaces include daemon
run/job/verdict state, artifact publication state, Git/docs refs, ticket or issue
mirrors, verifier receipts, operator reports, doctor/status posture, and
deferral-custody entries. Omission of an in-scope daemon row, artifact, Git/docs
revision, ticket/issue mirror, verifier receipt, operator report, doctor/status
red flag, or deferral custody entry is stop pressure.

A fresh lane must either verify the inventory against the named source handles or
receive daemon-scoped proof that the inventory is complete for the boundary under
review. Replay results expire at a stated boundary and are not reusable done
proof for later mutable state. Every advancement or done seal must revalidate
in-scope sources at seal time, or prove no in-scope source changed since replay.
Any newer in-scope verifier receipt, Git/docs mutation, ticket/issue mirror
mutation, deferral-custody mutation, red doctor/status signal, or unreachable
required source after replay is stop pressure, not a warning.

These floors answer the falsifiers directly: the shortlist may proceed only as a
bounded product/RFC direction. It still does not authorize implementation,
architecture selection, schema, route maps, UI, ticket backend, build planning,
or source changes.

## Claim 1: Authority Receipt Expiration Is The Right Authority Spine

**Falsifiable claim.** A multi-campaign supervisor should not inherit authority
from an accepted chat, dashboard row, ticket label, or old run summary. The
Level-1 lead is ready for product-decision/RFC drafting only if the next design
can be framed around stage-scoped, expiring authority receipts that name the
current scope, expiry point, allowed actions, forbidden actions, evidence
handles, deferrals, discovered slices, renewal criteria, and stop triggers.
Those receipts are admissible as proof or recommendation surfaces only until the
next RFC either keeps them proof-only or defines daemon-scoped or explicit
human/product-confirmed authority for the exact stage action.

**Evidence handles.** `IDEATION_SYNTHESIS.md` selects authority receipt
expiration as the strongest converged pick and describes renewal from durable
handles rather than chat. `CONVERGENCE.md` shows cluster A appearing across
B1.5, B2.6, B3.2, B4.1, and B5.6, with B4.1 tied for the top score. The
authority deepening artifact,
`deepened/deepen_1/DEEPENED.md`, identifies the load-bearing risk as evidence
laundering and makes concrete receipt fields the first design object. The
Level-0 PRD and `PROBLEM_BRIEF.md` both make bounded human-accepted authority
and explicit stop conditions central goals.

**Refuted if observed.** This claim fails if a product-decision/RFC draft can
only preserve the supervisor's authority by trusting inherited live
conversation, dashboard status, ticket labels, or prose-only claims; if a
receipt can renew from stale or missing daemon/artifact/Git/doc/ticket/verifier
handles; if a stage can advance after receipt expiry without renewal or stop
pressure; or if defining the receipt forces an architecture, schema, route, UI,
or build plan before the product decision. It also fails if a receipt, replay
pass, quarantine row, ticket field, dashboard row, or contradiction report can be
treated as permission without a current daemon-scoped authority object or
explicit human/product decision for the exact transition.

## Claim 2: Fresh-Context Replay Is The Advancement Proof

**Falsifiable claim.** Major stage advancement must be testable by a fresh lane
that receives bounded durable context only. The Level-1 synthesis is ready for
the next gate only if a blank lane can reconstruct the accepted arc, current
authority, stop conditions, deferrals, evidence handles, and next admissible
action without inherited chat history. The replay packet is not enough by
itself: it must carry the evidence inventory and freshness floor above, and
advancement must revalidate mutable sources at seal time or prove they did not
change since replay.

**Evidence handles.** `IDEATION_SYNTHESIS.md` lists fresh-context replay as a
shortlisted gate. `CONVERGENCE.md` shows cluster B recurring across B1.3,
B2.1, B2.3, B3.1, and B4.3, with B4.3 tied for the top score. The fresh-context
deepening artifact, `deepened/deepen_2/DEEPENED.md`, frames replay as a
stage-boundary proof over durable sources and names over-curation/context size
as the load-bearing risk. The Level-0 PRD records fresh context per major stage
as a live-human constraint, and `MULTI_CAMPAIGN_SUPERVISION_WORKFLOW_SELECTION.md`
requires durable handoffs between phases rather than inherited chat memory.

**Refuted if observed.** This claim fails if a fresh lane cannot restate the
admissible next state from durable inputs; if the replay packet has to hide or
summarize away contradictory handles to fit; if it cannot identify missing
evidence, stop pressure, or human-confirmation boundaries; if advancement can
proceed after replay failure; or if the replay mechanism duplicates daemon live
state instead of proving over daemon state and durable provenance. It also fails
if a replay pass can age into done proof after newer in-scope daemon, artifact,
Git/docs, ticket/issue, verifier, operator-report, doctor/status, or deferral
custody evidence appears.

## Claim 3: Deferrals And Discovered Slices Need Quarantine, Not Permission

**Falsifiable claim.** Every out-of-scope discovery in a supervised arc must be
refused or quarantined with custody: code, reason, owner, evidence handle,
wake-up condition, expiry or review pressure, and an explicit re-entry gate.
Visibility is not acceptance. A quarantined item can re-enter the arc only after
bounded authority, fresh-context payload, and contradiction-proof evidence make
that promotion admissible.

**Evidence handles.** `IDEATION_SYNTHESIS.md` combines deferral quarantine with
scope-drift refusal and warns that quarantine rows must not become permission.
`CONVERGENCE.md` shows cluster C recurring across B1.4, B2.2, B2.5, B3.3,
B4.2, and B4.6, with B4.2/B4.6 selected as a top distinct pick. The deferral
deepening artifact, `deepened/deepen_3/DEEPENED.md`, states the core risk: a
quarantine ledger can become an unofficial state machine unless promotion is
tied to daemon evidence and human-accepted authority. The Level-0 PRD and
`PROBLEM_BRIEF.md` both identify silent arbitrary deferral as a motivating
failure mode.

**Refuted if observed.** This claim fails if a deferred or discovered slice can
be treated as accepted work merely because it appears in a ticket, dashboard,
comment, or ledger; if the proposed quarantine fields cannot distinguish
refusal, waiting, blocked, and promotion states; if promotion can occur without
renewed authority and fresh-context proof; if quarantined work is invisible to a
fresh lane; or if the quarantine surface becomes a parallel workflow engine
beside the daemon.

## Claim 4: Cross-Surface Contradiction Gate Is The Proof Layer

**Falsifiable claim.** The first three picks need a contradiction gate before a
stage, slice, RFC arc, or campaign is called done. The gate must reconcile daemon
run/job/verdict state, artifact publication, Git/docs state, ticket or issue
mirror state, verifier receipts, operator reports, and known deferrals. Missing
or conflicting handles are stop pressure, not advisory warnings. The
reconciliation must be backed by the evidence-inventory manifest and same-boundary
freshness check described above, not by a selected packet that defines its own
search boundary.

**Evidence handles.** `IDEATION_SYNTHESIS.md` names the cross-surface
contradiction gate as the proof layer for the other picks. `CONVERGENCE.md`
shows cluster D recurring across B1.6, B3.4, B4.4, and B5.2, and treats
catastrophe exclusions as stop-condition rules attached to the lead design
rather than a standalone product shape. `MULTI_CAMPAIGN_SUPERVISION_IDEATION_BRIEF.md`
weights completion proof and reviewer perspectives around daemon, artifact,
Git, docs, tickets/issues, verifier receipts, and stop conditions. The current
`docs/operator/BRIEF.md` records the active operational boundary: red doctor or
provenance integrity problems are stop-and-fix conditions, not states to route
around.

**Refuted if observed.** This claim fails if a done or advancement claim can pass
while any required surface is missing, stale, or contradictory; if a ticket,
dashboard, or issue mirror overrides daemon state; if verifier receipts are not
part of the proof; if known deferrals can be omitted from the done claim; or if
the contradiction check silently downgrades stop pressure into a warning. It also
fails if the proof omits the inventory of searched surfaces, source handles,
freshness refs, unreachable surfaces, or omitted-handle rationale.

## Combined Gate Claim

These four claims are mutually dependent. Authority receipts without replay can
launder stale context. Replay without quarantine can forget deliberately deferred
work. Quarantine without contradiction proof can become a permission ledger.
Contradiction proof without authority expiry can prove the wrong thing: that
surfaces agree about an action the supervisor was no longer authorized to take.

The Level-1 synthesis is therefore ready for falsification, and possibly for the
next product-decision/RFC-drafting gate, only if falsifiers cannot produce a
concrete observation that breaks one of the four claims above while staying
inside the hard boundary.

After the cycle-2 revision, the combined gate also includes this rule: no
provenance artifact is action authority by itself, and no replay/done claim is
complete unless its evidence inventory and freshness contract are source
checkable.

## Explicit Non-Claims

This holder does not claim:

- any accepted architecture or data model
- any ticketing backend, local board, GitHub issue policy, or UI shape
- any daemon schema, RPC method, CLI route, route map, or authority-matrix change
- any source-file touch list, implementation ticket, build workflow, or test plan
- any design-to-build readiness verdict
- any approval to run multi-campaign supervision outside ordinary Striatum daemon
  authority

The strongest valid clearing result is: the operator may draft a product
decision or RFC that keeps these four concepts load-bearing and then subjects
that narrower design to its own review gate.
