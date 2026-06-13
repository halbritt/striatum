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
