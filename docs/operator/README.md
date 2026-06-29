# Operator Docs
author: operator-codex-gpt-5-001

This subtree is the operator-facing shelf for Striatum's current brief,
RFC execution roadmap, workflow fixtures, plans, progress notes, recovery
decisions, retrospectives, and generated provenance.

Runtime contract: these files are repository provenance and operator guidance,
not authoritative live workflow state. Live state belongs to daemon-owned
PostgreSQL and must be changed through daemon MCP/RPC, the local web UI, or
daemon-backed CLI commands. Editing files here does not claim work, resolve a
blocker, publish an artifact, complete a job, or make a run healthy.

Start with `striatum operator bootstrap --markdown` when the daemon is
available. It points to the bounded reading plan for the current situation.
For direct reading, use:

- [BRIEF.md](BRIEF.md) for current operator state and next actions.
- [rfc-roadmap.md](rfc-roadmap.md) for RFC sequencing.
- [workflows/README.md](workflows/README.md) for workflow fixtures and generated scaffolds.
- [artifacts/README.md](artifacts/README.md) for generated run evidence and provenance.
- [plans/README.md](plans/README.md) for older and current operator plans.
- [progress/README.md](progress/README.md) for dated progress records.
- [briefs/README.md](briefs/README.md) for superseded operator briefs.
- [recovery-decisions/README.md](recovery-decisions/README.md) for operator recovery decisions.
- [retrospectives/README.md](retrospectives/README.md) for post-run analysis.
- [daemon-perf-analysis/README.md](daemon-perf-analysis/README.md) for the 2026-06-17 daemon performance analysis bundle.

`INDEX.md` remains as a compatibility pointer for older links.
