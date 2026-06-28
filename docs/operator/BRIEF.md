---
schema_version: "striatum.operator_brief.v1"
artifact_kind: "operator_brief"
brief_id: "brief_2026-06-27_v2.39.0-release"
supersedes: "brief_2026-06-25_v2.38.0-release"
scope_links: ["docs/operator/plans/provenance-durability-campaign-2026-06-14.md", "docs/operator/plans/rfc-0126-0128-implementation-campaign-2026-06-14.md", "docs/rfcs/0126-multi-reviewer-revision-coherence.md", "docs/decisions/decision-log.md", "CHANGELOG.md"]
context_budget_lines: 420
retrieval_priority: "high"
status: "current"
---

# Operator Brief
author: operator-claude-opus-4-8-001

## 2026-06-28 delta - Multi-campaign supervision Level-1 completed

`MULTI_CAMPAIGN_SUPERVISION` Level-1 ideation completed as
`run_7899e132bf7996d49c9b81d0df905962` and was integrated to `main` through
`run.integrate` at `8ac96c62`. The workflow scaffold lives at
`docs/operator/workflows/multi-campaign-supervision-level1/workflow.json` and
uses Striatum's supported `divergent_ideation` shape with direct `codex`
agent-loop `pty_helper` lanes and per-job worktree isolation. The seed
explicitly carries the Level-0 live-human artifacts, the current operator brief, and
`docs/how-to/how-to-agent.md` as required context.

The completed artifacts are recorded under
`docs/operator/artifacts/multi-campaign-supervision-level1/` with 11/11 jobs
attested as supervised lanes. The shortlist is authority receipt expiration,
fresh-context replay tests, deferral quarantine / scope-drift refusal, and a
cross-surface contradiction gate. No architecture is accepted, and no product
code, schema, route, daemon method, UI, or build ticket was created. The next
legitimate step is to take the Level-1 synthesis into a falsification gate before
any design-to-build readiness claim.

## 2026-06-28 delta — RFC 0168 P0 build v3 integrated; RFC 0143 Slice B unblocked

RFC 0168 P0 build v3 is reviewed, accepted, integrated, and pushed to `main`.
The accepted Codex-only run was `run_aa4e1c988eddb78b255afa0e63a75e6c`, merged
through `run.integrate` at `b7f48ab1`; follow-up test isolation landed at
`42d9579c`, and current `main` includes later cleanup commits through
`3ad69236`. Runtime schema **47** adds `lane_uid_leases`; owner bundle **0023**
reasserts its runtime authority. `supervise.start` can allocate a concrete OS
user from `STRIATUM_LANE_UID_POOL`, writes the uid lease id/generation into
supervisor metadata and lane env, and the active-supervisor
control/attestation/report path fails closed if that generation no longer
matches the active lease row.

This v3 pass closes the final v2 blockers: uid return is gated on S1-S3 cleanup
plus complete P1-P5 proof (including kill failure fail-closed, provider/home
store absence, supervisor scratch absence, and no active lease worktrees or
workspaces); `supervise.report` validates the live lease generation before
heartbeat or terminal metadata writes; and relative provider credential
selectors are resolved against the lane launch root / repo root before the
in-repo refusal.

The build also moves MCP bearer config files under private
`.striatum/scratch/<supervisor_id>` directories, refuses provider-owned
credential/cache selectors that resolve inside the target repository while
allowing ordinary non-credential lane env, grants created job
worktrees/workspaces to the selected lane user, and surfaces stuck/quarantined
uid leases through recovery and `doctor`. Post-integration verification in the
operator session passed `go test ./...`, `make check-docs`, `make lint`, and
`make typecheck`; `doctor` is green with no active runs, blockers, or
checkpoints. RFC 0143 Slice B (`CapabilityReseal`) is now unblocked and is the
next roadmap item.

## 2026-06-28 delta — RFC 0171 accepted, first build slice shipped

RFC 0171 is accepted as D273. It addresses the 2026-06-28 architecture-review
finding that generated operator records and run-shaped docs are now the main
repo-cognition drag: bodies should move to daemon-indexed blob storage, while
git keeps compact dockets, pointer manifests, accepted product docs, source/doc
changes, and intentional publication indexes.

The first build slice shipped runtime schema **46** (`generated_records`),
the deterministic record docket domain, daemon-backed `records.docket` /
`striatum records docket <run-id>`, blob-required publish fail-closed posture,
workflow-generator placement defaults, `records migration inventory`, opt-in
check-docs Striatum URI and generated-body hygiene checks, and reference-doc
updates. `striatum://` materialize/hydrate, historical import reconstruction
proof, and doctor record/docket integrity checks remain pending. Broad
historical deletion remains blocked until byte-identical reconstruction proof
exists.

## 2026-06-28 delta — RFC 0171 historical import proof implemented

RFC 0171 now has the proof-first historical import path for the
`safe_to_blob_index` inventory subset. `striatum records migration import`
routes each selected manifest entry through daemon RPC
`records.migration.import`, uploads the body through the daemon blob client, and
upserts `striatumd.generated_records` without deleting tracked files.
`striatum records migration verify` compares the original inventory manifest to
daemon-fetched blob bytes, and `striatum records migration materialize` writes
verified bodies only under ignored `.striatum/scratch`.

`doctor --verbose --json` now includes `generated_record_integrity` with stable
problem codes for missing blobs, corrupt/unreadable bodies, swapped blob
key/hash metadata, duplicate source rows, and missing blob metadata. This moves
RFC 0171 from "inventory only" to "imported and reconstructable"; broad
historical deletion remains blocked until a separately authorized pilot uses
that proof to retire source files.

## 2026-06-27 delta — v2.39.0 release

**v2.39.0 (2026-06-27)** cuts the post-v2.38.0 reliability and RFC 0170 P0
release. It includes the bundle-0022 `runs` projection outage fix, terminal
fan-in debris doctor downgrades, RFC 0168 P0 design acceptance (D272), and RFC
0170 P0 observe-only culling substrate/runtime schema **45**. GitHub `main` CI
is green at `3b4ef294` before tagging; `striatum doctor --json` is green with no
problems, stale leases, waiting humans, or operator-needed items.

Deploy note: this release includes runtime migration **0045** and no new owner
bundle. Install/restart the v2.39.0 binaries to move the host from v2.38.0 to
v2.39.0; verify with `striatumd -describe`, `striatum doctor --json`, and the
live release workflow result for tag `v2.39.0`.

## 2026-06-27 delta — RFC 0170 P0 built, verified, and integrated

RFC 0170 P0 is on `main` as runtime schema **45** (`02a15e83`). The original
Claude-backed build run `run_8ff1498595f29cee792314080e9606de` was canceled
because Claude credits are unavailable until **2026-06-30 15:59 UTC**. The
workflow was switched to Codex lanes, README scope was added, and the corrected
Codex-only run `run_992bd797fc136f1e3d782f443f9fb2ad` completed with
`accept_with_findings` and integrated through `run.integrate`.

Implemented P0 surface: runtime migration `0045_cullable_entity.sql`,
read/write authority inventory rows, the read-only `DecayTickSweep` attached to
the recovery scheduler off the wait-gating path, README schema 45 status, and
operator artifacts under `docs/operator/artifacts/rfc-0170-p0-build/`.
Verification: strict verifier `builtin:go-build`/`builtin:go-vet` passed with
agreement; `builtin:go-test` passed as ASSERTED with exit 0; `make lint` passed;
`make typecheck` passed with live MCP endpoint env stripped; `make check-docs`
passed; live two-role PG tests for runtime migration apply and forbidden owner
FK passed under `STRIATUM_PG_TEST_URL=postgres://halbritt@/postgres?host=/var/run/postgresql`.
`striatum doctor --json` is green.

Issue #615 can close for the P0 design→build→verify tracer. P1 remains tracked
by #618 (whole-tree frozen citation exactness) and #619 (non-cooperative
filesystem-hang cull-slot fence). Do not launch Claude lanes before
2026-06-30 15:59 UTC unless credentials/credits are explicitly restored; use
Codex-only lane sets or wait.

## 2026-06-27 delta — RFC 0168 design accepted; v6 run wedge cleared

RFC 0168 P0 (per-lane pooled OS uid as lane security principal) is accepted
with follow-up as **D272** after the v6 falsification gate converged. The
decision artifact is
`docs/operator/artifacts/rfc-0168-design-v6/decision/DECISION_override_to_build.md`.
The v6 run `run_010c81ec8ca17ffd182e0bd7be3f28cc` was canceled after D272 to
clear the Claude spend-limit / stale daemon-socket wedge and its stale
adjudicate worktree row was retired through `recovery quarantine-lane`.
`striatum doctor --json` is green (`ok=true`, no problems, no stale leases).

Next RFC 0168 stage is BUILD from the v6 HOLDER spec plus D272's binding
constraint: the OQ4.1.2 coverage-gap gate must discriminate provider-owned
credential selectors from ordinary lane env selectors, keeping the typed
provider credential-dir refusal while proving legitimate in-repo non-credential
lane env still launches. Do not launch Claude lanes before 2026-06-30
15:59 UTC unless credentials/credits are explicitly restored; use a Codex-only
lane set or wait.

## 2026-06-26 delta — active RFC 0170 v2 false-red barrier diagnostic fixed

Fresh RFC 0170 P0 design v2 is live as
`run_3506471695cec27400eda2f3f33d4f6f` on
`striatum/rfc-0170-p0-design-v2`; the holder lane is alive and revising the
cycle-1 SPEC. `doctor` went red while the holder was still running because
`barrier_status` counted the downstream `falsifier_1`/`falsifier_2`
`jobs.state='blocked'` rows as hard `BARRIER_BLOCKED` seats. That state is also
the scheduler's ordinary pre-queue state for dependency-blocked jobs, so the
barrier was only pending on the live holder, not intervention-blocked.

**Fixed in this change:** the doctor barrier invariant and blocked manifest now
normalize dependency-blocked seats to `PENDING`; a blocked seat is a hard barrier
blocker only when its own dependencies are satisfied or it carries an open
blocking/human-checkpoint blocker. `doctor` and `join verify` normalize the old
deployed view shape so upgraded databases stop false-reddening without rewriting
an applied DDL migration. Verification: `make -C go build`,
`STRIATUM_PG_TEST_URL=postgres:///postgres go test ./pkg/reads -count=1`, and
`STRIATUM_PG_TEST_URL=postgres:///postgres go test ./pkg/db -count=1` pass; the
full `make -C go test` target also passes when the live Striatum daemon runtime
environment is stripped from the test process.

## 2026-06-26 delta — SEV-1 runner outage fixed (rowByID SELECT * vs bundle 0022)

The v2.38.0 restart (2026-06-25 19:17 UTC) silently took the whole workflow
engine **down for ~12h**: every run-scoped mutation returned
`permission denied for table runs (SQLSTATE 42501)` and **zero events** were
recorded. Root cause: owner bundle 0022 (RFC 0167 P0) REVOKEs table-level SELECT
on `striatumd.runs` from `striatumd_rw` and re-GRANTs all columns except
`created_by_principal_id`, but the mutation surface's shared `mutations.rowByID`
helper still issued `SELECT *` (≈55 run-load call sites). The `reads` package had
been migrated to explicit projections; `mutations` was missed, and the verify
pgtests run as the owner pool so the runtime-role grant never bit in CI.

**Fixed + deployed (this session):** `rowByID` projects explicit runs columns
(commit `82bb94c2`); hermetic + two-role-grant regression guards added;
`2c6add5a` fixes a test matcher. System daemon rebuilt/restarted 2026-06-26
07:08 UTC — mutations succeed, events flow, recovery sweep active. Post-mortem:
**#614** (open for the prevention follow-up: run mutations pgtests under the
two-role grant). `doctor ok=true` after clearing the 5 pre-existing integrity
problems via daemon paths (worktree anchor, accept-quarantined + force-release,
and acknowledged-loss entries for two branch-cleanup-orphaned cc_rfc0142_p0
process artifacts). VERSION stayed **v2.38.0** for that same-day bugfix window;
v2.39.0 later packaged it with the RFC 0170 P0 migration. **RFC 0170
design→build→verify (the session's original mission) was NOT started** — the
outage took precedence.

## 2026-06-26 delta — RFC 0170 P0 design cycle-1 + a stranded-barrier runner defect (#616)

Drove RFC 0170 P0 (self-culling repo) into a `falsification_gate` design run
(scaffold landed `13fd54fd`; issue **#615**; roadmap 14b). **Cycle-1 ran fully to
a rigorous `needs_revision`** (`run_85afe0ff`): G3 substrate (migration
`0045_cullable_entity.sql` + read/write authority-inventory + no `SELECT *`) and
G4 forward-compat MET; **G1 + G2 unmet** with two precise binding constraints —
**G1'** reconcile the supersession predicate so a still-live-cited superseded RFC
(`rfc:0097`, cited by 0101/0103) isn't nominated, **G2'** add a sub-cadence
cull-fold deadline + HANG regression test so a blocked `DecayTickSweep` can't
stall the recovery goroutine. v1 dialogue snapshot `/tmp/rfc0170-v1-snapshot/`.

**Friction resolved by #616:** the `#587` cycle routed `needs_revision` to a
futile falsifier re-attack; I `run cancel`-ed it **mid-strict-fanin-re-stage**,
which stranded the fan-in barrier (`barrier_committed_manifest_mismatch` +
`strict_fanin_required_seat_unrecoverable`) against a canceled run and left the
dead seat's worktree row pointing at an already-deregistered orphan directory.
Substantively benign (artifacts durably anchored), but the old daemon kept
`doctor` red. **Fixed + deployed 2026-06-26:** terminal-run fan-in barrier
debris now reports as `barrier_debris_terminal_run` warnings, not ok-reddening
problems, and `recovery quarantine-lane` retires already-unregistered worktree
rows. Live recovery of
`job_run_85afe0ffd067616db27edf0a3c4e4afa_falsifier_2` returned
`already_removed=true` / `worktree_cleanup=already_unregistered`; `striatum
doctor --json` is `ok=true`, `problem_count=0`. **Lesson: never `run cancel` a
strict fan-in mid-revision-cycle — let it reach `needs_operator`.** **Next:**
fresh `-v2` revising holder discharges G1'/G2' → D271 → BUILD→VERIFY.

## 2026-06-25 delta — v2.38.0 release

**v2.38.0 (2026-06-25)** cuts the D270 subtraction release over v2.37.1.
The supported cross-repo product surface is retired: `go/pkg/crossrepo`,
`cross_repo.*` daemon RPC methods, `striatum cross-repo ...` CLI routes,
daemon handlers, generated method tables, and current reference-doc support
are removed. RFC 0128's single-repo write-scope guardrail remains.

**Deploy note:** this release has no new DDL, runtime migration, or owner bundle.
Upgraded databases may still contain historical RFC 0032 `cross_repo_*` tables
and `runs.cross_repo_run_id`; those are compatibility/provenance schema only.
Install/restart the v2.38.0 binaries to move the host from v2.37.1 to v2.38.0.

## 2026-06-24 delta — v2.37.0 / v2.37.1 (superseded by v2.38.0)

**v2.37.0** cut the post-v2.36.0 source state: RFC 0167 P0 operator
identity/run attribution + session-bound `operator.bootstrap` + owner bundle
**0022**, RFC 0143 Slice A recovery legibility, the D269/#527 fan-in
default-live cutover, the D264-D269 audit closeout gates, and the
`scripts/check_release_version.py` README/VERSION/CHANGELOG agreement gate.
**v2.37.1** hotfixed a deploy skew where the 0022
`operator_identity_run_attribution` read-projection stamp was in
`readScopeReasserts` but missing from `SupportedAuthorityCapabilities()`, so the
daemon refused the schema after the DDL applied; v2.37.1 adds the capability +
a parity guard. (Note: bundle 0022's runs SELECT-revoke later surfaced the
SEV-1 `rowByID` outage above — see the 2026-06-26 delta / #614.)

## 2026-06-24 delta — audit closeout gates

The 2026-06-24 architecture-audit closeout added D264-D269. Operators must not
launch new feature-wave RFC design/build work while `striatum doctor` is red.
Use direct sync-guarded operator commits, not daemon dogfood flows, for narrow
source/truth fixes until integrity is green again. `docs/operator/rfc-roadmap.md`
now carries the active WIP cap, self-hosting-tax classification, and
subtraction-release checklist. D269 closes the #527 source cutover with PG/unit
proof: fan-in is live by default, `STRIATUM_BARRIER_FANIN=0` is the kill switch,
and live deployment equivalence is now the post-green validation path.

## 2026-06-24 delta — RFC 0167 P0 built + verified + integrated

RFC 0167 P0 (operator identity & run attribution, D260/D263) is **on `main`**
(`525c4696`), landed autonomously through Striatum's own design → build → verify
workflows. **Design:** a `falsification_gate` committee ran **4 cycles**, each
surfacing a real, source-verified, build-breaking defect (pre-run session
impossible under `sessions.run_id NOT NULL`; the run-origin stamp hitting the
0006 token read-scope `42501`; the operator token lacking `run.prepare` admin;
the operator-session token over-granting + a composed `client_id→principal_id`
re-leak) before clearing `accept_with_findings` with two binding §F constraints.
**Build:** a `code_change` run implemented all ten §9 items — owner bundle
**0022** (`operator_handles` + `operator_sessions` + `runs.created_by_principal_id`
/`created_by_handle_id` write-once trigger + the SECURITY DEFINER identity
projections `run_origin_identity`/`runs_for_origin_client`/`runs_missing_origin`
+ the `runs` REVOKE/re-GRANT + `operator_handles`/`operator_sessions`
column-scoped grants + the three `runs` star-reader conversions),
`mintOperatorSessionToken`, the `operator.bootstrap`/`heartbeat`/`close` RPCs
with the `striatum operator bootstrap` CLI as their client, `striatum whose`,
`status --mine`, and the `attribution_unknown` doctor advisory. **Verify:** all
**10 live two-role pgtests PASS** under the non-superuser owner DSN (the C2″
composed-route closure, the write-once trigger, the two-`maya` disambiguation,
the operator-token authorization, the drift reassert); `go build`/`go vet` green.

Bundle 0022's deployable release is v2.37.1. Apply it only with a binary that
declares `operator_identity_run_attribution` in
`SupportedAuthorityCapabilities()`, then restart the daemon; after that, `whose`,
`status --mine`, and the operator-bootstrap mint RPC are live. **P1–P3** (custody
log; honest bylines + handoff naming + chips + opt-in OSC title; lineage) are
sequenced behind this P0 release/deploy.

## Older deltas (≤ v2.37 — superseded; see CHANGELOG + decision log)

- **v2.37.0 / v2.37.1** (superseded by v2.38.0): RFC 0167 P0 operator
  identity/run attribution + owner bundle **0022**, RFC 0143 Slice A, the
  D269/#527 fan-in default-live cutover, the D264-D269 audit closeout. v2.37.1
  added the `operator_identity_run_attribution` capability + a parity guard.
  (Bundle 0022's `runs` SELECT-revoke later surfaced the SEV-1 `rowByID` outage
  above — #614.)
- **RFC 0167 P0** (D260/D263) is on `main` and deployed via bundle 0022;
  **P1-P3** (custody log / honest bylines + handoff naming / lineage) are
  sequenced — roadmap row 21, issues #609/#610/#611.
- **Audit closeout D264-D269**: the WIP cap, self-hosting-tax classification, and
  subtraction-release checklist live in `docs/operator/rfc-roadmap.md`.
- **v2.34-v2.36** releases, the owner-bundle 0020 watermark hotfix (#581), RFC
  0165 v2 quarantine (#583), #311 per-job quarantine (D209), and the 2026-06-16
  reliability-reset closeout are historical — see `CHANGELOG.md` and
  `docs/decisions/decision-log.md`.

## State

Latest release is **v2.39.0 (2026-06-27)** — ships runtime schema 45 with the
RFC 0170 P0 observe-only culling substrate, RFC 0168 P0 design acceptance, and
the post-v2.38.0 reliability fixes. **v2.38.0 (2026-06-25)** retired the D270
cross-repo product surface without new DDL while preserving the single-repo
write-scope guardrail. **v2.37.1 (2026-06-24)** hotfixed the v2.37.0 owner-bundle 0022
capability-parity deploy skew after shipping operator identity/run attribution
(RFC 0167 P0, owner bundle 0022), session-bound operator bootstrap, RFC 0143
Slice A recovery legibility, D269 fan-in barrier default-live cutover, and the
audit closeout gates. **v2.36.0 (2026-06-23)** was a bugfix-only cut over
v2.35.0 (#581 owner-bundle watermark deploy crash-loop, #582 release publish
pipeline, doctor superseded-artifact false-red, checkpoint artifact-integrity;
owner bundle 0020).
**v2.34.1 (2026-06-18)** was a docs/maintenance cut (no code change). **v2.34.0
(2026-06-18)** packaged six reliability/security fixes +
the RFC 0135 sealed-barrier primitive (opt-in/shadow). The earlier
**v2.33.0 (2026-06-16)** tag at `564a8209` packaged the post-v2.32.0 landing set:
doctor integrity legibility **P0+P1** (D204/D205, #300), **#290/D206** fan-in
run-branch integration, **#296** codex push-lane loud-fallback, **#301/#307**
workflowgenerate fixes, **#304** dangling-blocker resolution, and **#311**
recovery-escalation legibility (details in CHANGELOG `v2.33.0`). The prior
**v2.32.0** (2026-06-13) packaged RFC 0118/0119/0120 plus session-recovery edge
fixes; the v2.10.0 → v2.31.0 release burst is indexed in CHANGELOG and the
decision log. Historical open sets such as #212/#263-#267 are no longer current.

## Deep architecture review 2026-06-11 — mostly closed in v2.34.0

`STRIATUM_DEEP_ARCHITECTURE_REVIEW_CLAUDE_FABLE_5_2026-06-11.md` (`0e8671ed`):
verdict **ROUGHLY RIGHT-SIZED · ON TRACK**. v2.34.0 closed the ranked blocker,
untested spine, scoped deletion pass, conformance honesty, truth mechanization,
and boundary-hygiene batch. **STILL OPEN:** P1 token-out-of-argv and
`docs/operator/` exhaust relocation (#364, ready-for-human).

## Current Frontier

- **Reliability reset gate is green enough for release.** `striatum doctor` is
  green and `main` CI passed at `3b4ef294`; keep the remaining recovery defects
  ahead of feature growth and do not launch new dogfood runs on a red doctor.
- **Recovery fixture gate:** #302/#308/#309 are closed, but their failure class
  remains load-bearing evidence: prove `agent_exited_unsealed` plus durable,
  valid artifacts reaches completion without renewed-lease waiting, then keep
  `striatum doctor --json` green.
- **Docs/current-state truth:** this brief, README status, docs index, and
  roadmap/todo references must share one current issue frontier. This revision
  updates the brief/README/index; roadmap/todo guardrails remain follow-up work.
- **RFC 0120 (await-packet idle exit + wake boundary, D180) — LANDED.**
  Phase 1 terminal idle envelopes carry `idle_behavior=exit_session`;
  bootstrap no longer tells lanes to poll after `no_work`; the PTY receiver
  exits the lane cleanly. Phase 2 landed on main in `81b51959`: the
  notify-only wake bus adds read-shaped `wake.wait`, post-commit wake hints
  for work/message/turn availability, and `run drive` wake waits with bounded
  missed-notification fallback. Wake events stay hints over committed state,
  never authoritative. The earlier `issue-248-wake-bus-implementation` runs
  were canceled/superseded dogfood attempts; do not drive them as live work.
- **RFC 0119 (warm-tier memory boundary, D179) — accepted; hot tier
  implemented.** Authorizes the `hippo`/`fornix` warm-tier adjunct (separate
  repo, `~/git/hippo`) + a striatum-native read-only hot tier (`recall.*`
  over the daemon's own artifact stream, scaffold-time digest injection,
  default-off redacted `lane_trajectory` export, `progress_note`-only git
  eviction). The hot tier shipped (`recall.*`, `RecallMemory`, commit
  `80dc82e7`) with C1-C4 discharged; only the runtime evictor (D193) remains
  deferred. No
  `memory.*` capability, no retrieval-dependent state transition.
- **RFC 0118 (#240)** implementation is on main and the issue is closed:
  frozen verdict provenance stamps, override posture/basis, completion
  provenance gate + `needs_operator` escalation, durable
  `run_completion_record`, and `recovery.invalidate_job` supersede receipts.
  The accumulated post-v2.31.0 work shipped in v2.32.0.
- **Live housekeeping:** `doctor` is OK (0 problems) but still warns that
  the local Codex config points at a stale MCP endpoint unless launched
  through `striatum codex`. The worktree-ref-safety/run-drive residue in
  #259/#260/#261, the config crash-loop recovery in #262, and the
  blob-gated artifact-anchor doctor check in #217 are closed on `main`.

## Next Actions

1. **Keep current-state docs truthful:** after every issue-closeout or release,
   refresh this brief, README status, docs index summaries, and any roadmap/todo
   surface that claims to list current open work; the README version row is now
   mechanically gated by `make check-docs`.
2. **Work the active defect frontier first:** #612, #579, #576, #512, and #506
   are the current operator-facing recovery defects.
3. **Bound doctor warnings:** keep `problem_count=0`, but turn the warning channel
   into named classes with allowed baselines/deltas.

## Blockers / Open Issues (24)

Open GitHub tracker state rechecked on 2026-06-27 before the v2.39.0 release.

- **Active defects / recovery:** #620 adjudicate ledger published but not
  anchored, #617 running-run barrier mismatch variant, #612 cross-user
  falsifier handoff publish wedge, #579 idle-stalled builder lane blocks
  downstream jobs, #576 lease-warmed lane never completes, #512 boot-epoch
  rotation reseal Slice B now unblocked by RFC 0168, #506 reviewer
  over-rejection/blob-exhaust legibility.
- **Reliability/security follow-ups:** #592 RFC 0142 P4 activation/verify run,
  #590 gate-compute timing, #589 structural-root precheck, #588 falsification
  recursion tripwire, #587 auto-bank/rescaffold clean revision cycles, #585 RFC
  0143 Slice B build now unblocked by per-lane security principal.
- **Feature/design backlog:** #619/#618 RFC 0170 P1 follow-ups, #611/#610/#609
  RFC 0167 P3/P2/P1, #578 schema-drift refuse-to-serve flip, #577 verified-stale
  rung, #572 RFC 0142 P5 rehearsal receipt, #569 provider-auth
  absence-of-success alerting, #387 events/audit-log partitioning, #380
  remaining git-hoist lock holders.

## Hazards / Do Not

- **Operators scaffold dogfoods; they do not implement role artifacts.**
- **Hold the anti-bets** (review §F.4 + decision log): no new shapes while
  the RFC 0106 freeze holds; no daemon auto-spawn before the D175 evidence
  trigger; no Engram/memory absorption (D179 boundary is narrow and
  test-gated); no hosted/multi-tenant anything.
- Stay on the daemon boundary: no direct Postgres, no tmux/marker-file
  state, no telemetry/transcript capture without a product decision.
- Trust only returned JSON; verify every state-changer with a read;
  state-changing calls sequential.
- The daemon runs as the **system** unit (`/etc/systemd/system/striatumd.service`);
  restart with `sudo systemctl restart striatumd` (NOT `--user`). `make install`
  does NOT restart it; build from a clean worktree off `origin/main` (not the
  contended shared tree), then verify the running `/proc/<pid>/exe` sha.
- **The user-scope `striatumd.service` is masked/removed — do NOT recreate it via
  `striatum daemon install`.** It lacks the owner-DB env (`STRIATUM_OWNER_DB_URL`
  …) so it crash-loops on `daemon_pg_owner_bootstrap_failed` and conflicts with
  the system unit (recurring daemon-down incidents). Fix the install path before
  un-masking — see #509.
- CI always runs pgtests; check `gh run list` before assuming green.
  Reproduce lint locally with golangci-lint v2.12.2 (pinned in
  `go/Makefile`; absent binary = invisible red).
- Concurrent agents sweep the shared tree: commit deliverables same-turn;
  land code via isolated worktrees off `origin/main`.

## Pointers

- `STRIATUM_DEEP_ARCHITECTURE_REVIEW_CLAUDE_FABLE_5_2026-06-11.md` — the
  standing work-list (§E missing pieces, §G recommendations)
- `docs/rfcs/0118-gate-run-completion-on-attested-provenance.md`
- `docs/rfcs/0119-warm-tier-memory-boundary.md` (+ hippo RFC 0001)
- `docs/rfcs/0120-await-packet-idle-exit-and-wake-boundary.md`
- `docs/rfcs/0116-zero-operator-touch-dag.md` / `0117-worktree-branch-ref-safety.md`
- `docs/decisions/decision-log.md` (D161–D181 cover this brief's span)
- `CHANGELOG.md` (v2.10.0 → v2.39.0 + Unreleased)
- `docs/reference/command-authority-matrix.md` (lags 16 live methods —
  reconcile on contact, per AGENTS rule)
