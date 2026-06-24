# HOLDER (v2) — RFC 0166 P0 falsifiable SPEC: the sealed-progress silence budget

author: holder-author-001

> This is the **v2 revision** of the leading proposal the falsifiers re-attack. It
> is a surgical revision of the v1 SPEC
> (`docs/operator/artifacts/rfc-0166-design/dialogue/holder/HOLDER.md`), **not** a
> rewrite. v1 ratified the **AND-not-OR no-false-kill core** and the Part 1–4
> mechanism shape; the adjudicator
> (`docs/operator/artifacts/rfc-0166-design/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md`)
> returned `needs_revision` on two source-anchored GATE constraints, **C1** and
> **C2**, and routed them here. This revision **discharges C1 and C2** and carries
> forward, unregressed, everything v1 ratified. Every claim is anchored to named
> source the falsifiers can open and the build run can execute against.

---

## Addressing the v1 constraints (the auditable map)

| v1 constraint | status in v2 | where |
|---|---|---|
| **C1** (GATE, critical) — the detector **clock** must consume *novelty*, not events; one novelty primitive on **every** reset surface (Part-1 floor, Part-4 telomere reset, RFC 0131 confidence-gate `progressAdvanced`); decide the undeclared-in-scope publish contract; falsification test | **RESOLVED** | §1 (floor), §3 (the single primitive + publish contract), §4 (telomere reset + confidence gate), §7-T1 (test) |
| **C2** (GATE before arming, high) — correct the over-claimed no-false-kill proof; narrow to tool-fresh/in-tool/heartbeating **or** add a build-bearing reprieve; name the long-silent legitimate-think interval; falsification test | **RESOLVED** | §2 (narrowed proof + the heartbeat reprieve + the bounded residual), §7-T2 (test) |
| **Carry-forward: AND-not-OR core** (rung fires iff `sealedSilenceBreached AND ToolProgressWedged`, exact #324 predicate exported) | **INTACT, unregressed** | §2 |
| **Carry-forward: Part 1–4 mechanism shape** (derived per-session clock; AND correctness; reset-consumes-novelty; telomere ladder + one idempotent escalation) | **INTACT, unregressed** | §1–§4 |
| **Carry-forward: shadow-first P0** (`SealedSilenceSeconds` default 0/advisory; action arms only on explicit opt-in) | **INTACT, unregressed** | §2, §6 |

**Two corrections to v1 the revision also makes (accuracy, not scope change):**

- **(M1)** v1 Claim 1.3 said the new stall class "must be added to the migration-0012
  CHECK constraint set," citing #324 as precedent. That is **inaccurate**: I verified
  the only migration touching `sessions_liveness_stall_class_check` is
  `0012_mcp_activity_liveness.sql:27-37`, and its list does **not** contain
  `wedged_no_tool_progress` (the #324 class). The new class is decided in
  `recoverStuckJobs` (a recovery-tree action input), **not** by the pure `Classify()`,
  so it never flows through `refreshRunLiveness`'s `result.StallClass` write to the
  constrained `sessions.liveness_stall_class` column (`recovery.go:1237-1248`). It
  therefore needs **no** 0012 CHECK change. See §1, Claim 1.3.
- **(M2)** v1 Claim 2.4 named the tool-axis polling-gamer "a P1 seam" but did not state
  that the *novelty-publish* gamer is the **same** residual. With C1 fixed, the
  junk-publish attack and the polling-gamer collapse into one class (a lane kept
  tool-fresh by its own MCP calls), and the AND deliberately spares it. §2 states this
  honestly so the gate ratifies what is actually built.

---

## 0. The exact gap (unchanged from v1 — restated so the predicate's job is unambiguous)

A lane reaches `recoverStuckJobs` (`go/pkg/mutations/recovery_decision_tree.go:704`)
and is classified by the **pure** `sessionliveness.Classify` (`liveness.go:475`).
The live #576 lane traces through `Classify` to the `default` branch of the recovery
switch (`recovery_decision_tree.go:1105`), where, finding `!confirmedDead()` (the
agent is alive), it `continue`s (`:1118-1120`) — **no action, forever**:

- The supervisor renews the work-lease from **PTY output** (`lease.heartbeat`, source
  `supervisor_pty_progress`), so `ActiveLeaseHeartbeatAt` stays fresh. It is in both the
  lease-fresh base and `protocolActivityFresh` (`liveness.go:827-848`), so the lane
  reads `working_protocol` right after each heartbeat and `quiet` between — never
  `StallLeaseHeartbeat`, never `ProtocolStalled`.
- The #324 wedge (`StallToolProgress`, `liveness.go:70`) is gated on
  `working.Protocol == ProtocolWorkingLocal` (`:603,610`), which requires PTY fresh
  **and** `!protocolActivityFresh`. A lane with any fresh protocol/lease-heartbeat
  signal reads `working_protocol`, so the `working_local`-gated #324 rung **cannot fire
  for it**.

So the target is precisely: **a lease holder kept non-stalled only by
forgeable/raw liveness (PTY-driven lease heartbeats, raw PTY frames), whose
forgery-resistant sealed timeline AND tool-call timeline are both stale.** The lane
the new rung must catch; and the single-final-seal genuinely-working lane (real tool
calls toward one terminal `work.complete`) is the lane it must never touch.

---

## 1. PART 1 — the clock (derived detector), now reading NOVELTY, not events  *(C1 discharge, half 1)*

**Claim 1.1 (derived, not stored, restart-reproducible — UNCHANGED structure).** The
deadline is recomputed every sweep as `now − floor`. No `deadline_at` / `max_duration`
column is added; the floor is composed from durable PostgreSQL rows so a process
restart reproduces the identical clock.

**Claim 1.2 (the floor uses the CURRENT lease, not the job's first start — UNCHANGED).**
For a requeued lane the floor anchors on the **current active lease's** `acquired_at`
(`leases` DDL, `0005_repo_local_workflow_state.sql`), so a fresh requeued session
starts with a full budget; the cross-session telomere (Part 4) bounds total fresh
sessions. This preserves the #145 CASE-2 guarantee against the original-`started_at`
mid-work kill.

**Claim 1.3 (CHANGED — the floor's progress input is the NOVELTY primitive, not raw
`jobSealedProgressAt`).** v1's floor was
`GREATEST(jobSealedProgressAt, currentLeaseAcquiredAt, started_at)` where
`jobSealedProgressAt = GREATEST(max(artifacts.created_at), max(verdicts.created_at))`
(`recovery_decision_tree.go:446-465`) — a **raw, event-gated** clock. Falsifier 2
showed (decisively, unrebutted) that a hostile lane source-publishing a fresh
**undeclared in-scope** artifact (distinct `content_sha256`, fresh `logical_name`)
each budget−ε advances `max(artifacts.created_at)`, so the floor stays fresh,
`sealedSilenceBreached` is never true, and the rung never arms — durable across a
restart. v2 replaces the progress input with the single novelty primitive of §3:

```
floor = GREATEST( novelSealedProgressAt(job),   -- §3: NOVELTY, not raw created_at
                  currentActiveLeaseAcquiredAt,  -- per-session reset on requeue (Claim 1.2)
                  jobs.started_at )              -- floor when nothing has sealed yet
sealedSilenceBreached := SealedSilenceSeconds > 0 && missed(floor, SealedSilenceSeconds, now)
```

`novelSealedProgressAt` is the **timestamp of the last strict novelty advance** (§3),
not the timestamp of the last *row*. An undeclared / identical-content / replayed row
does **not** move it, so junk no longer rescues the clock.

- *Evidence the substitution does not regress legitimate behavior:* the
  `currentActiveLeaseAcquiredAt` term is retained, so a fresh requeued session still
  resets its per-session clock (Claim 1.2 intact); and a genuine declared seal still
  advances `novelSealedProgressAt`. The only behavioral delta is that an **undeclared
  junk row no longer advances the floor** — exactly C1's intent.
- *Refutation test:* set the floor inputs with a stream of undeclared distinct-content
  rows landing each budget−ε; assert the floor equals `novelSealedProgressAt` (stale)
  and `sealedSilenceBreached` becomes true on schedule, identically before and after a
  daemon restart. If a junk row moves the floor, Claim 1.3 is false. (Full §7-T1.)

**Claim 1.4 (new stall class, evaluated where the data lives; no CHECK change — CORRECTED
per M1).** A new constant `StallSealedProgress = "wedged_no_sealed_progress"` sits next
to `StallToolProgress` (`liveness.go:70`). It is **not** decidable by the pure
`Classify` (which never SELECTs per-job `artifacts`/`verdicts`); it is evaluated **only**
in `recoverStuckJobs`, which already holds the tx, already SELECTs the job/lease/session
rows, and already calls `jobSealedProgressAt`. It is inserted in the `default` branch
(`recovery_decision_tree.go:1105`) **before** the `if !confirmedDead() { continue }` —
the exact point the §0 lane escapes today. Like `StallToolProgress` it maps to a
recovery **action** (Protocol-stalled CASE-2 close-and-requeue), and it is **not** a
`Classify()` output, so it never reaches `refreshRunLiveness`'s write of
`result.StallClass` to the constrained `sessions.liveness_stall_class` column
(`recovery.go:1237-1248`); the 0012 CHECK (`0012_mcp_activity_liveness.sql:27-37`),
which does not list `wedged_no_tool_progress` either, therefore needs no change.
- *Build obligation (falsifiable):* if any persistence path is found that would write
  `wedged_no_sealed_progress` to `sessions.liveness_stall_class`, the same migration
  MUST extend `sessions_liveness_stall_class_check` to include it — follow whatever the
  `StallToolProgress` precedent does. The build verifies the column is never written the
  new value (a CHECK-violation test).

---

## 2. PART 2 — AND, not OR: the structure RATIFIED, the proof CORRECTED  *(C2 discharge)*

**Claim 2.1 (the rung fires only when BOTH axes are stale — RATIFIED, UNCHANGED).**

```
toolWedged := sessionliveness.ToolProgressWedged(activity, policy, now)  // the EXACT #324 predicate
fire       := sealedSilenceBreached && toolWedged
```

`ToolProgressWedged` is the existing `toolProgressWedged` (`liveness.go:793-805`)
promoted to **exported** so the AND's tool half is bit-identical to #324 and cannot
drift. It ages against `toolProgressBase = latestTime(LastToolCallStartedAt,
LastToolCallFinishedAt)` (`liveness.go:772-778`) — the tool-call timeline only — and
inherits three exclusions for free: zero-tool-history (`base == nil → false`), in-tool
(`if inTool { return false }`, the #145 long-foreground-command reprieve), and
disabled-when-`ToolProgressSeconds<=0`. The AND **strictly narrows** #324's firing set,
so it cannot kill anything #324 already spares. **I re-ratify the AND as the
no-false-kill core; this section repairs only the *proof*, per C2.**

**Claim 2.2 (CORRECTED — the no-false-kill property, stated as what is actually
proven).** I withdraw v1's unqualified "alive-and-legitimately-working ⇒ never killed."
The **proven** property is:

> **No false-kill for any lane whose tool-call timeline is fresh within
> `ToolProgressSeconds`** — i.e. a lane that, inside the window, is (i) currently inside
> a tool call (`working_tool`), (ii) issues any tool call, **or** (iii) emits a
> `work.heartbeat` (which, like every MCP `tools/call`, stamps the tool-call timeline —
> see Claim 2.3). For every such lane `toolWedged == false`, so the AND is false and the
> rung cannot fire, regardless of the sealed clock.

This is a proof, not a calibration argument: it follows directly from the exported #324
predicate's definition.

**Claim 2.3 (the build-bearing reprieve — `work.heartbeat` keeps a long-think lane
tool-fresh).** Every MCP `tools/call` stamps the tool-call boundary:
`mcp.Service.ToolsCall` records `LastToolCallStartedAt` before dispatch and
`LastToolCallFinishedAt` after, for **any** method that carries `repository_id` +
`session_id` (`go/pkg/mcp/tools.go:62-67`). `work.heartbeat` is such a call. Therefore
a lane in a long legitimate planning/model-generation interval that honors the
documented **local-work heartbeat contract** (`work.heartbeat` with `local_work=true`
within `ToolProgressSeconds`, the cadence the lane prompt already mandates) refreshes
`toolProgressBase` → `toolWedged == false` → **provably spared**. The reprieve is
build-bearing because it is the *existing* heartbeat path stamping the *existing* #324
timeline — no new agent surface, no new RPC. The PTY-driven lease heartbeat
(`supervisor_pty_progress`) does **not** make a `tools/call`, so it does *not* stamp the
timeline — which is exactly why the §0 lane (PTY-only) is correctly catchable while a
heartbeating lane is not.

**Claim 2.4 (the accepted bounded residual — named, not hidden).** The residual the AND
does **not** protect is: a lane that, for longer than `ToolProgressSeconds`, makes **no
tool call and emits no local-work heartbeat**, is **not** inside a tool call, is kept
warm **only** by PTY, **and** has sealed no novelty past `SealedSilenceSeconds` — yet is
genuinely working (e.g. a non-conformant harness mid-generation that cannot self-emit a
heartbeat). At the forgery-resistant grain this lane is **indistinguishable** from a
true wedge: every signal the daemon can trust is stale. P0 accepts it as a **bounded**
residual, bounded by four independent containments:

1. **Shadow-first default (the blast-radius cap):** `SealedSilenceSeconds` defaults to
   **0 = disabled** (the `ToolProgressSeconds` convention, `liveness.go:320`). With the
   default, `sealedSilenceBreached` is *always false* and the rung takes **no
   destructive action at all** — it is an advisory `doctor`/`dashboard` signal. Landing
   to `main` cannot false-kill anything.
2. **Arming is explicit opt-in + calibrated:** the action arms only when an operator
   sets `SealedSilenceSeconds > 0`, chosen from the shadow-bake corpus to exceed the
   longest observed *legitimate* sealed-silence, so the residual window is small by
   construction.
3. **The action is a fresh-session requeue, not a kill (severity reduction):** the first
   breach reuses `transfer_requeue` / `requeueJobSameAttempt` (§4) — it closes the
   stalled owner and starts a fresh session on the **same attempt**; it does not
   `kill -9` the host process or `run cancel`. Any durable in-scope authored artifact is
   recoverable via the existing `tryFinalizeUnsealedFromDurableArtifact` path
   (`recovery_decision_tree.go:1751`) / `recovery complete-stalled`. So even when the
   residual fires, the loss is *unsealed in-flight reasoning*, strictly smaller than the
   full #145 CASE-2 durable-artifact loss.
4. **Operator-grant seam (P1, §6):** a known-slow job is whitelisted by a
   daemon-recorded grant — a lane cannot self-grant.

- *Refutation test (Falsifier 2's target):* a single-final-seal lane, last tool call
  older than `ToolProgressSeconds`, supervised process alive and working. Assert: (a) at
  the default `SealedSilenceSeconds=0` the rung takes **no** destructive action (advisory
  only); (b) a lane emitting `work.heartbeat(local_work=true)` within
  `ToolProgressSeconds` is spared even when armed (`toolWedged==false`); (c) the SPEC's
  accepted-residual statement matches the armed behavior. Full §7-T2.

**Claim 2.5 (ratify AND over OR / sealed-only — UNCHANGED).** OR / sealed-only reduce to
the banned plain wall-clock cap (kill a lane that has not sealed for N seconds), which
false-kills the single-final-seal lane mid-work. The AND is the minimal predicate that
closes §0 without that regression.

---

## 3. PART 3 — the ONE novelty primitive (used by every reset surface)  *(C1 discharge, core)*

**Claim 3.1 (a single, novelty-aware progress primitive — this is the C1 core).** v2
defines exactly one primitive and uses it for the Part-1 floor (§1), the Part-4 telomere
reset (§4), and the RFC 0131 confidence-gate `progressAdvanced` (§4) — closing the v1
defect that novelty gated only the telomere *counter* while three other surfaces read
raw `created_at`.

The **novelty position** is the strict-increase cursor of v1 Claim 3.1/3.3, hardened to
declared/milestone artifacts and scoped across the whole job (all attempts):

```
pos(job) = ( D = count(distinct content_sha256 of the job's DECLARED/milestone artifacts),
             V = count(sealed verdicts for the job),
             M = highest satisfied REQUIRED expected_artifacts milestone index )
```

Novelty advanced iff `pos` strictly increases lexicographically (any dimension).

The **novelty timestamp** — the value every reset surface reads — is a deterministic
query over durable rows, the novelty-aware analog of `jobSealedProgressAt`:

```sql
novelSealedProgressAt(job) = GREATEST(
  -- newest genuinely-new declared content: first appearance of each distinct hash,
  -- scoped to logical_names declared in the job's expected_artifacts (resolved per attempt)
  ( SELECT max(first_seen_at) FROM (
      SELECT min(created_at) AS first_seen_at
        FROM striatumd.artifacts
       WHERE repository_id = $1 AND job_id = $2
         AND logical_name = ANY ($declared_logical_names)
       GROUP BY content_sha256 ) d ),
  -- verdicts are intrinsically un-gameable (Claim 3.4), counted raw
  ( SELECT max(created_at) FROM striatumd.verdicts
     WHERE repository_id = $1 AND job_id = $2 )
)
```

- It is a **pure function of durable rows** (`artifacts`, `verdicts`,
  `jobs.expected_artifacts_json`), so it is **restart-stable**: recomputation after a
  restart yields the identical value (the C1 "deterministic restart-stable
  recomputation").
- It is **monotone non-decreasing** because `artifacts`/`verdicts` are append-only and a
  `(logical_name, attempt)` row is immutable (`0018_artifact_attempt_scope.sql`, `0005`
  triggers). A new distinct declared hash, or a new verdict, is the *only* way it
  advances — and that advance is, by definition, genuine sealed novelty.
- **`novelSealedProgressAt` strictly increasing ⇔ `pos` strictly advancing** (M is
  carried by the declared-artifact term; V by the verdict term), so the single timestamp
  is itself the cursor: a surface can detect "novelty advanced since X" by comparing
  `novelSealedProgressAt` against a stored X.

**Claim 3.2 (persisted memo + the deterministic invariant — the C1 "persisted OR
recompute," satisfied as BOTH).** A new column
`last_novel_sealed_progress_at timestamptz` is added to `striatumd.job_recovery_state`
via a migration in the **0035 `ADD COLUMN IF NOT EXISTS` degrade-safe style**
(`0035_job_recovery_confidence_gate.sql`). It is written transactionally — in the same
upsert that updates the cursor — to `= novelSealedProgressAt(job)` (the deterministic
value, **never** `now()`), so the persisted memo *equals* the recomputation by
construction. The Part-1 floor and the confidence gate read the column (fast path); a
deployment behind the migration, or a null column, falls back to recomputing
`novelSealedProgressAt` (degrade-safe: if neither is available the rung simply does not
arm, matching the 131-C / bundle-0012 degrade pattern). The C1 note that "the three
count columns alone cannot recover that timestamp" is honored: we persist the
**timestamp**, defined to equal the deterministic query.
- *Doctor invariant (falsifiable):* `last_novel_sealed_progress_at == novelSealedProgressAt(job)`
  for every live recovery row; a mismatch is a `doctor` finding.

**Claim 3.3 (the publish contract for undeclared in-scope rows — DECISION:
allowed-but-ignored-for-deadline).** I verified that `expected_artifacts` are
**required-presence assertions, not an allowlist** (`artifact_source_publish.go:14-16`),
so a repo-write lane legitimately source-publishes *undeclared* in-scope files (tests,
migrations, `__init__.py`) as distinct `content_sha256` rows; the "covered" set at
`work.complete` is `declared expected_artifacts ∪ source-published`
(`artifact_source_publish.go:206`). The decision:

> **Undeclared in-scope artifact rows remain fully publishable and durable provenance,
> but they do NOT participate in the sealed-silence deadline:** they are excluded from
> `novelSealedProgressAt` (the `logical_name = ANY($declared_logical_names)` filter) and
> from the novelty cursor. They are *allowed-but-ignored-for-deadline*, never
> *forbidden*.

Rationale for choosing this over "expected-artifact-only for deadline-participating
jobs": (1) the alternative changes the **publish contract** for every job and would
regress the legitimate undeclared-provenance feature (`artifact_source_publish.go`),
widening blast radius far beyond the recovery subsystem; (2) declared-scoping the
*primitive* already neutralizes the gaming surface without touching the publish path —
the change stays inside `recoverStuckJobs` and one migration; (3) it preserves the
local-first boundary (no new write-path policy). A lane whose *only* output is undeclared
provenance gets no deadline credit — but if it is alive it is tool-fresh (AND-spared,
§2), and if it is tool-silent and producing nothing declared it is exactly the §0 lane
the deadline targets.

**Claim 3.4 (identical-replay and verdict/milestone intrinsics — UNCHANGED, carried
forward).** `GROUP BY content_sha256` with `min(created_at)` means a re-published
identical hash introduces no new group and cannot move `first_seen_at`, so the
identical-replay vector is closed (`artifacts` DDL,
`0005_repo_local_workflow_state.sql`). `verdicts` rows are daemon-written only for
verdict-capable jobs (structurally 0 for non-reviewers; producing the required verdict
*is* completing a reviewer job); the milestone index is monotone over **required**
artifacts and cannot decrease.

---

## 4. PART 4 — the action: telomere ladder, resetting on the SAME primitive  *(C1 discharge, half 2)*

**Claim 4.1 (first breach auto-requeues one fresh session — UNCHANGED).** The new rung
sets `action = "transfer_requeue"` (`closeStalledOwner = true`, `forceExpire = true`) in
the `default` branch and lands in the existing `requeueJobSameAttempt` +
`closeStalledOwningSession` path (`recovery_decision_tree.go:1080,1353`) that CASE-2
already uses. No new requeue mechanism; convergence is inherited.

**Claim 4.2 (CHANGED — the telomere counter resets on the §3 primitive, not on raw
events).** `job_recovery_state.requeue_count` (or, for the unsealed shape, the smaller
`unsealedRequeueBudget`, `recovery_decision_tree.go:1160,295`) resets to 0 **only** when
`novelSealedProgressAt` strictly advances past the value stored at the last reset — i.e.
on genuine sealed novelty, never on a junk/identical/undeclared row. A lane burning
fresh sessions without sealing novelty never resets → `requeue_count` climbs
monotonically toward `policy.maxRequeues` (`defaultMaxRequeues = 2`,
`recovery_decision_tree.go:76`) / `unsealedRequeueBudget` → the telomere floor, the
`current >= limit` branch (`:1168`).

**Claim 4.3 (CHANGED — the RFC 0131 confidence-gate `progressAdvanced` uses the §3
primitive).** Today the gate computes
`progressAdvanced := (hasSealed && sealedAt.After(windowStart)) || cohortHasFresherLiveness(...)`
where `sealedAt` is **raw** `jobSealedProgressAt` (`recovery_decision_tree.go:1201-1207`)
— the second event-gated reset Falsifier 2 named, where a junk row resets
`consecutive_silent_sweeps`. v2 replaces the sealed term with the novelty primitive:

```
progressAdvanced := ( hasNovel && novelSealedProgressAt(job).After(windowStart) )
                    || cohortHasFresherLiveness(rows, jobID, windowStart, now)
```

The cohort-liveness term (`recovery_decision_tree.go:481`) is unchanged (it is
cross-lane corroboration, not a per-job forgeable signal). Now a junk row neither resets
the silent-sweep counter nor defers escalation. `windowStartForGate`
(`recovery_decision_tree.go:536`), `applyConfidenceGate` (`:594`),
`writeConfidenceGateState` (`:661`), and the topology-adaptive cap/threshold (`:1219`)
are otherwise untouched.

**Claim 4.4 (escalation is bounded, storm-free, with a copy-pasteable verb —
UNCHANGED).** At the floor, the gate (for the §0 PTY lane: `!sessionDead`,
`!confirmedDead()`, basis `deadline_elapsed_only` → `gateApplies`,
`recovery_decision_tree.go:1194`) requires `escalateThreshold` (≥2) consecutive silent
sweeps or the Layer-4 cap before `markRecoveryEscalation` flags `escalation_pending`
**once** (`escalated_at` set via `COALESCE`, idempotent, `:418-434`). The single
recovery verb is `recovery complete-stalled` when a durable artifact reconstructs
(`tryFinalizeUnsealedFromDurableArtifact`, `:1751`), else `recovery requeue-stale
--override`.
- *Convergence proof:* `requeue_count` is monotone non-decreasing absent a §3 novelty
  reset, bounded by `maxRequeues`; the gate's silent-sweep cap is finite
  (`(maxRequeues*2)+3` floor); `escalation_pending` idempotency forbids a storm. A
  deterministically-flailing lane escalates in bounded sweeps with **exactly one**
  operator escalation.

---

## 5. Hard-core proof obligations (the three the gate gates on)

1. **No false-kill — STATED AS PROVEN (C2).** Proven for every tool-timeline-fresh lane
   by Claim 2.1's AND + Claim 2.2 + the Claim 2.3 heartbeat reprieve: inside a tool call
   → `working_tool`, excluded; any tool call or `work.heartbeat` within
   `ToolProgressSeconds` → `toolWedged == false`; per-session clock resets on requeue
   (Claim 1.2). The **only** residual (Claim 2.4 — tool-silent, no-heartbeat,
   PTY-only-warm, alive) is named, bounded by shadow-first default + opt-in calibration +
   requeue-not-kill + operator-grant, and tested (§7-T2). No unqualified claim remains.
2. **Un-gameable (fake progress ⇒ no reset, no evasion) — RESTORED ON EVERY SURFACE
   (C1).** Proven by Claim 3.1–3.4: the single `novelSealedProgressAt` primitive feeds
   the Part-1 floor (§1), the Part-4 telomere reset (§4.2), and the confidence-gate
   `progressAdvanced` (§4.3); undeclared/identical/replayed rows advance none of them.
   The composed residual — a lane that keeps the **tool axis** fresh by its own MCP calls
   (the junk-*publisher* and the polling-gamer are the **same** class, M2) — evades the
   *deadline* (AND-spared) but cannot reset the *telomere* (it seals no novelty); it is
   named as the P1 tool-axis-novelty seam (§6), not silently dropped.
3. **Restart + requeue consistent.** Proven by Claim 1.1 (derived inputs) + Claim 3.2
   (the persisted `last_novel_sealed_progress_at` equals its deterministic recomputation;
   a restart mid-ladder resumes the same cursor and counters, no spurious reset, no
   phantom advance) + Claim 1.2 (current-lease per-session reset). Tested across a
   restart in §7-T1.

---

## 6. P0 slice boundary & named later seams

**P0 ships (minimum that closes #576 safely):**
- The derived clock with the **novelty floor** (§1) + the `wedged_no_sealed_progress`
  rung inserted in `recoverStuckJobs`' `default` branch (Claim 1.4); no 0012 CHECK
  change (M1).
- The AND gate reusing the exported `ToolProgressWedged` (§2).
- The **one novelty primitive** `novelSealedProgressAt` (§3) + a degrade-safe
  `ADD COLUMN IF NOT EXISTS last_novel_sealed_progress_at timestamptz` (and the cursor
  count columns `sealed_progress_distinct_artifacts/_verdicts/_milestone int` if the
  build prefers explicit counts over the timestamp-as-cursor) on
  `job_recovery_state`, in the 0035 style.
- The novelty primitive wired into all three reset surfaces: floor (§1), telomere reset
  (§4.2), confidence-gate `progressAdvanced` (§4.3).
- First-breach auto-requeue-one + telomere escalation through the existing budgeted path
  (§4).
- **Shadow-first arming:** `SealedSilenceSeconds` a new `sessionliveness.Policy` field
  defaulting to **0 = disabled**; advisory `doctor`/`dashboard` signal first; the
  escalating action arms only on explicit opt-in.

**Named later seams (out of P0):**
- **Tool-axis novelty** — the Claim 2.4 / M2 residual: require tool-call *novelty*
  (distinct tool-call signatures / forward progress) rather than recency, so a lane kept
  tool-fresh by junk-publishing or polling `await_packet` is also caught without
  false-killing the genuine single-final-seal lane.
- **Threshold calibration** — per-`job_type` vs global `SealedSilenceSeconds` from a
  shadow-bake corpus.
- **Operator known-slow grant** (RFC Part 5) — `recovery grant-silence <run> <job>
  --seconds N` writing a forgery-resistant daemon-recorded floor
  (`floor = GREATEST(novelSealedProgressAt, currentLeaseAcquiredAt, started_at, max(grant))`);
  a lane cannot self-grant.
- **Run-scoped sibling-fairness preemption** (RFC appendix / #579 cousin).

**Local-first boundary preserved.** Every input is daemon-owned PostgreSQL state
(`artifacts`, `verdicts`, `leases`, `jobs`, `job_recovery_state`); no hosted service,
telemetry, or external persistence is introduced.

---

## 7. The two mandated falsification tests (specified for the build)

### §7-T1 — C1: junk publishing must not move the floor, the cursor, or the sweeps; the lane must converge with exactly one escalation (across a restart)

**Setup.** Arm the rung (`SealedSilenceSeconds = S > 0`). A job whose owning session is
kept warm only by PTY (`ActiveLeaseHeartbeatAt` fresh; tool-call timeline stale past
`ToolProgressSeconds` ⇒ `toolWedged == true`). Before every budget expiry (each `S−ε`)
an **undeclared** in-scope artifact row lands (allowed kind, fresh `logical_name`
`junk_N`, distinct `content_sha256`).

**Assert.** (a) `novelSealedProgressAt` does not advance (the junk rows are excluded by
the declared-`logical_name` filter), so the floor does not move and `sealedSilenceBreached`
becomes true on schedule; (b) the §3 cursor does not advance; (c) the telomere
`requeue_count` and the confidence gate's `consecutive_silent_sweeps` keep advancing
(no junk-driven reset on either surface); (d) the lane reaches the telomere floor and
escalates **exactly once** (`escalation_pending` idempotent), with the correct verb
(`complete-stalled` iff a durable required artifact reconstructs, else `requeue-stale
--override`). (e) **Restart mid-ladder**: drop in-memory state, recompute; assert the
persisted `last_novel_sealed_progress_at` equals the recomputed `novelSealedProgressAt`
and the counters resume unchanged (no spurious reset, no phantom advance).

**Refutes if:** any junk row moves the floor / cursor / silent-sweeps; or the lane never
arms; or it escalates zero or more-than-once; or the restart perturbs the cursor.

### §7-T2 — C2: an armed rung must not destructively act on an alive, legitimately-working tool-silent single-final-seal lane

**Setup.** A single-final-seal lane (zero intermediate seals; one terminal
`work.complete` is its only deliverable) whose last tool call is **older** than
`ToolProgressSeconds` but whose supervised process is **alive and working**.

**Assert.** (a) **Default** (`SealedSilenceSeconds = 0`): the rung takes **no**
destructive action — advisory `doctor`/`dashboard` only (the shadow-first blast-radius
cap); (b) **conformant lane**: a lane emitting `work.heartbeat(local_work=true)` within
`ToolProgressSeconds` is spared even when armed, because the heartbeat stamps the
tool-call timeline (`tools.go:62-67`) ⇒ `toolWedged == false` ⇒ AND false (the
build-bearing reprieve, Claim 2.3); (c) **accepted residual stated**: for the
non-heartbeating tool-silent alive lane when armed, the action is a fresh-session
requeue (not a kill / not `run cancel`), with durable-artifact recovery available
(`tryFinalizeUnsealedFromDurableArtifact`), matching the §2.4 bounded-residual
statement.

**Refutes if:** the default takes any destructive action; or a heartbeating lane is
acted on; or the armed behavior on the residual lane is a hard kill / run-cancel rather
than the stated fresh-session requeue.

---

## 8. Single-sentence claim for the falsifiers

*Adding a `wedged_no_sealed_progress` rung to `recoverStuckJobs`' `default` branch — a
derived per-session clock floored on **`novelSealedProgressAt`** (a single,
declared-scoped, content-deduped, restart-stable novelty primitive that also drives the
telomere reset and the RFC 0131 confidence-gate `progressAdvanced`), gated by an AND
with the exact exported #324 `ToolProgressWedged` predicate — closes the
alive-but-never-completing gap in bounded, restart-consistent sweeps with **exactly one**
operator escalation, cannot be reset by undeclared/identical/replayed rows on any
surface (C1), and **provably** false-kills no tool-fresh, in-tool, or
`work.heartbeat`-ing lane (C2), the only residual being the named, shadow-default-and-
opt-in-bounded tool-silent-no-heartbeat-yet-alive lane, requeued-not-killed when armed.*
