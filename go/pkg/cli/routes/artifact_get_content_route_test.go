package routes

import (
	"strings"
	"testing"
)

// Issue #506 part (b): a reviewer finding routed to blob_exhaust storage is not
// surfaced by evidence/corpus export (metadata only) and has no CLI to fetch its
// body by id, so the operator can only reconstruct a needs_revision verdict from
// PTY noise. The read-only artifact.get_content RPC already returns body_base64
// (it backs the web UI), but it is not reachable from the CLI. This guard pins
// the operator-facing `striatum artifact get-content <artifact-id>` route so the
// body is fetchable from the command line; it maps the single positional to
// artifact_id and inherits the handler's read capability + single_repo scope.
func TestArtifactGetContentRouteIsReachable(t *testing.T) {
	route, consumed, ok := Lookup([]string{"artifact", "get-content", "art_1"})
	if !ok {
		t.Fatalf("artifact get-content route was not found")
	}
	if consumed != 2 {
		t.Fatalf("consumed = %d, want 2", consumed)
	}
	if route.Method != "artifact.get_content" {
		t.Fatalf("route.Method = %q, want artifact.get_content", route.Method)
	}
	if route.ParamsGroup != "artifact_get_content" {
		t.Fatalf("route.ParamsGroup = %q, want artifact_get_content", route.ParamsGroup)
	}
	if route.RequiredCapability != "read" {
		t.Fatalf("route.RequiredCapability = %q, want read", route.RequiredCapability)
	}
	if route.RepositoryScopeMode != "single_repo" {
		t.Fatalf("route.RepositoryScopeMode = %q, want single_repo", route.RepositoryScopeMode)
	}
	inGenerated := false
	for _, r := range generatedRoutes {
		if r.Method == "artifact.get_content" {
			inGenerated = true
			break
		}
	}
	if !inGenerated {
		t.Fatalf("artifact.get_content must be an on-contract generated route")
	}
	if help := route.RenderHelp(); !strings.Contains(help, "artifact-id") {
		t.Fatalf("help does not advertise <artifact-id>: %q", help)
	}
}
