# Task — Holder: present RFC 0142 and specify a build-ready P0

You are the **holder**. Read `SEED.md` and `RFC-0142.md` (both in your context
docs) in full first. Your published artifact is the **claim** the falsifiers will
attack.

Do not re-ideate the RFC and do not restate it section by section. Your job is to
(a) crisply state RFC 0142's single load-bearing claim and the P0 slice, and
(b) turn P0 into a **build-ready spec** precise enough to implement test-first.

## Deliverable

Write **one** artifact at the path declared in your work packet
(`docs/operator/artifacts/fg_rfc0142_design/dialogue/holder/HOLDER.md`), kind
`handoff`. No special front matter is required, but lead with a short title block.

It must contain:

1. **The claim under test.** Restate (don't re-derive) RFC 0142's load-bearing
   claim and the P0 load-bearing claim quoted in `SEED.md` ("a two-role pgtest
   fixture … reds the PR for exactly the migrations that would `42501` in prod …
   no false reds, no false greens"). These are what falsifiers will try to break.

2. **The build-ready P0 spec.** Concretely:
   - **Files to change.** The pgtest harness (`go/pkg/pgtest/pgtest.go`, anchor
     #6) and the migration test suite (`go/pkg/db/migrations_test.go` and/or a new
     `*_pg_test.go`). Name them.
   - **Role + ownership topology the fixture must provision.** Be explicit about
     the owner role vs. `striatumd_rw`, who owns the authority/append-only tables,
     and **how the migration suite is run as the privilege-constrained runtime
     role** (connect-as vs `SET ROLE`; `INHERIT`/`NOINHERIT`; whether role
     *membership* leaks owner power — see SEED falsification target #1). State the
     exact mechanism that makes a `42501` actually fire.
   - **The one red regression test.** Name it (e.g.
     `TestRuntimeMigrationOwnerTableTouchIsDeniedTwoRole`), state the owner table
     it touches, the DDL it attempts, and the expected `42501`/permission-denied
     failure that reproduces #442 / D248.
   - **A green control.** A legal runtime DML/migration that must **pass** under
     the same two-role fixture, so the fixture proves discrimination, not blanket
     failure.
   - **Consistency with existing guards.** How P0 relates to
     `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` (anchor #4) — complement, not
     duplicate — and where the "owner-held relation" set comes from so it does not
     drift from the static guard (RFC's "one source" claim).
   - **Boundary check.** Confirm P0 adds no runtime/owner migration and no daemon
     behavior change (test-harness + test code only).

3. **Falsifiable claims list.** For each load-bearing claim, state the evidence
   that would support it and the concrete observation that would refute it.

4. **Known risks you already see** (don't hide them — pre-empting a falsifier is
   stronger than being caught). At minimum address the fixture-fidelity and
   per-test-cost risks from SEED.

## Output contract

Write only your one artifact. Do not edit source, do not touch `.striatum/`, do
not treat downstream falsifier completion as acceptance — the collaboration
ledger decides whether the gate clears.
