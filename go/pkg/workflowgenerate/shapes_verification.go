package workflowgenerate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/halbritt/striatum/go/pkg/verifier"
)

// RFC 0141 / D238 — the generatable verification_gate shape.
//
// This is the scaffold that makes "completion is a checked property" runnable
// out of the box. It compiles a build -> verify -> adjudicate -> commit graph
// modeled on examples/verification-gate-flow/workflow.json, but the verify job
// carries a REAL `type: verify` job type (owner bundle 0016): the lane runs
// `striatum verifier run` against the in-binary builtin checks (Unit A,
// pkg/verifier) and mints receipts, instead of the example's social/agent-only
// witness execution. The completion gate (RFC 0134, already shipped) reads the
// build job's claim_ledger downstream — this shape only feeds it.
//
// Three honesty properties are load-bearing and live HERE, in the generator:
//
//   - SEPARATION OF DUTIES. The hashless intent file (the sanctioned check set)
//     is emitted into the verify job's forbidden_paths, so a verified lane can
//     never author its own sanctioned set.
//   - FILLED vs TEMPLATE_UNFILLED. The generated workflow.json carries an
//     `allowlist_status` top-level field a downstream validator (Unit E) reads.
//     A builtin-only gate is FILLED (runnable with zero operator JSON); any
//     external check needs per-host pins, so it scaffolds TEMPLATE_UNFILLED.
//   - ASSERTED-by-default + the VERIFIED-only-builtin REFUSE. Builtins cap at
//     ASSERTED (they self-pin the striatum binary, not the tool). A gate whose
//     floor targets VERIFIED while every named check is a builtin is permanently
//     un-satisfiable, so the generator refuses to emit it.
const (
	// allowlistStatusFilled marks a generated gate that is runnable as-is: every
	// named check is a builtin (self-pinned), so there is no per-host pin to fill.
	allowlistStatusFilled = "FILLED"
	// allowlistStatusTemplateUnfilled marks a gate whose intent names external
	// checks that need `striatum verifier pin --host-here` before it can run.
	allowlistStatusTemplateUnfilled = "TEMPLATE_UNFILLED"

	// gateFloorAsserted is the default (RFC 0141 resolved Open Question): a
	// runnable-and-honest gate, VERIFIED reserved as an opt-in release tier.
	gateFloorAsserted = "asserted"
	// gateFloorVerified targets the VERIFIED rung; it is only satisfiable when at
	// least one named check is an external (operator-pinned-and-attested) check.
	gateFloorVerified = "verified"

	// verificationIntentRelPath is the intent file emitted under the scaffold root
	// (relative). It is also the path pushed onto the verify job's forbidden_paths.
	verificationIntentRelPath = "verification/allowlist.intent.json"
	// verificationGitignoreRelPath is the gitignore guarding the per-host pins.
	verificationGitignoreRelPath = "verification/.gitignore"
)

// compileVerificationGate compiles the RFC 0141 verification_gate shape: a
// phased (striatum.workflow.v1.1) build -> verify -> adjudicate -> commit graph
// whose verify job is a real `type: verify` job. It mirrors the hand-authored
// examples/verification-gate-flow/workflow.json job graph (a Verify phase gated
// by an adjudicating phase_synthesis, then a Commit phase) but substitutes the
// engine-executable verify job and pushes the sanctioned-set intent path onto
// that job's forbidden_paths (separation of duties).
//
// It returns jobs/edges/cycles/phases like the multi-phase/collaboration
// compilers; the shape-specific extras — the emitted intent template + .gitignore
// files and the workflow-level allowlist_status field — are threaded through
// Generate via verificationGateExtras / applyVerificationGateWorkflowFields,
// matching the per-shape branches already in Generate (e.g. divergent_ideation).
func compileVerificationGate(spec Spec) ([]map[string]any, []map[string]any, []map[string]any, []map[string]any, error) {
	// Validate the gate options up front so a permanently-un-satisfiable gate is
	// refused before any jobs are built (acceptance criterion).
	if _, err := verificationGateFloor(spec); err != nil {
		return nil, nil, nil, nil, err
	}
	if _, err := verificationChecks(spec); err != nil {
		return nil, nil, nil, nil, err
	}
	if err := refuseVerifiedOnlyBuiltinGate(spec); err != nil {
		return nil, nil, nil, nil, err
	}

	base := spec.ArtifactRoot
	authorLaneID := authorLane(spec)
	reviewerLaneID := reviewerLane(spec, 1)
	intentPath := spec.ScaffoldRoot + "/" + verificationIntentRelPath

	// build: ship the slice and publish the claim ledger naming capability claims.
	build := job(
		"build",
		"build",
		"Build slice and publish claim ledger",
		"builder",
		authorLaneID,
		base+"/build",
		"CLAIM_LEDGER.md",
		"claim_ledger",
		"claim_ledger",
		"claim_build",
		"Build the slice and publish a claim ledger. Every capability claim carries a status (VERIFIED|ASSERTED|DESIGNED) and an id the verify step can reference. Anything above DESIGNED names the check that substantiates it. Deferral is allowed but must appear as a DESIGNED row, never hidden in prose.",
	)
	build["phase_id"] = "verify"

	// verify: a REAL type=verify job. The lane runs `striatum verifier run`
	// against the sanctioned/builtin checks and mints receipts; it never authors
	// the sanctioned set (intent is in forbidden_paths below).
	verify := job(
		"verify",
		"verify",
		"Run sanctioned checks and mint receipts",
		"verifier",
		reviewerLaneID,
		base+"/verify",
		"RECEIPTS.md",
		"receipt",
		"verification_receipts",
		"verify_run",
		"Run `striatum verifier run` against the sanctioned checks (the builtin go-test/vet/build/anchor checks need zero operator JSON; external checks come from verification/allowlist.intent.json once pinned). Mint a receipt per check from the engine's exit codes — never from prose. Publish the receipts; a builtin receipt caps at ASSERTED, VERIFIED is reserved for an operator-pinned-and-attested external check.",
	)
	verify["phase_id"] = "verify"
	verify["fresh_session_required"] = true
	// Separation of duties: the verified lane may NEVER author its own sanctioned
	// set. The job() helper seeds forbidden_paths with [".striatum/"]; append the
	// intent path so the verify lane cannot rewrite which checks are sanctioned.
	appendForbiddenPath(verify, intentPath)

	// adjudicate: the Verify phase's phase_synthesis gate. It reads claim_ledger +
	// receipts and publishes the verdict. needs_revision routes back to build
	// (bounded intra-phase cycle), exactly like the example.
	adjudicate := job(
		"adjudicate",
		"phase_synthesis",
		"Adjudicate claims against receipts",
		"adjudicator",
		reviewerLaneID,
		base+"/adjudicate",
		"COLLABORATION_LEDGER_${cycle}.md",
		"collaboration_ledger",
		"collaboration_ledger_${cycle}",
		"adjudicate",
		"Read the claim ledger and the minted receipts and publish the collaboration ledger verdict. Rule: if ANY claim is stated above the status its receipt earns — VERIFIED with a missing/RED receipt, or completion language over an ASSERTED/DESIGNED row — record verdict needs_revision and name the offending claims. Otherwise accept.",
	)
	adjudicate["phase_id"] = "verify"
	adjudicate["fresh_session_required"] = true

	// commit: publish the cleared release only after an accepting verdict.
	commit := job(
		"commit_verified",
		"synthesis",
		"Commit receipt-cleared release",
		"committer",
		authorLaneID,
		base+"/commit/release",
		"VERIFIED_RELEASE.md",
		"synthesis",
		"verified_release",
		"commit_verified",
		"Publish the cleared release only after the collaboration ledger records an accepting verdict. Stamp every claim with its EARNED status and the receipt of record; no completion language survives above the receipted status.",
	)
	commit["phase_id"] = "commit"

	// final_summary: the Commit phase's phase_synthesis gate, summarizing the
	// cleared run (which claims shipped at which earned status, the receipts of
	// record). A phase needs exactly one phase_synthesis job plus a peer.
	finalSummary := job(
		"final_summary",
		"phase_synthesis",
		"Finalize verification run",
		"adjudicator",
		reviewerLaneID,
		base+"/commit/final",
		"FINAL_SUMMARY.md",
		"synthesis",
		"final_summary",
		"final_summary",
		"Summarize the cleared verification gate: which claims shipped at which earned status (VERIFIED|ASSERTED|DESIGNED), and the receipts of record. Do not re-run the checks; this is the closing record.",
	)
	finalSummary["phase_id"] = "commit"

	jobs := []map[string]any{build, verify, adjudicate, commit, finalSummary}
	edges := []map[string]any{
		{"from": "build", "to": "verify", "on": "completed"},
		{"from": "verify", "to": "adjudicate", "on": "completed"},
		// cross-phase: from the Verify phase's synthesis gate to the Commit phase.
		{"from": "adjudicate", "to": "commit_verified", "on": "completed"},
		{"from": "commit_verified", "to": "final_summary", "on": "completed"},
	}
	max, err := maxCycles(spec)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	cycles := []map[string]any{
		{"from": "adjudicate", "to": "build", "on_verdict": "needs_revision", "max_iterations": max},
	}
	phases := []map[string]any{
		{
			"id":               "verify",
			"name":             "Verify",
			"description":      "Build with a claim ledger, run the sanctioned checks to mint receipts, adjudicate claims against receipts.",
			"synthesis_job_id": "adjudicate",
		},
		{
			"id":               "commit",
			"name":             "Commit",
			"description":      "Publish only the receipt-cleared release, every claim stamped with its earned status.",
			"synthesis_job_id": "final_summary",
		},
	}
	return jobs, edges, cycles, phases, nil
}

// appendForbiddenPath appends a path to a job's write_scope.forbidden_paths,
// preserving the existing entries (job() seeds [".striatum/"]). It is a no-op if
// the path is already present.
func appendForbiddenPath(job map[string]any, path string) {
	scope, ok := job["write_scope"].(map[string]any)
	if !ok {
		return
	}
	existing, _ := scope["forbidden_paths"].([]string)
	for _, p := range existing {
		if p == path {
			return
		}
	}
	scope["forbidden_paths"] = append(append([]string{}, existing...), path)
}

// verificationGateFloor resolves the gate floor option (default asserted). The
// RFC's resolved Open Question makes ASSERTED the default: runnable-and-honest
// out of the box, with VERIFIED an opt-in release tier.
func verificationGateFloor(spec Spec) (string, error) {
	value := strings.ToLower(strings.TrimSpace(fmt.Sprint(defaultAny(spec.Options["gate_floor"], gateFloorAsserted))))
	switch value {
	case gateFloorAsserted, gateFloorVerified:
		return value, nil
	default:
		return "", genErr("gate_floor must be one of asserted, verified", "spec.options.gate_floor")
	}
}

// verificationChecks resolves the named checks for the gate. The default is the
// builtin go-test/vet/build trio (runnable with zero operator JSON). An explicit
// `checks` option (a JSON string array) names the checks the gate targets, which
// may mix builtins and external (operator-pinned) check ids.
func verificationChecks(spec Spec) ([]string, error) {
	raw, ok := spec.Options["checks"]
	if !ok {
		return defaultVerificationChecks(), nil
	}
	var checks []string
	// The CLI delivers --option values as raw strings, so accept a comma-separated
	// list (`--option checks=mypy,builtin:go-test`) in addition to a JSON string
	// array (from a spec file or programmatic caller).
	if s, isStr := raw.(string); isStr {
		for _, part := range strings.Split(s, ",") {
			if t := strings.TrimSpace(part); t != "" {
				checks = append(checks, t)
			}
		}
	} else {
		var err error
		checks, err = stringList(raw, "spec.options.checks")
		if err != nil {
			return nil, err
		}
	}
	if len(checks) == 0 {
		return nil, genErr("checks must be a non-empty list of check ids (comma-separated or a JSON string array)", "spec.options.checks")
	}
	for idx, id := range checks {
		if strings.TrimSpace(id) == "" {
			return nil, genErr("checks entries must be non-empty check ids", fmt.Sprintf("spec.options.checks[%d]", idx))
		}
	}
	return checks, nil
}

// defaultVerificationChecks is the zero-JSON builtin set: go build/test/vet.
// Builtins self-pin the striatum binary, so they are runnable with no operator
// allowlist and cap the gate at ASSERTED.
func defaultVerificationChecks() []string {
	return []string{"builtin:go-build", "builtin:go-test", "builtin:go-vet"}
}

// allExternalChecks reports the external (non-builtin) checks among the named
// set. A check is external iff it is NOT a builtin id (verifier.IsBuiltinID).
func externalChecks(checks []string) []string {
	out := []string{}
	for _, id := range checks {
		if !verifier.IsBuiltinID(id) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// refuseVerifiedOnlyBuiltinGate is the acceptance-criterion guard: a gate whose
// floor targets VERIFIED while every named check is a builtin is permanently
// un-satisfiable (builtins cap at ASSERTED on the self-pin), so the generator
// refuses to emit it. It points the operator at the fix: either keep the floor
// at ASSERTED (the runnable default) or sanction an external check.
func refuseVerifiedOnlyBuiltinGate(spec Spec) error {
	floor, err := verificationGateFloor(spec)
	if err != nil {
		return err
	}
	if floor != gateFloorVerified {
		return nil
	}
	checks, err := verificationChecks(spec)
	if err != nil {
		return err
	}
	if len(externalChecks(checks)) == 0 {
		return genErr(
			"verification_gate gate_floor=verified is un-satisfiable when every check is a builtin: builtins self-pin the striatum binary and cap at ASSERTED, never VERIFIED. Keep gate_floor=asserted (the runnable default), or name at least one external check in --option checks=[...] and sanction it in verification/allowlist.intent.json (pinned + attested).",
			"spec.options.gate_floor",
		)
	}
	return nil
}

// allowlistStatusForSpec decides the generated workflow's allowlist_status. A
// builtin-only gate is FILLED (runnable as-is, no per-host pin); any external
// check needs `striatum verifier pin --host-here`, so it scaffolds
// TEMPLATE_UNFILLED until the operator fills the per-host pins.
func allowlistStatusForSpec(spec Spec) (string, error) {
	checks, err := verificationChecks(spec)
	if err != nil {
		return "", err
	}
	if len(externalChecks(checks)) > 0 {
		return allowlistStatusTemplateUnfilled, nil
	}
	return allowlistStatusFilled, nil
}

// applyVerificationGateWorkflowFields stamps the verification_gate-specific
// top-level fields onto the generated workflow before validation. It is called
// from Generate for the verification_gate shape only.
func applyVerificationGateWorkflowFields(spec Spec, workflow map[string]any) error {
	status, err := allowlistStatusForSpec(spec)
	if err != nil {
		return err
	}
	workflow["allowlist_status"] = status
	// Declare the real repo-relative intent path so the validate / run-start
	// hard-block (verifier.EvaluateAllowlistTemplate) finds the sanctioned set under
	// the scaffold root (not the repo-root convention) when the gate is UNFILLED.
	workflow["intent_path"] = spec.ScaffoldRoot + "/" + verificationIntentRelPath
	// On the single-lane `local` fixture set, verify and the adjudicate gate share
	// one lane, so the same_model_adjudicator_pair lint (CLI-refused) would reject
	// the generated starter — exactly the documented legitimate override, matching
	// the hand-authored example. Record the inline acceptance so the builtin-only
	// default validates out of the box.
	if spec.LaneSet == "local" || spec.LaneSet == "" {
		workflow["allow_same_model_review_pairing"] = true
	}
	return nil
}

// verificationGateExtras returns the shape-specific scaffold files for the
// verification_gate shape: the hashless intent template (which VALIDATES against
// verifier.ParseIntent) and a .gitignore for the per-host pins/attestations. It
// is appended to the rendered file set in Generate.
func verificationGateExtras(spec Spec) ([]map[string]any, error) {
	intent, err := verificationIntentTemplate(spec)
	if err != nil {
		return nil, err
	}
	return []map[string]any{
		{"path": spec.ScaffoldRoot + "/" + verificationIntentRelPath, "content": intent},
		{"path": spec.ScaffoldRoot + "/" + verificationGitignoreRelPath, "content": verificationGitignoreContent()},
	}, nil
}

// verificationIntentTemplate renders the committed, HASHLESS intent file. It
// always declares ONE example external check carrying argv + backs_claim + a
// negative_control, so the file PARSES cleanly via verifier.ParseIntent (which
// requires every entry to name a backed claim and a non-empty negative_control,
// and rejects any binary_sha256 / builtin id). The builtin checks the default
// gate runs are NOT listed here — they are resolved from the in-binary registry.
// The operator edits this file to sanction real external checks; the example row
// is the template they replace.
func verificationIntentTemplate(spec Spec) (string, error) {
	checks, err := verificationChecks(spec)
	if err != nil {
		return "", err
	}
	external := externalChecks(checks)
	var entries []verifier.IntentEntry
	for _, id := range external {
		// Sanction each operator-named external check so the generated intent ⋈ gate
		// is coherent (the verify job references only sanctioned ids). argv /
		// backs_claim / negative_control are TEMPLATE placeholders the operator
		// replaces with the real command before pinning — the gate stays UNFILLED
		// (hard-blocked) until then.
		entries = append(entries, verifier.IntentEntry{
			ID:         id,
			Argv:       []string{id},
			PassWhen:   "exit_zero",
			BacksClaim: id + "-claim",
			NegativeControl: &verifier.NegativeControl{
				Argv: []string{id, "--striatum-negative-control"},
				// mutation_of is MANDATORY at the supported tier (RFC 0141 / D243 / #482):
				// the control must mutate a paired passing fixture so it provably exercises
				// the same code path. This is a TEMPLATE placeholder the operator replaces
				// with the real fixture path before pinning (the gate stays UNFILLED until).
				MutationOf:  "TEMPLATE-replace-with-paired-passing-fixture-path",
				Description: "TEMPLATE: a known-bad the check MUST fail on; replace with a one-line mutation of a paired passing fixture (mutation_of) so it exercises the same code path.",
			},
			Description: "TEMPLATE: replace argv with the real command for '" + id + "', set backs_claim to the claim_ledger claim it substantiates, and give a discriminating negative control. Then `striatum verifier pin --host-here` (never hand-type a sha) and `striatum verifier attest`.",
		})
	}
	if len(entries) == 0 {
		// Builtins-only default: keep one illustrative example so the committed file
		// teaches the external→VERIFIED road and always parses. It is NOT wired into
		// the runnable builtin gate, so it never blocks the default.
		entries = []verifier.IntentEntry{{
			ID:         "example-external-lint",
			Argv:       []string{"true"},
			PassWhen:   "exit_zero",
			BacksClaim: "example-claim",
			NegativeControl: &verifier.NegativeControl{
				Argv: []string{"false"},
				// mutation_of is MANDATORY at the supported tier (RFC 0141 / D243 / #482).
				MutationOf:  "fixtures/passing-example",
				Description: "A known-bad invocation the check MUST fail on; replace it with a one-line mutation of the paired passing fixture (mutation_of) so it exercises the same code path.",
			},
			Description: "TEMPLATE: replace with a real external check (argv), the claim_ledger claim id it backs, and a discriminating negative control. Pin its bytes with `striatum verifier pin --host-here` (never hand-type a sha). Builtin checks (builtin:go-test/vet/build) need no entry here.",
		}}
	}
	file := verifier.AllowlistIntentFile{
		SchemaVersion: verifier.AllowlistIntentSchemaVersion,
		Checks:        entries,
	}
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return "", err
	}
	// Defensive: the template must always parse — fail generation loudly rather
	// than emit an intent file the lane's verifier would reject at run time.
	if _, err := verifier.ParseIntent(body); err != nil {
		return "", fmt.Errorf("generated verifier intent template does not parse: %w", err)
	}
	return string(body) + "\n", nil
}

// verificationGitignoreContent ignores the per-host pins and the attestation
// sidecar: both are observed/operator-minted host-local facts that are NEVER
// committed (verifier.PinsFileName / AttestFileName produce these globs).
func verificationGitignoreContent() string {
	return strings.Join([]string{
		"# RFC 0141: per-host verifier pins + attestations are observed/operator-minted,",
		"# host-local, and never committed. The committed policy is allowlist.intent.json.",
		"allowlist.pins.*.json",
		"allowlist.pins.*.attest.json",
		"",
	}, "\n")
}
