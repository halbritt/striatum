package artifactcontracts

import "testing"

func TestResolvePlacementUsesExplicitAllowedValue(t *testing.T) {
	if got := ResolvePlacement("synthesis", PlacementGitPublication); got != PlacementGitPublication {
		t.Fatalf("ResolvePlacement = %q, want %q", got, PlacementGitPublication)
	}
	if got := ResolvePlacement("handoff", PlacementGitPointerManifest); got != PlacementGitPointerManifest {
		t.Fatalf("ResolvePlacement pointer = %q", got)
	}
}

func TestResolvePlacementFallsBackToLegacyKindRouting(t *testing.T) {
	if got := ResolvePlacement("finding", nil); got != PlacementBlobExhaust {
		t.Fatalf("finding default = %q, want blob", got)
	}
	if got := ResolvePlacement("decision", ""); got != PlacementGitPublication {
		t.Fatalf("decision default = %q, want git", got)
	}
	if got := ResolvePlacement("synthesis", "unknown"); got != PlacementBlobExhaust {
		t.Fatalf("bad explicit default = %q, want synthesis legacy blob", got)
	}
}

func TestPlacementStorePredicates(t *testing.T) {
	if !PlacementUsesBlob(PlacementBlobExhaust) {
		t.Fatal("blob_exhaust should use blob storage")
	}
	if PlacementUsesBlob(PlacementGitPublication) {
		t.Fatal("git_publication should not use blob storage")
	}
	if !PlacementUsesGitAnchor(PlacementGitPointerManifest) {
		t.Fatal("git_pointer_manifest should use git anchor")
	}
}

func TestBlobRequiredPostureDeclared(t *testing.T) {
	for _, value := range []map[string]any{
		{"blob_required": true},
		{"artifact_placement_posture": BlobRequiredPosture},
		{"blob_posture": "required"},
		{"options": map[string]any{"artifact_blob_posture": BlobRequiredPosture}},
	} {
		if !BlobRequiredPostureDeclared(value) {
			t.Fatalf("BlobRequiredPostureDeclared(%#v) = false, want true", value)
		}
	}
	if BlobRequiredPostureDeclared(map[string]any{"artifact_placement_posture": "compatibility"}) {
		t.Fatal("compatibility posture must not require blob storage")
	}
}

func TestNormalizePlacementPosture(t *testing.T) {
	cases := []struct {
		value any
		want  string
		ok    bool
	}{
		{BlobPreferredPosture, BlobPreferredPosture, true},
		{"preferred", BlobPreferredPosture, true},
		{"required", BlobRequiredPosture, true},
		{"compatibility", GitCompatiblePosture, true},
		{"git", GitCompatiblePosture, true},
		{"unknown", "", false},
		{true, "", false},
	}
	for _, tc := range cases {
		got, ok := NormalizePlacementPosture(tc.value)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("NormalizePlacementPosture(%#v) = %q, %v; want %q, %v", tc.value, got, ok, tc.want, tc.ok)
		}
	}
}
