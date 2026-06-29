---
schema_version: "striatum.finding.v1"
artifact_kind: "finding"
title: "RFC 0101 Layer 2 design synthesis review (codex)"
author: reviewer-codex-gpt-5.5-xhigh-001
lane: codex
status: blocked
verdict_intent: needs_revision
summary: "Verdict withheld because required synthesizer interrogation is unavailable; two design findings require revision."
---

# Design Review: RFC 0101 Layer 2

Verdict is withheld. The packet requires live interrogation of the synthesizer
before verdict, but this session cannot open an interrogation: `interrogation.open`
returned `capability_denied` for the `interrogate` capability and
`interrogation.list` showed no existing thread to use.

## Trust Boundaries And Attack Surfaces

- Adapter CLI and hosted provider boundary: C1, the health/auth probe, and the
  exit-75 path decide whether a live adapter run is treated as contract evidence
  or neutral infrastructure absence.
- Daemon protocol boundary: C3-C10 depend on daemon-observed MCP activity,
  leases, artifact rows, and liveness classification rather than PTY text.
- Helper metadata boundary: the design still uses PTY-helper event metadata and
  progress byte counts for some failure-class distinctions.
- Target repository versus operational scratch boundary: C12 protects tracked
  and untracked target-repo files outside `.striatum/scratch`, while agy MCP
  settings are proposed under `.striatum/scratch`.
- CI policy boundary: required-to-merge semantics depend on correctly mapping
  exit 1, exit 75, and release-mode hard failures.
- External freshness boundary: skip promotion and some gating decisions depend
  on installed adapter versions and, for issue-gated skips, networked issue
  state.

## Findings

### R1 - Neutral provider outcomes can still become an indefinite false-green door

The synthesis improves attempt 1 by separating contract failures from provider
or auth infra outcomes, but the accepted design still leaves per-PR exit 75 as
neutral/re-runnable with no adopted freshness bound. OQ6 proposes a
last-green-proof record plus an N-day staleness budget, but it is explicitly
"proposed not adopted." That means a sustained provider outage, expired shared
secret, or repeatedly flaky provider can keep every per-PR live conformance run
absent-but-neutral while merges continue. Release mode hard-failing helps
release cuts, but it does not protect the normal required-to-merge signal that
is supposed to catch the next #101-class CLI regression.

This is not only a policy problem. The contract/infra split trusts the C1
health/auth probe to decide whether the real adapter can be exercised. If that
probe is weak, adapter-controlled, or overbroad, a genuine adapter regression can
be routed into `AdapterProviderUnavailable` instead of a contract class. The
bounded retry then does not help, because C2-C12 never run after a C1 infra
outcome.

Required change: adopt a bounded freshness invariant in the design, not as an
open question. A provider/auth infra outcome may be neutral only while there is
a recent green live proof for the same adapter binary/version and the same
conformance mode. Past that staleness budget, per-PR CI must hard-block or the
system no longer provides a live adapter-conformance gate. Also require each C1
probe to be harness-owned and class-specific enough that adapter/provider
failure cannot be used to suppress C2-C12 contract evidence.

### R2 - The agy token move relocates the secret into an excluded scratch area

The synthesis picks a per-supervisor agy config home under
`.striatum/scratch/<supervisor_id>/agy-home/.gemini/settings.json` and then
uses C12 as the binding no-leak invariant. But C12 explicitly scans the target
repo "outside `.striatum/scratch/`." That protects durable provenance and normal
work-tree files, but it does not detect bearer persistence, symlink escape,
permission drift, copied auth material, or cleanup failure inside the scratch
home where the token is now intentionally written.

The design calls `.striatum/scratch` operational diagnostics rather than durable
workflow state, but that boundary does not make bearer material non-sensitive.
An attacker or failed teardown does not need the token to land in a tracked file
for the lane to have leaked capability material. The proposed mechanism also
has OQ1 open on whether redirected HOME/XDG_CONFIG_HOME de-auths agy, which may
encourage credential seeding or symlinking into the scratch home. That makes the
absence of a scratch-hygiene invariant more important, not less.

Required change: add a separate scratch-auth hygiene clause or extend C12 into a
two-part invariant: work-tree provenance scan plus scratch secret lifecycle
scan. The scratch clause should assert 0700/0600 permissions, no symlinked
credential paths crossing into the real user home, no bearer residue after
teardown, and token rotation/scrubbing guidance on failure. If the intent is
that `.striatum/scratch` is allowed to contain a live bearer only while the
lane is running, the clause should state that lifecycle explicitly.

## Non-Blocking Notes

- The attempt-3 correction that C0 is only an our-code golden and that #101-class
  CLI regressions are caught by Tier B C3/C4 is sound.
- The ordered daemon-observable clause list is the right backbone for Layer 2,
  provided the infra-neutral path and scratch-token lifecycle are closed.
- OQ2 and OQ3 are acceptable panel questions: they affect classification
  precision, but they do not by themselves create a green path if the hard/neutral
  gating and token lifecycle issues above are fixed.
