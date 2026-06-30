# How to Act as Human Principal

Per [RFC 0053](../rfcs/0053-human-principal-and-terminology-truing.md)
and [D103](../decisions/decision-log.md), the human principal's only role is to
**resolve unresolvable blockers or decisions**. Routine workflow
execution — claim, ack, publish, verdict, complete — is the AI
operator's job, covered in [HOW_TO_AGENT.md](how-to-agent.md).
Use this doc when an escalation has surfaced and you need to look
at it.

The full operator-by-hand walkthrough is retained at the bottom of
this page as **reference**: skip to the [Manual operator
reference](#manual-operator-reference) section only if you are
specifically driving the runner by hand (debugging, demo, or the
rare case where no AI operator is in the loop). For normal use you
will not read past the escalation playbook.

## Escalation playbook

### What you'll see

The AI operator escalates to you in one of two shapes:

1. **A declared blocker.** A blocker `kind` from the closed set
   the runner cannot auto-resolve: `ambiguous_goal`,
   `missing_authority`, `contradicting_decisions`,
   `no_available_reviewer_lane`,
   `committee_stalemate` (RFC 0052), `override_required`.
2. **An AI-self-declared escalation.** An `escalation` artifact
   the AI operator published when it judged itself stuck and no
   declared blocker class fit. `striatum.escalation.v1` artifacts are
   linked to existing escalation-class blockers; publishing one enriches
   the escalation inbox projection but does not create a new live blocker.

Either way the escalation appears in your daemon-backed inbox alongside
ordinary state. Check it whenever you sit down at the runner:

```bash
striatum --repo "$TARGET_REPO" inbox --json
striatum --repo "$TARGET_REPO" status --json | jq '.blockers'
```

`inbox --session-id <session_id>` is still available as the
operator-on-behalf packet helper; the principal inbox does not require a
session id.

### Inspect

For an escalation, inspect the projected escalation row and the live state it
references:

```bash
striatum --repo "$TARGET_REPO" escalation show --escalation-id <escalation_id> --json
striatum --repo "$TARGET_REPO" why <blocker_or_job_or_session_id> --json
striatum --repo "$TARGET_REPO" run summary --run-id <run_id> --json

# Follow the live derived dialogue or provenance trajectory of a run:
striatum --repo "$TARGET_REPO" trajectory watch <run_id> dialogue --since-seq 0
striatum --repo "$TARGET_REPO" trajectory export <run_id> provenance --json
```

`why` includes the active blockers with their `kind` and `reason`.
Read the most recent artifact the AI was working on (the workflow
will tell you where it lives on disk) and any decision artifacts
the AI cited.

If the run uses a tmux-backed PTY, attach only for local observation:

```bash
tmux list-sessions
tmux attach -t <session-name>
```

Use `status`, `dashboard`, `why`, `supervise status`, and durable
artifacts for decisions. Tmux pane text, terminal output, and
transcripts are not workflow state and must not be used as proof that
a job completed, produced a verdict, or resolved a blocker.

### Decide

Form the resolution outside the runner — you are the authority.
Common shapes:

- **Ambiguous goal** → narrow the goal, then record it as a
  decision.
- **Missing authority** → either delegate (record a decision
  granting the authority) or substitute the action with one the AI
  is already authorized to take.
- **Contradicting decisions** → record a new decision that
  supersedes one of the prior ones (RFC 0047 propagates the
  supersession through the verdicts table).
- **No available reviewer lane** → either change the workflow to
  use available lanes or accept a single-lane review with a
  recorded rationale.
- **Committee stalemate (RFC 0052)** → record a decision that
  selects one of the contending designs or rejects all and
  re-scopes.
- **Override required** → record an `accepted` or `rejected`
  decision against the run; RFC 0047 propagates it.

### Resolve

Record the decision through the runner so the audit chain
captures it:

```bash
striatum --repo "$TARGET_REPO" decision record \
  --run-id <run_id> \
  --path docs/decisions/principal-resolution.md \
  --outcome accepted | rejected | accepted_with_follow_up \
  --title "<short>" \
  --rationale "<why>" \
  --json
```

If the decision resolves an escalation-class blocker, record the resolution
through daemon `escalation.resolve` so the AI operator can proceed:

```bash
striatum --repo "$TARGET_REPO" escalation resolve \
  --escalation-id <escalation_id> \
  --decision-id <decision_id> \
  --json
```

Use `striatum recovery resume` only for process-adapter blockers.
`escalation resolve` records an `escalation.resolved` event with the
decision id or resolution note.

The AI operator picks up the next packet automatically.

### When to override a verdict

Sometimes the escalation is a non-accepting verdict you disagree
with. Use `override-verdict` per RFC 0047:

```bash
striatum --repo "$TARGET_REPO" override-verdict \
  <session_id> \
  <job_id> \
  <verdict> \
  --rationale "<why this overrides>" \
  --auto-fresh-session \
  --json
```

The override flows through the same audit-chain machinery; the
prior verdict is marked superseded.

### When not to publish on behalf

You almost never need to. The AI operator has tools for stalls
(RFC 0051 auto-finalize from frontmatter, RFC 0046 lane-evidence
guard with operator override) that cover most of what
publish-on-behalf used to do. If you find yourself needing
`publish-artifact --allow-no-process-execution`, that is itself a
signal — record what went wrong as a decision so the next AI
operator session can avoid the same trap.

### Cross-reference

- [HOW_TO_AGENT.md](how-to-agent.md) — the AI operator's playbook.
- [SPEC.md § Branch Confirmation](../reference/spec.md) — confirmation is the
  operator's job, not the principal's.
- [RFC 0052](../rfcs/0052-committee-deliberation-workflow.md) —
  committee stalemate is one of the named escalation triggers.

---

## Manual operator reference

> The rest of this document is **reference** for the rare case
> where a human drives the runner by hand. Per RFC 0053 / D103
> this is no longer the default; the AI operator does the work and
> you (the principal) only show up for escalations covered above.
> Read past this point only if you really are the keyboard for
> some specific reason.

The examples assume you are in a striatum checkout and want to
orchestrate some target repository. Set these once:

```bash
RUNNER=striatum
TARGET_REPO=/path/to/target/repo
WORKFLOW=examples/rfc-ledger-cleanup/workflow.json
OUTPUT_DIR=striatum/rfc-ledger    # see "Where artifacts land" below
```

Point `TARGET_REPO` at the repository you want to orchestrate.

`OUTPUT_DIR` is **not** a runner setting — striatum has no
"output directory" flag. The runner accepts each artifact's path
verbatim from the *workflow file* (every job declares its
`expected_artifacts[].path` and `write_scope.allowed_paths`). The
shell variable below is just a convenience for the example
commands; in your own workflow you choose where artifacts land
when you author it.

For first-contact use, pick an output directory that is easy to
delete if you change your mind (see
[Where artifacts land](#where-artifacts-land)). The example
workflow `examples/rfc-ledger-cleanup/workflow.json` declares
`docs/reviews/rfc-ledger/` as its output root; if you don't want
that path created in your target repo, point `WORKFLOW` at a
different fixture or copy the example into a scratch tree first.

## Initialize

```bash
"$RUNNER" repo add "$TARGET_REPO" --init --json
"$RUNNER" --repo "$TARGET_REPO" status --json
"$RUNNER" --repo "$TARGET_REPO" doctor --json
```

This creates `.striatum/` as operational scratch under the target
repo (supervised wrapper FIFOs, pidfiles, and transient supervisor
scratch) and adds `.striatum/` to that repo's
`.gitignore`. Authoritative workflow state lives in the daemon-
owned PostgreSQL instance under a `repository_id` scope per D094 /
RFC 0043; `repo add --init` registers the repository with the daemon
and initializes scratch. The daemon is a hard prerequisite — without a
reachable daemon, repository-scoped verbs refuse with exit code 11
(`daemon_unreachable`); against a pre-D094 SQLite-only repo they
refuse with exit code 12 (`repo_not_migrated`) and tell you to
archive/remove legacy SQLite files before registering. See
[postgres-transition.md](postgres-transition.md) for the current
PostgreSQL bootstrap runbook.

To also drop a self-contained agent skill bundle that teaches a
Striatum-aware agent how to drive the runner (RFC 0015 V1):

```bash
# Claude Code:
"$RUNNER" --repo "$TARGET_REPO" skills install --profile claude_code --json
"$RUNNER" --repo "$TARGET_REPO" plugin install --profile claude_code --json

# Codex CLI:
"$RUNNER" --repo "$TARGET_REPO" skills install --profile codex --json
"$RUNNER" --repo "$TARGET_REPO" plugin install --profile codex --json

# Gemini CLI:
"$RUNNER" --repo "$TARGET_REPO" skills install --profile gemini --json

# Anything else:
"$RUNNER" --repo "$TARGET_REPO" skills install --profile generic --json

# All skill profiles at once (deterministic order, disjoint paths):
"$RUNNER" --repo "$TARGET_REPO" skills install --profile all --json
```

The current Go installer ships four skill profiles plus an `all`
fan-out: `claude_code`, `codex`, `agy`, and `generic`. All profiles
are byte-identical on re-install; operator edits are preserved unless
you pass `--force`.

Register the repo, then install the agent-side bundle:

```bash
"$RUNNER" repo add "$TARGET_REPO" --init --json
"$RUNNER" --repo "$TARGET_REPO" skills install --profile claude_code --json
```

Current Go builds do not expose `init --with-ddd-layout` or
`init --with-striatum-layout`. To place workflow files and artifacts
under the recommended committed `striatum/` tree, pass
`--scaffold-root` and `--artifact-root` to `workflow generate`.

## Author or validate a workflow

```bash
"$RUNNER" --repo "$TARGET_REPO" workflow validate "$WORKFLOW" --json
```

`workflow validate` checks required fields, role/lane references,
artifact paths, dependency edges, bounded cycles, declared
parallelism, and lane constraints. YAML files are rejected.

The canonical authoring path is `striatum workflow generate` (see
[WRITING_WORKFLOWS.md](writing-workflows.md) for the full surface and
options):

```bash
"$RUNNER" workflow templates list --kind shape --json
"$RUNNER" workflow generate \
  --shape code_change \
  --lane-set local \
  --workflow-id my-change \
  --scaffold-root striatum/workflows/my-change \
  --artifact-root striatum/my-change \
  --json
```

`--shape` selects the graph family; `--lane-set` selects the lane
topology. Preview is the default. Add `--write` to write the workflow
tree.

### Refresh existing workflow fixtures

RFC 0040 V1 bakes the "no-questions" front-matter completeness fragments into
the bundled template catalog (see
[`docs/HARNESS_FRICTION_PATTERNS.md`](../reference/harness-friction-patterns.md)).
New workflows scaffolded via `workflow generate` pick them up automatically.

The Python-era `workflow upgrade` CLI is not part of the current Go CLI. For an
existing workflow, regenerate the nearest starter into a temporary path, copy
over the wanted prompt/role fragments by hand, then validate the edited
workflow:

```bash
"$RUNNER" workflow generate \
  --shape code_change \
  --lane-set local \
  --workflow-id my-change-refresh \
  --scaffold-root /tmp/striatum-workflow-refresh \
  --artifact-root striatum/my-change \
  --write \
  --json
"$RUNNER" workflow validate path/to/workflow.json --json
```

Do not mutate a workflow that has a non-terminal run unless the operator has
decided that the active run should consume the changed file. Duplicate the
workflow if the active run should be left alone.

## Drive a dogfood through daemon MCP

The retired Python `striatum serve --web --allow-mutations` chat surface no
longer exists. Current operator agents should use daemon MCP tools directly
when a capability token and endpoint are available, with the daemon-backed CLI
commands as the compatibility fallback. The lifecycle remains the same even
though the old composite dogfood chat tool names are gone:

```bash
"$RUNNER" --repo "$TARGET_REPO" run prepare --workflow "$WORKFLOW" --json
"$RUNNER" --repo "$TARGET_REPO" run start --run-id <run_id> --json
"$RUNNER" --repo "$TARGET_REPO" register-session --run-id <run_id> --role <role> --lane <lane> --json
"$RUNNER" --repo "$TARGET_REPO" supervise start --session-id <session_id> --json
"$RUNNER" --repo "$TARGET_REPO" claim-next <session_id> --json
"$RUNNER" --repo "$TARGET_REPO" ack <session_id> <message_id> <lease_id> --json
"$RUNNER" --repo "$TARGET_REPO" publish-artifact <session_id> <job_id> <lease_id> <kind> <logical_name> <path> --json
"$RUNNER" --repo "$TARGET_REPO" complete <session_id> <job_id> <lease_id> --summary "..." --json
"$RUNNER" --repo "$TARGET_REPO" supervise stop --session-id <session_id> --reason "done" --json
"$RUNNER" --repo "$TARGET_REPO" run summary --run-id <run_id> --path "$OUTPUT_DIR/RUN_SUMMARY.md"
"$RUNNER" --repo "$TARGET_REPO" evidence export --run-id <run_id> --path "$OUTPUT_DIR/RUN_EVIDENCE.md" --json
```

Daemon MCP and the local web UI are the normal live-control surfaces. The bash
CLI remains a daemon-backed compatibility and debugging client.

For deeper authoring guidance, see
[WRITING_WORKFLOWS.md](writing-workflows.md).

To choose from the bundled generator catalog instead of a fixed starter:

```bash
"$RUNNER" workflow templates list --kind shape --json
"$RUNNER" workflow generate \
  --shape code_change \
  --lane-set local \
  --workflow-id my-change \
  --scaffold-root striatum/workflows/my-change \
  --artifact-root striatum/my-change \
  --json
```

The preview writes nothing. Add `--write` to create the workflow tree,
then validate or edit
`striatum/workflows/my-change/workflow.json` before `run prepare`.

## Prepare a run

```bash
"$RUNNER" --repo "$TARGET_REPO" run prepare --workflow "$WORKFLOW" --json
```

Copy the returned `run_id` for later commands. The run is now
prepared but not claimable.

## Confirm the branch and start

```bash
"$RUNNER" --repo "$TARGET_REPO" branch confirm \
  --run-id <run_id> \
  --branch striatum/rfc-ledger-cleanup \
  --json

"$RUNNER" --repo "$TARGET_REPO" run start --run-id <run_id> --json
```

Add `--create`, `--use-current`, or `--strict` to drive git
instead of just recording (see [SPEC.md § Branches And
Commits](../reference/spec.md#branches-and-commits)).

## Register a session

```bash
"$RUNNER" --repo "$TARGET_REPO" register-session \
  --run-id <run_id> \
  --role author \
  --lane codex \
  --capability write \
  --json
```

Copy the returned `session_id`. The display slug looks like
`author-codex-1`.

## Claim and acknowledge work

```bash
"$RUNNER" --repo "$TARGET_REPO" claim-next --session-id <session_id> --json
```

If work is available, the response includes a `packet` with
`job_id`, `message_id`, `lease_id`, expected artifacts, write
scope, task prompt, and the commands to use. Expected artifact
metadata includes a privacy-safe lowercase byline such as
`author: author-codex-gpt-5.5-001`. Put that exact line near the
top of any workflow-authored Markdown artifact.

If the work packet contains `worktree_required: true`, run the
suggested `striatum worktree create` command before publishing.

After reading the packet:

```bash
"$RUNNER" --repo "$TARGET_REPO" ack \
  --session-id <session_id> --message-id <message_id> --lease-id <lease_id> \
  --json

"$RUNNER" --repo "$TARGET_REPO" heartbeat \
  --session-id <session_id> --lease-id <lease_id> --extend-seconds 1800 --json
```

## Publish artifacts and complete non-review work

```bash
"$RUNNER" --repo "$TARGET_REPO" publish-artifact \
  --session-id <session_id> --job-id <job_id> --lease-id <lease_id> \
  --kind handoff \
  --logical-name draft \
  --path "$OUTPUT_DIR/RFC_LEDGER_DRAFT.md" \
  --json

"$RUNNER" --repo "$TARGET_REPO" complete \
  --session-id <session_id> --job-id <job_id> --lease-id <lease_id> \
  --summary "Draft artifact published." --json
```

`--path` must match the workflow's declared
`expected_artifacts[].path` for the job exactly. The publisher
refuses any path outside `write_scope.allowed_paths` with exit
code 6.

Completion may enqueue downstream jobs once dependencies are
satisfied.

## Submit review work

```bash
"$RUNNER" --repo "$TARGET_REPO" submit-review \
  --session-id <review_session_id> \
  --job-id <review_job_id> \
  --lease-id <review_lease_id> \
  --path "$OUTPUT_DIR/codex/RFC_LEDGER_REVIEW.md" \
  --verdict accept_with_findings --json
```

`--verdict` accepts `accept`, `accept_with_findings`,
`needs_revision`, or `reject`. For unusual flows, call
`publish-artifact` and `verdict` separately. In particular, when a review job
is re-claimed after its finding artifact was already published for the current
attempt, do not run `submit-review` again; use `verdict` to record the verdict
against the existing artifact.

## Record owner decisions

```bash
"$RUNNER" --repo "$TARGET_REPO" decision record \
  --run-id <run_id> \
  --path docs/decisions/owner-choice.md \
  --outcome accepted_with_follow_up \
  --title "Keep decisions as durable artifacts" \
  --follow-up "Review fuller decision schemas later." \
  --json
```

The generated Markdown includes machine-checkable front matter and
is recorded as artifact kind `decision`. `decision record` does
not require an active lease.

## Report a blocker

```bash
"$RUNNER" --repo "$TARGET_REPO" block \
  --session-id <session_id> --job-id <job_id> --lease-id <lease_id> \
  --kind missing_input \
  --severity human_checkpoint \
  --description "Need principal decision before continuing." \
  --json
```

Use `--severity blocked` for normal blockers and `human_checkpoint`
when the run needs explicit human judgment.

To resolve a `human_checkpoint` blocker explicitly once the
operator has decided, use `striatum checkpoint resolve`:

```bash
# Continue: closes the blocker and returns the affected job to the queue.
"$RUNNER" --repo "$TARGET_REPO" checkpoint resolve \
  --blocker-id <blocker_id> \
  --action continue \
  --decision-id <decision_id> \
  --json

# Cancel: closes the blocker and cancels the affected job.
"$RUNNER" --repo "$TARGET_REPO" checkpoint resolve \
  --blocker-id <blocker_id> \
  --action cancel \
  --json

# Override (revision_routing checkpoints only): accepts the
# needs_revision verdict as superseded by a recorded decision and
# makes the downstream gate reachable WITHOUT re-queueing the review.
# Requires --decision-id pointing at an accepting run-level decision.
"$RUNNER" --repo "$TARGET_REPO" checkpoint resolve \
  --blocker-id <blocker_id> \
  --action override \
  --decision-id <decision_id> \
  --json
```

`--decision-id` is optional for `continue`/`cancel` but recommended.
When present, it must reference an existing run-level decision artifact
recorded with `striatum decision record`; the resolution event payload
then links back to that artifact for audit.

`--action override` is for `revision_routing` checkpoints (opened when a
`needs_revision` verdict has no matching workflow cycle). It requires
`--decision-id` pointing at a run-level decision whose outcome is
`accepted` or `accepted_with_follow_up`. Override creates no new
authority: it completes the stalled review job and records a superseding
clearing verdict (posture `override`) so the downstream gate is reachable;
the rationale lives entirely in the referenced decision artifact. Use
`continue` instead to re-run the same review.

## Inspect, watch, and export evidence

```bash
"$RUNNER" --repo "$TARGET_REPO" status --run-id <run_id> --json
"$RUNNER" --repo "$TARGET_REPO" why <blocker_or_job_or_artifact_id> --json
"$RUNNER" --repo "$TARGET_REPO" doctor --run-id <run_id> --verbose --json
"$RUNNER" --repo "$TARGET_REPO" dashboard --run-id <run_id>           # live; --once for one frame
"$RUNNER" --repo "$TARGET_REPO" run summary --run-id <run_id> --path "$OUTPUT_DIR/RUN_SUMMARY.md"
"$RUNNER" --repo "$TARGET_REPO" evidence export \
  --run-id <run_id> --path "$OUTPUT_DIR/RUN_EVIDENCE.md" --json
```

### Inspect and export run trajectories (RFC 0080)

The run trajectory is a read-only projection of a run's history from PostgreSQL. It provides a structured timeline of events (messages, event logs, artifacts, verdicts, blockers).

Two profiles are supported:
- `dialogue`: contains only agent-authored messages and published artifacts.
- `provenance`: contains dialogue records plus system lifecycle events, verdicts, and blockers.

* **Export the trajectory** (capability: `read`):
  ```bash
  # Export dialogue timeline
  "$RUNNER" --repo "$TARGET_REPO" trajectory export <run_id> dialogue --json

  # Export system provenance timeline
  "$RUNNER" --repo "$TARGET_REPO" trajectory export <run_id> provenance --json
  ```
* **Watch/tail new trajectory events** (capability: `read`):
  ```bash
  "$RUNNER" --repo "$TARGET_REPO" trajectory watch <run_id> provenance --since-seq 25 --json
  ```

> **Note**: `trajectory export` and `trajectory watch` pull structured events from PostgreSQL. This is distinct from `supervise trajectory --session-id <session_id>`, which tails raw console output from the local PTY log scratch file.

### Inspect Dialogue and Work Packets

To inspect dialogue Q&A, transcripts, and work packets without attaching to tmux panes:

- **List and Show Interrogations**:
  ```bash
  "$RUNNER" --repo "$TARGET_REPO" interrogation list <run_id> --json
  "$RUNNER" --repo "$TARGET_REPO" interrogation show <interrogation_id> --json
  ```
- **List and Show Conversations**:
  ```bash
  "$RUNNER" --repo "$TARGET_REPO" conversation list <run_id> --json
  "$RUNNER" --repo "$TARGET_REPO" conversation show <conversation_id> --json
  ```
- **Inspect Work Packets**:
  Work packets carry task prompts, allowed paths, and metadata. By default, task prose is omitted to avoid terminal noise. Pass `--raw` to include the full `packet_json`:
  ```bash
  "$RUNNER" --repo "$TARGET_REPO" work packet-show --job-id <job_id> --raw --json
  ```

To explicitly cancel a non-terminal job (and optionally its
blocked-only-through-this dependents), use `striatum recovery
cancel-job`:

```bash
"$RUNNER" --repo "$TARGET_REPO" recovery cancel-job \
  --run-id <run_id> \
  --job-id <job_id> \
  --reason "operator chose to abandon this work" \
  --cascade \
  --json
```

Without `--cascade` the command refuses with exit code 4 if the
job has blocked dependents whose only path was through it; rerun
with `--cascade` to cancel them transitively in the same
transaction. Terminal-state jobs (`completed`, `failed`,
`canceled`, `skipped`) cannot be canceled.

For unattended runs across **multiple** registered repositories, keep
`striatumd` running as the local daemon (normally through the systemd user
unit installed by `striatum daemon install`). The resident daemon sweeps
recovery across active runs in every registered repo from one process; see
"Daemon / multi-repo coordination" below.

### Doctor triage and recovery

`striatum doctor --verbose --json` returns both the legacy
`problems` strings and structured `problem_records`. The web UI
groups those records by problem kind and links each group back to
this section, so start by reading the group name, then inspect the
record `context` for the run id, job id, lease id, blocker id, or
session id involved.

Common recovery paths are:

- `stale_queue_message_claim`, `unreaped_expired_lease`, and
  other stale-lease symptoms: run `striatum recovery stale-leases`
  first, then use `striatum recovery requeue-stale` only for
  review-only work the runner says is safe to requeue.
- `active_session_on_terminal_run`: close the session with
  `striatum session close` as described in
  [Close active sessions](#close-active-sessions).
- `open_blocker_on_terminal_run`: inspect the blocker with `why`
  and decide whether it should be resolved, canceled, or left as
  audit evidence.
- `process_*` supervisor issues: run
  `striatum recovery process-reconcile --run-id <run_id> --json`
  before requeueing anything.
- `job_stuck_no_live_session` (warning): a non-terminal job
  (`running` / `claimed` / `stale_lease` / `blocked`) has no live
  session and no recent progress — the run is wedged but quiet. The
  warning record names the `run_id`, `job_id`, `job_state`, and the
  remediation verb. For a `blocked` job whose lane finished but whose
  seal failed transiently (e.g. a `55P03` lock timeout under concurrent
  runs), run `striatum recovery reseal --run-id <run_id> --job-id
  <job_id>` to move it `blocked -> queued` on the same attempt; `run
  drive` then re-claims and re-seals it. `run retry-job` refuses this
  case (it would exceed `max_attempts`), and `run drive` reports
  `cannot advance <job> (cannot_advance_blocked): …` rather than idling
  silently.
  If `recovery reseal` repairs a job that already has an open
  `human_checkpoint`, it does not requeue the same attempt. It anchors the
  already-published artifact, releases any stray work lease, restores the job to
  `waiting_human`, and resolves derivative `immutable_artifact_conflict`
  blockers caused by a failed duplicate rerun. Continue through
  `checkpoint resolve`; its `continue` path opens the fresh attempt.

| Visible symptom | Inspect with | Recovery command |
|---|---|---|
| Stale lease or no heartbeat. | `recovery stale-leases --run-id <run_id> --json` | `recovery requeue-stale` only for review-safe or force-justified work. |
| Process exited, outputs missing, or supervisor mismatch. | `recovery process-reconcile --run-id <run_id> --json` | `recovery resume --blocker-id <id>` after the artifact/verdict issue is fixed. |
| Lane finished but the seal/publish failed (job `blocked`, work durable on disk; `run drive` says `cannot advance`). | `doctor --run-id <run_id> --verbose --json` (look for `job_stuck_no_live_session`); `why <blocker_id>`. | `recovery reseal --run-id <run_id> --job-id <job_id>` (re-seal same attempt, or restore an already-open checkpoint). For a dangling blocker no completion cleared, `recovery resolve-blocker <blocker_id>`. |
| Human checkpoint or revision-routing blocker. | `why <blocker_id> --run-id <run_id>` plus artifacts. | `decision record`, then `checkpoint resolve --blocker-id <id> --action continue\|cancel`. For a `revision_routing` checkpoint you can instead `--action override --decision-id <id>` to accept the verdict as superseded without re-running the review. |
| Escalation artifact or principal inbox item. | `inbox --json` and `escalation show --escalation-id <id> --json`. | `decision record`, then `escalation resolve --escalation-id <id> --decision-id <decision_id>`. |
| Terminal run with active sessions. | `doctor --run-id <run_id> --verbose --json`. | `session close --session-id <session_id> --reason terminal-run-cleanup`. |
| Review-panel cycle block (#222 fresh-review gate) or manual lane re-claims. | `work packet-show --job-id <job_id> --raw` | Record a scoped decision: `decision record <run_id> <path> accepted "Override claim" --subject-session-id <session_id> --subject-job-id <job_id>`, then `work claim-override <session_id> <job_id> <decision_id>`. |

For unattended recovery sweeps, the resident daemon sweeps recovery across active runs automatically (configured via the `--sweep-interval-seconds` daemon option).
To manually trigger a single recovery sweep against a specific run, use `recovery auto`:

```bash
"$RUNNER" --repo "$TARGET_REPO" recovery auto \
  --run-id <run_id> \
  --json
```

Add `--dry-run` to report sweep eligibility without side effects.

### Close active sessions

When a run is terminal but a session is still active, close the
session explicitly:

```bash
"$RUNNER" --repo "$TARGET_REPO" session close \
  --session-id <session_id> \
  --reason terminal-run-cleanup \
  --json
```

Closing a session records the lifecycle transition; it does not
delete artifacts, verdicts, or events. If `doctor` reports several
active sessions on terminal runs, close each listed `session_id`.

## Drive or inspect peer communication (conversations and interrogations)

Active runs can have live peer communication between agent sessions. As a human principal or operator, you can list, inspect, or close these communications. If needed, you can use the operator override to answer an interrogation on behalf of a lane.

### List and inspect conversations
* List all conversations in a run:
  ```bash
  "$RUNNER" --repo "$TARGET_REPO" conversation list <run_id> --json
  ```
* View conversation metadata and the full transcript:
  ```bash
  "$RUNNER" --repo "$TARGET_REPO" conversation show <conversation_id> --json
  ```

### List and inspect interrogations
* List all interrogations in a run:
  ```bash
  "$RUNNER" --repo "$TARGET_REPO" interrogation list <run_id> --json
  ```
* View interrogation metadata and transcript:
  ```bash
  "$RUNNER" --repo "$TARGET_REPO" interrogation show <interrogation_id> --json
  ```

### Operator override for interrogation
If a lane is stuck or offline and you must answer an interrogation on its behalf, you can answer the interrogation from an admin/operator session. The reply is recorded in the transcript with honest provenance (`responder: operator`):
```bash
"$RUNNER" --repo "$TARGET_REPO" interrogation answer <operator_session_id> <interrogation_id> --body "Here is the override answer." --json
```

## Daemon / multi-repo coordination (RFC 0028 V1)

`striatum daemon` is now the required local control plane for every
workflow-state verb. It owns the PostgreSQL substrate for registered
target repositories, exposes daemon RPC to CLI/MCP/web clients, runs
recovery sweeps, and enforces capability-scoped access. `.striatum/`
next to a target repo is operational scratch only.

Start the daemon and register two target repos:

```bash
# Render and start the systemd user unit. On non-systemd hosts, run
# `striatumd -socket "$XDG_RUNTIME_DIR/striatum/daemon-go.sock"` directly.
"$RUNNER" daemon install
"$RUNNER" daemon status

# Register repos. `striatumd` bootstraps one admin token on first startup
# and writes an owner-only client-token file under the runtime directory;
# treat that file as degraded storage compared with an OS keyring.
"$RUNNER" repo add /path/to/repo-a --init --json
"$RUNNER" repo add /path/to/repo-b --init --json   # repeat per repo

"$RUNNER" repo list --json
```

`repo add` is admin-gated. It canonicalizes the repository
root, refuses symlink/path-traversal ambiguity, derives a
realpath/inode-based repository identity, and refuses active
path re-occupation by a different identity. Pass `--init`
when no `.striatum/` directory exists; it creates operational
scratch only and does not create `.striatum/retired-local-state`. If a
pre-D094 repo-local SQLite source exists, registration refuses and
points at archive/remove guidance; writable SQLite imports are retired.

`repo remove <id>` is idempotent, revokes live repo-scoped
capabilities, preserves audit rows, and never reuses
`repository_id` (re-adding allocates a fresh id).

Read across registered repos:

```bash
"$RUNNER" status --json
"$RUNNER" doctor --json
"$RUNNER" why <job-or-blocker-id> --json
"$RUNNER" dashboard --all
```

Mapped CLI verbs route through the daemon RPC envelope under the
appropriate token/capability. The V1 `--no-daemon` flag is retired
(D094 / RFC 0043); parsing it returns the standard argparse
"unrecognized arguments" error. A missing daemon refuses with exit
code 11 (`daemon_unreachable`); an unregistered repository or a
repository with legacy SQLite state refuses with exit code 12
(`repo_not_migrated`).

`dashboard --all` fans out across daemon-registered repositories and
requires `read` capability.

Audit shape:

- Audit rows are metadata-only: command, authorization
  result, client/repository ids when known, payload hash,
  and a continuous hash chain across retained rows.
- Closed segment manifests are SQL-guarded against
  daemon-API rewrites and checked by `striatum doctor`.
- Audit deliberately excludes transcripts, request/response
  bodies, artifact text, blocker prose, token secrets, and
  tracebacks. It is per-machine daemon evidence, not
  transcript evidence or authorship proof.

What daemon mode does **not** ship today:

- It does not ship Windows daemon support, sealed-apply
  authority owning hosted semantics, or remote/network-accessible serving.
- It does not bundle PostgreSQL; operators install and own the
  Postgres service. Bundled, embedded, and Dockerized
  distributions are deferred (RFC 0033 §8, inherited by RFC 0043).
- Legacy SQLite is not a production fallback or operator migration source. It
  remains only as historical tombstones, golden fixtures, or explicitly gated
  subprocess compatibility paths.

## Cross-repo workflow foundation

RFC 0032 adds the V2 foundation for local cross-repo workflows on the
daemon PostgreSQL substrate. A cross-repo workflow declares at least two
registered repositories:

```json
{
  "repositories": {
    "primary": {"repo_id": "repo_primary"},
    "consumer": {"repo_id": "repo_consumer"}
  },
  "primary_repository": "primary"
}
```

Every job in that workflow declares its `repository` alias explicitly.
Artifact paths and write scopes are interpreted relative to that job's
target repo. The daemon DB owns the `cross_repo_run_id`; each
participant repo's workflow rows live in daemon Postgres under that
repo's `repository_id` and record a `runs.cross_repo_run_id`
back-reference.

Operator inspection commands:

```bash
"$RUNNER" cross-repo list --json
"$RUNNER" cross-repo describe <cross_repo_run_id> --json
"$RUNNER" cross-repo why <cross_repo_run_id> --json
"$RUNNER" cross-repo cancel <cross_repo_run_id> --reason "<why>" --json
```

`cross-repo cancel` is the recovery cancel path for cross-repo runs. It routes
through daemon RPC, cancels non-terminal participant runs via the PG-native
participant runner, skips terminal participants, and returns `blocked` with
participant diagnostics when any repository cannot be canceled.

If a participant repository disappears mid-run, the daemon pauses the
cross-repo run and the operator must re-register the same repository id or run
`striatum cross-repo cancel <cross_repo_run_id> --reason "<why>"`. If cancel
returns `blocked`, inspect participant states, repair or re-register the
unreachable repository, then retry cancel or cancel the affected participant
manually. Cross-repo coordination is best-effort local orchestration, not
atomic file mutation across repositories.

Daemon MCP mutation tools follow least privilege. A read-only token sees
only read tools. Mutation grants (`write`, `review`, `claim`, `apply`,
`recovery`, or `admin`) must be granted deliberately, should usually be
short-lived, and are re-checked on every `tools/call`. Prompt-injected
tool arguments cannot escalate beyond the token's grants.

### Mint a capability token (`apply`, `recovery`, …)

`striatumd` bootstraps a single admin client token and writes it to the
owner-only client-token file under the runtime directory. Current bootstrap
also grants `apply` (and every other capability) to that token, so
`run integrate` (capability `apply`) works out of the box. But a token minted
by an **older** daemon may carry `admin` without `apply`: `branch confirm`
(admin) succeeds while `run integrate` fails closed with `capability_missing`.
The refusal now names the missing capability, the method, and this
remediation.

Mint a fresh token that carries the capability the error names with the
admin-gated `daemon token-create` verb (RPC `daemon.token.create`,
daemon-global):

```bash
# Apply-capable token so `run integrate` can land a completed run worktree.
"$RUNNER" daemon token-create --capability apply --display-name operator-apply --json

# Multiple grants in one token (repeat --capability).
"$RUNNER" daemon token-create \
  --capability apply --capability recovery \
  --display-name operator-ops --json
```

The cleartext token is returned once in the response. Supply it to subsequent
CLI verbs with `--capability-token <token>` (or `--capability-token-file`), or
write it to the runtime client-token file to replace the bootstrap token. Keep
it short-lived; pass `--expires-in <duration>` to bound its lifetime.

If you cannot mint an apply-capable token (no admin access, or the daemon is
unavailable) and you must land a completed run, the campaign fallback is a
strict fast-forward of the run branch — never a conflict resolution:

```bash
# The run branch must be strictly ahead of the integration target.
git merge --ff-only <run-branch>
```

Use `--ff-only` only when the merge is a zero-conflict fast-forward; if git
refuses, do not hand-resolve — mint an apply token and use the serialized
`run integrate` instead. Record the manual fast-forward in the target repo's
commit message so the provenance trail stays honest.

For chat-assisted workflow generation over the daemon-mounted web service, set
`STRIATUM_DAEMON_WEB_ALLOW_MUTATIONS=1` on the daemon process before startup
only when you want the web surface to write generated workflow files.
`POST /workflows/generate/preview` remains read-only; `POST
/workflows/generate` fails closed unless web mutations are enabled.
- It is not a replacement for daemon sweeping, which handles
  automated recovery sweeps across registered repositories.

### Daemon storage substrate (RFC 0033 + D094 / RFC 0043)

RFC 0033 put daemon-global state on operator-installed system
PostgreSQL. D094 / RFC 0043 then moves per-repository workflow
state — runs, jobs, sessions, queue messages, leases, artifacts,
verdicts, blockers, worktrees, process supervisors, and repo-local
events — into the same daemon-owned Postgres under a
`repository_id` scope. The daemon is a hard prerequisite for every
Striatum verb; the V1 `--no-daemon` direct-CLI path is retired and
parsing the flag returns the standard argparse "unrecognized
arguments" error. See
[postgres-transition.md](postgres-transition.md) for the full
runbook.

The operator provides PostgreSQL. Striatum connects through
`STRIATUM_DAEMON_DB_URL`, `~/.config/striatum/daemon.toml`, or an
explicit `--postgres-url` client surface; the daemon owns schema
migrations and roles, but it does not install, start, stop, or
upgrade PostgreSQL. Bundled, embedded, and Dockerized Postgres
distributions are deferred.

RFC 0048 completed in v1.55.0: production mapped verbs are
daemon/Postgres-backed and fail closed without daemon authority.
The paired `STRIATUM_DAEMON_REQUIRED=0 STRIATUM_TEST_HARNESS=1`
escape is for subprocess compatibility fixtures only.

The old SQLite cutover commands are fully removed.
`daemon migrate` and `daemon migrate-repo-local` are no longer parseable
compatibility spellings. To inspect current daemon reachability,
registration, and runtime posture, use:

```bash
"$RUNNER" daemon status
"$RUNNER" --repo "$TARGET_REPO" doctor --verbose --json
```

CLI verbs against an unregistered repo refuse with exit code 12
(`repo_not_migrated`) and tell the operator to archive/remove legacy SQLite
files before registering; CLI verbs without a reachable daemon refuse with
exit code 11 (`daemon_unreachable`). See
[postgres-transition.md](postgres-transition.md) for the full
runbook and rollback notes.

RFC 0030/0031 add the daemon V2 RPC and supervision/apply foundation on
top of this storage substrate. The daemon RPC envelope is versioned JSON;
`daemon.hello` / `daemon.welcome` negotiate envelope and framing, and
`daemon.describe` publishes the capability-bound method registry. Version
or framing incompatibility refuses with exit code 10 and does not silently
fall back to direct mode.

Daemon-owned supervision is represented by daemon DB supervisor rows and
per-repository pointer state under the registered repository scope. The
`striatum supervise` compatibility verbs remain CLI clients of the daemon
surface, and daemon-routed `supervise.*` calls use the same packet/FIFO
and lane-attestation invariants.

Reviewed-patch apply is intentionally absent from the production daemon RPC
contract. Per D112, stale direct calls to `apply.reviewed_patch` return and
audit as `method_unknown`. Apply receipts remain readable/verifiable as
daemon-owned evidence helpers; a receipt is an AI guardrail, not proof of
model-token authorship or resistance to a malicious local operator with
filesystem or database access.

The Go daemon's current signing-key path is the local Ed25519 fallback
file: `STRIATUM_DAEMON_SIGNING_KEY_PATH` when set, otherwise
`$XDG_STATE_HOME/striatum/daemon/signing_key`. `daemon.key.rotate` writes
that file with `0600` permissions and `daemon.hello` advertises the public
key when the file is loadable. Rotation preserves malformed private fallback
files as `.invalid.<timestamp>` backups, but over-permissive key files still
fail closed. OS keyring custody and the full reviewed-patch apply gate remain
deferred.

### Go daemon port notes (RFC 0039 / RFC 0068)

> Status: current production daemon path. D109 made the Go daemon the default,
> D111 retired the Python daemon selector, and D112 removed
> `apply.reviewed_patch` from the production daemon RPC contract.
> `striatumd` is the Go daemon; `striatum daemon install` renders the service
> unit and `systemctl --user start|stop|restart striatumd` manages it.

RFC 0039 produced a Go `go/cmd/striatumd` prototype that speaks the
RFC 0030 envelope-v1 wire protocol over the RFC 0033 PostgreSQL
substrate. The D105 Python-primary constraint was superseded by D107; active
contract methods now have Go handlers. The Python daemon module is deleted;
the Python MCP wrapper is deleted; the legacy local-state package/facades and
fixtures are deleted. This is no longer a production reviewed-patch RPC blocker
or operator import-window concern.

Build the binary from a contributor checkout:

```bash
make -C go build
ls go/bin/striatumd
```

The build requires Go 1.23+ and the system `make`. The root `make install`
target copies the built Go binaries into `$(PREFIX)/bin`; release archives are
built with `make release-archives`.

Run it directly for developer inspection:

```bash
./go/bin/striatumd \
  --socket "${XDG_RUNTIME_DIR:-/tmp}/striatum/daemon.sock" \
  --postgres-url "$STRIATUM_DAEMON_DB_URL" \
  --migrations-sha-source go/pkg/db/sql
```

`daemon.describe` exposes the supported schema, migration count, and method
etag. A missing or generic `not_implemented` active handler is an RFC 0068
regression, not accepted product behavior.

Coexistence rule: only one daemon may own the PostgreSQL substrate at a
time. Stop any old transitional Python daemon before starting current
Striatum. The Go binary refuses to start with exit code 14
`daemon_already_running` when it detects another `striatumd-*`
connection in `pg_stat_activity`.

The RFC 0035 multi-repo test harness now targets the Go daemon. The
`daemon_core` parameter remains only as a deprecated compatibility seam and
accepts `go`:

```python
from _harness.multi_repo import MultiRepoHarness

harness = MultiRepoHarness(daemon_pg_url=...)

# The harness invokes `make -C go build` if the binary is missing.
harness = MultiRepoHarness(daemon_pg_url=..., daemon_core="go")
```

## Web UI

The local web service is mounted by `striatumd` on the daemon's loopback HTTP
listener. Discover the base URL from the daemon endpoint file and strip the
MCP suffix:

```bash
BASE_URL=$(sed 's#/mcp$##' "${XDG_RUNTIME_DIR}/striatum/mcp-http-endpoint")
TOKEN=$(cat "${XDG_RUNTIME_DIR}/striatum/client-token")
curl -H "Authorization: Bearer ${TOKEN}" "${BASE_URL}/v1/health"
```

The loopback service requires `Authorization: Bearer <client-token>`, and web
mutations are disabled unless `STRIATUM_DAEMON_WEB_ALLOW_MUTATIONS=1` was set
on the daemon process before startup. For browser access without bearer-header
tooling, use the read-only tailnet identity listener:

```bash
STRIATUM_DAEMON_WEB_TAILSCALE=1 systemctl --user restart striatumd
tailscale serve --bg unix:${XDG_RUNTIME_DIR}/striatum/web-ui.sock
```

The Go web surface currently exposes daemon-backed run/status JSON and a small
server-rendered run list/status page. Important routes: `/` for the run list,
`/run?run_id=<id>` for a run status page, `/v1/runs/<run_id>/events` for SSE,
`/v1/runs/<run_id>/dashboard` for the dashboard DTO, `/v1/runs/<run_id>/why`
for explanations, `/v1/artifacts/<id>/raw` for raw artifacts, and
`/workflow-templates` / `/workflows/generate` for generator surfaces.

Retired Python-era pages such as `/view/`, `/workflows/new`,
`/workflows/edit/<path>`, `/chat`, and `/doctor` are no longer advertised as
current Go routes. Use the CLI or daemon MCP for those workflows until a new Go
route is documented.

## Dashboards and graphs

For a compact at-a-glance view of a run, use the dashboard. It is
a dependency-free terminal renderer over the same daemon-owned
PostgreSQL state that `status` exposes:

```bash
"$RUNNER" --repo "$TARGET_REPO" dashboard --run-id <run_id>
```

The dashboard refreshes every 2 seconds by default and clears the
screen between frames. It shows run state and branch, job counts
by state, verdict counts, open blockers (including human
checkpoints), claimable work grouped by role/lane, deterministic
next actions, and the most recent events. Use `Ctrl-C` to quit.

Useful flags:

- `--refresh <seconds>`: change the refresh cadence.
- `--once`: render a single frame to stdout and exit. Handy in
  scripts and CI assertions where a redrawing TUI is not what you
  want.
- `--graph` / `--no-graph`: force the layered dependency-graph
  panel on or off. Default is auto: rendered when the terminal is
  at least 100 columns wide and 30 lines tall and the workflow has
  at least one edge.
- `--graph-only`: hide the rest of the frame and show only the
  graph.
- `--graph-style {auto,layered,list,fancy}`: pick a layout.
  `fancy` uses Unicode box-drawing characters (`┌`, `┐`, `└`,
  `┘`, `─`, `│`); falls back to `layered` ASCII when the
  per-slot width drops below 14 columns.
- `--graph-orient {tb,lr}`: top-to-bottom (default) or
  left-to-right. LR arranges layers as columns instead of
  rows; useful for long workflow chains. Falls back to TB when
  too many layers don't fit horizontally.
- `--graph-no-cycles`: suppress dashed `~~>` back-edges for
  revision cycles (or `╌╌▶` in fancy mode).

For a one-shot snapshot outside the dashboard, use
`striatum run graph --run-id <id> --format ascii`; it reuses the
same renderer and accepts the same `--graph-style` and
`--graph-orient` flags.

To publish a redacted run snapshot:

```bash
"$RUNNER" --repo "$TARGET_REPO" evidence export \
  --run-id <run_id> \
  --path "$OUTPUT_DIR/RUN_EVIDENCE.md" \
  --json
```

The export path must be inside the repository and outside
`.striatum/`.

## Where artifacts land

striatum has no built-in concept of a "default output
directory." Every output path is named in the workflow file:

- Each job's `expected_artifacts[].path` is the *exact*
  repo-relative path the agent must write and the publisher later
  validates and records.
- Each job's `write_scope.allowed_paths` is the *set of
  prefixes* the agent may write inside.
- `evidence export` and `run summary` use the path you pass on
  the command line; they have to be inside the repo and outside
  `.striatum/`.

So if you don't like where a workflow's artifacts land, the fix
is in the *workflow file*, not the runner.

### Recommended layout for first-contact use

If you are trying striatum on a real repo and want to keep its
output corralled (so you can `rm -rf` it cleanly without
disturbing the rest of the tree), put everything under a
top-level `striatum/` directory — sibling to the runner's
`.striatum/` operational scratch directory but checked in:

```text
<your-repo>/
├── .striatum/                 # gitignored operational scratch (FIFOs, pidfiles, token cache)
│   ├── scratch/
│   └── bin/
├── striatum/                  # checked-in workflow output (parallel name)
│   └── <run-slug>/
│       ├── RUN_SUMMARY.md
│       ├── RUN_EVIDENCE.md
│       ├── <draft>.md         # build / synthesis artifacts
│       ├── <reviewer>/        # one subdir per reviewer lane
│       │   └── <review>.md
│       └── final/
│           └── <final-review>.md
└── workflow.json              # the workflow itself can live anywhere;
                               # the example fixtures put it under
                               # examples/<slug>/ in the runner repo
```

The directory name `striatum/` is just a convention — pick
whatever you like in your workflow's `allowed_paths`. The
parallel naming (`.striatum/` for operational scratch,
`striatum/` for durable output) is a clean visual reminder that:

- `.striatum/` is **not** committed (gitignored by `init`); it's
  operational scratch. Authoritative workflow state lives in the
  daemon-owned PostgreSQL instance, not in this directory.
- `striatum/` **is** committed; it's the durable provenance the
  runner produces.

### Adapting an example workflow

To use this layout with the bundled example, edit
`examples/rfc-ledger-cleanup/workflow.json` and change every
`docs/reviews/rfc-ledger/...` path under `expected_artifacts`
and `write_scope.allowed_paths` to `striatum/rfc-ledger/...`.
Then re-run `striatum workflow validate` to confirm the edits
parse cleanly. Better yet, copy the example into your target
tree first so the bundled fixture stays untouched:

```bash
mkdir -p striatum/rfc-ledger
cp -r path/to/striatum/examples/rfc-ledger-cleanup .
sed -i 's|docs/reviews/rfc-ledger|striatum/rfc-ledger|g' \
    rfc-ledger-cleanup/workflow.json
"$RUNNER" --repo "$TARGET_REPO" workflow validate \
    rfc-ledger-cleanup/workflow.json --json
```

For new workflows, see
[WRITING_WORKFLOWS.md § "Recommended output layout"](writing-workflows.md#recommended-output-layout).

## Optional: export a corpus bundle for an external memory consumer

`striatum corpus export --since <ref> --out <dir>` (RFC 0044 V1) emits a
redacted JSONL bundle of Striatum's durable provenance — RFCs,
decision-log rows, operator reports, run summaries, audit-chain entries,
changelog entries, ubiquitous-language terms, harness-friction patterns,
and recent commits — plus a verifying `manifest.json`. This is an
*optional, post-run maintenance step*. It does not modify live state,
does not write under `.striatum/`, and is never required for any
workflow command to succeed.

```bash
"$RUNNER" --repo "$TARGET_REPO" corpus export \
    --since "$(git -C "$TARGET_REPO" merge-base origin/main HEAD)" \
    --out  "$TARGET_REPO/striatum-corpus-bundle"
```

The bundle is durable, replay-stable provenance: re-running over
unchanged inputs produces byte-identical JSONL files and identical
per-file SHA-256s (only `generated_at` varies). An optional retrieval
consumer (Engram is the first reference under RFC 0044) may ingest the
bundle locally and serve search over it; Striatum does not call the
consumer at runtime and continues to run unchanged when no consumer is
configured. The V2 contract decisions (multi-corpus identity,
redaction-tier metadata, incremental watermarks, optional workflow-level
context-injection policy) are scoped by
[RFC 0057](../rfcs/0057-corpus-contract-v2.md).

## See also

- **[CLI_REFERENCE.md](../reference/cli-reference.md)** — every verb in one
  flat list with stable exit codes.
- **[WRITING_WORKFLOWS.md](writing-workflows.md)** — author your
  own `workflow.json`.
- **[SPEC.md](../reference/spec.md)** — the implementation contract; the
  source of truth when this doc and the runner disagree.
