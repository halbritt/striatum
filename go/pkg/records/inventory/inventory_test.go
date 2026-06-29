package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestRunSortsEntriesByPath(t *testing.T) {
	repoRoot := t.TempDir()
	writeFile(t, repoRoot, "docs/audits/zeta.md", "z")
	writeFile(t, repoRoot, "docs/operator/progress/alpha.md", "alpha")
	writeFile(t, repoRoot, "docs/operator/progress/beta.md", "beta")

	manifest, err := Run(context.Background(), Options{
		RepoRoot: repoRoot,
		Roots: []string{
			"docs/audits",
			"docs/operator/progress",
		},
		SourceCommit: func(_ context.Context, _, relPath string) (string, error) {
			return "commit-for-" + relPath, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := entryPaths(manifest.Entries)
	want := []string{
		"docs/audits/zeta.md",
		"docs/operator/progress/alpha.md",
		"docs/operator/progress/beta.md",
	}
	if len(got) != len(want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths = %#v, want %#v", got, want)
		}
	}
	first := manifest.Entries[1]
	if first.Size != int64(len("alpha")) {
		t.Fatalf("size = %d, want %d", first.Size, len("alpha"))
	}
	if first.SHA256 != sha256Hex("alpha") {
		t.Fatalf("sha256 = %q, want %q", first.SHA256, sha256Hex("alpha"))
	}
	if first.SourceCommit != "commit-for-docs/operator/progress/alpha.md" {
		t.Fatalf("source_commit = %q", first.SourceCommit)
	}
}

func TestClassificationRulesAreConservative(t *testing.T) {
	tests := []struct {
		path           string
		recordClass    string
		classification string
	}{
		{"docs/operator/BRIEF.md", "operator_current_surface", ClassificationKeepInGit},
		{"docs/operator/recovery-decisions/FINAL_REVIEW_RECOVERY_DECISION_2026-06-16.md", "operator_decision", ClassificationKeepInGit},
		{"docs/dogfoods/rfc-0101-l2-conformance/workflow.json", "workflow_fixture", ClassificationKeepInGit},
		{"docs/dogfoods/rfc-0101-l2-conformance/README.md", "dogfood_index", ClassificationKeepInGit},
		{"docs/audits/STRIATUM_DEEP_ARCHITECTURE_REVIEW.md", "audit_record", ClassificationSafeToBlobIndex},
		{"docs/records/_frozen/requests/ORIGINAL_REQUEST.md", "frozen_historical_record", ClassificationSafeToBlobIndex},
		{"docs/operator/artifacts/rfc-0171/review/REVIEW.md", "historical_operator_artifact_doc", ClassificationSafeToBlobIndex},
		{"docs/operator/workflows/rfc-0171/prompts/review.md", "historical_operator_workflow_doc", ClassificationSafeToBlobIndex},
		{"docs/operator/plans/rfc-0078-remaining-work.md", "operator_work_plan", ClassificationSafeToBlobIndex},
		{"docs/dogfoods/rfc-0097-self-hosting/OPERATOR_REPORT.md", "operator_report", ClassificationSafeToBlobIndex},
		{"docs/operator/doctor-acknowledged-loss.json", "unknown", ClassificationManualReview},
		{"docs/operator/daemon-perf-analysis/instrument.sh", "unknown", ClassificationManualReview},
		{"docs/operator/daemon-perf-analysis/REPORT.md", "unknown", ClassificationManualReview},
	}

	for _, tt := range tests {
		recordClass, proposed, classification := Classify(tt.path)
		if recordClass != tt.recordClass || proposed != tt.classification || classification != tt.classification {
			t.Fatalf("Classify(%q) = (%q, %q, %q), want record_class=%q classification=%q",
				tt.path, recordClass, proposed, classification, tt.recordClass, tt.classification)
		}
	}
}

func TestRunRejectsRootsOutsideRepository(t *testing.T) {
	repoRoot := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "record.md", "outside")

	_, err := Run(context.Background(), Options{
		RepoRoot: repoRoot,
		Roots:    []string{outside},
		SourceCommit: func(context.Context, string, string) (string, error) {
			return "", nil
		},
	})
	if err == nil {
		t.Fatal("expected outside root to be rejected")
	}
}

func writeFile(t *testing.T, repoRoot, relPath, body string) {
	t.Helper()
	target := filepath.Join(repoRoot, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func entryPaths(entries []Entry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	return paths
}

func sha256Hex(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
