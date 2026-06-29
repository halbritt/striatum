# RFC 0025: Agent-CLI Plugin Bundles

Status: accepted (V1)
Date: 2026-05-09
Context:
`docs/SPEC.md` § "Adapter Boundary",
`docs/DECISION_LOG.md` (D006, D009, D020, D028),
`docs/rfcs/0010-tool-harness-profiles.md`,
`docs/rfcs/0012-local-service-api.md`,
`docs/rfcs/0015-self-contained-agent-skills.md`,
`src/striatum/skills/`,
[Claude Code plugins](https://code.claude.com/docs/en/plugins),
[Claude Code plugins reference](https://code.claude.com/docs/en/plugins-reference),
[Codex CLI plugins](https://developers.openai.com/codex/plugins),
[Codex CLI build plugins](https://developers.openai.com/codex/plugins/build),
[Gemini CLI extensions](https://geminicli.com/docs/extensions/),
[Gemini CLI extension reference](https://geminicli.com/docs/extensions/reference/)

Implemented in dogfood-028 (V1 step 1: `claude_code` plugin profile) and
dogfood-029 (V1 steps 2+3: `codex` and `gemini` plugin profiles).

## Problem

RFC 0015 ships per-tool **skills** for Claude Code, Codex (custom-agent
roles), Gemini (best-effort generic), and a generic Markdown guide.
Skills are loose Markdown files at conventional paths
(`.claude/skills/`, `.codex/agents/`, etc.) and are sufficient for an
operator who is willing to (a) regenerate them via
`striatum skills install` after every Striatum upgrade and (b) drive the
agent CLI by hand.

Since RFC 0015 V1 landed, all three target agent CLIs have promoted
skills into a higher-level packaging unit:

- **Claude Code** ships a plugin format (`.claude-plugin/plugin.json`)
  that bundles skills, slash commands, sub-agents, hooks, MCP servers,
  LSP servers, and monitors under one manifest, namespaced by plugin
  name (e.g. `/striatum:claim-next`), with marketplace-driven install
  via `/plugin install <name>@<marketplace>`.
- **Codex** (≥ v0.117.0) ships a near-identical plugin format
  (`.codex-plugin/plugin.json`) that bundles skills, MCP servers,
  app connectors, and hooks, installable from local or git
  marketplaces via `codex plugin marketplace add` plus a
  `marketplace.json` directory.
- **Gemini CLI** ships **extensions** (`gemini-extension.json` at the
  extension root) that bundle a `GEMINI.md` context file, custom
  commands under `commands/`, MCP servers, hooks, skills, and
  sub-agents. Installed via `gemini extensions install <source>`.

Loose skill files do not benefit from any of this. They cannot ship
with their slash-command counterparts; they cannot ship a Striatum MCP
server entry alongside the prose; they cannot be installed once into
`~/.claude/plugins/` and reused across many target repos by name; and
they offer no version handshake when an operator's installed Striatum
moves and the on-disk skills go stale.

This RFC defines how Striatum emits a **plugin bundle** for each of the
three agent CLIs that already understand the concept, so an operator
with one `striatum plugin install` invocation gets a packaged,
namespaced, version-stamped bundle that the agent CLI can install
through its own native mechanism.

## Goals

- After a single `striatum plugin install --profile <id>` invocation,
  the target environment contains a plugin bundle in the on-disk
  layout the agent CLI expects, with a valid manifest and the same
  skill content RFC 0015 already generates.
- One plugin per agent-CLI family covers: skills (carry-over from
  RFC 0015), the small set of imperative slash commands an operator
  uses by name (`claim-next`, `status`, `why`, `dashboard`,
  `doctor`), and a hook stub the operator can wire up later.
- Bundles are regenerable, idempotent, version-stamped, and refuse to
  clobber operator edits without `--force` (same protocol as
  `skills install`).
- The bundle stays self-contained per RFC 0015 D020: no external
  URLs, no source-repo dependency, no hosted services. Generation
  happens offline from packaged templates.
- Profiles `claude_code`, `codex`, `gemini` ship at parity in V1.
  `generic` continues to mean "single Markdown guide; no plugin
  packaging" and is unchanged from RFC 0015.
- A local marketplace fixture is emitted alongside the bundle so the
  operator can register it via the agent CLI's own marketplace
  mechanism (Claude Code, Codex) without standing up a git
  repository or hosted service.

## Non-Goals

- A hosted Striatum plugin marketplace, version registry, or
  update-fetch service. D020 stands; bundles ship inside the
  installed Python distribution.
- Auto-installing plugins into the agent CLI's user-scope directory
  on the operator's behalf. The runner emits files; the operator
  runs the agent CLI's install command.
- A Striatum MCP server. RFC 0010 V2 deferred MCP-based supervision
  and per-packet skill installation; this RFC is silent on those.
  The plugin manifest's `mcpServers` entry, where present, ships
  empty in V1 and is reserved for a future RFC that lands a
  Striatum MCP server.
- Generating workflow-specific task prompts. Task prompts live on
  the workflow's jobs (RFC 0015 § Non-Goals).
- Replacing `skills install`. The two verbs coexist; the plugin
  surface is a strict superset for the three CLIs that support it
  and a no-op for `generic`.
- Native parity for agent CLIs that lack a plugin format
  (the historical `generic` profile). Those continue to be served
  by `skills install --profile generic`.

## Proposal

### 1. New CLI verb: `striatum plugin`

```text
striatum plugin install
  [--target <path>]            # default: target repo root
  [--profile <id>]             # claude_code | codex | gemini | all (default: claude_code)
  [--scope project|user]       # default: project; user writes under ~/
  [--namespace <prefix>]       # default: striatum
  [--force]                    # overwrite operator-edited files
  [--dry-run]                  # print plan, write nothing
  [--with-marketplace]         # also emit marketplace.json fixture (default on for claude_code, codex)
  [--json]
```

`striatum plugin uninstall --profile <id>` removes a previously
generated bundle's tracked files (manifest-driven; same edit-detect
protocol as `--force`).

The same packaged-template mechanism that powers RFC 0015 powers
this verb. Templates live at
`src/striatum/plugins/templates/<profile>/...` and reuse the
existing `src/striatum/skills/templates/<profile>/` skill bodies
verbatim — the plugin layer is a packaging change, not a content
rewrite.

### 2. Per-profile bundle layout

V1 ships three first-class plugin profiles. Paths are written
relative to `--target` (the target repo) when `--scope project`,
or under `~/` when `--scope user`.

#### 2.1 `claude_code`

Project scope writes under `.striatum/plugins/claude_code/`:

```
.striatum/plugins/claude_code/
├── .claude-plugin/
│   └── plugin.json
├── skills/
│   ├── striatum-workflow/SKILL.md
│   ├── striatum-scaffold/SKILL.md
│   ├── striatum-claim-loop/SKILL.md
│   ├── striatum-supervise/SKILL.md
│   └── striatum-recover/SKILL.md
├── commands/
│   ├── claim-next.md
│   ├── status.md
│   ├── why.md
│   ├── dashboard.md
│   └── doctor.md
├── hooks/
│   └── hooks.json            # empty handlers + commented stubs
├── .mcp.json                 # empty {} placeholder (reserved)
├── .manifest.json            # Striatum-side regeneration manifest
└── README.md                 # how to install via `/plugin install ./...`
```

`.claude-plugin/plugin.json` carries:

```json
{
  "name": "striatum",
  "description": "Drive Striatum workflows from Claude Code: claim work, publish artifacts, recover stalled runs.",
  "version": "0.7.3",
  "author": {"name": "Striatum"},
  "license": "MIT"
}
```

User scope (`--scope user`) writes under
`~/.claude/plugins/striatum/` instead. After install, the operator
runs `claude --plugin-dir .striatum/plugins/claude_code` (project)
or `/plugin install ./.striatum/plugins/claude_code` (project,
session-scoped) to load the bundle. `--with-marketplace` also
emits a sibling `.striatum/plugins/marketplace.json` so the
operator can `/plugin marketplace add ./.striatum/plugins/` and
install via `/plugin install striatum@local-striatum`.

The five skills are byte-identical to RFC 0015's
`claude_code` profile output. Skill files in a plugin bundle use
namespaced invocation (`/striatum:claim-loop`), which the bundle's
own `commands/*.md` slash commands are also reachable through
(e.g. `/striatum:claim-next`).

#### 2.2 `codex`

Project scope writes under `.striatum/plugins/codex/`:

```
.striatum/plugins/codex/
├── .codex-plugin/
│   └── plugin.json
├── skills/
│   ├── striatum-workflow/SKILL.md
│   └── ... (same five as claude_code)
├── hooks/
│   └── hooks.json
├── .mcp.json                 # empty {} placeholder
├── .manifest.json
└── README.md
```

`.codex-plugin/plugin.json`:

```json
{
  "name": "striatum",
  "version": "0.7.3",
  "description": "Drive Striatum workflows from Codex.",
  "skills": "./skills/",
  "mcpServers": "./.mcp.json",
  "hooks": "./hooks/hooks.json",
  "interface": {
    "displayName": "Striatum",
    "shortDescription": "Local-first workflow runner for terminal AI agents.",
    "category": "Developer Tools"
  }
}
```

`--with-marketplace` emits `.striatum/plugins/marketplace.json` with
a `{"source": "local", "path": "./codex"}` entry. The operator runs
`codex plugin marketplace add ./.striatum/plugins/` and then
`@striatum` from inside Codex.

#### 2.3 `gemini`

Project scope writes under `.striatum/plugins/gemini/`:

```
.striatum/plugins/gemini/
├── gemini-extension.json
├── GEMINI.md                 # context: top-level Striatum-driving guide
├── commands/
│   ├── claim-next.toml
│   ├── status.toml
│   ├── why.toml
│   ├── dashboard.toml
│   └── doctor.toml
├── skills/                   # carry-over from RFC 0015 generic
│   └── striatum-workflow/SKILL.md ... (five files)
├── agents/
│   └── striatum-recover.md
├── .manifest.json
└── README.md
```

`gemini-extension.json`:

```json
{
  "name": "striatum",
  "version": "0.7.3",
  "description": "Drive Striatum workflows from Gemini CLI.",
  "contextFileName": "GEMINI.md",
  "excludeTools": []
}
```

User scope writes under `~/.gemini/extensions/striatum/`. From
project scope the operator runs
`gemini extensions install ./.striatum/plugins/gemini` to copy the
bundle into `~/.gemini/extensions/striatum/`. Gemini's extension
mechanism does not have a marketplace concept comparable to the
other two, so `--with-marketplace` is a no-op for this profile and
prints a notice instead of writing a file.

This is also the first time `gemini` is a first-class profile;
RFC 0015 V1 fell back to `generic`. The skill bodies that were
already generated under `src/striatum/skills/templates/gemini/`
(currently a single `STRIATUM_GEMINI_GUIDE.md.tmpl`) are split into
the same five-skill shape Claude Code uses, sharing the underlying
content. The Markdown guide remains available under
`skills install --profile generic` for tools without an extension
format.

### 3. Generation manifest

Each bundle root contains `.manifest.json` (separate from the
agent-CLI's own manifest):

```json
{
  "schema_version": "striatum.plugins.manifest.v1",
  "striatum_version": "0.7.3",
  "generated_at": "2026-05-09T18:42:11Z",
  "profile": "claude_code",
  "namespace": "striatum",
  "files": [
    {
      "path": ".claude-plugin/plugin.json",
      "sha256": "ab12...",
      "template": "claude_code/plugin.json.tmpl"
    },
    {
      "path": "skills/striatum-claim-loop/SKILL.md",
      "sha256": "cd34...",
      "template": "claude_code/skills/striatum-claim-loop.md.tmpl"
    }
  ]
}
```

The manifest is the single source of truth for "did the operator
edit this file?". It is not consulted by the runner at workflow
time; it is read only by `plugin install` and `doctor`. Same
contract as RFC 0015's `.manifest.json` for skills.

### 4. Marketplace fixture

For `claude_code` and `codex`, `--with-marketplace` (default on) also
writes `.striatum/plugins/marketplace.json`:

```json
{
  "name": "local-striatum",
  "interface": {"displayName": "Local Striatum"},
  "plugins": [
    {
      "name": "striatum",
      "source": {"source": "local", "path": "./claude_code"},
      "policy": {"installation": "AVAILABLE"},
      "category": "Developer Tools"
    },
    {
      "name": "striatum",
      "source": {"source": "local", "path": "./codex"},
      "policy": {"installation": "AVAILABLE"},
      "category": "Developer Tools"
    }
  ]
}
```

A marketplace.json that already exists at the same path is treated
under the same edit-detect / `--force` rules as any other generated
file. Profiles whose marketplace entry isn't present in the file
are appended; profiles already listed are updated in place when
their version moves.

### 5. Slash command set

V1 ships exactly five slash commands per profile, each a thin
imperative wrapper around the CLI. They exist so operators don't
have to type the full Striatum verb in chat:

| Command | Wraps | Notes |
|---|---|---|
| `claim-next` | `striatum claim-next --session-id $S --json` | Reads `session_id` from env or prompts. |
| `status` | `striatum status --run-id $RUN --json` | |
| `why` | `striatum why --run-id $RUN --job-id $JOB` | |
| `dashboard` | `striatum dashboard --run-id $RUN --once` | Single-frame mode for chat. |
| `doctor` | `striatum doctor --verbose --json` | |

Slash-command files use each CLI's native format: Markdown for
Claude Code, Markdown for Codex (sharing the same body shape), and
TOML for Gemini CLI. Bodies are templated from the parser, exactly
as `skills install` already does.

### 6. Hook stub

V1 emits an empty `hooks/hooks.json` (or `hooks.json` for Gemini)
with commented stubs the operator can opt into. No hooks fire by
default. Two stubs are shipped commented out:

```json
{
  "hooks": {
    "Stop": [
      // Example: auto-call `striatum complete` when the agent finishes.
      // Uncomment and adjust to fit your workflow.
      // {"matcher": "*", "hooks": [{"type": "command", "command": "striatum complete --session-id $SESSION_ID --job-id $JOB_ID"}]}
    ],
    "PreToolUse": [
      // Example: gate file writes against the work packet's write_scope.
      // {"matcher": "Write|Edit", "hooks": [{"type": "command", "command": "striatum check-write-scope --path $TOOL_PATH"}]}
    ]
  }
}
```

The runner does not interpret these hooks; they exist so
`plugin install` is the obvious place an operator adds them, and so
operator edits to `hooks.json` are protected by the manifest
edit-detect rule.

### 7. `striatum init --with-plugins` and doctor checks

- `striatum init` gains `--with-plugins [profile]`. When passed, the
  same code path as `plugin install` runs immediately after
  `.striatum/` is created. Mirrors RFC 0015's `--with-skills`.
  `--with-skills` continues to work and is independent.
- `striatum doctor` adds two checks:
  - `plugin_missing` — the manifest says a bundle should exist for
    a profile that operators are using, but the bundle directory
    is gone.
  - `plugin_outdated` — the manifest's `striatum_version` is older
    than `striatum.__version__`, or templates' hashes differ from
    the manifest.
  Both surface the exact `striatum plugin install` invocation that
  fixes them. Doctor never auto-regenerates.

### 8. Self-contained guarantee

The same three guarantees from RFC 0015 § Self-contained extend to
plugin bundles:

- **No external links.** Generation refuses to emit a URL outside
  the bundle's namespace directory. Cross-skill references use
  relative paths inside the bundle.
- **No source-repo dependency.** Templates ship inside the
  installed Python package.
- **Self-describing version.** Each bundled file's header records
  the runner version. If a bundle and the installed runner
  disagree, the operator is told to regenerate.

## Acceptance Criteria

- `striatum plugin install --profile claude_code` in an empty
  target writes the directory tree above (manifest +
  `.claude-plugin/plugin.json` + 5 skills + 5 commands +
  `hooks/hooks.json` + `.mcp.json` + `README.md`), and a second
  invocation with the same install is byte-identical.
- `striatum plugin install --profile codex` writes the parallel
  `.codex-plugin/`-rooted bundle.
- `striatum plugin install --profile gemini` writes
  `gemini-extension.json` + `GEMINI.md` + `commands/` (TOML) +
  `skills/` (five SKILL.md) + `agents/`. Promotes `gemini` to a
  first-class profile (closes the RFC 0015 V1 fallback).
- `striatum plugin install --profile all` writes all three
  bundles plus a single shared `marketplace.json`.
- After install, an operator-modified
  `commands/claim-next.md` is preserved across a re-run; `--force`
  overwrites it; `--dry-run` prints a plan that names the conflict.
- `striatum init --with-plugins` produces a target tree containing
  `.striatum/` operational scratch plus the requested plugin bundle; it
  does not create live workflow state in `retired-local-state` for current
  production runs.
- `striatum doctor` reports `plugin_missing` for a target whose
  manifest references a bundle that has been deleted, and
  `plugin_outdated` after the package is upgraded but
  `plugin install` has not been re-run.
- A Claude Code session pointed at a fresh clone of an unrelated
  target repo, after `striatum init --with-plugins claude_code`,
  can run `/plugin marketplace add ./.striatum/plugins/`,
  `/plugin install striatum@local-striatum`, and then drive a
  packet through the claim/ack/publish/complete loop using only
  the bundle.
- The Codex profile produces a bundle that survives
  `codex plugin marketplace add ./.striatum/plugins/` followed by
  install via the Codex marketplace browser.
- The Gemini profile produces a bundle installable via
  `gemini extensions install ./.striatum/plugins/gemini`.
- Generated bundle content contains no URLs and no paths outside
  the bundle root. A unit test enforces this on rendered output
  (mirrors RFC 0015's no-external-URL invariant).
- `tests/test_plugin_install.py` covers profile selection,
  idempotent regeneration, edit-detection refusal, `--force`,
  `--dry-run`, manifest shape, marketplace.json append/update
  semantics, and the no-external-URL invariant.

## Open Questions

- **Plugin vs. skill double-install.** A target that runs both
  `skills install` and `plugin install` for the same profile ends
  up with two copies of the same skill content (once in
  `.claude/skills/`, once in `.striatum/plugins/claude_code/skills/`).
  V1 leaves them independent; doctor surfaces both manifests.
  Open question: should `plugin install --profile claude_code`
  imply removing the loose `.claude/skills/striatum-*/`, or stay
  silent? Default-silent is safest; reviewer pushback welcome.
- **MCP server entry shape.** The `.mcp.json` placeholder is
  empty in V1 because Striatum has no MCP server today. RFC 0010
  V2 deferred this. When that future RFC lands, the manifest's
  `mcpServers` entry becomes load-bearing — should this RFC
  pre-reserve a stable shape (e.g.
  `{"striatum": {"command": "striatum", "args": ["mcp", "serve"]}}`)
  or wait for the MCP RFC to define it? V1 reserves the file path
  but ships an empty object so adding the server later is
  additive.
- **Slash command surface size.** V1 ships five commands. A
  larger surface (`requeue-stale`, `submit-review`,
  `publish-artifact`, `worktree-create`, `decision`) would cover
  more of the agent's actual flow but bloats the install. V1
  picks the five that show up most often in operator chat
  transcripts; promotion is additive.
- **Marketplace name collisions.** The marketplace fixture is
  named `local-striatum`. If two target repos both register their
  fixtures into the same Claude Code user scope, the second
  `marketplace add` either overwrites or errors depending on the
  agent CLI's policy. V1 documents the collision; an opt-in
  `--marketplace-name` flag is a follow-up.
- **Codex `apps/` and `assets/`.** The Codex manifest accepts an
  `apps` field (app connectors) and `assets/` (icons, brand
  imagery). V1 omits both — Striatum has no hosted-service
  connector and no brand assets to ship. Operators can add them
  by hand; the manifest's edit-detect protects their additions.
  Promoting to first-class is a follow-up.
- **Gemini extensions install flow with a directory prefix.** The
  Gemini docs say `gemini extensions install <source>` where
  `<source>` is a path or git URL. V1 documents the path form.
  Whether bundling a per-target git repo is preferable to the
  copy-into-`~/.gemini/extensions/` flow is operator preference;
  V1 stays with copy.
- **Bundling RFC 0015's `generic` profile.** `generic` does not
  map to any plugin format. V1 leaves it under `skills install`.
  An open question: should `plugin install --profile all` skip
  `generic` silently (current proposal) or emit a single
  Markdown bundle alongside the three plugins? V1 skips silently.
- **User-scope vs project-scope default.** Mirrors RFC 0015's
  open question. Project scope keeps the bundle reproducible
  across machines and survives `git clean`-vs-`.striatum/`
  cleanup; user scope means one install across many target repos.
  V1 defaults to project; user scope is `--scope user`.

## Domain Modeling

The plugin bundle is a **value object** projected from the
existing Plugin Profile aggregate root introduced by RFC 0010 and
RFC 0015. It has no identity of its own; two bundles produced by
the same Striatum version, profile, and namespace are
interchangeable. The aggregate root remains the runner-installed
template set; the manifest is the bundle's projection key. See
[`docs/DDD.md` § "Adding to the model"](../reference/domain-driven-design.md#adding-to-the-model).

The closed set of supported plugin profiles
(`claude_code`, `codex`, `gemini`) is a value object; promoting a
new profile is a workflow decision, not a runtime decision.

## Relationship To Other RFCs

- **RFC 0015 — self-contained agent skills.** This RFC layers
  packaging on top. Skill bodies are reused; the new code path
  produces a plugin manifest, slash commands, and a hooks stub
  alongside them. RFC 0015's `skills install`, manifest contract,
  edit-detection, and self-contained guarantees are inherited
  verbatim.
- **RFC 0010 — tool harness profiles.** Profile fields decide
  which agent-CLI idioms appear inside the plugin (slash command
  format, hook event names, MCP entry shape). Promoting `gemini`
  here closes RFC 0015 V1's open item on Gemini parity.
- **RFC 0012 — local service API.** Independent of this RFC. A
  future Striatum-as-MCP-server (deferred under RFC 0010 V2)
  would populate the `mcpServers` entry that V1 ships empty.
- **D006 / D009, superseded for current substrate/interface behavior by
  D094 / D104** — daemon-owned PostgreSQL is the live state, and agents
  update state through approved daemon clients. Plugin slash commands wrap
  the CLI; they do not touch live state directly. The bundle's hooks stub,
  when an operator opts in, also calls the CLI.
- **D020** — no hosted services. Plugin bundles ship inside the
  installed Python distribution. Generation is offline. The
  marketplace fixture is `local`-source by construction; no git
  remote, no hosted endpoint.
- **D028** — no transcripts. Plugin slash commands and hooks do
  not capture or emit model output as authoritative state.

## Implementation Path

V1 ships in three landable steps. Each step has its own acceptance
test set; RFC 0025 is "accepted" once steps 1 and 2 land, with
step 3 promoting additional profiles without re-opening the RFC.

1. **Generator core + Claude Code plugin profile.** Add
   `src/striatum/plugins/` with templates and a renderer that
   wraps `src/striatum/skills/templates/claude_code/`. Add the
   `striatum plugin install` verb. Ship the Claude Code bundle
   plus marketplace fixture. Add `tests/test_plugin_install.py`.
   Smallest tractable PR; gives operators a working bundle on
   the most-exercised CLI.
2. **Codex plugin profile + bootstrap convenience.** Add the
   Codex `.codex-plugin/` profile, the `--with-plugins` flag on
   `striatum init`, and the `plugin_missing` / `plugin_outdated`
   doctor checks.
3. **Gemini extension profile.** Add the Gemini `gemini-extension.json`
   profile with split-out skill content (was generic in
   RFC 0015 V1). Promote `gemini` to first-class across docs.
