---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
topic: "RFC 0136 range-partition events/audit_log by created_at/ts — the PK/unique-key reshape, owner-DDL discipline, partition-DROP retention subsuming #386, and the honest 0142-P5 dependency boundary"
participants:
  - "holder-author-001"
  - "falsifier-reviewer-001"
  - "falsifier-reviewer-002"
  - "adjudicator-author-001"
cycle: 1
adjudicators:
  - "adjudicator-author-001"
adjudication_mode: "single"
entries:
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: "CORE — declarative range partitioning of events (by created_at) and audit_log (by ts) FORCES a PK/UNIQUE reshape on both owner-held, append-only, hash-chained tables AND that reshape preserves every existing integrity guarantee, provided the one invalidated inbound FK (repo_event_chain_heads -> events) moves its RI into the co-transactional Go/SD write path and the live ~14M-row cutover is rehearsed on RFC 0142 P5 before production."
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: "H1 — the reshape is forced and partition-legal: events PK -> (repository_id, event_id, created_at); audit_log PK -> (audit_id, ts) and UNIQUE -> (row_hash, ts); PostgreSQL refuses a partitioned PK/UNIQUE that omits the partition key. Both partition columns are already NOT NULL and already written by the append functions."
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: "H3 — the chain-head FK is dropped and its referential integrity is enforced in Go/SD inside the head-advance transaction; safe because the closed three-writer set (append_event_row, mutations.go:1782-1818, escalation_resolve.go:552-588) each inserts the referenced event in the same FOR UPDATE transaction; a grep/AST build guard asserts the writer set is closed and co-transactional."
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: "H4 — the two hash chains are byte-invariant across partitioning: created_at/event_id (event hash) and ts (audit hash) are already hash inputs, so adding them to the PK tuple changes no hash input; the verifier checks linkage, not physical layout."
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: "H5 — append-only triggers and append routing survive partitioning (row triggers propagate to partitions; INSERTs route transparently), and partition DETACH/DROP is catalog DDL that does NOT fire the *_no_delete row triggers."
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: "H6 — a partition is droppable only after the chain segment it contains is sealed and its boundary hashes are recorded; the shipped event_chain_segments ledger (P1/D242), SealEventChainSegment, and the seam-unproven doctor invariant give the event side the same seal-before-drop capability the audit chain has."
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: "H7 — partition DETACH/DROP subsumes #386's delete-time RI seq-scan for the retention path, because retiring old data becomes catalog DDL with zero child-side RI scan (no inbound FK remains after the chain-head FK moves to Go); the #386 covering indexes are retained for point lookups and the parent-delete direction."
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: "H8 — the reshape is owner DDL on a re-fetched ordinal >= 0022 (C-1: owner 0020 is taken by the watermark-read bundle and 0021 is reserved by RFC 0142 P4), capability-parity gated, never a runtime migration; the runtime-DDL guard is TestFutureRuntimeMigrationsDoNotCarryOwnerDDL (corrected to migrations_test.go:643)."
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: "H9 — execution uses backfill form A (attach the legacy heap as a sealed historical partition + rename cutover); the live ~14M-row cutover's online-safety is un-rehearsable until RFC 0142 P5's expand/contract + rehearsal_receipt harness exists (accepted D258, not yet built)."
  - kind: challenge
    by: "falsifier-reviewer-001"
    refs: ["dialogue:2"]
    text: "Dropping the chain-head SQL FK leaves a DB-level RI hole: repo_event_chain_heads retains a direct runtime GRANT SELECT,INSERT,UPDATE to striatumd_rw (0006:106-112) and is classed ClassRuntimeDML, not SD-gated (write_authority_inventory.go:50-55,115). The FK was the only DB guard preventing a direct runtime UPDATE from pointing last_event_id at a non-existent event; H3's Go grep/AST writer-set guard cannot cover the SQL privilege surface, so the chain-head FK guarantee is NOT preserved."
    correspondence: landed_unrebutted
  - kind: challenge
    by: "falsifier-reviewer-001"
    refs: ["dialogue:2"]
    text: "The CORE over-claims 'preserves every existing integrity guarantee': the reshape narrows DB-enforced uniqueness — UNIQUE(row_hash) -> UNIQUE(row_hash, ts) lets a duplicate row_hash with a different ts commit, and PK (repository_id, event_id) -> (repository_id, event_id, created_at) lets duplicate logical event ids commit across partitions, so (repository_id, event_id) is no longer DB-enforced row identity (and is exactly what the chain head stores). The SPEC neither carries the D241 Q3=(a) acceptance into the CORE nor adds a compensating in-DB guard."
    correspondence: landed_unrebutted
  - kind: challenge
    by: "falsifier-reviewer-002"
    refs: ["dialogue:3"]
    text: "The event retention purge transition H6/H7 depend on is forbidden by shipped source: the 0041 trigger refuse_sealed_event_segment_change raises on any UPDATE where OLD.state <> 'open', and TestSealedEventSegmentIsAppendOnly asserts UPDATE ... SET retention_state='purged' on a sealed segment MUST fail. So a sealed event segment cannot be marked purged/retired to witness a partition DROP; H6 over-claims event-side parity with audit, and the SPEC names neither the reachable state transition, its runtime-vs-owner-bundle DDL placement, nor a fail-closed sequencing check — the exact owner/runtime plane D187/#244 punishes."
    correspondence: landed_unrebutted
  - kind: challenge
    by: "falsifier-reviewer-002"
    refs: ["dialogue:3"]
    text: "H7's #386-subsumption language over-claims: it says 'retiring old events/audit_log data becomes a partition DETACH/DROP', but D241 pins audit_log as partitioned-but-never-dropped (infinite retention; P4's live DROP path is events-only). The subsumption must narrow to the events retention-DROP path; audit_log gets read/VACUUM benefits and keeps #386, with no live audit DROP path unless a future decision sets a finite audit horizon."
    correspondence: landed_unrebutted
verdict: "needs_revision"
rationale: "Both falsifier challenges land material, source-confirmed gaps that the v1 SPEC does not pre-empt, each mapping onto a named needs_revision trigger. F1 breaks the integrity-preservation core on two axes: (a) the dropped chain-head FK is not replaced by an equivalent DB-level guard given repo_event_chain_heads' retained runtime DML privilege (verified: GRANT SELECT,INSERT,UPDATE at 0006:109; ClassRuntimeDML at write_authority_inventory.go:51-55) — a Go-only writer-set guard cannot close a SQL-privilege hole; and (b) the CORE's 'preserves every existing integrity guarantee' is an over-claim while the reshape silently drops DB-enforced global row_hash uniqueness and per-repo (repository_id,event_id) row identity. F2 breaks the retention contract: the sealed->purged transition H6/H7 require is forbidden by shipped source (0041 trigger + TestSealedEventSegmentIsAppendOnly, both verified), and the SPEC over-claims event-side seal-before-drop parity and build-readiness while leaving the retention state machine and its cross-plane DDL placement unspecified — over-claiming designability ahead of the P5-gated execution. The hard core HOLDS and must carry forward intact: H1 (forced reshape), H4 (hash-chain byte-invariance), H5 (trigger/routing survival + trigger-clean DROP), H8's ordinal/owner-DDL discipline for the reshape itself (incl. the verified C-1/C-2/C-3 corrections), and H9's honest P5-gating of the live cutover lock-window. This is the single allowed v1 revision cycle: the holder revises HOLDER.md to close C1-C4 below; the falsifiers re-attack. A second needs_revision ends the gate unCleared and routes to the operator for a fresh -v2 run."
findings:
  - id: F1
    severity: critical
    posture: design
    status: open
    challenge: "Chain-head FK dropped without a DB-level replacement given the retained runtime DML privilege. repo_event_chain_heads has a direct runtime GRANT SELECT,INSERT,UPDATE to striatumd_rw and is classed ClassRuntimeDML (not SD-gated); the FK was the only DB guard against a direct runtime UPDATE pointing last_event_id at a non-existent event. H3's co-transactional Go writer-set + grep/AST guard cannot catch a raw SQL DML statement, so the chain-head referential-integrity guarantee is not preserved."
    affected_invariants:
      - "repo_event_chain_heads.last_event_id references a real events row"
      - "chain-head referential integrity (formerly the DEFERRABLE FK, 0006:80-81)"
    source_refs: ["dialogue:2"]
  - id: F2
    severity: high
    posture: design
    status: open
    challenge: "CORE over-claims integrity preservation. The reshape narrows DB-enforced uniqueness: UNIQUE(row_hash, ts) accepts a duplicate row_hash in a different ts bucket, and PK (repository_id, event_id, created_at) accepts duplicate logical event ids in different partitions — so global audit row_hash uniqueness and per-repo (repository_id, event_id) row identity are no longer database-enforced. The SPEC must either add a coherent in-DB replacement or honestly narrow the CORE to state these guarantees are deliberately dropped per D241 Q3=(a) and name the replacing application/SD invariant."
    affected_invariants:
      - "global audit_log.row_hash uniqueness"
      - "per-repo (repository_id, event_id) row identity"
    source_refs: ["dialogue:2"]
  - id: F3
    severity: critical
    posture: design
    status: open
    challenge: "The event retention purge transition is unreachable in shipped source. 0041's refuse_sealed_event_segment_change raises on any UPDATE where OLD.state <> 'open', and TestSealedEventSegmentIsAppendOnly asserts a sealed segment's UPDATE ... SET retention_state='purged' must fail. The retention executor therefore cannot record the purge act that distinguishes a policy-driven partition DROP from tampering. H6 over-claims event-side seal-before-drop parity with audit; the SPEC specifies neither the reachable state transition, its DDL placement (runtime migration vs owner-bundle step vs paired expand/contract), nor a fail-closed check so the owner DROP cannot run before the runtime purge-evidence path exists."
    affected_invariants:
      - "partition_dropped_without_sealed_segment doctor invariant"
      - "retention act is auditable and daemon-mediated"
      - "owner/runtime DDL ownership boundary (D187/#244)"
    source_refs: ["dialogue:3"]
  - id: F4
    severity: medium
    posture: design
    status: open
    challenge: "H7's #386-subsumption language over-claims by including audit_log, which D241 pins as partitioned-but-never-dropped (infinite retention; P4's live DROP path is events-only). The subsumption claim must narrow to the events retention-DROP path; audit_log gets partitioning read/VACUUM benefits and keeps #386, with no live audit DROP path unless a future decision sets a finite audit horizon."
    affected_invariants:
      - "D241 audit_log infinite-retention policy"
    source_refs: ["dialogue:3"]
constraints:
  - id: C1
    posture: design
    severity: critical
    kind: invariant
    binding: true
    source_finding: F1
    final_review_required: true
    text: "Close the chain-head referential-integrity hole at the database level. EITHER add last_created_at to repo_event_chain_heads and re-declare a real composite FK to events(repository_id, event_id, created_at), OR revoke direct runtime INSERT/UPDATE on repo_event_chain_heads and route the head advance through an SD function/trigger that verifies the target event row under lock. The writer-set guard must cover the SQL privilege surface, not only Go call sites."
    source_refs: ["dialogue:2"]
    verification:
      gate: "pgtest: after the SQL FK is removed, a direct runtime-DML UPDATE of repo_event_chain_heads.last_event_id to a non-existent event is rejected; a build guard asserts the SQL write-privilege surface of the table, not only Go writer sites"
      expected_stage: "rfc-0136-build P2 (events reshape, owner bundle >= 0022)"
  - id: C2
    posture: design
    severity: high
    kind: schema
    binding: true
    source_finding: F2
    final_review_required: true
    text: "Resolve the CORE integrity over-claim. EITHER add coherent in-DB replacements for the dropped narrow uniqueness (an unpartitioned row_hash registry or deferred constraint-trigger for global audit row_hash; an unpartitioned (repository_id, event_id) identity registry), OR narrow the CORE claim to state explicitly that the reshape deliberately drops DB-enforced global row_hash uniqueness and per-repo event_id row identity per D241 Q3=(a), naming the exact application/SD-sole-writer invariant that replaces each."
    source_refs: ["dialogue:2"]
    verification:
      gate: "pgtest: a duplicate audit row_hash across partitions and a duplicate (repository_id, event_id) across partitions are either rejected by the replacement guard, or the CORE is narrowed and the replacing invariant is named and tested for"
      expected_stage: "rfc-0136-build P2/P3 (events + audit_log reshape)"
  - id: C3
    posture: design
    severity: critical
    kind: gate
    binding: true
    source_finding: F3
    final_review_required: true
    text: "Specify the event retention state machine and its cross-plane DDL placement. Define whether retirement updates event_chain_segments.state, retention_state, or an append-only partition-purge ledger; make the sealed->purged/retired transition reachable for a sealed segment without reopening arbitrary sealed-segment mutation (the current 0041 trigger forbids it); state whether that schema change is a runtime migration, an owner-bundle step, or a paired expand/contract step; and add a fail-closed capability check so the owner partition DROP cannot run before the runtime purge-evidence path exists. Assign the full runtime-purge-evidence + owner-DROP + doctor sequence to 0142 P5's expand/contract rehearsal."
    source_refs: ["dialogue:3"]
    verification:
      gate: "pgtest+doctor: a sealed segment is retired exactly once; a non-sealed or boundary-straddling segment cannot be retired; doctor reds on a dropped partition with no matching purged-segment evidence; the owner DROP fails closed without the runtime purge-evidence path"
      expected_stage: "rfc-0136-build P4 retention executor (rehearsed under RFC 0142 P5)"
  - id: C4
    posture: design
    severity: medium
    kind: policy
    binding: false
    text: "Narrow H7's #386-subsumption language to the events retention-DROP path. State that audit_log is partitioned-but-never-dropped (infinite retention, P4 DROP path events-only per D241) and receives only the partitioning read/VACUUM benefits while retaining #386, with no live audit DROP path unless a future decision sets a finite audit horizon and reopens the audit purge design."
    source_refs: ["dialogue:3"]
branches:
  design: blocked
---

# Collaboration Ledger — RFC 0136 design run (cycle 1)

author: adjudicator-author-001

> Adjudication of the cycle-1 dialogue trajectory for the `rfc-0136-design`
> `falsification_gate` run. Inputs read in full: the Holder SPEC
> (`dialogue/holder/HOLDER.md`), both falsifier challenges
> (`dialogue/falsifier_1/FALSIFIER.md`, `dialogue/falsifier_2/FALSIFIER.md`),
> the run `SEED.md` charter, and RFC 0136 as context. The falsifiers' three
> load-bearing source anchors were re-verified against the worktree at the run
> branch (they are durable provenance, not raw provider logs / private
> diagnostics): (1) `repo_event_chain_heads` carries `GRANT SELECT, INSERT,
> UPDATE` to `striatumd_rw` (`0006_events_chain_anchors.sql:109`) and is classed
> `ClassRuntimeDML` (`write_authority_inventory.go:51-55`); (2) the chain-head
> FK is at `0006:80-81` (confirms the Holder's C-2 correction); (3) the 0041
> trigger `refuse_sealed_event_segment_change` raises on any UPDATE where
> `OLD.state <> 'open'`, and `TestSealedEventSegmentIsAppendOnly` asserts a
> sealed segment's `retention_state='purged'` UPDATE MUST fail
> (`event_chain_segments_pg_test.go:317-323`). All three confirm the challenges.

## Verdict

**verdict: needs_revision**

Both material challenges landed and **both stand unrebutted by the SPEC as
written**. Each maps directly onto a named `needs_revision` trigger in the
adjudication rubric: an integrity guarantee breaks (F1 — the chain-head FK is
dropped with no DB-level replacement, against a table that retains a runtime DML
privilege; and the CORE silently drops two DB-enforced uniqueness guarantees),
and the retention path cannot record the chain-safe purge act it depends on (F3
— the sealed→purged transition is forbidden by shipped source) while the SPEC
over-claims that contract and the integrity preservation are build-ready ahead
of the cross-plane state machine and the P5-gated execution.

This is **not** a reject. The hard core of the SPEC is sound and the run was
productive — the gate does not clear only because the *replacement* mechanisms
the reshape substitutes for the constraints it removes (the moved chain-head RI,
the narrowed uniqueness, the segment-purge retention transition) are themselves
incomplete or unspecified. One revision cycle is available: the Holder revises
`HOLDER.md` to discharge C1–C4 below, and the falsifiers re-attack the revised
SPEC. A second `needs_revision` ends the gate unCleared and routes to the
operator for a fresh `-v2` run with a revising holder.

## Per-claim ledger (the SEED hard core)

| Claim | Status | Note |
| --- | --- | --- |
| **CORE** — reshape forced *and* preserves every integrity guarantee | **NOT HELD (over-claimed)** | The "forces a reshape" half holds (H1); the "preserves every guarantee" half does not — F1/F2 show two DB-enforced guarantees dropped and the chain-head FK not equivalently replaced. The claim must be narrowed or backed by compensating guards (C1, C2). |
| **H1** — reshape forced & partition-legal | **HELD** | Correct PostgreSQL behavior; both falsifiers concede it. The forced PK/UNIQUE shapes and the already-NOT-NULL partition columns are right. |
| **H2** — only the chain-head FK is the inbound-FK casualty; outbound FKs unaffected | **HELD (consequence flows to H3)** | The FK inventory and catalog-query proof obligation are sound. But the *consequence* — the head stores only `(repository_id, last_event_id)`, which after the reshape is no longer a unique row reference — is what F1/F2 exploit. |
| **H3** — drop chain-head FK, move RI to co-transactional Go/SD | **NOT HELD** | The three-writer co-transactionality (and C-4 honesty about it) is good, but the safety basis is incomplete: `repo_event_chain_heads` retains a runtime SQL DML privilege a Go writer-set guard cannot police (F1). |
| **H4** — hash chains byte-invariant | **HELD** | Correct by construction; partitioning changes no hash input. Uncontested. |
| **H5** — triggers/routing survive; partition DROP trigger-clean | **HELD** | Correct: row triggers propagate to partitions; DDL DROP does not fire row triggers. Uncontested. |
| **H6** — partition droppable only after sealed segment + boundary hashes | **NOT HELD** | The *rule* is right, but the claim of event-side seal-before-drop *parity with audit* is false in shipped source: the sealed→purged transition is forbidden (F3). |
| **H7** — partition DROP subsumes #386 retention RI-scan | **PARTIALLY HELD** | The events-side subsumption is correct *given C1 closes the inbound-FK premise*. The headline language over-claims by including `audit_log` (never-dropped per D241) — narrow per C4. |
| **H8** — owner DDL, ordinal ≥ 0022, capability-parity, never runtime migration | **HELD (for the reshape itself)** | The ordinal discipline and the verified C-1/C-3 corrections are strong. Incomplete only for the retention state-machine schema change F3 surfaces, whose cross-plane placement is unspecified (folded into C3). |
| **H9** — backfill form A; live cutover rehearsable only via 0142 P5 | **HELD (cutover) / under-specified (retention)** | The P5-gating of the live ~14M-row cutover lock-window is honest and well-drawn. But H9 under-specifies *what* P5 must rehearse — it omits the full retention-state sequence (C3). |

## Per-falsifier judgment

### Falsifier 1 (reshape / chain-integrity lens) — **STANDS (material)**

Three sub-challenges, all landing:

1. **Runtime-DML hole (the sharpest) — STANDS, source-verified.** Dropping the
   chain-head FK and relying on a Go grep/AST writer-set guard does not close the
   DB-level hole, because `repo_event_chain_heads` retains `GRANT SELECT, INSERT,
   UPDATE` to the runtime role (`0006:109`) and is classed `ClassRuntimeDML`
   (`write_authority_inventory.go:51-55`). A raw `UPDATE … SET last_event_id =
   <nonexistent>` is permitted by privilege; today only the FK rejects it. H3's
   guard polices Go call sites, not the SQL privilege surface. → **C1**.
2. **Narrowed-uniqueness loss — STANDS.** `UNIQUE(row_hash, ts)` accepts a
   duplicate `row_hash` in a different `ts`; PK `(repository_id, event_id,
   created_at)` accepts duplicate logical event ids across partitions. The
   reshape genuinely removes two DB-enforced guarantees. The RFC itself resolved
   this as D241 Q3=(a) (accept the sole-writer invariant), but the Holder SPEC
   neither carries that acceptance into the CORE nor adds a compensating guard —
   so the CORE's "preserves *every* existing integrity guarantee" over-claims.
   → **C2**.
3. **Row-identity for the head value — STANDS** (a consequence of #2): the head
   stores only `(repository_id, last_event_id)`, which is no longer a unique row
   reference post-reshape; H3's "cross-partition resolution" test proves append
   works, not that the stored value still names one row. Folded into C1/C2.

The Holder's strongest available rebuttal — "the sole-writer/SD path makes the
duplicate operationally infeasible, and a duplicate `row_hash` is a hash break" —
is true but *lowers the guarantee from database-enforced to application-enforced*.
For an RFC whose load-bearing promise is preserving DB-enforced integrity through
the reshape, that is exactly the gap the falsifier names. Not rebutted.

### Falsifier 2 (owner-DDL / retention / P5-dependency lens) — **STANDS (material)**

1. **Retention purge unreachable (the load-bearing one) — STANDS,
   source-verified.** The 0041 trigger `refuse_sealed_event_segment_change`
   raises on any UPDATE of a non-open segment, and `TestSealedEventSegmentIsAppendOnly`
   asserts the `retention_state='purged'` UPDATE on a sealed segment must fail.
   So H6's claimed event-side "same seal-before-drop capability the audit chain
   has" is contradicted by shipped source: a sealed event segment cannot be
   marked purged to witness a partition DROP. The retention act is therefore not
   auditable as the daemon-mediated operation H6/H7 require, and the SPEC names
   neither the reachable transition, its runtime-vs-owner-bundle DDL placement,
   nor a fail-closed sequencing check — the precise owner/runtime plane that
   D187/#244 punishes. → **C3**.
2. **Audit-log over-claim — STANDS (minor).** H7's "events/audit_log …
   DETACH/DROP" framing contradicts D241's audit-infinite-retention policy.
   → **C4**.

The Holder's available rebuttal — "P4 may add the executor / a purge ledger /
an allowed sealed→purged transition later, and the live cutover is honestly
P5-gated" — does not clear the gate: the adjudicator must score whether **P0
pins the retention contract the build run will execute**, and right now that
contract depends on a transition current source forbids and a cross-plane DDL
rule the SPEC has not named. "P5 will make purged reachable" is not a
build-ready specification. Not rebutted.

## Credited strengths (the revision must build on these, not regress them)

- **H1, H4, H5 are correct and uncontested** — the forced reshape, hash-chain
  byte-invariance, and trigger/routing survival + trigger-clean DROP are the
  sound spine the revision keeps.
- **The source re-verification is excellent and is credited in full.** The
  Holder's corrections C-1 (owner `0020` taken, `0021` reserved → ordinal
  ≥ `0022`), C-2 (chain-head FK at `0006:80-81`, not `:72`), C-3 (guard at
  `migrations_test.go:643`, not `:423`), and C-4 (three co-transactional writers,
  not a "sole" writer — the *stronger* basis) were all re-confirmed here and
  materially improve on the RFC text. Owner-DDL discipline and capability-parity
  framing (H8) are right for the reshape itself.
- **The P5-dependency boundary for the live cutover (H9 / §9) is honest** and is
  the model the revision should extend to the retention state machine (C3),
  rather than narrow.

## Constraints carried into the v1 revision

The four constraints in the front matter are the binding/advisory carry-forward.
C1–C3 are **binding** and gate the next clearing attempt; C4 narrows an
over-claim. The revised `HOLDER.md` must discharge each, and the falsifiers
re-attack the revision. Summary:

- **C1 (critical, binding)** — close the chain-head RI hole at the DB level
  (composite FK with `last_created_at`, or revoke runtime DML + SD-gate the head
  advance); the writer-set guard must cover SQL privileges.
- **C2 (high, binding)** — resolve the CORE over-claim: add in-DB replacements
  for the dropped narrow uniqueness, or honestly narrow the CORE to the D241
  Q3=(a) acceptance and name the replacing invariant.
- **C3 (critical, binding)** — specify the reachable event retention state
  machine, its cross-plane DDL placement, and a fail-closed owner-DROP gate;
  assign the full purge-evidence sequence to 0142 P5's rehearsal.
- **C4 (medium, advisory)** — narrow H7's #386 subsumption to the events-only
  retention-DROP path; state audit_log is partitioned-but-never-dropped.

## Local-first boundary

Held. Nothing in this adjudication introduces hosted services, cloud APIs,
telemetry, durable transcript capture, or external persistence. The required
repairs (in-DB guards/registries, an SD-gated head advance, a runtime/owner
retention state machine, the ephemeral 0142-P5 rehearsal clone) all stay within
the daemon-owned local PostgreSQL boundary.
