# RFC 0169: Provider-agnostic lane credential readiness

Status: proposed
Date: 2026-06-24
Context: [#583](https://github.com/halbritt/striatum/issues/583);
[RFC 0165](0165-claude-provider-credential-freshness.md) (Claude credential
freshness — this RFC generalizes it); [RFC 0121](0121-lane-provider-auth-preflight.md)
(lane provider-auth preflight gate); [RFC 0162](0162-lane-auth-silent-failure-observability.md)
(lane auth observability); [RFC 0143](0143-lane-credential-survival-across-boot-epoch-rotation.md)
(lane Striatum-token survival); [RFC 0168](0168-per-lane-security-principal.md)
(per-lane OS principal); RFC 0096 (supervised-lane trust boundary); RFC 0110
(lane OS user / PostgreSQL isolation); RFC 0122 (scheduler dependencies);
`go/pkg/laneproviderauth/`; `go/pkg/agentloop/mcpconfig.go`;
`go/pkg/mutations/supervision_provider_auth.go`
author: proposer-claude-opus-4-8-001

## Problem

[RFC 0165](0165-claude-provider-credential-freshness.md) prevents one provider
(Claude) from launching a lane with a stale credential. The obvious follow-up —
"do the same for the other providers" — is the wrong question, because the three
providers Striatum launches do not share a credential model. A code audit of
`go/pkg/laneproviderauth/` and `go/pkg/agentloop/` (2026-06-24) finds:

| Provider | Credential model | Stale-copy failure class (#583)? |
| --- | --- | --- |
| **claude** | Rotating OAuth **copied** operator → lane home (`~/.claude/.credentials.json`); the lane holds a frozen copy it **cannot self-refresh**. | **Yes** — the #583 wedge. |
| **codex** | The lane OS user's **own** `~/.codex/auth.json` (`codex login`), which the codex CLI **self-refreshes**; already gated by RFC 0121's offline preflight. | **No** — the lane owns the refresh token. |
| **agy / gemini** | **No provider OAuth at all.** The daemon writes an **ephemeral** `.gemini/settings.json` carrying a freshly-minted Striatum MCP token at every launch (`writeEphemeralGeminiSettings`). | **No** — nothing durable to go stale. |

Two conclusions follow. First, **Claude is the odd one out**, not the template:
copying RFC 0165's hydrator onto codex and agy would solve problems they do not
have. Second, and more useful, **agy already runs the architecture RFC 0165 is
groping toward** — the daemon is the sole custodian and the credential is minted
fresh at every spawn, so the lane never holds a durable rotating secret.

The genuine gap is not "more hydrators." It is that the three credential models
are special-cased across roughly five hardcoded switch sites
(`agentloop/loop.go`, `supervision_lane_config.go`, `laneproviderauth/resolver.go`,
`laneproviderauth/expiry.go`, `laneproviderauth/lane_provider_auth.go`), so:

- there is no uniform place a provider declares *how its freshness is proven*,
  and an unknown/future provider falls through to "assume it works until it
  wedges" rather than a refusal;
- RFC 0121 (gate, codex-only), RFC 0162 (observability, OAuth-expiry-scoped +
  codex heartbeat), and RFC 0165 (prevention, Claude-only) each re-implement a
  slice of "is this lane's credential ready" with no shared contract;
- both RFC 0162 and RFC 0165 explicitly **defer the same primitive** — keying
  credential readiness by `(provider, lane_user, generation)` rather than by
  lane OS user alone — so multiple providers under one lane user cannot be
  reasoned about independently.

This RFC proposes the provider-agnostic spine those three RFCs are missing, and
converges the heterogeneous credential models onto the one that already works.

## Goals

- Give **every** provider — current and future — a single, uniform, refuse-closed
  admission contract for credential readiness, with the provider-specific auth
  knowledge isolated behind one interface instead of five switch sites.
- Make "stale copied credential" (the #583 class) **structurally impossible** for
  the copied-OAuth provider (Claude) by minting its credential fresh per spawn,
  the way agy already does, rather than gating against a persistent stale copy.
- Treat an unregistered/unknown provider as **guilty-until-proven-fresh**
  (quarantined refusal), so adding a provider is a deliberate act that clears the
  contract, not a silent launch that burns recovery budget.
- Make credential readiness **provable and auditable without storing secrets**,
  and make stale-credential recovery **tamper-proof** and **non-amplifying** (one
  operator action, not N lane retries) across all providers.
- Subsume — not duplicate — RFC 0121 (becomes one assurance class's predicate),
  RFC 0162 (feeds the contract its resolver + expiry observations), and RFC 0165
  (becomes the `OAUTH_COPIED` class implementation).
- Preserve the RFC 0096 / #135 / #296 trust boundary: lanes authenticate to
  Striatum only with their own session-bound capability token and never receive
  daemon/admin tokens, token-minting authority, or another provider's credential.

## Non-Goals

- Do not replace the third-party provider CLIs' credential discovery. claude,
  codex, and gemini read credentials from **files** and call their vendor APIs
  directly; this RFC does not assume an fd-injection, custom-auth-endpoint, or
  in-process-token-callback path that those CLIs do not support.
- Do not reimplement any vendor's OAuth refresh. "Fresh source" for a copied
  credential means copying the operator's already-refreshed file at spawn, not
  the daemon minting vendor access tokens.
- Do not make a background timer the correctness boundary (RFC 0165 §6 stands): a
  pre-warm timer may reduce latency, but launch must verify or place
  synchronously.
- Do not move credential bytes, OAuth access/refresh/id tokens, full operator
  paths, or provider stdout/stderr into daemon DB rows, repo artifacts, metrics,
  events, or doctor output.
- Do not solve per-lane OS-user isolation here; that is [RFC 0168](0168-per-lane-security-principal.md).
  This RFC composes with it but does not depend on it.
- Do not redefine RFC 0162's alert thresholds or RFC 0143's Striatum-token
  survival semantics.

## Proposal

Three layers plus one cross-cutting invariant. Layer 1 is the spine and lands
first as a pure refactor; layers 2 and 3 plug into it.

### 1. Provider Readiness Contract (the spine)

Introduce one interface in `go/pkg/laneproviderauth` that every provider adapter
implements:

```text
type ProviderReadinessAdapter interface {
    AssuranceClass() CredentialAssuranceClass
    ValidateReadiness(laneUser string, generation int64, timeout time.Duration) (ReadinessReceipt, error)
}
```

A package-level `ProviderRegistry` replaces the current ~five switch sites; an
unregistered provider is a refusal (`provider_readiness_unregistered`), not a
fall-through. The `supervise.start` gate
(`go/pkg/mutations/supervision_provider_auth.go`) collapses to a single
registry lookup + `ValidateReadiness` call, refuse-closed on any error, timeout,
or panic. Each provider declares a **freshness assurance class**, and the gate
applies the class's predicate uniformly:

| Assurance class | Provider(s) | Predicate at admission |
| --- | --- | --- |
| `EPHEMERAL_DAEMON_MINTED` | agy / gemini | Daemon mints the credential at spawn → readiness is trivially satisfied (no-op receipt). |
| `OAUTH_LANE_OWNED_SELF_REFRESH` | codex | RFC 0121's existing offline `Check` predicate, verbatim. The lane owns and self-refreshes its token. |
| `OAUTH_COPIED` | claude | The credential is a daemon-placed copy; predicate proves expiry lead-time and generation freshness (this is RFC 0165's gate, now one class — see Layer 2). |
| `STATIC_API_KEY` | future | Credential is present, non-empty, and (if probeable) valid; no expiry. |
| `UNKNOWN` | unregistered | Always refuse (quarantine). |

This makes RFC 0121 the `OAUTH_LANE_OWNED_SELF_REFRESH` predicate, RFC 0165 the
`OAUTH_COPIED` predicate, and agy's existing behavior the
`EPHEMERAL_DAEMON_MINTED` predicate — three slices of the same contract instead
of three bespoke code paths.

### 2. Ephemeral fresh-per-spawn credential placement (the prevention)

Rather than retrofit RFC 0165's "persistent copy + freshness gate" onto each
provider, converge the copied-OAuth provider onto agy's model: the daemon owns a
single `CredentialPlacer` step at lane exec.

```text
type CredentialPlacer interface {
    Place(ctx context.Context, laneDir string) error   // mint/copy fresh, owned by lane user, mode 0600
    Cleanup(laneDir string) error
}
```

agy's existing `writeEphemeralGeminiSettings` becomes the first implementation
(zero behavior change); `ClaudeCredentialPlacer` is added alongside it. The
daemon sets `HOME` / `XDG_CONFIG_HOME` to a lane-private prefix
(e.g. `/run/striatum/lanes/<id>/`) at exec, and `Place` writes the credential
there atomically, owned by the lane user, mode `0600`, immediately before exec —
so the file exists (the CLI needs it) but is **spawn-fresh, lane-private, and
re-minted every launch**, never a persistent days-old artifact. For Claude the
"fresh source" is the operator's own credential file (refreshed by the operator's
own CLI), copied at spawn with a generation/rotation-race guard (read source
before and after the copy; refuse `provider_credential_source_unstable` on
mid-copy rotation). Two placement strategies, selectable by deploy config:

- **atomic chown-write** into the per-lane directory (works everywhere, no kernel
  capability required, revocable by deleting the directory); or
- **per-lane tmpfs** in a mount namespace forked at spawn (stronger isolation,
  invisible to other host processes), via a small auditable setuid helper rather
  than granting the daemon `CAP_SYS_ADMIN`.

This is the mechanism that makes #583 structurally impossible without modifying
the provider CLI.

### 3. Daemon-owned secret-free custody, bad-generation memory, and breaker

A provider-agnostic durability/anti-abuse layer that works regardless of auth
model, hardened against a compromised lane (a lane is a sandboxed but untrusted
OS user):

- **Custody receipts** in daemon-owned PostgreSQL keyed
  `(repository_id, provider, lane_user, generation)`: timestamp, verifier result,
  expiry, and a non-secret generation fingerprint (HMAC-SHA256 over credential
  bytes + expiry, daemon-local key). No secret bytes, no raw path, never written
  to a lane-readable file (so it cannot be spoofed via `touch`/mtime or used as an
  exfiltration side channel).
- **Bad-generation immune memory**: an append-only, daemon-write-only blacklist of
  `(provider, lane_user, generation)` that wedged. A blacklisted generation is
  refused even if it parses cleanly — preventing the "rotated-but-not-yet-updated
  credential wedges a second lane" cascade.
- **Tamper-proof circuit breaker**: trip counters live only in daemon state with
  no lane verb to read, reset, or decrement; only the daemon clears them on a
  confirmed fresh generation. Recovery classification
  (`go/pkg/mutations/recovery_decision_tree.go`) folds repeated stale-credential
  stalls into **one** `reseed_required` escalation per `(provider, lane_user)`
  rolling window instead of N generic `agent_mcp_discovery_stall` retries.

The generation concept is exactly the `(provider, lane_user, generation)` key
that RFC 0162 and RFC 0165 both defer. For opaque-credential providers the daemon
cannot fingerprint (codex self-refreshes inside the sandbox), the fallback is a
daemon-assigned monotonic dispatch sequence as the generation, with
lane-reported probe results trusted **advisory-STALE-only, never
authoritative-FRESH** (a lane has no incentive to falsely report itself stale;
FRESH must come from a daemon-side observation where possible).

### Cross-cutting invariant: admission freshness ≠ runtime freshness

A spawn-time gate only proves freshness *at spawn*. #583 also includes the case
where the operator's source credential expires **mid-run** and the lane 401s with
no self-recovery. The contract is therefore explicitly named
`POINT_IN_TIME_ADMISSION`, and one of two runtime closures is required:

- the supervisor-helper periodically re-invokes the adapter's `ValidateReadiness`
  (or re-places the credential) for `OAUTH_COPIED` lanes on its existing
  heartbeat ticker, sending SIGTERM + escalation on a flip to refusal; and/or
- placement treats credential removal as the authoritative end-of-lease signal
  (remove → SIGTERM), so a stale credential is replaced by a clean restart rather
  than a degraded mid-run failure.

## Relationship to adjacent RFCs

- **RFC 0165** becomes the `OAUTH_COPIED` assurance-class implementation +
  `ClaudeCredentialPlacer`; its custody receipt and circuit breaker generalize
  into Layer 3. RFC 0169 supersedes 0165's framing (Claude-as-special-case) with
  Claude-as-one-class.
- **RFC 0121** becomes the `OAUTH_LANE_OWNED_SELF_REFRESH` predicate; the gate
  mechanism it established is reused, not replaced.
- **RFC 0162** supplies the per-provider resolver + expiry parser the contract
  calls, and observes the custody/breaker state it cannot prevent. Detection
  (0162) and prevention (0169) stay separate layers over one key.
- **RFC 0143 / RFC 0168** concern the *Striatum* session token and the lane OS
  principal; this RFC concerns *provider* credentials and composes with both
  without depending on them.

## Acceptance Criteria

- All three current providers launch through one registry-backed gate; adding a
  provider requires implementing one interface and registering it, with no edit
  to the gate or to the former switch sites.
- An unregistered provider is refused at admission with a typed quarantine
  reason, never launched.
- A Claude lane's credential is spawn-fresh and lane-private; a stale persistent
  copy cannot be the cause of a launch (the #583 class is closed by construction,
  verified by a test that ages a pre-existing copy and asserts the placed
  credential is current).
- A dogfood spanning longer than the Claude OAuth access-token TTL completes
  without an `agent_mcp_discovery_stall` caused by a stale lane credential
  (runtime-freshness closure exercised).
- A rotated-and-blacklisted generation is refused on the next launch even though
  it parses; recovery raises exactly one `reseed_required` per `(provider,
  lane_user)` rolling window rather than N generic stalls.
- No credential bytes, OAuth tokens, full operator path, or provider stdout reach
  DB rows, repo artifacts, metrics, events, or doctor output; custody surfaces
  show only redacted generation observations and breaker state.
- `make test` covers: registry completeness/unknown-provider refusal; the codex
  predicate behaving identically post-refactor; spawn-fresh placement vs aged
  copy; source-rotation race during placement; bad-generation refusal; and the
  breaker folding repeated failures into one escalation.

## Implementation Plan

### P0 — Failing fixtures

Tests before behavior: unknown provider refused at the gate; an aged Claude copy
is replaced by a spawn-fresh credential; a source rotation during placement does
not bless a mixed generation; repeated stale-credential launches collapse to one
`reseed_required`; custody output redacts secrets and private paths.

### P1 — Provider Readiness Contract (pure refactor)

`adapter.go` with the interface + assurance-class constants. Extract the existing
codex branch into `OAuthSelfRefreshAdapter` with **zero behavior change**,
register it, and route the gate through the registry. `init()`-based registration
(database/sql-driver style) so the registry is open for extension while the gate
stays closed. Prove the seam before any new logic lands.

### P2 — CredentialPlacer + ephemeral placement

`CredentialPlacer` interface; refactor agy's `writeEphemeralGeminiSettings` to
implement it (no behavior change); add `ClaudeCredentialPlacer` (atomic
chown-write strategy first) + lane-private `HOME` override + the pre-spawn JWT
expiry gate + source-rotation guard.

### P3 — Custody, immune memory, breaker

`GenerationProbe` interface; `credential_custody` + `bad_generation_blacklist`
migrations (daemon-owned); wire dispatch to probe → check blacklist → write
receipt before issuing a packet; teach recovery to classify stale-credential
drift as `reseed_required` and de-duplicate escalations.

### P4 — Runtime-freshness closure + (optional) scheduler dependency

Supervisor-helper re-admission heartbeat for `OAUTH_COPIED` lanes and/or
cleanup-as-lease-boundary; optional tmpfs/setuid-helper placement strategy; and,
if adopted, surface provider-auth readiness as a first-class run dependency so
the RFC 0122 scheduler can park provider-dependent jobs behind a single reseed
gate before spending run setup (see Open Questions).

## Security and Privacy

The credential placer and any hydrator are an intentional privilege boundary
(operator account → lane account). Authority must stay narrow:

- source and destination paths come from provider resolvers, never lane-supplied
  arbitrary paths; the placer holds a short-lived, job-scoped, single-use
  capability minted by the daemon (closes the privilege-bridge / replay attack);
- destination is owned by exactly the configured lane user, mode `0600`; a
  credential is written by fd/atomic-rename or inside a per-lane namespace to
  defeat symlink/TOCTOU;
- custody fingerprints and breaker counters live only in daemon state, never in a
  lane-readable file (closes spoofed-heartbeat and exfil-via-custody side
  channels);
- one credential file per provider per lane; no cross-provider read (private
  per-lane prefix / namespace);
- conservative default: if ownership, path resolution, source stability, parsing,
  or expiry cannot be proven, refuse. A refused launch is cheaper than a
  green-but-doomed lane.

## Open Questions

1. Runtime-freshness closure: re-admission heartbeat, cleanup-as-lease-boundary,
   or both? What re-check interval for `OAUTH_COPIED` lanes?
2. Placement strategy default: atomic chown-write (universal) vs per-lane tmpfs +
   setuid helper (stronger). Per-deploy flag, or pick one?
3. Generation identity for opaque-credential providers (codex): is the
   monotonic-dispatch-seq + advisory-STALE-only model sufficient, or is a
   daemon-side secondary probe required for FRESH confirmation?
4. Minimum expiry lead time for `OAUTH_COPIED` placement: fixed (10/30 min) or a
   fraction of observed TTL?
5. Should provider-auth readiness become a first-class **run dependency**
   (declared `(provider, lane_user)` bindings at `run.start`) so the scheduler
   parks dependent jobs before setup, or stay a launch-time property?
6. Does this RFC subsume RFC 0165 entirely (mark 0165 superseded), or does 0165
   remain the `OAUTH_COPIED` detail doc with 0169 as the umbrella?

## Domain Modeling

Per [`docs/DDD.md` "Adding to the model"](../reference/domain-driven-design.md#adding-to-the-model),
this RFC adds:

- **Credential assurance class** — a value object naming how a provider proves
  freshness (`EPHEMERAL_DAEMON_MINTED`, `OAUTH_LANE_OWNED_SELF_REFRESH`,
  `OAUTH_COPIED`, `STATIC_API_KEY`, `UNKNOWN`).
- **Provider credential generation** — a non-secret value object: a point-in-time
  observation of provider auth material keyed `(provider, lane_user, generation)`.
- **Provider auth readiness dependency** — daemon-owned readiness state that gates
  lane launch and recovery behavior for a `(provider, lane_user)` pair.

All three sit **outside** the Striatum session-token authority model: they
describe whether a lane can start its provider CLI, not who the lane is allowed
to be inside Striatum. Suggested typed domain events:

```text
provider_credential.placed
provider_credential.placement_refused
provider_auth.reseed_required
provider_auth.reseed_cleared
```

## ADHD Ideation Record

This RFC was generated with the `adhd` skill (5 isolated divergent branches under
distinct cognitive frames, then scoring, clustering, and 3 deepened branches),
grounded in a parallel code audit of the per-provider auth models. The brainstorm
is recorded for inspectability; it is not decision authority.

### Reframe

The audit reframed the brief: codex (lane-owned, self-refreshing) and agy
(no provider OAuth; daemon-minted ephemeral) do not have the #583 stale-copy
failure. Claude is the odd one out, and agy already runs the target architecture.
"Prevention for the other lanes" became "a uniform contract + converge Claude onto
agy's model," not "copy the Claude hydrator outward."

### Wide set (clusters, with novelty/viability/fit chips)

- **Uniform contract over heterogeneous providers:** per-provider assurance class
  `[N8 V8 F10]`; `ValidateReadiness` adapter interface `[N7 V9 F10]`; thymus
  self-test thunk `[N8 V8 F10]`; signed freshness attestation `[N7 V7 F9]`;
  guilty-until-proven-fresh / unregistered-quarantined `[N6 V8 F8]`.
- **Custody / waybill (gate reads a manifest, not the secret):** waybill use-by
  `[N7 V8 F9]`; custody-chain receipt `[N7 V8 F9]`; daemon-owned non-lane-readable
  custody `[N7 V9 F9]`; SKU shelf-life (remaining-life ≥ job horizon)
  `[N7 V8 F9]`; telomere TTL-in-file `[N7 V7 F8]`.
- **Daemon-as-custodian / kill the durable lane secret (agy's model):** ephemeral
  namespace-private file `[N8 V6 F8]` ★CLI-compatible; mount-ns-private cred
  `[N7 V6 F8]`. Traps: fd-at-execve `[N9 V3 F7]`, sidecar API proxy `[N8 V3 F6]`,
  local OAuth token-exchange server `[N7 V3 F6]`, blood-brain-barrier socket
  `[N8 V3 F6]`, memfd capability `[N8 V2 F5]`, env-block+LD_PRELOAD `[N8 V3 F5]`.
- **Pre-warm / supply positioning:** par-stock `[N7 V7 F8]`; hormone-pulse
  refresher `[N7 V7 F8]`; demand-forecast pre-warm `[N7 V6 F7]`; milk-run courier
  `[N7 V6 F8]`.
- **Tamper-proof recovery / bad-generation memory:** immune memory of bad
  generations `[N8 V8 F9]`; daemon-owned breaker counts `[N7 V9 F9]`; single-use
  hydrator capability `[N7 V8 F8]`; returns-desk `[N7 V6 F7]`; apoptosis
  self-terminate `[N7 V5 F7]`; plugin-hash allowlist `[N7 V7 F8]` (premature).
- **Graduated assurance → work eligibility:** freshness level gates which jobs a
  lane may claim (ASSURED/DEGRADED/STALE) `[N8 V6 F8]` ★.

### Converge

Recommended stack: **(1)** Provider Readiness Contract (the spine; subsumes
0121/0162/0165), **(2) ★** ephemeral fresh-per-spawn placement (converge Claude
onto agy's model), **(3)** daemon-owned custody + immune memory + tamper-proof
breaker. Traps rejected: killing the credential file entirely (infeasible against
third-party CLIs that read files); background timer as the correctness boundary;
plugin-hash allowlist (no dynamic plugins exist); milk-run "hit the vendor
refresh endpoint" (reimplements vendor OAuth — re-copy the operator file
instead).

### Provocation

All three deepened branches independently surfaced the same unbuilt idea (also
RFC 0165's parting question): make provider-auth a first-class **run dependency**
so the RFC 0122 scheduler parks dependent jobs behind one reseed gate before
spending setup — turning "8 lanes each wedge on the same dead token" into "the run
never starts until the credential is fresh." Open Question 5 carries this forward.

No code lands with this proposal.
