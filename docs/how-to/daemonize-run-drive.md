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
#   (logs: journalctl --user -u striatum-drive-run_abc123 -f;
#    stop:  systemctl --user stop striatum-drive-run_abc123)
```

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
systemctl --user stop striatum-drive-$RUN          # stop driving this run (safe; re-drive resumes)
```

Per-invocation opt-out:

```sh
striatum --repo <target> run start --run-id <id> --no-drive
```

Global opt-out (e.g. in CI, or when you drive runs yourself):

```sh
export STRIATUM_RUN_DRIVE_AUTO=0     # also accepts false / no / off
```

## Composition with explicit `run drive` and the refactoring-campaign skill

Because the driver is idempotent, auto-drive composes safely with anything that
also calls `run drive`:

- The **refactoring-campaign skill** (`run prepare` → `run start` → `run drive`)
  still works unchanged. With auto-drive on, the explicit foreground `run drive`
  is now *optional* — it remains useful purely as a terminal-state **waiter** for
  stage sequencing (or use the passive `wait-run.sh`); the background driver and
  a foreground driver reconcile the same run harmlessly.
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
- [RFC 0122](../rfcs/0122-scheduler-principal-auto-spawn.md) — scheduler
  principal that lets the daemon spawn directly (the next step beyond auto-drive).
- [Daemon runbook](daemon-runbook.md) — daemon lifecycle and token minting.
