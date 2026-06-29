// Package adapterconformance is the RFC 0101 Layer 2 adapter-conformance
// harness: detection + prevention for the lane-bootstrap, turn-loop, and
// lane-env-hardening contracts the production stack relies on. A CLI bump that
// breaks bootstrap (the #101 shape) or a refactor that re-leaks the daemon env
// (#87) must FAIL CI here instead of wedging a live run.
//
// This package is built in two slices (RFC 0101 Phase 2):
//
//   - Slice 1 (THIS code) — the hermetic "Tier A" core: the closed failure
//     taxonomy (this file), the ordered C0..C12 contract (contract.go), and the
//     two clauses that can be asserted with no live CLI, no daemon, and no
//     PostgreSQL: C0 (AdapterContract golden, the #101 regression gate) and C2
//     (LaneEnvHardened golden, #87). These ride inside `make -C go check` on
//     every PR. C1 and C3..C12 are DECLARED in the ordered list with their
//     ID/Name/Deadline/Profile/FailureClass, but their Assert returns
//     StatusDeferred — they require the Slice-2 live/fake-agent runner and are
//     NOT faked here.
//   - Slice 2 (DEFERRED, see doc.go) — the live/fake-agent runner
//     (runner.go + go/pkg/pgtest in-process striatumd), observer.go, preflight.go
//     (C1), fixture.go, skipledger.go + conformance_skips.json, testagent/, and
//     the go/cmd/striatum-adapter-conformance driver binary, which carry the
//     C1/C3..C12 live assertions.
//
// See docs/dogfoods/rfc-0101-l2-conformance/artifacts/DESIGN_SYNTHESIS.md for the
// accepted design (esp. §1.1 the contract, §1.2 the taxonomy, §1.3 the
// architecture, §1.4 lane-env).
package adapterconformance

// FailureClass is the closed, named set a failed clause emits (DESIGN §1.2).
// Each failed clause emits EXACTLY ONE primary class plus the structured fields
// carried on ClauseResult. The set is partitioned into:
//
//   - contract classes — a genuine non-conformance. These hard-block: the
//     adapter/runner is doing the wrong thing.
//   - infra classes — the contract could not be run (binary absent,
//     unauthenticated, provider erroring). These gate whether the live clauses
//     run and are loud-but-neutral per-PR (re-runnable), never a contract
//     failure.
//
// The partition is total and disjoint over the declared classes; the harness
// self-validates this in taxonomy_test.go.
type FailureClass string

const (
	// FailureNone is the zero value: a passing or not-yet-run clause carries no
	// FailureClass. It belongs to NEITHER partition.
	FailureNone FailureClass = ""

	// --- Contract classes (C0, C2..C12): a genuine non-conformance. ---

	// AdapterContractViolation — C0: the adapter bootstrap-delivery construction
	// in go/pkg/agentloop does not match the declared contract (e.g. a revert to
	// pty_submit, a dropped codex URL override, or a missing #76 survey key).
	AdapterContractViolation FailureClass = "AdapterContractViolation"
	// EnvSecretLeak — C2: a banned DSN/Postgres/secret var reaches the child env.
	EnvSecretLeak FailureClass = "EnvSecretLeak"
	// BootstrapStall — C3: no last_tools_list_at by deadline, no progress bytes.
	BootstrapStall FailureClass = "BootstrapStall"
	// SurveyPromptBlocked — C3 (agy): alive, survey-suppressed, yet no progress.
	SurveyPromptBlocked FailureClass = "SurveyPromptBlocked"
	// DiscoveryProbeStall — C4: tools/list + progress bytes but no foreground
	// claim (#85).
	DiscoveryProbeStall FailureClass = "DiscoveryProbeStall"
	// AwaitPacketStall — C4: tools/list but no await_packet at all.
	AwaitPacketStall FailureClass = "AwaitPacketStall"
	// AckStall — C5: packet delivered, never acked.
	AckStall FailureClass = "AckStall"
	// HeartbeatMissed — C6: lease held, heartbeat stops.
	HeartbeatMissed FailureClass = "HeartbeatMissed"
	// InterrogationIgnored — C7: question pending past deadline.
	InterrogationIgnored FailureClass = "InterrogationIgnored"
	// ArtifactMissing — C8: no artifact row/blob.
	ArtifactMissing FailureClass = "ArtifactMissing"
	// ArtifactRejected — C8: publish rejected (front_matter exit 6 / write_scope
	// / byline / hash); the specific reason rides in ClauseResult.Fields.
	ArtifactRejected FailureClass = "ArtifactRejected"
	// CompleteMissing — C9: never completed.
	CompleteMissing FailureClass = "CompleteMissing"
	// LoopWedged — C10: stall raised instead of clean idle.
	LoopWedged FailureClass = "LoopWedged"
	// NoWorkLoopMissed — C10: never re-awaited after complete.
	NoWorkLoopMissed FailureClass = "NoWorkLoopMissed"
	// TurnExitEarly — C11: agent_exited before contract end.
	TurnExitEarly FailureClass = "TurnExitEarly"
	// TokenLeakInWorkTree — C12: bearer / .gemini residue in the work tree.
	TokenLeakInWorkTree FailureClass = "TokenLeakInWorkTree"
	// ControlPlaneHelperInWorkTree — C12: lane-authored control-plane helper.
	ControlPlaneHelperInWorkTree FailureClass = "ControlPlaneHelperInWorkTree"
	// KnownGapExpired — a skip past its expiry or version ceiling.
	KnownGapExpired FailureClass = "KnownGapExpired"
	// UnexpectedKnownGapPass — a skipped agy clause unexpectedly passes.
	UnexpectedKnownGapPass FailureClass = "UnexpectedKnownGapPass"

	// --- Infra classes (pre-flight C1): the test could not be run. ---

	// AdapterUnavailable — C1: declared binary absent on the supervised PATH.
	AdapterUnavailable FailureClass = "AdapterUnavailable"
	// AdapterVersionUnknown — C1: binary present, version uncapturable.
	AdapterVersionUnknown FailureClass = "AdapterVersionUnknown"
	// AdapterUnauthenticated — C1: present but no usable credentials.
	AdapterUnauthenticated FailureClass = "AdapterUnauthenticated"
	// AdapterProviderUnavailable — C1 (or a post-green retry): reachable but the
	// provider is erroring/429/outage.
	AdapterProviderUnavailable FailureClass = "AdapterProviderUnavailable"
)

// contractClasses is the closed set of contract-partition classes.
var contractClasses = map[FailureClass]bool{
	AdapterContractViolation:     true,
	EnvSecretLeak:                true,
	BootstrapStall:               true,
	SurveyPromptBlocked:          true,
	DiscoveryProbeStall:          true,
	AwaitPacketStall:             true,
	AckStall:                     true,
	HeartbeatMissed:              true,
	InterrogationIgnored:         true,
	ArtifactMissing:              true,
	ArtifactRejected:             true,
	CompleteMissing:              true,
	LoopWedged:                   true,
	NoWorkLoopMissed:             true,
	TurnExitEarly:                true,
	TokenLeakInWorkTree:          true,
	ControlPlaneHelperInWorkTree: true,
	KnownGapExpired:              true,
	UnexpectedKnownGapPass:       true,
}

// infraClasses is the closed set of infra-partition classes.
var infraClasses = map[FailureClass]bool{
	AdapterUnavailable:         true,
	AdapterVersionUnknown:      true,
	AdapterUnauthenticated:     true,
	AdapterProviderUnavailable: true,
}

// AllFailureClasses returns every declared FailureClass (excluding the
// FailureNone zero value), in a stable order: contract classes first, then
// infra classes, each in declaration order. Used by the taxonomy
// self-validation and report rendering.
func AllFailureClasses() []FailureClass {
	return []FailureClass{
		// contract
		AdapterContractViolation,
		EnvSecretLeak,
		BootstrapStall,
		SurveyPromptBlocked,
		DiscoveryProbeStall,
		AwaitPacketStall,
		AckStall,
		HeartbeatMissed,
		InterrogationIgnored,
		ArtifactMissing,
		ArtifactRejected,
		CompleteMissing,
		LoopWedged,
		NoWorkLoopMissed,
		TurnExitEarly,
		TokenLeakInWorkTree,
		ControlPlaneHelperInWorkTree,
		KnownGapExpired,
		UnexpectedKnownGapPass,
		// infra
		AdapterUnavailable,
		AdapterVersionUnknown,
		AdapterUnauthenticated,
		AdapterProviderUnavailable,
	}
}

// IsContract reports whether the class is a contract-partition failure (a
// genuine non-conformance that hard-blocks).
func (c FailureClass) IsContract() bool {
	return contractClasses[c]
}

// IsInfra reports whether the class is an infra-partition outcome (the contract
// could not be run; gated, never a contract failure).
func (c FailureClass) IsInfra() bool {
	return infraClasses[c]
}

// String returns the class name (the FailureClass is its own name).
func (c FailureClass) String() string { return string(c) }
