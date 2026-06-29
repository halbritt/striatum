# Codex CLI — research for RFC 0010

Date: 2026-05-08

This note feeds the `codex_default` profile candidate for RFC 0010 (Tool
Harness Profiles). It is a docs-only research artifact; it does not modify
RFC 0010, lane fixtures, or source. All public sources are cited inline.

## Native delegation features

### Invocation modes

Codex CLI is distributed as `codex` (npm `@openai/codex`) and exposes two
primary surfaces relevant to a Striatum lane:

- `codex` — interactive TUI; not appropriate for the Striatum process
  adapter except behind a PTY.
- `codex exec "<prompt>"` — the documented non-interactive entry. From the
  upstream reference: "Run tasks without human interaction." It runs a
  single session to completion, emits events to stdout/stderr, and exits
  when the agent decides the task is done. (`developers.openai.com/codex/noninteractive`,
  `developers.openai.com/codex/cli/reference`,
  `deepwiki.com/openai/codex/4.2-headless-execution-mode-(codex-exec)`).

Stdin / stdout / stderr semantics for `codex exec`:

- **stdin**: if a prompt argument is also passed, stdin is treated as
  *additional context*; passing `-` as the prompt makes stdin the full
  prompt (`developers.openai.com/codex/noninteractive`).
- **stdout (default)**: only the final agent message is printed.
- **stdout (`--json`)**: newline-delimited JSON event stream (one event
  per state change). Documented event types: `thread.started`,
  `turn.started`, `turn.completed`, `turn.failed`, `item.started`,
  `item.completed`, `error`. Item types include agent messages,
  reasoning, command executions, file changes, MCP tool calls, web
  searches, and plan updates.
- **stderr**: progress lines while the run is in flight.
- **`--output-last-message <path>` / `-o`**: writes the final assistant
  message to the given file in addition to stdout.
- **`--output-schema <path>`**: forces the final response to conform to a
  JSON Schema — useful for structured artifacts.
- **Exit code**: non-zero on task submission failure or git apply
  conflicts; documented as the standard way scripts decide success
  (`developers.openai.com/codex/cli/reference`).
- **`--ephemeral`**: skip persisting the session rollout to disk.
- **`--skip-git-repo-check`**: bypass the "must be in a git repo" guard.
- **`--ignore-user-config` / `--ignore-rules`**: skip
  `$CODEX_HOME/config.toml` and execpolicy `.rules` respectively.
- **Sandbox**: `--sandbox read-only|workspace-write|danger-full-access`
  (default `read-only` per upstream reference; the interactive default is
  documented as auto/workspace-write but `codex exec`'s default is
  read-only unless overridden).
- **Approvals**: configure via `-c approval_policy=untrusted|on-request|never`
  (Codex 0.130.0 removed the standalone `--ask-for-approval` flag on
  `codex exec`; the `approval_policy` config key remains). The
  `--dangerously-bypass-approvals-and-sandbox` / `--yolo` flags remain
  for full policy-bypass.

A separate **Codex SDK** (`@openai/codex-sdk` / Python `AsyncCodex`,
experimental) talks to a local app-server over JSON-RPC and exposes
threads programmatically (`developers.openai.com/codex/sdk`). This is a
plausible later upgrade path for Striatum but does not change the V1
process-adapter contract.

**TTY requirements (RFC 0009 supervisor relevance)**: `codex exec` is
explicitly designed for non-TTY use — stderr progress + stdout final
message. It does not require a PTY. Long-lived supervised use is
plausible because Codex also exposes `codex app-server --listen
stdio://`, which speaks JSON-RPC over stdio, but that is a separate
shape from `codex exec` and is out of scope for V1.

### Agent loop / orchestration

Background reading: OpenAI's "Unrolling the Codex agent loop"
(`openai.com/index/unrolling-the-codex-agent-loop/`); WebFetch returned
403, content reconstructed from the Hacker News and ZenML LLMOps mirrors
(`news.ycombinator.com/item?id=46737630`,
`zenml.io/llmops-database/building-production-ready-ai-agents-openai-codex-cli-architecture-and-agent-loop-design`).

Loop shape: each turn assembles inputs, runs inference, executes any
requested tools, and feeds tool output back into context. A turn ends
when the model returns an assistant message rather than another tool
call. Subsequent user messages extend the same conversation.

Configurable knobs that matter for a Striatum lane:

- `auto_compact_limit` — token threshold at which Codex calls the
  internal `/responses/compact` endpoint to lossy-compress history.
  Compaction returns a smaller list of items including an opaque
  `encrypted_content` element that preserves "the model's latent
  understanding."
- `model_context_window` — token cap available to the active model.
- `model_reasoning_effort` — `minimal | low | medium | high | xhigh`.
- `model_reasoning_summary` — `auto | concise | detailed | none`.
- `plan_mode_reasoning_effort` — separate override for plan mode.
- `agents.job_max_runtime_seconds` — per-worker timeout for batch
  spawn jobs (default 1800s).
- Per-agent `model`, `model_reasoning_effort`, `sandbox_mode` overrides
  in custom-agent TOML.

Codex does not document a public "max iterations" or hard tool-call
budget for the top-level loop. Hacker News commenters explicitly call
out the absence of intervention hooks beyond the lifecycle hook system
(see below) and note that reasoning tokens persist across tool turns
but are dropped between user turns. Practical orchestrators in the wild
enforce their own iteration cap by wrapping `codex exec` in a controller
that tracks turn count or runtime
(`codex.danielvaughan.com/2026/04/18/running-multiple-codex-agents-parallel-orchestration/`).

### Custom agent roles

Codex supports persistent custom agent definitions
(`developers.openai.com/codex/subagents`). Definition format is
**standalone TOML files**, one per agent.

Required fields:

- `name` — identifier used when Codex spawns or references the agent.
- `description` — when Codex should pick it.
- `developer_instructions` — core behavioral directives.

Optional fields, all of which override the parent's session settings
when set:

- `model`
- `model_reasoning_effort`
- `sandbox_mode`
- `nickname_candidates = ["Atlas", "Delta", "Echo"]` — display names
  cycled when many instances of the same agent run in parallel.
- `mcp_servers` — per-agent MCP server allowlist.
- `skills.config` — per-agent skill enablement.

Scope:

- Personal: `~/.codex/agents/` (or `$CODEX_HOME/agents/`).
- Project: `.codex/agents/` (only loaded if the project is `trusted` per
  `projects.<path>.trust_level`).

Persistence: definitions live on disk; they survive across invocations
and are picked up at startup. Per-session "spawned" agents do not
persist beyond the session — only the definitions do.

Example from upstream docs:

```toml
name = "reviewer"
description = "PR reviewer focused on correctness, security, and missing tests."
developer_instructions = """
Review code like an owner.
Prioritize correctness, security, behavior regressions, and missing test coverage.
"""
nickname_candidates = ["Atlas", "Delta", "Echo"]
model = "gpt-5.4"
sandbox_mode = "read-only"
```

Invocation: "Codex only spawns a new agent when you explicitly ask it to
do so." That is, the parent agent must natural-language-decide to spawn
based on user prompt and the loaded `description` of each agent. There
is also a slash command `/agent` to inspect, switch, steer, stop, or
close threads in the interactive TUI.

### Skills / tools

Historical note: this research predated Striatum's current Codex profile.
Current Striatum installs Codex-facing skill guidance as flat agent docs under
`.codex/agents/striatum-*.md` for project scope or
`~/.codex/agents/striatum-*.md` for user scope, matching
`src/striatum/skills/install.py` and the regression tests.

Current Striatum install scopes:

| Scope | Path |
|-------|------|
| Repository/project | `.codex/agents/striatum-*.md` |
| User | `~/.codex/agents/striatum-*.md` |

Selection: progressive disclosure. Codex loads only `name` +
`description` + path at startup (capped at ~2% of context, ~8 KB), and
loads the full SKILL.md only when it picks the skill. Explicit
selection via `/skills` or `$skill-name` mention.

Restriction: `[[skills.config]] path = "..." enabled = false` in
`config.toml`, plus optional `agents/openai.yaml` with
`allow_implicit_invocation: false` for explicit-only.

Default Codex tool surface (controllable via `features.*` in
`config.toml`): `features.shell_tool` (default on),
`features.unified_exec` (PTY-backed exec, default on except Windows),
`features.multi_agent` (default on; gates the
`spawn_agent`/`send_input`/`resume_agent`/`wait_agent`/`close_agent`
tools per `developers.openai.com/codex/config-reference`),
`features.computer_use`, `features.browser_use`, `features.memories`,
`features.undo`, `features.codex_hooks`.

### Multiple isolated workspaces

Codex's official answer for parallel agents is the **Codex App
Worktrees** feature in the desktop/UI surface
(`developers.openai.com/codex/app/worktrees`,
`verdent.ai/guides/codex-app-worktrees-explained`,
`macaron.im/blog/codex-app-worktrees-parallel-agents`). For the CLI
specifically, the published 2026 guidance is to use **git worktrees +
separate `CODEX_HOME` per worker**.

Concrete worktree pattern from public guides
(`docs.bswen.com/blog/2026-04-28-codex-parallel-worktrees/`,
`particula.tech/blog/parallel-coding-agents-worktree-pattern-oh-my-codex`,
`codex.danielvaughan.com/2026/04/18/running-multiple-codex-agents-parallel-orchestration/`):

```bash
git worktree add ../agent-auth -b feature/auth main
git worktree add ../agent-api  -b feature/api  main
# Per-worker CODEX_HOME isolates rollouts, history, sessions, hooks
CODEX_HOME=$(pwd)/.codex-auth codex exec --sandbox workspace-write \
  --skip-git-repo-check --ephemeral - < packet.json
```

Filesystem isolation semantics:

- `git worktree` gives each worker its own working tree, HEAD, and
  staging area; the `.git` object database is shared. The known
  hazards are still the global git index/HEAD if two workers share a
  worktree (`dev.to/skeptrune/llm-codegen-go-brrr-parallelization-with-git-worktrees-and-tmux-2gop`).
- `CODEX_HOME` reroutes rollout files (`$CODEX_HOME/sessions/...`),
  history, hooks, auth, and the session SQLite state DB. Without that,
  parallel `codex exec` instances read each other's session files and
  cross-pollinate context (open issue
  `github.com/openai/codex/issues/11435`).
- `--ephemeral` further suppresses rollout persistence, so a worker
  cannot leak even via the rollout.

**Striatum integration / RFC 0008 overlap**: RFC 0008's `worktree_required`
flag and `striatum worktree create` already give us the git-worktree
half. The new requirement Codex imposes is **per-job `CODEX_HOME`**.
The cleanest place for that is the lane's adapter `env` block, with a
tokenised value such as `${STRIATUM_SCRATCH_DIR}/codex-home`. This
keeps the two isolations layered: RFC 0008 owns the working tree, the
harness profile owns the agent state directory.

### MCP support

Codex consumes MCP servers as a first-class config surface
(`developers.openai.com/codex/mcp`). Configuration lives in
`config.toml` under `[mcp_servers.<id>]`:

```toml
[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
env_vars = ["LOCAL_TOKEN"]
cwd = "/path/to/dir"
startup_timeout_sec = 10
tool_timeout_sec = 60
enabled = true
required = false
enabled_tools = ["tool1", "tool2"]
disabled_tools = ["tool3"]

[mcp_servers.context7.env]
MY_VAR = "value"

[mcp_servers.figma]
url = "https://mcp.figma.com/mcp"
bearer_token_env_var = "FIGMA_OAUTH_TOKEN"
http_headers = { "X-Header" = "value" }
```

Highlights:

- Stdio and streamable-HTTP transports.
- `enabled_tools`/`disabled_tools` allow per-server tool gating.
- `required = true` fails startup if the server cannot initialize.
- Custom agents can scope their own `mcp_servers` allow-list, so a
  `reviewer` subagent can be given a smaller tool surface than its
  parent.
- CLI surface: `codex mcp add <name> -- <command>`,
  `codex mcp list [--json]`, `codex mcp login <name>` for OAuth.
- A known papercut: the `--with-api-key` and various MCP edits
  require a **CLI restart** to take effect; the GitHub issue
  `openai/codex#3441` documents config.toml MCP servers being silently
  ignored by long-running processes until restart.

### Hooks / lifecycle events

Codex ships a hook engine
(`developers.openai.com/codex/hooks`,
`agenticcontrolplane.com/blog/codex-cli-hooks-reference`). Enable with
`[features] codex_hooks = true`. Configure either inline in
`config.toml` `[hooks]` tables or in a sibling `hooks.json`.

Documented events:

- `SessionStart` — session begin/resume.
- `UserPromptSubmit` — user submits a prompt.
- `PreToolUse` — gatekeeper before a tool call (Bash, `apply_patch`,
  MCP).
- `PermissionRequest` — when approval is needed.
- `PostToolUse` — after tool execution.
- `Stop` — conversation halts.

Common hook input fields: `session_id`, `transcript_path`, `cwd`,
`hook_event_name`, `model`. Turn-scoped events also include `turn_id`.

Common output fields:

- `continue` (boolean) — if `false`, marks the hook as stopped.
- `stopReason` (string) — recorded reason.
- `systemMessage` (string) — surfaced in UI / log.
- `suppressOutput` — parsed but currently a no-op.
- `hookSpecificOutput.decision` for `PermissionRequest` (allow/deny)
  and `PostToolUse` (`block`).

Hook scripts are arbitrary executables that read JSON on stdin and
emit JSON on stdout; this is identical in spirit to Claude Code's
hook surface, so a Striatum-side bridge could be reused.

### Session resume

`codex exec resume [SESSION_ID]` (and `--last` / `--all`) plus
`codex resume` for the interactive TUI
(`developers.openai.com/codex/cli/reference`,
`deepwiki.com/openai/codex/4.4-session-resumption-and-forking`). State
lives in `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl`; resuming
appends to the same file, preserving the original session ID. There is
also `codex fork [--last]` to branch a session into a new thread, and
`codex apply <task-id>` to drag a Cloud task's diff into a local
checkout.

The RolloutRecorder's JSONL format is the source of truth for resume.
This means a Striatum lane that wants resume must keep the per-job
`CODEX_HOME` *durable* (not in `/tmp`) — but if the lane prefers a
strict no-transcript posture, `--ephemeral` skips rollout persistence
entirely and forfeits resume. Striatum's D028 transcript-off default
suggests `--ephemeral` is the right baseline, with an opt-in to
durable rollout for jobs that explicitly need it.

### Multi-instance behavior

Open issue `github.com/openai/codex/issues/11435` documents the failure
mode plainly: parallel `codex exec` instances **share session state via
`~/.codex/`** and "detect each other's session files and attempt to
restore from them," producing context corruption, unwanted resume
prompts, and non-deterministic CI failures. The reporter's three
proposed fixes (per-PID directories, disable restore in non-interactive
mode, file locking) are not yet shipped as of public 2026 docs.

Public mitigations actually in use:

1. **Distinct `CODEX_HOME` per worker** — bypasses the bug entirely
   because each worker writes to its own sessions directory.
2. **`--ephemeral`** — skips rollout persistence, which alone removes
   the cross-read vector.
3. **Git worktree per worker** — addresses the orthogonal git index/HEAD
   collision.

For a Striatum lane, **(1) + (2) + (3) together** are the safest
combination, and only (3) overlaps RFC 0008.

### Permissions / sandbox

Sandbox modes (`developers.openai.com/codex/cli/reference`,
`developers.openai.com/codex/agent-approvals-security`):

- `read-only` — default for `codex exec`.
- `workspace-write` — writes confined to declared `writable_roots`,
  optionally network-on, with `exclude_slash_tmp` /
  `exclude_tmpdir_env_var` toggles.
- `danger-full-access` — no sandbox.

Enforcement is OS-level
(`pierce.dev/notes/a-deep-dive-on-agent-sandboxes`,
`gist.github.com/rtzll/8ec03ad8a4cca3ae43ce3db7eb7dcc09`):

- **Linux**: Landlock (kernel 5.13+) for filesystem caps + seccomp-BPF
  to block `connect`/`bind`/`sendto` and most network syscalls;
  `AF_UNIX` allowed; namespaces for process/network/mount/user.
- **macOS**: Seatbelt via `sandbox-exec` profiles. Default-deny; net
  off unless explicitly granted. The known papercut
  `openai/codex#10390` notes that `network_access = true` is silently
  ignored on macOS in some configurations.

Approval policy: `untrusted | on-request | never` plus a granular
table mode for category-level rules (`sandbox_approval`, `rules`,
`mcp_elicitations`, `request_permissions`, `skill_approval`). The
`approvals_reviewer = "auto_review"` setting plus
`[auto_review].policy` lets a hook/policy file take the human's place.

`permissions.<name>` profiles let an operator declare a reusable
filesystem/network policy that any sandbox-enabled command consumes,
with named built-ins `:read-only`, `:workspace`, `:danger-no-sandbox`.
Network mode is `limited | full`, with domain allow/deny maps and
unix-socket maps.

Striatum's adapter contract maps cleanly:

- `network=forbidden` → `--sandbox read-only` or `--sandbox workspace-write`
  with `sandbox_workspace_write.network_access = false`. Codex
  enforces this at the OS level on Linux/macOS, so the Striatum lane
  can graduate `network=forbidden` from `advisory_strict` to
  effectively `enforced` at the Codex layer (without changing the
  process adapter's own enforcement claim).
- `repo_scope=local_only` → `--sandbox workspace-write` with the
  worktree path as the only writable root. Same enforcement story.
- `transcripts=off` → `--ephemeral` + `--ignore-user-config` to keep
  Codex from writing rollouts and from honouring a user-side config
  that re-enables them.

## How teams use it in the wild

- **Codex App Worktrees** is OpenAI's own first-party recipe for
  "create N branches and let N agents go" in the desktop product
  (`developers.openai.com/codex/app/worktrees`,
  `verdent.ai/guides/codex-app-worktrees-explained`).
- **`oh-my-codex` pattern** — community git-worktree-per-task launcher
  (`particula.tech/blog/parallel-coding-agents-worktree-pattern-oh-my-codex`).
- **`codex-orchestrator` and `codex-yolo`** — tmux-based fan-out
  patterns that wrap `codex` in shell controllers, demonstrating that
  external orchestration is the dominant pattern when more than one or
  two agents run together
  (`codex.danielvaughan.com/2026/04/18/running-multiple-codex-agents-parallel-orchestration/`).
- **Daniel Vaughan's blog** consistently recommends per-agent token
  budgets (180k frontend, 280k backend) and an 8-iteration cap with
  forced reflection — both knobs that Striatum could surface as
  profile-level guidance even without Codex enforcing them directly.
- **`hatayama/codex-hooks`** — community runner that lets Codex reuse
  Claude Code hooks settings, signalling that hook portability matters
  to operators
  (`github.com/hatayama/codex-hooks`).
- **`ComposioHQ/awesome-codex-skills`** and **`openai/skills`** — the
  curated catalogues of `SKILL.md`-shaped workflows
  (`github.com/ComposioHQ/awesome-codex-skills`,
  `github.com/openai/skills`).

Common takeaways across these:

1. Per-worker isolation is achieved by **filesystem separation**
   (worktree + `CODEX_HOME`), not by a Codex-internal scheduler.
2. Custom agents are real but invoked by *natural-language* decisions
   from the parent — there is no `--use-agent reviewer` CLI flag in
   `codex exec`.
3. Hooks are the most-requested-but-still-incomplete extension surface;
   community tooling is filling the gap.

## Mapping to RFC 0010 schema

### `codex_default` profile

```json
{
  "tool_family": "codex",
  "strategy_version": "2026-05-08",
  "native_delegation": {
    "mode": "encouraged",
    "instruction": "Spawn additional Codex agents only when each delegated task is independently bounded by the work packet's write scope. Prefer review-only or research delegations before broad repo-write delegations. Wait for all spawned agents before publishing artifacts.",
    "max_parallel_native_agents": "tool_default"
  },
  "feature_flags": {
    "subagents": "allowed",
    "agent_teams": "unsupported",
    "skills": "allowed",
    "custom_agent_roles": "allowed",
    "hooks": "allowed",
    "mcp_tools": "allowed",
    "headless": "required_for_lane",
    "worktree_agents": "preferred"
  },
  "accountability": {
    "native_subagents": "internal_to_parent_session",
    "first_class_registration": "not_supported"
  },
  "agent_loop_budget": {
    "auto_compact_limit": "tool_default",
    "model_reasoning_effort": "medium",
    "max_runtime_seconds_per_subagent": 1800,
    "max_threads": 6,
    "max_depth": 1
  },
  "workspace_isolation": {
    "git_worktree": "required",
    "codex_home_per_job": "required",
    "rollout_persistence": "ephemeral"
  },
  "session_resume": "off",
  "fallback_profile_id": "generic_default",
  "prompt_envelope_path": null
}
```

Justification for each value:

- `tool_family: "codex"` — namespaces the profile to a specific tool
  family without tying it to a model id.
- `strategy_version: "2026-05-08"` — date of this research note.
- `native_delegation.mode: "encouraged"` — Codex subagents are
  first-class and the documented "spawn one per point and summarize"
  pattern works well, but they are not always cheaper than a single
  pass; "encouraged" beats "required."
- `max_parallel_native_agents: "tool_default"` — defers to
  `agents.max_threads` (default 6). Striatum should not double-cap.
- `feature_flags.subagents: "allowed"` — supported via
  `features.multi_agent` + `[agents]` config.
- `feature_flags.agent_teams: "unsupported"` — Codex has no public
  "agent teams" concept comparable to Claude Code's; subagents are the
  only delegation primitive.
- `feature_flags.skills: "allowed"` — Striatum writes Codex profile docs to
  `.codex/agents/` for project scope and `~/.codex/agents/` for user scope;
  project-scoped docs survive Striatum worktree creation as long as they are
  committed.
- `feature_flags.custom_agent_roles: "allowed"` — `.codex/agents/*.toml`
  and `~/.codex/agents/*.toml`.
- `feature_flags.hooks: "allowed"` — `[features] codex_hooks = true`
  plus `hooks.json`. We allow but do not require, because Striatum
  already owns lifecycle through CLI commands.
- `feature_flags.mcp_tools: "allowed"` — `[mcp_servers.*]`.
- `feature_flags.headless: "required_for_lane"` — the lane *must* run
  `codex exec`, never the TUI.
- `feature_flags.worktree_agents: "preferred"` — git worktrees plus
  per-worker `CODEX_HOME` are the only safe parallel pattern today.
- `accountability.*` — unchanged from the RFC's V1 stance; Codex
  subagents are not Striatum sessions.
- **New** `agent_loop_budget.*` — exposes Codex's loop knobs at profile
  level. RFC 0010 v1 currently has no place for compaction limits or
  reasoning effort; without them Striatum cannot record (or change)
  the bounds on a Codex run.
- **New** `workspace_isolation.*` — encodes the
  worktree+`CODEX_HOME`+`--ephemeral` triple needed to safely run
  parallel `codex exec` after issue #11435.
- **New** `session_resume: "off"` — pairs with `--ephemeral`; an
  alternative `"durable"` value would imply a stable per-session
  `CODEX_HOME` and require RFC 0009 supervisor coordination.
- `fallback_profile_id: "generic_default"` — drops to the generic
  process lane if Codex is unavailable.

### Proposed RFC 0010 schema additions for Codex

The current RFC 0010 draft is intentionally minimal. The Codex evidence
suggests three additions worth raising in the open-questions list, in
priority order:

1. **`agent_loop_budget`** — `auto_compact_limit`,
   `model_reasoning_effort`, per-subagent runtime/turn caps, and a
   `max_threads` cap. Codex exposes all of these and the absence of
   them in V1 makes it impossible to encode the documented community
   guardrails (8-iteration cap, 180k/280k token budgets) in a profile.
2. **`workspace_isolation`** — `git_worktree`,
   `codex_home_per_job` / `state_dir_per_job`, and
   `rollout_persistence` (ephemeral|durable). This sits adjacent to
   RFC 0008 but is its own concern: RFC 0008 isolates the working
   tree, this isolates the *agent state directory*, which is the
   actual root cause of issue #11435.
3. **`session_resume`** (off|durable) — explicit choice, because
   "ephemeral" and "resume" are mutually exclusive on Codex and the
   profile is the right place to lock that choice.

A lighter alternative is to encode (1)–(3) as free-form keys under
`feature_flags` for V1 and lift them into typed fields once a second
provider needs them. The profile JSON above uses dedicated keys to
make the proposal concrete; either packaging is fine for the RFC.

## Recommended Striatum lane configuration

Workflow lane block, including the new `harness_profile_id`:

```json
{
  "lanes": {
    "codex": {
      "adapter": "process",
      "harness_profile_id": "codex_default",
      "worktree_isolation": "per_job",
      "command": [
        "codex",
        "exec",
        "--json",
        "--ephemeral",
        "--skip-git-repo-check",
        "--sandbox", "workspace-write",
        "-c", "approval_policy=never",
        "--ignore-user-config",
        "-"
      ],
      "stdin_mode": "packet",
      "env": {
        "CODEX_HOME": "${STRIATUM_SCRATCH_DIR}/codex-home",
        "CODEX_API_KEY": "${CODEX_API_KEY}"
      },
      "constraints": {
        "network": "forbidden",
        "transcripts": "off",
        "repo_scope": "local_only"
      },
      "required_enforcement": {
        "transcripts": "enforced",
        "network": "advisory_strict",
        "repo_scope": "advisory_strict"
      }
    }
  }
}
```

Why each piece:

- `command[0..1] = ["codex", "exec"]` — non-interactive entry per
  upstream reference.
- `--json` — captures the structured event stream so a future Striatum
  observer can ingest tool-call events without parsing terminal text
  (still optional; Striatum does not require it for state).
- `--ephemeral` — no rollout written to disk. Aligns with D028
  (transcripts off) and prevents issue #11435 cross-reads.
- `--skip-git-repo-check` — Striatum's worktree is a real worktree but
  may live outside the operator's expectation; safe to bypass the
  guard.
- `--sandbox workspace-write` — so Codex can edit files under the
  worktree. With `sandbox_workspace_write.network_access = false` (in
  the per-job config.toml or via `--config`), network is denied at the
  OS level on Linux/macOS.
- `-c approval_policy=never` — Striatum is the operator; the human is
  not at the terminal. (Codex 0.130.0 removed the `--ask-for-approval`
  flag from `codex exec`; the `approval_policy` config key still works
  via `-c`.)
- `--ignore-user-config` — pin behaviour to the per-job CODEX_HOME's
  config; ignore whatever the operator has in their home.
- Trailing `-` — consume the work packet from stdin (stdin_mode
  `packet`).
- `stdin_mode: "packet"` — matches Striatum's existing
  `process_executions.stdin_mode` constraint
  (`src/striatum/process_adapter.py`).
- `env.CODEX_HOME` — points at a per-job state directory inside the
  process adapter's scratch path, eliminating the issue #11435 race
  without touching shared user state. (`STRIATUM_SCRATCH_DIR` is the
  conventional name; the actual env var is whatever
  `run_process_adapter` exports for the scratch path. If no such var
  exists today, this profile motivates adding one.)
- `env.CODEX_API_KEY` — operator-supplied secret; passed through
  unchanged.
- `constraints.network = "forbidden"` and `repo_scope = "local_only"` —
  the existing process-adapter posture, with Codex enforcing them at
  the kernel level. The Striatum-side enforcement claim stays
  `advisory_strict` because Striatum itself does not enforce; Codex
  does. We could optionally introduce a per-lane `enforcement_layer`
  field to record that the harness rather than the adapter is the
  enforcer, but RFC 0010 V1 does not require it.
- `transcripts = "off"` / `enforced` — `--ephemeral` plus suppressed
  stdio plus `--ignore-user-config` is genuinely enforced; nothing
  about the lane makes Codex write transcripts.

Parallelism story:

- N parallel `codex exec` workers, each in its own RFC 0008 worktree
  with `STRIATUM_SCRATCH_DIR` distinct, get distinct `CODEX_HOME` and
  therefore do not corrupt each other's session state.
- `agents.max_threads = 6` and `agents.max_depth = 1` are Codex
  defaults; the profile leaves them alone unless a workflow author
  overrides via `agent_loop_budget`.
- The work-packet `harness_profile` block (RFC 0010, work-packet
  exposure) carries the "spawn the maximum number of useful agents"
  instruction so Codex picks the parallelism level rather than
  Striatum trying to legislate it.

Per-job custom-agent / skill / hook layout (project-scoped, lives in
the worktree):

```
<worktree>/.codex/
  agents/
    reviewer.toml
    fixer.toml
  hooks.json
<worktree>/.agents/
  skills/
    striatum-publish-artifact/
      SKILL.md
```

The job's worktree-create step optionally seeds these from a target
repo or fixture path. They are picked up by Codex automatically because
the per-job `CODEX_HOME` lives under the worktree (so trust applies)
and `.codex/` / `.agents/` are at the project root.

## Gaps and risks

- **Issue #11435 is unresolved upstream**. The mitigation
  (per-`CODEX_HOME`) works empirically, but Codex could change session
  discovery in a way that re-opens the cross-read. The profile should
  carry a `strategy_version` so a regression can be flagged.
- **`--ignore-user-config` is broad**. It also disables hooks from the
  operator's `~/.codex/hooks.json`, which may surprise users who expect
  their global guardrails to apply to Striatum jobs. The profile
  trade-off is determinism over user customisation; this should be
  called out in operator docs.
- **`features.multi_agent` is a global toggle in `config.toml`**. We
  rely on it being on by default; if an operator's
  `~/.codex/config.toml` flips it off, `--ignore-user-config` saves us
  but only because we ignore the operator config entirely.
- **Codex SDK is the better long-term shape**. Once the Python SDK
  graduates from experimental, an alternative `harness_kind: "sdk"`
  lane could replace the `codex exec` invocation with an in-process
  Codex thread, removing the JSON-stream parsing and giving Striatum
  programmatic resume. RFC 0010's `tool_family` is correctly tool-, not
  invocation-shape-, scoped, so the same `codex_default` profile could
  describe both lane shapes.
- **Skills path divergence resolved for Striatum** — older research compared
  `.agents/skills` and `$CODEX_HOME/skills`. Striatum now pins its generated
  Codex profile docs to `.codex/agents/` / `~/.codex/agents/` and tests that
  install shape directly.
- **Resume vs ephemeral**. `--ephemeral` forecloses `codex exec resume`.
  If a Striatum lane wants long-running supervised Codex (RFC 0009), it
  must drop `--ephemeral` *and* keep `CODEX_HOME` durable across
  packets. That is the supervisor-mode profile, distinct from this
  one.
- **Hook surface still maturing**. Several documented behaviors
  (`continue: false` outside Stop, `suppressOutput`, `additionalContext`
  in `PreToolUse`) are partially implemented per upstream issues
  `openai/codex#14882` and `openai/codex#19385`. We do not rely on
  them for V1.
- **Sandbox papercut on macOS** (`openai/codex#10390`) means
  `sandbox_workspace_write.network_access` may be silently ignored on
  some macOS configs. Linux is the safer bet for the strict
  `network=forbidden` lane.

## Sources

- `developers.openai.com/codex/cli`
- `developers.openai.com/codex/cli/reference`
- `developers.openai.com/codex/cli/features`
- `developers.openai.com/codex/noninteractive`
- `developers.openai.com/codex/subagents`
- `developers.openai.com/codex/skills`
- `developers.openai.com/codex/mcp`
- `developers.openai.com/codex/hooks`
- `developers.openai.com/codex/config-reference`
- `developers.openai.com/codex/config-advanced`
- `developers.openai.com/codex/concepts/customization`
- `developers.openai.com/codex/agent-approvals-security`
- `developers.openai.com/codex/guides/agents-md`
- `developers.openai.com/codex/sdk`
- `developers.openai.com/codex/app/worktrees`
- `openai.com/index/unrolling-the-codex-agent-loop/` (mirrored via
  `news.ycombinator.com/item?id=46737630` and
  `zenml.io/llmops-database/building-production-ready-ai-agents-openai-codex-cli-architecture-and-agent-loop-design`)
- `deepwiki.com/openai/codex/4.2-headless-execution-mode-(codex-exec)`
- `deepwiki.com/openai/codex/4.4-session-resumption-and-forking`
- `github.com/openai/codex` (`docs/exec.md`, `docs/skills.md`,
  `docs/config.md`)
- `github.com/openai/codex/issues/11435` (parallel `codex exec`
  session-restore race)
- `github.com/openai/codex/issues/3441` (config.toml MCP servers
  ignored without restart)
- `github.com/openai/codex/issues/10390` (macOS Seatbelt
  `network_access` ignored)
- `github.com/openai/codex/issues/14882` (proposal for
  PreToolUse/PostToolUse parity)
- `github.com/openai/codex/issues/19385` (`additionalContext` parity)
- `github.com/openai/skills`
- `github.com/ComposioHQ/awesome-codex-skills`
- `github.com/hatayama/codex-hooks`
- `agenticcontrolplane.com/blog/codex-cli-hooks-reference`
- `pierce.dev/notes/a-deep-dive-on-agent-sandboxes`
- `gist.github.com/rtzll/8ec03ad8a4cca3ae43ce3db7eb7dcc09` (Codex
  sandboxing notes)
- `codex.danielvaughan.com/2026/04/18/running-multiple-codex-agents-parallel-orchestration/`
- `codex.danielvaughan.com/2026/04/08/cross-surface-session-sync/`
- `codex.danielvaughan.com/2026/03/31/codex-cli-network-security-requirements-toml/`
- `docs.bswen.com/blog/2026-04-28-codex-parallel-worktrees/`
- `particula.tech/blog/parallel-coding-agents-worktree-pattern-oh-my-codex`
- `verdent.ai/guides/codex-app-worktrees-explained`
- `macaron.im/blog/codex-app-worktrees-parallel-agents`
- `dev.to/skeptrune/llm-codegen-go-brrr-parallelization-with-git-worktrees-and-tmux-2gop`
- `blog.fsck.com/2025/12/19/codex-skills/`
- `simonwillison.net/2025/Dec/12/openai-skills/`
- `vladimirsiedykh.com/blog/codex-mcp-config-toml-shared-configuration-cli-vscode-setup-2025`
