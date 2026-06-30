# RFC 0165 Holder Proposal (v3): Daemon-Brokered Access-Token Projection for Claude Lanes
author: holder-author-001

This is the **v3 revision** of the RFC 0165 implementation SPEC (Claude provider
credential freshness + spawn-time hydration; GH #583). It is a *revision of the
v1 HOLDER*, not a rewrite: the v1 launch-time spine is carried forward intact and
the two binding v1 findings are discharged by changing **what the lane receives**
(an access token, never a refresh token) and by adding a **runtime-expiry
classification hook in recovery**. v2 is quarantined and is not relied on here.

---

## Addressing the v1 findings (auditable map)

The v1 cycle-1 ledger landed two unrebutted findings and four binding
constraints (C1–C4). Here is exactly how this revision discharges each, and
which v1 decisions are carried forward unregressed.

### F2 — no lane raw-refresh-token custody (CRITICAL) → **RESOLVED**

**v1 defect.** v1 copied the *whole* operator `~/.claude/.credentials.json`
(access **and** refresh token) into each lane home. That gives every lane raw
refresh-token custody: with OAuth refresh-token rotation a lane-side `claude`
refresh rotates the refresh token in the lane file while the operator source
stays stale, invalidating the operator source for later lanes and possibly the
operator's own CLI; concurrent lanes share stale copied refresh-token state.

**v3 fix (the structural change).** The lane never receives a refresh token.
Refresh authority is held in exactly one place — the **operator-side credential
owner** — and lanes receive only an **access-token-only projection** produced by
a daemon-owned projector/broker. Concretely:

1. **Single refresh authority (one writer of the refresh token).** The operator
   source credential file is the single durable store of the OAuth *refresh*
   token, and the operator-side Claude credential owner (the operator CLI, or the
   host pre-warm timer running **as the operator**) is the only party that ever
   performs a refresh-token rotation. The daemon does **not** add an Anthropic
   OAuth client and does **not** write the operator source (see "Non-goal:
   daemon-side OAuth refresh" below). No second writer exists, so there is no
   writer to desynchronize.
2. **Access-token-only delivery to lanes.** What a lane gets is the *current*
   short-lived OAuth **access** token and its `expiresAt` — never the refresh
   token. Two delivery mechanisms, B1 primary, B2 the named fallback; **both deny
   the lane a refresh token**, so the F2 discharge does not depend on which the
   Claude CLI accepts (that only selects B1-vs-B2):
   - **B1 (primary) — access-token-only file projection.** The daemon writes a
     lane-owned `0600` `.credentials.json` containing `accessToken`, `expiresAt`,
     and the non-secret account/scope fields the CLI needs, with **no
     `refreshToken` key**. With no refresh token present, the lane's `claude`
     physically cannot rotate anything; if its access token expires it fails
     closed (caught by the F1 circuit breaker) instead of rotating the operator
     family.
   - **B2 (fallback, only if the CLI refuses an access-token-only file) —
     daemon credential-broker socket + CLI credential helper.** A daemon-owned
     unix-domain socket at a lane-readable path; the lane's Claude CLI is
     configured at launch (injected `apiKeyHelper`/credential-helper setting,
     never workflow-authored) to fetch the *current* access token from a thin
     `striatum-supervisor-helper credential` subcommand. The broker returns only
     the access token, never the refresh token. **Caller identity = the lane OS
     uid via `SO_PEERCRED`**, not a Striatum capability token — preserving the
     trust boundary. **Token lifetime = the access token's own TTL**; the helper
     is invoked on demand so the lane always sees the freshest projection and
     nothing is persisted on the lane disk.
3. **Lane has no read path to the operator source.** The operator credential is
   operator-owned `0600`; the lane OS user is a *distinct* uid and has no
   filesystem access to it. The daemon reads it under its own/operator authority
   (existing `LaneFileReader` shape, reversed for the operator side), never as
   the lane. The lane cannot reach the refresh token by reading the source
   either.

This discharges constraint **C1** (no lane raw refresh-token custody; broker IPC
surface, token lifetime, caller-identity check, and CLI configuration all named).
The named concurrent-lane and subsequent-lane RTR tests are in *Required tests*.

### F1 — runtime-expiry circuit breaker (HIGH) → **RESOLVED**

**v1 defect.** Spawn-time hydration can pass, then the access token expires
mid-session before the lane first calls Claude. The stored dependency still says
`ready` and the receipt says `passed`, so recovery treats the resulting
`agent_mcp_discovery_stall` as generic and burns `requeue_count` / `transfer_count`
budget before `reseed_required` is found on a later launch.

**v3 fix.** Recovery performs a **current** freshness check from daemon-owned
state *before* it increments generic counters, and a daemon-owned periodic signal
surfaces near-expiry of running lanes as provider-auth readiness debt:

1. **Recovery-time freshness classification (C2).** In the recovery decision tree
   `recoverStuckJobs` (`go/pkg/mutations/recovery_decision_tree.go:704`), for a
   Claude lane whose stall class is `agent_mcp_discovery_stall`, a provider-auth
   freshness check runs **after** `readJobRecoveryBudget` (line ~334/1143) and
   **before** `recordRecoveryAction` (line 1406) increments `requeue_count` /
   `transfer_count`. It computes **current** seconds-to-expiry from the
   daemon-owned `provider_auth_dependencies` row (and, if stale, a single bounded
   re-sample). If the projection is expired / near-expiry / unverifiable *now*,
   recovery sets dependency state `reseed_required` (or `unverifiable`), emits one
   redacted event, and **does not** increment the generic counters or escalate
   `recovery_exhausted`; it re-projects against a fresh operator generation and
   requeues only jobs blocked against the stale generation.
2. **Daemon-owned decay signal (C3).** The same periodic, daemon-owned recovery /
   reconcile sweep (`HandleRecoveryAuto`, `go/pkg/mutations/recovery.go:553`,
   which already runs `recoverStuckJobs`) evaluates each *running* Claude lane's
   projected-token `expires_at` from the `provider_auth_dependencies` row — i.e.
   from **daemon broker state, never from a lane-authored heartbeat claim or
   provider stdout/stderr**. When a running lane crosses the near-expiry lead, the
   sweep emits `provider_auth.expiry_warning` and marks the dependency near-expiry
   debt **before** the generic MCP-discovery recovery path can classify the same
   lane. (The supervisor heartbeat path today is event-driven with no per-lane
   tick — `refreshSupervisorHeartbeat`, `supervision_control.go:1509` — so the
   periodic daemon-owned tick is this recovery/reconcile sweep, not a lane
   heartbeat; this keeps the signal off any lane-trusted channel.)

This discharges constraints **C2** and **C3**.

### Carry-forwards INTACT (v1 got these right — not regressed)

- **Spawn-time freshness gate.** A Claude lane still cannot launch with a stale,
  expired, unparseable, or generation-drifted credential — on every spawn,
  respawn, and recovery requeue. v3 changes the *operation* from "copy whole
  credential" to "project access-token-only," but keeps the gate, its placement
  before supervisor rows/scratch/token-mint/process, and the typed refusal
  vocabulary (see *Spawn-time projection gate*).
- **RFC 0096 / #135 / #296 trust boundary.** Provider OAuth (access token via B1
  file or B2 `SO_PEERCRED` socket) stays wholly separate from the Striatum
  session-bound capability token (`STRIATUM_MCP_TOKEN`, injected at
  `supervision_env.go`). The broker never reads or vends `/run/striatum/client-token`,
  the daemon bootstrap admin token, or any capability token; lanes still
  authenticate to Striatum with their own session token and receive no
  daemon/admin/mint authority.
- **`provider_auth_gate=off` cannot bypass.** Projection + freshness is
  independent of that flag (which only governs the Codex preflight in
  `runSuperviseProviderAuthGate`). Only a separate explicit
  `provider_credential_projection=off` emergency flag can skip it; it marks the
  dependency `disabled`, emits a redacted bypass event, is never implied by
  `provider_auth_gate=off`, and is documented unsafe.
- **Redacted, private-safe custody receipts.** No raw OAuth material (refresh
  token, access token, id token, raw bytes), full private paths, provider
  stdout/stderr, or Striatum control-plane tokens in DB rows, repo artifacts,
  metrics, events, or doctor output. The access token now lives **only** in
  transient daemon memory and the `0600` lane projection (B1) or the
  `SO_PEERCRED` socket response (B2); the refresh token is never read into the
  projector at all.

---

## Claim

Claude credential freshness must become a synchronous launch invariant **and** a
runtime-classified dependency, delivered to lanes as an **access-token-only
projection** that never carries a refresh token. Before any real Claude lane
process starts, the daemon projects the operator's *current* access token into
the lane (B1 file projection, or B2 broker socket), verifies freshness, persists
a redacted custody receipt, and refuses launch when freshness cannot be proven.
While a lane runs, the daemon-owned recovery/reconcile sweep tracks the
projected token's expiry from daemon state and classifies runtime decay as
provider-auth readiness debt before generic recovery burns budget. Refresh
authority is held in exactly one place (the operator-side credential owner);
lanes can neither receive nor exercise a refresh token, so no lane can
desynchronize or invalidate the operator source — concurrently or subsequently.

This is narrower than a full provider-auth broker protocol: it fixes #583 by
denying lanes the one piece of material (the refresh token) that makes the
local-file model unsafe, while reusing the existing launch, sampling, and
recovery surfaces.

---

## Non-goal: daemon-side OAuth refresh

The daemon does **not** add an Anthropic OAuth client and does **not** perform
refresh-token rotation. Two reasons, both load-bearing:

- It would make the daemon a *second* refresh-token writer racing the operator's
  own `claude` CLI — recreating the exact desync F2 condemns, just between daemon
  and operator-CLI instead of between lanes.
- No daemon OAuth client exists today (confirmed: no `net/http`/`oauth2` token
  exchange in `go/pkg/laneproviderauth`); building one is a large, network-facing,
  out-of-scope lift the RFC's own non-goals already forbid ("refresh authority
  stays with the operator-side Claude credential owner").

Refresh authority therefore stays operator-side (status quo of *who refreshes*),
but lanes are now insulated from it: they only ever see access tokens. The host
pre-warm timer's role is bounded to keeping the **operator source** fresh (e.g.
running a refresh **as the operator**); it must **not** copy whole credentials
into lane homes — lane credential material is produced *only* by the daemon
projector. This closes the timer backdoor that would otherwise reintroduce lane
refresh-token custody.

---

## Current Source Anchors (verified against `main`)

- **Launch path.** `go/pkg/mutations/supervision_control.go::HandleSuperviseStart`
  (lines 83–323) orders: `loadSupervisionStartConfig` (97) → `runSuperviseProviderAuthGate`
  (101) → scratch dir + FIFO (104–135) → tx: `mintSessionBoundToken` (158) →
  `insertStartingsSupervisorRowsWithCleanError` (165) → `supervisionLaunch` /
  `launchSupervisedProcess` (193). The projection gate inserts after line 97 and
  before line 104 (scratch), mirroring the v1 placement.
- **Provider-auth gate.** `go/pkg/mutations/supervision_provider_auth.go::runSuperviseProviderAuthGate`
  (37–73) is Codex-only; Claude is unsupported in `auto`/`required`. The new
  Claude projection gate is a distinct step, not a widening of this Codex gate.
- **Adapter detection.** `go/pkg/agentloop/mcpconfig.go::LaneAdapterName` (157–161)
  and `go/pkg/mutations/supervision_lane_config.go::adapterName` (66–75) →
  `"claude"`.
- **Lane env / no credential write today.** `go/pkg/mutations/supervision_env.go::supervisedLaneEnv`
  (38–49) and `providerAuthPreflightEnv` (66–75) build the launch env and inject
  `STRIATUM_MCP_TOKEN`; **nothing sets `CLAUDE_CONFIG_DIR` or writes a credential
  file today** — the projector adds the first daemon-side lane credential write.
  The allowlist must add `CLAUDE_CONFIG_DIR` only as a trusted selector (or fail
  closed).
- **Credential resolve / sample / parse (reuse).**
  `go/pkg/laneproviderauth/resolver.go::ResolveCredential` (50–98, Claude →
  `$CLAUDE_CONFIG_DIR/.credentials.json` else `$HOME/.claude/.credentials.json`,
  path only); `go/pkg/laneproviderauth/expiry.go::ParseExpiry`/`claudeExpiry`
  (37–118, reads `claudeAiOauth.expiresAt` ms / top-level `expiresAt`, **never
  reads token values**); `go/pkg/laneproviderauth/sampler.go::LaneFileReader`
  (87–106, `sudo -n -u <lane> -- env -i ... cat -- <path>`, **read-only — no
  atomic write helper exists**) and `SampleLaneCredential` (53–78).
- **Recovery (F1 anchor).** `go/pkg/mutations/recovery.go::HandleRecoveryAuto`
  (553) is the periodic daemon sweep; it calls
  `go/pkg/mutations/recovery_decision_tree.go::recoverStuckJobs` (704). Branches
  classify requeue vs transfer (1046–1141); `recordRecoveryAction` (394, called
  1406) increments `requeue_count`/`transfer_count` in `job_recovery_state`
  (`go/pkg/db/sql/0020_job_recovery_state.sql`); `readJobRecoveryBudget` (334).
  `markRecoveryEscalation` (418) flags exhaustion without incrementing.
- **Liveness.** `go/pkg/sessionliveness/liveness.go::Classify` (475) emits
  `StallDiscovery = "agent_mcp_discovery_stall"` (const line 52); it is a pure
  function with no DB access, so the provider-auth check must live in the recovery
  decision tree (which has `tx`), not inside `Classify`.
- **Heartbeat.** `go/pkg/mutations/supervision_control.go::refreshSupervisorHeartbeat`
  (1509–1538) is event-driven (packet delivery / rebridge / helper progress),
  coalesced at 30s; there is **no per-lane periodic tick** here — the daemon-owned
  periodic signal for decay is the recovery/reconcile sweep.
- **RFC 0162 telemetry (integrate, don't replace).** `go/pkg/metrics/render.go`
  (43–50) and `go/pkg/metrics/lane_auth.go` already emit
  `striatum_lane_cred_seconds_to_expiry`, `_resolver_mismatch`, `_sample_present`,
  `lane.auth_success`; `go/pkg/reads/doctor_lane_provider_auth.go::HandleDoctorLaneProviderAuth`
  (30–61) is Codex-only today. v3 adds Claude provider-auth dependency state and
  joins it to these existing surfaces.

The downstream build run needs source write scope outside this design workflow's
artifact-only lanes (flagged here per the implementation-envelope instruction;
these exceed the frozen design write_scope and must be granted to the build run):

- `go/pkg/laneproviderauth/` (new `claude_projector.go`, access-token extractor,
  atomic lane-file **writer**, B2 broker socket + helper)
- `go/pkg/mutations/supervision_provider_auth.go`, `supervision_control.go`,
  `supervision_env.go`
- `go/pkg/mutations/recovery_decision_tree.go`, `go/pkg/mutations/recovery.go`
- `go/pkg/reads/doctor_lane_provider_auth.go`, `go/pkg/rpc/error_catalog.go`
- `go/pkg/db/sql/` (new migration for `provider_auth_dependencies` +
  `provider_credential_custody_receipts`)
- `go/cmd/striatum-supervisor-helper/` (B2 `credential` subcommand)
- `docs/reference/command-authority-matrix.md`, `docs/reference/spec.md`,
  `docs/decisions/decision-log.md`, and any generated/guarded daemon-contract docs

---

## Implementation Spec

### 1. Access-token-only projector in `laneproviderauth`

Add `go/pkg/laneproviderauth/claude_projector.go` with fakeable filesystem,
clock, user-lookup, and reader/writer seams:

```text
ProjectClaudeAccessToken(ctx, params) -> ProjectionReceipt
```

Inputs (resolved by Striatum, never workflow-authored): `Provider=claude`,
`Kind=oauth`, `RepositoryID/RunID/SessionID/LaneID`, `RunAsUser` (lane OS user
after same-user collapse), `OperatorEnv` (trusted), `LaneLaunchEnv`,
`MinFreshness` (fixed V1 default `30m`).

Steps, in order:

1. **Resolve + read the operator source** under operator/daemon authority
   (`ResolveCredential` for the operator side; read as operator, never as lane).
   If untrustworthy, refuse `provider_credential_resolver_mismatch`.
2. **Extract access token + expiry into transient memory.** Parse the source
   JSON; take `claudeAiOauth.accessToken`, `expiresAt`, and non-secret
   account/scope fields. **Drop `refreshToken` — it is never read into a variable
   that outlives this function, never written to the lane, never persisted.**
   `ParseExpiry` supplies the expiry; `HasExpiry == false` is unverifiable and
   fails closed. Launch also fails when `expires_at <= now + MinFreshness`.
3. **Resolve the lane destination** from lane identity (same-user collapse →
   verify-only no-op; distinct lane user → lane home `.claude/.credentials.json`;
   or daemon-configured lane Claude config dir). A workflow `CLAUDE_CONFIG_DIR`
   is honored **only** if it matches the trusted destination; any other path
   fails closed `provider_credential_resolver_mismatch` (copying OAuth material
   to an arbitrary path is a privilege bridge).
4. **Write the access-token-only projection (B1)** via a net-new atomic
   temp-file → `chmod 0600` → `chown lane uid/gid` → fsync → rename helper (the
   first daemon-side lane-file *write*; today only `LaneFileReader` exists). The
   written JSON contains `accessToken` + `expiresAt` + non-secret fields and **no
   `refreshToken` key**. Verify the final file is regular, lane-owned, `0600`,
   and parses with a future-enough expiry.
5. **Record a redacted projection/custody receipt** (section 5).

A `ProviderCredentialGeneration` is a non-secret value: provider, kind,
source/destination selector, an **HMAC-SHA256 over the source bytes using the
daemon authority secret** (never a raw hash, never the bytes), `expires_at`,
file size/mode/owner/mtime, observed time. The source generation is observed
before and after the read; if it changes between observations the projector
retries once, then refuses `provider_credential_source_unstable` (closes the
rotation race where the operator refreshes mid-projection).

**B2 broker socket (named fallback).** If `TestClaudeCLIAcceptsAccessTokenOnlyCredential`
(below) shows the CLI rejects an access-token-only file, switch delivery to a
daemon-owned `AF_UNIX` socket: the lane's Claude settings get an injected
`apiKeyHelper`/credential-helper that calls `striatum-supervisor-helper credential
--provider claude`, which connects to the socket; the daemon verifies the peer
uid via `SO_PEERCRED` equals the lane OS user and returns only the current access
token. No file, no refresh token, no Striatum token on this channel. F2 is
discharged identically under B1 or B2.

### 2. Path and Ownership Rules (carried forward)

- Source path comes only from trusted daemon/operator env + operator home; a full
  operator home path is never persisted to events/metrics/doctor/dashboard/repo
  artifacts/errors; source must be a regular file (symlink source refused).
- Destination comes from lane OS identity + a trusted selector, never a
  workflow-arbitrary path; every parent component stays under the trusted lane
  credential dir after symlink evaluation; parent created `0700` lane-owned only
  for the lane-home default or a daemon-configured dir.
- Destination written through temp file → `0600` → chown lane uid/gid → fsync →
  rename; existing destination symlink / non-regular / wrong owner / wrong mode is
  a refusal unless overwritten through the safe temp path and the final file
  verifies regular lane-owned `0600`.

### 3. Spawn-time projection gate (carried forward; placement unchanged)

Integrate at the existing `supervise.start` authority point:

1. `HandleSuperviseStart` resolves `session_id`, `provider_auth_gate`, and
   `supervisionStartConfig` (line 97).
2. If `config.adapterName() == "claude"` and a distinct lane OS user is
   configured, run `runSuperviseClaudeCredentialGate` (which calls
   `ProjectClaudeAccessToken`) **before** scratch creation (104), session-bound
   token minting (158), supervisor row insert (165), and process launch (193).
3. The existing Codex preflight remains in `runSuperviseProviderAuthGate`.
4. Only after projection succeeds may the handler create scratch, mint/inject the
   Striatum session token, insert supervisor rows, and launch.

The invariant: Claude access-token projection + verification happen **before** a
real Claude process can enter MCP discovery and before recovery can spend a work
retry on it. `provider_auth_gate=off` does not bypass this; only
`provider_credential_projection=off` (default `auto`, documented unsafe, emits
`provider_credential.projection_bypassed`, marks dependency `disabled`, forwarded
by `run drive` only when explicitly requested, never implied by
`provider_auth_gate=off`) can.

Typed refusal vocabulary (launch-precondition failures, not lane-execution
failures): `provider_credential_projection_failed`,
`provider_credential_generation_stale`, `provider_credential_source_unstable`,
`provider_credential_expiry_too_near`, `provider_credential_owner_mode_invalid`,
`provider_credential_resolver_mismatch`, `provider_auth_reseed_required`.

### 4. Runtime-expiry circuit breaker (F1 / C2) and decay signal (C3)

**Recovery hook (C2).** In `recoverStuckJobs`
(`recovery_decision_tree.go:704`), for a Claude lane with stall class
`agent_mcp_discovery_stall`, after `readJobRecoveryBudget` and **before**
`recordRecoveryAction` (line 1406):

- Look up the lane's `provider_auth_dependencies` row and compute **current**
  seconds-to-expiry for the projected token (a DB-row timestamp comparison; if the
  row is stale, one bounded `SampleLaneCredential` re-read of the lane projection
  expiry — daemon-owned, never lane-authored). This is cheap (timestamp compare)
  and race-free (we classify, we do not use the token, so no TOCTOU on the
  token).
- If expired / within `MinFreshness` of expiry / unverifiable **now**: set
  dependency `reseed_required` (or `unverifiable`), emit one redacted
  `provider_auth.reseed_required` event, and route through
  `markRecoveryEscalation`-style debt handling that **does not** increment
  `requeue_count`/`transfer_count` and **does not** escalate `recovery_exhausted`
  for this cause. Re-project against a fresh operator generation; requeue only
  jobs blocked against the stale generation (generation ids in the receipt
  prevent a stale blocker clearing against the same bad source).
- If a job already has an open provider-auth blocker for the same dependency
  generation, subsequent sweeps are idempotent no-ops.

**Decay signal (C3).** The same periodic daemon-owned sweep
(`HandleRecoveryAuto`, `recovery.go:553`) evaluates each *running* Claude lane's
projected `expires_at` from its `provider_auth_dependencies` row. When a running
lane crosses the near-expiry lead it emits `provider_auth.expiry_warning` and
marks near-expiry debt **before** generic MCP-discovery recovery can classify the
lane. The signal is computed from daemon broker state only — never a lane
heartbeat claim or provider stdout/stderr — satisfying "do not trust
lane-authored claims." (For B2, the broker already knows each fetch's token
expiry, making decay state authoritative without any file re-read.)

This is the key #583 invariant: stale runtime Claude OAuth becomes typed
provider-auth readiness debt, not another `agent_mcp_discovery_stall` that burns
recovery budget and escalates `recovery_exhausted`.

### 5. Durable state and receipts (revised; access token never persisted)

Current-state table keyed by repository, provider, kind, lane user, destination
selector:

```text
striatumd.provider_auth_dependencies
  repository_id, provider, kind, lane_user, destination_selector
  state                         -- ready|hydrating|reseed_required|unverifiable|disabled
  source_selector, source_generation_id, destination_generation_id
  expires_at, min_freshness_seconds
  last_receipt_id, last_failure_class, last_failure_reason, updated_at
```

Append-only custody receipts (bounded retention, last ~100 per key or 30 days):

```text
striatumd.provider_credential_custody_receipts
  receipt_id, repository_id, run_id, session_id, lane_id, lane_user
  provider, kind, source_selector, destination_selector
  source_generation_id, destination_generation_id
  source_observed_at_before, source_observed_at_after
  projection_started_at, projection_completed_at
  expires_at, min_freshness_seconds
  destination_owner_ok, destination_mode_ok, destination_parse_ok
  refresh_token_absent_ok        -- NEW: asserts the lane projection carries no refresh token
  verifier_result                -- passed|source_missing|source_unstable|source_unparseable|destination_write_failed|destination_unparseable|expiry_too_near|owner_mode_invalid|resolver_mismatch
```

Compact run-history events: `provider_credential.projected`,
`provider_credential.projection_refused`, `provider_auth.reseed_required`,
`provider_auth.reseed_cleared`, `provider_auth.expiry_warning`,
`provider_credential.projection_bypassed`. Payloads carry ids, enum selectors,
provider, kind, lane user, expiry, failure class, receipt id — **no credential
bytes, token values, full private paths, or provider output**.

### 6. Error codes and operator remediation

Stable daemon errors: the typed refusal vocabulary in section 3 plus
`provider_auth_reseed_required`. Operator-facing remediation is exact and
private-safe:

```text
Claude provider credential is not fresh enough for lane launch. Re-authorize or
refresh Claude as the operator user, then retry the run. No lane process was
started.
```

Doctor/dashboard may show
`provider=claude lane_user=striatum-lane state=reseed_required
reason=provider_credential_expiry_too_near expires_at=<ts>
action=refresh_operator_claude_login_then_retry`, and must never show home
paths, raw JSON, access/refresh/id tokens, provider stdout/stderr, or
control-plane token material.

### 7. Redaction contract (carried forward; extended)

Persist allowed: closed enums (provider/kind/state/selector/failure class), lane
user/id, run/session/job ids, receipt id, HMAC generation ids (daemon authority
secret), expiry/observed timestamps, size/mode, owner/mode/parse/`refresh_token_absent`
booleans, safe remediation strings.

Forbidden outside transient process memory and the `0600` lane projection / B2
socket response: raw OAuth bytes; **refresh tokens (never read into the projector
at all)**; access tokens, id tokens, account ids; full private operator/lane
paths; provider stdout/stderr, transcript, model output; daemon bootstrap admin
token, runtime `client-token`, session-bound/capability tokens, DSNs; raw
unsalted hashes of credential bytes. A table-driven redaction test serializes
every receipt/event/error/doctor/dashboard/metric payload and searches for
fixture secrets and private-path substrings, and asserts the lane projection file
contains `accessToken` but **no `refreshToken`**.

---

## Falsifiable assertions and their tests

Each binding constraint is stated as an assertion that a single named test
refutes if false.

| # | Falsifiable assertion | Refuting observation | Test |
|---|---|---|---|
| C1 | A lane never holds a refresh token by any route (file, env, broker, source read). | Any lane-readable surface yields a `refreshToken`, or a lane-side refresh rotates the operator family. | `TestLaneNeverReceivesRefreshToken`, `TestConcurrentLanesNoRefreshTokenDesync`, `TestSubsequentLaneAfterOperatorRefresh` |
| C1 | The operator source stays valid across concurrent + subsequent lane runs. | Operator source generation changes due to a lane action; a later lane projects from an invalidated source. | `TestConcurrentLanesNoRefreshTokenDesync`, `TestSubsequentLaneAfterOperatorRefresh` |
| C2 | Runtime-expired Claude credential becomes `reseed_required` before any generic counter increments. | `requeue_count`/`transfer_count` increments, or `recovery_exhausted` escalates, for a runtime-expiry cause. | `TestRecoveryClassifiesRuntimeExpiryBeforeGenericBudget` |
| C3 | A running lane's near-expiry is reported from daemon state before generic recovery fires. | The near-expiry signal is missing, or derives from a lane heartbeat claim / provider stdout. | `TestDecaySignalFromDaemonStateBeforeRecovery` |
| C4 | No raw OAuth/refresh/access/id material, private path, provider output, or control-plane token reaches any durable surface. | A serialized payload or the projection file contains a forbidden substring. | `TestProjectionReceiptRedaction` |
| carry | The spawn-time gate refuses a stale/expired/unparseable/drifted credential before supervisor rows exist. | A real Claude process starts, then wedges in MCP discovery because projection failed. | `TestProjectionRefusesBeforeSupervisorRows` |
| carry | `provider_auth_gate=off` does not bypass projection. | `provider_auth_gate=off` launches Claude with a stale projection. | `TestProviderAuthGateOffDoesNotBypassProjection` |
| carry | Provider OAuth stays separate from Striatum control-plane tokens. | Any path copies/stores/exposes a Striatum control-plane token while projecting Claude OAuth, or the B2 socket accepts a Striatum token as identity. | `TestTrustBoundaryNoControlPlaneTokenToLane` |
| carry | Resolution is not a privilege bridge. | A workflow `CLAUDE_CONFIG_DIR` causes the daemon to write OAuth material to an arbitrary path. | `TestProjectionResolverRejectsWorkflowPath` |

---

## Required tests (named matrix — maps to modules/transitions)

- `TestLaneNeverReceivesRefreshToken` — projection file + env + (B2) socket
  response contain no `refreshToken`; `claude_projector.go`,
  `supervision_env.go`.
- `TestConcurrentLanesNoRefreshTokenDesync` (C1/C4) — two lanes share operator
  generation G; each gets an access-token-only projection; simulate each lane CLI
  attempting refresh → no refresh token → no rotation; assert (a) neither lane
  file has a refresh token, (b) operator source generation unchanged after both,
  (c) both projections derive from G and stay valid. `claude_projector.go`.
- `TestSubsequentLaneAfterOperatorRefresh` (C1) — lane 1 from G1; operator source
  rotates to G2 (operator-side authority); lane 2 projects current G2; assert lane
  2 gets a G2 access token (not stale G1), lane 1 never invalidated G2, operator
  source valid throughout. `claude_projector.go`, `provider_auth_dependencies`.
- `TestRecoveryClassifiesRuntimeExpiryBeforeGenericBudget` (F1/C2) — launch fresh
  (35m lead), 45m local work, projected token expires before first Claude action →
  `agent_mcp_discovery_stall`; recovery sets `reseed_required`, emits one event,
  does **not** increment `requeue_count`/`transfer_count`, does not escalate
  `recovery_exhausted`; only re-projection against a fresh generation requeues.
  `recovery_decision_tree.go:704/1406`, `recovery.go:553`.
- `TestDecaySignalFromDaemonStateBeforeRecovery` (C3) — running lane crosses
  near-expiry lead; sweep emits `provider_auth.expiry_warning` / near-expiry debt
  from `provider_auth_dependencies`, before generic MCP-discovery recovery; assert
  signal source is daemon state, not lane heartbeat/provider stdout.
  `recovery.go:553`.
- `TestProjectionReceiptRedaction` (C4) — serialize every
  receipt/event/error/doctor/dashboard/metric payload; assert absence of fixture
  refresh/access/id tokens, raw JSON, raw hashes, private-path substrings, and
  control-plane tokens; assert lane file has `accessToken`, no `refreshToken`.
- `TestProjectionRefusesBeforeSupervisorRows` (carry) — expired/missing/
  unparseable/expiry-too-near source returns a provider credential error and **no**
  scratch, token mint, supervisor rows, helper, tmux, or Claude process.
  `supervision_control.go:83–193`.
- `TestProjectionSourceRotationRace` (carry) — source generation A before read, B
  after → retry once; a second drift refuses `provider_credential_source_unstable`.
- `TestProjectionDestinationOwnerMode` / `TestProjectionSymlinkEscape` (carry) —
  final file must be regular, lane-owned, `0600`; symlinked parent/path escaping
  the trusted lane dir refuses.
- `TestProviderAuthGateOffDoesNotBypassProjection` (carry) — `provider_auth_gate=off`
  still projects; only `provider_credential_projection=off` skips and records a
  `disabled` dependency.
- `TestTrustBoundaryNoControlPlaneTokenToLane` (carry) — projector/receipts/B2
  socket never read or vend `STRIATUM_MCP_TOKEN`, runtime `client-token`, admin
  tokens, DSNs, or capability tokens; B2 identity is `SO_PEERCRED` uid only.
- `TestProjectionResolverRejectsWorkflowPath` (carry) — workflow `CLAUDE_CONFIG_DIR`
  outside the trusted lane dir fails closed and writes nothing.
- `TestClaudeCLIAcceptsAccessTokenOnlyCredential` (B1-vs-B2 decider, P0 spike) —
  the Claude CLI operates from an access-token-only `.credentials.json` while the
  token is unexpired; failure selects the B2 broker-socket delivery. (F2 holds
  under either outcome.)

---

## TDD build order

1. **P0 fixtures + spike.** Failing unit tests on a fake projector filesystem:
   happy path, stale/expired/unparseable source refusal, source-rotation race,
   wrong owner/mode, symlink escape, expiry-inside-30m, redaction, and the
   `refresh_token_absent` assertion. Run `TestClaudeCLIAcceptsAccessTokenOnlyCredential`
   to settle B1-vs-B2.
2. Migration + data-access tests for `provider_auth_dependencies` and custody
   receipts (upsert, idempotent same-generation refusal, retention, payload
   shape).
3. Implement Claude source resolve + access-token extraction + the atomic
   lane-file **writer** in `laneproviderauth`, reusing `ResolveCredential`,
   `ParseExpiry`, `LaneFileReader` patterns; add `CLAUDE_CONFIG_DIR` to the safe
   env path only as a trusted selector. (If B2: add the broker socket + helper.)
4. Wire `HandleSuperviseStart` so a stale/unverifiable Claude credential refuses
   before scratch, token mint, supervisor rows, helper/tmux, or Claude process.
5. Extend `run drive`, auto-spawn, doctor, dashboard, error catalog so refusal is
   visible and not looped as ordinary MCP-discovery failure.
6. Teach `recoverStuckJobs` to consult provider-auth dependency state and run the
   current-freshness check before `recordRecoveryAction`; add the
   `reseed_required` idempotency and decay-signal tests.
7. Docs: `command-authority-matrix.md`, `spec.md`, CLI/RPC docs, decision-log
   entries for the new daemon state, error codes, and B2 socket if adopted.

---

## Load-bearing claims

| Claim | Evidence that supports it | Observation that refutes it |
|---|---|---|
| Lanes never hold a refresh token. | Projection file/env/B2-socket carry only an access token; `refresh_token_absent_ok` asserted; concurrent/subsequent RTR tests pass. | Any lane-readable surface yields a `refreshToken`, or a lane-side refresh rotates the operator family. |
| The operator source cannot be desynchronized by a lane. | Refresh authority is operator-side and single-writer; lanes have no refresh token and no read path to the source; RTR tests keep the source valid. | A concurrent or subsequent lane invalidates the operator source or the operator CLI login. |
| Runtime expiry is classified before generic budget burn. | Recovery computes current expiry before `recordRecoveryAction`; expired → `reseed_required`, no counter increment. | A runtime-expiry stall increments `requeue_count`/`transfer_count` or escalates `recovery_exhausted`. |
| Decay is detected from daemon state. | Sweep reads projected `expires_at` from `provider_auth_dependencies`; emits `expiry_warning` before generic recovery. | The decay signal is absent or sourced from a lane heartbeat claim / provider stdout. |
| Projection runs before real provider launch. | Stale source → `supervise.start` error with no supervisor rows/scratch/token mint/process. | A real Claude process starts and later wedges in MCP discovery because projection failed. |
| Resolution is not a privilege bridge. | Source = daemon/operator-owned; destination = lane-home/daemon-configured; workflow `CLAUDE_CONFIG_DIR` escape fails closed without writing. | A workflow `CLAUDE_CONFIG_DIR` causes the daemon to write OAuth bytes to an arbitrary path. |
| Custody records are useful but private-safe. | Doctor/dashboard/recovery join dependency state to receipt id + failure class; redaction tests prove no raw credential/token/private path emitted. | An emitted payload contains OAuth bytes, token substrings, raw hashes, or private path strings. |
| `provider_auth_gate=off` does not bypass the fix. | A test with `provider_auth_gate=off` still projects and refuses stale credentials. | `provider_auth_gate=off` launches Claude with a stale projection. |
| Striatum control-plane credentials stay separate. | Projector inputs/outputs/receipts never read or copy `STRIATUM_MCP_TOKEN`, `client-token`, admin tokens, DSNs; B2 identity is `SO_PEERCRED`. | Any path copies/stores/exposes a Striatum control-plane token while projecting Claude OAuth. |
| The daemon adds no OAuth client. | No `net/http`/`oauth2` token-exchange in `laneproviderauth`; refresh stays operator-side. | The daemon performs an Anthropic OAuth refresh and becomes a second refresh-token writer. |

---

## Minimal closure scope for GH #583

Closure requires code on `origin/main` plus verifier evidence that:

- a Claude lane receives an access-token-only projection (no refresh token) and a
  stale/expired projection is refused or re-projected before a real Claude process
  starts;
- a long dogfood spanning more than the Claude access-token TTL does not wedge new
  or running Claude lanes on credential-caused `agent_mcp_discovery_stall`, and a
  mid-session expiry becomes `reseed_required` without burning generic recovery
  budget;
- concurrent and subsequent lane launches never desynchronize or invalidate the
  operator source credential family;
- no manual `cp`/`chown`/`chmod`/escalation resolution is needed for the #583
  expiry class;
- RFC 0162 telemetry still reports expiry/absence, but launch and runtime
  correctness do not depend on the timer or metrics path.

An optional host timer in `halbritt/proximal` may pre-warm the **operator source**
freshness (refresh as the operator); it must not write lane credential files and
is not part of the correctness boundary. If the timer is absent or late, the
spawn-time projection gate and the runtime circuit breaker still hold or refuse
synchronously.
