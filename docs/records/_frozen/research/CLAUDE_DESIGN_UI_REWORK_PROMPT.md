# Claude Design Prompt: Striatum UI Rework

You are Claude Design working on `<striatum-repo>` (for example
`~/git/striatum`). Your job is
to produce an implementation-ready UI redesign handoff for Codex implementers.
Do not implement code in this pass.

## Required Context

Read these files first:

- `AGENTS.md`
- `docs/SPEC.md`
- `docs/HOW_TO_AGENT.md`
- `docs/UBIQUITOUS_LANGUAGE.md`
- `docs/DECISION_LOG.md`
- `docs/rfcs/0037-web-ui-ergonomic-improvements.md`
- `docs/rfcs/0038-web-ui-feature-additions-and-frontend-toolchain.md`
- `src/striatum/service.py` (server entry, route dispatch, response shaping)
- `src/striatum/web/__init__.py`
- `src/striatum/web/workflows.py`
- `src/striatum/web/markdown.py`
- `src/striatum/web/graph_svg.py`
- `src/striatum/web/chat_provider.py`
- `src/striatum/dashboard.py` (the compact terminal view that web should
  feel consistent with)

Then inspect every template under `src/striatum/web/templates/` —
`base.html`, `run_list.html`, `run_detail.html`, `job_detail.html`,
`run_posture_verdicts.html`, `workflows_index.html`, `workflow_detail.html`,
`workflow_new.html`, `workflow_edit.html`, `doctor.html`, `chat.html`,
`chat_index.html`, `artifact_view.html`, `view_file.html`, `view_tree.html` —
and the islands under `src/striatum/web/frontend/src/islands/`
(`code-viewer`, `tree-browser`, `workflow-chooser`,
`workflow-graph-editor`, `shared`).

Inspect related tests on demand: `tests/test_service.py`,
`tests/test_web_*.py`, `tests/test_dashboard.py`, and any browser or CLI
tests that describe operator flows.

## Objective

Redesign the Striatum operator UI around the current product direction:
a local-first orchestration runner for terminal-based AI coding agents
that drives multi-lane workflows (designer → synth → reviewer → builder
→ build-review) over a target repository, with daemon-owned PostgreSQL
as authoritative live state and `.striatum/` as operational scratch. The
design must help an operator drive
runs through claim → ack → publish → complete, triage stale leases and
checkpoints, override verdicts, inspect artifacts and audit chains, and
recover process-adapter failures — without implying unsupported
provenance, attestation, or model-identity claims.

The output must be directly usable by Codex as an implementation brief.
Prefer precise interface structure, labels, state tables, component
behavior, and acceptance checks over broad visual direction.

## Design Constraints

- Treat this as an operational engineering tool, not a marketing page.
- Do not create a landing-page hero. No splash, no animated illustration,
  no testimonial cards, no marketing copy about AI.
- Avoid decorative gradients, orbs, bokeh, dashboard "card sea" layouts
  with vanity metrics, and oversized empty states.
- Favor dense but readable information hierarchy, predictable navigation,
  compact controls, monospace identifiers, and side-by-side comparison
  where it improves decisions.
- Preserve truthful claim boundaries. Byline integrity, attestation
  status, verdict provenance, override-vs-natural verdicts, blocker
  severity, and process-adapter evidence must remain visibly qualified.
  See "Truthfulness And Claim-State Rules" below.
- Make warnings and unsupported fields visible without blocking normal
  exploration. The operator must be able to drill into a blocker, an
  override verdict, or a non-conformant artifact without leaving the
  current run context.
- Keep web and `striatum dashboard --once` terminal views conceptually
  aligned. The same primitives (run state pill, job state pill, lane
  attestation chip, verdict provenance chip, posture chip) should be
  recognisable across both surfaces, even though the renderers differ.
- Do not require new backend capabilities unless you explicitly label
  them as future/backlog. The daemon is the only writer of live state
  (D094/D104); UI changes must not introduce new writers or new
  state machines.
- Do not capture stdout/stderr transcripts (D028). UI must never propose
  a "live terminal output" panel that mirrors a supervised process.
- Do not hide domain complexity behind vague copy. Use precise but
  concise labels (`needs_revision`, `stale_lease`, `process_exit_nonzero`,
  `lane_attestation=unattested:no_attached_supervisor`).
- Design for responsive browser layouts. Mobile can be compact and
  inspectable; it does not need to make every operator workflow equally
  fast — claim-next, publish-artifact, and override-verdict are
  desktop-first.

## Required Deliverable

Create a Markdown design handoff at `docs/design/UI_REWORK.md` with these
sections. Use the V1 finding/decision front-matter pattern if and only if
you choose to publish it as a Striatum artifact later — for the handoff
itself, plain Markdown is fine.

1. **Design Intent**
   - One short paragraph describing the redesigned operator surface.
   - Explicit statement of the claims the UI must not make (model-author
     forgery, calibrated lane provenance for unattested sessions,
     transcript capture, externalized state).

2. **Primary Operator Flows**
   - Start a run: `init` → `workflow validate` → `run prepare` →
     `branch confirm` → `run start` (mostly CLI; surface what the UI
     can shorten).
   - Claim-next and publish-artifact loop for an active session.
   - Verdict + override-verdict (including the new V1.41
     `--auto-fresh-session` ergonomics and B2 next-action surface).
   - Recovery triage: stale-lease, process-adapter blocker,
     human-checkpoint blocker, `recovery auto-publish` (V1.41).
   - Cross-run audit: byline integrity, verdict provenance, override
     reasoning, attestation state.
   - `striatum doctor` problem groups + per-record disposition.

3. **Information Architecture**
   - Proposed top-level navigation or layout regions.
   - What appears in the first viewport on desktop for `/run/<id>`,
     `/run/<id>/job/<wfjob>`, `/`, `/workflows/`, `/doctor`.
   - What collapses or moves on narrow screens.
   - How the new V1.41 burn-down verbs (`byline`, `inbox`,
     `recovery auto-publish`) surface in the UI's recovery panel and
     in `striatum dashboard --once`.

4. **Screen Specifications**
   - For each major screen/panel — `run_list`, `run_detail`,
     `job_detail`, `run_posture_verdicts`, `workflows_index`,
     `workflow_detail`, `workflow_new`, `workflow_edit`, `doctor`,
     `chat`, `artifact_view`, `view_file`, `view_tree` — include
     purpose, visible data, controls, empty states, loading states,
     error states, and disabled states.
   - Include exact user-facing labels for important controls and
     warnings, especially: `lane_attestation` ("attested" /
     "unattested: no_attached_supervisor" / "unattested:
     pid_identity_mismatch"), verdict provenance ("natural" /
     "operator-override (rationale)"), blocker recovery affordances,
     and the V1.41 `expected_author_line` surface.

5. **Component Inventory**
   - List reusable components Codex should build or refactor toward.
   - Include props/data requirements, states, and expected
     interactions. At minimum:
     - `RunStatePill` (states: prepared, needs_branch_confirmation,
       ready, running, paused, completed, failed, canceled — plus a
       future-backlog `compromised` per GH #3).
     - `JobStatePill` (states: queued, blocked, ready, claimed,
       running, completed, failed, canceled, skipped, stale_lease,
       waiting_human).
     - `VerdictChip` (variants: accept, accept_with_findings,
       needs_revision, reject; with a `provenance` slot for
       natural-vs-override).
     - `LaneAttestationChip` (attested, unattested with reason
       sub-text, optional supervisor_id link).
     - `PostureChip` (threat_model, ergonomics_dx, adversarial,
       neutral).
     - `BylineLine` (renders the canonical
       `author: <role>-<model>-<ord>` or `author: operator [self-
       declared: <label>]`; refuses to be substituted for a free-text
       author label).
     - `BlockerTriagePanel` (lists open blockers; surfaces `next_actions`
       including `recovery cancel-job --cascade`, `recovery resume
       --force`, `recovery auto-publish --dry-run`, and the new V1.42
       terminal-blocker dismiss path).
     - `ExpectedArtifactsTable` (the V1.41 publish-defaults source-of-
       truth: shows declared path / kind / logical_name / required and
       links to `striatum publish-artifact --path` recipes).
     - `ProcessExecutionEvidence` (future-backlog: paired with
       GH #2/#5 V1.7 work; renders matching `process_executions` rows
       for an artifact so unattested operator-on-behalf publishes are
       visibly qualified).
   - Make every component support both web (HTML+CSS+TS island where
     interactivity is needed) and the dashboard terminal view (text-
     mode equivalent).

6. **Truthfulness And Claim-State Rules**
   - Table of statuses and warning language for: byline (attested /
     unattested / operator / operator-self-declared), verdict
     provenance (natural / operator-override / cycle-revised),
     attestation reason (`session_missing`, `no_attached_supervisor`,
     `pid_gone`, `pid_identity_mismatch`, `lane_command_missing`,
     `run_mismatch`, `session_mismatch`), blocker severity
     (`blocked` / `human_checkpoint`), process-adapter outcome
     (`process_exit_nonzero`, `process_timeout_exceeded`,
     `process_outputs_missing`, `process_review_verdict_missing`,
     `process_lost_with_outputs_missing`), and provenance evidence
     (V1.7 backlog: `lane_evidence_present`,
     `lane_evidence_missing_operator_override`).
   - Specify how each surfaces in `striatum dashboard --once` (single
     ASCII chip) and in the web UI (chip + hover/tap detail card).
   - Pin two regressions explicitly: (a) byline computation must never
     emit a model byline (`<role>-<model>-<ord>`) for an unattested
     session; (b) verdict-override pages must always render the
     override rationale prominently and never silently substitute it
     for the original verdict.

7. **Visual System**
   - Layout density, typography scale, spacing, icon usage,
     table/list style, plot treatment, and color semantics.
   - Keep the palette restrained but not one-note. Avoid dominant
     purple, beige/tan, dark slate/blue, or brown/orange themes.
   - Specify colors as semantic tokens (`--status-running`,
     `--status-blocked`, `--status-compromised`, `--attestation-warn`,
     `--override-marker`), not just hex values.
   - Define a single dense table style for runs/jobs/verdicts/blockers
     and reuse it. No mixing of card grids and tables for the same
     entity type.
   - Monospace identifiers (`run_<hex>`, `job_<hex>`, `art_<hex>`,
     `sess_<hex>`) everywhere; never wrap or ellipsize without a
     copy-on-click affordance.

8. **Implementation Map**
   - Map each proposed UI change to likely files/modules:
     - server-side rendering: `src/striatum/service.py` (route handler
       + response shaping) plus the relevant template under
       `src/striatum/web/templates/`.
     - vite islands: `src/striatum/web/frontend/src/islands/<island>/`
       with the build path under `src/striatum/web/static/build/`.
     - dashboard parity: `src/striatum/dashboard.py`.
     - new components: under
       `src/striatum/web/frontend/src/shared/` plus matching CSS
       tokens under `src/striatum/web/static/base.css` /
       `src/striatum/web/static/app.css`.
   - Identify which changes are safe frontend-only work and which
     require backend or model support. Be explicit about
     `process_executions` lookups (V1.7 backlog) and the
     `runs.state = compromised` enum (V1.7 GH #3 backlog).
   - Mark any proposed future work that should become an RFC instead
     of being implemented immediately. Reference RFC 0037 / 0038
     numbering and propose RFC 0046 (or next available) when
     appropriate.
   - Reference the V1.41 burn-down (`recovery auto-publish`,
     `striatum byline`, `striatum inbox`,
     `override-verdict --auto-fresh-session`,
     publish-artifact defaults) explicitly: the UI's recovery and
     publish-on-behalf surfaces must surface these affordances.

9. **Acceptance Checks For Codex**
   - Concrete checklist for automated tests:
     - Browser smoke: every template renders against a seed fixture
       and contains the expected chips (run state, job state,
       verdict, posture, lane attestation).
     - Responsive screenshots at 1440, 1024, 768, 375 widths for
       `/run/<id>` and `/run/<id>/job/<wfjob>`.
     - Warning text assertions: the byline regression test must
       refuse to render a model byline for an unattested session.
     - Keyboard/accessibility: tab order through the next-actions
       banner; aria-labels on every status chip; visible focus rings.
     - Regression tests for unsupported claims:
       - No template emits `author: <role>-<lane>` text for a
         session whose `lane_attestation.attested` is `False`.
       - No verdict-override page omits the override rationale.
       - No blocker view promises recovery via a path that does not
         exist (e.g. `recovery resume` against a terminal job
         without `--force`).
     - `make ui-verify-bundle` continues to refuse committed
       placeholder bundles after the redesign (the v1.40.0
       `STRIATUM_MULTI_REPO_REQUIRE_PG`-style sentinel pattern;
       see dogfood-045 D099 + RFC 0038 V1.5 history).
     - Dashboard parity test: `striatum dashboard --once --run-id <id>`
       produces the same chips/labels as the corresponding web
       header for a single fixture run.

10. **Open Questions**
    - Only list questions that block implementation.
    - If a reasonable assumption is safe, make the assumption and
      label it.
    - Include at minimum: (a) whether to land the `runs.state =
      compromised` enum + propagation as part of this redesign or
      defer to GH #3 V1.7; (b) whether the V1.7
      `lane_evidence_present` chip should appear now as a "future"
      stub or wait until the publish-time guard ships.

## Output Rules

- Output only the design handoff Markdown to
  `docs/design/UI_REWORK.md`.
- Use file references where relevant (paths are relative to the repo
  root).
- Do not include implementation patches.
- Do not include an `author:` line in the handoff itself (the
  publisher will compute the canonical byline if and when you choose
  to publish this through the runner; raw handoff text should stay
  byline-less to avoid stale ordinals).
- Be specific enough that Codex can implement without interpreting
  visual intent from prose alone.
- When you name a label, give the exact string; when you name a state,
  use the exact identifier from `docs/UBIQUITOUS_LANGUAGE.md` and the
  schema.
