# Claude Code — research for RFC 0010

Date: 2026-05-08
Researcher: claude-code-guide subagent

## Native delegation features

### Sub-agents

**Definition and scope:**
Sub-agents are specialized AI assistants that run within a single parent session. Each sub-agent has:
- Its own isolated context window (separate from the parent)
- Custom system prompt
- Specific tool access (restricted or expanded)
- Independent permission rules
- Selectable model (including cost-optimized models like Haiku)

Sub-agents are defined as Markdown files with YAML frontmatter in `.claude/agents/` (project-scoped or `~/.claude/agents/` user-scoped). A subagent definition includes:
- `name`: Display name
- `description`: When Claude should delegate to this agent
- `model`: Model override (e.g., `"claude-haiku-4.5"` for cost optimization)
- `tools`: Tool allowlist (restricts what the subagent can do)
- `instructions`: Markdown body with task-specific guidance
- `skills` (optional): Skills preloaded into the subagent context

**Invocation semantics:**
Claude decides when to delegate based on the subagent's description and the current task. The parent agent spawns the subagent, which works independently, summarizes findings, and returns results to the parent. Subagents do not communicate with each other—all coordination flows through the parent session.

**Striatum relevance:**
Sub-agents are ideal for bounded, isolated tasks within a single work packet:
- Code review of a specific module
- Test execution in a read-only context
- Security scanning of a narrowly scoped file
- Research/investigation that doesn't require repo writes
- Cost-optimized analysis by routing to cheaper models

Sub-agents preserve context by keeping exploration out of the main conversation, and they enforce constraints by restricting tool access. For Striatum, a sub-agent can be spawned per work packet with task-specific permissions.

**Citations:**
- [Create custom subagents - Claude Code Docs](https://code.claude.com/docs/en/sub-agents)

---

### Agent teams

**Definition and semantics:**
Agent teams are an *experimental* feature (disabled by default, enabled with `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`) that coordinates multiple independent Claude Code sessions working in parallel. Key differences from sub-agents:

1. **Separate sessions**: Each teammate is a fully independent Claude Code instance with its own context window, not a subprocess.
2. **Direct inter-agent communication**: Teammates can message each other directly; they don't bottleneck through a parent intermediary.
3. **Shared task list**: All teammates claim work from a shared, locked task queue. Task dependencies are automatically managed.
4. **Long-running**: Teammates stay alive across multiple turns; they can be assigned work, complete it, claim new work autonomously.
5. **Centralized lead**: One session acts as the "lead," spawning, monitoring, and coordinating teammates.

**Architecture:**
- **Team config**: `~/.claude/teams/{team-name}/config.json` (auto-generated, runtime state)
- **Task list**: `~/.claude/tasks/{team-name}/` (shared file-locked task state)
- **Parallelism**: True parallelism—teammates execute in separate processes simultaneously
- **Communication**: Mailbox system with automatic message delivery (no polling needed)
- **Display modes**: In-process (single terminal, cycle with Shift+Down) or split-pane (tmux/iTerm2)

**Invocation:**
Teams are created on demand via natural language: "Create a team to review PR #142 from three angles." Claude decides whether to spawn teammates and how many based on the task. Explicit control available: "Create a team with 4 teammates" or "Use Sonnet for each teammate."

**Parallelism semantics:**
Teams work best when:
- Work can be split into independent, non-overlapping tasks
- Teammates don't write to the same files (data hazards)
- Each teammate owns a distinct piece of the problem
- Token cost justifies the overhead (teams use 3-5x more tokens than a single session)

Teams struggle with:
- Sequential dependencies
- Same-file editing (race conditions)
- High coordination overhead
- Plan mode and session resumption have limitations

**Striatum relevance:**
Agent teams map well to Striatum's parallel-lane model when each lane can be a separate Claude Code session. However, RFC 0015 (Striatum's own multi-agent coordination) must decide whether to:
1. Spawn dedicated teams per Striatum run and let teams self-coordinate work
2. Keep teams internal to parent sessions (RFC 0010's D021 accountability boundary)
3. Register teammates as first-class Striatum sessions (future decision, not V1)

For V1, agent teams are too experimental and too stateful (`.claude/teams/` files) to integrate directly; they're better used *within* a single Striatum lane's Claude Code session as an internal optimization.

**Known limitations (experimental):**
- No session resumption with in-process teammates
- Task status can lag (teammates sometimes fail to mark tasks complete)
- Shutdown is slow
- One team per session
- No nested teams
- Permissions set at spawn time (cannot dynamically adjust per-teammate)
- Split-pane mode requires tmux or iTerm2

**Citations:**
- [Orchestrate teams of Claude Code sessions - Claude Code Docs](https://code.claude.com/docs/en/agent-teams)
- [Claude Code Agent Teams: Setup & Usage Guide 2026](https://claudefa.st/blog/guide/agents/agent-teams)
- [Shipyard | Multi-agent orchestration for Claude Code in 2026](https://shipyard.build/blog/claude-code-multi-agent/)

---

### Slash commands / skills

**Unified definition (as of 2026):**
Slash commands and skills have been unified. Files in `.claude/commands/` still work for backward compatibility, but the recommended approach is `.claude/skills/<skill-name>/SKILL.md`. Every skill gets a `/slash-command` interface.

**Definition format:**
A skill is a directory containing:
- `SKILL.md` (required): YAML frontmatter + Markdown instructions
- Supporting files (optional): templates, examples, scripts

Frontmatter fields:
- `name`: Display name (matches directory name if omitted)
- `description` (recommended): When to use this skill
- `when_to_use`: Additional trigger hints
- `disable-model-invocation`: Set `true` to prevent Claude from auto-invoking
- `user-invocable`: Set `false` to hide from `/` menu (Claude-only)
- `allowed-tools`: Pre-approve specific tools while skill is active
- `model`: Override model for this skill
- `effort`: Effort level override
- `context`: Set to `fork` to run in a subagent (isolated context)
- `agent`: Which subagent type to use with `context: fork`
- `arguments`: Named positional arguments for dynamic substitution

**Invocation semantics:**
- **User-invoked**: Type `/skill-name` in the prompt
- **Model-invoked**: Claude decides to use the skill based on `description` and conversation context
- Both can invoke unless `disable-model-invocation: true` (user-only) or `user-invocable: false` (Claude-only)

**Dynamic context injection:**
Skills can run commands before Claude sees the content:
```yaml
---
description: Summarize changes
---

## Current diff
!`git diff HEAD`
```

The command runs first, output replaces the placeholder. Useful for grounding prompts in live data.

**Scope:**
- **Project**: `.claude/skills/<skill-name>/SKILL.md` (committed to repo, project-only)
- **Personal**: `~/.claude/skills/<skill-name>/SKILL.md` (all projects)
- **Plugin**: `<plugin>/skills/<skill-name>/SKILL.md` (where plugin is enabled)
- **Enterprise**: Managed settings (organization-wide)

Precedence: Enterprise > Personal > Project; Plugin skills use `plugin-name:skill-name` namespace.

**Striatum relevance:**
Skills are lightweight reusable prompts that live in `.claude/` and can be versioned. They're ideal for:
- Wrapping Striatum CLI commands as skills (e.g., `/claim-next`, `/publish-artifact`)
- Defining standard workflows (e.g., `/test-changes`, `/review-pr`)
- Automating multi-step procedures

Skills could be bundled with a Striatum lane definition so agents spawn with consistent task templates. Dynamic context injection (`!`git diff``) is powerful for feeding live work state into Claude.

**Distinction: Slash commands vs. skills:**
- **Slash commands** (legacy `.claude/commands/`): Static markdown prompts
- **Skills** (new `.claude/skills/`): Directory-based with frontmatter control, supporting files, and dynamic invocation rules

Skills are the recommended approach going forward.

**Citations:**
- [Extend Claude with skills - Claude Code Docs](https://code.claude.com/docs/en/skills)
- [Claude Code Skills Complete Guide: SKILL.md, MCP, Subagents & Teams (2026)](https://duet.so/guides/claude-code-skills-complete-guide)

---

### Hooks

**Definition:**
Hooks are user-defined automation that execute at specific points in Claude Code's lifecycle. They run custom scripts (Bash, HTTP, MCP tool, Prompt, or Agent) and make decisions based on the output.

**Five hook types:**

| Type | Mechanism | Best for |
|------|-----------|----------|
| **Command** | Execute shell script, read/write JSON on stdin/stdout | Complex logic, local tools, Striatum integration |
| **HTTP** | POST JSON to URL, read JSON response | Remote decision-making (APIs, webhooks) |
| **MCP tool** | Call tool on connected MCP server | Leveraging existing MCP integrations |
| **Prompt** | Send JSON to Claude model for yes/no decision | AI-powered evaluation |
| **Agent** | Spawn subagent with tool access for verification | Multi-step validation logic |

**Hook lifecycle events:**

| Event | Runs when | Use case |
|-------|-----------|----------|
| `SessionStart` | Session begins | Load dynamic context, user permissions, project policies |
| `SessionEnd` | Session ends | Audit/cleanup |
| `UserPromptSubmit` | Before Claude sees prompt | Validate/enhance input |
| `PreToolUse` | Before tool execution | Block/allow/modify tool calls (most powerful) |
| `PostToolUse` | After tool execution | Audit/react, trigger side effects |
| `PostToolUseFailure` | Tool execution fails | Handle errors |
| `PermissionRequest` | Permission dialog appears | Auto-approve/deny with modifications |
| `Stop` | User presses Ctrl+C | Interrupt handler |
| `StopFailure` | Stop hook itself fails | Error handling |
| `PreCompact`, `PostCompact` | Context compaction | Track when context is summarized |
| `ConfigChange` | Settings change | React to config edits |
| `FileChanged` | Watched file changes | Monitor CLAUDE.md, skill files |

**Key powers (PreToolUse is most powerful):**
- **Allow/Deny**: `permissionDecision: "allow" | "deny" | "ask" | "defer"`
- **Modify**: Change tool input before execution (`updatedInput`)
- **Defer**: Pause for external approval (e.g., async API decision)

**For Striatum integration:**

Hooks are the primary integration point between Claude Code and Striatum. Key patterns:

1. **SessionStart hook (HTTP)**: Fetch user's Striatum permissions, project policy, allowed paths. Inject as context.
   ```bash
   curl http://striatum:8080/api/user-context \
     -H "Authorization: Bearer $STRIATUM_TOKEN"
   ```

2. **PreToolUse hook (Command)**: Validate Bash commands against Striatum policy before Claude executes.
   ```bash
   COMMAND=$(jq -r '.tool_input.command')
   curl http://striatum:8080/api/validate-command \
     -d "{\"command\": \"$COMMAND\"}" | jq -r '.allowed'
   ```

3. **PostToolUse hook (Command)**: Log executed commands to Striatum audit trail.
   ```bash
   curl http://striatum:8080/api/log-execution \
     -d "{\"command\": \"$COMMAND\", \"exit_code\": $EXIT_CODE}"
   ```

4. **Stop hook (Command)**: Auto-complete Striatum job when Claude finishes.
   ```bash
   striatum complete --session-id $SESSION_ID --job-id $JOB_ID
   ```

**Citation:**
- [Hooks in Claude Code](https://code.claude.com/docs/en/hooks)

---

### MCP integration

**What is MCP:**
The Model Context Protocol (MCP) is an open standard for connecting AI tools to remote services. Claude Code can connect to:
- **Built-in MCP servers**: Gmail, Google Calendar, Google Drive (configured in user settings)
- **Custom project MCP servers**: Local or remote, defined in `.claude/mcp.json`
- **Community MCP servers**: Published registry of third-party integrations

**Custom MCP setup:**
Define in `.claude/mcp.json` (project-scoped) or `~/.claude/mcp.json` (user-scoped):

```json
{
  "striatum-supervisor": {
    "command": "python",
    "args": ["-m", "striatum.mcp", "--framing", "line"],
    "env": { "STRIATUM_REPO": "/path/to/repo" }
  }
}
```

Claude Code launches the MCP process as a subprocess, communicates via stdin/stdout (JSON-RPC with LSP-style framing or newline-delimited), and exposes all MCP tools to Claude as callable functions.

**Scope:**
- **Project MCP** (`.claude/mcp.json`): Project-only, committed to repo
- **User MCP** (`~/.claude/mcp.json`): All projects
- **Enterprise MCP**: Managed settings

**Striatum relevance:**
Striatum itself can be exposed as an MCP server:
- Tool: `striatum-claim-next` — claim work packet
- Tool: `striatum-publish-artifact` — publish findings
- Tool: `striatum-complete` — mark job done
- Tool: `striatum-status` — query run state
- Tool: `striatum-doctor` — diagnose problems

A Claude Code session running as a Striatum lane can then call Striatum commands via MCP, removing the need for direct Bash invocation. This is cleaner for permissions and audit logging.

Reference implementation in Striatum: `src/striatum/mcp.py` provides the MCP wrapper.

**Citation:**
- [Connect Claude Code to tools via MCP - Claude Code Docs](https://code.claude.com/docs/en/mcp)
- `docs/MCP.md` (Striatum's own MCP docs)

---

### Headless mode and stdin

**Non-interactive invocations:**
Claude Code supports several headless modes for scripting and automation:

| Flag | Behavior | Use case |
|------|----------|----------|
| `claude -p "prompt"` | Print mode: read prompt, generate response, exit | Scripting, CI |
| `--print` | Same as `-p` | Explicit form |
| `--continue` | Resume last session without interaction, exit when done | Continuation mode |
| `--resume` | Resume session, return to interactive prompt | Manual resumption |
| `echo "prompt" \| claude` | Pipe mode: read from stdin (single-shot) | Scripts, pipes |

**Behavior under piped stdin:**
- When stdin is not a TTY (pipe/file), Claude Code reads the prompt from stdin and exits after generating a response
- No interactive mode, no checkpoints, no `/` commands
- Streaming output to stdout (unless `--json` flag is set)
- Exit code reflects success/failure

**TTY requirements:**
- Interactive mode requires a TTY (terminal)
- Headless modes work with pipes, files, or TTY without distinction
- `--print` and `--continue` are TTY-independent

**JSON output:**
- `--json` flag: Structured JSON output (work with scripts)
- Exit codes: 0 (success), non-zero (failure)

**Striatum relevance:**
For RFC 0009 (Striatum's supervisor mode), Claude Code is NOT viable in print mode (`-p`) because:
1. Print mode is single-shot: reads one prompt, generates response, exits
2. Supervisor mode requires a long-lived process that reads multiple work packets across multiple turns
3. The process must stay alive across `striatum supervise send` calls (newline-delimited JSON on stdin)

A Striatum lane command must:
1. Stay alive across packets
2. Read newline-delimited JSON from stdin
3. Call back via `striatum` CLI to advance workflow state
4. Never rely on supervisor's stdout/stderr

This rules out raw `claude` invocations for Striatum lanes. Instead, use:
- A wrapper script that reads packets and invokes `/claude-api` skill or wrapped `claude` session
- A long-lived Claude Code session with supervisor mode (RFC 0009)
- Custom integration via MCP (Striatum as MCP server)

**Citation:**
- [CLI reference - Claude Code Docs](https://code.claude.com/docs/en/cli-reference)
- [Interactive mode - Claude Code Docs](https://code.claude.com/docs/en/interactive-mode)
- `docs/SPEC.md` section "Supervised Lane Command Contract"

---

### Multi-instance / worktree behavior

**Running multiple Claude Code instances:**
Claude Code has no built-in multi-instance coordination. Running multiple instances simultaneously:
- Each gets its own context window, session history, and state
- No shared state collision (independent SQLite state is not applicable—Claude Code doesn't use SQLite)
- No file-level locking at the Claude Code layer (conflicts are user's responsibility)

**Worktree support (external, not built-in):**
Claude Code can work with Git worktrees but doesn't manage them:
- User creates worktrees manually: `git worktree add <path> <branch>`
- Each Claude Code instance can operate in a different worktree
- No automatic isolation or coordination

**Parallelism via worktrees + agent teams:**
- Spawn 1 agent team per worktree (lead agent manages teammates in that worktree)
- Each worktree is independent, no file conflicts
- Teammates in a team can parallelize work within the worktree
- But teams themselves don't span worktrees—each team is one worktree-aware session

**Striatum relevance:**
RFC 0008 (Worktree Isolation) handles per-job isolation in Striatum. Claude Code doesn't need to know about Striatum worktrees; it just operates in whatever directory the lane command specifies (cwd from the process adapter). When a Striatum lane is configured for per-job isolation:
1. Striatum creates a worktree before claiming work
2. Lane command is invoked with cwd = worktree path
3. Claude Code reads/writes in that worktree
4. Striatum releases the worktree after the job completes

No special coordination needed—Claude Code sees it as a normal directory.

**Citation:**
- [Agent teams - Claude Code Docs](https://code.claude.com/docs/en/agent-teams) (section "Limitations")
- Striatum `docs/SPEC.md` section "Worktree Isolation"

---

## How teams use it in the wild

### Real-world configurations

**Pattern 1: Everything Claude Code (affaan-m)**
Production system with 48 agents, 182 skills, and 68 legacy command shims. Evolved over 10+ months of daily use building real products. Key lessons:
- Heavy use of subagents for specialized roles (security-reviewer, test-runner, debugger)
- Hooks for validation, logging, and policy enforcement
- Skills for common workflows (deploy, commit, review-pr)
- Agent naming convention for discoverability (role-verb, e.g., `security-scan-codebase`)
- Cost optimization by routing simple tasks to Haiku

**Pattern 2: Claude Code Showcase (ChrisWiles)**
Comprehensive configuration example including:
- Hooks for GitHub Actions, git workflows, and custom status lines
- Skills for PR review, change summarization, and testing
- Scheduled workflow agents that run on a cron schedule for maintenance
- Skill evaluation system that analyzes every prompt and suggests which skills to activate

**Pattern 3: Multi-Agent Orchestration System (wshobson)**
Intelligent automation and multi-agent orchestration for Claude Code:
- Coordinator agent that routes work to specialists
- Task decomposition (planning, implementation, testing, review as separate phases)
- Error recovery and fallback patterns
- Event-driven architecture (message bus pattern)

**Pattern 4: Awesome Claude Code Subagents (VoltAgent)**
Collection of 100+ specialized subagents covering:
- Code review (security, performance, maintainability)
- Testing (unit, integration, e2e)
- Documentation (API docs, changelog, guides)
- DevOps (deployment, monitoring, logging)
- Domain-specific (frontend, backend, database)

Key insight: Subagents are reusable across projects when committed to a shared repository or published as a plugin.

### Notable orchestration patterns from blogs

**1. Generator-Verifier Pattern** (Claude official)
- One agent generates output (code, design, plan)
- One agent verifies with explicit criteria
- Reduces hallucination and increases quality
- Map to Striatum: generator lane + verification lane with review job

**2. Orchestrator-Subagent Pattern** (Mae Capozzi)
- Coordinator agent decides which specialists to invoke (not hardcoded routing)
- Each specialist is a subagent or team
- Coordinator sees all results, synthesizes
- Map to Striatum: root job is coordinator, delegated jobs are specialists

**3. Phase-based Orchestration** (Addy Osmani)
- Work broken into discrete phases: planning, implementation, testing, review
- Each phase is a recovery point (can restart from a phase)
- Dependencies explicit between phases
- Map to Striatum: phases = lanes, each lane has jobs for planning/impl/test/review

**4. Message Bus Pattern** (Claude official)
- Event-driven pipeline: agents emit events, other agents subscribe
- Useful for cross-cutting concerns (logging, monitoring, notifications)
- Map to Striatum: hooks become subscribers to Striatum events

**5. Shared-State Collaboration** (Claude official)
- Multiple agents build on each other's findings (not isolated subagents)
- Used for research, design refinement, or iterative problem-solving
- Map to Striatum: review cycle with multiple reviewers, each can see prior feedback

### Community resources

- **[Awesome Claude Code](https://github.com/hesreallyhim/awesome-claude-code)** - Curated list of skills, hooks, agents, plugins
- **[Claude How-To](https://github.com/luongnv89/claude-howto)** - Visual, example-driven guide with copy-paste templates (May 2026, compatible with v2.1+)
- **[Claude Code Topics on GitHub](https://github.com/topics/claude-code-agents)** - Trending repositories and projects
- **[Dive into Claude Code (VILA-Lab)](https://github.com/VILA-Lab/Dive-into-Claude-Code)** - Systematic analysis for designing AI agent systems

---

## Mapping to RFC 0010 schema

### `claude_code_default` profile

Based on research into Claude Code's capabilities, here is a recommended `claude_code_default` harness profile:

```json
{
  "tool": "claude_code",
  "strategy_version": "2026-05-08",
  "capabilities": {
    "subagents": "preferred",
    "agent_teams": "allowed_experimental",
    "skills": "encouraged",
    "custom_agent_roles": "allowed",
    "hooks": "encouraged",
    "mcp_tools": "allowed",
    "headless_flags": "not_viable_for_supervised_lanes",
    "stdin_mode": "newline_delimited_json"
  },
  "fallback_profile_id": null,
  "notes": {
    "supervision_requirement": "RFC 0009 lanes must use a long-lived session that reads newline-delimited JSON packets from stdin and calls back via striatum CLI, not print-mode or single-shot invocations",
    "parallelism_recommendation": "Use subagents for bounded research/verification tasks within a single lane session; use agent teams only when spawning as an internal optimization, not as first-class Striatum sessions",
    "hooks_for_striatum": "SessionStart (load permissions), PreToolUse (validate commands), PostToolUse (audit), Stop (auto-complete jobs)",
    "mcp_integration": "Expose Striatum as an MCP server so lanes invoke striatum commands via MCP rather than direct Bash"
  }
}
```

### Justification of capability values

**`subagents: preferred`**
- Subagents are stable, well-tested, and ideal for Striatum's lane model
- Each work packet can spawn 1-N subagents (researcher, reviewer, test-runner)
- Subagents preserve context by isolating exploration and enforce constraints via tool allowlists
- Striatum lanes should define subagent configurations in a `claude_code.agents.json` file under the `.claude/` directory, allowing workflows to specify which subagents are needed

**`agent_teams: allowed_experimental`**
- Teams are experimental (disabled by default, many limitations)
- Not recommended for V1 Striatum integration
- If enabled, teams must remain internal to a lane session (not first-class Striatum sessions per D021)
- Future: upgrade to `preferred` once session resumption and task coordination stabilize

**`skills: encouraged`**
- Skills are lightweight, versioned, and reusable
- Perfect for wrapping Striatum CLI commands (`/claim-next`, `/publish-artifact`, `/complete`)
- Dynamic context injection (`!`git diff``) is powerful for live data
- Striatum lanes should commit common skills to `.claude/skills/` so agents spawn with task templates

**`custom_agent_roles: allowed`**
- Custom subagent definitions (e.g., `security-reviewer`, `test-runner`) should be defined in `.claude/agents/` and referenced by lane configuration
- Agent definitions use the same Markdown format as skills, making them versionable and discoverable

**`hooks: encouraged`**
- Hooks are the primary integration point for Striatum policy enforcement
- `SessionStart` hook: Load user's Striatum permissions as context
- `PreToolUse` hook: Validate commands against Striatum's write scope
- `PostToolUse` hook: Audit tool execution to Striatum
- `Stop` hook: Auto-complete Striatum jobs when Claude finishes
- These can be configured in `.claude/hooks.json` (project-scoped) or settings.json (global)

**`mcp_tools: allowed`**
- MCP is the clean integration point for Striatum CLI commands
- Expose Striatum as an MCP server, making `claim-next`, `publish-artifact`, `complete` available as MCP tools
- Cleaner than direct Bash invocation, better audit trail

**`headless_flags: not_viable_for_supervised_lanes`**
- Print mode (`-p`) and single-shot invocations are incompatible with RFC 0009 supervision
- Supervised lanes must use a long-lived process that reads newline-delimited packets
- Workflows that want headless execution should use non-supervised lanes with `adapter run` (single-shot)

**`stdin_mode: newline_delimited_json`**
- RFC 0009 lanes receive work packets as newline-terminated JSON on stdin
- Claude Code processes must be configured to parse this format (not a built-in feature)
- Wrapper scripts or custom integrations needed

---

## Recommended Striatum lane configuration

For a Striatum lane using Claude Code as the tool, here's a concrete configuration:

### Lane definition

```json
{
  "lanes": {
    "claude_code": {
      "adapter": "process",
      "harness_profile_id": "claude_code_default",
      "command": [
        "bash",
        "-c",
        "python -m striatum.adapters.claude_code_supervised --model opus --skills-enabled"
      ],
      "env": {
        "STRIATUM_SESSION_ID": "${SESSION_ID}",
        "STRIATUM_JOB_ID": "${JOB_ID}",
        "STRIATUM_LEASE_ID": "${LEASE_ID}",
        "CLAUDE_MODEL": "claude-opus-4-6-20250514"
      },
      "stdin_mode": "packet",
      "constraints": {
        "network": "forbidden",
        "repo_scope": "local_only",
        "transcript": "off"
      },
      "worktree_isolation": "per_job"
    }
  }
}
```

### Process adapter contract

The `command` array is invoked by `striatum adapter run` with:
- **cwd**: Repository root or worktree path
- **env**: Work packet environment variables + lane-configured env
- **stdin**: Work packet JSON (newline-delimited if supervised, single packet if single-shot)
- **stdout/stderr**: Redirected to `/dev/null` (no transcript capture)
- **exit code**: 0 (success), non-zero (failure, retry according to work packet policy)

### Striatum lane command scaffold

The lane command should be a Python script (or wrapper) that:

1. **Read work packet from stdin** (newline-delimited JSON)
2. **Register session or reuse existing session** (`striatum register-session` if fresh)
3. **Invoke Claude Code** with the work packet as a prompt skeleton
4. **Parse Claude's response** to detect artifact publications, verdicts, or completion signals
5. **Call back to Striatum CLI** to publish artifacts, emit verdicts, and complete the job
6. **Loop for next packet** (if running supervised, loop continues until SIGTERM)

Example (pseudocode):

```python
#!/usr/bin/env python3
import json
import sys
import subprocess
from pathlib import Path

def run_supervised_lane():
    """RFC 0009 lane: read packets, invoke Claude Code, update Striatum."""
    session_id = None
    
    for line in sys.stdin:
        packet = json.loads(line)
        
        # Register session on first packet
        if not session_id:
            result = subprocess.run(
                ["striatum", "register-session", "--role", "dev", "--lane", "claude_code"],
                capture_output=True, text=True
            )
            session_id = json.loads(result.stdout)["session_id"]
        
        job_id = packet["job_id"]
        lease_id = packet["lease_id"]
        task_prompt = packet["task"]
        
        # Invoke Claude Code (would use a long-lived session or subprocess here)
        # Claude reads the task, generates a response, reads files, runs tests, etc.
        # Claude detects when work is done (based on task and CLAUDE.md guidance)
        
        # Parse output and call back to Striatum
        # Example: Claude's response signals "ready to publish"
        subprocess.run([
            "striatum", "publish-artifact",
            "--session-id", session_id,
            "--lease-id", lease_id,
            "--path", "findings/analysis.md",
            "--kind", "finding"
        ])
        
        subprocess.run([
            "striatum", "complete",
            "--session-id", session_id,
            "--lease-id", lease_id,
            "--job-id", job_id
        ])

if __name__ == "__main__":
    run_supervised_lane()
```

### Recommended settings.json for Claude Code lanes

```json
{
  "permissions": {
    "defaultMode": "acceptEdits",
    "allow": [
      "Bash(git *)",
      "Bash(find *)",
      "Bash(grep *)",
      "Read(.)",
      "Skill(striatum-*)"
    ],
    "deny": [
      "Bash(curl *)",
      "Bash(rm -rf *)"
    ]
  },
  "model": "claude-opus-4-6",
  "env": {
    "STRIATUM_SESSION_ID": "${STRIATUM_SESSION_ID}",
    "STRIATUM_REPO": "${PWD}"
  },
  "hooks": {
    "SessionStart": [
      {
        "type": "command",
        "script": ".claude/hooks/load-striatum-context.sh"
      }
    ],
    "PreToolUse": [
      {
        "type": "command",
        "matcher": "Bash",
        "script": ".claude/hooks/validate-command.sh"
      }
    ]
  },
  "mcpServers": {
    "striatum": {
      "command": "python",
      "args": ["-m", "striatum.mcp"],
      "env": { "STRIATUM_REPO": "${PWD}" }
    }
  }
}
```

### Constraints and parallelism

**Per-lane constraints (enforced by adapter):**
- `network: forbidden` — no external HTTP (enforced advisory_strict)
- `repo_scope: local_only` — no access to other repos (enforced advisory_strict)
- `transcript: off` — no transcript logging (enforced)

**Parallelism strategy:**
1. **Multiple independent lanes** can run in parallel (Striatum scheduler manages parallelism)
2. **Within a lane**: Use subagents to parallelize research/review/testing in isolation
3. **Agent teams**: Not recommended for V1; enable only if teams stay internal to a lane
4. **Worktree isolation**: Enable per-job so multiple lanes can write to different worktrees simultaneously

**Throughput optimization:**
- Route simple tasks (code style checks, unit tests) to Haiku subagents to reduce cost
- Use `allowed-tools` in subagent definitions to restrict what each specialist can do
- Batch independent artifacts into single skill invocations (e.g., `/review-multiple-files` instead of individual reviews)

---

## Gaps and risks

### Things RFC 0010 doesn't capture yet

1. **Subagent resource allocation**: No way to specify how many subagents a lane should spawn, or what the cost/token budget should be per lane. RFC 0010 leaves `max_parallel_native_agents` as `"tool_default"`, which isn't specific enough for Striatum's deterministic planner.

2. **Nested subagents**: If a subagent spawns its own subagent, who owns the responsibility? RFC 0010 says subagents are internal to the parent session, but it's silent on cascading delegation.

3. **Skill preloading**: No profile field for which skills should be preloaded into a lane's session (vs. loaded on-demand). This affects cost and startup latency.

4. **MCP server provisioning**: No profile field for which MCP servers should be attached to a lane, or how to provision/configure them at lane startup.

5. **Model selection and effort**: Profile should allow specifying model and effort level overrides per lane (e.g., "use Haiku for research lanes, Opus for final review").

6. **Headless mode semantics**: RFC 0010 doesn't address whether lanes should be interactive (developer-facing) or headless (automated). The schema has no field for this.

### Things Claude Code does that don't fit RFC 0010's abstraction

1. **Session resumption** (`/resume`, `/rewind`): Claude Code can pause and resume a conversation across multiple CLI invocations. Striatum's work packet model doesn't support this; once a job is claimed, it runs to completion in a single invocation. Resumption would require a different lane command contract.

2. **Checkpoints**: Claude Code lets users undo edits and restore code to a prior state. Striatum doesn't expose this as a first-class operation. Recovery via worktrees is available but requires explicit operator action.

3. **Interactive permission dialogs**: Claude Code prompts the user to approve tool usage in real-time. In a Striatum workflow, there's no interactive user present. Hooks must pre-approve or pre-deny based on policy, not prompt.

4. **Context compaction**: Claude Code automatically summarizes conversation history to free context space. This is transparent to the user but creates non-deterministic behavior for Striatum jobs (same job might take different paths depending on when compaction happens).

5. **Model switching mid-conversation** (`/model`, `Alt+P`): Claude Code allows switching models between turns. Striatum lanes are single-shot; switching would require explicit lane configuration, not runtime selection.

### Open questions for the RFC 0010 owner

1. **Should `harness_profile_id` map to the semantic tool name (`claude_code`, `codex`, `gemini_cli`) or a concrete version string (e.g., `claude_code_v2.1.132`)?** Profiles will need to be versioned as tools evolve. Should Striatum pin profiles to tool versions, or let profiles be generic across versions?

2. **Should subagent definitions live in `.claude/agents/` (committed to repo) or in a Striatum manifest file (e.g., `workflow.json`'s `subagent_roles` field)?** The former is more flexible but harder to version alongside workflow changes. The latter is discoverable but less reusable.

3. **Should Striatum validate lane commands against the harness profile at workflow validation time, or defer to runtime?** For example, if a profile says "subagents preferred" but the lane command doesn't load any subagents, should that be a warning or error?

4. **What should a `harness_improvement_proposal` artifact propose?** RFC 0005 mentions this, but it's vague. Should proposals target:
   - **Prompt**: "Add more specific guidance in the work packet to help Claude decide when to delegate"?
   - **Workflow**: "Split this job into two lanes, one for research and one for implementation"?
   - **Defaults**: "Enable agent teams by default for parallel research tasks"?
   - **Documentation**: "Document when to use subagents vs. agent teams"?

5. **For RFC 0009 (supervisor mode), should we codify a standard "lane wrapper" script in Striatum that handles the work packet loop, or leave that to users?** A standard wrapper would make it easy for users to adopt supervised lanes; custom wrappers are more flexible.

6. **Should agent teams ever become first-class Striatum sessions (D021 decision)?** If yes, how do teams leak into artifact ownership, review responsibility, and audit trails?

---

## Recommended schema additions to RFC 0010

### New profile fields

Add these fields to the `claude_code` profile schema to address Striatum's needs:

```json
{
  "tool": "claude_code",
  "strategy_version": "2026-05-08",
  "capabilities": { ... },
  
  "parallelism": {
    "max_subagents": 4,
    "max_agent_teams": 1,
    "team_mode": "internal_to_lane",
    "subagent_routing": "automatic_or_explicit"
  },
  
  "model_config": {
    "primary_model": "claude-opus-4-6",
    "specialist_model": "claude-haiku-4-5",
    "effort_level": "high"
  },
  
  "context_management": {
    "skills_preload": ["striatum-claim-next", "striatum-publish-artifact"],
    "skill_sources": "project_scoped",
    "auto_compaction": true,
    "compaction_alert_threshold": 80
  },
  
  "mcp_config": {
    "mcp_servers": ["striatum"],
    "mcp_auto_discovery": false
  },
  
  "integration": {
    "supervision_compatible": true,
    "stdin_format": "newline_delimited_json",
    "work_packet_mode": "cli_callback_loop"
  }
}
```

### Rationale

- **`parallelism`**: Lets workflows declare expected parallelism (number of subagents, teams) so the planner can reserve resources and warn if a lane is configured incorrectly.

- **`model_config`**: Allows per-lane model selection (Opus for final review, Haiku for research) without hardcoding in the lane command.

- **`context_management`**: Controls which skills load at startup (reducing startup latency) and alerts when context is running low.

- **`mcp_config`**: Declares which MCP servers the lane needs, so they can be provisioned before the lane starts.

- **`integration`**: Explicitly documents that this lane is supervision-compatible and expects newline-delimited JSON work packets.

---

## Summary for dogfood and future work

### For dogfood-001 (RFC 0010 validation)

1. Implement a minimal Claude Code lane with subagent support (no agent teams yet).
2. Define a simple skill wrapper around `striatum` commands (`/claim-next`, `/publish-artifact`).
3. Test `SessionStart` hook to load Striatum permissions.
4. Measure token costs and record as `harness_improvement_proposal` artifacts if optimizations are found.

### For future iterations

1. **RFC 0015** (multi-agent coordination): Decide whether agent teams should graduate to first-class Striatum sessions or remain internal.
2. **Supervision stability**: Upgrade agent teams from experimental to preferred once session resumption and task coordination are stable.
3. **MCP integration**: Publish Striatum as a community MCP server; add to the Anthropic MCP registry.
4. **Hook standardization**: Document recommended hooks for Striatum integration in `.claude/hooks/` templates.
5. **Skills marketplace**: Publish common Striatum skills (claim-next, publish-artifact, complete) as a reusable plugin.

---

## Citations

### Claude Code documentation
- [Create custom subagents - Claude Code Docs](https://code.claude.com/docs/en/sub-agents)
- [Orchestrate teams of Claude Code sessions - Claude Code Docs](https://code.claude.com/docs/en/agent-teams)
- [Extend Claude with skills - Claude Code Docs](https://code.claude.com/docs/en/skills)
- [Hooks in Claude Code](https://code.claude.com/docs/en/hooks)
- [Connect Claude Code to tools via MCP - Claude Code Docs](https://code.claude.com/docs/en/mcp)
- [CLI reference - Claude Code Docs](https://code.claude.com/docs/en/cli-reference)
- [Interactive mode - Claude Code Docs](https://code.claude.com/docs/en/interactive-mode)

### Community resources
- [Claude Code Agent Teams: Setup & Usage Guide 2026](https://claudefa.st/blog/guide/agents/agent-teams)
- [Claude Code Skills Complete Guide: SKILL.md, MCP, Subagents & Teams (2026)](https://duet.so/guides/claude-code-skills-complete-guide)
- [Understanding Claude Code's Full Stack: MCP, Skills, Subagents, and Hooks Explained | alexop.dev](https://alexop.dev/posts/understanding-claude-code-full-stack/)
- [Shipyard | Multi-agent orchestration for Claude Code in 2026](https://shipyard.build/blog/claude-code-multi-agent/)
- [AddyOsmani.com - The Code Agent Orchestra](https://addyosmani.com/blog/code-agent-orchestra/)
- [Awesome Claude Code](https://github.com/hesreallyhim/awesome-claude-code)
- [Everything Claude Code (affaan-m)](https://github.com/affaan-m/everything-claude-code)
- [Claude Code Showcase](https://github.com/ChrisWiles/claude-code-showcase)
- [Claude How-To](https://github.com/luongnv89/claude-howto)
- [GitHub Topics: claude-code-agents](https://github.com/topics/claude-code-agents)
- [Dive into Claude Code (VILA-Lab)](https://github.com/VILA-Lab/Dive-into-Claude-Code)

### Striatum internal
- `docs/SPEC.md` (Adapter Boundary, Supervised Lane Command Contract, Worktree Isolation)
- `docs/MCP.md` (Striatum MCP wrapper)
- `src/striatum/mcp.py` (MCP reference implementation)
