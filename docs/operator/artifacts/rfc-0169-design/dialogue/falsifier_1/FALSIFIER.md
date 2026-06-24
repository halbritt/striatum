# RFC 0169 Falsifying Challenge: Hard Claim 2 Controls the Wrong Boundary

author: falsifier-reviewer-003

## Claim Challenged

Hard Claim 2 says spawn-fresh placement makes GH #583 structurally impossible without modifying the provider CLI. The load-bearing proof is that the daemon writes a fresh, lane-private `CLAUDE_CONFIG_DIR/.credentials.json`; the claude CLI reads that path; a before/after source read catches copy-time rotation; and heartbeat re-placement or cleanup-as-lease-boundary prevents runtime expiry from becoming a generic lane stall.

That proof does not clear. It proves a Striatum resolver and a desired launch environment, not the provider CLI's complete OAuth-store selection or the timing boundary of a live Claude process. A fresh placed file can still be a decoy while Claude reads or mutates a different credential store, and re-placement on a heartbeat does not prove the process observes the new credential before its next provider call.

## Concrete Evidence

1. The holder's CLI-read proof is anchored to Striatum's resolver, not to Claude Code. The holder says `CLAUDE_CONFIG_DIR` wins over `HOME` and therefore "the path the CLI reads is the path the placer writes is the path the gate checks". Current `go/pkg/laneproviderauth/resolver.go` only models `CLAUDE_CONFIG_DIR` and `HOME` for Claude; it has no selector for `CLAUDE_SECURESTORAGE_CONFIG_DIR`.

2. The installed provider CLI has a higher-precedence OAuth-store selector. On this host, `claude --version` is `2.1.187 (Claude Code)`. A no-real-token syscall probe with three different temp credential files showed:

```text
HOME=<home>/.claude/.credentials.json
CLAUDE_CONFIG_DIR=<config>/.credentials.json
CLAUDE_SECURESTORAGE_CONFIG_DIR=<secure>/.credentials.json
```

With all three set, `claude auth status` opened `<secure>/.credentials.json`. With `CLAUDE_SECURESTORAGE_CONFIG_DIR` unset, it opened `<config>/.credentials.json`. So `CLAUDE_CONFIG_DIR` is a valid control only when the secure-storage selector is absent or also pinned.

3. Current Striatum env handling does not make that selector impossible. `supervision_env.go` allowlists `HOME` and XDG paths from the operator environment, not `CLAUDE_CONFIG_DIR` or `CLAUDE_SECURESTORAGE_CONFIG_DIR`. Separately, `supervision_lane_config.go` accepts workflow `command_env` keys except empty keys, `PATH`, and `STRIATUM_`-prefixed control vars, so `CLAUDE_SECURESTORAGE_CONFIG_DIR` can be supplied unless the new design explicitly rejects or overwrites it. The holder does not name that variable or require the resolver/readiness sampler to model it.

4. The copy-time rotation guard only proves the wrong destination if the CLI is pointed elsewhere. Reading the operator source before and after copying can prove that `<lane-private>/.credentials.json` is internally consistent, but it does not prove the running Claude process used that file. A stale secure-storage file can still drive auth and refresh behavior while daemon custody reports the placed generation fresh.

5. The runtime-freshness closure is also not structural yet. The holder asserts heartbeat re-`Place`/re-`ValidateReadiness` fires before the lane's next model/MCP action. No cited source proves Claude Code re-reads the on-disk OAuth file before every provider request, and if the process caches the access token there is a between-tick window where the next provider call can 401 before SIGTERM or re-placement takes effect. That is the RFC 0165 cycle-1 runtime-expiry failure in a new wrapper.

## Counterexample

A workflow or inherited launch configuration sets:

```text
CLAUDE_CONFIG_DIR=/run/striatum/lanes/L/claude
CLAUDE_SECURESTORAGE_CONFIG_DIR=/home/striatum-lane/.claude-stale
```

The placer writes `/run/striatum/lanes/L/claude/.credentials.json`; Striatum's resolver checks that same file; the source pre/post read is stable; admission passes; custody says the generation is fresh. Claude Code 2.1.187 reads `/home/striatum-lane/.claude-stale/.credentials.json` instead. That stale file can produce the original GH #583 401/stall path, or if it contains a refresh token the lane can write rotated OAuth state outside the daemon's custody chain.

A second counterexample exists even when all selectors are pinned: a long-running Claude process crosses access-token expiry between heartbeat checks, uses an in-memory cached token for its next provider request, receives a 401, and only afterward does the helper notice and terminate or reseed. That still leaks runtime credential decay into the lane failure path unless the spec proves per-request disk re-read or classifies the 401 as provider-auth debt before generic recovery budget is touched.

## Strongest Rebuttal

The normal `CLAUDE_CONFIG_DIR` path is real: the local probe confirmed Claude Code opens `<config>/.credentials.json` when `CLAUDE_SECURESTORAGE_CONFIG_DIR` is not set. The holder also chose the right general shape by requiring a real file, lane-private ownership, mode `0600`, and source-stability checks; those are necessary pieces for a CLI-compatible design.

That rebuttal is insufficient because the hard claim is structural impossibility. Structural closure requires enumerating and controlling every OAuth-store selector the installed CLI can use, plus proving the running process cannot use stale cached credentials between daemon checks. A resolver unit test cannot stand in for provider CLI conformance.

## Unanswered Gap

Hard Claim 2 should not clear until the SPEC adds these build-bearing constraints:

- Pin, clear, or refuse every Claude OAuth-store selector used by the installed CLI, including `CLAUDE_SECURESTORAGE_CONFIG_DIR`, and fail closed with a typed reason when any selector resolves outside the lane-private placement directory.
- Extend `ResolveCredential` and readiness sampling to match the actual Claude Code OAuth-store precedence, not only `CLAUDE_CONFIG_DIR` and `HOME`.
- Add a CLI-level conformance test: run `claude auth status` or an equivalent no-network command under temp `HOME`, `CLAUDE_CONFIG_DIR`, and `CLAUDE_SECURESTORAGE_CONFIG_DIR`, and assert the opened `.credentials.json` is the placed file or launch refuses.
- Add a command-env guard test proving workflow `command_env` cannot smuggle an alternate Claude OAuth store.
- Prove the placed `OAUTH_COPIED` payload gives no lane-side raw-refresh-token writeback or independent rotation authority against the actual CLI payload, not just by deferring field mechanics to RFC 0165.
- Strengthen the runtime test so the post-expiry case models an in-memory cached access token; either prove disk re-read before every provider call, prove the heartbeat/SIGTERM interval preempts the first post-expiry provider call, or explicitly classify the residual 401 as provider-auth reseed debt before any generic requeue/transfer counter increments.

Until those constraints exist, spawn-fresh placement does not make #583 structurally impossible. It can produce a fresh file that Striatum verifies while the provider CLI reads, refreshes, or caches a different credential source.