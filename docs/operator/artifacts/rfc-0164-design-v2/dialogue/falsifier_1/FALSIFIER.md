# FALSIFIER - RFC 0164 v2 C1 checkout/smudge gap

author: falsifier-reviewer-001

## Gate impact

Needs revision. The v2 holder SPEC does not genuinely discharge C1. It widens the taxonomy to the mutation funnel, but it misclassifies checkout-style worktree operations as safe once they use `mutationEnv()`. That is false: `git worktree add` and `git reset --hard` can execute repo-local smudge filters while checking out files, under the daemon identity. The SPEC's P0 closures cover `status` -> fsmonitor, porter `add` -> clean filters, and porter `commit` -> hooksPath, but they leave this checkout/smudge carrier out of the taxonomy, the corpus, and the invariant.

This is a gate-stopping C1 failure because a helper-level env invariant can be green while the daemon still executes attacker-controlled repo config.

## Claim challenged

The challenged claims are A0, A2, A12, A20, A27, and the section 11 green-build claim.

The v2 taxonomy says `mutations/worktree.go:118/181/192/200/297/634/732` (`worktree add/remove`, `rev-parse`, `branch`) closes in P0 with `mutationEnv()`, and says `mutations/revision_routing.go:627` (`reset --hard`) closes with `mutationEnv()`. It does not classify either route as an in-repo-config-sensitive checkout carrier, and it does not require minted config, `--no-checkout` plus controlled materialization, filter-free checkout plumbing, or typed refusal before exec.

That misses a real git execution surface. `mutationEnv()` removes ambient/global/system config, but it does not stop Git from reading the target repo's local config and `.gitattributes` during checkout. Checkout paths honor `filter.<driver>.smudge` for files with matching attributes. So the current P0 proof can route `runGitWorktreeCommand` through `safegit`, satisfy the proposed env-only invariant, and still run attacker code.

## Concrete evidence

Source routes:

- `go/pkg/mutations/worktree.go:118` runs `runGitWorktreeCommand(ctx, repoRoot, "worktree", "add", "--detach", target, inputs.BaseBranch)` to create the per-job worktree.
- `go/pkg/mutations/revision_routing.go:627` runs `runGitWorktreeCommand(ctx, target, "reset", "--hard", tip)` to advance a reopened worktree.
- `go/pkg/mutations/worktree.go:1603-1604` is the funnel body: `exec.CommandContext(ctx, "git", args...)`, with `cmd.Dir = repoRoot` and no subcommand-specific carrier policy today.

I locally reproduced the carrier outside the repository tree. With a temp repo containing:

```ini
[filter "pwn"]
    smudge = sh -c 'touch <sentinel>; cat'
    clean = cat
```

and `.gitattributes` containing `*.pwn filter=pwn`, running `git -C <repo> worktree add --detach <wt> HEAD` created the sentinel. A second temp repo showed `git -C <repo> reset --hard <commit>` also created the sentinel. Both commands are the same checkout class as the daemon routes above. A closed `mutationEnv()` would not change this, because the smudge driver is repository-local config plus attributes, not ambient process env.

The three C1 corpus rows the holder adds do not cover this:

- `quarantine_status_fsmonitor` covers `status` and `core.fsmonitor`.
- `porter_add_filter_clean` covers `git add` and `filter.clean` through a proposed `StageBlob` replacement.
- `porter_commit_hookspath` covers `commit` and `core.hooksPath`.

None of them exercises checkout/materialization carriers: `filter.smudge` during `worktree add`, `reset --hard`, or equivalent checkout paths. The SPEC's residual list is also too narrow: it names arbitrary render and whole-tree `add -A`, but not daemon worktree checkout.

## Secondary exhaustiveness failure

A fresh grep also refutes the literal claim that the table enumerates every daemon-identity call site. For example, `integrateGit` callers exist at `go/pkg/mutations/barrier_run_entity.go:138`, `go/pkg/mutations/barrier_assembly.go:310`, `go/pkg/mutations/recovery_quarantine_lane.go:256`, and many later `worktree.go` sites such as `775`, `1065`, `1174`, `1190`, `1223`, `1236`, `1275`, `1325`, `1336`, `1351`, and `1793`. Some may be semantically safe under a correctly hardened helper, but the v2 table says it is the complete call-site taxonomy and the test allowlist. It is not.

The checkout/smudge issue is the material blocker. The omitted call-site rows are the proof that A2 is still being asserted more strongly than the source-backed taxonomy supports.

## Strongest rebuttal and why it fails

The strongest holder rebuttal is that `worktree add` and `reset --hard` are worktree-admin operations, not read/ref or porter-add operations, and that routing the helper through `mutationEnv()` is enough for P0 while minted config remains Slice 2.

That fails on the holder's own C1 standard. The required taxonomy must route every in-repo-config-sensitive route and every `textconv/filter/hook/fsmonitor` carrier through minted config or a typed pre-exec refusal. Checkout is a filter carrier: it materializes blobs through smudge filters. This is not the arbitrary agent-diff residual, and it is not a whole-tree `add -A` residual. It is the daemon's own worktree creation and reopening path. Treating it as ordinary W-class admin work leaves a daemon-identity RCE open.

A second rebuttal is that the proposed invariant inspects helper call sites. That is necessary but insufficient. If the invariant only proves the helper definition uses `mutationEnv()` and the call-site subcommand is allowlisted, it will go green with this RCE intact. The invariant must also inspect argv-sensitive carrier classes and require minted config, checkout-free materialization, or typed refusal for checkout/smudge operations.

## Unanswered gap

Before C1 can clear, the SPEC needs to add checkout/materialization carriers to the taxonomy and corpus, with red-before / green-after tests such as:

- `worktree_add_filter_smudge`: repo-local `.gitattributes filter=pwn` plus `filter.pwn.smudge=touch S`; drive `worktree.go:118`; assert sentinel red-before and absent after the fix.
- `reset_hard_filter_smudge`: same plant; drive `revision_routing.go:627`; assert sentinel red-before and absent after the fix.

The fix must be one of: route these operations through minted config, avoid checkout by using controlled object extraction/materialization that never invokes filters, or typed-refuse before exec until Slice 2. `mutationEnv()` alone is not a fix.

Until that is specified, the taxonomy is not complete, `recovery_quarantine_lane.go:425` and the three named rows do not prove severance completeness, A12 can be green for the wrong reason, and the C1 gate should not clear.