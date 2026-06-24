# FALSIFIER 2 - RFC 0166 v2 C2 no-false-kill re-attack

author: falsifier-reviewer-002

## Claim Challenged

The v2 holder claims C2 is resolved because it withdraws the unqualified no-false-kill proof, proves safety for tool-fresh / in-tool / `work.heartbeat(local_work=true)` lanes, names the long-silent no-heartbeat interval as a bounded residual, and specifies §7-T2 as the mandated false-kill test (holder v2 §2 and §7-T2).

The challenged claim is narrower: §7-T2 is not the C2 test the seed required. It accepts fresh-session requeue for the exact alive, legitimately-working, tool-silent single-final-seal lane whose last tool call is older than `ToolProgressSeconds`. That is still destructive action by the armed rung.

## Concrete Refutation

Construct the required killer case:

1. A single-final-seal job has zero intermediate artifacts; its only intended seal is terminal `work.complete`.
2. The owning supervised process is alive and legitimately working in a long model-generation / local-computation interval. It is not inside an instrumented tool call, and its last tool call is older than `ToolProgressSeconds`.
3. It emits no `work.heartbeat(local_work=true)` during that interval. This is the holder's own residual class: no tool call, no local-work heartbeat, PTY-only warm, no sealed novelty past `SealedSilenceSeconds`.
4. The rung is explicitly armed with `SealedSilenceSeconds > 0`.

In that state the source predicates line up against the lane. `toolProgressWedged` uses only the latest tool-call start/finish, ignores PTY, and returns true once that base is older than `ToolProgressSeconds` when no tool is in flight (`go/pkg/sessionliveness/liveness.go:772-805`). The v2 holder's AND is `sealedSilenceBreached && toolWedged`, so after the novelty floor crosses `SealedSilenceSeconds`, the rung fires.

The holder then says the residual action is acceptable because it is a fresh-session requeue, not `kill -9` or `run cancel` (holder v2 lines 207-214 and 488-496). But the recovery path it cites is still destructive to the live lane: `requeueJobSameAttempt` is followed by `closeStalledOwningSession`, explicitly so the parked owner cannot wake up to double-work or reclaim the job (`go/pkg/mutations/recovery_decision_tree.go:1353-1380`), and the action record reports `stalled_owner_closed` (`:1409-1421`). That closes/transfers an alive working owner and discards unsealed in-flight reasoning. That is the same class of mid-work intervention C2 was meant to prevent; lowering the severity from host-process kill to session close/requeue does not make it non-destructive.

So the armed-rung test still fails for the exact sentence in the seed: a single-final-seal lane whose last tool call is older than `ToolProgressSeconds` but whose supervised process is alive and legitimately working is destructively acted on by the armed rung.

## Strongest Rebuttal

The strongest defense is real but limited. The holder did fix the overclaim for conformant lanes: `work.heartbeat(local_work=true)` advances `last_tool_call_finished_at` in `HandleHeartbeat` (`go/pkg/mutations/lifecycle.go:843-886`), so a lane that can heartbeat within `ToolProgressSeconds` is spared by the AND. The shadow-first default also matters: with `SealedSilenceSeconds = 0`, landing the code cannot take destructive action by default. And the C2 constraint allowed a narrowed proof with a named accepted residual.

That defense does not make C2 resolved as stated. The constraint was explicitly "before the action arms" and required the named falsification test to show that the alive-working tool-silent single-final-seal lane is not destructively acted on by the armed rung. The v2 test changes the expectation: for the non-heartbeating residual when armed, it asserts only that the action is requeue rather than hard kill / run-cancel. That bakes the failure into the test instead of falsifying it. The operator-grant seam is also listed as P1, so it is not a build-bearing protection before P0 arming.

## Carry-Forward Regression Check

I do not see a regression in the AND-not-OR core itself: the holder preserves `sealedSilenceBreached && ToolProgressWedged` and keeps the #324 predicate as the tool half. I also do not challenge the `local_work=true` heartbeat reprieve for conformant lanes. The shadow-first default is intact.

The standing regression is in the C2 gate semantics: the action ladder is now allowed to close/requeue an alive, legitimately-working residual lane once explicitly armed, while the spec simultaneously labels C2 resolved and titles §7-T2 as a no-destructive-action test. That is not a cleared no-false-kill correction; it is an accepted false-positive class.

## Unanswered Gap / Required Test Shape

A clearing design needs one of these before the action arms:

- a build-bearing reprieve that prevents `transfer_requeue` / owner-session close when the supervised process is alive and the lane is plausibly in a legitimate non-sealed non-tool interval;
- an armed-mode operator-grant / challenge / advisory-only seam that is actually in P0, not deferred to P1;
- or an explicit admission that C2 remains open because the product accepts destructive requeue of this residual lane.

The load-bearing test should arm `SealedSilenceSeconds`, create the exact alive tool-silent single-final-seal residual, and assert no `transfer_requeue`, no `requeue_count` increment, and no `stalled_owner_closed` action occurs while the process is alive and legitimately working. The current §7-T2 would pass while closing the owner, so it cannot clear the C2 gate.