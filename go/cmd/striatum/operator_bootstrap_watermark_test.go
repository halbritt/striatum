package main

import (
	"strings"
	"testing"
)

// TestBootstrapNextActionsSurfacesOwnerDDLApply is the RFC 0142 Layer 2 operator
// surfacing: when doctor reports an owner_bundle_watermark_shortfall, the operator
// bootstrap packet hoists the exact out-of-band remediation
// (`striatum daemon owner-ddl apply`) as a next_action so the operator applies it
// BEFORE restarting, rather than discovering it as a boot-time halt.
func TestBootstrapNextActionsSurfacesOwnerDDLApply(t *testing.T) {
	packet := operatorBootstrapPacket{
		Daemon: operatorDaemonSummary{Reachable: true, Authorized: true},
		Repository: operatorRepositorySummary{
			Root:         "/repo",
			RepositoryID: "repo_1",
		},
		Doctor: operatorDoctorSummary{
			Available:    true,
			OK:           false,
			ProblemCount: 1,
			Problems: []string{
				"owner_bundle_watermark_shortfall: applied owner-bundle watermark 1 is below the 19 this binary requires; apply the pending owner bundle(s) as the database owner (striatum daemon owner-ddl apply) BEFORE restarting onto this binary, or the daemon will halt with awaiting_owner_ddl (RFC 0142 Layer 2)",
			},
		},
	}
	actions := bootstrapNextActions(packet)
	joined := strings.Join(actions, "\n")
	if !strings.Contains(joined, "striatum daemon owner-ddl apply") {
		t.Fatalf("next actions must surface the owner-ddl apply remediation; got: %#v", actions)
	}
}

// TestBootstrapNextActionsNoOwnerDDLWhenHealthy confirms a healthy doctor (no
// watermark shortfall) does NOT inject the owner-ddl apply remediation.
func TestBootstrapNextActionsNoOwnerDDLWhenHealthy(t *testing.T) {
	packet := operatorBootstrapPacket{
		Daemon:     operatorDaemonSummary{Reachable: true, Authorized: true},
		Repository: operatorRepositorySummary{Root: "/repo", RepositoryID: "repo_1"},
		Doctor:     operatorDoctorSummary{Available: true, OK: true},
	}
	actions := bootstrapNextActions(packet)
	if strings.Contains(strings.Join(actions, "\n"), "owner-ddl apply") {
		t.Fatalf("healthy bootstrap must not surface owner-ddl apply; got: %#v", actions)
	}
}
