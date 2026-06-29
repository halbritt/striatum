---
type: record
status: frozen
owner: GEMINI
expires: null
author: worker-gemini-1
---

# Striatum Git Hygiene Cleanup Report — 2026-06-10

## Executive Summary
This report details the execution of Milestone 3 (Cleanup Execution) of the Git Hygiene task for the Striatum repository at `~/git/striatum`.
Following the safety guidelines, 14 local branches, 6 remote-tracking refs, 7 obsolete stashes, and 3 dogfood tags have been successfully deleted/dropped.
All protected/deferred refs, including `main`,checked-out branches in active subagent and daemon worktrees, release tags, and unmerged unique branches, remain completely untouched.
A comprehensive recovery map has been created, capturing the tip SHAs of every deleted ref, and all deletions have been verified to still exist in the Git object database (revertible).
Post-cleanup tests ran and passed successfully.

---

## Category Inventory Counts

| Ref Category | Count (Before) | Count (After) | Change | Action Taken |
| --- | --- | --- | --- | --- |
| **Local Branches** | 31 | 17 | -14 | Deleted 14 merged or patch-equivalent branches |
| **Remote-Tracking Refs** | 12 | 6 | -6 | Deleted 6 merged or patch-equivalent remote-tracking branches |
| **Obsolete Stashes** | 7 | 0 | -7 | Dropped 7 landed/obsolete stashes (highest index to lowest) |
| **Tags** | 116 | 113 | -3 | Deleted 3 dogfood tags (reachable from `main`) |
| **Worktrees** | 22 | 22 | 0 | None deleted (all are active, locked, or dirty) |
| **Remotes** | 1 | 1 | 0 | None deleted (single valid remote `origin` protected) |

---

## Recovery Map

The following table lists every deleted ref, its tip SHA, last-commit date, commit subject, evidence class, and the exact Git command to recover/restore the ref. All tip SHAs remain in the Git object database and can be fully recovered.

| Ref / Type | Tip SHA | Date | Subject | Evidence Class | Recovery Command |
| --- | --- | --- | --- | --- | --- |
| **Local Branch** `tmp-land-a` | `2eade55c4187f67adf32df51a4476d801557d410` | Sun Jun 7 04:18:37 2026 | fix(claim): legible needs_operator claim + auto-close moot recovery blocker (#207) | **Class A**: Fully merged local branch | `git branch tmp-land-a 2eade55c4187f67adf32df51a4476d801557d410` |
| **Local Branch** `tmp-land-c` | `1a2ea87b9c6e5ee9449f9b0f6b5543747c1f9d14` | Sun Jun 7 05:22:58 2026 | fix(recovery): never confirm death on pid_identity_unavailable probe (#198) | **Class A**: Fully merged local branch | `git branch tmp-land-c 1a2ea87b9c6e5ee9449f9b0f6b5543747c1f9d14` |
| **Local Branch** `fix/wave-a-quick-wins` | `15a682f2ae0c18dd1ee53e7744ae1ca7bba0d828` | Sun Jun 7 03:02:06 2026 | fix(claim): legible needs_operator claim + auto-close moot recovery blocker (#207) | **Class B**: Patch-equivalent local branch | `git branch fix/wave-a-quick-wins 15a682f2ae0c18dd1ee53e7744ae1ca7bba0d828` |
| **Local Branch** `fix/wave-b-revision-integrity` | `cce4828af30a6ee960bad4af7d63dc4dfafa4e65` | Sun Jun 7 04:04:54 2026 | chore(go): raise test/race/coverage timeout to 30m for the real-PG suite | **Class B**: Patch-equivalent local branch | `git branch fix/wave-b-revision-integrity cce4828af30a6ee960bad4af7d63dc4dfafa4e65` |
| **Local Branch** `fix/wave-c-daemon-load` | `f04b809458020851da1c16fb2ef3444f05e4a901` | Sun Jun 7 03:11:40 2026 | fix(status): bound repo-wide payload + degrade helper drain on contention (#193) | **Class B**: Patch-equivalent local branch | `git branch fix/wave-c-daemon-load f04b809458020851da1c16fb2ef3444f05e4a901` |
| **Local Branch** `fix/wave-d-usage-gen` | `a9c32f8b497ae101cfdd5a029eb92898bd5e6eff` | Sun Jun 7 03:53:26 2026 | feat(cli): generate --help for daemon-derived verbs from the params table (#194) | **Class B**: Patch-equivalent local branch | `git branch fix/wave-d-usage-gen a9c32f8b497ae101cfdd5a029eb92898bd5e6eff` |
| **Local Branch** `rfc/0111-failure-legibility` | `5a4004e672d53493c83eaf7a950dda9d827928c4` | Wed Jun 3 16:30:35 2026 | docs(rfc-0111): link companion work items to tracking issues #160/#161 | **Class B**: Patch-equivalent local branch | `git branch rfc/0111-failure-legibility 5a4004e672d53493c83eaf7a950dda9d827928c4` |
| **Local Branch** `rfc/0114-read-scope-principals-sessions` | `3efa3908956176a944eed10263cea9e6d73eb8ff` | Sat Jun 6 17:08:06 2026 | docs(rfc-0114): read-scope least privilege successor — principals + sessions (#164) | **Class B**: Patch-equivalent local branch | `git branch rfc/0114-read-scope-principals-sessions 3efa3908956176a944eed10263cea9e6d73eb8ff` |
| **Local Branch** `striatum/agy-loop-smoke` | `61d87d4ffbee3fd44f34cfbb372d7dda382e780d` | Fri May 29 05:58:18 2026 | fix(agentloop): inject agy MCP via .gemini/settings.json, not --mcp-config (#51) | **Class B**: Patch-equivalent local branch | `git branch striatum/agy-loop-smoke 61d87d4ffbee3fd44f34cfbb372d7dda382e780d` |
| **Local Branch** `striatum/fix-51-agy-submit-driver` | `d8587b7720443d132decff45c5f966e8b53e6d3a` | Fri May 29 06:06:39 2026 | fix(agentloop): inject agy MCP via .gemini/settings.json, not --mcp-config (#51) | **Class B**: Patch-equivalent local branch | `git branch striatum/fix-51-agy-submit-driver d8587b7720443d132decff45c5f966e8b53e6d3a` |
| **Local Branch** `striatum/followup-cleanup` | `3d26321f697b98c67f97cfcafd36727711ad5b70` | Fri May 29 07:13:33 2026 | fix(reads): stop counting recovered/completed expired leases as stale (#45) | **Class B**: Patch-equivalent local branch | `git branch striatum/followup-cleanup 3d26321f697b98c67f97cfcafd36727711ad5b70` |
| **Local Branch** `striatum/issue-51-followups` | `f168fd9bf024b0fda60f85b84ddd9f3c6a34f276` | Fri May 29 06:27:15 2026 | fix(agentloop): steer agents to execute claimed packets inline (#51) | **Class B**: Patch-equivalent local branch | `git branch striatum/issue-51-followups f168fd9bf024b0fda60f85b84ddd9f3c6a34f276` |
| **Local Branch** `striatum/rfc-0088-finalize` | `dd373c6f4c3f429d5b1b2a42d0cb10fedfa55cc7` | Fri May 29 06:15:19 2026 | docs: land RFC 0088 — accept D148-D151, flip status, glossary delta | **Class B**: Patch-equivalent local branch | `git branch striatum/rfc-0088-finalize dd373c6f4c3f429d5b1b2a42d0cb10fedfa55cc7` |
| **Local Branch** `striatum/rfc-0114-impl-164` | `4ec6b7f89112772d3a02eee7c925ed941a92104c` | Sun Jun 7 09:50:30 2026 | docs(rfc-0114): apply-job summary — gates re-run, operator runbook (#164) | **Class B**: Patch-equivalent local branch | `git branch striatum/rfc-0114-impl-164 4ec6b7f89112772d3a02eee7c925ed941a92104c` |
| **Remote-Tracking Ref** `origin/striatum/rfc-0110-pg-auth-panel` | `43c2521b1ff12499b4a98eff6ebd918e04ef00a0` | Wed Jun 3 18:02:10 2026 | docs(rfc-0110): design-panel artifacts — implementation-ready spec (run_8e14cb48) | **Class C**: Gone remote-tracking ref | `git update-ref refs/remotes/origin/striatum/rfc-0110-pg-auth-panel 43c2521b1ff12499b4a98eff6ebd918e04ef00a0` |
| **Remote-Tracking Ref** `origin/fix/wave-c-daemon-load` | `f04b809458020851da1c16fb2ef3444f05e4a901` | Sun Jun 7 03:11:40 2026 | fix(status): bound repo-wide payload + degrade helper drain on contention (#193) | **Class C**: Gone remote-tracking ref | `git update-ref refs/remotes/origin/fix/wave-c-daemon-load f04b809458020851da1c16fb2ef3444f05e4a901` |
| **Remote-Tracking Ref** `origin/striatum/fix-51-agy-submit-driver` | `d8587b7720443d132decff45c5f966e8b53e6d3a` | Fri May 29 06:06:39 2026 | fix(agentloop): inject agy MCP via .gemini/settings.json, not --mcp-config (#51) | **Class C**: Gone remote-tracking ref | `git update-ref refs/remotes/origin/striatum/fix-51-agy-submit-driver d8587b7720443d132decff45c5f966e8b53e6d3a` |
| **Remote-Tracking Ref** `origin/striatum/followup-cleanup` | `3d26321f697b98c67f97cfcafd36727711ad5b70` | Fri May 29 07:13:33 2026 | fix(reads): stop counting recovered/completed expired leases as stale (#45) | **Class C**: Gone remote-tracking ref | `git update-ref refs/remotes/origin/striatum/followup-cleanup 3d26321f697b98c67f97cfcafd36727711ad5b70` |
| **Remote-Tracking Ref** `origin/striatum/issue-51-followups` | `f168fd9bf024b0fda60f85b84ddd9f3c6a34f276` | Fri May 29 06:27:15 2026 | fix(agentloop): steer agents to execute claimed packets inline (#51) | **Class C**: Gone remote-tracking ref | `git update-ref refs/remotes/origin/striatum/issue-51-followups f168fd9bf024b0fda60f85b84ddd9f3c6a34f276` |
| **Remote-Tracking Ref** `origin/striatum/rfc-0088-finalize` | `dd373c6f4c3f429d5b1b2a42d0cb10fedfa55cc7` | Fri May 29 06:15:19 2026 | docs: land RFC 0088 — accept D148-D151, flip status, glossary delta | **Class C**: Gone remote-tracking ref | `git update-ref refs/remotes/origin/striatum/rfc-0088-finalize dd373c6f4c3f429d5b1b2a42d0cb10fedfa55cc7` |
| **Stash** `stash@{0}` | `e492b51274df762cbde344809107d123f15e97ad` | Fri Jun 5 15:05:12 2026 | On main: striatum-rfc0112-preserve-out-of-scope-20260605T150512Z | **Class D**: Landed stash | `git branch recover-stash-0 e492b51274df762cbde344809107d123f15e97ad` (or `git stash apply e492b51274df762cbde344809107d123f15e97ad`) |
| **Stash** `stash@{1}` | `cea3a845bb84a72be4b621e9db56bb782120bb57` | Fri May 29 09:35:23 2026 | WIP on striatum/rfc-0088-0089-closeout: 9f934fe feat(supervision): complete RFC 0088 and 0089 closeout | **Class D**: Landed stash | `git branch recover-stash-1 cea3a845bb84a72be4b621e9db56bb782120bb57` (or `git stash apply cea3a845bb84a72be4b621e9db56bb782120bb57`) |
| **Stash** `stash@{2}` | `ddc861309855a9c9b18160e0098390437ef982fa` | Mon May 11 02:02:56 2026 | On striatum/dogfood-030-rfc-0026-0027-provenance: operator-design-artifacts-from-run-4ee6 | **Class D**: Landed stash | `git branch recover-stash-2 ddc861309855a9c9b18160e0098390437ef982fa` (or `git stash apply ddc861309855a9c9b18160e0098390437ef982fa`) |
| **Stash** `stash@{3}` | `9d8b3394c9594ecd76780095452d4d278a7d5090` | Mon May 11 01:52:35 2026 | On striatum/dogfood-030-rfc-0026-0027-provenance: operator-design-artifacts-from-run-4233675 | **Class D**: Landed stash | `git branch recover-stash-3 9d8b3394c9594ecd76780095452d4d278a7d5090` (or `git stash apply 9d8b3394c9594ecd76780095452d4d278a7d5090`) |
| **Stash** `stash@{4}` | `0203610e29fa32b0e239caf7c0ebd12ddfc24d31` | Sun May 10 23:46:50 2026 | On striatum/dogfood-030-rfc-0026-0027-provenance: operator-gemini-artifact-from-canceled-run-debace | **Class D**: Landed stash | `git branch recover-stash-4 0203610e29fa32b0e239caf7c0ebd12ddfc24d31` (or `git stash apply 0203610e29fa32b0e239caf7c0ebd12ddfc24d31`) |
| **Stash** `stash@{5}` | `76221b5c981504b5e2f20dea05651a2c6199330a` | Sun May 10 23:37:50 2026 | On striatum/dogfood-030-rfc-0026-0027-provenance: operator-gemini-artifact-before-clean-retry | **Class D**: Landed stash | `git branch recover-stash-5 76221b5c981504b5e2f20dea05651a2c6199330a` (or `git stash apply 76221b5c981504b5e2f20dea05651a2c6199330a`) |
| **Stash** `stash@{6}` | `bc2b3e78e88cf29fed555e2817ea810ae758f8af` | Sun May 10 23:23:40 2026 | On striatum/dogfood-030-rfc-0026-0027-provenance: operator-run-residue-before-rebase | **Class D**: Landed stash | `git branch recover-stash-6 bc2b3e78e88cf29fed555e2817ea810ae758f8af` (or `git stash apply bc2b3e78e88cf29fed555e2817ea810ae758f8af`) |
| **Tag** `dogfood-001` | `4e372820200c6ed7ef35075fffb604ea1f4d4243` | Thu May 7 23:56:11 2026 | first V1 dogfood: DOT export shipped, four harness findings filed | **Class E**: Reachable scratch tag | `git tag dogfood-001 4e372820200c6ed7ef35075fffb604ea1f4d4243` |
| **Tag** `dogfood-001-v2` | `8b60e633d7b6f23085d087e1154fc87bc96145a9` | Fri May 8 02:03:03 2026 | harness fixes round 1: HARNESS-001/002/003/004 landed | **Class E**: Reachable scratch tag | `git tag dogfood-001-v2 8b60e633d7b6f23085d087e1154fc87bc96145a9` |
| **Tag** `dogfood-002` | `043ba8c8191fef23d60118a9da2c319b33fee147` | Fri May 8 02:31:46 2026 | land RFC 0011 (session close + run-terminal auto-close) | **Class E**: Reachable scratch tag | `git tag dogfood-002 043ba8c8191fef23d60118a9da2c319b33fee147` |

---

## Detailed Sections

### Done Branches
- **Local branch `tmp-land-a` & `tmp-land-c`**: Both local branches were fully merged into the default branch `main`. We used standard delete `git branch -d`.
- **12 other local branches (`fix/wave-a-quick-wins`, `fix/wave-b-revision-integrity`, `fix/wave-c-daemon-load`, `fix/wave-d-usage-gen`, `rfc/0111-failure-legibility`, `rfc/0114-read-scope-principals-sessions`, `striatum/agy-loop-smoke`, `striatum/fix-51-agy-submit-driver`, `striatum/followup-cleanup`, `striatum/issue-51-followups`, `striatum/rfc-0088-finalize`, `striatum/rfc-0114-impl-164`)**: These branches were patch-equivalent; their changes have been squashed/rebased into `main`. We used `git branch -D` to safely delete them since they weren't strictly fast-forward/direct merges.
- **6 remote-tracking refs (`origin/striatum/rfc-0110-pg-auth-panel`, `origin/fix/wave-c-daemon-load`, `origin/striatum/fix-51-agy-submit-driver`, `origin/striatum/followup-cleanup`, `origin/striatum/issue-51-followups`, `origin/striatum/rfc-0088-finalize`)**: These tracking branches represent branches that have already been merged/completed upstream. We cleaned them up locally using `git branch -dr`.

#### Exact Execution Commands & Outputs for Branches:
```
$ git branch -d tmp-land-a
Deleted branch tmp-land-a (was 2eade55c).

$ git branch -d tmp-land-c
Deleted branch tmp-land-c (was 1a2ea87b).

$ git branch -D fix/wave-a-quick-wins
Deleted branch fix/wave-a-quick-wins (was 15a682f2).

$ git branch -D fix/wave-b-revision-integrity
Deleted branch fix/wave-b-revision-integrity (was cce4828a).

$ git branch -D fix/wave-c-daemon-load
Deleted branch fix/wave-c-daemon-load (was f04b8094).

$ git branch -D fix/wave-d-usage-gen
Deleted branch fix/wave-d-usage-gen (was a9c32f8b).

$ git branch -D rfc/0111-failure-legibility
Deleted branch rfc/0111-failure-legibility (was 5a4004e6).

$ git branch -D rfc/0114-read-scope-principals-sessions
Deleted branch rfc/0114-read-scope-principals-sessions (was 3efa3908).

$ git branch -D striatum/agy-loop-smoke
Deleted branch striatum/agy-loop-smoke (was 61d87d4f).

$ git branch -D striatum/fix-51-agy-submit-driver
Deleted branch striatum/fix-51-agy-submit-driver (was d8587b77).

$ git branch -D striatum/followup-cleanup
Deleted branch striatum/followup-cleanup (was 3d26321f).

$ git branch -D striatum/issue-51-followups
Deleted branch striatum/issue-51-followups (was f168fd9b).

$ git branch -D striatum/rfc-0088-finalize
Deleted branch striatum/rfc-0088-finalize (was dd373c6f).

$ git branch -D striatum/rfc-0114-impl-164
Deleted branch striatum/rfc-0114-impl-164 (was 4ec6b7f8).

$ git branch -dr origin/striatum/rfc-0110-pg-auth-panel
Deleted remote-tracking branch origin/striatum/rfc-0110-pg-auth-panel (was 43c2521b).

$ git branch -dr origin/fix/wave-c-daemon-load
Deleted remote-tracking branch origin/fix/wave-c-daemon-load (was f04b8094).

$ git branch -dr origin/striatum/fix-51-agy-submit-driver
Deleted remote-tracking branch origin/striatum/fix-51-agy-submit-driver (was d8587b77).

$ git branch -dr origin/striatum/followup-cleanup
Deleted remote-tracking branch origin/striatum/followup-cleanup (was 3d26321f).

$ git branch -dr origin/striatum/issue-51-followups
Deleted remote-tracking branch origin/striatum/issue-51-followups (was f168fd9b).

$ git branch -dr origin/striatum/rfc-0088-finalize
Deleted remote-tracking branch origin/striatum/rfc-0088-finalize (was dd373c6f).
```

### Other Done Items
- **Stashes**: All 7 stashes were dropped. Stashes 0 and 1 modified code that already landed on `main`. Stashes 2-6 contained dogfood 030 files which have been migrated to blob storage and deleted from the working tree per RFC 0072. We dropped stashes sequentially from index 6 to 0 to prevent index shifting.
- **Tags**: Dogfood tags `dogfood-001`, `dogfood-001-v2`, and `dogfood-002` were deleted. All of these were reachable from `main`'s history, meaning no commit history was lost.

#### Exact Execution Commands & Outputs for Stashes & Tags:
```
$ git stash drop stash@{6}
Dropped stash@{6} (bc2b3e78e88cf29fed555e2817ea810ae758f8af)

$ git stash drop stash@{5}
Dropped stash@{5} (76221b5c981504b5e2f20dea05651a2c6199330a)

$ git stash drop stash@{4}
Dropped stash@{4} (0203610e29fa32b0e239caf7c0ebd12ddfc24d31)

$ git stash drop stash@{3}
Dropped stash@{3} (9d8b3394c9594ecd76780095452d4d278a7d5090)

$ git stash drop stash@{2}
Dropped stash@{2} (ddc861309855a9c9b18160e0098390437ef982fa)

$ git stash drop stash@{1}
Dropped stash@{1} (cea3a845bb84a72be4b621e9db56bb782120bb57)

$ git stash drop stash@{0}
Dropped stash@{0} (e492b51274df762cbde344809107d123f15e97ad)

$ git tag -d dogfood-001
Deleted tag 'dogfood-001' (was 4e372820)

$ git tag -d dogfood-001-v2
Deleted tag 'dogfood-001-v2' (was 8b60e633)

$ git tag -d dogfood-002
Deleted tag 'dogfood-002' (was 043ba8c8)
```

### Deferred Items
The following items were deferred to the maintainer for protection:
1. **Local branch `main`**: Primary branch, checked out in `~/git/striatum`.
2. **13 Local checked-out branches in active worktrees**:
   - `fix/issue57-write-scope-guard` (worktree locked by subagent pid 1953046)
   - `fix/issue58-idempotent-submit-review` (worktree locked by subagent pid 1953046)
   - `fix/issue62-gemini-teardown` (worktree locked by subagent pid 1953046)
   - `fix/issue63-attestation-stall` (worktree locked by subagent pid 1953046)
   - `fix/issue63-cli-dx` (worktree locked by subagent pid 1953046)
   - `fix/issue63-f10-delivery-gate` (worktree locked by subagent pid 1953046)
   - `fix/issue63-f2-override` (worktree locked by subagent pid 1953046)
   - `fix/issue63-liveness-dx` (worktree locked by subagent pid 1953046)
   - `fix/issue63-retire-agy-oneshot` (worktree locked by subagent pid 1953046)
   - `fix/issue63-revision-routing` (worktree locked by subagent pid 1953046)
   - `fix/phase2-reopen-safety` (worktree locked by subagent pid 1953046)
   - `worktree-agent-a0acdbd5727213fd5` (worktree locked by subagent pid 1953046)
   - `worktree-agent-a6404a376a18e0652` (worktree locked by subagent pid 1953046)
3. **3 Local branches with unique unmerged work**:
   - `prompts/run-retrospective-migration`: Adds unique file `prompts/RUN_RETROSPECTIVE.md`.
   - `striatum/rfc-0115-agent-loop-telemetry`: Contains unique telemetry output docs.
   - `wip/rfc-0094-slice1-dogfood`: Contains WIP dogfood output and known bugs.
4. **4 Remote-tracking branches with unique unmerged work**:
   - `origin/claude/striatum-overview-Qu4b8`: Proposes RFC 0088 `--with-optional` flag.
   - `origin/prompts/run-retrospective-migration`
   - `origin/striatum/rfc-0115-agent-loop-telemetry`
   - `origin/wip/rfc-0094-slice1-dogfood`

### Verified Clean Categories
- **Worktrees**: 22 worktrees remain registered. None were prunable because they are either locked, active, or contain uncommitted changes.
- **Remotes**: Configured remote `origin` is protected and active. No dead or duplicate remotes exist.
- **Release Tags**: 113 release-shaped tags matching `v*` are preserved.

### Follow-ups
1. **Pruning Remote Branches**: Since authority for deleting remote tracking refs locally was executed, once the local active sessions/worktrees are completed and unlocked by subagents, the corresponding protected branches can also be merged/cleaned up.
2. **Object Pruning**: Since `git gc` and object pruning were forbidden, all deleted/dropped commits still physically occupy space in the repository. They will eventually be reclaimed by standard Git auto-GC cycles in the future, but they remain revertible for now.
