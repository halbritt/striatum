# RFC 0165 v5 Falsifying Challenge: Projection-Off Needs A Fail-Closed OAuth Classifier
author: falsifier-reviewer-002

## Challenge

V5 closes the literal v4 counterexample for lanes that are already classified as Claude OAuth: `provider_credential_projection=off` returns `provider_credential_projection_disabled_unsupported` before scratch, FIFO/ACL work, session-token minting, supervisor rows, helper/tmux setup, or provider process launch. That is the right direction, and it preserves the normal distinct-UID B1/B2 access-token-only projection path.

The remaining C3 gap is the exception around "non-Claude or non-OAuth" diagnostic paths. The SPEC does not define a fail-closed pre-launch classifier that proves a projection-disabled Claude lane is actually non-OAuth before allowing it to continue. Current `supervisionStartConfig` carries adapter, loop mode, command, lane env, and `RunAsUser`, but no credential `kind`; current source infers the provider from argv and, where it models Claude credential paths at all, assumes `KindOAuth` for the resolver/credential-domain guard. If the later build treats an absent, unknown, or unmodeled kind as "non-OAuth diagnostic," the v4 route can reappear.

## Claim Challenged

The challenged claim is not that v5's intended OAuth refusal is unsafe. The challenged claim is that the v5 SPEC fully discharges C3 while keeping `provider_credential_projection=off` diagnostic-only for non-OAuth paths.

The load-bearing missing predicate is:

```text
Before a projection-disabled Claude self-driving lane may launch, Striatum has positively proven that this launch cannot use Claude OAuth credential discovery.
```

Without that predicate, "non-OAuth" can become a default/fallback classification rather than a proven condition.

## Concrete Counterexample

1. A distinct-UID self-driving `claude` lane is launched, so the same-user refusal does not fire.
2. The lane launch environment names a lane-readable whole Claude credential: either `$HOME/.claude/.credentials.json` or `$CLAUDE_CONFIG_DIR/.credentials.json`, containing a fixture `refreshToken`.
3. The launch sets `provider_credential_projection=off`.
4. The build's projection-disabled gate refuses only when a parsed or rostered credential kind is exactly `oauth`; an absent, unknown, or "diagnostic" kind falls through as non-OAuth.
5. The generic provider-auth gate is `auto` or `off`, so no later Codex-only preflight blocks the Claude lane.
6. Scratch, token minting, supervisor rows, helper/tmux setup, or process launch can proceed; the Claude process then resolves the existing OAuth file from HOME or `CLAUDE_CONFIG_DIR` and obtains raw refresh-token custody.

That counterexample does not require same-user mode, lane-authored freshness, Striatum control-plane token leakage, or a broken access-token-only projection. It only requires the non-OAuth exception to be treated as "not proven OAuth" instead of "positively proven not OAuth."

## Evidence

V5 says projection-off fails closed for "Claude OAuth self-driving lanes" and is "diagnostic-only for non-Claude or non-OAuth paths." It also defines the same-user condition with `kind == oauth`, and the launch order names `runSuperviseClaudeProjectionDisabledGate`, but the SPEC does not state where that `kind` comes from before launch or how unknown kind is handled.

The current source shape makes this ambiguity material:

- `supervisionStartConfig` has no credential-kind field; it has command, adapter-derived identity, loop mode, launch env, and `RunAsUser`.
- `runSuperviseProviderAuthGate` is still Codex-oriented and unsupported providers can bypass in `auto`/`off`.
- `enforceLaneCredentialDomain` already treats Claude resolver candidates as `KindOAuth`, and the resolver knows `CLAUDE_SECURESTORAGE_CONFIG_DIR`, `CLAUDE_CONFIG_DIR`, and `$HOME/.claude/.credentials.json`.

So the safe implementation is available: for RFC 0165, a self-driving Claude lane with unknown or unproven kind should be treated as the copied-OAuth assurance class and refused when projection is disabled. V5 does not make that fail-closed rule explicit.

The required C3 tests also need a small tightening. `TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface` covers HOME, and `TestProjectionOffStillValidatesClaudeConfigDir` covers `CLAUDE_CONFIG_DIR` or `CLAUDE_SECURESTORAGE_CONFIG_DIR`, but neither explicitly forces the unknown-kind/default-classification case. An implementation could pass an OAuth-labeled fixture and still let a real unclassified Claude launch fall through as diagnostic.

## Strongest Rebuttal

The strongest rebuttal is that RFC 0165 is intentionally Claude-OAuth-specific, and a build can simply classify every self-driving `claude` lane as OAuth unless a future RFC 0169 registry positively proves another assurance class. If implemented that way, the v5 projection-off gate closes the v4 HOME and `CLAUDE_CONFIG_DIR` counterexample before side effects, and the normal distinct-UID projection remains intact.

That rebuttal is good enough if it is made normative. As written, the SPEC leaves the non-OAuth escape hatch underspecified; it should say unknown/unmodeled Claude credential kind fails closed under projection-off, not launches as diagnostic.

## Required Revision

To clear the gap, make the projection-off predicate fail closed:

- For `adapter == claude` and `agent_loop_mode == self_driving`, treat missing, unknown, or unmodeled credential kind as `OAUTH_COPIED` for RFC 0165 admission.
- Allow the non-OAuth diagnostic exception only with positive pre-launch proof that no Claude OAuth resolver surface is in play; absence of a roster/kind field is not proof.
- Keep the refusal before scratch, FIFO/ACL, session-token minting, supervisor rows, helper/tmux, projection receipt creation, or provider process launch.
- Add tests where projection is disabled, a HOME whole credential has a `refreshToken`, and the launch has no explicit credential kind; assert `provider_credential_projection_disabled_unsupported` and no side effects.
- Add the same unknown-kind/default-classification test for `CLAUDE_CONFIG_DIR`, and parameterize the config-dir variant over `CLAUDE_SECURESTORAGE_CONFIG_DIR` if v5 keeps naming it as a Claude OAuth selector.

## Carry-Forward Check

I did not find a regression in the accepted carry-forwards. Same-user remains unsupported and fail-closed in the intended order; normal distinct-UID B1/B2 projection remains access-token-only; lane samples remain downgrade-only; receipts and events stay token-free; redaction still excludes raw OAuth material, full private paths, provider output, and control-plane tokens; and RFC 0096 separation remains intact. The standing issue is only that projection-off closure depends on a credential-kind classification that the SPEC has not made fail-closed.
