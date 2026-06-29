# Striatum Deep Architecture Review - GPT_5_CODEX - 2026-06-28

## Verdict

**ROUGHLY RIGHT-SIZED | ON TRACK**

Confidence: **medium-high**.

Biggest risk: **the self-hosting/provenance machinery is still growing faster
than the repository can delete, hide, or expire the evidence it creates**.

Load-bearing assumption: Striatum is the product described in
`docs/reference/spec.md`: a local-first workflow runner for terminal AI coding
agents, with daemon-owned PostgreSQL as the authority, capability-gated RPC/MCP
surfaces, supervised terminal lanes, durable provenance, and self-hosted
multi-agent review. If the real goal were only "a small CLI that starts one or
two local agents sometimes", this repository would be overbuilt. Under the
stated product boundary, the core substrate is mostly the right size.

Risk posture: **acceptable but narrow**. The project is actively correcting
known excesses: D270 deleted the cross-repo surface, D264 created an explicit
subtraction gate, and D271/RFC 0170 added the first observe-only culling
substrate. That is the right trajectory. The remaining risk is that route
count, provenance volume, and DB authority exceptions create more future
failure classes than the current tests and operator discipline can absorb.

## Review Scope And Method

Prompt source: `~/git/prompts/DEEP_ARCHITECTURE_REVIEW.md`.

Working tree baseline:

- `main` at `8d794fb8` / `origin/main`.
- Shared checkout was clean before review.
- `striatum operator bootstrap --markdown` reported daemon reachable and
  authorized, `VERSION=2.39.0`, active runs `0`, open blockers `0`, and doctor
  `ok=true` with no problems.

I used static review, repository inventory, and git history. I did not execute
target build/test/package-manager scripts or network calls. The prompt forbids
executing target code/tests/builds/scripts/hooks/package managers/Docker/network
without explicit authority; the only live command intentionally run was the
project cold-start bootstrap required by `AGENTS.md`.

Directly reviewed source and docs included:

- `README.md`
- `ARCHITECTURE.md`
- `docs/index.md`
- `docs/reference/spec.md`
- `docs/reference/ubiquitous-language.md`
- `docs/reference/todo.md`
- `docs/operator/BRIEF.md`
- `docs/operator/rfc-roadmap.md`
- `docs/decisions/decision-log.md`
- `docs/how-to/blob-transition.md`
- RFCs 0033, 0043, 0048, 0072, 0078, 0123, 0142, 0167, 0170 by targeted reads
  and references
- `contracts/daemon_methods.json`
- `go/go.mod`
- `go/cmd/striatum/main.go`
- `go/cmd/striatumd/main.go`
- `go/cmd/striatum-supervisor-helper/main.go`
- `go/pkg/rpc/*`
- `go/pkg/mcp/*`
- `go/pkg/db/*`
- `go/pkg/db/sql/*.sql` by inventory, with direct review of the current
  `0045_cullable_entity.sql`
- `go/pkg/mutations/*` by package inventory and targeted review of run,
  claim, artifact, worktree, recovery, supervision, review, and registry paths
- `go/pkg/reads/*` by package inventory and targeted review of status, doctor,
  artifact, list, trajectory, and bootstrap read paths
- `go/pkg/recovery/*`
- `go/pkg/blob/*`
- `go/pkg/agentloop/*`
- `go/pkg/supervisor/*`
- `go/pkg/verifier/*`
- `go/pkg/webservice/*`
- `go/pkg/metrics/*`
- `go/pkg/workflowauthoring/*`
- `go/pkg/cli/localcommands/*`

Skipped or intentionally de-emphasized:

- Ignored local build/cache/venv/dist/bin/coverage output.
- `.striatum/` runtime scratch.
- Most historical frozen records and individual operator-run artifact bodies.
  I counted and sampled these because they are architecturally important as
  accumulated provenance, but reading every historical artifact body would
  overweight old dogfood transcripts against current product behavior.

## Stated Architecture

The stated architecture is clear and unusually explicit:

- Striatum is a standalone, local-first workflow runner for terminal AI coding
  agents, not an Engram-specific process script
  (`docs/reference/spec.md:10-19`).
- Live workflow state lives in daemon-owned PostgreSQL; `.striatum/` is
  operational scratch only (`docs/reference/spec.md:21-40`,
  `docs/reference/spec.md:68-76`).
- Repository files are durable provenance, not the live control plane.
- The daemon is mandatory; local-state/no-daemon fallback is retired
  (`docs/reference/spec.md:31-40`, `docs/reference/spec.md:43-57`).
- RPC, MCP, CLI, local web UI, and metrics are client surfaces around the same
  local daemon (`ARCHITECTURE.md`, `go/cmd/striatumd/main.go:333-421`).
- Product scope explicitly avoids hosted services, telemetry, provider SDK
  integration, durable transcript capture, and remote-serving semantics unless a
  later decision says otherwise (`docs/reference/spec.md:10-19`).

This is a coherent product boundary. It is not the smallest possible tool, but
the hard parts are real: crash recovery, capability-scoped terminal lanes,
review provenance, run-branch integration, replayable artifacts, and local-only
operation without relying on provider-specific hosted APIs.

## Actual Architecture

The actual implementation mostly matches the statement:

- `striatumd` owns the database connection, migrations, capability authorizer,
  RPC server, MCP HTTP/SSE adapter, optional web UI, metrics, recovery sweep,
  and optional auto-spawn scheduler (`go/cmd/striatumd/main.go:47-83`,
  `go/cmd/striatumd/main.go:333-421`).
- The RPC layer is the real authority choke point. It requires handshake,
  checks method registry membership, enforces repository scope, authorizes the
  capability token, threads the auth context into handlers, and refuses to
  answer without audit provenance when audit append fails
  (`go/pkg/rpc/server.go:70-165`).
- The route contract is generated from `contracts/daemon_methods.json`.
  Current surface area is 151 daemon methods, 99 CLI routes, and 10 deprecated
  methods.
- Mutation registration is centralized but very broad. `mutations.Register`
  wires session, work, artifact, repo, process, git, worktree, supervise,
  workflow, review, run, operator, branch, decision, checkpoint, verifier,
  recovery, interrogation, conversation, corpus, and blob-backfill handlers
  (`go/pkg/mutations/mutations.go:70-182`).
- The read/write authority inventories are explicit. Writes to audit,
  artifacts, and events are SECURITY DEFINER gated, while much live
  coordination state remains runtime DML (`go/pkg/db/write_authority_inventory.go:22-33`,
  `go/pkg/db/write_authority_inventory.go:50-160`). Reads still classify many
  sensitive tables as runtime-readable pending further least-privilege work
  (`go/pkg/db/read_authority_inventory.go:52-121`).
- Blob storage is optional and S3-compatible. It is disabled when
  `STRIATUM_BLOB_ENDPOINT` is absent, but when configured it is a real MinIO/S3
  gateway (`go/cmd/striatumd/main.go:1010-1025`, `go/pkg/blob/client.go:17-39`).
  The implementation does integrity readback on upload and sha checks on reads
  (`go/pkg/blob/client.go:103-165`, `go/pkg/blob/client.go:196-224`).
- RFC 0170 P0 self-culling is observe-only. The recovery scheduler launches a
  detached cull scan, skips if one is already in flight, applies a timeout, and
  writes only candidacy state (`go/pkg/recovery/decay_tick_sweep.go:66-95`,
  `go/pkg/recovery/decay_tick_sweep.go:155-175`).
- Local CLI exceptions are documented rather than accidental. Workflow
  authoring, skill/plugin installation, operator bootstrap, daemon install,
  owner DDL, and migration helpers are local by design
  (`go/pkg/cli/localcommands/localcommands.go:9-22`).

The implementation is therefore not a pile of disconnected scripts. It is a
large but coherent local control plane with a strong authority spine. The main
architectural debt is surface and artifact accumulation, not absence of design.

## Value Vs Complexity Ledger

| Subsystem | Value | Complexity | Recommendation |
| --- | --- | --- | --- |
| Daemon + PostgreSQL as authority | Core. This is the product's source of truth and recovery basis. | High, but justified. | Keep. Do not reintroduce file-marker authority. |
| RPC registry + capability gates | Core. One method registry and one authorizer keep CLI/MCP/web honest. | Moderate-high. | Keep, but freeze net-new routes unless they replace or retire existing surface. |
| Mutation handlers | Core. They encode the workflow state machine. | Very high. `mutations.Register` is now a front-door map of almost everything. | Simplify by retiring aliases and one-shot migrations; split only where it improves authority review. |
| Read/write DB inventories | High. They make authority posture inspectable. | Moderate. | Keep. Finish read least-privilege closure. |
| Recovery scheduler and doctor | High. This is what makes self-hosted runs recoverable. | High. | Keep, but keep recovery features behind observed failure classes. |
| Terminal supervision and agent loop | Core. Product value depends on portable terminal agents. | High but load-bearing. | Keep. Security principal work remains important. |
| Worktree/run-branch integration | Core for reviewed code_change flows. | High. | Keep. Avoid manual bypasses; this is correctly treated as provenance-critical. |
| Artifact publishing and blob storage | High if dogfood artifacts continue to grow. | Medium-high; optional S3 expands the boundary. | Keep local/operator-provided blob storage, but fix product-boundary wording. |
| Workflow authoring/generation | Useful for adoption and self-hosted design/build/verify. | Medium. | Keep, but do not let template count become a substitute for product primitives. |
| Web UI | Useful local read surface. | Bounded. | Keep read-oriented; avoid growing it into a hosted product. |
| Metrics | Useful for live ops and lane auth/recovery. | Bounded if allowlisted and consent-gated. | Keep; no broad telemetry expansion. |
| Operator artifacts/workflows in git | Provenance value, but large search/cloning/cognition tax. | Very high and growing. | Aggressively move exhaust to blob/pointers and make RFC 0170 P1 real. |
| Deprecated aliases and one-shot migration routes | Compatibility value. | Low individually, high collectively. | Cut once current skills/CLI no longer need them. |
| Cross-repo orchestration | Low current value. | High. | Correctly cut by D270; do not revive without a second-adopter workflow. |

## Findings

### 1. The authority spine is right-sized, not ceremonial

The daemon/PostgreSQL/RPC design is expensive, but the repository has enough
failure modes to justify it. The key evidence is not just the diagrams; it is
that mutation authority, audit append, repo scoping, and capability checks meet
at one RPC choke point (`go/pkg/rpc/server.go:70-165`) and that the daemon
refuses malformed config before serving (`go/cmd/striatumd/main.go:130-140`).

The project has repeatedly hit real recovery and authority failures: two-role
PostgreSQL DDL incidents, stale sessions, cross-session token problems,
unsealed exits, fan-in correctness, and row-projection outages. A simpler
file-marker runner would probably be easier to read and less able to recover
honestly.

Recommendation: keep the spine. Optimize around it, not around bypassing it.

### 2. Provenance accumulation is now the main architectural drag

The repository is carrying thousands of tracked docs and operator records. The
current tracked inventory is dominated by `docs/`, including hundreds of
operator artifacts and more than a thousand workflow records. D264 says the
same thing in product language: RFCs, branches, decisions, and docs were
accumulating faster than verification/subtraction could retire them
(`docs/decisions/decision-log.md:43`). The roadmap now encodes a red-doctor
budget, RFC/WIP cap, and feature-wave fuse (`docs/operator/rfc-roadmap.md:63-89`).

RFC 0170 is the right response, but P0 is deliberately only observe-only.
`DecayTickSweep` launches a detached scan and writes candidacy state, not
deletion or repo cleanup (`go/pkg/recovery/decay_tick_sweep.go:66-95`). The
roadmap explicitly records the remaining P1 blockers: whole-tree frozen-citation
exactness and a fence for non-cooperative filesystem hangs
(`docs/operator/rfc-roadmap.md:140`).

Recommendation: make culling P1 the next structural subtraction priority after
security-principal and deploy-safety blockers that are already in flight. Until
then, treat every new durable Markdown artifact as carrying a cleanup cost.

### 3. RPC and route surface area is high enough to become its own failure class

The route count is already large: 151 daemon methods, 99 CLI routes, 10
deprecated methods. Some breadth is inherent, but the generated method table
now includes product surfaces like `conversation.*`, `interrogation.*`,
historical dogfood corpus migration, artifact blob backfill, recovery variants,
workflow authoring, web/dashboard reads, and low-level process/worktree paths.

This matters because every route needs capability classification, repo scoping,
audit class, docs, CLI/MCP parity, and tests. The recent rowByID/run projection
outage described in the operator state is evidence that the current surface can
outrun two-role/read-scope coverage.

Recommendation: add a "route budget" gate to the roadmap next to the RFC/WIP
cap: new RPC method only if it retires an older method, exposes a truly new
daemon-owned state transition, or has an explicit decision record. Retire the
10 deprecated aliases once generated skills and current CLI wrappers no longer
use them.

### 4. Product-boundary docs contradict optional blob storage

The spec says Striatum "does not provide hosted services, external persistence,
telemetry" (`docs/reference/spec.md:10-19`). Later, the same spec describes
`blob_exhaust`, repository blob buckets, blob hashes, and pointer manifests
(`docs/reference/spec.md:1010-1027`). The implementation includes a real
MinIO/S3-compatible client and optional daemon wiring
(`go/cmd/striatumd/main.go:1010-1025`, `go/pkg/blob/client.go:17-39`).

This is not necessarily a bad architecture. Local Garage/MinIO is a reasonable
way to keep lane exhaust out of git while preserving reconstructability. The
problem is wording. "No external persistence" reads absolute, while current
behavior supports operator-provided S3-compatible storage when configured.

Recommendation: change the product boundary to say "no hosted or external
persistence by default; optional operator-provided local/S3-compatible blob
storage is allowed only under RFC 0072/0123 placement rules." If cloud S3 is
still allowed but discouraged, say so directly and call out the local-first
tradeoff.

### 5. Runtime read least-privilege is still incomplete

The write side has a strong and explicit inventory: audit, artifacts, and events
are SECURITY DEFINER gated while live coordination tables are classified as
runtime DML (`go/pkg/db/write_authority_inventory.go:22-33`,
`go/pkg/db/write_authority_inventory.go:50-160`). The read side is more honest
than complete: many sensitive tables still sit in
`runtime_sensitive_select`, including artifacts, events, clients,
conversations, jobs, sessions, work packets, runs, verdicts, and supervisor
state (`go/pkg/db/read_authority_inventory.go:52-121`).

That is not a hidden flaw. The comments explicitly say the inventory "does not
claim private-read denial" (`go/pkg/db/read_authority_inventory.go:5-8`). But
the blast radius of a leaked runtime credential remains meaningful.

Recommendation: continue the #164/read-projection work before expanding remote
read surfaces or tailnet/web capabilities. The existing inventory gives a good
checklist; use it to drive table-by-table closure rather than broad claims.

### 6. Local CLI exceptions are mostly justified, but they need constant pressure

The architecture says the daemon is authoritative, but the CLI still has local
commands. The good news is that these are named and rationalized:
workflow-validation/generation, skill/plugin installation, operator bootstrap,
daemon systemd/config bootstrap, migration, and owner DDL
(`go/pkg/cli/localcommands/localcommands.go:9-22`).

Those exceptions are reasonable because they exist before the daemon is ready or
operate on local authoring/install files rather than daemon workflow state. The
risk is exception creep: every future "just local" helper weakens the mental
model unless it is classified the same way.

Recommendation: keep the local-command rationale file as an authority test. A
local command should be accepted only if it is pre-daemon bootstrap, local
authoring/install, or owner/admin deployment work that the runtime role cannot
perform.

### 7. The project is feature-rich but correctly biased toward reliability now

The roadmap has the right sequencing: reliability waves before feature waves
(`docs/operator/rfc-roadmap.md:56-62`), a red-doctor budget and feature fuse
(`docs/operator/rfc-roadmap.md:63-89`), and explicit in-flight security and
lane-health work (`docs/operator/rfc-roadmap.md:105-140`). D270 shows actual
subtraction, not just talk: cross-repo product surface was deleted because
maintenance/front-door complexity exceeded value
(`docs/decisions/decision-log.md:37`).

That makes the project "on track". It does not mean safe to accelerate feature
work. Security-principal work, deploy-safety activation, read least-privilege,
and culling P1 are all structural prerequisites to lower operational load.

Recommendation: preserve the current reliability-first roadmap. Do not reopen
feature-wave items until the wave gates say they are clear.

### 8. Historical language still leaks into active Go-era code comments

There are still Go comments and docs that mirror or reference old Python-era
surfaces, including the handler-registration comment in
`go/cmd/striatumd/main.go:1040-1045` and package comments in active read/supervisor
paths. This is not a runtime architecture defect, but it is a cognition defect:
new contributors have to constantly ask whether a comment is historical,
parity-oriented, or current truth.

Recommendation: run a narrow docs/comment cleanup pass for "Python", "parity",
old cross-repo wording, and historical Engram phrasing in active code comments.
Do not touch frozen records.

## Inverse Missing Check

If Striatum were underbuilt, I would expect to find:

- Direct file markers or terminal scraping as live workflow authority.
- Mutating CLI/MCP/web paths that bypass the same authorization/audit layer.
- Recovery behavior that hand-finishes stranded worktrees and calls that
  success.
- No durable record of decisions, verifier posture, or run state.
- No tests around the DB authority boundary.
- Hosted service dependencies or provider SDK lock-in hidden in the control
  plane.

That is not what this repository shows. The missing pieces are narrower:

- Read-side least privilege is incomplete.
- Culling is observe-only and not yet a repo-cleaning mechanism.
- Per-lane security principal work is designed but not built.
- Some route aliases and historical migration/backfill paths remain after their
  value window.
- Product-boundary wording has not caught up with optional blob storage.

So the right diagnosis is not "underbuilt". It is "large, mostly coherent, and
needing subtraction discipline."

## What To Keep

Keep these parts even though they are complex:

- Daemon-owned PostgreSQL as sole live state.
- RPC method registry and capability-gated dispatch.
- Fail-closed audit append.
- Per-repository scoping.
- Work-packet/session/lease model.
- Supervised terminal lanes instead of provider SDK integration.
- Run-branch/worktree integration through daemon state.
- Doctor and recovery scheduler.
- Verifier receipts that honestly distinguish ASSERTED from stronger claims.
- Optional local blob storage for exhaust, if the docs state the boundary
  clearly.

These are not accidental overengineering; they are the product's differentiation.

## What To Cut Or Constrain

Highest-value cuts:

1. Retire deprecated RPC aliases once current generated skills and CLI wrappers
   are proven off them.
2. Retire or quarantine one-shot migration/backfill methods after they have done
   their job, especially `corpus.migrate_historical_dogfood_file` and
   `artifact.backfill_blob`.
3. Re-evaluate `conversation.*` as a current product surface. If it has no
   active workflow owner, mark it experimental, hide it, or schedule removal.
4. Keep D270 closed: do not revive cross-repo orchestration without a concrete
   second-adopter workflow and a new RFC.
5. Move more lane exhaust out of tracked git and into blob/pointer placement.
6. Add an explicit route-budget/subtraction check to RFC design acceptance.

## Recommendations

| Priority | Recommendation | Why | Acceptance Check |
| --- | --- | --- | --- |
| P0 | Fix spec/product-boundary wording for blob storage. | Current docs say no external persistence while code and later spec sections support optional S3-compatible blob storage. | `docs/reference/spec.md` distinguishes hosted/external persistence from operator-provided optional blob storage and links RFC 0072/0123. |
| P0 | Add a route-budget rule to the roadmap or command-authority docs. | 151 methods is already enough that every new route should justify itself against retirement. | New RPC methods require explicit "replaces/extends/why not existing method" rationale. |
| P1 | Complete RFC 0170 P1 blockers before relying on culling for cleanup. | P0 only nominates; it does not reduce repo size or artifact load. | #618 and #619 closed; cull action remains opt-in with false-positive protection. |
| P1 | Continue read-side least-privilege closure from the inventory. | Runtime-sensitive SELECT remains broad. | Sensitive tables move to denied/projection/column-scoped classes with two-role tests. |
| P1 | Retire deprecated aliases and migration/backfill routes after compatibility proof. | Cuts surface area without changing core behavior. | Generated contracts show fewer methods; CLI/MCP skill bundles still pass route freshness checks. |
| P2 | Clean active code comments that still describe Python-era parity or retired surfaces. | Lowers cognitive load for future maintainers. | Active Go comments no longer imply Python is the current authority. Frozen records unchanged. |
| P2 | Keep web and metrics surfaces read/local/allowlisted. | They are useful but can easily become an accidental hosted product. | No new remote mutation surface without decision log entry. |

## Open Questions

1. Is cloud S3 an explicitly supported deployment for blob storage, or only a
   technically-compatible-but-discouraged operator choice?
2. What is the intended support window for deprecated bare aliases like
   `claim_next`, `ack`, `heartbeat`, and `publish_artifact`?
3. Does `conversation.*` have a current workflow owner, or is it leftover
   exploratory surface?
4. Should route budget be enforced mechanically by tests, or is decision-log
   discipline enough for now?
5. Once RFC 0170 P1 is built, which artifact classes should move first from git
   publication to blob/pointer placement?

## Final Assessment

Striatum is not small, but the current size largely follows from a demanding and
well-specified product: local terminal-agent orchestration with recoverable
state, strict provenance, capability boundaries, and self-hosted review. The
core architecture is defensible.

The project is on track because it is actively deleting or gating excess rather
than rationalizing all of it: cross-repo was cut, the roadmap now has a
subtraction budget, and culling has started. The next architectural gains should
come from fewer routes, less tracked exhaust, tighter runtime reads, and clearer
blob-storage boundary language, not from replacing the daemon/Postgres spine.
