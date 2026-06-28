# Review - RFC 0143 Slice B CapabilityReseal

Review the implementation against D261, RFC 0143, RFC 0168, and the current
source. Return `needs_revision` for any load-bearing gap. This review is the
gate that keeps the credential boundary narrow.

Check these gates explicitly:

- No lane can read the daemon admin client-token, operator token, or shared
  capability-token fallback. No group-read or chmod broadening is acceptable.
- `CapabilityReseal` is not exposed as a general public bearer route. Any
  public/test alternate must be test-only, documented, and covered by authority
  guardrails.
- Reseal authority is bound to daemon-owned job/session/supervisor state and the
  active RFC 0168 lane uid lease id/generation. Stale generation, missing lease,
  lease mismatch, closed session, requeued job, and sibling-lane replay must
  fail closed with typed errors.
- Artifact identity comes from daemon `expected_artifacts` state and durability
  checks, not terminal output or lane-supplied paths. Unexpected paths must not
  be resealed.
- Expired-beyond-grace behavior routes to the Slice A
  `session_unrecoverable_across_rotation` floor and never revives a stale lease
  or leaks an untyped lease error.
- Existing Slice A tests and ordinary completion/publish behavior still pass.
- Any schema migration, owner bundle, route map, method registry, command
  authority matrix, or authority guardrail changes are complete and in sync.
- Docs that claim current state are updated in the same change.

Run or inspect enough verification to support the verdict: `cd go && go build
./...`, `cd go && go vet ./...`, focused touched-package tests, `cd go && go
test ./...`, `make check-docs`, `make lint`, and `make typecheck` where the
worktree permits. If a live two-role PG check is required but not runnable in
the lane, state the exact operator command and why it remains residual.

Publish exactly one `REVIEW.md` finding artifact with verdict
`accept`, `accept_with_findings`, or `needs_revision`, concrete file/line
evidence for each gate, commands run with results, and any residual risk.
