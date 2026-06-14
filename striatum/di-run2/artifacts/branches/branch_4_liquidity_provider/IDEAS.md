# Diverge — Frame: liquidity_provider

author: diverger-claude-opus-4.8-002

**Frame vantage:** Who silently bears the volatility, and what do they
implicitly charge? Which hidden subsidy makes the graduation look cheaper than
it really is?

---

## 1. The escalation channel is the option-seller of last resort, and its premium is unpriced

`escalates-loud-never-silently-wedges` is the leg that absorbs every fault the
matrix did *not* enumerate. It is short a naked put on the entire un-modeled
tail. No cash changes hands, so the gate books the premium as zero — but it is
paid, in operator attention, every time it fires. The graduation looks cheap
precisely because nobody is tracking the *strike and frequency*: a fixture that
proves "escalation can fire" while never measuring how often the live shape
forces it is selling volatility it has not priced. Make the fixture quote that
premium: count escalations-per-completed-run as a stated, bounded charge, and
fail the gate when realized escalation frequency blows through it.

## 2. The budget is a bid-ask spread, and a generous one is a subsidy that hides latency volatility

A bounded budget is what makes "completes-or-escalates" decidable — but its
*width* is a spread the fixture quotes against the workflow's latency. Quote it
wide and the run always settles inside the budget, so the gate goes green and
the harness looks reliable. That green is subsidized: you have warehoused all
the per-branch timing variance in slack you never charge for. A market maker who
quotes a spread so wide it is never crossed never discovers their true cost. The
honest fixture tightens the budget until it *occasionally* trips into escalation
on purpose — the trip is the price discovery that proves the bound is real and
not decorative headroom.

## 3. The deterministic lane stub is the liquidity provider, and graduation is really certifying the stub, not the workflow

"No model call in any state transition, unattended in CI" forces the live lanes
to be replaced by deterministic fixtures. That stub *is* the liquidity provider:
it absorbs 100% of the non-determinism risk by simply refusing to be
non-deterministic, and it charges an implicit spread equal to every behavior a
real model would exhibit that the stub does not. The system looks cheap and
reliable because the stub warehouses the basis risk silently. What earns the
`supported` stamp is the plumbing's response to *stub* output — the gap between
stub-shape and model-shape is unpriced basis the gate never sees. Surface it:
make the stub emit deliberately adversarial structural shapes (empty branch,
duplicate top-K, oversized artifact) so the harness is graded against the
*worst legal* output, not the convenient one.

## 4. Structural assertions are the spread between "checkable" and "true," and the convergence critic earns it by collapsing diversity into a count

The gate can only assert structure — artifact present, kind correct, top-K
selected. The convergence critic stands between messy order flow (N raw idea
sets) and the clean tape (the run graph), and it earns its spread by discarding
the semantic diversity that cannot be cheaply checked. "Six artifacts of kind
`synthesis` exist" is trivially assertable; that triviality is subsidized by
never pricing whether the collapse was *good*. The fixture can verify the
market maker showed up to quote — never that the quote was fair. Lean into the
spread instead of pretending it is zero: assert the *shape* of the collapse
(cluster count within band, no single cluster swallowing >X of picks) so the
critic's spread becomes a checkable invariant rather than an invisible one.

## 5. Reviewer fungibility is a subsidy that only clears because review is forbidden from mattering

The fault matrix's "reviewer replacement" survives only because reviewers are
treated as interchangeable liquidity: any maker can fill the quote and the run
still settles. That fungibility is the hidden subsidy — it is "free" *only*
because the gate forbids semantic acceptance, so a substitute reviewer is
structurally incapable of disagreeing in a way that changes the outcome. The
graduation therefore buys robustness-to-reviewer-churn by quietly making the
reviewer not load-bearing. Price the subsidy honestly: have the fixture inject a
replacement reviewer that returns a *different structural verdict* (reject vs.
approve) and assert the run still reaches a bounded terminal state — proving the
shape tolerates reviewer *disagreement*, not merely reviewer *substitution*.

## 6. CI is a clearinghouse posting margin against model-variance flake, and the reliability is leveraged on today's low realized volatility

For the fixture to "live in CI without flaking on model variance," something
absorbs the residual variance — retries, quarantine, a flake-tolerance
threshold. That is CI acting as a clearinghouse, posting margin (compute,
re-runs, maintainer triage) against the workflow's volatility. The green
checkmark is cheap to *read*, which disguises that the margin is real and grows
with model drift. The reliability claim is leveraged on the fact that current
models are calm; a future model swap is a volatility spike that triggers the
margin call — a flake storm the gate never sized. An LP-honest fixture states
its flake tolerance as an explicit spread and fails *loud* the moment realized
structural variance crosses it, converting silent flake-warehousing into a
bounded, visible charge.
