---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
author: adjudicator-author-001
workflow: "rfc-0169-design"
run_id: "run_e4f752e682f1a2fb6f1a968d29b158b3"
cycle: 1
topic: "RFC 0169 P0-P2 provider-agnostic lane credential-readiness spine + ephemeral fresh-per-spawn placement for GH #583"
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
      The holder proposes RFC 0169 P0-P2: a provider-agnostic ProviderReadinessAdapter
      registry that collapses ~5 hardcoded switch sites into one interface + gate lookup
      (codex = RFC 0121 Check verbatim, agy = no-op receipt, claude = OAUTH_COPIED,
      unregistered = refuse-closed), plus a CredentialPlacer that re-mints a lane-private
      0600 Claude credential spawn-fresh under a lane-private CLAUDE_CONFIG_DIR/HOME prefix
      with a source-rotation race guard, and a daemon-side re-place-before-expiry heartbeat
      so the lane never reaches its own refresh path. Two load-bearing claims: (1) the
      registry is a behavior-preserving refactor with a single intended delta (unknown
      refuses); (2) spawn-fresh placement makes #583 structurally impossible without
      modifying the provider CLI, closing the 0165 cycle-1 F1/F2 findings at spine altitude.
  - kind: challenge
    by: "falsifier-reviewer-001"
    refs: ["dialogue:2"]
    correspondence: landed_unrebutted
    text: >-
      Falsifier 1 shows Hard Claim 2 does not pin the Claude CLI's actual OAuth store. The
      installed Claude Code 2.1.187 selects its credential store via a selector that consults
      CLAUDE_SECURESTORAGE_CONFIG_DIR BEFORE the normal config dir; the SPEC proves only
      Striatum's own ResolveCredential resolver (telemetry), never the CLI's selection, and
      its CLAUDE_CONFIG_DIR override does not cover the secure-storage selector. supervision_env.go
      allowlists only HOME/XDG_CONFIG_HOME and supervision_lane_config.go lets workflow command_env
      smuggle arbitrary non-STRIATUM_ keys, so a lane can read or rotate a different store while
      custody says the placed generation was fresh -- the placed file becomes a fresh decoy. The
      installed CLI also reads claudeAiOauth.refreshToken and writes refreshed OAuth back to the
      same store, so a placed refresh token reopens 0165 F2; an access-token-only payload requires
      proof the running process re-reads disk before each provider call, which a between-tick
      heartbeat does not provide.
  - kind: challenge
    by: "falsifier-reviewer-002"
    refs: ["dialogue:3"]
    correspondence: landed_unrebutted
    text: >-
      Falsifier 2 shows Hard Claim 1's P1 gate matrix is not coherently behavior-preserving.
      Today supervision_provider_auth.go has a codex-only support guard: under GateAuto a
      non-codex/unknown provider returns nil (passes), under GateRequired it refuses
      lane_provider_preflight_unsupported. The holder simultaneously asserts (a) the gate collapses
      to one registry lookup that refuses unregistered providers closed in all modes, (b) the
      class-independent support guard + GateAuto&&RunAsUser=="" skip + best-effort lane.auth_success
      scaffolding are preserved unchanged, and (c) agy and the P1 claude stub have zero behavior
      change. These cannot all hold: making unknown refuse under GateAuto requires changing the
      support guard, which also flips agy/claude GateRequired from unsupported-refusal to adapter-pass
      and can emit new lane.auth_success events -- more than the single declared delta. No full
      pre/post gate matrix resolves where the guard and skip live or proves unknown cannot fall
      through them.
  - kind: constraint
    by: "adjudicator-author-001"
    refs: ["dialogue:2"]
    text: >-
      The revision must pin EVERY Claude OAuth-store selector the installed CLI can use --
      including CLAUDE_SECURESTORAGE_CONFIG_DIR -- to the lane-private placement directory or
      refuse launch when any selector resolves outside it, prove it with a CLI-level conformance
      test (not only a resolver unit test), prevent workflow command_env from smuggling an
      alternate store, and prove the placed payload grants no lane-side raw-refresh-token
      writeback or independent rotation authority against the actual CLI payload.
  - kind: constraint
    by: "adjudicator-author-001"
    refs: ["dialogue:3"]
    text: >-
      The revision must supply a full pre/post gate matrix across {codex, agy, claude,
      unregistered} x {off, auto, required} x {self-driving, non-self-driving} x {RunAsUser
      empty, non-empty} with expected RPC error code and event emission per cell, state
      precisely where the support guard and no-lane-user skip live in the registry design,
      prove unregistered providers cannot fall through those skips, and either preserve
      agy/claude GateRequired + lane.auth_success behavior or enumerate each change as an
      intentional, separately-gated delta (the "single intended delta" framing is incomplete).
  - kind: constraint
    by: "adjudicator-author-001"
    refs: ["dialogue:2"]
    text: >-
      The revision must prove the runtime-freshness closure (OQ1) actually pre-empts the first
      post-expiry provider request of a long-running Claude process whose access token is cached
      in memory -- either by proving disk re-read before each provider call or by proving the
      re-validate interval + SIGTERM lands before the lane's next model/MCP action -- otherwise
      heartbeat re-placement is a race window between ticks, not the structural closure the
      acceptance criterion ("a dogfood longer than the access-token TTL completes without a
      stale-credential agent_mcp_discovery_stall") requires.
verdict: "needs_revision"
rationale: >-
  Both falsifier challenges are material and unrebutted by the proposal as written, and they
  attack the two load-bearing claims the gate exists to prove, not observability or polish. The
  holder does serious, well-anchored work: it ratifies the audit reframe against current source,
  states each claim as a falsifiable assertion with a named refuting test, discharges OQ2-OQ6
  soundly, and specifies a Layer 3 custody/breaker design that is daemon-state-only and
  tamper-resistant on its face. But Hard Claim 2 is NOT proven: the SPEC proves Striatum's own
  ResolveCredential resolver, not the installed Claude CLI's OAuth store selection, and Falsifier 1
  exhibits a concrete second selector (CLAUDE_SECURESTORAGE_CONFIG_DIR) that the SPEC never names
  and the supervised-env handling does not control -- so spawn-fresh placement can be a fresh decoy
  while the CLI reads or mutates a different store, which is the #583 class under a new path. Hard
  Claim 1 is NOT proven: Falsifier 2 shows the P1 properties are mutually incompatible and the
  required pre/post gate matrix is absent, so the "behavior-preserving seam before new logic"
  cannot be merge-gated as written. The OQ1 runtime-freshness closure is asserted as structural but
  is a between-tick race against an in-memory cached access token. Because either unproven hard
  claim, an unclosed runtime-freshness path, or a standing material falsifier challenge each
  independently blocks a clearing verdict -- and all three are present -- the dialogue does not
  clear. This is the single allowed v1 revision cycle; a revising holder must close the constraints
  below. The custody/breaker, generation-key (OQ3), placement-strategy (OQ2), expiry-lead (OQ4),
  run-dependency surfacing (OQ5), and subsumption (OQ6) decisions are sound and should be preserved.
findings:
  - id: F1-CLAUDE-OAUTH-STORE-SELECTOR-UNPINNED
    severity: critical
    posture: design
    status: open
    source_refs: ["dialogue:2"]
    affected_invariants:
      - "the path the CLI reads MUST be the path the placer writes is the path the gate checks (no decoy store)"
      - "a stale persistent copy cannot be the cause of a launch (#583 closed by construction)"
      - "a placed OAUTH_COPIED credential grants no lane-side refresh-token writeback or independent rotation"
    challenge: >-
      The SPEC proves only Striatum's ResolveCredential resolver (CLAUDE_CONFIG_DIR then
      HOME/.claude), not the installed Claude CLI's actual OAuth-store selection. Falsifier 1
      shows Claude Code 2.1.187 consults CLAUDE_SECURESTORAGE_CONFIG_DIR before the normal config
      dir; supervision_env.go allowlists only HOME/XDG_CONFIG_HOME, and command_env can smuggle a
      non-STRIATUM_ override, so the CLI can read or write a different store than the placed file
      while custody reports the placed generation fresh. The CLI's refresh-token writeback (it reads
      claudeAiOauth.refreshToken and persists rotated OAuth) is unaddressed; deferring the field-level
      mechanism to RFC 0165 is circular while claiming this spine closes 0165 F2.
    closest_acceptable_answer: >-
      Enumerate every OAuth-store selector the installed CLI can use, force each to the lane-private
      placement directory or refuse launch (e.g. provider_credential_selector_unmanaged) when any
      resolves outside it; extend ResolveCredential/readiness sampling to model the real CLI store
      precedence; prove with a CLI-level conformance test (run a no-network claude auth-status
      equivalent under temp HOME/CLAUDE_CONFIG_DIR/CLAUDE_SECURESTORAGE_CONFIG_DIR and assert the
      opened .credentials.json is the placed file or the launch refuses); prove command_env cannot
      smuggle an alternate store; and prove the placed payload cannot grant lane-side refresh/writeback.
    requested_constraint_shape:
      kind: gate
  - id: F2-P1-GATE-MATRIX-NOT-BEHAVIOR-PRESERVING
    severity: high
    posture: design
    status: open
    source_refs: ["dialogue:3"]
    affected_invariants:
      - "P1 is a pure refactor: codex Check verbatim, agy no-op, claude unchanged, seam proven before new logic"
      - "an unregistered provider refuses closed in ALL gate modes"
      - "the only intended behavior delta is the unregistered-provider refusal"
    challenge: >-
      The codex-only support guard means agy/claude/unknown pass under GateAuto and refuse
      unsupported under GateRequired today. The holder cannot simultaneously (a) make unknown refuse
      closed in all modes through one registry, (b) preserve the support guard + no-lane-user skip +
      event scaffolding unchanged, and (c) leave agy/claude with zero behavior change. Achieving (a)
      changes the guard, which flips agy/claude GateRequired and can emit new lane.auth_success
      events -- contradicting the single-delta claim. Assertion 1.3 (TestUnregisteredProviderRefusedAllModes)
      and 1.5 (TestGateModeMatrixUnchanged) cannot both pass with the guard intact.
    closest_acceptable_answer: >-
      Provide a full pre/post gate matrix across provider x gate-mode x agent-loop-mode x RunAsUser
      with expected RPC error and event per cell; state where the support guard and no-lane-user skip
      live in the registry design; prove unregistered providers cannot fall through those skips; and
      either preserve agy/claude GateRequired + lane.auth_success behavior or name each change as an
      intentional, separately-gated behavior delta (correcting the "single intended delta" framing).
    requested_constraint_shape:
      kind: gate
  - id: F3-RUNTIME-FRESHNESS-CLOSURE-IS-A-RACE-WINDOW
    severity: high
    posture: design
    status: open
    source_refs: ["dialogue:2"]
    affected_invariants:
      - "runtime decay never enters the generic agent_mcp_discovery_stall path"
      - "the daemon re-places before expiry so the lane never reaches its own refresh path"
      - "a dogfood longer than the access-token TTL completes without a stale-credential stall"
    challenge: >-
      The OQ1 closure (supervisor-helper re-ValidateReadiness/re-Place on the heartbeat ticker +
      SIGTERM on flip) is asserted to fire "before the lane's next model/MCP action," but a
      long-running Claude process caches the access token in memory; between heartbeat ticks the
      lane can issue a provider request and 401 before SIGTERM lands. Re-placing the on-disk file
      does not pre-empt an in-memory cached token, so the closure is a periodic race, not the
      structural guarantee the acceptance criterion demands. This is the 0165 cycle-1 F1
      runtime-expiry finding re-surfacing at spine altitude.
    closest_acceptable_answer: >-
      Prove the running provider process re-reads the credential from disk before each provider call,
      OR prove the re-validate interval + SIGTERM provably pre-empts the first post-expiry provider
      request (not merely lead/2 spacing), OR honestly bound and document the residual mid-run 401
      window and route it to provider-auth reseed_required rather than generic recovery budget.
    requested_constraint_shape:
      kind: gate
constraints:
  - id: C1-PIN-ALL-CLAUDE-OAUTH-STORE-SELECTORS
    posture: credential_custody
    severity: critical
    kind: gate
    binding: true
    source_finding: F1-CLAUDE-OAUTH-STORE-SELECTOR-UNPINNED
    source_refs: ["dialogue:2"]
    text: >-
      The revised spec must set or force-clear EVERY Claude OAuth-store selector the installed CLI
      uses -- including CLAUDE_SECURESTORAGE_CONFIG_DIR -- to the lane-private placement directory,
      or refuse launch with a typed reason when any selector resolves outside it. It must extend the
      resolver/readiness-sampling contract to model the actual Claude Code store precedence (not only
      CLAUDE_CONFIG_DIR and HOME), and must reject or overwrite any non-STRIATUM_ command_env key
      that can re-point an OAuth store.
    verification:
      gate: "A CLI-level conformance test runs a no-network claude auth-status equivalent under temp HOME, CLAUDE_CONFIG_DIR, and CLAUDE_SECURESTORAGE_CONFIG_DIR and asserts the opened .credentials.json is the placed lane-private file or the launch refuses; TestClaudeCommandEnvCannotSelectUnplacedSecureStorage proves command_env cannot smuggle an alternate store."
    final_review_required: true
  - id: C2-NO-LANE-RAW-REFRESH-TOKEN-WRITEBACK
    posture: credential_custody
    severity: critical
    kind: gate
    binding: true
    source_finding: F1-CLAUDE-OAUTH-STORE-SELECTOR-UNPINNED
    source_refs: ["dialogue:2"]
    text: >-
      The revised spec must prove, against the actual placed payload and the installed CLI's
      refresh/writeback path, that a placed OAUTH_COPIED credential cannot grant the lane independent
      refresh-token rotation or mutate the operator source family. If it keeps any file-copy design it
      must prove rotation cannot desynchronize/invalidate the operator source; if it brokers tokens it
      must name the surface. RFC 0165 may own the field-level mechanism, but this spine must state and
      test the rotation-isolation invariant rather than assume it. (Carries 0165 cycle-1 C1 forward.)
    verification:
      gate: "TestOAuthCopiedNoRawRefreshTokenWriteback simulates a lane-side refresh against the placed payload and asserts the operator source remains valid and the lane retains no independent refresh authority; concurrent-lane and subsequent-lane RTR cases included."
    final_review_required: true
  - id: C3-FULL-PREPOST-GATE-MATRIX
    posture: refactor_soundness
    severity: high
    kind: gate
    binding: true
    source_finding: F2-P1-GATE-MATRIX-NOT-BEHAVIOR-PRESERVING
    source_refs: ["dialogue:3"]
    text: >-
      The revised spec must include a full pre/post gate matrix across {codex, agy, claude,
      unregistered} x {off, auto, required} x {self-driving, non-self-driving} x {RunAsUser empty,
      non-empty}, with the expected RPC error code and event emission per cell. It must state where
      the support guard and the no-lane-user skip live in the registry design, prove unregistered
      providers cannot fall through those skips, and either preserve agy/claude GateRequired +
      lane.auth_success behavior or enumerate each as an intentional, separately-gated behavior delta
      -- correcting the "single intended delta" framing to list every delta.
    verification:
      gate: "TestGateModeMatrixUnchanged and TestUnregisteredProviderRefusedAllModes are reconciled so both pass; each matrix cell maps to its source transition, and every behavior delta is named and individually test-gated."
    final_review_required: true
  - id: C4-RUNTIME-CLOSURE-PREEMPTS-FIRST-POST-EXPIRY-CALL
    posture: recovery
    severity: high
    kind: gate
    binding: true
    source_finding: F3-RUNTIME-FRESHNESS-CLOSURE-IS-A-RACE-WINDOW
    source_refs: ["dialogue:2"]
    text: >-
      The revised spec must prove the runtime-freshness closure pre-empts the first post-expiry
      provider request of a long-running Claude process whose access token is cached in memory --
      by proving disk re-read before each provider call, by proving the re-validate interval + SIGTERM
      lands before the lane's next model/MCP action, or by honestly bounding the residual 401 window
      and classifying it as provider-auth reseed_required before any generic requeue/transfer counter
      increments. A between-tick race is not the structural closure the acceptance criterion requires.
      (Carries 0165 cycle-1 C2/C3 forward; the detecting signal stays daemon/helper-owned, never a
      lane-authored claim.)
    verification:
      gate: "TestRuntimeExpirySIGTERMsAndReseedsWithoutGenericBudget exercises a lane crossing the access-token TTL and asserts no generic requeue/transfer counter increments and no recovery_exhausted before reseed_required; the test models the in-memory-cached-token case, not only an on-disk re-read."
    final_review_required: true
branches:
  design: blocked
---

# Collaboration Ledger - RFC 0169 design run (cycle 1)

author: adjudicator-author-001

## Verdict

**verdict: needs_revision**

The holder proposal is a serious, well-anchored spine design. It ratifies the audit
reframe against current source (the gate is codex-only and fall-through for everything
else; codex's `Check` is the offline predicate to preserve; agy already places a fresh
lane-private `0600` file per launch; the RFC 0162 resolver/expiry already exist), states
each load-bearing claim as a falsifiable assertion with a named refuting test, discharges
OQ2-OQ6 soundly, and specifies a Layer 3 custody/breaker that is daemon-state-only and
tamper-resistant on its face. That is real progress, but it does not clear this gate.

A clearing verdict requires **both** hard claims PROVEN, the runtime-freshness closure
genuinely real, Layer 3 tamper-proof, and **no** standing material falsifier challenge.
Two of the three required conditions fail and both falsifier challenges stand.

## Per-hard-claim record

- **Hard Claim 1 (registry is a behavior-preserving refactor): NOT PROVEN.** Falsifier 2
  demonstrates the P1 properties are mutually incompatible. The current codex-only support
  guard makes non-codex/unknown providers pass under `GateAuto` and refuse
  `lane_provider_preflight_unsupported` under `GateRequired`. The holder cannot at once
  (a) refuse unregistered providers closed in **all** modes through one registry,
  (b) preserve the support guard / no-lane-user skip / event scaffolding unchanged, and
  (c) leave agy and claude with zero behavior change. Achieving (a) changes the guard, which
  flips agy/claude `GateRequired` and can emit new `lane.auth_success` events — more than the
  single declared delta. Assertions 1.3 (`TestUnregisteredProviderRefusedAllModes`) and 1.5
  (`TestGateModeMatrixUnchanged`) cannot both pass with the guard intact, and no full pre/post
  gate matrix resolves the contradiction. The "seam proven before new logic lands" cannot be
  merge-gated as written.

- **Hard Claim 2 (spawn-fresh placement structurally closes #583 without CLI modification):
  NOT PROVEN.** Falsifier 1 demonstrates the SPEC proves Striatum's own `ResolveCredential`
  resolver, not the installed Claude CLI's OAuth-store selection. The CLI (Claude Code 2.1.187)
  consults `CLAUDE_SECURESTORAGE_CONFIG_DIR` **before** the normal config dir; the SPEC never
  names that selector, `supervision_env.go` does not control it (it allowlists only
  `HOME`/`XDG_CONFIG_HOME`), and `command_env` can smuggle it. So the placed lane-private file
  can be a **fresh decoy** while the CLI reads or mutates a different store — the #583 class
  under a new path. The SPEC's own invariant ("the path the CLI reads is the path the placer
  writes … there is no global `~/.claude` decoy") is falsified. The refresh-token writeback
  path (the CLI persists rotated OAuth to the same store) is unaddressed; deferring the
  field-level mechanism to RFC 0165 is circular while claiming the spine closes 0165 F2.

## Per-open-question record

- **OQ1 (runtime-freshness closure): NOT genuinely closed.** The heartbeat re-`ValidateReadiness`
  / re-`Place` + SIGTERM is asserted to fire "before the lane's next model/MCP action," but a
  long-running Claude process caches its access token in memory; between ticks the lane can 401
  before SIGTERM. Re-placing the on-disk file does not pre-empt an in-memory token. This is a
  periodic race, not the structural closure the acceptance criterion demands (finding F3).
- **OQ2 (placement strategy default): RESOLVED.** atomic chown-write default, tmpfs+setuid P4
  opt-in; `O_CREAT|O_EXCL` temp + `fchown` + atomic rename, symlink-at-dest refused. Sound — and
  not materially attacked. (Note: its correctness is downstream of Hard Claim 2 — a correct write
  into the wrong store is moot.)
- **OQ3 (generation identity for opaque codex): RESOLVED.** monotonic-dispatch-seq +
  advisory-STALE-only, daemon `Check` authoritative for FRESH; lane FRESH claims ignored. Sound
  and not attacked.
- **OQ4 (minimum expiry lead time): RESOLVED.** `max(10min, TTL/4)`, re-place at `lead/2`. Reasonable;
  but `lead/2` spacing does not by itself prove pre-emption of an in-memory cached token (see OQ1/F3).
- **OQ5 (run dependency): RESOLVED (correctly surfaced, not over-committed).** P4-optional per SEED
  guidance.
- **OQ6 (subsumption): RESOLVED.** KEPT SEPARATE per operator decision; 0165 stays the `OAUTH_COPIED`
  detail; doc cross-link. Correct. (The cite-don't-re-derive split is fine in principle, but the
  carry-forward closure of 0165 F2 is not actually proven — it ties to Hard Claim 2 / C2.)

## Layer 3 and trust boundary

Layer 3 (daemon-only custody fingerprints + breaker counters, single-use job-scoped placer
capability, `0600` lane-owned destination written by fd/atomic-rename, no-cross-provider-read,
redaction of all secret material and private paths) is **not materially attacked** and is sound
on its face; it preserves the RFC 0096/#135/#296 trust boundary as specified. It is **not** the
blocker here. Its real-world value is, however, downstream of Hard Claim 2: a tamper-proof
custody chain over a credential the CLI never reads provides false assurance, so closing F1 also
strengthens Layer 3's guarantee.

## Per-falsifier-challenge record

- **Falsifier 1 (structural-prevention + CLI-compatibility + runtime-closure lens): STANDS.**
  Material and unrebutted by the proposal as written. Drives F1 (store-selector + refresh writeback)
  and F3 (runtime race). Its own strongest-rebuttal section fairly concedes that with only
  `CLAUDE_CONFIG_DIR` set the CLI does open the placed file — a valid partial control point — but
  that does not clear the claim, because the SPEC must pin **all** selectors and prove the payload
  grants no lane refresh authority.

- **Falsifier 2 (refactor-soundness + tamper-resistance + carry-forward lens): STANDS.**
  Material and unrebutted. Drives F2 (gate matrix). Its strongest-rebuttal section concedes the
  unknown-refusal is an intended improvement and that better `GateRequired` semantics for registered
  non-codex providers could be a feature — but that contradicts the holder's repeated "zero behavior
  change / single intended delta" framing, so the claim as written does not hold.

## Binding revision constraints (the single allowed v1 revision cycle)

The revising holder must close all four before downstream proposal publication:

1. **C1** — Pin every Claude OAuth-store selector (including `CLAUDE_SECURESTORAGE_CONFIG_DIR`) to
   the lane-private placement dir or refuse launch; model the real CLI store precedence in the
   resolver; reject/overwrite smuggled `command_env` keys; prove with a **CLI-level** conformance
   test, not only a resolver unit test.
2. **C2** — Prove no lane-side raw-refresh-token writeback / independent rotation against the actual
   placed payload (concurrent-lane and subsequent-lane RTR cases); carries 0165 C1 forward.
3. **C3** — Supply the full pre/post gate matrix; state where the support guard and no-lane-user
   skip live; prove unregistered cannot fall through; reconcile 1.3 with 1.5; enumerate every
   behavior delta.
4. **C4** — Prove the runtime closure pre-empts the first post-expiry provider call against an
   in-memory cached token, or bound the residual window honestly and classify it as
   `reseed_required` before any generic counter increments; carries 0165 C2/C3 forward.

## Preserve

The revision should preserve the holder's sound work: the `ProviderReadinessAdapter` /
assurance-class framing; codex = RFC 0121 `Check` verbatim and agy = no-op receipt as the
behavior-preserving targets (once the gate matrix in C3 is specified); the `CredentialPlacer`
seam with agy's `writeEphemeralGeminiSettings` as the zero-change first impl; the source-rotation
before/after race guard (`provider_credential_source_unstable`); OQ2 atomic chown-write +
symlink/TOCTOU refusal; OQ3 advisory-STALE-only generation key with daemon-authoritative FRESH;
OQ4 `max(10min, TTL/4)` lead; OQ5 run-dependency surfaced-not-committed; OQ6 kept-separate; and the
entire daemon-state-only, secret-free Layer 3 custody/breaker design and its preservation of the
RFC 0096 trust boundary.

> Gate status: this is the single allowed v1 revision cycle. A revising holder addresses C1-C4;
> if a second adjudication still returns `needs_revision`, the gate ends uncleared and routes to
> the operator (a fresh `-v2` run with a revising holder).
