# RFC 0169 Holder Proposal: Provider-Agnostic Lane Credential-Readiness Spine + Ephemeral Fresh-Per-Spawn Placement
author: holder-author-001

## Claim

RFC 0169 P0–P2 ships a **provider-agnostic credential-readiness contract** (the
spine) and **converges the one copied-OAuth provider — Claude — onto the
daemon-mints-fresh-per-spawn model agy already runs**, so that the #583
stale-credential class is closed *by construction* rather than by a new
per-provider gate.

Two load-bearing claims carry the spec, each stated below as a falsifiable
assertion anchored to named source sites with the observation that refutes it:

1. **The `ProviderReadinessAdapter` registry is a behavior-preserving refactor.**
   It replaces ~5 hardcoded provider switch sites with one interface + registry;
   the `supervise.start` gate collapses to *one lookup + `ValidateReadiness`,
   refuse-closed*; codex's predicate is RFC 0121's `Check` **verbatim** and agy's
   is a no-op receipt — both with **zero behavior change**; the *only* intended
   behavior delta is that an unregistered provider becomes a typed refusal
   (`provider_readiness_unregistered`) instead of a silent GateAuto fall-through.
2. **Spawn-fresh placement makes #583 structurally impossible without modifying
   the provider CLI.** The CLIs read credential *files* (no fd-injection); so the
   spec keeps a real file but re-mints it lane-private, `0600`, spawn-fresh every
   launch via a `CredentialPlacer`, behind a lane-private `CLAUDE_CONFIG_DIR`
   prefix the claude CLI provably resolves, with a source-rotation-race guard —
   and (the part a spawn-only gate cannot do) the daemon **re-places before
   expiry** so the lane never reaches its own refresh path, which is what also
   closes the 0165 cycle-1 runtime-expiry (F1) and refresh-token-rotation (F2)
   findings at spine altitude.

## Altitude and relationship to RFC 0165 (kept SEPARATE)

Per the operator decision recorded in the SEED and RFC 0169 Open Question 6, this
proposal stays at the **spine + structural-prevention** altitude: the contract,
the registry, the fresh-per-spawn placement, the tamper-proof custody/breaker. It
treats Claude's concrete credential-shape mechanics — *which* `claudeAiOauth`
fields a placed copy may carry, exactly how the claude CLI behaves on an
access-token-only file — as the **`OAUTH_COPIED` detail RFC 0165 owns**. Where
this spec must reference those mechanics (the refresh-token-isolation invariant),
it states the invariant as a *contract obligation on the `OAUTH_COPIED` placer*
and **cites RFC 0165 for the mechanism rather than re-deriving it**. RFC 0169
marks RFC 0165 as the `OAUTH_COPIED` detail doc, not superseded (OQ6).

## Current source anchors (verified 2026-06-24)

The audit reframe is ratified by the code:

- **The gate today is codex-only and fall-through for everything else.**
  `go/pkg/mutations/supervision_provider_auth.go::runSuperviseProviderAuthGate`
  (line 37): `supported := config.AgentLoopMode == agentLoopModeSelfDriving &&
  provider == laneproviderauth.ProviderCodex`. If `!supported`, `GateRequired`
  refuses `lane_provider_preflight_unsupported` but **`GateAuto` returns `nil`
  (passes)** — so *claude and any unknown provider launch unchecked in the
  default mode*. That silent pass is the #583 surface.
- **The codex predicate to preserve verbatim** is
  `go/pkg/laneproviderauth/lane_provider_auth.go::Check` (line 178): a cheap
  OFFLINE `$CODEX_HOME/auth.json` probe (`checkCodexOfflineAuth`, line 497) with a
  live-smoke fall-through (`classifyCodexResult`, line 433). The gate reaches it
  via the package var `supervisionProviderAuthCheck = laneproviderauth.Check`
  (supervision_provider_auth.go line 13).
- **agy already places a fresh, lane-private, 0600 credential file per launch.**
  `go/pkg/agentloop/mcpconfig.go::writeEphemeralGeminiSettings` (line 179) →
  `writeGeminiSettingsAt` writes the settings file with `os.WriteFile(path, body,
  0o600)` (line 241), fresh every launch, removed on teardown. (Its *content* is
  the rotating Striatum MCP bearer, not a vendor OAuth token — agy has no vendor
  provider OAuth in the supervised path. The **placement contract** — fresh,
  lane-private, `0600`, per-launch — is what generalizes; see Hard Claim 2.)
- **The resolver and expiry parser already exist (RFC 0162) and are
  provider-agnostic + fail-closed.**
  `go/pkg/laneproviderauth/resolver.go::ResolveCredential` (line 57): for claude,
  `$CLAUDE_CONFIG_DIR/.credentials.json` wins over `$HOME/.claude/.credentials.json`
  (lines 78–92); unknown provider → `ErrResolverMismatch` (fail closed, line 96).
  `go/pkg/laneproviderauth/expiry.go::ParseExpiry` (line 41) →
  `claudeExpiry` reads `claudeAiOauth.expiresAt` (line 100).
- **The switch sites the registry subsumes:**
  `agentloop/loop.go` (`bootstrapDeliveryModeFor` ~191, `ensureClaudeWorkspaceTrusted`
  ~258, `mcpReconnectPrompt` ~680); `agentloop/mcpconfig.go::injectLaneMCPConfigWithRewritePath`
  (line 74, `switch LaneAdapterName`: claude/agy/codex/default);
  `mutations/supervision_lane_config.go::adapterName` (line 66);
  `mutations/supervision_provider_auth.go::runSuperviseProviderAuthGate` (line 37);
  the `laneproviderauth` resolver/expiry/Check switches above.
- **The gate's launch ordering** is fixed in
  `go/pkg/mutations/supervision_control.go::HandleSuperviseStart` (line 83):
  `providerAuthGateMode` (93) → `loadSupervisionStartConfig` (97) →
  `runSuperviseProviderAuthGate` (101) → scratch `MkdirAll 0700` (112–115) →
  `mintSessionBoundToken` (158) → insert supervisor rows (165) →
  `supervisionLaunch` (193). The **placer's `Place` must run after the cleared
  gate and immediately before `supervisionLaunch`** so the file is fresh at exec.
- **Breaker counters are already daemon-owned, not lane-readable.**
  `go/pkg/mutations/recovery_decision_tree.go` reads `requeue_count,
  transfer_count, respawn_count, escalation_pending` from a daemon DB row (line
  ~336) — the new custody + bad-generation state follows the same daemon-owned,
  no-lane-file pattern (Layer 3).

---

## Hard Claim 1 — The contract spine is a behavior-preserving refactor

### Design

Introduce in `go/pkg/laneproviderauth`:

```text
type CredentialAssuranceClass string
const (
    EphemeralDaemonMinted   = "EPHEMERAL_DAEMON_MINTED"    // agy/gemini
    OAuthLaneOwnedSelfRefresh = "OAUTH_LANE_OWNED_SELF_REFRESH" // codex (RFC 0121)
    OAuthCopied             = "OAUTH_COPIED"               // claude (RFC 0165 detail)
    StaticAPIKey            = "STATIC_API_KEY"             // future
    Unknown                 = "UNKNOWN"                    // unregistered → refuse
)

type ProviderReadinessAdapter interface {
    AssuranceClass() CredentialAssuranceClass
    ValidateReadiness(laneUser string, generation int64, timeout time.Duration) (ReadinessReceipt, error)
}
```

A package-level `ProviderRegistry` with `init()`-based registration
(database/sql-driver style: registry open for extension, gate closed).
`runSuperviseProviderAuthGate` collapses to: resolve `config.adapterName()` →
registry lookup → `ValidateReadiness`, **refuse-closed on any error, timeout, or
panic** (a panicking adapter is recovered and mapped to refusal, never a pass).
Unregistered → `provider_readiness_unregistered`.

### What "behavior-preserving" precisely means (the seam the falsifier will probe)

The refactor preserves, **per registered provider, byte-for-byte**:

- **codex (`OAuthSelfRefreshAdapter`):** `ValidateReadiness` is exactly
  `laneproviderauth.Check(ctx, Params{...})` with the *same* `Params`
  (Provider/Binary/RunID/LaneID/RunAsUser/Env) the current gate builds
  (supervision_provider_auth.go lines 55–62), returning the same `Result`. The
  **class-independent gate scaffolding stays in the gate, not the adapter**, and
  is preserved unchanged: `GateOff → return nil` (line 38); the
  `AgentLoopMode == selfDriving` support guard; `GateAuto && RunAsUser=="" →
  return nil` (line 52, the "no lane user, codex uses the daemon's own auth" skip);
  and the **best-effort `emitLaneAuthSuccessEvent` on `result.Passed()`** (line 69,
  FA-7: its error is discarded, never flips the verdict). The error mapping in
  `providerAuthRPCError` (line 124) is reused verbatim.
- **agy (`EphemeralDaemonMintedAdapter`):** `ValidateReadiness` returns a no-op
  satisfied receipt (the daemon's placement already happened; nothing durable can
  be stale). No gate behavior changes for agy: today it is `!supported` and passes
  in `GateAuto`; after, it routes to the no-op adapter and passes — identical
  outcome.
- **claude in P1 (pure refactor, before P2 placement lands):** preserves the
  *current* behavior — observe-only / pass in `GateAuto` — registered as
  `OAuthCopiedAdapter` whose predicate is a stub that returns satisfied until P2
  wires the placer + the `OAUTH_COPIED` predicate. **P1 introduces no claude
  behavior change.**

### The one intended behavior delta, stated honestly

P1 changes exactly one observable behavior: an **unregistered provider** that
today passes in `GateAuto` now refuses with `provider_readiness_unregistered`.
This is RFC 0169's explicit goal (guilty-until-proven-fresh) and is gated by its
own P0 test — it is *not* incidental refactor drift, and codex/agy/claude are
unaffected by it.

### Falsifiable assertions (Claim 1)

| # | Assertion | Refuted if... | Test |
|---|-----------|---------------|------|
| 1.1 | The codex path is byte-identical pre/post refactor | A golden capture of the codex `Result` + `providerAuthRPCError` + emitted `lane.auth_success` event differs after routing through the registry | `TestCodexPredicateCharacterizationUnchanged` — record the pass-case `Result`, the auth-fail-case RPC error, and the `GateAuto && RunAsUser==""` skip BEFORE the refactor; assert identical after |
| 1.2 | agy is a no-op with no gate change | An agy lane's launch path observes any new check, event, or refusal | `TestAgyNoOpReceiptNoBehaviorChange` |
| 1.3 | An unregistered provider refuses | A provider with no registry entry launches in any gate mode | `TestUnregisteredProviderRefusedAllModes` (asserts `provider_readiness_unregistered`) |
| 1.4 | The gate is refuse-closed on adapter error/timeout/panic | A `ValidateReadiness` that panics or times out yields a *launched* lane | `TestAdapterPanicRefusesClosed`, `TestAdapterTimeoutRefusesClosed` |
| 1.5 | The gate-mode + support + skip semantics are unchanged | `GateOff` runs a check, or `GateRequired` on an unsupported-but-registered provider changes class behavior | `TestGateModeMatrixUnchanged` |

**The seam is proven before any new logic lands:** P1 merges only when 1.1–1.5
are green; the placer (Claim 2) is a separate slice that cannot regress the codex
characterization.

---

## Hard Claim 2 — Spawn-fresh placement structurally closes #583 without CLI modification

### Why a file (not fd-injection) — and why that is sufficient

The third-party CLIs read credentials from files and call vendor APIs directly;
Striatum cannot modify them (RFC 0169 Non-Goals). So the spec keeps a **real
file** but removes everything that makes a file go stale: it is **re-minted every
launch, lane-private, `0600`, owned by the lane user**, immediately before
`supervisionLaunch`. A `CredentialPlacer` owns this step:

```text
type CredentialPlacer interface {
    Place(ctx, laneDir string) error   // mint/copy fresh, lane-user-owned, 0600
    Cleanup(laneDir string) error
}
```

- **agy's `writeEphemeralGeminiSettings` becomes the first implementation, zero
  behavior change** (it already writes `0600` fresh-per-launch; the refactor wraps
  the existing call behind the interface).
- **`ClaudeCredentialPlacer` is added** (atomic chown-write strategy, OQ2): resolve
  the operator source via `ResolveCredential("claude", operatorEnv)` and the lane
  destination via the lane-private prefix; write `0600` owned by the lane user.

### The CLI provably reads the placed file (the lane-private prefix proof)

The claude CLI resolves `$CLAUDE_CONFIG_DIR/.credentials.json` when
`CLAUDE_CONFIG_DIR` is set, else `$HOME/.claude/.credentials.json` — this is
already encoded in `ResolveCredential` (resolver.go lines 78–92), the **same**
resolver RFC 0162's sampler uses to find the live credential. The placer sets
`CLAUDE_CONFIG_DIR=<lane-private>/` (e.g. `/run/striatum/lanes/<id>/claude`) in the
lane launch env (`HOME`/`CLAUDE_CONFIG_DIR`/`XDG_CONFIG_HOME` are already in the
supervised-env allowlist, `supervision_env.go` lines 461/471) and writes
`<lane-private>/.credentials.json`. Because admission, placement, and observability
all route through one resolver, **the path the CLI reads is the path the placer
writes is the path the gate checks** — there is no global `~/.claude` decoy.

### The part a spawn-only gate cannot do (and why #583 needs it)

A spawn-time placement only proves freshness *at spawn*. #583 also includes the
operator source expiring **mid-run** (0165 cycle-1 F1). The spine closes this
structurally, not by detecting a 401 after it happens:

- The **OAUTH_COPIED placement carries no lane-independent rotation authority over
  the operator credential family** (the 0165 cycle-1 C1 binding constraint, carried
  forward). Concretely the placer hands the lane a short-lived credential it cannot
  use to rotate the operator's refresh-token family; the daemon, not the lane, is
  the refresher. *Which fields a placed `claudeAiOauth` may carry is the RFC 0165
  `OAUTH_COPIED` detail; the spine only requires the rotation-isolation property
  and a test that proves it.*
- The **daemon re-places before expiry**: the supervisor-helper re-invokes
  `ValidateReadiness` / re-`Place` on its existing heartbeat ticker for
  `OAUTH_COPIED` lanes (OQ1). On a flip to refusal it sends **SIGTERM + emits one
  `provider_auth.reseed_required`** — *before* the lane's next model/MCP action can
  401 into a generic `agent_mcp_discovery_stall`. The re-validate is a
  **daemon/helper observation over a trusted channel, never a lane-authored claim**
  (0165 cycle-1 C3).
- **Cleanup-as-lease-boundary** is the backstop: removing the placed credential is
  the authoritative end-of-lease signal (remove → next re-validate fails → SIGTERM),
  so an expired credential is replaced by a clean restart, not a degraded mid-run
  failure.

This is why "converge Claude onto agy's model" is the right frame: the lane holds
no durable rotating secret; the daemon owns freshness end to end.

### The source-rotation race during placement

Read the operator source **before and after** the copy. If the source generation
changes between the two reads, refuse `provider_credential_source_unstable` (retry
once first) — never bless a mixed generation. (This is RFC 0165's race guard,
generalized as a placer obligation.)

### Falsifiable assertions (Claim 2)

| # | Assertion | Refuted if... | Test |
|---|-----------|---------------|------|
| 2.1 | A pre-existing aged copy cannot be the launch credential | Aging `<dest>/.credentials.json` to days old and launching uses the aged file rather than a re-placed current copy | `TestAgedCopyReplacedBySpawnFresh` (assert placed generation == operator source generation) |
| 2.2 | The CLI reads the placed lane-private file, not a global decoy | With `CLAUDE_CONFIG_DIR=<lane-private>`, `ResolveCredential` resolves the global `~/.claude` path | `TestClaudeConfigDirOverrideResolvesPlacedFile` |
| 2.3 | A mid-copy source rotation never blesses a mixed generation | Rotating the source between pre/post read yields a launched lane | `TestSourceRotationDuringPlacementRefuses` (`provider_credential_source_unstable`) |
| 2.4 | A mid-run source expiry is caught before generic stall recovery | A long-run lane crossing the access-token TTL increments any generic requeue/transfer counter, or escalates `recovery_exhausted`, before `reseed_required` | `TestRuntimeExpirySIGTERMsAndReseedsWithoutGenericBudget` (the GATE property) |
| 2.5 | A placed OAUTH_COPIED credential cannot rotate the operator family | A simulated lane-side refresh invalidates the operator source, or the lane retains independent rotation authority | `TestOAuthCopiedNoLaneRotationAuthority` (mechanism per RFC 0165) |
| 2.6 | Placement is the only credential path; no agy behavior change | The agy lane's settings file content/mode/lifecycle differs after the placer wrap | `TestAgyPlacerZeroBehaviorChange` |

---

## The six open questions, discharged as build-bearing constraints + tests

**OQ1 — Runtime-freshness closure: BOTH.** Primary = supervisor-helper
re-`ValidateReadiness`/re-`Place` for `OAUTH_COPIED` lanes on its existing
heartbeat ticker; flip-to-refusal → SIGTERM + one `provider_auth.reseed_required`.
Backstop = cleanup-as-lease-boundary (remove → SIGTERM). **GATE property:** the
re-validate is a daemon-side observation and fires *before* the lane's next
model/MCP action, so runtime decay never enters the generic
`agent_mcp_discovery_stall` path. *Test:* `TestRuntimeExpirySIGTERMsAndReseedsWithoutGenericBudget`
(2.4 above) + `TestCleanupRemovalIsLeaseBoundary`.

**OQ2 — Placement strategy default: atomic chown-write (P2 default); tmpfs+setuid
opt-in (P4).** chown-write is universal (no kernel capability), composes with the
existing `sudo -n -u <lane>` model and RFC 0168's per-lane OS user, and is
revocable by deleting the lane dir. Per-lane tmpfs in a mount namespace needs
`CAP_SYS_ADMIN` or a new setuid helper — a privileged component deferred until the
universal path proves insufficient. *Test:* `TestPlacementAtomicChownOwnerMode0600`
(O_CREAT|O_EXCL temp + `fchown` + atomic rename inside the lane-private dir;
pre-existing symlink at dest refused, not followed — defeats TOCTOU/symlink).

**OQ3 — Generation identity for opaque codex: monotonic-dispatch-seq +
advisory-STALE-only.** The daemon cannot fingerprint codex's self-refreshing token
(it rotates inside the sandbox), so the generation = a daemon-assigned monotonic
dispatch sequence per `(provider, lane_user)`. A lane's probe result is trusted
**STALE-only, never authoritative-FRESH** (a lane has no incentive to falsely
report itself stale). codex FRESH comes only from the daemon's own
`laneproviderauth.Check` offline observation. *Test:*
`TestCodexLaneFreshClaimIgnoredDaemonCheckAuthoritative` (a lane reporting FRESH on
a generation `Check` says is stale is not admitted; a lane reporting STALE
short-circuits to reseed).

**OQ4 — Minimum expiry lead time: `max(10min, TTL/4)` of observed TTL.** A fixed
30min over-refuses short-TTL tokens and under-protects long-TTL ones; a fraction
adapts, the floor guarantees a minimum buffer for placement + first MCP action.
TTL is read via `ParseExpiry` (`claudeExpiry`, expiry.go line 100). The OQ1
re-place interval = `lead/2` so two re-checks fall inside the lead window. *Test:*
`TestExpiryLeadFractionWithFloor` (within lead → `provider_credential_expiry_too_near`;
outside → pass; re-place fires at `lead/2`).

**OQ5 — Run dependency: SURFACE, do not over-commit P0.** Recommend P4-optional:
promote `(provider, lane_user)` readiness to a first-class RFC 0122 run dependency
so the scheduler parks dependent jobs behind ONE reseed gate before spending run
setup ("8 lanes wedge on one dead token" → "the run never starts until fresh").
P0–P2 stay launch-time. The spine is designed so this is **additive** — the same
registry + custody key feeds the scheduler. *Deferred test named:*
`TestProviderReadinessAsRunDependencyParksJobs` (P4). No P0 commitment.

**OQ6 — Subsumption: KEPT SEPARATE.** RFC 0169 is the umbrella spine; RFC 0165 is
the `OAUTH_COPIED` detail doc, not superseded. *Doc task (P1):* 0169 references 0165
as the `OAUTH_COPIED` mechanism; 0165's status line notes it details RFC 0169's
`OAUTH_COPIED` assurance class. No code; a `make check-docs` cross-link assertion.

---

## Layer 3 hardened against an untrusted, compromised lane

A lane is a sandboxed but **untrusted** OS user (RFC 0096 / #135 / #296). Layer 3
is built so a compromised lane cannot spoof readiness, exfil secrets, or replay
authority:

- **Custody fingerprints + breaker counters live ONLY in daemon PostgreSQL, never
  in a lane-readable file.** New `credential_custody` and `bad_generation_blacklist`
  tables are daemon-write-only, keyed `(repository_id, provider, lane_user,
  generation)` — the exact primitive RFC 0162 *and* RFC 0165 both deferred. This
  follows the existing pattern where `recovery_decision_tree.go` already reads
  breaker counters from a daemon DB row (no lane file). → no spoof-via-`touch`/mtime,
  no exfiltration side channel. *Test:* `TestCustodyAndBreakerNoLaneReadableSurface`.
- **The placer holds a short-lived, job-scoped, single-use daemon-minted
  capability.** It authorizes exactly one placement for one `(job, provider,
  lane_user, destination)`; a second use or replay is refused. This closes the
  privilege-bridge / replay attack on the operator→lane boundary. *Test:*
  `TestPlacerCapabilitySingleUseNoReplay`.
- **Destination owned by exactly the lane user, `0600`, written by fd/atomic-rename
  or per-lane namespace.** Source + destination paths come from `ResolveCredential`,
  **never lane-supplied arbitrary paths**; a symlink/TOCTOU at the destination is
  refused, not followed. One credential file per provider per lane; no cross-provider
  read (private per-lane prefix). *Test:* `TestPlacementDefeatsSymlinkAndCrossProviderRead`.
- **No secret material crosses the redaction boundary.** No credential bytes, OAuth
  access/refresh/id tokens, full operator path, or provider stdout reach DB rows,
  repo artifacts, metrics, events, or doctor output. Custody surfaces show only an
  HMAC-SHA256 generation fingerprint (daemon-local key), a source-selector enum
  (`claude_config_dir`/`home_default`, not the full path), the parsed expiry, and a
  verifier-result enum — mirroring the existing redacted `lane.auth_success` payload
  (`supervision_provider_auth.go` lines 86–91, carrying only `lane_user`/`provider`/
  `kind`). *Test:* `TestCustodyRedactionNoSecretsNoPrivatePaths`.

The RFC 0096/#135/#296 trust boundary is preserved: the lane still authenticates to
Striatum only with its own session-bound capability token and never receives a
daemon/admin token, minting authority, or another provider's credential.

## Carry-forward from the RFC 0165 cycle-1 ledger

The 0165 cycle-1 gate returned `needs_revision` on two material, unrebutted
findings. This spine closes both at its altitude:

- **F1 / C2 / C3 (runtime-expiry circuit breaker):** closed by OQ1 — daemon-side
  re-validate on the helper heartbeat classifies decay as provider-auth debt
  (`reseed_required`) and SIGTERMs before generic recovery budget is touched; the
  detecting signal is daemon/helper-owned, never a lane claim. (Assertions 2.4,
  OQ1 tests.)
- **F2 / C1 / C4 (refresh-token-rotation desync):** closed by the OAUTH_COPIED
  rotation-isolation invariant (the lane carries no independent rotation authority
  over the operator family; the daemon re-places before expiry so the lane never
  reaches its own refresh path) — the spine states the invariant + test (assertion
  2.5); RFC 0165 owns the field-level Claude mechanism. The test matrix (C4)
  includes long-run TTL-crossing, concurrent lanes sharing one source generation,
  subsequent-launch-after-refresh, and the redaction checks.

## Implementation slices

### P0 — Failing fixtures (tests before behavior)

`TestUnregisteredProviderRefusedAllModes` (1.3); `TestCodexPredicateCharacterizationUnchanged`
(1.1); `TestAgedCopyReplacedBySpawnFresh` (2.1); `TestSourceRotationDuringPlacementRefuses`
(2.3); `TestOAuthCopiedNoLaneRotationAuthority` (2.5, mechanism per 0165);
`TestRuntimeExpirySIGTERMsAndReseedsWithoutGenericBudget` (2.4 / OQ1);
`TestBadGenerationRefusedThoughParses`; `TestBreakerFoldsToOneReseedPerWindow`;
`TestCustodyRedactionNoSecretsNoPrivatePaths`.

### P1 — Provider Readiness Contract (PURE refactor)

`adapter.go` (interface + assurance-class constants + `ProviderRegistry`, `init()`
registration). Extract codex into `OAuthSelfRefreshAdapter` (= `Check` verbatim);
add agy `EphemeralDaemonMintedAdapter` (no-op); register claude `OAuthCopiedAdapter`
preserving current pass-through. Collapse `runSuperviseProviderAuthGate` to lookup
+ `ValidateReadiness`, refuse-closed; unregistered → `provider_readiness_unregistered`;
preserve gate-mode/support/skip/best-effort-event scaffolding. **Merge-gated on
1.1–1.5 green.** Update `docs/reference/command-authority-matrix.md` if a new
typed refusal reason is surfaced.

### P2 — CredentialPlacer + ephemeral placement

`CredentialPlacer` interface; agy `writeEphemeralGeminiSettings` refactored to
implement it (zero change); `ClaudeCredentialPlacer` (atomic chown-write) +
lane-private `CLAUDE_CONFIG_DIR`/`HOME` override + pre-spawn `ParseExpiry` lead
gate (`max(10min, TTL/4)`) + source-rotation guard + rotation-isolation property.
`Place` wired between the cleared gate (supervision_control.go line 101) and
`supervisionLaunch` (line 193). OAUTH_COPIED `ValidateReadiness` turns on.

### P3 — Custody, immune memory, breaker (seam surfaced)

`GenerationProbe` interface; `credential_custody` + `bad_generation_blacklist`
migrations (daemon-owned, daemon-write-only); dispatch probes → checks blacklist →
writes receipt before issuing a packet; recovery (`recovery_decision_tree.go`)
classifies stale-credential drift as `reseed_required` and de-dups escalations to
one per `(provider, lane_user)` rolling window.

### P4 — Runtime-freshness closure + optional scheduler dependency (seam surfaced)

Supervisor-helper re-admission heartbeat for OAUTH_COPIED + cleanup-as-lease-boundary
(OQ1, specified now, builds here); optional tmpfs/setuid placement strategy (OQ2);
optional RFC 0122 run dependency (OQ5).

## What this proposal does NOT claim (scope honesty)

- It does **not** re-derive RFC 0165's Claude credential-shape mechanics; it names
  the `OAUTH_COPIED` rotation-isolation invariant and cites 0165 for the field-level
  detail.
- It does **not** modify the provider CLIs, reimplement vendor OAuth refresh, or
  assume fd-injection / a custom-auth endpoint.
- It does **not** make a background timer the correctness boundary (a pre-warm timer
  is optional ergonomics; launch and re-validate are synchronous).
- It does **not** commit the OQ5 run-dependency or the OQ2 tmpfs strategy to P0–P2;
  both are surfaced as additive P4 seams.

This is the published claim the falsifiers re-attack.
