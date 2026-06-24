---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
author: adjudicator-author-001
workflow: "rfc-0166-design-v2"
run_id: "run_18b937d089aac777ba76036db712cb0a"
cycle: 1
topic: "RFC 0166 P0 v2 REVISION — the sealed-progress silence budget (wedged_no_sealed_progress rung): discharge C1 (one novelty-aware clock on every reset surface) and C2 (corrected no-false-kill proof) while carrying forward the v1-ratified AND-not-OR core and the Part 1-4 mechanism shape (#576)"
participants:
  - holder
  - falsifier_1
  - falsifier_2
  - adjudicator
entries:
  - kind: claim
    by: holder
    refs: ["dialogue:1"]
    text: >-
      The v2 holder publishes a surgical revision of the v1 SPEC that claims to discharge both
      gate constraints while carrying forward everything v1 ratified. C1: the Part-1 floor's
      progress input is changed from raw jobSealedProgressAt = GREATEST(max(artifacts.created_at),
      max(verdicts.created_at)) to a single novelty primitive novelSealedProgressAt — the timestamp
      of the last STRICT advance of a declared-scoped, content-deduped cursor (pos = (distinct
      content_sha256 of DECLARED/milestone artifacts, sealed verdicts, highest satisfied required
      milestone index)), persisted as last_novel_sealed_progress_at on job_recovery_state in the
      0035 ADD COLUMN IF NOT EXISTS degrade-safe style and defined to equal a deterministic
      restart-stable recomputation over append-only rows. The SAME primitive is wired into all three
      reset surfaces the constraint names: the Part-1 floor (§1), the Part-4 telomere reset (§4.2),
      and the RFC 0131 confidence-gate progressAdvanced/consecutive_silent_sweeps (§4.3). The
      undeclared-in-scope publish contract is DECIDED as allowed-but-ignored-for-deadline (rows stay
      publishable provenance but are excluded from novelSealedProgressAt via the
      logical_name = ANY($declared_logical_names) filter). C2: the unqualified "alive-and-working ⇒
      never killed" proof is WITHDRAWN; the proven property is narrowed to "no false-kill for any
      lane tool-fresh / in-tool / emitting work.heartbeat(local_work=true) within ToolProgressSeconds"
      (the heartbeat stamps the #324 tool-call timeline via tools.go:62-67, the build-bearing
      reprieve), and the long-silent no-heartbeat alive lane is named an ACCEPTED bounded residual
      (contained by shadow-first default SealedSilenceSeconds=0, opt-in calibration,
      requeue-not-kill severity reduction, and a P1 operator-grant seam). Carry-forwards declared
      intact: the AND-not-OR core (fire iff sealedSilenceBreached AND exported ToolProgressWedged),
      Parts 1-4, the shadow-first default, the single idempotent escalation. Two accuracy
      corrections: M1 (the new wedged_no_sealed_progress class is decided in recoverStuckJobs, never
      written to the constrained sessions.liveness_stall_class column, so no 0012 CHECK change) and
      M2 (the junk-publisher and the polling-gamer collapse into one tool-fresh class the AND
      deliberately spares). Two named falsification tests: §7-T1 (junk-floor-freeze + converge with
      exactly one escalation across a restart) and §7-T2 (alive-working tool-silent lane no
      hard-kill).
  - kind: challenge
    by: falsifier_1
    refs: ["dialogue:2"]
    correspondence: landed_unrebutted
    text: >-
      C1 re-attack, DECISIVE and unrebutted. The raw-clock hole is repaired on the FLOOR, but the
      gaming surface merely MOVED to the tool-recency side of the AND, and the mandated C1 test is
      not realizable as a real hostile lane. To publish an undeclared junk row a lane must call
      artifact.publish, which requires session_id/job_id/lease_id and enforces the active acting
      session (artifact.go:48-85); and EVERY MCP tools/call carrying repository_id+session_id stamps
      LastToolCallStartedAt/LastToolCallFinishedAt (tools.go:40-67), the exact base
      toolProgressWedged ages against (liveness.go:772-805). So the realistic counterexample is: arm
      SealedSilenceSeconds; the hostile lane never publishes its required artifact but calls
      artifact.publish for fresh undeclared junk_N (distinct bytes) at a cadence below
      ToolProgressSeconds and before each sealed-silence expiry. The C1 primitive correctly freezes
      the floor (so sealedSilenceBreached is true), BUT each publish refreshes the tool timeline so
      toolWedged is false and the AND (sealedSilenceBreached && toolWedged) NEVER fires — the
      recovery action is never entered, requeue_count never advances, the RFC 0131 gate never
      accumulates the promised silent sweeps, the telomere floor is never reached, and there is no
      escalation. This is the SAME hostile undeclared-artifact publisher C1 was written to defeat,
      now evading via the tool axis. The C1 constraint required junk-before-budget-expiry to leave
      the cursor/floor frozen AND let requeue_count, silent sweeps, and exactly-one-escalation
      proceed; v2 delivers only the first half. The holder's own §6 names the residual as a P1
      tool-axis-novelty seam, so the holder concedes the convergence the gate demanded is deferred.
      The §7-T1 setup (tool-call timeline stale past ToolProgressSeconds WHILE junk rows land before
      every expiry) is internally contradictory for a real lane — it is realizable only by
      out-of-band DB row insertion, which is not a hostile lane publishing through Striatum. Required:
      deadline-specific tool-axis novelty in P0 (the sealed-silence AND must not treat
      undeclared/deadline-ignored artifact.publish, polling, or other non-forward-progress calls as
      tool progress, while still honoring work.heartbeat(local_work=true) as the C2 reprieve), OR a
      publish contract that stops undeclared junk publishes from serving as a keepalive. Without one,
      C1's hostile-junk-publisher convergence test is not genuinely discharged.
  - kind: challenge
    by: falsifier_2
    refs: ["dialogue:3"]
    correspondence: landed_and_rebutted
    text: >-
      C2 re-attack plus carry-forward regression check. The challenge: §7-T2 is not the no-false-kill
      test the seed required because for the exact alive, legitimately-working, tool-silent,
      no-heartbeat single-final-seal residual lane, when armed, §7-T2(c) accepts a fresh-session
      requeue — and that requeue IS destructive: transfer_requeue → requeueJobSameAttempt is followed
      by closeStalledOwningSession, explicitly so the parked owner cannot reclaim the job
      (recovery_decision_tree.go:1353-1380), the action records stalled_owner_closed (:1409-1421), and
      unsealed in-flight reasoning is discarded; lowering severity from kill to session-close/requeue
      does not make it non-destructive. The constraint said "before the action arms," yet the only
      armed-mode protection (operator-grant) is deferred to P1. The challenge concedes real holder
      progress: the overclaim IS fixed for conformant lanes (work.heartbeat(local_work=true) advances
      last_tool_call_finished_at via HandleHeartbeat at lifecycle.go:843-886, sparing heartbeating
      lanes by the AND); the shadow-first default holds; the AND-not-OR core, the #324 tool half, the
      heartbeat reprieve, and the shadow-first default are all explicitly verified UNREGRESSED.
      ADJUDICATION: the central demand — "no destructive action even when armed on the residual" —
      EXCEEDS what C2 authorized. C2 option (a) (which the holder took) explicitly allowed narrowing
      the claim and naming the long-silent interval an ACCEPTED bounded residual, and the v1 ledger's
      C2 verification gate accepted "the SPEC states the bounded accepted residual AND proves the
      advisory default takes no destructive action" as a clearing path — which §7-T2(a)+(b)+(c)
      satisfy. The holder rebuts the core by faithfully exercising an authorized path. What stands as
      a real (non-gate) finding is narrower: §7-T2 is OVER-TITLED ("must not destructively act" while
      its body accepts a requeue), and the residual's named coverage ("advisory default + operator-
      grant seam") is incomplete in P0 because operator-grant is P1. These are cleanup items, not an
      open C2 gate.
  - kind: constraint
    by: adjudicator
    refs: ["dialogue:2"]
    text: >-
      C1 carries forward UNCLEARED (GATE). The detector clock's FLOOR is genuinely fixed — junk no
      longer advances novelSealedProgressAt, the one primitive drives all three named reset surfaces,
      it is restart-stable, and the publish contract is decided — but the anti-gaming guarantee is
      NOT restored "on every surface": the AND's tool-recency half is a fourth keepalive surface a
      junk-publisher refreshes through artifact.publish itself, so the deterministically-flailing
      junk-publisher provably never enters the recovery action and never converges to the telomere
      floor with one escalation. The named C1 test (§7-T1) cannot be realized against a real hostile
      lane (its tool-stale + junk-per-expiry setup is achievable only by out-of-band row insertion).
      The revision must make the sealed-silence path deadline-aware on the tool axis — e.g. the AND
      must not count undeclared/deadline-ignored artifact.publish or bare polling as tool progress
      for sealedSilenceBreached purposes, while still honoring work.heartbeat(local_work=true) as the
      C2 reprieve — OR adopt a publish contract that prevents undeclared junk publishes from serving
      as a keepalive, so a hostile junk-publisher driving REAL artifact.publish calls still reaches
      the telomere floor with exactly one escalation. If P0 instead deliberately spares the
      junk-publisher, the SPEC must STOP claiming "un-gameable on every surface / no evasion" and
      re-scope the C1 falsification test to a floor-mechanism-only assertion, naming the un-converging
      junk-publisher as an explicit accepted residual rather than a discharged convergence.
  - kind: constraint
    by: adjudicator
    refs: ["dialogue:3"]
    text: >-
      C2 is SUBSTANTIALLY discharged (non-binding cleanup only). The holder faithfully took the
      authorized narrow-with-named-residual path: the unqualified proof is withdrawn, no-false-kill is
      proven for the tool-fresh/in-tool/heartbeating set, the shadow-first default is proven to take
      no destructive action, the heartbeat reprieve is build-bearing, and the residual is named and
      bounded — matching the v1 ledger's C2 verification clearing path. Falsifier 2's demand for
      no-armed-action on the residual exceeds the constraint and is rebutted. Two cleanup items remain
      for the revision (and for the build's test authoring): (1) retitle §7-T2 so it asserts
      no-destructive-action for the PROTECTED set and the default, and requeue-not-hard-kill for the
      named accepted residual — do not title an accepted-false-positive test "must not destructively
      act"; (2) reconcile the residual's named coverage with what P0 ships: either bring the
      operator-grant seam into P0 to cover the armed residual as the C2 text ("advisory default +
      operator-grant seam") implied, or restate the residual as covered by the advisory default only,
      with operator-grant explicitly deferred and the armed requeue-of-an-alive-residual-lane named as
      the accepted P0 risk. Neither item blocks the gate on C2 alone.
verdict: "needs_revision"
rationale: >-
  needs_revision. The v2 revision is strong, source-anchored, and makes real progress: the Part-1
  floor genuinely now consumes NOVELTY not events (novelSealedProgressAt — declared-scoped,
  content-deduped, monotone over append-only rows, restart-stable, persisted as
  last_novel_sealed_progress_at and defined to equal its deterministic recomputation), the SAME
  primitive drives all three reset surfaces the C1 constraint enumerated (Part-1 floor, Part-4
  telomere reset, RFC 0131 progressAdvanced), the undeclared-in-scope publish contract is decided
  (allowed-but-ignored-for-deadline), and the M1 0012-CHECK correction is sound. C2 is substantially
  discharged: the over-claim is withdrawn, no-false-kill is proven for the tool-fresh/in-tool/
  heartbeating set, the work.heartbeat reprieve is build-bearing, and the residual is honestly named
  and bounded by the shadow-first default — exactly the authorized narrow-with-named-residual path.
  No carry-forward regressed: both falsifiers confirm the AND-not-OR core, the #324 tool half, the
  shadow-first default, Parts 1-4, and the single idempotent escalation are intact. BUT the gate does
  not clear because C1 is NOT genuinely discharged. Falsifier 1 lands an unrebutted, source-anchored
  defect: fixing the FLOOR is necessary but not sufficient — a hostile junk-publisher must publish
  through artifact.publish, an MCP tools/call that stamps the #324 tool-call timeline
  (tools.go:40-67), so it keeps toolWedged==false and the AND (sealedSilenceBreached &&
  ToolProgressWedged) never fires; the recovery action is never entered, requeue_count and silent
  sweeps never advance, and the lane never converges to the telomere floor with one escalation. The
  C1 falsification test the gate REQUIRED — "the cursor does not advance, the floor does not move,
  requeue_count and silent sweeps continue advancing, and the lane reaches the telomere floor with
  exactly one operator escalation" under junk-before-every-budget-expiry — is therefore unsatisfiable
  for a real hostile lane: §7-T1's tool-stale-while-junk-publishing setup is realizable only by
  out-of-band DB insertion, which is not a lane publishing through Striatum. The holder ADMITS this
  (§5 obligation 2: the composed residual "evades the deadline (AND-spared)"; §6 defers tool-axis
  novelty to P1), so the §5 claim "Un-gameable (fake progress ⇒ no reset, no evasion) — RESTORED ON
  EVERY SURFACE" is still over-stated and the deterministically-flailing junk-publisher — precisely
  the alive-but-never-completing lane #576 targets — still never escalates. Per the seed's
  needs_revision triggers, "the proof is still over-stated" fires. The fix is concrete and
  in-P0-buildable (deadline-aware tool-axis novelty, or a publish contract that denies junk a
  keepalive, or an explicit re-scope of the C1 test + accepted residual), so this is needs_revision,
  not reject. This is the single allowed v2 revision cycle: a second needs_revision ends the gate
  uncleared and routes to the operator for a fresh -v3 run.
findings:
  - id: F-C1-TOOL-AXIS-KEEPALIVE-EVADES-ACTION
    severity: high
    posture: anti_gaming_broken
    status: converted_to_constraint
    source_refs: ["dialogue:2"]
    affected_invariants:
      - "the deadline must consume novelty, not events, on EVERY keepalive surface — including the AND's tool-recency half, not only the three named reset surfaces"
      - "a deterministically-flailing junk-publisher must converge to the telomere floor with exactly one operator escalation (the core #576 guarantee)"
      - "the C1 falsification test must be realizable by a real hostile lane driving artifact.publish through Striatum, not by out-of-band DB row insertion"
    challenge: >-
      v2 fixes the FLOOR (junk no longer advances novelSealedProgressAt) but the gaming surface moves
      to the tool-recency half of the AND. artifact.publish is an MCP tools/call that stamps
      LastToolCallStartedAt/Finished (tools.go:40-67), the base toolProgressWedged ages against
      (liveness.go:772-805). A hostile lane junk-publishing before every sealed-silence expiry (and
      below ToolProgressSeconds) freezes the floor (sealedSilenceBreached true) yet keeps
      toolWedged==false, so the AND never fires, the recovery action is never entered, requeue_count
      and consecutive_silent_sweeps never advance, the telomere floor is never reached, and there is
      no escalation. The mandated C1 test (§7-T1) is unrealizable for a real lane — its
      tool-stale-while-junk-publishing setup is achievable only by out-of-band row insertion. The
      holder concedes the residual (§5 obl 2 "evades the deadline (AND-spared)"; §6 P1 tool-axis-
      novelty seam), so "un-gameable on every surface" is over-stated and the junk-publisher variant
      of the #576 lane still never escalates.
    closest_acceptable_answer: >-
      Make the sealed-silence path deadline-aware on the tool axis in P0: the AND must not count
      undeclared/deadline-ignored artifact.publish or bare polling (await_packet/heartbeat-less
      keepalive) as tool progress for sealedSilenceBreached, while still honoring
      work.heartbeat(local_work=true) as the C2 reprieve — so a junk-publisher driving REAL
      artifact.publish calls still reaches the telomere floor with exactly one escalation; OR adopt a
      publish contract that denies undeclared junk publishes a keepalive role. If P0 deliberately
      spares the junk-publisher instead, withdraw the "un-gameable on every surface / no evasion"
      claim, re-scope §7-T1 to a floor-mechanism-only assertion (verified against a genuinely
      tool-silent stuck lane, not a junk-publisher), and name the non-converging junk-publisher an
      explicit accepted residual rather than a discharged convergence.
    requested_constraint_shape:
      kind: gate
  - id: F-C2-NO-ACTION-TEST-OVERTITLED-AND-OPERATOR-GRANT-DEFERRED
    severity: medium
    posture: safety_claim_overclaimed
    status: converted_to_constraint
    source_refs: ["dialogue:3"]
    affected_invariants:
      - "a falsification test's title must match what it asserts — an accepted-false-positive test must not be titled a no-destructive-action test"
      - "the named coverage of an accepted residual (advisory default + operator-grant seam) must match what the P0 slice actually ships"
    challenge: >-
      §7-T2 is titled "an armed rung must not destructively act on an alive, legitimately-working
      tool-silent single-final-seal lane," but its body (c) accepts a fresh-session requeue for the
      armed residual, and that requeue closes the alive owner via closeStalledOwningSession and
      discards unsealed in-flight reasoning (recovery_decision_tree.go:1353-1380, action
      stalled_owner_closed at :1409-1421). The residual is named as "covered by the advisory default +
      operator-grant seam," but the operator-grant seam is deferred to P1, so in armed P0 the residual
      lane has no armed-mode coverage beyond the requeue-not-kill severity reduction. (Falsifier 2's
      stronger demand — no armed action at all — exceeds what C2 authorized and is rebutted; these
      remain as cleanup.)
    closest_acceptable_answer: >-
      Retitle §7-T2 to assert no-destructive-action for the protected set and the default, and
      requeue-not-hard-kill for the named accepted residual; and reconcile the residual's named
      coverage with the P0 slice — either pull the operator-grant seam into P0 to cover the armed
      residual, or restate the residual as covered by the advisory default only with operator-grant
      explicitly deferred and the armed requeue-of-an-alive-residual-lane named as the accepted P0
      risk.
    requested_constraint_shape:
      kind: policy
constraints:
  - id: C1-DEADLINE-AWARE-TOOL-AXIS-NOVELTY-OR-RESCOPED-TEST
    source_finding: F-C1-TOOL-AXIS-KEEPALIVE-EVADES-ACTION
    posture: anti_gaming_broken
    severity: high
    kind: gate
    binding: true
    source_refs: ["dialogue:2"]
    text: >-
      The revision must close the tool-axis keepalive hole so the anti-gaming guarantee holds on
      EVERY surface, not only the floor. Either (a) make the sealed-silence AND deadline-aware on the
      tool axis — undeclared/deadline-ignored artifact.publish and bare polling do not count as tool
      progress for sealedSilenceBreached, while work.heartbeat(local_work=true) is preserved as the
      C2 reprieve — or (b) adopt a publish contract that denies undeclared junk publishes a keepalive
      role, so that a hostile lane junk-publishing through REAL artifact.publish before every budget
      expiry still leaves the floor frozen AND has requeue_count + consecutive_silent_sweeps advance
      to the telomere floor with exactly one escalation. If P0 will not close it, the SPEC must
      withdraw the "un-gameable on every surface / no evasion" claim, re-scope the C1 falsification
      test to a floor-mechanism-only assertion (a genuinely tool-silent stuck lane, not a
      junk-publisher), and name the non-converging junk-publisher an explicit accepted residual.
    verification:
      gate: >-
        Build-bearing falsification test driven by REAL artifact.publish calls from the owning
        session (NOT out-of-band row insertion): a hostile lane that publishes undeclared
        distinct-content junk_N before every sealed-silence budget expiry must leave
        novelSealedProgressAt / the floor frozen AND drive requeue_count and consecutive_silent_sweeps
        to the telomere floor with exactly one idempotent operator escalation, holding across a daemon
        restart; meanwhile a lane emitting work.heartbeat(local_work=true) within ToolProgressSeconds
        is still spared (the C2 reprieve preserved). If instead the junk-publisher is a declared
        accepted residual, the test asserts the floor-freeze only and the SPEC names the
        non-convergence as accepted — no claim of discharged convergence survives.
    final_review_required: true
  - id: C2-CORRECT-NO-ACTION-TEST-TITLE-AND-RESIDUAL-COVERAGE
    source_finding: F-C2-NO-ACTION-TEST-OVERTITLED-AND-OPERATOR-GRANT-DEFERRED
    posture: safety_claim_overclaimed
    severity: medium
    kind: policy
    binding: false
    source_refs: ["dialogue:3"]
    text: >-
      C2 is substantially discharged; this is cleanup, not a gate on C2. Retitle §7-T2 so it asserts
      no-destructive-action for the protected (tool-fresh/in-tool/heartbeating) set and the
      shadow-first default, and requeue-not-hard-kill for the named accepted residual — do not title
      an accepted-false-positive test "must not destructively act." Reconcile the residual's named
      coverage with the P0 slice: either bring the operator-grant seam into P0 to cover the armed
      residual as the C2 text ("advisory default + operator-grant seam") implied, or restate the
      residual as covered by the advisory default only, with operator-grant explicitly deferred and
      the armed requeue-of-an-alive-residual-lane named as the accepted P0 risk.
    verification:
      expected_stage: "rfc-0166-design-v3 holder revision and rfc-0166-build test authoring"
    final_review_required: false
branches:
  anti_gaming_broken: "blocked"
  safety_claim_overclaimed: "cleared_with_constraints"
---

# Collaboration Ledger — RFC 0166 P0 v2 REVISION (cycle 1)

**Verdict: `needs_revision`.** The v2 revision genuinely fixes the C1 *floor* and
substantially discharges C2, with no carry-forward regression — but the C1
anti-gaming guarantee is **not restored on every surface**: the junk-publisher
attack simply moved from the sealed clock to the **tool-recency half of the AND**,
where `artifact.publish` itself keeps the lane tool-fresh, so the rung never fires
and the flailing lane never escalates. The C1 falsification test the gate required
is therefore unsatisfiable for a real hostile lane. This is the single allowed v2
revision cycle: a second `needs_revision` ends the gate uncleared and routes to the
operator for a fresh `-v3` run.

## C1 — novelty-aware clock on every reset surface: NOT GENUINELY DISCHARGED

**What is genuinely fixed (real progress):**

- The Part-1 floor now reads `novelSealedProgressAt` (a declared-scoped,
  content-deduped, milestone/verdict cursor), **not** raw `jobSealedProgressAt`.
  An undeclared/identical/replayed row no longer advances the floor — the v1
  Falsifier-2 raw-clock hole is closed on the floor.
- The **same** primitive drives all three reset surfaces the constraint
  enumerated: the Part-1 floor (§1), the Part-4 telomere reset (§4.2), and the
  RFC 0131 confidence-gate `progressAdvanced` / `consecutive_silent_sweeps` (§4.3).
- The cursor is **restart-stable** (persisted `last_novel_sealed_progress_at`
  defined to equal a deterministic recomputation over append-only rows; the
  doctor invariant `last_novel_sealed_progress_at == novelSealedProgressAt(job)`).
- The publish contract is **decided**: undeclared in-scope rows are
  *allowed-but-ignored-for-deadline*.

**Why it still does not clear (Falsifier 1, `landed_unrebutted`):** fixing the
floor is necessary but not sufficient. A real junk-publisher must publish through
`artifact.publish`, which is an MCP `tools/call` that stamps the #324 tool-call
timeline (`tools.go:40-67`); `toolProgressWedged` ages against exactly that base
(`liveness.go:772-805`). So a lane junk-publishing before every sealed-silence
expiry (and below `ToolProgressSeconds`) freezes the floor (`sealedSilenceBreached`
true) **but keeps `toolWedged==false`** — the AND `sealedSilenceBreached &&
ToolProgressWedged` never fires, the recovery action is never entered,
`requeue_count` and `consecutive_silent_sweeps` never advance, the telomere floor
is never reached, and there is **no escalation**.

The C1 constraint explicitly required the junk-publisher to leave the cursor/floor
frozen **and** let "`requeue_count` and silent sweeps continue advancing, and the
lane reaches the telomere floor with exactly one operator escalation." v2 delivers
only the first half. The mandated test §7-T1 papers over this by positing a
tool-stale lane *while* junk rows land each budget−ε — a setup realizable only by
**out-of-band DB row insertion**, not a lane publishing through Striatum. The
holder concedes the gap (§5 obligation 2: the residual "evades the deadline
(AND-spared)"; §6 defers tool-axis novelty to P1), so §5's "Un-gameable … RESTORED
ON EVERY SURFACE / no evasion" is **over-stated** and the junk-publishing variant
of the #576 alive-but-never-completing lane still never escalates. → **C1 (gate),
binding, carries to `-v3`.**

## C2 — corrected no-false-kill: SUBSTANTIALLY DISCHARGED (cleanup only)

The holder took the **authorized** C2 path: it withdrew the unqualified proof,
proved no-false-kill for the tool-fresh / in-tool / `work.heartbeat(local_work=true)`
set (the heartbeat stamping the #324 timeline via `lifecycle.go:843-886` /
`tools.go:62-67` is the build-bearing reprieve), proved the shadow-first default
takes no destructive action, and named the long-silent no-heartbeat alive lane an
**accepted bounded residual**. This matches the v1 ledger's C2 verification gate,
which explicitly accepted "the SPEC states the bounded accepted residual and proves
the advisory default takes no destructive action" as a clearing path.

Falsifier 2's central demand — *no* armed action even on the residual — **exceeds**
what C2 authorized (option (a) allowed naming the residual accepted), so the core
challenge is `landed_and_rebutted`. Two real **cleanup** findings remain, neither
gating C2 on its own: (1) §7-T2 is **over-titled** ("must not destructively act"
while its body accepts a requeue that closes the alive owner via
`closeStalledOwningSession`, `recovery_decision_tree.go:1353-1380`); (2) the
residual's named coverage ("advisory default + operator-grant seam") is incomplete
in P0 because operator-grant is deferred to P1. → **C2 cleanup (policy),
non-binding.**

## Carry-forward regression check — ALL INTACT

Both falsifiers confirm, and I concur:

| Carry-forward | Status | Basis |
|---|---|---|
| AND-not-OR core (`sealedSilenceBreached && ToolProgressWedged`, exact #324 predicate) | **INTACT** | Falsifier 2 explicit; preserved in §2 |
| `work.heartbeat(local_work=true)` reprieve for conformant lanes | **INTACT** | Falsifier 2 explicit (`lifecycle.go:843-886`) |
| Shadow-first default (`SealedSilenceSeconds=0`, advisory; arms on opt-in) | **INTACT** | Both falsifiers; §2.4 / §6 |
| Parts 1–4 mechanism shape + single idempotent escalation | **INTACT** | §1–§4; no regression found |

## What clears the gate on `-v3`

1. **C1 (binding gate):** close the tool-axis keepalive hole — make the
   sealed-silence AND deadline-aware on the tool axis (undeclared/deadline-ignored
   `artifact.publish` and bare polling do not count as tool progress, while
   `work.heartbeat(local_work=true)` is preserved), or adopt a publish contract
   that denies junk a keepalive — so a real junk-publisher converges to the
   telomere floor with exactly one escalation; **or** explicitly re-scope §7-T1 to
   a floor-only assertion and name the non-converging junk-publisher an accepted
   residual (withdrawing the "no evasion on every surface" claim).
2. **C2 (non-binding cleanup):** retitle §7-T2 to match what it asserts, and
   reconcile the residual's named coverage with the actual P0 slice (operator-grant
   in P0, or operator-grant explicitly deferred with the armed requeue named as the
   accepted P0 risk).

This is the single allowed v2 revision cycle. A second `needs_revision` ends the
gate uncleared and routes to the operator for a fresh `-v3` run.
