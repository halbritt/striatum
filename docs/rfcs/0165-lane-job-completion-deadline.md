# RFC 0165: Lane/job completion deadline (seal-keyed dual-clock)

Status: proposed
Date: 2026-06-22
Context: [#576](https://github.com/halbritt/striatum/issues/576) (alive-but-never-completing lane); residual of [#540](https://github.com/halbritt/striatum/issues/540); RFC 0091 (lane-health / liveness classification), RFC 0131 (transport-aware liveness confidence), RFC 0135 (monotonic seal ledger / barrier keying), RFC 0140 (alive-but-silent attestation — the carve point), RFC 0110 (two-role DB-enforced write boundary), RFC 0142 (safe-by-construction shadow-first phased rollout). #145 is the genuinely-long-lane false-positive case the current design deliberately protects.
author: proposer-claude-opus-4-8

## Problem

A lane can stay **alive and chatty but never complete its work**, and the run
wedges with no clean daemon recovery. The lease and the work-progress are
measured by the **same signal** (PTY/heartbeat bytes), so a wedged-but-talking
lane is structurally indistinguishable from a healthy slow one:

- `refreshActiveLeaseWorkHeartbeat` (`go/pkg/mutations/supervision.go:824-870`)
  renews the lease `expires_at` to `now+1800s` and stamps `last_pty_activity_at`
  on every meaningful PTY progress event — so the lease stays warm forever.
- `ToolProgressSeconds = 600` (`go/pkg/sessionliveness/liveness.go:320`) only
  trips the `#324 wedged_no_tool_progress` rung when the lane has a *recorded
  tool-call history that went stale*; a lane with no tool history stays
  `working_local` indefinitely.
- Even when that rung trips, RFC 0140
  (`go/pkg/lanehealth/lanehealth.go:263-299`) re-attests the lane as
  `LivenessAliveButSilent` (kept warm, not transferred) whenever the PID probe
  is alive and identity-matched.
- `doctor`'s stuck-job check (`go/pkg/reads/doctor_stuck_job.go`) skips any job
  with `live_session_count > 0`.

There is **no wall-clock completion deadline anywhere**: `max_attempts` is an
attempt budget (the wedge is *within* one attempt), and `ToolProgressSeconds` is
the only time bound — defeated by heartbeats and by the alive-PID re-attestation.

## Goals

- Detect a lane that is alive but is not *completing sealed work*, independent of
  lease/heartbeat warmth.
- Make the detection signal **un-forgeable** by a flailing lane (no amount of PTY
  chatter should reset the completion clock).
- Carve cleanly against RFC 0140 alive-but-silent and against the genuinely-long
  lane (#145) **without hand-tuned magic numbers**.
- Be **safe-by-construction and shadow-deployable**: the mechanism must observe
  and alert long before it can ever kill, and a false positive must never cause
  an outage merely by landing to `main`.
- On breach, leave a **durable, auditable warrant** — never a silently-recycled
  session that loses why it died.

## Non-Goals

- Replacing or weakening RFC 0140 alive-but-silent attestation — this layers
  *on top of* it with an explicit carve, it does not remove the warm-keep for
  legitimately-silent live PIDs.
- Killing on liveness alone (that is the existing `#324`/RFC 0140 surface).
- A run-level / barrier-pressure global budget (noted as future work, not P0–P2).
- Adversarial anti-forgery beyond the attested-lane trust model (rotating
  nonces, etc. — the daemon-witnessed seal cursor already defeats replay).

## Proposal

### 1. Seal-keyed dual-clock trigger

A job breaches its completion deadline only when **both** hold:

1. `now - jobs.started_at` exceeds a per-job-type cap, **and**
2. a monotonic **sealed-progress cursor** has not advanced for `N` minutes.

The cursor advances **only** on a `work.*` *SEALED state-transition* — the same
seal ledger RFC 0135 keys barriers on — never on a PTY byte, heartbeat, or lease
renewal. This is the load-bearing carve against RFC 0140: warmth alone never
satisfies the completion clock; only real sealed state movement resets it. A
chatty-but-wedged lane trips; a slow lane that keeps sealing artifacts is immune.

### 2. Sweep-owned arming watermark (DB-enforced, un-forgeable)

Persist `last_sealed_progress_at` in a column the lane has **no write path to**
(RFC 0110 two-role style — only the daemon/recovery-sweep principal writes it on
witnessing a seal). The trigger reads the *sweep-owned* watermark, not any field
the lane keeps fresh. Lease-warmth and completion-progress become structurally
independent, and the watermark + cap + frozen cursor together form the
audit-grade kill warrant.

### 3. #145 carve — self-calibrating cap + explicit grant

- Default cap = `k × p95(historical completion for this job_type)` — a
  genuinely-long lane merely at the long tail of its own distribution never
  trips, but one `5×` past every sibling does. No magic flat `N`.
- Overridable in workflow shape config (per-job-type default → shape override).
- A genuinely-long phase carves itself out **only** by an explicitly minted
  `declared_long_phase` grant (a logged, privileged, audited act by the
  agent/operator), never by emitting heartbeats. Legitimacy is an explicit
  ledger exception, not something earned by staying noisy.

### 4. Phased, shadow-first rollout (RFC 0142-style)

- **P0 (shadow):** additive migration adds the sweep-owned
  `last_sealed_progress_at` + per-job-type cap columns; a `completion_deadline`
  `doctor` probe + a shadow event stream record *would-breach* verdicts only — no
  kill, no requeue. Bake and graph predicted-breach vs actual-wedge before arming
  anything. (Same shadow-first pattern used for RFC 0142 P3 / #570.)
- **P1 (calibrate + carve):** `k × p95` caps + shape-config override + the
  `declared_long_phase` grant token; still shadow.
- **P2 (arm):** breach → `needs_operator` + a sealed escalation artifact citing
  `started_at`, the last sealed seq, elapsed-vs-cap, and the heartbeat-vs-seal
  divergence; auto-requeue **refused** until a recovery verb consumes the
  warrant; each requeue spends a capped, non-renewable fresh-session token so
  requeue thrash self-extinguishes into `needs_operator`. Carve explicitly
  documented against RFC 0140 at `go/pkg/lanehealth/lanehealth.go:263-299`.

## Acceptance Criteria

- A lane that emits continuous PTY progress but seals no new `work.*` transition
  trips the (shadowed) deadline after `cap`, while a lane that keeps sealing
  never does.
- The arming watermark is unwritable by the lane principal (negative test: a
  lane-role write to `last_sealed_progress_at` is refused at the DB boundary).
- A `declared_long_phase` grant suppresses the breach for its declared window and
  is recorded as a privileged ledger act.
- In shadow mode no job is ever killed/requeued; would-breach verdicts are
  recorded and visible in `doctor` + the event stream.
- In armed mode a breach emits a schema-valid escalation artifact and never
  auto-requeues without the warrant being consumed.

## Open Questions

- **Q1 (cap basis):** ship P1 with `k × p95` from historical completions, or
  start with a conservative per-job-type flat default and migrate to `p95` once
  enough history exists? (History may be sparse for new job types.)
- **Q2 (grant authority):** may a lane mint its own `declared_long_phase`, or
  only the operator/run-owner? (Self-mint is ergonomic but weakens the carve.)
- **Q3 (`N` vs cap):** is the frozen-cursor window `N` a separate tunable, or
  derived from the cap?

## Domain Modeling

- **completion cursor** — a monotonic count of sealed `work.*` transitions for a
  job; advanceable only by daemon-witnessed seals (RFC 0135 ledger).
- **arming watermark** — `last_sealed_progress_at`, sweep-owned, lane-unwritable.
- **completion deadline** — `(started_at + cap)` paired with the frozen-cursor
  window; a breach requires both clocks.
- **declared_long_phase grant** — a privileged, logged exception extending the
  cap for a bounded window.
- **completion warrant** — the sealed escalation artifact emitted on an armed
  breach.

## Wider opportunity (optional follow-up, out of scope)

- **Witness-sibling race:** instead of killing on breach, fork a fresh competing
  sibling under the same barrier and let the run progress on whichever seals
  first — turns every deadline decision from a blind trust-the-daemon kill into a
  *falsifiable* race. High value, high complexity (barrier interaction +
  double-provenance); a candidate P3.
- **Run-level / barrier-pressure weighting:** scale the deadline's bite to how
  many sibling lanes are blocked on this job — preempt a wedged lane on the
  critical path first, leave an idle-but-unblocking long lane alone.

## Pointers

- `go/pkg/mutations/supervision.go:824-870` — `refreshActiveLeaseWorkHeartbeat`
  (lease renew on PTY progress).
- `go/pkg/sessionliveness/liveness.go:320,575-619` — `ToolProgressSeconds`,
  `wedged_no_tool_progress` rung.
- `go/pkg/lanehealth/lanehealth.go:263-299` — RFC 0140 `LivenessAliveButSilent`
  re-attestation (the carve point).
- `go/pkg/reads/doctor_stuck_job.go` — stuck-job check (skips live-session jobs).
- `go/pkg/mutations/recovery_decision_tree.go` — recovery routing.
- RFC 0135 (seal ledger), RFC 0110 (two-role write boundary), RFC 0142 P3 / #570
  (the shadow-first boot-gate precedent).

Designed via `/adhd` (5 isolated cognitive frames → 30 ideas → converged). No
code lands with the proposal.
