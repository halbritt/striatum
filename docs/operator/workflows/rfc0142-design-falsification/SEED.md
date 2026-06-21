# SEED — RFC 0142 design committee (falsification gate)

This run is a **design committee** that hardens **RFC 0142 — Safe-by-construction
database-change deployment** by adversarial falsification, and emits a
**build-ready P0 spec** the downstream implementation run will build.

- The design of record is **`RFC-0142.md`** (sibling file, verbatim copy of PR
  #538, `docs/rfcs/0142-safe-by-construction-database-change-deployment.md`,
  status *proposed, unmerged*). **Read it in full. Do not restate it.**
- The RFC was already produced by a divergent `/adhd` pass (5 frames → 8 converged
  mechanisms → 3 deepened branches). That exploration is captured in the RFC.
  **Do not re-run divergence.** This committee's job is *falsification and
  build-readiness*, not re-ideation.

## What this committee must produce

The deliverable of the whole run is the **commit_proposal synthesis**: a
**build-ready P0 spec** — a spec precise enough that an implementation lane can
build RFC 0142 **P0 (the two-role pgtest fixture)** test-first, with no remaining
design ambiguity. The collaboration ledger's surviving objections become
**binding constraints** the P0 spec must discharge.

**Scope this committee to P0 plus the design invariants P0 leans on.** P1–P5 are
in-scope only as *falsification targets for whether P0 is the right foundation*
(e.g. "does P0's oracle actually generalize to the Layer 1 lint / Layer 2
watermark it's meant to anchor?"). The build-ready spec output is **P0 only**.

### P0, precisely (RFC 0142 Layer 1a / Phase P0)

> Provision **both** the owner role and `striatumd_rw` in the pgtest cluster with
> the **real GRANT/ownership topology**, run the migration suite as
> `striatumd_rw`, and add **one red regression test** that reproduces the #442 /
> D248 `42501` (a runtime migration that `ALTER`s or FK-references an owner-held
> table fails `permission denied`). The single-role blind spot that hides `42501`
> locally stops existing.

## Verified source anchors (re-verified against `main` this session)

These were re-checked against current source — cite them, do not re-derive. (Go
module lives in `go/`.)

| # | Claim | Verdict | Anchor |
| --- | --- | --- | --- |
| 1 | `ConnectAndMigrate` applies runtime migrations as the **runtime role** automatically on daemon boot | TRUE | `go/pkg/db/connection.go:331` (`ConnectAndMigrate`), called from `go/cmd/striatumd/main.go:192-193`; applies via `ApplyMigrations(ctx, pool.Runner, daemonVersion)` as the connected (runtime) role |
| 2 | Runtime migration frontier = **0042** | TRUE | `go/pkg/db/sql/0042_verifier_attestations.sql`; `LatestDaemonDBVersion = 42` at `go/pkg/db/migrations.go:17` |
| 3 | Owner-bundle frontier = **0019** | TRUE | `go/pkg/db/sql/owner/0019_supervisor_pointer_runtime_ownership.sql`; `LatestOwnerBundleVersion = 19` at `go/pkg/db/owner.go:23` |
| 4 | Static build guard `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` exists | TRUE | `go/pkg/db/migrations_test.go:776-790`; floor version 27; calls `runtimeMigrationOwnerDDLViolations(migration, runtimeOwned)` — **static SQL parse**, not a live-role probe |
| 5 | Two-role boundary: owner role owns authority schema + SD write fns + append-only `events`/`audit_log`; `striatumd_rw` does DML + runtime migrations | TRUE | owner tables in `go/pkg/db/sql/owner/0001_authority_phase0.sql:20-87` + `go/pkg/db/write_authority_inventory.go:24-43`; owner-only: `daemon_auth_registry`, `daemon_auth_log`, `schema_authority`, `owner_bundle_meta`, `schema_meta`, `schema_migrations`; append-only via `assert_daemon_authority()` SD wrappers |
| 6 | **pgtest is single-role** (the P0 gap) | TRUE | `go/pkg/pgtest/pgtest.go:68-98`: `ensureRuntimeRole()` ensures canonical `striatumd_rw` exists; a **per-test role** `striatumd_rw_<dbname>` is created and made a **member** of `striatumd_rw` (`GRANT striatumd_rw TO <test_role>`, ~line 298). The base connection that runs `ConnectAndMigrate`/migrations is the **DSN user** (line 70) — typically owner/superuser, so an owner-table touch does **not** `42501` in tests today |
| 7 | `striatum daemon owner-ddl apply` applies owner bundles out-of-band, advancing `owner_bundle_meta` | TRUE | `go/pkg/cli/localcommands/daemon.go:84-159`; `db.ApplyOwnerBundles(...)` at line 131 |
| 8 | Two separate watermarks: `schema_migrations`/`schema_meta` (runtime) vs `owner_bundle_meta` (owner) | TRUE | `ReadSchemaVersion()` `go/pkg/db/migrations.go:167-177` (`schema_meta.substrate_version`); `OwnerBundleVersion()` `go/pkg/db/owner.go:94-115` (`MAX(version) FROM owner_bundle_meta`) |

**NEW symbols RFC 0142 proposes — confirmed ABSENT on `main`** (so they are
genuinely new, not duplications): `schema_state`, `ExpectedFingerprint`,
`LiveFingerprint`, `requires_owner_bundle`, `striatum daemon deploy`,
`striatum daemon rehearse`. (P0 introduces **none** of these — P0 is purely the
two-role test fixture + one regression test.)

## Why P0 is the right first slice (the claim to attack)

RFC 0142's self-applied discipline asserts the five failure modes reduce to one
coupling. **P0 does not fix the coupling** — it builds the **executable oracle**
that makes failure-mode #1 (the two-role `42501` trap) visible in CI, which every
later layer (the Layer 1 lint, Layer 2 watermark, Layer 3 deployer) leans on for
its differential property test. The load-bearing P0 claim the falsifiers should
try hardest to break:

> *"A two-role pgtest fixture that runs the migration suite as a privilege-
> constrained `striatumd_rw` against a cluster with the real owner/runtime
> ownership topology will red the PR for exactly the migrations that would
> `42501` in prod, and green for the ones that wouldn't — with no false reds
> (legal runtime DML mis-flagged) and no false greens (an owner-table touch that
> slips through)."*

## Falsification targets (non-exhaustive — find more)

1. **Fixture fidelity.** pgtest today runs migrations as the DSN user and makes
   the per-test role a *member* of `striatumd_rw` (anchor #6). Does merely
   `SET ROLE`/connecting as `striatumd_rw` reproduce prod privileges faithfully,
   or does role *membership* (inherited vs. `SET ROLE`, `INHERIT`/`NOINHERIT`,
   `SECURITY DEFINER` ownership, default-privilege quirks) leak owner power so the
   `42501` still won't fire? What exactly must the fixture provision to be honest?
2. **Who owns what in the test DB.** For the `42501` to reproduce, owner tables
   must be **owned by a distinct owner role** the runtime role lacks rights on. Can
   pgtest create that ownership split cheaply per test DB, given migrations +
   owner bundles must both be applied? In which order, by which role?
3. **Cost / isolation.** pgtest spins ephemeral DBs per test. Two-role setup adds
   role + GRANT + ownership-transfer work per DB. Is that affordable at suite
   scale, or does it need a shared template/cluster? Does it race under the
   parallel `_pg_test.go` suites?
4. **Does the oracle generalize?** RFC claims P0 is the oracle the Layer 1 lint
   and Layer 2 watermark lean on. Is the fixture's notion of "owner-held relation"
   the *same* source the lint denylist derives from (RFC says "one source so they
   cannot drift")? If P0 hardcodes a separate owner-table list, the anti-drift
   claim is already broken at P0.
5. **The regression test's honesty.** A red test that reproduces #442/D248 must
   fail for the *right reason* (`42501` on an owner-table touch), not for an
   unrelated setup error. How is that pinned? Is there a matching **green**
   control (a legal runtime migration that must pass) so the fixture isn't just
   "fails always"?
6. **Static-guard overlap.** `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL`
   already statically forbids owner-table DDL in runtime migrations (anchor #4).
   Does P0 duplicate it, subsume it, or complement it? RFC's own risk section says
   static SQL resolution is unsound — does P0's live fixture actually catch what
   the static guard misses (dynamic SQL, `DO`/`EXECUTE format()`), and is *that*
   demonstrated by a test, or merely asserted?

## Open questions carried from the RFC (P5-era; note, don't solve here)

The RFC's four Open Questions (full-data clone mechanism; cheap tier-down
prescreen; "how atomic is atomic"; should-a-deploy-be-a-run) are **P3–P5**
concerns. Do not block P0 on them. Flag only if a falsifier shows P0's shape
forecloses a viable answer to one of them.

## Product boundary (binding — see AGENTS.md, `docs/reference/spec.md`)

Local-first, single-operator, single-host, ONE Postgres, ONE daemon. No hosted
services, replicas-as-dependency, cloud APIs, telemetry, or external persistence.
An ephemeral local clone for rehearsal is in-boundary (operator-local scratch,
dropped after). P0 adds **no** runtime/owner migration and **no** new daemon
behavior — it is test-harness + test code only.

## Role focus for this committee

- **holder (claude):** Present RFC 0142 as the published claim, then specify a
  **concrete, build-ready P0 spec**: exactly which files change
  (`go/pkg/pgtest/pgtest.go` and the migration test suite), the exact role +
  ownership topology the fixture must provision, the **one red regression test**
  (name it, state the `42501` it reproduces and the owner table it touches) and a
  **green control**, and how P0 stays consistent with anchors #4–#6. Make every
  load-bearing claim falsifiable (state what evidence would refute it).
- **falsifier_1, falsifier_2 (agy/gemini):** Attack the P0 spec and its
  load-bearing claim. Use the falsification targets above as a floor, not a
  ceiling. Ground every challenge in the cited anchors or in concrete Postgres
  role/privilege semantics. Name the claim, the concrete counterexample, the
  strongest rebuttal you can still justify, and the unanswered gap.
- **adjudicate (claude, fresh session):** Read only the dialogue. Publish the
  `collaboration_ledger`. Convert each surviving material objection into a
  **binding constraint** the P0 build must discharge. Verdict
  `accept_with_findings` (clears with constraints folded in) if the P0 shape
  survives; `needs_revision` only if a falsifier lands a blow that invalidates the
  P0 shape itself.
- **commit_proposal (claude):** Only after a clearing verdict, publish the
  **build-ready P0 spec** synthesis: the P0 design with every binding constraint
  discharged inline, ready to hand to the implementation run.
- **final_summary (agy/gemini):** Summarize the cleared gate and what the
  downstream P0 build must honor.
