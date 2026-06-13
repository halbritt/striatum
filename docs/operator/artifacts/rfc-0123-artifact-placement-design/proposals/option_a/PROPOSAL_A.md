# Proposal A: Additive Placement Column
author: proposer-a-local-fixture-001

## Summary

Add `artifacts.placement` as a nullable text column with a normal migration and
teach artifact writes to store the resolved placement. Treat null placement as
legacy compatibility in reads and doctor logic.

## Shape

- Add placement constants in Go near artifact publication.
- Resolve placement from the job's current `expected_artifacts_json` row by
  `logical_name`, `kind`, `path`, and attempt-aware cycle substitution.
- If a workflow omits placement, resolve through the existing RFC 0072
  kind-based routing rule.
- Route blob upload when resolved placement is `blob_exhaust`.
- Route git anchor checks only for `git_publication` and
  `git_pointer_manifest`.

## Benefits

- Smallest schema change.
- Best compatibility with existing rows.
- Keeps old workflows readable and publishable.
- Avoids a large data migration before the behavior is useful.

## Risks

- Null rows require every read path to apply the same defaulting rule.
- The security-definer append function signature must change with the Go
  append wrapper.
