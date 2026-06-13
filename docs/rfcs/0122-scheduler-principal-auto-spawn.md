# RFC 0122: Scheduler principal — run-owner pre-authorization for daemon-side `supervision.auto_spawn`

Status: accepted (D189)
Date: 2026-06-13
author: proposer-claude-opus-4-8-001
Context: RFC 0116 / D175 (`run drive` + the `supervision.auto_spawn` deferral, #212); RFC 0107 / D160 (multi-principal trust model — this RFC is its scheduler-principal successor); RFC 0110 (PG-auth + DB-enforced write boundary, L2 run-as / L3 principal attribution); RFC 0096 V2 / #135 (session-bound capability tokens); RFC 0105 / D161 (unattended-reliability yolo gate); RFC 0103 W7 (operator as bounded processor); RFC 0120 / D180 (await-packet idle exit + notify-only wake bus). Code: `go/pkg/mutations/supervision_control.go` (`HandleSuperviseStart`), `go/pkg/mutations/session_token.go` (`mintSessionBoundToken`), `go/pkg/db/authority.go` (`BeginAuthorizedMutation` / `authorityPreludeSQL` → `striatum.principal_id`), `go/pkg/db/sql/0023_principals.sql`, `go/pkg/cli/rundrive/rundrive.go` (the reconcile predicate to be reused).

## Summary

Define a **scheduler principal** so the daemon can spawn lanes from its own job
scheduler — closing `supervision.auto_spawn` (#212) without weakening
attestation. The principal is **not** a new synthetic non-human actor; it is the
**run owner's pre-authorization**, captured at `run start` and replayed by the
scheduler for that run's DAG only. The daemon-side scheduler is **design only —
no `go/` scheduler code lands with acceptance**; the operator-side **auto-drive
wiring** (the do-now token-burn fix) ships alongside it (§Phase 0). Acceptance
supersedes the D175 deferral of `supervision.auto_spawn` specifically, on
evidence the deferral did not weigh.

## Problem

D175 (RFC 0116 §"Verb 2") analyzed daemon-side `supervision.auto_spawn` and
explicitly deferred it behind a three-part evidence trigger, recommending
operator-side `striatum run drive` first. That was the right call at the time.
Two forces the deferral did not weigh have since become load-bearing:

1. **Operator-model token burn.** RFC 0116's framing — and RFC 0103 W7's "the
   operator is a bounded, well-served processor" — silently assume the
   operator's engagement is approximately free. When the operator is an
   expensive frontier model, every cycle it must wake to do mechanical
   orchestration (`register-session` + `supervise.start` as the DAG unblocks)
   has real, recurring cost. `run drive` already removes that cost for the
   *driven* loop (it is a Go binary, zero model tokens) — but only while a
   process owns the loop. The honest endgame of "stop spending model tokens on
   mechanical spawning" is for the spawn to need *no* operator process at all.

2. **Control-surface reduction for a yolo operator.** RFC 0116 treated "operator
   in the loop" as a *safety property to preserve*. That holds when the operator
   is a trusted human. When the operator is a model running in yolo mode
   (`--dangerously-skip-permissions`), keeping it in the spawn loop is the
   opposite of safety: the model must hold register-session / supervise-start
   capability and, when `run drive` is daemonized (see
   [daemonize-run-drive](../how-to/daemonize-run-drive.md)), a standing process
   holds the operator's capability token for the whole run. **Least privilege
   says move spawn authority into the daemon**, so the yolo operator model needs
   no spawn capability and no standing credential rides along. This *inverts*
   the W7 objection: for an untrusted operator, daemon-owned spawn is a control
   surface *reduction*, not the boundary erosion the deferral assumed.

Neither force makes auto_spawn trivially safe. The deferral's real objection —
**a scheduler-initiated spawn has no contemporaneous operator principal** — is
genuine and remains the gate. This RFC closes exactly that gap.

## What the deferral got right (and this RFC must preserve)

RFC 0116 §"Verb 2" raised four objections. Restated, with the property each one
protects:

- **Attestation (RFC 0110 L3).** Every spawn today is attributed to the operator
  principal that called `supervise.start`: `BeginAuthorizedMutation`
  (`authority.go`) installs `set_config('striatum.principal_id', …, true)` as the
  transaction's first statement, so the audit chain records *whose* capability
  authorized the lane. A scheduler with no caller has no `principal_id` to set.
- **Run-as / sandbox (RFC 0110 L2).** `mintSessionBoundToken`
  (`session_token.go`) mints a session-bound capability (claim/write/read/review,
  TTL-scoped, bound to the session + repository) and `HandleSuperviseStart` wires
  it into the lane env, all anchored to the *registering request*. Auto-spawn has
  no registering request to anchor which OS user, which environment, which token.
- **Crash/restart.** `run drive` crash = re-drive reconciles idempotently. A
  daemon scheduler that *decides to spawn* is new restart surface: on
  `systemctl restart striatumd` it must not double-spawn already-satisfied jobs,
  and must not spawn a job a human meant to hold.
- **Boundary posture (RFC 0103 W7).** A daemon that initiates work with no
  operator in the loop is a product decision, to be made with data.

The design below answers all four. The principle throughout: **the scheduler
never invents authority — it replays authority the run owner already granted.**

## Design

### 1. The flag

`supervision.auto_spawn: true` — an opt-in field on a lane (or workflow-wide
default) in the run's workflow snapshot. It tells the daemon scheduler: when a
job for this lane becomes ready and the lane has a registered launch command,
the daemon may register a session + spawn its supervisor *without* a
contemporaneous operator RPC. Default `false` everywhere; absence means today's
behavior exactly.

### 2. Run-owner pre-authorization (the principal)

The novel primitive, and the reason this is tractable where a "synthetic
scheduler principal" was not: **the authorizing principal is the run owner, and
authorization is captured at `run start`, not minted at spawn time.**

- At `run start`, the operator already authenticates with a capability token
  carrying a `principal_id` (RFC 0107 / `0023_principals.sql`). When the run's
  snapshot contains any `auto_spawn: true` lane, `run start` persists a
  **spawn-authorization grant**: `(run_id, owner_principal_id, run_as_spec,
  capability_envelope, expires_at)`. This is a durable, run-scoped, revocable
  record that says "principal P pre-authorizes the daemon to spawn the lanes of
  this DAG, as this run-as identity, until this run terminates or the grant is
  revoked."
- It is **deferred capability**, the same shape the project already trusts:
  session-bound tokens (RFC 0096 V2) defer a capability to a future lane-loop
  call; this defers a capability to a future *scheduler* call. No new actor
  kind, no non-human author — the author of record stays the human run owner.

### 3. Scheduler mint + attribute (reuse, don't reinvent)

When the scheduler spawns under an `auto_spawn` lane:

- It opens the mutation with `BeginAuthorizedMutation`, setting
  `striatum.principal_id` to the **captured `owner_principal_id`** from the
  grant. Attestation is preserved unchanged: the audit chain reads exactly as if
  the owner had called `supervise.start`, because — by their pre-authorization —
  they did. No synthetic principal, no impersonation of a different identity.
- It calls the **same** `mintSessionBoundToken` path, anchoring the mint on the
  grant's `capability_envelope` and `run_as_spec` instead of a live request.
  This requires factoring the "anchor" out of `HandleSuperviseStart` so both the
  request-driven and grant-driven callers feed one minting function — the token
  shape, hashing, TTL, and session binding are identical.

### 4. Run-as resolution

`run_as_spec` is resolved **once, at `run start`, by the operator's live
request** — the moment RFC 0110 L2 already resolves it for `supervise.start`.
The scheduler never *chooses* a run-as identity for "a lane no operator asked
for"; it reads the identity the owner fixed when they authorized the DAG. The
hardened default (dedicated PG-less lane OS user, `0700` socket dir) is captured
verbatim into the grant. A run whose grant cannot resolve a run-as identity is
refused at `run start`, loudly, before any auto-spawn is possible.

### 5. The reconcile predicate is one algorithm in two homes

Per RFC 0116's own closing note, `auto_spawn` must **reuse `run drive`'s exact
reconcile predicate** so the two paths are one tested algorithm. Concretely:
extract the slot-readiness / launch-decision logic from
`go/pkg/cli/rundrive/rundrive.go` (the `slotKey` launch map, the
"job ready ∧ slot unclaimed ∧ no live session" decision) into a shared package
consumed by both the CLI driver and the daemon scheduler. The daemon path adds
no *new* spawn decision — it runs the predicate the driver already runs, on
post-commit wake hints (RFC 0120) instead of a poll.

### 6. Crash / restart semantics

- **Idempotent re-spawn.** The scheduler holds no durable spawn state of its own;
  on startup it re-derives the launch map from daemon reads, exactly as
  `run drive` re-drives. The existing `slotHasUnclaimedParallelWork` guard (and
  the live-session check) prevents double-spawn; this RFC requires those guards
  be covered for the scheduler invoker, not only the operator invoker.
- **Human-hold respect.** A job a human means to hold is expressed as the absence
  of `auto_spawn` (or an explicit hold marker on the run); the scheduler spawns
  *only* lanes whose snapshot opted in. Restart re-reads the snapshot, so a hold
  set before restart survives it.
- **Grant lifecycle.** The spawn-authorization grant is bounded by run terminal
  state and an `expires_at`, and is revocable (a `run stop` / explicit revoke
  drops it). After expiry/revocation the scheduler refuses to spawn and escalates
  loud — it never silently falls back to a stale credential.

### 7. RFC 0105 gate extension

Per D175's condition, the scheduler-spawn path ships **behind the RFC 0105
unattended-reliability gate, extended to cover auto-spawn**: the hermetic chaos
fixture must drive a full DAG to terminal state via the daemon scheduler (no
`run drive`, no operator RPC after `run start`), and must prove loud-fail on a
poisoned spawn (expired grant, unresolved run-as, double-spawn attempt). The gate
is the behavioral contract, not a unit assertion.

### 8. Authority matrix / audit

`auto_spawn` adds **no new client-facing RPC** — the scheduler invokes the
existing internal spawn path. But it adds a **non-client invoker** to a mutation
that the command-authority-matrix and authority-guardrail tests currently assume
is reachable only via a client capability. This RFC requires:
`docs/reference/command-authority-matrix.md` gains a row documenting the
daemon-scheduler invoker of the spawn path and its grant-anchored authorization,
and the guardrail tests gain a case asserting the scheduler invoker cannot spawn
without a valid, unexpired, run-scoped grant.

## How this answers the four objections

| RFC 0116 §Verb 2 objection | Resolution |
| --- | --- |
| No contemporaneous principal (attestation) | Captured run-owner principal replayed via `set_config('striatum.principal_id')`; audit chain unchanged. Author of record stays the human owner. |
| Run-as / token minting has no request to anchor | `run_as_spec` + capability envelope captured at `run start` (the live request RFC 0110 L2 already uses); scheduler reads, never chooses. |
| Crash/restart double-spawn | Same idempotent re-derive as `run drive`; existing guards covered for the scheduler invoker; grant bounded + revocable; loud-fail on stale. |
| Boundary posture (operator in the loop) | For a *yolo* operator this is a least-privilege gain: the operator model needs no spawn capability and no standing credential. The product decision is made here, with the token-burn + control-surface evidence the deferral asked for. |

## Goals

1. Let the daemon spawn lanes for `auto_spawn: true` lanes with attestation,
   run-as, and audit **identical** to the operator-driven path.
2. Reuse one reconcile predicate across `run drive` and the scheduler.
3. Remove spawn capability from the operator-model's required grant set when a
   run is fully `auto_spawn` (least privilege for yolo operation).
4. Ship behind the RFC 0105 gate extended to the scheduler-spawn path.

## Non-Goals

- **No synthetic non-human author.** No principal kind that authors work on its
  own behalf; the run owner remains the author of record. (A future
  genuinely-autonomous scheduler principal would be a separate RFC 0107
  successor; this RFC deliberately stops at *deferred human authorization*.)
- **No hosted/tenanted service, no external scheduler, no cron-from-the-cloud.**
  Self-hosted boundary unchanged (RFC 0107).
- **No new durable workflow state, queue, or transcript store.** The grant is
  authorization metadata over existing run state, not a new authoritative bus;
  `claim_next` stays the authoritative transition (RFC 0120 invariant).
- **No co-driving.** `auto_spawn` and a live `run drive` on the same run are
  mutually exclusive (both run the same predicate; the advisory concurrent-driver
  marker extends to the scheduler).
- **No rescue authority.** The scheduler uses only normal lifecycle spawn; stale
  leases recover through the existing operator/daemon recovery path.

## Phased implementation (no `go/` code lands with acceptance)

- **Phase 0 (this RFC + the do-now wiring):** design + decision (D189)
  superseding the D175 auto_spawn deferral. No daemon-side *scheduler* code lands
  with acceptance; C-contracts C1–C6 below are the acceptance criteria for later
  phases. Shipping alongside acceptance is the **operator-side auto-drive
  wiring** — a `run start` interceptor that launches a detached `run drive` for
  the started run (transient systemd user unit, idempotent, best-effort,
  `--no-drive` / `STRIATUM_RUN_DRIVE_AUTO=0` opt-out; see
  [daemonize-run-drive](../how-to/daemonize-run-drive.md)). This is *not* the
  scheduler path: it still spawns under the operator principal via the normal
  `supervise.start` RPC and still holds a standing operator credential for the
  run's life — it removes the operator *model* from the loop, and Phases 2–4
  remove the standing *credential*.
- **Phase 1:** extract the shared reconcile predicate from `rundrive.go`; prove
  byte-identical launch decisions between CLI and the extracted package
  (`TestReconcilePredicateParity*`). No daemon behavior change yet.
- **Phase 2:** spawn-authorization grant — schema (owner-applied migration per
  RFC 0079 §5, since it touches the principals/authority surface), `run start`
  capture, run-as resolution + loud refusal on unresolved identity
  (`TestSpawnGrantCapture*`, `TestRunAsRefusal*`).
- **Phase 3:** scheduler spawn under the grant — refactor the mint anchor out of
  `HandleSuperviseStart`; scheduler mints + attributes under the captured
  principal; double-spawn + human-hold guards covered for the scheduler invoker
  (`TestSchedulerSpawnAttribution*`, `TestSchedulerNoDoubleSpawn*`,
  `TestSchedulerRespectsHold*`).
- **Phase 4:** RFC 0105 gate extension — hermetic fixture drives a DAG to
  terminal via the scheduler with no post-start operator RPC, plus poisoned-spawn
  loud-fail cases; command-authority-matrix row + guardrail case.

### Contracts

- **C1 — attestation parity:** a scheduler-spawned lane's audit attribution is
  indistinguishable from the same lane spawned by the owner via `supervise.start`.
- **C2 — no authority invention:** the scheduler cannot spawn without a valid,
  unexpired, run-scoped grant; a missing/expired/revoked grant → loud refusal,
  never a silent fallback.
- **C3 — predicate parity:** scheduler and `run drive` make identical spawn
  decisions on identical run state.
- **C4 — restart safety:** restart never double-spawns and never spawns a held
  job; re-derive is idempotent.
- **C5 — least privilege:** for a fully-`auto_spawn` run, the operator principal's
  grant set need not include spawn capability, and a run completes with no
  post-`run start` operator RPC.
- **C6 — boundary hold:** no new client RPC, no hosted service, no new
  authoritative state; `claim_next` remains the sole authoritative transition.

## The three-part trigger, reconciled

D175 set three conditions, all required. Status at this RFC:

1. **`run drive` routinely daemonized** — the
   [daemonize-run-drive](../how-to/daemonize-run-drive.md) how-to makes this the
   recommended operating mode for an expensive operator; adopting it generates
   the evidence directly. The *motivation* (token burn) is now explicit, not
   hypothetical.
2. **Poll cadence a measured bottleneck** — RFC 0120's wake bus already took the
   cheaper "event-driven ticking first" step, so this trigger is largely
   *retired* rather than met; the scheduler reuses those same wake hints. Latency
   is no longer the load-bearing argument — **token burn + control surface are.**
3. **A non-human scheduler principal model exists** — **this RFC supplies it**,
   in the narrowest defensible form (deferred run-owner authorization, not an
   autonomous actor).

The decision the deferral asked for is therefore now makeable *with data*: not
because latency forced it, but because the operator-cost and yolo-control-surface
evidence — absent from the original analysis — favors moving spawn authority into
the daemon.

## Decision ask

Accept this RFC as the scheduler-principal design that **supersedes the D175
deferral of `supervision.auto_spawn` specifically** (the rest of D175 / RFC 0116
stands). Acceptance authorizes the phased implementation above behind the RFC
0105 gate; it lands no `go/` code by itself. Record as a new decision-log entry
noting the token-burn + control-surface rationale and the C1–C6 contracts as the
guardrails.
