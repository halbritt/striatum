# RFC 0165 Holder Proposal (v5): Launch-Bound Claude Credential Freshness With Projection-Off Fail-Closed
author: holder-author-001

This is the **v5 surgical revision** of the RFC 0165 implementation SPEC
(Claude provider credential freshness + spawn-time hydration; GH #583). It is a
continuation of the v4 holder, not a replacement for the accepted spine. The v4
adjudicator found that the distinct-UID access-token-only projection, same-user
fail-closed custody argument, downgrade-only lane sampling, redacted receipts,
and RFC 0096 control-plane separation were intact. v5 keeps those pieces and
changes only the three carry-forward points the v4 ledger bound:

- **C1** - recovery positive freshness is bound to the stalled job's own launch
  credential generation, not to the latest singleton lane-user row.
- **C3** - `provider_credential_projection=off` is not a live Claude OAuth
  launch bypass; it fails closed before any side effect.
- **C2 ordering** - same-user Claude OAuth refusal runs before the generic
  provider-auth gate, so `auto`, `off`, and `required` all return the typed
  `provider_credential_same_user_unsupported` floor when no higher-priority
  credential-domain violation exists.

This revision also addresses the cycle-2 adjudicator finding: for self-driving
Claude launches, missing, unknown, or unmodeled credential kind is treated as
`OAUTH_COPIED` / Claude OAuth for RFC 0165 admission. A projection-disabled
launch can use a non-OAuth diagnostic exception only after positive pre-launch
proof that no Claude OAuth resolver surface is in play.

The provider-agnostic RFC 0169 spine remains separate. This SPEC names the seam
where the Claude assurance class can plug into RFC 0169 later, but it does not
defer any v5 constraint to RFC 0169.

---

## Binding Constraints Discharged

### C1 - Positive recovery freshness is launch-bound

**v4 defect.** v4 made daemon-owned state the only positive freshness authority,
but it read the latest `provider_auth_dependencies` row keyed by
`(repository_id, provider, kind, lane_user, destination_selector)`. That row is
a readiness cache for the lane user, not a proof for a particular stalled
session. A newer session B under the same lane user can overwrite the row with a
fresh generation G2 while older session A is still running on expired generation
G1. Recovery for A then reads a genuinely daemon-owned, receipt-backed G2 row
and incorrectly treats A as fresh.

**v5 fix.** Each successful Claude launch writes a **launch credential binding**
owned by the daemon and tied to the launched job/session/supervisor generation.
Recovery uses that binding as the only positive proof for the stalled owner. The
latest `provider_auth_dependencies` row remains useful for admission and for
answering "can a fresh relaunch be attempted now"; it is never sufficient to
prove an older stalled process fresh.

The launch binding is written in the same authority path as the projection
receipt and supervisor start transaction. It carries only redacted fields:

```text
striatumd.provider_credential_launch_bindings
  binding_id, repository_id, run_id, job_id, session_id, supervisor_id
  lane_id, lane_user, provider, kind, destination_selector
  delivery_mode                    -- b1_file | b2_broker
  adoption_model                   -- launch_static | broker_per_fetch | proven_runtime_adoption
  receipt_id, source_generation_id, destination_generation_id
  destination_expires_at, min_freshness_seconds
  state                            -- active | superseded | reseed_required | unverifiable | disabled
  bound_at, updated_at
```

The existing latest-row table remains:

```text
striatumd.provider_auth_dependencies
  repository_id, provider, kind, lane_user, destination_selector
  state, source_generation_id, destination_generation_id
  expires_at, min_freshness_seconds, last_receipt_id, updated_at
```

The dependency row may point to G2 for future launches while A's launch binding
still points to G1. Recovery must respect that split.

**Recovery predicate.** In `recoverStuckJobs`, for a Claude self-driving lane
whose stall class is `agent_mcp_discovery_stall`, run the provider-auth branch
after `readJobRecoveryBudget` and before any budget exhaustion decision,
`markRecoveryEscalation`, `requeueJobSameAttempt`, `closeStalledOwningSession`,
or `recordRecoveryAction`. Load the launch binding by the stalled job's owner
identity: `repository_id`, `run_id`, `job_id`, `session_id`, and
`supervisor_id` when present. A binding is positively fresh only if **all** of
these are true:

1. the binding exists and is tied to the stalled owner, not merely the same lane
   user;
2. `provider=claude`, `kind=oauth`, and the binding's lane/session/supervisor
   match the recovering row's owner context;
3. `state == active`;
4. `destination_expires_at > now + MinFreshness`;
5. `receipt_id` resolves to a custody receipt with `verifier_result == passed`;
6. the receipt's `source_generation_id` and `destination_generation_id` exactly
   match the binding;
7. the daemon-reobserved operator source generation still matches the binding's
   `source_generation_id`, unless the binding's delivery mode is B2 and a
   broker-per-fetch receipt for the same binding proves a newer adopted
   generation for this same supervisor/session;
8. any lane-file sample is no fresher than the binding; a lane sample may only
   downgrade the result.

If any clause fails, recovery records provider-auth debt for that binding and
does **not** increment `requeue_count`, `transfer_count`, or
`recovery_exhausted` for the credential cause. The debt key includes
`binding_id` and `destination_generation_id`, so a newer projection for the same
lane user cannot clear or overwrite debt for an older running session unless
the daemon records adoption for that same binding.

**B1/B2 runtime rule.** B1 file projection is launch-static for recovery unless
the build proves an already-running Claude process re-reads and adopts the
newest projection before each provider action. v5 does not rely on that
unproven behavior. A B1 session whose bound generation crosses the near-expiry
lead becomes per-session reseed/restart debt. B2 broker mode may update the same
launch binding on each authenticated `SO_PEERCRED` fetch because the broker
knows the peer uid, supervisor/session binding, access-token expiry, and receipt
for that specific fetch.

**Falsifiable assertions.**

| Assertion | Refuting observation | Required test |
|---|---|---|
| Recovery positive freshness is bound to the stalled launch binding, not the latest lane-user row. | An older stalled session A on expired G1 reads a newer G2 dependency row and falls through to generic recovery. | `TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection` |
| Provider-auth expiry debt is per session/generation. | A newer G2 projection clears or hides A's G1 near-expiry debt without proof A adopted G2. | `TestRecoveryPerSessionDecayDebtSurvivesNewerProjection` |
| A missing, stale, inconsistent, or owner-mismatched binding fails closed without generic budget burn. | Recovery increments `requeue_count`/`transfer_count` or escalates `recovery_exhausted` for a non-positively-fresh binding. | `TestRecoveryLaunchBindingMissingOrMismatchedFailsClosed` |
| Lane-readable credential samples are downgrade-only. | A later lane-file `expiresAt` upgrades a stale binding to fresh. | `TestRecoveryLaneSampleIsDowngradeOnly` |
| A genuinely fresh launch-bound binding may fall through to generic recovery for a non-auth stall. | A valid active binding with matching receipt and future expiry is misclassified as provider-auth debt. | `TestRecoveryPositivelyFreshLaunchBindingFallsThroughToGeneric` |

### C3 - Projection disabled is not a Claude OAuth bypass

**v4 defect.** v4 kept `provider_credential_projection=off` as a distinct-UID
Claude launch path. If a lane home or `CLAUDE_CONFIG_DIR` still contains the
incident-shape whole credential with a `refreshToken`, projection-off can skip
the B1/B2 access-token-only placement and start a Claude process that can read
the old raw refresh token.

**v5 fix.** Before any self-driving Claude launch side effect, Striatum
classifies the RFC 0165 credential assurance shape. For `adapter == claude` and
`agent_loop_mode == self_driving`, a missing, unknown, or unmodeled credential
kind is treated as `OAUTH_COPIED` / Claude OAuth for admission. Absence of an
explicit kind field, resolver roster entry, or diagnostic label is not proof
that Claude OAuth discovery is impossible.

For Claude OAuth or unproven self-driving lanes,
`provider_credential_projection=off` fails closed as a typed launch
precondition. A non-OAuth diagnostic exception may launch only after positive
pre-launch proof that the launch cannot use `$HOME/.claude`,
`CLAUDE_CONFIG_DIR`, `CLAUDE_SECURESTORAGE_CONFIG_DIR`, helper settings,
inherited credential paths, or any other Claude OAuth resolver surface. The
refusal happens after the same-user precondition and before the generic
provider-auth gate, scratch creation, FIFO/ACL work, session-token minting,
supervisor rows, projection receipt creation, helper/tmux setup, or provider
process launch. The typed error is:

```text
provider_credential_projection_disabled_unsupported
```

The remediation is private-safe:

```text
Claude OAuth self-driving lanes require Striatum's access-token-only projection.
Remove provider_credential_projection=off or use a positively proven
non-Claude/non-OAuth diagnostic lane. No lane process was started.
```

Because the launch is refused, the lane never gets an opportunity to resolve
`$CLAUDE_SECURESTORAGE_CONFIG_DIR`, `$CLAUDE_CONFIG_DIR`, `$HOME/.claude`, a
credential-bearing env entry, a helper setting, or an inherited credential path
as a raw-token source. The existing all-surfaces `refresh_token_absent` scan
still applies to the normal distinct-UID projection path. The projection-off
tests seed the raw-token surfaces anyway, omit any explicit credential kind for
the unknown-kind cases, and assert that the typed refusal wins before a process
can read them.

**Falsifiable assertions.**

| Assertion | Refuting observation | Required test |
|---|---|---|
| `provider_credential_projection=off` cannot launch a Claude OAuth or unproven self-driving Claude lane. | A Claude lane with projection disabled and missing, unknown, or unmodeled credential kind reaches scratch, FIFO/ACL, token minting, supervisor rows, projection receipt creation, helper/tmux, or a provider process. | `TestProjectionOffUnknownKindFailsClosedBeforeSideEffects`, `TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface` |
| `CLAUDE_CONFIG_DIR` and `CLAUDE_SECURESTORAGE_CONFIG_DIR` do not become projection-off raw-token routes. | Projection disabled plus a lane-readable config dir containing a whole credential starts a Claude process or reports success because no explicit kind was present. | `TestProjectionOffStillValidatesClaudeConfigDir`, `TestProjectionOffSecureStorageConfigDirUnknownKindFailsClosed` |
| A projection-disabled non-OAuth diagnostic launch needs positive proof. | A Claude self-driving launch falls through to diagnostic non-OAuth because the kind field or resolver roster entry is absent. | `TestProjectionOffNonOAuthDiagnosticRequiresPositiveProof` |
| The normal distinct-UID access-token-only projection still launches. | The projection-off closure breaks a distinct-UID Claude lane whose projection is enabled and whose source is fresh. | `TestDistinctUIDAccessTokenProjectionStillLaunches` |
| `provider_auth_gate=off` does not imply or bypass `provider_credential_projection=off`. | `provider_auth_gate=off` skips projection or same-user/projection-off preconditions. | `TestProviderAuthGateOffDoesNotBypassProjection`, `TestProviderAuthGateOffDoesNotBypassSameUserRefusal` |

### C2 ordering - same-user precedes provider-auth, credential-domain outranks it

**v4 caveat.** The v4 policy was sound on custody, but the placement after
`runSuperviseProviderAuthGate` meant `provider_auth_gate=required` could return
a generic unsupported-provider error before the intended same-user remediation.

**v5 fix.** `HandleSuperviseStart` runs `enforceLaneCredentialDomain` after
`loadSupervisionStartConfig` and before the Claude same-user precondition. That
is intentional: a credential selector resolving inside the target repository, or
an uncovered provider credential selector inside the repository, is a
higher-priority fail-closed precondition because it protects the repository
boundary. A same-user Claude OAuth launch with repo-inside `CLAUDE_CONFIG_DIR` or
`CLAUDE_SECURESTORAGE_CONFIG_DIR` may therefore return
`lane_credential_cache_inside_repo` or
`lane_uncovered_credential_selector_inside_repo` before
`provider_credential_same_user_unsupported`; all of those errors occur before
scratch, FIFO/ACL work, session-token minting, supervisor rows, projection files,
custody receipts, helper/tmux setup, or provider process launch.

When no credential-domain violation is present, the same-user precondition runs
before `runSuperviseProviderAuthGate`. This ordering is independent of
`provider_auth_gate`. If the lane is Claude OAuth and the resolved lane identity
is the daemon/operator identity, the launch returns
`provider_credential_same_user_unsupported` in `auto`, `off`, and `required`.

The same-user condition is:

```text
adapter == claude
AND kind == oauth
AND agent_loop_mode == self_driving
AND (trim(config.RunAsUser) == "" OR lookup(config.RunAsUser).uid == daemon_uid)
```

The empty `RunAsUser` case covers the current collapse where an unset or
same-as-daemon `STRIATUM_LANE_OS_USER` executes the command directly as the
operator uid. The uid lookup backstop covers aliases and shared-uid usernames.
Same-user is refused before projection-off when no credential-domain violation is
present, so the operator gets the strongest custody remediation first when both
conditions are present.

**Falsifiable assertions.**

| Assertion | Refuting observation | Required test |
|---|---|---|
| Same-user Claude OAuth refusal precedes the generic provider-auth gate in `auto`, `off`, and `required`. | Any same-user Claude lane returns `lane_provider_auth_failed`, `lane_provider_preflight_unsupported`, or another generic provider-auth error before the typed same-user error. | `TestSameUserClaudeLaneRefusedBeforeSideEffects` parameterized over `provider_auth_gate=auto,off,required` |
| Same-user refusal has no side effects. | Scratch, FIFO/ACL, session token, supervisor row, helper/tmux state, projection file, custody receipt, or Claude process exists after refusal. | `TestSameUserClaudeLaneRefusedBeforeSideEffects` |
| Credential-domain violations intentionally outrank same-user remediation. | A same-user Claude lane with repo-inside `CLAUDE_CONFIG_DIR` or `CLAUDE_SECURESTORAGE_CONFIG_DIR` returns `provider_credential_same_user_unsupported` or performs a side effect before the declared credential-domain refusal. | `TestSameUserCredentialDomainViolationPrecedesSameUserRefusal` |
| The uid backstop catches aliases. | A username alias resolving to the daemon uid launches a Claude OAuth lane. | `TestSameUserRefusalByResolvedUid` |

---

## Carry-Forwards Preserved

- **Distinct-UID access-token-only projection** remains the nominal Claude OAuth
  path. B1 writes a lane-owned `0600` file containing only access-token material
  and expiry; B2 returns only an access token over a daemon-owned `AF_UNIX`
  broker after `SO_PEERCRED` uid verification.
- **Same-user Claude OAuth self-driving lanes remain unsupported** because no
  in-uid boundary can hide the operator's credential file from a process running
  as that uid.
- **Refresh authority stays outside the lane.** The daemon does not add an
  Anthropic OAuth client and does not become a refresh-token writer. Operator
  source refresh remains operator-side.
- **Lane-side samples are downgrade-only.** They may make a classification
  worse; they may never prove positive freshness.
- **Receipts and events are redacted.** Durable state may contain ids, enums,
  expiry timestamps, HMAC generation ids, owner/mode booleans, and
  `refresh_token_absent_ok`; it must not contain OAuth bytes, access tokens,
  refresh tokens, id tokens, raw credential JSON, full private paths, provider
  stdout/stderr, transcripts, DSNs, or Striatum control-plane tokens.
- **RFC 0096 separation remains intact.** Striatum session-bound capability
  tokens are not used as provider credentials, and provider OAuth handling never
  vends daemon/admin token material to a lane.
- **RFC 0169 remains separate.** This v5 SPEC supplies the Claude
  `OAUTH_COPIED` assurance-class closure that a later provider-agnostic registry
  can call; it does not require the RFC 0169 refactor to close #583.

Each carry-forward is test-backed:

| Assertion | Refuting observation | Test |
|---|---|---|
| Distinct-UID lanes never receive a refresh token. | Any lane-readable file/env/helper/socket surface contains `refreshToken` or the lane rotates the operator credential family. | `TestLaneNeverReceivesRefreshTokenAllSurfaces`, `TestConcurrentLanesNoRefreshTokenDesync`, `TestSubsequentLaneAfterOperatorRefresh` |
| Provider OAuth stays separate from Striatum control-plane auth. | Projector, broker, receipt, event, doctor, or dashboard payload contains `STRIATUM_MCP_TOKEN`, runtime `client-token`, admin token, DSN, or a capability token. | `TestTrustBoundaryNoControlPlaneTokenToLane` |
| Redaction covers every durable surface. | Serialized receipt/event/error/doctor/dashboard/metric payload includes fixture OAuth bytes, token substrings, raw hashes, provider output, or private-path substrings. | `TestProjectionReceiptRedaction` |
| Resolution is not a privilege bridge. | Workflow `CLAUDE_CONFIG_DIR` causes the daemon to write OAuth material to an arbitrary path. | `TestProjectionResolverRejectsWorkflowPath` |
| The daemon remains outside OAuth refresh. | `laneproviderauth` performs an Anthropic OAuth token exchange or writes the operator source refresh token. | `TestProjectorDoesNotRefreshProviderOAuth` |

---

## Current Source Anchors

These anchors are verified against the v5 run branch.

- `go/pkg/mutations/supervision_control.go::HandleSuperviseStart` currently
  orders `loadSupervisionStartConfig` at line 97, `enforceLaneCredentialDomain`
  at line 101, `runSuperviseProviderAuthGate` at line 104, scratch creation at
  line 118, session-token minting at line 159, supervisor row insertion at line
  166, and process launch at line 194. v5 inserts the Claude same-user and
  projection-off preconditions before line 104, and the enabled distinct-UID
  projection gate before line 118.
- `go/pkg/mutations/supervision_provider_auth.go::runSuperviseProviderAuthGate`
  supports Codex only in the current gate; with `provider_auth_gate=required`,
  unsupported providers return a generic preflight error before later
  Claude-specific checks. v5 fixes this by moving the same-user/projection-off
  Claude checks in front of that generic gate.
- `go/pkg/mutations/supervision_env.go::configuredLaneRunAsUser` collapses an
  unset or same-as-daemon lane user to `RunAsUser == ""`; `supervisedLaneCommandContext`
  then executes directly with no `sudo -u` split when `RunAsUser == ""`. That is
  the same-user custody surface the precondition must refuse.
- `go/pkg/mutations/supervision_lane_config.go::laneCommandEnv` permits
  provider-specific env keys while blocking only `PATH` and `STRIATUM_*`, so
  `CLAUDE_CONFIG_DIR` / `CLAUDE_SECURESTORAGE_CONFIG_DIR` must be considered
  lane-readable credential surfaces.
- `go/pkg/laneproviderauth/resolver.go::ResolveCredentialCandidates` resolves
  Claude credentials from `CLAUDE_SECURESTORAGE_CONFIG_DIR`, then
  `CLAUDE_CONFIG_DIR`, then `$HOME/.claude/.credentials.json`.
- `go/pkg/laneproviderauth/sampler.go::LaneFileReader` reads as the lane user,
  and `SampleLaneCredential` parses only expiry. This remains downgrade-only.
- `go/pkg/mutations/recovery_decision_tree.go::recoverStuckJobs` scans the
  owning session, active lease, and supervisor pointer for unfinished jobs; it
  reads recovery budget before action and calls `recordRecoveryAction` after
  requeue/transfer work. v5's provider-auth branch must run before any of that
  path can consume generic budget for a Claude credential cause.

---

## Implementation Spec

### 1. Launch precondition order

`HandleSuperviseStart` becomes:

```text
loadSupervisionStartConfig
enforceLaneCredentialDomain
runSuperviseClaudeSameUserPrecondition        -- new, before generic gate
runSuperviseClaudeProjectionDisabledGate      -- new, before generic gate
runSuperviseProviderAuthGate                  -- existing Codex-oriented gate
runSuperviseClaudeCredentialGate              -- enabled distinct-UID projection
scratch / FIFO / ACL
tx: lock, allocate lane uid, mint session token, insert supervisor rows,
    write launch binding
launchSupervisedProcess
```

The launch binding may be inserted in the same transaction as supervisor rows
once the projection receipt is known. If the implementation must allocate a
lane uid inside that transaction, the preconditions that need only the resolved
same-user/projection flag still run before scratch, and the projection gate runs
after uid allocation but before token minting, supervisor rows, and process
launch. If this requires source paths outside the downstream build envelope, the
build must call that out rather than widening scope silently.

### 2. Projection and receipts

`ProjectClaudeAccessToken` keeps v4 behavior with one addition: on success it
returns enough redacted launch-binding fields for the caller to persist:
`receipt_id`, `delivery_mode`, `source_generation_id`,
`destination_generation_id`, `destination_expires_at`,
`destination_selector`, `lane_user`, and `min_freshness_seconds`.

The custody receipt remains append-only and token-free. `refresh_token_absent_ok`
must be asserted for every projection. B2 broker fetches also emit token-free
per-fetch receipts tied to the same launch binding; they may update that binding
for the same supervisor/session only.

### 3. Recovery classification

Add a helper with fakeable clock and DB seams:

```text
ClassifyClaudeLaunchBindingFreshness(ctx, tx, stalledOwner, now) -> result
```

Result states:

```text
fresh_for_stalled_launch
reseed_required
unverifiable
binding_missing
binding_owner_mismatch
binding_superseded
```

Only `fresh_for_stalled_launch` may fall through to generic recovery. Every
other state writes provider-auth debt keyed by `binding_id` or by the missing
owner tuple, emits one redacted event per generation, and returns without
generic counter mutation.

### 4. Projection-off closure

Add `provider_credential_projection_disabled_unsupported` to the daemon error
catalog and event vocabulary:

```text
provider_credential.projection_disabled_refused
```

Payloads carry provider, kind, lane id/user, session id, gate mode, and the
closed failure enum only. They do not carry env values, credential paths, token
bytes, provider output, or private diagnostics.

### 5. Downstream write scope

The later build workflow needs source/doc write scope for:

- `go/pkg/laneproviderauth/`
- `go/pkg/mutations/supervision_provider_auth.go`
- `go/pkg/mutations/supervision_control.go`
- `go/pkg/mutations/supervision_env.go`
- `go/pkg/mutations/recovery_decision_tree.go`
- `go/pkg/mutations/recovery.go`
- `go/pkg/reads/doctor_lane_provider_auth.go`
- `go/pkg/rpc/error_catalog.go`
- `go/pkg/db/sql/`
- `go/cmd/striatum-supervisor-helper/`
- `docs/reference/command-authority-matrix.md`
- `docs/reference/spec.md`
- `docs/decisions/decision-log.md`
- generated or guarded daemon-contract docs and authority guardrail tests, if
  new RPC methods or route maps are added

This design lane does not modify those files.

---

## Required Tests

**C1 launch-bound recovery freshness**

- `TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection`:
  launch session A with G1 expiring in 35 minutes; launch session B under the
  same lane user/destination with fresh G2; advance A past G1 expiry; trigger
  `agent_mcp_discovery_stall` for A; assert recovery writes provider-auth debt
  for A's binding and does not increment `requeue_count` or `transfer_count`.
- `TestRecoveryPerSessionDecayDebtSurvivesNewerProjection`: mark A near-expiry,
  then project G2 for B; assert A's debt remains visible until A is restarted or
  the daemon proves A adopted a newer generation.
- `TestRecoveryLaunchBindingMissingOrMismatchedFailsClosed`: missing binding,
  mismatched session/supervisor, missing receipt, receipt mismatch, rotated
  source generation, and expired binding all refuse positive freshness without
  generic budget burn.
- `TestRecoveryPositivelyFreshLaunchBindingFallsThroughToGeneric`: active
  binding, matching passed receipt, future expiry, and matching source
  generation fall through to generic recovery for a non-auth stall.
- `TestRecoveryLaneSampleIsDowngradeOnly`: an earlier lane sample downgrades a
  binding; a later lane sample cannot upgrade one.

**C3 projection-off closure**

- `TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface`: seed a
  distinct-UID lane home with a whole Claude credential containing a fixture
  `refreshToken`, set `provider_credential_projection=off`, and assert
  `provider_credential_projection_disabled_unsupported` before scratch,
  FIFO/ACL, session-token minting, supervisor rows, projection receipt creation,
  helper/tmux, or process launch.
- `TestProjectionOffUnknownKindFailsClosedBeforeSideEffects`: omit the explicit
  credential kind, set `provider_credential_projection=off`, seed a whole Claude
  credential in lane `$HOME/.claude/.credentials.json`, and assert the same
  typed refusal before any launch side effect.
- `TestProjectionOffStillValidatesClaudeConfigDir`: set projection off and point
  `CLAUDE_CONFIG_DIR` or `CLAUDE_SECURESTORAGE_CONFIG_DIR` at a lane-readable
  credential dir containing a whole credential; assert the same typed refusal,
  no process, no raw path or token in durable payloads.
- `TestProjectionOffNonOAuthDiagnosticRequiresPositiveProof`: assert that a
  self-driving Claude launch cannot use the non-OAuth diagnostic exception from
  absent kind, absent resolver roster entry, or an unmodeled credential selector.
- `TestDistinctUIDAccessTokenProjectionStillLaunches`: with projection enabled,
  fresh operator source, and distinct lane uid, assert B1 or B2 launches and the
  lane-readable surfaces contain no `refreshToken`.

**C2 ordering**

- `TestSameUserClaudeLaneRefusedBeforeSideEffects`: parameterized over
  `provider_auth_gate=auto`, `off`, and `required`; each case returns
  `provider_credential_same_user_unsupported` before scratch, FIFO/ACL, session
  token, supervisor row, projection file, custody receipt, helper/tmux, or
  Claude process.
- `TestSameUserRefusalByResolvedUid`: username alias/shared uid resolving to the
  daemon uid refuses; a genuinely distinct uid proceeds to the projection gate.
- `TestSameUserCredentialDomainViolationPrecedesSameUserRefusal`: same-user
  Claude OAuth with repo-inside `CLAUDE_CONFIG_DIR` and
  `CLAUDE_SECURESTORAGE_CONFIG_DIR` returns the declared credential-domain
  refusal before scratch, token, supervisor row, projection file, custody receipt,
  helper/tmux, or Claude process.

**Carry-forward regression tests**

- `TestLaneNeverReceivesRefreshTokenAllSurfaces`
- `TestConcurrentLanesNoRefreshTokenDesync`
- `TestSubsequentLaneAfterOperatorRefresh`
- `TestProjectionRefusesBeforeSupervisorRows`
- `TestProjectionSourceRotationRace`
- `TestProjectionDestinationOwnerMode`
- `TestProjectionSymlinkEscape`
- `TestProviderAuthGateOffDoesNotBypassProjection`
- `TestProviderAuthGateOffDoesNotBypassSameUserRefusal`
- `TestProjectionResolverRejectsWorkflowPath`
- `TestProjectionReceiptRedaction`
- `TestTrustBoundaryNoControlPlaneTokenToLane`
- `TestClaudeCLIAcceptsAccessTokenOnlyCredential`
- `TestProjectorDoesNotRefreshProviderOAuth`

---

## Minimal Closure Scope For GH #583

GH #583 is closed only when source on `main` proves:

- same-user Claude OAuth lanes refuse with
  `provider_credential_same_user_unsupported` in every gate mode before side
  effects;
- projection-disabled Claude OAuth lanes refuse with
  `provider_credential_projection_disabled_unsupported` before side effects;
- distinct-UID Claude lanes receive only access-token material through B1 or B2;
- recovery classifies runtime Claude provider-auth expiry from the stalled
  launch binding, not a newer singleton lane-user row, and never spends generic
  recovery budget for that credential cause;
- per-session/generation debt survives newer projections until the old session
  is restarted or proven to have adopted the newer generation;
- durable receipts, events, doctor, dashboard, metrics, artifacts, and errors
  contain no raw OAuth material, no private full paths, no provider output, and
  no Striatum control-plane tokens.

No hosted service, cloud callback, telemetry export, durable transcript capture,
daemon OAuth client, or manual lane-home credential copy is part of the closure.
