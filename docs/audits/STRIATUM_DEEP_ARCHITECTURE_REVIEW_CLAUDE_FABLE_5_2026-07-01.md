# Striatum Deep Architecture Review — Claude Fable 5 — 2026-07-01

## Thesis

**ROUGHLY RIGHT-SIZED | ON TRACK** — confidence **medium-high** — biggest
risk: **the headline guarantees (model-diverse review, enforced provenance)
are recorded weaker than they read — declared-labels-only, validate-time-only,
advisory-by-default — while operator attention is consumed servicing the
provenance machine's own exhaust.**

The right-sizing is skewed, not uniform: **overbuilt along the
identity/confidentiality axis** (credential projection, attribution
machinery for principals that do not exist yet), **underbuilt along the
failure-boundary and wake-the-human axis** (untyped crash-loop boot error,
no escalation notifier, codex-only credential doctor check, no model
identity on verdict rows).

Load-bearing assumption: striatum is the product described in
`docs/reference/spec.md` — a generic, local-first runner whose value is
multi-model reviewed autonomous work with recoverable provenance — and it
will eventually be pointed at a target repository that is not itself. If it
never leaves self-hosting, the sidecar-provenance and credential-tower
calculus inverts and several KEEPs below become OVERBUILT. That is the
assumption I would least like to be wrong about; the evidence that would
flip it is a 2026 roadmap that never schedules a second target repo.

## Method — and what is different about this entry in the series

This is the fifth deep architecture review in `docs/audits/` in 30 days
(06-02 Opus, 06-11 Fable, 06-24 Opus, 06-28 GPT-5 Codex). The 06-28 entry
did a whole-tree pass at `8d794fb8`, three days and ~30 commits ago. This
review deliberately does not re-derive that baseline; it stands on it and
does what the series has not yet done: an adversarial divergent-frame pass
(five isolated cognitive frames: regulator, attacker, 3am on-call,
$0-budget, remove-the-load-bearing-assumption; 29 candidate findings), with
the top three fused findings **premise-checked against source at file:line
and tagged VERIFIED / REFUTED / ASSERTED** before being allowed into this
report. Two claims a reviewer would naturally assert were refuted by that
discipline and are recorded below so future reviews stop re-asserting them.

Coverage is therefore honest, not total: I read the whole repository's
*inventory* and history, the full prior-review series, the operator brief,
roadmap, spec sections, and every file cited in evidence below — I did not
re-read all 121k lines of Go this cycle. Execution was read-only
(`git`/`grep`/file reads); no builds, tests, or daemon mutations were run.

### Premise-check results (the series should reuse these)

| Claim | Status | Evidence |
| --- | --- | --- |
| Migration hash mismatch crash-loops the daemon | **VERIFIED** | `go/pkg/db/migrations.go:284` returns an *untyped* `fmt.Errorf`; propagates to `go/cmd/striatumd/main.go:252` `log.Fatalf`, exit 1; unit has `Restart=on-failure` with `RestartPreventExitStatus=78 79` (`go/pkg/cli/localcommands/striatumd.service.tmpl:16,37`) — RFC 0142 built typed exit-79 parks for three sibling boot failures (`main.go:218-250`); hash mismatch never joined them. This is the 2026-06-26 44-restarts-in-90s incident signature, still live. |
| Doctor RED blocks recovery sweeps | **REFUTED** | Zero code coupling: no `doctor` reference in `go/pkg/recovery/`; sweeps continue past per-run failures (`go/pkg/recovery/sweep.go:100-113`). The blocking is operator policy (AGENTS.md "red = stop-and-fix") amplified by the one-bit fold at `go/pkg/reads/doctor.go:367` (`ok = len(problems)==0`) mixing availability and provenance classes. |
| A poison run's sweep panic bounces the whole daemon | **REFUTED** | Fixed as #451/FMA-001: per-run `recover` at `go/pkg/recovery/sweep.go:32-41`; outer backstop recovers and cancels cleanly (`go/cmd/striatumd/main.go:910-916`). Residual gap: the degraded cursor never latches — a poison run is re-attempted every tick forever. |
| Reviewer/implementer silently collapse to the same model | **REFUTED (strong form)** | A validate-time same-model lint refuses with exit 8 (`go/cmd/striatum/main.go:876-879,945-981`; rules in `go/pkg/workflowauthoring/lint.go:92-189`), and `sealed_patch` runs refuse to start rather than downgrade (`go/pkg/mutations/run.go:68-69`). **Survivors:** the check is declared-labels-only and validate-time-only; `verdicts` rows carry no model/provider columns (`go/pkg/db/sql/0005_repo_local_workflow_state.sql:235-254`; 0024 adds only attestation stamps); byline model is operator-typed free text (`go/pkg/mutations/claim.go:1496-1525`); `--allow-same-model-pairing` leaves no per-run stamp. |
| Provider-credential death is invisible until a lane wedges | **PARTIALLY VERIFIED** | `go/pkg/reads/doctor_lane_provider_auth.go:32` returns unless provider==codex — Claude credential expiry (the top observed wedge class in operator history) surfaces only as a generic liveness stall, despite RFC 0162's `laneproviderauth` already parsing Claude expiry. |
| No notifier exists on escalation | **VERIFIED** | Escalation is pure PG state (`go/pkg/mutations/recovery_decision_tree.go:452`, `recovery_escalation.go:176-250`); zero ntfy/webhook/notify hits in `go/`. RFC 0020 (accepted) specified `escalation_hook.kind ∈ {marker_file, webhook, shell}` (`docs/rfcs/0020-...md:216-222`) — it died in the RFC 0078 Python retirement and was never ported to Go. |
| Provenance bot-commits dominate mainline | **VERIFIED** | 59 of the last 200 main commits (29.5%) are `striatum:` bot commits (37 artifact publication, 14 lane source, 8 integrate). Porter path: `go/pkg/mutations/artifact_durability.go:157-159` commits artifact bodies onto the per-job worktree HEAD; integration merges carry them into main's tree under `docs/operator/artifacts/`. |
| Dual provenance chains carry a reconciliation tax | **VERIFIED** | PG hash chain (`repo_event_chain_heads.last_hash`) and git anchors (`content_sha256` vs `git show HEAD:<path>`, `artifact_durability.go:175-244`) are kept agreeing by ~1,620 LOC of doctor/porter/reseal machinery: `doctor_artifact_anchor.go` (900), `worktree_refs.go` (452), porter (270), plus `recovery_reseal.go` — 6 hard + 6 warning doctor classes exist for this alone. Note: blob-backed artifacts already bypass the git probe (`artifact_durability.go:189-192`) — a second durability path exists today. |

## Files reviewed / skipped

Directly read this cycle: `README.md` (prior cycles), `ARCHITECTURE.md`,
`AGENTS.md`, `docs/reference/spec.md` (provenance-modes and boundary
sections), `docs/operator/BRIEF.md` (head + 06-29/06-30 deltas),
`docs/operator/rfc-roadmap.md` (wave structure), `docs/decisions/decision-log.md`
(head, D264/D270/D276 rows), `docs/audits/` series (all four prior deep
reviews in full), `docs/audits/README.md`, and every file:line cited in the
premise-check table above (read by me or by a premise-checking agent whose
excerpt I reviewed). Quantitative inventory over the whole tree: tracked
files, per-package LOC, migration/bundle counts, churn, commit cadence,
author distribution.

Verified-by-agent (excerpts reviewed, full files not re-read by me):
`go/pkg/mutations/{artifact_durability,worktree,barrier_run_entity,claim,review,run,recovery_decision_tree,recovery_escalation}.go`,
`go/pkg/reads/{doctor,doctor_artifact_anchor,doctor_lane_provider_auth,worktree_refs}.go`,
`go/pkg/recovery/sweep.go`, `go/pkg/db/{migrations,connection,authority_bootstrap}.go`,
`go/pkg/db/sql/{0005,0024,0026}*.sql`, `go/pkg/workflowauthoring/lint.go`,
`go/cmd/striatumd/main.go`, `go/pkg/cli/localcommands/striatumd.service.tmpl`,
`docs/rfcs/{0020,0140}*.md`.

Deliberately skipped: the other ~100k lines of Go re-read (covered by the
06-28 whole-tree pass at `8d794fb8`; delta since is ~30 commits, dominated
by RFC 0165 v5 design artifacts and doctor cleanup), historical operator
artifact bodies under `docs/operator/artifacts/` and `docs/records/_frozen/`
(provenance, not behavior), `build/` output, `.striatum/`, lockfiles.

## The numbers

- 2,357 tracked files; **121,200 LOC non-test Go; 108,701 LOC test Go**
  (0.90:1 test:code — healthy and real).
- **117,539 lines of Markdown across 919 files** — the docs corpus now
  weighs as much as the codebase (0.97:1 docs:code).
- 158 RFC files; 277 logged decisions; 47 runtime migrations + 22 owner
  bundles; 151 daemon methods / 99 CLI routes / 10 deprecated (06-28 count).
- 1,873 commits in ~9 weeks (~30/day, agent-powered); 29.5% of recent
  mainline commits are provenance bot-commits.
- Largest package: `go/pkg/mutations` at 42.5k non-test LOC; largest file
  `recovery_decision_tree.go` at 2,447 lines.
- Churn (4 weeks) concentrates in state docs: `CHANGELOG.md` ×157,
  `docs/rfcs/README.md` ×95, `decision-log.md` ×95, `BRIEF.md` ×73 — vs
  `mutations.go` ×51 as the top code file.

## Value-vs-complexity ledger

| Component | What it does | Value (to the solo operator) | Complexity | Verdict | If SIMPLIFY/CUT: what changes |
| --- | --- | --- | --- | --- | --- |
| Daemon + PG spine (`db` 5.5k, `rpc` 2.2k, 47 migrations, 22 owner bundles) | Single-writer authority, fail-closed audit, capability gates | Core: crash recovery and honest state are the product | High, earned | **KEEP** | — |
| `mutations` (42.5k) | The entire workflow state machine | Core | Very high; front-door map of everything | **SIMPLIFY** | Retire 10 deprecated aliases + one-shot backfills (`corpus.migrate_historical_dogfood_file`, `artifact.backfill_blob`); consider splitting the 2.4k-line recovery decision tree along its own stage boundaries |
| `reads` incl. doctor (19k) | Status, doctor, exports, bootstrap | High: the operator's eyes | High; doctor's `ok` is one bit folding two planes (`doctor.go:367`) | **KEEP + SPLIT** | Tag problem records `availability` vs `provenance`; emit `availability_ok`/`provenance_ok`; D276's notices/warnings split already walked halfway there |
| Lane runtime (`supervisor` 2.3k, `agentloop` 2.3k, `sessionliveness` 1.1k, `lanehealth` 0.6k, `adapterconformance` 3.4k) | Drives arbitrary terminal CLIs under supervision | Core: provider-agnostic terminal lanes ARE the differentiation | High, load-bearing | **KEEP** | — |
| Lane auth/identity (`laneproviderauth` 1.5k + RFC 0110/0142/0143/0165/0167/0168 arc across `mutations`/`db`) | Credential projection, attribution, DB-enforced writes | Mixed: two-role split is also the deploy-safety/test-oracle substrate; but confidentiality depth serves absent principals | High and still growing (0165 needed five design versions) | **REBALANCE** | Freeze confidentiality depth at current level; spend the next unit of effort on the *freshness* axis (TTL monitoring, pre-emptive pause, claude-side doctor check) where the live wedges actually are |
| Provenance porter + anchor reconciliation (~1.6k in porter/doctor/reseal + `artifactcontracts` 2.3k, `blob` 0.8k, `records` 0.9k) | Keeps PG chain and git anchors agreeing; lands artifacts on mainline | Medium: tamper-evidence is real; the dual-chain agreement cost is pure tax | Very high per unit value | **SIMPLIFY** | Per-artifact placement policy (`mainline\|sidecar\|blob\|ephemeral`); blob-first for dialogue chatter (the Garage endpoint is already provisioned); Rule B + acknowledged-loss machinery retires for new artifacts |
| Workflow authoring/generation (`workflowauthoring` 4k + `workflowgenerate` 5.3k + `templates` 0.9k) | Declares and generates workflow.json shapes | High for repeatable dogfoods | Medium; shapes frozen per RFC 0106 | **KEEP** | Watch template count; no new shapes without a consuming run |
| Verifier (2.2k) | Sealed receipts, ASSERTED-vs-VERIFIED honesty | High: the only executable-witness path | Medium | **KEEP** | — |
| Web UI (`webservice`+`websse`+`webassets` ≈1.5k) | Browser read surface | Modest | Bounded and small | **KEEP** | The divergent-frame instinct to cut it was refuted by size — it is not where the mass is |
| Metrics (3.4k) | Prometheus exporter | Modest-high (feeds the host's observability stack) | Bounded, allowlisted | **KEEP** | — |
| CLI + installers (`cli` 4.7k, `cmd/striatum` 4.3k, `installers` 1.7k) | Thin RPC client + bootstrap | Core | Moderate | **KEEP** | Adopt the 06-28 route-budget rule — still unactioned |
| Docs corpus (919 files, 117.5k lines, 158 RFCs, 277 decisions) | Provenance, decisions, operator state | High for archaeology; the retrieval/cognition tax is now code-sized | Very high and compounding | **SIMPLIFY** | RFC 0170 P1 + placement policy are the levers; every durable Markdown artifact carries a cleanup debt |
| Recovery (`recovery` 1.5k + recovery files in `mutations` ≈5k) | Sweeps, auto-finalize, decision tree, escalation | Core: the unattended-operation promise | High | **KEEP + FINISH** | Typed boot-error park, latching breaker, escalation notifier — the last mile is missing, see inverse check |

Roll-up: 13 components; 0 CUT, 4 SIMPLIFY, 1 REBALANCE, rest KEEP. The
plausible reclaim is not measured in LOC deleted but in recurring tax
retired: ~1.6k LOC of anchor-reconciliation machinery plus 6 doctor problem
classes (placement policy), ~30% of mainline commit noise, and the
alias/backfill surface. **The single worst piece of over-engineering:** the
dual-provenance agreement complex — porter-commits-to-mainline plus the
doctor classes and reseal paths that exist only because artifact bodies
share the source worktree and default branch with human edits. It is not
speculative-scale or resume-driven; it is an early convenient default
(publish into the tree you already have) that the system then built an
immune system around instead of revisiting the placement decision. The
process-side twin: RFC 0165 consuming five design versions × three gate
cycles to project a credential on a box whose only principal is its owner.

## The inverse check — what's actually missing

High-bar only; each is a live risk, each verified absent:

1. **A typed park for migration hash mismatch.** The park rail exists
   (exit 79, `RestartPreventExitStatus`), three sibling failures use it,
   this one crash-loops instead. One deploy away from repeating the
   2026-06-26 incident. ~40 lines, two files.
2. **Any notifier on escalation.** Every hardened recovery path terminates
   in `escalation_pending`/`needs_operator` — a state nobody is told about.
   The product's own goal is unattended operation; RFC 0020 accepted the
   hook and the Go rewrite dropped it. Wedges are currently discovered by
   polling, usually at 9am.
3. **Claude-side provider-auth doctor check.** The top observed wedge class
   (expired Claude credentials) is invisible to doctor because the check
   guards on codex only, while the expiry parser for Claude already exists
   in `laneproviderauth`.
4. **Model identity on verdict rows.** The system's headline claim —
   adversarial cross-model review — is unauditable after the fact: no
   verdict records which model family produced it, and the same-model
   override leaves no stamp. Until this exists, "multi-model review
   happened" is ASSERTED, not VERIFIED, for every historical run.

Nothing else clears the bar. Notably, recovery breadth, authority
enforcement, and test mass — the usual missing things — are all present.

## On track?

**Stated:** reliability-first waves, subtraction budget, feature fuse
(`docs/operator/rfc-roadmap.md`). **Actual:** the commits agree — the last
two weeks are lane-health, doctor legibility (D276), credential design
(0165 v5), and reseal fixes, not features. Subtraction is real: D264
(subtraction gate), D270 (cross-repo surface deleted), 58+62 debris rows
pruned through daemon-supported paths. Velocity (~30 commits/day) is
agent-powered; the binding resource is operator attention, and the churn
table shows where it goes: state-doc maintenance (CHANGELOG ×157, RFC index
×95, decision log ×95 touches/month) now rivals code work.

Two honest cautions. First, the 06-28 review's two P0 recommendations
(blob boundary wording, route-budget rule) show zero follow-through three
days later — early, but this series only works if each entry's P0s are
discharged or explicitly refused by the next entry; otherwise reviews
become another artifact class accumulating faster than they are retired.
Second, this is the fifth deep review in 30 days, all concluding
RIGHT-SIZED/ON TRACK: the review cadence itself has become part of the
provenance exhaust it keeps diagnosing. **Mine:** budget the series like
everything else — one deep review per shipped wave, not per ~9 days.

## Greenfield / north-star

Given the real constraints, I would rebuild *almost* this: single Go
daemon, daemon-owned Postgres, capability-gated RPC/MCP, supervised PTY
lanes (the provider-agnostic terminal boundary is the product; an SDK
integration would be lock-in and lose every non-API CLI), worktree-per-job
isolation. The deltas, in order of conviction:

1. **One provenance chain, not two.** PG hash chain authoritative;
   artifact bodies content-addressed (blob-first); git carries an *export*
   (receipts as notes or a sidecar ref), never a co-equal authority that
   must be reconciled. This retires the anchor-mismatch class family at the
   root.
2. **Placement policy per artifact class from day one** — final
   deliverables integrate to mainline; per-cycle dialogue never touches it.
3. **A typed boot-failure taxonomy with park-by-default** — deterministic
   failures park (exit 79), only plausibly-transient errors restart-loop.
4. **Two-plane doctor from day one**; the stop-and-fix contract binds to
   availability only.
5. **Model-identity stamps on verdicts from day one** — the guarantee you
   sell is the first thing you make auditable.
6. What I would *not* do: event-sourcing/durable-workflow substrate
   (leases/heartbeats are epicycles, but replay semantics for PTY side
   effects is a research project, and the current shape is proven under
   real crashes); per-lane containers/microVMs (heavier than the two-user
   model for zero present adversaries).

The gap between current code and this north star is closable
incrementally — items 1–5 are the recommendations table. The delta is not
cosmetic, but nothing requires a rewrite.

## Strengths worth preserving

- **The single RPC choke point with fail-closed audit append**
  (`rpc/server.go`) — every surface funnels through one authority; refusing
  to answer without audit provenance is the correct default and would be
  lost by any "fast path".
- **Refusal-over-downgrade discipline**: `sealed_patch` refuses to start
  rather than silently downgrading (`run.go:68-69`); the publisher refuses
  invalid front matter with exit 6; same-model pairings refuse at validate
  with exit 8. This is the house style and it is right.
- **The typed error catalog reaching agents in-band** (RFC 0111) — failure
  legibility as contract is what lets an AI operator dispatch instead of
  flailing.
- **Two-role pgtest oracle** (RFC 0142 P0) — the 42501 SEV proved grants
  must be tested under the runtime role; this fixture is the only thing
  standing between the authority inventories and fiction.
- **Verifier receipts that refuse to inflate ASSERTED to VERIFIED** — the
  same epistemic honesty this review depends on.
- **Recovery-through-the-daemon-only discipline** (AGENTS.md paste-over
  guardrail) — the provenance is only worth its cost because manual
  bypasses are culturally forbidden.
- **Active subtraction with receipts** (D264/D270/D276) — rare and worth
  protecting from feature pressure.

## Concerns, ranked

- **blocker** — Untyped migration-hash-mismatch → `log.Fatalf` crash-loop
  (`migrations.go:284` → `main.go:252`). Recurred 2026-06-26 (44× in 90s,
  wedged two live runs). The fix rail already exists.
- **blocker (for the unattended-operation goal)** — Escalation terminates
  in unnotified PG state; RFC 0020's accepted hook was never ported. Every
  reliability investment upstream funnels into a silent terminal state.
- **serious** — Verdict rows carry no model identity; the same-model
  override leaves no stamp; co-blindness elimination is unauditable
  post-hoc (`0005_...sql:235-254`, `claim.go:1496-1525`).
- **serious** — `doctor_lane_provider_auth.go:32` guards codex-only; the
  most frequent real wedge class (Claude credential expiry) is doctor-dark.
- **serious** — 29.5% of mainline commits are provenance exhaust backed by
  ~1.6k LOC of reconciliation machinery; for any target repo that is not
  striatum itself this is an adoption dealbreaker
  (`artifact_durability.go:157-159`).
- **smell** — Docs mass ≈ code mass (117.5k vs 121.2k lines) with
  state-doc churn rivaling code churn; RFC 0170 P1 is the named lever and
  is still observe-only.
- **smell** — 151 methods / 10 deprecated aliases; the route-budget rule
  recommended 06-28 remains unadopted.
- **smell** — `recovery_decision_tree.go` at 2,447 lines is the largest
  file in the repo and growing; the sweep breaker that would bound it never
  latches.
- **smell** — Five deep reviews in 30 days; introspection cadence outrunning
  remediation cadence.

## Recommendations

Only changes I would personally make, smallest-viable first:

| Priority | Change | Rationale | Benefit | Risk | Effort |
| --- | --- | --- | --- | --- | --- |
| P0 | Type `MigrationHashMismatchError` in `go/pkg/db/migrations.go:284`; add the `errors.As` branch beside `AwaitingOwnerDDLError` in `go/cmd/striatumd/main.go:218` → exit 79 park with remediation text | Kills the worst live incident signature; the rail exists | Crash-loop class retired; wedged-run blast radius gone | Minimal — two files, typed-error test beside `migrations_test.go:1268` | hours |
| P0 | Widen `doctor_lane_provider_auth.go` past codex-only using `laneproviderauth`'s existing Claude expiry parser | Top real wedge class becomes pre-run-visible | Credential death caught before lanes wedge | Low | hours |
| P1 | Resurrect RFC 0020 `escalation_hook` as opt-in post-commit best-effort POST (`STRIATUM_ESCALATION_NOTIFY_URL`, loopback/tailnet ntfy), fired from `recovery_escalation.go`'s inbox insert; never inside the transaction | Closes the unattended-operation loop; no cloud; already-accepted design | Escalations reach a human on purpose | Low if post-commit and best-effort | ~1 day |
| P1 | Migration 0048: `model_identity_declared` / `model_family_at_record` / `model_identity_basis` on `verdicts`, stamped at `review.go:286` (+ recovery/operator sites as `unknown`); run-level `co_blindness` qualifier in the completion record; retro-audit read over historical runs | Makes the headline guarantee auditable; the retro-audit data decides whether an enforcement tier is ever worth building | ASSERTED→VERIFIED for the product's core claim | Must fail toward `unknown` — rendering declared labels as authoritative manufactures the false assurance it exists to kill | days |
| P1 | Doctor plane tags + `availability_ok`/`provenance_ok` split at `doctor.go:367`; rebind the AGENTS.md stop-and-fix rule to `availability_ok` | D276 walked halfway; finish the walk | Provenance quarantine stops freezing healthy-run operations | Mislabeling dual-nature checks — keep the recovery-gate breach on the availability plane | ~1 day |
| P2 | Per-artifact placement policy (`mainline\|sidecar\|blob\|ephemeral`) behind per-repo opt-in; blob-first for dialogue chatter; keep final deliverables on mainline | 29.5% commit noise + Rule B/acknowledged-loss machinery retire for new artifacts; the `refs/striatum/*` namespace and blob path already half-exist | Adoptable against non-self targets | **Reviewer-context seeding**: downstream lanes read upstream artifacts from run-branch-seeded worktrees — sidecar placement must overlay or route through `artifact.get_content`, or reviews go silently blind (worse than noise) | ~1 week, gated on a design note for the seeding path |
| P2 | Latching sweep breaker: `consecutive_degraded` on the scheduler cursor; exclude tripped runs from the candidate SELECT (`sweep.go:71`); trip flips `escalation_pending` | The per-tick recover exists; the latch doesn't | Poison runs stop burning sweep budget forever | Low | ~1 day |
| P2 | Discharge or explicitly refuse the 06-28 P0s (route-budget rule; blob boundary wording), and budget this review series to one per shipped wave | A review series that accumulates unactioned P0s is provenance exhaust with a verdict attached | Series stays load-bearing | None | hours |

## Open questions

1. Is a second target repository (not striatum) a 2026 goal? The placement
   policy's priority — and part of the thesis — hangs on this.
2. What fraction of historical accepted runs were actually cross-family?
   (The P1 retro-audit answers this; if the answer is "most were
   same-family under overrides", the diversity stamp escalates to P0.)
3. Is the 0165/0168 credential-projection build still the right next unit
   of security effort versus the freshness axis (TTL monitoring,
   pre-emptive lane pause) where the observed failures are?
4. Should the deep-review cadence itself be governed by the D264-style
   budget — and should each entry be required to open tracker items for its
   P0s so follow-through is mechanical rather than aspirational?
