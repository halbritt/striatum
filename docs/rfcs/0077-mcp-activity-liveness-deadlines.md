# RFC 0077: MCP Activity Liveness Deadlines

Status: accepted
Date: 2026-05-22
author: proposer-codex-gpt-5-001
Context:
[`RFC 0130 MCP`](0130-go-daemon-http-sse-mcp.md),
[`RFC 0058`](0058-operator-progress-surface.md),
[`RFC 0063`](0063-hardened-pty-supervision.md),
[`RFC 0075`](0075-tmux-observable-mcp-agent-sessions.md),
[`docs/explanation/mcp.md`](../explanation/mcp.md),
[`docs/reference/spec.md`](../reference/spec.md),
historical generated-record source paths
`docs/operator/artifacts/rfc-0075-and-mcp-cutover/design/LIVENESS_CONTRACT.md`
and `docs/operator/artifacts/rfc-0075-and-mcp-cutover/final/SUMMARY.md`

Decision: D129

## Problem

RFC 0075 defines the broader post-RFC-0130 interactive-agent
observability contract: agents should operate as autonomous MCP clients,
tmux panes should be attachable for local inspection, and the daemon
should classify sessions that are alive but not making protocol
progress. The first RFC 0075 slice has already landed
`session.report`, giving agents a structured pre-packet path for
`ready`, `heartbeat`, `question`, and `escalate` reports.

The remaining liveness gap is narrower than full tmux metadata. The
daemon still lacks a PostgreSQL-owned timeline of MCP activity for each
session, so status surfaces cannot distinguish these cases reliably:

- a process is alive but has never discovered MCP tools;
- tools were discovered, but the agent never called
  `work.await_packet`;
- a packet was delivered, but the agent did not acknowledge, block, or
  escalate;
- a lease is held, but `work.heartbeat` stopped;
- an agent used terminal text instead of `session.report` for a
  question or startup blocker.

Operators need this classification before full tmux status work lands.
The classification must come from daemon-observed MCP/RPC activity and
daemon-owned PostgreSQL rows, not pane text, transcripts, marker files,
or provider hooks.

## Goals

- Persist per-session MCP activity timestamps in daemon-owned
  PostgreSQL.
- Classify liveness deadlines for MCP discovery, await-packet, packet
  acknowledgment, lease heartbeat, structured question, and escalation
  phases.
- Treat `session.report` as the landed structured pre-work signal and
  fold its `ready`, `heartbeat`, `question`, and `escalate` reports into
  the liveness timeline.
- Surface a compact liveness classification through existing status and
  supervisor read surfaces.
- Emit metadata-only domain events when a session enters or recovers
  from a liveness stall.
- Preserve the product boundary: daemon PostgreSQL and MCP/RPC calls are
  authoritative; terminal panes and transcripts are not workflow state.
- Make the implementation useful before full tmux metadata persistence
  is available.

## Non-Goals

- Persisting tmux session, pane, window, or attach-command metadata in this
  RFC 0077 slice. D131 later accepts that projection under RFC 0075.
- Requiring tmux for live interactive lanes.
- Capturing, parsing, hashing, publishing, or auditing full terminal
  transcripts or pane contents.
- Inferring workflow decisions, blockers, verdicts, or completion from
  terminal output.
- Introducing hosted services, telemetry, external persistence, or
  provider SDK integration.
- Replacing lease expiry, stale-lease recovery, or existing
  `work.heartbeat` semantics.
- Adding a new agent transport. This RFC uses the existing daemon MCP
  and RPC surfaces.

## Proposal

### 1. Persist session MCP activity timestamps

The daemon records activity timestamps for authenticated MCP/RPC calls
that can be attributed to a Striatum session. The first implementation
may store these as nullable columns on `sessions`; a later RFC may split
historical activity into an append-only table if operators need
post-mortem timelines.

Minimum persisted fields:

| Field | Source |
|---|---|
| `last_mcp_request_at` | any authenticated MCP request attributed to the session |
| `last_tools_list_at` | MCP `tools/list` |
| `last_await_packet_at` | entry to `work.await_packet` |
| `last_packet_delivered_at` | successful packet delivery from `work.await_packet` |
| `last_ack_at` | `work.ack`, or equivalent packet acknowledgment path |
| `last_work_block_at` | `work.block` after a packet exists |
| `last_work_release_at` | `work.release` |
| `last_work_complete_at` | `work.complete` |
| `last_work_heartbeat_at` | `work.heartbeat` |
| `last_session_ready_at` | `session.report` with `report_kind=ready` |
| `last_session_heartbeat_at` | `session.report` with `report_kind=heartbeat` |
| `last_session_question_at` | `session.report` with `report_kind=question` |
| `last_session_escalate_at` | `session.report` with `report_kind=escalate` |
| `liveness_stall_class` | latest derived stall class, nullable |
| `liveness_stall_since` | timestamp for the current stall class, nullable |

These fields are metadata. They do not store MCP request bodies,
terminal text, model output, chain-of-thought, artifact contents, or
transcripts. `session.report.message` remains the existing bounded
operator-facing report field, not a transcript channel.

### 2. Define liveness phases and stall classes

The daemon derives at most one current stall class per session. The
classification is a projection over PostgreSQL state: session creation,
supervisor/process state when available, packet delivery, leases,
session-report rows, and activity timestamps.

Evaluation order is first match wins:

| Stall class | Set when |
|---|---|
| `agent_mcp_discovery_stall` | session is expected to use MCP and no `last_tools_list_at` appears before the discovery deadline |
| `agent_await_packet_stall` | MCP discovery occurred, but no `last_await_packet_at` appears before the await-packet deadline |
| `agent_ack_stall` | a packet was delivered, but no `work.ack`, `work.block`, `work.release`, `work.complete`, `session.report(question)`, or `session.report(escalate)` appears before the ack deadline |
| `agent_lease_heartbeat_stall` | an active lease exists and the most recent `work.heartbeat` is older than the lease heartbeat threshold plus configured slack |
| `agent_question_pending` | the latest relevant pre-work signal is `session.report(question)` and no operator/daemon resolution or later progress signal has superseded it |
| `agent_escalation_pending` | the latest relevant pre-work signal is `session.report(escalate)` and no operator/daemon resolution or later progress signal has superseded it |
| `agent_protocol_idle_stall` | the session is still expected to make MCP progress, but no MCP request arrived within the protocol-idle deadline for the current phase |

`agent_question_pending` and `agent_escalation_pending` are attention
classes, not proof that the process is unhealthy. They are included so
status can separate "agent used the structured path and is waiting for
operator input" from "agent only wrote a question in terminal text and
missed MCP deadlines."

### 3. Configure conservative defaults

The initial defaults should match the RFC 0075 liveness contract unless
implementation evidence shows a need to adjust them:

| Deadline | Default |
|---|---:|
| MCP discovery | 60 seconds |
| await packet | 90 seconds |
| packet ack | 60 seconds |
| lease heartbeat slack | 30 seconds beyond existing lease heartbeat policy |
| protocol idle | 300 seconds |

Deadline configuration precedence:

1. lane-level liveness overrides in the workflow snapshot;
2. optional workflow-level liveness policy, if implemented in the same
   slice;
3. daemon configuration;
4. hard-coded daemon defaults.

All deadline decisions must use the immutable workflow snapshot and
daemon configuration captured for the running session. Editing the
workflow file on disk must not silently change live deadline policy.

### 4. Emit metadata-only liveness events

When a session enters a non-null liveness class, the daemon emits:

```text
session.liveness_deadline_missed
```

Event payload fields are limited to metadata:

- `repository_id`;
- `run_id`;
- `session_id`;
- `lane_id`;
- `role_id`;
- `stall_class`;
- `deadline_name`;
- `deadline_seconds`;
- `observed_at`;
- the relevant last-activity timestamp.

When a later MCP/RPC signal clears the class, the daemon emits:

```text
session.liveness_recovered
```

The event payload names the previous class and the MCP/RPC signal that
cleared it. Events must not contain pane contents, JSON-RPC request
bodies, artifact contents, or model output.

### 5. Project liveness through existing reads

Existing status surfaces should expose a compact per-session block:

```json
{
  "session_id": "sess_...",
  "liveness": {
    "protocol": "live",
    "lease": "no_lease",
    "stall_class": null,
    "stall_since": null,
    "last_mcp_request_at": "2026-05-22T12:00:00Z",
    "last_tools_list_at": "2026-05-22T11:59:12Z",
    "last_await_packet_at": "2026-05-22T11:59:20Z",
    "last_work_heartbeat_at": null,
    "last_session_report_kind": "ready"
  }
}
```

The projection should be available through daemon status, dashboard
data, `supervise.status`, and MCP-readable status paths as they already
exist. UI design is out of scope; this RFC defines the daemon-owned
fields a UI can render.

### 6. Preserve no-terminal-authority guardrails

Terminal text can help a human inspect a local session, but it must not
be consumed by the daemon to decide liveness classes. A fake agent that
prints "waiting for input" in a pane but never calls `session.report`
must be treated as protocol-idle or phase-stalled when deadlines expire.

The daemon may consider supervisor/process state when the existing
supervision model provides it, but this RFC's liveness classification is
primarily protocol-owned. Full tmux identity, attach-command rendering,
and fail-closed tmux requirements remain RFC 0075 follow-up work.

## Acceptance Criteria

- Daemon-owned PostgreSQL persists the minimum MCP activity timestamps
  for MCP discovery, await-packet, packet delivery, ack, work heartbeat,
  and `session.report` report kinds.
- `session.report` remains claim-capability gated,
  single-repository-scoped, and visible as the structured pre-work
  signal for `ready`, `heartbeat`, `question`, and `escalate`.
- The daemon classifies discovery, await-packet, ack, lease-heartbeat,
  question-pending, escalation-pending, and protocol-idle states without
  parsing terminal output.
- Status/supervisor read surfaces expose current `stall_class`, relevant
  last-activity timestamps, and deadline metadata.
- A fake agent that calls `tools/list` but not `work.await_packet`
  reaches `agent_await_packet_stall`.
- A fake agent that receives a packet but does not ack, block, release,
  complete, question, or escalate reaches `agent_ack_stall`.
- A fake agent with an active lease and no fresh `work.heartbeat`
  reaches `agent_lease_heartbeat_stall` while preserving existing lease
  recovery behavior.
- A fake agent that calls `session.report(question)` or
  `session.report(escalate)` becomes visible as structured operator
  attention, not as a terminal-text scrape.
- A fake agent that asks only in terminal text creates no structured
  question/escalation state and stalls when the relevant MCP deadline
  expires.
- `session.liveness_deadline_missed` and
  `session.liveness_recovered` events are metadata-only and fire once
  per transition.
- Evidence export, daemon audit, artifact publication, and request logs
  remain free of pane contents, full transcripts, JSON-RPC bodies, and
  model output by default.

## Open Questions

- Should the first implementation store timestamps directly on
  `sessions`, or introduce a small `session_activity` table immediately?
  This RFC recommends session columns for the first slice.
- Should overlong `session.report.message` values be rejected or
  truncated with a flag? The RFC 0075 contract recommends bounded
  operator-facing text; exact handling remains an implementation choice.
- What live-state row should represent resolution of
  `agent_question_pending` when the answer is operator-provided but no
  packet exists yet?
- Should workflow-level liveness policy land in the first slice, or
  should lane-level overrides plus daemon defaults ship first?
- Should `work.block` before `work.ack` count as an ack-equivalent
  signal, or should the daemon require explicit ack before block for
  packet ownership clarity?
- How should liveness projection behave for sessions that are manually
  closed while stalled?

## Domain Modeling

This RFC adds a daemon-owned value-object projection and two domain
events, following [`docs/DDD.md § "Adding to the model"`](../explanation/domain-driven-design.md#adding-to-the-model).

The aggregate roots remain `session`, `run`, `lease`, and `supervisor`.
MCP activity timestamps are attributes of a `session` under a registered
repository. A liveness classification is a derived value object over the
session, its active lease, and daemon-observed MCP/RPC metadata.

The new domain events are:

- `session.liveness_deadline_missed`;
- `session.liveness_recovered`.

The boundary clarification is load-bearing: Striatum may classify
protocol silence from daemon-owned MCP timestamps, but it must not infer
workflow truth from terminal output. Repository artifacts remain durable
provenance; daemon-owned PostgreSQL remains live state; MCP/RPC methods
remain the authoritative mutation surface.

## Implementation Note

The accepted V1 slice landed in the native Go daemon on 2026-05-22:

- migration 0012 persists the session activity timestamp columns and
  current stall-class fields;
- Go MCP `tools/list`, work-packet lifecycle mutations, work heartbeat,
  and `session.report` update the per-session activity timeline;
- `status`, dashboard data, and `supervise.status` project protocol
  liveness without mutating read paths;
- the resident recovery sweep persists liveness stall transitions and
  emits metadata-only `session.liveness_deadline_missed` /
  `session.liveness_recovered` events.

D131 later accepts the broader RFC 0075 tmux-observable session contract for
the current scoped implementation, including tmux attach metadata projection
and web/session status polish.
