# RFC 0165 v4 Falsifying Challenge: Projection-Off Leaves A Raw-Token Lane Surface

author: falsifier-reviewer-004

## Challenge

C3 does not clear as written. The v4 holder closes the same-user source-read route and the normal distinct-UID B1/B2 projection route, but it still leaves a distinct-UID Claude launch path where `provider_credential_projection=off` can skip the projection. In that path, the SPEC does not require launch refusal, does not scrub or overwrite a pre-existing lane credential, and does not run the C3 all-surfaces no-refresh-token scan before the Claude process starts.

That is a material custody route because the RFC's starting state is a lane-home Claude credential file that can be a copied whole OAuth credential containing a stale `refreshToken` (`docs/rfcs/0165-claude-provider-credential-freshness.md:13-32`). A distinct-UID lane that starts with projection disabled can still read its own `$HOME/.claude/.credentials.json` or a `$CLAUDE_CONFIG_DIR/.credentials.json` named by launch env. The lane may not be able to refresh successfully if the token is stale, but C3 is stricter: the lane must not obtain raw refresh-token custody by any route.

## Claim Challenged

The holder claims C3 is discharged because same-user is refused, distinct-UID source read has no path, projection file/env/socket carry only access tokens, and recovery re-sample is downgrade-only (`HOLDER.md:148-159`). The challenged part is the unconditional "by any route" claim. The same holder also says only `provider_credential_projection=off` can skip projection for distinct-UID delivery (`HOLDER.md:430-438`) and lists that bypass as a carry-forward boundary (`HOLDER.md:181-182`).

## Evidence

The exhaustive C3 test is scoped to a projected distinct-UID lane: it enumerates the resolver-proven destination, `$CLAUDE_CONFIG_DIR/.credentials.json`, `$HOME/.claude/.credentials.json`, credential-bearing env entries, and the B2 socket response, then asserts no `refreshToken` (`HOLDER.md:646-653`). That is the correct scan when projection has run, but the test matrix does not name the corresponding projection-disabled case. The carry-forward tests cover `provider_auth_gate=off`, not `provider_credential_projection=off` (`HOLDER.md:596-604`).

Current source makes the fallback concrete. Workflow `command_env` can provide provider env such as `CLAUDE_CONFIG_DIR`; it is only forbidden from setting `PATH` or `STRIATUM_*` keys (`go/pkg/mutations/supervision_lane_config.go:411-450`), and that launch env is layered into the lane env (`go/pkg/mutations/supervision_env.go:102-117`). The Claude resolver then chooses `$CLAUDE_CONFIG_DIR/.credentials.json` if present, otherwise `$HOME/.claude/.credentials.json` (`go/pkg/laneproviderauth/resolver.go:78-90`). If the projection gate is skipped, the SPEC has not stated that those resolver surfaces are still validated for `refresh_token_absent` before launch.

## Concrete Counterexample

1. The host has the pre-RFC lane credential at `~striatum-lane/.claude/.credentials.json`, copied from the operator profile and containing `claudeAiOauth.refreshToken`. This is the incident shape the RFC describes.
2. A Claude lane launches with a distinct lane OS user, so the same-user refusal is not triggered.
3. The launch uses `provider_credential_projection=off`.
4. Because projection is skipped, the daemon does not write the B1 access-token-only file, does not force B2 as the only credential source, and does not run a required all-surfaces scan for this disabled-projection route.
5. Claude resolves its credential from `$HOME/.claude/.credentials.json` or from a workflow-provided `$CLAUDE_CONFIG_DIR/.credentials.json`. If that lane-readable file is the old whole credential, the lane has raw refresh-token custody.

This refutes C3 without relying on same-user mode, lane-authored freshness, or Striatum control-plane token leakage.

## Strongest Rebuttal

The strongest rebuttal is that `provider_credential_projection=off` is documented unsafe, emits a bypass event, and marks the dependency `disabled`. That can be an operator break-glass story, but it is not a proof of C3. A disabled dependency row does not remove a lane-readable OAuth file, and "documented unsafe" is still a route if it starts a Claude process that can read a refresh token.

The gate could clear with a narrower claim: C3 holds only when projection is not disabled, and projection-disabled Claude OAuth launch is an accepted risk. The holder does not make that narrowing; it presents C3 as resolved and keeps the disabled-projection launch path in the same implementation contract.

## Required Revision

To clear C3, make one of these closures explicit:

- For Claude OAuth self-driving lanes, `provider_credential_projection=off` fails closed before scratch, session-token mint, supervisor rows, helper/tmux, or process launch. It can remain a diagnostic flag for non-OAuth or non-Claude paths, but not a live Claude OAuth bypass.
- If the flag must launch Claude, it must still run the C3 surface scan first: resolver-proven destination, `$CLAUDE_CONFIG_DIR`, `$HOME/.claude`, credential-bearing env entries, B2/helper settings, and any launcher-provided credential path or fd. Any `refreshToken`, untrusted credential path, or unprovable surface refuses launch.
- Add `TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface`: seed a distinct-UID lane home with a whole Claude credential containing a known `refreshToken`, set `provider_credential_projection=off`, and assert a typed precondition refusal before side effects.
- Add `TestProjectionOffStillValidatesClaudeConfigDir`: set `provider_credential_projection=off` and `CLAUDE_CONFIG_DIR` to a lane-readable directory containing a whole credential; assert refusal and no Claude process.

## Carry-Forward Check

The same-user policy is directionally sound: `RunAsUser == ""` and resolved daemon-uid equality are refused before launch in the intended design. I also did not find a separate raw-token inherited-fd path in the current launch shape; the material route is the lane-readable credential file left reachable when projection is disabled. The normal B1/B2 projection, RFC 0096 control-plane separation, redaction contract, and downgrade-only recovery sample are intact on their intended path. The standing regression is that the spawn-time projection gate and `refresh_token_absent` proof are bypassable by the SPEC's own `provider_credential_projection=off` mode.
