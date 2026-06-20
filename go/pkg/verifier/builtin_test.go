package verifier

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
)

// TestCheckRunEnvGoBuiltinPrependsGoBinDir is the regression guard for the sandbox
// PATH fix: a go builtin must be able to resolve `go` even when the toolchain is not
// installed under a system bin dir (hosts with go in ~/.local/go/bin previously
// failed EVERY go builtin with a spurious "go: not found" exit 1). The host go's own
// directory is prepended to the otherwise-fixed sandbox PATH; a non-go check keeps
// only the bare system PATH (the hermetic envelope is not widened for it).
func TestCheckRunEnvGoBuiltinPrependsGoBinDir(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not on PATH; nothing to resolve")
	}
	goBin := filepath.Dir(goPath)

	rex, err := builtinResolvedExec("builtin:go-test")
	if err != nil {
		t.Fatalf("builtinResolvedExec: %v", err)
	}
	path := envValue(t, checkRunEnv(rex, "/some/cwd", t.TempDir()), "PATH")
	if !strings.HasPrefix(path, goBin+":") {
		t.Fatalf("go builtin PATH must start with the host go bin dir %q; got %q", goBin, path)
	}
	for _, want := range []string{"/usr/local/bin", "/usr/bin", "/bin"} {
		if !strings.Contains(path, want) {
			t.Fatalf("go builtin PATH must retain the fixed system dir %q; got %q", want, path)
		}
	}

	nonGo := ResolvedExec{CheckID: "x"} // not a builtin:go-* check
	if got := envValue(t, checkRunEnv(nonGo, "/some/cwd", t.TempDir()), "PATH"); got != "/usr/local/bin:/usr/bin:/bin" {
		t.Fatalf("non-go check PATH must be the fixed system PATH; got %q", got)
	}
}

func envValue(t *testing.T, env []string, key string) string {
	t.Helper()
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix)
		}
	}
	t.Fatalf("env %q not set in %v", key, env)
	return ""
}

// TestBuiltinGoChecksRunOnValidModule is the end-to-end regression for the live
// builtin path: a trivially-VALID Go module must actually PASS every go builtin
// (and the anchor check), capped at ASSERTED. This is the test that catches the
// real-execution bugs unit tests missed — the tool resolving off the fixed sandbox
// PATH, GOCACHE/HOME landing on the read-only cwd, and `go build` writing its
// binary to the read-only cwd. It runs against whatever sandbox posture the host
// resolves (strict bwrap here, degraded none in CI); the pass + ASSERTED-cap
// invariant holds under both, since a builtin can never reach verified_eligible.
func TestBuiltinGoChecksRunOnValidModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not installed")
	}
	work := t.TempDir()
	mustWrite(t, filepath.Join(work, "go.mod"), "module striatumverifiertest\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(work, "main.go"), "package main\n\nfunc main() { _ = 1 }\n")
	scratch := t.TempDir()

	for _, id := range []string{"builtin:go-vet", "builtin:go-build", "builtin:go-test"} {
		res, err := ExecuteCheck(context.Background(), nil, RunRequest{CheckID: id, Cwd: work, ScratchDir: scratch})
		if err != nil {
			t.Fatalf("%s: ExecuteCheck: %v", id, err)
		}
		// The cardinal builtin invariant — never above ASSERTED.
		if res.Classification == classVerifiedEligible {
			t.Fatalf("%s: a builtin must never be verified_eligible: %+v", id, res)
		}
		if res.Receipt.BuiltinID != id {
			t.Fatalf("%s: receipt must seal builtin_id, got %q", id, res.Receipt.BuiltinID)
		}
		if !res.Receipt.Posture.Strict {
			// Degraded envelope — e.g. the GitHub runner, where systemd-run/unshare
			// is on PATH but cannot actually launch the check (no user systemd
			// manager / restricted userns), so a non-strict posture did not produce a
			// clean run. The builtin cap is verified above; the real-execution
			// regression can only be asserted on a host whose sandbox actually runs
			// the check (strict bwrap). Skip rather than false-fail (f484996c precedent).
			t.Logf("%s: non-strict posture %q (exit=%d) — real-run assertion skipped (degraded env)",
				id, res.Receipt.Posture.Mechanism, res.Receipt.ExitCode)
			continue
		}
		// Strict host: a trivially-valid module MUST pass — the regression for the
		// tool-PATH / GOCACHE / go-build-output bugs — capped at ASSERTED.
		if res.Receipt.NegativeControlVoid {
			t.Fatalf("%s: a real check must not void on its negative control: %+v", id, res)
		}
		if !res.Passed || res.Receipt.ExitCode != 0 {
			t.Fatalf("%s: a valid module must pass under a strict sandbox (exit=%d class=%s); regression in the sandbox exec env",
				id, res.Receipt.ExitCode, res.Classification)
		}
		if res.Classification != classAsserted {
			t.Fatalf("%s: a passing builtin must classify asserted, got %q", id, res.Classification)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestGoModuleDirLocatesNestedModule is the #515 unit guard: the go builtins must
// run `go ./...` in the directory that holds go.mod. A module at the worktree root
// returns cwd (unchanged); a SINGLE nested module (striatum's `go/`) returns that
// subdir; an absent or AMBIGUOUS module falls back to cwd (fail-legibly, never guess).
func TestGoModuleDirLocatesNestedModule(t *testing.T) {
	const modfile = "module x\n\ngo 1.21\n"

	atRoot := t.TempDir()
	mustWrite(t, filepath.Join(atRoot, "go.mod"), modfile)
	if got := goModuleDir(atRoot); got != atRoot {
		t.Fatalf("module at root must return cwd: got %q want %q", got, atRoot)
	}

	nested := t.TempDir()
	mod := filepath.Join(nested, "go")
	mustWrite(t, filepath.Join(mod, "go.mod"), modfile)
	if got := goModuleDir(nested); got != mod {
		t.Fatalf("single nested module must be located: got %q want %q", got, mod)
	}

	none := t.TempDir()
	mustWrite(t, filepath.Join(none, "README.md"), "no module here\n")
	if got := goModuleDir(none); got != none {
		t.Fatalf("no module must fall back to cwd: got %q want %q", got, none)
	}

	ambiguous := t.TempDir()
	mustWrite(t, filepath.Join(ambiguous, "a", "go.mod"), modfile)
	mustWrite(t, filepath.Join(ambiguous, "b", "go.mod"), modfile)
	if got := goModuleDir(ambiguous); got != ambiguous {
		t.Fatalf("ambiguous (two modules) must fall back to cwd, not guess: got %q want %q", got, ambiguous)
	}

	// A go.mod buried under testdata/ is a fixture, not the repo's module — it must be
	// skipped so the real nested module is found unambiguously.
	withFixture := t.TempDir()
	mustWrite(t, filepath.Join(withFixture, "testdata", "fix", "go.mod"), modfile)
	real := filepath.Join(withFixture, "src")
	mustWrite(t, filepath.Join(real, "go.mod"), modfile)
	if got := goModuleDir(withFixture); got != real {
		t.Fatalf("testdata go.mod must be skipped, real module found: got %q want %q", got, real)
	}
}

// TestBuiltinGoChecksRunOnModuleInSubdirectory is the #515 end-to-end regression: a
// repo whose Go module lives in a SUBDIRECTORY (exactly striatum's own `go/`) must
// still PASS every go builtin. Running `go ./...` at the worktree root fails with
// "directory prefix . does not contain main module"; the verifier must locate the
// nested module, run the tool there, yet bind the WHOLE worktree — proven by a test
// in the module that reads a file OUTSIDE the module (../sibling.txt). Capped at
// ASSERTED, and the receipt must honestly record working_subdir=go.
func TestBuiltinGoChecksRunOnModuleInSubdirectory(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not installed")
	}
	root := t.TempDir()
	// A sibling OUTSIDE the module; the module's test reads it via ../sibling.txt, so a
	// passing go-test proves the whole worktree (not just the module dir) is bound.
	mustWrite(t, filepath.Join(root, "sibling.txt"), "outside the module\n")
	mod := filepath.Join(root, "go")
	mustWrite(t, filepath.Join(mod, "go.mod"), "module striatumverifiersubdir\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(mod, "main.go"), "package main\n\nfunc main() { _ = 1 }\n")
	mustWrite(t, filepath.Join(mod, "main_test.go"),
		"package main\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestReadsSibling(t *testing.T) {\n\tif _, err := os.ReadFile(\"../sibling.txt\"); err != nil {\n\t\tt.Fatalf(\"sibling not bound: %v\", err)\n\t}\n}\n")
	scratch := t.TempDir()

	for _, id := range []string{"builtin:go-vet", "builtin:go-build", "builtin:go-test"} {
		res, err := ExecuteCheck(context.Background(), nil, RunRequest{CheckID: id, Cwd: root, ScratchDir: scratch})
		if err != nil {
			t.Fatalf("%s: ExecuteCheck: %v", id, err)
		}
		if res.Classification == classVerifiedEligible {
			t.Fatalf("%s: a builtin must never be verified_eligible: %+v", id, res)
		}
		// The receipt must honestly state WHERE go ran, regardless of sandbox posture.
		if res.Receipt.WorkingSubdir != "go" {
			t.Fatalf("%s: receipt must record working_subdir=go, got %q", id, res.Receipt.WorkingSubdir)
		}
		if !res.Receipt.Posture.Strict {
			// Degraded envelope (e.g. the CI runner) — the module-location fix is asserted
			// above via WorkingSubdir; the real-run pass can only be asserted where the
			// sandbox actually runs the check (strict bwrap). Skip rather than false-fail.
			t.Logf("%s: non-strict posture %q (exit=%d) — real-run assertion skipped (degraded env)",
				id, res.Receipt.Posture.Mechanism, res.Receipt.ExitCode)
			continue
		}
		if res.Receipt.NegativeControlVoid {
			t.Fatalf("%s: a real check must not void on its negative control: %+v", id, res)
		}
		if !res.Passed || res.Receipt.ExitCode != 0 {
			t.Fatalf("%s: a module-in-subdir must pass under a strict sandbox (exit=%d class=%s); stderr tail:\n%s",
				id, res.Receipt.ExitCode, res.Classification, res.StderrTail)
		}
		if res.Classification != classAsserted {
			t.Fatalf("%s: a passing builtin must classify asserted, got %q", id, res.Classification)
		}
	}
}

// TestTailWriterKeepsLastBytes — the diagnostic stderr capture retains only the LAST
// `limit` bytes (where a tool's real error sits) across many small writes and a
// single oversized write.
func TestTailWriterKeepsLastBytes(t *testing.T) {
	w := &tailWriter{limit: 8}
	for _, s := range []string{"abc", "def", "ghij"} { // "abcdefghij" → keep last 8
		_, _ = w.Write([]byte(s))
	}
	if got := w.String(); got != "cdefghij" {
		t.Fatalf("incremental tail: got %q want %q", got, "cdefghij")
	}
	w2 := &tailWriter{limit: 4}
	_, _ = w2.Write([]byte("0123456789")) // single oversized write → keep last 4
	if got := w2.String(); got != "6789" {
		t.Fatalf("oversized tail: got %q want %q", got, "6789")
	}
}

// TestExecuteResolvedSurfacesStderrTail — a failing check's stderr is surfaced on the
// CheckResult for diagnosis (it is deliberately NOT in the sealed receipt). Forced
// none-posture so the path is CI-deterministic (no sandbox primitive needed).
func TestExecuteResolvedSurfacesStderrTail(t *testing.T) {
	shBin := firstExisting("/bin/sh", "/usr/bin/sh")
	falseBin := firstExisting("/bin/false", "/usr/bin/false")
	if shBin == "" || falseBin == "" {
		t.Skip("need /bin/sh and /bin/false")
	}
	orig := lookPath
	defer func() { lookPath = orig }()
	lookPath = func(string) (string, error) { return "", os.ErrNotExist } // → MechanismNone

	res, err := executeResolved(context.Background(), ResolvedExec{
		CheckID:         "noisy",
		Argv:            []string{shBin, "-c", "echo BOOM-marker 1>&2; exit 1"},
		BinarySHA256:    "x",
		NegativeControl: &NegativeControl{Argv: []string{falseBin}}, // fails as required → check proceeds
	}, RunRequest{Cwd: t.TempDir(), ScratchDir: t.TempDir()})
	if err != nil {
		t.Fatalf("executeResolved: %v", err)
	}
	if res.Passed {
		t.Fatalf("a check exiting 1 must not pass: %+v", res)
	}
	if !strings.Contains(res.StderrTail, "BOOM-marker") {
		t.Fatalf("stderr tail must surface the check's stderr for diagnosis, got %q", res.StderrTail)
	}
	// stderr is a DIAGNOSTIC on the CheckResult, never a sealed receipt field — the
	// receipt records stdout_sha256 but carries no stderr at all (capturing it would
	// make the environment-dependent stderr break the re-execution agreement / seal).
	if doc := res.Receipt.MarshalFrontMatter(); strings.Contains(doc, "stderr") {
		t.Fatalf("the receipt must carry no stderr field (stderr is a CheckResult-only diagnostic):\n%s", doc)
	}
}

// TestSealCoversWorkingSubdir — the recorded working dir is part of WHERE the tool
// ran; relocating it must change the seal (so a receipt cannot misstate that
// `go ./...` ran at the root when it ran in a submodule).
func TestSealCoversWorkingSubdir(t *testing.T) {
	base := Receipt{CheckID: "c", Argv: []string{"go"}, BinarySHA256: "s", ExitCode: 0, CwdTreeSHA: "t"}
	relocated := base
	relocated.WorkingSubdir = "go"
	if base.computeSeal() == relocated.computeSeal() {
		t.Fatal("working_subdir must be covered by the seal (changing it must change the digest)")
	}
}

// TestBuiltinReceiptCapsAtAssertedAtGate is the cardinal RFC 0141 invariant, read
// at the AUTHORITATIVE point (the daemon-side gate read): a builtin receipt that is
// otherwise a perfect two-signal mint — strict sandbox, agreement, exit-0, seal
// binding the claim's inputs — STILL reads ASSERTED, never VERIFIED. A self-pin
// proves which harness invoked the tool, never which tool ran.
func TestBuiltinReceiptCapsAtAssertedAtGate(t *testing.T) {
	mint := func(builtinID string) ReceiptSignals {
		r := Receipt{
			CheckID:         "c",
			Argv:            []string{"go", "test", "./..."},
			BinarySHA256:    "selfsha",
			ExitCode:        0,
			StdoutSHA256:    "out",
			CwdTreeSHA:      "tree",
			Posture:         Posture{Mechanism: MechanismBubblewrap, Strict: true},
			AgreementSignal: true,
			BuiltinID:       builtinID,
			StriatumVersion: "2.34.1",
		}
		r.SealDigest = r.computeSeal()
		sig, err := ReceiptSignalsFromDocument(r.MarshalFrontMatter())
		if err != nil {
			t.Fatalf("parse signals: %v", err)
		}
		return sig
	}

	// Builtin: capped at ASSERTED despite a flawless two-signal posture.
	builtinSig := mint("builtin:go-test")
	if builtinSig.BuiltinID != "builtin:go-test" {
		t.Fatalf("gate must read builtin_id from the receipt body, got %q", builtinSig.BuiltinID)
	}
	if got := EffectiveStatusFromReceipt(builtinSig, builtinSig.SealDigest, builtinSig.SealDigest); got != artifactcontracts.ClaimStatusAsserted {
		t.Fatalf("a builtin receipt must cap at ASSERTED at the gate, got %q", got)
	}

	// Control: the SAME receipt without the builtin marker IS the two-signal mint.
	externalSig := mint("")
	if got := EffectiveStatusFromReceipt(externalSig, externalSig.SealDigest, externalSig.SealDigest); got != artifactcontracts.ClaimStatusVerified {
		t.Fatalf("an equivalent non-builtin two-signal receipt must reach VERIFIED, got %q", got)
	}
}

// TestClassifyResultBuiltinCap asserts the lane-side classification also caps a
// builtin at ASSERTED even when strict+agreement+passed all hold.
func TestClassifyResultBuiltinCap(t *testing.T) {
	strict := Posture{Strict: true}
	pass := runOutcome{exitCode: 0}
	if got := classifyResult(strict, pass, true, true, true, false); got != classVerifiedEligible {
		t.Fatalf("non-builtin strict+agree+pass must be verified_eligible, got %q", got)
	}
	if got := classifyResult(strict, pass, true, true, true, true); got != classAsserted {
		t.Fatalf("builtin strict+agree+pass must cap at asserted, got %q", got)
	}
}

// TestSealCoversBuiltinID — stripping the builtin marker to dodge the ASSERTED cap
// must change the seal (the markers are part of the tamper-evident transcript).
func TestSealCoversBuiltinID(t *testing.T) {
	base := Receipt{CheckID: "c", Argv: []string{"go"}, BinarySHA256: "s", ExitCode: 0, CwdTreeSHA: "t"}
	withBuiltin := base
	withBuiltin.BuiltinID = "builtin:go-test"
	if base.computeSeal() == withBuiltin.computeSeal() {
		t.Fatal("builtin_id must be covered by the seal (stripping it must change the digest)")
	}
	withVoid := base
	withVoid.NegativeControlVoid = true
	if base.computeSeal() == withVoid.computeSeal() {
		t.Fatal("negative_control_void must be covered by the seal")
	}
}

// TestNegativeControlVoids is the Pillar 3 vacuity guard: a check whose negative
// control unexpectedly PASSES (a known-bad the check fails to catch) yields a VOID
// receipt — the check does not discriminate the defect it claims to, so it earns
// nothing. Forced none-posture so the path is CI-deterministic.
func TestNegativeControlVoids(t *testing.T) {
	trueBin := firstExisting("/bin/true", "/usr/bin/true")
	falseBin := firstExisting("/bin/false", "/usr/bin/false")
	if trueBin == "" || falseBin == "" {
		t.Skip("need /bin/true and /bin/false")
	}
	orig := lookPath
	defer func() { lookPath = orig }()
	lookPath = func(string) (string, error) { return "", os.ErrNotExist }

	work := t.TempDir()
	if err := os.WriteFile(work+"/f.txt", []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := RunRequest{Cwd: work, ScratchDir: t.TempDir()}

	// Control = /bin/true → it PASSES → the check cannot discriminate → VOID.
	void, err := executeResolved(context.Background(), ResolvedExec{
		CheckID:         "vacuous",
		Argv:            []string{trueBin},
		BinarySHA256:    "x",
		NegativeControl: &NegativeControl{Argv: []string{trueBin}},
	}, req)
	if err != nil {
		t.Fatalf("executeResolved (void path): %v", err)
	}
	if void.Classification != classVoid || void.Passed || !void.Receipt.NegativeControlVoid {
		t.Fatalf("a passing negative control must VOID the receipt, got %+v", void)
	}
	if void.VoidReason != "negative_control_did_not_fail" {
		t.Fatalf("void must name its reason, got %q", void.VoidReason)
	}
	// The gate read must give a voided receipt nothing.
	sig, _ := ReceiptSignalsFromDocument(void.Receipt.MarshalFrontMatter())
	if got := EffectiveStatusFromReceipt(sig, sig.SealDigest, sig.SealDigest); got != artifactcontracts.ClaimStatusAsserted {
		t.Fatalf("a voided receipt must earn ASSERTED at the gate, got %q", got)
	}

	// Control = /bin/false → it FAILS as required → the check proceeds and passes.
	ok, err := executeResolved(context.Background(), ResolvedExec{
		CheckID:         "honest",
		Argv:            []string{trueBin},
		BinarySHA256:    "x",
		NegativeControl: &NegativeControl{Argv: []string{falseBin}},
	}, req)
	if err != nil {
		t.Fatalf("executeResolved (honest path): %v", err)
	}
	if ok.Classification == classVoid || !ok.Passed {
		t.Fatalf("a failing negative control must let the check proceed and pass, got %+v", ok)
	}
}

// TestBuiltinResolvedExecSelfPins — a builtin resolves to the registry argv and
// stamps the striatum self-pin + builtin markers (never VerifyBinary on argv[0]).
func TestBuiltinResolvedExecSelfPins(t *testing.T) {
	rex, err := builtinResolvedExec("builtin:go-vet")
	if err != nil {
		t.Fatalf("builtinResolvedExec: %v", err)
	}
	if rex.BuiltinID != "builtin:go-vet" || rex.BinarySHA256 == "" {
		t.Fatalf("builtin must carry its id and a self-pin sha, got %+v", rex)
	}
	// argv[0] is resolved to the absolute host path (PATH-independent in the
	// sandbox); its basename is still the registry tool, and the rest of argv is
	// verbatim from the registry.
	if len(rex.Argv) < 3 || filepath.Base(rex.Argv[0]) != "go" || rex.Argv[1] != "vet" || rex.Argv[2] != "./..." {
		t.Fatalf("builtin argv must resolve the registry tool to an absolute path, got %+v", rex.Argv)
	}
	if !filepath.IsAbs(rex.Argv[0]) {
		t.Fatalf("builtin tool must be resolved to an absolute path, got %q", rex.Argv[0])
	}
	if rex.NegativeControl == nil || filepath.Base(rex.NegativeControl.Argv[0]) != "go" {
		t.Fatalf("builtin must carry a negative control with the resolved tool, got %+v", rex.NegativeControl)
	}
	if _, err := builtinResolvedExec("builtin:does-not-exist"); err == nil {
		t.Fatal("unknown builtin must error")
	}
}
