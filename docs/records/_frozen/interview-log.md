# Interview Log

Status: historical (incubation)
Date: 2026-05-06

> **This is a historical incubation artifact, not active product
> material.** The interview rounds below produced the original
> `docs/PRD.md` and the early `docs/DECISION_LOG.md` rows
> (D001–D040 territory). Decisions made *since* striatum
> graduated to its own repo land via RFCs and `D###` rows, not
> via this log. Read this only when you need to understand how
> a load-bearing decision was originally framed; for current
> product state, read `docs/SPEC.md` and `docs/DECISION_LOG.md`.

This log captures the interview-driven design process for `striatum`.

## Ground Rules

- Ask one question at a time.
- Provide a recommended answer with each question.
- Wait for owner feedback before moving to the next question.
- Record accepted, rejected, deferred, and superseded answers in
  `docs/DECISION_LOG.md`.
- Preserve uncertainty instead of prematurely forcing agreement.

## Rounds

### Q001: Starting Artifact

Question: Should the project start from a PRD, RFCs, or a specification?

Recommended answer: Start with a PRD, backed by a decision log. Use RFCs for
contested architectural branches once the PRD defines user needs and product
boundaries. Write the implementation specification after the PRD and initial
RFCs have resolved the major decisions.

Owner answer: agreed.

Resolution: accepted as D001 in `docs/DECISION_LOG.md`.

### Q002: V1 Product Boundary

Question: What is the v1 product boundary: a generic local terminal-agent
orchestrator, or an Engram-specific Phase 3 runner extracted later?

Recommended answer: Build `striatum` as a generic local terminal-agent
orchestrator from the start, with Engram Phase 3 and the RFC-ledger workflow as
reference fixtures. The product should not embed Engram-specific marker names,
prompt ordinals, or review directories into core logic.

Owner answer: pending.

Owner answer: accepted.

Resolution: accepted as D022 in `docs/DECISION_LOG.md`.

Owner answer: A. Generic from the start.

Additional owner input:

- Use all three frontier model lanes.
- Allow any one frontier model lane to act as the main coordinator for a build
  phase.
- Support interaction with the coordinator via Slack eventually.
- Keep runners/sub-agents introspectable through tmux.
- Consider a TUI dashboard, and later a web dashboard with chat.
- Add a lightweight message bus, while keeping open how agents wire into it.
- Support `gemini-cli`, `claude-code`, and Codex CLI out of the box.
- Let the coordinator accept chat commands such as "read prompt foo" and then
  execute repo-defined instructions/workflows.
- Model portability is a design goal, as with Engram.

Resolution: accepted as D002 in `docs/DECISION_LOG.md`.

Model portability was also recorded as D003 in `docs/DECISION_LOG.md`.

### Q003: Coordinator Shape

Question: What is the coordinator: a deterministic orchestrator, an AI agent,
or a hybrid?

Recommended answer: Use a hybrid. A deterministic local coordinator owns state,
gates, process launching, message routing, retries, stop conditions, and
write-scope safety. Exactly one selected, portable model lane provides chat and
project-manager style workflow control through the same control plane for a
workflow or phase.

Owner answer: Hybrid is good, but the AI coordinator should be instantiated
with a project-manager prompt and stay focused on moving toward the stated
outcome. Do not make synthesis a default coordinator responsibility because
that risks attention dilution.

Resolution: accepted as D004 in `docs/DECISION_LOG.md`.

### Q004: Coordinator Responsibilities

Question: What are the first-class coordinator responsibilities and explicit
non-responsibilities?

Recommended answer: The AI coordinator should own conversational control, goal
tracking, next-action selection, blocker triage, human checkpoint escalation,
and invoking workflow commands. It should not directly synthesize major
artifacts, write source patches, or bypass deterministic gates unless assigned
a specific job by the workflow.

Owner answer: agreed.

Resolution: accepted as D005 in `docs/DECISION_LOG.md`.

### Q005: Live Coordination Layer

Question: What should be the v1 live coordination layer: repo files,
SQLite-backed local message bus, filesystem queue outside the repo, or local
HTTP/Unix-socket service?

Recommended answer: Use SQLite as the local state store/message bus, with a CLI
API as the primary interface. Add an optional Unix-socket or local HTTP API
later for Slack, TUI, and web adapters. Repo files remain durable artifacts,
not the live communication layer.

Owner answer: SQLite was an immediate positive signal.

Resolution: accepted as D006 in `docs/DECISION_LOG.md`.

### Q006: State Store Location

Question: Where should the SQLite state store live by default: inside each repo
under `.striatum/`, in a user-level directory keyed by repo path, or
configurable per workflow?

Recommended answer: Store v1 state inside the target repo under
`.striatum/`, ignored by default. This makes runs portable with the worktree
and keeps project state easy to inspect. Add a user-level override later if
people want one dashboard across many repos.

Owner answer: accepted.

Resolution: accepted as D007 in `docs/DECISION_LOG.md`.

### Q007: Common Agent Integration Contract

Question: What is the minimum common integration contract across supported
agent CLIs?

Recommended answer: Treat the POSIX-like process boundary as the minimum
contract: launch command, cwd, env, stdin prompt/context, stdout/stderr, exit
code, and optional PTY. Model-specific adapters can add MCP, JSON events,
session resume, hooks, and sandbox controls.

Owner answer: pending.

### Q008: Agent Lifecycle

Question: What is the default agent lifecycle: one process per job, persistent
session per lane/phase, or both?

Recommended answer: Support persistent sessions as the preferred interactive
mode, with explicit task envelopes and deterministic completion handshakes.
Keep one-process-per-job as a clean fallback for headless runs, flaky sessions,
or jobs that require fresh context.

Owner input: preserving the context window seems valuable; one process per job
may throw away useful phase-level situational awareness.

Owner answer: persistent sessions preferred until role expires.

Resolution: accepted as D011 in `docs/DECISION_LOG.md`.

### Q009: Agent Control Surface

Question: How should agents signal completion, take new work, and communicate
with the coordinator: CLI commands, MCP tools, terminal text conventions, or
provider-specific hooks?

Recommended answer: Use the `striatum` CLI as the primary agent control
surface. MCP tools may wrap the same commands later. Use terminal text
conventions only for human readability, not as the source of truth.
Provider-specific hooks can improve adapters later but should not be the
portable core.

Owner input: CLI seems cleaner than MCP.

Resolution: accepted as D013 in `docs/DECISION_LOG.md`.

### Q010: SQLite Access Boundary

Question: Should agents interact with SQLite directly, or only through
`striatum` APIs/tools that own the schema and invariants?

Recommended answer: Agents should not write SQLite directly. They should
interact through MCP tools and CLI commands that enforce leases,
acknowledgements, state transitions, and artifact validation. Read-only SQL or
exported views can be allowed for debugging and introspection.

Owner input: if agents can interact with SQLite, it can serve as the message
bus.

Owner answer: create a binary that agents interact with to make updates.

Resolution: accepted as D009 in `docs/DECISION_LOG.md`.

### Q011: Agent Mutation Commands

Question: What should the `striatum` binary's minimum mutation command set
be?

Recommended answer: Start with:

- `register-session`
- `claim-next`
- `ack`
- `send`
- `block`
- `complete`
- `verdict`
- `publish-artifact`
- `heartbeat`
- `release`

Commands that claim or mutate work require session identity. Keep admin/status
commands separate from agent mutation commands.

Owner input: the agent needs to identify itself. If an agent issues
`striatum claim-next`, the work packet it gets depends on its role.

Owner answer: good enough for now, but details are still foggy.

Resolution: accepted provisionally as D023 in `docs/DECISION_LOG.md`.

### Q012: Agent Session Naming

Question: How should agent sessions be named and identified?

Recommended answer: Use an opaque stable `session_id` as the database identity,
plus a human-readable slug shaped like `<role>-<model>-<ordinal>` for tmux
windows, dashboards, logs, and prompts. Store role, model/lane, and ordinal as
separate fields instead of parsing behavior from the slug.

Owner input: maybe `role-model-ordinal`.

Owner answer: accepted.

Resolution: accepted as D012 in `docs/DECISION_LOG.md`.

### Q013: Cyclic State Graph

Question: Does `striatum` need a cyclic state graph, or is an acyclic
workflow DAG with explicit retry/revision edges enough?

Recommended answer: Use a workflow graph that is mostly DAG-shaped but permits
explicit bounded cycles for retry, revision, re-review, and human checkpoint
loops. Avoid unbounded autonomous cycles in v1.

Owner input: asked whether a cyclic state graph is needed.

Owner answer: accepted.

Resolution: accepted as D014 in `docs/DECISION_LOG.md`.

### Q014: Same-Role Parallelism

Question: How should the coordinator decide whether to run multiple agents with
the same role in parallel?

Recommended answer: For v1, require workflow-declared parallelism with disjoint
write scopes or review-only scopes. Defer AI-inferred build parallelization to
v2, where it can be mediated by dependency/write-scope analysis and explicit
human approval.

Owner input: asked how to ascertain whether a build can be parallelized and
suggested this may be v2.

Owner answer: accepted; AI-inferred build parallelization is v2.

Resolution: accepted as D015 in `docs/DECISION_LOG.md`.

### Q015: Work Packet Contents And Role Definition

Question: What exactly belongs in a work packet, and where does role definition
live?

Recommended answer: A work packet should include a context doc reference, role
reference, prompt/task instructions, expected artifacts, allowed write scope,
completion commands, stop conditions, and relevant prior messages. Role
definition should live in workflow config or reusable role docs; the task prompt
can add job-specific role emphasis.

Owner input: start with context doc. Unsure whether role definition lives there
or in the prompt. The model needs to be told "you're a code reviewer" or similar.

Owner answer: roles should be reusable; task prompts can reference them; context
docs should be generic.

Resolution: accepted as D019 in `docs/DECISION_LOG.md`.

Follow-up owner answer: hand exact work packet shape to a design team.

Resolution: deferred to D024 design-team output.

### Q016: Durable Idempotent Build Artifacts

Question: Should build outputs be idempotent and durable?

Recommended answer: Yes. Prompts, findings, syntheses, markers, and handoff
artifacts should be durable repo-published outputs so repeated runs converge
and other agents can inspect them.

Owner answer: accepted. Build process should be idempotent; prompts, findings,
and similar artifacts are durable.

Resolution: accepted as D016 in `docs/DECISION_LOG.md`.

### Q017: Coordinator Skills And Prompt Execution

Question: How should coordinator chat commands like "read prompt foo" work?

Recommended answer: Treat them as coordinator skills backed by deterministic
CLI operations: resolve prompt artifact, load allowed context, create or enqueue
a work packet, and ask for human confirmation when the action starts workflow
execution or writes files.

Owner input: prompt should contain the specific directions; maybe a coordinator
skill.

Owner answer: agreed.

Resolution: accepted as D020 in `docs/DECISION_LOG.md`.

### Q018: Interaction Surfaces

Question: What interaction surfaces are v1 and later?

Recommended answer: V1 CLI plus tmux introspection; TUI next; Slack and web
dashboard/chat later.

Owner answer: accepted.

Resolution: accepted as D025 in `docs/DECISION_LOG.md`.

### Q019: Branches And Commits

Question: How should branches and commits work?

Recommended answer: Coordinator starts or selects a branch and requests a
commit from the human. It does not commit autonomously by default.

Owner answer: accepted.

Resolution: accepted as D017 in `docs/DECISION_LOG.md`.

### Q020: Implementation Language

Question: What implementation language should v1 use?

Recommended answer: Python.

Owner answer: accepted.

Resolution: accepted as D018 in `docs/DECISION_LOG.md`.

### Q022: Native Sub-Agents

Question: How should `striatum` handle native sub-agents spawned by
supported agent CLIs?

Recommended answer: Treat native sub-agents as an internal implementation
detail of the parent agent session in v1. The parent session remains
accountable for write scope, artifacts, completion, and messages. If a sub-agent
needs independent auditability, queue claims, or parallel scheduling,
`striatum` should register it as a first-class session instead.

Owner input: the respective agent CLI tools can spawn sub-agents.

Owner answer: parent agent is responsible.

Resolution: accepted as D021 in `docs/DECISION_LOG.md`.

### Q023: Branch Confirmation

Question: Should branch creation happen automatically or with confirmation?

Recommended answer: Confirm branch creation or branch selection at run start.

Owner answer: accepted; confirm branch on start.

Resolution: accepted as D026 in `docs/DECISION_LOG.md`.

### Q024: Workflow Config Format

Question: What format should workflow configuration use?

Recommended answer: JSON.

Owner answer: JSON forever; no YAML.

Resolution: accepted as D027 in `docs/DECISION_LOG.md`.

### Q025: Artifact Policy

Question: What is the repo artifact policy?

Recommended answer: Publish decisions, prompts, findings, syntheses, markers,
and handoffs; do not capture or publish transcripts by default.

Owner answer: agreed. Not even transcripts. Decisions for sure. The coordinator
will need to record decisions.

Resolution: accepted as D028 in `docs/DECISION_LOG.md`.

### Q026: Fresh Session Policy

Question: When should fresh sessions be used?

Recommended answer: Fresh context means new role instantiation. Multi-pass
reviews until consensus and builds until complete are persistent by default.

Owner answer: accepted.

Resolution: accepted as D029 in `docs/DECISION_LOG.md`.

### Q027: First Validation Workflow

Question: What should the first validation workflow be?

Recommended answer: RFC-ledger cleanup.

Owner answer: accepted.

Resolution: accepted as D030 in `docs/DECISION_LOG.md`.

### Q028: Project Structure

Question: What project structure should `striatum` emulate?

Recommended answer: Emulate Engram's Python project discipline where
appropriate.

Owner answer: accepted.

Resolution: accepted as D031 in `docs/DECISION_LOG.md`.

### Q029: Three-Model Design Input

Question: Should the one-shot MVP process require design input from all three
model lanes?

Recommended answer: Yes. Require separate Claude, Codex, and Gemini design
inputs before synthesis/build.

Owner answer: accepted. The one-shot should involve design input from all three
models.

Resolution: accepted as D032 in `docs/DECISION_LOG.md`.

### Q030: Bootstrap Tmux Runner

Question: Should the Engram tmux runner be reused as bootstrap orchestration for
the `striatum` one-shot?

Recommended answer: Allow reuse or adaptation of the Engram tmux runner as a
temporary bootstrap harness for the three-model design/build pass, but keep it
out of the product core. The product should implement its own generic tmux/PTY
adapter after design review.

Owner input: we might need to reuse the tmux runner.

Owner answer: pending.

### Q031: Engram Incubation

Question: Should `striatum` be incubated inside Engram before being split
out?

Recommended answer: Yes. Commit it under `engram/striatum` for MVP context,
then split it into a separate project after MVP validation.

Owner answer: accepted.

Resolution: accepted as D033 in `docs/DECISION_LOG.md`.
