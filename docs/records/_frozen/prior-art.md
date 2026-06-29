# Prior Art Notes

Status: historical reference
Date: 2026-05-06

> **This is reference material from the incubation phase.** The
> table below is the survey that informed the original PRD; it
> is not a list of currently-tracked dependencies or active
> design influences. Any *current* design rationale lives in
> the relevant RFC under `docs/rfcs/` and the
> `docs/DECISION_LOG.md` row that accepts it.

These notes are inspiration, not adopted design. Any product or architecture
choice still needs promotion into `docs/DECISION_LOG.md`.

## Relevant Systems

| System | Relevant Ideas | Notes For striatum |
|--------|----------------|------------------------|
| [Crewly](https://crewlyai.com/) | Orchestrates Claude Code, Gemini CLI, and Codex in live terminal sessions; agents communicate through bash-based skills; includes task management, roles, persistent memory, and a dashboard. | Confirms the terminal-agent orchestration shape. The bash-skill communication idea maps to a CLI fallback, but the product abstraction felt too opinionated for exact-model/headless control. |
| [Forge MCP](https://forgemcp.dev/) | Terminal MCP server with persistent PTY sessions, event subscriptions, pattern matching, `wait_for`, and multi-agent delegation. | Strong signal for an MCP control surface plus event-driven terminal observation. Persistent sessions and "new output only" reads address context and introspection pain. |
| [Agent Swarm](https://www.agent-swarm.dev/) | MCP-powered lead/worker orchestration, persistent memory, task lifecycle states such as unassigned, offered, claimed, in-progress, reviewing, completed. | Useful vocabulary for task lifecycle and claim/offer mechanics. |
| [tmux-ide](https://www.tmux-ide.com/) | Milestone gating, validation contracts, skill-based dispatch, knowledge library, researcher agent, live metrics. | Good precedent for gates, validation contracts, and metrics as first-class orchestration concepts. |
| [TermLoop](https://termloop.ai/) | Runs multiple agents in parallel, often each in its own git worktree; quick actions and mobile control. | Good reminder that write isolation via worktrees may be a later mode, even if v1 supports same-branch workflows. |
| [OctoAlly](https://www.octoally.com/) | Dashboard for Claude Code and Codex sessions; active sessions grid and real-time output. | Confirms the value of a dashboard over raw tmux pane spelunking. |
| [agentmux](https://agentmux.app/) | tmux-based TUI for multiple coding agents; human-in-the-loop, real-time status, notifications, offline-first. | Confirms that a TUI/tmux-native surface is a plausible early dashboard. |
| [oopunsoosu](https://oopunsoosu.com/) | Tmux sessions and YAML task queues with a hierarchy of orchestrator, task manager, and workers. | Supports the idea of workflow data files and explicit task queues. |
| [ANT](https://www.antonline.dev/) | Self-hosted coordination layer for terminals, rooms, prompt cards, linked discussions, evidence trails, and mobile triage. | Useful product vocabulary for rooms/evidence trails; defer unless dashboard scope expands. |
| [crewswarm](https://crewswarm.ai/) | Local-first AI workspace, persistent sessions, dashboard/chat clients, PM-led builds, session resume. | Similar high-level ambition; useful comparison point for local-first orchestration and session resume. |

## Agent-Native Capabilities

- Claude Code supports MCP, custom slash commands, and hooks for tool use,
  notifications, and prompt submission. See [Claude MCP](https://docs.claude.com/en/docs/claude-code/mcp),
  [slash commands](https://docs.claude.com/en/docs/claude-code/slash-commands),
  and [hooks](https://docs.claude.com/en/docs/claude-code/hooks).
- Codex CLI supports MCP configuration and non-interactive execution. See
  [Codex MCP](https://developers.openai.com/codex/mcp) and
  [Codex non-interactive mode](https://developers.openai.com/codex/noninteractive).
- Gemini CLI supports MCP servers and headless/non-interactive execution. See
  [Gemini MCP](https://google-gemini.github.io/gemini-cli/docs/tools/mcp-server.html)
  and [Gemini headless mode](https://google-gemini.github.io/gemini-cli/docs/cli/headless.html).

## Design Ideas To Consider

- Use `striatum` CLI commands as the primary agent control surface:
  `claim-next`, `send`, `complete`, `block`, `publish-artifact`, `read-prompt`,
  and `status`.
- Consider exposing those same operations through MCP later, but do not make
  MCP the core v1 contract.
- Treat task handoff as a structured task envelope, not just a prose prompt.
- Prefer event-driven PTY observation over polling where the adapter supports
  it.
- Keep process-level integration as the minimum portable contract: command,
  cwd, env, stdin, stdout, stderr, exit code, and optional PTY.
- Keep persistent sessions as a supported mode, but let workflows request fresh
  sessions for adversarial reviews and context reset.
