# RFC 0042: Run-List Workflow Identity and Graph Viewer Ergonomics

Status: implemented (re-scoped to the live Go SSE UI per D224, closes #400) — the original Phase A targeted the now-deleted Python UI; the run-list problem was still real (`go/pkg/webassets/templates/page.html` rendered only `run_id` + `branch_name`, no workflow identity). The Go SSE dashboard now surfaces a curated `workflow_name` on every run row: the `status` read folds `workflow_snapshots.workflow_id` (and `workflow_version` when present) onto each run, the selected-run card shows a `Workflow:` line, and the sidebar run list shows + filters on the workflow name. A clickable workflow link is not added (no stable per-workflow route exists today); the link affordance is noted as residual future work.
Date: 2026-05-12
Context:
[`RFC 0013`](0013-local-web-ui.md),
[`RFC 0022`](0022-web-ui-redesign.md),
[`RFC 0024`](0024-workflow-browser-and-builder.md),
[`RFC 0034`](0034-workflow-generator-and-template-catalog.md),
[`RFC 0037`](0037-web-ui-ergonomic-improvements.md),
[`RFC 0038`](0038-web-ui-feature-additions-and-frontend-toolchain.md),
`go/pkg/webassets/templates/page.html` (the live Go SSE run-list surface, renders `run_id` + `branch_name`)

## Problem

The `/` run-list page is the operator's primary triage surface. After RFC
0022 V1 (`run_list.html`) and RFC 0037 V1 (filter + duration column) it
shows the following columns:

`RUN | STATE | BRANCH | WORKFLOW | CREATED | DURATION`

In practice, three of these columns fail to identify *what work the run
did*:

- **`RUN`** is the opaque 32-character `run_id` (e.g.,
  `run_ea41c27b6fc34fa1a3a44e6f694caf96`). It is the system identifier,
  not a human handle. An operator scanning a list of recent runs cannot
  tell from this column what any given run was *for*.
- **`BRANCH`** is the durable Git branch name on `runs.branch_name`
  (e.g., `striatum/dogfood-041-rfc-0038-ui-features`). This is the most
  human-legible field today, but it has two failure modes: (a) once a
  branch is merged and deleted, the column still shows the gone-now
  branch with no affordance to follow it back to the work it represents;
  (b) for cross-repo runs (RFC 0032 territory) the branch is per-target
  repo, not per-run, so the column is structurally wrong rather than
  merely stale.
- **`WORKFLOW`** is intended to be the workflow identity. The template
  renders `run.workflow_id` (e.g., `dogfood-041-rfc-0038-ui-features`)
  when present. In the screenshots that triggered this RFC the cell
  collapsed to `—` because `_render_run_list_page` parses `workflow_id`
  out of the workflow snapshot's `workflow_json` blob rather than reading
  `workflow_snapshots.workflow_id` directly, and the JSON path was empty.
  Even when populated, `workflow_id` is the slug — not the
  human-readable name the workflow author wrote in `workflow.name`.

What an operator wants from this row of columns is:

1. A direct way to say "this run executed *workflow X*" using the same
   name the workflow author wrote — `name: "Dogfood 041: RFC 0038 web
   UI feature additions + Vite/React/TypeScript toolchain"`.
2. A way to *navigate* from that row to the directory the workflow's
   artifacts and scaffolding live in — both as a local link to the
   in-repo workflow detail page (already at `/workflows/<path>`) and,
   when the repo's origin is on GitHub, as an outbound link to the
   directory on GitHub so the operator can browse the produced
   artifacts after the local branch is gone.

The workflow snapshot already persists enough state to answer both
questions without a schema change:

- `workflow_snapshots.workflow_id` (column, `NOT NULL`).
- `workflow_snapshots.source_path` (column, the on-disk
  `workflow.json` path captured at snapshot time).
- `workflow_snapshots.workflow_json` — the full workflow body, which
  includes `name`, `scaffold_root`, and `artifact_root` (per RFC 0034
  V1's generated workflows).

The run-list query already joins `workflow_snapshots`; it just doesn't
project these fields out.

### Run-detail / workflow-detail graph viewer

The dependency graph shipped by RFC 0022 V1 (server-rendered SVG via
`src/striatum/web/graph_svg.py`) and tooltipped by RFC 0037 V1 has a
second class of usability gaps:

- **Default rendering is hard to read.** Node title font size is
  whatever the SVG inherits (browser default ~16 px scaled down by
  natural-size layout to roughly 11 px effective). At realistic
  dogfood workflow sizes (20+ jobs across 4–6 layers) the labels
  collapse to near-illegibility, and increasing browser zoom scales
  the entire page rather than the graph.
- **No pan affordance.** The viewer is a `<div class="graph-container">`
  with `overflow: auto`, so the only way to pan a wide or tall graph
  is to scroll the container — there is no click-drag pan, and the
  scrollbars compete with the page scroll.
- **No zoom affordance.** Because the SVG ships with both `viewBox`
  and explicit `width`/`height`, the graph renders at one fixed
  natural size. Operators have no way to zoom into a dense cluster
  of review jobs, and no way to zoom out to see the whole run on a
  laptop screen.
- **No fit-to-screen.** When a graph is wider than the container,
  the operator must scroll-hunt for the entry node or the failed
  job. There is no "show me the whole graph" button.
- **No keyboard control.** Operators using keyboard navigation have
  no way to scan node-to-node within the graph. RFC 0037 V1 added
  page-level `g r / g w / g c / g d` shortcuts but nothing
  graph-internal.
- **Long node titles overflow or truncate without affordance.**
  `workflow_job_id` strings like
  `implement_toolchain_codex_pre_review_codex` exceed the 180 px
  fixed `NODE_W` and either visually overflow or get the SVG-default
  truncation, with no way to read the full string short of clicking
  through to the job page.

These gaps compound: an operator looking at a 25-node dogfood run
typically sees a wall of small text and resorts to clicking the
specific job from the side rail rather than using the graph as a
navigation surface — defeating the purpose of rendering it.

## Goals

Run list:

- Make the run-list row uniquely answer "what work did this run do?"
  without forcing the operator to click into the detail page.
- Surface the workflow author's `name` as the human handle for each row.
- Provide a navigable link from the row to the workflow's directory:
  local first, GitHub second when available.
- Tolerate workflows authored before `name` / `scaffold_root` /
  `source_path` were populated — every fallback path is explicit and
  ordered.
- Tolerate runs where the workflow file on disk has moved or been
  deleted; the row still renders something the operator can recognize.

Graph viewer:

- Make the dependency graph a first-class navigation surface for runs
  of realistic size (20–60 jobs across 4–8 layers).
- Add click-drag pan and wheel / pinch zoom over the existing
  server-rendered SVG, preserving the click-to-navigate and hover-tooltip
  behavior shipped by RFC 0022 V1 / RFC 0037 V1.
- Add an explicit toolbar with zoom in / zoom out / fit-to-screen /
  reset-to-100% / help.
- Add keyboard shortcuts scoped to the graph (`+` / `−` / `0` / `f` /
  arrow-pan).
- Improve default node legibility: explicit title and meta font sizes;
  a small bump in node height; ellipsis-truncate long titles with the
  full string still available via hover tooltip.
- Apply the same affordances to both `/run/<id>` and
  `/workflows/<path>` graph embeds — they use the same renderer and
  the same container shape.

Cross-cutting:

- No schema migration. No new endpoint. No new runtime dependency.
- Server-rendered Jinja2 + vanilla JS, per D073 (and consistent with
  the RFC 0037 V1 ergonomic pass on the same surface). React Flow is
  not adopted for the viewer — only for the existing edit island.

## Non-Goals

Run list:

- A *redesign* of the run list. Column order, filter row (RFC 0037 V1),
  state pills, and duration column (RFC 0037 V1) stay.
- A *new* run-list endpoint. The existing `_render_run_list_page` SQL
  is extended; no `/v1/runs/*` change.
- A general-purpose Git host inference. V1 handles GitHub `origin`
  remotes only; GitLab / Bitbucket / Codeberg are deferred to a future
  RFC if asked for.
- A workflow-by-workflow rollup view ("show me all runs of *workflow
  X*"). That belongs in `/workflows/<path>` detail (already lists runs)
  or a future workflows-overview page.
- Cross-repo column adaptation for `runs.cross_repo_run_id`. The V1
  surface stays "one row per local run" and the cross-repo coordination
  surface (RFC 0032 territory) is out of scope.
- Removing the `BRANCH` column. It still carries information for runs
  whose branch has not been deleted; the operator UX is to *augment*
  identity, not to subtract.
- Mutating any artifact location, scaffold convention, or workflow
  schema. This RFC is read-only presentation polish over fields that
  already exist.

Graph viewer:

- Replacing the server-rendered SVG with a client-side graph library.
  The existing `graph_svg.py` renderer stays; the viewer wraps it.
- Adopting React Flow for the run-detail / workflow-detail viewer.
  The editor island (RFC 0038 V1) keeps React Flow; the viewer stays
  Jinja2 + vanilla JS so that read pages are not weighed down by a
  React payload.
- Adding a minimap. Deferred until a real complaint about navigating
  graphs larger than 60 nodes lands — at which point the natural
  affordance is to render a minimap; not before.
- Editing nodes / edges from the viewer. That stays the
  workflow-graph-editor island's job (RFC 0038 V1).
- Persisting per-user pan/zoom state across runs. The view always
  opens at fit-to-screen (graph viewer's natural framing).
- Re-laying-out the graph dynamically. The server's layered top-down
  layout (RFC 0024 V2.1) is unchanged.
- SVG export. Operators can already right-click → save image; an
  in-UI export button is a separate ask.

## External Prior Art

Run list:

- **GitHub Actions runs list** — each row's primary handle is the
  workflow name (`build.yml` → "Build and test"), with the commit
  message as secondary text and the branch shown as a chip. The opaque
  run number is demoted to a sub-line. We follow the same hierarchy:
  workflow `name` as the primary cell, `workflow_id` as the secondary
  line, run identifier behind the existing first-column link.
- **GitLab CI pipelines list** — each pipeline row links out to a
  "Pipelines" tab on the project; tertiary links jump to the source
  `.gitlab-ci.yml` on the default branch. The "↗ github" link in our
  V1 cell mirrors the GitLab "source" link affordance.
- **Argo Workflows UI** — workflow runs list shows `workflowTemplateRef`
  as the primary handle. The Argo pattern of separating "workflow"
  (the recipe) from "workflow run" (the execution) maps cleanly onto
  Striatum's `workflow_snapshots` vs. `runs` distinction and validates
  that the run row should foreground the recipe identity.

Graph viewer:

- **Argo Workflows graph** — drag-pan, wheel-zoom, fit-to-screen, and
  100% buttons in a small floating toolbar. Argo's interaction model
  is the canonical operator-graph affordance and is exactly what V1
  imitates.
- **GitHub Actions workflow visualizer** — pan/zoom over a graph
  rendered as styled SVG, with click-to-detail and hover. The
  comparison we explicitly avoid is GitHub's mini-map (deferred).
- **draw.io / Excalidraw** — viewBox-based pan/zoom on plain SVG. We
  adopt the viewBox-manipulation pattern (no node-level CSS transform)
  so click hit-testing and hover tooltips keep working without a
  coordinate-system rewrite.
- **D3 v6+ `d3-zoom`** — the reference implementation for SVG
  pan/zoom. We do not pull in D3 (too heavy for the read surface),
  but the math (cursor-anchored zoom; clamped scale; semantic pan)
  is the same.
- **Mermaid Live Editor** — keyboard shortcuts (`+` / `−` / `0`)
  that we reuse verbatim. Mermaid does not have a fit-to-screen, so
  we draw fit from Argo and inherit `0` (reset) from Mermaid.

## Proposal

### 1. Extend the run-list SQL projection

`src/striatum/service.py` → `_render_run_list_page`.

Today's SQL:

```sql
SELECT r.run_id, r.state, r.branch_name, r.created_at,
       r.started_at, r.completed_at, ws.workflow_json
FROM runs r
LEFT JOIN workflow_snapshots ws
  ON ws.workflow_snapshot_id = r.workflow_snapshot_id
ORDER BY r.created_at DESC
```

V1 projection:

```sql
SELECT r.run_id, r.state, r.branch_name, r.created_at,
       r.started_at, r.completed_at,
       ws.workflow_id   AS snapshot_workflow_id,
       ws.source_path   AS snapshot_source_path,
       ws.workflow_json
FROM runs r
LEFT JOIN workflow_snapshots ws
  ON ws.workflow_snapshot_id = r.workflow_snapshot_id
ORDER BY r.created_at DESC
```

For each row, derive:

- `workflow_name` — from `workflow_json.name`. Empty string if the JSON
  is absent / malformed / lacks the key.
- `workflow_id` — `snapshot_workflow_id` (the column, always present
  because `workflow_snapshots.workflow_id` is `NOT NULL`) with a
  defensive fallback to `workflow_json.workflow_id`.
- `scaffold_root` — `workflow_json.scaffold_root`, falling back to
  `workflow_json.artifact_root`. Empty string when neither is set.
- `workflow_local_url` — preferring `snapshot_source_path`; otherwise
  `<scaffold_root>/workflow.json` when `scaffold_root` is set. Yields
  `/workflows/<path>` (the RFC 0024 detail page route) or empty when
  no path can be derived. The route already serves a 404 envelope when
  the file is gone from disk, so the link is allowed to dangle.
- `workflow_github_url` — `<github_base>/tree/<default_branch>/<scaffold_root>`
  when the repo has a GitHub `origin` remote and `scaffold_root` is set;
  empty otherwise.

### 2. Repository identity helpers

`src/striatum/service.py`, attached to `ServiceState` and cached for
the life of the service process:

- `github_base_url() -> str | None` — returns
  `https://github.com/<owner>/<repo>` for the repo's `origin` remote,
  or `None`. Parses `https://github.com/...`, `git@github.com:...`,
  and `ssh://git@github.com/...` forms. Non-GitHub remotes return
  `None`.
- `default_branch() -> str` — `git symbolic-ref --short refs/remotes/origin/HEAD`,
  with `main` as the fallback when origin/HEAD is not set.

Both helpers shell out to `git` with a 2-second timeout and a single
`subprocess.run` per call; results are memoized on `ServiceState`
(`_UNSET` sentinel for cold cache). The Git invocation cost is
amortized to one-per-process-lifetime, paid the first time `/` is
rendered.

These helpers live on `ServiceState` (not in a new module) because they
are pure read-only convenience attached to the running service's repo
identity, alongside the existing `state.repo` and `state.allow_mutations`
properties.

### 3. Update the run-list template

`src/striatum/web/templates/run_list.html`.

Column order stays unchanged. The `WORKFLOW` cell becomes:

```jinja
<td class="workflow-cell">
  {% if run.workflow_name or run.workflow_id %}
    {% if run.workflow_local_url %}
      <a href="{{ run.workflow_local_url }}" class="workflow-name">
        {{ run.workflow_name or run.workflow_id }}
      </a>
    {% else %}
      <span class="workflow-name">{{ run.workflow_name or run.workflow_id }}</span>
    {% endif %}
    {% if run.workflow_id and run.workflow_name %}
      <div class="workflow-id-line"><code>{{ run.workflow_id }}</code></div>
    {% endif %}
    {% if run.workflow_github_url %}
      <a href="{{ run.workflow_github_url }}"
         class="workflow-github-link"
         target="_blank" rel="noopener"
         title="Open scaffold/artifact directory on GitHub">↗ github</a>
    {% endif %}
  {% else %}
    <span class="muted">—</span>
  {% endif %}
</td>
```

The `<tr>` gains `data-workflow-name="{{ run.workflow_name or '' }}"`
so the existing RFC 0037 V1 filter input matches against `name` as
well as `workflow_id` / branch / `run_id`.

### 4. Extend run-list filter haystack

`src/striatum/web/static/run_list.js`.

The `rowMatches(row, filter)` haystack already includes
`runId | branch | workflowId`. Add `workflowName` to the list so
free-text search picks up the human name. No other behavior changes
to the filter; existing state and date filters are independent.

### 5. CSS

`src/striatum/web/static/base.css` — small additions, scoped to the
`.workflow-cell` namespace:

- `.workflow-name { font-weight: 500; }`
- `.workflow-id-line { font-size: 0.85em; color: var(--fg-muted); }`
- `.workflow-github-link { font-size: 0.8em; color: var(--fg-muted); }`

No palette changes; uses existing tokens established in RFC 0022 V1
and RFC 0037 V1.

### 6. Server-rendered SVG legibility refinements

`src/striatum/web/graph_svg.py` + `src/striatum/web/static/base.css`.

The renderer produces a `<svg viewBox="0 0 W H" width="W" height="H">`
with rectangles per job and a `.graph-node-title` text inside each.
V1 makes three small server-side and CSS adjustments before any
interaction layer:

- **Explicit font sizing.** Set `.graph-node-title { font-size: 12px;
  font-weight: 500; }` and `.graph-node-meta { font-size: 10px; }`
  in `base.css`. The renderer already emits the two text classes;
  this RFC just pins their size.
- **Node height bump.** `NODE_H = 50` (was 44) gives the two-line
  layout breathing room.
- **Title truncation with full string on hover.** Truncate node
  titles to ~22 characters with a Unicode ellipsis when rendered.
  The full `workflow_job_id` continues to appear in the existing
  RFC 0037 V1 tooltip (which reads `data-job-id`); no information
  is lost.

These three changes alone improve legibility without any client
interaction. They are independent of sections 7–9 and could ship
first.

### 7. Pan / zoom / fit toolbar

`src/striatum/web/static/run_detail.js` (extend) +
`src/striatum/web/static/base.css` (new toolbar rules) + a small
template change to wrap the embedded SVG.

The current container shape:

```html
<div class="graph-container">
  {{ graph_svg | safe }}
</div>
```

V1 shape:

```html
<div class="graph-container" data-graph-viewer="on">
  <div class="graph-toolbar" role="toolbar" aria-label="Graph controls">
    <button type="button" data-graph-action="zoom-out" title="Zoom out (−)">−</button>
    <button type="button" data-graph-action="zoom-in"  title="Zoom in (+)">+</button>
    <button type="button" data-graph-action="fit"      title="Fit to screen (f)">Fit</button>
    <button type="button" data-graph-action="reset"    title="Reset to 100% (0)">100%</button>
    <button type="button" data-graph-action="help"     title="Keyboard shortcuts (?)" aria-haspopup="dialog">?</button>
  </div>
  <div class="graph-viewport" tabindex="0" aria-label="Workflow dependency graph viewport">
    {{ graph_svg | safe }}
  </div>
</div>
```

The viewer JS module (initialized on `DOMContentLoaded`, idempotent,
opt-out via `data-graph-viewer="off"`) does the following:

- **Reads natural extent.** The embedded SVG ships with both
  `viewBox="0 0 W H"` and matching `width`/`height` attributes.
  The viewer captures `(W, H)` once at init and removes the explicit
  `width`/`height` so the SVG scales to fill the viewport.
- **Drag-to-pan.** On `pointerdown` over `.graph-viewport` (not over
  a `.graph-node-link` so click-navigation keeps working): captures
  the pointer, tracks deltas, and translates them through the
  current viewBox scale to update `viewBox` minX / minY.
- **Wheel zoom.** `wheel` listener (passive: false) zooms around the
  cursor anchor: compute the SVG-space cursor coordinate before the
  scale change, apply the scale, then offset the viewBox so the same
  SVG point still sits under the cursor. Pinch (the touchpad
  convention of `wheel + ctrlKey`) feeds the same path. Scale is
  clamped to `[0.25×, 4×]` of the natural extent.
- **Toolbar buttons.** `+` / `−` step zoom by `1.2×`/`1/1.2` around
  the viewport center; `Fit` resets viewBox to `(0, 0, W, H)`
  letterboxed into the viewport aspect ratio; `100%` snaps back to
  the natural SVG size centered in the viewport; `?` opens a
  `<dialog>` listing the keyboard shortcuts (reusing the RFC 0037
  V1 `?` overlay pattern).
- **Keyboard.** When `.graph-viewport` has focus: `+` / `=` zoom in;
  `−` / `_` zoom out; `0` reset; `f` fit; arrow keys pan by 1/10 of
  the current viewBox width/height. Each shortcut is no-op when the
  active element is an input / textarea / contenteditable.
- **Initial framing.** On first load, the viewer applies `Fit` so the
  whole graph is visible by default rather than overflowing the
  container. Operators who want the natural-pixel rendering hit
  `100%` (or pan/zoom from the fit framing).
- **Tooltip + click compatibility.** Pan/zoom modify only the SVG
  viewBox, not any element transforms; the RFC 0037 V1 hover
  tooltip layer (`run_detail.js` existing code) keeps working
  because hit-testing is unchanged. Click navigation through the
  `<a class="graph-node-link">` wrappers (`graph_svg.py`) continues
  to work for the same reason. Pan must distinguish itself from a
  click via a small movement threshold (e.g., 4 px) — only pans
  beyond the threshold suppress the synthesized click.

The implementation is one vanilla JS module (~150 lines), imported
by `run_detail.html` and `workflow_detail.html` script blocks. No
new dependency; no React; no SPA conversion.

### 8. Container layout updates

`src/striatum/web/static/base.css`.

The existing `.graph-container` is a scrolling box. V1 changes it to
a fixed-height, position-relative viewport so the toolbar can overlay:

- `.graph-container` — fixed `height: 60vh` (`min-height: 400px`),
  `position: relative`, `overflow: hidden`.
- `.graph-toolbar` — absolutely positioned `top: var(--space-2);
  right: var(--space-2);` inside the container, with a soft
  background and small button row using existing
  `.compact-button` styling.
- `.graph-viewport` — fills the container, takes the new pan/zoom
  behavior, focusable via `tabindex="0"` with a visible focus ring.
- `.graph-viewport > svg` — `width: 100%; height: 100%;` (the
  natural `width`/`height` attributes are stripped by JS at init).

The 60vh cap is large enough to hold realistic graphs at fit-to-
screen on a laptop, and small enough to leave room for the
"Next actions" panel (RFC 0037 V1) above and the jobs rail beside
it. Dark mode inherits from the existing `prefers-color-scheme: dark`
blocks; the toolbar buttons use the existing `.secondary-button`
tokens.

### 9. Server-side accessibility annotations

`src/striatum/web/graph_svg.py`.

- The root `<svg>` already has `role="img"` and an `aria-label`. V1
  adds `aria-roledescription="dependency graph"` to clarify the
  interaction model.
- Each `<g class="graph-node">` gains an `aria-label` of the form
  `"<state>: <full title>"` so screen readers announce node state
  alongside the title rather than reading only the truncated text.
- The toolbar buttons (client-side template) carry their own
  `title`/`aria-label`.

No structural change to the SVG; no new test fixtures needed beyond
asserting the new attributes exist.

### 10. No changes to

- The `runs` / `workflow_snapshots` schema (RFC 0006 territory).
- The `/v1/runs` API surface (RFC 0012 territory).
- The `BRANCH` column. Per the user note that triggered this RFC, the
  branch *might be gone* post-merge, but the column still informs
  triage for live and recently-terminal runs.
- The `RUN` column. The opaque `run_id` stays as the durable link
  target for the detail page; it is still the right value for
  copy-paste into CLI verbs (`striatum run summary --run-id ...`).
- The `STATE`, `CREATED`, `DURATION` columns.
- The chat surface or workflow visual builder.
- The `run_detail.html` jobs rail, next-actions panel (RFC 0037 V1),
  or artifact list.
- The `workflow_graph_editor` React Flow island (RFC 0038 V1) —
  pan/zoom is handled there by React Flow already.
- The server's layered top-down layout (RFC 0024 V2.1).
- The MCP surface or audit chain.

## Acceptance Criteria

Run list:

- For runs whose snapshot has a populated `workflow.name`, the
  `WORKFLOW` column shows the name as the primary, link-styled text.
- The cell links to `/workflows/<source_path>` when `source_path` is
  recorded, else `/workflows/<scaffold_root>/workflow.json`, else
  unlinked text. The fallback chain is documented in code comments.
- When the repo's `origin` is a GitHub remote and the snapshot has a
  scaffold or artifact root, a small `↗ github` outbound link points
  to `<github_base>/tree/<default_branch>/<scaffold_root>` on a new
  tab.
- When `origin` is not GitHub or no `scaffold_root` is recorded, no
  GitHub link renders — there is no broken or guessed link.
- When neither name nor id is available (truly empty workflow_json
  on an older run), the cell renders `—` with the existing muted
  style.
- The run-list filter input matches workflow `name` substrings in
  addition to the existing `run_id | branch | workflow_id` haystack.
- No regressions to RFC 0037 V1 behavior (state pills, duration
  column, date filter, localtime toggle).
- `_render_run_list_page` performs no extra Git invocations per row
  — `github_base_url()` and `default_branch()` are memoized on
  `ServiceState`.
- A targeted unit test asserts the GitHub URL parser handles the
  three common origin shapes and rejects non-GitHub hosts.
- A targeted integration test asserts that a workflow with
  `name + scaffold_root` renders both the local and GitHub link;
  that a workflow with `workflow_id` only renders just the id; and
  that an empty-snapshot run renders `—`.

Graph viewer:

- The `/run/<id>` and `/workflows/<path>` pages render the
  dependency graph inside a fixed-height (60vh / min 400px)
  container with a visible toolbar overlay.
- Click-drag inside the graph viewport pans the graph; mouse wheel
  zooms around the cursor anchor; ctrl-wheel (pinch on touchpads)
  zooms the same way. Scale is clamped to `[0.25×, 4×]` of natural
  size.
- The toolbar exposes zoom-in / zoom-out / fit-to-screen /
  reset-to-100% / help (`?`).
- When the viewport has focus, `+` / `=` zoom in; `−` / `_` zoom out;
  `0` resets; `f` fits; arrow keys pan. None of these intercept
  when the active element is an input / textarea / contenteditable.
- The graph opens at fit-to-screen on first load and stays at the
  operator's chosen zoom for the rest of the page lifetime.
- Click navigation on a node still routes to `/run/<id>/job/<jid>`
  (RFC 0022 V1 behavior) — only pans beyond the 4 px threshold
  suppress the click.
- Hover tooltips (RFC 0037 V1) continue to fire and position
  correctly under pan/zoom.
- Node titles render at 12 px and meta at 10 px with `NODE_H = 50`.
  Long titles truncate with an ellipsis; the full title is in the
  tooltip data attributes.
- Each `<g class="graph-node">` carries an `aria-label` with state
  and full title.
- A targeted unit test asserts the viewer's cursor-anchored zoom
  math (given a viewBox, cursor coordinates, and a scale factor,
  the resulting viewBox keeps the cursor on the same SVG point).
- A targeted integration test asserts that:
  - Initial render applies fit-to-screen.
  - A click on a node link still navigates (no pan triggered by a
    sub-threshold movement).
  - `data-graph-viewer="off"` skips initialization.
- No regression in click-to-detail or hover-tooltip behavior.

Cross-cutting:

- No schema change.
- No new runtime dependency (no `d3-zoom`, no `svg-pan-zoom`, no
  React Flow on the viewer surface).
- `make lint`, `make typecheck`, `make test`, `make smoke` pass.
- README / CHANGELOG / `docs/UBIQUITOUS_LANGUAGE.md` updated if a
  vocabulary entry for the WORKFLOW cell or the graph viewer is
  warranted. `docs/HOW_TO_HUMAN.md` gains a short subsection on the
  graph viewer's keyboard shortcuts, alongside the RFC 0037 V1
  page-level shortcuts.

## Implementation Plan

The two halves of this RFC are independent and can land separately.
Run-list goes first because it touches fewer files and unblocks a
visible operator pain immediately.

### Phase A — Run-list workflow identity

1. **SQL + dict shape.** Extend `_render_run_list_page` to project the
   two extra columns from `workflow_snapshots` and to build the
   `workflow_name | workflow_id | workflow_local_url |
   workflow_github_url` dict per run. Unit test the derivation given
   a stubbed row.
2. **Repo identity helpers.** Add `_parse_github_remote`,
   `_git_config_get`, `_git_symbolic_ref`, and the cached
   `ServiceState.github_base_url() / default_branch()` methods. Unit
   test the parser. Integration test ensures `default_branch` returns
   `main` when the repo has no `origin/HEAD`.
3. **Template.** Update `run_list.html` `WORKFLOW` cell + add
   `data-workflow-name` to the row.
4. **Filter.** Extend the JS haystack to include `workflowName`.
5. **CSS.** Add the three `.workflow-cell` rules.

### Phase B — Graph viewer

6. **Server-side legibility.** Bump `NODE_H` to 50 in
   `graph_svg.py`; add the ellipsis-truncation for long titles
   (preserving full string in existing data attributes); add
   `aria-label` + `aria-roledescription`. Update graph-svg unit
   tests for the new attributes and truncation.
7. **CSS.** Pin `.graph-node-title` to 12 px and `.graph-node-meta`
   to 10 px. Reshape `.graph-container` to a fixed-height
   relative-positioned viewport; add `.graph-toolbar` and
   `.graph-viewport` rules + dark-mode parity.
8. **Viewer JS.** Add `src/striatum/web/static/graph_viewer.js`
   (~150 lines): viewBox pan/zoom, toolbar wiring, keyboard
   shortcuts, the 4 px click vs. pan threshold, initial fit-to-
   screen framing. Idempotent init from
   `run_detail.js` and a small script tag on `workflow_detail.html`.
9. **Template wiring.** Wrap the `{{ graph_svg | safe }}` block in
   `.graph-toolbar + .graph-viewport` on both
   `run_detail.html` and `workflow_detail.html`.
10. **Tests.** Add unit tests for the cursor-anchored zoom math
    (pure-function, JS-side). Add an integration test for click vs.
    pan threshold and for `data-graph-viewer="off"` opt-out.

### Phase C — Docs

11. Update `docs/UBIQUITOUS_LANGUAGE.md` only if the WORKFLOW cell
    semantics or the graph viewer warrants a glossary entry.
12. Update `docs/HOW_TO_HUMAN.md` with the graph viewer keyboard
    shortcut table and a one-paragraph note on the new run-list
    `↗ github` affordance.
13. Update README if a screenshot/example references the old
    single-line workflow cell or the old static graph.

## Open Questions

- **Should the `BRANCH` column move or shrink?** Argument for: with a
  good `WORKFLOW` cell, `BRANCH` is secondary. Argument against: the
  branch is still load-bearing for active runs and for navigating
  back into Git. Recommendation: V1 leaves the column unchanged; a
  future RFC may demote it to a smaller chip if dogfood data shows
  operators don't read it.
- **Should the GitHub link target the run's branch instead of the
  default branch?** Argument for: the artifacts the operator wants
  to read may exist only on the run's branch. Argument against: the
  branch may be deleted (the original motivation); a `tree/<branch>`
  URL on a deleted branch returns 404. Recommendation: V1 uses the
  default branch, accepting that mid-flight branch-specific browsing
  is a click into the run detail page.
- **Should the helper detect GitLab / Bitbucket / Codeberg origins?**
  Argument for: not every operator host is GitHub. Argument against:
  the parser surface multiplies and the URL paths differ per host
  (GitLab `-/tree/`, Bitbucket `src/`). Recommendation: V1 ships
  GitHub only; revisit if a non-GitHub operator asks. The helper is
  small enough to extend.
- **Should `_render_view_path` learn to serve directory views so the
  local link can target the directory rather than the workflow.json
  file?** Argument for: parity with the GitHub link's
  directory-listing affordance. Argument against: a directory view
  is a separable feature, and `/workflows/<path>` already gives the
  operator a rich workflow detail page (graph, jobs, runs). Out of
  scope for this RFC; track as a follow-up if asked.
- **Should the GitHub URL be stored on the run record at run-prepare
  time rather than derived per page render?** Argument for: avoids
  any per-render Git invocation. Argument against: would couple
  presentation to schema and require a migration; current
  invocations are memoized to one-per-process-lifetime. V1 stays
  derive-on-render with the cache.
- **Should the new helper module live somewhere other than
  `service.py`?** Argument for: `service.py` is large. Argument
  against: the helper is small and `ServiceState` already owns
  repo-shaped state. V1 keeps it in `service.py`; if the file
  continues to grow, a `service_helpers.py` split is a separate
  refactor RFC.

Graph viewer:

- **Why not adopt React Flow for the viewer to match the editor
  island?** Argument for: visual and behavioral consistency across
  the two graph surfaces; pan/zoom/minimap for free. Argument
  against: the editor island already costs ~150 KB of bundled JS
  (RFC 0038 V1); the viewer is rendered on every run-detail page
  load and adopting React Flow would put that payload on the hot
  read path. V1 stays vanilla JS over the server SVG; revisit if
  the operator UX gap between viewer and editor becomes a complaint.
- **Should the initial framing be 100% (natural size) or fit-to-
  screen?** Argument for 100%: predictable, identical to current
  behavior. Argument for fit-to-screen: the whole graph is visible
  immediately, which is the operator's most common first need.
  Recommendation: fit-to-screen. Operators who want the natural
  rendering have a labeled `100%` button.
- **Should the viewer persist pan/zoom in `localStorage` per run?**
  Argument for: returning to a long-running graph keeps the
  operator's framing. Argument against: storage churn, and the
  "right" framing is usually the current run state's hot region,
  which the operator wants to re-fit anyway. V1 does not persist.
- **Should we add a minimap?** Argument for: helps on 60+ node
  graphs. Argument against: pure-CSS implementation is awkward
  without a library, and operator complaints so far cap at "I
  can't read the labels and I can't pan", not "I'm lost in the
  graph". Defer until a real lost-in-the-graph complaint lands.
- **Should the click-vs-pan threshold be configurable?** No. 4 px
  is the OS-standard drag threshold and the user expectation
  matches. If a hand-tremor accessibility case shows up the
  threshold becomes a future RFC.
- **Should `Fit` reframe to a subgraph (e.g., only running +
  blocked jobs)?** Tempting. Out of scope for V1 because it
  changes the meaning of the button. A future ergonomic-V2 RFC
  could add a "focus on running/blocked" toolbar entry instead of
  overloading Fit.
- **Should the toolbar appear inside the SVG or in a sibling DOM
  element?** Sibling DOM, per section 7. Inside-the-SVG buttons
  would scale with the viewBox (or require counter-scale math) and
  fight the focus-ring affordance. The sibling pattern is what
  Argo and draw.io use.
- **Should we ship a one-pixel "you are not at 100%" affordance
  on the toolbar?** Argument for: makes the zoom state legible.
  Argument against: extra UI for a state most operators won't
  read. Recommendation: V1 adds a small zoom-percent readout
  inside the toolbar (e.g., `45%`) — cheap and self-documenting.

## Domain Modeling

This RFC introduces no new domain concepts. The run, workflow
snapshot, and workflow aggregates from D006 / RFC 0034 V1 are
unchanged. The new rendered fields (`workflow_name`,
`workflow_local_url`, `workflow_github_url`) are derived presentation
values over `workflow_snapshots.workflow_json`, not first-class
domain attributes — they have no persistence and no canonical schema
position.

The graph viewer half is similarly a presentation refinement: the
server-rendered SVG (and the underlying `workflow_graph_data` from
RFC 0007 V1) remains canonical; the pan/zoom layer is a viewBox
transform applied client-side with no persistence, no event
emission, and no impact on the run / job / artifact aggregates.

The change is a presentation refinement of the existing run-list
WORKFLOW column and the existing graph surface, in keeping with the
RFC 0037 V1 ergonomic-polish pattern.
