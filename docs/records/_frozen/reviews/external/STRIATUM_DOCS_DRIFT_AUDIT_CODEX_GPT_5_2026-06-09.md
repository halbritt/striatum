# Striatum Docs Drift Audit - Codex GPT-5 - 2026-06-09

## 0. Audit Basis

Target: `~/git/striatum`. Scope: whole-repo documentation survey
with deep dives into operator runbooks, CLI/authority docs, contributor
onboarding, web UI docs, and reusable examples/prompts.

Repo state at preflight: `main...origin/main` with a pre-existing dirty edit in
`docs/reference/cli-reference.md` removing `operator current-brief`. I preserved
that deletion and made additional narrow fixes in the same file because the CLI
reference is a current public contract and contained ledger-backed drift.

Authority: the user explicitly granted doc-fix authority by asking to execute
the audit and correct findings. I changed documentation/examples only. I did
not change code, generated outputs by running generators, services, migrations,
or repository state.

Commands run: `git status --short --branch`, `git diff --check`, `git diff
--stat`, `git diff --name-status`, `rg`, `sed`, `nl`, `wc`, `ls`, and static
file reads. Commands not run - not authorized: `make lint`, `make typecheck`,
`make test`, `make smoke`, `striatum --help`, `striatum workflow validate`,
Docker compose examples, docs generators, link checkers, and browser checks.

Files/evidence read: `README.md`, `AGENTS.md` instructions supplied by the
session, `Makefile`, `go/Makefile`, `.github/workflows/release.yml`,
`go/pkg/cli/localcommands/localcommands.go`, `go/pkg/cli/localcommands/daemon.go`,
`go/pkg/cli/routes/routes_generated.go`, `go/pkg/rpc/registry_methods.go`,
`contracts/daemon_methods.json`, `go/cmd/striatumd/main.go`,
`go/cmd/striatumd/web_service.go`, `go/pkg/webservice/service.go`,
`go/pkg/webassets/assets.go`, `go/pkg/db/sql/*`, `go/pkg/db/sql/owner/*`,
`docs/how-to/daemon-runbook.md`, `docs/reference/spec.md`,
`docs/reference/daemon-method-tables.md`, current runbooks, examples, prompt
index, and the prior inherited report
`STRIATUM_DOCS_DRIFT_AUDIT_GEMINI_3_5_FLASH_2026-06-08.md`.

Evidence tiers used: all ranked findings are `static-traced`; the prior Gemini
report was used as `inherited` triage input, not as sole evidence.

Depth ledger:

| Document / cluster | Class | Pass | Why selected | Docs read | Repo evidence read | Commands / inherited | Evidence | Residual risk |
|---|---|---|---|---|---|---|---|---|
| PostgreSQL and daemon lifecycle runbooks | operational | deep-audit | Highest operator risk; prior audit flagged wrong daemon commands | `postgres-transition.md`, `daemon-runbook.md`, `using-striatum.md` slices | Go daemon localcommands, routes, db SQL, Makefile | prior Gemini audit, static search | static-traced | Runtime output not executed |
| CLI and authority docs | contract | deep-audit | Public command/method contract | `cli-reference.md`, `command-authority-matrix.md`, generated method tables | generated routes, localcommands, RPC registry | static search | static-traced | Full CLI help not executed |
| Contributor and release docs | operational | deep-audit | Default contributor path was likely stale after Go-only transition | `CONTRIBUTING.md`, releasing doc | root/go Makefiles, release workflow, VERSION | static search | static-traced | CI not run |
| Web UI docs and glossary | architecture / operational | deep-audit | Stale `src/striatum/web/frontend` references were high confidence | `frontend-development.md`, spec Local Web UI, glossary | Go webservice/webassets source | static search | static-traced | Visual route behavior not executed |
| Examples and reusable prompts | example / lifecycle | deep-audit | Copy-paste commands and reusable prompt index affect operators | implementation-panel example, prompts README, scaffold prompt | Go CLI localcommands, real doc paths | static search | static-traced | Example validation not executed |
| RFCs, operator artifacts, archived docs, dogfood history | historical / aspirational | survey | Large corpus, many old paths expected by design | targeted `rg` hits only | lifecycle docs and AGENTS instructions | static search | static-traced | Some active TODO/roadmap stale references remain survey-only |

## 1. Verdict

Verdict: `MISLEADING_DRIFT` at discovery time. Confidence: medium.

Finding counts: 0 `BLOCKER`, 4 `SERIOUS`, 2 `MINOR`. All six ranked findings
were fixed as `SAFE_DOC_FIX` edits in this run.

Main reason: current, operator-facing docs still described the retired Python
runtime and pre-Go daemon surfaces as current: `daemon doctor`, `daemon
service`, `daemon start`, `workflow upgrade`, PyPI/Python release mechanics,
and a removed React/Vite frontend tree. These are default reader journeys and
public contracts, so the audit verdict remains `MISLEADING_DRIFT` even though
the safe fixes have now been applied.

## 2. Documentation Inventory

Canonical entrypoints: `README.md`, `ARCHITECTURE.md`, `docs/index.md`,
`docs/reference/spec.md`, `docs/decisions/decision-log.md`,
`docs/reference/ubiquitous-language.md`, `docs/how-to/how-to-agent.md`,
`docs/how-to/how-to-human.md`, and `docs/how-to/daemon-runbook.md`.

Operational docs: install and daemon setup in README, `using-striatum.md`,
`getting-started.md`, `daemon-runbook.md`, `postgres-transition.md`,
`blob-transition.md`, `lane-sandbox.md`, `CONTRIBUTING.md`, and
`docs/how-to/releasing.md`.

Contract docs: `docs/reference/cli-reference.md`,
`docs/reference/command-authority-matrix.md`,
`docs/reference/daemon-method-tables.md`, `contracts/daemon_methods.json`,
workflow docs, front-matter schemas referenced from the spec, and generated
method tables.

Examples and fixtures: `examples/implementation-panel-flow`, workflow catalog
examples, dev PostgreSQL compose profile, and historical RFC/dogfood fixtures.

Design/status corpora: RFCs, decision log, roadmap, TODO, operator brief,
operator plans/progress/artifacts, and archived issue/review materials. I
treated dated operator artifacts, archived issues, and older dogfood prompts as
historical unless an active index presented them as reusable/current.

Generated docs: `docs/reference/daemon-method-tables.md` is generated from
`contracts/daemon_methods.json`. I did not regenerate it because generator
execution was not authorized and the ranked fixes were to curated docs.

Skipped or unread in detail: most RFC bodies, archived external reviews,
operator artifacts, and historical dogfood workflows. Static searches show many
old Python paths there, but lifecycle semantics make most historical-ok.

## 3. Ranked Drift Ledger

| id | severity | drift class | documented claim | current reality | reader impact | evidence | smallest fix direction | fix status |
|---|---|---|---|---|---|---|---|---|
| DDA-001 | SERIOUS | wrong-command-or-workflow | `docs/how-to/postgres-transition.md` pre-fix claimed `striatum daemon doctor --apply-migrations`, `daemon service install/start`, and `daemon start` were current operator paths. | `go/pkg/cli/localcommands/localcommands.go` exposes `daemon install`, `uninstall`, `status`, `migrate-db`, and `owner-ddl`; `daemon-runbook.md` documents `systemctl --user` and foreground `striatumd`. | Operators following the runbook would hit unknown commands during first setup or repair. | static-traced; verification status: static only | Rewrite runbook around `daemon migrate-db`, `owner-ddl apply`, `systemctl`, foreground `striatumd`, `daemon status`, and `doctor`. | Fixed in `docs/how-to/postgres-transition.md`. |
| DDA-002 | SERIOUS | wrong-command-or-workflow | `CONTRIBUTING.md` pre-fix told contributors to use Python-era `.venv`, ruff, mypy, pytest, PyPI, `pyproject.toml`, and `src/striatum/__init__.py`. | Root `Makefile`, `go/Makefile`, `VERSION`, and `.github/workflows/release.yml` show Go-only build/test/release and GitHub release archives. | Contributors and release operators would run dead tooling or bump the wrong version source. | static-traced; verification status: static only | Replace Python/PyPI workflow with Go Makefile and `VERSION`/archive release path. | Fixed in `CONTRIBUTING.md`. |
| DDA-003 | SERIOUS | architecture-mismatch | `docs/how-to/frontend-development.md` pre-fix documented `src/striatum/web/frontend`, Vite/React islands, npm, and `make ui-*` as current. | `rg --files` shows no `src/`, no `package.json`, no `pyproject.toml`; current web assets/routes live in `go/pkg/webassets`, `go/pkg/webservice`, and `go/cmd/striatumd/web_service.go`. | UI contributors would search for deleted files and run unavailable commands. | static-traced; verification status: static only | Mark React/Vite guide retired and document current Go web surface. | Fixed in `docs/how-to/frontend-development.md` and glossary. |
| DDA-004 | SERIOUS | contract-mismatch | `docs/reference/command-authority-matrix.md`, `docs/how-to/how-to-human.md`, and `docs/reference/cli-reference.md` pre-fix described retired/unrouted CLI surfaces such as `workflow upgrade`, `workflow graph`, `daemon doctor`, `daemon service`, and special `doctor --first-run`. | Generated routes and localcommands do not expose those CLI commands; `workflow.upgrade` exists as daemon method metadata but not as a current Go CLI route. `doctor` accepts generic flags but has no first-run handler in `go/pkg/reads/doctor.go`. | Contract readers would design automation against commands the CLI does not provide. | static-traced; verification status: static only | Split current CLI from RPC-only methods; remove special first-run doctor claims; label retired commands. | Fixed in `command-authority-matrix.md`, `how-to-human.md`, `cli-reference.md`, and glossary. |
| DDA-005 | MINOR | example-drift | `examples/implementation-panel-flow/README.md` pre-fix used `PYTHONPATH=src python3 -m striatum.cli`; `workflow.json` pointed at `docs/INDEX.md`, `docs/HOW_TO_AGENT.md`, and other moved doc paths. | Current CLI is Go binary `striatum`; actual docs are lowercase paths under `docs/index.md`, `docs/how-to/`, and `docs/reference/`. | Users validating the example would copy dead commands and context docs would not resolve. | static-traced; verification status: static only | Update example command and context doc paths. | Fixed in example README and workflow JSON. |
| DDA-006 | MINOR | status-lifecycle-drift | `prompts/README.md` pre-fix listed `RFC_0026_0027_SCAFFOLD_PROMPT.md` under reusable prompts, while the prompt itself was marked `Status: historical/reference` and contained Python-era commands. | Prompt body declares historical status and contains stale `PYTHONPATH=src` commands and old doc paths. | A fresh agent could choose a historical dogfood prompt as a current scaffold. | static-traced; verification status: static only | Move the prompt to historical reference and warn to rewrite before use. | Fixed in `prompts/README.md`. |

## 4. Claim Checks

Verified-current:

- README daemon setup uses `daemon install --no-start`, `daemon migrate-db`,
  `systemctl --user start striatumd`, `daemon status`, and current repo add /
  workflow commands.
- `docs/how-to/daemon-runbook.md` matches the Go local daemon commands and
  foreground `striatumd` recipe.
- `docs/reference/spec.md` Local Web UI section already says the Go daemon
  mounts the web service and the richer Python-era pages are retired.
- `docs/how-to/releasing.md` already describes Go archives, `VERSION`, and the
  release workflow accurately.

Historical-ok:

- P001-P004 prompts, Engram incubation docs, archived issue docs, older
  dogfood/operator artifacts, and many RFC bodies contain old Python paths and
  command shapes, but AGENTS.md and prompt/index lifecycle text identify those
  corpora as historical unless a current index marks them reusable.

Aspirational-ok:

- TODO and roadmap entries often discuss future cleanup, RFC follow-ups, and
  old paths as work items. I did not grade every old `src/striatum` reference
  there as drift because the lifecycle is status/planning rather than a current
  operator instruction.

Stale-anchor-only / contained:

- Some active docs use friendly uppercase link labels while linking to the
  lowercase current path. I normalized the highest-exposure contributor/agent
  labels, but did not treat every label as a ranked finding where the link
  target was correct.

Unverifiable-gated:

- Actual CLI help output and example workflow validation would improve certainty
  on copy-paste help text, but CLI/test execution was not authorized.

## 5. Historical And Aspirational Docs

I interpreted archived reviews, issue handoffs, operator progress artifacts,
and dated dogfood outputs as historical provenance. Their old command lines and
Python paths are not drift by themselves.

The key lifecycle contradiction was `prompts/README.md`: it promoted a prompt
as reusable even though the prompt body itself declared it historical/reference.
That index-level presentation made the old Python command shape actionable and
therefore drift.

The command authority matrix was ambiguous because it still retained historical
`python authority` columns. I left those columns as retirement provenance but
updated the status, source inputs, direct Postgres table, CLI-only table, and
unrouted method rows so current readers do not mistake retired CLI commands for
supported ones.

TODO/roadmap still contain many survey hits for old source paths, daemon-doctor
phrasing, and prior status claims. Some are likely stale, but a safe correction
would require a separate lifecycle/status audit of active vs historical TODO
items rather than a narrow docs drift fix.

## 6. Verification

Static traces:

- Current local CLI commands: `go/pkg/cli/localcommands/localcommands.go` and
  `go/pkg/cli/localcommands/daemon.go`.
- Current generated CLI routes:
  `go/pkg/cli/routes/routes_generated.go`.
- Current daemon method registry:
  `go/pkg/rpc/registry_methods.go` and `contracts/daemon_methods.json`.
- Current release/build:
  `Makefile`, `go/Makefile`, `.github/workflows/release.yml`, and `VERSION`.
- Current web surface:
  `go/cmd/striatumd/web_service.go`, `go/pkg/webservice/service.go`, and
  `go/pkg/webassets/assets.go`.
- Current PostgreSQL schema/bundles:
  `go/pkg/db/sql/` and `go/pkg/db/sql/owner/`.

Commands run:

- Read-only/status/static commands listed in section 0.
- `git diff --check` passed with no whitespace errors.

Commands not run - not authorized:

- `make lint`, `make typecheck`, `make test`, `make smoke`.
- `striatum --help`, `striatum workflow validate`, `striatum doctor`, and
  example commands.
- Docker compose profile and any daemon/service execution.
- Docs generators and link checkers.

Evidence that could change severity:

- Running CLI help could reveal more stale `cli-reference.md` rows, but the
  main unsupported-command findings were already statically established from
  generated routes and localcommands.
- Running example validation could catch schema drift beyond the fixed context
  doc paths.

## 7. Optional Fixes

Fix authority was granted by the user. All edits were docs/example-only
`SAFE_DOC_FIX` changes: the current source of truth was clear, no behavior
change was required, and no generator execution was needed.

Files changed:

- `docs/how-to/postgres-transition.md`: replaced obsolete daemon-doctor/service
  runbook with current `migrate-db`, `owner-ddl`, `systemctl`, `striatumd`,
  `daemon status`, and `doctor` flow.
- `docs/how-to/frontend-development.md`: replaced retired React/Vite guide with
  current Go web-service guide.
- `CONTRIBUTING.md`: corrected Go-only contributor and release workflow.
- `docs/reference/cli-reference.md`: removed stale `doctor --first-run`
  contract text while preserving the pre-existing `operator current-brief`
  deletion.
- `docs/reference/command-authority-matrix.md`: updated source inputs,
  direct-Postgres bootstrap table, CLI-only table, and unrouted method rows.
- `docs/how-to/how-to-human.md`: removed `workflow upgrade` procedure and stale
  daemon packaging path.
- `docs/reference/ubiquitous-language.md`: marked retired frontend/toolchain and
  first-run smoke terms honestly; updated operator and harness-fragment claims.
- `docs/index.md`, `CLAUDE.md`, `docs/agents/domain.md`: updated summaries and
  current doc paths.
- `examples/implementation-panel-flow/README.md` and `workflow.json`: updated
  current validation command and context doc paths.
- `prompts/README.md`: moved the RFC 0026/0027 scaffold prompt out of reusable
  prompts and into historical reference.

Verification after fixes:

- Targeted `rg` scans over active/touched docs found no unqualified Python CLI,
  PyPI, `operator current-brief`, `doctor --first-run`, uppercase moved-doc path,
  or missing frontend-toolchain command claims. Remaining hits are explicitly
  labeled retired or historical.
- `git diff --check` passed.

Deferred unsafe/broader fixes:

- I did not regenerate generated docs.
- I did not edit broad historical operator artifacts or RFC bodies.
- I did not run CLI examples or tests.

## 8. Residual Risk And Follow-ups

Residual risk is medium. The active docs deep dives are corrected, but the repo
has a large historical/status corpus. TODO, roadmap, operator artifacts, and
some RFC status notes still contain old `src/striatum`, `daemon doctor`,
`daemon start`, and `workflow upgrade` phrases. Many are historical-ok, but a
dedicated status-lifecycle audit would be appropriate before a release.

Suggested follow-ups:

- Run an `INTERFACE_AUDIT.md`-style full CLI contract audit against current
  generated routes and `striatum --help` once command execution is authorized.
- Run example validation for `examples/implementation-panel-flow/workflow.json`
  once CLI execution is authorized.
- Run `REPO_HYGIENE.md` for stale historical docs/index placement if the team
  wants to reduce old Python/PyPI surface area.
- Consider regenerating any generated method/CLI docs from their source in a
  separate authorized pass.

## 9. Handoff Execution Addendum

Continuation session: 2026-06-09, Codex GPT-5.

Additional active-doc drift was found while executing the handoff and corrected
against the current Go source:

- `docs/tutorials/using-striatum.md` and `docs/tutorials/getting-started.md`:
  replaced retired day-zero `daemon doctor`, `daemon service`, `daemon start`,
  and `doctor --first-run` instructions with `daemon migrate-db`,
  `daemon owner-ddl apply`, `striatumd`/systemd startup, `daemon status`, and
  `doctor --verbose`.
- `docs/how-to/how-to-human.md`: replaced retired `init`, `adopt`, and
  `init --with-skills` examples with `repo add --init`, `skills install`, and
  `plugin install`.
- `docs/how-to/writing-workflows.md`: marked the RFC 0038 React Flow workflow
  editor as retired in the current Go web UI.
- `docs/how-to/blob-transition.md`: replaced retired `adopt`,
  `daemon doctor`, generic `invoke`, and `corpus verify` command shapes with
  current `repo add --apply-blob-creation`, `doctor --verbose`, and Go web API
  reads; explicitly marked the historical dogfood bulk-migration CLI wrapper as
  absent from the current Go CLI.
- `docs/reference/cli-reference.md` and `docs/reference/spec.md`: removed
  current-contract claims for unimplemented `corpus verify`, `archive verify`,
  and `archive inspect`; the current Go CLI exposes `corpus export` and
  `archive create`.
- `docs/reference/domain-driven-design.md`: replaced a stale `workflow graph`
  example with current local workflow-authoring wording.
- `go/pkg/rpc/error_catalog.go`, `go/pkg/admin/repo_init.go`,
  `go/pkg/mutations/corpus_migrate.go`, and
  `docs/reference/command-authority-matrix.md`: corrected the runtime
  `blob_apply_required` suggestion from retired `adopt` wording to
  `striatum repo add <path> --apply-blob-creation`.

Validation run in the continuation:

- `git diff --check` passed.
- `make -C go build` passed; this produced a source-built CLI because the
  installed `striatum` on PATH was stale and rejected `operator bootstrap`.
- `go test ./cmd/striatum ./pkg/cli/routes ./pkg/cli/routestest ./pkg/rpc ./pkg/reads`
  passed.
- `./go/bin/striatum workflow validate examples/implementation-panel-flow/workflow.json --json`
  passed with `valid: true`.
- `./go/bin/striatum operator bootstrap --markdown` ran successfully and
  returned a degraded but valid packet; the reported daemon doctor/worktree,
  operator brief, and skill-manifest warnings were pre-existing operational
  state outside this docs correction.
- `make test` passed after the docs and source-string fixes.
- A final targeted `go test ./pkg/admin` passed after a comment-only cleanup.

Not run:

- `make lint` was not run because `golangci-lint` is not installed on PATH.
- No daemon/service start, PostgreSQL migration, Docker, docs generator, or
  historical dogfood migration command was run.

## 10. Review + Method-Tables Consolidation Addendum

Review session: 2026-06-09, Claude Opus 4.8.

Verification of the inherited pass (all green): `make -C go build`, full
`make test`, `git diff --check`, `go generate ./...` (drift-clean), and the
authority-matrix error-catalog guardrail (`go/pkg/rpc/error_catalog_test.go`,
ran fresh). Every new command shape introduced by the docs pass was traced to
the live CLI surface and confirmed present; every retired shape
(`daemon doctor/service/start`, `serve --web`, `corpus verify`,
`archive verify/inspect`, `doctor --first-run`,
`corpus migrate-historical-dogfoods`) was confirmed absent from the generated
route table and is framed as retired. The four Go source-string edits are
accurate: `repo add <path> --apply-blob-creation` works because the CLI flag
parser (`go/pkg/cli/params/params.go`) normalizes any `--foo-bar` to `foo_bar`
and forwards it, and `repo.add` reads `apply_blob_creation`
(`go/pkg/admin/repo_init.go`). CONTRIBUTING release mechanics
(`VERSION`, `make release-check`/`smoke`, Go archives) match `Makefile` and
`.github/workflows/release.yml`.

One real defect found and fixed (DDA-007). The matrix edit (and the
pre-existing `docs/index.md:42`) pointed readers at
`docs/reference/daemon-method-tables.md` and called it "generated", but that
file was a stale May-28 leftover (198 method rows, missing 27 current methods
incl. `conversation.*`, `repo.write`, `repo.patch_*`, `process.run`,
`worktree.gc`, `run.integrate`, `work.packet_show`, `supervise.trajectory`,
`daemon.token.create`). The live `//go:generate` directive
(`go/pkg/rpc/registry.go:2`) wrote the current 225-row table to
`docs/architecture/DAEMON_METHOD_TABLES.md`. Per operator decision, consolidated
the generated doc forward under the Diataxis `reference/` tree:

- Retargeted the `markdown-tables` `//go:generate` directive to write
  `docs/reference/daemon-method-tables.md`.
- Fixed the `markdown-tables` generator header
  (`go/pkg/cli/routergen/main.go`) to credit `go/pkg/cli/routergen` instead of
  the retired `scripts/generate_daemon_method_tables.py`.
- Regenerated `docs/reference/daemon-method-tables.md` (now 225 rows, corrected
  header) and removed `docs/architecture/DAEMON_METHOD_TABLES.md`.
- Updated `docs/decisions/decision-log.md` D108 (an accepted, present-tense
  invariant entry) to the relocated `reference/` paths for both
  `command-authority-matrix.md` and `daemon-method-tables.md`.

Deliberately NOT rewritten (point-in-time records with their then-correct
paths, consistent with the historical-ok policy in section 5): the
`docs/_archive/reviews/*` external reviews, dated operator
artifacts/workflows/plans/gate-prompts, the RFC 0060 body, and the Python-era
milestone bullets in `roadmap.md`/`todo.md` (those bullets cite the retired
Python generator inline; updating only a path would create a mixed
Python/Go statement). These now hold dangling links to the relocated file by
design; their resolution belongs to a separate status-lifecycle pass.

Residual / follow-ups:

- The `rpc-registry` generator header (`go/pkg/cli/routergen/main.go` and the
  emitted `go/pkg/rpc/registry_methods.go`) still credits the retired
  `scripts/generate_go_rpc_registry.py`; left untouched to avoid widening this
  pass into an unrelated generated file.
- `--apply-blob-creation` works but is not listed in `repo add --help`
  (`go/pkg/cli/routes/usage.go` `repo_add` group); the new error strings now
  reference it, so adding it to the usage table would make help self-consistent.
- `make lint` still not run (`golangci-lint` absent on PATH).

## 11. Status-Lifecycle Pass + "The Rest" Closeout Addendum

Continuation session: 2026-06-10, Claude Opus 4.8. This discharges the
"separate status-lifecycle pass" deferred in section 10 and the section 8
follow-ups. Items 1, 2, 5, 6 of the handoff landed earlier
(`dcc141ce` rpc-registry header + `repo add --apply-blob-creation` help;
`5da9cec8` this ledger archived/generalized; `golangci-lint` v2.12.2 installed,
`make -C go lint` = 0 issues).

Ground truth re-established before editing (the prior round had one stale
premise): `src/` is entirely gone (0 tracked files, absent on disk), the
Python generator scripts are gone, and `contracts/daemon_methods.json` is
present and the live generation source. `striatum daemon start` and
`daemon doctor` are confirmed retired; `daemon status`/`migrate-db` remain.

Item 3 — active-vs-historical resolution:

- `docs/reference/todo.md` (Status: active): F35 and F36 marked closed — the
  whole `src/striatum/web/{static,static/build,templates,frontend}` Node/Vite
  surface was deleted in the RFC 0078 cutover (not embedded), so the minimal
  server-rendered Go surface (`go/pkg/webassets`, `//go:embed`) is the product
  and no web-UI decision is pending. Item 2's dead Python `workflow.py`/
  `repo_policy.py` file:line citations were repointed to the Go validator
  `go/pkg/workflowauthoring/workflow.go`. Item 25's stale "Python CLI may remain
  a client" / `daemon start` framing was closed (systemd-managed `striatumd`).
- `docs/decisions/decision-log.md` D108: the present-tense reference to the
  deleted `tests/architecture/test_authority_guardrails.py` was replaced with
  the four live Go guardrails (`registry_contract_test.go`,
  `routes_freshness_test.go`, `error_catalog_test.go`, `routes_test.go`), and
  the entry now states honestly that the curated matrix's human-classified
  columns are review-owned and not directly drift-tested.
- Dangling-link batch decision — LEAVE, now recorded so it is not re-deferred:
  the 15 remaining links to `architecture/DAEMON_METHOD_TABLES.md` all sit in
  point-in-time/frozen records — the four `docs/_archive/reviews/*` external
  reviews, the dated operator artifacts/workflows/plans/gate-prompts (all for
  completed RFCs 0050/0076/0078, which no agent re-runs), the RFC 0060 body
  (which also still says "Python loads METHOD_REGISTRY", so a path-only fix
  would be a mixed-vintage Frankenstatement), `docs/reference/roadmap.md` (a
  banner-flagged frozen `v1.57.0` snapshot whose own header says "treat the
  sequencing below as historical"), and `todo.md`'s struck-through item-50
  changelog bullet. Per the section 5/section 10 historical-ok policy these are
  dangling by design; the file was moved, not deleted, and the links were
  correct when written.

Item 4 — section 8 follow-ups:

- `examples/implementation-panel-flow/workflow.json` validates
  (`workflow validate --json` → `valid: true`); a deeper run needs a live
  daemon and was not exercised.
- CLI contract audit against generated routes + live `striatum --help`: fixed
  two verified `cli-reference.md` drifts — removed the phantom `recovery watch`
  (no route, no localcommand, `unknown command` at runtime; the other eight
  `recovery *` verbs are real) and documented the previously-undocumented
  run-control verbs `run pause|resume|cancel|retry-job|integrate` (verified
  present in `routes_generated.go` and live help). NOT changed: `repo add
  --no-migrate` (real, accepted compatibility flag) and the `adapter` section
  (already framed as retired) — both were flagged by a fan-out auditor but
  disproved on direct check. Remaining coverage gaps (the `escalation`,
  `interrogation`, and `conversation` command groups are undocumented in
  `cli-reference.md`) are a bounded follow-up; left for a dedicated CLI-doc
  pass rather than bulk-edited here, since the active reference doc warrants
  per-claim verification. The `REPO_HYGIENE.md`-style historical-surface sweep
  remains optional/deferred.

Validation: `git diff --check` clean; no generated file or generator was
touched, so the `go generate ./...` drift gate is unaffected; the
authority-matrix guardrail (`go test ./pkg/rpc`) does not read any edited file.

## 12. Dedicated CLI-Reference Pass Closeout

Continuation session: 2026-06-10, Claude Opus 4.8. This discharges the bounded
CLI-doc follow-up deferred in section 11 item 4. Method: built `./go/bin/striatum`
(v2.31.0) and reconciled every `<cmd> [sub] --help` against
`routes_generated.go` + `localcommands.go`; no auditor summary was trusted
without a direct source/runtime check.

Reverse-direction stale sweep (higher priority) came back **clean**: every
`striatum <verb>` string in `cli-reference.md` maps to a live verb. The five
sweep hits were all correct — the deliberately-retired `adapter run` section and
four negative "does not exist" statements (`daemon sweep`, `keys`, `serve`, and a
`striatum install supports` regex artifact). No documented-but-removed command
remained after the section 11 `recovery watch` removal.

Forward gaps closed in `cli-reference.md` (commit `8cb3b0d1`, 27 verbs):
- New `## Mediated repository and process surfaces (RFC 0099)` section:
  `repo write`, `repo patch-preview`, `repo patch-apply`, `process run`,
  `scope-check`, `work claim-override`.
- New live-collaboration sections: `## Conversation (RFC 0086)` (open/say/close/
  list/show), `## Interrogation (RFC 0082/0083)` (open/ask/answer/close/list/
  show), `## Trajectory (RFC 0081)` (export/watch — distinct from
  `supervise trajectory`), `## Escalation and principal inbox (RFC 0053/0102)`
  (`inbox` + escalation list/show/resolve).
- New `## Codex launcher` section (`striatum codex`).
- Added to existing sections: `daemon token-create`; `recovery invalidate-job`
  (RFC 0118 P1-6 — landed `a73fddfe` *after* this handoff was written, so absent
  from its surface listing; caught by the live-help re-derivation); `work
  packet-show`; and the `repo add` flag set `--display-name` /
  `--apply-blob-creation` / `--no-migrate`.

Not changed (verified adequate, not gaps): `run pause|resume|cancel|retry-job|
integrate` and `workflow accept-risk|accepted-risks` are already documented in
prose with capabilities and argument shapes; the `adapter run` retired-section
is correct as-is.

Validation: `git diff --check` clean; code fences balanced (42); guardrails
`go test ./pkg/rpc ./pkg/cli/routes ./pkg/cli/routestest` green (pure-doc edit is
test-neutral, run as a sanity). The CLI-reference audit deferred since the
docs-drift effort is now complete.
