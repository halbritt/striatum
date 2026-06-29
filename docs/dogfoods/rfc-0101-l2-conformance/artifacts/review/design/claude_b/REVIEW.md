---
schema_version: "striatum.finding.v1"
artifact_kind: "finding"
verdict_intent: "accept"
title: "Devil's-advocate review (round 2) of the RFC 0101 L2 conformance design synthesis"
author: reviewer-claude-opus-4.8-001
date: 2026-05-31
severity: info
---

# Devil's-advocate design review (round 2) — RFC 0101 L2 adapter conformance

**Posture:** `devils_advocate`. Acceptance means the claims survived my strongest
counterarguments. This is the round-2 re-review of attempt 3, which was revised
in response to my round-1 `needs_revision` (R1–R3). I did **not** rubber-stamp:
I verified each closure against the revised text and then mounted three fresh
attacks on the *new* machinery the fix introduced. The closures hold and the
fresh attacks did not falsify the design — they landed on residuals the document
either prices explicitly or has already routed to the panel as OQ6. **Verdict:
accept.**

## Evidence checked

- Re-read `DESIGN_SYNTHESIS.md` attempt 3 (`design_synthesis`, 44663 B) in full.
- Cross-checked my round-1 finding (`design_review_claude_b`, verdict
  `needs_revision`, `verdict_d3e32abf…`, which routed the revision) against the
  attempt-3 closures line by line.
- Document-only per packet policy; no live-tree re-derivation.
- Interrogation again unavailable: this reviewer session's capabilities are
  `[write, review, synthesis]` with **no `interrogate`** (so `interrogation.open`
  is denied — the same gap the sibling reviewer escalated). Voted on the document
  per no-block guidance. (Scaffold fix: grant reviewer lanes `interrogate`.)

## Closure verification (R1–R3 from round 1)

**R1 — provider-unavailability imported as a hard merge gate (false-red whose
realistic mitigation is a false-green). CLOSED.** Attempt 3 adds a three-way
clause outcome — pass / contract-fail / **infra-outcome** (§1.1) — and a
**contract-vs-infra taxonomy partition** (§1.2). The pre-flight C1 gate (§2.7)
emits `AdapterUnavailable` / `AdapterUnauthenticated` / `AdapterProviderUnavailable`
as *infra* classes, never contract `FailureClass`es. The §1.6 mode×outcome table
makes the gating concrete: `--ci` maps a provider/auth blip to `EX_TEMPFAIL`/75
(neutral, re-runnable) and a contract failure to exit 1 (hard). The false-green
horn is closed in-text: because transience surfaces as an honest "provider
unavailable — re-run" status rather than a misleading contract red, a maintainer
has no incentive to disable the job, so a real regression still hard-blocks.
Acceptance gate (6) tests exactly this discrimination. This is a real fix, not a
relabel.

**R2 — "C0 is the #101 fix" overclaim + unspecified Tier B gating/CLI-pinning.
CLOSED.** §1.1 "C0 scope (R2 correction)" and §2.8(a) now state plainly that C0
is a golden over *our own* `bootstrapDeliveryModeFor`/`appendBootstrapArgv` and
**cannot** catch a CLI behavior regression like #101; the #101 class is caught by
**Tier B live C3/C4**. §2.8(b) declares the conformance job required-to-merge and
per-PR; §2.8(c) documents the unpinned-CLI detection-latency (attribution by
run/date+version, not PR) and records `binary_path`+`version` per adapter.
Acceptance gates (1) and (1b) separate the two catches.

**R3 — fold OQ5's auth soft-spot into R1's probe. CLOSED.** OQ5 is marked
RESOLVED and folded into the §2.7 pre-flight auth probe: a present-but-
unauthenticated adapter now fails fast as `AdapterUnauthenticated` instead of
surfacing later as a misleading `BootstrapStall`/`AwaitPacketStall`.

## Fresh adversarial attacks on the round-2 machinery (and why they don't block)

I treated the new §2.7 / §1.6 / §2.8 as a fresh attack surface:

**A1 — the bounded retry-once converts an *intermittent* genuine regression into
a green (new false-green).** §2.7 step 2 retries the whole live case once after a
green pre-flight, and treats a second stall as the genuine contract failure. But
a genuine #101-adjacent bug that submits its bootstrap only ~50% of the time
(a bootstrap *race*, a very plausible regression shape) will: first attempt
stall → retry → pass → reported PASS. The retry-once rule cannot distinguish an
intermittent genuine stall from a provider blip — by the document's own thesis
(you can't tell them apart by shape). **Why non-blocking:** this is an inherent,
acknowledged cost of the R1 fix, and the doc prices the symmetric case
explicitly ("one extra live turn… a bounded 2×… paid only on the rare failing
path"). Detection of a flaky regression is preserved across runs (§2.8c
latency). OQ6(b) already asks whether the retry is "sound." I record A1 as a
residual the implementer should note in `report.go` (e.g. surface "passed on
retry" rather than a clean green), not as a blocker.

**A2 — the entire R1/R3 resolution is load-bearing on a "cheap auth/provider
probe that does not burn a turn," which may not exist for these CLIs.** If
`preflight.go` reduces in practice to `--version` + local token-file presence,
then a *present-but-expired* credential passes C1 green, the turn then fails
mid-flight with a provider 401, that presents as a C3/C4 stall, gets retried
once, still 401, and is finally emitted as a **contract** `BootstrapStall` —
i.e., an auth problem mis-routed as a contract red (a residual false-red, and
the precise OQ5 failure mode R3 claimed to close). **Why non-blocking:** the doc
does not overclaim here — it explicitly scopes the *exact* per-adapter probe as
"an implementation detail to verify against each installed CLI (see OQ6)" and
OQ6(b) asks whether a green pre-flight is a sound precondition for the retry. The
architectural placement (a pre-flight gate emitting infra outcomes) is correct;
R3's closure is conditional on OQ6(b) resolving favorably, which the document
states. I flag that the probe's fidelity is the single highest-leverage
implementation risk in the design and should be the first thing proven in Step 4.

**A3 — the `EX_TEMPFAIL`/75-neutral mapping is itself a false-green door during a
*sustained* outage.** If the provider is down for days, every per-PR run returns
75/neutral, so the live signal is *absent* (not green) indefinitely, and a real
regression merged during that window is never caught by the only clause that
could (Tier B C3/C4). **Why non-blocking:** the document *raises this itself* as
OQ6(a) and proposes a concrete candidate disposition — a last-green-proof record
+ staleness budget N, past which per-PR escalates to hard-block. As devil's
advocate I confirm OQ6(a) is now the **single most important residual** in the
design and that the staleness-budget shape is the right answer; I recommend (for
the panel, not as a revision gate) that it be **adopted, not merely proposed**,
because without it the R1 fix's neutral mapping reintroduces a bounded-time
false-green. This is a panel decision the document correctly surfaced, so it does
not block acceptance.

## What this leaves

The spine I credited in round 1 is unchanged and still sound (real in-process
`striatumd` over a stub via the production `supervise.start` path; deadlines
derived from `sessionliveness.DefaultPolicy()`; the data-ledger non-rotting
skip). The R1–R3 closures are concrete and verifiable. My three fresh attacks
(A1–A3) found only residuals that are either explicitly priced (A1), honestly
scoped to implementation + OQ6 (A2), or already routed to the panel with a
candidate fix (A3). No new affirmative defect that the document denies or hides
survived.

## Verdict

**accept.** The claims survived my strongest counterarguments. Non-blocking
carry-forwards for the implementer/panel: **(A2)** prove the cheap pre-flight
auth/provider probe actually distinguishes expired creds and provider-down for
claude/codex/agy *before* relying on it (highest implementation risk; first thing
to validate in Step 4); **(A3 / OQ6a)** strongly consider *adopting* the
last-green-proof + staleness-budget so a sustained outage cannot leave a
bounded-time false-green; **(A1)** have `report.go` mark "passed-on-retry"
distinctly from a clean green so an intermittent regression is visible. Also a
workflow/scaffold note independent of the artifact: reviewer lanes should be
granted the `interrogate` capability — both devil's-advocate and ergonomics
reviewers were forced to vote document-only because `interrogation.open` is
denied for this role.
