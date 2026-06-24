# FALSIFIER - RFC 0168 P0 C2 ACL transition exactness challenge

author: falsifier-reviewer-004

## Result

C2 is materially improved but not genuinely cleared. I credit the revised holder for the intended final ACL state: no `g:striatum-lanes` access or default ACL under `.striatum/` or `.git`, `.striatum/` traverse keyed only to the currently leased uid, scratch access scoped to the lane's own supervisor directory, worktrees owned by the leasing uid, and A16 checking unleased and different-leased pool uids against MCP bearer, PTY log, token cache, and foreign worktree reads.

The remaining C2 blocker is narrower and still load-bearing: the spec permits a live-repo transition that first applies `setfacl -R -m g:striatum-lanes:rX -m d:g:striatum-lanes:rX <repoRoot>` and only afterward strips `.striatum/` and `.git`. That transiently grants the pool group read/traverse to existing `.striatum/` control-plane files before the final-state carve-out runs. A session bearer or PTY log copied during that window is already lost; a later green final-state audit cannot revoke the disclosure.

## Claim Challenged

The C2 seed requires an ACL grant that excludes `.striatum/`, preserves the `u:<lane>:--x` traverse-only carve-out, grants exactly enough access to the leased uid, and proves a pool uid that is not leased to the target cannot read another lane's `0600` control-plane file. The v2 holder restates that same goal as a hard boundary, but its implementation section still allows two incompatible claims:

- `HOLDER.md:350-355` blesses a raw recursive grant over `<repoRoot>` followed by a mandatory strip of `<repoRoot>/.striatum` and `<repoRoot>/.git`.
- `HOLDER.md:356-362` says the gate is the auditable end-state, while the invariant says no `.striatum/` path carries the group entry before or after provisioning.
- `HOLDER.md:394-402` argues the original `-R ...:rX` adds-group-read-to-0600 problem cannot arise because no group ACL ever touches `.striatum/`.

The first bullet contradicts the second and third. A recursive grant over `<repoRoot>` necessarily touches `.striatum/` while it is running. The fact that the next command removes the entry proves the final state can be made right, not that `.striatum/` was excluded.

## Source Evidence

The exposed material is real control-plane state, not harmless source:

- `go/pkg/agentloop/mcpconfig.go:550-570` creates `lane-mcp-config-*.json` under `<repoRoot>/.striatum/scratch` and chmods it `0600`; its body carries the MCP endpoint and bearer material.
- `go/pkg/agentloop/loop.go:139-145` resolves `pty.log` under `.striatum/scratch/<supervisor_id>/`, and `loop.go:280-300` opens it as a local-only `0600` operator diagnostic.
- `go/pkg/mutations/scratch_acl.go:31-48` documents the existing narrow carve-out: `.striatum` gets only `u:<lane>:--x`, explicitly to avoid broadening read access to private operator state.
- `docs/how-to/lane-sandbox.md:350-355` says `.striatum/` is daemon/operator-private and that the only lane exception is that job's `.striatum/worktrees/<id>` subtree.
- `go/pkg/admin/repo_acl.go:21-31` shows the existing ACL helper is a recursive `setfacl -R -m`; `repo_acl.go:126-135` targets both the repo tree and `.striatum/worktrees` today, which is exactly the recursion shape the v2 pool spec says must not be replicated into control-plane scratch.

I also checked POSIX ACL behavior on this host in `/tmp`: starting from an existing mode `0600` file under `.striatum/scratch/s1`, `setfacl -R -m g:$(id -gn):rX <tmp-root>` added a named-group `r--` ACL to that file; the later `setfacl -R -x g:$(id -gn) -k <tmp-root>/.striatum` removed it. That matches the v1 C2 premise: `0600` is not a rebuttal once the recursive group grant has run.

## Concrete Failing Case

1. A registered repo already has a live lane S1 with `.striatum/scratch/<S1>/lane-mcp-config-*.json` and `.striatum/scratch/<S1>/pty.log`, both mode `0600`.
2. A different pool uid U2 is active in another lane, or an unleased residual pool uid process exists. U2 repeatedly attempts to open S1's bearer path. This is the exact C2 attacker class: a uid that is not S1's leased uid must not read S1's control-plane material.
3. The operator or build implements the v2-permitted provisioning path: recursive `g:striatum-lanes:rX` over `<repoRoot>`, then strip `.striatum/`.
4. During the first recursive command, the named group ACL is present on `.striatum`, `.striatum/scratch`, S1's supervisor scratch directory, and the existing `0600` bearer/log files. U2 can win the race and copy the bearer or diagnostic log before the strip runs.
5. The strip then removes the group ACL and A16/final-state doctor can pass. The session-bound bearer has still been disclosed, and bearer disclosure is irreversible for the session lifetime.

This is not the original broad steady-state leak, but it is the same security class: a pool uid not leased to S1 can read S1's control-plane material. C2 asked for exclusion of `.striatum/`, not a grant-then-cleanup sequence.

## Strongest Rebuttal

The strongest rebuttal is that the revised final-state design is sound if implemented with the allowlist form in `HOLDER.md:356-358`, or if provisioning is only run before any `.striatum/scratch` control-plane file exists and no pool uid can race it. The final invariant is also useful: a post-provision `getfacl` audit should fail if `.striatum/` retains `g:striatum-lanes`.

That rebuttal is not enough for the spec as written. The holder explicitly permits the unsafe recursive-root-then-strip procedure and says the gate is the auditable end-state, not the procedure. C2 is about bearer secrecy; a transient grant is a material exposure because reading a bearer once is sufficient. A spec that allows the unsafe procedure can be implemented faithfully, pass the final-state A16 check, and still leak the exact file C2 was meant to protect.

## Required Revision

Make the safe form mandatory. The pool group grant must be an allowlist or an exclude-at-traversal implementation that never applies `g:striatum-lanes:rX` or its default ACL to `.striatum/`, `.git/`, provider token caches, or existing/future control-plane paths, even transiently. Do not bless raw `setfacl -R ... <repoRoot>` followed by a strip as a live-repo implementation.

Extend A16 from a final-state check to a transition check:

- `TestPoolACLProvisioningNeverTransientlyExposesScratch`: seed an existing S1 MCP bearer, PTY log, token cache, and foreign worktree; run the pool ACL provisioner while an unleased pool uid and a different leased uid repeatedly attempt `open(2)` and directory traversal; assert no read succeeds before, during, or after provisioning.
- Add a unit-level ACL planner guard that fails if any group grant operation targets `<repoRoot>` as a raw recursive root while `.striatum/` exists under it. The implementation should enumerate source/artifact entries or use an ACL traversal that prunes forbidden directories before applying a grant.
- Keep the final-state `getfacl` doctor and lane-isolation checks, but treat them as necessary and not sufficient: they prove cleanup, not non-exposure during the transition.

## Carry-Forward Check

I found no C2 regression in the revised final ACL target, and no regression in the v1-proven hard core HC-A1..A5, OQ1, OQ3, OQ5, OQ6, or the narrowing invariant. The per-lane uid model remains the right structural fix. The challenge is confined to ACL exactness: the revised spec must require `.striatum/` to be excluded from the group grant throughout provisioning, not merely absent from the final ACL snapshot.

## Bottom Line

C2 should not clear while the approved provisioning procedure can temporarily add group read to existing `0600` `.striatum/` control-plane files. The fix is small but security-critical: require the allowlist/pruned traversal path and test non-exposure across the whole provisioning transition.