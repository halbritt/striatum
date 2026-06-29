---
type: record
status: frozen
owner: OPUS
expires: null
---

# Striatum — Deep Architecture Review

**Reviewer:** Claude Opus 4.8 (adversarial peer review)
**Date:** 2026-06-02
**Target:** `/home/halbritt/git/striatum` @ `fb87f231` (v2.9.3, schema 22)
**Audience:** the sole maintainer

---

## A. Thesis (one line, no hedging)

> **OVERBUILT · DRIFTING · confidence: medium.** Biggest risk: complexity is accreting on the *trust/provenance/choreography* axes faster than reliability is being earned on the *execution* axis — the product's own core path (a multi-lane revision cycle) still cannot run to completion without operator rescue, yet it is wrapped in capability tokens, tamper-evident hash chains, S3 offload, cross-repo coordination, and ~14 workflow choreographies.

The nuance the headline can't carry: the **core substrate is right-sized-to-sophisticated and largely earns its keep**; it's the **breadth around it** that has outrun the depth beneath it. And the most recent arc (RFC 0095→0101→0102→0103) is the maintainer *correcting* toward robustness — the right instinct, not yet finished. I defend all of this below.

---

## B. Files reviewed / files skipped

This is a 222K-line repository (90K LOC Go across 295 files; 132K lines of Markdown across 1,396 files). I did **not** read all 90K LOC line-by-line, and I will not pretend I did. An architecture verdict here rests on the data model, the runtime wiring, the dependency surface, the git trajectory, and the empirical failure record — which I covered in full — plus the load-bearing handlers read in full and the rest surveyed by structure (signatures, sizes, DDL, churn). I separate the tiers honestly.

**Read in full (line-by-line):**
`README.md` · `AGENTS.md` · `CLAUDE.md` · `docs/index.md` · `docs/operator/BRIEF.md` · `Makefile` · `go/Makefile` · `go/go.mod` · `go/pkg/db/sql/0001_baseline.sql` · `go/pkg/mutations/mutations.go` (1,087 lines) · `go/cmd/striatumd/main.go` (799 lines) · `docs/dogfood/FRICTION_LOG.md` · `STRIATUM_REPO_HYGIENE_CLAUDE_OPUS_4_8_2026-05-30.md` · head of `docs/_archive/reviews/internal/STRIATUM_ARCHITECTURE_REVIEW_TEAMWORK_PREVIEW_2026-05-29.md` · `CHANGELOG.md` (Unreleased + v2.9.x).

**Surveyed structurally (DDL, exported signatures, file/function inventory, sizes, churn — not every line):**
all 22 SQL migrations (`go/pkg/db/sql/0001`–`0022`, full DDL deltas) · `contracts/daemon_methods.json` (all 128 method names) · per-file LOC + function inventory of `go/pkg/mutations/*` (incl. the 99-function `supervision_control.go`) · per-file LOC of `go/pkg/reads/*` · exported symbols of `supervisor`, `workflowgenerate`, `workflowauthoring`, `webservice`, `agentloop`, `artifactcontracts`, `crossrepo`, `mcp`, `sessionliveness`, `lanehealth`, `installers` · `go/pkg/workflowgenerate/generate.go` shape + template switch tables · all 102 RFC titles (`docs/rfcs/0*.md`) · the `docs/` tree · full git history (cadence, churn, oldest-untouched files, first/last commit).

**Deliberately skipped (with reason):**
- The ~84K LOC of Go I did not open line-by-line — read by structure instead; flagged where a verdict needs a closer look.
- The 102 RFC *bodies* (read titles + the BRIEF's and CHANGELOG's summaries; the RFCs are intent, and intent is well-summarized by the operator surfaces).
- The body of the four mega-docs: `spec.md` (110KB), `decision-log.md` (167KB / ~150+ D-rows), `todo.md` (105KB), `roadmap.md` (79KB), `ubiquitous-language.md` (63KB). Read their role and size, not their full text — their *existence and scale* is itself a finding (§ ledger).
- `docs/_archive/**` — explicitly historical Engram-incubation fixtures per `AGENTS.md`.
- Per-run dogfood artifact bodies under `docs/dogfood/<id>/` and `docs/dogfoods/*/artifacts/` — read the run *structure* and OPERATOR_REPORT framing, not every artifact.
- `examples/**` workflow JSON (17 example dirs — read the catalog, sampled shapes via the generator).
- `skills/optional/**`, `go/pkg/installers/templates/**`, `go/web/**` static/templates — read by inventory.
- The 365KB `CHANGELOG.md` body below v2.9 · vendored deps · lockfiles · `dist/` archives · binary blobs.

---

## C. What it is (so the verdict has a referent)

**Stated** (`README.md:3`, `AGENTS.md`): "A local workflow runner for terminal-based AI coding agents." A daemon (`striatumd`) owns live state in one PostgreSQL schema; the CLI, a local web UI, and an MCP endpoint are capability-gated clients of the daemon's RPC boundary. The target repo holds durable Markdown provenance; `.striatum/` is scratch. No hosted services, no telemetry, no vendor SDK in the runner.

**Actual** (code + schema): exactly that, and more than that. The daemon is a serializable PostgreSQL state machine (migration `0005_repo_local_workflow_state.sql`: `runs`, `jobs`, `job_dependencies`, `leases`, `queue_messages`, `work_packets`, `artifacts`, `verdicts`, `blockers`, `process_executions`, `events`, `job_worktrees`, `process_supervisors`) wrapped in a daemon-global control plane (`0001_baseline.sql`: `clients`, `client_capabilities`, hash-chained `audit_log` + `audit_chain_head`, `repositories`, `scheduler_cursors`). Every mutation is a short transaction that appends a SHA-256-chained event (`mutations.go:797` `appendEvent`). Lanes are real terminal processes supervised over PTY/tmux by a decoupled helper (`go/pkg/supervisor`, `go/cmd/striatum-supervisor-helper`). A 60s in-daemon recovery sweep (`cmd/striatumd/main.go:553`) acts on a liveness classifier.

**Mine:** the one-line pitch undersells and the codebase oversells. This is not "a runner"; it is a **provenance-grade, capability-secured, multi-agent orchestration control plane** that happens to run on a laptop. The question this review exists to answer is whether that's the right thing to have built for one person — and the answer is *the core yes, the totality no.*

---

## D. The value-vs-complexity ledger

One row per meaningful subsystem. LOC are tracked Go (tests excluded from the headline number but noted). "Solo-op value" is the honest question: does this earn its complexity *for one operator on a laptop, today*.

| Component | What it does | Value (concrete, to whom) | Complexity it carries | Verdict |
|---|---|---|---|---|
| **PG substrate + migrations + hash chain** (`db`, 1.6K + 22 migrations) | Single daemon-owned Postgres; append-only events/artifacts with `previous_hash`/`row_hash`; serialized chain heads; PL/pgSQL append-only triggers | The genuine USP: a crashed agent leaves a tamper-evident record. To: the operator auditing what untrusted agents did | Serializable txns; chain-head `FOR UPDATE` serialization; owner-vs-runtime role DDL split (a known foot-gun, see memory) | **KEEP** — the right state model; the differentiator |
| **Mutation handlers / lifecycle state machine** (`mutations`, 32K LOC incl. tests; ~18K non-test) | The write path: claim→ack→complete, review/verdict, run lifecycle, revision routing, attempt scoping, DAG enqueue | The product's core behavior | THE complexity sink. `supervision_control.go` alone = 2,867 LOC / 99 funcs. `recovery.go` 1,971, `claim.go` 1,220, `lifecycle.go` 1,147, `review.go` 1,052, `recovery_auto_finalize.go` 1,164. Known-incoherent under revision + multi-reviewer (RFC 0095) | **KEEP core / SIMPLIFY** — split the god-file; shrink the state surface by cutting speculative shapes (below) |
| **Read handlers** (`reads`, 13.5K incl. tests) | status/dashboard/detail/graph/redaction/export projections | Operator + web UI + CLI visibility | Large but mechanical; `supervision.go` 1,408 is the heavy one | **KEEP** |
| **PTY/tmux supervisor + helper** (`supervisor` 3.9K, `agentloop` 2.2K, `striatum-supervisor-helper`) | Spawns/supervises terminal agents; FIFO/PTY pumps; tmux-backed observability; MCP bootstrap per adapter | Lanes are *real terminal agents*, not API calls — this is hard, novel, and necessary | PTY + tmux + FIFO + per-adapter bootstrap (claude/codex/gemini); start-time anti-PID-recycling | **KEEP** — correctly decoupled (helper imports no DB/RPC) |
| **Liveness + lane health + recovery** (`sessionliveness` 1.3K, `lanehealth` 1.2K, `recovery` 0.3K + the 9-verb `recovery.*` family) | Classifies lanes (`working_protocol/local/tool/quiet/stalled/dead`); bounded autonomous requeue/escalate | Directly answers the #1 historical failure: lanes silently die mid-task | **18 liveness timestamp columns** on `sessions` (migrations 0012+0019); 6 states; **9 recovery RPC verbs** | **KEEP / SIMPLIFY** — justified by the empirical record, but the 9-verb surface is a *symptom*: runs get stuck nine ways |
| **MCP control plane** (`mcp` 1.5K, `rpc` 1.8K) | HTTP/SSE JSON-RPC; capability-filtered tool visibility; envelope + audit append + method registry | The agent-facing control surface; the whole "MCP-first" thesis | 128 methods, several flat+namespaced **duplicate aliases** (`complete`+`work.complete`, `verdict`+`review.verdict`, `publish_artifact`+`artifact.publish` …) | **KEEP / SIMPLIFY** — collapse the legacy flat aliases |
| **Workflow generate + authoring** (`workflowgenerate` 4.5K, `workflowauthoring` 3.2K, `workflowtemplates` 0.9K) | Generate/validate/lint `workflow.json`; ~14 shapes + ~30 embedded role-prompt templates (incl. a **16-template `ace_*` family** for one RFC-0098 shape) | Operator convenience for authoring runs | ~7.6K LOC, much of it embedded prompt strings + per-shape graph builders for choreographies that *wedge in practice* | **SIMPLIFY hard** — keep 3 proven shapes; quarantine the rest as `experimental`. Single worst over-engineering (see roll-up) |
| **Artifact contracts** (`artifactcontracts` 2.1K in 2 files) | Hand-rolled front-matter schema validation for ~13 artifact kinds (decision/finding/synthesis/collaboration_ledger/…); refuses invalid FM w/ exit 6 | Provenance integrity of durable artifacts | 2K LOC of bespoke validation + a large enum vocabulary; collaboration_ledger truth-tables | **KEEP / watch** — born of a real falsification incident (dogfood-020..029); don't grow the vocabulary |
| **Interrogation + conversation** (`mutations/interrogation*`, `conversation*`; migrations 0016/0017) | Live multi-turn Q&A and N-party dialog against a running lane | Interactive design panels; "interrogable" reviews | A whole sub-protocol with its own window/closure state that **interacts badly with revision cycles** (#65/#84/#131/#134) | **SIMPLIFY / gate** — the most lifecycle-destabilizing feature; isolate behind the core loop being solid |
| **Web service + assets** (`webservice` 1.6K, `web/`, `webassets`, `websse`) | Server-rendered HTML + SSE proxy to daemon RPC; run graph, chat views, recovery actions | Human operator UI (loopback) | Thin proxy — *good*. But it's the 5th+ web-UI iteration (RFCs 0013/0022/0023/0024/0037/0038/0061/0085/0092) | **KEEP (as-is)** — resist further UI RFCs until the core is reliable |
| **Blob storage** (`blob` 0.9K; migrations 0009/0010; minio-go dep) | Optional S3/MinIO offload of artifact bodies | Off by default (`STRIATUM_BLOB_ENDPOINT` unset → disabled, `main.go:617`) | A whole S3 client + 2 migrations + backfill verbs for a **local-first single-operator tool** | **CUT or DORMANT** — for a laptop, the filesystem + PG already hold artifacts. Nothing breaks if removed; keep only if a real remote-artifact need exists |
| **Cross-repo workflows** (`crossrepo` 0.8K; migration 0003; RFCs 0028/0032/0035) | Coordinate runs across multiple repos | Thin: `Cancel/Describe/Why/List` delegating to local run verbs | 4 RPC methods + cross-repo tables; most of the package is a test fake | **DORMANT** — speculative for a solo op; don't extend |
| **Corpus / archive / trajectory export** (`reads/corpus_historical.go`, `archive.go`, `trajectory.go`; `corpus.*` verbs) | Export redacted JSONL; migrate historical Engram dogfood files into blobs | Replay/provenance sharing | One-time historical-migration verbs + export machinery | **SIMPLIFY** — keep `corpus.export`; the `*_historical_dogfood_*` migration verbs are one-shot, retire after use |
| **Capability tokens + sealed-apply + audit segments** (`admin`, `apply`, `rpc/auth_pg`; migrations 0001/0022) | Per-session bound tokens; signed sealed-patch apply; tamper-evident audit segments | Defends "a compromised lane impersonates another lane / reads another repo" | Real crypto + session-binding (RFC 0096/0103 W1) for a **loopback, single-human threat model** | **KEEP minimal / DON'T GROW** — the impersonation guard is cheap now it exists; sealed-apply + audit *segments* are insurance on a 1-user system |
| **Installers / skills** (`installers` 1.3K + templates) | Render claude_code/codex/gemini skill + plugin bundles | Lets an agent drive the runner without reading source | Template rendering per profile | **KEEP** |
| **CLI** (`cmd/striatum` 2.1K) | Daemon-backed bootstrap/diagnostics/fallback client | Operator + CI + recovery | scope_check + codex helpers | **KEEP** |
| **Adapter conformance + chaos suite** (`adapterconformance` 3.9K) | Fake-agent fixture; C0–C12 contract; fault-injection chaos suite (RFC 0101 P5) | The *right* kind of test — caught a real deployed recovery bug | PG-gated, time-warp fault injection | **KEEP / EXPAND** — this is where test investment should go, not unit coverage |

**Roll-up.** ~18 meaningful subsystems. I would **CUT or render dormant 4** (blob/S3, cross-repo, corpus-historical migration, the flat RPC aliases), **SIMPLIFY hard 3** (workflow shape catalog, interrogation/conversation, the `supervision_control.go` god-file), and **KEEP** the rest. Conservatively that reclaims **~8–12K LOC** and — more important than LOC — it **removes lifecycle states the core state machine must be correct in.**

**The single worst piece of over-engineering:** the **workflow-shape catalog in `workflowgenerate/generate.go`** — ~14 generator shapes and ~30 embedded role-prompt templates, including a **16-template `ace_*` family** for the *single* RFC-0098 "adjudicated constraint extraction" choreography, plus `falsification_gate`, `cross_examination`, `implementation_panel`, `multi_phase`, and N-party `conversation`. **Why it exists:** not resume-driven and not cargo-culted — it's **speculative generality born of genuine intellectual interest in multi-agent choreography** (RFCs 0052/0074/0086/0087/0093/0094/0098). It is a menu of elaborate dinners built before the kitchen can reliably cook eggs: the dogfood record (BRIEF "revision-cycling interrogating panels wedge"; #65/#84/#120/#121) shows these very shapes are the ones that hang. Each shape is new surface the lifecycle state machine must handle correctly, and the lifecycle is exactly what's not yet coherent.

---

## E. The inverse check — what's genuinely missing (high bar)

Short, because the project's problem is the opposite of under-building. Two things are load-bearing and absent:

1. **A green, hermetic, end-to-end acceptance test of the real multi-lane code-change-with-revision flow.** The chaos suite (`adapterconformance/chaos_test.go`) proves *recovery* at the daemon level with a fake agent — excellent — but the actual multi-lane revision lifecycle (the product's headline shape) has **no hermetic gate**, which is precisely why it regresses across 0095/0101/0103. Unit coverage floor is **20%** (`go/Makefile:14` `CORE_COVERAGE_FLOOR ?= 20.0`) on the load-bearing packages. Tests pass; the system wedges. The missing test is the difference. **This is the single most valuable thing to build.**

2. **One idempotent operator-rescue verb for "lane died after writing a valid artifact, before its publish/complete callback."** `FRICTION_LOG.md` dogfood-060/F1 documents *five* recovery verbs each refusing this case for a different reason; RFC 0101 Phase 3 added `recovery requeue-stale --force`, but the BRIEF still lists window-closure recovery gaps (#131/#134). The 9-verb `recovery.*` family is breadth where one coherent verb is needed.

Everything else a solo operator needs at this stage is present or over-present.

---

## F. Lenses

**1 — Overbuilt?** Yes, on balance — see the ledger. The discriminating test (value-per-unit-complexity, not absolute sophistication): the **substrate, supervisor, MCP plane, and liveness/recovery pass** — their sophistication is matched by the difficulty of coordinating untrusted terminal agents with provenance. The **shape catalog, blob/S3, cross-repo, sealed-apply/audit-segments, and the interrogation sub-protocol fail** — each is generality, security, or storage machinery whose justifying scenario (many shapes in real use; remote artifacts; multiple repos; a hostile lane; multiple humans) does not exist for this operator today. Generality with one caller is overbuild: cross-repo has one operator; blob has zero remote consumers; most shapes have zero non-dogfood runs (open question O3).

**2 — On track?** *Drifting, recently correcting.* The git history tells it cleanly: a Python MVP (weeks 19–21, the most-churned files are all `src/striatum/*.py`), then a **256-commit, 2-day Go rewrite (May 17–18)** triggered by a mid-May multi-model review burst (`docs/_archive/reviews/external/`, 05-16→05-18) — a genuinely good correction. Velocity is extreme and **declining but still high** (W20 278 → W22 185 commits; ~33/day average for a solo dev). The *stated* roadmap (RFC 0102 "one control surface, fewer things to stare at"; RFC 0103 "production hardening") is consolidation; the *actual* commit stream **still widens** — RFCs 0093/0094/0098 added *new* collaboration shapes inside the same window the core loop was being hardened. That gap between stated (consolidate) and actual (widen) is the drift. It is not stalled — chaos suite, recovery supervisor, session-bound tokens all landed and deployed — but the finish line (reliable multi-agent self-hosting) keeps receding because new surface is added beside it. The **"self-hosting paradox"** the BRIEF names ("a broken runner can't reliably dogfood its own fixes") is the trajectory's central tension, stated in the maintainer's own words.

**3 — Greenfield / north-star.** Given the *real* constraints (one operator, laptop, ~3 terminal agents), I would build **80% of what's here and 40% of its surface**:
- Same daemon-owned single Postgres; same append-only hash-chained event log (cheap, and it's the USP). **Keep.**
- Same MCP-first boundary with CLI/web as thin clients. **Keep.**
- Same PTY/tmux supervisor-helper with the no-DB-import decoupling. **Keep.**
- A **smaller state machine**: run → DAG jobs → lease → artifact → verdict → *one* revision edge. No multi-phase, no choreography DSL, no interrogation/conversation sub-protocol in v1.
- **Liveness as a trichotomy** (dead / stalled / working) on ~3 timestamps + a heartbeat, adding states only where a real failure demands one — not 18 columns and 6 states up front.
- **Provenance kept, security deferred**: event log + artifact records + operator-override byline (the falsification fix) — yes. Per-session capability tokens, sealed-apply signing keys, audit *segments*, blob/S3, cross-repo — **not until a second user or a real adversary exists.** They're already written, so the recommendation isn't "delete the crypto" so much as "stop extending it and don't let it gate the roadmap."
- **3 shapes shipped as example JSON**, not a 7.6K-LOC generator.

Distance from current to that north-star is **subtractive, not a rewrite** — which is the good news. The delta is "freeze and prune," ~weeks of deletion + one hard test, not months of rebuild. The core I'd keep is already the strongest part of the code.

**4 — Future directions (bets, not a wishlist).**
- **Bet 1 (this month, the only P0): make the multi-lane code-change-with-revision flow boring.** Drive it through the runner end-to-end, 10 times consecutively, no operator rescue; encode that as a hermetic gate (extend the chaos suite from recovery-only to the real revision lifecycle). *Payoff:* the product's core promise becomes true. *Effort:* weeks. *Forecloses:* nothing — it's the critical path.
- **Bet 2 (this month, days): prune to 3 shapes.** Quarantine `falsification_gate`/`cross_examination`/`adjudicated_constraint_extraction`/`conversation`/`multi_phase` behind an `experimental` flag; cut `generate.go`'s embedded prompt catalog to draft/review/apply. *Payoff:* ~6K LOC and a much smaller set of lifecycle states to be correct in. *Forecloses:* the choreography-menu vision — acceptable; it returns once the core holds.
- **Bet 3 (a year out, only on a trigger): the dormant machinery switches on.** If a second operator or an adopter appears (open question O1), the capability/audit/blob/cross-repo layers are *already built* — that's the upside of the overbuild. Leave them dormant, don't extend, switch on with a real consumer.
- **Anti-bet: do NOT build the Engram memory layer (RFC 0041/0044).** Striatum was *extracted from* Engram; re-absorbing a memory subsystem is scope-reversal. Keep it an optional external integration or drop it.
- If the honest future is "stop adding, harden what's here" — **it is.** Bets 1 and 2 *are* that.

**5 — Strengths worth preserving (do not break these in any refactor).**
- **The append-only hash-chained event log + serialized chain head** (`mutations.go:797`/`897`, migration 0006, PL/pgSQL `refuse_*` triggers). This is the differentiator and it's correctly implemented (canonical-JSON SHA-256, chain-head `FOR UPDATE` so forks are impossible). Touching the chain format or the append-only triggers would forfeit the one thing that makes Striatum more than a task queue.
- **The 4-direct-dependency tree** (`pgx`, `creack/pty`, `minio-go`, `yaml.v3`; `go.mod`). For 90K LOC of Go with an HTTP/SSE server, JSON-RPC, a state machine, and PTY supervision to lean entirely on the stdlib (no web framework, no ORM, no DI, no cobra) is discipline most teams lack. **Preserve ruthlessly** — every new dep should hurt.
- **The supervisor-helper decoupling** — the helper pumps PTY/FIFO and imports no DB or RPC package (`go/pkg/supervisor/helper.go`). Correct boundary; it's why a wedged lane can't corrupt daemon state.
- **The honest-provenance discipline** (operator-override byline, attestation guard, the append-only DB row as evidence-of-claim even when the on-disk file is corrected). Born of the real dogfood-020..029 falsification — keep it, don't grow the vocabulary.
- **The chaos/fault-injection test posture** (`adapterconformance`). It caught a real bug deployment missed. This is the model for *all* future testing here.
- **The `--no-daemon` retirement + fail-closed daemon** (`main.go:175` refuses to start without Postgres; loopback-only bind enforced; web auth fails closed with a deny token). Security defaults are right.

**6 — Concerns, ranked.**
- **BLOCKER — the revision-cycle lifecycle is not coherent.** The product's headline path wedges under `needs_revision` + multi-reviewer/interrogating panels. Evidence: BRIEF Hazards ("revision-cycling interrogating panels wedge … a one-shot single-implementer build sidesteps the panel revision incoherence"); the existence of the entire RFC 0095/0101/0103 arc; `supervision_control.go` 2,867 LOC + 9 recovery verbs as scar tissue; #65/#84/#131/#134 still open. A product whose core workflow needs operator rescue is pre-reliable regardless of how good the provenance is.
- **SERIOUS — 20% coverage floor on the load-bearing state machine** (`go/Makefile:14`) while live runs fail in ways unit tests don't model. The chaos suite proving a *deployed* bug confirms unit tests are not catching the failures that matter. Coverage isn't the metric; **a real lifecycle acceptance gate** is.
- **SERIOUS — CI lint silently red.** `golangci-lint` is not installed locally, so the lint phase of `make check` cannot run in the dev loop; `main` was red across many commits over two `errcheck` findings before anyone noticed (BRIEF "CI/release pipeline unbroken 2026-06-02"; memory). A correctness-critical project whose own gate is invisible locally is a real operational hole.
- **SERIOUS — documentation/governance mass exceeds what one person can keep true.** 102 RFCs, 150+ decision records, and ~525KB of core reference docs (`spec.md` 110KB, `decision-log.md` 167KB, `todo.md` 105KB, `roadmap.md` 79KB, `ubiquitous-language.md` 63KB). Drift is already visible: `README.md:83` claims **schema v6** (actual **22**); the RFC index marks landed RFCs `proposed` (BRIEF); `FRICTION_LOG.md` stops at dogfood-060 (mid-May). Docs this voluminous *will* lie, and lying docs are worse than fewer docs for a solo maintainer.
- **SMELL — `supervision_control.go` is a 2,867-LOC / 99-function god-file.** Cohesive but unmaintainable as one unit.
- **SMELL — 128 RPC methods with flat+namespaced duplicate aliases** (`mutations.go:67-130`). Compatibility cruft; a real surface to keep correct.
- **SMELL — speculative shapes outnumber proven ones** (ledger D, lens 1).
- **SMELL — two RFCs both numbered 0050** (`0050-go-daemon-http-sse-mcp.md`, `0050-operator-ui-rework-and-provenance-honesty.md`). Trivial, but it's a canary for the governance-mass problem.

---

## G. Recommendations (only changes I would personally make; subtraction-biased)

| Priority | Change | Rationale | Benefit | Risk | Effort |
|---|---|---|---|---|---|
| **P0** | **Freeze feature surface. Build one hermetic acceptance gate for the real multi-lane code-change-with-revision flow** (extend `adapterconformance` chaos harness from recovery-only to the full revision lifecycle); make it pass 10× consecutively unattended. | The core path wedges; nothing else matters until it doesn't. Unit coverage (20%) doesn't model it. | The product's promise becomes true; regressions in 0095/0101/0103's territory get caught before deploy. | Low — additive test; may surface more bugs (that's the point). | **1–2 weeks** |
| **P0** | **Stop adding workflow shapes / RFCs until P0 lands.** | The stated roadmap is consolidation; the commits still widen. Close the stated-vs-actual drift by decree. | Velocity redirected from breadth to depth. | None. | 0 (discipline) |
| **P1** | **Quarantine speculative shapes behind `experimental`; cut `generate.go`'s embedded prompt catalog to draft/review/apply.** Delete the `ace_*`/falsification/cross-exam/multi-phase generator paths from the default surface. | Worst over-engineering; each shape is lifecycle surface that must be correct. | ~6K LOC gone; far fewer states for the core loop to handle. | Medium — must confirm no live (non-dogfood) run depends on them (O3). | **2–4 days** |
| **P1** | **Install `golangci-lint` in the dev loop / pre-push hook** (the exact CI version, `v2.12.2`). | A correctness gate that's invisible locally let `main` sit red. Ties to a concrete failure. | CI lint reproducible before push; no more silent-red. | None. | **1 hour** |
| **P1** | **Split `supervision_control.go`** into `supervise_start.go` / `supervise_send.go` / `supervise_stop.go` / `supervise_rebridge.go` / `supervisor_state.go` / `supervisor_delivery.go`. | 2,867 LOC / 99 funcs is unnavigable; this is where lifecycle bugs hide. | Reviewability of the most bug-dense file; easier to reason about the revision path. | Low — mechanical move; tests guard it. | **0.5 day** |
| **P2** | **Render blob/S3, cross-repo, and `corpus.*_historical_*` dormant**: stop extending; document as "built, not in use." Remove the `minio-go` dep if blob is truly idle. | Generality with zero/near-zero callers for a solo op. | One fewer dep; smaller surface; honest status. | Low — opt-out already; removal is clean. | **0.5–1 day** |
| **P2** | **Collapse the flat RPC aliases** (`complete`/`verdict`/`block`/`ack`/`publish_artifact`/…) to the namespaced forms; keep a deprecation shim for one release. | 128→~110 methods; less to keep correct. | Cleaner contract; fewer guardrail rows. | Low–medium — check skill bundles/CLI for flat-name use. | **1 day** |
| **P2** | **Doc consolidation + truth pass.** Fix `README.md` schema v6→v22; reconcile the RFC index status column; demote `todo.md`/`roadmap.md` overlap into one; banner or retire `FRICTION_LOG.md` (superseded by GH issues). Stop minting a decision record for every change. | 525KB of core docs + 150+ D-rows can't stay true under solo maintenance; drift already present. | Docs a single person can keep honest; less ceremony per change. | Low. | **1–2 days, ongoing discipline** |
| **n/a** | **Do not build the Engram memory layer (RFC 0041/0044).** | Re-absorbs the parent project Striatum was extracted from. | Avoids scope reversal. | — | — |

If only three things happen: **P0 (acceptance gate + freeze), P1 (prune shapes), P1 (lint in the loop).** Those convert "impressive but pre-reliable" into "boring and trustworthy," which is the only thing standing between this project and its own stated goal.

---

## H. Open questions (what I could not determine; needed to firm the verdict)

1. **Is there a real second operator / adopter on the horizon, or is this a solo tool indefinitely?** This single fact flips the verdict on ~4 subsystems. If a second user is coming, the capability tokens / audit segments / cross-repo machinery is prescient insurance and the "overbuilt" charge on *those* softens to "early." If not, they're dead weight to freeze.
2. **Is the elaborate collaboration-shape catalog a research goal in itself, or instrumental to shipping code changes?** If exploring multi-agent choreography *is* the point (a research platform), then the shape catalog is the product and "overbuilt" is the wrong frame — the frame becomes "is the research substrate reliable enough to learn from?" (still no). If the shapes are instrumental, prune them. The whole verdict's weighting depends on which this is.
3. **How many real, non-dogfood workflows has the maintainer run to completion in the last month, and of which shapes?** I saw 3 dogfoods (`rfc-0097/0101/0103`) and a friction record dominated by wedges. If the answer is "I run the review+synthesis shape weekly and it works," the core is healthier than the failure log suggests. If it's "mostly dogfoods that wedge," the BLOCKER stands at full weight.
4. **Is the daemon-owned-Postgres role-DDL foot-gun** (migrations can't `ALTER` owner tables; recovery needs owner-applied DDL — per memory and RFC 0079 §5) **a recurring operational tax or a one-time scar?** Determines whether the substrate's operability needs work or is settled.
5. **What does the maintainer want Striatum to *be in 12 months*** — a personal multi-agent harness (then prune aggressively), or a shippable product others adopt (then P0 reliability is existential and the docs/security mass becomes justified)? The right amount of everything in this review hinges on that one answer.

---

*Review method note: read the intent, data model, runtime wiring, dependency surface, git trajectory, empirical failure record, and the load-bearing handlers in full; surveyed the remaining Go by structure. Verdicts are grounded in the cited paths; where a claim rests on the maintainer's own operator BRIEF or memory rather than first-hand execution, it is marked. I did not drive a live multi-lane run — hence medium, not high, confidence on the BLOCKER's current severity.*
