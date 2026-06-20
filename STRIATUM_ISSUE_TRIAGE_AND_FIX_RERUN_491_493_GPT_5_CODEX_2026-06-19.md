# Striatum Issue Triage And Fix Rerun: #491 / #493

author: codex-gpt-5-002
date: 2026-06-19
base: 2ef8c098677fe8145800c7330a9814702bd4b51c
scope: GitHub issues #491 and #493 opened after the first 2026-06-19 issue batch.

## Snapshot

After closing #475, #479, and #480, the live open issue set had 12 issues:
#354, #380, #381, #387, #476, #477, #478, #481, #482, #483, #491, and #493.
The new issues for this rerun were #491 and #493.

## Route Ledger

| Issue | Route | Outcome |
|---|---|---|
| #491 | FIX | Keep RFC 0140 local_work keepalive enabled for Codex lanes even though the PTY-side daemon receiver remains disabled for Codex. |
| #493 | FIX | Doctor dissent-ledger completeness now accepts the verdict_id-linked forward-write row across later live-attempt drift, preserving the old live-attempt fallback for null verdict_id rows. |

## Changes

- `go/pkg/agentloop/loop.go`: decoupled the local-work keepalive gate from `daemonReceiverDisabled`.
- `go/pkg/agentloop/bootstrap.go`: tells lane agents to send `work.heartbeat` with `local_work=true` during long local work.
- `go/pkg/agentloop/loop_test.go` and `go/pkg/agentloop/bootstrap_test.go`: regression coverage for the Codex carve-out and prompt contract.
- `go/pkg/reads/doctor_quorum.go`: changed dissent completeness to prefer `dissent_ledger.verdict_id = verdicts.verdict_id`.
- `go/pkg/reads/doctor_quorum_pg_test.go`: regression coverage for a blocking verdict whose dissent row exists at attempt 1 while the job's live attempt later advances.

## Verification

Focused:

```bash
cd go
go test ./pkg/agentloop -run 'TestBuildBootstrapPromptNamesNativeMCPBoundary|TestLocalWorkKeepalive.*|TestDaemonReceiverDisabledEnv' -count=1
go test ./pkg/reads -run 'TestDoctorDissentLedgerCompleteness' -count=1
go test ./pkg/mutations -run 'TestHeartbeatLocalWorkAdvancesToolProgress' -count=1
go test ./pkg/sessionliveness -run 'TestClassifyWedgedNoToolProgress|TestClassifyWedgedNoToolProgressDisabledByZeroPolicy' -count=1
go test ./pkg/lanehealth -run 'TestClassifyAliveButSilentKeepsAttestation' -count=1
```

Broad:

```bash
cd go && go test ./...
make -C go build
git diff --check
make check-docs
```

All commands passed in the isolated worktree.

## Residuals

No code changes were made for the older open queue in this rerun. The existing route
ledger remains: #476, #478, #481, #482, #483, #354, #380, #381, and #387 need
human/RFC, split, parked, or gated handling; #477 remains a separate small
read-surface UX fix candidate.

