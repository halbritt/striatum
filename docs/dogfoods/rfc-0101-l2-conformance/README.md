# RFC 0101 Layer 2 — adapter-conformance dogfood

A striatum dogfood that designs and builds **RFC 0101 Layer 2: the
adapter-conformance CI harness + lane-env hardening** (closes #76/#85/#70;
promotes the #101 claude argv-bootstrap fix to a contract clause).

Shape: two independent design lanes (codex + claude_code) → an interrogable
synthesis → an adversarial design-review panel (threat_model / ergonomics_dx /
devils_advocate) that interrogates the live synthesizer → an interrogable
implementation → a three-seat interrogating build-review panel, each panel
gated by a bounded `needs_revision` cycle.

The full task and the design seed (conformance contract, failure taxonomy,
lane-env hardening, adversarial attack surface) live in the role prompts under
`prompts/`. Durable artifacts land under `artifacts/`.

This is the first real exercise of the just-landed panel-window fix (#65 P1):
reviewers 2..N must be able to interrogate the same live target without
`target_unavailable`.
