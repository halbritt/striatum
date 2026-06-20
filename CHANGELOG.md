# Changelog

## Unreleased

### Fixed

- **RFC 0141 `verifier run` builtins are now actually runnable end-to-end** (the
  shipped unit tests passed but the live sandbox path was broken — found by
  dogfooding). Five fixes: (1) `verifier run --check-id builtin:*` no longer
  requires `--allowlist` (it contradicted "runnable with no operator JSON"), and a
  new `--intent [--pins --attest]` resolves external two-layer-allowlist checks;
  (2) builtin tools resolve to an ABSOLUTE host path so they are found regardless of
  the fixed sandbox `PATH` (`go` may live in `~/.local/bin`); (3) go builtins point
  `GOCACHE`/`GOPATH`/`HOME` at the writable scratch (the sandbox binds cwd read-only)
  and `GOMODCACHE` at the host cache with `GOPROXY=off` (offline); (4) `go build`'s
  output is redirected into scratch with `-o` (it otherwise writes the binary into
  the read-only cwd for a single-main-package module); (5) `GOTOOLCHAIN=auto` so a
  project requiring a newer `go` than the host's base binary uses the already-cached
  toolchain offline. Verified live: all four builtins pass under a strict bubblewrap
  envelope on a valid module (capped at ASSERTED), plus a new live regression test.

### Added

- **RFC 0136 P1 — event-chain segment sealing (#387, D242).** Generalizes the
  `audit_segments` "seal + boundary-hash + retention_state" model to the
  per-repository event chain so an event partition can be SEALED and
  proven-continuous BEFORE any future partition DROP (the chain-safe retention
  boundary P2/P4 depend on). New **runtime** migration
  `0041_event_chain_segments.sql` adds the per-`repository_id`
  `striatumd.event_chain_segments` ledger (append-only via refuse-triggers,
  explicit `striatumd_rw` GRANT + `REVOKE DELETE`, **no FK into owner-held
  `events`** — integrity enforced in Go per D215). New `pkg/mutations`
  `SealEventChainSegment` closes the open segment with its first/last event +
  hash boundaries and opens a chained successor (cross-segment hash witnesses for
  seam continuity), with the boundary referential-integrity check in Go. New
  doctor invariant `event_chain_segment_seam_unproven`
  (`go/pkg/reads/doctor_event_chain_segment.go`) reds when a sealed segment lacks
  its boundary hashes or its seam witnesses do not link to its predecessor.
  Corrects the RFC's stale P2/P3 owner-bundle reference `0016`→`0020`. P2–P5 are
  NOT implemented; #387 stays open as their tracker. No owner-bundle, RPC, or
  daemon restart.
- **RFC 0042 run-list workflow identity in the Go SSE dashboard (D224, #400).**
  The `status` read now folds a curated `workflow_name` onto every run row
  (`workflow_snapshots.workflow_id`, suffixed `@ <workflow_version>` when a
  version is recorded) via a left join, so the live web dashboard can tell which
  workflow a run belongs to. The selected-run card renders a `Workflow:` line and
  the sidebar run list shows + filters on the workflow name (HTML-escaped). The
  raw snapshot columns are folded away, leaving one stable field. A clickable
  per-workflow link is not added — no stable per-workflow route exists yet — and
  is noted as residual future work in the RFC.
- **RFC 0094 adjudicator-reliability extras (#402, D240, PR #487).** The
  `collaboration_ledger` contract gains the residual adjudication shape deferred by
  PR #432: a **Check-B correspondence rubric** (per-`challenge`
  `landed_and_rebutted` / `landed_unrebutted` / `not_material`; a clearing verdict
  requires ≥1 `landed_and_rebutted` and no `landed_unrebutted`), **v1.1 per-entry
  fields** (`correspondence`, `coverage`) plus top-level `adjudicators[]` /
  `adjudication_mode`, and a **second-adjudicator-on-disagreement gate**
  (`adjudication_mode: second_on_disagreement` requires ≥2 distinct adjudicators for
  a clearing verdict; a contested clear → `needs_revision`). Additive and enforced at
  the publisher exit-6 front-matter contract — every existing v1/v1.1 ledger stays
  valid.
- **RFC 0141 (generatable `verification_gate` workflow shape) implemented at the
  `experimental` tier (D239, #473).** `striatum workflow generate --shape
  verification_gate` now scaffolds a real `type: verify` job → `claim_ledger` gate
  that is **runnable out of the box** (builtin checks, capped honestly at ASSERTED)
  and **cannot lie green**. Three pillars:
  - **Two-layer allowlist** — a committed, hashless, reviewable
    `verification/allowlist.intent.json` (`striatum.verifier_allowlist_intent.v1`,
    in the verify job's `forbidden_paths` so the lane can't sanction its own checks)
    overlaid by a gitignored per-host pins layer the operator never hand-types.
    New verbs: `striatum verifier pin --host-here` (runs in the lane, OBSERVES each
    sanctioned binary's sha; refuses drift / real-pin overwrite without `--force`)
    and `striatum verifier attest` (the PINNED→VERIFIED hinge — **refused inside a
    supervised lane** so the verified lane cannot bless its own pins). A re-pin of
    different bytes invalidates a stale attestation.
  - **Built-in check library** (`builtin:go-test`/`go-vet`/`go-build`/
    `artifact-anchor-integrity`) self-pinned to the striatum binary, with
    `builtin_id`+`striatum_version` sealed into `receipt.v1`. A builtin receipt
    **caps at ASSERTED** at the daemon gate read regardless of strict posture +
    agreement (a self-pin proves which harness invoked the tool, not which tool
    ran); the generator refuses a `gate_floor=verified` gate composed only of
    builtins.
  - **The gate cannot lie green** — an UNFILLED gate (a sanctioned external check
    with no host pin) hard-blocks `workflow validate` (exit 8) and `run start`,
    naming the entry + the literal `verifier pin --host-here` fix (and clears once
    pinned, no regeneration); a mandatory `negative_control` runs FIRST and voids
    the receipt if the known-bad passes (catches a vacuous `true`).
  - Registered in the workflow catalog at `experimental`; the interim
    `examples/verification-gate-flow/` stays as the portable today-primitives demo
    until graduation. D227 preserved: the daemon executes nothing. Graduation
    follow-ups: gate-side daemon-authoritative attestation enforcement (#482),
    doctor self-pin/pin-drift classes + version-skew resweep (#483).

### Changed

- **RFC 0134 (executable verification gate + claim status-provenance) graduated to
  `implemented` (D237).** Both build halves were already on `main` under D227's
  validate-not-execute accepted form (the daemon NEVER executes a check; the
  off-gate-path `striatum verifier run` lane mints a tamper-evident `receipt.v1`
  under the strictest available sandbox, and the run-completion gate is a pure read
  that degrades a missing/wedged verify to ASSERTED, never blocking on liveness).
  This change confirms-and-graduates rather than rebuilds: owner bundle 0016 is
  live (owner DB at bundle 18, `verify` in `jobs_job_type_check`); the live mint
  classifies a passing check `verified_eligible` and a failing check `asserted`
  under a strict bubblewrap envelope; and the RFC/index status + the (previously
  stale, rejected-form) index description are corrected to the shipped form.
  - **New connected regression** `TestRunClaimVerificationEndToEndRealReceiptMint`
    (`go/pkg/mutations`) wires the REAL sandboxed mint (`verifier.ExecuteCheck`)
    through the REAL daemon gate read (`evaluateRunClaimVerification`) in one flow,
    no fabricated seal: a strict host reads VERIFIED (`two_signal_sealed_receipt`),
    a re-mint over a changed worktree tree auto-decays to ASSERTED
    (`receipt_seal_mismatch`), and a degraded host asserts the non-strict fail-safe.
  - **Operator legibility:** the evidence export now renders a deterministic
    `## Claim Verification` section (authored vs. effective claim status + degrade
    basis) from the frozen `run_completion_record`; `TestEvidenceExportRendersProvenanceSections`
    extended to assert it. No new RPC/route/schema/migration/owner-bundle — no
    deploy required.

### Fixed

- **Actionable RFC-implementation wave (2026-06-19, D240).** Implements the
  design-complete open issues from the post-audit handoff as direct runner-fix
  PRs (one worktree-isolated agent each), graduating their RFCs
  `proposed`→`implemented`:
  - **barrier: a strict fan-in with a permanently-dead required seat now has a
    finite terminal-gap exit instead of only parking in `needs_operator`
    (#453, RFC 0138, PR #488).** Option A ships unconditionally — a sharper
    `needs_operator` message and a new `strict_fanin_required_seat_unrecoverable`
    doctor reason. Option B is opt-in per barrier (`fanin_tolerates_sealed_gap` /
    `max_sealed_gaps`, sealed on the `fanin_freeze_points` record): a gap is
    admitted **only** for a provably-dead required seat (reusing the quorum
    `supervisedAgentConfirmedDead` oracle — no new liveness check), composed into
    RFC 0135's `is_terminal_gap` predicate as a disjunct (no predicate fork), and
    the degraded fire records `status=terminal_gap` + a `damage_code` in the join
    manifest so a downstream gate can refuse a short join. Completeness is never
    silently forged. Runtime migration `0039`.
  - **daemon: the supervisor reconcile/heartbeat loop no longer write-amplifies
    `process_supervisor_pointers` (#421, RFC 0139, PR #489).** A Go write-skip
    coalesce floor (`STRIATUM_SUPERVISOR_HEARTBEAT_COALESCE`, ~30 s, computed from
    the already-read row) skips redundant timestamp bumps in
    `refreshSupervisorHeartbeat`/`refreshReportSupervisorHeartbeat`, and `state` is
    dropped from the non-partial `idx_process_supervisor_pointers_run` so the common
    intra-live transitions become HOT updates. The `#417` phantom-supervisor
    stabilization (partial-unique `…_per_session` index, `state` column, reap
    migration 0033) is untouched, and nothing is added inside the `lockRun`
    advisory-lock transaction (#198/#355). Runtime migration `0040` + owner bundle
    `0019` (transfers the three supervisor tables to `striatumd_rw` first, since
    migration 0005 left them bootstrap-owned — apply `owner-ddl 0019` before the
    daemon restart). Targets: ≥80 % fewer timestamp writes, new-page 20 %→≤5 %,
    HOT→≥92 %.
  - **attestation: a lane doing honest long tool-call-less local work keeps its
    publishable byline (#457, RFC 0140, PR #486).** The agent loop now emits a
    `work.heartbeat local_work=true` keepalive during long local work, and
    `lanehealth.Classify` reclassifies a `wedged_no_tool_progress` stall on a
    PID-alive, identity-matched lane as `alive_but_silent` (attestation preserved)
    instead of an unconditional `Attested=false`. The byline-forgery guard
    (RFC 0026/D080) is preserved — a confirmed-dead, hijacked, or no-PID-oracle
    lane still loses attestation and is reaped.
- **Failure-mode audit remediation + open-issue triage wave (2026-06-19, D236).**
  Resolves the SERIOUS/MINOR availability & liveness findings from the
  `STRIATUM_FAILURE_MODE_AUDIT_OPUS_4_8_2026-06-19.md` audit (#451–#458) plus the
  prod-critical owner-DDL crash-loop (#442/#441) and three smaller runner bugs
  (#445/#446/#447), each as a direct runner-fix PR:
  - **daemon: a background-sweep panic no longer crash-loops the single writer
    (#451, FMA-001).** The recovery and auto-spawn sweep goroutines now convert a
    per-run panic into the same degraded-cursor + backoff path an error already
    takes (the recovery loop's `panic(r)` re-raise is removed; the auto-spawn loop
    gains the missing recover), so one poison durable row degrades that run instead
    of downing the daemon on every restart.
  - **db: migration apply is now atomic (#452, FMA-002/008).** `applyOne` wraps the
    DDL and both version stamps in one transaction (mirroring `applyOneOwnerBundle`),
    closing the crash-window that left DDL applied but the version unstamped; the
    in-progress version's recorded hash is verified inside that tx, and a guard test
    trips if any future runner migration introduces non-transactional DDL.
  - **db: a two-role prod bootstrap no longer crash-loops on an owner-owned runtime
    table (#442/#441, D236).** New owner bundle `0018` transfers the pre-split
    runtime-data table cohort (`job_recovery_state` et al.) to `striatumd_rw`
    ownership (with the required `GRANT CREATE ON SCHEMA` prerequisite) before any
    runtime migration ALTERs them, so migration 0035's `ADD COLUMN` succeeds under
    the runtime role instead of failing 42501. The unsound name-based owner-DDL
    allowlist is removed; the guard now derives the runtime-ALTERable set from the
    bundles' actual `OWNER TO` transfers, so it passes only because a table is
    truthfully runtime-owned. Migration 0035's SQL is untouched (hash-stable).
  - **blob: `PutBytes` verifies a content readback hash, not just object size
    (#454, FMA-004).** A truncated/corrupt upload that satisfied the size check is
    now caught at publish (new `ErrContentReadbackMismatch`) rather than late at the
    run-completion reconstructability gate.
  - **recovery: auto-finalize enforces the per-job durability floor (#455,
    FMA-005).** `completeAutoFinalizedJob` now also runs
    `ensurePerJobPublishedArtifactsDurable`, matching `completeRecoveredJob`, so a
    job cannot seal `completed` with a non-durable optional published artifact.
  - **supervisor: buffered `supervised_push` packets survive a daemon restart
    (#456, FMA-006).** No-reader-buffered packets are persisted (new runtime table
    via migration `0038`) and replayed on reader-attach, so a push lane no longer
    silently loses a packet across a restart (self-driving pull lanes already
    self-healed).
  - **db: owner-bundle re-apply is legible and self-healing on a missing
    cross-bundle dependency (#458, FMA-007).** An undefined-object failure now
    reports the bundle + missing object + remediation, and a one-shot ordered
    idempotent re-apply re-creates a missing earlier object before a later bundle
    depends on it; fail-closed is preserved as the final safety property.
  - **supervisor: the RFC 0015 skill bundle is installed into the lane user's
    `~/.claude/skills/` at `supervise start` (#445).** Idempotent and non-fatal, it
    closes the CLI-fallback gap where a lane user never received the protocol skills.
  - **drive: `cannot_advance_blocked` distinguishes dependency-blocked from
    seal-failed (#446).** A job merely waiting on an upstream dependency
    (`started_at IS NULL`) no longer reports a phantom "lane finished but the seal
    failed" — message-only, no state-machine change.
  - **barrier: the RFC 0135 assembly commit is deterministic (#447).** The
    `commit-tree` step now pins `GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE` + identity, so
    crash-recovery re-assembly reproduces the journaled commit byte-for-byte (fixes
    the `TestBarrierAssemblyCrashMidAssemblyResumes` CI flake).
- **runner: a bare interactive agent-CLI lane now drives `work.*` instead of
  timing out (#431, D235) — the self-hosting crux.** A `process` lane whose
  command is a bare `claude`/`codex`/`agy` (argv where
  `agentloop.BootstrapDeliveryModeFor == argv`) with `agent_loop` unset is now
  auto-promoted to the self-driving agent loop at `supervise start`
  (`loadSupervisionStartConfig`, `go/pkg/mutations/supervision_lane_config.go`),
  so it receives the agent-loop bootstrap prompt + injected MCP config and runs
  the claim → heartbeat → `work.complete` loop. Before this, such a lane
  defaulted to `supervised_push`, where the daemon pushed a packet to an
  interactive agent that read it as conversational input, never called
  `work.await_packet`/`work.heartbeat`/`work.complete`, and died at
  `session.liveness_deadline_missed` with **zero `work.*` events and no sealed
  artifact** — blocking every RFC → design → build dogfood. The promotion
  mirrors `workflowgenerate.defaultAgentLoopLane` (authoring already set the
  flag), so it also covers hand-edited / copied / pre-generate snapshots that
  pasted a real agent command over the `local` parking fixture. An explicit
  `agent_loop:false` on such a command is now refused with an actionable error
  rather than launched into a guaranteed silent timeout (RFC 0111 legibility,
  symmetric with `requireSupportedAgentLoopAdapter`). The promotion is surfaced
  as `agent_loop_auto_promoted` on the `supervisor.starting`/`supervisor.started`
  events and the `supervise start` result. Regression tests
  `TestSuperviseStartAutoPromotesBareAgentCLILaneToSelfDriving` +
  `TestSuperviseStartRefusesExplicitlyDisabledAgentLoopOnAgentCLI`;
  live-verified end-to-end (a bare `claude` lane auto-promoted, drove
  draft → publish → `work.complete`, and sealed an artifact; the run completed).
  No schema, migration, or RPC change.

### Changed

- **barrier: RFC 0135 FULL cutover — quorum (P4) and run.integrate (P6) sealed
  barriers are now the LIVE default (#354, D216/D233).** The panel-quorum and
  run-entity sealed barriers, which shipped as shadow/opt-in behind D206, are
  flipped to govern the live workflow-completion path, retiring their legacy
  predicates for the cases they cover. **P4:** `dependenciesSatisfied`
  (`go/pkg/mutations/mutations.go`) now routes a GATING review panel through
  `panelQuorumSatisfied` (entity=`review_seat`, seal=`attempt`) over the frozen
  declared-seat denominator, retiring the edge-by-edge `latestVerdict` recency
  read for paneled review gates; at the default abstention budget 0 it is
  byte-identical to the old gate, and STRICTER only where it kills the
  stale-seal-accept trap (a requeued seat's prior-attempt accept no longer
  satisfies). **P6:** `HandleRunIntegrate` (`go/pkg/mutations/integrate.go`) now
  gates on the run-entity barrier `runEntityBarrierReady` (the run is terminal
  AND every declared job-level barrier fired) instead of the bare
  `state == 'completed'` check; because no run on the live completion path
  declares a job-level barrier yet, the gate reduces EXACTLY to the terminal-state
  check today while composing correctly for a future fan-in barrier. **P5**
  (revision coherence) was already the live gate (`review_generation` is the
  seal). Each flip carries a recoverable env kill switch
  (`STRIATUM_BARRIER_QUORUM=0`, `STRIATUM_BARRIER_RUN_ENTITY=0`) reverting to the
  legacy path without redeploying older code, and a same-decision equivalence
  fixture (`TestPanelQuorumCutoverEqualsEdgeByEdge`,
  `TestRunIntegrateRunEntityBarrierGate`). **P1/P2 fan-in stay SHADOW** — the
  per-completion D206 merge (`fanInIntegrateRunBranch`) remains the default
  fan-in path; flipping the deferred-join barrier live needs a `barrier_assembly`
  job dispatcher + staging-at-completion wiring that does not exist, so it is an
  unproven behavior change, not a provable equivalence flip. Also closes #351
  (RFC 0133 OQ2): the run branch stays an AUTHORITATIVE ref advanced by the
  barrier CAS, not a projection of the barrier chain (D233). No schema, migration,
  or RPC change.

### Added

- **fan-in: base-drift-as-a-recoverable-leg at the join (RFC 0133 future-slice,
  #353; builds on #352).** A fan-in staging contribution that does NOT descend
  from the frozen tip is no longer refused outright. `stageFaninContribution`
  classifies it (`classifyContributionBase`): a **recoverable base drift** — the
  run branch evolved under a sibling's feet, so the sibling forked off an evolved
  base that shares a real merge-base with the frozen tip and folds in **no foreign
  root** — is now RECORDED as a recoverable rebase leg (new runtime columns
  `base_drift_onto_sha` / `base_drift_reason` on `barrier_staged_contributions`,
  migration 0036) and assembled cleanly: `assembleFaninBarrier` 3-way-merges the
  drift onto the frozen tip and folds the staged commit as an **extra commit-tree
  parent leg** (RFC 0133 Risks: "a recorded, recoverable extra commit-tree parent
  leg, not a CAS wedge"), preserving both the frozen line and the evolved-base line
  (the #299 invariant). A **contaminated base** — disjoint history, or an off-base
  foreign root smuggled in via a merge (the #352 shape, now reached via a
  non-descendant base) — stays REFUSED with `barrier_smuggled_content`. The #352
  per-commit tree-provenance walk is reused for the drift path, anchored at the
  merge-base, so no foreign graft can enter the recovered range either. Opt-in /
  shadow only: the default stays the D206 per-completion merge; nothing flips the
  live path. Regression fence: `TestFaninStagingRecoversBaseDrift` (legit drift
  assembles cleanly, records the leg) + `TestFaninStagingRefusesContaminatedBaseDrift`
  (contamination still refuses, legit drift still stages).
- **liveness: pipe-transport RPC liveness rung (RFC 0131 Layer 2 / 131-B,
  #335).** `sessionliveness.Classify()` adds a `pipeMidRPCFresh` rung — a
  `TransportPipe` lane whose `last_mcp_request_at` is fresh within
  `ProtocolFreshSeconds` is mid-RPC and reads `working_local`, not stalled,
  BEFORE the await-packet / ack stall rungs (which anchor on stale
  `last_tools_list_at` / `last_packet_delivered_at` and would otherwise misread a
  mid-RPC pipe lane as stalled — a pipe lane has no PTY oracle and the RPC touch
  is its analogue of `last_pty_activity_at`). Scoped to `pipe` transport
  (pty_helper lanes keep their exact prior classification) and never weakens
  dead-lane detection: a pipe lane whose RPC touch has aged past
  `ProtocolFreshSeconds` falls through to the existing rungs (the protocol-idle
  catch-all already folds `last_mcp_request_at` into its base, so a
  genuinely-silent pipe lane still stalls).
- **recovery: confidence-gated pipe-lane escalation + escape-valve cap (RFC 0131
  Layers 3+4 / 131-C, #336).** A budget-exhausted, still-present,
  non-confirmed-dead lane on a `deadline_elapsed_only` basis (the pipe / no-oracle
  case) no longer escalates the run on a single sweep. `recoverStuckJobs`
  interposes a confidence gate: forgery-resistant **sealed-work progress** since
  the last sweep (a published artifact anchor or a sealed verdict — daemon-written
  rows a dead-but-spinning #324 loop cannot forge — read by `jobSealedProgressAt`,
  or a cross-lane cohort sibling showing fresher liveness) resets the counters and
  defers; otherwise the silent-sweep counter compounds, a
  `recovery.escalation_debounced` event is written, and escalation commits only on
  the **2nd consecutive `deadline_elapsed_only` sweep** OR when the Layer-4
  escape-valve cap (`consecutive_silent_sweeps ≥ (maxRequeues*2)+3`, floored at 3,
  operator-overridable via `recovery_policy.max_silent_sweeps`) fires regardless of
  confidence — so a genuinely-hung pipe lane is escalatable in bounded time and
  never un-escalatable. A confirmed-dead or closed/absent session keeps its
  immediate pre-131-C escalation timing (it already has a death oracle). Migration
  `0035_job_recovery_confidence_gate.sql` (substrate_version → 35) adds
  `misfire_evidence_score` / `consecutive_silent_sweeps` / `last_probe_basis` to
  the runtime-owned `job_recovery_state`; a deployment behind on it degrades safely
  to today's ungated escalation. The `recovery.budget_exhausted` event records
  `confidence_gated` / `cap_fired` so a gated escalation is auditable provenance.
- **recovery: confidence-gate escalation-decision legibility + escape-valve doctor
  invariant (RFC 0131 131-D, #337).** The confidence-gate state is now observable so
  an operator can see WHY a lane is or is not escalated. `run.summary` carries a
  `recovery_gate` block (`reads.recoveryGateSummary`) projecting each job's
  `consecutive_silent_sweeps` vs its `silent_sweep_cap`, `misfire_evidence_score`,
  `last_probe_basis`, and a `gate_state` of `debounced` / `escalated`. A new
  **doctor** block (`recovery_escape_valve`, `reads.doctorRecoveryGateIntegrity`)
  raises a hard **problem** (`recovery_escape_valve_uncapped`) when a job's
  `consecutive_silent_sweeps` reached its escape-valve cap but it is NOT
  `escalation_pending` on a still-actionable run — the never-un-escalatable breach
  the Layer-4 cap exists to prevent, so the safety floor is itself observable. The
  per-job cap is derived from the run's `recovery_policy` exactly as
  `recoveryPolicy.silentSweepCap` does; both surfaces degrade safely (skip) when
  migration 0035 is absent.
- **recovery: topology-adaptive escape-valve cap (RFC 0131 131-future, #349).** The
  pipe-lane escalation decision adapts to run topology: a job whose downstream
  dependent is still blocked (a critical path waiting on it) escalates at the tight
  2-sweep floor exactly as before, while a leaf job whose silence wedges nothing
  downstream is given a generous escalation threshold (the floor cap) and a loosened
  ceiling (the floor doubled) before the escape valve fires. The single cap stays the
  FLOOR — no job escalates sooner than the pre-#349 single cap — and the threshold
  and ceiling stay finite, so the never-un-escalatable invariant holds for every
  topology. `jobHasBlockedDownstream` reads `job_dependencies` for a non-terminal
  dependent.
- **liveness: synthetic pipe-read liveness for pipe-transport lanes (RFC 0131
  131-future, #350).** A genuinely-working pipe lane mid long local generation now
  refreshes liveness without an intervening MCP call: the supervisor helper's
  meaningful-output progress event stamps `sessions.last_pipe_read_at` (the pipe
  analogue of `last_pty_activity_at`) for a `pipe`-transport lane, which the
  classifier folds into `working_local` via `localOutputAt` — so the lane reads
  working rather than aging toward the `deadline_elapsed_only` stall the confidence
  gate would otherwise have to debounce. It is **forgery-resistant w.r.t. the
  Layer-4 escape-valve cap by reuse**: it is RAW output, so the #324
  `wedged_no_tool_progress` guard still reclassifies a chattering-but-hung pipe lane
  (stale tool-call timeline) as stalled regardless of how fresh the read signal is,
  and the silent-sweep counter resets only on sealed-work progress — a synthetic
  pipe read can never let a hung lane defer escalation past the cap. The column lives
  on the owner-held `sessions` table via **owner bundle 0017**
  (`go/pkg/db/sql/owner/0017_pipe_read_liveness.sql`, `LatestOwnerBundleVersion =
  17`); degrade-safe before the bundle (the `SessionPipeReadColumnPresent` probe
  gates both the liveness read projection and the daemon stamp, which falls back to
  `last_pty_activity_at`).
- **verification: executable verifier lane + receipt-reading completion gate
  (RFC 0134 executable half / D227, #395).** The off-gate-path executable half
  of the claim-status verification gate, built as **validate-not-execute** — the
  daemon never runs a check; it validates sealed receipts and curates the bytes.
  - A `verify` job type, widened onto the owner-held `jobs_job_type_check` via
    **owner bundle 0016** (`go/pkg/db/sql/owner/0016_verify_job_type.sql`,
    `LatestOwnerBundleVersion = 16`) — per D215 the job_type CHECK is owner-held,
    so this is an owner bundle, never a runtime migration.
  - A new `go/pkg/verifier` package + the lane-side `striatum verifier run`
    command (`go/cmd/striatum/verifier.go`). It resolves a named check against an
    operator-curated, git-tracked, **content-addressed allowlist** (a workflow
    NAMES a check; it never AUTHORS the executed bytes — an unknown id or a binary
    whose sha256 drifted from the pinned hash is refused), runs it under the
    strictest available **sandbox envelope** (bubblewrap → systemd-run →
    unshare+ulimit → none; no-network, no-new-privileges, read-only-except-scratch,
    cgroup/cpu/mem/wall-clock caps with an honest resolved posture), and mints a
    tamper-evident `receipt.v1` (argv + resolved binary sha256 + exit code +
    stdout digest + cwd tree-sha + seal).
  - **Two-signal VERIFIED**: the check runs twice; VERIFIED requires the sealed
    receipt PLUS the independent re-execution agreement under a *strict* sandbox.
    A lone exit-0 earns only ASSERTED; a timeout / envelope-violation / non-strict
    posture is INDETERMINATE → ASSERTED, never VERIFIED.
  - A **non-blocking run-completion gate READ**
    (`go/pkg/mutations/claim_verifier_gate.go`, wired into `maybeCompleteRun`)
    that loads the run's `claim_ledger` + the receipts its VERIFIED claims name
    and records the effective claim status on the `run.completed` event. It is a
    pure read: it executes nothing, adds no failing gate, and a missing / wedged /
    timed-out verify degrades the claim to ASSERTED — it never blocks completion
    on engine liveness. **No command execution is on the daemon's gate path.**
- **liveness: transport-aware `probe_basis` classifier outputs (RFC 0131 Layer
  1 / 131-A, #334).** `sessionliveness.Classify()` now threads a
  `TransportType` (`pty_helper`/`pipe`/unknown, derived in `ActivityFromRow`
  from the supervised lane's supervisor pointer `transport` metadata) and stamps
  a typed `Result.ProbeBasis` on every stall verdict. The pure classifier (which
  has no oracle) always stamps `deadline_elapsed_only`; the recovery decision
  tree upgrades a `pty_helper` lane's verdict to `pty_confirmed_dead` via
  `UpgradeProbeBasisConfirmedDead` once `supervisedAgentConfirmedDead()`
  positively judges the process dead (a `pipe`/unknown lane has no PTY oracle and
  stays `deadline_elapsed_only`). `recoverStuckJobs` carries `transport` +
  `probe_basis` onto the `recovery.budget_exhausted` event payload and the
  requeue/transfer action records, so every escalation records WHAT KIND of
  evidence it acted on. This is OUTPUTS only — no confidence gate, no
  escape-valve cap (RFC 0131 131-C/#336), and **no migration**.
- **claim-status lattice (RFC 0134 lattice slice / D227, #395).** The first,
  validation-only half of RFC 0134: a `claim_ledger.v1` first-class artifact
  carrying claims with a status on the lattice `VERIFIED > ASSERTED > DESIGNED`,
  and a `receipt.v1` artifact for the sealed evidence a `VERIFIED` claim binds
  to (`go/pkg/artifactcontracts/claim_ledger.go`, registered in `contracts.go`
  with the publisher's exit-code-6 schema guard). A **provenance lint** refuses
  a claim whose status exceeds its evidence — `VERIFIED` requires a bound
  `receipt_ref` and an `evidence_digest` matching the claim's
  `bound_input_digest`; a claim that fails to bind reads back as `ASSERTED`
  (VERIFIED→ASSERTED auto-decay), so an unwitnessed claim asserts at most
  `ASSERTED`. The **daemon writer** enforces the cross-seal lattice rules
  against the prior ledger (`go/pkg/mutations/claim_ledger.go`): the
  `ledger_seal` is monotonic and append-only, and a claim is demotable but
  never self-promotable. This is **validate-not-execute**: the daemon validates
  and reads prior ledgers but never runs a command — there is no `verify` job
  type that executes `checks[]` and no wiring into the run-completion gate in
  this slice. The executable half (the sandboxed off-gate-path verifier lane
  that mints receipts, and the completion gate that merely reads them) is a
  later, separate slice. Docs: `docs/reference/spec.md`,
  `docs/reference/ubiquitous-language.md`, RFC 0134.
- **RFC 0132 P4b: advisory-review guards (#341) + quorum/dissent doctor checks
  and finalize legibility (#342).** The quorum/dissent core shipped in v2.34.0;
  this lands the explicitly-deferred remainder.
  - **Layer C advisory guards (#341).** Advisory seats — previously only
    excluded from the gating denominator — are now non-blocking-but-never-silent.
    Three guards evaluated co-transactionally with each panel verdict
    (`pkg/mutations/barrier_advisory.go`, wired into `applyVerdict`):
    `advisory_corroborated_abstention` (a gating abstention co-occurring with a
    live advisory `needs_revision`/`reject`, reclassified to the `must_escalate`
    outcome), `unanimous_advisory_reject` (every submitted advisory seat
    rejecting blocks finalize even under full gating accept), and
    `advisory_only_panel_ungrounded` (a panel with no gating seats). A fired
    guard opens an operator-resolvable `blocked`-severity blocker on the
    downstream gate (the `checkpoint resolve` shape) plus an `escalation_inbox`
    row, and `dependenciesSatisfied` holds the gate from enqueuing until it is
    resolved — advisory stops the line, never auto-flips a verdict, never
    silently wedges. A mandatory `advisory_minority_report.v1` front-matter
    artifact contract (`pkg/artifactcontracts`) records the per-seat advisory
    tally + which guard fired, with exit-6 publish validation. Default behavior
    is unchanged (no advisory seats ⇒ no advisory blocker).
  - **Quorum/dissent doctor checks + dissent-ledger completeness (#342 / #339).**
    A new doctor block (`pkg/reads/doctor_quorum.go`, wired via `doctor.go` as
    `quorum_integrity`) detects `quorum_seat_unresolvable` (a declared seat with
    no live job row — a permanent fail-closed deadlock),
    `quorum_denominator_mismatch` (the live gating denominator drifted from the
    frozen snapshot), `finalize_ignored_advisory_dissent` (a completed gate that
    finalized while ignoring a live advisory dissent), and
    `dissent_ledger_incomplete` (a live blocking verdict with no forward-written
    `dissent_ledger` row, folded in from #339). `run.summary` gains a
    `quorum_dissent` block surfacing live dissent rows and open advisory holds so
    a quorum/advisory park is self-explaining before `checkpoint resolve`.
- **RFC 0094 prerequisite mechanisms — work-packet type sequencing +
  conversation `post_dialog_hook` (#402).** Two engine mechanisms that unblock
  the deferred RFC 0094 collaboration shapes (`fog_of_war_review` /
  `synaptic_prune`, built in a later slice), with no new daemon RPC method or
  route.
  - **Work-packet type sequencing (generator).** A phase may declare
    `gate: {withhold_packet_types: [<type>...], until_verdict_clears:
    <gate_job_id>}` so jobs of a withheld type are unreachable until a named
    verdict job clears. It compiles to ordinary cross-phase dependency edges
    (the daemon already gates jobs behind dependencies); `workflow validate`
    rejects a gate whose gate job is missing, is not a verdict-emitting type, or
    whose withheld-type jobs lack the dependency edge, and `workflow lint`
    surfaces the sequencing as an info finding.
  - **Conversation `post_dialog_hook` (daemon).** An optional declaration on
    `conversation.open` (`{deliver_to, packet_type}`). On conversation close —
    explicit or auto-close at `max_rounds` — the daemon emits exactly one work
    packet to the coordinator session, carrying the participant session ids +
    a transcript reference, inside the close transaction so it lands before any
    participant teardown (emit-before-teardown). It reuses the existing
    `queue_messages` delivery path and is the same "keep participants live
    through a gate" mechanism RFC 0095 Phase 3 references for review panels.
    Runtime migration 0034 persists the declaration in a sidecar table
    (`conversation_post_dialog_hooks`, no FK into owner-held tables, its own
    GRANT).
- **RFC 0094 deferred collaboration shapes — `fog_of_war_review` +
  `synaptic_prune` (#402).** Both deferred shapes are now real generated shapes
  built on the prerequisite mechanisms above; each compiles through `workflow
  generate --shape <name>` to a `striatum.workflow.v1.1` phased graph
  (`go/pkg/workflowgenerate/shapes_fog_synaptic.go`), is registered in the
  catalog and generator, and validates / lints clean. No new daemon method,
  route, or artifact contract.
  - **`fog_of_war_review`.** Four phases — fragment distribution (the
    coordinator partitions the spec into disjoint fragments; a judge alone holds
    the full spec) → reconstruction (interrogable reconstructor lanes interrogate
    peers to recover the constraints their fragment omitted) → coverage gate (the
    full-spec judge scores reconstructed / hallucinated / missed and publishes a
    `collaboration_ledger`) → proposal. The `proposal`-typed job is **withheld**
    behind the coverage verdict via §2 work-packet type sequencing
    (`gate.withhold_packet_types: [proposal]` / `until_verdict_clears:
    coverage_gate`), compiled to an ordinary cross-phase dependency. A
    coverage→reconstruction revision cycle re-opens on `needs_revision`. The
    `proposal` work-packet type is stored as DB job_type `build` at `run.prepare`
    (the snapshot keeps the authoring-level type the sequencing gate keys on).
  - **`synaptic_prune`.** Three phases — forum (the coordinator opens a
    round-robin `conversation.open` declaring a §1 `post_dialog_hook` so close
    emits the prune fan-out before participant teardown) → nomination (each
    still-live participant nominates one claim to retire) → prune tally (the
    adjudicator retires every ≥2-vote claim into a `collaboration_ledger` — the
    durable negative preamble for future runs on the same topic; a dead target is
    recorded, not hung on).
  - Both ship at support-tier `experimental` (RFC 0106: no graduation without a
    green RFC 0105 unattended-reliability fixture). Still deferred (RFC 0094
    slices 2 & 4): the semantic **Check-B** adjudicator rubric, the additive
    ledger `v1.1` fields, the second-adjudicator-on-disagreement gate, and the
    anti-theater regression corpus.

### Fixed

- **daemon: resolve open blockers when a run reaches a terminal state (#420).**
  Open blockers were resolved on every state transition EXCEPT the run reaching a
  terminal state, so a blocker (including `human_checkpoint` / escalation-class) on
  a canceled/completed/failed run lingered `open` forever (38 such rows, ages up to
  ~21 days, reading as pending operator work; #419 had hidden them read-side).
  `resolveTerminalRunOpenBlockers` (called from the terminal cleanup path) now
  resolves all kinds — the adjudication obligation is moot once the run is dead, so
  it records the terminal cause as honest provenance (not a forged decision) + a
  `blocker.resolved` event, and keeps the `escalation_inbox` mirror consistent. It
  only ever runs from terminal cleanup, never short-circuiting live-run
  adjudication. Migration 0037 backfills the already-accumulated rows (pure DML).
- **status: exclude terminal-run blockers/checkpoints from the repo-wide
  frontier (#417 follow-on).** `statusBlockers` surfaced every `state='open'`
  blocker and `human_checkpoint` repo-wide, including those on
  canceled/completed runs, so a dead run's stale blockers read as pending
  operator work forever (the frontier showed 26 `open_blockers` + 15
  `human_checkpoints`, all on terminal runs, ages up to ~21 days). The #193
  terminal-run scoping that already excludes such runs from `claimable_jobs` /
  `blocked_downstream_jobs` was missing here. The repo-wide `statusBlockers`
  query now joins runs and drops terminal-run blockers; a `run_id`-scoped call
  is unchanged. No data is mutated — the blocker rows are preserved as durable
  provenance of why each run was blocked when it terminated. The remaining
  write-side gap (run-terminal does not formally resolve its open blockers) is
  tracked in #420.
- **daemon: reap supervisors stranded on terminal runs — `striatum status`
  RPC storm (#417).** Supervisor rows were only reaped via
  `closeRemainingSessions`, which iterates sessions in `state='active'` (and
  skips active-lease holders). A lane that dies abnormally has its session
  `closed` before the run terminates, so its supervisor is never reached and
  lingers `attached`/`starting` forever. The status/dashboard read path
  LEFT-JOINs `process_supervisors ON state='attached'` and sudo+tmux
  `ProbeLaneLiveness` each one, so a single repo-wide `status` fanned out
  hundreds of failing probes and blew the 30s CLI read deadline (the daemon sat
  at 71% CPU with 100% of its DB work being supervisor reconcile; the incident
  had **562** stranded `process_supervisors` across **128** terminal runs, 511
  with an already-closed session). Fix: a run-scoped **backstop** in
  `closeRemainingSessions` reaps every still-live supervisor for a now-terminal
  run regardless of its session's state or lease; a **status read-path guard**
  never drains/probes a session whose run is terminal; and migration **0033**
  (pure DML, no owner DDL, idempotent) backfills the already-accumulated debris.
  After deploy: `status` 30s-timeout → 0.08s, daemon CPU 71% → 0.2%.
- **daemon: retry transient `lock_timeout` (SQLSTATE 55P03) on the
  lifecycle/event-write path.** Under multi-run state-DB contention a writer
  that lost a race for a contended run-aggregate or event-chain-head lock could
  be aborted at `lock_timeout` and surface the raw `append_event_row (sd):
  55P03`, wedging the job (e.g. consuming a `max_attempts:1` job's only attempt,
  or hard-failing `run.prepare`/`run.start`). 55P03 is now classified transient
  alongside `statement_timeout` (57014) and the class-57 teardown codes, so the
  bounded retry wrappers (`withTxRetryOnTransientLoad` for `run.prepare`,
  `withTxRetryOnDeadlock` for `artifact.publish`/`review.submit`/`run.retry_job`/
  interrogation/supervision-control) and the await/claim poll loop re-attempt
  with backoff and self-heal once the convoy clears. If the bounded budget is
  exhausted the verb surfaces the legible `daemon_under_load` error instead of
  the raw SQLSTATE. Deadlock (40P01) handling is unchanged. Addresses #389
  (gap 1) and #383 (item 3).
- **recovery: dead/parked lane wedges no longer require manual `session close` /
  re-dispatch dead-ends (#373, #388).** Three compounding recovery
  state-machine gaps that wedged otherwise-successful runs:
  - **#373A** — the autonomous-recovery requeue/transfer path now resolves the
    job's open NON-escalation autonomous blockers (e.g. an agent-raised
    `branch_contamination`) using the exact #304 completion-time selection, so a
    requeued attempt no longer wedges behind its own dangling blocker. A genuine
    human-attention blocker (`human_checkpoint` / any escalation-class kind,
    including `recovery_exhausted`) is never auto-resolved.
  - **#373B** — the recovery decision tree now reaps a still-active, no-lease
    session in the `agent_escalation_pending` (ProtocolAttention) posture that
    has NO genuine open human-attention blocker — closing it and transferring the
    attempt to a fresh lane, exactly what a manual `session close` did. Guarded:
    a session with an open `human_checkpoint`/escalation-class blocker is left for
    the human (the same guard now also protects the protocol-idle CASE 2 reap).
  - **#388** — `escalation resolve` of a `recovery_exhausted` escalation whose
    sessionless job was left `running` with a spent budget now re-dispatches the
    job: it resets the job to `queued` (releasing the lease, re-pending the work
    message) and clears its recovery budget (`escalation_pending=false`,
    `requeue_count`/`transfer_count`/`respawn_count`=0, `run_escalated_at`=NULL)
    so the re-armed run/sweep re-dispatches with a fresh budget instead of
    re-escalating. The `recovery_exhausted` escalation's
    `suggested_operator_actions` now name `escalation resolve` (and
    `recovery complete-stalled`) as the working paths.

### Changed

- **docs: adopt shared documentation convention + structural fold (Phase 1–2,
  warn-only).** Vendors `github.com/halbritt/doc-convention-lint` (pinned by SHA
  via pre-commit) and adds `docs/reference/doc-convention.{md,yaml}` as the
  layout/enforcement companion to `doc-map.md` (concept ownership). Phase 1 is
  warn-only — it reports drift, it does not block. Phase 2 folds genuinely-dead
  exhaust (root audits, `.agents/`, stale operator/campaign drafts) into
  `docs/records/`, and sanctions `docs/operator/` + `docs/campaigns/` as in-place
  operational regions (the RFC 0058 runtime contract the daemon reads + accepted
  frozen provenance) via a new extend-only `sanctioned_regions` overlay key —
  dropping ~1030 spurious unmapped-path findings. The relocated `docs/records/`
  tree is excluded from the retired-vocabulary gate. (#406/#407/#408). No code or
  behavior change.

- **docs: RFC index status reconciliation (2026-06-18 disposition audit).**
  Promoted ~19 rows in `docs/rfcs/README.md` and the matching RFC file headers
  from `proposed`/`blocked` to `implemented`/`superseded` to match shipped
  code: the RFC 0135 sealed-barrier primitive (P0–P6, opt-in/shadow behind
  D206) with its 0132 quorum and 0133 fan-in folds; 0119 hot tier; 0118, 0112,
  0099, 0051, 0064, 0057, 0066, 0044, 0054, 0055; and supersessions
  0097→0116/0122/0124, 0027→0127, 0049→0088, 0041→0044/0057/0119, 0102
  (lever-1 realized indirectly). Corrected the D212/D213/D216 outcome columns
  in `docs/decisions/decision-log.md`. The two genuinely-live builds (0131,
  0134) and the split/decision remainders were filed as tracked issues
  (#394–#404). No code or behavior change.

## v2.34.0 — 2026-06-18

### Added

- **#338/#339/#340 RFC 0135 P4 — panel quorum consumes the sealed barrier
  (entity=review_seat, seal=attempt).** The panel-quorum caller (D214/D216) lands
  as an instance of P0's entity/seal-generic `db.BarrierReadySQL`, NOT a fourth
  ad-hoc predicate. **#338 — `panel_role` + frozen quorum.** A per-reviewer
  `panel_role: gating|advisory` (default `gating`, lint-validated in
  `workflowauthoring.Validate`) and a per-gate `max_gating_abstentions` budget
  (default **0**) are workflow config — **NO DDL**. `panelQuorumSatisfied`
  (`go/pkg/mutations/barrier_quorum.go`) evaluates the P0 predicate over the **frozen
  declared-seat denominator** (the gate's review-seat dependencies), keyed on the
  **stable `workflow_job_id`** (NEVER `job_id`, which recovery churns), seal=attempt.
  A stale-seal accepting verdict is structurally invisible (`staged.attempt =
  live.attempt`). **DEFAULT BEHAVIOR UNCHANGED:** at budget 0 every gating seat is
  required — identical to the edge-by-edge `dependenciesSatisfied` path it branches
  off. **#339 — forward-written `dissent_ledger`.** New RUNTIME migration **`0032`**
  (`go/pkg/db/sql/0032_dissent_ledger.sql`, `LatestDaemonDBVersion` 31→32) adds an
  append-only, seal-durable dissent witness keyed on the stable `workflow_job_id`,
  written **co-transactionally with a blocking (needs_revision/reject) verdict** in
  the review apply path under the existing per-run advisory lock. Per D215: RUNTIME,
  **NO SQL FK to `striatumd.jobs`** (integrity in Go), explicit SELECT/INSERT-only
  GRANT + a BEFORE UPDATE/DELETE refuse-trigger (mirrors P1's freeze table),
  classified in BOTH read+write authority inventories. A LIVE dissent blocks the
  quorum wherever recovery moved the seat's lineage; a superseded-seal dissent no
  longer blocks. **#340 — verdict-less abstention stub + skip-only-provably-dead.**
  D214(a): an abstention stub holds a seat / raises the frozen denominator but
  carries **NO verdict value** — classified `abstain`, never accept/reject; it cannot
  satisfy `staged.seal = live.seal`. D214(b): a gating seat may be treated as
  terminally-absent (counted against the budget) **only** when
  `structurally_unrecoverable`, bound to the forgery-resistant
  `supervisedAgentConfirmedDead` oracle — a LIVE gating seat blocks (silence ≠
  consent). `k_of_n` generalization is deferred to lint (D214). Tests:
  `TestPanelQuorumOverFrozenDenominatorBudgetZero`,
  `TestPanelQuorumStaleSealAcceptDoesNotSatisfy`,
  `TestQuorumStubHoldsSeatWithoutVerdict` (D214a),
  `TestQuorumSkipsOnlyProvablyDeadSeat` (D214b),
  `TestQuorumDeadSeatBeyondBudgetStillBlocks`,
  `TestQuorumLiveDissentBlocksAcrossRecovery`,
  `TestQuorumAdvisorySeatExcludedFromDenominator`, `TestDissentLedgerIsAppendOnly`,
  `TestValidatePanelQuorum`; `TestBarrierPredicateHasNoRefCount`,
  `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL`, and the authority-inventory
  completeness guards stay green. Deferred to **P4b**: #341 (advisory guards +
  `advisory_minority_report.v1`), #342 (doctor checks), #343 (optional
  `dissent_quarantine` owner bundle 0014).
- **RFC 0135 P5 — `review_generation` is named as the seal instance (docs +
  tests only, NO behavior change, NO migration).** The revision-coherence fold
  that justifies the primitive's "seal not attempt" framing: RFC 0126's
  build-owned monotonic `review_generation` (D194) IS the sealed expectation
  barrier primitive's seal (`db.BarrierReadySQL` with `seal :=
  review_generation`). Source comments now name each operation as a seal
  operation — `bumpReviewGeneration` ("advance the entity's seal",
  `go/pkg/mutations/revision_routing.go`), the `applyVerdict` `review_generation`
  stamp ("embed the seal in the contribution", `go/pkg/mutations/review.go`), and
  the RFC 0126 per-build finalization set-difference (`required obligations MINUS
  current-generation accepting verdicts`) as the primitive readiness predicate
  (`go/pkg/mutations/run_completion_gate.go`). The default finalization path is
  unchanged — the seal predicate is a pure equivalence witness, never a new
  production code path. New regression fence `TestRevisionCoherenceIsTheSealInstance`
  (`go/pkg/mutations/revision_coherence_seal_test.go`) re-expresses the RFC 0126
  #282 shape (a revised build at generation 2; reviewer A accepts at gen 2;
  reviewer B's gen-1 `needs_revision` survives) and evaluates BOTH the RFC 0126
  set-difference AND the P0 primitive predicate (`seal := review_generation`) over
  the same live rows, asserting they agree — both refuse naming reviewer B until B
  records its own current-seal accepting verdict. The RFC's P5 slice row, the
  "Revision coherence" reconciliation, and the `TestRevisionCoherenceIsTheSealInstance`
  test-plan entry are marked IMPLEMENTED in `docs/rfcs/0135-sealed-barrier-primitive.md`.
- **RFC 0135 P6 — `run.integrate` folds in as the run-entity sealed barrier
  (equivalence-gated, non-breaking).** The highest-risk fold (RFC 0135 Risks):
  `run.integrate`'s `run_id`-keyed gate is recast as the **run-entity** instance
  of the sealed expectation barrier (`entity = run`), whose in-edges are the run's
  job-level barriers and whose readiness composes (a) the run's terminal-acceptable
  state (`completed`, matching `HandleRunIntegrate`) AND (b) every declared
  job-level sealed barrier having fired — expressed through P0's
  `db.BarrierReadySQL` shape (`entity_kind='run'`,
  `go/pkg/mutations/barrier_run_entity.go`). The RFC 0108 merge-tree →
  conflict-detection → commit-tree plumbing is factored into
  **`assembleRunEntityIntegration`**, a pure, ref-free computation shared verbatim
  by the live `HandleRunIntegrate` path and the run-entity barrier (the same way
  RFC 0133's `barrier_assembly` is the job-entity's assembly) — so the live path's
  per-repo serialization (`lockRepo`), integration idempotency (`runIntegratedInto`
  no-op), conflict/plumbing error surfaces, and integrated tree are preserved
  **byte-for-byte**. **NON-BREAKING / SHADOW PROOF:** nothing flips a default. The
  run-entity barrier ships as the asserted-equivalent **shadow**
  (`shadowRunEntityIntegrate`); `TestRunIntegrateIsTheRunEntityBarrier`
  (the deliverable gate) proves it produces the **same integrated tree OID** and
  the **same idempotency outcome** as today's `HandleRunIntegrate` BEFORE any
  caller flips. No migration (uses existing run/integration state); the P0 static
  anti-`COUNT(*)` guard (`TestBarrierPredicateHasNoRefCount`) now also covers
  `barrier_run_entity.go`.
- **#347 RFC 0135 P3 — barrier doctor invariant + `BARRIER_BLOCKED` +
  `striatum join verify`.** P3 closes the sealed expectation barrier primitive
  with its doctor/refusal/verify surface. **Runtime migration `0031`**
  (`go/pkg/db/sql/0031_barrier_status_view.sql`) adds the read-only
  `striatumd.barrier_status` view over the three barrier tables (freeze /
  staging / `barrier_state`) — `CREATE VIEW` is runtime-safe (no owner bundle, no
  `ALTER`/`DROP`, no FK), with its own explicit `IF EXISTS striatumd_rw`-guarded
  `GRANT SELECT`; `LatestDaemonDBVersion` 30→31. **Generalized barrier doctor
  invariant** (`go/pkg/reads/doctor_barrier.go`, wired into `HandleDoctor` as the
  `barrier_integrity` block): fires on a sealed-but-corrupt barrier — an
  `assembling` barrier whose journaled `target_commit_sha` is unreachable
  (`barrier_assembling_target_unreachable`), a `committed` barrier whose manifest
  disagrees with the staged refs at the live seal
  (`barrier_committed_manifest_mismatch`), an orphaned staging ref with no freeze
  record (`barrier_orphaned_staging_ref`), and the `barrier_blocked` condition —
  and stays quiet on a healthy in-flight barrier. It subsumes the per-integration
  `fanin_sibling_unintegrated` check at the barrier level (the per-worktree warning
  remains the worktree-scoped view). **`BARRIER_BLOCKED` + `blocked_manifest`:** a
  barrier with a live blocking in-edge (a `blocked`/`waiting_human`/`failed` seat,
  or an open blocking/human_checkpoint blocker) — not a clean terminal gap — is
  surfaced as the named `BARRIER_BLOCKED` condition with a `blocked_manifest`
  enumerating which seat blocks and why. **Placement choice:** `BARRIER_BLOCKED`
  is a DERIVED runtime/doctor condition emitted by the `barrier_status` view and
  the `barrier_blocked` error code — NOT a `barrier_state` CHECK value. The
  `barrier_state` assembly-journal lifecycle stays
  `sealed→assembling→committed|failed` (runtime-owned, owner-bundle-free); "blocked"
  is an UPSTREAM, pre-seal condition over the in-edges, so keeping it out of the
  `barrier_state` CHECK avoids touching an owner-held constraint (D215). **New RPC +
  CLI verb `striatum join verify <barrier-id>`** (`join.verify`,
  `go/pkg/reads/join_verify.go`): read-only verification that a barrier's manifest
  matches the staged refs at the live seal and its assembly journal is consistent;
  it returns `barrier_integrity_failed` / `barrier_blocked` (with `blocked_manifest`)
  on a corrupted or blocked barrier so it is usable as a CI/operator gate. Wired
  through `contracts/daemon_methods.json` + `go generate` (`registry_methods.go`,
  `routes_generated.go`, `daemon-method-tables.md`), two new error-catalog codes,
  and the command-authority matrix. **D206 per-completion remains the DEFAULT**
  (the barrier stays opt-in/shadow; nothing flips here). Tests:
  `TestMigrationThirtyOneBarrierStatusViewIsOwnershipSafe`,
  `TestBarrierStatusViewReturnsExpectedRows`,
  `TestDoctorBarrierQuietOnHealthyBarrier`,
  `TestDoctorBarrierFiresOnBlockedBarrier`,
  `TestDoctorBarrierFiresOnOrphanedStagingRef`,
  `TestJoinVerifyPassesOnGoodBarrier`,
  `TestJoinVerifyFailsOnTamperedBarrier`,
  `TestJoinVerifyMissingBarrierIsNotFound`;
  `TestBarrierPredicateHasNoRefCount`, `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL`,
  the P1 equivalence + P2 deployment-tolerance/crash-recovery tests, and the
  authority/contract/error-catalog guards all stay green.

- **#372/#379 chain-head lock-wait gauges + bounded doctor convoy warnings.**
  Owner bundle 0014
  (`go/pkg/db/sql/owner/0014_chain_lock_wait_gauges.sql`) adds nullable
  `lock_wait_us` columns to `striatumd.events` and `striatumd.audit_log`, then
  restates the existing SECURITY DEFINER append functions so they measure the
  `FOR UPDATE` wait on `repo_event_chain_heads` and `audit_chain_head`. The
  gauge is excluded from row-hash inputs. `doctor` now exposes warning-only
  `event_chain_head_lock_convoy` and `audit_chain_head_lock_convoy` blocks:
  events are sampled by recent/active candidate runs plus a per-run event tail,
  audit rows by a bounded newest-`audit_id` tail. Missing 0014 columns skip the
  checks instead of reddening doctor, so binary-before-owner-bundle deploys stay
  tolerant. No runtime migration, no new index, no CLI/API flag. Closes #372 and
  #379.
- **#346 RFC 0135 P2 — recoverable `barrier_assembly` job + owner bundle 0013 +
  N=1 unification.** The P1 opt-in fan-in assembly graduates into a first-class,
  CRASH-RECOVERABLE operation. **Owner bundle 0013**
  (`go/pkg/db/sql/owner/0013_barrier_assembly_job_type.sql`) widens the owner-held
  `striatumd.jobs` `jobs_job_type_check` to include `barrier_assembly`, mirroring
  bundle 0012 exactly (idempotent DROP+re-ADD guarded by a `pg_get_constraintdef`
  probe; `LatestOwnerBundleVersion` 12→13). The earlier D215/RFC 0132
  `dissent_quarantine` reservation did not land before #372/#379 consumed owner
  bundle 0014, so that optional run-state form must use the next available owner
  bundle if it ships. **Runtime migration `0030`**
  (`go/pkg/db/sql/0030_barrier_state.sql`) adds the `barrier_state` journal table
  (`sealed → assembling → committed | failed`) with two-phase-journal columns
  (`target_commit_sha` + `tree_sha`) and the barrier/seat identity as BARE COLUMNS
  with NO SQL foreign key to the owner-held `jobs` table (integrity in Go) and its
  own explicit `IF EXISTS striatumd_rw`-guarded GRANT (D215). The recoverable
  assembly journals its target intent (state=`assembling`) to PG BEFORE the git
  CAS, advances the run branch idempotently, then flips to `committed`; a crash
  mid-assembly recognizes its OWN journaled intent (or its already-applied commit)
  and resumes — never wedging, never double-committing. **N=1 is NOT special-cased:**
  a single-sibling barrier routes through the same `assembleFaninBarrier` fold,
  producing the same final tree as the D206 single-completion path.
  **Deployment-tolerance (mirrors D209 guard d):** a daemon BEHIND on owner bundle
  0013 detects the unwidened CHECK via the `jobBarrierAssemblyTypePermitted` probe
  and falls back to D206 rather than persisting a `barrier_assembly` job and
  CHECK-failing. **NON-BREAKING:** D206 per-completion remains the DEFAULT (the
  barrier_assembly path is opt-in/shadow; nothing flips here). Owner bundle 0013
  must be applied with `striatum daemon owner-ddl apply` before the new daemon
  image at go-live (deferred — not deployed by this change). Tests:
  `TestOwnerBundleThirteenAddsBarrierAssemblyJobType`,
  `TestMigrationThirtyBarrierStateIsOwnershipSafe`,
  `TestBarrierAssemblyJobTypePermittedTracksOwnerBundle`,
  `TestBarrierAssemblyTwoPhaseJournalAndN1`,
  `TestBarrierAssemblyCrashMidAssemblyResumes`,
  `TestBarrierAssemblyAlreadyAppliedCommitIsIdempotent`;
  `TestFutureRuntimeMigrationsDoNotCarryOwnerDDL` stays green (the CHECK widening is
  in `owner/`, not the runtime migration).
- **#345 RFC 0135 P1 — fan-in sealed barrier (entity=job, seal=attempt), opt-in
  + equivalence fixture.** The FIRST LIVE instance of the RFC 0135 sealed
  expectation barrier (D216), consuming P0's entity/seal-generic
  `db.BarrierReadySQL`. Runtime migration `0029` creates two runtime-owned tables —
  the append-only `fanin_freeze_points` freeze record (SELECT/INSERT-only grant + a
  `BEFORE UPDATE OR DELETE` refuse-trigger, mirroring the events/artifacts
  append-only triggers) and the attempt-addressed `barrier_staged_contributions`
  staging table — both carrying the `(repository_id, run_id, workflow_job_id,
  attempt)` seat identity as BARE COLUMNS with NO SQL foreign key to the owner-held
  `jobs` table (referential integrity is enforced in Go), each with its own explicit
  GRANT (D215). The barrier readiness predicate JOINs each declared in-edge's staged
  contribution against the seat's LIVE attempt (`staged.attempt = live.attempt`) via
  the minted predicate, so a requeued/resumed attempt's stale ref is structurally
  invisible (RFC 0133 trap #1 kill) — never a COUNT(\*) of staged refs. Includes the
  requeue tombstone, `recovery/`-prefix exclusion, merge-base contamination check
  (ancestry), quarantine-as-terminal-in-edge, and the per-run advisory-lock fire
  serialization (RFC 0104). The barrier emits the `join_manifest.v1` provenance
  (P0's contract) recording each seat's seal. **NON-BREAKING / cutover discipline:**
  the shipped D206 per-completion run-branch merge stays the DEFAULT; the barrier
  assembly is an opt-in/shadow mechanism, and a same-final-tree equivalence fixture
  proves the barrier produces a byte-identical integrated tree to D206 before any
  workflow flips. P2 (the `barrier_assembly` job type / owner bundle 0013) and the
  default-flip come later. Tests: `TestSealedBarrierJoinsOnLiveSeal`,
  `TestSealedBarrierFreezePointIsImmutable`, `TestSealedBarrierRequeueTombstone`,
  `TestFaninBarrierSameFinalTreeAsPerCompletion`,
  `TestMigrationTwentyNineFaninBarrierIsOwnershipSafe`.
- **#303 `recovery prune-debris <run-id>` prunes terminal-run artifact debris.**
  `doctor` reported `degraded` almost entirely on historical artifact debris from
  terminal/abandoned runs whose files are gone from the default branch
  (unrecoverable), with no supported verb to clear it. The new verb (capability
  `recovery`) reuses the exact doctor classifiers so eligibility is byte-identical
  to what doctor reports — only TERMINAL runs, debris-classified, file absent and
  no recoverable anchor. It honors the append-only/owner-owned `artifacts`
  boundary: no hard delete, no migration — it prunes via append-only
  `recovery.debris_pruned` tombstone events and the doctor pass suppresses
  tombstoned artifacts so a pruned run reports clean. `--dry-run` previews;
  idempotent; REFUSES non-terminal runs and still-present/anchored artifacts;
  `--sweep-pins` clears dead `refs/striatum` pins. Closes #303.
- **#298 `recovery quarantine-lane <run-id> <job-id>` preserves a terminal run's
  dirty lane worktree.** A canceled/terminal run could strand uncommitted
  repo-write work with no daemon-owned recovery (complete-stalled refuses canceled
  runs; `worktree gc` would silently `--force`-discard it). The new verb
  (capability `recovery`) snapshots the dirty worktree — daemon-owned, never
  disturbing the lane (scratch-index `write-tree` → `commit-tree`) — to an
  auditable `refs/striatum/quarantine/<run>/<job>/<attempt>` ref and an append-only
  `recovery.lane_quarantined` event BEFORE removing the worktree. `--dry-run`,
  idempotent, terminal-only. `worktree gc` is also hardened to SKIP (reason
  `dirty_uncommitted_work`) rather than discard a dirty terminal-run worktree.
  Closes #298.
- **#316 boot-epoch identity check rejects a stale lane reaching a recycled daemon
  port (defense-in-depth, #296 follow-up).** The daemon mints a per-process
  boot-epoch at startup (distinct from the restart-stable instance id), injects it
  into the lane (`STRIATUM_MCP_BOOT_EPOCH` → alias-agnostic
  `X-Striatum-Boot-Epoch` header), and rejects any MCP request whose epoch differs
  from the live daemon's — BEFORE bearer validation/dispatch — with the distinct
  `stale_daemon_identity` code, so a lane that dials a recycled port now bound by a
  different daemon run cannot touch another run's state. Backward-compatible: a
  request presenting no epoch is allowed; protection activates for lanes launched
  after this ships. Closes #316.

### Changed

- **#326 publication now commits ALL in-scope `write_scope` changes by default
  (regression of #297).** Publishing only the declared `expected_artifacts`
  stranded other in-scope files (tests, migrations, `__init__.py`) and dropped
  edits to pre-existing tracked files (e.g. a `pyproject.toml` dependency add),
  leaving the run branch incomplete. In-scope source publish was opt-IN and
  off-by-default (enabled only for the generated `code_change` shape). It is now
  the DEFAULT for any bounded repo-write scope — the `write_scope` is the
  contract and `expected_artifacts` are required-presence assertions, not an
  allowlist — with an explicit `publish_source_changes:false` opt-out and the
  unbounded-scope safety floor preserved. Closes #326.
- **#322 `run drive` and the auto-spawn scheduler now honor
  `parallelism.max_active_jobs`.** The cap was dead config no path read, so every
  unblocked job launched in one tick (including two on the same lane),
  re-triggering the #290/#302 wedges. The shared launch predicate
  `runreconcile.PlanLaunch` now enforces the global cap plus an implicit per-lane
  in-flight cap of 1 (so two jobs never share a lane concurrently); both homes
  inherit it. Closes #322.
- **#378 `supervisor.progress` no longer enters the durable event hash chain.**
  Helper progress reports remain valid liveness input: meaningful progress still
  refreshes supervisor/session liveness and the active lease when present, and the
  derived `lease.heartbeat` event remains chained as the durable lease-state
  transition. The raw `supervisor.progress` sample is no longer appended to
  `striatumd.events`, and no off-chain progress table was added. See D217.
  Closes #378 P1.2.
- **#377 recovery sweeps now latch the wedged/quiet discriminator for doctor.**
  The recovery scheduler cursor records `claimable_job_count` and
  `last_lane_advanced_at` after each sweep, outside the mutation lock. `striatum
  doctor` now flags a running run when claimable work exists and no lane has
  advanced past the five-minute threshold, while staying quiet when there is no
  claimable work. Latch read failures are surfaced as doctor problems instead of
  being hidden in the cursor JSON. Closes #377 P0.3.
- **#376 daemon panic breadcrumbs now name the failing RPC or recovery scheduler.**
  RPC connection dispatch and the recovery sweep goroutine log the panic and Go
  stack, including RPC method/request IDs where available, then immediately
  re-panic so systemd remains the restart boundary. Closes #376 P0.2.
- **#375 deadlock retry exhaustion now increments `deadlock.retry_exhausted`.**
  The bounded deadlock retry path remains non-durable and does not open a new
  transaction after rollback; the in-process expvar counter makes a future
  lock-order regression visible without touching the event or audit chains.
  Closes #375 P0.1.

### Fixed

- **#355 reconcile liveness probe no longer runs inside the event-append tx
  (uncovered #198 isomorph).** `HandleRecoveryProcessReconcile` opened a
  lock-holding, `process.lost`-appending transaction and then shelled out to
  `tmux list-panes` (wrapped as `sudo -n -u <user> -- env -i tmux …` under
  `STRIATUM_LANE_OS_USER`) for every running process row — inside the window that
  holds the per-repo `repo_event_chain_heads FOR UPDATE` (the global event-append
  serialization point). Under a multi-run supervise storm every other append on
  the repo, including `run prepare`'s `run.created`, queued behind it and died at
  `statement_timeout` as `append_event_row (sd): 57014`. This was the one in-tx
  subprocess path the #198 fix missed. The probe is now pre-computed OUTSIDE the
  transaction via a reconcile-specific liveness oracle that mirrors the loop's
  exact JOIN/probePID selection (so the cache key matches), and the in-tx loop
  reads the snapshot through `probeLaneLivenessCached`. A bounded
  `withTxRetryOnTransientLoad` now wraps `run prepare` so transient 57014
  back-pressure self-heals into a legible `daemon_under_load` error instead of a
  raw SQLSTATE, and a short `SET LOCAL statement_timeout` in the reconcile tx caps
  any residual convoy as defense-in-depth. Closes #355.
- **#330 hot event-read covering index.** The #1 daemon query by total exec time
  (~31.9k calls, ~1,190s cumulative) seq-scanned/under-indexed
  `striatumd.events`; owner bundle `0011` adds a composite covering index
  `(repository_id, run_id, actor_session_id, event_type, created_at DESC,
  event_id DESC)` (built `CONCURRENTLY` out of band on a live box; idempotent
  `IF NOT EXISTS` in the bundle). Measured generic-plan cost ~35,118 → ~8.7.
  Closes #330.
- **#327 fan-in no longer mislabels a 0-conflict merge-tree failure as a
  write-scope violation.** A parallel-group job could be wedged into
  `waiting_human` with `fan_in_conflict` reporting "conflicts in 0 path(s)":
  `integrateGit` collapsed stdout/stderr so the conflict parser saw the wrong
  stream. Fan-in now reads merge-tree stdout separately, filters paths
  byte-identical between the run tip and head (already-integrated sibling
  output), and only raises the disjoint-scope error when real conflicts remain;
  a non-zero exit with no real conflict surfaces an honest error (the same guard
  applied to `run integrate`). Closes #327.
- **#317 a same-attempt requeue of a published-but-non-durable job is reopened on
  a fresh attempt** instead of trapping the re-run on
  `artifact_immutable_byline_mismatch`. As a sibling to the #308 finalize
  short-circuit, the recovery decision tree reopens the job on a fresh attempt
  (bumping `max_attempts` in lockstep) so it can republish into a clean
  `(logical_name, attempt)` namespace; the prior attempt's append-only artifact
  row is retained. Closes #317.
- **#323 a mid-run daemon restart no longer strands a lane on the rotated MCP
  endpoint.** The endpoint was launch-time-pinned; a surviving lane lost its
  `repo_write` path when the port rotated. The agent loop now re-resolves the
  runtime endpoint+token on rotation, rewrites the ephemeral claude `--mcp-config`
  (0600), and prompts a reconnect (no-op fallback for other adapters). Closes
  #323.
- **#313 the non-functional operator-by-hand path is no longer advertised.** Under
  RFC 0088 a claim requires an attached supervisor and the `local` lane set sinks
  the packet (no artifact), so an auto-driven `local` run with
  `expected_artifacts` parks. The `local` lane-set description and the
  how-to-agent workflow loop now say so. Closes #313.
- **#311 P0 a single flaky job no longer wedges a whole run at
  `needs_operator`.** When one job exhausted its autonomous-recovery budget, the
  recovery decision tree flipped the ENTIRE run to `needs_operator`, discarding
  the durable, attested work of every already-completed sibling (the incident:
  one flaky agy/Gemini reviewer wedged a run whose other 8 jobs had completed).
  Now, when a single exhausted job's downstream is clear, only THAT job moves to
  a new non-terminal `quarantined` state and the run finalizes-the-majority on
  its completed deliverables, recording a quarantine manifest (which job + lane +
  stall_class) on the terminal `run.completed` event and the
  run_completion_record, with `stop_reason='quarantined_jobs'`. A
  `recovery.job_quarantined` event names the single offending job. The job is
  quarantined ONLY when ALL hold: (a) no unfinished job transitively depends on
  it, (b) it is not a provenance-required reviewer the RFC 0118 run-completion
  gate would refuse, (c) the per-run cap `recovery_policy.max_quarantinable_jobs`
  (default 1) is not exceeded, and (d) the `quarantined` job state is permitted
  by the live schema (owner bundle `0012`); if any guard fails the run still
  escalates exactly as before, so a deployment behind on owner bundles or a run
  with multiple simultaneous flaky jobs is never silently swallowed. The
  quarantined job is NEVER completed and NEVER has an artifact sealed on its
  behalf — it is the one narrow thing surfaced to the operator, who terminalizes
  it with the new `recovery accept-quarantined <run-id> <job-id>` verb (resolves
  the blocker + marks the job canceled-by-operator; idempotent). The new
  `quarantined` job state is added to the owner-held `jobs_state_check` CHECK via
  owner bundle `0012` (idempotent DROP+re-ADD; apply with `striatum daemon
  owner-ddl apply` before the new daemon image).
  New RPC `recovery.accept_quarantined`. See D209. Closes #311 (P0).
- **#306 the gated DEEPENED picks are git-retained for git-only auditability.**
  divergent_ideation's diverge-branch IDEAS the issue named were already
  git-retained (`handoff` kind), but the DEEPENED picks (the gated inputs to
  `final_synthesis`) were blob-routed. They now carry `git_publication` placement,
  so a git-only auditor can verify the synthesis faithfully represented its gated
  inputs. Per-shape override; RFC 0123 default blob-routing for other shapes is
  untouched. Closes #306.
- **#299 regression guard: `run integrate` preserves intervening main work.**
  Already fixed by the merge-based integrate (3-way merge against the real
  merge-base, landed 2026-06-04 before the report); this adds the missing
  regression test that a run branch integrates to a 2-parent merge whose tree
  carries BOTH the run's change AND main's intervening post-fork work (never
  reverted). Closes #299.
- **#305 closed wontfix-by-design (terminal run-state legibility).** Committed git
  is durable artifact provenance, not authoritative run-state (RFC 0033/0043);
  the terminal disposition lives in the `run_completion_record` and is exposed via
  `striatum status`, the dashboard, `run.summary`, and `archive export` (the
  sanctioned offline-audit path). Committing run-state markers to the run branch
  would create a second, drift-prone source of truth against the boundary. See
  D210. Closes #305.

- **#312 `repo add --init <path>` no longer fails with "repo.add requires
  path".** The CLI flag parser greedily consumed the positional after a
  value-less flag, so `--init <path>` bound `init="<path>"` and left the `path`
  positional unset. `parseFlags` now threads each command group's known boolean
  flags (the route metadata `Bool: true` set, surfaced via `params.BoolFlags`)
  so a presence flag sets `true` without swallowing the next positional. A drift
  guard (`TestBoolFlagsMatchUsageMetadata`) keeps the bool table in sync with the
  route usage metadata. Closes #312.

- **#324 a lane that lost its daemon endpoint and spins with no tool-call
  progress is now classified `wedged_no_tool_progress` and reclaimed.** The
  liveness classifier never consumed `last_tool_call_finished_at`, so a wedged
  agent emitting spinner PTY frames kept `last_pty_activity_at` fresh and read as
  `working_local` forever (the lease-heartbeat rung never fired); the driver only
  relaunches sessions the daemon reports inactive, so an endpoint-orphaned lane
  was never forgotten. A new stall rung flags a lane that holds an active
  lease/job but has made no tool-call progress for `ToolProgressSeconds` (600s),
  explicitly NOT counting spinner PTY activity, mapping to Protocol `stalled` →
  the recovery decision tree's transfer/requeue so the wedged owner is closed and
  the slot relaunched. The new class is added to the owner-held
  `sessions_liveness_stall_class_check` CHECK via owner bundle `0010` (idempotent
  DROP+re-ADD; apply with `striatum daemon owner-ddl apply` before the new daemon
  image runs). Closes #324.

- **#325 concurrent lane completes/publishes no longer deadlock (SQLSTATE 40P01)
  and lose a lane's work.** `artifact.publish` took a run-scoped `FOR UPDATE` on
  the job row and appended to the per-repo event chain but opened its transaction
  with a plain `withTx` and never took `lockRun` first — inverting the RFC 0104
  lock order against the claim/complete paths, so a publish racing a sibling's
  complete/publish deadlocked and Postgres aborted, killing the lane. Both
  `artifact.publish` and `work.complete` now take `lockRunForJob` as the first
  statement and run under `withTxRetryOnDeadlock`, giving every per-run
  transaction one serialization point plus a retry backstop. The
  `TestPerRunHandlersTakeLockRunFirst` guardrail now covers both handlers.
  Closes #325.

- **#310 a per-job-isolated repo-write lane can no longer publish into the
  operator's shared, tracked `repo_root`.** The claim packet resolves a job's
  lane via a session fallback (so a lane with an empty job selector still
  supervises as `striatum-lane` under a per-job worktree), but the publish-time
  worktree gate read only the job selector — so an empty selector made the
  per-job worktree NOT required and the publish target fell back to `repoRoot`,
  letting the lane write the operator's tracked checkout (owned by
  `striatum-lane`, blocking operator `git pull`/`reset`) and bypassing the RFC
  0125 porter. `worktreeRequirementForJob` now resolves the lane with the same
  session fallback, so an isolated repo-write job without an active worktree is
  refused with the actionable `worktree_required` error instead of a silent
  `repoRoot` write. Scoped to `worktree_isolation: per_job` lanes — the explicit
  shared-checkout path (`allow_shared_checkout_repo_write`) is unchanged. See
  D207. Closes #310.

- **#329 read-side helper-event drains now present daemon authority before
  appending supervisor events.** Dashboard/status/supervise read projections
  opportunistically drain PTY helper events, and those helper events append
  through the same SECURITY DEFINER `append_event_row` path as mutations under
  `pg_write_boundary=full`. The drain paths now open their sub-transaction via
  `db.BeginAuthorizedMutation`, preserving the existing short status
  `lock_timeout` and stale-metadata fallback while installing
  `striatum.daemon_auth` before `DrainHelperEventsHook` can write
  `supervisor.progress` events. This closes the long-running production
  `daemon authority secret missing` log storm without weakening the database
  write boundary. Tests: `TestDrainStatusHelperEventsInstallsAuthorityPrelude`
  plus the PostgreSQL regression
  `TestReadSideHelperEventDrainsUseAuthorizedTx` for environments with
  `STRIATUM_PG_TEST_URL`. Closes #329.

- **#302 residual — a clean lane exit now records a durable dead-signal so the
  recovery sweep can reclaim a falsely-active session even when no live probe is
  possible.** PR #318 established that the sweep already recovers a dead-pane /
  queued lane whenever there is an *unforgeable* dead signal (a `tmux_pane_dead`
  probe, a `lost`/`stopped` supervisor pointer, or a dead PID — see
  `supervisedAgentConfirmedDead`). The deferred residual was the case with **no**
  unforgeable signal: the supervisor probe returns `tmux_unavailable` /
  `pid_identity_unavailable` (or the helper was torn down, leaving no recorded
  pointer), so the `active` session reads as possibly-live and the queued job is
  never reclaimed — the run wedged until a manual `session close --requeue-job`.
  Forcing a close on an *ambiguous* probe would re-introduce the #145/#147
  false-requeue class. The fix records a **durable** dead-signal at the
  daemon-issued idle-exit contract instead: when `work.await_packet` returns the
  `no_work` / `idle_behavior=exit_session` envelope (RFC 0120 Phase 1) — i.e. the
  daemon has TOLD the lane to stop — the handler stamps the lane's most-recent
  non-terminal supervisor pointer `stopped` (`recordCleanLaneExitSignal`,
  `go/pkg/mutations/claim.go`) BEFORE the pane/helper tears down. The existing
  sweep then reads that recorded `stopped` pointer as conclusive and reclaims the
  session + leaves the queued job claimable, even with no readable live probe.
  Crucially only the **recorded** terminal exit counts: the SESSION row is left
  `active` (the sweep, not the await handler, closes it, so the #291/#302
  queued-scan still applies), the pointer write is guarded to flip only an
  `attached`/`detached` pointer (a `starting` supervisor that never ran is left
  alone, and the write is idempotent), and a recording failure is swallowed so a
  no_work answer can never become an RPC error. An ambiguous/unavailable probe
  with NO recorded terminal exit is STILL treated as possibly-live and left
  untouched — the #145/#147 guard is preserved verbatim. No schema/migration, no
  new RPC, no client/route surface change. Tests:
  `TestAwaitPacketRecordsDurableCleanExitSignalOnIdleExit` (failing-then-green:
  the pointer stayed `attached` on `origin/main`),
  `TestSweep302NoSignalResidualUnrecoveredThenRecoveredByDurableSignal`
  (residual unrecovered on an unavailable probe alone, recovered by the durable
  `stopped` pointer), and `TestSweep302DoesNotReclaimAmbiguousProbeWithoutTerminalSignal`
  (safety: `pid_identity_unavailable` + no recorded exit is left alone). Closes #302.

## v2.33.0 — 2026-06-16

### Decisions

- **Doctor integrity legibility P1 — artifact-loss problems → `0` (D205, #300).**
  Extends the D204 reclassification so `striatum doctor`'s artifact integrity
  check (read-only, `go/pkg/reads/`) no longer reds `ok` on preserved-but-not-at-tip
  or operator-acknowledged historical content, taking the 42 residual artifact
  problems to `0`. Three rules: **(A) default-branch *history* awareness** — content
  whose `content_sha256` matches `repo_path` at any reachable revision of the default
  branch (not only its tip) is durably preserved → clean (bounded `--max-count=200`,
  `ctx`-cancellable, memoized, safe-degrading); **(B) `artifact_superseded_on_default_branch`**
  — a deliverable whose `repo_path` is still live on the default-branch tip (only the
  recorded draft `content_sha256` is unverifiable, the lane draft having been revised
  before merge) → warning, not a problem; **(C) `artifact_acknowledged_loss`** — a
  curated, sha-bound baseline (`docs/operator/doctor-acknowledged-loss.json`, schema
  `striatum.doctor.acknowledged_loss.v1`) downgrades reviewed, immaterial losses to a
  warning. Rule C is honored **only** on `artifact_id` + `content_sha256` match, so a
  stale/wrong entry can never mask a different or future loss; an unlisted genuine loss
  still reds `ok`. The reader safe-degrades when the baseline is absent. No
  schema/migration/RPC. Tied to the `AGENTS.md` "Do not paste over a broken runner"
  guardrail: a green doctor is only trustworthy if any future real gap flips it red.
- **#297 undeclared in-scope files no longer drop silently at `work.complete` (D203).**
  Completion now computes the in-scope, attempt-authored files that are neither
  declared as `expected_artifacts` nor published via the D197
  `write_scope.publish_source_changes` opt-in and surfaces them loudly
  (`stranded_in_scope_paths` + a `warnings` entry on the result, plus a durable
  `job.in_scope_paths_stranded` provenance event). Non-breaking: it warns, never
  refuses — the silent drop becomes loud without changing the legacy default.
- **`run start` / `run drive` auto-drive lifecycle fixes (D202, #295/#293).**
  Auto-drive derives the run id from both the `--run-id` flag and the positional
  `<run-id>` form; `run drive` refuses a second live drive for the same run (and
  reaps a stale dead-pid marker) instead of warning-and-coexisting; the stop hint
  and `daemonize-run-drive.md` name `run drive` as the resume command.
- **#291 hung-supervised-session stall detection + recovery (D201).** A
  `queued`/claimable job whose bound supervised session is hung (dead `tmux`
  pane still `active`, or an `active` session that never claimed and holds
  `no_lease`) is now detected and recovered: the decision tree scans `queued`
  jobs and binds the leaseless session by the claim path's role+lane eligibility,
  and the dashboard surfaces the wedge (`leaseless_count`).
- **#292 stalled-job finalize path — `recovery complete-stalled` (D200).** A new
  daemon verb (`recovery.complete_stalled`, CLI `recovery complete-stalled
  <run-id> <job-id>`) non-destructively completes a job whose agent published its
  required artifacts (durably) then died before `work.complete`, leaving the run
  `needs_operator` behind a `recovery_exhausted` blocker. It verifies the required
  artifacts are present AND body-reconstructable from their declared placement
  (RFC 0125 P0-3, worktree-independent), then drives the same server-side
  completion `work.complete` would have — resolving the now-moot
  `recovery_exhausted` blocker + escalation and restoring the run to `running`
  (reusing the #207 path). It refuses verdict-capable jobs (never bypasses the
  RFC 0118 verdict gate), refuses a job whose lane still holds a live active lease
  (finalizes a dead lane only), and is keyed on an open `recovery_exhausted`
  blocker (`--force` relaxes that; `--dry-run` previews). Closes the dead-end the
  #289 work surfaced but could not exit. No schema change.

- **`divergent_ideation` graduated to `supported` (D199, RFC 0106).** The shape
  now carries a green RFC 0105 unattended-reliability fixture
  (`go/pkg/adapterconformance/divergent_ideation_test.go`), registered in
  `ReliabilityFixtureShapes` and reconciled with `supportedShapes` by the
  shape-tier guard. It proves the distinctive **double fan-out/join** lifecycle
  (diverge→converge, deepen→final_synthesis) drives to `completed` unattended, and
  that a hard dead-lane fault in a branch of *either* fan-out — including the
  second one, after the first join fired — self-recovers on the same attempt via
  the production sweep with no escalation. Docs flipped experimental→supported.
- **RFC 0126 — multi-reviewer revision coherence accepted (D194).** The #282
  follow-up: replace DELETE-on-revision of reviewer verdicts with a build-owned
  monotonic `review_generation` stamped at the write boundary; finalization
  asserts every required reviewer has a current-generation accepting verdict.
  Stale verdicts become structurally invisible (no manual invalidation), history
  preserved. Phased P0–P3 in `docs/rfcs/0126-multi-reviewer-revision-coherence.md`.
- **RFC 0127 — retire the lane git identity accepted (D195).** The RFC 0125 P2-2
  end-state: the per-job workspace becomes a plain daemon-owned directory, the
  lane is a pure byte producer, and the daemon owns all git (base staging,
  daemon-side write-scope diff, porter commit, anchoring). Opt-in, reversible.
  `docs/rfcs/0127-retire-lane-git-identity.md`.
- **RFC 0128 — cross-repo run boundary accepted (D196).** The #280 product
  decision: keep the single-repo run as the invariant; ship a fail-fast guardrail
  (refuse cross-repo reach at validate/dispatch instead of silently narrowing) +
  read-only artifact federation; achieve cross-repo outcomes by decomposition;
  decline first-class multi-repo atomic writes (recorded as a deferred option).
  `docs/rfcs/0128-cross-repo-run-boundary.md`.
- **#287 opt-in source-change publish (D197).** A repo-write job may set
  `write_scope.publish_source_changes=true`; `work.complete` then commits the
  lane's in-scope source edits to the run branch alongside its declared
  artifacts. The git-worktree form of RFC 0127 P1's daemon-owned change-set
  commit, brought forward opt-in so the `code_change` dogfood pipeline produces
  reviewable code on the run branch.
- **#289 unsealed-agent-exit recovery policy (D198).** A confirmed-dead agent
  that produced output but never sealed (`work.complete`) is classified
  `agent_exited_unsealed`, distinct from a hard crash, with a smaller requeue
  budget and an inspect-the-worktree escalation.

### Added

- **RFC 0087 + RFC 0129 — `divergent_ideation` workflow shape (first-class).**
  Implements the long-proposed shape that widens a design space before narrowing
  it — the striatum-native, provenance-backed, provider-portable port of the ADHD
  method (`UditAkhourii/adhd`, MIT). No daemon method, no model call in any state
  transition, no vendor import. New `compileDivergentIdeation`
  (`go/pkg/workflowgenerate/shapes_divergent.go`): a flat `striatum.workflow.v1`
  fan-out (`frame_problem` brief → N fresh-session diverge branches → convergence
  critic `findings_ledger` → K deepen → `final_synthesis`), registered in the
  shapes set, dispatch, catalog (`experimental`), and a fixture at
  `examples/divergent-ideation-flow/`. Frame library
  (`go/pkg/workflowgenerate/frames.go`, RFC 0129): ADHD's 15 personas plus three
  categories surfaced by a multi-model divergent run of the method on itself
  (operation/transform, temporal-forensic, risk-pricing), each with `frame_kind`,
  `category`, and distortion-axis `dimensions`; deterministic seed-based selection
  with a wild-frame guarantee, a min-structure gate, and an anti-redundancy gate
  (no two frames sharing ≥2 distortion axes). Branches round-robin across the lane
  ring so a custom `claude`/`codex`/`agy` set carries different frames on different
  models; the convergence critic records cross-family agreement; the generator
  warns on single-family runs. `workflow generate` gains a repeatable
  `--lane-modifier` flag. Validated live: a 3-model (Opus/GPT-5.5/Gemini) inaugural
  run produced genuine cross-family divergent ideation end-to-end.
- **#287 source-change publish to the run branch.** `publishWorktreeSourceChanges`
  at `work.complete` commits a per-job-isolated repo-write job's in-scope source
  edits (the exact complement of the write-scope violation set) to the run branch
  via the RFC 0125 porter plumbing, emitting a bounded `job.source_changes_published`
  provenance event. The generator opts `code_change` repo-write jobs in by default
  (`enableSourceChangePublish`); legacy default (declared artifacts only) is
  unchanged without the flag.
- **#289 `agent_exited_unsealed` recovery class.** New stall class + recovery
  budget (`recovery_policy.max_unsealed_requeues`, default 1) for a confirmed-dead
  agent that produced output without `work.complete`; confirmed-dead agents are
  excluded from the CASE 2 stalled-transfer path so a delayed sweep cannot
  misroute them to the larger transfer budget.

### Changed

- **#300 doctor integrity legibility (D204).** `striatum doctor`'s artifact/worktree
  integrity checks no longer red `ok=false` on un-actionable findings: content
  reachable from / present on the repository default branch is treated as durably
  preserved (a `worktree_unanchored_on_default_branch` warning, not a problem),
  `canceled`/`failed`-run worktree/artifact leftovers become `*_debris_terminal_run`
  warnings, and pre-blob-storage artifacts (empty `blob_key`) become
  `artifact_legacy_unverifiable` warnings — while genuine unpreserved loss stays a
  problem. Reclassified findings move to an additive `warnings` channel (`ok` is
  unchanged: `len(problems)==0`); verbose mode gains `warning_records`. The default
  branch is resolved without hardcoding `main` and degrades safely. Read-only change
  in `go/pkg/reads/{worktree_refs,doctor_artifact_anchor,doctor}.go`; no schema
  change. This makes a red `doctor` an actionable stop-condition again — the
  prerequisite for the `AGENTS.md` "Do not paste over a broken runner" guardrail to
  be enforceable. Produced by the `docs/campaigns/doctor-integrity-legibility/`
  dogfood (operator-gated). **Takes effect on daemon restart.**

- **#294 `revision_routing` checkpoint affordances — clarify `continue` vs
  `override` (docs/affordance only, no behavior change).** On a `revision_routing`
  checkpoint, `continue` re-runs the reviewer on the current branch (a revision
  cycle) — the sanctioned proceed-past-the-verdict path is `override --decision-id`
  (D157). The `checkpoint resolve --action` CLI help, the per-checkpoint
  `resolve_action_hints` in `status` (now carries `continue`/`cancel`, not just
  `override`), and the checkpoint `description` now say so.
- **Operator guardrail: do not paste over a broken runner.** A new project rule
  in `AGENTS.md` and a shared boundary rendered into every generated operator
  skill (`striatum-scaffold`/`workflow`/`supervise`/`recover`/`claim-loop`, both
  profiles): when a verb fails, a lane strands edits in its per-job worktree, a
  run wedges, or `doctor` is red (`job_completed_without_anchor`,
  `worktree_head_unreachable`, `artifact_anchor_missing_file` / `_hash_mismatch`,
  `artifact_blob_metadata_missing`), never hand-finish the work (manual worktree
  capture, cherry-pick, hand-commit) and report it complete — that masks the
  defect as success and corrupts daemon-owned provenance. Recover through the
  daemon (`recovery requeue-stale`/`resume`/`complete-stalled`,
  `checkpoint resolve`) or surface it (issue + friction + escalate). The
  `striatum-recover` skill gains a "Recover honestly" section and lists
  `recovery complete-stalled` (D200) in its verb table.

- **#288 `workflow generate` DX.** `--scaffold-root` (and generated scaffold
  targets) may live under `.striatum/scratch/`; `--option workflow_id` /
  `artifact_root` / `scaffold_root` route to the matching top-level field; missing
  lane commands report in one batched error with a JSON-array `--option` hint
  surfaced in CLI + JSON output; a generated `single_agent`/`author_reviewer`
  `code_change` scaffold validates out of the box (repo-write lanes get
  `worktree_isolation: per_job`; `single_agent` records the structural same-model
  review acceptance). `templates show single_agent`/`author_reviewer` carry a
  worked `usage_example`.

- **RFC 0125 P0-3 — body-reconstructability completion gate (#285).** The
  fail-closed gate the campaign deferred: `verifyRequiredArtifactReconstructable`
  re-reads + hash-verifies every required artifact body per declared placement
  (blob readback; or a git-anchor probe over all `refs/striatum/<run>/<job>/*`
  attempt pins, the legacy pin, then the run branch), wired into
  `verifyRunCompletionProvenance`. A positive loss fails completion with key
  `required_artifact_unreconstructable` (orthogonal to the RFC 0118 verdict path,
  which does not regress). Degrade ladder: hard-fail only on positive evidence
  (recorded blob whose readback fails; an existing anchor definitively missing
  the body); WARN — never wedge — when the substrate is unverifiable (anchor
  pending pre-integration, no blob client, git-probe fault). The escalation
  points an unreconstructable failure at `recovery reseal`.
- **RFC 0125 P1-1 — content-addressed RUN_LEDGER (#286).** The write-once
  `run_completion_record` now carries a self-contained `run_ledger`: per
  completed verdict-capable gate, its verdict + frozen attestation and every
  required artifact body's reconstructability provenance (placement, content/blob
  sha, git anchor ref/commit, `readback_verified`). The record's sha256 is
  anchored in the terminal event, so a retrospective reconstructs every
  gate/verdict/SHA offline from the hash alone. The ledger walk is best-effort —
  a per-job probe fault is recorded, never propagated, so it cannot roll back a
  completion the gate approved.
- **RFC 0125 — durable gate artifact provenance (D192).** Closes the gap the
  Hippo retrospective exposed (a run finalizing `completed` while required gate
  artifact bodies were unreconstructable) plus the friction issues #270–#283.
  - **Daemon-as-porter (#278, #281):** at `work.complete` the daemon force-adds
    and commits a lane's published artifacts onto the run branch (past
    `.gitignore`, from the detached per-job worktree, as the operator user), then
    re-probes durability and anchors a durable ref — so a lane no longer has to
    commit its own publication. Scoped to the completion path;
    `recovery.reseal` / `recovery.resume --complete` stay pure durability gates.
  - **`artifact.publish` body over the MCP envelope (#272):** an optional
    `body_base64` lets a lane that cannot enter the operator-owned worktree
    publish; the daemon materializes the body at the artifact path.
  - **`recovery.reseal` (#271):** completes a remediated worktree-durability
    blocker on the SAME attempt (no attempt bump, no duplicate provenance),
    refusing if the body is still not durable.
  - **`artifact.get_content` git-anchor fallback (#275):** resolves a body from
    the durable run-branch / `refs/striatum/…` job-pin anchor when the working
    tree is on another branch, instead of reporting the body missing.
  - **Status legibility (#283, #282):** `latest_non_accepting_review_verdicts`
    excludes superseded verdicts and flags `upstream_revised_after_verdict` with a
    precise per-row `recovery_action`. **Recovery legibility (#274):**
    auto-finalize explains blocked-job skips and points at `recovery reseal`.

### Fixed

- **Recovery-exhausted escalations now name the offending job + lane (#311 carve-out).**
  An RFC 0101 Phase 4 `recovery_exhausted` escalation previously surfaced only the
  bare `recovery_exhausted` reason, never the lane behind the stalled job. The
  escalation now threads the offending job's lane
  (`jobs.lane_selector_json->>'lane_id'`) into the structured escalation
  `payload_json` (`lane`, when resolvable), the blocker `description`
  (`lane=<lane>`), and the `run.needs_operator` event (a structured `stuck_jobs`
  array of `{workflow_job_id, lane, stall_class}`); the stable `recovery_exhausted`
  reason code is preserved for existing consumers. An unresolvable lane degrades to
  empty rather than erroring. No schema/RPC change. The design-heavy parts of #311
  (restart-churn cap, finalize-majority/isolate-stalled-job, non-`pty_helper`
  liveness recomputation) remain ready-for-human.
- **#290 parallel fan-in siblings are integrated into the run branch, not stranded
  (D206).** When N author jobs fanned in to a downstream job, only the first to
  complete fast-forwarded the run branch; each later sibling's worktree had forked
  from the pre-FF tip, so its HEAD could no longer FF and was only pinned under
  `refs/striatum/<run>/<job>/<attempt>` — durable but unreachable, so a downstream
  worktree (seeded from the run branch) never saw it. The anchor now integrates a
  non-fast-forwardable HEAD via a conflict-free object-DB content merge
  (`merge-tree --write-tree` → `commit-tree` → compare-and-swap `update-ref`, the
  same plumbing as `run integrate`) and still pins it for provenance; an overlap
  (two siblings wrote the same path) is surfaced loudly rather than silently
  resolved to a last writer. `doctor` gains a `fanin_sibling_unintegrated` warning
  (running runs only) for a completed job reachable only via a pin. No schema/RPC
  change. The deferred post-completion join barrier + join manifest remain follow-ups.
- **#296 codex push lanes fail loud when the MCP endpoint/token is unresolvable.**
  A stdin-FIFO ("push") codex lane whose live Striatum MCP endpoint or session
  capability token could not be resolved used to silently degrade to a bare
  `codex` — which launches, looks healthy, but points its MCP client at a stale
  port (or nothing), so it can never reach `work.await_packet`/publish/complete
  and the run wedges while `doctor` shows only a warning. `supervisedPushCommand`
  now refuses such a launch (loud + recoverable: the supervisor is marked lost
  and `supervise.start` returns a legible error), bringing the push path to
  parity with the already-loud self-drive path. A hermetic, codex-CLI-gated
  regression test pins the other half of #296 — that the launch-time
  `-c mcp_servers.striatum.url=<live>` override wins over a pre-existing
  `[mcp_servers.striatum]` config.toml section (previously asserted, never
  tested). No schema/RPC change.
- **#304 a `blocked`-severity blocker no longer dangles open after a
  retry/recovery completion.** A non-escalation `work.block` blocker raised on
  an earlier job attempt is now resolved on **every** `job.completed` path —
  including the recovery/retry paths (`completeRecoveredJob`,
  `completeAutoRecoveredJob`, `completeAutoFinalizedJob`), not only the normal
  `work.complete` path — emitting `blocker.resolved_on_completion` and reusing
  the existing #175/#207 resolution mechanism, so a completed run stops inflating
  the `open_blockers` frontier. A new operator verb `recovery resolve-blocker
  <blocker-id>` (RPC `recovery.resolve_blocker`) closes any already-dangling
  non-escalation, non-checkpoint blocker by id; it refuses `human_checkpoint`
  blockers (use `checkpoint resolve`) and escalation-class blockers (use
  `escalation resolve`), and does not mutate run/job state.
- **#308 auto-driven runs self-heal a final job killed in the publish→complete
  window.** When a lane published its required artifact and then died before
  `work.complete` (`agent_exited_unsealed`), the recovery sweep's only budgeted
  action was `requeue_same_attempt` → escalate, wedging the whole run at
  `needs_operator` even though the deliverable was durable. The decision tree now
  auto-finalizes such a job from its already-published, body-reconstructable
  required artifacts (reusing the D200 finalize-from-durable-artifact path and all
  its safety gates: verdict-capable refusal, artifact-row presence, RFC 0125 P0-3
  reconstructability) instead of requeueing it. It fires only when the work is
  genuinely complete-but-unsealed; a dead lane with no durable artifact still
  requeues.
- **#309 `recovery complete-stalled` no longer waits out a fictional live lease.**
  The finalize liveness guard keyed on the lease *time* deadline, so a recovery
  `requeue_same_attempt` that renewed a confirmed-dead lane's lease made it read
  as alive and refused (`job still holds a live active lease`) for as long as the
  renewed deadline lasted (~31 min observed). The guard now keys on *session*
  liveness — a lease whose owning session is `stopped`/`closed`/absent is
  finalizable immediately regardless of the lease deadline — while still refusing
  a genuinely live lane (active lease AND active owning session).
- **#302 dead-pane active-session queued jobs are recoverable; coverage locked
  in.** Investigation confirmed the #291 decision-tree path already recovers a
  dead `tmux` pane whose session stays `active` and job `queued` whenever there is
  an unforgeable dead signal (dead-pane probe, terminal pointer, dead PID),
  including the previously-untested shape of a released prior-claim lease; a
  regression test pins it. The cases that remain operator-only are precisely those
  with no unforgeable dead signal (probe unavailable / no recorded pointer), where
  acting would re-introduce the #145/#147 false-requeue class.
- **#291 hung supervised sessions no longer stall a run silently.** A
  `queued`/claimable job whose bound supervised session is hung used to sit
  indefinitely with `supervisor_stalls.stalled_count:0` and no blocker; the
  recovery decision tree and dashboard projection now scan `queued` jobs and
  resolve the leaseless bound session, so the stall is detected, surfaced, and
  recovered (closing the hung owner). (D201)
- **#295 `run start <id>` positional form no longer skips auto-drive.** The
  positional run id previously left auto-drive with an empty run id, so the run
  sat `running` with a claimable job and zero lanes; the id is now derived from
  both arg forms. **#293** `run drive` refuses a duplicate live drive and the
  stop/resume guidance points at `run drive`. (D202)
- **#297 multi-file code slices no longer strand undeclared in-scope files.**
  Tests/migrations/secondary modules an agent wrote but did not declare as
  `expected_artifacts` are now reported loudly at `work.complete` instead of
  being dropped untracked. (D203)
- **#301 `workflow generate` multi-lane sets now emit `worktree_isolation:
  "per_job"` on every autonomous repo-write lane, not just the first.** The
  generator derived the repo-write lane set from a lane-name heuristic (every
  lane not named `*reviewer*`), so a fan-out shape like `divergent_ideation` that
  round-robins repo-write diverge/deepen jobs onto a lane named `reviewer` left
  that lane without per-job isolation. `workflow validate` / `run prepare` then
  rejected the generator's own output. Isolation is now reconciled against the
  actual repo-write job→lane assignments in the compiled job graph (the same
  per-job `repo_write` signal `RefuseAutonomousSharedCheckoutRepoWrite` enforces),
  so generate and validate cannot disagree and a bare `author_reviewer` /
  `multi_review` scaffold passes validate without a hand-edit or a manual
  `--lane-modifier worktree_isolated`.
- **#307 `divergent_ideation` deepen artifacts now carry uniform front matter
  across lanes.** The `deepener` role stub and `deepen` prompt stub now instruct
  every deepen lane to emit an `author:` byline and a complete `inputs:` list
  naming both the convergence ledger (`CONVERGENCE.md`) and the problem brief
  (`PROBLEM_BRIEF.md`) in its `synthesis.v1` front matter, so deepen artifacts
  stay machine-comparable regardless of which model ran the lane (one lane used
  to omit `author:` from front matter and list only the convergence ledger).
- `run.retry_job` refuses to bump a job past its `max_attempts` during recovery,
  points the operator at `recovery reseal` (the same-attempt path), and records a
  deliberate override as an audited `attempt_budget_override`; revision-cycle
  reopens are exempt. (#273)
- `recovery resume`'s positional argument maps to `blocker_id` (its own
  `recovery_resume` params group) instead of the shared `run_id`. (#270)
- dispatch no longer leaks an ambient `STRIATUM_REPOSITORY_ID` into
  `daemon_global` RPCs, so a lane's repo id can't contaminate `make check`. (#276)
- The supervisor prepares `.striatum/scratch` ACLs for a non-owner lane user
  before launch, so non-Codex lanes can write their ephemeral MCP config. (#279)
- Documented the lane publication and cross-repo provisioning boundary in
  `docs/how-to/lane-sandbox.md`: lanes publish artifacts (durable locally via the
  porter) and do not push remotes (#277); cross-repo jobs need operator-provisioned
  ACLs on every secondary repo (#280).

### Changed

- CI is faster without weakening the gate. `make check` now runs a single
  instrumented test pass (`check-tests`: race detection + core coverage in one
  run) instead of three separate runs of the PostgreSQL-heavy suite (`test`,
  `race`, `coverage`); `-count=1` keeps results uncached so a warm build cache
  never serves a stale pass. The `ci.yml` workflow splits vet+lint (no database)
  onto a runner parallel to the test job, and both jobs restore an accumulating
  Go build/module cache. The release workflow builds archives in parallel with
  the installed-CLI gate (publish still requires both) instead of serializing
  behind it. Standalone `make test` / `race` / `coverage` are unchanged for
  local use.

### Fixed

- Supervised lane launch env files now stay available until the lane command
  sources them, then remove themselves from disk. Tmux setup and attach
  commands no longer consume the lane env file, keeping `STRIATUM_MCP_TOKEN`
  out of run-as argv while avoiding missing `/tmp/striatum-supervisor-env.*`
  source failures. (#264, #266)
- `workflow generate` now emits direct Codex, Claude, and agy lane commands as
  agent-loop PTY-helper lanes by default, and `workflow validate`, `run
  prepare`, and `supervise start` refuse retired `codex exec` one-shot lanes
  before they can stall without acking. (#263, #267)

## v2.32.0 — 2026-06-13

### Added

- `striatumd -check-config` validates the daemon's configuration (Postgres URL
  shape, `--pg-write-boundary`, and blob storage env) with no side effects —
  prints every problem at once, exits 0 when clean and 78 (`EX_CONFIG`) when
  not — so operators can verify a config before restarting.

- CI guard for operator-brief freshness (`TestOperatorBriefStaysCurrent`):
  the suite reuses the `operator bootstrap` freshness probe and fails when
  `docs/operator/BRIEF.md` has invalid `operator_brief` front matter, is not
  `status: current`, or does not mention the current `VERSION` — so a version
  bump now requires refreshing the brief in the same change (2026-06-11
  architecture review, truth-mechanization P1).
- RFC 0120 Phase 2 now has a read-shaped `wake.wait` daemon RPC and
  post-commit in-process wake hints for newly available work, peer messages,
  and conversation turns. `run drive` waits on that surface between reconcile
  passes and falls back to the existing bounded polling interval when a daemon
  does not support wake waits.
- `striatum worktree anchor <run-id> <job-id> <worktree-id>` gives operators a
  daemon-backed repair path for completed repo-write jobs whose worktree HEAD
  still exists but was not anchored through the normal completion path.
- Hermetic CI integration coverage for the RFC 0120 review fixes that unit
  tests missed: `TestRevisionLifecycleRunDriveRelaunchesDeadLane` drives the
  real `run drive` reconciler through a launched-lane death + same-attempt
  requeue against the production daemon and asserts it forgets the dead session
  and relaunches (the F2 wedge), and
  `TestAwaitPacketTerminalEnvelopeDrivesReceiverExit` feeds the real
  `work.await_packet` envelope for every non-active session state into the
  exported `agentloop.EnvelopeRequestsIdleExit` predicate, locking the
  daemon↔receiver exit contract (the F3 error-loop). Both were confirmed to go
  red against their respective reverted fix.
- `TestWakeBusIsolatesAcrossRunsAndRepositories` pins the wake bus's negative
  filter case (RFC 0120 review Q13): a run-scoped waiter is never woken by
  another run, and a repository-wide waiter never by another repository. The
  RFC 0120 review provenance (findings F1–F5) is recorded in the RFC, with the
  one deferred observation (F5: `run drive` terminal teardown on
  `needs_operator`/`waiting_human`) tracked as #261.

### Changed

- A deterministic daemon config error (malformed `STRIATUM_BLOB_*` value, bad
  `--pg-write-boundary`, missing/unparseable Postgres URL) now fails fast with
  exit 78 (`EX_CONFIG`) **before** any side effect, and the installed systemd
  unit sets `RestartPreventExitStatus=78`, so a config typo parks the daemon in
  `failed` with the exact error instead of crash-looping under
  `Restart=on-failure`. Transient/operational failures (database briefly
  unreachable, stale socket) keep their non-78 exit and still auto-restart.
  Existing installs pick this up after `striatum daemon install` +
  `systemctl --user daemon-reload`.
- RFC 0120 / D180 now includes the notify-only wake bus as Phase 2 of the
  accepted design, rather than deferring it to a separate future RFC. Daemon
  auto-spawn remains deferred to #212.

### Fixed

- `work.complete` retried after a verdict-driven session close folds into the
  idempotent `already_completed` answer again instead of refusing with
  `session_inactive`; genuinely new state-changing work from inactive sessions
  is still refused. Restores the idempotency broken alongside the
  closed-session recovery guidance. (RFC 0120 adversarial review)
- `run drive` now drops `launched` slot entries whose session is no longer
  active, so a recovery `requeue_same_attempt`, a lane that died before
  claiming, or a pause/resume no longer wedges the slot until the driver
  restarts — the freed slot relaunches through the normal adopt/register path
  on the same reconcile pass, and the driver no longer retries
  `supervise.stop` forever against an already-gone session. (RFC 0120
  adversarial review)
- `run drive` now stops lanes it launched before returning terminal for
  non-finalization run states such as `needs_operator`, so a driver-owned lane
  does not linger idle after the operator-actionable blocker is surfaced. (#261)
- `work.await_packet` answers every non-active session state (`closed`,
  `expired`, `lost` — not just `stopped`) with the in-band `session_terminal`
  no-work envelope instead of a retryable RPC error, and no longer records
  session liveness for terminal sessions, so an agent-loop receiver can never
  error-loop against a finished session. Lanes also treat any unrecognized
  non-empty `idle_behavior` value as `exit_session` (fail closed). (RFC 0120
  adversarial review)
- The frozen-write-scope drift refusal names the pinned `frozen write_scope`
  guidance again, matching the sibling guard message and un-redding the
  frozen-attempt-scope regression tests on main.
- `work.await_packet` terminal idle envelopes now include
  `idle_behavior=exit_session`, and agent-loop bootstrap instructions no longer
  tell lanes to keep polling after `no_work`. The PTY daemon receiver also exits
  the lane on that explicit idle signal, preserving `run drive` as the
  operator-authorized wake surface while avoiding model-side no-work loops.
  (#248, RFC 0120)
- `session.register` now serializes behind the per-run lock and refuses
  terminal runs; repeated `run cancel` calls also close leaked active sessions
  and release active leases left behind on already-canceled runs. (#253)
- Session-scoped artifact, review, and work-complete writes now return a typed
  `session_token_stale` remediation when a closed predecessor session's bound
  token tries to act as the active successor for the same run/role/lane slot.
- Closed sessions now fail `artifact.publish` and `work.complete` with a typed
  `session_inactive` recovery envelope that points operators at same-attempt
  requeue plus fresh-session retry, while preserving normal artifact and
  write-scope validation on the recovered path. (#255)

## v2.31.0 — 2026-06-07

### Runtime read-scope least privilege — identity surfaces (#164, RFC 0114 / D173)

- **Owner bundle 0006** (`go/pkg/db/sql/owner/0006_identity_read_scope.sql`,
  owner-applied out-of-band via `striatum daemon owner-ddl apply`): transfers
  ownership of `striatumd.principals`, `striatumd.principal_clients`, and
  `striatumd.client_sessions` to the owner role FIRST (a `REVOKE` against a
  runtime-owned table is not a boundary — the owning role can self-re-grant),
  then installs owner-owned `SECURITY DEFINER` projections gated by
  `assert_daemon_authority()` (`get_principal`,
  `resolve_principal_for_client`, `list_principal_scopes`) and revokes direct
  runtime `SELECT`: `principals` and `client_sessions` fully denied;
  `principal_clients` column-gated (`principal_id` denied;
  `client_id`/`linked_at`/`unlinked_at` stay readable for the live
  `UPDATE ... WHERE` in token rotation). DML grants are preserved exactly.
  RFC 0114 Open Question 4's contingency was taken: PostgreSQL demands
  SELECT on the `ON CONFLICT` arbiter columns (verified live), so the
  active-link upsert moved behind the owner-owned
  `link_client_to_principal(p_daemon_secret, ...)` write function; the Go
  caller is a thin dual-path wrapper and external behavior is unchanged.
- Principal read paths in `go/pkg/admin` now route through the projections
  when daemon authority is bootstrapped, with SQLSTATE `42883` fallback to the
  direct SQL while bundle 0006 is unapplied (and permanently for secretless /
  un-adopted databases). DTO shapes are byte-identical (projection bodies are
  verbatim transplants).
- `ReassertReadRevokes` is map-driven from the capability stamps
  (`auth_projection_read`, `identity_projection_read`), and
  `striatum daemon owner-ddl apply` now re-runs the write + read reasserts
  after the bundles — re-running it is the documented grant-drift repair.
- `daemon doctor` `pg_read_scope.posture` is now DERIVED from
  `schema_authority` stamps + live privilege/ownership probes instead of
  hard-coded: `partial_projection_gated` once bundle 0006 is stamped and
  verified, `broad_runtime_select` otherwise, with a `grant_drift` array
  naming failing surfaces. `private_read_denial` stays `false` (RFC 0113
  R2/R3 remain open). Per-surface gates report
  `stamped`/`verified`/`owner_ok`.
- Read-authority inventory: new class `runtime_projection_read`
  (`principals`); `client_sessions` reclassified `runtime_select_denied`;
  `principal_clients.principal_id` added to the denied-columns map. PG-gated
  guards cover column denial, direct-read `42501`, unauthorized projection
  `28000`, projection parity, ownership-transfer self-re-grant refutation,
  grant-drift repair, post-close principal/link/rotation semantics, and the
  doctor posture derivation in both states.

### Fixed

- `supervise start` no longer marks a live cross-user (run-as) agent-loop
  lane as exited: `pidAliveLocal` treats signal-0 `EPERM` as alive (the
  process exists but the daemon cannot signal across users) with an explicit
  `/proc/<pid>/stat` zombie exclusion, so the EPERM-leniency cannot keep a
  zombie "alive"; on a genuine failed attach, the orphaned lane tmux session
  is now killed (via a run-as tmux runner) instead of leaking; supervise
  stop/status paths use the same run-as runner so cross-user lanes can be
  stopped and inspected. (#205)

## v2.30.0 — 2026-06-07

Second triage-execution wave: the 13 issues filed against 2.27.0 under
multi-run load (#192–#198, #202–#207 minus the human-gated ones) were
root-caused by 5 read-only triage agents and fixed by 4 parallel fix waves,
each landing test-first with an independent review pass. Also: supervised
token-usage telemetry (RFC 0115 implementation), RFC 0116 + RFC 0117 accepted
(D175/D176) with companion issues #208–#217 filed.

### Daemon load — the #198 convoy (parent of #197 and #193's latency)

The minutes-long lock holder was the 60s recovery sweep, not supervise.report:
per-run sweep transactions held the run advisory lock + FOR UPDATE row locks
while shelling `tmux list-panes` / reading /proc per stuck job AND draining
helper FIFOs. Under N lanes × M runs the subprocess calls serialized inside
the lock window; every event append / claim / status read queued behind it
into global SQLSTATE 57014.

- **#198** lane-liveness probes are pre-probed BEFORE the sweep transaction
  (read-only snapshot injected via context; in-tx decision logic unchanged;
  live-probe fallback for operator RPCs) and helper-event drains moved to
  short per-supervisor transactions before the sweep (single-drain failure
  isolates instead of failing the sweep).
- Review hardening: a `pid_identity_unavailable` probe verdict (PID signalable
  — i.e. alive — but /proc momentarily unreadable) no longer confirms death;
  treating it as dead force-expired a LIVE agent's lease (the #145/#147
  false-requeue class), and the pre-tx oracle would have cached the transient
  for the whole sweep window.
- **#197** claim-next/await long-poll classifies statement-timeout (57014 +
  class-57 teardown) as transient daemon load — lanes keep polling and see
  `{status: no_work, reason: transient_daemon_load}` at the deadline instead
  of a raw SQL error that parked them.
- **#193** `status` repo-wide payload bounded: non-terminal runs + the 20 most
  recent terminal runs by default (`--all-runs` / `--run-limit` restore
  history); claimable/blocked enumeration excludes terminal runs (also hides
  the orphan pending messages `run.cancel` leaks — mechanism noted on the
  issue); the status-read helper drain takes `lock_timeout=500ms` and degrades
  with a `helper_drain_skipped` note instead of blocking >30s.

### Revision-cycle integrity

- **#203** recovery.auto no longer completes a revision-cycled job with the
  pre-revision artifact: the expired-lease scan now uses the shared
  `db.ExpiredLeaseStillStalePredicate` (one definition; recovery.auto +
  stale_leases + dashboard.all all routed through it) and auto-publish is
  attempt-gated per RFC 0095 Goal 2 — on-disk content byte-identical to a
  lower-attempt artifact is refused with a `recovery.auto_publish_refused`
  event instead of silently discarding the reviewer's revision request.
- **#206** re-claimed review jobs can no longer republish the prior round's
  finding verbatim: `review.submit` refuses a finding byte-identical to the
  same job's lower-attempt finding (new catalogued code
  `fresh_review_byte_identical`, message tells the lane to delete the stale
  file and review the revision), and re-opened review packets carry
  `context.revision_context` (prior finding artifact id/path/sha + prior
  verdict recovered from the durable `verdict.recorded` event).

### Supervision & liveness

- **#202** session-bound capability tokens now carry `review` — supervised
  reviewer lanes can submit verdicts without an operator-minted token (the
  capability-set completeness bug behind every review parking at
  `capability_missing`).
- **#204** exited supervisor-helper children are reaped (wait goroutine,
  mirroring the pipe-process path) — no more unbounded zombie accumulation
  when supervisors go lost; restart-survival (#141, context.WithoutCancel)
  unaffected.
- **#192** a bootstrap grace window (120s, additive to the 60s discovery
  deadline) suppresses the `agent_mcp_discovery_stall` false positive that
  fired on every claude lane cold-start; a genuinely wedged lane still flags
  at 180s.
- **#207** (with the auth half already fixed by v2.29.0/#176): claims gated by
  a `needs_operator` run now return `ineligible_reason: run_needs_operator` +
  the open escalation blocker id instead of bare `no_work`, and a
  `recovery_exhausted` blocker whose job subsequently completes a genuine
  attempt auto-closes (the run flips back to running when no other
  escalation-class blocker remains) — human-checkpoint and human escalation
  kinds remain untouched.

### CLI / DX

- **#194** `--help` for all 93 daemon-derived verbs now renders real usage:
  the 45 uncovered ParamsGroups synthesize their positional arguments from the
  parser's own table (cannot drift); the pointer to the unshipped
  command-authority-matrix doc is gone.
- **#219** the `TestPrivilegeRevocation` setup deadline no longer flakes the
  full gate under parallel real-PG package load.

Not in this wave (open, human-gated): #195/#196 (fold into accepted RFC 0117
implementation), #201 (helper privilege-boundary decision; Option A
recommended on the issue), #205 (superset fix in flight on
`issue-205-supervise-liveness`).

## v2.29.0 — 2026-06-06

Triage-execution wave: 17 issues fixed directly (every S/M-tier issue from the
2026-06-06 full-backlog triage), each landed with a test-first regression and a
multi-angle code review. Design-class clusters got RFC proposals on review
branches (RFC 0116 zero-operator-touch DAG for #178/#188-policy; RFC 0117
per-job worktree & branch ref-safety for #186/#184) — those await maintainer
review and are NOT part of this release.

### `claude --print` retirement (#199 — deadline 2026-06-15)

After 2026-06-15 a live `claude --print` invocation bills API tokens (real
money per packet) instead of plan usage. The retired one-shot mode is now
impossible to reach from a workflow without an explicit override:

- `.striatum/bin/*-supervised-wrapper.sh` untracked (`.striatum/` fully
  ignored; on-disk operator copies preserved); archived template stamped
  DO-NOT-USE; deployed copies in registered target repos retired in place.
- The `deprecated_claude_print_lane` lint escalated warning → **refusal** at
  `workflow validate`, `run prepare`, and supervise launch (last line of
  defense on the frozen snapshot). Override: inline lane option
  `allow_claude_print: true`. The refusal names the cost consequence.
- CI hygiene guard: no tracked executable/operational file may invoke
  `claude --print` (lint enforcement + historical docs excluded).

### Daemon robustness

- **#176** `escalation.resolve` no longer raises `daemon_auth_lost` under
  `pg_write_boundary=full`: `withResolveTx` now begins an authorized mutation
  (RFC 0110 authority prelude) and appends the mutation audit row in the same
  tx, mirroring the mutations chokepoint. Real-PG regression.
- **#180/#200** `HandleSuperviseStatus` no longer panics (`index out of range
  [0]`) when the post-drain re-fetch fails or returns empty — the daemon-stop
  crash that rotated the MCP port and orphaned live lanes.
- **#191** supervisor `RunHelper` joins the packet-stream goroutine on child
  exit (bounded drain), retiring the `packet_accepted` data race; the drain is
  skipped when the stream already ended (no gratuitous 500ms exit delay).
  Verified 200/200 under `-race -count=200 -cpu=4`.
- **#179** `recovery.stale_leases` (and the `dashboard.all` mirror) no longer
  report already-transferred historical leases as stale
  (`release_reason IN (recovery_transfer, operator_transfer,
  recovery_requeue)` excluded, NULL-safe).

### Session-lease safety (#189, resolves #174)

`register-session --replace` is refused when it would displace a session that
heartbeated within the advertised heartbeat window — the verified production
mechanism behind #174's "requeue before advertised expiry" (event stream shows
`lease.released reason=superseded` on live sessions; daemon expiry exonerated).
New catalogued content code `displaced_session_live` names the displaced
session, its last-heartbeat age, and the window; `--force-live --reason "..."`
is the recorded escape hatch. The work packet's `heartbeat_after_seconds` now
derives from the canonical liveness policy instead of a hardcoded 300.

### Lifecycle & guards

- **#175** `work.complete` resolves the completing job's open autonomous
  blockers in the same tx (`blocker.resolved_on_completion` event per blocker);
  human-checkpoint and escalation-class blockers stay open.
- **#181** supervise start refuses agent-loop lanes whose argv0 cannot
  self-drive, derived from the C0-pinned `agentloop.BootstrapDeliveryModeFor`
  contract (no parallel adapter list to drift).
- **#183** worktree create creates a confirmed-but-refless branch ref at the
  recorded base via ref-only `git branch` (never `checkout -b`; primary HEAD
  untouched; concurrent-create race tolerated). Standalone stopgap — RFC 0117
  proposes the full ref-safety lifecycle.
- **#188** (text half) the fresh-reviewer refusal names the active author
  session and suggests `session close` alongside `--force-non-fresh`; the
  policy half is RFC 0116 scope.
- **#190 / D174** agy seat demoted `supported → degraded` (Antigravity 1.0.6
  is OAuth-only; the Installed CLI Gate now detects the login picker and
  skips-with-reason instead of stalling). Re-promotion path: RFC 0109
  graduation gate once a headless auth path returns.

### CLI / DX

- **#185** `striatum why <target_id>` accepts its positional argument.
- **#187** `workflow generate --option lanes.<id>.command=<JSON array>` routes
  into `spec.lanes`; a reconcile test guarantees every catalog-advertised
  `required_options` key stays CLI-settable.
- **#182** new `striatum daemon token-create` verb (authority matrix +
  generated route contracts updated); `capability_missing`/`capability_expired`
  refusals now name the method, the missing capability, and the mint
  remediation; operator runbook section added.
- **#177** `striatum skills install --optional <name>` / `skills list`:
  manifest-tracked optional-skill tier rendering Striatum-authored skills only
  (`refactoring-campaign`); third-party suggestions stay suggest-only.

## v2.28.0 — 2026-06-06

### RFC 0106 / D172 — graduate `adjudicated_constraint_extraction` to `supported`

`adjudicated_constraint_extraction` (ACE) graduates `experimental → supported`,
the fourth distinct-fixture shape to clear the RFC 0106 / D162 graduation gate
(after `implementation_panel` D166, `falsification_gate` D168, and the
isomorphic co-graduation of `cross_examination` D169/D170). This graduates an
EXISTING shape; the RFC 0106 new-shape FREEZE remains in force.

- Graduation rests on the RFC 0112 explicit-interrogation-consumer fixture
  `go/pkg/adapterconformance/ace_interrogation_test.go` — the ONLY fixture that
  drives genuine interrogation (`interrogation.open/ask/answer/close`) through the
  production handlers, composing a fan-out of interrogating cross-examiners + a
  join INSIDE a recursive revision re-cascade across a `phase_synthesis` gate.
  Four cells, all green vs live PostgreSQL: happy (preserved-context secret never
  written to artifacts), revision-reopen (fresh attempt-aware target),
  dead-lane-during-re-cascade (same-attempt requeue, zero escalations, join stays
  blocked, run completes), and waiting-human/advisory-evidence
  (`interrogation.required_skipped` + `interrogation.unavailable_signaled`).
- Flips both maps (`workflowtemplates.supportedShapes` +
  `adapterconformance.ReliabilityFixtureShapes`, reconciled by the bidirectional
  guard `TestSupportedShapesHaveReliabilityFixture`); regenerates
  `docs/reference/workflow-catalog.md` (ACE now `supported`); repoints the
  `experimental_shape` lint test to the still-experimental
  `iterated_interrogating_panel`.
- Discharges the historical gating risk D171 deferred ("ACE graduation remains
  out of scope until the RFC 0105 fixture lands"): an interrogable job +
  interrogation window now provably drives cleanly without wedging on the RFC 0095
  revision reopen. Adjudicated through a dogfood decision panel (author + claude
  reviewer + codex reviewer + synthesis): unanimous accept.

### CI / hygiene (since v2.27.0)

- `chore(ci)`: bump remaining GitHub Actions to Node 24 majors.
- `fix(ci)`: repair the agy CLI install in the Installed CLI Gate.
- `chore`: fix gofmt drift and exempt provenance artifacts from whitespace
  checks.

## v2.27.0 — 2026-06-05

### RFC 0112 / D171 — explicit interrogation consumers (V1)

Workflow jobs may declare `interrogation_targets` to consume an upstream
interrogable job's preserved-context window without fake graph edges. ACE's
cross-examiners sit behind the `convener_synthesis` phase gate, so
direct-dependency inference closed `convener_draft`'s window before
cross-examination could open; the explicit declaration keeps it live.

- `workflow validate` enforces the D171 rules (self-target, duplicate target,
  chained interrogable consumer, unknown/non-interrogable/unreachable target =
  hard errors); `workflow lint` warns on >3 targets, a lane without the
  `interrogate` capability, unknown entry fields, and redundant direct-dep
  targets.
- The consumer relation is snapshot-derived (frozen `workflow_json` + live
  `jobs` rows); no new table, migration, RPC family, or dependency edge.
- All production terminal transitions route through a `markJobTerminal` choke
  point that releases interrogation windows for terminal consumers;
  `TestTerminalJobStateWritesRouteThroughMarkJobTerminal` (AST guard) fails any
  new direct terminal write outside it (allowlist: `run.cancel` family).
- `claim-next` packets project `context.interrogation_targets` attempt-aware
  (`available`/`unavailable`/`not_ready` with `target_session_id`,
  `target_attempt`, `reason`, daemon-authored instruction); the
  `session.awaiting_interrogation` event payload gains additive `attempt`.
- Advisory V1 `required`: skipped required targets record
  `interrogation.required_skipped`; non-wedging opens against retired targets
  record `interrogation.unavailable_signaled`. No hard completion gate.
- The ACE generator declares every cross-examiner as an explicit consumer of
  `convener_draft`; `adjudicated_constraint_extraction` gains an RFC 0105-style
  conformance fixture (window across the phase gate, revision-reopen fresh
  window, dead-lane during re-cascade, advisory evidence) but stays
  `experimental` pending a separate graduation decision.

### Recovery — deterministic released-lease resolution in the dead-lane scan

The ACE fault fixture exposed an inverse-#145 shape: when a prior attempt's
released lease and the current attempt's released lease share the same
second-granular `acquired_at`, the random `lease_id` tiebreak could resolve the
PRIOR attempt's lease — whose owner session is still active — masking the
genuinely dead current owner so the dead lane was never requeued.
`recoverStuckJobs` now prefers the job's own `current_lease_id`
(NULL-safe) between the active-lease preference and the timestamp ordering.

### RFC 0096 V2 — daemon launches supervised lanes as `STRIATUM_LANE_OS_USER`

When `STRIATUM_LANE_OS_USER` names an OS user distinct from the daemon's, the
daemon launches supervised lane commands and their tmux sessions through
`sudo -n -u <lane-user> -- env -i ...` with a sanitized lane environment, and
probes/stops/rebridges tmux as that user. Launch fails closed if the sudo rule
is missing. `doctor` reports `daemon_launch_enforced`, the launch mechanism,
and the passwordless-sudo requirement under `lane_sandbox`; `run_as_user` is
recorded in supervisor metadata, events, and status views. GH #87 still
requires host adoption (PG-less lane user, sudoers, `pg_hba` reject rules) and
a green lane-isolation gate.

### RFC 0113 R1 follow-through — token-admin reads prefer the authority projection

Token revoke/rotate now route `loadTokenForUpdate` through the
`striatumd.load_token_for_update` SECURITY DEFINER projection whenever the
daemon-authority secret is present, instead of attempting the direct
secret-column SELECT and falling back on `42501`. The `pg_read_scope` doctor
block derives its sensitive-surface inventory and denied columns from
`go/pkg/db/read_authority_inventory.go` instead of a hand-copied list.

### RFC 0106 / D169 — `cross_examination` co-graduates by structural isomorphism

`cross_examination` now shares the `falsification_gate` RFC 0105 reliability
fixture instead of carrying a shallow renamed duplicate. The graduation is gated
by `TestCrossExaminationIsStructurallyIsomorphicToFalsificationGate`, which
normalizes the generated graph and fails if the two shapes structurally drift
while ignoring role/artifact/prose naming.

- `workflowtemplates.supportedShapes` and
  `adapterconformance.ReliabilityFixtureShapes` now include
  `cross_examination`.
- The RFC 0106 policy now allows narrow isomorphic co-graduation only with an
  explicit drift guard; the new-shape freeze remains in force.
- The experimental-shape lint regression now uses
  `adjudicated_constraint_extraction`.

## v2.26.0 — 2026-06-04

### RFC 0110 §10 — pgtest consumes the production grant surface (C-PGTEST-NO-DML-GRANT)

`pgtest` no longer hand-builds the unprivileged role's DML with imperative
`GRANT`/`REVOKE` on the protected append-only tables — a false-green channel for
every `42501` negative-path gate. The per-test login role is now only a login
shell over `striatumd_rw` (membership); its DML surface comes from the production
migration provisioning (migration 0005 grants `striatumd_rw` broad DML, then
`REVOKE`s `UPDATE`/`DELETE` on the append-only `events`/`artifacts`). The role is
created before migrate so that provisioning fires on each test database.

- **`roleSetupStatements`** issues only non-protected grants (CONNECT, schema +
  sequence USAGE, and the two role-membership grants); **G-PGTEST-GRANTS**
  (`TestRoleSetupIssuesNoProtectedDML`) fails if any setup statement `GRANT`/`REVOKE`s
  DML naming a protected table or blanket-(re)grants table DML across the schema.
- `TestPrivilegeRevocation` still proves `UPDATE`/`DELETE` on `events`/`artifacts`
  is denied (`42501`) — now via the inherited production revokes, not a hand-built
  one. Test-only change; no product behavior change.

### RFC 0106 Phase 4 — `falsification_gate` graduates `experimental → supported` (D168)

The second workflow shape to clear the RFC 0106 graduation gate (after
`implementation_panel`, D166/v2.24.0). A new RFC 0105 reliability fixture,
`go/pkg/adapterconformance/falsification_gate_test.go`, drives the dialogue-chain
graph (holder → falsifier_1 → falsifier_2 → adjudicator gate → commit → final)
through the production claim/ack/complete + `review.verdict` handlers and the real
`mutations.SweepRun`:

- **Happy cell:** a `needs_revision` verdict on the adjudicator gate transitively
  re-blocks the WHOLE downstream chain — `falsifier_2` (depth 1) AND the gate
  itself (depth 2), both attempt-bumped — exercising
  `resetDownstreamForRevision`'s recursive downstream re-block past the depth-1
  base case the single-job review-cycle fixture (`lifecycle_revision_test.go`)
  stops at. The chain then re-cascades to a clearing verdict and the run completes
  unattended.
- **Fault cell:** a hard dead lane injected into a MIDDLE dialogue node
  (`falsifier_2`) DURING the revision re-cascade is requeued by the recovery sweep
  on the same attempt while the gate stays blocked (the re-cascade is not lost); a
  fresh lane finishes it and the run reaches `completed` with zero escalations.

Both maps flipped (`workflowtemplates.supportedShapes` +
`adapterconformance.ReliabilityFixtureShapes`, reconciled by the bidirectional
guard `TestSupportedShapesHaveReliabilityFixture`);
`docs/reference/workflow-catalog.md` regenerated; the `experimental_shape` lint
test repointed to `cross_examination`. The RFC 0106 new-shape FREEZE stays in
force — this graduates an EXISTING shape, it does not lift the freeze.

## v2.25.0 — 2026-06-04

### RFC 0110 Phase 2 `full` — events become a SECURITY-DEFINER-only write surface (D167)

The last L1 phase of RFC 0110: `striatumd.events` joins `audit_log` and
`artifacts` as a database-enforced, owner-owned `SECURITY DEFINER`-only write
surface. With `--pg-write-boundary full` and owner bundle 0004 applied, the
doctor `pg_write_boundary=full` posture licenses the sole-durable-write-path
claim — *the daemon's durable write paths are DB-enforced*.

- **Owner bundle `0004_phase2_events.sql`**: `append_event_row` (SECURITY
  DEFINER) asserts daemon authority, enforces transcript exclusion, locks the
  per-repository chain head, computes the v3 chain hash in-DB, appends the event,
  and advances the chain head — all atomic with the caller's mutation
  transaction. `REVOKE INSERT ON striatumd.events FROM striatumd_rw`; `GRANT
  EXECUTE` on the SD fn; stamp `event_sd_append`. `LatestOwnerBundleVersion` → 4.
- **The event chain hash is computed entirely in-DB, with no Go counterpart**
  (`event_v3_row_hash`, reusing bundle 0001's length-prefixed `audit_v3_enc_text`
  and folding `payload_json` in as its canonical jsonb text). Unlike the audit
  chain — whose doctor verifier recomputes hashes in Go — nothing in Go ever
  recomputes an event row hash, so the in-DB-only hash satisfies G1 (the only
  write path computes the hash and holds the chain lock in-DB) with zero
  Go↔PL/pgSQL porting hazard. The chain stays linear across the v2→v3 boundary.
- **Durable-event transcript exclusion** (`C-EVENT-NO-TRANSCRIPTS`): a payload
  with a top-level `stdout`/`stderr`/`transcript`/`raw_output`/`provider_output`
  key, or one over the 256 KiB cap, is `RAISE`d (SQLSTATE `23514`, distinct from
  the events FK `23503`) before any row lands — keeping the DB a curated record,
  not a transcript store (AGENTS.md product boundary, D028).
- **Routing**: `db.AppendEventRowSD` + `db.EventRow`; both event chokepoints
  (`mutations.appendEvent`, `reads.appendResolveEvent`) route through the SD fn at
  `ActiveWriteBoundary().AtLeast(PhaseFull)`, byte-identical direct INSERT
  otherwise (behavior-neutral until P2 is adopted). `events` → `ClassSDGated` in
  the write-authority inventory; `SupportedAuthorityCapabilities` += `event_sd_append`.
- **Gates** (green vs live PG): T-42501-P2 + T-GRANT-DRIFT, T-EXEC-AUTH (events),
  T-EVENT-NOTRANSCRIPT (forbidden-key + oversize, both directions, zero rows),
  positive end-to-end (a real event lands and the chain advances in-DB), routing
  units, T-SD-HARDEN (+`append_event_row`).
- **Deploy**: deploy order is load-bearing — the binary (supports
  `event_sd_append`) before owner bundle 0004; `--pg-write-boundary full` in
  lockstep. Rollback: `GRANT INSERT ON striatumd.events TO striatumd_rw` (as
  owner) + remove the `full` drop-in + restart. `repo_event_chain_heads` stays
  runtime-writable for parity with `audit_chain_head` (derived pointer, advanced
  by the SD fn as owner).

## v2.24.0 — 2026-06-04

### RFC 0106 — graduate the `implementation_panel` shape to `supported`

The first workflow shape to clear the RFC 0106 graduation gate (D162 → D166):
`implementation_panel` moves `experimental → supported`, earned with a genuine RFC
0105 reliability fixture rather than a bare map flip.

- **New fixture** `go/pkg/adapterconformance/implementation_panel_test.go` drives
  the real fan-out/join graph — `frame → {proposal_a, proposal_b, proposal_c} →
  scorecards → arbitration → dissent → decision` (no cycle) — through the
  production `work.claim_next` / `work.ack` / `work.complete` handlers and the real
  recovery sweep (`mutations.SweepRun`):
  - the **happy cell** proves the 3-way **fan-out** enqueues every parallel
    proposal at once and the **multi-predecessor join** at `scorecards` stays
    blocked until the last proposal completes, then the run reaches `completed`
    unattended;
  - the **fault cell** injects a hard dead lane into ONE parallel proposal and
    proves the sweep requeues it on the same attempt while the join correctly stays
    blocked (it never loses the recovered branch); a fresh lane finishes it and the
    run completes unattended with zero escalations — coverage neither the
    single-job (`minimal` / `code_change`) nor the review-cycle (`review` /
    `multi_review_synthesis`) fixtures provide.
- **Graduation** adds `implementation_panel` to `workflowtemplates.supportedShapes`
  and `adapterconformance.ReliabilityFixtureShapes`; the bidirectional guard
  `TestSupportedShapesHaveReliabilityFixture` reconciles them so the tier cannot
  lie. `docs/reference/workflow-catalog.md` now renders it `supported` and
  `workflow.lint` no longer emits `experimental_shape` on it.
- The RFC 0106 **new-shape freeze remains in force** — this graduates an existing
  shape; it does not lift the freeze. The eight remaining collaboration /
  interrogation shapes stay `experimental` until each earns its own fixture.

## v2.23.0 — 2026-06-04

### RFC 0108 Phase 4 — serialized, gated integration

The new `run.integrate` method merges a **completed** run's branch into a target
mainline branch (`--into`), **one run at a time per repository**, and **never
auto-resolves** a conflict — completing RFC 0108 ("two showerthoughts → two
product branches, at once", now integrated cleanly).

- **`HandleRunIntegrate`** (`go/pkg/mutations/integrate.go`): serialized on the
  same per-repo `lockRepo` the Phase 2/3 gates take (held across the merge so a
  concurrent integration cannot interleave). A conflicting merge is refused with
  the new error code `merge_conflict` (RFC 0111 catalog) naming the conflicting
  paths; mainline is left untouched.
- **Pure git plumbing, no working-tree mutation**: `merge-tree --write-tree`
  (read-only 3-way merge simulation — detects conflicts, produces the merged
  tree) → `commit-tree` (merge commit, `striatum-integrator` identity) →
  compare-and-swap `update-ref` to advance the mainline ref. The operator's
  checkout (in any worktree) is never touched — only the mainline ref moves —
  which makes integration safe against a live repo with other runs' per-job
  worktrees checked out.
- The integration is recorded as a `run.integrated` event **before** the ref
  advance (git is not transactional with the DB), and re-integrating into the
  same target is idempotent. No schema migration — integration lives in the event
  chain.
- New method wired through the contract (`contracts/daemon_methods.json` →
  regenerated registry + routes), capability `apply`, CLI `run integrate
  <run-id> --into <branch>`.
- **Gate** (`go/pkg/adapterconformance/multirun_test.go`):
  `TestMultiRunSerializedIntegrationMergesCleanAndSurfacesConflict`.

## v2.22.0 — 2026-06-04

### RFC 0110 Phase 1 — `audit_artifacts` write closure (§7)

The phased DB-enforced write boundary advances from P0 `audit_only` to P1
`audit_artifacts`: artifact writes now route through an owner-owned
`SECURITY DEFINER` function that asserts daemon authority in-DB before the
INSERT, so a leaked runtime credential can no longer forge an artifact.

- **Owner bundle 0003** (`go/pkg/db/sql/owner/0003_phase1_artifacts.sql`):
  `append_artifact_row` SD function (hardened: pinned `search_path`, no PUBLIC
  execute, `EXECUTE` to `striatumd_rw` only), `REVOKE INSERT ON
  striatumd.artifacts FROM striatumd_rw`, and the `artifact_sd_append`
  capability stamp. `LatestOwnerBundleVersion` → 3.
- **Phase-routed write path** (`db.AppendArtifactInTx`): the three artifact
  INSERT sites (publish, recovery, operator decision) route through the SD
  function once the active write-boundary phase reaches `audit_artifacts`, and
  the historical direct INSERT otherwise. The two paths emit an identical row.
- **`--pg-write-boundary` flag** (`STRIATUM_PG_WRITE_BOUNDARY`,
  `none|audit_only|audit_artifacts|full`): the operator-committed phase, kept in
  lockstep with the applied owner bundles. Empty derives from the audit hash
  format (v3 ⇒ `audit_only`). The `daemon doctor` `pg_write_boundary` posture is
  now this phase string, and each phase's note states exactly the claim it
  licenses (only `full` licenses "the daemon's durable write paths are
  DB-enforced", C-PHASED-WRITE-CLOSURE).
- **Capability parity** (§8.2): the binary now supports `artifact_sd_append`,
  and a P1/P2 phase requires the matching stamp — boot fails closed naming a
  missing stamp (new-binary/old-schema) or an unsupported stamp
  (old-binary/new-schema). **Deploy order is load-bearing:** deploy the binary
  *before* applying owner bundle 0003 and set `--pg-write-boundary
  audit_artifacts` in lockstep with the apply.
- **`ReassertWriteRevokes`** now derives the protected set from the stamped
  capabilities, so a grant-repair re-closes exactly the surfaces the live
  deployment has closed — never a surface whose bundle is unapplied.
- **Gates:** T-42501-P1 + T-GRANT-DRIFT (artifacts INSERT revoked, EXECUTE
  granted, drift re-closed, events untouched), T-EXEC-AUTH (artifacts), the
  phase-routing unit gate, the posture gate, and the write-authority inventory
  (artifacts → `sd_gated`).

### RFC 0110 operational follow-ups

- **#168 — stable per-daemon instance id.** `daemonInstanceID()` minted a fresh
  random id per process, so the owner-owned `striatumd.daemon_auth_registry`
  grew one row per restart and a restart within the 5-minute role-scoped rotator
  probe window (RFC 0110 §9.4) tripped a false `rotator_collision`. The instance
  id is now persisted in the daemon runtime dir (0600) and read back on the next
  boot, so a restart UPSERTs the single existing registry row.
- **#169 — two-role L0 adoption prereq documented.** The postgres-transition
  runbook and RFC 0110 §L0 now spell out the PostgreSQL 16 prerequisite that a
  non-superuser owner must hold admin option on the runtime role
  (`GRANT striatumd_rw TO <owner> WITH ADMIN OPTION, INHERIT FALSE, SET FALSE`)
  before adopting two-role boot-time password rotation.
- **#170 — doctor skips terminal-run supervisor probes and terminal cleanup stops
  lane helpers.** `doctor` now bounds its supervisor liveness probe and filters
  out supervisors whose runs are already `completed` / `failed` / `canceled`, so
  orphaned helper/tmux state from old runs cannot hang the authority-posture
  check. Terminal run session auto-close now also stops attached supervisors,
  killing tmux-backed lanes or helper PIDs when safe and recording
  `supervisor.stopped`.

## v2.21.0 — 2026-06-04

### RFC 0108 Phase 5 — repo concurrency read-view

`dashboard.all` and `status` now carry a `concurrent_runs` view: the repo-scoped
parallel fan-out, so operators and maintainers see every live run and its
collisions on one surface (the RFC 0102 attention principle).

- **`repoConcurrentRuns`** (`go/pkg/reads/concurrent_runs.go`) returns, per
  `running` run on the repo: its branch, the repo-write paths it intends to touch
  (union of its repo-write jobs' `allowed_paths`), its lane sessions
  (operator/role/lane/state), and the live collisions with other active runs — a
  shared branch (`kind:"branch"`) or an overlapping repo-write scope
  (`kind:"write_scope"`). The collision logic reuses the same branch-equality +
  bidirectional path-prefix-overlap reasoning the Phase 2/3 run.start gates apply,
  so the view and the gate agree on what collides.
- `integration_status` is a `"in_flight"` placeholder until Phase 4 populates real
  integration state. Surfaced on `dashboard.all` (per repository) and `status`
  (repo-level, independent of an optional `run_id` filter). SELECT-only.
- **Gate** (`go/pkg/reads/concurrent_runs_test.go`):
  `TestRepoConcurrentRunsSurfacesFanOutAndCollisions` +
  `TestRepoConcurrentRunsExcludesTerminalRuns`.

## v2.20.0 — 2026-06-04

### RFC 0108 Phase 3 — cross-run collision detection at run.start

`run.start` now detects when starting a run would collide with another **active**
run on the repository, distinguishing a definite collision from a potential one:

- **Same target branch → refused** with the new error code `cross_run_collision`
  (RFC 0111 catalog) unless the operator passes `--allow-overlap`. Two runs
  cannot share one git branch (they would clobber each other and collide at
  integration). The `--allow-overlap` flag flows through the generic CLI param
  parser as `allow_overlap` — no route change needed.
- **Overlapping repo-write `allowed_paths` → non-blocking warning** in the
  `run.start` result `warnings[]`, naming the colliding run and path. On distinct
  branches with per_job worktrees the runs do not collide at write time; the
  overlap only risks a *merge* conflict at integration (which Phase 4 serializes),
  so it is surfaced up front (RFC 0102 attention) rather than blocking. Path
  overlap reuses `write_scope_guard.go` normalization and is bidirectional prefix
  containment.
- Runs inside the same `lockRepo`-held transaction as the Phase 2 check, so the
  active-runs snapshot cannot race a concurrent start.
- **Gates** (`go/pkg/adapterconformance/multirun_test.go`):
  `TestMultiRunSameBranchRefusedWhileSiblingActive` and
  `TestMultiRunWriteScopeOverlapWarns`.

## v2.19.0 — 2026-06-04

### RFC 0108 Phase 2 — isolation by default under concurrency

Once a run is active on a repository, a **second** run that would write the
**shared main checkout** is now refused at `run.start`, so two concurrent runs
can never scribble one working tree. This promotes the long-standing
`repo_write_without_worktree_isolation` lint *warning* to an enforced
*precondition* — but only under genuine concurrency.

- **`HandleRunStart` precondition** (`go/pkg/mutations/run.go`): when a run
  transitions `ready -> running` and **another run on the same repo is already
  `running`**, the start is refused if the run has a repo-write job on a lane
  without `worktree_isolation: per_job`. The decision reuses the exact isolation
  logic buildPacket / `HandleWorktreeCreate` already apply
  (`laneWorktreeIsolation` × `isRepoWrite`), so the gate and runtime never
  disagree. A per_job-isolated run (its own detached worktree), a document-only
  run, and the single-run case (no sibling active) all start unaffected.
- **New error code `concurrent_run_isolation_required`** (RFC 0111 catalog,
  `go/pkg/rpc/error_catalog.go` + authority-matrix doc): the refusal names the
  active run and the offending job and suggests the fix — set
  `worktree_isolation: per_job` on the repo-write lane, or wait for the active
  run to finish.
- **Race-free**: `HandleRunStart` takes a per-repository advisory lock
  (`lockRepo`) first, so concurrent starts serialize and two runs can never both
  observe "no sibling active" and race onto the shared checkout. `lockRepo` is
  wider than RFC 0104's per-(repo,run) `lockRun`; no mutation takes both, so they
  cannot form a lock-ordering cycle.
- **Gates** (`go/pkg/adapterconformance/multirun_test.go`):
  `TestMultiRunIsolationRequiredWhenSiblingActive` and
  `TestMultiRunConcurrentUnisolatedStartsResolveToOne` (N concurrent unisolated
  starts resolve to exactly one `running` + N−1 refusals, no `40P01`, chain
  linear).

## v2.18.2 — 2026-06-04

### RFC 0110 Release N+1 (slice 2) — fix: owner bundle 0002 runtime parity read grant

Found during the live v3 cutover. Owner bundle 0001 created
`striatumd.schema_authority` (the capability-stamp table) as write-owner-only but
never granted the runtime role SELECT. `db.VerifyCapabilityParity` reads that
table *as the runtime role* (`striatumd_rw`) at startup, so once any owner bundle
is applied the daemon failed parity with `permission denied for table
schema_authority (42501)` and crash-looped under systemd `Restart=on-failure`. The
slice-1 parity tests ran as the PEER owner and could not catch a runtime-role gap.

- **Owner bundle `0002_runtime_grants.sql`**: `GRANT SELECT ON
  striatumd.schema_authority TO striatumd_rw` (read-only, idempotent; the table's
  write class stays owner-only, RFC 0110 §13). `LatestOwnerBundleVersion` → 2.
  Additive — applied by `striatum daemon owner-ddl apply` on top of 0001.
- **Gate — `TestParityReadGrant`**: after `ApplyOwnerBundles`, asserts
  `striatumd_rw` holds SELECT (and not INSERT) on `schema_authority` via
  `has_table_privilege` — the runtime-role privilege oracle the slice-1
  enforcement tests use, which the owner-pool parity tests missed.

> **Operator note (PG 16 L0 adoption prerequisite):** the owner DSN's role must
> hold ADMIN OPTION on the runtime role to rotate it — `ALTER ROLE … PASSWORD`
> requires CREATEROLE **and** ADMIN OPTION on the target in PG 16. Provision once
> as a superuser: `GRANT striatumd_rw TO <owner> WITH ADMIN OPTION, INHERIT FALSE,
> SET FALSE;`. Without it, the L0 bootstrap fails closed at startup.

## v2.18.1 — 2026-06-04

### RFC 0110 Release N+1 (slice 2) — fix: restart-safe L0 boot order

A correctness fix for the two-role L0 credential bootstrap shipped in v2.18.0.
The daemon brought up its runtime pool from the `daemon.toml` password *before*
`BootstrapAuthority` rotated that password (`ALTER ROLE … PASSWORD`) to a fresh
RAM-only value. Because the rotation is never written back to `daemon.toml` (by
design — §9.1: "A DSN captured before a restart fails after it"), the boot
worked once but the *next* restart would present the now-stale password to the
initial runtime connect and `log.Fatalf` — a crash loop under the unit's
`Restart=on-failure`. This made the deferred operator v3 flip unsafe: the first
restart after the flip would brick the daemon until the role password was reset
by hand.

- **`db.BootstrapAndConnect`** establishes the runtime pool in the §9.1 order:
  run the authority bootstrap over the owner connection FIRST (rotating the
  runtime password where two-role), THEN connect the runtime pool with the
  rotated credential. `ALTER ROLE … PASSWORD` does not need the old password, so
  the runtime connect always uses a current credential regardless of what
  `daemon.toml` holds — restart-safe across any number of restarts. The old
  connect-then-reconnect dance in `cmd/striatumd` is removed.
- **Inert path unchanged**: with no owner bundle applied (the live daemon), the
  bootstrap is schema-absent, no rotation occurs, and the runtime pool connects
  with the configured DSN exactly as before — so installing this binary on the
  unflipped production daemon is behavior-neutral.
- **Gate — `TestBootstrapAndConnectRecoversFromStaleRuntimePassword`** (two-role
  TCP): boot once (rotates the password away), assert a direct connect with the
  now-stale password fails (the brick precondition), then assert a second
  `BootstrapAndConnect` with the same stale config still comes up.

### RFC 0108 Phase 1 — multi-run composition gate (hermetic, no new behavior)

The standing gate proving ≥2 independent runs compose on ONE repository, the
multi-run extension of the RFC 0105 reliability harness. New
`go/pkg/adapterconformance/multirun_test.go`:

- **`TestMultiRunConcurrentComposeNoDeadlock`** — drives batches of independent
  run lifecycles (implement → complete → review → accept → completed)
  CONCURRENTLY on one repo through the production handlers. Asserts every run
  reaches `completed`, no driver surfaces a `40P01`/deadlock (RFC 0104 lockRun +
  the single shared `repo_event_chain_heads` lock cannot form a cross-run
  cycle), and the per-repo event hash chain stays linear + verifying as it grows
  under sustained concurrent appends. A serial warmup run establishes the chain
  genesis first, mirroring production (repo registration + run prepare/start
  append the first events serially, long before two runs claim at once).
- **`TestMultiRunPerJobWorktreesComposeIsolated`** — two concurrent repo-write
  runs each get their OWN confirmed branch + OWN detached worktree (distinct
  on-disk paths, no shared checkout), with no `40P01` (RFC 0008 substrate).
- **`TestMultiRunSharedCheckoutTurnsRed`** — the "deliberately-induced shared
  checkout turns it red" boundary: `worktree_isolation` off makes the substrate
  refuse an isolated checkout up front (the precondition Phase 2 promotes to
  isolation-by-default), so the shared-checkout hazard is surfaced, never
  silently allowed.

## v2.18.0 — 2026-06-04

### RFC 0110 Release N+1 (slice 2) — L0 rotation + R-V3 cutover mechanism (default v2)

The live-activation mechanism for the daemon→PostgreSQL write boundary, behind
an operator-committed flag **defaulting to v2**. Still additive and live-safe:
the mechanism is **inert unless the owner bundle is applied** (the live daemon
has no bundle, so it is unchanged), and the v3 flip is a deliberate operator
step — this release does not flip any running daemon.

- **L0 credential bootstrap** (`db.BootstrapAuthority`, RFC 0110 §9): at startup,
  over an owner connection, the daemon generates + registers a per-instance
  `crypto/rand` authority secret (digest in `daemon_auth_registry`) and, in the
  two-role posture, rotates the runtime password (`ALTER ROLE`) and reconnects
  with it. Single-role (owner==runtime) skips rotation
  (`rotation_skipped_single_role`). Fail-closed on any owner-connection failure
  (`daemon_pg_owner_bootstrap_failed`, §9.2). Role-scoped rotator probe (§9.4).
- **The daemon presents its secret**: the in-transaction prelude now carries the
  live secret (`AuthorityFromContext` + the process `AuthorityRuntime`), so
  authorized mutations satisfy `assert_daemon_authority`.
- **R-V3 cutover** (`audit.hash_format` flag, `--audit-hash-format` /
  `STRIATUM_AUDIT_HASH_FORMAT`, **default v2**, §5.2): when set to `v3` the
  daemon's audit append routes through the SD `append_audit_row` (v3 in-DB hash,
  authority-asserted, atomic with the mutation); `v2` keeps the Go path. A
  lost/superseded registry surfaces a structured `daemon_auth_lost` (§4.5).
- **doctor** surfaces the live authority posture (rotation, `audit_hash_format`,
  rotator collision); never reads the secret.

**Operator runbook — the v3 flip (forward-only) and rollback:** (1) apply the
owner bundle (`striatum daemon owner-ddl apply`); (2) restart the daemon (it
registers its secret; still v2); (3) set `--audit-hash-format=v3` and restart to
cut over. Rollback before the flip is a two-way door (restart with the flag
unset/v2 — no v3 row was produced). After the flip, v3 rows exist and the format
is forward-only; a uniform post-flip `VerifyRows` failure indicates clock/format
skew (see the spec R-V3 checklist), an isolated failure indicates tamper.

Gates: T-ROLLBACK-POSTURE, T-PRELUDE-OBSERVER, T-AUTH-LIVENESS, T-OWNER-FAILCLOSED,
T-DOCTOR-SINGLEROLE, T-ROTATOR-SCOPE, two-role rotation+reconnect (TCP). Deferred:
the operator-driven live v3 flip; P1/P2 surface closure; `#88-dynamic-creds`;
L2 (#87).

## v2.17.0 — 2026-06-04

### RFC 0110 Release N+1 (slice 1) — authority schema + v3 hash (additive, opt-in)

The DB-enforced write-boundary schema, proven via pgtest. **Additive and
opt-in**: a binary with the owner bundle UNapplied behaves exactly like v2.15.0
(Release N) — the live single-role daemon (it connects as the table owner) is
unaffected and stays on the v2 Go audit path. Live activation (L0 credential
rotation, the daemon presenting its secret, the R-V3 hash flip) is deferred to a
later slice.

- **Owner-DDL bundles** (`C-OWNER-DDL-SPLIT`): new `go/pkg/db/sql/owner/*.sql`
  embed + `ApplyOwnerBundles` (atomic per version, idempotent, marker stamped
  last), applied out-of-band as the owner via **`striatum daemon owner-ddl
  apply`** — the runtime role never DDLs owner objects (RFC 0079 §5).
- **Authority schema** (owner bundle 0001): `daemon_auth_registry` (owner-only —
  `T-REGISTRY-ACL`), `daemon_auth_log`, `assert_daemon_authority()` (raises
  SQLSTATE 28000 unless the presented `striatum.daemon_auth` secret matches a
  registry digest — the secret is authority), the `append_audit_row` SECURITY
  DEFINER write path, and Phase-0 (`audit_only`) `REVOKE INSERT ON audit_log`.
  SD-hardening template throughout (`T-SD-HARDEN`).
- **v3 audit hash** (`C-AUDIT-FORMAT-CUTOVER` builder): Go `V3RowHash` is
  byte-identical to the in-database `audit_v3_row_hash` (`T-HASH-PARITY`),
  timezone-independent (`T-TS`); `VerifyRows` dispatches per
  `hash_format_version` (2/3/unknown→fail, `T-VERIFY-MIXED`); `V2RowHash` kept
  permanently. The Go daemon still writes v2 until the deferred R-V3 flip.
- **Deploy capability parity** (`C-DEPLOY-CAPABILITY-PARITY`): the owner bundle
  stamps `audit_sd_append`; the binary declares it supported, so an old binary
  meeting an authority-bearing schema fails closed at startup (`T-DEPLOY-SKEW`).
  Fixes a latent v2.15.0 bug where `readStampedCapabilities` read a bare boolean
  (pgx scans `t` not `true` under simple protocol) and would silently skip the
  old-binary check once a bundle was applied.
- **Phase-0 enforcement** proven against the production bundle SQL, not
  imperative Go grants (`C-PGTEST-NO-DML-GRANT` intent): `T-42501-P0`,
  `T-EXEC-AUTH`, plus `ReassertWriteRevokes` for `T-GRANT-DRIFT`.
- **Write-authority inventory** (`PX3-006`): every `striatumd.*` table is
  classified (`sd_gated`/`runtime_dml`/`owner_only`); a guard fails on any
  unclassified live table.

Deferred to the next slice: L0 rotation + `STRIATUM_OWNER_DB_URL` + fail-closed
owner dep; the daemon generating/presenting its secret; the R-V3 flag flip
(default v2) + `T-PRELUDE-OBSERVER`/`T-AUTH-LIVENESS`/`T-ROTATOR-SCOPE`; then P1
`audit_artifacts` / P2 `full`, then L2 (#87 closure).

## v2.16.0 — 2026-06-04

- **RFC 0111 accepted (D165) and fully implemented — in-band failure
  legibility.** P1: `toolResult`/`contentSummary` (`go/pkg/mcp/tools.go`) now
  render the dispatchable error code + message into the MCP `content` text an
  agent reads — a failed state-changer is no longer a contentless
  `<error>method</error>`; success stays a terse one-line summary;
  `structuredContent.error`/`error_message` unchanged. Live-verified against
  the restarted daemon. P2+P3 below were built through a reviewed
  implementation dogfood (`dogfoods/rfc-0111-p2p3/`, claude implementer + agy
  reviewer, verdict accept) and also live-verified end to end.
- **#160 — one-line installer.** New root `install.sh`: OS/arch detection,
  SHA256SUMS verification, installs `striatum` + `striatumd` +
  `striatum-supervisor-helper` from a GitHub Release (latest or `--version`),
  and surfaces the daemon-restart step (`--restart-daemon` to run it) so a
  replaced binary is not mistaken for a deployed one. Tested against the real
  v2.12.0 and v2.15.0 release assets. The existing tag-driven `release.yml`
  remains the build pipeline (no goreleaser duplication; npm deferred).
- **#161 — `ARCHITECTURE.md`.** Single navigable map of the substrate
  (components, state ownership, surfaces, write boundary, run model, failure
  legibility), linked from `README.md`, `AGENTS.md`, and `docs/index.md`.
- **#148 / RFC 0088 — `claude --print` lanes are deprecated, not hardened.**
  `claude --print`/`-p` is the retired one-shot mode (RFC 0088 / D148 retired
  `-p`/`--print`/`exec` for all lanes in favor of daemon-owned interactive
  agent-loop PTY sessions). It cannot run the work-packet loop — it prints once
  and exits without claiming — so a `--print` lane silently parks/dies (the #148
  symptom). New `workflow lint` warning `deprecated_claude_print_lane` flags any
  `claude --print`/`-p` lane (fires even with `agent_loop=true`, since `--print`
  defeats the interactive loop) and points to the supported shape
  (`["claude","--dangerously-skip-permissions"]` + `adapter_capabilities.agent_loop`),
  mirroring `agy_one_shot_pipe_lane` (D156). Also fixed the shipped
  `workflow.generate` example in the `mcp` skill templates, which still showed
  `["codex","exec"]` / `["claude","--print"]` — the source of such lanes — to the
  supported interactive commands. (The right answer to "a `--print` lane wedges"
  is not to harden `--print`; it is to not configure one. The silent-death
  *diagnostic* remains as a separate general lane-death legibility item.)
- **RFC 0111 P2+P3 — in-band remediation + closed error-code catalog.**
  `rpc.Error` gains a first-class `Suggestion` field (`suggestion,omitempty`).
  `ErrorResponse` fills it centrally from the new per-code catalog when a call
  site sets none (explicit call-site suggestions win), threads it through
  `Response.Data`, and the MCP boundary renders it in-band: a failing
  `tools/call` content text now reads
  `<method> failed: <code>: <message> — suggestion: <s>`, and
  `structuredContent` gains a sibling `suggestion` key (the
  `error`/`error_message` contract is unchanged). `go/pkg/rpc/error_catalog.go`
  enumerates all 62 live error codes (code, meaning, default suggestion) as a
  closed contract; `go/pkg/rpc/error_catalog_test.go` guard-reconciles it in
  both directions against the source literals (`NewError("…")`, `Code: "…"`,
  capability `DenialReason` values) and against the new "Error code catalog"
  section of `docs/reference/command-authority-matrix.md`, whose stale
  reference to the retired Python authority guardrails now names the live Go
  guard tests instead.
- **Mission acceptance met — 10× unattended, zero-rescue DoD (live).** Added
  `scripts/dod/driver.py`, the outside-CI live acceptance: it drives N consecutive
  multi-lane, review-gated, revision-capable runs to `completed` using only normal
  lane lifecycle verbs (register-session / supervise start / close-completed-lane),
  never a rescue verb. Result of record (2026-06-03, against the deployed
  v2.14.0 daemon): **10/10 consecutive clean unattended passes** — every run
  completed, every review `accept`, zero rescue-type events, zero escalations
  (independently verified in the daemon). Unblocked by #162 (session-bound token
  `read`) and #163 (claude workspace auto-trust). This is the standing proof of
  "no human intervention", complementing the hermetic RFC 0105 harness.

## v2.15.0 — 2026-06-03

### RFC 0110 Release N — daemon→PostgreSQL authority plumbing (behavior-neutral)

First implementation release of RFC 0110 (daemon→PostgreSQL authentication +
DB-enforced write boundary, D164). Release N installs the *plumbing* with **no
schema authority yet**, so it is behavior-neutral by design (no new client
denials); the owner-DDL bundle, L0 rotation, and phased write closure follow in
N+1. Step-0 documentation gate landed first (D164/spec.md/RFC body amended;
read-scope successor #164 and scheduler-gap #165 filed; #87 reopened with its
four L2 closure gates).

- **In-transaction authority/attribution prelude over the extended protocol**
  (`C-EXTENDED-AUTH-PRELUDE`). `TxRunner.ExecBound` forces
  `pgx.QueryExecModeExec` so the daemon-authority secret and the
  `rpc_id`/`principal_id`/`session_id` labels travel in Bind messages and never
  appear in `pg_stat_activity.query`; the pool default stays simple protocol for
  migration DDL. Gate `G-PRELUDE-MODE`.
- **`BeginAuthorizedMutation` constructor** (`C-AUTH-TX-WRAPPER`) issues the
  prelude as the transaction's first statement (`set_config('striatum.daemon_auth',…)`
  is statement 1, secret empty in N). All mutating handlers route through it at
  the single `withTx` chokepoint; admin token handlers migrated off raw
  `BeginTx`. Gates `G-MUTATION-TX` (source-scan + allowlist + doc-presence
  PX3-005), `T-SQL-ORDER`.
- **Bounded-discard reset** (`C-ATTR-RESET-FAIL`/OPS-12): transaction-local GUCs
  vanish at every transaction outcome; a connection released with leftover
  transaction state is destroyed (counted for the doctor reconnect-storm
  signal), a clean connection returns to the pool with no DISCARD. Gate
  `T-ATTR-RESET` (commit/rollback/cancel/timeout/panic).
- **Fail-closed, mutation-coupled audit append** (`C-AUDIT-AUTH-PRELUDE`): a
  successful mutation appends its audit row as the final write **inside its own
  transaction** (atomic); standalone appends (reads/denials/errors) convert an
  append failure into an `audit_append_failed` error. The fail-open
  ignore-`auditErr` path is removed. Gate `T-AUDIT-FAILCLOSED`.
- **Inert deploy capability-parity checker** (`C-DEPLOY-CAPABILITY-PARITY`)
  wired into startup — a pass-through until N+1 stamps markers, which is what
  makes the old-binary check real later.
- **doctor `pg_write_boundary` posture** (`none` in N + `rotation_skipped_single_role`
  + `conn_reset_destroys`) and the **`daemon_auth_log` redactor**
  (`C-AUTH-LOG-PRIVACY`, strict key whitelist + DSN/credential redaction; DB
  insert inert until N+1). Gate `T-AUTHLOG-REDACT`.

Deferred to N+1+: owner-DDL bundle (`daemon_auth_registry` + `assert_daemon_authority`
+ `append_audit_row` SD fn), L0 rotation, pgtest replumb, v3 hash R-V3 cutover,
phased P0/P1/P2 closure, L2 socket hardening.

## v2.14.0 — 2026-06-03

- **#163 — supervised claude lanes auto-trust their workspace (no more parking on
  the trust dialog).** claude 2.1.x prompts "Is this a project you trust?" the first
  time it runs in a directory and parks an interactive PTY lane there;
  `--dangerously-skip-permissions` does not bypass it, and the dialog is only skipped
  for already-trusted dirs or `-p` mode — so a claude lane on a fresh target repo
  silently wedged before it could claim (the #148-class park). The agent-loop
  executor now pre-accepts the per-folder trust for the lane's `repo_root` in
  `~/.claude.json` before launching claude (idempotent, once-per-repo, best-effort,
  preserves the operator's existing config; a corrupt config is left untouched).
  Guarded by the `seedClaudeWorkspaceTrust` / `ensureClaudeWorkspaceTrusted` tests.

## v2.13.0 — 2026-06-03

### Autonomy regression — supervised lanes could not claim

- **Session-bound lane token now carries `read`, so a supervised lane can actually
  start.** Once #135 wired the session-bound capability token into the supervised
  lane's env, the token granted only `{claim, write}` — but the agent-loop daemon
  receiver polls `supervise.status` (a `read` method) as its `work.await_packet`
  readiness gate, so every supervised lane was denied with "daemon RPC
  authorization failed" and looped forever without ever claiming its first packet
  (a silent rescue-forcing wedge, found by the v2.12.0 live floor-dogfood
  acceptance). `sessionBoundCapabilities` now includes `read`. Guarded by
  `TestSessionBoundTokenCoversLaneLoopRPCs`, which couples the grant set to the live
  daemon registry's `RequiredCapability` for the RPCs a lane drives.

### RFC 0103 tail (reduce autonomous-lane friction)

- **#115 — `supervise start` warns when the run's frozen snapshot diverges from the
  on-disk workflow.** A prepared/running run uses the workflow snapshot captured at
  `run prepare`, so editing `workflow.json` afterward is a silent no-op — operators
  burned time before discovering the run was pinned. `supervise start` now compares
  the snapshot's `content_sha256` against the canonical sha of the current file at
  its `source_path` and, on a positive mismatch, returns a
  `snapshot_divergence_warning` ("…edits will NOT apply — prepare a NEW run…"). The
  comparison is over the same `json.Marshal` canonical form `run prepare` used, so
  cosmetic edits (whitespace, key order) do not false-trigger; an unreadable/now-
  invalid file stays silent. Gate: `TestWorkflowSnapshotDivergence`.
- **#126 — front-matter rejections name their enums and embed a copy-pasteable
  skeleton.** A supervised review lane that wrote a `striatum.finding.v1` artifact
  needed multiple repair attempts because `severity: blocker` was rejected with a
  bare "is invalid" (the `severity` enum was missing from the error-message source)
  and because the rejection never showed the valid shape. `enumFieldValues` is now
  synced with every schema `oneOfValue` field (severity, blocker_kind,
  retrieval_priority, status, scope_kind, state, gate_status, shape, …) so every
  enum rejection names its allowed values, and `ValidateFrontMatter` rejections now
  embed the minimal valid front-matter block for the kind (via the new
  `artifactcontracts.Skeleton(kind)`). Guarded by `TestEnumFieldValuesMatchValidators`
  (drift), `TestSkeletonRoundTripsForFindingKinds`, and the severity/skeleton
  rejection gates.

## v2.12.0 — 2026-06-03

### Autonomy cluster (recovery/liveness rescue-blockers)

- **#147 Symptom A — `supervise status` exposes distinct liveness signals.** The
  single `liveness` field (gone/alive/stalled) was computed off the supervisor
  bridge and conflated three independent facts, so an operator could not tell a
  detached/stopped bridge whose agent child is still alive (a false `gone`) from a
  genuinely dead lane. The status projection now also surfaces `agent_pid_alive`
  (the probed liveness of the supervised process), `supervisor_state` (the bridge
  state), and `lease_fresh` (whether the work lease is still live). With Symptom B
  already fixed (`de30eb11`, masked-dead-agent requeue), this closes #147.
- **#146 (partial) — supervised lanes report an honest delivery mode.** A lane that
  does not use the agent loop is a stdin-FIFO/push consumer, not a true self-driver
  that calls `work.await_packet`, yet every supervised lane was hardcoded
  `agent_loop_mode: self_driving`. That made `claim-next` emit the misleading
  `self_claim_note` ("the agent self-claims … do not run `supervise send`") for push
  wrappers — the exact opposite of what they need — sending operators down a dead
  path. `supervise start` now records `supervised_push` for non-agent-loop lanes, so
  `sessionHasSelfDrivingSupervisor` (and the claim hint) is accurate: push lanes get
  the `supervise_send` hint, true agent-loop lanes keep the self-claim note. (The
  push-lane auto-dispatch half of #146 remains; the supported autonomous path is
  agent-loop lanes, which already auto-dispatch via `work.await_packet`.)
- **#144 — recovery auto-publish of a review now records its verdict.** When the
  stale-lease auto-publish pass completed a review/phase_synthesis job from its
  on-disk finding artifact, it recorded no verdict, so the verdict-gated
  `--accepted review-->` downstream edge never fired and the run wedged with every
  job green (and the operator's obvious `override-verdict` via `register-session
  --replace` knocked the completed job into messageless `queued`). The auto-publish
  pass now recovers the verdict from the finding's `verdict_intent` front matter and
  runs the same completion / bounded-cycle / downstream routing `review.verdict`
  does (the shared core was factored out as `applyVerdict`, which tolerates the
  stale lease). accept / accept_with_findings / needs_revision are applied; a
  recovered `reject` (whose interactive self-correction guard would error) falls
  back to plain completion rather than wedging the sweep.
- **#145 — recovery no longer false-requeues an actively-working lane.** The
  liveness classifier's lease-heartbeat rung tripped `agent_lease_heartbeat_stall`
  (→ `ProtocolStalled`) without considering PTY/tool output, unlike the adjacent
  protocol-idle rung. A lane running a long foreground command (a full test suite,
  a browser-acceptance profile) emits no work-heartbeat for minutes while its
  PTY/tool timeline stays fresh — so the recovery decision tree's CASE 2
  transfer-requeued it mid-work, closing the session and losing the artifact. The
  rung now resolves to `working_local`/`working_tool` when output is demonstrably
  fresh (the G2 invariant the rest of the classifier already honors); a lane that
  goes quiet past the PTY window still trips the stall, preserving dead-lane
  detection. Also fixed a latent same-second lease-resolution tiebreak in
  `recoverStuckJobs` (prefer the `active` lease) so a prior attempt's released
  lease can no longer resolve over the live one and falsely requeue.

## v2.11.0 — 2026-06-03

### RFC 0109 — agy lane first-class seat (P3: the standing installed-CLI gate + graduation; RFC closed)

- **The agy seat is now `supported`, and a standing gate keeps it that way.** RFC
  0109's scope guard closes only when P3 (the standing installed-CLI conformance
  gate, #149) lands alongside P1 (the four defects #95/#85/#76/#139). Both landed:
  the agy seat graduated `degraded` → `supported`.
- **P3 — installed-CLI conformance gate (`adapterconformance.RunInstalledCLI`).**
  A new runner drives the **real** agy CLI through the production agent loop over a
  two-turn `claim → publish → claim` cycle and asserts the **same attested session**
  drives both turns (#95). The Layer-2 harness gained a unix-socket RPC listener
  (`rpc.Server.Serve`, the production pair) so the agent-loop receive loop is driven
  — `RunLive`'s in-process testagent reuses its session by construction and is
  structurally immune to #95, so this installed-CLI path is the instrument that
  first makes #95 reproducible. New `agentloop.RunContext` threads a caller context
  for the test. Gated behind `STRIATUM_P3_INSTALLED_CLI` (a release-blocking
  scheduled tier; skips cleanly when the CLI or `STRIATUM_PG_TEST_URL` is absent).
- **Finding: the historical agy defects no longer reproduce against the current
  CLI.** agy holds the two-turn seat (green ×3), launches past the folder-trust /
  telemetry prompts (#76/#139), and reaches `work.claim` without a discovery stall
  (#85). P1's defects are resolved as-of the current CLI; the gate's enduring value
  is **anti-re-rot** — the day a CLI version bump or config drift breaks the seat,
  CI goes red instead of a live panel three weeks later.
- **Graduation.** `adapterconformance.InstalledCLISeatFixtures` and
  `workflowtemplates.supportedSeats` both carry `agy`, reconciled in both directions
  by `TestSupportedSeatsHaveInstalledCLIFixture` (the tier cannot lie). `degradedSeats`
  is now empty; `RegisterDegradedSeatForTest` keeps the `degraded_seat_lane` warning
  path under test. `codex` stays `experimental` (it does not reach `work.claim`
  against the in-process httptest harness; it works live — its hermetic MCP path is a
  follow-up).
- **Live-corroborated (`run_139c5981`).** A 3-lane (claude + codex + agy)
  interrogating panel held the agy seat across a `needs_revision` cycle (agy voted
  `needs_revision` on attempt 1, the presenter revised, agy re-reviewed + accepted on
  attempt 2 under a fresh attested session — the #139 inverse). The same run then
  **survived a mid-run `systemctl restart striatumd`** and finalized (codex +
  interrogable presenter resumed post-restart; `daemon.recovery_sweep` re-bridged the
  lanes). A direct agy-restart-while-leased leg is the one tracked follow-up.

## v2.10.0 — 2026-06-03

### RFC 0109 — agy lane first-class seat (P2: count the seat as a support tier)

- **The agy seat is now a *counted* tier, not a silent collapse.** RFC 0109 §B
  names the meta-defect: the agy seat has been "the deferrable one" across RFC
  0088/0096/0101/0103 because its brokenness never *blocked* anything, so the cost
  was never counted (#139 is the only issue that names the collapse, and only as a
  tolerated degradation). P2 makes the degradation a recorded, surfaced event.
- **Seat support tiers (the seat analog of RFC 0106 shape tiers).**
  `workflowtemplates.SeatTierForAdapter` classifies each adapter seat
  (`supported` | `experimental` | `degraded` | `unsupported`): **agy=`degraded`**
  (#95/#85/#76/#139, with an operator-facing reason); every other adapter
  =`experimental` (holds a seat in practice but is **ungated by an installed-CLI
  fixture** — the RFC's own thesis); **no seat is `supported`** until its RFC 0109
  P3 installed-CLI gate is green.
- **`workflow.lint` surfaces degraded seats** via the non-blocking
  `degraded_seat_lane` warning: a workflow declaring an `agy` lane now warns that
  the run may deliver one fewer voice than it names (closing the silent-collapse
  half of #139), while working-but-ungated claude/codex stay silent. Faithful to
  the RFC: surface `degraded`/`unsupported`, not `experimental`.
- **The seat tier cannot lie.** `adapterconformance.InstalledCLISeatFixtures` (the
  RFC 0109 P3 backing registry, empty until #149) plus the graduation guard
  `TestSupportedSeatsHaveInstalledCLIFixture` reconcile the supported-seat set
  against the fixture registry in both directions — so a seat cannot be marked
  `supported` without a green installed-CLI fixture, exactly as
  `TestSupportedShapesHaveReliabilityFixture` backs the RFC 0106 shape tier with
  RFC 0105. golangci-lint 0 issues; build/vet clean.
- **Not done yet (scope guard):** this is **P2 only**. RFC 0109's "Definition of
  done" requires **P3** (the standing installed-CLI conformance gate, #149) landed
  **alongside P1** (#95 keystone / #76 / #85 / transport). P1-without-P3 — and P2
  alone — does **not** close RFC 0109.

### RFC 0107 — multi-principal trust model (self-hosted, not SaaS)

- **Multi-user is now a deliberate, bounded design over the existing trust
  substrate** — not an ad-hoc accretion on the single-operator assumptions. A
  **principal** (`kind` ∈ `human` | `ai_operator` | `service`) is a named
  identity that holds capability tokens; it sits above the `clients` /
  `client_capabilities` tables and owns one or more clients (the principal→client
  link survives token rotation, which mints new client rows). The `human` kind
  generalizes RFC 0053's single escalation-only human to several humans.
- **Isolation reuses the existing substrate unchanged:** per-principal capability
  + repository scoping via `client_capabilities.repository_id` (RFC 0028) and
  session-binding (RFC 0096) — a session-bound token acts only as its own session,
  so principal A's token cannot act for principal B's session
  (`rpc.AuthContext.MayActAsSession`, the shared predicate). The daemon-global
  hash-chained audit log attributes every mutation to a principal by resolving
  `client_id` through `principal_clients`.
- **Owner-table-trap-safe migration** `0023_principals.sql` (`LatestDaemonDBVersion`
  22→23): two NEW tables only (`principals`, `principal_clients`) — no `ALTER` of
  an owner-held table, and `principal_clients.client_id` is a bare column with no
  FK to `clients` (referential integrity enforced in Go, like `audit_log.client_id`),
  so the runtime role can apply it. `daemon doctor` gains a `principals` block
  (kind / client-count / repositories / effective capability scope; never token
  material). `daemon.token.create`/`rotate` carry an optional principal — no new
  wire RPC method.
- **Explicitly not SaaS:** principals are local capability grants on the
  operator-owned daemon + PostgreSQL — no hosted control plane, tenant
  provisioning, external IdP/SSO, or telemetry. Cross-principal + cross-repo
  isolation and per-principal audit attribution are proven by tests
  (`TestCrossPrincipalSessionIsolation`, `TestCrossRepoIsolationIsEnforcedAndAttributed`,
  `TestPrincipalLinkResolvesAuditAttribution`, …). (D160. Landed via the parallel
  Track B branch and integrated serially — the RFC 0108 happy-path proof.)

### RFC 0106 — workflow-shape support tiers (govern the catalog, don't prune it)

- **The shape catalog now tells the truth about which choreographies survive an
  unattended run.** Each generator shape carries a `support_tier` of `supported`
  (it has a green RFC 0105 reliability fixture) or `experimental` (it exists and
  may be valuable, but is not yet proven unattended). The classification is a
  single source of truth (`workflowtemplates.supportedShapes` =
  `{minimal, review, code_change, multi_review_synthesis}`) stamped onto every
  catalog entry and rendered as a badge in `docs/reference/workflow-catalog.md`.
- **Honest, non-blocking lint.** `workflow.lint` emits an `experimental_shape`
  warning when a run declares an experimental shape ("no unattended-reliability
  gate; expect to supervise it"), and is silent on a `supported` shape or when no
  shape is declared — yolo can opt in knowingly; the default path surfaces the
  risk. It does **not** block.
- **The tier cannot lie.** A graduation guard test
  (`TestSupportedShapesHaveReliabilityFixture`) reconciles the catalog's
  `supported` set with the RFC 0105 fixture registry
  (`adapterconformance.ReliabilityFixtureShapes`) in *both* directions — marking
  a shape `supported` without a green fixture, or shipping a fixture without
  graduating the shape, fails CI.
- **No shape removed; new-shape authoring frozen.** Every choreography stays
  available (the collaboration / anti-hallucination shapes are the product
  value); the decision log records a freeze on authoring *new* shapes until the
  existing catalog graduates, redirecting velocity from breadth to depth. (D162.)

### RFC 0105 — standing unattended-reliability harness (the yolo gate)

- **The full multi-lane revision lifecycle now has a standing hermetic gate.**
  Where the RFC 0101 chaos suite proved a single-job lane self-recovers-or-
  escalates under fault, `go/pkg/adapterconformance/lifecycle_revision_test.go`
  proves the same for the real two-lane, review-gated, `needs_revision`-cycle
  lifecycle: implement (att1) → review → needs_revision → re-open implement
  (att2) → re-implement → re-review → accept → `completed`. Both lanes are driven
  through the production mutation handlers against the in-process daemon + an
  isolated pgtest database (the real state machine).
- **Fault matrix, asserting complete-or-escalate-loud (never a silent wedge):**
  `TestRevisionLifecycleHappyPathCompletes` (baseline),
  `TestRevisionLifecycleLaneDeathSelfRecovers` (the att2 lane dies mid-revision →
  the recovery sweep requeues its job on the same attempt → a fresh lane finishes
  → the re-review accepts → the run completes, with no operator and no
  escalation), and `TestRevisionLifecycleUnrecoverableEscalatesLoudly` (the att2
  lane dies repeatedly past the requeue budget → the run flips to needs_operator
  with exactly one `recovery_exhausted` escalation_inbox row).
- **A standing gate, not a one-shot:** the fixtures live in the conformance
  package, so they run under `make -C go check` (`go test ./...`) and the CI
  PostgreSQL tier on every commit — a regression in the revision lifecycle turns
  CI red. They build on RFC 0104 (a reverted run lock turns the paired deadlock
  regression red) and are deterministic (a completed lane closes its session so
  the recovery decision tree's dead-lane resolution is unambiguous across
  attempts).
- **`ReliabilityFixtureShapes`** is the per-shape graduation entry point RFC 0106
  consumes: a shape may be marked `supported` only if it has a green fixture
  here. (D161.)

### RFC 0104 — per-run serialization invariant (retire the lifecycle deadlock class)

- **The multi-lane lifecycle deadlock is fixed structurally, not tolerated.** Two
  hot per-run write paths locked the same run's rows in opposite order — claim
  (`work.claim_next`/`work.await_packet`) takes `sessions → runs → jobs`, while
  verdict-completion (`review.submit`/`review.verdict` → `maybeCompleteRun` →
  `closeRemainingSessions`) takes `jobs → runs → sessions`, with the 60s recovery
  sweep a third concurrent party. The `{sessions, runs}` inversion let Postgres
  abort one transaction with `40P01`; `withTxRetryOnDeadlock` only *tolerated* it
  (#98/#103/#137) and exhausts under multi-lane/multi-repo load — fatal under
  yolo/minimal-human-intervention where no operator retries by hand. (It explains
  why the single-`claude`-lane RFC 0097 self-hosting dogfood completed while
  multi-lane panels wedged.)
- **The fix:** generalize RFC 0101 Phase 0a's `lockRunInterrogation` into
  `lockRun` — a per-`(repository_id, run_id)` transaction-scoped
  `pg_advisory_xact_lock` — and take it as the **first statement** of every
  per-run mutation transaction (claim/await, submit-review/verdict/override, the
  lifecycle-completion paths `work.complete`/`run.cancel`/`run.retry_job`/
  `checkpoint.resolve`, the interrogation handlers, and the per-run recovery
  sweep/handlers). Unrelated runs and repositories never serialize against each
  other; the claim queue's `FOR UPDATE … SKIP LOCKED` is untouched. No schema
  change. `withTxRetryOnDeadlock` is retained only as defense-in-depth — a
  surfaced `40P01` is now a should-never-happen signal.
- **Gate-first + guarded:** a PG-gated regression (`TestRunLockClaimVerdictSweep-`
  `Deadlock`) reproduced a raw `40P01` at iteration 0 before the fix and runs
  240 iterations clean after it; a guard test (`TestPerRunHandlersTakeLockRun-`
  `First`) drives the per-run handlers through a SQL-recording runner and asserts
  the advisory lock precedes any run-scoped `FOR UPDATE`, so a future handler that
  forgets `lockRun` fails CI. The `pkg/mutations` suite and the RFC 0101 chaos
  suite stay green; `golangci-lint` reports `0 issues`. (D159; spec.md Run
  Lifecycle carries the invariant. Foundation for RFC 0105.)

### RFC 0103 W3 — the lane survives transport and daemon churn (RFC 0091 / 0101)

- **#141 — a daemon restart no longer orphans supervised lane helpers.** A
  `systemctl restart striatumd` (or an `on-failure` auto-restart) killed the
  `Setsid`-detached supervisor helper, surfacing `helper_process_gone`, even
  though a restart only means to replace the daemon process. A live mid-run
  restart test isolated **two** independent killers, both now closed:
  1. **systemd cgroup kill.** The unit defaulted to `KillMode=control-group`, so a
     restart SIGKILLed the whole `striatumd.service` cgroup (which the helper is
     in). The unit now sets `KillMode=process`, signalling only the main daemon
     process. (The tmux-backed agent lane lives in tmux's own `tmux-spawn-*.scope`
     and already survived.)
  2. **the daemon's own `exec.CommandContext` cancellation.** The helper was
     spawned with the daemon-lifetime context, so `CommandContext` SIGKILLed it
     when that context cancelled on shutdown — independent of systemd, so
     `KillMode=process` alone did not save it. The helper (and any non-tmux pipe
     lane) is now spawned with `context.WithoutCancel`, so it outlives the daemon;
     teardown still terminates helpers explicitly by PID (`supervise stop`).
  With both fixes the helper, tmux session, and agent lane survive a restart; the
  daemon re-binds the surviving helper on startup (`tmux_ok` / `reattachable`, not
  `helper_process_gone`) and the agent-loop receiver re-dials the recreated socket,
  so the in-flight repo-write job completes through the production handlers with no
  escalation. Live-corroborated twice on a real `systemctl restart` mid-run (the
  run completed end-to-end both before and after the helper-survival fix). A hard
  cgroup kill (`systemctl kill` / OOM) that still takes a helper is recovered by
  the on-demand `supervise rebridge` verb. Re-run `striatum daemon install` +
  `systemctl --user daemon-reload` to apply the unit change. Tests: the
  rendered-unit test asserts `KillMode=process`; the supervision suite covers the
  detached-spawn launch path.

- **#125 — `work.ack` is non-substitutable.** A supervised lane (observed with
  codex) could report readiness via `session.report` instead of calling
  `work.ack`, leaving its claimed work packet stuck in `claimed` (the job never
  advances to `running`) — the lane believes it is progressing while the control
  plane sees an unacknowledged packet, and the run stalls silently.
  `session.report` now flags an outstanding claimed-but-unacked packet: the report
  is still recorded (liveness is preserved), but the response and the
  `session.reported` event carry an `unacked_packet` block (`message_id`,
  `job_id`, `lease_id`, and guidance to call `work.ack` before reporting ready) so
  the control plane and the pane agree. A `session.report` does not advance a
  claimed packet — only `work.ack` does. Test: a PG-gated regression claims a
  packet through the production handler, asserts a subsequent `session.report`
  flags the unacked packet, and asserts the flag clears after a real `work.ack`.

### RFC 0103 W4 — the interrogation window outlives a single reviewer (RFC 0095)

- **#131 — a retry/replacement reviewer no longer wedges when the panel
  interrogation window has closed.** After an interrogating panel completes, the
  panel-owned `awaiting_interrogation` window is retired and the interrogable
  target session closes. A retry/replacement review attempt that the workflow
  still expects could then call `interrogation.open` against the now-closed
  target and receive a hard `target_unavailable` error, wedging the reviewer on a
  mandatory interrogation it could never satisfy (the target's lane is genuinely
  gone — a revision reopen closes it and spawns a fresh lane, so the window cannot
  be reopened against the dead session). `interrogation.open` now returns a
  structured, non-wedging `interrogation_unavailable` result (`reason:
  panel_window_closed`, plus proceed-on-the-published-artifact `guidance` and the
  `interrogable_job_id`) when the target is a legitimately-retired panel target in
  the interrogator's run. The replacement reviewer proceeds on the published
  artifact and reaches a verdict (there is no daemon gate requiring a completed
  interrogation before a verdict). A genuinely bogus target — one that never
  entered an interrogation window — still receives the hard `target_unavailable`
  error. Test: a PG-gated regression drives a full panel to terminal (window
  retired), re-opens a review job for a replacement reviewer, asserts the
  non-wedging signal, and drives the replacement to an `accept` verdict.

## v2.9.3 — 2026-06-02

### RFC 0103 W1 — the supervised lane becomes a real sandbox (RFC 0096 V2)

- **#135 — the lane authenticates with its OWN session-bound token.** A supervised
  lane previously inherited the daemon's shared operator-override `STRIATUM_MCP_TOKEN`,
  so the per-session impersonation guard (shipped as a mechanism in v2.9.1) never
  bit in live runs. `supervise start` now mints a session-bound capability token and
  injects it as the lane's `STRIATUM_MCP_TOKEN`, and the supervised-env allowlist no
  longer passes through `STRIATUM_MCP_TOKEN`/`STRIATUM_MCP_TOKEN_FILE` — the only
  token a lane can carry is its own bound one (a missing mint fails loudly rather
  than silently falling back to the override). The cross-session guard
  (`enforceSessionBinding`) is now applied across the whole session-scoped surface —
  `work.claim_next`/`await_packet`/`ack`/`heartbeat`/`release`/`complete`/`block`,
  `work.send_message`, and `artifact.publish` — not just `interrogation.answer`, so a
  bound token can only ever act as its own session. Tests: the conformance C2 golden +
  the `mutations` env golden assert the lane carries the bound token and drop the
  shared override and all DSN vars; new PG-gated cross-session-rejection tests for
  `artifact.publish` and `work.claim_next`; an `enforceSessionBinding` contract test.
- **#70 — agy bearer token never enters git provenance.** The ephemeral agy
  `.gemini/settings.json` (which carries a rotating bearer) is now added to the work
  tree's local `.git/info/exclude` for the lane's lifetime, so it never appears in
  `git status`, never dirties a write-scope baseline, and is never swept into a commit
  by `git add -A`. The exclusion (and the file) are removed on every teardown path; an
  operator's pre-existing excludes are preserved. Worktree-aware
  (`git rev-parse --git-path info/exclude`). Hermetic fixture asserts the credentialed
  file stays out of `git status`.
- **#87 — `doctor` surfaces lane↔PostgreSQL reachability + adoption runbook.** The
  DSN-leak half already shipped (env allowlist). For the residual same-OS-user
  peer-auth reachability, `striatum doctor` now reports a `lane_sandbox` block and
  warns `lane_pg_reachable` until a dedicated PG-less lane OS user is adopted
  (`STRIATUM_LANE_OS_USER`); the full close is documented in the new
  `docs/how-to/lane-sandbox.md` adoption runbook (create an unprivileged lane user
  with no PG role, deny it in `pg_hba.conf`, run lanes as it). Configuration-posture
  proxy only — no DSN/token value is read.

## v2.9.2 — 2026-06-02

### #142 — `list workflows` queries the real `workflow_snapshots` columns

`HandleListWorkflows` selected `snapshot_sha256` / `captured_at`, but the table
has `content_sha256` / `loaded_at`, so `striatum list workflows` failed live with
`column "snapshot_sha256" does not exist (SQLSTATE 42703)`. It now projects the
real columns under the existing output aliases (`content_sha256 AS
snapshot_sha256`, `loaded_at AS captured_at`) and orders by `loaded_at`. A
hermetic schema-column regression test (`TestHandleListWorkflowsUsesCurrentSchemaColumnNames`)
locks the projection, mirroring the artifacts one.

### #133 — `register-session --replace` retries on a recovery deadlock

`register-session --replace` could abort with a raw 40P01 deadlock while
replacing a stale reviewer during autonomous recovery, leaving queued packets
unclaimed until an operator retried by hand. `HandleRegisterSession` now runs
under `withTxRetryOnDeadlock` — the same bounded tolerate-pattern RFC 0101 Phase
0a applied to the claim/review/interrogation handlers (the register-session
handler was missed). The transaction body fully rolls back on abort, so
re-running is safe.

### #127 / #132 / #140 — review verdict semantics: idempotent complete, synonym vocabulary, recoverable reject

Three related review-path fixes:

- **#127** — after `review.submit`/`verdict` completes a review job (releasing the
  lease), a lane that then follows the generic packet instruction and calls
  `work.complete` no longer hits a misleading `lease is not active`. A completed
  verdict-capable job returns an idempotent `already_completed` no-op.
- **#132** — `review.submit`/`verdict` now normalizes reviewer-natural verdict
  synonyms onto the canonical vocabulary `{accept, accept_with_findings,
  needs_revision, reject}` (e.g. `accepted_with_follow_up` → `accept_with_findings`,
  `approve` → `accept`, `request_changes`/`changes_requested` → `needs_revision`,
  `rejected` → `reject`). Unknown tokens still surface the "unknown verdict" error.
- **#140** — a `reject` verdict on a review job whose workflow declares a bounded
  revision cycle is refused with a clear correction (submit `needs_revision`, or
  use `override-verdict` to force terminal rejection) instead of failing the run
  with no recovery path. Workflows with no revision cycle keep the historical
  terminal-reject behavior. See `docs/decisions/decision-log.md`.

### #101 — suppress Claude Code welcome/update screen in supervised lanes (self-hosting unblocker)

A supervised `claude` lane spawned by the daemon otherwise parks on the Claude
Code auto-updater "a new version is available" nag / welcome splash and never
acts on its work packet — the single most common reason a live dogfood wedges
(the implement-lane stall behind #121, RFC 0097 self-hosting milestone).

The supervised lane env now carries, scoped to the `claude` adapter, the
authoritative suppression switches (Claude Code docs
`code.claude.com/docs/en/env-vars`; confirmed present in the installed
claude 2.1.159 binary):

- `DISABLE_AUTOUPDATER=1` — disables the auto-updater and its "update
  available" check; per the docs it takes precedence over the `autoUpdates`
  config.
- `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` — the bundle switch (equivalent
  to `DISABLE_AUTOUPDATER` + `DISABLE_FEEDBACK_COMMAND` +
  `DISABLE_ERROR_REPORTING` + `DISABLE_TELEMETRY`), set alongside the explicit
  `DISABLE_AUTOUPDATER` so the nag stays suppressed even if a future build
  narrows the bundle.

Injected in `mutations.supervisedEnvEntries` via a new per-adapter
`supervisedAdapterEnvEntries` helper, keyed on the bare CLI adapter name
(`agentloop.LaneAdapterName` of the raw lane argv0), so a real `supervise start`
claude lane inherits the keys regardless of agent-loop wrapping. This is the
claude sibling of the agy `usageStatisticsEnabled:false` survey-suppression
(#76) and the #87 DSN/secret env-drop. The C2 lane-env conformance golden and
the `mutations` env tests now assert the claude lane env carries both keys; a
revert that drops them fails the golden with `AdapterContractViolation`
(mirroring the C0 #101 argv-bootstrap regression gate). The keys do NOT reach
codex/agy lanes. The operator's `~/.claude.json` is not touched, so the
first-run onboarding/theme splash (gated on the `hasCompletedOnboarding` config
flag, not an env var) remains the operator's responsibility for a
never-onboarded profile; live efficacy on a real claude 2.1.159 lane is the
remaining live-verification step.

## v2.9.1 — 2026-06-01

Security: RFC 0096 V2 first slice — per-session capability-token binding (#135),
deployed at schema 22. Closes the cross-session impersonation vector for any
session-bound caller and records honest operator-override provenance for the
shared operator token. Remaining V2 (lane-env wiring so live lanes use their
session-bound token, #70 token-out-of-work-tree, #87 PG-less lane sandbox) is
live-lane-verification-gated and tracked as follow-up.

### RFC 0096 V2 / #135 — per-session capability-token binding

Closes the cross-session impersonation vector: any holder of the repo-scoped
capability token (the operator CLI, or a compromised lane) could `interrogation
answer --session-id <other_session>` and have the turn falsely attributed to the
target lane's PTY. Capability tokens can now be BOUND to a single session, and
the session-scoped interrogation handlers enforce that binding.

- **Auth model.** Added an optional `SessionID` to `rpc.CapabilityGrant` /
  `rpc.AuthContext` and to the `client_capabilities` table (schema 22,
  `0022_session_bound_capability.sql`, additive nullable column; NULL =
  session-unbound). Both `MemoryAuthorizer` and the production
  `PostgresAuthorizer` surface the grant's `session_id` on the resolved
  `AuthContext`; `AuthContext.IsSessionBound()` reports whether a token is bound.
- **AuthContext threaded to handlers.** `rpc.Server.handle` now wraps the request
  context with the resolved `AuthContext` (`rpc.WithAuthContext`) immediately
  after authorization succeeds; handlers read it via `rpc.AuthFromContext(ctx)` —
  no per-handler signature change. When absent (direct unit tests), the caller is
  treated as session-UNBOUND, never a bound-impersonation bypass.
- **Enforcement (the close).** `interrogation.open`/`ask`/`answer`/`close`
  enforce per-session binding via `enforceSessionBinding`: a session-bound token
  may act ONLY as its own session, else `capability_denied` ("token is bound to
  session X; cannot act as session Y"). An unbound operator/coordinator token is
  still allowed (recovery/operator convenience) but `interrogation.answer`
  records HONEST provenance — the turn payload and the `interrogation.answered`
  event carry `responder=operator` / `operator_override=true` (vs
  `responder=target_session` for a bound lane answering for itself). The
  `interrogation.show` turn projection surfaces `responder` so the distinction is
  visible in the turn record.
- **Per-session token minting.** `session.register` now mints a capability token
  bound to the new session (claim+write, repo-scoped, bounded TTL) and returns it
  as `session_capability_token`. The production authorizer resolves it as
  session-bound. Wiring the supervised-lane env to actually USE this per-session
  token (so live lanes become bound and the enforcement fully bites end-to-end)
  is the remaining follow-up — until then live lanes still carry the shared
  repo-scoped token, which the enforcement treats as an honest operator override.
- **Backward compatibility.** `AllowAllAuthorizer` (tests/dev) yields an unbound
  `AuthContext`, so existing interrogation/work/claim tests keep passing under
  operator-override semantics. No bound check can be silently satisfied by an
  unbound token.
- **Tests.** `TestInterrogationAnswerRejectsCrossSessionBoundToken` (the vector,
  closed → `capability_denied`, nothing recorded),
  `TestInterrogationAnswerBoundTokenSucceedsForOwnSession`,
  `TestInterrogationAnswerOperatorTokenRecordsOverrideProvenance` (turn + event
  record `responder=operator`), `TestPostgresAuthorizerSurfacesSessionBinding`,
  and `TestRegisterSessionMintsSessionBoundToken`.

## v2.9.0 — 2026-06-01

RFC 0101 (robust autonomous workflow execution) Phases 1–5 complete + deployed
(schema 21): honest liveness, adapter conformance, the in-daemon autonomous
recovery supervisor (same-attempt requeue, per-job budgets, stalled/dead-lane
recovery), loud structured escalation (`needs_operator` run state), and the
fault-injection chaos-suite regression gate — plus the 40P01 interrogation
deadlock root-fix and the post-2.8.0 runner/DX batch.

### RFC 0101 Phase 5 — fault-injection chaos suite

The end-to-end behavioral gate for the recovery→escalation arc. Phases 3/4 have
unit tests that hand-seed stuck DB state; Phase 5 drives the REAL fake-agent
lifecycle through the in-process daemon, INJECTS a lane failure, runs the
production recovery sweep, and asserts the run **self-recovers** (a fresh lane
completes it on the same attempt, no operator) OR **escalates loudly**
(`needs_operator` + a `recovery_exhausted` escalation, within budget) — never a
silent wedge. All fault injection lives in the test layer; no shipped daemon code
gained a fault hook.

- **Fault-injection seam** (`go/pkg/adapterconformance/chaos_test.go`, test layer
  only): `injectDeadLane` (close the session + release its lease → a hard dead
  pane), `injectStalledLane` (the core time-warp primitive: age every session
  `last_*` column + `registered_at` + the active lease's
  `acquired_at`/`expires_at`/`last_heartbeat_at` ~2h into the past so
  `sessionliveness.Classify` reports `stalled` on a still-active session, with NO
  sleeping — fully deterministic), `runSweep` (drives the production
  `mutations.SweepRun` and returns the `recovery_actions`/`escalations` summary),
  and `freshReplacementAgent` (registers a NEW session and runs the REAL
  `testagent` ModeHappy lifecycle through the production
  claim/ack/heartbeat/publish/complete handlers to finish the requeued packet).
  A repo-write fixture (`seedRepoWriteFixture`) seeds a `repo_write` `implement`
  job in a real git repo so the `work.complete` write-scope-clean gate passes for
  the replacement lane.
- **Chaos scenarios** (PG-gated; skip cleanly without `STRIATUM_PG_TEST_URL`):
  `TestChaosDeadRepoWriteLaneSelfRecovers` (acceptance #2 / the #121 case — dead
  repo-write lane → sweep requeues on the same attempt, no escalation → fresh
  agent completes → run completed, no operator),
  `TestChaosStalledLaneSelfRecovers` (stalled-but-active lane → transfer-requeue +
  stalled-owner close → fresh agent completes),
  `TestChaosUnrecoverableLaneEscalatesLoudly` (acceptance #4 — repeated dead lanes
  past `max_requeues` → `needs_operator` + one pending `recovery_exhausted`
  `escalation_inbox` row with a structured `striatum.recovery_escalation.v1`
  payload, within a bounded cycle cap that fails loudly on a silent wedge), and
  `TestChaosHonestLivenessDuringFault` (acceptance #3 — the classifier reports the
  TRUE state: a working lane is never `stalled`, a stalled lane reads `stalled`, a
  dead/closed pane never reads as a `working_*`/`quiet` state).
- **Recovery hardening surfaced by the chaos suite** (`requeueJobSameAttempt` in
  `go/pkg/mutations/recovery.go`): the live foreground claim path
  (`HandleClaimNext`) binds the work message to a lease but does NOT stamp
  `jobs.current_message_id`, so a genuinely live-claimed job that then died
  arrived at the autonomous requeue with `current_message_id` NULL while its work
  message was still non-terminal (`acked`). Minting a fresh pending message in
  that case tripped the `uq_active_work_message_per_job` partial unique index
  (a 23505 that wedged the sweep). `requeueJobSameAttempt` now resolves the job's
  still-live work message directly (keyed on the same `pending/claimed/acked` set
  the index covers) and REUSES it. The hand-seeded Phase 3 unit tests masked this
  by pre-setting `current_message_id`; the chaos suite drives the real claim path
  and caught it. No schema change.
- **Gating:** PG-gate only (no build tag), consistent with the existing Tier-A
  conformance tests — they share the same `NewHarness`/pgtest substrate and CI
  PostgreSQL tier. The hermetic `make -C go test` runs without
  `STRIATUM_PG_TEST_URL`, so the package's tests skip in ~0.004s and add ZERO
  cost. Red→green proof: stubbing `recoverStuckJobs`/`escalateExhaustedJobs` to
  no-ops makes the three self-recover/escalate scenarios FAIL ("job still stuck" /
  "run never escalated"), confirming they are genuine regression guards against a
  reintroduced silent wedge.

### RFC 0101 Phase 4 — loud structured escalation (needs_operator run state)

A run must never silently sit `running`. Phase 3 made the recovery sweep
autonomously reclaim genuinely-stuck jobs within per-job budgets and, when a
budget is exhausted, set `striatumd.job_recovery_state.escalation_pending=true` +
`escalated_at` and emit a `recovery.budget_exhausted` event — but nothing
consumed that flag. Phase 4 consumes it: it turns an unrecoverable stuck job into
a loud, structured, operator-actionable escalation (RFC 0062) and flips the run
to an explicit `needs_operator` state.

- **Migration `0021_run_needs_operator.sql`** (owner-applied at deploy; pgtest
  applies automatically): adds `'needs_operator'` to the `runs_state_check` CHECK
  (DROP+ADD, same pattern as `0007_decision_propagation.sql`) and
  `job_recovery_state.run_escalated_at timestamptz` (`ADD COLUMN IF NOT EXISTS` —
  0020 is left immutable). No new GRANT (no new table). `LatestDaemonDBVersion`
  20→21 in `go/pkg/db/migrations.go` with the version-21 label.
- **Run-health escalation pass** (`go/pkg/mutations/recovery_escalation.go`,
  `escalateExhaustedJobs`): called from `HandleRecoveryAuto` right AFTER
  `recoverStuckJobs`, inside the same `withTx`, skipped on `dry_run`. For each
  `job_recovery_state` row with `escalation_pending=true AND run_escalated_at IS
  NULL` it creates a `blockers` row (`state='open'`, `blocker_kind='recovery_exhausted'`,
  `severity='blocked'`, a description naming the workflow_job_id + stall_class) and
  an `escalation_inbox` row (`state='pending'`, SAME id) carrying a structured
  `payload_json` (`schema_version="striatum.recovery_escalation.v1"`,
  `stuck_job`/`job_id`/`stall_class`/`last_recovery_action`/`requeue_count`/
  `transfer_count`/`recovery_attempts` + a `suggested_operator_actions` list:
  re-prepare with corrected write_scope / transfer to a fresh session / cancel the
  run). It stamps `run_escalated_at=now()` (idempotency guard), appends a
  `run.escalated` event, and — once at least one escalation is raised and the run
  is still `running` — flips the run to `needs_operator` (guarded `WHERE
  state='running'`) and appends a `run.needs_operator` event. The daemon authors
  no markdown; the structured payload IS the escalation.
- **New blocker kind `recovery_exhausted`** added to `isEscalation`
  (`go/pkg/mutations/lifecycle.go`), `isEscalationClassBlocker`
  (`go/pkg/mutations/artifact.go`), and `escalationPredicate`
  (`go/pkg/reads/detail.go`) so it validates and is treated as an escalation
  everywhere (it already matched `validBlockerKind`'s `^[a-z0-9._-]{1,64}$`).
- **Transitions**: `running → needs_operator` (the sweep). Resolving the
  escalation via `HandleEscalationResolve` (`go/pkg/reads/escalation_resolve.go`)
  flips the run `needs_operator → running` (guarded `UPDATE ... WHERE
  state='needs_operator' RETURNING run_id`) and emits a `run.resumed` event, so
  the recovery sweep (`running`/`paused` filter) and new claims (claim gating on
  `run.state='running'`) resume. `run.cancel` already allows `needs_operator →
  canceled` (it only blocks `completed`/`failed`). The sweep's active-run query
  (`go/pkg/recovery/sweep.go`, `state IN ('running','paused')`) already excludes
  `needs_operator`, so an escalated run is not re-swept (confirmed; left unchanged).
- **Surface**: `go/pkg/reads/doctor.go` now reports a `needs_operator` count +
  `needs_operator_runs` list and adds each such run to `problems` (drives `ok`
  false) — a problem, not a warning. `run status` already returns `r.state`, so
  `needs_operator` + the open `recovery_exhausted` blocker surface there.
- No new RPC method (the change extends the existing sweep + `escalation.resolve`
  handlers), so the command-authority matrix is unchanged.
- Tests (`go/pkg/mutations/recovery_escalation_test.go`, PG-gated):
  `TestSweepEscalatesBudgetExhaustedJobToNeedsOperator` (a budget-exhausted job
  becomes a `recovery_exhausted` blocker + `pending` escalation with the structured
  payload, `run_escalated_at` stamped, run flipped to `needs_operator`);
  `TestNeedsOperatorRunIsNotSwept` (excluded from the active-run query + a re-sweep
  raises no duplicate escalation/blocker — idempotent via `run_escalated_at`);
  `TestEscalationResolveClearsNeedsOperator` (resolve flips `needs_operator →
  running`, marks both rows resolved, run is sweepable/claimable again). The
  `pkg/reads` escalation-resolve fake learned to return zero rows for the new
  `UPDATE striatumd.runs ... RETURNING` (the unit case is a still-running run).
  All existing Phase 3 sweep + recovery tests stay green.

### RFC 0101 Phase 3 Slice 2b — recover stalled-but-session-active lanes (#121 parked agent)

Closes a gap found reviewing Slice 2: the autonomous recovery decision tree
(`recoverStuckJobs`) could not recover a parked/stalled lane whose owning session
was still `state='active'` (the #121 welcome-screen case, the silent wedge
RFC 0101 must eliminate). `HandleRecoveryAuto` calls `expireLeases(...)` BEFORE
the tree runs, so by the time the tree saw the job the stalled lane's lease was
already `expired` ⇒ the old CASE 2 (`stalled && hasActiveLease && leaseExpired`)
could never fire; and because nobody closed the session (no operator), it was
`active` + `stalled` (not `dead`) ⇒ CASE 1 could not fire either. The job fell to
`default: continue`, was recovered by neither case, and was never escalated
(escalation only follows a real budget-exhausted action) — wedging forever.

- **Broadened CASE 2** (`go/pkg/mutations/recovery_decision_tree.go`): it now fires
  for an unfinished job whenever the owning session is present-and-`active` but
  honestly `stalled` (`!sessionDead && protocol == ProtocolStalled`), regardless of
  whether the lease is `expired`, `released`, or absent — the `hasActiveLease &&
  leaseExpired` precondition is dropped (`requeueJobSameAttempt` already
  force-expires any residual active lease). The Phase 1 honest-liveness contract
  guarantees `stalled` means NO protocol + NO PTY + NO tool-call progress past the
  deadline, so acting on it is safe. CASE 1 (dead/closed/absent → `requeue_count`)
  is unchanged and still takes precedence; CASE 2's `!sessionDead` guard makes the
  ordering unambiguous. The transfer keeps the `transfer_count` budget.
- **Closes the superseded stalled session** (`closeStalledOwningSession`): when
  CASE 2 transfers a job whose owning session is still `active`, the tree now also
  closes that session (`state='closed'`, `close_reason='recovery_stalled_transfer'`)
  and emits a `session.closed` event — mirroring the #121 manual flow (the operator
  did `session close`) so the parked lane cannot wake up to double-work or reclaim a
  job a fresh lane now owns. Guarded on still-`active` (idempotent); only the
  session that OWNS the recovered job is touched — interrogation-target sessions
  remain the panel-window logic's responsibility.
- **Preserved Slice 2 safety**: `working_protocol` / `working_local` / `working_tool`
  / `quiet` (pre-deadline) sessions are still NEVER requeued — the #80 protection
  holds even with an expired lease. Budgets, `escalation_pending`, convergence, and
  the `recovery_actions` summary are unchanged (the summary now also carries
  `stalled_owner_closed` / `stalled_owner_session` on a transfer action).
- No DB schema change in this slice.
- Tests (`go/pkg/mutations/recovery_decision_tree_test.go`, PG-gated):
  `TestSweepRecoversStalledSessionActiveLane` (the gap — failed `acted_count=0`
  before the fix; now requeues to `queued`+`pending` same attempt, `transfer_count`
  →1, owning session `closed` with `recovery_stalled_transfer`, fresh session
  claims it); `TestSweepDoesNotRequeueWorkingLocalWithExpiredLease` and
  `TestSweepDoesNotRequeueWorkingToolPreDeadline` (the #80 protection holds with an
  expired lease); every existing Slice 2 test stays green.

### RFC 0101 Phase 3 Slice 2 — autonomous recovery decision tree + per-job budgets

The crash-safe recovery sweep (`recovery.RunScheduler` →
`ActiveRunSweep.SweepOnce` → `mutations.SweepRun` → `HandleRecoveryAuto`, ~60s per
active run) gains an autonomous in-daemon recovery decision tree (OQ4 resolved
in-daemon, D094). It runs INSIDE `HandleRecoveryAuto`'s existing `withTx`, after
the auto-publish pass (so any recoverable job has already completed and is
excluded) and before `refreshRunLiveness` (which then classifies whatever the
tree leaves untouched). This builds directly on the Slice-1
`requeueJobSameAttempt` primitive — Slice 1 was the manual
`recovery.requeue_stale --force` entrypoint; Slice 2 is the autonomous one.

- **New decision tree `recoverStuckJobs`** (`go/pkg/mutations/recovery_decision_tree.go`)
  scans UNFINISHED jobs (`state IN ('claimed','running','stale_lease')` ) for the
  run, classifies each job's owning session via `sessionliveness.Classify`, and:
  - owning session `dead`/closed/absent AND lease released/expired/absent →
    `requeueJobSameAttempt` on the SAME attempt (`requeue_count` budget). repo-write
    jobs are reclaimed with `operatorOverride` — the bounded daemon loop IS the
    inspection D036 requires of an interactive operator, so autonomous recovery
    overrides the repo-write manual gate.
  - `stalled` past its deadline with a still-`active`-but-expired lease →
    force-expire (`release_reason='recovery_transfer'`) + `requeueJobSameAttempt`
    (`transfer_count` budget).
  - a leaked interrogation window (an interrogation-target session with no active
    lease / open interrogations / pending panel consumers) → closed via
    `maybeCloseInterrogationTarget` (no budget).
  - `working_*` / `quiet` (pre-deadline) sessions and live unexpired leases are
    left untouched — only genuinely stuck jobs are acted on.
  It is idempotent + convergent: a requeued job becomes `queued` and is no longer
  selected by the scan, and `requeueJobSameAttempt`'s `already_reclaimable` no-op
  path skips the budget increment, so the 60s re-run does not climb counters.
- **Per-job budgets** in new table `striatumd.job_recovery_state`
  (migration `0020_job_recovery_state.sql`): `requeue_count` / `transfer_count` /
  `respawn_count`, `last_recovery_action` / `last_recovery_at` / `last_stall_class`,
  and `escalation_pending` / `escalated_at`. Before acting the tree reads the row;
  if the relevant counter is at/over its limit it does NOT act — it sets
  `escalation_pending=true` + `escalated_at` (Phase 4 will consume this to flip the
  run to `needs_operator`; this slice only records it) and emits a
  `recovery.budget_exhausted` event. On a real action it increments the counter +
  stamps the action metadata. Defaults `max_requeues=2`, `max_transfers=3`, read
  from the workflow's optional top-level `recovery_policy` block (RFC 0020 §Step 2;
  this is its first Go consumer), honoring the documented `max_total_requeues_per_job`
  as the requeue fallback and extended with `max_requeues` / `max_transfers`.
- **`recovery_actions` surface**: `HandleRecoveryAuto` now reports a
  `recovery_actions` summary (`acted_count`, per-job `actions`,
  `escalation_pending_count`) alongside `published` / `skipped` / `liveness`.
- Migration registered by bumping `db.LatestDaemonDBVersion` 19→20 and adding the
  label in `go/pkg/db/migrations.go` (no SHA manifest to regenerate — SHAs are
  computed from the embedded FS at runtime). The operator applies the idempotent
  `CREATE TABLE IF NOT EXISTS` owner-side and bumps `substrate_version` to 20 at
  deploy; pgtest applies it automatically.
- Tests (`go/pkg/mutations/recovery_decision_tree_test.go`, PG-gated): a single
  `SweepRun` auto-requeues a dead-lane running-limbo job to `queued`+`pending` on
  the same attempt (`requeue_count`→1, fresh session claims it); the re-run is a
  convergent no-op; a `working_local` job is NOT requeued; budget exhaustion sets
  `escalation_pending` + stops requeuing; a leaked interrogation window is closed.

### RFC 0101 Phase 3 Slice 1 — same-attempt requeue for dead-lane repo-write jobs (#121)

Closed #121 ask #1: when a supervised implement lane dies (operator
`session close`, dead pane, missed heartbeat) its repo-write job was left in
"running-limbo" — `jobs.state` in `claimed`/`running`/`stale_lease`,
`current_lease_id` NULL, the lease already `released` (not `expired`), and zero
artifacts published. Nothing returned it to claimable on the SAME attempt: the
auto-publish recovery skips a zero-artifact job, `recovery.requeue_stale` only
matched a job JOINed to an `expired` lease (a `released` lease never qualified,
so even `--force` errored "job has no stale expired lease to requeue"), and
`reopenJobForAttempt` bumps the attempt and resets downstream (a content, not
operational, recovery). `run_9925b2502a256e077a24805c35004707` wedged here.

- **New internal primitive `requeueJobSameAttempt`** (`go/pkg/mutations/recovery.go`)
  returns a dead-lane unfinished job to `queued` WITHOUT bumping `attempt`/`max_attempts`
  and WITHOUT resetting downstream jobs. It force-expires any residual `active`
  lease (`release_reason='recovery_requeue'`), reuses the job's live work message
  (flipped back to `pending`, `current_lease_id` NULL) or mints a fresh pending
  one via `insertPendingMessageForJob` when the current message is NULL/terminal,
  and appends a `recovery.requeued_same_attempt` event carrying
  `{repo_write, operator_override?, justification?, author?}`. It is idempotent:
  an already-`queued`+`pending` job is a no-op success (`already_reclaimable`).
- **`recovery.requeue_stale` now reclaims the dead-lane running-limbo case.**
  When the expired-lease JOIN finds nothing and no `active` lease exists (the
  job is not held by a live claimant), the verb now finds the running-limbo job
  (`state IN ('claimed','running','stale_lease','queued')`) and routes it through
  the new primitive. The D036 inspection gate is preserved — a repo-write job
  still requires `--force --justification "<reason>"`, but `--force` now actually
  SUCCEEDS for it instead of erroring. The #82 live-claimant transfer guidance
  (active lease present) and the non-repo-write behavior / `next_actions` shape
  are unchanged; the verb still also emits the legacy `recovery.stale_requeued`
  audit event for backward compatibility.
- **`session close --requeue-job`** (param `requeue_job`): opt-in flag that returns
  the closing session's in-flight job to the queue on the same attempt (the
  active-lease guard already guarantees the lease is released, i.e. the dead-lane
  case) so a fresh lane can pick it up. Reports the requeued job under
  `requeued_job` in the result; absent without the flag.
- PG-gated regressions in `go/pkg/mutations/recovery_dead_lane_test.go`:
  `TestRequeueStaleForceReclaimsDeadLaneRepoWriteSameAttempt` (requeue +
  fresh `claim_next`, attempt unchanged, downstream untouched),
  `TestRequeueStaleDeadLaneRepoWriteRefusedWithoutForce` (D036 preserved),
  `TestRequeueStaleLiveClaimantGuidesToTransfer` (#82 guidance kept),
  `TestRequeueStaleDeadLaneIdempotent` (second call = `already_reclaimable`),
  `TestSessionCloseRequeueJobReturnsInflightJob`, and
  `TestSessionCloseWithoutRequeueLeavesJobRunning`. No schema change.
  Scope: the autonomous recovery-sweep integration + per-job budgets are Slice 2.

### RFC 0101 Phase 0a — interrogation deadlock root-fix

Root-fixed the Postgres deadlock (SQLSTATE `40P01`) the Phase-2 conformance
harness previously masked with an `InterrogationReady` handshake (#137/#133/
#134/#130/#131; #103 shape). The confirmed cycle is between an interrogation
target's `work.await_packet` claim transaction — which locks `striatumd.sessions`
(target) then `striatumd.runs` `FOR UPDATE` — and the target's `interrogation.answer`
transaction, which locks the same `sessions` row via `sessionliveness.Record`
while a sibling claim on the run holds `runs`: two transactions acquire
`{sessions, runs}` in opposite order and Postgres aborts one. The fix uses the
two existing patterns: (1) **tolerate** — all four interrogation handlers
(`interrogation.open/ask/answer/close`) now run under `withTxRetryOnDeadlock`
(the same wrapper the claim path uses) so a transient `40P01` is retried, not
surfaced; and (2) **root-fix** — a per-run transaction-scoped advisory lock
(`pg_advisory_xact_lock(hashtext("striatum:interrogation:"+repo+run))`) acquired
as the FIRST statement in each interrogation handler AND in `HandleClaimNext`
*only when the awaiting session is a live interrogation target*
(`sessionInterrogationTarget`). This gives both sides an identical, earlier
serialization point so the cycle cannot form, while ordinary parallel claims on
a run with no live interrogation never take the lock — preserving the #103
parallel-claim throughput on the hot `await_packet`/claim path (the narrowest key
that breaks the cycle). A new PG-gated regression
(`go/pkg/adapterconformance/interrogation_deadlock_test.go`,
`TestInterrogationAwaitDeadlock`) races `work.await_packet`/`work.claim_next`
against `interrogation.ask`+`interrogation.answer` on one run with no handshake,
50 iterations; it reproduces the `40P01` deterministically with the fix reverted
and passes clean with it. The `InterrogationReady` ("ask committed") workaround
is retired from the conformance harness — the C7 path now runs `interrogation.open`/
`ask` CONCURRENTLY with the agent's `work.await_packet` and stays green, proving
the fix end to end (a narrower `InterrogationSeeded` signal now serializes only
the raw fixture attestation INSERTs, a test-seeding artifact, not the product
race).

### RFC 0101 Phase 2 (Slice 2) — fake-agent lifecycle conformance runner

The chosen fake-agent fixture: `go/pkg/adapterconformance/` gains an in-process
daemon harness (`harness.go` — an isolated `pgtest` database + the **production**
RPC stack `mutations`/`reads`/`repositories` wrapped in `mcp.NewHTTPHandler` on
an `httptest` loopback with an ephemeral capability token; never the live daemon
or PostgreSQL), a configurable fake MCP agent (`testagent/agent.go`) that speaks
JSON-RPC `tools/call`, a read-only `DaemonObserver` (`observer.go`), a fixture
seeder (`fixture.go`), and a `RunLive` runner (`runner.go`) that turns clauses
**C3–C10 + C7** from `Deferred` into live asserts. The happy path passes all
in-scope clauses; the broken modes **arm the taxonomy** — each yields exactly its
contract `FailureClass`: `never_tools_list`→`BootstrapStall` (C3),
`await_never`→`AwaitPacketStall` (C4), `ack_never`→`AckStall` (C5),
`no_heartbeat`→`HeartbeatMissed` (C6), `ignore_interrogation`→`InterrogationIgnored`
(C7), `exit_before_complete`→`CompleteMissing`/`NoWorkLoopMissed` (C9/C10). Tests
are PG-gated (skip cleanly without `STRIATUM_PG_TEST_URL`, run in CI); confirmed
green against an ephemeral DB (~13s). The harness also surfaced a real
`40P01`/#103-class deadlock between `interrogation.answer` polling and
`interrogation.ask` (accommodated test-side via an `InterrogationReady`
handshake; the daemon-side concurrency wart belongs to the #137/#133 cluster).
C1 (real-CLI pre-flight), C11 (PTY launch via `supervise.start`), C12 (work-tree
scan), the skip-ledger, and the `striatum-adapter-conformance` driver binary +
Make/CI Tier-B wiring remain deferred.

### RFC 0101 Phase 2 (Slice 1) — hermetic adapter-conformance core (Tier A)

New package `go/pkg/adapterconformance/` implementing the accepted RFC 0101
Layer-2 design (`dogfoods/rfc-0101-l2-conformance/artifacts/DESIGN_SYNTHESIS.md`)
as a hermetic suite that rides `make -C go check` (no PostgreSQL, daemon, or
agent CLI): the closed `FailureClass` taxonomy partitioned into **contract** vs
**infra** classes (with a self-validation test that the partition is total and
disjoint), the ordered C0–C12 `Clause` list with `ContractProfile`
(`Full`/`SingleShot`) and `ClauseResult.Status ∈ {Pass, ContractFail,
InfraOutcome, Deferred}`, and two real hermetic asserts:
- **C0 — AdapterContract golden (the #101 regression gate):** asserts
  claude/codex/agy resolve to `argv` bootstrap delivery (a revert to `pty_submit`
  re-introduces the #101 TUI-buffering stall and fails C0 with
  `AdapterContractViolation`), agy appends `--prompt-interactive`, codex/claude
  use a trailing positional, codex injects `-c mcp_servers.striatum.url=…`, and
  the agy settings body carries `usageStatisticsEnabled:false` (#76). Verified to
  fail loudly on a `pty_submit` revert.
- **C2 — lane-env hardening golden:** the production supervised-lane env carries
  the required keys and drops every banned `DATABASE_URL`/`PG*`/`*POSTGRES*`/
  `*DSN*`/secret (`EnvSecretLeak` otherwise).

C1 and C3–C12 are declared with their metadata but return `Deferred` (never a
faked pass) — their live asserts + the `testagent/` fake-agent runner +
in-process-daemon harness land in Slice 2. Tight, behavior-neutral exported
accessors added to `agentloop` (`BootstrapDeliveryModeFor`,
`AdapterBootstrapContract`, `CodexMCPURLOverrideArg`, `GeminiSettingsBody`) and
`mutations` (`SupervisedLaneEnv`) so the golden asserts the real construction
without duplicating it.

### #116 / #124 — honest operator signals (no raw 23505; no spurious auto-finalize rec)

**#116**: `supervise start` no longer leaks a raw Postgres `23505` unique-constraint
error when a stale supervisor row exists for the session. Inside the
advisory-locked transaction it `SELECT … FOR UPDATE`s any
`starting`/`attached`/`detached` supervisor; with `--replace` it supersedes the
stale one (marks it lost via `markSupervisorLostInTx`) so the INSERT succeeds,
and without `--replace` it returns a clean `invalid_transition` naming the stale
supervisor id and pointing to `--replace`. The INSERT is also wrapped to convert
any residual `23505` (the narrow post-SELECT race) into the same actionable
error. `--replace` added to the `supervise start` usage. **#124**: `striatum
status` only recommends `recovery_auto_finalize` when the auto-finalize dry-run
reports `eligible_count > 0` (artifacts actually on disk and ready), not on
`candidate_count` alone (running jobs that merely *might* be finalizable) — the
cheap status path no longer emits a misleading next-action when nothing has
landed. Tests in `supervision_control_test.go` + `status_test.go`.

### #113 — bundled example workflows converted off retired one-shot lane commands

Six bundled examples drove their lanes with retired one-shot commands
(`codex exec` via `sh -c`, `claude --print`) that print once and exit without
ever calling `work.await_packet`, so the lane never claimed work. Each such lane
is converted to the proven agent-loop shape: a bare interactive CLI
(`codex --yolo` / `claude --dangerously-skip-permissions`),
`adapter_capabilities.agent_loop: true`, and
`supervision: {transport: pty_helper, require_tmux: true}` — the shape the daemon
wraps in the agent-loop executor and bootstraps over the PTY. Converted:
`code-change-flow`, `support-ledger-flow`, `failed-review-revision-cycle`,
`rfc-ledger-cleanup`, `three-lane-design-build-review`,
`iterated-interrogating-panel` (already-agent-loop `agy` lanes left untouched).
`harness-profiles` was a grep false positive (doc string, not a lane command);
`rfc-0014-operational-artifact-home` is a historical fixture and left alone. The
generator/catalog were confirmed not to emit retired commands. All six now pass
`striatum workflow validate` (exit 0), including the new #119 agent-loop-capability
check; two orthogonal pre-existing authoring gaps were also closed to make them
runnable (same-model review-pairing acceptance; `document_only` reviewer
`inputs`).

### #119 — `workflow validate` catches agent-loop capability + artifact-kind misconfig

Two misconfigurations that previously slipped past `validate` and only failed at
run/publish time (or stalled silently) are now caught at author time (exit 8):
a lane whose `supervision` sets `transport: pty_helper` or `require_tmux: true`
but does **not** declare `adapter_capabilities.agent_loop: true` is rejected (the
daemon's `laneUsesAgentLoop` would be false, the PTY bootstrap prompt never
delivered, and the lane would stall without claiming — the #113 shape); and an
`expected_artifacts[].kind` that is absent or not a known artifact kind is
rejected with a field-pathed error naming the full valid-kind set. Helper
`sortedKindList`; 5 new tests in `workflow_test.go`.

### RFC 0101 Phase 1 (slice 2) — honest classifier states + in-tool/dead signals (#83/#117)

`sessionliveness.Classify` no longer collapses every live lane to a generic
`live`. After the (unchanged) stall rungs it derives a precise, **projection-only**
`Protocol` state — `working_protocol` (fresh MCP), `working_local` (PTY output
between MCP calls, #80), `working_tool` (inside an MCP/tool call, with visible
`tool_call_since`/`tool_call_deadline`, #83), `quiet`, or `dead`. These states
live on `Result.Protocol` only and are **never** persisted to
`liveness_stall_class`, so the migration-0012 CHECK constraint and the recovery
library are untouched. **#117**: a lane past the discovery deadline with 0 MCP
calls and no PTY output now reports `Protocol: dead` (honest "dead at spawn")
while keeping `StallClass: agent_mcp_discovery_stall` for the recovery sweep; a
lane still producing PTY output keeps the plain discovery stall. **#83**:
`mcp/tools.go` stamps `last_tool_call_started_at`/`last_tool_call_finished_at`
around each tool call (volume/timing only). **#80 producer**: the daemon stamps
`last_pty_activity_at` on every meaningful PTY-progress event (lease-independent),
which both feeds `working_local` and keeps an honestly-working local lane out of
`agent_protocol_idle_stall`. New columns threaded through `lanehealth.Check` +
the four `reads/*` SELECTs. **Migration 0019** (owner-applied, schema 19) adds
the three `timestamptz` columns to `striatumd.sessions`. Restart-gated; the
migration must be owner-applied before deploying the schema-19 binary. First-cut
windows (`ProtocolFreshSeconds`/`PTYFreshSeconds`=60, `ToolCallSeconds`=180) per
OQ1 — tune against the installed CLIs. New tests in `liveness_test.go`,
`mcp/http_test.go`, `migrations_test.go`.

### #129 — remove `.claude/scheduled_tasks.lock` on lane teardown

A supervised Claude lane writes `.claude/scheduled_tasks.lock` into the target
work tree; teardown left it behind, dirtying the tree. New
`agentloop.CleanupClaudeScheduledTasksLock` removes ONLY that one lock file
(never broader `.claude/` contents, which may be operator config), wired at the
terminal-state choke point `cleanupSupervisorLaneMCPConfig` (mutations + reads
copies) plus the `supervise.stop`, session-close, and recovery teardown paths —
path-independent and idempotent, mirroring the #62 `.gemini/settings.json`
cleanup. Tests `TestCleanupClaudeScheduledTasksLock` +
`TestUpdateSupervisorStateCleansLaneMCPConfigOnEveryTeardown`.

### RFC 0101 Phase 1 (slice 1) — PTY-output lease auto-heartbeat (#80/#136)

A self-driving lane doing long LOCAL work (reading/editing files between MCP
calls) emitted no protocol heartbeat, so the protocol-only classifier tripped
`agent_lease_heartbeat_stall` on a lane that was demonstrably alive and
producing output. The supervisor-helper now meters PTY **output volume** (D028 —
volume/timing only, never content) and tags a progress event `meaningful` once
output crosses an OQ1 threshold (≥512 bytes AND ≥20s since the last fire, both
env-overridable via `STRIATUM_HELPER_MEANINGFUL_BYTES` /
`STRIATUM_HELPER_HEARTBEAT_INTERVAL`); a steady spinner/redraw never crosses it.
On a meaningful progress event the **daemon** (sole lease authority, D094)
refreshes the session's active lease the way `work.heartbeat` does
(`last_heartbeat_at` + extended `expires_at` + `last_work_heartbeat_at` via
`sessionliveness.Record`) and appends a `lease.heartbeat` event tagged
`source: supervisor_pty_progress`; no active lease is a safe no-op. Migration-free
(no classifier/schema change — reuses the existing `last_work_heartbeat_at`
rung). New `progressMeter` (`go/pkg/supervisor/progress_meter.go`); tests in
`progress_meter_test.go` + `supervision_test.go`. Restart-gated: takes effect
after the new `striatumd` + `striatum-supervisor-helper` binaries are deployed.
Slice 2 (distinct pty-activity / in-tool classifier states for #83/#117) needs a
migration + an MCP-server tool-call seam and is deferred.

### #123 — auto branch confirmation verifies the branch before recording it confirmed

`run.prepare` with `branch.mode: auto` could mark the working branch `confirmed`
(run → `ready`) without the branch actually existing or being checked out, so a
later `commit-apply` / `worktree.create` silently operated on the wrong branch.
`runPrepare` now checks the current branch and, when it differs from the
suggested branch, checks out an existing branch or creates it (matching
`branch.confirm --create` semantics) before recording confirmation; a failed git
operation surfaces a `workflow_error` instead of a ghost confirmation, and the
`run.branch_confirmed` event records the real `created` flag. Helper
`gitBranchExists`; tests `TestGitBranchExists`, `TestAutoConfirmBranchCreatesOrChecksOut`.

### #122 — top-level `--help` lists the local workflow authoring subcommands

`striatum --help` advertised `workflow accept-risk | accepted-risks` (the
daemon-routed verbs from `routes.All()`) but omitted the local authoring
subcommands `validate` / `generate` / `templates`, which `runWorkflow()`
dispatches before the daemon route. `usage()` now adds them so the full workflow
surface is discoverable. Test extends `TestTopLevelHelpAndUnknownCommand`.

### #114 — `workflow validate` rejects duplicate top-level keys

A `workflow.json` with two top-level `"lanes"` keys was silently accepted
(`encoding/json` takes last-wins). `Load` now scans the JSON token stream via
`detectDuplicateTopLevelKey` before decoding and fails with an error naming the
duplicated key. Helper `skipValue` keeps the decoder in sync across nested
values. Tests `TestLoadFileRejectsDuplicateTopLevelKey`,
`TestLoadFileAcceptsWorkflowWithNoDuplicateKeys`.

### #111 — catalog/generator shape reconciliation + `workflow --help`

The template catalog advertised `iterated_interrogating_panel` as a shape, but
`workflow generate --shape iterated_interrogating_panel` rejected it (it is an
example fixture, not one of the generator's 13 shapes), with an opaque "must be
one of" error.

- The catalog entry is now marked `generatable: false`; `workflow templates list`
  flags example-only shapes ("not a `workflow generate --shape` value; copy the
  example workflow at `<path>`").
- `workflow generate` with an example-only shape returns a clear, example-pointing
  error instead of the generic list (`exampleOnlyShapeHint`).
- A reconcile test (`TestCatalogAndGeneratorShapesAgree`) fails if the catalog and
  the generator ever drift — every generatable catalog shape must be a generator
  shape and vice versa. The generator exports `SupportedShapes()`/`IsSupportedShape`.
- `striatum workflow --help` and `workflow {generate,validate,templates} --help`
  now print usage instead of "unknown command/flag" (the #104 help fix reaches the
  local workflow subcommands).

### #110 — enum front-matter rejections name the accepted values

A `commit_request` with an invalid `confirmation_status` was rejected with a bare
"field is invalid". Enum front-matter fields now name their accepted values in
the error via a small `enumFieldValues` registry (generalizing the
`collaboration_ledger.verdict` case): `commit_request`/`pr_request`
`confirmation_status`, `decision.outcome`, `finding.verdict_intent`,
`harness_improvement_proposal.target`, and the ledger verdict all report e.g.
"allowed values are pending, operator_confirmed, human_confirmed, refused". Test
`TestCommitRequestInvalidConfirmationStatusNamesEnum`.

### #109 — write_scope violation names the path and explains the fix

The `work.complete` write-scope-drift error was generic ("changed paths outside
allowed_paths or inside forbidden_paths"). It now names the offending path(s)
inline and states the two valid resolutions — revert them, or (if a legitimate
index/status file such as `docs/rfcs/README.md` a cleanup step must update)
**widen the job's `write_scope.allowed_paths` in the workflow**, since a job
cannot extend its own scope at `work.complete`. Striatum can't auto-scope a
custom job's index files, but the failure is now self-explaining instead of
leaving the operator to leave the index stale / fail completion / force a
revision. Helper `writeScopeViolationMessage`; test
`TestWriteScopeViolationMessageGuidesIndexFileScope`.

### #85 (partial) — bootstrap prompt steers lanes off background MCP-discovery probes

A fresh agy lane tripped `agent_mcp_discovery_stall` because it spawned a
background task to probe the MCP endpoint and waited on it past the 60s discovery
deadline before claiming any work. The bootstrap prompt now tells lanes not to
spawn a background probe/curl/"discovery" of the MCP endpoint (it is already
configured and reachable) and to call `tools/list` then `work.await_packet`
directly in the foreground. Best-effort behavior steering (like #76); the
deterministic fix (a discovery-stall recovery / the agy turn-driver, #95) remains
open.

### #80 (partial) — bootstrap prompt tells lanes to heartbeat during long local work

A self-driving lane doing long local artifact repair (reading source, editing,
re-publishing) between MCP calls was classified `stalled`
(`agent_lease_heartbeat_stall`) even though it was actively working — the
lease-heartbeat is the authoritative liveness signal, but the agent was never
told to keep it warm during local work. The bootstrap prompt now instructs lanes
to call `work.heartbeat` periodically (honoring the packet's
`lease.heartbeat_after_seconds`) when a single packet's local work runs longer
than a few minutes between MCP calls — the issue's accepted alternative ("provide
an explicit heartbeat cadence/API for long local work"). The supervisor-helper
auto-heartbeat and the status-honesty half (distinguish protocol silence during
visible child activity from a true stall) remain open.

### #108 — `release --transfer`: operator transfer of a repo-write job without an attempt bump

A plain `work.release` of a repo-write job moves it to `blocked` (it can't
`--requeue`), which reads like a failure — so an operator transferring a job away
from a slow lane reached for `run.retry_job`, bumping the attempt and polluting
run history. New `release --transfer` performs an operator-inspected transfer:
the repo-write job returns to the queue with the **same attempt** (no retry bump)
so a fresh session can claim it. A plain repo-write release-to-blocked now also
carries a `note` + `next_actions` pointing at `--transfer` (or `recovery
requeue-stale --force`) instead of `retry-job`. Completes the #82 transfer story
from the `release` side. Tests `TestReleaseTransferRepoWritePreservesAttempt`,
`TestReleaseRepoWriteBlockedGuidesToTransfer`.

### #107 — `no_work` explains a fresh-session ineligibility instead of polling

A spent session that finished one job kept getting bare `no_work` from
`work.await_packet` while a same-role job sat `queued` — because that job is
`fresh_session_required` and the spent session can never claim it, so the lane
polled to the deadline and the coordinator had to infer the mismatch.
`claim_next` now distinguishes "no work" from "work exists but this session is
ineligible": it returns `ineligible_reason: fresh_session_required` plus the
queued `workflow_job_id` and a hint to register a fresh session, and
`await_packet` surfaces that and **stops polling** immediately (the session won't
become eligible). Test `TestClaimNextExplainsFreshSessionIneligibility`.

### #104 — `striatum help` lists the command surface

The top-level help printed only the one-line synopsis, so a self-driving lane (or
operator) trying to discover the control surface fell back to raw MCP
`tools/list` over curl. `striatum help` / `--help` (and the no-command case) now
enumerate every command — daemon-routed verbs (with their subcommands) plus the
local commands — grouped and sorted, and name the **work-packet loop** a lane
runs (`claim-next → ack → publish-artifact / submit-review → complete`, or the
equivalent MCP tools such as `work.await_packet`, which has no CLI verb), with a
pointer to `striatum <command> [subcommand] --help` for flags. Generated from the
route table so it stays complete. Test extends
`TestTopLevelHelpAndUnknownCommand`.

### #105 — work packets resolve workflow-relative paths from the repo root

A self-driving lane runs from the repo root, but workflow-declared paths (role
definition, prompts, context docs) are relative to the workflow *directory* — so
a packet's `role.definition_path: roles/final_reviewer.md` failed `wc
roles/final_reviewer.md` from the root (the file is at
`striatum/<wf>/roles/final_reviewer.md`). The work packet now:

- surfaces `run.workflow_root` — the explicit repo-root-relative workflow
  directory (from the snapshot `source_path`) as the base for any
  workflow-relative path; and
- resolves `role.definition_path` to a repo-root-relative path (keeping the
  original under `role.workflow_relative_path`), matching the #90 task-prompt
  treatment.

Helpers `workflowRootDir` / `resolveWorkflowRelativePath`; test
`TestResolveWorkflowRelativePathAndRoot`.

### #106 — byline rejection names the file and the exact replacement

The `artifact.publish` author-line rejection (enriched in #73 to name the expected
byline) now also names the **target file** and frames the fix as a concrete
one-edit repair — "author line in `<path>` is `X` but the work packet requires
`Y`; set the title-block author line to exactly `Y` and re-publish" — and points
at the packet's `expected_artifacts[].author_line` where the required byline also
lives. An agent can repair in one edit instead of a failed-publish round-trip.
Test: `TestPublishArtifactBylineMismatchNamesBothLines`.

### #76 — disable the agy usage/feedback survey in the supervised lane

A supervised agy lane could stall on the gemini-cli usage survey ("How's the CLI
experience? [1] Good …") inside the PTY while a work packet was active, until an
operator typed `0` to skip. The ephemeral `.gemini/settings.json` Striatum writes
for the lane now sets `usageStatisticsEnabled: false` (the documented gemini-cli
key that disables usage statistics and the periodic feedback prompt), unless the
project already declares it. Additive and harmless if a given agy build ignores
the key; needs live confirmation against an agy lane. Test extends
`TestInjectLaneMCPConfigAgyPreservesExistingGeminiSettings`.

### #101 — claude_code agent-loop bootstrap is submitted (argv, not TUI-type-then-CR)

Claude Code v2.1.x buffers a typed multi-line bootstrap prompt in its TUI input
editor and a trailing carriage return no longer submits it (a manual `tmux
send-keys Enter` did not either), so `claude_code` self-driving lanes sat idle at
the prompt — never calling `tools/list` / `work.await_packet` — while
`supervise.list` read `attached` / `healthy`. `bootstrapDeliveryModeFor` now puts
`claude` on the **argv** path alongside codex/agy: `claude [options] <prompt>`
takes the bootstrap as the initial positional prompt and starts an interactive
session by default (no `--print`), which Claude submits itself; the agent-loop
receive loop drives subsequent turns. Verified against the locally installed
claude v2.1.158 (`claude [options] [prompt]` — "starts an interactive session by
default"). Test renamed/flipped to
`TestPrepareLaneCommandForBootstrapUsesClaudeInitialPromptArg`. Restores
claude_code model diversity (the prior workaround was codex-only lanes).

### #100 — register the documented parallel same-(role,lane) fanout sessions

`session.register` unconditionally refused a second active session on a
`(run, role, lane)` slot (without `--replace`), even though the documented
disjoint-scope fanout shape needs a distinct session per parallel queued job —
the refusal text said "register a fresh session for a distinct queued job" with
no way to do it (a launcher had to inject the session directly). Registration now
**allows** a second (and Nth) session on the slot when the slot has genuinely
more live work messages (`pending`/`claimed`/`acked`) than active sessions; it
still **refuses** a duplicate when no additional parallel job remains (the
accidental double-register, #60) and still supersedes only under `--replace`.
Each parallel session gets a fresh ordinal. Test:
`TestRegisterSessionAllowsParallelWhenWorkRemains`.

### #102 — write-scope leniency for a live sibling's not-yet-published artifact

The sibling-published-artifact leniency (#93) was digest-based: it only ignored a
sibling lane's artifact once that artifact was already **published**. Same-stage
parallel proposal lanes in a shared worktree race — a sibling writes its declared
artifact but `work.complete` of another lane can run before the sibling publishes,
so the dirty sibling file tripped the write-scope guard (self-healing only on
retry). Now a touched out-of-scope path that is the **declared expected artifact
of a sibling job currently holding an active lease** (a live concurrent writer
with a disjoint scope) is also ignored, closing the pre-publish window. Scoped to
an active lease so it never masks a path no live sibling is working. Test:
`TestPublishedRunArtifactIgnoredPathsHonorsLiveSiblingExpectedArtifact`.

### #103 — bounded retry on a parallel-claim deadlock

`work.await_packet` → `work.claim_next` is the first durable receive-loop call in
the agent bootstrap. When sibling reviewers claim in parallel, Postgres can abort
one claim transition with a deadlock (SQLSTATE 40P01), which surfaced to the lane
as an internal control-plane error. `HandleClaimNext` now runs inside
`withTxRetryOnDeadlock` (the bounded retry introduced for `review.submit` in #98,
now generalized): the claim is idempotent and re-selects on retry, so a transient
deadlock is retried in-daemon instead of failing the await loop. The retry helper
is covered by `TestWithTxRetryOnDeadlock` / `TestIsDeadlockError`.

### #82 — transfer a live repo-write claim to a fresh session without bumping the attempt

`recovery.requeue_stale --force --justification "<reason>"` now also recovers a
job held by a **live** (active-lease) claimant, not just an expired/stale one.
Under `--force` it force-expires the job's active lease (`release_reason:
operator_transfer`), marks a claimed/running job `stale_lease`, and requeues the
**same** work message and attempt to a fresh session — a lease-ownership
correction that, unlike `run.retry_job`, does **not** increment the attempt
counter or reset downstream. Without `--force`, a job held by a live claimant now
returns guidance toward the transfer path instead of a bare "no stale lease"
error. Tests: `TestRequeueStaleForceTransfersRepoWriteWithoutBumpingAttempt`.

### #77 — adjudicator absorbs a reviewer's needs_revision (no spurious checkpoint)

In cross-examination / forum shapes a reviewer's `needs_revision` is dissent for
the adjudicator to weigh, not a trigger for the reviewer's own revision cycle.
Two coupled faults made a single dissenting cross-examiner stall the whole panel
and force an operator override:

- **run.prepare over-gated adjudicator inputs.** Every edge from a verdict-capable
  job (review / phase_synthesis) got `requires_verdict:[accept,
  accept_with_findings]` — *including* edges into an adjudicator. So the
  adjudicator's own gate required its cross-examiners to clear, defeating its
  purpose. Edges **into** a `phase_synthesis` adjudicator now stay ungated
  (`edgeRequiresClearingVerdict`); every other review→downstream edge is gated as
  before.
- **needs_revision always opened a checkpoint when no cycle matched.** A review
  whose downstream consumers all *absorb* its verdict (no `requires_verdict`, or
  one that includes it — i.e. an adjudicator) now completes and enqueues the
  adjudicator (`reviewFeedsAbsorbingAdjudicator`), instead of opening a
  `revision_routing` human checkpoint. A reviewer with no downstream, or any
  downstream that hard-gates on a clearing verdict, still checkpoints.

Applies at prepare time to generated and hand-authored workflows alike. Tests:
`TestNeedsRevisionFeedingAdjudicatorIsAbsorbed`,
`TestNeedsRevisionFeedingGatedSynthesisStillCheckpoints`,
`TestEdgeRequiresClearingVerdictExemptsAdjudicatorInbound`.

### #91 — scope-check reads the work packet (RFC 0099 Phase 1)

`striatum scope-check` (the read-only pre-`work.complete` write-scope drift
diagnostic) no longer requires pasting each `--allowed`/`--forbidden` path. It
now accepts `--packet-file <work-packet.json>` and reads
`write_scope.allowed_paths` / `write_scope.forbidden_paths` directly from the
active work packet (the same object the daemon enforces, found at the top level
or under a `data`/`packet`/`work_packet` wrapper). Explicit
`--allowed`/`--forbidden` still merge on top. The command stays **daemon-free**
(no endpoint or capability token), so it works as a local pre-completion check
even when the lane cannot reach the daemon. Tests in `cmd/striatum`.

### #67 — honest supervise rebridge delivery state

`supervise rebridge` rebuilds the delivery transport (fresh helper + persistent
FIFO), after which the re-attached tmux attach-*observer* re-emits a benign
`attach_client_exited` (#63 F7). The handler treated that benign event as a real
degradation and reported `delivery_state: degraded` immediately after a
successful rebridge — telling the operator to intervene on a healthy lane. Now
rebridge preserves the degraded state only when the helper reports a *real*
transport failure on relaunch (`helper_error` / `agent_exited`); the benign
attach-observer exit reports `healthy` and clears the noisy `delivery_liveness`
block, consistent with the lanehealth classifier. Tests
`TestSuperviseRebridgeClearsBenignAttachExitDelivery` and
`TestSuperviseRebridgePreservesRealDeliveryFailure`.

### RFC 0100 Phase 1 — self-describing artifact contracts (#74 / #79 / #88)

Artifact front-matter validation stops forcing lanes to reverse-engineer Go
source mid-run.

- **Standard optional-metadata allowlist (#74/#79).** Any artifact kind now
  tolerates a common set of byline/workflow metadata keys (`author`, `workflow`,
  `phase`, `lane`, `role`, `model`, `date`, `created_at`, `updated_at`,
  `visibility`, `title`, `status`, `tags`, `summary`, `related`, `run_id`,
  `session_id`, `ordinal`, `cycle`) free-form, so a lane keeps the natural front
  matter its template produces. A kind that gives one of these a required,
  checked meaning (e.g. `decision.title`) still enforces it.
- **Enriched validation errors (#74/#79).** A genuinely unknown field, a missing
  required field, or a failed value check now names the kind's required keys,
  optional keys, and the standard-metadata set, and points at
  `docs/reference/spec.md#artifact-front-matter-schemas`. The
  `collaboration_ledger` `entries` error describes the required
  `{ by, kind, refs: [dialogue:<seq>] }` shape instead of a bare "is invalid".
  The substance gate itself is unchanged (still strict by design).
- **Clearing-verdict wording (#88).** The generated `adjudicator` role and
  `adjudicate_collaboration` task prompt now name the `verdict` enum explicitly
  and state that a clearing verdict is `accept`/`accept_with_findings`, never
  `clear` (the value that failed publication in the Engram entity forum run).
- No schema change; spec updated. Tests in `pkg/artifactcontracts`.

### #65 P1 / RFC 0095 Phase 3 — panel-owned interrogation window

The interrogating panel's preserved-context window is now owned by the review
**panel/gate**, not by the first interrogation thread. Previously the first
reviewer's `interrogation.close` tore down the interrogable target session
(`close_reason: interrogation_window_closed`) whenever no interrogation was
*currently* open against it — a race that left reviewers 2..N with
`target_unavailable`, forcing them to vote without interrogating the live author
(exactly what an interrogating panel exists to prevent). This was the last
structural blocker for multi-reviewer interrogating panels (the RFC 0070 quiz
panel, the RFC 0094 build panel). Problem 2 (stale-lease auto-publish of the
unchanged artifact) was already fixed in Phase 2.

- **Panel-scoped window.** The direct downstream dependents of an interrogable
  job are exactly its review panel (the next phase depends on the reviewers, not
  on the interrogable job), so the window stays live while any reviewer dependent
  is still in a pre-verdict working state. `maybeCloseInterrogationTarget` no
  longer closes the target while a panel consumer is pending.
- **Authoritative closer.** The last reviewer's `interrogation.close` cannot close
  the target (its own job is still active at that moment), so the window is
  retired when the **final reviewer job terminates** — wired into the
  `review.submit`/override accept paths (`releaseInterrogationTargetForCompletedReview`).
- **Revision boundary.** Re-opening the interrogable job for a revision attempt
  now explicitly retires the superseded target session (and any interrogation
  still open against it) so the longer-lived window does not leak across cycles
  (`closeInterrogationTargetForReopen` in `reopenJobForAttempt`).
- Migration-free (Go logic only); regression tests
  `TestInterrogationPanelOwnedWindowSurvivesFirstClose` and
  `TestInterrogationWindowClosesOnRevisionReopen`. Non-panel ad-hoc
  interrogations are unchanged (no dependents ⇒ legacy single-thread close).

### #84 / RFC 0095 §1 — attempt-scoped artifacts (revision cycles republish same logical_name)

The load-bearing lifecycle fix. The append-only `artifacts` table keyed uniqueness
on `(run_id, job_id, logical_name)` (and `(run_id, repo_path, content_sha256)`)
with no attempt dimension, so a re-opened revision attempt republishing the same
**fixed** `logical_name` collided (`artifact logical name already exists with
different content`) — the wedge that forced `${cycle}`-templated names and
stranded fixed-name revision loops (and, per #87, drove a lane to reach for the
daemon DB).

- **Migration 0018 (owner-applied, RFC 0079 §5).** Adds `artifacts.attempt`
  (`NOT NULL DEFAULT 1`, metadata-only) and widens both unique keys with
  `attempt`. Because it ALTERs the owner-held `artifacts` table it is applied by
  the owner via `striatum daemon migrate-db --admin-url <owner-dsn>`, not the
  runtime role (which would crash-loop). `LatestDaemonDBVersion` 17 → 18.
- **Publish** records `jobs.attempt`; the collision and path+content idempotency
  checks are attempt-scoped — a fresh attempt republishes its own row, while
  same-attempt different-content still rejects and same-attempt same-content stays
  an idempotent no-op (#58 preserved).
- **Gates** (`verifyRequiredArtifacts`, recovery auto-finalize, surgical recovery)
  honor only `attempt == jobs.attempt`, so a `needs_revision` can't be cleared by
  a prior attempt's stale artifact. Across-cycle readers (collaboration_ledger,
  operator) are unchanged.
- Workflows no longer need `${cycle}` logical names to survive a revision cycle
  (the RFC 0098 generator shape keeps them for clarity, but a fixed name now
  republishes cleanly). Deployed to the live daemon (schema 18, healthy).

### RFC 0098 slices 2–3 — generator shape + discharge-verifying final review

Completes the RFC 0098 V1 target (slices 1–3).

- **Slice 2 (generator + fixture):** `adjudicated_constraint_extraction` is
  registered in the collaboration shape pack, so
  `workflow generate --shape adjudicated_constraint_extraction` emits a
  `striatum.workflow.v1.1` 8-phase graph (survey → convener_synthesis →
  cross_exam → adjudication → revision_synthesis → constraint_discharge_review →
  spec_publication → final_review), one `phase_synthesis` per phase, with
  `${cycle}`-templated logical names on every re-publishable revision-cycle
  artifact (so republish doesn't collide on the append-only table). Roles +
  posture-specific prompts; starter fixture
  `examples/adjudicated-constraint-extraction-flow/`. Passes `workflow validate`
  and the `run.prepare` phase rules.
- **Slice 3 (discharge gate):** `finding`/`findings_ledger` additively accept a
  `constraint_discharge[]` block (`discharged|missing|partial|accepted_risk`;
  `accepted_risk` requires owner+stage). `final_review` becomes a **typecheck**:
  `enforceConstraintDischarge` (in `publishArtifact`, covering both
  `artifact.publish` and `review.submit`, no new daemon method) loads the latest
  cleared ACE `collaboration_ledger.v1.1`'s binding+`final_review_required`
  constraints and **fails closed** (exit 6, naming the offending ids) unless every
  one is `discharged` or `accepted_risk` — without re-running prior phases.

### RFC 0078 Gate G — delete the retired `src/` tree

The deferred final cleanup from RFC 0078. Removed the `src/` tree (~71 tracked
files: the dead TS/React frontend, vite output, and legacy web/static assets)
together with its coupled build gates — the Makefile `ui-*` targets
(`ui-install`/`ui-build`/`ui-check-bundle`/…) and the `.github/workflows/ci.yml`
`frontend` job. The only shipped web surface is the Go server-rendered SSE UI
(`go/pkg/webassets`, RFC 0092); no Go code referenced `src/` except historical
provenance comments. Also dropped the now-dead `.gitignore` un-ignore rules for
the deleted bundle and added RFC-0078 historical banners to
`docs/explanation/harness-friction-patterns.md` and `docs/reference/prd.md`
(D018) where they cited retired `src/striatum/...py` paths. Go build + vet + lint
green; no CI step depends on the removed frontend.

### Backlog triage sweep — close/RFC/fix the open dogfood-friction issues

Triaged all 36 open GH issues (friction logs from three dogfood runs). Closed the
ones RFC 0095/0096 Phase 1+2 already resolved (#81/#75/#86/#78/#63), routed the
partials/design items to their owning RFCs (0091/0095/0096) with progress
comments, and filed two new RFCs for the design-needing clusters:
**RFC 0099** (constrained operator mode, #92) and **RFC 0100** (self-describing
artifact contracts, #74/#79/#96/#88).

Bounded runner/DX bugs fixed directly (bootstrapped via subagents + tests, since
the wedged runner can't dogfood its own fixes):

- **#66**: `workflow validate` shares `run.prepare`'s phase-shape rules via one
  source (`workflowauthoring.ValidatePhaseShapes`) — no more false-green-then-
  launch-failure. **#99**: non-JSON `workflow.json` reports a clear "not JSON"
  error. **#97**: a `document_only` reviewer with empty `inputs` is rejected.
- **#93**: the write-scope guard checks the sibling-published-artifact leniency
  **before** the `forbidden_paths` rejection, ending the multi-lane gate-job
  deadlock at `work.complete` in a shared worktree (the wedge that blocked the
  RFC 0098/0094 dogfoods). **#90**: `task_prompt.path` no longer double-joins the
  workflow dir. **#98**: `review.submit` retries the Postgres `40P01` deadlock
  (bounded). **#68**: `claim-next` suppresses the misleading `supervise_send` hint
  for self-driving lanes and emits a self-claim note.
- **#91**: new `striatum scope-check` read-only diagnostic reports write-scope
  drift before `work.complete` (RFC 0099 Phase-1 seed; auto-reading the active
  packet's scope still needs a daemon read method). **#64**: new `striatum codex`
  launcher injects the live MCP endpoint + token without printing it, plus a
  `doctor` stale-codex-config/absent-token warning. **#72**: `session close`
  usage lists `--reason` as required. **#69**: the checked-in binary is already
  untracked/gitignored (acute cause gone).
- **#96**: `submit-review` infers `logical_name`/`kind` from the sole required
  expected artifact when omitted, and the missing-artifact error names submitted
  vs expected tuples. **#73**: the artifact byline-mismatch error reports the
  expected canonical author line. **#94**: the logical-name-conflict error
  explains the safe path (true cross-cycle supersession is the #84 attempt-scoped
  work). **#71**: run status/detail surface a `branch_divergence` warning when
  `branch_name` differs from the actual checkout branch.

### RFC 0098 slice 1 — `collaboration_ledger.v1.1` + productive-refusal gate

From GitHub #89 (the Engram entity-relationship forum run). RFC 0098 promotes the
**adjudicated constraint-extraction loop** into a first-class successor to
RFC 0093: adjudicator refusal must compile objections into binding constraints
the next revision discharges. Slice 1 lands the contract surface, additively,
with no new daemon method:

- `striatum.collaboration_ledger.v1.1` extends `v1` additively in
  `go/pkg/artifactcontracts`: optional `constraints[]` (typed, sourced rows),
  `branches{}` posture-disposition map, `cycle`, and `findings[]`. Every valid
  `v1` ledger still validates.
- **Productive-refusal gate** in `validateCollaborationLedger` (the single
  function all three write paths funnel through): a
  `shape: adjudicated_constraint_extraction` ledger requires `v1.1`, and an
  `adjudicated_constraint_extraction` + `needs_revision` ledger requires a
  non-empty `constraints[]` (≥1 `binding: true` row or `kind: unresolved_question`).
  Rejected via the existing `artifact_error` (CLI exit 6) — no new error code.
- A binding constraint must resolve `source_finding` to a same-ledger
  `high`/`critical` `findings[]` row and carry non-empty `verification`.
- The front-matter `verdict` enum stays exactly
  `accept | accept_with_findings | needs_revision | reject`; the RFC 0098
  refinements `blocked_pending_answer` / `defer_with_successor` are `branches{}`
  dispositions only (widening the verdict enum would wedge `recordVerdict` with
  `invalid_transition` — caught by the design panel during the dogfood).
- Built by a single-implementer dogfood from a 3-lane interrogated design
  synthesis; verified with `make -C go check` (vet + race + lint + coverage) and
  the new contract/mutation tests green against live PostgreSQL. Slices 2–3
  (generator shape + fixture; discharge-verifying final review) and the
  first-class `constraint.*` objects are deferred. The 3-lane design panel's own
  build phase surfaced #93 (write-scope deadlock) and #95 (agy one-shot) and
  re-confirmed #84 (revision-republish collision) as live blockers.

### RFC 0095 / 0096 Phase 1 — revision-safe lifecycle + lane-sandbox local fixes

Bootstrapped via subagents (the runner-fixes can't be dogfooded through the
broken runner). No schema change.

- #57 (RFC 0095 §6): the write-scope guard now flags **only** what the current
  attempt did — a path it created outside `allowed_paths`, or a tracked file it
  mutated away from baseline. `dirty→clean` baseline transitions and untouched
  operator files are no longer false violations.
- #58 (RFC 0095 §7): `review.submit` / `publish-artifact` is idempotent — an
  already-published identical finding (same `repo_path` + `content_sha256`) is a
  no-op success that records the verdict, instead of a raw Postgres
  unique-constraint crash. Different content at the same path still errors cleanly.
- #59 (artifact contracts): the front-matter parser accepts standard multi-line
  YAML sequences (block-style lists) instead of rejecting them, and reports
  malformed front matter with line-numbered syntax errors. Covered by
  `TestParseFrontMatterAllowsMultilineLists` /
  `TestParseFrontMatterReturnsLineNumberedSyntaxErrors`.
- #81 (RFC 0095 §4): a non-active (`closed`/`superseded`/`expired`) session is
  refused `work.claim_next` / `work.await_packet` — a closed-but-alive session
  can no longer reclaim its revision-cycle job.
- #60/#75 (RFC 0095 §5): `register-session --replace`/`--force` atomically closes
  the prior `(run, lane)` session; without it the duplicate error names the exact
  session to close. Parallel same-`(role,lane)` jobs now hold distinct active
  sessions (no implicit supersede-on-register).
- #87/#70/#86 (RFC 0096 §2/§3): supervised lanes get a **minimal allowlist
  environment** (no daemon DSN/secret inheritance); the agy `.gemini/settings.json`
  bearer-token file is removed on every teardown path (graceful/stop/kill); the
  agent-loop bootstrap prompt forbids authoring control-plane helper scripts in
  the target repo.

### RFC 0095 Phase 2 core — migration-free revision re-open safety

No schema change (the clean `attempt`-column key is RFC 0095 §1 / Phase 2.5,
deferred as a daemon-owned migration). Logic-only; bootstrapped via subagents.

- #65 P3 (RFC 0095 §3): every re-open path — the revision-cycle router
  (`routeRevisionCycle`), `run.retry_job`, and `checkpoint.resolve continue` —
  now funnels through one atomic helper `reopenJobForAttempt`. In a single
  transaction it releases the prior active lease, cancels the prior in-flight
  work message (incl. a `blocked` message parked on a human checkpoint), cancels
  open blockers, re-blocks the target's transitive downstream terminal jobs +
  clears their stale verdicts (RFC 0083 review-after-revision), clears the job's
  stale verdicts, bumps `attempt`, and re-enqueues a fresh message. This closes
  the `duplicate active job lease` wedge (a re-open that never released the prior
  lease made the fresh `work.claim_next` fail `uq_active_resource_lease`). The
  retry-job and checkpoint paths previously skipped the lease release entirely.
- #65 P2 (RFC 0095 §2): auto-finalize recovery is now attempt-aware. A re-opened
  job is no longer auto-finalized from the **prior** attempt's unchanged
  artifact: when the only satisfying same-content artifact's `created_at`
  predates the current attempt's re-enqueue boundary (the current work message's
  `created_at`), recovery refuses (`stale_attempt`) and leaves the job in the
  fresh lane's hands. NULL boundary preserves legacy behavior; an equal-second
  tie favors the lane.
- #84 (partial, migration-free): RFC 0093 `${cycle}`-templated artifacts
  republish cleanly across attempts — because `reopenJobForAttempt` bumps
  `attempt` consistently and cycle resolution keys off `jobs.attempt`, a
  re-opened cycle artifact gets a distinct `logical_name`/`path`. A FIXED
  (non-`${cycle}`) `logical_name` still collides on the append-only artifacts
  table; the real fix (the `attempt`-column uniqueness widening) is deferred to
  Phase 2.5.

### GH #62 / #63 follow-ups (daemon + CLI fixes)

- #63 F2: `checkpoint.resolve` gains an `override` action for `revision_routing`
  checkpoints — it requires `--decision-id` referencing an accepting run-level
  `striatum.decision`, resolves the checkpoint, completes the stalled review
  (settled by the decision, not re-run), records a superseding clearing verdict
  (`accept_with_findings`, `posture=override`) under a minted operator-labeled
  reviewer session, and makes the downstream gate reachable. No new override
  authority, no new RPC method/route; audit lives in the decision (D157).
  Surfaced in `status` (`resolve_actions`/`resolve_action_hints`), the CLI
  `--action` enum, and the recover skill templates.
- #63 F10: the `supervise.send` delivery gate now keys purely on the live
  reconciled probe (`!health.Deliverable`) instead of also requiring stale
  supervisor `delivery_liveness` metadata — a helper/transport that died
  abruptly without writing a metadata record (main PID alive) no longer slips
  the gate and dispatches a packet to a dead FIFO. The benign F7
  `attach_client_exited` case stays deliverable; `supervisorDeliveryDegraded`
  survives only as the fallback reason source.
- #62: the ephemeral per-launch `.gemini/settings.json` (rotating MCP bearer
  token) is now removed/restored on every supervisor teardown path — graceful
  completion, `supervise stop`, and tmux kill/lost — via a central cleanup at
  the `updateSupervisorState` terminal transition, not just the agent-loop's
  own `cleanupMCP`.
- #63 F7: an exited `tmux attach-session` OBSERVER client no longer marks
  packet delivery degraded (or blocks `supervise.send`) when the pane is alive
  and the real transport is healthy; genuine transport failures
  (`helper_process_gone`, `stdin_reader_missing`) still reject.
- #63 F8: a lane that holds an active work lease and is heartbeating it is no
  longer falsely flagged `agent_protocol_idle_stall` during a long generation;
  the lease-heartbeat rung is authoritative for lease holders (a lease holder
  that stops heartbeating still trips `agent_lease_heartbeat_stall`).
- #63 F9: operator verbs now print real `--help`/usage (required + optional
  flags, incl. `--reason`, repeatable `--capability`, `--action`); added a
  `session register` alias alongside `register-session`.
- #63 F5: retired the dead `agy` one-shot pipe lane config (migrated live
  templates/examples to the working `agent_loop` shape) + a `workflow lint`
  warning for an `agy --print` lane; recorded D156.

## v2.8.0 — 2026-05-29

### RFC 0093: structured live-collaboration workflow shapes (substance-gated dialog)

- Adds the `striatum.collaboration_ledger.v1` artifact contract: clearing
  verdicts require referenced `claim`/`challenge`/`rebuttal` entries (Check A),
  entry refs resolve to `dialogue:<seq>` turns and `by` names a participant
  (Check B), and the recorded verdict must equal the ledger front-matter
  verdict on BOTH `review.submit` and the primitive `recordVerdict` path
  (closing the publish-then-accept bypass).
- Adds the `adjudicator` role + a `phase_synthesis` substance gate, and the
  generator shapes `falsification_gate` and `cross_examination` plus a `scribe`
  participant modifier; cycle-scoped (`cycle_<attempt>`) ledger naming lets a
  `needs_revision` revision iteration re-publish without a content-hash
  collision. `workflow validate` now refuses `same_model_adjudicator_pair`
  unless overridden. Example fixtures + docs (spec, ubiquitous-language,
  workflow-types, RFC 0083 re-expression) included.
- Deferred: `fog_of_war_review`, `synaptic_prune`, `post_dialog_hook`. No new
  daemon method. (RFC 0093 accepted, D155.)

### GH #63: revision-routing, retriable cycle targets, attestation-stall (daemon fixes)

- Cycle router (F1): a `needs_revision` verdict now routes to its declared
  workflow cycle — re-opening the target, re-blocking the downstream reviews,
  and superseding their stale verdicts — instead of always opening a human
  checkpoint. Completed cycle-target jobs are retriable for revision (F3);
  budget exhaustion falls back to the checkpoint.
- Discovery-stall classifier (F4): any recorded MCP activity now satisfies the
  discovery deadline, so live agent-loop lanes no longer trip
  `agent_mcp_discovery_stall` and demote attested bylines to `author: operator`;
  a zombie-guard keeps the protocol-idle catch-all for lanes that pinged once
  then died.

### RFC 0092 follow-up: live-dialogue SSE allowlisted over the tailnet identity socket

- Adds `GET /v1/runs/{run_id}/live-dialogue` to the RFC 0085 identity
  read-route allowlist so the live agent dialogue feed is reachable over
  `tailscale serve` (it returned `route_forbidden` before).

### RFC 0089: tmux-backed lane monitoring substrate

- Replaces attach-client liveness with tmux session/pane liveness for
  supervised lanes: the supervised pane PID and start token are the lane
  identity, while operator `tmux attach-session` clients are observer-only.
- Records helper-owned attach-bridge exits as degraded packet delivery when
  the pane remains live, so `supervise.send` refuses further delivery instead
  of reporting a false-healthy lane.
- Adds tmux session-name hash suffixing, start-token verification/fallbacks,
  safer tmux-backed teardown, and explicit pipe-reader degradation handling.

## v2.7.1 — 2026-05-27

### D147: tailnet-identity UI serves the read-only HTML dashboard

- The RFC 0085 (D143) tailnet-identity socket previously allowlisted only the
  `/v1/...` JSON read routes, so a tailnet browser hitting `/` got
  `route_forbidden` and the human HTML dashboard was loopback+bearer only. The
  allowlist now also permits `GET /`, `GET /run` (the server-rendered `status`
  page) and `GET /static/{asset}` — so the dashboard is viewable over
  `tailscale serve` at `https://<magicdns-host>:9443/` by identity-allowlisted
  users. Still GET-only; `POST /v1/invoke` and all mutations remain 403; no-identity
  remains 401; `path.Clean` blocks `/static/` traversal. `IdentityReadRoutes` +
  `PermitIdentityRoute` updated together, kept in sync by the normative
  `TestIdentityRouteAuditMatchesAllowlist`. Live-verified over `web-ui.sock`.

## v2.7.0 — 2026-05-27

### F44: supervised turn-driver hardening (D146)

Fixes the production-path bug F42's live verification surfaced: a daemon-spawned
single-shot turn-driver inherited the daemon's systemd `PATH`, could not find its
generator binary (`exec: "gemini": executable file not found in $PATH`), crashed
instead of parking the floor, and zombied with stale `alive` liveness.

- **Generator findable.** Supervised lanes now build one effective `PATH`: the
  inherited system `PATH` plus any existing operator-local bin dirs
  (`$HOME/.local/bin`, `$HOME/.npm-global/bin`, or `STRIATUM_SUPERVISED_PATH_DIRS`),
  deduped. No hardcoded home; only existing dirs are appended. This is not
  control state, so it does not weaken the D145 topic+transcript-only generator
  boundary. The operator `striatumd.service.d/path.conf` workaround is no longer
  needed.
- **Graceful generator failure.** `turndriver.Loop` routes exhausted generator
  failures (including exec-not-found) through `OnFailure` → `session.report`
  escalation + parked floor, instead of crashing `RunTurnDriver`.
- **Honest liveness.** Pipe supervisors capture `pid_start_time` and reap via an
  async `cmd.Wait`; read-side liveness is zombie- and start-token-aware;
  `supervise.status`/dashboard surface the latest escalation report. An exited
  supervised child now reports `gone`, not a frozen `alive`.
- Designed + built through the iterated-interrogating-panel dogfood
  (`run_8e1f8965…`), four `accept_with_findings` verdicts with live interrogations.
  Live-verified in isolation: with the daemon forced to a minimal `PATH` (gemini
  not on it), a supervised gemini turn-driver's PATH was augmented to include
  `~/.local/bin`, the generator executed, the driver stayed healthy, and
  `supervise.status` reported honest liveness with no zombie. Deferred: durable
  unexpected-exit terminal-state persistence; resident retry-after-escalation.

## v2.6.0 — 2026-05-26

### F42: autonomous conversation turn-driver (D145)

- **Generic turn-driver for non-self-driving lanes.** RFC 0086 proved the
  conversation primitive, but `gemini -p` (single-shot, non-agentic) could not
  hold the stateful `await_packet → say → await_packet` loop and needed an
  operator-side shell driver. F42 makes a non-self-driving lane participate
  autonomously by moving the loop into Striatum.
- **`go/pkg/turndriver`** — pure, fake-tested loop (`Conversation` + `Generator`
  seams; `ConversationContext` carries only `Topic`+`Transcript`; output
  sanitization, bounded retries, not-our-floor wait, closed-conversation exit;
  idempotent/crash-safe, no double-speak).
- **`striatumd -agent-loop -turn-driver`** runtime wiring: the **driver** is the
  MCP client (holds the token; calls `work.await_packet`, `conversation.show`,
  `conversation.say`); the child agent is invoked once per turn as a content
  generator receiving topic+transcript only. `ContentOnlyEnv` strips all
  `STRIATUM_*` from the child environment.
- **Selection by capability, not model name.** `supervise.start` runs driven
  mode when a lane declares `adapter_capabilities.single_shot: true` (or
  `self_driving: false`); `supervise.status` / dashboard surface
  `agent_loop_mode=turn_driver`. The conversation-3way recipe + gemini guide
  templates document the path; the `/tmp/gemini-driver.sh` operator hack is
  obsoleted.
- **Boundary enforcement** (the prohibited packet-spoon-feeding proxy): a
  reflection test pins `ConversationContext` to topic+transcript only; the
  generator's output is routed solely to `conversation.say` and never parsed as
  control; if `await_packet` returns a work packet or interrogation question to a
  turn-driver session, the driver errors rather than feeding it to the child.
- Designed + built through the iterated-interrogating-panel dogfood
  (`run_63a8ffa4a77edebfd25620876fe9e7ce`); four `accept_with_findings` verdicts
  with genuine live interrogations of the synthesizer and implementer. Residual
  v1 risk (same-user child daemon-material discovery) recorded in D145.

## v2.5.0 — 2026-05-26

### RFC 0086: multi-party conversation on the MCP agent-loop (D144)

- **New live N-party conversation primitive** generalizing RFC 0082 interrogation
  (the 1→1 special case) to symmetric, round-robin, agent-loop-native group
  dialogue. `conversation.{open,say,close,list,show}`; a new
  `conversation_message` envelope on `work.await_packet`; turns reuse the message
  bus; lifecycle/floor in a plain new `conversations` table (migration 0017, no
  owner-table FKs — runtime-role-applicable; schema 16 → 17).
- **Crash-safe floor-derived delivery**: "your turn" is derived from durable
  `floor_index`, not a consumable message — a floor-holder that errors/restarts
  before `say` simply sees its turn again (the live run surfaced and this fixed a
  round-robin stall). Idempotent + read-only.
- **Proven end-to-end**: three frontier models — Claude Opus, Codex GPT-5.5,
  Gemini 2.5 Pro (models pinned explicitly per lane) — held a live 9-turn,
  3-round conversation at ~16s/turn on the agent-loop (transcript:
  `docs/operator/workflows/conversation-3way/TRANSCRIPT.md`). Gemini's speed was
  fixed by pinning the GA `gemini-2.5-pro` (the preview default was
  capacity-throttled).
- Follow-ups: F42 (gemini-cli is unreliable at the multi-step await/say loop —
  needed an operator-side turn driver; harden with a thin conversation-loop
  wrapper or default lease), F43 (render conversations in the chat UI, reusing
  the RFC 0084 renderer with speaker = participant).

## v2.4.2 — 2026-05-26

### RFC 0085: tailnet-identity UI auth (run through interrogation, design + build)

- **Loopback preserved; opt-in read-only tailnet UI.** `striatumd --web-tailscale`
  (default off) starts a dedicated `0600` unix socket
  (`$STRIATUM_DAEMON_RUNTIME_DIR/web-ui.sock`) serving the web UI in a
  Tailscale-identity auth mode, the stable target for `tailscale serve`. The
  daemon's loopback bind and bearer/MCP path are unchanged and never trust
  identity headers.
- **Auth = allowlisted tailnet identity, read-only via an explicit route
  allowlist.** A request is authenticated iff `Tailscale-User-Login` is in
  `STRIATUM_DAEMON_WEB_TAILSCALE_USERS` (unset/empty/whitespace → deny all, fail
  closed); permitted routes are an audited GET allowlist (`/v1/health`,
  `/v1/runs[/{id}[/interrogations[/{id}]]]`) — everything else (incl.
  `POST /v1/invoke` for any method, workflow generation) is denied. A normative
  route-audit test asserts no mutating route is reachable over the socket.
- **Process:** RFC 0085 was run through the interrogating panel. The design
  interrogation (`intg_4b69c562`) returned `needs_revision`, rejecting
  verb-based "GET means safe"; the RFC was revised to the explicit route
  allowlist + audit test before any code. The build interrogation accepted; live
  verification over the socket confirmed allowed-identity GET → 200 (chat renders
  6 turns), not-in-allowlist/POST/non-allowlisted → 403, no identity → 401, and
  the MagicDNS-Host path → 200. `serve` only; `funnel` prohibited. D143.
- Follow-up F41: the route-audit test's `allRoutes` is manually kept in sync with
  the router (build-review finding) — derive it from the dispatch table.

## v2.4.1 — 2026-05-26

### RFC 0084 follow-ups (dogfooded via the iterated interrogating panel)

- **D1 — Go web service mounted in the daemon.** `striatumd`'s HTTP listener now
  multiplexes `/mcp` (MCP JSON-RPC/SSE) and everything else (`/v1/...`, `/run`,
  `/static`) to the Go web service, so the RFC 0084 interrogation chat route is
  reachable live. Verified end-to-end: MCP unregressed, `/v1/health` 200 with
  bearer + 401 without, and `/v1/runs/{id}/interrogations/{id}?view=chat` renders
  the real interrogation thread with `html/template` escaping + run-ownership
  404. Build-review interrogation + live verification additionally fixed a
  fail-open empty-token auth path (now an unguessable deny token →
  fail-closed) and a `[]map[string]any` turns type-assertion that silently
  dropped all turns. Mounted web service scopes via
  `STRIATUM_DAEMON_WEB_REPOSITORY_ID`; per-run multi-repo resolution is TODO F38.
- **D2 — Gemini agent-loop reliability.** A Gemini lane fixed its own failure
  mode: both Gemini agent guides gain an "Agent-loop reliability (Gemini)"
  section (request a long lease, do not explore the repo while holding a packet,
  pass explicit `repository_id`/`session_id`, complete promptly). Running with a
  900s lease + no-exploration discipline, the lane completed the full packet loop
  that previously failed on lease expiry.
- Tracked follow-ups: F38 (per-run web repo resolution), F39 (harden MCP
  `review.verdict`/`artifact.publish` + name the missing `verdict_intent`), F40
  (diagnose the `work.release` hang).

## v2.4.0 — 2026-05-26

### RFC 0083 + RFC 0084: iterated interrogating panel, agent-loop interrogation, chat UI

- **RFC 0083 (D139/D140)**: new reusable *Iterated Panel Review with
  Interrogation* workflow pattern — design + build loops, each fan-out (3 lanes)
  → synthesis → interrogating panel with ≤3 interrogation rounds + a bounded
  revision cycle. Reusable example `examples/iterated-interrogating-panel/`,
  template catalog entry, `docs/WORKFLOW_TYPES.md` section. Conditional
  deprecation of the `--print` supervised wrapper for new workflows, gated on
  per-adapter agent-loop validation.
- **Agent-loop substrate validation**: headless `claude -p` and `codex exec`
  drive genuine MCP agent-loop packet loops (await → publish → ack → complete);
  the `striatumd -agent-loop` PTY launcher does not submit prompts to TUI
  agents; gemini connects but is too slow (lease expiry). See
  `docs/operator/workflows/interrogating-panel-2026-05-25/SUBSTRATE_VALIDATION.md`.
- **RFC 0084 (D141)**: interrogable agent-loop attestation. `requireLiveTarget`
  now accepts a live session in the `awaiting_interrogation` window as a valid
  interrogation target, not only wrapper-attested sessions — unblocking genuine
  model-to-model interrogation on the agent-loop (the only lanes with preserved
  context). Artifact byline attestation (D080) is unchanged. Regression test
  `TestInterrogationOpenAcceptsAwaitingInterrogationTarget`.
- **RFC 0084 (D142)**: interrogation-log chat UI. The Go web service renders a
  run's interrogation thread as a server-rendered chat at
  `/v1/runs/{runID}/interrogations[/{id}]`, backed by the existing interrogation
  read path (`html/template` escaping, run-ownership 404, D028 curated fields
  only). Closes the presentation-layer half of F36 for interrogation threads.

## v2.3.2 — 2026-05-25

### Follow-ups (F33/F34/F35) + doc scrub

- **F33**: seeded live-PostgreSQL trajectory test (`go/pkg/reads`) asserting
  `trajectory.export`/`watch` reproduce a seeded run in derived-`seq` order for
  both profiles, D028-safe.
- **F34**: `striatum daemon migrate-db [--admin-url <dsn>]` applies pending
  daemon PostgreSQL migrations via an owner/admin DSN (RFC 0079 §5), so DDL the
  runtime role cannot perform is applied before the daemon serves. Distinct from
  the retired SQLite-era `daemon migrate`.
- **F35**: removed the dead Python-era Jinja `src/striatum/web/templates` (the
  Go web service embeds its own `go/pkg/webassets`); kept the live Node frontend
  + bundle pipeline. The Vite-bundle-not-embedded architectural finding is
  tracked as F36.
- **Doc scrub**: corrected `docs/CLI_REFERENCE.md` to the actual Go workflow
  verbs (`validate`, `generate`, `templates {list,show}`), removed unported
  Python-era prose (`workflow {init,lint,plan,graph,upgrade,templates render-md}`),
  documented `daemon migrate-db` + owner-applied-migration recovery in the daemon
  runbook, and refreshed stale version strings in README/GETTING_STARTED. A full
  CLI_REFERENCE audit is tracked as F37.

## v2.3.1 — 2026-05-25

### Fix: wire the `workflow generate` / `workflow templates` Go CLI verbs

RFC 0078 ported the workflow generator/catalog packages but never wired their
CLI verbs (only `workflow validate` was exposed), so RFC 0081's documented
`workflow generate --shape conversation …` failed with `unknown command`. Wire
`workflow generate` (preview by default, `--write` to commit; `--shape`,
`--lane-set`, `--workflow-id`, `--scaffold-root`, `--artifact-root`,
`--option key=value`) and `workflow templates {list,show}` as local commands
over the existing `workflowgenerate`/`workflowtemplates` packages, with CLI
tests. `workflow generate --shape conversation` now scaffolds the RFC 0081
conversation workflow type end-to-end (defaults to the `local` fixture lane set).

## v2.3.0 — 2026-05-25

### RFC 0082: interrogation sessions (accepted)

Adds the construct the event-bus assessment found missing: a worker preserves
its context window and other workers iteratively interrogate it for design/build
review. `interrogation.{open,ask,answer,close,list,show}` daemon methods + CLI +
MCP tools; targeted delivery via a typed `work.await_packet` envelope
(`work_packet` | `interrogation_question` | `none`); an `awaiting_interrogation`
context-preservation window on the MCP agent-loop with `interrogable` jobs;
curated turns (D028) surfaced in the RFC 0081 `dialogue` trajectory. Migration
0016 (`interrogations`) is ownership-safe. All 9 RFC 0082 Required Tests pass
under live PostgreSQL, including the end-to-end intention test (a reviewer
interrogates a builder's preserved context). See D138.

## v2.2.0 — 2026-05-25

### RFC 0079 / 0080 / 0081: operability, test hardening, conversation trajectories

- **RFC 0079 (operability & install, accepted)**: `striatum daemon install`
  generates a portable systemd user unit (`%h`/`%t`); `make install` places
  binaries + unit + skills and verifies `doctor`; a daemon runbook documents the
  runtime layout and `daemon.toml` DSN; the canonical socket is `daemon-go.sock`.
  PostgreSQL migrations are applied by an owner/admin connection (the daemon's
  runtime role lacks DDL on owner tables).
- **RFC 0080 (test & build hardening, accepted)**: reusable `go/pkg/pgtest`
  PostgreSQL harness; live-PG tests run in CI; restored coverage; `go vet`,
  `go test -race`, lint, and coverage gates; the `complete` write-scope guard now
  baseline-diffs instead of flagging pre-existing untracked paths.
- **RFC 0081 (conversation trajectories, accepted)**: `striatum trajectory
  export/watch` with `dialogue`/`provenance` profiles over a read-model of
  existing daemon events — ordering derived at read time (no stored column, no
  new authority, no `ALTER` of owner tables), `trajectory_segments` for export
  metadata, a tmux monitor, and a `conversation` workflow type. Curated
  structured provenance only; never raw provider transcripts (D028). The design
  was produced by a recorded two-model conversation over the message bus.

See D135/D136/D137. Daemon-owned PostgreSQL remains the only live-state substrate.

## v2.1.0 — 2026-05-25

### RFC 0078: Go-only runtime and Python removal (accepted)

Striatum is now Go-only at repository HEAD. The legacy Python runtime,
source, the Python test suite, packaging, and tracked Python scripts are
removed; their behavior is ported to Go or explicitly retired.

- **Corpus**: redaction-tier compliance ported into the Go reads/export
  path; the standalone Python corpus modules retired.
- **Installers**: `striatum skills install` / `plugin install` reimplemented
  in Go with embedded templates and `doctor` version-stamp parity; the
  `scaffold` flag retired.
- **Generators**: the daemon-method-table and Go RPC-registry generators
  consolidated into the Go `routergen` tool, fed by
  `contracts/daemon_methods.json` with a byte-identical `go generate`
  round-trip; the Python generators removed.
- **Tests**: Python test coverage migrated to Go (and frontend/shell) tests
  or retired by recorded ledger reason; all tracked Python tests deleted.
- **Packaging/docs**: `pyproject.toml`, the legacy Python Makefile targets,
  and the remaining Python scripts removed; operator guidance rewritten to
  Go-only; release is Go binary archives.
- **Guardrail**: the temporary Python-trace guardrail was retired after active
  Python runtime surfaces were removed or archived.

Daemon-owned PostgreSQL remains the only live-state substrate. See D134 and
`docs/operator/artifacts/rfc-0078-closure/`.

## v1.57.0 — 2026-05-19

### GH #25 / #26 / #27 cluster: doctor parity + repo list + artifacts trigger

Three follow-up issues surfaced after the GH #22/#23/#24 cluster
shipped, all driven through the docs/issues/<N>/ workflow shape and
verified end-to-end:

- **GH #25**: `striatum repo list` (no `--json`) now consults the
  daemon repo registry instead of pre-flighting on
  `.striatum/retired-local-state`. The misleading `repo_not_migrated`
  refusal is replaced by a human-readable table; daemon-unreachable
  errors surface cleanly. CLI dispatch routed through
  `src/striatum/cli/daemon_rpc_route.py` (+93 LOC).

- **GH #26 / RFC 0073**: `striatum daemon doctor` now surfaces the
  RFC 0072 blob diagnostics block under `data.blob`. The Python
  doctor handler delegates to a focused Go `reads.doctor_blob_block`
  sub-handler (Option B from RFC 0073), so the operator sees
  `{configured, reachable, bucket, bucket_status, ...}` at the same
  level as `daemon_diagnostics`. Closes the silent-gap pattern that
  caused two SCOPE artifacts to publish to repo-path only during the
  earlier cluster.

- **GH #27**: PG migration 0010 introduces a column-aware
  `artifacts_no_update` trigger that allows UPDATEs touching ONLY
  `(blob_key, blob_sha256, blob_content_type)` and refuses any other
  identity-column update with the existing `P0001`. `roles.py` grants
  column-level UPDATE for those three columns to `striatumd_rw`.
  `artifact.backfill_blob` no longer requires owner trigger-disable.
  400-line test suite covers positive, negative, mixed, DELETE-still-
  refused, and migration-idempotency cases.

`artifact.backfill_blob` itself shipped in v1.56.0; the v1.57.0 work
makes it operable for the runtime role.

## v1.56.0 — 2026-05-19

### GH #22 / #23 / #24 daemon-recovery cluster

Three daemon-recovery bugs surfaced while bringing the local 1.54.0 →
1.55.0 daemon back up after RFC 0072 migration 0009 landed:

- **GH #22**: `striatum daemon doctor --apply-migrations --as-owner
  <owner-url>` is the first-class operator path for applying pending
  daemon migrations as the table-owner role. `striatum daemon stop`
  is now pidfile-driven (audit is best-effort), so a pending migration
  cannot lock the daemon out of being stopped. The migration-privilege
  hint string names the supported `--as-owner` shape and no longer
  advertises the test-harness env var.

- **GH #23**: the Go daemon now writes
  `<runtime_dir>/striatumd.pid` atomically (temp + rename) before
  binding the Unix socket, and removes it on clean shutdown. A
  pre-existing pidfile pointing at a live foreign `striatumd` refuses
  to start; a stale PID is overwritten. `striatum daemon status` is
  now accurate without the manual `echo $pid > striatumd.pid`
  workaround.

- **GH #24**: `claim-next` surfaces `data.packet_id` beside
  `data.status` / `data.packet`, and `data.next_steps.supervise_send`
  carries the literal command. `supervise send` detects obvious
  wrong-kind IDs (`msg_*`, `lease_*`, `job_*`, `sess_*`, `sup_*`) and
  fails with a message pointing at the right field. `release --requeue`
  on `repo_write` jobs refuses with `invalid_transition` and names
  `striatum recovery requeue-stale` as the recovery verb instead of
  silently parking the job in `blocked`. Skill templates
  (claim-loop, supervise, gemini guide, generic guide) updated with
  the real CLI paths.

### Architecture remediation follow-through

The remediation plan from the 2026-05-16 architecture review is now tracked
in the roadmap/TODO and has several production slices landed:

Current behavior is summarized in this section. Older release entries below
remain historical notes for the behavior that shipped at that tag.

Recent checkpoints:

- D107 supersedes D105: Go is now the production/default daemon, active
  contract-method parity is landed, D111 retires the Python daemon selector,
  and the retired Python daemon module is deleted. Python CLI/web clients
  remain useful, while SQLite is retired from production and operator
  compatibility paths. RFC 0068 records the port; RFC 0069-0071 cover
  daemon-global PG, client-boundary, and diagnostic follow-ups.
- Stale decision/RFC wording now reflects the Go/PostgreSQL runtime boundary:
  durable artifact provenance, evidence identity, worktree state, dogfood
  composite tooling, and packaging notes no longer imply a current Python
  daemon or repo-local SQLite authority.
- `import striatum.cli` no longer eagerly imports SQLite-backed legacy
  evidence/introspection/list/mutation/recovery/run-summary/worktree modules;
  historical package-level re-exports now resolve lazily when callers request
  a specific compatibility symbol.
- Evidence redaction policy and Markdown rendering now live in a
  substrate-neutral presenter module. PostgreSQL evidence handlers and corpus
  redaction use that shared code directly instead of importing the legacy
  SQLite-backed CLI evidence reader.
- Run-summary Markdown formatting, duration formatting, and verdict grouping
  now live in a substrate-neutral formatter used by PostgreSQL handlers and
  corpus exports; the SQLite-backed CLI module keeps only its legacy snapshot
  and export wrapper.
- The remaining direct SQLite CLI dispatch block now runs only under the
  paired legacy test-harness escape; production commands that are not
  daemon-routed fail closed before opening repo-local state.
- Importing `striatum.cli.dispatch` no longer eagerly imports `sqlite3`,
  `striatum.db`, legacy workflow/artifact helpers, or SQLite-backed CLI
  reader/mutation modules; fixture-only imports are loaded only after the
  paired legacy test-harness gate.
- The deterministic `next_actions` projection moved into a substrate-neutral
  module. PostgreSQL read-model status no longer imports the SQLite-backed
  CLI introspection module for that helper.
- `current_git_branch` moved into a substrate-neutral Git helper so
  PostgreSQL run-summary and branch-confirm handlers no longer import the
  SQLite-backed CLI mutation module for Git branch inspection.
- Artifact-kind constants, front-matter validation, and Markdown byline
  parsing moved into `striatum.artifact_contracts`; PostgreSQL artifact
  publish/recovery handlers no longer import the SQLite-backed legacy
  `striatum.artifacts` module for neutral contract helpers.
- Daemon PostgreSQL handler registration no longer eagerly imports
  `striatum.artifacts` or `striatum.workflow`; architecture guardrails now
  cover those legacy module boundaries in addition to `sqlite3`,
  `striatum.db`, and SQLite-backed CLI readers.
- The legacy repo-local SQLite artifact publisher moved to
  `striatum.legacy_sqlite.artifacts`; `striatum.artifacts` is now a neutral
  compatibility facade that imports the legacy publisher only when callers
  invoke legacy publish/byline helpers.
- Legacy repo-local SQLite workflow live-state helpers (`create_run` and
  `compute_node_states`) moved to `striatum.legacy_sqlite.workflow`;
  `striatum.workflow` now keeps validation, graph, and planning helpers
  separate from the fixture-only run-state implementation.
- The legacy repo-local SQLite autonomous recovery sweep moved to
  `striatum.legacy_sqlite.recovery_auto`; `striatum.recovery.auto` now
  exposes only lazy compatibility wrappers for that retired path.
- Process-adapter diagnostic envelope and recovery-command helpers remain in
  neutral `striatum.process_completion`; SQLite output validation and blocker
  insertion moved to `striatum.legacy_sqlite.process_completion` and now load
  lazily for legacy adapter paths.
- The legacy repo-local SQLite process adapter moved to
  `striatum.legacy_sqlite.process_adapter`; `striatum.process_adapter` now
  keeps neutral env expansion/schema constants and lazy wrappers for legacy
  adapter calls.
- The legacy repo-local SQLite supervisor helper moved to
  `striatum.legacy_sqlite.supervisor`; `striatum.supervisor` now exposes only
  the active-state constant and lazy wrappers for legacy supervise calls.
- The SQLite-bound dogfood operator composites moved to
  `striatum.legacy_sqlite.dogfood_operator_tools`; `striatum.dogfood` and
  `striatum.dogfood.operator_tools` now import without loading SQLite.
- The legacy SQLite worktree CLI helpers moved to
  `striatum.legacy_sqlite.cli_worktree`; `striatum.cli.worktree` now exposes
  lazy wrappers for compatibility callers.
- The legacy SQLite evidence CLI reader/exporter moved to
  `striatum.legacy_sqlite.cli_evidence`; `striatum.cli.evidence` now exposes
  lazy wrappers for compatibility callers.
- The legacy SQLite run-summary CLI reader/exporter moved to
  `striatum.legacy_sqlite.cli_run_summary`; `striatum.cli.run_summary` now
  exposes lazy wrappers for compatibility callers.
- The legacy SQLite list CLI readers moved to
  `striatum.legacy_sqlite.cli_list_commands`; `striatum.cli.list_commands`
  now keeps neutral filter constants and lazy wrappers for compatibility
  callers.
- Product docs now describe the Python daemon module/selector as deleted,
  with remaining cleanup limited to legacy SQLite fixture/import conversion
  or deletion.
- Service/web legacy SQLite fallback now requires the explicit
  `STRIATUM_LEGACY_SERVICE_FIXTURE=1` marker in addition to the paired
  test-harness daemon opt-out; broad pytest daemon opt-out alone no longer
  disables daemon RPC routing for service calls.
- Legacy SQLite status/why/doctor introspection helpers moved to
  `striatum.legacy_sqlite.cli_introspect`; `striatum.cli.introspect` now
  exposes neutral constants and lazy compatibility accessors.
- Legacy SQLite recovery mutation helpers moved to
  `striatum.legacy_sqlite.cli_recovery`; `striatum.cli.recovery` now keeps
  parity constants and lazy compatibility accessors.
- Legacy SQLite workflow-loop mutation helpers moved to
  `striatum.legacy_sqlite.cli_mutations`; `striatum.cli.mutations` now keeps
  the neutral verdict-job constant and lazy compatibility accessors.
- Legacy SQLite DB imports used by the paired test-harness CLI dispatch path
  moved behind `striatum.legacy_sqlite.cli_dispatch_db`; importing
  `striatum.cli.dispatch` no longer imports `sqlite3` or `striatum.db`.
- `daemon doctor --repo --authority` now uses the SQLite-free
  `striatum.daemon_pg.repo_cutover_report` module for cutover verification
  instead of importing the retired repo-local SQLite migrator.
- The repo-local SQLite engine and schema migrations moved to
  `striatum.legacy_sqlite.db` and `striatum.legacy_sqlite.migrations`;
  root `striatum.db` / `striatum.migrations` are lazy compatibility facades
  that do not import SQLite until legacy fixture callers request attributes.
- The retired repo-local SQLite import helper moved to
  `striatum.legacy_sqlite.repo_local_migration`; the old
  `striatum.daemon_pg.repo_local_migration` import path is now a lazy facade
  and no longer imports SQLite on plain module import.
- Legacy service/chat/MCP subprocess fixtures now opt into
  `STRIATUM_LEGACY_SERVICE_FIXTURE=1` explicitly, and the authority matrix
  names the retired repo-local migration import fixture's bounded direct-PG
  exception.
- Legacy SQLite tests now import `striatum.legacy_sqlite.*` directly instead
  of the root `striatum.db` / `striatum.migrations` compatibility facades, and
  architecture guardrails prevent new test fixture imports through those root
  facades.
- The GH #7 terminal-blocker regression test no longer imports `sqlite3` just
  to mock the early-return connection path.
- The web cancel-route test now uses the explicit legacy SQLite fixture helper
  instead of importing `sqlite3` directly for its completed-run setup.
- The web pause/resume and posture-verdict tests now use the explicit legacy
  SQLite fixture helper instead of opening repo-local SQLite directly.
- Web/dashboard provenance, recovery-panel, breadcrumb, and posture-override
  tests now seed legacy repo-local state through the explicit legacy SQLite
  fixture helper instead of importing `sqlite3` directly.
- Pause/resume, cancel, retry-job, and recovery-resume tests now use the
  explicit legacy SQLite fixture helper instead of opening repo-local SQLite
  directly.
- Process-adapter, dogfood recovery/publish, session-close, and supervise
  tests now use the explicit legacy SQLite fixture helper instead of opening
  repo-local SQLite directly.
- Worktree-isolation, recovery, reviewer-policy, harness, and artifact-schema
  tests now route legacy repo-local state setup through the explicit legacy
  SQLite fixture helper instead of direct `sqlite3` opens.
- Review-posture introspection and service fixture tests now use the explicit
  legacy SQLite helper for repo-local setup instead of direct `sqlite3` opens.
- The broad CLI MVP regression suite now uses the explicit legacy SQLite
  helper for repo-local state setup instead of direct `sqlite3` imports.
- Stale remediation/review artifacts now call out deleted Python-daemon
  references as historical evidence instead of current source state.
- Operator docs now describe legacy SQLite migration/tombstone paths as
  historical remnants or fixture-only compatibility, while current setup
  guidance uses daemon PostgreSQL registration.
- External architecture review and remediation-plan artifacts moved from the
  repository root into `docs/reviews/external/`; comparison and design
  research notes moved into `docs/research/`, leaving the root limited to
  canonical project files.
- The frontend Vite build now groups Shiki's dynamic imports into one
  `island-shiki-*` lazy chunk and the bundle guard enforces file/chunk-count
  limits so stale generated chunk piles cannot quietly accumulate again.
- The 2026-05-18 architecture review artifacts were refreshed to reflect the
  Go/PostgreSQL daemon, capability-token, public-adoption, and
  human-principal escalation direction; the rejected embedded-storage and
  single-binary recommendations remain out of the actionable roadmap.
- The retired `src/striatum/daemon.py` Python daemon / daemon-global SQLite
  registry module was deleted. Architecture guardrails now assert the module
  remains absent and keep daemon-global refusal coverage on the PostgreSQL
  admin helper surface.
- The Go daemon launch contract now reports supported daemon PostgreSQL schema
  and migration count from `--describe`; the Python launcher refuses stale Go
  daemon binaries before socket bind when their schema, migration count, or
  method contract does not match the source tree.
- Go daemon builds now stamp `striatumd --describe` with the Python package
  version, git SHA, and dirty/clean state. The Python launcher also rejects
  unstamped `go-dev` binaries and binaries that omit git provenance before
  they can bind a socket.
- `doctor --first-run` now returns a single V1 diagnostic report that combines
  day-zero smoke checks, Go daemon binary provenance, and the daemon authority
  report so operators can validate the local stack with one command.
- The local stdio MCP compatibility wrapper no longer advertises or executes
  CLI-shaped aliases through `tools/list` / `tools/call`. Production MCP tool
  discovery stays on the daemon registry surface, while `striatum/invoke` and
  read resources remain available only as compatibility/manual paths.
- Active operator docs now frame legacy SQLite handling as archive/remove plus
  repository registration, not a current per-repo migration workflow; smoke
  scripts also stopped exporting the legacy daemon SQLite registry path.
- The retired daemon-global SQLite registry cutover implementation
  (`striatum.daemon_pg.cutover`) was deleted; compatibility tests now assert
  the old `daemon migrate --from sqlite --to pg` spelling still refuses before
  importing any cutover code.
- The command authority matrix now names the bounded direct-PostgreSQL
  bootstrap/admin plane, and an architecture guardrail scans Python client/CLI
  sources so new direct daemon-PG helper imports must be explicitly listed.
- The Go cross-repo runner boundary was narrowed from one speculative
  `LocalRunner` interface to per-operation prepare/start/cancel interfaces,
  removing the unused `ParticipantIntact` and `HumanCheckpoint` hooks and the
  Go daemon's placeholder `Prepare` method.
- Go migration SHA-source verification now rejects extra newer Python-source
  migrations, closing the stale-binary gap where an old Go binary could pass
  hash checks until it hit a migrated database.
- Fresh Go daemon startup now bootstraps the first PostgreSQL admin client
  and writes the private runtime `client-token`, matching the legacy Python
  daemon's first-start auth contract without requiring that daemon core.
- The Go daemon now starts a resident recovery scheduler after socket bind.
  It runs an immediate PostgreSQL active-run sweep, calls the Go
  `recovery.sweep` path, records `daemon.recovery_sweep`, updates
  `striatumd.scheduler_cursors`, and accepts `--sweep-interval-seconds` plus
  bounded-test `--max-sweeps` flags through the Python launcher.
- `make daemon-go-conformance` is now the Go production-daemon CI gate. It
  builds and tests the Go daemon, then runs the multi-repo harness with
  `CORE=go`, including Go daemon smoke, audit, mutation-registry, and
  supervisor smoke coverage.
- Go `daemon.shutdown` now wires through the daemon process cancellation path
  and returns an accepted shutdown response instead of the previous
  fail-closed `shutdown_unavailable` placeholder.
- Go `doctor` now reads `striatumd.schema_meta['substrate_version']` instead
  of querying a nonexistent `schema_meta.version` column.
- Daemon RPC handshakes from the CLI and day-zero first-run smoke now use
  `striatum.__version__` instead of hardcoded client versions.
- The Go daemon now has an executable handler-coverage ledger for missing
  and placeholder methods, and `recovery.sweep` is registered on the Go
  mutation surface instead of only the deprecated `recovery.auto` alias.
- D110 removes the SQLite-bound `daemon.migrate_repo_local`,
  `dogfood.publish_on_behalf`, and `dogfood.surgical_recovery` RPC methods
  from the production daemon contract and MCP discovery. Unknown calls now
  audit as `method_unknown`.
- D112 removes `apply.reviewed_patch` from the production daemon RPC contract
  instead of carrying it as a fail-closed RFC 0068 retirement blocker. Stale
  direct calls now return and audit as `method_unknown`; apply receipt reads
  and daemon signing-key rotation remain supported.
- SQLite import windows are now closed for production/operator paths.
  `striatum daemon migrate` and `striatum daemon migrate-repo-local` remain
  parser-compatible compatibility spellings, but they refuse with exit code
  12 before importing or opening SQLite migration code. Direct
  `migrate_repo_local()` use is guarded behind the explicit
  `STRIATUM_LEGACY_SQLITE_IMPORT=1` fixture escape; `adopt`, repo
  registration, and repo-not-migrated hints now point operators to archive or
  remove legacy SQLite files and register with `adopt` / `repo add --init`.
- Daemon MCP `resources/list` and `resources/read` now require an explicit
  daemon PostgreSQL connection. The no-`pg_conn` legacy SQLite registry
  fallback is retired, and the corresponding Python-daemon MCP resource
  helpers were removed.
- `striatum.api` no longer imports `sqlite3` or `striatum.db`; it uses the
  shared primitives types and leaves SQLite-era failures outside the local API
  compatibility wrapper.
- `workflow upgrade` and `workflow upgrade --add-phases` now use only the
  daemon PostgreSQL running-run guard. The repo-local SQLite fallback and its
  paired test-harness escape were removed from the workflow-upgrade path.
- Corpus manifest construction no longer accepts or fakes a SQLite
  connection. Manifests now carry explicit `state_authority` metadata, and the
  PostgreSQL `corpus.export` handler reads daemon/repository schema metadata
  directly instead of emulating `PRAGMA user_version`.
- Production daemon CLI/admin dispatch now imports the PostgreSQL-only
  `striatum.daemon_pg.client_admin` surface instead of the legacy Python daemon
  module. The CLI-side legacy daemon registry wrapper and its direct
  `--daemon`/`dashboard --all` SQLite fallback paths are removed; remaining
  `striatum.daemon` references are guardrail/test assertions or historical docs
  (D117).
- Legacy daemon security fixture coverage was narrowed again: runtime token and
  daemon MCP denial checks now exercise `daemon_runtime`, `daemon_pg.client_admin`,
  and daemon RPC capability helpers directly, leaving only cutover/quarantine
  fixture coverage for retired behavior.
- The multi-repo Go daemon test harness no longer imports the legacy Python
  daemon module for runtime environment constant names; it uses
  `daemon_runtime` and the PostgreSQL admin client surface directly.
- CLI daemon-route parsing, workflow scaffolding, skill/plugin installers,
  daemon supervisor helper imports, and several tests now import neutral
  primitives/path-policy helpers directly instead of loading them through
  `striatum.db`; the SQLite quarantine allowlist shrank accordingly.
- Legacy SQLite cutover guardrail tests now live with the legacy quarantine
  tripwires, so `tests/test_daemon_pg.py` no longer imports the retired Python
  daemon module.
- Mixed legacy modules now import neutral JSON/id/time/path helpers directly
  from `striatum.primitives` and `striatum.repo_policy`; an architecture
  guardrail prevents new `striatum.db` imports of substrate-neutral helpers.
- Runtime path and token-file helpers now live in `striatum.daemon_runtime`,
  and PostgreSQL repository registration helpers used by day-zero setup and
  daemon RPC routing now live in `striatum.daemon_pg.repositories`, reducing
  Python CLI/client imports of the legacy Python daemon module.
- The unused repo-local SQLite supervisor pointer helper
  (`striatum.daemon_supervisor.pointer`) was deleted; current supervisor
  pointer writes live under the daemon/PostgreSQL handlers.
- `striatum.daemon_supervisor.progress_watcher` no longer imports `sqlite3`;
  its optional connection is typed generically while the caller owns the
  legacy repo-local connection.
- Legacy corpus export helpers no longer import `sqlite3` or `striatum.db`;
  their caller supplies the connection while corpus-specific row lookup stays
  local to the compatibility exporter.
- Shared identity helpers no longer import `sqlite3`; the legacy
  session-lane attestation path accepts a generic row-capable connection while
  PostgreSQL code keeps using the substrate-neutral author/process helpers.
- The legacy `striatum.process_progress` SQLite wrapper was deleted; the
  retired Python-daemon sweep path no longer invokes repo-local supervised
  progress reconciliation, while shared progress-watcher coverage remains.
- The legacy SQLite `recovery watch` loop was deleted. The CLI-local watcher
  always runs the daemon-backed scheduler over `recovery.sweep`, including
  paired test-harness invocations.
- The dead legacy SQLite view-file breadcrumb reader was removed from the
  service fallback quarantine; `/view/...` no longer has a repo-local
  SQLite breadcrumb escape.
- Unused legacy Python-daemon `read_status` and `read_why` registry readers
  were deleted; status/why reads are owned by daemon RPC and PostgreSQL paths.
- The unused legacy Python-daemon `dashboard_all` repo-local fallback was
  deleted; daemon-global dashboard reads stay on the PostgreSQL client/admin
  and Go daemon paths.
- The legacy service artifact-row wrapper was removed; the remaining SQLite
  service fallback uses the shared web artifact row shaper directly.
- The web doctor page no longer has a legacy SQLite fallback; daemon doctor
  DTO errors fail closed as HTTP-shaped doctor page errors.
- The unused SQLite registry audit-segment rotation test helper was deleted
  from the legacy Python daemon module.
- Duplicate repo add/list/remove helpers and their legacy SQLite registry
  fallbacks were deleted from the legacy Python daemon module; repo
  registration now lives only on the PostgreSQL admin/repository helpers.
- Legacy Python-daemon global entry points (`status`, `stop`, `health`,
  `audit`, `sweep`, `doctor`, and foreground startup) no longer open the
  SQLite daemon registry; without a PostgreSQL daemon URL they fail closed
  before touching registry files.
- The now-unreachable SQLite daemon auth/audit/doctor helper island was
  removed from the legacy Python daemon module after the global fallbacks
  moved to PostgreSQL-only behavior.
- The obsolete standalone legacy-registry opt-in environment variable is no
  longer exported by daemon helper modules or surfaced in authority doctor
  diagnostics.
- The standalone `striatum.daemon_pg.sqlite_compat` helper was removed. Its
  last repository-identity calculation now lives beside the one-way
  repo-local migration fixture, and the unused daemon audit-chain validators
  are gone.
- D111 retires the operator-facing Python daemon selector. `striatum daemon
  start` always launches the Go daemon; `--core go` remains a deprecated
  no-op compatibility flag, while `--core python` and
  `STRIATUM_DAEMON_CORE=python` no longer select a Python daemon.
- The `striatumd` console script now targets a small Go-daemon launcher shim
  instead of importing the legacy Python daemon module; the old
  `striatumd --foreground` spelling is accepted as a compatibility alias.
- The multi-repo test harness no longer initializes participant repositories
  with repo-local SQLite. Participant prepare/start/cancel/checkpoint
  assertions now use daemon-owned PostgreSQL rows under `striatumd.*`.
- Packaged wheels now stage the Go daemon binary before build, and fresh-clone
  smoke builds `go/bin/striatumd` before the default daemon start path.
- Go PostgreSQL mutation paths now encode structured JSONB arguments through
  a shared pgx-safe helper, covering workflow snapshots, job definitions,
  queue messages, work packets, session capabilities, supervisor metadata,
  blockers, recovery cursors, and event payloads.
- The Go RPC envelope validator now matches the published daemon contract by
  accepting non-empty method strings, including contracted undotted reads such
  as `status` and `dashboard`.
- Release metadata checks now source both package name and version from
  `pyproject.toml`, avoiding false failures when an unrelated `striatum`
  distribution is installed beside `striatum-orchestrator`.
- Go now registers the canonical `recovery.auto_publish_stale_artifacts`
  method, keeps the deprecated `recovery.auto` alias on the same handler, and
  requires every auto-published file to match the expected byline.
- Go now owns the `recovery.auto_finalize` RPC handler as a dry-run-by-default
  projection with workflow-opt-in or forced live mode over stable expected
  artifact files.
- The first Go read-detail cluster is registered for `run.detail`,
  `job.detail`, `run.events`, `run.posture_verdicts`, `artifact.show`,
  `escalation.list`, `escalation.show`, and `escalation.resolve`, reducing
  missing contract handlers while keeping remaining web-context parity gaps
  visible.
- Go now owns `archive.create` for the V1 run archive bundle format, including
  safe repo-relative output paths, PostgreSQL run-scoped row export, and
  deterministic manifest/file hashes.
- Go `evidence.export` now writes the Markdown evidence file under the target
  repository and uses current PostgreSQL artifact/verdict column aliases; the
  same alias fix covers Go `run.summary` and `corpus.export`.
- Go now owns the read-only `worktree.list` handler over PostgreSQL
  `job_worktrees`, returning the Python-compatible `worktrees` row list with
  optional run filtering.
- Go now owns `worktree.create` and `worktree.release` over PostgreSQL
  worktree state, with repo-scope/lease/workflow validation, safe
  `.striatum/worktrees/` path confinement, and Git worktree add/remove calls
  performed directly by the Go daemon.
- Go now owns `work.send_message`, inserting completed agent messages and
  appending `message.sent` through the hash-chained PostgreSQL event helper.
- Go now owns `workflow.templates.list` and `workflow.templates.show` from an
  embedded copy of the workflow template catalog, with a drift test against
  the Python package-data catalog.
- Go now owns workflow file-authoring handlers: `workflow.validate`,
  `workflow.plan`, and `workflow.graph` validate repo-local workflow JSON and
  return plan/graph projections without mutating daemon state or opening
  SQLite.
- Go now owns workflow generation handlers: `workflow.generate.preview`
  produces read-only planned writes; `workflow.generate` and `workflow.init`
  write safe repo-relative scaffold files; `workflow.upgrade` uses
  PostgreSQL running-run checks and fails closed when PostgreSQL state is
  unknown, including `--add-phases` rewrites and
  `workflow.generate --shape multi_phase` V1.1 phase graph generation.
- Go `workflow.upgrade --add-phases` now matches the Python V1-to-V1.1
  phase-inference path for preview/apply, synthesis-job insertion,
  cross-phase edge rewriting, and non-terminal-run refusal.
- Web and chat workflow-generation preview now call
  `workflow.generate.preview` through daemon RPC in all modes; the old local
  in-process preview fallback was removed from service/chat fixtures.
- Production `cross-repo` CLI dispatch now refuses the remaining direct
  PostgreSQL fallback path if daemon RPC routing did not handle the command;
  the direct path is limited to the explicit legacy test-harness escape.
- Go cross-repo lifecycle reads now return typed `not_found` RPC errors for
  missing cross-repo run ids instead of leaking plain internal errors.
- Go daemon socket-level conformance now covers `cross_repo.cancel` against
  a live CORE=go Unix RPC daemon and PostgreSQL state, including mixed
  canceled/blocked participants, audit evidence, and JSONB-safe event payload
  insertion for pgx-backed mutation handlers.
- `striatum init --with-striatum-layout` now scaffolds the RFC 0056
  consumer-repo directories `striatum/workflows/` and
  `striatum/<workflow-slug>/` without writing workflow files or `.gitignore`
  policy. Day-zero docs, agent skill examples, and `adopt`'s suggested
  starter path now use the generated-tree form
  `striatum/workflows/<name>/workflow.json`.
- Go `daemon.key.rotate` now rotates a local Ed25519 sealed-apply signing
  key into the `0600` fallback key file, returns the new key id/public key
  metadata, and `daemon.hello` advertises the current public key when the
  fallback key is loadable. Malformed private fallback files are preserved as
  `.invalid.<timestamp>` backups during rotation; over-permissive key files
  still fail closed. Full apply-gate mutation and OS keyring custody remain
  deferred.
- Go now owns `supervise.status`, `supervise.list`, and
  `supervise.reattach_status` as read-only PostgreSQL projections. The status
  handler reports liveness, lane attestation, and stalled-supervisor fields
  without mutating pointer rows or draining helper events.
- Go now owns `supervise.start`, `supervise.send`, and `supervise.stop` over
  PostgreSQL supervisor rows and FIFO/helper transport. Sends preserve the
  delivered-unacknowledged contract, and stops update terminal supervisor state
  before signaling/removing control paths.
- Go now owns `daemon.migrate`, applying the embedded daemon PostgreSQL
  migrations without Python or SQLite.
- Go now owns daemon token lifecycle handlers:
  `daemon.token.create/revoke/rotate` write only daemon PostgreSQL client and
  capability rows, store HMAC-SHA256 token hashes, and return cleartext bearer
  tokens only at creation/rotation time.
- `apply.reviewed_patch` is no longer a production daemon RPC. The supported
  apply-adjacent surface is receipt read/verify plus daemon key rotation until
  a future sealed-apply decision reintroduces a mutation.
- Go now owns `repo.init` as PostgreSQL-backed repository initialization that
  creates only operational scratch and refuses repo-local SQLite state.
- The Go daemon handler-coverage ledger now reports zero generic
  `not_implemented` handlers for active contract methods; removed unsupported
  method names are expected to audit as `method_unknown`.
- Go now owns `run.graph` for JSON, Mermaid, DOT, and ASCII run graph
  projections from PostgreSQL workflow snapshots, materialized dependencies,
  latest job attempts, and review verdicts.
- Go `cross_repo.cancel` now calls the Go cross-repo lifecycle service and
  local run-cancel mutation instead of returning `not_implemented`, and now
  matches Python participant-cancel parity for terminal skips, preparing
  participants without local runs, inactive participant repositories, and
  persisted `blocked_errors`.
- Go now owns `repo.add`, `repo.list`, and `repo.remove` handlers over
  daemon-owned PostgreSQL, including SQLite-source refusal, operational
  scratch initialization, active-path conflict checks, and repo-scoped
  capability revocation on removal.
- Go now owns daemon-global `repo.resolve`, a read-capability bootstrap method
  that normalizes a repository path and returns active repository metadata
  without requiring CLI/web clients to open daemon PostgreSQL directly.
- The retired Python-daemon compatibility path also handles `repo.resolve`
  through PostgreSQL for legacy fixture coverage; production deployments use
  the Go daemon.
- Python CLI and service repository-scoped RPC routing now resolve repository
  ids through daemon RPC instead of importing daemon PostgreSQL connection
  helpers. Resolution errors fail closed rather than falling back to local
  state.
- Production Python daemon startup no longer opens the legacy SQLite daemon
  registry when PostgreSQL is configured. `connect_registry()` is explicitly
  gated to migration/test compatibility escapes, and startup uses PostgreSQL
  daemon metadata plus PostgreSQL sweep plumbing.
- `/v1/invoke` now sends daemon-mapped production reads and mutations through
  daemon RPC. The local `striatum.api.invoke` path remains available for
  explicit local/test surfaces and workflow authoring, not production run
  authority.
- Local MCP and web chat tools now use the same daemon-routing policy for
  mapped status, why, run lifecycle, artifact, review, and recovery commands;
  `striatum.api.invoke` remains only for unmapped local authoring and explicit
  fixture compatibility.
- Go `run.prepare` now loads workflow files through the Go workflow-authoring
  loader before writing rows, so repo-bound path checks and JSON-only workflow
  source validation are enforced in the Go daemon path.
- The SQLite-bound `dogfood.publish_on_behalf` and
  `dogfood.surgical_recovery` composites are removed from the production
  daemon contract in favor of primitive daemon methods until a
  PostgreSQL-native composite is designed.
- Production daemon MCP `tools/list` now hides local workflow-file authoring
  methods in both Python and Go; direct calls to removed dogfood composites
  now audit as `method_unknown`.
- SQLite registry-probe guardrails now classify every remaining direct
  `striatum.daemon.connect_registry()` caller and runtime-tripwire daemon MCP
  resource reads, so newly introduced daemon-global SQLite probes fail the
  architecture tests before they can become production fallback paths.
- `striatum daemon doctor --authority --json` now emits a cutover authority
  report covering PostgreSQL live-state authority, disabled legacy SQLite
  registry status, daemon method fallback counts, allowed migration/test-only
  SQLite exceptions, and remediation recommendations.
- The repository `/view/<path>` page no longer consults the legacy
  SQLite-backed run breadcrumb helper; file viewing stays a pure repository
  file read with no production SQLite touchpoint.
- Go now owns a read-only `dashboard.all` handler over daemon-owned
  PostgreSQL repositories. It reports per-repository status and stale-lease
  projections without opening SQLite; follow-up parity now also exposes
  per-active-run `run_progress` with phase progress, auto-finalize dry-run
  summary, and stalled-supervisor detail in both Go and Python/PostgreSQL
  dashboard-all projections.
- The compact terminal dashboard now renders single-run text frames from the
  daemon/PostgreSQL `dashboard` DTO in production. The old repo-local SQLite
  payload reader has been deleted, and paired test-harness assertions now use
  renderer fixtures; JSON `dashboard --run-id` and daemon-global
  `dashboard --all` remain RPC DTO surfaces.
- Go `status` now uses the PostgreSQL/Python read-model shape instead of raw
  row dumps: job counts by state, nested verdict counts by posture/verdict,
  queue-based claimable jobs, blocker/checkpoint payloads, run-scoped process
  health, supervisor stalls, phase/provenance fields, auto-finalize dry-run
  visibility, and deterministic `next_actions`.
- RFC 0058 V1.5 landed: `striatum operator current-brief` reads and validates
  the current operator brief without daemon RPC, and `operator_brief`
  `context_budget_lines` overruns are schema errors instead of warnings.
- Daemon diagnostics now fail closed without traceback leakage when the
  runtime PostgreSQL role cannot apply pending migrations, returning a
  structured `daemon status --json` error with the owner/admin repair hint.
  `daemon doctor --postgres-url` also threads that explicit URL into
  secondary daemon diagnostics instead of relying on env/config and risking an
  implicit legacy-registry probe.
- Daemon MCP `resources/list` and `resources/read` now use PostgreSQL-backed
  repository visibility, status, doctor, blocker, run, why, dashboard, and
  stale-lease projections whenever a daemon PostgreSQL connection is present;
  regression coverage runs those paths with the SQLite registry tripwire on.
  If the daemon MCP server is constructed without a PostgreSQL connection,
  resource list/read now fail closed before opening the legacy SQLite registry
  unless the paired legacy test-harness escape is active.
- `striatum daemon audit` now reads and authorizes against PostgreSQL when a
  daemon DB is configured, keeps the legacy audit output field names for CLI
  compatibility, and has SQLite-registry tripwire coverage for direct and
  dispatcher paths.
- `striatum daemon health` now uses PostgreSQL and appends to the PostgreSQL
  audit chain when a daemon DB is configured, avoiding the legacy registry
  probe while preserving the existing health JSON shape.
- `daemon doctor` no longer probes the legacy SQLite registry after a
  successful PostgreSQL doctor check. It reports the SQLite registry as
  post-cutover/unused, carries PostgreSQL-backed daemon diagnostics separately,
  and `read_doctor` uses PostgreSQL for global and repo-scoped diagnostics when
  a daemon DB is configured.
- `striatum daemon status` and `striatum daemon stop` now authorize and audit
  through PostgreSQL when a daemon DB is configured, preserving pidfile/runtime
  lifecycle behavior without opening the legacy registry.
- `supervise.status`, `doctor`, and `status` now surface stalled attached
  supervisors, and recovery sweep opens
  `heartbeat_stall_lease_expired` blockers when stalled leases expire.
- The historical three-lane design/build/review workflow fixture is now
  indexed under `examples/three-lane-design-build-review/` with graph and
  referenced-file regression coverage.
- The `/doctor` HTML page renders from daemon `doctor` in production while
  keeping the old fixture payload path quarantined for the subprocess test
  harness.
- PostgreSQL lane-liveness attestation now verifies the session/run binding,
  live PID identity, PID start-time token, and workflow snapshot lane command.
- The Postgres supervision handler suite now launches the real
  `go/bin/striatum-supervisor-helper` in a focused integration test and
  verifies start, send, packet acknowledgement, status drain, and agent-exit
  event ingestion across the Python/Go boundary. CI now promotes that check
  through a Linux/Postgres `daemon-go-helper-integration` target instead of
  relying on full-suite discovery.
- Go now owns the `supervise.report` mutation for direct wrapper control
  events and helper JSONL batches, recording supervisor heartbeat/exit state
  and hash-chained `supervisor.*` events without SQLite fallback.
- Existing supervisor paths now reconcile restart state before trusting an
  attached process: `supervise.status`, `supervise.send`, and claim-next
  auto-delivery record `supervisor.reattached` for surviving PID identity,
  fail closed for unverifiable repair states, and mark stale PID identity as
  `lost` before any packet write.
- The supervised-wrapper fixture suite now covers Claude, Codex, and Gemini
  wrappers, verifying multi-packet loops, inner-command failure isolation,
  clean EOF exits, temp scratch logging, and the non-interactive tool-approval
  flags that keep lanes from stalling on prompts.
- Chat transcript, briefing, session listing, display projection, and
  workflow-write confirmation helpers now live in `striatum.web.chat_session`
  with focused regression coverage.
- Web-only legacy SQLite fallbacks moved from the root service namespace into
  `striatum.legacy_sqlite.service`; the root `service_legacy.py` module is
  gone, and quarantine tests now assert that the primary service only loads
  the explicit legacy package through a lazy fallback boundary.
- The local service no longer eagerly imports the legacy `striatum.api`
  wrapper at module load; `/v1/invoke` keeps the compatibility wrapper but
  lazy-loads the legacy API only when that test-harness path is called.
- D108 resolves RFC 0071's authority-matrix generation question: the command
  authority matrix stays curated for authority/status classification, while
  architecture tests enforce generated CLI route labels and runtime CLI
  fallback cells.
- `striatum daemon doctor --repo <path> --authority --json` now mirrors the
  verify-only `striatum.repo_cutover_report.v1` inside doctor output and
  summarizes repository cutover health in `striatum.authority_report.v1`
  without opening SQLite.
- Static asset lookup and content-type mapping moved from `service.py` into
  `striatum.web.static_assets`, keeping HTTP response writing in the service
  handler while making the non-SQLite web split independently testable.
- Workflow editor file resolution, scaffold payloads, validation, atomic
  writes, and If-Match checks moved from `service.py` into
  `striatum.web.workflows`; the service handler now keeps only HTTP request
  parsing, template rendering, and JSON response mapping for those routes.
- Run-list presentation helpers for GitHub remote parsing, source-path
  normalization, workflow tree links, and state chips moved from `service.py`
  into `striatum.web.run_list`.
- Artifact web helpers for safe repo-relative path resolution, raw download
  content-type selection, and inline Markdown rendering moved from
  `service.py` into `striatum.web.artifacts`.
- The `/v1/invoke` read/mutation classifier moved from `service.py` into
  `striatum.service_command_policy`, keeping the legacy
  `striatum.service.is_read_command` import surface stable.
- Phase 7, Phase 8, and Phase 12 policy blockers are now explicit in the
  backlog/RFCs: accepted lint-risk persistence waits on a durable authority
  decision, global/default auto-finalize waits on live dogfood confidence plus
  a product decision, and Git/PR integration remains read-only-local-only until
  commit authority and hosted-provider boundaries are accepted.
- The repository file-view helpers for safe path validation, binary detection,
  text/Markdown payload shaping, and inline Markdown rendering moved from
  `service.py` into `striatum.web.view_file`; the service keeps the route,
  template rendering, and legacy breadcrumb injection.
- SSE replay offset parsing and event framing moved from `service.py` into
  `striatum.service_sse`, keeping the stream loop and daemon polling in the
  service handler.
- Local service process state, GitHub remote/default-branch caching,
  shutdown signaling, web-context secret generation, and per-run SSE slot
  accounting moved from `service.py` into `striatum.service_state`.
- Local service runtime helpers for version/mode reporting, loopback binding
  validation, PID-file single-instance checks, startup exceptions, and idle
  shutdown waiting moved from `service.py` into `striatum.service_runtime`.
- Web template environment construction and HTML escaping helpers moved from
  `service.py` into `striatum.web.template_env`, keeping the existing
  `striatum.service` private aliases stable for tests and route methods.
- Request authentication, bearer-token checks, same-origin mutation policy,
  and override-verdict web-context validation moved from `service.py` into
  `striatum.service_request_security` with pure decision helpers and focused
  CSRF/context-token tests.
- Workflow template listing/show and workflow generation preview/write response
  shaping moved from `service.py` into `striatum.web.workflow_generation`.
- Request-body parsing and JSON/HTML response helpers moved from `service.py`
  into `striatum.service_request_io`, keeping the handler wrappers stable.
- Daemon-backed run-event SSE streaming moved from `service.py` into
  `striatum.service_sse`, keeping the handler responsible for slot accounting
  and legacy fixture fallback selection.
- `recovery watch` is now a daemon-backed foreground scheduler over the
  canonical `recovery.sweep` RPC, with the broken `recovery.watch` method and
  CLI route removed from the shared contract, generated docs, Python registry,
  and Go registry.
- Documentation stale-state cleanup now records the shipped status for the
  workflow chooser, chat-assisted workflow scaffolding, escalation artifact
  schema/inbox, current-scope process supervision, and the RFC 0039 blocker.
- Doctor page DTO loading, legacy fallback selection, record recipe shaping,
  and problem grouping moved from `service.py` into `striatum.web.doctor`.
- Workflow browser index/detail page DTO shaping moved from `service.py` into
  `striatum.web.workflows`, keeping the handler responsible only for template
  rendering and HTTP error mapping.
- Chat index/session rendering, chat creation, provider send/tool loop,
  workflow-write confirmation, stop redirects, and transcript SSE tailing
  moved from `service.py` into `striatum.web.chat_routes`; service-private
  briefing and git-helper aliases remain stable.
- Run list/detail, job detail, artifact view, and posture-verdict page
  rendering moved from `service.py` into `striatum.web.run_pages`, leaving
  stable private handler wrappers for existing route tests and callers.
- Artifact raw download orchestration moved from `service.py` into
  `striatum.web.artifacts`, with the service wrapper still owning the HTTP
  handler entry point and response writer callbacks.
- Workflow run-now, branch-confirm, run cancel/pause/resume, and job
  cancel/retry route handling moved from `service.py` into
  `striatum.web.run_actions`, preserving the private service wrappers and
  legacy fixture fallback/error-mapping boundaries.
- Workflow browser and visual-editor route rendering/saving moved from
  `service.py` into `striatum.web.workflows`; the service now keeps only
  stable private wrappers and passes the existing template factory seam.
- Repository `/view` page rendering moved from `service.py` into
  `striatum.web.view_file`, with the legacy dogfood run-breadcrumb lookup
  injected through the service wrapper.
- JSON read helpers, repo-tree reads, daemon-read fallback handling, and
  run-event SSE route control moved from `service.py` into
  `striatum.service_api_routes`, preserving handler wrappers for direct tests.
- `cross-repo cancel` now routes through the daemon RPC contract to
  `cross_repo.cancel`, uses the PG-native participant-cancel runner, delegates
  each non-terminal participant to the daemon `run.cancel` handler, skips
  terminal or not-yet-local participants, and records blocked participant
  diagnostics in `last_reconcile_error`.
- PostgreSQL `recovery.sweep` now executes configured checkpoint-timeout
  escalation hooks (`marker_file`, `webhook`, `shell`) through the shared
  recovery hook dispatcher, keeps dry-runs side-effect-free, and folds hook
  failures into `escalations[]` instead of raising or reporting the old
  deferred placeholder.
- Local service GET/POST route selection moved from `service.py` into
  `striatum.service_routes`, keeping the handler's stable wrapper methods
  while continuing the daemon-first web-service split.
- Local service TCP/Unix binding, PID-file handling, signal shutdown, and
  serve loop orchestration moved from `service.py` into
  `striatum.service_server`; private compatibility wrappers remain in place.
- Workflow validation now rejects `needs_revision` cycles whose `from`/`to`
  jobs cross phase boundaries, closing the RFC 0045 V1.5 cycle phase-jump
  validator gap.
- Workflow validation now accepts canonical job `phase` fields from the React
  workflow editor, keeps `phase_id` as a compatibility alias, and rejects
  conflicting aliases.
- Explicit v1.1 phase arrays now require `phases[].synthesis_job_id` to
  point at the same phase's unique `phase_synthesis` job; generator, upgrade,
  fixtures, and phase-progress tests now emit the field.
- The React workflow editor now keeps missing/unknown phase jobs visible in an
  invalid phase bucket, removes the explicit-phase `(unset)` dropdown bypass,
  and defaults newly dropped jobs to the first declared phase.
- `dogfood.publish_on_behalf` mid-composite failures now report the failed
  step, partial composition steps, and nested specific error details through
  the helper result, rollback event, daemon RPC error, and MCP
  `structuredContent`.
- Archive create/verify replay now covers archived command request,
  process-supervisor, and process-supervisor-pointer rows, and replay
  verification rejects duplicate or missing ids for those rows plus archived
  verdict, blocker, process-execution, and job-worktree rows.
- Roadmap kickoff status and remediation sequencing notes were refreshed to
  match the post-v1.55.0 daemon-first architecture work and the current
  blocked-policy boundaries.
- Current docs, RFC status notes, reusable prompts, and root reference
  artifacts were swept for stale substrate/runtime guidance: daemon-owned
  PostgreSQL is now the live-state authority, `.striatum/` is operational
  scratch, RFC 0048 is marked completed, and Engram is framed only as optional
  external augmentation.
- Daemon-routed CLI commands now fail closed on unexpected route-layer
  exceptions instead of falling through to the legacy dispatch body; an
  architecture guardrail keeps the SQLite-connect tripwire armed for that
  path.
- Backlog records for the same-model validator rule and real UI bundle /
  supply-chain polish were closed after verifying the current validator,
  package, bundle, and guardrail tests cover the formerly open work.
- Job-detail page DTO shaping moved from `service.py` into
  `striatum.web.job_detail`, leaving the route handler responsible for daemon
  RPC/fallback, template selection, and HTTP error mapping.
- `repo.add`, `repo.list`, and `repo.remove` now route through daemon RPC and
  operate directly on daemon-owned Postgres registration rows. `repo add
  --init` creates only `.striatum/` operational scratch and no
  `.striatum/retired-local-state`; existing repo-local SQLite sources must be
  archived/removed before registration. The legacy importer is fixture-only.
- Production `striatum init` and `striatum adopt` now use the same
  scratch-only bootstrap and no longer create repo-local SQLite, including
  `init --with-skills` in paired test-harness mode.
- `adapter run` is retired outside that same legacy fixture escape, closing
  another production path to the repo-local SQLite process-adapter tables.
- The legacy `byline` helper and `inbox --session-id` packet helper are also
  retired outside fixtures; production clients should use daemon read
  surfaces.
- The legacy SQLite daemon registry compatibility escape now requires the
  paired test-harness markers; setting only
  `STRIATUM_ALLOW_LEGACY_SQLITE_REGISTRY=1` no longer reopens production
  registry access.
- `workflow upgrade` now fails closed instead of falling back to repo-local
  SQLite running-run checks; unknown PostgreSQL state is a refusal even when
  legacy SQLite files are present.
- RFC 0058 V1 now has publisher-visible operator artifact kinds
  (`operator_brief`, `work_plan`, `progress_note`, `operator_report`),
  corpus metadata columns for operator docs, and a seeded
  `docs/operator/` current-state surface that supersedes ad-hoc handoffs.
- `daemon migrate-repo-local --verify-cutover --json` now emits
  `striatum.repo_cutover_report.v1` using PostgreSQL queries plus raw
  source/tombstone/sentinel file checks, without opening SQLite as a database.
- Fresh-clone and package smoke scripts now exercise only the daemon/Postgres
  repo registration path. If PostgreSQL setup is unavailable they skip with a
  clear prerequisite message instead of falling back to repo-local SQLite
  test-harness mode; the scripts still keep their smoke workflow inside the
  target repository for `run prepare`, install the packaged RPC method
  contract into wheels, and use the current `striatum-orchestrator`
  distribution artifact names.
- Artifact view template-context shaping, byline display, recorded
  attestation chips, lane-evidence chips, and expected-artifact row shaping
  moved into `striatum.web.artifacts`; the daemon-backed artifact page no
  longer reaches into the legacy SQLite fallback module for pure
  presentation shaping.
- Run posture-verdict template-context shaping moved into
  `striatum.web.run_posture_verdicts`; the service route keeps daemon
  RPC/fallback and HTTP error mapping while posture DTO validation and
  verdict-row filtering live in web presentation code.
- Current docs were swept again for stale routing/runtime language after
  PG-native repo registration: quick-start docs now favor `adopt` or
  `repo add --init`, `dashboard --all` is described as daemon/Postgres-backed,
  Pattern 5 in the harness friction notes is marked historical/resolved for
  daemon-routed command and post-tombstone init slices, and evidence exports
  no longer imply `.striatum/` SQLite is live state.

- **Command authority and fallback guardrails.**
  `docs/architecture/COMMAND_AUTHORITY_MATRIX.md` now names the authority
  owner for daemon RPC, CLI translation, Python PG handlers, Go helper
  registrations, and remaining SQLite quarantine paths. Guardrail tests
  keep daemon registry methods classified, prevent new CLI fallback routes
  from appearing silently, and tripwire representative production commands
  against direct SQLite opens.
- **Single daemon method contract source.**
  `contracts/daemon_methods.json` drives the Python compatibility registry,
  `daemon.describe`, generated Go registry metadata, MCP tool descriptors,
  generated architecture reference tables for daemon methods and CLI route
  translation, runtime CLI route lookup through the declarative `cli_routes`
  map, and contract parity tests. Workflow authoring remains explicitly
  CLI-local.
- **Go production-daemon strategy.**
  D107 supersedes D105 and restores the Go production-daemon port as the
  active architecture target. D109 made Go the default daemon core, and D111
  retires the Python daemon selector while leaving the Python CLI/web layers
  as daemon clients.
- **Daemon-first web service.**
  The local web service now uses daemon RPC for run cancel/pause/resume, job
  cancel/retry, branch confirm, workflow run-now lifecycles, run listing,
  chat briefing active-run
  summaries, the JSON read endpoints for status/doctor/why/dashboard/run
  artifacts, the artifact detail page, and the posture-verdict drill-down
  page. The run detail page now renders from daemon `run.detail` in
  production, keeping SVG/HTML rendering local while moving page state to a
  read DTO. The job detail page now renders from daemon `job.detail`,
  including expected artifacts, process evidence, and verdict override
  context. Run-now now calls daemon `run.prepare`, `branch.confirm`, and
  `run.start` in production, preserving the historical 422 field-level
  workflow validation response through daemon RPC error details. The new
  `run.posture_verdicts` daemon DTO backs the posture page,
  while `artifact.show` can now include run, expected-author, and provenance
  context for the artifact page. Legacy CLI/SQLite fallbacks are retained
  only for the subprocess test-harness escape. The `/v1/invoke`
  mutation gate now classifies daemon-routed commands from
  `METHOD_REGISTRY.required_capability`, with only CLI-local workflow
  authoring reads kept in an explicit service list. Production service
  startup now checks daemon/repository health through daemon `doctor` before
  binding; the old SQLite integrity check is limited to the subprocess
  compatibility harness. The `/doctor` HTML page now renders from daemon
  `doctor` in production while retaining per-record recovery recipes and a
  test-harness-only legacy fixture fallback. The web SSE event stream now
  polls daemon `run.events` in production, with the old SQLite event tail
  kept only for the same subprocess harness. As the first behavior-preserving split,
  pure HTTP/security helpers moved from `service.py` into
  `service_http.py` while keeping the existing `striatum.service` imports
  stable. Chat transcript projection, briefing, JSONL append, timestamp,
  stable-hash, safe-git, multipart, session path/listing, display-message,
  and workflow-write confirmation helpers now live in
  `striatum.web.chat_session`, leaving `service.py` focused on HTTP routing,
  provider streaming, and response handling.
  The gated subprocess-fixture mutation fallbacks and legacy error mappers
  now live in `striatum.legacy_sqlite.service`. The remaining legacy
  page-read payload builders, view-file breadcrumb lookup, doctor-page
  fixture payload, SSE event tail, and legacy startup integrity check are now
  quarantined there as well. `service.py` no longer imports or opens
  repo-local SQLite directly, and importing the primary service no longer
  eagerly imports the legacy SQLite fallback module. Static asset lookup and
  MIME selection now live in `striatum.web.static_assets`, with service-level
  response writing kept unchanged. Workflow editor file resolution,
  new-workflow scaffolding, validation, atomic writes, and If-Match handling
  now live in `striatum.web.workflows`, while service-level route methods keep
  the HTTP request/response boundary. Run-list presentation helpers for
  GitHub remote parsing, workflow source-path normalization, tree-link
  construction, and state chips now live in `striatum.web.run_list`. Artifact
  path validation, raw download content-type selection, and inline Markdown
  rendering now live in `striatum.web.artifacts`. The `/v1/invoke`
  read/mutation classifier now lives in `striatum.service_command_policy`,
  keeping the service route focused on request validation and dispatch.
  Repository file-view path validation and content payload shaping now live in
  `striatum.web.view_file`; `service.py` keeps route-level rendering and the
  legacy run-breadcrumb fallback injection. SSE replay offset parsing and event
  framing now live in `striatum.service_sse`. Local service process state and
  per-run SSE slot accounting now live in `striatum.service_state`. Service
  runtime helpers now live in `striatum.service_runtime`, and template
  environment helpers now live in `striatum.web.template_env`. Request
  security policy now lives in `striatum.service_request_security`. Workflow
  generation endpoint response helpers now live in
  `striatum.web.workflow_generation`. Request-body parsing plus JSON/HTML
  response helpers now live in `striatum.service_request_io`. Doctor page
  response shaping now lives in `striatum.web.doctor`.
- **Escalation inbox foundation.**
  `escalation.list`, `escalation.show`, and `escalation.resolve` project
  human-principal escalations from blocker state. The `escalation` artifact
  kind, `striatum.escalation.v1` front matter schema, CLI routes, daemon
  contract entries, and artifact-to-blocker linkage are in place. Escalation
  projections now suppress stale artifact links unless they match a real
  artifact row by id, path, and content hash; idempotent artifact publish
  retries repair missing links and reject conflicting blocker metadata.
- **Supervisor control channel.**
  Supervision now records structured control events through
  `supervise.report`, reports delivered-unacknowledged sends explicitly, and
  includes a standalone Go `striatum-supervisor-helper` that launches agents
  under PTY while emitting JSONL control events without importing domain DB
  or RPC code. Lanes can now opt in to `supervision.transport: "pty_helper"`,
  letting `supervise.start` launch the helper, persist pointer metadata, and
  ingest helper JSONL acknowledgements through the existing control-event
  path. Pipe transport also has an explicit
  `supervision.stdin_delivery: "one_shot_eof"` opt-in for single-prompt
  commands such as `cmd -`; default supervised lanes continue to use the
  persistent FIFO contract. The daemon now implements
  `supervise.reattach_status` as a read-only supervisor health DTO, and
  `doctor` surfaces non-healthy reattach states for stale supervisors without
  mutating runner state. Recovery sweep now owns attached-supervisor
  heartbeat-stall detection: `supervise.status` can report
  `liveness: "stalled"` with `last_progress_age_seconds`, `doctor` and
  `status` surface stale attached supervisors, and expired stalled leases
  become open `heartbeat_stall_lease_expired` blockers without auto-killing
  the OS process. PostgreSQL lane-liveness attestation now matches the
  stricter legacy semantics: attached supervisor rows attest only when the
  session/run binding, live PID, PID start-time token, and workflow snapshot
  lane command all match. The Postgres supervision handler tests now include
  a focused integration case that launches the built Go helper and verifies
  helper event ingestion across `supervise.start`, `supervise.send`, and
  `supervise.status`; CI now runs that case explicitly through
  `make daemon-go-helper-integration` on Linux runners with Postgres.
  Reattach/lost-state reconciliation now runs on existing status, send, and
  claim auto-delivery paths, updating daemon-instance metadata for surviving
  supervisors and marking stale PID identity lost before delivery. The
  supervised-wrapper fixture suite now exercises
  `.striatum/bin/{claude,codex,gemini}-supervised-wrapper.sh` with provider
  stubs, pinning the persistent FIFO loop contract and the auth-bypass flags
  required for non-interactive lane operation.
- **Workflow risk lint.**
  `striatum workflow lint` supports structured warnings, opt-in strict mode,
  accepted-risk rationale and decision references, advisory coverage scoring,
  service/API surfacing, workflow browser warnings, and generator preview
  summaries. `workflow validate` now refuses same-model
  implementer/reviewer pairings by default when the existing lint rules find
  them, with `--allow-same-model-pairing` as the explicit override.
- **Runner-owned workflow fixture cleanup.**
  The historical P001 three-lane design/build/review shape is now indexed as
  `examples/three-lane-design-build-review/`, covered by a regression test
  for its graph and referenced files, and marked complete in the TODO and
  roadmap trackers.
- **Auto-finalize, archive, replay, packaging, and setup slices.**
  `recovery.auto_finalize` landed as a daemon/Postgres recovery method with
  dry-run and opt-in live modes, status/dashboard preview surfacing, and
  auto-from-artifact provenance. Recovery sweep acceptance coverage now pins
  a dogfood-shaped run where three valid written review findings auto-finalize
  without operator-on-behalf or override provenance. Run archive and corpus
  verification foundations, archive replay event-row hash recomputation,
  frontend bundle integrity checks, and day-zero setup docs were advanced as
  part of the same remediation sequence.
- **Redaction hardening.**
  Evidence redaction now treats `safe` policy entries as scalar-only, so
  injected objects/lists in otherwise safe fields are replaced with the
  redaction placeholder. Corpus source-path deny checks are
  case-insensitive for transcript/output/private path shapes, with synthetic
  injection coverage for workflow/job prompts, verdict rationales, blocker
  text, transcript-like fields, nested payloads, and path hygiene.
- **Operator terminology cleanup.**
  Reader-facing docs, CLI help, scaffold text, workflow templates, and
  recovery skill templates now use principal/operator vocabulary where the
  product means a user decision or operational action, while leaving durable
  schema and event identifiers unchanged.
- **CI portability.**
  The multi-repo harness CI install now includes the `daemon-pg` extra before
  running Postgres-backed tests, and Go supervisor process-launch tests resolve
  `true`/`cat` from `PATH` instead of assuming Linux-style absolute paths.
  The Makefile's Postgres-backed test targets now install the same extra into
  the project `.venv` before invoking the harness.

## v1.55.0 — 2026-05-15

### RFC 0048 V1.5 hardening + Schema v6

The substrate flip from RFC 0043 V1.6 → RFC 0048 Phase A/B/C is now
hardened end-to-end:

- **F2 — capability-denial test matrix.**
  `tests/daemon_pg/test_capability_denial_matrix.py` parametrizes every
  PG-backed RPC method × five denial reasons (token_missing,
  capability_missing, token_revoked, token_expired,
  capability_scope_mismatch). 70 deny cases lock the fail-closed
  routing-rule for the ported handler set. Plus an audit-row append
  assertion for the deny path.
- **F3 — audit-chain row-lock.**
  `src/striatum/daemon_rpc/request_log.py::append_audit_row` now
  `SELECT … FROM striatumd.audit_chain_head … FOR UPDATE` inside an
  explicit `conn.transaction()` so concurrent appenders serialize on
  the singleton head row. Without it, two transactions could compute
  `row_hash` over the same `previous_hash` and fork the chain.
  `tests/daemon_pg/test_audit_chain_concurrency.py` verifies a contiguous
  chain across 12 simultaneous denied requests.
- **F4 — append-only role-grant tests.**
  `tests/daemon_pg/test_append_only_role_grants.py` asserts the
  `striatumd_rw` role lacks UPDATE/DELETE on `striatumd.events` and
  `striatumd.artifacts` (migration 0005 REVOKE) while retaining
  UPDATE/DELETE on transient state tables. End-to-end SQLSTATE 42501
  checks gated on TCP auth (peer-auth setups skip).
- **HIGH#1 — parity rig.**
  `tests/daemon_pg/handlers/_parity.py` provides `assert_payload_parity`
  (recursive dict/list diff with ignore-keys for timestamps/UUIDs).
  Removed the historical `_stub_missing_workflow_loop_modules` workaround
  from `tests/daemon_pg/handlers/recovery_evidence/conftest.py` and
  `_helpers.py` (Track A landed in v1.49.0, stubs are dead). Removed
  the `RFC0048_PARITY` env-var skipif from `test_stale_leases` and
  `test_requeue_stale` so PG-handler invocations run by default. Full
  per-handler byte-equivalent fixture seeding (16 handlers) tracked
  as follow-up.
- **HIGH#2 — inline-helper wiring.**
  `src/striatum/daemon_pg/handlers/workflow_loop/complete_job.py`
  exports `complete_inline(...)` and `ack_work.py` exports
  `ack_inline(...)`. `recovery.resume --complete` (`resume_blocker`) and
  `recovery.auto` live mode (`auto_publish_stale_artifacts`) no longer
  raise ImportError on the inline-helper imports.
- **Schema v6 — migration 0006.**
  `striatumd.events` gains dedicated `previous_hash` (nullable) and
  `row_hash` columns plus a `striatumd.repo_event_chain_heads`
  singleton-per-repository pointer. The migration backfills both
  columns from `payload_json._event_chain` and strips that key from
  payload_json on existing rows; refuses to migrate if any row lacks
  anchor metadata. `RepoHandlerContext.append_event` (Python) and
  `pkg/mutations.insertEvent` (Go) read the chain head with `FOR
  UPDATE`, write the columns directly, and upsert the head pointer —
  serializing concurrent appenders per-repository on the parent
  `striatumd.repositories` row.
- **CLI dispatch fail-closed.**
  Earlier commits also flipped the CLI dispatch hook so mapped daemon
  RPC verbs no longer fall back to SQLite when the daemon is
  unreachable or the target repository is not registered (`src/striatum/
  cli/daemon_rpc_route.py`). The `daemon doctor`'s legacy SQLite
  registry probe is surfaced as
  `{"status": "post_pg_cutover_unused", …}` rather than a scary
  `token_invalid` error.

### Pre-flight cleanup (aaf5d3c)

- `daemon doctor`'s SQLite-registry probe now reports
  `post_pg_cutover_unused` when PG is the authoritative auth surface.
- `.gitignore`: `build/` un-commented (the tracked dogfood-043
  HANDOFFs remain un-ignored via existing `!…/HANDOFF.md` exceptions).
- `docs/handoffs/2026-05-15-rfc-0048-postgres-transition.md`: `daemon
  doctor --explain --json` jq path corrected to `.data.explain`.

## v1.54.0 — 2026-05-15

### RFC 0048 Phase B (read surface) — Go-core parity for the 12 read CLI verbs

The Go daemon previously registered every single-repo handler as
`notImplementedHandler` (codex F2 finding from dogfood-049). Phase B
ports the read-surface handlers — same shape as Python's
`src/striatum/daemon_pg/handlers/reads/`, same return-shape parity
contract so CLI + operator UI don't detect the substrate-language flip.

New `go/pkg/reads/` package:

- `reads.go` — shared helpers: `Queryer` interface (narrowed
  `pgx.Rows` access), `collectRows` for generic `map[string]any`
  result sets, `requireRepositoryID`, parameter helpers.
- `status.go` — `HandleStatus`: runs/jobs/sessions/verdicts/blockers
  scoped by repository_id + optional run_id; computes claimable +
  blocked_downstream + next_actions; returns the legacy JSON shape.
- `dashboard.go` — `HandleDashboard`: jobs_by_state / verdicts_by_state /
  blockers / sessions / last-10 events. Defaults to the most recent run
  when no run_id supplied (parity with Python).
- `doctor.go` — `HandleDoctor`: schema_version + stale-lease +
  waiting-human counts + problems list.
- `why.go` — `HandleWhy`: events touching a target_id across job/session/
  run/message/lease/payload-json columns.
- `listings.go` — `HandleListRuns` / `HandleListSessions` /
  `HandleListJobs` / `HandleListArtifacts` / `HandleListWorkflows`. Each
  accepts state/role/lane/workflow_job_id/kind filters (matches the
  Python translator's parameter propagation) with bounded `limit`
  (max 1000, default 200-500 per-method).
- `exports.go` — `HandleRunSummary` (run row + jobs + artifacts +
  verdicts + doctor block via `HandleDoctor` for parity), `HandleEvidenceExport`
  (scoped artifacts + verdicts + doctor), `HandleCorpusExport`
  (corpus_contract_version=1 manifest + paged artifact rows).

`go/cmd/striatumd/main.go` calls `reads.Register(server, runner)` before
the not-implemented stub loop. The for-loop's
`if _, exists := server.Handlers[method]; exists { continue }` then
skips these methods, so existing fallbacks remain for unported
mutations. Snapshot: ~12 fewer "not_implemented" methods.

`go build ./... && go vet ./...` clean. Read handlers integrate with
the existing `PostgresAuthorizer` + `AuditRecorder` so capability
checks + audit chain semantics are unchanged from cross_repo handlers
(no per-handler auth shim required).

### Companion: Python GH #19 PG-side message parity

`src/striatum/daemon_pg/handlers/recovery_evidence/requeue_stale.py`
and `tests/test_cli_mvp.py` updated to point operators to the new
`--force --justification "<reason>"` flag that shipped in v1.53.0. The
SQLite-backed path's message was already updated in v1.53.0; this
brings the PG-backed handler's message + the integration test in line.

### Outstanding Phase B (still deferred)

- Write-surface Go ports — 16 mutation handlers (session.register,
  claim_next, ack_work, complete_job, release_lease, block_job,
  record_verdict, submit_review, override_review_verdict +
  recovery.\* + evidence.\*). Each requires transaction + audit-chain
  append, materially more complex than reads. Tracked as next
  Phase B milestone.
- Cross-implementation parity tests (`make test-multi-repo CORE=go`
  byte-identical state assertion). Land after writes port.
- RFC 0048 Phase C SQLite-removal default flip — gated on
  Phase B mutations + the V1.5 fix-up items (codex F2-F4 + claude
  HIGH#1/#2 + schema migration 0006).

## v1.53.0 — 2026-05-15

### GH #19 — recovery requeue-stale --force --justification

`striatum recovery requeue-stale` now accepts `--force --justification
"<reason>"` to override the `repo-write stale jobs require manual
inspection` refusal after the operator has inspected the on-disk
artifact and decided requeue is appropriate. The override is
audit-chained: the resulting `recovery.stale_requeued` event payload
gets `operator_override=true` and `justification=<reason>` fields so
future audits can replay the decision.

Without `--force --justification`, the original refusal still fires
(regression guard).

### GH #21 — serve refuses to start over a corrupted retired-local-state

Adds `_verify_state_health(repo)` to the `striatum serve` startup path
(both TCP and Unix transports). Before binding any socket, the function:

- Refuses to open if `retired-local-state` exists but cannot be opened by
  `sqlite3.connect`.
- Runs `PRAGMA integrity_check`; if the result isn't `ok`, raises
  `ServiceConfigError` naming the file + remediation (quarantine to
  `.corrupt`, run `striatum init`, retry).
- Runs `PRAGMA wal_checkpoint(TRUNCATE)` on the existing DB so any
  pending WAL is flushed to the main file before the new serve takes
  the write lock. This closes the failure mode observed 3 times in one
  session: SIGKILL on the previous serve left WAL in an inconsistent
  state; SQLite recovery on the new serve truncated to the last
  checkpoint, losing MB-scale active-run rows down to KB-scale.

### RFC 0048 V1.5 — daemon doctor --explain

New `--explain` flag on `striatum daemon doctor` adds a per-method
table to the doctor output. Each row reports:

- `method` — RPC method name (from `striatum.daemon_rpc.registry`).
- `pg_backed` — whether `resolve_pg_handler(method)` returns a handler
  (true = ported in Phase A or Phase C; false = still falls through).
- `sqlite_fallback_route` — the legacy CLI route in `CLI_ROUTES`, if
  any. Methods with no fallback route are PG-only.
- `required_capability` / `repository_scope` / `deprecated` — registry
  metadata.

Plus summary: `method_count`, `pg_backed_count`.

Current snapshot post-v1.52.0: 93 methods / 34 PG-backed / 68
SQLite-fallback-routed.

### Outstanding RFC 0048 V1.5 items (still deferred)

- codex F2 capability-denial test matrix (16 handlers × 6 denial cases).
- codex F3 audit-chain SERIALIZABLE/row-lock per write handler.
- codex F4 append-only role-grant tests at the daemon-pg layer.
- claude HIGH#1 byte-equivalence parity rig wired into all 16+ tests
  (the `ReadSeed` / `pg_ctx` / `sqlite_conn` / `assert_payload_parity`
  helpers).
- claude HIGH#2 dead code paths (`complete_inline`, `ack_inline`,
  `recovery.resume --complete`, `recovery.auto` live mode).
- Schema migration 0006 (`events.previous_hash` / `row_hash` columns).
- RFC 0048 Phase B (Go core parity).
- RFC 0048 Phase C SQLite-removal default flip.

## v1.52.0 — 2026-05-15

### RFC 0048 Phase C complete — read-surface PG handlers (dogfood-060)

Closes the substrate flip. All 12 read-surface CLI verbs now have
native PG-backed handlers under `src/striatum/daemon_pg/handlers/reads/`
and route through the daemon RPC instead of falling through
`CLI_ROUTES` → `invoke()` → repo-local SQLite. After
`daemon migrate-repo-local`, the `STRIATUM_DAEMON_REQUIRED=0
STRIATUM_TEST_HARNESS=1` escape is no longer required for the read
verbs.

Ported handlers (12):

- `status`, `dashboard`, `why`, `doctor` — core operator reads.
- `list.runs`, `list.sessions`, `list.jobs`, `list.artifacts`,
  `list.workflows` — listing reads.
- `run.summary`, `evidence.export`, `corpus.export` — reporting /
  export reads.

Each handler:

- Registered via `@register_pg_handler("<method>", read_only=True)`
  decorator from the Phase A registry.
- Scopes by `ctx.repository_id` on every SELECT (no cross-repo leakage).
- Returns the same top-level JSON shape as the legacy SQLite-backed
  function (parity contract — CLI and operator UI don't detect the
  substrate flip).

Implementation supports + plumbing:

- New `_read_model.py`, `_registry.py`, `_sql.py` shared infrastructure
  under `daemon_pg/handlers/reads/`.
- `daemon_pg/handlers/__init__.py` imports the `reads` subpackage so
  decorator registrations fire on `import striatum.daemon_pg.handlers`.
- `cli/daemon_rpc_route.py` translator updates: `list.*` filters
  (state/role/lane/workflow_job_id/kind/limit) now propagate to RPC
  params; added missing `("corpus", "export")` lookup entry.
- `corpus/redaction.py` extended to redact artifact/session prose
  before rendering corpus run-summary rows (closes a build-review
  finding about evidence leak).
- `status` handler uses the legacy operator action vocabulary from
  `cli/introspect.py:857` (closes a build-review finding about
  dashboard/web-UI parity).
- `run.summary` + `evidence.export` call the real PG `doctor` handler
  instead of hardcoding `{"ok": true, "schema_version": 5}` (closes a
  build-review finding about always-green doctor in post-migration
  exports).

Tests:

- 12 handler test files under `tests/daemon_pg/handlers/reads/` plus
  shared `read_handler_fixtures.py` for cross-suite reuse.
- `tests/test_cli_daemon_rpc_route.py` covers the translator
  parameter-propagation and corpus-export wiring.
- `tests/test_corpus_redaction.py` covers the redaction additions.
- Full target test sweep: 83 passed, 5 skipped (gated multi-repo PG
  fixtures).

### Operator-driven completion note

The dogfood-060 workflow ran the design + synth + review_design phases
and the first build review. The build review verdicts (codex
threat_model: needs_revision on missing handler-level threat-model
evidence; claude ergonomics_dx: needs_revision on parity-rig absence +
CLI translator drops + next_actions divergence + hardcoded doctor +
missing corpus-export route) named the revision punch list precisely.
The operator addressed all findings directly rather than restarting the
workflow loop, because the build-review report itself was the
implementer spec.

The structural gaps that made the workflow loop expensive — GH #19
(stale-lease recovery for repo_write jobs) and GH #21 (serve restart
clobbers retired-local-state) — are tracked separately and remain V1.6
follow-up scope.

### Outstanding follow-ups (deferred)

- GH #19 stale-lease operator recovery path.
- GH #21 serve startup must not clobber active state.
- RFC 0048 V1.5 fix-up items: codex F2 capability-denial test matrix,
  F3 audit-chain SERIALIZABLE/row-lock per handler, F4 append-only
  role-grant tests, claude HIGH#1 byte-equivalence parity rig (the
  one named in dogfood-057's reviews — still not wired), HIGH#2 dead
  code cleanup, schema migration 0006 (events.previous_hash /
  row_hash columns), `daemon doctor --explain`.
- RFC 0048 Phase B (Go core parity) — multi-week.
- RFC 0048 Phase C SQLite-removal flip (the actual default switch
  away from CLI_ROUTES fallback) — pending V1.6 fix-up landing.

## v1.51.0 — 2026-05-14

### RFC 0048 Phase C (partial) — CLI dispatch routes through daemon RPC

Lands the substrate-flip plumbing for CLI verbs. The dispatch hook now
checks the daemon socket and routes any verb mapped in the new
``daemon_rpc_route`` lookup through ``DaemonRpcRouter`` over Unix
socket instead of running in-process against SQLite. Falls through to
legacy SQLite when the daemon is offline, the verb is bootstrap-only
(``init``, ``skills``, ``plugin``, ``daemon``, ``repo``, ``cross-repo``,
``serve``, ``byline``, ``inbox``), or ``STRIATUM_TEST_HARNESS=1``.

New module ``src/striatum/cli/daemon_rpc_route.py`` with translators
for status / why / doctor / dashboard / list / run.\* / register-session /
claim-next / ack / heartbeat / release / block / complete /
publish-artifact / verdict / submit-review / override-verdict /
recovery.\* / evidence.export / decision.record / checkpoint.resolve /
branch.confirm. Each translator builds the RPC envelope (with capability
token loaded from ``read_runtime_token()``) and the dispatch hook calls
``daemon_rpc.client.call_unix`` with the daemon's Unix-socket handshake.

Plumbing fixes:

- ``run_daemon_foreground`` always resolves ``daemon.toml`` via
  ``daemon_pg.config.resolve_config`` (the v1.50 implementation only
  fired the PG path when the env var was set — systemd-launched daemons
  silently came up SQLite-only).
- ``run_daemon_foreground`` now bootstraps an admin client into
  ``striatumd.clients`` on first start and writes the runtime token to
  ``runtime_dir() / 'client-token'``. Mirrors the SQLite ``clients``
  bootstrap but targets the Postgres-side table that ``authorize()`` reads.
- Daemon PG connection sets ``row_factory = psycopg.rows.dict_row`` so
  ``authorize()._row_dict`` works on per-cursor results.
- ``daemon_rpc.request_log.append_audit_row`` made compatible with both
  ``tuple_row`` and ``dict_row`` factories (the codebase mixed both).
- ``DaemonRpcRouter._repo_root_for`` no longer rejects requests whose
  registered repo_root differs from the router's startup CWD — the
  daemon serves every registered repository per RFC 0043 §3, not just
  the one it was launched from.
- ``daemon_rpc.envelope.RpcEnvelope.from_mapping`` no longer requires
  dotted method names (matches the in-process ``mcp_dispatch`` behavior;
  the registry has both dotted and undotted methods).
- ``DaemonRpcRouter._route``'s CLI_ROUTES fallback sets
  ``STRIATUM_IN_DAEMON_HANDLER=1`` around ``invoke()`` so the CLI's
  Phase C hook short-circuits and doesn't re-route through the daemon
  recursively.

### systemd user unit

``~/.config/systemd/user/striatumd.service`` ships as the supported
launch path. ``systemctl --user enable --now striatumd.service`` brings
the daemon up; daemon.toml + ~/.local/bin/striatum on PATH supply the
rest. Restart on failure with a 5-second backoff.

### Operator-mode update CLI

``pip install -e . --force-reinstall --user --break-system-packages``
brings the locally-installed ``striatum`` console script forward
between minor bumps when the editable install metadata lags. RFC 0048
V1.5 follow-up will add ``striatum self-update`` as the documented
operator wrapper.

### Phase C remaining (deferred to V1.6 / dogfood-060)

The mutation surface (16 PG handlers from RFC 0048 V1 Phase A) routes
end-to-end via the new Phase C hook + daemon RPC. The read surface
(status, dashboard, list.\*, run.summary, why, doctor, evidence.export,
corpus.export) still falls through ``CLI_ROUTES`` in the daemon to
``invoke()`` which uses repo-local SQLite. After
``daemon migrate-repo-local`` finalizes the SQLite as a tombstone,
those read verbs return exit 3 (``state is not initialized``). To make
the substrate flip complete, RFC 0048 needs PG handlers for the read
verbs too. Captured in OPERATOR_REPORT.md for the next dogfood.

For now: operators run with ``STRIATUM_DAEMON_REQUIRED=0
STRIATUM_TEST_HARNESS=1`` for un-migrated repos; migrated repos can
use the mutation verbs through daemon RPC but cannot inspect state
until read handlers land.

## v1.50.0 — 2026-05-14

### RFC 0048 V1.5 — Daemon Unix-socket accept loop + role-provisioning runbook

Closes the V1.5 migration-blocking gap from dogfood-057's V1 Phase A. The
RFC's V1 Phase A landed PG-backed handlers and a router with PG-vs-CLI
delegation; what made the daemon-required CLI non-functional was that
`run_daemon_foreground` bound a Unix socket and listened, but never
called `accept()`. So `striatum status` (and every other daemon-required
verb) refused with exit 11 even though the daemon process was alive.

Adds the missing accept loop to `src/striatum/daemon.py::run_daemon_foreground`
(synthesis pattern from dogfood-058):

- One accept thread polls `sock.accept()` with a 0.5s timeout against a
  `threading.Event` stop flag.
- Each accepted connection gets a daemon thread that wraps
  `conn.makefile("rwb")`, iterates NDJSON envelopes via
  `striatum.daemon_rpc.framing.read_envelopes(stream)`, and dispatches
  through `DaemonRpcRouter.handle(envelope, connection_id=<uuid>,
  transport="unix", require_handshake=True)` — writing each response
  back via `striatum.daemon_rpc.framing.write_response`.
- Router constructed once at startup with the daemon's PG connection
  (from `daemon_pg.connection.connect` after `doctor(..., apply=True)`
  succeeds) and `substrate_schema` from the doctor's reported schema
  version.
- Graceful shutdown: SIGTERM/SIGINT sets the stop event → closes the
  listener (breaks `accept()`) → joins accept thread with 2s timeout →
  joins per-connection threads with 0.5s each → closes daemon PG
  connection → unlinks socket + pid files.

Smoke-tested end-to-end:
- `daemon.hello` via `daemon_rpc.client.call_unix` returns
  `daemon_version` + `methods_etag` (the bound socket now actually
  serves RPC, not just probes).
- `striatum status` without `STRIATUM_DAEMON_REQUIRED=0
  STRIATUM_TEST_HARNESS=1` exits 12 (`repo_not_migrated`) instead of
  exit 11 (`daemon_unreachable`) — the daemon-required path is alive;
  the next step (`daemon migrate-repo-local`) is now reachable through
  the supported CLI flow.

### `POSTGRES_TRANSITION.md` — daemon-role provisioning runbook

Adds the "Provision the daemon-required role" section (operator
friction identified in dogfood-057's setup phase). Copy-pasteable SQL
block creates `striatumd_rw` with the right grants and revokes
(`REVOKE UPDATE, DELETE ON striatumd.{audit_log,events,artifacts}`).
Fresh installs that previously used the database owner as the
connecting role would trip the `unsafe_privileges` doctor refusal;
this section is the documented remediation.

### V1.5 follow-up still outstanding (deferred to V1.6 / dogfood-059)

dogfood-058 was scaffolded as a full 10-job V1.5 fix-up but the
cycle-exhaustion hit on `review_design` (Track-A/Track-B boundary
clarifications that codex couldn't fix in two synth revisions). After
operator override + cascade-cancel the run terminated without an
implementer phase. The accept loop + role runbook above are the
operator-driven subset that unblocks migration; the rest of V1.5
(codex F2 capability-denial test matrix, F3 audit-chain
SERIALIZABLE/row-lock per handler, F4 append-only role-grant test,
claude HIGH#1 actual byte-equivalence parity rig, claude HIGH#2 dead
code cleanup, schema migration 0006 for `striatumd.events.previous_hash`/
`row_hash`, `daemon doctor --explain`) is captured as a V1.6 / dogfood-059
follow-up in `docs/dogfood/058/OPERATOR_REPORT.md`.

Also outstanding: the migration-retry-after-rollback path (clean
re-migration when `repo_migrations` checkpoint mismatches the source
SQLite sha256) requires a `--reset-checkpoint` flag or manual
superuser cleanup; tracked in OPERATOR_REPORT.md.

## v1.49.0 — 2026-05-14

### RFC 0048 V1 Phase A — Python handler port (dogfood-057)

Land the Python side of the substrate-facade fix. All 16 single-repo
mutation handlers move from `striatum.cli` SQLite-backed dispatch into
native PG-backed handlers under `src/striatum/daemon_pg/handlers/`:

- **`workflow_loop/`** (9 methods, Track A codex implementer):
  `register_session`, `claim_next`, `ack_work`, `complete_job`,
  `release_lease`, `block_job`, `record_verdict`, `submit_review`,
  `override_review_verdict`.
- **`recovery_evidence/`** (7 methods, Track B claude implementer):
  `stale_leases`, `requeue_stale`, `cancel_job`, `process_reconcile`,
  `resume_blocker`, `auto_publish_stale_artifacts`, `evidence_export`.
- Shared infra: `handlers/__init__.py`, `handlers/registry.py`,
  `handlers/context.py`. `DaemonRpcRouter._route` resolves the PG
  handler before falling back to legacy `CLI_ROUTES`. Track B
  registers via decorator self-registration so its write scope can
  stay disjoint from Track A's server/registry/__init__ write scope.
- Tests for all 16 methods under `tests/daemon_pg/handlers/`.

**V1.5 follow-up risks** (accepted in this V1 landing — see
`docs/rfcs/0048-daemon-side-substrate-migration.md#v15-follow-up`):
codex F1-F4 (fail-closed routing, capability-denial tests, audit-chain
concurrency, append-only role enforcement) and claude HIGH#1/#2
(byte-equivalence parity tests advertised but unused; dead code paths
in `recovery.resume --complete` / `recovery.auto`). RFC 0048 V1.5
fix-up dogfood will scope these.

### Operator playbook — substrate friction observed during the run

`docs/POSTGRES_TRANSITION.md` still lacks a fresh-install
role-provisioning runbook; an operator who installs Postgres locally
and uses the database owner as the connecting role will trip the
`unsafe_privileges` doctor check (owner has implicit UPDATE/DELETE on
`striatumd.audit_log`). Workaround used during dogfood-057:
`CREATE ROLE striatumd_rw WITH LOGIN PASSWORD '...' ;
GRANT CONNECT/USAGE/SELECT/INSERT/UPDATE/DELETE on schema + tables;
REVOKE UPDATE, DELETE ON striatumd.{audit_log,events,artifacts};
GRANT CREATE ON DATABASE + SCHEMA (for migrations)`. Worth either
documenting or having `daemon doctor --apply-migrations` provision
the role on first run. Tracked as RFC 0048 V1.5 ergonomics.

## v1.48.2 — 2026-05-14

### Fixed — CI green again after 6 days of red

`gh run list --workflow CI` showed 298 consecutive failures since
`2c7237d` (2026-05-08T17:14:49Z). Two root causes:

- **Python typecheck (all 4 Python matrix cells, 16 mypy errors)** —
  missing third-party stubs (`keyring`, `psycopg`), one stale
  `# type: ignore`, one real `str.isoformat()` double-format bug in
  `daemon_pg/repo_local_migration.py::_write_sentinel`, three real
  `object`-not-iterable narrowing gaps in `test_dashboard_web_parity`,
  and untyped test functions in `test_daemon_go_supervisor` +
  `test_registry_rfc0043_coverage`. Fixed in-place; `python -m mypy`
  reports `Success: no issues found in 212 source files`.

- **Go matrix (4 cells, build step fails)** —
  `.github/workflows/ci.yml:27` pinned Go to `1.22` but `go/go.mod`
  requires `1.23` since RFC 0039 V1.5's pgx adoption. CI's setup-go
  installed 1.22; `go build` refused the toolchain mismatch. Also
  added `cache-dependency-path: go/go.sum` so setup-go can warm its
  module cache. (TODO item 30 / RFC 0039 V1.6 F1 covered the
  unchecksummed `go.sum` angle; the actual CI break was the version
  pin, not the sum file.)

No source behavior changes; the wrappers / dogfood artifacts / RFCs
shipped in v1.46.0-v1.48.1 are unaffected.

## v1.48.1 — 2026-05-14

### Fixed — claude / gemini lane wrappers exit cleanly without producing artifacts

Root cause for the 10+ instance claude permission-prompt no-publish stall and
many "gemini wrote artifact but didn't publish" failure modes was identified
by inspecting
`$STRIATUM_SCRATCH_DIR/{claude,gemini}-logs/packet-NNNN.log` after
dogfood-056: each agent CLI's permission system was prompting interactively
on the striatum CLI shell calls the packet required, and since stdin was
already consumed by the packet payload there was no one to answer the
prompt — the agent exited cleanly with the prompt as its last stdout line
and no artifact written / no CLI verb invoked.

- `.striatum/bin/claude-supervised-wrapper.sh` — `claude --print` now
  invoked with `--permission-mode acceptEdits --allowedTools "Bash"`.
  Auto-approves the striatum CLI verbs the agent must call; filesystem
  boundaries are still enforced by the packet's write_scope.
- `.striatum/bin/gemini-supervised-wrapper.sh` — `gemini --prompt -`
  approval mode changed from `auto_edit` to `yolo`. `auto_edit` approved
  file edits but not `run_shell_command`, which is why gemini wrote
  artifacts but couldn't invoke striatum to finalize.
- `.striatum/bin/codex-supervised-wrapper.sh` — no functional change; the
  existing `--dangerously-bypass-approvals-and-sandbox -c approval_policy=never`
  already cleared the same surface. Added a clarifying comment so the
  three wrappers document the same auth contract.

This is the operational complement to RFC 0051 (auto-finalize from
frontmatter): once the wrappers stop stalling on permission prompts,
the agent itself calls the closing CLI verbs, and the auto-finalize
path becomes the fallback for genuinely-crashed agents rather than the
default for every claude review.

## v1.48.0 — 2026-05-14

### Added — RFC 0050 V2: interactive layer (recovery panel island, override modal, copy-on-click, graph-editor data binding)

Lands RFC 0050 V2 via dogfood-056. Closes RFC 0050 across V1 (v1.46.0),
V1.5 (v1.47.0), and V2 (this release).

**dogfood-056 (V2 interactive layer):**
- **Recovery panel island** (`src/striatum/web/frontend/src/islands/recovery-panel/`)
  — React island enhances the server-rendered recovery panel with a dry-run
  preview of `striatum recovery auto-publish` via `/v1/invoke`. No-JS fallback
  preserved per UI_REWORK.md §8.3.
- **Override verdict modal** (`src/striatum/web/static/override_verdict.js`)
  — ARIA `<dialog>` with focus trap, Escape close, focus return.
  Posts only allowed override fields to `/v1/invoke`; identifiers come
  from server-rendered `data-*` attributes per UI_REWORK.md §8.6.
- **Copy-on-click** (`src/striatum/web/static/copy_on_click.js` + `base.js`
  wiring) — `[data-copy]` targets initialize globally on `DOMContentLoaded`,
  Enter/click copy, 1.2s toast. Identifier regex
  `^(run|job|sess|art|proc|super|lease)_[0-9a-f]+$` per UI_REWORK.md §7.7.
- **Workflow graph editor `require_attested_lane`** — per-node data binding
  in `WorkflowGraphEditor.tsx`. Stored in state, rendered in node body +
  textual summary, round-trips through serializer. **Data-binding only**;
  no viewport overlay (deferred to React Flow v12 per GH #6).
- 7 regression tests:
  `test_recovery_panel_dry_run`, `test_override_modal_payload`,
  `test_copy_on_click`, `test_run_detail_recovery_panel` (updated),
  `recovery-panel.test.tsx`, `workflow-graph-editor.test.ts`, and
  bundle-hash discipline in `test_web_ui.py`.

**Known follow-up findings (recorded in dogfood-056 review/build/):**
- **HIGH (gemini F1)**: `/v1/invoke` lacks CSRF protection +
  Content-Type validation; cross-site command execution risk on local
  runner. **Security-hardening pass deferred to v1.48.x.**
- **MEDIUM (gemini F2/F3)**: Override modal DOM tampering + recovery
  dry-run side-effect surface. Deferred to v1.48.x.
- **LOW (gemini F4/F5, claude F1-F3)**: Clipboard hijack via arbitrary
  `data-copy`, graph-editor ghost field on job type change, recovery
  panel error-state copy affordance, modal submit feedback.



### Added — RFC 0050 V1.5: template extensions + provenance-honesty fixes

Lands RFC 0050 V1.5 across dogfood-055 (template extensions) +
dogfood-055b (provenance honesty fix-up). Honest V1.5 acceptance —
gemini's 3 V1.5 provenance findings on 055 were closed in 055b before
the V1.5 override on 055's gemini verdict.

**dogfood-055 (template extensions):**
- New partials: `_recovery_panel.html`, `_expected_artifacts_table.html`,
  `_session_chip.html`.
- Templates extended to consume V1 primitives + new partials:
  `run_detail.html`, `job_detail.html`, `artifact_view.html`,
  `run_posture_verdicts.html`, `doctor.html`, `view_file.html`.
- 6 regression tests:
  `test_run_detail_recovery_panel`, `test_job_detail_expected_artifacts`,
  `test_artifact_view_provenance_trail`, `test_posture_verdicts_override_provenance`,
  `test_doctor_per_record_recipes`, `test_view_file_breadcrumb_heuristic`.

**dogfood-055b (provenance honesty fix-up):**
- `service.py::_recorded_artifact_attestation_chip` now requires both an
  exact `expected_author_line` match AND `attestation_override_rationale
  IS NULL` to render `attested`. Closes byline-forgery vector against
  operator-on-behalf publishes whose recorded byline looks model-shaped.
- `service.py::_shape_verdict_rows` distinguishes `previously_attested`
  (closed/lost supervised session) from `unattested` (never attested) —
  attestation drift over time no longer collapses into the same warning.
- `service.py::_lane_evidence_chip` + `LaneEvidenceChip.tsx` surface
  `override: <rationale>` when `attestation_override_rationale` is
  present, instead of muted `not_yet_correlated`. Closes
  override-rationale visibility gap.
- Updated regression tests: `test_byline_regression`,
  `test_override_rationale_regression`, `test_lane_evidence_guard`.



### Added — RFC 0050 V1: operator UI primitives + dashboard parity + provenance honesty

Lands RFC 0050 V1 across dogfood-054 (primitives) + dogfood-054b
(provenance honesty fix-up). Honest V1 acceptance — gemini's
adversarial findings on 054 were closed in 054b before the V1
override on 054's gemini verdict.

**dogfood-054 (primitives):**
- New shared TypeScript components under
  `src/striatum/web/frontend/src/shared/components/`:
  `RunStatePill`, `JobStatePill`, `VerdictChip` (with override
  provenance slot), `LaneAttestationChip` (with reason
  sub-text), `PostureChip`, `BylineLine`, `LaneEvidenceChip`
  (always `not_yet_correlated` muted per RFC 0050 — never
  green pre-correlation), `ExpectedArtifactsTable`.
- `templates/_components.html` — Jinja2 macros mirroring the
  TypeScript components so server-rendered and island surfaces
  speak the same vocabulary.
- `service.py` page-payload shaping for `run_list` /
  `run_detail` / `job_detail`.
- `dashboard.py` text-mode parity: same chip vocabulary as
  ASCII glyphs, consumes V1.45.0 `next_actions` verbatim.
- `static/base.css` semantic tokens
  (`--status-*`, `--attestation-*`, `--override-marker`,
  `--evidence-not-yet-correlated`). Reserved
  `--status-compromised` for V1.7.
- 3 regression tests: `test_byline_regression.py`,
  `test_dashboard_web_parity.py`,
  `test_override_rationale_regression.py`.

**dogfood-054b (V1 provenance honesty fix-up):**
Closes 4 V1 non-negotiable violations gemini caught in 054:
- **F1 byline forgery loophole closed.** `_components.html:72`
  + `BylineLine.tsx:13` force `author: operator` (or
  self-declared form) when `attested=false`. The forged disk
  byline is not rendered, not just CSS-decorated.
  `service.py:316` + `dashboard.py:473` apply the same
  substitution. Pinned by `tests/test_byline_regression.py:70`
  + `byline-line.test.tsx:7`.
- **F2 inferred-override removed.** `service.py` no longer
  guesses `operator_override` from accepting-after-non-accepting
  patterns. Missing `verdicts.source` → `natural`. Real
  overrides still render via the `verdict.overridden` event
  trail. Pinned by `test_override_rationale_regression.py:26+82`.
- **F3 attestation recording-time.** Lane attestation chips
  read from `artifacts.author_line` + recording-time supervisor
  state, not live recompute. Live recompute only on
  intrinsically-current surfaces.
- **F4 dashboard rationale.** `_verdict_chip` accepts and
  renders truncated rationale for override verdicts.

**V1.45.0 inbox SQL bug fix (incidental):**
`src/striatum/cli/dispatch.py::_cli_inbox` was selecting
`leases.job_id` but the column is named `resource_id`. The
correct subquery is `SELECT resource_id FROM leases WHERE
owner_session_id = ? AND state = 'active' AND resource_type =
'job'`. Without the fix the helper returned a random
session's packet, not the queried session's. Caught during
dogfood-054b reviewer drive.

**Provenance discipline:** every operator-on-behalf publish on
both 054 and 054b used the RFC 0046 V1
`--allow-no-process-execution --override-rationale` path. No
silent operator publishes; audit-chain records every override.

### Backlog queued for v1.47.0 / v1.48.0

- **dogfood-055** (RFC 0050 V1.5) scaffolded + validated:
  template extensions for `run_detail` (recovery panel +
  next-actions banner + sessions strip), `job_detail`
  (expected-artifacts + process-evidence), `artifact_view`
  (provenance trail), `run_posture_verdicts` (override
  visual distinction), `doctor` (per-record recipes),
  `view_file` (breadcrumb). New partials.
- **dogfood-056** (RFC 0050 V2) scaffolded + validated:
  `recovery-panel` island, `override_verdict.js` modal,
  `copy_on_click.js`, `workflow-graph-editor`
  `require_attested_lane` data binding (no viewport overlay
  pending reactflow v12).

Both ready to kick off the moment their predecessor lands.

## v1.45.0 — 2026-05-14

### Added — RFC 0050 V1 prerequisites

Unblocks the `dogfood-054` UI rework run (RFC 0050 V1). The
implementation work happens in a follow-up dogfood; this release
ships only the prerequisites the design handoff (`docs/design/UI_REWORK.md`)
calls out as blocking-for-acceptance.

- **Version drift fix.** `src/striatum/__init__.py::__version__`
  was hardcoded `"1.37.0"` and never bumped with `pyproject.toml`,
  so `striatum --version` reported 1.37.0 while pip showed v1.44.1.
  Now derived from `importlib.metadata.version("striatum-orchestrator")`
  — single source of truth, drift eliminated.
- **OQ-4 — V1.41 burn-down verbs in `next_actions`.**
  `src/striatum/cli/introspect.py::next_actions` emits three new
  deterministic action names so the `dashboard --once` ↔ web
  parity tests (UI_REWORK.md §9.9 + §9.10) can read a single
  source of truth:
  - `inspect_packet_with_inbox` — surfaces whenever a packet is
    claimable; signals the operator should run `striatum inbox`.
  - `derive_expected_byline` — surfaces alongside any verdict
    override or checkpoint resolution; signals `striatum byline`.
  - `recovery_auto_publish` — surfaces when `has_stale_leases=True`;
    signals the V1.41 stale-lease auto-publish sweep would
    self-heal at least one job.
  - New `_has_stale_leases_with_on_disk_artifacts` helper does
    the cheap precheck (existence of `expected_artifacts[].path`
    on disk; the auto-publish call itself enforces full byline
    conformance).
- **RFC 0050.** New RFC adopting `docs/design/UI_REWORK.md` as
  the canonical UI spec; three-phase landing plan (V1 / V1.5 /
  V2). Skips the standard design-ceremony triple because the
  handoff IS the design output.

### Regression tests

- `tests/test_next_actions_v141_burndown.py` — 6/6 pass pinning
  the new action names, conditions, ordering, and dedup behavior.

## v1.44.1 — 2026-05-13

### Fixed — GH #8: v16 runs rebuild leaves runs_new residue

Engram operator runs on 2026-05-13 hit a real bug in the v1.44.0 v16
migration: the SQLite rebuild ran with `PRAGMA foreign_keys = ON`,
the `DROP TABLE runs` step failed because other tables reference
`runs`, and `runs_new` was left behind. Every subsequent CLI command
then failed with `table runs_new already exists`.

Fix:
- `_apply_v16_decision_propagation` now routes through the existing
  `rebuild_table` helper, which toggles `PRAGMA foreign_keys` around
  the rebuild and `DROP TABLE IF EXISTS` any prior temp-table
  residue. Operator-side checkouts hit the GH #8 wedge had to apply
  the same patch locally before commands recovered.
- `tests/test_gh8_v16_rebuild_idempotent.py` pins both halves:
  (1) a clean v16 leaves no `runs_new` behind; (2) a DB with the
  post-failure residue migrates cleanly on the second attempt.

Affected production runs (per GH #8):
- RFC0038 UI rework run_468b22aff5e54a9280a867d3c81314e6
- RFC0044 tenant isolation run_322110269dfb4ec98fc6f7ea818448c0

## v1.44.0 — 2026-05-13

### Added — RFC 0047 V1: decision-record propagation (closes GH #3)

`striatum decision record --outcome rejected` now propagates the
rejection to first-class surfaces. Downstream consumers no longer
have to walk the events table looking for `decision.recorded` —
status, why, dashboard, and evidence export all read the projection.

- **Schema migration v16** (`src/striatum/migrations.py`):
  - `runs.state` CHECK widened to include `compromised`. Table
    rebuilt in place via the standard SQLite drop-and-recreate idiom.
  - `verdicts.superseded_by_decision_id` + `superseded_at` columns
    added. NULL = not superseded; non-null = superseded by the named
    decision at the named time.
- **Propagation** (`src/striatum/cli/mutations.py::_propagate_decision_outcome`):
  - `outcome=rejected` against a non-compromised run → flips
    `runs.state` to `compromised`, marks every accepting verdict
    (`accept`, `accept_with_findings`) as superseded by the
    decision id, emits a `run.compromised` event with the
    superseded-verdict count.
  - `outcome=accepted` against a compromised run → reopens to
    `completed`, emits `run.reopened_after_compromised`. Existing
    verdict supersession trail is preserved (the rejection
    history stays in the audit chain).
  - `outcome=rejected` against an already-compromised run is a
    no-op (no extra event emitted).
  - `outcome=accepted_with_follow_up` and `outcome=accepted` against
    a non-compromised run do not change run state — the follow-up
    is tracked through the existing decision artifact + event.
- **Idempotency:** re-running the same outcome against a run already
  in that state is a no-op.
- **Audit chain:** the existing `decision.recorded` event stays
  authoritative; the new `run.compromised` /
  `run.reopened_after_compromised` events extend the audit-chain
  payload shape but not its hashing strategy.

### Regression tests

- `tests/test_decision_propagation.py` — 7/7 pass.
  - Migration v16 admits `compromised` in CHECK + adds supersession
    columns.
  - Rejected propagates + supersedes accepting verdicts + emits
    event.
  - Rejected against compromised is a no-op.
  - Accepted reopens compromised → completed; supersession trail
    preserved.
  - Accepted against completed run is a no-op.
  - Accepted_with_follow_up does not change state.

### Backlog after v1.44.0

- **GH #2** (operator-asserted lane attestation): broader trust-model
  framing concern. The V1 lane evidence guard (RFC 0046, v1.43.0)
  significantly reduces the practical attack surface; full closure
  needs RFC 0046 V1.7 (path-specific check) + RFC 0048 Phase B
  (Go-core attestation).
- **RFC 0046 V1.7 polish:** add `observed_output_paths_json` to the
  `process_executions` schema; tighten `_lane_evidence_present` to
  path-specific. Web UI `LaneEvidenceChip` + dashboard `evid:`
  column.
- **RFC 0048 V2.0 phase:** the substrate flip — port single-repo
  business logic to PG-backed daemon-internal handlers + Go core
  parity + remove the TEST_HARNESS escape.

## v1.43.0 — 2026-05-13

### Added — V1.7 backlog batch

Three RFCs drafted (0046, 0047, 0048) and one V1 implementation
landed (RFC 0046). Two surgical V1.7 fixes shipped alongside (RFC
0039 V1.7 macOS reader + PointerStore boot wire-up, GH #6 reactflow
ViewportPortal removal). Dogfood-053 ran the RFC 0046 V1 ceremony
and the new lane evidence guard self-validated by refusing the
operator-on-behalf publish until the override flag was supplied.

#### RFC 0046 V1 — Lane evidence guard at publish-artifact (closes GH #2 + #5)

- `src/striatum/migrations.py` v15: new
  `attestation_override_rationale TEXT` column on `artifacts`.
- `src/striatum/artifacts.py::publish_artifact`: if the resolved
  byline is a model byline (not `author: operator [...]`), refuse
  publish when the session has no completed exit-0
  `process_executions` row. New helpers `_is_operator_byline` and
  `_lane_evidence_present`.
- `src/striatum/cli/parser.py` + `dispatch.py`:
  `publish-artifact --allow-no-process-execution
  --override-rationale "<text>"` operator opt-in. Empty rationale
  refuses with exit code 2. `submit-review` gets the same pair of
  flags so operator-composed reviews can also flow.
- New `provenance.publish_without_process_execution` event emitted
  on every override, carrying byline + path + rationale.
- Self-validated by dogfood-053: the operator's publish-on-behalf
  of the implementer HANDOFF was refused with
  `lane_evidence_missing` until `--allow-no-process-execution
  --override-rationale "..."` was supplied.

#### RFC 0039 V1.7 — macOS pid reader + PointerStore wire-up

- `go/pkg/supervisor/start_time_{linux,darwin,other}.go` split the
  per-OS readers via build tags. darwin uses
  `/bin/ps -o lstart= -p <pid>`; non-Linux/darwin returns
  `(_, false)` so the caller falls back to signal-0 only.
- `go/pkg/db/connection.go::Pool.RawPool *pgxpool.Pool` exposes the
  underlying pool to consumers needing typed access (e.g. the
  supervisor pointer store).
- `go/cmd/striatumd/main.go` constructs `SupervisorPointerStore` at
  boot with a `supervisor.PointerStore`-conformant adapter
  (`db.PointerRow ↔ supervisor.PointerRow`). The not_implemented
  handlers stay; RFC 0048 Phase B will wire the actual handler
  ports.

#### GH #6 — reactflow ViewportPortal fix

- `WorkflowGraphEditor.tsx` removes the v12-only `ViewportPortal`
  import and returns `null` from `PhaseBands` with a comment
  pointing to the V1.5 polish backlog. `make ui-build` now produces
  real Vite output (6KB–622KB bundles, not 50–75 byte placeholders).
  `make ui-verify-bundle` passes.

#### RFCs drafted for the rest of the backlog

- **RFC 0046** (V1.7) Lane evidence guard at publish-artifact —
  V1 landed in this release; V1.7 follow-up scope tracked.
- **RFC 0047** (V1.8) Decision-record propagation +
  `runs.state = compromised` (GH #3). Schema migration, byline
  rewrite path, status/why surface changes scoped for next.
- **RFC 0048** (V2.0 phase) Daemon-side substrate migration. Three
  phases: A) PG-backed Python handlers; B) Go core parity;
  C) remove the `STRIATUM_DAEMON_REQUIRED=0 + STRIATUM_TEST_HARNESS=1`
  escape entirely.

### Tests

- `tests/test_lane_evidence_guard.py` — 6/6 pass
  (`_is_operator_byline`, `_lane_evidence_present`, migration v15
  shape).
- 77/77 pass in the broader unit-test slice.
- `make lint` + `make typecheck` clean across the touched files.

## v1.42.0 — 2026-05-13

### Fixed — GH #7: process-adapter post-completion blocker

Closes the recurring "adapter session naturally completed, then exited
nonzero, blocker stuck on terminal job" pattern.

- `evaluate_and_block_inline` in `src/striatum/process_completion.py`
  now short-circuits when the job state is already terminal
  (`completed`, `failed`, `canceled`, `skipped`). The nonzero exit is
  treated as a benign trailing signal from the supervised process,
  not a workflow failure.
- `recovery resume --force` in `src/striatum/cli/recovery.py` now
  dismisses a legacy open process-adapter blocker against a terminal
  job as a no-op (resolves the blocker, preserves terminal state,
  emits `recovery.blocker_dismissed_terminal` event). Closes the
  "no current lease" recovery dead-end on already-affected runs.
- Regression: `tests/test_gh7_terminal_blocker.py` pins the guard
  for all four terminal states.

### Backlog (triaged from open GH issues)

The remaining open issues need design work beyond this surgical
release. Each is documented for the next session:

- **GH #2** (operator-asserted lane treated as attested): the byline
  path already differentiates via `attestation.attested` →
  `operator_author_line`, so unattested sessions DO get `author:
  operator`. Investigation: confirm whether attestation drift
  mid-flight can re-introduce model bylines. If so, treat as a
  regression here. If not, the issue is downstream consumer
  documentation, not byline forgery.
- **GH #3** (decision record propagation): needs a `run.state =
  'compromised'` enum value, byline-rewrite path, and status/why
  surface changes. Multi-file design. V1.42 documents the gap;
  V1.7 implements.
- **GH #5** (publish-artifact without process_execution): related
  to GH #2; add a publish-time guard event
  `provenance.publish_without_process_execution` and an
  `--allow-no-process-execution` opt-in. V1.7.
- **GH #6** (web UI placeholder bundles): mechanical — `make
  ui-build`, commit real Vite output. Blocked by node/npm
  availability in the operator environment. V1.7.

## v1.41.0 — 2026-05-13

### Added — harness friction burn-down

Closes the recurring operator-on-behalf frictions observed across
dogfoods 048-052 (claude-no-explicit-publish 6+ instances, gemini
byline-drift 4 instances, override-fresh-session dance, etc.). No
new dogfood ceremony — this *is* the burn-down.

- **A1 — `striatum recovery auto-publish --run-id`** (`src/striatum/cli/recovery.py`).
  Walks stale leases. For each, if the work-packet's `expected_artifacts[].path`
  is present on disk and the on-disk byline canonicalises exactly to the
  `expected_author_line`, auto-runs ack + publish-artifact + complete on
  behalf of the dead session. Two-condition gate (byline + path) prevents
  misfiring. Dry-run mode reports without writing.
- **A2 — front-matter author wins** (`src/striatum/artifacts.py`).
  `markdown_title_block_author_lines` returns front-matter author lines
  exclusively when present; in title block, only the *first* canonical
  byline counts. Closes the gemini `Author: <real-name>` body-mention
  competing pattern.
- **A3 — `publish-artifact` defaults from `expected_artifacts`**
  (`src/striatum/cli/dispatch.py::_resolve_publish_defaults`). When
  `--path` matches a declared `expected_artifacts[].path` and only one
  declared artifact matches, `--kind` and `--logical-name` default from
  the workflow. Ambiguity errors list declared paths.
- **A4 — `striatum byline --session-id --job-id`**. Prints the exact
  `expected_author_line`; replaces the manual python -c spelunking.
- **C1 — `striatum inbox --session-id`**. Prints the current packet's
  ids + expected artifacts + byline; replaces the multi-step `striatum
  why <sid> --json` parsing operators were doing.
- **B1 — `override-verdict --auto-fresh-session`**. When the supplied
  session already has a verdict for the job (so override-verdict would
  refuse), the flag registers a fresh operator reviewer session on the
  same lane and uses it. Removes the manual two-step dance.

### Regression tests

- `tests/test_harness_friction_burndown.py` — front-matter-wins scanner
  + canonical byline form.
- `tests/exit_codes/test_rfc0043_split_brain.py` — `db.connect` refuses
  fresh SQLite when sentinel/tombstone present.
- `tests/daemon_pg/test_repo_local_migration_locking.py` — concurrent
  `migrate-repo-local` refuses with exit code 8.
- `tests/cli/test_parser_help.py` — per-flag help on
  `daemon migrate-repo-local`.

### Out of scope (still backlog)

- Default workflow-artifact-output path (TODO #30).
- `striatum self-update` (separate feature).
- Operator sub-agent workflows as first-class skill (memory item).
- Daemon-side substrate migration (RFC 0043 V2.0).

## v1.40.0 — 2026-05-13

### Added — RFC 0039 V1.6 Go daemon hardening (dogfood-051)

Closes the V1.6 follow-ups recorded in v1.39.0 across F-pty,
F-pid-recycling, F-perms, F-store, F-ci. Implementer slot was
operator-driven (recurring 5+-instance claude-no-publish anti-pattern;
harness backlog item).

- **F-pty** — `github.com/creack/pty v1.1.24` integrated into
  `go/go.mod` + `go/go.sum`. `go/pkg/supervisor/pty.go::launchPTY`
  uses `pty.Start(cmd)` returning the master fd as `StdinWriter`. The
  not-wired sentinel is removed; the supervisor test now asserts
  functional PTY launch against `/bin/true`.
- **F-pid-recycling** — `go/pkg/supervisor/liveness.go` adds
  `processAliveAtStartTime` + `readProcessStartTime` reading
  `/proc/<pid>/stat` field 22 plus `/proc/stat`'s `btime` with 2s
  tolerance. Liveness goroutine passes `row.StartedAt` on each tick.
  Non-Linux falls back to signal-0 only (V1.7 macOS path with
  `proc_pidinfo` / sysctl).
- **F-perms** — `go/pkg/supervisor/pointer.go` + `pty.go` scratch dir
  `0o700`, pidfile `0o600`, stdout/stderr fallback `0o600`.
- **F-store** — new `go/pkg/db/supervisor_pointers.go`:
  `SupervisorPointerStore{pool *pgxpool.Pool}` implementing
  `supervisor.PointerStore` (`Upsert` / `MarkLost` / `Get`) via UPSERT
  on `striatumd.process_supervisor_pointers`. Typed
  `ErrSupervisorNotFound` returned from `Get` and `MarkLost` when
  rows-affected is zero.
- **F-ci** — `.github/workflows/ci.yml` adds a "Verify Go binary
  present" step under `daemon-core == 'go'` that fails fast with
  `::error::` annotation if `go/bin/striatumd` is missing after
  `make daemon-go-build`. Closes dogfood-049 gemini F6 (CI matrix
  bypass risk).

### Added — RFC 0043 V1.6 substrate hardening (dogfood-052)

Closes the V1.6 follow-ups recorded in v1.38.0 across F-escape,
F-split-brain, F-lock, F-help. Gemini A1 (daemon-side substrate
migration) **stays deferred to V2.0** as a separate phase RFC.

- **F-escape** — `src/striatum/cli/daemon_required.py`:
  `resolve_requirement` opt-out now requires
  `STRIATUM_DAEMON_REQUIRED == "0"` **and**
  `STRIATUM_TEST_HARNESS == "1"`. The bare env var no longer
  bypasses production enforcement. `tests/conftest.py` exports both.
  Closes codex dogfood-050 threat-model finding.
- **F-split-brain** — `src/striatum/db.connect`: before creating a
  fresh SQLite (file absent), checks for sentinel
  `.striatum/retired-local-state.migrated` OR tombstone
  `.striatum/retired-local-state.tombstone`. Raises `StriatumError(exit_code=12)`
  with `repo_not_migrated` remediation text. Closes gemini A2.
- **F-lock** —
  `src/striatum/daemon_pg/repo_local_migration.py`: new
  `MigrationInProgressError(StriatumError, exit_code=8)` and
  `_exclusive_migrate_lock(repo)` context manager taking a non-blocking
  exclusive `fcntl.flock` on `.striatum/retired-local-state.migrate.lock`
  (sidecar — survives the source-file rename during finalization and
  does not fight SQLite's own POSIX byte-range locks). Refusal message
  names the source SQLite path. Exit code reuses the V1.5
  ``migrate-repo-local`` refusal code per the V1.6 design synthesis
  ("avoid introducing a new exit code for this narrow V1.6 slice").
  Closes gemini A3.
- **F-help** — `src/striatum/cli/parser.py` registers
  `description=` + `help=` on every `migrate-repo-local` flag
  (`--from`, `--to`, `--repo`, `--postgres-url`, `--dry-run`,
  `--confirm-delete`, `--keep-sqlite-readonly`,
  `--no-keep-sqlite-readonly`, `--json`). Closes claude
  dogfood-050 F-dx-1.

### Known follow-ups

- **V1.7 (RFC 0039):** macOS process start-time reader (proc_pidinfo
  / sysctl); wire Postgres-backed `SupervisorPointerStore` into
  `cmd/striatumd/main.go` boot path.
- **V2.0 (RFC 0043):** daemon-side single-repo business logic on
  Postgres (gemini A1) — full substrate flip at the daemon RPC
  business-logic layer.

## v1.39.0 — 2026-05-13

### Added

- RFC 0039 Phase 2 — Go daemon completion (Steps 3-6) landed under
  dogfood-049 as a two-track split (Track A codex / Track B claude).
  The Go daemon now ships in the wheel, is selectable via
  `striatum daemon start --core go`, exposes the RFC 0043 method
  vocabulary, and has a Go-side supervisor lifecycle scaffold.

  Track A (codex implementer, 90% natural):
  - `go/pkg/rpc/registry.go` expanded to the RFC 0043 canonical dotted
    method vocabulary (`session.register`, `work.*`, `artifact.publish`,
    `review.*`, `decision.record`, `checkpoint.resolve`, `recovery.*`,
    `worktree.*`, `branch.confirm`, `run.*`, `workflow.*`). Legacy
    undotted aliases registered + deprecated.
  - `go/pkg/apply/{receipt.go,service.go}` — apply receipt lookup +
    fail-closed sealed-apply skeleton (cryptographic verification is
    V1.6 follow-up per gemini F2).
  - `go/pkg/mcp/{capabilities.go,tools.go}` — capability-filtered tool
    visibility + `tools/call` dispatch through Go RPC server.
  - `go/pkg/crossrepo/{prepare.go,lifecycle.go}` — cross-repo lifecycle
    helpers over Postgres.
  - `go/cmd/striatumd/main.go` wired to register apply + cross-repo
    handlers + stable fail-closed handlers for the broader mutation
    surface (deterministic `not_implemented` instead of
    `method_unknown`).
  - `src/striatum/cli/daemon.py` — `daemon start --core {python,go}`
    with `STRIATUM_DAEMON_CORE` env default. Go binary resolver order:
    packaged `_daemongo` → `STRIATUMD_GO_BIN` → `go/bin/striatumd` →
    PATH.
  - `src/striatum/cli/parser.py` — `--core` flag on `daemon start`.

  Track B (claude implementer stalled, operator-driven):
  - `go/pkg/supervisor/pointer.go` — `PointerStore` interface +
    `PointerRow` mirroring `striatumd.process_supervisor_pointers`;
    atomic pidfile write under `<scratch>/<supervisor_id>/pid`.
  - `go/pkg/supervisor/liveness.go` — heartbeat goroutine + dead-PID
    detection via signal-0 probe + SIGTERM-with-grace cleanup. Defaults
    5s heartbeat / 30s lost-after / 5s grace-on-term match the Python
    supervisor.
  - `go/pkg/supervisor/pty.go` — `LaunchSpec` + non-PTY (pipe) launch
    path. **PTY branch returns "not wired" sentinel error** — the
    `creack/pty` integration is V1.6 follow-up.
  - `go/pkg/supervisor/supervisor_test.go` — table-driven tests
    (pidfile round-trip, dead-pid lost-detection, empty-command
    rejection, pipe-mode `/bin/true` launch).
  - `go/Makefile` — `release-{linux,darwin}-{amd64,arm64}` targets
    with `CGO_ENABLED=0`.
  - Top-level `Makefile` — `daemon-go-install` (host-only) and
    `daemon-go-release` (cross-compile + stage under
    `src/striatum/_daemongo/binaries/`).
  - `src/striatum/_daemongo/__init__.py` — `find_binary()` /
    `platform_slug()` package-data resolver. Returns `None` on sdist
    or missing platforms; CLI falls through to `STRIATUMD_GO_BIN`.
  - `pyproject.toml` — `"striatum._daemongo" = ["binaries/*"]` under
    `[tool.setuptools.package-data]`.
  - `MANIFEST.in` — `recursive-include src/striatum/_daemongo *`.
  - `.github/workflows/ci.yml` — `daemon-core: ["python", "go"]`
    matrix axis as explicit jobs (not in-process parametrization);
    `STRIATUM_MULTI_REPO_REQUIRE_PG=1` sentinel against all-skipped
    pass (closes dogfood-047 F3).
  - `.github/workflows/release.yml` — early `make daemon-go-release`
    step + `striatumd-binaries` upload artifact + wheel ships binaries
    via package-data.
  - `tests/test_daemon_go_supervisor.py` — Python harness scaffold;
    functional FIFO/heartbeat/SIGTERM assertions deferred to V1.6
    pending PTY landing.

  Inline operator fix during review phase:
  - `src/striatum/cli/dispatch.py:888-890` rewired from
    `run_daemon_foreground(...)` direct call to
    `launch_daemon_start(args)`. Closes F1 from both codex and claude
    build reviews (`--core go` was silently inert pre-fix).

### Known follow-ups (V1.6)

- **Full PTY integration on Go supervisor** — fold `creack/pty` into
  `go.mod`, wire the PTY branch, replace harness scaffold with
  functional assertions.
- **Full Go mutation handler suite** — implement every registered RPC
  method against Postgres-backed repo-local schema (currently most
  return `not_implemented`).
- **Apply-receipt cryptographic verification** — replace lookup-only
  `apply.VerifyReceipt` with signature check (gemini F2).
- **PID-recycling protection** — pair signal-0 probe with
  `/proc/<pid>/stat` start-time check (gemini F1).
- **Tighten scratch-dir perms** to 0700 / 0600 (gemini F3).
- **`STRIATUM_DAEMON_CORE` operator-clarity** — warn/refuse when env
  disagrees with explicit `--core` flag (gemini F5).
- **CI hard-fail on missing Go binary** when `daemon-core=go`
  (gemini F6).
- **Concrete Postgres-backed `PointerStore`** under
  `go/pkg/db/supervisor_pointers.go`.

## v1.38.0 — 2026-05-13

### Added

- RFC 0043 V1.5 — D102 follow-up findings closure under dogfood-050.
  Single-track claude implementer (deliberately not codex per D102
  anti-pattern note). Four named findings closed:
  - **F-crash:** Transactional rollback + checkpointed resume of
    `striatum daemon migrate-repo-local` after a kill-9 between
    Postgres commit and SQLite finalization. Adds atomic `.migrated`
    sentinel write after commit and before tombstone/delete; resume
    helper re-enters from the early-return path on rerun.
    (`src/striatum/daemon_pg/repo_local_migration.py`,
    `tests/daemon_pg/test_repo_local_migration_crash_resume.py`.)
  - **F-escape:** `STRIATUM_DAEMON_REQUIRED` default flip — unset env
    now enforces daemon-required; only `STRIATUM_DAEMON_REQUIRED == "0"`
    opts out. `resolve_requirement` in
    `src/striatum/cli/daemon_required.py` returns enforcement by
    default; per-command optional list and explicit-zero remain the
    only bypass surfaces.
  - **F-parser:** `striatum daemon migrate-repo-local` subcommand
    wired into argparse + dispatch end-to-end
    (`src/striatum/cli/parser.py:167-199`,
    `src/striatum/cli/dispatch.py:881-887`,
    `src/striatum/cli/daemon.py:24-44`).
  - **F-test:** Exit-code-12 (`repo_not_migrated`) e2e regression
    against real dispatch
    (`tests/exit_codes/test_rfc0043_refusals.py:207-243`) — runs
    `dispatch.main(["--repo", str(tmp), "status"])` against a tmp
    repo with a `.striatum/retired-local-state` plus listening daemon
    socket, asserts rc == 12 and that the remediation line names
    `striatum daemon migrate-repo-local --from sqlite --to pg --repo`.

### Known follow-ups (V1.6)

- Codex threat-model finding: `STRIATUM_DAEMON_REQUIRED=0` is still
  documented as an operator migration path. V1.6 will remove the
  runtime escape entirely (test-only gating or removal).
- Gemini adversarial findings A1/A2/A3:
  - A1 (critical): server-side substrate mismatch — daemon RPC
    delegates single-repo verbs back to SQLite-backed CLI logic;
    actual substrate flip is incomplete at the daemon business-logic
    layer. V1.6 will port daemon-internal single-repo logic onto
    Postgres directly.
  - A2 (high): split-brain — `striatum.db.connect` creates a fresh
    SQLite when the file is missing post-migration. V1.6 will refuse
    to create when a migration checkpoint exists.
  - A3 (medium): no exclusive lock on the source SQLite during
    migrate-repo-local. V1.6 will add explicit locking.
- Claude ergonomics finding F-dx-1: per-flag help text on
  `migrate-repo-local` is sparse (only two flags carry `help=`).

## v1.37.0 — 2026-05-13

### Added

- RFC 0043 V1 — Postgres as Sole Substrate + Daemon-Required Runtime
  landed under dogfood-048. Per D094, supersedes the local-SQLite
  assumption in D006/D007/D036 and the SQLite half of D009. The
  substrate flip lands on a two-track split so schema and CLI surface
  could proceed in parallel once the shared design synthesis fixed the
  schema name and method vocabulary.
  - **15 repo-local workflow tables in daemon-owned Postgres.**
    `src/striatum/daemon_pg/sql/0005_repo_local_workflow_state.sql`
    creates the full repo-local workflow surface under the existing
    `striatumd.*` schema with `repository_id text NOT NULL REFERENCES
    striatumd.repositories(repository_id)` on every repo-scoped table:
    `workflow_snapshots`, `runs`, `sessions`, `jobs`,
    `job_dependencies`, `queue_messages`, `leases`, `work_packets`,
    `artifacts`, `verdicts`, `blockers`, `command_requests`,
    `process_executions`, `events`, `job_worktrees`,
    `process_supervisors`, `process_supervisor_pointers`. (The prompt
    named 15 tables; `workflow_snapshots` and `job_dependencies` are
    required structural tables in `src/striatum/schema.py` and were
    added by the synthesis to avoid breaking
    `runs.workflow_snapshot_id` and job gating.) Index strategy is
    repository-prefixed versions of the current SQLite access paths
    plus the partial-unique constraints from prior migrations
    (`leases(repository_id, resource_type, resource_id) WHERE state =
    'active'`, `queue_messages` partial unique on
    `(repository_id, job_id) WHERE kind = 'work' AND state IN
    ('pending','claimed','acked')`, `process_supervisor*` partial
    unique on `(repository_id, session_id) WHERE state IN
    ('starting','attached','detached')`, etc). Same SQL file creates
    `striatumd.repo_migrations` checkpoint table
    (`repository_id`, `source_substrate`, `target_substrate`,
    `source_user_version`, `source_event_manifest_sha256`,
    `source_artifact_manifest_sha256`, `source_state_db_sha256`,
    `migrated_at`, `tombstone_path`, `row_counts jsonb`) and installs
    append-only trigger functions on `events` and `artifacts`,
    revoking `UPDATE` / `DELETE` on those tables from the daemon
    runtime role. `src/striatum/daemon_pg/migrations.py` bumps
    `LATEST_DAEMON_DB_VERSION` from 4 to 5 and registers the migration
    as `PgMigration(5, "repo-local workflow state",
    "0005_repo_local_workflow_state.sql")`.
  - **`striatum daemon migrate-repo-local` migration verb** with
    `--from sqlite --to pg --repo <path> --postgres-url <url>
    [--dry-run] [--keep-sqlite-readonly] [--confirm-delete] [--json]`.
    Body in `src/striatum/daemon_pg/repo_local_migration.py`
    (separate from `cutover.py` so daemon-registry cutover and
    repo-local workflow cutover stay distinct):
    `RepoLocalMigrationOptions`, `migrate_repo_local()`, and
    `compute_repo_local_reanchor()`. Algorithm: authorize daemon
    admin → resolve or implicitly register the repository → refuse if
    a `repo_migrations` row already exists (returns
    `already_migrated: true`) → open `.striatum/retired-local-state`
    read-only → verify `PRAGMA user_version ==
    striatum.migrations.LATEST_VERSION` → for full runs, copy every
    repo-scoped row in dependency order inside one `SERIALIZABLE`
    Postgres transaction → write the `repo_migrations` checkpoint
    inside the same transaction → commit → rename
    `.striatum/retired-local-state → retired-local-state.tombstone` with mode
    `0444` (default `--keep-sqlite-readonly`). If
    `--no-keep-sqlite-readonly` is supplied, deletion still requires
    `--confirm-delete`; otherwise the command refuses with exit code
    8. Dry-run path applies pending daemon migrations if needed, then
    reports source counts and manifest hashes without inserting
    repo-local rows. `compute_repo_local_reanchor` defines the
    byte-equivalence check: canonical JSON arrays of source rows
    ordered by stable primary key for `events` and `artifacts`,
    projected to source-column names and compact UTF-8 JSON, SHA-256
    must match between SQLite and Postgres. Daemon-command helper at
    `src/striatum/cli/daemon.py` (Track A) — full parser wiring of
    the subparser deferred to V1.5.
  - **Exit code 11 `daemon_unreachable` + exit code 12
    `repo_not_migrated` with named remediation.** `src/striatum/errors.py`
    introduces `DaemonUnreachableError` and `RepoNotMigratedError`
    plus an `EXIT_*` integer constant table for codes 1–15;
    `src/striatum/cli/daemon_required.py` (new) defines
    `enforce_daemon_required(command, repo)` and the canonical
    stderr / JSON-envelope refusal shapes. Exit 11 stderr lists four
    remediation channels (Linux systemd: `systemctl --user start
    striatumd`; macOS launchd: `launchctl bootstrap gui/$UID
    ~/Library/LaunchAgents/io.striatum.striatumd.plist`; foreground:
    `striatumd --foreground`; Postgres: `striatum daemon doctor
    --postgres-url <url>` or `STRIATUM_DAEMON_DB_URL`). Exit 12
    stderr names the single fix (`striatum daemon migrate-repo-local
    --from sqlite --to pg --repo <path>`). JSON envelope under
    `--json` carries `{"ok": false, "error": {"message": "...",
    "code": 11|12, "hint": "..."}}`. Activation is currently
    env-gated on `STRIATUM_DAEMON_REQUIRED=1`; flipping the default
    to enforced is part of the V1.5 follow-up (closes the CLI escape
    path). `DAEMON_OPTIONAL_COMMANDS` allowlist (`daemon`, `init`,
    `skills`, `plugin`) keeps doctor and lifecycle commands reachable
    without a daemon (RFC 0043 §3 acceptance criterion). Legacy V1
    RFC 0028 daemon errors renumbered to free codes 11 and 12
    (`DaemonAuthError → 14`, `DaemonCapabilityError → 15`); the older
    `DaemonUnreachableError` from `src/striatum/daemon.py` stays at
    code 10 with a docstring pointing at the new entry-layer error.
    Tests assert daemon errors by class name, not numeric exit code,
    so no test fixture broke on renumbering.
  - **`--no-daemon` retired.** Removed from
    `src/striatum/cli/parser.py`'s daemon mutual-exclusion group; no
    hidden alias. Argparse now exits 2 with `unrecognized arguments:
    --no-daemon` for the retired flag. `--daemon` remains as the V1
    RFC 0028 read-mode opt-in until daemon-mediated CLI dispatch
    absorbs it. New `tests/cli/test_no_daemon_retired.py` covers the
    rejection plus `--help` absence assertion.
  - **`.striatum/retired-local-state` retained read-only when
    `--keep-sqlite-readonly` is set** (mode `0444` tombstone at
    `.striatum/retired-local-state.tombstone`); otherwise the
    `--confirm-delete` flag deletes the source DB after the
    checkpoint commits. Post-migration `.striatum/` survives as
    operational scratch only — FIFOs, pidfiles, supervisor stdout,
    token cache, marker files — never as the live message bus.
  - **RFC 0030 method registry expanded for repo-local mutations.**
    `src/striatum/daemon_rpc/registry.py::_ENTRIES` and
    `src/striatum/daemon_rpc/server.py::CLI_ROUTES` widened to cover
    every mutation in `src/striatum/cli/mutations.py` per RFC 0043
    §5. New dotted vocabulary: `session.register`, `session.close`,
    `work.claim_next`, `work.ack`, `work.heartbeat`, `work.complete`,
    `work.block`, `work.release`, `work.send_message`,
    `artifact.publish`, `review.submit`, `review.verdict`,
    `review.override`, `decision.record`, `checkpoint.resolve`,
    `branch.confirm`, `run.prepare`, `run.start`, `run.pause`,
    `run.resume`, `run.cancel`, `run.retry_job`, `worktree.create`,
    `worktree.release`, `worktree.list`, `recovery.stale_leases`,
    `recovery.requeue_stale`, `recovery.cancel_job`,
    `recovery.process_reconcile`, `recovery.resume`, `recovery.auto`,
    `recovery.watch`, `supervise.start`, `supervise.send`,
    `supervise.stop`, `supervise.status`, `supervise.list`,
    `supervise.reattach_status`, plus the `workflow.*` and read-side
    surface (`status`, `why`, `doctor`, `dashboard`, `dashboard.all`,
    `evidence.export`, `corpus.export`, `run.summary`, `run.graph`,
    `list.*`). Daemon-global additions: `repo.list`,
    `daemon.migrate_repo_local`. Legacy undotted names (`ack`,
    `heartbeat`, `release`, `block`, `complete`, `publish_artifact`,
    `claim_next`, `verdict`, `submit_review`) kept as
    `deprecated=True` entries so in-flight clients keep resolving
    while callers migrate. New `tests/daemon_rpc/test_registry_rfc0043_coverage.py`
    is the exhaustiveness test: static map of mutation function names
    → RFC 0043 §5 method names, asserts every mutation has a
    registered method, every method's required capability matches §5,
    every canonical method either routes via `CLI_ROUTES` or sits in
    the inline allowlist, legacy aliases are flagged
    `deprecated=True`, and repo-scope modes (single_repo /
    cross_repo / daemon_global) match the synthesis.
  - **D094 supersession of D006 / D007 / D036 / SQLite half of D009
    is now executable.** The local-SQLite assumption baked into those
    earlier decisions no longer holds for repo-local workflow state;
    `.striatum/retired-local-state` is migration source or read-only
    tombstone only. RFC 0039's Go-core scope can now drop SQLite
    entirely (TODO item 25 marked unblocked).
  - **Files (uncommitted in this branch at merge time):** Track A —
    `src/striatum/daemon_pg/sql/0005_repo_local_workflow_state.sql`
    (new), `src/striatum/daemon_pg/repo_local_migration.py` (new),
    `src/striatum/daemon_pg/migrations.py` (modified for v5
    registration), `src/striatum/cli/daemon.py` (new daemon-command
    helper), `tests/daemon_pg/test_repo_local_migration.py` (new),
    `tests/fixtures/v1_repo_local_sqlite/` (new SQLite fixture).
    Track B — `src/striatum/cli/dispatch.py` (modified for
    `enforce_daemon_required` hook + dedicated `DaemonUnreachableError /
    RepoNotMigratedError` except arm + retired `args.no_daemon`),
    `src/striatum/cli/parser.py` (modified for `--no-daemon` removal),
    `src/striatum/cli/daemon_required.py` (new),
    `src/striatum/daemon.py` (modified for V1 daemon error
    renumbering 11/12 → 14/15), `src/striatum/daemon_rpc/registry.py`
    (modified for §5 vocabulary expansion),
    `src/striatum/daemon_rpc/server.py` (modified for `CLI_ROUTES`
    expansion), `src/striatum/errors.py` (modified for new error
    classes + `EXIT_*` constants), `tests/cli/__init__.py`,
    `tests/cli/test_no_daemon_retired.py`,
    `tests/cli/test_daemon_doctor_without_daemon.py`,
    `tests/exit_codes/__init__.py`,
    `tests/exit_codes/test_rfc0043_refusals.py`,
    `tests/daemon_rpc/__init__.py`,
    `tests/daemon_rpc/test_registry_rfc0043_coverage.py` (all new).
    Handoffs at `docs/dogfood/048/build/track_a/HANDOFF.md` and
    `docs/dogfood/048/build/track_b/HANDOFF.md`; combined handoff at
    `docs/dogfood/048/BUILD_HANDOFF.md`; operator narrative at
    `docs/dogfood/048/PHASE_1_OPERATOR_NOTES.md`.

### Decided

- D102 (`dec_0b953435368e40109e793378e1a75054`,
  `accepted_with_follow_up`): cycle-exhaustion override for the
  dogfood-048 build review. Codex `review_build_codex` returned
  `needs_revision severity=high` and gemini `review_build_gemini`
  returned `needs_revision severity=medium` — both with real findings
  (crash-recovery persistence gap between Postgres commit and SQLite
  tombstone rename; CLI escape path remains under the env-gated
  enforcement default; `daemon migrate-repo-local` subcommand body
  exists in `daemon_pg/repo_local_migration.py` but the parser
  subparser is not yet wired). Single accepting verdict claude
  `accept_with_findings` low (cross-lane scope-met envelope).
  **D102 is distinct from D095-D101 in finding character.** Prior
  cycle-exhaustion overrides have fallen into two anti-pattern
  families: (a) codex/codex implementer+reviewer co-blindness
  (D095 dogfood-042 Track A, D096 dogfood-042 Track C, D097
  dogfood-043, D098 dogfood-044, D100 dogfood-046) where the
  reviewer's findings cluster around the implementer's same blind
  spots; (b) codex-reviewer-of-claude-implementer baseline
  conservatism (D099 dogfood-045 reject critical, D101 dogfood-047
  needs_revision high) where codex applies threat_model-posture
  conservatism to a different model's work. D102 belongs to neither —
  the codex/codex pairing in Track A and the gemini reviewer both
  produced real findings on real scope gaps that the operator
  acknowledged and folded into V1.5 (TODO item 31). Codex+gemini
  findings absorbed into RFC 0043 V1.5; ships at V1 because the
  in-scope substrate-flip correctness contract is met and the
  remaining deltas are operator-side wiring + crash-recovery
  hardening, not architectural defects. Two run-quality regressions
  surfaced and were operator-recovered: (1) **3rd
  `claude-no-artifact` instance** — claude reviewer's session
  composed no REVIEW.md artifact in `docs/dogfood/048/review/build/
  claude/`; operator composed the verdict on-behalf with attribution
  preserved. (2) **3rd `gemini-no-frontmatter` instance** — gemini
  reviewer's REVIEW.md was missing `striatum.finding.v1` front
  matter; operator-fixed inline. Operator also performed SQL surgery
  on `artifacts.logical_name` in the live `.striatum/retired-local-state`
  because the on-behalf publish call had passed the wrong logical
  name during recovery (the artifact's underlying file path was
  correct but the `logical_name` column needed a one-row UPDATE to
  align with the workflow's `expected_artifacts[]` entry). All three
  recurrences are now well-characterized enough that the operator
  recovery scripts under `striatum recovery resume / surgical_recovery`
  should grow targeted helpers for them in a future pass.



### Added

- RFC 0039 V1.5 — Go daemon correctness slice F1-F5 landed under
  dogfood-047. Implementation order respected the synthesis lock
  **F5 → F4 → F1 → F2 → F3** because F4 and F1 needed F5's
  parameter-binding and transaction support before they could land.
  - **F5 — Pure-Go PostgreSQL driver.** `go/pkg/db/connection.go`
    rewritten on top of `github.com/jackc/pgx/v5 v5.7.2` — the Go
    daemon's first third-party runtime dependency. New `db.Runner`
    and `db.TxRunner` interfaces expose parameterized `Exec`,
    `QueryRow`, `QueryScalar`, and (Runner-only) `BeginTx`;
    `PgxRunner` and `PgxTxRunner` are the concrete adapters; `db.Row`
    is a type alias for `pgx.Row` so `rpc` can reference the row
    type without an import cycle. Pool configured with
    `application_name = "striatumd-go/<daemon_version>"`, default
    `statement_timeout = 60000`, and
    `DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol` (simple
    protocol is required because existing migration files contain
    multi-statement DDL; pgx still binds parameters with safe
    client-side quoting under simple protocol, so the SQL-injection
    surface is unchanged). `PsqlRunner`, `exec.Command("psql", ...)`,
    and `fmt.Sprintf` literal interpolation are removed from
    production code paths. `RedactURL` and `ResolveConfig` keep their
    existing contracts; no code path logs raw Postgres URLs.
  - **F4 — Transactional audit append.**
    `go/pkg/db/audit.go::AuditRecorder.RecordRPC` opens one
    `READ COMMITTED` transaction via the F5 runner, locks the
    singleton `striatumd.audit_chain_head` row with
    `SELECT ... FOR UPDATE`, derives the open audit segment
    (creating one only if absent — `0001_baseline.sql` bootstraps an
    open segment so the create branch is dead in practice but
    defends against operator-side cleanup that closes the open
    segment without opening a new one), computes the v2 row hash
    from the locked `previous_hash`, inserts the audit row with
    `INSERT ... RETURNING audit_id`, updates
    `striatumd.audit_chain_head` to the new id and hash, commits,
    and returns the audit id as `strconv.FormatInt`. Rollback fires
    from a deferred function whenever Commit was not reached.
    Public API of `RecordRPC` is unchanged so
    `go/pkg/rpc/server.go` keeps calling it after response
    construction, and the returned `audit_id` flows into the RFC
    0030 response envelope — closing the V1 envelope-shape
    regression where the Go core returned empty `audit_id` to
    clients. Row-hash payload matches the Python `v2_row_hash`
    byte-for-byte: nullable strings encode as JSON `null`,
    `exit_code` is an int when present, `segment_id` is an int64,
    `ts` is RFC3339 truncated to the second.
  - **F1 — Postgres-backed RPC authorization (replaces
    `AllowAllAuthorizer` in production).**
    `go/pkg/rpc/auth_pg.go` introduces `PostgresAuthorizer`. Token
    secrets are HMAC-SHA256(`token_salt`, supplied secret) compared
    with `subtle.ConstantTimeCompare` against the stored
    `token_hash`; capability lookup mirrors
    `src/striatum/daemon_rpc/capability.py` exactly (same WHERE
    clause, same wildcard ordering, same scope-mismatch fallback
    query); revocation and expiry take effect on the next request;
    no positive or negative cache ships in V1.5. The denial-reason
    vocabulary is identical to the Python authorizer so clients
    cannot tell the two cores apart from the refusal envelope.
    `go/cmd/striatumd/main.go` wires
    `&rpc.PostgresAuthorizer{Runner: pool.Runner, Clock: time.Now}`
    whenever a Postgres URL is configured. `AllowAllAuthorizer{}`
    is now strictly the test default. Implementation deviates from
    the synthesis field type to keep `rpc → db → rpc` from becoming
    a cycle: `auth_pg.go` declares a local `rpc.AuthQuerier`
    interface using `pgx.Row`; `db.Runner` satisfies it
    structurally, so `main.go` still passes `pool.Runner` directly.
  - **F2 — Go harness launch contract.**
    `go/cmd/striatumd/main.go` accepts the synthesis-locked flag
    surface: `--socket`, `--postgres-url`, `--migrate`,
    `--describe`, and the new optional
    `--migrations-sha-source` which compares embedded migration
    file hashes against the SQL files at the supplied path before
    serving and exits non-zero on drift (replaces V1's
    `--migrations-dir` reloader without giving up the drift
    signal). `go/Makefile` writes the binary to `go/bin/striatumd`
    (V1 emitted `go/striatumd`, which the harness probed at
    `go/bin/striatumd` — fixed). `tests/_harness/daemon.py` builds
    via `make -C go build` when the binary is missing and honors
    the `STRIATUMD_GO_BIN` developer override;
    `_start_go` launches with the locked argv
    `--socket <sock> --postgres-url <url> --migrations-sha-source
    src/striatum/daemon_pg/sql` (no `--db-url`, no
    `--migrations-dir`). The narrow launch regression is
    `tests/test_daemon_go_smoke.py`: constructs
    `MultiRepoHarness(daemon_core="go")`, asserts the socket
    exists, runs `daemon.hello` and `daemon.describe`, and verifies
    the audit chain head moved.
  - **F3 — `make test-multi-repo CORE=go` wired + pytest
    parametrization.** Top-level `Makefile` exposes
    `CORE ?= python` and forwards it as
    `STRIATUM_MULTI_REPO_DAEMON_CORE` into pytest;
    `tests/conftest.py` adds a class-scoped `daemon_core` fixture
    that reads `STRIATUM_MULTI_REPO_DAEMON_CORE` (raising
    `pytest.UsageError` on unknown values) and threads it through
    `MultiRepoHarness`. New tests
    `tests/test_daemon_go_smoke.py` and
    `tests/test_daemon_go_audit.py` join the `test-multi-repo`
    target list; both skip when
    `STRIATUM_MULTI_REPO_DAEMON_CORE != "go"` so they do not break
    `CORE=python` runs. CI shape is the synthesis-locked **two
    explicit jobs** (`make test-multi-repo CORE=python` and
    `make test-multi-repo CORE=go`) rather than in-process pytest
    parametrization — Go-core failures surface as separately-named
    jobs rather than as parametrized subtests.
  - Files: `go/cmd/striatumd/main.go`, `go/pkg/db/audit.go`,
    `go/pkg/db/connection.go`, `go/pkg/db/migrations.go`,
    `go/pkg/db/migrations_test.go`, `go/pkg/db/audit_race_test.go`
    (new, opt-in on `STRIATUM_PG_TEST_URL`),
    `go/pkg/rpc/auth_pg.go` (new), `go/Makefile`, `go/go.mod`,
    `tests/_harness/daemon.py`, `tests/conftest.py`, `Makefile`,
    `tests/test_daemon_go_smoke.py` (new),
    `tests/test_daemon_go_audit.py` (new),
    `docs/rfcs/0039-go-daemon-core.md` (V1.5 deltas section).
- Operator-side ergonomics: `striatum --version` flag — prints
  `striatum <version>` and exits zero; wired in
  `src/striatum/cli/parser.py`. Separate from the V1.5 packet but
  rides along on the `striatum/dogfood-047-rfc-0039-v1-5` branch.
- Item 63 (TODO sweep results): items 3, 14, 18 promoted to
  ✅ done after the snapshot table review; items 1, 2, 13 retain
  🟡 most done status with named gaps captured in the per-item
  bodies (item 1 PTY path; item 2 sandbox/worktree adapter for
  mechanical `network`/`repo_scope` enforcement promotion; item 13
  runner-owned design+build+review fixture under `examples/`).
- Item 13 partial: `examples/three-lane-design-build-review/`
  runner-owned workflow fixture scaffolded (workflow.json, roles,
  prompts, README) reproducing the historical P001 three-lane
  shape against the standalone product surface — last operator
  step before the tmux harness fully retires from active workflow
  guidance.
- Pre-scaffold: `docs/dogfood/048/` (workflow.json, roles,
  prompts, OPERATOR_REPORT.md skeleton) staged for RFC 0043 V1
  (2-track: codex schema/migration + claude CLI/RPC). Not started
  in this packet; rides along on the branch so the next dogfood
  has the directory structure ready.

### Decided

- D101 (`dec_f8d268f392ca44dd8a9bccb634249979`,
  `accepted_with_follow_up`): override for the dogfood-047 build
  review. Codex `review_build_codex` returned `needs_revision
  severity=high` under the threat_model posture on five findings
  (F1 `go.sum` not regenerated for the new `pgx/v5` runtime
  dependency, F2 unauthenticated/no-audit production fallback when
  no `--postgres-url` is configured, F3 `make test-multi-repo
  CORE=go` can pass with all tests skipped, F4 smoke-test asserts
  no denial reason on unauthenticated `daemon.describe`, F5
  audit-append race regression not executable without
  `STRIATUM_PG_TEST_URL`). Cross-lane majority disagreed (claude
  `accept_with_findings` low ergonomics_dx, gemini
  `accept_with_findings` medium threat_model); 2-of-3 cross-lane
  consensus said scope was met. **D101 is distinct from D095-D100
  codex/codex co-blindness anti-pattern** — this dogfood
  deliberately routed implementation to **claude** (Go + Python
  harness mix), so the reviewer was scrutinizing a different model's
  work. This is the **codex-reviewer-of-claude-implementer pattern**
  first surfaced under D099 (dogfood-045, RFC 0038 V1.5): codex-as-
  reviewer baseline conservatism appears to be independent of the
  codex/codex convergent-blind-spot anti-pattern, and now has two
  instances on the books (D099 reject critical, D101
  needs_revision high). Codex findings F1-F5 are real but the
  V1.5 slice meets the in-scope correctness contract and ships;
  findings absorbed into RFC 0039 V1.6 follow-up (TODO item 30).

### Notes

- Dogfood-047 ran the multi-track design + build + review workflow
  for RFC 0039 V1.5 with the codex/codex anti-pattern explicitly
  avoided by routing implementation to claude. As with
  dogfood-044/045/046, the `consolidate` job was not part of the
  workflow; the operator authored this changelog entry,
  `docs/rfcs/README.md` status update, `docs/TODO.md` item-24
  promotion + new item 30 follow-up + F48 snapshot row,
  `docs/dogfood/047/BUILD_HANDOFF.md` (combined handoff per the
  consolidate-job-absent pattern), and
  `docs/dogfood/047/PHASE_1_OPERATOR_NOTES.md` out-of-band after
  the run.
- The codex/codex anti-pattern is now well-characterized across
  five instances (D095, D096, D097, D098, D100), and the
  codex-reviewer-of-claude-implementer pattern across two
  instances (D099, D101). The refuse-by-default validator rule for
  same-model implementer↔reviewer pairing (TODO item 26) remains
  deferred. For dogfood-047 the operator-side mitigation (route
  implementation to claude when the reviewer set includes codex
  with threat_model posture) is the same one used in dogfood-045,
  and produced the same outcome shape: codex reviewer comes back
  harsh on a different model's work; cross-lane majority overrides.
- The HANDOFF documents a verification gap on the implementer side:
  `striatum ack` and other Bash commands were denied by the harness
  permission gate, so no `make lint` / `make typecheck` / `make
  test` / `go test ./...` / `go mod tidy` / `make test-multi-repo`
  / binary smoke ran during the implementer session. The
  implement-prompt escape hatch ("If `striatum ack` is denied,
  write the HANDOFF and exit normally") governed the rest of the
  run. The codex review's F1 finding (`go.sum` not regenerated)
  follows directly from this gap: the `go.mod` was hand-edited
  with the canonical `pgx v5.7.2` line, but `go.sum` cryptographic
  hashes were not generated. Operator-side or CI follow-up: run
  `(cd go && go mod tidy)` and commit the resulting `go.sum`
  before merge so `make daemon-go-build` succeeds (folded into RFC
  0039 V1.6, TODO item 30).

## v1.35.0 — 2026-05-13

### Added

- RFC 0044 V1 — Striatum-side corpus export landed under dogfood-046.
  New `striatum corpus export --since <ref> --out <dir> [--json]` CLI
  verb wired in `src/striatum/cli/parser.py` and dispatched through
  `src/striatum/cli/dispatch.py`. New `src/striatum/corpus/` package
  splits the export into focused modules: `types.py`
  (`SUB_KINDS` / `JSONL_FILES` closed mapping for the nine JSONL
  bundle files; `CorpusBundleResult.to_json` shape with
  `status="exported"`, repo-relative `manifest_path`, `out`, `since`
  ref + resolved commit, `row_counts`, `bundle_sha256`), `git.py`
  (`resolve_commit` via `git rev-parse --verify <ref>^{commit}`),
  `enumerator.py` (durable-provenance source enumeration over RFCs,
  decisions, commits, operator reports, changelog, ubiquitous-language
  terms, harness-friction rows, run summaries; no SQLite blobs, no
  `FROM runs|FROM verdicts|FROM artifacts|FROM jobs|FROM sessions`
  queries from the live state DB), `redaction.py` (denylist-based
  source-path refusal for `.env`, `.env.local`, `keys/private.pem`,
  `.striatum/retired-local-state`, `transcripts/`, `raw_model_output/`,
  `docs/transcript.txt`; co-author-email + 64-char-token scrubbing on
  commit messages; redaction policy enforces no-secrets/no-PII so the
  JSONL bundle is Engram-compatible), `writer.py` (deterministic
  JSONL emission with canonical UTF-8 newline normalization),
  `manifest.py` (per-file SHA-256 + row counts + repo HEAD +
  dirty-tree flag + `since` ref + schema version + `generated_at` —
  manifest hashes cover post-redaction bytes), and `export.py`
  (orchestrator that refuses `--out` outside the repo, under
  `.striatum/`, or pointing at a file; resolves `--since` before
  writing; verifies row counts and SHA-256s after emission; returns
  the standard CLI JSON envelope). Tests:
  `tests/test_corpus_enumerator.py`, `tests/test_corpus_redaction.py`,
  `tests/test_corpus_writer.py`, `tests/test_corpus_manifest.py`,
  `tests/test_cli_corpus_export.py` (incl.
  `test_corpus_export_cli_success_and_manifest`,
  `test_corpus_export_invalid_since_returns_json_error_code_8`,
  `test_corpus_export_rejects_bad_output_targets`,
  `test_no_engram_imports_or_memory_capabilities_in_striatum`),
  `tests/test_corpus_export_integration.py` (incl.
  `test_corpus_export_replays_with_stable_jsonl_hashes` — the
  RFC 0044 §3 acceptance test: byte-equality on JSONLs across two
  CLI invocations into different `--out` dirs, manifest equality
  after stripping `generated_at`). 31/31 corpus-targeted tests
  green; full suite 739 passed / 33 skipped with one pre-existing
  documentation-budget failure in `tests/test_doc_links.py` outside
  this packet's write scope. The corpus package is imported lazily
  from the dispatch branch so unrelated verbs do not pay its
  startup cost. Augmentation-not-dependency boundary is pinned by
  `test_no_engram_imports_or_memory_capabilities_in_striatum` —
  asserts `import engram` / `from engram` / `memory.` absent across
  `src/striatum/corpus/`, `src/striatum/cli/`,
  `src/striatum/daemon_rpc/`, `src/striatum/daemon_pg/`,
  `src/striatum/mcp.py`, `src/striatum/service.py`, and
  `pyproject.toml`. Scope was **Striatum-side ONLY**; the
  Engram-side ingester (`engram ingest-striatum`), the standalone
  `engram-mcp-stdio` MCP server, the four read-only retrieval tools
  (`engram.search`, `engram.fetch_reference`, `engram.describe_corpus`,
  `engram.health`), and the Engram-local `memory.*` capabilities are
  explicitly out of scope and live in `~/git/engram/` as a separate
  effort. Implementer was **codex** (Python) — 5th consecutive
  codex-as-implementer dogfood where the codex/codex reviewer
  pairing converged on its own findings (precedents D095 dogfood-042
  Track A, D096 dogfood-042 Track C, D097 dogfood-043, D098
  dogfood-044). Files:
  `src/striatum/cli/parser.py`, `src/striatum/cli/dispatch.py`,
  `src/striatum/corpus/__init__.py`, `src/striatum/corpus/types.py`,
  `src/striatum/corpus/git.py`, `src/striatum/corpus/enumerator.py`,
  `src/striatum/corpus/redaction.py`, `src/striatum/corpus/writer.py`,
  `src/striatum/corpus/manifest.py`, `src/striatum/corpus/export.py`,
  `tests/test_corpus_enumerator.py`, `tests/test_corpus_redaction.py`,
  `tests/test_corpus_writer.py`, `tests/test_corpus_manifest.py`,
  `tests/test_cli_corpus_export.py`,
  `tests/test_corpus_export_integration.py`, `tests/test_web_ui.py`
  (test-only `Traversable.read_text(errors=...)` → `read_bytes().
  decode(..., errors=...)` compatibility adjustment so `make
  typecheck` passes under the current `importlib.resources` typing
  surface).

### Decided

- D100 (`dec_b3b26d4c86df408ab75f4cf515a82d1e`,
  `accepted_with_follow_up`): cycle-exhaustion override for
  dogfood-046 build review. **5th codex/codex anti-pattern
  instance.** Codex `review_build_codex` returned
  `needs_revision severity=high` under the threat_model posture on
  redaction completeness + JSONL secret leakage. Gemini
  `review_build_gemini` returned `needs_revision severity=medium`
  under threat_model posture — but every gemini finding (A1
  contradictory capability spec, A2 lack of authorization in
  `fetch_reference`, A3 cross-repository context leakage via shared
  `corpus_id`, A4 redaction bypass in curated artifacts via memory
  poisoning, A5 `describe_corpus` metadata leakage) targeted the
  Engram-side surface (MCP server, ingester, capability model)
  which is **OUT OF SCOPE** for this dogfood — none of those
  components ship in `src/striatum/` this run. Claude
  `review_build_claude` returned `accept_with_findings severity=low`
  on the in-scope Striatum-side surface (ergonomics_dx posture:
  five discoverability findings F1-F5, all low, none blocking
  function). Single accepting verdict + 2 out-of-scope/anti-pattern
  needs_revisions; impl meets V1 scope acceptance criteria. Codex
  findings (redaction policy specification, manifest privacy-safe
  paths, canonical JSONL serialization + hash coverage, MCP output
  redaction) are absorbed back into RFC 0044's threat model and
  forwarded to the Engram-side follow-up. Gemini findings are
  forwarded to `~/git/engram/` since they describe the Engram-side
  threat surface Striatum is not building.

### Notes

- Dogfood-046 ran the multi-track design + build + review workflow
  for RFC 0044 V1 with the Striatum-side scope only. As with
  dogfood-044/045, the `consolidate` job was not part of the
  workflow; the operator wrote this changelog entry, the
  `docs/rfcs/README.md` status update, the `docs/TODO.md` item-23
  promotion + new F47 row, `docs/dogfood/046/BUILD_HANDOFF.md`, and
  `docs/dogfood/046/PHASE_1_OPERATOR_NOTES.md` out-of-band after
  the run.
- **Claude reviewer produced no on-disk artifact** — only a 3.8 KB
  packet log was emitted. The operator composed a minimal
  `accept_with_findings` review at
  `docs/dogfood/046/review/build/claude/REVIEW.md` from the
  packet-log content to unblock the workflow. This is the **6th
  distinct anti-pattern instance** the dogfood loop has
  surfaced — distinct from both the codex/codex co-blindness
  (D095-D098, D100) and the codex-threat_model-reviewer harshness
  (D099). The reviewer-emits-no-artifact pattern is a new harness
  failure mode: the run cannot proceed without a published review
  artifact, and there is no current operator-recovery surface short
  of writing the artifact by hand. Forwarded to the harness
  improvement RFC backlog along with the codex/codex anti-pattern
  (TODO item 26).
- **Gemini byline-prefix bug surfaced AGAIN.** This is a recurrence
  of the dogfood-044 gemini reviewer profile bug: gemini emitted
  no front-matter YAML block at all, and used the non-conformant
  byline `**Author:** Gemini (Reviewer)` (markdown bold form)
  instead of the required plain `author: <slug>` byline.
  `docs/dogfood/046/review/build/gemini/REVIEW.md` was therefore
  operator-rewritten to preserve gemini's substantive review
  content while adding the required `striatum.finding.v1` front
  matter + a plain `author: reviewer-unknown-model-001` byline.
  The dogfood-044 gemini reviewer profile fragment update did not
  fully fix this — gemini still drops the front matter and still
  reaches for markdown-bold author lines. Forwarded to the
  reviewer-profile audit follow-up alongside the codex
  threat_model harshness pattern from D099.

## v1.34.0 — 2026-05-13

### Added

- RFC 0038 V1.5 — web UI integration gaps landed under dogfood-045.
  (F1) `placeholderIslandPlugin` removed from
  `src/striatum/web/frontend/vite.config.ts`; `plugins` is now
  `[react()]`. `manifest` flipped to `false` so the build no longer
  emits `.vite/manifest.json`; the existing `manifest.sha256` remains
  the single committed manifest. A new `make ui-verify-bundle` target
  rejects (a) any stable island entry whose body contains the V1
  sentinel `Striatum frontend island placeholder loaded`, (b) any
  `island-shared-*.js` chunk containing the same sentinel, and
  (c) any stable island entry under 1024 bytes (unless a sibling
  `island-shared-*.js` chunk ≥ 1024 bytes covers the legitimate
  factored-chunk case). `make ui-check-bundle` now depends on both
  `ui-build` and `ui-verify-bundle`. Python sentinel guard
  `tests/test_web_ui.py::test_island_bundles_have_no_placeholder_sentinel`
  reads each stable island bundle through `importlib.resources` and
  asserts the sentinel is absent so the guard survives `pip install`.
  (F2) `/workflows/new` chooser prop-contract fix: the
  `/workflow-templates` route is unchanged (it already returns
  `{"ok": true, "data": {"templates": list_templates(kind=kind)}}`);
  `src/striatum/web/frontend/src/shared/types.ts` adds
  `WorkflowTemplate` + `WorkflowTemplateListResponse` mirroring the
  server fields and removes the dead `WorkflowShape` /
  `WorkflowLaneSet` / `WorkflowTemplateCatalog` types.
  `WorkflowChooser.tsx` reads `res.data.templates`, partitions by
  `kind`, derives `shape` from the picked `kind: "shape"` row's
  `template_id`, pre-fills `lane_set` from the first overlapping
  `default_lane_sets` entry, and drops the V1 modifier UI (the server
  never returned `catalog.modifiers`). Wizard is now four steps:
  Template → Details → Preview → Save. `__testing` exports `buildSpec`
  + `recommendedForText`; the V1 `isModifierEnabled` export is gone.
  (F3) Island-shared double-mount fix: new
  `src/striatum/web/frontend/src/shared/island-shared-entry.ts`
  (`import "./theme.css"; export {};`) is the new Rollup input for the
  `island-shared` bundle. `src/main.ts` still exists for the Vite dev
  server (`make ui-dev`) but is no longer a production Rollup input,
  so it cannot mount islands twice. Vitest regression
  `src/striatum/web/frontend/src/__tests__/island-shared-no-mount.test.ts`
  mocks `react-dom/client.createRoot`, imports the shared entry plus
  the chooser entry into a JSDOM page with only
  `#island-workflow-chooser`, and asserts `createRoot` is called
  exactly once. (F4) Vite output semantics aligned with the
  package-data layout: output stays at `src/striatum/web/static/build/`,
  public URLs unchanged, and `pyproject.toml`
  `[tool.setuptools.package-data]` already matches the `manifest: false`
  layout (`"striatum.web.static" = [..., "build/*.js", "build/*.css",
  "build/*.sha256"]` + explicit `"striatum.web.static.build" = ["*.js",
  "*.css", "*.sha256"]` sub-package entry). New
  `tests/test_web_workflows.py::test_workflows_edit_renders_graph_editor_island`
  pins `/workflows/edit/<path>`. Supply-chain hygiene: `ui-install`
  now uses `npm ci` (lockfile-reproducible installs); new
  `ui-update-lock` for intentional dependency bumps; new `ui-audit`
  runs `npm audit --audit-level=high`;
  `src/striatum/web/frontend/npm-audit-baseline.json` ships as the
  accepted-findings tracker.
  Files: `Makefile`, `src/striatum/web/frontend/vite.config.ts`,
  `src/striatum/web/frontend/src/shared/api-client.ts`,
  `src/striatum/web/frontend/src/shared/types.ts`,
  `src/striatum/web/frontend/src/shared/island-shared-entry.ts` (new),
  `src/striatum/web/frontend/src/islands/workflow-chooser/WorkflowChooser.tsx`,
  `src/striatum/web/frontend/src/__tests__/workflow-chooser.test.ts`,
  `src/striatum/web/frontend/src/__tests__/workflow-chooser-fetch.test.tsx` (new),
  `src/striatum/web/frontend/src/__tests__/island-shared-no-mount.test.ts` (new),
  `src/striatum/web/frontend/npm-audit-baseline.json` (new),
  `tests/test_web_ui.py`, `tests/test_web_workflows.py`. Implementer
  was **claude** (TypeScript/Vite work), the first dogfood deliberately
  not using codex as implementer to avoid the codex/codex anti-pattern
  (precedents D095-D098). Real-bundle commit and `make` verification
  remain operator-side follow-up — the new sentinel/size guard + Python
  resource test refuse another placeholder commit from reaching CI.

### Decided

- D099: Reject override for the dogfood-045 build review
  (`dec_ccfa1685878d41d69ccc6496cd6612fd`, `accepted_with_follow_up`).
  Codex `review_build_codex` returned `reject severity=critical` under
  the threat_model posture; cross-lane consensus disagreed (claude
  `accept_with_findings` medium, gemini `accept` low). Codex critical
  rests on (a) committed bundles still being V1 placeholders pending
  operator-side rebuild, (b) build verification gates not executed in
  the implementer run, and (c) source-side mitigations being unproven
  against real output. The HANDOFF explicitly documents the
  real-bundle commit as an operator follow-up and the new sentinel
  guard refuses to ship another placeholder commit, so the
  cross-lane 2-of-3 majority overrides. Codex findings absorbed into
  RFC 0038 V1.6 follow-up (TODO item 29). First dogfood with a
  codex-reviewer-of-claude-implementer pattern (not codex/codex);
  the harsh codex verdict suggests codex-as-reviewer baseline
  conservatism is independent of the codex/codex convergent
  blind-spot anti-pattern. Recovery path on this run was non-trivial:
  the codex reject pushed the run state to `failed`, requiring SQL
  surgery + `striatum verdict --override` to recover.

### Notes

- Dogfood-045 ran the 9-job single-track workflow for RFC 0038 V1.5
  (F1-F4 + supply-chain hygiene findings from dogfood-041 deferred
  under cycle-exhaustion override
  `dec_251e8a5f3d674c409de0dad9eacd5844`). Like dogfood-044, the
  `consolidate` job was not in the workflow; the operator authored
  this changelog entry, `docs/rfcs/README.md` status update,
  `docs/TODO.md` follow-ups, `docs/dogfood/045/BUILD_HANDOFF.md`, and
  `docs/dogfood/045/PHASE_1_OPERATOR_NOTES.md` out-of-band.
- The codex review verdict surfaced an operator-facing harness gap:
  a reviewer `reject` verdict transitions the run state to `failed`
  before the operator can decide whether to override. Recovery here
  required SQL surgery on the verdict + run state followed by
  `striatum verdict --override` (the override-accepting verdict path
  landed in v1.32.x). A future RFC could plumb an explicit
  "operator-pending" run state distinct from `failed` so verdicts
  awaiting override do not require manual SQL recovery.

## v1.33.0 — 2026-05-13

### Added

- RFC 0040 V1.5 — daemon-side dispatch + composite tools + watcher
  invocation landed under dogfood-044. (F1) Daemon MCP `tools/call`
  now dispatches through the RFC 0030 method registry via new
  `src/striatum/daemon_pg/mcp_dispatch.py::dispatch_mcp_tool_call`
  which owns lookup, capability authorization, envelope build, and
  routing through `DaemonRpcRouter.handle(...)`; the previous stub
  that returned a fake `ok: true` is gone. `DaemonRpcRouter.handle`
  accepts `transport` (default `"rpc"`; MCP passes `"mcp"`) and
  `require_handshake` (default `True`; MCP passes `False`). Audit
  rows are post-dispatch: unknown methods + authorization denials
  emit one `transport="mcp"` deny row; allowed calls emit exactly
  one row carrying the real handler exit code. MCP response shape
  `{content, structuredContent, isError}` preserved; structuredContent
  carries `ok`, `method`, `audit_id`, and `data` on success.
  (F2/F3) `dogfood.publish_on_behalf` runs ack/publish/verdict inside
  one outer `with transaction(conn):` block via new transaction-free
  helpers `_ack_on_behalf_locked`, `_publish_artifact_locked`,
  `_record_verdict_locked`, and `_complete_locked`; review jobs
  require `verdict` up front (validated against the same enum the
  direct-Python helper accepts); `findings_artifact_id` defaults to
  the published artifact id when kind=`finding`; on success exactly
  one `dogfood.publish_on_behalf` event is inserted in-transaction
  with `composition_steps` covering ack/publish/verdict-or-complete;
  on failure the transaction rolls back and a best-effort
  `dogfood.publish_on_behalf_failed` event is written tagged
  `outcome: "rolled_back"`. (F4) New
  `src/striatum/process_progress.py::progress_loop_once` runs one
  bounded supervised-progress pass per repository joined to the
  `runs` table so only attached supervisors under running/paused
  runs tick; called from `daemon.daemon_sweep_once` inside
  `connect_repo(repo)` immediately before per-run auto-sweep work
  and folded into the sweep return payload as `"progress"`. The
  loop materializes each row as a `SupervisedProgressTarget` and
  ticks `SupervisedProgressWatcher`, whose heartbeat callback calls
  `striatum.cli.mutations.heartbeat` on the same repo connection.
  Metadata-only events:
  `supervisor.progress_watcher_heartbeat`,
  `supervisor.progress_watcher_idle`,
  `supervisor.progress_watcher_lost`. Log contents are never read.
  (F5) `ProcessProgressConfig.startup_grace_seconds` defaults to
  60 s; within grace a missing scratch path returns `waiting_for_log`
  with no warning; watcher catches `FileNotFoundError`/`OSError`
  while scanning `*.log` files so rotated logs follow without
  recreating the target; loop accepts a `should_stop` predicate and
  checks it between supervisors so SIGTERM cannot start a new
  heartbeat after shutdown; `progress_advisory_lock(repo,
  job_id=...)` is shared with `surgical_recovery` (watcher tick
  returns `lock_busy`, surgical recovery returns
  `progress_lock_busy`); PID-reuse guard via `process_start_time(pid)`
  flips the row to `state='lost'` on mismatch versus stored
  `pid_start_time` and emits `supervisor.progress_watcher_lost`.
  (F6) New `tests/test_mcp_dogfood_e2e.py` drives MCP `tools/call`
  round-trips for `dogfood.publish_on_behalf` covering completion
  and review-verdict paths (marked `pytest.mark.multi_repo`);
  `tests/test_supervised_progress_watcher.py` extended with
  `test_progress_loop_once_heartbeats_attached_supervisor` +
  `test_progress_loop_once_refuses_pid_identity_mismatch`. Files:
  `src/striatum/mcp.py`, `src/striatum/process_progress.py`,
  `src/striatum/db.py`, `src/striatum/daemon.py`,
  `src/striatum/daemon_pg/mcp_dispatch.py` (new),
  `src/striatum/daemon_rpc/server.py`,
  `src/striatum/dogfood/operator_tools.py`. Tests: 42 passed,
  10 skipped (multi_repo skips without PG harness).

### Decided

- D098: Cycle-exhaustion override for the dogfood-044 build review.
  4th instance of the codex/codex implementer+reviewer
  convergent-blind-spot anti-pattern (precedents D095 dogfood-042
  Track A, D096 dogfood-042 Track C, D097 dogfood-043 Python
  build). Codex needs_revision overridden; cross-lane claude
  accept_with_findings (medium), gemini accept (low). Codex
  findings absorbed into RFC 0040 V1.6 follow-up (TODO item 28).
  Anti-pattern now well-characterized across four independent
  runs; refuse-by-default validator rule (TODO item 26) remains
  the deferred half.

### Notes

- Dogfood-044 ran the 9-job single-track workflow for RFC 0040
  V1.5 (F1-F6 codex findings from dogfood-040). The `consolidate`
  job was not present in the workflow; the operator authored this
  changelog entry, `docs/rfcs/README.md` status update,
  `docs/TODO.md` follow-ups, `docs/dogfood/044/BUILD_HANDOFF.md`,
  and `docs/dogfood/044/PHASE_1_OPERATOR_NOTES.md` out-of-band
  (dogfood-043 lesson applied).
- Stale-lease intervention: codex finished writing code, but the
  supervisor lease expired at ~30 min default before the
  HANDOFF.md was published; operator composed the build HANDOFF
  on behalf of the implementer (per-finding status read from
  source). 30-min default-lease issue noted as a harness gap
  separate from the V1.5 implementation scope.
- Byline-prefix bug observed in 3 of 4 reviewed dogfoods now:
  both gemini and claude reviewers emit
  `(role)-lane-unknown-model-NN` instead of
  `(role)-unknown-model-NN`. Operator hand-edited the bylines
  before publication.

## v1.32.0 — 2026-05-13

### Added

- RFC 0045 V1 multi-phase workflow schema landed
  (`striatum.workflow.v1.1`): new top-level `phases` array, a
  `phase_synthesis` job type that gates phase transitions, validator
  rules refusing cross-phase dependencies that bypass the synthesis
  gate, runtime materialization of phase synthesis fan-in edges,
  `status --json` derives `phases` + `current_phase_id` from the
  workflow snapshot plus latest job attempts, dashboard + service
  run-detail surfaces receive phase progress from the status payload,
  workflow generator gains `shape: "multi_phase"` emitting v1.1
  workflows with phased track jobs + synthesis gates, and
  `striatum workflow upgrade --add-phases` previews by default and
  writes with `--apply`. V1 workflows continue to validate and run
  unchanged. Files: `src/striatum/workflow.py`,
  `src/striatum/cli/{introspect,mutations,parser,dispatch,workflow}.py`,
  `src/striatum/{dashboard,service}.py`,
  `src/striatum/workflow_generator/{core,catalog}.py`,
  plus tests under `tests/test_workflow_phases.py`,
  `tests/test_workflow_generator.py`, `tests/test_workflow_upgrade.py`,
  `tests/test_cli_mvp.py`, `tests/test_dashboard.py`,
  `tests/test_service.py`, and fixture
  `tests/fixtures/multi_phase_workflow.json`.
- RFC 0045 V1 React Flow editor extensions (Track B): phase color
  bands rendered via `<ViewportPortal>` so bands pan/zoom with nodes;
  cross-phase edges receive distinct styling
  (`className: "cross-phase-edge"`, thick black stroke,
  `data: { crossPhase, sourcePhase, targetPhase }`); new
  `PhaseInspector` swaps into the right-hand inspector slot when a
  band header is clicked (edit `title`/`description`, show
  `synthesis_job_id`, list jobs in phase); drag-drop refuses
  cross-band moves with snap-back + inline `role="alert"` error;
  new `phase` selector in the job inspector (gated on
  `workflow.phases?.length > 0`); `syncWorkflowEdges` strips
  derived `crossPhase`/`sourcePhase`/`targetPhase` keys on save;
  `selectedJobId` upgraded to a `GraphSelection` discriminated union.
  V1 workflows keep the original square-grid layout, thin grey edges,
  and job-only inspector with no visual change. Files:
  `src/striatum/web/frontend/src/shared/types.ts`,
  `src/striatum/web/frontend/src/shared/theme.css`,
  `src/striatum/web/frontend/src/islands/workflow-graph-editor/WorkflowGraphEditor.tsx`,
  and new unit suites in
  `src/striatum/web/frontend/src/__tests__/workflow-graph-editor.test.ts`.

### Decided

- D097: Cycle-exhaustion override for the dogfood-043 Python build
  review. 2-of-3 cross-lane reviewers accept (claude
  accept_with_findings low, gemini accept low); codex needs_revision
  (high) overridden because the codex/codex implementer+reviewer
  pairing produced the third instance of the convergent-blind-spot
  anti-pattern (precedents D095 dogfood-042 Track A, D096 dogfood-042
  Track C). Codex findings (cycle phase-jump validator gap, strict
  phase-skip restriction, phase_id strict-on-v1 check, drag-drop
  dropdown bypass, malformed v1.1 tolerance) absorbed into RFC 0045
  V1.5 follow-up (TODO item 27). Anti-pattern now well-characterized
  across three independent runs; full validator refuse-by-default
  remains the deferred half of TODO item 26 (a soft warning landed in
  the dogfood-043 prep commit).

### Notes

- Dogfood-043 ran with two parallel tracks (Track A Python core
  implemented by codex; Track B React Flow editor implemented by
  claude) and 3-way build review postures (codex threat_model,
  claude ergonomics_dx, gemini adversarial). The `consolidate` job
  was not present in the workflow; the operator authored this
  changelog entry, the `docs/rfcs/README.md` status update, the
  `docs/TODO.md` follow-ups, the `docs/dogfood/043/BUILD_HANDOFF.md`
  cross-track handoff, and the `docs/dogfood/043/PHASE_1_OPERATOR_NOTES.md`
  operator narrative in its place (dogfood-042 lesson applied: the
  in-workflow consolidate job was the wrong locus when the operator
  is already the synthesizing surface).

## v1.31.0 — 2026-05-13

### Added

- Track A (dogfood-042): RFC 0039 V1 Steps 1+2 Go daemon core landed
  under `go/`. New `go/cmd/striatumd` entry point, `go/pkg/rpc`
  (envelope-v1 validation/serialization, RFC 0030 method registry,
  capability vocabulary, in-memory capability helpers, handshake,
  `daemon.describe`, duplicate request detection, RPC server framework
  for read-only routes), `go/pkg/db` (daemon Postgres config
  resolution/redaction, dependency-free `psql` runner, migration
  loading/application, embedded SQL migrations, audit hash/recording),
  `go/go.mod` + `go/go.sum` + `go/Makefile`, and root `Makefile`
  `daemon-go-build` / `daemon-go-test` / `daemon-go-lint` targets.
  Python harness gained `daemon_core: Literal["python","go"]` parameter
  on `DaemonProcess` and `MultiRepoHarness` (default `"python"`,
  backward-compatible); Go invocation resolves the binary via
  `STRIATUMD_GO_BIN` or `<repo>/go/bin/striatumd` and runs
  `make -C go build` on demand. Phase 1 partial — Steps 3-6 (CLI
  integration, mutating verbs, supervised processes, distribution)
  deferred to a Phase 2 dogfood per RFC 0039 §9. Documentation
  updated in `docs/HOW_TO_HUMAN.md`, `docs/SPEC.md`, and
  `docs/UBIQUITOUS_LANGUAGE.md`.
- Track B (dogfood-042): RFC 0044 drafted as the Engram Phase 1
  implementation spec — Engram as an optional read-only memory
  augmentation for Striatum operators. Pull-mode ingestion with
  Striatum-owned redacted JSONL export, Engram-owned
  `ingest-striatum`, standalone `engram-mcp-stdio` MCP server, four
  read-only retrieval tools, Engram-local `memory.*` capabilities,
  and a hard augmentation-not-dependency boundary. RFC text only;
  implementation lands via a future dogfood.
- Track C (dogfood-042): repo-local-state-to-Postgres design work
  superseded by main's RFC 0043 (Postgres as Sole Substrate and
  Daemon-Required Runtime, accepted via D094) which landed during this
  dogfood from the parallel session. Track C dogfood artifacts (3
  designs + synthesis + 3 reviews + decision) retained under
  `docs/dogfood/042/track_c/` as historical provenance; the draft
  `docs/rfcs/0042-repo-local-state-to-postgres.md` is NOT shipped
  (collides with main's RFC 0042 number, scope absorbed by RFC 0043).

### Decided

- D095: Cycle-exhaustion override for Track A Go daemon build.
  2-of-3 reviewers accept_with_findings (claude, gemini); codex
  needs_revision overridden because the codex/codex
  implementer+reviewer pairing converged on its own findings. Codex
  findings absorbed into RFC 0039 V1.5 follow-up (TODO item 24).
  Follow-up: forbid codex/codex implementer+reviewer pairs in the
  workflow validator (TODO item 26).
- D096: Cycle-exhaustion override for Track C build review.
  2-of-3 reviewers accept-equivalent (claude accept, gemini
  accept_with_findings); codex needs_revision overridden because the
  same codex/codex anti-pattern recurred. Track C's repo-local-PG
  design intent is absorbed by main's RFC 0043; the draft RFC file is
  not shipped.

### Notes

- The dogfood-042 multi-phase workflow with three parallel tracks
  completed with two cycle-exhaustion overrides (D095 Track A, D096
  Track C). The `consolidate_phase_1` job was cascaded into
  cancellation; the operator wrote this changelog entry, the
  `docs/rfcs/README.md` index updates, the `docs/TODO.md` follow-ups,
  and the `docs/dogfood/042/BUILD_HANDOFF.md` cross-track handoff in
  its place.

## 1.30.0 — 2026-05-13

### Added

- RFC 0038 V1 web UI feature additions and frontend toolchain. Vite +
  React + TypeScript contributor-side toolchain bundled into the wheel
  via `src/striatum/web/static/build/` (operators stay pip-only). New
  `make ui-install` / `make ui-build` / `make ui-dev` / `make ui-test`
  targets. Five user-facing additions: workflow detail's Edit
  affordance promoted from a muted text link to a button next to "Run
  this workflow now"; new `/view/` repo file browser with lazy
  expansion via `GET /v1/repo/tree`; new `/workflows/new` chooser
  wizard over the RFC 0034 V1 generator endpoints with a
  `<dialog>`-driven operator confirmation gate; drag-drop React Flow
  workflow graph editor with structured per-node widgets at
  `/workflows/edit/<path>`; Shiki-based syntax-highlighted code viewer
  for non-Markdown files at `/view/<path>` with line numbers, copy,
  raw link, and wrap toggle. New `docs/FRONTEND_DEVELOPMENT.md`
  contributor guide. Dark-mode parity inherited from `base.css`.
- New shared TypeScript prop contract in
  `src/striatum/web/frontend/src/shared/types.ts` mirroring the
  workflow validator's closed vocabularies.

### Decided

- D092 (re-cited): supersede D073's implicit "no node toolchain" rule
  for the contributor-side build path. Operator install remains pip
  only; bundled JavaScript ships in the wheel under
  `src/striatum/web/static/build/`. Bundle drift is detected by a
  committed `manifest.sha256` in CI.

## 1.29.0 — 2026-05-12

### Added

- RFC 0040 V1 operator-side slice of the MCP-driven dogfood harness:
  twelve dogfood-lifecycle chat-tool entries (`run_prepare`,
  `run_start`, `register_session`, `supervise_start`, `claim_next`,
  `ack`, `publish_artifact`, `verdict`, `complete`, `supervise_stop`,
  `run_summary`, `evidence_export`) over `striatum.api.invoke`. Ten
  state-mutating tools are gated behind `serve --allow-mutations`; two
  read-shaped tools (`run_summary`, `evidence_export`) stay available
  unconditionally.
- Per-model harness-profile fragments baked into the bundled workflow
  template catalog (`claude_code_default`, `codex_default`,
  `gemini_default`, `generic_default`). `workflow generate` enriches
  any user-supplied profile body with the catalog defaults when
  `native_delegation.instruction` is missing; existing instructions
  are preserved verbatim.
- `striatum workflow upgrade <path>` CLI verb that backports the
  catalog fragments into existing workflow.json files. Refuse-on-
  conflict default with `--force` to overwrite; `--dry-run` reports
  the change set without writing; refuses when any non-terminal run
  references the workflow.
- New `docs/HARNESS_FRICTION_PATTERNS.md` documenting the four
  observed friction patterns (036 strategy-then-exit, 037 ask-and-
  exit, 038 lease-expiry-under-active-load, 038/039 front-matter
  completeness) and the V1 fixes.
- `docs/MCP.md` "Dogfood-Lifecycle Tools" section listing each new
  tool, its capability requirement, and an example sequence.
- `docs/HOW_TO_HUMAN.md` walkthrough of driving a dogfood through the
  MCP chat tools instead of bash CLI, plus a `workflow upgrade`
  recipe.
- `docs/HOW_TO_AGENT.md` note for operator-AI sessions to prefer the
  MCP chat tools over shelling out to bash; supervised roles still
  use the work-packet `commands` block verbatim.

### Decided

- D093: Accept RFC 0040 V1 as the operator-side slice. Composite
  tools (`dogfood.publish_on_behalf`, `dogfood.surgical_recovery`)
  and the daemon-side supervised-progress heartbeat land in the
  systems half of the RFC. See `docs/HARNESS_FRICTION_PATTERNS.md`
  for the long-form record.

## 1.28.0 — 2026-05-13

### Added

- RFC 0037 V1 web UI ergonomics: run/workflow filters, run duration
  and workflow last-modified columns, grouped doctor problems with a
  terminal-run filter, UTC/local timestamp toggle, graph tooltips,
  keyboard shortcuts, app-specific dark-mode parity, promoted run next
  actions, and empty states for the main triage pages.

## 1.27.0 — 2026-05-13

### Added

- RFC 0035 V1 test infrastructure: `tests/_harness/MultiRepoHarness`
  for ephemeral Postgres + daemon + multi-repository e2e coverage, five
  cross-repo harness-backed test modules, and `make test-multi-repo`.

## 1.26.0 — 2026-05-13

### Added

- RFC 0036 V1: `striatum-mcp` skill coverage for loose skill installs
  and plugin bundles, plus chat tools `generate_workflow_preview` and
  `generate_workflow_write` over the RFC 0034 workflow generator.
- Chat workflow writes are hidden when `serve` lacks
  `--allow-mutations`; crafted write calls fail with
  `mutations_disabled`.
- Chat workflow writes queue a one-shot operator confirmation in the web
  UI before generated workflow files are written.

## 1.25.0 — 2026-05-12

### Added

- RFC 0034 V1 workflow generator: bundled shape/lane-set catalog,
  `workflow templates list/show`, `workflow generate`, local service
  catalog and generation endpoints, custom-plan compilation, and
  `workflow init --style` compatibility over the generator.

### Decided

- D091: OPERATOR_REPORT.md is written incrementally during a dogfood
  run, not only at the end. Refines D089. The operator appends a dated
  entry per intervention (publish-on-behalf, recovery sweep,
  override-verdict, decision recording) at the moment it occurs;
  end-of-run only writes the wrap-up sections.

## 1.24.0 — 2026-05-12

### Added

- RFC 0032 V2 slice: cross-repo workflow schema validation, repo-local
  `runs.cross_repo_run_id`, daemon DB migration v3 for cross-repo run
  metadata, daemon RPC method scope modes, `recovery` capability, daemon
  MCP `tools/list` filtering and `tools/call` re-authorization/audit
  scaffolding, and mocked cross-repo lifecycle helpers.

### Documentation

- Documented the dogfood-035 deferral: real two-repo daemon end-to-end
  integration tests and live scheduler progression wait for TODO Open
  item 19, the multi-repo test harness RFC.

## 1.23.0 — 2026-05-11

### Added

- RFC 0030 daemon RPC foundation: envelope-v1 codec, newline JSON
  framing helpers, owner-local Unix socket and loopback HTTP guards,
  `daemon.hello` / `daemon.welcome`, `daemon.describe`, a
  capability-bound method registry, and PostgreSQL request/audit helper
  wiring.
- RFC 0031 daemon-owned supervision/apply foundation: daemon DB
  migration v2 for method metadata, daemon supervisor ownership, and
  apply receipts; repo-local migration v13 for supervisor pointers; and
  fail-closed apply-key/refusal helpers.

### Documentation

- RFC 0030 and RFC 0031 are now marked accepted for the V2 foundation.
  Docs distinguish the landed RPC/schema boundary from deferred
  cross-repo workflows, MCP mutation expansion, hosted services,
  Windows daemon support, and any claim of third-party cryptographic
  non-repudiation.

## 1.22.1 — 2026-05-11

### Fixed

- Byline tolerance: a Markdown-decorated byline like
  `**Author:** value`, `# Author: value`, or `_author_: value` is now
  recognised by the publisher and stored in `artifacts.author_line`
  as the canonical lowercase `author: value` form. Models seen in
  dogfood-031 and dogfood-033 produced the bold-decorated form, which
  previously caused the publisher to silently drop the byline (stored
  as NULL); the canonicaliser now normalises decoration before
  matching. Mismatched bylines still refuse with the documented error.

### Added

- `publish-artifact` auto-attaches default front matter for the
  `synthesis` artifact kind when the file omits the `---` block. The
  publisher prepends `schema_version: "striatum.synthesis.v1"` and
  `artifact_kind: "synthesis"` (the only required fields, both
  constants the publisher already knows from `--kind synthesis`),
  rewrites the file on disk so the stored SHA agrees with downstream
  reads, and proceeds with the rest of validation. The agent's body
  is preserved verbatim after the prepended block. Other
  schema-bearing kinds (`finding`, `decision`, `findings_ledger`,
  `support_ledger`, `action_item_ledger`,
  `harness_improvement_proposal`) have semantic required fields the
  publisher cannot invent (`verdict_intent`, `outcome`, etc.) and
  continue to silently accept missing front matter — adding an
  explicit refusal there would be a policy break and should land
  behind a workflow-level opt-in.

- Hard byline + front matter discipline section in every dogfood-033
  design prompt (`design_codex.md`, `design_claude_code.md`,
  `design_gemini.md`, `synthesize_design.md`): forbid Markdown bold
  (`**Author:**`), heading prefix (`# author`), italics (`_author_`),
  and quotes around the value; require lowercase `author:` exactly;
  include the JSON-encoded front matter template for schema-bearing
  kinds.

## 1.22.0 — 2026-05-11

### Added

- RFC 0033 daemon PostgreSQL substrate scaffolding: optional
  `striatum-orchestrator[daemon-pg]` driver dependency, packaged
  forward-only daemon DB baseline migration, `daemon doctor`
  PostgreSQL onboarding checks, `daemon start --postgres-url`, and
  `daemon migrate --from sqlite --to pg` cutover wiring with V1 audit
  hash preservation.

### Documentation

- RFC 0033 is now documented as accepted V2: daemon-owned state moves to
  operator-supplied system PostgreSQL, with forward-only daemon DB
  migrations and `striatum daemon migrate --from sqlite --to pg` for the
  V1 registry cutover. The docs keep repo-local
  `.striatum/retired-local-state` as workflow truth and avoid claiming daemon
  RPC, MCP mutations, daemon-owned supervision, cross-repo mutation, or
  sealed apply before their later RFCs.

## 1.21.1 — 2026-05-11

### Fixed

- Parallel-reviewer cascade-child UNIQUE collision: when two
  reviewer postures fan out from a single cycle target (e.g. three
  parallel design-review postures all routing back to one
  `synthesize_design` via `needs_revision` cycles), the second
  `submit-review` no longer raises
  `UNIQUE constraint failed: jobs.run_id, jobs.idempotency_key`.
  Cycle-target cloning is now idempotent on
  `(run_id, workflow_job_id, attempt)`; parallel reviewers share a
  single revision attempt of the shared cycle target. Surfaced in
  dogfood-031 by `dec_operator_security_cascade_collision_2026_05_11`.

### Added

- `.striatum/bin/codex-supervised-wrapper.sh` mirroring the existing
  claude/gemini supervised wrappers. Codex `exec ... -` hangs on
  empty FIFO stdin in supervised mode in some environments
  (observed during dogfood-031); the wrapper spawns a fresh
  `codex exec` per packet, matching the RFC 0010 V2 one-packet-per-
  invocation model. Updated the bundled `examples/harness-profiles/`
  workflow and `docs/dogfood/031/workflow.json` reference to use
  the wrapper so future runs avoid the FIFO hang.
- `docs/CLI_REFERENCE.md` and `docs/HOW_TO_HUMAN.md` now document
  the RFC 0028 V1 daemon/repo/dashboard verbs (`striatum daemon
  start/status/stop/sweep`, `striatumd` console script, `repo
  add/list/remove`, `--daemon` read routing, `dashboard --all`,
  bootstrap admin token semantics, audit shape, and the V1
  deferrals: no RPC server, no daemon-owned supervision, no
  mutation MCP tools, no Windows daemon support, no
  cross-repository workflows).

## 1.21.0 — 2026-05-11

### Added

- RFC 0026 V1 lane-liveness attestation: work-packet and publish-time
  bylines now downgrade unattested sessions to `author: operator`;
  attached supervised sessions regain lane/model bylines only when the
  pid identity and snapshot command binding match. Added
  `register-session --operator-label`, per-session attestation surfacing,
  and review-job `require_attested_lane: true` gates.
- RFC 0027 provenance-mode guardrails: workflows may declare
  `provenance_mode` (`advisory`, `attested_bylines`, `sealed_patch`).
  `sealed_patch` validates path policy but refuses to start until real
  source containment ships.
- RFC 0028 V1 registry-backed multi-repo acceptance slice: optional
  `striatumd` / `striatum daemon start` foreground sweep process, daemon
  registry, `repo add/list/remove`, explicit `--daemon` read routing,
  `dashboard --all`, resources-only daemon MCP, metadata-only hash-chained
  audit, and recovery sweeping across registered active runs. V1 does not
  ship a daemon RPC server; CLI and MCP clients open the owner-only
  registry SQLite directly under token/capability checks.
- Dogfood-031 revision round 2 hardens the daemon slice: unsupported
  forced-daemon verbs refuse instead of falling back to direct mode,
  `repo add` authorizes before repo-local access and requires `--init`
  for absent state databases, daemon MCP uses explicit tokens with
  repo-scope filtering, audit segment manifests are guarded and checked
  by doctor, and foreground sweeps write repo-local
  `daemon.recovery_sweep` events bylined `striatumd-<instance-id>`.
- Dogfood-031 revision round 3 removes `STRIATUM_DAEMON_TOKEN` plaintext
  env-var support, uses realpath/inode-based repository identity for new
  registrations, admin-gates manual `daemon sweep`, audits denied
  dashboard/MCP aggregate reads with client attribution on allowed reads,
  and documents RPC server, audit retention/rotation, HTTP transport, and
  full underlying recovery-byline propagation as follow-up RFC scope.

## 1.20.1 — 2026-05-10

### Added

- "Verdicts by posture" chips on `/run/<id>` are now clickable.
  New route `GET /run/<id>/posture/<posture>` lists every verdict
  recorded with that posture for the run: verdict value, review
  job, role/lane, session slug, finding artifact link, and
  rationale. Page also shows a one-paragraph "what does this
  posture mean?" explanation per RFC 0018's posture vocabulary
  (`devils_advocate`, `security`, `threat_model`, etc.).

## 1.20.0 — 2026-05-09

### Added

- RFC 0025 V1 Steps 2+3 (dogfood-029): `codex` and `gemini`
  plugin profiles, completing the V1 plugin scope.
  - **`codex` profile**: 14-file Codex plugin bundle under
    `.striatum/plugins/codex/` with `.codex-plugin/plugin.json`, 5
    skills (byte-shared with claude_code), 5 Markdown commands,
    `hooks/hooks.json`, `.mcp.json`, `README.md`, `.manifest.json`.
    User scope: `~/.codex/plugins/<namespace>/`.
  - **`gemini` profile** (promotes from RFC 0015 generic
    fallback): 14-file Gemini extension under
    `.striatum/plugins/gemini/` with `gemini-extension.json`,
    `GEMINI.md` context file, 5 skills (byte-shared), 5 TOML
    commands (bare top-level form per Gemini extension spec),
    `agents/striatum-recover.md` sub-agent definition,
    `README.md`, `.manifest.json`. User scope:
    `~/.gemini/extensions/<namespace>/`.
  - **`--profile all`** aggregates all three profiles into one
    install invocation. Result shape: `{"profile": "all",
    "results": [...]}`.
  - Marketplace fixture continues to be reentrant; gemini
    short-circuits with `{"skipped": True, "reason": "gemini has
    no marketplace concept"}` so JSON callers can detect the skip.
  - F1 byte-match test extended: skill template trees under all
    three profiles must match `skills/templates/claude_code/`
    byte-for-byte.

RFC 0025 status: **accepted (V1)** — three first-class profiles
shipped.

### Deferred to V2

- Cross-target install (one bundle into many target repos).
- Hosted marketplace.
- Codex `apps/` and `assets/`.
- Per-target git-repo extension format for gemini.

## 1.19.0 — 2026-05-09

### Added

- RFC 0025 V1 Step 1 (dogfood-028): `claude_code` plugin profile.
  - `striatum plugin install --profile claude_code` emits a
    14-file Claude Code plugin bundle under
    `.striatum/plugins/claude_code/`. Layout: `.claude-plugin/plugin.json`,
    `skills/striatum-{workflow,scaffold,claim-loop,supervise,recover}/SKILL.md`,
    `commands/{claim-next,status,why,dashboard,doctor}.md`,
    `hooks/hooks.json`, `.mcp.json`, `README.md`, `.manifest.json`.
  - `striatum plugin uninstall --profile claude_code` reads the
    bundle's manifest and deletes only manifest-tracked files;
    refuses to delete operator-edited files without `--force`.
  - `striatum init --with-plugins [profile]` mirrors
    `--with-skills`. Default profile is `claude_code`.
  - `--with-marketplace` (default on) writes
    `.striatum/plugins/marketplace.json` with a `local-striatum`
    fixture entry; reentrant — re-installs update in place.
  - Doctor checks `plugin_missing` and `plugin_outdated` walk every
    installed bundle's `.manifest.json` and surface the exact
    `striatum plugin install --profile <id>` invocation that fixes
    the drift.
  - Skill bodies are byte-shared with `skills/templates/claude_code/`
    via a CI test (`test_skill_templates_match_skills_module`)
    so future skill edits propagate to both surfaces.
  - URL-leak invariant: `test_claude_code_no_external_urls`
    forbids `https?://`, `git://`, `file://`, `ssh://`, `ftp://`
    in any rendered file.

### Deferred to V1 Step 2 / Step 3

- `codex` plugin profile (`.codex-plugin/plugin.json` + Codex
  commands).
- `gemini` profile promotion (split the current single-guide shape
  into the same five-skill structure used by claude_code).
- `--profile all` aggregation.
- Cross-target install (one bundle, many target repos).

## 1.18.0 — 2026-05-09

### Added

- RFC 0024 V4 (dogfood-027): pause/resume + per-job mutations.
  - **Migration v11** adds `runs.paused_at` and `runs.paused_reason`
    columns. Forward-only; idempotent against fresh DB whose
    schema baseline already includes them.
  - **`pause_run(conn, *, run_id, reason)`** sets the columns;
    idempotent on already-paused; refuses terminal states.
  - **`resume_run(conn, *, run_id)`** clears the columns;
    idempotent on not-paused; refuses terminal states (use
    `retry_job` to revive a canceled run).
  - **`claim_next` gate**: runs with `paused_at IS NOT NULL`
    return `{"status": "no_work", "paused": True}`. Active leases
    keep ticking; expire-leases at the top of `claim_next`
    handles paused-with-stale-leases.
  - **`retry_job(conn, *, run_id, job_id)`** resets a
    failed/canceled/blocked job to `queued`, increments
    `attempt`, marks prior `queue_messages` rows as `canceled`
    (preserving the partial unique index), re-enqueues, and
    revives canceled/failed runs to `running` with a loud
    `run.revived` event.
  - CLI: `striatum run pause/resume/retry-job`.
  - HTTP: `POST /run/<id>/pause`, `/run/<id>/resume`,
    `/run/<id>/job/<jid>/cancel`, `/run/<id>/job/<jid>/retry`. All
    mutation-gated.
  - UI: Pause/Resume buttons + paused status pill on the run
    detail page; Cancel/Retry buttons on the job detail page.
    Cancel confirm reads "Cancel this job AND its dependents…".
    All islands CSP-safe.

### Run-revival semantics (D078 follow-up)

Per RFC 0024 V4 design-review F1 (option C): when an operator
retries a job whose run is `canceled` or `failed`, the run
transitions back to `running` and a `run.revived` event is emitted
with `previous_run_state` payload. The terminal-state guarantee
softens for operator-triggered revival but stays loud (event +
documented). `retry_job` refuses to revive a `completed` run.

### Deferred to V5 if needed

- Pause-with-deadline (auto-resume at timestamp).
- Per-lane pause.
- Recovery integration of pause as escalation hook target.
- Consolidate `_read_json_body` / `_read_json_body_strict` helpers.

## 1.17.0 — 2026-05-09

### Added

- RFC 0024 V3 (dogfood-026): cancel-run mutation surface plus the
  dirty-tree visibility V2 deferred.
  - `cancel_run(conn, *, run_id, reason)` in `db.py` — top-down
    cancel that voids active leases, marks in-flight jobs (queued,
    running, blocked, ready, claimed) as canceled, transitions the
    run to `canceled`, emits `run.canceled`, and closes remaining
    sessions. Idempotent on already-canceled; refuses completed /
    failed via `InvalidTransitionError`.
  - `striatum run cancel --run-id <id> [--reason <text>]` — CLI.
  - `POST /run/<id>/cancel` — mutation-gated HTTP endpoint.
    Returns 200 on success (and on idempotent re-cancel); 405 / 404 /
    409 / 415 for the other paths.
  - Cancel button on the run-detail page when state is non-terminal
    (prepared / needs_branch_confirmation / ready / running). CSP-safe
    JS island in `/static/run_cancel.js`.
- Run-now dirty-tree visibility (closes V2 design-review F3):
  `POST /workflows/run/<path>` now returns 409 with
  `error.kind: "dirty_tree"` and `error.git_status` (first ~80
  lines of `git status --short`) when `git_create_or_checkout_branch`
  fails. Operators see the blocker without context-switching to a
  terminal.

### Deferred to V4

- Pause / resume runs.
- Auto-branch suffix (research showed multi-run-per-branch is
  by-design — the friction operators feel is dirty-tree, which V3
  fixes directly).
- Per-job mutation buttons (kill running job, retry).
- Programmatic re-run with parameter overrides.
- Recovery integration: cancel-run as an escalation hook target.

## 1.16.1 — 2026-05-09

### Added

- RFC 0024 V2.1: branch-confirm button on `/run/<id>` for runs in
  `needs_branch_confirmation` state. Operator no longer has to drop
  to the CLI when a `confirm`-mode workflow is started via the
  Run-now button. POST `/run/<id>/branch-confirm` with
  `{branch, create, use_current}` calls `branch_confirm` and
  `run_start` in one transaction; reload reveals the now-running
  run.

### Fixed

- SVG dependency graph rendered with explicit `width`/`height`
  attributes instead of relying solely on `viewBox` + CSS
  `max-width: 100%`. Small graphs no longer scale up to fill the
  full container width — boxes render at their natural pixel size
  and the SVG only shrinks for narrow viewports.

## 1.16.0 — 2026-05-09

### Added

- RFC 0024 V2 (dogfood-025): three editor additions on
  `/workflows/*` — run-now lifecycle, `If-Match` concurrency
  guard, and field-level validation errors.
  - `POST /workflows/run/<path>` — mutation-gated; calls
    `create_run + branch_confirm(create=True) + run_start`;
    returns `{run_id}` on 200; 409 on dirty-tree branch refusal;
    422 on validation failure with structured `errors[]`. When
    `branch.mode == "confirm"`, returns 200 with status
    `needs_branch_confirmation` so the operator can finish out of
    band.
  - `If-Match: <sha256>` precondition on `POST
    /workflows/edit/<path>`. GET stamps the disk sha into a hidden
    `<script id="workflow-sha256">` tag; editor JS echoes it on
    POST; on stale sha the server returns 412 with
    `current_sha256` so the editor can prompt for reload. Missing
    header → V1.5 opt-out (backward compatible).
  - `WorkflowError` extended with optional `field_path`. 8
    high-traffic raise sites tagged: `schema_version`, duplicate
    job id, unknown role, unknown lane, invalid artifact path,
    cycle references unknown job, cycle `max_iterations < 1`. The
    422 body now includes `error.errors: [{field_path, message}]`;
    editor highlights the offending form field via a
    `data-field-path` attribute. Untagged raise sites keep `None`
    and the editor falls back to the V1.5 top-of-form banner.
  - "Run this workflow now" button on `/workflows/<path>` (only
    rendered when the workflow is `valid`). On 200 navigates to
    `/run/<run_id>`. CSP-safe: behavior lives in
    `/static/workflow_run.js`.

### Workflow-trust model

V2 lets any operator with `--allow-mutations` launch a run from any
committed `workflow.json`. This matches the CLI surface (`striatum
run prepare --workflow <path>` from a shell). No new attack surface.

### Deferred to V3

- Drag-and-drop graph editor.
- Workflow templates / marketplace.
- "Diff against another workflow" view.
- AI-assisted scaffolding via chat tool that *writes*
  workflow.json (would require per-tool gating).
- Multi-error reporting (collect all errors, not just first).
- Field-path coverage for the remaining ~22 raise sites.
- `flock()` for hard concurrency guarantees.
- 409 body carrying `git status --short` output.

## 1.15.0 — 2026-05-09

### Added

- RFC 0024 V1.5 (dogfood-024): workflow visual builder.
  - `GET /workflows/edit/<path>` renders a form-driven editor
    for any repo-relative `workflow.json`. Existing files load
    their parsed JSON (even invalid — the editor opens so the
    operator can fix); non-existent paths render an empty
    scaffold with the workflow_id derived from the path stem.
  - `POST /workflows/edit/<path>` saves: validates the body
    via `validate_workflow`; on success atomically writes via
    `<path>.tmp` + rename; on validation failure returns 422
    with the WorkflowError message (file unchanged).
  - Mutation-gated (`--allow-mutations` required for POST).
  - Body capped at 1 MB; non-`application/json` content-types
    rejected with 415 (per design-review F1).
  - Path safety mirrors `/view/<path>`: `..`, leading `/`, null
    bytes, hidden dirs (`.git`, `.striatum`) refused.
  - JS island (`workflow_edit.js`) renders form sections from
    in-memory state: header, roles, lanes, jobs, edges, cycles.
    Add/remove buttons mutate state; save POSTs the full state
    as JSON; on success redirects to the detail page.
  - localStorage backup persists the in-progress draft so a
    browser-crash doesn't lose work; recovered with operator
    confirmation on reload.
  - "Edit" link added to the workflow detail page.

### Deferred to V2

- Drag-and-drop graph editor.
- Workflow templates / marketplace.
- "Diff against another workflow" view.
- "Run this workflow now" full lifecycle button.
- Field-level error highlighting (requires `validate_workflow`
  API change).
- `If-Match: <sha256>` precondition for safe concurrent edits.
- AI-assisted scaffolding via chat tool that *writes*
  workflow.json (would require per-tool gating).

## 1.14.0 — 2026-05-09

### Added

- RFC 0024 V1 (dogfood-023): workflow browser (read-only).
  - `GET /workflows` lists every `**/workflow.json` in the
    target repo with validation status, workflow_id,
    job/lane/role counts. Hidden dirs (`.git`, `.striatum`,
    `.venv`, `node_modules`, etc.) excluded from discovery.
  - `GET /workflows/<repo-path>` renders a detail page with
    the SVG dependency graph (reusing RFC 0022 V1's renderer)
    plus tables for jobs, lanes, roles, edges, and cycles.
    Invalid workflows render their `WorkflowError` message
    inline; the page never 500s.
  - Path safety mirrors `/view/<path>`.
  - New chat tool `list_workflows` extends RFC 0023 V1.5's
    closed read-only tool set; the model can answer "which
    workflow produced run X?". Capped at 100 entries.
- `Workflows` link in the top nav (between Runs and Chat).

### Deferred to V1.5

V1.5 (separate dogfood) ships the *visual builder*: form-driven
editor at `/workflows/edit/<path>`, save action with server-side
validation, per-job posture + required_review_postures widgets,
flash banner + redirect-after-save.

## 1.13.0 — 2026-05-09

### Added

- RFC 0023 V1.5 (dogfood-022): chat tool use + system-prompt briefing.
  Closes the V1.5 deferral from RFC 0023 V1.
  - **Six closed-set read-only tools** wired into the chat backend:
    `read_file(path)`, `list_dir(path)`, `striatum_status(run_id?)`,
    `striatum_why(target_id)`, `git_log(limit?)`,
    `git_diff(path?)`. The model decides when to call them; the
    backend executes server-side and feeds results back. Closed-
    set membership enforced in `execute_tool`; unknown tool names
    return error strings rather than executing. No tool that
    mutates state.
  - **Tool-call loop** in `_handle_chat_send`: up to 10 iterations
    of (request → assistant text + tool calls → execute → re-request
    with results). Loop terminates on a no-tool-calls response.
  - **System-prompt briefing** at chat-session creation: repo path,
    current branch, last 10 commits, top-level entries, AGENTS.md
    content (capped at 8 KB), active-run summary, tool-use
    guidance. The chat now has bearings on its first turn.
  - **Per-flavor tool wiring**: Anthropic Messages tool-use shape
    (content blocks with `type: "tool_use"` + `tool_result`) and
    OpenAI Chat tool-use shape (`tool_calls` + `role: "tool"`)
    both supported. Streaming tool calls are accumulated server-
    side and emitted as discrete events.
  - **JSONL transcript extensions**: new role values `tool_use`
    and `tool_result` persist tool calls + their wrapped results.
    Existing user/assistant/system roles unchanged.
  - **Prompt-injection defense**: tool results are wrapped in
    `<tool_result_begin name="..." args="..."> ... <tool_result_end>`
    delimiters. The system briefing instructs the model to treat
    content between the delimiters as data, not instructions
    (defense in depth; closes design-review F1).
- Chat history page now renders `tool_use` and `tool_result`
  entries as collapsed-by-default `<details>` blocks alongside
  user/assistant turns.

### Fixed

- **Graph-node click 404** (RFC 0022 V1 regression): SVG graph
  nodes link by *workflow* job id (e.g., `research_chat`) but
  the `/run/<id>/job/<id>` route handler queried by the *full*
  job id only. The handler now accepts either form.
- **Doctor page rendered no list**: the template referenced
  `doctor.checks` but the `doctor()` function returns
  `doctor.problems` (list[str]) and `doctor.problem_records`
  (list[dict]). Template rewritten to render the actual shape;
  CSS for the problem list added.
- **Chat double-render of user messages**: the JS island
  optimistically appended the user's message on form submit, then
  the SSE round-trip rendered the same message a second time
  (with timestamp). Optimistic append removed; the SSE stream is
  now the single source of truth for message rendering. ~250ms
  perceived latency before the user's own message appears, no
  duplication.

## 1.12.0 — 2026-05-09

### Added

- RFC 0023 V1 (dogfood-021): web chat surface +
  `/view/<path>` endpoint + inline Markdown rendering on
  artifact pages. Provider-neutral chat client streams HTTP
  to an operator-configured endpoint via four env vars
  (`STRIATUM_CHAT_API_BASE_URL`, `STRIATUM_CHAT_API_KEY`,
  `STRIATUM_CHAT_MODEL`, `STRIATUM_CHAT_API_FLAVOR`). Two
  flavors: `anthropic_messages` and `openai_chat` (covers
  OpenAI, OpenRouter, Ollama, vLLM, LiteLLM proxy, etc.).
  No default provider; operators opt in explicitly. URL
  scheme validation refuses non-loopback `http://`. Chat
  startup is `--allow-mutations`-gated.
- `/view/<path>` read-only file viewer: `.md` renders as
  HTML, text as `<pre>`, binaries as a metadata panel.
  Path traversal refused; `.git/` and `.striatum/` hidden
  by default. Directory listings deferred to V1.5.
- `/run/<id>/artifact/<id>` now renders `.md` artifact
  bodies inline (closes RFC 0022 V1.5 deferred).
- Chat transcripts in `.striatum/scratch/chat-<id>/transcript.jsonl`
  (gitignored). SQLite unchanged. No artifacts published.

### Dependency

- **`markdown-it-py` ≥ 4.0** is now a runtime dependency
  (the project's second after Jinja2). `html: False` at
  parse time; no separate sanitizer needed for V1.

### Boundary clarification (D074)

- AGENTS.md "no cloud APIs without explicit product
  decision" gets its first carve-out: outbound HTTP from
  striatum to an operator-configured endpoint is permitted
  for chat (and only chat). No hosted striatum service; no
  default endpoint; no telemetry. D028 (transcripts off)
  gets a parallel narrow carve-out: chat transcripts live
  in scratch JSONL only, never SQLite, never artifacts.

### Dogfood pattern (first 3-lane review)

- dogfood-021 declares three parallel design-review jobs
  (security, devils_advocate, threat_model) and three
  parallel build-review jobs (security, devils_advocate,
  ergonomics_dx) — first run to use RFC 0018 V1's
  `required_review_postures` reachability gate at full
  3-posture coverage.

## 1.11.1 — 2026-05-09

### Changed (docs only)

- Refresh the documentation set against the current state
  (RFCs 0001–0022, v1.11.0 features). Mention
  `--with-ddd-layout` (RFC 0021) + `--ddd-layout-force` /
  `--ddd-layout-dry-run` (V1.5) in `README.md`,
  `docs/GETTING_STARTED.md`, `docs/HOW_TO_HUMAN.md`, and
  `docs/CLI_REFERENCE.md`. Update `README.md` "Status" section
  from `v1.1.0` to `v1.11.0` + add the PyPI `pip install
  striatum-orchestrator` instructions. Rewrite `docs/SPEC.md` §
  "Local Web UI" against the RFC 0022 V1 server-rendered shape
  (Jinja2 multi-page, SVG dependency graph, dark-mode CSS
  custom properties).
- Apply explicit "historical" banners to incubation-era
  documents per `docs/CONTEXT_HYGIENE.md` § "Failure modes" #1
  (mixed live/historical material with no label):
  `docs/INTERVIEW_LOG.md`, `docs/PRIOR_ART.md`,
  `docs/RFC_0014_DOGFOOD_FIX_SPEC.md`, and
  `docs/dogfood/HISTORICAL.md`. The `docs/INDEX.md` table-of-
  contents now lists these in a dedicated "Historical" section
  with a header-level callout, separating them from active
  reference material.
- `docs/dogfood/HISTORICAL.md` extended with a "current
  cadence" subsection listing recent runs (014–020) and what
  each shipped (RFC + tag + highlights), so a reader can find
  a recent canonical run instead of copying patterns from the
  incubation-era 001–013 directories.

No behavior change; no schema change; no new tests.

## 1.11.0 — 2026-05-09

### Added

- RFC 0022 V1 (dogfood-020): web UI redesign. Server-rendered
  Jinja2 multi-page UI replaces the hash-routed SPA. Five pages:
  `/`, `/run/<id>`, `/run/<id>/job/<id>`,
  `/run/<id>/artifact/<id>`, `/doctor`. Each page is real HTML
  that copy/pastes cleanly and works without JS. The JSON API
  (`/v1/*`) and SSE feed (`/events`) are unchanged.
- Refreshed visual palette: CSS custom properties for theme +
  status colors, `prefers-color-scheme: dark` media query for
  dark mode (no toggle button — OS preference wins), system
  font stack, 4px-grid spacing scale. New `base.css` replaces
  `app.css`.
- SVG dependency graph on `run_detail.html`: layered top-down
  layout (longest-path topological depth), state-colored nodes
  via custom-property `fill`, click-to-navigate to job detail,
  SVG `<title>` tooltip on hover for accessibility. Cycles
  (revision loops) are not rendered as edges — only the forward
  DAG from `workflow_graph_data().graph.edges`.
- Legacy hash-route redirect: a small JS island in `base.html`
  reads `window.location.hash` on load and rewrites
  `#/run/<id>` to `/run/<id>` so bookmarked SPA URLs still
  work.

### Dependency

- **Jinja2 ≥ 3.1** is now a runtime dependency (the project's
  first; previously zero-runtime-dep). Adds ~250 KB to the
  install size, pulls in `markupsafe` (~30 KB transitively).
  Trade-off taken for HTML correctness over hand-written
  string-format escaping.

### Removed

- `src/striatum/web/static/app.js`'s hash-router and the
  associated SPA mount. The mutation-button JS is preserved as
  a per-page island. The CSP header is byte-identical
  (`default-src 'self'; …` with no `unsafe-inline` / `unsafe-eval`).

### Deferred to V1.5

- Inline dogfood Markdown rendering on `/run/<id>/artifact/<id>`.
- SVG graph zoom / pan interactivity.

## 1.10.0 — 2026-05-09

### Added

- RFC 0021 V1.5 (dogfood-019): `--ddd-layout-force` and
  `--ddd-layout-dry-run` flags on `striatum init
  --with-ddd-layout`.
  - `--ddd-layout-force` overwrites existing regular-file
    targets with the template body. The envelope reports
    `status: "overwritten"` plus a `prior_sha256` field for
    audit. Non-regular-file targets (directories, broken
    symlinks) still surface as `status: "error"` regardless
    of force — the operator must resolve those manually.
  - `--ddd-layout-dry-run` reports what *would* happen without
    writing any files. The envelope's top-level `dry_run` flag
    is True; per-file statuses use a `would_*` vocabulary
    (`would_create`, `would_skip`, `would_overwrite`,
    `would_error`). Combine with `--ddd-layout-force` to
    preview a destructive overwrite.
  - Both flags without `--with-ddd-layout` are silent no-ops.
- `scaffold_ddd_layout(repo, *, force, dry_run)` public API
  signature is unchanged from V1; V1's `force=False,
  dry_run=False` defaults map to V1's behavior. Callers that
  pass either flag get the new V1.5 branches without
  deprecation work.

RFC 0021 status moves from `accepted (V1)` to
`accepted (V1+V1.5)`. V1.6 candidates (template parameter
substitution, multi-layout, `striatum scaffold sync`, doctor
check) remain deferred until operator evidence shows they're
wanted.

## 1.9.0 — 2026-05-09

### Added

- RFC 0018 step 3 V1.5 (dogfood-018): `verdicts.posture` column
  + introspection surfacing across six paths.
  - Migration v10 ALTERs `verdicts` to add a `posture TEXT NOT
    NULL DEFAULT 'neutral'` column and a covering index
    `idx_verdicts_posture`. Existing rows backfill to
    `'neutral'`. Forward-only; idempotent.
  - `record_review_verdict` reads the review job's posture from
    the workflow snapshot (defaulting to `'neutral'` when
    omitted) and writes it on INSERT. The `verdict.recorded`
    event payload now carries `posture` alongside `verdict`.
  - `status --json` adds a `verdicts_by_posture` dict alongside
    the existing verdict counts. Always emitted (empty dict
    when no verdicts) for stable shape.
  - `run summary` Markdown adds a `[posture: \`<name>\`]` suffix
    on each per-build verdict line *only* when at least one
    non-neutral posture exists in the run. Posture-omitting
    runs render byte-identically to v1.8.1.
  - `evidence export` JSON snapshot includes `posture` on every
    verdict row.
  - `run graph --format json` adds `posture` to each review
    node's `latest_verdict` block (when a verdict exists).
  - Dashboard verdicts panel renders a `Postures: <p1>=<n1>,
    <p2>=<n2>` summary line when at least one non-neutral
    posture exists. Sorted by count descending, then posture
    name ascending for deterministic ties; truncates to the
    top-3 with `+N more` overflow.
  - Web UI verdict list renders a posture chip alongside each
    verdict badge for non-neutral postures. New
    `.posture-chip` CSS class with `max-width: 12em` +
    `text-overflow: ellipsis` for long `custom:<name>` strings;
    full posture name shows on hover via `title` attribute.

### Changed (intentional)

- `evidence export` JSON snapshot's per-verdict block now
  includes a `posture` field. Downstream consumers parsing the
  redacted snapshot by key name (e.g. `verdict`,
  `findings_artifact_id`) tolerate the additive field; consumers
  that rely on a fixed shape may need an update.

### Tests

- `tests/test_review_postures_introspection.py` (15 cases)
  covering migration idempotency, submit-review backfill across
  declared/undeclared/custom postures, and each of the six
  introspection surfaces (including byte-identical zero-
  regression assertions for posture-omitting runs).

## 1.8.1 — 2026-05-09

### Changed

- PyPI distribution renamed from `striatum` (taken on PyPI by an
  unrelated project) to `striatum-orchestrator`. Module imports
  (`import striatum`) and the `striatum` console script are
  unchanged. Operators upgrading from a hypothetical earlier
  install would `pip uninstall striatum && pip install
  striatum-orchestrator`; in practice no one was on PyPI before
  this release.

## 1.8.0 — 2026-05-09

### Added

- RFC 0021 V1 (dogfood-017): `striatum init --with-ddd-layout`
  scaffolds the seven canonical human-facing DDD documents
  (`docs/SPEC.md`, `docs/PRD.md`, `docs/DECISION_LOG.md`,
  `docs/UBIQUITOUS_LANGUAGE.md`, `docs/DDD.md`,
  `docs/rfcs/README.md`, `docs/rfcs/0001-template.md`) into the
  target repo. Mirrors RFC 0015's `--with-skills` for agent-
  facing files: opt-in (default off, plain `striatum init`
  unchanged), idempotent (existing files reported as `skipped`
  with `reason: "exists"`), composable (both flags can be
  combined; scaffold runs after skills install). New
  `src/striatum/scaffold/` package with seven `.md.tmpl`
  templates shipped via setuptools package-data; `scaffold_ddd_
  layout(repo, *, force, dry_run) -> dict` envelope shape:
  `{"layout": "ddd", "files": [...], "dry_run": bool}`.
- Per-file safety: a target that exists but is *not* a regular
  file (directory, broken symlink, etc.) returns
  `{"status": "error", "reason": "target exists but is not a
  regular file"}` rather than silently `skipped`. OSError during
  write surfaces per-file as `status: "error"` without aborting
  the rest of the scaffold.

### Dogfooded

The dogfood-017 workflow itself uses RFC 0018 V1 fields for the
first time end-to-end: both review jobs declare
`review_posture: "devils_advocate"`, the build job declares
`required_review_postures: ["devils_advocate"]`, and the
workflow validator's reachability gate accepts the run.

## 1.7.0 — 2026-05-09

### Added

- RFC 0018 V1 (dogfood-016): focused adversarial review postures.
  Workflow review jobs accept a new `review_posture` field
  (closed set of nine first-class values:
  `neutral | devils_advocate | security | threat_model |
  latency_performance | ergonomics_dx | accessibility |
  compliance_license | supply_chain`, plus a `custom:<name>`
  grammar for off-list flavors). Build jobs accept a new
  `required_review_postures: [...]` list declaring which postures
  must cover the build. The work-packet `review_policy` block
  exposes `posture` when declared and appends a deterministic
  posture-specific instruction sentence to `instruction` for
  first-class postures. The workflow validator walks the directed
  edge graph in both directions from each build with
  `required_review_postures` and refuses (exit code 8) when any
  required posture is not the `review_posture` of a reachable
  review job.

### Design note

The runtime build-completion gate as written in the original RFC
text deadlocks against striatum's lifecycle (a build's `complete`
mutation precedes its downstream review's verdict by
construction); D069 / V1_ACCEPTANCE record the re-cast to a
workflow-validation gate. Today's edge-verdict mechanism plus
existing run-completion semantics preserve runtime enforcement.
RFC 0018's text is patched to match.

### Deferred

RFC 0018 step 3 (`verdicts.posture` column + introspection
surfacing in `status`, `run summary`, `evidence export`,
`run graph --format json`, dashboard, web UI) remains deferred
to V1.5 per the RFC's own implementation path.

## 1.6.0 — 2026-05-09

### Added

- RFC 0020 step 3 (dogfood-015): `striatum recovery watch
  --run-id <id>` long-lived sweeper daemon. Wraps the existing
  `recovery auto` orchestrator in a sleep loop with single-
  instance pidfile (`.striatum/scratch/recovery-watch-<run_id>.pid`),
  `SIGTERM`/`SIGINT` signal-driven shutdown, JSONL emission per
  sweep + a final `watch_exit` envelope, exit-on-terminal default,
  `--max-sweeps` cap, and the same CLI overrides as `recovery
  auto`. Stale pidfiles (dead PIDs) are overwritten cleanly;
  active-PID collisions exit 4 with a clear message. New
  `src/striatum/recovery/watch.py`. Tests at
  `tests/test_recovery_watch.py` (8 cases, including a SIGTERM
  shutdown test that interrupts a long sleep). RFC 0020
  transitions to `accepted (V1)` — the "step 3 deferred"
  qualifier drops.

## 1.5.0 — 2026-05-09

### Added

- RFC 0019 (D067): `docs/DDD.md` documents striatum's domain-
  driven framing — bounded context, ubiquitous language,
  aggregate roots, value objects, domain events, the
  original CLI-only write-boundary invariant, and an "Adding to the
  model" section that gives future RFCs a citation pattern.
  README `## What It Is For` cites it; `docs/INDEX.md` lists
  it; the RFC template gets an optional `## Domain Modeling`
  section. Documentation only.

- RFC 0020 V1 (dogfood-014): autonomous stalled-run recovery
  step 1+2. New `striatum recovery auto --run-id <id>` one-shot
  sweeper composable with cron / systemd timer; runs lazy lease
  expiry, optional process reconciliation, autonomous review-
  only requeue (D036-safe), human_checkpoint timeout escalation,
  and eligible-blocker doctor flagging. Returns a structured
  envelope `{run_id, swept_at, policy_source, dry_run, actions,
  escalations, still_stuck}`. New optional top-level
  `recovery_policy` workflow block with workflow-declared
  thresholds and an `escalation_hook` (kinds: `marker_file`,
  `webhook`, `shell`); validator rejects `.striatum/` marker
  paths, non-http(s) webhook URLs, and negative thresholds.
  Defaults preserve today's flow byte-for-byte
  (`autonomous_*` defaults are `false`; CLI flags
  `--autonomous-review-requeue` and
  `--autonomous-process-reconcile` opt in per sweep).
  Hook runners (`marker_file`, `webhook`, `shell`) emit a status
  dict that folds into the envelope's `escalations[]`; webhook
  failures continue the sweep without raising. New
  `src/striatum/recovery/` package (`auto.py`, `hooks.py`,
  `policy.py`). Tests at `tests/test_recovery_auto.py` (21
  cases). Step 3 (`recovery watch` daemon) deferred per RFC
  0020 § 4.

## 1.4.1 — 2026-05-09

### Added

- Web UI run-level artifact rollup. The run-detail view now
  shows every published artifact for the run as a table (kind,
  logical name, path, source job, byline, timestamp, sha256
  prefix). Clicking the logical name routes to the existing
  artifact viewer; clicking the source job routes to the
  job-detail view. New endpoint `GET /v1/runs/<id>/artifacts`
  wraps the existing read-only `striatum list artifacts
  --run-id <id>` verb. The change is purely additive — it
  closes the discoverability gap from RFC 0013 V1+step 7 where
  per-run Markdown (BUILD_HANDOFF, DESIGN_SYNTHESIS, RUN_SUMMARY,
  decisions, findings) was reachable only by drilling into the
  job that produced it. 3 new tests at `tests/test_web_ui.py`
  (16 total).

## 1.4.0 — 2026-05-08

### Added

- RFC 0013 step 7 (dogfood-013): web UI mutation buttons.
  `POST /v1/invoke` was already gated by `--allow-mutations`
  (RFC 0012); step 7 adds five click-driven buttons to the SPA
  that POST the same argv shapes:
  - **Continue blocker** / **Cancel blocker** on the job-detail
    view (when an open blocker is present); maps to
    `striatum checkpoint resolve --blocker-id <id> --action {continue, cancel}`.
  - **Record verdict** on review-job detail (when state =
    running); collects verdict + rationale + session/lease ids
    and maps to `striatum verdict ...`.
  - **Record decision** on the run-detail view (always
    available; no lease required); maps to
    `striatum decision record ...`.
  - **Requeue stale review** on stale-lease review-only jobs;
    maps to `striatum recovery requeue-stale ...`.
  Each button opens a confirmation modal showing the literal
  argv before firing; destructive actions (cancel job, reject
  verdict) get a red confirm button. `/v1/health` gains an
  `allow_mutations: bool` field the SPA caches once per page
  load to hide buttons when the gate is off; the runner-side
  gate stays authoritative as defence-in-depth. CSP unchanged
  (no external deps, no `eval`, no inline handlers).
  Tests at `tests/test_web_ui.py` (5 new cases, 13 total)
  cover health-flag both states, mutation refusal without the
  flag (HTTP 405 envelope), SPA wiring grep, and the
  no-external-URL invariant.

## 1.3.0 — 2026-05-08

### Added

- RFC 0016 step 3 (dogfood-012): Unicode `fancy` graph style +
  `--graph-orient {tb, lr}`. The dashboard graph panel and
  `striatum run graph --format ascii` now support box-drawn
  rendering with portable BMP characters (`┌`, `┐`, `└`, `┘`, `─`,
  `│`, `╌╌▶` for cycle back-edges) and a left-to-right layout
  that arranges layers as columns instead of rows. Both upgrades
  fall back deterministically: `fancy → layered` when per-slot
  width drops below 14, `lr → tb` when per-column width drops
  below 14. Color path unchanged; `_format_fancy_box` wraps the
  inner content (not the box frame) so the frame stays uniform
  across states. New flags on both `dashboard` and `run graph`:
  `--graph-orient {tb, lr}` (default `tb`) and the existing
  `--graph-style` choices now include `fancy` as a real renderer.
  8 new tests in `tests/test_dashboard.py` (23 total).

## 1.2.0 — 2026-05-08

### Added

- RFC 0015 step 3 (dogfood-011): codex + gemini skill profiles
  + `--profile all`. `striatum skills install --profile codex`
  writes five Markdown files at `.codex/agents/striatum-*.md`
  reusing the Claude Code skill bodies verbatim.
  `--profile gemini` writes a single
  `striatum-STRIATUM_GEMINI_GUIDE.md` (single-guide fallback per
  RFC 0015 § "Profile coverage" until Gemini CLI's skill
  convention stabilizes; the dedicated filename keeps
  `--profile all` collision-free with `generic`).
  `--profile all` fans out across the four first-class profiles
  (`claude_code, codex, gemini, generic`) in deterministic
  order, returning a `{"profile": "all", "results": [...]}`
  envelope. `striatum init --with-skills all` works the same
  way. Doctor's `skills_missing` / `skills_outdated` checks now
  cover every profile. Tests at `tests/test_skills_install.py`
  (10 new cases, 25 total) cover idempotent regeneration,
  manifest shape, edit detection, fan-out, and template-SHA
  parity for the new profiles.

## 1.1.0 — 2026-05-08

### Changed

- RFC 0017 V1 (dogfood-010): documentation reorganization. README
  trimmed from ~1,000 lines to 125 with seven canonical sections
  (Status, Install, Quick Start (Human Operator), Quick Start
  (Coding Agent), What It Is For, Documentation Map, License).
  Behavior model, sequential 1–11 usage walkthrough, dogfood-NNN
  history, per-RFC subsections, and command reference moved out
  of the README into `docs/GETTING_STARTED.md`,
  `docs/HOW_TO_HUMAN.md`, `docs/HOW_TO_AGENT.md`,
  `docs/WRITING_WORKFLOWS.md`, `docs/CLI_REFERENCE.md`,
  `docs/INDEX.md`, and `docs/dogfood/HISTORICAL.md`. AGENTS.md
  slimmed (153 → 104 lines) to point at `docs/HOW_TO_AGENT.md`
  rather than reciting the verbs inline. Three new tests in
  `tests/test_doc_links.py` enforce relative-link integrity, the
  README line budget, and the human/agent quick-start heading
  split. Documentation only — no behavior change, no schema move.

## 1.0.0 — 2026-05-08

First stable release. Every RFC under `docs/rfcs/` is now in an
`accepted` (or `accepted (V1)`) state, and every V1 RFC has shipped
its implementation slice. The `0.x` line tracked individual RFC
landings on top of the V1 MVP baseline; `1.0.0` is the version the
runner exposes once the full V1 surface is on main.

### Highlights since 0.1.0

- **RFC 0006** — forward-only SQLite migration system (`PRAGMA
  user_version`); a database newer than the runner exits with
  code 9.
- **RFC 0007** — workflow visualization (`workflow graph` and
  `run graph` with Mermaid / JSON / Graphviz DOT / state-annotated
  ASCII output).
- **RFC 0008** — opt-in per-job git worktree isolation
  (`worktree create | release | list`) for parallel repo-write
  jobs.
- **RFC 0009** — long-lived process supervision
  (`supervise start | send | stop | status | list`) so an agent
  CLI can be held alive across multiple work packets.
- **RFC 0010 V1+V1.5+V2** — tool harness profiles surfaced on work
  packets, plus the reference Claude Code supervised wrapper at
  `.striatum/bin/claude-supervised-wrapper.sh`.
- **RFC 0011** — explicit session close + run-terminal auto-close
  (`session close`); doctor's `active_session_on_terminal_run`
  warning now clears by construction on clean-finish runs.
- **RFC 0012 V1** — local HTTP / Unix-socket service
  (`striatum serve`) with SSE for events and a mutation gate
  (`--allow-mutations`).
- **RFC 0013 V1** — local web UI: vanilla-JS SPA bundled at
  `src/striatum/web/static/` and served by `striatum serve --web`.
- **RFC 0014 V1** — process adapter completion guarantees
  (post-exit output validation, structured blocker payloads,
  `recovery process-reconcile`, doctor `process_*` checks). Closed
  [issue #1](https://github.com/halbritt/striatum/issues/1).
- **RFC 0015 V1** — self-contained agent skill bundles
  (`striatum skills install`, `init --with-skills`, doctor
  `skills_missing` / `skills_outdated`).
- **RFC 0016 V1** — live dependency graph panel in
  `striatum dashboard`; `run graph --format ascii` reuses the same
  pure renderer for one-shot snapshots.
- **Reviewer policy & artifact contracts** — RFCs 0002/0003/0004/0005
  shipped reviewer access scope + context policy fields, support
  ledgers, action-item ledgers, and harness improvement proposals
  with V1 front-matter schemas under `striatum.artifacts`.

### Tooling

- 50 source modules under `src/striatum/`, 260 tests under
  `tests/`, lint + mypy clean. The Makefile targets `install`,
  `lint`, `typecheck`, `test`, `smoke` are the supported entry
  points.
- `pyproject.toml`'s `[tool.setuptools.package-data]` ships the
  web SPA (`striatum.web.static`) and the agent skill templates
  (`striatum.skills.templates`) with the wheel.

### Notes for upgraders

- The `1.0.0` jump from `0.5.0` is purely a release-naming change;
  every behavior in `1.0.0` already shipped on main as part of the
  `0.2.0`–`0.5.0` line.
- The `striatum.workflow.v1`, `striatum.work-packet.v1`,
  `striatum.skills.manifest.v1`, and the per-kind front-matter
  schema versions remain V1; future schema changes will continue
  to use V1.x suffixes or new V2 schemas behind explicit RFCs.

## 0.5.0 — 2026-05-08

### Added

- RFC 0015 V1 (dogfood-009): self-contained agent skill bundles.
  New `striatum skills install [--profile {claude_code, generic}]
  [--scope {project, user}] [--namespace <prefix>] [--force]
  [--dry-run]` writes a Markdown bundle into the target tree that
  teaches a Striatum-aware agent how to drive the runner without
  reading the source repo. The Claude Code profile produces five
  skills (`striatum-workflow` router plus `striatum-scaffold`,
  `striatum-claim-loop`, `striatum-supervise`, `striatum-recover`)
  under `.claude/skills/<namespace>striatum-*/SKILL.md`; the
  generic profile produces a single
  `<namespace>STRIATUM_AGENT_GUIDE.md` for any agent CLI without a
  skill-discovery convention. Each install records a
  `striatum.skills.manifest.v1` JSON manifest with the rendered
  SHA256, the bundled-template SHA256, and the runner version per
  file. A re-install is byte-identical; an operator-edited file is
  `refused_modified` without `--force`; `--dry-run` writes nothing
  and prints the plan. New `striatum init [--with-skills [profile]]`
  flag runs the same install pipeline immediately after `init`.
  New doctor checks `skills_missing` (recorded file absent on disk)
  and `skills_outdated` (manifest version older than running
  install, or template SHA drift) surface the exact `skills install`
  invocation that would clear the condition; the runner never
  auto-regenerates. The bundle emits no external URLs (a unit test
  enforces no `http://` / `https://`) and ships inside the Python
  distribution via `[tool.setuptools.package-data]`. Tests at
  `tests/test_skills_install.py` (16 cases). `__version__` bumped
  to 0.5.0 (alongside the pyproject bump). The `codex` and
  `gemini` profiles plus `--profile all` and parser-walked verb
  tables are step 3 of the RFC's path and remain deferred.

## 0.4.0 — 2026-05-08

### Added

- RFC 0016 V1 (dogfood-008): live dependency graph panel in
  `striatum dashboard`. The frame now appends a layered ASCII view
  of the run's workflow graph annotated with current job state
  (`Q`/`R`/`C`/`B`/`H`/`F`/`P`/`X`/`S`) when the terminal is at
  least 100 columns wide and 30 lines tall and the workflow has at
  least one edge. Auto-detection can be overridden with `--graph` /
  `--no-graph`; `--graph-only` hides the rest of the frame for
  graph-first viewing; `--graph-style {auto,layered,list,fancy}`
  forces a layout (`fancy` falls back to `layered` in V1);
  `--graph-no-cycles` suppresses dashed `~~>` back-edges. ANSI 16
  colors quantize the existing Mermaid state palette and are gated
  on `isatty()` plus `NO_COLOR` (de-facto standard). New
  `striatum run graph --format ascii` reuses the same pure renderer
  for one-shot snapshots. Refactor: `compute_node_states(conn, *,
  run_id)` lifted from `cli/introspect.run_graph` to
  `striatum.workflow` so the dashboard and the existing graph CLI
  share one source of truth for "current state after a requeue."
  Tests at `tests/test_dashboard.py` (11 new cases covering
  layered/list/no-cycles/color/no-color/graph-only/ASCII format
  parity and an ANSI-table-vs-Mermaid-fills coverage guard).

## 0.3.0 — 2026-05-08

### Added

- RFC 0013 V1 (dogfood-007): local web UI. Bundled vanilla-JS SPA at
  `src/striatum/web/static/{index.html,app.js,app.css}` served by
  `striatum serve --web` (no-op flag in 0.2.0; now serves the real
  UI). Five views: run list, run detail with live SSE event log,
  job detail, artifact viewer with per-kind front-matter formatting
  (decision badge, finding verdict + severity chip,
  harness-improvement-proposal target chip, synthesis input list),
  and doctor. Tiny in-house Markdown renderer with HTML escaped at
  the input boundary; no external CDN imports; CSP header on every
  static and artifact-raw response. New endpoint
  `GET /v1/artifacts/<id>/raw` streams artifact bytes for the
  viewer. Static assets ship inside the wheel via
  `[tool.setuptools.package-data]`. Tests at
  `tests/test_web_ui.py` (8 cases). Mutation buttons (step 7 of
  the RFC) deferred.

### Fixed

- CI release-metadata check now sources the expected version from
  `pyproject.toml` instead of a hardcoded constant, so version
  bumps don't require touching the script.
- Test service-readiness window bumped to 30s so cold imports on
  macOS GitHub runners don't false-fail.
- Unix-socket service test uses a short `tempfile.mkdtemp` path so
  macOS's ~104-byte AF_UNIX limit doesn't trigger.

## 0.2.0 — 2026-05-08

First tagged release since the V1 scaffolding. The backlog of RFCs
landed before this point (run recovery / dogfood fixes, reviewer
independence policy, support ledgers + critique-to-action loops +
harness meta-optimization, SQLite migrations, workflow
visualization, worktree isolation, long-lived process supervision,
tool harness profiles V1+V1.5+V2, session close + auto-close,
process adapter completion guarantees) is treated as the `0.1.0`
baseline. `0.2.0` lands RFC 0012 V1 on top of that baseline as the
first explicitly versioned release. Subsequent RFCs bump the minor
version on landing.

### Added

- RFC 0012 V1 (dogfood-006): local HTTP / Unix-socket service. New
  `striatum serve` command runs a `ThreadingHTTPServer` on TCP
  loopback (default `127.0.0.1`) or a Unix-domain socket; refuses
  non-loopback hosts at startup with exit 8. Endpoints:
  `/v1/health`, `POST /v1/invoke`, `/v1/runs`, `/v1/runs/<id>`,
  `/v1/runs/<id>/why`, `/v1/runs/<id>/dashboard`,
  `/v1/runs/<id>/events` (SSE), `/v1/doctor`. Mutations gated
  behind `--allow-mutations` (whitelist of read verbs); auth via
  filesystem permissions on Unix sockets or optional `--token` on
  HTTP (length-safe constant-time compare). Single-instance via
  PID file; graceful shutdown on SIGTERM/SIGINT. New module
  `src/striatum/service.py`; tests at `tests/test_service.py` (16
  cases). Closes the long-standing D006 promise of an "optional
  Unix-socket / local HTTP API later for Slack, TUI, and web
  adapters" — the four V1 acceptance criteria all pass.
- RFC 0014 V1 / issue #1 (dogfood-005): process adapter completion
  guarantees. After every `striatum adapter run` exit (including
  timeout-fired SIGTERMs), the runner inspects required
  `expected_artifacts` and, for review jobs, the verdict table. When
  any required output is missing — or the child exited non-zero or
  hit the timeout — the job transitions from `running` to `blocked`,
  a blocker row is inserted with a structured `blocker_kind`
  (`process_outputs_missing`, `process_review_verdict_missing`,
  `process_exit_nonzero`, `process_timeout_exceeded`,
  `process_lost_with_outputs_missing`), and a privacy-safe diagnostic
  envelope is recorded as the new `blockers.payload_json` column.
  The envelope contains zero child stdout/stderr (D028 preserved); it
  carries `process_id`, `command`, `exit_code`, `duration_seconds`,
  `timeout_seconds`, `missing_artifact_paths`, `review_verdict_missing`,
  and operator-copyable `recovery_commands`. New CLI surface:
  `striatum adapter run --timeout-seconds <n>` (overrides
  `lanes.<id>.adapter_timeout_seconds`; capped at 86400) and
  `striatum recovery process-reconcile --run-id <id>` (mirrors the
  `recovery requeue-stale` lazy-on-CLI shape from D036). Two new
  doctor checks (`process_running_but_pid_gone`,
  `process_running_with_expired_lease`) and a `process_health`
  summary on `striatum status --run-id`. Migrations v8
  (`process_executions.state` enum + `'timed_out'` and `'lost'`) and
  v9 (`blockers.payload_json`); both idempotent against fresh DBs.
  Tests at `tests/test_process_adapter.py` (15 new cases). Closes
  [issue #1](https://github.com/halbritt/striatum/issues/1).
- `branch.mode` is now a closed enum (`"auto"` or `"confirm"`) and
  defaults to `"auto"` when omitted. In auto mode, `run prepare`
  atomically creates the suggested branch and transitions the run to
  `ready`, eliminating the separate `striatum branch confirm --create`
  step that was previously required. The response includes
  `branch_mode`, `branch`, `branch_created`, `current_git_branch`, and
  any warning. Workflows that explicitly want the manual gate can set
  `branch.mode: "confirm"`; behaviour there is unchanged. If git
  checkout fails during auto mode (dirty tree, conflicting branch),
  the run falls back to `needs_branch_confirmation` so the operator
  can resolve the issue and run `branch confirm` manually. Migrated
  the in-repo dogfood-001/-001-v2/-002/-003/-004 and the
  `examples/harness-profiles/` workflows to auto mode; remaining
  example fixtures keep `mode: "confirm"` for test-coverage symmetry.
  Five new tests in `tests/test_cli_mvp.py` cover the auto path,
  default-when-omitted, the still-functioning confirm path, unknown
  mode rejection, and the auto-without-suggested-name guard.
- RFC 0010 V2 / HARNESS-001 (dogfood-004): reference Claude Code
  supervised wrapper at `.striatum/bin/claude-supervised-wrapper.sh`.
  Bash `while IFS= read -r` loop that spawns a fresh `claude --print`
  per packet — each Striatum work packet is independent, so per-packet
  fresh-context matches the workflow's `fresh_session_required`
  defaults and avoids depending on Claude Code's undocumented
  multi-turn `--input-format stream-json` behaviour. Inner stdout
  and stderr go to `/dev/null` (RFC 0009 / D028); SIGTERM trap
  cleans up the in-flight inner process. Verification test at
  `tests/test_claude_supervised_wrapper.py` (4 cases, stub-claude on
  `$PATH` so it does not depend on the real binary). Closes
  `docs/dogfood/003/findings/HARNESS-001.md`.
- RFC 0010 V1.5 (HARNESS-001 follow-up): workflow-validate lint warning
  for missing repo-relative process-lane command paths. Fires when
  `lane.command[0]` looks like a repo-relative path (contains a slash
  or starts with `./`/`../`) and the file does not exist under the
  workflow's repo root. Surfaces under the `warnings` key in
  `workflow validate --json` and `workflow plan --json`. Non-blocking;
  bare binary names and absolute paths are not checked. Closes the V1.5
  step of `docs/dogfood/003/findings/HARNESS-001.md`.
- RFC 0010 V1 (dogfood-003): optional `harness_profiles` workflow map
  and per-lane `harness_profile_id` reference. When a lane references a
  declared profile, `claim-next` adds a `harness_profile` block to the
  work packet (passthrough projection of the profile body plus
  `profile_id`). Workflows that omit `harness_profiles` produce
  unchanged packets. Validation accepts the closed tool-family set
  `{generic, codex, claude_code, gemini_cli}`, requires `tool_family`
  and `strategy_version`, and enforces D021 accountability
  (`native_subagents = internal_to_parent_session`,
  `first_class_registration = not_supported`). Unknown sibling fields
  on profile bodies are accepted as lint warnings, surfaced under a
  `warnings` key in `striatum workflow validate --json` and
  `workflow plan --json`. Reference fixture lives at
  `examples/harness-profiles/workflow.json`. Tests in
  `tests/test_harness_profiles.py` cover validation, packet exposure,
  backwards compatibility, and fixture loading (including the
  dogfood-003 four-profile fixture).
- D055 follow-ups (post-RFC-0011): `recovery cancel-job --cascade`
  over a whole run now transitions `runs.state` to `'canceled'`
  (previously `'completed'`) when no job actually completed; auto-close
  fires under `source: "run_canceled"`, matching the source enum value
  RFC 0011 reserved. A new `test_run_failed_auto_closes_active_sessions`
  rounds out the source-enum matrix by exercising the reject-verdict
  path that drives a run to `'failed'`. Migration helper
  `striatum.migrations.rebuild_table()` extracts the FK-safe rebuild
  pattern (PRAGMA foreign_keys OFF + IF EXISTS partial-state recovery
  + DROP/RENAME) so future migrations against tables with
  self-referential FKs do not re-discover the requirement; v7 is
  retrofitted onto the helper. v5 remains untouched as immutable
  historical record.
- RFC 0011 (dogfood-002): explicit session close + run-terminal
  auto-close. New `striatum session close --session-id <id> --reason
  <text>` command transitions an `active` session to a new `closed`
  terminal state, recording `closed_at` and a non-empty
  `close_reason` and emitting a `session.closed` event with
  `source: "explicit"`. Idempotent against already-terminal sessions
  (returns the existing row plus a `note: "session was already
  <state>"`); refuses with exit 4 when the session holds an active
  lease (message points the operator at `striatum release`). When a
  run transitions to a terminal state, every still-active session on
  the run is auto-closed inside the same transaction with `source` of
  `"run_completed"`, `"run_failed"`, or `"run_canceled"` — eliminating
  the persistent `active_session_on_terminal_run` doctor warning that
  fired on every clean-finish run before this change. Migration
  version 7 adds the `closed` state value plus the `closed_at` and
  `close_reason` columns. `evidence export` and `run summary` carry a
  per-session block with the new fields; `RUN_SUMMARY.md` gains a
  `## Sessions` section.
- HARNESS-001 fixes (dogfood-001 v2): `docs/SPEC.md` "Supervised lane
  command contract" subsection making the three supervised-lane
  requirements explicit (alive across packets, NDJSON stdin, calls back
  via `striatum` CLI). New `doctor` problem record
  `supervisor_lost_with_held_lease` plus the stable `status` next-action
  `recover_orphan_supervisor` that fires when a supervisor row is
  `lost` while the session still owns an unexpired active lease.
  `striatum supervise stop` is idempotent against an already-`lost` or
  `stopped` supervisor: returns the existing terminal row plus a
  `note` describing the prior state instead of raising
  `InvalidTransitionError`.
- HARNESS-002 fixes (dogfood-001 v2): new `doctor` problem record
  `editable_install_outside_repo` warns when the running install is
  outside the repo argument and the repo is itself a Striatum source
  tree (suppressed when the repo is just a target, to avoid false
  positives). `striatum init` against a fresh DB now refuses with exit
  3 when the repo's source-tree `LATEST_VERSION` is higher than the
  running install's, with a clear message pointing at
  `pip install -e <repo>`. `Makefile install` resolves the install path
  via `$(MAKEFILE_DIR)` so `make install` from any cwd installs *this*
  Makefile's directory in editable mode (the previous `pip install -e
  .` was cwd-dependent and silently pinned to a Claude Code worktree).
- HARNESS-003 fixes (dogfood-001 v2): `docs/SPEC.md` "Reviewer
  Independence (advisory)" and "Byline Integrity" subsections making
  the runner's enforcement boundary explicit. New `doctor` problem
  record `reviewer_independence_unverified` flags two observable
  breaches — sessions that share a supervisor pid, or a reviewer
  session running unsupervised on a run whose author is supervised.
  `register-session --role reviewer` refuses when the workflow
  declares `reviewer_context_policy: fresh` and an active author
  session already exists, unless `--force-non-fresh --reason "..."` is
  passed; the reason is recorded in the new
  `sessions.non_fresh_reason` column. `publish-artifact` records the
  artifact file's actual `author:` line in the new
  `artifacts.author_line` column (NULL when the file omits it);
  evidence exports and run summaries read the actual column so a
  missing byline renders as `author: <missing>` rather than the
  workflow's declared expected. Migration version 6 adds both columns.
- HARNESS-004 fix (dogfood-001 v2): `docs/dogfood/001/roles/reviewer.md`
  now points reviewer harness proposals at
  `docs/dogfood/001/review/HARNESS-NNN.md` (inside the review job's
  `write_scope.allowed_paths`) instead of `docs/dogfood/001/findings/`
  (which is the author's path and is rejected by the publisher with
  exit 6). `tests/test_harness_v2_fixes.py::test_reviewer_role_doc_paths_match_write_scope`
  walks every dogfood reviewer role doc and asserts each
  `HARNESS-NNN.md` instruction path is contained in the corresponding
  review job's allowed paths.
- `striatum workflow graph --format dot <workflow.json>` emits a Graphviz
  `digraph striatum_workflow { ... }` alongside the existing Mermaid
  (default) and JSON outputs. Same nodes, dependency edges, parallel
  groups (rendered as `subgraph cluster_<group>` blocks), and bounded
  `needs_revision` cycle edges (rendered as dashed arrows with the
  `max_iterations` count). Pipe through `dot -Tsvg` to render.
- Three new artifact kinds and front-matter schemas (RFCs 0003/0004/0005,
  accepted): `support_ledger` (`striatum.support_ledger.v1`),
  `action_item_ledger` (`striatum.action_item_ledger.v1`), and
  `harness_improvement_proposal`
  (`striatum.harness_improvement_proposal.v1`). Migration version 5 drops the
  SQL `CHECK (artifact_kind IN (...))` on the `artifacts` table; allowed kinds
  now live in `striatum.artifacts.ALLOWED_ARTIFACT_KINDS` and are enforced by
  `publish-artifact` (`ArtifactError`, exit 6) and workflow validation
  (`WorkflowError`, exit 8). Reference fixture
  `examples/support-ledger-flow/` exercises the produce -> support ledger ->
  evidence audit -> final review pattern; "evidence audit" is a workflow
  convention name, not a new `job_type`.
- Reviewer independence policy fields on review jobs (RFC 0002, D051).
  `type: "review"` jobs may declare `reviewer_access_scope`
  (`document_only` | `artifact_augmented` | `repo_level`) and
  `reviewer_context_policy` (`fresh` | `cross_round`). The validator
  rejects unknown values, rejects the fields on non-review jobs, and
  rejects the explicit `reviewer_context_policy: "fresh"` +
  `fresh_session_required: false` conflict. Setting
  `reviewer_context_policy: "fresh"` without `fresh_session_required`
  silently stores the prepared job row with `fresh_session_required = 1`.
  Work packets gain a `review_policy` block (`access_scope`,
  `context_policy`, `instruction`) only when the workflow declares at
  least one of the fields; existing fixtures produce identical packets.
  The `examples/rfc-0014-operational-artifact-home/workflow.json` fixture
  now labels its three independent root reviews as `document_only` and
  `fresh`.
- `striatum run graph --run-id <id> [--format mermaid|json]` renders the
  workflow graph for an existing run with each node colored by current job
  state. Mermaid output appends a `classDef` palette plus per-node `class`
  assignments (completed/running/claimed/acked/blocked/stale_lease/
  waiting_human/failed/canceled/queued/pending); JSON output adds
  `current_state`, `attempt`, and a `latest_verdict` block on review nodes.
  The runner picks the highest-`attempt` row per `workflow_job_id` so
  requeued attempts show their latest state.
- `striatum list ...` subcommand group for read-only enumeration of runs,
  sessions, jobs, artifacts, and workflow snapshots. Each command returns a
  stable `{"items": [...], "count": N}` envelope shaped from existing SQLite
  state. `list runs` joins `workflow_snapshots` to surface `workflow_id`;
  `list sessions --run-id <id>` accepts `--state`, `--role`, `--lane`;
  `list jobs --run-id <id>` includes the latest verdict for review jobs and
  accepts `--state` and `--workflow-job-id`; `list artifacts --run-id <id>`
  embeds the structured author byline and accepts `--kind`; `list workflows`
  reports loaded snapshots with their `content_sha256`. Every run-scoped
  variant applies the lazy lease-expiry sweep before reading.
- `striatum checkpoint resolve --blocker-id <id> --action {continue|cancel}
  [--decision-id <id>]` resolves an open `human_checkpoint` blocker:
  `continue` re-queues the affected job and emits `checkpoint.resolved`;
  `cancel` marks the affected job `canceled` and emits
  `checkpoint.canceled`. Optional `--decision-id` validates a run-level
  decision artifact and records it on the resolution event payload.
- `striatum recovery cancel-job --run-id <id> --job-id <id> --reason <text>
  [--cascade]` is the explicit operator cancel for a non-terminal job.
  Refuses terminal-state jobs and refuses jobs with blocked dependents
  unless `--cascade` is set, in which case dependents are canceled
  transitively in the same transaction.
- Supervised-aware `claim-next`: when the claiming session has an
  `attached` supervisor, the runner writes the freshly built packet
  through the supervisor's stdin pipe inside the same transaction,
  refreshes `heartbeat_at`, and emits a `supervisor.packet_delivered`
  event. The CLI response gains an optional `supervisor_delivery` field.
  Pipe-missing or write-fail transitions the supervisor to `lost` while
  still committing and returning the packet so the caller can recover.
- Optional per-kind Markdown front-matter validation in `publish-artifact` for
  `decision` (`striatum.decision.v1`), `finding` (`striatum.finding.v1`),
  `findings_ledger` (`striatum.findings_ledger.v1`), and `synthesis`
  (`striatum.synthesis.v1`). Front matter is read with a minimal
  `key: <json-value>` parser, validated only when present, and never rewritten
  by the publisher. Other artifact kinds remain unschemaed.
- New example fixtures: `examples/human-checkpoint-flow/` (analyze -> review
  -> decide, where the decide job is a `human_checkpoint`-typed job whose
  session calls `block --severity human_checkpoint` to surface an operator
  checkpoint and the operator records the decision via
  `striatum decision record --outcome accepted`), and
  `examples/adapter-unavailable-flow/` (a process-lane workflow that requests
  `network=enforced` and is rejected at validation because the process adapter
  only provides `advisory_strict` for that constraint). Both are covered by
  end-to-end tests in `tests/test_cli_mvp.py`.
- `striatum dashboard` command: a compact, dependency-free terminal dashboard
  over the existing SQLite state that summarizes run state, job counts,
  verdicts, open blockers, claimable work, deterministic next actions, and
  the most recent events. Supports `--refresh` for live mode and `--once` for
  one-shot rendering in scripts and CI.
- Long-lived process supervision (RFC 0009). New `striatum supervise
  start | send | stop | status | list` commands hold an agent CLI alive
  across multiple work packets: `start` forks the lane command with
  `start_new_session=True` and a per-supervisor named pipe at
  `.striatum/scratch/<supervisor_id>/stdin.pipe`, `send` delivers a stored
  work packet as a newline-terminated JSON line through that pipe, `stop`
  sends `SIGTERM` (then `SIGKILL` after a five-second grace), `status` probes
  liveness and lazily transitions stuck rows to `lost`, and `list` reports
  supervisors for a run. The single-shot `striatum adapter run` command is
  unchanged — both flows coexist. Migration version 4 adds the new
  `process_supervisors` table with a partial unique index enforcing "at most
  one active supervisor per session". `expire_leases` marks supervised
  sessions `lost` without auto-killing the OS process, and `striatum doctor`
  flags supervisors whose pid is gone or whose stdin pipe is missing from
  disk. Stdout and stderr are sent to `DEVNULL`; the supervisor never
  captures transcripts or parses agent output for workflow state, preserving
  D028 and D037.
- `striatum workflow init [--style minimal|review|code-change] <path>` writes
  a starter workflow tree (`workflow.json` plus `roles/` and `prompts/`
  stubs) that validates cleanly with `workflow validate`. Refuses to
  overwrite an existing path. The `review` default mirrors the
  `examples/code-change-flow/` shape with placeholder paths; `minimal` skips
  review; `code-change` adds a one-shot `needs_revision` cycle.
- New example fixtures: `examples/code-change-flow/` (draft -> review -> apply
  with a one-shot needs_revision cycle) and
  `examples/failed-review-revision-cycle/` (single review whose second
  needs_revision opens a configured human checkpoint).
- Opt-in per-job git worktree isolation for parallel repo-write jobs
  (RFC 0008). Lanes declare `worktree_isolation: per_job` and the runner
  advertises `worktree_required: true` plus the `striatum worktree create`
  command on matching work packets without auto-creating anything. New CLI
  subcommands `worktree create | release | list` manage the worktrees,
  `publish-artifact` reads files from the active per-job worktree but
  records logical repo-relative paths so artifacts stay valid main-branch
  provenance, lease expiry marks worktrees `abandoned` for operator
  inspection, and `doctor` flags orphaned and missing-on-disk worktree rows.
  Migration version 2 adds the new `job_worktrees` table.
- Forward-only SQLite migration system. Schema version is tracked through
  `PRAGMA user_version`, the current schema is registered as
  `user_version = 1`, `striatum init` and every connect apply pending
  migrations inside a single `BEGIN IMMEDIATE` transaction, and a database
  newer than the runner supports is refused with the new exit code 9.
- Fourth adapter enforcement level `advisory_strict` (between `advisory` and
  `enforced`). The process adapter graduates `network=forbidden` and
  `repo_scope=local_only` to `advisory_strict`: proxy env vars are scrubbed
  from the child env when network is forbidden, and
  `STRIATUM_NETWORK_POLICY` / `STRIATUM_REPO_SCOPE` sentinels are set so
  cooperating agents can honor the policy.
- RFC 0009 (proposed) describing the V2 long-lived process supervisor for
  agent CLIs that span multiple work packets.

### Changed

- Split `striatum.cli` from a single ~3.5k-line module into a package
  (`src/striatum/cli/`) organized by concern: `parser`, `dispatch`,
  `mutations`, `introspect`, `evidence`, `run_summary`, `recovery`,
  `worktree`, `supervise`, and `workflow_init`. Public surface is preserved
  via re-exports in `striatum/cli/__init__.py`; the `striatum.cli:main`
  console entry point and `python -m striatum.cli` continue to work
  unchanged. Behavior is identical (pure refactor, all existing tests pass);
  cross-module helper calls that need to honor `monkeypatch.setattr` against
  `striatum.cli` use a lazy `from striatum import cli as _cli` lookup.
- `striatum doctor --verbose` now augments the historical string `problems`
  list with a `problem_records` list of structured rows. Each record carries
  a stable `check` name (e.g. `active_job_without_active_lease`,
  `stale_queue_message_claim`, `worktree_path_missing_on_disk`), the
  affected `id`, and a small `context` map. The string list is preserved
  verbatim so callers that already grep `problems` keep working.
- `striatum run summary` Markdown output now groups verdicts by review job
  with an attempt count and rolled-up prior verdicts, appends the structured
  author byline (`author: <role>-<model>-<ordinal>`) to each artifact line,
  surfaces the recorded branch alongside the current git branch with an
  explicit `(MISMATCH)` annotation when they differ, and prints a Timing
  block with `created_at`, `started_at`, `completed_at`, and wall-clock
  `duration`.
- Workflow validator now rejects cross-job expected-artifact path
  collisions, write-scope `allowed_paths` that overlap `forbidden_paths`,
  expected artifacts outside the job's write scope, unsound revision cycles
  whose target does not feed back into the cycle source through workflow
  edges, and parallel groups that mix `repo_write` with review-only jobs.
- Workflow validator emits a deprecation warning to stderr when jobs declare
  the legacy `needs` field; `edges` remains authoritative.
- Cycle resolution now redirects downstream dependencies to the new review
  attempt so jobs gated on the review verdict unblock once the new attempt
  accepts.
- MCP wrapper now speaks LSP-style `Content-Length` framing by default with
  automatic line-delimited fallback. Real MCP clients (Claude Desktop, IDE
  MCP integrations) can connect cleanly; existing line-delimited scripts and
  tests keep working unchanged. Added `python -m striatum.mcp --framing
  {auto,line,framed}` for operators that need to pin the wire shape.
- `striatum branch confirm` now honors the previously inert `--create` and
  `--use-current` flags and adds a new `--strict` flag. `--create` runs
  `git checkout -b <branch>` (with idempotent fallback to `git checkout`),
  `--use-current` records the actual current git branch, and `--strict`
  refuses to record unless the working tree already matches. Default
  behavior remains records-only, and the JSON response now includes `mode`
  and `created` fields.
- Replaced the evidence-export key-name blocklist with a default-deny policy
  registry. Any field not explicitly classified as `safe` in
  `EVIDENCE_POLICY` is redacted from exported Markdown, so future schema
  additions cannot silently leak agent or user prose.
- Pushed the `fresh_session_required` filter in `claim_next` into a single
  SQL query using a `NOT EXISTS` correlated subquery, replacing the
  per-candidate Python loop. Added covering index migration for
  `work_packets(run_id, session_id)`.

### Tooling

- (No tooling-only changes pending in this Unreleased window. Tooling work
  in this cycle is bundled with the feature commits above.)

## 0.1.0 - 2026-05-07

- Split Striatum from Engram with history preserved from the former
  `agent-runner/` incubation directory.
- Renamed the package, CLI, workflow schema, and repo-local state directory
  to `striatum`.
- Replaced the initial all-rights-reserved status with Apache-2.0 licensing.
- Added standalone project metadata, CI, and a fresh-clone smoke script.
- Added workflow planning, run-summary export, stale-lease recovery
  introspection, local API wrapper, and minimal process-adapter launch
  support.
- Added workflow graph export, bounded stale-work requeue, decision-artifact
  recording, a local MCP-like stdio wrapper, and explicit adapter
  enforcement validation.
- Added stricter release checks with `ruff`, `mypy`, wheel/sdist smoke, and
  installed package metadata validation.
