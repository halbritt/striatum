# Proposal C: Kind Remapping Only
author: proposer-c-local-fixture-001

## Summary

Extend the existing `blobRoutedKinds` table to cover more lane-exhaust kinds and
leave workflow JSON unchanged.

## Shape

- Add `handoff`, `test_report`, `patch_summary`, `prompt`, and other
  lane-exhaust kinds to the blob-routed set.
- Keep git-retained kinds out of the set.
- Doctor skips blob-routed kinds for git anchors.

## Benefits

- Very small code change.
- No workflow schema migration.

## Risks

- Does not implement RFC 0123's main goal: placement is role-based, not
  kind-based.
- Cannot represent final `synthesis` as git publication while intermediate
  synthesis is blob exhaust.
- Would keep the current ambiguity that caused the #217 transition check to be
  too broad.
