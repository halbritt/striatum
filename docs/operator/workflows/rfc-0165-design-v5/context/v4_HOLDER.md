# RFC 0165 Holder Proposal (v4): Same-User Fail-Closed + Daemon-Owned Recovery Freshness for the Access-Token-Only Claude Projection
author: holder-author-001

This is the **v4 revision** of the RFC 0165 implementation SPEC (Claude provider
credential freshness + spawn-time hydration; GH #583). It is a **surgical
revision of the v3 HOLDER**, not a rewrite. The v3 access-token-only projection
spine for **distinct-UID** lanes was adjudicated *sound and must be carried
forward*; v4 discharges the three binding constraints the v3 adjudicator routed
to the operator and regresses nothing the v1+v3 gate cleared.

The three discharges are localized:

- **C1** — recovery's runtime-freshness classification now takes its **positive**
  freshness value **only** from daemon-owned state; the lane-owned projection is
  demoted to a downgrade-only (negative) signal, and a stale / missing /
  internally inconsistent daemon row **fails closed** to a typed
  reseed-required, never a generic MCP-discovery retry and never a green
  inference.
- **C2** — a Claude OAuth self-driving lane in **same-user** mode
  (`config.RunAsUser == ""` or resolving to the daemon/operator uid) is refused
  at a **typed launch-precondition gate before any side effect**, instead of the
  v3 verify-only no-op.
- **C3** — because same-user mode now fails closed, no lane in that shape ever
  reaches the rotating refresh token; the distinct-UID projection already denies
  it; an **exhaustive same-user + distinct-UID no-refresh-token test** scans
  every lane-readable credential surface named by the launch environment.

---

## Addressing the v3 constraints (auditable map)

The v3 cycle-1 ledger
(`docs/operator/artifacts/rfc-0165-design-v3/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md`)
returned `needs_revision` with three binding constraints, restated by the v4 SEED
as C1/C2/C3. Here is exactly how each is discharged and which v3 decisions are
carried forward unregressed. (The SEED's C1↔C3 map onto the v3 ledger's
`C2-RUNTIME-FRESHNESS-DAEMON-ONLY-POSITIVE-AUTHORITY`,
`C1-NO-LANE-RAW-REFRESH-TOKEN-CUSTODY`, and
`C3-TEST-MATRIX-COVERS-SAME-USER-AND-LANE-AUTHORED-FRESHNESS` respectively; this
SPEC uses the SEED's C1/C2/C3 labels as primary.)

### C1 — daemon-owned state is the ONLY positive freshness authority in recovery → **RESOLVED**

**v3 defect (landed unrebutted).** v3's recovery hook permitted "one bounded
`SampleLaneCredential` re-read of the lane projection expiry" when the daemon
`provider_auth_dependencies` row was stale, and labelled that "daemon-owned,
never lane-authored." Current source refutes the label:
`go/pkg/laneproviderauth/sampler.go::LaneFileReader` (87-106) reads the file **as
the lane user** and `SampleLaneCredential` (53-78) parses only
`HasExpiry`/`ExpiresAt` from the JSON with **no** daemon MAC, receipt id, or
generation check; `go/pkg/laneproviderauth/expiry.go` (102-109) accepts
`claudeAiOauth.expiresAt` / top-level `expiresAt` straight from that payload. The
B1 projection is a lane-owned `0600` file, so a lane process (or stale orphan, or
provider rewrite) can set `expiresAt` to a future value **with no refresh token**,
upgrading a stale daemon row to "fresh" and falling through to
`recordRecoveryAction`, burning `requeue_count`/`transfer_count` for a runtime
provider-auth cause.

**v4 fix (structural change to the recovery decision).** The recovery
runtime-freshness classification reads its **positive** freshness value **only**
from the daemon-owned `provider_auth_dependencies` row, inside the recovery
transaction. A lane-file re-sample is **downgrade-only**: it may move a
classification from fresh → not-fresh, and may **never** upgrade a stale /
missing / inconsistent daemon row to fresh. Concretely (section 5):

1. **Positive freshness predicate is daemon-owned and total.** A Claude
   `agent_mcp_discovery_stall` is treated as a *non-provider-auth* (generic) cause
   **only if** the daemon row positively proves freshness: a row exists for the
   key; `state == ready`; `expires_at > now + MinFreshness`; `last_receipt_id`
   resolves to a custody receipt with `verifier_result == passed` whose
   `source_generation_id`/`destination_generation_id` match the row; and
   `source_generation_id` still matches the **daemon-re-observed** current
   operator-source generation. Every column in this predicate is written **only**
   by the daemon's own projector — the lane has no write path to
   `provider_auth_dependencies` or the receipts table.
2. **Stale / missing / internally inconsistent → fail closed.** If the row is
   absent, `state != ready`, expired / within `MinFreshness`, the receipt is
   missing or mismatched, or the source generation has rotated, recovery sets
   dependency `reseed_required` (or `unverifiable`), emits **one** redacted
   `provider_auth.reseed_required` event, and routes through reseed-debt handling
   that **does not** increment `requeue_count`/`transfer_count` and **does not**
   escalate `recovery_exhausted`. It is **never** a generic MCP-discovery retry
   and **never** a green/ready inference from a missing row. (Justification: every
   successful Claude launch writes a fresh `ready` row at the gate, so a
   Claude lane that reached MCP discovery without a positively-fresh row is itself
   evidence the projection freshness is gone — fail-closed-to-reseed is
   well-founded, and a possibly-unnecessary reseed prompt is strictly cheaper than
   the budget-burn leak it replaces.)
3. **Race-free read.** The row read and the recovery-action record happen in the
   **same** `tx` held by `recoverStuckJobs`
   (`go/pkg/mutations/recovery_decision_tree.go:704`); recovery only **classifies**
   and never **uses** the token, so there is no TOCTOU on token material, and the
   lane cannot flip the row green between read and decision because it can never
   write the row at all.

This discharges **C1**: daemon-owned state is the only positive freshness
authority; the lane-side file/socket is never the freshness oracle.

### C2 — explicit same-user decision (fail closed) → **RESOLVED**

**v3 defect (landed unrebutted).** v3 ran the Claude credential gate **only when a
distinct lane OS user is configured** and listed same-user collapse as a
supported "verify-only no-op." Current source keeps same-user mode live:
`go/pkg/mutations/supervision_env.go::configuredLaneRunAsUser` (228-238) collapses
an unset or same-as-daemon `STRIATUM_LANE_OS_USER` to `RunAsUser == ""`, and
`supervisedLaneCommandContext` (259-272) then execs the lane **directly with no
`sudo -u` identity split**. In that mode the lane *is* the operator user.

**v4 fix.** A new **typed launch-precondition gate** refuses Claude OAuth
self-driving lanes whenever the resolved lane identity is the operator/daemon
identity (section 3). The gate runs for **all** Claude lanes — it no longer skips
same-user; it **refuses** it — and the refusal is emitted **before** scratch
creation, session-token minting, supervisor rows, helper/tmux, or any process
launch, with the typed error `provider_credential_same_user_unsupported`. The
condition is `config.adapterName() == "claude"` **and** (`config.RunAsUser == ""`
**or** `RunAsUser` resolves to the daemon uid). The check is by **resolved uid**,
not only the username string, so a distinct username that aliases the daemon uid
(shared uid, `user\name` suffix forms already handled by `sameOSUsername`, etc.)
also fails closed.

This discharges **C2**: no silent same-user credential sharing; same-user is a
typed, side-effect-free launch refusal.

### C3 — no raw refresh-token custody by ANY route → **RESOLVED**

**Why same-user must be UNSUPPORTED, not brokered (the load-bearing argument).**
A broker / access-token projection cannot protect a same-user lane, because the
lane shares the operator's uid **and** home. The operator's real
`~/.claude/.credentials.json` (carrying the rotating `refreshToken`) is on disk
and readable by **any** process running as that uid; a self-driving lane runs
arbitrary commands and can simply read it, regardless of what `apiKeyHelper` or
`CLAUDE_CONFIG_DIR` the daemon injects. No in-uid boundary can hide a file from a
process running as that uid. Therefore the **only** sound C3 answer for same-user
is to **fail closed** (the C2 gate), and the brokered access-token projection is
reserved for the **distinct-UID** shape where a filesystem boundary actually
exists. v4 adopts exactly this: **same-user Claude OAuth self-driving is
unsupported**; distinct-UID lanes keep the v3 access-token-only projection (B1
file / B2 `SO_PEERCRED` socket) that never carries a refresh token.

**Operator consequence (flagged, not hidden).** Because same-user Claude lanes
now fail closed, the operator remediation "unset `STRIATUM_LANE_OS_USER`"
(`command-authority-matrix.md:426`) is **withdrawn for Claude OAuth lanes**; the
correct remediation is to configure a **distinct lane OS user** so the
distinct-UID projection path applies. The downstream build must update
`spec.md`, `command-authority-matrix.md`, and the decision log accordingly
(flagged in *Downstream write scope*).

**Required exhaustive test.** `TestSameUserClaudeLaneRefusedNoCredentialSurface`
plus `TestLaneNeverReceivesRefreshTokenAllSurfaces` assert that (a) in same-user
mode `supervise.start` refuses before any Claude process starts, so **no** lane
surface is ever readable, and (b) for distinct-UID lanes, **every lane-readable
credential surface named by the launch environment** — the resolver-proven
destination file, `$CLAUDE_CONFIG_DIR/.credentials.json`,
`$HOME/.claude/.credentials.json`, every credential-bearing env entry, and the B2
socket response — contains an `accessToken` but **no `refreshToken`** (section 9).

This discharges **C3**: the lane cannot reach the rotating refresh token by any
route — same-user (refused), distinct-UID source read (no read path), projection
file/env/socket (access-token-only), or recovery re-sample (downgrade-only).

### Carry-forwards INTACT (v1+v3 got these right — not regressed)

- **Access-token-only projector** in `laneproviderauth` (B1 lane-owned `0600`
  file with no `refreshToken` key, or B2 `SO_PEERCRED` broker socket) — the
  distinct-UID custody model the adjudicator accepted as sound, unchanged.
- **Path / ownership rules** and the **atomic temp→chmod 0600→chown→fsync→rename**
  lane-file writer — unchanged (section 4).
- **Spawn-time projection gate** and its placement before scratch / token-mint /
  supervisor rows / process — unchanged (section 3); the only change is that the
  gate now *also* refuses same-user instead of skipping it.
- **F1 runtime-expiry circuit breaker + daemon-owned decay signal** — preserved;
  hardened so its *positive* freshness value is daemon-owned only (section 5).
- **Durable state + receipts**; the **access token is never persisted**, the
  **refresh token is never read into the projector at all** (sections 6-9).
- **Redaction contract** — no raw OAuth material, private paths, provider output,
  or control-plane tokens in DB rows, repo artifacts, metrics, events, or doctor
  (section 8).
- **RFC 0096 / #135 / #296 trust boundary** and `refresh_token_absent` — lanes
  authenticate to Striatum with their own session-bound capability token, never
  daemon/admin tokens; B2 identity is `SO_PEERCRED` uid only.
- **`provider_auth_gate=off` cannot bypass** projection; only the separate
  documented-unsafe `provider_credential_projection=off` can.

---

## Relationship to RFC 0169 (kept separate; v4 stands alone)

RFC 0169 is the **provider-agnostic lane credential-readiness spine** — the
uniform refuse-closed readiness contract every provider adapter must satisfy.
RFC 0165 v4 is **Claude-specific**: the Claude assurance class and the
daemon-broker custody story (no raw refresh token to the lane). The seam:

- 0169 defines the cross-provider readiness *contract* (a launch must refuse
  closed when an adapter cannot prove credential readiness, and recovery must
  classify provider-auth debt distinctly from generic stalls).
- 0165 v4 is the Claude *implementation* of that contract's deep custody half —
  the same-user fail-closed precondition (C2/C3), the access-token-only
  projection (carry-forward), and the daemon-owned recovery freshness authority
  (C1).

v4 does **not** defer C1/C2/C3 to 0169: every mechanism here is fully specified
against current Claude source and ships on its own. When 0169 lands, the Claude
gate/projector/recovery hook described here become 0169's Claude adapter
instances without re-opening these constraints.

---

## Claim

Claude credential freshness must be a synchronous launch invariant **and** a
runtime-classified, daemon-owned dependency, delivered to **distinct-UID** lanes
as an **access-token-only projection** that never carries a refresh token, and
**refused before any side effect** for same-user lanes that would otherwise share
the operator's raw credential. Before any real Claude lane process starts the
daemon (a) refuses same-user Claude OAuth lanes with a typed launch-precondition
error, or (b) projects the operator's *current* access token into the distinct
lane (B1 file / B2 broker socket), verifies freshness, persists a redacted
custody receipt, and refuses launch when freshness cannot be proven. While a lane
runs and during recovery, **daemon-owned state is the only positive freshness
authority**: a stale / missing / inconsistent dependency row fails closed to
typed reseed-required without burning generic recovery budget, and a lane-owned
file may only *downgrade* a classification, never upgrade it. Refresh authority is
held in exactly one place — the operator-side credential owner; no lane can
receive, read, or exercise a refresh token by any route.

This is narrower than a full provider-auth broker protocol: it fixes #583 by
denying lanes the one piece of material (the refresh token) that makes the
local-file model unsafe, closing the same-user route entirely, and making the
recovery freshness decision unforgeable by a lane.

---

## Non-goal: daemon-side OAuth refresh (carry forward, unchanged)

The daemon does **not** add an Anthropic OAuth client and does **not** perform
refresh-token rotation. Two load-bearing reasons:

- It would make the daemon a *second* refresh-token writer racing the operator's
  own `claude` CLI — recreating the exact desync C1/F2 condemn, between daemon and
  operator-CLI instead of between lanes.
- No daemon OAuth client exists today (no `net/http`/`oauth2` token exchange in
  `go/pkg/laneproviderauth`); building one is a large, network-facing,
  out-of-scope lift the RFC's own non-goals forbid.

Refresh authority therefore stays operator-side (status quo of *who refreshes*);
lanes are insulated from it. The optional host pre-warm timer's role is bounded to
keeping the **operator source** fresh (refresh **as the operator**); it must
**not** copy whole credentials into lane homes — lane credential material is
produced *only* by the daemon projector.

---

## Current Source Anchors (verified against branch `striatum/rfc-0165-design-v4`)

- **Launch path / gate placement.**
  `go/pkg/mutations/supervision_control.go::HandleSuperviseStart` (83) orders:
  `loadSupervisionStartConfig` (97) → `runSuperviseProviderAuthGate` (101) →
  scratch `MkdirAll` (112-115) → tx: `mintSessionBoundToken` (158) →
  `insertStartingsSupervisorRowsWithCleanError` (165) → `supervisionLaunch` /
  `launchSupervisedProcess` (193). The new Claude **same-user precondition gate
  and projection gate insert after line 101 and before line 112** — before every
  side effect.
- **Same-user collapse (the C2 attack surface).**
  `go/pkg/mutations/supervision_env.go::configuredLaneRunAsUser` (228-238) returns
  `""` when `STRIATUM_LANE_OS_USER` is unset **or** `sameOSUsername(laneUser,
  daemonUser)`; `supervisedLaneCommandContext` (259-272) execs **directly with no
  `sudo -u` split** when `runAsUser == ""`. `config.RunAsUser` reaching
  `HandleSuperviseStart` is this **post-collapse** value (the Codex gate already
  branches on `config.RunAsUser == ""` at `supervision_provider_auth.go:52`), so
  the C2 condition is `config.RunAsUser == ""` plus a uid-equality backstop.
- **Provider-auth gate (Codex-only today).**
  `go/pkg/mutations/supervision_provider_auth.go::runSuperviseProviderAuthGate`
  (37-73) — Claude is unsupported in `auto`/`required`; the new Claude gate is a
  distinct step, not a widening of this Codex gate.
- **Credential resolve / sample / parse (reuse, read-only today).**
  `go/pkg/laneproviderauth/resolver.go::ResolveCredential` (57-98): Claude →
  `$CLAUDE_CONFIG_DIR/.credentials.json` else `$HOME/.claude/.credentials.json`,
  path only, fail-closed `ErrResolverMismatch`.
  `go/pkg/laneproviderauth/expiry.go` (102-109) reads `claudeAiOauth.expiresAt` ms
  / top-level `expiresAt`, **never token values**.
  `go/pkg/laneproviderauth/sampler.go::SampleLaneCredential` (53-78) +
  `LaneFileReader` (87-106): reads **as the lane user**
  (`sudo -n -u <user> env -i … cat`), parses a **lane-forgeable** `expiresAt`, has
  **no atomic write helper** — this is exactly why the lane sample can only be a
  downgrade signal, never a positive freshness authority (C1).
- **Recovery (C1 anchors).**
  `go/pkg/mutations/recovery.go::HandleRecoveryAuto` is the periodic daemon sweep
  that calls
  `go/pkg/mutations/recovery_decision_tree.go::recoverStuckJobs` (704);
  `readJobRecoveryBudget` (334), `recordRecoveryAction` (394) increments
  `requeue_count`/`transfer_count`, `markRecoveryEscalation` (418) flags
  exhaustion without incrementing. The provider-auth freshness branch runs inside
  `recoverStuckJobs`'s `tx`, **before** `recordRecoveryAction`, for a Claude lane
  whose stall class is `agent_mcp_discovery_stall`.
- **Liveness.** `go/pkg/sessionliveness/liveness.go` emits
  `StallDiscovery = "agent_mcp_discovery_stall"` (const line 52); `Classify` is a
  pure function with no DB access, so the provider-auth check lives in the recovery
  decision tree (which holds `tx`), not in `Classify`.
- **Net-new (none exist on this branch — confirmed by grep).**
  `runSuperviseClaudeCredentialGate`, `ProjectClaudeAccessToken`,
  `provider_credential_same_user_*`, and `provider_auth_dependencies` are all
  net-new; no Claude credential gate exists yet.

### Downstream write scope (exceeds this design run's artifact-only lanes; grant to `rfc-0165-build`)

- `go/pkg/laneproviderauth/` — new `claude_projector.go` (access-token extractor,
  atomic lane-file **writer**), same-user uid-resolution helper, B2 broker socket
  + helper.
- `go/pkg/mutations/supervision_provider_auth.go`, `supervision_control.go`,
  `supervision_env.go` — the same-user precondition gate + projection gate.
- `go/pkg/mutations/recovery_decision_tree.go`, `go/pkg/mutations/recovery.go` —
  daemon-owned freshness branch + decay sweep.
- `go/pkg/reads/doctor_lane_provider_auth.go`, `go/pkg/rpc/error_catalog.go`.
- `go/pkg/db/sql/` — migration for `provider_auth_dependencies` +
  `provider_credential_custody_receipts`.
- `go/cmd/striatum-supervisor-helper/` — B2 `credential` subcommand.
- `docs/reference/command-authority-matrix.md` (withdraw the unset-lane-user
  remediation for Claude OAuth), `docs/reference/spec.md`,
  `docs/decisions/decision-log.md`, and any generated/guarded daemon-contract docs.

---

## Implementation Spec

### 1. Access-token-only projector in `laneproviderauth` (distinct-UID only)

Add `go/pkg/laneproviderauth/claude_projector.go` with fakeable filesystem,
clock, user-lookup, and reader/writer seams:

```text
ProjectClaudeAccessToken(ctx, params) -> ProjectionReceipt
```

Inputs (resolved by Striatum, never workflow-authored): `Provider=claude`,
`Kind=oauth`, `RepositoryID/RunID/SessionID/LaneID`, `RunAsUser` (the **distinct**
lane OS user — the same-user gate in section 3 has already refused empty/operator
identity, so the projector is **never** invoked for a same-user lane),
`OperatorEnv` (trusted), `LaneLaunchEnv`, `MinFreshness` (fixed V1 default `30m`).

Steps, in order:

1. **Resolve + read the operator source** under operator/daemon authority
   (`ResolveCredential` for the operator side; read as operator, never as lane).
   Untrustworthy → refuse `provider_credential_resolver_mismatch`.
2. **Extract access token + expiry into transient memory.** Parse the source
   JSON; take `claudeAiOauth.accessToken`, `expiresAt`, and non-secret
   account/scope fields. **Drop `refreshToken` — never read it into a variable
   that outlives this function, never written to the lane, never persisted.**
   `ParseExpiry` supplies expiry; `HasExpiry == false` is unverifiable and fails
   closed; launch also fails when `expires_at <= now + MinFreshness`.
3. **Resolve the lane destination** from the *distinct* lane identity (lane home
   `.claude/.credentials.json`, or a daemon-configured lane Claude config dir). A
   workflow `CLAUDE_CONFIG_DIR` is honored **only** if it matches the trusted
   destination; any other path fails closed `provider_credential_resolver_mismatch`
   (copying OAuth material to an arbitrary path is a privilege bridge). The v3
   "same-user verify-only no-op" branch is **deleted** — same-user never reaches
   here.
4. **Write the access-token-only projection (B1)** via the net-new atomic
   temp-file → `chmod 0600` → `chown lane uid/gid` → fsync → rename helper. The
   written JSON contains `accessToken` + `expiresAt` + non-secret fields and **no
   `refreshToken` key**. Verify the final file is regular, lane-owned, `0600`, and
   parses with a future-enough expiry.
5. **Record a redacted projection/custody receipt** (section 6) and **upsert the
   `provider_auth_dependencies` row** to `state=ready` with the projected
   `expires_at`, `source_generation_id`, `destination_generation_id`, and
   `last_receipt_id` — this row is the daemon-owned freshness authority recovery
   later reads (section 5).

A `ProviderCredentialGeneration` is a non-secret value: provider, kind,
source/destination selector, an **HMAC-SHA256 over the source bytes using the
daemon authority secret** (never a raw hash, never the bytes), `expires_at`,
file size/mode/owner/mtime, observed time. The source generation is observed
before and after the read; a change between observations retries once then
refuses `provider_credential_source_unstable` (closes the operator-rotation race).

**B2 broker socket (named fallback).** If
`TestClaudeCLIAcceptsAccessTokenOnlyCredential` shows the CLI rejects an
access-token-only file, switch delivery to a daemon-owned `AF_UNIX` socket: the
lane's Claude settings get an injected `apiKeyHelper`/credential-helper that calls
`striatum-supervisor-helper credential --provider claude`, the daemon verifies the
peer uid via `SO_PEERCRED` equals the **distinct** lane OS user, and returns only
the current access token. No file, no refresh token, no Striatum token on this
channel. The no-refresh-token guarantee holds identically under B1 or B2, and the
B2 broker's per-fetch expiry feeds the same daemon-owned dependency row (section
5), so even the decay signal needs no lane-file read.

### 2. Path and ownership rules (carried forward, unchanged)

- Source path comes only from trusted daemon/operator env + operator home; the
  full operator home path is never persisted to events/metrics/doctor/dashboard/
  artifacts/errors; source must be a regular file (symlink source refused).
- Destination comes from the distinct lane OS identity + a trusted selector, never
  a workflow-arbitrary path; every parent component stays under the trusted lane
  credential dir after symlink evaluation; parent created `0700` lane-owned only
  for the lane-home default or a daemon-configured dir.
- Destination written through temp file → `0600` → chown lane uid/gid → fsync →
  rename; an existing destination symlink / non-regular / wrong owner / wrong mode
  is refused unless overwritten through the safe temp path with the final file
  verifying regular, lane-owned, `0600`.

### 3. Launch precondition gates (same-user fail-closed [C2/C3] + spawn-time projection)

Integrate at the existing `supervise.start` authority point, **after**
`runSuperviseProviderAuthGate` (101) and **before** scratch (112), so both gates
precede every side effect (scratch, FIFO/ACL, token mint, supervisor rows,
helper/tmux, process):

1. `HandleSuperviseStart` resolves `session_id`, `provider_auth_gate`, and
   `supervisionStartConfig` (line 97).
2. **Same-user precondition gate (C2/C3) — runs for every Claude lane.** If
   `config.adapterName() == "claude"` **and** the resolved lane identity is the
   operator/daemon identity — i.e. `strings.TrimSpace(config.RunAsUser) == ""`
   **or** `RunAsUser` resolves (via `user.Lookup` → uid) to the daemon uid — refuse
   with the typed launch-precondition error
   `provider_credential_same_user_unsupported`. The refusal happens here, before
   scratch/token-mint/rows/process; **no** `ProjectClaudeAccessToken` call, **no**
   lane-file write, **no** Claude process. Remediation string: *configure a
   distinct lane OS user for Claude OAuth lanes* (section 7). The uid backstop
   beyond `configuredLaneRunAsUser`'s username collapse closes shared-uid / alias
   gaps.
3. **Spawn-time projection gate (carry-forward) — distinct-UID only.** With a
   distinct lane OS user proven, run `runSuperviseClaudeCredentialGate` (which
   calls `ProjectClaudeAccessToken`). On any projector refusal, fail launch with
   the typed vocabulary below — still before scratch/token-mint/rows/process.
4. The existing Codex preflight remains in `runSuperviseProviderAuthGate`
   (unchanged).
5. Only after both gates pass may the handler create scratch, mint/inject the
   Striatum session token, insert supervisor rows, and launch.

The invariant: Claude same-user refusal **or** access-token projection +
verification happen **before** a real Claude process can enter MCP discovery and
before recovery can spend a work retry on it. `provider_auth_gate=off` does not
bypass either gate; only `provider_credential_projection=off` (default `auto`,
documented unsafe, emits `provider_credential.projection_bypassed`, marks the
dependency `disabled`, never implied by `provider_auth_gate=off`) can skip the
projection — and it **cannot** skip the same-user refusal (same-user remains
unsupported regardless of the projection flag, because the projection flag governs
distinct-UID delivery, not the custody class).

Typed refusal vocabulary (launch-precondition failures, not lane-execution
failures): `provider_credential_same_user_unsupported` (**new**),
`provider_credential_projection_failed`, `provider_credential_generation_stale`,
`provider_credential_source_unstable`, `provider_credential_expiry_too_near`,
`provider_credential_owner_mode_invalid`, `provider_credential_resolver_mismatch`,
`provider_auth_reseed_required`.

### 4. (reserved — projector writer details folded into section 1)

### 5. Runtime-expiry circuit breaker — daemon-owned positive authority (C1) + decay signal

**Positive freshness is daemon-owned only.** In `recoverStuckJobs`
(`recovery_decision_tree.go:704`), for a Claude lane whose stall class is
`agent_mcp_discovery_stall`, after `readJobRecoveryBudget` and **before**
`recordRecoveryAction` (line 1406), evaluate the daemon `provider_auth_dependencies`
row inside the same `tx`. The **`isPositivelyFresh`** predicate (ALL must hold):

- a row exists for `(repository_id, provider=claude, kind=oauth, lane_user,
  destination_selector)`; **else → fail closed**;
- `state == ready` (not `hydrating`/`reseed_required`/`unverifiable`/`disabled`);
- `expires_at > now + MinFreshness` (DB timestamp compare; no lane read);
- `last_receipt_id` resolves to a custody receipt with `verifier_result ==
  passed`, `source_generation_id == row.source_generation_id`, and
  `destination_generation_id == row.destination_generation_id` (internal
  consistency);
- `row.source_generation_id ==` the **daemon-re-observed** current operator-source
  generation (an HMAC the daemon computes over operator-owned bytes — lane-unreadable);
  a newer operator generation means the row is stale.

Branch outcomes (ordering explicit — provider-auth branch precedes any generic
requeue/transfer):

- **`isPositivelyFresh == true`** → the stall is **not** a provider-auth cause;
  fall through to the normal generic recovery path. (Correct: the credential is
  provably fine.)
- **`isPositivelyFresh == false`** (stale / missing / inconsistent / near-expiry /
  rotated) → set dependency `reseed_required` (or `unverifiable`), emit one
  redacted `provider_auth.reseed_required` event, route through reseed-debt
  handling that does **not** increment `requeue_count`/`transfer_count` and does
  **not** escalate `recovery_exhausted` for this cause; re-project against a fresh
  operator generation and requeue only jobs blocked against the stale generation.
  If a job already holds an open provider-auth blocker for the same dependency
  generation, the sweep is an idempotent no-op.

**Lane-file re-sample is downgrade-only.** An optional `SampleLaneCredential`
re-read may **only** turn `isPositivelyFresh == true` into `false` (e.g. the lane
file's expiry is *earlier* than the row — evidence of decay). It may **never**
turn a stale/missing/inconsistent row into fresh; a *later* lane-file expiry is
ignored. This is the structural inversion of the v3 leak: the lane-forgeable
`expiresAt` can subtract confidence, never add it.

**Decay signal (carry-forward).** The same periodic daemon-owned sweep
(`HandleRecoveryAuto` → `recoverStuckJobs`) evaluates each *running* Claude lane's
projected `expires_at` from its `provider_auth_dependencies` row (or, under B2,
the broker's per-fetch expiry). When a running lane crosses the near-expiry lead
it emits `provider_auth.expiry_warning` and marks near-expiry debt **before**
generic MCP-discovery recovery can classify the lane. The signal is computed from
daemon state only — **never** a lane heartbeat claim, a lane-file expiry, or
provider stdout/stderr.

This is the key #583 invariant: stale runtime Claude OAuth becomes typed
provider-auth readiness debt, not another `agent_mcp_discovery_stall` that burns
recovery budget and escalates `recovery_exhausted` — and no lane can forge its way
back to "fresh."

### 6. Durable state and receipts (access token never persisted)

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
  refresh_token_absent_ok        -- asserts the lane projection carries no refresh token
  verifier_result                -- passed|same_user_unsupported|source_missing|source_unstable|source_unparseable|destination_write_failed|destination_unparseable|expiry_too_near|owner_mode_invalid|resolver_mismatch
```

Only the daemon writes either table; lanes have **no** write path (load-bearing for
C1). Compact run-history events: `provider_credential.projected`,
`provider_credential.projection_refused`, `provider_credential.same_user_refused`
(**new**), `provider_auth.reseed_required`, `provider_auth.reseed_cleared`,
`provider_auth.expiry_warning`, `provider_credential.projection_bypassed`.
Payloads carry ids, enum selectors, provider, kind, lane user, expiry, failure
class, receipt id — **no credential bytes, token values, full private paths, or
provider output**.

### 7. Error codes and operator remediation

Stable daemon errors: the typed refusal vocabulary in section 3 plus
`provider_auth_reseed_required`. Operator-facing remediation is exact and
private-safe:

```text
# same-user refusal (C2/C3)
Claude OAuth self-driving lanes are not supported in same-user mode (the lane
would run as the operator user and could read the operator refresh token).
Configure a distinct lane OS user (STRIATUM_LANE_OS_USER) and retry. No lane
process was started.

# stale/expired projection (carry-forward)
Claude provider credential is not fresh enough for lane launch. Re-authorize or
refresh Claude as the operator user, then retry the run. No lane process was
started.
```

Doctor/dashboard may show
`provider=claude lane_user=<u> state=reseed_required
reason=provider_credential_expiry_too_near expires_at=<ts>
action=refresh_operator_claude_login_then_retry`, or for the same-user case
`state=disabled reason=provider_credential_same_user_unsupported
action=configure_distinct_lane_os_user`, and must never show home paths, raw
JSON, access/refresh/id tokens, provider stdout/stderr, or control-plane token
material.

### 8. Redaction contract (carried forward; extended)

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

| # | Falsifiable assertion | Refuting observation | Test |
|---|---|---|---|
| C1 | Daemon-owned state is the only **positive** freshness authority in recovery; a stale/missing/inconsistent row fails closed to reseed_required without generic budget burn. | A lane-forged future `expiresAt` upgrades a stale/missing row to fresh, or recovery increments `requeue_count`/`transfer_count`/escalates `recovery_exhausted` for a non-positively-fresh row. | `TestRecoveryRejectsLaneAuthoredProjectionFreshness`, `TestRecoveryStaleOrMissingDaemonRowFailsClosed`, `TestRecoveryInconsistentDaemonRowFailsClosed` |
| C1 | A lane-file re-sample can only downgrade a classification, never upgrade it. | A later lane-file expiry turns a not-fresh classification fresh. | `TestRecoveryLaneSampleIsDowngradeOnly` |
| C2 | A same-user Claude OAuth lane is refused with a typed precondition error before any side effect. | `supervise.start` creates scratch / mints a token / inserts supervisor rows / launches a process for a same-user (empty or operator-uid `RunAsUser`) Claude lane. | `TestSameUserClaudeLaneRefusedBeforeSideEffects` |
| C2 | The refusal triggers on uid-equality, not only username string. | A distinct username aliasing the daemon uid launches a same-user Claude lane. | `TestSameUserRefusalByResolvedUid` |
| C3 | No lane reaches the rotating refresh token by any route. | Any lane-readable surface named by the launch env yields a `refreshToken`, or a same-user lane reads the operator source. | `TestSameUserClaudeLaneRefusedNoCredentialSurface`, `TestLaneNeverReceivesRefreshTokenAllSurfaces` |
| C3/C1 | The operator source stays valid across concurrent + subsequent distinct-UID lane runs. | A lane action changes the operator source generation; a later lane projects from an invalidated source. | `TestConcurrentLanesNoRefreshTokenDesync`, `TestSubsequentLaneAfterOperatorRefresh` |
| C4 | No raw OAuth/refresh/access/id material, private path, provider output, or control-plane token reaches any durable surface. | A serialized payload or the projection file contains a forbidden substring. | `TestProjectionReceiptRedaction` |
| carry | The spawn-time gate refuses a stale/expired/unparseable/drifted credential before supervisor rows exist. | A real Claude process starts, then wedges in MCP discovery because projection failed. | `TestProjectionRefusesBeforeSupervisorRows` |
| carry | `provider_auth_gate=off` does not bypass projection or the same-user refusal. | `provider_auth_gate=off` launches Claude with a stale projection or in same-user mode. | `TestProviderAuthGateOffDoesNotBypassProjection`, `TestProviderAuthGateOffDoesNotBypassSameUserRefusal` |
| carry | Provider OAuth stays separate from Striatum control-plane tokens. | Any path copies/stores/exposes a Striatum control-plane token while projecting Claude OAuth, or the B2 socket accepts a Striatum token as identity. | `TestTrustBoundaryNoControlPlaneTokenToLane` |
| carry | Resolution is not a privilege bridge. | A workflow `CLAUDE_CONFIG_DIR` causes the daemon to write OAuth material to an arbitrary path. | `TestProjectionResolverRejectsWorkflowPath` |

---

## Required tests (named matrix — maps to modules/transitions)

**C1 — daemon-owned positive freshness authority (new/hardened):**

- `TestRecoveryRejectsLaneAuthoredProjectionFreshness` — launch distinct-UID with a
  35m projection (daemon row `ready`, `expires_at=now+35m`); advance 45m so the
  row is stale; **mutate the lane projection `expiresAt` to a future value**;
  trigger `agent_mcp_discovery_stall`. Assert recovery sets
  `reseed_required`/`unverifiable`, emits one event, does **not** increment
  `requeue_count`/`transfer_count`, does **not** escalate `recovery_exhausted`, and
  does **not** treat the lane-forged expiry as fresh.
  `recovery_decision_tree.go:704/1406`.
- `TestRecoveryStaleOrMissingDaemonRowFailsClosed` — no row (or `state != ready` /
  expired row) for a Claude `agent_mcp_discovery_stall` → fail closed to
  `reseed_required`, no generic counter increment, no green inference.
- `TestRecoveryInconsistentDaemonRowFailsClosed` — row `state=ready` but
  `expires_at <= now`, or `last_receipt_id` mismatched/missing, or
  `source_generation_id` rotated → fail closed to `unverifiable`.
- `TestRecoveryLaneSampleIsDowngradeOnly` — (a) lane-file expiry *earlier* than the
  row downgrades to not-fresh; (b) lane-file expiry *later* than a stale/missing
  row is ignored and never upgrades to fresh.
- `TestRecoveryPositivelyFreshFallsThroughToGeneric` — a fully consistent,
  future-dated, receipt-backed `ready` row makes a Claude stall fall through to the
  normal generic recovery path (no provider-auth debt for a genuinely non-auth
  stall).

**C2/C3 — same-user fail-closed + exhaustive no-refresh-token (new):**

- `TestSameUserClaudeLaneRefusedBeforeSideEffects` — `STRIATUM_LANE_OS_USER` unset
  (or same-as-daemon) so `config.RunAsUser == ""`; operator source carries a known
  `refreshToken`; `supervise.start` returns `provider_credential_same_user_unsupported`
  and creates **no** scratch, FIFO/ACL, session token, supervisor rows, helper/tmux,
  or Claude process. `supervision_control.go:97-112`, `supervision_provider_auth.go`.
- `TestSameUserRefusalByResolvedUid` — a distinct username that resolves (uid) to
  the daemon uid also refuses; a genuinely distinct uid proceeds to projection.
- `TestSameUserClaudeLaneRefusedNoCredentialSurface` — because the same-user lane
  never launches, assert no lane process exists and no projection file was written;
  the only artifact is the typed refusal + `state=disabled` dependency row.
- `TestLaneNeverReceivesRefreshTokenAllSurfaces` (C3 exhaustive) — for a distinct-UID
  lane, enumerate **every lane-readable credential surface named by the launch
  environment**: the resolver-proven destination (`ResolveCredential` over the lane
  launch env), `$CLAUDE_CONFIG_DIR/.credentials.json`,
  `$HOME/.claude/.credentials.json`, every credential-bearing env entry, and (B2)
  the socket response; assert each contains `accessToken` and **no `refreshToken`**,
  and that none resolves to the operator source path. `claude_projector.go`,
  `resolver.go`, `supervision_env.go`.

**C1/C3 — distinct-UID RTR (carry-forward, preserved):**

- `TestConcurrentLanesNoRefreshTokenDesync` — two distinct-UID lanes share operator
  generation G; each gets an access-token-only projection; simulate each CLI
  attempting refresh → no refresh token → no rotation; assert neither lane file has
  a refresh token, the operator source generation is unchanged, and both projections
  derive from G.
- `TestSubsequentLaneAfterOperatorRefresh` — lane 1 from G1; operator source rotates
  to G2 (operator-side authority); lane 2 projects current G2; assert lane 2 gets a
  G2 access token (not stale G1), lane 1 never invalidated G2, operator source valid
  throughout.

**Carry-forward (preserved unchanged):**

- `TestProjectionRefusesBeforeSupervisorRows`, `TestProjectionSourceRotationRace`,
  `TestProjectionDestinationOwnerMode`, `TestProjectionSymlinkEscape`,
  `TestProviderAuthGateOffDoesNotBypassProjection`,
  `TestProviderAuthGateOffDoesNotBypassSameUserRefusal` (**new pairing**),
  `TestTrustBoundaryNoControlPlaneTokenToLane`,
  `TestProjectionResolverRejectsWorkflowPath`, `TestProjectionReceiptRedaction`,
  `TestClaudeCLIAcceptsAccessTokenOnlyCredential` (B1-vs-B2 decider, P0 spike;
  the no-refresh-token guarantee holds under either outcome).

---

## TDD build order

1. **P0 fixtures + spike.** Failing unit tests on a fake projector filesystem:
   happy path, stale/expired/unparseable source refusal, source-rotation race,
   wrong owner/mode, symlink escape, expiry-inside-30m, redaction, and the
   `refresh_token_absent` assertion. Add the **same-user precondition** failing
   tests (`TestSameUserClaudeLaneRefusedBeforeSideEffects`,
   `TestSameUserRefusalByResolvedUid`). Run
   `TestClaudeCLIAcceptsAccessTokenOnlyCredential` to settle B1-vs-B2.
2. Migration + data-access tests for `provider_auth_dependencies` and custody
   receipts (upsert, idempotent same-generation refusal, retention, payload shape,
   **lane has no write path**).
3. Implement Claude source resolve + access-token extraction + the atomic
   lane-file **writer** in `laneproviderauth`, reusing `ResolveCredential`,
   `ParseExpiry`, `LaneFileReader` patterns; add `CLAUDE_CONFIG_DIR` to the safe
   env path only as a trusted selector. (If B2: add the broker socket + helper.)
4. Wire `HandleSuperviseStart`: **first** the same-user precondition refusal
   (C2/C3), **then** the distinct-UID projection gate — both before scratch, token
   mint, supervisor rows, helper/tmux, or Claude process.
5. Extend `run drive`, auto-spawn, doctor, dashboard, error catalog so both the
   same-user refusal and the projection refusal are visible and not looped as
   ordinary MCP-discovery failure.
6. Teach `recoverStuckJobs` the daemon-owned `isPositivelyFresh` predicate and the
   downgrade-only re-sample rule, ordered before `recordRecoveryAction`; add the C1
   tests (`TestRecoveryRejectsLaneAuthoredProjectionFreshness`,
   `TestRecoveryStaleOrMissingDaemonRowFailsClosed`,
   `TestRecoveryInconsistentDaemonRowFailsClosed`,
   `TestRecoveryLaneSampleIsDowngradeOnly`,
   `TestRecoveryPositivelyFreshFallsThroughToGeneric`) and the decay-signal test.
7. Docs: `command-authority-matrix.md` (withdraw unset-lane-user remediation for
   Claude OAuth; add same-user-unsupported), `spec.md`, CLI/RPC docs, decision-log
   entries for the new daemon state, error codes, same-user policy, and B2 socket if
   adopted.

---

## Load-bearing claims

| Claim | Evidence that supports it | Observation that refutes it |
|---|---|---|
| Recovery's positive freshness authority is daemon-owned only. | `isPositivelyFresh` reads only daemon-written `provider_auth_dependencies`/receipt columns inside `tx`; lane has no write path; lane re-sample is downgrade-only. | A lane-forged `expiresAt` upgrades a stale/missing row to fresh, or a non-positively-fresh row burns generic budget. |
| Stale/missing/inconsistent daemon row fails closed to reseed_required. | The predicate is total; any failing clause routes to reseed-debt with no generic counter increment and no green inference. | A missing/stale/inconsistent row yields a generic MCP-discovery retry or a green/ready classification. |
| Same-user Claude OAuth lanes never reach the refresh token. | The gate refuses `RunAsUser == ""` or operator-uid before any side effect; no projector call, no process; argument: no in-uid boundary can hide a file from a same-uid process, so refusal is the only sound option. | A same-user Claude lane launches and its CLI reads the operator `refreshToken`. |
| Distinct-UID lanes never hold a refresh token. | Projection file/env/B2 socket carry only an access token; `refresh_token_absent_ok` asserted; the all-surfaces scan finds no `refreshToken`. | Any lane-readable surface yields a `refreshToken`, or a lane-side refresh rotates the operator family. |
| The operator source cannot be desynchronized by a lane. | Refresh authority is operator-side single-writer; distinct lanes have no refresh token and no read path; same-user is refused; RTR tests keep the source valid. | A concurrent/subsequent lane invalidates the operator source or the operator CLI login. |
| Projection / refusal runs before real provider launch. | Stale source or same-user → `supervise.start` error with no supervisor rows/scratch/token mint/process. | A real Claude process starts and later wedges in MCP discovery because a gate failed. |
| Resolution is not a privilege bridge. | Source = daemon/operator-owned; destination = distinct-lane-home/daemon-configured; workflow `CLAUDE_CONFIG_DIR` escape fails closed without writing. | A workflow `CLAUDE_CONFIG_DIR` causes the daemon to write OAuth bytes to an arbitrary path. |
| Custody records are useful but private-safe. | Doctor/dashboard/recovery join dependency state to receipt id + failure class; redaction tests prove no raw credential/token/private path emitted. | An emitted payload contains OAuth bytes, token substrings, raw hashes, or private path strings. |
| Striatum control-plane credentials stay separate. | Projector inputs/outputs/receipts never read or copy `STRIATUM_MCP_TOKEN`, `client-token`, admin tokens, DSNs; B2 identity is `SO_PEERCRED`. | Any path copies/stores/exposes a Striatum control-plane token while projecting Claude OAuth. |
| The daemon adds no OAuth client. | No `net/http`/`oauth2` token-exchange in `laneproviderauth`; refresh stays operator-side. | The daemon performs an Anthropic OAuth refresh and becomes a second refresh-token writer. |

---

## Minimal closure scope for GH #583

Closure requires code on `origin/main` plus verifier evidence that:

- a distinct-UID Claude lane receives an access-token-only projection (no refresh
  token) and a stale/expired projection is refused or re-projected before a real
  Claude process starts; a same-user Claude lane is refused with
  `provider_credential_same_user_unsupported` before any side effect;
- a long dogfood spanning more than the Claude access-token TTL does not wedge new
  or running Claude lanes on credential-caused `agent_mcp_discovery_stall`, and a
  mid-session expiry becomes `reseed_required` from daemon-owned state — never via a
  lane-forged expiry — without burning generic recovery budget;
- concurrent and subsequent distinct-UID lane launches never desynchronize or
  invalidate the operator source credential family, and no lane (same-user or
  distinct) reads the operator refresh token;
- no manual `cp`/`chown`/`chmod`/escalation resolution is needed for the #583
  expiry class;
- RFC 0162 telemetry still reports expiry/absence, but launch and runtime
  correctness do not depend on the timer or metrics path.

An optional host timer in `halbritt/proximal` may pre-warm the **operator source**
freshness (refresh as the operator); it must not write lane credential files and
is not part of the correctness boundary. If the timer is absent or late, the
same-user refusal, the spawn-time projection gate, and the daemon-owned runtime
circuit breaker still hold or refuse synchronously.
