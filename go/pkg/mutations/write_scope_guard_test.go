package mutations

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/jackc/pgx/v5"
)

func TestWriteScopeViolationsRejectsOutsideAndForbiddenPaths(t *testing.T) {
	got := writeScopeViolations(
		[]string{"docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/design/codex/DESIGN.md", "go/pkg/mcp/capabilities.go", ".striatum/scratch/pid"},
		[]string{"docs/operator/workflows/rfc-0050-http-sse-mcp/artifacts/design/codex/"},
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

func TestParseGitPorcelainStatusZMarksUntrackedPaths(t *testing.T) {
	got := parseGitPorcelainStatusZ([]byte(" M go/edited.go\x00?? OPERATOR_REPORT.md\x00R  docs/new.md\x00docs/old.md\x00"))
	want := []gitPorcelainEntry{
		{Path: "OPERATOR_REPORT.md", Untracked: true},
		{Path: "docs/new.md", Untracked: false},
		{Path: "docs/old.md", Untracked: false},
		{Path: "go/edited.go", Untracked: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
}

// The four-quadrant matrix from RFC 0095 §6 / acceptance-criterion 7, run
// directly against the unified rule. baseline holds the claim-time dirty
// snapshot (path -> hash); current is the completion-time snapshot.
func TestWriteScopeViolationsSinceBaselineFourQuadrants(t *testing.T) {
	allowed := []string{"go/"}
	forbidden := []string{".striatum/"}
	baseline := map[string]string{
		// pre-existing operator state, dirty at claim, outside allowed_paths
		"OPERATOR_REPORT.md":   "report-v1",
		"go/in/baseline_in.go": "go-base",
	}
	current := []gitPathSnapshot{
		// Quadrant 1: in-scope file the attempt created -> ok.
		{Path: "go/new/created.go", Hash: "go-new"},
		// Quadrant 2: out-of-scope file the attempt created (not in baseline) -> violation.
		{Path: "config/secret.yaml", Hash: "secret"},
		// Quadrant 4a: out-of-scope untracked operator file mutated further
		// during the run (the #57 incremental OPERATOR_REPORT.md) -> ok.
		{Path: "OPERATOR_REPORT.md", Hash: "report-v2", Untracked: true},
		// In-scope baseline file the attempt mutated -> ok (in allowed_paths).
		{Path: "go/in/baseline_in.go", Hash: "go-changed"},
	}
	// Quadrant 3 (baseline-dirty -> clean out-of-scope) is represented by a
	// baseline path that is ABSENT from `current`; it never enters the loop.
	got := writeScopeViolationsSinceBaseline(current, baseline, allowed, forbidden, nil)
	want := []string{"config/secret.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("violations = %#v, want %#v", got, want)
	}
}

// Regression for #57: an out-of-scope file that was dirty at claim and is then
// stashed/cleaned/deleted (dirty->clean) must not raise a violation, nor must
// an out-of-scope operator file the attempt never touched.
func TestWriteScopeViolationsSinceBaselineDirtyToCleanAndUntouchedAreNotViolations(t *testing.T) {
	allowed := []string{"go/"}
	baseline := map[string]string{
		"OPERATOR_REPORT.md": "h-report",  // pre-existing operator file
		"stashed.txt":        "h-stashed", // dirty at claim, cleaned before complete
	}
	current := []gitPathSnapshot{
		// dirty->clean: stashed.txt is absent from current entirely.
		// OPERATOR_REPORT.md unchanged (same hash) and untracked -> untouched operator state.
		{Path: "OPERATOR_REPORT.md", Hash: "h-report", Untracked: true},
		{Path: "go/work.go", Hash: "h-work"}, // the attempt's in-scope work
	}
	got := writeScopeViolationsSinceBaseline(current, baseline, allowed, nil, nil)
	if len(got) != 0 {
		t.Fatalf("expected no violations, got %#v", got)
	}
}

// Rule 2: a tracked (committed) file that was dirty at claim and which the
// attempt mutates further, outside allowed_paths, IS a violation.
func TestWriteScopeViolationsSinceBaselineTrackedMutationOutOfScopeViolates(t *testing.T) {
	allowed := []string{"go/"}
	baseline := map[string]string{"docs/tracked.md": "h1"}
	current := []gitPathSnapshot{
		{Path: "docs/tracked.md", Hash: "h2", Untracked: false},
	}
	got := writeScopeViolationsSinceBaseline(current, baseline, allowed, nil, nil)
	want := []string{"docs/tracked.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("violations = %#v, want %#v", got, want)
	}
}

// Forbidden paths are absolute: even a pre-existing untracked baseline path
// inside a forbidden prefix is a violation.
func TestWriteScopeViolationsSinceBaselineForbiddenIsAbsolute(t *testing.T) {
	baseline := map[string]string{".striatum/scratch/pid": "h"}
	current := []gitPathSnapshot{
		{Path: ".striatum/scratch/pid", Hash: "h", Untracked: true},
	}
	got := writeScopeViolationsSinceBaseline(current, baseline, []string{"."}, []string{".striatum/"}, nil)
	want := []string{".striatum/scratch/pid"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("violations = %#v, want %#v", got, want)
	}
}

// End-to-end through gitChangedPathSnapshots: a real worktree where an
// out-of-scope file is dirty at claim, then deleted (dirty->clean), while the
// attempt edits its in-scope file. No violation should result.
func TestWriteScopeViolationsSinceBaselineLiveStashedOrRestored(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "Test User")
	mustWrite(t, filepath.Join(repo, "go", "allowed.go"), "base\n")
	runGit(t, repo, "add", "go/allowed.go")
	runGit(t, repo, "commit", "-m", "base")

	// outside-stashed.txt is dirty at baseline but removed (restored to clean) later.
	mustWrite(t, filepath.Join(repo, "outside-stashed.txt"), "will be stashed\n")
	baselineSnap, err := gitChangedPathSnapshots(context.Background(), repo)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	baseline := map[string]string{}
	for _, item := range baselineSnap {
		baseline[item.Path] = item.Hash
	}

	if err := os.Remove(filepath.Join(repo, "outside-stashed.txt")); err != nil {
		t.Fatalf("remove outside-stashed: %v", err)
	}
	mustWrite(t, filepath.Join(repo, "go", "allowed.go"), "changed\n")

	current, err := gitChangedPathSnapshots(context.Background(), repo)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	got := writeScopeViolationsSinceBaseline(current, baseline, []string{"go/"}, nil, nil)
	if len(got) != 0 {
		t.Fatalf("stashed/restored files should not trigger violation, got %#v", got)
	}
}

// End-to-end: an out-of-scope file CREATED during the attempt (absent from the
// claim-time baseline) is a violation.
func TestWriteScopeViolationsSinceBaselineLiveAttemptCreatedOutOfScopeViolates(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "Test User")
	mustWrite(t, filepath.Join(repo, "go", "allowed.go"), "base\n")
	runGit(t, repo, "add", "go/allowed.go")
	runGit(t, repo, "commit", "-m", "base")

	// Clean tree at claim time -> empty baseline.
	baseline := map[string]string{}
	// The attempt edits its in-scope file AND creates an out-of-scope file.
	mustWrite(t, filepath.Join(repo, "go", "allowed.go"), "changed\n")
	mustWrite(t, filepath.Join(repo, "outside-new.txt"), "created by attempt\n")

	current, err := gitChangedPathSnapshots(context.Background(), repo)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	got := writeScopeViolationsSinceBaseline(current, baseline, []string{"go/"}, nil, nil)
	want := []string{"outside-new.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("violations = %#v, want %#v", got, want)
	}
}

// Regression for #93: a dirty path that is BOTH inside forbidden_paths AND a
// sibling lane's already-published run artifact (matching digest) must not be
// attributed to this gate job — otherwise every multi-lane gate (synthesis
// forbids artifacts/design/, implement forbids artifacts/review/, ...)
// deadlocks at work.complete in a shared worktree. A forbidden-path file the
// attempt actually authored (no matching sibling-published digest) still
// violates.
func TestWriteScopeViolationsSinceBaselineSiblingPublishedForbiddenIsNotViolation(t *testing.T) {
	allowed := []string{"artifacts/synthesis/"}
	forbidden := []string{"artifacts/design/"}
	baseline := map[string]string{}
	current := []gitPathSnapshot{
		// Sibling design lane's published artifact, inside this job's
		// forbidden_paths but provably the sibling's write (matching digest).
		{Path: "artifacts/design/codex/DESIGN.md", Hash: "sibling-digest"},
		// A forbidden-path file the attempt actually authored (no matching
		// published digest) -> still a violation.
		{Path: "artifacts/design/self/SECRET.md", Hash: "self-authored"},
		// The attempt's own in-scope work.
		{Path: "artifacts/synthesis/SYNTHESIS.md", Hash: "synth"},
	}
	ignored := map[string]bool{"artifacts/design/codex/DESIGN.md": true}
	got := writeScopeViolationsSinceBaseline(current, baseline, allowed, forbidden, ignored)
	want := []string{"artifacts/design/self/SECRET.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("violations = %#v, want %#v", got, want)
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

// #102: a path that is the declared expected artifact of a LIVE sibling lane
// (active lease) is ignored for this lane's work.complete even before the
// sibling publishes — the pre-publish half of the digest-based leniency, for
// same-stage parallel lanes with disjoint scopes in a shared worktree.
func TestPublishedRunArtifactIgnoredPathsHonorsLiveSiblingExpectedArtifact(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_sibling_expected"
	runID := "run_" + repoID
	siblingPath := "docs/reviews/quiz/proposals/ENTITY_DIAGNOSTICS_WORKBENCH.md"
	selfPath := "docs/reviews/quiz/proposals/LOCAL_LLM_REWRITE.md"

	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{
		"workflow_id": "wf",
		"roles":       map[string]any{"proposer": map[string]any{}},
		"lanes":       map[string]any{"agy": map[string]any{}},
	})
	sib := "sess_sib_" + repoID
	intgSeedSession(t, ctx, runner, repoID, runID, sib, "proposer", "agy", []string{"write"}, "active")

	expectedArg, err := db.JSONBArg(runner, []any{map[string]any{
		"path": siblingPath, "logical_name": "entity_diagnostics", "kind": "synthesis", "required": true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, idempotency_key, expected_artifacts_json, created_at, started_at
		) VALUES ($1,'job_sib',$2,'proposal_entity_diagnostics',1,'running','proposer','P','build',
		          'idem_sib_'||$1,$3::jsonb,NOW(),NOW())`,
		repoID, runID, expectedArg); err != nil {
		t.Fatalf("insert sibling job: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.leases (
		  repository_id, lease_id, run_id, resource_type, resource_id,
		  owner_session_id, state, acquired_at, expires_at
		) VALUES ($1,'lease_sib_'||$1,$2,'job','job_sib',$3,'active',NOW(),NOW()+INTERVAL '1 hour')`,
		repoID, runID, sib); err != nil {
		t.Fatalf("insert sibling active lease: %v", err)
	}

	ignored, err := publishedRunArtifactIgnoredPaths(
		ctx, runner, repoID, t.TempDir(),
		map[string]any{"run_id": runID, "job_id": "job_self"},
		[]string{siblingPath, selfPath},
	)
	if err != nil {
		t.Fatalf("ignored paths: %v", err)
	}
	if !ignored[siblingPath] {
		t.Fatalf("#102: a live sibling's expected artifact path must be ignored, got %#v", ignored)
	}
	if ignored[selfPath] {
		t.Fatalf("this lane's own path must not be ignored, got %#v", ignored)
	}
}

// #109 / #257: the write_scope violation message names the offending path(s)
// and explains the frozen-attempt recovery path instead of telling a running
// attempt to widen its own historical scope.
func TestWriteScopeViolationMessageGuidesIndexFileScope(t *testing.T) {
	msg := writeScopeViolationMessage(
		[]string{"docs/rfcs/README.md"},
		[]string{"docs/rfcs/0070-x.md", "CHANGELOG.md"},
		[]string{".striatum/"},
	)
	for _, want := range []string{
		"docs/rfcs/README.md", // offending path named
		"frozen write_scope",  // source of truth named
		"recovery resume",     // audited recovery path
		"do not mutate",       // no silent historical widening
		"allowed_paths=",      // scope echoed
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q; got: %s", want, msg)
		}
	}
}
