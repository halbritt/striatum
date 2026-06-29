# RFC 0103: Self-Hosting Production Hardening — close the residual lane / lifecycle / contract / operator tail between *proven once* and *production-grade* self-hosting

Status: accepted
Date: 2026-06-02
author: proposer-claude-opus-4-8-001

## Review reconciliation (2026-06-02 RFC 0103 review dogfood)

Reviewed live **through the runner** by a multi-lane panel
(`docs/dogfoods/rfc-0103-review/`, run `run_05c653068a094c25ca8ce2da0b190a33`): a
claude presenter published a review brief; a **codex** (threat_model) and a
**claude** (devil's-advocate) reviewer each published a finding and submitted a
verdict — **both `needs_revision`**, through the production handlers. Both
confirmed the **partition is exact** (W1=3, W2=4, W3–W7=2 each = 17, each issue
once) and the slice-RFC ownership coherent; both scoped their objection entirely
to the **acceptance framework**. Reconciled and accepted (the panel pre-authorized
acceptance "once R1/R2 + the F-notes are addressed"):

- **R1** (acceptance not uniformly regression-gated) → added the **[hermetic
  gate] / [live-corroborated] / [qualitative]** rigor taxonomy; W1 gains a
  hermetic **cross-session-token-rejection** test (carrying the token ≠ the daemon
  refusing the impersonation); W3 splits the **real systemd socket-recreation**
  gate from the in-process chaos **approximation**; W7's cooperative outcome is
  relabeled qualitative with an **audited-escape-log** proxy.
- **R2** (umbrella was itself "proven once") → acceptance is now **two tiers** —
  the multi-lane single-fault dogfood is the **floor/precondition**, and
  **production-grade** is earned only by a **fault-class matrix** (W1 isolation /
  W3 churn / W4 reviewer-replacement) across both seats.
- **F1** ordering is **priority-by-risk, not dependency**; the umbrella's critical
  path is W2/W3/W4. **F2** W2 (agy) is the **most deferrable**, not a blocker.
  **F3** #138 flagged as a **new primitive** that may spin to an RFC 0097/0099
  follow-up. **F4** added the **selection criterion** for the 17 (and why #101/#65
  sit outside the count). **F5** the two seats are named as "two instances of the
  one supported shape."

The review findings are preserved at
`docs/dogfoods/rfc-0103-review/artifacts/review/{codex,claude}/REVIEW.md`.

## Context

- **RFC 0101** made autonomous execution *robust at the daemon level* (honest
  liveness, in-daemon recovery, loud escalation, a fault-injection chaos suite),
  and **RFC 0097** self-hosting **acceptance #5 was proven live on 2026-06-01**
  (`8e9ac86b`): a minimal single-`claude`-lane document dogfood ran end-to-end
  through the production handlers, hands-off, and the run auto-finalized to
  `completed` with the artifact `content_sha256` matching the on-disk file.
- That proof, plus the 2026-06-01/02 issue burn-down, **closed the tractable
  mechanical tail** — 14 issues, e.g. the `list workflows` schema drift
  ([#142](https://github.com/halbritt/striatum/issues/142)), the
  register-session recovery deadlock ([#133](https://github.com/halbritt/striatum/issues/133)),
  and the review-verdict semantics cluster
  ([#127](https://github.com/halbritt/striatum/issues/127)/[#132](https://github.com/halbritt/striatum/issues/132)/[#140](https://github.com/halbritt/striatum/issues/140),
  D158). It also **corrected three over-closes** (the RFC 0101 Phase 0a deadlock
  commit had over-attributed [#131](https://github.com/halbritt/striatum/issues/131)/[#134](https://github.com/halbritt/striatum/issues/134)/[#133](https://github.com/halbritt/striatum/issues/133)).
- What remains is **not mechanical**. The minimal proof used the safest possible
  shape (one `claude` lane, one document job, no review panel, no parallelism, no
  daemon restart). The **17 open issues are the gap between that proof and a
  *production-grade* self-host** — a runner that can carry a real **multi-lane,
  multi-turn, review-gated build of its own fixes** without an operator at the
  keyboard. **Selection criterion (review F4):** the set is exactly the issues that
  are (a) **open**, (b) **not mechanical** (the tractable mechanical tail was
  closed in the 2026-06-01/02 burn-down), and (c) **block production-grade
  self-hosting**. Issues cited inside the workstreams but *outside* this count
  (e.g. **#101** claude lane-env, **#65** panel-owned interrogation window) are
  excluded because they already **landed or are owned by their slice-RFC** — they
  are referenced as context, not open work. This makes the partition's
  *completeness* checkable, not just its internal cleanliness. They cluster into
  seven workstreams, each extending a slice-RFC, but **no existing RFC owns
  "production-grade self-hosting" as a property**:
  - **Lane is not a sandbox.** A supervised lane still uses the shared override
    capability token rather than its session-bound one, writes a bearer token
    into the target worktree, and can reach Postgres directly as the daemon's OS
    user. ([#135](https://github.com/halbritt/striatum/issues/135)/[#70](https://github.com/halbritt/striatum/issues/70)/[#87](https://github.com/halbritt/striatum/issues/87), RFC 0096 V2.)
  - **One adapter can't hold a seat.** `agy` closes its session after a single
    turn, stalls in MCP discovery, and blocks on a feedback prompt — so a 3-lane
    panel collapses to 2 on a fresh repo.
    ([#95](https://github.com/halbritt/striatum/issues/95)/[#85](https://github.com/halbritt/striatum/issues/85)/[#76](https://github.com/halbritt/striatum/issues/76)/[#139](https://github.com/halbritt/striatum/issues/139).)
  - **The lane does not survive transport/daemon churn.** A daemon restart
    (`Restart=on-failure`) orphans lane helpers with no reconnect; a codex lane
    reports readiness instead of `work.ack` and stalls.
    ([#141](https://github.com/halbritt/striatum/issues/141)/[#125](https://github.com/halbritt/striatum/issues/125).)
  - **The interrogation window dies with one reviewer.** A retry/replacement
    reviewer cannot interrogate after the panel target left the live window.
    ([#131](https://github.com/halbritt/striatum/issues/131)/[#134](https://github.com/halbritt/striatum/issues/134), RFC 0095.)
  - **Contracts/orchestration/operator legibility** still convert good work into
    friction or leave the operator blind:
    [#126](https://github.com/halbritt/striatum/issues/126)/[#128](https://github.com/halbritt/striatum/issues/128) (RFC 0100 P2),
    [#115](https://github.com/halbritt/striatum/issues/115)/[#138](https://github.com/halbritt/striatum/issues/138) (RFC 0097),
    [#92](https://github.com/halbritt/striatum/issues/92)/[#112](https://github.com/halbritt/striatum/issues/112) (RFC 0099/0102).
- **Vehicle is now unlocked.** Because RFC 0097 self-hosting is proven, these
  fixes can be **developed by scaffolded dogfoods driven *through* the runner**
  (operator scaffolds, never implements role artifacts) rather than only by
  bootstrap-via-subagent. This RFC is the durable scaffolding for that — it
  replaces the ephemeral operator plan with a reviewable, dogfoodable spine.
- This RFC adds **no new persistence, hosted service, telemetry, or transcript
  capture**. It is built within **D094/D005** (the daemon is the only writer;
  lanes are daemon-mediated), **D028/D151** (curated artifacts + operator-local
  scratch, not raw transcripts, are workflow state), and **D026/D080/D149**
  (attestation governs byline provenance).

## Proposal — seven workstreams

Each workstream names its owning slice-RFC, the issues it closes, **what already
landed**, the **remaining work**, and a **per-workstream acceptance**. The
acceptance rigor is **not uniform** across workstreams (review R1), and the RFC
must not pretend it is. Each acceptance is tagged with its strongest available
gate:

- **[hermetic gate]** — a deterministic test (PG-gated or fixture) or
  chaos/conformance assertion that ships *without* a live dogfood: W1's
  cross-session-token-rejection test, W4's replacement-reviewer PG test, W5's
  `artifact describe`/`lint` checks, W6's stale-snapshot + shared-gate tests.
- **[live-corroborated]** — a hermetic gate proves the mechanism, with a live
  observation as *corroboration only* (never the primary gate — that is the
  one-shot evidence this RFC criticizes RFC 0097 for): W1's "lane S cannot act as
  S′ in a live run", W2's installed-CLI conformance fixture, W3's reconnect.
- **[qualitative]** — a cooperative/operator outcome that is **not** mechanically
  gateable and is labeled honestly as best-effort, with the nearest *audited*
  proxy named: W7 (operator restraint is unobservable; the gateable proxy is
  "zero out-of-band control-plane escapes recorded in the audit log").

### W1 — The supervised lane becomes a real sandbox (RFC 0096 V2)

- **Issues:** [#135](https://github.com/halbritt/striatum/issues/135) (session-bound capability token reaches the lane), [#70](https://github.com/halbritt/striatum/issues/70) (bearer token out of the target worktree), [#87](https://github.com/halbritt/striatum/issues/87) (lane cannot reach Postgres directly / bypass the artifact API).
- **Already landed:** per-session capability-token **minting + enforcement**
  (v2.9.1, #135 mechanism); the lane env is an **explicit allowlist** that drops
  every `*DSN*`/`*POSTGRES*`/`PG*`/`DATABASE_URL` var (`supervisedEnvAllowlistKeys`,
  the #87 env-leak half).
- **Remaining:** (a) **#135** — wire the session-bound token into the lane env so
  `STRIATUM_MCP_TOKEN` is the lane's own minted token, not the shared override;
  the per-session check then *bites in live runs* (today lanes pass the shared
  token, treated as honest operator-override — the spoof is closed in mechanism
  but not enforced end-to-end). (b) **#70** — generate the agy `.gemini/settings.json`
  outside the worktree (or guarantee teardown removal), so a token never lands in
  durable provenance. (c) **#87** — deny the lane direct Postgres: a PG-less lane
  OS user / dropped peer-auth path so the lane's only control-plane is the MCP
  surface, not a libpq socket as the daemon's user.
- **Acceptance:**
  - **[hermetic gate]** a daemon-side test that a request **bearing session S's
    minted token but presented as session S′ is rejected** on receipt (review R1:
    carrying the bound token is necessary but not sufficient — the security
    property is the daemon *refusing* the cross-session use); the conformance
    golden asserts the lane env carries the bound token and no DSN; a fixture
    asserts a lane worktree `git status` never shows a credentialed settings file
    (#70) and a lane process cannot open the daemon's Postgres (#87).
  - **[live-corroborated]** a lane started with session S cannot act as session S′
    in a live run — corroboration of the hermetic gate, not the primary gate.

### W2 — Every declared adapter can hold a multi-turn seat (RFC 0096 / 0088)

- **Issues:** [#95](https://github.com/halbritt/striatum/issues/95) (agy session closes after first turn, re-registers an unattested duplicate), [#85](https://github.com/halbritt/striatum/issues/85) (agy stalls in MCP discovery before claiming), [#76](https://github.com/halbritt/striatum/issues/76) (agy blocks on an interactive feedback prompt), [#139](https://github.com/halbritt/striatum/issues/139) (umbrella: agy not viable on a fresh repo → 3-lane panel collapses to 2).
- **Remaining:** make the `agy` agent-loop hold a seat across turns (the
  submit-driver re-enters the same session instead of re-registering), suppress
  the trust-gate/feedback prompt via env or config the way #76/#101 suppress the
  claude/codex nags, and bound the MCP-discovery probe so it never idles past the
  deadline. Until then the **claude/codex seats are the supported multi-lane
  shape** (already true in the bundled examples).
- **Acceptance:** the **RFC 0101 Layer-2 adapter-conformance fixture** runs an
  `agy` lane through a two-turn claim→publish→claim cycle against the *installed*
  CLI in CI; a version bump that breaks the seat fails CI, not a live panel.

### W3 — The lane survives transport and daemon churn (RFC 0091 / 0101)

- **Issues:** [#141](https://github.com/halbritt/striatum/issues/141) (a daemon restart orphans lane helpers `helper_process_gone`; the agent-loop receiver busy-loops on the recreated socket and never reconnects), [#125](https://github.com/halbritt/striatum/issues/125) (a codex lane reports readiness via `session.report` instead of `work.ack` and stalls).
- **Remaining:** (a) the agent-loop receiver **reconnects with backoff** across a
  socket recreation instead of dying; the restarted daemon **reconciles attached
  supervisors** (rebind helpers) rather than leaving them `helper_process_gone`;
  investigate whether the observed crashes were themselves schema-drift
  (#141 names the `snapshot_sha256` shape — already fixed as #142, so re-verify
  the crash is gone before building reconnect). (b) `work.ack` is made
  **non-substitutable** — a `session.report` claiming acknowledgement before
  `work.ack` is flagged, not accepted, so the control plane and the pane agree.
- **Acceptance** (review R1/codex-R1 — the in-process chaos restart is **not**
  #141's real surface and must not be the only gate):
  - **[live-corroborated, primary for #141]** a real **OS-level/systemd restart
    that recreates the socket** mid-run asserts the daemon **rebinds helpers**,
    the agent-loop receiver **reconnects**, `work.ack` integrity holds, and the
    repo-write job **completes through production handlers** — *escalation is not
    an acceptable outcome for this expected, recoverable fault*.
  - **[hermetic gate]** the existing in-process chaos restart is kept but labeled
    a **known approximation** (it does not orphan OS helpers or recreate the
    socket); escalation-within-budget is the success criterion only for an
    explicitly **unrecoverable** injected fault class, with a separate assertion
    that it does not silently wedge the run.
  - Re-verify the #142 schema-drift crash is gone before building reconnect (so
    the gate cannot go green while the real systemd-restart orphaning persists).

### W4 — The interrogation window outlives a single reviewer attempt (RFC 0095)

- **Issues:** [#131](https://github.com/halbritt/striatum/issues/131) (a retry/replacement reviewer cannot interrogate after the panel target left the live `awaiting_interrogation` window), [#134](https://github.com/halbritt/striatum/issues/134) (`interrogation.open` rejects a claimed, acked reviewer packet because the interrogator/target session is no longer active).
- **Remaining:** the **panel-owned interrogation window** (RFC 0095 #65) must
  remain openable for *all* reviewer attempts the workflow still expects — a
  replacement reviewer either re-attaches to a held window or receives an
  explicit, non-wedging "interrogation unavailable, proceed on the published
  artifact" signal, instead of a hard `target_unavailable`. These are the two
  issues over-attributed to the Phase 0a deadlock fix and reopened on 2026-06-01.
- **Acceptance:** a PG-gated test drives a panel through a reviewer
  replacement/retry after the target session leaves the window and asserts the
  replacement reviewer reaches a verdict (no `target_unavailable` wedge).

### W5 — Artifact contracts are legible at the point of need (RFC 0100 P2)

- **Issues:** [#126](https://github.com/halbritt/striatum/issues/126) (finding artifact packets carry the exact front-matter skeleton + severity enum), [#128](https://github.com/halbritt/striatum/issues/128) (workflow authoring guards downstream `write_scope` drift).
- **Remaining:** extend RFC 0100 Phase 1 (enriched errors + the optional-metadata
  allowlist already landed) so a `finding`/`findings_ledger` packet ships the
  literal skeleton and enum the publisher will accept (#126), and `workflow
  validate`/`lint` warns when a downstream job's `write_scope.allowed_paths` no
  longer covers an upstream artifact it must consume (#128). Purely legibility,
  not permissiveness.
- **Acceptance:** `striatum artifact describe finding` emits the skeleton; a
  drifted `write_scope` is a `workflow lint` warning with the offending pair named.

### W6 — Run orchestration is honest and coordinated (RFC 0097)

- **Issues:** [#115](https://github.com/halbritt/striatum/issues/115) (editing `workflow.json` no-ops on a prepared/running run — the frozen snapshot — with no operator signal), [#138](https://github.com/halbritt/striatum/issues/138) (workflows need shared-resource coordination for DB-backed review gates).
- **Remaining:** **#115** — `run prepare`/`run start`/dashboard surface that a run
  is pinned to its workflow **snapshot** and that a live `workflow.json` edit will
  not take effect (prepare a fresh run); this is the exact gap hit live during the
  RFC 0097 proof. **#138** — declare and serialize a shared resource (e.g. a
  DB-backed review gate) so parallel jobs that contend on it coordinate rather
  than race.
- **Acceptance:** a stale-snapshot edit produces an operator-visible warning (not
  a silent no-op); a declared shared-resource gate serializes its contending jobs
  under test.

### W7 — The operator is a bounded, well-served processor (RFC 0099 / 0102)

- **Issues:** [#92](https://github.com/halbritt/striatum/issues/92) (constrained AI operator mode limited to the Striatum control surface), [#112](https://github.com/halbritt/striatum/issues/112) (supervised lanes use the tmux backend by default + extract/log trajectories).
- **Remaining:** **#92** — the RFC 0099 constrained-operator surface (one control
  plane, scope-checked mediated writes, audited escapes) that consumes the RFC
  0101 Phase-4 escalation. **#112** — make tmux-backed lanes the default and
  extract their trajectories to the one operator surface (RFC 0102's "one surface,
  one high-signal view, `(run, workflow_job_id)` identifiers"; the operator
  "headline ask"; +RFC 0075).
- **Acceptance** (review R1 — operator restraint is unobservable/unfalsifiable, so
  the headline outcome is **[qualitative]**, gated by an audited proxy):
  - **[hermetic gate]** a full run completes with **zero out-of-band control-plane
    escapes recorded in the audit log** (the gateable proxy for "stayed on the one
    surface"); audited escape decisions (RFC 0099) are the only sanctioned exits.
  - **[qualitative]** the operator can drive the run without dropping to
    tmux/systemctl/psql in the normal loop — best-effort, corroborated by the
    audit-log gate above, not independently gateable.
  - **Trajectory privacy clause:** extracted lane trajectories on the operator
    surface are **ephemeral operator-local diagnostics** (D028/D151/D154) — never
    durable transcript capture/export, daemon state, byline, or verdict input.

## Priority ordering (not a hard dependency)

Suggested order W1 → (W2, W3, W4) → (W5, W6) → W7. **This is a priority ordering by
risk, not a dependency graph** (review F1): each layer is shippable alone, so there
is no hard "blocks" relation. The distinction matters because the **umbrella
acceptance's critical path is W2/W3/W4, not W1** — W1 is sequenced first for
*risk*, not because the headline goal needs it first.

1. **W1 (sandbox)** first **by risk** — an un-sandboxed lane carrying the
   operator's own credentials is the worst failure mode to leave open (the trust
   substrate RFC 0097 named). It is *not* on the umbrella's critical path.
2. **W2/W3/W4** are the **multi-lane viability tier** and the umbrella's actual
   critical path — every seat holds, lanes survive churn, panels survive reviewer
   replacement. **Caveat (review F2):** the umbrella needs only *two* seats =
   claude + codex, both of which already hold; **W2 (agy) is therefore the most
   deferrable workstream**, not a blocker for the headline goal — it is sequenced
   here for completeness of the declared adapter set, not necessity.
3. **W5/W6** are **legibility/coordination** — they remove the friction that turns
   good multi-lane work into a failed completion. **Scope note (review F3):**
   #138's "declare + serialize a shared resource" is a *new orchestration
   primitive*, not pure residual tail; if it grows beyond a bounded gate it should
   spin out to an RFC 0097/0099 follow-up that owns it (see Non-goals).
4. **W7** is the **operator-side payoff** — it consumes the honest signals the
   other layers produce.

## Acceptance (behavioral)

This RFC is accepted in slices (each workstream's per-workstream acceptance is a
landing gate). The umbrella acceptance has **two tiers** — review R2 caught that a
single multi-lane trial with one injected fault is itself *"proven once"* (the very
weakness this RFC criticizes RFC 0097 for), so it is named honestly as the **new
floor**, not as "production-grade":

> **Floor (the new precondition, multi-lane proven once):** a **runner-fix
> developed by a real multi-lane, review-gated dogfood driven through the runner**
> — the **two supported seats (claude + codex**; both `process`/`agent_loop` lanes,
> so "distinct" means two instances of the one supported shape until W2 lands —
> review F5), one bounded `needs_revision` cycle with a live interrogation,
> surviving **one** injected lane/daemon fault — that completes end-to-end through
> the production handlers, hands-off, and lands the fix. *(The 2026-06-02 RFC 0103
> review dogfood already cleared the multi-lane review-gated half of this floor:
> claude presenter + codex/claude reviewers, two `needs_revision` verdicts through
> production handlers.)*
>
> **Production-grade (the ceiling — what earns the word):** the floor dogfood is
> **repeated across a fault-class matrix, not a single fault** — separate coverage
> for **(a)** W1 lane credential isolation, **(b)** W3 daemon/transport churn
> (real socket-recreation restart), and **(c)** W4 reviewer replacement /
> interrogation-window survival — and across **both supported seats**. One pass is
> the floor; the matrix is the ceiling. The minimal single-lane document proof
> (2026-06-01) sits *below* the floor.

## Non-goals

- No new durable persistence, hosted service, cloud API, telemetry, or durable
  transcript capture/export (D094/D028/D151 intact).
- Not a rewrite of any slice-RFC — this RFC **consolidates and sequences** their
  residual tail; each workstream lands within its owning RFC's model.
- Not new operator authority — W7 is a cooperative harness contract plus a
  Striatum-mediated surface (RFC 0099's honest-limit framing), not forcible
  sandboxing of a process Striatum did not spawn.

## Relationship to prior RFCs

- **RFC 0096** (lane trust boundary) — W1/W2 are its V2.
- **RFC 0095** (revision-safe lifecycle) — W4 is its interrogation-window tail.
- **RFC 0091/0101** (lane health / robust execution) — W3 extends the chaos suite
  and honest-liveness work; this RFC is the residual tail of the RFC 0101 arc.
- **RFC 0100** (self-describing artifact contracts) — W5 is its Phase 2.
- **RFC 0097** (run orchestration) — W6; and this whole RFC is the hardening that
  takes RFC 0097 self-hosting from proven-once to production-grade.
- **RFC 0099/0102** (constrained operator / attention economy) — W7.
