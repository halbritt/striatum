---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
author: adjudicator-author-004
workflow: "rfc-0165-design-v5"
run_id: "run_efde0bcac1a8712b90c94e22e9f5db97"
session_id: "sess_0489bdbfc0c2f1bc28cece6a2a344858"
cycle: 3
topic: "RFC 0165 Claude provider credential freshness v5: launch-bound recovery freshness and projection-off classifier closure"
participants:
  - "holder-author-001"
  - "falsifier-reviewer-001"
  - "falsifier-reviewer-002"
  - "adjudicator-author-004"
entries:
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: >-
      The current v5 holder keeps the accepted access-token-only projection
      spine and claims all carried constraints are discharged: C1 by binding
      positive recovery freshness to the stalled job/session/supervisor launch
      credential generation; C3 by treating missing, unknown, or unmodeled
      self-driving Claude credential kind as OAUTH_COPIED and refusing
      projection-disabled launches unless non-OAuth is positively proven before
      launch; and C2 by running same-user refusal before the generic provider
      auth gate while explicitly declaring credential-domain violations the
      higher-priority fail-closed precondition.
  - kind: challenge
    by: "falsifier-reviewer-001"
    refs: ["dialogue:2"]
    correspondence: "landed_and_rebutted"
    text: >-
      Falsifier 1's bounded C2 challenge was that enforceLaneCredentialDomain
      can preempt provider_credential_same_user_unsupported for same-user Claude
      launches whose CLAUDE_CONFIG_DIR or CLAUDE_SECURESTORAGE_CONFIG_DIR
      resolves inside the target repository. The challenge landed as an
      ordering/specification ambiguity, but the current holder answers it
      directly by making credential-domain violations an intentional
      higher-priority fail-closed precondition and naming a no-side-effect
      same-user credential-domain precedence test.
  - kind: challenge
    by: "falsifier-reviewer-002"
    refs: ["dialogue:3"]
    correspondence: "landed_and_rebutted"
    text: >-
      Falsifier 2's material C3 challenge was that projection-off closure was
      unsafe if absent, unknown, or unmodeled Claude credential kind could fall
      through as a non-OAuth diagnostic launch. The current holder answers that
      specific gap: for adapter == claude and self-driving mode, missing,
      unknown, or unmodeled kind is treated as OAUTH_COPIED / Claude OAuth for
      RFC 0165 admission, and a non-OAuth projection-off diagnostic exception may
      launch only with positive pre-launch proof that no Claude OAuth resolver
      surface is in play.
  - kind: rebuttal
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: >-
      The holder's revised C3 section makes absence of a kind field, resolver
      roster entry, or diagnostic label insufficient proof of non-OAuth. It
      refuses projection-disabled Claude OAuth or unproven self-driving launches
      before scratch, FIFO/ACL work, session-token minting, supervisor rows,
      projection receipt creation, helper/tmux setup, or provider process
      launch, and names HOME, CLAUDE_CONFIG_DIR, and
      CLAUDE_SECURESTORAGE_CONFIG_DIR unknown-kind tests. Its C2 section also
      declares credential-domain violations higher priority than same-user
      remediation and names the corresponding same-user tests.
  - kind: constraint
    by: "adjudicator-author-004"
    refs: ["dialogue:1", "dialogue:2", "dialogue:3"]
    text: >-
      The downstream commit proposal may carry this design forward, but source
      implementation remains out of this run's artifact-only downstream
      envelope. A later build workflow must include the source, SQL, generated
      contract or authority-map, and product-doc write scopes the holder lists;
      it must not silently widen the frozen commit/final-summary scope.
verdict: "accept_with_findings"
rationale: >-
  The reopened v5 revision clears the gate. C1 is discharged because positive
  recovery freshness is evaluated from a daemon-owned launch binding tied to
  the stalled job/session/supervisor generation, while the latest lane-user
  dependency row remains admission-only; named tests cover expired G1 despite
  fresh G2, per-session decay debt, missing or mismatched bindings, downgrade-
  only lane samples, and the fresh-binding fall-through case. C3 is now
  discharged because projection-off no longer relies on a permissive credential
  kind default: missing, unknown, or unmodeled self-driving Claude kind is
  OAUTH_COPIED for RFC 0165 admission, and a non-OAuth diagnostic exception
  requires positive pre-launch proof that no Claude OAuth resolver surface is in
  play. The holder also preserves the required HOME, CLAUDE_CONFIG_DIR, and
  CLAUDE_SECURESTORAGE_CONFIG_DIR projection-off tests and the distinct-UID
  projection positive control. C2 ordering is discharged with an explicit
  exception: credential-domain violations intentionally outrank same-user
  remediation, while same-user refusal still precedes the generic provider-auth
  gate in auto, off, and required modes when no credential-domain violation is
  present. No accepted carry-forward regressed: same-user unsupported
  fail-closed, normal distinct-UID access-token-only projection, path and
  ownership rules, downgrade-only lane samples, F1 decay, durable token-free
  receipts, redaction, RFC 0096 separation, and refresh_token_absent remain
  intact. The verdict is accept_with_findings rather than plain accept because
  the build must preserve the named tests and must open a source/doc write scope
  beyond this run's commit/final artifact envelope.
findings:
  - id: C3-PROJECTION-OFF-UNKNOWN-KIND-FALLTHROUGH
    severity: critical
    posture: credential_custody
    status: answered
    source_refs: ["dialogue:3"]
    affected_invariants:
      - "provider_credential_projection=off must not be a Claude OAuth raw-token bypass"
      - "missing or unmodeled credential kind is not proof that Claude OAuth discovery is impossible"
      - "no lane may obtain raw refresh-token custody through HOME, CLAUDE_CONFIG_DIR, CLAUDE_SECURESTORAGE_CONFIG_DIR, helper settings, or inherited credential paths"
    challenge: >-
      Projection-disabled self-driving Claude launch was unsafe if missing,
      unknown, or unmodeled credential kind could default to diagnostic
      non-OAuth and start a process with lane-readable whole credentials.
    closest_acceptable_answer: >-
      The current holder supplies the answer: treat missing, unknown, or
      unmodeled self-driving Claude kind as OAUTH_COPIED / Claude OAuth for RFC
      0165 admission; permit non-OAuth diagnostic launch only after positive
      pre-launch proof that no Claude OAuth resolver surface is in play; refuse
      before all launch side effects; and test HOME, CLAUDE_CONFIG_DIR, and
      CLAUDE_SECURESTORAGE_CONFIG_DIR unknown-kind surfaces.
    requested_constraint_shape:
      kind: gate
  - id: C2-CREDENTIAL-DOMAIN-PRECEDENCE-AMBIGUITY
    severity: medium
    posture: launch_precondition
    status: answered
    source_refs: ["dialogue:2"]
    affected_invariants:
      - "same-user Claude OAuth remediation ordering must be explicit across fail-closed launch preconditions"
      - "credential-domain guard precedence must be tested when it outranks same-user remediation"
    challenge: >-
      enforceLaneCredentialDomain can fail before the same-user Claude OAuth
      remediation for repo-inside Claude credential selectors.
    closest_acceptable_answer: >-
      The current holder declares credential-domain violations a higher-priority
      fail-closed precondition, keeps same-user refusal ahead of the generic
      provider-auth gate when no credential-domain violation exists, and names a
      same-user credential-domain test with no launch side effects.
    requested_constraint_shape:
      kind: policy
  - id: IMPLEMENTATION-SCOPE-REQUIRES-BUILD-WORKFLOW
    severity: medium
    posture: implementation_scope
    status: deferred_with_owner
    source_refs: ["dialogue:1"]
    affected_invariants:
      - "artifact-only commit/final-summary scope must not be silently widened into source implementation"
      - "new RPC methods or handwritten route maps must update command authority docs and guardrail tests"
    challenge: >-
      The accepted design requires later source, SQL, error-catalog, generated
      contract or authority-map, and product-doc changes outside this run's
      downstream commit and final-summary artifact directories.
    closest_acceptable_answer: >-
      Keep this run's downstream work to the declared commit proposal and final
      summary artifacts. Open a later build workflow with explicit source,
      SQL, generated-contract or authority-map, and product-doc write scopes
      before implementing the design.
    requested_constraint_shape:
      kind: policy
constraints:
  - id: BUILD-PRESERVE-C3-UNKNOWN-KIND-GATE
    posture: credential_custody
    severity: high
    kind: gate
    binding: false
    source_finding: C3-PROJECTION-OFF-UNKNOWN-KIND-FALLTHROUGH
    source_refs: ["dialogue:1", "dialogue:3"]
    text: >-
      The later build must preserve the current holder's fail-closed C3 shape:
      for self-driving Claude launches, missing, unknown, or unmodeled
      credential kind is OAUTH_COPIED / Claude OAuth for RFC 0165 admission, and
      provider_credential_projection=off refuses before launch side effects
      unless non-OAuth is positively proven before launch.
    verification:
      expected_stage: "later rfc-0165 build workflow"
    final_review_required: true
  - id: BUILD-PRESERVE-C2-PRECEDENCE-TEST
    posture: launch_precondition
    severity: medium
    kind: policy
    binding: false
    source_finding: C2-CREDENTIAL-DOMAIN-PRECEDENCE-AMBIGUITY
    source_refs: ["dialogue:1", "dialogue:2"]
    text: >-
      The later build must preserve the declared order: credential-domain
      violations outrank same-user remediation, and same-user Claude OAuth
      refusal outranks the generic provider-auth gate when no credential-domain
      violation is present. Tests must prove the chosen error and the no-side-
      effect boundary.
    verification:
      expected_stage: "later rfc-0165 build workflow"
    final_review_required: true
  - id: OPEN-BUILD-WRITE-SCOPE
    posture: implementation_scope
    severity: medium
    kind: policy
    binding: false
    source_finding: IMPLEMENTATION-SCOPE-REQUIRES-BUILD-WORKFLOW
    source_refs: ["dialogue:1"]
    text: >-
      Do not implement source changes inside this run's downstream artifact-only
      envelope. The successor build workflow must explicitly grant the source,
      SQL, generated-contract or authority-map, and product-doc paths required
      by the accepted design.
    verification:
      expected_stage: "successor build workflow setup"
    final_review_required: false
branches:
  recovery: "cleared"
  credential_custody: "cleared_with_constraints"
  launch_precondition: "cleared_with_constraints"
  implementation_scope: "defer_with_successor"
---

# Collaboration Ledger - RFC 0165 design run v5 (cycle 3)

author: adjudicator-author-004

## Verdict

**verdict: accept_with_findings**

The current v5 revision clears the substance gate. The standing C3
projection-off classifier gap from cycle 2 is answered directly: unknown,
missing, or unmodeled self-driving Claude credential kind is not allowed to fall
through as diagnostic non-OAuth, and projection disabled refuses before launch
side effects unless non-OAuth is positively proven before launch.

## Determination

### C1 - launch-generation-bound recovery freshness: cleared

The holder binds positive recovery freshness to
`provider_credential_launch_bindings` for the stalled owner tuple rather than to
the latest lane-user dependency row. Newer G2 readiness can justify a fresh
relaunch, but it cannot prove older stalled G1 fresh. Debt is keyed by the
binding or owner tuple and destination generation, lane samples are
downgrade-only, and the named tests cover expired G1 despite fresh G2,
per-session decay, missing/mismatched bindings, and the fresh-binding fall
through case.

### C3 - projection-off raw-token route: cleared with build obligations

Falsifier 2's challenge was material in cycle 2, but the current holder has now
answered it. The spec says that for `adapter == claude` and self-driving mode,
missing, unknown, or unmodeled credential kind is treated as OAUTH_COPIED /
Claude OAuth for RFC 0165 admission. Absence of an explicit kind, resolver
roster entry, or diagnostic label is not proof of non-OAuth. A non-OAuth
diagnostic exception may launch only after positive pre-launch proof that no
Claude OAuth resolver surface is in play.

The required tests now include HOME, `CLAUDE_CONFIG_DIR`, and
`CLAUDE_SECURESTORAGE_CONFIG_DIR` unknown-kind surfaces, plus a positive control
that normal distinct-UID access-token-only projection still launches. That is
falsifiable enough for a design clear; the later build must preserve those exact
test shapes.

### C2 - same-user ordering: cleared with explicit precedence

The original v4 C2 caveat required same-user Claude OAuth refusal to precede the
generic provider-auth gate in `auto`, `off`, and `required`. The holder keeps
that order when no credential-domain violation exists.

Falsifier 1's narrower challenge was that `enforceLaneCredentialDomain` can
still run first. The holder now states that this is intentional:
credential-domain violations are higher-priority fail-closed preconditions
because they protect the repository boundary. The spec names a same-user
credential-domain test for repo-inside `CLAUDE_CONFIG_DIR` and
`CLAUDE_SECURESTORAGE_CONFIG_DIR`, with no scratch, token, supervisor row,
projection file, custody receipt, helper/tmux state, or provider process.

## Carry-Forward Determination

No accepted carry-forward regressed:

- same-user Claude OAuth self-driving lanes remain unsupported and fail closed;
- distinct-UID B1/B2 access-token-only projection remains the normal path;
- path, ownership, and resolver rules stay in scope;
- lane samples remain downgrade-only;
- F1 decay and recovery debt remain daemon-owned;
- receipts, events, errors, doctor, dashboard, metrics, and artifacts stay
  token-free and redacted;
- RFC 0096 control-plane/provider-credential separation remains intact;
- `refresh_token_absent` remains the custody assertion for projection surfaces.

## Findings

This is not a source-build authorization. The accepted design requires later
source, SQL, generated-contract or authority-map, and product-doc write scopes
outside this run's downstream commit/final artifact envelope. The next phase can
publish the commit proposal from the declared artifact scope, but implementation
must be routed through a successor build workflow with explicit write scope.
