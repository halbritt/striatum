---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
topic: "RFC 0168 P0 — per-lane OS uid as the lane security principal: v2 REVISION re-attack (discharge C1 lease lifecycle + scrub postcondition proof, and C2 ACL exactness, while carrying the v1-proven hard core HC-A1..A5 and the credited OQ1/OQ3/OQ5/OQ6 + narrowing invariant forward unregressed)"
participants:
  - "holder-author-001"
  - "falsifier-reviewer-001"
  - "falsifier-reviewer-002"
  - "adjudicator-author-001"
cycle: 1
entries:
  - kind: claim
    by: "holder-author-001"
    refs: ["dialogue:1"]
    text: "v2 REVISION of the RFC 0168 P0 falsification_gate spec, revising the v1 HOLDER to discharge the two cycle-1 constraints while carrying the v1-proven hard core forward unregressed. C1 (OQ2 lease lifecycle): a durable FOUR-state machine active/scrubbing/quarantined/returned in daemon-owned PostgreSQL striatumd.lane_uid_leases, a partial UNIQUE held-index uq_lane_uid_held on pool_uid WHERE state IN active|scrubbing|quarantined, a free predicate of no row in active|scrubbing|quarantined (never the v1 no-active-row), an allocate / scrub-begin / scrub-finalize THREE-transaction boundary with the side-effecting scrub strictly between txns (crash-safe; mirrors the #198 probes-out-of-tx reasoning at recovery.go:565-587), a scrub POSTCONDITION PROOF P1-P5 by /proc + socket + stat observation rather than exit codes, a leaked-active + stuck-scrubbing reaper hung off the 60s recovery sweep, a doctor surface, and exhaustion accounting that excludes scrubbing+quarantined (closing the v1 OQ1 caveat). C2 (OQ4 ACL): a hard boundary at .striatum/ — group striatum-lanes:rX on shared source/artifact only, stripped from .striatum/ and .git/ with the auditable end-state invariant that no path under .striatum/ carries a g:striatum-lanes access-or-default entry; .striatum/ reachability per-leased-uid only (--x traverse, re-keyed from scratch_acl.go), per-supervisor scratch rwx to the leased uid alone, per-job worktree chowned to the leasing uid, all per-lease ACLs removed on scrub (ties to OQ2 proof P5); plus the A16 non-exposure test over an unleased and a different-leased pool uid against the bearer/PTY-log/token-cache/foreign-worktree. Carries HC-A1..A5, OQ1/OQ3/OQ5/OQ6 and the narrowing invariant forward; closes the v1 OQ1 exhaustion-accounting caveat and OQ6 stale-store contingency. Every source citation re-verified against worktree HEAD."
  - kind: challenge
    by: "falsifier-reviewer-001"
    refs: ["dialogue:2"]
    correspondence: landed_unrebutted
    text: "C1 scrub-postcondition lens. CREDITS the v2 fix to the v1 failure boundary: the durable four-state machine, the held-unique index excluding dirty uids from allocation, scrub-begin removing the uid from the free set before any side effect, failed-proof to quarantined, quarantine surviving restart, the leaked/stuck reaper, and exhaustion excluding scrubbing+quarantined are all real and close the original no-non-free-dirty-state hole (HOLDER.md:138-168,:178-199,:248-262,:293-309). Standing material challenge (narrower than v1): P1, the load-bearing process-domain proof, does NOT actually prove the per-uid kill domain is empty. As written it blocks return only on a pool_uid process in a code-runnable state R/S/D and records Z zombies without blocking (HOLDER.md:222-227,:237-239), but Linux /proc/<pid>/status also has non-zombie T (stopped) and t (tracing-stop) states (and other non-R/S/D states). A T/t task is not a zombie and has not exited: it holds the same uid, memory, file descriptors, and HOME/credential reachability, and can be resumed with SIGCONT to run code. Concrete failing case: S1 leaves a U-owned process that is T or t; teardown enters scrubbing and correctly holds U out of the free set; the process survives the scrub command (exactly the class C1 says command success cannot prove away); P1 enumerates the U-owned PID, sees state T/t, and as written does not declare it blocking; P2-P5 all pass; tx_scrub_finalize records a clean proof and moves U to returned even though a live non-zombie U-owned process remains; a later S2 lease of U shares an OS uid domain with S1 residue — reopening the same cross-lease residue class C1 exists to close. This is exactly the postcondition the v1 ledger prescribed (zero non-zombie uid-owned processes; COLLABORATION_LEDGER_cycle_1.md:45,:250-252) and that the v2 P1 narrowed to R/S/D-only. A17 only says a surviving process fails P1 without covering the stopped/traced variant against P1's literal R/S/D enumeration. Required revision: tighten P1 to zero pool_uid-owned tasks except zombies/dead tasks that cannot execute and hold no resources — any observed non-zombie state (T, t, or unknown) must block returned and finalize as quarantined with lane_uid_scrub_failed; add TestStoppedOrTracedUIDProcessBlocksReturn (or extend A9'/A17); and record observed PIDs and /proc states in scrub_proof so doctor can distinguish tolerated Z residue from a non-zombie quarantine cause. Carry-forward check: NO regression in HC-A1..A5 or OQ1/OQ3/OQ5/OQ6/narrowing; the challenge is confined to C1's scrub postcondition predicate. needs_revision."
  - kind: challenge
    by: "falsifier-reviewer-002"
    refs: ["dialogue:3"]
    correspondence: landed_unrebutted
    text: "C2 ACL-exactness / provisioning-transition lens. CREDITS the v2 fixed FINAL ACL state: no g:striatum-lanes access-or-default entry under .striatum/ or .git, .striatum/ traverse keyed only to the currently leased uid, per-supervisor scratch ACLs scoped to that leased uid, and A16 checking both an unleased and a different-leased pool uid against the MCP bearer, PTY log, token cache, and foreign worktree (HOLDER.md:360-424). If implemented as a true allowlist, that end-state closes the original v1 C2 leak. Standing material challenge (narrower than v1): the same spec still PERMITS, and explicitly blesses as equivalent (gates on the auditable end-state, not the procedure; HOLDER.md:356-362), a recursive-root grant followed by a carve-out strip — setfacl -R -m g:striatum-lanes:rX -m d:g:striatum-lanes:rX <repoRoot>, then setfacl -R -x g:striatum-lanes -k <repoRoot>/.striatum (HOLDER.md:350-355). The first -R necessarily touches .striatum/ and adds group r to existing 0600 control-plane files BEFORE the strip runs — the HOLDER itself admits this (-x removes the group r the first -R had added). The later strip restores the correct end-state but cannot un-read a bearer or PTY log already read during the exposure window, and bearer exfiltration is IRREVERSIBLE (the session-bound bearer can be copied and replayed against the daemon as S1). A16 checks only the after-state, not the transition. Concrete failing case: an adopted repo with a live S1 lane (.striatum/scratch/<S1>/lane-mcp-config-*.json + pty.log, both 0600); a different/residual pool uid U2 loops on open(2) of S1's bearer; the operator/build runs the blessed recursive path; before the carve-out reaches .striatum/, U2 reads the bearer; the later strip makes the A16/doctor audit green while the bearer has already been exfiltrated. Source-confirmed: bearer 0600 under .striatum/scratch (mcpconfig.go:550-571), pty.log 0600 (loop.go:139-145,:298-300), the deliberately-narrow scratch carve-out (scratch_acl.go:31-48), and a host POSIX-ACL sanity check confirming setfacl -R -m g:<grp>:rX adds named-group r-- to an existing 0600 file that the later -x/-k removes. The HOLDER's rebuttal that no group ACL ever touches .striatum/ (HOLDER.md:394-402) is true only of the pure allowlist form, not of the blessed recursive form. Required revision: make the safe form MANDATORY — an allowlist / exclude-at-traversal implementation that never applies g:striatum-lanes:rX to .striatum/, .git/, or provider/token-cache paths even transiently; do NOT bless setfacl -R <repoRoot>-then-strip on a live repo; extend A16 into TestPoolACLProvisioningNeverTransientlyExposesScratch (adversarial reader before/during/after provisioning) plus an ACL-planner guard/unit test that fails if any g:striatum-lanes op targets <repoRoot> as a raw recursive root while .striatum/ exists under it. Carry-forward check: NO regression in HC-A1..A5, OQ1/OQ3/OQ5/OQ6, or the narrowing invariant; the revised C2 final state is the right shape; the challenge is confined to provisioning exactness. needs_revision."
verdict: "needs_revision"
rationale: "Adjudicates the RFC 0168 P0 v2 REVISION falsification_gate trajectory (one revised holder spec re-attacked by two independent falsifiers; the cycle ends at adjudication with no further holder turn) against the SEED clearing condition: a clearing verdict (accept / accept_with_findings) requires C1 GENUINELY DISCHARGED and C2 GENUINELY DISCHARGED, no carry-forward regressed, and no new material challenge standing unrebutted. The v2 revision is strong and genuine: it closes the exact v1 structural holes the cycle-1 ledger prescribed. For C1 I independently credit the durable four-state machine active/scrubbing/quarantined/returned, the partial UNIQUE held-index that makes a held uid host-wide-exclusive, the free predicate of no row in active|scrubbing|quarantined, the allocate/scrub-begin/scrub-finalize three-transaction boundary (scrub strictly between txns, crash-safe, mirroring the #198 probes-out-of-tx reasoning at recovery.go:565-587), the leaked-active + stuck-scrubbing reaper hung off the 60s recovery sweep (recovery.go:553, HandleRecoveryAuto), the doctor surface, quarantine surviving a boot-epoch rotation (the free set DERIVED from PostgreSQL, never memory; main.go:722/731/665-690), and exhaustion accounting that counts scrubbing+quarantined consumed (closing the v1 OQ1 caveat). For C2 I credit the correct final ACL end-state — the hard .striatum/ boundary invariant (no g:striatum-lanes access-or-default entry under .striatum/), the per-leased-uid --x traverse re-keyed from scratch_acl.go:42-49, the rwx pushed down to .striatum/scratch/<supervisor_id> for the leased uid alone, the chowned per-job worktree, all per-lease ACLs removed on scrub (tied to OQ2 P5), and an A16 non-exposure test over an unleased AND a different-leased pool uid against the bearer/PTY-log/token-cache/foreign-worktree. BUT the gate does NOT clear: TWO new material, source-confirmed challenges land, each inside a C1/C2 clearing requirement, and BOTH stand unrebutted (the holder had no further turn). NEITHER C1 NOR C2 IS GENUINELY DISCHARGED — each retains a narrow but verdict-driving hole that maps directly onto a named SEED needs_revision trigger. C1 (falsifier-reviewer-001): the scrub POSTCONDITION PROOF — the single most central element of C1 — is incomplete at its process-domain predicate. P1 blocks return only on R/S/D and tolerates Z, but Linux /proc state also has non-zombie T (stopped) and t (tracing-stop) tasks that hold the uid, memory, FDs, and HOME/credential reachability and can be resumed to run code. A stopped/traced survivor passes P1, P2-P5 pass, tx_scrub_finalize records a clean proof, and the uid reaches returned and is re-leased — a same-uid cross-lease residue re-lease, the exact class RFC 0168 exists to eliminate, and the exact named trigger dirty uid can be re-leased. The v1 ledger prescribed precisely zero non-zombie uid-owned processes (COLLABORATION_LEDGER_cycle_1.md:45,:250-252); the v2 P1 narrowed it to R/S/D-only, so the postcondition the build would execute admits a dirty return. I confirmed against tmux_liveness.go:576-591 that the codebase's existing /proc state handling is binary (Z or not), so an implementer wiring P1 off this would let a T/t survivor through — the gap is buildable, not theoretical. C2 (falsifier-reviewer-002): the spec gates on the auditable end-state, not the procedure (HOLDER.md:356-362) and blesses a recursive-root grant setfacl -R -m g:striatum-lanes:rX <repoRoot> followed by a carve-out strip (HOLDER.md:350-355). The first -R necessarily adds group r to existing .striatum/ 0600 control-plane files before the strip — the HOLDER itself admits this — so during provisioning on a live repo .striatum/ is transiently group-readable (the named trigger .striatum/ still group-readable), and a bearer read in that window is an irreversible exfiltration replayable as another lane. A16 tests only the after-state, not the transition, so the required non-exposure test does NOT catch the leak. The HOLDER's own rebuttal that no group ACL ever touches .striatum/ (HOLDER.md:394-402) is internally contradicted by its blessed recursive procedure (it is true only of the pure allowlist form). I confirmed the recursive-root pattern against repo_acl.go:25-31 (setRepoACL is setfacl -R over repoRoot + .striatum/worktrees) and the exposed 0600 bearer/PTY-log against mcpconfig.go:241,266 and loop.go:145,300. CARRY-FORWARD: INTACT and unregressed. Both falsifiers independently report NO regression in HC-A1..A5, OQ1/OQ3/OQ5/OQ6, or the narrowing invariant; I concur — the hard core is carried verbatim from v1 (re-verified against pty.go/tmux_liveness.go HEAD per the HOLDER table), and the v1 OQ1 exhaustion-accounting caveat and OQ6 stale-store contingency are positively CLOSED by the C1 structural fix (improvements, not regressions). NEW CHALLENGES: both STAND (material, source-confirmed, unrebutted). Why not accept_with_findings: each gap is a no-replay/soundness defect inside a C1/C2 clearing requirement and maps onto a named needs_revision trigger (a dirty uid that can be re-leased; .striatum/ that is transiently group-readable) — exactly the post-clearance-finding exclusion the v1 ledger applied to the same two constraint classes; neither is trackable polish. Why not reject: no path widens admin-token exposure, no minted credential carries an elevated verb, no lane-readable shared reseal bearer exists; both falsifiers confirm the no-widening invariant and the per-lane-uid direction (D261) and the proven hard core hold; both required corrections are narrow and precisely specified. Per-constraint record: C1 NOT RESOLVED (structural machine RESOLVED; scrub-postcondition proof predicate OPEN — under-classifies T/t). C2 NOT RESOLVED (final ACL end-state RESOLVED; provisioning-transition exactness OPEN — blessed recursive path transiently exposes; A16 after-state-only). Carry-forwards INTACT: HC-A1..A5 INTACT, OQ1 INTACT (+caveat closed), OQ3 INTACT, OQ5 INTACT, OQ6 INTACT (+contingency closed), narrowing INTACT. Verdict: needs_revision. Gate-cycle consequence: per the SEED this was the single allowed v2 revision cycle; this second needs_revision ENDS THE GATE UNCLEARED and routes to the operator for a fresh -v3 run with a revising holder. The two fixes are small and load-bearing: (C1) make P1 zero non-zombie uid-owned tasks + add TestStoppedOrTracedUIDProcessBlocksReturn + record observed PIDs/states in scrub_proof; (C2) make the allowlist/exclusion provisioning MANDATORY (never apply the group ACL to .striatum/ even transiently) + extend A16 into a before/during/after transition test + add an ACL-planner guard. The maintainer-ratified direction (D261) carries regardless; adjudicator clearance gates the spec's soundness, not the product call."
findings:
  - id: HC-ORACLE-INTACT
    severity: high
    posture: design
    status: accepted
    source_refs: ["dialogue:1", "dialogue:2", "dialogue:3"]
    affected_invariants:
      - "structural no-replay: a per-lane uid dissolves the BC1-W1-ORACLE same-uid tmux/0600/ptrace replay class on this host (Yama ptrace_scope=1) — CARRIED FORWARD UNREGRESSED from v1"
    challenge: "INTACT (carry-forward). The v1-proven hard core HC-A1..A5 (per-uid tmux 0700 socket so a sibling cannot respawn-pane the target pane; cross-uid 0600 DAC; cross-uid ptrace/setns//proc denial by ptrace_may_access — the exact axis namespace-inode failed under D261; SO_PEERCRED uid discriminator; every residual same-uid surface closed) is carried into v2 verbatim (HOLDER §Part 1, re-verified against pty.go/tmux_liveness.go HEAD). Both falsifiers independently re-checked and found NO regression. The two C1/C2 rewrites only remove surface; they do not touch the launch path or the structural claim. Not reopened, not regressed."
  - id: OQ2-SCRUB-POSTCONDITION
    severity: high
    posture: design
    status: open
    source_refs: ["dialogue:1", "dialogue:2"]
    affected_invariants:
      - "no cross-lease same-uid residue: a returned uid must be PROVABLY empty before re-lease — the scrub postcondition must prove zero non-zombie uid-owned tasks, not merely zero R/S/D tasks"
    challenge: "OPEN — verdict-driving (falsifier-reviewer-001), C1 NOT genuinely discharged. The durable state machine half of C1 is RESOLVED (four states active/scrubbing/quarantined/returned, partial held-unique index, free predicate excludes scrubbing+quarantined, allocate/scrub-begin/scrub-finalize transaction boundary, leaked-active+stuck-scrubbing reaper, doctor surface, quarantine-survives-restart, exhaustion excludes dirty uids — all credited). But the scrub POSTCONDITION PROOF, the heart of C1, is incomplete: P1 blocks return only on a pool_uid process in R/S/D and tolerates Z (HOLDER.md:222-227,:237-239), while Linux /proc/<pid>/status also has non-zombie T (stopped) and t (tracing-stop) tasks that hold the uid, memory, FDs, and HOME/credential reachability and can be SIGCONT-resumed to run code. A T/t survivor passes P1; P2-P5 pass; tx_scrub_finalize records a clean proof; the uid reaches returned and is re-leased to a later session sharing S1 residue — the exact cross-lease residue re-lease C1 exists to close, matching the named SEED needs_revision trigger dirty uid can be re-leased. The v1 ledger prescribed zero NON-ZOMBIE uid-owned processes (COLLABORATION_LEDGER_cycle_1.md:45,:250-252); v2 narrowed it to R/S/D-only. Confirmed buildable against tmux_liveness.go:576-591 (existing /proc state handling is binary Z-or-not). Required revision (v3): P1 = zero pool_uid-owned tasks except zombies/dead tasks that cannot execute and hold no resources (any non-zombie state — T, t, or unknown — blocks returned and finalizes quarantined with lane_uid_scrub_failed); add TestStoppedOrTracedUIDProcessBlocksReturn (or extend A9'/A17); record observed PIDs+/proc states in scrub_proof. needs_revision, not accept_with_findings (a state machine that can re-lease a dirty uid is a soundness defect inside the C1 gate)."
  - id: OQ4-ACL-PROVISIONING-TRANSITION
    severity: high
    posture: design
    status: open
    source_refs: ["dialogue:1", "dialogue:3"]
    affected_invariants:
      - "ACL exactly-enough without over-grant: .striatum/ control-plane (MCP bearer, PTY log, token cache, foreign worktree) must be unreadable by a non-leased pool uid at ALL times — before, during, AND after provisioning — not merely in the final audited end-state"
    challenge: "OPEN — verdict-driving (falsifier-reviewer-002), C2 NOT genuinely discharged. The final ACL end-state half of C2 is RESOLVED (hard .striatum/ boundary invariant, per-leased-uid --x traverse re-keyed from scratch_acl.go, per-supervisor scratch rwx to the leased uid only, chowned worktree, per-lease ACLs removed on scrub, A16 over an unleased and a different-leased uid — all credited). But the spec gates on the auditable end-state, not the procedure (HOLDER.md:356-362) and BLESSES a recursive-root grant setfacl -R -m g:striatum-lanes:rX <repoRoot> followed by a strip (HOLDER.md:350-355). The first -R necessarily adds group r to existing .striatum/ 0600 control-plane files before the strip — the HOLDER admits this — so during provisioning on a live repo .striatum/ is TRANSIENTLY group-readable (the named SEED trigger .striatum/ still group-readable), and a bearer read in that window is an irreversible exfiltration replayable as another lane. A16 tests only the after-state, so the required non-exposure test does not catch the leak. The HOLDER's rebuttal that no group ACL ever touches .striatum/ (HOLDER.md:394-402) is internally contradicted by its blessed recursive procedure (true only of the pure allowlist form). Confirmed against repo_acl.go:25-31 (recursive -R over repoRoot), mcpconfig.go:241,266 and loop.go:145,300 (bearer/PTY-log 0600 under .striatum/scratch). Required revision (v3): make the allowlist/exclude-at-traversal form MANDATORY — never apply g:striatum-lanes:rX to .striatum/, .git/, or provider/token-cache paths even transiently; do NOT bless recursive-root-then-strip on a live repo; extend A16 into TestPoolACLProvisioningNeverTransientlyExposesScratch (adversarial reader before/during/after); add an ACL-planner guard that fails if any g:striatum-lanes op targets <repoRoot> as a raw recursive root while .striatum/ exists under it. needs_revision, not accept_with_findings (a transient bearer exposure is a no-replay/soundness defect inside the C2 gate)."
  - id: OQ-CREDITED-SET-INTACT
    severity: high
    posture: design
    status: accepted
    source_refs: ["dialogue:1", "dialogue:2", "dialogue:3"]
    affected_invariants:
      - "the v1-credited resolved set carried into v2 unregressed: OQ1 sizing+fail-closed exhaustion, OQ3 launch-as-only provisioning, OQ5 generation token, OQ6 hydration shape, restart-survival, and the narrowing invariant"
    challenge: "INTACT (carry-forward, v1 caveats closed). Both falsifiers report NO regression and I concur. OQ1: host-global ceiling + typed fail-closed lane_uid_pool_exhausted refuse-and-requeue is carried, and its v1-flagged caveat (exhaustion accounting reads the quarantine state) is CLOSED — free = N − active − scrubbing − quarantined, with A20 asserting exhaustion fires at the reduced ceiling. OQ3: static host-runbook pool, daemon holds only the launch-as (%striatum-lanes) grant and the new scrub uses only that grant — no useradd/userdel authority — carried unregressed (A12). OQ5: leased-uid + monotonic per-uid generation token minted in tx_alloc, compared on every attestation AND control-frame path — carried (A14). OQ6: per-spawn per-uid hydration into the leased uid's 0600 store scrubbed on return, with its v1 stale-store contingency CLOSED by C1 proof P3 (a failed P3 quarantines) — carried+extended (A15). Restart-survival: the free set is DERIVED from PostgreSQL (never memory), quarantine survives a boot-epoch rotation. NARROWING confirmed: both C1 (more state, a proof, a reaper) and C2 (a tighter ACL) only remove surface; no new authority. Do NOT reopen or regress in v3."
branches:
  design: blocked
---

# Collaboration Ledger — RFC 0168 P0 design (v2 REVISION, cycle 1)

author: adjudicator-author-001

> Adjudication of the RFC 0168 P0 **v2 REVISION** `falsification_gate` dialogue
> trajectory (*per-lane OS uid as the lane security principal*). The v2 revision
> set out to discharge the two source-anchored constraints the v1 cycle-1
> ledger left open — **C1** (`OQ2` lease lifecycle / scrub postcondition) and
> **C2** (`OQ4` ACL exactness) — while carrying the v1-proven hard core and the
> credited open-question set forward unregressed. The direction (a pre-provisioned
> pool of per-lane uids, leased per lane) is maintainer-ratified (**D261**,
> 2026-06-24) and is **not** relitigated. Inputs read: the revised Holder spec
> (`rfc-0168-design-v2/dialogue/holder/HOLDER.md`), both v2 falsifier re-attacks
> (`dialogue/falsifier_1/FALSIFIER.md`, `dialogue/falsifier_2/FALSIFIER.md`), the
> v2 `SEED.md` charter, RFC 0168, and — as required context for what the revision
> had to fix — the v1 Holder
> (`rfc-0168-design/dialogue/holder/HOLDER.md`) and the v1 cycle-1 adjudicator
> ledger (`rfc-0168-design/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md`).
> No raw terminal output was read. Load-bearing source citations were
> independently re-verified against the current worktree HEAD.

## Verdict

**verdict: needs_revision**

The v2 revision is a strong, genuine pass: it closes the **structural** v1 holes
exactly as the cycle-1 ledger prescribed. But **two new, material,
source-confirmed challenges land — each inside a C1/C2 clearing requirement — and
both stand unrebutted** (one revised-holder turn, two independent falsifier
re-attacks; the cycle ends at adjudication). **Neither C1 nor C2 is genuinely
discharged**: each retains a narrow but verdict-driving hole that maps directly
onto a named SEED `needs_revision` trigger.

- **C1** — the durable state machine is fixed, but the **scrub postcondition
  proof** (the heart of C1) under-classifies a surviving non-zombie process, so a
  **dirty uid can still be re-leased**.
- **C2** — the final ACL end-state is correct, but the spec **blesses a
  recursive-root provisioning procedure that transiently makes `.striatum/`
  group-readable**, and the required non-exposure test checks only the
  after-state.

**Why not `accept_with_findings`.** Each gap is a no-replay / soundness defect
*inside* a C1/C2 clearing requirement, not trackable post-clearance polish: a
state machine that can re-lease a **dirty** uid (the SEED trigger *"dirty uid can
be re-leased"*) and a provisioning path that transiently exposes a control-plane
**bearer** (the SEED trigger *"`.striatum/` still group-readable"*). The v1 ledger
applied exactly this exclusion to the same two constraint classes; consistency and
the contract both foreclose a clearing verdict.

**Why not `reject`.** No path widens admin-token exposure, no minted credential
carries an elevated verb, and no lane-readable shared reseal bearer exists. Both
falsifiers confirm the no-widening invariant; the per-lane-uid direction (D261)
and the proven hard core hold. Both required corrections are **narrow and
precisely specified** — this is a soundness gap, not a wrong design.

## Per-constraint discharge record (the SEED clearing axes)

| Constraint | Structural half | The remaining hole | Disposition |
| --- | --- | --- | --- |
| **C1 / `OQ2` lease lifecycle + scrub postcondition** | **RESOLVED** — durable four-state machine `active/scrubbing/quarantined/returned`; partial held-unique index; free predicate excludes `scrubbing`+`quarantined`; allocate/scrub-begin/scrub-finalize transaction boundary (crash-safe); leaked-active + stuck-scrubbing reaper; doctor surface; quarantine-survives-restart; exhaustion excludes dirty uids | **OPEN** — P1 blocks return only on `R/S/D` and tolerates `Z`, silent on non-zombie `T`/`t`; a stopped/traced survivor passes the proof → `returned` → **re-leased** | **NOT genuinely discharged — `needs_revision`** |
| **C2 / `OQ4` ACL exactness** | **RESOLVED** — hard `.striatum/` boundary invariant; per-leased-uid `--x` traverse; per-supervisor scratch `rwx` to the leased uid only; chowned worktree; per-lease ACLs removed on scrub; `A16` over an unleased **and** a different-leased uid | **OPEN** — spec *blesses* `setfacl -R …:rX <repoRoot>` then strip; the first `-R` transiently adds group `r` to existing `.striatum/` `0600` files (HOLDER admits it); `A16` tests only the after-state | **NOT genuinely discharged — `needs_revision`** |

## Carry-forward record (must stay INTACT and unregressed)

| Carry-forward | Status |
| --- | --- |
| **Hard core HC-A1..A5** (per-uid tmux socket; cross-uid `0600` DAC; cross-uid `ptrace`/`setns`//proc denial; `SO_PEERCRED` discriminator; residual-surface closure) | **INTACT** — carried verbatim from v1 §Part 1; both falsifiers re-checked, no regression |
| **OQ1** host-global ceiling + typed fail-closed exhaustion | **INTACT** — and the v1 exhaustion-accounting caveat is **positively CLOSED** (free excludes `scrubbing`+`quarantined`; `A20`) |
| **OQ3** launch-as-only provisioning, no `useradd`/`userdel` | **INTACT** — the new scrub uses only the launch-as grant the daemon already holds |
| **OQ5** leased-uid + monotonic generation token | **INTACT** — minted in `tx_alloc`; compared on every attestation **and** control-frame path |
| **OQ6** per-spawn per-uid hydration, scrubbed on return | **INTACT** — and its v1 stale-store contingency is **CLOSED** by C1 proof P3 |
| **Narrowing invariant** (no new authority) | **INTACT** — both C1 and C2 only **remove** surface |

## The two standing grounds (independently confirmed against the worktree)

### CHALLENGE 1 — `OQ2-SCRUB-POSTCONDITION`: the proof under-classifies a non-zombie survivor (verdict-driving)

The v2 holder did the hard structural work C1 demanded — and I credit it. The
remaining hole is exactly where C1 is load-bearing: the **postcondition proof**.
P1 asserts *"no `pool_uid`-owned process in a code-runnable state (`R`/`S`/`D`)
remains"* and records `Z` zombies without blocking (`HOLDER.md:222-227,:237-239`).
But Linux `/proc/<pid>/status` also reports non-zombie **`T`** (stopped) and
**`t`** (tracing-stop) tasks. A stopped/traced task is **not** a zombie and has
**not** exited: it holds the same uid, memory, file descriptors, and
HOME/credential reachability, and can be `SIGCONT`-resumed to run code. As
written, such a survivor passes P1, P2–P5 pass, `tx_scrub_finalize` records a
clean proof, and the uid reaches `returned` and is re-leased — a same-uid
cross-lease residue re-lease, the precise class RFC 0168 exists to eliminate and
the named SEED trigger *"dirty uid can be re-leased."*

The v1 ledger prescribed the correct predicate — *"zero non-zombie uid-owned
processes"* (`COLLABORATION_LEDGER_cycle_1.md:45,:250-252`) — and the v2 P1
narrowed it to `R/S/D`-only. I confirmed the gap is **buildable, not
theoretical**: the codebase's existing `/proc` state handling
(`processZombie`, `tmux_liveness.go:576-591`) reasons about the state field as a
single character (`Z` or not), so an implementer wiring P1 off that helper would
let a `T`/`t` survivor through. `A17` only says *"a surviving process"* fails P1
without binding it to a predicate that covers the stopped/traced variant.

### CHALLENGE 2 — `OQ4-ACL-PROVISIONING-TRANSITION`: a blessed procedure transiently exposes `.striatum/` (verdict-driving)

The v2 holder fixed the **final** ACL state — and I credit it (the
`.striatum/`-boundary invariant, per-leased-uid `--x`, per-supervisor scratch,
chowned worktree, all removed on scrub, plus an `A16` that checks both an
unleased and a different-leased uid). The hole is that the spec *"gates on the
auditable end-state, not the procedure"* (`HOLDER.md:356-362`) and **blesses** a
recursive-root grant `setfacl -R -m g:striatum-lanes:rX <repoRoot>` followed by a
carve-out strip (`HOLDER.md:350-355`). The first `-R` necessarily adds group `r`
to existing `.striatum/` `0600` control-plane files **before** the strip runs —
the holder itself admits this. So on a live repo, `.striatum/` is **transiently
group-readable** during provisioning (the named SEED trigger *"`.striatum/` still
group-readable"*), and a bearer read in that window is an **irreversible**
exfiltration replayable as another lane. `A16` checks only the after-state, so the
required non-exposure test does **not** catch the leak.

I confirmed the source anchors: the recursive helper is `setfacl -R` over
`repoRoot` (+ `.striatum/worktrees`) at `repo_acl.go:25-31`; the exposed material
is the `0600` MCP bearer (`mcpconfig.go:241,266`) and the `0600` `pty.log`
(`loop.go:145,300`) under `.striatum/scratch/<supervisor_id>/`; the deliberately
narrow carve-out the recursion violates is `scratch_acl.go:42-48` (`.striatum`
→ `u:<lane>:--x`, *"never broaden read access to private operator state"*). The
holder's rebuttal that *"no group ACL ever touches `.striatum/`"*
(`HOLDER.md:394-402`) is **internally contradicted** by its own blessed
procedure — it is true only of the pure allowlist form.

## Falsifier challenge dispositions

- **falsifier-reviewer-001 — C1 scrub postcondition (material; landed
  unrebutted).** Credits the durable state-machine fix; lands a narrower but
  verdict-driving gap in P1's process predicate. Material? **Yes** — it permits a
  dirty re-lease, the exact class C1 closes, and would change the spec (tighten
  P1, add a stopped/traced test, record `/proc` states). Rebutted? **No** — no
  further holder turn. Disposition: **`OQ2-SCRUB-POSTCONDITION` open;
  verdict-driving; `needs_revision`.**
- **falsifier-reviewer-002 — C2 provisioning transition (material; landed
  unrebutted).** Credits the correct final ACL state; lands a narrower but
  verdict-driving gap in the blessed provisioning procedure. Material? **Yes** —
  bearer exfiltration is irreversible and the spec blesses the transiently-leaking
  path while the required test checks only the after-state. Rebutted? **No** — the
  holder's standing rebuttal is internally contradicted by its own procedure.
  Disposition: **`OQ4-ACL-PROVISIONING-TRANSITION` open; verdict-driving;
  `needs_revision`.**

Both falsifiers **credit the structural fixes** and then land **distinct,
independent, source-confirmed** new grounds in **different** constraints (C1's
proof predicate vs C2's provisioning transition) — corroboration that each
residue is genuine, not reviewer idiosyncrasy.

## What v3 must fix to clear on re-attack

Retain the **entire** v2-delivered structure (the proven hard core; the durable
four-state lease machine + held-unique index + transaction boundary + reaper +
doctor surface + quarantine-survives-restart + dirty-excluding exhaustion; the
correct final ACL end-state + the `A16` after-state test; OQ1/OQ3/OQ5/OQ6 + the
narrowing invariant). Then, the two narrow corrections:

1. **(`OQ2-SCRUB-POSTCONDITION`) Tighten P1 to the prescribed predicate.** After
   bounded re-kill/re-probe, require **zero `pool_uid`-owned tasks except
   zombies/dead tasks that cannot execute and hold no resources**. Any observed
   non-zombie state — `T`, `t`, or any unknown state — **blocks** `returned` and
   finalizes `quarantined` with `lane_uid_scrub_failed`. Add
   `TestStoppedOrTracedUIDProcessBlocksReturn` (or extend `A9'`/`A17`) injecting a
   stopped/traced survivor and asserting quarantine + non-allocation +
   restart-preserved quarantine. Record observed PIDs and `/proc` states in
   `scrub_proof` so `doctor` can distinguish tolerated `Z` residue from a
   non-zombie quarantine cause.
2. **(`OQ4-ACL-PROVISIONING-TRANSITION`) Make the safe form mandatory.** The pool
   group grant **must** be an allowlist / exclude-at-traversal implementation that
   **never** applies `g:striatum-lanes:rX` to `.striatum/`, `.git/`, or
   provider/token-cache paths — even transiently. Do **not** bless
   `setfacl -R …:rX <repoRoot>` followed by a strip as an acceptable live-repo
   path. Extend `A16` into a **transition** test
   (`TestPoolACLProvisioningNeverTransientlyExposesScratch`): an unleased and a
   different-leased uid attempt `open(2)`/traversal **before, during, and after**
   provisioning, and **no** read succeeds. Add an ACL-planner guard / unit test
   that fails if any `g:striatum-lanes` operation targets `<repoRoot>` as a raw
   recursive root while `.striatum/` exists under it.

Everything else carries forward **unchanged** — do **not** reopen the hard core,
the durable lease machine, the final ACL state, OQ1/OQ3/OQ5/OQ6, or the narrowing
invariant.

## Gate-cycle note

Per the SEED, this was the **single allowed v2 revision cycle**. This second
`needs_revision` (v1 was the first) **ends the gate uncleared** and routes to the
**operator**, who spins a fresh **`-v3`** run with a revising holder to land the
two narrow corrections above. The maintainer-ratified **direction** (per-lane
pooled OS uid, D261) carries regardless of this verdict; adjudicator clearance
gates the **spec's soundness**, not the product call.

---
<sub>Adjudicator collaboration ledger for the RFC 0168 P0 `falsification_gate`
design run (**v2 REVISION**, cycle 1). The ledger verdict — not falsifier
completion — gates the phase: `needs_revision` returns the spec uncleared. v2
**genuinely closes the v1 structural holes** (the durable four-state lease machine
with a held-unique index, transaction boundary, reaper, doctor surface,
quarantine-survives-restart, and dirty-excluding exhaustion; and the correct final
`.striatum/`-excluding ACL end-state with the `A16` non-exposure test) and carries
the proven hard core + OQ1/OQ3/OQ5/OQ6 + the narrowing invariant forward
unregressed (closing the v1 OQ1 exhaustion caveat and OQ6 stale-store contingency).
But **two narrow, material, source-confirmed challenges stand unrebutted, one
inside each constraint**: C1's scrub postcondition (P1) admits a non-zombie `T`/`t`
survivor and re-leases a dirty uid; C2's blessed recursive-root provisioning path
transiently makes `.striatum/` group-readable on the `0600` bearer before the
strip, and `A16` tests only the after-state. No admin-token widening, no
lane-readable shared reseal bearer, no elevated credential — `needs_revision`, not
`reject`; both gaps are no-replay/soundness defects inside the C1/C2 clearing
requirements, so not `accept_with_findings`. Single allowed v2 cycle exhausted:
the gate ends uncleared and routes to the operator for a `-v3` run.</sub>
