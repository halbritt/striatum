# Audits

This directory holds audit, review, reconciliation, and hygiene reports. The
files are evidence records: read them to understand findings and prior checks,
but do not treat them as the live operator state.

Current operator state belongs in [`../operator/`](../operator/); accepted
product and architecture decisions belong in [`../decisions/`](../decisions/).

## Deep Architecture Review Follow-Through

Deep architecture reviews are durable inputs to operator planning, not a second
unmanaged backlog. A new durable deep architecture review must point to a
follow-through artifact before it is treated as actionable. Use a campaign plan
under [`../operator/plans/`](../operator/plans/) or an update to an existing
campaign plan, and include the active tracker set for any P0/P1 findings.

For each P0 or P1 finding, the review or its follow-through artifact must record
one of these dispositions:

- **Owned:** assigned to a campaign workstream with tracker handles and an owner
  or owning role.
- **Refused:** explicitly rejected with the reason the finding will not be
  implemented.
- **Deferred:** postponed with the trigger, owner or owning role, and place it
  will be reconsidered.

Do not leave a durable deep review that adds P0/P1 work without one of those
dispositions. P2 and lower findings may stay as advisory notes, but they should
be promoted into the same follow-through path before they become operator work.

Default cadence is one deep architecture review per shipped roadmap wave in
[`../operator/rfc-roadmap.md`](../operator/rfc-roadmap.md). Run additional deep
reviews only when the operator asks for an incident-specific audit; those audits
still need the same follow-through artifact and P0/P1 disposition record.
