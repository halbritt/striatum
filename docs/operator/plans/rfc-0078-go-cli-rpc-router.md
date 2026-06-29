# RFC 0078 Go CLI RPC Router Plan

Status: active
Date: 2026-05-25
author: operator-codex-gpt-5.5-001

## Scope

Execute the next RFC 0078 gate: replace the hand-built first Go CLI slice with
a generated Go CLI RPC router sourced from `contracts/daemon_methods.json`.
This is not the full Python deletion gate. It is the bounded executable slice
that makes the Go `striatum` binary capable of routing daemon-backed CLI verbs
through the daemon RPC envelope while preserving local workflow-authoring
commands as explicit local exceptions.

## Inputs

- `contracts/daemon_methods.json`
- `docs/operator/artifacts/rfc-0078-go-only-runtime-and-python-removal/cli/HANDOFF.md`
- `docs/operator/artifacts/rfc-0078-go-only-runtime-and-python-removal/final/SUMMARY.md`
- `docs/architecture/COMMAND_AUTHORITY_MATRIX.md`
- `docs/architecture/CLI_RETIREMENT_PARITY.md`
- `go/cmd/striatum/`
- `go/pkg/rpc/`
- `go/pkg/reads/`
- `go/pkg/mutations/`
- `go/pkg/workflowauthoring/`
- `go/pkg/workflowgenerate/`
- `go/pkg/workflowtemplates/`

## Execution Shape

The workflow lives at
`docs/operator/workflows/rfc-0078-go-cli-rpc-router.json`.
It declares `parallelism.max_active_jobs` as `20` to match the high-parallel
RFC 0078 operator pattern. The actual first wave is split into disjoint
implementation lanes after a contract inventory:

- generated route metadata and drift checks;
- daemon RPC transport/client plumbing for the CLI;
- read/list/status-style parameter builders;
- mutation/recovery/supervision parameter builders;
- explicit local-command boundary for workflow-authoring verbs.

Those streams converge into a CLI dispatch integration job, then independent
authority and coverage reviews plus aggregate validation feed a final summary.

## Acceptance Gate

The slice is accepted when the final summary shows all of the following:

- route metadata is generated from `contracts/daemon_methods.json`;
- a committed freshness check fails on route drift;
- daemon-backed commands route through the Go daemon RPC envelope instead of
  duplicating daemon reads or mutations;
- local workflow-authoring commands remain explicit and documented as local
  exceptions;
- the aggregate validation command includes the generated-router tests and the
  existing Go test suite subset needed for RPC, MCP, workflow authoring, and
  daemon method registry parity;
- any unported CLI commands are listed with a replacement, retirement decision,
  or next gate.

## Out Of Scope

- deleting active Python source or tests;
- replacing the local web service;
- packaging/release cutover;
- hosted provider integrations;
- changing daemon method semantics outside the router contract.
