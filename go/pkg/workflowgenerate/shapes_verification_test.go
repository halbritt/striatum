package workflowgenerate

import (
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/verifier"
	"github.com/halbritt/striatum/go/pkg/workflowauthoring"
)

// verificationRaw builds a minimal generator spec map for the verification_gate
// shape, mirroring divergentRaw's shape so the tests stay terse.
func verificationRaw(laneSet string, lanes map[string]any, options map[string]any) map[string]any {
	if options == nil {
		options = map[string]any{}
	}
	if lanes == nil {
		lanes = map[string]any{}
	}
	return map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            "verification_gate",
		"lane_set":         laneSet,
		"workflow_id":      "vg-test",
		"name":             "vg-test",
		"workflow_version": "2026-06-19",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/vg-test", "allow_dirty": false},
		"scaffold_root":    "docs/operator/workflows/vg-test",
		"artifact_root":    "docs/operator/artifacts/vg-test",
		"lanes":            lanes,
		"lane_modifiers":   []any{},
		"options":          options,
	}
}

func generateVerification(t *testing.T, options map[string]any) Generated {
	t.Helper()
	spec, err := SpecFromMap(verificationRaw("local", nil, options))
	if err != nil {
		t.Fatalf("SpecFromMap: %v", err)
	}
	gen, err := Generate(spec)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return gen
}

// TestVerificationGateGeneratesValidWorkflowWithRealVerifyJob is the core happy
// path: the default builtin-only gate generates a workflow that passes the same
// in-process validation `workflow validate` runs (ValidateWorkflow +
// ValidatePhaseShapes), the verify job carries type:verify with the intent path
// in forbidden_paths, and the gate is FILLED (runnable with no operator JSON).
func TestVerificationGateGeneratesValidWorkflowWithRealVerifyJob(t *testing.T) {
	gen := generateVerification(t, nil)

	// Same validation surface `workflow validate` / `run prepare` apply.
	if err := ValidateWorkflow(gen.Workflow); err != nil {
		t.Fatalf("ValidateWorkflow: %v", err)
	}
	if err := workflowauthoring.ValidatePhaseShapes(gen.Workflow); err != nil {
		t.Fatalf("ValidatePhaseShapes (run.prepare rules): %v", err)
	}

	if got := gen.Workflow["allowlist_status"]; got != allowlistStatusFilled {
		t.Fatalf("allowlist_status = %v, want %q (builtin-only gate is runnable)", got, allowlistStatusFilled)
	}

	jobs := jobsOf(t, gen)
	verify := jobByID(jobs, "verify")
	if verify == nil {
		t.Fatalf("verify job missing from generated workflow")
	}
	if got := verify["type"]; got != "verify" {
		t.Fatalf("verify job type = %v, want \"verify\"", got)
	}

	intentPath := "docs/operator/workflows/vg-test/" + verificationIntentRelPath
	scope, _ := verify["write_scope"].(map[string]any)
	forbidden, _ := scope["forbidden_paths"].([]string)
	if !containsString(forbidden, intentPath) {
		t.Fatalf("verify job forbidden_paths %v missing intent path %q (separation of duties)", forbidden, intentPath)
	}
	// The default gate names only builtins, so go-build/test/vet are runnable with
	// zero operator JSON. Sanity-check the option resolution agrees.
	checks, err := verificationChecks(Spec{Options: map[string]any{}})
	if err != nil {
		t.Fatalf("verificationChecks default: %v", err)
	}
	for _, id := range checks {
		if !verifier.IsBuiltinID(id) {
			t.Fatalf("default check %q should be a builtin", id)
		}
	}
}

// TestVerificationGateEmitsParsingIntentAndPinsGitignore asserts the shape emits
// the two extra scaffold files beyond workflow.json + role/prompt stubs: a
// hashless intent template that PARSES via verifier.ParseIntent, and a .gitignore
// that guards the per-host pins/attestations.
func TestVerificationGateEmitsParsingIntentAndPinsGitignore(t *testing.T) {
	gen := generateVerification(t, nil)

	intentPath := "docs/operator/workflows/vg-test/" + verificationIntentRelPath
	gitignorePath := "docs/operator/workflows/vg-test/" + verificationGitignoreRelPath

	var intentContent, gitignoreContent string
	for _, f := range gen.Files {
		switch f["path"] {
		case intentPath:
			intentContent, _ = f["content"].(string)
		case gitignorePath:
			gitignoreContent, _ = f["content"].(string)
		}
	}
	if intentContent == "" {
		t.Fatalf("intent template file %q not emitted", intentPath)
	}
	if gitignoreContent == "" {
		t.Fatalf("pins .gitignore file %q not emitted", gitignorePath)
	}

	// The committed intent template MUST validate against the verifier's parser
	// (backs_claim + non-empty negative_control on every entry, no binary_sha256,
	// no builtin id).
	intent, err := verifier.ParseIntent([]byte(intentContent))
	if err != nil {
		t.Fatalf("emitted intent template does not parse: %v", err)
	}
	if len(intent.IDs()) == 0 {
		t.Fatalf("emitted intent template declares no checks")
	}

	// The .gitignore guards both per-host artifacts.
	for _, glob := range []string{"allowlist.pins.*.json", "allowlist.pins.*.attest.json"} {
		if !strings.Contains(gitignoreContent, glob) {
			t.Fatalf(".gitignore missing %q glob; got:\n%s", glob, gitignoreContent)
		}
	}
}

// TestVerificationGateExternalCheckIsTemplateUnfilled asserts that naming an
// external (non-builtin) check scaffolds an unfilled gate (needs per-host pins),
// while the builtin-only default stays FILLED.
func TestVerificationGateExternalCheckIsTemplateUnfilled(t *testing.T) {
	gen := generateVerification(t, map[string]any{"checks": []any{"builtin:go-test", "my-external-lint"}})
	if got := gen.Workflow["allowlist_status"]; got != allowlistStatusTemplateUnfilled {
		t.Fatalf("allowlist_status = %v, want %q (an external check needs per-host pins)", got, allowlistStatusTemplateUnfilled)
	}
}

// TestVerificationGateRefusesVerifiedOnlyBuiltinGate is the acceptance-criterion
// guard: a gate whose floor targets VERIFIED while every named check is a builtin
// is permanently un-satisfiable (builtins cap at ASSERTED), so the generator
// REFUSES it. Naming an external check makes the same VERIFIED floor satisfiable.
func TestVerificationGateRefusesVerifiedOnlyBuiltinGate(t *testing.T) {
	spec, err := SpecFromMap(verificationRaw("local", nil, map[string]any{"gate_floor": "verified"}))
	if err != nil {
		t.Fatalf("SpecFromMap: %v", err)
	}
	_, err = Generate(spec)
	if err == nil {
		t.Fatalf("expected refusal: a VERIFIED floor with only builtin checks is un-satisfiable")
	}
	if !strings.Contains(err.Error(), "un-satisfiable") || !strings.Contains(err.Error(), "ASSERTED") {
		t.Fatalf("refusal message should explain builtins cap at ASSERTED; got: %v", err)
	}

	// The same VERIFIED floor with an external check is allowed.
	spec2, err := SpecFromMap(verificationRaw("local", nil, map[string]any{
		"gate_floor": "verified",
		"checks":     []any{"my-external-lint"},
	}))
	if err != nil {
		t.Fatalf("SpecFromMap (external): %v", err)
	}
	if _, err := Generate(spec2); err != nil {
		t.Fatalf("VERIFIED floor with an external check must generate: %v", err)
	}
}

// TestVerificationGateDefaultsToAssertedFloor asserts the RFC's resolved Open
// Question: the default generated gate floors at ASSERTED (runnable-and-honest),
// so a builtin-only gate generates without an explicit gate_floor option.
func TestVerificationGateDefaultsToAssertedFloor(t *testing.T) {
	floor, err := verificationGateFloor(Spec{Options: map[string]any{}})
	if err != nil {
		t.Fatalf("verificationGateFloor default: %v", err)
	}
	if floor != gateFloorAsserted {
		t.Fatalf("default gate_floor = %q, want %q", floor, gateFloorAsserted)
	}
	// And the default (no gate_floor option) generates cleanly.
	generateVerification(t, nil)
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
