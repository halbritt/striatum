# RFC 0165 v4 Falsifying Challenge: Projection-Off Reopens the Old Refresh-Token File Route
author: falsifier-reviewer-002

## Challenge

C3 is not genuinely resolved. The v4 holder closes the same-user route and the nominal distinct-UID projection route, but it still leaves `provider_credential_projection=off` as a distinct-UID Claude launch path that can skip the projection. The SPEC does not require that disabled-projection path to refuse Claude OAuth launch, overwrite/scrub a pre-existing lane credential, force a controlled empty Claude config dir, or run the same all-surfaces no-refresh-token scan before the process starts.

That matters because the RFC's starting incident is exactly a lane-home Claude credential file that is a point-in-time copy of the operator credential and can contain a stale `refreshToken` (`docs/rfcs/0165-claude-provider-credential-freshness.md:13-33`). If projection is skipped, the daemon has not delivered the access-token-only B1 file or B2 broker, and the existing Claude resolver path can fall back to a lane-readable credential file. A distinct-UID lane then obtains raw refresh-token custody by the old file route.

## Claim Challenged

The holder claims C3 is discharged because no lane can reach the rotating refresh token by any route: same-user is refused; distinct-UID source read has no read path; projection file/env/socket are access-token-only; and the recovery sample is downgrade-only (`HOLDER.md:150-159`, `HOLDER.md:208-224`).

The challenged claim is the unconditional "by any route" part. The SPEC itself keeps one launch route where the access-token-only projection is not present.

## Evidence

The v4 SEED asks this lane to test C3 against any raw-token route, including a lane-readable file/env/fd/fallback surface, and requires scanning every lane-readable credential surface named by the launch environment (`SEED.md:68-77`, `SEED.md:85-90`).

The holder's all-surfaces test is scoped to the normal projected distinct-UID lane: enumerate the resolver-proven destination, `$CLAUDE_CONFIG_DIR/.credentials.json`, `$HOME/.claude/.credentials.json`, credential-bearing env entries, and the B2 socket; assert access token and no refresh token (`HOLDER.md:646-653`). That is the right test for the normal gate, but it is not stated for the projection-disabled route.

The holder then says only `provider_credential_projection=off` can skip the projection for distinct-UID delivery, while same-user refusal still cannot be skipped (`HOLDER.md:430-438`). The required carry-forward tests cover `provider_auth_gate=off`, not `provider_credential_projection=off` (`HOLDER.md:667-676`). So the route that actually skips projection has no named no-refresh-token test.

Current source makes the fallback surface concrete. The Claude resolver uses `$CLAUDE_CONFIG_DIR/.credentials.json` first and `$HOME/.claude/.credentials.json` otherwise (`go/pkg/laneproviderauth/resolver.go:78-90`). Workflow `command_env` is layered into the supervised lane environment (`go/pkg/mutations/supervision_env.go:102-113`), and the parser only forbids `PATH` and `STRIATUM_*` keys (`go/pkg/mutations/supervision_lane_config.go:411-450`). In the projected path, the new projector is supposed to validate that `CLAUDE_CONFIG_DIR` matches the trusted destination. In the disabled-projection path, the SPEC does not say that validation still runs.

## Concrete Counterexample

1. The host has the pre-RFC or manually repaired lane credential at `~striatum-lane/.claude/.credentials.json`, containing `claudeAiOauth.refreshToken`. This is not hypothetical; it is the current problem statement and live repair shape for #583.
2. A Claude lane launches with a distinct lane OS user, so the same-user precondition passes.
3. The operator or workflow enables `provider_credential_projection=off`. Per v4, this can skip projection for distinct-UID delivery.
4. Because projection is skipped, no B1 access-token-only file overwrites the old lane file, no B2 broker becomes the only credential source, and no spec-required scan refuses the old file.
5. Claude resolves its credential from `$CLAUDE_CONFIG_DIR/.credentials.json` or `$HOME/.claude/.credentials.json`. If that lane-readable file is the old copied credential, the lane reads the raw rotating refresh token and can attempt the normal Claude refresh flow.

That is C3's forbidden route: a distinct-UID lane-readable fallback file yields a refresh token even though same-user is refused and the normal projector is sound.

## Strongest Rebuttal

The best rebuttal is that `provider_credential_projection=off` is explicitly documented unsafe, marks the dependency `disabled`, and is a break-glass escape outside the C3 proof. That would be defensible only if the SPEC said the no-refresh-token guarantee is suspended under that flag and the gate cannot clear while the flag is used.

The v4 text does not draw that boundary. It presents C3 as cleared, keeps the flag in the launch design, and says the flag can skip projection. A launch path that is "documented unsafe" is still a route if it starts a Claude lane that can read a stale whole-credential file.

## Required Revision

To clear C3, the SPEC needs one of these explicit closures:

- Preferred: for Claude OAuth self-driving lanes, `provider_credential_projection=off` fails closed before launch, just like same-user mode. It may be a diagnostic flag for non-OAuth adapters, but not a live Claude launch bypass.
- If the flag must remain for Claude, it cannot simply skip projection and start the lane. It must first prove `refresh_token_absent` across the same surfaces named by C3: resolver-proven destination, `$CLAUDE_CONFIG_DIR`, `$HOME/.claude`, credential-bearing env entries, B2/helper settings, and any inherited fd/config path the launcher gives the lane. If any lane-readable surface contains `refreshToken` or resolves to an untrusted credential path, launch refuses.
- Add `TestProjectionOffCannotLaunchWithRefreshTokenCredentialSurface`: seed a distinct-UID lane home with a whole Claude credential containing a known `refreshToken`, set `provider_credential_projection=off`, launch Claude, and assert a typed precondition refusal before scratch/token/supervisor/process.
- Add `TestProjectionOffStillValidatesClaudeConfigDir`: with projection disabled and workflow `CLAUDE_CONFIG_DIR` pointing at a lane-readable credential directory outside the trusted destination, assert refusal and no process.

## Carry-Forward Check

I did not find a same-user raw-token path in the intended v4 policy: `RunAsUser == ""` and resolved-uid equality are refused before launch. The nominal distinct-UID B1/B2 access-token-only projection, RFC 0096 control-plane separation, redaction contract, and downgrade-only recovery sample are directionally intact. The standing C3 regression is the explicit projection-disabled fallback path, which can leave the old raw-refresh-token lane file in place and unscanned.
