package mutations

import (
	"testing"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
)

func TestResolvePublishPlacementUsesExpectedArtifactPlacement(t *testing.T) {
	job := map[string]any{
		"expected_artifacts_json": []any{map[string]any{
			"logical_name": "summary",
			"kind":         "synthesis",
			"path":         "docs/SUMMARY.md",
			"placement":    artifactcontracts.PlacementGitPublication,
			"required":     true,
		}},
	}
	got := resolvePublishPlacement(job, "synthesis", "summary", "docs/SUMMARY.md", 1)
	if got != artifactcontracts.PlacementGitPublication {
		t.Fatalf("placement = %q, want git publication", got)
	}
}

func TestResolvePublishPlacementFallsBackToLegacyKindDefault(t *testing.T) {
	job := map[string]any{"expected_artifacts_json": []any{}}
	if got := resolvePublishPlacement(job, "finding", "review", "docs/REVIEW.md", 1); got != artifactcontracts.PlacementBlobExhaust {
		t.Fatalf("finding placement = %q, want blob default", got)
	}
}
