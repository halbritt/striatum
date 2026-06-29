# RFC 0075: Tmux-Observable MCP Agent Sessions And Liveness Deadlines

Status: accepted
Date: 2026-05-21
author: proposer-codex-gpt-5-001
Decision: D131
Context:
[`RFC 0009`](0009-long-lived-process-supervision.md),
[`RFC 0020`](0020-autonomous-stalled-run-recovery.md),
[`RFC 0130 MCP`](0130-go-daemon-http-sse-mcp.md),
[`RFC 0058`](0058-operator-progress-surface.md),
[`RFC 0063`](0063-hardened-pty-supervision.md),
[`docs/HOW_TO_AGENT.md`](../how-to/how-to-agent.md),
[`docs/UBIQUITOUS_LANGUAGE.md`](../reference/ubiquitous-language.md)

## Problem

RFC 0130 moves live lane execution toward autonomous MCP agents: the
daemon exposes `/mcp`, agents discover tools, call `work.await_packet`,
and drive the lane loop through `tools/call` instead of being spoon-fed
JSON packets by a CLI or wrapper.

That transition improves authority boundaries, but it creates a new
operator-observability problem. A terminal agent can be alive while doing
no useful protocol work. It may be waiting on an auth prompt, asking a
question in terminal text, stuck before tool discovery, idling after a
model timeout, or holding a lease without calling `work.heartbeat`.

Today Striatum has several related mechanisms, but they are not one
post-transition product contract:

- PTY and tmux supervision exist as infrastructure, and PTY-helper lanes can
  opt into fail-closed tmux with `supervision.require_tmux: true`; D131 accepts
  the current UI/status polish as the post-transition product contract.
- Work leases have heartbeats, but a live process without MCP activity is
  a different failure mode from a stale lease.
- The project intentionally does not treat tmux panes, terminal output,
  or transcripts as authoritative workflow state.
- Operators need an attachable pane and a precise stall reason before a
  silent agent wastes an entire run.

The missing requirement is explicit: after the MCP transition, every live
interactive agent session should be inspectable through a daemon-created
tmux pane, and the daemon should classify "alive but not progressing"
states without parsing terminal output or capturing transcripts.

## Goals

- Make daemon-created PTY sessions tmux-observable for live interactive
  agent lanes, with fail-closed tmux opt-in for lanes that require an
  attachable pane.
- Preserve the authority boundary: daemon PostgreSQL and MCP calls remain
  authoritative; tmux panes and terminal output are operational
  observability only.
- Add liveness deadlines for the MCP startup path: tool discovery,
  `work.await_packet`, packet acknowledgment, and lease heartbeat.
- Distinguish supervisor liveness, MCP protocol liveness, and lease
  liveness.
- Surface precise stall reasons to the operator, including an attach
  command and enough metadata to inspect the pane.
- Give agents a structured way to ask a pre-work question or escalate a
  startup blocker instead of waiting silently in terminal text.
- Keep transcript capture off by default. Operational progress bytes may
  stay in scratch for liveness detection, but they are not durable
  provenance and must not be published as artifacts.

## Non-Goals

- Reintroducing terminal output as workflow state.
- Parsing model text to infer decisions, blockers, verdicts, or
  completion.
- Persisting full transcripts, model output, or chain-of-thought.
- Making tmux a hosted or remote control plane. This RFC is local-only.
- Replacing the MCP work loop with tmux commands.
- Requiring tmux for non-interactive one-shot fixtures, unit tests, or
  explicitly headless CI adapters unless those lanes opt into the live
  interactive profile.
- Solving full operator UI design. This RFC defines the daemon/session
  contract that a UI can render.

## Proposal

### 1. Make live interactive agent sessions tmux-observable

After RFC 0130 Phase D/E are accepted and live agents are expected to use
MCP directly, Striatum should treat PTY supervision plus tmux attach metadata
as the operator-observability surface for live interactive agent lanes. The
accepted current implementation provides fail-closed tmux through
`supervision.require_tmux` on PTY-helper lanes; making tmux universal by
default is a later profile/default-policy decision, not an implicit runtime
requirement.

For each live interactive session, the daemon records operational
metadata:

- supervisor id;
- session id;
- run id and repository id;
- lane id and role id;
- tmux session name;
- tmux window or pane id when available;
- attach command;
- child command and cwd as already permitted by the process contract;
- process liveness state;
- last pane activity time as progress metadata only;
- last MCP request time;
- last `tools/list` time;
- last `work.await_packet` time;
- last `work.ack` time;
- last `work.heartbeat` time while holding a lease;
- current liveness/stall classification.

The daemon must not use pane contents as workflow facts. The operator may
attach to inspect what the agent is doing, but state transitions still
come from MCP/RPC methods, PostgreSQL rows, events, and artifacts.

### 2. Split liveness into three signals

The daemon should model three related but independent signals:

| Signal | Meaning | Source |
|---|---|---|
| supervisor heartbeat | The OS process or tmux session still appears alive. | supervisor/tmux/process probe |
| protocol heartbeat | The agent has made recent MCP requests. | daemon MCP request metadata |
| lease heartbeat | The agent refreshed an active work lease. | `work.heartbeat` |

This avoids the common false comfort of "the terminal is still open."
A session can be supervisor-live and protocol-dead. A session can be
protocol-live without holding a lease. A session can hold a lease but
miss the heartbeat deadline.

### 3. Add startup and work-loop deadlines

Live interactive profiles should define default deadlines, overrideable
by workflow policy or lane constraints:

| Deadline | Starts when | Expected action | Stall reason |
|---|---|---|---|
| MCP discovery | agent process starts | `tools/list` | `agent_mcp_discovery_stall` |
| await packet | successful discovery | `work.await_packet` | `agent_await_packet_stall` |
| packet ack | packet delivered | `work.ack`, `work.block`, or structured escalation | `agent_ack_stall` |
| lease heartbeat | lease acquired | `work.heartbeat` before expiry threshold | `agent_lease_heartbeat_stall` |
| idle protocol | last MCP request | any expected MCP call for the current phase | `agent_protocol_idle_stall` |

The daemon should emit a metadata-only event when a deadline is missed
and surface the stall through dashboard, operator current-brief, daemon
status, and future UI surfaces.

### 4. Add pre-work session tools

`work.block` is appropriate after a packet exists. It does not cover an
agent that is stuck before `work.await_packet` or needs human input
before it can safely begin.

Add or reserve daemon MCP methods for session-level startup health:

- `session.ready`: agent confirms it has connected, discovered tools, and
  is about to call `work.await_packet`.
- `session.heartbeat`: agent-level heartbeat when no work lease is held.
- `session.question`: agent asks a structured pre-work question.
- `session.escalate`: agent reports a startup blocker requiring operator
  attention.

Exact method names may change during implementation if the daemon method
contract chooses a different namespace. The product requirement is that a
pre-packet agent has a structured MCP path for "I am blocked" and "I need
input" that does not rely on terminal text.

### 5. Tighten the bootstrap instruction

The agent-loop bootstrap prompt should become explicit about silent
waiting:

```text
After launch, connect to the MCP endpoint and call tools/list.
Then call work.await_packet.

If you need human input before you can call work.await_packet, use the
session.question or session.escalate MCP tool. Do not wait silently in
the terminal and do not ask only in pane text.

After receiving a packet, call work.ack before starting work. While
holding a lease, call work.heartbeat periodically until you complete,
block, release, or otherwise close the packet through MCP.
```

The prompt is not the security boundary. It is an ergonomics and
operational contract that supports the daemon deadlines.

### 6. Preserve no-transcript policy

Tmux observability must not become broad transcript capture.

Allowed:

- tmux session/window/pane identifiers;
- attach command;
- process and pane liveness metadata;
- byte-growth or mtime-derived progress metadata in scratch;
- short operator-authored stall notes;
- hashes of emitted operational chunks if needed for diagnostics.

Disallowed by default:

- durable full terminal transcripts;
- parsing terminal text for workflow decisions;
- publishing pane output as artifacts;
- adding terminal output to daemon audit bodies;
- using model text as a substitute for `work.block`, `verdict`,
  `publish-artifact`, `complete`, or decision artifacts.

## Acceptance Criteria

- Live interactive PTY-helper lanes can launch inside daemon-created
  tmux-backed PTY sessions with attach metadata.
- `tmux` absence fails closed for lanes that set
  `supervision.require_tmux: true`, with a clear adapter error and
  remediation text, while lanes without that opt-in keep their documented
  behavior.
- Operator-facing status surfaces show the tmux attach command and
  current liveness classification for each live interactive session.
- Tests distinguish supervisor-live/protocol-dead, protocol-live/no-lease,
  and active-lease/missed-heartbeat cases.
- Deadline tests cover missed tool discovery, missed `work.await_packet`,
  missed `work.ack`, and missed `work.heartbeat`.
- A fake agent that asks a pre-work question through MCP becomes visible
  as a structured operator escalation without relying on pane text.
- A fake agent that asks only in terminal output is classified as stalled
  when MCP deadlines expire.
- No test or production path treats tmux output as authoritative workflow
  state.
- Evidence export, daemon audit, and artifact publication remain free of
  broad transcript capture by default.

## Implementation Status

Accepted by D131 for the current local-first contract. The landed
implementation includes:

- Go agent-loop PTY bootstrap that launches agents as autonomous MCP clients;
- `session.report` as the typed pre-work ready/heartbeat/question/escalation
  path;
- RFC 0077 daemon-owned MCP activity timestamps, deadline classifications, and
  metadata-only liveness events;
- tmux attach metadata from PTY-helper supervision, projected through
  `status`, dashboard data, `supervise.status`, `supervise.list`, terminal
  dashboard frames, and web run-detail session chips;
- fail-closed `supervision.require_tmux` opt-in for PTY-helper lanes;
- no-transcript/no-terminal-authority tests that prevent pane capture and keep
  terminal output out of artifact, audit, and recovery paths.

The original universal "tmux required by default for all future live
interactive profiles" clause is not a hidden open implementation gate in this
accepted scope. The current contract exposes a lane-level fail-closed opt-in.
A later profile/default-policy RFC can tighten that default if the product
needs it.

## Implementation Scaffold

The first implementation workflow is scaffolded at
[`docs/operator/workflows/rfc-0075-and-mcp-cutover.json`](../operator/workflows/rfc-0075-and-mcp-cutover.json).
It deliberately starts with a cutover map and liveness-contract design before
any source implementation. The paired operator plans are:

- [`RFC 0130 CLI Retirement Cutover`](../operator/plans/rfc-0050-cli-retirement-cutover.md)
- [`RFC 0075 Tmux-Observable MCP Agent Sessions`](../operator/plans/rfc-0075-tmux-observable-mcp-agent-sessions.md)

## Resolved Questions

- The structured pre-work path is one typed `session.report` daemon method.
- Deadline defaults are owned by RFC 0077's daemon liveness policy for the
  current slice.
- Tmux metadata is an allow-listed inspection projection only: session name,
  window id, pane id, attach command, and unavailability/remediation metadata.
- The current implementation supports fail-closed tmux through
  `supervision.require_tmux`; broader default policy and alternate local PTY
  multiplexers require a later decision.

## Domain Modeling

This RFC is a boundary clarification and domain-event proposal.

The aggregate roots remain `session`, `supervisor`, `lease`, and `run`.
Tmux pane identity is operational metadata attached to a supervised
session, not a new state authority. MCP protocol activity and liveness
deadlines produce domain events such as:

- `session.mcp_discovered_tools`;
- `session.await_packet_started`;
- `session.startup_question_raised`;
- `session.liveness_deadline_missed`;
- `lease.heartbeat_missed`.

The important invariant is that Striatum may observe a terminal for
liveness and human inspection, but it must not infer workflow truth from
terminal output. Workflow truth remains in daemon-owned PostgreSQL,
daemon method calls, artifacts, verdicts, decisions, and explicit
operator actions.
