---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_deferred-20-todo59-external-fetch-closure"
scope_kind: "phase"
scope_ref: "docs/TODO.md#59-phase-11-replay-archive-and-corpus-v2-foundations"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "TODO 59 optional external-consumer fetch and UI UX classified as no-action for Striatum core; current reference-only packet metadata is sufficient until a later optional-augmentation decision accepts a richer fetch surface."
supersedes: "docs/operator/plans/todo-59-corpus-v2-archive.md"
retrieval_priority: "normal"
---

# Deferred 20 TODO 59 External Fetch Closure
author: deferred20-todo59-codex-gpt-5-001

## Objective

Classify the remaining TODO 59 optional external-consumer fetch and UI UX
surface after the Corpus Contract V2 core work. Close the item with evidence,
or make only a narrow local-only source/test change if the current packet
reference surface is unsafe or incomplete.

## Scope

Owned paths for this bounded closure:

- `docs/operator/plans/deferred-20-todo59-external-fetch-closure.md`
- `docs/operator/workflows/deferred-20-todo59-external-fetch-closure/`
- `docs/operator/artifacts/deferred-20-todo59-external-fetch-closure/`

Shared TODO, roadmap, operator brief, RFC, and decision-log files are out of
scope for this worker. Source/test edits are allowed only if they close a
local-only UX gap without adding external consumers, hosted services, retrieval
commands, provider SDKs, telemetry, transcript capture, or external
persistence.

## Outcome

No source or test change is required in this slice. D126 and the current
roadmap classify richer external-consumer fetch UX as optional and out of core.
The implemented Striatum core surface already exposes workflow-authored
`augmentation.mode: "reference_only"` local `corpus_bundle` references on
claimed work packets, including `fetch_mode: "agent_side_local_bundle"`,
manifest summary metadata, and non-blocking missing/unreadable status.

Any richer external-consumer fetch or UI workflow should wait for a later
optional-augmentation decision. Adding it here would reopen the
augmentation-not-dependency boundary without a product decision.

## Validation

```bash
PYTHONPATH=src .venv/bin/python -m striatum.cli workflow validate docs/operator/workflows/deferred-20-todo59-external-fetch-closure.json --json
PYTHONPATH=src .venv/bin/python -m pytest -q tests/test_corpus_verify.py::test_corpus_v2_surface_keeps_augmentation_boundary_local tests/test_workflow_field_errors.py::test_reference_only_augmentation_validates tests/test_workflow_field_errors.py::test_augmentation_source_must_be_local_corpus_bundle
(cd go && go test ./pkg/mutations -run 'TestAugmentation')
PYTHONPATH=src .venv/bin/python - <<'PY'
from pathlib import Path
from striatum.artifact_contracts import validate_artifact_front_matter

for kind, raw_path in [
    ("work_plan", "docs/operator/plans/deferred-20-todo59-external-fetch-closure.md"),
    ("synthesis", "docs/operator/artifacts/deferred-20-todo59-external-fetch-closure/closure/NO_ACTION.md"),
]:
    path = Path(raw_path)
    validate_artifact_front_matter(kind=kind, path=path, payload=path.read_bytes())
print("front matter valid")
PY
git diff --check -- docs/operator/plans/deferred-20-todo59-external-fetch-closure.md docs/operator/workflows/deferred-20-todo59-external-fetch-closure docs/operator/artifacts/deferred-20-todo59-external-fetch-closure
```
