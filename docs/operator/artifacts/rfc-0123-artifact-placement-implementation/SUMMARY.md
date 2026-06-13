---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
inputs:
  - "draft"
  - "review"
author: author-local-fixture-007
---

# RFC 0123 Implementation Summary

Implemented the accepted artifact placement model from D190:

- `expected_artifacts[].placement` is validated as a closed enum and generated
  by workflow authoring/generation surfaces.
- Artifact publication resolves placement from the expected artifact contract,
  persists the resolved value, and routes blob upload by placement instead of
  kind alone.
- Owner schema bundle 7 adds the additive `artifacts.placement` column plus a
  placement-aware `append_artifact_row` overload with a legacy wrapper.
- Read, export, run-summary, corpus, trajectory, and content surfaces project
  placement with legacy defaults for older rows.
- Artifact doctor now checks blob-exhaust artifacts through blob metadata/body
  verification and keeps git-anchor checks for git placements.
- Documentation records D190 and describes the placement contract and blob
  transition verification behavior.

Verification completed:

- `git diff --check`
- `striatum workflow validate docs/operator/workflows/rfc-0123-artifact-placement-design/workflow.json --json`
- `striatum workflow validate docs/operator/workflows/rfc-0123-artifact-placement-implementation/workflow.json --json`
- `go test ./...`
- `make test`
- `make lint`
- `make smoke`
- `make build`

`make smoke` passed; its PostgreSQL integration segment was skipped because the
configured database was not reachable in this environment.
