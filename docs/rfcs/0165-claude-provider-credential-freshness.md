# RFC 0165: Claude provider credential freshness and spawn-time projection

Status: accepted (D277, 2026-07-01; build delegated to RFC 0173)
Date: 2026-06-23
Context: [#583](https://github.com/halbritt/striatum/issues/583); RFC 0096
(supervised-lane trust boundary); RFC 0121 (lane provider-auth preflight gate);
RFC 0143 (lane credential survival across boot-epoch rotation); RFC 0162 (lane
auth silent-failure observability)
author: proposer-codex-gpt-5-001

## Accepted Form

Accepted by [D277](../decisions/decision-log.md) after the
`rfc-0165-design-v5` falsification gate integrated on 2026-06-30. The accepted
design is the v5 proposal at
`docs/operator/artifacts/rfc-0165-design-v5/commit/proposal/PROPOSAL.md` plus
the cycle-3 `accept_with_findings` ledger. This RFC accepts the product
direction and constraints. It does not land source behavior by itself.

The implementation work is delegated to
[RFC 0173](0173-claude-provider-credential-projection-build.md), which turns the
accepted design into the `rfc-0165-build` source, SQL, generated-contract,
authority-map, test, and product-doc scope.

## Problem

Claude supervised lanes currently rely on a provider credential file under the
lane OS user's home:

```text
~striatum-lane/.claude/.credentials.json
```

That file is a point-in-time copy of the operator user's Claude OAuth credential.
It is not a durable Striatum capability token and it is not part of the daemon's
session-bound auth model. It is provider setup material needed before the Claude
CLI can participate in MCP discovery.

Claude OAuth refresh tokens rotate. After the operator's Claude CLI refreshes, a
lane copy taken earlier may hold both an expired access token and a stale refresh
token. The lane cannot self-refresh from that stale refresh token. In long
dogfoods, newly launched Claude lanes then wedge during MCP discovery with
`agent_mcp_discovery_stall`; autonomous recovery requeues the same doomed launch,
exhausts its budget, and raises `needs_operator` / doctor red. The live #583
incident required a manual operator copy from the fresh operator credential into
the lane home, followed by `chown`, `chmod 0600`, escalation resolution, and
re-drive.

Existing related designs cover adjacent surfaces but do not prevent this class:

- RFC 0121 gates supported provider auth at launch, but the implemented gate is
  Codex-scoped and does not hydrate Claude credentials.
- RFC 0143 protects Striatum session-token survival across daemon boot-epoch
  rotation; it is about MCP/RPC capability credentials, not provider OAuth files.
- RFC 0162 observes provider-auth expiry and missing success; it detects the
  rot, but detection alone still leaves the lane credential as stale inventory.

The root problem is that Claude provider credentials are treated as a copied
file, while operationally they are a rotating dependency with a custody chain,
freshness deadline, and admission gate.

## Goals

- Prevent a Claude lane from launching with a stale, expired, unparseable, or
  generation-drifted provider credential.
- Make credential freshness a spawn-time invariant for every Claude lane spawn,
  respawn, and recovery requeue path.
- Preserve the RFC 0096 / #135 / #296 Striatum trust boundary: lanes still
  authenticate to Striatum with their own session-bound capability token and
  never receive daemon/admin tokens or authority to mint tokens.
- Keep provider credential movement local, explicit, auditable, and
  private-safe: no raw OAuth material in daemon DB rows, repo artifacts,
  metrics, events, or doctor output.
- Stop burning autonomous recovery budget on repeated stale-provider-auth
  launches; classify this as provider-auth readiness debt with one operator
  action.
- Integrate with RFC 0162 telemetry instead of replacing it.

## Non-Goals

- Do not group-read or copy `/run/striatum/client-token`, the daemon bootstrap
  admin token, or any Striatum capability token into the lane account.
- Do not add a hosted provider service, Claude SDK dependency, cloud callback,
  telemetry export, or durable transcript capture.
- Do not ask a lane to refresh the operator's Claude OAuth credential. Refresh
  authority stays with the operator-side Claude credential owner.
- Do not make a background timer the correctness boundary. A timer may pre-warm
  state, but launch must synchronously verify freshness.
- Do not solve every provider in V1. This RFC targets Claude OAuth because #583
  is a Claude-specific rotating-refresh-token failure.
- Do not redefine RFC 0162 alert thresholds or RFC 0143 session-token survival.

## Proposal

Adopt a four-part design:

1. **Spawn-time Claude credential projection**: before every Claude lane
   `supervise.start` launch, respawn, or recovery requeue launch, the daemon
   invokes a narrow host-local projector. The projector reads the current
   operator Claude OAuth credential but gives the lane only access-token
   material that cannot refresh or rotate the operator credential family.
2. **Generation, custody, and launch binding gate**: the projection is accepted
   only if non-secret source and destination observations prove that the lane
   credential was projected from the current operator credential generation.
   The daemon also records which generation was bound to the launched
   job/session/supervisor so recovery cannot prove an older stalled process
   fresh with a newer lane-user row.
3. **Projection-off fail-closed classifier**: disabling provider credential
   projection is not a Claude OAuth bypass. A self-driving Claude launch with
   missing, unknown, or unmodeled credential kind is treated as Claude OAuth for
   RFC 0165 admission unless Striatum has positive pre-launch proof that no
   Claude OAuth resolver surface is in play.
4. **Provider-auth circuit breaker**: if Claude provider credentials are stale
   or unverifiable, recovery classifies the failure as provider-auth readiness
   debt and blocks repeated requeues until projection succeeds.

The load-bearing distinction is that provider OAuth material and Striatum
control-plane credentials remain separate. The lane may receive a Claude
provider access token because Claude needs it to start; the lane may not
receive a refresh token, operator credential path, daemon runtime token, admin
token, or refresh authority over Striatum's own control plane.

### 1. Claude Credential Projector

Add a provider-specific projector for Claude. Its job is small and deliberately
boring:

```text
ProjectClaudeCredential(source, destination, lane_user) -> ProjectionReceipt
```

Inputs are resolved by Striatum, not by workflow-authored shell:

- source: the operator-side Claude credential file, resolved with the same
  precedence the operator Claude CLI uses: `$CLAUDE_CONFIG_DIR/.credentials.json`
  when set, otherwise `$HOME/.claude/.credentials.json`.
- destination: the lane-side Claude projection target, resolved with the launch
  environment the lane will actually receive: `$CLAUDE_CONFIG_DIR/.credentials.json`
  when set, otherwise `$HOME/.claude/.credentials.json` for `striatum-lane`.
- lane_user: the configured `STRIATUM_LANE_OS_USER` after owner-same-user
  collapse.

The projector writes by temp file plus rename, then verifies:

- destination parent exists and is not a symlink escape;
- destination owner is the lane user;
- destination mode is `0600`;
- destination bytes parse as the lane projection shape expected by Claude;
- destination expiry is outside a configured minimum lead time;
- destination non-secret generation matches the source generation observed for
  this projection;
- every lane-readable credential surface named by the launch environment is
  `refresh_token_absent`.

The source may contain the operator refresh token because it is the operator's
Claude credential. The destination must not contain `refreshToken`, `idToken`,
or any other credential capable of refreshing the operator OAuth family. A B1
file projection writes only access-token material and expiry. A later B2 broker
mode may serve access tokens over a daemon-owned local channel after peer-uid
verification, but it still never gives the lane the refresh source.

The source and destination are read around the projection. If the source
generation changes between pre-projection and post-projection observation, the
projector retries once and then refuses with
`provider_credential_source_unstable`. This closes the rotation race where the
operator credential refreshes while the lane projection is being made.

### 2. Non-Secret Generation and Custody Records

Record provider credential freshness as redacted daemon-owned provenance. A
credential generation is not the credential contents and is not a reusable bearer
token. It is a privacy-safe observation such as:

- provider: `claude`;
- kind: `oauth`;
- source selector: `claude_config_dir` or `home_default`, not the full operator
  path when that path would expose a private home directory;
- fingerprint: an HMAC-SHA256 over the credential bytes using a daemon-local
  secret key, never a raw hash exported to repo artifacts;
- `expires_at` parsed through the existing RFC 0162 Claude expiry parser;
- observed file owner, mode, size, and mtime;
- observed_at.

For each projection, record a custody receipt:

- repository_id;
- run_id and session_id when available;
- lane_id and lane OS user;
- provider and kind;
- source generation id;
- destination generation id;
- projection_started_at and projection_completed_at;
- destination owner and mode verification result;
- verifier result: passed, source_unstable, source_missing, destination_write_failed,
  destination_unparseable, expiry_too_near, owner_mode_invalid,
  resolver_mismatch, refresh_token_present.

No raw credential bytes, OAuth access tokens, refresh tokens, id tokens, full
operator credential path, provider stdout/stderr, or Claude transcript content
is persisted.

Each successful launch also records a launch credential binding tied to the
launched owner, not merely to the latest lane-user readiness row:

```text
provider_credential_launch_binding
  repository_id, run_id, job_id, session_id, supervisor_id
  lane_id, lane_user, provider, kind, destination_selector
  delivery_mode, receipt_id
  source_generation_id, destination_generation_id
  destination_expires_at, min_freshness_seconds
  state, bound_at
```

Recovery uses this binding as the positive freshness authority for the stalled
process. A newer successful projection for the same lane user may prove that a
fresh relaunch is possible; it does not prove that an older running process has
adopted the newer generation.

### 3. Launch Gate Integration

Claude lane launch crosses this gate before the provider CLI starts real work.
For `supervise.start`, the order is:

1. Resolve the lane config, provider adapter, launch environment, and Claude
   credential assurance kind.
2. Enforce the lane credential-domain guard. A credential selector that resolves
   inside the target repository, or an unmodeled provider credential selector
   that resolves inside the target repository, is a higher-priority fail-closed
   precondition than same-user remediation because it protects the repository
   boundary.
3. If there is no credential-domain violation, enforce the same-user Claude
   OAuth precondition before the generic provider-auth preflight. A self-driving
   Claude OAuth lane whose resolved lane uid is the daemon/operator uid refuses
   with `provider_credential_same_user_unsupported`.
4. If `provider_credential_projection=off`, run the projection-off classifier.
   For `adapter == claude` and self-driving launches, missing, unknown, or
   unmodeled credential kind is treated as `OAUTH_COPIED` / Claude OAuth for
   RFC 0165 admission. The launch refuses with
   `provider_credential_projection_disabled_unsupported` unless Striatum has
   positive pre-launch proof that no Claude OAuth resolver surface is in play.
   Absence of a kind field, resolver roster entry, or diagnostic label is not
   proof.
5. If projection is enabled and the provider is Claude, project the lane Claude
   credential, record a custody receipt, record the launch binding, and verify
   that every lane-readable credential surface is `refresh_token_absent`.
6. Refuse launch if projection, generation verification, same-user policy, or
   projection-off classification fails.
7. Run any supported provider-auth preflight.
8. Mint and inject the Striatum session-bound capability token.
9. Create supervisor rows, scratch, FIFO/ACL state, tmux/helper state, and
   launch the lane.

The exact placement may vary in implementation, but the invariant must not:
Claude credential projection and verification happen before scratch creation,
FIFO/ACL work, session-token minting, supervisor rows, projection receipt
creation, helper/tmux setup, or provider process launch, and before recovery can
spend a work retry on that process.

Refusal errors use typed vocabulary:

```text
provider_credential_projection_failed
provider_credential_generation_stale
provider_credential_source_unstable
provider_credential_expiry_too_near
provider_credential_owner_mode_invalid
provider_credential_resolver_mismatch
provider_credential_same_user_unsupported
provider_credential_projection_disabled_unsupported
provider_credential_refresh_token_present
```

These are launch-precondition failures, not lane execution failures.

A non-OAuth diagnostic exception to projection-off may launch only after
positive pre-launch proof that the launch cannot use `$HOME/.claude`,
`CLAUDE_CONFIG_DIR`, `CLAUDE_SECURESTORAGE_CONFIG_DIR`, helper settings,
inherited credential paths, or any other Claude OAuth resolver surface. If
Striatum cannot model the surface, it fails closed.

### 4. Provider-Auth Dependency and Circuit Breaker

Promote Claude provider-auth readiness to a first-class daemon dependency for
supervision and recovery. The dependency state is keyed by repository, provider,
lane OS user, and source generation:

```text
ready
hydrating
reseed_required
unverifiable
disabled
```

When a Claude launch fails because the projected lane generation is stale,
missing, expired, or unverifiable, recovery must not keep requeueing the same
job as though the agent were flaky. It sets provider state to
`reseed_required`, records the affected runs and sessions, and emits one
operator-facing remediation:

```text
refresh or re-authorize the operator Claude credential, then retry projection
```

Once projection succeeds for a newer source generation, recovery may requeue only
jobs whose failure was classified against the stale generation. This preserves
provenance and avoids turning one host auth problem into many unrelated
`agent_mcp_discovery_stall` recoveries.

For an already-running Claude lane that stalls in MCP discovery, recovery must
load the stalled job/session/supervisor's launch credential binding. The binding
is positively fresh only when it exists for that owner, is `active`, has matching
receipt/source/destination generation ids, expires after the minimum lead time,
and the receipt verifier passed. Lane-readable file samples may downgrade this
classification; they may not upgrade a stale or missing binding. Missing,
stale, owner-mismatched, or inconsistent binding evidence records provider-auth
debt for that binding and does not increment generic requeue or transfer
budget.

### 5. Relationship To RFC 0162

RFC 0162 remains the alerting and metrics layer. This RFC supplies the behavior
that turns the alert into prevention:

- RFC 0162 `seconds_to_expiry` can page before projection would fail.
- This RFC's custody records explain which generation was projected into which
  lane.
- Doctor can join both surfaces: "alert says expiry is near" plus "launch gate
  refused generation N because operator generation N+1 exists."

If RFC 0162 sees a stale lane credential but no launch is happening, that is an
alertable health risk. If a launch is happening, this RFC makes it a hard
refusal before the lane consumes a work packet.

### 6. Host Timer Is Optional

A host-level `systemd` timer in `halbritt/proximal` may pre-warm the lane
credential projection every 20 to 30 minutes. That improves readiness latency and
operator ergonomics, but it is not the correctness mechanism. The launch path
must project or verify synchronously every time, because timers can be disabled,
delayed, or run against the wrong boot/user environment.

The timer should call the same projector path or a thin CLI wrapper around it,
not duplicate copy/chown/chmod logic in shell.

## Acceptance Criteria

- A dogfood spanning longer than the Claude OAuth access-token TTL completes
  without a Claude `agent_mcp_discovery_stall` caused by stale lane credentials.
- A stale lane `~/.claude/.credentials.json` is replaced by an access-token-only
  projection or refused before a real Claude lane process starts.
- If the operator credential rotates during projection, launch fails with
  `provider_credential_source_unstable` or retries into a current generation; it
  never blesses a mixed-generation projection.
- The normal distinct-UID path never gives the lane a `refreshToken`, `idToken`,
  raw operator credential JSON, or refresh authority.
- `provider_credential_projection=off` plus a self-driving Claude launch with
  missing, unknown, or unmodeled credential kind refuses with
  `provider_credential_projection_disabled_unsupported` before scratch, FIFO/ACL
  work, session-token minting, supervisor rows, projection receipt creation,
  helper/tmux setup, or provider process launch.
- Projection-off tests cover whole-credential fixtures with a `refreshToken` in
  lane `$HOME/.claude`, `CLAUDE_CONFIG_DIR`, and
  `CLAUDE_SECURESTORAGE_CONFIG_DIR`.
- Same-user Claude OAuth lanes without a credential-domain violation return
  `provider_credential_same_user_unsupported` before side effects for
  `provider_auth_gate=auto`, `off`, and `required`.
- Same-user Claude OAuth lanes with repo-inside `CLAUDE_CONFIG_DIR` or
  `CLAUDE_SECURESTORAGE_CONFIG_DIR` return the declared credential-domain error
  before side effects.
- Recovery classifies an expired launch generation against the stalled
  job/session/supervisor binding even when a newer projection exists for the
  same lane user.
- No daemon bootstrap admin token, runtime `client-token`, session-bound
  Striatum token, OAuth access token, OAuth refresh token, or provider stdout is
  written to repo artifacts, metrics, events, or doctor output.
- Recovery stops spending repeated requeues on provider generation drift and
  surfaces one provider-auth remediation action.
- Doctor and dashboard can show the latest Claude provider dependency state and
  the redacted custody reason for a launch refusal.
- `make test` covers: happy-path projection, stale lane generation refusal,
  source-rotation race, wrong owner/mode refusal, unparseable Claude credential
  refusal, projection-off unknown-kind refusal, same-user refusal precedence,
  credential-domain precedence, and recovery circuit-breaker behavior.

## Implementation Plan

### P0 - Failing fixtures

Add tests before behavior changes:

- stale lane credential generation refuses Claude launch before supervisor rows
  and process launch;
- source rotates during projection and does not pass;
- recovery classifies repeated stale-provider-auth launch attempts as a single
  provider-auth readiness problem;
- custody output redacts credential bytes and private path material;
- projection disabled with unknown or missing Claude credential kind refuses
  before scratch, supervisor rows, helper/tmux, token mint, custody receipt, or
  provider process;
- same-user Claude OAuth refuses before the generic provider-auth gate when no
  credential-domain violation exists;
- credential-domain violations intentionally precede same-user remediation when
  a same-user Claude OAuth selector resolves inside the target repository;
- recovery uses the stalled launch credential binding rather than a latest
  lane-user dependency row.

### P1 - Claude projector

Implement the provider-specific resolver and atomic write/verify helper. Reuse
the existing RFC 0162 Claude credential resolver and expiry parser where possible
so launch, metrics, and doctor agree on where Claude credentials live.
The destination projection must omit refresh tokens and id tokens.

### P2 - Launch gate and custody receipt

Wire projection, same-user refusal, projection-off classification, and launch
credential binding into the Claude `supervise.start` path before real provider
launch. Persist redacted custody receipts and launch bindings in daemon-owned
PostgreSQL or another daemon-owned local state surface; do not commit receipts
as repo artifacts.

### P3 - Recovery classification

Teach recovery and liveness classification to prefer provider-auth custody
evidence bound to the stalled launch when a Claude MCP discovery stall follows
a stale or unverifiable generation. Add `reseed_required` state and suppress
duplicate requeues until a fresh generation is available.

### P4 - Host integration

Add the optional `proximal` timer or host unit as a pre-warm caller of the same
projector. The timer is deploy hygiene, not a substitute for P2.

## Security And Privacy

The projector is an intentional privilege boundary. It reads provider OAuth
material from the operator account and writes only lane-safe projection material
into the lane account, so its authority must be narrow:

- source and destination paths come from provider resolvers, not user-supplied
  arbitrary paths;
- only the configured lane OS user can be the destination owner;
- only Claude credentials are handled in V1;
- projection output is exactly one credential file with mode `0600`, or a
  daemon-owned broker response, and neither contains refresh authority;
- no shell interpolation is needed;
- receipt data is redacted and token-free;
- the lane receives no Striatum admin or daemon runtime token.

The first implementation should be conservative: if ownership, path resolution,
source stability, credential parsing, expiry extraction, credential kind, or
absence of lane-readable refresh-token surfaces cannot be proven, the gate
refuses. A refused launch is cheaper and safer than a green-but-doomed Claude
lane.

## Open Questions

1. Where should the daemon-local HMAC key for credential fingerprints live, and
   how should it rotate without invalidating useful custody history?
2. Should custody receipts be a new table, a structured event stream, or a
   provider-auth dependency table with last-N receipt retention?
3. `provider_auth_gate=off` does not bypass Claude projection, same-user
   refusal, projection-off classification, or recovery freshness binding. It
   disables only the provider-auth smoke/preflight, not credential custody
   admission.
4. What is the minimum freshness lead time for Claude OAuth credentials before
   launch: 10 minutes, 30 minutes, or a fraction of observed TTL?
5. How should multi-provider use under one lane OS user be keyed if future
   providers add similar projectors?

## Domain Modeling

This RFC adds two domain concepts:

- **Provider credential generation**: a value object representing a non-secret
  observation of provider auth material at a point in time.
- **Provider auth dependency**: daemon-owned readiness state that gates lane
  launch and recovery behavior for a provider/lane pair.

Both concepts belong outside the Striatum session-token authority model. They
describe whether a lane can start its provider CLI, not who the lane is allowed
to be inside Striatum.

Per [`docs/DDD.md` "Adding to the model"](../reference/domain-driven-design.md#adding-to-the-model),
the behavior should be named in the ubiquitous language and exposed through
typed domain events such as:

```text
provider_credential.projected
provider_credential.projection_refused
provider_credential.launch_bound
provider_auth.reseed_required
provider_auth.reseed_cleared
```

## ADHD Ideation Record

This RFC was generated with the `adhd` skill: five isolated divergent branches,
then scoring, clustering, and three focus branches. The full branch output is
summarized here so the chosen shape is inspectable without treating the
brainstorm as decision authority.

### Brief

The reframe was: do not "sync a file"; make Claude provider auth a rotating,
auditable dependency that gates lane admission.

### Wide Set

**Generation and admission gates**

- Credential-generation attestation [N8 V9 F10]
- Provider-auth refusal gate [N6 V9 F9]
- Rotation quarantine [N7 V8 F8]
- Claude credential freshness as lane-admission gate [N7 V9 F10]
- Monotonic provider credential generations [N8 V8 F9]
- Provider credential generation gate [N8 V9 F10]
- Thymus admission self-test [N7 V8 F8]
- Immune memory without antibodies [N8 V8 F8]

**Hydration and custody**

- Claude-only credential broker ticket [N8 V6 F8]
- Credential custody ledger [N7 V9 F9]
- JIT credential cross-dock [N8 V9 F10]
- Milk-run refresh courier [N6 V7 F7]
- Waybill-bound credentials [N7 V8 F8]
- Hub-and-spoke credential broker [N7 V7 F8]
- Endocrine refresh pulses [N7 V7 F7]
- Microbiome sidecar nursery [N8 V6 F7]
- Claude-auth agent socket [N8 V6 F7]
- Sealed in-memory credential projection [N9 V4 F6]
- Credential refresh work item before spawn [N7 V8 F8]

**Recovery and circuit breakers**

- Auth-escalation covenant [N6 V8 F7]
- Stale-auth retry circuit breaker [N7 V9 F9]
- Returns desk for bad auth [N6 V8 F8]
- Quorum-sensing auth rot detector [N7 V7 F7]
- Provider-auth as first-class run dependency [N8 V8 F9]

**Canaries and sensors**

- Sacrificial MCP-discovery lane [N7 V6 F6]
- Quarantined provider-auth canary lane [N7 V6 F6]
- Cold-chain freshness gauges [N6 V8 F8]
- Apoptotic credential envelopes [N7 V7 F7]

**Opaque handle and lease variants**

- Short-lived per-lane provider leases [N9 V5 F7]
- Daemon-issued opaque Claude auth handle [N9 V4 F6]

### Converge

- Provider credential generation gate: selected because it gives a crisp
  falsifiable launch invariant.
- JIT credential hydration: selected because it is the smallest robust
  prevention mechanism and does not depend on a timer.
- Provider-auth circuit breaker: selected because it prevents recovery from
  converting one stale credential into many lane failures.
- [STAR] Custody ledger: non-obvious but viable; it is not just diagnostics,
  it is what makes the launch refusal reviewable without storing secrets.

Traps:

- Opaque provider handle: attractive, but it invents a larger broker protocol
  before a local file hydrator proves insufficient.
- In-memory projection only: elegant, but fragile across current CLI behavior
  and hard to verify portably.
- Background timer as the fix: useful as pre-warm, unsafe as the correctness
  boundary.
- Sacrificial canary as the fix: detects breakage but still lets real launch
  correctness depend on a separate path.

### Focus

**Generation gate and custody ledger**

Make Claude lane credential custody an explicit daemon-side preflight. The
operator credential source gets a daemon-observed generation record containing
only fingerprint, expiry, verifier status, and observed time. Each lane copy
gets a redacted custody receipt tying file owner, mode, copy time, source
generation, and verifier result to the lane session. The load-bearing risk is
the copy/verify race, so implementation must observe the source before and after
copy and refuse unstable generations. First step: add a failing launch test that
rotates the source generation after a lane copy and asserts launch refusal.

**Just-in-time hydration**

Make Claude credential freshness a spawn-time invariant owned by the host
supervisor. Every spawn or respawn invokes one hydrator that writes the current
operator credential to the lane path, fixes owner/mode, and records a generation
attestation. Background sync can exist, but launch revalidates synchronously.
The load-bearing risk is turning the hydrator into an implicit privilege bridge.
First step: test that provider discovery is preceded by a successful hydration
record newer than the stale lane file.

**Provider-auth circuit breaker**

Make Claude provider auth a daemon-owned dependency record. When observed lane
generation is missing, expired, or stale, flip provider state to
`reseed_required` and stop requeueing the same failure as a generic stall. The
operator surface shows owner, expiry, last-good generation, affected runs, and
retry budget already consumed. The load-bearing risk is distinguishing provider
auth drift from ordinary MCP discovery failures without letting lanes spoof
health. First step: add the dependency state and wire Claude MCP discovery
failures to `provider_auth_reseed_required`.

### Provocation

If provider auth is a dependency, should workflows declare provider readiness
requirements explicitly so the scheduler can park Claude-dependent jobs before a
run starts?
