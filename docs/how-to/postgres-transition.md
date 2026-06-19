# PostgreSQL Transition Runbook

This runbook is the current operator path for Striatum's PostgreSQL-first
state model. The daemon is a hard prerequisite for every stateful Striatum
verb; `.striatum/` next to a target repository is operational scratch, not
live workflow state.

Read [daemon-runbook.md](daemon-runbook.md) for daemon lifecycle details,
[HOW_TO_HUMAN.md](how-to-human.md) for the operator playbook, and
[SPEC.md - State Store](../reference/spec.md#state-store) for the product
contract.

## What Changed

- RFC 0033 moved daemon-global state (repository registry, capability tokens,
  audit chain, scheduler cursors, and RPC request log) into operator-owned
  PostgreSQL.
- D094 / RFC 0043 moved per-repository workflow state into that same daemon
  PostgreSQL substrate under a `repository_id` scope. The direct
  `--no-daemon` path is retired.
- RFC 0078 made Striatum Go-only. The current binaries are `striatum`,
  `striatumd`, and `striatum-supervisor-helper`.

Current PostgreSQL schema sources live under `go/pkg/db/sql/`; owner-applied
authority bundles live under `go/pkg/db/sql/owner/`.

## Prerequisites

- A PostgreSQL service the operator can connect to. Striatum does not manage
  the PostgreSQL server lifecycle.
- A runtime DSN for `striatumd`, normally in `STRIATUM_DAEMON_DB_URL` or
  `~/.config/striatum/daemon.toml`.
- An owner/admin DSN for bootstrap and upgrades that need DDL privileges,
  passed with `--admin-url` / `--owner-url` or
  `STRIATUM_DAEMON_ADMIN_DB_URL`.
- Striatum's Go binaries installed from a release archive or a contributor
  checkout with `make install`.

For an existing pre-D094 target repository, do not import old writable SQLite
state. Archive or remove `.striatum/retired-local-state` before registering
the repository with `striatum repo add --init`.

## Provision PostgreSQL Roles

Use your normal PostgreSQL administration path to create the database and a
runtime login role. This is one common local shape:

```bash
sudo -u postgres psql <<'SQL'
CREATE DATABASE striatum_daemon;
CREATE ROLE striatumd_rw WITH LOGIN PASSWORD '<runtime-password>';
GRANT CONNECT ON DATABASE striatum_daemon TO striatumd_rw;
SQL
```

Use any admin connection that can create the database/role and run owner DDL:
Postgres.app, Homebrew PostgreSQL, a managed database admin console, or a local
superuser are all fine. The Striatum requirement is the privilege boundary, not
the specific `sudo -u postgres` command.

Configure the daemon runtime DSN to use the runtime role, for example:

```bash
export STRIATUM_DAEMON_DB_URL='postgres://striatumd_rw:<runtime-password>@127.0.0.1:5432/striatum_daemon?sslmode=disable'
```

Keep an owner/admin DSN available for migrations and owner bundles:

```bash
export STRIATUM_DAEMON_ADMIN_DB_URL='postgresql:///striatum_daemon'
```

On PostgreSQL 16+, a non-superuser owner that rotates the runtime role's
password needs admin membership on the runtime role:

```bash
sudo -u postgres psql -d striatum_daemon <<'SQL'
GRANT striatumd_rw TO <owner-role> WITH ADMIN OPTION, INHERIT FALSE, SET FALSE;
SQL
```

Superuser owners do not need that grant. Single-role local deployments skip
runtime-password rotation and are useful for development, but the two-role
posture is the production-local shape.

## Configure The Daemon DSN

The daemon resolves its runtime PostgreSQL URL in this order:

1. `--postgres-url <url>` where a command accepts it.
2. `STRIATUM_DAEMON_DB_URL`.
3. `postgres_url` in `~/.config/striatum/daemon.toml`.

`striatum daemon install --no-start` renders the systemd user unit and
scaffolds `daemon.toml` if it is missing. It does not provision PostgreSQL.

```bash
striatum daemon install --no-start
```

## Apply Migrations And Owner Bundles

Apply schema migrations before first start or after upgrades that add new
migrations:

```bash
striatum daemon migrate-db --admin-url "$STRIATUM_DAEMON_ADMIN_DB_URL" --json
```

Apply owner bundles for SECURITY DEFINER functions, authority stamps, and
read/write privilege closure:

```bash
striatum daemon owner-ddl apply --owner-url "$STRIATUM_DAEMON_ADMIN_DB_URL" --json
```

Both commands are local bootstrap helpers, not daemon RPC calls. `owner-ddl
apply` is also the documented grant-drift repair action because it reasserts
the protected read/write revokes after applying bundles.

Regular runtime migrations after schema version 26 must stay applicable by the
daemon runtime role: do not add owner-table `ALTER TABLE` or `DROP TABLE` DDL
against existing `striatumd.*` tables to those migrations. Put owner-table
shape changes, SECURITY DEFINER function updates, and grant/revoke repair in
owner bundles or owner/admin helpers, then run them with the owner/admin DSN.
The Go migration tests enforce this forward boundary while preserving the
hashes of deployed historical migrations.

A runtime migration may, however, `ALTER` a table the runtime role itself
*owns*. A table created by an early migration that ran under the owner role
(e.g. `striatumd.job_recovery_state`, migration 0020) is owned by the bootstrap
role, not the runtime role, so a later runtime `ALTER` of it would crash-loop
the daemon (`must be owner of table …`, SQLSTATE 42501). Owner bundle 0018
transfers the pre-split runtime-data cohort (recovery, barrier, conversation,
interrogation, dissent, workspace, and spawn-grant tables) to `striatumd_rw`
ownership — and grants the runtime role `CREATE` on the `striatumd` schema,
which owning those tables requires — so the runtime role can apply those
`ALTER`s without the owner-only failure. Because the cohort transfer is an owner
bundle, on any upgrade that adds a runtime `ALTER` of a transferred table you
must run `daemon owner-ddl apply` (with the owner DSN) **before** starting the
daemon, so the ownership is already in place when the daemon's startup migrate
runs as the runtime role. The owner-DDL guard test is ownership-aware: it
permits a runtime `ALTER` only of tables an owner bundle has actually
transferred to `striatumd_rw`, never via a name-based allowlist escape.

If a migration fails with an ownership or permission error during daemon
startup, stop the service, rerun `daemon migrate-db` with an owner/admin DSN,
then start the service again:

```bash
systemctl --user stop striatumd
striatum daemon migrate-db --admin-url "$STRIATUM_DAEMON_ADMIN_DB_URL" --json
systemctl --user start striatumd
```

## Start And Check The Daemon

On a systemd user-service host:

```bash
systemctl --user start striatumd
striatum daemon status
```

On a host without systemd user services, run the daemon in the foreground:

```bash
striatumd -socket "${XDG_RUNTIME_DIR}/striatum/daemon-go.sock"
```

Then, in another shell:

```bash
striatum doctor --verbose --json
```

`striatum daemon status` reports the local service/runtime layout and folds in
the read-only `striatum doctor` result when the daemon is reachable.

The first successful daemon boot creates the runtime `client-token` under the
daemon runtime directory. Treat that file as operator-local secret material.

## Register A Target Repository

```bash
TARGET_REPO=/path/to/target/repo
striatum repo add "$TARGET_REPO" --init --json
striatum --repo "$TARGET_REPO" skills install --profile claude_code --json
```

`repo add --init` registers the repository in daemon PostgreSQL, creates
`.striatum/scratch`, and adds `.striatum/` to `.gitignore`. `skills install`
writes agent-side assets and does not create workflow state.

Verify the registered repository:

```bash
striatum --repo "$TARGET_REPO" status --json
striatum --repo "$TARGET_REPO" doctor --verbose --json
striatum --repo "$TARGET_REPO" list runs --json
```

## Dev-Local PostgreSQL Profile

For disposable contributor smoke tests, the repo includes
`examples/dev-postgres/docker-compose.yml`:

```bash
cd examples/dev-postgres
docker compose up -d
export STRIATUM_DAEMON_DB_URL=postgresql://striatum_admin:striatum_dev_password@127.0.0.1:5432/striatum_daemon
striatum daemon migrate-db --json
striatum daemon owner-ddl apply --json
```

Do not treat the compose profile as the production path; it exists only to
make local development reproducible without editing a system PostgreSQL
cluster.

## Retired Commands

These commands are not part of the current Go CLI:

- `striatum daemon doctor`
- `striatum daemon service install|start|status`
- `striatum daemon start`
- `striatum daemon migrate --from sqlite --to pg`
- `striatum daemon migrate-repo-local --from sqlite --to pg`
- `striatum serve --web`

Use `striatum daemon install`, `systemctl --user start|stop|restart
striatumd`, foreground `striatumd`, `striatum daemon migrate-db`,
`striatum daemon owner-ddl apply`, `striatum daemon status`, and
`striatum doctor` instead.

## Exit Codes

| Code | Meaning | Operator remediation |
|---:|---|---|
| 10 | Daemon RPC transport, handshake, or version-skew refusal. | Reconcile client and daemon versions; check `striatum daemon status` and `striatum doctor`. |
| 11 | `daemon_unreachable`. | Start `striatumd` with systemd or the foreground command and check the socket path in stderr. |
| 12 | `repo_not_migrated`. | Archive/remove legacy SQLite files if present, then register with `striatum repo add --init`. |

See [CLI_REFERENCE.md - Stable exit codes](../reference/cli-reference.md#stable-exit-codes)
for the full closed list.

## Rollback And Inspection Limits

- Archive legacy `.striatum/retired-local-state` before registration if you may
  need manual inspection later.
- Current Striatum does not perform operator SQLite imports.
- Once Postgres-side registration exists, deleting it is data deletion. Normal
  recovery is restoring PostgreSQL from backup/PITR and restoring the target
  repository from the matching Git snapshot.
- `.striatum/` remains scratch only; workflow state after registration lives
  in PostgreSQL.

## See Also

- [daemon-runbook.md](daemon-runbook.md) - daemon install, lifecycle, runtime
  layout, logs, and troubleshooting.
- [GETTING_STARTED.md](../tutorials/getting-started.md) - first-15-minutes
  walkthrough.
- [HOW_TO_HUMAN.md](how-to-human.md) - operator playbook.
- [CLI_REFERENCE.md](../reference/cli-reference.md) - CLI verbs and exit codes.
- [SPEC.md - State Store](../reference/spec.md#state-store) and
  [SPEC.md - CLI](../reference/spec.md#cli) - implementation contract.
- [DECISION_LOG.md - D094](../decisions/decision-log.md) - decision that made
  PostgreSQL the sole live workflow substrate.
