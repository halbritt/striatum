---
type: record
status: frozen
owner: OPUS
expires: null
---

# Striatum Decision-Record Currency Audit

- **Auditor model:** claude-opus-4-8
- **Reference date ("now"):** 2026-06-16 (system date; no override supplied)
- **Report path:** `STRIATUM_DECISION_RECORD_AUDIT_OPUS_4_8_2026-06-16.md` (invoking directory)
- **Verdict:** `STALE` · confidence `medium`

---

## Remediation Addendum (applied 2026-06-16, after the read-only audit)

The maintainer granted edit authority to correct the findings. All fixes were
**doc-only** (RFC index + RFC file headers + one todo item); the decision log
proper was left unchanged (its supersession/dated-commitment hygiene was already
sound). Applied across two commits (`c0eb28e0`, `ff0cab59`), `make check-docs`
green:

- **DRA-001/002/004 (shipped-but-proposed):** RFC 0073, 0108, 0129 index +
  headers advanced `proposed → accepted / implemented`.
- **DRA-003 (header↔index split):** RFC 0087 index advanced to match its header
  (`accepted / implemented; D199`).
- **DRA-005/006 (stale headers):** RFC 0083/0084/0085/0086 headers
  `proposed → accepted (D139/D141/D143/D144)`; 0044 header records the landed
  Striatum-side V1 (D100).
- **DRA-007 (index overstated):** RFC 0069/0070 index `implemented →
  partially/mostly implemented` (matched the conservative header).
- **DRA-008 (expired-commitment):** todo.md item 68 `🟡 …blocked → ✅ done`;
  narrative tense corrected to historical.
- **DRA-009 — corrected on remediation.** The "22 header-less files" claim was a
  **grep artifact**: those files are not header-less — they carry status in
  `**Status:**` (bold inline) or `## Status` (H2) forms my `^Status:` sweep did
  not match. No spurious `Status:` lines were inserted (the two backfill agents
  correctly refused). The *real* latent defect the broader form-agnostic sweep
  then exposed was **header↔index lifecycle conflicts** the original audit
  missed: RFC **0047, 0051, 0053** (`proposed` headers vs `accepted` index) and
  **0095, 0096, 0098, 0100, 0101** (`proposed` headers vs `partially
  implemented` index) — all eight headers aligned to the index. The missing
  **0042** index row was added. The duplicate **0050** RFC number was
  **deliberately not renumbered** (cross-reference risk; warrants its own
  hygiene pass / decision).
- **Final state:** a full-corpus, form-agnostic sweep reports **0 residual
  header/index lifecycle conflicts** and **no missing index rows**.

The original ranked findings below are preserved as the as-audited record.

---

## 0. Audit Basis

- **Target:** this repository root (`striatum`; default — no other root named).
- **Scope:** whole decision corpus — no scope slug. Prioritised by exposure: the
  RFC status index (`docs/rfcs/README.md`), the decision log
  (`docs/decisions/decision-log.md`), and dated/event-triggered commitments in
  `docs/reference/{todo,spec,roadmap,prd}.md` and `docs/operator/BRIEF.md`.
- **Critical inputs:** none supplied by the user.
- **Repository state:** branch `main`, HEAD `9b2f2874`, tree clean except one
  untracked file (`STRIATUM_RUN_RETROSPECTIVE_DI_RUN2_OPUS_4_8_2026-06-16.md`,
  out of scope). Git history readable; `git log/show/grep` used freely.
- **Authority:** read-only. File reads, `rg`, `find`, and read-only git
  (`log`, `show`, `grep`). No edits, builds, tests, services, network, or
  commits. The only deliberate write is this report.
- **Conventions discovered (used as the grading rubric, not imposed):**
  - Decision log statuses: `proposed | accepted | deferred | rejected |
    superseded`. In practice only `accepted` (195) and `superseded` (11) appear
    across 206 rows (D001–D205).
  - Supersession rule (decision-log header + D118): mark a fully-replaced row
    `superseded` and **name the successor** (D-id / RFC / commit); for partial
    supersession keep the status reflecting the live part and name the
    successor.
  - **RFC dual-record rule (decision-log header):** "RFC status changes must be
    reflected **both** at the top of the RFC file **and** in
    `docs/rfcs/README.md`." A header↔index disagreement is therefore a
    first-class currency defect, not cosmetic.
  - RFC index statuses are a rich free-text vocabulary (`proposed`, `accepted`,
    `accepted (Vn)`, `implemented`, `partially implemented`, `superseded`,
    `shelved`, plus `(Dnnn)` anchors).
- **Files read (representative):** `docs/rfcs/README.md`; RFC headers for all
  ~128 `docs/rfcs/[0-9]*.md`; `docs/decisions/decision-log.md` (full ID/Status
  extraction + targeted row reads); `docs/reference/{todo,spec,roadmap,prd}.md`;
  `docs/operator/BRIEF.md`; `CHANGELOG.md` (targeted); `go/` source existence
  probes; `git log` for RFC 0073/0087/0108/0129.
- **Commands run:** status extraction via `awk -F'|'` on both tables; per-RFC
  `Status:` header reads; `rg`/`find` existence probes in `go/`;
  `git log --oneline --all | rg` for RFC commit traces; `git ls-files 'src/*'`
  and `git ls-files '*.py'` for the Python-removal check;
  `sed -n` for cited lines. No mutating commands.
- **Inherited inputs:** four parallel read-only sub-audits (proposed-RFC
  reality, header/index sweep, supersession integrity, dated commitments). All
  promoted findings were **re-verified directly** before ranking. One inherited
  claim was **rejected** on re-verification (see §4 note on D081).
- **Assumptions:** the system date 2026-06-16 is authoritative "now"; the two
  remaining tracked `*.py` files (both under `scripts/`) are out of RFC 0078's
  `src/` deletion scope.
- **Access limits / residual risk:** RFC bodies were surveyed, not all
  deep-read; implementation claims are **existence/history-traced** (file +
  commit present), not behaviour-verified. Live daemon/prod runtime state
  (e.g. `STRIATUM_AUTO_SPAWN_SCHEDULER`) was out of authority. These cap
  confidence at `medium`.

---

## 1. Verdict

**`STALE` · confidence `medium`.** Findings: **4 SERIOUS, 4 MINOR, 1 NOTE-grade
cluster.** No `BLOCKER`.

**Strongest reason:** the **RFC status index lags implementation**. At least
three RFCs marked `proposed` have fully shipped (0108 — five phases through
v2.23.0; 0073 — live daemon-doctor blob parity; 0129 — merged frame library
graduated `supported` by accepted D199), and the index disagrees with RFC file
headers in several more places (0087 inverse; 0083–0086 headers; 0069/0070
overstated). For the RFC-index decision family specifically, a reader cannot
trust "proposed" to mean "not built" without cross-checking code/CHANGELOG —
that is substantial currency rot on a **primary onboarding surface**.

**Is the live decision map safe to rely on?** *Split.* The **decision log
proper is sound** — supersession integrity is clean (11/11 superseded rows
correctly linked; zero reversed-still-accepted), and only one dated commitment
is mismarked. The **RFC index/headers are not safe** for distinguishing
proposed-from-shipped. Treat the decision log as authoritative and the RFC
"proposed" label as unreliable until reconciled.

---

## 2. Decision-Corpus Inventory And Depth Ledger

| Corpus area | Convention / vocabulary | Pass depth | Selection reason | Strongest evidence tier | Residual risk |
|---|---|---|---|---|---|
| `docs/rfcs/README.md` (RFC index, 129 rows) | free-text lifecycle + `(Dnnn)` anchors | **deep** | primary onboarding surface; highest exposure | history- + static-impl-traced | none material |
| `docs/rfcs/[0-9]*.md` headers (~128 files) | `Status:` header line | **deep** (status line) / inventory (bodies) | dual-record rule makes header↔index a defect | decision- + history-traced | 22 files carry **no** Status header (convention gap) |
| `docs/decisions/decision-log.md` (206 rows) | `accepted`/`superseded`; D118 supersession rule | **deep** (status + all 11 superseded rows + reversal scan) | authoritative decision record | decision-traced | bodies not all deep-read; impl claims existence-traced |
| `docs/reference/todo.md` (1648 lines) | ✅/🟡/🔴 status snapshot + sections | **deep** (dated/triggered scan) | cold-start reading surface | history- + calendar-traced | none material |
| `docs/reference/spec.md`, `roadmap.md`, `prd.md` | prose + status banners | **deep** (trigger scan) | "temporary until" / deadline language | calendar-traced | open-ended deferrals (non-findings) noted |
| `docs/operator/BRIEF.md` | operator state | **inventory** | dated-commitment scan | — | low |
| `CHANGELOG.md` (472 KB) | release notes | **inventory** (targeted `rg`) | confirm shipped triggers | history-traced | not read end-to-end |
| `go/` source tree | n/a (existence probe) | **deep** (targeted) | confirm named artifacts exist | static-impl-traced | existence only, not behaviour |

**Coverage:** ~128 RFC files vs 129 index rows; all 206 decision rows status-classified; 11/11 superseded rows deep-read; all four dated-commitment seeds resolved. Unread: full RFC bodies and full CHANGELOG.

---

## 3. Lifecycle / Currency Map (inspected scope)

- **Decision log:** 195 `accepted`, 11 `superseded` (D006, D007, D008, D009,
  D013, D018, D081, D084, D105, D125, D174). Every superseded row names a live
  successor that exists. No `proposed`/`deferred`/`rejected` rows present.
- **RFC index:** 33 plain `accepted`, ~25 `accepted (Vn…)`, 8 `implemented`,
  ~10 `partially implemented`, 16 starting `proposed`, 3 `superseded`, 1
  `shelved (D106)`.
- **Superseded RFCs (correctly archived):** 0006 (→ PostgreSQL), 0028 (→
  D087/D094/D104), 0039 (→ D107/D111/RFC 0068). These match convention and are
  **not** findings.
- **Drift cluster (this audit's core):** RFC index "proposed" no longer tracks
  implementation — 0073/0108/0129 shipped, 0087 header/index split, 0083–0086
  header lag, 0069/0070 overstated.

---

## 4. Ranked Currency Findings

> Severity order. Every row carries: class, recorded claim (file:line),
> contradicting fact, evidence tier, verification note, confidence, smallest fix.

### DRA-001 — RFC 0108 marked `proposed`; all five phases shipped — `SERIOUS` · `stale-lifecycle`
- **Recorded claim:** `docs/rfcs/README.md:123` status `proposed`; RFC header
  `docs/rfcs/0108-parallel-independent-runs.md:3` `Status: proposed`. **Both
  sides** read pre-implementation.
- **Contradicting fact:** P2–P5 landed across v2.19.0–v2.23.0 — commits
  `de55960f` (P2, v2.19), `80d8ee35` (P3, v2.20), `a3a3afa4` (P5, v2.21),
  `6859c06c` (P4 `run.integrate`, v2.23). Decision rows D175/D192/D194
  reference RFC 0108 as the landed concurrency substrate.
- **Evidence tier:** `history-traced` (+ `static-implementation-traced`).
- **Verification note:** `git log --oneline --all | rg "RFC0108"` — four feat
  commits confirmed directly; `c31fab2c` is the original "propose" commit.
- **Confidence:** high. **Fix:** advance README:123 **and** header to
  `accepted / implemented (P1–P5, v2.19.0–v2.23.0)`.

### DRA-002 — RFC 0073 marked `proposed`; daemon-doctor blob parity is live — `SERIOUS` · `stale-lifecycle`
- **Recorded claim:** `docs/rfcs/README.md:88` `proposed`; RFC header
  `docs/rfcs/0073-daemon-doctor-blob-parity.md:3` `Status: proposed`.
- **Contradicting fact:** commit `a2b8c055` "GH #26 fix: surface RFC 0072 blob
  diagnostics through striatum daemon doctor (RFC 0073)"; live source
  `go/pkg/reads/doctor_blob.go`, `doctor_blob_handler.go`,
  `doctor_blob_handler_test.go`.
- **Evidence tier:** `static-implementation-traced` (+ `history-traced`).
- **Verification note:** `ls go/pkg/reads/doctor_blob*.go` (3 files) + commit
  message text.
- **Confidence:** high. **Fix:** advance README:88 + header to
  `accepted / implemented` (closes GH #26).

### DRA-003 — RFC 0087 index says `proposed`; file header + accepted D199 say implemented — `SERIOUS` · `superseded-unmarked` (header↔index split)
- **Recorded claim:** `docs/rfcs/README.md:102` `proposed`.
- **Contradicting fact:** RFC header
  `docs/rfcs/0087-divergent-ideation-workflow-shape.md:3`
  `Status: accepted (implemented 2026-06-14; frame layer per RFC 0129)`;
  **accepted** `D199` (`decision-log.md:41`): "Graduate the `divergent_ideation`
  shape (RFC 0087 + RFC 0129) from `experimental` to `supported`"; code
  `go/pkg/workflowgenerate/shapes_divergent.go`, commit `34d2099c`. This is a
  **direct violation of the dual-record rule** — the index is the stale side.
- **Evidence tier:** `decision-traced` (+ `static-implementation-traced`).
- **Verification note:** `rg "^\| \[0087\]" README.md` vs `rg -m1 "^Status:"`
  on the RFC; `D199` row read directly.
- **Confidence:** high. **Fix:** set README:102 to match the header
  (`accepted / implemented, D199`).

### DRA-004 — RFC 0129 marked `proposed`; frame library merged + graduated by accepted D199 — `SERIOUS` · `stale-lifecycle`
- **Recorded claim:** `docs/rfcs/README.md:144` `proposed`; RFC header
  `docs/rfcs/0129-cognitive-frame-library.md:3` `Status: proposed`. Both sides
  pre-implementation.
- **Contradicting fact:** `go/pkg/workflowgenerate/frames.go` (+ `frames_test.go`)
  exist; commit `34d2099c` "implement divergent_ideation shape first-class
  (RFC 0087 + RFC 0129)"; `37f1049e` graduates to `supported`; **accepted**
  `D199` names RFC 0129 explicitly.
- **Evidence tier:** `static-implementation-traced` (+ `decision-traced`).
- **Verification note:** `ls go/pkg/workflowgenerate/frames.go`; `git log` trace;
  D199 read.
- **Confidence:** high. **Fix:** advance README:144 + header to
  `accepted / implemented (D199)`. *(0087 + 0129 are the same shipped feature;
  reconcile together.)*

### DRA-005 — RFC 0083/0084/0085/0086 file headers read `proposed`; accepted by D139/D141/D143/D144 — `MINOR` · `stale-lifecycle`
- **Recorded claim:** headers line 3 of each:
  `0083-iterated-panel-review-with-interrogation.md`,
  `0084-interrogable-agent-loop-attestation-and-chat-ui.md`,
  `0085-tailnet-identity-ui-auth.md`,
  `0086-multiparty-conversation.md` — all `Status: proposed`.
- **Contradicting fact:** README rows 98–101 say `accepted`; decision log
  accepts them as D139 / D141 / D143 / D144 respectively. **The index is
  correct; only the RFC headers lag** (lower exposure → MINOR), but each still
  violates the dual-record rule.
- **Evidence tier:** `decision-traced`. **Verification note:** per-file
  `rg -m1 "^Status:"` vs README rows vs decision-log D-rows.
- **Confidence:** high. **Fix:** set each header to `accepted (Dxxx)`.

### DRA-006 — RFC 0044 header `proposed`; README records Striatum-side V1 landed — `MINOR` · `stale-lifecycle`
- **Recorded claim:** `docs/rfcs/0044-engram-phase-1-implementation-spec.md:3`
  `Status: proposed`.
- **Contradicting fact:** `docs/rfcs/README.md:58`
  `proposed (+ Striatum-side V1 landed under dogfood-046)`; **accepted** D100;
  `go/pkg/reads/corpus_historical.go` + CHANGELOG v1.35.0 corpus export.
- **Evidence tier:** `decision-traced`. **Verification note:** header vs README
  row vs `rg "RFC 0044" decision-log.md`.
- **Confidence:** high. **Fix:** header → `proposed (Striatum V1 landed, D100)`
  to match README.

### DRA-007 — RFC 0069/0070 index says `implemented`; file headers say `partially`/`mostly implemented` — `MINOR` · `stale-lifecycle`
- **Recorded claim:** `docs/rfcs/README.md:84` (0069) `implemented`,
  `:85` (0070) `implemented`.
- **Contradicting fact:** `0069-pg-only-daemon-global-surfaces.md:3`
  `partially implemented`; `0070-daemon-client-service-boundary.md:3`
  `mostly implemented`. The **index overstates completion** — the more
  dangerous direction (reader infers done) — but both are substantially built,
  so exposure is bounded → MINOR.
- **Evidence tier:** `decision-traced` (record-vs-record). **Verification
  note:** header vs README rows.
- **Confidence:** medium (did not adjudicate which side is truer behaviourally).
  **Fix:** reconcile each pair to a single lifecycle phrase.

### DRA-008 — TODO item 68 presents RFC 0078 final Python deletion as blocked; deletion shipped — `MINOR` · `expired-commitment` (discharged-unmarked)
- **Recorded claim:** `docs/reference/todo.md:122` — item 68 status
  `🟡 gates executed; final deletion blocked`; reinforced at `todo.md:1392–1393`
  ("Final Python deletion is the last remaining gate").
- **Contradicting fact:** deletion landed (Gate G, commits `a382dd7d` /
  `c68e3e6a`); `git ls-files 'src/*'` → 0 Python files; `spec.md:47` & `:2197`
  and AGENTS.md state Python is "completely retired and deleted." The two
  remaining tracked `*.py` are under `scripts/` (out of RFC 0078 `src/` scope).
- **Evidence tier:** `history-traced` (+ `static-implementation-traced`).
- **Verification note:** `git ls-files 'src/*'`, `git ls-files '*.py'`,
  `sed -n '122p' todo.md`.
- **Confidence:** high. **Fix:** flip item 68 to `✅ done`; drop the
  "last remaining gate" framing at todo.md:1392–1393.

### DRA-009 — RFC convention-coverage anomalies — `NOTE` · structural (currency-convention gaps)
- **Recorded claim / facts:** (a) **22 RFC files carry no `Status:` header**
  (0046–0067 cluster), so the dual-record rule is structurally unmet — those
  RFCs rely on the index alone. (b) **Duplicate RFC number 0050**:
  `0050-operator-ui-rework-and-provenance-honesty.md` and
  `0050-go-daemon-http-sse-mcp.md` (the README row text itself flags the
  collision). (c) **RFC 0042 file exists** (`0042-run-list-workflow-identity.md`,
  header `proposed`) **but has no README index row** (index jumps 0041→0043).
- **Evidence tier:** `static-implementation-traced` (file existence / header
  absence). **Verification note:** header sweep across `docs/rfcs/[0-9]*.md`.
- **Confidence:** high. **Fix:** add Status headers; renumber one 0050; add a
  0042 index row (or fold into hygiene cleanup — borders `REPO_HYGIENE.md`).

---

## 5. Expired Time-Bound Commitments

Filtered re-view of the only `expired-commitment`-class promotion (DRA-008).

| Locator | Commitment | Trigger | Fired? | Status |
|---|---|---|---|---|
| `todo.md:122`, `:1392-1393` | RFC 0078 final Python deletion "blocked / last remaining gate" | event (src/ deletion) | **yes** — `src/` empty, commits `a382dd7d`/`c68e3e6a` | **DRA-008** (discharged-unmarked) |

All other dated/triggered conditions inspected were **correctly discharged and
marked** or are non-findings:
- **D125** ("dry-run until three successful live dogfoods"): condition met and
  row already `superseded` → **D133** ("the D125 gate is satisfied…"). Clean.
- **D105** ("temporarily keep the Python daemon as primary core"): already
  `superseded` → **D107**. Clean.
- **D106 / RFC 0049** ("after June 15, 2026"): date now past, but it sits in the
  *rationale* column of a `shelve` decision, not presented as a live future
  trigger → not a finding (monitor only).
- `spec.md:2157` (rotation policy "deferred until a future RFC") and
  `roadmap.md:23` ("historical until the next vX.Y.0 refresh") are open-ended /
  self-disclaiming → standing deferrals, not findings.

---

## 6. Verified-Current / Live Decisions (positive examples)

- **Decision-log supersession integrity is clean.** All 11 superseded rows name
  live, existing successors with correct back-references: D006/D007/D008 →
  D094/D104; D009 → D094/D104 (D094 explicitly preserves D009's live part);
  D013 → D104/RFC 0050; D018 → D107/D111; D081 → **D087/D094/D104** (D087 is a
  real accepted row at `decision-log.md:149`); D084 → D105→D107/RFC 0068
  (narrow-then-restore arc recorded); D105 → D107; D125 → D133; D174 → D177.
- **No reversed-still-accepted defects.** Every reversal/restore is coherently
  marked: agy seat D163(grant)→D174(demote)→D177(restore) nets to `supported`
  with D163's consequences amended to cite D177; D175's `auto_spawn` deferral is
  partially superseded by D189 (RFC 0122) with the live remainder correctly kept
  `accepted`.
- **Honestly-partial RFC records** (accurate, not findings): 0027
  (`proposed, phase-2 guardrails shipped`), 0054/0055 (`Phase A shipped`), 0099
  (`Phase 1+2`), and the genuinely-still-proposed 0041, 0052, 0094, 0102, 0115.
- **Correctly superseded RFCs:** 0006, 0028, 0039 all archived per convention.

---

## 7. Gated Verification And Residual Risk

- **RFC 0097 (`proposed`) — superseded-in-part candidate, not promoted.** The
  self-hosting orchestration vehicle was proven live (`8e9ac86b`, 2026-06-01),
  but the `run execute` orchestrator it specifies was largely re-shaped into
  **RFC 0116 `run drive` (D175)**. I did not diff 0097's named surface against
  what 0116 actually shipped, so its true state ("vehicle proven; spec partly
  superseded") is unproven within budget. *Maintainer call:* mark 0097
  `superseded-in-part by RFC 0116` rather than `proposed`.
- **RFC 0057 (`proposed (scaffold)`) — partial-decision candidate.** D126
  *accepts the product choices* (corpus contract V2) but the manifest/schema
  impl appears unbuilt. Surfacing D126 in the record would make the
  accepted-decision-vs-unbuilt-scaffold split legible; not promoted (the
  "scaffold" label is defensible).
- **RFC 0094 (`proposed`) — false-positive guard.** Shape *names*
  (`fog_of_war_review`, `synaptic_prune`) appear in
  `go/pkg/artifactcontracts/contracts.go`, but 0094's deferred *mechanisms* show
  no implementing source. Correctly left `proposed`; a shallow grep could
  mis-read the enum presence as "shipped."
- **Live runtime state out of authority:** `STRIATUM_AUTO_SPAWN_SCHEDULER`
  default and prod enablement live in a systemd drop-in, not the repo; no repo
  doc carries a fired-trigger commitment about it → no doc finding, but
  unverifiable here.
- **Behaviour not verified:** all DRA-001..004 implementation claims are
  existence/history-traced (file + commit present), not behaviour-tested
  (read-only authority). This is the main cap on overall confidence.

## 8. Rejected Candidates And Fix-Direction Themes

- **REJECTED — "D081 dangling D087 successor pointer."** An inherited sub-audit
  claim that D081's `D087` successor does not exist. **Re-verification refuted
  it:** `rg "^\| D087 "` returns `decision-log.md:149` (accepted, RFC 0030
  daemon RPC foundation). D081's pointer is valid; not a finding. *(Lesson: the
  sub-agent's grep pattern missed the row; promoting it would have been a false
  positive — re-verified per the evidence standard.)*
- **REJECTED — superseded RFCs 0006/0028/0039 as "stale."** They describe old
  state but are correctly marked superseded with live successors → historical,
  not findings.
- **REJECTED — D106 June-15-2026 date as expired-commitment.** The date is in a
  shelve-decision rationale, not a live forward trigger.
- **Fix-direction themes (consolidated):**
  1. **Reconcile the RFC index to implementation** — the dominant theme. Advance
     0073/0108/0129 to `implemented` and 0087 README to match its header; these
     are the four that would most mislead an onboarding agent.
  2. **Enforce the dual-record rule mechanically** — header↔index drift
     (0044, 0069, 0070, 0083–0086) plus 22 header-less files suggests a CI lint
     comparing each RFC's `Status:` header against its README row would prevent
     recurrence.
  3. **Keep todo.md status snapshot honest at release** — DRA-008 is a
     completed-but-unflipped gate; fold the snapshot update into the
     release/decision-log discipline already practised for the decision log.
- The **decision log itself needs no remediation** beyond the RFC-record
  reconciliations above; its supersession and dated-commitment hygiene is sound.
