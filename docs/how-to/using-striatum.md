# Using Striatum — Day-zero guide

This is the doc to read first. It covers what Striatum is, the two
named roles (AI operator + human principal), what you need
installed, how to start a workflow, and what your role looks like
when something escalates. Plan ~10 minutes.

If you want a quick install summary instead, see the
[`README.md`](../../README.md). For the long-form playbooks, see
[`how-to-human.md`](../how-to/how-to-human.md) and
[`how-to-agent.md`](../how-to/how-to-agent.md).

When resuming Striatum development work, read
[`operator/BRIEF.md`](../operator/BRIEF.md) after the project instructions.
That brief is the bounded current-state handoff; older
`docs/handoffs/` files are provenance.

## What is Striatum?

Striatum is a local workflow runner for terminal-based AI coding
agents. It coordinates draft → review → repair → synthesize loops
across multiple agents (Codex, Claude Code, agy (Antigravity), or any
runtime you can wrap as a command), records every state transition
in a daemon-owned PostgreSQL audit chain, and never touches a hosted
service.

The runner does not decide that an agent is done because a terminal printed a
phrase. Agents move work through daemon MCP/RPC methods, humans use the local
web UI for routine operator actions, and the `striatum` CLI remains a
daemon-backed bootstrap, diagnostics, and compatibility client.

## The two roles

Striatum runs with two named roles. RFC 0053 fixes the model.

**AI operator** — the default driver.

- Claims work, publishes artifacts, advances state through daemon MCP tools.
- Humans have local web UI and CLI compatibility access, but routine lane work
  belongs to the AI operator.
- Long-form companion to the operator skill bundle:
  [`how-to-agent.md`](../how-to/how-to-agent.md).

**Human principal** — escalation only.

- Resolves blockers the AI judges itself stuck on (`escalation`
  artifacts or declared blocker classes like `human_checkpoint`).
- Watches the inbox; investigates when something paged; signs off
  on the change.
- Long-form playbook:
  [`how-to-human.md`](../how-to/how-to-human.md).

Routine work belongs to the operator. If you find yourself using CLI fallback
to push a healthy run forward, the operator harness should be improved.

## Prerequisites

- **Striatum Go binaries.** Install from a GitHub release archive or
  from a source checkout with `make install`. Normal operator use does
  not require Python or a virtual environment. Striatum is a compiled Go
  binary application backed by PostgreSQL. The legacy Python runtime, Python CLI
  wrappers, and direct SQLite databases have been completely retired.
- **PostgreSQL 14+** running locally. The daemon is a hard
  prerequisite; SQLite is no longer the live substrate (D094 /
  RFC 0043). See [`postgres-transition.md`](../how-to/postgres-transition.md)
  for the install runbook including the `striatumd_rw` role
  provisioning.
- **An agent runtime** — Claude Code, Codex, agy (Antigravity), or any
  CLI tool that takes a session prompt and writes a deliverable.
  Striatum provides a skill bundle for the first three (claude_code, agy, codex);
  `--profile generic` writes a paste-into-system-prompt guide for
  anything else.
- **Optionally**, a target repository you want to orchestrate.
  Striatum can register multiple target repos with the same
  daemon.

## Day-zero setup

```bash
# 1. Install from a Go release archive, or from source with `make install`.
tar -xzf striatum_2.0.0_linux-amd64.tar.gz
export PATH="$PWD/striatum_2.0.0_linux-amd64/bin:$PATH"

# 2. Configure PostgreSQL, then apply daemon DB bootstrap work.
# Set postgres_url in ~/.config/striatum/daemon.toml or export
# STRIATUM_DAEMON_DB_URL / STRIATUM_DAEMON_ADMIN_DB_URL first.
striatum daemon install --no-start --json
striatum daemon migrate-db --json
striatum daemon owner-ddl apply --json

# 3. Start the user service, or use foreground striatumd.
systemctl --user start striatumd
# OR: striatumd -socket "${XDG_RUNTIME_DIR}/striatum/daemon-go.sock"
striatum daemon status

# 4. Register a target repo, then install agent-side skills/plugins.
TARGET_REPO=/path/to/your/repo
striatum repo add "$TARGET_REPO" --init --json
striatum --repo "$TARGET_REPO" skills install --profile claude_code --json
striatum --repo "$TARGET_REPO" plugin install --profile claude_code --json

# 5. Check repository registration, daemon reachability, and posture.
striatum --repo "$TARGET_REPO" doctor --verbose --json
```

What this setup does:

- Registers the repo with the daemon-owned Postgres substrate.
- Writes the operator skill bundle to the agent's project-scope location
  such as `.claude/skills/striatum-*/`, `.agy/skills/striatum-*/`, or `.codex/agents/striatum-*.md`
  (`~/.codex/agents/striatum-*.md` for user-scope Codex installs).
- Writes the agent-CLI plugin bundle for the selected profile when one
  exists. (The agy profile reuses the claude_code plugin and skill templates
  wholesale via standard imports under .agy/plugins/striatum/).

On first initiation the operator skill bundle also prompts you about
**optional, third-party agent skills** (for example a divergent-ideation
skill). These are not part of Striatum or its runtime — they install
agent-side and Striatum never fetches, vendors, or calls them. The operator
installs only the ones you confirm. The curated registry lives in
[`skills/optional/`](../../skills/optional/README.md).

If the target repo follows the recommended layout in
[`consumer-repo-layout.md`](../reference/consumer-repo-layout.md), your
workflow file lives under `striatum/workflows/` and artifacts land
under `striatum/<workflow-name>/`. Generated workflow trees use
`striatum/workflows/<name>/workflow.json`.
For an empty target repo, use `workflow generate --scaffold-root` and
`--artifact-root` to create that layout with a concrete workflow.

## Your first run

```bash
WORKFLOW="$TARGET_REPO/striatum/workflows/code-change/workflow.json"
# Or use a starter from the repo:
WORKFLOW=examples/code-change-flow/workflow.json

# Validate the workflow JSON against the schema.
striatum --repo "$TARGET_REPO" workflow validate "$WORKFLOW" --json

# Prepare a run (records the workflow snapshot + creates the runs row).
striatum --repo "$TARGET_REPO" run prepare --workflow "$WORKFLOW" --json
# -> note the run_id from the envelope

# Confirm a working branch (optional — required for code-change workflows).
striatum --repo "$TARGET_REPO" branch confirm \
  --run-id <run_id> --branch striatum/example --json

# Start the run.
striatum --repo "$TARGET_REPO" run start --run-id <run_id> --json

# Watch progress.
striatum --repo "$TARGET_REPO" dashboard --run-id <run_id> --once
# OR for a live terminal view:
striatum --repo "$TARGET_REPO" dashboard --run-id <run_id>
```

Now hand the agent the run. With Claude Code or agy, open an interactive
session in the target repo (e.g. using agy -i or agy --continue) and tell it:

> Drive the workflow at `striatum/workflows/code-change/workflow.json`
> using striatum.

The operator skill bundle teaches the agent the daemon MCP methods it needs
(`work.await_packet`, `work.ack`, `work.complete`, `artifact.publish`,
`review.verdict`, `review.submit`, recovery methods). The agent
self-supervises through the loop until the run completes or hits a blocker it
can't resolve.

### Watching agent sessions

Use daemon state first:

```bash
striatum --repo "$TARGET_REPO" status --run-id <run_id> --json
striatum --repo "$TARGET_REPO" dashboard --run-id <run_id>
striatum --repo "$TARGET_REPO" supervise status --session-id <session_id> --json
```

Under RFC 0088, interactive agent sessions run in daemon-owned persistent PTY
lanes managed directly by `striatumd`. While there is no strict tmux dependency for
execution, human operators can easily monitor active progress through the local web UI,
the console dashboard, or by attaching to the daemon's allocated PTY wrappers.

Pane text, terminal output, and transcripts are not workflow state. Do
not infer completion, verdicts, blockers, or authority from what appears
in PTY sessions; use Striatum commands and durable artifacts for that.

### Recovery triage

| Visible state | Inspect with | Recovery action |
|---|---|---|
| Stale lease or no heartbeat. | `recovery stale-leases --run-id <run_id> --json` | Requeue only review-safe work with `recovery requeue-stale`. |
| Lost process or supervisor issue. | `recovery process-reconcile --run-id <run_id> --json` | Requeue or publish on behalf only after reconcile reports the safe next action. |
| `human_checkpoint`. | `why <blocker_id> --run-id <run_id> --json` | Record a decision, then run `checkpoint resolve`. |
| Principal inbox escalation. | `inbox --json` | Decide, run `decision record`, then run `escalation resolve`. |
| Terminal run with active sessions. | `doctor --run-id <run_id> --verbose --json` | Close listed sessions with `session close --reason terminal-run-cleanup`. |

## Your role as principal

The AI operator is the default driver. You're escalation-only.
What that looks like in practice:

```bash
# 1. Check the inbox for escalations + open blockers.
striatum --repo "$TARGET_REPO" inbox --json

# 2. For a stuck run, look at the dashboard and ask `why`.
striatum --repo "$TARGET_REPO" status --run-id <run_id> --json
striatum --repo "$TARGET_REPO" why <target_id> --run-id <run_id>

# 3. Resolve a checkpoint or recover stale work.
striatum --repo "$TARGET_REPO" checkpoint resolve \
  --blocker-id <id> --action continue --json
# OR, for a revision_routing checkpoint, accept the verdict as
# superseded by a recorded decision (no re-review):
striatum --repo "$TARGET_REPO" checkpoint resolve \
  --blocker-id <id> --action override --decision-id <decision_id> --json
# OR
striatum --repo "$TARGET_REPO" recovery requeue-stale \
  --run-id <run_id> --job-id <job_id> --force \
  --justification "<reason>" --json
```

The principal's playbook —
[`how-to-human.md`](../how-to/how-to-human.md) — covers the full set of
recovery verbs with the documented reasons each is appropriate.
The short version: don't reach for these by default. If the
operator AI is repeatedly hitting the same blocker, that's
harness friction worth filing in `reference/harness-friction-patterns.md`.

## Where to go next

- **Long-form playbooks** —
  [`how-to-human.md`](../how-to/how-to-human.md) for the human principal,
  [`how-to-agent.md`](../how-to/how-to-agent.md) for the AI operator (and
  the long-form companion to the skill bundle).
- **The implementation contract** — [`spec.md`](../reference/spec.md). When
  this doc disagrees with the runner, the SPEC wins.
- **Every CLI verb + stable exit codes** —
  [`cli-reference.md`](../reference/cli-reference.md).
- **Workflow shapes and lane sets** —
  [`workflow-types.md`](../reference/workflow-types.md).
- **Authoring a workflow.json** —
  [`writing-workflows.md`](../how-to/writing-workflows.md).
- **System architecture (Mermaid)** —
  [`README.md`](../../README.md) §"At a glance".
- **Postgres prerequisites + repository registration** —
  [`postgres-transition.md`](../how-to/postgres-transition.md).
- **Target-repo layout recommendations** —
  [`consumer-repo-layout.md`](../reference/consumer-repo-layout.md).
- **Every doc in `docs/`** — [`index.md`](../index.md).
