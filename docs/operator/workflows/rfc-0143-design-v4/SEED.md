# Design-Run Seed (v4 / REVISION) — RFC 0143 Lane credential survival across a daemon boot-epoch rotation

> **THIS IS THE FOURTH REVISION (v4).** Three prior design runs ran the same
> falsification gate on this RFC. v1 (`rfc-0143-design`) returned `needs_revision`
> with seven findings F1–F7. v2 (`rfc-0143-design-v2`) **resolved F2 and F4
> cleanly** and distilled the residue into the five binding constraints BC1–BC5.
> v3 (`rfc-0143-design-v3`) **resolved BC2, BC3, and BC4 at the design level**
> (both falsifiers credited them) and **carried the v2-credited set forward
> unregressed**, but returned `needs_revision` **again**: **BC1 stands open on
> three independent material grounds** and **BC5 has two open precision items**,
> and both falsifiers' re-attacks landed unrebutted (the v3 cycle exhausted its
> single allowed revision). This v4 run is a **proper revision**: the holder
> starts from the **v3** `HOLDER.md` (a required context doc), REVISES the spec to
> **resolve the two remaining binding constraints BC1 and BC5** (each with its
> exact unrebutted grounds, below), and **carries the entire v3-credited resolved
> set forward unregressed** (BC2, BC3, BC4 + F2, F4, the F7 file-mirror half,
> AF1, AF4, the no-admin-token-widening invariant, the A1–A14 assertion
> discipline); the falsifiers re-attack the revised spec.
>
> The v3 design record — `dialogue/holder/HOLDER.md`,
> `dialogue/falsifier_1/FALSIFIER.md`, `dialogue/falsifier_2/FALSIFIER.md`, and
> `dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md` — lives under
> `docs/operator/artifacts/rfc-0143-design-v3/`; the **v3** `HOLDER.md` (the spec
> being revised) and the **v3** collaboration ledger (the verdict + the full
> BC1/BC5 analysis, the exact unrebutted grounds, and the prescribed fixes) are
> wired in as required `context_docs`.
>
> This document is the **required input** for the RFC 0143 design run. It is
> operator-supplied design-run scaffolding. The canonical proposal is committed at
> `docs/rfcs/0143-lane-credential-survival-across-boot-epoch-rotation.md` (status
> `proposed`) — read it in full as your primary source; this SEED carries the
> charter, **pins the ratified design shape (do not relitigate)**, states **what
> already cleared by v3 (carry forward, do not reopen)**, and lists the **two
> binding constraints v4 MUST resolve** (BC1's three grounds + BC5's two items),
> each anchored to exact source sites. Read this whole file, the **v3**
> `HOLDER.md` + the **v3** collaboration ledger, and the RFC before producing any
> artifact.

## Framing — what this run must produce

This is a **design run**, not an implementation run. RFC 0143 is the security/authz
problem of **lane credential survival across a daemon boot-epoch rotation**: when a
lane loses its live RPC connection across a daemon restart, its
credential-resolution chain falls through to the full-authority bootstrap admin
`client-token` (which a `striatum-lane` lane cannot read), so a complete-on-disk
deliverable exits unsealed with a misleading permission error. The deliverable of
this run is a **falsifiable implementation spec** the `rfc-0143-build` run can
execute contract-first (TDD), produced by hardening the v3 spec against adversarial
falsification.

The v3 falsification gate found the v3 design **`needs_revision`**. The holder must
produce a proposal that **resolves the two remaining binding constraints BC1 and
BC5 below** while **carrying the v3-credited resolved set forward unregressed**. A
revised spec that leaves BC1 or BC5 open — or regresses any carried-forward item
(BC2, BC3, BC4, F2, F4, the F7 file-mirror half, AF1, AF4, the
no-admin-token-widening invariant, the A1–A14 discipline) — has NOT cleared the
gate. This is the gate's single allowed revision cycle for v4, so a second
`needs_revision` ends the gate uncleared. Both constraints must land in **one
coherent proposal**, both inside the **security/lifecycle channel** the v3 spec
already pinned (the daemon-internal `resealInFlightJob` mutation, the trusted-wrapper
exit code, the inherited-fd control channel, the `jobs.recovery_generation` column,
the numeric grace + lock order).

## Ratified design shape (do NOT relitigate)

The maintainer has ratified the trust-model shape and the F2 replay defense; these
are binding and **override any softer framing**. No prior gate cycle contested them —
do not reopen them, build on them:

- **OQ1 — trust-model shape (ratified): Option 4 + ratification-gated Option 2 +
  minimal Option 3.**
  - **Slice A (mandatory, lands first, ZERO trust-model change):** Option 4 — a
    legible, self-escalating `session_unrecoverable_across_rotation` signal
    replacing the silent unsealed exit. This is the floor; it must be buildable and
    valuable on its own. **Per the still-open BC1 it must route over a real,
    non-PTY-output channel with the same-uid authentication fixed before it lands.**
  - **Slice B (ratification-gated):** Option 2's *narrow* reseal authority — a
    session-scoped `CapabilityReseal` covering ONLY the in-flight job's seal
    (`work.complete` / `artifact.publish` / `interrogation.answer`), **never** any
    of `{admin, apply, recovery, surgical_recovery}` and **never plain `write`** —
    folding in a minimal Option 3 per-session endpoint+epoch republish so the lane
    never needs to read the admin `client-token`.
- **F2 — replay defense (DECIDED): non-bearer, daemon-owned, session-tied channel.
  NO readable reseal token file.** Because all lanes currently share the
  `striatum-lane` OS user, a `0600` reseal *file* is a same-uid replay surface
  readable by sibling lanes. The ratified resolution: deliver/verify the
  `CapabilityReseal` authority over the **daemon-owned supervisor session-tied
  channel** — there is NO lane-readable reseal token file at all. The daemon proves
  the calling session, not a bearer file. Do NOT reintroduce a readable bearer file
  as the reseal credential under any option.
- **Slice B requires maintainer ratification before any build slice touches
  credential code.** Adjudicator clearance gates the spec's *soundness*, not the
  maintainer's product call. Slice A is zero-trust-change and may land first under
  the normal review gate **once BC1 makes it routed over a real, non-PTY-output
  channel with the same-uid authentication fixed.**

## Carried forward — resolved by v3 (do NOT reopen)

> The v3 collaboration ledger records the following as genuinely resolved / sound;
> **both v3 falsifiers credited BC2, BC3, BC4 and confirmed F2, F4, the F7
> file-mirror half, AF1, AF4, the no-widening invariant, and the A1–A14 discipline
> are NOT regressed.** The v4 revision MUST preserve them — verbatim from the **v3**
> `HOLDER.md` where applicable — and the cycle-4 adjudicator's clearing verdict
> requires them intact. Re-opening any of these is a regression that fails the gate.

- **BC2 — RESOLVED (artifact identity from daemon state).** `resealInFlightJob`
  derives the expected-artifact set from the job's `expected_artifacts` (daemon
  state, attempt-resolved via `resolveExpectedArtifactCycles`), reuses
  `verifyRequiredArtifacts` / `ensurePerJobPublishedArtifactsDurable`
  (`go/pkg/mutations/mutations.go:828-876`), publishes only a `path` that is an open
  expected entry from the job's own worktree, and **refuses any unexpected path**;
  the signal supplies neither path nor content, and a front-matter/author-line
  failure routes to the Option-4 floor rather than a silent drop. Both falsifiers
  credited "no material security gap in BC2." Its *trigger* depends on the open BC1
  channel; the identity-from-state property itself is sound. Keep
  `TestCodexResealUsesReceiverNotProviderStdout` (and add the BC1 positive case).
- **BC3 — RESOLVED (`CapabilityReseal` is a daemon-internal marker).**
  `resealInFlightJob` maps `supervisor_id` → `session_id` from the supervision row,
  constructs an **internal** `rpc.AuthContext{Capability: CapabilityReseal,
  SessionID, RepositoryID}` **without** the public `Authorize` prelude
  (`go/pkg/rpc/server.go:107-111`), and calls the lower-level publish/complete
  routines against the active worktree; the public route-alternate
  (`MethodEntry.ResealAlternate` on only `interrogation.answer` / `work.complete` /
  `artifact.publish`, recording `reseal` not `write`) is kept **test-only** since no
  production bearer exists. `registry_methods.go` is generated; the
  `command-authority-matrix` reseal column + the authority guardrail are updated.
  Both falsifiers credited this. Keep `TestResealCapabilityIsDaemonInternalNotBearer`
  / `TestResealTokenCanReachOnlyResealRoutesWithoutWrite`. (Its publish/complete
  reuse path overlaps the BC1/BC5 backend-gate question — close that under BC1/BC5.)
- **BC4 — RESOLVED (concrete monotonic generation column).** The concrete
  `jobs.recovery_generation` column ships in owner bundle
  `go/pkg/db/sql/owner/0021_job_recovery_generation.sql`
  (`ALTER TABLE striatumd.jobs ADD COLUMN IF NOT EXISTS recovery_generation integer
  NOT NULL DEFAULT 0`), bumps `LatestOwnerBundleVersion` 20→21
  (`go/pkg/db/owner.go:23`) with the ordinal-21 `RESERVATIONS.toml` reservation,
  modelled exactly on the credited `review_generation` precedent
  (`go/pkg/db/sql/owner/0009_review_generation.sql`); `striatumd.jobs` is owner-held
  (consistent with `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL`, and with bundles
  12/13/16 already `ALTER`ing it). A degrade-safe `JobRecoveryGenerationColumnPresent`
  probe routes to the typed floor when the column is absent. The four increment
  points (claim, requeue-same-attempt, recovery-sweep expire/transfer/respawn,
  release), each in the same UPDATE that retires/rebinds the authoritative lease
  under `lockRun`, are named; the post-increment value is stamped into
  `work_packets.packet_json` `lease.recovery_generation`, compared equal/unequal at
  reseal under the lock (mismatch → typed class). Both falsifiers credited this as
  "no longer the v2 hand-wave." Keep `TestResealPredicateUsesStampedRecoveryGeneration`
  / `TestResealTokenRefusedAfterLeaseExpiryAndRecoveryRequeue`.
- **F2 — RESOLVED (bearer-file retirement).** No lane-readable reseal bearer exists;
  the v1 `0600` same-uid file replay stays retired. The *residual*
  replay/false-provenance question migrates onto the BC1 channel — that residual is
  **BC1**, not a reopening of F2. Keep `TestBorrowedResealBearerCannotSealVictimSession`.
- **F4 — RESOLVED (auth mechanism without plain `write`).** The route-specific
  `MethodEntry.ResealAlternate` admits `CapabilityReseal` on **only** the three
  routes and records `AuthContext.Capability == reseal` (never `write`). Keep
  `TestResealTokenCanReachOnlyResealRoutesWithoutWrite` and the
  command-authority-matrix reseal column. Preserve this wiring exactly.
- **F7 file-mirror half — RESOLVED.** Endpoint/epoch moves to a **daemon-owned,
  lane-read-only `0644` file** with `O_NOFOLLOW` symlink defense and atomic
  temp-and-rename, and a supervised request with a **MISSING** boot-epoch header is
  **rejected** — closing the permissive header-absent #316 path on the supervised
  path. Keep `TestResealEpochMirrorRejectsTamperOrMissingEpoch`. (The *channel*
  half of F7 inherits the open BC1.)
- **AF1 — reachability-not-reminting.** The session-bound token stays *valid* across
  a restart; only its *reachability* breaks. The fix is **routing**, not re-minting.
  Keep `TestTokenValidAcrossRestart`.
- **AF4 — epoch/token decoupling.** Endpoint rotation and boot-epoch rotation are
  coupled; #316 deliberately retires a surviving lane's connection. The token does
  NOT rotate on a normal restart (only the endpoint does). Preserve this framing.
- **The categorical no-admin-token-widening invariant.** No lane ever reads the
  daemon's full-authority bootstrap admin `client-token`
  (`go/pkg/admin/bootstrap.go:18-27` grants
  `{admin,read,write,claim,review,apply,recovery,surgical_recovery}`); no minted
  credential carries any of `{admin, apply, recovery, surgical_recovery}`. Held +
  strengthened by never materializing `CapabilityReseal` into any lane-readable
  file. Keep `TestResolveRefusesRuntimeClientTokenForLane`.
- **The per-claim falsifiable-assertion discipline (A1–A14).** Every load-bearing
  claim is paired with the named test / game-day that refutes it. Extend it to cover
  the BC1 peer-credential/dumpability/nonce-isolation and the BC5 migration-site +
  lock-order mechanisms; do not abandon it.

## The 2 binding constraints v4 MUST resolve (the v3 adjudicator's unrebutted needs_revision grounds)

> The v3 ledger §"What the next (out-of-run) revision MUST fix" pins the exact
> repairs. This SEED carries them verbatim in shape. Each names exact source sites;
> anchor every load-bearing claim in the revised spec to them, paired with the named
> test.

### BC1 — the replacement (non-PTY) control channel is STILL same-uid replayable; and the positive-intent source is unspecified (closes F1 / F6 / the channel half of F2 / F7-channel)

The v3 spec stops relying on parsed PTY output and names two non-PTY mechanisms —
the trusted-wrapper **OS exit code** (97/98, captured by the helper via
`agentExitPayload` / `processExitCode`, `go/pkg/supervisor/helper.go:427-439`, never
read from stdout/stderr; recognised in the existing `agent_exited` branch of
`recordSuperviseReportEvent`, `go/pkg/mutations/supervision.go:298-306`) and a
daemon-owned **inherited file descriptor** (`socketpair`, fd 3 via `ExtraFiles`, not
inherited by the provider child). Both falsifiers agree v3 no longer relies on
parsed PTY output. **But BC1 is open on THREE unrebutted grounds, ALL of which must
be fixed in one place:**

1. **Same-uid replay on the inherited-fd channel (C1, material).** v3's inherited-fd
   control channel is **still same-uid reachable**: a sibling `striatum-lane` process
   can open `/proc/<wrapper-pid>/fd/3` to duplicate the wrapper's control fd and read
   `/proc/<wrapper-pid>/environ` to recover `STRIATUM_SUPERVISOR_CONTROL_NONCE`. The
   frame authenticates only **fd-possession + nonce**, both obtainable by a same-uid
   process, so a sibling can send a syntactically valid `reseal_requested` /
   `unrecoverable_across_rotation` frame for the **victim supervisor** — false
   provenance, the daemon never authenticates that the victim *wrapper* requested the
   seal. The "no filesystem name" rationale **repeats the exact same-uid category
   mistake** that killed the v1 `0600` file (the SEED threat model forbids it — the
   reason F2 retired the file). The named v3 tests do not attempt this attack.
   **Fix (in one place):** pin same-uid authentication on the channel —
   - a **peer-credential check against the launched wrapper's pid + start-time via
     `SO_PASSCRED` / `SCM_CREDENTIALS`** (the daemon/helper verifies the connecting
     peer is the **expected wrapper process**, not merely same-uid);
   - call **`PR_SET_DUMPABLE(0)` on the wrapper BEFORE the control fd and the nonce
     are live** (so `/proc/<wrapper-pid>/fd` and `/proc/<wrapper-pid>/environ` are not
     readable by same-uid siblings);
   - keep the **nonce out of the same-uid-readable environment**.

   **Negative tests:** `TestSiblingSameUIDCannotOpenControlFDOrReplayNonceViaProc`,
   `TestControlFrameRequiresExpectedWrapperPeerCredentials` — **both run against a
   process that is NEITHER the provider child NOR the launched wrapper** (a non-child,
   non-wrapper, same-uid process).

2. **Reserved exit codes not actually reserved (C2, material).** Reserved agentloop
   exit codes 97/98 are **not reserved** until the wrapper is COMMITTED to never
   propagating a provider child's status into them. Choosing a high range "to avoid
   collision" is **not an auth boundary**: if the provider child exits 97/98 and the
   wrapper **propagates** that status, the helper records the wrapper exit code
   (`helper.go:427-439`) and v3 routes it into a blocker / `resealInFlightJob`.
   **Fix:** the wrapper must **never** forward provider child statuses 97/98 into the
   reserved codes (remap them to a non-control `agent_exited`); the agentloop owns the
   97/98 semantics exclusively. **Test:**
   `TestProviderExitCodeCannotDriveReservedAgentloopResealOrBlocker`.

3. **Positive-intent gap (material).** By cutting the provider out of the channel
   (provider does not inherit fd 3; the wrapper does not inspect provider output),
   v3 leaves **NO specified non-PTY, non-bearer, non-sibling-replayable source of
   publish/complete *intent*** — the Slice-B reseal trigger is a **trusted channel
   with no trusted protocol**. The provider is the only actor that knows it finished
   the deliverable, but the current agent-loop puts the semantic work in the provider,
   not the wrapper (`go/pkg/agentloop/loop.go:220-368`: the wrapper prepares the
   command, runs the PTY/goroutines, and `cmd.Wait()`s for the child — it does not
   know the deliverable is complete). Plus an `agent_exited` / `ensureWorkSessionBackend`
   backend-gate conflict: the reserved exit arrives as an `agent_exited` event whose
   branch **stops the supervisor** (`supervision.go:298-306`), and the normal
   `work.complete` core then requires a live attached backend via
   `ensureWorkSessionBackend` (`lifecycle.go:1135-1183`), so a straight reuse would
   leak `invalid_transition` / backend errors instead of the typed floor class.
   **Fix:** pin the trusted positive-intent source for Slice-B reseal — EITHER
   - **(a) automatic/speculative reseal on a precise daemon-observed post-rotation
     condition** (post-rotation child exit with ALL required `expected_artifact`
     paths present + modified in the active worktree since the packet;
     `resealInFlightJob` attempts only daemon-derived artifacts; ALL
     validation/backend failures mapped to the typed floor class); OR
   - **(b) a concrete non-PTY / non-bearer / non-sibling-replayable
     provider-to-wrapper intent path** (a path satisfying the C1 peer-credential
     defense above).

   Make `TestCodexResealUsesReceiverNotProviderStdout` include a **POSITIVE** case
   (a real provider can *request* — or the daemon can *automatically observe* — a
   reseal, not merely be proven unable to forge one); and **define whether the
   post-exit reseal-complete path bypasses/replaces the `ensureWorkSessionBackend`
   gate** so it routes the typed `session_unrecoverable_across_rotation` class —
   test `TestResealExit98BypassesBackendGateOrRoutesTyped`.

BC1 is the security cluster's load-bearing closure (F1 / F6 and the F2 / F7 channel
halves all inherit it); the security invariant must hold **structurally** — no-replay
must hold structurally on the channel, not as a trackable post-clearance finding.

### BC5 — lifecycle precision items still open (closes F5)

The v3 numeric grace and lock-order *shape* are concrete and correct
(`resealGraceWindow = 30s` hard-capped at `heartbeat_after_seconds`; one same-lease
extension; `lockRunForJob` (`pg_advisory_xact_lock(hashtext(run_id))`) before
`FOR UPDATE` on `jobs` → `leases` → `job_recovery_state`; the sweep's
drain-before-lock / expire-inside-lock structure; expired-beyond-grace always routing
the typed class with no `activeLeaseFor` / no raw `lease_error`). **But two precision
items land:**

1. **The load-bearing `leases.reseal_grace_extended_at` column has NO pinned
   migration site.** v3 left "owner bundle 0021 if `leases` is owner-held, else a
   runtime migration" as a downstream decision — the **same
   resolved-without-a-concrete-mechanism shape BC4 was required to avoid**.
   **Fix:** pin it to a concrete migration / owner-bundle location.
   `striatumd.leases` is created in runtime migration `0005`
   (`go/pkg/db/sql/0005_repo_local_workflow_state.sql:166`) and is **owner-held** —
   it is NOT in the migration-0016+ ownership-transfer cohort
   (`go/pkg/db/sql/owner/0018_runtime_table_ownership_transfer.sql`), so an
   owner-held column-add is owner DDL; pin `reseal_grace_extended_at` to the **same
   owner bundle 0021** as `jobs.recovery_generation`, consistent with
   `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` — name this concretely, do not
   leave it as an open downstream decision.

2. **The `work.complete` lock-order claim is INACCURATE.** v3 asserted the seal paths
   "already take `lockRunForJob` first"; for `work.complete` that is false — its
   session-binding (`enforceSessionBindingForSession`) and active-session
   (`enforceActiveActingSession`) gates run **BEFORE** `lockRunForJob`
   (`go/pkg/mutations/lifecycle.go:1135-1155`), and `activeLeaseFor` +
   `ensureWorkSessionBackend` run after the lock (`:1178-1183`). **Fix:** correct the
   lock-order story — name **exactly which pre-lock gates are skipped or replayed**
   under the internal reseal path (the internal `resealInFlightJob` projects the
   AuthContext from daemon state, so the public session-binding/active-session prelude
   does not apply the same way), and state **how the internal `resealInFlightJob` path
   serializes against `artifact.publish` / `work.complete` / the recovery sweep** so
   that expired-beyond-grace ALWAYS routes the typed
   `session_unrecoverable_across_rotation` class (this is the same backend-gate routing
   question as BC1's positive-intent conflict). Keep
   `TestResealBeyondGraceRoutesTypedNotLeaseError` /
   `TestResealGraceCannotReviveRequeuedLease` /
   `TestRecoveryRequeueWinsOverExpiredLeaseReseal` / `GD-1b`, and add
   `TestResealExit98BypassesBackendGateOrRoutesTyped`.

## Clearing condition for this revision

The adjudicator clears the gate only if **both binding constraints BC1 (all three
grounds) and BC5 (both items) are genuinely resolved** with a concrete mechanism and
named tests, **AND the v3-credited resolved set is carried forward unregressed**
(BC2, BC3, BC4, F2, F4, the F7 file-mirror half, AF1, AF4, the
no-admin-token-widening invariant, the A1–A14 assertion discipline), **AND no new
material challenge** stands unrebutted. The verdict is `reject` only if a path widens
admin-token exposure or mints a credential carrying any of
`{admin, apply, recovery, surgical_recovery}`; otherwise `needs_revision` if BC1 or
BC5 remains open, if any credited item is regressed, or any new material challenge
lands. One revision cycle is available within this run; the falsifiers re-attack the
revised spec.

## Maintainer-ratification note (carries regardless of verdict)

Slice B — the daemon-internal `rpc.CapabilityReseal` marker, the test-only
auth-prelude route alternate, the inherited-fd supervisor control channel, the
reserved agentloop exit codes, the `jobs.recovery_generation` owner-bundle column,
and the endpoint/epoch republish plumbing — is a **security/authz trust-model change
requiring maintainer ratification before any build slice touches credential code**.
Adjudicator clearance gates the spec's **soundness**, not the maintainer's product
call. Slice A (the Option-4 typed-exit-code floor) is zero-trust-change but, per the
open BC1, still must route over a real, non-PTY channel **with the same-uid
authentication fixed** before it lands.

---
<sub>Operator scaffold for the RFC 0143 falsification-gate design run (v4 / REVISION
of `rfc-0143-design-v3`; resolves the two remaining binding constraints BC1 — its
three unrebutted grounds: same-uid peer-credential/dumpability/nonce-isolation,
reserved-exit-code commitment, positive-intent source — and BC5 — its two precision
items: pinned migration site + corrected `work.complete` lock-order — and carries the
v3-credited set BC2/BC3/BC4 + F2/F4/F7-file/AF1/AF4/no-widening/A1–A14 forward
unregressed). Lanes: author=claude (holder/adjudicator/committer), reviewer=codex
(falsifiers).</sub>
