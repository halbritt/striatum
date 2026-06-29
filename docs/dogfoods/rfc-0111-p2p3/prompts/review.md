# Review the RFC 0111 P2+P3 implementation

You are reviewing the implementation synthesis at
`docs/dogfoods/rfc-0111-p2p3/artifacts/IMPLEMENTATION.md` and the code it
describes, against the accepted RFC 0111
(`docs/rfcs/0111-in-band-failure-legibility-and-self-heal.md`) §Acceptance.

## What to verify (each one concretely, not by trusting the prose)

1. **Suggestion end to end.** Read `go/pkg/rpc/envelope.go` and
   `go/pkg/mcp/tools.go`: `rpc.Error` has `Suggestion`; `ErrorResponse` puts
   a non-empty `suggestion` into `Response.Data`, with catalog defaults
   filled centrally; the MCP failure content text carries it. Confirm the
   named tests exist in the diff and actually assert this (not weaker
   variants).
2. **Closed catalog + guard.** `go/pkg/rpc/error_catalog.go` enumerates the
   codes; the guard test fails on an uncataloged source literal AND on a
   catalog entry with no source use. Spot-check 3 codes you pick yourself
   from the source (grep `NewError("`) and confirm they are cataloged with
   sane meanings/suggestions.
3. **Docs.** `docs/reference/command-authority-matrix.md` gained the catalog
   section and no longer points at the retired Python guardrail file.
4. **Run the evidence.** Re-run the test commands quoted in the artifact
   (`cd go && go test ./pkg/rpc/ ./pkg/mcp/` at minimum). If a quoted result
   does not reproduce, that is an automatic needs_revision finding.
5. **No contract regressions.** `structuredContent.error`/`error_message`
   unchanged; no new transport/persistence; exit codes untouched.

## Verdict

Submit exactly one verdict via your work packet's review verb:

- `accept` (or `accept_with_findings` for non-blocking nits) when every
  acceptance criterion is met with reproducible evidence.
- `needs_revision` with concrete, file-and-line findings when any criterion
  fails. Do not pad findings; one real blocker beats five vague ones.

You may interrogate the implementer (the implement job is interrogable) when
something is ambiguous — prefer one sharp question over a guessed finding.
