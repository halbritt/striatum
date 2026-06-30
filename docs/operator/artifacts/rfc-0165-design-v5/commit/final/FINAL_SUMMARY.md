---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
---

# RFC 0165 v5 Final Summary
author: adjudicator-reviewer-001

## Gate Outcome

The RFC 0165 v5 collaboration gate cleared in
`docs/operator/artifacts/rfc-0165-design-v5/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_3.md`
with verdict `accept_with_findings`.

The downstream publication for this design run is
`docs/operator/artifacts/rfc-0165-design-v5/commit/proposal/PROPOSAL.md`. It
consolidates the falsification-cleared SPEC for Claude provider credential
freshness and explicitly stops short of source-build authorization. The accepted
finding is implementation-scope: source, SQL, error catalog, generated contract
or authority-map, tests, and product-doc changes belong in a successor
`rfc-0165-build` workflow with explicit write scope.

## Challenge Resolution

### C1 - launch-generation-bound recovery freshness

The v4 C1 failure was that recovery read the latest
`provider_auth_dependencies` row for `(repository_id, provider, kind,
lane_user, destination_selector)`. A newer G2 launch could therefore make an
older stalled G1 process look fresh and burn generic recovery budget.

V5 closes that race by introducing daemon-owned launch credential bindings tied
to the stalled owner tuple: repository, run, job, session, supervisor, lane,
provider, credential kind, destination selector, receipt, source generation,
destination generation, expiry, delivery mode, and adoption model. Recovery may
fall through to generic recovery only when the stalled launch binding is
`fresh_for_stalled_launch`. Missing, stale, owner-mismatched, receipt-mismatched,
source-generation-mismatched, superseded, or unverifiable bindings create
provider-auth debt without incrementing `requeue_count`, `transfer_count`, or
generic recovery-exhaustion counters. The latest dependency row remains an
admission and fresh-relaunch cache only; it is not positive proof for an older
running process.

### C3 - projection-disabled Claude OAuth closure

The v4 C3 gap was `provider_credential_projection=off`: a distinct-UID Claude
lane could skip B1/B2 access-token-only projection and read an old whole
credential from `$HOME/.claude`, `CLAUDE_CONFIG_DIR`, or another lane-readable
surface.

V5 closes the route by treating `adapter == claude` plus self-driving mode as
Claude OAuth / `OAUTH_COPIED` for RFC 0165 admission unless non-OAuth is
positively proven before launch. Missing, unknown, or unmodeled credential kind
is not enough to claim a diagnostic non-OAuth launch. For Claude OAuth or
unproven self-driving Claude launches, `provider_credential_projection=off`
fails closed with `provider_credential_projection_disabled_unsupported` before
scratch creation, FIFO/ACL work, session-token minting, supervisor rows,
projection receipt creation, helper/tmux setup, or provider process launch.
Normal distinct-UID access-token-only projection remains the supported path.

### C2 - same-user gate ordering

The v4 C2 caveat was that the generic provider-auth gate could preempt the typed
same-user Claude OAuth refusal. V5 moves same-user refusal before
`runSuperviseProviderAuthGate`, so `provider_auth_gate=auto`, `off`, and
`required` return `provider_credential_same_user_unsupported` before launch
side effects when no higher-priority credential-domain violation exists.

Cycle 3 also accepted one explicit precedence rule: repository credential-domain
violations intentionally outrank same-user remediation. A repo-inside
`CLAUDE_CONFIG_DIR` or `CLAUDE_SECURESTORAGE_CONFIG_DIR` may return
`lane_credential_cache_inside_repo` or
`lane_uncovered_credential_selector_inside_repo` first, but still before all
launch side effects.

## Preserved Carry-Forwards

The clearing ledger kept the v3 and v4 spine intact:

- same-user Claude OAuth self-driving lanes remain unsupported and fail closed;
- distinct-UID Claude OAuth lanes use B1/B2 access-token-only projection and
  never receive a refresh token through file, environment, helper, or broker
  surfaces;
- path, resolver, ownership, mode, source-rotation, and symlink-escape rules
  stay in scope;
- lane-side credential samples remain downgrade-only and never prove positive
  freshness;
- F1 decay and provider-auth debt remain daemon-owned;
- receipts, events, errors, doctor, dashboard, metrics, DB rows, and artifacts
  stay token-free and redacted;
- RFC 0096 control-plane separation remains intact: provider OAuth handling does
  not vend daemon, admin, runtime, session-token, DSN, or MCP-token material to a
  lane;
- `refresh_token_absent` remains the required custody assertion on projection
  surfaces.

## RFC 0169 Boundary

RFC 0169 remains the separate provider-agnostic credential-readiness spine. RFC
0165 v5 does not defer its closure to RFC 0169. It supplies the Claude-specific
`OAUTH_COPIED` assurance-class behavior that a later RFC 0169 registry can
generalize.

## Successor Build Contract

The successor `rfc-0165-build` workflow should implement the SPEC from
`docs/operator/artifacts/rfc-0165-design-v5/commit/proposal/PROPOSAL.md` and
preserve these named tests.

C1 recovery tests:

- `TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection`
- `TestRecoveryPerSessionDecayDebtSurvivesNewerProjection`
- `TestRecoveryLaunchBindingMissingOrMismatchedFailsClosed`
- `TestRecoveryPositivelyFreshLaunchBindingFallsThroughToGeneric`
- `TestRecoveryLaneSampleIsDowngradeOnly`

C3 projection-off tests:

- `TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface`
- `TestProjectionOffUnknownKindFailsClosedBeforeSideEffects`
- `TestProjectionOffStillValidatesClaudeConfigDir`
- `TestProjectionOffSecureStorageConfigDirUnknownKindFailsClosed`
- `TestProjectionOffNonOAuthDiagnosticRequiresPositiveProof`
- `TestDistinctUIDAccessTokenProjectionStillLaunches`
- `TestProviderAuthGateOffDoesNotBypassProjection`
- `TestProviderAuthGateOffDoesNotBypassSameUserRefusal`

C2 ordering tests:

- `TestSameUserClaudeLaneRefusedBeforeSideEffects`
- `TestSameUserRefusalByResolvedUid`
- `TestSameUserCredentialDomainViolationPrecedesSameUserRefusal`

Carry-forward regression tests:

- `TestLaneNeverReceivesRefreshTokenAllSurfaces`
- `TestConcurrentLanesNoRefreshTokenDesync`
- `TestSubsequentLaneAfterOperatorRefresh`
- `TestProjectionRefusesBeforeSupervisorRows`
- `TestProjectionSourceRotationRace`
- `TestProjectionDestinationOwnerMode`
- `TestProjectionSymlinkEscape`
- `TestProjectionResolverRejectsWorkflowPath`
- `TestProjectionReceiptRedaction`
- `TestTrustBoundaryNoControlPlaneTokenToLane`
- `TestClaudeCLIAcceptsAccessTokenOnlyCredential`
- `TestProjectorDoesNotRefreshProviderOAuth`

Minimal source closure for GH #583 requires source on `main` to prove:
same-user Claude OAuth refusal before side effects; projection-disabled Claude
OAuth or unproven self-driving Claude refusal before side effects; distinct-UID
access-token-only projection with no refresh-token custody; runtime provider-auth
expiry classified from the stalled launch binding, not a newer singleton row;
no generic recovery budget burn for credential causes; per-session/generation
debt surviving newer projections until restart or proven adoption; and redacted,
token-free durable surfaces.
