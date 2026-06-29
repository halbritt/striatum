# Striatum RFC Disposition Audit

- **Project:** striatum
- **Auditor model:** claude-opus-4-8 (Opus 4.8, 1M)
- **Reference date ("now"):** 2026-06-20 (system date; HEAD `e9d01815`, 2026-06-20 00:05 UTC)
- **Prompt:** `RFC_STATUS_AUDIT.md` (disposition audit — "what should happen to each proposal next")

---

## 0. Audit Basis

**Target.** Repository rooted at the current working directory, `/home/halbritt/git/striatum`, branch `main`, clean working tree (`git status --short --branch` → `## main...origin/main`, no changes). Git repository confirmed; full history available.

**Scope.** Unscoped (full proposal backlog). No `Critical` invariants, `Inputs`, or `Plan-of-record` supplied by the user. Authority: read-only static inspection (default); the only deliberate write is this report.

**Locus of truth (resolved).**
- **Authoritative recorded RFC status:** `docs/rfcs/README.md` (the RFC index table). RFC files carry a per-file `Status:` header; index + header are kept in sync by convention (README §"When an RFC is fully replaced…").
- **Authoritative decision record:** `docs/decisions/decision-log.md` (reverse-chronological; D001–D241). RFC status changes must reflect in both the RFC file and the index.
- **Authoritative live/forward state:** `docs/operator/BRIEF.md` (`status: current`, `brief_2026-06-18_v2.34.1-release`) plus `striatum operator bootstrap`. `docs/reference/roadmap.md` and `docs/reference/todo.md` are **archived** (D232) and now redirect here — they are *not* the live plan.
- **Implementation trackers:** GitHub Issues (10 open as of audit).

**Lifecycle vocabulary (discovered, not assumed).** Status words in use: `proposed`, `accepted`, `accepted/implemented`, `implemented`, `partially implemented`, `mostly implemented`, `superseded`, `deprecated`, `blocked`, plus support-tier graduation `experimental` → `supported` (RFC 0106) for workflow shapes. Decision-log status set: `proposed | accepted | accepted-with-revisions | deferred | rejected | superseded`.

**Coverage.** 141 numbered RFC slots (0001–0141): 139 files in `docs/rfcs/`, 2 frozen under `docs/records/_frozen/rfcs/` (0006, 0059), and **0137 absent from `main`** (see RSA-004). Phase-1 metadata inventory spanned the whole index. Phase-2 deep verification (read-only `rg`/`find`/`git`/`gh` + three parallel read-only sub-agents) covered ~22 non-terminal RFCs and the load-bearing artifacts they name. The ~110 terminal `accepted/implemented` rows were surveyed, not deep-read.

**Commands run.** `git status/log/branch`; `ls docs/rfcs`; `find … -name '*0137*'`; `rg 'PARTITION BY|range.?partition' go/` (→ no matches); `ls go/pkg/db/sql{,/owner}` (runtime migrations to `0040`, owner bundles to `0019`); `rg` for verifier/barrier/collaboration_ledger symbols; `gh issue list/view`. Read: RFC index, decision-log head + D218–D241 + tail, operator BRIEF, archived todo/roadmap stubs. **Not run:** tests, builds, daemon, network, live DB. No edits except this report.

**Inherited input.** A prior RFC status audit was run 2026-06-18 (branch `docs/rfc-status-audit-2026-06-18`; grooming PR #405 merged). Treated as context, re-verified fresh; not cited as truth.

**Assumptions / caveats.** (1) The backlog was *heavily and recently groomed* (D219–D241, 2026-06-18→19) — most non-terminal RFCs already carry an explicit, days-old disposition; this audit's value is verifying those hold and surfacing what the grooming left live. (2) Promote recommendations for 0061/0069/0070 rest partly on RFC implementation notes plus targeted code reads, not exhaustive line-by-line reads — confidence capped accordingly. (3) RFC 0137's exact branch merge/needs_revision state was not fully traced.

---

## 1. Verdict

**`NEEDS_TRIAGE`** — confidence **medium-high**.

In-scope (non-terminal) RFCs: **~28** of 141 numbered slots. Disposition tally: **implement 3** (0136, 0141/#482, 0042), **promote 5** (0094 + 0061/0062/0069/0070 currency), **consolidate/defer-gated 2** (#354 barrier fold, 0096/#87), **legibility 1** (0137), **defer-as-is ~17** (already-recorded deferrals/blocks/optional-phase remainders).

**Strongest reason.** The backlog is *broadly legible and fresh* — it was triaged into the decision log two days ago, and spot-checks against code confirm those dispositions (e.g. RFC 0136 P0 resolved while `events`/`audit_log` remain un-partitioned; RFC 0141 shipped at `experimental` with its security gap filed as #482; the barrier predicate is shared in code with P1/P2 still shadow). It is **not** rotting. But it is **not** clean either: one index row is stale (RFC 0094 lists work that shipped and closed two days ago), one number is a silent hole (RFC 0137 absent from the index with no note), and the grooming deliberately left a small set of genuinely-live items needing a *next action*, not just a status — chiefly the events/audit partitioning (a data-grounded scaling cliff) and the RFC 0141 graduation security blocker.

**Is the proposal set safe to read as the live plan?** Mostly yes — the index plus the decision log accurately describe what is live, with the two exceptions above. A reader who trusts the index would (a) believe RFC 0094's adjudicator extras are still unbuilt, and (b) never learn RFC 0137 exists.

---

## 2. Backlog Inventory And Depth Ledger

Terminal `accepted/implemented` rows (≈110, e.g. 0001–0040 core, 0043–0093 substrate/lifecycle, 0104–0135 reliability/barrier) are out of scope except as cluster context; surveyed, not listed per-row. Below: the non-terminal in-scope set, bucketed by disposition class. Pass depth: `deep` = artifact-verified this audit; `dec` = decision-traced; `survey` = index/RFC-text only.

### 2A. Live / actionable (see Section 3)

| RFC | Recorded status | Standing | Depth | Evidence tier | Residual risk |
|---|---|---|---|---|---|
| 0136 range-partition events/audit | proposed (P0 resolved D241) | live (P0 done, P1+ unbuilt) | deep | static + decision | reshape is highest-risk owner-DDL slice |
| 0141 generatable verification_gate | implemented @experimental (D239) | shipped, security gap | deep | static + decision | #482 forge-able sidecar until gate-side enforcement |
| 0094 deferred collab shapes | **partially impl (#402 deferred)** | shipped (extras landed) | deep | history + static | **index row stale** |
| 0137 prometheus exporter | *absent from index* | in-flight on branch | deep | static + history | exact needs_revision/merge state untraced |
| #354 (RFC 0135/0133 fold) | accepted/impl, P1/P2 shadow | partially folded | deep | static + decision | gated on barrier_assembly dispatcher + #333/#346 |
| 0096 supervised-lane trust (#87) | partially implemented | code landed, gate operator-gated | deep | static | `lane-isolation-check` not in CI; host provisioning |
| 0061 daemon-first web service | partially implemented | shipped, polish remainder | deep | static | promote rests partly on RFC notes |
| 0062 real escalation inbox | partially implemented | shipped (D130) | deep | decision | optional schema strictness |
| 0069 pg-only daemon-global | partially implemented | shipped | deep | static | promote rests partly on RFC notes |
| 0070 daemon client boundary | mostly implemented | shipped | deep | static | legacy cleanup only |
| 0042 run-list workflow identity | proposed (re-scoped D224) | live (small Go-UI fix) | dec | decision | `page.html` lacks `workflow.name` |

### 2B. Defer-as-is (recorded deferral/block/optional remainder — verified fine, no new action)

| RFC | Status | Standing | Depth | Note |
|---|---|---|---|---|
| 0052 committee deliberation | proposed (deferred D225) | live, unscheduled | dec | unblocked; owner defers; no `committee` shape exists |
| 0067 optional git/PR | blocked on product decision | blocked | dec | 7 open product questions; clean boundary |
| 0115 token-usage telemetry | proposed — deferred (D226) | live, gated | dec | depends on dashboard-ingest; product-clean |
| 0098 ACE | implemented (slice-4 deferred D222) | shipped | dec | `supported`; slice-4 needs 2nd consumer |
| 0099 constrained operator | partially impl (Ph1-2) | shipped | deep | Ph3 = harness-level escape, deferred-as-is |
| 0100 self-describing contracts | partially impl (Ph1) | shipped | deep | Ph2 = packet schema + `describe`, deferred |
| 0113 read-scope least privilege | partially impl (R1, D170) | shipped | deep | R2/R3 deferred-by-design; `private_read_denial=false` is intended |
| 0117 worktree/branch ref-safety | accepted (Ph1-4) | shipped | survey | Ph5 doctor projection = follow-up |
| 0066 replay/archive corpus-v2 | implemented (fetch deferred D221) | shipped | dec | augmentation-fetch build-on-demand |
| 0095 revision-safe lifecycle | partially impl (Ph1-3) | shipped | deep | Ph4 folds into #354 |
| 0053/0054/0055/0056 | accepted (Phase A) | shipped | survey | optional polish (vocab renames, SVG, chooser) |
| 0074/0075 | accepted | shipped | survey | richer chooser / tmux-by-default deferred |

### 2C. Done — promote/record only

| RFC | Status | Standing | Depth | Note |
|---|---|---|---|---|
| 0101 robust autonomous exec | umbrella-of-record (D223) | mapped to shipped slices | dec | L1–L5 → lanehealth/adapterconformance/0095/0099/chaos_test |
| 0102 operator attention economy | proposed (folded D219) | resolved by folding | dec | no net-new verb; surfaces exist |
| 0041/0044/0057/0119 Engram cluster | superseded / implemented | cleanly terminal | deep | no `import engram`, no `memory.*` capability; boundary regression-tested |

---

## 3. Per-Proposal Disposition Ledger (ranked, actionable)

**RSA-001 — RFC 0136 (range-partition `events`/`audit_log`): `implement` (split).**
- *Standing:* `live`. P0 policy knobs resolved (`docs/rfcs/README.md:151`; D241, `decision-log.md:35`: weekly granularity, events 3-month / audit ∞ retention). P1+ unbuilt.
- *Deciding fact:* `rg 'PARTITION BY|range.?partition' go/` → **no matches**; the two append-only logs are still single-heap. D241 grounds urgency in measured prod: `events` 14.0M rows / 20 GB, `audit_log` 17.3M rows / 8.8 GB over ~5 weeks, June ~4× May daily, ~14 GB/month and accelerating; un-partitioned `events` is a ~170 GB/yr disk + VACUUM/retention cliff.
- *Evidence tier:* static-implementation-traced + calendar/decision. *Confidence:* high.
- *Smallest next step:* ship **P1 first** — `event_chain_segments` sealing (generalize the `audit_segments` seal/boundary-hash model, Q5) — before the P2/P3 owner-DDL PK/UNIQUE reshape. **Use the next-free owner bundle, not `0016`:** the RFC text still names "owner bundle 0016", which is taken (`0016_verify_job_type.sql`); D241 already flags this stale (latest on main is `0019`). #387 is the tracker.

**RSA-002 — RFC 0141 (generatable `verification_gate`): `implement` #482 (graduation blocker, security).**
- *Standing:* `shipped` at `experimental` (D239, `README.md:155`) with a documented forge-able gap.
- *Deciding fact:* attestation is enforced only at the operator-token **verb** level (lane-context refusal in `verifier attest`); the daemon completion gate read does **not** re-verify it — `go/pkg/verifier/evaluate.go` `EffectiveStatusFromReceipt` caps builtins at ASSERTED but never reads the attestation sidecar, so "a forged sidecar could in principle reach VERIFIED" (D239, `decision-log.md:37`). Filed as #482 (security, ready-for-agent).
- *Evidence tier:* static-implementation-traced + decision admission. *Confidence:* high.
- *Smallest next step:* in `evaluateRunClaimVerification`, read + verify the attestation sidecar at gate-read time and fail-closed to ASSERTED when missing/invalid/stale; this plus a green RFC 0105 fixture graduates `experimental` → `supported`.

**RSA-003 — RFC 0094 (deferred collaboration shapes): `promote` (status currency).**
- *Standing:* `shipped` — but the index row understates it. `README.md:109` still lists "Check-B + ledger `v1.1` + second-adjudicator **deferred**".
- *Deciding fact:* those exact extras shipped under **D240(d)** (`decision-log.md:36`, PR #487) and **#402 is CLOSED (2026-06-19T22:42)**. Code present: `go/pkg/artifactcontracts/collaboration_ledger.go`, `adjudicator_reliability_test.go`, `go/pkg/mutations/constraint_discharge_gate_test.go`, `go/pkg/workflowgenerate/shapes_fog_synaptic.go`.
- *Evidence tier:* history-traced (closed issue) + static-implementation-traced. *Confidence:* high.
- *Smallest next step:* update the index row + RFC header to "implemented (D234 shapes + D240 extras); graduate `experimental`→`supported` pending an RFC 0105 fixture." The *only* genuinely-remaining piece is the support-tier graduation fixture.
- *Post-audit update (2026-06-20, at merge):* **Remediated in this change.** The RFC 0094 file header was already current; the stale `docs/rfcs/README.md` index row was reconciled to it (extras landed, #402 closed; graduation fixture remains).

**RSA-004 — RFC 0137 (striatumd Prometheus exporter): `revise`/record (legibility).**
- *Standing:* in-flight design-run, parked. The index jumps `0136`→`0138` with **no 0137 row and no explanatory note**; no `docs/rfcs/0137-*.md` on `main`.
- *Deciding fact:* `docs/operator/workflows/rfc-0137-design/` (a falsification-gate design-run packet) exists; branches `rfc/0137-striatumd-prometheus-exporter` and `striatum/rfc-0137-design` carry a fully-drafted spec revised "per falsification-gate design run"; the run reached `needs_revision`.
- *Evidence tier:* static + history. *Confidence:* high on the gap; medium on the precise branch state.
- *Smallest next step:* add a placeholder index row — `0137 | proposed (design-run on branch `rfc/0137-…`, needs_revision) | striatumd Prometheus exporter` — so the hole is legible, *or* land/decline it through review. A reserved number silently missing from the index is exactly the rot this audit guards against.
- *Post-audit update (2026-06-20, at merge):* **Resolved on `main` independently of this audit.** RFC 0137 was merged via PR #450 (commit `e3148f19`) concurrent with this run — `docs/rfcs/0137-striatumd-prometheus-exporter.md` now exists and the index carries a 0137 row (status `proposed`). The legibility gap is closed; no further action.

**RSA-005 — #354 / RFC 0135 (shared `(entity, seal)` barrier fold): `defer` (gated) — the strategic consolidation.**
- *Standing:* partially folded. The shared predicate exists (`go/pkg/.../barrier_predicate.go`, guard `TestBarrierPredicateHasNoRefCount`); P4 (quorum) + P6 (run.integrate) flipped live behind kill switches, P5 (revision) already live (`review_generation` *is* the seal), P1/P2 (fan-in deferred-join + recoverable assembly) **stay shadow** (D233, `decision-log.md:43`; corroborated by the operator BRIEF: "owner bundle 0013 unapplied, go-live flips not done").
- *Deciding fact:* `fanInIntegrateRunBranch` (per-completion D206 merge) remains the default fan-in path; the `barrier_assembly` job dispatcher + staging-at-completion wiring is unbuilt. #354 is also gated on #333/#346 (D236, `decision-log.md:40`).
- *Evidence tier:* static + decision. *Confidence:* high.
- *Smallest next step:* land the P1/P2 dispatcher + staging behind a `same-final-tree` equivalence fixture, then flip `STRIATUM_BARRIER_*`. This is the move that collapses four barrier code paths (0132/0095/0108/fan-in) into one — high leverage, but correctly behind its equivalence gate.

**RSA-006 — RFC 0096 (#87 hardened-host lane isolation): `implement`/`defer` (decision needed).**
- *Standing:* code Phases 1–3 landed (env allowlist, session-bound token, work-tree hygiene, `STRIATUM_LANE_OS_USER` `sudo -n -u` launch, socket ACL). The remainder is the **green host-isolation gate**.
- *Deciding fact:* `make lane-isolation-check` exists (Makefile) but is **not wired into CI `release-check`** and requires operator host provisioning (PG-less lane user, passwordless sudo, PG reject rules). README:112 still says "#87 still requires host adoption and a green lane-isolation gate."
- *Evidence tier:* static-implementation-traced. *Confidence:* medium-high.
- *Smallest next step:* decide — wire `lane-isolation-check` into `release-check` as mandatory, or formally record it as operator-provisioned hardening; either way close the #87 ambiguity.

**RSA-007 — RFC 0061 / 0062 / 0069 / 0070 (daemon/web/service boundary partials): `promote` (currency, bundle).**
- *Standing:* `shipped` — the load-bearing daemon boundary is in place for all four (`go/pkg/webservice/`, `/v1/invoke` RPC routing, PG-backed daemon-global reads, empty CLI_ROUTES fallback); the named remainders read as open-ended modularity/cleanup, not missing features. 0062's artifact-only-creation question was closed link-only by D130.
- *Evidence tier:* static-implementation-traced + decision (0062). *Confidence:* medium (promote calls for 0061/0069/0070 lean partly on RFC implementation notes).
- *Smallest next step:* one currency pass — re-status to "implemented (residual = optional polish)" after a focused grep for any remaining registry/Python fallback in error paths.

**RSA-008 — RFC 0042 (run-list workflow identity): `implement` (small).**
- *Standing:* `live`, re-scoped to the Go SSE UI (D224, `decision-log.md:52`; #400 closed). The Python templates it originally named are deleted.
- *Deciding fact:* `go/pkg/webassets/templates/page.html` renders only `run_id` + `branch_name`, no `workflow.name`.
- *Evidence tier:* decision-traced. *Confidence:* high.
- *Smallest next step:* surface `workflow.name` + a workflow link in `page.html`. Low leverage, low cost.

---

## 4. Backlog Strategy

**Three-to-five highest-leverage moves, in order:**

1. **RFC 0136 partitioning (RSA-001).** The single scaling cliff in the backlog, grounded in measured prod growth. Sequence inside it: P1 chain-segment sealing → P2/P3 owner-DDL PK/UNIQUE reshape (the highest-risk slice, on a deliberate owner-bundle cutover) → P4 retention executor. Do P1 before any reshape; renumber the owner bundle off the stale `0016`.
2. **RFC 0141 #482 (RSA-002).** A security graduation blocker with a clean, bounded fix (gate-side attestation read). Unblocks `experimental`→`supported`.
3. **A documentation-currency pass (RSA-003 + RSA-004 + RSA-007).** Cheap, high-trust-preservation: promote the stale RFC 0094 row, give RFC 0137 a legible index placeholder, and bundle the 0061/0062/0069/0070 promotes. This keeps the freshly-groomed index honest before the next planning cycle — the whole reason the backlog reads as healthy.
4. **#354 barrier fold (RSA-005).** The structural consolidation (four barrier paths → one). Correctly gated; schedule the P1/P2 dispatcher + equivalence fixture when barrier work is next prioritized.
5. **RFC 0096 #87 decision (RSA-006).** Close the last self-hosting hardening ambiguity with an explicit "mandatory-in-CI vs operator-provisioned" call.

**Dependency order.** 0136 P1 → P2/P3 (owner-DDL). #354 P1/P2 → its equivalence fixture → kill-switch flip. 0141 #482 → RFC 0105 fixture → graduation. The currency pass and RSA-008 are independent and can land anytime.

**Clusters.**
- *Barrier/lifecycle:* 0133/0135/0132/0095/0108/0126 → converge via **#354** (one `(entity, seal)` primitive). Do not let 0132/0095/0108 drift back to per-mechanism keying.
- *Verification:* 0134 (gate, done) + 0141 (shape, experimental) → graduate together once #482 lands.
- *Engram/memory:* 0041/0044/0057/0119 — **terminal, leave alone**; the augmentation-not-dependency boundary is intact and regression-tested (no `import engram`, no `memory.*`).
- *Operator/UX:* 0042/0102/0050 — 0102 folded, 0042 a small fix; cluster is essentially closed.

**What to leave deferred-as-is (do not re-open absent a trigger):** 0052, 0067, 0115, 0098 slice-4, 0099 Ph3, 0100 Ph2, 0113 R2/R3, 0117 Ph5, 0066 fetch, 0053/0054/0055/0056/0074/0075 polish. Each carries a recorded deferral with a named re-open trigger.

---

## 5. Gated Verification And Residual Risk

- **RFC 0061/0069/0070 promote (RSA-007):** confirmed shipped via targeted code reads, but "fully implemented" leans partly on RFC implementation-note prose rather than exhaustive line reads. Lower the promote to "implemented (residual polish)" only after a focused grep confirms no live registry/Python fallback in error paths.
- **RFC 0096 #87 (RSA-006):** the host-isolation gate's CI wiring and host provisioning state could not be fully confirmed read-only (no `.github` CI reference to `lane-isolation-check` found). Needs a maintainer fact: is the gate meant to be mandatory?
- **RFC 0137 (RSA-004):** the precise branch state (needs_revision vs merge-conflict-parked) was not traced; the legibility gap itself is certain.
- **Survey-only coverage:** ~110 terminal `accepted/implemented` rows and the Phase-A doc RFCs (0053–0056, 0074, 0075) were not deep-read; if any silently regressed, this audit would not catch it. The recent grooming makes this low-risk but non-zero.
- **No execution:** all standing/shipped calls rest on static + history + decision evidence; nothing was run.

---

## 6. Rejected / Held Candidates

- **Deprecate-on-age:** none. No in-scope RFC should be deprecated purely because it is old or carries an elapsed informal "someday" — the only date-driven item (RFC 0136 retention) is a *resolved policy*, not a deprecation. The grooming already deprecated the one genuinely-overtaken proposal (0049, by RFC 0088).
- **"Looks unused, delete it":** explicitly rejected by D231 for `crossrepo`, the one-shot migration RPCs, the auto-finalize circuit breaker, and `conversation.*` — each was falsified as load-bearing or near-term on inspection. Not re-litigated here; cutting any needs a dedicated zero-consumer evidence pass.
- **Confirmed deferred/grandfathered fine:** 0052 (D225, unblocked but owner-deferred), 0067 (blocked on a real product decision with a clean read-only boundary), 0115 (D226, no consumer yet), 0098 slice-4 (D222, one writer no reader), 0066 fetch (D221), 0099 Ph3 / 0100 Ph2 / 0113 R2-R3 (scoped-future by design). Each has a recorded re-open trigger; none is rotting.
- **0094 not held:** its remainder shipped (RSA-003) — the only "deferred" thing left is the support-tier graduation fixture, which is real future work, not a held candidate.

---

*End of report. Read-only audit; no RFC, status record, or code was modified. The only write is this file.*
