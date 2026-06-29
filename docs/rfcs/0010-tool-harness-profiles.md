# RFC 0010: Tool Harness Profiles

Status: accepted (V1)
Date: 2026-05-07 (revised 2026-05-08 with per-tool research; V1
implementation accepted 2026-05-08 under dogfood-003 acceptance
decision `dec_6abd3957ab1748949ff0967221b346c4`).

## V2 Implementation Slice

Implemented under dogfood-004 (decision artifact
`dec_191214fea393400db73657720b6181bc`). The V2 build slice landed:

- Reference Claude Code supervised wrapper at
  `.striatum/bin/claude-supervised-wrapper.sh`. Bash `while IFS=
  read -r` loop that spawns a fresh `claude --print` per packet,
  redirects inner stdout/stderr to `/dev/null`, and traps SIGTERM
  to clean up the in-flight inner process.
- Verification test at `tests/test_claude_supervised_wrapper.py`
  with four cases: multiple-packet loop, failing-inner survival,
  empty-input EOF, one-packet-then-EOF (per design-review F3).
  Tests substitute a stub `claude` on `$PATH` so they do not depend
  on the real binary.
- Updated 2026-05-17: RFC 0063 follow-through expanded that same test
  module into provider-parameterized fixtures for
  `.striatum/bin/{claude,codex,gemini}-supervised-wrapper.sh`. The suite
  now verifies the persistent FIFO loop, inner-command failure isolation,
  clean EOF behavior, temp scratch logging, and non-interactive
  tool-approval flags across all shipped wrappers.
- Docs: SPEC's "Supervised Lane Command Contract" subsection points
  at the reference wrapper; UBIQUITOUS_LANGUAGE adds "supervised
  lane wrapper".
- The dogfood-004 and `examples/harness-profiles/` workflows now
  validate without the V1.5 lint warning naming the missing
  wrapper path. RFC 0010 V1.5 is closed by the wrapper's existence.

`docs/dogfood/003/findings/HARNESS-001.md` transitions from
proposed to resolved.

Deferred per the V2 design synthesis (out of scope for V2; future
work):

- Long-lived `claude` session via `--input-format stream-json`
  multi-turn input (unverified upstream).
- MCP-based supervision (Striatum-as-MCP-server).
- Per-packet skill installation (`.claude/skills/` Striatum bundle).
- Real-claude smoke test in CI.
- Worktree-aware wrapper variant.

## V1 Implementation Slice

Implemented under dogfood-003. The V1 build slice landed:

- Optional `harness_profiles` workflow map; closed tool-family set
  `{generic, codex, claude_code, gemini_cli}`; required `tool_family`
  and `strategy_version` per profile.
- Per-lane `harness_profile_id` reference, validated against declared
  profiles.
- Strict `accountability` enforcement: V1 rejects any value other than
  `native_subagents = internal_to_parent_session` and
  `first_class_registration = not_supported`.
- Lint-warning posture for unknown sibling fields on profile bodies,
  surfaced via the `warnings` key in `workflow validate --json` and
  `workflow plan --json`. The supervised-lane and missing-lane-command
  lints from the RFC's open questions are deferred to V1.5.
- Work-packet `harness_profile` block: passthrough projection of the
  declared profile body plus a `profile_id` key. Omitted entirely for
  lanes without a profile reference (existing workflows are unchanged).
- Reference fixture at `examples/harness-profiles/workflow.json`
  covering generic, Codex, and Claude Code profiles. The Gemini CLI
  profile remains in the dogfood-003 fixture as advisory content; V2
  promotes it.
- Tests at `tests/test_harness_profiles.py`.

Deferred to V2 / future RFCs:

- Strict (non-lint) profile validation rollout.
- Profile reference by file path.
- Workflow-validate enforcement of `supervision.compatible !=
  "verify_pipe_behavior_first"` for supervised lanes.
- Workflow-validate enforcement of `feature_flags.native_worktree:
  forbidden` by inspecting lane commands.
- Doctor checks scanning `~/.gemini/agents/*.md` and
  `~/.codex/agents/*.toml` for remote/A2A subagents.
- Job-level `harness_profile_id` overrides.
- First-class registration of native sub-agents as Striatum sessions.


Context:
`docs/DECISION_LOG.md` (D003, D015, D021, D022, D037, D054),
`docs/rfcs/0005-harness-meta-optimization.md`,
`docs/rfcs/0008-worktree-isolation-for-parallel-jobs.md`,
`docs/rfcs/0009-long-lived-process-supervision.md`,
`docs/records/_frozen/research/0010-tool-harness-profiles/claude_code.md`,
`docs/records/_frozen/research/0010-tool-harness-profiles/codex.md`,
`docs/records/_frozen/research/0010-tool-harness-profiles/gemini_cli.md`,
[Claude Code sub-agents](https://code.claude.com/docs/en/sub-agents),
[Claude Code agent teams](https://code.claude.com/docs/en/agent-teams),
[Codex](https://openai.com/codex/),
[Codex agent loop](https://openai.com/index/unrolling-the-codex-agent-loop/),
[Gemini CLI subagents](https://geminicli.com/docs/core/subagents/)

## Problem

Striatum's current adapter model intentionally starts from the minimum
portable process contract: command, cwd, environment, stdin, stdout, stderr,
exit code, and optional PTY. That boundary keeps the runner generic and
protects the daemon-owned PostgreSQL workflow state from provider-specific
hooks.

The leading terminal-agent tools are moving faster than the common process
contract. Claude Code exposes custom sub-agents and experimental agent teams.
Codex workflows can use custom agent roles, skills, and multiple isolated
agent workspaces. Gemini CLI exposes its own headless and MCP-oriented
surfaces. Users are already learning that prompts such as "spawn the maximum
number of useful agents to accomplish this task" can produce better outcomes
when the underlying tool knows how to delegate.

Without a Striatum-level concept for tool-specific harness behavior, this
knowledge lands in scattered prompts, local habits, or unreviewed operator
memory. That creates three problems:

- Workflows cannot say which native tool features are desirable for a lane.
- Agents may over-delegate or under-delegate because the work packet carries
  only the generic job contract.
- Harness improvements discovered during dogfood runs are hard to route back
  into a durable, reusable profile for the next run.

The runner needs a way to optimize interactions for each tool while
preserving the core boundary: provider-specific features are allowed at the
edge, but workflow authority remains in Striatum.

## Goals

- Define an optional tool harness profile layer distinct from the adapter
  layer.
- Let workflows describe how a lane should use native tool capabilities such
  as sub-agents, agent teams, skills, custom roles, hooks, MCP tools, and
  headless execution.
- Keep native sub-agents internal to the parent Striatum session by default,
  preserving D021 accountability.
- Surface harness guidance in work packets in a structured, auditable form.
- Let dogfood runs emit `harness_improvement_proposal` artifacts that can
  improve tool profiles over time.
- Preserve model portability: workflows should still run with a generic
  profile when a provider-specific feature is unavailable.

## Non-Goals

- Do not make Striatum parse provider transcripts, terminal output, or
  native sub-agent logs as authoritative state.
- Do not let hidden native sub-agent trees independently own Striatum queue
  messages, leases, artifacts, or verdicts unless they are explicitly
  registered as first-class sessions under a future decision.
- Do not require hosted services, cloud coordination, telemetry, or external
  persistence.
- Do not auto-apply harness changes. RFC 0005 remains the gate: proposals
  are advisory until reviewed and accepted.
- Do not hardcode one provider as the product identity. A lane remains
  configuration, not a provider assumption.

## Proposal

Add an optional `harness_profiles` section to workflow configuration and a
per-lane reference to one profile:

```json
{
  "harness_profiles": {
    "codex_default": {
      "tool_family": "codex",
      "strategy_version": "2026-05-07",
      "native_delegation": {
        "mode": "encouraged",
        "instruction": "Spawn the maximum number of useful agents only when their work is independent and bounded by the packet write scope.",
        "max_parallel_native_agents": "tool_default"
      },
      "feature_flags": {
        "skills": "allowed",
        "custom_agent_roles": "allowed",
        "worktree_agents": "allowed"
      },
      "accountability": {
        "native_subagents": "internal_to_parent_session",
        "first_class_registration": "not_supported"
      }
    }
  },
  "lanes": {
    "codex": {
      "adapter": "process",
      "harness_profile_id": "codex_default",
      "command": ["codex", "exec", "-"]
    }
  }
}
```

### Profile Fields

V1 profile fields should be intentionally small:

- `tool_family`: one of `generic`, `codex`, `claude_code`, `gemini_cli`, or
  another validator-accepted string once a profile schema exists.
- `strategy_version`: a human-readable version or date for the profile
  guidance.
- `native_delegation`: describes whether native delegation is `off`,
  `allowed`, `encouraged`, or `required_for_lane`, plus a short instruction.
- `feature_flags`: advisory declarations for native capabilities the lane may
  use, such as `subagents`, `agent_teams`, `skills`, `custom_agent_roles`,
  `hooks`, `mcp`, `headless`, or `worktree_agents`.
- `accountability`: records that native sub-agents are
  `internal_to_parent_session` unless future work supports first-class
  registration.
- `prompt_envelope_path` (optional): a reusable Markdown prompt wrapper that
  is appended to or referenced by the work packet for that tool family.
- `fallback_profile_id` (optional): a profile to use when the native feature
  set is unavailable.

### Work Packet Exposure

When a lane references a harness profile, `claim-next` should include a
`harness_profile` block in the work packet:

```json
{
  "harness_profile": {
    "profile_id": "codex_default",
    "tool_family": "codex",
    "strategy_version": "2026-05-07",
    "native_delegation": {
      "mode": "encouraged",
      "instruction": "Spawn the maximum number of useful agents only when their work is independent and bounded by the packet write scope."
    },
    "accountability": {
      "native_subagents": "internal_to_parent_session"
    }
  }
}
```

The block is guidance, not authority. The authoritative job contract remains
the existing work packet fields: write scope, expected artifacts, lease,
commands, review policy, adapter constraints, and worktree requirements.

### Delegation Semantics

The phrase "maximum number of useful agents" should not mean maximum process
count. It should mean the maximum number of independently useful native
delegations that satisfy all of these conditions:

- The delegated task is concrete, bounded, and materially advances the parent
  job.
- The parent session can integrate or reject the result without violating the
  work packet's write scope.
- Parallel native agents do not write overlapping files unless the parent
  tool provides isolated workspaces and the parent session remains responsible
  for final integration.
- Review-only, research, fixture inspection, and independent verification are
  preferred native delegation targets before broad repo-write work.
- The parent session still publishes the required artifacts and completes or
  blocks the Striatum job.

### Relationship To Existing Decisions

This RFC keeps D021 intact. Native sub-agents remain internal to the parent
session in V1. Tool profiles simply make the parent's delegation strategy
explicit.

This RFC also keeps D015 intact. Striatum's scheduler still requires declared
parallelism for first-class jobs. Native delegation inside a parent session is
tool-local optimization, not a new source of claimable Striatum work.

### Schema Additions From 2026-05-08 Tool Research

The research notes under `docs/records/_frozen/research/0010-tool-harness-profiles/` (one
per tool: Claude Code, Codex, Gemini CLI) surfaced fields the original
schema cannot represent. The three are added at parity:

- **`supervision`** — `{compatible: bool, stdin_format: "newline_delimited_json"|"packet"|"prompt_text", wrapper_required: bool}`. Required because Claude Code's `-p` print mode is **not** compatible with RFC 0009 long-lived supervision (single-shot exit). A wrapper script that reads stdin packets and dispatches into a long-lived `claude` session is needed; the profile records the requirement so workflow validation can refuse a supervised lane that targets `-p` directly.
- **`workspace_isolation`** — `{state_dir_per_job: bool, rollout_persistence: "off"|"durable"}`. Codex without `CODEX_HOME=$PER_JOB_DIR` + `--ephemeral` corrupts session state when two `codex exec` instances run in parallel ([openai/codex#11435](https://github.com/openai/codex/issues/11435)). RFC 0008 owns the working tree; `workspace_isolation` owns the *agent state directory* and is independent.
- **`agent_loop_budget`** — `{auto_compact_limit: int, model_reasoning_effort: string, max_iterations: int}`. Codex exposes all three; the field lets workflows encode community-standard guardrails (e.g. 8-iteration cap, 280k token budget) without scattering them across prompts.
- **`approval_mode`** — `"default"|"auto_edit"|"yolo"|"plan"`. Distinct from `native_delegation.mode`. Gemini CLI's first-class flag; Claude Code/Codex have analogues via permission settings.
- **`output_format`** — `"text"|"json"|"stream-json"`. Lets the supervisor parse structured output instead of sniffing.
- **`memory_files`** — list of repo-local memory file names the tool reads on session start (`CLAUDE.md`, `AGENTS.md`, `GEMINI.md`). Striatum already authors `AGENTS.md`; this field documents which other files the tool family expects.
- **`mcp_servers`** — `[{name, scope, transport, command, args}]` declared at profile scope so workflow validation can sanity-check that the lane will boot with the MCP servers it expects.
- **`turn_caps`** — `{max_session_turns: int, subagent_max_turns: int, subagent_timeout_mins: int}`. Matters for budget control in parallel subagent fan-outs.

`feature_flags` is extended with explicit values some tools require:

- `headless_print_mode: "forbidden_for_supervised_lanes"|"allowed"` (Claude Code).
- `ephemeral_sessions: "required_for_parallel"|"allowed"|"off"` (Codex).
- `remote_subagents_a2a: "forbidden"|"allowed"` (Gemini CLI; clashes with the no-hosted-services boundary unless explicitly allowed).
- `native_worktree: "forbidden"|"allowed"` (Gemini CLI ships `--worktree`; recommended `forbidden` to keep RFC 0008 authoritative).

### Layered Enforcement

For lanes that declare adapter constraints, the tool's own enforcement may
exceed the four-level model in `D046`/`D054`. Codex enforces
`network=forbidden` and `repo_scope=local_only` at the OS level via Landlock
+ seccomp on Linux and Seatbelt on macOS — the actual safety bar is
`enforced` even though the Striatum process adapter still claims
`advisory_strict`. Profile authors should not treat the adapter level and
the tool's level as equivalent; the harness profile's strategy notes
should call out where layered enforcement applies so reviewers and
operators can read both.

### Concrete Profile Examples

These are the recommended starting points. Each carries its own
`strategy_version` so it can evolve independently as the tools change. The
profiles below are advisory in V1 — workflows opt in by setting
`harness_profile_id` on a lane.

#### `claude_code_default`

```json
{
  "tool_family": "claude_code",
  "strategy_version": "2026-05-08",
  "native_delegation": {
    "mode": "preferred",
    "instruction": "Spawn the maximum number of useful sub-agents whose work is bounded, independent, and stays inside the packet write scope. Prefer specialist sub-agents (review, research, fixture inspection) over broad repo-write fan-out. Agent teams are experimental; use sub-agents first.",
    "max_parallel_native_agents": "tool_default"
  },
  "feature_flags": {
    "subagents": "preferred",
    "agent_teams": "allowed_experimental",
    "skills": "encouraged",
    "custom_agent_roles": "allowed",
    "hooks": "encouraged",
    "mcp": "allowed",
    "headless_print_mode": "forbidden_for_supervised_lanes"
  },
  "supervision": {
    "compatible": true,
    "stdin_format": "newline_delimited_json",
    "wrapper_required": true
  },
  "approval_mode": "auto_edit",
  "memory_files": ["CLAUDE.md", "AGENTS.md"],
  "accountability": {
    "native_subagents": "internal_to_parent_session",
    "first_class_registration": "not_supported"
  }
}
```

**For Claude Code, do**:
1. Author `.claude/agents/<role>.md` sub-agent definitions per role used in the lane (reviewer, researcher, fixture inspector); restrict their tool sets in frontmatter so the parent owns repo-write.
2. Author `.claude/commands/striatum-*.md` slash commands wrapping the common Striatum CLI calls (claim, ack, publish-artifact, complete) so the agent invokes them by name.
3. Register a `Stop` hook that calls `striatum complete` automatically when the agent finishes, and a `PreToolUse` hook gating tool calls against the packet's write scope.
4. Use long-lived sessions (NOT `-p` print mode) for supervised lanes; ship a wrapper script that reads newline-delimited JSON packets from stdin and feeds them as user turns.
5. Treat agent teams as `allowed_experimental`: useful for read-only research fan-out, not yet for repo-write work.

#### `codex_default`

```json
{
  "tool_family": "codex",
  "strategy_version": "2026-05-08",
  "native_delegation": {
    "mode": "encouraged",
    "instruction": "Use sub-agents by routing the parent prompt; ship .codex/agents/<role>.toml definitions. Spawn parallel codex exec instances ONLY when each has its own CODEX_HOME and --ephemeral, otherwise session state corrupts (openai/codex#11435).",
    "max_parallel_native_agents": "tool_default"
  },
  "feature_flags": {
    "subagents": "allowed_via_natural_language_routing",
    "agent_teams": "not_supported",
    "skills": "encouraged",
    "custom_agent_roles": "allowed",
    "hooks": "allowed",
    "mcp": "allowed",
    "ephemeral_sessions": "required_for_parallel"
  },
  "workspace_isolation": {
    "state_dir_per_job": true,
    "rollout_persistence": "off"
  },
  "agent_loop_budget": {
    "auto_compact_limit": 280000,
    "model_reasoning_effort": "medium",
    "max_iterations": 8
  },
  "supervision": {
    "compatible": true,
    "stdin_format": "packet",
    "wrapper_required": false
  },
  "output_format": "json",
  "approval_mode": "default",
  "memory_files": ["AGENTS.md"],
  "accountability": {
    "native_subagents": "internal_to_parent_session",
    "first_class_registration": "not_supported"
  }
}
```

**For Codex, do**:
1. Author `.codex/agents/<role>.toml` definitions; the parent invokes them by natural-language routing.
2. Set `CODEX_HOME=${STRIATUM_SCRATCH_DIR}/codex-home` per job and pass `--ephemeral` to neutralise issue #11435 on parallel `codex exec`.
3. Use `codex exec --json --ephemeral --skip-git-repo-check --sandbox workspace-write -c approval_policy=never --ignore-user-config -` as the lane command; `-` reads packet on stdin. (Codex 0.130.0 removed the `--ask-for-approval` flag; configure approval policy via `-c approval_policy=...` instead.)
4. Codex enforces `network=forbidden` at the OS level (Landlock+seccomp on Linux, Seatbelt on macOS); the profile keeps Striatum's claim at `advisory_strict` but operators can document layered enforcement is `enforced` in practice.
5. Pin `agent_loop_budget` to community-standard guardrails (8 iterations, 280k tokens, medium effort) unless a specific lane needs more.

#### `gemini_cli_default`

```json
{
  "tool_family": "gemini_cli",
  "strategy_version": "2026-05-08",
  "native_delegation": {
    "mode": "allowed",
    "instruction": "Use @<agent-name> syntax to invoke local sub-agents authored under .gemini/agents/. Parallel sub-agents are supported but Google's own guidance is to avoid parallel code-write delegations; prefer parallel for read/research/test fan-out.",
    "max_parallel_native_agents": "tool_default"
  },
  "feature_flags": {
    "subagents": "allowed",
    "remote_subagents_a2a": "forbidden",
    "agent_teams": "not_supported",
    "skills": "allowed",
    "custom_agent_roles": "allowed",
    "hooks": "allowed",
    "mcp": "encouraged",
    "native_worktree": "forbidden"
  },
  "supervision": {
    "compatible": "verify_pipe_behavior_first",
    "stdin_format": "prompt_text",
    "wrapper_required": false
  },
  "approval_mode": "auto_edit",
  "output_format": "stream-json",
  "turn_caps": {
    "max_session_turns": null,
    "subagent_max_turns": 50,
    "subagent_timeout_mins": 30
  },
  "memory_files": ["GEMINI.md"],
  "accountability": {
    "native_subagents": "internal_to_parent_session",
    "first_class_registration": "not_supported"
  }
}
```

**For Gemini CLI, do**:
1. Author `.gemini/agents/<role>.md` (Markdown + YAML frontmatter) sub-agent definitions; invoke via `@<agent-name>` from the parent prompt.
2. Author `.gemini/commands/<name>.toml` slash commands wrapping Striatum CLI calls.
3. Use `gemini --prompt - --output-format stream-json --approval-mode auto_edit` as the lane command. **Verify pipe behavior under `os.mkfifo` before committing the supervisor flow** — Gemini CLI's stdin handling under named pipes is the least-tested path of the three.
4. Forbid `remote_subagents_a2a: kind: remote` — it requires a hosted A2A endpoint and clashes with Striatum's no-hosted-services boundary.
5. Forbid `native_worktree`/`--worktree` in the lane command — RFC 0008 owns Striatum's worktree concept; the tool's own worktree feature would shadow it.
6. Register MCP servers in `.gemini/settings.json:mcpServers` rather than via `gemini mcp add` so the project repo is the source of truth.

### Recommended Lane Configurations

Concrete starting points (operators will tune):

```json
{
  "lanes": {
    "claude_code": {
      "adapter": "process",
      "harness_profile_id": "claude_code_default",
      "command": [".striatum/bin/claude-supervised-wrapper.sh"],
      "constraints": {"transcripts": "off", "repo_scope": "local_only"},
      "required_enforcement": {"transcripts": "enforced"}
    },
    "codex": {
      "adapter": "process",
      "harness_profile_id": "codex_default",
      "command": [
        "codex", "exec", "--json", "--ephemeral",
        "--skip-git-repo-check",
        "--sandbox", "workspace-write",
        "-c", "approval_policy=never",
        "--ignore-user-config", "-"
      ],
      "env": {"CODEX_HOME": "${STRIATUM_SCRATCH_DIR}/codex-home"},
      "constraints": {"network": "forbidden", "transcripts": "off", "repo_scope": "local_only"},
      "required_enforcement": {"transcripts": "enforced"}
    },
    "gemini_cli": {
      "adapter": "process",
      "harness_profile_id": "gemini_cli_default",
      "command": [
        "gemini", "--prompt", "-",
        "--output-format", "stream-json",
        "--approval-mode", "auto_edit"
      ],
      "env": {"GEMINI_CLI_TRUST_WORKSPACE": "1"},
      "constraints": {"transcripts": "off", "repo_scope": "local_only"},
      "required_enforcement": {"transcripts": "enforced"}
    }
  }
}
```

Note that `claude_code` references a wrapper script (not a direct `claude`
invocation): Claude Code's `-p` print mode terminates after one prompt and
is incompatible with RFC 0009 long-lived supervision. The wrapper reads
newline-delimited JSON packets from stdin and feeds them to a long-lived
`claude` session as user turns. Implementation of the wrapper is a
follow-up under RFC 0009 / supervisor PTY work.

### Dogfood Path

Dogfood-001 should be used to collect the first practical profile changes:

1. Run the existing Striatum-on-Striatum workflow with real Claude Code and
   Codex lanes.
2. Record friction as `harness_improvement_proposal` artifacts.
3. Convert high-signal proposals into initial `codex`, `claude_code`, and
   `generic` profile fixtures.
4. Add profile exposure to work packets only after the initial profile shape
   survives review.

## Acceptance Criteria

- Workflow validation accepts a `harness_profiles` map and validates that any
  lane `harness_profile_id` references a declared profile.
- Unknown or malformed profile fields produce workflow validation errors, or
  profile lint warnings if the project chooses an advisory-first rollout.
- `claim-next` includes a `harness_profile` block in work packets for lanes
  that reference a profile.
- The default behavior for workflows without `harness_profiles` is unchanged.
- A generic profile fixture, a Codex-oriented profile fixture, and a Claude
  Code-oriented profile fixture are added under examples or dogfood docs.
- Documentation states that native sub-agents remain internal to the parent
  Striatum session unless registered as first-class sessions in a future
  decision.
- At least one dogfood run produces or reviews a
  `harness_improvement_proposal` that targets one of `prompt`, `workflow`,
  `defaults`, or `documentation` for a tool profile.

## Open Questions

- Should profile validation be strict from day one, or should unknown fields
  be accepted as lint warnings while provider capabilities are changing fast?
  *Updated 2026-05-08:* lint-warning rollout recommended given the breadth of
  schema additions surfaced by the per-tool research; strict validation can
  follow once dogfood evidence accumulates.
- Should `harness_profiles` live directly in workflow JSON, in reusable
  profile files referenced by path, or both?
- Should Striatum ship built-in profile templates for `codex`,
  `claude_code`, and `gemini_cli`?
  *Updated 2026-05-08:* the three concrete profiles in this RFC now cover the
  primary lanes. Question becomes whether they ship as in-repo fixtures
  under `examples/harness-profiles/` or as defaults built into
  `striatum.workflow` validation.
- Should native delegation limits be numeric (`max_native_agents: 4`) or
  semantic (`tool_default`, `bounded_by_write_scope`, `review_only`)?
  *Updated 2026-05-08:* both. Codex's documented community guardrails are
  numeric (8 iterations / 280k tokens), while Claude Code's sub-agent budget
  is semantic (`tool_default`). The schema accepts both; a profile can pin
  whichever its tool exposes.
- What evidence should a parent session publish when it uses native
  sub-agents, given the no-transcript default?
- When, if ever, should native sub-agents graduate into first-class Striatum
  sessions with independent leases and artifacts?
- Codex enforces `network=forbidden` at the OS level via Landlock/seccomp
  (Linux) or Seatbelt (macOS). Should the workflow validator surface this
  layered enforcement, or keep the four-level model as the single source of
  truth and let operators read the profile's strategy notes? *(New 2026-05-08.)*
- Gemini CLI ships native `--worktree`. Do we forbid it in profiles to keep
  RFC 0008 authoritative, or allow it under specific opt-in profiles where
  the tool's worktree wins? *(New 2026-05-08.)*
- Claude Code's `-p` print mode is incompatible with RFC 0009 long-lived
  supervision. Should Striatum ship a standard wrapper script
  (`.striatum/bin/claude-supervised-wrapper.sh`) under the Striatum repo,
  or document the pattern and leave authoring to operators? *(New 2026-05-08.)*
