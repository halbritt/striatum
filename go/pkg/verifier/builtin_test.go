package verifier

import (
	"context"
	"os"
	"testing"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
)

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
	if len(rex.Argv) == 0 || rex.Argv[0] != "go" {
		t.Fatalf("builtin argv must come from the registry, got %+v", rex.Argv)
	}
	if rex.NegativeControl == nil {
		t.Fatal("builtin must carry a negative control")
	}
	if _, err := builtinResolvedExec("builtin:does-not-exist"); err == nil {
		t.Fatal("unknown builtin must error")
	}
}
