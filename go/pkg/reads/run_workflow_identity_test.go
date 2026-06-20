package reads

import "testing"

// TestDecorateRunWorkflowIdentity covers RFC 0042: a run row carries a curated
// workflow_name folded from the joined workflow_snapshots identity, and the raw
// snapshot columns are removed so the run payload exposes one stable field.
func TestDecorateRunWorkflowIdentity(t *testing.T) {
	cases := []struct {
		name        string
		row         map[string]any
		wantName    any
		wantPresent bool
	}{
		{
			name:        "id and version",
			row:         map[string]any{"workflow_id": "refactoring-campaign", "workflow_version": "v3"},
			wantName:    "refactoring-campaign @ v3",
			wantPresent: true,
		},
		{
			name:        "id only",
			row:         map[string]any{"workflow_id": "design-run", "workflow_version": ""},
			wantName:    "design-run",
			wantPresent: true,
		},
		{
			name:        "id only, version absent",
			row:         map[string]any{"workflow_id": "design-run"},
			wantName:    "design-run",
			wantPresent: true,
		},
		{
			name:        "no workflow identity (left join miss)",
			row:         map[string]any{"workflow_id": "", "workflow_version": ""},
			wantPresent: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decorateRunWorkflowIdentity(tc.row)
			if _, leaked := tc.row["workflow_id"]; leaked {
				t.Fatalf("raw workflow_id not folded away: %#v", tc.row)
			}
			if _, leaked := tc.row["workflow_version"]; leaked {
				t.Fatalf("raw workflow_version not folded away: %#v", tc.row)
			}
			got, present := tc.row["workflow_name"]
			if present != tc.wantPresent {
				t.Fatalf("workflow_name present = %v, want %v (row %#v)", present, tc.wantPresent, tc.row)
			}
			if tc.wantPresent && got != tc.wantName {
				t.Fatalf("workflow_name = %#v, want %#v", got, tc.wantName)
			}
		})
	}
}
