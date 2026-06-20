# RFC 0157: Canonical run/job state projection across operator read surfaces

Status: proposed (revised 2026-06-20; option pinned; ratification pending)
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
  **no top-level run `state` key at all**. `docs/reference/spec.md` §Dashboard
  (line ~1789) states the dashboard "shows run state and branch," but that prose
  describes the live **TUI render** (which refreshes every 2 seconds), not the
  `dashboard --once` JSON; the concrete gap is that the `--once` JSON a script
  consumes lacks the top-level `state` parity the TUI prose implies.
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
- The dashboard's missing top-level run `state` is the closest thing to a
  concrete doc-vs-source disagreement, but the spec prose it brushes against
  (line ~1789) describes the TUI render, not the `--once` JSON — so the gap is
  that the `--once` JSON lacks the top-level `state` a script reasonably expects
  for parity with the rendered view. The rest is shape divergence across verbs,
  each shape internally consistent with its own contract.
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

## Alternatives considered

1. **Additive shared `state_projection` block on all three verbs**
   (`{run_state, jobs:[{id,state}]}`). Most directly satisfies #481. The open
   sub-questions it raises — field name, whether `jobs[]` carries
   `attempt`/`role_id`, behavior when `run_id` is omitted and the verb is
   repo-wide — are now answered by this revision (see Resolution). **Pinned.**
2. **Dashboard-only doc/field fix** — add the top-level run `state` the spec
   TUI prose implies to the `dashboard --once` JSON. A genuinely narrow,
   defensible sub-fix (closes the one doc-vs-source gap), but on its own it only
   partially addresses #481's three-way shape disagreement and still picks a
   field name in isolation. **Folded into Alternative 1** so the shape is chosen
   once: the canonical `state_projection.run_state` subsumes the standalone
   dashboard `state` field, and dashboard also gains a top-level `state`
   mirroring `state_projection.run_state` to satisfy the TUI-parity expectation
   without a second, divergent field name.
3. **Document the canonical query only** (no code change) — note in
   `docs/reference/cli-reference.md` that `.run.state` (summary) is the
   authoritative single-run state read. Cheapest, but `run.summary` hard-refuses
   without a `run_id` (single-run only), so a document-only path leaves the
   repo-wide "where are my runs" consumer (`status`/`dashboard`) with no
   canonical answer — exactly the AFK-monitor case that motivated #481. Leaves
   the divergence in place. **Rejected.**
4. **wontfix** — declare the divergence intentional (each verb optimized for
   its role). A viable product stance, but the projection is additive and the
   existing per-verb shapes are preserved unchanged, so it costs nothing already
   consumed while removing real AFK-driving friction. **Rejected** in favor of
   the additive projection.

## Resolution (revised 2026-06-20; ratification pending)

This revision resolves the SERIOUS RFC_REVIEW finding (an accepted RFC must
name one disposition, not a menu) by pinning a single additive contract.
**Disposition: Alternative 1 with Alternative 2 folded in** — add one shared,
strictly additive `state_projection` block to all three verbs, and give
`dashboard --once` a top-level `state` mirroring it.

Pinned contract:

- **Field name:** `state_projection` (a top-level object on each verb's result
  map).
- **Shape:** `{ "run_state": <string|null>, "jobs": [ { "id": <string>,
  "state": <string> }, ... ] }`.
- **`jobs[]` minimal shape:** `id` and `state` **only**. It deliberately does
  **not** carry `attempt` or `role_id` — those are `run.summary`-specific detail
  and remain available on the existing `run.summary` `.jobs[]` list. The
  canonical projection is the lowest-common-denominator pair (`id`, `state`)
  every consumer needs.
- **`run_id`-omitted / repo-wide behavior:** when the verb is invoked without a
  single `run_id` (i.e. the repo-wide `status` / `dashboard` window), `run_state`
  is `null` and the `jobs` array is empty (or the `state_projection` block may be
  omitted entirely on those calls). The projection is meaningful only for a
  single-run scope; it does not attempt to flatten a multi-run window into one
  `run_state`.
- **Strictly additive — no breaking change.** Every existing key is untouched:
  `run.summary` keeps `.run.state` and its full `.jobs[]` list (with `attempt`,
  `role_id`); `dashboard` keeps `.jobs_by_state` and gains both
  `state_projection` and a top-level `state` (mirroring
  `state_projection.run_state`); `status` keeps `.runs[].state` and `.jobs{}`.
  Existing AFK scripts and CI assertions on the current shapes do not break;
  the new field is purely added surface. Per RFC 0030 §95 an additive result
  field requires no `schema_version` bump.

The field name and `jobs[]` shape are now part of the contract this RFC asks to
ratify; renaming or reshaping them after ratification is the breaking change the
RFC's blast-radius reasoning warns against, which is why they are pinned here
rather than left to the implementer.

## Handoff to RFC_REVIEW

This RFC remains a docs decision; it implements **no** projection, field, or
contract change. Ratification (next free decision number) records the pinned
disposition above. On ratification, file an implementation issue whose DoD is:

- add the additive `state_projection` block (and the dashboard top-level
  `state`) to `HandleRunSummary` / `HandleDashboard` / `HandleStatus`;
- a contract/golden-shape test extending the reads package's SQL-dispatch
  fake-runner tests (e.g. `dashboard_test.go`) that asserts the pinned shape on
  all three verbs, including the `run_id`-omitted `null`/empty case;
- update `docs/reference/spec.md` §Dashboard / §Run Summary and
  `docs/reference/cli-reference.md` to describe the ratified `state_projection`
  shape.

If a maintainer instead prefers `wontfix` (Alternative 4), record that as the
ratifying decision and close #481 `wontfix`; both are legitimate, but the RFC is
no longer left listing open options.
