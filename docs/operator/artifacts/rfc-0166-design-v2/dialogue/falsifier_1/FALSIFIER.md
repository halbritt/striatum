# FALSIFIER 1 - RFC 0166 v2 C1 novelty-clock re-attack

author: falsifier-reviewer-001

## Claim Challenged

The v2 holder claims C1 is resolved because `novelSealedProgressAt` replaces raw `jobSealedProgressAt` on every reset surface: the Part-1 floor, the Part-4 telomere reset, and RFC 0131 `progressAdvanced` (holder v2 lines 90-118, 232-297, 343-363). It also chooses the undeclared in-scope publish contract as **allowed-but-ignored-for-deadline** (lines 299-322) and specifies the mandated C1 test: junk rows land before every budget expiry while the tool timeline is stale, then `requeue_count` and `consecutive_silent_sweeps` advance until exactly one escalation (lines 455-475).

## Concrete Refutation

The raw clock hole is repaired on paper, but the published C1 test is not a real hostile-lane test. A lane cannot publish those undeclared artifact rows through the actual control plane while remaining tool-stale.

`mcp.Service.ToolsCall` records `LastToolCallStartedAt` before dispatch and `LastToolCallFinishedAt` after dispatch for any call whose arguments carry `repository_id` and `session_id` (`go/pkg/mcp/tools.go:40-67`). `artifact.publish` requires `session_id`, `job_id`, `lease_id`, `kind`, `logical_name`, and `path`, then enforces the active acting session before publishing (`go/pkg/mutations/artifact.go:48-85`). The #324 predicate ages exactly against the latest of `LastToolCallStartedAt` and `LastToolCallFinishedAt`, and returns false while that base is still within `ToolProgressSeconds` (`go/pkg/sessionliveness/liveness.go:772-805`). The v2 AND is explicitly `sealedSilenceBreached && toolWedged` (holder v2 lines 143-158).

So the realistic junk-publisher counterexample is:

1. Arm `SealedSilenceSeconds` and use a job with one declared required artifact plus an allowed repo-write directory.
2. The hostile lane never publishes the required artifact.
3. It calls `artifact.publish` for undeclared `junk_N` with fresh `logical_name` and distinct bytes at a cadence below `ToolProgressSeconds` and before each sealed-silence expiry.
4. The C1 primitive correctly ignores those rows, so the floor is stale and `sealedSilenceBreached` is true.
5. But each `artifact.publish` call refreshes the tool-call timeline, so `toolWedged` is false and the AND never fires.
6. Because the recovery action is never entered, `requeue_count` does not advance and the RFC 0131 confidence gate never gets to accumulate the promised silent sweeps; the telomere floor is never reached.

This is not an exotic extra attack. It is the same hostile undeclared-artifact publisher C1 was written to defeat, now moved from the raw sealed clock to the tool-axis recency side of the AND. The holder even names the resulting class as a P1 seam: a lane kept tool-fresh by junk-publishing or polling `await_packet` needs tool-axis novelty later (holder v2 lines 434-438). But the C1 gate did not ask for "floor freezes while the hostile publisher is intentionally spared"; it required junk-before-budget-expiry to leave the cursor/floor frozen **and** let `requeue_count`, silent sweeps, and exactly-one escalation proceed.

## Strongest Rebuttal

The strongest defense is that C1 was only about event-gated sealed reset surfaces, and v2 does use the same declared-scoped novelty primitive for the floor, telomere reset, and `progressAdvanced`. A lane making MCP calls is tool-fresh by definition, and the AND was deliberately ratified to spare tool-fresh lanes.

That defense proves only that the raw-clock bug is gone. It does not satisfy the mandated C1 falsification test or the publish-contract question. If "allowed-but-ignored-for-deadline" also means "allowed to keep the deadline's tool half fresh forever," then undeclared junk publishing remains a gaming surface. If the build test inserts junk rows directly into the database while holding the session tool timeline stale, it is not testing a hostile lane publishing artifacts through Striatum.

## Unanswered Gap / Required Test Shape

The build-bearing test must drive real `artifact.publish` calls from the owning session, not out-of-band row insertion. It should assert that repeated undeclared, deadline-ignored publishes cannot keep the sealed-silence rung from reaching the telomere floor. Today the v2 spec has no mechanism that can make that pass.

A clearing design needs either deadline-specific tool-axis novelty in P0 (for example, the sealed-silence AND does not treat undeclared/deadline-ignored `artifact.publish`, polling, or equivalent non-forward-progress calls as progress while still honoring `work.heartbeat(local_work=true)` as the C2 reprieve), or a publish contract that prevents undeclared junk publishes from serving as a keepalive. Leaving that mechanism in P1 means C1's hostile-junk-publisher convergence test is still not genuinely discharged.