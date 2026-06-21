# RFC 0143: Lane credential survival across a daemon boot-epoch rotation

Status: proposed (needs maintainer decision; security/authz blast-radius)
Date: 2026-06-21
author: proposer-claude-opus-4-8

## Summary

When `striatumd` restarts mid-run, a supervised lane that runs as the
`striatum-lane` OS user (the PG-less lane sandbox, RFC 0096 §2 /
docs/how-to/lane-sandbox.md) can be left unable to reseal already-completed work
because its only reachable credential paths fail across the restart:

- the **RPC endpoint rotates** on a boot-epoch change (the daemon publishes a
  fresh `mcp-http-endpoint`), and
- when the lane's credential-resolution chain falls through to the daemon's
  **runtime client-token** (`/run/striatum/client-token`, or
  `$STRIATUM_DAEMON_RUNTIME_DIR/client-token`), that file is owner-only
  (`halbritt`, mode `0600`). A lane running as `striatum-lane` cannot read it,
  so both the RPC reconnect path and the CLI fallback are locked out and the
  lane exits **unsealed** even though its deliverable is complete on disk.

GH issue #512 reports run `run_a4111203b0b833c864618ca28a1abaed` (repo
`prompts`, workflow `website-review-committee`): the daemon restarted ~14:50–15:07Z
during #503 migration-pin maintenance; the `design_claude` lane had already
written its 29 KB `DESIGN.md` but had not yet `work.complete`. It correctly
reasoned the old session was unrecoverable across the boot-epoch rotation and
stopped cleanly, leaving its design unsealed. An operator requeue (`supervise
stop` → `session close` → `recovery auto`) was required to re-run and seal it.

This RFC asks the maintainer to decide **whether, and how, to give a
striatum-lane lane a credential it can read after a boot-epoch rotation**. It
deliberately does **not** implement a change: every concrete option touches the
daemon capability-token trust model, which is an explicit decision surface, not
a triage edit.

## Affected issue

- **#512** — `striatum-lane` lanes cannot reseal after a boot-epoch rotation;
  rotated `/run/striatum/client-token` is `halbritt`-owned `0600` (RPC + CLI
  fallback both locked out).

Adjacent/related (cross-reference only; do not fold in):

- **#478 / RFC 0152 / D249** — the `agent_exited_unsealed` requeue-budget
  escalation this unsealed exit triggers downstream. That RFC governs *recovery
  policy after* an unsealed exit; this RFC governs *whether the exit happens at
  all*. Distinct surfaces.
- **#537 / #539** — committee POSIX-ACL provisioning on the repo tree (lane can
  write / daemon can manage). Those are filesystem ACLs on the *target
  repository* (an accepted, extendable provisioning convention, fixed directly).
  This issue is about a *daemon-private capability token* in the runtime dir —
  a different trust boundary, which is why it routes to RFC while #537/#539 did
  not.
- **#513** — `run drive` exits on transient socket loss (driver self-heal
  theme); related symptom family, separate fix.

## Current behavior (anchored at `origin/main` @ `fd26deb8`)

The lane's intended credential is the **session-bound token** minted in
`mintSessionBoundToken` (`go/pkg/mutations/session_token.go`) at supervise
start and injected into the lane env as `STRIATUM_MCP_TOKEN`
(`HandleSuperviseStart`, RFC 0096 V2 / #135 / #296). Its grants are
`{claim, write, read, review}` bound to the registering `session_id`, 24h TTL.
Crucially the design intent (documented at
`go/pkg/agentloop/endpoint.go:110-116`, `ResolveTokenMaterialFresh`) is that the
lane **reuses the session-bound bearer it already holds** — the token "does NOT
rotate on a normal daemon restart (only the endpoint does)".

The lane's credential-resolution order (`agentloop.ResolveTokenMaterial`,
`go/pkg/agentloop/token.go`) is:

1. `STRIATUM_MCP_TOKEN` env literal (the session-bound token — the intended path);
2. `STRIATUM_MCP_TOKEN_FILE`;
3. the **runtime `client-token` file** (the daemon bootstrap admin token);
4. the repo `.striatum/capability_token`.

On the #512 path the lane lost its live RPC connection across the boot-epoch
rotation. The agent-loop's #323 endpoint-rotation recovery re-reads the token
via `ResolveTokenMaterialFresh`, and the CLI fallback re-resolves via
`ResolveTokenMaterial`; both fall through to step 3 — the runtime
`client-token`. That file is the daemon's **bootstrap admin token**:
`admin.BootstrapRuntimeTokenIfNeeded` (`go/pkg/admin/bootstrap.go`) grants it the
full `bootstrapCapabilities` set — `{admin, read, write, claim, review, apply,
recovery, surgical_recovery}` — and `writeRuntimeToken` writes it `0600` in a
`0700` runtime dir, owner-only by construction. A `striatum-lane` lane cannot
read it, and `agentloop.ReadTokenFile` would reject it anyway (it refuses any
token file that is not owner-only).

## The unresolved decision

The issue suggests two fixes; both change the credential trust model:

1. **Group-read the runtime `client-token`** (e.g. a `striatum-lane` group with
   read access). This **widens** who can read the daemon's *full-authority
   bootstrap admin token* to every lane user. It directly contradicts the
   RFC 0096 V2 / #135 / #296 trust model whose entire point is that a lane
   authenticates as its own **session-bound** token (`{claim, write, read,
   review}`, session-scoped, 24h) and **never** as the shared operator admin
   override. Granting lanes read of the admin token would let any lane present
   `admin` / `apply` / `recovery` / `surgical_recovery` capability — a
   privilege-escalation that dissolves the session-binding enforcement. This
   option is categorically out of bounds for a FIX.

2. **Mint a lane-scoped token the lane can read** across a boot-epoch rotation.
   This is the trust-model-preserving direction, but it is a **new
   credential-distribution mechanism**, not a constant tweak:
   - The session-bound token today is deliberately env-baked and in-memory only
     (never written to a lane-readable file), so that a leaked file cannot
     outlive the session. A durable, lane-readable session-token *file* needs
     its own ownership (`striatum-lane`-owned `0600`, or a lane-group-readable
     mode), lifecycle, and revocation semantics — all new.
   - Across a boot-epoch rotation the lane reasoned the **session itself** was
     unrecoverable. A reseal-after-rotation token must define what authority
     survives a rotation (just `write` to seal the in-flight job? the full lane
     set?), for how long, and how it is invalidated when the session truly ends —
     i.e. it must define a *boot-epoch-survival* credential class that does not
     exist today.
   - It interacts with the boot-epoch invalidation the daemon publishes
     (`writeBootEpochFile`, `go/cmd/striatumd/main.go`) and with the #323
     endpoint-rotation recovery in the agent loop.

Both readings touch the same surface — *what credential a lane may hold and
read* — and resolving them is a security/authz product judgment, not a triage
edit. The decision to make:

1. **Keep status quo** — a boot-epoch rotation orphans an in-flight lane's
   output as complete-but-unsealed; the operator requeue (`supervise stop` →
   `session close` → `recovery auto`) is the supported recovery. Accept the
   interruption; treat daemon restarts mid-run as rare maintenance events.
2. **Mint a durable, lane-owned, session-scoped reseal token** written
   `striatum-lane`-owned `0600` (analogous to the lane-scratch ACL convention)
   carrying only the capabilities needed to seal the in-flight job, invalidated
   on session close and bounded by the session TTL. Preserves the session-bound
   trust model; adds a new credential file + lifecycle.
3. **Make the boot-epoch rotation re-mint and re-inject** the session-bound
   token into the live lane env / token file as part of the rotation handshake,
   so the lane never needs to read the admin token at all (the resolution chain
   would stop at step 1/2). Keeps the credential session-scoped; adds rotation
   plumbing.
4. **Narrow the fallback** — make the lane credential-resolution chain refuse to
   reach the admin `client-token` for a non-owner lane and instead surface an
   explicit, self-escalating "session unrecoverable across rotation" signal so
   the lane fails *legibly* (and the run's recovery routes it) rather than
   silently exiting unsealed. Does not let the lane reseal, but removes the
   misleading "permission denied" dead-end. Could combine with 1.

## Why a direct FIX was rejected (security reasoning)

- The literal issue suggestion ("group-read the client-token") **widens who can
  read the daemon's full-authority bootstrap admin token** beyond the existing
  accepted model. That is the single hottest blast-radius dimension here
  (`security_or_authz`): it grants every lane `admin`/`recovery`/`apply`
  authority and dissolves the session-binding the whole lane-token design
  (#135/#296) exists to enforce. A FIX must *narrow to a cited invariant*, not
  reverse the trust model.
- The trust-model-preserving alternative ("mint a lane-scoped token the lane can
  read") is **not a small reversible change**: it introduces a new
  credential-distribution mechanism (durable lane-readable token file, its
  ownership/mode, a boot-epoch-survival authority class, and its
  revocation/lifecycle). New credential mechanisms are a design decision, not a
  triage edit.
- There is no failing proof obtainable for a FIX that does not itself require
  defining the new credential class or widening token exposure — so per the
  routing rule the correct route is RFC.

## Hot blast-radius dims that forced RFC

- **security_or_authz** — the runtime `client-token` is the daemon's
  full-authority bootstrap admin credential, deliberately owner-only. Any change
  to who can read it, or to what credential a lane may durably hold, changes the
  daemon capability-token trust model (RFC 0096 / RFC 0110 / #135 / #296). This
  is exactly the model AGENTS.md and the triage routing guard flag as
  RFC-not-FIX.
- **cross_team_contract** — the session-bound token grant set, its env-only
  distribution, and the boot-epoch rotation handshake are read by the agent loop
  (#323 recovery), the supervisor, and the recovery sweep; a new credential
  class touches all of them.

## Out of scope

- This RFC does not implement any token change and touches no token code.
- It does not re-classify the downstream `agent_exited_unsealed` recovery policy
  (RFC 0152 / D249) — that governs what happens *after* an unsealed exit.
- It does not change the committee POSIX-ACL provisioning (#537 / #539), which is
  a target-repository filesystem convention, not a daemon-private credential.

## Acceptance / next steps

A maintainer picks one of options 1–4 (or a combination), which becomes a
decision-log entry. Only then does an implementation slice touch the credential
code. Until then the supported recovery for #512 is the operator requeue path
above; option 4 (legible failure) is the lowest-risk partial mitigation if the
maintainer wants to reduce the "silent unsealed exit" surprise without changing
what a lane may read.
