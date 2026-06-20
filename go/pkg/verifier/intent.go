package verifier

// RFC 0141 / D238 — the two-layer allowlist that makes a verification gate
// generatable without committing a host-specific binary sha.
//
// The shipped allowlist (RFC 0134, allowlist.go) content-addresses bytes that
// differ per host/distro, so it can never be a committed fixture. This file
// splits that artifact in two:
//
//   - INTENT (`striatum.verifier_allowlist_intent.v1`): committed, hashless,
//     reviewable. It is `AllowlistEntry` MINUS `binary_sha256` PLUS `backs_claim`
//     and an optional `negative_control`. It says WHICH checks are sanctioned and
//     WHAT each one proves. The scaffold emits it into the run's `forbidden_paths`
//     so a verified lane can never sanction its own checks.
//   - PINS (`striatum.verifier_allowlist.v1`, the existing Allowlist shape):
//     gitignored, per-host, NEVER committed and NEVER lane-authored. Produced by
//     `striatum verifier pin --host-here`, which OBSERVES each sanctioned binary's
//     sha inside the disposable sandbox — the operator never transcribes a hash.
//
// `JoinIntentPins` is the PURE join the lane runs at `verifier run`: intent ⋈ pins
// ⋈ attestations → a three-valued status per check (NAMED-but-unpinned /
// PINNED-but-unattested / VERIFIABLE). It executes nothing; it only resolves which
// rung a check is ELIGIBLE for before any process runs.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
)

// AllowlistIntentSchemaVersion marks the committed, hashless intent file.
const AllowlistIntentSchemaVersion = "striatum.verifier_allowlist_intent.v1"

// IntentEntry is one sanctioned check in the committed intent layer: the existing
// AllowlistEntry MINUS binary_sha256 (host-specific, observed not authored), PLUS
// backs_claim (which claim_ledger claim it substantiates) and an optional
// negative_control (Pillar 3 vacuity defense).
type IntentEntry struct {
	// ID is the stable check identifier a workflow's checks[] row references.
	ID string `json:"id"`
	// Argv is the exact command to sanction. argv[0] is the binary whose sha the
	// pin step OBSERVES on this host — the intent never carries the hash itself.
	Argv []string `json:"argv"`
	// PassWhen is the mechanical pass condition (only exit_zero honored).
	PassWhen string `json:"pass_when,omitempty"`
	// BacksClaim names the claim_ledger claim id this check substantiates, so the
	// reviewer of the (committed, hashless) intent can audit "what each one proves".
	BacksClaim string `json:"backs_claim"`
	// NegativeControl is the known-bad invocation the check MUST fail on; the
	// verifier runs it FIRST and voids the receipt if it unexpectedly passes.
	NegativeControl *NegativeControl `json:"negative_control,omitempty"`
	// Description is operator-facing prose; informational only.
	Description string `json:"description,omitempty"`
}

// NegativeControl is a known-bad input a check MUST fail on (Pillar 3). Running it
// FIRST and voiding the receipt when it PASSES catches `true`, an empty suite, and
// any unconditional exit-0 before the real run is ever trusted.
type NegativeControl struct {
	// Argv is the known-bad invocation. argv[0] should be the SAME binary as the
	// check it controls (the pin covers it); the inputs are mutated to be bad.
	Argv []string `json:"argv"`
	// MutationOf optionally names the paired passing fixture this control is a
	// one-line mutation of, so the control provably exercises the same code path
	// (the stretch rigor in the RFC; opt-in at experimental, a graduation bar).
	MutationOf string `json:"mutation_of,omitempty"`
	// Description is operator-facing prose; informational only.
	Description string `json:"description,omitempty"`
}

// AllowlistIntentFile is the on-disk JSON shape of the committed intent layer.
type AllowlistIntentFile struct {
	SchemaVersion string        `json:"schema_version"`
	Checks        []IntentEntry `json:"checks"`
}

// AllowlistIntent is the parsed, validated intent: sanctioned checks keyed by id.
type AllowlistIntent struct {
	entries map[string]IntentEntry
	order   []string
}

// LoadIntent reads and validates a committed intent file. It fails closed — and,
// critically, REJECTS any entry that carries a binary_sha256: a hash in the
// committed, portable layer is the exact non-portable footgun this split exists to
// forbid (the hash belongs in the per-host pins, observed not authored).
func LoadIntent(path string) (*AllowlistIntent, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-curated, git-tracked intent path supplied by the lane packet
	if err != nil {
		return nil, fmt.Errorf("read verifier allowlist intent %q: %w", path, err)
	}
	return ParseIntent(raw)
}

// ParseIntent is the pure, side-effect-free half of LoadIntent (used by tests).
func ParseIntent(raw []byte) (*AllowlistIntent, error) {
	// Reject a stray binary_sha256 BEFORE structural validation, so the error names
	// the real mistake (a hash in the hashless layer) rather than a vague shape miss.
	var probe struct {
		Checks []map[string]json.RawMessage `json:"checks"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil {
		for idx, c := range probe.Checks {
			if _, hasHash := c["binary_sha256"]; hasHash {
				return nil, fmt.Errorf("verifier allowlist intent checks[%d] carries a binary_sha256: the committed intent layer is HASHLESS by design (a host-specific hash is non-portable); pin the bytes with `striatum verifier pin --host-here`, never by hand", idx)
			}
		}
	}

	var file AllowlistIntentFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse verifier allowlist intent: %w", err)
	}
	if strings.TrimSpace(file.SchemaVersion) != AllowlistIntentSchemaVersion {
		return nil, fmt.Errorf("verifier allowlist intent schema_version must be %q, got %q", AllowlistIntentSchemaVersion, file.SchemaVersion)
	}
	if len(file.Checks) == 0 {
		return nil, fmt.Errorf("verifier allowlist intent must declare at least one check")
	}
	entries := make(map[string]IntentEntry, len(file.Checks))
	order := make([]string, 0, len(file.Checks))
	for idx, entry := range file.Checks {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			return nil, fmt.Errorf("verifier allowlist intent checks[%d].id must be a non-empty string", idx)
		}
		if _, exists := entries[id]; exists {
			return nil, fmt.Errorf("verifier allowlist intent checks[%d].id %q is duplicated", idx, id)
		}
		// A builtin id is self-pinned from the registry, so an intent entry that
		// names one must NOT also be present in the operator allowlist road; reject
		// it here so the two roads never collide on one id.
		if IsBuiltinID(id) {
			return nil, fmt.Errorf("verifier allowlist intent checks[%d].id %q is a reserved builtin id; builtins are resolved from the in-binary registry, never sanctioned in the operator intent", idx, id)
		}
		if len(entry.Argv) == 0 || strings.TrimSpace(entry.Argv[0]) == "" {
			return nil, fmt.Errorf("verifier allowlist intent check %q must declare a non-empty argv", id)
		}
		if strings.TrimSpace(entry.BacksClaim) == "" {
			return nil, fmt.Errorf("verifier allowlist intent check %q must name the claim it backs (backs_claim); a sanctioned check exists to substantiate a specific claim", id)
		}
		passWhen := strings.TrimSpace(entry.PassWhen)
		if passWhen == "" {
			passWhen = passWhenExitZero
		}
		if passWhen != passWhenExitZero {
			return nil, fmt.Errorf("verifier allowlist intent check %q has unsupported pass_when %q (only %q is honored in this slice)", id, passWhen, passWhenExitZero)
		}
		// Pillar 3 (vacuity): every sanctioned external check on the road to VERIFIED
		// MUST declare a negative_control — a known-bad it MUST fail on — so a vacuous
		// `true` can never silently substantiate a claim. Builtins (capped at ASSERTED)
		// are exempt and never reach here (rejected as reserved ids above).
		if entry.NegativeControl == nil || len(entry.NegativeControl.Argv) == 0 || strings.TrimSpace(entry.NegativeControl.Argv[0]) == "" {
			return nil, fmt.Errorf("verifier allowlist intent check %q must declare a non-empty negative_control.argv (a known-bad the check MUST fail on); a sanctioned check with no negative control can be vacuous and lie green", id)
		}
		entry.ID = id
		entry.PassWhen = passWhen
		entries[id] = entry
		order = append(order, id)
	}
	return &AllowlistIntent{entries: entries, order: order}, nil
}

// IDs returns the sanctioned check ids in declaration order.
func (in *AllowlistIntent) IDs() []string {
	out := make([]string, len(in.order))
	copy(out, in.order)
	return out
}

// Entry returns the intent entry for an id (ok=false if not sanctioned).
func (in *AllowlistIntent) Entry(id string) (IntentEntry, bool) {
	e, ok := in.entries[strings.TrimSpace(id)]
	return e, ok
}

// ResolvedStatus is the three-valued status of a sanctioned external check after
// joining intent ⋈ pins ⋈ attestations. It is the new intermediate rungs the RFC
// adds BETWEEN "we ran a check" and "a human stood behind the bytes".
type ResolvedStatus string

const (
	// StatusNamedUnpinned — sanctioned in intent, but no matching pin on this host.
	// The check CANNOT be executed (no sha to bind against); it caps at DESIGNED.
	StatusNamedUnpinned ResolvedStatus = "named_but_unpinned"
	// StatusPinnedUnattested — bytes observed and pinned, but no operator
	// attestation. The check can RUN, but its claim caps at ASSERTED: nobody with
	// an unforgeable principal has stood behind these bytes.
	StatusPinnedUnattested ResolvedStatus = "pinned_but_unattested"
	// StatusVerifiable — sanctioned AND pinned AND operator-attested. Only now may
	// the two-signal sealed receipt promote the claim to VERIFIED.
	StatusVerifiable ResolvedStatus = "verifiable"
)

// Unpinned is the TYPED sentinel for a NAMED-but-unpinned check — never a magic
// string, so a typo can never be mistaken for a real sha. It names the host whose
// pins are missing, the literal fix command, and a JSON pointer at the offending
// entry, so `workflow validate` / `run start` can fail closed with an actionable
// message and the lane can REFUSE on the sentinel rather than execute.
type Unpinned struct {
	HostFingerprint string `json:"host_fingerprint"`
	FixCommand      string `json:"fix_command"`
	EntryPointer    string `json:"entry_pointer"`
}

// ResolvedCheck is one sanctioned external check after the join. For VERIFIABLE
// it carries the resolved pin (the exact bytes); for NAMED-but-unpinned it carries
// the typed Unpinned sentinel.
type ResolvedCheck struct {
	ID       string
	Status   ResolvedStatus
	Intent   IntentEntry
	Pin      *AllowlistEntry
	Attested bool
	Unpinned *Unpinned
}

// JoinIntentPins is the PURE three-valued join the lane runs at `verifier run`
// resolution time. It executes NOTHING — it only decides which rung each
// sanctioned external check is ELIGIBLE for, given the per-host pins and the
// operator attestations. A check is VERIFIABLE only when all three hold:
// sanctioned in intent, pinned on this host, AND attested against the pinned sha
// (re-pinning new bytes invalidates a stale attestation, since the attest set is
// keyed by id→sha). pins/attest may be nil (no pins file / no attestations yet).
func JoinIntentPins(intent *AllowlistIntent, pins *Allowlist, attest *Attestations, hostFingerprint string) []ResolvedCheck {
	out := make([]ResolvedCheck, 0, len(intent.order))
	for i, id := range intent.order {
		entry := intent.entries[id]
		rc := ResolvedCheck{ID: id, Intent: entry}
		var pin *AllowlistEntry
		if pins != nil {
			if p, ok := pins.entries[id]; ok {
				cp := p
				pin = &cp
			}
		}
		if pin == nil {
			rc.Status = StatusNamedUnpinned
			rc.Unpinned = &Unpinned{
				HostFingerprint: hostFingerprint,
				FixCommand:      "striatum verifier pin --host-here",
				EntryPointer:    fmt.Sprintf("/checks/%d", i),
			}
			out = append(out, rc)
			continue
		}
		rc.Pin = pin
		rc.Attested = attest.Covers(id, pin.BinarySHA256)
		if rc.Attested {
			rc.Status = StatusVerifiable
		} else {
			rc.Status = StatusPinnedUnattested
		}
		out = append(out, rc)
	}
	return out
}

// HostFingerprint derives the per-host+arch key the pins file is named by. It is
// a short, stable, non-secret tag: a hostname digest prefix plus GOOS/GOARCH, so
// pins from a different machine/arch never silently satisfy this host's checks.
func HostFingerprint() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "unknown-host"
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(host))))
	return fmt.Sprintf("%s-%s-%s", hex.EncodeToString(sum[:])[:8], runtime.GOOS, runtime.GOARCH)
}

// PinsFileName is the gitignored per-host pins filename for a given fingerprint.
func PinsFileName(fingerprint string) string {
	return fmt.Sprintf("allowlist.pins.%s.json", fingerprint)
}

// AttestFileName is the per-host attestation sidecar filename. Attestations live
// in a SEPARATE artifact (not the lane-written pins file) so the trust signal
// never sits in a path the verified lane writes; the operator-token `attest` verb
// is the only sanctioned writer.
func AttestFileName(fingerprint string) string {
	return fmt.Sprintf("allowlist.pins.%s.attest.json", fingerprint)
}

// PinsAreFilled reports whether every sanctioned external check in the intent has
// a matching pin (i.e. the template is FILLED, not TEMPLATE_UNFILLED). It is a
// pure read used by `workflow validate` / `run start`.
func PinsAreFilled(intent *AllowlistIntent, pins *Allowlist) (filled bool, unpinned []string) {
	for _, id := range intent.order {
		if pins == nil {
			unpinned = append(unpinned, id)
			continue
		}
		if _, ok := pins.entries[id]; !ok {
			unpinned = append(unpinned, id)
		}
	}
	sort.Strings(unpinned)
	return len(unpinned) == 0, unpinned
}
