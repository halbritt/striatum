# RFC 0029: Operator Recovery for Process-Adapter Blockers

Status: accepted (V1 core)
Date: 2026-05-10
Context:
[`RFC 0014`](0014-process-adapter-completion-guarantees.md),
[`RFC 0009`](0009-long-lived-process-supervision.md),
[`RFC 0013`](0013-local-web-ui.md),
[`docs/DECISION_LOG.md`](../decisions/decision-log.md) (D028, D036, D055, D057, D122),
[`docs/SPEC.md`](../reference/spec.md) § "Adapter Boundary",
`src/striatum/process_completion.py`,
`src/striatum/cli/recovery.py` (retired),
`src/striatum/cli/mutations.py` (retired) (`checkpoint_resolve`)

V1 core acceptance (D122) covers the CLI primitive, process-adapter envelope
commands, documentation, skills/plugin guidance, and focused regression tests.
Dashboard and web-UI action surfacing remain deferred follow-up work.

## Problem

RFC 0014 V1 makes process-adapter exits *deterministic*: every exit lands the
job in one of `success`, `blocked-with-process_outputs_missing`,
`blocked-with-process_review_verdict_missing`,
`blocked-with-process_exit_nonzero`, `blocked-with-process_timeout_exceeded`,
or (via the reconciler) `blocked-with-process_lost_with_outputs_missing`.
That half of the contract is sound.

What is missing is the **operator-facing path back out of the blocked state**
once the operator has remediated. The blocker envelope advertises a recovery
sequence:

```json
"recovery_commands": [
  "striatum publish-artifact ...",
  "striatum verdict ...",
  "striatum recovery requeue-stale --run-id ... --job-id ...",
  "striatum recovery process-reconcile --run-id ..."
]
```

In practice this sequence does not close the loop for any **repo-write** job:

- `recovery requeue-stale` refuses repo-write jobs by design
  (D036, `recovery.py:104-105` (retired)) and
  Additionally requires an *expired* lease — the lease deliberately stays
  active per RFC 0014's "do not release on block" rule
  (`process_completion.py:188-195`).
- `recovery process-reconcile` only acts on `process_executions` rows whose
  `state = 'running'` and whose pid is gone
  (`recovery.py:365-373` (retired)). Inline
  blockers come from processes that already exited cleanly; their rows are
  not in `running`.
- `checkpoint resolve` refuses anything other than `human_checkpoint`
  blockers (`mutations.py:912-915` (retired)).
- `complete` refuses jobs whose state is not `running`
  (`db.py:1444-1445` (retired)).

The result is a sealed terminal state for repo-write process-adapter
blockers: the operator can publish the missing artifact, can record the
missing verdict, can verify with `doctor` that everything else is fine —
and still has no advertised CLI verb to mark the blocker resolved and let
the workflow continue. The only working paths were unsupported manual
database mutation or `recovery cancel-job --cascade`, which kills every
downstream job and loses the work that was successfully completed.

A concrete observation from a recent dogfood run on a three-lane fresh
design step (Codex / Claude Code / Gemini designers, all repo-write):

| Job | Process outcome | Blocker kind | What the operator can do today |
|---|---|---|---|
| `design_codex` | exit 2, no artifact | `process_exit_nonzero` | nothing terminating cleanly; cancel + cascade or unsupported manual DB mutation |
| `design_claude_code` | exit 0, no artifact | `process_outputs_missing` | publish artifact, then nothing terminating cleanly |
| `design_gemini` | exit 0, artifact published *after* the inline validator ran | `process_outputs_missing` | artifact is on disk and in the `artifacts` table; lease is still active; `complete` refuses because state is `blocked`; nothing terminating cleanly |

The Gemini case is the cleanest illustration: every piece of evidence the
runner needs is already present in the database, and the runner refuses
to acknowledge it. The advertised recovery sequence cannot close the
blocker. The downstream `synthesize_design` job and every job after it
stay blocked because the runner has no operator-callable verb that
mirrors the `checkpoint resolve continue` semantics for the
process-adapter blocker family.

This is a product-boundary failure: RFC 0014's diagnostic envelope makes
the failure legible, but the runner does not honor the envelope's own
"recovery_commands" promise for the dominant case (repo-write jobs).

## Goals

- Provide an explicit, audited operator CLI verb that resolves an open
  process-adapter blocker once the underlying remediation is on disk and
  in the database.
- Preserve RFC 0014's deterministic block semantics: the verb only
  resolves blockers when the runner can re-verify that the original
  failure condition no longer holds.
- Preserve RFC 0014's "do not release lease on block" rule so the
  resolved-then-completed flow uses the existing lease and existing
  `process_executions` row — no new identifiers, no synthetic process.
- Honor the privacy boundary (D028): the verb produces structured
  events; it does not capture or surface child stdout/stderr.
- Follow D036's lazy operator-driven recovery posture: no auto-resume
  during ordinary CLI traffic; the operator is in the loop.
- Surface the verb in `striatum status`, `striatum why`, the diagnostic
  envelope's `recovery_commands` array, the dashboard, and the web UI's
  blocker view (RFC 0013 step 7 already established this pattern for
  `checkpoint resolve`).
- Make the asymmetry between `publish-artifact` (accepts a blocked
  job's active lease) and `complete` (refuses a blocked job) either
  intentional and documented, or removed.

## Non-Goals

- **No re-running of the agent process.** That is `cancel-job` plus
  workflow re-prepare territory and is already supported. This RFC is
  for the case where the operator wants to *accept* the work that was
  done.
- **No auto-publishing of missing artifacts.** The operator (or a follow-on
  agent process) is responsible for producing artifacts and recording
  verdicts. This RFC only resolves blockers whose remediating evidence
  is already present.
- **No relaxation of the no-transcripts boundary (D028).** The
  envelope shape stays; resolution events do not capture child output.
- **No change to RFC 0009 long-lived supervision.** Supervised lanes
  have their own liveness model and their own (separate) recovery
  surface; this RFC targets the one-shot adapter path RFC 0014
  governs.
- **No new state in `process_executions` or `blockers` schemas.** The
  existing `blockers.state = 'resolved'` transition and the existing
  `process_executions.state` enum are sufficient.

## Proposal

### 1. New CLI verb: `striatum recovery resume`

```text
striatum recovery resume --blocker-id <id>
                         [--complete --session-id <id> --summary <text>]
                         [--force]
                         [--json]
```

Behavior:

1. Look up the blocker. Refuse with exit code 4 if not found, not open,
   or not in the process-adapter blocker family
   (`process_outputs_missing`, `process_review_verdict_missing`,
   `process_exit_nonzero`, `process_timeout_exceeded`,
   `process_lost_with_outputs_missing`).

2. Re-run `validate_outputs(conn, job=job)`
   (retired Python path `src/striatum/process_completion.py:73`).

3. **For evidence-grounded blockers** (`process_outputs_missing`,
   `process_review_verdict_missing`, `process_lost_with_outputs_missing`):
   - If `validate_outputs` still reports missing artifacts or a missing
     verdict, refuse with exit code 4 and surface the still-missing set
     in the error envelope. The operator's remediation is incomplete.
   - If `validate_outputs` is clean, proceed.

4. **For exit-grounded blockers** (`process_exit_nonzero`,
   `process_timeout_exceeded`): there is no artifact-presence signal
   strong enough to imply the agent's work is sound. Require `--force`
   to acknowledge that the operator has manually inspected the work.
   Without `--force`, refuse with exit code 4 and a message pointing at
   the manual-inspection requirement. With `--force`, proceed.

5. Atomically, in a single transaction:
   - `UPDATE blockers SET state = 'resolved', resolved_at = ? WHERE blocker_id = ?`.
   - `UPDATE jobs SET state = 'running' WHERE job_id = ?` (the job's
     active lease and `current_message_id` are unchanged — RFC 0014
     never released them).
   - Insert a `recovery.process_blocker_resolved` event whose payload
     embeds the original blocker envelope plus a `resolution` block
     `{ "verb": "recovery resume", "force": <bool>, "completed_inline": <bool> }`.

6. **Optional inline complete**: when `--complete --session-id <id>
   [--summary <text>]` is supplied, the verb proceeds to call the
   existing `complete_job` path
   (`db.py:1432` (retired)) using the lease the job
   already carries. This is a convenience wrapper — the lease ID is
   read from `jobs.current_lease_id`; the operator does not have to
   look it up. The session ID is required because `complete_job`
   validates lease ownership.

7. Return a structured envelope:

```json
{
  "status": "resumed",
  "run_id": "...",
  "job_id": "...",
  "workflow_job_id": "design_gemini",
  "blocker_id": "...",
  "blocker_kind": "process_outputs_missing",
  "force": false,
  "completed_inline": true,
  "next_actions": ["claim_available_work", "monitor_run_progress"]
}
```

### 2. Update the diagnostic envelope's `recovery_commands`

`build_recovery_commands` in
retired Python path `src/striatum/process_completion.py:145`
currently emits a `recovery requeue-stale` line that does not work for
repo-write jobs. Replace the trailing two lines for the
process-adapter blocker family with:

```text
striatum recovery resume --blocker-id <blocker_id>
striatum recovery resume --blocker-id <blocker_id> --complete --session-id <session_id> --summary "<text>"
```

Keep `recovery process-reconcile` in the suggestion list only when the
blocker is in the lost family or when the underlying
`process_executions` row is `'running'` with a missing pid.

### 3. Audit the publish/complete asymmetry

`publish_artifact` accepts a blocked job's active lease
(retired Python path `src/striatum/artifacts.py:387`). `complete_job`
does not (`db.py:1444-1445` (retired)). That asymmetry
is what made the Gemini race possible: the agent's late artifact landed
cleanly, but the agent could not call `complete` to close the job out
because the inline validator had already blocked it.

This RFC does **not** remove the asymmetry. The asymmetry is what
gives the operator the ability to *use* the late artifact. But the
asymmetry should be documented in `docs/SPEC.md` § "Adapter Boundary"
and in the docstrings of `publish_artifact` and `complete_job` so that
future readers understand why the two checks diverge.

### 4. Web UI surface (RFC 0013 follow-on)

The web UI's blocker detail view (RFC 0013 step 7, D065) renders
`checkpoint resolve continue/cancel` buttons for `human_checkpoint`
blockers. Add a parallel pair for the process-adapter blocker family:

- **Resume** — calls `recovery resume --blocker-id <id>` (without
  `--complete`).
- **Resume and complete** — calls `recovery resume --blocker-id <id>
  --complete --session-id <id>` where the session is the operator's
  current registered session.
- **Cancel** — calls the existing `recovery cancel-job` (already
  surfaced by `recovery cancel-job` on the same page).

The `--force` form is *not* exposed in the web UI in V1; operators
who want it use the CLI. This keeps the UI safe by default for the
exit-grounded blocker family.

### 5. Doctor / status surface

`striatum status --run-id` already exposes `process_health`. Add a
`resumable_blockers` count that totals open process-adapter blockers
whose `validate_outputs` would now succeed (i.e. would not need
`--force` to resume). `doctor` adds a single check
`process_blocker_resumable` that lists blocker IDs whose remediation
is already complete and whose only missing step is calling `recovery
resume`.

This is a deliberate operator nudge: the runner notices the operator
finished the manual remediation but never closed the loop.

## Acceptance Criteria

- `striatum recovery resume --blocker-id <id>` resolves an open
  `process_outputs_missing` blocker when all required artifacts are
  now present in the `artifacts` table, transitions the job from
  `blocked` to `running`, leaves the existing lease active and
  unchanged, and emits a `recovery.process_blocker_resolved` event
  whose payload embeds the original diagnostic envelope.
- The same verb resolves an open `process_review_verdict_missing`
  blocker when a `verdicts` row now exists for the job.
- The same verb resolves an open `process_lost_with_outputs_missing`
  blocker under the same conditions.
- The verb refuses (exit code 4) when required artifacts or the
  required verdict are still missing, surfacing the still-missing set
  in the error envelope.
- The verb refuses (exit code 4) for `process_exit_nonzero` or
  `process_timeout_exceeded` blockers without `--force`. With
  `--force`, it resolves them on the operator's authority.
- `recovery resume --complete --session-id <id>` performs the resolve
  and the `complete_job` call atomically, returning the standard
  `complete` envelope.
- The diagnostic envelope's `recovery_commands` array contains the
  `recovery resume` line for process-adapter blockers, and operators
  can copy-paste it directly.
- `striatum status --run-id` reports `resumable_blockers` count;
  `doctor` reports `process_blocker_resumable` with blocker IDs.
- The web UI blocker view shows **Resume** and **Resume and complete**
  buttons for process-adapter blockers (parallel to the
  `checkpoint resolve` buttons for human checkpoints).
- The Gemini reproduction lands cleanly: a fixture workflow that
  emits an artifact via a forked publish call after the parent
  process exits results in a blocker that one `recovery resume`
  call closes.
- `docs/SPEC.md` § "Adapter Boundary" documents the `publish_artifact`
  / `complete_job` lease-state asymmetry and points at this RFC.
- `tests/test_recovery_resume.py` covers each blocker family, the
  `--force` gating, the `--complete` inline path, the still-missing
  refusal path, the schema-event payload shape, and idempotency
  against an already-resolved blocker.

## Open Questions

- **Verb name.** `recovery resume` reads well next to `recovery
  requeue-stale`, `recovery cancel-job`, `recovery process-reconcile`.
  Alternatives considered: `recovery resolve-blocker`,
  `recovery unblock`, `blocker resolve`. `recovery resume` wins on
  symmetry with `checkpoint resolve continue` (the verb is the
  *operator's intent*, not the data shape).
- **Should `--complete` be the default?** Arguments for: every
  realistic resume immediately wants to complete the job, since the
  agent process is gone and there is no way for the agent to call
  `complete` itself. Arguments against: keeping resume and complete
  separate matches the `checkpoint resolve continue` shape (which
  re-queues, does not complete) and leaves room for an operator to
  publish a follow-up artifact under the same lease before closing.
  V1 leans toward making `--complete` *opt-in* and documenting that
  the common case is to use it.
- **Should `--force` be required for `process_exit_nonzero`?** A
  non-zero exit is the strongest negative signal the runner has.
  Forcing a resume past it is roughly "I as the operator have
  manually inspected the work and accept it." Recording `force: true`
  on the resolution event preserves the audit trail. The alternative
  is to refuse outright and require `cancel-job` instead, but that
  destroys the (possibly valuable) artifacts produced before the
  crash.
- **Idempotency.** Two concurrent `recovery resume` calls on the same
  blocker should resolve once and produce a clean
  `already_resolved` envelope on the second call, mirroring how the
  reconciler handles double-blocking. The proposed implementation
  takes the row-level lock implicit in the transaction; explicit
  state checks fall out of the existing `blocker.state != 'open'`
  refusal.
- **Auto-resume on `complete`.** A more aggressive shape: when
  `complete_job` is called against a blocked job whose
  `validate_outputs` is now clean, auto-resume and proceed. This is
  more ergonomic but quieter; D036's lazy-operator-driven posture
  argues for the explicit verb. V1 keeps the operator in the loop.
- **Web UI `--force` exposure.** V1 keeps `--force` CLI-only. A V2
  could add a confirmation dialog ("This blocker has no
  artifact-presence evidence; proceed?") and expose it in the UI.
- **Adapter-side preventative for the Gemini race.** The race that
  produced the Gemini blocker (artifact published after the inline
  validator ran) is the *underlying* failure. RFC 0029 makes the
  blocker recoverable; a follow-on change in `process_adapter.py`
  could close the race itself by waiting on child Striatum processes
  spawned in the agent's session, or by re-running `validate_outputs`
  in a short grace window after the parent exits. That fix is
  outside this RFC's scope but is the right next move once 0029
  lands.

## Relationship to Other RFCs

- **RFC 0014** — this RFC is the missing operator half. RFC 0014 makes
  the failure deterministic; RFC 0029 makes the recovery
  deterministic. The two RFCs together close the loop the
  diagnostic envelope's `recovery_commands` field promises.
- **RFC 0009** — independent. The supervised long-lived path has its
  own liveness and recovery surface; RFC 0029 targets the one-shot
  `adapter run` path RFC 0014 governs.
- **RFC 0013 step 7** — establishes the `checkpoint resolve` button
  pattern in the web UI. This RFC adds the parallel pair for
  process-adapter blockers using the same RFC 0012 mutation gate
  and the same web UI argv-passthrough shape.
- **D028** — preserved. `recovery resume` produces structured
  resolution events that embed the original envelope; no child
  stdout/stderr capture.
- **D036** — followed. The verb is operator-driven, lazy, and
  refuses when the underlying state does not justify the
  transition.
- **D057** — RFC 0014 V1 acceptance event. RFC 0029 V1 acceptance
  becomes the next decision in the same sequence.

## Implementation Path

V1 ships in three landable steps; each has its own acceptance test.

1. **`recovery resume` core.** New handler in
   `src/striatum/cli/recovery.py`; argparse wiring in
   `src/striatum/cli/parser.py`; envelope-builder fix in
   `src/striatum/process_completion.py:build_recovery_commands` to
   point at the new verb. Tests at
   `tests/test_recovery_resume.py` covering every blocker kind, the
   missing-evidence refusal, the `--force` gating, the
   already-resolved idempotency case, and the
   `recovery.process_blocker_resolved` event payload shape.
2. **`--complete` inline path + status / doctor surface.** Wraps
   `complete_job` under the same transaction; adds
   `resumable_blockers` to `process_health`; adds the
   `process_blocker_resumable` doctor check. Documents the
   `publish_artifact` / `complete_job` lease-state asymmetry in
   `docs/SPEC.md` § "Adapter Boundary".
3. **Web UI surface.** Two new buttons on the blocker detail view,
   reusing the RFC 0013 mutation argv-passthrough. No new HTTP
   endpoints; no new SSE event types beyond the
   `recovery.process_blocker_resolved` event the core step adds.

RFC 0029 is "accepted" once all three steps land and the Gemini
reproduction at `examples/process-adapter-late-publish-fixture/`
demonstrates the closed loop.

## Domain Modeling

`Blocker` is an aggregate root in the runner's domain model
([`docs/DDD.md`](../reference/domain-driven-design.md)). RFC 0029 adds a new lifecycle
transition (`open → resolved`) for the **process-adapter blocker
family**, mirroring the transition that already exists for
`human_checkpoint` blockers under `checkpoint resolve`. The
transition's pre-condition is a re-evaluation of the original
domain invariant (`validate_outputs`) — the resolution is *not* a
mere flag flip; it is a re-assertion that the failure condition no
longer holds.

The new domain event `recovery.process_blocker_resolved` is a value
object embedded in the `events` table. It carries the original
diagnostic envelope by reference (the envelope is already on the
blocker row's `payload_json`) plus the operator's resolution intent.

The `publish_artifact` / `complete_job` lease-state asymmetry is a
**boundary clarification** in DDD terms: artifact publication and
job completion are two distinct invariants over the same lease, and
RFC 0029 names the asymmetry rather than dissolving it.

Per `docs/DDD.md § "Adding to the model"`, the new vocabulary
introduced here is `recovery resume`, `process-adapter blocker
family`, `evidence-grounded blocker`, `exit-grounded blocker`,
`resumable blocker`. All are CLI- or domain-event-derivable; none
become new daemon Postgres tables or columns.
