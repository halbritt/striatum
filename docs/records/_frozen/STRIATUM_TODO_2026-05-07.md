# striatum TODO — ARCHIVED

Status: superseded

> **ARCHIVED (2026-06, D232).** This product-improvement tracker is no longer
> maintained. It is **superseded by [`docs/operator/BRIEF.md`](../../operator/BRIEF.md)**
> plus the live operator cold-start: run **`striatum operator bootstrap --markdown`**
> and follow its `next_actions` and bounded `reading_plan`. Open and active work
> lives in the open GitHub issues; the RFC frontier lives in the RFC index
> ([`docs/rfcs/README.md`](../../rfcs/README.md)). This snapshot was last dated
> `2026-05-07`; the repository is now well past it. The full historical TODO is
> preserved verbatim below the divider for provenance, and its stable numbered
> IDs keep resolving for older references — treat every status entry as stale.

---

## Historical TODO (archived, stale — kept for provenance)

# striatum TODO

Status: superseded
Date: 2026-05-07
author: coordinator-codex-gpt-5.5-001

This list tracks product improvements after the V1 split. Engram remains the
first validation fixture, not the product boundary. Numbered IDs are stable
so external references keep resolving even as items move between sections.

## Status Snapshot

| ID | Item | Status |
|---:|------|:------:|
| R1 | Public repository name and history-preserving split | ✅ done |
| R2 | Standalone repo scaffold | ✅ done |
| R3 | `--repo` flag replaces `TARGET_REPO=..` | ✅ done |
| R4 | Standalone metadata, license, CI, fresh-clone smoke | ✅ done |
| R5 | Engram retains incubation copy + pointer | ✅ done |
| 1 | Process adapter (single-shot + supervised) | ✅ done for current scope |
| 2 | Adapter constraint enforcement | ✅ done for process adapter scope |
| 3 | Workflow authoring tooling | ✅ done |
| 4 | Human-checkpoint UX | ✅ done |
| 5 | Decision-artifact support | ✅ done |
| 6 | Artifact schema (front matter) | ✅ 8 kinds + open registry |
| 7 | Redaction tests | ✅ synthetic injection coverage landed |
| 8 | Recovery commands | ✅ done |
| 9 | TUI / local dashboard | ✅ done |
| 10 | Local API and MCP | ✅ done |
| 11 | Worktree isolation (RFC 0008) | ✅ done |
| 12 | Richer fixture suite | ✅ done |
| 13 | Replace bootstrap scripts | ✅ done |
| 14 | Packaging and release | ✅ done |
| 15 | `run summary` polish | ✅ done |
| 16 | Keep generic language current | ✅ current sweep done; standing guardrail |
| 17 | Legacy SQLite migration fixture (RFC 0006/D094) | ✅ done |
| 18 | Workflow type catalog and chooser | ✅ done |
| F1 | Run historical bootstrap as runner workflow | ✅ done |
| F2 | Fuller publication policy | ✅ done |
| F3 | Round-6 RFC 0002 + 0003/0004/0005 follow-up | ✅ done |
| F4 | RFC 0010 V1 (tool harness profiles, dogfood-003) | ✅ done |
| F5 | RFC 0014 V1 (process adapter completion guarantees, dogfood-005) | ✅ done |
| F6 | RFC 0012 V1 (local service API, dogfood-006) | ✅ done |
| F7 | RFC 0013 V1 (local web UI, dogfood-007) | ✅ done |
| F8 | RFC 0016 V1 (dashboard dependency graph, dogfood-008) | ✅ done |
| F9 | RFC 0015 V1 (self-contained agent skills, dogfood-009) | ✅ done |
| F10 | RFC 0017 V1 (README + docs reorganization, dogfood-010) | ✅ done |
| F11 | RFC 0015 step 3 (codex + gemini skill profiles, dogfood-011) | ✅ done |
| F12 | RFC 0016 step 3 (Unicode fancy + --graph-orient, dogfood-012) | ✅ done |
| F13 | RFC 0013 step 7 (web UI mutation buttons, dogfood-013) | ✅ done |
| F14 | RFC 0020 V1 (autonomous recovery sweeper, dogfood-014) | ✅ done |
| F15 | RFC 0020 step 3 (`recovery watch` foreground scheduler over daemon sweep, dogfood-015) | ✅ done |
| F16 | RFC 0018 V1 (review postures, dogfood-016) | ✅ done |
| F17 | RFC 0021 V1 (DDD layout scaffold, dogfood-017) | ✅ done |
| F18 | RFC 0018 step 3 V1.5 (verdicts.posture + introspection, dogfood-018) | ✅ done |
| F19 | RFC 0021 V1.5 (--force + --dry-run, dogfood-019) | ✅ done |
| F20 | RFC 0022 V1 (web UI redesign, dogfood-020) | ✅ done |
| F21 | RFC 0023 V1 (web chat + view + artifact md, dogfood-021) | ✅ done |
| F22 | RFC 0023 V1.5 (chat tool use + briefing, dogfood-022) | ✅ done |
| F23 | RFC 0024 V1 (workflow browser, dogfood-023) | ✅ done |
| F24 | RFC 0024 V1.5 (visual builder, dogfood-024) | ✅ done |
| F25 | RFC 0024 V2 (run-now + If-Match + field-level errors, dogfood-025) | ✅ done |
| F26 | RFC 0024 V3 (cancel run + dirty-tree visibility, dogfood-026) | ✅ done |
| F27 | RFC 0024 V4 (pause/resume + per-job cancel/retry, dogfood-027) | ✅ done |
| F28 | RFC 0025 V1 Step 1 (claude_code plugin, dogfood-028) | ✅ done |
| F29 | RFC 0025 V1 Steps 2+3 (codex + gemini plugins, dogfood-029) | ✅ done |
| F30 | RFC 0026 V1 + RFC 0027 Phase 2 guardrails (dogfood-030) | ✅ done |
| F31 | RFC 0028 V1 registry-backed multi-repo read/sweep slice (dogfood-031) | ✅ done |
| F32 | RFC 0030 + RFC 0031 V2 RPC/supervision/apply foundation (dogfood-034) | ✅ done |
| F33 | RFC 0033 V2 system-Postgres daemon substrate (dogfood-033) | ✅ done |
| F34 | RFC 0032 V2 cross-repo workflows + MCP mutation capabilities (dogfood-035) | ✅ done |
| F35 | RFC 0034 V1 workflow generator + template catalog (dogfood-036) | ✅ done |
| F36 | RFC 0036 V1 MCP harness + chat workflow generation tools (dogfood-038) | ✅ done |
| F37 | RFC 0035 V1 multi-repo test harness (dogfood-037) | ✅ done |
| F38 | RFC 0037 V1 web UI ergonomic improvements (dogfood-039) | ✅ done |
| F39 | RFC 0040 V1 MCP-driven dogfood harness (operator-side slice; dogfood-040) | ✅ done |
| F40 | RFC 0038 V1 web UI feature additions + frontend toolchain (dogfood-041) | ✅ done |
| F41 | RFC 0039 V1 Steps 1+2 Go daemon core (dogfood-042 Track A) | ✅ done |
| F42 | RFC 0044 draft Engram Phase 1 implementation spec (dogfood-042 Track B) | ✅ done |
| F43 | RFC 0042 draft repo-local state to Postgres (dogfood-042 Track C) | ✅ done |
| F44 | RFC 0045 V1 multi-phase workflow schema + React Flow editor (dogfood-043) | ✅ done |
| F45 | RFC 0040 V1.5 daemon-dispatch + composite tools + watcher (dogfood-044) | ✅ done |
| F46 | RFC 0038 V1.5 web UI integration gaps (F1-F4 + supply-chain, dogfood-045) | ✅ done |
| F47 | RFC 0044 V1 Striatum-side corpus export (dogfood-046; Engram-side separate) | ✅ done |
| F48 | RFC 0039 V1.5 Go daemon F1-F5 deltas (dogfood-047; D101 override) | ✅ done |
| F49 | RFC 0043 V1 Postgres-as-sole-substrate + daemon-required (dogfood-048; D102 override) | ✅ done |
| F50 | RFC 0050 V1+V1.5+V2 operator UI rework (dogfoods 054/054b/055/055b/056; v1.46.0-v1.48.0) | ✅ done |
| F51 | v1.48.1 wrapper auth fix — closes 10+ instance claude/gemini no-publish stall (validated by gh-16) | ✅ done |
| F52 | v1.48.2 CI green — Python typecheck + Go matrix pin (6 days of red closed) | ✅ done |
| F53 | `docs/issues/<N>/` GH-issue-driven workflow type (gh-16 first instance, accept verdict) | ✅ done |
| 31 | RFC 0043 V1.5 follow-up | ✅ done |
| 33 | RFC 0042 V1 run-list workflow identity | ✅ done |
| 34 | RFC 0046 V1 lane evidence guard at publish-artifact | ✅ done |
| 35 | RFC 0047 V1 decision-record propagation + `compromised` run state | ✅ done |
| 36 | RFC 0048 daemon-side substrate migration (Phases A+B+C + V1.5 hardening + migration 0006) | ✅ done (v1.55.0) |
| 37 | RFC 0049 interactive claude lane via MCP (experimental) | 💤 shelved |
| 38 | RFC 0050 follow-ups — GH #9-13 V2 surface findings | ✅ done |
| 39 | RFC 0051 V1 auto-finalize from frontmatter (downgraded urgency post-v1.48.1) | ✅ done (D133 default-on cutover) |
| 40 | GH #14 — recovery cannot clear terminal-run `process_exit_nonzero` blocker | ✅ done |
| 41 | GH #15 — docs clarify PostgreSQL transition guidance | ✅ done |
| 42 | GH #17 — Striatum doc consistency for Engram memory integration | ✅ done |
| 48 | Architecture remediation Phase 0 — command authority matrix and fallback guardrails | ✅ done |
| 49 | RFC 0059 Architecture remediation Phase 1 — close production SQLite fallback | ✅ done |
| 50 | RFC 0060 Architecture remediation Phase 2 — single daemon method contract source | ✅ done |
| 51 | Architecture remediation Phase 3 — daemon core strategy decision | ✅ done |
| 52 | RFC 0061 Architecture remediation Phase 4 — daemon-first web service | 🟡 core web/API + artifact reads daemon-routed |
| 53 | RFC 0062 Architecture remediation Phase 5 — real escalation inbox | 🟡 projection + typed inbox table + escalation artifact linkage + blocker payload schema landed |
| 54 | RFC 0063 Architecture remediation Phase 6 — hardened PTY supervision | ✅ done |
| 55 | RFC 0064 Architecture remediation Phase 7 — workflow risk lint and review diversity enforcement | ✅ done |
| 56 | Architecture remediation Phase 8 — auto-finalize from front matter | ✅ D133 default-live cutover landed with explicit workflow opt-out |
| 57 | RFC 0065 Architecture remediation Phase 9 — UI packaging and bundle cleanup | ✅ done; chunking monitor only |
| 58 | RFC 0059 Architecture remediation Phase 10 — day-zero setup improvements | ✅ done |
| 59 | RFC 0059 RFC 0066 Architecture remediation Phase 11 — replay, archive, and corpus v2 foundations | ✅ done for core; optional external consumer UX remains out of core |
| 60 | RFC 0059 RFC 0067 Architecture remediation Phase 12 — optional Git/PR integration | ✅ local core done; hosted providers remain out of core |
| 61 | RFC 0068 Go production daemon port and Python daemon retirement | ✅ done |
| 62 | RFC 0069 PostgreSQL-only daemon-global surfaces | ✅ done; guardrails cover future probes |
| 63 | RFC 0070 daemon client/service boundary completion | ✅ done; primitive daemon methods are supported path |
| 64 | RFC 0071 operator diagnostics and cutover evidence | ✅ accepted diagnostic slice done |
| 65 | RFC 0058 operator progress surface | ✅ done |
| 66 | Decision/RFC supersession hygiene and duplicate decision-id cleanup | ✅ done |
| 67 | RFC 0130/RFC 0075 MCP cutover and tmux-observable sessions | ✅ done |
| 68 | RFC 0078 Go-only runtime and Python removal | ✅ done |

Legend: ✅ done · 🟡 most done (sub-tasks remain) · ⏳ open/blocked · 💤 shelved

## Completed

### Repo Split (2026-05-07)

- ~~**R1.** Public repository name `striatum` and the legacy Python
  distribution name. Engram
  extraction tagged `striatum-extraction-2026-05-07`; history-preserving split
  from the former `agent-runner/` prefix.~~

- ~~**R2.** Standalone repo root scaffolded with `src/`, `tests/`, `docs/`,
  `examples/`, `prompts/`, `scripts/`, `README.md`, `Makefile`,
  `pyproject.toml`, `.gitignore`. Engram dogfood material retained as redacted
  validation history.~~

- ~~**R3.** Removed `TARGET_REPO=..` as the primary usage path. Replaced with
  the `--repo` CLI flag pattern.~~

- ~~**R4.** Standalone metadata: Apache-2.0 license, contribution notes,
  changelog, supported Python versions, CI, and
  `scripts/fresh_clone_smoke.sh`.~~

- ~~**R5.** Engram retains the incubation copy as historical provenance plus
  a pointer to the standalone repository until the owner chooses how to
  archive, subtree, or submodule it.~~

### Product Improvements (delivered)

- ~~**3. Workflow authoring tooling.** All authoring verbs ship: `workflow
  validate`, `plan`, `graph` (mermaid/json/dot), `init` with styles,
  `upgrade` (`--force` / `--dry-run` / `--add-phases` / `--apply`),
  `templates list/show`, `generate` (shape + lane-set + artifact-root +
  modifiers), and stateful `run graph`. Implementations live in
  `src/striatum/workflow.py:259-380` plus
  `src/striatum/workflow_generator/{core,catalog,write}.py` (1266 lines).
  Validator covers cross-job artifact path collisions,
  write-scope/forbidden-path overlap, artifact-in-write-scope, unsound
  cycle target, parallel-group repo_write/review-only consistency, and a
  `needs` deprecation warning. Lint-style warnings already surface through
  the `warnings` channel. Deferred minor: dedicated `workflow lint` verb
  separating style from validation errors.~~

- ~~**4. Human-checkpoint UX.** `status` and `why` carry decision context,
  affected jobs, unblock path, and next actions. `striatum checkpoint resolve
  --blocker-id <id> --action {continue|cancel} [--decision-id <id>]` is the
  explicit operator resume/cancel surface; `continue` requeues the affected
  job, `cancel` transitions it to `canceled`, and the optional `--decision-id`
  links the resolution to a recorded decision artifact.~~

- ~~**5. Decision-artifact support.** `striatum decision record` writes
  durable Markdown with `striatum.decision.v1` front matter for `accepted`,
  `rejected`, and `accepted_with_follow_up` outcomes, no active lease
  required.~~

- ~~**8. Recovery commands.** `recovery stale-leases` and `recovery
  requeue-stale` distinguish review-only from repo-write work and refuse
  repo-write requeues. `striatum recovery cancel-job --run-id <id> --job-id
  <id> --reason <text> [--cascade]` is the explicit operator cancel for
  non-terminal jobs; refuses terminal-state jobs and refuses jobs with
  blocked dependents unless `--cascade` is set.~~

- ~~**9. Compact terminal dashboard.** `striatum dashboard --run-id <id>
  [--refresh N] [--once]` renders a single-screen view of run state, job
  counts, verdicts, blockers, claimable work, next actions, and recent events
  using only the standard library.~~

- ~~**10. Local API and MCP.** `striatum.api.invoke` wraps the same
  parser/dispatcher without direct SQLite writes; the local stdio JSON-RPC
  wrapper speaks `Content-Length` framing with line-delimited fallback, keeps
  resource reads plus explicit `striatum/invoke`, and no longer advertises or
  executes CLI-shaped aliases through `tools/list` / `tools/call`.
  Daemon-mapped local service/chat/manual invocations route through daemon RPC
  in production and retain `api.invoke` only for unmapped local authoring or
  explicit test-fixture compatibility.~~

- ~~**11. Per-job git worktree isolation (RFC 0008, accepted).** Lanes opt
  in with `worktree_isolation: per_job`; work packets carry
  `worktree_required: true` and the `striatum worktree create` invocation.
  Migration version 2 adds `job_worktrees`. `publish-artifact` reads from the
  worktree but records the logical repo-relative path; lease expiry marks
  worktrees `abandoned` for operator inspection; `doctor` flags orphaned and
  missing-on-disk worktree rows.~~

- ~~**12. Richer fixture suite beyond Engram.** Generic docs-only review,
  code-change with one-shot needs_revision cycle, single-review failed
  revision opening a configured human checkpoint, explicit human-checkpoint
  flow resolved by `decision record`, and adapter-unavailable rejected at
  validation. All listed gaps delivered.~~

- ~~**14. Packaging and release.** `pyproject.toml` declares setuptools
  build, console scripts (`striatum`, `striatumd`), and `[dev]`
  Python lint/type/test extras. `.github/workflows/ci.yml` ran the Python
  lint/type tools + the Python test suite + UI build/test + `release_metadata_check.py` + `package_smoke.sh`
  + `fresh_clone_smoke.sh` across ubuntu/macOS and py3.11/py3.12; the smoke
  scripts exercise daemon/PostgreSQL state when setup is available and skip
  instead of creating SQLite fallback state when it is not.
  `.github/workflows/release.yml` builds Python package artifacts on `v*` tags, runs
  `twine check --strict`, publishes to PyPI via OIDC trusted publishing,
  and cuts a GitHub Release. Documentation policy items (signing,
  security disclosure, release cadence) tracked separately.~~

- ~~**15. `run summary` polish.** Markdown groups verdicts by review job with
  attempt counts, appends the structured author byline to each artifact,
  surfaces recorded vs. current git branch with `(MISMATCH)` when they
  differ, and prints a Timing block (`created_at`, `started_at`,
  `completed_at`, wall-clock `duration`).~~

- ~~**17. SQLite migration system (RFC 0006, accepted).** Historical pre-D094
  repo-local SQLite schema versioning shipped with `PRAGMA user_version`,
  transactional migrations, and newer-database refusals. Current production
  state is daemon-owned Postgres; the legacy implementation code and fixtures
  have since been deleted.~~

- ~~**18. Workflow type catalog and chooser.**
  `src/striatum/workflow_generator/{catalog.py,core.py,write.py}` plus
  `src/striatum/workflow_templates/catalog.json` provide the generator
  core. CLI verbs `workflow templates list/show` and `workflow generate`
  are wired through `src/striatum/cli/parser.py:255-267`. Web chooser
  lives at
  `src/striatum/web/frontend/src/islands/workflow-chooser/WorkflowChooser.tsx`
  with template `src/striatum/web/templates/workflow_new.html` and tests
  `workflow-chooser*.test.ts*`. Chat-assisted scaffolding ships via RFC
  0036 V1 (`generate_workflow_preview`, `generate_workflow_write`).
  Future target-repo catalog extensions remain a separate decision.~~

## In Progress

1. ~~**Process adapter.**~~ ✅ Done for current scope: single-shot `adapter run`
   and long-lived supervision are production daemon/PostgreSQL-backed surfaces;
   legacy local-state adapter/supervisor code has been deleted. Supervised
   wrappers remain under `.striatum/bin/{claude,codex,gemini}-supervised-wrapper.sh`:
   `process_supervisors` table (migration version 4), `striatum supervise
   start | send | stop | status | list`, lazy lease-expiry recovery that
   flags supervisors `lost` without auto-killing the OS process,
   supervised-aware `claim-next` that auto-delivers the freshly built packet
   through the supervisor's stdin pipe and lazily marks pipe-missing/write-fail
   supervisors `lost`, `doctor` checks for dead pids and missing stdin pipes,
   PTY helper transport, and the explicit `supervision.stdin_delivery`
   value `"one_shot_eof"` for single-prompt commands that require stdin EOF.
   Runner-owned stall alarms/blockers now surface attached supervisors whose
   heartbeat/progress is stale, including `liveness: "stalled"` status,
   `doctor`/`status` read-model warnings, and
   `heartbeat_stall_lease_expired` blockers when the lease expires without
   auto-killing the OS process. Follow-up PG/helper integration coverage now
   launches the real Go helper and verifies start, send, acknowledgement,
   status drain, and agent-exit event ingestion across the Python/Go boundary;
   wrapper fixtures cover Claude, Codex, and Gemini. **Remaining:** no known
   Phase 6 supervision coverage debt; future adapter work belongs to new
   transport or sandbox/worktree adapter decisions.

2. **Adapter constraint enforcement.** Workflow validation supports lane
   `required_enforcement` and rejects lanes whose adapters cannot satisfy it
   (lane-constraint validation lives in the Go workflow validator
   `go/pkg/workflowauthoring/workflow.go`, which rejects unknown constraint
   keys + unsatisfiable enforcement levels; the retired Python
   `src/striatum/workflow.py`/`repo_policy.py` were deleted in the RFC 0078
   Go-only cutover). The four-level model (`enforced`,
   `advisory_strict`, `advisory`, `unsupported`) is in place; the process
   adapter graduates `network` and `repo_scope` to `advisory_strict` via
   proxy-env scrubbing and sentinel env vars. The 2026-05-23 TODO 2 closure
   workflow added focused validator coverage and closes the current
   process-adapter scope. Mechanically promoting `network`/`repo_scope` to
   `enforced` requires a future sandbox/worktree adapter RFC; it is not
   lingering TODO 2 work.

6. **Artifact schema support.** Optional Markdown `author:` metadata is
   machine-validated; per-kind front-matter schemas exist for eight kinds:
   `decision` (`striatum.decision.v1`), `finding` (`striatum.finding.v1`),
   `findings_ledger` (`striatum.findings_ledger.v1`), `synthesis`
   (`striatum.synthesis.v1`), `support_ledger` (`striatum.support_ledger.v1`,
   RFC 0003), `action_item_ledger` (`striatum.action_item_ledger.v1`, RFC
   0004), `harness_improvement_proposal`
   (`striatum.harness_improvement_proposal.v1`, RFC 0005), and
   `escalation` (`striatum.escalation.v1`, RFC 0053). Migration version
   5 dropped the SQL `CHECK` on `artifact_kind`; allowed kinds now live in
   `striatum.artifacts.ALLOWED_ARTIFACT_KINDS` and are enforced by both
   `publish-artifact` (exit 6) and workflow validation (exit 8). The
   publisher records artifacts rather than rewriting them. The 2026-05-23
   schema/redaction closure confirmed no missing current schema; schemas for
   additional kinds land with their accepting RFCs.

7. **Redaction tests.** Default-deny evidence-export policy registry is in
   place; new evidence fields default to redacted unless explicitly marked
   safe. Synthetic injection coverage now exercises workflow/job prompt
   fields, model rationales, blocker text, transcript-like fields, nested
   payloads under safe scalar fields, case-insensitive path hygiene for
   transcript/output/private paths, and session close/non-fresh reason prose.
   Future evidence fields must extend this coverage with their policy entry.

13. ~~**Replace bootstrap scripts with runner-owned workflows.**~~ ✅ Done:
    the minimal process adapter and supervised sessions cover claimed-work
    launch. `scripts/` is now CI-smoke-only (`fresh_clone_smoke.sh`,
    `package_smoke.sh`); `.striatum/bin/*-supervised-wrapper.sh` are
    supervisor wrappers, not bootstrappers; no P00* prompt is referenced
    from `src/`. `examples/three-lane-design-build-review/` is the
    runner-owned successor to the historical P001 three-lane design,
    synthesis, build, and review flow, and `tests/test_example_workflows.py`
    guards its graph plus referenced role/prompt files.

## Open

16. **Keep generic language current.** New docs should say "target
    repository", "workflow fixture", "runner state", "artifact", and
    "adapter" rather than assuming Engram-specific paths or marker names.
    Current sweep (2026-05-18): refreshed current docs, RFC status notes,
    prompts, and root reference artifacts so daemon-owned PostgreSQL is the
    authoritative live state, `.striatum/` is operational scratch, workflow
    trees are generated with explicit scaffold/artifact roots, and Engram remains
    optional external augmentation rather than a runtime dependency.
    Follow-up sweeps (2026-05-23): refreshed README status language,
    consumer-layout examples, historical tmux bootstrap wording, and the RFC
    0056 corpus phrasing; added a guardrail for stale current-doc Engram
    phrases. This is a standing hygiene guardrail, not a blocked backlog item.

20. ~~**RFC 0040 V1.5 follow-up.** Six codex findings (F1-F6) from
    dogfood-040 build review iteration 2.~~ ✅ Done: shipped under
    dogfood-044 (v1.33.0). (F1) daemon MCP `tools/call` now dispatches
    through `daemon_pg/mcp_dispatch.py::dispatch_mcp_tool_call` →
    composite tools `dogfood.publish_on_behalf` +
    `dogfood.surgical_recovery` are now functional through the MCP
    path in the historical SQLite-era implementation; D110 later removed
    those SQLite-bound composites from the production daemon contract until a
    PostgreSQL-native replacement is accepted. (F2/F3)
    `publish_on_behalf` runs ack/publish/verdict inside
    one outer transaction with rollback-event emission on failure, and
    review verdicts are validated + recorded with `findings_artifact_id`
    defaulting from the published artifact when kind=`finding`; (F4)
    the legacy `process_progress.progress_loop_once` wrapper was later
    retired with the Python-daemon sweep path; the shared
    `daemon_supervisor.progress_watcher` checks remain as compatibility
    coverage. 4th codex/codex
    anti-pattern instance (D098 cycle-exhaustion override). Codex
    needs_revision findings absorbed into RFC 0040 V1.6 (item 28
    below).

28. ~~**RFC 0040 V1.6 follow-up.**~~ Composite failure observability landed for
    `dogfood.publish_on_behalf`: mid-composite rollback errors now include
    `failed_step`, partial `composition_steps`, and a nested
    `specific_error`; rollback events record the same details; daemon RPC
    converts dogfood helper failure envelopes into RPC errors; and MCP
    structured content surfaces nested error codes instead of flattening every
    composite failure to `command_failed`. The 2026-05-23 RFC 0040 closure
    added PostgreSQL artifact-summary byline evidence (`author.line` and
    `author.actual_author_line`, preserving the old `author.author_line`) and
    closes the packet-evidence residual for current scope. Any larger
    provenance packet redesign now requires a separate product decision.
    Codex needs_revision findings from
    dogfood-044 build review, deferred by cycle-exhaustion override
    per D098 (decision `dec_242ea0b026d547c9baad9b353b149033`). 4th
    instance of the codex/codex implementer+reviewer anti-pattern
    (precedents D095 dogfood-042 Track A, D096 dogfood-042 Track C,
    D097 dogfood-043). 2-of-3 cross-lane verdicts: claude
    accept_with_findings (medium), gemini accept (low). The anti-pattern
    is now well-characterized across four independent runs; the
    refuse-by-default validator rule (TODO item 26) has landed.

21. ~~**RFC 0038 V1.5 follow-up.** Codex attempt-2 findings (F1-F4)
    from dogfood-041 build review iteration 2 + gemini attempt-1
    findings, deferred by cycle-exhaustion override (decision
    `dec_251e8a5f3d674c409de0dad9eacd5844`).~~ ✅ Done: shipped under
    dogfood-045 (v1.34.0). (F1) `placeholderIslandPlugin` removed from
    `vite.config.ts`; new `make ui-verify-bundle` + Python sentinel
    test refuse placeholder bundles. (F2) `/workflows/new` chooser
    rewritten around the server-stable `{"templates": [...]}` shape;
    `types.ts` / `api-client.ts` / `WorkflowChooser.tsx` realigned;
    modifier step removed. (F3) New
    `src/striatum/web/frontend/src/shared/island-shared-entry.ts`
    non-mounting entry is now the Rollup input for `island-shared`;
    vitest regression `island-shared-no-mount.test.ts` pins the
    single-mount guarantee. (F4) Vite output semantics aligned with
    package-data layout (`manifest: false`; sub-package entry already
    matches). Supply-chain hygiene: `npm ci` in `ui-install`,
    `ui-update-lock`, `ui-audit`, `npm-audit-baseline.json` committed.
    Implementer was **claude** (not codex) — first dogfood deliberately
    avoiding the codex/codex anti-pattern after 4 instances (D095-D098).
    Codex reviewer still came back harsh (`reject` critical,
    threat_model); cross-lane majority disagreed (claude
    `accept_with_findings` medium, gemini `accept` low); D099
    (`dec_ccfa1685878d41d69ccc6496cd6612fd`) overrode the codex reject.
    Codex critical findings (placeholder bundles still committed
    pending operator `make ui-update-lock` + `make ui-build`; supply-chain
    polish items) absorbed into RFC 0038 V1.6 follow-up (item 29 below).

29. ~~**RFC 0038 V1.6 follow-up.**~~ ✅ Done: the real bundle is committed
    under `src/striatum/web/static/build/`, `@vitejs/plugin-react` lives in
    `devDependencies`, the lockfile matches, and package/bundle guardrails
    cover the former placeholder and dependency-placement risks. Verification
    in the 2026-05-17 remediation pass included `make ui-check-bundle`,
    `make ui-test`, `make lint`, `make typecheck`, and full `make test`.
    Historical context: codex reject-override deltas from dogfood-045 build
    review (decision `dec_ccfa1685878d41d69ccc6496cd6612fd`, D099).

22. ~~**Implement RFC 0043 V1 (Postgres as Sole Substrate, daemon-required).**
    Per D094 (accepted; supersedes D006/D007/D036 and SQLite half of D009).~~
    ✅ Done: shipped under dogfood-048 (v1.37.0). Two-track split:
    **Track A (codex)** landed daemon-side schema migration v5
    (`src/striatum/daemon_pg/sql/0005_repo_local_workflow_state.sql`)
    creating the 17 repo-local tables (`workflow_snapshots`, `runs`,
    `sessions`, `jobs`, `job_dependencies`, `queue_messages`, `leases`,
    `work_packets`, `artifacts`, `verdicts`, `blockers`,
    `command_requests`, `process_executions`, `events`, `job_worktrees`,
    `process_supervisors`, `process_supervisor_pointers`) under
    `repository_id text NOT NULL REFERENCES striatumd.repositories`,
    plus `striatumd.repo_migrations` checkpoint table and append-only
    `events`/`artifacts` triggers. Daemon DB version bumped 4 → 5.
    Migration body in `src/striatum/daemon_pg/repo_local_migration.py`
    (`RepoLocalMigrationOptions`, `migrate_repo_local`,
    `compute_repo_local_reanchor`) opens source SQLite read-only,
    verifies `PRAGMA user_version == LATEST_VERSION`, copies rows in
    dependency order inside one `SERIALIZABLE` Postgres transaction,
    writes the repo-migration checkpoint, then renames
    `.striatum/retired-local-state → retired-local-state.tombstone` (mode `0444`)
    unless `--confirm-delete` is set. Byte-equivalent audit-chain
    re-anchor compares canonical `events` and `artifacts` row manifests
    between SQLite and Postgres via SHA-256. Daemon command helper at
    `src/striatum/cli/daemon.py`. **Track B (claude)** retired
    `--no-daemon` (argparse exit 2 `unrecognized arguments: --no-daemon`),
    introduced `DaemonUnreachableError` (exit 11) and
    `RepoNotMigratedError` (exit 12) in `src/striatum/errors.py` with
    canonical stderr remediation templates (Linux systemd, macOS
    launchd, foreground, Postgres) + JSON envelope `hint` field,
    wired env-gated `enforce_daemon_required` in
    `src/striatum/cli/daemon_required.py` + `src/striatum/cli/dispatch.py`
    with `DAEMON_OPTIONAL_COMMANDS` allowlist (`daemon`, `init`,
    `skills`, `plugin`), renumbered legacy V1 daemon errors
    (auth → 14, capability → 15) so codes 11 and 12 stay unambiguous
    for the RFC 0043 entry layer, and expanded
    `src/striatum/daemon_rpc/registry.py` + `server.py::CLI_ROUTES`
    to cover every mutation in `cli/mutations.py` per RFC 0043 §5
    (dotted vocabulary: `session.*`, `work.*`, `artifact.publish`,
    `review.*`, `decision.record`, `checkpoint.resolve`,
    `branch.confirm`, `run.*`, `worktree.*`, `recovery.*`,
    `supervise.*`, `workflow.*` + daemon-global `repo.list` +
    `daemon.migrate_repo_local`), keeping legacy undotted aliases as
    `deprecated=True`; D110 later removed `daemon.migrate_repo_local` from
    the production daemon contract. New test suites:
    `tests/cli/test_no_daemon_retired.py`,
    `tests/cli/test_daemon_doctor_without_daemon.py`,
    `tests/exit_codes/test_rfc0043_refusals.py`,
    `tests/daemon_rpc/test_registry_rfc0043_coverage.py`,
    `tests/daemon_pg/test_repo_local_migration.py`,
    `tests/fixtures/v1_repo_local_sqlite/`. D102 cycle-exhaustion
    override applied: codex `needs_revision` high + gemini
    `needs_revision` medium (both with real findings on crash-recovery
    persistence gap, CLI escape path closure, migrate-repo-local
    subcommand wiring) overridden by single accepting verdict (claude
    `accept_with_findings` low). **D102 is distinct from D095-D101 in
    finding character**: both codex+gemini hit `needs_revision` with
    real findings rather than the codex/codex co-blindness anti-pattern
    (D095-D098, D100) or the codex-reviewer-of-claude-implementer
    pattern (D099, D101). Two run-quality regressions surfaced: the
    3rd `claude-no-artifact` instance (claude reviewer composed no
    REVIEW.md — operator-composed to recover) and the 3rd
    `gemini-no-frontmatter` instance (gemini REVIEW.md missing v1
    front matter — operator-fixed). Operator also performed SQL
    surgery on the `artifacts.logical_name` because the
    publish-on-behalf call passed the wrong logical name during the
    recovery. Findings folded into RFC 0043 V1.5 follow-up (item 31
    below).

23. ~~**Implement RFC 0044 V1 (Engram Phase 1 read-only MCP).** RFC drafted
    under dogfood-042 Track B; build review 3-of-3 accept (codex,
    claude, gemini). Implementation lands: Striatum-owned redacted
    JSONL export, Engram-owned `ingest-striatum`, standalone
    `engram-mcp-stdio` MCP server, four read-only retrieval tools,
    Engram-local `memory.*` capabilities, and the hard augmentation-
    not-dependency boundary (Striatum runs with Engram unavailable).~~
    🟡 Striatum-side V1 done under dogfood-046 (v1.35.0):
    `striatum corpus export --since <ref> --out <dir>` ships the
    redacted JSONL bundle (nine files + `manifest.json`) backed by
    `src/striatum/corpus/` (types, git helpers, enumerator, redactor,
    JSONL writer, manifest, export orchestration). The augmentation
    boundary is pinned by the active V2 guardrail
    `tests/test_corpus_verify.py::test_corpus_v2_surface_keeps_augmentation_boundary_local`
    (no `import engram`, no `from engram`, no `memory.*` capabilities
    across `corpus/`, `cli/`, `daemon_rpc`, `daemon_pg`,
    `service.py`, and `pyproject.toml`). D100 cycle-exhaustion
    override applied: codex `needs_revision` (5th codex/codex
    anti-pattern instance after D095/D096/D097/D098) + gemini
    `needs_revision` (focused entirely on out-of-scope Engram-side
    attack surface — MCP server, ingester, capability model — none of
    which ship in this dogfood); single accepting verdict claude
    `accept_with_findings` low covered the in-scope Striatum-side
    surface. Engram-side (ingester `engram ingest-striatum`,
    standalone `engram-mcp-stdio` server, four read-only retrieval
    tools, `memory.*` capabilities) remains a separate follow-up at
    `~/git/engram/` and is explicitly NOT in Striatum's TODO scope.
    Engram-side adversarial findings from gemini (RFC 0044 §6
    contradiction on `memory.read_personal` default, `corpus_id`
    isolation, indirect prompt injection memory poisoning, manifest
    forgery without cryptographic signing, secret leakage through
    curated artifacts) are forwarded to the Engram-side
    implementation effort.

32. **Queue Engram-side tenant-aware RFC 0044 Phase 1.** Striatum-side export
    is already done under dogfood-046 and remains the only in-repo shipped
    surface. The external Engram follow-up at `~/git/engram/` should implement
    the tenant-aware Phase 1 contract from RFC 0044: `tenant_id` as the local
    application-memory boundary, `corpus_id` as the workload/dataset boundary,
    `engram ingest-striatum --bundle <dir> [--repo <name>]`, read-only
    `engram-mcp-stdio`, and capability tests proving default Striatum operator
    access is restricted to the Striatum tenant/corpus while existing personal
    memory remains isolated. This is queued external work; do not add Engram
    ingester or MCP code to Striatum.

24. ~~**RFC 0039 V1.5: address Track A build review findings.** Cycle-
    exhaustion override per D095 (decision
    `dec_b75d66f38a3d40228891248c91a27774`). 2-of-3 reviewers
    accept_with_findings (claude, gemini); codex needs_revision
    overridden because the codex/codex implementer+reviewer pairing
    converged on its own findings (anti-pattern documented in D095
    follow-up). Land the codex / claude / gemini findings deltas via
    a future dogfood folded into Phase 2.~~ ✅ Done: shipped under
    dogfood-047 (v1.36.0). All five synthesis findings (F1-F5)
    landed in implementation order **F5 → F4 → F1 → F2 → F3**:
    (F5) `go/pkg/db/connection.go` rewritten on top of
    `github.com/jackc/pgx/v5 v5.7.2` — the Go daemon's first
    third-party runtime dependency; `PsqlRunner` /
    `exec.Command("psql", ...)` removed from production code paths;
    `db.Runner` + `db.TxRunner` interfaces expose parameterized
    `Exec`/`QueryRow`/`QueryScalar`/`BeginTx`; pool configured with
    `application_name="striatumd-go/<daemon_version>"` and default
    `statement_timeout=60000`. (F4) `go/pkg/db/audit.go::RecordRPC`
    opens one `READ COMMITTED` transaction via the F5 runner, locks
    the singleton `striatumd.audit_chain_head` row with
    `SELECT ... FOR UPDATE`, derives the open audit segment, computes
    the v2 row hash from the locked `previous_hash`, inserts with
    `INSERT ... RETURNING audit_id`, updates the chain head, commits,
    and returns the audit id (closes the V1 envelope-shape regression
    that returned empty `audit_id` to clients). (F1)
    `go/pkg/rpc/auth_pg.go` introduces `PostgresAuthorizer`; tokens
    are HMAC-SHA256(salt, secret) compared via
    `subtle.ConstantTimeCompare`; capability lookup mirrors
    `src/striatum/daemon_rpc/capability.py` exactly (same WHERE,
    wildcard ordering, scope-mismatch fallback); denial vocabulary
    matches Python so clients cannot tell the two cores apart from
    the refusal envelope; `go/cmd/striatumd/main.go` wires it
    whenever a Postgres URL is configured. (F2)
    `go/cmd/striatumd/main.go` flag surface is the synthesis-locked
    `--socket / --postgres-url / --migrate / --describe /
    --migrations-sha-source`; `go/Makefile` writes
    `go/bin/striatumd`; `tests/_harness/daemon.py::_start_go`
    launches with the locked argv. The original dogfood-047 F3
    two-core harness shape is historical; D111 now makes the top-level
    `Makefile`, `tests/conftest.py`, and CI multi-repo lane Go-only. New tests:
    `tests/test_daemon_go_smoke.py` (boot + `daemon.hello` +
    `daemon.describe` + audit-chain-head moved),
    `tests/test_daemon_go_audit.py` (concurrent audit-emitting RPC
    calls against `MultiRepoHarness(daemon_core="go")`),
    `go/pkg/db/audit_race_test.go` (opt-in on
    `STRIATUM_PG_TEST_URL`). Implementer was **claude** (Go +
    Python harness mix), deliberately not codex — second dogfood
    avoiding the codex/codex anti-pattern (precedents D095-D098,
    D100). D101 override applied: codex `needs_revision` high
    (codex-reviewer-of-claude-implementer pattern, distinct from
    codex/codex co-blindness — same axis as D099 dogfood-045)
    overridden via 2-of-3 cross-lane consensus (claude
    `accept_with_findings` low ergonomics_dx, gemini
    `accept_with_findings` medium threat_model). Codex
    needs_revision findings F1-F5 (`go.sum` unchecksummed,
    unauthenticated/no-audit production fallback when no
    `--postgres-url` is configured, `CORE=go` matrix can pass with
    all tests skipped, smoke-test asserts no denial reason on
    unauthenticated `daemon.describe`, audit-append regression not
    executable without `STRIATUM_PG_TEST_URL`) absorbed into RFC
    0039 V1.6 follow-up (item 30 below).

30. ~~**RFC 0039 / D105 Go support-runtime hardening.**~~ ✅ Done,
    historical: the helper-focused slice landed `go/Makefile`
    `verify`, `test-helper`, and `helper-check`; root
    `daemon-go-helper-check`; focused Go CI; transitive helper dependency
    inspection via `go list -deps ./cmd/striatum-supervisor-helper`; and
    the startup no-Postgres/no-socket regression. D107 later reopened full Go
    daemon parity as item 61 / RFC 0068. Historical context: Codex needs_revision findings from
    dogfood-047 build review, deferred under D101 (decision
    `dec_f8d268f392ca44dd8a9bccb634249979`). Codex
    reviewer-of-claude-implementer pattern (distinct from codex/codex
    co-blindness; same axis as D099 dogfood-045). Land the codex
    F1-F5 deltas under the Go port where they still apply: (F1)
    `(cd go && go mod tidy)` and commit
    `go.sum` so `pgx/v5` and indirect dependencies are
    cryptographically pinned and helper builds succeed; (F2) remove
    the unauthenticated/no-audit socket-serving fallback in
    `go/cmd/striatumd/main.go:49` or gate that serving mode behind the
    RFC 0068 conformance boundary; (F3) replace the old
    `make test-multi-repo CORE=go` parity expectation with a
    non-optional Go conformance target once the production port lands;
    (F4) keep denial/audit coverage for any Go RPC-serving path that
    remains during transition; (F5) run Go database/audit race tests
    in CI for helper-owned code paths and any transitional RPC code
    still shipped. Gemini accept_with_findings medium threat_model also
    flagged dependency-budget hygiene (`go mod verify`) and
    migration-advisory-lock persistence under the new `pgx` pool; keep that
    hygiene in the RFC 0068 conformance gate.

25. **Phase 2 (RFC 0039 Steps 3-6): Go replacement daemon.**
    Reopened by D107 / RFC 0068 and now superseded by item 61. `striatum
    daemon start` launches the Go daemon after active contract-method
    parity; D111 retires the Python daemon selector. The Python CLI may remain
    a client.
    ✅ SUPERSEDED/DONE (RFC 0078): the Go daemon `striatumd` is the production
    daemon, started via systemd — the `striatum daemon start` and `daemon
    doctor` CLI verbs are retired (the top-level `striatum doctor`, `daemon
    status`, and `daemon migrate-db` remain). The Python runtime and CLI were
    fully removed in the Go-only cutover; there is no longer a Python client.

26. ~~**Harness improvement: forbid codex/codex implementer+reviewer
    pairing in workflow validator.**~~ ✅ Done: cycle-exhaustion observed three
    times across recent runs (dogfood-042 Track A per D095;
    dogfood-042 Track C per D096; dogfood-043 Python build per D097).
    When the implementer and a reviewer are both the same model
    (codex+codex specifically observed), the reviewer's findings
    cluster around the implementer's same blind spots, producing
    apparent "needs_revision" verdicts that 2-of-3 majority overrides.
    Soft warning landed in the dogfood-043 prep commit; `workflow lint
    --strict` refuses same-model review-pair and revision-cycle warnings
    unless the operator supplies an explicit override rationale; and
    `workflow validate` now refuses the same lint findings by default unless
    `--allow-same-model-pairing` is passed. Durable accepted-risk policy is
    tracked separately under Phase 7 / TODO 55; do not keep this row open for
    future policy decisions.

27. ~~**RFC 0045 V1.5: address codex build review findings from
    dogfood-043** (cycle phase-jump validator gap closed; strict phase-skip
    restriction, phase field compatibility, phase_id strict-on-v1 check,
    `phases[].synthesis_job_id` validation/generation, frontend drag-drop
    phase bypass, and invalid/unknown phase display tolerance all landed) —
    see D097.~~ ✅ Done.
    Cycle-exhaustion
    override per D097 (decision
    `dec_2c5fbf49e91441aca3562a66919ea8c1`). 2-of-3 cross-lane
    reviewers accept (claude accept_with_findings low, gemini accept
    low); codex needs_revision overridden because the codex/codex
    implementer+reviewer pairing produced the third instance of the
    convergent-blind-spot anti-pattern (D095, D096, D097). The frontend
    follow-up now keeps invalid/missing phase jobs visible in an invalid
    bucket, removes the explicit-phase `(unset)` dropdown bypass, and defaults
    new explicit-phase jobs to the first declared phase.

31. ~~**RFC 0043 V1.5 follow-up.** Codex + gemini needs_revision findings
    from dogfood-048 build review, deferred under D102 (decision
    `dec_0b953435368e40109e793378e1a75054`).~~ ✅ Done / tracker stale.
    The deferred findings landed across the later V1.5/V1.6 hardening
    slices: the former repo-local migration path is now retired from operator
    use; daemon-required enforcement is the default, and the old
    `daemon migrate-repo-local` spelling is parser-compatible only so it can
    refuse with exit code 12 before opening SQLite; and
    focused regression coverage now includes crash-resume, split-brain,
    lock contention, parser/help, registry coverage, exit-code 11/12
    dispatch paths, and a foreground-daemon socket refusal smoke. The
    root `make test-rfc0043` target runs that slice with the existing
    Postgres harness convention (`STRIATUM_MULTI_REPO_REQUIRE_PG=1`;
    provide `STRIATUM_TEST_POSTGRES_URL` or `STRIATUM_DAEMON_DB_URL`).

19. ~~**RFC for multi-repo / cross-repo test harness.** RFC 0035 V1
    landed in dogfood-037. `tests/_harness/MultiRepoHarness` boots a
    daemon + N registered target repositories with ephemeral Postgres,
    resets daemon DB state between tests, and supports prepare/lifecycle/
    crash-recovery/MCP-capability-scope/per-repo-write-scope e2e
    coverage through `make test-multi-repo`.~~

## V1.7-V2.0 Backlog

Items 33-39 cover RFCs proposed after RFC 0045 (item 27 boundary).
Sequencing and acceptance criteria live in `docs/ROADMAP.md`; this
section is the canonical status snapshot.

33. ~~**RFC 0042 V1 (run-list workflow identity).**~~ Done:
    `striatum list runs` and daemon `list.runs` return a
    `workflow_identity` triple (`workflow_id`, `workflow_version`,
    `workflow_snapshot_id`), and the web run list renders the workflow
    name with local workflow and default-branch GitHub affordances when
    available. The run detail graph viewer now has pan, zoom in/out,
    fit, reset, and keyboard navigation controls, with focused web
    regression coverage.

34. ~~**RFC 0046 V1 (lane evidence guard at publish-artifact).**~~ Closes
    GH #2 + #5. V1.7 scope. Already exercised informally in
    dogfoods-054b/055b/056 (operator-on-behalf publishes use
    `--allow-no-process-execution --override-rationale`). Done:
    daemon/Postgres publish validates model-bylined artifacts against
    path-specific supervisor `artifact_observed` evidence when present,
    keeps the clean `process_executions` fallback for legacy wrappers,
    stores `attestation_override_rationale` on artifacts via PG
    migration 0008, and records override provenance events.

35. ~~**RFC 0047 V1 (decision-record propagation +
    `compromised` run state).**~~ Done: SQLite and daemon/Postgres
    `decision.record` both project rejected decisions to
    `runs.state='compromised'`, supersede accepting verdicts with the
    decision id, and allow a later accepted decision to reopen the run
    to `completed` while preserving the supersession trail. PG migration
    0007 adds the daemon schema projection.

36. ~~**RFC 0048 (daemon-side substrate migration).**~~ ✅ done in
    v1.55.0. Phase A (v1.49.0) ported 16 mutation handlers into
    `src/striatum/daemon_pg/handlers/`; Phase B (v1.50.0–v1.54.0 +
    follow-up) shipped the Go-core mutation surface
    (`go/pkg/{reads,mutations}/`) and the Unix-socket accept loop;
    Phase C (v1.51.0–v1.52.0) routed mapped CLI verbs through the
    daemon and bootstrapped admin auth into `striatumd.clients`.
    V1.5 hardening (v1.55.0): F2 capability-denial matrix
    (`tests/daemon_pg/test_capability_denial_matrix.py`), F3
    audit-chain `FOR UPDATE` row-lock on `audit_chain_head`
    (`src/striatum/daemon_rpc/request_log.py`), F4 append-only
    role-grant tests (`tests/daemon_pg/test_append_only_role_grants.py`),
    HIGH#1 parity rig (`tests/daemon_pg/handlers/_parity.py`),
    HIGH#2 inline-helper exports (`complete_inline`, `ack_inline`),
    and migration 0006 (events chain anchors + repo_event_chain_heads).
    Mapped CLI fail-closed flip removed the legacy SQLite fallback
    for ported verbs. Closes gemini A1 from dogfood-050 + codex F2
    from dogfood-049. ROADMAP §5.3.

37. **RFC 0049 (interactive claude lane via MCP).** Shelved capability
    RFC. Motivated by Anthropic's 2026-06-15 plan-credit
    policy (`claude -p` moves off subscription quota onto separate
    $20-$200/month credit). On Max 20x the subscription is ~100×
    token-per-dollar improvement. **v1.48.1's wrapper auth fix relieved
    the urgency** — RFC 0049 is now a capability RFC rather than a
    blocker. D106 records the durable shelf decision. Reopen only if
    billing terms change materially or an operator explicitly funds the
    PTY/MCP spike. A 2026-05-23 closure workflow rechecked RFC 0130/0075
    prerequisites and current Claude plan-credit docs; the reopen criteria
    are still unmet. ROADMAP §5.5.

38. **RFC 0050 (operator UI rework and provenance honesty).** All
    three phases landed:
    - V1 (v1.46.0, dogfood-054 + 054b): UI primitives + dashboard parity.
    - V1.5 (v1.47.0, dogfood-055 + 055b): template extensions + 3
      provenance honesty fixes.
    - V2 (v1.48.0, dogfood-056): recovery panel island, override modal,
      copy-on-click, graph editor data binding.

    ~~Open follow-ups from V2 review filed as GH issues #9-13~~ are
    closed by focused regressions: `/v1/invoke` CSRF/content-type
    refusal (`tests/test_invoke_csrf_refused.py`), override modal
    context-token validation
    (`tests/test_override_modal_context_validation.py`), recovery
    dry-run no-side-effect coverage
    (`tests/test_recovery_dry_run_no_side_effects.py`), copy-on-click
    scoping (`tests/test_copy_on_click.py`), and workflow editor ghost
    field purging (`workflow-graph-editor.test.ts`).

39. **RFC 0051 V1 (auto-finalize from frontmatter).** Proposed
    2026-05-14. Driven by 8 operator-on-behalf publishes across
    dogfoods-054b/055/055b/056. Runner auto-finalizes when expected
    artifact appears on disk with valid `verdict_intent` and byline
    match. **Downgrades from urgent to safety-net-only after gh-16
    empirically validated v1.48.1's wrapper auth fix** (zero
    operator-on-behalf publishes across all 3 lanes). Phase 8 daemon
    slice landed `recovery.auto_finalize` as a dry-run-by-default manual
    command and default-live recovery method over declared
    `expected_artifacts`. It validates stable mtime, artifact kind,
    front matter, required byline, active lease/session ownership, and
    lane evidence; review jobs derive the verdict from
    `verdict_intent`; auto-finalized artifacts are marked in PG evidence
    summaries. A follow-up slice now surfaces dry-run eligibility and
    refusal reasons through status/dashboard projections and the web recovery
    panel while preserving read-only previews.
    The bounded sweep integration now routes `recovery auto` through
    canonical `recovery.sweep`, runs auto-finalize before lazy lease
    expiry unless the workflow explicitly opts out, and routes stale-artifact
    auto-publish through explicit `recovery.auto_publish_stale_artifacts`.
    PostgreSQL sweep now also executes configured checkpoint-timeout
    escalation hooks in live mode, keeps dry-runs side-effect-free, and
    folds hook failures into `escalations[]`.
    Automated PG recovery coverage now pins a dogfood-shaped run where
    three valid written artifacts auto-finalize with zero
    `dogfood.publish_on_behalf` or operator-override provenance events.
    D133 completes the default policy cutover after live dogfood confidence:
    absent workflow policy allows live auto-finalize, and
    `recovery.auto_finalize.enabled=false` is the explicit opt-out.

43. **RFC 0052 V0 (committee deliberation workflow).** Proposed
    2026-05-14. Committee shape for high-stakes design phases: N
    producers deliberate under a named arbitrator with optional panel
    escalation and an adversarial-review sub-shape. Debate turns are
    typed front-mattered artifacts (`debate_turn`,
    `arbitration_ruling`, `panel_vote`, `panel_verdict`,
    `debate_synthesis`); solves D095-D102 reviewer co-blindness via
    lane composition rather than RFC 0018 posture labelling. Phase 0
    scaffold landed (RFC body + schema sketches). A 2026-05-23 closure
    classified the V0 proposal as not directly implementable: a bounded
    Phase A implementation RFC/design is required before production work.
    RFC 0074's generated `implementation_panel` shape does not replace the
    RFC 0052 debate/panel semantics.
    ROADMAP §5.8.

44. **RFC 0053 V0 (human principal as escalation-only).** Proposed
    2026-05-14; doc-side fixes landed. Names the human role as
    `human principal`, restricts function to resolving unresolvable
    blockers or decisions; AI operator is the default driver. Same CLI
    surface, functionally bounded role. SPEC.md / GETTING_STARTED.md /
    HOW_TO_HUMAN.md prose realigned in commit 7e21399. D103 recorded
    in DECISION_LOG. A follow-up wording sweep realigned reader-facing docs,
    CLI help, scaffold output, workflow-template text, and recovery skill
    templates around principal/operator language while leaving durable
    identifiers unchanged. The 2026-05-23 Phase B closure classified the
    workflow.json schema-field rename (`human_checkpoint` →
    `escalation_checkpoint`) and `waiting_human` run-state rename as a
    coordinated schema/runtime migration requiring a version bump,
    `workflow upgrade` rule, PostgreSQL/Go/Python runtime updates, and UI/read
    compatibility policy before implementation. The `escalation` artifact kind,
    `striatum.escalation.v1` front matter schema, publish-time blocker
    linkage, and daemon RPC projection methods landed under remediation
    item 53.
    ROADMAP §5.8.

45. ~~**RFC 0054 V0 (day-zero usage guide).**~~ Phase A shipped
    v1.55.0 (commit `a88f44d`). `docs/USING_STRIATUM.md` lives
    alongside `GETTING_STARTED.md` (additive — resolved Open
    question 1 toward avoiding a breaking docs rename). The 2026-05-23
    closure classified Phase B as not warranted: operator onboarding content
    should not be copied into the generic target-repository DDD scaffold.

46. ~~**RFC 0055 V0 (marketing README + architecture graphics).**~~
    Phase A shipped v1.55.0 (commit `a88f44d`). `README.md`
    rewritten with vision/value framing, Mermaid system-architecture
    diagram, ASCII architecture view, and demoted docs link table. The
    2026-05-23 closure classified optional SVG polish as no-action unless a
    concrete docs/product need appears.

47. ~~**RFC 0056 V0 (consumer-repo directory-structure opinions).**~~
    Phase A shipped v1.55.0 (commit `a88f44d`).
    `docs/CONSUMER_REPO_LAYOUT.md` written with ASCII tree, per-section
    rationale, mid-life adoption guidance, dogfood-heavy-projects
    extension. Current Go builds do not expose the historical
    `init --with-striatum-layout` scaffold; use `workflow generate` with
    explicit scaffold/artifact roots for new workflow trees.

## Architecture Remediation Backlog

Items 48-60 track the 2026-05-16 architecture remediation plan. The source
review and plan are root-level operator artifacts:
`reviews/external/STRIATUM_ARCHITECTURE_REVIEW_2026-05-16.md` and
`reviews/external/STRIATUM_ARCHITECTURE_REMEDIATION_PLAN_2026-05-16.md`.

48. ~~**Phase 0: command authority matrix and guardrails.**~~ Done:
    `docs/architecture/COMMAND_AUTHORITY_MATRIX.md` inventories the
    parser, daemon route translator, daemon registry, Python PG handler
    registry, Go handler coverage, and SQLite dependencies. Guardrail
    tests classify every daemon registry method by authority path,
    keep handwritten fallback routes from reappearing silently, and use a
    SQLite-connect tripwire for representative
    daemon-required production commands. `AGENTS.md` now requires matrix
    and guardrail updates for new RPC methods or handwritten route maps.

49. ~~**Phase 1: close production SQLite fallback.**~~ ✅ Done. Production
    daemon RPC fallback is closed: `CLI_ROUTES` is empty, mapped CLI/service/MCP
    calls fail closed through daemon RPC, and Go implements the workflow
    authoring/generation surfaces without live-state mutation. The 2026-05-24
    cleanup completes the remaining retirement slice: `src/striatum/legacy_sqlite/`,
    root `striatum.db` / `striatum.migrations` facades, the direct corpus
    exporter, the V1 local-state schema module, the deterministic repo-local
    fixture, and the broad skipped legacy fixture tests are deleted. Active
    guardrails assert production source does not import `sqlite3`,
    `striatum.legacy_sqlite`, `striatum.db`, or `striatum.migrations`; the
    remaining `.striatum/retired-local-state` handling is refusal/inspection of a
    retired file name, not live-state support.

50. ~~**Phase 2: single method-contract source.**~~ ✅ Done. Contract source is now
    live at `contracts/daemon_methods.json`; Python `METHOD_REGISTRY`
    loads from it; Go `go/pkg/rpc/registry_methods.go` is generated from
    it via `scripts/generate_go_rpc_registry.py`; parity tests guard
    Python/Go drift; CLI/MCP contract tests ensure routed methods are
    registered, CLI-local workflow methods stay hidden, deprecated
    aliases are not advertised as MCP tools, and daemon MCP tool descriptors
    are derived from `METHOD_REGISTRY`. `scripts/generate_daemon_method_tables.py`
    now renders `docs/architecture/DAEMON_METHOD_TABLES.md` from the contract,
    including the declarative `cli_routes` section. Runtime CLI route lookup
    now builds from the same contract route map and keeps only CLI-local
    parameter extraction in `src/striatum/cli/daemon_rpc_route.py`; focused
    tests guard contract/route drift and fail-closed registered-method routing.
    `cross_repo.cancel` now participates in the same declarative CLI route
    map, requires the `recovery` capability, and uses a PG-native
    participant-cancel runner rather than the historical repo-local SQLite
    runner.

51. ~~**Phase 3: daemon core strategy decision.**~~ ✅ Done. D105 briefly
    recorded Python as the primary production daemon core, but D107 / RFC 0068
    superseded it. Go is the production/default daemon; the Python daemon
    selector and module are deleted; the Python MCP wrapper is deleted; Python
    CLI/web code remains only as daemon-client surface where useful.

52. **Phase 4: daemon-first web service.** Initial slices landed:
    web POST mutations for run cancel/pause/resume, job cancel/retry, and
    branch confirm now call daemon RPC (`run.cancel`, `run.pause`,
    `run.resume`, `recovery.cancel_job`, `run.retry_job`,
    `branch.confirm`, `run.start`) through `service_daemon.py`, with
    daemon-routing regression tests. The web run list now renders from daemon
    `list.runs` DTOs in production; a `STRIATUM_TEST_HARNESS` fallback
    preserves legacy subprocess web fixtures only. Chat-session briefing now
    uses daemon `list.runs` DTOs for its active-run summary without opening
    retired local state. The posture-verdict drill-down page now uses daemon
    `run.posture_verdicts` in production, again with only the test-harness
    compatibility fallback. The `/v1` status, doctor, why, dashboard, and run
    artifact-rollup read endpoints now call daemon read DTOs directly
    instead of the legacy CLI invoke wrapper. The artifact detail page now
    uses daemon `artifact.show` with optional web context for run scoping,
    expected author line, and provenance events. The `/doctor` HTML page now
    renders from daemon `doctor` in production, with per-record recipe shaping
    kept in web presentation code and local compatibility retained only for the
    subprocess fixture fallback. The `/v1/invoke` mutation
    gate now derives daemon-routed read classification from
    `METHOD_REGISTRY.required_capability`, with only CLI-local workflow
    authoring reads left in an explicit service allowlist. Production service
    startup now verifies daemon/repository health via daemon `doctor` before
    binding; legacy local integrity checks are gone from production service
    fixtures under the test-harness escape. The web SSE stream now uses
    daemon `run.events` in production, with local event tailing
    limited to the same subprocess fixture path. Workflow run-now now calls
    daemon `run.prepare`, `branch.confirm`, and `run.start` in production,
    with only narrow subprocess compatibility fixtures outside production.
    The run detail page now calls daemon `run.detail` for page state in
    production, with HTML/SVG rendering kept local. The job detail page now calls daemon
    `job.detail` for page state in production, with override-verdict
    context-token minting kept local. First behavior-preserving split landed:
    `service_http.py` owns pure HTTP/security helpers while `service.py`
    re-exports the same names for existing callers. Follow-up split landed:
    `web/chat_session.py` owns chat transcript projection, briefing,
    JSONL append, timestamp, stable-hash, safe-git, multipart, session
    path/listing, display-message, and workflow-write confirmation helpers.
    Follow-up cleanup landed: the old `legacy_sqlite/service.py` quarantine
    module has been deleted; production web/API reads and mutations use daemon
    DTO/RPC paths, and compatibility fixtures no longer reopen retired
    repo-local state. `service.py` no longer eagerly imports
    the legacy `striatum.api` wrapper at module load; the compatibility
    `invoke()` seam lazy-loads it only when explicitly called. Follow-up split
    landed: `web/static_assets.py` owns static asset lookup, path validation,
    content-type mapping, JSON error mapping, CSP/header selection, and
    response body orchestration while `service.py` keeps a thin route wrapper
    and supplies context callbacks.
    Follow-up split landed: `web/workflows.py` owns workflow editor file
    resolution, new-workflow scaffold payloads, validation, atomic writes, and
    If-Match checks while `service.py` keeps HTTP request parsing, template
    rendering, and JSON response mapping for those routes. Follow-up split
    landed: `web/run_list.py` owns GitHub remote parsing, workflow source-path
    normalization, workflow tree-link construction, and run state-chip shaping.
    Follow-up split landed: `web/dogfood_routes.py` owns historical dogfood
    route dispatch and raw/page context construction while `service.py` keeps a
    thin request-handler adapter.
    Follow-up split landed: `web/artifacts.py` owns safe repo-relative artifact
    path resolution, raw download content-type selection, and inline Markdown
    rendering helpers for artifact views. Follow-up split landed:
    `web/run_posture_verdicts.py` owns posture-verdict template-context
    shaping and verdict-row filtering while `service.py` keeps daemon
    RPC/fallback and HTTP response mapping. Follow-up split landed:
    `service_command_policy.py` owns `/v1/invoke` read/mutation classification
    while `service.py` preserves the `is_read_command` compatibility import.
    Follow-up split landed: `web/view_file.py` owns repository file-view path
    validation, binary detection, text/Markdown payload shaping, and inline
    Markdown rendering, while `service.py` keeps route/template handling.
    Follow-up split landed:
    `service_sse.py` owns SSE replay offset parsing and event framing while
    `service.py` keeps daemon polling and stream-loop control. Follow-up split
    landed: `service_state.py` owns process-local service state, GitHub
    remote/default-branch caching, shutdown signaling, web-context secret
    generation, and per-run SSE slot accounting. Follow-up split landed:
    `service_runtime.py` owns version/mode reporting, loopback bind
    validation, PID-file single-instance checks, startup exceptions, and idle
    shutdown waiting. Follow-up split landed: `web/template_env.py` owns HTML
    escaping and Jinja environment construction for server-rendered templates.
    Follow-up split landed: `service_request_security.py` owns request
    authentication, bearer-token checks, same-origin mutation policy, and
    override-verdict web-context validation decisions. Follow-up split landed:
    `web/workflow_generation.py` owns workflow template listing/show and
    workflow generation preview/write response shaping. Follow-up split
    landed: `service_request_io.py` owns request-body parsing plus JSON/HTML
    response helpers while `service.py` keeps route-level wrappers.
    Follow-up split landed: `web/doctor.py` owns doctor page DTO loading,
    gated legacy fallback selection, record recipe shaping, problem grouping,
    template rendering, and response/error mapping while `service.py` keeps a
    stable route wrapper.
    Follow-up split landed: `web/workflows.py` owns workflow browser index and
    detail page DTO shaping while `service.py` keeps template rendering and
    HTTP error mapping for those pages. Follow-up split landed:
    `web/job_detail.py` owns job-detail template context shaping and
    override-context-token minting while `service.py` keeps daemon
    RPC/fallback and HTTP response mapping. Follow-up split landed:
    `web/artifacts.py` now also owns artifact-view template-context shaping,
    byline display, recorded attestation chips, lane-evidence chips, and
    expected-artifact row shaping. Follow-up split landed:
    `service_sse.py` owns daemon-backed run-event streaming while
    `service.py` keeps slot accounting and legacy fixture fallback selection.
    Follow-up split landed: `web/chat_routes.py` owns chat index/session
    rendering, chat creation, provider send/tool-loop handling, workflow-write
    confirmation, stop redirects, and transcript SSE tailing while
    `service.py` keeps route dispatch plus compatibility aliases for briefing
    and git context helpers. Follow-up split landed: `web/run_pages.py` owns
    run list/detail, job detail, artifact view, and posture-verdict page
    rendering while `service.py` keeps route dispatch plus stable private
    handler wrappers for existing tests and callers. Follow-up split landed:
    `web/artifacts.py` now also owns artifact raw download orchestration,
    with `service.py` supplying the stable handler wrapper and HTTP response
    writer callbacks. Follow-up split landed: `web/run_actions.py` owns
    workflow run-now, branch-confirm, run cancel/pause/resume, and job
    cancel/retry route handling while `service.py` keeps route dispatch and
    stable private wrappers. Follow-up split landed: `web/workflows.py` now
    also owns workflow browser and visual-editor route rendering/saving while
    `service.py` keeps route dispatch and stable private wrappers. Follow-up
    split landed: `web/view_file.py` now also owns repository file-view route
    rendering without legacy dogfood run-breadcrumb injection. Follow-up split
    landed: `service_api_routes.py` owns JSON read
    helpers, repo-tree reads, daemon-read fallback handling, and run-event SSE
    route control while `service.py` keeps dispatch/authentication wrappers.
    Follow-up split landed: `service_routes.py` owns GET/POST route selection
    while `service.py` keeps stable handler wrapper methods and endpoint
    contexts. Follow-up split landed: `service_server.py` owns TCP/Unix
    binding, PID-file handling, signal shutdown, and serve-loop orchestration
    while `service.py` keeps private compatibility wrappers.
    Remaining: continue splitting `service.py` along stable non-SQLite
    request-handling and rendering boundaries.

53. **Phase 5: real escalation inbox.** First slice landed:
    `escalation.list`, `escalation.show`, and `escalation.resolve`
    project explicit escalations over `striatumd.blockers`; the daemon
    contract and Go registry include the methods; CLI routing supports
    `striatum escalation ...`; and `striatum inbox --json` now shows the
    principal escalation inbox without requiring a session id. Follow-up
    slice landed the `escalation` artifact kind and
    `striatum.escalation.v1` front matter schema. Follow-up linkage now
    records successful `escalation` artifact publishes under
    `blockers.payload_json.escalation_artifact` and projects the linked
    artifact through `escalation.list` / `escalation.show`. Hardening now
    suppresses stale artifact-link projections unless id/path/hash metadata
    matches a real artifact row, repairs missing links on idempotent publish
    retries, and rejects conflicting blocker metadata. D130 closes
    artifact-only escalation creation as link-only: `artifact.publish` does
    not synthesize live blockers or inbox rows. Follow-up storage hardening
    landed a typed `striatumd.escalation_inbox` table in both Python and Go
    migrations. Follow-up payload hardening landed `work.block` request
    validation plus `striatum.blocker_payload.v1` payloads on blocker rows,
    escalation inbox rows, and block events. Remaining: a future dedicated
    create/update method only if product scope needs it, and eventual
    packet-helper rename (`packet inbox`) if needed.

54. **Phase 6: hardened PTY supervision / Go helper.** Add daemon-owned
    PTY supervision through a narrow helper protocol, with Python retaining
    daemon RPC and domain-state authority. First control-channel slice
    landed: `supervise.send` now returns an explicit
    delivered-unacknowledged state and `supervise.report` records wrapper
    control events (`packet_accepted`, `agent_started`,
    `artifact_observed`, `progress`, `agent_exited`) as daemon events
    without parsing model output. Follow-up slice landed a standalone
    `striatum-supervisor-helper` binary and protocol: it launches the agent
    under PTY, forwards packet bytes from stdin or a FIFO, and emits JSONL
    control events while an architecture guardrail keeps it out of DB/RPC,
    mutation, read, apply, and cross-repo packages. Follow-up slice landed
    Python-side consumption of helper JSONL event batches through
    `supervise.report`, including durable `helper_error` events and
    `agent_exited` stop-state transitions. Follow-up slice landed daemon
    `supervise.reattach_status` as a read-only supervisor health DTO, with
    daemon `doctor` surfacing non-healthy reattach states for stale
    supervisors. Follow-up slice landed explicit `supervision.transport:
    "pty_helper"` lane opt-in: the daemon PostgreSQL supervision handler
    launches `striatum-supervisor-helper`, persists helper pointer metadata,
    and drains helper JSONL acknowledgements through `supervise.report` during
    start/send/stop/status. Follow-up slice landed explicit
    `supervision.stdin_delivery: "one_shot_eof"` for pipe-transport lanes,
    letting single-prompt commands consume one packet and then receive EOF
    while preserving persistent FIFO behavior by default. Follow-up slice
    landed runner-owned stall alarms/blockers for attached but idle
    supervisors, including stalled status liveness, doctor/status surfacing,
    and lease-expired blockers. Follow-up slice made PostgreSQL
    lane-liveness attestation require same session/run binding, live PID,
    matching PID start token, and matching workflow snapshot lane command
    before `require_attested_lane` or byline derivation treat a lane as
    attested. Follow-up slice added a focused Postgres handler integration
    test that launches the built Go `striatum-supervisor-helper` and verifies
    start, send, packet acknowledgement, status drain, and agent-exit event
    ingestion across the Python/Go boundary. Follow-up slice promoted that
    check into `make daemon-go-helper-integration` and CI's Linux/Postgres
    matrix. Follow-up slice landed restart reattach/lost-state reconciliation
    on existing `supervise.status`, `supervise.send`, and push
    auto-dispatch paths: surviving supervisors record `supervisor.reattached`
    and refresh daemon-instance metadata, while stale PID identity is marked
    `lost` before any packet write. Final slice expanded
    `tests/test_claude_supervised_wrapper.py` into Claude, Codex, and Gemini
    supervised-wrapper fixtures that verify multi-packet loops, inner-command
    failure isolation, EOF exit behavior, temp scratch logging, and
    non-interactive tool-approval flags. Phase 6 is closed.

55. **Phase 7: workflow risk lint and review diversity enforcement.**
    `workflow lint <workflow.json> --json` returns structured advisory
    warnings for same-model review pairs/revision cycles, review jobs
    without fresh context, broad repo-write scopes, repo-write jobs
    without per-job worktree isolation, and review workflows missing a
    revision/escalation path. Follow-up slice landed opt-in
    `workflow lint --strict`, which refuses warnings unless the operator
    supplies a non-empty `--override-rationale` and includes the refused
    lint payload in JSON/API error details. Follow-up web slice surfaced
    warning counts and short warning lists in the workflow index/detail
    pages. Follow-up generator slice added advisory coverage scoring,
    surfaced lint in generated workflow preview envelopes and the workflow
    chooser, and lets strict overrides record an accepted-risk decision
    reference with `--accepted-risk-decision-id`. Follow-up validator slice made
    `workflow validate` refuse same-model review-pair and revision-cycle
    findings by default, with `--allow-same-model-pairing` as the explicit
    operator override. D124 accepts daemon-core lint as the authoritative
    accepted-risk surface: durable accepted-risk override state may be written
    only through daemon-backed CLI/UI/MCP clients, must cite a decision
    artifact, and must bind to an immutable workflow snapshot or fingerprint.
    First implementation slices have landed: Go daemon `workflow.lint`,
    `workflow.accept_risk`, and `workflow.accepted_risks.list` provide
    fingerprint/snapshot-bound accepted-risk records in PostgreSQL, and MCP
    capability gates expose the read/admin surfaces without writing workflow
    metadata. CLI client routing now includes `workflow accepted-risks` and
    `workflow accept-risk` over the same daemon methods. The local web
    workflow detail page now reads daemon lint plus accepted-risk records,
    presents accepted warnings and accepted-risk rows, and can append
    accepted-risk records through `workflow.accept_risk` when the service is
    started with `--allow-mutations`. Workflow-file metadata remains advisory
    and is not live authority.

56. **Phase 8: auto-finalize from front matter.** Bounded daemon slice
    landed: `recovery.auto_finalize` dry-run/live PG handler, CLI route,
    method contract entry, generated Go registry entry, explicit
    `artifact.auto_finalized` and `job.auto_finalized` events, review
    `verdict_intent` handling, no-partial-publish guard, default-live
    workflow policy with explicit opt-out, PG evidence
    `publish_origin=auto_from_artifact`, and
    status/dashboard/web dry-run visibility for eligibility/refusal reasons.
    Follow-up checkpoint split the overloaded recovery method surface:
    `recovery.sweep` is the canonical `recovery auto` RPC, stale-artifact
    auto-publish is explicit as `recovery.auto_publish_stale_artifacts`,
    and deprecated `recovery.auto` is no longer emitted by the CLI.
    The sweep invokes live auto-finalize unless the workflow explicitly opts
    out and never supplies the standalone force override. It also executes
    configured checkpoint-timeout escalation hooks in live mode while
    preserving dry-run no-side-effect behavior and folding hook failures
    into `escalations[]`. Automated dogfood-shaped acceptance coverage now
    proves valid written artifacts can auto-finalize with zero
    operator-on-behalf publishes. The skipped-candidate cause-class slice has
    also landed: every skip/refusal now carries a stable `cause`, artifact
    refusals carry per-artifact causes, and `reason` strings remain
    display-compatible. Lane-finalization visibility also landed across
    dry-run/live return payloads, status/dashboard/web projections, and the
    Go SQL summary path. The consecutive-failure circuit breaker is now
    table-backed with workflow policy defaults, open-breaker status in
    dry-run projections, force-resistant refusal until explicit live reset,
    reset/open audit events, and mirrored Python/Go migration support. D133
    flips the global default live allowance after D125's satisfied evidence
    gate. The policy projection now reports
    `global_default_mode="live"` plus the D125 default-live evidence gate,
    and `auto_finalize_gate_evidence` artifacts validate the required three
    live successes, two lane shapes, and zero contested audit-chain events.
    The 2026-05-24 synthesis evidence slice satisfied that gate with three
    live successes across review, build, and synthesis lane shapes and
    `contested_audit_chain_events: 0`. D133 completes the default-on
    implementation; any rollback or narrower policy requires a new decision.

57. ~~**Phase 9: UI packaging and bundle cleanup.**~~ Done:
    `ui-build` depends on `ui-clean`, `ui-check-bundle` also runs a
    bundle-size gate, `@vitejs/plugin-react` moved to `devDependencies`,
    the package archive has a size gate aligned with the UI bundle gate,
    and packaging tests pin those contracts. Manual chunking is now a
    monitor-only performance decision: no code slice remains until bundle
    evidence shows the current Rollup output is a problem.

58. ~~**Phase 10: day-zero setup improvements.**~~ Done:
    `daemon doctor --provision-rw-role` / `--repair-grants` can repair
    the common local `striatumd_rw` role/grant shape or return pasteable
    admin SQL; `daemon service install/start/status` renders and controls
    systemd-user or launchd daemon services; `repo add` registers the repo
    into daemon PostgreSQL; `skills install` / `plugin install` write
    agent-side bundles; `doctor
    --first-run` returns a V1 diagnostic report covering daemon socket,
    Go daemon binary provenance, Postgres, runtime token, repo
    registration, MCP visibility, a sample read route, and daemon
    authority routing; and the
    dev-only compose profile in `examples/dev-postgres/` is documented
    separately from the production-local system Postgres path.

59. **Phase 11: replay, archive, and corpus v2 foundations.** Partial
    slice landed: `striatum corpus verify --bundle` validates existing
    deterministic RFC 0044 V1 bundles without daemon state, hosted
    services, or external memory dependencies, and treats missing
    `corpus_contract_version` as implied V1 per RFC 0057 backward
    compatibility. A bounded run-archive foundation also landed:
    `striatum archive create --run-id <id> --out <dir>` writes a
    daemon/Postgres-backed local archive of run state, command requests,
    process executions, job worktrees, process supervisors, process
    supervisor pointers, artifact metadata, and event metadata.
    `striatum archive verify --bundle <dir>` validates the archive manifest
    and file hashes locally and now runs offline deep-chain semantic replay
    by default for run/repository consistency, archived-row references, event
    ordering, event-chain continuity, event-row hash recomputation, and
    duplicate/missing id rejection for archived command request,
    process-supervisor, process-supervisor-pointer, verdict, blocker,
    process-execution, and job-worktree rows. `--manifest-only` is the
    explicit fast path that skips semantic replay; optional artifact content
    hash checks still require `--repo-root`. `striatum archive inspect
    --bundle <dir>` reports read-only semantic and privacy metadata using the
    same local verifier. D126 accepts the Corpus Contract V2
    direction: composite `corpus_id` identity (`slug:sha256`), graduated
    redaction tiers, workflow opt-in augmentation by reference with agent-side
    fetch, hybrid archive bundles, verification replay by default, read-only
    semantic inspection, no comparative replay, deep-chain verification
    always, and optional daemon audit-chain cross-check. The first V2 manifest
    slice has landed: new exports emit explicit
    `corpus_contract_version=2`, composite `corpus_id`, `redaction_tier`,
    `augmentation_policy`, `verification_depth=deep_chain`,
    hybrid-archive default metadata, a corpus-scoped
    `incremental_export_watermark`, and optional `git_snapshot_hash`, while
    verification still accepts implied-V1 bundles. The archive follow-up now
    emits `archive_contract_version=2`, enforces `verification_depth=deep_chain`
    plus hybrid archive defaults when advertised, and preserves legacy-v1
    archive verification compatibility. Workflows can now opt into
    `augmentation.mode: "reference_only"` with local `corpus_bundle` sources;
    claimed work packets expose optional `context.augmentation_references`
    with manifest status, and missing/unreadable bundles never block workflow
    progress. This is the core augmentation-reference surface. The
    2026-05-23 closure classifies richer external-consumer fetch/UI UX as out
    of Striatum core unless a later optional-augmentation decision accepts it.

60. **Phase 12: optional Git/PR integration.** D127 decides the boundary:
    Striatum core does not autonomously commit, push, call hosted providers, or
    import provider SDKs. The first safe slice has landed as daemon read
    method `git.snapshot` plus `striatum git snapshot --json`: branch/HEAD,
    dirty counts, changed paths, and bounded ancestry are observed through a
    closed read-only local-git allowlist. It excludes remote URLs, diff hunks,
    commit bodies, hosted PR metadata, and provider actions. Durable
    `commit_request` and `pr_request` artifact schemas have landed as
    provenance-only request records. Daemon `git.commit_apply` plus
    `striatum git commit-apply <commit-request> --confirm
    --confirm-request-id <id>` may create a local commit only after explicit
    operator confirmation, a confirmed request artifact, matching base HEAD
    and branch, and dirty paths limited to `included_paths`. It disables Git
    hooks for the commit invocation and does not push, fetch, call hosted
    providers, import provider SDKs, or load provider credentials. Hosted
    provider actions are classified by the 2026-05-23 closure as future
    optional-plugin/out-of-core work requiring a product decision with
    human-principal confirmation; current source scans found no provider SDK
    violation.

61. ~~**RFC 0068: Go production daemon port.**~~ ✅ Done for the current
    Go/Python cutover. D107 supersedes D105: Go is the production/default
    daemon and active contract-method parity has landed. The Python daemon
    selector/module, Python MCP wrapper, legacy local-state package, root
    DB/migration facades, direct corpus exporter, V1 local-state schema module,
    deterministic repo-local fixture, and broad skipped compatibility tests are
    deleted. Keep Python CLI/web clients where useful as daemon clients, and
    keep the guardrails that prevent the retired implementation paths from
    reappearing.

62. ~~**RFC 0069: PostgreSQL-only daemon-global surfaces.**~~ Done for
    current scope. Daemon sweep, dashboard-all, daemon MCP resource
    list/read, and registry probes now use PostgreSQL/Go-owned
    daemon handlers. Go now owns first-start PostgreSQL admin/runtime-token
    bootstrap. The retired Python `connect_registry()` implementation is gone;
    any new daemon-registry probe must be PostgreSQL/Go-owned. `dashboard.all`
    is now a Go/PostgreSQL read-only projection with per-active-run
    `run_progress` parity for phase progress, auto-finalize dry-run visibility,
    and supervisor stalls. The retired legacy Python daemon module is deleted;
    the Go daemon has a resident recovery scheduler over active PostgreSQL
    runs. Daemon MCP resource list/read now use PostgreSQL-backed repository
    visibility plus status/doctor/run/why/blocker/dashboard/stale-lease
    projections when `pg_conn` is present; the no-`pg_conn` retired registry
    fallback is retired and the legacy Python daemon module is deleted.
    `striatum daemon status`,
    `striatum daemon stop`,
    `striatum daemon health`, `striatum daemon audit`, and
    daemon-global/repo-scoped `read_doctor` now read from and audit to
    PostgreSQL when a daemon DB is configured, with legacy audit field names
    retained for CLI compatibility. The old single-variable legacy-registry
    opt-in has no active product or diagnostic surface. The terminal dashboard
    now renders production
    text frames from the daemon/PostgreSQL `dashboard` DTO; the old
    repo-local gatherer and its legacy package are deleted. Go `status` now
    matches the PostgreSQL/Python read-model shape
    for job counts, nested verdict posture counts, queue-based claimable work,
    blocker/checkpoint payloads, process health, supervisor stalls, phase
    progress, provenance mode, auto-finalize dry-run visibility, and
    deterministic next actions. Production daemon CLI/admin dispatch now uses
    PostgreSQL-only helpers, `dashboard --all` routes through daemon RPC, and
    the old CLI-side daemon registry wrapper is removed. Architecture tests
    now assert production sources do not import `striatum.daemon` and that the
    retired Python daemon module stays deleted.
    The 2026-05-23 closure ran the focused PG/global guardrail suite and
    found no actionable residuals. Future registry-probe/global-surface
    regressions are guardrail failures, not open TODO 62 work. The
    workflow-upgrade running-run guard is now PostgreSQL-only and fails
    closed when PostgreSQL state is unknown, even when retired local-state
    files are present.

63. ~~**RFC 0070: daemon client/service boundary completion.**~~ Done.
    Daemon-side `repo.resolve` is registered as a daemon-global read bootstrap
    method; CLI and service clients no longer open daemon PostgreSQL to map a
    repo path to `repository_id`; daemon-mapped `/v1/invoke` production reads
    and mutations route through daemon RPC; local MCP `striatum/invoke` and
    chat mapped commands share that daemon-routing policy, while local MCP
    `tools/list` / `tools/call` no longer expose CLI-shaped aliases; D110
    removed the old dogfood composites from the production daemon
    contract, and D112 removed `apply.reviewed_patch`. Production daemon MCP
    `tools/list` now hides local
    workflow-file authoring methods in both Python and Go, while direct calls
    to removed method names audit as `method_unknown`. The 2026-05-23 closure
    records primitive daemon methods as the supported production path. The
    removed `dogfood.publish_on_behalf`, `dogfood.surgical_recovery`, and
    `apply.reviewed_patch` names stay out of the production contract unless a
    future accepted product decision introduces PostgreSQL-native composites
    or sealed apply.

64. **RFC 0071: operator diagnostics and cutover evidence.** Diagnostic slices
    landed: `striatum daemon doctor --authority --json` reports
    `striatum.authority_report.v1`, including PostgreSQL live-state authority,
    retired local-state status, method fallback counts, and remediation
    recommendations;
    `striatum daemon doctor --repo <path> --authority --json` reports
    `striatum.repo_cutover_report.v1` without opening SQLite as a database.
    D108 keeps the command authority matrix
    curated for authority/status classification while architecture tests now
    enforce generated CLI route labels and runtime CLI fallback cells.
    The direct PostgreSQL bootstrap/admin plane is now explicitly listed in
    the matrix and guarded by an import scan so ordinary workflow commands
    cannot quietly add direct daemon-PG helper imports.
    `striatum daemon doctor --repo <path> --authority --json` now mirrors the
    verify-only repository cutover report in doctor output and summarizes that
    repository cutover in the authority report. Remaining: no known
    implementation work for the accepted RFC 0071 diagnostic slice.

65. **RFC 0058: operator progress surface.** Done:
    `operator_brief`, `work_plan`, `progress_note`, and `operator_report`
    are publisher-known artifact kinds with V1 front-matter schemas;
    corpus export emits operator-doc metadata columns; and `docs/operator/`
    now carries the current brief, open plans, progress notes, and a handoff
    deprecation pointer. V1.5 landed `striatum operator current-brief`,
    configurable `--operator-docs-root`, daemon-enforcement/RPC exemptions for
    that local read, and schema errors for `operator_brief`
    `context_budget_lines` overruns. The 2026-05-23 closure classifies
    operator-tree init/rotation as optional future work outside the accepted
    RFC 0058 V1.5 slice.

67. ~~**RFC 0130/RFC 0075: MCP cutover and tmux-observable sessions.**~~
    Done. Native Go daemon MCP HTTP/SSE, autonomous MCP packet-loop proof,
    `session.report`, agent-loop PTY bootstrap, Python MCP wrapper deletion,
    RFC 0077 daemon-owned MCP activity liveness, tmux attach metadata, web
    session-observability rendering, and fail-closed tmux opt-in have landed.
    D131 accepts RFC 0075 for the current scoped implementation. The final
    cutover adds UI parity for
    remaining operator actions, updates current docs and agent skill templates
    to MCP-first workflow control, and reclassifies all non-read CLI routes in
    `docs/architecture/CLI_RETIREMENT_PARITY.md` as bootstrap,
    lane-compatibility, or operator-compatibility survivors. No live
    workflow-control operation now requires CLI; pane output and transcripts
    still must not become workflow state. Hiding or deleting CLI compatibility
    verbs is a later deprecation/release decision, not a TODO 67 blocker.

68. **RFC 0078: Go-only runtime and Python removal.** Scaffolded, executed,
    and mostly landed. The owner
    direction is to remove all Python traces from the active Striatum
    repository head, not only the already-retired Python daemon/MCP/local-state
    paths. The RFC supersedes the RFC 0068/RFC 0070 carve-out that allowed
    Python CLI/web clients to remain useful, and scopes a full cutover ledger:
    Go CLI parity or explicit command retirement, Go local web/service
    replacement or route retirement, workflow-authoring and artifact-schema
    ports, Python-test-to-Go coverage migration, Go-only packaging/release docs,
    and guardrails that keep Python source, tests, packaging, and active
    operator instructions from returning. Git object history rewrite is out of
    scope. The 2026-05-24 workflow
    `docs/operator/workflows/rfc-0078-go-only-runtime-and-python-removal/workflow.json`
    completed as `run_ef93ee9055bb77e40d2ae2c846337176`: it used
    `max_active_jobs: 20`, reached the current six-sub-agent execution limit,
    produced the cutover ledger and per-surface handoffs, added the first Go
    `striatum workflow validate` CLI scaffold, and expanded Go artifact
    contract/front-matter parity for operator, Git/PR, and auto-finalize gate
    artifacts. Remaining work at that point was the final documentation rewrite
    and the terminal Python source/test deletion. On 2026-05-25, the remaining work
    was split into six dedicated executable workflows plus an umbrella tracker,
    then executed with six parallel sub-agents. Landed slices include the
    generated Go CLI RPC router, shared Go artifact contracts, expanded Go
    workflow validation and generator reuse, Go web service/security/static/SSE
    scaffolding, Go-only release archives and smoke scripts, and the
    Python-trace deletion guardrail. Aggregate validation is green for Go
    tests, workflow validation, frontend API-client tests, release/package
    smokes, doc-link/current-brief tests, and route freshness checks.
    RFC 0078 is complete: the terminal `src/` Python tree was deleted
    (Gate G, commit a382dd7d) and the runtime is Go-only.

## GH issue follow-ups

40. ~~**GH #14 — recovery cannot clear terminal-run
    `process_exit_nonzero` blocker.**~~ Done: `docs/issues/14/`
    completed with accepting review; the CLI and PG recovery paths can
    dismiss terminal process blockers without requiring a current lease,
    the process adapter avoids creating new post-terminal process blockers,
    and the autonomous recovery sweep reports or clears them according to
    policy.

41. ~~**GH #15 — docs clarify PostgreSQL transition guidance.**~~ Done:
    README/SPEC/getting-started/human/MCP/Postgres-transition docs now
    describe daemon-owned PostgreSQL as live workflow state and `.striatum/`
    as operational scratch or migration/tombstone context only.

42. ~~**GH #17 — Striatum doc consistency for Engram memory integration.**~~
    Done: `docs/issues/17/` completed with accepting review and RFC 0057
    now carries the Corpus Contract V2 decision surface. Remaining Corpus
    V2 implementation is tracked separately under Phase 11 / TODO 59.

## Immediate Follow-Up

F1. ~~Exercise the minimal process adapter on a Striatum-owned version of the
    historical bootstrap workflow, then retire the tmux harness from active
    workflow guidance.~~ Done: `examples/three-lane-design-build-review/`
    carries the runner-owned workflow fixture and
    `tests/test_example_workflows.py` validates the graph and referenced
    files.

F2. ~~Define any fuller publication policy after the initial package smoke,
    typecheck, metadata check, and macOS/Linux CI wiring.~~ ✅ Done:
    `docs/SPEC.md` now documents every registered front-matter schema from
    `FRONT_MATTER_SCHEMAS`, including operator/provenance artifacts and
    Git/PR request artifacts, and `tests/test_artifact_schemas.py` guards
    future schema additions against SPEC drift.

F3. ~~Land the round-6 follow-up integrations.~~ Done: RFC 0002 landed
    (D051) — reviewer-policy workflow fields plus work-packet exposure plus
    RFC 0014 fixture labels. RFCs 0003/0004/0005 landed (D052/D053/D054) —
    migration v5 opens `artifacts.artifact_kind` to Python validation, three
    new kinds (`support_ledger`, `action_item_ledger`,
    `harness_improvement_proposal`) registered with v1 front-matter schemas,
    workflow + publish validation reject unknown kinds, and
    `examples/support-ledger-flow/` ships as the reference fixture.

F30. ~~Land lane-liveness attestation and provenance-mode guardrails.~~ Done:
    unattested sessions now publish under `author: operator`, operator labels
    are constrained and self-declared, review jobs can require an attached lane
    supervisor, and `sealed_patch` workflows validate structurally but refuse
    to start until real containment exists.

F31. ~~Land the RFC 0028 V1 daemon acceptance slice.~~ Done: optional
    `striatumd` / `striatum daemon start`, daemon registry, repo
    add/list/remove with explicit `--init`, explicit daemon read routing,
    global dashboard, resources-only daemon MCP with explicit token
    parameters and repo-scope filtering, metadata-only audit with segment
    checks, and foreground recovery sweep events bylined
    `striatumd-<instance-id>` are in place. The old V1 deferred list has
    since been resolved or reclassified: daemon supervision, MCP mutation
    tools, cross-repo foundations, and service-manager support landed; sealed
    apply, full live cross-repo scheduling, Windows support, and local
    multi-operator tenancy require separate accepted RFCs before
    implementation.

F32. ~~Land the RFC 0030 + RFC 0031 daemon V2 foundation.~~ Done:
    envelope-v1 daemon RPC codec, handshake, method registry, owner-local
    transport guards, PostgreSQL request/audit helpers, daemon DB
    supervisor/apply receipt tables, repo-local supervisor pointers, and
    fail-closed sealed-apply authority helpers.

F33. Add a seeded live-PG trajectory test (RFC 0081) via `go/pkg/pgtest`: seed a
    run with bus messages + artifacts and assert `trajectory.export` reproduces
    them in derived-`seq` order for both `dialogue` and `provenance` profiles,
    with a D028 guard that no projected row carries provider stdout/stderr.
    Non-blocking follow-up from the v2.2.0 closure (verify evidence:
    `docs/operator/artifacts/rfc-0079-0081-closure/verify/SUMMARY.md`); the
    feature is already verified end-to-end against the recorded two-model
    conversation run.
    ✅ DONE (v2.3.1): `go/pkg/reads/trajectory_integration_test.go`
    (`TestTrajectoryExportReproducesSeededRun`) seeds a run with chat messages +
    an artifact and asserts `trajectory.export`/`watch` reproduce them in
    derived-`seq` order for `dialogue` and `provenance`, with a D028 assertion.

F34. Implement owner-applied daemon migrations (RFC 0079 §5). The daemon
    currently auto-migrates as runtime role `striatumd_rw`, which cannot DDL
    owner-held `striatumd` tables (surfaced when RFC 0081's migration 0015
    crash-looped the daemon). Add `striatum daemon migrate` run via an
    owner/admin DSN (or acquire the admin DSN for the migrate step only) and a
    guard test asserting a migration that adds an owner-referencing object also
    `GRANT`s the runtime role. See `docs/DECISION_LOG.md` D135 and RFC 0079 §5.
    ✅ DONE (v2.3.1): `striatum daemon migrate-db` applies pending migrations via
    an owner/admin DSN (`--admin-url` / `STRIATUM_DAEMON_ADMIN_DB_URL` /
    daemon.toml fallback). Named `migrate-db` to avoid the retired SQLite-era
    `daemon migrate`. Migration 0016 is already proven ownership-safe by
    `TestMigrationSixteenInterrogationsIsOwnershipSafe`; a general migration-lint
    guard remains optional.

F35. Review remaining `src/striatum/web` residue. RFC 0078/0079 cleanup removed
    the dead Python trees, but `src/striatum/web/static`, `web/static/build`,
    and `web/templates` are still tracked. Confirm which the Go web service
    (`go/pkg/webassets`) actually serves/embeds, then migrate the live assets
    under `go/` (embed) and delete the dead remainder so `src/striatum` holds
    only the live Node frontend source. Non-blocking follow-up from the v2.2.0
    closure.
    ✅ DONE (v2.3.1) for the dead part: deleted the Python-era Jinja
    `src/striatum/web/templates` (the Go web service embeds its own
    `go/pkg/webassets/{static,templates}`). Kept the live Node frontend
    (`src/striatum/web/frontend`) and the bundle pipeline
    (`src/striatum/web/static/build`, gated by `make ui-check-bundle`).
    Remaining architectural finding split out as F36.
    ✅ FULLY CLOSED (RFC 0078 Go-only cutover): the entire `src/` tree —
    including `src/striatum/web/{static,static/build,templates,frontend}` and
    its Node/Vite bundle pipeline — was deleted. No `src/striatum/` residue
    remains tracked; the Go web service ships only its embedded
    `go/pkg/webassets/{static,templates}` assets. F36 is resolved alongside it.

F36. Decide and wire the Go web service's served assets. `go/pkg/webassets`
    embeds only three hand-authored files (`app.js`, `base.css`, `page.html`);
    the React/Vite islands bundle built to `src/striatum/web/static/build` is
    built + bundle-hash-checked in CI but is NOT embedded or served by the Go
    daemon's web service. Decide whether the Go web service should embed/serve
    the Vite bundle (repoint Vite `outDir` into `go/pkg/webassets/static/build`
    and extend the embed + `page.html` island mounts) or whether the minimal
    server-rendered surface is the intended product. This is a web-UI product
    decision (likely a short RFC), not residue cleanup. Surfaced 2026-05-25.
    ✅ RESOLVED BY RFC 0078: the React/Vite islands bundle and its
    `src/striatum/web/static/build` output were deleted in the Go-only cutover
    rather than embedded, so the minimal server-rendered Go surface is the
    intended product. `go/pkg/webassets` embeds `static/{app.js,base.css}` plus
    `templates/{page,conversation,interrogation}.html` (`//go:embed`); no Vite/
    Node bundle pipeline remains and no web-UI decision is pending here.

F37. Full `docs/CLI_REFERENCE.md` audit against the Go command surface. The
    2026-05-25 doc scrub corrected the workflow-authoring verbs (`validate`,
    `generate`, `templates {list,show}`), removed the unported
    `workflow {init,lint,plan,graph,upgrade,templates render-md}` prose, and
    added `daemon migrate-db`. A complete pass should reconcile every documented
    verb against the daemon route table (`go/pkg/cli/routes`) + local commands
    (`go/pkg/cli/localcommands`), flagging any other Python-era spellings that
    no longer exist in the Go CLI. Non-blocking doc accuracy follow-up.

F38. Per-run repository resolution for the daemon-mounted web service. RFC 0084
    D1 mounted `/v1` on the daemon listener, but the web service injects
    `repository_id` only from `STRIATUM_DAEMON_WEB_REPOSITORY_ID`; the daemon is
    multi-repo, so run-scoped routes (`/v1/runs/{runID}/...`) return
    `repo_not_registered` unless that env scopes the daemon to one repo. Resolve
    `repository_id` per-run from the `runID` path segment (runs are globally
    unique and belong to one repo) so the mounted web service works across all
    registered repos without per-deploy config. Surfaced by D1 live verification
    2026-05-26.

F39. Harden the MCP mutation surface for review/publish. In the RFC 0083/0084
    live runs, lane agents reported that the MCP `review.verdict` and
    `artifact.publish` tools returned opaque errors and fell back to the CLI
    `commands` block; the finding schema also silently requires a
    `verdict_intent` front-matter field the publisher rejects without naming.
    Reproduce, surface structured/actionable errors through the MCP tool path,
    and make the missing-`verdict_intent` rejection name the field. Surfaced
    2026-05-25/26.

F40. Diagnose the `striatum release` (work.release) hang. During the 2026-05-25
    operator-driven run, `striatum release --run-id --session-id` blocked past
    120s (had to be abandoned). Reproduce against `HandleReleaseWork`
    (`go/pkg/mutations/lifecycle.go`) + the CLI client, identify whether the
    block is daemon-side or CLI-side, and fix or add a timeout. Surfaced
    2026-05-25.

F41. Derive the RFC 0085 identity route-audit allowlist from the dispatch table.
    `TestIdentityRouteAuditMatchesAllowlist` is normative for the current route
    surface, but `allRoutes` is kept in sync with `routeGET`/`routePOST`
    manually — a future route added to the router without updating the audit
    list would not be caught (RFC 0085 build-review finding). Make the audit
    enumerate routes from the actual dispatch so a new mutating route is rejected
    over the identity socket by construction. Surfaced 2026-05-26.

F42. [DONE 2026-05-26, v2.6.0, D145] Harden gemini-cli as an autonomous
    agent-loop participant. Shipped the generic `striatumd -agent-loop
    -turn-driver` (pure `go/pkg/turndriver` loop): a non-self-driving lane
    declared `adapter_capabilities.single_shot: true` runs under a Striatum-owned
    turn-driver that holds the MCP client and calls `conversation.say`, invoking
    the child once per turn as a topic+transcript content generator (selection by
    capability, not model name). `ContentOnlyEnv` strips `STRIATUM_*`; the
    spoon-feeding boundary is reflection-test-pinned. Designed+built via the
    iterated-interrogating-panel dogfood `run_63a8ffa4a77edebfd25620876fe9e7ce`
    (4× accept_with_findings, real interrogations). The `/tmp/gemini-driver.sh`
    hack is obsoleted. Follow-ups recorded in
    `docs/operator/workflows/f42-conversation-turn-driver/OPERATOR_REPORT.md`:
    interrogation-window closes after the first interrogation (breaks sequential
    multi-reviewer panels); reviewer sessions need the `interrogate` capability at
    registration; `review.submit` must be a single call. Live gemini verification
    PASSED 2026-05-26 (conv reached max_rounds; gemini's turns driven by
    `striatumd -agent-loop -turn-driver` via `supervise.start`, no shell script)
    — see F44 for the PATH bug found + fixed during verification.

F44. [DONE 2026-05-27, v2.7.0, D146] Make daemon-spawned single-shot
    turn-driver lanes find their generator binary, and fail gracefully. Shipped:
    supervised lanes append existing operator-local bin dirs (`$HOME/.local/bin`,
    `$HOME/.npm-global/bin`, `STRIATUM_SUPERVISED_PATH_DIRS`) to one deduped
    `PATH`; `turndriver.Loop` routes exhausted generator failures through
    `OnFailure`/escalation instead of crashing; pipe supervisors reap via async
    `cmd.Wait` and liveness is zombie/start-token-aware (`supervise.status`
    reports `gone`, not stale `alive`). Built via dogfood `run_8e1f8965…` (4×
    accept_with_findings). Live-verified in isolation on a minimal-PATH daemon:
    supervised PATH augmented, generator executed, no zombie, honest liveness; the
    operator `path.conf` workaround is retired. Deferred: durable terminal-state
    persistence; resident retry-after-escalation. ORIGINAL CONTEXT BELOW.
    Live F42 verification found the supervised
    turn-driver zombies because it inherits the daemon's systemd `PATH`
    (`/usr/local/sbin:…:/snap/bin`), which lacks `~/.local/bin` /
    `~/.npm-global/bin` where `gemini` lives: `exec: "gemini": executable file
    not found in $PATH`. (1) `supervise.start` should add the operator local bin
    dirs to the supervised lane `PATH` (or resolve the lane command to an
    absolute path); (2) generator-not-found / repeated generation failure should
    park the floor + escalate, not crash the whole turn-driver; (3) the daemon
    should reap exited supervised children and not report stale `alive` liveness.
    Local workaround in use: a `striatumd.service.d/path.conf` systemd drop-in.
    Surfaced 2026-05-26.

F43. Render conversations in the chat UI. RFC 0086 conversation turns are
    queryable (`conversation.show`/`list`) and persist on the message bus like
    interrogation turns; reuse the RFC 0084 chat renderer (speaker = participant
    session/lane) and add `/v1/runs/{runID}/conversations[/{id}]` GET routes so a
    3-way conversation is viewable as chat (read-only, over `tailscale serve` per
    RFC 0085) — the same way interrogation threads are. Surfaced 2026-05-26.

F45. Make the turn-driver content generator hermetic (gemini-slowness diagnosis,
    2026-05-27). Diagnosis: the turn-driver (`CommandGenerator`,
    go/pkg/agentloop/turn_driver.go) runs the per-turn content generator in the
    TARGET-REPO working directory, so gemini-cli loads the repo's project
    `.gemini/settings.json` and tries to connect its configured `striatum` MCP
    server every turn. That config pins a daemon loopback port, which rotates on
    every daemon restart, so it goes stale → "MCP issues detected" + ~3-9s of
    failed-connection overhead per turn (measured: repo cwd WITH stale MCP
    13.8-20.4s vs WITHOUT 11-12.8s). Under concurrent invocations / a
    less-responsive endpoint this compounds toward the 180s `GenerateTimeout`
    (the ~3.5min no-progress window seen in F44 live verification). Base
    gemini-2.5-pro latency is itself ~11-13s/turn (vs ~3s trivial) — inherent,
    not a bug, but relevant for `GenerateTimeout` sizing and lane choice
    (claude/codex turn over much faster). Fix: (1) generic — run the content
    generator in a NEUTRAL/temp working directory, not the target repo, so no
    project agent-config (`.gemini`, `.codex`, …) or its MCP servers load (also
    tightens the D145 content-only boundary); (2) targeted — for gemini, pass
    `--allowed-mcp-server-names` (empty) so it never attempts MCP regardless of
    cwd. Immediate operator cleanup: the repo `.gemini/settings.json` is stale
    local cruft from the manual gemini-driver experiments pointing at a dead port
    (gitignored, not in repo) — remove/refresh it. Surfaced 2026-05-27.
