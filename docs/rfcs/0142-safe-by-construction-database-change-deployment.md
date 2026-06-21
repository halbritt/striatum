# RFC 0142: Safe-by-construction database-change deployment — decouple schema mutation from serve-boot, catch the two-role/ordering traps before prod, and rehearse irreversible reshapes on an ephemeral clone

Status: proposed
Date: 2026-06-21
author: proposer-claude-opus-4-8-001

Context:
- The recurring production incident class this RFC closes is **a database
  change that wedges the single-writer daemon**: a runtime migration that
  `ALTER`s or `FOREIGN KEY`-references an **owner-held** table fails at boot
  with `ERROR: permission denied … (SQLSTATE 42501)`, the daemon **crash-loops**,
  the schema cannot advance, and there is no clean rollback. Live examples:
  [#442](https://github.com/halbritt/striatum/issues/442) / D236 (owner bundle
  `0018` had to transfer a pre-split runtime-table cohort to `striatumd_rw`
  before a runtime `ALTER`), and **D248** — RFC 0136 P1's migration
  `0041_event_chain_segments` carried a `REFERENCES striatumd.repositories` FK
  that the runtime role lacks `REFERENCES` privilege for; the new binary
  crash-looped and could not advance past schema 40 until the FK was removed.
- The **two-role security boundary** is load-bearing
  ([RFC 0110](0110-daemon-postgres-authentication-and-database-enforced-write-boundary.md)
  / D164, [D215](../decisions/decision-log.md) the per-object ownership split):
  an **owner/bootstrap role** owns the authority schema, the `SECURITY DEFINER`
  write functions, and the hash-chained append-only forensic tables
  (`events`, `audit_log`); a **runtime role** (`striatumd_rw`) does normal DML
  and applies runtime migrations. The boundary must be preserved — the runtime
  role must never gain owner privileges.
- Two change channels exist today: (1) **runtime migrations**
  `go/pkg/db/sql/NNNN_*.sql` (frontier `0042`), applied by `striatumd_rw`
  **automatically on daemon boot** (`ConnectAndMigrate`), advancing
  `schema_migrations`; (2) **owner bundles** `go/pkg/db/sql/owner/NNNN_*.sql`
  (frontier `0019`), applied **out-of-band** by the owner role via
  `striatum daemon owner-ddl apply`, advancing `owner_bundle_meta`. The build
  guard `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` enforces part of the
  boundary statically today.
- The highest-blast-radius pending change is [RFC 0136](0136-range-partition-events-audit-log-by-time.md)
  (#387) **P2/P3**: reshaping the PRIMARY KEY / UNIQUE keys of the **14M-row /
  17.3M-row live hash-chained** `events` / `audit_log` tables and attaching the
  legacy heaps as historical partitions — a one-shot owner-DDL cutover with **no
  rehearsal and no rollback** under the current model.
- This RFC reuses the **plan-hash receipt** pattern from
  [RFC 0134](0134-executable-verification-gate-and-claim-status-provenance.md) /
  [RFC 0141](0141-generatable-verification-gate-shape.md) — a sealed receipt
  bound to the exact bytes it certifies — and the **doctor-integrity** vocabulary
  (`*_unanchored`, `*_unreachable`) for surfacing the new invariants.
- Product boundary: **local-first, single-operator, single-host, ONE Postgres,
  ONE daemon (the single writer).** No hosted services, replicas-as-dependency,
  cloud APIs, or external persistence (AGENTS.md / D094). An **ephemeral local
  clone** of the database for rehearsal is within the boundary (it is operator-
  local scratch, dropped after use).

> **Self-applied discipline.** The single load-bearing claim of this RFC —
> *"most of these failure modes are symptoms of one coupling: the serving daemon
> mutates its own schema on restart, irreversibly, using the role least
> privileged to do it"* — was `ASSERTED`, then **`VERIFIED` against the source
> boot path** (`ConnectAndMigrate` runs the runtime migrations as `striatumd_rw`
> inside the serving daemon's startup) and **against the incident record** (#442,
> D248, and the owner-bundle-before-restart hazard all reduce to that coupling).
> The design therefore attacks the coupling directly (Layer 3) rather than only
> hardening each symptom.

## Problem

The current model fails in five distinct ways, each observed in production this
month:

1. **Two-role ownership trap.** A runtime migration that touches an owner-held
   table dies at boot with `42501`. **pgtest is single-role**, so this class is
   **invisible in CI and local runs** — it only appears against a real two-role
   cluster. (#442, D248.)
2. **Ordering hazard.** Owner bundles must be applied *before* the daemon
   restarts onto new code. Restarting with a pending owner bundle
   "force-commits a half-applied forward deploy" and crash-loops (seen when
   runtime migration `0040` depended on owner bundle `0019`'s ownership transfer).
3. **Irreversible apply on boot, no rehearsal.** Migrations apply on startup of
   the live writer with no dry-run, canary, or rehearsal against prod-shaped
   data. A bad migration wedges the single writer with no rollback seam.
4. **Migration-number collisions.** Concurrent PRs independently mint the same
   `NNNN` ordinal (two PRs both grabbed `0039` this month), resolved only by a
   manual renumber at merge.
5. **Un-rehearsable reshapes.** RFC 0136 P2/P3 must change the identity key of
   live hash-chained tables and attach legacy heaps — a destructive, one-shot
   owner-DDL step with no way to prove the tamper-evident chain stays
   byte-identical, no rollback, and no lock-duration budget before it stalls the
   single writer.

## The reframe

Failure modes 1–3 are not independent. They are all consequences of one
architectural decision: **the process that serves traffic is the same process
that mutates its own schema, it does so implicitly as a side effect of restart,
the mutation is irreversible, and it runs under the runtime role.** Remove that
coupling and:

- mutation stops being a restart side effect → no "restart force-commits a
  half-applied deploy" (failure 2);
- the serving daemon can hold **zero DDL privilege** → a runtime migration
  *cannot* touch an owner table because the serving role never applies DDL at
  all (failure 1's last line of defense);
- mutation becomes an explicit, ordered, **resumable, dry-runnable** operation →
  rehearsal and rollback seams become possible (failures 3, 5).

So the design is **layered defense-in-depth**, with Layer 3 (decoupling) as the
structural keystone and the other layers catching the remaining classes earlier
and cheaper.

## Proposed design

Five layers, each independently valuable and shippable, ordered by leverage and
inverse risk. A change passes **every applicable gate** before it touches prod.

### Layer 0 — Collision-proof ordinals (kills failure 4)

A committed **reservation ledger** (`go/pkg/db/sql/RESERVATIONS.toml`, listing
every claimed runtime-migration and owner-bundle ordinal with its PR / author)
plus a **CI guard** that fails any branch introducing a duplicate or a gap.
Ordinals stay human-legible monotonic integers (operators reason about
"schema 42"); the ledger is the allocation authority a reviewer cannot forget to
check. *(The content-addressed-hash-DAG alternative is a Rejected Alternative —
see below.)*

### Layer 1 — Pre-prod gates (catches failure 1 at author- and CI-time)

- **Two-role pgtest fixture.** The ephemeral test cluster provisions **both** the
  owner role and `striatumd_rw` with the **real GRANT/ownership topology**, and
  runs the migration suite as `striatumd_rw`. The single-role blind spot that
  hides `42501` locally **stops existing**: an illegal `ALTER` on `events` reds
  the PR. This is the executable oracle the other layers lean on, and the first
  thing to build.
- **Ownership pre-flight lint.** A load-time pass resolves each runtime
  migration's target relations and checks them against table ownership (a
  build-time owner-held-relation denylist derived from the owner-bundle
  manifests, cross-checked by a `has_table_privilege` probe), **refusing at
  parse time** any runtime migration that `ALTER`s or FK-references an owner-held
  relation — routing the author to an owner bundle before a statement runs. The
  denylist and the Layer 2 watermark are generated from **one** source so they
  cannot drift.

### Layer 2 — Owner-bundle watermark interlock + fail-clean (kills failure 2)

The daemon binary declares `requires_owner_bundle >= N` (generated from the
owner bundles it was built against). On boot it compares that to the applied
`owner_bundle_meta` watermark. On a shortfall it **HALTS CLEANLY** — a typed
`awaiting_owner_ddl` exit, **the database untouched**, printing the exact
`striatum daemon owner-ddl apply` command — instead of force-committing a
half-applied deploy and thrashing (*apoptosis, not necrosis*). The same shortfall
is surfaced as a `striatum doctor` precondition and an `operator bootstrap`
`next_action`, so the gap is visible **before** the restart. The symmetric
**downgrade** direction (binary older than the applied watermark) has an explicit
encoded policy (refuse vs. tolerate-forward), so a botched binary rollback can't
crash-loop either.

### Layer 3 — Decouple apply from serve (the keystone; dissolves failures 1–3)

- **Schema fingerprint contract.** The serving daemon computes
  `ExpectedFingerprint()` — a hash of the ordered set of runtime migrations +
  owner bundles its binary embeds — and reads `LiveFingerprint(db)` from a
  `schema_state` row recording the last successfully-applied deploy plan. On
  divergence it **refuses to serve** with a structured `schema_drift` error
  (naming the missing/extra steps + the exact reconcile command) and **mutates
  nothing**.
- **One-shot deployer.** `striatum daemon deploy` is the **only** mutator. It
  loads an ordered **deploy plan** (a manifest enumerating every step with its
  role — `owner` or `runtime` — and dependency edges), opens **both** role
  connections, topologically orders the steps, and applies them, advancing a
  durable `deploy_cursor` **after each committed step** so a crash resumes at the
  next clean boundary rather than re-running or half-applying. Ownership-of-step
  is a property **inside one plan**, so the owner-before-restart race disappears.
- **Serving role loses DDL.** Because the serving daemon never applies DDL, the
  runtime role's DDL grant can be **revoked** — failure 1 becomes structurally
  impossible on the serving path.
- **Deploy receipt.** `deploy` writes a hash-chained **deploy receipt** into the
  owner-held `audit_log`, so every schema change is first-class adjudicated
  provenance; `doctor` gains `schema_deploy_unrecorded`.

### Layer 4 — Rehearsal receipt + expand/contract (kills failures 3, 5)

- **Rehearsal on an ephemeral two-role clone.** `striatum daemon rehearse <plan>`
  spins `CREATE DATABASE …_rehearsal_<planhash>` provisioned with the **real
  two-role topology** (replay the owner GRANT bundle, not a superuser shortcut,
  so a privilege failure surfaces here), replays the exact migration+bundle byte
  sequence, and emits a signed **`rehearsal_receipt.v1`**:
  `{plan_hash, fidelity_tier ∈ {schema_only, sampled, full_data}, per-table
  row_counts, chain_continuity_proof, lock_duration_ms, inverse_ddl_present}`.
  The prod apply path **refuses** unless a receipt whose `plan_hash` matches the
  plan exists and verifies — binding the proof to the applied bytes exactly as
  RFC 0134 binds a verifier receipt to claim bytes.
- **Fidelity tiering (the honesty hinge).** Clone cost is tiered by failure
  class, not by dogma. A **schema-only / sampled** clone (seconds) is sufficient
  for DDL / grant / lock-shape / FK-rebuild failures. **Full-data is required**
  for any UNIQUE/PK reshape, because the highest-value failure — a duplicate
  `row_hash` straddling the new partition key when global uniqueness is re-scoped
  — only fires against real data. The receipt **records its own fidelity tier**,
  and the gate **refuses a sampled-tier receipt for a UNIQUE/PK reshape class**.
  Full-data is made affordable on one host via a CoW / filesystem snapshot of
  just the two chained tables (minutes, dropped after), never a hosted replica.
- **Expand/contract for irreversible reshapes.** A reshape ships as a reversible
  additive **expand** owner bundle (build the new partitioned shadow, backfill
  online, keep the old heap byte-intact, prove the chain byte-identical across
  the boundary on the clone), then a **separately-gated contract** bundle that
  does the atomic `RENAME` swap behind an **operator-revocable abort window** —
  never an in-place, irreversible boot-time rewrite. A **lock-budget guardrail**
  derived from the receipt's measured `lock_duration_ms` (scaled by the
  prod/clone row ratio) sets prod `lock_timeout`, so a slower-than-rehearsed swap
  self-aborts inside the window instead of stalling the single writer.

## Phasing

| Phase | Scope | Why this order |
| --- | --- | --- |
| **P0 — two-role pgtest fixture** | Layer 1a. Provision both roles + real grants; one red regression test reproducing the #442 `42501`. | Highest leverage, lowest ambiguity, becomes the oracle for everything else. Catches the named root cause in CI today. |
| **P1 — ownership pre-flight lint + Layer 0 ledger** | Static owner-table denylist (generated) + load-time refusal + reservation ledger + CI collision guard. | Cheap author-time gates; remove the two most common foot-guns before they reach CI. |
| **P2 — watermark interlock + fail-clean** | Layer 2: `requires_owner_bundle` declaration, clean `awaiting_owner_ddl` halt, doctor precondition, downgrade policy. | Converts any surviving ordering/ownership miss from a crash-loop into a clean, actionable stop. Small, high-fit. |
| **P3 — schema-fingerprint drift gate** | Layer 3 part 1: `schema_state`, `ExpectedFingerprint`/`LiveFingerprint`, refuse-to-serve on drift — **without moving where DDL runs yet**. | Ships the loud, test-covered drift gate first; de-risks P4. |
| **P4 — one-shot deployer** | Layer 3 part 2: lift auto-apply out of serve-boot into `striatum daemon deploy` (both-role coordinator, resumable cursor, deploy receipt); revoke serving-role DDL. | The structural change; only after the drift contract is proven. |
| **P5 — rehearsal receipt + expand/contract** | Layer 4: `rehearse`, `rehearsal_receipt.v1`, fidelity tiering, expand/contract primitive, lock-budget. Unblocks RFC 0136 P2/P3 safely. | Highest-risk owner DDL; lands last, on the foundation P0–P4 provide. |

P0–P2 are pure safety nets shippable immediately as DIRECT runner-fix PRs
(D208/D210). P3–P5 are larger and benefit from being sliced into tracked issues.

## Load-bearing risks

- **Static SQL resolution is unsound (Layer 1 lint).** Migrations can construct
  relation names dynamically (`DO` blocks, `EXECUTE format()`, `search_path`-
  relative names), so a naive parser yields false-negatives (dangerous) or
  false-positives (authors bypass the lint). Mitigation: the **two-role fixture
  (P0) is the real oracle**; the lint is a fast pre-filter, and a differential
  property test asserts the lint and the live grants agree on exactly which
  operations are forbidden. If the resolver can't be made sound, lean on the
  fixture + the watermark interlock.
- **"Atomic plan" is partly a lie (Layer 3).** You cannot wrap a mixed
  owner+runtime plan in one transaction (two connections; non-transactional DDL
  like `CREATE INDEX CONCURRENTLY`). The real contract is **per-step-atomic with
  a resumable cursor**: every step must be idempotent and leave a coherent
  intermediate the fingerprint classifies as "incomplete, resume" — not "unknown
  drift, panic." This is the hard correctness core of P4.
- **Rehearsal is only as honest as the clone (Layer 4).** The highest-value
  failure (straddling-duplicate `row_hash` under a re-scoped UNIQUE) is exactly
  what a cheap clone can't catch; a half-built partition set on an append-only
  hash-chained table is indistinguishable from tampering. The gate's credibility
  depends entirely on the receipt faithfully encoding its fidelity tier **and**
  the prod gate refusing the insufficient tier for the specific reshape class.

## Rejected alternatives (the traps the divergent pass surfaced)

- **In-prod savepoint / "transactional migrations" dry-probe.** Probe each
  migration in `SAVEPOINT/ROLLBACK` against the live DB before committing.
  **Rejected:** many DDLs auto-commit or cannot run transactionally
  (`CREATE INDEX CONCURRENTLY`, several `ALTER`s) — precisely the big reshapes
  this most needs to cover — and probing in the live DB still risks side effects.
  Rehearse on a clone (Layer 4) instead.
- **Content-addressed migration hashes + dependency DAG.** Replace the monotonic
  ordinal with a content-hash "codon" + `depends_on` parent. **Rejected:**
  premature abstraction for a single-operator tool; a large loader rewrite and a
  loss of human-legible "schema 42" reasoning, for a *minor* pain. The
  reservation ledger + CI guard (Layer 0) is the right-sized form.
- **Schema-only / sampled clone as the universal rehearsal substrate.**
  **Rejected as a default:** false confidence — a data-dependent failure only
  fires on full data. Kept only as an explicit **fidelity tier** the gate
  reasons about, never as the silent default for a UNIQUE/PK reshape.
- **Declarative desired-schema spec + diff engine that auto-generates DDL.**
  **Rejected:** that is a whole migration framework (Atlas/skeema-shaped); risky
  for hash-chained owner tables where the exact DDL bytes are load-bearing, and a
  large external-tool dependency that strains the local-first boundary.

## Open Questions

1. **Full-data clone mechanism.** CoW filesystem snapshot of `PGDATA` vs.
   `pg_dump --schema-only` + a targeted `COPY` of the two chained tables vs.
   `CREATE DATABASE … TEMPLATE`. The right answer depends on the host filesystem
   (Btrfs/ZF S CoW is near-free; ext4 is a full copy of ~29 GB). **Operator/host
   call**, pinned before P5.
2. **Cheap tier-down prescreen.** A read-only `GROUP BY … HAVING count(*) > 1`
   on prod under the *proposed* partition scoping can prove no straddling
   duplicate exists, letting the gate *safely* accept a schema-only receipt for a
   UNIQUE/PK reshape. Worth the complexity, or always pay for full-data on
   UNIQUE/PK changes? Lean: implement the prescreen — it is the difference
   between minutes and "always full clone."
3. **How atomic is "atomic"?** Confirm the per-step-resumable-cursor contract is
   sufficient for every owner+runtime interleaving we actually ship, or whether a
   small set of steps need a stricter (single-connection, single-transaction)
   sub-protocol. Pin before P4.
4. **Should a deploy be a Striatum run?** (See Provocation.) Decide whether the
   deployer is a plain verb or a dogfooded run shape before P4/P5 lock the verb
   surface.

## Domain Modeling

This introduces several **value objects** and one **boundary clarification**.

- **Deploy plan** — the ordered, role-tagged, dependency-edged manifest of schema
  steps; the *single clock* that replaces the two independent channel clocks.
- **Schema fingerprint** — the content hash of the applied plan; the contract
  between "what the binary expects" and "what the live DB is." Drift is a
  legible, named state, not a panic.
- **Rehearsal receipt** (`rehearsal_receipt.v1`) — a sealed, plan-hash-bound,
  fidelity-tiered proof that a plan landed clean on a prod-shaped clone; the
  unit the prod gate refuses-without.
- **Chain segment boundary proof** — the recomputed head/tail `row_hash` pairs
  across each reshape boundary (reusing RFC 0136 P1's `event_chain_segments`
  sealing), turning the receipt into a *post-apply oracle*, not just a pre-apply
  permit.

The **boundary clarification** is that **schema mutation is an explicit,
provenance-tracked operation owned by a dedicated deployer**, not an implicit
side effect of the serving process's startup. Cites
[`docs/explanation/domain-driven-design.md § "Adding to the model"`](../explanation/domain-driven-design.md#adding-to-the-model);
RFC 0110 (the two-role boundary), RFC 0134 (the receipt-binding precedent), and
RFC 0136 (the reshape this unblocks) are the precedents.

## Provocation (a direction to push, not yet proposed)

**What if a schema change is itself a Striatum run?** The deploy becomes a
first-class, adjudicated, hash-chained run (`expand_rehearsal` → `contract_swap`
shapes), the rehearsal receipt is the inter-stage hand-off artifact, the
operator-revocable abort window maps onto an acceptance gate, and `doctor`
already owns the integrity checks — i.e. **Striatum deploys its own schema
through its own runner**, with the verifier-receipt + provenance machinery it
already has. That would make "no dry-run before an irreversible apply"
*structurally* impossible rather than a discipline the operator must remember —
at the cost of bootstrapping (the runner needs a schema to run the deploy that
changes the schema). Captured as Open Question 4.
