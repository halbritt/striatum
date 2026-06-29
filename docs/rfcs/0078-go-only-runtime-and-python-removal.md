# RFC 0078: Go-Only Runtime And Python Removal

Status: accepted
Date: 2026-05-24
Accepted: 2026-05-25 (closure run `run_74896f697a83f9f4c76c090b53cd7508`;
aggregate validation green — see
`docs/operator/artifacts/rfc-0078-closure/verify/SUMMARY.md`)
Final cleanup: 2026-05-30 — the deferred `src/` tree (the dead TS/React frontend
+ vite output, ~71 files) was deleted together with the Makefile `ui-*` targets
and the CI `frontend` job. The only shipped web surface is the Go SSE UI
(`go/pkg/webassets`, RFC 0092); no Go code referenced `src/` except historical
provenance comments.
author: proposer-codex-gpt-5.5-001
Context:
[`RFC 0030`](0030-daemon-rpc-server-and-version-skew-protocol.md),
[`RFC 0043`](0043-postgres-as-sole-substrate-and-daemon-required-runtime.md),
[`RFC 0068`](0068-go-production-daemon-port.md),
[`RFC 0070`](0070-daemon-client-service-boundary.md),
[`docs/SPEC.md`](../reference/spec.md),
[`docs/DECISION_LOG.md`](../decisions/decision-log.md),
[`docs/TODO.md`](../reference/todo.md)

Supersedes if accepted:
D018's V1 Python implementation preference, RFC 0068's "keep Python CLI/web
clients where useful" carve-out, and RFC 0070's non-goal of removing the
Python CLI.

Current implementation note:
The max-parallel execution workflow completed on 2026-05-24 as
`run_ef93ee9055bb77e40d2ae2c846337176`. It scaffolded the cutover ledger and
per-surface handoffs, added the first Go `striatum workflow validate` CLI
scaffold, and expanded Go artifact kind/front-matter parity for operator,
Git/PR, and auto-finalize gate artifacts. Full Python removal remains open.
On 2026-05-25, the remaining work was scaffolded as six executable gates plus
an umbrella tracker under `docs/operator/workflows/rfc-0078-*`; all workflow
JSON files validate. Those six gates were then executed with six parallel
sub-agents. The integrated result lands Go CLI RPC routing, Go artifact
contracts and workflow/generator parity slices, Go web service scaffolding,
and Go-only release archives and smoke scripts. RFC 0078 is accepted: active
Python source, pytest, packaging, scripts, and current runtime guidance are
removed or archived.

## Problem

The Go daemon cutover is complete for the production daemon and MCP
authority path, but the repository still has a Python application surface:
the CLI, local web service, workflow authoring helpers, frontend packaging
glue, tests, release metadata, and docs still assume Python is a supported
runtime.

That mixed-language state now creates product and maintenance cost:

- operators still see Python installation, packaging, and test guidance even
  though the daemon authority is Go;
- contributors must keep Python and Go behavior aligned across CLI, web,
  workflow validation, generator output, front-matter schemas, and smoke tests;
- architecture guardrails must distinguish "retired Python daemon/MCP" from
  "still-supported Python client", which is increasingly subtle;
- "remove Python daemon" and "remove Python entirely" sound similar but are
  very different cutovers.

The owner direction is now stronger than RFC 0068: remove all Python traces
from the active repository head. This needs a dedicated RFC because it
supersedes accepted implementation-language decisions and changes packaging,
tests, docs, and operator installation, not only daemon internals.

For this RFC, "all Python traces" means the current tracked repository head
and shipped product surfaces. Normal Git history is not rewritten by this
cutover.

## Goals

- Make Go the only Striatum runtime language in the active product tree.
- Replace the Python CLI with a Go `striatum` CLI that preserves the accepted
  daemon-required command contract or explicitly retires commands through a
  release decision.
- Replace the Python local web/service process with a Go local service, or
  retire the route if a feature is no longer part of the product.
- Port workflow validation, workflow generation, artifact/front-matter
  validation, skill/plugin scaffold generation, operator-doc helpers, and
  smoke/doctor behavior needed by the accepted product surface.
- Move tests from pytest/Python fixtures to Go tests and focused shell or
  browser tests where appropriate.
- Remove Python packaging and install surfaces: `pyproject.toml`,
  setuptools/package-data metadata, Python console scripts, Python-only
  release checks, and Python virtualenv guidance.
- Remove Python source, Python tests, Python fixtures, Python cache rules, and
  current docs that tell operators or agents to run Python.
- Add guardrails that prevent new Python source, tests, packaging, or
  operator-facing instructions from reappearing.
- Preserve the local-first boundary: daemon-owned PostgreSQL remains live
  state; no hosted services, telemetry, external persistence, or cloud APIs
  are introduced.

## Non-Goals

- Rewriting Git object history to erase past Python commits.
- Changing the daemon RPC envelope, capability model, PostgreSQL authority, or
  MCP transport semantics unless a missing Go client requires a compatible
  implementation.
- Adding a second runtime language as the replacement for Python.
- Deleting historical provenance solely because it mentions that Python used
  to exist, unless the owner chooses strict active-doc cleanup for repository
  HEAD.
- Removing Node/Vite frontend contributor tooling unless a separate UI
  packaging decision chooses to do so. This RFC targets Python.
- Reopening repo-local SQLite, Python daemon, Python MCP, or legacy local-state
  code as temporary bridges.

## Proposal

### 1. Inventory And Cutover Ledger

Create a generated or curated ledger of every Python trace in tracked HEAD:

- source files: `*.py`, `*.pyi`;
- tests and fixtures under `tests/`;
- packaging and release metadata: `pyproject.toml`, setuptools config,
  console scripts, wheel/sdist checks, package-data references;
- CI and Makefile targets that invoke Python tools;
- shell scripts and examples that call `python`, `pytest`, `pip`, `venv`,
  `mypy`, or `ruff`;
- docs, RFCs, skills, templates, and operator briefs that describe Python as
  a current runtime;
- generated frontend or web packaging glue that assumes Python resources.

The ledger classifies each item as one of:

| Class | Meaning |
|---|---|
| `port` | Replace with Go code or Go tests before deletion. |
| `retire` | Delete because the behavior is obsolete or already superseded. |
| `rewrite_doc` | Update current docs to Go-only language. |
| `historical_provenance` | Retain only if accepted as historical text; otherwise rewrite or move out of active docs. |
| `delete_after_gate` | Remove after a named parity or packaging gate passes. |

No implementation slice should delete a Python surface without naming the
ledger row and the replacement, retirement reason, or doc rewrite.

### 2. Go CLI Parity Or Explicit Command Retirement

The shipped `striatum` executable becomes a Go binary. Existing command
families fall into three buckets:

- **daemon clients:** commands that should call existing daemon RPC methods;
- **local authoring helpers:** commands such as workflow validation or
  scaffolding that need Go-native local implementations;
- **retired compatibility commands:** commands that survive today only as
  compatibility shims and should be hidden or removed through a release note.

The cutover must not duplicate daemon authority in the CLI. Mutating live
workflow behavior stays behind daemon RPC. Local helpers must not open
PostgreSQL except through accepted admin/bootstrap paths.

### 3. Go Web And Local Service Replacement

The local web UI remains local-only and loopback-bound if retained. The Go
service should serve the accepted UI routes, static assets, workflow browser,
artifact view, doctor/status views, and mutation endpoints by calling daemon
RPC. Static assets should be embedded or otherwise shipped with the Go
distribution without Python package-data machinery.

Routes that cannot justify their maintenance cost must be explicitly retired
in the cutover ledger and docs. Silent feature loss is not acceptable.

### 4. Go Workflow Authoring And Validation

Port the remaining Python-only workflow authoring behavior into Go:

- `workflow validate`;
- workflow graph/plan output needed by current docs and examples;
- workflow generator catalog and shape rendering;
- workflow upgrade behavior that remains accepted;
- front-matter schema validation for publishable artifacts;
- docs/link or fixture validation that protects examples.

The Go implementation must preserve JSON-only workflow config and current
validation semantics unless the RFC acceptance decision names an explicit
behavior change.

### 5. Test Suite Cutover

Replace pytest as a required development and CI surface. Coverage must move
to:

- Go unit tests for packages and command handlers;
- Go integration tests for daemon RPC, MCP, repository registration,
  workflow lifecycle, recovery, artifact publication, and web routes;
- shell smoke tests only where process-level packaging behavior is the point;
- optional browser tests for frontend behavior if the local web UI keeps
  interactive islands.

The migration should preserve behavioral coverage, not line-for-line test
files. Deleted Python tests must be paired with Go tests or an explicit
retirement reason in the ledger.

### 6. Packaging, Release, And Install Cutover

The release artifact becomes Go-first:

- `striatum` and `striatumd` are Go-built binaries or one combined binary with
  subcommands;
- CI builds and tests Go artifacts without creating a Python virtual
  environment;
- README/getting-started instructions use Go install, release archive, or
  local binary paths;
- package smoke tests verify the Go distribution and embedded assets;
- version metadata has one authoritative source.

The Python distribution name is retired. If the project needs a transition
period, the deprecation notice must be a release artifact, not active runtime
code.

### 7. Documentation And Vocabulary Cleanup

Current docs must describe a Go-only product. At minimum:

- `README.md`, `docs/SPEC.md`, `docs/GETTING_STARTED.md`,
  `docs/CLI_REFERENCE.md`, `docs/HOW_TO_AGENT.md`, `docs/HOW_TO_HUMAN.md`,
  `docs/ROADMAP.md`, `docs/operator/BRIEF.md`, skill templates, plugin
  templates, and smoke/runbooks stop instructing Python setup;
- RFC 0068 and RFC 0070 get status notes naming this RFC as the successor for
  the Python CLI/web carve-out if accepted;
- old decisions that chose Python for V1 are marked superseded rather than
  deleted;
- examples and workflows stop invoking Python except as a target-repository
  command supplied by an operator workflow.

The strict closure check should distinguish Striatum product references from
target-repository examples. A workflow may still orchestrate a target project
that happens to use Python; Striatum itself must not require Python.

### 8. Guardrails

Add architecture tests or CI checks that fail when active Striatum surfaces
reintroduce Python:

```text
rg --files | rg '(^src/.*\.py$|^tests/.*\.py$|\.pyi$|pyproject\.toml$)'
rg -i '\b(pytest|mypy|ruff|pip install|python3? -m striatum|venv)\b'
```

The exact checks should allow generated third-party frontend assets only when
they are not Striatum Python code or instructions. Any historical-provenance
allowlist must be explicit and reviewed; the default target is no active
Python product surface.

## Acceptance Criteria

- The repository builds a usable `striatum`/`striatumd` Go distribution from a
  clean checkout without Python.
- No tracked Striatum source or test file is Python.
- No CI, smoke, release, or packaging path creates a Python virtual
  environment or invokes pytest/mypy/ruff/pip for Striatum itself.
- Operator docs and agent-facing skills do not tell users to install or run
  Striatum through Python.
- Current accepted CLI, MCP, daemon, workflow, recovery, artifact, doctor, and
  web behavior is covered by Go or shell/browser tests, or retired by an
  explicit release decision.
- Daemon-owned PostgreSQL remains the only live-state substrate and the Go
  daemon remains the only daemon core.
- Python daemon, Python MCP, repo-local SQLite, and legacy local-state code do
  not return as transition bridges.
- `go test ./...` passes, plus the replacement aggregate validation command
  defined by this RFC's implementation workflow.
- The temporary Python-trace guardrail is retired after final deletion;
  historical Python references live only in archived/provenance material.

## Implementation Plan

1. **Design workflow.** Run a three-lane audit workflow that inventories every
   Python trace, maps behavior to Go replacements or retirements, and produces
   a cutover ledger.
2. **CLI/runtime scaffold.** Make the Go binary expose the command families
   needed by current smoke tests, routing daemon-owned behavior through daemon
   RPC.
3. **Workflow-authoring port.** Port validation, generation, graph/plan, and
   artifact schema behavior used by accepted examples and docs.
4. **Web/service port.** Move loopback service and web routes to Go or retire
   specific routes with docs and tests.
5. **Test migration.** Convert focused Python tests to Go integration tests and
   remove broad Python fixtures after parity gates pass.
6. **Packaging and docs.** Replace Python packaging, install docs, skills, and
   CI with Go-only release guidance.
7. **Deletion gate.** Delete remaining Python files and add guardrails that
   keep them gone.
8. **Aggregate validation.** Run the Go test suite, package smoke, workflow
   validation replacement, and web smoke if retained.

## Risks

- The Python test suite currently encodes many subtle workflow and operator
  invariants. A mechanical delete would lose coverage.
- Web UI behavior may depend on Python template/resource helpers that need a
  clean Go equivalent before packaging can be trusted.
- Existing docs contain historical Python decisions. Removing every textual
  mention may reduce provenance clarity unless supersession notes are handled
  carefully.
- Go CLI parity may expose mismatches that were hidden behind Python client
  helpers.
- Third-party target repositories may be Python projects. Guardrails must not
  forbid workflows from running target-repository Python commands when the
  target project declares them.

## Open Questions

- Should historical RFCs and decision rows keep the word "Python" as
  provenance, or should active docs be rewritten to avoid the term entirely
  in repository HEAD?
- Should the Go distribution keep separate `striatum` and `striatumd`
  binaries, or should `striatumd` become a compatibility alias for
  `striatum daemon start`?
- Which local web routes are still worth porting before Python deletion, and
  which should be retired?
- Is a temporary Python package deprecation release allowed, or should the
  next release immediately stop publishing Python artifacts?
- What is the replacement aggregate validation command after pytest is gone?

## Domain Modeling

This RFC changes implementation and packaging boundaries, not the workflow
domain model. The daemon aggregate, repository identity, workflow snapshot,
artifact, session, lease, event, and capability concepts remain the same. The
primary domain clarification is that "client surface" no longer implies a
Python implementation; Go becomes the only Striatum runtime implementation in
active HEAD. This follows the boundary-clarification guidance in
[`docs/DDD.md § Adding to the model`](../reference/domain-driven-design.md#adding-to-the-model).
