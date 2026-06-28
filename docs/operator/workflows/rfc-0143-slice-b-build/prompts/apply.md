# Apply - RFC 0143 Slice B CapabilityReseal

Apply the accepted review findings and finalize the Slice B build. Do not widen
scope beyond the reviewed implementation unless the review explicitly requires
it; if the right fix needs a larger authority or product decision, stop and
publish that blocker rather than smuggling it into the apply pass.

Before publishing `SUMMARY.md`, ensure the final state still satisfies:

- no lane-readable admin/operator/shared token;
- no general public reseal bearer surface;
- reseal bound to repository/run/job/session/supervisor plus active lane uid
  lease id and generation;
- stale generation, lease mismatch, sibling-lane replay, closed session, requeue,
  and expired-beyond-grace cases fail closed and are typed;
- expected artifact identity and durability come from daemon state;
- Slice A typed floor and ordinary completion behavior are preserved;
- route/capability docs and guardrail tests are current when any map changes;
- current-state docs and CHANGELOG reflect the shipped state.

Re-run the relevant verification after changes. Prefer the full gate:
`cd go && go build ./...`, `cd go && go vet ./...`, focused touched-package
tests, `cd go && go test ./...`, `make check-docs`, `make lint`, and
`make typecheck`. If any check is impractical in the lane, state the exact
reason and the operator command needed.

Publish `SUMMARY.md` with files changed, review findings addressed, final gate
coverage, commands run with results, and remaining operator/deploy work.
