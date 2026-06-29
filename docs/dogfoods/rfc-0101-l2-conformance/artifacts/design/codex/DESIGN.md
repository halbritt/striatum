# RFC 0101 Layer 2 Adapter Conformance Design
author: designer-codex-gpt-5.5-xhigh-001
date: 2026-05-31
status: proposed

## 1. Problem Restatement

Striatum's daemon state machine is now reasonably well defined, but the lane
boundary is still treated as "whatever the installed terminal CLI happens to do
today." That is the wrong trust boundary. The daemon can be healthy while a
lane never submits its bootstrap, blocks on an adapter survey, hides inside a
background discovery probe, writes a bearer token into the target work tree, or
exits after one turn. Those failures are currently discovered during live
dogfoods, after a run is already wedged.

RFC 0101 Layer 2 should make the lane boundary an executable contract. Every
supported adapter must prove, in CI and against the installed CLI binary, that
it can bootstrap, discover MCP, claim work, keep a lease alive, handle one
interrogation turn, publish an artifact, complete the job, and return to the
receive loop. The same fixture must also prove that lane environment hardening
is deterministic: no prompt steering as the only defense for #76/#85/#70, no
target-repo credential file, no inherited daemon secrets, and no silent green
skip for agy while #95 remains open.

## 2. Proposed Design

### 2.1 Conformance Matrix

The harness owns a typed matrix, not ad hoc test names:

| Adapter | Required contract | Bootstrap mode | Notes |
|---|---|---|---|
| `claude_code` | Full Layer 2 contract | argv bootstrap | Promotes #101 to a non-optional clause: bootstrap must be delivered as an initial argv prompt, not typed into the TUI and submitted with CR. |
| `codex` | Full Layer 2 contract | argv bootstrap plus `-c mcp_servers.striatum.url=...` | Bearer stays in `STRIATUM_MCP_TOKEN`; no repo config write. |
| `agy` | Single-shot subset now; full contract is expected-fail until #95 lands | `--prompt-interactive` | Must still pass bootstrap, MCP discovery, claim, heartbeat, publish, complete, env hardening, and token-leak clauses. Multi-turn/interrogation/no-work-loop clauses are an explicit known gap, not a silent pass. |

For CI, required adapters are named by `STRIATUM_ADAPTER_CONFORMANCE`.
`make adapter-conformance` fails when a required adapter binary is missing. A
developer-local optional mode may skip missing binaries, but release/CI mode
does not. The harness records the binary path and version for every adapter in
the JSON report so a CLI bump is tied to the run that proved or broke it.

### 2.2 Ordered Contract Clauses

The contract is a list of ordered clauses because the failure point matters:

1. `AdapterBinaryResolved`: resolve the configured installed binary on the
   supervised PATH and capture `--version` or an adapter-specific version probe.
   Failure is `AdapterUnavailable` or `AdapterVersionUnknown`.
2. `LaneEnvHardened`: start the lane through the production supervision path
   with an allowlisted environment. Assert no `DATABASE_URL`, `PG*`,
   `STRIATUM_POSTGRES_*`, or arbitrary daemon secret reaches the child. Assert
   `PATH`, `HOME`, terminal basics, `STRIATUM_MCP_URL`, token material, run id,
   session id, supervisor id, repository id, and lane id are present exactly
   where intended.
3. `BootstrapSubmitted`: launch the CLI under the PTY helper and require the
   first session-bound daemon protocol signal within the discovery deadline.
   The canonical signal is `sessions.last_tools_list_at` becoming non-null.
   The bootstrap prompt and skill templates should explicitly tell lanes to
   call `tools/list` with both `repository_id` and `session_id`; the harness
   treats TUI output, redraws, and spinner bytes as irrelevant.
4. `AwaitPacketForeground`: after `tools/list`, require
   `work.await_packet` within the await deadline and require the returned
   packet to create the active lease for the expected session/job. This catches
   the #85 shape: background discovery activity is not enough; the foreground
   receive loop must claim real work.
5. `PacketAcknowledged`: after packet delivery, require `work.ack` before the
   ack deadline. Assert `last_ack_at` and the lease owner.
6. `LeaseHeartbeatObserved`: require at least one `work.heartbeat` while the
   job is active. Assert both `last_work_heartbeat_at` and active lease
   heartbeat freshness.
7. `InterrogationRoundTrip`: while the job lease is still active, the harness
   opens and asks one interrogation against the session. The lane must call
   `work.await_packet`, receive `interrogation_question`, answer through
   `interrogation.answer`, and return to the work. This is skipped only for
   agy's declared #95 known gap.
8. `ArtifactPublished`: the lane writes one expected artifact inside a temp
   target repo write scope and publishes it through `artifact.publish`. Assert
   path, hash, logical name, kind, and byline through daemon state. The harness
   does not inspect PTY output.
9. `WorkCompleted`: require `work.complete` for the claimed job and assert the
   job state transitions to completed.
10. `ReceiveLoopContinues`: after completion, require another
   `work.await_packet` and observe `no_work`. This proves the lane did not stop
   at completion prose and can continue a durable receive loop. This is skipped
   only for agy's declared #95 known gap.
11. `NoPrematureExit`: the lane process must remain alive through the full
   contract for full adapters. For agy's single-shot subset, exit after
   `WorkCompleted` is allowed only under the #95 known-gap record.
12. `NoWorkTreeCredentialLeak`: scan the target repo before launch, during the
   active lane, after completion, and after teardown. A bearer token, MCP
   config, or control-plane helper under the target repo outside `.striatum/`
   is a hard failure. `.striatum/scratch/` remains operational scratch and is
   never published.

### 2.3 Harness Architecture

Add a new package plus one CI-facing command:

- `go/pkg/adapterconformance/contract.go`: typed `ClauseID`, adapter matrix,
  timeouts, known-gap metadata, and ordered contract definitions.
- `go/pkg/adapterconformance/runner.go`: orchestration for one adapter run.
  It creates an isolated temp target repo, an isolated PostgreSQL database from
  `STRIATUM_PG_TEST_URL`, starts a real `striatumd`, adopts/registers the repo,
  prepares a minimal workflow, starts a supervised lane, and observes daemon
  state until the clauses pass or fail.
- `go/pkg/adapterconformance/fixture.go`: minimal workflow, role, prompt, and
  expected artifact builder. The prompt is intentionally small and imperative:
  call `tools/list`, `work.await_packet`, ack, heartbeat, wait for exactly one
  interrogation, write one file, publish, complete, then await again.
- `go/pkg/adapterconformance/observe.go`: daemon/RPC observation helpers. These
  read `run.detail`, `supervise.status`, protocol-liveness projections, and
  artifact/job records. They may use PTY helper event metadata such as
  `agent_started`, `progress` byte counts, and `agent_exited` for failure
  classification, but never PTY text.
- `go/pkg/adapterconformance/failures.go`: stable failure taxonomy and JSON
  report schema for later Layer 3 routing.
- `go/cmd/striatum-adapter-conformance`: `--adapters`, `--timeout`,
  `--json-report`, `--ci`, and `--promote-agy-multiturn` flags. The command is
  what Make and CI call.
- `go/pkg/adapterconformance/testagent`: a hermetic fake agent used only for
  harness self-tests. It proves the harness and failure taxonomy without
  pretending to satisfy real adapter conformance.

The root `Makefile` should add:

```make
.PHONY: adapter-conformance
adapter-conformance: go-build
	$(MAKE) -C "$(GO_DIR)" adapter-conformance
```

The Go `Makefile` should add:

```make
.PHONY: adapter-conformance
adapter-conformance: build
	go run ./cmd/striatum-adapter-conformance --ci --json-report ../dist/adapter-conformance.json
```

CI runs the target after the normal Go build/test lane on the PostgreSQL-backed
Linux job. Harness unit tests and fake-agent integration stay in `go test
./pkg/adapterconformance/...`; the real installed-CLI target is separate so a
missing or regressed adapter is reported as adapter conformance failure, not as
a generic package test panic.

### 2.4 Real Daemon, Not A Stub

Use a real daemon over a real pgtest PostgreSQL database. A stub would hide the
exact regression Layer 2 exists to catch: #101 was not a pure Go function bug,
it was an integration bug between a real TUI, argv/stdin bootstrap, tmux/PTY
delivery, the MCP endpoint, session activity recording, and the claim loop. A
stub can prove the harness state machine, but it cannot prove the installed CLI
still honors the lane contract.

The live fixture's flakiness is controlled by keeping everything local except
the adapter CLI itself: loopback MCP, isolated PG database, temp repo, bounded
deadlines, deterministic prompts, no terminal-output scraping, and one retry
only for daemon startup readiness, not for adapter clause failures.

### 2.5 Failure Taxonomy

Each failed clause emits one primary class plus structured fields:
`adapter`, `binary_path`, `version`, `clause`, `deadline`, `last_protocol_event`,
`supervisor_state`, `active_lease_id`, and `known_gap_id` when applicable.

| Class | Meaning |
|---|---|
| `AdapterUnavailable` | Required adapter binary is absent from the supervised PATH. |
| `AdapterVersionUnknown` | Binary exists but version cannot be captured. CI can warn locally, but release mode fails. |
| `BootstrapStall` | No session-bound `tools/list` before the discovery deadline after `agent_started`. |
| `DiscoveryProbeStall` | Some MCP/progress activity occurs, but no foreground `work.await_packet` claims work before the await deadline. This is the #85 routing class. |
| `AwaitPacketStall` | `tools/list` was recorded, but no `work.await_packet` followed. |
| `PacketAckStall` | A work packet was delivered but not acknowledged before the ack deadline. |
| `HeartbeatMissed` | Active lease exists but no `work.heartbeat` was observed within the clause window. |
| `InterrogationIgnored` | Harness asked an interrogation and the lane did not receive or answer it through MCP. |
| `ArtifactPublishMissing` | The expected artifact file or publish call never appeared. |
| `ArtifactPublishRejected` | `artifact.publish` was attempted but rejected by write scope, byline, kind, or hash validation. |
| `WorkCompleteMissing` | The job did not transition through `work.complete`. |
| `NoWorkLoopMissed` | After completion, the lane did not call `work.await_packet` again and observe `no_work`. |
| `TurnExitEarly` | The lane process exited before completing the required full-contract clauses. |
| `SurveyPromptBlocked` | agy/gemini remains alive but makes no protocol progress in a fixture that has survey suppression enabled. The harness does not need to read the prompt text to classify it. |
| `TokenLeakInWorkTree` | Bearer token or MCP config appears in the target repo outside `.striatum/scratch/`. |
| `ControlPlaneHelperInWorkTree` | A lane-authored helper such as `scripts/striatum_client.py` appears in the target repo. |
| `EnvSecretLeak` | A banned daemon secret or DSN-shaped env var reaches the lane. |
| `KnownGapExpired` | A declared expected-fail skip has passed its expiry or version ceiling. |
| `UnexpectedKnownGapPass` | A skipped agy multi-turn clause unexpectedly passes; CI fails and tells the implementer to promote agy instead of silently accepting green. |

### 2.6 Lane-Env Hardening

The current code has good direction: `supervisedEnv` is already allowlist-based,
`usageStatisticsEnabled:false` is written for agy, and terminal supervisor
states call `CleanupGeminiSettings`. Layer 2 should turn these from local tests
into adapter contract clauses and close the remaining unsafe shapes.

For #76, survey/feedback suppression belongs in adapter launch material, not in
the prompt. Keep `usageStatisticsEnabled:false` in the agy/gemini settings body
and add any adapter-supported noninteractive flag only after verifying it with
the installed CLI. If the installed agy still blocks, conformance fails with
`SurveyPromptBlocked`; the fix is a launch/config change, not a stronger prompt.

For #85, the deterministic guard is the ordered protocol deadline. The lane must
produce a session-bound `tools/list`, then foreground `work.await_packet`, then
ack. Background discovery can still happen inside an opaque CLI, but it cannot
make the lane read healthy or pass conformance. Production should reuse the same
session-liveness classes so a live run fails loudly instead of idling.

For #70, stop writing bearer-bearing settings into the target repo. Replace
`writeEphemeralGeminiSettings(repoRoot, ...)` with an agy runtime-home strategy:

- create `.striatum/scratch/<supervisor_id>/agy-home/.gemini/settings.json`
  with `0600` permissions;
- set `HOME`, `XDG_CONFIG_HOME`, and any verified gemini/agy config-home env
  vars for the child process to that scratch home;
- keep `cmd.Dir` as the target repo so file edits still happen in the right
  workspace;
- fail launch with `unsafe_mcp_config_surface` if the installed agy cannot load
  MCP settings from that out-of-tree home;
- never fall back to `<repo>/.gemini/settings.json` with a bearer token.

The conformance fixture proves this by scanning the target repo for the literal
test bearer and for `mcpServers.striatum` config before, during, and after the
run. Cleanup is still centralized at terminal supervisor transitions, but the
only credential file left to clean is under `.striatum/scratch/`.

## 3. Key Decisions

**Bootstrap delivered detection:** use daemon-side protocol state, specifically
session-bound `last_tools_list_at`, as the definition of bootstrap delivered.
PTY text is not a signal. A spinner can produce bytes forever while the agent
has not joined the control plane; the daemon protocol timestamp is the first
meaningful proof.

**Real daemon vs. stub:** the acceptance fixture uses a real `striatumd` and
PostgreSQL. The fake agent is only for testing the harness itself. Stubs hide
the integration seams that broke #101 and #85: argv handling, PTY lifecycle,
MCP auth/config, session activity recording, and artifact publication.

**Failure taxonomy:** failures are named by the first broken contract clause and
carry enough state for Layer 3 recovery routing. For example,
`BootstrapStall` points at launch/bootstrap, `DiscoveryProbeStall` points at
foreground receive-loop failure, and `TokenLeakInWorkTree` points at lane-env
hardening.

**Agy known gap:** agy's multi-turn/interrogation/no-work-loop clauses are an
expected failure while #95 is open, but the skip is data, not a comment. It must
include issue URL, observed agy version, version ceiling or expiry date, and
promotion instructions. CI fails on `KnownGapExpired` and on
`UnexpectedKnownGapPass`. When #95 lands, `--promote-agy-multiturn` flips those
clauses to required and refuses any remaining skip.

**No unsafe agy config fallback:** if agy cannot read Striatum MCP settings from
scratch HOME or another verified out-of-tree config surface, the adapter is not
safe for production MCP lanes. Writing a bearer into the target repo is no
longer an allowed compatibility path.

## 4. Risks

The load-bearing risk is that real AI CLIs are slower and less deterministic
than normal integration-test binaries. A flaky conformance target would train
maintainers to ignore it.

Mitigation: the fixture is minimal, local, and protocol-based. It checks daemon
events, not prose quality. It uses tight per-clause deadlines with precise
failure classes instead of one giant timeout. It separates harness self-tests
from installed-CLI acceptance, records adapter versions, and makes missing
required CLIs a clear setup failure. For agy, the known-gap path is explicit and
expires, so the matrix cannot become permanently green by omission.

## 5. Test Plan

1. Unit-test `go/pkg/adapterconformance` contract ordering, matrix validation,
   known-gap expiry, JSON report generation, and failure classification.
2. Add fake-agent integration tests that simulate each failure class:
   no tools/list, no await, no ack, missing heartbeat, ignored interrogation,
   publish rejection, early exit, and token leak.
3. Add lane-env unit tests around the production spawn path: allowlist env,
   no DSN inheritance, no duplicate `PATH`, agy scratch-home settings,
   terminal cleanup, and repo leak scan.
4. Add a pgtest-backed real-daemon fake-agent test that drives the full fixture
   through bootstrap, claim, heartbeat, interrogation, publish, complete, and
   no_work. This proves the harness without requiring provider CLIs.
5. Add `make adapter-conformance` and wire it into CI's PostgreSQL Linux job.
   In CI/release mode, required installed adapters must pass or fail with a
   stable failure class. The target emits `dist/adapter-conformance.json` for
   debugging but publishes no transcripts.
6. Require the real `claude_code` and `codex` adapters to pass the full
   contract. Require `agy` to pass the single-shot and lane-env subset, with
   only the #95 multi-turn clauses marked as expiring known gaps.
