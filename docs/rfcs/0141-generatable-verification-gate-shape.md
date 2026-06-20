# RFC 0141: Generatable verification_gate workflow shape

Status: implemented (D239)
Date: 2026-06-19
author: proposer-claude-opus-4-8

## Summary

RFC 0134 / D227 shipped the executable verification gate (the daemon NEVER
executes a check; a disposable sandboxed verifier LANE runs `striatum verifier
run` against an operator-curated, content-addressed allowlist, mints a
tamper-evident `receipt.v1`, and publishes a `claim_ledger` whose VERIFIED claims
bind to the receipt seal; the run-completion gate is a pure read that degrades a
missing/wedged verify to ASSERTED and never blocks). It is `implemented` (D237).
But the only realization is one **hand-authored** example
([`examples/verification-gate-flow/`](../../examples/verification-gate-flow/), on
today-primitives); there is **no generatable shape** — `striatum workflow generate
--shape <name>` has no `verification_gate` (GH #473).

The obstacle that kept this out of D237 is structural: **a runnable allowlist
cannot be a committed fixture, because `binary_sha256` content-addresses
host/distro-specific bytes by design.** This RFC proposes a `verification_gate`
shape that resolves that tension instead of papering over it, on three pillars:

1. **Split the allowlist** into a committed, hashless, reviewable **`intent`**
   layer and a gitignored, per-host **`pins`** layer the operator never types by
   hand — the sandbox observes the hash; the operator attests it with a token the
   verified lane cannot impersonate.
2. **A built-in, Striatum-pinned check library** (`builtin:go-test`, …) so the
   generated shape is **runnable out of the box** for the common case, capped
   honestly at ASSERTED (it attests *who invoked the tool*, not *which tool ran*).
3. **The gate cannot lie green** — an unfilled template or a vacuous check reads
   RED, never a false green, enforced before and during the run.

Proposed at the `experimental` tier (no RFC 0105 unattended-reliability fixture
yet). No code lands with the proposal.

## Context

- **GH #473** filed at D237 graduation: the `verify` job type, the verifier lane,
  owner bundle 0016, and the pure-read completion gate are live and proven, but
  authoring a verification workflow is a fully manual exercise — there is no
  starter shape, and the one example uses today-primitives (a `build` "verifier"
  job, not a real `type: verify` job).
- **The shipped primitives this shape composes** (do not re-design them):
  - `go/pkg/verifier/allowlist.go` — `AllowlistFile` / `AllowlistEntry` (required
    `binary_sha256`, `schema_version` marker `striatum.verifier_allowlist.v1`,
    fail-closed `LoadAllowlist`/`ParseAllowlist`); `Resolve` rejects an unlisted
    id before touching a binary; `VerifyBinary` pins `argv[0]` content.
  - `go/pkg/verifier/receipt.go` — `computeSeal` already covers `BinarySHA256`,
    argv, exit code, stdout digest, cwd tree-sha, posture, agreement.
  - `go/pkg/mutations/claim_verifier_gate.go` — the pure-read
    `evaluateRunClaimVerification` the completion gate calls; degrade bases are
    already legible (`no_receipt_ref`, `receipt_unavailable`,
    `receipt_seal_mismatch`, `sandbox_not_strict`, `no_reexecution_agreement`, …).
  - `go/pkg/workflowgenerate/` — the shape registry (`shapes[]` map +
    `SupportedShapes()`), with a catalog-reconcile test against
    `workflowtemplates/catalog.json` and the generated
    `docs/reference/workflow-catalog.md`.
- **Adjacent decisions:** D215 (job-type CHECK is owner-held — `verify` is owner
  bundle 0016, unchanged here); D227 (validate-not-execute — the cardinal rule
  this RFC must never violate); D234 precedent (a new shape may ship at
  `experimental` before it has an unattended-reliability fixture).

## Problem

A first-class feature whose entire purpose is rigor is the single hardest
workflow to wire up. To use the verification gate today an operator hand-authors:
a `workflow.json` with a real `type: verify` job and a downstream
`claim_ledger`-reading gate; an allowlist JSON pinning exact binary hashes; and
role/prompt scaffolding teaching the lane to mint a receipt and bind its claims.
There is no `generate` shortcut and no safe template.

A naive generated shape makes the friction *worse*, because the obvious template
forms are footguns:

- A committed runnable allowlist is **non-portable** (the pinned hash is
  host-specific) — it works only on the author's machine.
- A template with a blank/placeholder hash that the verifier treats permissively
  **silently verifies nothing** and reports green — the exact "docs outran code"
  failure RFC 0134 exists to forbid, reproduced one level up.
- A copy-paste example invites pinning `true` (or a wrapper script) as the
  "test" — a **vacuous** gate that is structurally valid and semantically empty.

## Goals

1. `striatum workflow generate --shape verification_gate [--write]` produces a
   `striatum workflow validate` → `valid` workflow with a real `type: verify`
   job feeding a downstream `claim_ledger`-reading gate, plus role/prompt
   scaffolding and a **template** allowlist — without committing any
   host-specific hash.
2. The common case ("this project's own tests/vet/build pass") is **runnable
   immediately** with zero operator JSON.
3. The operator fills in the host-specific part through a single, safe,
   sandbox-observed step — never by transcribing a sha by hand.
4. An **unfilled** template and a **vacuous** check both read RED (blocked
   pre-run or degraded-to-ASSERTED at the gate), never a false green.
5. The verified party (the lane) can author **neither** the sanctioned set
   **nor** its own pins **nor** its own attestation.
6. Slots into the existing generator + catalog at the `experimental` tier without
   breaking the catalog-reconcile test, and decides the fate of the interim
   hand-authored example.

## Non-Goals

- Re-opening D227: the daemon still NEVER executes a check. Every execution path
  here (builtins included) runs only in the off-gate-path verifier lane; the
  daemon only reads sealed receipts.
- A hosted/CI runner, cross-host quorum, or reproducible-build (SLSA/Nix)
  provenance infrastructure — out of scope for a single-operator, local-first
  tool (see Alternatives).
- Auto-generating *witnesses/checks themselves*. The shape scaffolds structure
  and sanctions named checks; it does not invent what to test.
- Changing the owner-held `jobs_job_type_check` (the `verify` job type is already
  live via owner bundle 0016).

## Proposal

### Pillar 1 — Two-layer allowlist + sandbox-observed pinning

Split the operator-curated allowlist into two artifacts:

- **`verification/allowlist.intent.json`** — committed, repo-relative,
  **hashless**. Schema marker `striatum.verifier_allowlist_intent.v1`: the
  existing `AllowlistEntry` **minus** `binary_sha256`, **plus** a `backs_claim`
  field naming which `claim_ledger` claim each check substantiates. This is the
  portable, reviewable "which checks are sanctioned, and what each one proves"
  layer. The scaffold emits it into the generated run's **`forbidden_paths`**, so
  **no lane can author or mutate its own sanctioned set** (separation of duties).
- **`verification/allowlist.pins.<host-arch-fp>.json`** — gitignored, per-host.
  The existing `striatum.verifier_allowlist.v1` shape, keyed by a host+arch
  fingerprint. Never committed, never lane-authored.

A new `striatum verifier pin --host-here` runs **inside the disposable sandbox
lane** (the same place `verifier run` already executes — never the daemon): it
loads the intent, resolves each sanctioned `argv[0]` on this host, recomputes its
sha256, and writes/updates the pins file — printing a per-entry diff, refusing to
pin when the resolved path/argv has drifted from the intent, and refusing to
overwrite an existing real pin without `--force`. The operator never transcribes
a hash; the sandbox observes it.

At `verifier run`, the lane joins intent ⋈ pins by check id and the
`claim_ledger` records a **three-valued** status (extending, not replacing, the
lattice):

- **NAMED-but-unpinned** — sanctioned in intent, no matching pin → never executed,
  caps at DESIGNED/ASSERTED.
- **PINNED-but-unattested** — bytes resolved and pinned, but no operator
  attestation → caps at ASSERTED.
- **VERIFIED** — intent-sanctioned **and** pinned **and** operator-attested **and**
  the two-signal sealed receipt binds the claim's inputs.

**Load-bearing risk (from the divergent pass):** the PINNED → VERIFIED promotion
is the whole trust hinge, and `pin --host-here` runs in the same sandbox the
verified party drives. If attestation were just a marker the pin step writes, a
compromised or overeager lane self-attests its own pins and silently re-collapses
VERIFIED into "we ran a check." **Therefore attestation MUST originate from a
principal the lane cannot impersonate** — the operator's daemon-minted capability
token, via a separate `striatum verifier attest --pin <id>` that is refused unless
invoked with an operator token (never a lane/session token). `pin --host-here`
only ever produces PINNED-but-unattested rows.

### Pillar 2 — Built-in, Striatum-pinned check library

Add a built-in check class the shape can name with **zero** operator allowlist
entry: `builtin:go-test`, `builtin:go-vet`, `builtin:go-build`,
`builtin:artifact-anchor-integrity`. An in-binary registry maps each id to a
fully-resolved argv plus a **self-pin** — the verifier process hashes its **own**
executable (`os.Executable()` → sha256) and stamps that into a synthetic
allowlist entry. `ExecuteCheck` consults the builtin registry first and falls
through to the operator allowlist only for unknown ids. So `generate --shape
verification_gate` yields a **runnable** workflow with no operator JSON for the
go-test/vet/build 80% case. Execution stays in the lane; the daemon still only
reads receipts; `BuiltinID` + `StriatumVersion` are added to the sealed receipt
transcript.

**Load-bearing risk (the sharpest finding):** a builtin escapes the
host-specific-hash problem only by relocating the pin from the *tool* (`go`,
whose hash differs per host) to the *striatum binary* (whose hash the lane can
self-compute). But the striatum self-pin proves **striatum invoked `go test`** —
it does **not** prove **which `go` ran**. A host with a trojaned `go` on PATH
produces a green builtin receipt indistinguishable from a clean one. This is a
lattice category-confusion: **a `builtin:*` receipt must cap at ASSERTED on the
self-pin alone.** It may reach VERIFIED only if the lane *also* seals an
independent toolchain fingerprint (`go version` / `go env GOROOT` digest captured
inside the sandbox) — but that quietly rebuilds a per-host hash, so it must be an
explicit per-repo opt-in (`builtin+toolchain:go-test`), never the default.
Correspondingly, the **generator refuses** to emit a `verification_gate` whose
completion threshold is VERIFIED while every `checks[]` entry is `builtin:*`
(else the gate is permanently un-satisfiable).

### Pillar 3 — The gate cannot lie green

Two failure modes the shape must defeat:

- **UNFILLED.** The template ships a typed sentinel — not a magic string — for
  each unpinned entry (an `Unpinned{host_fingerprint, fix_command, entry_pointer}`
  union variant, so a typo can never be mistaken for a real sha). `striatum
  workflow validate` (a pure dry-run, no lane) and `run start` both **hard-block**
  with a degrade reason naming the exact JSON pointer and the literal fix command
  (`striatum verifier pin --host-here …`); the workflow.json carries
  `allowlist_status: TEMPLATE_UNFILLED | FILLED` that `run start` reads. If the
  lane is somehow reached, it REFUSEs on the sentinel rather than executing.
- **VACUOUS.** Each sanctioned check declares a `negative_control` (a known-bad
  input the check MUST fail on). The verifier runs the control **first** and voids
  the receipt (`negative_control_did_not_fail`, RED) if it unexpectedly passes. At
  scaffold/graduation time a "thymic" dry-run exposes each templated check to a
  canonical all-green and all-red fixture and refuses to graduate any check that
  returns 0 on the all-red — catching `true`, empty suites, and unconditional
  exit-0 before the check is ever sanctioned.

**Load-bearing risk:** the negative control is itself an authored artifact that
can be vacuous ("fails because file-not-found," telling you nothing about whether
the check discriminates the *real* defect). "Fails on known-bad" is necessary but
not sufficient. Mitigation (stretch): require each control to be a **one-line
mutation of a paired passing fixture** (inject the exact defect class), so the
control provably exercises the same code path; flaky controls surface as a
`negative_control_nondeterministic` doctor class rather than silently eroding
trust through false-RED fatigue.

### Operator UX (the happy path)

```
striatum workflow generate --shape verification_gate --write   # runnable for builtins; ASSERTED gate
# (optional, to reach VERIFIED with external/custom checks:)
$EDITOR verification/allowlist.intent.json                      # sanction checks (hashless) + backs_claim
striatum verifier pin --host-here                              # sandbox observes + writes pins; no hand-typed sha
striatum verifier attest --pin <id>                            # operator-token-gated; promotes PINNED→attestable
striatum workflow validate <path>                              # blocks if any sentinel/unfilled
```

### Generator + catalog integration

Register `"verification_gate"` in the `shapes[]` map in
`go/pkg/workflowgenerate/generate.go`, add the matching generatable row to
`workflowtemplates/catalog.json` (+ regenerate `docs/reference/workflow-catalog.md`)
at the **`experimental`** tier so the existing shape-catalog-reconcile test stays
green. The shape scaffolds: `workflow.json` (real `type: verify` job →
`claim_ledger`-reading downstream gate), the hashless intent file (pushed onto
`forbidden_paths`), a `.gitignore` line for `allowlist.pins.*.json`, and
role/prompt docs teaching the lane to run the verifier and publish receipt +
ledger.

### Fate of the interim example

Keep [`examples/verification-gate-flow/`](../../examples/verification-gate-flow/)
as the **portable today-primitives demonstration** until the shape graduates from
`experimental`; then update its README to point at `generate --shape
verification_gate` and decide whether to regenerate it from the shape or retire it.

## Acceptance Criteria

- `striatum workflow generate --shape verification_gate --write` produces a
  `validate` → `valid` workflow that is **runnable with no operator JSON** for a
  `builtin:go-test` check, and the resulting gate's effective claim status reads
  **ASSERTED** (never VERIFIED) on the builtin self-pin alone.
- An unfilled external check (sentinel pin) makes both `workflow validate` and
  `run start` **fail closed** with a message naming the entry and the fix command.
- A vacuous check (`true`, or a control that passes) yields a **RED/void**
  receipt with a named reason; the thymic dry-run refuses to graduate a check
  that passes the all-red fixture.
- `pin --host-here` writes pins by sandbox observation and never requires a
  hand-typed hash; `attest` is refused without an operator token; the lane can
  author neither intent nor attestation.
- The generator REFUSES a VERIFIED-threshold gate composed only of `builtin:*`
  checks.
- No daemon-side execution is introduced; `evaluateRunClaimVerification` still
  shells out to nothing.

## Open Questions

- **Default gate floor.** Should the *default* generated gate target **ASSERTED**
  (runnable-and-honest out of the box, VERIFIED as an opt-in release-gate tier),
  rather than dangling a VERIFIED threshold that requires the full pin/attest
  ceremony? (The divergent pass's provocation — likely yes.)
- **Where attestation lives.** A separate `allowlist.pins.<fp>.attest.json`
  sidecar vs. an attestation field inside the pins file the `attest` verb stamps;
  both must keep the signal outside any lane-writable path.
- **Toolchain-fingerprint tier.** Is `builtin+toolchain:*` worth shipping at all,
  given it reintroduces a lane-computed per-host hash — or do external checks via
  the operator allowlist remain the only honest road to VERIFIED?
- **Negative-control rigor.** Is the mutation-of-a-paired-fixture requirement
  mandatory at graduation, or an opt-in `experimental`→`supported` bar?
- **Cross-host pins.** A `verifier pin --from-receipt <seal>` fast-path (same sha
  across hosts ⇒ attestable; divergent ⇒ flagged) for homelab/CI matrices —
  in-scope here or a separate follow-up?

## Alternatives Considered (and why not)

- **Pin a reproducible-build provenance attestation (SLSA / in-toto / Nix
  derivation) instead of a raw sha.** Portable and elegant, but imposes heavy
  external build-provenance infrastructure on a single-operator, local-first
  tool — premature. (Revisit if a fleet/CI need materializes.)
- **Cross-host quorum (a hash is trusted only once N hosts agree).** Scale
  mismatch — there is one operator, often one host; needs multi-host coordination
  that does not exist.
- **Execution-shape heuristics for vacuity (flag 0-byte / sub-10ms passes).**
  False-positive-prone (a legitimate fast `grep` looks vacuous) → a flaky gate
  operators learn to suppress. Negative controls are the deterministic form.
- **Claim↔check coverage-binding (refuse a strong claim backed by a weak check).**
  Requires semantic scoring of whether a check "covers" a claim — exactly the LLM
  judgment RFC 0134 exists to remove from the loop.
- **Affinity-maturation generations (`--generation N` seeded from prior
  receipts).** Genuinely novel but speculative and adds complex generator state;
  out of scope for a v1 shape.
- **TOFU (trust-on-first-use) as the default pinning mode.** Attractive UX, but
  blessing the first-observed bytes is a security-weaker default; acceptable only
  as an opt-in with a drift alarm, never the default road to VERIFIED.

## Domain Modeling

Per [`docs/DDD.md § "Adding to the model"`](../explanation/domain-driven-design.md#adding-to-the-model):

- **`AllowlistIntent`** — a **value object**: the committed, hashless, sanctioned
  set (`Check{id, argv, pass_when, backs_claim, negative_control}`). Reviewable
  policy; lives in `forbidden_paths`.
- **`HostPins`** — a per-host **value object** keyed by host+arch fingerprint,
  produced by sandbox observation, never committed, never lane-authored.
- **`Attestation`** — a value object minted only by an operator-token principal;
  the trust signal the lane cannot forge. The PINNED → VERIFIED hinge.
- **`Claim.status`** gains the explicit intermediate rungs **NAMED-but-unpinned**
  and **PINNED-but-unattested** between DESIGNED/ASSERTED and VERIFIED, so "we ran
  a check" never collapses into "a human stood behind the bytes."
- **`BuiltinCheck`** — a Striatum-release-pinned check whose identity is the
  striatum binary's own sha; honest about *who invoked the tool*, capped at
  ASSERTED.
- **Boundary clarification** — this shape draws the line between *portable policy*
  (committed intent) and *host-local fact* (observed pins), which is the general
  answer to "commit the policy, observe the bytes per host" the runner can reuse
  for other host-specific content-addressed resources.

## Implementation (D239)

Landed at the `experimental` tier. What shipped and how the Open Questions resolved:

- **Pillar 1 — two-layer allowlist.** `go/pkg/verifier/intent.go`
  (`striatum.verifier_allowlist_intent.v1`, hashless; `ParseIntent` rejects a stray
  `binary_sha256` and requires `backs_claim` + `negative_control`), the pure
  `JoinIntentPins` three-valued join + typed `Unpinned` sentinel, and
  `go/pkg/verifier/attest.go` (`striatum.verifier_allowlist_attest.v1`, keyed by
  id→sha). Verbs in `go/cmd/striatum/verifier.go`: `verifier pin --host-here`
  (observes the sha in the lane; drift/overwrite-refusing) and `verifier attest`
  (**refused inside a supervised lane** — `STRIATUM_SESSION_ID`/`STRIATUM_LANE_ID`
  present — so the verified lane cannot bless its own pins).
- **Pillar 2 — builtins.** `go/pkg/verifier/builtin.go`
  (`builtin:go-test`/`go-vet`/`go-build`/`artifact-anchor-integrity`, self-pinned to
  the striatum binary, `BuiltinID`+`StriatumVersion` sealed). The **HARD CAP** is
  enforced at the daemon gate read (`EffectiveStatusFromReceipt` returns ASSERTED for
  any `builtin_id` receipt regardless of strict posture + agreement) AND at lane-side
  classification. The generator **refuses** a `gate_floor=verified` gate composed
  only of builtins.
- **Pillar 3 — cannot lie green.** UNFILLED: `verifier.EvaluateAllowlistTemplate`
  (pure read, no execution) hard-blocks `workflow validate` (exit 8) and daemon
  `run start` naming the entry + the literal `verifier pin --host-here` fix; it is
  self-updating (pinning clears the block without regenerating). VACUOUS: a
  mandatory `negative_control` runs FIRST and voids the receipt
  (`negative_control_did_not_fail`) if the known-bad passes.
- **Shape + catalog.** `verification_gate` registered in `workflowgenerate` at the
  `experimental` tier (catalog-reconcile green), scaffolding a real `type: verify`
  job → `claim_ledger` gate, the hashless intent template (in `forbidden_paths`), a
  `.gitignore` for `allowlist.pins.*`, and role/prompt stubs.

Resolved Open Questions: **default gate floor = ASSERTED** (runnable-and-honest out
of the box; VERIFIED is the opt-in `gate_floor=verified` external road).
**Attestation lives in a sidecar** (`allowlist.pins.<fp>.attest.json`, never a
lane-writable path under the sanctioned authoring road). **`builtin+toolchain:*`
NOT shipped** — external operator-allowlisted checks remain the only honest road to
VERIFIED. **Mutation-of-paired-fixture negative-control rigor stays an opt-in**
graduation bar (the basic known-bad control is mandatory).

Graduation follow-ups (experimental → supported): **gate-side, daemon-authoritative
attestation enforcement** so a forged sidecar cannot reach VERIFIED (the current
verb-level operator-token gate is the experimental-tier boundary) — GH #482; doctor
self-pin/pin-drift classes + version-skew resweep — GH #483; and an RFC 0105
unattended-reliability fixture, after which the interim
`examples/verification-gate-flow/` is regenerated or retired. D227 preserved: the
daemon executes nothing; `evaluateRunClaimVerification` still shells out to nothing.
