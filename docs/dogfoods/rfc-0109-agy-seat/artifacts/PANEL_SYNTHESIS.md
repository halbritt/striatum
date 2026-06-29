---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
inputs:
  - docs/rfcs/0109-agy-lane-first-class-seat.md
  - docs/dogfoods/rfc-0109-agy-seat/prompts/present.md
  - docs/dogfoods/rfc-0109-agy-seat/artifacts/finding_needs_revision.md
---

# RFC 0109 — Agy-Seat Panel Synthesis

author: presenter-claude-opus-4.8-002

This panel exists to prove one thing the runner could not previously prove: that
a workflow which *declares* a three-lane interrogating panel actually *delivers*
three independent voices, and that each voice keeps its seat long enough to mean
something. Three short ideas carry that proof.

> **Revision note (attempt 2).** The agy reviewer
> (`reviewer-antigravity-gemini-001`) returned `needs_revision` against attempt 1,
> which claimed the seat holds by "re-entering the same attested session." That
> was wrong, and the correction is itself part of the proof — see the seat
> section below.

## Declared lanes must deliver three, or surface why not (the #139 collapse)

Naming three review lanes is a promise of three-way corroboration. The
dangerous failure is not a missing voice — it is a missing *signal* that a voice
is missing. When a panel silently delivers two because the third lane gated on a
folder-trust prompt at launch (#76 / #139) or collapsed after one turn (#95), the
run still reports green while the corroboration guarantee it was built to provide
has quietly evaporated. A declared-but-undelivered lane is worse than a panel
honestly declared as two: the operator believes they hold three-way agreement
while they hold two-way agreement wearing a three-way badge.

So the invariant: a declared lane either delivers its turn, or the runner
surfaces a concrete, attributable reason it did not — a degraded seat tier, an
attestation failure, a transport stall — never a silent downgrade. RFC 0109's
`degraded_seat_lane` tier is exactly that surfaced reason; it is allowed to fire,
but it is never allowed to be invisible.

## Holding a supervised multi-turn seat across `needs_revision`

A "seat" is the continuity of an attested *participant* — a specific lane/role
identity (here `claude`/`presenter`, `antigravity`/`reviewer`) — across the whole
of its participation, not one turn. In an interrogating panel that participation
must survive at least three daemon-mediated turns: (a) an interrogation exchange
against the presenter, (b) a first verdict, and — when that verdict is
`needs_revision` — (c) a *re-review* of the revised artifact on a later attempt.

Crucially, "the same seat" does **not** mean "the same session ID." The runner is
*expected* to rotate sessions across a `needs_revision` re-open: that verdict
closes the prior interrogable lane's session and spawns a fresh next-ordinal
lane, which registers a new session ID and earns its own fresh attestation. This
very document is the proof — attempt 1 was authored by
`presenter-claude-opus-4.8-001` in one session; you are reading attempt 2,
authored by `presenter-claude-opus-4.8-002` in a *different* session, with a
different session ID and independently established attestation. Attempt 1's claim
of a "byte-identical attested session" was therefore false, and the agy reviewer
was right to refuse it.

So the seat "holds" not by reusing a session, but by three verifiable properties
surviving the rotation:

- **Identity continuity** — attempt 2 is driven by the same `lane_id` / `role_id`
  (and the same model family / adapter) as attempt 1, so the voice is the same
  *kind* of voice, not a context-blind substitute from a different lane.
- **Auditable linkage** — the daemon's attempt chain and verdict ledger tie
  attempt 2 back to attempt 1 under one job, so the re-review is provably the
  continuation of the turn that demanded the revision, not an unrelated drive-by.
- **Per-attempt fresh attestation** — each rotated session is genuinely attested
  in its own right; the new session ID is *surfaced*, not hidden, and the rotation
  is the audited, expected event rather than a silent downgrade.

Session-ID rotation is thus a *feature to verify*, not a contradiction to hide:
the ledger should show two distinct, attested session IDs for the one presenter
role across attempts 1 and 2, both linked to the same job. The agy reviewer's
seat holds by the same rule — it re-reviews attempt 2 as the same
`antigravity` / `reviewer` identity, re-attested, with its second verdict chained
to its first.

This is the inverse of #95 / #139. #95 is the collapse: the lane keeps its session
against an in-process test agent but loses it against the real installed CLI after
one turn. #139 is the gate: the lane wedges on folder-trust before it can take its
first turn. Proving the seat *holds* is proving the negation of both — agy
interrogates, votes `needs_revision` with one concrete finding, and is still
there, attested (in whatever session the daemon assigns it), to re-review
attempt 2 and accept.

## Why the revision cycle is the load-bearing test

The cycle is the stress test, not an edge case. A seat that holds for a single
accept proves almost nothing — most adapters manage one turn. The cycle forces
the seat to persist across a daemon-mediated state transition
(`needs_revision` → revise → re-open) that historically closed the prior session
and spawned a next-ordinal lane (the lifecycle incoherence RFC 0095 addressed).
An adapter that holds its seat through that transition is one you can put on a
real review panel and trust the verdict ledger for: the lane and role on
attempt 1 and attempt 2 are the same identity — even though their session IDs
differ by design, and *because* that difference is surfaced and linked rather than
papered over — so the three-way agreement is real. That second agy turn surviving
the revision is the fact this entire panel was built to show; everything else is
scaffolding around it.
