You are the **Committer** for the RFC 0143 design run. The adjudicator's
collaboration ledger has cleared the gate. Publish the final, falsification-
hardened **implementation spec** as your `PROPOSAL.md` artifact — this is the design
run's primary deliverable, the spec the `rfc-0143-build` run will build
contract-first.

Start from the Holder's `HOLDER.md` and fold in every challenge the adjudicator
recorded as material-and-incorporated. The committed spec MUST:

- **Carry the BC1 resolution (the security cluster) in full:** the same-uid channel
  AUTHENTICATION — a peer-credential check against the launched wrapper's pid +
  start-time (`SO_PASSCRED` / `SCM_CREDENTIALS`), `PR_SET_DUMPABLE(0)` on the wrapper
  before the control fd/nonce are live, and the nonce out of the same-uid-readable
  env (with `TestSiblingSameUIDCannotOpenControlFDOrReplayNonceViaProc` /
  `TestControlFrameRequiresExpectedWrapperPeerCredentials` run against a non-child,
  non-wrapper, same-uid process); the COMMITMENT that the wrapper never propagates
  provider statuses 97/98 into the reserved agentloop exit codes
  (`TestProviderExitCodeCannotDriveReservedAgentloopResealOrBlocker`); and the pinned
  trusted POSITIVE-intent source — automatic/speculative reseal on a precise
  daemon-observed post-rotation condition (all validation/backend failures mapped to
  the typed floor) OR a concrete provider→wrapper intent path — with a POSITIVE
  `TestCodexResealUsesReceiverNotProviderStdout` case and a defined post-exit
  backend-gate route (`TestResealExit98BypassesBackendGateOrRoutesTyped`).
- **Carry the BC5 resolution (the lifecycle cluster) in full:** the pinned migration
  site for `leases.reseal_grace_extended_at` (owner bundle 0021, since `leases` is
  owner-held), and the corrected `work.complete` lock-order story — exactly which
  pre-`lockRunForJob` gates the internal `resealInFlightJob` path skips/replays, and
  how it serializes against `artifact.publish` / `work.complete` / the recovery sweep
  so expired-beyond-grace always routes the typed
  `session_unrecoverable_across_rotation` class (with
  `TestResealBeyondGraceRoutesTypedNotLeaseError` /
  `TestResealGraceCannotReviveRequeuedLease` /
  `TestRecoveryRequeueWinsOverExpiredLeaseReseal` / `GD-1b`).
- **Carry the v3-credited resolved set forward unregressed:** BC2 (artifact identity
  from daemon `expected_artifacts` state), BC3 (`CapabilityReseal` a daemon-internal
  marker projected by `resealInFlightJob`, public route-alternate test-only), BC4 (the
  concrete `jobs.recovery_generation` column in owner bundle 0021, increment points,
  stamped value), F2 (no lane-readable reseal bearer), F4 (route-alternate records
  `reseal` not `write`), the F7 file-mirror half, AF1, AF4, and the A1–A14 discipline.
- **Carry the security invariant explicitly:** the new credential never carries
  `{admin, apply, recovery, surgical_recovery}`; no lane ever reads the bootstrap
  admin client-token; `CapabilityReseal` is never materialized into any lane-readable
  file; the no-replay property holds structurally on the BC1 channel. State each as a
  falsifiable assertion + the named test that proves it.
- **Specify the build slices in contract-first order** (smallest safe first — Slice A
  the Option-4 legible-failure floor over the same-uid-authenticated channel, then
  Slice B the reseal mechanism), each with its named Go tests and the
  migration/owner-bundle changes (owner bundle 0021 for `jobs.recovery_generation`
  and `leases.reseal_grace_extended_at`; `LatestOwnerBundleVersion` 20→21). Apply the
  shadow-first convention for the risky new credential/boot path: new behavior
  defaults OFF behind an env flag; additive migrations only; self-record before
  enforce.
- **State the explicit Acceptance Criteria** an impl-run + verify-run must meet,
  including the mandatory **game-day fire test** (`GD-1` / `GD-1b`: restart the daemon
  mid-job and show the lane survives-and-reseals OR fails legibly-and-is-routed, with
  no silent unsealed exit and no elevated-capability exposure) and the same-uid
  sibling-replay negative game-day.
- **Open with the maintainer-ratification banner:** Slice B is a security/authz
  trust-model change; the spec is a RECOMMENDATION the maintainer ratifies before the
  build lands credential code. State the recommended shape and the one-line security
  rationale up front; note Slice A is zero-trust-change but must route over a real
  non-PTY channel with the same-uid authentication fixed before it lands.
- Stay strictly inside the Non-Goals and the local-first product boundary.

Publish the spec only after confirming the ledger verdict cleared the gate.
