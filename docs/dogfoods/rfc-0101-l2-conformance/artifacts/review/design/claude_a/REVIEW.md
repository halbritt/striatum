---
schema_version: "striatum.finding.v1"
artifact_kind: "finding"
verdict_intent: "accept"
title: "Design review (ergonomics_dx) — RFC 0101 L2 adapter-conformance synthesis (attempt 3)"
author: reviewer-claude-opus-4.8-002
lane: claude_code
severity: "info"
tags:
  - "ergonomics_dx"
  - "ci-integration"
  - "developer-experience"
  - "rfc-0101-l2"
  - "revision-accept"
---

# Design review — RFC 0101 L2 synthesis, attempt 3 (ergonomics_dx posture)

**Verdict: `accept`.**

Posture: developer-ergonomics / CI-integration. Acceptance bar — *are the
operator/developer affordances discoverable and consistent for a first-time
user?* The attempt-3 synthesis (author `synthesizer-claude-opus-4.8-003`) keeps
every ergonomics finding I raised resolved and introduces **no** new
developer-facing affordance that could regress discoverability or consistency.
Re-confirming `accept`.

## Why a re-review at all

This is attempt 3. Attempt 3 was driven by the **devils_advocate** panelist's
`needs_revision` (R1–R3, written against attempt 1: provider-availability gating,
the C0/#101 scope claim, and the OQ5 auth soft-spot). Those are outside my
posture; my job here is to confirm the changes made to satisfy that reviewer did
not damage the operator/developer surface I accepted at attempt 2. I re-read the
current `DESIGN_SYNTHESIS.md` fresh (document_only) rather than relying on my
prior verdict.

## Evidence checked

- **Re-read in full:** the current `DESIGN_SYNTHESIS.md` (attempt 3), the sole
  `inputs` artifact `design_synthesis`.
- **Interrogation:** still unavailable to this lane (this reviewer session's
  capabilities are `["write","review","synthesis"]`; `interrogation.open`
  requires `interrogate`, `go/pkg/mutations/interrogation.go:50`), already
  reported via `session.report`. No ergonomics ambiguity remained that needed it.

## My findings remain resolved (attempt 2 → attempt 3, no regression)

- **R1 (local invocation / hard-fail scoping)** — §1.6 mode×outcome table, the
  copy-pasteable `STRIATUM_CONFORMANCE_ADAPTERS=claude_code make
  adapter-conformance-local`, and both `go/Makefile` + root `Makefile` targets
  (§4) are carried forward verbatim. Acceptance gate (7) intact.
- **R2 (human remediation surface)** — `report.go`'s per-failed-clause stdout
  line + static per-class hint map (§1.3) and the §4 CI wiring (uploads
  `dist/adapter-conformance.json` as a build artifact + echoes the summary to the
  job log) are unchanged.
- **R3 (`STRIATUM_CONFORMANCE_DEADLINE_SCALE` default)** — still pinned to `3.0`
  with rationale + worked example (§1.1).
- **R4 (env-prefix family)** — §2.3 still documents the `STRIATUM_CONFORMANCE_*`
  family and the intentional word-order divergence, surfaced via `--help` + README.

The revision note (§"Revision note (attempt 3)") explicitly records my attempt-2
`accept` and these sections as carried forward verbatim, and I verified that
against the body.

## Attempt-3 changes — ergonomics impact (none adverse)

The new attempt-3 material is internal-auditability and transparency, not new
user-facing affordances:
- **§0 / §1.4** add exact source citations for the C2 env surface
  (`supervisedEnvPassThrough` / `supervisedEnvAllowlistKeys` /
  `supervisedEnvEntries`, `supervision_control.go:2315/2339/2381`) and note the
  live C2 snapshots the *actually spawned* child env. Improves auditability;
  no operator-facing surface change.
- **§2.7** now states the bounded-retry cost explicitly (one extra live turn on
  the worst-case failing path, bounded 2×). This is a *positive* for DX — an
  operator reasoning about CI timing now sees the worst-case cost spelled out.
- **OQ6(a)** is sharpened to a named candidate (last-green-proof + staleness
  budget) but explicitly **"proposed not adopted,"** left to the panel. Correctly
  deferred — it is an open question, not a half-specified affordance, so there is
  nothing under-documented for a first-time user to trip on.

The previously-introduced exit-code (`0`/`1`/`75`)/`--release`/infra-vs-contract
surface remains documented and CI-mapped (§1.6, §2.7, §4); local mode still uses
only the simple `0`/`1` pair. No new discoverability or consistency defect found.

## Verdict rationale

All four of my findings stay resolved with the exact section anchors I accepted,
the spine is intact, and the attempt-3 hardenings do not touch — let alone
regress — the developer/operator surface. The affordances remain discoverable
and consistent. `accept`.
