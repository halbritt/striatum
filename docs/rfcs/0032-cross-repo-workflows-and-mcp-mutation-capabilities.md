# RFC 0032: Cross-Repo Workflows and MCP Mutation Capabilities

Status: accepted (V2 slice)
Date: 2026-05-11
Context:
[`RFC 0028`](0028-long-running-daemon-and-multi-repository-control-plane.md),
[`RFC 0030`](0030-daemon-rpc-server-and-version-skew-protocol.md) (accepted),
[`RFC 0031`](0031-daemon-owned-supervision-and-sealed-apply-boundary.md) (accepted),
[`RFC 0033`](0033-storage-substrate-rewrite-for-daemon-v2.md) (accepted),
[`docs/DECISION_LOG.md`](../decisions/decision-log.md) (D082, D083, D086),
[`docs/MCP.md`](../reference/mcp.md),
[`docs/SPEC.md`](../reference/spec.md) § "Product Boundary", § "State Store", and
§ "Workflow Config",
`src/striatum/mcp.py`

RFC 0032 is the last RFC in the daemon V2 follow-up sequence. It depends on
RFC 0030 (daemon RPC server) being in place because both cross-repo
operations and MCP mutation flow over that boundary.

Implementation status: dogfood-035 landed the accepted V2 slice: workflow
schema validation for `repositories` / per-job `repository`, daemon DB
migration v3 for cross-repo run metadata, method-registry scope modes,
the `recovery` capability, daemon MCP `tools/list` filtering and
`tools/call` re-authorization/audit scaffolding, and mocked lifecycle
helpers for prepare/start/cancel/reconcile. Current production run state is
daemon-owned PostgreSQL under participating repositories' `repository_id`
scopes; no per-repo SQLite rows are written outside migration fixtures.
The real two-repo daemon end-to-end test harness and helper-level
prepare/start/cancel progression landed in follow-up work after this RFC.
Production live cross-repo scheduler fan-out remains future product work and
requires a new bounded RFC.

## Problem

RFC 0028 V1 ships multi-repository **introspection**: `dashboard --all`
aggregates read-only views across registered repos, daemon MCP exposes
resources spanning repos, and recovery sweep iterates over registered
active runs. It does not ship multi-repository **mutation**: no workflow
can declare jobs that touch two repositories, no MCP client can drive a
workflow change beyond status reads, no daemon-mediated transaction
coordinates work across repos.

Two open questions from RFC 0028 land here:

- **OQ#4**: should cross-repository workflows be in scope? V1 said
  "introspection only." Daemon V2 with an RPC trust boundary
  (RFC 0030) and substrate that supports MVCC and `LISTEN`/`NOTIFY`
  (RFC 0033) lets us answer "yes, but bounded."
- **OQ#5**: which capabilities are safe to expose through MCP by
  default? V1 daemon MCP is resources-only. Daemon-mediated mutations
  now exist (RFC 0030 RPC). Without explicit gating, a prompt-injected
  MCP client could exercise any capability its token carries.

Common product use cases that motivate cross-repo:

- A monorepo split into N service repos that ship together.
- Library + consumer pairs where review of a library change requires
  evidence from a consumer-side smoke run.
- A documentation repo that mirrors API changes from a service repo;
  the workflow wants to coordinate both ends without two separate
  manually-driven runs.

V2 has the building blocks for these (registered repos, daemon RPC,
capability tokens, audit). The missing pieces are workflow schema
support, transaction semantics, and MCP-side gating.

## Goals

- Define a workflow-schema vocabulary for cross-repo jobs and their
  artifacts.
- Define daemon-mediated transaction semantics across repository
  boundaries: when a cross-repo job lands, what consistency guarantees
  does the daemon offer?
- Define the MCP mutation capability vocabulary and the default-deny
  posture: which MCP methods accept which capabilities, what the
  daemon does when a token lacks the requested capability, and how the
  audit records the decision.
- Preserve V1 single-repo workflows unchanged. Cross-repo is opt-in.
- Resolve RFC 0028 OQ#4 and OQ#5.

## Non-Goals

- Hosted or networked cross-machine coordination. All repos remain
  local under D082 / D083.
- Multi-user MCP semantics. Single OS user per machine (D083).
- Defining sealed apply or daemon-owned supervision. Those are RFC
  0031. Cross-repo workflows MAY use sealed apply receipts where each
  repo is independently sealed-eligible, but the cross-repo orchestration
  layer does not invent new provenance.
- Replacing the existing workflow.json schema. Cross-repo is additive
  via opt-in fields.

## Proposal

### 1. Cross-repo workflow schema

Workflows that span repositories declare a `repositories` block at the
top level:

```json
{
  "schema_version": "striatum.workflow.v1",
  "workflow_id": "cross-repo-example",
  "repositories": {
    "primary": {"repo_id": "repo_abc..."},
    "consumer": {"repo_id": "repo_def..."}
  },
  "jobs": [
    {
      "id": "draft",
      "repository": "primary",
      "role_id": "author",
      "lane_id": "codex",
      "...": "..."
    },
    {
      "id": "consumer_smoke",
      "repository": "consumer",
      "role_id": "implementer",
      "lane_id": "codex",
      "needs": ["draft"],
      "...": "..."
    }
  ]
}
```

Rules:

- A workflow with a `repositories` block is a **cross-repo workflow**.
  The block enumerates participating repos by registered `repo_id`.
- Every job declares its target `repository`. Jobs without an explicit
  `repository` target the workflow's primary repo (the first entry by
  registration order, or an explicit `primary_repository_id`).
- Each repo must be registered with the daemon before `run prepare`.
  Day-zero registration is handled by `striatum adopt`, `striatum repo add
  <path> --init`, or by retiring legacy SQLite state before registration.
- Cross-repo run state is stored in daemon-owned PostgreSQL under the
  participating repositories' `repository_id` scopes. No per-repo SQLite
  rows are written; crash recovery reconciles daemon Postgres rows.
- Write scopes are evaluated per repo: a job's `write_scope.allowed_paths`
  is interpreted relative to that job's target repo.
- Artifacts are per-repo: a job's `expected_artifacts[].path` is
  written into the job's target repo.
- Reviewer access scopes (RFC 0018) are extended to allow
  `cross_repo_artifact_augmented` — the reviewer receives artifacts
  from all participating repos' relevant jobs.

### 2. Transaction semantics

Cross-repo runs do not promise distributed transactions. They promise:

- **Daemon-mediated coordination**: all state transitions for the
  cross-repo run pass through the daemon. The daemon writes cross-repo
  metadata and per-repository run rows into daemon-owned PostgreSQL under
  the participating `repository_id` scopes in one daemon transaction.
- **Best-effort consistency on crash**: if the daemon crashes mid-transition,
  startup reconciliation observes daemon Postgres rows and completes or
  rolls back the transition; tests assert both branches.
- **No cross-repo atomic file mutations**: workflows that need
  "two repos must end up consistent" remain the workflow author's
  responsibility. The daemon coordinates ordering and verdicts, not
  file-system atomicity.

V2 explicitly refuses to design cross-machine or cross-host semantics.
Cross-repo means cross-registered-local-repo only.

### 3. Cross-repo cycle and edge handling

- Edges may cross repo boundaries (`{"from": "draft", "to":
  "consumer_smoke", "on": "completed"}`). The daemon resolves the
  reference via the cross-repo run.
- Cycles may cross repo boundaries. `max_iterations` is global to the
  cycle, not per-repo.
- Parallelism limits in `parallelism.max_active_jobs` are global to
  the cross-repo run, not per-repo. Per-repo limits can be set with
  `parallelism.per_repo_max_active_jobs`.

### 4. Cross-repo coordinator

A cross-repo workflow declares one coordinator session that may span
repos. The coordinator session is registered against the cross-repo
run, not a single repo, and carries a capability scope that allows
read across all participating repos.

The coordinator's authority is the same as in V1: route work, monitor
state, escalate human checkpoints. It does not perform role work.

### 5. MCP mutation capability vocabulary

V1 daemon MCP exposes resources only. V2 adds tools, gated by explicit
capabilities.

New capability vocabulary (extends RFC 0030 §4):

```text
read         introspection across repos
write        ordinary workflow mutations (claim, ack, publish, complete)
review       record verdicts, submit reviews
claim        claim work packets
apply        sealed-apply authority (RFC 0031)
admin        register repos, create/revoke tokens, recovery cancel
recovery     run recovery sweeps and resume blockers
```

V2 token defaults:

- A new admin token (bootstrapped by `daemon start`; repo registration reads
  the runtime `client-token` file when needed)
  carries `admin` only.
- A token granted to an MCP client by default carries `read` only. The
  operator must explicitly add `write`, `review`, `claim`, `apply`,
  `recovery`, or `admin`.
- A capability may be scoped to a `repo_id` or be daemon-global
  (`scope: "daemon"`).

MCP tool gating:

- `tools/list` returns only tools whose required capability the client
  token holds.
- `tools/call` rejects calls whose required capability the token does
  not hold; the daemon records an audit `denied` row with
  `denial_reason: capability_missing`.
- The daemon never bypasses capability gating when an MCP client claims
  a "trusted" identity. There is no equivalent of V1's global
  `--allow-mutations` flag in V2.

MCP method registry maps 1:1 onto the RFC 0030 method registry. Each
RPC method that has a daemon route is exposed via MCP as a tool with
the same name and capability requirements. The daemon advertises this
registry via MCP `initialize` so MCP clients can discover what they
may call.

### 6. MCP audit and prompt-injection safety

- Every MCP tool call records the same audit row shape as the
  equivalent RPC call (RFC 0030 §5). Audit retention applies.
- Prompt-injection mitigation: capability tokens are the only access
  path. A prompt-injected MCP client cannot escalate beyond the
  capabilities its token holds; the operator controls token grants.
- Tokens are explicit and revocable. Operators may grant short-lived
  tokens via `daemon.token.create --expires-in 1h`.
- MCP clients are documented as untrusted: the operator chooses what
  capabilities a given client receives. Defaults are conservative.

### 7. CLI surface for cross-repo

```text
striatum run prepare --workflow <path>      # auto-detects cross-repo
striatum status --run-id <cross_repo_id>    # daemon-routed
striatum dashboard --run-id <cross_repo_id>
striatum --daemon dashboard --all           # unchanged
```

New verbs:

```text
striatum cross-repo list                    # list cross-repo runs
striatum cross-repo why <cross_repo_id>     # cross-repo blocker view
striatum cross-repo describe <cross_repo_id>
```

V1 single-repo verbs continue to work unchanged. The CLI client library
resolves the run id to either a single-repo or cross-repo run.

### 8. Compatibility with V1

- V1 single-repo workflows are unchanged. No schema migration required.
- V1 daemon MCP resources-only behavior is preserved as the default
  for tokens that lack any mutation capability.
- V1 `--allow-mutations` semantics on `striatum serve` (RFC 0012) are
  retired by V2; the V2 capability model replaces it. Documentation
  must call this out.
- The dogfood-031 BUILD_HANDOFF and DESIGN_SYNTHESIS already document
  that V1 MCP is read-only; V2 adds mutation under capability gating.

### 9. Test infrastructure

- A two-repo cross-repo harness: two `tmp_path` subdirectories each
  initialized as Striatum repos, both registered with the daemon, one
  cross-repo workflow exercising at least one edge across the
  boundary.
- Cross-repo restart tests: daemon crashes mid-transition; daemon startup
  reconciles daemon Postgres rows.
- MCP capability tests: a token with only `read` cannot call a tool
  requiring `write`; the audit row records `denied`.
- MCP audit tests: every tool call produces an audit row matching the
  RPC audit shape.
- Prompt-injection-shape tests: a malicious MCP request asking for
  `tools/list` and then `tools/call` with elevated args is refused
  unless the token explicitly carries the capability.

### 10. Provenance and trust implications

- Cross-repo runs concentrate authority more than single-repo runs.
  The daemon must not be tempted to invent cross-repo provenance.
  Bylines remain per-job, per-session, attested per RFC 0026.
- MCP mutation is a real attack surface. Documentation must say
  prompt-injection is a threat and the operator-issued capability
  token is the defense.
- Apply receipts (RFC 0031) span single repos only in V2; cross-repo
  sealed apply requires a follow-up RFC.
- Local-only constraint preserved.

## Compatibility and Migration

- RFC 0030 (RPC) and RFC 0033 (substrate) land first.
- RFC 0031 (supervision + sealed) lands next; sealed receipts are now
  start-able.
- RFC 0032 adds cross-repo workflow schema and MCP mutation gating.
- V1 single-repo workflows and V1 MCP resources behavior unchanged.
- New workflow validator surface accepts the `repositories` block;
  unknown fields elsewhere still rejected.

## Downsides and risks

- Cross-repo coordination concentrates failure radius: a daemon bug
  can disrupt N repos at once.
- MCP mutation defaults trade prompt-injection safety for ergonomics.
  Conservative defaults plus explicit capability grants must be the
  documented operator UX.
- Cross-repo runs' best-effort consistency is a real footgun. Tests
  must exercise the daemon-crash branch carefully.
- The `repositories` block is new schema territory. Reviewers will
  push on edge cases (renamed repos, repos removed during a run,
  diamond-shaped cross-repo dependencies).
- Cross-repo workflows are harder to dogfood than single-repo; the
  initial dogfood-032 fixture will need to spin two registered repos.

## Benefits

- Workflow authors can express service+consumer, library+docs, and
  similar paired-repo cases without two manually-coordinated runs.
- Daemon MCP becomes useful for agent-driven workflows beyond
  introspection.
- Capability vocabulary is finally complete: read, write, review,
  claim, apply, admin, recovery — operators can reason about
  least-privilege tokens.
- Cross-repo audit lives in one daemon DB; one place to inspect
  multi-repo activity.

## Acceptance Criteria

- A two-repo cross-repo workflow validates, prepares, and runs to
  completion against the daemon RPC. Test asserts artifacts land in
  the correct per-repo paths.
- The daemon's cross-repo coordination survives a daemon crash during
  transition; restart reconciles to either `started` or `aborted`.
- Per-repo write-scope enforcement is preserved: a job targeting
  repo A cannot write into repo B.
- MCP `tools/list` filters by capability; `tools/call` refuses
  capability-missing requests and records the audit row.
- A token with only `read` cannot invoke a `write` tool; the audit
  row records `denial_reason: capability_missing`.
- A token with `apply` scoped to repo A cannot apply against repo B.
- `striatum cross-repo list/why/describe` work end-to-end.
- Documentation updates: `docs/SPEC.md`, `docs/MCP.md`,
  `docs/UBIQUITOUS_LANGUAGE.md`, `docs/CLI_REFERENCE.md`,
  `docs/HOW_TO_HUMAN.md`, RFC 0028 status block.

## Open Questions

- What happens when a participating repo is unregistered mid-run?
  Recommendation: pause the cross-repo run with a human checkpoint;
  refuse to advance until the repo is re-registered or the run is
  canceled.
- What is the right schema field for the cross-repo run id? A separate
  `cross_repo_run_id` namespace? Recommendation: yes, distinct from
  per-repo `run_id` to avoid prefix-search confusion.
- Should `tools/list` cache by `methods_etag` (RFC 0030 §3)?
  Recommendation: yes, with per-token effective-tool-set caching.
- Should the operator be able to bulk-grant capabilities to MCP
  clients? Recommendation: no; explicit grants only. Future RFC may
  add an "MCP profile" if the manual UX becomes painful.
- Should cross-repo workflows support cycles that re-enter a repo
  after leaving? V1 cycles are bounded; cross-repo cycles need careful
  iteration accounting. Recommendation: allow but require explicit
  `cross_repo_cycle: true` flag and document the iteration bound is
  global, not per-repo.

## Domain Modeling

Terms to add to `docs/UBIQUITOUS_LANGUAGE.md` after acceptance:

- **Cross-repo run** — a daemon-mediated run spanning two or more
  registered repositories. Identified by `cross_repo_run_id` in the
  daemon DB; each participating repo records a `local_run_id`
  pointing back to it.
- **Primary repository** — the workflow's anchor repo. Jobs without
  explicit `repository` target it. The first entry in the
  `repositories` block or the explicit `primary_repository_id`.
- **Cross-repo coordinator** — a coordinator session registered
  against a cross-repo run with capability scope spanning all
  participating repos.
- **MCP mutation capability** — a daemon capability granting the right
  to invoke an MCP tool that mutates state (write, review, claim,
  apply, admin, recovery). Distinct from `read`, which only permits
  introspection.
- **Effective tool set** — the subset of daemon MCP tools a token may
  invoke, computed by intersecting the daemon's method registry with
  the token's capability scope. Surfaced via `tools/list`.
