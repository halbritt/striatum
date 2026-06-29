# Striatum Architecture Review — Claude Opus 4.7 — 2026-05-18
author: reviewer-claude-opus-4.7-001

> Revision note (2026-05-18, post-interview). The first version of this
> review (committed as `a05bb4b`) made two structural mistakes: it read
> "local-first single-operator" as "one human, one AI session, one repo,"
> and it argued for SQLite WAL plus an in-process daemon as the north-star.
> A subsequent maintainer interview corrected that to "one human, 8+ AI
> operators, 3+ repos" with a team-adoption target. The substrate and
> daemon decisions are correct for that constraint set. This revision
> retains the structural concerns about half-finished migrations, doc
> volume, and packaging hygiene; it retracts the SQLite/in-process
> north-star argument and the capability-scope-reduction recommendation;
> and it adds the install-UX and CI-as-deliverable concerns that the
> team-adoption target makes load-bearing.

Reviewer voice convention, used throughout:

- **stated** — what the project's docs/READMEs claim
- **actual** — what the code actually does
- **mine** — my opinion as a peer reviewer

Project name resolved from `pyproject.toml:6` (`name = "striatum-orchestrator"`) — **striatum** (Python module name).

## 0. Files reviewed

- `README.md`
- `pyproject.toml`
- `Makefile`
- `AGENTS.md` (= `CLAUDE.md` content)
- `CHANGELOG.md` (lines 1–100)
- `contracts/daemon_methods.json` (lines 1–60 of 979)
- `docs/INDEX.md`
- `docs/SPEC.md` (lines 1–400 of 1810)
- `docs/DECISION_LOG.md` (header + spot rows; full ID scan via `grep`; 117 numbered decisions)
- `docs/UBIQUITOUS_LANGUAGE.md`
- `docs/DDD.md`
- `docs/PRD.md`
- `docs/TODO.md` (lines 1–150 of 1500)
- `docs/ROADMAP.md` (lines 1–150 of 1248)
- `docs/operator/BRIEF.md`
- `docs/dogfood/` (directory listing; 66 numbered run directories)
- `src/striatum/cli/__init__.py`
- `src/striatum/cli/dispatch.py` (lines 1–300 of 1935)
- `src/striatum/cli/daemon_required.py` (lines 1–100 of 217)
- `src/striatum/daemon_rpc/registry.py` (lines 1–80 of 242)
- `src/striatum/schema.py` (lines 1–80 of 304)
- `STRIATUM_ARCHITECTURE_REVIEW_CODEX_GPT_5_2026-05-18.md` (first 30 lines, for voice convention)
- Source/test/doc inventory via `find`/`wc`/`grep`.
- Live correction round with the maintainer (5 questions × 2 = 11 datapoints) covering operator topology, audience, daemon status, doc purpose, substrate driver, RFC purpose, web UI consumption, first external user profile, CI health, workflow authoring path, top friction, definition of "done," next priority, and capability-token usage.

## 1. Executive summary

- **The system is shaped correctly for its actual operating profile.** One human pilots 8+ concurrent AI operators across 3+ repos. Postgres-as-sole-substrate (D094) stacks three independent justifications at that concurrency: appender contention, audit-chain row-lock semantics, and ops ergonomics for many repos. The daemon as single writer is the right shape, not over-engineering. The eight capability scopes are exercised in practice (different tokens per operator).
- **The vocabulary is load-bearing and code-enforced.** `docs/DDD.md` is real: verdict, posture, lease, byline, attestation, capability live at the daemon RPC boundary (`contracts/daemon_methods.json`). A reviewer can't return "looks good" because the API doesn't accept it. This is the project's best decision; protect it.
- **The work-in-flight is the load-bearing risk.** Top friction reported is substrate-migration drag. The tree carries Python `daemon_pg/` (19k LOC, deprecated mid-deletion), Go `go/` (25k + 7k test, production target), legacy SQLite quarantine (`src/striatum/legacy_sqlite/`, 7k LOC, mid-deletion), and three named schema authorities (`schema.py:5` = "1", `repo_local_schema.py:5` = 16, PG migrations at v6). "Done" is bounded by maintainer answer: legacy SQLite *and* Python daemon deleted *and* fixtures collapsed *and* a fresh-clone PyPI install works on macOS + Linux through `adopt` and a real workflow.
- **`make check` / `release-check` is not known-green on `main`.** For a project whose stated first external user is "a team adopting striatum," CI green-and-known-green is itself a deliverable, not a side effect. The 2026-05-17/18 commit cadence has outpaced the test-matrix reconciliation.
- **277 stale `island-shared-*.js` files (9.9 MB) committed in `src/striatum/web/static/build/`.** Vite emits content-hashed bundles, the build target doesn't atomically replace siblings, and git stores everything. Small, fixable, embarrassing for an adopter who clones fresh.
- **The repo root reads as a workbench, not a maintained project.** `final_status.json` (256 KB), `status.json` (176 KB), six `STRIATUM_*_REVIEW_*.md` (including this one), four `STRIATUM_*_REMEDIATION_PLAN*.md`, `ENGRAM_DEVELOPER_REQUEST.md`, `GASTOWN_COMPARISON.md`, `PROJECT_COMPARISON.md`, `CLAUDE_DESIGN_UI_REWORK_PROMPT.md`. First impression for a team adopter is "this is someone's scratchpad."
- **`src/striatum/cli/dispatch.py` (1,935 LOC) is the seam through which every CLI verb passes**, and it's where SQLite-fixture compat, daemon routing, and direct command dispatch are all interleaved. Splitting this is the unblock for the rest of the cutover.
- **Doc apparatus is justified in volume but underweight in discoverability.** RFCs are forward design (so 72 is "we did 72 design exercises," not paperwork). DECISION_LOG and dogfood ledgers are provenance and AI-operator context. SPEC at 1,810 lines is too long to be the source of truth in practice; the DDD doc at 199 lines is doing that work instead. For a team adopter, the cost is not "too many words" — it's "no curated 6-RFC reading list."
- **The web UI is built ahead of consumption.** Confirmed-purpose is human-principal escalation triage. Today's surface (five islands: tree-browser, workflow-chooser, workflow-graph-editor, code-viewer, recovery-panel) covers a lot more than the next-priority escalation flow. `island-workflow-graph-editor.js` is dead weight because workflow authoring is via the `striatum workflow generate` CLI generator, not the visual editor.
- **The project does not currently meet its own "first external user" bar.** That's the right thing to fix next, immediately after substrate cutover. The substantive recommendations in §7 cluster around making `pip install striatum-orchestrator && striatum adopt && striatum run prepare && striatum run start` work cleanly on a fresh laptop with a fresh Postgres.

## 2. What the project is trying to be

### stated

`README.md:3` and `docs/SPEC.md:11-28`: a local-first workflow runner for terminal-based AI coding agents, daemon-owned PostgreSQL as the only authoritative state, target repos as durable provenance, no hosted services, no telemetry, no transcript capture, no vendor SDK imports.

`docs/PRD.md:23-50`: 33 seed decisions (D001–D033) covering hybrid coordinator, model portability through lanes-as-config, fresh sessions, bounded cycles, JSON workflow config only, durable artifacts, no broad transcripts.

`docs/DDD.md`: the vocabulary is the model. Aggregate roots, value objects, an event log, and a single write surface (daemon RPC) are the four pillars.

`docs/UBIQUITOUS_LANGUAGE.md`: 200+ glossary terms. Every RFC concept gets added here first.

`docs/operator/BRIEF.md`: current operational concern is the Go daemon port (D107/RFC 0068) and legacy SQLite quarantine.

### actual

The boundary matches the stated boundary at the daemon RPC level. `contracts/daemon_methods.json` enumerates 100+ methods; `src/striatum/daemon_rpc/registry.py:17` defines the 8-capability vocabulary; each token can be repo-scoped, daemon-global, single-repo, or cross-repo; each method declares its required capability and audit class. Different concurrent operators today run under different tokens with differentiated scopes. The capability vocabulary is exercised, not aspirational.

Two daemons coexist: Python `src/striatum/daemon_pg/` (mid-deletion) and Go `go/` (production target). The Python handler tree is currently 19k LOC and shrinking; Go is 25k LOC + 7k test and stable.

Legacy SQLite (`src/striatum/legacy_sqlite/`, ~7k LOC) is quarantined under lazy compat wrappers. Production paths refuse SQLite (`src/striatum/cli/daemon_required.py:73-79` enforces daemon-required + paired-test-harness gate). The cleanup is in-flight, not done.

The doc apparatus reflects forward-design RFCs (per maintainer: "RFCs are forward-looking design proposals"), provenance archives (decisions, dogfood ledgers), and explicit AI-operator context (skill bundles per RFC 0015). Total: 184k LOC of Markdown across 1,500 files. Three primary consumer profiles confirmed: AI operators, future-you on cold start, provenance / audit. External contributors are *not* the primary consumer.

### mine

The thesis is sound and the implementation matches it. My first-pass framing was wrong: "single operator" in the docs is *not* "one human, one session" — it's the role that the AI agent plays, with a human principal as escalation-only. The runner does coordinate multi-lane terminal-agent workflows with real audit-chain provenance and real refusal semantics, at concurrency levels that justify the substrate.

Three genuine framing tensions remain:

1. **"Demo-stage maturity"** (`pyproject.toml:22` says `Development Status :: 3 - Alpha`) vs **"first external user = a team adopting striatum"**. The latter is a higher bar than the former; releasing under "alpha" while targeting team adoption means you're depending on adopters' willingness to live with churn. Pick one. Either commit to alpha (raise the breakage tolerance in install docs) or target adoption (raise the install/CI/version-tag discipline).
2. **"No model dependency in the runner"** vs **first-class plugin bundles for `claude_code`, `codex`, `gemini_cli`**. The plugins are configuration, not imports — so the strict claim survives — but the runner's mental model is biased toward those three CLIs and the supervised wrapper scripts encode each tool's permission flags. Fine; just acknowledge it.
3. **"Read SPEC if docs disagree"** vs a 1810-line SPEC. The DDD doc at 199 lines is doing the *actual* orienting work. SPEC needs a brutal cull to ~300 lines of contract-only material, with the operational detail moved into per-RFC docs.

## 3. Current architecture

### Components

- **CLI** (`src/striatum/cli/`, 18 modules, 7,968 LOC). User-facing surface. Dispatch lives in `dispatch.py` (1,935 LOC), argparse in `parser.py` (1,343 LOC), mutations in `mutations.py` (1,288 LOC).
- **Daemon RPC contract** (`contracts/daemon_methods.json`, 979 lines, 100+ method routes). Versioned shape with capability and audit-class annotations; consumed by both Python registry and Go registry generators.
- **Python daemon-PG handlers** (`src/striatum/daemon_pg/`, ~19,000 LOC across ~50 files; mid-deletion). Workflow loop, reads, recovery/evidence, run lifecycle, supervision, registry, worktree.
- **Go daemon** (`go/`, 24,595 LOC source + 7,390 LOC test; production target). Independent implementation of the same RPC surface and Postgres schema. 8 migrations under `go/pkg/db/sql/` matching the 8 in `src/striatum/daemon_pg/sql/`.
- **Web service + frontend** (`src/striatum/service*.py`, 12 modules totalling ~2,500 LOC; `src/striatum/web/` with Jinja2 server-render and Vite/React islands). Purpose: human-principal escalation triage.
- **MCP** (`src/striatum/mcp.py`, 602 LOC). Both stdio wrapper and daemon MCP resource surface.
- **Workflow engine** (`src/striatum/workflow.py`, 2,568 LOC). Validator + planner + generator hooks. Two schema versions live: `striatum.workflow.v1` and `striatum.workflow.v1.1`.
- **Legacy SQLite quarantine** (`src/striatum/legacy_sqlite/`, ~7,000 LOC across 11 modules). Lazy-loaded behind compat wrappers; production paths refuse.
- **Skills/plugins** (`src/striatum/skills/`, `src/striatum/plugins/`). Per RFC 0015: generate per-tool skill bundles for `claude_code`, `codex`, `gemini_cli`, `generic`.

### Runtime

- One daemon process per machine, owning a PostgreSQL connection pool and a Unix-domain socket at `~/.local/state/striatum/runtime/striatumd.sock` (Linux) or `~/Library/Caches/striatum/runtime/striatumd.sock` (macOS) (`src/striatum/cli/daemon_required.py:82-94`).
- CLI invocations open a Unix-socket RPC with capability-scoped envelope. Audit-chained response.
- Refusals are exit-coded: 11 (daemon unreachable), 12 (repo not migrated), 8 (invalid transition), 9 (schema version skew). Codes are stable and tested.
- Supervised lanes write to FIFOs under `.striatum/scratch/<supervisor_id>/stdin.pipe`. Supervised wrappers (`.striatum/bin/{claude,codex,gemini}-supervised-wrapper.sh`) consume one packet per line and shell to the provider CLI with non-interactive approval flags (`docs/ROADMAP.md:110-122`).
- Stdout/stderr of supervised processes go to `DEVNULL` by construction; the runner never parses agent output for state (D028 invariant).

### State / storage

- **Authoritative**: daemon-owned PostgreSQL. 8 migrations (`0001_baseline.sql` through `0008_lane_evidence_publish_guard.sql`). Schema v6 anchors per-event hash chain in `previous_hash`/`row_hash` columns plus a `striatumd.repo_event_chain_heads` pointer for O(1) chain-head reads.
- **Scratch**: `.striatum/` next to each target repo for supervisor FIFOs, pidfiles, transient stdout.
- **Provenance**: durable Markdown artifacts in the target repo, with front-matter validation per kind (`decision`, `finding`, `findings_ledger`, `synthesis`, `support_ledger`, `action_item_ledger`, `harness_improvement_proposal`, `escalation`, `operator_brief`, `work_plan`, `progress_note`, `operator_report`).
- **Schema authority confusion**: `src/striatum/schema.py:5` declares `SCHEMA_VERSION = "1"` (legacy SQLite). `src/striatum/repo_local_schema.py:5` declares `LATEST_REPO_LOCAL_SCHEMA_VERSION = 16`. PG substrate is at v6. The first two are quarantined corpses; they should be deleted, not "quarantined."

### Surfaces

- **CLI**: 100+ verbs/subverbs (`striatum` console script, `pyproject.toml:43-44`). Primary surface for both AI operators and the maintainer.
- **Daemon RPC**: Unix socket; envelope per RFC 0030; capability-token authorization; different operators today carry different tokens.
- **Web**: `striatum serve --web`. Localhost-only. Jinja2 server-render + 5 Vite/React islands + SSE event stream. **Intended primary consumer: the human principal**, for escalation triage. AI operators don't use it.
- **MCP**: production daemon MCP resource API + legacy stdio wrapper for compat. Capability-gated `tools/list` and `tools/call`.
- **Local Python API**: `striatum.api.invoke` (`src/striatum/api.py`). Kept for authoring/test compat.

### Tests

- 220 test files, 54,860 LOC. `pyproject.toml:62-64` registers one custom marker (`multi_repo`). `Makefile:141-154` runs the multi-repo suite under `STRIATUM_MULTI_REPO_REQUIRE_PG=1`, parameterized by `STRIATUM_MULTI_REPO_DAEMON_CORE` (Python vs Go).
- `tests/_harness/` and `tests/fixtures/` carry test infrastructure. `tests/architecture/` enforces import-boundary guardrails (e.g., production code doesn't import `striatum.daemon`).
- Multiple Go-daemon test files (`tests/test_daemon_go_*.py`) invoke the Go binary and assert RPC parity with the Python read-model.
- **`make check` / `release-check` is not currently known-green on `main`.** Per maintainer: "honestly not sure." 2026-05-17/18 commit cadence outpaced CI reconciliation.

### Release

- 25 version tags between v1.31.0 (2026-05-13) and v1.55.0 (2026-05-15). 2026-05-18 working tree has an `Unreleased` block. Each tag carries multiple paragraphs of CHANGELOG prose.
- `Makefile:175` defines `release-check: check smoke`; `check` runs lint, typecheck, test, ui-check-bundle, ui-test, metadata-check, wheel-size, package-smoke.
- Wheel ships Go daemon binaries via `src/striatum/_daemongo/binaries/striatumd-<plat>-<arch>` (`pyproject.toml:57`, `Makefile:107-121`). Build pipeline cross-compiles four platforms: linux-x86_64, linux-aarch64, darwin-x86_64, darwin-arm64.
- **The fresh-clone install story is the load-bearing acceptance test** for substrate-migration done. It's not currently part of CI in a known-green state.

### Where code disagrees with docs

1. **SPEC says SQLite is retired**; codebase still has `schema.py`/`migrations.py`/`db.py` (4,719 LOC) wired through compat wrappers. Production refuses to open SQLite (correct), but the substrate code is loaded into the process. Docs are ahead of cleanup, which is one defensible direction but should be acknowledged.
2. **README and SPEC say the daemon is Go**; `daemon_pg/handlers/` is still 19k LOC of Python handlers. Maintainer confirmed: deprecated, mid-deletion. Should be marked deprecated in code, not only in docs.
3. **`pyproject.toml:22` says "Development Status :: 3 - Alpha"** while READMEs aim at team adoption. Pick one classifier.

## 4. Strengths

- **DDD framing is code-enforced, not retrofit** (`docs/DDD.md:138-148`). The 100-method `contracts/daemon_methods.json` is the data-driven realization of the single-write-surface invariant. Reviewer-can't-say-"looks good" because the API doesn't accept it. This is the project's load-bearing design and worth protecting.
- **Single source of truth for the RPC contract** (`contracts/daemon_methods.json` + generator scripts in `scripts/`). Capability and audit class declared per method; Python and Go both consume the same contract. This is the right shape for cross-implementation parity.
- **Append-only events with chain anchors and FOR UPDATE chain heads** (Schema v6 per `docs/SPEC.md:51-55`). Only sane choice for an audit chain under concurrent appenders, and the concurrent-appenders requirement is real (8+ ops).
- **Capability scopes are exercised, not aspirational.** Different operators carry different tokens today. The read/write/review/claim/apply/admin/recovery/surgical_recovery vocabulary maps to actual authorization decisions, not just schema columns.
- **Front-matter schemas are kind-specific and validate at publish time** (`src/striatum/artifact_contracts.py`, 621 LOC). Machine-checkable artifacts without forcing YAML/Pydantic dependency was the right call.
- **`daemon-required` enforcement with paired test-harness gate** (`src/striatum/cli/daemon_required.py:73-79`). The bare `STRIATUM_DAEMON_REQUIRED=0` opt-out was correctly rejected as an operator escape. The paired-marker requirement is the threat-model fix; that's the right level of care.
- **`init` no longer creates SQLite fixtures** (commit `10fb20a`). The bootstrap path stops materializing the legacy substrate. Right closing move on the migration.
- **Workflow validator refuses same-model implementer/reviewer pairings by default** (`docs/SPEC.md:208-212`). The reviewer co-blindness anti-pattern (`docs/ROADMAP.md:133-138`) is a real failure mode; encoding it as a lint refusal at validate time is the right place.
- **Provider portability is structural** (`pyproject.toml:30` — only `jinja2`, `markdown-it-py` as runtime dependencies). Two runtime deps for a system this size is impressive restraint and protects the no-vendor-SDK invariant by construction.
- **The dogfood ledger pattern** (`docs/dogfood/HISTORICAL.md`, 66 run directories). Running the runner against itself is a real validation mechanism. Harness-friction patterns + proposal-artifact kind close the feedback loop. This is the genuine basis for the project's confidence claims at the concurrency the docs assert.
- **Workflow generator covers authoring** (`striatum workflow generate`, RFC 0034). Per maintainer, this is the primary authoring path. JSON is the output, not the input — which means the contract is the schema, not the source format. Good architectural separation.

## 5. Concerns

Ranked **blocker / serious / smell** with file evidence.

### Blocker

**B1. Finish deleting the Python daemon_pg handlers.** Per maintainer: deprecated, mid-deletion. State of the tree: 19,385 LOC under `src/striatum/daemon_pg/handlers/` plus 285 LOC of read handlers under `daemon_pg/handlers/reads/`. The Go daemon is the production target. Every day both exist is a day where new code might land in the wrong tree. *Resolution*: pick a sprint, finish the deletion in one push, collapse the Python-daemon test fixtures into a parity-only minimal set. The Go conformance suite (`make daemon-go-conformance`) becomes the only daemon test target.

**B2. Finish the legacy SQLite quarantine.** `git log --oneline -30` shows 15+ consecutive "Quarantine legacy SQLite *" commits. `src/striatum/legacy_sqlite/` is now 11 modules, ~7,000 LOC. `src/striatum/cli/dispatch.py:27-31, 154-167, 640, 1638, 1711` carries fixture-gating logic interleaved with production dispatch. Production refusals are correct; the cleanup is what's not done. The risk window is a fixture path leaking into production exit codes via a single-bit env-var mistake. *Resolution*: delete `src/striatum/schema.py`, `src/striatum/migrations.py`, `src/striatum/db.py` after porting any fixture migration tests to a sealed migration-only module. Stop importing `sqlite3` from `src/striatum/cli/dispatch.py` (line 86 still has `import sqlite3 as _sqlite3` in an exception path). Top maintainer-reported friction is exactly this drag.

**B3. `make check` / `release-check` not known-green on `main`.** "Honestly not sure" + team-adoption target = this is itself a blocker. A team adopter who runs `git clone && make check` on Friday must see green. The 2026-05-17/18 cadence has left the matrix unverified. *Resolution*: pause feature commits for one Friday. Run `make release-check` against `main`. Fix what's red. Tag what's green. Treat that as v1.56.0. Then keep the matrix green by treating any CI red as stop-the-line.

**B4. No fresh-clone PyPI install acceptance test.** Per maintainer, "done" includes a clean PyPI install story: fresh clone → `pip install striatum-orchestrator` → `striatum adopt --profile claude_code` → first workflow runs on macOS + Linux without hand-holding. `scripts/fresh_clone_smoke.sh` and `Makefile:170` (`smoke` target) exist but it's not clear whether they actually exercise this path through a real Postgres + daemon-doctor + adopt sequence. *Resolution*: write the test that asserts the team-adoption first-30-minutes works. Put it on CI. Don't tag a release that doesn't pass it.

### Serious

**S1. `src/striatum/cli/dispatch.py` (1,935 LOC) is the seam through which every CLI verb passes.** Substrate routing, fixture compat, recovery, init, adopt, skills, plugins, daemon RPC routing all live here. The legacy-SQLite gating logic is interleaved with daemon routing. Refactoring into `router.py`, `daemon_dispatch.py`, `local_authoring_dispatch.py`, `legacy_compat.py` is the unblock for B1 and B2.

**S2. The version cadence misleads for the team-adoption target.** 25 versions in 6 days, each tag a snapshot rather than a release contract. Team adopters pin to versions. For a project that targets adoption, this is a real cost: every minor bump suggests "compatible improvement," but in practice the cadence reflects internal iteration. *Resolution*: shift to date-stamped versioning (`v2026.05.18-1`), or batch real releases (one a week, max) with a frozen changelog block.

**S3. 277 stale `island-shared-*.js` files committed in `src/striatum/web/static/build/`.** Total bundle dir 9.9 MB. Vite emits content-hashed bundles, but the build target doesn't `rm -rf` first (`Makefile:51` does `ui-clean` → `ui-build`, but only `make ui-build` runs it; manual `npm run build` doesn't). The accumulated files are in git and in the wheel. *Fix*: make `ui-clean` mandatory before any commit touching the build directory, and have CI refuse if more than the 5 named islands plus exactly one `island-shared-<hash>.js` are present.

**S4. SPEC at 1,810 lines is too long to be the source of truth.** In practice the DDD doc (199 lines) is doing the orienting work. For a team adopter, SPEC's role should be "what to expect from `striatum`, exit-code-by-exit-code" — a ~300-line contract document. The current SPEC mixes contract, narrative, RFC summaries, and operational guidance. *Fix*: cull to contract-only; move operational detail to per-RFC docs; move narrative to README/PRD.

**S5. The CLI argument parser is 1,343 lines** (`src/striatum/cli/parser.py`). Subparsers are added imperatively. Adding a verb requires editing `parser.py`, `contracts/daemon_methods.json`, a Python handler, and (usually) a Go handler. Four-place ceremony per verb. RFC 0060 (single daemon method contract source) addresses this for the contract side but not for the CLI parser. *Optional fix*: generate the parser from the contract.

**S6. Repo-root pollution.** `final_status.json` (256 KB), `status.json` (176 KB), six `STRIATUM_*_REVIEW_*.md` (this one included), four `STRIATUM_*_REMEDIATION_PLAN*.md`, `ENGRAM_DEVELOPER_REQUEST.md`, `GASTOWN_COMPARISON.md`, `PROJECT_COMPARISON.md`, `CLAUDE_DESIGN_UI_REWORK_PROMPT.md`. For a team-adoption target, the repo root is the first impression. *Fix*: move external-review artifacts into `docs/reviews/external/`. Delete or `.gitignore` the JSON dev-scratch files. Move comparison docs into `docs/records/_frozen/research/`.

**S7. Workflow editor island is dead weight.** Per maintainer, workflow authoring is via `striatum workflow generate`, not the React Flow editor. `src/striatum/web/static/build/island-workflow-graph-editor.js` exists but isn't the primary surface. *Fix*: delete the `island-workflow-graph-editor.js` and `island-workflow-chooser.js` entry points and their backing React code. Reduce islands to the escalation-relevant ones (`island-recovery-panel`, plus whatever chat surface the human-principal inbox needs).

**S8. Docs discoverability for new adopters.** RFCs as forward-design proposals (per maintainer) justifies the volume — 72 RFCs is "we did 72 design exercises," not paperwork. But for a team adopter, no curated entry path exists. They land on `docs/INDEX.md` and face 72 RFCs, 117 decisions, 66 dogfood runs, 1,500 Markdown files. *Fix*: produce a 6-RFC reading list for first-time adopters as `docs/ADOPTER_READING_PATH.md`. The list is small. Pick six RFCs that explain how the system thinks (probably 0019 DDD, 0026 lane attestation, 0028 long-running daemon, 0030 RPC envelope, 0043 PG-as-substrate, 0053 human principal + terminology).

### Smell

**Sm1. The `coordinator`-as-claimed-session path is documented but unused.** `docs/UBIQUITOUS_LANGUAGE.md:55`: "declared in every dogfood workflow but never actually claimed in any run." A first-class role that's been declared-but-never-claimed for the lifetime of the project should be deleted or unblocked. Schema cruft for unused features is a maintenance tax.

**Sm2. `src/striatum/cli/__init__.py` is a 78-entry `_SYMBOL_MODULES` shim.** Lazy imports for backward compatibility is reasonable. 78 entries means the public surface was never properly culled. Pick a sprint and decide which symbols are public.

**Sm3. `service*.py` is twelve files.** `src/striatum/service.py`, `service_http.py`, `service_api_routes.py`, `service_routes.py`, `service_request_io.py`, `service_request_security.py`, `service_server.py`, `service_sse.py`, `service_state.py`, `service_runtime.py`, `service_command_policy.py`, `service_daemon.py`. Twelve files is a code-smell that there's one service module trying to escape. Collapse to three: `service.py` (HTTP entry), `service_state.py` (state), `service_security.py` (security/policy).

**Sm4. `from striatum.legacy_sqlite ...` is still imported from `src/striatum/service.py` and `src/striatum/workflow.py`** (per `grep`). Production paths refuse SQLite at runtime, but the import is loaded into the process. Defense-in-depth concern, not correctness. Fold into B2 fix.

**Sm5. CHANGELOG.md is 4,730 lines of essay**, not a release log. Each version block is multiple paragraphs. For a team-adopting target, the CHANGELOG should answer "what changes if I upgrade." Currently it answers "what happened that week."

## 6. North-star architecture

The system as designed is approximately the right shape for the actual constraints (one human pilots 8+ AI operators across 3+ repos, audit-chain provenance, no hosted state, no vendor SDK). The north-star is *not a different architecture* — it's the current one with the migrations completed and the surfaces consolidated.

What I would change from the current trajectory:

**Keep.**
- Daemon as single writer. Capability-token authorization. Per-repo scope.
- PostgreSQL substrate. Audit-chain with FOR UPDATE chain heads. Append-only events.
- DDD vocabulary as the model. Daemon RPC method registry as the contract.
- Workflow generator as primary authoring path.
- Provider portability through lanes-as-config + supervised wrappers. No vendor SDK in core.
- Per-kind front-matter schema validation at publish time.
- Dogfood-as-validation cadence.

**Finish.**
- The Python daemon deletion. Go daemon as the only daemon.
- The legacy SQLite quarantine. Three schema authorities collapse to one (Postgres v6+).
- `dispatch.py` split. The substrate cutover is gated on this.

**Consolidate.**
- Web UI to escalation-only surfaces. Two islands max (recovery + chat-driven inbox). Delete the workflow editor and chooser islands; deprioritize the tree browser and code viewer until the human principal actually uses them.
- `service*.py` to three modules.
- SPEC to ~300 lines of contract-only material.
- CHANGELOG to weekly batched releases, not per-commit minors.

**Add.**
- A green-and-known `make release-check` on `main`. Treat it as a deliverable, not a side effect.
- A fresh-clone PyPI install acceptance test running on CI for macOS + Linux that exercises `pip install → adopt → run` end-to-end.
- A curated adopter reading path of 6 RFCs.

**Don't add.**
- A second daemon (already deleting the old one — don't reverse).
- A managed mode. Hosted mode. Cloud anything. Out of boundary by SPEC.
- A real-time collaboration UI. The web UI is escalation triage, not pair-programming.
- Per-RFC "ratification" workflows; the dogfood cadence is already that.

The deliberate tension to preserve: "demo-stage maturity" vs "first external user = team." Resolve by picking the **team** side and raising install/CI/version discipline, *or* picking the **alpha** side and writing "expect churn; pin to a SHA, not a tag" into the README. Hedging between the two costs more than picking either.

## 7. Recommended changes

Only changes I would personally make. Effort is "back-of-envelope, single-operator."

| Priority | Change | Rationale | Benefit | Risk | Effort |
|---|---|---|---|---|---|
| P0 | Finish Python daemon_pg deletion in one push | B1; maintainer-confirmed top friction is substrate drag | Half the code, half the test matrix; cutover closes | One painful week of test refactor | 1 week |
| P0 | Finish legacy SQLite quarantine deletion | B2; same driver; closes fixture-leak risk | One substrate, one path | Some compat tests get deleted | 3–5 days |
| P0 | Green-and-known `make release-check` on `main` | B3; team-adoption target makes this a deliverable | Tag-able state | None | 1–2 days |
| P0 | Fresh-clone PyPI install smoke for macOS + Linux on CI | B4; the actual definition of "done" | Adopter trust; release confidence | Need a CI runner that can install Postgres | 2–3 days |
| P0 | Move 11+ dev-scratch files out of repo root | S6; first impression for adopters | Repo reads as maintained | None | 30 min |
| P0 | `ui-clean` mandatory before every UI commit; CI refuses extra island-shared files | S3; bundle hygiene | Wheel size; reviewer cognitive load | Need to verify CI catches drift | 1 hour |
| P1 | Split `src/striatum/cli/dispatch.py` (1,935 LOC) | S1; gates B1/B2 cleanup | Cleanup becomes tractable | Touches every test path | 2–3 days |
| P1 | Cull `docs/SPEC.md` to ~300 lines of contract-only material | S4; SPEC isn't currently the source of truth in practice | DDD doc gets to be the orienting read | Decide what's contract vs description | 1 day |
| P1 | Delete `coordinator`-as-claimed-session schema cruft | Sm1; never used | Less unused vocabulary | Future workflows may want it; document explicitly that it's gone | 2 hours |
| P1 | Collapse `service*.py` (12 → 3 modules) | Sm3 | Easier to find things | Some import path changes | 1 day |
| P1 | Delete `island-workflow-graph-editor.js` and `island-workflow-chooser.js` | S7; not the authoring path | Less frontend surface | Removes a feature; document the deprecation | 4 hours |
| P1 | Write `docs/ADOPTER_READING_PATH.md` (6 RFCs to read first) | S8; discoverability for the team-adoption target | Adopters can land softly | None | 4 hours |
| P2 | Shift to date-stamped versioning OR batched-weekly releases | S2; cadence misleads adopters | Real release contract | None until someone depends on a tag shape | 1 hour + write the policy |
| P2 | Delete `src/striatum/schema.py`, `migrations.py`, `db.py` (after fixture port) | Three schema authorities is too many | One schema authority | Test refactor; part of B2 | Part of B2 |
| P2 | Cull `_SYMBOL_MODULES` shim in `src/striatum/cli/__init__.py` | Sm2 | Smaller public surface | Some old import sites may break | 4 hours |
| P3 | Generate `src/striatum/cli/parser.py` from `contracts/daemon_methods.json` | S5 | Adding a verb is one edit | Loses argparse hand-tuning | 1 week, optional |
| P3 | Rewrite CHANGELOG.md to upgrade-impact prose | Sm5 | Adopter sees "what breaks if I upgrade" | None | 1 day |

## 8. Functionality I'd add

| Priority | Change | Rationale | Benefit | Risk | Effort |
|---|---|---|---|---|---|
| P1 | `striatum run replay <run_id> --until <event_id>` | The event log is already the source; replay is implicit but not surfaced | Cheapest possible debugging tool; AI operator can introspect its own run | None | 1 week |
| P1 | A focused human-principal escalation web UI (chat + inbox + decision verb) | Maintainer-named next priority; web islands exist but aren't yet escalation-tuned | The right web surface for the actual target user | Slow if scoped wrong | 2–3 weeks |
| P1 | `striatum doctor --watch` (always-on doctor with degradation alerts) | The doctor exists; the "I noticed three hours ago" gap doesn't | Catches stuck supervisors, stale leases early | None | 2–3 days |
| P2 | `striatum diff-workflow <ref1> <ref2>` (aggregate-level workflow diff) | Workflow changes mid-run are a known footgun | "Did I just change the contract?" answered | None | 2 days |
| P2 | `striatum corpus query` over redacted bundle | Corpus is for external augmentation but no local query surface | Self-service "what did I ship?" without spinning up Engram | Surface ambiguity vs evidence export | 3–5 days |
| P3 | `decision propose` verb that scaffolds + opens in $EDITOR | Decision artifact is the escalation surface; today hand-authored | One verb instead of a checklist | None | 1 day |
| skip | Hosted mode | Out of boundary | — | — | — |
| skip | Team mode with login | Stated scope is one-human-pilots-N-ops | — | — | — |
| skip | Built-in model billing/usage tracking | Out of boundary | — | — | — |

## 9. Execution roadmap

### Today (concrete first step, startable in the next hour)

Move 11+ dev-scratch files out of the repo root: `final_status.json`, `status.json`, six `STRIATUM_*_REVIEW_*.md`, four `STRIATUM_*_REMEDIATION_PLAN*.md`, `ENGRAM_DEVELOPER_REQUEST.md`, `GASTOWN_COMPARISON.md`, `PROJECT_COMPARISON.md`, `CLAUDE_DESIGN_UI_REWORK_PROMPT.md`. Either move into `docs/reviews/external/` and `docs/records/_frozen/research/` or delete. Then run `make ui-clean ui-build` and commit the result; the 277 stale `island-shared-*.js` files should drop. These are no-risk 30-minute changes that materially improve first-impression for an adopter.

### Next week

P0 cluster. In order:

1. Split `dispatch.py` to unblock the cleanup.
2. Finish Python daemon_pg deletion.
3. Finish legacy SQLite quarantine.
4. Get `make release-check` green-and-known on `main`. Tag whatever that is as v1.56.0.

While that's in flight, freeze new RFC acceptance and pause feature commits.

### Next month

P0 install story + P1 consolidation.

1. Write the fresh-clone PyPI install acceptance test for macOS + Linux. Put it on CI. Don't tag a release that doesn't pass it.
2. Cull SPEC to contract-only.
3. Collapse `service*.py` to three modules.
4. Delete unused workflow editor + chooser islands.
5. Write `docs/ADOPTER_READING_PATH.md`.

### Next quarter

The next product priority per maintainer: human-principal escalation UX. Focused chat + inbox + decision verb on the web UI. With the substrate cutover closed and the install story working, this is the next thing that materially improves the team-adoption experience.

### Long-term

The unresolved framing question: "Development Status :: Alpha" vs "team adoption." That's a product decision, not an engineering one. Whichever way it goes, the architecture is fine.

## 10. Open questions

What I couldn't determine from the code, and that the next reviewer (or future-you) would need to confirm.

- **What's the right deletion-day for the `daemon-go-conformance` parity tests?** Once Python `daemon_pg` is gone, the Go conformance harness is testing the Go daemon against… itself. The "parity" frame loses meaning. Probably the suite collapses into the regular multi-repo test set.
- **What's the actual install footprint for a team adopting striatum?** `pip install` works in theory; in practice the adopter also needs Postgres running, `daemon doctor` to provision roles, and the daemon process started via systemd or manually. Is that documented end-to-end? Tested end-to-end? If not, where does an adopter give up?
- **Does the human-principal escalation flow actually fire today?** Per `docs/TODO.md:107` (RFC 0062), the escalation projection and artifact schema landed but the inbox is still 🟡. If escalations are still mostly aspirational, the web UI is being built ahead of consumption. That's fine intentionally, but worth checking.
- **What's the canonical bootstrap path for a fresh laptop with no Postgres installed?** `docs/POSTGRES_TRANSITION.md` exists. `striatum daemon doctor --apply-migrations` exists. `striatum adopt` exists. But the end-to-end "I just cloned, what do I do?" doesn't have a single canonical script. Should be one.
- **Are the dogfood ledger entries (66 runs) still actively read?** Or are they write-mostly archives? If actively read, the discoverability fix needs to surface them; if write-mostly, they're provenance and can stay below the fold.
- **What's the deletion plan for `striatum.api.invoke`?** It's kept for "authoring/test compat" (`docs/PRD.md:196-197`), but for a daemon-required runtime, every production mutation should go through RPC. Is there a path to removing the local Python API entry point, or is it intentionally kept as a low-tech surface?

---

*Closing note.* The technical core is good. The DDD framing is real and code-enforced. The substrate decision is correct for the actual concurrency. The supervised-wrapper / lanes / harness-profile machinery is well-thought-through. What the project needs from here is not a redesign; it is the closing pass on substrate-migration and packaging. Pick a Friday. Don't tag anything until the following Friday. Spend the week doing the P0 deletes and getting `make release-check` green. The system that emerges on the other side of that week is the system you've been building all along.
