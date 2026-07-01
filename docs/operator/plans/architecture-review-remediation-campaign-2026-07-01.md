---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_architecture-review-remediation-campaign-2026-07-01"
scope_kind: "initiative"
scope_ref: "docs/audits/STRIATUM_DEEP_ARCHITECTURE_REVIEW_CLAUDE_FABLE_5_2026-07-01.md"
state: "open"
opened_at: "2026-07-01"
closed_at: null
closure_summary: null
supersedes: null
retrieval_priority: "high"
---

# Architecture Review Remediation Campaign - 2026-07-01
author: operator-gpt-5-codex-001

This plan converts the 2026-07-01 deep architecture review and the immediately
preceding 2026-06-28 deep architecture review into executable work. It is built
for a coordinator to hand to agents with: "fan out as many subagents as possible
and make this happen."

## Source Reviews

- Primary:
  `docs/audits/STRIATUM_DEEP_ARCHITECTURE_REVIEW_CLAUDE_FABLE_5_2026-07-01.md`
  (`85bd048a`, Claude Fable 5).
- Prior review:
  `docs/audits/STRIATUM_DEEP_ARCHITECTURE_REVIEW_GPT_5_CODEX_2026-06-28.md`
  (`8d794fb8` baseline, GPT-5 Codex).

Current-state verification for this plan ran at `478b3ef6` on `main`.
`operator bootstrap` reported daemon reachable and authorized, active runs 0,
open blockers 0, and `doctor ok=true` with six advisory worktree warnings.
`status --json --run-limit 0` reported no active runs. The primary review is
one commit behind current `main`; the 06-28 review baseline is 109 commits
behind current `main`. The findings below are therefore re-anchored to current
source where the review text drifted.

## Coordinator Rules

Start every execution session from the repository root:

```bash
./go/bin/striatum operator bootstrap --markdown
./go/bin/striatum status --json --run-limit 0
git fetch origin
git status --short --branch
```

Stop before launching subagents if `doctor` reports problems, if active runs
or dirty files overlap this campaign, or if `main` is behind `origin/main`.
Use isolated worktrees cut from freshly fetched `origin/main`. Each subagent
must own one workstream, must stay inside the listed paths, and must leave a
commit plus verification evidence. Do not open GitHub PRs. For tracker
coordination, use Plane project `Striatum` (`STRIATUM`); older GitHub issue ids
in the reviews are historical retrieval handles, not the place for new claims.

Global gate for every source-changing subagent:

```bash
git diff --check
make lint
make typecheck
make test
make check-docs
```

The coordinator reruns the global gate after integrating all branches, then
reruns:

```bash
./go/bin/striatum doctor --json
./go/bin/striatum operator bootstrap --markdown
```

## Triage Summary

The campaign keeps the reviews' priority ordering where it maps to current
source, but re-tiers stale or overstated items:

| ID | Tier | Source | Current decision |
| --- | --- | --- | --- |
| P0-MIGRATION-HASH-PARK | P0 | 07-01 blocker | Still live; fix first before any new migration-bearing slice lands. |
| P0-CLAUDE-PROVIDER-DOCTOR | P0 | 07-01 P0 recommendation | Still live; doctor route and `laneproviderauth.Check` are codex-only even though resolver/expiry code already models Claude. |
| P0-BLOB-BOUNDARY-WORDING | P0 | 06-28 P0 | Still live; spec product boundary says "no external persistence" while the same spec and source support operator-provided blob storage. |
| P1-ROUTE-BUDGET-GATE | P1 | 06-28 P0, 07-01 smell | Downgraded from P0: serious surface-control debt, not a runtime blocker. |
| P1-ESCALATION-NOTIFIER | P1 | 07-01 blocker/P1 | Keep as P1: escalation state exists; the missing wake-human path is an unattended-operation gap. Docs also overclaim an unimplemented hook. |
| P1-VERDICT-MODEL-IDENTITY | P1 | 07-01 serious | Still live; verdicts carry attestation/review-generation stamps but no declared model identity or same-model override stamp. |
| P1-DOCTOR-PLANES | P1 | 07-01 P1 | Still live; D276 split notices/warnings, but `doctor` still has one top-level `ok` fold and no `availability_ok` / `provenance_ok`. |
| P1-RFC0170-P1-CULLING | P1 | 06-28 P1, 07-01 smell | Still live; #618 and #619 remain open and roadmap lists P1 deferrals. |
| P1-READ-LEAST-PRIVILEGE | P1 | 06-28 P1 | Still live; many sensitive tables remain `runtime_sensitive_select` by explicit inventory. |
| P1-DEPRECATED-ROUTES | P1 | 06-28 P1 | Still live; contract has 155 methods and 10 deprecated aliases. |
| P2-PLACEMENT-ADOPTION | P2 | 07-01 P2 | Partly stale; artifact placement is already explicit and generator-tested. Remaining work is adoption policy/defaults for new non-self target repos. |
| P2-SWEEP-TRIP-LATCH | P2 | 07-01 P2 | Partly stale; read-side recovery cursor latch and doctor wedge checks exist. Missing piece is a breaker that excludes repeatedly degraded runs from candidate selection and escalates once. |
| P2-ACTIVE-COMMENT-CLEANUP | P2 | 06-28 P2 | Still useful but low risk; active docs and some Go tests/comments retain Python/parity/legacy wording. |
| P2-REVIEW-CADENCE-BUDGET | P2 | 07-01 smell | Process/documentation guardrail only; prevents reviews becoming another exhaust stream. |

## P0 Workstreams

### P0-MIGRATION-HASH-PARK

Source: 07-01 "Untyped migration-hash-mismatch -> log.Fatalf crash-loop".

Current actual: `go/pkg/db/migrations.go` returns
`fmt.Errorf("daemon PostgreSQL migration %d hash mismatch", ...)` from
`verifyRecordedHashTx`. `go/cmd/striatumd/main.go` handles
`AwaitingOwnerDDLError`, `SchemaDriftError`, and deploy activation halts with
exit 79, then falls through to `fatalf` for the hash mismatch.

Change: introduce `db.MigrationHashMismatchError`, return it from both recorded
hash checks and embedded/source hash mismatch checks where appropriate, and park
`striatumd` with the existing non-restartable exit 79 remediation branch.

Touches:

- `go/pkg/db/migrations.go`
- `go/pkg/db/migrations_test.go`
- `go/cmd/striatumd/main.go`
- `go/pkg/cli/localcommands/striatumd.service.tmpl` only if remediation text or
  exit-code documentation needs adjustment

Effort: hours.

Depends on: none. This lands before every workstream that adds migrations or
owner bundles.

Acceptance:

- New unit test proves a recorded hash mismatch is `errors.As`-detectable as
  `MigrationHashMismatchError`.
- New daemon boot-path test, or focused helper test if the boot path is hard to
  isolate, proves hash mismatch exits with the same non-restartable status used
  by owner-DDL/schema-drift halts.
- `make test` and `make check-docs` pass.

Parallelism: one subagent. Do not run model-identity, read-privilege, or culling
P1 migration work until this is merged.

### P0-CLAUDE-PROVIDER-DOCTOR

Source: 07-01 "doctor_lane_provider_auth.go guards codex-only".

Current actual: `HandleDoctorLaneProviderAuth` rejects any provider except
codex. `laneproviderauth.Check` also returns unsupported for non-codex
providers. Separately, `laneproviderauth` already has resolver and expiry
parsing for Claude credential files (`CLAUDE_CONFIG_DIR`,
`CLAUDE_SECURESTORAGE_CONFIG_DIR`, `.credentials.json`,
`claudeAiOauth.expiresAt`).

Change: make the explicit doctor provider-auth check provider-aware for codex
and Claude. For Claude, prefer a cheap offline credential freshness check using
the existing resolver/sampler/expiry machinery; do not run a billed model turn.
Keep unsupported providers fail-closed with a clear remediation.

Touches:

- `go/pkg/reads/doctor_lane_provider_auth.go`
- `go/pkg/reads/doctor_test.go`
- `go/pkg/laneproviderauth/lane_provider_auth.go`
- `go/pkg/laneproviderauth/*_test.go`
- `docs/reference/command-authority-matrix.md`
- `docs/reference/spec.md` and `docs/reference/cli-reference.md` if user-facing
  provider-auth docs mention codex-only behavior

Effort: 1 day.

Depends on: none.

Acceptance:

- `doctor --lane-provider-auth claude` reaches a checked offline result rather
  than `schema_invalid`.
- Tests cover fresh Claude OAuth expiry, expired Claude OAuth expiry, absent
  credential, resolver mismatch, and unsupported provider.
- The ordinary `doctor` path still does not execute provider auth preflights
  unless explicitly requested.
- Secret-bearing env is not returned in doctor output.

Parallelism: one subagent. Safe to run with P0-MIGRATION-HASH-PARK because paths
are disjoint.

### P0-BLOB-BOUNDARY-WORDING

Source: 06-28 P0 "Fix spec/product-boundary wording for blob storage".

Current actual: `docs/reference/spec.md` still says Striatum "does not provide
hosted services, external persistence" in the product boundary, but later
sections specify `blob_exhaust`, `git_pointer_manifest`, generated records in a
repository blob bucket, and RFC 0123 placement behavior. The source implements
placement and blob-backed artifacts.

Change: make the product boundary distinguish prohibited hosted/external
persistence from optional operator-provided local/S3-compatible blob storage.
Keep the local-first rule explicit: no bundled cloud service, telemetry, or
remote transcript export; operator-provided blob endpoints are a configured
durability backend under RFC 0072/0123/0171.

Touches:

- `docs/reference/spec.md`
- `README.md` or `ARCHITECTURE.md` only if they repeat the absolute wording
- `docs/reference/command-authority-matrix.md` only if the blob methods need a
  boundary note

Effort: hours.

Depends on: none.

Acceptance:

- `docs/reference/spec.md` product boundary links the blob sections/RFCs and no
  longer contradicts optional blob behavior.
- `make check-docs` passes.
- No product text suggests Striatum provides or manages hosted persistence.

Parallelism: one docs subagent. Safe with both P0 code subagents.

## P1 Workstreams

### P1-ROUTE-BUDGET-GATE

Source: 06-28 P0 route-budget recommendation; 07-01 noted it remained
unadopted.

Current actual: `docs/reference/command-authority-matrix.md` requires every new
RPC method or handwritten route map to update the matrix, and generated
guardrails check contract freshness. It does not require the "replaces,
extends, why not existing" rationale that the review asked for.

Change: add a route-budget rule to the command-authority contract and enforce it
with the lightest useful guardrail. New daemon methods must name whether they
replace an existing method, extend an existing state transition, or introduce a
new daemon-owned transition with an accepted decision/RFC. Deprecated aliases
and one-shot backfills must be excluded from "new surface" rationalization by a
retirement plan.

Touches:

- `docs/reference/command-authority-matrix.md`
- `docs/operator/rfc-roadmap.md`
- `contracts/daemon_methods.json` only if metadata is added there
- `go/pkg/rpc/*_test.go` or a small docs/contract guard if the rule becomes
  executable

Effort: 1 day.

Depends on: none.

Acceptance:

- The rule is visible in the authority matrix near the source-input contract.
- A test or check fails when a new non-deprecated daemon method lacks route
  budget rationale, or the doc explicitly records why this remains review-owned
  rather than machine-enforced.
- A subagent can point to the rule before adding any future RPC method.

Parallelism: one docs/contract subagent. It can start in wave 0 because it does
not add migrations.

### P1-ESCALATION-NOTIFIER

Source: 07-01 "Escalation terminates in unnotified PG state"; RFC 0020 hook
history.

Current actual: recovery escalation inserts blockers and `escalation_inbox`
rows, flips runs to `needs_operator`, and records events. Source search shows
no Go implementation for a live notifier. Current spec and CLI reference still
claim `recovery_policy.escalation_hook` runs in live sweeps, which appears stale
against source.

Change: choose one of two paths after a short source-confirmation spike:

- Preferred implementation path: add an opt-in, post-commit, best-effort
  notifier fired after escalation rows commit. Use a daemon env var such as
  `STRIATUM_ESCALATION_NOTIFY_URL` and allow only loopback or tailnet HTTP(S)
  targets unless a product decision says otherwise. Never run inside the DB
  transaction and never make notification success authoritative.
- If the current product decision is not to implement notification now, remove
  the stale hook claims from spec/CLI docs and record a tracked P1 gap instead.

Touches:

- `go/pkg/mutations/recovery_escalation.go`
- `go/pkg/mutations/recovery_auto.go`
- new small notifier helper package if useful
- `docs/reference/spec.md`
- `docs/reference/cli-reference.md`
- `docs/rfcs/0020-autonomous-stalled-run-recovery.md` status notes if needed

Effort: 1-2 days.

Depends on: P1-ROUTE-BUDGET-GATE only if a new daemon method is introduced; the
preferred env-var post-commit path should not need a new RPC.

Acceptance:

- Test proves escalation commits even when notification fails.
- Test proves no HTTP request is made in dry-run mode.
- Test proves notifier payload has run id, repository id, blocker id, job id
  when available, and stable escalation kind, with no raw transcript fields.
- Docs match source exactly.

Parallelism: one recovery subagent after P0s.

### P1-VERDICT-MODEL-IDENTITY

Source: 07-01 "Verdict rows carry no model identity".

Current actual: `verdicts` has posture, attestation stamps, provenance override
fields, supervisor id, and review generation. Workflow authoring can derive
model families from declared lane `display_model` or `model`, and validation
can refuse same-model pairings, but the recorded verdict row does not persist
declared model identity/family/basis.

Change: add a schema and write-boundary stamp for model identity on verdicts.
Use declared identity only; fail toward `unknown`, never toward stronger proof.
Add a run completion/co-blindness qualifier so same-model override risk is
auditable after the workflow snapshot is gone or changed.

Touches:

- new runtime migration `0048_*` and `go/pkg/db/migrations.go`
- possibly owner bundle if `verdicts` is owner-held in the live deployment
- `go/pkg/mutations/review.go`
- `go/pkg/mutations/claim.go`
- `go/pkg/reads/status.go`, `run.summary`, `evidence.export`, web/detail if
  they render verdict provenance
- `docs/reference/spec.md`
- `docs/reference/command-authority-matrix.md` only if surfaces change

Effort: several days.

Depends on: P0-MIGRATION-HASH-PARK.

Acceptance:

- Two-role pgtest proves the migration/owner-bundle path is safe under the
  runtime role.
- Review submit stamps `model_identity_declared`,
  `model_family_at_record`, and `model_identity_basis` from the immutable
  workflow snapshot or `unknown`.
- Override/operator/recovery verdict surfaces stamp `unknown` or explicit
  operator basis, not a fabricated model.
- Run summary/evidence export expose the fields redacted as needed.
- A retro-audit read can count historical verdicts as `unknown` without
  rewriting them.

Parallelism: one schema/review subagent. Do not start until
P0-MIGRATION-HASH-PARK has landed.

### P1-DOCTOR-PLANES

Source: 07-01 "Doctor plane tags + availability_ok/provenance_ok split".

Current actual: D276 split advisory notices from actionable warnings, but
`HandleDoctor` still returns a single top-level `ok = len(problems) == 0`.
Problem records do not all carry a plane, and operator policy still reads
"red doctor" as one stop condition.

Change: add plane classification to doctor problem records and top-level
`availability_ok` / `provenance_ok`. Preserve `ok` for compatibility, but make
operator docs bind the stop-and-fix rule to availability problems plus
provenance problems that imply durability loss or recovery-gate breach.

Touches:

- `go/pkg/reads/doctor.go`
- doctor subcheck files that produce problem records
- `go/pkg/reads/doctor*_test.go`
- `docs/reference/spec.md`
- `docs/how-to/how-to-agent.md`
- `docs/operator/BRIEF.md` when this lands

Effort: 1-2 days.

Depends on: none.

Acceptance:

- `doctor --json` includes `availability_ok`, `provenance_ok`, and per-problem
  `plane` where verbose records exist.
- Existing consumers of `ok` still pass.
- Tests cover at least one availability problem, one provenance problem, and one
  advisory notice.
- Agent/operator docs no longer imply every provenance-only warning freezes all
  unrelated operations.

Parallelism: one reads/docs subagent after P0s.

### P1-RFC0170-P1-CULLING

Source: 06-28 P1 "Complete RFC 0170 P1 blockers"; 07-01 docs/provenance
exhaust concern.

Current actual: RFC 0170 P0 observe-only is integrated. Roadmap still lists P1
deferrals #618 and #619: whole-tree `status:frozen` citation exactness and a
non-cooperative filesystem hang fence.

Change: close both P1 blockers without adding deletion/reaper behavior. Split
into two subagents:

- P1-RFC0170-FROZEN-CITATIONS: make Tier-1 citation scan treat all
  `status:frozen` records as non-live sources wherever they live, not just
  under `docs/records/_frozen/`.
- P1-RFC0170-CULL-SLOT-LIVENESS: bound non-cooperative filesystem hangs with a
  cull-slot liveness fence and late-writer generation guard so a stuck scan
  cannot corrupt candidacy or starve later scans.

Touches:

- RFC 0170 implementation under `go/pkg/recovery/decay_tick_sweep.go` and
  related cull code
- `go/pkg/db/sql/0045_cullable_entity.sql` only if an additive migration is
  needed for generation fencing; otherwise no schema change
- `docs/rfcs/0170-self-culling-repository-and-cull-workflow-class.md`
- `docs/operator/rfc-roadmap.md`

Effort: several days.

Depends on: P0-MIGRATION-HASH-PARK if any migration is needed.

Acceptance:

- Existing known-set corpus tests still have zero false positives.
- New fixture proves a frozen record outside `docs/records/_frozen/` is not a
  live citation source.
- New timeout/fence test proves a late result from an expired scan cannot write
  candidacy for the newer generation.
- Roadmap marks #618/#619 equivalent blockers closed or names remaining blocker.

Parallelism: two subagents, with one coordinator to reconcile shared cull tests.

### P1-READ-LEAST-PRIVILEGE

Source: 06-28 P1 "Continue read-side least-privilege closure".

Current actual: `go/pkg/db/read_authority_inventory.go` deliberately classifies
many sensitive tables as `runtime_sensitive_select`, including artifacts,
events, sessions, jobs, runs, verdicts, work packets, supervisors, generated
records, and `client_capabilities`. The first narrow slice formalizes the
existing owner-bundle-0005 `clients` token-secret gate as
`runtime_column_scoped_select`: `SELECT *` and `token_hash`/`token_salt` reads
are denied, while named non-secret metadata columns remain directly selectable.
The comments are honest that this is not private-read denial.

Change: pick one narrow table family and move it from broad runtime SELECT to a
projection/column-scoped read, with a two-role test. Do not try to close the
whole inventory in one branch. Suggested first target: `verdicts` after
P1-VERDICT-MODEL-IDENTITY, or `clients`/`client_capabilities` if a projection is
already mature.

Touches:

- `go/pkg/db/read_authority_inventory.go`
- owner bundle under `go/pkg/db/sql/owner/`
- relevant read handlers/tests for the chosen table family
- `docs/reference/spec.md`

Effort: several days per table family.

Depends on: P0-MIGRATION-HASH-PARK. For `verdicts`, also depends on
P1-VERDICT-MODEL-IDENTITY.

Acceptance:

- Runtime role cannot `SELECT *` from the narrowed table in a two-role pgtest.
- Required daemon read path still works through the projection or explicit
  column grant.
- Inventory class changes from `runtime_sensitive_select` to
  `runtime_projection_read`, `runtime_select_denied`, or a named column-scoped
  class with tests.

Parallelism: one subagent for the first table family. Additional table-family
subagents can run only after the first pattern is accepted.

### P1-DEPRECATED-ROUTES

Source: 06-28 P1 "Retire deprecated aliases and migration/backfill routes".

Current actual: `contracts/daemon_methods.json` has 155 methods, 10 deprecated
aliases: `recovery.auto`, `ack`, `heartbeat`, `release`, `block`, `complete`,
`publish_artifact`, `claim_next`, `verdict`, and `submit_review`. Historical
backfill methods (`corpus.migrate_historical_dogfood_file`,
`artifact.backfill_blob`) remain active.

Change: prove current generated skills, CLI routes, MCP descriptors, and docs no
longer require deprecated aliases, then retire the smallest safe batch. Keep
one-shot backfills until their no-op/closed-state proof is explicit.

Touches:

- `contracts/daemon_methods.json`
- generated RPC/CLI/MCP route files
- `docs/reference/daemon-method-tables.md`
- `docs/reference/command-authority-matrix.md`
- route freshness/registry tests

Effort: 1-2 days for aliases; more if backfills are included.

Depends on: P1-ROUTE-BUDGET-GATE.

Acceptance:

- Deprecated alias count decreases.
- Generated routes and installed skill bundle checks remain green.
- Removed methods audit as `method_unknown` with clear replacement docs.
- No current workflow packet command block depends on removed names.

Parallelism: one contract subagent. Do not mix this with unrelated route adds.

## P2 Workstreams

### P2-PLACEMENT-ADOPTION

Source: 07-01 provenance-exhaust concern and placement-policy recommendation.

Current actual: RFC 0123 placement classes are implemented; workflow validation
accepts `blob_exhaust`, `git_publication`, and `git_pointer_manifest`;
generated workflows test that expected artifacts declare placement. The
remaining gap is adoption pressure and per-target policy, not the primitive.

Change: add a per-repository/operator placement posture for new generated
workflows and non-self target repos. Default dialogue/review chatter to
`blob_exhaust` when blob is configured; keep final deliverables and compact
manifests git-retained.

Touches:

- `go/pkg/workflowgenerate/*`
- `go/pkg/artifactcontracts/placement.go`
- `docs/reference/spec.md`
- `docs/how-to/blob-transition.md`

Effort: several days.

Depends on: P0-BLOB-BOUNDARY-WORDING.

Acceptance:

- New generated multi-lane workflows keep final artifacts git-publication but
  route review/dialogue exhaust to blob where configured.
- A no-blob repository either refuses blob-required generated workflows with a
  precise error or emits git-compatible placement by policy.
- Reviewer-context seeding is proven: a downstream reviewer can read upstream
  blob-routed findings through `artifact.get_content` or explicit packet
  context, not by assuming the body is on the run branch.

Parallelism: one workflow-generation subagent. Defer if no second target repo is
being onboarded.

### P2-SWEEP-TRIP-LATCH

Source: 07-01 latching sweep-breaker recommendation.

Current actual: `upsertSchedulerCursor` stores read-side latch metadata
(`claimable_job_count`, `last_lane_advanced_at`, latch errors), and doctor can
red a recovery cursor wedge. The active-run sweep still selects every running or
paused run each tick; no repeated-degradation trip excludes a poison run from
candidate selection.

Change: convert the visibility latch into a breaker. Track consecutive
degraded sweep results for recovery cursors, trip after a small threshold, mark
the run `needs_operator` or `escalation_pending` through daemon recovery, and
exclude tripped runs from later candidate SELECTs until recovery clears them.

Touches:

- `go/pkg/recovery/sweep.go`
- `go/pkg/db/sql/0001_baseline.sql` only for comments; additive migration if a
  real counter column is chosen instead of JSON
- `go/pkg/reads/doctor.go`
- recovery cursor tests

Effort: 1-2 days.

Depends on: P0-MIGRATION-HASH-PARK if schema changes.

Acceptance:

- Test injects a per-run panic/error for N ticks and proves the run stops being
  swept after the trip threshold.
- Test proves a healthy run in the same tick continues to sweep.
- Doctor surfaces the tripped run and the required recovery command.
- Recovery clear/resume path resets the trip.

Parallelism: one recovery subagent.

### P2-ACTIVE-COMMENT-CLEANUP

Source: 06-28 P2 active comment cleanup.

Current actual: active docs and tests still mention Python, parity, SQLite, and
legacy states in places where that is historical context, not live behavior.
Some of this is valid retirement provenance; some of it is cognition noise.

Change: run a narrow cleanup over active Go comments and active docs only. Do
not touch frozen records. Convert "Python parity" comments into current
Go/PostgreSQL contract wording where the code no longer has Python behavior.

Touches:

- active Go comments under `go/`
- `README.md`, `ARCHITECTURE.md`, `docs/reference/spec.md`,
  `docs/reference/command-authority-matrix.md`, and active how-to docs

Effort: hours to 1 day.

Depends on: none.

Acceptance:

- `rg "Python|SQLite|parity|legacy" go docs/reference README.md ARCHITECTURE.md`
  has only intentional, current, or clearly historical hits.
- `make check-docs` passes.

Parallelism: one docs/comment subagent. Keep it out of migration or route files
being changed by higher-priority subagents until after integration.

### P2-REVIEW-CADENCE-BUDGET

Source: 07-01 "five deep reviews in 30 days" smell.

Current actual: reviews are landing as durable docs faster than remediation
campaigns can close them. There is no local rule requiring a deep review to
produce a tracker/campaign follow-through or to wait for a wave boundary.

Change: add an audit follow-through rule: a deep architecture review must either
open/update a campaign plan and tracker set for its P0/P1 findings, or explicitly
mark findings refused/deferred. Recommend one deep review per shipped roadmap
wave unless the operator asks for an incident-specific audit.

Touches:

- `docs/audits/README.md` if present, otherwise create it
- `docs/operator/rfc-roadmap.md` only if the review cadence should be tied to
  roadmap waves

Effort: hours.

Depends on: none.

Acceptance:

- Future deep-review instructions point to a follow-through artifact.
- No durable review can silently add P0/P1 work without an owner, refusal, or
  deferral.

Parallelism: one docs/process subagent.

## Suggested Fanout

Wave 0 can run four subagents in parallel:

1. P0-MIGRATION-HASH-PARK
2. P0-CLAUDE-PROVIDER-DOCTOR
3. P0-BLOB-BOUNDARY-WORDING
4. P1-ROUTE-BUDGET-GATE

Coordinator barrier after Wave 0:

- merge P0-MIGRATION-HASH-PARK first;
- rerun `make test`, `make check-docs`, and `doctor --json`;
- only then allow migration-bearing P1 work.

Wave 1 can run up to seven subagents after the barrier:

1. P1-ESCALATION-NOTIFIER
2. P1-VERDICT-MODEL-IDENTITY
3. P1-DOCTOR-PLANES
4. P1-RFC0170-FROZEN-CITATIONS
5. P1-RFC0170-CULL-SLOT-LIVENESS
6. P1-DEPRECATED-ROUTES
7. P2-SWEEP-TRIP-LATCH

Hold P1-READ-LEAST-PRIVILEGE until the model-identity schema path proves the
current migration/owner-bundle pattern. It is valuable but high-blast-radius.

Wave 2 cleanup can run after Wave 1 integration:

1. P2-PLACEMENT-ADOPTION
2. P2-ACTIVE-COMMENT-CLEANUP
3. P2-REVIEW-CADENCE-BUDGET
4. First narrow P1-READ-LEAST-PRIVILEGE table family

## Dependency Map

- P0-MIGRATION-HASH-PARK must land before any workstream that adds a runtime
  migration or owner bundle.
- P0-BLOB-BOUNDARY-WORDING must land before P2-PLACEMENT-ADOPTION.
- P1-ROUTE-BUDGET-GATE must land before P1-DEPRECATED-ROUTES and before any
  workstream that adds a daemon method.
- P1-VERDICT-MODEL-IDENTITY should land before choosing `verdicts` as the first
  P1-READ-LEAST-PRIVILEGE table.
- P1-DOCTOR-PLANES should land before changing the AGENTS/BRIEF stop policy for
  red doctor classes.
- P1-RFC0170-FROZEN-CITATIONS and P1-RFC0170-CULL-SLOT-LIVENESS can land in
  either order if they do not share schema.

## Deferred Or Refused

- Replace daemon/PostgreSQL/RPC with a smaller substrate: refused. Both reviews
  say the spine is justified by current failure modes.
- Hosted control plane, Kubernetes, managed cloud, telemetry, or provider SDK
  integration: refused by product boundary.
- Full artifact-placement rewrite as P0: refused. The placement primitive is
  already built; remaining work is adoption policy and reviewer context seeding.
- Full read least-privilege closure in one pass: refused. Use one table family
  at a time with two-role tests.
- Conversation/interrogation removal in this campaign: deferred. The route
  budget/deprecated-route work can re-triage them, but there is no evidence here
  that removal is safe.
- Web UI or metrics removal: refused. Reviews classify them as bounded and
  useful local read surfaces; keep them read/local/allowlisted.

## Open Questions

1. Is a second non-Striatum target repository a 2026 adoption goal? If yes,
   P2-PLACEMENT-ADOPTION should move up after the P0s because mainline
   provenance exhaust is an adoption blocker.
2. Should cloud S3 be explicitly discouraged while local Garage/MinIO is
   supported, or should all operator-provided S3-compatible endpoints be treated
   equally? P0-BLOB-BOUNDARY-WORDING must answer this in product text.
3. For P1-ESCALATION-NOTIFIER, is a loopback/tailnet HTTP POST enough, or does
   the operator want marker-file/shell hooks retained from RFC 0020? The smallest
   safe implementation is HTTP-only.
4. For P1-ROUTE-BUDGET-GATE, should the rationale live in
   `contracts/daemon_methods.json` metadata and be mechanically tested, or in
   the decision/RFC plus command-authority docs? Prefer metadata only if it stays
   compact.
5. Which table family should be first for P1-READ-LEAST-PRIVILEGE after the
   model-identity path: `verdicts`, `clients`, or `events`?

## Campaign Done

This campaign is done when:

- all P0 workstreams are landed and verified;
- P1 workstreams are either landed, explicitly refused, or split into accepted
  successor plans with tracker entries;
- `doctor --json` is green or has only acknowledged warnings unrelated to this
  campaign;
- `make lint`, `make typecheck`, `make test`, and `make check-docs` pass on
  integrated `main`;
- `docs/operator/BRIEF.md`, `docs/operator/rfc-roadmap.md`,
  `docs/reference/spec.md`, `docs/reference/command-authority-matrix.md`, and
  the decision log are updated where the landed changes alter current state;
- Plane work items record branch/worktree, base SHA, verification evidence, and
  final commit SHA for each workstream.
