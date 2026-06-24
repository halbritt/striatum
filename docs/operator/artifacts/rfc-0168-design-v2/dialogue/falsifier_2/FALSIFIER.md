# FALSIFIER - RFC 0168 P0 C2 ACL transition challenge

author: falsifier-reviewer-002

## Result

**C2 still has one gate-blocking exactness gap.** I credit the v2 holder for fixing the intended final ACL state: no `g:striatum-lanes` access or default entry under `.striatum/` or `.git`, `.striatum/` traverse keyed only to the currently leased uid, per-supervisor scratch ACLs scoped to that leased uid, and A16 checking both an unleased pool uid and a different leased uid against MCP bearer, PTY log, token cache, and foreign worktree reads (`HOLDER.md:360-424`). If implemented as a true allowlist, or only before any `.striatum/scratch` control-plane files exist, that end-state closes the original v1 C2 leak.

The remaining material issue is that the same spec still permits a recursive-root grant followed by a carve-out: `setfacl -R -m g:striatum-lanes:rX -m d:g:striatum-lanes:rX <repoRoot>`, then `setfacl -R -x g:striatum-lanes -k <repoRoot>/.striatum` (`HOLDER.md:350-355`). That first command necessarily touches `.striatum/` and existing `0600` files before the strip runs. The later strip restores the final ACL state, but it cannot revoke a bearer or PTY log already read during the exposure window. A16 checks the after-state, not the transition.

## Precise Claim Attacked

The C2 seed required an ACL grant that **excludes `.striatum/`**, preserves the `u:<lane>:--x` traverse-only carve-out, grants exactly enough access to the leased uid, and plants another lane's `0600` control-plane file to prove an unleased pool uid cannot read it (`SEED.md:83-103`). The v2 holder's load-bearing invariant is also phrased as no `g:striatum-lanes` access-or-default entry under `.striatum/` before or after provisioning (`HOLDER.md:360-362`), and its rebuttal says the `setfacl -R ...:rX` problem cannot arise because no group ACL ever touches `.striatum/` (`HOLDER.md:394-402`).

Those claims are contradicted by the permitted procedure. A recursive grant over `<repoRoot>` does touch `.striatum/`; the holder even acknowledges that the first `-R` adds group `r` to existing files before the strip removes it (`HOLDER.md:350-355`). Current source confirms the exposed material is real: the MCP config bearer is created under `.striatum/scratch` with mode `0600` (`mcpconfig.go:550-570`), the PTY log path is `.striatum/scratch/<supervisor_id>/pty.log` and is opened `0600` (`loop.go:139-145`, `loop.go:298-300`), and the existing scratch ACL code is deliberately narrow because it must not broaden read access to private operator state (`scratch_acl.go:31-48`).

I also sanity-checked POSIX ACL behavior on this host with the current group name because `striatum-lanes` is not present in this lane environment: `setfacl -R -m g:<group>:rX` on an existing `0600` file produced a named-group `r--` ACL, and the later `setfacl -R -x g:<group> -k .striatum` removed it. That is exactly the dangerous transient the v2 procedure still allows.

## Concrete Failing Case

1. An adopted repo already has a live S1 lane with `.striatum/scratch/<S1>/lane-mcp-config-*.json` and `.striatum/scratch/<S1>/pty.log`, both `0600` control-plane files.
2. A different pool uid `U2` is live in another lane, or a residual/unleased pool uid process exists. It loops on `open(2)` for S1's bearer path. This is the C2 attacker class: a pool uid not leased to S1 must never read S1's control-plane material.
3. The operator or build uses the v2-permitted provisioning path. During the first recursive `setfacl -R -m g:striatum-lanes:rX ... <repoRoot>`, the ACL is added to `.striatum/`, `.striatum/scratch/`, S1's scratch directory, and S1's existing `0600` bearer/log files.
4. Before the mandatory carve-out command reaches and strips `.striatum/`, `U2` reads the bearer or PTY log. The later strip makes the final ACL audit green, but the session-bound bearer has already been copied and can be replayed against the daemon as S1.
5. A16 can still pass, because it seeds files, runs provisioning, and checks unreadability after provisioning. It does not run an adversarial reader during the recursive grant, and it does not forbid the implementation from ever applying the group ACL to `.striatum/` in the first place.

This is the same replay surface C2 was meant to remove. The difference from v1 is only duration, not class: a transient read of a bearer is enough.

## Strongest Rebuttal

The strongest rebuttal is that the holder permits an equivalent allowlist form, and a careful implementation could choose that path or run provisioning only on a quiesced repo before any `.striatum/scratch` bearer exists. The final invariant is also sound if the implementation truly never includes `.striatum/` in the grant.

But the spec does not require that safe path. It explicitly says the gate is the auditable end-state, not the procedure (`HOLDER.md:356-362`), and blesses a procedure that first grants then strips. C2 is about control-plane secrecy, and local race windows are security-relevant because bearer exfiltration is irreversible. A build that implements the documented recursive-root-then-carve-out path can pass the final-state doctor/A16 checks while still leaking S1's bearer during provisioning.

## Required Revision

Make the safe form mandatory: the pool group grant must be an allowlist or an exclude-at-traversal implementation that never applies `g:striatum-lanes:rX` to `.striatum/`, `.git/`, provider token caches, or existing/future control-plane paths, even transiently. Do not bless `setfacl -R ... <repoRoot>` followed by a strip as an acceptable live-repo implementation.

Extend A16 into a transition test, not only a final-state test:

- `TestPoolACLProvisioningNeverTransientlyExposesScratch`: seed an existing S1 MCP bearer, PTY log, token cache, and foreign worktree; run the pool ACL provisioner while an unleased pool uid and a different leased uid repeatedly attempt `open(2)` and directory traversal; assert no read succeeds before, during, or after provisioning.
- Add an implementation-level guard or unit test around the ACL planner that fails if any `g:striatum-lanes` operation targets `<repoRoot>` as a raw recursive root when `.striatum/` exists under it. Final-state `getfacl` audits stay useful, but they are not sufficient for C2.

## Carry-Forward Check

I found no regression in the v1-proven hard core HC-A1..A5, OQ1/OQ3/OQ5/OQ6, or the narrowing invariant. The structural per-lane uid model remains right, and the revised C2 final state is the right shape. The challenge is confined to ACL provisioning exactness: the spec must forbid a transient `.striatum/` group-read window, not merely clean it up afterward.

## Bottom Line

C2 does not genuinely clear while the approved provisioning procedure can add group read to existing `0600` control-plane files before removing it. The fix is small but load-bearing: require the allowlist/exclusion implementation and test non-exposure across the provisioning transition.