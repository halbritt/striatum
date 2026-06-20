# RFC 0158: Verifier self-pin / pin-drift doctor classes and version-skew resweep

Status: proposed (revised 2026-06-20; idea-3 premise corrected; ratification pending)
Date: 2026-06-20
author: proposer-claude-opus-4-8

## Summary

RFC 0141 (`verification_gate` shape) shipped with a deliberately scoped-out set of
follow-on operability ideas tracked in GH #483. They turn a verification-gate
drift that today only surfaces at run time — or, for builtins, as a silent
*receipt re-seal under a new build identity* (the recorded build no longer matches
the running daemon) — into a **pre-flight** the operator sees before a run; and
they make a Striatum version bump that re-seals builtin receipts a **bounded,
legible re-verification sweep** rather than a silent build-identity drift. This
RFC frames the design decision for those classes. It does **not** land code: the
changes touch verification/attestation semantics (security/authz) and the meaning
of a `VERIFIED` product-safety claim across a version bump, so they need a
recorded decision before implementation.

> **Premise correction (RFC_REVIEW 2026-06-20, SERIOUS-1).** An earlier draft
> motivated idea 3 by a present-tense *mass `VERIFIED`→`ASSERTED` event on every
> version bump*. That premise is **inverted vs the gate code** and has been
> corrected throughout. `StriatumVersion` is sealed into a receipt **only** for
> builtin checks (`receipt.go:39-45`, `builtin.go:167`); for external checks — the
> *only* `VERIFIED`-capable receipts — it is empty/unsealed, and the gate's decay
> (`evaluate.go`) keys on `evidence_digest` vs `bound_input_digest`, never on a
> striatum version. Builtins are already capped at `ASSERTED` at the gate read
> (`evaluate.go:158`), so they cannot "downgrade" from `VERIFIED`. A version bump
> therefore does **not** mass-degrade external `VERIFIED` claims today. The real
> skew idea 3 (and idea 1) catch is **builtin self-pin / build-identity drift**:
> a version bump changes `selfSHA()`, so builtin receipts silently re-seal under a
> new recorded build identity, and `make install`-without-restart makes the
> on-disk and in-memory builds diverge. See SERIOUS-1 in the Findings reconciliation
> below; idea 3 (`verified_stale`) is re-justified on that real skew and gated on
> an explicit persisted-staleness-basis decision (SERIOUS-2).

The four child-ideas from #483:

1. **`builtin_selfpin_drift` doctor check** — recompute the verifier self-pin
   (`verifier.selfSHA()`, the sha256 of the running `striatum` executable) and
   compare it against the build that authored existing builtin receipts. Catches
   the known `make install`-without-restart gotcha (the on-disk binary advances
   while the running daemon keeps the old in-memory image; see
   `project_make_install_no_restart`) that would otherwise make builtin receipts
   silently **re-seal under a new build identity** — the recorded build no longer
   matches the running daemon — with no operator-visible warning. (Builtins are
   capped at `ASSERTED` and never reach `VERIFIED`, so this is a *build-identity /
   legibility* drift, not a status downgrade; see MINOR-2.)
2. **`verifier_pin_drifted` / `verifier_check_unpinned` doctor classes** — a
   pre-flight that reports "this run would degrade N `VERIFIED` claims on this
   host" (drift between the committed `intent` argv and the recorded per-host
   `pins` sha, or a sanctioned external check with no pin at all), surfaced by
   `striatum doctor` rather than only at run start. Pairs with a
   `verifier pin --diff-only` (read-only) mode.
3. **`verified_stale` rung + `verifier resweep --builtins`** — treat a Striatum
   version bump as a first-class staleness rung for the receipts whose recorded
   build identity actually moves (the builtins — their sealed `StriatumVersion` /
   `selfSHA()` drifts on a bump), and turn the upgrade into a **bounded, legible
   re-verification sweep** instead of leaving those receipts silently re-sealed
   under a new build identity (idea 1's drift, made actionable). This is the
   highest-risk slice: it redefines what `VERIFIED`/`ASSERTED` *means* across a
   version bump and, to fire at all, requires a recorded/persisted staleness basis
   the gate read can compare against — a sub-decision (SERIOUS-2) that MUST be made
   before any implementation. It is **not** a present-tense fix for a mass external
   `VERIFIED`→`ASSERTED` event (that event does not occur — see the Premise
   correction above); it guards the prospective contract in which a striatum-version
   field is folded into claim decay.
4. **Generalize the intent/pins split** — "commit the policy, observe the bytes
   per host" (RFC 0141 Domain Modeling §Boundary clarification) is reusable for
   other host-specific content-addressed resources. This is the lowest-priority
   idea and the most speculative; it is recorded here for completeness, not slated.

## Affected issue

- **GH #483** — "RFC 0141 child-ideas: builtin self-pin + pin-drift doctor classes
  + version-skew resweep" (labels: enhancement, ready-for-agent).

## Context and current evidence (origin/main @ 9e1b6475)

> Anchor refreshed from `d5d3cd86` to `9e1b6475` (MINOR-1). `9e1b6475`
> (`fix(verifier): builtin go-* checks verify a nested Go module + surface stderr
> (#515) (#528)`) landed since the original draft. #528 left `selfSHA()` and
> `builtin.go` **untouched** — the self-pin / `builtin_selfpin_drift` premise
> (idea 1) is unaffected — but it added `goModuleDir` discovery and now seals
> `working_subdir` into the receipt (`receipt.go:691`, surfaced in
> `evaluate.go`). `working_subdir` is non-empty only for a go-* builtin whose Go
> module lives in a subdirectory (e.g. striatum's own `go/`), so it is part of the
> builtin build-identity surface a self-pin-drift / `verified_stale` design must
> account for.

- **The verifier self-pin already exists and is honest.**
  `go/pkg/verifier/builtin.go::selfSHA()` hashes the running `striatum`
  executable; `builtinResolvedExec` seals `BuiltinID` + `BuiltinStriatumVersion`
  + that self-pin as `BinarySHA256` into the `receipt.v1` (now alongside
  `working_subdir`, per #528). The seal proves *which striatum build invoked the
  tool*, which is exactly why a builtin is capped at `ASSERTED` at the daemon gate
  read (`EffectiveStatusFromReceipt` returns `ASSERTED` for any `builtin_id`
  receipt — `evaluate.go:158`). Crucially, `StriatumVersion` is sealed **only**
  for builtin receipts (`receipt.go:39-45`, `builtin.go:167`); an *external*
  check's receipt seals no striatum version, and the gate's decay keys on
  `evidence_digest` vs `bound_input_digest`, never on a striatum version. So the
  raw material for a self-pin-drift check (a recorded build identity vs the current
  `selfSHA()`) is already in the *builtin* receipt body — but nothing reads it back
  to *warn*, and there is no version-keyed decay path for external claims at all.
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
wire-format change ships with this RFC.)

**Slice-3 gating sub-decision — persisted staleness basis (SERIOUS-2).** A
`verified_stale` rung that actually fires requires the gate read to compare a
*recorded* striatum-build identity against the running one. Today only builtin
receipts seal `StriatumVersion`/`selfSHA()`, and the gate's decay is
`evidence_digest`-only — so a real `verified_stale` for external claims would need
a **new sealed/persisted version field** on the external-receipt path or the claim
ledger (a seal-format/schema change), and for builtins it needs a place to record
the prior `selfSHA()` to diff against. This is a seal-contract change that D227 and
D243 lean on, so it is itself security/product-safety-weighted. Slice 3 MUST decide
this basis **before** any `verified_stale` rung is implemented; it cannot be left as
"the slice will figure it out" (a pure read with no persisted basis would either
re-open the seal/schema decision mid-implementation or ship a rung that is silently
inert). If that decision adds a persisted basis, its migration MUST fetch-and-check
the live max migration number first — runtime migration **0041** is already taken by
`verifier_attestations` (D243) and collided once with RFC 0136/#387's
`event_chain_segment` (MINOR-3), so a fresh, conflict-checked number is mandatory.

## Alternatives / rejected direct patches

- **Just add a `builtin_selfpin_drift` warning and call it done.** Rejected as the
  whole answer: it covers only idea (1) and leaves the version-skew resweep — the
  idea that turns a silent builtin re-seal-under-new-build-identity into a bounded,
  legible re-verification sweep — unaddressed. It is, however, a viable *first*
  implementation slice once this RFC is accepted (it is read-only, pairs cleanly
  with the existing self-pin, and expands the least contract), and it delivers the
  real, verifiable win (surfacing build-identity drift) independently of the
  contested `verified_stale` rung. The eventual to-issues split should sequence it
  first.
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
   **Gated**: this slice may be filed only with (a) its motivation re-stated on the
   real skew — builtin self-pin / build-identity re-seal on a version bump, NOT a
   present-tense mass external `VERIFIED`→`ASSERTED` event, which does not occur
   (see the Premise correction) — and (b) the persisted-staleness-basis sub-decision
   (SERIOUS-2) recorded first, including the seal-format/migration choice and a
   conflict-checked migration number. A `verified_stale` rung that cannot name its
   persisted basis is silently inert and must not be implemented.
4. Generalize the intent/pins split for other host-local content-addressed
   resources (speculative; lowest priority — may be closed as `wontfix` if no
   second consumer materializes).

## RFC_REVIEW reconciliation (2026-06-20)

The 2026-06-20 RFC_REVIEW (run `2026-06-20_9e1b6475_claude-opus-4-8`) returned
**ACCEPT_WITH_FINDINGS** (0 blockers, 2 serious, 3 minor). This revision resolves
them in-place; the RFC stays `proposed` (ratification pending). Slices 1 and 2
(read-only `builtin_selfpin_drift` warning; pin-drift doctor classes) are unchanged
and ready; slice 3 (`verified_stale` + resweep) is now explicitly **gated** on the
corrected premise plus the persisted-staleness-basis decision.

- **SERIOUS-1 (idea-3 premise inverted).** Resolved. The "mass `VERIFIED`→`ASSERTED`
  on every version bump" framing is removed; idea 3 is re-justified on the real skew
  — builtin self-pin / build-identity re-seal on a version bump (`StriatumVersion`
  sealed only for builtins, external checks unsealed, builtins already capped at
  `ASSERTED`). See the Premise correction (Summary) and the re-stated idea 3.
- **SERIOUS-2 (persisted staleness basis under-specified).** Resolved. The
  Hot-blast-radius section now names the persisted/sealed striatum-version basis as
  a **gating sub-decision for slice 3**, security/product-safety-weighted, that must
  be settled before any `verified_stale` rung — not deferred to the implementer.
- **MINOR-1 (stale anchor `d5d3cd86`; seal moved under it).** Resolved. Anchor
  refreshed to `9e1b6475`; `working_subdir` is now noted as seal-covered (#528),
  and #528 is reconciled as leaving `selfSHA()`/`builtin.go` untouched.
- **MINOR-2 ("mysteriously version-skew-downgrade" imprecise).** Resolved. Idea 1's
  symptom is restated as a *receipt re-seal under a new build identity* (legibility
  drift), not a status downgrade — builtins never reach `VERIFIED` to downgrade from.
- **MINOR-3 (migration-number collision risk unflagged).** Resolved. The slice-3
  gating note records that any persisted-basis migration must fetch-and-check the
  live max migration number (0041 already taken by `verifier_attestations`/D243 and
  previously collided with RFC 0136/#387).

In-force decisions are preserved: **D227** (the daemon validates, never executes —
`resweep` runs in the disposable verifier lane) and **D243/#482** (daemon-authoritative
attestation; only an operator-token `verifier.attest` promotes a claim) are unchanged
and not re-litigated.

## Handoff to RFC review

This is a stub for `RFC_REVIEW.md` discussion. It does **not** implement the
policy, contract, doctor classes, CLI verbs, or any migration. On acceptance:
record the decision in `docs/decisions/decision-log.md`, update the
`docs/rfcs/README.md` index status, and slice the implementation per the
child-issue split above (smallest read-only slice first). Until then #483 stays
open and tracks this RFC.
