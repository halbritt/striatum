# RFC 0040: MCP-Driven Dogfood Harness for Operator Sessions

Status: accepted (V1)
Date: 2026-05-13
Landed: 2026-05-12 (v1.29.0; D093). V1 ships the operator-side slice: the
twelve dogfood-lifecycle chat-tool entries, the per-model harness-profile
fragments in the bundled template catalog, generator enrichment by default,
and `striatum workflow upgrade`. The composite tools
(`dogfood.publish_on_behalf`, `dogfood.surgical_recovery`) and the daemon-
side supervised-progress heartbeat were scoped to the systems half and landed
under the same RFC; D110 later removed the SQLite-bound composite tools from
the production daemon contract, so operators now use primitive daemon methods
until a PostgreSQL-native composite is accepted. See
[`docs/HARNESS_FRICTION_PATTERNS.md`](../reference/harness-friction-patterns.md)
for the long-form record of the four observed friction patterns and the
fixes that landed.
Context:
[`RFC 0028`](0028-long-running-daemon-and-multi-repository-control-plane.md),
[`RFC 0030`](0030-daemon-rpc-server-and-version-skew-protocol.md),
[`RFC 0031`](0031-daemon-owned-supervision-and-sealed-apply-boundary.md),
[`RFC 0032`](0032-cross-repo-workflows-and-mcp-mutation-capabilities.md),
[`RFC 0034`](0034-workflow-generator-and-template-catalog.md),
[`RFC 0036`](0036-mcp-harness-for-daemon-v2-mutation-surface.md),
[`docs/decisions/decision-log.md`](../decisions/decision-log.md) (D087, D088, D090),
historical dogfood source paths `docs/dogfood/036/OPERATOR_REPORT.md`,
`docs/dogfood/037/OPERATOR_REPORT.md`,
`docs/dogfood/038/OPERATOR_REPORT.md`, and
`docs/dogfood/039/OPERATOR_REPORT.md`

## Problem

Driving a dogfood end-to-end currently takes ~20-30 bash CLI invocations
from the operator's AI session (this Claude Code instance, in current
practice) spread across a 1-2 hour run:

- `striatum run prepare` + `run start`
- `register-session` × 3-5 (one per lane, per phase)
- `supervise start` × 3-5
- `claim-next` × 3-5 (one per packet delivery)
- `ack` + `publish-artifact` + `verdict` + `complete` × 3-7 (operator-on-behalf publishes when claude/gemini deny ack from supervised wrappers)
- `recovery stale-leases` / `requeue-stale` / SQL surgery × occasionally (when codex lease expires under active load)
- `run summary` + `evidence export` at the end
- `supervise stop` × N at the end

Each invocation requires the operator to copy session IDs, lease IDs,
and queue message IDs from earlier output. The pattern recurs every
~30 minutes across the run and is mechanical enough that the operator
has been doing the same shape on autopilot across dogfoods 031 through
039.

Three specific friction concentrations stand out:

1. **Publish-on-behalf for permission-gate-denied lanes.** Claude_code's
   supervised `--print` denies `striatum ack` on every artifact in every
   dogfood since 031. At the time, the operator looked up active lease and
   claimed message IDs through direct state inspection, then ran ack +
   publish-artifact + (for review jobs) verdict + complete. This has
   happened >20 times.
   Same shape for gemini when it writes artifacts but doesn't call ack.
2. **Surgical recovery under active load.** Per dogfood-038 OPERATOR_REPORT
   intervention #5, when codex's lease expires while `make test` is still
   running, `recovery requeue-stale` refuses repo-write jobs as policy.
   The operator used unsupported direct state mutation to reactivate the
   lease + supervisor + job state, then ran publish-artifact + complete.
   This has now happened twice with the same shape.
3. **Front-matter shape correction.** Per dogfood-038 intervention #3 +
   dogfood-039 intervention #2: gemini writes finding artifacts with
   front-matter shape errors (missing `artifact_kind`, wrong tag values,
   author byline inside the block instead of after it). The operator
   hand-edits the front matter to schema-correct shape, then publishes.

All three concentrations are mechanical. The RFC 0030 RPC method
registry already exposes the underlying state-transition verbs as
capability-gated routes. RFC 0032 V2 (shipped v1.24.0) wired MCP
`tools/call` + `tools/list` filtering. RFC 0036 V1 (shipped v1.26.0)
proved the chat-tool pattern works for `generate_workflow_preview` /
`generate_workflow_write`. The dogfood-lifecycle verbs are the same
shape: capability-gated, default-deny, audit row appended for every
mutating call.

What's missing is the **MCP exposure of the dogfood-lifecycle verbs**
so the operator's AI session can call them as structured tools instead
of via bash CLI with hand-copied IDs. Plus a small set of
**capability-gated convenience tools** that compose the common
publish-on-behalf + surgical-recovery sequences into single calls.

Three supervised-model friction patterns also recur across dogfoods.
These are the "harness improvement" side of the same problem:

4. **Supervised-progress lease heartbeat** (from dogfood-038
   intervention #5). When codex makes forward progress (file writes,
   command executions visible in the supervised log) the lease should
   refresh; today it expires at the documented 30-minute window even
   while codex is actively working. Independent of the operator-side
   harness, this needs daemon-side work.
5. **No-questions profile fragment** (from dogfood-037 intervention
   #5). Supervised models (claude, gemini, codex) all run in a one-shot
   invocation; asking the operator a follow-up question and exiting is
   a friction shape that already wasted three full runs. The harness
   profile prompt fragment should say "this is one-shot; do not ask
   the operator a question, write the artifact".
6. **Front-matter completeness validation** (from dogfood-039
   intervention #2). The gemini prompt fragment for finding artifacts
   should list ALL five required front-matter fields with a "none are
   optional" callout.

Items #1-#3 are operator-side harness improvements (MCP tools the
operator's AI session calls). Items #4-#6 are supervised-side harness
improvements (daemon + prompt-fragment changes). The RFC scopes both
under one umbrella because they share a common goal: reduce the
mechanical friction recurring across every dogfood.

## Goals

- Expose the dogfood-lifecycle verbs as MCP tools so the operator's AI
  session calls them via structured `tools/call` instead of bash CLI
  with hand-copied IDs.
- Add a capability-gated `dogfood.publish_on_behalf` composite tool
  that handles the routine ack-denied case in a single call.
- Add a capability-gated `dogfood.surgical_recovery` composite tool
  that handles the lease-expired-under-active-load case in a single
  call, with an explicit operator-reason field that lands in the audit
  chain.
- Add daemon-side supervised-progress lease heartbeat that refreshes
  the lease when the supervised wrapper logs forward progress.
- Update the harness profiles (codex / claude_code / gemini) with a
  "no-questions" fragment in each `native_delegation.instruction`.
- Update the gemini profile + reviewer-role doc with a
  "front-matter completeness" callout listing all five required fields.

## Non-Goals

- Replacing the bash CLI for non-dogfood use cases. The CLI stays as
  the canonical operator surface; the MCP exposure is in addition to,
  not instead of.
- Auto-driving dogfoods without operator oversight. The MCP tools
  require capability tokens; the operator session still decides what
  to call when.
- Hosted-mode multi-tenant MCP. Single-user, owner-only per D083.
- New CLI verbs. Everything new is MCP-callable; the CLI surface
  stays the same.
- Removing operator-on-behalf publishes. Those still happen via the
  new MCP composite tool; this RFC just removes the SQL hand-querying.
- Self-healing supervised wrappers. Models that ask questions and
  exit can be improved with prompt fragments (item #5) but the wrapper
  itself stays single-shot.
- Backporting these improvements to the V1 daemon vs the planned Go
  daemon (RFC 0039). This was originally scoped as Python-daemon work;
  D107/D111 now make Go the production daemon core while Python may
  remain as CLI/web client code.

## External Prior Art

- **Kubernetes `kubectl` + the API server** — operator typing
  `kubectl get pods` is bash CLI; the same operations are MCP-tool-
  equivalent via the Kubernetes API for any client. Striatum's
  RFC 0030 envelope-v1 over MCP `tools/call` matches this shape.
- **Cron-job-as-MCP-tool patterns** — recurring mechanical operations
  become tool calls rather than scripts. This RFC adopts the same
  pattern for dogfood-lifecycle operations.
- **GitOps reconciliation loops** — automated state transitions
  authorized by tokens, with audit trails. Striatum's audit chain
  + capability tokens provide the same guarantees.

## Proposal

### 1. Expand the MCP tool registry with dogfood-lifecycle verbs

The RFC 0030 method registry already lists the RPC routes:
`run.prepare`, `run.start`, `register-session`, `supervise.start`,
`claim-next`, `ack`, `publish-artifact`, `verdict`, `complete`,
`run.summary`, `evidence.export`. Each has a required capability.

RFC 0040 wires each of these as a chat-tool entry in the RFC 0023
V1.5 closed set (extending what RFC 0036 V1 did for
`generate_workflow_preview` + `generate_workflow_write`). The tool
spec for each is a thin shell over the existing RPC method:

```text
run.prepare(workflow_path: str) -> RunPrepareResult
run.start(run_id: str) -> RunStartResult
register_session(run_id: str, role: str, lane: str, fresh: bool=true) -> SessionResult
supervise_start(session_id: str) -> SupervisorResult
claim_next(session_id: str) -> ClaimResult
ack(session_id: str, message_id: str, lease_id: str) -> AckResult
publish_artifact(session_id: str, job_id: str, lease_id: str, kind: str, logical_name: str, path: str) -> ArtifactResult
verdict(session_id: str, job_id: str, lease_id: str, verdict: str, findings_artifact_id: str, rationale: str) -> VerdictResult
complete(session_id: str, job_id: str, lease_id: str, summary: str) -> CompleteResult
run_summary(run_id: str, path: str) -> ExportResult
evidence_export(run_id: str, path: str) -> ExportResult
supervise_stop(session_id: str, reason: str) -> StopResult
```

Each tool requires the existing capability bound to its RPC method.
Audit row appended per call (allowed or denied). `tools/list` filters
by token capability.

### 2. New composite tool: `dogfood.publish_on_behalf`

When a supervised lane writes an artifact but denies `striatum ack`
from inside its supervised wrapper, the operator currently:

1. Looks up the active lease for the session through daemon/run state.
2. Looks up the claimed-but-unacked queue message for the job.
3. Runs `ack` + `publish-artifact` + (if review) `verdict` + `complete`.

Replace with a single composite tool:

```text
dogfood.publish_on_behalf(
  session_id: str,
  artifact_path: str,
  artifact_kind: str,
  logical_name: str,
  # Optional for review jobs:
  verdict: str | None = None,
  findings_artifact_id: str | None = None,
  verdict_rationale: str | None = None,
  # Always:
  reason: str,           # e.g. "claude --print denied ack"
) -> PublishOnBehalfResult
```

The daemon does the lookup + the sequence internally. The audit chain
records a single composite operation with the operator's `reason` as
metadata. Capability required: same as the underlying `publish-artifact` +
`complete`/`verdict` chain (`write` or `review`).

### 3. New composite tool: `dogfood.surgical_recovery`

Per dogfood-038 intervention #5: when a repo-write job's lease expires
while the model is still making forward progress (codex mid-`make
test`), `recovery requeue-stale` refuses. The operator previously used
unsupported direct state mutation to reactivate the lease + supervisor +
job state.

Replace with a single composite tool:

```text
dogfood.surgical_recovery(
  job_id: str,
  reason: str,           # operator inspection note: "BUILD_HANDOFF.md present, tests passed, ready to publish"
  extend_lease_seconds: int = 900,
) -> SurgicalRecoveryResult
```

The daemon validates the operator inspection (job has produced
expected artifacts on disk, no concurrent supervisor) and atomically:

1. Reactivates the expired lease with a new `expires_at`.
2. Updates the supervisor row from `lost` back to `attached` (for
   attestation derivation).
3. Updates the queue message + job state back to the post-ack-pre-
   complete state.

The audit chain records this with the operator's `reason` as
metadata. Capability required: new `surgical_recovery` (admin-bound,
emitted via short-lived token only).

### 4. Daemon-side supervised-progress lease heartbeat

Per dogfood-038 intervention #5 (root cause): the supervised wrapper
heartbeats the lease at the lease level, but when codex takes over
the supervised stdin loop and goes heads-down on a long-running task,
the wrapper's heartbeat doesn't fire because the wrapper is also
blocked on stdin.

Fix: the daemon supervises lane progress at the file-system level. When
the supervised wrapper's stdout log file (`<scratch>/codex-logs/
packet-NNNN.log`) shows growth in the last N seconds, the daemon
refreshes the associated lease automatically.

Implementation:

- Daemon owns a `supervised_progress_watcher` background task per
  supervisor.
- The watcher checks `os.stat(log_path).st_mtime` every 30s.
- If mtime is within the last 60s (configurable), the daemon
  internally calls `heartbeat` on the supervisor's session's active
  lease.
- If mtime hasn't changed for > `idle_threshold_seconds` (default 600,
  configurable), the watcher logs a warning and lets the normal lease
  expiry path fire.

This is daemon-side machinery; no model-side change. It was initially
specified against the Python daemon and later carried into the Go
production daemon target per D107/D111.

### 5. Per-model "no-questions" harness profile fragment

Update `harness_profiles` in every workflow.json scaffolded by
`workflow init` / `workflow generate` (RFC 0034 V1 generator):

For codex:
```
"native_delegation": {
  "mode": "encouraged",
  "instruction": "Use native delegation aggressively for parallelizable work. The parent session remains accountable for Striatum artifacts, write scope, verification, and completion. **This is a one-shot supervised invocation: you cannot ask the operator a follow-up question. If a step is ambiguous, choose the most-conservative default that matches the synthesis and proceed; the operator-on-behalf publishes the result if your CLI access is denied.**"
}
```

Same shape for claude_code and gemini, each with the bold instruction.
The RFC 0034 V1 workflow_generator picks up the fragment from the
generator's catalog metadata; existing workflows update via a one-pass
backport.

### 6. Per-model front-matter completeness callout

For gemini specifically (since dogfood-038/039 friction was gemini-
specific): update the gemini `native_delegation.instruction` to include:

```
"When writing finding artifacts, ALL FIVE front-matter fields are required (none are optional): schema_version, artifact_kind, verdict_intent, severity, tags. Use verdict_intent (not verdict); severity from {low,medium,high,critical} (not none); tags as a JSON array; the author: byline is a plain markdown line AFTER the front-matter block, not a key inside it. Handoff artifacts (DESIGN.md / BUILD_HANDOFF.md) do not need front matter; just the plain author: byline."
```

Update the standard reviewer-role template under
`src/striatum/skills/templates/*/recover.md.tmpl` and the equivalent
prompt-fragment locations so any new dogfood scaffolded via the RFC
0034 V1 generator picks up the corrected guidance.

### 7. Documentation

- `docs/MCP.md` — new section "Dogfood-Lifecycle Tools" listing each
  exposed tool, its capability requirement, and an example invocation.
- `docs/HOW_TO_HUMAN.md` — operator walkthrough of driving a dogfood
  via MCP tools instead of bash CLI.
- `docs/HOW_TO_AGENT.md` — note for AI sessions: prefer MCP tool calls
  over bash CLI for dogfood operations; capability token issuance is
  operator-side.
- New `docs/HARNESS_FRICTION_PATTERNS.md` — historical record of the
  three observed friction patterns (036 strategy-then-exit, 037 ask-
  question-and-exit, 038 lease-expiry-under-active-load) and the
  fixes that landed. Reference for future RFC scoping.
- `docs/UBIQUITOUS_LANGUAGE.md` — add "publish-on-behalf",
  "surgical-recovery", "supervised-progress heartbeat".
- `CHANGELOG.md` — Added entries.

### 8. No changes to

- CLI surface (`striatum *` verbs).
- JSON API (`/v1/*`).
- SSE event feed.
- CSP, mutation gate, audit chain semantics.
- Workflow JSON schema.
- The 7-capability vocabulary from RFC 0030 (new `surgical_recovery`
  capability is the only addition).

## Acceptance Criteria

- Every dogfood-lifecycle RPC method has a corresponding MCP chat-tool
  entry, capability-gated, with audit row append.
- `dogfood.publish_on_behalf` composite tool works end-to-end against
  a real ack-denied case; audit chain records a single operation.
- `dogfood.surgical_recovery` composite tool works end-to-end against
  a simulated lease-expired-under-active-load case; audit chain
  records the operator reason.
- Daemon supervised-progress watcher refreshes leases when the
  supervised wrapper's log file is growing.
- `workflow init` / `workflow generate` emit the new harness-profile
  fragments by default.
- Existing workflows can be one-pass backported with a new
  `workflow upgrade` CLI verb that applies the corrected fragments.
- `docs/HARNESS_FRICTION_PATTERNS.md` exists and documents the three
  observed patterns + their fixes.
- New `surgical_recovery` capability is added to the RFC 0030 closed
  vocabulary; admin-only.
- RFC 0035 multi-repo test harness extends with tests for: the new
  composite tools (publish_on_behalf, surgical_recovery), the
  supervised-progress heartbeat (simulate log growth + verify lease
  refresh), and the friction-pattern documentation rendering.
- No regression in existing dogfood-lifecycle behavior.

## Implementation Plan

### Step 1. MCP tool exposure of dogfood-lifecycle verbs

Land the chat-tool entries for each RPC method in
`src/striatum/web/chat_tools.py`. Wire to existing RPC routes; no new
endpoints. Capability gating reuses the existing per-method
capabilities from RFC 0030.

### Step 2. `dogfood.publish_on_behalf` composite tool

Add the composite logic to `src/striatum/web/chat_tools.py` (or a new
`src/striatum/dogfood/operator_tools.py` if it grows). Daemon-side
lookup uses existing SQL helpers. Audit row appended with `operation:
"publish_on_behalf"` and operator-supplied `reason`.

### Step 3. `dogfood.surgical_recovery` composite tool

Same shape. Atomic SQL transaction. New `surgical_recovery` capability
added to the closed vocabulary.

### Step 4. Supervised-progress watcher

Land in `src/striatum/daemon_supervisor/progress_watcher.py`. Daemon
starts one watcher per supervisor; mtime-based polling; calls
`heartbeat` internally when growth detected. Configurable thresholds
in daemon config.

### Step 5. Harness profile fragments + workflow upgrade

Update the RFC 0034 V1 generator's catalog to emit the corrected
fragments by default. Add `striatum workflow upgrade <path>` CLI verb
that backports the corrected fragments into existing workflow.json
files. Test against the dogfood-035..039 workflows.

### Step 6. Documentation + RFC 0035 test extensions

Land the friction-patterns doc + MCP tool docs + HOW_TO_AGENT
guidance. Extend RFC 0035 harness with test cases for the new
composite tools and the supervised-progress watcher.

## Open Questions

- Should `dogfood.publish_on_behalf` require a specific capability
  beyond the underlying `publish-artifact` + `complete`/`verdict`
  capabilities? Recommendation: no — composing existing capabilities
  is the same authority as calling them in sequence.
- Should `dogfood.surgical_recovery` require the operator to supply
  the specific lease/message/job IDs, or should it look them up from
  the job_id? Recommendation: look them up — that's the friction
  this RFC removes.
- Should supervised-progress heartbeat use file-system mtime or a
  proper progress signal (e.g., the wrapper emits a heartbeat line to
  a sidecar file)? Recommendation: mtime for V1 (simpler, works
  against current wrappers); sidecar-signal for V1.5 if the false-
  negative rate is too high.
- Should the per-model harness-profile fragments be operator-
  overridable per workflow? Recommendation: yes — the workflow
  schema's `harness_profiles.<profile>.native_delegation.instruction`
  field is already overridable; the catalog default just provides the
  recommended baseline.
- Should `workflow upgrade` apply other corrections (formatting,
  comments, etc.) or stay scoped to harness-profile fragments?
  Recommendation: scoped to harness-profile fragments in V1; other
  upgrades are separate verbs.
- Should this RFC's MCP tools be exposed via the Go daemon (RFC 0039)
  from day one? Historical recommendation: yes. Current status:
  D110 removed the SQLite-bound composite tools from the production
  contract; any replacement must be PostgreSQL-native and implemented
  against the Go production daemon contract.

## Domain Modeling

This RFC adds operator-facing composite tools and a daemon-side
progress watcher. No new domain aggregates. The capabilities
vocabulary gains one closed-set value (`surgical_recovery`,
admin-only).

The composite tools (`dogfood.publish_on_behalf`,
`dogfood.surgical_recovery`) are aggregates over existing state
transitions: they compose `ack` + `publish-artifact` +
`verdict`/`complete` (or lease reactivation + supervisor reattachment
+ job-state restoration) as single audit-chain entries with operator
metadata. The composed-vs-decomposed audit shape is a domain choice:
this RFC records the composed shape, with `composition_steps` as
audit-row metadata for traceability.
