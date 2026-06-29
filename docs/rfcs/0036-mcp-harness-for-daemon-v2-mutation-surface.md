# RFC 0036: MCP Harness for Daemon V2 Mutation Surface

Status: accepted (V1 implemented)
Date: 2026-05-12
Context:
[`RFC 0015`](0015-self-contained-agent-skills.md),
[`RFC 0025`](0025-agent-cli-plugin-bundles.md),
[`RFC 0030`](0030-daemon-rpc-server-and-version-skew-protocol.md),
[`RFC 0031`](0031-daemon-owned-supervision-and-sealed-apply-boundary.md),
[`RFC 0032`](0032-cross-repo-workflows-and-mcp-mutation-capabilities.md),
[`RFC 0034`](0034-workflow-generator-and-template-catalog.md) §10,
[`docs/DECISION_LOG.md`](../decisions/decision-log.md) (D061, D063, D080, D087, D088, D090),
[`docs/MCP.md`](../reference/mcp.md),
`src/striatum/skills/install.py`,
`src/striatum/skills/templates/claude_code/`,
`src/striatum/mcp.py`,
`src/striatum/daemon_rpc/registry.py`

## Problem

RFC 0032 V2 (shipped v1.24.0) wired MCP `tools/call` to the RFC 0030
method registry with capability-gated default-deny, per-token
`tools/list` filtering, and audit-row append for every mutating call
(allowed or denied) with the documented `capability_missing` /
`token_revoked` / `token_expired` / `method_unknown` denial vocabulary.
RFC 0034 V1 (shipped v1.25.0) added a non-mutating
`POST /workflows/generate/preview` endpoint that AI/operator-surrogate
clients can call freely, plus a mutation-gated `POST /workflows/generate`
behind `--allow-mutations` requiring `confirm_write: true`.

**The agent-facing harness has not caught up.**

The bundled skills installed by `striatum skills install` (per RFC 0015
and RFC 0025) — `striatum-claim-loop`, `striatum-workflow`,
`striatum-scaffold`, `striatum-supervise`, `striatum-recover` — only
teach CLI verbs. An AI client holding a capability token has no skill
that explains:

- which tools the daemon-mediated MCP surface actually exposes;
- how `tools/list` filtering works against the token's capability set;
- the preview-then-write idiom (`tools/call` with the preview verb
  before the mutation verb);
- the `confirm_write: true` requirement on write verbs;
- the `capability_missing` / `token_revoked` / `token_expired` /
  `method_unknown` denial vocabulary and how to recover from each;
- the capability scope semantics (`repo_id`-scoped vs daemon-global;
  a write-token scoped to repo A cannot call write-paths against repo
  B);
- the short-lived-token recommendation for mutation;
- the audit chain guarantee on every mutating call including denials.

Worse, RFC 0034 §10 explicitly deferred the chat-assisted scaffolding
tool to a follow-up. That tool is the AI-callable workflow generator
that consumes `POST /workflows/generate/preview` and (with explicit
operator confirmation) `POST /workflows/generate`. Without it, the
local API endpoints have no in-product caller; operators who want an
AI to help draft a workflow have to either copy the spec by hand or
write their own MCP integration.

The result is a gap: the daemon mutation surface ships, the audit
chain ships, the workflow generator API ships, but the agent-side
shape that uses any of it doesn't exist. Operators learn the CLI verbs
through `striatum skills install`; AI clients with capability tokens
have no equivalent.

## Goals

- Ship a `striatum-mcp` skill that teaches AI agents how to use the
  daemon-mediated MCP mutation surface safely, including the preview-
  then-write idiom, capability/token lifecycle, denial-vocabulary
  recovery, capability scope semantics, and audit-chain expectations.
- Ship the RFC 0034 §10 chat-assisted scaffolding tool (`generate_
  workflow_preview` + `generate_workflow_write`) as a closed-set chat
  tool over the existing RFC 0023 chat surface, with operator
  confirmation enforced before any write.
- Land both behind the existing RFC 0015 / RFC 0025 skill + plugin
  install paths so adoption is "run `striatum skills install`" not
  "edit an agent config".
- Keep skill bodies provider-neutral; the install plan still fans out
  per RFC 0015 profile coverage (`claude_code`, `codex`, `gemini`,
  `generic`).
- Preserve local-first boundary: no hosted skill registry, no
  telemetry, no remote chat tool fetch.

## Non-Goals

- A new MCP server. Striatum already exposes a daemon-mediated MCP
  surface per RFC 0028 (resources-only) and RFC 0032 (capability-gated
  `tools/call` + `tools/list`); this RFC adds the agent-facing harness
  on top.
- A new capability vocabulary. The seven existing capabilities
  (`read`/`write`/`review`/`claim`/`apply`/`admin`/`recovery`) from
  RFC 0030 are stable; the skill teaches them, doesn't redefine them.
- Auto-issuance of capability tokens. Operators issue tokens
  explicitly (admin-only) per RFC 0030/0031; the skill teaches the
  recommended posture (`daemon.token.create --capability write
  --expires-in 1h --repo <id>`) but does not generate tokens itself.
- Web UI chat changes. The chat surface from RFC 0023 V1.5 already
  hosts the read-only tools (`read_file`, `list_dir`,
  `striatum_status`, `striatum_why`, `git_log`, `git_diff`); this RFC
  adds two new closed-set tools without redesigning the chat
  lifecycle.
- Cross-machine / hosted-mode harness. Per D083, daemon V2 is single-
  user single-machine.

## External Prior Art

- **VS Code GitHub Copilot tools surface**: small closed set of tools
  exposed to the chat model, each with explicit input schema and a
  preview-before-act policy on side-effecting verbs. The useful idea
  is "tool schemas as the contract; preview-then-write as the safety
  posture". Striatum's equivalent is the GeneratedWorkflow envelope
  returned by preview, with operator confirmation gating the write.
- **MCP "Tools" pattern (Anthropic)**: capability-token authorization,
  default-deny on missing capabilities, per-call audit, structured
  errors with field paths. RFC 0032 already implements this; this RFC
  teaches the agent how to call it.
- **Existing Striatum chat tools (RFC 0023 V1.5)**: closed-set,
  read-only, system-prompt briefing. The useful idea is "closed set
  of tools the chat session sees, briefed at session start". This RFC
  adds two new tools to that closed set behind a new mutation gate.

## Proposal

### 1. New `striatum-mcp` skill

Add a fifth bundled skill (sixth counting the future
`striatum-chat-generate` below):

```
src/striatum/skills/templates/claude_code/mcp.md.tmpl
src/striatum/skills/templates/generic/mcp.md.tmpl
```

Register in `CLAUDE_CODE_SKILLS` so `striatum skills install --profile
claude_code` writes `.claude/skills/<ns>striatum-mcp/`. The generic
profile concatenation includes the same body. Gemini's single-file
guide (`GEMINI.md` derivative) appends a section.

Skill body covers:

#### When to invoke

- The agent has a capability token and wants to mutate workflow state
  through MCP rather than the CLI.
- The agent wants to generate a workflow through the local API rather
  than ask the operator to run the CLI.
- The agent saw a `capability_missing` / `token_revoked` /
  `token_expired` denial and wants to recover.

#### Authoritative reference

- `daemon.hello` / `daemon.welcome` for version handshake.
- `daemon.describe` to list available methods with their required
  capabilities.
- `tools/list` returns the token's effective supported production tool set
  (method registry ∩ token capabilities ∩ repository scope ∩ production
  visibility filter). The agent reads this first to know what it should call.
- `tools/call` invokes a method. Mutating calls must pass `confirm_
  write: true` where the method declares it (e.g., the generator
  write endpoint).
- Audit rows are appended for every mutating call including denials.
  The agent cannot suppress them; the operator inspects them via
  `daemon audit show` and the daemon DB.

#### Common patterns

```text
# 1. Effective tool set
tools/list -> {tools: [...]}

# 2. Preview before write
tools/call name=workflow.generate.preview args={spec}
  -> {workflow: {...}, files: [...], metadata: {...}, validation: {ok}}

# 3. Write with operator confirmation
tools/call name=workflow.generate args={spec, confirm_write: true}
  -> {written: [...], validation: {ok}}
```

#### Capability scope

- A `write`-capability token scoped to repo A cannot call write-paths
  against repo B. The daemon refuses with `capability_missing` and
  appends an audit row recording the attempted scope mismatch. The
  recovery is to ask the operator for a token scoped to repo B, not
  to retry with the wrong token.
- A token with only `read` does not see `write` tools in `tools/list`.
  This is filtering, not refusal: the tool simply isn't visible.

#### Denial recovery

- `capability_missing`: the token lacks the required capability for
  the called method. Ask the operator to issue a token with the
  required capability (named in the audit row's `denial_reason`).
- `token_revoked`: the token was revoked by the operator. Stop
  retrying; the operator made an explicit decision.
- `token_expired`: short-lived tokens expire by design. Ask the
  operator to issue a fresh one with the same scope.
- `method_unknown`: typo or version skew. Call `daemon.describe` to
  discover the correct method name.

#### What not to do

- Don't escalate by claiming a different identity. The daemon never
  bypasses capability gating regardless of client identity claims.
- Don't try to write directly to `.striatum/retired-local-state`. The
  daemon never exposes a way for a non-admin MCP token to write
  repo-local state directly. Mutations flow through capability-gated
  RPC.
- Don't request a wildcard capability ("give me admin"). The
  operator-mistake footgun list per RFC 0031 §Threat Model includes
  wildcard grants; the right posture is short-lived tokens with the
  narrowest capability that fits the task.
- Don't loop on `token_revoked`. The audit chain records the loop;
  the operator sees it.

### 2. New chat tools: `generate_workflow_preview` and `generate_workflow_write`

Extend the RFC 0023 V1.5 chat-tools closed set by two:

```text
generate_workflow_preview(spec: WorkflowGenerationSpec) -> GeneratedWorkflow
generate_workflow_write(spec: WorkflowGenerationSpec, confirm_write: bool) -> {written: [paths], validation: {ok}}
```

`generate_workflow_preview` is safe to call freely. It hits
`POST /workflows/generate/preview` and returns the full
`GeneratedWorkflow` envelope. The chat client displays the generated
graph and file list inline.

`generate_workflow_write` requires `confirm_write: true` AND an
operator confirmation gesture in the chat UI (button press or
explicit "yes, write it" message routed through the chat lifecycle's
mutation gate from RFC 0013 step 7). The chat model cannot bypass
the operator confirmation; the UI enforces it before issuing the
HTTP call. The endpoint already refuses missing `confirm_write` per
RFC 0034.

Update the existing chat-session system-prompt briefing (RFC 0023
V1.5) to mention the two new tools and the preview-then-write idiom.

Both tools live behind the existing `--allow-mutations` flag on
`striatum serve`. Chat sessions started against a non-mutation-allowed
service hide the write tool from `tools/list` and surface a useful
error if the model still tries to call it.

### 3. Skill install fan-out

Update `src/striatum/skills/install.py`:

```python
CLAUDE_CODE_SKILLS: tuple[str, ...] = (
    "workflow",
    "scaffold",
    "claim-loop",
    "supervise",
    "recover",
    "mcp",
)
```

Codex fan-out (already reuses claude_code templates) picks up the new
skill automatically. The generic profile concatenates the new body
into `STRIATUM_AGENT_GUIDE.md`. The Gemini single-file guide appends
the new section. Plugin bundles (RFC 0025) regenerate to include the
new skill body via `striatum plugin install`.

### 4. CLI surface additions

No new CLI verbs in V1 — the skill teaches existing daemon RPC verbs
and the chat tools use existing service endpoints. Future RFCs may
add `daemon mcp doctor` to surface the agent's effective tool set
from the operator's perspective, but that's deferred.

### 5. Local service surface additions

No new endpoints. The chat tools call the existing RFC 0034 V1
endpoints (`POST /workflows/generate/preview`, `POST /workflows/
generate`). The chat lifecycle from RFC 0023 already handles tool
dispatch and the mutation confirmation gate from RFC 0013 step 7.

### 6. Documentation

- `docs/MCP.md` — new section "Mutation Surface for Agents" covering
  the preview-then-write idiom, denial vocabulary, capability scope
  semantics, and short-lived-token recommendation.
- `docs/HOW_TO_AGENT.md` — note the new `striatum-mcp` skill in the
  skill list and what it teaches.
- `docs/HOW_TO_HUMAN.md` — operator-side: how to issue + revoke
  capability tokens for an agent that will use the new chat tools.
- `docs/UBIQUITOUS_LANGUAGE.md` — clarify "MCP mutation surface" and
  "effective tool set" entries.
- `docs/CLI_REFERENCE.md` — no new CLI verbs in V1, but cross-
  reference the skill body for agents that want to use MCP.
- RFC 0034 status update — §10 chat-assisted scaffolding tool moves
  from "deferred" to "implemented in RFC 0036".

## Acceptance Criteria

- `src/striatum/skills/templates/claude_code/mcp.md.tmpl` and
  `src/striatum/skills/templates/generic/mcp.md.tmpl` exist with
  body covering invoke triggers, authoritative reference, common
  patterns, capability scope, denial recovery, and what-not-to-do.
- `CLAUDE_CODE_SKILLS` tuple in `src/striatum/skills/install.py`
  includes `"mcp"`.
- `striatum skills install --profile claude_code` writes
  `.claude/skills/<ns>striatum-mcp/` with the rendered body.
- `striatum skills install --profile codex` writes
  `.codex/agents/<ns>mcp.md`.
- `striatum skills install --profile gemini` appends the mcp section
  to the gemini guide.
- `striatum skills install --profile all` covers all three.
- `striatum plugin install` regenerates plugin bundles with the new
  skill body included.
- The chat-tools closed set in `src/striatum/web/chat/` (or wherever
  the RFC 0023 V1.5 tools live) includes `generate_workflow_preview`
  and `generate_workflow_write`.
- The chat-session system-prompt briefing mentions the two new tools
  and the preview-then-write idiom.
- `generate_workflow_write` enforces operator confirmation in the
  chat UI; the chat model cannot bypass it.
- Both tools respect `--allow-mutations`: hidden from `tools/list`
  when not allowed; clear error if called anyway.
- Unit tests cover: skill body renders deterministically; chat tools
  call the right endpoints; `confirm_write` gating works; mutation-
  not-allowed path refuses cleanly.
- Doc-links pass.
- `docs/MCP.md`, `docs/HOW_TO_AGENT.md`, `docs/HOW_TO_HUMAN.md`,
  `docs/UBIQUITOUS_LANGUAGE.md`, RFC 0034 status updated.

## Implementation Plan

### Step 1. `striatum-mcp` skill body + install plan

Author the claude_code + generic templates; wire into
`CLAUDE_CODE_SKILLS`. Add unit test asserting the install plan
emits the new skill at all three target paths.

### Step 2. Chat tool wiring

Add `generate_workflow_preview` and `generate_workflow_write` to the
RFC 0023 V1.5 closed-set chat tools. Update system-prompt briefing.
Wire the existing operator-confirmation gate. Unit tests for each
endpoint call shape + the confirmation gate.

### Step 3. Mutation-not-allowed path

Verify both chat tools cleanly hide / refuse when `serve` is started
without `--allow-mutations`. Test the `tools/list` filter and the
fallback refusal.

### Step 4. Documentation updates

Update `docs/MCP.md`, `docs/HOW_TO_AGENT.md`, `docs/HOW_TO_HUMAN.md`,
`docs/UBIQUITOUS_LANGUAGE.md`, `docs/CLI_REFERENCE.md` cross-
reference, RFC 0034 status block update. Doc-links pass.

### Step 5. Plugin regeneration

`striatum plugin install` for each of the three first-class agent
CLIs regenerates with the new skill body included. Test that the
existing plugin install paths pick up the new skill without manual
intervention.

## Open Questions

- Should the skill teach `daemon.describe` as the canonical method-
  discovery path, or should it lean on `tools/list` (which is
  capability-filtered)? `tools/list` is honest about what the agent
  can actually call; `daemon.describe` is broader. Recommendation:
  teach `tools/list` first; mention `daemon.describe` as the operator-
  side lookup.
- Should `generate_workflow_write` require a `confirm_write: true`
  argument from the model AND a chat-UI confirmation gesture, or is
  the UI gesture sufficient? Belt-and-suspenders argues for both;
  ergonomics argues for one. Recommendation: keep both — the
  argument makes the model's intent explicit in the audit row;
  the UI gesture makes the operator's intent explicit in the audit
  chain.
- Should the skill name be `striatum-mcp` or something more specific
  like `striatum-daemon-tools` or `striatum-mutation-surface`?
  Recommendation: `striatum-mcp` — short, parallel to the existing
  `striatum-supervise` / `striatum-recover` names, and `mcp` is the
  vocabulary the agent already knows.
- Should this RFC bundle a small `examples/` workflow that
  exercises the chat-generate flow end-to-end? Recommendation: yes,
  but as a follow-up — keep this RFC scoped to skill + chat tools.

## Domain Modeling

This RFC adds harness — a skill template + two chat tools — not new
domain concepts. The MCP capability vocabulary, the audit chain, the
`GeneratedWorkflow` envelope, the chat-session lifecycle, the
mutation gate, and the operator-confirmation gesture are all existing
machinery. The skill teaches them; the chat tools call them.
