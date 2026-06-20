# CLI Reference

> This page is a copy-paste reference. It may lag the parser;
> `striatum --help` (and `striatum <verb> --help`) is
> authoritative.

## Core lifecycle

```text
striatum repo add <path> [--init]
striatum skills install [--profile <profile>] [--scope project|user]
striatum plugin install [--profile <profile>] [--scope project|user]
striatum workflow validate
striatum workflow generate
striatum workflow templates list
striatum workflow templates show
striatum run prepare
striatum branch confirm
striatum run start
striatum run drive
striatum operator bootstrap
striatum run summary
striatum archive create
```

`workflow generate` (below) is the way to scaffold a starter workflow tree;
`run prepare` requires an explicit `--workflow <path>` and creates no runtime
default. The generated tree uses a single `local` process lane as a valid
placeholder; edit lanes and job `lane_id` bindings for real agent runs.

`run drive --run-id <id> [--interval 15s]
[--provider-auth-gate auto|required|off] [--once] [--json]` is a local
operator loop over existing daemon RPC methods. It reads `run.detail` and
`list.sessions`, registers and supervises one fresh session per queued
role/lane as the DAG unblocks, adopts already-active matching sessions, and
stops terminal or superseded launched lanes before registering fresh reviewers.
It is not a daemon RPC method and does not call rescue verbs or force non-fresh
sessions. `--provider-auth-gate` is forwarded to `supervise.start`; when a
lane-provider-auth refusal blocks launch, the driver closes the fresh session,
emits a sanitized failure action, and exits nonzero instead of retrying the same
doomed launch.

`run pause [run-id]`, `run resume [run-id]`, and `run cancel [run-id]`
transition an existing run's lifecycle (capability `admin`). `run retry-job
[run-id] [job-id]` requeues a single failed job (capability `admin`).
`run integrate [run-id]` integrates completed run output into the target
branch (capability `apply`). All five are daemon RPC methods; pass the run id
(and job id) positionally or as `--run-id` / `--job-id`.

`operator bootstrap [--operator-docs-root <path>] [--limit N]
[--markdown|--json]`
is a bounded AI-operator cold-start packet. It is a local read-only
composite over existing daemon reads (`repo.resolve`, `status`, `doctor`)
plus local git, `VERSION`, daemon-runtime-path, MCP-endpoint-path, and
`docs/operator/BRIEF.md` probes. It prints frontier state, doctor counts,
operator-brief freshness, skill drift, exact next commands, and a bounded
reading plan without embedding full status, doctor, session, verdict, or
historical run arrays. `--limit` caps expanded lists and is bounded to 20.
The command creates no live state and is not a daemon RPC method.

`workflow templates list [--kind shape|lane_set|role_pack|adversary_pack]`
and `workflow templates show <template_id>` read the bundled local
workflow-template catalog (embedded under `go/pkg/workflowtemplates`).

`workflow generate --shape <shape> [--lane-set <set>] [--workflow-id <id>]
[--scaffold-root <path>] [--artifact-root <path>] [--option key=value ...]`
compiles a concrete workflow tree from that catalog. It previews the planned
repo-relative files by default and writes them only with `--write`; `--json`
emits the structured envelope. The lane set defaults to the `local` fixture
lane (which needs no real lane command), so `workflow generate --shape
conversation --option topic="…"` scaffolds a valid starter out of the box; edit
in real lanes (e.g. `--lane-set author_reviewer` then supply lane commands in
the generated `workflow.json`) before a real run. `--option phases=…`
(`multi_phase`) and shape-specific `--option key=value` values are
accepted as the shape requires.

The Python-era `workflow init`, `workflow lint`, `workflow plan`,
`workflow graph`, `workflow upgrade`, and `workflow templates render-md`
verbs are not part of the current Go CLI (RFC 0078 ported `validate`,
`generate`, and `templates {list,show}`); workflow-authoring lint is enforced
at `validate`/generation time. A fuller CLI_REFERENCE audit against the Go
command surface is tracked separately.

Same-model-pairing lint is enforced by `workflow validate` (refuse unless
`--allow-same-model-pairing`); operational accepted-risk overrides are recorded
through the daemon `workflow accept-risk` / `workflow accepted-risks` commands.

`striatum repo add <path> [--init]` registers a target repository
with the daemon-owned PostgreSQL registry. Pass `--init` for a fresh
target repo so the daemon creates `.striatum/scratch` and adds
`.striatum/` to `.gitignore`.

`striatum skills install --profile <profile>` writes the agent skill
bundle for `claude_code` | `codex` | `agy` | `generic` | `all`.
Use `--scope user` to install once in the user's agent config
directory instead of a project tree.

`striatum plugin install --profile <profile>` writes agent plugin
bundles for `claude_code`, `codex`, or `agy`. Project-scope plugin
installs also write a local marketplace fixture when supported by the
profile.

## Agent / session work loop

```text
striatum register-session
striatum claim-next
striatum ack
striatum heartbeat
striatum release
striatum send
striatum block
striatum publish-artifact
striatum complete
striatum verdict
striatum submit-review
striatum decision record
```

Use `submit-review` for the normal single-call review path: publish the
finding artifact and record the verdict. Use `verdict` when the required
review artifact is already published for the current attempt, such as a
re-claimed review job after lease recovery.

For operator-authored or otherwise unattested recovery of an accepting fresh
review, record an accepting run-level decision with
`--escape-surface review_provenance`, then pass
`--review-provenance-decision-id <decision-id>` to `submit-review` or `verdict`.
The verdict event records the decision id and artifact id as durable override
evidence.

`publish-artifact` validates lease ownership, write scope, path
safety, artifact kind, front matter, and byline. Model-bylined
artifacts require lane evidence: a path-specific supervised
`artifact_observed` event when the wrapper reports one, or the legacy
clean `process_executions` fallback. Operators can explicitly bypass
missing lane evidence with `--allow-no-process-execution
--override-rationale <text>`; the rationale is stored on the artifact
row and in the provenance event.
For review jobs declaring `require_attested_lane: true`, `publish-artifact`
also refuses publication unless the session has an attached lane supervisor.
D185 makes `publish-artifact` and `complete` share the frozen-attempt
write-scope contract: if a mid-run context or workflow edit makes the current
effective scope disagree with the job attempt's frozen scope, both commands must
fail with compatible typed write-scope errors that point to audited recovery
instead of silently widening historical scope.
`decision record --mark-run-compromised` records an accepting decision and
transitions a completed run to `compromised` for provenance invalidation; V1
uses this replacement-run-only path for compromised completed review jobs.

Artifact listing, detail, summary, export, and dashboard JSON includes
`provenance.category` so operator-bylined artifacts can be separated from
attested supervised-lane, daemon auto-finalized, operator-on-behalf,
self-declared operator, recovery-authored, and run-level operator artifacts.

## Mediated repository and process surfaces (RFC 0099)

```text
striatum repo write <session-id> <job-id> <lease-id> <path> --content <text>
striatum repo patch-preview <session-id> <job-id> <lease-id> --patch <text>
striatum repo patch-apply <session-id> <job-id> <lease-id> --patch <text>
striatum process run <session-id> <job-id> <lease-id> [-- <command> ...]
striatum scope-check [--packet-file <packet.json>] [--allowed <path> ...] [--forbidden <path> ...]
striatum work claim-override <session-id> <job-id> <decision-id>
```

These are the RFC 0099 constrained-operator mutation surfaces. `repo write`,
`repo patch-preview`, and `repo patch-apply` (capability `write`) require an
active repo-write job lease and refuse paths outside `write_scope.allowed_paths`
before mutating. `repo write` writes exact UTF-8 `--content` to one
repo-relative path; `repo patch-preview` checks a unified git-style `--patch`
for applyability and write-scope without mutating, and `repo patch-apply`
repeats those checks before applying.

`process run` (capability `write`) executes a command array — passed after `--`
or via `--command-json` — with `--timeout-seconds` (default 300, cap 1800) and
records `process_executions` evidence without storing stdout/stderr. It requires
`capability_requirements.process_execution: true` on the job, or a matching
typed escape decision whose `escape_surface` is `process.run` or `shell_command`.

`scope-check` is a daemon-free, read-only pre-`complete` diagnostic: it flags any
changed path outside `allowed_paths` or inside `forbidden_paths` and exits
nonzero on drift. Read the scope from the active work packet with
`--packet-file`, or paste `--allowed` / `--forbidden` paths directly. It does
not widen or refresh the daemon's frozen attempt scope.

`work claim-override <session-id> <job-id> <decision-id>` (capability `admin`) is
the narrow escape for the fresh-review process-lineage gate: it claims a pending
job for a session authorized by an accepted decision recorded with matching
`--subject-session-id` / `--subject-job-id`. A broad or mismatched decision is
refused; there is no normal-lane `claim-next --force`.

## Worktree (opt-in per lane via `worktree_isolation: per_job`)

```text
striatum worktree create
striatum worktree release
striatum worktree gc [--run-id <id>]
striatum worktree list
```

`worktree release --worktree-id <id>` removes reachable worktrees; `--force`
explicitly discards unreachable worktrees and can retire a missing-on-disk row
when the owning job is terminal. Missing-on-disk forced releases emit
`worktree.force_released` with `missing_on_disk: true`.

`worktree gc` removes terminal job worktrees whose on-disk HEAD is reachable
from the run branch or a `refs/striatum/` pin; it also retires terminal rows
whose path is already missing on disk. Skipped rows are reported with reasons.
Worktrees with no-blob published artifacts that are not present in the worktree
`HEAD` are skipped until the artifact content is durable outside the per-job
worktree.

## Supervisor (RFC 0009)

```text
striatum supervise start <session-id> [--provider-auth-gate auto|required|off]
striatum supervise send
striatum supervise stop
striatum supervise status
striatum supervise list
striatum supervise trajectory
```

`supervise start` owns the lane provider-auth launch gate. The default
`auto` mode runs the Codex smoke only for supported Codex agent-loop lanes under
a distinct configured lane OS user; `required` blocks unsupported providers and
auth-negative smoke results; `off` is the explicit rollback path.
The gate runs before scratch/FIFO creation, session-bound lane token minting,
supervisor rows/events, helper/tmux, or the provider lane process. Blocking
results return a safe `lane_provider_auth` details block. The block can include
the probe name, exit code, stdout/stderr byte counts, and bounded success-signal
state; it never includes raw stdout, stderr, final text, auth paths, provider
account ids, environment values, token material, PTY logs, or tracebacks.
For Codex, zero exit means the lane provider CLI authenticated successfully
even if the bounded `--output-last-message` signal is missing or mismatched;
that condition is diagnostic and does not block launch.

## Conversation (RFC 0086)

```text
striatum conversation open <participant-session-ids>
striatum conversation say <session-id> <conversation-id> <body>
striatum conversation close <session-id> <conversation-id>
striatum conversation list <run-id>
striatum conversation show <conversation-id>
```

The RFC 0086 multi-party conversation surface runs an N-party round-robin
exchange over persistent agent-loop sessions. `conversation open` starts a
conversation across the named participant sessions; `say` posts a turn (both
`write`), `close` ends it (`write`), and `list` / `show` (`read`) enumerate a
run's conversations and replay one transcript.

## Interrogation (RFC 0082 / 0083)

```text
striatum interrogation open <session-id> <target-session-id>
striatum interrogation ask <session-id> <interrogation-id>
striatum interrogation answer <session-id> <interrogation-id>
striatum interrogation close <session-id> <interrogation-id>
striatum interrogation list <run-id>
striatum interrogation show <interrogation-id>
```

Interrogation windows let a consumer session question an upstream
`interrogation target` (a completing job marked `interrogable: true` whose
session stays available for preserved-context questioning). `open` / `ask` /
`answer` / `close` (capability `write`) drive one window; `list` / `show`
(`read`) enumerate a run's interrogations and replay one exchange.

## Trajectory (RFC 0081)

```text
striatum trajectory export <run-id> [profile]
striatum trajectory watch <run-id> [profile]
```

`trajectory export` returns the ordered, profile-filtered conversation
trajectory for a run as replayable/diffable JSONL; `trajectory watch` live-tails
it (both capability `read`). The `dialogue` profile is the curated adjudicator
view (`striatum trajectory export <run-id> dialogue`). This is distinct from
`supervise trajectory`, which reads one supervisor's operator-local PTY log.

## Escalation and principal inbox (RFC 0053 / 0102)

```text
striatum inbox
striatum escalation list
striatum escalation show <escalation-id>
striatum escalation resolve <escalation-id>
```

`escalation list` (capability `read`) reports the open escalations awaiting
human-principal judgment; `striatum inbox` is the alias that routes to the same
`escalation.list` method. `escalation show` (`read`) prints one escalation, and
`escalation resolve` (`admin`) records the principal's disposition. Escalations
are also resolvable through `striatum decision record` (RFC 0053 escalation
artifacts are durable provenance for the principal inbox).

## Dashboard

```text
striatum dashboard
striatum dashboard --all
```

`dashboard --all` (RFC 0028 V1) groups registered repositories and
reports daemon/Postgres-backed per-repository runs, blockers, claimable jobs,
stale leases, and degraded repositories. It uses daemon-owned repository
registration state and requires a daemon `read` capability token from the
runtime `client-token` file.

## Daemon and multi-repo registry (RFC 0028 V1)

```text
striatum daemon install [--no-start] [--print-unit]
striatum daemon status
striatum daemon uninstall
striatum daemon migrate-db [--admin-url <dsn>] [--json]
striatum daemon owner-ddl apply [--owner-url <dsn>] [--json]
striatum daemon token-create <capability>
striatum doctor [--verbose] [--json]
striatum doctor --lane-provider-auth codex [--run-id <id>] [--lane-id <id>] [--timeout 45s] --json
striatumd [daemon-start options]
systemctl --user start|stop|restart|status striatumd
striatum repo add <path> [--init] [--display-name <name>] [--no-migrate] [--apply-blob-creation]
striatum repo list
striatum repo remove <id>
striatum cross-repo list
striatum cross-repo describe <cross_repo_run_id>
striatum cross-repo why <cross_repo_run_id>
striatum cross-repo cancel <cross_repo_run_id> [--reason <text>]
```

`striatum daemon install` renders the systemd user unit, scaffolds
`daemon.toml` when absent, and enables/starts `striatumd` unless `--no-start`
is passed. Use `systemctl --user start|stop|restart striatumd` for service
lifecycle after installation. On hosts without systemd user services, run
`striatumd -socket "${XDG_RUNTIME_DIR}/striatum/daemon-go.sock"` directly as
described in the daemon runbook.

If a system unit (`/etc/systemd/system/striatumd.service`) already owns the
daemon, `striatum daemon install` refuses with exit code 1 and
`daemon_install_system_unit_present` rather than writing a conflicting
user-scope unit. The user-scope unit carries no owner-DB bootstrap environment,
so re-creating it next to a system unit crash-loops the daemon and takes it down
for every session (#509). On such a host, manage the daemon with
`sudo systemctl restart striatumd` and use `--print-unit` to inspect the unit
template without installing it.

`striatumd` is the supported foreground daemon process. Per D094 / RFC 0043 the
daemon is a hard prerequisite for every Striatum verb; CLI verbs without a
reachable daemon refuse with exit code 11 (`daemon_unreachable`) and do not
fall back to direct mode.

On first successful startup, `striatumd` bootstraps a single admin token when
daemon-owned Postgres has no clients and writes a `0600` runtime
`client-token` file. Token secrets are never read from environment variables,
never logged to audit, and never stored in the registry.

Ordinary `doctor` and `doctor --verbose` do not run provider CLIs. The
provider-auth diagnostic is explicit-only because it may touch the network,
spend provider tokens, trigger auth refresh, or hang on an interactive prompt.
Its JSON result is the same private-safe block used by `supervise.start`, with
`raw_output_returned=false`. A Codex zero-exit smoke with a missing or
mismatched bounded success signal reports `success_signal` as `missing`,
`empty`, or `mismatch`; it is not an auth failure.
Authorization uses the closed daemon method capability vocabulary:
`read`, `write`, `review`, `claim`, `apply`, `admin`, `recovery`, and
`surgical_recovery`.

`striatum daemon token-create <capability>` (method `daemon.token.create`,
capability `admin`) mints an additional scoped capability token against the
daemon-owned Postgres. The capability argument is one of the closed vocabulary
above. The secret is returned once and never re-read from the registry.

`repo add` canonicalizes the repository root, refuses
symlink/path-traversal ambiguity, derives a realpath/inode-based
repository identity, and refuses active path re-occupation by a
different identity. Pass `--init` when no `.striatum/` directory
exists; it creates operational scratch only and does not create
`.striatum/retired-local-state`. If a pre-D094 repo-local SQLite source
exists, registration refuses and tells the operator to archive/remove
the legacy SQLite file before registering. `--display-name` sets the
operator-facing repository name (defaults to the directory basename);
`--apply-blob-creation` creates the per-repository blob-storage bucket when
the daemon is configured for blob storage and the bucket is absent.
`--no-migrate` is an accepted compatibility flag — production registration
never imports retired SQLite state.

`repo remove` is idempotent, revokes live repo-scoped
capabilities, preserves audit rows, and never reuses
`repository_id`; re-adding allocates a fresh id.

The resident recovery sweep runs inside `striatumd`. Use the daemon-backed
`recovery auto` family for explicit recovery diagnostics
where applicable; there is no `striatum daemon sweep` CLI command.

RFC 0033 V2 accepts system PostgreSQL as the daemon-owned
storage substrate for daemon-global state. Configure it with
`STRIATUM_DAEMON_DB_URL`, daemon config, or an explicit
`--postgres-url` client surface. The daemon owns schema
migrations and roles, but it does not install, start, stop, or
upgrade PostgreSQL. Bundled, embedded, and Dockerized Postgres
distributions are deferred.

`striatum doctor` is the daemon-backed health check. `doctor --verbose`
includes structured `problem_records` alongside the stable string `problems`
list. `doctor --lane-provider-auth codex [--run-id <id>] [--lane-id <id>]
[--timeout 45s] --json` is an explicit provider-auth smoke for the lane
identity; ordinary `doctor` and `doctor --verbose` never run provider CLIs.
`striatum daemon status` is the local bootstrap summary for unit state and
runtime paths; it folds in read-only doctor information when the daemon is
reachable.

`daemon migrate-db [--admin-url <dsn>] [--json]` (RFC 0079 §5) applies pending
daemon PostgreSQL schema migrations using an owner/admin DSN, so DDL the runtime
role (`striatumd_rw`) cannot perform — e.g. a migration that adds a foreign key
to an owner-held table — is applied before the daemon serves. The admin DSN is
resolved from `--admin-url`, then `STRIATUM_DAEMON_ADMIN_DB_URL`, then the normal
daemon DSN (flag/env/`daemon.toml`) as a fallback for additive migrations the
runtime role can apply itself. This is distinct from the retired SQLite-era
`daemon migrate` below.

`daemon migrate --from sqlite --to pg` and
`daemon migrate-repo-local --from sqlite --to pg --repo <path>` are fully
removed SQLite-era import spellings. They are no longer parseable compatibility
commands; stale automation receives an unknown-command parse failure. Use
`striatum daemon status`, `striatum doctor --verbose --json`, and
`striatum repo add <path> --init` for current registration/cutover diagnostics.
CLI verbs against an unregistered repo refuse with exit code 12
(`repo_not_migrated`) and point operators to archive/remove legacy SQLite
files, then register with `repo add --init`.

RFC 0030/0031 add the daemon V2 RPC and supervision/apply foundation on
top of RFC 0033. The wire envelope is versioned JSON; `daemon.hello`
negotiates envelope/framing, `daemon.describe` publishes the method
registry and `methods_etag`, and incompatible clients refuse with exit
code 10. RFC 0048 completed the production handler-port work in
v1.49.0-v1.55.0: mapped production verbs are daemon/Postgres-backed and
fail closed without daemon reachability, repository registration, and
capability authorization. Legacy SQLite paths remain only for migration
sources, golden fixtures, and explicitly gated subprocess compatibility
tests under `STRIATUM_DAEMON_REQUIRED=0 STRIATUM_TEST_HARNESS=1`.

## Daemon-required runtime

Production commands route through the daemon authority boundary by default.
There is no global `--daemon` flag in the Go CLI; daemon connectivity is
required and assumed.

```text
striatum status [--run-id <id>]
striatum doctor
striatum why <job-id>
striatum dashboard --all
```

The V1 `--no-daemon` flag is retired (D094 / RFC 0043); parsing it
returns the standard argparse "unrecognized arguments" error and exit
code 2. Production mutation and read verbs do not fall back to direct
repo-local mode.

Daemon RPC method capabilities use the closed vocabulary `read`,
`write`, `review`, `claim`, `apply`, `admin`, `recovery`, and
`surgical_recovery`.
`supervise.*` and `apply.*` are daemon RPC routes; sealed apply fails
closed unless a daemon signing key and `apply` capability are present.
The Go daemon rotates the local Ed25519 fallback signing key through the
admin RPC method `daemon.key.rotate`; there is not yet a stable
user-facing `striatum keys` CLI.

RFC 0032 adds cross-repo workflow schema and daemon MCP mutation
capability gating on the PostgreSQL daemon substrate. Cross-repo
workflow files declare `repositories`, `primary_repository`, and
per-job `repository` aliases. The daemon DB records canonical
`cross_repo_run_id` rows under participating repository scopes.
`cross-repo list|describe|why` inspect those daemon records according to
capability scope. `cross-repo cancel` is the `cross_repo.cancel` recovery
route: it cancels non-terminal participant runs through the PG-native
participant runner, skips terminal participants and preparing participants
that never created a local run, and returns `blocked` with diagnostics when a
participant cannot be canceled. Daemon MCP `tools/list` is filtered by each
token's effective capabilities and scope, and `tools/call` re-checks
authorization and audits denials.

RFC 0036 adds no new CLI verb. Regenerate agent-facing MCP guidance with
`striatum skills install` or `striatum plugin install`; chat workflow
generation uses the existing local service and RFC 0034 generator paths.

## Skills (RFC 0015)

```text
striatum skills install
```

## Codex launcher

```text
striatum codex [codex args ...]
```

`striatum codex` launches the `codex` CLI pre-wired to the live daemon MCP
endpoint: it injects `-c mcp_servers.striatum.url=<live endpoint>` (overriding
`~/.codex/config.toml`) and `-c
mcp_servers.striatum.bearer_token_env_var=STRIATUM_MCP_TOKEN`, and sets
`STRIATUM_MCP_TOKEN` in codex's environment from the runtime `client-token`
(never printed). Extra arguments pass through to `codex` unchanged.

## Daemon-Mounted HTTP Service (RFC 0012 / 0013 / 0085)

```text
striatumd -mcp-http-addr 127.0.0.1:0
BASE_URL=$(sed 's#/mcp$##' "$XDG_RUNTIME_DIR/striatum/mcp-http-endpoint")
TOKEN=$(cat "$XDG_RUNTIME_DIR/striatum/client-token")
curl -H "Authorization: Bearer $TOKEN" "$BASE_URL/v1/health"
```

### Web routes (RFC 0013 / 0022 / 0024 / 0038)

`striatumd` mounts the Go web service on the same localhost-only HTTP listener
as daemon MCP. The endpoint file includes `/mcp`; strip that suffix for web
routes. The loopback service requires `Authorization: Bearer <client-token>`;
there is no separate `striatum serve` command and no `serve --web` flag.
Mutations are read-only by default and require
`STRIATUM_DAEMON_WEB_ALLOW_MUTATIONS=1` on the daemon process before startup.

For ordinary browser access without bearer-header tooling, run the optional
read-only tailnet identity listener (`striatumd -web-tailscale` or
`STRIATUM_DAEMON_WEB_TAILSCALE=1`) and point
`tailscale serve --bg unix:$XDG_RUNTIME_DIR/striatum/web-ui.sock` at the
owner-only socket.

| Route | Surface |
| --- | --- |
| `/` | Run list (RFC 0013/0022/0037). |
| `/run?run_id=<id>` | Server-rendered run detail/status page. |
| `/v1/health` | Service health and mutation posture. |
| `/v1/runs` | Daemon `status` JSON. |
| `/v1/runs/<id>` | Daemon `status --run-id` JSON. |
| `/v1/runs/<id>/events` | Server-Sent Events over daemon `run.events`. |
| `/v1/runs/<id>/dashboard` | Dashboard DTO for the run. |
| `/v1/runs/<id>/why?id=<entity>` | Daemon `why` JSON. |
| `/v1/runs/<id>/artifacts` | Artifact list for the run. |
| `/v1/artifacts/<id>/raw` | Raw artifact content. |
| `/workflow-templates` | Workflow template catalog list. |
| `/workflow-templates/<id>` | Workflow template catalog entry. |
| `/workflows/generate/preview` | Workflow generator preview (`POST`). |
| `/workflows/generate` | Workflow generator write (`POST`; requires web mutations). |

## List (read-only enumeration)

```text
striatum list runs
striatum list sessions
striatum list jobs
striatum list artifacts
striatum list workflows
```

`list runs` includes the workflow identity triple for each run:
`workflow_id`, `workflow_version`, and `workflow_snapshot_id`. The web
run list uses the same snapshot identity to display the workflow name
and link back to the workflow detail when the source path is known.

## Inspection and recovery

```text
striatum status
striatum why
striatum work packet-show
striatum doctor
striatum git snapshot
striatum git commit-apply
striatum evidence export
striatum run graph
striatum recovery auto
striatum recovery auto-publish
striatum recovery auto-finalize
striatum recovery stale-leases
striatum recovery requeue-stale
striatum recovery cancel-job
striatum recovery invalidate-job
striatum recovery process-reconcile
striatum recovery resume
striatum recovery reseal
striatum recovery complete-stalled
striatum recovery accept-quarantined
striatum recovery resolve-blocker
striatum recovery prune-debris
striatum recovery quarantine-lane
striatum checkpoint resolve
striatum override-verdict
```

`recovery --help` (and bare `striatum recovery`) lists the full recovery
verb family above; run `striatum recovery <subcommand> --help` for a
subcommand's flags. Each `recovery` verb requires the `recovery` capability.
All recovery verbs are diagnostic/compatibility clients of the same
daemon boundary — they route remediation back through the daemon's own
state machine and never hand-finish a wedged lane (see `AGENTS.md`,
"Do not paste over a broken runner").

### Recovery verb family (when each applies)

The `recovery` verbs are the legitimate way to unstick a wedged lane or run.
The non-obvious entry point is the **"lane finished but the seal failed"**
path: a transient `artifact.publish` / seal failure (e.g. a DB lock timeout,
`SQLSTATE 55P03`, under concurrent runs) can leave the work *done and durable
on disk* but the job in state `blocked`, not requeued. `run retry-job` refuses
(it would exceed `max_attempts`), and `run drive` cannot launch a `blocked`
job — it now reports `cannot advance <job> (cannot_advance_blocked): …` instead
of idling silently. The fix is:

```text
striatum recovery reseal --run-id <run-id> --job-id <job-id>
```

`recovery reseal` moves the job `blocked -> queued` on the SAME attempt so the
driver re-claims and re-seals the already-durable artifact; it does not consume
a retry.

- `recovery reseal <run-id> <job-id>` — re-seal a job whose lane finished but
  whose seal/publish failed transiently; `blocked -> queued`, same attempt.
- `recovery resolve-blocker <blocker-id>` — close a dangling non-escalation,
  non-checkpoint blocker that no completion path cleared.
- `recovery complete-stalled <run-id> <job-id>` — finalize a
  recovery-exhausted job from its already-durable published artifacts when the
  lane is dead and the work is complete.
- `recovery accept-quarantined <run-id> <job-id>` — resolve a quarantined job's
  blocker and mark it canceled-by-operator.
- `recovery prune-debris <run-id>` — tombstone a terminal-debris run's
  unrecoverable artifact debris so `doctor` reports `ok` again.
- `recovery quarantine-lane <run-id> <job-id>` — snapshot a terminal run's
  dirty lane worktree to a durable quarantine ref, then clean the worktree.

`striatum doctor` surfaces this wedge family directly: a non-terminal job in
`running` / `claimed` / `stale_lease` / `blocked` with **no live session** and
no recent progress is reported as a `job_stuck_no_live_session` warning naming
the recovery verb to run. The warning is advisory (it does not flip `doctor`
to a hard red) so it tolerates normal in-flight latency.

`run graph --run-id <id> [--format mermaid|json|dot|ascii]`
renders the workflow graph for an existing run with each node
colored by current job state. Mermaid output appends
`classDef`/`class` lines; JSON adds `current_state`, `attempt`,
and a `latest_verdict` block on review nodes; `ascii` reuses the
dashboard's graph panel renderer (RFC 0016).

`recovery resume` resolves remediated process-adapter blockers with the
preserved lease. For remediated write-scope dirty-path blockers, it validates
that the tree is clean, resolves the blocker, and requeues the same attempt for
a fresh claim before completion. It is also the named recovery destination for
write-scope drift failures: recovery must audit the remediation or override and
must not mutate the historical attempt scope that caused `publish-artifact` or
`complete` to fail.

`git snapshot --json [--ancestry-limit N] [--no-ancestry]` emits the
daemon read-only `git.snapshot` projection for the registered target
repository: local branch, HEAD metadata, dirty counts, changed paths,
and bounded ancestry. It does not fetch, push, commit, read remote URLs,
or include diff hunks or commit bodies.

`git commit-apply <commit-request-path> --confirm --confirm-request-id <id>
--json` emits daemon method `git.commit_apply`. It creates only a local
commit from a `striatum.commit_request.v1` artifact whose
`confirmation_status` is already `operator_confirmed` or `human_confirmed`.
It refuses base-HEAD, branch, or dirty-path mismatches, disables repository
Git hooks for the commit invocation, and never pushes or calls hosted
providers.

`recovery auto` emits the daemon `recovery.sweep` method. The sweep
runs workflow-opt-in `recovery.auto_finalize` before lazy lease expiry,
then the existing stale-lease, process-reconcile, and review-only requeue
recovery pieces where policy allows. Timed-out human checkpoints execute
the configured `recovery_policy.escalation_hook` in live sweeps; dry-runs
report the hook kind without side effects, and hook failures are reported
inside `escalations[]`. `recovery auto-publish` emits the explicit
`recovery.auto_publish_stale_artifacts` method; the deprecated `recovery.auto`
alias is not emitted by the current CLI.
D184 allows the sweep to auto-cancel an abandoned running run after the default
24h threshold when there are no live sessions, no live supervisors or supervised
processes, no active leases, and no progress or durable events in the threshold
window. Any live-work evidence or inconclusive liveness probe fails closed and
leaves the run non-terminal for operator inspection; there is no
`needs_operator` intermediate when the abandonment predicate is proven.

`recovery invalidate-job <job-id> <decision-id>` (RFC 0118, capability
`recovery`) invalidates a completed job's provenance against an accepting
run-level decision and records a durable supersede receipt so a fresh attempt
can re-drive the work.

`work packet-show` (capability `read`) inspects work-packet metadata. Select a
packet with `[packet-id]` or `--message-id` / `--job-id` / `--session-id` /
`--run-id` (at least one selector required); `--limit` caps rows (default 20,
cap 200). It returns metadata and `packet_sha256` only; pass `--raw` to include
`packet_json`, which is omitted by default to avoid leaking task prose.

## Corpus export (RFC 0044 V1 / RFC 0057 contract)

```text
striatum corpus export --since <ref> --out <dir>
```

`corpus export` emits a redacted JSONL bundle of Striatum's durable
provenance (RFCs, decision-log rows, operator reports, run summaries,
audit-chain entries, changelog entries, ubiquitous-language terms,
harness-friction patterns, recent commits) plus a verifying
`manifest.json` with explicit `state_authority` metadata. Re-running over
unchanged inputs produces byte-identical JSONL files and stable per-file
SHA-256s; only `generated_at` varies, and it is excluded from the bundle
digest.
The bundle is operator-triggered local provenance, never streamed to any
external service. Optional consumers (Engram is the first reference under
RFC 0044) may ingest the bundle for retrieval, but Striatum does not call
them at runtime and runs identically when no consumer is configured. The
V2 contract decisions (multi-corpus identity, redaction-tier metadata,
incremental watermarks, optional context-injection policy) are scoped by
[RFC 0057](../rfcs/0057-corpus-contract-v2.md).

## Run archive

```text
striatum archive create --run-id <id> --out <dir>
```

`archive create` is a daemon/Postgres-backed read command that writes a
local archive directory for one run. The V2 archive contains the run row,
workflow snapshot, run-scoped rows, artifact metadata, event metadata, and a
self-verifying `manifest.json`; it does not copy artifact contents,
transcripts, or `.striatum/` scratch. The current Go CLI exposes archive
creation only; local `archive verify` / `archive inspect` verifier commands are
not part of the active command surface.

## Adapter

```text
striatum adapter run
```

`adapter run` is retired outside the explicit legacy test-fixture
compatibility environment. Use daemon-supervised process lanes instead.

## Session lifecycle

```text
striatum session close
```

## Stable exit codes

- `0`: success, including `claim-next` with `no_work`.
- `1`: generic / unhandled runtime error.
- `2`: CLI usage error (argparse).
- `3`: missing run, session, job, message, blocker, artifact,
  verdict, or session target.
- `4`: invalid state transition.
- `5`: lease expiry or ownership mismatch.
- `6`: artifact or write-scope violation.
- `7`: branch confirmation required before work can be claimed.
- `8`: workflow config rejected (also raised by `branch confirm`
  when a requested git operation cannot be performed).
- `9`: state schema is newer than this striatum install supports
  (daemon PostgreSQL in production; legacy SQLite only in fixture paths).
- `10`: daemon RPC transport, handshake, or version-skew refusal.
- `11`: `daemon_unreachable`. The CLI could not reach the daemon
  socket; stderr names the socket path and remediation. No SQLite
  fallback is attempted.
- `12`: `repo_not_migrated`. The target repository is not registered for
  daemon/PostgreSQL state or still has a legacy `.striatum/retired-local-state`;
  stderr and the `--json` hint tell the operator to archive/remove legacy
  SQLite files and register with `repo add --init`.

## See also

- [HOW_TO_HUMAN.md](../how-to/how-to-human.md) — the operator's playbook
  with examples per verb.
- [HOW_TO_AGENT.md](../how-to/how-to-agent.md) — the coding-agent
  companion to the RFC 0015 skill bundle.
- [SPEC.md](spec.md) — the implementation contract.
