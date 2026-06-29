# Documentation Index

Reader-facing Markdown documents under `docs/`, plus aggregate rows for large
historical, generated, or scaffold directories. Per-run/generated directories
are listed by directory instead of file by file.

## Onboarding and how-tos

| File | Audience | Summary |
|---|---|---|
| [using-striatum.md](tutorials/using-striatum.md) | New arrival (RFC 0054) | Day-zero usage guide: the operator + principal model, prerequisites, day-zero setup, first run, escalation surface. Read this first. |
| [adopter-reading-path.md](tutorials/adopter-reading-path.md) | Team adopting striatum | Curated 6-RFC reading list explaining how the system thinks: DDD, lane attestation, multi-repo daemon, RPC envelope, PG substrate, human-principal role. Read after USING_STRIATUM. |
| [getting-started.md](tutorials/getting-started.md) | New user | From a fresh target repo to a running workflow in ~15 minutes; leads with AI-operator setup and keeps operator-by-hand notes as a sidebar. Superseded by USING_STRIATUM.md for the role-aware walkthrough; retained for the deeper install / verify steps. |
| [how-to-human.md](how-to/how-to-human.md) | Human principal | Escalation playbook for resolving blockers and decisions; retains the operator-by-hand walkthrough as reference. |
| [consumer-repo-layout.md](reference/consumer-repo-layout.md) | Target-repo owner (RFC 0056) | Opinionated-but-non-mandatory directory layout recommendations: where the workflow file lives, where artifacts land, what to gitignore. |
| [how-to-agent.md](how-to/how-to-agent.md) | Coding agent | Long-form companion to the RFC 0015 skill bundle; the workflow loop, work-packet shape, and what not to do. |
| [context-hygiene.md](explanation/context-hygiene.md) | Operator / agent author | Why session quality is not a function of token budget; repo-side, session-side, and model-side practices for replicating high-taste sessions. |
| [workflow-types.md](reference/workflow-types.md) | Workflow selector | Which workflow shape and lane set to choose; current starters, examples, defaults, and the roadmap toward a chooser UI. |
| [workflow-catalog.md](reference/workflow-catalog.md) | Workflow selector | Generated reference for bundled workflow catalog shapes and lane sets, including Mermaid graph previews. |
| [writing-workflows.md](how-to/writing-workflows.md) | Workflow author | How to author `workflow.json` from scratch: required fields, scaffold layout, common graph shapes, validation. |
| [cli-reference.md](reference/cli-reference.md) | Anyone | Flat list of every CLI verb plus stable exit codes; `--help` is authoritative. |
| [postgres-transition.md](how-to/postgres-transition.md) | Operator | The D094 / RFC 0043 PostgreSQL runbook: prerequisites, role setup, `daemon migrate-db`, `daemon owner-ddl apply`, daemon startup, `striatum repo add`, PostgreSQL verification, and exit codes 11 / 12. |
| [blob-transition.md](explanation/blob-transition.md) | Operator | The RFC 0072 blob-storage runbook: configuring `striatumd` against an S3-compatible service, adopting repos with `--apply-blob-creation`, bulk-migrating `docs/dogfood/` into blob storage, and verifying the round trip. |
| [daemon-runbook.md](how-to/daemon-runbook.md) | Operator | The RFC 0079 daemon operability runbook: `striatum daemon install/uninstall/status`, the portable systemd user unit, runtime layout (`daemon-go.sock`, `client-token`, `mcp-http-endpoint`, pidfile), `daemon.toml` DSN, `journalctl --user -u striatumd`, and troubleshooting. |
| [daemonize-run-drive.md](how-to/daemonize-run-drive.md) | Operator | Run-drive daemonization notes for long-lived operator execution and restart behavior. |
| [frontend-development.md](how-to/frontend-development.md) | Contributor | Local web UI development workflow, build/test commands, and generated asset expectations. |
| [lane-sandbox.md](how-to/lane-sandbox.md) | Operator / maintainer | Lane sandbox setup and constraints for supervised lanes. |
| [operator/INDEX.md](operator/INDEX.md) | Operator | RFC 0058 current-state surface: read `operator/BRIEF.md` first; it owns the live frontier and points to bounded plan links. Treat older roadmap/todo issue lists as secondary until they are refreshed. |

## Specifications and decisions

| File | Audience | Summary |
|---|---|---|
| [../ARCHITECTURE.md](../ARCHITECTURE.md) | Fresh agent / human | One navigable map of the substrate: components, state ownership, surfaces, write boundary, run model, failure legibility (GH #161). |
| [spec.md](reference/spec.md) | Anyone | The implementation contract for the V1 surface. The source of truth when this index and the runner disagree. |
| [domain-driven-design.md](explanation/domain-driven-design.md) | Anyone curious about the framing | Why the vocabulary is the model, not bookkeeping; bounded context, aggregate roots, value objects, the events log, and the daemon-method write-boundary invariant. |
| [doc-map.md](reference/doc-map.md) | Anyone editing the docs | The boundary contract: which doc owns what, what each doc deliberately does *not* contain, and the direction citations should flow. |
| [prd.md](reference/prd.md) | Product reader | The product requirements that drove the V1 design. |
| [decision-log.md](decisions/decision-log.md) | Product / architecture reader | Every product and architecture decision (`D###` rows) with reason, consequences, and revisit triggers. |
| [ubiquitous-language.md](reference/ubiquitous-language.md) | Anyone | Glossary of striatum-specific terms (run, session, lease, work packet, lane, etc.). |
| [todo.md](reference/todo.md) | Maintainer | Archived pointer retained for older links; current work lives in `operator/BRIEF.md`, `operator/rfc-roadmap.md`, bootstrap output, and open GitHub issues. |
| [roadmap.md](reference/roadmap.md) | Operator kicking off / resuming work | Forward-looking sequencing of TODO items, RFC follow-ups, and active runway. Use only after `operator/BRIEF.md`; live issue frontiers belong in the brief until roadmap refresh is mechanized. |
| [releasing.md](how-to/releasing.md) | Maintainer | Versioning policy and release cadence: when to bump major/minor/patch, the pre-release checklist, and changelog discipline. |
| [command-authority-matrix.md](reference/command-authority-matrix.md) | Maintainer | Inventory of CLI/RPC authority paths across Go daemon RPC route translations, capability scopes, and local PostgreSQL authority guardrails. |
| [daemon-method-tables.md](reference/daemon-method-tables.md) | Maintainer | Generated daemon method registry and CLI route translation reference, sourced from `contracts/daemon_methods.json` and guarded by Go daemon handler coverage and contract registry tests. |
| [doc-convention.md](reference/doc-convention.md) | Maintainer | Documentation placement convention for curated docs, write-once records, generated bodies, and ignored scratch. |
| [architecture/REMEDIATION_SYNTHESIS_2026-05-17.md](architecture/REMEDIATION_SYNTHESIS_2026-05-17.md) | Maintainer | Synthesis of the Codex remediation plans: D107, RFC 0068-0071, Go daemon port sequencing, PostgreSQL-only cleanup, Gemini/Antigravity decommissioning, and dogfood-065 execution plan. |

## Background and reference

| File | Audience | Summary |
|---|---|---|
| [mcp.md](explanation/mcp.md) | MCP integrator | Native Go daemon HTTP/SSE MCP endpoint discovery, authentication, tool discovery, tool calls, and agent-loop boundaries. |
| [harness-friction-patterns.md](explanation/harness-friction-patterns.md) | Maintainer / RFC author | Long-form record of recurring dogfood friction shapes (036-039) and the V1 fixes that landed; companion to RFC 0040. |
| [readme.md](readme.md) | Doc tree reader | Pointer file for `docs/`. |

## Agent guidance

> These are cited directly by the repo's `CLAUDE.md`; they tell coding
> agents how to consume this repo's domain docs, issue tracker, and triage
> vocabulary.

| File | Audience | Summary |
|---|---|---|
| [agents/domain.md](agents/domain.md) | Coding agent | Which domain/decision docs to read before exploring the codebase, and the repo's single-context documentation layout. |
| [agents/issue-tracker.md](agents/issue-tracker.md) | Coding agent | Issues and PRDs live as GitHub issues; the `gh` CLI conventions for create/read/list/comment/label/close. |
| [agents/triage-labels.md](agents/triage-labels.md) | Coding agent | Maps the five canonical triage roles to the GitHub label strings used in this repo. |
| [agents/roles/adjudicator.md](agents/roles/adjudicator.md) | Coding agent | The RFC 0093 adjudicator role: read only curated workflow state, publish a `collaboration_ledger` artifact at the packet-provided path. |

## Campaign scaffolds

| Path | Summary |
|---|---|
| [campaigns/](campaigns/) | Per-campaign workflow scaffolds (`<name>/workflow.json` plus `prompts/`, `roles/`, `artifacts/`) for multi-stage design and remediation runs (e.g. doctor-integrity-legibility, issue-290 fan-in design, RFC 0126–0128). |

## Research and comparison material

| Path | Summary |
|---|---|
| [research/](research/) | Background research and project comparisons: provenance/containment analyses (`PROVENANCE.md`, `TRUE_PROVENANCE_AND_CONTAINMENT.md`, `OPERATOR_BYPASS_DEFENSE_IN_DEPTH.md`), prior-art comparisons (`GASTOWN_COMPARISON.md`, `PROJECT_COMPARISON.md`), tool-harness profiles (`0010-tool-harness-profiles/`), and incubation requests. Frozen provenance — excluded from the doc-link check. |

## Historical (incubation provenance — not current product material)

> Each file below carries a banner at the top calling out its
> historical status. Read these only when you need to understand
> how a load-bearing decision was originally framed; for current
> behavior, the sources of truth are `docs/reference/spec.md`,
> `docs/decisions/decision-log.md`, and `docs/rfcs/`.

| File | Summary |
|---|---|
| [prior-art.md](explanation/prior-art.md) | Pre-PRD survey of orchestration tools that shaped early framing. Not a list of currently-tracked dependencies. |
| [interview-log.md](explanation/interview-log.md) | The interview rounds that produced the original PRD and the early `D###` decision rows. |
| [ENGRAM_INCUBATION_CONTEXT.md](records/_frozen/ENGRAM_INCUBATION_CONTEXT.md) | Engram-extraction provenance; striatum was extracted from a parent project. |
| [RFC_0014_DOGFOOD_FIX_SPEC.md](records/_frozen/RFC_0014_DOGFOOD_FIX_SPEC.md) | Pre-RFC-0001 dogfood findings; everything actionable here landed in subsequent RFCs. |

## RFCs

| File | Summary |
|---|---|
| [rfcs/](rfcs/) | All accepted/proposed Striatum RFCs. Each RFC has its own `.md` file plus a current entry in `rfcs/README.md`. |

## Dogfood material

| Path | Summary |
|---|---|
| [dogfood/](dogfood/) | Aggregate historical dogfood notes and friction register. |
| [dogfood/FRICTION_LOG.md](dogfood/FRICTION_LOG.md) | Aggregate friction register across runs. |
| [dogfood/HISTORICAL.md](dogfood/HISTORICAL.md) | Distinguishes the historical incubation runs from the current cadence. |
| [dogfoods/](dogfoods/) | Preserved named dogfood fixtures with workflow scaffolds, prompts, and roles. |

## Repository-level files

| File | Summary |
|---|---|
| [../README.md](../README.md) | Top-level pitch, daemon/Postgres quick start, current release/status table, and documentation table. |
| [../AGENTS.md](../AGENTS.md) | Project instructions for agents and contributors working on the striatum source. |
| [../CHANGELOG.md](../CHANGELOG.md) | Per-version release notes since `0.1.0`. |
