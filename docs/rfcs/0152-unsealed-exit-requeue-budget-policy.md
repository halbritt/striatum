# RFC 0152: Recovery budget policy for `agent_exited_unsealed` reviewer lanes

Status: proposed
Date: 2026-06-20
author: proposer-claude-opus-4-8

## Summary

A confirmed-dead supervised agent that engaged the work protocol and emitted
PTY output but never called `work.complete` is classified
`agent_exited_unsealed` and recovered on a deliberately **smaller** requeue
budget (`recovery_policy.max_unsealed_requeues`, default **1**, clamped to
`<= max_requeues` whose default is 2). At `requeue_count == 1` the class hits
`current >= limit` and — because a `pty_confirmed_dead` basis skips the RFC 0131
confidence-gate debounce by design — escalates the **whole run** to
`needs_operator` immediately.

GH issue #478 reports that on multi-stage committee runs a single transient
reviewer-lane unsealed exit (lane `claude_code`, e.g. `review_final_claude`)
consumes that one-respawn budget and strands the run with one reviewer missing,
even though a fresh session re-reviews fine on operator re-drive — and that it
recurred on a **stable daemon with no flap**, so it is not purely an
infrastructure transient.

This RFC asks the maintainer to decide whether, and how, to loosen the
`agent_exited_unsealed` escalation policy. It does **not** implement a change:
the current behavior is a ratified decision with a pinned invariant test, so a
direct constant bump is out of bounds for triage.

## Affected issue

- **#478** — `claude_code` reviewer lane: `agent_exited_unsealed` requeue budget
  (`limit=1`) escalates the whole run on a single transient agent exit.

Adjacent/related (do not fold in; cross-reference only):

- **#289 / D198** — the decision that introduced the `agent_exited_unsealed`
  class and its smaller-budget policy (the contract this RFC would amend).
- **#381** — a separate reserved recovery-policy RFC slot (distinct surface;
  not this budget). Kept distinct per triage steer.

## The unresolved decision

D198 (accepted) ratified the smaller unsealed budget with an explicit rationale:
a *systematic* unsealed exit (turn-end / context-budget / rate-limit death
mid-task) rarely self-heals on repeat respawn, so the class should reach the
operator **sooner** than a hard crash, burning less compute, and pointing the
operator at the per-job worktree (the deliverable may be complete-but-unsealed;
the daemon must not seal on the agent's behalf — that would forge attestation).

#478 supplies the counter-evidence D198's own "Revisit" trigger anticipated:
for a stateless reviewer lane a *transient* unsealed exit is common and a fresh
session almost always succeeds, so escalating after a single respawn maximizes
operator interruptions for the most frequent `needs_operator` cause in committee
runs. The two readings conflict on the **same constant**; resolving them is a
product judgment, not a triage edit.

The decision to make:

1. **Keep** the global default at 1 (status quo: optimize for not wasting
   compute on systematic exits; accept operator interruptions on transient ones).
2. **Raise** the global default for `agent_exited_unsealed` (e.g. to 2, equal to
   the hard-crash budget) — favors self-healing transient exits at the cost of
   the "escalate sooner than a crash" guarantee and the pinned invariant.
3. **Differentiate** the budget by lane kind / job type rather than globally —
   e.g. a larger budget for stateless reviewer/review lanes (where a fresh
   session is a clean retry) while keeping the tight budget for stateful
   repo-write lanes (where a repeated unsealed exit signals systematic failure).
   This preserves D198's intent for the lanes it was written for.
4. **Auto-grant one fresh-attempt** (a fresh-session respawn, distinct from a
   same-attempt requeue) before escalating, so the first transient exit always
   gets a clean session without changing the steady-state budget semantics.
5. **Detect a complete-but-unsealed deliverable** (a published artifact / sealed
   verdict present in the worktree) and route differently — explicitly listed as
   out-of-scope by D198 because naive auto-completion would forge attestation;
   would need its own forgery-resistant signal.

## Current evidence and claim boundaries

Anchored at `origin/main` @ `d5d3cd8`:

- **Constant + invariant** — `go/pkg/mutations/recovery_decision_tree.go`:
  `defaultMaxUnsealedRequeues = 1`, `defaultMaxRequeues = 2`;
  `recoveryPolicyFromWorkflow` parses `recovery_policy.max_unsealed_requeues`
  and clamps it to `<= max_requeues`.
- **Escalation path** — in the same file, the budget branch sets
  `limit = policy.maxUnsealedRequeues` for the unsealed class; at
  `current >= limit` the run escalates. The RFC 0131 confidence-gate debounce is
  explicitly bypassed for a confirmed-dead / `pty_confirmed_dead` basis
  (`gateApplies := !sessionDead && !confirmedDead() && probeBasis() ==
  ProbeBasisDeadlineElapsedOnly`), which matches the `probe_basis` reported in
  #478 — so this class is escalate-immediately by construction, not a tuning
  oversight.
- **Pinned invariant test** — `go/pkg/mutations/dx_289_test.go`
  (`TestRecoveryPolicyUnsealedBudget`) asserts
  `defaultMaxUnsealedRequeues < defaultMaxRequeues`;
  `TestSweepUnsealedExitEscalatesOnSmallerBudget` pins the escalate-at-count-1
  behavior. Both are green at the anchor. Any change to option 2/3/4 above must
  revisit these tests, which is precisely why this is not a triage-safe edit.
- **Operator-side workaround already exists** — `recovery_policy` accepts a
  per-workflow `max_unsealed_requeues` override today, so a committee workflow
  can already raise its own budget without a code change. Whether the *global
  default* should move is the product question this RFC raises.

**Claim boundaries:** this RFC does not assert which option is correct, does not
re-classify the stall class, and does not touch attestation/sealing. It asserts
only that the constant is contract-governed (D198) and pinned, so the resolution
belongs to a decision, not to a triage constant bump.

## Why a direct FIX was rejected

- Changing `defaultMaxUnsealedRequeues` from 1 to 2 (the issue's literal
  suggestion) directly contradicts ratified **D198**, whose accepted rationale
  is that this class must escalate **sooner** than a hard crash.
- It **breaks the pinned invariant test** (`defaultMaxUnsealedRequeues <
  defaultMaxRequeues`) — a triage FIX is supposed to be the smallest reversible
  change against an accepted invariant, not a reversal of one.
- The change **broadens** behavior (more autonomous respawns, deferred operator
  escalation, more compute burned on potentially-systematic exits); it does not
  narrow behavior to a cited invariant, which is the only carve-out that would
  let a hot-dim change skip an RFC.
- There is no failing proof obtainable for a FIX that does not itself require
  re-opening D198, so per the routing engine the correct route is RFC, not a
  fabricated FIX.

## Hot blast-radius dims that forced RFC

- **cross_team_contract** — `agent_exited_unsealed` budget + escalation timing is
  a ratified recovery-policy contract (D198) with a documented operator
  remediation and a pinned invariant test. Other surfaces (doctor recovery gate,
  metrics necrosis taxonomy, escalation remediation copy) read this
  classification.
- **product_safety_claim** — D198's safety posture ("escalate sooner, don't burn
  compute on systematic exits, never seal on the agent's behalf / forge
  attestation"). Any loosening must show it does not weaken the
  no-forged-attestation guarantee or the bounded-escalation guarantee.

(Not hot: no public API signature, no persisted schema or migration — the budget
shares the existing `requeue_count` counter and the stall class is a runtime
string; no wire format change.)

## Alternatives / rejected direct patches

- **Bump the global default to 2** — rejected as a triage edit: reverses D198 and
  breaks the invariant test (see above). Could become the accepted resolution
  only via this RFC if option 2 is chosen.
- **Document the per-workflow `max_unsealed_requeues` override and close** —
  partial: it unblocks any single workflow today, but does not resolve the
  product question of the *global default* the issue is about, and most committee
  workflows would each have to opt in. Reasonable interim mitigation; not a
  decision.
- **Loosen the confidence-gate to debounce confirmed-dead unsealed exits** —
  rejected: the gate intentionally excludes a confirmed-dead oracle; debouncing a
  real death signal would regress the dead-lane escalation timing the gate was
  built to preserve.

## Handoff

Maintainer decision required (no `RFC_REVIEW.md` exists in-repo; route via the
standard `rfc/NNNN-*` maintainer-review branch convention). On acceptance, mint
the next decision number (D249 is the next free slot at the anchor) and pick one
of options 1–5; the implementation (constant/policy change, lane-kind
differentiation, or fresh-attempt grant) plus the corresponding update to
`dx_289_test.go` and D198's revisit cell lands in a follow-up implementation
run, not here. Until then #478 stays open with this RFC as its disposition and
the per-workflow `max_unsealed_requeues` override as the documented interim
mitigation.
