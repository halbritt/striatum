package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/verifier"
)

// writeReceiptFile mints a receipt.v1 document with the given posture/agreement
// and writes it under the run repo at receiptRel. It returns the receipt's seal
// digest so the test can bind a claim's evidence_digest to it.
func writeReceiptFile(t *testing.T, repoRoot, receiptRel, checkID, treeSHA string, exitCode int, strict, agreement bool) string {
	t.Helper()
	r := verifier.Receipt{
		CheckID:         checkID,
		Argv:            []string{"/bin/true"},
		BinarySHA256:    "abc",
		ExitCode:        exitCode,
		StdoutSHA256:    "def",
		CwdTreeSHA:      treeSHA,
		CreatedAt:       "2026-06-18T00:00:00Z",
		Posture:         verifier.Posture{Mechanism: verifier.MechanismBubblewrap, Strict: strict},
		AgreementSignal: agreement,
	}
	// The seal the receipt carries is keyed to the worktree tree-sha so the gate's
	// bound-input check (evidence_digest == seal_digest == bound_input_digest)
	// holds: the claim binds the receipt that sealed THESE inputs.
	r.SealDigest = treeSHA
	full := filepath.Join(repoRoot, receiptRel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := r.MarshalFrontMatter()
	if err := os.WriteFile(full, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return treeSHA
}

// seedAttestation inserts a daemon-owned operator attestation row binding
// (repo_claim, checkID, binarySHA256). The gate (RFC 0141 / D243 / #482) requires
// such an un-revoked row before an external two-signal receipt may read VERIFIED.
// The fixtures mint receipts with binary_sha256 "abc" (writeReceiptFile), so the
// genuine, operator-blessed path seeds the matching row here.
func seedAttestation(t *testing.T, ctx context.Context, runner db.Runner, checkID, binarySHA256 string) {
	t.Helper()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.verifier_attestations (
		  repository_id, attestation_id, check_id, binary_sha256, attested_by
		) VALUES ('repo_claim', $1, $2, $3, 'operator-test')
		ON CONFLICT DO NOTHING`,
		"att_"+checkID+"_"+binarySHA256, checkID, binarySHA256); err != nil {
		t.Fatalf("seed attestation: %v", err)
	}
}

func claimVerificationEntry(t *testing.T, entries []map[string]any, claimID string) map[string]any {
	t.Helper()
	for _, e := range entries {
		if fmt.Sprint(e["claim_id"]) == claimID {
			return e
		}
	}
	t.Fatalf("no claim_verification entry for %q in %#v", claimID, entries)
	return nil
}

// TestRunClaimVerificationVerifiedWithTwoSignalReceipt verifies the gate READ:
// a VERIFIED-authored claim bound to a strict, agreeing, exit-0 receipt reads
// back as VERIFIED. This is the executable-half completion read — the daemon
// reads the sealed receipt; it executes nothing.
func TestRunClaimVerificationVerifiedWithTwoSignalReceipt(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := setupClaimLedgerRun(t, ctx, runner)

	const treeSHA = "treesha_verified"
	writeReceiptFile(t, repoRoot, "docs/run/receipts/C1.md", "go-test", treeSHA, 0, true, true)
	// RFC 0141 / D243 (#482): an external two-signal receipt now ALSO requires a
	// daemon-owned operator attestation for (check_id, binary_sha256) before it may
	// read VERIFIED. writeReceiptFile pins binary_sha256 "abc"; bless those bytes.
	seedAttestation(t, ctx, runner, "go-test", "abc")

	body := claimLedgerDoc("0", `  - id: C1
    status: VERIFIED
    text: "go test passes under a sealed two-signal receipt."
    bound_input_digest: "`+treeSHA+`"
    receipt_ref: "docs/run/receipts/C1.md"
    evidence_digest: "`+treeSHA+`"
`)
	if _, err := publishClaimLedger(t, ctx, runner, repoRoot, "claims_0", "docs/run/CLAIMS_0.md", body); err != nil {
		t.Fatalf("publish ledger: %v", err)
	}

	entries, err := evaluateRunClaimVerification(ctx, runner, "repo_claim", "run_claim")
	if err != nil {
		t.Fatalf("evaluateRunClaimVerification: %v", err)
	}
	entry := claimVerificationEntry(t, entries, "C1")
	if got := fmt.Sprint(entry["effective_status"]); got != "VERIFIED" {
		t.Fatalf("a strict two-signal receipt must read VERIFIED, got %q (basis %v)", got, entry["verification_basis"])
	}
	if got := fmt.Sprint(entry["verification_basis"]); got != "two_signal_sealed_receipt" {
		t.Fatalf("basis = %q, want two_signal_sealed_receipt", got)
	}
}

// TestRunClaimVerificationDegradesWhenAttestationMissing is the RFC 0141 / D243
// (#482) security regression: a genuine, strict, two-signal external receipt that
// would otherwise read VERIFIED must FAIL CLOSED to ASSERTED when no daemon-owned
// operator attestation blesses its (check_id, binary_sha256). This is the gap the
// graduation closes — a non-compliant lane could forge the repo-file attest
// sidecar, but the gate consults daemon PG, not the lane-writable sidecar. With no
// attestation row, the forge buys nothing.
func TestRunClaimVerificationDegradesWhenAttestationMissing(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := setupClaimLedgerRun(t, ctx, runner)

	const treeSHA = "treesha_unattested"
	writeReceiptFile(t, repoRoot, "docs/run/receipts/C1.md", "go-test", treeSHA, 0, true, true)
	// Deliberately seed NO attestation (or, equivalently, the lane forged a sidecar
	// the daemon never saw): the gate has no PG row to honor.

	body := claimLedgerDoc("0", `  - id: C1
    status: VERIFIED
    text: "a strict two-signal receipt, but no operator ever blessed these bytes."
    bound_input_digest: "`+treeSHA+`"
    receipt_ref: "docs/run/receipts/C1.md"
    evidence_digest: "`+treeSHA+`"
`)
	if _, err := publishClaimLedger(t, ctx, runner, repoRoot, "claims_0", "docs/run/CLAIMS_0.md", body); err != nil {
		t.Fatalf("publish ledger: %v", err)
	}

	entry := claimVerificationEntry(t, mustEvaluate(t, ctx, runner), "C1")
	if got := fmt.Sprint(entry["effective_status"]); got != "ASSERTED" {
		t.Fatalf("an unattested external receipt must fail closed to ASSERTED, got %q (basis %v)", got, entry["verification_basis"])
	}
	if got := fmt.Sprint(entry["verification_basis"]); got != "attestation_missing" {
		t.Fatalf("basis = %q, want attestation_missing", got)
	}
}

// TestRunClaimVerificationDegradesWhenAttestationRevoked verifies a revoked
// attestation is not honored: revocation is fail-closed at the gate (the unique
// active index is partial WHERE revoked_at IS NULL, and the gate query filters on
// it). A blessing the operator withdrew can no longer carry a claim to VERIFIED.
func TestRunClaimVerificationDegradesWhenAttestationRevoked(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := setupClaimLedgerRun(t, ctx, runner)

	const treeSHA = "treesha_revoked"
	writeReceiptFile(t, repoRoot, "docs/run/receipts/C1.md", "go-test", treeSHA, 0, true, true)
	seedAttestation(t, ctx, runner, "go-test", "abc")
	if err := runner.Exec(ctx, `
		UPDATE striatumd.verifier_attestations
		   SET revoked_at = now(), revoke_reason = 'test revoke'
		 WHERE repository_id = 'repo_claim' AND check_id = 'go-test'`); err != nil {
		t.Fatalf("revoke attestation: %v", err)
	}

	body := claimLedgerDoc("0", `  - id: C1
    status: VERIFIED
    text: "blessed once, then revoked."
    bound_input_digest: "`+treeSHA+`"
    receipt_ref: "docs/run/receipts/C1.md"
    evidence_digest: "`+treeSHA+`"
`)
	if _, err := publishClaimLedger(t, ctx, runner, repoRoot, "claims_0", "docs/run/CLAIMS_0.md", body); err != nil {
		t.Fatalf("publish ledger: %v", err)
	}

	entry := claimVerificationEntry(t, mustEvaluate(t, ctx, runner), "C1")
	if got := fmt.Sprint(entry["effective_status"]); got != "ASSERTED" {
		t.Fatalf("a revoked attestation must fail closed to ASSERTED, got %q (basis %v)", got, entry["verification_basis"])
	}
	if got := fmt.Sprint(entry["verification_basis"]); got != "attestation_missing" {
		t.Fatalf("basis = %q, want attestation_missing", got)
	}
}

// TestRunClaimVerificationAttestationBoundToExactBytes verifies an attestation for
// a DIFFERENT sha does not satisfy the gate: the row is keyed by the exact bytes,
// so a re-pin of different bytes (or an attestation of unrelated bytes) cannot
// carry a receipt sealed to the real bytes to VERIFIED.
func TestRunClaimVerificationAttestationBoundToExactBytes(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := setupClaimLedgerRun(t, ctx, runner)

	const treeSHA = "treesha_wrongbytes"
	writeReceiptFile(t, repoRoot, "docs/run/receipts/C1.md", "go-test", treeSHA, 0, true, true)
	// The receipt pins binary_sha256 "abc"; the operator blessed DIFFERENT bytes.
	seedAttestation(t, ctx, runner, "go-test", "deadbeef")

	body := claimLedgerDoc("0", `  - id: C1
    status: VERIFIED
    text: "attested, but for the wrong bytes."
    bound_input_digest: "`+treeSHA+`"
    receipt_ref: "docs/run/receipts/C1.md"
    evidence_digest: "`+treeSHA+`"
`)
	if _, err := publishClaimLedger(t, ctx, runner, repoRoot, "claims_0", "docs/run/CLAIMS_0.md", body); err != nil {
		t.Fatalf("publish ledger: %v", err)
	}

	entry := claimVerificationEntry(t, mustEvaluate(t, ctx, runner), "C1")
	if got := fmt.Sprint(entry["effective_status"]); got != "ASSERTED" {
		t.Fatalf("an attestation of different bytes must not satisfy the gate, got %q (basis %v)", got, entry["verification_basis"])
	}
	if got := fmt.Sprint(entry["verification_basis"]); got != "attestation_missing" {
		t.Fatalf("basis = %q, want attestation_missing", got)
	}
}

// TestRunClaimVerificationDegradesOnMissingReceipt verifies the load-bearing
// D227 rule: a missing/wedged verify degrades the claim to ASSERTED and NEVER
// blocks completion — the gate reads, finds no receipt, and records ASSERTED.
func TestRunClaimVerificationDegradesOnMissingReceipt(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := setupClaimLedgerRun(t, ctx, runner)
	_ = repoRoot

	const treeSHA = "treesha_missing"
	// Author VERIFIED with a receipt_ref that is never written (the verifier lane
	// wedged / timed out). The provenance lint passes (the ledger is internally
	// consistent), but the gate read finds no receipt bytes.
	body := claimLedgerDoc("0", `  - id: C1
    status: VERIFIED
    text: "claims verified, but the verifier lane never produced the receipt."
    bound_input_digest: "`+treeSHA+`"
    receipt_ref: "docs/run/receipts/MISSING.md"
    evidence_digest: "`+treeSHA+`"
`)
	if _, err := publishClaimLedger(t, ctx, runner, repoRoot, "claims_0", "docs/run/CLAIMS_0.md", body); err != nil {
		t.Fatalf("publish ledger: %v", err)
	}

	entries, err := evaluateRunClaimVerification(ctx, runner, "repo_claim", "run_claim")
	if err != nil {
		t.Fatalf("evaluateRunClaimVerification: %v", err)
	}
	entry := claimVerificationEntry(t, entries, "C1")
	if got := fmt.Sprint(entry["effective_status"]); got != "ASSERTED" {
		t.Fatalf("a missing receipt must degrade to ASSERTED, got %q", got)
	}
	if got := fmt.Sprint(entry["verification_basis"]); got != "receipt_unavailable" {
		t.Fatalf("basis = %q, want receipt_unavailable", got)
	}
}

// TestRunClaimVerificationDegradesOnNonStrictReceipt verifies a lone-signal
// receipt (passing but non-strict sandbox, no agreement) reads back as ASSERTED,
// never VERIFIED — a lone exit-0 earns only ASSERTED.
func TestRunClaimVerificationDegradesOnNonStrictReceipt(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := setupClaimLedgerRun(t, ctx, runner)

	const treeSHA = "treesha_nonstrict"
	// exit-0 but non-strict sandbox and no agreement → only one signal.
	writeReceiptFile(t, repoRoot, "docs/run/receipts/C1.md", "go-test", treeSHA, 0, false, false)

	body := claimLedgerDoc("0", `  - id: C1
    status: VERIFIED
    text: "passing but under a non-strict envelope with no agreement."
    bound_input_digest: "`+treeSHA+`"
    receipt_ref: "docs/run/receipts/C1.md"
    evidence_digest: "`+treeSHA+`"
`)
	if _, err := publishClaimLedger(t, ctx, runner, repoRoot, "claims_0", "docs/run/CLAIMS_0.md", body); err != nil {
		t.Fatalf("publish ledger: %v", err)
	}

	entries, err := evaluateRunClaimVerification(ctx, runner, "repo_claim", "run_claim")
	if err != nil {
		t.Fatalf("evaluateRunClaimVerification: %v", err)
	}
	entry := claimVerificationEntry(t, entries, "C1")
	if got := fmt.Sprint(entry["effective_status"]); got != "ASSERTED" {
		t.Fatalf("a non-strict lone-signal receipt must read ASSERTED, got %q (basis %v)", got, entry["verification_basis"])
	}
}

// TestRunClaimVerificationPassesThroughLowerRungs verifies DESIGNED / ASSERTED
// claims need no receipt and pass through at their authored rung (no read).
func TestRunClaimVerificationPassesThroughLowerRungs(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := setupClaimLedgerRun(t, ctx, runner)

	body := claimLedgerDoc("0", `  - id: C1
    status: DESIGNED
    text: "we intend to."
  - id: C2
    status: ASSERTED
    text: "a structured read says so."
`)
	if _, err := publishClaimLedger(t, ctx, runner, repoRoot, "claims_0", "docs/run/CLAIMS_0.md", body); err != nil {
		t.Fatalf("publish ledger: %v", err)
	}

	entries, err := evaluateRunClaimVerification(ctx, runner, "repo_claim", "run_claim")
	if err != nil {
		t.Fatalf("evaluateRunClaimVerification: %v", err)
	}
	if got := fmt.Sprint(claimVerificationEntry(t, entries, "C1")["effective_status"]); got != "DESIGNED" {
		t.Fatalf("C1 must pass through DESIGNED, got %q", got)
	}
	if got := fmt.Sprint(claimVerificationEntry(t, entries, "C2")["effective_status"]); got != "ASSERTED" {
		t.Fatalf("C2 must pass through ASSERTED, got %q", got)
	}
}

// TestRunClaimVerificationNoLedgerIsNoOp verifies a run with no claim_ledger
// yields no entries (the gate is unaffected).
func TestRunClaimVerificationNoLedgerIsNoOp(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	_ = setupClaimLedgerRun(t, ctx, runner)

	entries, err := evaluateRunClaimVerification(ctx, runner, "repo_claim", "run_claim")
	if err != nil {
		t.Fatalf("evaluateRunClaimVerification: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("a run with no claim_ledger must yield no entries, got %#v", entries)
	}
}

// realTrueBin returns an always-pass binary (`/bin/true`), skipping the test if
// the host has none.
func realTrueBin(t *testing.T) string {
	t.Helper()
	for _, p := range []string{"/usr/bin/true", "/bin/true"} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	t.Skip("no /bin/true on host")
	return ""
}

// mintRealReceipt runs the REAL sandboxed verifier (verifier.ExecuteCheck — the
// same code path `striatum verifier run` drives in a verify-job lane) against an
// always-pass allowlisted check over cwd, and returns the minted result. The seal
// it carries is computed over the real transcript, not fabricated.
func mintRealReceipt(t *testing.T, trueBin, cwd, scratch string) verifier.CheckResult {
	t.Helper()
	data, err := os.ReadFile(trueBin) //nolint:gosec // hashing a known system binary to pin the allowlist
	if err != nil {
		t.Fatalf("read %s: %v", trueBin, err)
	}
	sum := sha256.Sum256(data)
	allowJSON := fmt.Sprintf(
		`{"schema_version":%q,"checks":[{"id":"always-pass","argv":[%q],"binary_sha256":%q,"pass_when":"exit_zero"}]}`,
		verifier.AllowlistSchemaVersion, trueBin, hex.EncodeToString(sum[:]),
	)
	allow, err := verifier.ParseAllowlist([]byte(allowJSON))
	if err != nil {
		t.Fatalf("parse allowlist: %v", err)
	}
	res, err := verifier.ExecuteCheck(context.Background(), allow, verifier.RunRequest{
		CheckID: "always-pass", Cwd: cwd, ScratchDir: scratch,
	})
	if err != nil {
		t.Fatalf("ExecuteCheck: %v", err)
	}
	return res
}

// TestRunClaimVerificationEndToEndRealReceiptMint is the connected RFC 0134 / D227
// proof: it wires the REAL sandboxed verifier mint (verifier.ExecuteCheck) through
// the REAL daemon-side completion gate read (evaluateRunClaimVerification) in one
// flow, with NO fabricated seal — the claim binds to the receipt's actual computed
// seal_digest. The other tests prove the mint and the gate read separately; this
// proves they compose.
//
//   - On a strict-sandbox host (bwrap), the minted two-signal receipt reads back
//     VERIFIED (basis two_signal_sealed_receipt) — the full executable path.
//   - On a degraded host (no strict primitive), it asserts the fail-safe: a
//     non-strict mint can never read VERIFIED (it caps at ASSERTED).
//   - Re-minting over a CHANGED worktree tree yields a different seal; the claim
//     still naming the old seal auto-decays to ASSERTED (basis
//     receipt_seal_mismatch) — VERIFIED auto-decays when bound inputs change.
func TestRunClaimVerificationEndToEndRealReceiptMint(t *testing.T) {
	ctx := context.Background()
	trueBin := realTrueBin(t)
	runner := pgtest.Pool(t).Runner
	repoRoot := setupClaimLedgerRun(t, ctx, runner)

	// A dedicated worktree the check witnesses (read-only-bound in the sandbox),
	// kept out of the run's docs/run/ artifact tree so receipt/ledger writes do
	// not perturb its tree-sha.
	cwd := filepath.Join(repoRoot, "verify_cwd")
	scratch := filepath.Join(repoRoot, "verify_scratch")
	for _, d := range []string{cwd, scratch} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cwd, "target.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := mintRealReceipt(t, trueBin, cwd, scratch)
	if res.Receipt.SealDigest == "" || res.Receipt.CwdTreeSHA == "" {
		t.Fatalf("a real mint must carry a seal and a tree-sha: %+v", res.Receipt)
	}
	// RFC 0141 / D243 (#482): the gate requires a daemon-owned operator attestation
	// for the external check's exact bytes. mintRealReceipt pins the real /bin/true
	// sha as check "always-pass"; bless those exact bytes so the strict-host arm can
	// still read VERIFIED through the full executable path.
	seedAttestation(t, ctx, runner, "always-pass", res.Receipt.BinarySHA256)

	const receiptRel = "docs/run/receipts/C1.md"
	full := filepath.Join(repoRoot, receiptRel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(res.Receipt.MarshalFrontMatter()), 0o644); err != nil {
		t.Fatal(err)
	}

	seal := res.Receipt.SealDigest
	body := claimLedgerDoc("0", `  - id: C1
    status: VERIFIED
    text: "always-pass exits 0 under the sandboxed verifier lane."
    bound_input_digest: "`+seal+`"
    receipt_ref: "`+receiptRel+`"
    evidence_digest: "`+seal+`"
`)
	if _, err := publishClaimLedger(t, ctx, runner, repoRoot, "claims_0", "docs/run/CLAIMS_0.md", body); err != nil {
		t.Fatalf("publish ledger: %v", err)
	}

	entry := claimVerificationEntry(t, mustEvaluate(t, ctx, runner), "C1")
	if res.Receipt.Posture.Strict {
		// Strict host: the two-signal mint must read back VERIFIED through the gate.
		if !res.Passed || !res.Receipt.AgreementSignal {
			t.Fatalf("a strict mint of an always-pass check must pass and agree: %+v", res)
		}
		if got := fmt.Sprint(entry["effective_status"]); got != "VERIFIED" {
			t.Fatalf("a strict two-signal receipt must read VERIFIED, got %q (basis %v)", got, entry["verification_basis"])
		}
		if got := fmt.Sprint(entry["verification_basis"]); got != "two_signal_sealed_receipt" {
			t.Fatalf("basis = %q, want two_signal_sealed_receipt", got)
		}
	} else {
		// Degraded host fail-safe: a non-strict mint can never read VERIFIED.
		if got := fmt.Sprint(entry["effective_status"]); got != "ASSERTED" {
			t.Fatalf("a non-strict mint must read ASSERTED (fail-safe), got %q (basis %v)", got, entry["verification_basis"])
		}
	}

	// Auto-decay on bound-input change: change the witnessed worktree, re-mint (the
	// new tree-sha yields a new seal), and overwrite the receipt. The published
	// claim still names the OLD seal, so the gate read now finds a seal mismatch
	// and decays the claim to ASSERTED — host-independent.
	if err := os.WriteFile(filepath.Join(cwd, "target.txt"), []byte("v2-changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	res2 := mintRealReceipt(t, trueBin, cwd, scratch)
	if res2.Receipt.SealDigest == seal {
		t.Fatalf("re-mint over a changed tree must produce a different seal (got identical %q)", seal)
	}
	if err := os.WriteFile(full, []byte(res2.Receipt.MarshalFrontMatter()), 0o644); err != nil {
		t.Fatal(err)
	}

	entry2 := claimVerificationEntry(t, mustEvaluate(t, ctx, runner), "C1")
	if got := fmt.Sprint(entry2["effective_status"]); got != "ASSERTED" {
		t.Fatalf("a claim bound to a stale seal must auto-decay to ASSERTED, got %q", got)
	}
	if got := fmt.Sprint(entry2["verification_basis"]); got != "receipt_seal_mismatch" {
		t.Fatalf("decay basis = %q, want receipt_seal_mismatch", got)
	}
}

// mustEvaluate runs the gate read or fails the test.
func mustEvaluate(t *testing.T, ctx context.Context, runner any) []map[string]any {
	t.Helper()
	entries, err := evaluateRunClaimVerification(ctx, runner, "repo_claim", "run_claim")
	if err != nil {
		t.Fatalf("evaluateRunClaimVerification: %v", err)
	}
	return entries
}
