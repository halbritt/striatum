# Striatum Issue Triage And Fix Rerun: #495 / #496

author: codex-gpt-5-003
date: 2026-06-19
base: b0f635888e573109f4177e62515b972a2a6c07d6
scope: GitHub issues #495 and #496 opened after the #491/#493 rerun.

## Snapshot

After closing #491 and #493, the live open issue set had 12 issues:
#354, #380, #381, #387, #476, #477, #478, #481, #482, #483, #495, and #496.
The new issues for this rerun were #495 and #496.

## Route Ledger

| Issue | Route | Outcome |
|---|---|---|
| #495 | FIX | Daemon socket ACL grant now skips redundant traverse ACLs for already world-traversable directories, avoiding fatal `setfacl` on `/run` while preserving fatal failures for private ancestors, the socket directory, and the socket file. |
| #496 | FIX | `striatum daemon status` now reports the configured client socket and inspects both user and system unit scopes for status output, while install/uninstall remain user-unit scoped. |

## Changes

- `go/cmd/striatumd/socket_acl.go`: filters grant targets whose directory already has `other execute`; clear still targets the complete ACL set and ignores cleanup failures.
- `go/cmd/striatumd/socket_acl_test.go`: regression coverage for skipping a world-traversable ancestor without skipping the socket directory or socket file.
- `go/pkg/cli/localcommands/daemon.go`: status layout honors `STRIATUM_DAEMON_SOCKET`; status unit inspection checks user and system scopes, preferring an active user unit, then an active system unit.
- `go/pkg/cli/localcommands/daemon_test.go`: regression coverage for configured nested sockets and active system-unit status.

## Verification

Focused:

```bash
cd go
go test ./cmd/striatumd -run 'TestGrantDaemonSocketAccessToLaneUser|TestClearDaemonSocketAccessFromLaneUser' -count=1
go test ./pkg/cli/localcommands -run 'TestResolveLayout|TestInspectDaemonUnit|TestRunDaemonStatusUsesDiscoveryTokenWhenClientTokenMissing' -count=1
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

No behavior was added to install or uninstall system units. The status command only
reports them when present or active. Full system-unit lifecycle management remains
a separate product decision if desired.

