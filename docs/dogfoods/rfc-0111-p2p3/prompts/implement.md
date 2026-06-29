# Implement RFC 0111 P2 + P3

You are implementing the two remaining slices of the accepted RFC 0111
(`docs/rfcs/0111-in-band-failure-legibility-and-self-heal.md`, D165). P1 is
already on main (`contentSummary` in `go/pkg/mcp/tools.go` renders code+message
into the MCP content text on failure). Read the RFC first — its §Proposal and
§Acceptance are the contract. Work test-first (RED → GREEN) per house
convention.

## P2 — Remediation on the envelope

1. Add `Suggestion string` (JSON `suggestion,omitempty`) to `rpc.Error`
   (`go/pkg/rpc/envelope.go`).
2. Thread it through `ErrorResponse` → `Response.Data` (key `suggestion`,
   only when non-empty) → the MCP boundary: `ToolsCall` extracts it alongside
   `code`/`message`, and `contentSummary` appends it to the failure content
   text (e.g. `… failed: <code>: <message> — suggestion: <s>`), so an agent
   reads the remediation in-band.
3. Populate it **centrally, not at 165+ call sites**: in `ErrorResponse`,
   when `rpcErr.Suggestion == ""`, fill it from the P3 catalog's per-code
   default suggestion. Explicit call-site suggestions win over defaults.
4. Defaults must exist for the high-traffic families the RFC names, grounded
   in the codes that actually exist in this codebase (grep
   `NewError("` and `&Error{Code:`): lifecycle (`invalid_transition`,
   `already_completed`), lease/session (`lease_error`,
   `fresh_session_required`, `interrogation_unavailable` where used as error
   codes), capability (`capability_missing`, `capability_denied`,
   `token_invalid`, `token_expired`, `token_revoked`, `token_malformed`),
   and the confirmation gates (`confirmation_required`,
   `branch_confirmation_required`). A suggestion is one imperative sentence
   that names the next action (and the CLI/MCP verb when one exists, e.g.
   `striatum recovery stale-leases` for stale-lease shapes per RFC 0020/0029
   vocabulary).

## P3 — Closed, guard-tested code catalog

1. New `go/pkg/rpc/error_catalog.go`: a single registry enumerating every
   error code the daemon can return — each entry carries the code, a one-line
   meaning, and the default suggestion (empty allowed where no remediation is
   sensible). Export a lookup the P2 default-fill uses.
2. Guard test `go/pkg/rpc/error_catalog_test.go` (mirror the spirit of
   `registry_contract_test.go`): statically scan the Go source under `go/`
   for `NewError("<code>"` and `&Error{Code: "<code>"` / `Code: "<code>"`
   literals and FAIL when a code is absent from the catalog; also FAIL when a
   catalog entry matches no source literal (guard-reconciled in both
   directions, like the seat-tier guards).
3. Add an "Error code catalog" section to
   `docs/reference/command-authority-matrix.md` (code | meaning | default
   suggestion), and fix that doc's stale guardrail reference: it still points
   at the retired Python `tests/architecture/test_authority_guardrails.py`;
   name the live Go guards instead (`go/pkg/rpc/registry_contract_test.go`,
   `go/pkg/rpc/registry_rfc0043_test.go`, and your new
   `error_catalog_test.go`). A doc↔code drift guard for the matrix section is
   welcome but optional.

## Acceptance (from RFC §Acceptance — all must hold)

- For the chosen high-traffic codes, a failing call carries a non-empty
  `suggestion` end to end: RPC `Response.Data` AND the MCP result (content
  text + structured data). Add/extend tests in `go/pkg/rpc/envelope_test.go`
  and `go/pkg/mcp/http_test.go` proving it.
- The guard test enumerates every code and fails on an uncataloged one.
- The matrix gains the catalog section.
- Existing `pkg/mcp` + `pkg/rpc` suites stay green. Run the full suite
  (`cd go && go test ./...`) and the CI-exact lint:
  `golangci-lint run --default=none --enable=govet --enable=staticcheck
  --enable=errcheck --enable=ineffassign ./...` (must be `0 issues`; binary
  at `~/go/bin/golangci-lint`).
- Do NOT change `structuredContent.error` / `error_message` semantics, exit
  codes, or capability denial reasons. Do NOT add new transports or
  persistence. The declined §6.3 daemon-tier idea stays declined.
- Update `CHANGELOG.md` under `Unreleased`.

## Artifact

Publish `docs/dogfoods/rfc-0111-p2p3/artifacts/IMPLEMENTATION.md` (kind
`synthesis`, valid V1 front matter — the publisher refuses invalid front
matter with exit 6 and embeds a skeleton in the error). It must contain:

- What changed, file by file, with the design choice (central default-fill)
  called out.
- **Typed evidence**: the exact test names you added and the verbatim final
  lines of `go test ./...` and the lint run — evidence a reviewer can re-run,
  not prose claims.
- Any codes you found that the RFC's families name but the codebase does not
  actually emit (list them; do not invent catalog entries for them).

Commit your code changes in your worktree as you go (small commits are fine);
the operator lands the branch after review.
