---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
inputs:
  - "docs/rfcs/0111-in-band-failure-legibility-and-self-heal.md"
  - "docs/dogfoods/rfc-0111-p2p3/prompts/implement.md"
---

# RFC 0111 P2+P3 Implementation Synthesis

author: implementer-claude-opus-4.8-001

Implements the two remaining slices of RFC 0111 (accepted, D165) on top of the
already-landed P1: **P2** threads a first-class remediation `Suggestion`
through `rpc.Error` → `ErrorResponse` → the MCP `tools/call` boundary, and
**P3** closes the daemon's error-code vocabulary into a single guard-tested
catalog rendered into `docs/reference/command-authority-matrix.md`. Work was
test-first (RED → GREEN at each step; the guard test failed against an empty
catalog with all 62 codes listed before the catalog was filled, and the P2
envelope tests failed at build level before `Suggestion` existed).

## What changed, file by file

### `go/pkg/rpc/error_catalog.go` (new — P3)

The closed contract: `ErrorCatalog` enumerates **62** error codes in live use,
each with a one-line meaning and a default suggestion (empty where no generic
remediation is sensible). Exports `LookupErrorCode` and `DefaultSuggestion` —
the lookup the P2 default-fill uses.

### `go/pkg/rpc/error_catalog_test.go` (new — P3 guard)

Mirrors the spirit of `registry_contract_test.go`. Statically scans all
non-test Go source under `go/` (whole-file regex, so multiline literals are
caught) for three shapes that provably become agent-visible error codes:

1. `NewError("<code>"` (incl. `rpc.NewError(`) — the constructor;
2. `Code: "<code>"` — `rpc.Error` / client-shim error struct literals
   (`pkg/cli/rpcclient`, the MCP local-request guard);
3. `DenialReason: "<code>"` / `DenialReason = "<code>"` — capability denial
   reasons, included **beyond the prompt's minimum patterns** because
   `RequireAllowed` (`go/pkg/rpc/capability.go`) converts them verbatim into
   `rpc.Error` codes; without this shape, `token_missing`,
   `capability_scope_mismatch`, and `capability_expired` — codes the daemon
   really returns — could never be cataloged (reverse reconciliation would
   reject them).

It fails in **both directions**: an emitted code absent from the catalog, and
a catalog entry matching no source literal. `error_catalog.go` itself is
excluded from the scan so its own `Code:` literals cannot satisfy reverse
reconciliation. The optional doc↔code drift guard is included: every cataloged
code must appear as a ``| `code` |`` row in the matrix doc.

### `go/pkg/rpc/envelope.go` (P2)

- `rpc.Error` gains `Suggestion string` (JSON `suggestion,omitempty`).
- `ErrorResponse` is the **central fill point** (the called-out design
  choice): when `rpcErr.Suggestion == ""` it fills from
  `DefaultSuggestion(code)`, so none of the 165+ `NewError` call sites change;
  an explicit call-site suggestion always wins over the catalog default. The
  `suggestion` key is added to `Response.Data` only when non-empty (omitted,
  not empty-string).

### `go/pkg/mcp/tools.go` (P2)

`ToolsCall` extracts `suggestion` from `response.Data` alongside
`code`/`message`; `toolResult` and `contentSummary` take it through. Failure
content text now renders
`<method> failed: <code>: <message> — suggestion: <s>`, and
`structuredContent` gains a sibling `suggestion` key when non-empty. The
`structuredContent.error` / `error_message` semantics, `isError`, exit codes,
and capability denial reasons are unchanged; success summaries stay terse.

### `go/pkg/rpc/envelope_test.go`, `go/pkg/mcp/http_test.go` (extended)

See "Typed evidence" below for the exact test names.

### `docs/reference/command-authority-matrix.md`

- New **"Error code catalog (RFC 0111)"** section: code | meaning | default
  suggestion table for all 62 codes, generated from `ErrorCatalog` (not
  hand-transcribed) and guard-pinned by
  `TestErrorCatalogMatrixDocCarriesEveryCode`.
- Fixed the stale guardrail reference: the retired Python
  `tests/architecture/test_authority_guardrails.py` (RFC 0078) is now
  superseded in the text by the live Go guards
  (`go/pkg/rpc/registry_contract_test.go`,
  `go/pkg/rpc/registry_rfc0043_test.go`, `go/pkg/rpc/error_catalog_test.go`),
  in both places it was named.

### `CHANGELOG.md`

`Unreleased` entry describing P2+P3.

### Commits (this worktree, branch `striatum/rfc-0111-p2p3`)

- `f86f92e2` feat(rpc): RFC 0111 P3 — closed, guard-tested error-code catalog
- `a84b3193` feat(rpc,mcp): RFC 0111 P2 — thread remediation Suggestion to the
  MCP boundary

## Typed evidence

Tests added (all runnable by name):

- `go/pkg/rpc/error_catalog_test.go`:
  `TestErrorCatalogHasNoDuplicateOrEmptyEntries`,
  `TestErrorCatalogIsClosedAgainstSource`,
  `TestErrorCatalogLookupAgreesWithEntries`,
  `TestErrorCatalogMatrixDocCarriesEveryCode`
- `go/pkg/rpc/envelope_test.go`:
  `TestErrorResponseCarriesExplicitSuggestion`,
  `TestErrorResponseFillsDefaultSuggestionFromCatalog`,
  `TestErrorResponseOmitsSuggestionWhenNoneKnown`,
  `TestHighTrafficCodesCarryNonEmptyDefaultSuggestion`
- `go/pkg/mcp/http_test.go`:
  `TestHTTPHandlerToolsCallFailureContentCarriesSuggestion`,
  `TestHTTPHandlerToolsCallExplicitSuggestionWinsOverDefault`,
  `TestHTTPHandlerToolsCallFailureWithoutSuggestionStaysP1Shaped`

Fresh (uncached) run of the touched packages —
`go test -count=1 ./pkg/rpc/ ./pkg/mcp/` — verbatim output:

```
ok  	github.com/halbritt/striatum/go/pkg/rpc	0.057s
ok  	github.com/halbritt/striatum/go/pkg/mcp	0.006s
```

Full suite — `cd go && go test ./...` — verbatim final lines (exit 0, no
failures anywhere above):

```
ok  	github.com/halbritt/striatum/go/pkg/websse	(cached)
?   	github.com/halbritt/striatum/go/pkg/webtest	[no test files]
ok  	github.com/halbritt/striatum/go/pkg/workflowauthoring	(cached)
ok  	github.com/halbritt/striatum/go/pkg/workflowgenerate	(cached)
ok  	github.com/halbritt/striatum/go/pkg/workflowtemplates	(cached)
```

CI-exact lint —
`~/go/bin/golangci-lint run --default=none --enable=govet --enable=staticcheck
--enable=errcheck --enable=ineffassign ./...` — verbatim complete output:

```
0 issues.
```

High-traffic defaults proven end to end (RPC `Response.Data` and the MCP
result, content text + structured data) for: `invalid_transition`,
`lease_error`, `capability_missing`, `capability_denied`, `token_invalid`,
`token_expired`, `token_revoked`, `token_malformed`, `confirmation_required`,
`branch_confirmation_required` (see
`TestHighTrafficCodesCarryNonEmptyDefaultSuggestion` and the two HTTP
suggestion tests).

## Codes the RFC's families name that this codebase does NOT emit as error codes

Per the prompt, listed here and **not** invented as catalog entries — each is
a result *status* or packet *field*, not an `rpc.Error.Code`:

- `already_completed` — result status in `work.complete`'s idempotent re-ack
  path (`pkg/mutations/lifecycle.go:982`); lifecycle refusals use
  `invalid_transition`. The whole `already_*` family (`already_registered`,
  `already_removed`, `already_paused`, `already_canceled`,
  `already_reclaimable`) is likewise statuses on OK responses.
- `fresh_session_required` — workflow job flag and claim
  `ineligible_reason` (`pkg/mutations/claim.go:132`), never an error code.
- `interrogation_unavailable` — RFC 0103's deliberately **non-wedging result
  status** (`pkg/mutations/interrogation.go:767`); making it an error would
  regress that design.
- `worktree_required` — work-packet field (`pkg/mutations/claim.go:405`),
  not an error code.

Known boundary (documented, intentionally out of catalog scope): the MCP
layer's own non-`rpc.Error` codes — `tool_hidden` / `daemon_rpc_missing` /
`command_failed` (constructed as `toolResult` arguments in
`pkg/mcp/tools.go`) and the JSON-RPC transport codes in `pkg/mcp/http.go`
(`malformed_body`; its other literals — `schema_invalid`, `method_unknown`,
`token_missing`, `token_malformed` — are already cataloged via the scanned
shapes). They never cross `ErrorResponse`, so they cannot carry catalog
defaults; folding them in would need a deliberate scan-shape extension.

## Boundaries respected

No change to `structuredContent.error` / `error_message` semantics, exit
codes (`ExitCode` default 10 untouched), capability denial reasons, or the
front-matter exit-6 publish contract. No new transports or persistence. The
declined §6.3 daemon-optional tier stays declined.
