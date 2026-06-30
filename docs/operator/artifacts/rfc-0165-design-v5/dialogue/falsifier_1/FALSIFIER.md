# RFC 0165 v5 Falsifying Challenge: Credential-Domain Guard Can Preempt The C2 Same-User Floor
author: falsifier-reviewer-001

## Challenge

The v4 C1 latest-row attack is not sustained against the v5 holder as written.
The holder adds daemon-owned `provider_credential_launch_bindings`, ties positive
recovery freshness to the stalled job/session/supervisor launch binding, keeps
the latest `provider_auth_dependencies` row admission-only, records
provider-auth debt by `binding_id` and `destination_generation_id`, and names
both required tests:
`TestRecoveryClassifiesExpiredLaunchGenerationDespiteNewerProjection` and
`TestRecoveryPerSessionDecayDebtSurvivesNewerProjection`.

The strongest remaining gap in my assigned lens is C2 ordering. The holder says
same-user Claude OAuth refusal is the first Claude credential floor and returns
`provider_credential_same_user_unsupported` in `provider_auth_gate=auto`, `off`,
and `required` before side effects. But the implementation order it specifies is:

```text
loadSupervisionStartConfig
enforceLaneCredentialDomain
runSuperviseClaudeSameUserPrecondition
runSuperviseClaudeProjectionDisabledGate
runSuperviseProviderAuthGate
...
```

`enforceLaneCredentialDomain` can fail first for a Claude lane with a
repo-relative or repo-inside credential selector, returning
`lane_credential_cache_inside_repo` or
`lane_uncovered_credential_selector_inside_repo` before the same-user
precondition runs. That still fails closed before scratch, FIFO/ACL, session
token minting, supervisor rows, helper/tmux setup, or provider process launch,
so it does not reopen raw refresh-token custody. It does, however, falsify the
holder's broader typed-floor claim for same-user Claude OAuth starts unless the
SPEC explicitly makes the credential-domain guard a higher-priority exception.

## Claim Challenged

The holder's C2 claim is that same-user Claude OAuth refusal precedes the generic
provider-auth gate and every side effect, and that same-user launches in
`auto`, `off`, and `required` all return the typed
`provider_credential_same_user_unsupported` remediation. It also labels
same-user refusal as "the first Claude credential floor."

The missing condition is precedence against the existing credential-domain
guard. The holder moves the same-user check before `runSuperviseProviderAuthGate`,
but leaves it after `enforceLaneCredentialDomain`.

## Evidence

The current source order is exactly the one the holder cites:
`go/pkg/mutations/supervision_control.go::HandleSuperviseStart` loads the start
config, then calls `enforceLaneCredentialDomain`, then calls
`runSuperviseProviderAuthGate`; scratch creation, token minting, supervisor rows,
and process launch occur later.

`go/pkg/mutations/credential_domain_guard.go::enforceLaneCredentialDomain`
resolves Claude OAuth credential candidates and rejects modeled credential caches
inside the target repository with `lane_credential_cache_inside_repo`. It also
rejects provider-owned credential selectors that are not modeled but resolve
inside the repository with `lane_uncovered_credential_selector_inside_repo`.

`go/pkg/laneproviderauth/resolver.go::ResolveCredentialCandidates` treats
`CLAUDE_SECURESTORAGE_CONFIG_DIR`, then `CLAUDE_CONFIG_DIR`, then
`HOME/.claude/.credentials.json` as Claude credential surfaces. Therefore a
same-user Claude OAuth launch whose environment points one of those selectors
inside the target repository can be refused by the credential-domain guard before
the proposed same-user precondition executes.

The named C2 test, as stated by the holder, is parameterized over
`provider_auth_gate=auto,off,required` and checks the typed same-user refusal
before side effects. It does not explicitly include credential-selector
variations that force `enforceLaneCredentialDomain` to fire before the proposed
same-user check.

## Strongest Rebuttal

The best rebuttal is that `enforceLaneCredentialDomain` is not the generic
provider-auth gate that v4 carried forward as the C2 caveat. It is a separate
repository-boundary precondition, it is fail-closed, and it runs before all of
the same side effects C2 cares about. If the product wants repo credential-domain
violations to outrank same-user remediation, then this is not a custody defect.

That rebuttal only holds if v5 says so explicitly. As written, the holder makes
the stronger typed-floor assertion for same-user Claude OAuth lanes and does not
name this precedence exception or test it.

## Unanswered Gap

Either move `runSuperviseClaudeSameUserPrecondition` before
`enforceLaneCredentialDomain`, or state that credential-domain violations are a
higher-priority fail-closed precondition than the same-user remediation and add a
test proving the intended precedence. A useful test shape is a same-user Claude
OAuth lane with `provider_auth_gate=required` and `CLAUDE_CONFIG_DIR` or
`CLAUDE_SECURESTORAGE_CONFIG_DIR` resolving inside the target repository; assert
the chosen error code and assert no scratch, token, supervisor row, projection
file, custody receipt, helper/tmux state, or Claude process exists.

## Bottom Line

C1 appears discharged under the assigned overlapping-session race: v5 no longer
uses a fresh G2 latest lane-user row as positive proof for an expired G1 stalled
session. C2 is improved over v4 because the generic provider-auth gate no longer
preempts same-user refusal, but the SPEC still leaves an earlier
credential-domain guard that can preempt the promised typed same-user floor.
This is a bounded ordering gap, not a raw-token custody reopening.
