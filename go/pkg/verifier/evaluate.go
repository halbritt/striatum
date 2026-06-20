package verifier

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
)

// MarshalFrontMatter renders the receipt as a receipt.v1 markdown document the
// publisher accepts (validated by the artifactcontracts "receipt" schema). The
// front matter carries the schema-required transcript fields; the body records
// the resolved sandbox posture and the agreement signal (both covered by the
// seal) so the gate reader can apply the two-signal rule from durable bytes.
func (r Receipt) MarshalFrontMatter() string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("schema_version: \"striatum.receipt.v1\"\n")
	b.WriteString("artifact_kind: \"receipt\"\n")
	fmt.Fprintf(&b, "check_id: %q\n", r.CheckID)
	b.WriteString("argv: [")
	for i, tok := range r.Argv {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", tok)
	}
	b.WriteString("]\n")
	fmt.Fprintf(&b, "binary_sha256: %q\n", r.BinarySHA256)
	fmt.Fprintf(&b, "exit_code: %d\n", r.ExitCode)
	fmt.Fprintf(&b, "stdout_sha256: %q\n", r.StdoutSHA256)
	fmt.Fprintf(&b, "cwd_tree_sha: %q\n", r.CwdTreeSHA)
	fmt.Fprintf(&b, "seal_digest: %q\n", r.SealDigest)
	if r.CreatedAt != "" {
		fmt.Fprintf(&b, "created_at: %q\n", r.CreatedAt)
	}
	b.WriteString("---\n\n")
	b.WriteString("# Verifier receipt\n\n")
	fmt.Fprintf(&b, "- check_id: `%s`\n", r.CheckID)
	fmt.Fprintf(&b, "- exit_code: `%d`\n", r.ExitCode)
	fmt.Fprintf(&b, "- sandbox_mechanism: `%s`\n", r.Posture.Mechanism)
	fmt.Fprintf(&b, "- sandbox_strict: `%s`\n", strconv.FormatBool(r.Posture.Strict))
	fmt.Fprintf(&b, "- independent_reexecution_agreement: `%s`\n", strconv.FormatBool(r.AgreementSignal))
	// RFC 0141 markers (body, seal-covered — like the posture/agreement signals):
	// builtin_id caps the gate read at ASSERTED, negative_control_void marks a
	// receipt the vacuity guard rejected. Both are read back by the gate.
	if r.BuiltinID != "" {
		fmt.Fprintf(&b, "- builtin_id: `%s`\n", r.BuiltinID)
		if r.StriatumVersion != "" {
			fmt.Fprintf(&b, "- striatum_version: `%s`\n", r.StriatumVersion)
		}
	}
	fmt.Fprintf(&b, "- negative_control_void: `%s`\n", strconv.FormatBool(r.NegativeControlVoid))
	if r.WorkingSubdir != "" {
		// Honest record of WHERE the tool ran: non-empty only for a go-* builtin whose
		// module is in a subdirectory (e.g. striatum's `go/`). Seal-covered.
		fmt.Fprintf(&b, "- working_subdir: `%s`\n", r.WorkingSubdir)
	}
	if len(r.Posture.Notes) > 0 {
		fmt.Fprintf(&b, "- posture_notes: %s\n", strings.Join(r.Posture.Notes, "; "))
	}
	return b.String()
}

// ReceiptSignals is the gate-side projection of a sealed receipt: the minimum a
// pure reader needs to apply the D227 two-signal rule WITHOUT executing anything.
// It is parsed from a receipt.v1 front-matter map plus the receipt body markers
// (the strict-posture and agreement signals the seal covers).
type ReceiptSignals struct {
	CheckID     string
	ExitCode    int
	SealDigest  string
	CwdTreeSHA  string
	Strict      bool
	Agreement   bool
	BodyPresent bool
	// BinarySHA256 is the sha256 of the resolved binary the check ran (argv[0]),
	// sealed into the transcript. The gate reads it to bind a daemon-owned operator
	// attestation to the EXACT bytes (RFC 0141 / D243 / #482): VERIFIED requires an
	// un-revoked attestation row for (repository, check_id, binary_sha256). Empty for
	// a malformed receipt → no attestation can match → fail-closed to ASSERTED.
	BinarySHA256 string
	// BuiltinID is non-empty for a RFC 0141 builtin receipt (self-pinned to the
	// striatum binary). It CAPS the gate read at ASSERTED — a self-pin attests which
	// harness invoked the tool, never which tool ran.
	BuiltinID string
	// NegativeControlVoid marks a receipt the Pillar 3 vacuity guard rejected (the
	// known-bad control passed). A void receipt earns nothing.
	NegativeControlVoid bool
}

// ReceiptSignalsFromDocument parses a receipt.v1 markdown document into the
// gate-read projection. It is the daemon-side reader: it does NOT execute, it
// only reads sealed bytes. The strict/agreement markers live in the body
// (covered by the seal); a receipt whose body omits them reads as non-strict /
// no-agreement, which caps its earned status at ASSERTED — fail-safe.
func ReceiptSignalsFromDocument(doc string) (ReceiptSignals, error) {
	block, ok := artifactcontracts.FrontMatterBlock(doc)
	if !ok {
		return ReceiptSignals{}, fmt.Errorf("receipt has no front matter block")
	}
	parsed, err := artifactcontracts.ParseFrontMatterBlock(block)
	if err != nil {
		return ReceiptSignals{}, fmt.Errorf("parse receipt front matter: %w", err)
	}
	sig := ReceiptSignals{
		CheckID:             stringField(parsed["check_id"]),
		SealDigest:          stringField(parsed["seal_digest"]),
		CwdTreeSHA:          stringField(parsed["cwd_tree_sha"]),
		BinarySHA256:        strings.ToLower(stringField(parsed["binary_sha256"])),
		Strict:              bodyMarker(doc, "sandbox_strict"),
		Agreement:           bodyMarker(doc, "independent_reexecution_agreement"),
		BuiltinID:           bodyStringMarker(doc, "builtin_id"),
		NegativeControlVoid: bodyMarker(doc, "negative_control_void"),
		BodyPresent:         true,
	}
	if v, ok := parsed["exit_code"]; ok {
		sig.ExitCode = intField(v)
	}
	return sig, nil
}

// EffectiveStatusFromReceipt computes the claim-status rung a sealed receipt
// EARNS for a claim, given the claim's bound input digest and evidence digest.
// This is the pure-read core the run-completion gate uses to allow a claim's
// VERIFIED status WITHOUT executing anything. The rules (D227):
//
//   - The receipt must bind THESE inputs: evidence_digest == seal_digest AND
//     evidence_digest == bound_input_digest (the claim is bound to the receipt
//     that sealed its current inputs). A mismatch auto-decays → ASSERTED.
//   - VERIFIED requires TWO signals: the sealed receipt (exit-0) PLUS the
//     independent re-execution agreement, under a STRICT sandbox. A lone exit-0
//     earns only ASSERTED.
//   - A non-strict posture, a missing agreement, or a non-zero exit → ASSERTED.
//   - The gate NEVER blocks on a missing/wedged receipt: the caller treats a
//     parse failure or absent receipt as ASSERTED (the lattice floor for a
//     claim that named a receipt but cannot produce sealed two-signal evidence).
func EffectiveStatusFromReceipt(sig ReceiptSignals, boundInputDigest, evidenceDigest string) string {
	if boundInputDigest == "" || evidenceDigest == "" {
		return artifactcontracts.ClaimStatusAsserted
	}
	if evidenceDigest != boundInputDigest {
		return artifactcontracts.ClaimStatusAsserted // decayed: receipt binds stale inputs
	}
	if sig.SealDigest == "" || sig.SealDigest != evidenceDigest {
		return artifactcontracts.ClaimStatusAsserted // the named receipt's seal does not match the claim's evidence
	}
	if sig.NegativeControlVoid {
		return artifactcontracts.ClaimStatusAsserted // Pillar 3: a vacuity-voided receipt earns nothing
	}
	if sig.ExitCode != 0 {
		return artifactcontracts.ClaimStatusAsserted // a failing or indeterminate check earns no upgrade
	}
	// RFC 0141 HARD CAP: a builtin receipt self-pins to the striatum binary, which
	// proves which harness invoked the tool, NEVER which `go`/`git` ran. It can
	// never independently earn VERIFIED, regardless of strict posture + agreement.
	if sig.BuiltinID != "" {
		return artifactcontracts.ClaimStatusAsserted
	}
	if sig.Strict && sig.Agreement {
		return artifactcontracts.ClaimStatusVerified // two signals under a strict envelope: the sealed mint
	}
	return artifactcontracts.ClaimStatusAsserted // a lone exit-0 (no strict envelope or no agreement)
}

func stringField(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func intField(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		i, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(v)))
		return i
	}
}

// bodyMarker reads a `- <key>: `true|false`` line from the receipt body (the
// seal-covered posture/agreement markers MarshalFrontMatter writes).
func bodyMarker(doc, key string) bool {
	needle := "- " + key + ": `true`"
	return strings.Contains(doc, needle)
}

// bodyStringMarker reads the value of a `- <key>: `<value>`` line from the receipt
// body (the seal-covered builtin_id marker). Returns "" when the marker is absent.
func bodyStringMarker(doc, key string) string {
	prefix := "- " + key + ": `"
	idx := strings.Index(doc, prefix)
	if idx < 0 {
		return ""
	}
	rest := doc[idx+len(prefix):]
	end := strings.IndexByte(rest, '`')
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}
