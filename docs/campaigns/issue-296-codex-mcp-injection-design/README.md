# Issue 296 Codex MCP Injection Design

Workflow fixture for a parallel design campaign around Codex MCP injection. It
frames the problem, runs multiple divergent branches, converges and scores the
results, deepens the top picks, and produces a final synthesis.

- `workflow.json` defines the framing, divergence, convergence, deepening, and
  final synthesis jobs.
- `prompts/` contains the job prompts.
- `roles/` contains the problem framing, divergence, convergence, deepening, and
  synthesis role definitions.

Runtime `artifacts/` outputs are declared by the workflow but are not present in
this fixture snapshot.
