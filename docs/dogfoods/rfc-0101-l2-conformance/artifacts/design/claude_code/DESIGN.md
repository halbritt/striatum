---
title: "RFC 0101 Layer 2 — Adapter-Conformance Harness + Lane-Env Hardening"
kind: handoff
author: designer-claude-opus-4.8-001
lane: claude_code
status: design-proposal
---

# RFC 0101 Layer 2 — Adapter-Conformance Harness + Lane-Env Hardening

> Independent design proposal (claude_code lane). Not coordinated with the
> codex lane. Reconciliation is the synthesizer's job.

## 1. Problem restatement

Striatum drives interactive terminal coding CLIs (`claude_code`, `codex`,
`agy`/gemini) through a PTY, supervised by a daemon (`striatumd`) that owns all
authoritative state in PostgreSQL (D094). Each adapter has its own, *silent*
failure modes at the lane boundary, and a CLI version bump can regress
bootstrap or the turn loop **with no test catching it** — the regression is
discovered live, mid-run, by a wedged lane. Three concrete instances:

- **#101** — Claude Code v2.1.x stopped submitting a PTY-typed bootstrap (a
  trailing CR no longer submitted; the prompt sat buffered in the TUI line
  editor). Fixed by switching claude to **argv bootstrap**
  (`bootstrapDeliveryModeFor` → `bootstrapDeliveryArgv`, `go/pkg/agentloop/loop.go:148`).
  Nothing in CI would have caught it; both claude lanes simply idled.
- **#76** — `agy` blocks on the gemini-cli interactive usage/feedback survey.
- **#85** — `agy` spawns a background MCP-discovery probe and idles past the
  deadline before claiming work.
- **#70** — `agy` writes a bearer token into the target repo's
  `.gemini/settings.json` (token material in the work tree).

RFC 0101 Layer 2 makes the lane boundary a **contract every adapter must
satisfy, verified in CI against the *actually installed* CLI binary**, so a
CLI bump that breaks bootstrap or the turn loop **fails CI** instead of wedging
a live run. Layer 2 is detection + prevention; Layer 3 (out of scope here) is
automated recovery, so the failure taxonomy must be precise enough to later
route a recovery action.

The current daemon already has most of the observable substrate I need:
`go/pkg/sessionliveness/liveness.go` defines the canonical protocol-progress
columns (`last_tools_list_at`, `last_await_packet_at`, `last_ack_at`,
`last_work_heartbeat_at`, `last_work_complete_at`, …) and a stall classifier
(`StallDiscovery`, `StallAwaitPacket`, `StallAck`, `StallLeaseHeartbeat`,
`StallQuestionPending`, `StallProtocolIdle`). The harness should **reuse** these
signals, not invent a parallel truth.

## 2. Proposed design

### 2.1 The conformance contract (ordered clauses)

The contract is an **ordered list of clauses**, each asserted against a
**daemon-observable signal**, never against PTY bytes. A lane is conformant iff
it advances every applicable clause within that clause's deadline. Clauses are
ordered because a later clause is only meaningful once the earlier one has
fired (you cannot heartbeat a lease you never claimed).

| # | Clause | Daemon-side assertion | Source signal |
|---|--------|----------------------|---------------|
| C0 | **AdapterContract** (#101 promotion) | `bootstrapDeliveryModeFor(cmd)` returns the adapter's declared mode; argv-bootstrap adapters (`claude`,`codex`,`agy`) take the prompt via argv, never PTY-submit | golden/unit, `go/pkg/agentloop/loop.go` |
| C1 | **BootstrapDelivered** | `sessions.last_tools_list_at` becomes non-null within `DiscoverySeconds` | `sessionliveness.LastToolsListAt`, stamped at `go/pkg/mcp/tools.go:29` |
| C2 | **AwaitPacket** | `last_await_packet_at` advances after C1 within `AwaitPacketSeconds` | `LastAwaitPacketAt` |
| C3 | **Claimed/Acked** | the seeded job transitions to `acked`/`claimed`; `last_ack_at` set | job state + `LastAckAt` |
| C4 | **HeartbeatObserved** (≥1) | `last_work_heartbeat_at` (or `active_lease_last_heartbeat_at`) advances ≥ once while the job is open | `LastWorkHeartbeatAt` |
| C5 | **InterrogationRound** | harness opens 1 interrogation; lane answers; `last_session_question` clears (answer recorded) before `DeadlineQuestionPending` | `interrogation.*` + `StallQuestionPending` non-fire |
| C6 | **ArtifactPublished** | expected artifact row exists, blob present, front matter valid (publisher exit ≠ 6) | `artifacts` table + `artifactcontracts` |
| C7 | **WorkCompleted** | job state `completed`; `last_work_complete_at` set | `LastWorkCompleteAt` |
| C8 | **LoopToNoWork** | next `work.await_packet` returns `no_work`; lane idles cleanly with **no** stall class raised for `ProtocolIdle` grace | `work.await_packet` result + `Classify()` == live/idle |

C0 is the **#101 fix promoted to a contract clause**: a pure, always-run
assertion that claude (and codex/agy) construct an argv bootstrap, so a future
refactor that reverts claude to PTY-submit fails immediately and hermetically.
C1–C8 exercise the *live* CLI end-to-end.

### 2.2 Harness architecture (real `go/` paths)

New package **`go/pkg/adapterconformance/`**:

- `contract.go` — the ordered `[]Clause`, each `Clause{ID, Name, Deadline,
  Assert(ctx, obs DaemonObserver) ClauseResult}`. `Deadline` defaults are
  derived from `sessionliveness.DefaultPolicy()` so the harness and the live
  liveness classifier never drift.
- `observer.go` — `DaemonObserver`: thin reads over the **same** daemon (RPC +
  `sessionliveness.ProjectionFromRow`) the production stack uses. It polls
  `supervise.status` / the session row and the `artifacts`/`jobs` tables. **No
  PTY scraping**; the only PTY touch is the operator trajectory log, which the
  harness ignores.
- `runner.go` — `Run(ctx, AdapterSpec, ContractProfile) Report`. It:
  1. provisions PostgreSQL via `go/pkg/pgtest` (auto-provision when
     `STRIATUM_PG_TEST_URL` is set), `db.ConnectAndMigrate`;
  2. boots an **in-process real daemon** (the same RPC mutation handlers +
     `go/pkg/mcp` HTTP server) bound to a loopback port + an ephemeral 0600
     capability token — *not* a stub (see §3.2);
  3. registers a repo, seeds a **single-job conformance workflow** (a
     `document_only`/`draft` job for the single-shot profile; a multi-turn job
     with one scripted interrogation for the full profile);
  4. launches the lane via the **production `supervise.start` →
     `go/pkg/supervisor` `RunHelper`** path with the adapter's real `command`
     (e.g. `claude --dangerously-skip-permissions --model …`), so the actual
     PTY + argv bootstrap + `injectLaneMCPConfig` path is exercised;
  5. drives the scripted interrogation via `interrogation.ask`/`interrogation.show`;
  6. evaluates each clause against `DaemonObserver`, collecting timing + the
     failure class on the first unmet clause;
  7. tears down (supervise.stop) and runs the **post-run hygiene scan** (C-Token,
     §2.4).
- `taxonomy.go` — the `FailureClass` enum (§2.3).
- `skipledger.go` — the non-rotting skip mechanism (§2.5).
- `failingadapters/` — tiny fake-CLI scripts that misbehave in known ways, used
  to self-validate the taxonomy (§5).

Driver binary **`go/cmd/striatum-conformance/main.go`** — a thin `main` that
runs the matrix and exits non-zero on any non-skipped clause failure, for the
`make conformance` target / CI. (Equivalently a build-tagged
`//go:build conformance` test `TestAdapterConformance`; I prefer the explicit
binary so CI logs read as a conformance report, and a `_test.go` thin wrapper
so `go test` developers get it too.)

### 2.3 Failure taxonomy

Each failed clause emits exactly one `FailureClass`. Classes map 1:1 to a
daemon signal and are named so Layer 3 can route a recovery action:

| FailureClass | Fires when | Daemon signal it reads | Likely L3 recovery |
|---|---|---|---|
| `AdapterContractViolation` | C0 construction mismatch | golden, no daemon | code fix (blocks merge) |
| `BootstrapStall` | C1: no `last_tools_list_at` by deadline, lane alive | `StallDiscovery` w/ progress bytes seen | re-deliver bootstrap / argv fallback |
| `DiscoveryProbeStall` | C1: lane alive + emitting progress but no tools/list (the #85 idle-probe shape) | `StallDiscovery` + tmux_ok + progress>0 | kill background probe / nudge |
| `AwaitPacketStall` | C2: tools/list but no await_packet | `StallAwaitPacket` | re-prompt receive loop |
| `AckStall` | C3: packet delivered, never acked | `StallAck` | requeue packet |
| `HeartbeatMissed` | C4: lease held, heartbeat stops | `StallLeaseHeartbeat` | requeue stale lease |
| `TurnExitEarly` | C4–C7: `agent_exited` before complete | helper `agent_exited` event | relaunch / rebridge |
| `InterrogationIgnored` | C5: question pending past deadline | `StallQuestionPending` | re-ask / escalate |
| `ArtifactMissing` | C6: no artifact row | `artifacts` table | re-request publish |
| `ArtifactInvalidFrontMatter` | C6: publisher exit 6 | `artifactcontracts` | bounce to author |
| `CompleteMissing` | C7: never completed | `LastWorkCompleteAt` null | finalize/requeue |
| `LoopWedged` | C8: stall raised instead of clean idle | `Classify()` != live | recover lane |
| `TokenLeakInWorkTree` | hygiene scan finds bearer/`.gemini` residue | filesystem + git | scrub + rotate token |

`BootstrapStall` vs `DiscoveryProbeStall` is deliberately split: both surface as
`StallDiscovery` in the live classifier, but the harness can distinguish them
because it controls the fixture — a lane that emitted **progress bytes** (helper
`progress` events) yet produced no `tools/list` is *alive but probing/idle*
(#85), whereas a lane with no progress bytes never bootstrapped (#101-class).
That distinction is what makes the class precise enough for L3 routing.

### 2.4 Lane-env hardening (#76 / #85 / #70) — deterministic, not prompt-steering

These are **structural** mechanisms verified by conformance clauses, so the fix
cannot silently rot back into prompt-steering.

- **#76 (agy survey block)** — *Mechanism (already partially in
  `go/pkg/agentloop/mcpconfig.go:128`)*: `writeEphemeralGeminiSettings` sets
  `usageStatisticsEnabled:false` in the ephemeral `.gemini/settings.json`,
  which disables the gemini-cli usage-statistics + periodic feedback survey
  deterministically (a config key, not a prompt). *Conformance*: a **hermetic
  golden test** asserts the generated settings carry the key; the **live agy
  run** asserts C1 fires within `DiscoverySeconds` (no survey wedge).

- **#85 (background MCP-discovery probe / idle)** — *Mechanism*: the lane must
  never need to *discover* the endpoint — the ephemeral settings.json / argv
  `--mcp-config` already pre-seed the striatum server, so any "discovery probe"
  is wasted work that idles the lane. The deterministic close is two-part:
  (a) **pre-seed** is mandatory (already done per adapter in
  `injectLaneMCPConfig`); (b) the harness asserts the **first protocol event is
  `tools/list`** within deadline and classes a progress-but-no-tools/list lane
  as `DiscoveryProbeStall`. For agy specifically I propose adding a settings key
  to suppress any auto-discovery/onboarding network call if the gemini build
  exposes one (additive + harmless if ignored, same pattern as
  `usageStatisticsEnabled`). The clause is the guardrail: if a CLI bump
  re-introduces an idle probe, C1 fails in CI.

- **#70 (token in work tree)** — *Mechanism*: the bearer must not survive in the
  work tree. Today `writeEphemeralGeminiSettings` writes a 0600
  `<repo>/.gemini/settings.json` containing `Authorization: Bearer …` and relies
  on guaranteed teardown (`CleanupGeminiSettings`, centralized at the supervisor
  terminal-state transition — callers in `go/pkg/mutations/{supervision_control,
  lifecycle,recovery}.go`). The structural hardening I propose:
  1. Ensure `.gemini/` is **git-ignored** in the target repo template so the
     token can never be committed even mid-run (belt to the teardown's
     suspenders).
  2. Add a hard conformance clause `TokenLeakInWorkTree`: after a lane run
     **and teardown**, the harness scans the work tree (git-tracked + untracked)
     for (i) the literal bearer string and (ii) any `.gemini/settings.json`
     residue. Must be **zero**. This makes the "teardown actually ran" property
     an asserted invariant, not a hope.
  3. Restate D150: relocating the file *outside* the work tree is the real fix
     but is blocked on agy exposing an out-of-repo config path/env. Until then,
     ignore-list + teardown + leak-scan is the deterministic Layer-2 guarantee;
     when agy gains the flag, the clause flips to "no `.gemini/` written at all".

### 2.5 `agy` handling + non-rotting skip

`agy`/gemini cannot hold a multi-turn seat yet (#95 turn-driver deferred), so
it runs a **reduced profile** plus the agy-specific hardening clauses (which it
*must* pass — they are the whole point of #76/#85/#70):

| Profile | Clauses run |
|---|---|
| `Full` (claude_code, codex) | C0–C8 (multi-turn + interrogation) |
| `SingleShot` (agy) | C0, C1, C2, C3, C6, C7, C8 on a single-turn `document_only` job + `usageStatisticsEnabled` golden + `TokenLeakInWorkTree` |
| skipped for agy | C4 (multi-heartbeat across turns), C5 (interrogation round) |

The skip is **not** a bare `t.Skip()`. It is a ledger entry
(`skipledger.go` / `conformance_skips.json`):

```
{ "adapter": "agy", "clause": "C5_InterrogationRound",
  "issue": 95,
  "reason": "agy multi-turn seat deferred; #95 turn-driver",
  "promote_when": { "min_adapter_version": "0.X.Y", "issue_state": "closed" } }
```

Three properties keep it from rotting into a permanently-green lie:

1. **Visible**: the harness prints an explicit `SKIPPED agy/C5 (#95)` line in
   the report — never a silent pass; a skipped clause is reported distinctly
   from a passed clause.
2. **Version-gated promotion**: the harness reads the *installed* agy `--version`.
   If it is `>= promote_when.min_adapter_version`, the skip is **invalid** and
   the clause is enforced (failing CI until it actually passes). So the day agy
   ships the turn-driver and the runner upgrades, the skip auto-promotes.
3. **Issue-gated promotion**: a CI step (or a unit test over the ledger) checks
   `gh issue view 95` — if #95 is closed, an unresolved skip referencing it
   **fails** the ledger-integrity test. The skip cannot outlive its own excuse.

A skip with no `promote_when` is rejected at ledger load — every skip must carry
its own expiry condition.

## 3. Key decisions (answers to the open questions)

### 3.1 "Bootstrap delivered" detection = first daemon-side protocol event

**Decision**: bootstrap is "delivered" the instant `sessions.last_tools_list_at`
becomes non-null (stamped daemon-side at `go/pkg/mcp/tools.go:29` when the lane
issues `tools/list`). **Defence**: this is the strong candidate in the brief and
it is correct precisely because it is a *protocol* event observed at the daemon,
immune to TUI redraw/spinner noise. A PTY-scrape heuristic ("saw the prompt
echo", "saw a spinner") cannot distinguish a CLI that *rendered* the prompt from
one that *submitted* it — which is exactly the #101 trap (claude rendered the
buffered prompt and looked healthy while never submitting). `tools/list` only
fires once the CLI has actually connected its MCP client to the daemon, so it
cannot be forged by redraw. C1's deadline reuses `DiscoverySeconds` so the
harness and the live `Classify()` agree on "too slow."

### 3.2 Real daemon, not a stub

**Decision**: the fixture drives a **real, in-process daemon over pgtest**, not a
stub. **What a stub hides**: the #101 regression lives in the seam between the
*real CLI's TUI* and the *real PTY bootstrap path* (`prepareLaneCommandForBootstrap`
→ argv vs PTY-submit → the CLI actually submitting). A stub that simply records
"tools/list happened" never runs the CLI, never exercises argv bootstrap, and so
is structurally blind to the entire class of regression Layer 2 exists to catch.
Conversely, a stubbed *daemon* with a real CLI would diverge from the production
mutation/liveness code and could pass while real `Classify()` would stall — a
false green. So: real CLI **and** real daemon. **Flakiness a live daemon risks**:
model/network latency, PTY timing, an un-authenticated or absent CLI on the
runner. **Mitigations**: (a) deadlines anchored on `DefaultPolicy()` with a
generous live multiplier (env-overridable `STRIATUM_CONFORMANCE_DEADLINE_SCALE`);
(b) retry only on *infrastructure* errors (PG connect, port bind, CLI-not-found),
never on a protocol stall; (c) a pre-flight that the adapter binary is present +
authenticated, recorded as an explicit `unconfigured` outcome — but that outcome
is itself non-rotting (see §4).

### 3.3 Failure taxonomy

As in §2.3 — reuse the existing `sessionliveness` stall classes as the source of
truth, add harness-only refinements (`DiscoveryProbeStall`, `TurnExitEarly`,
`TokenLeakInWorkTree`) that the fixture can distinguish because it controls the
job. Each class names a single daemon signal → directly routable by L3.

### 3.4 Preventing the permanent `xfail`

As in §2.5 — version-gated + issue-gated + must-be-visible skip ledger, with a
ledger-integrity unit test that fails if any skip lacks a `promote_when` or
references a closed issue. This is the single most important anti-rot control.

## 4. Risks

**Load-bearing risk: the suite becomes a *false green via skipping*.** The whole
harness is worthless if, on the CI runner, the live CLIs are absent or
un-authenticated and the suite quietly skips into all-green — the exact silent
failure Layer 2 is meant to abolish, reintroduced one level up. (#101 itself
"read healthy" while broken; a skipped conformance suite would too.)

**Mitigation — two non-skippable tiers + a configured-runner assertion**:

- **Tier A (hermetic, always runs, never skips)** under `make -C go test`:
  C0 adapter-contract golden, settings.json golden (#76 key present), token-leak
  scanner unit test, skip-ledger integrity test, and the taxonomy
  self-validation against `failingadapters/` fakes (§5). This tier catches the
  *construction* regressions (claude reverting off argv, the survey key
  dropping, a skip losing its `promote_when`) with **no** live CLI dependency.
- **Tier B (live-adapter)** under `make conformance`: runs the installed CLIs.
  The canonical CI conformance job sets
  `STRIATUM_CONFORMANCE_ADAPTERS=claude_code,codex` (and agy where installed).
  The runner **asserts every declared adapter actually ran** — if a declared
  adapter is missing/unauthenticated, that is a **hard failure**, not a skip. So
  the only way the live tier is "skipped" is on a dev laptop that declared no
  adapters; the CI runner declares them and therefore cannot silently skip.

Secondary risks: (b) PTY/model flakiness → generous, policy-anchored deadlines +
infra-only retries; (c) pgtest needs PostgreSQL → already provided in CI
(`postgres:16` service, `STRIATUM_PG_TEST_URL` set, `pgtest` auto-provisions);
(d) interrogation scripting racing the lane → C5 waits on the daemon-recorded
answer, not on timing.

## 5. Test plan (how the harness is validated and wired)

**Self-validation (the harness tests itself)** — `failingadapters/` ships tiny
fake "CLI" scripts that each break in one known way:

- `never_tools_list.sh` (connects PTY, emits progress, never calls tools/list)
  → must yield `DiscoveryProbeStall`.
- `exit_before_complete.sh` (claims, then exits) → `TurnExitEarly`.
- `ignore_interrogation.sh` (works but never answers) → `InterrogationIgnored`.
- `leak_token.sh` (writes the bearer into a tracked file) → `TokenLeakInWorkTree`.
- `no_heartbeat.sh` (claims, holds lease, never heartbeats) → `HeartbeatMissed`.

These run hermetically (Tier A) — they prove each `FailureClass` actually fires,
so a green conformance run means the taxonomy is *armed*, not merely silent.

**Live matrix** — Tier B drives the real `claude`/`codex` (and agy reduced
profile) through C0–C8 against the pgtest daemon.

**Wiring**:

- `go/Makefile`: add `conformance:` → `go run ./cmd/striatum-conformance` (or
  `go test -tags=conformance ./pkg/adapterconformance/...`), and fold the
  hermetic tests into the existing `test`/`check` targets so they always run.
- Root `Makefile`: add `conformance:` delegating to `make -C go conformance`.
- `.github/workflows/ci.yml`: add a `conformance` job that reuses the existing
  `postgres:16` service + `STRIATUM_PG_TEST_URL`, installs the lane CLIs, sets
  `STRIATUM_CONFORMANCE_ADAPTERS`, and runs `make conformance`. Tier A already
  rides inside `make -C go check` in the existing `go` job, so even if the
  conformance job is gated to a runner with CLIs installed, the construction
  guarantees (C0, #76 key, token scanner, skip-ledger) are enforced on **every**
  PR unconditionally.
- A ledger-integrity unit test (`skipledger_test.go`) + an optional CI step that
  cross-checks open/closed state of each referenced issue via `gh`.

**Acceptance**: (1) a deliberate revert of claude to PTY-submit fails C0 in Tier
A; (2) a simulated #85 idle probe fails C1 as `DiscoveryProbeStall`; (3) a
leaked token fails `TokenLeakInWorkTree`; (4) removing a skip's `promote_when`
fails the ledger test; (5) on the configured CI runner, a missing declared
adapter is a hard failure, never a skip.

## Appendix — grounding (paths read)

- `go/pkg/agentloop/loop.go` — `bootstrapDeliveryModeFor`, `appendBootstrapArgv`,
  `writePromptThenSubmit`, daemon receiver loop (the #101 argv fix lives here).
- `go/pkg/agentloop/mcpconfig.go` — `injectLaneMCPConfig`,
  `writeEphemeralGeminiSettings` (`usageStatisticsEnabled` #76, token-in-repo #70),
  `CleanupGeminiSettings`.
- `go/pkg/agentloop/bootstrap.go` / `token.go` — bootstrap prompt + token resolution.
- `go/pkg/sessionliveness/liveness.go` — protocol columns + `Classify()` stall taxonomy
  (the reused failure-class substrate; `LastToolsListAt` is C1's signal).
- `go/pkg/mcp/tools.go:29` — where `last_tools_list_at` is stamped (C1's daemon-side event).
- `go/pkg/supervisor/helper.go` — `RunHelper`, `agent_started`/`progress`/`agent_exited`
  events (the live launch path the harness drives).
- `go/pkg/pgtest/pgtest.go` — auto-provisioning PostgreSQL fixture for the live daemon.
- `go/pkg/mutations/supervision_control.go` — `HandleSuperviseStart`, teardown
  cleanup wiring (#70 guaranteed removal).
- `.github/workflows/ci.yml`, `Makefile`, `go/Makefile` — CI/make wiring targets.
