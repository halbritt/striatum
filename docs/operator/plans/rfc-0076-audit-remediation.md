---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_rfc-0076-audit-remediation"
scope_kind: "rfc"
scope_ref: "docs/rfcs/0076-three-lane-code-and-doc-audit-workflow.md"
state: "open"
opened_at: "2026-05-22"
closed_at: null
closure_summary: null
supersedes: "plan_rfc-0076-three-lane-code-doc-audit-workflow"
retrieval_priority: "high"
---

# RFC 0076 Audit Remediation Plan
author: coordinator-codex-gpt-5-001

## Outcome

Verify and close the remediation plan produced by the first accepted RFC 0076
audit run. The prior patch landed the initial source, test, docs, and
ergonomics fixes for REM-001 through REM-010; this plan gives operators a
bounded workflow to prove closure, apply any missed low-risk gap fixes, and
decide whether generator/catalog integration should be scheduled next.

## Inputs

- [`RFC 0076`](../../rfcs/0076-three-lane-code-and-doc-audit-workflow.md)
- [`D128`](../../decisions/decision-log.md)
- Historical generated-record source path: `docs/operator/artifacts/rfc-0076-code-doc-audit/REMEDIATION_PLAN.md`
- Historical generated-record source path: `docs/operator/artifacts/rfc-0076-code-doc-audit/SYNTHESIS.md`
- [`docs/operator/workflows/rfc-0076-audit-remediation.json`](../workflows/rfc-0076-audit-remediation.json)

## Workstreams

| Workstream | State |
|---|---|
| Verify source/test remediations for REM-001, REM-002, and REM-009 | scaffolded |
| Verify docs/status remediations for REM-003 through REM-010 | scaffolded |
| Apply any missed low-risk gap fixes discovered by verification | scaffolded |
| Decide whether generator/catalog work should be immediate or deferred | scaffolded |
| Publish a closure artifact mapping every REM id to closed/deferred/no-action | scaffolded |

## Current Remediation Status

| ID | Current state | Closure evidence to collect |
|---|---|---|
| REM-001 | landed; verify | Prompt paths are resolved relative to workflow source paths in both Go and Python packet builders, with regression tests. |
| REM-002 | landed; verify | Hidden production MCP tools fail closed through `tools/call`, including write-token coverage and command-authority docs. |
| REM-003 | landed; verify | RFC 0050 MCP index status matches current Go daemon, agent-loop, and Python wrapper source reality. |
| REM-004 | landed; verify | RFC 0076 is accepted by D128 and links the first run plus remediation scaffold. |
| REM-005 | landed; verify | Private project memory is defined and separated from repo-shared context. |
| REM-006 | landed; verify | RFC 0077 owns MCP activity timestamp and deadline work under the RFC 0075 umbrella. |
| REM-007 | landed; verify | Operator docs include tmux/PTY watching guidance with no-terminal-authority wording. |
| REM-008 | landed; verify | Operator docs include dashboard/status recovery triage. |
| REM-009 | landed; verify | `striatum adopt --json` points at workflow-shape guidance. |
| REM-010 | landed; verify | PostgreSQL transition docs cover non-Linux and non-`sudo` setup variants. |
| REM-011 | no action | Guardrail confirmations remain evidence only; no new work is required. |

## Workflow Scaffold

Run the scaffolded workflow when the operator wants a recorded remediation
closure pass:

```bash
striatum workflow validate --allow-same-model-pairing docs/operator/workflows/rfc-0076-audit-remediation.json
```

The workflow is intentionally verification-first. Its implementation job is
for small missed gap fixes only; new product behavior should become a separate
RFC, TODO, or operator plan.

## Decisions Made

- RFC 0076 acceptance does not wait for generator/catalog support.
- The first remediation closure pass should verify the landed fixes before
  scheduling new work.
- REM-011 remains no-action evidence. Do not rewrite historical dogfood
  artifacts or old prompts unless a current doc claims their behavior is live.
- Tmux panes and terminal output remain observation-only and are not closure
  evidence for workflow state.

## Open Questions

- Whether `code_doc_audit` should become a generated workflow template in the
  next RFC 0074 catalog slice.
- Whether repeated audit runs justify a dedicated
  `striatum.audit_finding.v1` schema.
- Whether operator UI should project audit findings as an issue-like queue or
  keep them as artifact-backed claims.
