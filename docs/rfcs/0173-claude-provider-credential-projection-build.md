# RFC 0173: Claude provider credential projection build

Status: proposed
Date: 2026-07-01
Context: [RFC 0165](0165-claude-provider-credential-freshness.md), [D277](../decisions/decision-log.md),
[#583](https://github.com/halbritt/striatum/issues/583), RFC 0096,
RFC 0121, RFC 0143, RFC 0162, RFC 0168, RFC 0169, and
`docs/operator/artifacts/rfc-0165-design-v5/commit/proposal/PROPOSAL.md`.
author: proposer-codex-gpt-5-001

## Problem

RFC 0165 is accepted, but the source build is intentionally unstarted. The
accepted design proves the product shape: Claude lanes need spawn-time,
access-token-only credential projection, launch-bound recovery freshness,
projection-off fail-closed handling, and strict separation between provider
OAuth material and Striatum control-plane credentials.

The build risk is not just missing code. A weak implementation can accidentally
create a second credential path, let recovery borrow a newer lane-user state for
an older stalled process, or prove refusal only in a simulator that production
launch does not use. That would make the accepted RFC look shipped while leaving
the #583 stale-copy class alive.

## Goals

- Implement Claude access-token-only credential projection before every real
  Claude lane launch, respawn, and recovery requeue launch.
- Make the credential projection contract one generated source of truth for
  storage fields, RPC/CLI denial states, supervisor launch input, route
  authority tests, redaction rules, docs obligations, and downgrade fixtures.
- Record immutable, redacted launch credential bindings tied to the launched
  run, job, session, supervisor, lane user, projection receipt, and credential
  generation.
- Make recovery evaluate provider-auth freshness against the launch binding for
  the stalled process, not the latest provider-ready state for the lane user.
- Refuse same-user Claude OAuth and projection-off unknown-kind cases before any
  launch side effects, using the same planner that production launch consumes.
- Keep provider-auth debt separate from generic recovery budget exhaustion.
- Preserve the RFC 0096 control-plane trust boundary: lanes never receive a
  daemon admin token, runtime client token, refresh token, raw operator
  credential JSON, or refresh authority.
- Keep all durable surfaces private-safe and token-free.

## Non-Goals

- Do not give a lane Claude refresh-token custody or authority to refresh the
  operator OAuth credential family.
- Do not add a hosted service, Claude SDK dependency, cloud callback,
  telemetry export, or durable transcript capture.
- Do not generalize all providers in this build. RFC 0169 owns the
  provider-agnostic readiness spine.
- Do not make a background timer, pre-warm unit, or operator copy command the
  correctness boundary.
- Do not add a `provider_auth_gate=off` or projection-off bypass for Claude
  OAuth admission, same-user refusal, or recovery freshness binding.
- Do not store raw access tokens, refresh tokens, id tokens, full private home
  paths, provider stdout, or provider stderr in PostgreSQL rows, repo artifacts,
  metrics, events, doctor output, dashboard output, or operator briefs.

## Proposal

Implement RFC 0165 as one `rfc-0165-build` code-change campaign with six
reviewable slices. Each slice should be small enough for ordinary Striatum
review, but the final acceptance criteria apply to the whole campaign.

### B0 - Generated Projection Contract

Add a checked-in credential projection contract, such as
`contracts/provider_credential_projection.yaml`, that models only the Claude
access-token-only build surface for this RFC. The contract names:

- provider: `claude`;
- credential kinds and assurance classes;
- allowed delivery modes;
- required binding fields;
- denial states;
- redaction classes;
- durable-surface obligations;
- route and RPC/CLI surfaces that may expose denial states;
- generated test fixture targets.

No contract field may hold a refresh token, id token, raw access token, raw
credential JSON, provider stdout/stderr, or full operator credential path.

Generate or mechanically validate the following spokes from the contract:

- Go constants or enum validation for denial states and redaction classes;
- RPC/CLI error vocabulary coverage;
- authority-map tests for launch, provider-auth, and recovery routes;
- SQL migration assertions that reject refresh-token-bearing storage shapes;
- redaction manifest tests for durable surfaces;
- docs obligation checks for `docs/reference/spec.md`,
  `docs/reference/command-authority-matrix.md`, and the ship checklist.

The first B0 test should fail if RPC/CLI denial enums drift from the contract.

### B1 - Pure Pre-Spawn Planner And Refusal Witness

Extract provider launch authorization into a side-effect-free planner:

```text
PlanProviderLaunch(request) -> LaunchPlan | LaunchRefusal
```

The request includes target repository, provider, requested projection mode,
resolved OS user, adapter, relevant credential selectors, and available
credential handles. The output includes either:

- a launch plan whose credential projection input is safe for production launch;
  or
- a typed refusal with a redacted projection/env diff and denial reason.

Production `supervise.start`, respawn, and recovery requeue launch paths must
consume this planner output before scratch directory creation, FIFO/ACL work,
session-token minting, supervisor rows, projection receipts, helper/tmux setup,
or provider process launch. The planner is not a parallel simulator with its own
policy. It is the production precondition.

The refusal witness test must snapshot absence of those side effects for
same-user Claude OAuth, projection-off unknown-kind, missing-kind, unmodeled
kind, and refresh-token-present cases.

### B2 - Claude Access-Token-Only Projection

Implement the projector selected by B1. The first build can use a file
projection because the Claude CLI currently expects a credential file; a later
broker mode remains future work unless this build proves file projection
insufficient.

The projector must:

- resolve source and destination through Striatum-owned provider resolvers;
- observe the source before and after projection;
- retry once on source generation movement, then refuse
  `provider_credential_source_unstable`;
- write by temp file plus rename;
- verify destination owner, mode `0600`, selector, non-symlink ancestry, parse
  shape, expiry lead, and `refresh_token_absent`;
- emit only redacted projection receipt fields;
- refuse any lane-readable refresh token, id token, raw operator credential
  JSON, or unmodeled OAuth resolver surface.

### B3 - Immutable Launch Credential Binding

Persist a redacted `LaunchCredentialBinding` when a launch plan becomes a real
Claude lane launch. The binding is immutable after launch and names:

- repository, run, job, session, supervisor, lane, and lane user;
- provider, credential kind, assurance class, projection mode, and delivery
  mode;
- source and destination generation ids;
- receipt id and receipt digest;
- principal fingerprint;
- expiry, minimum freshness lead, bound time, and launch generation;
- allowed downgrade modes;
- redaction class and provider-auth debt class when refused or downgraded.

The binding must store opaque local secret references or digests only when
needed. It must not store token bytes, refresh-token material, raw credential
JSON, full operator credential paths, provider output, or Striatum control-plane
tokens.

Recovery must join against this binding before it consults any current
lane-user provider-ready state.

### B4 - Recovery Provider-Auth Debt

Teach recovery to classify provider-auth freshness from the stalled process's
launch binding. A missing, stale, mismatched, expired, owner-mismatched, or
receipt-inconsistent binding records provider-auth debt and fails closed or
downgrades only when the binding explicitly permits downgrade diagnostics.

Provider-auth debt classes should include at least:

```text
credential_missing
projection_disabled
principal_mismatch
token_expired
source_generation_stale
binding_missing
binding_mismatch
downgrade_only_required
refresh_token_present
```

This debt does not burn generic recovery requeue or transfer budget. Generic
budget remains a scheduling guard; provider-auth debt is the semantic reason a
lane cannot safely resume or relaunch.

Lane credential samples are downgrade-only evidence. They may explain why a
binding is no longer safe. They may not upgrade a missing, stale, or mismatched
binding into fresh authority.

### B5 - Operator Surfaces And Product Docs

Expose token-free provider-auth state through the existing operator surfaces:

- `doctor --json`;
- `status --json`;
- dashboard;
- recovery refusal/remediation output;
- operator reports and briefs;
- `docs/reference/spec.md`;
- `docs/reference/command-authority-matrix.md`;
- `docs/operator/rfc-roadmap.md`;
- `CHANGELOG.md`.

If the build adds or renames RPC methods or handwritten route maps, update the
command authority matrix and authority guardrail tests in the same slice.

### B6 - Verification And Dogfood

The campaign ships only after ordinary Go verification plus an end-to-end
dogfood whose Claude work spans longer than the observed access-token TTL or a
fixture that proves the same rotation behavior without requiring real token
material in artifacts.

The dogfood proof must show:

- fresh distinct-UID Claude launch succeeds without lane refresh-token custody;
- same-user and projection-off unsupported cases refuse before side effects;
- recovery classifies stale launch bindings as provider-auth debt;
- no durable surface leaks token-shaped values or private credential paths.

## Acceptance Criteria

- `TestCredentialProjectionContractDenialEnumsMatchRPCAndCLI` proves generated
  denial states match RPC/CLI surfaces.
- `TestGeneratedCredentialContractCoversProjectionRoutes` proves launch,
  provider-auth, and recovery routes are represented in authority-map tests.
- `TestProjectionContractRejectsRefreshTokenStorage` proves migrations or
  schema helpers cannot add refresh-token-bearing rows for this build.
- `TestPreSpawnProjectionPlanHasNoSideEffects` proves refusal occurs before
  scratch, FIFO/ACL, session-token, supervisor row, helper/tmux, receipt, or
  provider process side effects.
- `TestSameUserClaudeLaneRefusedBeforeSideEffects` and
  `TestSameUserRefusalByResolvedUID` prove same-user refusal is based on the
  resolved uid and happens before launch side effects.
- `TestSameUserCredentialDomainViolationPrecedesSameUserRefusal` proves
  repo-inside credential selector violations outrank same-user remediation.
- `TestProjectionOffUnknownKindFailsClosedBeforeSideEffects` proves
  projection-off cannot treat missing, unknown, or unmodeled Claude credential
  kind as safe.
- `TestProjectionOffSecureStorageConfigDirUnknownKindFailsClosed` and
  `TestProjectionOffStillValidatesClaudeConfigDir` cover the Claude resolver
  surfaces that can otherwise hide OAuth.
- `TestProjectionOffNonOAuthDiagnosticRequiresPositiveProof` proves absence of
  kind or resolver labels is not positive non-OAuth proof.
- `TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface` proves
  projection-off does not bless a lane-readable refresh-token fixture.
- `TestProviderAuthGateOffDoesNotBypassProjection` and
  `TestProviderAuthGateOffDoesNotBypassSameUserRefusal` prove
  `provider_auth_gate=off` does not bypass RFC 0165 admission.
- `TestDistinctUIDAccessTokenProjectionStillLaunches` proves the intended
  distinct-lane-user access-token-only path works.
- `TestLaneNeverReceivesRefreshTokenAllSurfaces` proves lane-readable Claude
  credential surfaces are `refresh_token_absent`.
- `TestProjectionReceiptRedaction` and
  `TestTrustBoundaryNoControlPlaneTokenToLane` prove durable surfaces and lane
  envs do not receive provider secrets or Striatum admin/runtime tokens.
- `TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection` proves
  recovery evaluates the stalled launch binding rather than a newer lane-user
  readiness row.
- `TestRecoveryPerSessionDecayDebtSurvivesNewerProjection` proves a newer
  projection does not erase debt for an older stalled process.
- `TestRecoveryLaunchBindingMissingOrMismatchedFailsClosed` proves missing or
  inconsistent binding evidence cannot be upgraded by samples.
- `TestRecoveryLaneSampleIsDowngradeOnly` proves lane samples can downgrade but
  cannot upgrade binding freshness.
- `TestProviderAuthDebtDoesNotBurnGenericRecoveryBudget` proves provider-auth
  debt has its own accounting and operator remediation.
- `TestRecoveryPositivelyFreshLaunchBindingFallsThroughToGeneric` proves a
  valid binding leaves ordinary recovery semantics intact.
- A real or fixture-backed dogfood spanning longer than the Claude access-token
  TTL completes without a stale-copy `agent_mcp_discovery_stall`.
- `make lint`, `make typecheck`, `make test`, `make smoke` and
  `make check-docs` pass, or any intentionally deferred gate has a recorded
  blocker and does not hide token/custody failures.

## Build Workflow Scope

Open the build as `rfc-0165-build`. The expected write scope includes:

- `contracts/` for the projection contract and generated/validated contract
  fixtures;
- `go/pkg/laneproviderauth/` for provider credential planning, projection, and
  redaction helpers;
- `go/pkg/mutations/supervision_provider_auth.go` and related supervision launch
  control paths;
- recovery mutation/read packages that classify launch-bound provider-auth debt;
- `go/pkg/db/migrations/` and authority inventory updates if new tables or rows
  are needed;
- daemon RPC/error catalog and CLI surfaces that expose typed denial states;
- doctor/status/dashboard read surfaces;
- targeted Go tests for every acceptance criterion;
- `docs/reference/spec.md`;
- `docs/reference/command-authority-matrix.md`;
- `docs/operator/rfc-roadmap.md`;
- `docs/operator/BRIEF.md`;
- `CHANGELOG.md`.

The build workflow should not write `.striatum/`, private diagnostics,
provider credential files, token caches, or target-repository artifacts outside
the run's explicit scope.

## ADHD Build Synthesis

This build RFC used the `adhd` skill as a design stress pass after RFC 0165 was
accepted. Five isolated divergent branches produced ideas, which were then
clustered and focused into three build constraints:

| Cluster | Why it survived |
| --- | --- |
| Generated projection contract | Prevents contract drift between storage, RPC/CLI, supervisor launch input, route authority, tests, and docs. |
| Immutable launch binding and provider-auth debt | Makes recovery judge the stalled process's original credential envelope, not whatever is fresh now. |
| Pure pre-spawn planner and refusal witness | Proves refusal on the real launch path before side effects, not in a parallel simulator. |
| Downgrade-only lane samples | Lets diagnostics explain unsafe state without letting samples become authority. |
| Redaction and no-secret manifests | Keeps accepted custody claims tied to durable behavior instead of prose. |

The rejected traps were: background timer as correctness boundary, lane refresh
token custody, an emergency projection-off bypass, a broker protocol before file
projection proves insufficient, and a simulator that production launch does not
consume.

## Open Questions

1. Is the first contract artifact YAML, JSON, or a Go-native schema with a
   generated docs fixture?
2. Does the first build need a broker mode, or is access-token-only file
   projection sufficient for the current Claude CLI?
3. What exact minimum freshness lead should the projector enforce for Claude
   access tokens?
4. Should the side-effect sentinel check provider process lists and helper/tmux
   state in unit fixtures, integration fixtures, or both?
5. Which operator surface should own provider-auth debt history retention:
   recovery status, doctor detail, or a future dedicated read?

## Domain Modeling

This build adds four domain concepts:

- **ProviderCredentialProjectionContract**: a contract artifact and validation
  source for credential projection vocabulary, redaction classes, durable
  surfaces, and route authority obligations.
- **ProviderLaunchPlan**: a value object that decides launch authorization
  before side effects and is consumed by production launch.
- **LaunchCredentialBinding**: an immutable aggregate member for the launched
  process's provider credential envelope.
- **ProviderAuthDebt**: a recovery classification explaining why a process
  cannot safely resume or relaunch under its launch binding.

The expected domain events are:

```text
provider_credential.projection_planned
provider_credential.projection_refused
provider_credential.projected
provider_credential.launch_bound
provider_auth.debt_recorded
provider_auth.debt_cleared
```

Per [`docs/reference/domain-driven-design.md` "Adding to the model"](../reference/domain-driven-design.md#adding-to-the-model),
these names should be added to the ubiquitous language when the source build
lands.
