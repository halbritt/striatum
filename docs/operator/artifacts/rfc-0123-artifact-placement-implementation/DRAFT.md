---
schema_version: "striatum.handoff.v1"
artifact_kind: "handoff"
run_id: "rfc-0123-artifact-placement-implementation"
author: author-local-fixture-001
---

# RFC 0123 Implementation Draft

## Scope

Implement the first compatible slice of explicit artifact placement:

1. Add one shared placement resolver with three values:
   `blob_exhaust`, `git_publication`, and `git_pointer_manifest`.
2. Persist resolved placement on artifact rows while preserving legacy
   kind-based defaults for workflows and rows that do not carry placement.
3. Validate `expected_artifacts[].placement` as an optional closed enum.
4. Generate explicit placement in new workflow specs.
5. Route `artifact.publish` from resolved placement, not from kind alone.
6. Project placement through artifact reads, listings, details, and exports.
7. Make artifact-anchor doctor checks apply only to git-retained artifacts, and
   check blob-exhaust rows for blob metadata.
8. Update docs that describe blob transition and artifact durability.

## Compatibility Constraints

- Old workflows remain valid when they omit placement.
- Existing artifact rows with null placement resolve through the same fallback
  used by publish and read paths.
- Direct repository-path artifact reading remains available for historical
  artifacts and git-publication rows.
- Owner/admin schema authority must be preserved for artifacts table DDL and
  the security-definer append function.

## Acceptance Checks

- Focused tests cover placement validation, publish routing, read projection,
  and placement-aware doctor behavior.
- Owner append function tests cover the new placement argument.
- Workflow validation accepts valid placement and rejects unknown values.
- `git diff --check`, focused Go tests, broader Go tests, and workflow
  validation are run before publishing the branch.
