---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
---

# RFC 0165 Commit Proposal: Launch-Bound Claude Credential Freshness
author: committer-author-001

## Gate Clearance

Publish this as the downstream implementation SPEC for RFC 0165 after the
clearing ledger:

- `docs/operator/artifacts/rfc-0165-design-v5/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_3.md`
- verdict: `accept_with_findings`

The cycle 3 adjudicator accepted the v5 design with one finding: this is not
source-build authorization. The implementation must run through a successor
build workflow with explicit write scope for source, SQL, generated contracts or
authority maps, tests, and product docs.

This proposal consolidates the falsification-cleared SPEC for RFC 0165 only.
RFC 0169 remains the later provider-agnostic credential-readiness spine; RFC
0165 supplies the Claude `OAUTH_COPIED` assurance-class closure that RFC 0169 can
generalize later. Nothing in this proposal defers the RFC 0165 closure to RFC
0169.

## Implementation Claims

### C1 - Positive recovery freshness is bound to the stalled launch generation

RFC 0165 implementation must introduce daemon-owned launch credential bindings
for Claude self-driving lanes. Recovery may treat a stalled Claude lane as
positively fresh only from the binding for the stalled owner tuple, not from the
latest lane-user readiness row.

The launch binding records token-free evidence:

- `repository_id`, `run_id`, `job_id`, `session_id`, and `supervisor_id`;
- `lane_id`, `lane_user`, `provider=claude`, `kind=oauth`, and
  `destination_selector`;
- delivery mode, adoption model, receipt id, source generation id, destination
  generation id, destination expiry, minimum freshness lead, state, and
  timestamps.

`provider_auth_dependencies` remains a latest readiness cache for admission and
fresh relaunch decisions. It is never positive proof that an older stalled
process is still fresh.

Recovery classification for `agent_mcp_discovery_stall` must run before generic
requeue, transfer, recovery-exhaustion, owner-session closure, or
`recordRecoveryAction`. Only `fresh_for_stalled_launch` may fall through to
generic recovery. Missing, stale, expired, owner-mismatched, receipt-mismatched,
source-generation-mismatched, superseded, or unverifiable bindings record
provider-auth debt and do not spend generic recovery budget. Debt is keyed by
the binding or owner tuple and destination generation, so a newer projection for
the same lane user cannot clear an older session's debt unless the daemon proves
that same session adopted the newer generation.

B1 file projection is launch-static for recovery unless the build proves a
running Claude process re-reads and adopts a newer projection before each
provider action. B2 broker mode may update the same launch binding on authenticated
per-fetch receipts for the same supervisor/session.

Falsifiable assertions and tests:

| Assertion | Refuting observation | Required test |
|---|---|---|
| Recovery freshness is bound to the stalled launch binding, not the latest lane-user row. | Older session A on expired G1 reads newer G2 readiness and falls through to generic recovery. | `TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection` |
| Provider-auth expiry debt is per session/generation. | Newer G2 projection clears or hides A's G1 debt without proof A adopted G2. | `TestRecoveryPerSessionDecayDebtSurvivesNewerProjection` |
| Missing, stale, inconsistent, or owner-mismatched binding fails closed without generic budget burn. | Recovery increments `requeue_count`, `transfer_count`, or `recovery_exhausted` for a non-fresh binding. | `TestRecoveryLaunchBindingMissingOrMismatchedFailsClosed` |
| Lane-readable credential samples are downgrade-only. | A lane-file `expiresAt` upgrades a stale binding to fresh. | `TestRecoveryLaneSampleIsDowngradeOnly` |
| A valid launch-bound binding can fall through for a non-auth stall. | Active matching receipt, future expiry, and matching source generation are misclassified as provider-auth debt. | `TestRecoveryPositivelyFreshLaunchBindingFallsThroughToGeneric` |

### C3 - Projection disabled is not a Claude OAuth bypass

For `adapter == claude` and self-driving lane mode, missing, unknown, or
unmodeled credential kind is treated as `OAUTH_COPIED` / Claude OAuth for RFC
0165 admission. Absence of a kind field, resolver roster entry, or diagnostic
label is not proof that Claude OAuth discovery is impossible.

For Claude OAuth or unproven self-driving Claude launches,
`provider_credential_projection=off` must fail closed with:

```text
provider_credential_projection_disabled_unsupported
```

The refusal occurs before scratch creation, FIFO/ACL work, session-token
minting, supervisor rows, projection receipts, helper/tmux setup, or provider
process launch. A non-OAuth diagnostic exception may launch only after positive
pre-launch proof that no Claude OAuth resolver surface is in play, including
`$HOME/.claude`, `CLAUDE_CONFIG_DIR`, `CLAUDE_SECURESTORAGE_CONFIG_DIR`, helper
settings, inherited credential paths, credential-bearing environment entries,
or other Claude OAuth selectors.

Normal distinct-UID Claude lanes keep the access-token-only projection path:
B1 writes only lane-owned access-token material with mode `0600`, and B2 returns
only an access token over a daemon-owned `AF_UNIX` broker after `SO_PEERCRED`
verification. The existing all-surfaces `refresh_token_absent` assertion remains
mandatory for the projected path.

Falsifiable assertions and tests:

| Assertion | Refuting observation | Required test |
|---|---|---|
| Projection-off cannot launch Claude OAuth or unproven self-driving Claude. | Projection-disabled Claude reaches scratch, token mint, supervisor rows, receipt creation, helper/tmux, or process launch. | `TestProjectionOffUnknownKindFailsClosedBeforeSideEffects`, `TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface` |
| HOME and config-dir whole credentials are not projection-off raw-token routes. | Projection off plus a lane-readable whole credential under HOME, `CLAUDE_CONFIG_DIR`, or `CLAUDE_SECURESTORAGE_CONFIG_DIR` starts a Claude process or emits success. | `TestProjectionOffStillValidatesClaudeConfigDir`, `TestProjectionOffSecureStorageConfigDirUnknownKindFailsClosed` |
| Non-OAuth diagnostic launch requires positive proof. | A self-driving Claude launch falls through to diagnostic non-OAuth because kind or resolver modeling is absent. | `TestProjectionOffNonOAuthDiagnosticRequiresPositiveProof` |
| Normal distinct-UID projection still launches. | Projection-off closure breaks enabled access-token-only Claude projection with a fresh source. | `TestDistinctUIDAccessTokenProjectionStillLaunches` |
| `provider_auth_gate=off` does not bypass projection or same-user refusal. | Gate mode `off` skips projection, projection-off refusal, or same-user refusal. | `TestProviderAuthGateOffDoesNotBypassProjection`, `TestProviderAuthGateOffDoesNotBypassSameUserRefusal` |

### C2 - Same-user Claude OAuth fails closed before the generic provider-auth gate

Same-user Claude OAuth self-driving lanes remain unsupported. No in-uid boundary
can prevent a lane process running as the operator/daemon uid from reading the
operator's real Claude credential.

The same-user precondition applies when:

```text
adapter == claude
AND kind == oauth
AND agent_loop_mode == self_driving
AND (trim(config.RunAsUser) == "" OR lookup(config.RunAsUser).uid == daemon_uid)
```

When no higher-priority credential-domain violation exists, this precondition
runs before `runSuperviseProviderAuthGate` and before every side effect. It
returns:

```text
provider_credential_same_user_unsupported
```

with distinct-lane-user remediation in `provider_auth_gate=auto`, `off`, and
`required`.

Credential-domain violations intentionally outrank same-user remediation:
repo-inside or uncovered repo-local credential selectors protect the repository
boundary and may return `lane_credential_cache_inside_repo` or
`lane_uncovered_credential_selector_inside_repo` first. Those errors must still
occur before scratch, FIFO/ACL work, session-token minting, supervisor rows,
projection files, custody receipts, helper/tmux state, or provider process.

Falsifiable assertions and tests:

| Assertion | Refuting observation | Required test |
|---|---|---|
| Same-user refusal precedes the generic provider-auth gate in `auto`, `off`, and `required`. | Same-user Claude returns `lane_provider_auth_failed`, `lane_provider_preflight_unsupported`, or another generic provider-auth error first. | `TestSameUserClaudeLaneRefusedBeforeSideEffects` |
| Same-user refusal has no launch side effects. | Scratch, FIFO/ACL, session token, supervisor row, helper/tmux state, projection file, receipt, or Claude process exists after refusal. | `TestSameUserClaudeLaneRefusedBeforeSideEffects` |
| Credential-domain violations intentionally outrank same-user remediation. | Repo-inside `CLAUDE_CONFIG_DIR` or `CLAUDE_SECURESTORAGE_CONFIG_DIR` returns same-user first, or performs a side effect before the domain refusal. | `TestSameUserCredentialDomainViolationPrecedesSameUserRefusal` |
| UID aliases are caught. | A username alias resolving to the daemon uid launches a Claude OAuth lane. | `TestSameUserRefusalByResolvedUid` |

## Preserved Carry-Forwards

The build must preserve these accepted claims:

- Distinct-UID Claude OAuth lanes use access-token-only projection; lanes never
  receive a refresh token through file, env, helper, or broker surfaces.
- Refresh authority stays operator-side. Striatum does not add an Anthropic OAuth
  client, does not become a refresh-token writer, and does not ask a lane to
  refresh the operator credential.
- Lane-side samples are downgrade-only and never establish positive freshness.
- Receipts, events, errors, doctor, dashboard, metrics, artifacts, and durable
  DB rows are redacted. They may contain ids, enums, owner/mode booleans, expiry
  timestamps, HMAC generation ids, and `refresh_token_absent_ok`; they must not
  contain OAuth bytes, access tokens, refresh tokens, id tokens, raw credential
  JSON, full private paths, provider stdout/stderr, transcripts, DSNs, or
  Striatum control-plane tokens.
- RFC 0096 separation remains intact: Striatum session-bound capability tokens
  are not provider credentials, and provider OAuth handling never vends daemon,
  admin, runtime, or session-token material to a lane.
- `provider_auth_gate=off` cannot bypass same-user refusal, projection-off
  refusal, enabled projection, or recovery freshness classification.

Carry-forward regression tests:

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

## Build Layout And Scope

The successor build workflow needs explicit write scope for at least:

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
  the build adds RPC methods or handwritten route maps

If the correct implementation needs paths outside that envelope, the build lane
must report the mismatch instead of silently widening scope.

## Minimal Closure For RFC 0165 / GH #583

RFC 0165 closes only when source on `main` proves all of the following:

- same-user Claude OAuth lanes refuse with
  `provider_credential_same_user_unsupported` in every provider-auth gate mode
  before side effects, except for declared higher-priority credential-domain
  refusals that also occur before side effects;
- projection-disabled Claude OAuth or unproven self-driving Claude lanes refuse
  with `provider_credential_projection_disabled_unsupported` before side
  effects;
- distinct-UID Claude lanes receive only access-token material through B1 or B2,
  and every lane-readable credential surface satisfies `refresh_token_absent`;
- recovery classifies runtime Claude provider-auth expiry from the stalled
  launch binding, not a newer singleton lane-user row;
- generic recovery budget is not spent for credential causes;
- per-session/generation debt survives newer projections until the old session
  is restarted or the daemon proves the old session adopted the newer
  generation;
- durable receipts, events, doctor, dashboard, metrics, artifacts, and errors
  contain no raw OAuth material, private full paths, provider output, or Striatum
  control-plane tokens.

No hosted service, cloud callback, telemetry export, durable transcript capture,
daemon OAuth client, or manual lane-home credential copy is part of the closure.
