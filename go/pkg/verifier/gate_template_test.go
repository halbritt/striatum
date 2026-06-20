package verifier

import (
	"os"
	"path/filepath"
	"testing"
)

func seedIntent(t *testing.T, repoRoot string, external bool) {
	t.Helper()
	dir := filepath.Join(repoRoot, "verification")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"schema_version":"striatum.verifier_allowlist_intent.v1","checks":[` +
		`{"id":"ext","argv":["mypy","src"],"backs_claim":"types","negative_control":{"argv":["mypy","bad.py"],"mutation_of":"f"}}]}`
	if !external {
		body = `{"schema_version":"striatum.verifier_allowlist_intent.v1","checks":[` +
			`{"id":"only","argv":["a"],"backs_claim":"x","negative_control":{"argv":["a","bad"],"mutation_of":"f"}}]}`
	}
	if err := os.WriteFile(filepath.Join(dir, "allowlist.intent.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func verifyWorkflow(status string) map[string]any {
	wf := map[string]any{
		"jobs": []any{
			map[string]any{"id": "v", "type": "verify"},
		},
	}
	if status != "" {
		wf["allowlist_status"] = status
	}
	return wf
}

// TestEvaluateUnfilledBlocksAndClearsOnPin is the Pillar 3 UNFILLED hard-block: an
// unpinned external check blocks; writing the pin clears the block WITHOUT
// regenerating the workflow (the live files override the static hint).
func TestEvaluateUnfilledBlocksAndClearsOnPin(t *testing.T) {
	repo := t.TempDir()
	seedIntent(t, repo, true)
	wf := verifyWorkflow(AllowlistStatusUnfilled)

	tb := EvaluateAllowlistTemplate(repo, wf)
	if tb == nil {
		t.Fatal("an unpinned external check must hard-block run start / validate")
	}
	if len(tb.UnpinnedEntries) != 1 || tb.UnpinnedEntries[0] != "ext" {
		t.Fatalf("block must name the unpinned entry, got %+v", tb.UnpinnedEntries)
	}
	if tb.FixCommand == "" || tb.Reason != "verification_gate_unfilled" {
		t.Fatalf("block must carry the fix command + stable reason, got %+v", tb)
	}

	// Operator pins the bytes → the block clears with no regeneration.
	pins := AllowlistFile{SchemaVersion: AllowlistSchemaVersion, Checks: []AllowlistEntry{
		{ID: "ext", Argv: []string{"mypy", "src"}, BinarySHA256: "abc123"},
	}}
	writePins(t, repo, pins)
	if tb := EvaluateAllowlistTemplate(repo, wf); tb != nil {
		t.Fatalf("a pinned gate must NOT block; got %+v", tb)
	}
}

// TestEvaluateBuiltinsOnlyNeverBlocks — the runnable default (a verify job, no
// intent/allowlist hint) is never blocked: builtins need no pins.
func TestEvaluateBuiltinsOnlyNeverBlocks(t *testing.T) {
	repo := t.TempDir()
	if tb := EvaluateAllowlistTemplate(repo, verifyWorkflow("")); tb != nil {
		t.Fatalf("a builtins-only verify workflow must be runnable, got %+v", tb)
	}
	// A non-verification workflow is untouched entirely.
	if tb := EvaluateAllowlistTemplate(repo, map[string]any{"jobs": []any{map[string]any{"type": "draft"}}}); tb != nil {
		t.Fatalf("a non-verification workflow must never block, got %+v", tb)
	}
}

// TestEvaluateFilledStatusPasses — an explicit FILLED hint with no declared intent
// passes (the gate has no external checks to pin).
func TestEvaluateFilledStatusPasses(t *testing.T) {
	repo := t.TempDir()
	if tb := EvaluateAllowlistTemplate(repo, verifyWorkflow(AllowlistStatusFilled)); tb != nil {
		t.Fatalf("a FILLED gate must not block, got %+v", tb)
	}
}

func writePins(t *testing.T, repoRoot string, f AllowlistFile) {
	t.Helper()
	data := `{"schema_version":"` + f.SchemaVersion + `","checks":[`
	for i, c := range f.Checks {
		if i > 0 {
			data += ","
		}
		data += `{"id":"` + c.ID + `","argv":["` + c.Argv[0] + `"],"binary_sha256":"` + c.BinarySHA256 + `"}`
	}
	data += `]}`
	path := filepath.Join(repoRoot, "verification", PinsFileName(HostFingerprint()))
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
