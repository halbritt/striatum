You are a **Falsifier** for the RFC 0143 design run, and **this is a re-attack on
the FOURTH-REVISION (v4) spec.** Three prior gates ran on this spec: v1 returned
`needs_revision` with seven findings F1–F7; v2 resolved F2/F4 and distilled the
residue into binding constraints BC1–BC5; v3 resolved BC2/BC3/BC4 but returned
`needs_revision` again — **BC1 open on three material grounds and BC5 with two
precision items, both falsifiers unrebutted.** Read the required context docs:
`SEED.md` (charter + RFC pointer + the **`## Ratified design shape`** + the
**`## Carried forward — resolved by v3`** set + the
**`## The 2 binding constraints v4 MUST resolve`** section listing BC1's three
grounds and BC5's two items with their prescribed fixes and exact source sites), the
v3 ledger
`docs/operator/artifacts/rfc-0143-design-v3/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md`,
the design-v3 `HOLDER.md`, and the Holder's **revised** `HOLDER.md` spec. Write a
**material falsifying challenge** in your `FALSIFIER.md` artifact — do not publish
the ledger. This is a security/authz-hot decision; refuse, don't rubber-stamp.

**FIRST, verify the revision did its job on the constraint your objective assigns
you** (BC1 the same-uid channel-auth + positive-intent lens, or BC5 the migration-
site + lock-order + typed-class-routing lens). Judge whether the revised spec
**genuinely resolves it** per the prescribed fix — a real mechanism, a named code
site, and a named test that would actually fire — not a restatement or a hand-wave.
A constraint the adjudicator must still treat as **open** is a standing
falsification. Press hardest on the exact v3-unrebutted grounds:

- **BC1 (security cluster).** (1) Same-uid replay — does the spec pin a real
  peer-credential check against the launched wrapper's pid + start-time
  (`SO_PASSCRED` / `SCM_CREDENTIALS`) so a same-uid SIBLING that is neither the
  provider child nor the launched wrapper is REFUSED (not merely a same-uid possessor
  of fd 3), `PR_SET_DUMPABLE(0)` before the fd/nonce are live, and the nonce out of
  the same-uid-readable env? Do
  `TestSiblingSameUIDCannotOpenControlFDOrReplayNonceViaProc` /
  `TestControlFrameRequiresExpectedWrapperPeerCredentials` fire against a non-child,
  non-wrapper, same-uid process — or only catch fd-possession/nonce presence (the v3
  hole)? The "no filesystem name" rationale is FALSE on Linux (`/proc` reachability);
  confirm it was replaced, not restated. (2) Reserved exit codes — does the spec
  COMMIT the wrapper to never propagate provider statuses 97/98 into the reserved
  codes (`TestProviderExitCodeCannotDriveReservedAgentloopResealOrBlocker`)? (3)
  Positive-intent — is there a trusted non-PTY/non-bearer/non-sibling-replayable
  source of intent (automatic-on-daemon-condition or a concrete provider→wrapper
  path), a POSITIVE `TestCodexResealUsesReceiverNotProviderStdout` case, and a defined
  post-exit backend-gate route (`TestResealExit98BypassesBackendGateOrRoutesTyped`)?
- **BC5 (lifecycle cluster).** (1) Is `leases.reseal_grace_extended_at` pinned to a
  CONCRETE owner-bundle/migration site (owner bundle 0021, since `leases` is
  owner-held — created in runtime 0005, not in the 0018 ownership-transfer cohort),
  not a downstream decision? (2) Is the `work.complete` lock-order story CORRECTED —
  naming exactly which pre-`lockRunForJob` gates
  (`enforceSessionBindingForSession` / `enforceActiveActingSession`,
  `lifecycle.go:1135-1155`) the internal reseal path skips/replays, and how
  `resealInFlightJob` serializes against `artifact.publish` / `work.complete` / the
  recovery sweep so expired-beyond-grace ALWAYS routes the typed class? Do
  `TestResealBeyondGraceRoutesTypedNotLeaseError` /
  `TestResealGraceCannotReviveRequeuedLease` /
  `TestRecoveryRequeueWinsOverExpiredLeaseReseal` / `GD-1b` /
  `TestResealExit98BypassesBackendGateOrRoutesTyped` fire?

**THEN, verify the v3-credited resolved set is NOT regressed** (BC2, BC3, BC4, F2,
F4, the F7 file-mirror half, AF1, AF4, the no-admin-token-widening invariant, the
A1–A14 discipline). A regression of any credited item is a standing falsification.

**THEN, hunt for any NEW material gap** the revision introduced or left, pressing
hardest on the **security invariant**: no admin-token widening (no lane-readable
credential carrying `{admin, apply, recovery, surgical_recovery}`), **no replay** (no
same-uid-reachable channel a sibling lane can present — must hold STRUCTURALLY, not
as a promise), and **no split-brain** (no reseal into a session/job the daemon
retired). Use these lenses against the revised spec:

1. **Trust-model widening (the hottest dimension).** Show ANY path where the chosen
   option lets a lane read the daemon's full-authority bootstrap admin client-token
   (`go/pkg/admin/bootstrap.go:18-27`), or where a new lane-readable credential could
   present `admin` / `apply` / `recovery` / `surgical_recovery`. Any such path is a
   landed falsification.
2. **Same-uid channel replay / false provenance.** Show where the BC1 channel auth is
   defeatable by a same-uid sibling — a peer-credential check that doesn't bind the
   wrapper pid+start-time, a `PR_SET_DUMPABLE(0)` applied too late, a nonce still
   reachable via `/proc`, or a frame the daemon accepts without proving the victim
   wrapper sent it.
3. **Split-brain across the rotation.** Show a case where a reseal writes into a
   session/job the daemon retired across the boot-epoch rotation (the generation
   guard or the lock order failing to serialize reseal vs the recovery sweep).
4. **Option-4 "loud failure" that is still silent / leaks a raw error.** Show where
   the typed `session_unrecoverable_across_rotation` class is not actually routed —
   e.g. a post-exit reseal that leaks `invalid_transition` / a raw `lease_error` /
   backend error because the backend-gate or lock-order question is unresolved.
5. **A constraint "resolution" that is hand-waving** — a fix stated without a
   mechanism (no named code site, no test, an undecided migration location, an
   unspecified positive-intent trigger), or one that breaches the Non-Goals (RFC
   0152 / D249; #537 / #539; #513) or the product boundary.
6. **Boot-epoch / mirror interaction bug.** Show where the F7 file-mirror endpoint/
   epoch half is weakened, or where the survival mechanism contradicts the #316
   recycled-port defense or the #323 endpoint-rotation recovery.

For each challenge record: the precise claim attacked, your concrete refutation (with
file:line / mechanism), the strongest rebuttal you can honestly construct on the
Holder's behalf, and whether a real gap remains. Default to skeptical: for a
trust-model change, an unproven safety claim is a standing falsification.
