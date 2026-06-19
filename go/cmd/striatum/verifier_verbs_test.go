package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// writeIntent seeds a hashless intent naming a real host binary, so pin can
// observe its sha deterministically.
func writeIntent(t *testing.T, dir, bin string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "verification"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"schema_version":"striatum.verifier_allowlist_intent.v1","checks":[` +
		`{"id":"probe","argv":["` + bin + `"],"backs_claim":"present",` +
		`"negative_control":{"argv":["` + bin + `","--definitely-not-a-flag"]}}]}`
	if err := os.WriteFile(filepath.Join(dir, "verification", "allowlist.intent.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func realBin(t *testing.T) string {
	t.Helper()
	for _, p := range []string{"/bin/true", "/usr/bin/true", "/bin/echo", "/usr/bin/git"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("no real binary to pin")
	return ""
}

// TestVerifierPinThenAttest is the happy-path: pin observes the sha (no hand-typed
// hash), attest blesses it; the pins + attest sidecars both land on disk.
func TestVerifierPinThenAttest(t *testing.T) {
	dir := t.TempDir()
	writeIntent(t, dir, realBin(t))
	var out, errb bytes.Buffer

	if code := runVerifierPin([]string{"--host-here"}, &out, &errb, dir); code != 0 {
		t.Fatalf("pin failed: code=%d err=%s", code, errb.String())
	}
	pins, _ := filepath.Glob(filepath.Join(dir, "verification", "allowlist.pins.*.json"))
	if len(pins) != 1 {
		t.Fatalf("pin must write exactly one per-host pins file, got %v", pins)
	}

	// attest must NOT be refused from a clean (operator) environment.
	t.Setenv("STRIATUM_SESSION_ID", "")
	t.Setenv("STRIATUM_LANE_ID", "")
	out.Reset()
	errb.Reset()
	if code := runVerifierAttest([]string{"--all"}, &out, &errb, dir); code != 0 {
		t.Fatalf("attest failed: code=%d err=%s", code, errb.String())
	}
	att, _ := filepath.Glob(filepath.Join(dir, "verification", "allowlist.pins.*.attest.json"))
	if len(att) != 1 {
		t.Fatalf("attest must write the sidecar, got %v", att)
	}
}

// TestVerifierAttestRefusedInLane is the load-bearing trust boundary: attest is
// refused inside a supervised lane (STRIATUM_SESSION_ID / STRIATUM_LANE_ID set), so
// the verified lane can never bless its own pins.
func TestVerifierAttestRefusedInLane(t *testing.T) {
	dir := t.TempDir()
	writeIntent(t, dir, realBin(t))
	var out, errb bytes.Buffer
	_ = runVerifierPin([]string{"--host-here"}, &out, &errb, dir)

	for _, env := range []string{"STRIATUM_SESSION_ID", "STRIATUM_LANE_ID"} {
		out.Reset()
		errb.Reset()
		t.Setenv("STRIATUM_SESSION_ID", "")
		t.Setenv("STRIATUM_LANE_ID", "")
		t.Setenv(env, "set-by-supervisor")
		code := runVerifierAttest([]string{"--all"}, &out, &errb, dir)
		if code == 0 {
			t.Fatalf("attest must REFUSE when %s is set (a lane context); got code 0", env)
		}
		if !bytes.Contains(errb.Bytes(), []byte("OPERATOR")) {
			t.Fatalf("refusal must name the operator-token requirement, got: %s", errb.String())
		}
	}
}

// TestVerifierPinRequiresHostHere — there is no hand-typed-sha form; pin observes.
func TestVerifierPinRequiresHostHere(t *testing.T) {
	dir := t.TempDir()
	writeIntent(t, dir, realBin(t))
	var out, errb bytes.Buffer
	if code := runVerifierPin([]string{}, &out, &errb, dir); code == 0 {
		t.Fatal("pin without --host-here must be refused")
	}
}
