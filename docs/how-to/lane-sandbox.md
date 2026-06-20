# Lane Sandbox Runbook — isolate supervised lanes from the daemon's PostgreSQL (#87 / RFC 0096 §2)

This runbook closes the residual half of the supervised-lane trust boundary
([RFC 0096](../rfcs/0096-supervised-lane-trust-boundary.md) V2, [#87](https://github.com/halbritt/striatum/issues/87)):
a supervised lane must not be able to reach the daemon's PostgreSQL directly or
otherwise bypass the artifact API.

Read [HOW_TO_HUMAN.md](how-to-human.md) for the broader operator playbook and
[the PostgreSQL Transition Runbook](postgres-transition.md) for daemon DB
provisioning.

## The gap

The daemon spawns supervised lanes as **its own OS user**. Two leak vectors
follow:

1. **DSN in the lane env / pane** — *already closed.* The supervised-lane
   environment is built from an explicit allowlist
   (`supervisedEnvAllowlistKeys`) that drops every `*DSN*` / `*POSTGRES*` /
   `PG*` / `DATABASE_URL` var, and `STRIATUM_MCP_TOKEN` is now the lane's *own*
   session-bound token, never the shared operator override
   ([#135](https://github.com/halbritt/striatum/issues/135)).
2. **Same-OS-user PostgreSQL reachability** — *the residual this runbook
   closes.* Even with no DSN in its environment, a lane running as the daemon's
   OS user can open the daemon's Postgres over the local unix socket via
   **peer authentication** (`psql "host=/var/run/postgresql dbname=striatumd"`),
   because peer auth keys off the process UID. The live #87 incident was a lane
   that, hitting an artifact-publish conflict, connected directly and tried to
   delete artifact rows and disable an append-only trigger.

Peer-auth reachability is ultimately an **OS / PostgreSQL configuration**
property. The daemon now consumes `STRIATUM_LANE_OS_USER` and launches supervised
lane commands/tmux sessions through `sudo -n -u <lane-user> -- env -i ...`; the
host must still provide the dedicated, unprivileged OS user, noninteractive sudo
permission, target-repository access, and PostgreSQL rejection rule. The lane
user must have **no PostgreSQL role** and be denied by `pg_hba.conf`, so the
lane's only control plane is the MCP surface.

`striatum doctor` reports this posture under `lane_sandbox` and emits a
`lane_pg_reachable` warning until the isolation is adopted (see
[Verify](#verify)). It is a configuration-posture proxy — it does **not** open a
PostgreSQL connection — and is best-effort by design.

## Adopt the PG-less lane OS user

> These steps mutate your machine's OS users and PostgreSQL host-based auth.
> They are an operator adoption step, not performed by the daemon.

### 1. Create a dedicated, login-less lane user

```sh
sudo useradd --system --no-create-home --shell /usr/sbin/nologin striatum-lane
```

### 2. Ensure it has no PostgreSQL role

The lane user must NOT have a Postgres role. Confirm none exists (this should
print nothing):

```sh
sudo -u postgres psql -tAc "SELECT rolname FROM pg_roles WHERE rolname = 'striatum-lane'"
```

If a role exists, drop it: `sudo -u postgres psql -c 'DROP ROLE "striatum-lane"'`.

### 3. Deny the lane user at `pg_hba.conf`

Add **reject** rules for the lane user, ABOVE the broader rules so they are
matched first (path varies by distro, e.g. `/etc/postgresql/16/main/pg_hba.conf`).
Reject both the local socket (peer) path and the loopback TCP path so the lane is
denied over **both** routes the negative gate probes — a host whose default
loopback rule is `host all all 127.0.0.1/32 scram-sha-256` would otherwise let a
lane with a role/password through:

```
# Reject the supervised-lane OS user before any broader rule (#87).
local   all   striatum-lane                    reject
host    all   striatum-lane   127.0.0.1/32     reject
host    all   striatum-lane   ::1/128          reject
```

Reload PostgreSQL **without restarting it** (a restart would drop the live
daemon's connections). Either `sudo systemctl reload postgresql`, or, as the
PostgreSQL superuser over the peer socket,
`sudo -u postgres psql -c "SELECT pg_reload_conf()"`. The daemon's own role is
unaffected — it continues to authenticate as before.

### 4. Permit the daemon to launch lanes as the lane user

Grant the daemon OS user passwordless sudo to only the lane account. For example,
if the daemon runs as `striatumd`, add a sudoers fragment with `visudo`:

```
striatumd ALL=(striatum-lane) NOPASSWD: ALL
```

The daemon uses `sudo -n -u striatum-lane -- env -i ...` when
`STRIATUM_LANE_OS_USER=striatum-lane` is present. If sudo prompts or the rule is
missing, supervised lane launch fails closed instead of silently falling back to
the daemon's OS user.

Also ensure `striatum-lane` can read/write the target repository paths that the
workflow allows. Do not grant access to the daemon's private PostgreSQL socket
directory, daemon config, or `.striatum/` scratch.

### 5. Enable daemon launch enforcement and declare adoption to `doctor`

Set the lane OS user in the daemon's environment so `striatum doctor` confirms
the isolation, stops warning, and supervised lanes actually launch as that user:

```sh
# e.g. in the daemon's systemd unit / environment:
STRIATUM_LANE_OS_USER=striatum-lane
```

`doctor` checks that this user exists and differs from the daemon's user, and
reports `daemon_launch_enforced=true` with
`daemon_launch_mechanism="sudo -n -u striatum-lane"` when the posture is adopted.

On daemon startup, Striatum grants the lane user only the ACL entries needed to
reach the daemon MCP/RPC socket: execute on the socket parent directory, execute
on the daemon runtime directory, and read/write on `daemon-go.sock`. The daemon
still refuses permissive runtime-directory permissions before it binds the
socket; on POSIX ACL systems, the displayed mode may include the ACL mask bit
after startup. This ACL is what lets the dedicated lane user call the daemon
without making the runtime tree public.

### 6. Enable the secure-profile doctor gate

After adopting the lane OS user and protected PostgreSQL socket posture, enable
the RFC 0110 secure-profile gate:

```sh
STRIATUM_SECURITY_PG_SOCKET_HARDENED=1
```

With this flag, `doctor` treats `lane_pg_reachable` as a blocking problem
instead of an advisory warning. This flag does not create the lane user, alter
PostgreSQL socket permissions, or edit `pg_hba.conf`; it only makes the daemon
health check fail closed if the configured posture is still unsafe.

## Verify

```sh
striatum doctor --json | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['lane_sandbox']); print([w for w in d['warnings'] if 'lane_pg' in w])"
```

Adopted state:

- `lane_sandbox.lane_pg_isolated == true`
- `lane_sandbox.pg_socket_hardened == true` when the secure-profile gate is
  enabled
- no `lane_pg_reachable` warning.

To prove the close end-to-end, from a shell running **as the lane user**, a
direct connection must be refused:

```sh
sudo -u striatum-lane psql "host=/var/run/postgresql dbname=striatumd" -c 'SELECT 1'
# expected: FATAL: ... "striatum-lane" ... (pg_hba reject)
```

The automated RFC 0110 L2 negative gate is `make lane-isolation-check`. It
requires explicit probe URLs for both PostgreSQL paths and fails if the lane OS
identity can connect over either one:

```sh
STRIATUM_LANE_OS_USER=striatum-lane \
STRIATUM_LANE_ISOLATION_UNIX_URL="postgres:///striatumd?host=/var/run/postgresql&connect_timeout=2" \
STRIATUM_LANE_ISOLATION_TCP_URL="postgres://localhost/striatumd?connect_timeout=2" \
make lane-isolation-check
```

Set `host=` in the UNIX URL to your cluster's actual `unix_socket_directories`
(`SHOW unix_socket_directories;`). On a default Debian/Ubuntu cluster that is
`/var/run/postgresql`; a deployment that runs PostgreSQL behind a dedicated
protected socket directory points the URL there instead. The gate passes when the
lane identity is rejected over **both** the UNIX socket and loopback TCP, e.g.:

```
ok: lane identity denied over protected UNIX socket
ok: lane identity denied over loopback TCP
T-LANE-ISOLATION-NEG: ok
```

This target is intended for the hardened-profile CI/operator job after the lane
user, protected socket directory, and PostgreSQL `pg_hba.conf` deny rule are in
place. It is not part of the ordinary unit suite because it depends on host OS
users and PostgreSQL listener configuration.

### Wiring the gate into CI (D244)

Per [D244](../decisions/decision-log.md), this gate is **operator-provisioned
hardening, not mandatory-in-CI** — a stock runner has none of the host
provisioning above. It is surfaced via a **conditional CI job**: `make
lane-isolation-check-ci` (wrapping `scripts/check_lane_isolation_ci.sh`) runs the
real negative control **only** when the host advertises provisioning via the
guard variable, and otherwise **skips loudly** (exit 0, printing that the gate
did NOT run) so a green CI never falsely implies the gate executed.

The `lane-isolation-gate` job in `.github/workflows/ci.yml` always appears in the
checks list. To make it actually run on a provisioned (self-hosted) runner, set
these as repo/org **Actions variables** (the workflow maps them into the job env):

```sh
STRIATUM_LANE_ISOLATION_HOST=1
STRIATUM_LANE_OS_USER=striatum-lane
STRIATUM_LANE_ISOLATION_UNIX_URL=postgres:///striatumd?host=/var/run/postgresql&connect_timeout=2
STRIATUM_LANE_ISOLATION_TCP_URL=postgres://localhost/striatumd?connect_timeout=2
```

Once `STRIATUM_LANE_ISOLATION_HOST=1` is set, a missing probe URL / lane user /
sudo rule is a **loud failure** (exit 2), never a silent skip — the
negative-control assertion is never weakened when provisioned. An operator can
run the same wrapper locally: `STRIATUM_LANE_ISOLATION_HOST=1 … make
lane-isolation-check-ci`.

## The privilege split (#201)

Keep the **trusted** half of supervision on the daemon/operator side of the user
boundary, and drop privileges only for the lane's own CLI process:

| Component | Runs as | Gets |
|---|---|---|
| daemon, supervisor bookkeeping, attestation, packet delivery | operator | daemon socket, client token, PostgreSQL authority |
| supervisor helper / attach / liveness probing | operator (daemon side) | reaches the daemon over its own authority |
| the lane CLI process (claude/codex/agy) | the dedicated lane OS user | only its repo/worktree + provider auth + injected MCP endpoint/token |

The lane OS user must **not** be able to read the operator's
`"$XDG_RUNTIME_DIR"/striatum/client-token`, the daemon unix socket, or the daemon
PostgreSQL DSN. The lane authenticates only with its injected, session-bound
`STRIATUM_MCP_TOKEN` over the injected MCP endpoint — never a socket-backed local
`striatum` CLI path, which is invalid for the sandbox user anyway. If a lane is
falling back to a socket-backed CLI, treat that as a provisioning bug, not a
reason to widen the lane user's access.

> Do **not** "fix" a sandboxed lane by making the operator runtime dir, client
> token, daemon socket, or database reachable by the lane user. That re-opens the
> exact boundary [#87](https://github.com/halbritt/striatum/issues/87) closed.

## Provision the lane user's host access

`sudo -u <lane-user> env -i …` starts the lane with an empty environment, so a
fresh host needs each of these provisioned explicitly or the lane stalls at
launch/attest:

- **HOME** — the lane user needs a real, writable home; the daemon sets
  `HOME`/`USER`/`LOGNAME` from the OS user record, but the home directory must
  exist and be owned by the lane user (provider CLIs write config/cache there).
- **Provider auth** — the lane user needs its own provider credentials in its
  HOME (e.g. the claude/codex/agy login/config), since it cannot read the
  operator's. Log in once as the lane user, or copy the minimal credential files
  with lane-user ownership.
- **Repository traversal ACLs** — the lane user must be able to traverse the path
  to the target repo and read it. Grant `r-x` along each parent directory and on
  the repo, e.g. `setfacl -R -m u:<lane-user>:rX <repo>` plus `setfacl -m
  u:<lane-user>:--x` on each ancestor directory.
- **git `safe.directory`** — because the repo is owned by the operator, git run
  as the lane user refuses it as "dubious ownership". Add the repo (and each
  per-job worktree path) to the lane user's `~/.gitconfig` `safe.directory`, or
  set it system-wide.
- **Grounding repo read-only** — a read-only grounding/reference repo only needs
  `r-x` for the lane user; do not grant write.
- **Per-job worktree write** — when the workflow uses `worktree_isolation:
  per_job`, the lane must be able to **write** its per-job worktree under
  `.striatum/worktrees/<id>`. Grant the lane user write on that worktree path
  (e.g. a default ACL on the worktrees parent so new per-job worktrees inherit
  it).
- **Ephemeral MCP config scratch** — the supervisor prepares the lane ACL on
  `.striatum/scratch` (and a default ACL) before launch so non-Codex lanes can
  write their ephemeral MCP config there (#279). No operator step is needed for
  the registered repo; this is listed for completeness.
- **Secondary repositories for cross-repo jobs (#280)** — a job whose prompt
  touches more than the run's registered target repo (e.g. a cross-repo
  hardening job that also writes a sibling repo) needs the **same** host access —
  traversal ACLs, `safe.directory`, and per-job-worktree write — provisioned on
  every secondary repo **before** the run. Striatum does not auto-provision
  secondary repos: there is no structured cross-repo touch-point declaration in
  the workflow schema today, so the daemon cannot know which sibling repos a
  prompt will reach, and the lane silently narrows its scope when it hits a
  missing ACL. Provision each secondary repo the lane must write exactly as the
  primary, or scope the job to a single repo. A future workflow schema for
  declared cross-repo paths could turn this into a pre-dispatch preflight; until
  then it is an operator runbook step.

## Artifact publication and the remote-push boundary (#277)

A supervised lane makes its work durable by **publishing artifacts**, not by
pushing git remotes. Two RFC 0125 mechanisms remove any need for a lane to hold
push credentials:

- `artifact.publish` accepts the body over the MCP envelope (`body_base64`), so a
  lane that cannot even write the operator-owned per-job worktree still publishes
  (#272).
- The **daemon-as-porter** commits each published artifact onto the run branch at
  `work.complete` — past `.gitignore`, from a detached worktree, as the operator
  user — and anchors it under a durable `refs/striatum/…` ref (#278 / #281). The
  provenance is therefore durable **locally**, in the daemon-owned repository,
  the moment the job completes.

A lane must **not** attempt `git push` to a hosted remote: the lane OS user
deliberately has no GitHub credentials (the sandbox boundary forbids copying the
operator's), so a push fails with `could not read Username for
'https://github.com'` after wasting time on `git` / `gh` / `ssh` fallbacks.
Pushing the run branch — or merging it — to a hosted remote is an **operator**
action performed from the operator shell where credentials live; it is
intentionally outside any lane's authority and outside the workflow's definition
of done, which is satisfied by durable local provenance. Workflows should not
instruct lanes to push remotes; when remote publication is required, leave it as
an operator follow-up after the run completes (a credential-safe daemon push
proxy is explicitly out of scope — it would reintroduce hosted-credential reach
into the lane boundary).

## Provider auth preflight (#252)

Cross-user lanes must prove the provider CLI can authenticate as the lane OS
user before a supervised lane is launched. `supervise start` accepts
`--provider-auth-gate auto|required|off`:

- `auto` is the default. It runs the Codex provider-auth smoke for supported
  Codex agent-loop lanes only when `STRIATUM_LANE_OS_USER` names a distinct
  lane user.
- `required` blocks launch on any unsupported provider or auth-negative smoke
  result.
- `off` explicitly bypasses the launch gate for emergency rollback.

The smoke runs as the lane OS user with a sanitized `env -i` environment and
does not pass Striatum MCP tokens, PostgreSQL DSNs, provider token variables, or
raw workflow command output into the result. It may use the network and provider
tokens, so ordinary `striatum doctor` and `doctor --verbose` do not run it.
For Codex, a zero-exit smoke proves the lane provider CLI could authenticate;
missing or mismatched `--output-last-message` text is reported as a safe
`success_signal` diagnostic rather than treated as an auth failure. Blocking
results and doctor output include safe fields such as the probe name, exit
code, stdout/stderr byte counts, and success-signal state, but never raw
provider output.
Operators can request the same primitive explicitly:

```sh
striatum doctor --lane-provider-auth codex --json
```

When a `run drive` launch hits a provider-auth refusal, the driver forwards the
same gate mode to `supervise.start`, closes the freshly registered session with
a sanitized reason, and exits nonzero instead of repeatedly spawning doomed
lanes.

### The `.striatum/` contradiction, resolved

`.striatum/` is daemon-owned operational scratch (PTY FIFOs, pidfiles, the
capability-token cache) and is **private to the daemon/operator** — the lane user
gets no blanket access to it. The one explicit exception is per-job worktrees:
when a workflow uses worktree isolation, the daemon provisions lane write access
to that job's `.striatum/worktrees/<id>` subtree specifically, and nothing else
under `.striatum/`.

## Reading lane liveness: dead vs attach-failed (#201)

A launch that the daemon cannot attach is not necessarily a dead lane. `supervise
start` now distinguishes the two and records an accurate state:

- **`lost` / `supervisor child exited before it could be attached`** — the lane
  pane/process is gone. This is a genuine failure; start fresh.
- **`detached` / `attach_failed_lane_alive`** — the pane/process is alive but the
  daemon's attach leg failed. The common cause is a sandboxed lane whose helper
  runs as the lane user and cannot reach the daemon to attest, or missing lane
  provisioning (HOME, provider auth, repo ACLs). The session stays recoverable:
  fix the provisioning above and **rebridge** the supervisor rather than treating
  it as dead.

A healthy pane is never recorded as `lost` just because the attach leg failed, so
`status`/`dashboard` and the `supervisor.detached` event point at the real
problem (provisioning/attach) instead of a misleading "child exited".

## Lane launch environment (`path_prefix` / `command_env`, #223)

A lane sometimes needs a binary (e.g. `agy`) that is not on the daemon's PATH,
or a provider-specific env var. Do **not** reach for a machine-local `/tmp`
wrapper such as `["/usr/bin/env", "PATH=…", "agy", …]`: that makes the lane's
argv0 `env`, so adapter detection no longer sees `agy`, the agent-loop guard
refuses it, and the workflow stops being portable.

Instead declare the launch environment on the lane itself, in the workflow — it
travels with the snapshot and stays auditable:

```json
"lanes": {
  "agy": {
    "adapter": "process",
    "command": ["agy", "--sandbox"],
    "adapter_capabilities": { "agent_loop": true },
    "path_prefix": ["/opt/agy/bin"],
    "command_env": { "AGY_HOME": "/opt/agy" }
  }
}
```

- `path_prefix` — absolute directories **prepended** to the supervised lane PATH.
  The lane binary resolves from here regardless of the daemon's own PATH, so the
  command stays the normal `["agy", …]` shape and the adapter identity stays
  `agy`.
- `command_env` — extra non-secret env entries for the lane process. It **cannot**
  set `PATH` (use `path_prefix`) or any `STRIATUM_`-namespaced control var; the
  daemon's injected identity/token/MCP-endpoint env always wins, so the lane
  still authenticates as its own session and reaches the control plane over the
  injected MCP configuration — never a socket-backed local `striatum` CLI path
  that would be invalid for the sandbox user.

`validate`/`lint` enforce these shapes, and supervised launch refuses an
agent-loop lane whose argv0 is not a supported adapter — so the wrapper anti-
pattern is rejected rather than silently accepted.

## Scope and non-goals

- This is OS-level isolation of a process Striatum spawns; it is **not** new
  daemon authority or forcible sandboxing of a process Striatum did not spawn
  (RFC 0103 W7 / RFC 0099 honest-limit framing).
- It adds no new persistence, hosted service, or telemetry (D094/D028/D151
  intact).
- The artifact-publish *conflict* that drove the #87 incident toward direct DB
  edits is addressed separately by the artifact-contract legibility work
  (RFC 0100 / RFC 0103 W5) — this runbook removes the lane's *ability* to reach
  the DB regardless.
