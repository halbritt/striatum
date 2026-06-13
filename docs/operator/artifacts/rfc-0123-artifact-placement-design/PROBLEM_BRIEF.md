# RFC 0123 Artifact Placement Problem Brief
author: problem-framer-local-fixture-001

## Scope

Implement RFC 0123 as an additive placement model over the existing RFC 0072
blob-storage substrate. The implementation must preserve legacy workflows that
omit placement and must not disturb RFC 0117 worktree ref-safety or RFC 0118
completion provenance gates.

## Decisions To Settle

- Persist placement on artifact rows without breaking the live
  `append_artifact_row` security-definer write path.
- Validate `expected_artifacts[].placement` as a closed value while defaulting
  absent placement through the current kind-based routing rule.
- Route `artifact.publish` from declared placement, not just artifact kind.
- Expose placement on list/detail/export/read surfaces without exposing bodies,
  transcripts, or blob credentials.
- Narrow the #217 git-anchor doctor check to git-retained placements and add
  blob-exhaust metadata checks.

## Constraints

- No hosted services, telemetry, transcript capture, or external persistence.
- Keep legacy repo-path reads working.
- Keep the migration additive and idempotent.
- Keep current artifact kind/front-matter contracts intact.
- Keep generated workflows explicit about placement so future operators can
  inspect the body store intent directly in workflow JSON.
