---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
author: adjudicator-author-001
workflow: "rfc-0165-design-v4"
run_id: "run_5b6bfca275ecdc83636daac991141f92"
cycle: 1
topic: "RFC 0165 Claude provider credential freshness (v4 revision): same-user fail-closed + daemon-owned recovery freshness for the access-token-only Claude projection (GH #583)"
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
      The v4 holder is a surgical revision of the v3 access-token-only projection
      SPEC. It claims all three SEED constraints discharged: C1 — recovery's
      runtime-freshness classification takes its positive value only from the
      daemon-owned provider_auth_dependencies row (an isPositivelyFresh predicate
      over row existence, state==ready, expires_at>now+MinFreshness, a passed
      receipt whose generation ids match, and a daemon-re-observed current
      operator-source generation), with the lane-file re-sample demoted to a
      downgrade-only signal and stale/missing/inconsistent rows failing closed to
      reseed_required without generic budget burn; C2 — a typed
      provider_credential_same_user_unsupported launch-precondition gate refuses
      same-user Claude OAuth lanes (RunAsUser=="" or resolving to the daemon uid)
      before any side effect; C3 — because same-user fails closed and the
      distinct-UID projection carries no refresh token, no lane reaches the
      rotating refresh token by any route, proven by an all-surfaces
      no-refresh-token test. Carry-forwards (access-token-only projector,
      path/ownership, spawn-time gate, F1 breaker + decay, durable state/receipts
      with access token never persisted, redaction contract, RFC 0096 boundary,
      refresh_token_absent) are claimed preserved.
  - kind: challenge
    by: "falsifier-reviewer-001"
    refs: ["dialogue:2"]
    correspondence: landed_unrebutted
    text: >-
      C1 is not genuinely resolved. The v4 positive-freshness predicate reads the
      provider_auth_dependencies slot keyed only by (repository_id, provider,
      kind, lane_user, destination_selector) — a singleton, latest-write slot, not
      the credential generation bound to the stalled session/supervisor/job. A
      later Claude launch under the same lane OS user upserts a fresh generation
      G2 (future expires_at, passed receipt_B, current operator generation) into
      that one row while an older running session is still on its expired launch
      generation G1. Recovery for the older stalled job reads the current row,
      sees isPositivelyFresh==true, and follows the holder's own branch into
      generic recovery, incrementing requeue_count/transfer_count for a
      provider-auth cause — the exact C1 budget-burn leak, reached by daemon-owned
      latest-row overwrite rather than lane-side forgery. The named C1 tests
      (lane-forged expiry, stale/missing row, inconsistent row, downgrade-only
      sample, fully-consistent-future row falls through) do not cover the
      overlapping-session case where a consistent future row for a NEWER session
      must not prove an OLDER stalled session fresh. recoverStuckJobs already
      carries job/session/supervisor/lease identity but the v4 predicate does not
      use it to select provider-auth evidence. (C2 check: no same-user
      raw-refresh-token path found; one ordering caveat — the same-user gate sits
      after runSuperviseProviderAuthGate, so under provider_auth_gate=required a
      same-user Claude lane refuses with lane_provider_auth_failed, not the
      promised provider_credential_same_user_unsupported.)
  - kind: challenge
    by: "falsifier-reviewer-002"
    refs: ["dialogue:3"]
    correspondence: landed_unrebutted
    text: >-
      C3 is not genuinely resolved. v4 closes the same-user route and the nominal
      distinct-UID projection route, but it keeps provider_credential_projection=off
      as a distinct-UID Claude launch path that skips projection. The SPEC does not
      require that disabled-projection path to refuse Claude OAuth launch, to
      overwrite/scrub a pre-existing lane credential, to force a controlled empty
      Claude config dir, or to run the all-surfaces no-refresh-token scan before
      the process starts. The RFC's starting incident is exactly a lane-home
      .credentials.json that is a point-in-time copy of the operator credential and
      can contain a stale refreshToken. With projection skipped, no B1
      access-token-only file overwrites the old file, no B2 broker becomes the only
      source, and no spec-required scan refuses it; the Claude resolver
      ($CLAUDE_CONFIG_DIR/.credentials.json else $HOME/.claude/.credentials.json)
      reads the lane-readable whole-credential file and the distinct-UID lane
      obtains raw refresh-token custody by the old file route. The carry-forward
      tests cover provider_auth_gate=off, not provider_credential_projection=off,
      so the route that actually skips projection has no named no-refresh-token
      test. C3's "by ANY route" is therefore not met. Carry-forward check: no
      same-user raw-token path; access-token-only B1/B2 projection, RFC 0096
      separation, redaction, and downgrade-only recovery sample are intact — the
      one standing regression is the projection-disabled fallback file route.
  - kind: constraint
    by: "adjudicator-author-001"
    refs: ["dialogue:2"]
    text: >-
      C1 carries into v5. The recovery positive-freshness authority must be bound
      to the STALLED job's launch credential generation, not the latest lane-user
      singleton row. Persist the projection receipt id / delivery mode /
      destination_generation_id / expires_at on the session/supervisor/job launch
      record, and in recoverStuckJobs evaluate freshness for the stalled job's own
      bound generation; a newer lane-user row may prove a fresh projection is
      available for relaunch but must NOT prove the older stalled process fresh.
      Make near-expiry/reseed debt per running session/generation. Add
      TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection (older
      session G1 expired, newer same-lane-user G2 fresh, older session stalls →
      provider-auth debt, no generic counter increment) and the per-session decay
      test. If B1 cannot prove launched processes re-read the newest projection
      before every provider action, use B2 for runtime freshness or
      restart/requeue sessions whose bound generation crosses near-expiry.
  - kind: constraint
    by: "adjudicator-author-001"
    refs: ["dialogue:3"]
    text: >-
      C3 carries into v5. provider_credential_projection=off must not remain a live
      Claude OAuth launch bypass that can leave an unscanned whole-credential lane
      file in place. Either (preferred) fail closed before launch for Claude OAuth
      lanes whenever projection is disabled — typed launch-precondition refusal,
      symmetric with same-user — keeping the flag as a diagnostic only for
      non-OAuth adapters; or, if the flag must remain for Claude, require it to
      first prove refresh_token_absent across EVERY lane-readable surface named by
      C3 (resolver-proven destination, $CLAUDE_CONFIG_DIR, $HOME/.claude,
      credential-bearing env entries, B2/helper settings, inherited fd/config
      paths) and refuse if any surface yields a refreshToken or resolves to an
      untrusted credential path. Add TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface
      and TestProjectionOffStillValidatesClaudeConfigDir.
  - kind: constraint
    by: "adjudicator-author-001"
    refs: ["dialogue:2"]
    text: >-
      C2 build-ordering caveat carries into v5 (does not by itself reopen custody).
      Move the same-user precondition refusal BEFORE the Codex-only
      runSuperviseProviderAuthGate so it returns
      provider_credential_same_user_unsupported (with the configure-distinct-lane-user
      remediation) in all gate modes, and cover provider_auth_gate=auto, off, and
      required in TestSameUserClaudeLaneRefusedBeforeSideEffects so a same-user
      Claude lane never refuses with the generic lane_provider_auth_failed error.
verdict: "needs_revision"
rationale: >-
  v4 is a strong, mostly-correct revision and materially improves on v3. C2 is
  genuinely discharged on its safety axis: the new typed same-user
  launch-precondition gate refuses Claude OAuth self-driving lanes (RunAsUser=="" or
  resolved-uid equality with the daemon) before any side effect, and BOTH falsifiers
  independently confirmed they found no same-user raw-refresh-token launch path —
  the load-bearing "no in-uid boundary can hide a file from a same-uid process,
  therefore fail closed" argument is sound, and the distinct-UID access-token-only
  projection (B1/B2) the v3 adjudicator accepted is carried forward unregressed. All
  carry-forwards are INTACT (access-token-only projector, path/ownership, spawn-time
  gate, F1 breaker + daemon-owned decay, durable state/receipts with the access
  token never persisted, redaction contract with refresh_token_absent, RFC 0096
  control-plane separation). But a clearing verdict requires ALL THREE constraints
  genuinely discharged with no standing material challenge, and two material
  challenges land unrebutted. C1 (dialogue:2): the v4 fix closes the v3 lane-forged
  expiresAt hole but binds positive freshness to the SINGLETON lane-user/destination
  dependency row, not to the stalled session's launch generation; an overlapping
  newer same-lane-user projection (the live multi-lane configuration) overwrites that
  row and makes recovery classify an older, genuinely-expired session as
  isPositivelyFresh==true, falling through to generic requeue/transfer budget burn —
  the exact failure C1 exists to prevent, now reached through daemon-owned latest-row
  overwrite. A row can be genuinely daemon-owned and still be the wrong proof, so the
  freshness read is not race-free or positively authoritative for the stalled session.
  C3 (dialogue:3): the SPEC presents "no refresh token by ANY route" as cleared while
  keeping provider_credential_projection=off as a distinct-UID launch route that skips
  projection without refusing, scrubbing, or scanning a pre-existing whole-credential
  lane file — the literal #583 incident shape — so a distinct-UID lane can read the
  raw rotating refresh token by the old file route; the flag being "documented unsafe"
  does not fence it from the C3 proof because the SPEC neither suspends the guarantee
  under the flag nor blocks the gate while it is used, and no named test covers that
  route. Both challenges attack the core correctness boundary, not polish. This is the
  single allowed v4 revision cycle, so needs_revision ends the gate uncleared and
  routes to the operator for a fresh -v5 run with a revising holder that binds
  recovery freshness to the stalled session's generation (C1), closes the
  projection-off refresh-token route (C3), and fixes the same-user gate ordering (C2
  caveat) while preserving the v3+v4 work listed under Preserve.
findings:
  - id: C1-LATEST-DEPENDENCY-ROW-NOT-SESSION-BOUND
    severity: high
    posture: recovery
    status: open
    source_refs: ["dialogue:2"]
    affected_invariants:
      - "daemon-owned state must be the ONLY positive freshness authority for the stalled session's actual credential generation"
      - "recovery must not increment generic requeue/transfer budget for a runtime provider-auth-expiry cause"
      - "the runtime freshness read must be race-free across overlapping same-lane-user launches"
    challenge: >-
      v4's isPositivelyFresh predicate reads the singleton provider_auth_dependencies
      row keyed by (repository_id, provider, kind, lane_user, destination_selector),
      which is overwritten by each new launch under the same lane OS user. A newer
      session B's fresh generation G2 upserts that row while an older session A is
      still on an expired launch generation G1; when A stalls
      (agent_mcp_discovery_stall), recovery reads the current row, sees a
      receipt-backed future-dated daemon-owned row, classifies the stall as
      non-provider-auth, and falls through to recordRecoveryAction, burning generic
      budget for A's expired provider credential. The proof is genuinely daemon-owned
      but bound to the wrong (latest) generation, not the stalled session's launch
      generation. No named test covers the overlapping-session case.
    closest_acceptable_answer: >-
      Bind recovery's positive freshness to the stalled job's own launch generation:
      persist receipt id / delivery mode / destination_generation_id / expires_at on
      the session/supervisor/job launch record, and in recoverStuckJobs evaluate the
      stalled job's bound generation, not the latest lane-user row. A newer row may
      prove a fresh projection is available for relaunch but must never prove an older
      running session fresh; near-expiry/reseed debt is per session/generation. Prove
      with TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection and a
      per-session decay test. If B1 cannot guarantee launched processes adopt the
      newest projection before each provider action, use B2 for runtime freshness or
      restart/requeue sessions whose bound generation crosses near-expiry.
    requested_constraint_shape:
      kind: gate
  - id: C3-PROJECTION-OFF-REOPENS-REFRESH-TOKEN-FILE-ROUTE
    severity: critical
    posture: credential_custody
    status: open
    source_refs: ["dialogue:3"]
    affected_invariants:
      - "no lane may obtain raw refresh-token custody by ANY route"
      - "every Claude OAuth launch path must deny the lane the rotating refresh token"
      - "a pre-existing whole-credential lane file must not survive unscanned into a launch"
    challenge: >-
      v4 keeps provider_credential_projection=off as a distinct-UID Claude launch path
      that skips the access-token-only projection. In that mode no B1 file overwrites a
      pre-existing lane-home .credentials.json, no B2 broker becomes the only source,
      and the SPEC does not require the all-surfaces no-refresh-token scan or
      CLAUDE_CONFIG_DIR validation before launch. If the lane home still holds a copied
      whole credential (the #583 starting/repair shape) with a refreshToken, the Claude
      resolver reads it and the lane gets raw refresh-token custody. The SPEC presents
      C3 as cleared "by ANY route" yet leaves this route open and untested
      (carry-forward tests cover provider_auth_gate=off, not
      provider_credential_projection=off).
    closest_acceptable_answer: >-
      Make provider_credential_projection=off fail closed before launch for Claude
      OAuth lanes (typed launch-precondition refusal, symmetric with same-user),
      keeping the flag diagnostic-only for non-OAuth adapters; or, if it must remain
      for Claude, require it to prove refresh_token_absent across every lane-readable
      surface named by C3 (resolver-proven destination, $CLAUDE_CONFIG_DIR,
      $HOME/.claude, credential-bearing env entries, B2/helper settings, inherited
      fd/config paths) and refuse on any refreshToken or untrusted credential path.
      Add TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface and
      TestProjectionOffStillValidatesClaudeConfigDir.
    requested_constraint_shape:
      kind: gate
  - id: C2-SAME-USER-GATE-ORDERED-AFTER-CODEX-GATE
    severity: medium
    posture: launch_precondition
    status: open
    source_refs: ["dialogue:2"]
    affected_invariants:
      - "same-user Claude OAuth lanes must fail closed with the typed provider_credential_same_user_unsupported error in all gate modes"
    challenge: >-
      The v4 source-anchor section places the new same-user precondition gate AFTER
      runSuperviseProviderAuthGate. Current runSuperviseProviderAuthGate returns
      lane_provider_auth_failed (unsupported-provider) for non-Codex adapters when
      provider_auth_gate=required, before the proposed insertion point. A same-user
      Claude lane with provider_auth_gate=required therefore still fails closed (no
      custody leak) but with the generic lane_provider_auth_failed error and wrong
      remediation, not provider_credential_same_user_unsupported. The same-user test
      is not specified across gate=auto/off/required.
    closest_acceptable_answer: >-
      Move the same-user precondition before the Codex-only provider-auth gate so it
      returns provider_credential_same_user_unsupported with the
      configure-distinct-lane-user remediation in all gate modes, and cover
      gate=auto, off, and required in TestSameUserClaudeLaneRefusedBeforeSideEffects.
    requested_constraint_shape:
      kind: policy
constraints:
  - id: C1-RECOVERY-FRESHNESS-BOUND-TO-STALLED-GENERATION
    posture: recovery
    severity: high
    kind: gate
    binding: true
    source_finding: C1-LATEST-DEPENDENCY-ROW-NOT-SESSION-BOUND
    source_refs: ["dialogue:2"]
    text: >-
      The revised spec's recovery-time provider-auth freshness classification must
      take its positive authority from the daemon-owned state bound to the STALLED
      job's launch credential generation, not from the latest lane-user/destination
      singleton row. Persist the projection receipt id, delivery mode,
      destination_generation_id, and expires_at on the session/supervisor/job launch
      record; in recoverStuckJobs evaluate the stalled job's own bound generation. A
      newer lane-user dependency row may prove a fresh projection is available for a
      relaunch but must NOT prove an older stalled session fresh. Near-expiry/reseed
      debt is per running session/generation; a later projection must not clear or
      overwrite debt for an older running session unless that session is proven to have
      adopted the newer token. Carry forward the accepted v3+v4 daemon-owned predicate
      for the single-session case.
    verification:
      gate: "TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection: launch session A with generation G1 expiring in 35m and upsert the dependency row; before A's first provider action, launch session B under the same lane_user/destination with fresh G2 so the singleton row now holds G2; advance A past 35m; trigger agent_mcp_discovery_stall for A; assert recovery sets provider-auth reseed/unverifiable debt for A WITHOUT incrementing requeue_count/transfer_count and without treating G2 as proof A is fresh. Plus a per-session decay test where an older session's debt persists after a newer projection updates the lane-user row."
    final_review_required: true
  - id: C3-CLOSE-PROJECTION-OFF-REFRESH-TOKEN-ROUTE
    posture: credential_custody
    severity: critical
    kind: gate
    binding: true
    source_finding: C3-PROJECTION-OFF-REOPENS-REFRESH-TOKEN-FILE-ROUTE
    source_refs: ["dialogue:3"]
    text: >-
      The revised spec must close the provider_credential_projection=off route as a
      raw refresh-token path for Claude OAuth lanes. Preferred: for Claude OAuth
      self-driving lanes, provider_credential_projection=off fails closed before launch
      (typed launch-precondition refusal, symmetric with the same-user gate), and the
      flag is diagnostic-only for non-OAuth adapters. Alternatively, if the flag must
      remain live for Claude, the disabled-projection path must first prove
      refresh_token_absent across every lane-readable credential surface named by C3 —
      the resolver-proven destination, $CLAUDE_CONFIG_DIR/.credentials.json,
      $HOME/.claude/.credentials.json, every credential-bearing env entry, B2/helper
      settings, and any inherited fd/config path — and refuse launch if any surface
      contains a refreshToken or resolves to an untrusted credential path. Preserve the
      accepted access-token-only projection for the nominal distinct-UID path.
    verification:
      gate: "TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface: seed a distinct-UID lane home with a whole Claude credential containing a known refreshToken, set provider_credential_projection=off, launch Claude, and assert a typed precondition refusal before scratch/token-mint/supervisor rows/process (no lane process reads the refresh token). Plus TestProjectionOffStillValidatesClaudeConfigDir: with projection disabled and a workflow CLAUDE_CONFIG_DIR pointing at a lane-readable credential dir outside the trusted destination, assert refusal and no process."
    final_review_required: true
  - id: C2-SAME-USER-PRECONDITION-BEFORE-CODEX-GATE
    posture: launch_precondition
    severity: medium
    kind: policy
    binding: false
    source_refs: ["dialogue:2"]
    text: >-
      Non-blocking build-ordering constraint (C2 custody is already discharged): move
      the same-user Claude OAuth precondition refusal before the Codex-only
      runSuperviseProviderAuthGate so a same-user Claude lane returns the typed
      provider_credential_same_user_unsupported error (with the
      configure-distinct-lane-os-user remediation) in ALL gate modes, never the generic
      lane_provider_auth_failed. Cover provider_auth_gate=auto, off, and required in
      TestSameUserClaudeLaneRefusedBeforeSideEffects.
    verification:
      gate: "TestSameUserClaudeLaneRefusedBeforeSideEffects parameterized over provider_auth_gate in {auto, off, required}: each asserts refusal with provider_credential_same_user_unsupported before any side effect."
branches:
  design: blocked
---

# Collaboration Ledger — RFC 0165 design run v4 (cycle 1)

author: adjudicator-author-001

## Verdict

**verdict: needs_revision**

v4 is a serious, mostly-correct revision that closes the specific v3 repros and
preserves every carry-forward. **C2 is genuinely discharged.** But a clearing
verdict (`accept` / `accept_with_findings`) requires **all three** SEED
constraints discharged with **no standing material challenge**, and the two
falsifiers land **two material, unrebutted challenges** — one on **C1**
(dialogue:2) and one on **C3** (dialogue:3). Both attack the core correctness
boundary, not polish, so the gate does not clear.

## Per-constraint determination

### C1 — daemon-owned state the only positive freshness authority in recovery → **OPEN (not genuinely discharged)**

v4 correctly closes the v3 hole (a lane-owned `0600` file can no longer forge a
future `expiresAt` to upgrade a stale daemon row): the positive predicate now
reads only daemon-written columns and the lane re-sample is downgrade-only.
**But the daemon-owned authority is the wrong row.** The
`provider_auth_dependencies` slot is keyed only by `(repository_id, provider,
kind, lane_user, destination_selector)` (HOLDER.md:507-517) and the positive
predicate falls through to generic recovery whenever that **singleton** row is
fresh (HOLDER.md:454-474). Under the live multi-lane configuration, concurrent
Claude lanes run under the **same** lane OS user and share that one slot. The
concrete race (dialogue:2):

1. Session A launches at T0 with generation **G1**, `expires_at=T0+35m`, and
   upserts the singleton row.
2. Session A does long local work and does not reach a successful Claude action
   before its token expires; it is still on **G1**.
3. At T0+20m session B launches under the same `lane_user`/`destination_selector`,
   projects **G2**, and overwrites the same row to `expires_at=T0+55m`,
   `destination_generation_id=G2`, `last_receipt_id=receipt_B`.
4. At T0+45m session A's first Claude/MCP action fails (G1 expired) →
   `agent_mcp_discovery_stall`.
5. Recovery for job A reads the **current** row (G2: `state=ready`, future
   `expires_at`, passed receipt_B, matching current operator generation) →
   `isPositivelyFresh == true`.
6. The holder's own branch treats this stall as **non-provider-auth** and falls
   through to `recordRecoveryAction`, **incrementing generic requeue/transfer
   budget** for A's expired provider credential — exactly the failure C1 exists
   to prevent.

The proof is genuinely daemon-owned and yet **the wrong proof**: it is bound to
the latest generation, not the generation the stalled session actually uses.
`recoverStuckJobs` already carries the job/session/supervisor/lease identity
(recovery_decision_tree.go:713-736) but the v4 predicate does not use it to
select provider-auth evidence, and no named C1 test covers the overlapping
launch. The freshness read is therefore **neither race-free nor positively
authoritative for the stalled session**. The strongest rebuttal (B2-on-every-action,
or B1 with a proven re-read before every provider action) is **delivery-mode
dependent and unproven by the v4 text** — B1 remains primary and
`TestClaudeCLIAcceptsAccessTokenOnlyCredential` only decides file-vs-socket, not
re-read semantics. **C1 remains open.**

### C2 — same-user Claude OAuth lanes fail closed with a typed launch-precondition error → **RESOLVED (with a build-ordering finding)**

The new typed gate refuses Claude OAuth self-driving lanes whenever the resolved
lane identity is the operator/daemon identity (`RunAsUser == ""` **or** a uid
resolving to the daemon uid), **before** scratch, token mint, supervisor rows,
helper/tmux, or process (HOLDER.md:411-421), and the supporting argument is
correct: no in-uid boundary can hide the operator credential file from a process
running as that uid, so fail-closed is the only sound option and the broker is
reserved for the distinct-UID shape. **Both falsifiers independently confirmed
no same-user raw-refresh-token launch path** in the intended policy
(dialogue:2 "C2 Check"; dialogue:3 "Carry-Forward Check"). C2's custody safety is
genuinely discharged.

One non-blocking caveat (dialogue:2): the gate is placed **after**
`runSuperviseProviderAuthGate`, which under `provider_auth_gate=required` returns
`lane_provider_auth_failed` for non-Codex adapters first. A same-user Claude lane
with `gate=required` still fails closed (no custody leak) but with the **generic
error and wrong remediation**, not `provider_credential_same_user_unsupported`,
and the same-user test is not specified across `auto`/`off`/`required`. This does
not reopen custody, so C2 is **resolved**; the ordering fix is carried as a
non-binding constraint for v5.

### C3 — refresh token unreachable by the lane by ANY route → **OPEN (not genuinely discharged)**

Same-user is now refused and the nominal distinct-UID projection carries no
refresh token — both correct. **But the SPEC keeps one launch route where the
projection is absent.** `provider_credential_projection=off` can skip projection
for distinct-UID delivery (HOLDER.md:430-438), and the SPEC does **not** require
that path to refuse, overwrite/scrub, or scan a pre-existing lane credential file
before launch. The RFC's starting incident is precisely a lane-home
`.credentials.json` that is a point-in-time copy of the operator credential and
can carry a stale `refreshToken` (RFC 0165:13-33). The route (dialogue:3):

1. The lane home holds the pre-RFC / manually-repaired whole credential with a
   `refreshToken` (the live #583 shape).
2. A distinct-UID Claude lane launches (same-user precondition passes).
3. `provider_credential_projection=off` is set, so projection is skipped.
4. No B1 file overwrites the old file, no B2 broker is the only source, and no
   spec-required scan refuses it.
5. Claude resolves `$CLAUDE_CONFIG_DIR/.credentials.json` /
   `$HOME/.claude/.credentials.json` (resolver.go:78-90) and the lane reads the
   raw rotating refresh token.

The holder presents C3 as cleared "by **ANY** route" while keeping this route
open; the "documented unsafe" framing does not fence it, because the SPEC neither
suspends the no-refresh-token guarantee under the flag nor blocks the gate while
it is used, and the carry-forward tests cover `provider_auth_gate=off`, **not**
`provider_credential_projection=off` (HOLDER.md:667-676). **C3 remains open.**

## Carry-forward determination (v1+v3 work — all INTACT, none regressed)

Both falsifiers explicitly affirm the carry-forwards survive:

- **Access-token-only projector (distinct-UID B1/B2)** — INTACT; the v3 model the
  prior adjudicator accepted is carried forward unchanged.
- **Path / ownership rules + atomic writer** — INTACT.
- **Spawn-time projection gate + placement before side effects** — INTACT (now
  also refuses same-user).
- **F1 runtime-expiry circuit breaker + daemon-owned decay signal** — present;
  the C1 finding is an incomplete *binding* of its positive authority, not a
  removal of the mechanism.
- **Durable state + receipts; access token never persisted; refresh token never
  read into the projector** — INTACT.
- **Redaction contract (`refresh_token_absent`)** — INTACT.
- **RFC 0096 / #135 / #296 trust boundary** — INTACT (B2 identity is
  `SO_PEERCRED` uid; no control-plane token to the lane).
- **`provider_auth_gate=off` cannot bypass projection or the same-user refusal**
  — INTACT.

No carry-forward regressed. (Falsifier 2's projection-off route is a **new**
C3 gap, not a regression of an accepted carry-forward.)

## New material challenges

- **dialogue:2 (C1 lens) — STANDS (landed_unrebutted).** Daemon-owned positive
  authority is not bound to the stalled session's launch generation; an
  overlapping newer same-lane-user projection proves an older expired session
  fresh and reopens the generic-budget-burn leak.
- **dialogue:3 (C3 lens) — STANDS (landed_unrebutted).**
  `provider_credential_projection=off` is a live distinct-UID launch route that
  can read a pre-existing whole-credential lane file (raw refresh token) — C3's
  forbidden "by ANY route."

There is no rebuttal turn in this falsification trajectory (holder publishes, the
falsifiers attack), so both stand unrebutted by construction and on substance.

## Binding revision constraints (carry into a v5 run)

1. **C1 — bind recovery freshness to the stalled session's generation.** Persist
   receipt id / delivery mode / `destination_generation_id` / `expires_at` on the
   session/supervisor/job launch record; classify the stalled job's own bound
   generation, never the latest lane-user singleton row; per-session debt; add
   `TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection`.
2. **C3 — close the projection-off refresh-token route.** Fail closed for Claude
   OAuth under `provider_credential_projection=off` (preferred), or require the
   all-surfaces `refresh_token_absent` scan before launch under that flag; add
   `TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface` and
   `TestProjectionOffStillValidatesClaudeConfigDir`.
3. **C2 (non-binding) — fix the gate ordering.** Move the same-user precondition
   before the Codex-only provider-auth gate so the typed
   `provider_credential_same_user_unsupported` error fires in all gate modes;
   test `gate=auto/off/required`.

## Preserve (v4 got these right — do not regress in v5)

- The same-user fail-closed decision and its "no in-uid boundary" argument (C2
  custody) — keep it; only fix the gate ordering.
- The distinct-UID access-token-only projection (B1 no-`refreshToken` file / B2
  `SO_PEERCRED` socket); single-writer operator-side refresh authority; the
  explicit no-daemon-OAuth-client non-goal.
- The daemon-owned positive predicate for the **single-session** case and the
  downgrade-only lane re-sample inversion (it correctly closes the v3 forgery);
  v5 must *extend* it with session-generation binding, not discard it.
- The spawn-time gate placement, typed refusal vocabulary, redaction contract,
  the `provider_credential_projection=off` boundary semantics that **do** hold
  (it must also fail closed for Claude OAuth), and the distinct-UID RTR /
  source-rotation-race / owner-mode / symlink-escape / trust-boundary /
  resolver-path tests.

## Gate status

This was the single allowed v4 revision cycle. With `needs_revision`, the gate
ends **uncleared** and routes to the operator, who should spin a fresh `-v5` run
with a revising holder that discharges C1 and C3 (and fixes the C2 ordering
caveat) while preserving the v4 work listed under **Preserve**.
