# RFC 0165 v4 Falsifying Challenge: Latest Dependency Row Can Prove the Wrong Session Fresh
author: falsifier-reviewer-001

## Challenge

C1 is not genuinely resolved. The v4 holder closes the v3 lane-authored `expiresAt` upgrade: a lane-owned file can no longer turn a stale daemon row into fresh. But the revised positive-freshness predicate still reads the current `provider_auth_dependencies` slot keyed only by `(repository_id, provider, kind, lane_user, destination_selector)`. That slot is daemon-owned, but it is latest-slot state, not the credential generation bound to the stalled session/supervisor/job.

A later Claude launch under the same lane OS user can upsert a fresh generation G2 into the singleton dependency row while an older running session is still using an expired launch generation G1. Recovery for the older stalled job then sees a receipt-backed, future-dated, daemon-owned row and follows the holder's own `isPositivelyFresh == true` branch into generic recovery. That burns `requeue_count` / `transfer_count` for a provider-auth cause without any lane-side forgery.

This is a C1 failure because daemon-owned state is only useful as positive freshness authority if it is the daemon-owned state for the credential generation the stalled process actually uses.

## Claim Challenged

The v4 holder claims C1 is resolved because recovery classifies Claude `agent_mcp_discovery_stall` before generic recovery and treats the stall as non-provider-auth only when `isPositivelyFresh` proves a daemon row exists, is `ready`, has `expires_at > now + MinFreshness`, points at a passed receipt, and matches the daemon-re-observed current operator-source generation.

The challenged claim is the identity binding of that proof. The holder proves the latest lane-user/destination projection is fresh; it does not prove the stalled job's launch/session/supervisor projection is fresh.

## Evidence

In the v4 holder, the positive predicate requires a row for `(repository_id, provider=claude, kind=oauth, lane_user, destination_selector)` and falls through to normal generic recovery when it returns true (`HOLDER.md:454-474`). The current-state table is keyed by the same lane-user/destination shape and stores only one `source_generation_id`, `destination_generation_id`, `expires_at`, and `last_receipt_id` for that slot (`HOLDER.md:507-517`).

Receipts do include `run_id`, `session_id`, and `lane_id` (`HOLDER.md:521-525`), but the positive predicate does not require `last_receipt_id` to belong to the stalled job's owner session, supervisor pointer, active lease, or launch generation. It only requires the receipt to match the current row's generation ids (`HOLDER.md:461-466`).

Current recovery has the context needed to bind this correctly: `recoverStuckJobs` scans `job_id`, `run_id`, latest lease owner, `session_id`, liveness fields, active lease, and supervisor pointer metadata (`recovery_decision_tree.go:713-736`) before it reads the recovery budget and eventually calls `recordRecoveryAction` (`recovery_decision_tree.go:1143-1148`, `1406-1420`). The v4 spec does not use that job/session/supervisor identity when selecting provider-auth evidence.

The named tests also miss the overlap case. They test lane-forged future expiry, missing/stale daemon row, inconsistent daemon row, downgrade-only sample, and a fully consistent future row falling through to generic (`HOLDER.md:610-632`). They do not test that a fully consistent future row for a newer session must not prove an older stalled session fresh.

## Concrete Race

1. Session A launches a distinct-UID Claude lane at T0. The projector writes access-token-only generation G1, records receipt A, and upserts the singleton dependency row with `expires_at=T0+35m`, `destination_generation_id=G1`, `last_receipt_id=receipt_A`.
2. Session A does long local work and does not successfully use Claude until after T0+45m. Its effective provider credential is still G1 unless the design proves the running process has adopted a newer generation.
3. At T0+20m, session B launches under the same `lane_user` and `destination_selector`. The daemon projects generation G2 and upserts the same dependency row to `expires_at=T0+55m`, `destination_generation_id=G2`, `last_receipt_id=receipt_B`.
4. At T0+45m, session A's first Claude/MCP action fails because G1 is expired, and the session is classified as `agent_mcp_discovery_stall`.
5. Recovery handles job A. The v4 predicate sees the current singleton row for that lane user/destination: `state=ready`, future `expires_at`, matching receipt B, and current operator-source generation. `isPositivelyFresh == true`.
6. The holder-specified branch says this stall is not a provider-auth cause and should fall through to generic recovery. `recordRecoveryAction` can then increment generic budget for session A's expired provider credential.

This is the same safety failure C1 was meant to remove, reached through daemon-owned latest-row overwrite rather than a lane-authored file.

## Strongest Rebuttal

The best rebuttal is delivery-mode dependent. If B2 broker mode is used for every running Claude action, the broker can return the current access token on demand and can bind per-fetch expiry in daemon state. If B1 file mode is used but Claude reliably re-reads the shared projection file before every provider action, session A might adopt G2 before it stalls, so the current row would be a fair authority.

The v4 text does not prove either condition. B1 remains the primary delivery mechanism, and `TestClaudeCLIAcceptsAccessTokenOnlyCredential` only decides whether Claude accepts an access-token-only file; it does not prove a launched Claude process re-reads that file after a later projection. The C1 tests prove stale-row and lane-forged-expiry behavior, but not session/generation binding across overlapping launches.

## C2 Check

The intended same-user policy is materially stronger than v3: same-user Claude OAuth is unsupported, the condition includes `RunAsUser == ""` and resolved uid equality, and the refusal is specified before scratch, token mint, supervisor rows, helper/tmux, or process launch. I did not find a raw-refresh-token same-user launch path in that intended policy.

There is a secondary ordering caveat for the build plan: the v4 source-anchor section places the new Claude same-user/projection gates after `runSuperviseProviderAuthGate` and before scratch. Current `runSuperviseProviderAuthGate` returns a `lane_provider_auth_failed` unsupported-provider error for non-Codex adapters when `provider_auth_gate=required` (`supervision_provider_auth.go:45-49`), and `HandleSuperviseStart` calls it before the proposed insertion point (`supervision_control.go:97-112`). If the build literally inserts after that call, a same-user Claude lane with `provider_auth_gate=required` will fail before process launch, but not with the promised `provider_credential_same_user_unsupported` remediation. That does not reopen raw-token custody by itself, but it should be covered by `TestSameUserClaudeLaneRefusedBeforeSideEffects` with `provider_auth_gate=auto`, `off`, and `required`, or the same-user precondition should move before the Codex-only provider-auth gate.

## Required Revision

To clear C1, the build spec needs an explicit generation/session binding, not just a daemon-owned latest row:

- Store the projection receipt id, delivery mode, destination generation id, and `expires_at` on the session/supervisor/job launch record that recovery will later classify.
- In `recoverStuckJobs`, evaluate provider-auth freshness for the stalled job's owner session/supervisor generation. A newer lane-user dependency row may prove a fresh projection is available for a relaunch; it must not prove the older stalled process was fresh.
- Make near-expiry debt per running session/generation. A later projection for another session must not clear or overwrite debt for an older running session unless the design proves that session has adopted the newer token.
- If B1 cannot prove launched processes re-read the newest daemon projection before every provider action, use B2 for runtime freshness or restart/requeue sessions when their bound generation crosses the near-expiry lead.
- Add `TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection`: launch session A with G1 expiring in 35m, launch session B under the same lane user with fresh G2 before A's first provider action, advance A to 45m, trigger `agent_mcp_discovery_stall`, and assert recovery sets provider-auth debt for A without incrementing generic counters.
- Add a decay-signal test where an older running session crosses near-expiry, a newer projection updates the lane-user row, and the older session's debt remains visible until that session is restarted or proven to use the newer generation.

## Bottom Line

V4 resolves the specific v3 forged-lane-file C1 repro, and C2's same-user custody policy is directionally sound. The remaining C1 gap is that the daemon-owned positive authority is not bound to the stalled session's actual credential generation. A latest dependency row can be genuinely daemon-owned and still be the wrong proof.
