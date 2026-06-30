# RFC 0165 v4 Falsifying Challenge: Latest Daemon Row Is Not Bound To The Stalled Launch
agent: falsifier
author: falsifier-reviewer-003

## Challenge

C1 is still not fully resolved. V4 closes the v3 lane-authored `expiresAt` upgrade, but its positive freshness predicate proves only that the current daemon-owned dependency slot for a lane user and destination is fresh. It does not prove that the stalled job, session, or supervisor is using that generation.

A newer Claude launch under the same distinct lane OS user can update the singleton `provider_auth_dependencies` row with a fresh projection while an older running session is still bound to an expired launch projection. Recovery can then read a perfectly daemon-owned, receipt-backed, future-dated row and incorrectly route the older session's provider-auth failure to generic MCP-discovery recovery.

That is a C1 failure without any lane-side forgery: the authority is daemon-owned, but it is not the daemon-owned authority for the credential generation the stalled process actually has.

## Claim Challenged

The v4 holder claims recovery treats daemon-owned state as the only positive freshness authority and fails closed for stale, missing, or inconsistent rows. The concrete predicate is `isPositivelyFresh`: a row exists for `(repository_id, provider=claude, kind=oauth, lane_user, destination_selector)`, is `ready`, is future-dated, points at a passed receipt, and matches the daemon-reobserved current operator source generation; if true, the stall falls through to normal generic recovery.

The missing condition is identity binding. The predicate must prove freshness for the stalled launch/session/supervisor generation, not merely for the latest row occupying the same lane-user/destination slot.

## Evidence

The v4 predicate is keyed by lane user and destination selector, not by the stalled job's launch receipt or supervisor/session generation (`docs/operator/artifacts/rfc-0165-design-v4/dialogue/holder/HOLDER.md:451`). It falls through to generic recovery when that current row is positive-fresh (`HOLDER.md:472`), and it sends stale or inconsistent rows to reseed debt (`HOLDER.md:475`).

The durable current-state table has one row per `repository_id, provider, kind, lane_user, destination_selector`, carrying one `source_generation_id`, one `destination_generation_id`, one `expires_at`, and one `last_receipt_id` (`HOLDER.md:507`). Receipts include `run_id`, `session_id`, and `lane_id` (`HOLDER.md:521`), but the positive predicate only requires the receipt to match the current row's generation ids (`HOLDER.md:461`); it does not require the receipt to match the owner session, supervisor pointer, active lease, or launch generation being recovered.

Current recovery already has the identity context needed for that binding. `recoverStuckJobs` scans `job_id`, `run_id`, current/latest lease owner, `session_id`, liveness fields, active lease, and supervisor pointer metadata before budget handling (`go/pkg/mutations/recovery_decision_tree.go:713`). It then reads the recovery budget (`go/pkg/mutations/recovery_decision_tree.go:1143`) and later records the generic recovery action (`go/pkg/mutations/recovery_decision_tree.go:1406`). V4 does not specify using that job/session/supervisor identity when selecting provider-auth evidence.

The required C1 tests cover a lane-forged future expiry, missing/stale daemon rows, inconsistent daemon rows, downgrade-only lane sampling, and a genuinely fresh row falling through to generic (`HOLDER.md:610`). They do not cover a genuinely fresh row for a newer launch proving an older launch fresh.

## Concrete Race

1. Session A launches a distinct-UID Claude lane at T0. The daemon projects access-token-only generation G1, records receipt A, and upserts the dependency row for `(repo, claude, oauth, striatum-lane, home_default)` with `expires_at=T0+35m`, `destination_generation_id=G1`, `last_receipt_id=receipt_A`.
2. Session A runs long enough that its launch projection expires before its next provider action. Unless the design proves the running process has adopted a later projection, A is still effectively on G1.
3. At T0+20m, session B launches under the same lane user and destination selector. The daemon projects G2 and upserts the same dependency row to `expires_at=T0+55m`, `destination_generation_id=G2`, `last_receipt_id=receipt_B`.
4. At T0+45m, session A fails with `agent_mcp_discovery_stall` because its effective credential generation G1 is expired.
5. Recovery classifies job A. The v4 predicate reads the latest daemon row, sees G2 as `ready`, future-dated, receipt-backed, and current-source matched, and returns positive-fresh.
6. The holder-specified branch treats A's stall as non-provider-auth and falls through to generic recovery, allowing `requeue_count` or `transfer_count` to increment for an auth failure.

This is the same kind of budget-burn leak C1 is meant to close, reached through a daemon-owned latest-row overwrite rather than through a lane-authored file.

## Strongest Rebuttal

The strongest rebuttal is delivery-mode dependent. If B2 broker mode is used for every provider action, the running session may fetch the current access token on demand, making the latest row the right authority. If B1 file mode is used but the Claude process reliably re-reads the shared projection file before every provider action, session A may adopt G2 before it stalls.

V4 does not prove either condition. B1 remains the primary delivery mechanism, and `TestClaudeCLIAcceptsAccessTokenOnlyCredential` only decides whether Claude accepts an access-token-only file; it does not prove an already-running Claude process re-reads the file after a later projection. The C1 test matrix proves stale-row and lane-forged-expiry behavior, but not launch-generation binding across overlapping sessions.

## C2 Check

I did not find a material same-user raw-refresh-token path in the v4 policy. The holder deletes v3's same-user verify-only no-op, refuses `RunAsUser == ""` and resolved daemon-uid aliases, and places the typed `provider_credential_same_user_unsupported` refusal before scratch, token minting, supervisor rows, helper/tmux, or process launch.

One build-ordering caveat remains: the v4 source-anchor section inserts the Claude same-user/projection gates after the existing Codex-only `runSuperviseProviderAuthGate` call. Current source returns an unsupported-provider error for non-Codex adapters when `provider_auth_gate=required` before that insertion point (`go/pkg/mutations/supervision_provider_auth.go:45`; `go/pkg/mutations/supervision_control.go:101`). That still refuses before launch, so it does not reopen raw-token custody, but the promised typed same-user remediation should be tested under `provider_auth_gate=auto`, `off`, and `required`, or the same-user precondition should run before the Codex-only provider-auth gate.

## Required Revision

To clear C1, bind provider-auth freshness evidence to the stalled launch generation:

- Persist the projection receipt id, delivery mode, destination generation id, and expiry on the session, supervisor, or launch record that recovery later classifies.
- In `recoverStuckJobs`, evaluate freshness for the stalled owner session/supervisor generation. A newer lane-user dependency row may prove a fresh projection is available for a relaunch, but it must not prove the older stalled process was fresh.
- Make near-expiry debt per running session/generation. A later projection for another session must not clear or overwrite debt for an older running session unless the design proves the older session adopted the newer token.
- If B1 cannot prove already-running processes re-read the newest daemon projection before each provider action, use B2 for runtime freshness or restart/requeue sessions whose bound generation crosses the near-expiry lead.
- Add `TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection`: launch session A with G1 expiring in 35m, launch session B under the same lane user with fresh G2 before A's next provider action, advance A to 45m, trigger `agent_mcp_discovery_stall`, and assert recovery sets provider-auth debt for A without incrementing generic counters.
- Add a decay-signal test where an older running session crosses near-expiry, a newer projection updates the lane-user row, and the older session's debt remains visible until that session is restarted or proven to use the newer generation.

## Bottom Line

V4 resolves the specific v3 forged-lane-file C1 repro, and its same-user C2 policy is structurally sound. The remaining C1 gap is that the daemon-owned positive authority is not bound to the stalled session's actual credential generation. A latest dependency row can be genuinely daemon-owned and still be the wrong proof.
