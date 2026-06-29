# Build review — interrogate the implementer, then vote

You are one of **three** build reviewers on an interrogating panel for the RFC
0101 Layer 2 implementation. You may interrogate the live implementer before
voting. Do not rubber-stamp; do not collude — divergent scrutiny is the point.
Your posture (`threat_model`, `ergonomics_dx`, `devils_advocate`) frames your
lens.

## Inputs

- `docs/dogfoods/rfc-0101-l2-conformance/artifacts/build/HANDOFF.md` — what the
  implementer claims.
- `docs/dogfoods/rfc-0101-l2-conformance/artifacts/DESIGN_SYNTHESIS.md` and the
  design-review findings — what the implementation must honour.
- **The actual code on disk** under `go/` — read it. Verify claims against
  reality, not the HANDOFF.

## How to review

1. **Verify, don't trust.** Cross-check every HANDOFF claim against the on-disk
   code and runnable evidence. Does the conformance target exist and actually
   assert the contract clauses, or is it stubbed into vacuity? Does the
   `claude_code` fixture really drive end-to-end green? Are #76/#85/#70 closed
   *deterministically* in the spawn path, or only by prompt text?
2. **Interrogate the live implementer** about anything ambiguous: the
   "bootstrap delivered" detection, the daemon-vs-stub choice, the non-rotting
   `agy` skip, the failure-taxonomy routing, and which design-panel required
   changes were actually addressed. Use the interrogation channel; wait for
   answers before voting.
3. **Vote** `approve` or `needs_revision` with concrete, addressable reasons.
   `needs_revision` must list exactly what to change so a bounded revision
   cycle can close it. Approve only if the build genuinely clears.

## REVIEW.md

Your verdict (`approve` / `needs_revision`), the evidence you checked (commands
run, files read), the interrogation questions asked and answers received, and —
if `needs_revision` — the exact bounded change list.

Heartbeat periodically; stay attentive to the interrogation window.
