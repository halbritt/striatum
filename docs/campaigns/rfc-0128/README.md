# RFC 0128 P0 Cross-repo Lint

Workflow fixture for RFC 0128 P0, covering fail-fast cross-repository linting.
It defines the draft, review, and apply jobs for this implementation slice.

- `workflow.json` defines the draft, review, and apply jobs.
- `prompts/` contains the job prompts.
- `roles/` contains the author and reviewer role definitions.

Runtime `artifacts/` outputs are declared by the workflow but are not present in
this fixture snapshot.
