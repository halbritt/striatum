# Striatum Remediation Plan — Claude Opus 4.7 — 2026-05-18
author: planner-claude-opus-4.7-001

> This plan synthesizes three 2026-05-18 architecture reviews
> (Codex GPT-5, Gemini CLI, Claude Opus 4.7) rather than consuming a
> single review file. Where the three converge it is reported as
> consensus; where they diverge I take a position and say why.
> Operating-context correction the reviews share: one human principal
> pilots 8+ concurrent AI operators across 3+ repositories; the first
> external user is a team adopting striatum.

## 0. Source review(s)

Three reviews from 2026-05-18 consumed in full:

- `STRIATUM_ARCHITECTURE_REVIEW_CODEX_GPT_5_2026-05-18.md` (37 KB; the most file-grounded; explicit per-file evidence including Go internals)
- `STRIATUM_ARCHITECTURE_REVIEW_GEMINI_CLI_2026-05-18.md` (7 KB; the most compressed; treats the migration cleanup and CI as the only two real concerns)
- `STRIATUM_ARCHITECTURE_REVIEW_CLAUDE_OPUS_4_7_2026-05-18.md` (39 KB; the most concerned with packaging, repo hygiene, and docs discoverability)

**Staleness check against current `main` (HEAD = `0a475e3 Mark legacy fixtures explicit`):**

- Both Codex and the v1 of Claude cited `src/striatum/daemon.py` as still present and SQLite-importing. **Stale** — the file was deleted (commit `8457599 Delete retired Python daemon module`). The v2 of Claude (committed `783af5c`) caught this; Codex's review does not.
- Codex also cited `src/striatum/legacy_sqlite/daemon_registry.py`. **Stale** — also removed.
- `src/striatum/cli/dispatch.py` cited as 1,935 lines; now 1,933 lines. Unchanged in shape.
- `src/striatum/daemon_pg/` cited as ~19k LOC. **Now 18,516** — shrinking.
- `src/striatum/legacy_sqlite/` cited as ~7k LOC. **Now 13,811** — *grown*, because the recent quarantine commits have moved retired-but-not-yet-deleted code into it. The reviews described this as "shrinking, mid-deletion"; the working tree shows "widening before collapse." Plan corrects this below.
- 277 stale `island-shared-*.js` files cited; **now 276**. Unchanged in shape.
- `tests/architecture/test_legacy_sqlite_quarantine.py` cited as a guardrail with an allowlist — confirmed present and load-bearing.
- All 11+ repo-root scratch files cited by Claude (`final_status.json`, `STRIATUM_*_REVIEW_*.md`, `STRIATUM_*_REMEDIATION_PLAN*.md`, `ENGRAM_*`, `GASTOWN_*`, `PROJECT_*`, `CLAUDE_DESIGN_*`) — still present.

Net: substrate-deletion work is genuinely in flight (one Python daemon module gone, one legacy registry gone, daemon_pg shrinking), but the quarantine module has grown to absorb code that's been moved-not-yet-deleted. Both reviews are directionally right; the *shape* of "what's left" has shifted.

## 1. Executive summary

- **The three reviews converge on one diagnosis**: substrate-migration drag is the top friction, CI/install health is the next gap, and human-principal escalation UX is the next product priority *after* cutover. Nothing else competes for P0 attention.
- **The cutover is half-done, not done.** Python daemon module deleted; daemon_pg shrinking; legacy SQLite *widening before deletion*. The quarantine guardrail (`tests/architecture/test_legacy_sqlite_quarantine.py`) is the load-bearing safety net while this is in flight.
- **`make release-check` is not known-green on `main`** (maintainer-confirmed). For a project targeting team adoption, this is itself a P0. No release should ship until it is green.
- **No P0 architectural redesign is warranted.** The Go-daemon + PostgreSQL + capability-token-per-operator architecture is correct for the actual concurrency (8+ ops × 3+ repos). Codex and Gemini explicitly affirm this; the v2 Claude review retracted its earlier SQLite-WAL push-back.
- **The "done" line per maintainer**: legacy SQLite deleted + Python daemon_pg deleted + test fixtures collapsed + clean `pip install → adopt → workflow run` on macOS and Linux. The install acceptance test is the most important deliverable in this plan.
- **Disagreements among reviews are small.** Codex catalogs more Go-side hygiene (cross-repo placeholder hooks); Claude catalogs more packaging/docs hygiene (bundle drift, repo-root clutter, SPEC length); Gemini compresses everything to two items. Consensus is broader than the differences.
- **Plan shape**: 7 P0 items (4 substrate, 1 CI, 2 hygiene); 7 P1 items (surface consolidation, docs, escalation UX, version policy); 6 P2 items (small cleanups). Total roughly 6–8 weeks of concentrated maintainer effort if pursued exclusive of feature work.

## 2. Disagreements with the review(s)

The three reviews disagree relatively little. Concrete adjustments I make:

**Gemini #2 functionality ("Single-Command Doctor/Smoke") — drop as a separate item.** The verb largely exists already (`striatum doctor --first-run`, `daemon doctor --apply-migrations`, `striatum adopt --profile claude_code`). The gap is not "a new combined verb" but "a CI job that exercises the existing verbs end-to-end on a clean machine." That work lives in P0-INSTALL-SMOKE below.

**Codex R3 ("keep daemon MCP token-scoped") — drop as a remediation item.** Codex's own verification ("local stdio MCP no longer exposes CLI-shaped compatibility tools, Go MCP filters visible tools by token capability") says this is done. Not a remediation; an existing strength.

**Codex R5 ("generate more authority docs from method contract") — downgrade to P2.** Partial generation already exists (`scripts/generate_daemon_method_tables.py`, `docs/architecture/DAEMON_METHOD_TABLES.md`). The duplication with the curated authority matrix is small and the maintenance pain is low compared to substrate cutover. Worth doing eventually; not load-bearing.

**Claude S7 ("delete workflow editor island") vs. Codex R6 ("trim cross-repo placeholder hooks") — keep both, group separately.** They're different surfaces (web frontend vs. Go interface). The workflow-editor deletion is P1 because workflow authoring is confirmed-via-CLI-generator; the cross-repo trim is P1 because no active RFC wires it.

**Claude's "version cadence" recommendation — keep at P1, not P0.** Misleading for adopters, but it doesn't block cutover or install. Pick a versioning policy when the cutover lands.

**Codex's R7 ("make workflow generate canonical") — fold into a docs change, not a separate item.** The CLI already exists and is the maintainer's primary path. The remediation is doc hygiene: remove hand-edited-JSON framing from `WRITING_WORKFLOWS.md` and `HOW_TO_HUMAN.md`. Folded into P1-DOCS-AUTHORING.

**One item only Claude flagged that I want to keep at P0: repo-root clutter.** Codex and Gemini did not call it out. For a "team adopting striatum" target user, the repo root is the first impression. Eleven dev-scratch files at the top is a 30-minute fix with disproportionate signal value.

## 3. P0 — blocking

Order within tier reflects dependency, not effort.

### P0-DISPATCH-SPLIT

- **source**: Claude S1, Codex Concern 1 (file evidence: `src/striatum/cli/dispatch.py:5-25, 224-313, 686-1145`)
- **what**: Split `src/striatum/cli/dispatch.py` (1,933 LOC) into four focused modules: `router.py` (verb-to-handler routing), `daemon_dispatch.py` (daemon RPC route construction), `local_authoring_dispatch.py` (init/adopt/skills/plugins/workflow-authoring), `legacy_compat.py` (fixture-only dispatch paths gated by `STRIATUM_TEST_HARNESS=1`).
- **why**: Every other substrate-deletion item touches this file. The interleaving of routing + fixture-gating + direct command dispatch makes safe deletion of legacy paths impossible without first separating the responsibilities.
- **touches**: `src/striatum/cli/dispatch.py`, `src/striatum/cli/__init__.py` (re-exports), every test importing from `cli.dispatch`.
- **effort**: 2–3 days.
- **depends on**: none.
- **acceptance**: each of the four new modules under 600 LOC; `legacy_compat.py` not imported except when `STRIATUM_TEST_HARNESS=1` and `STRIATUM_DAEMON_REQUIRED=0` are both set; `tests/architecture/test_authority_guardrails.py` still passes; existing CLI tests unchanged.

### P0-LEGACY-SQLITE-COLLAPSE

- **source**: All three reviews. Codex R1, Gemini #1, Claude B2. Maintainer-confirmed top friction.
- **what**: Convert or delete every module under `src/striatum/legacy_sqlite/` (currently 13,811 LOC across 11 modules). For each module: (a) port its test coverage to PG/Go where it tests current behavior, (b) delete it where it only tests retired behavior. Delete `src/striatum/schema.py` (SQLite schema), `src/striatum/migrations.py` (SQLite migrations), `src/striatum/db.py` (SQLite connection helper) after porting any fixture migration tests to a sealed migration-only module.
- **why**: Per maintainer, this is the top day-to-day friction. The quarantine module *grew* during the recent cleanup (7k → 13.8k LOC) because retired-but-not-yet-deleted code was moved into it. Holding pattern is not the end state.
- **touches**: `src/striatum/legacy_sqlite/*`, `src/striatum/schema.py`, `src/striatum/migrations.py`, `src/striatum/db.py`, `src/striatum/cli/dispatch.py` (after P0-DISPATCH-SPLIT: the `legacy_compat.py` module shrinks then disappears), `tests/architecture/test_legacy_sqlite_quarantine.py` (allowlist drops to empty), every test under `tests/` that imports `legacy_sqlite` (currently 14 files).
- **effort**: 1–1.5 weeks.
- **depends on**: P0-DISPATCH-SPLIT.
- **acceptance**: `src/striatum/legacy_sqlite/` does not exist; no production code imports `sqlite3`; `tests/architecture/test_legacy_sqlite_quarantine.py` either deleted or its allowlist is empty; `make test` passes; legacy SQLite migration-fixture tests (the ones that actually still test migration semantics) live in a sealed `tests/fixtures/legacy_sqlite_migration/` directory and are skipped by default unless `STRIATUM_LEGACY_SQLITE_IMPORT=1` is explicitly set.

### P0-DAEMON-PG-DELETE

- **source**: Codex R2, Claude B1, Gemini #1.
- **what**: Delete `src/striatum/daemon_pg/handlers/` (~15.4k LOC) plus `src/striatum/daemon_pg/repo_local_migration.py` and anything else under `daemon_pg/` that exists only as parity with Go. Retain `src/striatum/daemon_pg/client_admin.py` *only* if `striatum daemon doctor`, `repo add/list/remove`, and `adopt` still need direct PostgreSQL helpers before a daemon is reachable; otherwise migrate those to Go-owned admin surfaces. Add a guardrail test that fails if a workflow-live-state RPC method gains a Python handler outside the explicit bootstrap allowlist.
- **why**: Go is the only daemon going forward (maintainer-confirmed, RFC 0068). Every day both implementations exist is a day where new code lands in the wrong tree. Codex notes this is the second-biggest source of substrate sprawl after legacy SQLite.
- **touches**: `src/striatum/daemon_pg/handlers/*` (delete), `src/striatum/daemon_pg/repo_local_migration.py` (delete), `tests/test_daemon_pg*.py` (delete most; keep only what's still validating Go parity), `tests/daemon_pg/handlers/*` (delete or repurpose).
- **effort**: 1 week.
- **depends on**: P0-LEGACY-SQLITE-COLLAPSE (because some daemon_pg tests still cross-reference SQLite fixtures).
- **acceptance**: `src/striatum/daemon_pg/handlers/` does not exist; `make daemon-go-conformance` passes; the existing Go conformance test surface is the only daemon-handler test surface; `tests/architecture/test_authority_guardrails.py` enforces "no Python handler for any active workflow-state-mutation method."

### P0-CI-GREEN

- **source**: All three reviews. Codex Concern 4, Gemini #2, Claude B3.
- **what**: Pause feature commits. Run `make release-check` against `main`. Fix what's red. Tag the resulting green state as v1.56.0. Treat any future CI red as stop-the-line.
- **why**: For a project that names "team adopting striatum" as its first external user, an unknown-green main branch is a credibility blocker. The 2026-05-17/18 commit cadence has left the matrix unverified; maintainer answer was "honestly not sure."
- **touches**: CI workflow files under `.github/workflows/`, any tests that are flaking or wedged, the `Unreleased` block in `CHANGELOG.md`.
- **effort**: 1–2 days if nothing is deeply broken; 3–5 days if multi-repo or Go-conformance has regressed.
- **depends on**: none (independent — can land in parallel with P0-DISPATCH-SPLIT).
- **acceptance**: `make release-check` exits 0 on `main` from a fresh checkout; GitHub Actions shows green on the head commit; a v1.56.0 tag is pushed reflecting that green state.

### P0-INSTALL-SMOKE

- **source**: All three reviews. Codex R4 + Concern 4, Gemini #2 + Open question, Claude B4.
- **what**: Add a CI job (one matrix entry per OS: ubuntu-22.04 + macos-14) that does: `pip install striatum-orchestrator==<version>` from PyPI (or from the just-built wheel), `striatum daemon doctor --apply-migrations` against a freshly-installed Postgres, `striatum daemon service install && striatum daemon service start`, `striatum adopt --profile claude_code --register <fresh repo>`, `striatum workflow generate --shape review --out <path>`, `striatum run prepare`, `striatum run start`, assert terminal-completed state via `striatum status --json`. No tag ships unless this passes both OSes.
- **why**: This is the maintainer's stated definition of "done" for substrate cutover. Without it, every "v1.X.0" tag is a guess.
- **touches**: `.github/workflows/install-smoke.yml` (new), `scripts/install_smoke.sh` (new — orchestrates the steps), `Makefile` (new `install-smoke` target wrapping the script).
- **effort**: 2–3 days. The hardest part is getting Postgres reliably installed on macOS GitHub runners.
- **depends on**: P0-CI-GREEN (no point adding a smoke job to a red CI).
- **acceptance**: CI badge in README shows green install-smoke for macOS + Linux; a release tag refuses to push if install-smoke is red.

### P0-REPO-ROOT-CLEANUP

- **source**: Claude S6 (Codex and Gemini did not flag).
- **what**: Move 11+ dev-scratch files out of the repo root. `final_status.json` and `status.json` → delete or move to `.scratch/`. Six `STRIATUM_*_REVIEW_*.md` (including all three from today and three older ones) and four `STRIATUM_*_REMEDIATION_PLAN*.md` → move to `docs/reviews/external/`. `ENGRAM_DEVELOPER_REQUEST.md`, `GASTOWN_COMPARISON.md`, `PROJECT_COMPARISON.md`, `CLAUDE_DESIGN_UI_REWORK_PROMPT.md` → move to `docs/records/_frozen/research/` or delete.
- **why**: First impression for a team adopter cloning the repo. Currently reads as "someone's scratchpad." For team-adoption target, this matters.
- **touches**: repo root (11+ files moved or deleted).
- **effort**: 30 minutes.
- **depends on**: none.
- **acceptance**: `ls /` of the repo root shows only canonical project files (README, CHANGELOG, LICENSE, Makefile, AGENTS.md, CLAUDE.md, CONTRIBUTING.md, MANIFEST.in, pyproject.toml, plus directories). No `*_REVIEW_*.md`, no `*_REMEDIATION_PLAN*.md`, no `final_status.json`, no `status.json` at root.

### P0-BUNDLE-HYGIENE

- **source**: Claude S3 (Codex and Gemini did not flag).
- **what**: Delete the 276 stale `island-shared-*.js` files in `src/striatum/web/static/build/`. Modify the `ui-build` Makefile target to invoke `ui-clean` unconditionally before invoking Vite, not just when the operator types `make ui-build` (per `Makefile:51`, the dependency exists but `npm run build` directly bypasses it). Add a CI check that refuses any commit touching `src/striatum/web/static/build/` if more than the 5 named islands plus exactly one `island-shared-<hash>.js` are present.
- **why**: 9.9 MB of stale bundle output in git history is wheel bloat and a confusing first-impression for adopters who clone fresh. The `manifest.sha256` drift-detection intent is correct but doesn't enforce atomic replacement.
- **touches**: `src/striatum/web/static/build/island-shared-*.js` (delete 275 of 276), `Makefile`, `scripts/check_ui_bundle_size.py` (extend to count shared-bundle siblings), `.github/workflows/*` (add the new check).
- **effort**: 1–2 hours.
- **depends on**: none.
- **acceptance**: `ls src/striatum/web/static/build/ | grep -c '^island-shared-'` returns 1; bundle directory is < 1 MB; CI refuses any future commit that increases that count above 1.

## 4. P1 — serious

### P1-DELETE-EDITOR-ISLANDS

- **source**: Claude S7. Workflow authoring is `striatum workflow generate` (maintainer-confirmed).
- **what**: Delete `island-workflow-graph-editor.js`, `island-workflow-chooser.js`, and their backing React code under `src/striatum/web/frontend/src/islands/`. Update `WRITING_WORKFLOWS.md` and `HOW_TO_HUMAN.md` to remove hand-edited-JSON and visual-editor framing.
- **why**: Two of five islands are dead surfaces. Maintaining React Flow code for an authoring path nobody uses is pure cost.
- **touches**: `src/striatum/web/static/build/island-workflow-graph-editor.js`, `src/striatum/web/static/build/island-workflow-chooser.js`, `src/striatum/web/frontend/src/islands/workflow-*/`, `docs/WRITING_WORKFLOWS.md`, `docs/HOW_TO_HUMAN.md:968-980`.
- **effort**: 4–6 hours.
- **depends on**: P0-BUNDLE-HYGIENE.
- **acceptance**: 3 islands remain (`island-recovery-panel`, `island-tree-browser`, `island-code-viewer`); workflow-editor and workflow-chooser routes return 404 or redirect to CLI generator docs.

### P1-ESCALATION-UX

- **source**: All three reviews. Codex §8 Functionality #1, Gemini #1 functionality, Claude §8 P1. Maintainer-confirmed next priority.
- **what**: Build a focused human-principal escalation surface on the web UI: an escalation inbox showing open `escalation` artifacts across all registered repos, a blocked-lanes view (jobs in `blocked` state across runs), a stale-leases view, a capability-denial audit view, and "what to do next?" CTAs that resolve via `striatum decision record` / `striatum checkpoint resolve` / `striatum override-verdict`. Consolidate around the existing `island-recovery-panel`.
- **why**: With 8+ AI operators running, the human principal will be triaging constantly. The web UI exists; the surface isn't escalation-tuned yet.
- **touches**: `src/striatum/web/escalations.py` (extend), `src/striatum/web/templates/` (new escalation views), `src/striatum/web/frontend/src/islands/recovery-panel/` (extend or add escalation-inbox island), `src/striatum/daemon_rpc/` (any new read endpoints needed).
- **effort**: 2–3 weeks.
- **depends on**: P0 cluster complete (no point polishing UI during substrate cutover).
- **acceptance**: `/escalations` lists open escalations across all repos with a 5-click resolve flow; `/blockers` cross-run view exists; resolved escalations transition the parent run state observably.

### P1-SPEC-CULL

- **source**: Claude S4. (Codex and Gemini did not flag SPEC length.)
- **what**: Cull `docs/SPEC.md` from 1,810 lines to ~300 lines of contract-only material: exit codes, refusal semantics, the workflow JSON schema reference, the daemon RPC envelope shape, the front-matter schema kinds. Move operational/narrative content into per-RFC docs and PRD.
- **why**: SPEC is asked to be the source of truth (`AGENTS.md`, `README.md:130`) but at 1,810 lines is too long to read on every task. In practice the DDD doc (199 lines) is doing the orienting work. For team adopters, a tight SPEC is the contract; the long version is reference.
- **touches**: `docs/SPEC.md` (cull), `docs/PRD.md` (absorb narrative), `docs/rfcs/*` (absorb operational detail), `docs/INDEX.md` (re-classify SPEC).
- **effort**: 1–2 days.
- **depends on**: none.
- **acceptance**: `wc -l docs/SPEC.md` < 350; the doc references RFC files for operational detail rather than duplicating it.

### P1-SERVICE-COLLAPSE

- **source**: Claude Sm3.
- **what**: Collapse the 12 `src/striatum/service*.py` modules to three: `service.py` (HTTP entry + routing), `service_state.py` (connection/runtime state), `service_security.py` (request io + security + command policy).
- **why**: Twelve files for one service is a navigability problem and a sign the original module was split by accretion. Three is the right number.
- **touches**: all 12 `src/striatum/service*.py` files (consolidated), every test importing service internals (likely 15+ files), `src/striatum/cli/__init__.py`.
- **effort**: 1 day.
- **depends on**: P0-CI-GREEN (don't refactor against an unknown baseline).
- **acceptance**: 3 service modules; `mypy` and tests pass; no import path broken outside the consolidated module set.

### P1-CROSSREPO-TRIM

- **source**: Codex Concern 6 / R6. (Claude and Gemini did not flag.)
- **what**: Remove or unexport placeholder methods (`Prepare`, `ParticipantIntact`, the no-op `HumanCheckpoint`) on the Go cross-repo runner interface in `go/cmd/striatumd/main.go:391-439`. Keep tested cancellation and describe/list behavior. Block addition of these methods behind an accepted RFC.
- **why**: Placeholder interface methods imply features that don't exist. Future maintainers reading the interface will assume capabilities that aren't wired.
- **touches**: `go/cmd/striatumd/main.go`, `go/pkg/crossrepo/lifecycle.go`.
- **effort**: 4 hours.
- **depends on**: none.
- **acceptance**: cross-repo runner interface has only methods backed by tested implementations and active contract methods.

### P1-ADOPTER-READING-PATH

- **source**: Claude S8. (Codex addressed RFC purpose differently; Gemini did not flag.)
- **what**: Write `docs/ADOPTER_READING_PATH.md`: a curated 6-RFC reading list for a team adopting striatum. Suggested set: RFC 0019 (DDD foundations), RFC 0026 (lane attestation + byline honesty), RFC 0028 (long-running daemon + multi-repo), RFC 0030 (daemon RPC envelope), RFC 0043 (PG-as-substrate + daemon-required), RFC 0053 (human principal + terminology).
- **why**: Discoverability for adopters. The 72-RFC count is justified (forward design); the no-entry-point cost is not. Six RFCs explain how the system thinks; the rest are decision-trail.
- **touches**: `docs/ADOPTER_READING_PATH.md` (new), `docs/INDEX.md` (link), `README.md` (link).
- **effort**: 4 hours (mostly reading-list curation + a 3-line summary per RFC).
- **depends on**: none.
- **acceptance**: file exists; linked from README and INDEX; each RFC entry has a one-sentence "why this matters" and a one-sentence "what to take away."

### P1-VERSIONING-POLICY

- **source**: Claude S2 / §7 P2. (Codex and Gemini did not flag.)
- **what**: Switch from per-commit minor bumps to either (a) date-stamped versions (`v2026.05.18-1`) or (b) batched-weekly real releases. Document the policy in a top-level `RELEASING.md` or in CONTRIBUTING. Stop bumping `pyproject.toml` minor on every PR.
- **why**: 25 versions in 6 days isn't a release contract — it's a snapshot stream. Team adopters pin to tags and read CHANGELOG. The current cadence misleads both signals.
- **touches**: `RELEASING.md` (new), `CONTRIBUTING.md` (note), `pyproject.toml` (next version), `CHANGELOG.md` (batched-weekly going forward).
- **effort**: 1 hour to write policy; ongoing discipline.
- **depends on**: P0-CI-GREEN + P0-INSTALL-SMOKE (the new policy presumes a release has a meaningful definition).
- **acceptance**: a `RELEASING.md` exists; next 4 weeks show ≤ 1 minor bump per week with a meaningful changelog block.

## 5. P2 — smell / nice-to-have

### P2-COORDINATOR-ROLE-DELETE

- **source**: Claude Sm1. `docs/UBIQUITOUS_LANGUAGE.md:55`: "declared in every dogfood workflow but never actually claimed in any run."
- **what**: Delete the `coordinator`-as-claimed-session schema field, vocabulary entry, and unused validator paths. Update glossary and SPEC.
- **why**: Schema cruft for a feature that has never been used in the project's lifetime is a maintenance tax.
- **effort**: 2 hours.
- **depends on**: P0-LEGACY-SQLITE-COLLAPSE (it's referenced in legacy schema).
- **acceptance**: no schema column or workflow field references the unused coordinator-session path; tests pass.

### P2-CLI-INIT-SHIM-CULL

- **source**: Claude Sm2.
- **what**: Cull `src/striatum/cli/__init__.py` `_SYMBOL_MODULES` from 78 entries to whichever subset is actually still imported externally (likely < 15).
- **why**: 78 backwards-compat lazy-import entries is a smell about never having decided what's public.
- **effort**: 4 hours.
- **depends on**: P0-DISPATCH-SPLIT (the split changes which symbols re-export from where).
- **acceptance**: ≤ 20 entries in `_SYMBOL_MODULES`; tests pass; no external caller broken (grep across repo).

### P2-PARSER-GENERATION

- **source**: Claude S5 / §7 P3.
- **what**: Generate `src/striatum/cli/parser.py` (currently 1,343 LOC) from `contracts/daemon_methods.json`. Today, adding a verb requires editing parser + contract + Python handler + Go handler.
- **why**: Four-place ceremony per verb. Optional improvement.
- **effort**: 1 week.
- **depends on**: P0-DAEMON-PG-DELETE (the Python handlers either go away or move; parser generation gets easier after).
- **acceptance**: `cli/parser.py` shrinks to < 400 LOC of hand-written code plus a generated portion; adding a verb is one edit to `contracts/daemon_methods.json`.

### P2-CHANGELOG-REWRITE

- **source**: Claude Sm5.
- **what**: Rewrite `CHANGELOG.md` from "what happened that week" to "upgrade-impact prose": each version block lists exit-code changes, removed CLI verbs, schema migrations, behavior changes.
- **why**: 4,730 lines of essay. For team adopters, CHANGELOG must answer "what breaks if I upgrade."
- **effort**: 1 day (mostly compressing existing prose).
- **depends on**: P1-VERSIONING-POLICY (the cadence has to slow down or the rewrite is busywork).
- **acceptance**: each `## v` block ≤ 30 lines and answers "what breaks if I upgrade."

### P2-ACCEPTED-RISK-PERSISTENCE

- **source**: Codex §8 #3. Also in `docs/TODO.md:107-114` (blocked on product decision).
- **what**: Persist accepted risk decisions in PostgreSQL. Table keyed by repository, run, artifact/job, risk id, accepting role, timestamp, rationale. Surface in dashboard/read DTOs; make `workflow validate` lint check it.
- **why**: The lint refusal exists; the override path doesn't persist. Currently unblocking workflows is operator-side-only knowledge.
- **effort**: 3–5 days.
- **depends on**: P0 cluster complete; product decision on whether persisted accepted-risk is in or out of scope (currently 🟡 in TODO).
- **acceptance**: a new PG table + RPC method + dashboard surface for accepted-risk records; lint consults it before refusing.

### P2-AUTHORITY-MATRIX-GEN

- **source**: Codex R5.
- **what**: Generate authority matrix rows (method, capability, repo-scope, CLI route) from `contracts/daemon_methods.json`; keep `docs/architecture/COMMAND_AUTHORITY_MATRIX.md` as curated rationale only.
- **why**: Reduces hand-maintained duplication. Codex's framing: the matrix is for human judgment; the data is from the contract.
- **effort**: 2 days.
- **depends on**: P0-DAEMON-PG-DELETE (the matrix shrinks once Python handlers are gone).
- **acceptance**: `scripts/generate_authority_matrix_rows.py` exists; the rendered matrix doc has clearly-marked generated and hand-curated sections.

### P2-DOCS-AUTHORING-PATH

- **source**: Codex R7 (folded here as docs change, not separate item).
- **what**: Remove hand-edited-JSON and React Flow framing from `docs/WRITING_WORKFLOWS.md` and `docs/HOW_TO_HUMAN.md:968-980`. Position `striatum workflow generate` as the only documented authoring path.
- **why**: Workflow authoring is generator-first per maintainer; the docs should match.
- **effort**: 3 hours.
- **depends on**: none.
- **acceptance**: both docs lead with `striatum workflow generate` examples; legacy paths labelled "advanced" or removed.

## 6. Dependency map

The substrate-cutover items form a strict chain:

`P0-DISPATCH-SPLIT` → `P0-LEGACY-SQLITE-COLLAPSE` → `P0-DAEMON-PG-DELETE`.

These three together define substrate-cutover-done.

`P0-CI-GREEN` is parallel to the above (independent runway). `P0-INSTALL-SMOKE` blocks on `P0-CI-GREEN`. Together those two define release-readiness-done.

`P0-REPO-ROOT-CLEANUP` and `P0-BUNDLE-HYGIENE` are independent and small — land them in the first day to clear the room.

The P1 cluster mostly waits for substrate cutover. Exceptions:
- `P1-SPEC-CULL`, `P1-ADOPTER-READING-PATH`, `P1-CROSSREPO-TRIM` are independent and small.
- `P1-DELETE-EDITOR-ISLANDS` blocks on `P0-BUNDLE-HYGIENE` (no point deleting bundles inside a polluted directory).
- `P1-ESCALATION-UX` is the long-running follow-on after P0 cluster — start once cutover lands.
- `P1-SERVICE-COLLAPSE` blocks on `P0-CI-GREEN` for refactor-against-known-state.
- `P1-VERSIONING-POLICY` blocks on `P0-CI-GREEN` + `P0-INSTALL-SMOKE` (a meaningful release needs both).

P2 items are mostly independent and can land opportunistically.

Critical-path edge list:

- `P0-DISPATCH-SPLIT` must land before `P0-LEGACY-SQLITE-COLLAPSE`
- `P0-LEGACY-SQLITE-COLLAPSE` must land before `P0-DAEMON-PG-DELETE`
- `P0-LEGACY-SQLITE-COLLAPSE` must land before `P2-COORDINATOR-ROLE-DELETE`
- `P0-CI-GREEN` must land before `P0-INSTALL-SMOKE`
- `P0-CI-GREEN` must land before `P1-SERVICE-COLLAPSE` and `P1-VERSIONING-POLICY`
- `P0-INSTALL-SMOKE` must land before `P1-VERSIONING-POLICY`
- `P0-BUNDLE-HYGIENE` must land before `P1-DELETE-EDITOR-ISLANDS`
- `P0-DAEMON-PG-DELETE` must land before `P2-PARSER-GENERATION` and `P2-AUTHORITY-MATRIX-GEN`

## 7. What I'd defer indefinitely

- **Hosted-mode / team-mode / cloud-anything.** All three reviews and SPEC explicitly forbid. Stays out.
- **Full hand-off of all docs to generated content (Codex R5 in its full form).** The curated matrix carries human judgment that wouldn't survive generation. Keep manual control over rationale; generate only the data rows. P2-AUTHORITY-MATRIX-GEN captures the bounded slice; don't expand it.
- **AI-inferred build parallelization.** D015 in PRD deferred this. Still right to defer.
- **Plugin marketplace / external skill catalog.** Out of scope per RFC 0015 V1 ("skill bundles are self-contained, no external URLs"). Not even worth considering.
- **A second workflow-authoring path** (re-investing in the React Flow editor or a new TUI editor). The CLI generator is the canonical path; adding alternatives is a rejection of the maintainer's decision.
- **Transcript capture, even opt-in.** D028 invariant. Permanent.
- **Restoring SQLite as an operator-supported backend.** Even for "small users." Capability scopes and audit-chain row locks don't compose with SQLite's serialization model; supporting both substrates indefinitely is the substrate-migration drag the maintainer just escaped.

## 8. Open questions

These I could not resolve from the reviews + the working tree; they don't change P0 selection but they affect P1 sequencing.

- **What's the deletion-day for the Go-conformance parity tests?** Once Python `daemon_pg` is gone (P0-DAEMON-PG-DELETE), the Go conformance harness is testing the Go daemon against itself. The "parity" frame loses meaning. Likely the suite collapses into the regular multi-repo test set, but the test inventory has to be re-curated. Affects P0-DAEMON-PG-DELETE acceptance.
- **Does macOS GitHub Actions reliably support a daemon-bound Postgres install?** P0-INSTALL-SMOKE depends on this. If macOS runners don't work, the smoke job becomes Linux-only and the macOS guarantee shifts to manual pre-release.
- **Is the human-principal escalation flow actually firing in current dogfood runs?** `docs/TODO.md:107` (RFC 0062) shows the projection landed but the inbox is 🟡. P1-ESCALATION-UX is sized assuming the surface is being built ahead of consumption; if it's actively used today, scope and priority both increase.
- **Should `daemon.migrate` remain the RPC method name** now that the `daemon migrate` CLI command is retired? Codex raised this. If yes, document it as PG-migration-only. If no, rename the RPC method during P0-DAEMON-PG-DELETE.
- **Are dogfood ledger entries (66 runs in `docs/dogfood/`) actively read?** If yes, they should be linked from ADOPTER_READING_PATH. If write-mostly archives, they stay below the fold and the discoverability fix is just the 6-RFC list.
- **Tombstone SQLite files** (`.striatum/retired-local-state.tombstone`): Gemini and I both flagged. After legacy SQLite is fully deleted, should the daemon eventually auto-delete them on registration? Affects P0-LEGACY-SQLITE-COLLAPSE acceptance: does it include sweeping tombstones from already-registered repos, or only refusing new ones?
- **Is optional Git/PR integration (RFC 0067) still in-scope?** `docs/TODO.md:114` shows ⏳ blocked on product decision. If accepted, it's a P1 functionality add; if rejected, it stays in §7.

---

*Closing note.* The plan has 7 P0 items, 7 P1 items, 6 P2 items. The three reviews agree on the shape of P0; the divergence is at P1 and P2 where each reviewer noticed different surfaces. The substrate cutover is the only thing that matters in the next two weeks. Everything else waits for it.
