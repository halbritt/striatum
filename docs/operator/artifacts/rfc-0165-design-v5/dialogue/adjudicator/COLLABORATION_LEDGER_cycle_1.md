---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
author: adjudicator-author-001
workflow: "rfc-0165-design-v5"
run_id: "run_efde0bcac1a8712b90c94e22e9f5db97"
session_id: "sess_ef2300b505907cb3f7a55cc9ed912771"
cycle: 1
topic: "RFC 0165 Claude provider credential freshness v5: launch-bound recovery freshness and projection-off fail-closed closure"
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
      The v5 holder preserves the v4-cleared spine and claims the remaining
      constraints discharged: C1 by adding daemon-owned launch credential
      bindings keyed to the stalled job/session/supervisor generation, with the
      latest provider_auth_dependencies row admission-only; C3 by making
      provider_credential_projection=off fail closed for Claude OAuth
      self-driving lanes before side effects; and C2 by moving the same-user
      Claude OAuth precondition before the generic provider-auth gate in auto,
      off, and required modes.
  - kind: challenge
    by: "falsifier-reviewer-001"
    refs: ["dialogue:2"]
    correspondence: "not_material"
    text: >-
      Falsifier 1 accepts the v5 C1 launch-generation binding and the named
      overlapping-session tests, but challenges the breadth of the C2 typed-floor
      claim: enforceLaneCredentialDomain still runs before the new same-user
      precondition and can return a repository credential-domain error before
      provider_credential_same_user_unsupported when CLAUDE_CONFIG_DIR or
      CLAUDE_SECURESTORAGE_CONFIG_DIR resolves inside the target repository.
      This is a real precedence clarification, but it is fail-closed, before
      side effects, and outside the v4 caveat about the generic provider-auth
      gate.
  - kind: challenge
    by: "falsifier-reviewer-002"
    refs: ["dialogue:3"]
    correspondence: "landed_unrebutted"
    text: >-
      Falsifier 2 lands a material C3 challenge. The holder says
      projection-off fails closed for Claude OAuth lanes but remains diagnostic
      for non-Claude or non-OAuth paths, while the current launch config has no
      credential-kind field and the SPEC does not state how OAuth kind is proven
      before launch. If missing, unknown, or unmodeled Claude credential kind is
      treated as non-OAuth diagnostic, projection-off can still launch a
      distinct-UID Claude lane with a lane-readable whole credential in HOME,
      CLAUDE_CONFIG_DIR, or CLAUDE_SECURESTORAGE_CONFIG_DIR. That reopens the raw
      refresh-token custody route C3 exists to close.
  - kind: constraint
    by: "adjudicator-author-001"
    refs: ["dialogue:3"]
    text: >-
      C3 carries into revision: projection-disabled self-driving Claude launches
      must fail closed unless Striatum has positive pre-launch proof that the
      launch cannot use Claude OAuth credential discovery. Missing, unknown, or
      unmodeled Claude credential kind is not proof of non-OAuth and must be
      treated as the copied-OAuth assurance class for RFC 0165 admission.
  - kind: constraint
    by: "adjudicator-author-001"
    refs: ["dialogue:2"]
    text: >-
      C2 carries as a non-blocking policy clarification: either move the
      same-user Claude OAuth precondition before enforceLaneCredentialDomain, or
      explicitly declare credential-domain violations a higher-priority
      fail-closed precondition and test the chosen precedence with no scratch,
      token, supervisor, projection, helper, tmux, or provider-process side
      effects.
verdict: "needs_revision"
rationale: >-
  C1 is discharged: v5 evaluates recovery positive freshness against a
  daemon-owned launch binding for the stalled job/session/supervisor generation,
  keeps the latest lane-user dependency row admission-only, keys provider-auth
  debt by binding/generation, and names the expired-G1-despite-fresh-G2 and
  per-session decay tests. The C2 generic-gate ordering caveat is also
  substantially discharged because same-user Claude OAuth refusal is specified
  before runSuperviseProviderAuthGate and before side effects in auto, off, and
  required modes. Falsifier 1's earlier credential-domain guard precedence issue
  is a bounded fail-closed ordering clarification, not a raw-token custody
  reopening. The gate still cannot clear because Falsifier 2 lands an unrebutted,
  material C3 challenge: projection-off fail-closed depends on a Claude OAuth
  classification the SPEC does not make fail-closed. A build that treats absent,
  unknown, or unmodeled credential kind as non-OAuth diagnostic can still start
  a projection-disabled Claude process with a lane-readable whole credential,
  including HOME, CLAUDE_CONFIG_DIR, or CLAUDE_SECURESTORAGE_CONFIG_DIR. That is
  the same raw-refresh-token custody route v5 must close. Carry-forwards remain
  intact on the stated paths: same-user unsupported fail-closed, distinct-UID
  access-token-only projection, path and ownership rules, downgrade-only lane
  samples, F1 decay, durable token-free receipts, redaction, RFC 0096 separation,
  and refresh_token_absent.
findings:
  - id: C3-PROJECTION-OFF-UNKNOWN-KIND-FALLTHROUGH
    severity: critical
    posture: credential_custody
    status: open
    source_refs: ["dialogue:3"]
    affected_invariants:
      - "provider_credential_projection=off must not be a Claude OAuth raw-token bypass"
      - "missing or unmodeled credential kind is not proof that Claude OAuth discovery is impossible"
      - "no lane may obtain raw refresh-token custody by HOME, CLAUDE_CONFIG_DIR, CLAUDE_SECURESTORAGE_CONFIG_DIR, or inherited credential surfaces"
    challenge: >-
      The v5 holder fails closed for launches already classified as Claude
      OAuth, but leaves a non-Claude/non-OAuth diagnostic exception without
      defining a pre-launch proof source for credential kind. Current launch
      config does not carry kind, and current Claude resolver surfaces are
      OAuth-shaped. If an implementation treats missing or unknown kind as
      diagnostic non-OAuth, projection-off can still launch a Claude process that
      reads a whole credential with refreshToken from lane-readable credential
      discovery surfaces.
    closest_acceptable_answer: >-
      For adapter == claude and agent_loop_mode == self_driving, treat missing,
      unknown, or unmodeled credential kind as OAUTH_COPIED for RFC 0165
      admission. Allow projection-disabled non-OAuth diagnostic launch only with
      positive pre-launch proof that no Claude OAuth resolver surface is in play;
      absence of a roster/kind field is not proof. The refusal must happen
      before scratch, FIFO/ACL, token minting, supervisor rows, projection
      receipt creation, helper/tmux, or process launch.
    requested_constraint_shape:
      kind: gate
  - id: C2-CREDENTIAL-DOMAIN-PRECEDENCE-AMBIGUITY
    severity: medium
    posture: launch_precondition
    status: accepted
    source_refs: ["dialogue:2"]
    affected_invariants:
      - "same-user Claude OAuth remediation should be predictable across fail-closed launch preconditions"
    challenge: >-
      enforceLaneCredentialDomain still runs before the new same-user
      precondition, so a same-user Claude launch with an in-repository
      credential selector can fail with a credential-domain error before the
      same-user typed remediation. This does not preempt the generic
      provider-auth gate and does not create side effects, but it should be
      explicit so the same-user floor claim is not broader than the intended
      launch-precondition ordering.
    closest_acceptable_answer: >-
      Either run the same-user precondition before enforceLaneCredentialDomain or
      state that credential-domain violations intentionally outrank same-user
      remediation and add a same-user plus repo-inside CLAUDE_CONFIG_DIR or
      CLAUDE_SECURESTORAGE_CONFIG_DIR test proving the selected error and
      no-side-effect boundary.
constraints:
  - id: C3-PROJECTION-OFF-KIND-CLASSIFIER-FAIL-CLOSED
    posture: credential_custody
    severity: critical
    kind: gate
    binding: true
    source_finding: C3-PROJECTION-OFF-UNKNOWN-KIND-FALLTHROUGH
    source_refs: ["dialogue:3"]
    text: >-
      The revised SPEC must define the projection-off classifier as fail-closed
      for self-driving Claude launches. For adapter == claude, missing, unknown,
      or unmodeled credential kind must be treated as OAUTH_COPIED/Claude OAuth
      for RFC 0165 admission, so provider_credential_projection=off refuses with
      provider_credential_projection_disabled_unsupported before side effects.
      A non-OAuth diagnostic exception may launch only after positive pre-launch
      proof that no Claude OAuth resolver surface is in play; default absence of
      kind is not sufficient.
    verification:
      gate: "Add projection-off tests where no explicit credential kind is present and HOME, CLAUDE_CONFIG_DIR, and CLAUDE_SECURESTORAGE_CONFIG_DIR each contain a whole Claude credential with refreshToken; assert provider_credential_projection_disabled_unsupported before scratch, FIFO/ACL, token minting, supervisor rows, projection receipt creation, helper/tmux, or provider process. Keep the positive control that normal distinct-UID access-token-only projection still launches."
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
      remediation. In either case, the chosen precedence must be tested for
      same-user Claude OAuth launches with repo-inside CLAUDE_CONFIG_DIR and
      CLAUDE_SECURESTORAGE_CONFIG_DIR selectors, with no launch side effects.
    verification:
      expected_stage: "build design/tests"
    final_review_required: false
branches:
  recovery: "cleared"
  credential_custody: "blocked"
  launch_precondition: "cleared_with_constraints"
---

# Collaboration Ledger - RFC 0165 design run v5 (cycle 1)

author: adjudicator-author-001

## Verdict

**verdict: needs_revision**

V5 is a strong surgical revision and clears the v4 C1 latest-row race. It also
fixes the original C2 generic-provider-auth ordering caveat. The gate still
does not clear because C3 depends on a projection-off credential-kind classifier
that the SPEC does not make fail-closed.

## Per-constraint Determination

### C1 - launch-generation-bound recovery freshness: cleared

The holder adds `provider_credential_launch_bindings` and requires recovery to
load the binding for the stalled job/session/supervisor owner rather than the
latest lane-user dependency row. The latest row can prove that a fresh relaunch
is possible, but it cannot prove an older stalled process fresh. Debt is keyed
to the binding and destination generation, and the required tests cover the
expired G1 / fresh G2 overlap plus per-session decay. Falsifier 1 did not sustain
the C1 attack against v5.

### C3 - projection-off raw-token route: open

The holder closes the route for launches already known to be Claude OAuth, but
the diagnostic exception for "non-Claude or non-OAuth" is underspecified. Current
launch config does not provide a credential kind, while the resolver treats
Claude credential surfaces as OAuth-shaped. Without a normative rule that
missing or unknown kind fails closed for self-driving Claude lanes, a build can
classify "not proven OAuth" as diagnostic non-OAuth and still launch with
projection disabled. That leaves the lane able to read a whole credential from
HOME, `CLAUDE_CONFIG_DIR`, or `CLAUDE_SECURESTORAGE_CONFIG_DIR`, including a
raw `refreshToken`.

The revision must define unknown/unmodeled Claude credential kind as
`OAUTH_COPIED` for RFC 0165 admission and extend the projection-off tests to the
unknown-kind/default-classification cases.

### C2 - same-user ordering: cleared with a clarification

The material v4 C2 caveat was that `runSuperviseProviderAuthGate` could preempt
same-user Claude OAuth refusal in `required` mode. V5 moves the same-user
precondition before that generic gate and before side effects in all gate modes.

Falsifier 1 identifies a separate earlier fail-closed guard:
`enforceLaneCredentialDomain` can preempt the same-user remediation for
repo-inside Claude credential selectors. That does not reopen custody and does
not violate the generic-provider-auth ordering requirement, but the SPEC should
state the intended precedence or move the same-user gate earlier.

## Carry-Forward Determination

No accepted carry-forward regressed on the stated paths:

- same-user Claude OAuth self-driving lanes remain unsupported and fail closed;
- distinct-UID access-token-only B1/B2 projection remains the nominal path;
- path, ownership, and resolver rules remain in scope;
- lane samples stay downgrade-only;
- F1 decay and recovery debt remain daemon-owned;
- durable receipts and events remain token-free and redacted;
- RFC 0096 control-plane/provider-credential separation remains intact;
- `refresh_token_absent` remains the custody assertion for projection surfaces.

## Required Revision

The next holder revision should be narrow:

1. Define the projection-off classifier as fail-closed for self-driving Claude
   launches with missing, unknown, or unmodeled credential kind.
2. Allow any non-OAuth diagnostic exception only with positive pre-launch proof
   that no Claude OAuth resolver surface is in play.
3. Add unknown-kind/default-classification projection-off tests for HOME,
   `CLAUDE_CONFIG_DIR`, and `CLAUDE_SECURESTORAGE_CONFIG_DIR`, asserting the
   typed refusal before every launch side effect.
4. Clarify the credential-domain-vs-same-user precedence and test the chosen
   ordering.
