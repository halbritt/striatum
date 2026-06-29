---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
---

# RFC 0143 Slice B Build Summary
author: author-author-003

## Outcome

RFC 0143 Slice B is finalized on reviewed run branch `striatum/rfc-0143-slice-b-build`
for `run_20d2fb3e999d1b5ae4e5de6b180d86a3`. The accepted review artifact
`art_378f7c4f46c7a71d0cbc77d85cdc7442` by `reviewer-reviewer-002` found no
blocking findings. This apply pass kept the reviewed implementation intact, updated
current-state docs from "draft in progress" to "accepted/apply-verified build", reran
the verification gate, and added this summary artifact.

The final implementation keeps `CapabilityReseal` daemon-internal. It does not add a
public reseal method, does not mint a general reseal bearer, does not make the daemon
admin/runtime token lane-readable, and does not add `reseal` to the grantable public
capability map.

## Files Changed By Gate

- Build artifacts: `docs/operator/artifacts/rfc-0143-slice-b-build/DRAFT.md` and this
  `docs/operator/artifacts/rfc-0143-slice-b-build/SUMMARY.md`.
- Runtime code: `go/pkg/rpc/registry.go` adds internal `CapabilityReseal`; no public
  grantable `reseal` capability is added.
- Recovery code: `go/pkg/mutations/recovery_decision_tree.go` adds the Slice B
  `capability_reseal` path for the exact `session_unrecoverable_across_rotation`
  class, validates same-run work lease state/grace, and requires active lane UID
  lease id/generation/session/supervisor/uid authority before finalizing from durable
  expected artifacts.
- Recovery tests: `go/pkg/mutations/recovery_unrecoverable_across_rotation_test.go`
  covers the positive reseal path, stale generation, sibling replay, foreign-run work
  lease, foreign-run lane UID lease, beyond-grace refusal, and expected-artifact-only
  finalization.
- Current docs: `CHANGELOG.md`, `docs/operator/BRIEF.md`,
  `docs/operator/rfc-roadmap.md`, `docs/how-to/lane-sandbox.md`,
  `docs/reference/command-authority-matrix.md`,
  `docs/rfcs/0143-lane-credential-survival-across-boot-epoch-rotation.md`, and
  `docs/rfcs/0168-per-lane-security-principal.md`.

## Review Findings Addressed

- The accepted review reported no blocking findings.
- The prior reopened blocker is addressed: the docs and handoff describe the recovery
  surface as authored or already-published expected artifacts, matching
  `tryFinalizeUnsealedFromDurableArtifact` salvage behavior.
- Authority exposure remains narrow: `CapabilityReseal` is an internal marker omitted
  from `rpc.Capabilities`, while daemon token creation rejects unsupported capability
  names.
- The reseal path remains scoped to repository, run, job, session, supervisor, work
  lease, and active lane UID lease generation. Stale/missing generation, sibling-lane
  replay, foreign-run replay, inactive session, and expired-beyond-grace work leases
  fail closed through `recovery.capability_reseal_unavailable` and the Slice A typed
  floor.
- Apply-time doc updates moved current state from "draft in progress" to
  "accepted/apply-verified build" without adding new route, capability, schema, or owner
  bundle surface.

## Verification

- `go test ./pkg/mutations -run 'TestRotationLockedLaneWithFreshLaneUIDLeaseCapabilityResealsDurableArtifact|TestRotationCapabilityReseal|TestTypedFloor|TestRecoveredSession|TestSameUidSibling|TestProviderExitCode|TestTmuxPane|TestReservedExitCode|TestOrdinaryUnsealed' -count=1 -v` - PASS; PostgreSQL-backed tests skipped because `STRIATUM_PG_TEST_URL` is unset.
- `go test ./pkg/rpc -count=1` - PASS.
- `go test ./pkg/agentloop ./pkg/sessionliveness -count=1` - PASS.
- `go build ./...` - PASS.
- `go test ./...` - PASS.
- `go vet ./...` - PASS.
- `make check-docs` - PASS before and after the final doc-status updates.
- `make lint` - PASS (`0 issues.`).
- `make typecheck` - PASS.
- `make smoke` - PASS; PostgreSQL integration skipped because `STRIATUM_DAEMON_DB_URL`
  is unset, and fresh-clone smoke reported OK.
- `git diff --check` - PASS after the final doc-status updates.

## Remaining Operator And Deploy Work

- Rerun the focused recovery tests with `STRIATUM_PG_TEST_URL` set so the PostgreSQL-backed
  Slice B fixtures execute instead of skipping.
- Run the live PostgreSQL smoke/deploy checks with `STRIATUM_DAEMON_DB_URL` set before
  enabling the posture on a deployed daemon.
- Complete normal daemon run integration, then verify the integrated `main` state and
  operator dashboard/doctor posture before treating #512 as fully closed in production.
