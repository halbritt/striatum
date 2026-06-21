package reads

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/verifier"
)

// --- slice 1: builtin self-pin drift -------------------------------------

func writeBinary(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil { //nolint:gosec // test fixture binary
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestVerifierSelfpinDriftCleanWhenBytesMatch(t *testing.T) {
	dir := t.TempDir()
	exe := writeBinary(t, dir, "striatumd", "DAEMON-BUILD-A")

	prevRunning := runningDaemonExePath
	prevPath := daemonExecutablePath
	t.Cleanup(func() { runningDaemonExePath = prevRunning; daemonExecutablePath = prevPath })
	runningDaemonExePath = exe
	daemonExecutablePath = func() (string, error) { return exe, nil }

	block, warnings := verifierSelfpinDriftBlock()
	if block["drift"] != false {
		t.Fatalf("expected drift=false, block=%#v", block)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings when bytes match, got: %v", warnings)
	}
}

func TestVerifierSelfpinDriftWarnsOnMakeInstallWithoutRestart(t *testing.T) {
	dir := t.TempDir()
	// The running image (a snapshot) and the on-disk binary differ: `make install`
	// replaced the on-disk file while the daemon kept its old in-memory image.
	running := writeBinary(t, dir, "striatumd.running", "OLD-IN-MEMORY-IMAGE")
	onDisk := writeBinary(t, dir, "striatumd", "NEW-DEPLOYED-BUILD")

	prevRunning := runningDaemonExePath
	prevPath := daemonExecutablePath
	t.Cleanup(func() { runningDaemonExePath = prevRunning; daemonExecutablePath = prevPath })
	runningDaemonExePath = running
	daemonExecutablePath = func() (string, error) { return onDisk, nil }

	block, warnings := verifierSelfpinDriftBlock()
	if block["drift"] != true {
		t.Fatalf("expected drift=true, block=%#v", block)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "verifier_selfpin_drift") {
		t.Fatalf("expected verifier_selfpin_drift warning, got: %v", warnings)
	}
}

func TestVerifierSelfpinDriftWarnsWhenOnDiskBinaryRemoved(t *testing.T) {
	dir := t.TempDir()
	running := writeBinary(t, dir, "striatumd.running", "RUNNING-IMAGE")
	missing := filepath.Join(dir, "striatumd-gone")

	prevRunning := runningDaemonExePath
	prevPath := daemonExecutablePath
	t.Cleanup(func() { runningDaemonExePath = prevRunning; daemonExecutablePath = prevPath })
	runningDaemonExePath = running
	daemonExecutablePath = func() (string, error) { return missing, nil }

	block, warnings := verifierSelfpinDriftBlock()
	if block["drift"] != true {
		t.Fatalf("expected drift=true when on-disk binary removed, block=%#v", block)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "verifier_selfpin_drift") {
		t.Fatalf("expected verifier_selfpin_drift warning, got: %v", warnings)
	}
}

func TestVerifierSelfpinDriftQuietWhenRunningImageUnreadable(t *testing.T) {
	prevRunning := runningDaemonExePath
	t.Cleanup(func() { runningDaemonExePath = prevRunning })
	runningDaemonExePath = filepath.Join(t.TempDir(), "no-such-running-image")

	block, warnings := verifierSelfpinDriftBlock()
	if block["comparable"] != false {
		t.Fatalf("expected comparable=false when running image unreadable, block=%#v", block)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings when not comparable, got: %v", warnings)
	}
}

// --- slice 2: intent ⋈ pins drift ----------------------------------------

// writeIntent writes a committed, hashless intent file that LoadIntent accepts
// (each external check requires a negative_control with mutation_of).
func writeIntent(t *testing.T, dir string, checks []verifier.IntentEntry) string {
	t.Helper()
	verDir := filepath.Join(dir, "verification")
	if err := os.MkdirAll(verDir, 0o755); err != nil { //nolint:gosec // test fixture dir
		t.Fatalf("mkdir verification: %v", err)
	}
	file := verifier.AllowlistIntentFile{SchemaVersion: verifier.AllowlistIntentSchemaVersion, Checks: checks}
	body, _ := json.MarshalIndent(file, "", "  ")
	p := filepath.Join(verDir, "allowlist.intent.json")
	if err := os.WriteFile(p, body, 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write intent: %v", err)
	}
	return p
}

func writePins(t *testing.T, intentPath string, entries []verifier.AllowlistEntry) string {
	t.Helper()
	fp := verifier.HostFingerprint()
	p := filepath.Join(filepath.Dir(intentPath), verifier.PinsFileName(fp))
	file := verifier.AllowlistFile{SchemaVersion: verifier.AllowlistSchemaVersion, Checks: entries}
	body, _ := json.MarshalIndent(file, "", "  ")
	if err := os.WriteFile(p, body, 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write pins: %v", err)
	}
	return p
}

func shaOfString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestVerifierPinDriftQuietWhenNoIntent(t *testing.T) {
	block, warnings := verifierPinDriftBlock(t.TempDir())
	if block["checked"] != true || block["intent_present"] != false {
		t.Fatalf("expected checked=true intent_present=false, block=%#v", block)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings with no intent, got: %v", warnings)
	}
}

func TestVerifierPinDriftSkipsWithoutRepoRoot(t *testing.T) {
	block, warnings := verifierPinDriftBlock("")
	if block["checked"] != false {
		t.Fatalf("expected checked=false, block=%#v", block)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}

func TestVerifierPinDriftReportsNamedButUnpinned(t *testing.T) {
	dir := t.TempDir()
	tool := writeBinary(t, dir, "mytool", "TOOL-BYTES")
	writeIntent(t, dir, []verifier.IntentEntry{{
		ID:         "lint",
		Argv:       []string{tool, "lint"},
		BacksClaim: "claim-lint",
		NegativeControl: &verifier.NegativeControl{
			Argv:       []string{tool, "lint", "--bad"},
			MutationOf: "fixtures/good",
		},
	}})
	// No pins file written.
	block, warnings := verifierPinDriftBlock(dir)
	if block["named_but_unpinned_count"] != 1 {
		t.Fatalf("expected 1 named-but-unpinned, block=%#v", block)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "verifier_check_unpinned.lint") {
		t.Fatalf("expected unpinned warning, got: %v", warnings)
	}
}

func TestVerifierPinDriftReportsArgvDrift(t *testing.T) {
	dir := t.TempDir()
	tool := writeBinary(t, dir, "mytool", "TOOL-BYTES")
	intentPath := writeIntent(t, dir, []verifier.IntentEntry{{
		ID:         "lint",
		Argv:       []string{tool, "lint", "--strict"},
		BacksClaim: "claim-lint",
		NegativeControl: &verifier.NegativeControl{
			Argv:       []string{tool, "lint", "--bad"},
			MutationOf: "fixtures/good",
		},
	}})
	// Pin has a DIFFERENT argv than the intent now declares.
	writePins(t, intentPath, []verifier.AllowlistEntry{{
		ID:           "lint",
		Argv:         []string{tool, "lint"},
		BinarySHA256: shaOfString("TOOL-BYTES"),
		PassWhen:     "exit_zero",
	}})
	block, warnings := verifierPinDriftBlock(dir)
	if block["argv_drifted_count"] != 1 {
		t.Fatalf("expected 1 argv-drift, block=%#v", block)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "verifier_pin_drifted.lint") {
		t.Fatalf("expected pin-drift warning, got: %v", warnings)
	}
}

func TestVerifierPinDriftReportsByteDrift(t *testing.T) {
	dir := t.TempDir()
	tool := writeBinary(t, dir, "mytool", "NEW-TOOL-BYTES")
	intentPath := writeIntent(t, dir, []verifier.IntentEntry{{
		ID:         "lint",
		Argv:       []string{tool, "lint"},
		BacksClaim: "claim-lint",
		NegativeControl: &verifier.NegativeControl{
			Argv:       []string{tool, "lint", "--bad"},
			MutationOf: "fixtures/good",
		},
	}})
	// Pin records an OLD sha; the binary on disk now hashes differently.
	writePins(t, intentPath, []verifier.AllowlistEntry{{
		ID:           "lint",
		Argv:         []string{tool, "lint"},
		BinarySHA256: shaOfString("OLD-TOOL-BYTES"),
		PassWhen:     "exit_zero",
	}})
	block, warnings := verifierPinDriftBlock(dir)
	if block["pin_bytes_drifted_count"] != 1 {
		t.Fatalf("expected 1 byte-drift, block=%#v", block)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "verifier_pin_drifted.lint") {
		t.Fatalf("expected pin-drift warning, got: %v", warnings)
	}
}

func TestVerifierPinDriftCleanWhenPinned(t *testing.T) {
	dir := t.TempDir()
	tool := writeBinary(t, dir, "mytool", "TOOL-BYTES")
	intentPath := writeIntent(t, dir, []verifier.IntentEntry{{
		ID:         "lint",
		Argv:       []string{tool, "lint"},
		BacksClaim: "claim-lint",
		NegativeControl: &verifier.NegativeControl{
			Argv:       []string{tool, "lint", "--bad"},
			MutationOf: "fixtures/good",
		},
	}})
	writePins(t, intentPath, []verifier.AllowlistEntry{{
		ID:           "lint",
		Argv:         []string{tool, "lint"},
		BinarySHA256: shaOfString("TOOL-BYTES"),
		PassWhen:     "exit_zero",
	}})
	block, warnings := verifierPinDriftBlock(dir)
	if block["named_but_unpinned_count"] != 0 || block["argv_drifted_count"] != 0 || block["pin_bytes_drifted_count"] != 0 {
		t.Fatalf("expected a clean pin, block=%#v", block)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for a clean pin, got: %v", warnings)
	}
}

func TestVerifierPinDriftSurfacesInvalidIntent(t *testing.T) {
	dir := t.TempDir()
	verDir := filepath.Join(dir, "verification")
	if err := os.MkdirAll(verDir, 0o755); err != nil { //nolint:gosec // test fixture dir
		t.Fatalf("mkdir: %v", err)
	}
	// A binary_sha256 in the hashless intent layer is rejected by LoadIntent.
	bad := `{"schema_version":"` + verifier.AllowlistIntentSchemaVersion + `","checks":[{"id":"x","binary_sha256":"abc","argv":["go"],"backs_claim":"c"}]}`
	if err := os.WriteFile(filepath.Join(verDir, "allowlist.intent.json"), []byte(bad), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write bad intent: %v", err)
	}
	block, warnings := verifierPinDriftBlock(dir)
	if block["intent_error"] == nil {
		t.Fatalf("expected intent_error set, block=%#v", block)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "verifier_intent_invalid") {
		t.Fatalf("expected verifier_intent_invalid warning, got: %v", warnings)
	}
}
