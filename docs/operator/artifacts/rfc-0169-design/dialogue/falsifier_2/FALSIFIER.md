# RFC 0169 Falsifying Challenge: P1 Gate Matrix Is Not Behavior-Preserving

author: falsifier-reviewer-002

## Claim Challenged

Hard Claim 1 says the `ProviderReadinessAdapter` registry is a
behavior-preserving refactor: codex keeps RFC 0121's `Check` verbatim, agy gets a
no-op receipt with zero behavior change, claude keeps today's observe/pass shape
until P2, and the only intended behavior delta is that an unregistered provider
refuses with `provider_readiness_unregistered`.

That claim does not clear as written. The holder has not specified a coherent
gate matrix that can both preserve today's non-codex behavior and route all
providers through the registry. At least one of the claimed properties must
change, and the SPEC does not say which one.

## Concrete Evidence

Current source has a codex-only support guard in
`go/pkg/mutations/supervision_provider_auth.go`:

- `GateOff` returns nil before any provider logic.
- `supported := config.AgentLoopMode == agentLoopModeSelfDriving && provider == laneproviderauth.ProviderCodex`.
- If `!supported`, `GateRequired` returns `lane_provider_preflight_unsupported`.
- If `!supported`, `GateAuto` returns nil.
- Only after that does the gate run `laneproviderauth.Check`; only a passed
  checked result emits `lane.auth_success`.

So today, agy, claude, and an unknown provider never reach the codex preflight in
`GateAuto`; they pass. In `GateRequired`, they refuse as unsupported.

The holder's P1 text asks for incompatible outcomes:

1. It says `runSuperviseProviderAuthGate` collapses to registry lookup plus
   `ValidateReadiness`, and unregistered providers refuse closed.
2. It also says the "class-independent gate scaffolding" remains in the gate and
   preserves the `AgentLoopMode == selfDriving` support guard, the
   `GateAuto && RunAsUser == ""` skip, and best-effort `lane.auth_success` on
   `result.Passed()`.
3. It says agy routes to a no-op adapter and has no gate behavior change because
   it passed in `GateAuto` before and passes after.
4. It says claude in P1 registers a satisfied stub and introduces no behavior
   change.

Those cannot all be true.

## Counterexample Matrix

For `provider=agy`, `AgentLoopMode=selfDriving`, `GateRequired`,
`RunAsUser=striatum-lane`: current behavior is refusal with
`lane_provider_preflight_unsupported`. If P1 routes agy through a no-op
`EphemeralDaemonMintedAdapter`, it launches. That is a behavior change. If P1
keeps the current codex-only support guard, the no-op adapter is unreachable and
the registry is not the one gate.

For `provider=claude`, `AgentLoopMode=selfDriving`, `GateRequired`,
`RunAsUser=striatum-lane`: current behavior is the same unsupported refusal. If
the P1 `OAuthCopiedAdapter` stub returns satisfied, it launches before P2
placement exists. That is also a behavior change, not "observe-only / pass in
GateAuto" limited to the current default mode.

For `provider=unknown`, `AgentLoopMode=selfDriving`, `GateAuto`,
`RunAsUser=striatum-lane`: current behavior is launch because `!supported`
returns nil. If P1 preserves that early return, `provider_readiness_unregistered`
is never reached. If P1 removes or broadens the guard so the registry refuses,
then the SPEC needs a precise statement that the support guard changed and that
this is the single intended behavior delta.

For `provider=unknown`, `GateAuto`, `RunAsUser=""`: if the
`GateAuto && RunAsUser == ""` skip is really class-independent and remains before
registry validation, an unknown provider can still pass without a registry entry.
If the skip is codex-only, then it is not the class-independent scaffolding the
holder describes.

There is a similar event-surface problem. Today `lane.auth_success` is emitted
only downstream of a real codex `Check` pass. If the new generic gate emits on
any adapter `result.Passed()`, agy and the P1 claude stub gain new auth-success
events. If it does not, the holder's statement that the existing
`result.Passed()` scaffolding is preserved is incomplete and needs provider/class
qualification.

## Strongest Rebuttal

The RFC intentionally wants one behavior delta: unknown providers should refuse
instead of silently launching. It is also plausible that `GateRequired` semantics
for registered non-codex providers should improve once those providers have real
adapters; in that reading, agy and claude passing under `GateRequired` is a
feature, not drift.

That rebuttal does not save Hard Claim 1 as written. The proposal repeatedly says
P1 is a pure refactor and that agy/claude have zero behavior change before new
logic lands. Changing `GateRequired` from unsupported refusal to adapter pass is
observable behavior. Emitting new success events is observable behavior. Keeping
the codex-only support guard avoids those changes but prevents both the registry
spine and the unknown-provider refusal from taking effect.

## Unanswered Gap

Hard Claim 1 should not clear until the SPEC includes a full pre/post gate matrix
for at least:

- provider: codex, agy, claude, unregistered/unknown;
- gate mode: off, auto, required;
- agent loop mode: self-driving and non-self-driving;
- `RunAsUser`: empty and non-empty;
- expected RPC error code and event emission.

The matrix must explicitly choose where the support guard and no-lane-user skip
live in the registry design, prove that unregistered providers cannot fall
through those skips, and prove that agy/claude `GateRequired` and
`lane.auth_success` behavior are either preserved or named as intentional
behavior deltas.

Until that exists, the P1 seam is not proven before new logic lands. The proposed
registry can either be behavior-preserving for today's non-codex paths or
refuse-closed for unknown providers through a single registry gate, but the holder
has not specified an implementation contract that does both.
