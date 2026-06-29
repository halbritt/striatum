# RFC 0047 — Decision-record propagation and compromised run state

**Status:** accepted / landed
**Scope:** V1.8 (multi-step)
**Closes:** GH #3

## Background

`striatum decision record` exists and writes a Markdown artifact + a
`decision.recorded` event with `outcome ∈ {accepted, rejected,
accepted_with_follow_up}`. The event lives in the audit chain but
does not propagate to:

1. `runs.state` — stays `completed` even after a `rejected` decision.
2. `verdicts` rows — the original `accept` verdict still wins
   completion-gating logic for downstream consumers.
3. On-disk artifact bylines — Markdown still says `author:
   reviewer-codex-gpt-5.5-001` after the run is "rejected".
4. `striatum status` / `striatum why` — read as normal completed run.
5. `evidence export` — emits events but no first-class compromised
   flag.

Downstream tooling must walk the events table looking for
`decision.recorded` with `outcome: rejected`. Most surfaces don't.

## Goals

- Introduce a first-class `compromised` state for runs whose
  `decision record --outcome rejected` has been applied.
- Propagate the rejection to `verdicts` (mark superseded), to
  `striatum status` / `why` / `dashboard`, and to `evidence export`.
- Provide a recovery path: `decision record --outcome
  accepted` against a previously compromised run reopens it as
  `completed` with the rejection trail preserved.
- On-disk artifact byline rewrite is out of scope (see Non-goals).

## Non-goals

- Mutating published Markdown files. Bylines stay; the compromised
  status surfaces alongside them via the runs/verdicts surfaces.
- Cascading rejection across runs (single-run scope per decision).
- New evidence schema for the rejected decision payload (existing
  `decision.recorded` event + the decision artifact carry the
  rationale).

## Design

### Schema migration

Repo-local SQLite + Postgres daemon — next schema version after RFC
0046:

```sql
-- New enum value on runs.state. SQLite is text-typed; the runner's
-- migration adds the value to the registered set in
-- src/striatum/schema.py.
-- Postgres-side: ALTER TYPE striatumd.run_state ADD VALUE 'compromised';

-- Verdict supersession marker. Existing verdicts keep their values;
-- the column records WHICH decision rejected them.
ALTER TABLE verdicts ADD COLUMN superseded_by_decision_id TEXT;
ALTER TABLE verdicts ADD COLUMN superseded_at TEXT;
```

### Decision propagation

`striatum decision record --outcome rejected`:

1. Insert the `decision.recorded` event (existing).
2. Write the decision artifact (existing).
3. Update `runs.state = 'compromised'` for the target run.
4. For every accepting verdict in scope (`accept` /
   `accept_with_findings`) on the run, set
   `superseded_by_decision_id = <decision_id>` and
   `superseded_at = utc_now()`.
5. Emit `run.compromised` event with `decision_id` and the
   superseded verdict count.

`striatum decision record --outcome accepted` against a `compromised`
run:

1. Insert the `decision.recorded` event with the new outcome.
2. Set `runs.state = 'completed'`.
3. Verdicts stay marked superseded — the rejection history is
   preserved; the accepted decision reopens the run but doesn't
   erase the trail.
4. Emit `run.reopened_after_compromised`.

### Run state semantics

`runs.state` enum (current + new):

| state                          | meaning |
|---|---|
| `prepared`                     | run created, branch not confirmed |
| `needs_branch_confirmation`    | branch confirmation pending |
| `ready`                        | branch confirmed, awaiting `run start` |
| `running`                      | active work |
| `paused`                       | operator paused |
| `completed`                    | all jobs terminal, no compromised flag |
| `failed`                       | any job failed and no recovery applied |
| `canceled`                     | operator-canceled |
| **`compromised`** (NEW)        | `decision record --outcome rejected` |

`maybe_complete_run` keeps existing semantics for natural completion
but ignores compromised runs (a compromised run stays compromised
until a new decision reopens it).

### Surfaces

- `striatum status --run-id <id>`: top-level `state` field returns
  `compromised` when applicable; new `superseded_verdicts` array
  lists `(job_id, original_verdict, decision_id)`.
- `striatum why <run_id>`: events output unchanged; the new
  `run.compromised` event surfaces in the chronological list.
- `striatum dashboard --once`: run-state pill renders
  `compromised` (red) instead of `completed`. Per-job lines show a
  `superseded` marker on rows whose accepting verdict was
  rejected.
- `striatum evidence export`: emits the decision event +
  `superseded_verdicts.jsonl` listing the affected verdicts.
- Web UI (per `../records/_frozen/research/CLAUDE_DESIGN_UI_REWORK_PROMPT.md`):
  `RunStatePill` adds a `compromised` variant; `VerdictChip`
  renders a strikethrough + "superseded by <decision_id>" overlay
  when applicable.

### Audit chain

The audit chain anchor recomputes only on event insert (existing
behavior). The new event types (`run.compromised`,
`run.reopened_after_compromised`) extend the audit-chain payload
shape, NOT its hashing strategy. Existing audit-chain verification
continues to pass.

### CLI verbs (no new verbs)

Reuse `striatum decision record`. The propagation is internal —
operators don't need a new verb.

## Acceptance

- `tests/test_decision_rejection_propagates.py`:
  - `decision record --outcome rejected` against a completed run:
    `runs.state` → `compromised`, verdicts get
    `superseded_by_decision_id`, `run.compromised` event emitted.
  - `striatum status --run-id <id>` reports `state: compromised`
    + `superseded_verdicts` array.
  - Subsequent `decision record --outcome accepted` reopens to
    `completed`, verdicts stay marked superseded, new event
    emitted.
  - Idempotency: re-running `decision record --outcome rejected`
    on an already-compromised run is a no-op.
  - Evidence export bundle includes the supersession info.
- Schema migration runs cleanly on existing repos (no data loss).
- `striatum dashboard --once` renders the compromised pill.
- `make lint`, `make typecheck`, `make test -m "not multi_repo"`
  green.

## Rollout

- v1.45.0: schema migration + propagation + surface updates.
- Operators who applied `decision record --outcome rejected` to old
  runs pre-V1.7 do not see the supersession marker on existing
  verdicts (the column was added empty). A one-shot
  `striatum recovery backfill-compromised --run-id <id>` is a
  follow-up convenience verb if migrations volume justifies.

## Open questions

1. Should `compromised` block downstream consumers from claiming
   blocked dependents that were waiting on a superseded verdict?
   Default: NO — superseded verdicts still gate downstream work
   exactly as they did. The `compromised` flag is reporting, not
   re-derivation.
2. How does this interact with the V2.0 daemon-side substrate
   migration (RFC 0048)? Default: the schema migration in this RFC
   lands on the V1 substrate; RFC 0048 inherits the column on the
   daemon-side PG schema.
