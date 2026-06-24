# RFC 0169 Falsifying Challenge: Hard Claim 2 Does Not Pin Claude's Actual OAuth Store

author: falsifier-reviewer-001

## Claim Challenged

Hard Claim 2 says spawn-fresh placement makes GH #583 structurally impossible without modifying the provider CLI: the placer writes a lane-private `CLAUDE_CONFIG_DIR/.credentials.json`, the claude CLI reads that file rather than a global `~/.claude` credential, source rotation is caught by a before/after copy check, and daemon heartbeat re-placement prevents runtime expiry and refresh-token rotation from escaping into generic lane failure.

That claim does not clear as written. The holder proves Striatum's own resolver path, not the full Claude Code OAuth storage contract, and it defers the refresh-token-isolation mechanism to RFC 0165 while still claiming this RFC closes the 0165 F1/F2 findings.

## Concrete Evidence

1. Current Striatum source is not the provider CLI proof. `go/pkg/laneproviderauth/resolver.go` resolves Claude credentials from `CLAUDE_CONFIG_DIR/.credentials.json`, then `HOME/.claude/.credentials.json`. That is useful telemetry code, but it is not authoritative for what the installed Claude Code binary reads. The holder's proposed `TestClaudeConfigDirOverrideResolvesPlacedFile` can pass while testing only Striatum's resolver.

2. The installed local provider CLI has another selector for the OAuth store. On this host, `claude --version` reports `2.1.187 (Claude Code)`. String inspection of `/home/halbritt/.local/share/claude/versions/2.1.187` shows the OAuth storage path is built as `Jfn() -> join(lK(), ".credentials.json")`, and `lK()` consults `CLAUDE_SECURESTORAGE_CONFIG_DIR` before falling back to the normal config directory. A syscall probe confirms the divergence: with `HOME`, `CLAUDE_CONFIG_DIR`, and `CLAUDE_SECURESTORAGE_CONFIG_DIR` pointing at three different temp directories, `claude auth status` opened `<secure>/.credentials.json`; with only `CLAUDE_CONFIG_DIR` set, it opened `<config>/.credentials.json`.

3. Current Striatum env handling does not close that selector. `go/pkg/mutations/supervision_env.go` allowlists `HOME` and `XDG_CONFIG_HOME`, but not `CLAUDE_CONFIG_DIR` or `CLAUDE_SECURESTORAGE_CONFIG_DIR`; `CLAUDE_CONFIG_DIR` can only be layered by launch env / future placer wiring. `go/pkg/mutations/supervision_lane_config.go` allows arbitrary non-`STRIATUM_` `command_env` keys except `PATH`, so a frozen workflow can still carry `CLAUDE_SECURESTORAGE_CONFIG_DIR` unless the new design explicitly rejects or overwrites it. The holder does not name this variable, update the resolver contract, or require a test where the two Claude selectors disagree.

4. The installed Claude Code binary still writes refreshed OAuth state back to the same store. The embedded code reads `claudeAiOauth.refreshToken`, refreshes with it, and persists returned OAuth fields through the plaintext credential store. Therefore, if the placed file contains a refresh token, the lane can still rotate and write credentials, reopening the RFC 0165 F2 refresh-token desync. If the placed file omits the refresh token, the proposal must prove the running Claude process rereads replacement access tokens from disk before every provider call; a periodic helper heartbeat does not prove "before the lane's next model/MCP action" because there is an interval between ticks, and the process may already have cached the old access token.

## Counterexample

A Claude lane starts with:

```text
CLAUDE_CONFIG_DIR=/run/striatum/lanes/L/claude
CLAUDE_SECURESTORAGE_CONFIG_DIR=/home/striatum-lane/.claude-stale
```

The placer writes `/run/striatum/lanes/L/claude/.credentials.json` and the RFC 0162 resolver samples that path, so admission passes. The installed Claude Code binary reads `/home/striatum-lane/.claude-stale/.credentials.json` for OAuth instead. That can be an aged global/stale credential or a raw-refresh-token-bearing copy. The lane can then 401 in MCP/model use or rotate a refresh token outside the daemon's custody chain, while the daemon's custody receipt says the placed generation was fresh.

The same failure exists if `CLAUDE_SECURESTORAGE_CONFIG_DIR` is inherited from operator env, added by `command_env`, or introduced by Claude Code default behavior in a newer release. The spec needs to make that impossible, not merely assume `CLAUDE_CONFIG_DIR` is the only selector.

## Strongest Rebuttal

The normal case is better than the holder's cited evidence: in a temp-env syscall probe with only `CLAUDE_CONFIG_DIR` set, Claude Code 2.1.187 did open `<config>/.credentials.json`, not `HOME/.claude/.credentials.json`. So a lane-private `CLAUDE_CONFIG_DIR` can be a valid control point when no secure-storage override is present.

That rebuttal does not clear hard claim 2. The spec has to prove all credential selectors the provider CLI can use are either pointed at the placed lane-private directory or refused. It also has to prove the placed payload cannot grant lane-side refresh/writeback authority. The holder does neither; it cites Striatum's resolver and says the refresh-token mechanics are RFC 0165 detail.

## Unanswered Gap

Hard Claim 2 should not clear until the SPEC adds these build-bearing constraints:

- Set or force-clear every Claude OAuth storage selector used by the installed CLI, including `CLAUDE_SECURESTORAGE_CONFIG_DIR`, or refuse launch if any selector resolves outside the lane-private placement directory.
- Extend `ResolveCredential` / readiness sampling to model the actual Claude Code OAuth store precedence, not only `CLAUDE_CONFIG_DIR` and `HOME`.
- Add a CLI-level conformance test, not only a resolver unit test: run `claude auth status` or an equivalent no-network auth command under temp `HOME`, `CLAUDE_CONFIG_DIR`, and `CLAUDE_SECURESTORAGE_CONFIG_DIR`, and assert the opened `.credentials.json` path is the placed file or the launch refuses.
- Add `TestClaudeCommandEnvCannotSelectUnplacedSecureStorage` to prove workflow `command_env` cannot smuggle an alternate OAuth store.
- Add `TestOAuthCopiedNoRawRefreshTokenWriteback` against the actual placed payload: a lane-side refresh attempt must not mutate the operator source or leave the lane with independent refresh authority.
- Add a runtime test proving a long-running Claude process observes daemon replacement before any provider request after expiry; otherwise heartbeat re-placement remains a race window, not a structural closure.

Until those constraints exist, spawn-fresh placement can still be a fresh decoy while the provider CLI reads or mutates a different OAuth store. That is the same #583 class under a new path, so the gate should not clear on hard claim 2.