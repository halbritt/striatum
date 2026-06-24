# FALSIFIER - RFC 0164 v2 C1 checkout/smudge carrier gap

author: falsifier-reviewer-003

## Gate impact

Needs revision. The v2 holder improves the C1 taxonomy by adding the mutation funnel and by naming the prior `status`/`add`/`commit` rows, but it still does not genuinely discharge severance-completeness. It classifies checkout-style worktree materialization as ordinary worktree administration closed by `mutationEnv()`. That is not enough: checkout paths can execute repo-local `filter.<driver>.smudge` commands through `.gitattributes`, under the daemon identity, even when the process environment is born-closed.

This leaves a daemon-run git funnel where A12 can be green for the wrong reason. A helper-level invariant that proves `runGitWorktreeCommand` uses `mutationEnv()` does not prove that an in-repo-config-sensitive checkout carrier was minted away or refused before exec.

## Claim challenged

The challenged claims are A0, A2, A12, A20, A27, and the section 11 green-build claim.

The holder's v2 taxonomy lists `mutations/worktree.go:118/181/192/200/297/634/732` as `worktree add/remove`, `rev-parse`, and `branch`, class W/R, closed in P0 by `mutationEnv()`. It also lists `mutations/revision_routing.go:627` `reset --hard` as class W, closed by `mutationEnv()`. Neither route is classified as a filter carrier. Neither is routed through minted config, checkout-free materialization, or a typed pre-exec refusal.

That violates C1's own rule: every in-repo-config-sensitive route, including filter carriers, must go through a minted config or typed refusal. Checkout is a filter carrier because Git materializes blobs through `.gitattributes` smudge filters.

## Concrete evidence

Source routes verified in the current worktree:

- `go/pkg/mutations/worktree.go:118` calls `runGitWorktreeCommand(ctx, repoRoot, "worktree", "add", "--detach", target, inputs.BaseBranch)` to create daemon worktrees.
- `go/pkg/mutations/revision_routing.go:627` calls `runGitWorktreeCommand(ctx, target, "reset", "--hard", tip)` to advance a reopened worktree.
- `go/pkg/mutations/worktree.go:1604` is the shared funnel body: `exec.CommandContext(ctx, "git", args...)`, with `cmd.Dir = repoRoot`.

I reproduced the carrier outside the repository tree with temp repos. The plant was repo-local config plus attributes:

```ini
[filter "pwn"]
    smudge = <script that touches a sentinel and cats stdin>
    clean = cat
```

and `.gitattributes` contained `*.pwn filter=pwn`.

Results from this run:

- `git -C <repo> worktree add --detach <wt> HEAD` created the sentinel.
- The same command under a closed environment shaped like the holder's `mutationEnv()` also created the sentinel: `SMUDGE_FIRED_UNDER_CLOSED_ENV`.
- `git -C <repo> reset --hard <commit>` under the same closed environment created the sentinel: `RESET_SMUDGE_FIRED_UNDER_CLOSED_ENV`.

So the failure is not ambient `GIT_*`, global/system config, or `os.Environ()` leakage. It is repo-local config and attributes read during checkout/materialization. `mutationEnv()` does not close it.

The three C1 rows the holder names do not cover this class:

- `quarantine_status_fsmonitor` covers `status` and `core.fsmonitor`.
- `porter_add_filter_clean` covers staging through `filter.clean` and a proposed `StageBlob` replacement.
- `porter_commit_hookspath` covers `commit` and `core.hooksPath`.

None exercises `filter.smudge` during `worktree add`, `reset --hard`, or an equivalent checkout/materialization operation.

## Strongest rebuttal and why it fails

The strongest rebuttal is that worktree creation and reset are W-class administration, not read/ref or porter staging, and that P0 is only trying to route the helper through a closed env while Slice 2 handles minted config later.

That fails under the holder's own revised C1 standard. The v2 SPEC says every in-repo-config-sensitive route and every textconv/filter/hook/fsmonitor carrier is either neutralized or refused. Checkout/materialization is a filter carrier on the daemon path. It is not the arbitrary agent-diff residual, and it is not the whole-tree `add -A` residual. It is how Striatum creates and reopens daemon worktrees.

The second rebuttal is that A12 inspects helper call sites. That is necessary but insufficient. If the invariant only proves the helper definition uses `mutationEnv()` and that `worktree` / `reset` are allowlisted, it will pass while this RCE remains. The invariant must understand argv-sensitive carrier classes and require minted config, checkout-free materialization, or typed refusal for checkout/smudge operations.

## Unanswered gap

Before C1 can clear, the SPEC needs to add checkout/materialization carriers to the taxonomy and corpus. The missing red-before / green-after rows are at least:

- `worktree_add_filter_smudge`: repo-local `.gitattributes filter=pwn` plus `filter.pwn.smudge=<sentinel>`; drive the `worktree.go:118` path; assert red-before and absent after the fix.
- `reset_hard_filter_smudge`: same plant; drive the `revision_routing.go:627` path; assert red-before and absent after the fix.

A valid fix must route these operations through minted config, avoid checkout by controlled materialization that never invokes filters, or typed-refuse before exec until Slice 2. `mutationEnv()` alone is not a fix.

Until this is specified, the taxonomy is still incomplete, the three named C1 rows are not a severance-completeness certificate, A12 can be green while a daemon-identity smudge RCE remains, and the C1 gate should not clear.