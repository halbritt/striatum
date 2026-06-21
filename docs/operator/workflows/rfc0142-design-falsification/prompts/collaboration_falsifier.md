# Task — Falsifier: try to break the P0 spec

You are a **falsifier** (a different model from the holder — your independence is
load-bearing). Read `SEED.md`, `RFC-0142.md`, and the published **holder
artifact** at `docs/operator/artifacts/fg_rfc0142_design/dialogue/holder/HOLDER.md`.
If a prior falsifier published, read it too and do **not** repeat its point —
advance a *distinct* line of attack.

Your goal is to **falsify**, not to review politely. Find the strongest concrete
reason the P0 spec or its load-bearing claim is wrong, insufficient, or unbuildable
as written. A challenge with no rebuttal acknowledged is weak; a challenge that
survives the holder's best rebuttal is strong.

## Where to aim (floor, not ceiling — find more)

Use the **falsification targets** in `SEED.md` (fixture fidelity / role-membership
privilege leak; owner-vs-runtime ownership split in the test DB; per-test cost and
parallel-suite races; whether the oracle generalizes to the Layer 1 lint / Layer 2
watermark; the regression test failing for the *right* reason + green control;
overlap with the static `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` guard).

Ground every challenge in **either** a cited source anchor (SEED table) **or**
concrete Postgres role/privilege semantics (role membership vs `SET ROLE`,
`INHERIT`/`NOINHERIT`, object ownership, default privileges, `has_table_privilege`,
what actually raises `SQLSTATE 42501`). A challenge that hand-waves "this might not
work" without a mechanism is not material.

The single highest-value falsification: **show a concrete configuration in which
the proposed fixture would NOT raise `42501` for an owner-table touch that fails in
prod (a false green), or WOULD red a legal runtime migration (a false red).**

## Deliverable

Write **one** artifact at the path declared in your work packet
(`.../dialogue/falsifier_1/FALSIFIER.md` or `.../falsifier_2/FALSIFIER.md`), kind
`handoff`. For each challenge include:

- **Claim challenged** — quote the holder's exact claim.
- **Counterexample / mechanism** — the concrete case or Postgres behavior that
  breaks it, with the anchor or semantics that grounds it.
- **Strongest rebuttal** — the best defense the holder could mount, stated
  honestly.
- **Residual gap** — what remains unanswered after that rebuttal, and what the P0
  spec would have to add to close it (this becomes a candidate binding constraint).
- **Severity** — does this invalidate the P0 *shape* (→ needs_revision) or is it a
  fixable gap the build must discharge (→ binding constraint under
  accept_with_findings)?

## Output contract

Write only your one challenge artifact. Do not invent missing holder content, do
not edit source, do not touch `.striatum/`, and do not decide the gate verdict —
that is the adjudicator's job.
