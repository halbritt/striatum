# RFC 0050 — Operator UI rework and provenance honesty

**Status:** accepted / implemented across v1.46.0-v1.48.0
**Scope:** historical UI rework plan; active service cleanup is tracked by
RFC 0061 / TODO 52
**Canonical design:** [`docs/design/UI_REWORK.md`](../design/UI_REWORK.md)
**Closes (partially):** `../records/_frozen/research/CLAUDE_DESIGN_UI_REWORK_PROMPT.md` deliverable; §8.10 deferred items

## Background

`docs/design/UI_REWORK.md` is a 1845-line implementation-ready
handoff produced by Claude Design from
`../records/_frozen/research/CLAUDE_DESIGN_UI_REWORK_PROMPT.md`. It covers the local web UI
(`striatum serve --web`) and the terminal dashboard
(`striatum dashboard`) end-to-end: design intent, six primary
operator flows, screen specifications for every template,
component inventory with closed enums, truthfulness rules
distinguishing attested / unattested / operator / superseded
verdicts, visual system tokens, a complete file map, and
acceptance checks per surface.

This RFC adopts that document as the canonical spec rather than
re-deriving design via dogfood designers. The dogfood ceremony
for V1/V1.5/V2 starts at synthesis (which simply summarises the
phase scope) and skips the three designer + design-review jobs.

## Goals

- Land the UI rework end-to-end without falsifying provenance.
- Pin the two non-negotiable regressions explicitly: no model
  byline for unattested sessions; no verdict-override page that
  omits the rationale.
- Surface dashboard ↔ web parity through a single
  ``next_actions`` source-of-truth (closes OQ-4).
- Render the GH #5 provenance-evidence chip as
  ``not_yet_correlated`` (muted) until the
  ``process_executions ↔ artifact`` lookup ships — never falsely
  green.
- Reserve the V1.7 ``--status-compromised`` token but do not use
  it until RFC 0046 V1.7 polish lands (closes OQ-1).

## Non-goals

- Reactflow v12 features (e.g. `ViewportPortal` viewport-locked
  overlays). The current `PhaseBands` stub stays `null` until
  v12 lands or a manual `useViewport()` rebuild lands. GH #6
  history.
- Live SSE / WebSocket streaming. V1 uses request/response;
  V1.5 may add SSE for the next-actions banner; V2 keeps it
  optional.
- Server-side mutation outside the existing
  `serve --allow-mutations` gate (D058 / D083 owner-only
  loopback).

## Three-phase landing

### V1 (this dogfood) — primitives + dashboard parity

- New `frontend/src/shared/components/` library: `RunStatePill`,
  `JobStatePill`, `VerdictChip` (with override provenance slot),
  `LaneAttestationChip` (with reason sub-text), `PostureChip`,
  `BylineLine`, `LaneEvidenceChip` (renders
  `not_yet_correlated` muted by default), `ExpectedArtifactsTable`.
- New Jinja2 macro partial `templates/_components.html` so
  server-rendered surfaces and islands speak the same vocabulary.
- `service.py` page-payload shaping for the new chips on
  `run_list` / `run_detail` / `job_detail`.
- `dashboard.py` text-mode parity: render the same chips as ASCII
  glyphs; consume the same `next_actions` list.
- CSS semantic tokens (`--status-running`, `--status-blocked`,
  `--attestation-warn`, `--override-marker`,
  `--evidence-not-yet-correlated`) in `static/base.css`.
- Backend hook **already shipped in v1.45.0**:
  `cli/introspect.py::next_actions` emits the V1.41 burn-down
  verbs (`inspect_packet_with_inbox`, `derive_expected_byline`,
  `recovery_auto_publish`). Pinned by
  `tests/test_next_actions_v141_burndown.py`.

### V1.5 — screen extensions

- `run_detail.html` restructure: next-actions banner + recovery
  panel + sessions strip.
- `job_detail.html` extend: `ExpectedArtifactsTable` partial +
  process-evidence section + override modal.
- `artifact_view.html` extend: byline integrity surface +
  provenance stub (operator-on-behalf trail + override rationale).
- `run_posture_verdicts.html` extend: provenance + attestation
  columns; override rows visually distinct from natural verdicts.
- `view_file.html` breadcrumb back to a run (heuristic match;
  never wrong-link).
- `doctor.html` extend: per-record recipes.

### V2 — islands + interactivity

- `frontend/src/islands/recovery-panel/` new island: dry-run
  preview for `recovery auto-publish`, copy-on-click for CLI
  recipes.
- `static/override_verdict.js` modal + `/v1/invoke` POST.
- `static/copy_on_click.js` for identifiers.
- `workflow-graph-editor` extension: per-node `require_attested_lane`
  field. **V1 scope is data-binding only** (store + render the
  field in the node body); viewport-positioned attestation
  overlays wait for reactflow v12.
- SSE live region for the next-actions banner.

## Acceptance per phase

Each phase ships a dogfood with its own implementation +
3-way build review. V1 is `dogfood-054`; V1.5 and V2 are
follow-up dogfoods.

For V1 specifically (this dogfood):

- All new shared components ship with TypeScript types matching
  the closed enums in `UI_REWORK.md` §5.
- `striatum dashboard --once --run-id <id>` and the web
  `/run/<id>` page render the same chips (same labels + same
  attestation reasons) for a single fixture run; verified by
  `tests/test_dashboard_web_parity.py` (new).
- `tests/test_next_actions_v141_burndown.py` passes (already
  landed v1.45.0 — burndown verbs in `next_actions`).
- Byline regression test: a fixture session whose
  `lane_attestation = 'unattested'` renders `author: operator` in
  both dashboard and web surfaces. No template path emits a
  model byline for that session.
- Override rationale regression test: a fixture verdict with
  `source = 'operator_override'` renders the rationale beside the
  pill in both surfaces.

## Provenance discipline (during this dogfood)

Per operator instruction (2026-05-14): operator-on-behalf
publishes are permitted to keep the dogfood moving, but every one
must use the V1 RFC 0046 path (`--allow-no-process-execution
--override-rationale "<text>"`) so the override is recorded in
the artifact row's `attestation_override_rationale` column and
emitted as a `provenance.publish_without_process_execution`
event. No silent operator publishes.

## Skipped ceremony

The standard dogfood shape (3 designs → synth → design review →
implement → 3-way build review) is collapsed to:

- **Synth** (codex): cite `docs/design/UI_REWORK.md` as the
  canonical input; produce a phase-scope synthesis naming the
  implementer's exact deliverables for V1.
- **Design review** (claude, ergonomics_dx): confirm the synthesis
  matches the canonical handoff and the V1 scope split.
- **Implement** (codex): per UI_REWORK.md §5 + §8 + §9 V1 scope.
  Sub-agents per concern: components, Jinja partial,
  `service.py` payload, `dashboard.py`, CSS tokens, tests.
- **3-way build review** (codex threat_model, claude ergonomics_dx,
  gemini adversarial): the standard cross-lane verification.

Rationale for skipping the design phase: `UI_REWORK.md` already
is the design output of `../records/_frozen/research/CLAUDE_DESIGN_UI_REWORK_PROMPT.md`. Three
more designers rediscovering the same scope would be redundant.

## Future RFCs absorbed

The four §8.10 deferred items from `UI_REWORK.md` are folded into
this RFC's V1.5 + V2 phases:

- Operator recovery UI (recovery panel, override modal,
  copy-on-click) — V2 scope here.
- Provenance honesty (LaneEvidenceChip muted by default,
  attestation-at-recording-time semantics) — V1 scope here.
- Override rationale prominence — V1.5 scope here.
- Compromised-state surfacing — defers to RFC 0047 V1.5 (UI for
  the v1.44.0 compromised state value).

## Open questions

- **OQ-A.** Does `dashboard.py` need a JSON output mode for the
  parity test, or does the test compare structured page-payload
  data against a dashboard text snapshot? Default: structured
  payload comparison, no new output mode.
- **OQ-B.** When does the V1.7 path-specific evidence check land
  so the `LaneEvidenceChip` can flip from `not_yet_correlated` to
  real states (`evidence_present` / `evidence_missing` /
  `override:<rationale>`)? Tracked separately; this RFC does not
  block on it.

## Provenance

- 2026-05-14: `docs/design/UI_REWORK.md` landed on main as
  `f6e81a4` via Claude Design pass against
  `../records/_frozen/research/CLAUDE_DESIGN_UI_REWORK_PROMPT.md`.
- 2026-05-14: operator added context (OQ-4 backend hook, OQ-1
  alignment, GH #5 muted-chip discipline, RFC renumbering to
  0050).
- This RFC adopts the design without re-deriving it.
