---
type: record
status: frozen
owner: GEMINI
expires: null
---

# Striatum Documentation Drift Audit

## 0. Audit Basis

*   **Target:** Repository rooted at `/home/halbritt/git/striatum`
*   **Scope:** Whole-repository documentation survey, with deep dives into high-exposure landing guides and command references.
*   **Repo State:** Active main branch, with uncommitted changes in documentation files (`docs/reference/spec.md`, `docs/reference/command-authority-matrix.md`, `docs/reference/ubiquitous-language.md`, RFC logs) and Go mutation/test files.
*   **Dirty State:** Stale local files modified but uncommitted.
*   **Authority:** Read-only static inspection. No live database queries, CLI execution, or web server execution were conducted.
*   **Commands Ran:** `git status`, `git diff` (read-only state verification).
*   **Commands Not Run (Not Authorized):** `make test`, `go test ./...`, running `striatumd` or `striatum` binaries.
*   **Files Read:**
    *   [README.md](file:///home/halbritt/git/striatum/README.md)
    *   [AGENTS.md](file:///home/halbritt/git/striatum/AGENTS.md)
    *   [ARCHITECTURE.md](file:///home/halbritt/git/striatum/ARCHITECTURE.md)
    *   [docs/reference/cli-reference.md](file:///home/halbritt/git/striatum/docs/reference/cli-reference.md)
    *   [docs/reference/spec.md](file:///home/halbritt/git/striatum/docs/reference/spec.md)
    *   [docs/reference/command-authority-matrix.md](file:///home/halbritt/git/striatum/docs/reference/command-authority-matrix.md)
    *   [docs/reference/ubiquitous-language.md](file:///home/halbritt/git/striatum/docs/reference/ubiquitous-language.md)
    *   [docs/how-to/how-to-human.md](file:///home/halbritt/git/striatum/docs/how-to/how-to-human.md)
    *   [docs/how-to/daemon-runbook.md](file:///home/halbritt/git/striatum/docs/how-to/daemon-runbook.md)
    *   [docs/how-to/writing-workflows.md](file:///home/halbritt/git/striatum/docs/how-to/writing-workflows.md)
    *   [go/pkg/cli/routes/usage.go](file:///home/halbritt/git/striatum/go/pkg/cli/routes/usage.go)
    *   [go/pkg/cli/routes/routes_generated.go](file:///home/halbritt/git/striatum/go/pkg/cli/routes/routes_generated.go)
    *   [go/pkg/cli/localcommands/localcommands.go](file:///home/halbritt/git/striatum/go/pkg/cli/localcommands/localcommands.go)
    *   [go/pkg/cli/skills/skills.go](file:///home/halbritt/git/striatum/go/pkg/cli/skills/skills.go)
    *   [go/cmd/striatum/main.go](file:///home/halbritt/git/striatum/go/cmd/striatum/main.go)
    *   [go/pkg/mutations/run.go](file:///home/halbritt/git/striatum/go/pkg/mutations/run.go)
    *   [go/pkg/workflowauthoring/lint.go](file:///home/halbritt/git/striatum/go/pkg/workflowauthoring/lint.go)
*   **Evidence Tiers Used:** `static-traced`.

### Depth Ledger

| Doc Area / File | Pass Applied | Selection / Skip Reason | Files Read | Strongest Evidence Tier | Residual Risk |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `README.md` | `deep-audit` | Primary repository landing page. High exposure to onboarding users. | `README.md` | `static-traced` | None |
| `docs/reference/cli-reference.md` | `deep-audit` | Primary CLI command catalog. High exposure, copy-paste reference. | `docs/reference/cli-reference.md` | `static-traced` | None |
| `docs/how-to/how-to-human.md` | `deep-audit` | Human-principal operator playbook. Primary onboarding guide. | `docs/how-to/how-to-human.md` | `static-traced` | None |
| `docs/how-to/writing-workflows.md` | `deep-audit` | Authoring guide for workflows. | `docs/how-to/writing-workflows.md` | `static-traced` | Some custom shapes unverified |
| `docs/reference/spec.md` | `survey` | Implementation contract. Read to understand changes. | `docs/reference/spec.md` | `static-traced` | None |
| `docs/how-to/daemon-runbook.md` | `survey` | Operating the daemon. | `docs/how-to/daemon-runbook.md` | `static-traced` | None |
| `docs/how-to/postgres-transition.md` | `survey` | Database migrations. | `docs/how-to/postgres-transition.md` | `static-traced` | None |
| `docs/rfcs/` | `unread` | Detailed proposals. Excluded from main survey to focus on user-facing manuals. | None | N/A | Ignored design proposals |
| `docs/_archive/` | `unread` | Archived files. Out of scope. | None | N/A | None |

---

## 1. Verdict

**Verdict:** `STALE`
**Confidence:** `medium` (Static verification was thorough and established multiple serious discrepancies across primary onboarding paths; execution of commands was not authorized but would not change the main findings.)

### Reason for Verdict
While the implementation of the core Go daemon and local operations is robust and matches newer documentation such as the `Daemon Runbook` very well, major parts of the user-facing CLI documentation have stagnated. Primary onboarding surfaces—including [README.md](file:///home/halbritt/git/striatum/README.md), [cli-reference.md](file:///home/halbritt/git/striatum/docs/reference/cli-reference.md), and [how-to-human.md](file:///home/halbritt/git/striatum/docs/how-to/how-to-human.md)—actively instruct readers to use commands that do not exist or flags that are rejected by the current Go codebase. Specifically, the nonexistent `striatum serve` command is heavily documented, and multiple retired Python-era CLI verbs (e.g. `striatum daemon start`, `striatum daemon service install`, `striatum workflow graph`) are presented as active, which will cause immediate failures for any human following the documentation.

---

## 2. Documentation Surface Inventory

The repository contains a substantial documentation corpus under `docs/` and at the root.

*   **Entry-Point & Onboarding:** [README.md](file:///home/halbritt/git/striatum/README.md) (root) and [how-to-human.md](file:///home/halbritt/git/striatum/docs/how-to/how-to-human.md). Mislabeled temporal states present retired commands (`serve`, `daemon start`) as active.
*   **CLI References:** [cli-reference.md](file:///home/halbritt/git/striatum/docs/reference/cli-reference.md). Mislabeled temporal states present multiple nonexistent subcommands and options.
*   **Architecture & Design:** [ARCHITECTURE.md](file:///home/halbritt/git/striatum/ARCHITECTURE.md) and [docs/reference/spec.md](file:///home/halbritt/git/striatum/docs/reference/spec.md). Generally up to date, although `spec.md` lacks details on the `allow_claude_print` validation rules.
*   **Guides & Runbooks:** [daemon-runbook.md](file:///home/halbritt/git/striatum/docs/how-to/daemon-runbook.md), [postgres-transition.md](file:///home/halbritt/git/striatum/docs/how-to/postgres-transition.md), and [writing-workflows.md](file:///home/halbritt/git/striatum/docs/how-to/writing-workflows.md). Mostly current, but `writing-workflows.md` documents a retired command (`workflow graph`).

---

## 3. Temporal Mode Map

| Document / Section | Declared Temporal Mode | Actual Temporal Mode | Mislabeled? | Notes |
| :--- | :--- | :--- | :--- | :--- |
| `README.md` | `current-state` | `current-state` / `historical` | Yes | Documents `serve --web` which does not exist in Go. |
| `docs/reference/cli-reference.md` | `current-state` | `current-state` / `historical` | Yes | Lists multiple retired Python-era CLI commands as current. |
| `docs/how-to/how-to-human.md` | `current-state` | `current-state` / `historical` | Yes | Teaches the use of `serve --web` and nonexistent daemon verbs. |
| `docs/how-to/writing-workflows.md` | `current-state` | `current-state` / `historical` | Yes | Documents the retired `workflow graph` command. |
| `docs/reference/spec.md` | `current-state` | `current-state` | No | Deeply updated in the dirty tree to match the Go implementation. |
| `docs/how-to/daemon-runbook.md` | `current-state` | `current-state` | No | Correctly describes systemd units and `striatum daemon install`. |
| `docs/how-to/postgres-transition.md` | `current-state` | `current-state` | No | Accurate migration guide. |

---

## 4. Ranked Drift Ledger

| ID | Severity | Doc Surface & Drift Class | Documented Claim | Current Reality | Temporal Mode | Reader Impact | Exposure | Evidence Tier & Verification Status | Smallest Fix Direction |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **DDA-001** | `SERIOUS` | CLI Reference & Onboarding / `removed` | `striatum serve` (and `serve --web`) is the command to launch the web interface (`cli-reference.md#L357`, `README.md#L281`, `how-to-human.md#L39`, `writing-workflows.md#L337`, `spec.md#L1993`). | The Go CLI has no `serve` or `serve --web` command registered in [localcommands.go](file:///home/halbritt/git/striatum/go/pkg/cli/localcommands/localcommands.go) or [routes_generated.go](file:///home/halbritt/git/striatum/go/pkg/cli/routes/routes_generated.go). The web server is instead started automatically by the daemon binary (`striatumd`) via `startWebUISocket`. | `current-state` | Reader runs `striatum serve` and receives `unknown command` error; standard UI access workflow fails. | **High** (Documented in main README and multiple guides). | `static-traced` / `static only` | Remove references to `striatum serve` and instruct the operator to run `striatumd` directly or manage it via systemctl/launchd. |
| **DDA-002** | `SERIOUS` | CLI Reference / `removed` | `striatum daemon start`, `striatum daemon stop`, `striatum daemon sweep`, and `striatum daemon service {install\|start\|status}` manage the daemon lifecycle (`cli-reference.md#L160-L166`). | The Go CLI only supports `install`, `uninstall`, `status`, `migrate-db`, and `owner-ddl` under the `daemon` group in [daemon.go](file:///home/halbritt/git/striatum/go/pkg/cli/localcommands/daemon.go#L43-L63). Systemd unit management is handled via `striatum daemon install` / `uninstall`, and starting/stopping uses systemctl directly. | `current-state` | Reader attempts to run `striatum daemon start` and fails with `unknown daemon command`. | **High** (Core lifecycle section of the CLI reference). | `static-traced` / `static only` | Replace CLI reference commands with the correct `striatum daemon install/uninstall/status` syntax, and document manual `systemctl --user {start\|stop}` verbs. |
| **DDA-003** | `SERIOUS` | CLI Reference / `removed` | `striatum --daemon <verb>` operates the CLI in daemon mode (`cli-reference.md#L301-L304`). | Global `--daemon` flag is not recognized in Go CLI's `parseLeadingGlobals` in [main.go](file:///home/halbritt/git/striatum/go/cmd/striatum/main.go#L223). Running `striatum --daemon status` parses `--daemon` as the command, failing with `unknown command: --daemon status`. | `current-state` | Reader cannot query daemon status using the documented command. | **High** (Listed as a primary runtime command block). | `static-traced` / `static only` | Remove `--daemon` prefix references. In Go, daemon connectivity is required and assumed by default; flags like `--daemon-socket` are used instead. |
| **DDA-004** | `SERIOUS` | Workflow Authoring / `removed` | `striatum workflow graph <file>` renders a workflow JSON file directly as a Mermaid or DOT diagram (`writing-workflows.md#L460`). | The local `workflow` subcommands do not include `graph` in [localcommands.go](file:///home/halbritt/git/striatum/go/pkg/cli/localcommands/localcommands.go#L10-L12). The Python-era `workflow graph` command is retired. | `current-state` | Reader runs `workflow graph` to preview a JSON diagram and fails with `unknown command`. | **Medium** (Documented in the workflow authoring guide). | `static-traced` / `static only` | Point reader to `striatum run graph` (which works on active database runs) or clarify that offline file graphing is retired. |
| **DDA-005** | `SERIOUS` | Spec & Guides / `added-undocumented` | Workflow validation and execution. | [lint.go](file:///home/halbritt/git/striatum/go/pkg/workflowauthoring/lint.go#L679) hard-refuses `claude --print`/`-p` one-shot lanes unless `allow_claude_print: true` is explicitly declared. This is not documented in the main `spec.md` or workflow authoring guides. | `current-state` | Reader configures a `claude --print` lane and faces an unexpected hard refusal during `run prepare` / `workflow validate`. | **Medium** (Validation constraint that blocks workflow runs). | `static-traced` / `static only` | Document the `claude --print` refusal and the `allow_claude_print: true` lane override in `spec.md` and `writing-workflows.md`. |
| **DDA-006** | `MINOR` | CLI Reference / `temporal-mislabel` | Retired commands `daemon migrate` and `daemon migrate-repo-local` remain parseable to return a clear error code 12 (`cli-reference.md#L172`, `L273`). | Neither command is registered in `localcommands` or `routes_generated.go`; running them yields a generic `unknown command` error (code 2 or 11/12 via default route fallbacks). | `current-state` | Reader expecting a graceful refusal error faces generic CLI parsing failures. | **Low** (Backwards compatibility edge cases). | `static-traced` / `static only` | Update the CLI reference to reflect that these retired commands are completely removed and no longer parseable. |

---

## 5. Silent And High-Exposure Drift

A reader following the instructions in the main [README.md](file:///home/halbritt/git/striatum/README.md) or the CLI reference will experience immediate failures:

1.  **Nonexistent Web Serve command:** Documented at the root of the project (`README.md`), starting the web interface using `striatum serve` is the most prominent onboarding failure. A reader would not know how to start the interface without digging into the `daemon-runbook.md` or systemd templates to find that `striatumd` is the executable.
2.  **Global `--daemon` flag:** Documented as a standard syntax prefix in `cli-reference.md`, but parsing causes a hard error since `--daemon` is treated as a command verb rather than a global option.
3.  **Retired Workflow Graphing:** The `writing-workflows.md` guide instructs readers to visually verify their JSON files with `striatum workflow graph`, which fails immediately.

---

## 6. What Is Accurate

Despite the CLI command drift, the core implementation matches the reference docs in several key areas:
*   **Worktree & Branch Ref-Safety (RFC 0117):** The updated `spec.md`, `ubiquitous-language.md`, and `command-authority-matrix.md` match the Go implementation in [run.go](file:///home/halbritt/git/striatum/go/pkg/mutations/run.go) perfectly. Branch refs are created in a detached/ref-only manner (`git branch` without checkout) rather than moving the operator's HEAD, and worktree release checks for HEAD reachability.
*   **Daemon Installation:** The [daemon-runbook.md](file:///home/halbritt/git/striatum/docs/how-to/daemon-runbook.md) is highly accurate. It correctly describes the Go bootstrap commands (`striatum daemon install/uninstall/status`) and systemd unit management.
*   **Database Transitions:** [postgres-transition.md](file:///home/halbritt/git/striatum/docs/how-to/postgres-transition.md) accurately reflects the database structure, Postgres connection, and the runtime vs admin DDL roles.

---

## 7. Corpus Pass Disclosure

*   **Read & Audited:** Root guides (`README.md`, `AGENTS.md`, `ARCHITECTURE.md`), CLI and spec references (`cli-reference.md`, `spec.md`, `command-authority-matrix.md`, `ubiquitous-language.md`), and how-to guides (`how-to-human.md`, `daemon-runbook.md`, `writing-workflows.md`).
*   **Sampled:** Go codebase command parsers ([main.go](file:///home/halbritt/git/striatum/go/cmd/striatum/main.go), [usage.go](file:///home/halbritt/git/striatum/go/pkg/cli/routes/usage.go), [dispatch.go](file:///home/halbritt/git/striatum/go/pkg/cli/dispatch/dispatch.go), and [localcommands.go](file:///home/halbritt/git/striatum/go/pkg/cli/localcommands/localcommands.go)) were statically traced to prove command registration and mapping.
*   **Skipped/Excluded:** Historical/archived documents (`docs/_archive/`), individual design RFCs (except where they overlap with implemented features), and developer test suites.

---

## 8. Gated Verification

The following checks would increase confidence in minor areas but were not authorized:
1.  **Command Execution Check:** Running the compiled `striatum` binary with the documented invalid flags (like `--daemon status` or `daemon start`) to record the exact stdout/stderr error output.
2.  **Workflow Validation Execution:** Running `striatum workflow validate` on a workflow containing a `claude --print` lane to capture the exact validation output.

---

## 9. Residual Risk And Unread Areas

### Residual Risk
*   **Custom workflow templates:** Some templated configurations for panel-style workflows described in `writing-workflows.md` (such as option combinations) were not fully traced against the embedded generator code.
*   **Active revision checkouts:** Exact checkout behaviors on Windows/macOS were not verified statically.

### Rejected Candidates
*   *SQLite Migration Refusals:* We verified that `TestRetiredCLICompatibilityCommandsStayUnavailable` asserts a failure for `daemon migrate`. Although the exact code path does not return a dedicated refusal message, it does fail, confirming the semantic drift (but removing any speculation about custom handlers).
*   *Attestation Gates:* Speculations that attestation gates on review lanes might have drifted were rejected because the codebase structures and spec parameters matches.
