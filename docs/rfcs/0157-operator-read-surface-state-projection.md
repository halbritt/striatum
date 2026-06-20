# RFC 0157: Canonical run/job state projection across operator read surfaces

Status: proposed
Date: 2026-06-20
author: author-claude-opus-4.8-001
Affected issue: GH #481 (enhancement, needs-triage)
Context: `go/pkg/reads/exports.go` (`HandleRunSummary`),
`go/pkg/reads/dashboard.go` (`HandleDashboard`),
`go/pkg/reads/status.go` (`HandleStatus`, `statusJobCounts`, `statusRuns`);
`docs/reference/spec.md` §Dashboard / §Run Summary; RFC 0042 (run-list
workflow identity), RFC 0030 (daemon RPC server + version-skew protocol).

## Problem (evidence, anchored to origin/main @ d5d3cd86)

The three primary operator read surfaces for "where is this run and its jobs"
return the same underlying run/job state in three structurally different
shapes, so a script or AFK agent must special-case each verb to extract a
plain `(run_state, per-job states)` answer. This was surfaced by an AI
operator monitoring a live design run on striatum 2.34.1 (#481).

The divergence is structural, not cosmetic:

- **`run.summary`** (`run summary --json`) — `go/pkg/reads/exports.go`
  `HandleRunSummary`, result map keys `"run"` and `"jobs"` (lines 146-157):
  run state is nested at `.run.state` (a single run object); jobs are a
  **list** of objects `[{workflow_job_id, job_id, role_id, state, attempt}]`
  selected at lines 67-73.
- **`dashboard`** (`dashboard --once`) — `go/pkg/reads/dashboard.go`
  `HandleDashboard`, result map (lines 270-278): exposes only `run_id` plus
  `jobs_by_state`, a **map** of `state -> count` (built lines 51-78). There is
  **no top-level run `state` key at all** — even though `docs/reference/spec.md`
  §Dashboard (line ~1789) states the dashboard "shows run state and branch."
- **`status`** (`status --json`) — `go/pkg/reads/status.go` `HandleStatus`,
  result map (lines 118-135): run state lives inside `.runs[]` (a **list** of
  run objects each carrying `.state`, via `statusRuns`, lines 252-316), and
  `jobs` is a **map** of `state -> count` (via `statusJobCounts`, lines
  383-406) — i.e. neither a `.run.state` scalar nor a per-job list.

So the same two facts (run state; per-job state) are reachable as:
`.run.state` + `.jobs[].state` (summary); *absent* + `.jobs_by_state{}`
(dashboard); `.runs[].state` + `.jobs{}` (status). An operator must probe
field-by-field per verb. The issue is filed as DX-only / legibility, but
read-surface consistency is load-bearing for unattended (AFK) driving.

## Claim boundaries

- This is a presentation/consistency gap, **not** a correctness bug: each
  verb returns accurate data for its own contract. No run or job is
  mis-stated; nothing is wedged.
- The dashboard's missing top-level run `state` is the single concrete
  doc-vs-source disagreement (spec prose promises "run state"); the rest is
  shape divergence across verbs that each shape is internally consistent with.
- No accepted decision in `docs/decisions/decision-log.md` pins a canonical
  cross-surface read shape, so there is no existing invariant a direct FIX
  could "strictly narrow to."

## Why a direct FIX was rejected

The issue's own suggested remedy — "offer one consistent state projection
across these verbs (e.g. a shared `{run_state, jobs:[{id,state}]}` block)" —
requires **introducing a new, additive cross-surface read contract** and
deciding its canonical shape. That is a ratification decision, not a
behavior-narrowing fix:

- A FIX is only permitted here if it strictly narrows behavior to a cited
  accepted invariant, expands no contract, and has a failing proof. Adding a
  shared `state_projection` block **expands** the read contract (a new field
  AFK agents and external scripts will hard-depend on); it does not narrow
  anything. The narrowing exception does not apply.
- The three surfaces serve deliberately different roles (summary = full
  per-job detail; dashboard = TUI-oriented counts; status = repo-wide,
  multi-run window). Picking one canonical `(run_state, jobs[])` shape and
  retrofitting it onto all three is a design choice with downstream
  dependents, not a local presentation tweak.
- The issue author explicitly invites `wontfix` "if the divergence is
  intentional" — a maintainer product judgment that a worker must not
  unilaterally resolve by shipping a new contract.

## Blast-radius dims that forced RFC

- **public_api** — the JSON shapes of `run.summary` / `dashboard` / `status`
  are the de-facto operator-facing read API consumed by CLI renderers, AFK
  agents, and CI assertions. A new shared projection field changes that
  surface for every consumer.
- **cross_team_contract** — #481 itself frames read-surface consistency as
  "load-bearing for AFK driving"; the projection becomes a contract other
  agents/scripts depend on, and renaming/reshaping it later is a breaking
  change.

(Not hot: no persisted schema change, no migration, no security/authz change,
no wire-version bump beyond an additive field, no product-safety claim.)

## Alternatives / rejected direct patches

1. **Additive shared `state_projection` block on all three verbs**
   (`{run_state, jobs:[{id,state}]}`). Most directly satisfies #481, but is
   precisely the new contract that needs ratification (field name, whether
   `jobs[]` carries `attempt`/`role_id`, behavior when `run_id` is omitted and
   the verb is repo-wide). Defer the field design to this RFC's acceptance.
2. **Dashboard-only doc/field fix** — add the top-level run `state` the spec
   prose already promises to the `dashboard --once` JSON. This is a genuinely
   narrow, defensible sub-fix (closes the one doc-vs-source gap), but it only
   partially addresses #481's three-way shape disagreement and still picks a
   field name in isolation; better folded into the canonical decision so the
   shape is chosen once.
3. **Document the canonical query only** (no code change) — note in
   `docs/reference/cli-reference.md` that `.run.state` (summary) is the
   authoritative single-run state read. Cheapest, but leaves the divergence in
   place and does not help scripts that consume `dashboard`/`status`.
4. **wontfix** — declare the divergence intentional (each verb optimized for
   its role). Viable product stance; requires a maintainer decision, which is
   the point of this RFC.

## Handoff to RFC_REVIEW

This stub does **not** implement any projection, field, or contract change.
For review: decide (a) FIX-via-additive-projection vs document-canonical vs
wontfix; (b) if additive, the exact shared shape and which verbs carry it;
(c) whether to land the narrow dashboard `state` doc-vs-source fix
independently of the broader decision. On acceptance, file an implementation
issue with a contract/snapshot test (the reads package already has SQL-dispatch
fake-runner tests, e.g. `dashboard_test.go`, that a golden-shape assertion can
extend) and update `docs/reference/spec.md` §Dashboard / §Run Summary plus
`docs/reference/cli-reference.md` to describe the ratified shape.
