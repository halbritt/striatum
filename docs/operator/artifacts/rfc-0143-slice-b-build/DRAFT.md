---
schema_version: "striatum.handoff.v1"
artifact_kind: "handoff"
---

# RFC 0143 Slice B Build Draft
author: author-author-001

## Scope

Implemented the RFC 0143 Slice B draft on top of the RFC 0168 per-lane UID
substrate. The recovery sweep now has a daemon-internal `CapabilityReseal` path
for exactly classified `session_unrecoverable_across_rotation` jobs whose
required expected artifacts were already published and are reconstructable.

This does not add a public reseal method, does not add `reseal` to the grantable
capability map, does not mint a general reseal bearer, and does not make the
daemon admin/runtime token lane-readable.

## Behavior

- `rpc.CapabilityReseal` exists only as an internal authority/provenance marker.
  It is intentionally omitted from `rpc.Capabilities`, so token minting cannot
  grant it.
- The Slice B path runs only from `recovery.sweep` after Slice A exact
  attribution has classified the dead lane as
  `session_unrecoverable_across_rotation`.
- Reseal requires the owning session to remain active, the work lease to be
  active or inside the short reseal grace window, and supervisor metadata to
  match an active `lane_uid_leases` row by lease id, generation, session id,
  supervisor id, and uid.
- Stale generation, missing UID lease, sibling supervisor/session replay,
  inactive session, inactive work lease, and beyond-grace expiry record
  `recovery.capability_reseal_unavailable` and fall back to the existing typed
  requeue/escalate floor.
- The finalization still uses daemon expected-artifact verification and
  reconstructability checks; unexpected lane-authored paths or content are not
  sealed by reseal authority.

## Files Changed

- `go/pkg/rpc/registry.go`: added internal `CapabilityReseal`.
- `go/pkg/mutations/recovery_decision_tree.go`: added UID-lease/generation
  reseal validation, positive `recovery.capability_resealed` eventing, and
  fail-closed `capability_reseal_unavailable` action/eventing.
- `go/pkg/mutations/recovery_unrecoverable_across_rotation_test.go`: updated
  the durable rotation case to require fresh lane UID authority and added
  focused stale-generation, sibling-replay, grace-expiry, and
  expected-artifact-only tests.
- Docs updated in `CHANGELOG.md`, RFC 0143, RFC 0168, the lane sandbox guide,
  the command authority matrix, the RFC roadmap, and the operator brief.

## Verification

- `go test ./pkg/mutations -run 'TestReservedExitCodeStaysInLockstepWithAgentloop|TestRecoverySweepClassifiesDaemonObservedStaleEpochAsUnrecoverableAcrossRotation|TestRecoverySweepClassifiesDirectExit97AsUnrecoverableAcrossRotation|TestOrdinaryUnsealedExitWithNoObservationStaysAgentExitedUnsealed|TestRecoveredSessionClearsStaleEpochObservationThenOrdinaryDeathStaysUnsealed|TestTmuxPaneDeadStatus97WithoutDaemonObservationStaysUnsealed|TestTypedFloorGrantsNoAutoSealAuthority|TestRotationLockedLaneWithFreshLaneUIDLeaseCapabilityResealsDurableArtifact|TestRotationCapabilityReseal'`
- `go test ./pkg/mutations -run TestRotationLockedLaneWithFreshLaneUIDLeaseCapabilityResealsDurableArtifact -v`
  skipped the live PostgreSQL case because `STRIATUM_PG_TEST_URL` is unset in
  this environment.
- `go test ./pkg/mutations -run 'TestRecoveryReseal|TestSweep308|TestRotation|TestTypedFloor|TestRecoverySweepClassifies|TestOrdinaryUnsealed|TestRecoveredSession|TestTmuxPane'`
- `go test ./pkg/rpc`
- `go test ./pkg/rpc ./pkg/agentloop ./pkg/sessionliveness`
- `go test ./...`
- `make check-docs`

## Residual Work

Review and verify this draft with `STRIATUM_PG_TEST_URL` set so the new
PostgreSQL recovery fixtures execute rather than skip. After review acceptance,
integrate through the daemon run path.
