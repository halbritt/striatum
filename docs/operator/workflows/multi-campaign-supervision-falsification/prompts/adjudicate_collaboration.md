# Task

Read only the curated dialogue trajectory for `MULTI_CAMPAIGN_SUPERVISION`
Level-1 synthesis falsification and publish the collaboration ledger verdict:
`accept`, `accept_with_findings`, `needs_revision`, or `reject`. A clearing
verdict is `accept` or `accept_with_findings`, never `clear`.

## Deliverable

Publish the `collaboration_ledger` from curated dialogue/artifact evidence only.

## Verdict Basis

Record whether each falsifier challenge landed, whether the holder answered it
directly, and which constraints carry forward.

A clearing verdict means only that the shortlist is ready for
product-decision / RFC drafting. It does not authorize implementation.

Use `needs_revision` if a material challenge lands against stage-scoped
authority, daemon-boundary fidelity, fresh-context replay, deferral
accountability, cross-surface contradiction proof, or the no-build boundary. Use
`reject` only if the shortlist is the wrong direction rather than under-specified.

## Output Contract

Use the declared collaboration_ledger schema and verdict vocabulary. Do not read raw provider logs or private diagnostics as evidence.
