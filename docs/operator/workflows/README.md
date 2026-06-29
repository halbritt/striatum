# Operator Workflows
author: operator-codex-gpt-5-001

This directory holds workflow fixtures and generated workflow scaffolds used by
operator runs. Top-level JSON files are prepared workflow definitions; nested
directories preserve larger fixtures with prompts, roles, artifacts, receipts,
or smoke-test variants.

Runtime contract: editing a workflow file does not alter a prepared or running
run. `run prepare` snapshots workflow JSON into daemon state; use daemon-backed
workflow commands to create or continue live runs.
