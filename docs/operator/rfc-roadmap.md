# RFC Roadmap — sequenced, themed, "do the next one"

Living doc. Last triaged: 2026-07-01. Owner: operator. This orders every RFC
that is **proposed, accepted-but-unbuilt, or partially implemented** into a
single execution sequence. Items not listed are `accepted / implemented`
(shipped) or superseded/closed-out.

When an item ships, mark it ✅ and move the wave boundary down; the sequence
numbers are stable so "do the next one" always resolves to the lowest-numbered
unshipped item whose blocker is clear.

---

## How an item ships: Design → Build → Verify

Every roadmap item passes through **three Striatum workflows**, in order, before
it reaches `main` and a deploy. Do not hand-implement an RFC; drive it through
the runner so the provenance is real (AGENTS.md: "anything needing an RFC also
gets a Striatum workflow").

1. **Design workflow — harden the spec into falsifiable acceptance criteria.**
   Scaffold a `falsification_gate` (or `implementation_panel` / `committee`)
   design-run seeded with the RFC. A holder lane proposes the spec; independent
   cross-model falsifier lanes attack it; an independent adjudicator ratifies it
   into *binding, verification-gated constraints*. Output: an accepted design
   with acceptance criteria + a recorded decision (`D###`). **Skip only when the
   RFC is already `accepted` with concrete criteria** (the "Design" cell below
   says `done` for those — go straight to Build).

2. **Build workflow — implement the ratified design in reviewed slices.**
   Scaffold a `code_change` run (draft → review → apply) per slice. The author
   lane builds; an independent reviewer lane returns `accept_with_findings` or
   `needs_revision`; the daemon integrates the accepted slice onto the run
   branch. One slice = one tracer-bullet vertical cut. The design's acceptance
   criteria are the reviewer's checklist.

3. **Verify workflow — mint an independent sealed receipt, then ship.**
   `striatum verifier run` mints sealed `go-build` / `go-vet` / `go-test`
   receipts (RFC 0134/0141) — the **second key**; the executing agent may not be
   its own verifier. Land to `main` (daemon run-integration for lane work, or a
   sync-guarded direct commit for operator-class fixes), confirm **CI green**,
   then deploy on the **next quiescent daemon restart** (never restart while
   design dogfoods are live — fixes are ancestors of `main` and auto-apply).

### "Do the next one" — operator protocol

1. `striatum operator bootstrap --markdown` (cold start; follow `next_actions`).
2. Open this file. Find the lowest-numbered item **not** marked ✅ whose
   **Blocked-by** is satisfied. That is "the next one."
3. Run **Design** (unless `Design: done`) → **Build** → **Verify** for it.
4. On ship: mark it ✅ here, update its tracking issue, note the deploy in
   `docs/operator/BRIEF.md`, and bump the "Last triaged" date.
5. Respect **in-flight** items (🔵) — a design dogfood is already running for
   those; monitor/continue it rather than starting a second run for the same RFC.

### Themes

- 🛡 **Reliability** — keeps self-hosting runs alive, correct, and recoverable.
  These break live dogfoods today, so they lead the sequence.
- ✨ **Feature** — new product surface / capability. Sequenced after the
  reliability spine is solid.

### Audit Closeout Budget And Subtraction Gate

Decision D264 is the current operator rule for the 2026-06-24 architecture-audit
closeout.

- **Red-doctor budget:** while `striatum doctor` is red, do not start new
  feature-wave RFC design/build work. Work may continue only when it reduces
  integrity risk, recovery risk, source-of-truth drift, or an explicit audit
  closeout ambiguity.
- **RFC/WIP cap:** after doctor is green, start at most two new in-flight design
  runs per wave before one ships, is canceled, or is explicitly quarantined.
  Existing in-flight Wave 0/Wave 1 reliability designs are grandfathered: finish
  or cancel them before launching more.
- **Self-hosting tax vs. adopter-critical reliability:** classify each new
  reliability item before sequencing it. Self-hosting tax protects Striatum's
  own dogfood economics, such as many concurrent lanes on one shared host.
  Adopter-critical reliability blocks a fresh target repository/operator on the
  documented single-box path. Example classifications: RFC 0166 is self-hosting
  tax until second-adopter evidence appears; RFC 0142 P4 and RFC 0133 fan-in are
  adopter-critical because they protect schema deployment and final tree
  correctness.
- **Feature-wave fuse:** v2.38.0 closes the audit subtraction-release checklist:
  README/front-door truth is mechanically guarded by `scripts/check_release_version.py`
  through `make check-docs`, and #598/#599/#600/#602/#603/#604/#605/#606/#607/#608
  are closed. Wave 4 still requires a green `doctor`, current README/brief/roadmap
  truth, and an explicit product reason in this file before new feature-wave work
  starts.

---

## The sequence

> **2026-06-24 design fan-out — banked & resume-ready (⏸).** Six reliability/security
> design runs (0136, 0164, 0165, 0166, 0168, 0169) were canceled when their operator
> departed mid-fan-out; `striatum doctor` is green. Each run's dialogue (HOLDER design +
> falsifier challenges + cycle-1 adjudicator ledger) is durably preserved on origin at
> `backup/rfc-<id>-<vN>-2026-06-24`. The cycle-2 adjudicator verdicts did **not** survive
> (revision-budget-exhaustion wedge, #587; cluster B 0136/0169 were blocked one stage
> earlier on cross-user worktree perms, #612 — their prepared falsifier_2 was salvaged into
> the backup). To pick one up: re-scaffold a fresh `-vN+1` run seeded from the banked
> dialogue rather than re-running from scratch.

### Wave 0 — In flight (finish what's running; do **not** start a second run)

| # | RFC | Theme | What it is | Stage | Track |
|---|---|---|---|---|---|
| 1 | **0142 P4** | 🛡 | One-shot `striatum daemon deploy` — make it the only schema mutator; lift auto-apply out of serve-boot; revoke serving-role DDL | ✅ **BUILT + LANDED** (`7a63d8a2`, 2026-06-24; migration 0044 `deploy_cursor`/`deploy_plan`/`deploy_receipt`, owner.go M2 surface, shadow-first default-OFF; D262 design + sealed go-build/vet receipts). Activation/verify run (arm `STRIATUM_DEPLOY_DECOUPLED` + B1/D1′/D6) is the follow-up; close #571. | #571 |
| 2 | **0143** | 🛡 | Lane credential survival across a daemon boot-epoch rotation (reseal without the owner-only client-token) | ✅ **Slice B build accepted + apply-verified** (`run_20d2fb3e999d1b5ae4e5de6b180d86a3`, 2026-06-29): Slice A (legible `session_unrecoverable_across_rotation` floor, pure daemon-side observability) **BUILT + LANDED** (`a6d5610f`, 2026-06-24; daemon-observed stale-epoch event → typed recovery class, observability-only / RFC-0168-bounded; v1+v2 gates converged the design; sealed go-build+go-vet receipts). RFC 0168 P0 is integrated; Slice B adds daemon-internal `CapabilityReseal` gated by active lane UID lease id/generation, with stale/missing/sibling/foreign-run/beyond-grace cases falling closed to the typed floor. | #512 |
| 2b | **0168** | 🛡 | Per-lane OS uid (pooled) as the lane security principal — dissolves the shared-uid `BC1-W1-ORACLE` wall the 0143 gate hit; **RFC 0143 Slice-B prerequisite** | ✅ **P0 BUILD v3 ACCEPTED + INTEGRATED** (`run_aa4e1c988eddb78b255afa0e63a75e6c`; merge `b7f48ab1`, follow-up `42d9579c`, 2026-06-28): runtime schema **47** adds `lane_uid_leases`; owner bundle **0023** reasserts authority; supervise.start can allocate from `STRIATUM_LANE_UID_POOL`, binds lease generation into env/metadata, and fails control/attestation/reporting closed on generation mismatch; uid return is blocked until S1-S3 cleanup plus P1-P5 proof is clean; private MCP bearer files live under `.striatum/scratch/<supervisor_id>`; provider-owned credential selectors inside the repo, including relative paths resolved from the lane launch root, are refused while ordinary lane env (`AGY_HOME`/`FIXTURE_CONFIG_DIR`) still launches; per-job worktrees/workspaces receive the selected lane user's ACL; doctor/recovery surface quarantined/stuck uid leases. | #512 / RFC 0143 Slice B blocker closed |
| 3 | **0165 / 0173** | 🛡 | Claude provider-cred freshness + spawn-time projection (supervisor side; complements the host cred-resync timer) | 🔵 **RFC 0165 accepted (D277); RFC 0173 build RFC proposed** (`run_efde0bcac1a8712b90c94e22e9f5db97`, integrated at `0fe4f398`, 2026-06-30): cycle-3 ledger `docs/operator/artifacts/rfc-0165-design-v5/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_3.md` returned `accept_with_findings`; proposal `docs/operator/artifacts/rfc-0165-design-v5/commit/proposal/PROPOSAL.md` remains the accepted design input. RFC 0173 now defines the successor `rfc-0165-build` contract: generated projection contract, pure pre-spawn planner/refusal witness, access-token-only projection, immutable launch binding, provider-auth debt, and redacted operator surfaces. Source build remains unstarted. Kept **separate** from 0169 per operator. | #583 |
| 3b | **0169** | 🛡 | Provider-agnostic lane credential-readiness spine — subsumes 0121/0162/0165 as assurance classes; spawn-fresh placement closes #583 by construction (converge Claude onto agy's daemon-minted model) | ⏸ **design v1 banked — run canceled 2026-06-24** (operator left mid-fan-out; falsifier_2 blocked on cross-user worktree perms #612 — prepared challenge salvaged into the backup; dialogue at `backup/rfc-0169-design-2026-06-24`; resume via fresh `-v2`) — `falsification_gate` proving the registry refactor is behavior-preserving + spawn-fresh placement structurally closes #583 without CLI modification; Layer 3 tamper-proof vs untrusted lane | #583 |

### Wave 1 — Stop the bleeding (lane-health reliability that wedges live runs)

| # | RFC | Theme | What it is | Design | Blocked-by | Track |
|---|---|---|---|---|---|---|
| 4 | **0166** | 🛡 | Completion deadline for an alive-but-never-completing lane (sealed-progress silence budget) | ⏸ **design v2 banked — run canceled 2026-06-24** (operator left mid-fan-out; dialogue at `backup/rfc-0166-design-v2-2026-06-24`; resume via fresh `-v3`) — v1 ratified the AND-not-OR core, returned `needs_revision` on C1 (novelty-aware clock: one primitive for all reset surfaces, junk-artifact can't move the floor) + C2 (corrected no-false-kill for an alive-working lane); v2 discharges both | #576 |
| 5 | **0162 + #569** | 🛡 | Lane auth silent-failure observability — detect absence-of-success; finish the detection layers + a live game-day | done (MVP shipped) | — | #569 |
| 6 | **0133** | 🛡 | Fan-in deferred-join barrier cutover — live default with `STRIATUM_BARRIER_FANIN=0` kill switch; stage+pin siblings and assemble before downstream join queues | ✅ **BUILT + PG-VERIFIED** (`D269`; source proof only while doctor is red) | live deployment equivalence after doctor green | #527 |

### Wave 2 — Deployment-safety chain (build on Wave 0's P4)

| # | RFC | Theme | What it is | Design | Blocked-by | Track |
|---|---|---|---|---|---|---|
| 7 | **0142 P3-arm** | 🛡 | Arm schema-drift refuse-to-serve (flip `STRIATUM_SCHEMA_DRIFT_REFUSE`) after a clean prod bake | done | one clean prod deploy cycle + P4 (#1) | #578 |
| 8 | **0142 P5** | 🛡 | Rehearsal receipt + expand/contract on an ephemeral two-role clone (highest-risk owner DDL) | done (D258 scope) | P4 (#1) | #572 |
| 9 | **0136** | 🛡 | Range-partition `events`/`audit_log` by `created_at`; partition `DROP` as the retention path | ⏸ **design v1 banked — run canceled 2026-06-24** (operator left mid-fan-out; blocked at falsifier_2 on cross-user worktree perms #612 — prepared challenge salvaged; HOLDER + falsifier_1/2 + cycle-1 ledger at `backup/rfc-0136-design-2026-06-24`; resume via fresh `-v2`) — still gated on P5 | P5 (#8) | #387 |

### Wave 3 — Hardening tail (correctness + security)

| # | RFC | Theme | What it is | Design | Blocked-by | Track |
|---|---|---|---|---|---|---|
| 10 | **0158** | 🛡 | `verified_stale` staleness rung + `verifier resweep --builtins` (needs a sealed version basis + migration) | done (D252); migration sub-decision open | — | #577 |
| 11 | **0164** | 🛡 | Untrusted-substrate hardening — read-side git neutralization + gate-evidence recovery contract | ⏸ **design v2 banked — run canceled 2026-06-24** (operator left mid-fan-out; dialogue at `backup/rfc-0164-design-v2-2026-06-24`; resume via fresh `-v3`) — v1 returned `needs_revision` on C1 (complete git-surface taxonomy: route every funnel incl. `runGitWorktreeCommand`/`integrateGit`, close `recovery_quarantine_lane.go:425` status→fsmonitor RCE + 3 corpus rows) + C2 (benign `[alias]`/`[pager]` never wedges); v2 discharges both | — |
| 12 | **0095** | 🛡 | Revision-safe lifecycle — remaining phases past 1–3 | done (per-phase) | — | — |
| 13 | **0100** | 🛡 | Self-describing artifact contracts — phases past 1 (packet + error ergonomics) | done | — | — |
| 14 | **0113** | 🛡 | Runtime read-scope least-privilege remainder (mostly carried by accepted 0114; confirm residual) | done | re-confirm vs 0114 | — |
| 14b | **0170** | 🛡 | Self-culling repository — the CULL workflow class (detect + shed dead artifacts); the systemic answer to the 2026-06-24 accretion finding. **P0 = `cullable_entity` ledger + read-only Tier-1 (supersession) `DecayTickSweep`, no deletion.** | ✅ **P0 built + verified + integrated** (D271; `run_992bd797fc136f1e3d782f443f9fb2ad`; main `02a15e83`). Runtime schema **45** adds `cullable_entity`; `DecayTickSweep` runs observe-only off the recovery scheduler. Verification: strict verifier build/vet passed with agreement; go-test passed as ASSERTED with exit 0; `make lint`; env-stripped `make typecheck`; `make check-docs`; live two-role PG migration/FK tests. P1 citation exactness blocker **#618** is closed: Tier-1 treats `status: frozen` Markdown records outside `docs/records/_frozen/` as non-live citation sources. P1 deferral remains **#619** (non-cooperative FS-hang cull-slot fence). | — | #615 |
| 14c | **0171** | 🛡 | Operator records blob dockets and virtual records — move generated operator/run bodies to daemon-indexed blob storage while keeping git reviewable through dockets, pointer manifests, and materializable virtual records. | partial (D273; import/materialize/verify + generated-record integrity + generated-record-backed artifact-anchor doctor shipped; dogfood operator-report pilot + 1,755-file operator artifact/workflow Markdown deletion pilot shipped) | broad historical deletion outside explicitly scoped generated-record classes remains blocked; next work is fuller `striatum://record` doc-link resolver coverage plus any separately authorized follow-up deletion pilot | — |

### Wave 4 — Features (once the reliability spine is solid)

| # | RFC | Theme | What it is | Design | Blocked-by | Track |
|---|---|---|---|---|---|---|
| 15 | **0099** | ✨ | Constrained operator mode — control-surface-only AI operator; phases past 1–2 | done | — | — |
| 16 | **0163** | ✨ | Staged-not-adopted offline self-improvement — nightly consolidation that can never ship an unreviewed change | needs design + product decision | — | — |
| 17 | **0052** | ✨ | Committee deliberation workflow shape (arbitration, panels, adversarial review) | needs scheduling + design | — | #403 |
| 18 | **0094** | ✨ | Deferred collaboration shapes — fog-of-war review, synaptic prune, adjudicator reliability | done (slices 1–3); remainder | — | — |
| 19 | **0115** | ✨ | Precise token-usage telemetry for supervised lanes | done | dashboard-ingest landing | #404 |
| 20 | **0067** | ✨ | Optional git + PR integration | — | **product decision first** | — |
| 21 | **0167** | ✨ | Operator identity & run attribution — leased handles over `principal_id` (RFC 0107): `striatum whose`, `status --mine` manifest, write-once run stamp, operator-named handoff files | **P0 built + verified + integrated** (D260/D263; owner bundle **0022**; 4-cycle design + code_change build; 10/10 live two-role pgtests) → **deploy pending quiescent window** (atomic: install new binary + `owner-ddl apply` + restart together — bundle 0022's `runs` REVOKE is coupled to the star-reader conversions). **P1–P3 sequenced** (custody log / honest bylines+handoff naming+chips+OSC title / lineage) | — | deploy + P1–P3 |
| 22 | **0172** | ✨ | Proof-only multi-campaign supervision — campaign arcs, authority receipts, fresh-context replay, deferral quarantine, contradiction reports, and read-only portfolio status for supervising many RFC arcs without treating status as permission | accepted (D275); build not started and no workflow-launch / sequencing / promotion authority granted | reliability spine / roadmap sequencing | — |

---

## Notes & rationale

- **Why reliability leads:** Waves 0–1 are the RFCs that wedge live self-hosting
  runs today (stuck lanes, dead creds, never-completing reviewers, fan-in
  correctness). Every feature in Wave 4 depends on a dogfood loop that doesn't
  stall, so they come last.
- **The two dependency chains:** (a) *deployment safety* — 0142 P4 → P3-arm →
  P5 → 0136 (the big DB reshape only rehearses safely once P5's ephemeral-clone
  rehearsal exists); (b) *lane-health/credential* — 0143, 0162, 0165, 0166, all
  tracing to self-hosting friction.
- **Already done, not on the sequence (optional residual only):** 0042, 0061,
  0062, 0066, 0069, 0070, 0098, 0102, 0119 (runtime evictor deferred), 0130.
- **Closed-out (do not pick up):** superseded/deprecated 0027, 0028, 0039, 0041,
  0049, 0097.
- This roadmap is a snapshot; re-triage when a wave empties or a new RFC lands.
  The authoritative per-RFC status is each file's `Status:` line under
  `docs/rfcs/`.
