# RFC 0062: Real Escalation Inbox

## Status
implemented (residual: optional polish) — the load-bearing escalation inbox
shipped (D130): `escalation.list`/`show`/`resolve`, `striatum inbox`, the typed
`striatumd.escalation_inbox` table, the `striatum.escalation.v1` artifact schema,
and artifact linkage. D130 closed the artifact-only-creation question link-only.
The residual is optional schema strictness (tighter blocker-payload shape for
escalation-class blockers). Currency-promoted in D245 (2026-06-20, RSA-007).

## Summary
Escalations now have daemon-backed projection routes and an operator inbox:
`escalation.list`, `escalation.show`, `escalation.resolve`, `striatum inbox`,
and the `striatum.escalation.v1` artifact front-matter schema have landed.
Artifact linkage into the inbox projection is implemented for the shipped
schema. The typed `striatumd.escalation_inbox` table now exists in both the
Python and Go migration sets.

## Motivation
RFC 0053 made the human principal an escalation-only role, but the product
needed a real projection rather than scattered blocker prose and artifact
conventions.

## Proposed Implementation
Completed work covers list/show/resolve daemon methods, the CLI inbox
projection, the typed `striatumd.escalation_inbox` table, escalation artifact
validation, and artifact linkage. D130 closes the artifact-only escalation
creation question as link-only: publishing an escalation artifact may link to
an existing escalation-class blocker, but it does not synthesize blocker rows
or escalation inbox rows.

The only residual is optional polish — narrower schema strictness: tighten
the blocker payload shape for escalation-class blockers, and consider a
dedicated create/update method only if future product work needs direct
escalation creation outside the existing blocker lifecycle. The typed table
itself is no longer missing, and the load-bearing inbox has shipped.
