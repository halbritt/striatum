# Proposal B: JSON-Only Placement
author: proposer-b-local-fixture-001

## Summary

Store placement only in workflow snapshots and expected artifact JSON, and
derive placement dynamically for every artifact row.

## Shape

- Validate `expected_artifacts[].placement`.
- Do not add an artifact table column.
- Join artifact rows back to their producing job and workflow snapshot whenever
  placement is needed.
- Treat legacy/default placement as a read-time projection.

## Benefits

- No database migration.
- No owner bundle/function signature change.

## Risks

- Artifact rows are less self-describing.
- Reads, exports, and doctor become dependent on joining mutable-looking JSON
  contracts for a value that should be part of artifact provenance.
- Future migration or corpus export would have to re-derive historical intent.
