# RFC 0168: Per-lane OS user as the lane security principal

Status: accepted (D272 accepted_with_follow_up; P0 build v3 accepted and integrated)
Date: 2026-06-24
author: proposer-claude-opus-4-8

## Summary

Every supervised lane today runs as the single shared `striatum-lane` OS user
(the PG-less lane sandbox, RFC 0096 §2 / `docs/how-to/lane-sandbox.md`). That
shared uid is the **root cause** that made RFC 0143's authenticated reseal
channel unsolvable across seven falsification-gate design cycles: while all
lanes share one uid, **no daemon-launched control surface can be bound to the
specific wrapper the daemon launched**, because any same-uid sibling lane can
mutate the oracle the daemon queries (the `BC1-W1-ORACLE` finding — see
`docs/operator/artifacts/rfc-0143-design-v7/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md`).

This RFC adopts a **per-lane OS uid** (a pre-provisioned pool of lane users,
one bound to each live lane) as the lane security principal. It is the
structural fix that dissolves the entire same-uid replay class: a per-lane-uid
process cannot `open`/`setns`/`respawn-pane` a sibling lane's resources, so a
per-lane-uid-owned `0600` file (a reseal token, a private control socket) is
finally safe. This is the **prerequisite for RFC 0143 Slice B** (the
`CapabilityReseal` authority); it is recorded here as a separate RFC because it
is a host-provisioning and trust-model commitment with its own blast radius,
not a sub-step of 0143.

## What forced this (the same-uid wall)

RFC 0143 asked how a `striatum-lane` lane can reseal already-complete work after
a daemon boot-epoch rotation. Its design gate (`falsification_gate`, v1→v7)
converged on a single irreducible obstruction:

- The production lane control surface is **tmux run as the shared lane uid**
  (`commandInvocationWithEnvFile` wraps every `RunAsUser` command as
  `sudo -n -u <runAsUser> -- env -i …`, `go/pkg/supervisor/pty.go`;
  `RunAsTmuxRunner` invokes bare `tmux` via the same run-as path,
  `tmux_liveness.go:125-133`), with a **deterministic session name** and **no
  private `tmux -S` socket / `TMUX_TMPDIR` isolation** anywhere in
  `go/pkg/supervisor`.
- The daemon's only handle on "the wrapper" is a **post-launch tmux query
  against a same-uid-mutable oracle**. A same-uid sibling can `respawn-pane -k`
  the target pane between the daemon's launch and its capture, so the daemon
  authenticates the *replacement*, not the wrapper it launched.
- This is the **identical** same-uid threat model that already rejected a
  `0600` reseal file in RFC 0143 (option 2): under a shared uid the file is a
  sibling-readable replay surface.

The gate proved **same-uid no-replay is unsolvable while lanes share one uid.**
The fix is not a finer authentication handshake; it is to stop sharing the uid.

## Decision (maintainer-ratified direction, 2026-06-24)

**Adopt a per-lane OS uid (a pre-provisioned pool) as the lane security
principal.** A lane is launched as a uid leased from the pool for the duration
of the lane/session and returned (and scrubbed) on teardown.

Rationale:

- **It is the only structural, host-independent fix that survives a daemon
  restart.** A per-lane uid is enforced by the kernel's own uid-isolation
  (file ownership, `kill`/`ptrace`/`setns` permission checks), independent of
  host LSM configuration, and it persists across boot-epoch rotations because
  it is a property of the launched process, not of a daemon-held in-memory
  binding.
- **It dissolves the whole same-uid class at once.** `BC1-W1-ORACLE`,
  `BC1-W1-CAPTURE`, the rejected `0600` reseal file, and the same-uid tmux
  replay all vanish: a sibling lane is a *different uid* and cannot reach the
  pane, socket, or file.
- **It collapses most of RFC 0143 Slice B's complexity.** With a per-lane uid,
  a per-lane-uid-owned `0600` reseal file (RFC 0143 option 2) is safe — so the
  elaborate daemon-internal connect-out control channel with `SO_PEERCRED`
  pid+kernel-start-token authentication (the W1/W2/W3 walls) is no longer
  needed to defend against a same-uid sibling. The credential mechanism reduces
  to "write a lane-scoped reseal token owned by that lane's uid, `0600`."

## Rejected alternatives (from the `/adhd` analysis)

- **Namespace-inode binding** — NOT structural on this host. With Yama
  `ptrace_scope=1`, a same-uid sibling can still `open`+`setns` into the
  target's namespaces; the isolation is not enforced against a same-uid peer.
- **AppArmor-hat + per-launch magic token** — works, but is **host-dependent**:
  it relies on a fixed, compiled AppArmor hat-set and an LSM that may not be
  present or identically configured on every host Striatum runs on. Per-lane
  uid is host-independent.
- **Private tmux socket alone (without a per-lane uid)** — insufficient: under
  a shared uid a sibling can still reach a `0700`/`0600` socket path it can
  guess or enumerate, and can `ptrace`/inspect the launching process. The
  socket helps only once the uid already separates the principals.

## What this unblocks / changes

- **RFC 0143 Slice B** (`CapabilityReseal` authority) is gated on this RFC
  landing. With the per-lane uid in place, Slice B becomes RFC 0143 option 2 in
  its safe form: a durable, **lane-uid-owned `0600`** session-scoped reseal
  token, invalidated on session close, bounded by the session TTL.
- **RFC 0143 Slice A** (the legible `session_unrecoverable_across_rotation`
  typed-exit floor, option 4) **does NOT depend on this RFC** and ships
  independently — its predicate is computed entirely from daemon-side durable
  state (tmux/`#{pane_dead_status}` liveness + `/proc`/`kill(0)` + the DB
  artifact rows + git-blob reconstructability of the deliverable), with no
  authenticated inbound channel. See D261.
- It interacts with the existing lane-user host provisioning convention
  (`setfacl` per target repo, `docs/how-to/lane-sandbox.md`), the run-as launch
  path (`go/pkg/supervisor/pty.go`), and the POSIX-ACL committee provisioning
  (#537/#539): per-lane uids must each receive the repo-tree ACL grants a lane
  needs, and the pool must be reflected in the host setup.

## Blast radius

- **host_provisioning** — a pool of OS users must be pre-created on every host
  that runs lanes, with sudoers entries, home dirs, credential-store dirs, and
  per-target-repo ACLs. This is the heaviest cost and the reason this is a
  separate, gated RFC rather than a triage edit.
- **security_or_authz** — moving the lane principal from one shared uid to a
  per-lane uid is a trust-model change (it is a *narrowing*, not a widening —
  it grants no new authority — but it redraws the principal boundary the lane
  sandbox, attestation, and ACL model are written against).
- **cross_team_contract** — the run-as launch path, the lane attestation
  (`lane_attestation`/`lane_attestation_reason`), the credential-resolution
  chain, the recovery sweep, and the ACL provisioning runbook all read or write
  "the lane uid"; a pooled per-lane uid touches each.

## Open questions (for the design gate)

1. **Pool size and exhaustion.** How many pre-provisioned uids; what happens
   when the pool is exhausted (block, grow, or refuse new lanes)? How does the
   pool size relate to `max_active_jobs` / concurrent-lane ceilings?
2. **Lease/allocation lifecycle.** Daemon-leased like the session lease? When is
   a uid returned and scrubbed (home dir, credential store, tmux server, stray
   processes)? How is a leaked/never-returned uid reaped?
3. **Provisioning ownership.** Is the pool a host-setup artifact (documented
   runbook, like the current single lane user) or daemon-managed
   (create/destroy on demand, requiring the daemon to hold uid-management
   authority — a larger blast radius)?
4. **ACL interaction.** Per-target-repo ACLs (#537/#539,
   `docs/how-to/lane-sandbox.md`) must cover every pool uid; does this become a
   `DEFAULT` ACL on a lane group, or per-uid grants?
5. **Attestation.** How does `lane_attestation` change when the principal is a
   pooled uid rather than the fixed `striatum-lane`? Does a recycled uid need a
   generation/epoch to prevent cross-lease confusion?
6. **Credential store.** Each pool uid needs its own provider-credential store
   (`~/.claude/.credentials.json` etc.); how does the RFC 0165 spawn-time
   hydrator (#583) populate per-uid stores without N copies going stale?

## Out of scope

- This RFC does not implement the pool, the lease, or any credential code.
- It does not change RFC 0143 Slice A (the typed-exit floor), which ships
  independently per D261.
- It does not re-open the RFC 0096 / #135 / #296 session-bound token grant set;
  the per-lane uid changes *who the OS principal is*, not *what capabilities the
  session token carries*.

## Acceptance / next steps

The maintainer has ratified the **direction** (per-lane OS uid, pooled). The
**spec** — pool size, lease lifecycle, provisioning ownership, ACL and
attestation interaction — goes through a `falsification_gate` design run before
any build slice touches host provisioning or credential code. RFC 0143 Slice B
build is blocked on this RFC reaching `accepted` + at least its P0 provisioning
slice landing (tracked blocker: #585).
