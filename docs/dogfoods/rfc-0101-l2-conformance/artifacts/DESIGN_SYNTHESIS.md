---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
title: "RFC 0101 Layer 2 — Adapter-Conformance Harness + Lane-Env Hardening (synthesis)"
author: synthesizer-claude-opus-4.8-003
lane: claude_code
status: design-synthesis
inputs:
  - "docs/dogfoods/rfc-0101-l2-conformance/artifacts/design/claude_code/DESIGN.md"
  - "docs/dogfoods/rfc-0101-l2-conformance/artifacts/design/codex/DESIGN.md"
---

# RFC 0101 Layer 2 — Adapter-Conformance Synthesis

Reconciliation of the `claude_code` and `codex` design proposals into one
buildable design. The two proposals agree on the spine (daemon-observable
contract, real daemon over pgtest, named failure taxonomy, deterministic
lane-env hardening, non-rotting agy skip). They differ in clause granularity,
the #70 token-relocation mechanism, and naming. Where they converge I state the
decision; where they conflict I pick one and say why (§2); genuinely unresolved
tensions go to the panel (§3). Source paths below were read against the live
tree, not the proposals.

## Revision note (attempt 3)

This is the third synthesis attempt. The attempt-2 spine is carried forward
**unchanged** — every reviewer credited the daemon-backed contract, the
source-grounded deadlines, the real-daemon-over-stub decision, and the
data-ledger skip, and explicitly asked me **not** to re-architect them. Attempt
3 re-grounds the source references against the live tree, makes the
devil's-advocate findings' closures auditable for a fast re-review, and hardens
the two residuals most exposed to the next attack.

**Review-panel state going into attempt 3:**

- **`reviewer-claude-opus-4.8-002` (ergonomics_dx): `accept`** of attempt 2 —
  all of R1–R4 (local-mode scoping, the human remediation surface, the
  `DEADLINE_SCALE` default, the env-prefix family) are resolved and carried
  forward verbatim (§1.1, §1.3, §1.6, §2.3, §4).
- **`reviewer-claude-opus-4.8-001` (devils_advocate): `needs_revision`
  (R1–R3)** — written against **attempt 1** (it cites attempt 1's OQ1–OQ5 and
  the verbatim attempt-1 lines *"a missing or unauthenticated declared adapter
  is a hard failure, never a skip"* + *"infra-only retries — never retry a
  protocol stall"*, the exact rules attempt 2 already refined). All three
  findings are **closed in the spine carried into attempt 3**:

  | Devil's-advocate finding | Closed in | Verify via |
  |---|---|---|
  | **R1** — provider availability imported as a hard merge gate → false-red whose realistic mitigation is a false-green | **§2.7** pre-flight provider/auth gate (C1) emitting *infra* outcomes (not contract `FailureClass`es) + post-green bounded retry + outcome-distinguished gating (exit `1` contract / `75` provider-transient / `0` pass); taxonomy split into contract-vs-infra classes (§1.2) | acceptance gate (6): a post-green 429 → bounded retry → `AdapterProviderUnavailable` exit 75 (neutral), while a genuine post-green stall → contract exit 1 |
  | **R2** — "C0 is the #101 fix" overstates an always-run golden; Tier B gating/CLI-pinning semantics unspecified | **§1.1 "C0 scope"** + **§2.8**: C0 = our-code golden (catches a Striatum revert to `pty_submit`); the **#101 class is caught by Tier B live C3/C4**; the Tier B job is required-to-merge + per-PR; unpinned-CLI detection-latency stated | acceptance gates (1) and (1b): C0 fails on our revert; a CLI that does not submit its bootstrap fails Tier B **C3** `BootstrapStall` |
  | **R3** — OQ5's auth soft-spot should be closed by R1's probe, not left open | **OQ5 (resolved)** folded into the §2.7 pre-flight auth probe → `AdapterUnauthenticated` fast, not a misleading downstream `BootstrapStall`/`AwaitPacketStall` | §2.7 step 1; OQ5 disposition |

  The devil's advocate explicitly asked that the spine (daemon-backed contract,
  source-grounded deadlines, data-ledger skip) **stay** — it does.

**Attempt-3 hardenings (surgical, no spine change):**

- §0 / §1.4 — re-grounded the live-tree references and named the **exact** C2
  env surface: `supervisedEnvPassThrough` filtered by
  `supervisedEnvAllowlistKeys` then merged with `supervisedEnvEntries`
  (`supervision_control.go:2315/2339/2381`); the builder comment already states
  *"everything else — including every *DSN*/*POSTGRES*/PG*/DATABASE_URL var — is
  dropped."*
- §2.7 — the bounded retry's **cost is now stated, not hidden**: a genuine
  #101-class regression (green pre-flight, then a broken bootstrap submission)
  costs one extra live turn before it reports contract-red — a bounded 2× on the
  worst-case failing path, paid only on the rare failing path and never twice.
- OQ6(a) — sharpened from fully-open toward a named candidate: a
  **last-green-proof record + staleness budget**, so a *sustained* provider
  outage cannot leave the per-PR live signal indefinitely "absent-but-loud."
  Left as OQ6 for the panel to set the budget and decide adopt-vs-defer.

The OQs the reviewers credited as correctly-deferred (OQ1–OQ4) are unchanged;
OQ5 is resolved; OQ6 carries the two gating residuals (now with a candidate
disposition for (a)).

## 0. Scope

Layer 2 is **detection + prevention**: a CLI bump that breaks bootstrap, the
turn loop, or lane-env hardening must **fail CI against the actually-installed
binary** instead of wedging a live run (the #101 shape — claude_code v2.1.x
stopped submitting its PTY-typed bootstrap and the control surface read
healthy). Layer 3 (automated recovery) is out of scope, so the taxonomy (§1.2)
must be precise enough to *later* route a recovery action. The harness reuses
the existing daemon liveness substrate — it does not invent a parallel truth.

Grounding (read in tree):
- `go/pkg/agentloop/loop.go:148` — `bootstrapDeliveryModeFor` returns `argv` for
  `claude`/`codex`/`agy`, `pty_submit` otherwise; `appendBootstrapArgv` (agy via
  `--prompt-interactive`/`-i`, codex/claude positional). The #101 fix lives here.
- `go/pkg/sessionliveness/liveness.go` — protocol columns (`last_tools_list_at`,
  `last_await_packet_at`, `last_ack_at`, `last_work_heartbeat_at`,
  `last_work_complete_at`), `DefaultPolicy()` (Discovery 60s / AwaitPacket 90s /
  Ack 60s / LeaseHeartbeat 300s / ProtocolIdle 300s), and `Classify()` stall
  classes (`StallDiscovery`, `StallAwaitPacket`, `StallAck`,
  `StallLeaseHeartbeat`, `StallQuestionPending`, `StallProtocolIdle`).
- `go/pkg/mcp/tools.go:29` — where `last_tools_list_at` is stamped (`recordActivity`).
- `go/pkg/agentloop/mcpconfig.go:75` — `writeEphemeralGeminiSettings`
  (`usageStatisticsEnabled:false` for #76; bearer-in-repo for agy, #70). The
  comment confirms agy reads `<cwd>/.gemini/settings.json` **or `~/.gemini/`**
  and has no out-of-repo settings flag (D150); codex reads `STRIATUM_MCP_TOKEN`,
  claude uses an ephemeral `--mcp-config` under `.striatum/scratch`.
- `go/pkg/mutations/supervision_control.go:2315` — `supervisedEnv` =
  `supervisedEnvPassThrough(os.Environ())` (allowlist filter
  `supervisedEnvAllowlistKeys`, :2339, #87) merged with `supervisedEnvEntries`;
  the builder comment (:2314) states *"everything else — including every
  *DSN*/*POSTGRES*/PG*/DATABASE_URL var — is dropped."* `HandleSuperviseStart`
  (:110) also wires `CleanupGeminiSettings` at the terminal-state transition.

---

## 1. Converged design

### 1.1 Conformance contract — one ordered clause list

The contract is an **ordered** per-adapter list. Each clause asserts a
**daemon-observable signal**, never PTY bytes; ordering matters because a later
clause is only meaningful once the earlier one fired (you cannot heartbeat a
lease you never claimed). Live-clause deadlines are **derived from
`sessionliveness.DefaultPolicy()`** so the harness and the production
`Classify()` never drift, scaled by an env-overridable
`STRIATUM_CONFORMANCE_DEADLINE_SCALE` (**default `3.0`** — chosen so the live
multiplier comfortably absorbs model/PTY latency while staying well under a CI
job timeout; e.g. C3's 60s discovery becomes a 180s harness deadline).

A clause's outcome is one of **pass**, **contract-fail** (emits a contract
`FailureClass`, §1.2), or — for the pre-flight gate C1 only — **infra-outcome**
(emits an *infra* class that gates whether the live clauses run and is **not** a
contract failure; see §2.7). This three-way distinction is the spine of the R1
fix: "the adapter is genuinely non-conformant" and "we could not run the
contract because the binary/provider/auth was unavailable" are different events
with different gating.

| # | Clause | Daemon-side assertion | Source signal | Class on failure |
|---|--------|----------------------|---------------|---------------|
| **C0** | **AdapterContract** (hermetic, no CLI) | `bootstrapDeliveryModeFor(cmd)` returns the adapter's declared mode (claude→argv positional; codex→argv positional + `-c mcp_servers.striatum.url`; agy→argv `--prompt-interactive`); for agy the generated settings body carries `usageStatisticsEnabled:false` | golden over `loop.go`/`mcpconfig.go` | `AdapterContractViolation` (contract) |
| **C1** | **AdapterResolved + ProviderReady** (pre-flight gate) | configured binary resolves on the *supervised* PATH; `--version` captured into the report; **plus a cheap health/auth probe** (§2.7) proving the adapter is present, authenticated, and reachable *before* a turn is burned | supervise PATH + version probe + adapter health/auth probe | **infra**: `AdapterUnavailable` / `AdapterVersionUnknown` / `AdapterUnauthenticated` / `AdapterProviderUnavailable` |
| **C2** | **LaneEnvHardened** | child env from the production `supervise.start` path carries the required keys (`PATH`,`HOME`,`STRIATUM_MCP_URL`, token material, run/session/supervisor/repo/lane ids) and **none** of the banned keys (`DATABASE_URL`, `PG*`, `STRIATUM_POSTGRES_*`, daemon secrets) | env snapshot vs allowlist (`supervision_control.go`) | `EnvSecretLeak` (contract) |
| **C3** | **BootstrapDelivered** | `sessions.last_tools_list_at` becomes non-null within `DiscoverySeconds` of `agent_started` | `LastToolsListAt` (`mcp/tools.go:29`) | `BootstrapStall` (agy refinement `SurveyPromptBlocked`) (contract) |
| **C4** | **AwaitPacketForeground** | `last_await_packet_at` advances after C3 within `AwaitPacketSeconds` **and** the returned packet creates the active lease for the expected session/job | `LastAwaitPacketAt` + lease row | `AwaitPacketStall` / `DiscoveryProbeStall` (#85) (contract) |
| **C5** | **PacketAcked** | seeded job transitions to `acked`/`claimed`; `last_ack_at` set within `AckSeconds` | job state + `LastAckAt` | `AckStall` (contract) |
| **C6** | **HeartbeatObserved** | `last_work_heartbeat_at` advances ≥1× while the job is open | `LastWorkHeartbeatAt` | `HeartbeatMissed` (contract) |
| **C7** | **InterrogationRoundTrip** | harness opens 1 interrogation; lane receives `interrogation_question`, answers via `interrogation.answer`; `StallQuestionPending` never fires before `DeadlineQuestionPending` | `interrogation.*` + stall non-fire | `InterrogationIgnored` (contract) |
| **C8** | **ArtifactPublished** | expected artifact row exists, blob present, front matter valid (publisher exit ≠ 6); path/hash/`logical_name`/kind/byline verified via daemon state | `artifacts` + `artifactcontracts` | `ArtifactMissing` / `ArtifactRejected{reason}` (contract) |
| **C9** | **WorkCompleted** | job state `completed`; `last_work_complete_at` set | `LastWorkCompleteAt` | `CompleteMissing` (contract) |
| **C10** | **ReceiveLoopContinues** | next `work.await_packet` returns `no_work`; `Classify()` == live/idle with no `ProtocolIdle` stall raised in grace | await result + `Classify()` | `LoopWedged` / `NoWorkLoopMissed` (contract) |
| **C11** | **NoPrematureExit** | for Full adapters the lane process stays alive through C10; agy may exit after C9 **only** under the #95 known-gap record | helper `agent_exited` event + exit code | `TurnExitEarly` (contract) |
| **C12** | **NoWorkTreeCredentialLeak** | after launch / active / complete / **teardown**, the target repo (tracked + untracked, outside `.striatum/scratch/`) contains zero: (i) literal test bearer, (ii) `.gemini/settings.json` / `mcpServers.striatum` residue, (iii) lane-authored control-plane helper (`scripts/striatum_client.py`, etc.) | filesystem + git | `TokenLeakInWorkTree` / `ControlPlaneHelperInWorkTree` (contract) |

**C0 scope (R2 correction).** C0 is a hermetic golden over **our own**
`bootstrapDeliveryModeFor`/`appendBootstrapArgv`/settings construction. It
catches a *Striatum* refactor that reverts claude to `pty_submit` or drops the
agy survey key — with no live CLI. It does **not**, and cannot, catch a
behavioral regression in someone else's binary: **#101 was a CLI behavior change
(claude_code v2.1.x stopped submitting its argv/PTY bootstrap), not a change to
our code**, so a golden over our code would have stayed green through #101. **The
#101 class is caught by Tier B live C3/C4** (no `last_tools_list_at` / no
`last_await_packet_at` against the actually-installed binary). C0 and C3/C4 are
complementary, cheap, and catch different regressions.

C1 is the **pre-flight gate**: it decides whether C2–C12 run at all and, on
failure, emits an *infra* outcome (not a contract failure). C2 and C12 are
codex's lane-env clauses elevated to first-class ordered status; C10 split into
wedged-vs-never-re-awaited keeps the two #85-adjacent endings distinguishable.

**Execution tiers** (which clauses run where — the most important anti-rot
control, from claude_code's framing):

- **Tier A — hermetic, always runs, never skips** (rides inside `make -C go
  check` on every PR): C0 for every adapter, plus the harness self-tests
  (skip-ledger integrity, taxonomy self-validation via the fake agent §1.3, the
  C2 env-allowlist unit test, the #76 settings golden). No live CLI dependency.
- **Tier B — live adapter** (`make adapter-conformance`): C1–C12 against the
  installed CLIs. Gating is **mode-scoped** (§1.6, §2.7, §2.8): under `--ci` a
  *missing* declared adapter binary is a hard failure; a *contract* failure is
  always a hard failure; a *provider/auth* infra outcome is loud-but-neutral
  (re-runnable) per-PR and hard only in release mode; without `--ci` (local),
  absent/unauthenticated adapters are reported, not failed.

### 1.2 Failure taxonomy — closed, named set (contract vs infra)

Each failed clause emits **exactly one** primary class **plus** structured
fields (codex): `adapter`, `binary_path`, `version`, `clause`, `deadline`,
`last_protocol_event`, `supervisor_state`, `active_lease_id`, `agent_exit_code`,
and `known_gap_id` when applicable. The class routes the L3 *category*; the
fields route the *specific* recovery (see OQ3, §3). **Classes are partitioned
into _contract_ classes (a genuine non-conformance — hard-blocks) and _infra_
classes (the test could not be run — gated per §2.7).** This partition is the
R1 fix surfaced in the taxonomy.

**Infra classes (pre-flight C1; not contract failures):**

| Infra class | Fires when | Reads | Gating / L3 |
|---|---|---|---|
| `AdapterUnavailable` | C1: declared binary absent on supervised PATH | PATH probe | hard fail in `--ci` (runner-config defect); reported (not failed) locally |
| `AdapterVersionUnknown` | C1: binary present, version uncapturable | version probe | warn locally; hard in release |
| `AdapterUnauthenticated` | C1: present but health/auth probe shows no usable creds | adapter auth probe | re-runnable-neutral per-PR (fix the secret); hard in release; **resolves OQ5** |
| `AdapterProviderUnavailable` | C1 (or a post-green retry, §2.7): reachable but provider erroring/429/outage | adapter health probe + retry | **retryable, non-conformance-neutral**; loud per-PR (`EX_TEMPFAIL`/75 → re-run); hard in release |

**Contract classes (C0, C2–C12; a genuine non-conformance — hard-blocks):**

| FailureClass | Fires when | Reads | Likely L3 recovery |
|---|---|---|---|
| `AdapterContractViolation` | C0 construction mismatch | golden | code fix (blocks merge) |
| `EnvSecretLeak` | C2: banned DSN/secret reaches child | env snapshot | scrub spawn env |
| `BootstrapStall` | C3: no `last_tools_list_at` by deadline, no progress bytes (after a green C1) | `StallDiscovery` | re-deliver/argv-fallback bootstrap |
| `SurveyPromptBlocked` | C3 (agy): alive, survey-suppressed, yet no progress | `StallDiscovery` + alive | launch/config fix (not prompt) |
| `DiscoveryProbeStall` | C4: tools/list + progress bytes but no foreground claim | `StallDiscovery`/`StallAwaitPacket` + progress>0 (#85) | kill probe / re-prompt receive loop |
| `AwaitPacketStall` | C4: tools/list but no await at all | `StallAwaitPacket` | re-prompt receive loop |
| `AckStall` | C5: packet delivered, never acked | `StallAck` | requeue packet |
| `HeartbeatMissed` | C6: lease held, heartbeat stops | `StallLeaseHeartbeat` | requeue stale lease |
| `InterrogationIgnored` | C7: question pending past deadline | `StallQuestionPending` | re-ask / escalate |
| `ArtifactMissing` | C8: no artifact row/blob | `artifacts` | re-request publish |
| `ArtifactRejected{reason}` | C8: publish rejected — `front_matter`(exit 6)/`write_scope`/`byline`/`hash` | `artifactcontracts` | bounce to author |
| `CompleteMissing` | C9: never completed | `LastWorkCompleteAt` null | finalize / requeue |
| `LoopWedged` | C10: stall raised instead of clean idle | `Classify()` != live | recover lane |
| `NoWorkLoopMissed` | C10: never re-awaited after complete | await absent | re-prompt receive loop |
| `TurnExitEarly` | C11: `agent_exited` before contract end | helper exit event + code | relaunch / rebridge |
| `TokenLeakInWorkTree` | C12: bearer / `.gemini` residue in work tree | fs + git | scrub + rotate token |
| `ControlPlaneHelperInWorkTree` | C12: lane-authored control-plane helper | fs + git | remove + re-steer |
| `KnownGapExpired` | skip past expiry or version ceiling | ledger + version | promote clause |
| `UnexpectedKnownGapPass` | a skipped agy clause unexpectedly passes | clause result vs ledger | promote agy (see OQ4) |

`BootstrapStall` vs `DiscoveryProbeStall` is split because both surface as
`StallDiscovery` live, but the harness *controls the fixture*: progress bytes +
no `tools/list` ⇒ alive-but-probing (#85); no progress bytes ⇒ never
bootstrapped (#101-class). (The robustness of the byte-count discriminator is
OQ2.)

### 1.3 Harness architecture

New package **`go/pkg/adapterconformance/`**:

- `contract.go` — `ClauseID` enum, `Clause{ID, Name, Deadline, Profile,
  Assert(ctx, obs DaemonObserver) ClauseResult}`, the ordered `[]Clause`,
  `AdapterSpec`, and `ContractProfile` (`Full` | `SingleShot`). Deadlines default
  from `sessionliveness.DefaultPolicy()` × `STRIATUM_CONFORMANCE_DEADLINE_SCALE`
  (default 3.0). A `ClauseResult` carries `Status ∈ {Pass, ContractFail,
  InfraOutcome}` so the runner can gate (§2.7) without re-deriving intent.
- `observer.go` — `DaemonObserver`: thin reads over the **same** daemon the
  production stack uses (RPC `run.detail`/`supervise.status` +
  `sessionliveness.ProjectionFromRow` over the session row + `artifacts`/`jobs`
  reads). It may read PTY-helper *event metadata* (`agent_started`, `progress`
  byte counts, `agent_exited` + exit code) for classification, but **never PTY
  text**.
- `preflight.go` — the C1 health/auth probe per adapter (§2.7): resolve binary +
  `--version`, then a cheap adapter-specific liveness/auth check (e.g.
  `claude --version` + an auth-state read; `codex` token presence;
  `agy`/gemini version + key check) that returns one of `Ready` /
  `AdapterUnavailable` / `AdapterVersionUnknown` / `AdapterUnauthenticated` /
  `AdapterProviderUnavailable` **without** burning a full agent turn. The exact
  per-adapter probe is an implementation detail to be verified against the
  installed CLI (residual in §2.7 / OQ6).
- `runner.go` — `Run(ctx, AdapterSpec, ContractProfile, Mode) Report`:
  1. provision an isolated PostgreSQL DB via `go/pkg/pgtest` (auto-provision when
     `STRIATUM_PG_TEST_URL` is set), `db.ConnectAndMigrate`;
  2. boot a **real, in-process `striatumd`** (production RPC mutation handlers +
     `go/pkg/mcp` HTTP server) on loopback + an ephemeral 0600 capability token —
     **not a stub** (§2 / §3.2 of both proposals);
  3. **run C1 pre-flight**; on an infra outcome, gate per §2.7 (skip C2–C12, emit
     the infra class) instead of launching a doomed turn;
  4. create an isolated **temp target repo**, register it, seed a **single-job
     fixture workflow** (`document_only`/`draft` for `SingleShot`; a multi-turn
     job with one scripted interrogation for `Full`);
  5. launch the lane via the **production `supervise.start` →
     `go/pkg/supervisor` `RunHelper`** path with the adapter's real `command`, so
     the actual PTY + argv bootstrap + `injectLaneMCPConfig` path is exercised;
  6. drive the scripted interrogation (`interrogation.ask`/`.show`);
  7. evaluate each clause against `DaemonObserver`, recording timing + the first
     unmet clause's class + structured fields; apply the **post-green-pre-flight
     bounded retry** (§2.7) on a first-attempt C3/C4 stall;
  8. tear down (`supervise.stop`) and run the C12 hygiene scan.
- `taxonomy.go` — the `FailureClass` enum (contract + infra), the contract/infra
  partition, and JSON report schema (§1.2).
- `report.go` — **human remediation surface (ergonomics R2):** in addition to the
  `dist/adapter-conformance.json` machine report, the runner prints, for each
  failed clause, **one line to stdout**: `FAIL <adapter>/<Cn> <Name>:
  <FailureClass> (deadline=…, last_protocol_event=…, supervisor_state=…[,
  agent_exit_code=…])` followed by a **static per-class one-line hint** (a
  `map[FailureClass]string`, e.g. `BootstrapStall → check installed CLI version
  & the argv bootstrap path (loop.go); this is the #101 shape`). Skips print
  `SKIPPED <adapter>/<Cn> (#<issue>)`; infra outcomes print
  `INFRA <adapter>/<Cn>: <class> — re-runnable`. No transcripts.
- `fixture.go` — minimal workflow/role/prompt/expected-artifact builder. **Must
  satisfy workflow validation**: a `document_only` job needs `inputs`, the review
  lane (if any) must differ in model family from the author, and branch/`--print`
  shapes must be honored (known scaffold gotchas). The fixture prompt is small
  and imperative: `tools/list` → `await_packet` → ack → heartbeat → answer one
  interrogation → write one file → publish → complete → await again.
- `skipledger.go` + `conformance_skips.json` — the non-rotting skip (§1.5).
- `testagent/` — **one configurable hermetic fake agent** (codex's `testagent`)
  parameterized into the broken modes claude_code enumerated as
  `failingadapters/`: `never_tools_list`, `discovery_probe`,
  `exit_before_complete`, `ignore_interrogation`, `no_heartbeat`, `leak_token`,
  `leak_helper`, `env_secret`. Each mode must yield exactly its contract
  `FailureClass`, proving the taxonomy is **armed** (Tier A). A "happy path" mode
  drives the full fixture against the real daemon to prove the harness state
  machine without any provider CLI.

Driver binary **`go/cmd/striatum-adapter-conformance/main.go`** — flags
`--adapters`, `--timeout`, `--json-report`, `--ci`, `--release`,
`--promote-agy-multiturn`. Runs the matrix, emits `dist/adapter-conformance.json`
(no transcripts), prints the human summary (above), and exits with a
**mode-aware, outcome-distinguished code** (§1.6). (A `_test.go` thin wrapper
also lets `go test` developers run Tier A locally.)

### 1.4 Lane-env hardening — structural, not prompt-steering

All four are **mechanisms verified by clauses**, so the fix cannot rot back into
prompt-steering.

- **#76 (agy survey block)** — keep `usageStatisticsEnabled:false` in the
  ephemeral gemini settings (`mcpconfig.go:128`). *Conformance*: C0 golden
  asserts the key is present; the live agy run asserts C3 fires within
  `DiscoverySeconds`. If the installed agy still blocks, C3 reports
  `SurveyPromptBlocked` and the fix is a launch/config change — never a stronger
  prompt.
- **#85 (background discovery probe / idle)** — the lane never *discovers* the
  endpoint (it is pre-seeded by `injectLaneMCPConfig` / argv `--mcp-config`), so
  any discovery probe is wasted work. The deterministic guard is the **ordered
  deadline**: C3 requires a session-bound `tools/list`, then C4 requires a
  **foreground `work.await_packet` that creates the real lease**. Background
  activity alone cannot make the lane read healthy; "progress but no foreground
  claim" classes as `DiscoveryProbeStall`. Production reuses the same
  `sessionliveness` classes so a live run fails loudly instead of idling.
- **#70 (bearer token in work tree)** — **converged fix: relocate the agy MCP
  settings to a per-lane out-of-tree config home** (codex; see §2.1 for why this
  beats keeping the repo-write). Write
  `.striatum/scratch/<supervisor_id>/agy-home/.gemini/settings.json` (0600) and
  point the child's config-home env at it; keep `cmd.Dir` as the target repo so
  edits land in the workspace. **C12** (`TokenLeakInWorkTree`) is the binding
  invariant that proves it worked — zero bearer / zero `.gemini` residue in the
  work tree after teardown — regardless of mechanism. Belt-and-suspenders:
  `.gemini/` git-ignored in the fixture target repo. (The auth-preservation
  wrinkle and the "forbid repo-write fallback vs interim" question are OQ1.)
- **Daemon-secret leakage (codex addition)** — C2 asserts no `DATABASE_URL` /
  `PG*` / `STRIATUM_POSTGRES_*` / arbitrary daemon secret reaches the child. The
  surface is the existing allowlist build — `supervisedEnvPassThrough` filtered
  by `supervisedEnvAllowlistKeys` (`supervision_control.go:2381/2339`). A Tier A
  unit test pins the *builder*; the **live C2** additionally snapshots the env
  of the *actually spawned* child, so an env mutation introduced by the real
  `supervise.start`/tmux launch path — which a unit test over the builder cannot
  see — is still caught as `EnvSecretLeak`.

### 1.5 `agy` handling + non-rotting skip

agy cannot hold a multi-turn seat yet (#95 turn-driver deferred), so it runs a
**reduced profile** but **must** pass the hardening clauses (the whole point of
#76/#85/#70):

| Profile | Clauses |
|---|---|
| `Full` (claude_code, codex) | C0–C12 (multi-turn + interrogation) |
| `SingleShot` (agy) | C0, C1, C2, C3, C4, C5, C8, C9, C12 on a single-turn `document_only` job + the #76 settings golden |
| skipped for agy (ledger) | C6 (multi-heartbeat), C7 (interrogation), C10 (no-work loop), C11 (no-premature-exit) |

The skip is **data, not a `t.Skip()`** — a ledger entry
(`skipledger.go` / `conformance_skips.json`):

```json
{ "adapter": "agy", "clause": "C7_InterrogationRoundTrip",
  "issue": 95, "issue_url": "https://github.com/.../issues/95",
  "observed_agy_version": "0.X.Y",
  "reason": "agy multi-turn seat deferred; #95 turn-driver",
  "promote_when": { "min_adapter_version": "0.X.(Y+1)", "issue_state": "closed" } }
```

Four anti-rot properties:

1. **Visible** — the report prints `SKIPPED agy/C7 (#95)` distinctly from a pass;
   never a silent green.
2. **Version-gated** — the harness reads the *installed* agy `--version`; at
   `>= promote_when.min_adapter_version` the skip is invalid and the clause is
   enforced (failing CI until it actually passes) → `KnownGapExpired`.
3. **Issue-gated** — a CI step cross-checks `gh issue view 95`; a skip
   referencing a closed issue fails the ledger-integrity check (OQ4 notes this
   gate is networked, not hermetic).
4. **No silent acceptance of a lucky pass** — if a skipped agy clause
   *unexpectedly passes*, CI fails with `UnexpectedKnownGapPass` and tells the
   implementer to promote agy via `--promote-agy-multiturn` (flakiness trade-off
   is OQ4).

A skip with no `promote_when` is **rejected at ledger load** — every skip
carries its own expiry condition.

### 1.6 Run modes + gating (ergonomics R1, devils_advocate R1/R2)

The single driver behaves differently by mode, and the outcome-distinguished
exit code is what lets CI gate correctly:

| Mode | Adapters run | Missing binary | Provider/auth infra | Contract failure | Exit code |
|---|---|---|---|---|---|
| **local** (no `--ci`) | only those present **and** authenticated; others reported, not failed | reported `AdapterUnavailable`, **no fail** | reported, **no fail** | hard fail | `0` if every *run* adapter passed; `1` on any contract fail |
| **`--ci`** (per-PR, required-to-merge) | every declared adapter | **hard fail** (runner-config defect) | `EX_TEMPFAIL` (**75**) → **re-runnable / neutral**, not merge-blocking | **hard fail** (`1`) | `1` contract / `75` provider-transient / `0` pass |
| **`--release`** | every declared adapter | hard fail | **hard fail** (a release must carry a fresh live proof) | hard fail | `1` on any non-pass |

The CI workflow maps **exit 75 → a neutral, re-runnable status** (operator
re-runs when the provider recovers) and **exit 1 → a hard, blocking status**.
Because provider-transience produces a *distinct, honest* status ("provider
unavailable — re-run") rather than a misleading contract red, maintainers have
no incentive to disable the whole job — which is exactly what closes the
false-green path in §2.7. **Copy-pasteable local invocation** (R1):

```sh
# run conformance for only the adapter you have authenticated locally:
STRIATUM_CONFORMANCE_ADAPTERS=claude_code make adapter-conformance-local
# (or, equivalently, the driver directly:)
go run ./go/cmd/striatum-adapter-conformance --adapters claude_code
```

`make adapter-conformance` keeps the `--ci` semantics for the CI job;
`make adapter-conformance-local` is the no-`--ci` target that runs the
present/authenticated subset and never hard-fails on an adapter you did not
intend to exercise.

---

## 2. Resolved conflicts (the pick + reason)

### 2.1 #70 mechanism — out-of-tree config-home (codex) over keep-repo-write+teardown (claude_code)

**Pick: codex.** claude_code argued relocation is "blocked on agy exposing an
out-of-repo flag" and kept the repo-tree write + guaranteed teardown +
leak-scan. But `mcpconfig.go:83` itself states agy reads `<cwd>/.gemini/` **or
`~/.gemini/`** — so a *per-supervisor redirected config-home* (a private `HOME`/
config-home pointing at `.striatum/scratch/<sup>/agy-home/`) is viable and is
**not** the "shared `~/.gemini` is unsafe to clobber" case the comment warns
about (that warning is about writing the *real* user-global home). Relocating
removes the token from the work tree **entirely** rather than depending on
teardown firing — strictly safer. claude_code's leak-scan clause (C12) is kept
as the asserting invariant for whichever mechanism ships. (Residual: agy
auth-preservation and the "ever allow repo-write fallback?" call → OQ1.)

### 2.2 Clause granularity — codex's fuller ordered set + claude_code's hermetic C0 and tiers

**Pick: both, merged.** codex contributes first-class ordered clauses for
`AdapterResolved` (C1), `LaneEnvHardened` (C2), `NoPrematureExit` (C11), and
`NoWorkTreeCredentialLeak` (C12) that claude_code only carried in its taxonomy.
claude_code contributes the **hermetic C0 construction golden**, the **Tier A /
Tier B execution split**, and **`DefaultPolicy()`-anchored deadlines +
`STRIATUM_CONFORMANCE_DEADLINE_SCALE`**. These are complementary, not competing
— a live bootstrap clause (C3) and a hermetic construction clause (C0) catch
different regressions, and both are cheap.

### 2.3 Naming

- **Command/target:** `go/cmd/striatum-adapter-conformance` + `make
  adapter-conformance` / `make adapter-conformance-local` (codex). Reason:
  namespaced for future conformance suites; a bare `conformance` would collide.
- **Adapter-declaration env var:** `STRIATUM_CONFORMANCE_ADAPTERS=claude_code,codex`
  (claude_code). Reason: list semantics read more clearly than
  `STRIATUM_ADAPTER_CONFORMANCE`, which sounds like a mode toggle.
- **Env prefix family (R4):** all conformance env vars use the
  **`STRIATUM_CONFORMANCE_*`** prefix (`STRIATUM_CONFORMANCE_ADAPTERS`,
  `STRIATUM_CONFORMANCE_DEADLINE_SCALE`). Note the word order *intentionally*
  differs from the `adapter-conformance` binary/target (a first-time user might
  guess `STRIATUM_ADAPTER_CONFORMANCE_*`); the driver `--help` and the README
  state the `STRIATUM_CONFORMANCE_*` family explicitly so it is discoverable.

### 2.4 Artifact-failure split

**Pick:** `ArtifactMissing` (no row/blob) + `ArtifactRejected{reason}` with
`reason ∈ {front_matter, write_scope, byline, hash}`. Reason: collapses
claude_code's `ArtifactInvalidFrontMatter` and codex's `ArtifactPublishRejected`
into one class with a routing sub-field — `front_matter` is publisher exit 6.

### 2.5 Fake-agent shape

**Pick:** one configurable hermetic `testagent` (codex) parameterized into the
modes claude_code listed as separate scripts. Reason: one fake with modes is less
code and stays in sync with the taxonomy more easily than N shell scripts.

### 2.6 Converged with no conflict (retry rule refined — see §2.7)

Bootstrap-delivered = `last_tools_list_at` (both); real daemon over pgtest, fake
agent only for harness self-tests (both); reuse `sessionliveness` stall classes
as the source of truth (both); minimal local protocol-based fixture (both). The
attempt-1 statement "infra-only retries — never retry a protocol stall" is
**refined** in §2.7: it was too coarse (a provider blip *manifests as* a protocol
stall), so the rule becomes "retry infra errors; retry a first-attempt C3/C4
stall **once** only after a green pre-flight; treat a recurrence as a genuine
non-retried protocol stall." "Missing declared adapter on the CI runner is a hard
failure" is preserved but **scoped** by mode (§1.6).

### 2.7 Provider-transient vs genuine non-conformance (devils_advocate R1; resolves OQ5)

**The collision (R1).** Attempt 1 affirmatively chose *"a missing or
unauthenticated declared adapter is a hard failure, never a skip"* **and**
*"infra-only retries — never retry a protocol stall."* A transient provider
outage / 429 / expired CI secret **during the agent turn** manifests **as a
protocol stall** (no `tools/list` or no `await_packet` advance — C3/C4). The
"infra-only retry" rule, keyed on stall *shape*, declines to retry it, and
"unauthenticated/unavailable = hard failure" turns it red — blocking unrelated
green-codebase PRs on a third party's rate limiter (false-red). The realistic
operator response — marking the whole live job non-blocking — then lets a *real*
adapter regression ride through (false-green). Both betray §0's purpose.

**Pick — pre-flight gate + bounded retry + outcome-distinguished gating:**

1. **Pre-flight provider/auth gate (C1).** Before launching a turn, the harness
   runs a *cheap* per-adapter health/auth probe (`preflight.go`). Its failure is
   an **infra outcome**, never a contract `FailureClass`:
   - missing binary → `AdapterUnavailable`;
   - present, no usable creds → `AdapterUnauthenticated` (**this is exactly the
     OQ5 soft-spot, now resolved** — a present-but-unauthenticated adapter fails
     *fast and honestly here*, not later as a misleading `BootstrapStall`/
     `AwaitPacketStall`);
   - reachable but provider erroring/429 → `AdapterProviderUnavailable`.
2. **Discriminator + bounded retry.** C2–C12 run **only after a green C1**. If a
   first-attempt **C3/C4 stalls after a green pre-flight**, the harness retries
   the **whole live case once** (the provider-blip allowance: a healthy
   pre-flight followed by a mid-turn stall is the blip shape). If the retry also
   stalls → it is a **genuine** protocol stall, emitted as the real contract
   `FailureClass` and **not** retried again. Genuine protocol stalls therefore
   stay non-retried; transient blips do not hard-block. **Cost, stated
   explicitly:** a genuine #101-class regression (green pre-flight, then a broken
   bootstrap submission) spends *one extra* live turn before reporting
   contract-red — a bounded 2× on the worst-case failing path. That is the
   accepted price of not re-importing R1's false-red; it is paid only on the rare
   failing path and never more than once.
3. **Outcome-distinguished gating (the false-green close).** We do **not** make
   the job non-blocking. Instead the driver exits with a *distinct code per
   outcome* (§1.6): **`1`** for any contract failure (hard, blocks merge — the
   #101-class value is preserved), **`75`/`EX_TEMPFAIL`** for an infra
   provider/auth outcome (loud, re-runnable, neutral per-PR). CI maps 75 →
   neutral/re-run and 1 → blocking. Because provider-transience surfaces as an
   honest "provider unavailable — re-run" status rather than a misleading
   contract red, maintainers never have cause to disable the job, so a real
   regression always hard-blocks. Release mode (`--release`) escalates infra
   outcomes to hard failures, because a release must carry a fresh live proof.

This threads both horns: genuine non-conformance hard-blocks (no false-green);
provider transience does not hard-block green PRs and is never silently skipped
(no false-red, no silent green).

### 2.8 C0 scope correction + conformance-job gating semantics (devils_advocate R2)

**(a) Claim correction.** C0 is a golden over **our** code; it catches a
Striatum refactor reverting claude to `pty_submit`. It does **not** catch the
#101 *class* (a CLI behavior regression in someone else's binary). The #101
catch is **Tier B live C3/C4** against the installed CLI. §1.1 now states this.

**(b) Gating semantics.** The `conformance` (Tier B) job is **required-to-merge
and per-PR**. A *missing declared CLI* or *unprovisionable postgres runner* is a
**hard failure** (runner-config defect) — distinct from the R1 provider-transient
case, which is `EX_TEMPFAIL`/neutral (§2.7). The distinction is: *can we even run
the contract?* (missing binary = config defect = hard) vs *is the provider
momentarily erroring?* (transient = neutral/re-run).

**(c) Unpinned-CLI detection latency.** Because CI installs the lane CLIs at
**latest/unpinned**, a CLI regression is caught on the **next** conformance run
and attributed to **a run/date+installed-version, not a specific PR** — adequate
for L2 *detection* but it must be stated so operators do not expect PR-level
attribution. The report records `binary_path` + `version` for every adapter so a
red is tied to the exact CLI build that broke it. (Whether to additionally pin a
known-good CLI version for attribution + run "latest" as a separate canary is a
cost/coverage call left as a note, not adopted here — see OQ6.)

---

## 3. Open questions (for the adversarial panel)

**OQ1 — agy auth under a redirected config-home (#70).** §2.1 picks the
out-of-tree config-home, but two things are unverified: (a) does the *installed*
agy actually load `mcpServers.striatum` from a redirected `HOME`/
`XDG_CONFIG_HOME`, and (b) does a wholesale `HOME` redirect to an empty scratch
dir **de-auth** agy (OAuth creds normally live under `~/.gemini`)? If so we must
seed/symlink the credential files into the scratch home or rely on
`GEMINI_API_KEY` env. And the product call codex forces: if neither works, do we
keep claude_code's repo-write + teardown + leak-scan as a sanctioned **interim**,
or declare agy **unsafe-for-MCP-lanes** until it exposes a config-home flag?
*This is the unresolved core of the §2.1 conflict.*

**OQ2 — `tools/list` forgeability + the #85 discriminator.** C3 trusts
`last_tools_list_at`. The `BootstrapStall`-vs-`DiscoveryProbeStall` split leans on
helper `progress` **byte counts** — a PTY-helper signal, not pure protocol. Is
"progress bytes but no tools/list" a durable discriminator, or does it couple the
taxonomy to helper internals that rot? Specifically: could a CLI issue
`tools/list` from a *background* probe (firing C3 green) while the foreground
receive loop is dead — and is C4's "the packet created the lease for the expected
session" sufficient to catch that **without** the byte-count heuristic?

**OQ3 — taxonomy adequacy for Layer 3 routing.** Is class + structured-fields
enough for L3 to pick a recovery, or do routing-ambiguous classes need a
mandatory *cause* sub-field? `TurnExitEarly` conflates CLI crash vs auth-expiry
vs OOM; `BootstrapStall` conflates never-launched vs launched-but-mute. The
daemon sees only protocol events + helper exit codes — where would a reliable
cause signal come from, and is it in Layer 2's scope to capture it?

**OQ4 — non-rotting skip: hermetic vs networked, and the lucky-pass trade-off.**
Version-gated promotion needs the installed agy binary (Tier B, not offline);
issue-gated promotion needs `gh` (network). Can the ledger-integrity guarantee be
fully **blocking-and-hermetic**, or is at least one promotion gate inherently
non-hermetic — and is `UnexpectedKnownGapPass` worth the CI flakiness it adds
(one lucky agy multi-turn pass fails CI and demands promotion; the next run
regresses)?

**OQ5 — RESOLVED (folded into §2.7).** The false-green-via-skip soft spot is
*authentication*, not presence: an *unauthenticated* claude that
bootstraps-but-cannot-answer would pass C0–C3 and fail later with a misleading
class. **Resolution:** the §2.7 pre-flight health/auth probe (C1) asserts
"authenticated" cheaply *before* a turn is burned, emitting `AdapterUnauthenticated`
fast rather than a misleading downstream stall. Residual for the panel: the
*exact* per-adapter "is it authenticated?" probe that does not itself burn a full
live turn (claude auth-state read, codex token presence, agy key/version) is an
implementation detail to verify against each installed CLI (see OQ6).

**OQ6 — gating dispositions the panel should attack.** Two residuals from
the §2.7/§2.8 picks: (a) Is the `EX_TEMPFAIL`/75 → neutral-re-runnable mapping
*itself* a false-green door — i.e., during a *sustained* provider outage in a
release window, release mode hard-fails (no release without a fresh proof), but
per-PR mode leaves the live-conformance signal *absent* rather than *proven
green*. **Candidate disposition (attempt 3, proposed not adopted):** record a
*last-green-proof* (`binary_path` + `version` + timestamp of the last green
Tier B run) plus a **staleness budget** of N days — inside N days of a green
proof, a provider-blocked per-PR run stays neutral/re-runnable; past N days with
no green live proof, the per-PR disposition escalates to hard-block, so a
sustained outage cannot leave the live signal indefinitely "absent-but-loud."
The panel sets N (or rejects this for the simpler "always neutral per-PR;
release mode is the only hard live gate"). (b) Is the cheap pre-flight
auth probe reliable enough that a green pre-flight followed by a turn stall is a
*sound* signal to spend a bounded retry — or can a probe be green while the turn
quota is exhausted, making the single retry wasteful? And the §2.8(c) call:
pin-a-known-good-CLI + latest-canary for PR-level attribution, or accept
detection-only latency?

---

## 4. Build outline (so the implementer can start)

**Step 1 — package skeleton** `go/pkg/adapterconformance/`: `contract.go`
(ClauseID enum, `Clause`, ordered list, `AdapterSpec`, `ContractProfile`,
`ClauseResult{Status}`), `taxonomy.go` (FailureClass + infra classes +
contract/infra partition + report fields), `observer.go` (DaemonObserver over
RPC + `sessionliveness.ProjectionFromRow`), `preflight.go` (per-adapter
health/auth probe → infra outcome), `fixture.go` (validation-passing single-job
workflow builder), `runner.go` (the Run flow incl. pre-flight gate + bounded
retry), `report.go` (JSON + human per-clause stdout summary + per-class hint
map), `skipledger.go` + `conformance_skips.json`, `testagent/` (configurable
fake).

**Step 2 — Tier A first (no CLI), so value lands on every PR immediately:**
C0 golden over `bootstrapDeliveryModeFor`/`appendBootstrapArgv` per adapter; the
#76 settings golden; skip-ledger integrity (reject missing `promote_when`,
closed-issue reference); taxonomy self-validation driving every `testagent`
broken mode → its `FailureClass`; the C2 env-allowlist unit test over
`supervision_control.go`'s env build. Also unit-test the §1.6 gating table
(mode × outcome → exit code) and the §2.7 retry discriminator with a fake whose
first attempt stalls and second passes.

**Step 3 — pgtest real-daemon happy-path integration:** drive the full fixture
(bootstrap → … → no_work) with `testagent` happy mode against a real in-process
`striatumd`, proving the harness state machine + observer end-to-end with no
provider CLI.

**Step 4 — Tier B live matrix:** `go/cmd/striatum-adapter-conformance` drives
installed claude_code + codex (`Full`) and agy (`SingleShot`) through the
contract against the pgtest daemon via the production `supervise.start` path,
gated by the C1 pre-flight + §1.6 modes.

**Step 5 — #70 mechanism change (separate PR, gated on OQ1):** replace the
repo-tree write in `writeEphemeralGeminiSettings` with the out-of-tree
config-home + env redirect; C12 is the asserting invariant. Until OQ1 is
verified live, ship **C12 + `.gemini/` gitignore** as the interim guarantee over
the existing repo-write + teardown.

**Make / CI wiring:**

- `go/Makefile`:
  ```make
  .PHONY: adapter-conformance adapter-conformance-local
  adapter-conformance: build
      go run ./cmd/striatum-adapter-conformance --ci --json-report ../dist/adapter-conformance.json
  adapter-conformance-local: build
      go run ./cmd/striatum-adapter-conformance --json-report ../dist/adapter-conformance.json
  ```
  and fold the Tier A tests into the existing `test`/`check` targets so they run
  on every PR.
- Root `Makefile`:
  ```make
  .PHONY: adapter-conformance adapter-conformance-local
  adapter-conformance: go-build
      $(MAKE) -C "$(GO_DIR)" adapter-conformance
  adapter-conformance-local: go-build
      $(MAKE) -C "$(GO_DIR)" adapter-conformance-local
  ```
- `.github/workflows/ci.yml`: the existing `go` job already runs Tier A via
  `make -C go check` (unconditional, no CLI). Add a `conformance` job on the
  `postgres:16`-backed Linux runner (reuse `STRIATUM_PG_TEST_URL`,
  `pgtest` auto-provisions) that installs the lane CLIs, sets
  `STRIATUM_CONFORMANCE_ADAPTERS=claude_code,codex` (+ agy where installed &
  authenticated), and runs `make adapter-conformance`. The job is
  **required-to-merge, per-PR**. Exit-code mapping (§1.6, §2.7): **exit 1 →
  hard, blocking**; **exit 75 → neutral, re-runnable** (provider-transient, not a
  contract failure); a missing declared CLI / unprovisionable runner → hard
  failure. The job **uploads `dist/adapter-conformance.json` as a build
  artifact** (ergonomics R2) and the runner echoes the per-failed-clause summary
  to the job log; it publishes **no transcripts**.

**Acceptance gates:** (1) revert claude to `pty_submit` → C0 fails in Tier A;
(1b) **#101-class**: a CLI that does not submit its bootstrap → Tier B **C3**
`BootstrapStall` (the catch C0 cannot make — R2); (2) simulated #85 idle probe →
C4 `DiscoveryProbeStall`; (3) leaked bearer → C12 `TokenLeakInWorkTree`; (4) a
skip missing `promote_when` → ledger test fails; (5) a declared adapter missing
on the CI runner → hard failure, never a skip; (6) **R1**: a transient provider
429 after a green pre-flight → one bounded retry, and if it persists →
`AdapterProviderUnavailable` exit 75 (neutral/re-runnable), **not** a contract
red — while a genuine post-green protocol stall → contract red exit 1; (7)
**ergonomics**: `make adapter-conformance-local` with one authenticated adapter
runs only that adapter and exits 0 (no hard-fail for the unauthenticated ones).
