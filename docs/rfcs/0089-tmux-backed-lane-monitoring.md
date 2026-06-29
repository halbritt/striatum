# RFC 0089: Tmux-backed lane monitoring

Status: accepted
Date: 2026-05-28
Context: RFC 0075, RFC 0077, RFC 0088, D028, D131
Author: proposer-codex-gpt-5.5-001

## Problem

RFC 0088 moves Striatum toward daemon-owned long-lived interactive PTY lanes.
That makes live lane inspection more important, because the operator needs to
see whether a provider TUI accepted the bootstrap prompt, whether MCP startup
completed, and whether the lane is waiting, asking, or stuck.

Today there are two partial answers:

- `trajectory watch` gives a curated daemon-event projection. It is useful for
  dialogue and provenance, but it deliberately excludes raw provider terminal
  output under D028.
- Agent-loop lanes may tee terminal output to
  `.striatum/scratch/<supervisor_id>/pty.log`. This is useful private scratch,
  but it is a tail-log workflow, not the operator's desired "attach to the lane"
  experience.

The tmux path is close but unsafe as a default. `go/pkg/supervisor/pty.go`
creates a tmux session and then launches `tmux attach-session` under a PTY.
The attach process can exit while the underlying tmux session and agent keep
running. When Striatum treats the attach process as the supervised process, it
misreports liveness as `gone`, can mark the supervisor lost, and can downgrade
artifact bylines to `author: operator` even though the real lane is still alive.

This RFC is therefore an implementation RFC, not a placeholder policy RFC. The
first required slice is the helper redesign: replace attach-as-liveness with
session/pane liveness. Once that is done, universal tmux monitoring is a
configuration/default change with tests, not a separate product-policy blocker.

## Goals

- Make every RFC 0088 agent-loop lane operator-attachable through tmux by
  default when tmux is available.
- Track liveness using the real tmux session and pane process, not the lifetime
  of a transient `tmux attach-session` client.
- Preserve D028: tmux pane text and PTY logs are private diagnostics, not
  workflow state, durable provenance, verdict input, byline input, or export
  content.
- Preserve D080/D149 byline semantics: attestation is based on a live owned
  lane process identity and the workflow snapshot command, not terminal text.
- Keep non-interactive tests and non-tmux environments usable through an
  explicit fallback or fail-closed lane policy.

## Non-Goals

- Capturing tmux pane text into daemon-owned PostgreSQL.
- Publishing raw terminal transcripts as artifacts.
- Making tmux a remote or hosted control plane.
- Replacing daemon MCP/RPC workflow control with tmux commands.
- Requiring tmux for local unit tests, archive verification, workflow
  generation, or other non-agent-loop surfaces.
- Solving every provider bootstrap sequence. RFC 0089 makes lanes observable;
  adapter-specific bootstrap delivery remains RFC 0088 work. Current substrate
  uses PTY submit for Claude and initial-prompt argv delivery for codex.

## Proposal

### Implementation Sequence

#### Phase 1 - Replace attach-as-liveness

Fix the helper/supervisor liveness model first. This is the unblocker and must
not be deferred behind a generic "tmux by default" discussion.

The supervisor should create a detached tmux session for the lane, record its
session/pane identity, and treat the lane as live based on that identity. A
`tmux attach-session` client is an observer only. It can come and go without
changing supervisor state.

Required code shape:

- Update `go/pkg/supervisor/pty.go` so `launchPTY` does not return the attach
  process as the supervised lane identity.
- Create the tmux session with a placeholder pane, set `remain-on-exit`, and
  only then respawn the real lane command. This preserves a dead pane for
  diagnostics if the lane command exits immediately at startup.
- Run every setup/cleanup tmux command through a bounded `CommandContext`
  timeout so a wedged tmux server cannot hang the daemon RPC handler
  indefinitely.
- Record tmux session name, window id, pane id, pane pid, and pane pid start
  token in supervisor metadata.
- Build tmux session names with a bounded human-readable prefix plus a stable
  hash suffix over the full unsanitized `(run_id, lane_id, supervisor_id)`
  tuple, so length truncation cannot make one supervisor kill another lane's
  session during startup.
- Add a tmux liveness probe based on `tmux has-session`, pane existence,
  `pane_dead`, pane pid, and pane pid start-token comparison.
- Accept only numeric pane start tokens as verified identity tokens. Literal
  tmux format strings or other non-numeric values are treated as unavailable,
  not as matching identity evidence.
- When tmux cannot report a numeric live `pane_start_time`, compare the
  recorded numeric pane start token against the OS process start token for the
  observed pane PID before downgrading attestation.
- If a platform or tmux version cannot verify the pane pid start token, keep
  pane liveness operational but downgrade lane attestation to
  `unattested` with reason `start_token_unverified`.
- Route `supervise.status`, delivery reconciliation, daemon doctor, and
  recovery sweep through that probe for tmux-backed lanes.
- Keep attach commands as metadata only.
- When the helper-owned attach bridge exits but the tmux pane is still live,
  keep pane liveness attached/attested while marking delivery liveness
  degraded until `supervise.rebridge`, restart, or a later send-keys delivery
  path clears it.
- Ship `supervise.rebridge` as the in-place repair path for delivery-degraded
  tmux lanes whose pane process is still live. It recreates the delivery FIFO
  if needed and starts a fresh helper attach path without killing, resetting,
  or respawning the pane.
- Treat helper-reported attach-exit liveness as advisory. The daemon must
  re-probe the recorded tmux session/pane identity before deciding whether the
  supervisor remains attached or moves to detached.
- Open supervisor delivery FIFOs in nonblocking mode. If there is no stdin
  reader, mark delivery liveness degraded with reason `stdin_reader_missing`
  and refuse delivery instead of blocking an RPC handler indefinitely.
- On helper-local cancellation or packet-forward failure, terminate tmux-backed
  lanes by killing the recorded tmux session. A direct pane-PID signal is only
  allowed after the recorded pane start token is numeric and still matches the
  current process identity; missing, literal, unavailable, or mismatched tokens
  skip direct signalling.

#### Phase 2 - Make attach commands first-class in reads

After Phase 1, status/dashboard/web projections should present an operator
attach command for every tmux-backed lane. This phase is read-surface work over
the metadata captured in Phase 1.

#### Phase 3 - Flip agent-loop defaults

After Phase 1 and Phase 2 tests pass, RFC 0088 agent-loop lanes launch through
the PTY-helper path and become tmux-backed by default when tmux is installed.
Workflows that set
`supervision.require_tmux: true` continue to fail closed when tmux is absent.
Workflows that do not require tmux may fall back to plain PTY, but that
fallback must be explicit in metadata and status. Workflows that still need
legacy pipe delivery must opt out with `supervision.transport: "pipe"`.

### 1. Split tmux ownership from attach clients

The supervisor should create a detached tmux session for the lane, but it must
not use `tmux attach-session` as the liveness proxy. The attach client is only
an operator UI handle.

On launch, Striatum records tmux metadata in the supervisor pointer metadata:

- tmux session name
- window id
- pane id
- pane pid
- pane pid start token when the platform can provide it
- attach command
- capture/log path, when an operator-local PTY log is active

The lane remains "attached" when the tmux session exists and the pane process
identity still matches. `tmux attach-session` exiting does not mark the
supervisor lost. If the exited attach client was the helper-owned delivery
bridge, `supervise.status` and dashboard reads must also surface
`delivery_liveness: {class: "degraded", reason: "attach_client_exited"}` so
pane liveness cannot be mistaken for healthy packet delivery.
The daemon makes that attached-versus-detached decision from a fresh tmux probe,
not from the helper's reported `tmux_liveness` field.

### 2. Probe tmux directly for liveness

`supervise.status`, delivery reconciliation, daemon doctor, and recovery sweep
should use a tmux-aware liveness probe when supervisor metadata says the lane is
tmux-backed:

1. `tmux has-session -t <session>` confirms the session still exists.
2. `tmux display-message -p -t <pane> "#{pane_pid} #{pane_dead}"` or an
   equivalent format query confirms the pane still maps to a live process.
3. The pane pid start token is compared against the stored token when
   available and numeric.

Failure classes should stay structured:

- `tmux_session_missing`
- `tmux_pane_missing`
- `tmux_pane_dead`
- `tmux_pane_pid_mismatch`
- `tmux_unavailable`

These classes appear in `supervise.status`, `doctor --verbose`, status
next-actions, and recovery sweep details. They do not rely on pane text.
Probe misses also produce a typed `probe_failure` record containing the
`failure_class`, optional `exit_code`, optional `errno`, optional
`pane_process_alive`, and optional `observed_pane_pid`. The liveness state
derived from the class is `healthy` for `tmux_ok`, `degraded` for transient
`tmux_unavailable`, and `lost` for terminal tmux identity failures.
During a transient `tmux_unavailable` soft window, pointer metadata and read
projections should expose the last successful probe time, the most recent
skipped probe time, and the consecutive unavailable count so operators get a
warning before the supervisor is marked lost.

### 3. Route operator attach through metadata

Status/dashboard/web projections should show the tmux attach command for each
tmux-backed lane. The operator path should be:

```bash
striatum supervise status --session-id <session_id> --json
tmux attach-session -t <tmux_session_name>
```

Attaching and detaching are local operator actions. They must not mutate
workflow state. Delivery repair is explicit: `striatum supervise rebridge
--session-id <session_id>` reattaches the helper-owned delivery bridge in place
only while pane-process liveness still holds. When tmux liveness is terminal
(`tmux_session_missing`, `tmux_pane_missing`, `tmux_pane_dead`, or
`tmux_pane_pid_mismatch`), delivery must stop and the operator path is to stop
the broken supervisor and start/reclaim a replacement lane through the daemon
workflow controls.

### 4. Make agent-loop lanes tmux-backed by default

Once Phase 1 and Phase 2 are implemented and tested, RFC 0088 agent-loop lanes
should launch with tmux backing by default when tmux is installed. Workflows may
still set `supervision.require_tmux: true` to fail closed when tmux is absent.

If tmux is unavailable and the lane does not require it, Striatum may fall back
to a plain PTY. The fallback must be explicit in metadata and status so the
operator can see that the lane is not tmux-attachable.

### 5. Keep transcript policy narrow

Tmux monitoring is an inspection surface. It is not transcript capture.

Raw pane text, alternate-screen content, and local `pty.log` bytes must remain
outside daemon-owned PostgreSQL, artifacts, corpus exports, evidence exports,
run archives, and byline/verdict/completion decisions. Existing curated
surfaces (`trajectory watch`, interrogation/conversation reads, artifacts,
events) remain the durable provenance path.

## Acceptance Criteria

- A tmux-backed lane whose operator attach client exits while the tmux session
  continues remains `attached` and lane-attested.
- A helper-owned attach-bridge exit with a live pane marks delivery liveness
  degraded, and `supervise.send` refuses that supervisor until
  `supervise.rebridge`, restart, or a future send-keys delivery path clears the
  degradation.
- A helper-owned attach-bridge exit whose fresh daemon tmux probe observes a
  dead/missing/mismatched pane moves the supervisor to `detached` even if the
  helper event reported `tmux_ok`.
- A missing stdin FIFO reader degrades delivery with reason
  `stdin_reader_missing`; `supervise.send` returns a structured refusal without
  blocking and does not record `supervisor.packet_delivered`. The degraded
  delivery value is honored whether it is nested under `tmux.delivery_liveness`
  or, for no-tmux/plain metadata, stored at top-level `delivery_liveness`.
- Killing the tmux session or pane transitions the supervisor/read projection
  to a structured lost/unhealthy state before any further packet delivery.
- Immediate lane command exit after tmux startup preserves a retained dead pane
  (`tmux_pane_dead`) rather than collapsing to `tmux_session_missing`.
- A wedged tmux setup command returns a bounded timeout error instead of
  hanging the daemon.
- Long run, lane, or supervisor identifiers do not collide after tmux session
  name truncation; the full identity contributes to a stable hash suffix.
- `supervise.status --json` exposes tmux session, pane, pane pid, attach
  command, and liveness classification for tmux-backed lanes.
- `supervise.status --json` downgrades start-token-unverified tmux lanes to
  `lane_attestation: unattested` with reason `start_token_unverified`.
- Literal or non-numeric `pane_start_time` output from tmux does not grant
  lane-attested bylines; it is treated the same as a missing/unverified start
  token.
- `doctor --verbose`, status/dashboard, and recovery sweep surface the same
  tmux failure classes without reading pane text.
- `supervise.stop` terminates the tmux-backed lane session and marks the
  supervisor stopped without relying on a transient attach process.
- `supervise.stop` gates any direct PID cleanup fallback on matching pid start
  tokens and records a skip reason instead of signalling stale helper or pane
  PIDs.
- Helper-local teardown paths, including context cancellation and
  packet-forward failure, use the same tmux-session-first identity rule and do
  not signal unverified pane PIDs.
- A live RFC 0088 agent-loop lane can be monitored with `tmux attach-session`
  while still completing work through MCP.
- A regression test proves attach-client exit does not mark the supervisor
  lost.
- A regression test proves missing tmux session/pane prevents packet delivery
  and reports a structured failure.
- A D028 guard test proves raw tmux pane text does not enter artifacts,
  trajectory export, evidence export, corpus export, run archive, or daemon
  event payloads.

## Open Questions

No open question blocks Phase 1.

Non-blocking choices for later polish:

- Single tmux session per run versus one session per supervisor. Phase 1 keeps
  the current one-session-per-supervisor shape because it is already present and
  simpler to clean up.
- A future `striatum supervise attach --session-id <id>` convenience wrapper.
  Phase 1 and Phase 2 expose a copyable `tmux attach-session` command only.
- Orphan retention after supervisor loss. Phase 1 preserves the existing
  operator-inspection posture: do not silently erase diagnostic state as part of
  marking a lane lost.

## Domain Modeling

This RFC clarifies the `supervisor` aggregate and its value objects. The tmux
session, pane id, pane pid, pane pid start token, and attach command are
supervisor metadata, not new workflow state authorities. The liveness
classification is a derived read-model value. The domain events remain
supervisor lifecycle events (`supervisor.started`, `supervisor.lost`,
`supervisor.stopped`, plus any future tmux-specific diagnostic event), and they
must carry metadata only, never terminal text.

See [`docs/DDD.md` "Adding to the model"](../reference/domain-driven-design.md#adding-to-the-model)
for the project rule that new concepts need an explicit domain home.

## Proposed Decision-Log Entry

- **D152** - Accept RFC 0089: agent-loop lanes become tmux-backed and
  operator-attachable by default once tmux liveness is tracked through tmux
  session/pane identity rather than transient `attach-session` clients. Tmux
  pane text remains private diagnostics only and never becomes workflow state,
  durable provenance, byline input, verdict input, or export content.
