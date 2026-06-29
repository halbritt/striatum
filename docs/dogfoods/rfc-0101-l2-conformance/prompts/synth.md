# Synthesize — RFC 0101 Layer 2 design

Reconcile the design proposals into a single, coherent, **buildable** design
the implementer will work from. Write `DESIGN_SYNTHESIS.md`. You stay live
(interrogable) after completing — the adversarial review panel will
cross-examine you.

## Inputs

- `docs/dogfoods/rfc-0101-l2-conformance/artifacts/design/codex/DESIGN.md`
- `docs/dogfoods/rfc-0101-l2-conformance/artifacts/design/claude_code/DESIGN.md`

## Your job

- **Reconcile, don't average.** Where proposals agree, state the converged
  decision. Where they conflict, pick one and say why; if a conflict is
  genuinely unresolved, record it as an explicit open question for the
  adversarial panel — do not paper over it.
- Produce, concretely: the **conformance contract** as one ordered list of
  testable clauses; the **failure taxonomy** as a closed set of named classes;
  the **harness architecture** (package location in `go/`, how a conformance
  run executes against the installed CLI, daemon-vs-stub decision); the
  **lane-env hardening** mechanism for #76/#85/#70; and the **`agy`
  handling** (single-shot subset + a non-rotting skip).
- Keep it interrogable: state assumptions explicitly; the panel will attack
  the "bootstrap delivered" detection, daemon-vs-stub, the non-rotting skip,
  the taxonomy's adequacy for Layer 3 routing, and lane-env completeness.

## DESIGN_SYNTHESIS.md structure

1. Converged design (contract clauses, taxonomy, harness architecture,
lane-env hardening, `agy` handling). 2. Resolved conflicts (the pick + reason).
3. Open questions (genuinely unresolved tensions, flagged for the panel).
4. Build outline (concrete first implementation steps + the `make`/CI wiring,
so the implementer can start).

Heartbeat periodically. After completing, remain available to answer the
review panel's interrogation questions.
