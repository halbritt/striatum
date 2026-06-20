# How-to: Auto-drive runs off the operator-model loop

`striatum run drive` is a deterministic Go reconcile loop over existing daemon
RPC methods (RFC 0116 / D175). It registers a session and supervises a lane for
each role/lane as the run's DAG unblocks, stops superseded or terminal lanes,
and blocks until the run reaches a terminal state — escalating loud on refusal
(RFC 0105). Crucially, **it spends zero operator-model tokens**: the mechanical
"spawn the next lane" work that an AI operator would otherwise hand-drive is
done by the binary, not by a frontier model.

**You do not have to launch it by hand.** As of the #212 auto-drive wiring,
`striatum run start` launches a detached driver for the run it just started, so
the run reconciles itself to a terminal state with **no operator process — and
no operator-model tokens — in the loop.** This is on by default; the rest of
this page is just how to observe, stop, or opt out of it.

> Auto-drive introduces no product-boundary change. Every spawn is still a
> capability-authenticated `supervise.start` RPC carrying the operator
> principal — the detached driver just owns the loop instead of an interactive
> operator session. The residual it does *not* remove (a process holding the
> operator's capability for the run's lifetime) is precisely what
> [RFC 0122](../rfcs/0122-scheduler-principal-auto-spawn.md) (accepted, D189)
> closes by moving spawn authority into the daemon.

## What happens on `run start`

```sh
striatum --repo /path/to/target run start --run-id run_abc123
# run start: auto-driving run_abc123 in background
#   (logs:   journalctl --user -u striatum-drive-run_abc123 -f;
#    stop:   systemctl --user stop striatum-drive-run_abc123;
#    resume after stop: striatum --repo /path/to/target run drive --run-id run_abc123)
```

The unit is registered with `--collect`, so `systemctl --user stop` does not
merely pause it — it **removes** the transient unit. `systemctl --user start
striatum-drive-<run>` after a stop therefore fails (`Unit ... not found`); the
resume command is `striatum run drive --run-id <run>`, not `systemctl start`
(#293).

`run start` runs the start verbatim, then launches the driver in a **transient
systemd user unit** named `striatum-drive-<run-id>`. The unit:

- runs as the operator OS user, in the operator's systemd user session — the
  same session the daemon runs in (`systemctl --user status striatumd`), so it
  inherits `XDG_RUNTIME_DIR` and discovers the runtime client-token exactly as
  any other CLI verb does;
- survives the `run start` process exiting;
- is **idempotent** — a second `run start` (or a manual `run drive`) for the
  same run is safe: the driver re-derives state from daemon reads and checks for
  a live session before spawning, so it never double-spawns a lane;
- garbage-collects itself (`--collect`) once the run reaches a terminal state.

Auto-drive is **best-effort**: if `systemd-run` is unavailable (e.g. a container
with no user systemd session) the start still succeeds and prints a one-line
hint with the manual `run drive` command. It never changes the start's exit
code, and its notices go to stderr only, so `--json` consumers are unaffected.

## Observe a driving run

```sh
RUN=run_abc123
journalctl --user -u striatum-drive-$RUN -f        # reconcile actions / loud escalations
systemctl --user status striatum-drive-$RUN        # is it still driving?
striatum dashboard --run-id $RUN                    # the human view of the run itself
```

When the run reaches a terminal state the driver exits and the transient unit is
collected. A non-zero driver exit means it hit a refusal it surfaced loudly
(RFC 0105) — read the journal and resolve the underlying job, then re-`run start`
(idempotent) or `striatum run drive --run-id $RUN` to resume driving.

## Stop or opt out

```sh
systemctl --user stop striatum-drive-$RUN          # stop driving this run (removes the transient unit)
striatum --repo <target> run drive --run-id $RUN   # resume after a stop (NOT `systemctl start`)
```

`run drive` refuses to start if a *live* drive for the same run is already
running (it would otherwise leave a stray duplicate behind the daemon's
double-claim guard, #293); stop that pid first, or pass `--force-concurrent` to
deliberately co-drive (e.g. a foreground terminal-state waiter alongside the
background unit).

Per-invocation opt-out:

```sh
striatum --repo <target> run start --run-id <id> --no-drive
```

Global opt-out (e.g. in CI, or when you drive runs yourself):

```sh
export STRIATUM_RUN_DRIVE_AUTO=0     # also accepts false / no / off
```

## Resume after a pause or a daemon/DB restart (re-arming the driver)

`run pause` holds a run for maintenance (e.g. a DB restart); `run resume` lifts
the hold by clearing `paused_at`. **Resume does not, by itself, restart the
driving loop** — and the transient `striatum-drive-<run>` unit from `run start`
**does not survive a daemon/DB outage** (it dies when its RPCs start failing,
and `--collect` is for terminal runs, not crashes). So after a
`pause → restart → resume`, the run is `running` but un-driven until a driver is
re-armed. To make the resume contract legible, `run resume` reports a `drive`
field naming which home will (or must) re-drive the run:

| `drive`                          | meaning | what you do |
| -------------------------------- | ------- | ----------- |
| `daemon_auto_spawn`              | the run holds an active auto_spawn grant **and** the daemon RFC 0122 scheduler is enabled | nothing — the scheduler re-adopts and re-drives the run on its next sweep |
| `auto_spawn_scheduler_disabled`  | the run holds a grant but the daemon scheduler is OFF on this deployment (`STRIATUM_AUTO_SPAWN_SCHEDULER` unset) | re-invoke the driver (`next_action` gives the exact command), or enable the scheduler |
| `operator_run_drive`             | the run has no auto_spawn grant, so the scheduler never adopts it | re-invoke the driver — `striatum run drive --run-id <run>` |

```sh
striatum --repo <target> run resume --run-id $RUN
# { ..., "status": "resumed", "drive": "operator_run_drive",
#   "next_action": "re-invoke the driver: striatum run drive --run-id run_abc123" }
striatum --repo <target> run drive --run-id $RUN   # re-arm when next_action is present
```

When `drive` is `daemon_auto_spawn` there is no `next_action` and no manual
`run drive` is needed — the daemon scheduler owns the loop. The grant exists only
for runs whose workflow opted a lane into `supervision.auto_spawn` at `run start`
(RFC 0122); every other run is operator-driven, so plan to re-`run drive` it after
any outage.

### Re-arming after a human checkpoint (`checkpoint resolve`)

A human checkpoint flips the run to `waiting_human`, which the drive loop treats
as drive-terminal: the transient `striatum-drive-<run>` unit exits and tears down
its lanes so a human takes a clean slot. Resolving the checkpoint with
`checkpoint resolve <blocker> continue|override` flips the run back to `running`
and unblocks the gated downstream work — but, like resume, **it does not by itself
restart the driver**. When the resolve leaves the run `running` with claimable
downstream work, the `checkpoint.resolve` response prepends the re-arm verb to its
`next_actions` so the operator (or an automation watching `next_actions`) re-drives
instead of the run silently stalling:

```sh
striatum --repo <target> checkpoint resolve $BLOCKER override --decision-id $DEC
# { ..., "run_state": "running",
#   "next_actions": ["run drive --run-id run_abc123", "claim_available_work", ...] }
striatum --repo <target> run drive --run-id $RUN   # re-arm when the hint is present
```

If the daemon RFC 0122 scheduler is enabled and the run holds an active grant, the
scheduler re-adopts the resolved run on its next sweep regardless; the hint is the
fallback for the operator-driven (no-grant / scheduler-off) deployments. The
`cancel` action terminates the gated work and gets no re-drive hint.

## Composition with explicit `run drive` and the refactoring-campaign skill

Because the driver is idempotent, auto-drive composes safely with anything that
also calls `run drive`:

- The **refactoring-campaign skill** (`run prepare` → `run start` → `run drive`)
  still works. With auto-drive on, the explicit foreground `run drive` is now
  *optional* — it remains useful purely as a terminal-state **waiter** for stage
  sequencing (or use the passive `wait-run.sh`). Because the background unit is
  already a live drive, a foreground `run drive` on the same run must pass
  `--force-concurrent` to co-drive (otherwise it refuses with the live pid, #293);
  the two reconcile the same run harmlessly behind the daemon double-claim guard.
- `run drive --once` and `dashboard --once` are unchanged single-shot frames.

## What this does and does not buy you

- **Token burn:** removed for the mechanical spawn/supervise/stop loop — the
  operator model only re-engages for genuine judgment or escalation, never to
  advance a ready job.
- **Latency:** `run drive` blocks on the RFC 0120 wake bus rather than a fixed
  sleep where available, so the poll interval is a fallback ceiling, not the
  steady-state reaction time.
- **Control surface (partial):** the operator model no longer has to *exercise*
  register-session / supervise-start to advance the DAG, but the transient unit
  still holds the operator's capability token for the run's lifetime. Fully
  removing the standing operator credential — so the *daemon* owns spawn
  authority and the operator model needs no spawn capability at all — is
  [RFC 0122](../rfcs/0122-scheduler-principal-auto-spawn.md) (`supervision.auto_spawn`).

## Related

- [RFC 0116](../rfcs/0116-zero-operator-touch-dag.md) — `run drive` design.
- [RFC 0124](../rfcs/0124-auto-drive-run-start.md) — the auto-drive-on-`run start`
  design (default-on, opt-outs, lifecycle/security, the paused-run hold).
- [RFC 0122](../rfcs/0122-scheduler-principal-auto-spawn.md) — scheduler
  principal that lets the daemon spawn directly (the next step beyond auto-drive).
- [Daemon runbook](daemon-runbook.md) — daemon lifecycle and token minting.
