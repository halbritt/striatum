package reads

import (
	"context"
	"testing"
)

// TestPgReadScopeDoctorBlock pins the #164 derived-posture behavior in the
// no-database case (nil runner): no stamp can be read, so no gate is claimed,
// the posture stays broad_runtime_select, and no private-read denial claim is
// made. The live derivation (stamps + probes flipping the posture to
// partial_projection_gated, grant/ownership drift detection) is pinned by the
// PG-gated TestPgReadScopePostureDerivation.
func TestPgReadScopeDoctorBlock(t *testing.T) {
	block := pgReadScopeDoctorBlock(context.Background(), nil)
	if block["posture"] != pgReadScopeBroadRuntimeSelect {
		t.Fatalf("posture = %v, want %s", block["posture"], pgReadScopeBroadRuntimeSelect)
	}
	if block["private_read_denial"] != false {
		t.Fatalf("private_read_denial = %v, want false", block["private_read_denial"])
	}
	if block["runtime_role_select_scope"] != "broad" {
		t.Fatalf("runtime_role_select_scope = %v, want broad", block["runtime_role_select_scope"])
	}
	if block["inventory_source"] != "go/pkg/db/read_authority_inventory.go" {
		t.Fatalf("inventory_source = %v", block["inventory_source"])
	}
	if _, drifted := block["grant_drift"]; drifted {
		t.Fatalf("grant_drift reported with no stamps: %#v", block["grant_drift"])
	}
	surfaces, ok := block["representative_sensitive_surfaces"].([]string)
	if !ok || len(surfaces) == 0 {
		t.Fatalf("expected representative sensitive surfaces, got %#v", block["representative_sensitive_surfaces"])
	}
	if block["sensitive_surface_count"] != len(surfaces) {
		t.Fatalf("sensitive_surface_count = %v, want %d", block["sensitive_surface_count"], len(surfaces))
	}
	columnScoped, ok := block["column_scoped_surfaces"].([]string)
	if !ok || !containsStringItem(columnScoped, "clients") {
		t.Fatalf("column_scoped_surfaces = %#v, want clients", block["column_scoped_surfaces"])
	}
	if block["column_scoped_surface_count"] != len(columnScoped) {
		t.Fatalf("column_scoped_surface_count = %v, want %d", block["column_scoped_surface_count"], len(columnScoped))
	}
	gates, ok := block["partial_projection_gates"].([]map[string]any)
	if !ok || len(gates) != 4 {
		t.Fatalf("partial_projection_gates = %#v, want the four RFC 0113/0114 gates", block["partial_projection_gates"])
	}
	gatesBySurface := map[string]map[string]any{}
	for _, gate := range gates {
		surface, _ := gate["surface"].(string)
		gatesBySurface[surface] = gate
		// Without a database no stamp is readable and no gate may claim
		// verification.
		for _, key := range []string{"stamped", "verified", "owner_ok", "private_read_denial"} {
			if gate[key] != false {
				t.Fatalf("gate %s %s = %v, want false with nil runner", surface, key, gate[key])
			}
		}
	}
	clientsGate := gatesBySurface["clients"]
	if clientsGate == nil || clientsGate["authority_stamp"] != "auth_projection_read" || clientsGate["gate"] != "columns" {
		t.Fatalf("clients gate = %#v, want columns gate stamped auth_projection_read", clientsGate)
	}
	deniedColumns, ok := clientsGate["denied_columns"].([]string)
	if !ok || !containsStringItem(deniedColumns, "token_hash") || !containsStringItem(deniedColumns, "token_salt") {
		t.Fatalf("clients denied_columns = %#v, want token_hash/token_salt", clientsGate["denied_columns"])
	}
	for _, surface := range []string{"principals", "client_sessions"} {
		gate := gatesBySurface[surface]
		if gate == nil || gate["authority_stamp"] != "identity_projection_read" || gate["gate"] != "table" {
			t.Fatalf("%s gate = %#v, want table gate stamped identity_projection_read", surface, gate)
		}
	}
	pcGate := gatesBySurface["principal_clients"]
	if pcGate == nil || pcGate["authority_stamp"] != "identity_projection_read" || pcGate["gate"] != "columns" {
		t.Fatalf("principal_clients gate = %#v, want columns gate stamped identity_projection_read", pcGate)
	}
	pcDenied, ok := pcGate["denied_columns"].([]string)
	if !ok || !containsStringItem(pcDenied, "principal_id") {
		t.Fatalf("principal_clients denied_columns = %#v, want principal_id", pcGate["denied_columns"])
	}
	if !containsStringItem(surfaces, "artifacts") || !containsStringItem(surfaces, "events") {
		t.Fatalf("expected artifacts and events in representative surfaces, got %#v", surfaces)
	}
	if containsStringItem(surfaces, "clients") {
		t.Fatalf("clients still listed as a broad sensitive SELECT surface after the column-scoped classification: %#v", surfaces)
	}
	if !containsStringItem(surfaces, "work_packets") {
		t.Fatalf("expected work_packets in representative surfaces, got %#v", surfaces)
	}
	// principals and client_sessions are reclassified out of
	// runtime_sensitive_select by RFC 0114 (projection-read / denied).
	if containsStringItem(surfaces, "principals") || containsStringItem(surfaces, "client_sessions") {
		t.Fatalf("principals/client_sessions still listed as sensitive broad-SELECT surfaces: %#v", surfaces)
	}
}

func containsStringItem(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}
