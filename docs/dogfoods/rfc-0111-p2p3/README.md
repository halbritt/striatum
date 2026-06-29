# Dogfood: RFC 0111 P2+P3 (Suggestion field + closed error-code catalog)

Implementation dogfood for the accepted RFC 0111 (D165), slices P2 and P3.
P1 (MCP content legibility) landed directly on main; these two slices change
the RPC error contract, so they route through a reviewed run per project
convention.

- **implement** (claude, per-job worktree, repo-write): adds
  `rpc.Error.Suggestion`, threads it `ErrorResponse` → MCP content, fills
  defaults centrally from the new `pkg/rpc` error-code catalog; adds the
  two-direction guard test and the `command-authority-matrix.md` catalog
  section. Publishes `artifacts/IMPLEMENTATION.md` with typed test evidence.
- **review** (agy, review-only): falsification review against RFC
  §Acceptance; one `needs_revision` cycle budgeted (max 2).

Launched 2026-06-04 by the operator session driving the RFC 0111 follow-up
(plan `snazzy-whistling-leaf`). The operator lands the run branch to main
after the review verdict and live-verifies the suggestion end to end.
