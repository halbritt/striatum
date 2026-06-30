---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
author: adjudicator-author-001
workflow: "rfc-0165-design-v3"
run_id: "run_453bf8ee937677d91c0d1bd87826181b"
cycle: 1
topic: "RFC 0165 Claude provider credential freshness hydration for GH #583 (v3 revision)"
participants:
  - "holder-author-001"
  - "falsifier-reviewer-001"
  - "falsifier-reviewer-002"
  - "adjudicator-author-001"
entries:
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: >-
      The v3 holder revises the SPEC to deliver lanes an access-token-only
      projection (B1 lane-owned 0600 file or B2 SO_PEERCRED broker socket) that
      never carries a refresh token; refresh authority is held in exactly one
      place (the operator-side credential owner) and the daemon adds no OAuth
      client. Recovery performs a current freshness check from the daemon-owned
      provider_auth_dependencies row before incrementing generic
      requeue/transfer budget, and a periodic daemon-owned recovery/reconcile
      sweep surfaces running-lane near-expiry as provider-auth debt. Claimed
      carry-forwards (spawn-time gate, RFC 0096 trust boundary,
      provider_auth_gate=off non-bypass, redacted custody receipts with
      refresh_token_absent) are preserved, and the named concurrent/subsequent
      RTR + runtime-expiry tests are specified.
  - kind: challenge
    by: "falsifier-reviewer-001"
    refs: ["dialogue:2"]
    correspondence: landed_unrebutted
    text: >-
      Falsifier 1 (F1 lens) shows the recovery freshness check is not purely
      daemon-owned. When the provider_auth_dependencies row is stale, the v3
      hook falls back to "one bounded SampleLaneCredential re-read of the lane
      projection expiry" and labels it "daemon-owned, never lane-authored."
      Current source contradicts that: sampler.go reads the credential file AS
      THE LANE USER and SampleLaneCredential parses only HasExpiry/ExpiresAt
      from the JSON with no daemon MAC, receipt id, or generation check of the
      sampled bytes; expiry.go accepts claudeAiOauth.expiresAt / top-level
      expiresAt from that payload. The B1 projection is a lane-owned 0600 file,
      so a lane process (or stale orphan / provider rewrite) can set expiresAt
      to a future value with no refresh token, upgrading a stale daemon row to
      "fresh." Recovery then falls through to recordRecoveryAction, incrementing
      requeue_count/transfer_count for a runtime provider-auth cause — the exact
      F1 leak. The check is therefore neither race-free nor positively
      daemon-owned.
  - kind: challenge
    by: "falsifier-reviewer-002"
    refs: ["dialogue:3"]
    correspondence: landed_unrebutted
    text: >-
      Falsifier 2 (F2 lens) shows F2 is discharged only for the distinct-UID
      lane shape. The whole F2 proof hinges on "the lane OS user is a distinct
      uid and has no read path to the operator source," but the spec still names
      same-user collapse as a supported destination mode (verify-only no-op,
      HOLDER.md:285-286) and runs runSuperviseClaudeCredentialGate only when a
      distinct lane OS user is configured (HOLDER.md:336-339). Current source
      keeps same-user lanes live and supported: supervision_env.go:228-237
      collapses unset/same-as-daemon STRIATUM_LANE_OS_USER to empty RunAsUser
      and :259-265 executes the command directly with no sudo -u split;
      spec.md:2638-2641 documents the preserved same-user behavior and
      command-authority-matrix.md:426 even presents unsetting
      STRIATUM_LANE_OS_USER as an operator remediation. In same-user mode the
      lane IS the operator user: its Claude CLI resolves the operator source
      credential, reads the raw refreshToken, and can independently rotate the
      operator credential family via the normal OAuth refresh flow — exactly the
      raw refresh-token custody and operator-source invalidation C1/F2 forbid
      "by ANY route." F1's recovery change does not repair this custody model.
  - kind: constraint
    by: "adjudicator-author-001"
    refs: ["dialogue:2"]
    text: >-
      Revision must make daemon-owned state the only positive freshness
      authority in recovery: a stale / missing / internally inconsistent
      provider_auth_dependencies (or broker) row fails closed to
      unverifiable/reseed_required and must NOT consume generic
      requeue/transfer budget. A lane-file re-sample may be used only as a
      downgrade-only (negative) signal — never to upgrade a stale or missing
      daemon row to fresh — unless the design adds a daemon-stamped,
      non-lane-forgeable receipt/generation check proving the sampled bytes are
      exactly the daemon's last projection. Make the provider-auth branch
      ordering explicit so it runs before any generic requeue/transfer for
      Claude agent_mcp_discovery_stall. Add
      TestRecoveryRejectsLaneAuthoredProjectionFreshness and a stale/missing
      dependency-row test.
  - kind: constraint
    by: "adjudicator-author-001"
    refs: ["dialogue:3"]
    text: >-
      Revision must make an explicit same-user decision for Claude OAuth
      self-driving lanes. Preferred: fail closed when config.RunAsUser == "" or
      resolves to the daemon/operator identity, with a typed launch-precondition
      error (e.g. provider_credential_same_user_unsupported) refused BEFORE
      scratch, session-token minting, supervisor rows, helper/tmux, or process
      launch. Alternatively, move the refresh source behind a daemon-only
      boundary the lane uid cannot read and deliver only via the broker /
      access-token path (a 0600 operator-home file under the same uid cannot be
      that boundary). Extend the no-refresh-token and RTR tests with same-user
      fixtures, and have them scan every lane-readable credential surface named
      by the launch environment — not only the newly written projection file.
verdict: "needs_revision"
rationale: >-
  The v3 revision is a serious, mostly correct redesign: moving lanes to an
  access-token-only projection (B1 file / B2 SO_PEERCRED broker), keeping refresh
  authority single-writer operator-side with no daemon OAuth client, and adding a
  daemon-row recovery-time freshness classification genuinely fixes the
  distinct-UID custody desync (F2) and the nominal launch-fresh-then-expire path
  (F1). The carry-forwards are preserved and unregressed. But a clearing verdict
  requires BOTH F1 and F2 genuinely discharged, and two material challenges land
  unrebutted by the SPEC as written. F2 is closed only for distinct-UID lanes:
  the still-supported, documented same-user mode leaves a Claude lane reading the
  raw operator refresh token and able to rotate the operator credential family
  directly — raw refresh-token custody "by ANY route," which F2/C1 forbid. F1's
  recovery hook gives itself a lane-authored escape hatch: a stale daemon row may
  be upgraded to "fresh" by re-sampling a lane-owned 0600 projection whose
  expiresAt a lane can forge without a refresh token, reopening the generic
  budget-burn leak; the freshness check is thus not race-free or
  positively daemon-owned as claimed. Both attack the core correctness boundary,
  not polish, so the dialogue does not clear. This is the single allowed v3
  revision cycle; a second needs_revision ends the gate uncleared and routes to
  the operator for a fresh -v4 run with a revising holder.
findings:
  - id: F1-RUNTIME-EXPIRY-CIRCUIT-BREAKER
    severity: high
    posture: design
    status: open
    source_refs: ["dialogue:2"]
    affected_invariants:
      - "stale Claude provider auth must become readiness debt, not generic MCP discovery retry"
      - "recovery must not burn generic retry budget for provider-auth causes"
      - "the runtime freshness check must be race-free and sourced from daemon-owned state, never lane-authored claims"
    challenge: >-
      v3 fixes the nominal path (recovery computes current expiry from the
      daemon provider_auth_dependencies row before recordRecoveryAction), but its
      stale-row fallback re-reads the lane-owned 0600 projection via
      SampleLaneCredential — which reads as the lane user and parses a
      lane-forgeable expiresAt with no daemon MAC/generation verification — and
      treats that as positive "daemon-owned" freshness. A lane can set a future
      expiresAt with no refresh token, upgrading a stale row to fresh and
      falling through to generic requeue/transfer budget burn. The check is not
      race-free and not positively daemon-owned.
    closest_acceptable_answer: >-
      Daemon-owned state (provider_auth_dependencies / broker state) is the only
      positive freshness authority. A stale/missing/inconsistent row fails
      closed to unverifiable/reseed_required without consuming generic
      requeue/transfer budget. A lane-file re-sample is downgrade-only unless a
      daemon-stamped, non-lane-forgeable receipt/generation check proves the
      sampled bytes are the daemon's last projection. The provider-auth branch
      runs before any generic requeue/transfer for Claude
      agent_mcp_discovery_stall, proven by
      TestRecoveryRejectsLaneAuthoredProjectionFreshness plus a stale/missing
      daemon-row test.
    requested_constraint_shape:
      kind: gate
  - id: F2-REFRESH-TOKEN-ROTATION-DESYNC
    severity: critical
    posture: design
    status: open
    source_refs: ["dialogue:3"]
    affected_invariants:
      - "no lane may obtain raw refresh-token custody by ANY route"
      - "a lane must not independently rotate or invalidate the operator source credential family"
      - "concurrent and subsequent lanes must not desynchronize the operator source"
    challenge: >-
      The access-token-only projection discharges F2 only for distinct-UID
      lanes. Same-user supervised lanes remain a supported, documented (and
      operator-recommended) mode in which RunAsUser collapses to empty, the lane
      runs as the operator user with no sudo -u identity split, and its Claude
      CLI resolves the very operator source credential. That lane reads the raw
      refreshToken and can independently rotate the operator credential family
      via the normal OAuth refresh flow. The v3 gate is a verify-only no-op /
      skipped in this mode, so raw refresh-token custody persists by source-read
      route.
    closest_acceptable_answer: >-
      For Claude OAuth self-driving lanes, fail closed when RunAsUser == "" or
      resolves to the daemon/operator identity (typed launch-precondition error
      refused before scratch/token-mint/supervisor rows/process), OR move the
      refresh source behind a daemon-only boundary the lane uid cannot read and
      deliver only the brokered access token. Add same-user no-refresh-token and
      same-user RTR tests that scan every lane-readable credential surface named
      by the launch environment.
    requested_constraint_shape:
      kind: gate
constraints:
  - id: C1-NO-LANE-RAW-REFRESH-TOKEN-CUSTODY
    posture: credential_custody
    severity: critical
    kind: gate
    binding: true
    source_finding: F2-REFRESH-TOKEN-ROTATION-DESYNC
    source_refs: ["dialogue:3"]
    text: >-
      The revised spec must prevent a Claude lane from obtaining raw refresh-token
      custody by ANY route, including the same-user lane shape. Either declare
      Claude OAuth same-user self-driving lanes unsupported and fail closed
      before launch (typed error such as provider_credential_same_user_unsupported,
      refused before scratch/session-token minting/supervisor rows/helper/tmux/
      process), or define a same-user design where the refresh source lives behind
      a daemon-only boundary the lane uid cannot read and only the brokered access
      token reaches the lane. The distinct-UID access-token-only projection (B1/B2)
      is accepted as sound and must be carried forward.
    verification:
      gate: "Same-user fixture: operator source carries a known refreshToken, STRIATUM_LANE_OS_USER unset or same-as-daemon; supervise.start refuses (or the lane provably cannot read the refresh token) before any Claude process starts. Same-user and distinct-UID RTR tests show no lane stores or rotates a raw refresh token and the operator source stays valid."
    final_review_required: true
  - id: C2-RUNTIME-FRESHNESS-DAEMON-ONLY-POSITIVE-AUTHORITY
    posture: recovery
    severity: high
    kind: gate
    binding: true
    source_finding: F1-RUNTIME-EXPIRY-CIRCUIT-BREAKER
    source_refs: ["dialogue:2"]
    text: >-
      The revised spec's recovery-time provider-auth freshness classification must
      treat daemon-owned state as the only positive freshness authority. A stale,
      missing, or internally inconsistent provider_auth_dependencies/broker row
      must fail closed to unverifiable/reseed_required and must not increment
      generic requeue_count/transfer_count or escalate recovery_exhausted. A
      lane-file re-sample may serve only as a downgrade (negative) signal unless a
      daemon-stamped, non-lane-forgeable receipt/generation check proves the
      sampled bytes are exactly the daemon's last projection. The provider-auth
      branch must be ordered before any generic requeue/transfer for Claude
      agent_mcp_discovery_stall.
    verification:
      gate: "TestRecoveryRejectsLaneAuthoredProjectionFreshness: launch with a 35m projection, advance 45m, mutate the lane projection expiresAt to a future value, trigger agent_mcp_discovery_stall, and assert recovery sets reseed_required/unverifiable without incrementing requeue/transfer or escalating recovery_exhausted. A stale/missing daemon-row test asserts the same fail-closed-to-debt behavior."
    final_review_required: true
  - id: C3-TEST-MATRIX-COVERS-SAME-USER-AND-LANE-AUTHORED-FRESHNESS
    posture: verification
    severity: high
    kind: gate
    binding: true
    source_finding: F2-REFRESH-TOKEN-ROTATION-DESYNC
    source_refs: ["dialogue:2", "dialogue:3"]
    text: >-
      The revised proposal's required tests must add (a) a same-user fixture for
      the no-refresh-token assertion that scans every lane-readable credential
      surface named by the launch environment, not only the newly written
      projection file; (b) a same-user RTR test whose expected result is refusal
      before launch (or provable lane inability to read the refresh token), never
      "operator source changed due to a lane action"; and (c) the
      lane-authored-freshness and stale/missing-daemon-row recovery tests from C2.
      Keep the existing v3 distinct-UID RTR, redaction, source-rotation-race,
      owner/mode, symlink-escape, provider_auth_gate=off, trust-boundary, and
      resolver-path tests.
    verification:
      expected_stage: "The next PROPOSAL.md names these tests and maps each to the source modules and state transitions it exercises."
    final_review_required: true
branches:
  design: blocked
---

# Collaboration Ledger - RFC 0165 design run v3 (cycle 1)

author: adjudicator-author-001

## Verdict

**verdict: needs_revision**

The v3 revision substantially improved on v1. Replacing the whole-credential
file copy with an **access-token-only projection** (B1 lane-owned `0600` file
with no `refreshToken` key, or B2 daemon-owned `SO_PEERCRED` broker socket),
holding refresh authority single-writer on the operator side, refusing to add a
daemon OAuth client, and adding a **daemon-row recovery-time freshness
classification** are the right structural moves. For the **distinct-UID** lane
shape — the live dogfood configuration — F2 is genuinely discharged, and the
nominal launch-fresh-then-expire-mid-session F1 path is genuinely classified
from daemon state before generic budget burn. No carry-forward regressed.

That is not enough to clear this gate. A clearing verdict (`accept` /
`accept_with_findings`) requires **both** F1 and F2 genuinely discharged, and
two material falsifier challenges land unrebutted by the SPEC as written.

## Per-finding determination

### F2 — no lane raw-refresh-token custody (critical) → **OPEN (NOT discharged)**

`falsifier-reviewer-002` shows the F2 proof depends entirely on a distinct lane
OS uid. The SPEC itself still lists **same-user collapse** as a supported
destination mode (a "verify-only no-op", `HOLDER.md:285-286`) and runs the new
gate **only when a distinct lane OS user is configured** (`HOLDER.md:336-339`).
Current source confirms same-user mode is live and supported, not hypothetical:
`supervision_env.go:228-237` collapses an unset / same-as-daemon
`STRIATUM_LANE_OS_USER` to empty `RunAsUser`; `:259-265` then executes the
supervised command directly with **no `sudo -u` identity split**;
`spec.md:2638-2641` documents the preserved same-user behavior; and
`command-authority-matrix.md:426` even presents unsetting `STRIATUM_LANE_OS_USER`
as an operator remediation. In that mode the lane **is** the operator user: its
Claude CLI resolves the operator source `.credentials.json`, reads the raw
`refreshToken`, and can independently rotate the operator credential family via
the normal OAuth refresh flow. That is precisely the raw refresh-token custody
and operator-source invalidation F2/C1 forbid "by **any** route." F1's recovery
change does not perturb or repair this custody model. The challenge is material
(it attacks the F2 correctness boundary directly) and unrebutted (the v3 holder
neither marks same-user Claude OAuth unsupported/fail-closed nor closes the
source-read path). **F2 remains open.**

### F1 — runtime-expiry circuit breaker (high) → **OPEN (NOT fully discharged)**

`falsifier-reviewer-001` concedes the v3 holder "mostly fixes" the direct F1
case when the daemon `provider_auth_dependencies` row is authoritative — a DB
timestamp comparison is cheap and daemon-owned. The standing gap is the
**stale-row fallback**: the hook permits "one bounded `SampleLaneCredential`
re-read of the lane projection expiry" and calls it "daemon-owned, never
lane-authored." Current source refutes that label — `sampler.go` reads the file
**as the lane user**, `SampleLaneCredential` returns only `HasExpiry`/`ExpiresAt`
parsed from the JSON with **no** daemon MAC / receipt id / generation check of
the sampled bytes, and `expiry.go` accepts the `claudeAiOauth.expiresAt` /
top-level `expiresAt` field a lane-owned `0600` file can carry. A lane process
(or stale orphan, or provider-side rewrite) can set `expiresAt` to a future
value **without any refresh token**, upgrading a stale daemon row to "fresh" so
recovery falls through to `recordRecoveryAction` and increments
`requeue_count`/`transfer_count` for a runtime provider-auth cause — the exact
F1 failure mode. The freshness check is therefore neither race-free nor
positively daemon-owned as claimed. The challenge is material and unrebutted.
**F1 remains open** (nominal path resolved; lane-authored positive-freshness
escape hatch reopens it).

## Carry-forward determination (v1 work — all INTACT, none regressed)

- **Spawn-time freshness gate — INTACT.** The gate, its placement before
  supervisor rows / scratch / token-mint / process, and the typed refusal
  vocabulary are preserved for the projection model. (Its same-user no-op
  behavior is the F2 finding above, not a regression of the carry-forward.)
- **RFC 0096 / #135 / #296 trust boundary — INTACT.** `falsifier-reviewer-002`
  explicitly found no daemon/admin token leak in the projection path; B2 uses
  `SO_PEERCRED` uid identity, not a Striatum capability token. Provider OAuth
  stays separate from control-plane tokens.
- **`provider_auth_gate=off` cannot bypass — INTACT.** Projection/freshness is
  independent of that flag; only a separate `provider_credential_projection=off`
  emergency flag (default `auto`, marks dependency `disabled`, emits a redacted
  bypass event, never implied by `provider_auth_gate=off`) can skip it.
- **Redacted, private-safe custody receipts — INTACT.** No raw OAuth material in
  DB rows / artifacts / metrics / events / doctor; the refresh token is never
  read into the projector; a `refresh_token_absent_ok` assertion and a
  table-driven redaction test are specified.

## New material challenges

Both falsifier challenges are re-attacks within the scope of the carried-forward
findings rather than wholly new findings: Falsifier 1 sharpens F1 (lane-authored
positive-freshness fallback); Falsifier 2 sharpens F2 (the same-user source-read
route the projection does not cover). Both **stand unrebutted**. No additional,
unrelated material challenge was raised.

## Binding revision constraints (carry into a v4 run)

1. **C1 — close the same-user refresh-token route.** Make a Claude OAuth
   same-user decision: fail closed before launch (preferred), or put the refresh
   source behind a daemon-only boundary the lane uid cannot read and deliver only
   the brokered access token. Preserve the accepted distinct-UID B1/B2 design.
2. **C2 — daemon state is the only positive freshness authority.** A
   stale/missing/inconsistent daemon row fails closed to
   `unverifiable`/`reseed_required` without burning generic requeue/transfer
   budget; a lane-file re-sample is downgrade-only unless a daemon-stamped,
   non-lane-forgeable receipt/generation check proves the sampled bytes are the
   daemon's last projection; order the provider-auth branch before any generic
   requeue/transfer for Claude `agent_mcp_discovery_stall`.
3. **C3 — extend the test matrix** with same-user no-refresh-token (scanning
   every lane-readable credential surface), a same-user RTR test that must end in
   refusal-before-launch, and the lane-authored-freshness and
   stale/missing-daemon-row recovery tests.

## Preserve (v3 got these right — do not regress in v4)

- The access-token-only projection model for distinct-UID lanes (B1 file with no
  `refreshToken`; B2 `SO_PEERCRED` broker socket), with F2 discharged identically
  under either delivery.
- Single-writer refresh authority held operator-side; the explicit non-goal of a
  daemon OAuth client (no second refresh-token writer).
- Daemon-row recovery-time freshness classification for the nominal path and the
  daemon-owned recovery/reconcile decay sweep reading `expires_at` from
  `provider_auth_dependencies` (never a lane heartbeat claim or provider stdout).
- The spawn-time gate placement, typed refusal vocabulary, redaction contract
  (`refresh_token_absent_ok`), the `provider_credential_projection=off` boundary,
  and the distinct-UID RTR / source-rotation-race / owner-mode / symlink-escape /
  trust-boundary / resolver-path tests.

## Gate status

This was the single allowed v3 revision cycle. With `needs_revision`, the gate
ends **uncleared** and routes to the operator, who should spin a fresh `-v4`
run with a revising holder that discharges the two constraints above while
preserving the v3 work listed under **Preserve**.
