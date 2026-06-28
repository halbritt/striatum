---
type: record
status: working
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: feature-design-bootstrap
author: operator-codex-gpt-5-002
created_at: 2026-06-28
updated_at: 2026-06-28
---

# MULTI_CAMPAIGN_SUPERVISION Repository Recon

## Snapshot

- Repository: `/home/halbritt/git/striatum`
- Branch at recon: `main`
- Head at recon: `a65d2db96a48da3c3fa036f599318c7e082769ae`
- Working tree before artifact edits: clean
- Version reported by bootstrap: `2.39.0`
- Daemon status at bootstrap: reachable and authorized
- Doctor status at bootstrap: ok, zero problems, zero stale leases, zero
  human-checkpoints, zero open blockers
- Current operator brief: `docs/operator/BRIEF.md`

The operator bootstrap was run before this recon, as required by
`AGENTS.md:11`.

## Repo Identity

Fact: Striatum is a Go-only local workflow runner for terminal-based AI coding
agents. Its root instructions identify it as standalone, local-first, and
generic orchestration for target repositories (`AGENTS.md:3`, `README.md:3`,
`AGENTS.md:106`).

Implication for design: multi-campaign supervision must be generic Striatum
orchestration, not a bespoke process for one downstream repository.

## Project Purpose

Fact: Striatum coordinates terminal-based AI agents through structured
multi-lane workflows with leases, verdicts, audit chains, and durable repo
artifacts, without hosted service, telemetry, or vendor SDK dependence
(`README.md:7`).

Implication for design: a multi-campaign design should preserve local-first
coordination and model-provider portability.

## Architecture

Fact: the runtime has three primary binaries: `striatumd`, `striatum`, and
`striatum-supervisor-helper` (`ARCHITECTURE.md:16`). `striatumd` is the single
writer and owns PostgreSQL; the CLI is a thin daemon client; the supervisor
helper bridges daemon-owned lanes to terminal agents (`ARCHITECTURE.md:20`,
`ARCHITECTURE.md:21`, `ARCHITECTURE.md:22`).

Fact: source packages include RPC/MCP, mutations/reads, DB/migrations,
supervisor/agent loop, workflow authoring/generation/templates, recovery, web
service/SSE, and adapter conformance (`ARCHITECTURE.md:24`).

Implication for design: possible attachment points exist in daemon read/write
surfaces, workflow generation, recovery/status projection, and web UI, but this
recon does not select among them.

## Workflows

Fact: every Striatum run starts from an explicit workflow; there is no default
workflow (`README.md:96`). The RFC roadmap defines a Design -> Build -> Verify
ship path for each roadmap item (`docs/operator/rfc-roadmap.md:14`).

Fact: the roadmap protocol starts with `striatum operator bootstrap --markdown`,
finds the lowest unshipped unblocked item, then runs Design, Build, and Verify
for it (`docs/operator/rfc-roadmap.md:45`).

Fact: workflow types include `divergent_ideation`, which widens a design space
before narrowing it and uses fresh-session isolated branches
(`docs/reference/workflow-types.md:489`, `docs/reference/workflow-types.md:498`).
They also include `falsification_gate`, which challenges a published proposal
and gates downstream work on a collaboration ledger
(`docs/reference/workflow-types.md:456`).

Implication for design: the human's desired arc-level coordination naturally
intersects the roadmap's Design -> Build -> Verify protocol, but the design
must decide how that protocol becomes a supervised portfolio arc.

## Extension Points

Fact: method authority is declared in a single registry and documented in the
command authority matrix (`ARCHITECTURE.md:61`). New RPC methods or handwritten
route maps must update the matrix and authority guardrail tests
(`AGENTS.md:116`).

Fact: workflow generation uses named shapes and templates; `divergent_ideation`
and `falsification_gate` are supported catalog shapes
(`docs/reference/workflow-catalog.md:105`, `docs/reference/workflow-catalog.md:147`).

Fact: the local web UI is a daemon-served operator browser surface
(`ARCHITECTURE.md:52`, `ARCHITECTURE.md:58`). The ubiquitous language also
defines a global dashboard read view that groups registered repositories and
reports runs, blockers, claimable jobs, stale leases, and degraded repositories
(`docs/reference/ubiquitous-language.md:231`).

Implication for design: candidate designs can investigate workflow generation,
daemon read projections, local web UI, and dashboard/status surfaces without
assuming a new authority layer.

## Entry Points

Fact: normal AI-operator cold start begins with
`striatum operator bootstrap --markdown` (`AGENTS.md:11`). Agents inside a
workflow drive work through daemon MCP/RPC state transitions, not printed
phrases or scraped terminal output (`AGENTS.md:62`).

Fact: the agent loop is `work.await_packet`, `work.ack`, optional heartbeat,
artifact publication, and `work.complete` or review submission
(`docs/how-to/how-to-agent.md:78`).

Implication for design: the meta-agent's routine entry point should probably be
daemon-backed and bootstrap-aware, but Level 1 must decide whether it is a CLI
command, UI action, workflow role, ticket action, or composition.

## Persistence

Fact: authoritative live state is daemon-owned PostgreSQL under a
`repository_id` scope; `.striatum/` is operational scratch; repository
artifacts are durable provenance only (`docs/reference/spec.md:31`,
`docs/reference/spec.md:38`, `ARCHITECTURE.md:34`, `ARCHITECTURE.md:40`,
`ARCHITECTURE.md:43`).

Fact: the daemon schema contains repositories, workflow snapshots, runs,
sessions, jobs, dependencies, queue messages, leases, work packets, artifacts,
and verdicts (`docs/reference/spec.md:78`).

Fact: durable generated operator/run-shaped records are being moved toward
daemon-indexed blob storage with record dockets and migration proof terms in the
ubiquitous language (`docs/reference/ubiquitous-language.md:36`,
`docs/reference/ubiquitous-language.md:37`, `docs/reference/ubiquitous-language.md:38`).

Implication for design: ticket/history/provenance choices must distinguish live
coordination state from reviewable durable records.

## Configuration

Fact: runtime discovery uses daemon sockets, client tokens, MCP endpoint files,
web UI sockets, and daemon config under local runtime/config paths
(`ARCHITECTURE.md:47`). Capability tokens gate every method
(`ARCHITECTURE.md:66`).

Implication for design: any autonomous meta-agent action must be scoped by
capabilities, repository identity, and session/arc authority; local ticketing or
UI features cannot bypass token boundaries.

## Testing And Build System

Fact: development uses Makefile targets `make install`, `make lint`,
`make typecheck`, `make test`, and `make smoke` (`AGENTS.md:96`). The root
Makefile delegates to `go/` (`AGENTS.md:106`).

Implication for design: Level 1 may define a later test strategy, but this
Level-0 recon does not run builds or tests.

## Documentation Conventions

Fact: new durable Markdown artifacts use lowercase privacy-safe bylines
(`AGENTS.md:125`). State docs must move when state changes; `make check-docs`
guards broken local doc links (`AGENTS.md:139`, `AGENTS.md:145`).

Fact: the ubiquitous language defines artifact, durable provenance, operator,
human principal, workflow, workflow shape, work packet, and related terms
(`docs/reference/ubiquitous-language.md:33`, `docs/reference/ubiquitous-language.md:35`,
`docs/reference/ubiquitous-language.md:64`, `docs/reference/ubiquitous-language.md:65`,
`docs/reference/ubiquitous-language.md:70`, `docs/reference/ubiquitous-language.md:73`,
`docs/reference/ubiquitous-language.md:118`).

Implication for design: the design should reuse existing vocabulary where it is
accurate and add or revise terms only after a product decision.

## Integration Boundaries

Fact: GitHub is an issue tracker only for landing work; source reaches `main`
through daemon run-integration or direct sync-guarded operator commits, not pull
requests (`AGENTS.md:131`).

Fact: hosted services, external persistence, telemetry, Slack/remote serving,
durable transcript capture, provider SDK integration, and automatic commits are
outside the current product boundary (`docs/reference/spec.md:12`).

Fact: cross-repository workflow mutation is retired by D270; remaining value is
historical schema and MCP provenance (`docs/reference/spec.md:21`).

Implication for design: GitHub issues may be considered as a ticketing medium,
but not as merge authority or live workflow truth. Local-first ticketing needs
serious evaluation.

## Architectural Constraints

- Fact: The daemon is the single writer for live state (`ARCHITECTURE.md:20`,
  `docs/reference/spec.md:31`).
- Fact: `.striatum/` is scratch and must not be committed (`ARCHITECTURE.md:40`,
  `AGENTS.md:127`).
- Fact: recovery should route through daemon recovery verbs; hand-finishing
  stranded lane work is forbidden (`AGENTS.md:149`).
- Fact: the shared checkout must stay clean and current (`AGENTS.md:166`).
- Fact: unbounded autonomous workflow cycles are out of scope for v1
  (`docs/reference/ubiquitous-language.md:92`).

Implication for design: a multi-campaign supervisor must be stop-condition
driven, provenance-backed, and bounded. It cannot be a hidden infinite agent loop
or an unofficial state machine.

## Recon Hypotheses

- Hypothesis: the strongest early design pressure is ticket/arc state, not
  implementation scheduling. This rests on the human's Stage 1 emphasis on UI
  introspection, handoffs, fresh context, and deferrals.
- Hypothesis: a local-first ticketing surface may need daemon-backed read/write
  state plus durable docket-style provenance. This rests on the tension between
  live coordination and reviewable artifacts, but it is not selected here.
- Hypothesis: a "campaign" may map to an RFC arc plus discovered slices rather
  than a single run. The repo has runs and workflow shapes, but no confirmed
  durable campaign object in this recon.

## Open Questions

- Does a durable "campaign" concept already exist in source code beyond planning
  language, or must Level 1 introduce one?
- Which current daemon methods already support the needed pause, quarantine,
  resume, scaffold, ticket-update, or checkpoint actions?
- Which local UI/web routes can show a ticket board or portfolio dashboard
  without new mutation authority?
- What is the minimal local-first ticket substrate that can carry handoff bodies
  and deferral justifications?
- Can the Design -> Build -> Verify roadmap protocol be scaffolded as a single
  accepted arc without creating an unbounded autonomous loop?
