package verifier

import (
	"strings"
	"testing"
)

const intentOK = `{
  "schema_version": "striatum.verifier_allowlist_intent.v1",
  "checks": [
    {"id": "mypy", "argv": ["mypy", "src"], "backs_claim": "types_sound",
     "negative_control": {"argv": ["mypy", "fixtures/known_type_error.py"]}}
  ]
}`

// TestParseIntentRejectsHash is the load-bearing portability guard: the committed
// intent layer is HASHLESS by design, so a stray binary_sha256 must be refused —
// it is the exact non-portable footgun the intent/pins split exists to forbid.
func TestParseIntentRejectsHash(t *testing.T) {
	withHash := `{"schema_version":"striatum.verifier_allowlist_intent.v1","checks":[` +
		`{"id":"mypy","argv":["mypy","src"],"backs_claim":"t","binary_sha256":"deadbeef",` +
		`"negative_control":{"argv":["mypy","bad.py"]}}]}`
	_, err := ParseIntent([]byte(withHash))
	if err == nil {
		t.Fatal("intent carrying a binary_sha256 must be rejected (the hashless layer must stay hashless)")
	}
	if !strings.Contains(err.Error(), "binary_sha256") {
		t.Fatalf("error must name the offending hash field, got: %v", err)
	}
}

// TestParseIntentRequiresNegativeControl enforces Pillar 3 at the parse boundary:
// a sanctioned external check on the road to VERIFIED MUST declare a negative
// control, so a vacuous `true` can never silently substantiate a claim.
func TestParseIntentRequiresNegativeControl(t *testing.T) {
	noControl := `{"schema_version":"striatum.verifier_allowlist_intent.v1","checks":[` +
		`{"id":"mypy","argv":["mypy","src"],"backs_claim":"t"}]}`
	if _, err := ParseIntent([]byte(noControl)); err == nil || !strings.Contains(err.Error(), "negative_control") {
		t.Fatalf("a check without a negative_control must be rejected, got: %v", err)
	}
}

// TestParseIntentRequiresBacksClaim — a sanctioned check exists to substantiate a
// specific claim; the reviewable intent must say which one.
func TestParseIntentRequiresBacksClaim(t *testing.T) {
	noBacks := `{"schema_version":"striatum.verifier_allowlist_intent.v1","checks":[` +
		`{"id":"mypy","argv":["mypy","src"],"negative_control":{"argv":["mypy","bad.py"]}}]}`
	if _, err := ParseIntent([]byte(noBacks)); err == nil || !strings.Contains(err.Error(), "backs_claim") {
		t.Fatalf("a check without backs_claim must be rejected, got: %v", err)
	}
}

// TestParseIntentRejectsBuiltinID — builtins are resolved from the in-binary
// registry and self-pinned; they must never be sanctioned in the operator intent
// (the two roads must not collide on one id).
func TestParseIntentRejectsBuiltinID(t *testing.T) {
	builtin := `{"schema_version":"striatum.verifier_allowlist_intent.v1","checks":[` +
		`{"id":"builtin:go-test","argv":["go","test"],"backs_claim":"t","negative_control":{"argv":["go","test","./bad"]}}]}`
	if _, err := ParseIntent([]byte(builtin)); err == nil || !strings.Contains(err.Error(), "builtin") {
		t.Fatalf("a builtin id in the intent must be rejected, got: %v", err)
	}
}

// TestJoinIntentPinsThreeValued is the heart of Pillar 1: the PURE join must
// resolve each sanctioned check to exactly one of the three rungs by whether it is
// pinned and attested — executing NOTHING.
func TestJoinIntentPinsThreeValued(t *testing.T) {
	intent, err := ParseIntent([]byte(`{"schema_version":"striatum.verifier_allowlist_intent.v1","checks":[
		{"id":"named_only","argv":["a"],"backs_claim":"c1","negative_control":{"argv":["a","bad"]}},
		{"id":"pinned_only","argv":["b"],"backs_claim":"c2","negative_control":{"argv":["b","bad"]}},
		{"id":"pinned_attested","argv":["c"],"backs_claim":"c3","negative_control":{"argv":["c","bad"]}}
	]}`))
	if err != nil {
		t.Fatalf("parse intent: %v", err)
	}
	// pins cover pinned_only and pinned_attested (not named_only).
	pins, err := ParseAllowlist([]byte(`{"schema_version":"striatum.verifier_allowlist.v1","checks":[
		{"id":"pinned_only","argv":["b"],"binary_sha256":"bb"},
		{"id":"pinned_attested","argv":["c"],"binary_sha256":"cc"}
	]}`))
	if err != nil {
		t.Fatalf("parse pins: %v", err)
	}
	// attest covers ONLY pinned_attested at the exact sha.
	attest, err := ParseAttestations([]byte(`{"schema_version":"striatum.verifier_allowlist_attest.v1","attestations":[
		{"id":"pinned_attested","binary_sha256":"cc"}
	]}`))
	if err != nil {
		t.Fatalf("parse attest: %v", err)
	}

	got := map[string]ResolvedStatus{}
	for _, rc := range JoinIntentPins(intent, pins, attest, "host-fp") {
		got[rc.ID] = rc.Status
	}
	want := map[string]ResolvedStatus{
		"named_only":      StatusNamedUnpinned,
		"pinned_only":     StatusPinnedUnattested,
		"pinned_attested": StatusVerifiable,
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("check %q: want status %q, got %q", id, w, got[id])
		}
	}

	// The NAMED-but-unpinned row must carry the typed sentinel naming the fix.
	for _, rc := range JoinIntentPins(intent, pins, attest, "host-fp") {
		if rc.ID == "named_only" {
			if rc.Unpinned == nil {
				t.Fatal("named_but_unpinned must carry a typed Unpinned sentinel, not a magic string")
			}
			if rc.Unpinned.FixCommand == "" || !strings.Contains(rc.Unpinned.FixCommand, "pin --host-here") {
				t.Errorf("sentinel must name the fix command, got %+v", rc.Unpinned)
			}
			if rc.Unpinned.HostFingerprint != "host-fp" {
				t.Errorf("sentinel must name the host fingerprint, got %q", rc.Unpinned.HostFingerprint)
			}
		}
	}
}

// TestJoinAttestationBindsToSha — a re-pin of DIFFERENT bytes must invalidate a
// stale attestation (the attest set is keyed by id→sha), dropping VERIFIABLE back
// to PINNED-but-unattested. Nobody stood behind THESE bytes.
func TestJoinAttestationBindsToSha(t *testing.T) {
	intent, _ := ParseIntent([]byte(`{"schema_version":"striatum.verifier_allowlist_intent.v1","checks":[
		{"id":"c","argv":["c"],"backs_claim":"x","negative_control":{"argv":["c","bad"]}}]}`))
	// pin now records sha "new" but the attestation blessed "old".
	pins, _ := ParseAllowlist([]byte(`{"schema_version":"striatum.verifier_allowlist.v1","checks":[
		{"id":"c","argv":["c"],"binary_sha256":"new"}]}`))
	attest, _ := ParseAttestations([]byte(`{"schema_version":"striatum.verifier_allowlist_attest.v1","attestations":[
		{"id":"c","binary_sha256":"old"}]}`))
	rcs := JoinIntentPins(intent, pins, attest, "fp")
	if len(rcs) != 1 || rcs[0].Status != StatusPinnedUnattested {
		t.Fatalf("a stale attestation (different sha) must NOT verify; got %+v", rcs)
	}
}

// TestPinsAreFilledUnpinned — the unfilled-template detector the validate/run-start
// hard-block reads. With no pins, every external check is unpinned (TEMPLATE_UNFILLED).
func TestPinsAreFilledUnpinned(t *testing.T) {
	intent, _ := ParseIntent([]byte(`{"schema_version":"striatum.verifier_allowlist_intent.v1","checks":[
		{"id":"a","argv":["a"],"backs_claim":"x","negative_control":{"argv":["a","bad"]}},
		{"id":"b","argv":["b"],"backs_claim":"y","negative_control":{"argv":["b","bad"]}}]}`))
	if filled, unpinned := PinsAreFilled(intent, nil); filled || len(unpinned) != 2 {
		t.Fatalf("no pins → not filled, both unpinned; got filled=%v unpinned=%v", filled, unpinned)
	}
	pins, _ := ParseAllowlist([]byte(`{"schema_version":"striatum.verifier_allowlist.v1","checks":[
		{"id":"a","argv":["a"],"binary_sha256":"aa"},
		{"id":"b","argv":["b"],"binary_sha256":"bb"}]}`))
	if filled, unpinned := PinsAreFilled(intent, pins); !filled || len(unpinned) != 0 {
		t.Fatalf("all pinned → filled; got filled=%v unpinned=%v", filled, unpinned)
	}
}

// TestAttestationsCoversNilSafe — a missing sidecar (nil) blesses nothing.
func TestAttestationsCoversNilSafe(t *testing.T) {
	var a *Attestations
	if a.Covers("x", "y") {
		t.Fatal("nil attestations must cover nothing (fail-closed)")
	}
	if EmptyAttestations().Covers("x", "y") {
		t.Fatal("empty attestations must cover nothing")
	}
}
