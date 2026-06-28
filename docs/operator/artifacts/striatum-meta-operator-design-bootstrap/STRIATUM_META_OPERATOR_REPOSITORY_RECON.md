---
type: record
status: working
feature_slug: STRIATUM_META_OPERATOR
source: feature-design-bootstrap
author: operator-codex-gpt-5-001
created_at: 2026-06-28
---

# STRIATUM_META_OPERATOR Repository Recon

## Snapshot

- Repository: `/home/halbritt/git/striatum`
- Branch at bootstrap: `main`
- Head at bootstrap: `42d9579c536ae4902ca03a3693778dcd04e9408a`
- Working tree at bootstrap: clean
- Version reported by bootstrap: `2.39.0`
- Daemon status at bootstrap: reachable and authorized
- Doctor status at bootstrap: ok, zero problems, zero stale leases, zero
  human-checkpoints, zero open blockers
- Current operator brief: `docs/operator/BRIEF.md`

The operator bootstrap was run before this recon, as required by `AGENTS.md:11-15`.

## Product Boundary

Striatum is a standalone, local-first workflow runner for terminal-based AI
coding agents. It is generic orchestration for target repositories, not an
Engram-specific process script. `docs/reference/spec.md` is the source of truth
for the product boundary. See `AGENTS.md:3-7`, `README.md:3-22`, and
`docs/reference/spec.md:10-19`.

The strongest constraint for a meta-operator is state ownership:

- Daemon-owned PostgreSQL is authoritative live state.
- Repository files are durable provenance.
- `.striatum/` is operational scratch.
- Marker files, tmux panes, terminal output, provider hooks, and scratch logs
  are not live workflow state.

Relevant sources: `README.md:59-63`, `ARCHITECTURE.md:32-50`,
`docs/reference/spec.md:31-40`, and `docs/how-to/how-to-agent.md:50-74`.

## Runtime Architecture

The Go-only runtime has three main binaries:

- `striatum`: CLI compatibility and operator-facing commands.
- `striatumd`: daemon-owned live state, RPC/MCP, web, and dashboard surfaces.
- `striatum-supervisor-helper`: local supervised agent helper.

The architecture map points to packages for app orchestration, CLI, daemon,
driver/scheduler, local supervision, protocol, storage, web, and workflow
generation. See `ARCHITECTURE.md:16-30`.

State mutation is exposed through RPC/MCP/Web/Dashboard surfaces and the method
authority registry. See `ARCHITECTURE.md:52-64`.

Capability tokens are principal-scoped and session-bound. Lane tokens are
short-lived and bound to run/job/session scope. Write scope is enforced at
claim and publication boundaries. See `ARCHITECTURE.md:66-77`.

## Workflow Model

Runs are created from explicit workflow configs. There is no default workflow.
The workflow model includes runs, jobs, leases, sessions, lanes, artifact
publication, recovery, and review/integration. See `README.md:96-99`,
`ARCHITECTURE.md:79-107`, and `docs/reference/workflow-types.md:15-34`.

Agent work packets provide exact commands, expected artifacts, write scope, and
context documents. A lane should register, await work, ack, heartbeat, publish
artifacts, and complete/review through daemon state transitions. See
`docs/how-to/how-to-agent.md:78-93` and `docs/how-to/how-to-agent.md:213-239`.

This implies a meta-operator cannot infer state from terminals or repo dirt. It
must use daemon APIs and explicitly cite repo artifacts only as provenance.

## Existing Workflow Shapes Relevant To Design

Supported or documented shapes that can participate in the next design phase:

- `divergent_ideation`: widen design before narrowing; isolated branches;
  convergence critic; deepen selected options. See
  `docs/reference/workflow-types.md:489-541` and
  `docs/reference/workflow-catalog.md:105-125`.
- `falsification_gate`: challenge a proposal and publish a collaboration
  ledger, without raw PTY/provider output. See
  `docs/reference/workflow-types.md:450-481` and
  `docs/reference/workflow-catalog.md:147-166`.
- `implementation_panel`: supported build/review panel after a design is
  accepted. See `docs/reference/workflow-catalog.md:207-235`.
- `multi_review_synthesis`: supported synthesis of several review lanes. See
  `docs/reference/workflow-types.md:339-367` and
  `docs/reference/workflow-catalog.md:315-333`.
- `iterated_interrogating_panel`: experimental/example-only; useful as a
  concept but too unstable for the default path. See
  `docs/reference/workflow-types.md:543-560` and
  `docs/reference/workflow-catalog.md:237-246`.

## Existing Product Threads Related To Meta-Operator Design

- RFC 0102, operator attention economy: frames the core pain as attention
  scarcity and fragmented surfaces. It proposes a compact control surface,
  trajectory, and run watch/drive concepts. See
  `docs/rfcs/0102-operator-attention-economy.md:8-20`,
  `:71-115`, and `:147-231`.
- RFC 0099, constrained operator mode: relevant to limiting ambient authority
  and declaring scope/capabilities/evidence before mutation. See
  `docs/rfcs/0099-constrained-operator-mode.md:31-65` and `:77-150`.
- RFC 0116, zero-operator-touch DAG: frames run driving and auto-supervision
  questions, but intentionally concerns one run driver rather than cross-run
  coordination. See `docs/rfcs/0116-zero-operator-touch-dag.md:28-55`,
  `:89-132`, and `:174-220`.
- RFC 0122, scheduler principal auto-spawn: introduces scheduler authority under
  the run owner's pre-authorization, with explicit non-goals around hosted
  schedulers, new durable stores, co-driving, and rescue authority. See
  `docs/rfcs/0122-scheduler-principal-auto-spawn.md:8-22`,
  `:83-183`, and `:203-218`.
- RFC 0124, auto-drive on run start: default-on single-run auto-drive and
  contracts C1-C6. See `docs/rfcs/0124-auto-drive-run-start.md:8-20`,
  `:55-78`, and `:147-168`.
- RFC 0128, cross-repo run boundary: keeps single-repo run invariants and
  models cross-repo outcomes through decomposition and typed handoffs, not
  atomic multi-repo writes. See `docs/rfcs/0128-cross-repo-run-boundary.md:20-35`
  and `:90-148`.
- RFC 0167, operator identity and run attribution: directly mentions one human
  driving many concurrent Striatum operators and the need to attribute runs to
  accountable principals. See `docs/rfcs/0167-operator-identity-and-run-attribution.md:18-30`
  and `:70-141`.

## Roadmap Context

The RFC roadmap requires Design -> Build -> Verify for every RFC item and
maintains a WIP cap, red-doctor budget, and feature-wave gate. See
`docs/operator/rfc-roadmap.md:14-54` and `:63-89`.

Wave 4 contains related feature items: constrained operator mode, offline
self-improvement, committee deliberation, optional git/PR integration, and
operator identity deployment. See `docs/operator/rfc-roadmap.md:143-153`.

The meta-operator design likely belongs near this feature wave, but this recon
does not assign it to a roadmap slot.

## Git And Checkout Discipline

The primary checkout must remain clean and current. Work should be isolated
where concurrent agents may be active, and turns should end clean. See
`AGENTS.md:166-197`.

This matters because the vague capability explicitly supervises several
campaigns/operators. Checkout ownership, branch freshness, and integration
serialization are first-class design inputs, not implementation details.

## Build And Verification Surfaces

Root targets delegate to Go:

- `make install`
- `make lint`
- `make typecheck`
- `make test`
- `make smoke`

Additional relevant targets include `make check-docs` and Go package tests under
`go/`. See `AGENTS.md:96-110`, `Makefile:32-49`, `Makefile:72-79`, and
`go/Makefile:30-45`.

## Documentation And Artifact Conventions

The docs map lives at `docs/index.md`. Reference docs include the spec, command
authority matrix, workflow docs, decision log, ubiquitous language, and roadmap.
See `docs/index.md:17-44`, `:72`, `:99`, and `:113-115`.

`docs/records` is the curated home for historical exhaust, but
`docs/operator/` and `docs/campaigns/` are intentionally not folded into records
because they remain runtime/operator contract surfaces. See
`docs/reference/doc-convention.md:24-40` and `docs/records/INDEX.md:10-29`.

These Level-0 artifacts therefore live under
`docs/operator/artifacts/striatum-meta-operator-design-bootstrap/`.

## Recon Implications

- A valid meta-operator design must compose existing daemon authority rather than
  creating an unofficial parallel state machine.
- A first design can be framed as control-plane coordination and proof, not as
  implementation work.
- Existing workflow shapes are enough to design the design. A new workflow shape
  or daemon object may be proposed later, but should not be assumed.
- The highest-risk boundaries are stale observations, checkout contention,
  authority creep, recovery bypass, and false completion claims.

## Recon Open Questions

- Which current daemon methods already support pause, quarantine, resume,
  refusal, recovery, or campaign classification?
- Is there a current durable concept named "campaign" in source or docs, or is
  it only a planning/operator term?
- Which proof surfaces are machine-readable today, and which require new API
  work?
- Would the first meta-operator supervise one daemon at a time or coordinate
  several local daemon contexts?
- Should this become an RFC, an operator workflow, a CLI command, a dashboard
  mode, or a combination?
