# Design-Run Seed - RFC 0165 (REVISION v5)

This is the fresh v5 continuation for RFC 0165 (Claude provider credential
freshness + spawn-time hydration, #583). It resumes from the banked v4 dialogue
on `origin/backup/rfc-0165-design-v4-2026-06-24`, commit
`da9f9da04a4bf31d0ac59fb0c3ce671d44e8046b`.

v5 is a surgical revision, not a restart. Do not re-derive the earlier design
from scratch. Preserve the v4-cleared spine and resolve only the v4 ledger's
remaining material constraints plus the non-blocking C2 ordering caveat.

Required context docs:

- `context/v4_HOLDER.md` - the v4 SPEC to revise.
- `context/v4_FALSIFIER_1.md` - C1 latest-row challenge.
- `context/v4_FALSIFIER_2.md` - C3 projection-disabled raw-token challenge.
- `context/v4_LEDGER_cycle_1.md` - binding v4 verdict and constraints.
- `context/v3_HOLDER.md` and `context/v3_LEDGER_cycle_1.md` - earlier accepted
  spine and constraints.
- `docs/rfcs/0165-claude-provider-credential-freshness.md` - authoritative RFC.
- `docs/rfcs/0169-provider-agnostic-lane-credential-readiness.md` - adjacent
  provider-readiness spine; keep separate for this run.

## Banked Source Handles

All banked context was copied from commit
`da9f9da04a4bf31d0ac59fb0c3ce671d44e8046b` on
`origin/backup/rfc-0165-design-v4-2026-06-24`.

| Blob | Source path | v5 context path |
|---|---|---|
| `bf7d5d191586f24e6fa60965cccbbabb00a339dd` | `docs/operator/workflows/rfc-0165-design-v4/SEED.md` | superseded by this seed |
| `6eeb52992a245d52baf97e5c78962636083b574c` | `docs/operator/artifacts/rfc-0165-design-v4/dialogue/holder/HOLDER.md` | `context/v4_HOLDER.md` |
| `da644dc007be161a12e5a1a69c7efb58246e38a9` | `docs/operator/artifacts/rfc-0165-design-v4/dialogue/falsifier_1/FALSIFIER.md` | `context/v4_FALSIFIER_1.md` |
| `916317db1dd242217b933f432fbdbc65f77657de` | `docs/operator/artifacts/rfc-0165-design-v4/dialogue/falsifier_2/FALSIFIER.md` | `context/v4_FALSIFIER_2.md` |
| `2626453e0a9de4f43ddeac1ed9a4d8ea38f8c017` | `docs/operator/artifacts/rfc-0165-design-v4/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md` | `context/v4_LEDGER_cycle_1.md` |
| `841916f6fb31178fa86ec758291ae1ce963ee071` | `docs/operator/artifacts/rfc-0165-design-v3/dialogue/holder/HOLDER.md` | `context/v3_HOLDER.md` |
| `c39f2d7916c708b29e291cf4ab547893cd69bf8d` | `docs/operator/artifacts/rfc-0165-design-v3/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md` | `context/v3_LEDGER_cycle_1.md` |
| `c1c0d42c7881c65eab8261a9e3a4c450d61079d3` | `docs/operator/artifacts/rfc-0165-design-v2/QUARANTINE.md` | `context/v2_QUARANTINE.md` |

## Relationship To RFC 0169

RFC 0169 is the provider-agnostic lane credential-readiness spine. RFC 0165 v5
stays Claude-specific because the roadmap keeps RFC 0165 and RFC 0169 separate:
0165 closes the copied-Claude-OAuth incident shape; 0169 later generalizes the
provider registry and readiness contract.

The v5 holder must name the seam so the Claude assurance class can plug into
RFC 0169 later, but it must not defer the v5 constraints to RFC 0169.

## Preserve - Cleared Through v3/v4

Do not reopen or weaken these claims unless a falsifier shows a new material
regression:

- Distinct-UID Claude lanes use an access-token-only projection: B1 lane-owned
  `0600` file or B2 `SO_PEERCRED` broker response, never a refresh token.
- Same-user Claude OAuth self-driving lanes are unsupported and fail closed.
  The load-bearing argument stands: no in-uid boundary can prevent a same-uid
  lane process from reading the operator's real Claude credential.
- Lane-side credential samples are downgrade-only. They may make freshness look
  worse; they may never prove positive freshness.
- Path and ownership rules, spawn-time projection placement, F1 runtime-expiry
  circuit breaker plus decay, durable redacted receipts, redaction, RFC 0096
  control-plane separation, and `refresh_token_absent` remain intact.
- No raw OAuth material, provider stdout/stderr, transcripts, full private
  operator paths, Striatum control-plane tokens, or private diagnostics may enter
  DB rows, events, artifacts, metrics, doctor output, dashboard output, or repo
  files.

## Binding V5 Constraints

### C1 - Bind Positive Freshness To The Stalled Launch Generation

v4 still read positive recovery freshness from a singleton latest
`provider_auth_dependencies` row keyed by lane user and destination selector.
That is daemon-owned, but it can be the wrong proof. A newer same-lane-user
launch can write generation G2 while an older stalled process still runs on
expired generation G1. Recovery for the older job must not read G2 and classify
G1 as fresh.

The v5 SPEC must bind positive freshness to the stalled job, supervisor, or
session launch credential generation. Persist enough redacted launch evidence
for recovery to evaluate the exact launched generation: receipt id, delivery
mode, destination generation id, expiry, and source generation. A newer row may
prove a fresh relaunch is possible; it must never prove the older stalled
process fresh.

Required tests:

- `TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection`
- A per-session or per-generation decay/reseed-debt test proving provider-auth
  expiry does not increment generic requeue or transfer counters.

### C3 - Close The Projection-Disabled Raw-Token Route

v4 kept `provider_credential_projection=off` as a distinct-UID Claude launch
path. That leaves the original incident shape open: a lane-readable whole
credential in `$HOME/.claude/.credentials.json` or `$CLAUDE_CONFIG_DIR` can
contain a raw `refreshToken`, and disabling projection means no access-token-only
file, broker, scrub, or all-surfaces scan necessarily runs before process start.

The v5 SPEC must close this path. Preferred answer: for Claude OAuth lanes,
`provider_credential_projection=off` fails closed before scratch creation,
session-token minting, supervisor rows, helper/tmux, or provider process launch,
with a typed launch-precondition refusal. If v5 keeps a Claude projection-off
launch path, it must first prove `refresh_token_absent` across every
lane-readable credential surface named by the launch environment: resolver
destination, `$CLAUDE_CONFIG_DIR`, `$HOME/.claude`, credential-bearing env
entries, helper or broker settings, inherited fd/config paths, and any
provider-specific credential path.

Required tests:

- `TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface`
- `TestProjectionOffStillValidatesClaudeConfigDir`
- A positive control proving normal distinct-UID access-token-only projection
  still launches.

### C2 Ordering - Same-User Refusal Before Generic Provider-Auth Gate

v4's same-user safety argument is sound, but the gate ordering must be explicit.
Same-user Claude OAuth refusal must run before the generic or Codex-only
provider-auth gate and before any side effect, so every mode reports the typed
`provider_credential_same_user_unsupported` floor with the distinct-lane-user
remediation.

Required test:

- `TestSameUserClaudeLaneRefusedBeforeSideEffects`, covering
  `provider_auth_gate=auto`, `off`, and `required`.

## Falsifier Guidance

Falsifier 1 should attack the C1 overlapping-session race and C2 ordering:
can a fresh G2 latest row still make an expired G1 stalled session look fresh,
or can same-user refusal still surface as a generic provider-auth failure?

Falsifier 2 should attack the C3 projection-disabled route and carry-forward
regression: can any Claude OAuth process launch with an unscanned whole
credential in lane-readable HOME or `CLAUDE_CONFIG_DIR`, or did the fix break
the normal access-token-only projection path?

The adjudicator clears v5 only if C1, C3, and the C2 ordering caveat are each
resolved with falsifiable tests and all v3/v4 carry-forwards remain intact.

## Build Boundary

This is a design workflow. Lanes write only v5 design artifacts. If the cleared
SPEC requires new RPC methods, routes, durable tables, error codes, generated
contracts, or handwritten authority paths, the later build workflow must include
the relevant source/doc write scopes and update
`docs/reference/command-authority-matrix.md` plus the authority guardrail tests.
