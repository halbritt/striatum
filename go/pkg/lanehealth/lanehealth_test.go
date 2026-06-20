package lanehealth

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
)

func TestClassify(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name     string
		facts    Facts
		expected Health
	}{
		{
			name: "No supervisor recorded",
			facts: Facts{
				SupervisorRecorded: false,
			},
			expected: Health{
				Reason: ReasonNoAttachedSupervisor,
			},
		},
		{
			name: "Supervisor not attached",
			facts: Facts{
				SupervisorRecorded: true,
				SupervisorState:    "detached",
			},
			expected: Health{
				Reason: ReasonNoAttachedSupervisor,
			},
		},
		{
			name: "PID missing",
			facts: Facts{
				SupervisorRecorded: true,
				SupervisorState:    "attached",
				PID:                0,
			},
			expected: Health{
				Reason: ReasonPIDMissing,
			},
		},
		{
			name: "Daemon supervisor missing (no pointer)",
			facts: Facts{
				SupervisorRecorded: true,
				SupervisorState:    "attached",
				PID:                123,
				HasPointer:         false,
			},
			expected: Health{
				Reason: ReasonDaemonSupervisorMissing,
				PID:    123,
			},
		},
		{
			name: "Pointer state mismatch",
			facts: Facts{
				SupervisorRecorded:        true,
				SupervisorState:           "attached",
				PID:                       123,
				HasPointer:                true,
				PointerDaemonSupervisorID: "dsup_1",
				PointerState:              "detached",
			},
			expected: Health{
				Reason: ReasonPointerStateMismatch,
				PID:    123,
			},
		},
		{
			name: "Daemon supervisor missing (no daemon record)",
			facts: Facts{
				SupervisorRecorded:        true,
				SupervisorState:           "attached",
				PID:                       123,
				HasPointer:                true,
				PointerDaemonSupervisorID: "dsup_1",
				PointerState:              "attached",
				HasDaemonSupervisor:       false,
			},
			expected: Health{
				Reason: ReasonDaemonSupervisorMissing,
				PID:    123,
			},
		},
		{
			name: "Daemon state mismatch",
			facts: Facts{
				SupervisorRecorded:        true,
				SupervisorState:           "attached",
				PID:                       123,
				HasPointer:                true,
				PointerDaemonSupervisorID: "dsup_1",
				PointerState:              "attached",
				HasDaemonSupervisor:       true,
				DaemonSupervisorID:        "dsup_1",
				DaemonState:               "detached",
			},
			expected: Health{
				Reason: ReasonDaemonStateMismatch,
				PID:    123,
			},
		},
		{
			name: "Pointer PID mismatch",
			facts: Facts{
				SupervisorRecorded:        true,
				SupervisorState:           "attached",
				PID:                       123,
				HasPointer:                true,
				PointerDaemonSupervisorID: "dsup_1",
				PointerState:              "attached",
				HasDaemonSupervisor:       true,
				DaemonSupervisorID:        "dsup_1",
				DaemonState:               "attached",
				PointerPID:                456,
			},
			expected: Health{
				Reason: ReasonPointerPIDMismatch,
				PID:    123,
			},
		},
		{
			name: "Tmux metadata corrupt",
			facts: Facts{
				SupervisorRecorded:        true,
				SupervisorState:           "attached",
				PID:                       123,
				HasPointer:                true,
				PointerDaemonSupervisorID: "dsup_1",
				PointerState:              "attached",
				HasDaemonSupervisor:       true,
				DaemonSupervisorID:        "dsup_1",
				DaemonState:               "attached",
				PointerPID:                123,
				PointerTmuxMeta: gosupervisor.TmuxMeta{
					Tmux: gosupervisor.TmuxMetaBlock{
						State: "backed", // IsValidTmux() will be false since pane_pid/sessionname/paneid are empty
					},
				},
			},
			expected: Health{
				Reason: ReasonTmuxMetadataCorrupt,
				PID:    123,
			},
		},
		{
			name: "Probe performed: PID gone",
			facts: Facts{
				SupervisorRecorded:        true,
				SupervisorState:           "attached",
				PID:                       123,
				HasPointer:                true,
				PointerDaemonSupervisorID: "dsup_1",
				PointerState:              "attached",
				HasDaemonSupervisor:       true,
				DaemonSupervisorID:        "dsup_1",
				DaemonState:               "attached",
				PointerPID:                123,
				ProbePerformed:            true,
				ProbeResult: gosupervisor.LaneLiveness{
					Alive: false,
					Class: "pid_gone",
				},
			},
			expected: Health{
				Bound:         true,
				Alive:         false,
				Reason:        ReasonPIDGone,
				LivenessClass: "pid_gone",
				PID:           123,
			},
		},
		{
			name: "Probe performed: start token unverified",
			facts: Facts{
				SupervisorRecorded:        true,
				SupervisorState:           "attached",
				PID:                       123,
				HasPointer:                true,
				PointerDaemonSupervisorID: "dsup_1",
				PointerState:              "attached",
				HasDaemonSupervisor:       true,
				DaemonSupervisorID:        "dsup_1",
				DaemonState:               "attached",
				PointerPID:                123,
				ProbePerformed:            true,
				ProbeResult: gosupervisor.LaneLiveness{
					Alive:  true,
					Backed: "tmux",
					Detail: "start_token_unverified",
				},
			},
			expected: Health{
				Bound:  true,
				Alive:  true,
				Reason: ReasonStartTokenUnverified,
				PID:    123,
			},
		},
		{
			name: "Attested and healthy lane",
			facts: Facts{
				SupervisorRecorded:        true,
				SupervisorState:           "attached",
				PID:                       123,
				HasPointer:                true,
				PointerDaemonSupervisorID: "dsup_1",
				PointerState:              "attached",
				HasDaemonSupervisor:       true,
				DaemonSupervisorID:        "dsup_1",
				DaemonState:               "attached",
				PointerPID:                123,
				ProbePerformed:            true,
				ProbeResult: gosupervisor.LaneLiveness{
					Alive: true,
					Class: "alive",
				},
			},
			expected: Health{
				Bound:         true,
				Alive:         true,
				Attested:      true,
				LivenessClass: "alive",
				PID:           123,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Classify(tt.facts, now)
			// Reset stall details in result for easy comparison
			res.Stall = tt.expected.Stall
			res.SupervisorID = tt.expected.SupervisorID
			res.Deliverable = tt.expected.Deliverable
			if !reflect.DeepEqual(res, tt.expected) {
				t.Errorf("Classify() = %#v, expected %#v", res, tt.expected)
			}
		})
	}
}

type namedTmuxRunner struct {
	name string
}

func (r namedTmuxRunner) Run(_ context.Context, _ ...string) (string, error) {
	return "", nil
}

func TestProdProbeSelectsRunAsTmuxRunnerFromMetadata(t *testing.T) {
	defaultRunner := namedTmuxRunner{name: "default"}
	probe := ProdProbe{
		Runner: defaultRunner,
		RunAsRunner: func(runAsUser string) gosupervisor.TmuxRunner {
			if runAsUser != "striatum-lane" {
				t.Fatalf("RunAsRunner user = %q, want striatum-lane", runAsUser)
			}
			return namedTmuxRunner{name: "run-as"}
		},
	}

	runner := probe.runnerForMeta(gosupervisor.TmuxMeta{
		Tmux: gosupervisor.TmuxMetaBlock{RunAsUser: " striatum-lane "},
	})
	got, ok := runner.(namedTmuxRunner)
	if !ok || got.name != "run-as" {
		t.Fatalf("runnerForMeta returned %#v, want run-as runner", runner)
	}
}

func TestProdProbeUsesInjectedRunnerWithoutRunAsMetadata(t *testing.T) {
	defaultRunner := namedTmuxRunner{name: "default"}
	probe := ProdProbe{
		Runner: defaultRunner,
		RunAsRunner: func(runAsUser string) gosupervisor.TmuxRunner {
			t.Fatalf("RunAsRunner called for user %q", runAsUser)
			return nil
		},
	}

	runner := probe.runnerForMeta(gosupervisor.TmuxMeta{})
	got, ok := runner.(namedTmuxRunner)
	if !ok || got.name != "default" {
		t.Fatalf("runnerForMeta returned %#v, want injected default runner", runner)
	}
}

// healthyBoundFacts returns Facts for a fully attached, bound, probe-alive lane
// (the structural prerequisites for delivery classification), parameterized by
// the recorded delivery degradation.
func healthyBoundFacts(deliveryDegraded bool, deliveryReason string, probeAlive bool) Facts {
	return Facts{
		SupervisorRecorded:        true,
		SupervisorState:           "attached",
		PID:                       123,
		HasPointer:                true,
		PointerDaemonSupervisorID: "dsup_1",
		PointerState:              "attached",
		HasDaemonSupervisor:       true,
		DaemonSupervisorID:        "dsup_1",
		DaemonState:               "attached",
		PointerPID:                123,
		ProbePerformed:            true,
		ProbeResult:               gosupervisor.LaneLiveness{Alive: probeAlive, Class: "alive"},
		DeliveryDegraded:          deliveryDegraded,
		DeliveryReason:            deliveryReason,
	}
}

// TestClassifyAttachClientExitedDeliveryReconciliation guards #63 F7: an exited
// tmux attach-session OBSERVER client must not mark packet delivery degraded
// while the pane is alive and the real transport is healthy. Genuine transport
// failures (helper_process_gone, stdin_reader_missing) must still degrade
// delivery, and an attach exit on a NOT-alive pane must not be silently cleared.
func TestClassifyAttachClientExitedDeliveryReconciliation(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name            string
		facts           Facts
		wantDeliverable bool
		wantReason      string
	}{
		{
			name:            "benign attach exit on a live pane is deliverable",
			facts:           healthyBoundFacts(true, "attach_client_exited", true),
			wantDeliverable: true,
			wantReason:      "",
		},
		{
			name:            "helper_process_gone still degrades delivery",
			facts:           healthyBoundFacts(true, "helper_process_gone", true),
			wantDeliverable: false,
			wantReason:      "helper_process_gone",
		},
		{
			name:            "stdin_reader_missing still degrades delivery",
			facts:           healthyBoundFacts(true, "stdin_reader_missing", true),
			wantDeliverable: false,
			wantReason:      "stdin_reader_missing",
		},
		{
			// Without positive probe evidence that the pane is alive we keep the
			// recorded degradation rather than assume the transport is healthy.
			name:            "attach exit on a dead pane is not cleared",
			facts:           healthyBoundFacts(true, "attach_client_exited", false),
			wantDeliverable: false,
			wantReason:      "attach_client_exited",
		},
		{
			name:            "healthy lane stays deliverable",
			facts:           healthyBoundFacts(false, "", true),
			wantDeliverable: true,
			wantReason:      "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.facts, now)
			if got.Deliverable != tt.wantDeliverable {
				t.Fatalf("Deliverable = %v, want %v (health=%#v)", got.Deliverable, tt.wantDeliverable, got)
			}
			if got.DeliveryReason != tt.wantReason {
				t.Fatalf("DeliveryReason = %q, want %q", got.DeliveryReason, tt.wantReason)
			}
		})
	}
}

func TestLegacyMapCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		health   Health
		expected map[string]any
	}{
		{
			name: "Attested",
			health: Health{
				SupervisorID:  "sup_1",
				PID:           123,
				Attested:      true,
				LivenessClass: "alive",
			},
			expected: map[string]any{
				"attested":      true,
				"state":         "attested",
				"supervisor_id": "sup_1",
				"pid":           123,
				"reason":        nil,
				"liveness":      "alive",
			},
		},
		{
			name: "Unattested - no attached supervisor",
			health: Health{
				Reason: ReasonNoAttachedSupervisor,
			},
			expected: map[string]any{
				"attested":      false,
				"state":         "unattested",
				"supervisor_id": nil,
				"pid":           nil,
				"reason":        "no_attached_supervisor",
			},
		},
		{
			name: "Unattested - pid missing",
			health: Health{
				SupervisorID: "sup_1",
				Reason:       ReasonPIDMissing,
			},
			expected: map[string]any{
				"attested":      false,
				"state":         "unattested",
				"supervisor_id": "sup_1",
				"pid":           nil,
				"reason":        "pid_missing",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LegacyMap(tt.health)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("LegacyMap() = %#v, expected %#v", got, tt.expected)
			}
		})
	}
}

// wedgedToolProgressFacts returns Facts for a fully attached, bound lane that is
// holding an active lease, is still PTY-fresh (working_local), but has issued no
// tool call for well over ToolProgressSeconds (600s) — so sessionliveness.Classify
// reclassifies it wedged_no_tool_progress (#324). The active PID probe is
// parameterized so the same fixture exercises both the alive-and-identity-matched
// (RFC 0140 alive_but_silent) case and the confirmed-dead forgery-guard case.
func wedgedToolProgressFacts(now time.Time, probeAlive bool, probeClass string) Facts {
	// Lease heartbeat is inside the lease-heartbeat window (LeaseHeartbeatSeconds+
	// slack = 330s) so the lease is NOT stalled, but past ProtocolFreshSeconds (60s)
	// so the lane is NOT working_protocol — leaving the fresh PTY frame as the only
	// fresh signal, i.e. working_local, which is exactly where the #324 tool-progress
	// wedge fires.
	leaseHB := now.Add(-120 * time.Second)  // lease fresh but not protocol-fresh
	ptyFresh := now.Add(-5 * time.Second)   // PTY frames fresh => working_local
	staleTool := now.Add(-20 * time.Minute) // last tool call long past 600s
	// Protocol signals are stale (well past ProtocolFreshSeconds=60s) so the lane
	// is NOT working_protocol; an await_packet recorded AFTER tools/list keeps the
	// classifier off the await-packet rung and onto the active-lease rung, where
	// the #324 tool-progress wedge fires on the stale tool-call timeline.
	staleProto := now.Add(-10 * time.Minute)
	awaitAfter := now.Add(-9 * time.Minute) // after tools/list, still stale
	return Facts{
		SupervisorRecorded:        true,
		SupervisorState:           "attached",
		PID:                       123,
		HasPointer:                true,
		PointerDaemonSupervisorID: "dsup_1",
		PointerState:              "attached",
		HasDaemonSupervisor:       true,
		DaemonSupervisorID:        "dsup_1",
		DaemonState:               "attached",
		PointerPID:                123,
		ProbePerformed:            true,
		ProbeResult:               gosupervisor.LaneLiveness{Alive: probeAlive, Class: probeClass},
		LivenessPolicy:            sessionliveness.DefaultPolicy(),
		SessionActivity: sessionliveness.Activity{
			SessionState:           "active",
			LastMCPRequestAt:       &staleProto,
			LastToolsListAt:        &staleTool,
			LastAwaitPacketAt:      &awaitAfter,
			LastToolCallStartedAt:  &staleTool,
			LastToolCallFinishedAt: &staleTool,
			LastPTYActivityAt:      &ptyFresh,
			ActiveLeaseID:          "lease_1",
			ActiveLeaseAcquiredAt:  &leaseHB,
			ActiveLeaseHeartbeatAt: &leaseHB,
			LastWorkHeartbeatAt:    &leaseHB,
			Transport:              sessionliveness.TransportPTYHelper,
		},
	}
}

// TestClassifyAliveButSilentKeepsAttestation is the RFC 0140 part B fix: a lane
// doing honest long local work — an active + identity-matched PID (active probe
// Alive == true), but no tool call for >= ToolProgressSeconds — must classify as
// alive_but_silent and KEEP attestation, rather than dropping it on the
// wedged_no_tool_progress stall. Without the fix step 6 sets
// ReasonSupervisorStalled and returns Attested=false.
func TestClassifyAliveButSilentKeepsAttestation(t *testing.T) {
	now := time.Now().UTC()
	f := wedgedToolProgressFacts(now, true, "alive")

	// Precondition: the underlying liveness verdict really is the #324 wedge,
	// so this test exercises the seam the RFC describes (not some other path).
	if got := sessionliveness.Classify(f.SessionActivity, f.LivenessPolicy, now).StallClass; got != sessionliveness.StallToolProgress {
		t.Fatalf("fixture precondition: stall class = %q, want %q", got, sessionliveness.StallToolProgress)
	}

	got := Classify(f, now)
	if !got.Alive {
		t.Fatalf("expected Alive=true (active PID probe positive), got %#v", got)
	}
	if !got.Attested {
		t.Fatalf("RFC 0140: a PID-alive, identity-matched, tool-call-silent lane must KEEP attestation; got Attested=false reason=%q", got.Reason)
	}
	if got.Reason == ReasonSupervisorStalled {
		t.Fatalf("alive_but_silent must not be a ReasonSupervisorStalled attestation drop; got reason=%q", got.Reason)
	}
	if got.LivenessClass != LivenessAliveButSilent {
		t.Fatalf("LivenessClass = %q, want %q", got.LivenessClass, LivenessAliveButSilent)
	}
	// The byline derivation reads LegacyMap(attested) -> the role byline.
	if LegacyMap(got)["attested"] != true {
		t.Fatalf("LegacyMap must report attested=true for alive_but_silent; got %#v", LegacyMap(got))
	}
}

// TestClassifyWedgedDeadLaneLosesAttestation is the RFC 0026 / D080 forgery-guard
// regression: the SAME tool-call-silent wedge on a lane whose active PID probe
// FAILS (PID gone / identity mismatch) must STILL be unattested and reaped — the
// alive_but_silent exemption must never rescue a genuinely dead lane. A dead probe
// is caught at step 5 (before stall classification), so this also proves the
// exemption keys on the same forgery-resistant PID oracle, not on PTY freshness.
func TestClassifyWedgedDeadLaneLosesAttestation(t *testing.T) {
	now := time.Now().UTC()
	f := wedgedToolProgressFacts(now, false, "pid_gone")

	got := Classify(f, now)
	if got.Alive {
		t.Fatalf("expected Alive=false for a confirmed-dead PID probe, got %#v", got)
	}
	if got.Attested {
		t.Fatalf("forgery guard: a confirmed-dead lane must lose attestation regardless of PTY freshness; got Attested=true")
	}
	if got.Reason != ReasonPIDGone {
		t.Fatalf("expected ReasonPIDGone (dead probe caught before stall), got %q", got.Reason)
	}
	if got.LivenessClass == LivenessAliveButSilent {
		t.Fatalf("a dead lane must never be classified alive_but_silent")
	}
}

// TestClassifyWedgedPipeLaneStaysUnattested guards RFC 0140 acceptance criterion
// 6 (pipe transport stays degrade-safe): a wedged_no_tool_progress lane with NO
// positive active PID probe (ProbePerformed=false — the pipe/no-oracle case) does
// NOT get the alive_but_silent exemption and keeps today's behavior
// (ReasonSupervisorStalled, Attested=false). The exemption keys on the PID probe,
// never on PTY freshness or the liveness verdict alone.
func TestClassifyWedgedPipeLaneStaysUnattested(t *testing.T) {
	now := time.Now().UTC()
	f := wedgedToolProgressFacts(now, true, "alive")
	// No PID oracle available for this lane (pipe transport / probe not performed).
	f.ProbePerformed = false
	f.ProbeResult = gosupervisor.LaneLiveness{}

	got := Classify(f, now)
	if got.Attested {
		t.Fatalf("a wedged lane with no positive PID probe must stay unattested (degrade-safe); got Attested=true")
	}
	if got.Reason != ReasonSupervisorStalled {
		t.Fatalf("expected ReasonSupervisorStalled for the no-oracle wedge, got %q", got.Reason)
	}
	if got.LivenessClass == LivenessAliveButSilent {
		t.Fatalf("a no-PID-oracle lane must never be classified alive_but_silent")
	}
}
