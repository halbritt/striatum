package mutations

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestWriteScopeViolationsRejectsOutsideAndForbiddenPaths(t *testing.T) {
	got := writeScopeViolations(
		[]string{"docs/rfc-0050/design/codex/DESIGN.md", "go/pkg/mcp/capabilities.go", ".striatum/scratch/pid"},
		[]string{"docs/rfc-0050/design/codex/"},
		[]string{".striatum/"},
	)
	want := []string{".striatum/scratch/pid", "go/pkg/mcp/capabilities.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("violations = %#v, want %#v", got, want)
	}
}

func TestWriteScopeViolationsAllowsBroadScopeButStillHonorsForbidden(t *testing.T) {
	got := writeScopeViolations(
		[]string{"src/striatum/workflow.py", ".striatum/state"},
		[]string{"."},
		[]string{".striatum/"},
	)
	want := []string{".striatum/state"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("violations = %#v, want %#v", got, want)
	}
}

func TestWriteScopeViolationsIgnoresMatchingSiblingArtifacts(t *testing.T) {
	got := writeScopeViolationsWithIgnored(
		[]string{
			"docs/rfc/design/codex/DESIGN.md",
			"docs/rfc/design/agy/DESIGN.md",
			"go/pkg/mcp/capabilities.go",
			".striatum/scratch/pid",
		},
		[]string{"docs/rfc/design/codex/"},
		[]string{".striatum/"},
		map[string]bool{"docs/rfc/design/agy/DESIGN.md": true},
	)
	want := []string{".striatum/scratch/pid", "go/pkg/mcp/capabilities.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("violations = %#v, want %#v", got, want)
	}
}

func TestPublishedRunArtifactIgnoredPathsRequiresMatchingDigest(t *testing.T) {
	repo := t.TempDir()
	publishedPath := "docs/rfc/design/agy/DESIGN.md"
	stalePath := "docs/rfc/design/claude/DESIGN.md"
	mustWrite(t, filepath.Join(repo, publishedPath), "published sibling\n")
	mustWrite(t, filepath.Join(repo, stalePath), "current file\n")

	ignored, err := publishedRunArtifactIgnoredPaths(
		context.Background(),
		writeScopeArtifactRunner{rows: []map[string]any{
			{"repo_path": publishedPath, "content_sha256": sha256Hex([]byte("published sibling\n"))},
			{"repo_path": stalePath, "content_sha256": sha256Hex([]byte("old digest\n"))},
		}},
		"repo_1",
		repo,
		map[string]any{"run_id": "run_1", "job_id": "job_current"},
		[]string{publishedPath, stalePath},
	)
	if err != nil {
		t.Fatalf("ignored paths: %v", err)
	}
	want := map[string]bool{publishedPath: true}
	if !reflect.DeepEqual(ignored, want) {
		t.Fatalf("ignored = %#v, want %#v", ignored, want)
	}
}

func TestParseGitPorcelainZIncludesRenameOldAndNewPaths(t *testing.T) {
	got := parseGitPorcelainZ([]byte("R  docs/new.md\x00docs/old.md\x00?? tests/new_test.py\x00"))
	want := []string{"docs/new.md", "docs/old.md", "tests/new_test.py"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
}

func TestBaselinePreexistingOutOfScopeIgnoresOnlyOutOfScopeBaselinePaths(t *testing.T) {
	job := map[string]any{
		"write_scope_baseline": map[string]any{
			"changed_paths": []any{
				map[string]any{"path": "docs/operator/workflows/x/workflow.json", "hash": "h1"},
				map[string]any{"path": "go/pkg/foo/foo.go", "hash": "h2"},
				map[string]any{"path": ".agents/scratch.md", "hash": "h3"},
			},
		},
	}
	allowed := []string{"go/", "docs/operator/workflows/x/artifacts/"}
	got := baselinePreexistingOutOfScope(job, allowed)
	// In-scope baseline path (under go/) must NOT be ignored; out-of-scope
	// pre-existing paths (the workflow.json + unrelated .agents scratch) must be.
	if got["go/pkg/foo/foo.go"] {
		t.Fatalf("in-scope baseline path should not be ignored: %v", got)
	}
	if !got["docs/operator/workflows/x/workflow.json"] || !got[".agents/scratch.md"] {
		t.Fatalf("out-of-scope baseline paths should be ignored: %v", got)
	}
}

func TestBaselinePreexistingOutOfScopeEmptyWhenNoAllowedPaths(t *testing.T) {
	job := map[string]any{"write_scope_baseline": map[string]any{"changed_paths": []any{
		map[string]any{"path": "anything.txt", "hash": "h"},
	}}}
	if got := baselinePreexistingOutOfScope(job, nil); len(got) != 0 {
		t.Fatalf("no allowed_paths means no baseline filtering, got %v", got)
	}
}

func TestGitTouchedPathsSinceBaselineIgnoresPreExistingUntrackedPaths(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "Test User")
	mustWrite(t, filepath.Join(repo, "allowed.txt"), "base\n")
	runGit(t, repo, "add", "allowed.txt")
	runGit(t, repo, "commit", "-m", "base")
	mustWrite(t, filepath.Join(repo, "outside-preexisting.txt"), "already here\n")

	baseline, err := gitChangedPathSnapshots(context.Background(), repo)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	entries := make([]any, 0, len(baseline))
	for _, item := range baseline {
		entries = append(entries, map[string]any{"path": item.Path, "hash": item.Hash})
	}

	mustWrite(t, filepath.Join(repo, "allowed.txt"), "changed\n")
	touched, err := gitTouchedPathsSinceBaseline(context.Background(), repo, map[string]any{
		"write_scope_baseline": map[string]any{"changed_paths": entries},
	})
	if err != nil {
		t.Fatalf("touched: %v", err)
	}
	want := []string{"allowed.txt"}
	if !reflect.DeepEqual(touched, want) {
		t.Fatalf("touched paths = %#v, want %#v", touched, want)
	}
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func mustWrite(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

type writeScopeArtifactRunner struct {
	rows []map[string]any
}

func (r writeScopeArtifactRunner) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return runPrepareRowsFromMaps(r.rows), nil
}
