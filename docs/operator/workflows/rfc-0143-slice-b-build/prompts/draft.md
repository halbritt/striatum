# Draft - RFC 0143 Slice B CapabilityReseal

Read the workflow packet and all required context docs before editing. This is
a build run, not a new design run: D261 accepted the RFC 0143 split, Slice A is
already landed, and RFC 0168 P0 is now integrated. Build Slice B from current
`origin/main` and current source, not from older same-uid design assumptions.

Implement the `CapabilityReseal` path on top of RFC 0168 per-lane uid leases.
Keep the authority narrow:

- Do not make `/run/striatum/client-token`, `.striatum/capability_token`, or any
  operator/admin token readable by a lane.
- Do not introduce a general public reseal bearer or route that any ordinary
  `write` token can exercise. If a reseal credential/file is introduced, it must
  be session/job scoped, seal-only, lane-uid-owned `0600`, and invalidated on
  session close/cleanup.
- Bind reseal to daemon-owned state: repository id, run id, job id, session id,
  supervisor id, expected artifact set, active lane uid lease id, and active
  lane uid generation. A stale generation, missing lease, lease mismatch, closed
  session, requeued job, or sibling-lane attempt must fail closed with a typed
  operator-legible reason.
- Preserve Slice A: boot-epoch loss must still route to
  `session_unrecoverable_across_rotation` when reseal is unavailable or expired;
  do not weaken the ordinary typed floor.

Recheck the current schema and owner-bundle frontier before adding DDL. At this
scaffold time source is runtime schema 47 and owner bundle 0023, but the source
tree is authoritative. If you add a daemon method, route, capability map, or
alternate authorization path, update `docs/reference/command-authority-matrix.md`
and the authority guardrail tests in the same change.

Add focused tests that prove at least:

- happy-path reseal of already-authored expected artifacts after a simulated
  boot-epoch rotation or stale endpoint condition;
- stale generation or lane uid lease mismatch fails closed;
- expired-beyond-grace reseal routes to the typed unrecoverable floor, not a raw
  lease error or revived stale lease;
- sibling-lane replay cannot reseal another lane's job;
- unexpected artifact paths/content are not resealed from lane input;
- existing Slice A typed floor behavior and ordinary non-rotation completion
  behavior still pass.

Update docs that move with the implementation, including RFC 0143, RFC 0168 if
needed, the roadmap, operator brief, README/docs index, CHANGELOG, reference docs,
and the decision log only if the implementation reopens a product/architecture
decision.

Run `cd go && go build ./...`, `cd go && go vet ./...`, focused touched-package
tests, `cd go && go test ./...`, `make check-docs`, `make lint`, and
`make typecheck` where feasible. If live PostgreSQL or two-role checks are
needed, write the test and state the operator command needed to prove it.

Publish the required `DRAFT.md` with a concise ledger: files changed, how each
Slice B gate is satisfied, route/capability changes, schema/owner-bundle frontier
handling, commands run with results, and any residual operator/deploy work.
