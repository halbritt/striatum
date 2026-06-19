package artifactcontracts

import (
	"strings"
	"testing"
)

// RFC 0094 §5 adjudicator-reliability extras (issue #402): the collaboration_ledger
// v1.1 additive fields that raise the anti-theater bar from STRUCTURAL presence to
// SEMANTIC correspondence (Check-B) and add the opt-in second-adjudicator gate.
//
// Three pieces under test, all enforced at the contract layer (publish-artifact /
// review.submit exit 6), which is the gate the whole collaboration-shape family
// shares:
//
//  1. Check-B rubric — per-entry `correspondence`
//     (landed_and_rebutted | landed_unrebutted | not_material) on challenge entries,
//     and the clearing rule: when Check-B is engaged a clearing verdict requires
//     >=1 landed_and_rebutted and NO landed_unrebutted.
//  2. v1.1 per-entry fields — `correspondence` (above) and `coverage`
//     (reconstructed | hallucinated | missed) for fog_of_war_review; plus the
//     top-level `adjudicators` list and `adjudication_mode`.
//  3. Second-adjudicator-on-disagreement gate — `adjudication_mode:
//     second_on_disagreement` requires >=2 distinct adjudicators recorded for a
//     clearing verdict (a contested clear cannot be expressed as a single-adjudicator
//     clear).

// checkBLedger builds a falsification_gate v1.1 ledger whose single challenge entry
// carries the given correspondence value, with the given verdict. The challenge is
// rebutted on the record so the structural (RFC 0093 V1) clearing requirement is met
// and only the Check-B semantic layer is exercised.
func checkBLedger(verdict, correspondence string) string {
	corr := ""
	if correspondence != "" {
		corr = "\n    correspondence: " + correspondence
	}
	return `---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
topic: "substance gate"
participants: ["sess_holder", "sess_falsifier", "sess_adjudicator"]
entries:
  - kind: claim
    by: sess_holder
    refs: ["dialogue:1"]
    text: "The proposal is ready."
  - kind: challenge
    by: sess_falsifier
    refs: ["dialogue:2"]
    text: "The proposal lacks migration evidence."` + corr + `
  - kind: rebuttal
    by: sess_holder
    refs: ["dialogue:3"]
    text: "The migration evidence is in the linked fixture."
verdict: "` + verdict + `"
rationale: "A material challenge landed and was rebutted on the record."
---

# Ledger
`
}

func TestCheckBCorrespondenceVocabulary(t *testing.T) {
	t.Run("landed_and_rebutted clears", func(t *testing.T) {
		if err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(checkBLedger("accept", "landed_and_rebutted"))); err != nil {
			t.Fatalf("landed_and_rebutted clearing ledger refused: %v", err)
		}
	})
	t.Run("standalone not_material clear is rejected", func(t *testing.T) {
		// A single not_material challenge alone does not satisfy ">=1 landed_and_rebutted";
		// a standalone not_material clear must be rejected.
		if err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(checkBLedger("accept", "not_material"))); err == nil {
			t.Fatal("a clear with only a not_material correspondence must be rejected (no landed_and_rebutted)")
		}
	})
	t.Run("unknown correspondence value is rejected", func(t *testing.T) {
		err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(checkBLedger("accept", "totally_invalid")))
		if err == nil || !strings.Contains(err.Error(), "correspondence") {
			t.Fatalf("invalid correspondence not rejected: %v", err)
		}
	})
}

func TestCheckBClearingRequiresLandedAndRebutted(t *testing.T) {
	// Check-B engaged (the challenge carries a correspondence judgment) but the only
	// judgment is landed_unrebutted: a clearing verdict must be refused (the
	// confident-non-rebuttal anti-theater case). needs_revision is fine.
	t.Run("landed_unrebutted blocks a clear", func(t *testing.T) {
		err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(checkBLedger("accept", "landed_unrebutted")))
		if err == nil || !strings.Contains(err.Error(), "landed_unrebutted") {
			t.Fatalf("landed_unrebutted clearing verdict not refused: %v", err)
		}
	})
	t.Run("landed_unrebutted with needs_revision validates", func(t *testing.T) {
		if err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(checkBLedger("needs_revision", "landed_unrebutted"))); err != nil {
			t.Fatalf("landed_unrebutted needs_revision refused: %v", err)
		}
	})
	t.Run("check-B not engaged keeps structural-only clearing", func(t *testing.T) {
		// No entry carries a correspondence value, so Check-B is not engaged and the
		// pre-existing structural V1 rule alone governs (claim+challenge+rebuttal present).
		if err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(checkBLedger("accept", ""))); err != nil {
			t.Fatalf("structural-only clearing ledger refused: %v", err)
		}
	})
	t.Run("correspondence only valid on challenge entries", func(t *testing.T) {
		// A correspondence judgment on a claim entry is a category error.
		payload := strings.Replace(
			checkBLedger("needs_revision", ""),
			`  - kind: claim
    by: sess_holder
    refs: ["dialogue:1"]
    text: "The proposal is ready."`,
			`  - kind: claim
    by: sess_holder
    refs: ["dialogue:1"]
    text: "The proposal is ready."
    correspondence: landed_and_rebutted`,
			1)
		err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(payload))
		if err == nil || !strings.Contains(err.Error(), "correspondence") {
			t.Fatalf("correspondence on a claim entry not rejected: %v", err)
		}
	})
}

func TestFogOfWarCoverageVocabulary(t *testing.T) {
	// `coverage` is the fog_of_war_review per-entry field scoring a reconstructed
	// constraint against the judge's ground truth.
	base := `---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "fog_of_war_review"
topic: "fog of war"
participants: ["sess_judge", "sess_reconstructor"]
entries:
  - kind: claim
    by: sess_reconstructor
    refs: ["dialogue:1"]
    text: "Reconstructed the rate-limit constraint."
  - kind: constraint
    by: sess_judge
    refs: ["dialogue:2"]
    text: "Rate-limit constraint scored against ground truth."
    coverage: COVERAGE_VALUE
verdict: "needs_revision"
rationale: "The judge scored reconstruction coverage."
---

# Ledger
`
	with := func(v string) []byte { return []byte(strings.Replace(base, "COVERAGE_VALUE", v, 1)) }
	t.Run("reconstructed validates", func(t *testing.T) {
		if err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", with("reconstructed")); err != nil {
			t.Fatalf("coverage reconstructed refused: %v", err)
		}
	})
	t.Run("hallucinated validates", func(t *testing.T) {
		if err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", with("hallucinated")); err != nil {
			t.Fatalf("coverage hallucinated refused: %v", err)
		}
	})
	t.Run("missed validates", func(t *testing.T) {
		if err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", with("missed")); err != nil {
			t.Fatalf("coverage missed refused: %v", err)
		}
	})
	t.Run("invalid coverage value rejected", func(t *testing.T) {
		err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", with("almost_there"))
		if err == nil || !strings.Contains(err.Error(), "coverage") {
			t.Fatalf("invalid coverage not rejected: %v", err)
		}
	})
}

func TestAdjudicationModeAndAdjudicators(t *testing.T) {
	ledger := func(mode, adjudicators, verdict string) string {
		modeLine := ""
		if mode != "" {
			modeLine = "adjudication_mode: " + mode + "\n"
		}
		adjLine := ""
		if adjudicators != "" {
			adjLine = "adjudicators: " + adjudicators + "\n"
		}
		return `---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
topic: "high-stakes gate"
participants: ["sess_holder", "sess_falsifier", "sess_adjudicator", "sess_adjudicator_2"]
entries:
  - kind: claim
    by: sess_holder
    refs: ["dialogue:1"]
    text: "The proposal is ready."
  - kind: challenge
    by: sess_falsifier
    refs: ["dialogue:2"]
    text: "The proposal lacks migration evidence."
  - kind: rebuttal
    by: sess_holder
    refs: ["dialogue:3"]
    text: "The migration evidence is in the linked fixture."
` + modeLine + adjLine + `verdict: "` + verdict + `"
rationale: "A material challenge landed and was rebutted on the record."
---

# Ledger
`
	}

	t.Run("single mode validates", func(t *testing.T) {
		if err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(ledger("single", `["sess_adjudicator"]`, "accept"))); err != nil {
			t.Fatalf("single adjudication_mode refused: %v", err)
		}
	})
	t.Run("invalid adjudication_mode names the enum", func(t *testing.T) {
		err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(ledger("triple_blind", `["sess_adjudicator"]`, "accept")))
		if err == nil || !strings.Contains(err.Error(), "second_on_disagreement") {
			t.Fatalf("invalid adjudication_mode did not name the enum: %v", err)
		}
	})
	t.Run("adjudicators must be a non-empty list of strings", func(t *testing.T) {
		err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(ledger("single", `[]`, "accept")))
		if err == nil {
			t.Fatal("empty adjudicators list accepted")
		}
	})
	t.Run("ledger without the new top-level fields still validates (additive)", func(t *testing.T) {
		if err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(ledger("", "", "accept"))); err != nil {
			t.Fatalf("ledger without adjudication fields refused: %v", err)
		}
	})
}

func TestSecondAdjudicatorOnDisagreementGate(t *testing.T) {
	ledger := func(adjudicators, verdict string) string {
		return `---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
topic: "high-stakes gate"
participants: ["sess_holder", "sess_falsifier", "sess_adjudicator", "sess_adjudicator_2"]
entries:
  - kind: claim
    by: sess_holder
    refs: ["dialogue:1"]
    text: "The proposal is ready."
  - kind: challenge
    by: sess_falsifier
    refs: ["dialogue:2"]
    text: "The proposal lacks migration evidence."
  - kind: rebuttal
    by: sess_holder
    refs: ["dialogue:3"]
    text: "The migration evidence is in the linked fixture."
adjudication_mode: second_on_disagreement
adjudicators: ` + adjudicators + `
verdict: "` + verdict + `"
rationale: "A material challenge landed and was rebutted on the record."
---

# Ledger
`
	}

	t.Run("two distinct adjudicators clears", func(t *testing.T) {
		if err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(ledger(`["sess_adjudicator", "sess_adjudicator_2"]`, "accept"))); err != nil {
			t.Fatalf("second_on_disagreement with two adjudicators refused: %v", err)
		}
	})
	t.Run("single adjudicator cannot clear in second_on_disagreement mode", func(t *testing.T) {
		err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(ledger(`["sess_adjudicator"]`, "accept")))
		if err == nil || !strings.Contains(err.Error(), "second_on_disagreement") {
			t.Fatalf("single-adjudicator clear in second_on_disagreement mode not refused: %v", err)
		}
	})
	t.Run("duplicate adjudicator does not satisfy the diversity requirement", func(t *testing.T) {
		err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(ledger(`["sess_adjudicator", "sess_adjudicator"]`, "accept")))
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "distinct") {
			t.Fatalf("duplicate adjudicator clear not refused: %v", err)
		}
	})
	t.Run("needs_revision does not require two adjudicators", func(t *testing.T) {
		// A contested clear is conservatively recorded as needs_revision; that verdict
		// must not itself require the second adjudicator be present.
		if err := ValidateFrontMatter("collaboration_ledger", "LEDGER.md", []byte(ledger(`["sess_adjudicator"]`, "needs_revision"))); err != nil {
			t.Fatalf("second_on_disagreement needs_revision refused: %v", err)
		}
	})
}
