# RFC 0158: Verifier self-pin / pin-drift doctor classes and version-skew resweep

Status: proposed
Date: 2026-06-20
author: proposer-claude-opus-4-8

## Summary

RFC 0141 (`verification_gate` shape) shipped with a deliberately scoped-out set of
follow-on operability ideas tracked in GH #483. They turn a verification-gate
failure that today only surfaces at run time (or, worse, as a silent
`VERIFIED`→`ASSERTED` downgrade) into a **pre-flight** the operator sees before a
run, and turn a Striatum version bump from a mass-degradation event into a
**bounded re-verification sweep**. This RFC frames the design decision for those
classes. It does **not** land code: the changes touch verification/attestation
semantics (security/authz) and the meaning of a `VERIFIED` product-safety claim
across a version bump, so they need a recorded decision before implementation.

The four child-ideas from #483:

1. **`builtin_selfpin_drift` doctor check** — recompute the verifier self-pin
   (`verifier.selfSHA()`, the sha256 of the running `striatum` executable) and
   compare it against the build that authored existing builtin receipts. Catches
   the known `make install`-without-restart gotcha (the on-disk binary advances
   while the running daemon keeps the old in-memory image; see
   `project_make_install_no_restart`) that would otherwise make builtin receipts
   mysteriously version-skew-downgrade.
2. **`verifier_pin_drifted` / `verifier_check_unpinned` doctor classes** — a
   pre-flight that reports "this run would degrade N `VERIFIED` claims on this
   host" (drift between the committed `intent` argv and the recorded per-host
   `pins` sha, or a sanctioned external check with no pin at all), surfaced by
   `striatum doctor` rather than only at run start. Pairs with a
   `verifier pin --diff-only` (read-only) mode.
3. **`verified_stale` rung + `verifier resweep --builtins`** — treat a Striatum
   version bump as a first-class staleness rung and turn every upgrade into a
   bounded re-verification sweep, instead of a mass `VERIFIED`→`ASSERTED` failure
   at the next run-completion gate read.
4. **Generalize the intent/pins split** — "commit the policy, observe the bytes
   per host" (RFC 0141 Domain Modeling §Boundary clarification) is reusable for
   other host-specific content-addressed resources. This is the lowest-priority
   idea and the most speculative; it is recorded here for completeness, not slated.

## Affected issue

- **GH #483** — "RFC 0141 child-ideas: builtin self-pin + pin-drift doctor classes
  + version-skew resweep" (labels: enhancement, ready-for-agent).

## Context and current evidence (origin/main @ d5d3cd86)

- **The verifier self-pin already exists and is honest.**
  `go/pkg/verifier/builtin.go::selfSHA()` hashes the running `striatum`
  executable; `builtinResolvedExec` seals `BuiltinID` + `BuiltinStriatumVersion`
  + that self-pin as `BinarySHA256` into the `receipt.v1`. The seal proves *which
  striatum build invoked the tool*, which is exactly why a builtin is capped at
  `ASSERTED` at the daemon gate read
  (`EffectiveStatusFromReceipt` returns `ASSERTED` for any `builtin_id` receipt).
  So the raw material for a self-pin-drift check (a recorded build identity vs the
  current `selfSHA()`) is already in the receipt body — but nothing reads it
  back to *warn*.
- **The forge-able-attestation graduation blocker is NOT this issue and is already
  closed.** GH #482 (a non-compliant lane forging the repo-file sidecar to reach
  `VERIFIED`) was fixed on origin/main by commit `5a5b74e5` / PR #502 (D243):
  gate-side, daemon-authoritative attestation enforcement, fail-closed
  (`verifier_attestations` PG store via migration 0041 + the operator-token
  `verifier.attest` RPC that refuses session-bound tokens; the run-completion gate
  refuses `VERIFIED` for an external claim lacking an un-revoked attestation row).
  This RFC must **not** re-litigate that boundary; it builds *operability* on top
  of it.
- **No doctor class reads verifier state today.** `go/pkg/reads/doctor.go`
  assembles its problem set from schema-meta, supervisor liveness, lane-sandbox,
  PG write-boundary, worktree-ref safety, artifact-anchor integrity, barrier,
  quorum, recovery-sweep-cursor, recovery-gate, stuck-job, and event-chain-segment
  blocks. None consult `verifier.selfSHA()`, the `pins` layer, or the
  `verifier_attestations` rows. So all four ideas are net-new doctor/CLI surface,
  not a narrowing of existing behavior.
- **RFC 0141 records these as the explicit remaining follow-ups** ("What the
  supported tier does NOT yet include … the doctor self-pin / pin-drift classes
  (#483)").

## Claim boundaries (what is and is NOT in scope)

- IN scope of the eventual implementation: read-only `doctor` classes, a
  read-only `verifier pin --diff-only`, a `verifier resweep` that *re-runs* the
  verifier lane (it does not bless anything), and a `verified_stale` staleness
  rung surfaced at the gate read.
- OUT of scope, hard line (preserve D227 and the D243 trust boundary): the daemon
  still executes no check; `resweep` runs in the disposable verifier LANE, never
  the daemon. A doctor class or `resweep` must NOT mint, auto-attest, or upgrade a
  claim's status — only an operator-token `verifier.attest` (RFC 0141 / D243) can
  promote PINNED→VERIFIED. A version-bump staleness rung must DEGRADE
  conservatively (fail-closed), never auto-renew a `VERIFIED` claim across a bump.

## Why a direct FIX was rejected

The routing scorecard for #483 has multiple HOT blast-radius dimensions:

- **security_or_authz / product_safety_claim (both HOT).** A `verified_stale` rung
  changes what `VERIFIED` *means* across a Striatum version bump — it is a
  modification to a product-safety claim contract, not a bug narrowing. The
  "this run would degrade N VERIFIED claims" pre-flight likewise reasons over the
  attestation/verification trust boundary that D243 just made daemon-authoritative.
- **No failing proof is obtainable.** These are net-new operability surfaces (new
  doctor classes, a new sweep verb, a new staleness rung). There is no existing
  invariant being violated to capture as a red regression test, so the FIX-route
  precondition ("capture a PRE-FIX failing proof tied to the issue") cannot be
  met without first *designing* the contract — which is the RFC's job.
- **Contract expansion.** New doctor problem classes are an operator/tooling
  contract (they appear in `striatum doctor` output and can flip global `ok`), and
  `verified_stale` adds a new staleness basis the gate read must agree on. Both
  expand contracts rather than strictly narrowing behavior.

Per the routing engine, any hot dimension forces RFC unless the change strictly
narrows behavior to a cited accepted invariant with a failing proof and no
contract expansion. None of those escape conditions hold here, and the operator
steer for #483 explicitly directs: *do not implement verifier policy; draft an RFC
stub.*

## Hot blast-radius dimensions that forced RFC

- `security_or_authz` — reasons over the attestation/verification trust boundary.
- `product_safety_claim` — redefines `VERIFIED` staleness across a version bump.

(Not hot for the proposal itself: no public API, persisted schema, migration, or
wire-format change ships with this RFC. A `verified_stale` rung *may* later want a
persisted staleness basis; that is a decision the eventual implementation slice
must surface, not something this stub commits.)

## Alternatives / rejected direct patches

- **Just add a `builtin_selfpin_drift` warning and call it done.** Rejected as the
  whole answer: it covers only idea (1) and leaves the version-skew resweep — the
  idea that prevents a mass `VERIFIED`→`ASSERTED` event — unaddressed. It is,
  however, a viable *first* implementation slice once this RFC is accepted (it is
  read-only, pairs cleanly with the existing self-pin, and expands the least
  contract). The eventual to-issues split should sequence it first.
- **Auto-resweep on daemon restart.** Rejected: silently re-blessing claims across
  a version bump is exactly the trust regression D243 closed. Any resweep must
  re-run the verifier lane and leave promotion to the operator-token road.
- **Fold all four ideas into one mega-issue and implement directly.** Rejected:
  ideas (1)/(2) are read-only doctor/CLI surface (low risk, near-FIX), while (3)
  changes claim semantics (needs a decision) and (4) is speculative. They belong
  as discrete child issues with different risk tiers, not one patch.

## Proposed child-issue split (handoff)

On acceptance, split #483 into discrete child issues (e.g. via `/to-issues`),
sequenced by risk:

1. `builtin_selfpin_drift` doctor warning (read-only; pairs with existing
   `selfSHA()`). Lowest risk — candidate first slice.
2. `verifier_pin_drifted` / `verifier_check_unpinned` doctor classes +
   `verifier pin --diff-only` (read-only pre-flight). Low risk.
3. `verified_stale` rung + `verifier resweep --builtins` (claim-semantics change —
   carries the security/product-safety weight; must preserve D227 + D243).
4. Generalize the intent/pins split for other host-local content-addressed
   resources (speculative; lowest priority — may be closed as `wontfix` if no
   second consumer materializes).

## Handoff to RFC review

This is a stub for `RFC_REVIEW.md` discussion. It does **not** implement the
policy, contract, doctor classes, CLI verbs, or any migration. On acceptance:
record the decision in `docs/decisions/decision-log.md`, update the
`docs/rfcs/README.md` index status, and slice the implementation per the
child-issue split above (smallest read-only slice first). Until then #483 stays
open and tracks this RFC.
