# RFC 0088: Deprecate `-p` — daemon-owned interactive PTY lanes, owned-PTY attestation, and the AGY migration

Status: accepted
Date: 2026-05-27
Author: proposer-claude-opus-4-7-001
Decisions: D148, D149, D150, D151 (accepted 2026-05-29)

> **Acceptance note (2026-05-29).** Decisions D148-D151 accepted. The
> turn-driver, `single_shot` capability, `gemini_cli`/`gemini_default` family,
> and the `--print`/`exec` supervised wrapper are deleted. Owned-PTY agent-loop
> verified end-to-end for **claude** (P1) and **codex** (P3); **agy** (P2)
> verified through MCP discovery + `work.claim` via the submit-driver +
> `.gemini/settings.json` fixes (#52). Remaining implementation follow-up before
> agy's one-shot pipe lane is also retired: agy must reliably *complete* a
> claimed packet (it currently over-builds a poll loop instead of executing),
> plus cleanup-on-kill of the token-bearing `.gemini/settings.json` — both
> tracked in #51.

## Summary

Retire the `-p` / `--print` / `exec` one-shot launch mechanism for **all**
agent lanes. Every lane becomes a daemon-owned, long-lived **interactive PTY
session** (the agent's native interactive mode + `--continue`/`resume`), driven
by submitting prompts through the PTY master the supervisor already owns. The
owned-PTY session earns first-class **lane-byline attestation** (pid +
launch-command-snapshot binding), so retiring `-p` does not regress provenance.
As the first concrete payoff, the **`gemini_cli` lane is replaced by `agy`**
(Antigravity) — gemini was the one adapter whose agent-loop never worked
natively, the reason the RFC 0082–0086 turn-driver crutch (F42–F45) exists —
and the turn-driver plus the `single_shot` adapter capability are deleted.

This completes the conditional `--print` deprecation accepted in **D140**, now
unconditionally, by making every adapter's persistent agent-loop a real
daemon-owned interactive process rather than a per-packet fresh process.

## Background: why `-p` blocks the product

- The `--print` supervised **wrapper** spawns a *fresh process per work packet*
  with **no preserved context** (D138/D140). A `--print` author can only answer
  from its already-published artifact — exactly the cold-context review that
  interrogation (RFC 0082/0083) is meant to replace.
- **D140** accepted deprecating the wrapper, but *conditionally per adapter*,
  gated on the MCP agent-loop working for that adapter. claude's agent-loop is
  proven; **gemini's never worked natively** ("unreliable await/say loop
  needing an operator driver" — F42), so F42–F45 built a generic turn-driver to
  fake an agent-loop for `single_shot` gemini.
- **D141** (RFC 0084) made agent-loop sessions *interrogable* (the
  `awaiting_interrogation` window) but explicitly did **not** grant them
  lane/model bylines — they still publish `author: operator`. So today you can
  have an attested one-shot author (no context) **or** an interrogable
  persistent session (no byline), never both.

The block is therefore structural: `-p` is the only attested authoring path,
but it is the one that cannot preserve context. Removing it requires giving the
persistent interactive session both the **driving** mechanism and the
**attestation** the wrapper had.

## Feasibility: all three adapters support a persistent session

| Adapter | Persistent interactive | Resume / continue | One-shot to retire |
| --- | --- | --- | --- |
| **agy** (Antigravity) | `-i` (`--prompt-interactive`) | `--continue` / `--conversation <id>` | `-p` / `--print` / `--prompt` |
| **claude** | default (no `-p`) | `-c` / `--continue`, `--resume` | `-p` / `--print` |
| **codex** | bare invocation | `resume --last`, `fork` | `exec` |

No adapter is structurally stuck on one-shot mode, so fleet-wide deprecation is
achievable.

## Decisions

### 1. Long-lived interactive PTY session, turns via stdin-submit

Each lane is launched once as its native interactive mode over a PTY the
daemon owns (`supervisor.Launch` with `UsePTY: true` already threads the PTY
master back as the daemon's stdin handle). Per-turn prompts are delivered by
**submitting through the PTY** — the per-adapter submit key-sequence
(Enter/`\r`, bracketed-paste as needed) that D140 Phase A identified as the
missing piece for TUI agents (claude buffered the bootstrap prompt unsubmitted).

Rejected: re-invoking `--continue`/`resume --last` per turn. It re-spawns a
fresh process each turn (pid churns, context reloads from disk, not live
memory) and still rides `exec`/`-p` per invocation — smuggling back the thing
we are deleting.

### 2. Owned-PTY sessions earn first-class lane-byline attestation

The daemon-owned long-lived PTY session has exactly the pid-start-time +
snapshot-command binding D080 attestation requires; it is the *same* mechanism
as the `--print` wrapper, just persistent rather than per-packet. Owned-PTY
sessions therefore derive the `author: <role-name>-<model-name>-<ordinal>`
byline (this is the "self-attest via owned PTY/pid" path D141 flagged for
revisit). Without this, fleet-wide `-p` removal would silently downgrade all
artifact provenance to `author: operator`.

Attestation is, and remains, an **anti-fabrication guardrail (friction), not
cryptographic non-repudiation** (D080). A long-lived session keeps its byline
as long as the owned pid identity holds and the launch command matched the
workflow snapshot; `--continue` turn-to-turn context drift does not weaken the
guarantee because we are buying friction against fabricated evidence, not proof.

### 3. Retire the turn-driver and the `single_shot` capability

`gemini` is the only lane that sets `adapter_capabilities.single_shot: true`,
and the turn-driver (`go/pkg/agentloop/turn_driver.go`, the `striatumd
-agent-loop -turn-driver` flag, F42–F45 / D145–D146) exists solely to drive it.
Once gemini is gone, both are deleted. **F45 (gemini turn-driver slowness)
becomes moot** — removal resolves it.

### 4. Replace `gemini_cli` with `agy`; `agy` is the canonical family

`agy` (Antigravity) is the canonical tool family; the duplicate `antigravity`
tool-family entry in `catalog.go` is dropped (it is the human-readable
`display_model`, not a second family). The `gemini_cli` family, the
`gemini_default` catalog profile, and the gemini installer profile are removed.
agy's invocation is claude-shaped, so its installer support **reuses the
claude bundle** (`agy plugin import claude`) rather than a parallel template
tree. agy runs whatever model Antigravity is configured for (no per-lane
`--model` flag); `display_model` records the model name for bylines.

### 5. MCP config generated fresh at launch; never persist the rotating port

For any lane that needs native MCP config, the daemon **generates it fresh at
each launch** from the live env-injected `STRIATUM_MCP_URL` / `STRIATUM_MCP_TOKEN`,
to an ephemeral per-lane path under `.striatum/` scratch, torn down after. No
lane reads a persisted port from a repo-tracked or gitignored file. This makes
the **F45 class of bug structurally impossible** (gemini's stale
`.gemini/settings.json` port was the F45 root cause). Agent-loop lanes need no
native config at all — the daemon holds the MCP client.

### 6. PTY logs are local diagnostics, not transcript provenance

Interactive lanes need operator-inspectable terminal trajectories while submit
drivers and provider TUIs are still being hardened. RFC 0088 therefore permits
daemon-owned agent-loop PTY sessions to tee their terminal stream to a
per-supervisor `0600` diagnostic file under `.striatum/scratch/` (default
`.striatum/scratch/<supervisor_id>/pty.log`, overridable or disabled with
`STRIATUM_AGENT_LOOP_DEBUG_LOG`).

This revisits D028 narrowly. The forbidden thing is still durable transcript
capture: no raw provider transcript may become daemon/PostgreSQL state, a
workflow artifact, a corpus/archive/evidence export, a verdict input, a byline
input, or a workflow-control signal. The allowed thing is private operational
scratch for the local operator. These logs may contain secrets or other
terminal-visible private text; Striatum does not redact them, publish them, or
treat them as provenance, and deleting them does not change workflow truth.

## Phasing

Mirrors D140's per-adapter conditional gate: prove each adapter's owned-PTY
interactive session before deleting the wrapper.

- **P1 — Foundation on claude (known-good baseline).** Implement the per-adapter
  PTY submit-key driver + owned-PTY persistent-session launch; extend byline
  attestation to owned-PTY sessions (Decision 2). Prove: a long-lived `claude`
  interactive session, daemon-driven, attested with a model byline,
  interrogable, with no `-p`. Retire claude's `-p` agent-loop path.
- **P2 — agy lane + gemini removal.** Add the `agy` lane (Decision 4), MCP
  fresh-at-launch (Decision 5), prove agy as a persistent interactive,
  interrogable, attested lane; swap gemini→agy in workflow templates; remove the
  `gemini_cli`/`antigravity` families, the gemini profile, and the gemini
  installer surface.
- **P3 — codex cutover + cleanup.** Prove codex persistent interactive
  (`codex` + `resume`) over PTY; retire codex `exec`. Then **delete** the
  turn-driver, the `single_shot` capability, and the `--print` supervised
  wrapper (Decision 3). Land SPEC / glossary / DECISION_LOG updates.

## Drawbacks / follow-ups

- **Per-adapter bootstrap fragility.** TUI submit semantics differ per CLI and
  can drift across CLI versions. Claude uses the PTY submit path; codex now
  receives the bootstrap as its initial prompt argument to bypass the TUI input
  editor. Remaining adapters still need per-adapter fixtures and a liveness
  check that the prompt was actually accepted.
- **Long-lived sessions need idle/heartbeat policy.** D141 already flagged that
  liveness beyond `state=active` (idle/heartbeat timeout) may be needed; more
  acute now that every lane is long-lived.
- **Resource cost.** N long-lived PTY processes per run vs ephemeral per-packet
  processes; needs a reap/limit policy.
- **agy model pinning.** No `--model` flag means the model is set by the
  Antigravity install, not the workflow; `display_model` must be kept honest.
- **Local transcript sensitivity.** PTY logs improve debuggability but may
  contain terminal-visible private text. They must stay in operational scratch
  unless a later decision accepts retention, redaction, export, or UI viewing.
- The Go web service is still not mounted in the running daemon (carried from
  RFC 0084 follow-ups); unrelated but adjacent.

## Proposed decision-log entries (on acceptance)

- **D148** — Deprecate `-p`/`--print`/`exec` for all lanes; lanes are
  daemon-owned long-lived interactive PTY sessions driven by stdin-submit
  (Decisions 1, 3). Completes D140 unconditionally.
- **D149** — Owned-PTY persistent sessions earn first-class lane-byline
  attestation via pid + command-snapshot binding; attestation remains
  anti-fabrication friction, not non-repudiation (Decision 2). Extends
  D080/D141.
- **D150** — Replace `gemini_cli` with `agy` (canonical family; antigravity
  family dropped; installer reuses the claude bundle); MCP config generated
  fresh at launch and never persisted (Decisions 4, 5). Retires F42–F45.
- **D151** — Narrow D028 for RFC 0088: operator-local PTY logs under
  `.striatum/scratch/` are allowed as private diagnostics, while raw provider
  transcripts remain forbidden as daemon state, artifacts, exports, verdict
  inputs, byline inputs, or workflow-control signals (Decision 6).

## Glossary delta (on acceptance)

Add to `docs/UBIQUITOUS_LANGUAGE.md`:

> **agy** — tool family for the Antigravity agent CLI. Claude-shaped invocation
> (`-i` interactive + `--continue`); driven as a daemon-owned interactive PTY
> lane. Replaces the retired `gemini_cli` family.

Mark `single_shot` and the turn-driver as retired where referenced.
