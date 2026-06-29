# Review Brief — RFC 0103: Self-Hosting Production Hardening

author: presenter-claude-opus-4.8-001

## Thesis

RFC 0097 self-hosting was **proven once** on 2026-06-01 (`8e9ac86b`) using the
safest possible shape: one `claude` lane, one document job, no review panel, no
parallelism, no daemon restart. RFC 0103 frames the gap between that minimal
proof and a **production-grade** self-host — a runner that can carry a real
**multi-lane, multi-turn, review-gated build of its own fixes** with no operator
at the keyboard. It claims the remaining 17 open issues are *not mechanical*;
they cluster into seven workstreams, each extending an existing slice-RFC, but
no RFC currently owns "production-grade self-hosting" as a property. It adds no
new persistence, hosted service, telemetry, or transcript capture.

## The seven workstreams

- **W1 — Lane becomes a real sandbox** (RFC 0096 V2): #135 session-bound token, #70 bearer token out of worktree, #87 deny direct Postgres.
- **W2 — Every adapter holds a multi-turn seat** (RFC 0096/0088): #95/#85/#76/#139 — make `agy` re-enter its session, suppress feedback prompt, bound MCP discovery.
- **W3 — Lane survives transport/daemon churn** (RFC 0091/0101): #141 reconnect-with-backoff + reconcile attached supervisors, #125 make `work.ack` non-substitutable.
- **W4 — Interrogation window outlives one reviewer** (RFC 0095): #131/#134 — replacement reviewer re-attaches or gets a non-wedging signal, not `target_unavailable`.
- **W5 — Artifact contracts legible at point of need** (RFC 0100 P2): #126 finding skeleton/enum in packet, #128 `lint` warns on downstream `write_scope` drift.
- **W6 — Run orchestration honest and coordinated** (RFC 0097): #115 surface frozen-snapshot pin on a running run, #138 declared/serialized shared-resource gate.
- **W7 — Operator is a bounded, well-served processor** (RFC 0099/0102): #92 constrained-operator surface, #112 tmux backend by default + trajectory extraction.

## Dependency ordering

`W1 → (W2/W3/W4) → (W5/W6) → W7`, though layers are mostly independent and each
ships alone. **W1 first** — it is the trust substrate and highest-risk surface
(an un-sandboxed lane carrying the operator's own credentials is the worst
failure to leave open). **W2/W3/W4** are the *multi-lane viability* tier: every
seat holds, lanes survive churn, panels survive reviewer replacement. **W5/W6**
remove the legibility/coordination friction that turns good multi-lane work into
a failed completion. **W7** is the operator-side payoff that consumes the honest
signals the lower layers produce.

## Three sharpest open questions / risks a reviewer should probe

1. **Is the W-grouping a real partition of the 17 issues, or loose bucketing?**
   The 7 workstreams claim ~17 issues but the Context cites overlapping clusters
   and some issues appear cross-cut (e.g. #87 spans env-leak + libpq access).
   Verify every open issue maps to exactly one workstream and none is dropped.
2. **Is the umbrella acceptance measurable / falsifiable?** It asks for a
   multi-lane, review-gated dogfood with "at least two distinct adapter seats,
   one bounded `needs_revision` cycle with a live interrogation, surviving at
   least one injected fault." But W2 admits `agy` is *not yet viable*, so the
   only supported second seat is codex — does the umbrella reduce to claude+codex
   and is "one injected fault" a strong enough bar, or a checkbox?
3. **Does any workstream really belong to a different RFC (or warrant its own)?**
   Each W "extends" an owning slice-RFC, yet RFC 0103 also asserts *no* RFC owns
   the umbrella property. Probe whether W3's daemon-reconcile and W7's operator
   surface are RFC 0103 scope or are being smuggled in as residual tails that
   their parent RFCs (0101 / 0099/0102) should own and gate directly.
