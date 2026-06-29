# RFC 0121: Lane Provider Auth Preflight Gate

Status: accepted (D181; implemented)
Date: 2026-06-12
author: rfc-author-codex-gpt-5-001
Context: GH #252; GH #250 regression fixture; RFC 0116 / D175 (`run drive`);
RFC 0096 (supervised-lane trust boundary); RFC 0110 / D164 (lane OS user and
PostgreSQL isolation); RFC 0009 (supervision); RFC 0120 / D180 (idle exit and
operator-side wake); `go/pkg/mutations/supervision_control.go`;
`go/pkg/mutations/supervision_lane_config.go`;
`go/pkg/mutations/supervision_env.go`; `go/pkg/reads/doctor_codex.go`;
`go/pkg/cli/rundrive/rundrive.go`; local Codex CLI `0.139.0` help observed at
authoring time for the proposed smoke flags.

## Problem

GH #252 captures a launch-time blind spot: a Codex lane can be configured,
registered, and supervised successfully from Striatum's point of view, then
fail immediately because the provider credentials visible to the lane OS user
are missing, stale, expired, or unable to refresh. By the time the failure is
observable, `supervise.start` may already have created supervisor rows, scratch
files, helper state, tmux state, and a launched process. `run drive` can then
retry the same broken launch as the DAG advances, producing repeated sanitized
but late failures instead of a single pre-launch refusal.

The existing Codex doctor check is the wrong authority for this problem. It
checks operator-side Codex MCP configuration drift so a direct operator Codex
session can reach Striatum. It does not prove that the lane OS user's provider
auth can run a fresh Codex turn. A pure doctor check is also too easy to forget,
and a pure `run drive` preflight misses manual `supervise.start`.

The preflight itself is sensitive. It may invoke a provider CLI, touch the
network, spend provider tokens, hang on an interactive prompt, and produce
provider output. It therefore cannot become part of ordinary `striatum doctor`,
cannot read auth files or token values, and cannot persist raw stdout, stderr,
or model text.

## Goals

- Fail a fresh Codex lane launch before `supervise.start` creates supervisor
  rows, mints or injects a session-bound lane token, creates supervisor scratch,
  starts tmux/helper processes, or launches the provider CLI for the real lane.
- Run the check under the same lane identity that the real supervised lane would
  use, not the operator identity.
- Keep `supervise.start` as the authoritative launch gate, so every launch path
  crosses the same decision.
- Let `run drive` pass the gate mode through and stop fast on a blocking
  refusal instead of owning a second launch policy.
- Provide an explicit, opt-in diagnostic surface for operators who want to
  inspect lane provider auth without launching a lane.
- Keep all outputs private-safe and local-first: no provider SDK, no hosted
  service integration, no durable state, no raw provider output, and no token or
  auth-file material in results.

## Non-Goals

- Do not run a provider CLI during ordinary `striatum doctor` or
  `striatum doctor --verbose`.
- Do not make `run drive` the owner of provider-auth policy. It remains a
  foreground operator loop over normal lifecycle verbs.
- Do not execute workflow-authored lane commands as the preflight command. The
  preflight command is a closed provider-specific smoke.
- Do not prove that a provider will remain available after launch. A passing
  smoke is a point-in-time readiness check, not durable workflow state.
- Do not add provider SDKs, durable transcript capture, provider account
  identity reporting, token inspection, or external persistence.
- Do not cover every adapter in V1. This RFC accepts the Codex gate first; other
  providers require explicit support and tests before `required` can pass.

## Decision

Accept a shared `lane_provider_auth` preflight primitive, exposed as an explicit
diagnostic and enforced by `supervise.start` before launching supported Codex
agent-loop lanes.

`supervise.start` is the authority because every fresh supervised lane launch,
manual or driven by `run drive`, crosses it. `run drive` passes a gate mode to
`supervise.start`; it does not run its own provider check or retry policy.

The gate mode vocabulary is:

- `auto` - the default. Run the Codex provider-auth smoke only for Codex
  agent-loop lanes when `STRIATUM_LANE_OS_USER` names a distinct configured OS
  lane user. Same-user launches and unsupported providers are skipped in `auto`.
- `required` - run the smoke for supported providers and block launch on any
  non-passing result. If the lane provider is unsupported, fail with
  `lane_provider_preflight_unsupported`.
- `off` - bypass the provider-auth gate. This is the emergency rollback path and
  must be explicit at the launch surface.

The default is intentionally not "run always." Provider auth probes can spend
tokens, reach the network, and trigger OAuth refresh behavior. The default is
also not "doctor only" because the failure must be prevented before launch.

## Command And RPC Surface

Add:

```text
striatum supervise start --session-id <id> [--provider-auth-gate auto|required|off]
striatum run drive --run-id <id> [--provider-auth-gate auto|required|off]
striatum doctor --lane-provider-auth codex [--run-id <run_id>] [--lane-id <lane_id>] [--timeout 45s] --json
```

`supervise.start` accepts RPC param `provider_auth_gate` with the same
`auto|required|off` vocabulary. The default is `auto`.

`run drive` forwards the selected mode to `supervise.start`. If a fresh session
was registered and `supervise.start` refuses with a blocking lane-provider-auth
code, `run drive` closes that session with a sanitized reason, emits one
sanitized action, exits nonzero, and does not loop on repeated register/start
attempts for the same failure.

The doctor flag is an explicit diagnostic over the same primitive. Ordinary
`doctor` and `doctor --verbose` do not invoke provider CLIs. The existing
`doctor_codex.go` check remains scoped to operator-side Codex MCP endpoint and
bearer-env drift; it is not redefined as lane provider auth.

## Result Shape

The shared primitive returns a private-safe block shaped like:

```json
{
  "checked": true,
  "provider": "codex",
  "run_id": "run_...",
  "lane_id": "author",
  "run_as_user": "striatum-lane",
  "status": "passed",
  "failure_class": null,
  "probe": "codex_exec_output_last_message",
  "exit_code": 0,
  "stdout_bytes": 0,
  "stderr_bytes": 0,
  "success_signal": "matched",
  "raw_output_returned": false,
  "network": "provider_cli_may_use_network",
  "costing": "provider_tokens_may_be_spent",
  "duration_ms": 1234,
  "remediation": "none"
}
```

Failure results use `status: "failed"` and one of these stable
`failure_class` values:

- `lane_provider_auth_failed` - provider credentials are absent, stale, expired,
  revoked, or unable to refresh.
- `lane_provider_binary_missing` - the provider CLI cannot be found or executed
  under the lane launch environment.
- `lane_provider_unavailable` - network, provider service, rate limit, or
  provider-side availability prevented an auth conclusion.
- `lane_provider_preflight_timeout` - the smoke timed out, including an
  interactive prompt or hung refresh path.
- `lane_provider_preflight_launch_failed` - Striatum could not start the closed
  smoke command under the intended lane identity.
- `lane_provider_preflight_unsupported` - the selected gate mode requires a
  provider that has no supported smoke.
- `lane_provider_preflight_unexpected_result` - the smoke reached an
  unsupported result shape that cannot be classified as auth success, auth
  failure, launch failure, timeout, binary missing, or provider unavailable.

The RPC error `Code` for a blocking `supervise.start` refusal is the same stable
classification when possible. Error `Details` may include the safe result block
above. It must not include provider stdout, stderr, final text, auth paths,
provider account ids, environment values, token material, raw PTY logs, or
tracebacks.

For Codex, a zero-exit smoke is treated as provider-auth success even when the
bounded `--output-last-message` signal is missing, empty, or mismatched. That
condition is returned as the safe `success_signal` diagnostic rather than a
blocking auth refusal, because the provider CLI has already authenticated and
completed the closed smoke.

MCP endpoint drift, repository ACL failure, and lane sandbox failure remain
separate classes. They must not be collapsed into
`lane_provider_auth_failed`. Existing doctor and supervision surfaces can keep
reporting those failures through their own checks.

## Codex Smoke

The supported Codex smoke is closed and owned by Striatum:

```text
codex exec \
  --ignore-user-config \
  --ignore-rules \
  --ephemeral \
  --skip-git-repo-check \
  --sandbox read-only \
  -c approval_policy="never" \
  -C <preflight-cwd> \
  --output-last-message <tmp-output> \
  --json \
  "Reply exactly: ok"
```

Rules:

- Execute as the lane OS user when `STRIATUM_LANE_OS_USER` is configured, using
  the same `sudo -n -u <lane-user> -- env -i ...` pattern as supervised lane
  launch. Same-user `required` checks use the same sanitized env construction
  without `sudo`.
- Preserve the lane user's provider-auth home. Do not point `CODEX_HOME` at a
  throwaway directory unless the implementation has an explicit supported lane
  auth-home configuration; otherwise the check would prove the wrong home.
- Run from a fresh temporary preflight cwd outside the target repository and
  outside workflow-authored paths. The smoke must not read target-repository
  rules or trust files and must not depend on a git checkout.
- Do not pass `STRIATUM_MCP_TOKEN`, `STRIATUM_MCP_TOKEN_FILE`, daemon runtime
  tokens, PostgreSQL DSNs, provider access tokens, or workflow-authored
  `command_env` secrets to the smoke.
- Use a bounded timeout, default `45s`.
- Read only the exit status, stdout/stderr byte counts, and bounded success
  signal state needed to classify or diagnose the result. Delete temporary
  files after classification. Never persist or return raw stdout, stderr, JSONL
  events, or final text.

## Ordering

`supervise.start` runs the gate after `loadSupervisionStartConfig` resolves the
frozen workflow snapshot, lane command, adapter, agent-loop mode, launch env,
and lane run-as user. It runs before:

- creating supervisor scratch or FIFOs,
- minting or injecting the session-bound lane MCP token,
- inserting `process_supervisors`, `daemon_supervisors`, or
  `process_supervisor_pointers` rows,
- recording `supervisor.starting`,
- starting the helper, tmux session, or provider lane process.

The preflight may create its own temporary directory for the smoke command, but
that directory is not workflow state and is removed after the check.

## Serialization

Provider-auth probes are serialized per provider auth home, keyed at least by
provider, run-as user, and resolved auth home. This avoids concurrent OAuth
refresh probes reusing or rotating the same refresh token at the same time. The
serialization is process-local to the daemon in V1 and does not require a new
database table or migration.

## Implementation Notes

- Add `go/pkg/laneproviderauth` with a fakeable command runner, Codex command
  builder, classifier, timeout handling, and keyed serialization.
- Reuse or extract lane-user exec and sanitized-env helpers currently split
  across `go/pkg/mutations/supervision_env.go`,
  `go/pkg/mutations/supervision_launch.go`, and the supervisor launch helpers,
  so the preflight and real launch cannot diverge on run-as identity.
- Wire `go/pkg/mutations/supervision_control.go` to evaluate the gate in the
  ordering described above.
- Add `go/pkg/reads/doctor_lane_provider_auth.go` for the explicit doctor flag.
  Keep `go/pkg/reads/doctor_codex.go` scoped to operator-side MCP drift.
- Update `go/pkg/cli/rundrive/rundrive.go` so the driver forwards
  `provider_auth_gate` and treats blocking gate failures as terminal for that
  drive invocation.
- Update CLI params, help text, the RPC error catalog, generated route tables,
  `docs/reference/command-authority-matrix.md`,
  `docs/reference/cli-reference.md`, `docs/reference/spec.md`, and
  `docs/how-to/lane-sandbox.md` when the implementation lands.
- No database migration, new durable event type, new artifact kind, provider
  SDK, or durable transcript store is authorized by this RFC.

## Acceptance Criteria

- Ordinary `striatum doctor` and `striatum doctor --verbose` never invoke a
  provider CLI.
- `striatum doctor --lane-provider-auth codex --json` runs the shared primitive
  only on explicit request and returns the safe result block with
  `raw_output_returned=false`.
- `supervise.start` with default `auto` runs the Codex smoke before launch for
  Codex agent-loop lanes when a distinct `STRIATUM_LANE_OS_USER` is configured.
- `supervise.start --provider-auth-gate required` fails unsupported providers
  with `lane_provider_preflight_unsupported`.
- `supervise.start --provider-auth-gate off` bypasses the provider-auth gate and
  preserves the previous launch path.
- A stale, missing, or revoked Codex auth fixture fails before supervisor rows,
  supervisor scratch, tmux/helper processes, lane processes, or lane token
  injection are created.
- The smoke executes as the lane OS user with an `env -i` sanitized environment
  and excludes Striatum MCP tokens, daemon runtime tokens, DSNs, provider tokens,
  and raw environment values from all returned data.
- Failure classification distinguishes provider auth from binary missing,
  provider/network unavailability, timeout/hang, unsupported provider, smoke
  launch failure, and unexpected success output. MCP drift and repository ACL
  failures stay in their existing diagnostic lanes.
- `run drive` forwards the selected gate mode to `supervise.start`, closes a
  freshly registered session after a blocking gate refusal, emits one sanitized
  action, exits nonzero, and does not keep registering or starting replacement
  sessions for the same gate failure.
- Concurrent checks against the same Codex auth home serialize.
- Unit tests cover command construction, env sanitization, result redaction,
  classifier cases for success/stale/missing/hang/network, doctor opt-in versus
  default doctor, `supervise.start` no-side-effect refusal, `run drive` stop-fast
  behavior, and the #250 live/manual smoke fixture where host auth is available.

## Rejected Alternatives

### Doctor-Only Preflight

Rejected. It is useful as an explicit diagnostic, but it is not a launch
authority and does not protect manual `supervise.start` or automation that
forgets to run it.

### Run-Drive-Owned Gate

Rejected. `run drive` is an operator loop over lifecycle verbs. If it owns the
provider-auth decision, manual `supervise.start` and future launch paths can
bypass the gate. `run drive` should forward mode and stop on refusal.

### Ordinary Doctor Runs Provider CLIs

Rejected. Ordinary doctor must stay low-cost and private-safe. A provider CLI
probe may use the network, spend tokens, or trigger auth refresh behavior, so it
requires an explicit flag.

### Workflow-Authored Preflight Commands

Rejected. The workflow author controls lane commands; executing arbitrary lane
commands as preflight would turn a diagnostic into an unaudited command runner.
Provider-auth preflight uses closed commands owned by Striatum.

## Domain Modeling

`lane_provider_auth` is a boundary check, not workflow state. It is a derived
readiness value at a point in time over a provider, lane identity, and auth home.
It can block `supervise.start` before launch, but it does not claim work, lease
work, complete work, publish artifacts, attest bylines, or create durable
provenance.

This matches the DDD rule in
[`docs/reference/domain-driven-design.md` "Adding to the model"](../reference/domain-driven-design.md#adding-to-the-model):
the new concept is named because it protects an existing aggregate boundary
(`supervise.start`) and has distinct invariants, but it does not become a new
aggregate root or persistence substrate.
