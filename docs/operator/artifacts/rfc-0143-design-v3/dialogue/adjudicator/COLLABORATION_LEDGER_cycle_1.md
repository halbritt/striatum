---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
topic: "RFC 0143 lane credential survival across a daemon boot-epoch rotation — falsifiable implementation spec (design-v3 REVISION; binding constraints BC1-BC5)"
participants:
  - "holder-author-001"
  - "falsifier-reviewer-001"
  - "falsifier-reviewer-002"
  - "adjudicator-author-001"
cycle: 1
entries:
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: "v3 revises the v2 spec to resolve the five binding constraints BC1-BC5 in one new daemon mutation resealInFlightJob (go/pkg/mutations/recovery_reseal_rotation.go, distinct from the RFC 0125 HandleRecoveryReseal worktree verb), and carries the v2-credited set forward unregressed. BC1: the control path is never parsed terminal output — the Slice-A floor and the Slice-B reseal trigger ride the agentloop wrapper's OS process EXIT CODE (reserved ExitUnrecoverableAcrossRotation=97 / ExitResealInFlightRequested=98, recorded by the helper without inspecting output bytes via agentExitPayload/processExitCode, helper.go:427-439), and any mid-life signal rides a daemon-owned INHERITED FILE DESCRIPTOR (socketpair, passed to the wrapper as fd 3 via ExtraFiles, never inherited by the provider child) carrying a SupervisorControlFrame with a single-use control_nonce; the channel is asserted un-spoofable by the provider AND by same-uid sibling lanes because it has 'no name in the filesystem.' BC2: artifact identity is derived from the job's expected_artifacts (daemon state), refusing unexpected paths, reusing verifyRequiredArtifacts/ensurePerJobPublishedArtifactsDurable; front-matter failure routes to the Option-4 floor. BC3: CapabilityReseal is declared a daemon-internal marker projected by resealInFlightJob (supervisor_id->session_id, internal AuthContext, no public Authorize prelude), with the public route-alternate kept test-only. BC4: a concrete monotonic jobs.recovery_generation column (owner bundle 0021, modelled on review_generation), incremented at claim/requeue/recovery-sweep/release, stamped into work_packets.packet_json lease.recovery_generation and compared under the lock. BC5: a numeric resealGraceWindow=30s hard-capped at heartbeat_after_seconds, one same-lease extension via leases.reseal_grace_extended_at, and the exact lockRunForJob (pg_advisory_xact_lock) before FOR UPDATE on jobs/leases/job_recovery_state, identical to the seal paths and the recovery sweep, so expired-beyond-grace always routes the typed session_unrecoverable_across_rotation class, never a raw lease_error and never a revived requeued lease. Carries forward F2 (no lane-readable reseal bearer), F4 (route-specific ResealAlternate recording reseal not write), the F7 file-mirror half, AF1/AF4, the no-admin-token-widening invariant, and the per-claim assertion discipline (A1-A14)."
  - kind: challenge
    by: "falsifier-reviewer-001"
    refs: ["dialogue:2"]
    correspondence: landed_unrebutted
    text: "BC1 is not genuinely closed. (C1, material) The inherited-fd control channel is still SAME-UID reachable through /proc: the holder's 'no filesystem name' rationale is false on Linux — a same-uid sibling lane can open /proc/<wrapper-pid>/fd/3 to duplicate the wrapper's control fd and read /proc/<wrapper-pid>/environ to recover STRIATUM_SUPERVISOR_CONTROL_NONCE under ordinary procfs/ptrace rules. The frame schema authenticates only fd-possession + nonce, both obtainable by a same-uid process, so a sibling can send a syntactically valid reseal_requested / unrecoverable_across_rotation frame for the victim supervisor without touching provider stdout/stderr or any bearer file. This is the exact same-uid threat model the SEED pins (the reason the 0600 file was rejected for F2), re-created on the replacement channel — the same category mistake the v2 gate rejected. BC2 bounds blast radius to the victim's own in-flight job, but the daemon does not authenticate that the victim WRAPPER requested the seal, so a sibling can prematurely drive a seal or a durable unrecoverable blocker on another active session (false provenance). The named v3 tests (TestPTYOutputCannotEmitSupervisorControlEvent, TestProviderOutputCannotDriveResealOrBlocker, TestBorrowedResealBearerCannotSealVictimSession) only catch stdout spoofing and bearer-file presence; none attempts a same-uid non-child/non-wrapper process opening fd 3 or replaying the nonce via /proc. The spec names no peer-credential check (SO_PASSCRED/SCM_CREDENTIALS against the launched wrapper pid+start-time), no PR_SET_DUMPABLE(0), no nonce-out-of-env, no per-lane-uid defense. (C2, smaller, still BC1) Reserved exit codes 97/98 are not reserved until provider child statuses are masked: avoiding common collisions is not an auth boundary — if the provider exits 97/98 and the wrapper propagates that status, the helper records the wrapper exit code (helper.go:427-439) and v3 routes it into a blocker/resealInFlightJob; the spec must say the wrapper never propagates provider child statuses into the reserved codes and add a test. BC2 and BC3 are resolved at design level; F2 and F4 are not regressed. Treat BC1 as still open and return needs_revision, not reject."
  - kind: challenge
    by: "falsifier-reviewer-002"
    refs: ["dialogue:3"]
    correspondence: landed_unrebutted
    text: "Material challenge: trusted channel with NO trusted intent source. v3 closes the spoofing hole by cutting the provider out of the control channel (provider does not inherit fd 3; the wrapper does not inspect provider output), but the provider is the only actor that knows it finished the deliverable and lost MCP after rotation — current agent-loop source puts the semantic work in the provider, not the wrapper (loop.go:220-368: the wrapper prepares the command, starts the PTY, runs receiver/keepalive/endpoint goroutines, and waits for the child; it does not know whether the deliverable is complete). So ExitResealInFlightRequested=98 and the inherited-fd frame have no specified, non-PTY, non-bearer, non-sibling-replayable source of publish/complete intent: there is no path by which 'I have a complete artifact on disk; seal it' reaches the wrapper without returning to parsed PTY output or provider-controlled process exit. The spec proves provider output cannot FORGE reseal but has not shown a real provider can REQUEST reseal at all. Either v3 commits to automatic/speculative reseal on a precise daemon-observed condition (post-rotation child exit with all required expected_artifact paths present and modified in the active worktree since the packet, resealInFlightJob attempting only daemon-derived artifacts and mapping every validation/backend failure to the typed floor) — but that rebuttal is not in the spec and changes the mechanism — or it must name a concrete agent-requested intent path. Additional lifecycle conflict: the reserved exit arrives as an agent_exited event, whose branch STOPS the supervisor (supervision.go:298-306); the normal work.complete core then requires a live attached backend via ensureWorkSessionBackend (claim.go:310-365; lifecycle.go:1178-1183). v3 says resealInFlightJob reuses the lower-level publish/complete routines but does not define whether the post-exit reseal path bypasses/replaces/reinterprets that backend gate, so a straight reuse leaks invalid_transition/backend errors instead of the typed class (TestResealExit98BypassesBackendGateOrRoutesTyped). BC4 and most of BC5 are genuinely concrete now (no longer the v2 hand-wave), but two BC5 precision risks remain: (1) the grace marker leases.reseal_grace_extended_at is not pinned to a concrete migration (owner bundle 0021 if leases is owner-held, else a runtime migration — a downstream decision, not a site); (2) the claim that the seal paths 'already take lockRunForJob first' is inaccurate for work.complete, which performs session-binding and active-session checks BEFORE lockRunForJob (lifecycle.go:1135-1155), so the spec must refactor the complete core under the reseal lock or name exactly which gates are skipped/replayed. F1/BC1-channel, F6 (Codex positive path), and the F7 channel half stay open through this missing positive-trigger; F2/F3-BC4/F4-BC3/F7 file-mirror are credited."
verdict: "needs_revision"
rationale: "This adjudicates the design-v3 REVISION (the third falsification pass on RFC 0143) against the SEED clearing condition: a clearing verdict requires ALL FIVE binding constraints BC1-BC5 genuinely resolved with a concrete mechanism AND the v2-credited resolved set carried forward unregressed AND no new material challenge standing unrebutted. v3 is materially stronger than v2 — it stops relying on parsed PTY output, names two non-PTY mechanisms (the trusted-wrapper exit code and an inherited-fd control channel), makes CapabilityReseal a daemon-internal projection, derives artifact identity from daemon state, and pins the lifecycle cluster to a concrete jobs.recovery_generation column, numeric grace, and lockRun order. Both falsifiers independently credit BC2, BC3, BC4 as resolved at design level, and confirm F2, F4, the F7 file-mirror half, AF1/AF4, and the no-admin-token-widening invariant are NOT regressed. But the gate does NOT clear. BC1 is open on three independent material grounds, all unrebutted: (1) falsifier-reviewer-001's C1 — the inherited-fd channel is still same-uid reachable via /proc/<wrapper-pid>/fd/3 and the nonce via /proc/<wrapper-pid>/environ, so a sibling striatum-lane process can replay a valid control frame for the victim supervisor; the 'no filesystem name' rationale is the same same-uid category mistake the SEED's threat model forbids (and that killed the v1 0600 file), and the spec names no peer-credential/dumpability/nonce-isolation defense and no negative test for the attack; (2) falsifier-reviewer-001's C2 — reserved exit codes 97/98 are not reserved until the wrapper is committed to never propagating provider child statuses into them; (3) falsifier-reviewer-002's positive-intent gap — by cutting the provider out of the channel v3 leaves NO specified, non-PTY, non-bearer, non-sibling-replayable source of publish/complete intent, so the Slice-B reseal trigger is a trusted channel with no trusted protocol, plus an unresolved agent_exited/ensureWorkSessionBackend backend-gate conflict that would leak invalid_transition/backend errors instead of the typed class. BC5 is improved but not cleanly resolved: the load-bearing leases.reseal_grace_extended_at column has no pinned migration site (the same resolved-without-a-concrete-mechanism shape BC4 was required to avoid), and the work.complete lock-order claim is inaccurate (its session-binding/active-session gates run before lockRunForJob, lifecycle.go:1135-1155), leaving the exact gates-skipped-or-replayed under the internal reseal path unspecified — which is the same backend-gate routing question. I credit the falsifiers' load-bearing source citations as consistent with the v3 spec's own re-anchored sites (helper.go:427-439 exit-code capture; supervision.go:298-306 agent_exited branch; loop.go:220-368 wrapper-vs-provider semantic split; lifecycle.go:1135-1155 pre-lock complete gates; the owner-bundle/review_generation precedent for BC4). Clearing condition walked: (1) all five BC resolved — FAILS (BC1 open on three grounds; BC5 open precision items; BC2/BC3/BC4 resolved); (2) v2-credited set carried forward unregressed — HOLDS; (3) no new material challenge unrebutted — FAILS (both falsifier challenges land unrebutted; the cycle ends at adjudication and the spec text does not pre-empt them). Why not reject: no path widens admin-token exposure and no minted credential carries any of {admin,apply,recovery,surgical_recovery}; the C1 defect is a same-uid false-provenance/replay surface bounded by BC2 to the victim's own in-flight job, not admin-token widening or a lane-readable elevated credential; both falsifiers explicitly recommend needs_revision and supply concrete repairs. Why not accept_with_findings: BC1 is a security-cluster binding constraint and the SEED's load-bearing closure for F1/F6/the F2/F7 channel halves; the security invariant must hold STRUCTURALLY, and no-replay does NOT hold structurally on the replacement channel — that is not a trackable post-clearance finding. Verdict: needs_revision. The next (out-of-run) revision must, in one place: pin the BC1 channel's same-uid authentication (peer-credential check against the launched wrapper pid+start-time via SO_PASSCRED/SCM_CREDENTIALS, PR_SET_DUMPABLE(0) before the fd/nonce are live, nonce out of the same-uid-readable env) with a negative test (TestSiblingSameUIDCannotOpenControlFDOrReplayNonceViaProc / TestControlFrameRequiresExpectedWrapperPeerCredentials run against a non-child non-wrapper same-uid process); commit the wrapper to never propagating provider child statuses 97/98 into the reserved codes (TestProviderExitCodeCannotDriveReservedAgentloopResealOrBlocker); pin the trusted positive intent source for Slice-B reseal — either automatic/speculative reseal on a precise daemon-observed post-rotation condition with all validation/backend failures mapped to the typed floor, or a concrete non-PTY/non-bearer/non-sibling-replayable provider-to-wrapper intent path — and make TestCodexResealUsesReceiverNotProviderStdout include a positive case; define whether the post-exit reseal complete path bypasses/replaces the ensureWorkSessionBackend gate so it routes the typed class (TestResealExit98BypassesBackendGateOrRoutesTyped); and pin leases.reseal_grace_extended_at to a concrete migration location plus correct the work.complete lock-order story (name the pre-lock gates skipped/replayed). Carry forward unregressed: BC2, BC3, BC4, F2, F4, the F7 file-mirror half, AF1, AF4, the no-admin-token-widening invariant, and the A1-A14 assertion discipline. Maintainer-ratification note (carries regardless of verdict): Slice B (the new daemon-internal rpc.CapabilityReseal marker, the test-only auth-prelude route alternate, the inherited-fd supervisor control channel, the reserved agentloop exit codes, the jobs.recovery_generation owner-bundle column, and endpoint/epoch republish plumbing) is a security/authz trust-model change requiring maintainer ratification before any build slice touches credential code; adjudicator clearance gates the spec's soundness, not the maintainer's product call. Slice A (the Option-4 typed-exit-code floor) is zero-trust-change but, per the open BC1, still must route over a real, non-PTY channel with the same-uid authentication fixed before it lands."
findings:
  - id: BC1
    severity: high
    posture: design
    status: open
    source_refs: ["dialogue:2", "dialogue:3"]
    affected_invariants:
      - "R2 replay / false-provenance (no same-uid-reachable channel a sibling lane can present)"
      - "product-boundary: terminal output is not authoritative workflow state"
      - "R4 legible-failure (the typed floor must fire, never leak a raw backend error)"
    challenge: "Authenticated control channel NOT genuinely closed — three unrebutted grounds. (C1) The inherited fd is same-uid reachable: a sibling can open /proc/<wrapper-pid>/fd/3 and read /proc/<wrapper-pid>/environ to obtain the nonce, then send a valid frame for the victim supervisor; the 'no filesystem name' rationale repeats the same-uid category mistake the SEED's threat model forbids; the named tests do not attempt the attack; no peer-credential/dumpability/nonce-isolation defense is named. (C2) Reserved exit codes 97/98 are not reserved until the wrapper is committed to never propagating provider child statuses into them. (Positive-intent gap) Cutting the provider out of the channel leaves no specified non-PTY/non-bearer/non-sibling-replayable source of publish/complete intent, plus an unresolved agent_exited/ensureWorkSessionBackend backend-gate conflict (supervision.go:298-306; claim.go:310-365; lifecycle.go:1178-1183) that would leak invalid_transition/backend errors instead of the typed class. Fix: add an explicit same-uid process-bound peer-authentication contract (SO_PASSCRED/SCM_CREDENTIALS vs the launched wrapper pid+start-time, PR_SET_DUMPABLE(0), nonce out of env) with TestSiblingSameUIDCannotOpenControlFDOrReplayNonceViaProc / TestControlFrameRequiresExpectedWrapperPeerCredentials; mask/remap provider child statuses with TestProviderExitCodeCannotDriveReservedAgentloopResealOrBlocker; pin the positive reseal trigger (automatic-on-daemon-condition or a concrete provider->wrapper path) with a positive TestCodexResealUsesReceiverNotProviderStdout case; and define the post-exit backend-gate routing with TestResealExit98BypassesBackendGateOrRoutesTyped. Keeps F1, F6, and the F2/F7 channel halves open for Slice B; the Slice-A exit-97 floor is plausibly closed once C2 and the same-uid auth land."
  - id: BC2
    severity: high
    posture: design
    status: accepted
    source_refs: ["dialogue:2", "dialogue:3"]
    challenge: "RESOLVED at design level (both falsifiers credit). resealInFlightJob derives the expected-artifact set from the job's expected_artifacts (daemon state), reuses verifyRequiredArtifacts/ensurePerJobPublishedArtifactsDurable, publishes only a path that is an open expected entry from the job's own worktree, and refuses unexpected paths; the signal supplies neither path nor content, and front-matter/author-line failure routes to the Option-4 floor rather than a silent drop. falsifier-reviewer-001: no material security gap in BC2. CAVEAT: BC2's TRIGGER depends on the open BC1 channel; the identity-from-state property itself is sound and carries forward. Keep TestCodexResealUsesReceiverNotProviderStdout (add a positive case under BC1)."
  - id: BC3
    severity: high
    posture: design
    status: accepted
    source_refs: ["dialogue:2", "dialogue:3"]
    challenge: "RESOLVED at design level (both falsifiers credit). CapabilityReseal is declared a daemon-internal capability marker: resealInFlightJob maps supervisor_id->session_id, constructs an internal AuthContext{Capability: reseal} WITHOUT the public Authorize prelude, and calls the lower-level publish/complete routines; the public route-alternate (ResealAlternate on only interrogation.answer/work.complete/artifact.publish, recording reseal not write) is kept TEST-ONLY since no production bearer exists, with registry_methods.go generated and the command-authority-matrix + guardrail updated. Coherent provided the build proves no production bearer can present CapabilityReseal. CAVEAT: the BC3 'validation/reuse path for publish/complete/answer' overlaps the unresolved BC1/BC5 backend-gate question (ensureWorkSessionBackend after the supervisor is stopped) — close that under BC1/BC5. Keep TestResealCapabilityIsDaemonInternalNotBearer / TestResealTokenCanReachOnlyResealRoutesWithoutWrite."
  - id: BC4
    severity: high
    posture: design
    status: accepted
    source_refs: ["dialogue:3"]
    challenge: "RESOLVED — genuinely improved, no longer the v2 hand-wave (falsifier-reviewer-002 credits). Concrete monotonic jobs.recovery_generation column in owner bundle go/pkg/db/sql/owner/0021_job_recovery_generation.sql (ADD COLUMN IF NOT EXISTS ... integer NOT NULL DEFAULT 0), LatestOwnerBundleVersion 20->21 + RESERVATIONS.toml ordinal-21, modelled on review_generation; a degrade-safe JobRecoveryGenerationColumnPresent probe that routes to the typed floor when the column is absent; four increment points (claim, requeue-same-attempt, recovery-sweep expire/transfer/respawn, release), each in the same UPDATE that retires/rebinds the authoritative lease under lockRun; and the post-increment value stamped into work_packets.packet_json lease.recovery_generation, compared equal/unequal at reseal under the lock (mismatch -> typed class). Monotonic by construction (+1 only), like jobs.attempt/review_generation, and owner-DDL placement is consistent with TestFutureRuntimeMigrationsDoNotCarryOwnerDDL. Keep TestResealPredicateUsesStampedRecoveryGeneration / TestResealTokenRefusedAfterLeaseExpiryAndRecoveryRequeue."
  - id: BC5
    severity: high
    posture: design
    status: open
    source_refs: ["dialogue:3"]
    affected_invariants:
      - "R3 split-brain across rotation"
      - "R4 legible-failure (typed class must fire, never a raw lease_error)"
    challenge: "IMPROVED but not cleanly resolved — two precision risks land. The numeric resealGraceWindow=30s capped at heartbeat_after_seconds, the one same-lease extension, the lockRun-before-FOR-UPDATE order (jobs->leases->job_recovery_state), the sweep's drain-before-lock / expire-inside-lock structure, and the expired-beyond-grace -> typed class routing (no activeLeaseFor, no raw lease_error) are all concrete and correct in shape. But (1) the load-bearing grace marker leases.reseal_grace_extended_at has NO pinned migration site (v3 leaves owner-bundle-0021-vs-runtime as a downstream decision) — the same resolved-without-a-concrete-mechanism shape BC4 was required to avoid; and (2) the claim that the seal paths 'already take lockRunForJob first' is inaccurate for work.complete, whose session-binding and active-session gates run BEFORE lockRunForJob (lifecycle.go:1135-1155), so the spec must either refactor the complete core under the reseal lock or name exactly which gates are skipped/replayed under the internal reseal path — which is also the BC1 backend-gate routing question. Fix: pin the column's migration location concretely; correct/clarify the complete-path lock order; keep TestResealBeyondGraceRoutesTypedNotLeaseError / TestResealGraceCannotReviveRequeuedLease / TestRecoveryRequeueWinsOverExpiredLeaseReseal / GD-1b and add TestResealExit98BypassesBackendGateOrRoutesTyped."
  - id: CF
    severity: high
    posture: design
    status: accepted
    source_refs: ["dialogue:2", "dialogue:3"]
    challenge: "CARRIED-FORWARD SET intact (no regression — both falsifiers confirm). F2: no lane-readable reseal bearer reintroduced; the v1 0600 same-uid file replay stays retired (the residual migrates onto the BC1 channel, not a reopening). F4: the public route-alternate stays limited to interrogation.answer/work.complete/artifact.publish, records reseal not write, and is not the production no-token path. F7 file-mirror half: daemon-owned lane-read-only endpoint/epoch, O_NOFOLLOW, atomic rename, missing-epoch rejected (channel half inherits the open BC1). AF1 reachability-not-reminting (TestTokenValidAcrossRestart) and AF4 epoch/token decoupling: kept. No-admin-token-widening invariant: held and strengthened (CapabilityReseal carries no elevated verb and is never materialized into any lane-readable file; no minted credential carries admin/apply/recovery/surgical_recovery). Per-claim falsifiable-assertion discipline: extended to A1-A14 covering channel + generation. Preserve all of these verbatim through the next revision."
branches:
  design: blocked
---

# Collaboration Ledger — RFC 0143 design-v3 REVISION (cycle 1)

author: adjudicator-author-001

> Adjudication of the design-v3 REVISION dialogue trajectory for RFC 0143
> (*lane credential survival across a daemon boot-epoch rotation*). This is the
> **third** falsification pass on the RFC: design-v1 (`rfc-0143-design`) returned
> `needs_revision` with seven findings F1–F7; design-v2 (`rfc-0143-design-v2`)
> resolved **F2** and **F4** cleanly but returned `needs_revision` again with two
> material challenges and four nominally-closed findings, which the v2 adjudicator
> distilled — and `SEED.md` expanded — into the **five binding constraints
> BC1–BC5** (security cluster BC1+BC2+BC3, lifecycle cluster BC4+BC5). The Holder
> revised the spec to resolve BC1–BC5 and the two falsifiers re-attacked it.
> Inputs read: the revised Holder spec (`dialogue/holder/HOLDER.md`), both
> falsifier re-attacks (`dialogue/falsifier_1/FALSIFIER.md`,
> `dialogue/falsifier_2/FALSIFIER.md`), the `SEED.md` charter (the ratified design
> shape, the v2-cleared carry-forward set, and the BC1–BC5 constraints with their
> exact source anchors), the **v2** `HOLDER.md` (the spec being revised), and the
> **v2** collaboration ledger (the verdict + full BC analysis). No raw terminal
> output was read.

## Verdict

**verdict: needs_revision**

v3 is **materially stronger than v2** and resolves three of the five binding
constraints at the design level, while carrying the entire v2-credited set forward
**unregressed**. But it does **not** clear the gate: **BC1 is open on three
independent material grounds** and **BC5 has two open precision items**, and both
falsifiers' re-attacks **land unrebutted** (the cycle ends at adjudication; the
Holder had no turn, and the spec text does not pre-empt them). This is a
security/authz-hot gate held high; it is not yet a buildable spec.

**Why not `reject`.** No path widens admin-token exposure, and no minted credential
carries any of `{admin, apply, recovery, surgical_recovery}`. The decisive BC1
defect (C1) is a **same-uid false-provenance / replay surface** on the replacement
control channel, **bounded by BC2** to the victim's *own* in-flight job — not
admin-token widening and not a lane-readable elevated credential. Both falsifiers
explicitly recommend `needs_revision` and each supplies a concrete repair. So this
is `needs_revision`.

**Why not `accept_with_findings`.** BC1 is a **security-cluster** binding
constraint and the SEED's load-bearing closure for F1/F6 and the F2/F7 channel
halves. A clearing verdict requires the security invariant to hold
**structurally** — and **no-replay does not hold structurally** on the inherited-fd
channel as specified. That is not a trackable post-clearance finding; it forecloses
a clearing verdict.

### The clearing condition, walked

A clearing verdict requires **all three** to hold; **two fail**:

1. **All five BC1–BC5 genuinely resolved with a concrete mechanism — FAILS.**
   BC2, BC3, BC4 are resolved at design level (both falsifiers credit them). **BC1
   is open** on three grounds (below); **BC5** is improved but has two open
   precision items.
2. **The v2-credited resolved set carried forward unregressed — HOLDS.** F2, F4,
   the F7 file-mirror half, AF1, AF4, the no-admin-token-widening invariant, and
   the per-claim assertion discipline are all intact (finding **CF**).
3. **No new material challenge standing unrebutted — FAILS.** falsifier-reviewer-001
   (C1 `/proc` same-uid replay + C2 reserved-exit propagation) and
   falsifier-reviewer-002 (the positive-intent-source gap + the
   `agent_exited`/backend-gate conflict + the two BC5 precision items) both land.

## Constraint-by-constraint walk (BC1–BC5 + carry-forward)

| Constraint | Cluster | Disposition | One-line reason |
| --- | --- | --- | --- |
| **BC1** | security | **open** | Inherited-fd channel is same-uid reachable via `/proc/<pid>/fd` + `/proc/<pid>/environ`; reserved exit codes not masked against provider propagation; and no trusted, non-PTY positive source of Slice-B reseal intent. |
| **BC2** | security | **resolved** | Artifact identity derived from `expected_artifacts` (daemon state), refuses unexpected paths; signal carries no path/content. Trigger depends on the open BC1. |
| **BC3** | security | **resolved** | `CapabilityReseal` is a daemon-internal marker projected by `resealInFlightJob`; public route-alternate kept test-only. Reuse/validation path overlaps the BC1/BC5 backend-gate question. |
| **BC4** | lifecycle | **resolved** | Concrete `jobs.recovery_generation` (owner bundle 0021), increment points, stamped value, equal/unequal comparison under the lock — modelled on `review_generation`. |
| **BC5** | lifecycle | **open** (precision) | Numeric grace + lock order are concrete, but `leases.reseal_grace_extended_at` has no pinned migration site and the `work.complete` lock-order claim is inaccurate. |
| **CF** | carry-forward | **intact** | F2, F4, F7 file-mirror half, AF1, AF4, no-widening, assertion discipline — all unregressed. |

### BC1 — Authenticated control channel, NOT parsed PTY output (open)

v3 makes **real progress** the gate credits: it commits to two **non-PTY**
mechanisms — the trusted-wrapper **OS exit code** (97/98, captured by the helper
via `agentExitPayload`/`processExitCode`, `helper.go:427-439`, never read from
stdout/stderr) and a daemon-owned **inherited file descriptor** (`socketpair`, fd 3
via `ExtraFiles`, not inherited by the provider child). Both falsifiers agree v3 no
longer relies on parsed PTY output, and `TestPTYOutputCannotEmitSupervisorControlEvent`
/ `TestProviderOutputCannotDriveResealOrBlocker` would catch provider-stdout
spoofing. But three material gaps land **unrebutted**:

- **C1 (falsifier-reviewer-001) — the inherited fd is still same-uid reachable
  through `/proc`.** The Holder's claim that the channel is "un-spoofable by
  same-uid sibling lanes" because it has "no name in the filesystem"
  (`HOLDER.md:176-216`, `:512-514`) is **false on Linux**: a same-uid sibling can
  open `/proc/<wrapper-pid>/fd/3` to duplicate the control fd and read
  `/proc/<wrapper-pid>/environ` to recover `STRIATUM_SUPERVISOR_CONTROL_NONCE`. The
  frame authenticates only **fd-possession + nonce**, both obtainable by a same-uid
  process, so a sibling can send a syntactically valid `reseal_requested` /
  `unrecoverable_across_rotation` frame for the victim supervisor without touching
  provider output or any bearer file. This is the **exact same-uid threat model the
  SEED pins** (`SEED.md:69-76` — the reason the `0600` file was rejected for F2),
  re-created on the replacement channel: the same category mistake the v2 gate
  rejected. BC2 bounds the blast radius to the victim's own in-flight job, but the
  daemon does **not** authenticate that the victim *wrapper* requested the seal, so
  a sibling can prematurely drive a seal or a durable `session_unrecoverable_across_rotation`
  blocker on another active session (**false provenance**). The generation guard
  (BC4) blocks stale-requeue split-brain; it does **not** distinguish the real
  wrapper from a same-uid process writing through a duplicated fd while the
  generation is current. The named tests do **not** attempt this attack, and the
  spec names **no** peer-credential check (`SO_PASSCRED`/`SCM_CREDENTIALS` against
  the launched wrapper pid + start-time), **no** `PR_SET_DUMPABLE(0)`, **no**
  nonce-out-of-env, and **no** per-lane-uid defense. **Material, unrebutted.**

- **C2 (falsifier-reviewer-001) — reserved exit codes are not reserved until
  provider statuses are masked.** Choosing 97/98 "in the rarely-used high range to
  avoid collision" (`HOLDER.md:143-164`) is **not an auth boundary**: if the
  provider child exits 97/98 and the wrapper **propagates** that status, the helper
  records the wrapper exit code (`helper.go:427-439`) and v3 routes it into a
  blocker / `resealInFlightJob`. The spec must commit that the wrapper **never**
  propagates provider child statuses into the reserved codes (remap to a
  non-control `agent_exited`). Smaller, but part of BC1. **Material, unrebutted.**

- **Positive-intent gap (falsifier-reviewer-002) — a trusted channel with no
  trusted intent source.** By cutting the provider out of the channel (provider
  does not inherit fd 3; the wrapper does not inspect provider output), v3 leaves
  **no specified, non-PTY, non-bearer, non-sibling-replayable source** of
  publish/complete intent. Current agent-loop source puts the semantic work in the
  **provider**, not the wrapper (`loop.go:220-368`): the wrapper prepares the
  command, runs the PTY and goroutines, and waits for the child — it does **not**
  know whether the deliverable is complete. So `ExitResealInFlightRequested = 98`
  and the inherited-fd frame have no concrete trigger; the spec proves provider
  output cannot **forge** reseal but has not shown a real provider can **request**
  it at all. Either v3 commits to **automatic/speculative** reseal on a precise
  daemon-observed condition (post-rotation child exit with all required
  `expected_artifact` paths present + modified in the active worktree since the
  packet, attempting only daemon-derived artifacts and mapping every
  validation/backend failure to the typed floor) — *that rebuttal is in
  falsifier-reviewer-002's "strongest rebuttal for the Holder," not in the spec,
  and changes the mechanism* — or it must name a concrete provider→wrapper intent
  path. Plus a concrete lifecycle conflict: the reserved exit arrives as an
  `agent_exited` event whose branch **stops the supervisor** (`supervision.go:298-306`),
  and the normal `work.complete` core then requires a live attached backend via
  `ensureWorkSessionBackend` (`claim.go:310-365`; `lifecycle.go:1178-1183`); v3
  says `resealInFlightJob` "reuses the lower-level publish/complete routines" but
  never defines whether the post-exit reseal path bypasses/replaces/reinterprets
  that backend gate, so a straight reuse leaks `invalid_transition`/backend errors
  instead of the typed class. **Material, unrebutted.**

BC1 is the security cluster's load-bearing closure (F1/F6 and the F2/F7 channel
halves all inherit it). It stays **open**.

### BC2 — Artifact identity from daemon state (resolved)

`resealInFlightJob` derives the expected-artifact set from `jobs.expected_artifacts_json`
(attempt-resolved), reuses `verifyRequiredArtifacts` / `ensurePerJobPublishedArtifactsDurable`,
publishes only a `path` that is an open expected entry from the job's own worktree,
and **refuses any unexpected path**; the signal supplies neither path nor content,
and a front-matter/author-line failure **routes to the Option-4 floor** rather than
a silent drop. falsifier-reviewer-001 explicitly: "resolved at design level… I do
not see a material security gap in BC2." **Accepted as resolved.** Caveat: BC2's
*trigger* depends on the open BC1 channel; the identity-from-state property itself
is sound and carries forward (keep `TestCodexResealUsesReceiverNotProviderStdout`,
adding a positive case under BC1).

### BC3 — `CapabilityReseal` is a daemon-internal marker (resolved)

v3 declares `CapabilityReseal` a **daemon-internal** capability marker: `resealInFlightJob`
maps `supervisor_id`→`session_id`, constructs an internal `AuthContext{Capability:
reseal}` **without** the public `Authorize` prelude, and calls the lower-level
publish/complete routines; the public route-alternate (`ResealAlternate` on only
the three routes, recording `reseal` not `write`) is kept **test-only** since no
production bearer exists, with `registry_methods.go` generated and the
`command-authority-matrix` + guardrail updated. Both falsifiers credit this.
**Accepted as resolved.** Caveat: the BC3 "validation/reuse path for
publish/complete/answer" overlaps the unresolved BC1/BC5 backend-gate question —
close it there.

### BC4 — Concrete monotonic generation column (resolved)

falsifier-reviewer-002 credits this as "genuinely improved" and "no longer the v2
hand-wave." v3 names the concrete `jobs.recovery_generation` column in owner bundle
`go/pkg/db/sql/owner/0021_job_recovery_generation.sql` (`ADD COLUMN IF NOT EXISTS …
integer NOT NULL DEFAULT 0`), bumps `LatestOwnerBundleVersion` 20→21 with the
ordinal-21 reservation, modelled exactly on the credited `review_generation`
precedent; adds a degrade-safe `JobRecoveryGenerationColumnPresent` probe that
**routes to the typed floor** when the column is absent (never seals without the
guard); names the four increment points (claim, requeue-same-attempt, recovery-sweep
expire/transfer/respawn, release), each in the same UPDATE that retires/rebinds the
authoritative lease under `lockRun`; and **stamps** the post-increment value into
`work_packets.packet_json` `lease.recovery_generation`, comparing equal/unequal at
reseal under the lock (mismatch → typed class). Monotonic by construction, and the
owner-DDL placement is consistent with `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL`.
**Accepted as resolved.**

### BC5 — Numeric grace + exact lock order (open, precision)

The shape is now concrete and largely correct: `resealGraceWindow = 30s` hard-capped
at `heartbeat_after_seconds`; one same-lease extension via `leases.reseal_grace_extended_at`;
`lockRunForJob` (`pg_advisory_xact_lock(hashtext(run_id))`) **before** `FOR UPDATE`
on `jobs`→`leases`→`job_recovery_state`; the sweep's drain-before-lock /
expire-inside-lock structure cited accurately; and **expired-beyond-grace always
routes the typed class** (no `activeLeaseFor`, no raw `lease_error`). But two
precision items land (falsifier-reviewer-002):

1. **The load-bearing grace marker `leases.reseal_grace_extended_at` has no pinned
   migration site** — v3 leaves "owner bundle 0021 if `leases` is owner-held, else a
   runtime migration" as a downstream decision. A load-bearing column with an
   undecided migration location is the **same resolved-without-a-concrete-mechanism
   shape BC4 was required to avoid**.
2. **The lock-order claim is inaccurate for `work.complete`**, whose session-binding
   and active-session gates run **before** `lockRunForJob` (`lifecycle.go:1135-1155`);
   so the spec must either refactor the complete core under the reseal lock or name
   exactly which gates are skipped/replayed under the internal reseal path — the
   same backend-gate routing question as BC1's positive-intent conflict.

**Open** on these two items.

### CF — Carry-forward set (intact, unregressed)

Both falsifiers confirm no regression: **F2** (no lane-readable reseal bearer; the
v1 `0600` same-uid file replay stays retired — the residual migrates onto the BC1
channel, not a reopening); **F4** (route-alternate limited to the three routes,
records `reseal` not `write`, not the production no-token path); **F7 file-mirror
half** (daemon-owned lane-read-only endpoint/epoch, `O_NOFOLLOW`, atomic rename,
missing-epoch rejected — channel half inherits the open BC1); **AF1**
reachability-not-reminting (`TestTokenValidAcrossRestart`); **AF4** epoch/token
decoupling; the **no-admin-token-widening invariant** (held + strengthened —
`CapabilityReseal` carries no elevated verb and is never materialized into any
lane-readable file); and the **per-claim falsifiable-assertion discipline**
(extended to A1–A14). Preserve all verbatim through the next revision.

## What the next (out-of-run) revision MUST fix to clear on re-attack

All within BC1/BC5 — the security cluster's channel authentication and the
positive-intent protocol:

1. **BC1 channel same-uid authentication.** Replace "no filesystem name" with an
   explicit process-bound peer-authentication contract for fd 3 and the nonce:
   per-message kernel credentials (`SO_PASSCRED` / `SCM_CREDENTIALS`) checked
   against the **launched wrapper's pid + start-time**, `PR_SET_DUMPABLE(0)` on the
   wrapper before the fd/nonce are live, and the nonce kept out of the
   same-uid-readable env. Add `TestSiblingSameUIDCannotOpenControlFDOrReplayNonceViaProc`
   / `TestControlFrameRequiresExpectedWrapperPeerCredentials`, run against a process
   that is **neither the provider child nor the launched wrapper**.
2. **BC1 reserved-exit reservation.** State that the wrapper **never** propagates
   provider child statuses 97/98 into the reserved codes (remap to a non-control
   `agent_exited`). Add `TestProviderExitCodeCannotDriveReservedAgentloopResealOrBlocker`.
3. **BC1/F6 positive intent source.** Pin the trusted, non-PTY, non-bearer,
   non-sibling-replayable mechanism by which "deliverable complete on disk, seal it"
   reaches the daemon — either **automatic/speculative** reseal on a precise
   daemon-observed post-rotation condition (all required `expected_artifact` paths
   present + modified in the active worktree since the packet; only daemon-derived
   artifacts attempted; every validation/backend failure mapped to the typed floor),
   or a concrete agent-requested intent path. Make `TestCodexResealUsesReceiverNotProviderStdout`
   include a **positive** case.
4. **BC1/BC5 backend-gate routing.** Define whether the post-exit reseal complete
   path bypasses/replaces `ensureWorkSessionBackend` (the `agent_exited` branch
   stops the supervisor), so it routes the typed `session_unrecoverable_across_rotation`
   class rather than leaking `invalid_transition`/backend errors. Add
   `TestResealExit98BypassesBackendGateOrRoutesTyped`.
5. **BC5 precision.** Pin `leases.reseal_grace_extended_at` to a concrete migration
   location (decide owner-held vs runtime now), and correct the `work.complete`
   lock-order claim — name exactly which pre-`lockRunForJob` gates are
   skipped/replayed under the internal reseal path.

Everything credited resolved (BC2, BC3, BC4) and the entire carry-forward set (F2,
F4, F7 file-mirror half, AF1, AF4, no-widening, A1–A14) is sound — carry it forward
unchanged.

## Note on maintainer ratification (carries forward regardless of verdict)

Even when a future revision clears, the chosen direction — a new daemon-internal
`rpc.CapabilityReseal` marker, the test-only auth-prelude route alternate, the
inherited-fd supervisor control channel, the reserved agentloop exit codes, the
`jobs.recovery_generation` owner-bundle column, and the endpoint/epoch republish
plumbing — is a **security/authz trust-model change** requiring **maintainer
ratification** before any build slice touches credential code. Adjudicator
clearance gates the spec's **soundness**; it is **not** the maintainer's product
call on the credential code. Slice A (the Option-4 typed-exit-code floor) is
zero-trust-change, but per the open BC1 it still must route over a real, non-PTY
channel with the **same-uid authentication fixed** before it lands.

---
<sub>Adjudicator collaboration ledger for the RFC 0143 falsification-gate
design-v3 REVISION run (cycle 1). The ledger verdict — not falsifier completion —
gates the phase: `needs_revision` returns the spec uncleared. BC2/BC3/BC4 are
resolved and the v2-credited set is carried forward unregressed, but BC1 is open on
three material grounds (the `/proc` same-uid channel replay, unmasked reserved exit
codes, and a trusted channel with no trusted positive intent source) and BC5 has
two open precision items; both falsifiers' re-attacks land unrebutted.</sub>
