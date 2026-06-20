package verifier

// RFC 0141 / D238 — the Attestation value object: the trust signal a verified lane
// cannot forge, and the PINNED → VERIFIED hinge.
//
// `pin --host-here` runs IN the disposable verifier lane and only ever produces
// PINNED-but-unattested rows: it observes bytes, it does not bless them. Promotion
// to VERIFIABLE requires a SEPARATE attestation minted by an OPERATOR principal
// (`striatum verifier attest`, refused for any lane/session token). The attestation
// lives in its OWN sidecar artifact (NOT the lane-written pins file), keyed by
// id→sha, so re-pinning new bytes silently invalidates a stale blessing.
//
// This file is the pure data + read side (parse, Covers). The operator-token gate
// and the write side live in the `attest` verb.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// AllowlistAttestSchemaVersion marks the operator attestation sidecar.
const AllowlistAttestSchemaVersion = "striatum.verifier_allowlist_attest.v1"

// AttestEntry is one operator attestation: "I, an operator, stand behind THESE
// bytes for THIS check." It binds the check id to the exact pinned sha, so a later
// re-pin of different bytes does not inherit the blessing.
type AttestEntry struct {
	ID           string `json:"id"`
	BinarySHA256 string `json:"binary_sha256"`
	AttestedAt   string `json:"attested_at,omitempty"`
	// AttestedBy records the operator principal id the daemon authorized the attest
	// against (attribution; the enforcement is the operator-token gate on the verb).
	AttestedBy string `json:"attested_by,omitempty"`
}

// AttestFile is the on-disk JSON shape of the attestation sidecar.
type AttestFile struct {
	SchemaVersion string        `json:"schema_version"`
	HostArchFP    string        `json:"host_arch_fingerprint,omitempty"`
	Attestations  []AttestEntry `json:"attestations"`
}

// Attestations is the parsed attestation set, keyed by check id → attested sha.
type Attestations struct {
	bySHA map[string]string
}

// EmptyAttestations is the nil-safe zero value (no attestations yet).
func EmptyAttestations() *Attestations { return &Attestations{bySHA: map[string]string{}} }

// Covers reports whether an operator has attested THIS check at THIS exact sha. It
// is nil-safe: a missing sidecar means nothing is attested (fail-closed → ASSERTED).
func (a *Attestations) Covers(id, binarySHA256 string) bool {
	if a == nil || a.bySHA == nil {
		return false
	}
	want, ok := a.bySHA[strings.TrimSpace(id)]
	return ok && want != "" && strings.EqualFold(want, strings.TrimSpace(binarySHA256))
}

// LoadAttestations reads an attestation sidecar; a missing file is NOT an error —
// it simply means nothing is attested yet (the common pre-attest state).
func LoadAttestations(path string) (*Attestations, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-controlled sidecar path
	if err != nil {
		if os.IsNotExist(err) {
			return EmptyAttestations(), nil
		}
		return nil, fmt.Errorf("read verifier attestation sidecar %q: %w", path, err)
	}
	return ParseAttestations(raw)
}

// ParseAttestations validates an attestation document already read into memory.
func ParseAttestations(raw []byte) (*Attestations, error) {
	var file AttestFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse verifier attestation sidecar: %w", err)
	}
	if strings.TrimSpace(file.SchemaVersion) != AllowlistAttestSchemaVersion {
		return nil, fmt.Errorf("verifier attestation sidecar schema_version must be %q, got %q", AllowlistAttestSchemaVersion, file.SchemaVersion)
	}
	bySHA := make(map[string]string, len(file.Attestations))
	for idx, e := range file.Attestations {
		id := strings.TrimSpace(e.ID)
		if id == "" {
			return nil, fmt.Errorf("verifier attestation sidecar attestations[%d].id must be non-empty", idx)
		}
		sha := strings.ToLower(strings.TrimSpace(e.BinarySHA256))
		if sha == "" {
			return nil, fmt.Errorf("verifier attestation %q must bind a binary_sha256 (an attestation blesses exact bytes)", id)
		}
		bySHA[id] = sha
	}
	return &Attestations{bySHA: bySHA}, nil
}
