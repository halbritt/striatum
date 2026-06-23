# FALSIFIER - RFC 0143 design-v3 security/authz re-attack

author: falsifier-reviewer-003

## Verdict

The v3 holder is materially stronger than design-v2. It no longer relies on parsed
PTY output as the primary control signal, moves `CapabilityReseal` to a
daemon-internal projection, derives artifact identity from daemon state, and keeps
F2's bearer-file retirement plus F4's narrow route-alternate shape.

It still should not clear. BC1 is now specified as an inherited-fd control channel
plus reserved wrapper exit codes, but the same-uid replay claim is not proven. The
seed explicitly says all lanes currently share the `striatum-lane` OS user and that
same-uid readability killed a `0600` reseal file (`SEED.md:69-75`). The v3 holder
answers by saying fd 3 has no filesystem name, so a sibling lane has nothing to
open (`HOLDER.md:183-191`), and by putting `control_nonce` in the wrapper env
(`HOLDER.md:205-210`). That is not yet a security boundary: the spec does not
state any procfs/ptrace/dumpability, peer-credential, per-lane-uid, or helper-owned
signing rule preventing a same-uid sibling from duplicating the wrapper's live fd
via `/proc/<pid>/fd/3` or reading the nonce from `/proc/<pid>/environ` and sending
a valid `reseal_requested` or `unrecoverable_across_rotation` frame.

## BC1 / BC2 / BC3 Check

- **BC1 - still open.** The design correctly rejects parsed terminal output. Current
  source confirms `HelperControlEvent` carries lifecycle metadata and byte counts
  only, with agent output bytes excluded (`go/pkg/supervisor/helper_protocol.go:41-44`);
  `RunHelper` only moves process bytes and reports control events (`helper.go:120-127`);
  and `pumpPTYProgress` watches output volume, not content (`helper.go:357-415`).
  But v3's new fd/nonce channel is asserted to be sibling-lane-unreachable without
  specifying the same-uid process boundary that makes that true.
- **BC2 - resolved at design level, contingent on BC1.** The v3 holder says a reseal
  signal carries no path or body, derives identity from `jobs.expected_artifacts_json`,
  verifies required artifacts, and refuses unexpected paths (`HOLDER.md:231-268`).
  That is the right artifact-identity rule. It only remains unsafe if BC1 lets a
  sibling lane forge the signal for the victim's own in-flight job.
- **BC3 - resolved at design level, but not implemented in current source.** The
  holder declares `CapabilityReseal` a daemon-internal marker projected by private
  `resealInFlightJob`, while keeping the public route alternate for tests only
  (`HOLDER.md:270-318`). That answers the v2 public-bearer confusion. A current
  `go/` search finds no `CapabilityReseal`, `resealInFlightJob`, control-fd event,
  or named v3 proof tests yet, so the tests do not actually fire today; they are
  still proposed gates for the build slice.
- **F2 - not regressed on the file finding.** There is still no lane-readable reseal
  bearer file in the v3 design. The residual replay risk has moved to fd/nonce
  possession and belongs under BC1, not the retired-file finding.
- **F4 - not regressed at design level.** The route alternate remains limited to
  `interrogation.answer`, `work.complete`, and `artifact.publish`, with production
  reseal projected internally rather than presented as a bearer (`HOLDER.md:295-307`).

## C1 - Same-uid fd/nonce replay is still a material BC1 gap

**Precise claim attacked.** The holder says the private control channel is
"un-spoofable by the provider AND by same-uid sibling lanes" because the helper
creates a `socketpair(2)`, passes one end as fd 3 to the wrapper, closes it across
the provider exec, and gives the channel no filesystem pathname (`HOLDER.md:176-195`).
It uses a per-launch nonce in the wrapper environment as replay protection
(`HOLDER.md:205-210`).

**Concrete refutation.** "No filesystem pathname" is not equivalent to "not
reachable by same-uid processes" on Linux. A sibling lane running as the same OS
user can often inspect another same-uid lane's `/proc/<pid>/fd` entries and
environment unless the launched process is made non-dumpable or otherwise protected
by procfs/ptrace policy. The spec names neither. Because the frame contains only
`schema_version`, `type`, `supervisor_id`, and `control_nonce` (`HOLDER.md:199-204`),
a sibling that duplicates fd 3 and reads the nonce can submit a syntactically valid
frame for the victim supervisor. BC2 limits this to the victim's own in-flight job,
but it still lets a sibling lane falsely publish/complete/answer or record the
typed blocker for work it does not own. That is exactly the same class of same-uid
replay concern that retired the `0600` file, just moved from a durable file to a
live descriptor and environment variable.

**Why the named tests would miss it.** The holder's BC1 tests prove PTY/stdout bytes
cannot drive control and that a provider child without fd 3 cannot signal
(`HOLDER.md:218-228`). The falsifiable assertions mention sibling callers only in
broad terms (`HOLDER.md:533-548`), but no test is named for `/proc/<pid>/fd` descriptor
borrowing, nonce exposure through `/proc/<pid>/environ`, or a same-uid foreign
process writing a valid frame on the victim's fd.

**Strongest rebuttal for the holder.** The implementation can close this by setting
the wrapper non-dumpable before fd/nonce exposure, avoiding nonce-in-environment,
using a helper-owned authenticated channel whose peer identity is kernel-checked
(e.g. credential-passing semantics where applicable), or giving lanes separate OS
uids. It can also make the nonce single-use and bind it to a stamped generation.
Those are plausible repairs, but they are not currently in the v3 contract.

**Gap remains.** Yes. BC1 should require a same-uid replay defense and a test such
as `TestSiblingLaneCannotBorrowSupervisorControlFDOrNonce`. Until then, the no-replay
invariant is not structurally proven.

## C2 - Reserved exit codes need wrapper/provider status separation

**Precise claim attacked.** The holder reserves wrapper exit codes 97 and 98 for
unrecoverable and reseal requests, and says the provider, as the wrapper's child,
cannot set the wrapper's exit code (`HOLDER.md:141-172`). Current helper source does
record only process exit status, not output (`helper.go:427-438`), which is the right
signal class.

**Concrete refutation.** A wrapper commonly propagates or maps the provider child's
exit status. If the provider exits 97 or 98 and the wrapper blindly returns that
status, the helper still sees a trusted wrapper exit code and the daemon cannot
separate "wrapper intentionally requested reseal" from "provider/tool process
returned a colliding code." Prompt-injected text cannot forge the status, but a
local command or provider failure mode can collide with the reserved values unless
the wrapper masks child statuses or reserves its own sentinel namespace explicitly.

**Strongest rebuttal for the holder.** The design can require the agentloop wrapper
to own the reserved range and remap provider child statuses 97/98 to ordinary
`agent_exited` failures. That would be enough for the Slice-A floor.

**Gap remains.** Smaller than C1, but still part of BC1. Add the remap rule and a
negative test that a provider child exit 97/98 does not trigger the reserved control
paths.

## Test Verification

The packet asked whether `TestPTYOutputCannotEmitSupervisorControlEvent`,
`TestProviderOutputCannotDriveResealOrBlocker`,
`TestAgentLoopReservedExitCodeRoutesUnrecoverableWithoutPTYParsing`, and
`TestCodexResealUsesReceiverNotProviderStdout` actually fire. In the current
worktree, exact searches under `go/` found no matches for those tests, nor for
`CapabilityReseal`, `resealInFlightJob`, `HelperEventResealRequested`, or the
new control-fd symbols. That is acceptable only if v3 is treated as a proposed
implementation spec; it is not evidence that the safety claims are already wired.
The build slice must add these tests plus the same-uid fd/nonce replay test above.

## Recommendation

Return `needs_revision`. Preserve the v3 direction, F2 file-bearer retirement, BC2
artifact-state derivation, and BC3 daemon-internal principal model. Require the next
revision to specify the same-uid process boundary for the inherited-fd channel,
keep the control nonce out of same-uid-readable process state or replace it with a
kernel-authenticated peer check, add explicit sibling-lane replay tests, and define
wrapper remapping for provider child exit codes 97/98.
