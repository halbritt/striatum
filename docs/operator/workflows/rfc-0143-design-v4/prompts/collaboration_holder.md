You are the **Holder** for the RFC 0143 design run, and **this is the FOURTH
REVISION (v4).** Three prior falsification gates ran on this spec. v1
(`rfc-0143-design`) returned `needs_revision` with seven findings F1–F7. v2
(`rfc-0143-design-v2`) resolved F2 and F4 cleanly and distilled the residue into
five binding constraints BC1–BC5. v3 (`rfc-0143-design-v3`) **resolved BC2, BC3,
and BC4 at the design level** and carried the v2-credited set forward unregressed,
but returned **`needs_revision`** again: **BC1 stands open on three independent
material grounds** and **BC5 has two open precision items**, both falsifiers'
re-attacks landing unrebutted. Read the required context docs first: `SEED.md` (it
carries the charter, a pointer to the committed RFC
`docs/rfcs/0143-lane-credential-survival-across-boot-epoch-rotation.md`, the
**`## Ratified design shape`** you must not relitigate, the
**`## Carried forward — resolved by v3`** set you must preserve, and the
**`## The 2 binding constraints v4 MUST resolve`** section listing BC1's three
grounds and BC5's two items with their prescribed fixes and exact source sites);
the design-v3 spec you are revising,
`docs/operator/artifacts/rfc-0143-design-v3/dialogue/holder/HOLDER.md`; and the v3
verdict
`docs/operator/artifacts/rfc-0143-design-v3/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md`
(read its findings + rationale for the exact prescribed repairs).

**Start from the v3 `HOLDER.md` (a required context doc).** Your revised spec MUST
**resolve BC1 (all three grounds) and BC5 (both items) per their prescribed fix**,
and must **carry the v3-credited resolved set forward UNREGRESSED** — re-opening or
regressing any of it fails the gate. Do NOT relitigate the ratified OQ1 trust-model
shape (Option 4 mandatory floor + ratification-gated Option 2 narrow
`CapabilityReseal` + minimal Option 3 per-session endpoint+epoch republish) or the
F2 non-bearer decision — both pinned in `SEED.md`'s `## Ratified design shape`.

Author the **revised falsifiable implementation spec** as your published
`HOLDER.md` artifact. This is the claim the falsifiers will RE-ATTACK and the
adjudicator will gate — make it concrete and falsifiable, not a restatement of the
RFC or the v3 spec. State every load-bearing security claim as a falsifiable
assertion paired with its named test / game-day.

Hold the root reframe: **a boot-epoch rotation must never force a lane to choose
between reading the daemon's full-authority bootstrap admin `client-token` and
exiting silently unsealed.** A `striatum-lane` lane authenticates as its own narrow,
session-scoped credential and *never* as the shared operator admin override.

Your spec MUST:

1. **Resolve BC1 — the same-uid channel authentication + positive-intent source —
   in ONE place (the security cluster).** Fix all three unrebutted v3 grounds:
   - **(C1) Same-uid replay.** v3's inherited-fd control channel is still same-uid
     reachable — a sibling `striatum-lane` can open `/proc/<wrapper-pid>/fd/3` and
     read `/proc/<wrapper-pid>/environ` for the nonce, then send a valid frame for
     the victim supervisor (false provenance). The "no filesystem name" rationale
     repeats the same-uid category mistake that killed the v1 `0600` file. Pin
     same-uid AUTHENTICATION: a **peer-credential check against the launched
     wrapper's pid + start-time via `SO_PASSCRED` / `SCM_CREDENTIALS`** (verify the
     connecting peer IS the expected wrapper, not merely same-uid);
     **`PR_SET_DUMPABLE(0)` on the wrapper BEFORE the control fd and nonce are live**
     (so `/proc/<wrapper-pid>/fd` and `/proc/<wrapper-pid>/environ` are unreadable by
     same-uid siblings); and keep the **nonce out of the same-uid-readable env**.
     Name negative tests `TestSiblingSameUIDCannotOpenControlFDOrReplayNonceViaProc`
     and `TestControlFrameRequiresExpectedWrapperPeerCredentials`, run against a
     process that is **neither the provider child nor the launched wrapper**.
   - **(C2) Reserved exit codes.** Reserved agentloop codes 97/98 are not reserved
     until the wrapper is COMMITTED to never propagating a provider child's status
     into them (the helper records the wrapper exit code, `helper.go:427-439`).
     Commit the wrapper to **never forward provider statuses 97/98** into the
     reserved codes (remap to a non-control `agent_exited`); name
     `TestProviderExitCodeCannotDriveReservedAgentloopResealOrBlocker`.
   - **(Positive-intent gap).** Cutting the provider out of the channel leaves no
     specified non-PTY/non-bearer/non-sibling-replayable source of publish/complete
     INTENT (the wrapper does not know the deliverable is complete,
     `loop.go:220-368`). Pin the trusted positive source — EITHER **automatic/
     speculative reseal on a precise daemon-observed post-rotation condition** (child
     exit with all required `expected_artifact` paths present + modified in the
     active worktree since the packet; only daemon-derived artifacts attempted; ALL
     validation/backend failures mapped to the typed floor) **OR** a concrete
     provider→wrapper intent path satisfying the C1 peer-credential defense. Make
     `TestCodexResealUsesReceiverNotProviderStdout` include a **positive** case, and
     **define whether the post-exit reseal-complete path bypasses/replaces the
     `ensureWorkSessionBackend` gate** (the `agent_exited` branch stops the
     supervisor, `supervision.go:298-306`; `ensureWorkSessionBackend` runs in
     `lifecycle.go:1135-1183`) so it routes the typed
     `session_unrecoverable_across_rotation` class — name
     `TestResealExit98BypassesBackendGateOrRoutesTyped`.

2. **Resolve BC5 — the two lifecycle precision items (the lifecycle cluster).**
   - **Pin the migration site.** `leases.reseal_grace_extended_at` had no concrete
     location in v3. `striatumd.leases` is created in runtime migration `0005`
     (`go/pkg/db/sql/0005_repo_local_workflow_state.sql:166`) and is **owner-held**
     (not in the migration-0016+ ownership-transfer cohort,
     `go/pkg/db/sql/owner/0018_runtime_table_ownership_transfer.sql`), so the
     column-add is owner DDL — pin it to the **same owner bundle 0021** as
     `jobs.recovery_generation`, consistent with
     `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL`. Do not leave it a downstream
     decision.
   - **Correct the lock-order story.** v3's claim that the seal paths "already take
     `lockRunForJob` first" is INACCURATE for `work.complete` — its
     `enforceSessionBindingForSession` and `enforceActiveActingSession` gates run
     **before** `lockRunForJob` (`go/pkg/mutations/lifecycle.go:1135-1155`), with
     `activeLeaseFor` + `ensureWorkSessionBackend` after the lock (`:1178-1183`).
     Name exactly which pre-lock gates the internal `resealInFlightJob` path
     **skips or replays**, and state how `resealInFlightJob` serializes against
     `artifact.publish` / `work.complete` / the recovery sweep so an
     expired-beyond-grace reseal ALWAYS routes the typed
     `session_unrecoverable_across_rotation` class — never a raw `lease_error`,
     never a lease revived after the sweep requeued the job. Keep
     `TestResealBeyondGraceRoutesTypedNotLeaseError`,
     `TestResealGraceCannotReviveRequeuedLease`,
     `TestRecoveryRequeueWinsOverExpiredLeaseReseal`, and `GD-1b`.

3. **Carry the v3-credited resolved set forward UNREGRESSED** (verbatim where
   applicable; see `SEED.md` `## Carried forward — resolved by v3`): **BC2** (reseal
   artifact identity from the job's `expected_artifacts` daemon state, refusing
   unexpected paths), **BC3** (`CapabilityReseal` a daemon-internal marker projected
   by `resealInFlightJob`, public route-alternate test-only), **BC4** (the concrete
   `jobs.recovery_generation` column in owner bundle 0021, increment points, stamped
   value compared under the lock), **F2** (no lane-readable reseal bearer), **F4**
   (route-alternate records `reseal` not `write`), the **F7 file-mirror half**
   (daemon-owned lane-read-only `0644` mirror, `O_NOFOLLOW`, atomic rename, reject
   MISSING boot-epoch header — closing #316), **AF1** reachability-not-reminting,
   **AF4** epoch/token decoupling, the categorical **no-admin-token-widening
   invariant**, and the **per-claim falsifiable-assertion discipline (A1–A14)**.

4. **Hold the security invariant as the spine.** Per the carried-forward set:
   `admin.BootstrapRuntimeTokenIfNeeded` (`go/pkg/admin/bootstrap.go:18-27`) grants
   the runtime client-token the FULL `bootstrapCapabilities` set
   `{admin, read, write, claim, review, apply, recovery, surgical_recovery}`, `0600`
   in a `0700` dir. Any path that lets a lane read that file, or mints a
   lane-readable credential carrying ANY of `{admin, apply, recovery,
   surgical_recovery}`, is **categorically out of bounds** — say so explicitly and
   keep it structurally impossible. The no-replay property must hold **structurally**
   on the BC1 channel, not as a trackable post-clearance finding.

5. **Stay inside the product boundary and the Non-Goals.** Do NOT re-classify the
   downstream `agent_exited_unsealed` recovery policy (RFC 0152 / D249), do NOT
   change the committee POSIX-ACL repo provisioning (#537 / #539), and do NOT touch
   `run drive`'s transient-socket behavior (#513). Do NOT collide with the RFC 0125
   `HandleRecoveryReseal` worktree-durability verb (separate file, separate verb).
   Local-first, single-host, daemon-owned PostgreSQL as the single writer.

6. **Flag the maintainer ratification gate.** Slice B (the daemon-internal
   `rpc.CapabilityReseal` marker, the test-only auth-prelude route alternate, the
   inherited-fd supervisor control channel, the reserved agentloop exit codes, the
   `jobs.recovery_generation` owner-bundle column, and endpoint/epoch republish
   plumbing) is a security/authz trust-model change. State plainly that the cleared
   spec is a RECOMMENDATION the maintainer ratifies before any build slice lands
   credential code, and that Slice A (the Option-4 floor) is zero-trust-change but
   must route over a real non-PTY channel with the same-uid authentication fixed
   before it lands.

Do not treat falsifier completion as acceptance — the adjudicator's collaboration
ledger decides whether the gate clears.
