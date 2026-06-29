# Design review — falsify the L2 synthesis, then vote

You review the synthesized design for RFC 0101 Layer 2. Your job is **not** to
rubber-stamp. Your job posture (`threat_model`, `ergonomics_dx`, or
`devils_advocate` — see your work packet) frames your lens, but every reviewer
must try to show how the design does **NOT** make the run robust.

## Input

- `docs/dogfoods/rfc-0101-l2-conformance/artifacts/DESIGN_SYNTHESIS.md`
- You may also read the live code to ground attacks: `go/pkg/supervisor/helper.go`,
  the adapter submit-driver code, `go/pkg/sessionliveness`, `go/pkg/agentloop`.

## Attack surface (cover the ones your posture owns; add your own)

1. **"Bootstrap delivered" detection** — if it is a daemon-side protocol
   event, can an adapter appear conformant while the human-visible TUI is
   actually wedged? Can redraw noise false-green or false-red the check?
2. **Real daemon vs. stub** — if a stub, prove it hides the #101-class
   regression it claims to catch; if a live/pgtest daemon, prove the
   flakiness/ordering risk that makes CI nondeterministic (heartbeat vs.
   rollback race, startup-gap contamination, daemon-leak-on-panic).
3. **`agy` skip rot** — show the concrete path by which the "non-rotting" skip
   becomes a permanently-green lie.
4. **Lane-env hardening completeness** — for #76/#85/#70 find a residual path
   where the survey still blocks, the background probe still spawns, or a token
   still lands in the work tree.
5. **Conformance ≠ robustness** — name a real lane failure that passes
   conformance yet still silently wedges a live run (the false confidence the
   green badge buys).
6. **Failure-taxonomy adequacy** — find a real failure that maps to no class
   or the wrong one for later Layer 3 routing.
7. **Token-scan false positives** — could the bearer-token work-tree scan
   collide with legitimate hex content and block CI?

Interrogate the **live synthesizer** on anything ambiguous before you vote —
use the interrogation channel and wait for answers.

## REVIEW.md

State your verdict (`approve` or `needs_revision`), the evidence you checked
(files read, the interrogation questions asked and the answers received), and —
if `needs_revision` — the exact, bounded change list. Be falsifiable: vague
concerns are not findings. Heartbeat periodically; stay attentive to the
interrogation window.
