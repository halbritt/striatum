---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_deferred-24-sealed-apply-closure"
scope_kind: "phase"
scope_ref: "docs/TODO.md#61-rfc-0068-go-production-daemon-port"
state: "closed"
opened_at: "2026-05-23"
closed_at: "2026-05-23"
closure_summary: "Sealed apply/signing is classified as fail-closed foundation only: apply receipts and daemon key rotation remain, apply.reviewed_patch stays removed from the production contract, and reintroduction requires a new accepted sealed-apply RFC or product decision."
supersedes: "docs/operator/plans/residual-deferred-closure-2026-05-23.md"
retrieval_priority: "normal"
---

# Deferred 24 Sealed Apply Closure
author: deferred24-sealed-apply-codex-gpt-5-001

## Objective

Close deferred item 24: classify sealed apply/signing and the removed
`apply.reviewed_patch` daemon method. Preserve the current fail-closed removal
unless the evidence shows an unguarded safety regression.

## Scope

Owned paths for this bounded closure:

- `docs/operator/plans/deferred-24-sealed-apply-closure.md`
- `docs/operator/workflows/deferred-24-sealed-apply-closure/`
- `docs/operator/artifacts/deferred-24-sealed-apply-closure/`

Shared TODO, roadmap, operator brief, decision-log, RFC, contract, source,
and test files are out of scope unless the current fail-closed status is not
guarded by executable checks.

## Outcome

No source or shared-doc change is required in this slice. The current product
state is intentionally fail-closed:

- `apply.reviewed_patch` is absent from `contracts/daemon_methods.json`, the
  Python registry, generated Go registry, MCP discovery, and the command
  authority matrix.
- Stale MCP/RPC calls to `apply.reviewed_patch` return and audit as
  `method_unknown`.
- `apply.receipt.show`, `apply.receipt.verify`, and `daemon.key.rotate`
  remain as the accepted foundation surfaces.
- Full reviewed-patch apply remains product-incomplete and must not be
  reintroduced without a new accepted sealed-apply RFC or product decision
  defining the apply gate, authority model, key custody, and operator UX.

Final artifact:
`docs/operator/artifacts/deferred-24-sealed-apply-closure/closure/RESULT.md`.

## Validation

```bash
PYTHONPATH=src .venv/bin/python -m striatum.cli workflow validate docs/operator/workflows/deferred-24-sealed-apply-closure.json --json
PYTHONPATH=src .venv/bin/python -m striatum.cli workflow plan docs/operator/workflows/deferred-24-sealed-apply-closure.json --json
PYTHONPATH=src .venv/bin/python -m striatum.cli workflow lint docs/operator/workflows/deferred-24-sealed-apply-closure.json --json
PYTHONPATH=src .venv/bin/python -m pytest -q tests/test_mcp_mutation_capabilities.py::test_daemon_mcp_tools_list_filters_by_capability tests/test_mcp_mutation_capabilities.py::test_daemon_mcp_retired_apply_reviewed_patch_is_default_denied_and_audited tests/test_mcp_capability_scope_e2e.py::test_retired_apply_reviewed_patch_denied_as_unknown_and_audited tests/architecture/test_authority_guardrails.py::test_authority_matrix_covers_active_registry_methods tests/architecture/test_authority_guardrails.py::test_registry_methods_have_explicit_authority_path
go test ./pkg/rpc -run 'TestRegistryMatchesDaemonMethodsContract|TestMethodsETagMatchesDaemonMethodsContract|TestHelloUsesDynamicSealedApplyStatus'
go test ./pkg/mcp -run 'TestHTTPHandlerToolsCallUnknownDaemonMethodReturnsMCPError|TestHTTPHandlerToolsListUsesBearerTokenAndHidesUnauthorized'
go test ./pkg/apply -run 'Test'
go test ./cmd/striatumd -run 'TestGoDaemonMethodCoverageIsExplicit|TestRegisterHandlersWiresKeyRotateHook'
PYTHONPATH=src .venv/bin/python -c "from pathlib import Path; from striatum.artifact_contracts import validate_artifact_front_matter; [validate_artifact_front_matter(kind=k, path=Path(p), payload=Path(p).read_bytes()) for k,p in [('work_plan','docs/operator/plans/deferred-24-sealed-apply-closure.md'),('synthesis','docs/operator/artifacts/deferred-24-sealed-apply-closure/closure/RESULT.md')]]; print('front matter valid')"
git diff --check -- docs/operator/plans/deferred-24-sealed-apply-closure.md docs/operator/workflows/deferred-24-sealed-apply-closure docs/operator/artifacts/deferred-24-sealed-apply-closure
```
