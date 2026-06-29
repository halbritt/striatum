# RFC 0078 Remaining Work Scaffold

Status: superseded / historical
Date: 2026-05-25
author: operator-claude-opus-4-7-001
Supersedes: the 2026-05-25 six-gate scaffold revision of this file
(`operator-codex-gpt-5.5-001`). The six scaffolding gates ran; this revision
records the *post-scaffold* reality and the narrower remaining path to
acceptance.

Superseded by D134 and RFC 0078 acceptance. This plan is retained as operator
provenance for the deletion run; its `python-trace-*` commands are retired and
are no longer active validation targets.

## Corrected State

The earlier framing ("port the remaining runtime code") overstates the work.
The production runtime is already Go-only:

- `make install|build|lint|typecheck|test|smoke` all delegate to `go/`.
  Python survives only as `legacy-python-*` Makefile targets.
- CI (`.github/workflows/ci.yml`) has **no pytest job** — only Go build/test/
  smoke, the frontend bundle check, and a "guard no active Python release
  path" step.
- The Go web service landed (`go/pkg/webservice`, `webassets`, `websse`,
  `webtest`); `/chat` and `/dogfood` are deliberately **retired** routes,
  guarded by `scripts/guard_rfc0078_web_retirement.sh`.

Acceptance is therefore a **delete-plus-doc-rewrite gated on parity**, not a
porting marathon. `scripts/python_trace_guardrail.sh` is a pure file-existence
+ grep scan with no "ported" ledger: it passes only when the tracked Python
files leave `git ls-files` and the current-guidance docs stop emitting Python
instructions.

Latest `make python-trace-report`: `blocked: 439`, `unclassified: 0`.

| Class | Count | Closure path |
|---|---:|---|
| `active_striatum_python_source` | 201 | delete after Gate A/B/C parity |
| `active_pytest_surface` | 176 | delete by ledger row (Gate D) |
| `active_python_runtime_guidance` | 56 | rewrite (Gate F) |
| `active_python_script` | 5 | port/retire (Gate C + Gate E) |
| `active_python_packaging` | 1 | remove (Gate E) |

The row-level coverage ledger
(`docs/operator/artifacts/rfc-0078-python-test-migration/coverage-ledger/COVERAGE_LEDGER.md`)
is **stale**: it predates the Go web service and still marks many web rows
`blocked` for "no Go service package." Refresh it during Gate D rather than
trusting its `blocked`/`needs_replacement` counts.

## Decided Gaps

Three surfaces carried embedded port-vs-retire decisions. All three are now
decided as bounded engineering tasks; none needs a further product call.

1. **Corpus → port redaction tier, retire the rest.** `corpus.export`,
   `archive.create`, and the historical-dogfood reads/migrate methods are
   already live Go handlers. The only load-bearing gap is redaction:
   `go/pkg/reads/exports.go:231` states redaction-tier compliance "stays in
   the Python handler." Port that tier into Go, then retire the standalone
   Python `src/striatum/corpus/` modules (enumerator, manifest, writer, verify,
   git, types) as superseded.
2. **Installers → port `skills`/`plugin` to Go, retire `scaffold`.**
   `striatum skills install` / `plugin install` are documented live features
   (README, CLI_REFERENCE, GETTING_STARTED, HOW_TO_AGENT; `striatum doctor`
   emits the invocation) with no Go command — they must be ported. Templates
   become Go-embedded assets (`embed.FS`); the `__init__.py` package-data
   markers are deleted, not ported. `scaffold` is only a `--scaffold` flag on
   `init` with no current-doc references and no Go route — retire it.
3. **Generators → port both stragglers to Go.** `routes_generated.go` already
   uses a Go generator (`//go:generate go run ../routergen`). The two Python
   stragglers — `generate_go_rpc_registry.py` (emits `registry_methods.go`) and
   `generate_daemon_method_tables.py` (emits
   `docs/architecture/DAEMON_METHOD_TABLES.md`) — read the same
   `contracts/daemon_methods.json`. Replace them with a Go generator mirroring
   `routergen`, rewire the two `//go:generate` directives, and keep
   `go/pkg/rpc/registry_contract_test.go` as the parity guard.

## Gates And Sequence

Seven gates. A/B/C are independent and parallelizable (disjoint write scopes).
D depends on A/B/C (a test is deleted only after its source is ported or the
row is marked `retire`). E depends on C (the last Python scripts) and on the
already-landed Go release/smoke scripts. F is independent. G is the terminal
acceptance gate and depends on all others.

```
A corpus ┐
B install ┼─→ D test-deletion ─┐
C genrtr ─┘                     ├─→ G final-deletion + acceptance
E packaging ────────────────────┤
F docs-rewrite ─────────────────┘
```

### Gate A — Corpus redaction port + Python corpus retirement
- **Write scope:** `go/pkg/reads/`, `go/pkg/mutations/corpus_migrate.go`,
  then deletion of `src/striatum/corpus/`.
- **Do:** port redaction-tier compliance into `exports.go`; confirm
  `corpus.export`/`evidence.export`/`archive.create` produce redaction-equal
  output to the Python path; delete `src/striatum/corpus/**`.
- **Validate:** `cd go && go test ./pkg/reads ./pkg/mutations ./pkg/blob`.

### Gate B — Installers (skills/plugin port, scaffold retire)
- **Write scope:** new `go/pkg/cli/localcommands` install command(s),
  `go/pkg/...` embedded templates, then deletion of `src/striatum/skills/`,
  `src/striatum/plugins/`, and the `--scaffold` init path.
- **Do:** implement `striatum skills install` / `plugin install` in Go with
  `--profile` parity and version-stamp output that `striatum doctor` already
  expects; embed the 65 template files (drop `__init__.py` markers); remove the
  `scaffold` flag from the Go `init` surface and Python parser/dispatch.
- **Validate:** `cd go && go test ./pkg/cli/... ./cmd/striatum`;
  manual `striatum skills install --profile all` round-trip; `striatum doctor`
  reports a fresh bundle.

### Gate C — Generators to Go
- **Write scope:** `go/pkg/cli/routergen/` (or a sibling tool),
  `go/pkg/rpc/registry.go` directive, `go/pkg/rpc/registry_methods.go`,
  `docs/architecture/DAEMON_METHOD_TABLES.md`, then deletion of
  `scripts/generate_go_rpc_registry.py` and
  `scripts/generate_daemon_method_tables.py`.
- **Do:** emit `registry_methods.go` and the method-tables doc from
  `contracts/daemon_methods.json` via Go; rewire both `//go:generate`
  directives; `go generate ./...` reproduces tracked output byte-for-byte.
- **Validate:** `cd go && go generate ./... && git diff --exit-code` then
  `go test ./pkg/rpc ./pkg/cli/routes`.

### Gate D — Pytest deletion by ledger row
- **Write scope:** `tests/`, plus the coverage ledger.
- **Do:** first refresh the coverage ledger against the current Go tree
  (re-classify the stale web `blocked` rows now that the Go service exists).
  Then delete each `tests/**/*.py` whose row is `covered`, `retire`, or
  `historical_exception`; for any surviving `needs_replacement` row, add the
  named Go/shell/browser test before deleting. Remove `tests/conftest.py`,
  package `__init__.py` markers, and `_harness/` last.
- **Validate:** `cd go && go test ./...`;
  `(cd src/striatum/web/frontend && npm test)`.

### Gate E — Packaging and tooling removal
- **Write scope:** `pyproject.toml`, `Makefile`, remaining `scripts/*.py`.
- **Do:** delete `pyproject.toml`; drop `legacy-python-*` Makefile targets and
  the `release_metadata_check.py` invocation (already replaced by
  `scripts/go_release_metadata_check.sh`); delete `check_wheel_size.py` and
  `check_ui_bundle_size.py` after confirming the Go/`ui-check-bundle` path
  covers bundle-size; `release_metadata_check.py` deletes with the legacy
  target.
- **Validate:** `scripts/go_release_metadata_check.sh`,
  `scripts/go_package_smoke.sh`, `scripts/go_fresh_clone_smoke.sh`, and the CI
  "guard no active Python release path" step.

### Gate F — Current-guidance doc rewrite
- **Write scope:** the 56 `active_python_runtime_guidance` paths (README,
  AGENTS.md, Makefile comments, `docs/*` current guidance, skills/plugins
  templates) — *not* historical/provenance docs, which the guardrail already
  allowlists.
- **Do:** rewrite each flagged line to Go-only install/run/test language;
  preserve target-repository Python examples (those are
  `target_workload_allowed`). Add supersession notes to RFC 0068/0070 and the
  superseded Python-for-V1 decision rows rather than deleting them.
- **Validate:** `make python-trace-report` shows
  `active_python_runtime_guidance: 0`.

### Gate G — Final deletion gate and acceptance
- **Do:** with A–F merged, confirm no tracked Striatum `*.py`/`*.pyi`/
  `pyproject.toml` remain; flip RFC 0078 to accepted; record the decision in
  `docs/DECISION_LOG.md`; bump `VERSION` + promote CHANGELOG `Unreleased`; tag.
- **Validate (aggregate):**
  ```bash
  make python-trace-guardrail           # strict: blocked=0 unclassified=0
  cd go && go test ./... && cd ..
  (cd src/striatum/web/frontend && npm test)
  scripts/go_release_metadata_check.sh
  scripts/go_package_smoke.sh
  scripts/go_fresh_clone_smoke.sh
  ```

## Acceptance Criteria Mapping (RFC 0078)

- "No tracked Striatum source/test file is Python" → Gates A–E + G.
- "No CI/smoke/release path uses pytest/mypy/ruff/pip/venv" → Gate E.
- "Operator docs/skills do not instruct Python" → Gate F.
- "Accepted CLI/MCP/daemon/workflow/recovery/artifact/doctor/web behavior is
  covered by Go or shell/browser tests, or explicitly retired" → Gates A–D +
  the refreshed coverage ledger.
- "PostgreSQL remains sole substrate; no SQLite/Python-daemon bridges" →
  unchanged; guarded by existing tests.
- "`go test ./...` plus the aggregate validation command pass; Python-trace
  guardrail passes" → Gate G.

## Replacement Aggregate Validation Command

The Gate G block above is the candidate replacement for `pytest`. It becomes
the official aggregate command once Gate G passes on tracked HEAD.

## Prior Scaffolds (retained)

The six earlier per-surface scaffolds remain as reference for the gates above:

- `docs/operator/workflows/rfc-0078-go-cli-rpc-router/`
- `docs/operator/workflows/rfc-0078-go-web-service-cutover/`
- `docs/operator/workflows/rfc-0078-workflow-artifact-parity/`
- `docs/operator/workflows/rfc-0078-python-test-migration/`
- `docs/operator/workflows/rfc-0078-go-only-packaging-release/`
- `docs/operator/workflows/rfc-0078-docs-guardrails-final-deletion/`

The umbrella `docs/operator/workflows/rfc-0078-remaining-work.json`
drove their scaffolding. The Gate A/B/C decided-gap work is new and should be
scaffolded as executable workflows (or driven directly) before Gate D begins.
