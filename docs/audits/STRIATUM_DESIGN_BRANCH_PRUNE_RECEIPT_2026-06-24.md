# Striatum Design Branch Prune Receipt

Date: 2026-06-24
Issue: #602
Receipt note: this file records remote archive/delete actions already executed
before the receipt commit.

## Scope

Pruned only superseded `operator/` scaffold branches with clear replacement
evidence in `docs/operator/rfc-roadmap.md` and `docs/decisions/decision-log.md`.
No active run branch, backup branch, release branch, fix branch, or unknown
worktree/run branch was deleted.

## Archive Tags

Each deleted remote head was first archived as an annotated tag under
`refs/tags/archive/striatum-audit-2026-06-24/`.

| Deleted branch | Archived commit | Archive tag | Supersession evidence |
|---|---|---|---|
| `operator/rfc-0142-p4-v5-scaffold` | `2e48207741c1b17e81c2c8f639f9cf2cddf05702` | `archive/striatum-audit-2026-06-24/operator-rfc-0142-p4-v5-scaffold` | D262 ratified RFC 0142 P4 design v9; roadmap marks P4 built and landed. |
| `operator/rfc-0142-p4-design-v6-scaffold` | `df2a9e703e6406390d354f39abc9b85ce3c970d7` | `archive/striatum-audit-2026-06-24/operator-rfc-0142-p4-design-v6-scaffold` | D262 ratified RFC 0142 P4 design v9; roadmap marks P4 built and landed. |
| `operator/rfc-0143-design-v3-scaffold` | `e5f04db8f632d4f2800a240f9de96e89942b153e` | `archive/striatum-audit-2026-06-24/operator-rfc-0143-design-v3-scaffold` | D261 records the RFC 0143 split after the v7 design sequence; Slice A is landed and Slice B is blocked on RFC 0168. |
| `operator/rfc-0143-v4-scaffold` | `a2b6ad84f16ac2b0945d9b698713b66f58c0fc2d` | `archive/striatum-audit-2026-06-24/operator-rfc-0143-v4-scaffold` | D261 records the RFC 0143 split after the v7 design sequence; Slice A is landed and Slice B is blocked on RFC 0168. |
| `operator/rfc-0143-v5-scaffold` | `96799d6ec244dd85987fcc23517bce8a1b96a5e9` | `archive/striatum-audit-2026-06-24/operator-rfc-0143-v5-scaffold` | D261 records the RFC 0143 split after the v7 design sequence; Slice A is landed and Slice B is blocked on RFC 0168. |
| `operator/rfc-0143-v6-scaffold` | `fa14291c9d443c44380b31170432be72dd2023a3` | `archive/striatum-audit-2026-06-24/operator-rfc-0143-v6-scaffold` | D261 records the RFC 0143 split after the v7 design sequence; Slice A is landed and Slice B is blocked on RFC 0168. |

Archive verification:

```text
2e48207741c1b17e81c2c8f639f9cf2cddf05702 refs/tags/archive/striatum-audit-2026-06-24/operator-rfc-0142-p4-v5-scaffold^{}
96799d6ec244dd85987fcc23517bce8a1b96a5e9 refs/tags/archive/striatum-audit-2026-06-24/operator-rfc-0143-v5-scaffold^{}
a2b6ad84f16ac2b0945d9b698713b66f58c0fc2d refs/tags/archive/striatum-audit-2026-06-24/operator-rfc-0143-v4-scaffold^{}
df2a9e703e6406390d354f39abc9b85ce3c970d7 refs/tags/archive/striatum-audit-2026-06-24/operator-rfc-0142-p4-design-v6-scaffold^{}
e5f04db8f632d4f2800a240f9de96e89942b153e refs/tags/archive/striatum-audit-2026-06-24/operator-rfc-0143-design-v3-scaffold^{}
fa14291c9d443c44380b31170432be72dd2023a3 refs/tags/archive/striatum-audit-2026-06-24/operator-rfc-0143-v6-scaffold^{}
```

## Remote Heads Before

```text
16a018faf76ee25ff63d90336b4706ce3e12e4a5 refs/heads/rfc/0166-alive-but-never-completing-lane-deadline
290d51819d45fb21d57a6f92bc552018d5343fa0 refs/heads/backup/rfc-0169-design-2026-06-24
2d481a44a4f3ca460d8d26574ac5b28039239852 refs/heads/rfc/0165-lane-completion-deadline
2e48207741c1b17e81c2c8f639f9cf2cddf05702 refs/heads/operator/rfc-0142-p4-v5-scaffold
4d686f8acdeb5467b97e41a7b0f840159fa93674 refs/heads/backup/rfc-0136-design-2026-06-24
4eb0b4e5e6cec5a2943aab2732cbf3e41909f5bf refs/heads/backup/rfc-0166-design-v2-2026-06-24
5c5cb36a8fb390e79b1413037373af32cf32258a refs/heads/worktree-agent-a738f710ff01da625
68af4387d97e3e573de027f98b9a9d1a094e311e refs/heads/backup/rfc-0164-design-v2-2026-06-24
6dbd75402c1897a8e2eb86124121c9ec7ac4e897 refs/heads/fix/rfc0142-p1-ownership-lint-and-reservation-ledger
96799d6ec244dd85987fcc23517bce8a1b96a5e9 refs/heads/operator/rfc-0143-v5-scaffold
a2b6ad84f16ac2b0945d9b698713b66f58c0fc2d refs/heads/operator/rfc-0143-v4-scaffold
bfe754a004b0e1a0f4e77b1c52ad001b838e99e1 refs/heads/main
c3716c5adb73bc9fb4f1983bd4796dbafcb20cfa refs/heads/backup/rfc-0168-design-v2-2026-06-24
ccec0b5b4d909d83e21341db185ee6617b8d39e1 refs/heads/fix/rfc0142-p2-owner-bundle-watermark-interlock
d282bc4dfcc8f14400eb0eb5b4cad57cf4aacdfe refs/heads/striatum/di-run2
da9f9da04a4bf31d0ac59fb0c3ce671d44e8046b refs/heads/backup/rfc-0165-design-v4-2026-06-24
df2a9e703e6406390d354f39abc9b85ce3c970d7 refs/heads/operator/rfc-0142-p4-design-v6-scaffold
e5f04db8f632d4f2800a240f9de96e89942b153e refs/heads/operator/rfc-0143-design-v3-scaffold
fa14291c9d443c44380b31170432be72dd2023a3 refs/heads/operator/rfc-0143-v6-scaffold
```

## Remote Heads After

```text
16a018faf76ee25ff63d90336b4706ce3e12e4a5 refs/heads/rfc/0166-alive-but-never-completing-lane-deadline
290d51819d45fb21d57a6f92bc552018d5343fa0 refs/heads/backup/rfc-0169-design-2026-06-24
2d481a44a4f3ca460d8d26574ac5b28039239852 refs/heads/rfc/0165-lane-completion-deadline
4d686f8acdeb5467b97e41a7b0f840159fa93674 refs/heads/backup/rfc-0136-design-2026-06-24
4eb0b4e5e6cec5a2943aab2732cbf3e41909f5bf refs/heads/backup/rfc-0166-design-v2-2026-06-24
5c5cb36a8fb390e79b1413037373af32cf32258a refs/heads/worktree-agent-a738f710ff01da625
68af4387d97e3e573de027f98b9a9d1a094e311e refs/heads/backup/rfc-0164-design-v2-2026-06-24
6dbd75402c1897a8e2eb86124121c9ec7ac4e897 refs/heads/fix/rfc0142-p1-ownership-lint-and-reservation-ledger
bfe754a004b0e1a0f4e77b1c52ad001b838e99e1 refs/heads/main
c3716c5adb73bc9fb4f1983bd4796dbafcb20cfa refs/heads/backup/rfc-0168-design-v2-2026-06-24
ccec0b5b4d909d83e21341db185ee6617b8d39e1 refs/heads/fix/rfc0142-p2-owner-bundle-watermark-interlock
d282bc4dfcc8f14400eb0eb5b4cad57cf4aacdfe refs/heads/striatum/di-run2
da9f9da04a4bf31d0ac59fb0c3ce671d44e8046b refs/heads/backup/rfc-0165-design-v4-2026-06-24
```

## Preserved Branches

- `backup/*`: preserved as explicit backup/archive heads, including active
  Wave 0/Wave 1 design backups.
- `rfc/0165-lane-completion-deadline` and
  `rfc/0166-alive-but-never-completing-lane-deadline`: preserved because they
  are not superseded scaffold heads.
- `fix/rfc0142-*`: preserved because they are fix branches, not design-vN
  scaffold branches.
- `striatum/di-run2` and `worktree-agent-a738f710ff01da625`: preserved as
  unknown live/worktree-like heads.
- `main`: preserved.

## Commands

```text
git ls-remote --heads origin
git tag -a archive/striatum-audit-2026-06-24/<name> <sha> -m "Archive superseded <branch> before #602 branch pruning"
git push origin refs/tags/archive/striatum-audit-2026-06-24/<name> ...
git push origin :refs/heads/operator/rfc-0142-p4-v5-scaffold ...
git ls-remote --heads origin
git ls-remote --tags origin 'refs/tags/archive/striatum-audit-2026-06-24/*^{}'
```
