# FALSIFIER 2 - RFC 0166 v2 C2 and carry-forward re-attack

author: falsifier-reviewer-004

## Claim Challenged

The v2 holder says C2 is resolved by narrowing the proven no-false-kill property
to tool-fresh, in-tool, or `work.heartbeat(local_work=true)` lanes, while naming
the long-silent legitimate-think interval as an accepted bounded residual. The
holder also titles the mandated test as: "an armed rung must not destructively act
on an alive, legitimately-working tool-silent single-final-seal lane."

That is the challenged claim. The narrowed proof is honest for conformant
heartbeating lanes, but the v2 test accepts destructive transfer/requeue for the
exact armed killer case C2 required us to construct. That does not clear the
no-false-kill gate before arming.

## Concrete Counterexample

Construct the required C2 lane:

1. The job is single-final-seal: it has no intermediate required artifact or
   verdict, and its only intended seal is a terminal `work.complete`.
2. The supervised process is alive and legitimately working in a long local
   computation or model-generation interval.
3. The process is not inside an instrumented tool call, its last tool call is
   older than `ToolProgressSeconds`, and it does not emit
   `work.heartbeat(local_work=true)` during this interval.
4. `SealedSilenceSeconds` is explicitly armed (`> 0`) and the novelty floor has
   aged past that budget.

The source predicates make the rung fire. `ToolProgressWedged` ages only the
latest tool-call start/finish, ignores PTY activity, returns false only for
zero-tool-history, in-tool, or disabled policy, and otherwise returns true when
that base misses `ToolProgressSeconds`
(`go/pkg/sessionliveness/liveness.go:772-805`). The v2 holder keeps the AND as
`sealedSilenceBreached && toolWedged` (holder v2 lines 143-158). In the scenario
above both halves are true while the process is alive and doing real work.

The v2 holder then says this residual is acceptable because the action is a
fresh-session requeue rather than `kill -9` or `run cancel` (holder v2 lines
207-214, 488-496). But the cited recovery path is still destructive to the live
owner. Before requeue, the active lease can be force-expired
(`recovery_decision_tree.go:1331-1339`). The transfer then calls
`requeueJobSameAttempt`, and if `closeStalledOwner` is set it calls
`closeStalledOwningSession` (`:1353-1380`). That helper updates the still-active
session to `state = 'closed'`, records `close_reason =
'recovery_stalled_transfer'`, and appends a `session.closed` event
(`:1647-1703`). The action audit also reports `stalled_owner_closed`
(`:1409-1421`).

Closing the active owner and transferring the job away from an alive working
process discards the unsealed in-flight reasoning. It is less severe than
host-process kill, but it is still destructive action by the armed rung against
the C2 killer case.

## Strongest Rebuttal

The holder has a real rebuttal for conformant agents. `work.heartbeat` with
`local_work=true` is build-bearing: `HandleHeartbeat` appends
`LastToolCallFinishedAt` to the recorded activity columns when `local_work` is
set (`go/pkg/mutations/lifecycle.go:843-886`), and every `tools/call` also
stamps tool-call start/finish when the request carries `repository_id` and
`session_id` (`go/pkg/mcp/tools.go:54-67`). A lane that heartbeats within
`ToolProgressSeconds` therefore keeps `toolWedged == false` and is spared by the
AND. The shadow-first default is also meaningful: with
`SealedSilenceSeconds = 0`, the destructive action is not armed.

Those facts narrow the proof, but they do not satisfy the armed C2 test. The v1
constraint was "GATE before the action arms"; it required either a real reprieve
for legitimate non-sealed non-tool intervals before destructive action, or an
honest narrowing plus a bounded residual covered by the advisory default and
operator-grant seam. In v2, the operator-grant seam is P1, not P0. Once an
operator arms `SealedSilenceSeconds`, the exact non-heartbeating alive residual
is transferred and closed. The test then declares success as long as the action
is not hard kill or run-cancel. That rewrites the test to permit the failure it
was supposed to catch.

## Carry-Forward Regression Check

I do not find a regression in the AND-not-OR expression itself. The holder still
requires `sealedSilenceBreached && ToolProgressWedged`, and still reuses the #324
tool-progress predicate. I also do not challenge the local-work-heartbeat
reprieve for conformant lanes, the shadow-first default, or the single
idempotent escalation shape.

The standing regression is semantic: the Part 4 action ladder is now explicitly
allowed to close/requeue an alive legitimately-working residual lane once armed,
while the SPEC labels C2 "resolved" and names §7-T2 as a no-destructive-action
test. That leaves an accepted false-positive class in the action rung rather
than a cleared no-false-kill correction.

## Required Clearing Test

The load-bearing test must arm `SealedSilenceSeconds`, create the exact
single-final-seal alive tool-silent residual, and assert that recovery does not
perform `transfer_requeue`, does not increment the requeue counter, and does not
record `stalled_owner_closed` while the supervised process is alive and
legitimately working.

If the product decision is instead to accept that armed requeue of this residual
is allowed, the SPEC should say C2 remains a bounded accepted false-positive
risk rather than a resolved no-false-kill property. As written, C2 should not
clear.
