package reads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

type doctorArtifactAnchorRunner struct {
	doctorFakeRunner
	repoRoot        string
	artifactRows    []map[string]any
	artifactQueried bool
}

func (r *doctorArtifactAnchorRunner) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "FROM striatumd.artifacts a"):
		r.artifactQueried = true
		return dashboardAllRowsFromMaps(r.artifactRows), nil
	case strings.Contains(sql, "FROM striatumd.repositories"):
		if r.repoRoot == "" {
			return dashboardAllRowsFromMaps(nil), nil
		}
		return dashboardAllRowsFromMaps([]map[string]any{{"repo_root": r.repoRoot}}), nil
	case strings.Contains(sql, "COUNT(*) AS c"):
		return dashboardAllRowsFromMaps([]map[string]any{{"c": int64(0)}}), nil
	default:
		return dashboardAllRowsFromMaps(nil), nil
	}
}

func TestHandleDoctorSkipsArtifactAnchorIntegrityWhenBlobDisabled(t *testing.T) {
	previous := packageBlobClient
	packageBlobClient = nil
	t.Cleanup(func() { packageBlobClient = previous })

	runner := &doctorArtifactAnchorRunner{}
	result, err := HandleDoctor(context.Background(), runner, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_anchor", "verbose": true},
	})
	if err != nil {
		t.Fatalf("HandleDoctor: %v", err)
	}
	if runner.artifactQueried {
		t.Fatal("artifact-anchor query ran while blob storage was disabled")
	}
	block := result["artifact_anchor_integrity"].(map[string]any)
	if block["checked"] != false || block["skipped"] != "blob_not_configured" {
		t.Fatalf("artifact_anchor_integrity = %#v, want disabled skip", block)
	}
	if result["ok"] != true {
		t.Fatalf("doctor ok = %v, want true; problems=%#v", result["ok"], result["problems"])
	}
}

func TestDoctorArtifactAnchorIntegritySkipsWhenBucketNotOK(t *testing.T) {
	runner := &doctorArtifactAnchorRunner{artifactRows: []map[string]any{artifactAnchorRow("/tmp/repo", "art_skip", "run_skip", "job_skip", "main", "docs/a.md", testSHA256("a"))}}
	block, problems, records := doctorArtifactAnchorIntegrity(context.Background(), runner, "repo_anchor", map[string]any{
		"configured":    true,
		"reachable":     true,
		"bucket_status": "not_provisioned",
	})
	if runner.artifactQueried {
		t.Fatal("artifact-anchor query ran before blob bucket status was ok")
	}
	if block["checked"] != false || block["skipped"] != "blob_bucket_not_ok" {
		t.Fatalf("block = %#v, want bucket skip", block)
	}
	if len(problems) != 0 || len(records) != 0 {
		t.Fatalf("problems=%#v records=%#v, want clean skip", problems, records)
	}
}

func TestDoctorArtifactAnchorIntegrityAcceptsRunBranchMatch(t *testing.T) {
	repoRoot, runBranch, artifactPath, contentSHA := seedAnchoredArtifact(t, "run-branch-match\n")
	row := artifactAnchorRow(repoRoot, "art_match", "run_match", "job_match", runBranch, artifactPath, contentSHA)

	block, problems, records := doctorArtifactAnchorIntegrity(context.Background(), &doctorArtifactAnchorRunner{artifactRows: []map[string]any{row}}, "repo_anchor", healthyBlobBlock())
	if block["checked"] != true || len(problems) != 0 || len(records) != 0 {
		t.Fatalf("block=%#v problems=%#v records=%#v, want clean match", block, problems, records)
	}
}

func TestDoctorArtifactAnchorIntegrityDoesNotGitCheckBlobExhaustArtifact(t *testing.T) {
	row := artifactAnchorRow("/tmp/repo", "art_blob", "run_blob", "job_blob", "main", "docs/missing.md", testSHA256("blob body\n"))
	row["artifact_kind"] = "synthesis"
	row["placement"] = "blob_exhaust"

	block, problems, records := doctorArtifactAnchorIntegrity(context.Background(), &doctorArtifactAnchorRunner{artifactRows: []map[string]any{row}}, "repo_anchor", healthyBlobBlock())
	if block["git_anchor_count"] != 0 || block["blob_exhaust_count"] != 1 {
		t.Fatalf("block counts = %#v", block)
	}
	if !strings.Contains(strings.Join(problems, "\n"), "artifact_blob_metadata_missing.art_blob") {
		t.Fatalf("problems = %#v, want blob metadata problem", problems)
	}
	if len(records) != 1 || records[0]["check"] != artifactBlobMetadataMissing {
		t.Fatalf("records = %#v, want blob metadata record", records)
	}
	contextMap := records[0]["context"].(map[string]any)
	if contextMap["placement"] != "blob_exhaust" {
		t.Fatalf("record context = %#v, want placement", contextMap)
	}
}

func TestDoctorArtifactAnchorIntegrityGitChecksExplicitGitPublicationSynthesis(t *testing.T) {
	repoRoot, runBranch, artifactPath, contentSHA := seedAnchoredArtifact(t, "git synthesis\n")
	row := artifactAnchorRow(repoRoot, "art_git_synth", "run_git_synth", "job_git_synth", runBranch, artifactPath, contentSHA)
	row["artifact_kind"] = "synthesis"
	row["placement"] = "git_publication"

	block, problems, records := doctorArtifactAnchorIntegrity(context.Background(), &doctorArtifactAnchorRunner{artifactRows: []map[string]any{row}}, "repo_anchor", healthyBlobBlock())
	if block["git_anchor_count"] != 1 || block["blob_exhaust_count"] != 0 {
		t.Fatalf("block counts = %#v", block)
	}
	if len(problems) != 0 || len(records) != 0 {
		t.Fatalf("problems=%#v records=%#v, want clean git-published synthesis", problems, records)
	}
}

func TestDoctorArtifactAnchorIntegrityReportsRunBranchMismatch(t *testing.T) {
	repoRoot, runBranch, artifactPath, _ := seedAnchoredArtifact(t, "actual body\n")
	expectedSHA := testSHA256("expected body\n")
	row := artifactAnchorRow(repoRoot, "art_mismatch", "run_mismatch", "job_mismatch", runBranch, artifactPath, expectedSHA)

	_, problems, records := doctorArtifactAnchorIntegrity(context.Background(), &doctorArtifactAnchorRunner{artifactRows: []map[string]any{row}}, "repo_anchor", healthyBlobBlock())
	problemText := strings.Join(problems, "\n")
	if !strings.Contains(problemText, "artifact_anchor_hash_mismatch.art_mismatch") {
		t.Fatalf("problems = %#v, want hash mismatch", problems)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one record", records)
	}
	contextMap := records[0]["context"].(map[string]any)
	if contextMap["anchor_kind"] != worktreeAnchorRunBranch || contextMap["content_sha256"] != expectedSHA {
		t.Fatalf("record context = %#v, want run_branch mismatch with expected sha", contextMap)
	}
}

func TestDoctorArtifactAnchorIntegrityReportsMissingFile(t *testing.T) {
	repoRoot := t.TempDir()
	baseSHA := readsGitInit(t, repoRoot)
	runBranch := "wf/missing-artifact"
	readsGitRun(t, repoRoot, "branch", runBranch, baseSHA)
	row := artifactAnchorRow(repoRoot, "art_missing", "run_missing", "job_missing", runBranch, "docs/missing.md", testSHA256("missing body\n"))

	_, problems, records := doctorArtifactAnchorIntegrity(context.Background(), &doctorArtifactAnchorRunner{artifactRows: []map[string]any{row}}, "repo_anchor", healthyBlobBlock())
	if !strings.Contains(strings.Join(problems, "\n"), "artifact_anchor_missing_file.art_missing") {
		t.Fatalf("problems = %#v, want missing file", problems)
	}
	contextMap := records[0]["context"].(map[string]any)
	if contextMap["reason"] != "path_not_present_in_checked_anchors" {
		t.Fatalf("record context = %#v, want missing-file detail", contextMap)
	}
}

func TestDoctorArtifactAnchorIntegrityReportsJobPinMismatch(t *testing.T) {
	repoRoot, _, artifactPath, _ := seedAnchoredArtifact(t, "actual pinned body\n")
	runID := "run_pin"
	jobID := "job_pin"
	commit := readsGitRevParse(t, repoRoot, "HEAD")
	readsGitRun(t, repoRoot, "update-ref", "refs/striatum/"+runID+"/"+jobID+"/1", commit)
	row := artifactAnchorRow(repoRoot, "art_pin_mismatch", runID, jobID, "", artifactPath, testSHA256("expected pinned body\n"))

	_, problems, records := doctorArtifactAnchorIntegrity(context.Background(), &doctorArtifactAnchorRunner{artifactRows: []map[string]any{row}}, "repo_anchor", healthyBlobBlock())
	if !strings.Contains(strings.Join(problems, "\n"), "artifact_anchor_hash_mismatch.art_pin_mismatch") {
		t.Fatalf("problems = %#v, want job-pin hash mismatch", problems)
	}
	contextMap := records[0]["context"].(map[string]any)
	if contextMap["anchor_kind"] != worktreeAnchorJobPin {
		t.Fatalf("record context = %#v, want job_pin anchor", contextMap)
	}
}

func healthyBlobBlock() map[string]any {
	return map[string]any{
		"configured":    true,
		"reachable":     true,
		"bucket_status": "ok",
	}
}

func seedAnchoredArtifact(t *testing.T, body string) (string, string, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	repoRoot := t.TempDir()
	readsGitInit(t, repoRoot)
	artifactPath := "docs/artifact.md"
	if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, filepath.FromSlash(artifactPath)), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	readsGitRun(t, repoRoot, "add", artifactPath)
	readsGitRun(t, repoRoot, "commit", "-q", "-m", "artifact")
	runBranch := "wf/artifact-anchor"
	readsGitRun(t, repoRoot, "branch", runBranch, "HEAD")
	return repoRoot, runBranch, artifactPath, testSHA256(body)
}

func artifactAnchorRow(repoRoot, artifactID, runID, jobID, runBranch, repoPath, contentSHA string) map[string]any {
	return map[string]any{
		"repository_id":   "repo_anchor",
		"artifact_id":     artifactID,
		"run_id":          runID,
		"job_id":          jobID,
		"logical_name":    "artifact",
		"repo_path":       repoPath,
		"content_sha256":  contentSHA,
		"artifact_kind":   "handoff",
		"workflow_job_id": "draft",
		"attempt":         int64(1),
		"repo_root":       repoRoot,
		"branch_name":     runBranch,
		"base_branch":     runBranch,
	}
}

func testSHA256(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
