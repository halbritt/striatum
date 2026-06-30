---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
author: adjudicator-author-002
workflow: "rfc-0165-design-v5"
run_id: "run_efde0bcac1a8712b90c94e22e9f5db97"
session_id: "sess_ae59c4de4cca05609e9edc7ea054797f"
cycle: 2
topic: "RFC 0165 Claude provider credential freshness v5: launch-bound recovery freshness and projection-off classifier closure"
participants:
  - "holder-author-001"
  - "falsifier-reviewer-001"
  - "falsifier-reviewer-002"
  - "adjudicator-author-002"
entries:
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: >-
      The v5 holder preserves the accepted distinct-UID access-token-only
      projection spine, same-user fail-closed policy, downgrade-only lane
      samples, redacted receipts, RFC 0096 separation, and refresh-token-absent
      custody contract. It claims C1 is closed by daemon-owned launch credential
      bindings tied to the stalled job/session/supervisor generation; C3 is
      closed because provider_credential_projection=off refuses Claude OAuth
      self-driving lanes before any side effect; and C2 is closed because the
      same-user Claude OAuth precondition runs before the generic provider-auth
      gate in auto, off, and required modes.
  - kind: challenge
    by: "falsifier-reviewer-001"
    refs: ["dialogue:2"]
    correspondence: "not_material"
    text: >-
      Falsifier 1 does not sustain the v4 C1 latest-row attack against v5. The
      remaining challenge is narrower: the holder calls same-user refusal the
      first Claude credential floor, but the proposed order leaves
      enforceLaneCredentialDomain before the same-user precondition. A same-user
      Claude OAuth launch with CLAUDE_CONFIG_DIR or
      CLAUDE_SECURESTORAGE_CONFIG_DIR resolving inside the target repository can
      fail with a credential-domain error before
      provider_credential_same_user_unsupported. This should be declared and
      tested, but it is a fail-closed, pre-side-effect launch-precondition
      ordering issue, not a custody reopening and not the generic provider-auth
      gate ordering defect carried from v4.
  - kind: challenge
    by: "falsifier-reviewer-002"
    refs: ["dialogue:3"]
    correspondence: "landed_unrebutted"
    text: >-
      Falsifier 2 lands the material C3 challenge. The holder makes projection
      disabled fail closed only for lanes already classified as Claude OAuth,
      while preserving a non-Claude or non-OAuth diagnostic exception. The SPEC
      does not define a fail-closed pre-launch classifier proving that a
      projection-disabled Claude self-driving lane is non-OAuth before launch.
      Current launch shape does not carry an explicit credential kind, while the
      resolver and credential-domain guard model Claude credential surfaces as
      OAuth-shaped. If missing, unknown, or unmodeled kind falls through as
      diagnostic non-OAuth, the old projection-off route can still start a
      distinct-UID Claude process with a lane-readable whole credential under
      HOME, CLAUDE_CONFIG_DIR, or CLAUDE_SECURESTORAGE_CONFIG_DIR.
  - kind: constraint
    by: "adjudicator-author-002"
    refs: ["dialogue:3"]
    text: >-
      C3 carries into revision: for adapter == claude and
      agent_loop_mode == self_driving, missing, unknown, or unmodeled
      credential kind must be treated as OAUTH_COPIED/Claude OAuth for RFC 0165
      admission. provider_credential_projection=off must refuse before scratch,
      FIFO/ACL work, session-token minting, supervisor rows, projection receipt
      creation, helper/tmux setup, or provider process launch unless Striatum has
      positive pre-launch proof that no Claude OAuth resolver surface is in play.
  - kind: constraint
    by: "adjudicator-author-002"
    refs: ["dialogue:2"]
    text: >-
      C2 carries only as a non-blocking policy clarification: either move
      same-user Claude OAuth refusal before enforceLaneCredentialDomain, or
      explicitly declare credential-domain violations a higher-priority
      fail-closed precondition. The selected precedence needs a same-user
      CLAUDE_CONFIG_DIR / CLAUDE_SECURESTORAGE_CONFIG_DIR test proving the error
      code and the no-side-effect boundary.
verdict: "needs_revision"
rationale: >-
  The gate cannot clear. C1 is discharged because the holder binds positive
  recovery freshness to the stalled launch credential binding rather than the
  latest lane-user dependency row, keys provider-auth debt by binding and
  destination generation, treats lane samples as downgrade-only, and names
  falsifiable overlapping-generation and per-session decay tests. The core v4
  C2 generic-provider-auth ordering caveat is also discharged: the holder places
  same-user Claude OAuth refusal before runSuperviseProviderAuthGate in auto,
  off, and required modes and before launch side effects. Falsifier 1's
  credential-domain precedence issue is real enough to preserve as an explicit
  policy/test constraint, but it fails closed before side effects and does not
  reopen raw refresh-token custody. The material blocker is Falsifier 2's C3
  classifier gap. The holder's projection-off closure depends on knowing a
  launch is Claude OAuth, but the SPEC does not make absent, unknown, or
  unmodeled Claude credential kind fail closed. A build could therefore treat an
  unclassified Claude launch as diagnostic non-OAuth and allow
  provider_credential_projection=off to start a distinct-UID process that reads
  a whole OAuth credential from HOME, CLAUDE_CONFIG_DIR, or
  CLAUDE_SECURESTORAGE_CONFIG_DIR. That is the raw-refresh-token custody route
  C3 exists to close. The accepted carry-forwards remain intact on their stated
  paths: same-user unsupported fail-closed, normal distinct-UID
  access-token-only projection, path and ownership rules, downgrade-only lane
  samples, F1 decay, durable token-free receipts, redaction, RFC 0096
  separation, and refresh_token_absent.
findings:
  - id: C3-PROJECTION-OFF-UNKNOWN-KIND-FALLTHROUGH
    severity: critical
    posture: credential_custody
    status: open
    source_refs: ["dialogue:3"]
    affected_invariants:
      - "provider_credential_projection=off must not be a Claude OAuth raw-token bypass"
      - "missing or unmodeled credential kind is not proof that Claude OAuth discovery is impossible"
      - "no lane may obtain raw refresh-token custody through HOME, CLAUDE_CONFIG_DIR, CLAUDE_SECURESTORAGE_CONFIG_DIR, helper settings, inherited credential paths, or other lane-readable resolver surfaces"
    challenge: >-
      The v5 holder fails closed for projection-disabled launches already known
      to be Claude OAuth, but it leaves a non-Claude/non-OAuth diagnostic
      exception without defining a positive pre-launch proof source for
      credential kind. Because the current launch config lacks an explicit kind
      and current Claude resolver surfaces are OAuth-shaped, treating missing or
      unknown kind as diagnostic non-OAuth would allow projection-disabled
      Claude launch to proceed with a lane-readable whole credential containing a
      refreshToken.
    closest_acceptable_answer: >-
      For adapter == claude and agent_loop_mode == self_driving, treat missing,
      unknown, or unmodeled credential kind as OAUTH_COPIED/Claude OAuth for RFC
      0165 admission. Permit a projection-disabled non-OAuth diagnostic launch
      only after positive pre-launch proof that no Claude OAuth resolver surface
      is in play. The refusal must happen before scratch, FIFO/ACL work,
      session-token minting, supervisor rows, projection receipt creation,
      helper/tmux setup, or provider process launch.
    requested_constraint_shape:
      kind: gate
  - id: C2-CREDENTIAL-DOMAIN-PRECEDENCE-AMBIGUITY
    severity: medium
    posture: launch_precondition
    status: accepted
    source_refs: ["dialogue:2"]
    affected_invariants:
      - "same-user Claude OAuth remediation should be predictable across fail-closed launch preconditions"
      - "credential-domain guard precedence must be explicit when it outranks same-user remediation"
    challenge: >-
      enforceLaneCredentialDomain still precedes the proposed same-user
      precondition. A same-user Claude OAuth lane whose credential selector
      resolves inside the target repository can therefore fail with
      lane_credential_cache_inside_repo or
      lane_uncovered_credential_selector_inside_repo before the typed
      provider_credential_same_user_unsupported remediation. This is not a
      custody route, but the SPEC should not overclaim the first-floor ordering.
    closest_acceptable_answer: >-
      Either run the same-user precondition before enforceLaneCredentialDomain,
      or state that credential-domain violations intentionally outrank
      same-user remediation. Add a same-user test with repo-inside
      CLAUDE_CONFIG_DIR and CLAUDE_SECURESTORAGE_CONFIG_DIR selectors proving
      the chosen error and no scratch, token, supervisor row, projection file,
      custody receipt, helper/tmux state, or Claude process.
    requested_constraint_shape:
      kind: policy
constraints:
  - id: C3-PROJECTION-OFF-KIND-CLASSIFIER-FAIL-CLOSED
    posture: credential_custody
    severity: critical
    kind: gate
    binding: true
    source_finding: C3-PROJECTION-OFF-UNKNOWN-KIND-FALLTHROUGH
    source_refs: ["dialogue:3"]
    text: >-
      The revised SPEC must define projection-off classification as fail-closed
      for self-driving Claude launches. For adapter == claude, missing, unknown,
      or unmodeled credential kind is OAUTH_COPIED/Claude OAuth for RFC 0165
      admission, so provider_credential_projection=off refuses with
      provider_credential_projection_disabled_unsupported before any launch side
      effect. A non-OAuth diagnostic exception may launch only after positive
      pre-launch proof that no Claude OAuth resolver surface is in play; absence
      of a kind field or roster entry is not proof.
    verification:
      gate: "Add projection-off tests where no explicit credential kind is present and HOME, CLAUDE_CONFIG_DIR, and CLAUDE_SECURESTORAGE_CONFIG_DIR each contain a whole Claude credential with refreshToken; assert provider_credential_projection_disabled_unsupported before scratch, FIFO/ACL, session-token minting, supervisor rows, projection receipt creation, helper/tmux, or provider process. Keep the positive control that normal distinct-UID access-token-only projection still launches."
    final_review_required: true
  - id: C2-CREDENTIAL-DOMAIN-PRECEDENCE-DECLARED
    posture: launch_precondition
    severity: medium
    kind: policy
    binding: false
    source_finding: C2-CREDENTIAL-DOMAIN-PRECEDENCE-AMBIGUITY
    source_refs: ["dialogue:2"]
    text: >-
      The revised SPEC should either move same-user Claude OAuth refusal before
      enforceLaneCredentialDomain or explicitly declare credential-domain
      violations a higher-priority fail-closed precondition than same-user
      remediation. The chosen order must be tested for same-user Claude OAuth
      launches with repo-inside CLAUDE_CONFIG_DIR and
      CLAUDE_SECURESTORAGE_CONFIG_DIR selectors, with no launch side effects.
    verification:
      expected_stage: "build design/tests"
    final_review_required: false
branches:
  recovery: "cleared"
  credential_custody: "blocked"
  launch_precondition: "cleared_with_constraints"
---

# Collaboration Ledger - RFC 0165 design run v5 (cycle 2)

author: adjudicator-author-002

## Verdict

**verdict: needs_revision**

V5 clears the launch-generation recovery race and the original
generic-provider-auth ordering caveat, but it still does not clear C3. The SPEC
must make projection-off classification fail closed when a self-driving Claude
launch has missing, unknown, or unmodeled credential kind.

## Determination

### C1 - launch-generation-bound recovery freshness: cleared

The holder's launch binding model answers the v4 latest-row challenge. Recovery
does not read the singleton lane-user dependency row as positive proof for the
stalled process. It reads the stalled job/session/supervisor binding, requires a
matching receipt and generation, keeps lane samples downgrade-only, and records
provider-auth debt against the binding/generation without burning generic
recovery budget. The named overlapping-session and per-session decay tests are
falsifiable.

### C2 - same-user before generic provider-auth gate: cleared with constraint

The v4 caveat was that the Codex-oriented provider-auth gate could preempt the
same-user Claude OAuth refusal. V5 moves the same-user refusal before that gate
for auto, off, and required modes and before launch side effects, so the carried
C2 gate-ordering issue is cleared.

Falsifier 1 identifies a narrower earlier precondition:
enforceLaneCredentialDomain can still refuse a repo-inside Claude credential
selector before same-user remediation. That ordering is fail-closed and before
side effects. It should be declared and tested, but it is not a material custody
blocker.

### C3 - projection-off raw-token route: open

The holder refuses `provider_credential_projection=off` for known Claude OAuth
self-driving lanes, which closes the literal v4 repro. The remaining problem is
the diagnostic exception for non-Claude or non-OAuth paths. The SPEC does not
say how kind is proven before launch and does not say that absent, unknown, or
unmodeled Claude kind is treated as OAuth/copied for RFC 0165 admission.

That ambiguity is material. A build that treats unknown kind as diagnostic
non-OAuth could launch a distinct-UID Claude process with projection disabled and
a lane-readable whole credential in HOME, CLAUDE_CONFIG_DIR, or
CLAUDE_SECURESTORAGE_CONFIG_DIR. The process would then have raw refresh-token
custody by the same file route C3 is meant to close.

## Revision Required

The next revision must make the projection-off classifier fail closed for
self-driving Claude lanes:

- For `adapter == claude`, missing, unknown, or unmodeled credential kind is
  `OAUTH_COPIED` / Claude OAuth for RFC 0165 admission.
- `provider_credential_projection=off` refuses with
  `provider_credential_projection_disabled_unsupported` before any launch side
  effect unless there is positive pre-launch proof that no Claude OAuth resolver
  surface is in play.
- Tests must cover no-explicit-kind launches with HOME, `CLAUDE_CONFIG_DIR`, and
  `CLAUDE_SECURESTORAGE_CONFIG_DIR` whole credentials containing a fixture
  `refreshToken`, and assert no scratch, FIFO/ACL, session token, supervisor
  row, projection receipt, helper/tmux state, or provider process.
- The normal distinct-UID access-token-only projection positive control must
  remain green.

The same-user credential-domain precedence should also be made explicit and
covered by a no-side-effect test, but it is not a blocker to the C3 gate once
declared.
