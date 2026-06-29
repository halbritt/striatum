# Gas Town vs. Striatum

A side-by-side read of [`gastownhall/gastown`](https://github.com/gastownhall/gastown)
against this repository. Sourced from gastown's `README.md`, `AGENTS.md`,
`docs/overview.md`, `docs/HOOKS.md`, and
`docs/concepts/{polecat-lifecycle,convoy,propulsion-principle,identity}.md`.
Specific quoted phrases are paraphrases via summarization and should be
re-checked before being relied on.

## What Gas Town is

Gas Town is a Go (~95%) multi-agent orchestration system for Claude Code,
GitHub Copilot, Codex, and Gemini, pitched as "multi-agent orchestration
... with persistent work tracking through git-backed hooks" and targeting
20–30 concurrent agents. The repo is large: `cmd/gt`,
`cmd/gt-proxy-server`, `cmd/gt-proxy-client`, plus ~80 packages under
`internal/` (`mayor`, `polecat`, `convoy`, `witness`, `deacon`, `refinery`,
`beads`, `doltserver`, `tmux`, `web`, `telemetry`, …).

Mental model — a steam-engine town:

- **Mayor** — singleton AI coordinator the human talks to.
- **Rigs** — git-repo containers.
- **Polecats** — worker agents with persistent identity, ephemeral session.
- **Hooks** — two senses: (a) a polecat's git-worktree workspace where
  assigned work "lands," and (b) Claude Code-style lifecycle JSON injected
  per-agent.
- **Convoys** — cross-rig batches grouping related issues.
- **Beads** — git-backed issue ledger (separate `bd` CLI).
- **Witness / Deacon / Dogs** — three-tier watchdog (per-rig, cross-rig,
  infra workers).
- **Refinery** — Bors-style merge queue.
- **Wasteland** — federation of multiple Gas Towns over **DoltHub**.
- **Propulsion principle**: "If it's on your hook, YOU RUN IT" — agents
  auto-execute on assignment.

## Where they overlap

Both projects solve roughly the same shape of problem: coordinating multiple
terminal-AI agents on real repos with state that survives session death.

| Concern                          | Gas Town                                                 | Striatum                                                                                  |
| -------------------------------- | -------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| Agent identity ≠ session         | Polecat identity persists; tmux/worktree ephemeral       | `agent identity` vs `agent session`; "first-class session" with stable role/lane          |
| Per-job filesystem isolation     | Polecat sandbox = git worktree                           | RFC 0008 `worktree-isolated job`, `striatum worktree create`                              |
| Long-lived agent process         | Polecat session reused on `gt sling`                     | RFC 0009 supervisor + `supervise send`, FIFO stdin pipe                                   |
| Autonomous claim-and-run         | "Propulsion principle"                                   | `claim-next` → `ack` → `complete`                                                         |
| Lease/recovery for stalled work  | Witness detects `Stalled` / `Zombie` polecats            | Lazy lease expiry + `recovery requeue-stale` (refuses repo-write)                         |
| Universal attribution            | `BD_ACTOR=gastown/polecats/toast`; "Agents execute. Humans own." | Privacy-safe byline `author: <role>-<model>-<ordinal>`; D028 redaction discipline   |
| Multi-CLI agent support          | Claude Code, Copilot, Codex, Gemini                      | Same target set, abstracted via "lane" + `harness_profile` (`generic / codex / claude_code / gemini_cli`) |

## Where they diverge sharply

### 1. Scope and product boundary

- **Gas Town** is a **multi-rig, multi-agent town** with an OS-level
  coordinator (Mayor), federation across machines (Wasteland/DoltHub),
  proxy server, browser dashboard, and 20–30 agents as the design target.
- **Striatum** is **target-repository-scoped, local-first, and daemon-backed**.
  [`AGENTS.md`](../../../../AGENTS.md) is explicit: "Do not introduce hosted services,
  cloud APIs, telemetry, transcript capture, or external persistence
  without an explicit product decision."
  [`docs/reference/ubiquitous-language.md`](../../../reference/ubiquitous-language.md) defines
  the unit as a registered target repository under a daemon `repository_id`.

### 2. State store

- **Gas Town**: git-backed hooks + Beads (Dolt-backed) + git worktrees.
  State is distributed across repo artifacts, Dolt rows, and worktree
  filesystems. Federation requires DoltHub.
- **Striatum**: daemon-owned PostgreSQL under a per-repository
  `repository_id`. Repository files are durable provenance, never the
  live message bus; `.striatum/` is operational scratch only
  ([`docs/reference/ubiquitous-language.md`](../../../reference/ubiquitous-language.md)).

### 3. Workflow shape

- **Gas Town** routes work via Beads + Convoys + Molecules (workflow
  templates) with ad-hoc Mayor orchestration: human → Mayor → convoy of
  beads → polecats execute.
- **Striatum** runs a declarative workflow JSON (`striatum.workflow.v1`)
  with a frozen snapshot per run, explicit jobs/dependencies/gates/
  write-scopes, and a deterministic coordinator. Branch confirmation is
  records-only. Cycles must be explicitly bounded.

### 4. Review and verdicts

- **Gas Town**: ad-hoc through the Bors-style Refinery merge queue.
- **Striatum**: structured `verdict` enum
  (`accept` / `accept_with_findings` / `needs_revision` / `reject`),
  `submit-review`, named review gates, `reviewer_access_scope` and
  `reviewer_context_policy` (RFC 0002), action-item ledgers (RFC 0004),
  evidence audits / support ledgers (RFC 0003).

### 5. Coordinator role

- **Gas Town**: the Mayor is an AI — a Claude instance. The orchestrator
  itself is a model.
- **Striatum**: explicitly splits the **deterministic coordinator** (owns
  state, gates, scope checks; never an LLM) from an optional **AI
  coordinator** lane (chats with the human, invokes commands, never
  silently mutates state).

### 6. Communication model

- **Gas Town**: `gt nudge` (live IPC), `gt mail send` (durable mailbox),
  `gt prime` (re-hydrate context). Standard stdout is invisible to other
  agents — they must use `nudge`.
- **Striatum**: daemon/Postgres queue messages + work packets delivered
  through CLI/MCP/supervisor surfaces, including stdin pipes for supervised
  agents. No agent-to-agent prose channel; communication is structured
  (artifacts, blockers, verdicts).

### 7. Privacy / transcripts

- **Gas Town**: OpenTelemetry, dashboards, activity feeds, Seance for
  "querying previous agent sessions." Transcripts are first-class.
- **Striatum**: D028 — supervised stdout/stderr go to `/dev/null`; no
  transcript capture; redacted evidence exports only. Curated artifacts
  are preferred over raw logs.

### 8. Artifact discipline

- **Gas Town**: artifacts are mostly git commits + bead records;
  attribution via `BD_ACTOR` and email author.
- **Striatum**: registered front-matter schemas (`striatum.decision.v1`,
  `striatum.finding.v1`, `striatum.findings_ledger.v1`,
  `striatum.synthesis.v1`, `striatum.support_ledger.v1`,
  `striatum.action_item_ledger.v1`,
  `striatum.harness_improvement_proposal.v1`); the publisher validates
  but never rewrites artifact files.

## One-line summary

Gas Town is a **multi-rig agent town with an AI mayor, distributed
git-backed state, federation, and a "go fast — propel yourself"
philosophy**. Striatum is a **local deterministic daemon/Postgres
coordinator with declarative workflows, structured verdicts, redaction
discipline, and an explicit anti-hosted product boundary**. They
share workshop primitives (worktree isolation, persistent identity /
ephemeral session, supervised long-lived processes, lease recovery) but
disagree on almost every axis above those primitives — Gas Town
optimizes for fleet scale and AI-driven coordination; Striatum optimizes
for one repo's auditable, deterministic provenance.

## Worth borrowing — or deliberately rejecting

**Borrow-shaped**

- The "propulsion principle" is a clean way to describe the
  supervisor-delivered-packet contract striatum already implements.
- Gas Town's split of **identity / sandbox / session** maps cleanly onto
  striatum's three concepts and might tighten the docs.

**Deliberately reject** (consistent with [`AGENTS.md`](../../../../AGENTS.md))

- Mayor (AI-as-coordinator) — striatum keeps the coordinator
  deterministic.
- DoltHub federation — out of product scope.
- Browser dashboard as the primary surface — TUI is the V1 dashboard.
- Transcript capture / Seance — D028 forbids it.
- `gt nudge`-style agent-to-agent prose channels — striatum's
  communication is structured, not free-text.
