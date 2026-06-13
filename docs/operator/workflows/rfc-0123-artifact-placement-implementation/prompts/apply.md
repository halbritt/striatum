Apply the accepted review by implementing RFC 0123's first compatible slice.

Expected work:

- Persist and project artifact placement with legacy defaulting.
- Validate and generate `expected_artifacts[].placement`.
- Route `artifact.publish` by explicit placement.
- Expose placement in artifact read/list/detail/export surfaces.
- Make artifact-anchor doctor checks placement-aware.
- Update docs that describe the artifact storage model.
- Publish the final synthesis artifact summarizing the implementation and
  verification.
