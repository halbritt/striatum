---
schema_version: striatum.finding.v1
artifact_kind: finding
verdict_intent: needs_revision
severity: medium
author: reviewer-claude-opus-4.8-001
title: "Devil's-advocate review of RFC 0103 — Self-Hosting Production Hardening"
workflow: rfc-0103-review
role: reviewer
lane: claude
related: docs/rfcs/0103-self-hosting-production-hardening.md
---

# Review for RFC 0103: Self-Hosting Production Hardening (devil's advocate)

## Verdict

**needs_revision.** The *consolidation* is sound — the seven workstreams are a
clean partition of the 17 issues and each plausibly extends an owning slice-RFC.
What does **not** survive adversarial reading is the RFC's **acceptance
framework**, which is the load-bearing deliverable of an umbrella/sequencing RFC.
Two of its central claims are materially overstated and concretely fixable; this
is a request to tighten the gates and the umbrella wording, **not** to redo the
partition.

## What survives my strongest attack (keep as-is)

1. **The partition is real, not loose bucketing.** Counting the workstreams:
   W1 (#135/#70/#87) = 3, W2 (#95/#85/#76/#139) = 4, W3 (#141/#125) = 2,
   W4 (#131/#134) = 2, W5 (#126/#128) = 2, W6 (#115/#138) = 2, W7 (#92/#112) = 2,
   total **17, each issue appearing exactly once**. No cross-workstream overlap
   (the apparent #87 env-leak/libpq split and the #139 umbrella-over-#95/#85/#76
   both resolve *within one workstream*). The brief's Q1 worry ("loose
   bucketing") does not land — the grouping is mutually exclusive and, for the
   enumerated set, exhaustive. This is the RFC's strongest part.

2. **The slice-RFC ownership map is mostly coherent.** W1/W2→0096, W4→0095,
   W3→0091/0101, W5→0100, W6→0097, W7→0099/0102 are defensible homes, and the
   claim "no RFC owns *production-grade self-hosting as a property*" is the
   genuine justification for an umbrella. That thesis stands.

## Required changes before acceptance

### R1 (primary) — "each per-workstream acceptance is regression-gated so it can ship without a live dogfood" is **false as a universal claim** (§Proposal, lines 64–66)

This sentence is the RFC's central process promise. It fails for at least three
of seven workstreams, and the failures are in the *strong* property each
workstream exists to guarantee:

- **W1 is gated below its own claim.** The behavioral claim is "a lane started
  with session S cannot act as session S′ (token binding **observed live**)" —
  *observed live* is not a regression gate, it is the very one-shot evidence the
  RFC criticizes RFC 0097 for. The only hermetic gate offered is a conformance
  golden that "asserts the lane env carries the bound token and no DSN." That is
  **necessary but not sufficient**: a lane can carry its bound token and still be
  able to impersonate S′ if the daemon does not *reject* a cross-session token
  use on receipt. The security property (#135 spoof-closed-and-enforced) is
  exactly the part left un-gated. **Fix:** add a hermetic daemon-side test that a
  request bearing session S's minted token, presented as session S′, is
  *rejected* — and demote "observed live" from acceptance to corroboration.

- **W7 is not gateable at all, by the RFC's own Non-goals.** Acceptance is "the
  operator can drive a full run without dropping to tmux/systemctl/psql in the
  normal loop." The Non-goals (lines 203–205) explicitly frame W7 as "a
  cooperative harness contract … not forcible sandboxing of a process Striatum
  did not spawn." You cannot regression-gate a cooperative outcome — that an
  operator *chose* not to open psql is unobservable and unfalsifiable. **Fix:**
  either replace the acceptance with a concrete, audited assertion (e.g. "a run
  completes with **zero out-of-band control-plane escapes recorded in the audit
  log**," which *is* gateable), or label W7's acceptance honestly as best-effort/
  qualitative and drop it from the "regression-gated, ship-without-a-dogfood"
  set.

- **W3's gate likely does not exercise the real failure.** The acceptance is a
  chaos fault that restarts the **in-process** daemon. Issue #141 is an
  **OS-level/systemd** restart (`Restart=on-failure`) that *recreates the socket*
  and orphans helper processes — a different failure surface (helper-process
  orphaning + socket recreation) than an in-process restart. The RFC even hedges
  that the crashes "were themselves schema-drift (#142) … re-verify the crash is
  gone before building reconnect," which means the gate could go green while the
  real systemd-restart orphaning persists. **Fix:** either drive the gate through
  a real process/socket-recreation restart, or state explicitly that the
  in-process chaos restart is a *known approximation* and add the
  socket-recreation reconnect as a separate, named gate.

Net: change line 64–66 from a universal claim to a per-workstream table that
distinguishes **hermetic gate** (W4's PG-gated replacement-reviewer test and
W5's `artifact describe` / `lint` checks are genuinely this) from **live
observation** (W1) and **qualitative/cooperative** (W7). As written, the RFC
claims a uniformity of rigor it does not have.

### R2 (primary) — The umbrella acceptance reproduces the "proven once" weakness it is built to criticize (§Acceptance, lines 184–195)

The RFC's thesis is that "proven once" (one lane, one job, one fault-free run) is
*not* production-grade. Its umbrella acceptance is "**a** multi-lane,
review-gated dogfood … one bounded `needs_revision` cycle … surviving **at least
one** injected fault … that lands the fix." That is **a single trial with a
single injected fault** — i.e. *proven once, multi-lane*. By the RFC's own
standard, passing it would still only earn "proven once (harder shape)," not
"production-grade." It is also, by nature, a **live one-shot**, which contradicts
R1's stated preference to "ship without a live dogfood once proven." **Fix:**
either (a) re-label this honestly as "the new **floor** — multi-lane proven once
— a *precondition* for, not a demonstration of, production-grade," or (b)
strengthen the bar so the word "production-grade" is earned (e.g. the fault is
injected at N independent points / the dogfood is repeated across both supported
seats / a fault-class matrix, not "at least one"). Right now the headline
acceptance and the headline thesis are in direct tension.

## Findings to record (accept-with-noting if R1/R2 are addressed)

- **F1 — "Dependency ordering" is a priority ordering, and W1-first does not
  advance the umbrella (lines 168–183).** The RFC states the order then says "the
  layers are mostly independent and each is shippable alone." If each ships
  alone, there is no dependency — it is a *priority* order. More sharply: the
  umbrella acceptance is gated on W2/W3/W4 (seats, fault-survival, interrogation
  window), **not** W1. Putting the sandbox first is a *risk* argument, not a
  *critical-path-to-the-headline-goal* argument. Say so: "ordered by risk, not by
  dependency; the umbrella's critical path is W2/W3/W4."

- **F2 — W2 (agy) is the most deferrable workstream yet is framed as
  "multi-lane viability" (lines 89–100, 176–178).** W2 itself admits agy "is not
  yet viable" and that "claude/codex seats are the supported multi-lane shape,"
  while the umbrella requires only "**two** distinct adapter seats" = claude +
  codex. So the headline goal is reachable **without W2 at all**. W2 is real
  work, but the "multi-lane viability tier" label overstates its necessity for
  the umbrella; it belongs nearer the deferrable end.

- **F3 — #138 (shared-resource coordination) reads as a new orchestration
  primitive, not a "residual tail" (lines 144–153).** "Declare and serialize a
  shared resource (e.g. a DB-backed review gate)" is a new declaration + runtime
  serialization mechanism, which sits uneasily with the Non-goal "Not a rewrite …
  consolidates and sequences their residual tail." Either justify #138 as an
  in-scope new primitive or move it to an RFC 0097/0099 follow-up that owns it.

- **F4 — The "17 issues are the gap" boundary is selected, not derivable from
  the document.** The workstream bodies cite **#101** (W2, "the way #76/#101
  suppress the claude/codex nags") and **#65** (W4, "RFC 0095 #65") — neither is
  among the 17 enumerated issues. A reader cannot verify from the RFC that the
  set of 17 is *exhaustive* of "the gap"; it is asserted, with issues referenced
  just outside the count. One line stating the selection criterion ("open,
  not-yet-mechanical, blocks production-grade self-hosting; #101/#65 already
  landed/owned elsewhere") would make the partition's *completeness* checkable,
  not just its *internal* cleanliness.

- **F5 — "Two distinct adapter seats" is thin.** Given F2, the umbrella's two
  seats are claude + codex — both `process` / `agent_loop` lanes. "Distinct"
  carries less weight than it implies; the umbrella is really "two instances of
  the one supported shape." Worth naming so the bar is not read as broader than
  it is.

## Summary

Accept the **consolidation** — the partition and the slice-RFC ownership are the
RFC's strong, surviving core. **needs_revision** is driven entirely by the
**acceptance framework**: R1 (the "uniformly regression-gated" claim is false for
W1/W3/W7) and R2 (the umbrella acceptance is itself "proven once," contradicting
the thesis). Both are bounded wording/rigor fixes, not structural ones. Address
R1 and R2 and the F-series notes, and this is a sound umbrella to land.
