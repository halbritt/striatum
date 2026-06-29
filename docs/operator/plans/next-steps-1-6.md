---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_next-steps-1-6"
scope_kind: "initiative"
scope_ref: "docs/operator/BRIEF.md"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Workflow run_f7659a42616591da5be84a822f8cf36e completed all six tracks and published final consolidation."
supersedes: "plan_remaining-runway-1-8"
retrieval_priority: "high"
---

# Next Steps 1-6 Plan
author: coordinator-codex-gpt-5-001

## Outcome

Run the next six operator tracks with maximum safe parallelism. This plan is
closed by `run_f7659a42616591da5be84a822f8cf36e` and the final summary at
`docs/operator/artifacts/next-steps-1-6/final/SUMMARY.md`.

1. TODO 55 accepted-risk UI client polish.
2. TODO 56 D125 live auto-finalize dogfood evidence gate.
3. RFC 0050 / RFC 0075 / TODO 67 CLI retirement and MCP/UI parity.
4. TODO 60 local commit confirmation.
5. TODO 59 optional Corpus Contract V2 augmentation-reference surface.
6. TODO 61 / 49 / 62 / 63 legacy and direct-state cleanup.

The workflow at
`docs/operator/workflows/next-steps-1-6.json` runs the six track
jobs in one parallel group and then publishes a final consolidation artifact.
Each track writes only to its own artifact directory so the workflow can run
with `max_active_jobs: 6` without treating repository artifacts as live state.

## Guardrails

- Keep daemon-owned PostgreSQL and daemon MCP as the live workflow authority.
- Do not hide or retire CLI workflow-control verbs before MCP/UI parity is
  tested.
- Preserve the global auto-finalize dry-run default until the D125 evidence
  gate is met.
- Keep Corpus V2 augmentation optional and local; no Engram import, hosted
  service, telemetry, or memory capability belongs in Striatum core.
- Keep Git/PR integration local-first: no autonomous commit, no push, no
  hosted provider action, and no provider SDK in core.
- Treat tmux panes, terminal output, and transcripts as non-authoritative
  inspection metadata.

## Validate And Run

```bash
PYTHONPATH=src python3 -m striatum.cli workflow validate --allow-same-model-pairing docs/operator/workflows/next-steps-1-6.json
PYTHONPATH=src python3 -m striatum.cli run prepare --workflow docs/operator/workflows/next-steps-1-6.json --json
PYTHONPATH=src python3 -m striatum.cli branch confirm --run-id <run_id> --json
PYTHONPATH=src python3 -m striatum.cli run start --run-id <run_id> --json
```
