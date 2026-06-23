# HOLDER — RFC 0143 falsifiable implementation spec (design-v3 REVISION)

author: holder-author-001

> This is the **third** falsification pass on RFC 0143 (*lane credential survival
> across a daemon boot-epoch rotation*) and a **proper revision**. v1
> (`rfc-0143-design`) returned `needs_revision` with seven findings F1–F7. v2
> (`rfc-0143-design-v2`) **genuinely resolved F2 and F4** (both falsifiers
> conceded) but returned `needs_revision` again: two material challenges landed
> unrebutted and four findings were only *nominally* closed. The v2 adjudicator
> distilled the residue into **two repairs the next revision must make in one
> place**; `SEED.md` expands those into the **five binding constraints BC1–BC5**
> (security cluster BC1+BC2+BC3, lifecycle cluster BC4+BC5).
>
> This v3 spec starts from the **v2** `HOLDER.md` (required context), **resolves
> BC1–BC5 with a concrete, source-anchored mechanism and named tests**, and
> **carries the v2-credited resolved set forward unregressed** (F2, F4, the F7
> file-mirror half, AF1, AF4, the categorical no-admin-token-widening invariant,
> the per-claim falsifiable-assertion discipline). It does **not** relitigate the
> ratified OQ1 trust-model shape or the F2 non-bearer decision (both pinned in
> `SEED.md` `## Ratified design shape`). Every source citation below was
> re-verified against current `main` while authoring this revision; the falsifiers
> re-attack this published claim.

## Root reframe (held, unchanged)

**A boot-epoch rotation must never force a lane to choose between reading the
daemon's full-authority bootstrap admin `client-token` and exiting silently
unsealed.** A `striatum-lane` lane authenticates as *its own* narrow,
session-scoped credential and **never** as the shared operator admin override.
v3 either lets the lane's in-flight work be sealed over a **daemon-projected,
session-tied authority that no lane bearer carries**, or makes the failure
**loud, typed, and routed** — never silent, never via the admin token.

## What v3 changes vs v2 (the two named repairs, in one place)

The v2 design named the right *shapes* but not the load-bearing *mechanisms*.
v3 supplies both, and lands them in **one new daemon mutation, `resealInFlightJob`**
(`go/pkg/mutations/recovery_reseal_rotation.go`, deliberately a *different* file
and verb from the existing RFC 0125 `HandleRecoveryReseal` /
`go/pkg/mutations/recovery_reseal.go`, which is the worktree-durability operator
verb and is unrelated to credentials):

1. **The control path is never parsed terminal output (BC1).** The Slice-A floor
   and the Slice-B reseal *trigger* ride the agentloop wrapper's **OS process
   exit code** — already recorded by the helper without inspecting a single
   output byte — and any richer/mid-life signal rides a **daemon-owned inherited
   file descriptor** that the provider child never inherits and that sibling lanes
   have no filesystem name to reach. Artifact identity comes from daemon state,
   not from the signal (BC2). `CapabilityReseal` becomes a **daemon-internal
   marker projected by `resealInFlightJob`**, not a public bearer capability (BC3).
2. **The lifecycle is a race-free, numeric, source-anchored contract (BC4+BC5).**
   A concrete monotonic `jobs.recovery_generation` column (owner bundle 0021,
   modelled exactly on `review_generation`) is stamped into the work-packet at
   claim and compared at reseal; a numeric `resealGrace` with a hard maximum and
   one-extension-only rule, plus the **exact `lockRun` per-run advisory-lock order**
   the recovery sweep and the seal paths already share, makes the post-rotation
   expiry race resolve deterministically to a same-lease seal **or** the typed
   `session_unrecoverable_across_rotation` class — never a raw `lease_error`,
   never a lease revived after the sweep requeued the job.

## Ratified design shape (pinned — built on, not relitigated)

- **OQ1 (ratified):** Slice A = Option 4 (mandatory, zero-trust-change, lands
  first) + Slice B (ratification-gated) = Option 2's narrow `CapabilityReseal`
  over a daemon-owned session-tied path + minimal Option 3 per-session
  endpoint+epoch republish. No lane-readable reseal bearer file under any option.
- **F2 (decided):** non-bearer, daemon-owned, session-tied channel; **no readable
  reseal token file at all** (every lane shares the `striatum-lane` uid, so any
  `0600` file is a same-uid replay surface). Not reopened.
- **Slice B requires maintainer ratification** before any build slice touches
  credential code. Adjudicator clearance gates the spec's *soundness*, not the
  maintainer's product call. Slice A is zero-trust-change and may land first under
  the normal review gate **once BC1 routes it over a real, non-PTY-output channel.**

## Architectural facts re-anchored (AF1–AF4 — carried forward unregressed)

- **AF1 — reachability, not reminting (credited v1/v2 strength, kept).**
  `mintSessionBoundToken` (`go/pkg/mutations/session_token.go`) inserts the client
  row + per-capability grants into daemon-owned PostgreSQL bound to `session_id`,
  24h TTL. **PostgreSQL survives a `striatumd` restart** (D094 / RFC 0043). After a
  boot-epoch rotation the token is still *valid* — it is only *unreachable*,
  because it lives as the `STRIATUM_MCP_TOKEN` env literal (step 1) and the
  post-rotation re-readers skip step 1. The fix is routing, not re-minting.
  *Falsifier:* `TestTokenValidAcrossRestart`.
- **AF2 — the post-rotation re-readers fall to step 3.** `ResolveTokenMaterial`
  (`go/pkg/agentloop/token.go:18-53`) reaches the runtime `client-token` branch at
  `:31-41` whenever steps 1/2 are absent; the #323 fresh re-read
  (`ResolveTokenMaterialFresh`, `go/pkg/agentloop/endpoint.go`) likewise skips the
  env literal. Since the session-bound token is never written to disk, the fresh
  re-read finds no lane-readable credential and falls to step 3 — the named bug.
- **AF3 — step 3 is the full-authority admin token in a `0700` dir.**
  `admin.BootstrapRuntimeTokenIfNeeded` (`go/pkg/admin/bootstrap.go:18-27`) grants
  the runtime `client-token` the full `bootstrapCapabilities`
  `{admin,read,write,claim,review,apply,recovery,surgical_recovery}`, written
  `0600` in a `0700` dir; `ReadTokenFile` (`token.go:75-92`) rejects any token file
  not owner-only (`mode&0077 != 0`). The `0700` dir is the first wall and is
  load-bearing for the invariant.
- **AF4 — epoch/token decoupling (credited strength, kept).** The endpoint and the
  boot epoch rotate together; #316 deliberately retires a surviving lane's
  connection by rejecting a stale epoch. The token does **not** rotate on a normal
  restart — only the endpoint does. Preserved.

## Carried forward from v2, unregressed (do NOT reopen)

| Item | Status | Anchor / test kept |
| --- | --- | --- |
| **F2** — v1 `0600` bearer file retired; reseal authority is non-bearer | resolved | no on-disk reseal bearer exists; `TestBorrowedResealBearerCannotSealVictimSession` |
| **F4** — route-specific `MethodEntry.ResealAlternate` admits `CapabilityReseal` on only `interrogation.answer`/`work.complete`/`artifact.publish`, records `AuthContext.Capability == reseal`, never `write` | resolved | `TestResealTokenCanReachOnlyResealRoutesWithoutWrite`; command-authority-matrix reseal column |
| **F7 file-mirror half** — endpoint/epoch on a daemon-owned lane-read-only `0644` file, `O_NOFOLLOW`, atomic rename, reject MISSING epoch header on the supervised path (closes #316 permissive header-absent) | resolved | `TestResealEpochMirrorRejectsTamperOrMissingEpoch` |
| **AF1** reachability-not-reminting | kept | `TestTokenValidAcrossRestart` |
| **AF4** epoch/token decoupling | kept | (above) |
| **No-admin-token-widening invariant** | held + strengthened | `TestResolveRefusesRuntimeClientTokenForLane` |
| **Per-claim falsifiable-assertion discipline** | extended to channel + generation | A1–A14 below |

---

# Security cluster (BC1 + BC2 + BC3)

## BC1 — Authenticated control channel, NOT parsed PTY output (closes F1 / F6 / the channel half of F2 / F7-channel)

**The gap (v2).** The v2 spec leaned on "a structured line on the PTY/helper
bridge" for both the Slice-A floor and the Slice-B reseal request. In current
source that is either ordinary terminal output — a product-boundary breach
(`AGENTS.md:43-56`; `docs/how-to/how-to-agent.md`: terminal output is not
authoritative workflow state) and spoofable (the provider CLI, a shell command
during local verification, or prompt-injected model text can print the same
sentinel) — or an unspecified private envelope. Verified source: the helper
control stream carries lifecycle/byte-count metadata only with agent output kept
out of it (`go/pkg/supervisor/helper_protocol.go:41-44`); `RunHelper`
"deliberately does not … inspect workflow state, publish artifacts, complete jobs,
or acknowledge work … only moves process bytes" (`go/pkg/supervisor/helper.go:120-127`);
`pumpPTYProgress` watches output **VOLUME not content** per D028
(`helper.go:357-415`); and the accepted supervise-report event whitelist
(`superviseReportEventTypes`, `go/pkg/mutations/supervision.go:19-28`) admits **no**
content/output event.

**v3 commits to BOTH halves of the BC1 either/or, and NEITHER ever parses output
bytes.**

### BC1-(b) — the trusted-wrapper EXIT CODE is the floor + the exit-time reseal trigger (already recorded without parsing output)

The agentloop wrapper is the process the helper launches (`HelperLaunchSpec.Command`,
`helper_protocol.go:31`); the provider CLI (claude/codex) runs as the wrapper's
**child**. The helper already captures the wrapper's **OS process exit status**
into the `agent_exited` event payload — `agentExitPayload` → `processExitCode`
(`helper.go:427-439`, via `(*exec.ExitError).ExitCode()`), an OS-level value,
**never read from stdout/stderr** — and the curated payload keeps only
`exit_code`/`error`/`cause` (`supervision.go:424-425`). The provider, being a
grandchild, **cannot set the wrapper's exit code**; prompt-injected provider text
can fill the PTY but cannot forge an exit status. This is the sound
trusted-wrapper signal the v2 gate said was acceptable "only if the helper records
it WITHOUT parsing output bytes" — and it already does.

v3 reserves two agentloop exit-code constants (new, `go/pkg/agentloop/exitcodes.go`;
chosen in the rarely-used high range to avoid collision with provider codes):

- `ExitUnrecoverableAcrossRotation = 97` — the wrapper exits this **instead of
  falling through to the admin `client-token`**. `ResolveTokenMaterial`
  (`token.go:31-41`) and `ResolveTokenMaterialFresh` (`endpoint.go`) return a typed
  `ErrSessionUnrecoverableAcrossRotation` for a supervised lane rather than the
  runtime `client-token`; `go/pkg/agentloop/loop.go` maps that sentinel to exit 97.
- `ExitResealInFlightRequested = 98` — the wrapper exits this when its required
  deliverable is complete-on-disk and it wants the daemon to seal the in-flight job.

The daemon recognises the reserved codes in the **existing** `agent_exited` branch
of `recordSuperviseReportEvent` (`supervision.go:298-306`), which already reads
`event.Payload["exit_code"]`. On 97 it records a durable
`session_unrecoverable_across_rotation` blocker (BC1 → the Option-4 floor; the
`blockers` table has **no CHECK on `blocker_kind`** —
`go/pkg/db/sql/0005_repo_local_workflow_state.sql:259-276` — so the new free-text
kind is buildable). On 98 it invokes `resealInFlightJob` (below). The lane never
calls `work.block`/`session.report` (both need caps a no-token lane lacks); the
daemon-owned helper, not the lane, talks to the daemon.

### BC1-(a) — the private authenticated control channel for richer / mid-life intent: a daemon-owned INHERITED FILE DESCRIPTOR

An exit code cannot express a mid-life reseal (e.g. `interrogation.answer` while
the lane keeps running). For that v3 names a private control channel that is
**un-spoofable by the provider AND by same-uid sibling lanes** — the precise
defect that killed the v2 PTY line and the v1 `0600` file:

- **Descriptor — an inherited fd, never a filesystem path.** At launch the helper
  (daemon uid) creates a `socketpair(2)` and passes **one end as an inherited fd**
  to the wrapper via `exec.Cmd.ExtraFiles` (the wrapper sees it as fd 3), keeping
  the other end itself. The helper advertises the number in the launch env
  (`STRIATUM_SUPERVISOR_CONTROL_FD=3`). **The wrapper does NOT pass fd 3 to the
  provider** (the provider is exec'd with only stdio + the PTY; fd 3 is closed/
  `O_CLOEXEC` across that exec). Because the channel has **no name in the
  filesystem**, a sibling `striatum-lane` process has nothing to open — the same-uid
  surface that defeats a `0600` file simply does not exist (this is the F2 win
  generalised to the live channel). `HelperLaunchSpec` gains a `ControlFD` plumbing
  field; `RunHelper` gains a `pumpControlChannel` reader goroutine alongside
  `pumpPTYProgress`/`forwardPacketStream` (`helper.go:200-208`).
- **Ownership / parser boundary.** Frames are read **only** from the control fd by
  `pumpControlChannel`; `pumpPTYProgress` stays volume-only (D028) and can emit no
  control event. The PTY (provider stdout/stderr) is fd 1/2 and reaches only the
  volume meter.
- **Message schema + framing.** One JSON object per line (mirroring
  `HelperControlEvent`): `SupervisorControlFrame{ schema_version:
  "striatum.supervisor_control.v1", type: "reseal_requested" |
  "unrecoverable_across_rotation", supervisor_id, control_nonce }`. It carries
  **NO** job_id, artifact path, kind, or body — identity is derived from daemon
  state (BC2).
- **Replay protection.** The daemon mints a single-use `control_nonce` per launch
  (per generation) into the wrapper env (`STRIATUM_SUPERVISOR_CONTROL_NONCE`); the
  wrapper echoes it; the daemon rejects any frame whose nonce ≠ the launch nonce
  for that `supervisor_id`, and the generation guard (BC4) refuses a nonce from a
  prior generation. fd-possession is the primary authentication; the nonce ties the
  frame to the supervision row. Even a forged frame can do **nothing but** "seal
  *my own* in-flight job from daemon state" — which is exactly what is allowed —
  because the daemon trusts no identity in the frame (BC2).
- **Daemon side.** The helper maps a valid frame to a new `reseal_requested`
  `HelperControlEvent`, added to `superviseReportEventTypes` (`supervision.go:19-28`);
  `recordSuperviseReportEvent` gains a branch that invokes `resealInFlightJob`. No
  bearer token round-trips to the lane.

**New tests (BC1):**
- `TestPTYOutputCannotEmitSupervisorControlEvent` — bytes written to the PTY
  (any volume/content) never produce a `reseal_requested`/blocker event;
  `superviseReportEventTypes` admits no content event.
- `TestProviderOutputCannotDriveResealOrBlocker` — a child writing the reserved
  sentinels to stdout/stderr, and a child that does **not** inherit fd 3, cannot
  drive a reseal or a blocker; only the wrapper's exit code and the inherited-fd
  frame do.
- `TestAgentLoopReservedExitCodeRoutesUnrecoverableWithoutPTYParsing` — exit 97
  routes the typed blocker and exit 98 routes `resealInFlightJob` purely from
  `event.Payload["exit_code"]`, with the PTY pump stubbed to assert no output read
  participates in the decision.

## BC2 — Artifact identity from daemon state, never from output (security cluster)

Even over an authenticated channel, the daemon must not trust the signal for
**what** is being sealed. `resealInFlightJob` derives the expected-artifact set
from **its own state** and refuses any unexpected path, reusing the existing
handler payload contracts verbatim:

- **`artifact.publish`** requires `session_id`/`job_id`/`lease_id`/`kind`/
  `logical_name`/`path` (`go/pkg/mutations/artifact.go:52-60`), takes
  `lockRunForJob` first and enforces session binding
  (`HandlePublishArtifact`, `artifact.go:64-83`).
- **`work.complete`** requires `session_id`/`job_id`/`lease_id`
  (`go/pkg/mutations/lifecycle.go:1124-1129`).
- **`interrogation.answer`** requires `session_id`/`interrogation_id`/`body`
  (`go/pkg/mutations/interrogation.go:217-221`).

For a reseal **complete**, the daemon resolves `jobs.expected_artifacts_json`
(attempt-resolved via `resolveExpectedArtifactCycles`) and verifies every required
artifact is durable, reusing `verifyRequiredArtifacts`
(`go/pkg/mutations/mutations.go:828-876`) and `ensurePerJobPublishedArtifactsDurable`
(`artifact_durability.go`). For a reseal **publish**, the daemon publishes **only**
a `path` that is an open entry in the job's `expected_artifacts`, reading the body
from the job's own worktree (`job_worktrees`, `0005:350-372`), and **refuses any
path not in the expected set**. The signal (exit code or frame) supplies neither
path nor content.

**Front-matter / author-line failure routes to the floor, never a silent drop.**
The existing publish path validates front matter (the publisher refuses invalid
front matter with exit code 6, `AGENTS.md`). If a reseal-publish hits an
author-line / front-matter validation failure, `resealInFlightJob` does **not**
swallow it: it records the `session_unrecoverable_across_rotation` blocker with the
validation error in the payload (the Option-4 floor), so a malformed reseal
surfaces loudly.

**New test (BC2):** `TestCodexResealUsesReceiverNotProviderStdout` — a Codex lane's
reseal seals the in-flight job's `expected_artifacts` from daemon state + worktree;
a frame/stdout claiming a path **not** in `expected_artifacts` is refused; nothing
is read from provider stdout.

## BC3 — `CapabilityReseal` is a daemon-internal marker, not a public bearer capability (closes the new cycle-2 finding C2)

**The gap (v2).** `CapabilityReseal` was conflated as BOTH a public bearer-auth
capability AND a daemon-internal projection. The public prelude authorises
`envelope.CapabilityToken` before dispatch
(`go/pkg/rpc/server.go:107-111`: `s.Authorizer.Authorize(entry.RequiredCapability,
…, envelope.CapabilityToken)` then `RequireAllowed`), and `PostgresAuthorizer`
resolves only a **token-backed** decision (`go/pkg/rpc/auth_pg.go:159-206`). With
**no reseal bearer** (the F2 pin retired the file), a public alternate route is
**unreachable** — a capability with no bearer cannot pass a prelude that demands a
bearer token.

**v3 declares `CapabilityReseal` a daemon-internal capability marker, projected by
the private `resealInFlightJob` mutation:**

- **Projection, not presentation.** `resealInFlightJob` maps `supervisor_id` →
  `session_id` from the supervision row (`process_supervisors` /
  `process_supervisor_pointers`, the same lookup `recordSuperviseReportEvent` uses
  via `findReportSupervisor`, `supervision.go:497-528`), constructs an **internal**
  `rpc.AuthContext{ Capability: CapabilityReseal, SessionID: s, RepositoryID: r }`
  **without going through the public `Authorize` prelude**, threads it with
  `WithAuthContext` (the same context seam server.go:120 uses), and calls the same
  lower-level publish/complete routines (`publishArtifactWithOptions`,
  the `HandleCompleteWork` core after its gates) against the job's active worktree.
  The authority is **daemon-projected**; no bearer reaches the lane.
- **Public route-alternate kept for tests only.** The v2/F4 wiring stays exactly as
  credited — `MethodEntry.ResealAlternate` set true on only `interrogation.answer`/
  `work.complete`/`artifact.publish`; the prelude re-authorises against
  `CapabilityReseal` on a `capability_missing` for those routes and records
  `AuthContext.Capability == reseal` (never `write`). Because there is no
  production reseal bearer, this path is exercised **only by the guardrail tests**
  (it proves reseal reaches *only* those three routes and resolves to `reseal`),
  not by any live caller. `registry_methods.go` is **generated**
  (`// Code generated by … routergen … DO NOT EDIT`), so `ResealAlternate` lands in
  the contract source `contracts/daemon_methods.json` + the `MethodEntry` struct
  (`go/pkg/rpc/registry.go`) + the regenerated map + a reseal column in
  `docs/reference/command-authority-matrix.md` + the authority guardrail (per
  `AGENTS.md` change-discipline).
- **Reseal payload schema + validation/reuse path.** The daemon-internal call
  reuses the existing payload contracts (BC2): complete = `{session_id, job_id,
  lease_id}`; publish = `{session_id, job_id, lease_id, kind, logical_name, path}`
  with `path ∈ expected_artifacts`; answer = `{session_id, interrogation_id, body}`.
  Validation failure (binding, write-scope, front matter, unexpected path) routes
  to the Option-4 floor (BC2), never a silent drop.

**New test (BC3):** `TestResealCapabilityIsDaemonInternalNotBearer` — no live caller
can present `CapabilityReseal` (no bearer exists); the only path that seals is the
internal `resealInFlightJob` projection keyed to `supervisor_id`↔`session_id`; the
route-alternate is reachable only from the guardrail harness.

---

# Lifecycle cluster (BC4 + BC5)

## BC4 — Concrete monotonic generation column for the split-brain guard (closes F3)

**The gap (v2).** The "no recovery-generation change" guard named no storage:
`jobs` has `current_lease_id` but no generation
(`0005_repo_local_workflow_state.sql:75-104`, `:166-186`); `job_recovery_state`
holds requeue/transfer/respawn **counters** — a recovery budget, not a lease-issued
generation (`0020_job_recovery_state.sql:13-28`); `review_generation` is the verdict
epoch (`owner/0009_review_generation.sql`); `activeLeaseFor` does no generation
check (`mutations.go:803-820`).

**v3 names a concrete column, modelled exactly on the credited `review_generation`
precedent.**

- **Column + migration / owner-bundle location.** `jobs` is an **owner-held** table
  in the two-role posture, so a column-add is owner DDL that must live in an owner
  bundle — `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` (floor v27) forbids a
  runtime migration from ALTERing it (see `owner/0009_review_generation.sql:1-24`,
  `owner/0017_pipe_read_liveness.sql:1-11`). v3 adds **owner bundle
  `go/pkg/db/sql/owner/0021_job_recovery_generation.sql`**:
  `ALTER TABLE striatumd.jobs ADD COLUMN IF NOT EXISTS recovery_generation integer
  NOT NULL DEFAULT 0;`, and bumps `LatestOwnerBundleVersion` 20→21
  (`go/pkg/db/owner.go:23`) with the matching `[[owner_bundle]]` ordinal-21
  reservation in `RESERVATIONS.toml` (`go/pkg/db/reservations.go`). Like
  `review_generation`, `striatumd_rw` already holds table-level grants that extend
  to the new column, so no new grant is required.
- **Degrade-safe presence probe.** A `db.JobRecoveryGenerationColumnPresent`
  helper (mirroring `SessionPipeReadColumnPresent` / `ArtifactPlacementColumnPresent`
  / `reviewGenerationEnabled`, `go/pkg/db/artifact_write.go:64-102`). If the daemon
  is ahead of owner bundle 21 (column absent), `resealInFlightJob` treats the
  generation as **unverifiable and routes to the Slice-A typed floor** — it never
  seals without the guard.
- **Increment points (each in the same tx/UPDATE that retires or rebinds the job's
  authoritative lease, all already under `lockRun`):**
  1. **claim** — `claimChosenJob` (`go/pkg/mutations/claim.go:222-228`) sets
     `state='claimed', current_lease_id=$1`; add `recovery_generation =
     recovery_generation + 1`.
  2. **requeue (same attempt)** — `requeueJobSameAttempt`
     (`go/pkg/mutations/recovery.go:2097-2109`, and the
     `insertPendingMessageForJob` branch at `:2086-2093`) sets `state='queued',
     current_lease_id=NULL`; increment there.
  3. **recovery sweep lease expiry / transfer / respawn** — the
     `current_lease_id = NULL` transitions in `HandleRecoveryAuto`/`SweepRun`
     (`recovery.go:619` `expireLeases`, transfer `:2546`, respawn/cancel paths
     `:2854`/`:2935`); increment on each.
  4. **release** — `work.release` lease-release path; increment.
  These are the complete set of points where the job's binding to a lease/session
  changes such that the prior lane must no longer seal. Monotonic by construction
  (only `+1`), exactly like `jobs.attempt`/`review_generation`.
- **Stamped value for reseal-time comparison.** `claimChosenJob` writes the
  post-increment `recovery_generation` into the work-packet's `lease` block
  (`buildPacket`, persisted in `work_packets.packet_json`,
  `claim.go:229-260`) as `lease.recovery_generation`. At reseal,
  `resealInFlightJob` reads the stamped generation from the lane's bound
  `work_packets` row and compares it to the **live** `jobs.recovery_generation`
  under the lock. **Equal → no requeue/re-lease since the claim → proceed; unequal
  → the job was requeued/re-leased → refuse with the typed class** (the
  generation-mismatch discipline `review_generation` uses, not a DELETE).

**New tests (BC4):**
- `TestResealPredicateUsesStampedRecoveryGeneration` — a reseal whose stamped
  generation ≠ live `jobs.recovery_generation` is refused with the typed class;
  equal generation proceeds.
- `TestResealTokenRefusedAfterLeaseExpiryAndRecoveryRequeue` (kept) — after a
  requeue bumps the generation (or the lease expires beyond grace), reseal is
  refused; an old lane cannot publish/complete into a requeued/retired job.

## BC5 — Numeric `resealGrace` + exact lock order vs the recovery race (closes F5)

**The gap (v2).** The "bounded reseal grace" had no number, source, or maximum;
`resealInFlightJob` was said to reuse `activeLeaseFor`, which returns a **raw
`lease_error`** on expiry with no generation check (`mutations.go:803-820`); and
there was no lock order vs the recovery sweep, which **drains helper events then
expires/requeues** (`recovery.go:575-590` drain-first, `:610` `lockRun`, `:619`
`expireLeases`), while the seal paths take `lockRunForJob` + row locks
(`artifact.go:64-83`, `lifecycle.go:1151-1180`).

**v3 pins the number, the maximum, the one-extension rule, and the exact lock order.**

- **`resealGrace` numeric + source + maximum.** A daemon constant
  `const resealGraceWindow = 30 * time.Second` (new, beside the lease constants in
  `go/pkg/mutations`), **hard-capped** at the packet's heartbeat window so grace can
  never outlive a single missed heartbeat: `grace = min(resealGraceWindow,
  packet.lease.heartbeat_after_seconds)` (the packet's
  `heartbeat_after_seconds` is 300s; grace is 30s in practice). It is a daemon-side
  allowance, **not** a lane-invokable `work.heartbeat` — `CapabilityReseal` carries
  no heartbeat verb.
- **One same-lease extension only, before a generation change forecloses it.**
  `resealInFlightJob` may move the bound lease row's `expires_at` forward by `grace`
  **exactly once**, gated by a new `leases.reseal_grace_extended_at timestamptz`
  (NULL until used; added in the same owner bundle 0021 if `leases` is owner-held,
  else a runtime migration — the build run keys this to the table's ownership). An
  extension is allowed only if `now() - expires_at ≤ grace` **and**
  `jobs.recovery_generation == stamped` (no requeue) **and**
  `reseal_grace_extended_at IS NULL`. A second expiry without a completed seal, or
  any generation change, forecloses further extension → typed floor.
- **EXACT lock order (the serialization spine).** `resealInFlightJob` runs in one
  `withTxRetryOnDeadlock` and takes, in this order — identical to the seal paths and
  the sweep so they cannot interleave:
  1. `lockRunForJob(ctx, tx, repositoryID, jobID)` → `lockRun(run_id)` =
     `pg_advisory_xact_lock(hashtext(run_id))` (`mutations.go:663-665`, RFC 0104),
     the **same per-run advisory lock** the recovery sweep takes
     (`HandleRecoveryAuto`, `recovery.go:610`) and the seal paths take first
     (`artifact.go:76`, `lifecycle.go:1154`). Re-entrant within the tx, so the
     internal reuse of `publishArtifactWithOptions`/the complete core (which take
     `lockRunForJob` again) is safe. The `run_lock_guard_test.go` guardrail requires
     this advisory lock **before** any run-scoped `FOR UPDATE`.
  2. `FOR UPDATE` on the `jobs` row, then the bound `leases` row, then the
     `job_recovery_state` row (stable key order; under the per-run advisory lock
     there is at most one run-scoped writer, so no cross-row deadlock).
  3. Evaluate the reseal predicate; on pass, project the internal AuthContext (BC3)
     and seal; on any failure, route the typed class.
  Because the sweep **drains helper events in its own short txns BEFORE `lockRun`**
  (`recovery.go:575-590`) but performs `expireLeases`/requeue **inside** the
  `lockRun` tx (`:610-621`), and `resealInFlightJob` also takes `lockRun` before it
  reads the generation/lease, the two are strictly serialized:
  - **Sweep wins the lock first:** it expires the lease and (on requeue) bumps the
    generation; reseal then blocks, acquires the lock, observes the changed
    generation / expired-beyond-grace lease, and routes the typed class — **never
    revives the requeued lease.**
  - **Reseal wins the lock first:** it seals within grace and commits; the sweep
    then observes a completed job and does not requeue.
- **Expired-beyond-grace ALWAYS routes the typed class.** `resealInFlightJob` does
  **not** call `activeLeaseFor` (whose `lease_error` is raw). It uses a
  reseal-specific predicate that, on expiry-beyond-grace, generation mismatch, or a
  closed/retired session, returns `ErrSessionUnrecoverableAcrossRotation` →
  recorded as the `session_unrecoverable_across_rotation` blocker. No raw
  `lease_error` ever reaches a post-rotation reseal.

**New tests (BC5):**
- `TestResealBeyondGraceRoutesTypedNotLeaseError` — a reseal past `expires_at +
  grace` routes the typed class, never a raw `lease_error`.
- `TestResealGraceCannotReviveRequeuedLease` — once the sweep requeues (generation
  bump + lease expired), no grace extension revives the old lease.
- `TestRecoveryRequeueWinsOverExpiredLeaseReseal` — concurrent sweep + reseal on
  an expired lease: the `lockRun` order yields requeue-wins or same-lease-seal,
  never split-brain; reuses the `run_lock_guard_test.go` advisory-lock assertion.
- `GD-1b` (kept) — restart `striatumd`, block reconnect past `expires_at`, drive a
  reseal: outcome is a single same-lease renew-and-seal within grace **or** the
  typed class — never raw `lease_error`, stale-lease limbo, or silent unsealed exit.

---

## The one place it lands: `resealInFlightJob` (contract sketch)

```
resealInFlightJob(repositoryID, supervisorID, intent):  // intent ∈ {complete, publish, answer}
  withTxRetryOnDeadlock:
    session   := supervisorSession(supervisorID)              // process_supervisors row; closed/none -> typed floor
    job, pkt  := inFlightJobAndPacket(session)                // bound work_packets row (stamped recovery_generation)
    lockRunForJob(job.run_id)                                 // pg_advisory_xact_lock — BEFORE any FOR UPDATE
    jobRow    := SELECT ... FROM jobs   WHERE job_id FOR UPDATE
    leaseRow  := SELECT ... FROM leases WHERE lease_id FOR UPDATE
    SELECT ... FROM job_recovery_state WHERE job_id FOR UPDATE
    if !JobRecoveryGenerationColumnPresent: return typedFloor("generation-unverifiable")
    if session not active:                 return typedFloor("session retired")
    if leaseRow.owner != session or leaseRow.resource != job: return typedFloor("lease not this job/session")
    if jobRow.recovery_generation != pkt.lease.recovery_generation: return typedFloor("generation changed")
    if leaseRow.expired:
        if within grace and generation matches and reseal_grace_extended_at IS NULL:
            UPDATE leases SET expires_at = now()+grace, reseal_grace_extended_at = now()  // ONE extension
        else: return typedFloor("expired beyond grace")
    if !supervisedEpochAccepted(session):  return typedFloor("epoch missing/mismatch")   // F7 channel half
    authCtx := internal AuthContext{Capability: reseal, SessionID: session}              // BC3 projection
    switch intent:
      complete: verifyRequiredArtifacts(job) ; completeCore(authCtx, job, lease)         // BC2
      publish:  require path ∈ expected_artifacts ; publishArtifactWithOptions(authCtx)  // BC2
      answer:   interrogationAnswerCore(authCtx, ...)
    // any binding/write-scope/front-matter failure -> typedFloor(reason)                // BC2/BC3
```
`typedFloor(reason)` records the durable `session_unrecoverable_across_rotation`
blocker (Option-4) — never a raw `lease_error`, never a silent drop.

## Security invariant (the spine) — held and strengthened

The runtime `client-token` carries the full `bootstrapCapabilities` and is `0600`
in a `0700` dir (AF3; `bootstrap.go:18-27`). **Any option that lets a lane read
that file, or mints a lane-readable credential carrying any of
`{admin, apply, recovery, surgical_recovery}`, is categorically out of bounds.**
v3 keeps this structurally impossible and strengthens it past v2:

- The lane never gets OS read of the `0700` dir (AF3); the Slice-A floor removes the
  only code path that would have read the `client-token` (`token.go:31-41` /
  `endpoint.go` return the typed error for a supervised lane).
- The only new authority, `CapabilityReseal`, carries **no elevated verb** and is
  **never materialised into any lane-readable file or bearer** (BC3 + F2). There is
  nothing to read, steal, or replay.
- The reseal is **projected by the daemon only** on the supervisor-proven path; a
  lane cannot present `CapabilityReseal`, let alone admin/apply/recovery.
- The control channel is an **inherited fd with no filesystem name** (BC1-a) or the
  **OS exit code** (BC1-b) — neither is a bearer, neither is reachable by a sibling
  lane or the provider.
- The epoch republish moves **endpoint + epoch only** (non-secret anti-confusion
  tags) over the daemon-owned, integrity-protected path (F7 file-mirror, kept);
  never the admin token.

*Falsifier:* `TestResolveRefusesRuntimeClientTokenForLane` — `ResolveTokenMaterial`/
`ResolveTokenMaterialFresh` return `ErrSessionUnrecoverableAcrossRotation` for a
supervised lane, never the runtime `client-token`.

## Falsifiable assertions (each with the named test / game-day that refutes it)

- **A1 — No-widening.** `CapabilityReseal` carries only the three reseal verbs and
  is daemon-internal. *Refuted if* `TestResealTokenCanReachOnlyResealRoutesWithoutWrite`
  or `TestResealCapabilityIsDaemonInternalNotBearer` shows it reaching any of
  `admin`/`apply`/`recovery`/`surgical_recovery`/`work.claim_next`/any non-reseal
  route, or resolving to `write`, or presentable as a bearer.
- **A2 — No admin-token fall-through.** *Refuted if*
  `TestResolveRefusesRuntimeClientTokenForLane` returns the runtime `client-token`
  for a supervised lane instead of the typed error.
- **A3 — No-replay, structural.** No lane-readable reseal bearer and no
  same-uid-reachable channel. *Refuted if*
  `TestBorrowedResealBearerCannotSealVictimSession` finds an on-disk reseal bearer,
  or a sibling/foreign-session caller (or the provider child) seals session A's job.
- **A4 — Control path never parses output.** *Refuted if*
  `TestPTYOutputCannotEmitSupervisorControlEvent` /
  `TestProviderOutputCannotDriveResealOrBlocker` shows PTY/stdout bytes driving a
  reseal or blocker, or the helper inspecting child output to make a control
  decision.
- **A5 — Floor is a typed exit code recorded without parsing.** *Refuted if*
  `TestAgentLoopReservedExitCodeRoutesUnrecoverableWithoutPTYParsing` shows exit 97
  failing to route the durable blocker, or the decision reading output bytes.
- **A6 — Reseal identity from daemon state.** *Refuted if*
  `TestCodexResealUsesReceiverNotProviderStdout` shows a path outside
  `expected_artifacts` accepted, or artifact identity/content read from provider
  stdout.
- **A7 — No split-brain, by stamped generation.** *Refuted if*
  `TestResealPredicateUsesStampedRecoveryGeneration` /
  `TestResealTokenRefusedAfterLeaseExpiryAndRecoveryRequeue` shows a reseal
  succeeding after a generation bump, or publishing into a requeued/retired job.
- **A8 — Numeric grace, never raw `lease_error`.** *Refuted if*
  `TestResealBeyondGraceRoutesTypedNotLeaseError` yields a raw `lease_error`, or
  grace exceeds `min(resealGraceWindow, heartbeat_after_seconds)`.
- **A9 — One extension, no revive.** *Refuted if*
  `TestResealGraceCannotReviveRequeuedLease` extends a lease twice or revives a
  requeued lease.
- **A10 — Lock order serializes reseal vs sweep.** *Refuted if*
  `TestRecoveryRequeueWinsOverExpiredLeaseReseal` or the `run_lock_guard_test.go`
  guardrail shows a reseal taking a run-scoped `FOR UPDATE` before
  `pg_advisory_xact_lock`, or an interleave that split-brains.
- **A11 — Epoch path does not weaken #316.** *Refuted if*
  `TestResealEpochMirrorRejectsTamperOrMissingEpoch` shows a lane-writable epoch
  source, a successful symlink/replace, or a missing-header supervised request
  accepted.
- **A12 — Token validity survives the restart.** *Refuted if*
  `TestTokenValidAcrossRestart` shows the PG-resident token rejected purely because
  the process restarted.
- **A13 — Loud, durable failure.** *Refuted if* game-day **GD-1** (restart
  `striatumd` mid-job, no reachable token file) shows a silent unsealed exit, a raw
  permission error, a local-only process error, or no durable
  `session_unrecoverable_across_rotation` blocker.
- **A14 — Lease-window bound.** *Refuted if* **GD-1b** yields a raw `lease_error`,
  stale-lease limbo, or a silent unsealed exit instead of a same-lease
  renew-and-seal within grace or the typed class.

## Adapter survival matrix (F6 — honest, re-grounded on BC1/BC2)

No adapter needs to reload its MCP launch args to seal the in-flight job: the seal
is daemon-side (`resealInFlightJob`) triggered by the wrapper's exit code or the
inherited-fd frame — both adapter-independent and neither parsed from provider
output.

| Adapter | Reseal-in-flight (Slice B) | Resume normal MCP work after rotation |
| --- | --- | --- |
| **Claude** (ephemeral MCP config) | exit code / inherited-fd frame → `resealInFlightJob` (no token reload) | #323 ephemeral-config rewrite + endpoint/epoch republish |
| **Agy / pipe** | same daemon-side path | same as Claude where supported |
| **Codex** (MCP URL baked into launch `-c` args; `applyMCPEndpointRotation` can only log + inject a relaunch prompt) | same daemon-side path — **no in-place MCP survival claimed** | operator-assisted relaunch / `supervise rebridge` only |

*Refuting game-day — GD-Codex-Reseal-Rotation:* restart `striatumd` mid-job for a
Codex lane; the in-flight job seals over the daemon-side path **or** fails legibly
to Option 4, and the spec does **not** claim the Codex MCP client reconnected in
place. *Refuted if* the spec relies on the Codex MCP client reloading baked args,
or the Codex lane silently exits unsealed.

## Scope discipline (Non-Goals held)

- Does **not** re-classify the downstream `agent_exited_unsealed` recovery policy
  (RFC 0152 / D249); the new `session_unrecoverable_across_rotation` blocker is a
  distinct, earlier class.
- Does **not** change committee POSIX-ACL repo provisioning (#537 / #539).
- Does **not** touch `run drive`'s transient-socket behavior (#513).
- Does **not** weaken the #316 boot-epoch recycled-port defense (BC1/F7 strengthen
  it on the supervised path).
- Does **not** introduce any lane-readable credential file (the v1 `0600` reseal
  file stays retired by the maintainer pin).
- Does **not** collide with the RFC 0125 `HandleRecoveryReseal` worktree-durability
  verb (separate file, separate verb, unrelated to credentials).
- Local-first, single-host, daemon-owned PostgreSQL as the single writer.

## Maintainer ratification gate (required)

**Slice B introduces a new daemon-internal capability marker
(`rpc.CapabilityReseal`), a test-only auth-prelude route alternate, the inherited-fd
supervisor control channel, the reserved agentloop exit codes, the
`jobs.recovery_generation` owner-bundle column, and endpoint/epoch republish
plumbing — a security/authz trust-model change.** This cleared spec is a
**RECOMMENDATION the maintainer ratifies before any build slice touches credential
code.** Slice A (the Option-4 typed-exit-code floor) is zero-trust-change and may
land first under the normal review gate **now that BC1 routes it over a real,
non-PTY-output channel.** Adjudicator clearance gates the spec's **soundness**; it
is not the maintainer's product call on the credential code. (The maintainer has
already ratified the OQ1 shape and the F2 non-bearer decision in `SEED.md`; this
gate governs the build slice that writes the code.)

---
<sub>Holder revised proposal (design-v3) for the RFC 0143 falsification-gate design
run. Resolves the cycle-2 binding constraints BC1–BC5 (security cluster
BC1/BC2/BC3, lifecycle cluster BC4/BC5) and carries the v2-credited resolved set
forward unregressed. The adjudicator's collaboration ledger — not falsifier
completion — decides whether this gate clears.</sub>
