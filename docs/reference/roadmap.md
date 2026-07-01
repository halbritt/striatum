# Striatum Roadmap — ARCHIVED

> **ARCHIVED (2026-06, D232).** This roadmap is no longer maintained. It is
> **superseded by [`docs/operator/BRIEF.md`](../operator/BRIEF.md)** plus the
> live operator cold-start: run **`striatum operator bootstrap --markdown`**
> and follow its `next_actions` and bounded `reading_plan`. The RFC frontier
> lives in the RFC index ([`docs/rfcs/README.md`](../rfcs/README.md)) and the
> open GitHub issues. This file was last anchored at `v1.57.0` and never
> refreshed through the v2.x line; the repository is now well past it. The full
> historical roadmap is preserved verbatim below the divider for provenance —
> treat every status claim in it as stale.

---

## Historical roadmap (archived, stale — kept for provenance)

# Striatum Roadmap

**Purpose:** This is the operator kickoff document. If you are picking up
Striatum cold — or resuming after a context compaction — read this first.
It sequences the deferred and blocked work in `docs/TODO.md`, the proposed
RFCs under `docs/rfcs/`, the open GitHub issues, and the in-flight
dogfood follow-ups; everything else is the authoritative status source.

This roadmap is *opinionated about ordering*. Items here exist in TODO,
RFCs, GH, or DECISION_LOG already — the roadmap only adds sequencing,
dependency edges, and "what would I do next" framing. Update on every
`vX.Y.0` version bump; treat as stale on minor bumps.

---

> **STALE (as of 2026-06-01):** this roadmap is anchored at `v1.57.0` /
> RFC 0078 and has not been refreshed through the v2.x line. For the current
> frontier read **`docs/operator/BRIEF.md`** and the RFC index
> (`docs/rfcs/README.md`). Short version: latest release is **v2.8.0**; the
> active arc is **RFC 0101 robust autonomous workflow execution** — Phases 1-2
> (honest liveness + adapter conformance) landed and deployed at schema 19,
> Phases 3-5 (autonomous recovery → loud escalation → chaos suite) are in
> progress. Treat the sequencing below as historical until the next `vX.Y.0`
> refresh.

## 1. State as of 2026-05-25 (RFC 0078 Go-only cutover in progress)

- **Latest tag:** `v1.57.0` remains the latest published release tag. The
  repository head is preparing the RFC 0078 Go-only archive cutover with root
  `VERSION=2.0.0`; do not describe `pyproject.toml` as the current release
  source.
- **Latest substantive release:** v1.57.0 — RFC 0073 implementation
  (daemon doctor surfaces blob diagnostics block), PG migration 0010
  (column-aware artifacts_no_update trigger), and `repo list`
  non-JSON cleanup (no more SQLite pre-flight). Closes the
  blob-routing observability and trigger-fragility gaps surfaced by
  the v1.56 backfill.
- **Current workstream:** RFC 0078 Go-only runtime and Python removal on top
  of the closed residual/deferred backlog. The 2026-05-23 closure artifacts
  under `docs/operator/`
  classify TODO 62, TODO 63, TODO 2, TODO 16, artifact schema/redaction, RFC
  0040 V1.6, and the deferred items formerly listed as 14-27. The actionable
  result is narrower than the old backlog: D125 evidence gate is satisfied
  and D133 flips default-live auto-finalize with explicit workflow opt-out,
  RFC 0130/0075 live workflow-control cutover is closed, TODO 52 and TODO 53
  have additional bounded cleanup slices landed, TODO 49/61 cleanup is closed,
  RFC 0074 Phase B generator support has landed for the lightweight
  `implementation_panel` shape. On 2026-05-25, the remaining RFC 0078 gates
  executed with six parallel sub-agents and landed the generated Go CLI RPC
  router, Go artifact contracts/workflow parity slices, Go web service
  scaffolding, Go-only release archives and smoke scripts, and the
  Python-trace guardrail. Final Python deletion remains blocked by active
  Python source, the Python test suite, packaging, scripts, and current guidance. RFC 0052 Phase A,
  RFC 0053 schema/runtime rename, Cross-Repo Live Scheduler V1, sealed apply,
  Windows support, and local multi-operator tenancy need separate accepted
  RFCs before implementation. Optional/out-of-core items are now explicitly
  classified rather than left as vague deferred work.
  D107 supersedes D105: Go is now the default production daemon core, active
  contract-method parity is landed, and the retired Python daemon module is
  deleted. RFC 0078 now supersedes the older "Python CLI/web clients stay
  useful" carve-out, but those source/test surfaces are not deleted until the
  trace guardrail blocker rows close. The 2026-05-24 cleanup deletes
  the remaining legacy local-state implementation residue: no legacy package,
  root DB/migration facades, direct corpus exporter, V1 local-state schema
  module, deterministic repo-local fixture, or broad skipped compatibility
  tests remain. D110 removed the retired migration and dogfood composite RPC
  names from the production contract, and D112 removed `apply.reviewed_patch`;
  stale direct calls to all removed names audit as `method_unknown`. D113
  closes writable import windows; the old migration spellings refuse without
  opening retired local state.
- **CI:** GitHub Actions has been backlogged during the 2026-05-17/18
  remediation commits. Treat latest-head CI failures as stop-the-line; queued
  and in-progress older runs are not by themselves blockers.
- **Active dogfoods:** no runner-owned workflow is currently live. The latest
  work is operator-scaffolded residual/deferred closure evidence plus focused
  tests.
- **Branches:** `main` is the active integration branch.

### 1.1 Current Operator Track: HTTP/SSE MCP Daemon And CLI Compatibility

Native HTTP MCP in the Go daemon has landed for RFC 0130 Phase A-G: `/mcp` is
the primary direct request endpoint, `/mcp/sse` remains the SSE/backcompat
alias, tool discovery/calls reuse daemon RPC authorization, the fake MCP agent
coverage completes a packet loop through `/mcp`, agent-loop is a PTY/MCP
bootstrapper, the Python MCP wrapper is deleted, and the final RFC 0130/RFC
0075 live workflow-control cutover is complete. The working spec is
[`RFC 0130 — Native Go Daemon HTTP/SSE MCP and Agent Loop`](../rfcs/0130-go-daemon-http-sse-mcp.md).

Order the work as a set of gates, not as one all-or-nothing cutover:

1. [done] Land a native Go `/mcp` endpoint, preserving `/mcp/sse` as an alias,
   with initialization and `tools/list`.
2. [done] Route one read-only daemon method through MCP with token enforcement.
3. [done] Route one low-risk mutation through MCP with fail-closed authorization and
   unsupported-method tests.
4. [done] Prove the lane work-packet loop with a fake MCP agent:
   prepare/start, `session.register`, `work.await_packet`, `work.ack`,
   `work.heartbeat`, `artifact.publish`, `work.complete`, and stale lease
   refusal now run through daemon-backed MCP `tools/call`.
5. [done] Refactor `go/pkg/agentloop` into a PTY bootstrapper that gives agents the
   endpoint/token/repository/lane instructions and then lets the agent use its
   own MCP client.
6. [done] Move live operator actions to MCP/UI surfaces until no
   workflow-control operation requires a human or AI operator to invoke
   `striatum` CLI verbs. The final RFC 0130/RFC 0075 cutover adds web/UI
   parity for the remaining operator actions, updates current agent docs and
   skill templates to MCP-first control, and reclassifies surviving CLI verbs
   as bootstrap, lane compatibility, or operator compatibility in
   `docs/architecture/CLI_RETIREMENT_PARITY.md`.
7. [done] Delete `src/striatum/mcp.py` and retire Python MCP launch docs.

For this roadmap, "eliminating the CLI" means eliminating CLI verbs as the
live workflow control plane. Bootstrap, diagnostics, and daemon-backed
compatibility commands survive intentionally; hiding or deleting commands is a
later deprecation/release decision.

Post-transition operator introspection is accepted in
[`RFC 0075 - Tmux-Observable MCP Agent Sessions And Liveness Deadlines`](../rfcs/0075-tmux-observable-mcp-agent-sessions.md):
live interactive agents use autonomous MCP sessions with daemon-owned
protocol liveness, tmux attach metadata, and a fail-closed tmux opt-in for
PTY-helper lanes. Daemon MCP activity and lease heartbeats remain the
authoritative liveness signals. Tmux panes are for human inspection, not
workflow state.
[`RFC 0077 - MCP Activity Liveness Deadlines`](../rfcs/0077-mcp-activity-liveness-deadlines.md)
landed the daemon-owned MCP activity timestamp and deadline-classification
slice; D131 accepts the current RFC 0075 tmux-observable session contract.
The current closure artifacts are
[`RFC 0130/RFC 0075 Final Cutover Design`](../operator/plans/rfc-0050-0075-final-cutover-design.md)
and
[`RFC 0130/RFC 0075 Final Cutover Implementation`](../operator/plans/rfc-0050-0075-final-cutover-implementation.md).

## 2. Just shipped (this week)

| Version | Scope | Notes |
|---:|---|---|
| v1.49.0 | RFC 0048 Phase A | Python PG-backed mutation handlers and router groundwork. |
| v1.50.0 | RFC 0048 V1.5 daemon accept loop | Unix-socket daemon RPC serving, role-provisioning runbook, and shutdown hygiene. |
| v1.51.0 | RFC 0048 Phase C dispatch | CLI verbs route through daemon RPC with fail-closed substrate plumbing. |
| v1.52.0 | RFC 0048 Phase C reads | Read-surface PG handlers for status/dashboard/list/run-summary/evidence/corpus paths. |
| v1.53.0 | Recovery and serve hardening | `requeue-stale --force --justification`, corrupted-state serve refusal, and `daemon doctor --explain`. |
| v1.54.0 | RFC 0048 Phase B read parity | Go read handlers plus Python PG-side stale-requeue message parity. |
| v1.55.0 | RFC 0048 V1.5 hardening + Schema v6 | Capability-denial matrix, audit-chain row lock, append-only grants, event-chain columns, and recovery inline-helper wiring. |
| Unreleased 2026-05-18 | Architecture remediation follow-through | Command authority guardrails, daemon-first web-service slices, Go daemon parity slices, RFC 0056 layout scaffold, and explicit product-decision blockers. |

## 3. Operator decision rules — read this before doing any work

These are historical operator patterns from recent dogfoods. Treat them as
recovery lore, not the default happy path.

### 3.1 Operator-on-behalf publish path (RFC 0046 V1, mandatory)

When an agent lane stalls but the on-disk artifact is valid:

```
striatum ack --session-id <S> --message-id <M> --lease-id <L>
striatum publish-artifact --session-id <S> --job-id <J> --lease-id <L> \
    --kind <K> --logical-name <N> --path <P> \
    --allow-no-process-execution \
    --override-rationale "<one-line reason>"
striatum verdict --session-id <S> --job-id <J> --lease-id <L> \
    --verdict <V> --rationale "<one-line reason>"
```

**Never** publish-on-behalf without `--allow-no-process-execution --override-rationale "..."`.
The 055b implementation now refuses model bylines without the override marker.
Every override gets audit-chained — that's the contract; respect it.

### 3.2 Operator verdict override (RFC 0046 V1)

When a reviewer's `needs_revision` is a packet-design artifact (e.g., the
review packet didn't include the fix-up's HANDOFF, so the reviewer correctly
refused on missing evidence), override after the fix-up dogfood ratifies:

```
striatum override-verdict --session-id <S> --job-id <J> \
    --verdict accept_with_findings \
    --auto-fresh-session \
    --rationale "<cite the fix-up dogfood commits + accepting reviewers>"
```

### 3.3 Fix-up dogfood pattern (054b → 055b → ...)

When an adversarial reviewer finds **V1 non-negotiable** violations:

1. Honestly submit the `needs_revision` verdict — do NOT override pre-fix.
   The run goes to `waiting_human` with a blocker.
2. Scaffold a `<N>b` fix-up dogfood whose implementer's spec is the
   adversarial REVIEW.md itself.
3. After 3-way build review of the fix-up ratifies the fixes, override
   the parent run's `needs_revision` verdict citing the fix-up's commits
   + accepting reviewers.
4. Both runs reach terminal `completed`. Ship as the parent run's version.

This pattern is in `docs/dogfood/054b/OPERATOR_REPORT.md` and `055b/OPERATOR_REPORT.md`.

### 3.4 Wrapper auth contract (v1.48.1)

`.striatum/bin/{claude,codex,gemini}-supervised-wrapper.sh` all enable
shell tool use without an interactive permission prompt:

- claude: `--permission-mode acceptEdits --allowedTools "Bash"`
- codex: `--dangerously-bypass-approvals-and-sandbox -c approval_policy=never`
- gemini: `--approval-mode yolo`

Filesystem boundaries are enforced by the packet's `write_scope`, not by
the CLI's permission system. **If you regenerate or reinstall wrappers,
verify these flags survive** — `striatum skills install --profile all` is
known to sometimes regenerate the wrappers.

### 3.5 Anti-patterns to recognize early

- **claude-no-publish** (was 10+ instances; mitigated by v1.48.1): claude
  wrapper alive, no `claude --print` subprocess produces work, no on-disk
  artifact. Check `$STRIATUM_SCRATCH_DIR/claude-logs/packet-NNNN.log`
  for the agent's last words — usually a permission prompt.
- **gemini-no-frontmatter** (3+ instances): gemini writes a valid review
  but the frontmatter lacks `verdict_intent` / `severity`. Operator must
  edit the file before publish-on-behalf succeeds. Don't fabricate a
  verdict the agent didn't intend — re-read the conclusion text.
- **codex/codex co-blindness** (5+ instances, D095-D098, D100):
  implementer and a reviewer are both codex; reviewer findings cluster
  around the implementer's blind spots, producing `needs_revision`
  verdicts that 2-of-3 cross-lane majority overrides. `workflow validate`
  now refuses same-model review-pair and revision-cycle lint findings by
  default; `--allow-same-model-pairing` is the explicit operator override.
- **packet-design gap** (observed dogfood-055b/056): fix-up review packets
  inherit the parent's `context.docs` and don't include the fix-up's
  HANDOFF + source diff. The reviewer correctly refuses on missing
  evidence. Either include the fix-up artifacts in the next workflow's
  `context_docs` or expect to override the codex verdict.

### 3.6 Cycle-exhaustion override

When a `needs_revision` verdict has no matching workflow cycle (workflow
declares 0 retries or the retry was already consumed), the runner opens
`blocker_kind: revision_routing` and `human_checkpoint`. Operator decides:

- **Real findings** → spawn a fix-up dogfood (§3.3).
- **Anti-pattern overrides** → record a D-decision (D095, D096, D097,
  D098, D099, D100, D101, D102 are precedents) and override via §3.2.
  Always document the anti-pattern variant in the decision record so
  future operators can recognize.

---

## 4. Active runway (this week, next 1-3 dogfoods)

### 4.1 ✅ shipped — Dogfood-057 / v1.49.0: RFC 0048 V1 Phase A handler port

**Reservation history.** §4.1 originally reserved dogfood-057 for the
v1.48.x security hardening (CSRF / origin-enforcement / context
validation) closing GH #9/#10/#11. That work shipped earlier the same
day via the `striatum/gh-issues-parallel` branch (commit `f5c8cca`
"Implement GH 9 10 11 security hardening"), making the original 057
reservation moot. **dogfood-057 was reassigned to RFC 0048 V1 Phase A**
(the substrate-facade fix) since it was the next biggest blocker on the
runway.

**Closes:** RFC 0048 V1 Phase A — the Python-side handler port. All 16
single-repo daemon RPC mutation handlers moved from SQLite-backed CLI
dispatch into native PG-backed handlers under
`src/striatum/daemon_pg/handlers/{workflow_loop,recovery_evidence}/`
with `DaemonRpcRouter._route` resolving the PG handler before the
legacy `CLI_ROUTES` fallback.

**V1 landing notes** (from the run's `OPERATOR_REPORT.md`):

- Codex F1-F4 (fail-closed routing, capability-denial tests,
  audit-chain concurrency, append-only role enforcement) accepted as
  V1.5 follow-up risk.
- Claude HIGH#1/#2 (byte-equivalence parity rig advertised but unused;
  `complete_inline`/`ack_inline` undefined and `recovery.resume
  --complete`/`recovery.auto` live-mode unreachable) accepted as V1.5
  follow-up risk.
- Schema migration for top-level `striatumd.events.previous_hash` /
  `row_hash` deferred — chain metadata currently lives inside
  `payload_json._event_chain` per implementer workaround.
- Run executed in legacy SQLite mode (`STRIATUM_DAEMON_REQUIRED=0
  STRIATUM_TEST_HARNESS=1`) because RFC 0048 is itself the gap that
  prevents the daemon-required CLI from working end-to-end. State-store
  corruption surfaced; SQLite was quarantined and reset.

**Follow-up:** completed through RFC 0048 V1.5 / v1.55.0. D105 briefly made
Python the primary production daemon core, but D107 / RFC 0068 supersedes that
constraint; Go is now the production/default daemon and the remaining daemon
cleanup is legacy fixture/import/module deletion.

### 4.2 🟡 landed bounded slice — RFC 0051 V1 auto-finalize from front matter

**Updates:** [TODO item 56](todo.md).

The bounded daemon slice has landed: `recovery.auto_finalize` supports manual
dry-run preview and default-live mode with explicit workflow opt-out, records
explicit `artifact.auto_finalized` and `job.auto_finalized` events, projects
eligibility/refusal state through status, dashboard, and web surfaces, and
leaves malformed/missing/byline-mismatched artifacts on the existing operator
recovery path.

**Current boundary:** global/default auto-finalize live allowance is enabled
by D133 after the D125 evidence gate. Workflows that require strict agent-only
finalization opt out with `recovery.auto_finalize.enabled=false`; status,
dashboard, and web projections remain dry-run/read-only previews.

### 4.3 ✅ completed — TODO #30 / RFC 0039 V1.6 Go support-runtime hardening

**Closes:** [TODO item 30](todo.md#L527).

**Status:** complete for the historical helper-focused hardening slice. D107
later reopened full Go daemon parity under RFC 0068, so this item is now
supporting groundwork rather than the daemon-core end state:

- (F1) `go/Makefile verify` now runs `go mod verify` and
  `go mod tidy -diff`.
- (F2) A startup regression asserts `striatumd` refuses to serve without a
  Postgres URL/config and does not bind its Unix socket.
- (F3) CI now runs `make daemon-go-conformance` on Linux/PostgreSQL as the
  production Go daemon conformance gate before default flip.
- (F4/F5) Helper boundary coverage now inspects transitive dependencies with
  `go list -deps ./cmd/striatum-supervisor-helper`; transitional Go RPC
  smoke/audit tests remain available but are not a parity gate.

**Suggested implementer:** claude (Go + Python harness). Deliberately
avoid codex (D101 precedent).

### 4.4 ✅ completed — Architecture remediation Phase 0: authority matrix and guardrails

**Closes:** [TODO item 48](todo.md).

**Why now:** the 2026-05-16 architecture review found the main product
risk is not a missing feature; it is authority ambiguity across daemon
RPC, native Python PG handlers, Go handlers, contract route translations,
and legacy local state. The next work had to make that ambiguity measurable
before deleting fallbacks.

**Landed in this slice:**
- `docs/architecture/COMMAND_AUTHORITY_MATRIX.md` inventories the current
  parser, route translator, daemon registry, Python PG handlers, and Go
  handler registrations.
- Guardrail tests fail when a daemon registry method lacks an explicit
  authority classification or when a handwritten fallback route appears
  without being named as transition debt.
- A retired-local-state tripwire test covers representative production-mode
  commands under daemon-required enforcement.
- `recovery auto-publish` no longer emits the unregistered
  `recovery.auto_publish` method.

**Current follow-up:** TODO item 61 / RFC 0068 is closed for the current
cutover. Keep the Go conformance gate green, keep the Python daemon
selector/module deleted, and preserve the authority matrix and contract tests
as drift guards.

---

### 4.5 🟡 substantially completed — Architecture remediation Phase 1: production fallback closure

**Updates:** [TODO item 49](todo.md).

**Landed in this slice:**
- Native Python PG handlers now cover `run.graph`, `worktree.*`,
  `supervise.*`, and the `recovery watch` CLI-local scheduler now delegates
  to daemon `recovery.sweep` without a `recovery.watch` RPC, in addition to the
  earlier read, workflow-loop, recovery, run lifecycle, branch, checkpoint,
  and decision handlers.
- `src/striatum/daemon_rpc/server.py` no longer imports or calls
  `striatum.api.invoke`; handwritten server fallback routes are gone and
  guarded by tests.
- Contract-backed CLI translations now route `run graph`,
  `worktree create/release/list`,
  and `supervise start/send/stop/status/list` through daemon RPC.
- Mapped CLI commands now fail closed when the route layer raises an
  unexpected exception, with an architecture guardrail proving the path does
  not open repo-local SQLite.
- `repo.add`, `repo.list`, and `repo.remove` now route through daemon RPC
  and update `striatumd.repositories` directly; `repo add` never creates
  `.striatum/retired-local-state`.
- The retired `striatum init` / `striatum adopt` paths no longer define
  production bootstrap; repo-local SQLite init is retained only for
  legacy test fixtures.
- Workflow authoring methods are no longer production live-state authority:
  legacy Python RPC compatibility code refuses them as CLI-local, Go implements
  the file authoring/generation handlers without mutating daemon state,
  production MCP tool listing hides them, and route tests prevent accidental
  production fallback.
- `workflow upgrade` checks daemon PG for non-terminal runs and fails closed
  whenever PostgreSQL state is unknown; it no longer has a repo-local SQLite
  fallback, including under legacy test-harness escapes.

**Phase 1 cleanup closure:** production daemon fallback is closed and the
legacy local-state implementation residue is deleted. `src/striatum/legacy_sqlite/`,
root `striatum.db` / `striatum.migrations` facades, the direct corpus exporter,
the V1 local-state schema module, the deterministic repo-local fixture, and the
broad skipped compatibility tests are gone. Remaining `retired-local-state` references
are refusal/inspection of a retired file name or redaction/test signals, not
live-state support.

---

### 4.6 ✅ completed — Architecture remediation Phase 2: daemon method contract source

**Updates:** [TODO item 50](todo.md).

**Landed in this slice:**
- `contracts/daemon_methods.json` is the source for all 104 registered
  daemon RPC methods, including deprecated aliases.
- Python `src/striatum/daemon_rpc/registry.py` builds `METHOD_REGISTRY`,
  `METHODS_ETAG`, and `daemon.describe` output from the contract while
  preserving the public `MethodEntry` shape.
- Go `go/pkg/rpc/registry_methods.go` is generated from the same contract
  via `scripts/generate_go_rpc_registry.py`; `go generate ./pkg/rpc` is
  reproducible and Go contract parity tests catch drift.
- CLI/MCP contract tests now assert routed CLI methods are registered,
  workflow authoring stays CLI-local, daemon fallback routes are unused,
  local MCP does not advertise CLI-shaped alias tools, and daemon MCP tools
  hide CLI-local, deprecated, and production-unsupported retired composite
  methods.
- Daemon MCP tool descriptors are now generated from `METHOD_REGISTRY`, so
  method name, required capability, and repository-scope mode are no longer
  hand-written in a Python MCP wrapper.
- `scripts/generate_daemon_method_tables.py` renders
  `docs/architecture/DAEMON_METHOD_TABLES.md` from the daemon method
  contract, with `--check` coverage to catch checked-in documentation drift.
- The runtime CLI command-to-RPC route map is now declared in the contract's
  `cli_routes` section and loaded by `src/striatum/cli/daemon_rpc_route.py`;
  that module retains only CLI-local parameter extraction. Focused tests keep
  workflow authoring CLI-local and catch route/contract drift.
- `cross_repo.cancel` now delegates to the PG-native participant-cancel
  runner through the daemon RPC route map; remaining cross-repo work is
  E2E coverage and unregistered prepare/start/reconcile policy, not an
  explicit placeholder. Go participant-cancel parity now covers terminal
  skips, preparing participants without local runs, inactive participant
  repositories, blocked-error persistence, and typed `not_found` errors for
  missing cross-repo run ids. Socket-level CORE=go conformance now exercises
  `cross_repo.cancel` through the Unix RPC daemon against live PostgreSQL
  state and audit evidence.
- Go `workflow.upgrade --add-phases` now matches the Python V1-to-V1.1
  phase-inference path for preview/apply, synthesis-job insertion,
  cross-phase edge rewriting, and non-terminal-run refusal.
- Go `workflow.generate --shape multi_phase` now matches the Python V1.1
  generator path for ordered phases, per-track job remapping,
  `phase_synthesis` gates, and cross-phase synthesis-to-entry edges.

---

### 4.7 ✅ completed / superseded — Architecture remediation Phase 3: daemon core strategy

**Closes:** [TODO item 51](todo.md).

**Decision:** D105 named Python as the primary production daemon core, but
D107 supersedes it. RFC 0068 has moved the production/default daemon to Go;
the retired Python daemon module is deleted, the Python MCP wrapper is deleted,
and the legacy local-state package/facades/fixtures are gone while Python
CLI/web clients remain useful daemon clients.

**Landed in this slice:**
- `docs/DECISION_LOG.md` records D107 and marks D105 superseded.
- TODO item 25 is reopened under RFC 0068.
- TODO item 30 remains completed helper groundwork.
- TODO item 61 owns the Go daemon port and Python-daemon retirement.

**Status:** RFC 0068 cleanup is closed for the current cutover. Keep the Go
contract/conformance gate green and prevent the retired Python daemon/Python MCP
and legacy local-state implementation paths from reappearing.

---

### 4.8 🟡 partially completed — Architecture remediation Phase 4: daemon-first web service

**Updates:** [TODO item 52](todo.md).

**Landed in this slice:**
- Added `src/striatum/service_daemon.py` as a narrow local-service daemon RPC
  helper.
- Web POST handlers for run cancel/pause/resume, job cancel/retry, and branch
  confirm now call daemon RPC instead of opening retired local state.
- Focused service tests cover daemon DTO routing for those POST paths.
- The web run-list page now calls daemon `list.runs` in production and renders
  the workflow identity/source DTO returned by the daemon handler. Only narrow
  subprocess compatibility fixtures may use local fallback paths.
- Chat-session briefing now calls daemon `list.runs` for its active-run
  summary and has a daemon-routing regression for the DTO path.
- The posture-verdict drill-down page now calls daemon
  `run.posture_verdicts` in production; retired local-state page reads are gone.
- The `/v1` JSON read endpoints for status, doctor, why, dashboard, and
  run artifact rollups now call daemon read DTOs directly instead of routing
  through the legacy CLI invoke wrapper. Test-harness fallbacks preserve the
  old subprocess fixture path only.
- The `/doctor` HTML page now calls daemon `doctor` in production, with
  grouped problem records and per-record recovery recipes still shaped for
  the template.
- The artifact detail page now calls daemon `artifact.show` with optional
  web context for run scoping, expected author line, and operator-on-behalf
  provenance. The existing raw-artifact endpoint remains backward-compatible
  with the default `artifact.show` metadata response.
- `/v1/invoke` now derives daemon-routed read classification from
  `METHOD_REGISTRY.required_capability`; only CLI-local workflow authoring
  reads remain in an explicit service-side allowlist, and daemon-mapped
  production reads/mutations dispatch through daemon RPC instead of
  `striatum.api.invoke`.
- Local MCP `striatum/invoke` and web chat tools now share that routing policy
  for mapped status, why, lifecycle, artifact, review, and recovery commands;
  local MCP `tools/list` / `tools/call` no longer advertise or execute
  CLI-shaped aliases. Local `api.invoke` remains for unmapped authoring and
  explicit test fixtures.
- Production service startup now verifies daemon/repository health through
  daemon `doctor` before binding. The old local-state integrity check is gone
  from the production service path.
- The web SSE stream now uses daemon `run.events` in production and retains
  only narrow subprocess compatibility fallback outside production.
- The workflow run-now POST path now calls daemon `run.prepare`,
  `branch.confirm`, and `run.start` in production, while preserving its
  historical field-level workflow validation response through daemon RPC
  error details.
- The run detail page now calls daemon `run.detail` in production for run,
  job, session, recovery-panel, verdict, and phase-progress state. The web
  service still owns HTML/SVG rendering.
- The job detail page now calls daemon `job.detail` in production for job,
  expected-artifact, artifact, process-evidence, and verdict state. Override
  context-token minting remains local to the web service.
- `src/striatum/service_http.py` now owns the pure HTTP/security helpers
  for token comparison, JSON content-type validation, origin parsing, bind
  origin derivation, argv flag lookup, and web-context HMAC tokens. The
  names remain re-exported through `service.py` for existing callers and
  tests.
- `src/striatum/web/chat_session.py` now owns chat transcript projection,
  chat-briefing construction, JSONL append, timestamp, stable-hash,
  safe-git, multipart parsing, session path/listing, display-message, and
  workflow-write confirmation helpers. `service.py` keeps HTTP routing,
  provider/tool orchestration, and response handling.
- The old `src/striatum/legacy_sqlite/service.py` quarantine module is now
  deleted. `service.py` no longer imports or opens retired repo-local state,
  and production web/API reads and mutations use daemon DTO/RPC paths.
- `service.py` no longer eagerly imports the legacy `striatum.api` wrapper at
  module load. The compatibility `invoke()` seam lazy-loads it only when that
  explicit legacy wrapper path is called.
- `src/striatum/web/static_assets.py` now owns bundled static asset lookup,
  path validation, content-type mapping, JSON error mapping, CSP/header
  selection, and response body orchestration. `service.py` keeps the thin
  `/static/*` route wrapper and supplies context callbacks.
- `src/striatum/web/workflows.py` now owns workflow editor file resolution,
  new-workflow scaffold payloads, validation, atomic writes, and If-Match
  handling. `service.py` keeps HTTP request parsing, template rendering, and
  JSON response mapping for the workflow editor routes.
- `src/striatum/web/run_list.py` now owns run-list presentation helpers for
  GitHub remote parsing, workflow source-path normalization, workflow tree-link
  construction, and run state-chip shaping.
- `src/striatum/web/dogfood_routes.py` now owns historical dogfood route
  dispatch and raw/page context construction. `service.py` keeps only a thin
  handler adapter for the browse surface.
- `src/striatum/web/artifacts.py` now owns safe artifact file resolution, raw
  download content-type selection, and inline Markdown rendering helpers for
  artifact views.
- `src/striatum/service_command_policy.py` now owns `/v1/invoke`
  read/mutation command classification; `service.py` keeps the
  `is_read_command` compatibility import and route-level request handling.
- `src/striatum/web/view_file.py` now owns repository file-view path
  validation, binary detection, text/Markdown payload shaping, and inline
  Markdown rendering. `service.py` keeps route-level rendering and legacy
  run-breadcrumb fallback injection.
- `src/striatum/service_sse.py` now owns SSE replay offset parsing, event
  framing, and daemon-backed run-event stream-loop control. `service.py`
  keeps per-run slot accounting and legacy fixture fallback selection.
- `src/striatum/service_state.py` now owns process-local service state,
  GitHub remote/default-branch caching, shutdown signaling, web-context secret
  generation, and per-run SSE slot accounting.
- `src/striatum/service_runtime.py` now owns local service runtime helpers for
  version/mode reporting, loopback bind validation, PID-file single-instance
  checks, startup exceptions, and idle shutdown waiting.
- `src/striatum/web/template_env.py` now owns HTML escaping and Jinja
  environment construction for server-rendered web templates.
- `src/striatum/service_request_security.py` now owns request authentication,
  bearer-token checks, same-origin mutation policy, and override-verdict
  web-context validation decisions.
- `src/striatum/web/workflow_generation.py` now owns workflow template
  listing/show and workflow generation preview/write response shaping.
- `src/striatum/service_request_io.py` now owns request-body parsing and
  JSON/HTML response helpers. `service.py` keeps stable route-level wrapper
  methods for existing call sites and tests.
- `src/striatum/web/doctor.py` now owns doctor page DTO loading, gated legacy
  fallback selection, record recipe shaping, problem grouping, template
  rendering, and response/error mapping. `service.py` keeps a stable route
  wrapper for `/doctor`.
- `src/striatum/web/workflows.py` now owns workflow browser index/detail page
  DTO shaping, including small index entries and detail graph-SVG selection.
  `service.py` keeps template rendering and HTTP error mapping for those pages.
- `src/striatum/web/job_detail.py` now owns job-detail template context
  shaping and override-context-token minting. `service.py` keeps daemon
  RPC/fallback and HTTP response mapping for the route.
- `src/striatum/web/artifacts.py` now also owns artifact-view template
  context shaping, byline display, recorded attestation chips, lane-evidence
  chips, and expected-artifact row shaping.
- `src/striatum/web/run_posture_verdicts.py` now owns posture-verdict
  template-context shaping and verdict-row filtering. `service.py` keeps the
  daemon RPC/fallback and HTTP error mapping for the route.
- `src/striatum/web/chat_routes.py` now owns chat page rendering, chat
  creation, provider send/tool-loop handling, workflow-write confirmation,
  stop redirects, and transcript SSE tailing. `service.py` keeps route
  dispatch and stable briefing/git-helper compatibility aliases.
- `src/striatum/web/run_pages.py` now owns run list/detail, job detail,
  artifact view, and posture-verdict page rendering, including daemon DTO
  loading, compatibility fallback selection, graph rendering, and template
  context assembly. `service.py` keeps route dispatch and stable private
  handler wrappers for existing tests/callers.
- `src/striatum/web/artifacts.py` now also owns artifact raw download
  orchestration, including daemon metadata lookup, gated legacy fallback
  selection, file loading, content-type selection, and response header/body
  framing through callbacks supplied by the service wrapper.
- `src/striatum/web/run_actions.py` now owns workflow run-now,
  branch-confirm, run cancel/pause/resume, and job cancel/retry route
  handling, including mutation gates, request-body validation, daemon RPC
  dispatch, dirty-tree/schema error mapping, and legacy fixture fallback
  delegation. `service.py` keeps route dispatch and stable private wrappers.
- `src/striatum/web/workflows.py` now also owns workflow browser and
  visual-editor route rendering/saving, including index/new/detail/edit
  template rendering, edit POST body parsing, validation-error projection,
  and stale-write metadata. `service.py` keeps route dispatch and stable
  private wrappers.
- `src/striatum/web/view_file.py` now also owns repository file-view route
  rendering, including tree/file template selection, error mapping, and
  breadcrumb injection through a legacy callback supplied by `service.py`.
- `src/striatum/service_api_routes.py` now owns JSON read helpers, repo-tree
  reads, daemon-read fallback handling, and run-event SSE route control while
  `service.py` keeps dispatch, authentication, and stable private wrappers.
- `src/striatum/service_routes.py` now owns GET/POST route selection while
  `service.py` keeps stable handler wrapper methods and endpoint contexts.
- `src/striatum/service_server.py` now owns TCP/Unix binding, PID-file
  handling, signal shutdown, and serve-loop orchestration while `service.py`
  keeps private compatibility wrappers.

**Remaining Phase 4 debt:** continue splitting `service.py` along stable
non-SQLite request-handling and rendering boundaries after the daemon-routed
paths are stable.

---

### 4.9 🟡 partially completed — Architecture remediation Phase 5: real escalation inbox

**Updates:** [TODO item 53](todo.md).

**Landed in this slice:**
- `escalation.list`, `escalation.show`, and `escalation.resolve` project
  human-principal escalations over existing blocker rows.
- The daemon method contract and generated Go registry include the escalation
  methods.
- CLI routing supports `striatum escalation list/show/resolve`.
- `striatum inbox --json` now runs as documented for the principal inbox,
  while `inbox --session-id` remains the session-packet helper.
- The `escalation` artifact kind and `striatum.escalation.v1` front matter
  schema landed, with workflow validation and publish-artifact coverage.
- Publishing an `escalation` artifact can now link to an existing
  escalation-class blocker via front matter; the linked artifact metadata is
  stored under `blockers.payload_json.escalation_artifact` and projected by
  `escalation.list` / `escalation.show`.
- Escalation projections verify the linked artifact still exists and matches
  id/path/hash metadata before surfacing it; idempotent escalation artifact
  publishes repair missing blocker links and reject conflicting existing links.
- The typed `striatumd.escalation_inbox` table has landed in both Python and
  Go migration sets, and escalation artifact linking updates that table.
- D130 closes artifact-only escalation creation as link-only. Publishing an
  escalation artifact does not synthesize live blockers or escalation inbox
  rows; live escalation state is created through `work.block` or a future
  accepted `escalation.create` design.
- `work.block` now validates blocker request shape and writes
  `striatum.blocker_payload.v1` payloads to blocker rows, escalation inbox
  rows, and block events in both Python and Go paths.

**Remaining Phase 5 debt:** consider a dedicated create/update method only if
product scope needs direct escalation creation, and decide whether to rename
the packet helper to `packet inbox`.

---

### 4.10 ✅ completed — Architecture remediation Phase 6: supervisor control channel

**Updates:** [TODO item 54](todo.md).

**Landed in this slice:**
- `supervise.send` returns an explicit delivered-unacknowledged state.
- `supervise.report` records wrapper control events for packet acceptance,
  agent start, artifact observation, progress, and agent exit without reading
  or parsing model stdout.
- Supervision tests cover event recording and stopped-state transition on
  reported agent exit.
- A standalone Go `striatum-supervisor-helper` binary now launches agents
  under PTY, forwards packet bytes from stdin or a FIFO, and emits JSONL
  control events (`agent_started`, `packet_accepted`, `progress`,
  `agent_exited`, `helper_error`) without importing daemon DB/RPC,
  mutation, read, apply, or cross-repo authority packages.
- `supervise.report` now consumes helper event batches from JSONL text, a
  path, or object lists; it records helper events through the existing durable
  event path, preserves helper timestamps as `reported_at`, records
  `helper_error`, and uses the existing `agent_exited` stopped-state
  transition.
- `supervise.reattach_status` now has a real daemon PG handler. It returns
  a read-only supervisor health DTO classifying supervisors as
  `reattachable`, `lost_candidate`, `needs_repair`, `needs_verification`, or
  `terminal`, including pointer/daemon-row context, PID liveness, PID
  start-time identity, and recommended operator action. Daemon `doctor`
  now surfaces non-healthy reattach states for stale supervisors without
  changing supervisor state.
- Lanes can opt in to `supervision.transport: "pty_helper"`. The daemon
  PostgreSQL supervision handler launches `striatum-supervisor-helper`,
  persists helper pointer metadata, and drains helper JSONL events through
  `supervise.report` during
  start/send/stop/status.
- Pipe-transport lanes can opt in to
  `supervision.stdin_delivery: "one_shot_eof"` for single-prompt commands
  that read stdin until EOF. Default supervised lanes keep the persistent
  FIFO contract.
- Runner-owned supervisor stall detection now marks stale attached lanes as
  `liveness: "stalled"` in `supervise.status`, adds status/doctor surfacing,
  and opens `heartbeat_stall_lease_expired` blockers when an attached
  supervisor's active lease expires without progress. The recovery path does
  not auto-kill the OS process.
- PostgreSQL lane-liveness attestation now matches the stricter legacy
  semantics: an attached supervisor row attests only when its session/run,
  live PID, PID start-time token, and command match the immutable workflow
  snapshot lane command.
- The Postgres supervision handler suite now has a focused real-helper
  integration test that launches `go/bin/striatum-supervisor-helper`, sends
  a work packet through the PTY-helper transport, drains packet-acknowledged
  and agent-exited JSONL events, and verifies the daemon/PostgreSQL
  state/event projection.
- `make daemon-go-helper-integration` now builds the Go helper and runs that
  focused Postgres-backed integration test, and CI runs the target on
  Linux runners with the Postgres service.
- Existing supervisor paths now perform restart reconciliation before
  delivery: `supervise.status`, `supervise.send`, and push auto-dispatch
  record `supervisor.reattached` for surviving PID identity,
  update daemon-instance metadata, fail closed for repair/verification
  states, and mark stale PID identity `lost` before writing to stdin.
- `tests/test_claude_supervised_wrapper.py` now runs the supervised-wrapper
  loop fixture across `.striatum/bin/{claude,codex,gemini}-supervised-wrapper.sh`,
  proving multi-packet delivery, inner-command failure isolation, clean EOF
  exit, temp scratch logging, and the non-interactive approval flags required
  by v1.48.1.

**Remaining Phase 6 debt:** none.

---

### 4.11 ✅ completed — Architecture remediation Phase 7: workflow risk lint

**Updates:** [TODO item 55](todo.md).

**Landed in this slice:**
- Added `striatum workflow lint <workflow.json> --json`.
- Lint findings are structured separately from validation errors.
- The first rules cover same-model review pairs/revision cycles,
  non-fresh review context, broad repo-write scope, repo-write without
  per-job worktree isolation, and review workflows with no revision or
  human-checkpoint escalation path.
- The local service read-command whitelist includes `workflow lint`.
- Opt-in strict mode landed: `workflow lint --strict` refuses warnings unless
  the operator supplies a non-empty `--override-rationale`, and JSON/API
  refusals include the lint payload under `error.details`.
- The workflow browser/detail pages surface lint warning counts and short
  warning lists without changing validation status.
- Lint now includes advisory coverage scoring for reviewer independence,
  fresh context, write isolation, revision/escalation path, and review
  posture diversity.
- `workflow validate` refuses same-model review-pair/revision-cycle lint
  findings by default unless `--allow-same-model-pairing` is supplied.
- Workflow generator preview envelopes and the workflow chooser surface the
  lint summary separately from generator warnings.
- Strict overrides can record an operator-supplied
  `--accepted-risk-decision-id` reference.
- Go daemon `workflow.lint`, `workflow.accept_risk`, and
  `workflow.accepted_risks.list` now provide D124 accepted-risk records bound
  to immutable workflow snapshots or canonical workflow fingerprints.
- Accepted-risk records are append-only PostgreSQL daemon state, require a
  decision artifact reference plus rationale, and are MCP capability-gated
  (`read` for lint/list, `admin` for accept).
- CLI client routing now exposes `workflow accepted-risks` and `workflow
  accept-risk` over the daemon accepted-risk methods.
- The local web workflow detail page now uses daemon lint/accepted-risk data,
  shows accepted warning state and accepted-risk records, and can append
  accepted-risk records through `workflow.accept_risk` when the local service
  is started with `--allow-mutations`.

**Remaining Phase 7 debt:** none for the accepted-risk authority/client
surface. Keep local `workflow lint` as advisory authoring unless it is
explicitly persisting accepted risk through daemon RPC. Do not make
workflow-file metadata a live authority.

---

### 4.12 ✅ completed; evidence gate satisfied — Architecture remediation Phase 8: auto-finalize from front matter

**Updates:** [TODO item 39](todo.md), [TODO item 56](todo.md).

**Landed in this slice:**
- Added `recovery.auto_finalize` as a daemon/Postgres recovery method with
  manual dry-run preview and default-live mode with explicit workflow opt-out.
- The checker validates declared required expected artifacts, stable mtime,
  front matter, exact byline, active lease/session ownership, and lane
  evidence before mutating state.
- Live review auto-finalize publishes the finding, derives the verdict from
  `verdict_intent`, records the verdict, and completes the job atomically.
- Events `artifact.auto_finalized` and `job.auto_finalized` mark
  auto-from-artifact reconciliation, and PG evidence artifact summaries expose
  `publish_origin=auto_from_artifact`.
- CLI routing and the shared daemon method contract include the split
  `recovery.auto_finalize` method instead of the former overloaded
  `recovery.auto` shape.
- Status/dashboard projections now include an `auto_finalize_dry_run` preview
  with eligible candidates and refusal reasons, and the web recovery panel can
  render the same read-only preview while live auto-finalize is globally
  allowed by policy.
- The recovery method surface is split: `recovery.sweep` is the canonical
  daemon RPC for `striatum recovery auto`, `recovery auto-publish` emits
  `recovery.auto_publish_stale_artifacts`, and the former deprecated
  `recovery.auto` alias is retired as `method_unknown`.
- The sweep invokes live auto-finalize before lazy lease expiry unless the
  workflow explicitly opts out and never supplies the standalone force override.
- PostgreSQL sweep executes configured checkpoint-timeout escalation hooks in
  live mode, reports hook eligibility without side effects in dry-runs, and
  folds hook failures into `escalations[]`.
- Recovery sweep acceptance coverage now pins a dogfood-shaped run where
  three valid written review findings auto-finalize without
  operator-on-behalf or override provenance.
- Stable skipped-candidate cause classes landed for the dry-run/live
  projections: every skip has `cause`, artifact refusals have per-artifact
  `cause`, and existing `reason` strings remain display-compatible.
- Lane-finalization visibility landed across dry-run/live return payloads,
  status/dashboard/web projections, and the Go SQL summary path.
- The consecutive-failure circuit breaker landed as mutable daemon state with
  workflow policy defaults, open-breaker dry-run/status visibility,
  force-resistant live refusal, explicit live reset, audit events, and mirrored
  Python/Go migration support.
- Recovery policy payloads now expose `global_default_mode="live"` plus
  the satisfied D125 default-live gate, and schema-bearing
  `auto_finalize_gate_evidence` artifacts validate the required three live
  successes, two lane shapes, and zero contested audit-chain events before
  the evidence gate can be marked satisfied.
- The 2026-05-24 synthesis evidence slice satisfied the D125 gate with three
  live successes across review, build, and synthesis lane shapes and zero
  current contested audit-chain events; D133 then flipped default-live
  allowance with explicit workflow opt-out.

**Remaining Phase 8 debt:** none for the evidence gate or default-live
cutover. Continue monitoring contested audit-chain events and false-positive
finalization reports.

---

### 4.13 ✅ completed — Architecture remediation Phase 9: UI packaging and bundle cleanup

**Updates:** [TODO item 57](todo.md).

**Landed in this slice:**
- `make ui-build` now clears `src/striatum/web/static/build/` before Vite
  emits assets, so stale hashed chunks cannot accumulate across builds.
- `make ui-check-bundle` runs the existing bundle drift check plus a
  deterministic bundle-size gate.
- `@vitejs/plugin-react` moved from runtime dependencies to
  `devDependencies`, with the lockfile updated.
- Focused packaging tests pin the clean-build Makefile contract, build-only
  dependency placement, and bundle-size checker behavior.
- The package archive now has a size gate aligned with the UI bundle gate.

**Remaining Phase 9 debt:** none currently actionable. Manual chunking is
monitor-only until bundle evidence shows the current Rollup output is a
problem; keep package-data/manifest loading aligned if Vite manifest output is
introduced later.

---

## 5. Near-term queue (after the active runway)

Order is **dependency-driven, not preference-driven**. Promote items up
when their blocker clears.

### 5.1 ✅ completed — RFC 0050 V2 ergonomics polish

**Closed:** [#12](https://github.com/halbritt/striatum/issues/12) (clipboard hijack), [#13](https://github.com/halbritt/striatum/issues/13) (ghost field).

The copy-on-click allowlist and workflow-editor purge are already covered by
targeted tests. The dogfood-056 ergonomic review items are not tracked as
active GitHub backlog unless they get promoted into explicit issues.

### 5.2 ✅ completed, superseded — D105 follow-up / Go supervisor protocol

**Historical note:** this slice was planned under D105. D107 / RFC 0068
later reopened TODO item 25's Go replacement-daemon phase.

Shipped scope from the D105 interval:
- Daemon RPC, authorization, audit, and domain transitions remain in Python.
- The Go support code handles the narrow PTY/process supervision protocol for start,
  send, stop/status, wrapper control events, reattach, and lost-state reporting.
- That interval did not deliver broad Go replacement-daemon parity.
- Focused CI and integration coverage validate the existing Python/Go boundary;
  RFC 0068 owns the broader conformance gate.

### 5.3 ✅ shipped — RFC 0048 daemon-side substrate migration (v1.49.0–v1.55.0)

All three phases landed:

- **Phase A** (v1.49.0): 16 single-repo mutation handlers ported into
  `src/striatum/daemon_pg/handlers/{workflow_loop,recovery_evidence}/`.
- **Phase B** (v1.50.0–v1.54.0 + follow-up): transition-era Go-core
  parity in `go/pkg/{reads,mutations}/`; daemon Unix-socket accept loop;
  12 read handlers byte-equivalent with the Python path. D105 temporarily
  narrowed future Go work; D107 / RFC 0068 later reopened full parity.
- **Phase C** (v1.51.0–v1.52.0): CLI dispatch routes ~30 mapped verbs
  through the daemon socket; mapped verbs fail closed instead of
  falling back to SQLite when the daemon is unreachable.
- **V1.5 hardening** (v1.55.0): F2 cap-denial test matrix
  (`tests/daemon_pg/test_capability_denial_matrix.py`), F3 audit-chain
  row-lock in `append_audit_row`, F4 append-only role-grant tests, HIGH#1
  parity rig (`tests/daemon_pg/handlers/_parity.py`), HIGH#2 inline
  helpers exported (`complete_inline`, `ack_inline`), schema migration
  0006 (events `previous_hash`/`row_hash` columns +
  `repo_event_chain_heads`).

### 5.4 TODO item 26 — Codex/codex pairing validator rule

5 documented instances (D095, D096, D097, D098, D100) of the implementer-
↔-reviewer co-blindness anti-pattern. Soft warning and strict lint refusal
with explicit override rationale have landed. The CLI `workflow validate`
path now refuses same-model review-pair/revision-cycle lint findings by
default, with `--allow-same-model-pairing` as the explicit operator override.

**Status:** complete for the validator-rule TODO. Durable accepted-risk
policy is tracked under TODO 55; do not add hidden daemon/generator refusals
without an accepted authority decision.

### 5.5 RFC 0049 (experimental) — Interactive claude lane via MCP — **SHELVED**

Decision D106 records the durable shelf decision. v1.48.1's wrapper auth fix
bought time; RFC 0049 is now a *capability* RFC, not a *blocker*. Reopen if
subscription-quota economics shift, Anthropic plan-credit terms change
materially, or an operator explicitly funds the PTY/MCP spike. (~100×
token-per-dollar improvement potential on Max 20x remains attractive but not
urgent.)

### 5.6 RFC 0047 — Decision-record propagation

Closes the GH #3 design surface (now-closed issue had no implementation
beyond an event row). Landed projection semantics: rejected decisions move
the run to `compromised` and supersede accepting verdicts; accepted
decisions can reopen a compromised run to `completed` while preserving the
supersession trail. The daemon/Postgres projection is carried by migration
0007.

### 5.7 Optional memory/corpus integration — Striatum Corpus Contract V2

**Driven by:** `~/git/engram/STRIATUM_MEMORY_ROADMAP.md` (Engram-side
roadmap dated 2026-05-14). Treat that roadmap as an external consumer
request, not a Striatum runtime dependency. Engram may augment Striatum
operators and workflow agents with retrieval-backed memory over exported
corpora, but Striatum must keep running with Engram absent and must not
pull from Engram unless an accepted policy explicitly opts a workflow or
operator surface into augmentation.

**RFC 0057 scaffold landed (2026-05-14); D126 resolved the core choices
(2026-05-21).** See
[`docs/rfcs/0057-corpus-contract-v2.md`](../rfcs/0057-corpus-contract-v2.md)
for the bounded V2 decision surface (contract version, multi-corpus
identity, redaction-tier metadata, incremental-export watermarks,
validation rules, V1→V2 backward compatibility, augmentation-boundary
regression coverage, optional context-injection policy). Filed through
the `docs/issues/17/` workflow; the scaffold is the Striatum side of
GH #17. D126 accepts composite `corpus_id` identity (`slug:sha256`),
graduated redaction tiers, workflow opt-in augmentation by reference with
agent-side fetch, hybrid archive bundles, verification replay by default,
read-only semantic inspection, no comparative replay, deep-chain verification
always, and optional daemon audit-chain cross-check. Archive defaults,
read-only inspection, manifest watermarking, and the core optional
augmentation-reference packet surface have landed.

**What already shipped on our side:**
- `striatum corpus export --since <ref> --out <dir>` (RFC 0044 V1,
  dogfood-046, v1.35.0) — nine JSONL files + `manifest.json`, redacted,
  with replay-stable hashes.
- Corpus Contract V2 manifest metadata now lands on new exports:
  `corpus_contract_version=2`, composite `corpus_id`, redaction tier,
  reference-only augmentation policy, `verification_depth=deep_chain`,
  hybrid archive defaults, corpus-scoped incremental export watermark,
  optional `git_snapshot_hash`, and V1-compatible verifier fallback for older
  bundles.
- Workflow-authored `augmentation.mode: "reference_only"` now exposes
  optional local `corpus_bundle` references on claimed work packets under
  `context.augmentation_references`. Missing or unreadable bundles are
  reported as optional metadata and do not block claims or state transitions.
- The augmentation-not-dependency boundary regression test in
  `tests/test_cli_corpus_export.py::test_no_engram_imports_or_memory_capabilities_in_striatum`
  pinning that no `import engram` / no `from engram` / no `memory.*`
  capabilities exist in Striatum source.

**External consumer asks (Striatum-side):**

1. **Corpus Contract V2 RFC** — RFC 0057. Define bundle manifest shape,
   source kinds, required + optional metadata, stable item IDs, content
   hashes, instance and repository identity, privacy/redaction metadata,
   incremental-export watermarks, validation rules, and backward
   compatibility. This is the dependency for external consumers that
   ingest Striatum exports.

2. **Multi-corpus support in the exporter** — the D126 composite
   `corpus_id` shape (`slug:sha256`) now lands in V2 manifests. Later work
   can add operator-selectable slugs without changing the verifier contract.

3. **Reciprocal augmentation-boundary record** — extend the V1
   regression test to cover any new Engram-integration entry points so
   the "Striatum runs without Engram" property survives the integration
   phases.

4. **Context-injection policy** — D126 chooses workflow opt-in augmentation
   by reference with agent-side fetch. The core packet-reference surface has
   landed; richer external-consumer fetch UX remains optional and must not
   become a hard prerequisite.

**Resolved decisions to encode before implementation** (from D126 and the
earlier Engram roadmap open-decision list):

- Composite `corpus_id` naming as `slug:sha256`.
- Graduated redaction tiers and tier metadata in the manifest.
- Workflow opt-in augmentation by reference with agent-side fetch.
- Hybrid archive bundle defaults and verification replay by default.
- Deep-chain verification always, with optional daemon audit-chain
  cross-check.
- Which log streams are mandatory vs. optional.
- How much git diff content to export by default.
- Incremental-export watermark storage location: landed in V2 manifests as a
  corpus-scoped git high-water mark with dirty-tree advanceability metadata.
- How to record Engram availability without creating a runtime dependency.
- Default per-packet memory injection budget.

**Suggested implementer:** no core Striatum implementation lane is currently
needed for Corpus V2 foundations. Future external-consumer or UI search UX
should land only behind a separate optional-augmentation decision.

**Blocked on:** no product decision blocker remains for the core V2 direction.
Optional external-consumer fetch UX remains out of core.

**Forward link:** §11 lists the Engram-side roadmap for context;
Engram's full backlog is at `~/git/engram/STRIATUM_MEMORY_ROADMAP.md`.

### 5.8 Documentation + role-model runway (RFC 0052-0056)

Five RFCs scaffolded in one operator session on 2026-05-14. They
cluster around the AI-operator-as-default + human-principal-as-
escalation model and the doc surfaces that express it.

Landing order: RFC 0053 first (already shipped to main — RFC, D103,
and doc-side fixes in SPEC, GETTING_STARTED, HOW_TO_HUMAN, plus the
UBIQUITOUS_LANGUAGE softening in fb0175c). Then RFC 0054 / 0055 /
0056 in any order (all single-track doc work). RFC 0052 implementation
is unblocked by the completed RFC 0048 substrate flip, but remains lower
priority than the active remediation runway unless scheduled explicitly.

- **RFC 0052** (committee deliberation workflow) — TODO #43.
  Phase 0 scaffold + schema sketches landed. A 2026-05-23 closure pass
  classified the V0 proposal as not directly implementation-ready. Schedule
  a bounded Phase A implementation RFC/design before production work; RFC
  0074 examples do not cover the typed debate/panel semantics.
- **RFC 0053** (human principal as escalation-only) — TODO #44.
  RFC body + D103 + doc-side prose realignment shipped on main.
  A follow-up wording sweep realigned reader-facing docs, CLI help,
  scaffold output, workflow-template text, and recovery skill templates
  around principal/operator language while preserving durable schema/state
  identifiers.
  Deferred Phase A landed under remediation Phase 5: `escalation`
  artifact-kind schema, publish-time blocker linkage, and daemon RPC
  projection methods.
  Phase B rename work is a coordinated schema/runtime migration, not a
  wording sweep: it needs a workflow schema version choice, `workflow upgrade`
  rule, PostgreSQL enum/check migration, Go/Python runtime updates,
  generator/catalog policy, and UI/read compatibility policy.
- **RFC 0054** (day-zero usage guide) — TODO #45. Phase 0
  scaffold + **Phase A shipped in v1.55.0** (commit `a88f44d`):
  `docs/USING_STRIATUM.md` added as a new doc alongside
  `GETTING_STARTED.md` (resolved Open question 1 toward additive,
  not replacement). Tutorial-warm tone; under 200 lines. The optional
  guide-to-layout harvest is closed as not warranted because operator
  onboarding does not belong in the generic target-repository DDD scaffold.
- **RFC 0055** (marketing README + architecture graphics) — TODO
  #46. Phase 0 scaffold + **Phase A shipped in v1.55.0** (commit
  `a88f44d`): `README.md` rewritten with vision-first framing,
  value-bullets above-fold, Mermaid architecture diagram, and a
  demoted docs-link table at the bottom. SVG polish is closed as no-action
  unless a concrete docs/product need appears.
- **RFC 0056** (consumer-repo directory-structure opinions) —
  TODO #47. Phase 0 scaffold + **Phase A shipped in v1.55.0**
  (commit `a88f44d`): `docs/CONSUMER_REPO_LAYOUT.md` added with
  ASCII tree, per-section rationale, mid-life adoption guidance,
  and dogfood-heavy-projects extension. The current Go CLI does not expose
  the historical `init --with-striatum-layout` scaffold; use
  `workflow generate --scaffold-root ... --artifact-root ...` for new
  workflow trees.

**Suggested implementer:** any lane. Documentation phases are
single-track and additive — they touch docs and don't intersect
running workflow state. The Phase B work for RFC 0053 (schema /
prompt sweep) is its own dogfood; the workflow.json bump is a
breaking schema change and should land paired with a
`workflow upgrade` rule.

**Blocked on:** RFC 0053 Phase B requires a coordinated schema/runtime
migration RFC. RFC 0052 requires a bounded Phase A implementation RFC. The
other doc phases are closed for current scope.

### 5.9 Architecture remediation sequence (TODO 49-64)

This sequence comes from `reviews/external/STRIATUM_ARCHITECTURE_REMEDIATION_PLAN_2026-05-16.md`.
Production daemon fallback is now closed for mapped Python paths, but D107
changed the runway: Go is now the production/default daemon, and the local-state
cleanup path is complete for current scope.

Release order after Phase 0:

1. **TODO 49 / Phase 1:** done. Production daemon fallback is closed and the
   legacy local-state package/facades/fixtures are deleted.
2. **TODO 50 / Phase 2:** contract source plus Python/Go registry
   generation, generated MCP descriptors, generated docs tables, and
   declarative runtime CLI route translation landed.
3. **TODO 51 / Phase 3:** D105 decided Python-primary temporarily; D107 later
   superseded it.
4. **TODO 52 / Phase 4:** make the web service a daemon client rather
   than a parallel state-store peer.
5. **TODO 53 / Phase 5:** implement a real escalation inbox for the
   human principal.
6. **TODO 54 / Phase 6:** harden process supervision with PTY support,
   wrapper control acks, and reattach/lost-state handling.
7. **TODO 55 / Phase 7:** workflow risk lint, opt-in strict enforcement,
   web surfacing, generator preview surfacing, coverage scoring, and
   accepted-risk decision references landed; daemon-owned accepted-risk
   records landed for MCP/daemon clients, with CLI/UI polish completed in
   §4.11.
8. **TODO 56 / Phase 8:** auto-finalize daemon method, status/dashboard/web
   visibility, bounded sweep integration, skipped-candidate cause classes, the
   D125 evidence gate, and the D133 default-live cutover landed; tracked in
   §4.12.
9. **TODO 57 / Phase 9:** clean-build, bundle-size, and archive-size gates
   landed; chunking is monitor-only and tracked in §4.13.
10. **TODO 58 / Phase 10:** day-zero Postgres/daemon setup slice
    landed: role/grant repair, service helpers, guided adoption,
    first-run diagnostic report with Go binary provenance and daemon
    authority routing, and a dev-only compose profile.
11. **TODO 59 / Phase 11:** replay/archive foundations landed, including
    offline event-chain, row-hash, and archived row-id verification for
    command requests, process supervisors, process supervisor pointers,
    verdicts, blockers, process executions, and job worktrees; D126 accepts
    the Corpus Contract V2 identity, redaction, augmentation-reference,
    archive, and verification direction. The core reference-only packet
    augmentation surface has landed. Richer external-consumer fetch/UI UX is
    out of core until a later optional-augmentation decision accepts it.
12. **TODO 60 / Phase 12:** D127 sets the Git/PR boundary. The read-only
    local `git.snapshot` daemon/CLI slice, durable commit/PR request
    artifacts, and explicit-operator-confirmed local `git.commit_apply`
    have landed. Hosted provider actions are optional-plugin/out-of-core and
    require a later product decision.
13. **TODO 61 / RFC 0068:** done for the Go/Python cutover. Keep the Go
    production daemon conformance suite green, keep removed method names
    returning `method_unknown`, and keep the deleted Python daemon/Python MCP
    and legacy local-state implementation paths from reappearing.
14. **TODO 62 / RFC 0069:** done for current scope. Daemon-global surfaces
    moved to PostgreSQL/Go,
    including scheduler cursors, PostgreSQL-backed daemon MCP resources, and
    PostgreSQL-backed daemon lifecycle/health/audit/doctor reads. The
    dashboard-all run-progress slice now exposes phase progress,
    auto-finalize dry-run visibility, and supervisor-stall detail; the
    terminal dashboard now renders production text frames from daemon DTOs;
    Go `status` now matches the PostgreSQL/Python read-model shape.
    `dashboard --all` now routes through daemon RPC, and architecture tests
    assert production sources do not import `striatum.daemon` and that the
    retired module remains deleted. The
    direct PostgreSQL bootstrap/admin plane is now explicitly listed in the
    command authority matrix and guarded by an import scan. Daemon
    MCP resource fallback without a PostgreSQL connection is retired and fails
    closed before the legacy registry can open. Future registry-probe/global
    regressions are guardrail failures, not open RFC 0069 work.
15. **TODO 63 / RFC 0070:** done. Primitive daemon methods are the supported
    production path; removed composites stay out unless a future accepted
    product decision reintroduces PostgreSQL-native composites or sealed
    apply.
16. **TODO 64 / RFC 0071:** authority doctor and repository cutover report
    diagnostics landed. D108 keeps the command authority matrix curated while
    drift tests enforce generated route labels and runtime CLI fallback cells.
    `daemon doctor --repo <path> --authority --json` now mirrors the
    verify-only repository cutover report and summarizes repository cutover
    health in the authority report; no accepted RFC 0071 diagnostic slice is
    left unimplemented.
17. **TODO 65 / RFC 0058:** V1 and V1.5 landed. Use
    `docs/operator/BRIEF.md` as the current-state authority; `striatum
    operator current-brief` is the local read helper, and
    `operator_brief` context-budget overruns are schema errors. Operator-tree
    init/rotation is optional future work outside RFC 0058.

**Blocked on:** the prior Phase 7 accepted-risk authority, Phase 8 default
auto-finalize policy, Phase 11 Corpus V2, and Phase 12 Git/PR product
questions are decided by D124-D127. Remaining work is implementation and the
normal dogfood substrate mismatch recorded in dogfoods 064/065. The Go port
itself is unblocked and should proceed without waiting for human approval.

---

## 6. RFC follow-ups (cycle-exhaustion deltas)

These are codex `needs_revision` findings deferred via D095-D102 overrides.
Each is a list of file:line corrections that should land in a future
dogfood. Order them by impact, not by RFC number.

| TODO | RFC | Origin | Decision | Scope |
|---:|---|---|---|---|
| [27](todo.md) | RFC 0045 V1.5 | dogfood-043 | D097 | ✅ Completed: cycle phase-jump, Python/editor phase-field mismatch, explicit synthesis-job metadata validation, frontend drag-drop phase bypass, and invalid/unknown phase display tolerance have landed. |
| [28](todo.md) | RFC 0040 V1.6 | dogfood-044 | D098 | ✅ Completed: composite failure observability plus PostgreSQL artifact byline evidence landed; larger packet redesign requires a separate product decision. |
| [29](todo.md) | RFC 0038 V1.6 | dogfood-045 | D099 | ✅ Completed: real-bundle commit + supply-chain polish. **First `reject critical` override.** |
| [30](todo.md) | RFC 0039 V1.6 | dogfood-047 | D101 | ✅ Completed in 4.3 as helper groundwork; full Go daemon parity is reopened by D107 / RFC 0068. |
| [31](todo.md) | RFC 0043 V1.5 | dogfood-048 | D102 | ✅ Completed / tracker stale: crash-recovery tombstone two-phase, daemon-required default flip, `daemon migrate-repo-local` subparser wiring, focused `make test-rfc0043`, and a foreground-daemon refusal smoke have landed. **Distinct from D095-D101 — both reviewers had real findings, not co-blindness.** |
| (NEW) | RFC 0050 follow-up | dogfood-056 | (no override) | 5 reviewer findings filed as GH #9-13; 1 ergonomic from claude review. Already in active runway as 4.1 + 5.1. |

---

## 7. Blocked / waiting

Item F1 is no longer listed here: `examples/three-lane-design-build-review/`
is the runner-owned historical bootstrap successor, and
`tests/test_example_workflows.py` guards the fixture shape and references.

| Item | Blocker | Unblock criterion |
|---|---|---|
| RFC 0049 spike | Shelved by D106; closure rechecked RFC 0130/0075 and current Claude plan-credit docs. | Explicit operator-funded spike + measurement. |
| RFC 0052 Phase A | V0 proposal is not implementation-ready. | New bounded Phase A implementation RFC/design. |
| RFC 0053 schema/runtime rename | Breaking workflow/state rename needs coordinated migration. | New schema/runtime migration RFC with upgrade rule and compatibility policy. |
| RFC 0074 Phase C chooser UX | Phase B generator support has landed for `implementation_panel`; richer chooser cost/artifact-volume UX is still future work. | New bounded UI workflow or product decision. |
| Cross-Repo Live Scheduler V1 | Existing cross-repo work covers schema, metadata, capability gating, tests, reads, and cancel, not full live fan-out scheduling. | New bounded scheduler RFC. |
| Sealed apply/signing | `apply.reviewed_patch` remains removed/fail-closed. | New sealed-apply RFC/product decision. |
| Windows daemon support | Out of current POSIX-local product scope. | New Windows support RFC. |
| Local multi-operator tenancy | Out of current single-operator local product scope. | New tenancy RFC. |
| Engram-side RFC 0044 Phase 1 | External repo work; Striatum has no `import engram` or `memory.*` capability. | Not a Striatum TODO unless a future optional-augmentation decision changes the boundary. |
| Item 16 (generic language sweep) | Standing documentation hygiene. | Current sweep is clean; guardrail remains active. |

---

## 8. Resolved GitHub issue follow-ups

| # | Title | Closed by |
|---|---|---|
| [9](https://github.com/halbritt/striatum/issues/9) | CSRF on `/v1/invoke` — no Content-Type validation | RFC 0048 V1 Phase A / dogfood-057 security hardening. |
| [10](https://github.com/halbritt/striatum/issues/10) | Override modal trusts DOM `data-*` for job/session IDs | RFC 0048 V1 Phase A / dogfood-057 security hardening. |
| [11](https://github.com/halbritt/striatum/issues/11) | Recovery panel dry-run relies on CLI-side read-only guarantee | RFC 0048 V1 Phase A / dogfood-057 security hardening. |
| [12](https://github.com/halbritt/striatum/issues/12) | `copy-on-click` works on any `data-copy` — clipboard poisoning | RFC 0050 V2 ergonomics polish. |
| [13](https://github.com/halbritt/striatum/issues/13) | Workflow editor — `require_attested_lane` not purged on type change | RFC 0050 V2 ergonomics polish. |
| [14](https://github.com/halbritt/striatum/issues/14) | Recovery cannot clear terminal-run `process_exit_nonzero` blocker without lease | `docs/issues/14/` workflow with accepting review. |
| [15](https://github.com/halbritt/striatum/issues/15) | Clarify PostgreSQL transition guidance | `docs/issues/15/` workflow and transition-doc sweep. |
| [16](https://github.com/halbritt/striatum/issues/16) | Add complete operator initialization prompt | `b9add6f` via `docs/issues/16/` workflow. **First production use of the new GH-issue workflow type.** Verify verdict `accept` severity `info`. End-to-end 21 minutes wall-clock, zero operator-on-behalf publishes — empirically validated v1.48.1's wrapper auth fix. |
| [17](https://github.com/halbritt/striatum/issues/17) | Striatum doc consistency for Engram memory integration | `docs/issues/17/` workflow plus RFC 0057 Corpus Contract V2 scaffold; remaining V2 implementation is tracked under TODO 59. |
| [18](https://github.com/halbritt/striatum/issues/18) | Supervised lane stdin EOF hang for `cmd -` commands | Explicit `supervision.stdin_delivery: "one_shot_eof"` opt-in for pipe-transport lanes, with claim-next/send metadata and PG tests. |
| [20](https://github.com/halbritt/striatum/issues/20) | `supervise`: lane-stall timeouts and alarms should be in the runner | Runner-owned heartbeat/lease stall blockers, stalled liveness, and doctor/status surfacing. |

---

## 9. Cross-cutting operator concerns

### 9.1 CI health (v1.55.0)

CI's multi-repo harness step now hard-fails on missing Postgres rather than
silently skipping. CI runs the Go-only multi-repo harness on ubuntu-latest,
`make daemon-go-helper-check` on every matrix leg, and `make
daemon-go-conformance` on ubuntu-latest as the production daemon gate.
GitHub-hosted macOS runners don't support `services:`, so PostgreSQL-backed
multi-repo and Go-conformance steps remain Linux-only. Package/fresh-clone
smoke scripts run on every matrix leg but skip their daemon workflow when
PostgreSQL setup is unavailable instead of entering SQLite test-harness mode.

### 9.2 Test failures status (v1.55.0)

`make lint typecheck test` on `main`:

- `test_static_assets_no_external_urls` — **passes** (W3C namespace +
  reactflow.dev URIs are now whitelisted).
- `test_decision_log_rows_under_word_budget` — **passes** (D094 prose
  trimmed or budget raised; current rows fit).

The full Python test sweep on the local dev machine (with halbritt granted
CREATEDB + CREATEROLE on the local PG so ephemeral DB fixtures actually
run, and `striatum_daemon.schema_meta.substrate_version=6` applied) is
1254 passed / 7 skipped / 0 expected failures as of v1.55.0
post-burn-down (commits `f80b889` → `9fc02d6`).

### 9.3 Wrappers regenerate sometimes

`striatum skills install --profile all` (which every supervisor invocation
runs as its `lane.command` prefix) appears to occasionally regenerate the
wrapper scripts under `.striatum/bin/`. Those wrappers (and the whole
`.striatum/` tree) are operational scratch and are no longer tracked in git
(#199). The historical per-packet `claude --print` wrapper is RETIRED: the
supported path is the daemon-owned long-lived interactive PTY agent-loop
session (RFC 0088 / D148), and `workflow validate` / `run prepare` /
`supervise start` now REFUSE any `claude --print`/`-p` lane. `claude --print`
must not survive in any live wrapper — beyond breaking the agent-loop, after
the 2026-06-15 deadline it bills against API tokens (real money per packet)
instead of Claude plan usage. There is therefore no longer any reason to
`grep "claude --print" .striatum/bin/claude-supervised-wrapper.sh`; if such a
string appears, treat it as a defect to remove, not a flag to verify.

### 9.4 Memory items (operator-side)

Read these before driving a multi-step run:

- `~/.claude/projects/<encoded-striatum-repo>/memory/MEMORY.md`
  — operator lessons learned (dogfood-driven over free-form, autonomous
  run decisions, finalize-without-asking, OPERATOR_REPORT incrementality,
  claude-stall recovery, lane attestation gap, CI poll discipline).

---

## 10. How to kick off a new dogfood

For a fresh operator context. Assumes the target dogfood number is `<N>`
and the scope is one RFC phase or one self-contained fix.

```bash
# 0. Pre-flight
cd <striatum-repo>
git status                                 # main, clean
gh issue list --state open --label rfc-XXXX  # know what you're closing
cat docs/ROADMAP.md                        # this doc

# 1. Scaffold
mkdir -p docs/dogfood/<N>/{prompts,roles}
# Copy workflow.json from a recent similar dogfood (056 is the latest V1 + 3-way reviewer pattern)
cp docs/dogfood/056/workflow.json docs/dogfood/<N>/workflow.json
$EDITOR docs/dogfood/<N>/workflow.json     # update workflow_id, context_docs, objective, allowed_paths
# Write per-job prompts pointing at the concrete spec (RFC or REVIEW.md)
$EDITOR docs/dogfood/<N>/prompts/synth.md docs/dogfood/<N>/prompts/implement.md docs/dogfood/<N>/prompts/review_build.md
# Initial OPERATOR_REPORT.md scaffold
$EDITOR docs/dogfood/<N>/OPERATOR_REPORT.md

# 2. Validate + prepare + start
striatum workflow validate docs/dogfood/<N>/workflow.json --json
striatum run prepare --workflow docs/dogfood/<N>/workflow.json --json   # remember run_id
striatum run start --run-id <run_id> --json

# 3. Drive each job (per workflow job in dependency order)
striatum register-session --run-id <run_id> --role <R> --lane <L> --fresh --json
striatum supervise start --session-id <S> --json
striatum claim-next --session-id <S> --json    # may auto-fire under supervisor

# 4. Monitor
striatum why <run_id>     # tail events, see state, see blockers
striatum dashboard --run-id <run_id> --once     # compact frame

# 5. Per-job recovery if a lane stalls
#    Start with control-plane evidence:
striatum doctor --run-id <run_id> --verbose --json
striatum supervise status --session-id <S> --json
striatum why <job_or_blocker_id>
#    Wrapper logs are secondary evidence, not the stall detector.

# 6. Override needs_revision verdicts only after the fix-up ratifies (§3.2)

# 7. Ship
$EDITOR docs/dogfood/<N>/OPERATOR_REPORT.md           # final outcome + decisions
$EDITOR pyproject.toml CHANGELOG.md                   # bump minor or patch
git add -A docs/dogfood/<N>/ pyproject.toml CHANGELOG.md src/ tests/
git commit -m "vX.Y.Z: ..."
git tag vX.Y.Z
git push origin main
git push origin vX.Y.Z
git branch -d striatum/dogfood-<N>-...  || true       # if a branch was used
git push origin --delete striatum/dogfood-<N>-... 2>/dev/null || true

# 8. Update this doc
$EDITOR docs/ROADMAP.md                                # promote what's done, advance the queue
```

---

## 11. Where to look next

| If you want... | Read |
|---|---|
| Authoritative status of any item | `docs/TODO.md` |
| Architectural rationale for a decision | `docs/DECISION_LOG.md` (latest accepted rows) |
| RFC design + acceptance criteria | `docs/rfcs/<NNNN>-*.md` and `docs/rfcs/README.md` index |
| Per-dogfood outcomes + interventions | `docs/dogfood/<N>/OPERATOR_REPORT.md` |
| Agent MCP workflow control + CLI compatibility | `docs/HOW_TO_AGENT.md`, `docs/MCP.md`, `docs/SPEC.md` |
| Patterns that aren't in SPEC | §3 above, MEMORY.md |
| What's actively broken | §1, §9.1, §9.2 |
| What to do today | §4 (active runway) |
| Engram memory integration (external dependency) | `~/git/engram/STRIATUM_MEMORY_ROADMAP.md` and §5.7 above |

---

## 12. Promotion checklist (update this doc per release)

On every `vX.Y.0`:

- [ ] Move items from §4 to §2 if they shipped.
- [ ] Promote items from §5 to §4 if their blocker cleared.
- [ ] Recompute §7 (blocked) — what's still gated and on what.
- [ ] Add new GH issues to §8.
- [ ] Note any new anti-pattern instances in §3.5.
- [ ] Move §1 forward to the new commit/version/CI state.
