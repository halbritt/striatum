# RFC 0045: Multi-Phase Workflow Editor and Schema Support

Status: accepted / implemented
Date: 2026-05-13
Context:
[`RFC 0034`](0034-workflow-generator-and-template-catalog.md),
[`RFC 0037`](0037-web-ui-ergonomic-improvements.md),
[`RFC 0038`](0038-web-ui-feature-additions-and-frontend-toolchain.md),
historical dogfood source paths `docs/dogfood/042/PHASE_1_OPERATOR_NOTES.md`
and `docs/dogfood/042/workflow.json` (validated multi-phase shape informally).

## Problem

`workflow.json` (schema `striatum.workflow.v1`) supports `parallel_group` for
sibling jobs that may run concurrently, but it has no first-class concept of
**phases** — coarser units of work where one phase's outputs gate the next
phase's inputs. Dogfood-042 worked around this by:

1. Hand-rolling cross-phase dependencies through review verdicts.
2. Naming `parallel_group` values like `design_a`, `synth_a`, `build_review_a`
   without a workflow-level "this is one phase, these are two phases" marker.
3. Adding a single bespoke `consolidate_phase_1` job at the end to glue
   tracks together.

Pain points the workaround exposed:

- The validator can't refuse cross-phase dependencies that bypass a synthesis
  gate (the dogfood-042 cascade quirk where `cancel-job --cascade` followed
  blocked_by chains aggressively into the consolidate gate is one symptom).
- The web UI graph editor (RFC 0038's React Flow surface) renders all nodes
  flat; visual mental model of "three parallel tracks under one phase" is
  lost.
- The workflow generator (RFC 0034) has no shape entry for multi-phase work;
  operators rewrite tracks from scratch each time.
- Workflow type chooser (TODO item 18, deferred under RFC 0034) has no
  schema-level signal that a workflow is multi-phase.

## Goals

1. New top-level `phases` array in `workflow.json` (schema bumps to
   `striatum.workflow.v1.1`) listing phase definitions in execution order.
2. New per-job optional `phase` field (string, must match a `phases[].id`).
3. New job `type: "phase_synthesis"` whose completion gates phase transitions.
   Exactly one phase_synthesis job per phase exit (the gate).
4. Validator refuses cross-phase dependencies that don't go through the
   source phase's `phase_synthesis` job.
5. `workflow_generator` catalog entry `multi_phase` (per-phase: track list +
   per-track lane set + per-track shape; phase_synthesis is auto-emitted).
6. React Flow editor (RFC 0038) renders phases as horizontal color-banded
   lanes; cross-phase edges visually distinct from intra-phase edges.
7. Backwards compatibility: `striatum.workflow.v1` workflows continue to
   validate and run unchanged. The `phases` field and `phase_synthesis`
   type are opt-in; absence implies a single implicit phase.
8. `striatum workflow upgrade <path>` (RFC 0040 V1) gains a `--add-phases`
   option that re-parses an existing single-phase workflow and infers a
   reasonable phase boundary from `parallel_group` clusters.

## Non-Goals

- Multi-machine / hosted / cross-repo phase fan-out (already covered by
  RFC 0032 cross-repo workflows; phases are intra-workflow only).
- Decision-point branching (`if verdict X then phase Y else phase Z`).
  Linear phases only in V1; conditional phase routing is a follow-up RFC.
- Phase-scoped retry budgets (re-running a whole phase). V1 reuses the
  existing per-job `max_attempts` semantics; phase-level retry is a
  follow-up.
- Phase parallelism (two phases running concurrently). V1 phases run
  sequentially; the parallelism inside a phase is what `parallel_group`
  already provides.

## Proposal

### 1. Schema additions (`striatum.workflow.v1.1`)

Top-level optional field:

```json
{
  "schema_version": "striatum.workflow.v1.1",
  "phases": [
    {
      "id": "phase_1_design",
      "title": "Design synthesis across three tracks",
      "synthesis_job_id": "synthesize_phase_1"
    },
    {
      "id": "phase_2_build",
      "title": "Implement across three tracks + consolidate",
      "synthesis_job_id": "consolidate_phase_2"
    }
  ],
  "jobs": [
    { "id": "design_go", "phase": "phase_1_design", ... },
    { "id": "design_engram", "phase": "phase_1_design", ... },
    ...
    { "id": "synthesize_phase_1", "type": "phase_synthesis", "phase": "phase_1_design", ... },
    { "id": "implement_go", "phase": "phase_2_build", ... },
    ...
  ]
}
```

Schema rules:

- `phases` is an ordered array. The first phase is the entry phase.
- Each `phases[i].id` is a unique string. Each `phases[i].synthesis_job_id`
  is the job_id of the phase_synthesis job in this phase (must exist in
  `jobs`, must have `type: "phase_synthesis"` and `phase: <this id>`).
- Each job MAY have `phase: <phases[i].id>`. If `phases` is absent or empty,
  `phase` on jobs is forbidden (single implicit phase mode = pure v1).
- Jobs in phase N may depend on jobs in phase ≤N. Cross-phase dependencies
  (job in phase N depending on a job in phase M<N) MUST be on the
  M-phase's `synthesis_job_id`. Validator refuses otherwise.
- A job with `phase` field must either share `phase` with its dependencies
  (intra-phase) or depend on a prior phase's synthesis job (cross-phase).
- `phase_synthesis` job type:
  - Required field `phase` (same as containing phase).
  - Implicit dependency on every other job in the same phase (validator
    generates this; authors don't write it).
  - Verdict-bearing like a review (`on_verdict` semantics inherit from
    existing review job type).
- Phase ordering is the array index. Phase N's `synthesis_job_id` is the
  gate: phase N+1's jobs start only after `synthesis_job_id` completes with
  an accepting verdict.

### 2. Validator changes (`src/striatum/workflow.py`)

- Accept both `striatum.workflow.v1` and `striatum.workflow.v1.1` in
  `schema_version`.
- New helper `validate_phases(workflow)`:
  - All `phases[].id` unique.
  - All `phases[].synthesis_job_id` resolve to a `phase_synthesis` job in
    the same phase.
  - Every job's `phase` field (if present) resolves to a known phase.
  - Cross-phase dependencies use synthesis jobs only.
  - Each phase has exactly one `phase_synthesis` job.
- Existing parallel_group logic stays untouched; phases are a coarser
  grouping over parallel_groups.

### 3. Runtime changes

- `run prepare` materializes phase_synthesis jobs the same way it
  materializes review jobs.
- `claim-next` and `submit-review` need no changes — phase_synthesis
  inherits the review job's lifecycle (claim → publish → verdict).
- Status output (`striatum status --json`) gains an optional `phases` block
  per run with current phase id, gate state, jobs-by-phase counts.
- Status and dashboard projections derive current phase and phase progress from
  persisted job state and workflow metadata.

### 4. Generator catalog entry (`multi_phase`)

`multi_phase` is exposed through the workflow-generator catalog. The generator
accepts a `phases` option array:

```python
class MultiPhaseShape:
    """N phases, each with M parallel tracks; tracks within a phase share
    a synthesis gate. Tracks may use any non-custom built-in generator
    shape, such as minimal, review, code_change, human_checkpoint,
    evidence_backed, or multi_review_synthesis."""

    parameters = {
        "phases": [
            {
                "id": "...",
                "name": "...",
                "tracks": [
                    {
                        "id": "...",
                        "shape": "minimal" | "review" | "code_change" | "human_checkpoint" | "evidence_backed" | "multi_review_synthesis",
                        "lane_id": "author",
                        "options": {"review_postures": [...]},
                    }
                ],
                "synthesis_lane_id": "reviewer",
            }
        ]
    }
```

The generator emits the per-track jobs and the phase_synthesis jobs.
Authors provide the phase shape and lane topology; generator owns the
phase_id / parallel_group / phase_synthesis wiring.

### 5. Web UI (React Flow editor under RFC 0038)

`src/striatum/web/frontend/src/islands/WorkflowGraphIsland.tsx` (or current
location) gets:

- Phase color bands: each phase gets a horizontal background band with a
  distinct color and the phase title rendered top-left.
- Job nodes render inside their phase's band (Y position derived from
  phase index, X from parallel_group/order).
- Cross-phase edges render as thick black arrows; intra-phase edges stay
  thin grey.
- Click on phase band header opens a side panel with phase metadata +
  synthesis job summary.
- Drag-drop respects phase boundaries: dropping a job in a different band
  rewrites its `phase` field.

### 6. CLI ergonomics

- `striatum workflow validate <path>` reports phase structure in `--json`
  output when phases exist.
- `striatum workflow upgrade <path> --add-phases` heuristic:
  - Cluster jobs by `parallel_group` prefix (e.g. `design_a`, `synth_a`,
    `build_review_a` → suggests one phase per parallel_group prefix).
  - Generate a phases array + insert phase_synthesis jobs.
  - Print proposed diff; refuse to write unless `--apply` is also passed.
- `striatum dashboard --run-id <id>` adds a phase progress bar.

### 7. Migration plan for existing workflows

- All shipped workflow.json files (dogfood-NNN scaffolds) stay at v1 — no
  forced rewrite.
- `docs/dogfood/042/workflow.json` documents in a comment that it used
  the informal multi-phase pattern that motivated this RFC.
- New workflows authored after RFC 0045 lands may opt into v1.1.

## Acceptance Criteria

1. New workflows can declare phases + phase_synthesis jobs; validator
   accepts well-formed v1.1 and refuses ill-formed ones with named errors.
2. Existing v1 workflows continue to validate and run unchanged.
3. `striatum workflow validate` reports phase structure on v1.1.
4. `striatum workflow generate --shape multi_phase --option phases='<json-array>'`
   produces a valid v1.1 workflow.
5. `striatum workflow upgrade --add-phases <path>` infers a phase
   structure from parallel_group clusters, previews by default, and writes
   the upgraded file when `--apply` is supplied.
6. React Flow editor renders phase color bands and cross-phase edges
   differently from intra-phase edges.
7. `striatum status --json` includes `phases` block for v1.1 runs.
8. Test fixture `tests/fixtures/multi_phase_workflow.json` exercises the
   full lifecycle in CI.

## Implementation Plan

Step 1: schema accept v1.1, validator emits phase errors. (Python only,
no UI.)

Step 2: runtime materializes phase_synthesis jobs, status reports phases.

Step 3: generator `multi_phase` shape + `workflow upgrade --add-phases`.

Step 4: React Flow editor band-rendering.

Step 5: CI fixture + e2e test exercising a 2-phase, 2-track-per-phase
workflow.

Steps 1+2 are the Python core; Steps 3-4 are operator-side; Step 5 is
verification. Steps may overlap across implementers (Track A: Python
core; Track B: generator + CLI ergonomics; Track C: React Flow editor).

Implementation status: the Python CLI and Go daemon both implement
`workflow generate --shape multi_phase` and `workflow upgrade --add-phases`
for V1.1 phase workflows. Generation emits ordered `phases`, per-track
job remapping, `phase_synthesis` gates, and cross-phase synthesis-to-entry
edges; upgrade remains a preview-by-default V1-to-V1.1 rewrite that infers
phases from `parallel_group`/job-id prefixes, inserts `phase_synthesis` jobs
when needed, rewrites cross-phase edges through the phase synthesis job, and
refuses live writes when non-terminal runs reference the workflow.

## Open Questions

- Should phase_synthesis jobs support cycle iteration the same way review
  jobs do? V1 inherits review semantics, so yes by default.
- Should the validator refuse phase_synthesis jobs that have zero
  dependencies (a phase with no work)? Probably yes — empty phases are a
  bug. V1: refuse.
- Should `parallel_group` values be scoped by phase (avoid collisions
  across phases)? V1: yes, parallel_group strings are namespaced by phase
  id implicitly.

## Domain Modeling

New concepts:

- **Phase**: a named, ordered unit of work; gates the next phase.
- **Phase synthesis**: a verdict-bearing job that consolidates a phase's
  outputs and gates the next phase's start.
- **Cross-phase edge**: a dependency from a job in phase N to the
  phase_synthesis of phase M<N.
- **Intra-phase edge**: a dependency from a job to another job in the
  same phase (existing `depends_on` semantics).

Existing concepts unchanged: lane, role, parallel_group, capability,
write_scope, verdict, lease, work packet.
