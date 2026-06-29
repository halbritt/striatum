# Getting Started

This guide walks you from a fresh target repository to a running
striatum workflow in about 15 minutes. The default path sets up an
AI agent CLI as the operator; a sidebar covers the rare case of
driving the runner by hand.

## The model: AI operator + human principal

Per [RFC 0053](../rfcs/0053-human-principal-and-terminology-truing.md)
and [D103](../decisions/decision-log.md), striatum has two distinct outside-the-
workflow roles:

- **AI operator** — a coding-agent CLI (Claude Code, Codex, Gemini)
  loaded with the RFC 0015 skill bundle. The operator is the keyboard:
  it registers sessions, claims work, publishes artifacts, completes
  jobs, and handles ordinary review/revision cycles. **This is the
  default driver for every run.**
- **Human principal** — the authority figure who resolves
  unresolvable blockers or decisions. The principal does *not* drive
  normal runs; they look at the inbox when the AI operator escalates,
  decide, and record the resolution. Same CLI surface as the AI
  operator, but role-scoped to escalation.

You set up the AI operator on day zero. The human principal only
appears later, when an escalation surfaces. If you want to drive
the runner by hand (rare — usually only for debugging or
demos), the **Manual operator** sidebar at the end of this guide
covers that path.

## Prerequisites

- Striatum's Go release binaries on `PATH`, or a source checkout where
  `make install` has installed them.
- A target repository (the one you want striatum to orchestrate).
  This is *not* the striatum source repository unless you are
  dogfooding striatum on itself.
- `git` available.
- A reachable PostgreSQL service. Per D094 / RFC 0043 the daemon
  is a hard prerequisite for every Striatum verb, and the daemon
  stores all live workflow state in operator-installed PostgreSQL
  (bundled / Dockerized distributions are deferred). Configure
  the connection through `STRIATUM_DAEMON_DB_URL`,
  `~/.config/striatum/daemon.toml`, or `--postgres-url`. See
  [POSTGRES_TRANSITION.md](../how-to/postgres-transition.md) for the full
  runbook (`daemon migrate-db`, `daemon owner-ddl apply`, daemon startup,
  retired SQLite import handling, repository registration, verification, and
  the documented
  refusal exit codes 11
  `daemon_unreachable` and 12 `repo_not_migrated`).

## Install striatum

Striatum is Go-only (RFC 0078/0079). From a clean checkout the single
path is clone → `make install` → `doctor`:

```bash
git clone https://github.com/halbritt/striatum
cd striatum
make install
```

`make install` (RFC 0079):

1. builds and installs the three Go binaries (`striatum`, `striatumd`,
   `striatum-supervisor-helper`) into `$(PREFIX)/bin` (default
   `~/.local/bin`);
2. runs `striatum daemon install --no-start`, which renders the portable
   systemd **user** unit `~/.config/systemd/user/striatumd.service` (using
   the `%h`/`%t` specifiers — no hardcoded home paths) and scaffolds a
   commented `~/.config/striatum/daemon.toml` if one does not already exist;
3. installs the skill bundle (`striatum skills install`);
4. attempts to start the daemon and runs a health check (best effort).

The daemon refuses to bind a socket until a Postgres DSN is configured, so
on a brand-new host you finish bootstrap with one edit:

```bash
# Set postgres_url in the scaffolded config (or export STRIATUM_DAEMON_DB_URL):
$EDITOR ~/.config/striatum/daemon.toml
striatum daemon install        # re-runs `systemctl --user enable --now`
striatum doctor                # expect: ok
```

`striatum daemon status` summarizes the unit state, runtime layout, and
`doctor` in one view; `make uninstall` reverses the install (binaries +
unit), leaving `daemon.toml` and data intact. The full lifecycle, runtime
layout, and troubleshooting live in
[operator/DAEMON_RUNBOOK.md](../how-to/daemon-runbook.md).

From a release archive instead of a source checkout:

```bash
tar -xzf striatum_2.3.2_linux-amd64.tar.gz
export PATH="$PWD/striatum_2.3.2_linux-amd64/bin:$PATH"
striatum daemon install        # render the user unit + scaffold daemon.toml
striatum --help
```

For the rest of this guide, `striatum` refers to either invocation.

## Manual-operator sidebar (rare — usually skip)

> Skip this unless you specifically want to drive the runner by
> hand (debugging, demo, or you really are the keyboard for now).
> The recommended path is the **Day-zero AI operator setup** below.

If you do want to drive by hand: you will run striatum yourself.

There is no repository-wide default workflow. The quick start below
uses the bundled code-change fixture so you can see the lifecycle
quickly; for choosing the right shape for your own run, see
[WORKFLOW_TYPES.md](../reference/workflow-types.md). That guide also covers lane
selection: whether to use one lane, separate author/reviewer lanes,
multiple model-family review lanes, supervised lanes, or constrained
lanes.

```bash
# The daemon runs as a systemd user service after `make install`
# (`striatum daemon status` to check it). To run it in the foreground
# instead — e.g. on a non-systemd host:
#   striatumd -socket "${XDG_RUNTIME_DIR}/striatum/daemon-go.sock"

# Register and drive the target repo.
TARGET_REPO=/path/to/your/repo
WORKFLOW=examples/code-change-flow/workflow.json   # or choose a type in WORKFLOW_TYPES.md

striatum repo add "$TARGET_REPO" --init --json
striatum --repo "$TARGET_REPO" workflow validate "$WORKFLOW" --json
striatum --repo "$TARGET_REPO" run prepare --workflow "$WORKFLOW" --json
# copy the run_id from the response
striatum --repo "$TARGET_REPO" run start --run-id <run_id> --json
striatum --repo "$TARGET_REPO" dashboard --run-id <run_id> --once
```

The dashboard prints a single frame to stdout: run state, job
counts, claimable work, recent events. From here you register a
session and claim work — see
[HOW_TO_HUMAN.md](../how-to/how-to-human.md), which is now the **escalation
playbook** but retains the full operator-by-hand walkthrough as
reference at the bottom of the page.

## Day-zero AI operator setup

You will register the target repository, install the skill bundle,
and hand the agent your target repo. The agent does the rest.

The current Go installer ships four skill profiles. Pick the one
that matches your agent. To install everything in one shot, use
`--profile all`.

| Agent CLI | Use this profile | Where files land |
|---|---|---|
| Claude Code | `claude_code` | `.claude/skills/striatum-*/SKILL.md` |
| Codex CLI | `codex` | `.codex/agents/striatum-*.md` |
| Agy | `agy` | `.agy/skills/striatum-*/SKILL.md` |
| Anything else | `generic` | `striatum-STRIATUM_AGENT_GUIDE.md` at the repo root |
| Multiple CLIs | `all` | All profiles, deterministic order, no collisions |

`codex` and `agy` reuse the Claude Code skill bodies where their
plugin/skill layout is compatible. `generic` is a single-guide
fallback.

### If your agent is Claude Code

```bash
TARGET_REPO=/path/to/your/repo
striatum repo add "$TARGET_REPO" --init --json
striatum --repo "$TARGET_REPO" skills install --profile claude_code --json
```

The first command registers the target repo with daemon PostgreSQL.
The second writes the RFC 0015 skill bundle to
`.claude/skills/striatum-*/`. The bundle teaches a Claude Code session
how to drive the runner without reading the striatum source.

### If your agent is Codex CLI

```bash
TARGET_REPO=/path/to/your/repo
striatum repo add "$TARGET_REPO" --init --json
striatum --repo "$TARGET_REPO" skills install --profile codex --json
```

Writes the same five-skill bundle as Claude Code, flat-file at
`.codex/agents/striatum-{workflow,scaffold,claim-loop,supervise,recover}.md`.

### If your agent is Agy

```bash
TARGET_REPO=/path/to/your/repo
striatum repo add "$TARGET_REPO" --init --json
striatum --repo "$TARGET_REPO" skills install --profile agy --json
```

Writes the skill bundle under `.agy/skills/striatum-*/`.

### If your agent is anything else

```bash
TARGET_REPO=/path/to/your/repo
striatum repo add "$TARGET_REPO" --init --json
striatum --repo "$TARGET_REPO" skills install --profile generic --json
```

Writes `striatum-STRIATUM_AGENT_GUIDE.md` at the repo root.

### Install everything (you switch CLIs, or you want a fallback)

```bash
striatum repo add "$TARGET_REPO" --init --json
striatum --repo "$TARGET_REPO" skills install --profile all --json
```

Fans out across all four profiles in deterministic order. The
profiles write to disjoint paths and each carries its own
manifest, so they don't collide.

### Scaffold a workflow

```bash
striatum --repo "$TARGET_REPO" workflow generate \
  --shape code_change \
  --workflow-id my-change \
  --scaffold-root striatum/workflows/my-change \
  --artifact-root striatum/my-change \
  --write \
  --json
```

This creates `workflow.json`, roles, and prompt stubs in the committed
`striatum/workflows/my-change` tree and points expected artifacts at
`striatum/my-change`. Edit the prompts and lane commands before
preparing a real run.

### Now drive the run

Point your agent at `$TARGET_REPO`. Tell it: *"drive the workflow
at `<path>/workflow.json` using striatum"*. The agent loads the
bundle, registers a session, claims work, and proceeds.
For the long-form companion to the bundle, see
[HOW_TO_AGENT.md](../how-to/how-to-agent.md).

## What's in `.striatum/`?

After `repo add --init`, the target repo contains operational scratch:

```text
.striatum/
  scratch/            # per-supervisor named pipes / pidfiles (RFC 0009)
  bin/                # optional; e.g., claude-supervised-wrapper.sh
```

`.striatum/` should be treated as operational scratch only.
Authoritative workflow state lives in the daemon-owned PostgreSQL
instance under a `repository_id` scope (D094 / RFC 0043). `repo add --init`
registers the repository and creates scratch without creating `retired-local-state`; if
the directory already carried a V1 `retired-local-state`
from a pre-D094 install, archive or remove that legacy SQLite file before
driving workflow verbs.

Keep `.striatum/` in the target repo's `.gitignore`. Repository
files outside `.striatum/` (artifacts, decisions, evidence exports)
are durable provenance and should be committed normally.

### Where will the workflow's output land?

striatum has **no** built-in output directory. The location of
every artifact comes from the workflow file itself: each job
declares `expected_artifacts[].path` and
`write_scope.allowed_paths`. The runner accepts those paths
verbatim.

If you are trying striatum on a real repo and want the runner's
output corralled (so a single `rm -rf` cleans up if you change
your mind), the recommended convention is a top-level
`striatum/` directory — sibling to the gitignored `.striatum/`
scratch directory but committed:

```text
<your-repo>/
├── .striatum/             # gitignored operational scratch
└── striatum/              # committed durable output
    └── <workflow-slug>/
        ├── RUN_SUMMARY.md
        ├── RUN_EVIDENCE.md
        └── ...
```

This is just a convention — your workflow chooses its own
paths. See [WRITING_WORKFLOWS.md § "Recommended output
layout"](../how-to/writing-workflows.md#recommended-output-layout) for
the full pattern, and [HOW_TO_HUMAN.md § "Where artifacts
land"](../how-to/how-to-human.md#where-artifacts-land) for how to adapt
an existing example fixture.

## Where to next

- **[HOW_TO_AGENT.md](../how-to/how-to-agent.md)** — the load-bearing playbook
  for the AI operator (the default driver). Long-form companion to
  the skill bundle.
- **[HOW_TO_HUMAN.md](../how-to/how-to-human.md)** — the human principal's
  escalation playbook (RFC 0053): how to resolve a blocker or
  decision when the AI operator escalates. Retains the manual-driver
  walkthrough at the bottom as a reference for the rare hands-on case.
- **[WORKFLOW_TYPES.md](../reference/workflow-types.md)** — choose a workflow
  shape and lane set; explains current starter styles, examples, and
  defaults.
- **[WRITING_WORKFLOWS.md](../how-to/writing-workflows.md)** — author your
  own `workflow.json` from scratch.
- **[CLI_REFERENCE.md](../reference/cli-reference.md)** — every CLI verb,
  flat list, with stable exit codes.
- **[POSTGRES_TRANSITION.md](../how-to/postgres-transition.md)** — operator
  runbook for the D094 / RFC 0043 PostgreSQL cutover, retired SQLite
  import handling, and repository registration.
- **[SPEC.md](../reference/spec.md)** — the implementation contract for the
  current V1 surface.
- **[INDEX.md](../index.md)** — every doc in `docs/` with a one-line
  summary.

## How to contribute

The striatum source tree's contributor rules live in
[`AGENTS.md`](../../AGENTS.md) at the repository root. The Makefile
targets `install`, `lint`, `typecheck`, `test`, and `smoke` are
the supported entry points; pull requests are expected to keep
all four green.
