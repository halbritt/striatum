package workflowauthoring

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileValidatePlanAndGraph(t *testing.T) {
	repo := t.TempDir()
	writeWorkflow(t, filepath.Join(repo, "workflow.json"), validWorkflow())

	workflow, sourcePath, err := LoadFile(repo, "workflow.json")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if sourcePath != "workflow.json" {
		t.Fatalf("sourcePath = %q", sourcePath)
	}

	plan, err := Plan(workflow)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	summary := plan["summary"].(map[string]any)
	if summary["jobs"] != 2 || summary["edges"] != 1 || summary["claim_steps"] != 2 {
		t.Fatalf("summary = %#v", summary)
	}
	claimOrder := plan["claim_order"].([]map[string]any)
	firstWave := claimOrder[0]["claimable"].([]map[string]any)
	if firstWave[0]["job_id"] != "draft" {
		t.Fatalf("claim_order = %#v", claimOrder)
	}

	graph, err := Graph(workflow, "json")
	if err != nil {
		t.Fatalf("Graph json: %v", err)
	}
	if graph["workflow_id"] != "go-authoring" {
		t.Fatalf("graph header = %#v", graph)
	}
	mermaid, err := Graph(workflow, "mermaid")
	if err != nil {
		t.Fatalf("Graph mermaid: %v", err)
	}
	source := mermaid["source"].(string)
	for _, needle := range []string{"flowchart TD", "draft<br/>draft author/codex", "-->|completed|"} {
		if !strings.Contains(source, needle) {
			t.Fatalf("mermaid source missing %q:\n%s", needle, source)
		}
	}
	dot, err := Graph(workflow, "dot")
	if err != nil {
		t.Fatalf("Graph dot: %v", err)
	}
	if !strings.Contains(dot["source"].(string), "digraph striatum_workflow") {
		t.Fatalf("dot source = %s", dot["source"])
	}
}

func TestLoadFileRejectsTraversalAndYaml(t *testing.T) {
	repo := t.TempDir()
	parent := filepath.Dir(repo)
	outside := filepath.Join(parent, "workflow.yaml")
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write outside workflow: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	if _, _, err := LoadFile(repo, "../workflow.yaml"); err == nil || !strings.Contains(err.Error(), "inside the repository") {
		t.Fatalf("LoadFile traversal error = %v", err)
	}
	writeWorkflow(t, filepath.Join(repo, "workflow.yaml"), validWorkflow())
	if _, _, err := LoadFile(repo, "workflow.yaml"); err == nil || !strings.Contains(err.Error(), "must be JSON") {
		t.Fatalf("LoadFile yaml error = %v", err)
	}
}

func TestValidateReturnsAuthoringErrors(t *testing.T) {
	workflow := validWorkflow()
	jobs := workflow["jobs"].([]any)
	review := jobs[1].(map[string]any)
	review["role_id"] = "missing"
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "unknown role") {
		t.Fatalf("Validate unknown role error = %v", err)
	}

	workflow = validWorkflow()
	jobs = workflow["jobs"].([]any)
	review = jobs[1].(map[string]any)
	review["expected_artifacts"] = []any{map[string]any{
		"logical_name": "review",
		"kind":         "finding",
		"path":         "../REVIEW.md",
		"required":     true,
	}}
	err = Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "invalid artifact path") {
		t.Fatalf("Validate artifact path error = %v", err)
	}
}

// TestValidatePanelQuorum verifies RFC 0135 P4 (#338, D214): panel_role defaults to
// gating, accepts gating|advisory on a review job, is rejected on a non-review job and
// for unknown values; max_gating_abstentions must be a non-negative whole number and
// defaults to 0 (behavior unchanged).
func TestValidatePanelQuorum(t *testing.T) {
	// Default: no panel_role declared → valid (defaults to gating).
	if err := Validate(validWorkflow()); err != nil {
		t.Fatalf("default workflow (no panel_role) rejected: %v", err)
	}

	// Explicit gating and advisory on a review job are valid.
	for _, role := range []string{"gating", "advisory"} {
		workflow := validWorkflow()
		review := workflow["jobs"].([]any)[1].(map[string]any)
		review["panel_role"] = role
		if err := Validate(workflow); err != nil {
			t.Fatalf("review job panel_role=%q rejected: %v", role, err)
		}
	}

	// An unknown panel_role value is rejected.
	workflow := validWorkflow()
	workflow["jobs"].([]any)[1].(map[string]any)["panel_role"] = "blocking"
	if err := Validate(workflow); err == nil || !strings.Contains(err.Error(), "unknown panel_role") {
		t.Fatalf("unknown panel_role error = %v", err)
	}

	// panel_role on a NON-review job is rejected.
	workflow = validWorkflow()
	workflow["jobs"].([]any)[0].(map[string]any)["panel_role"] = "gating"
	if err := Validate(workflow); err == nil || !strings.Contains(err.Error(), "non-review job") {
		t.Fatalf("panel_role on non-review job error = %v", err)
	}

	// max_gating_abstentions: zero (or absent) is valid without an opt-in; a negative
	// one is rejected as non-whole.
	workflow = validWorkflow()
	workflow["jobs"].([]any)[1].(map[string]any)["max_gating_abstentions"] = 0
	if err := Validate(workflow); err != nil {
		t.Fatalf("max_gating_abstentions=0 rejected: %v", err)
	}
	workflow = validWorkflow()
	workflow["jobs"].([]any)[1].(map[string]any)["max_gating_abstentions"] = -1
	if err := Validate(workflow); err == nil || !strings.Contains(err.Error(), "non-negative whole number") {
		t.Fatalf("negative max_gating_abstentions error = %v", err)
	}

	// D214 explicit opt-in: a non-zero budget WITHOUT allow_gating_abstentions is
	// rejected so the dangerous budget is a fixture-diff-visible author choice.
	workflow = validWorkflow()
	workflow["jobs"].([]any)[1].(map[string]any)["max_gating_abstentions"] = 1
	if err := Validate(workflow); err == nil || !strings.Contains(err.Error(), "allow_gating_abstentions") {
		t.Fatalf("budget>0 without opt-in error = %v", err)
	}

	// The same non-zero budget WITH allow_gating_abstentions: true is accepted.
	workflow = validWorkflow()
	review := workflow["jobs"].([]any)[1].(map[string]any)
	review["max_gating_abstentions"] = 1
	review["allow_gating_abstentions"] = true
	if err := Validate(workflow); err != nil {
		t.Fatalf("budget>0 with opt-in rejected: %v", err)
	}

	// allow_gating_abstentions must be a boolean.
	workflow = validWorkflow()
	workflow["jobs"].([]any)[1].(map[string]any)["allow_gating_abstentions"] = "yes"
	if err := Validate(workflow); err == nil || !strings.Contains(err.Error(), "allow_gating_abstentions must be a boolean") {
		t.Fatalf("non-boolean allow_gating_abstentions error = %v", err)
	}
}

// TestValidateCycleDebounceCohort verifies the RFC 0154 / D250 (#476) opt-in
// `cycles[].debounce_cohort` field: absent is the default sentinel (today's
// first-dissent route), "all" and a positive integer are accepted, and malformed
// values (an unknown string, zero, or a negative number) are rejected so a typo
// cannot silently disable the opt-in.
func TestValidateCycleDebounceCohort(t *testing.T) {
	setCohort := func(v any) map[string]any {
		workflow := validWorkflow()
		workflow["cycles"].([]any)[0].(map[string]any)["debounce_cohort"] = v
		return workflow
	}

	// Absent => valid (default sentinel; validWorkflow has no debounce_cohort).
	if err := Validate(validWorkflow()); err != nil {
		t.Fatalf("default workflow (no debounce_cohort) rejected: %v", err)
	}
	// Accepted values.
	for _, ok := range []any{"", "all", 1, 2, float64(3)} {
		if err := Validate(setCohort(ok)); err != nil {
			t.Fatalf("debounce_cohort=%v (%T) rejected: %v", ok, ok, err)
		}
	}
	// Rejected values.
	for _, bad := range []any{"quorum", "majority", 0, -1, float64(0), true} {
		if err := Validate(setCohort(bad)); err == nil || !strings.Contains(err.Error(), "debounce_cohort") {
			t.Fatalf("debounce_cohort=%v (%T): expected a debounce_cohort error, got %v", bad, bad, err)
		}
	}
}

// TestValidateLaneLaunchEnv verifies #223: a lane may declare path_prefix and
// command_env, and the validator enforces their shape and the control-plane
// boundary (no PATH or STRIATUM_-namespaced keys in command_env).
func TestValidateLaneLaunchEnv(t *testing.T) {
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{
		"adapter":     "process",
		"command":     []any{"agy", "--dangerously-skip-permissions"},
		"path_prefix": []any{"/opt/agy/bin"},
		"command_env": map[string]any{"AGY_HOME": "/opt/agy"},
	}
	if err := Validate(workflow); err != nil {
		t.Fatalf("valid launch env rejected: %v", err)
	}

	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{"adapter": "process", "command": []any{"true"}, "path_prefix": []any{"relative/bin"}}
	if err := Validate(workflow); err == nil || !strings.Contains(err.Error(), "absolute paths") {
		t.Fatalf("relative path_prefix error = %v", err)
	}

	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{"adapter": "process", "command": []any{"true"}, "command_env": map[string]any{"PATH": "/x"}}
	if err := Validate(workflow); err == nil || !strings.Contains(err.Error(), "must not set PATH") {
		t.Fatalf("command_env PATH error = %v", err)
	}

	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{"adapter": "process", "command": []any{"true"}, "command_env": map[string]any{"STRIATUM_MCP_TOKEN": "x"}}
	if err := Validate(workflow); err == nil || !strings.Contains(err.Error(), "STRIATUM_-namespaced") {
		t.Fatalf("command_env STRIATUM_ key error = %v", err)
	}
}

func TestRefuseAutonomousSharedCheckoutRepoWrite(t *testing.T) {
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{
		"adapter":              "process",
		"command":              []any{"codex"},
		"adapter_capabilities": map[string]any{"agent_loop": true},
	}
	err := RefuseAutonomousSharedCheckoutRepoWrite(workflow)
	if err == nil || !strings.Contains(err.Error(), "worktree_isolation: per_job") {
		t.Fatalf("expected autonomous repo-write isolation refusal, got %v", err)
	}

	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{
		"adapter":            "process",
		"command":            []any{"operator-script"},
		"worktree_isolation": "off",
	}
	if err := RefuseAutonomousSharedCheckoutRepoWrite(workflow); err != nil {
		t.Fatalf("plain operator-by-hand repo-write lane should remain warning-only: %v", err)
	}

	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{
		"adapter":            "process",
		"command":            []any{"codex"},
		"worktree_isolation": "per_job",
		"adapter_capabilities": map[string]any{
			"agent_loop": true,
		},
	}
	if err := RefuseAutonomousSharedCheckoutRepoWrite(workflow); err != nil {
		t.Fatalf("per-job isolated autonomous repo-write lane rejected: %v", err)
	}

	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{
		"adapter":                              "process",
		"command":                              []any{"codex"},
		"adapter_capabilities":                 map[string]any{"agent_loop": true},
		"allow_shared_checkout_repo_write":     true,
		"shared_checkout_repo_write_rationale": "interactive-human compatibility fixture",
	}
	if err := RefuseAutonomousSharedCheckoutRepoWrite(workflow); err != nil {
		t.Fatalf("shared-checkout compatibility override rejected: %v", err)
	}

	payload, err := Lint(workflow)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if !lintHasRule(payload, "shared_checkout_repo_write_override") {
		t.Fatalf("expected shared_checkout_repo_write_override warning, got %#v", payload["warnings"])
	}
	// #383 item 4: the validate-time warning must name the concurrency consequence
	// so it agrees with the run.start gate (concurrent_run_isolation_required) — a
	// shared-checkout repo-write lane passes validate but is refused at start when a
	// sibling run is active. The author should learn this at validate, not at start.
	finding, ok := lintFindingForRule(t, workflow, "shared_checkout_repo_write_override")
	if !ok {
		t.Fatalf("expected shared_checkout_repo_write_override finding")
	}
	msg, _ := finding["message"].(string)
	for _, want := range []string{"concurrent_run_isolation_required", "worktree_isolation: per_job"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("shared_checkout_repo_write_override message missing %q:\n%s", want, msg)
		}
	}
}

func TestValidateSharedCheckoutOverrideRequiresRationale(t *testing.T) {
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["codex"].(map[string]any)["allow_shared_checkout_repo_write"] = true
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "shared_checkout_repo_write_rationale") {
		t.Fatalf("expected missing shared_checkout_repo_write_rationale error, got %v", err)
	}
}

func TestLintOperatorContentNeutrality(t *testing.T) {
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["codex"].(map[string]any)["display_model"] = "Codex GPT-5"
	jobs := workflow["jobs"].([]any)
	jobs = append(jobs, map[string]any{
		"id":      "synthesis",
		"type":    "synthesis",
		"role_id": "author",
		"lane_id": "codex",
		"write_scope": map[string]any{
			"mode":            "review_only_artifact",
			"allowed_paths":   []any{"reviews/"},
			"forbidden_paths": []any{".striatum/"},
		},
		"expected_artifacts": []any{map[string]any{
			"logical_name": "synthesis",
			"kind":         "synthesis",
			"path":         "reviews/SYNTHESIS.md",
			"required":     true,
		}},
	})
	workflow["jobs"] = jobs
	workflow["edges"] = append(workflow["edges"].([]any), map[string]any{"from": "review", "to": "synthesis", "on": "completed"})
	payload, err := Lint(workflow)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if !lintHasRule(payload, "operator_content_role_model_overlap") {
		t.Fatalf("expected operator_content_role_model_overlap warning, got %#v", payload["warnings"])
	}

	workflow["operator_content_neutrality_override_rationale"] = "single-model fixture, accepted by operator"
	payload, err = Lint(workflow)
	if err != nil {
		t.Fatalf("Lint with override: %v", err)
	}
	if lintHasRule(payload, "operator_content_role_model_overlap") {
		t.Fatalf("override should suppress operator content overlap warning: %#v", payload["warnings"])
	}
}

func lintHasRule(payload map[string]any, rule string) bool {
	for _, warning := range payload["warnings"].([]map[string]any) {
		if warning["rule"] == rule {
			return true
		}
	}
	return false
}

func TestValidateRejectsInvalidLaneModel(t *testing.T) {
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{"adapter": "process", "command": []any{"true"}, "model": 123} // Invalid model type (int)
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "model must be a non-empty string") {
		t.Fatalf("Validate invalid lane model error = %v", err)
	}

	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{"adapter": "process", "command": []any{"true"}, "model": ""} // Invalid empty model
	err = Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "model must be a non-empty string") {
		t.Fatalf("Validate invalid lane model error = %v", err)
	}

	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{"adapter": "process", "command": []any{"true"}, "model": "gpt-4"} // Valid model
	err = Validate(workflow)
	if err != nil {
		t.Fatalf("Validate valid lane model error = %v", err)
	}
}

func TestValidateUsesSharedArtifactKindsAndLaneConstraints(t *testing.T) {
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{
		"adapter": "process",
		"command": []any{"true"},
		"constraints": map[string]any{
			"network":     "forbidden",
			"repo_scope":  "local_only",
			"transcripts": "off",
		},
		"required_enforcement": map[string]any{
			"network":     "advisory_strict",
			"repo_scope":  "advisory_strict",
			"transcripts": "enforced",
		},
	}
	jobs := workflow["jobs"].([]any)
	draft := jobs[0].(map[string]any)
	draft["expected_artifacts"] = []any{map[string]any{
		"logical_name": "brief",
		"kind":         "operator_brief",
		"path":         "src/BRIEF.md",
		"required":     true,
	}}
	if err := Validate(workflow); err != nil {
		t.Fatalf("Validate shared artifact kind and constraints: %v", err)
	}

	lanes["codex"].(map[string]any)["required_enforcement"] = map[string]any{"network": "enforced"}
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "adapter provides") {
		t.Fatalf("Validate invalid required_enforcement error = %v", err)
	}
}

func TestSharedResourcesValidatePlanAndLint(t *testing.T) {
	workflow := validWorkflow()
	jobs := workflow["jobs"].([]any)
	review := jobs[1].(map[string]any)
	review["parallel_group"] = "db_reviews"
	review["shared_resources"] = []any{map[string]any{
		"id":          "postgres:test-db",
		"description": "DB-backed validation fixture",
	}}
	jobs = append(jobs, map[string]any{
		"id":                     "db_review",
		"type":                   "review",
		"role_id":                "reviewer",
		"lane_id":                "codex",
		"parallel_group":         "db_reviews",
		"fresh_session_required": true,
		"shared_resources":       []any{"postgres:test-db"},
		"write_scope": map[string]any{
			"mode":            "review_only_artifact",
			"allowed_paths":   []any{"reviews/db/"},
			"forbidden_paths": []any{".striatum/"},
		},
		"expected_artifacts": []any{map[string]any{
			"logical_name": "db_review",
			"kind":         "finding",
			"path":         "reviews/db/REVIEW.md",
			"required":     true,
		}},
	})
	workflow["jobs"] = jobs
	workflow["edges"] = append(workflow["edges"].([]any), map[string]any{"from": "draft", "to": "db_review", "on": "completed"})

	if err := Validate(workflow); err != nil {
		t.Fatalf("Validate shared resources workflow: %v", err)
	}
	plan, err := Plan(workflow)
	if err != nil {
		t.Fatalf("Plan shared resources workflow: %v", err)
	}
	foundPlannedResource := false
	for _, step := range plan["claim_order"].([]map[string]any) {
		for _, planned := range step["claimable"].([]map[string]any) {
			if planned["job_id"] != "review" {
				continue
			}
			resources := planned["shared_resources"].([]map[string]any)
			if len(resources) == 1 && resources[0]["id"] == "postgres:test-db" && resources[0]["mode"] == "exclusive" {
				foundPlannedResource = true
			}
		}
	}
	if !foundPlannedResource {
		t.Fatalf("planned job did not surface shared_resources: %#v", plan["claim_order"])
	}
	if !lintRuleSet(t, workflow)["parallel_shared_resource_contention"] {
		t.Fatalf("expected parallel_shared_resource_contention warning")
	}
}

func TestValidateRejectsInvalidSharedResources(t *testing.T) {
	workflow := validWorkflow()
	jobs := workflow["jobs"].([]any)
	draft := jobs[0].(map[string]any)
	draft["shared_resources"] = []any{map[string]any{
		"id":   "postgres:test-db",
		"mode": "per_lane_namespace",
	}}
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "requires namespace") {
		t.Fatalf("Validate per_lane_namespace without namespace error = %v", err)
	}
}

func TestValidateRejectsInvalidInterrogationTargets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{
			name: "unknown target",
			mutate: func(workflow map[string]any) {
				interrogationConsumerJob(workflow)["interrogation_targets"] = []any{
					map[string]any{"workflow_job_id": "missing"},
				}
			},
			wantErr: "unknown interrogation target",
		},
		{
			name: "self target",
			mutate: func(workflow map[string]any) {
				interrogationConsumerJob(workflow)["interrogation_targets"] = []any{
					map[string]any{"workflow_job_id": "apply"},
				}
			},
			wantErr: "cannot target itself",
		},
		{
			name: "duplicate target",
			mutate: func(workflow map[string]any) {
				interrogationConsumerJob(workflow)["interrogation_targets"] = []any{
					map[string]any{"workflow_job_id": "draft"},
					map[string]any{"workflow_job_id": "draft"},
				}
			},
			wantErr: "duplicate interrogation target",
		},
		{
			name: "target not interrogable",
			mutate: func(workflow map[string]any) {
				jobs := workflow["jobs"].([]any)
				jobs[0].(map[string]any)["interrogable"] = false
				interrogationConsumerJob(workflow)["interrogation_targets"] = []any{
					map[string]any{"workflow_job_id": "draft"},
				}
			},
			wantErr: "does not declare interrogable",
		},
		{
			name: "target not upstream",
			mutate: func(workflow map[string]any) {
				jobs := workflow["jobs"].([]any)
				jobs = append(jobs, interrogationBuildJob("isolated"))
				workflow["jobs"] = jobs
				jobs[len(jobs)-1].(map[string]any)["interrogable"] = true
				interrogationConsumerJob(workflow)["interrogation_targets"] = []any{
					map[string]any{"workflow_job_id": "isolated"},
				}
			},
			wantErr: "must be reachable downstream",
		},
		{
			name: "chained interrogable consumer",
			mutate: func(workflow map[string]any) {
				consumer := interrogationConsumerJob(workflow)
				consumer["interrogable"] = true
				consumer["interrogation_targets"] = []any{
					map[string]any{"workflow_job_id": "draft"},
				}
			},
			wantErr: "cannot also declare interrogable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow := interrogationTargetWorkflow()
			tc.mutate(workflow)
			err := Validate(workflow)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateAcceptsReachableInterrogationTargets(t *testing.T) {
	workflow := interrogationTargetWorkflow()
	interrogationConsumerJob(workflow)["interrogation_targets"] = []any{
		map[string]any{"workflow_job_id": "draft", "required": true},
	}
	if err := Validate(workflow); err != nil {
		t.Fatalf("Validate reachable interrogation target: %v", err)
	}
}

func TestLintReportsInterrogationTargetWarnings(t *testing.T) {
	workflow := interrogationTargetWorkflow()
	interrogationConsumerJob(workflow)["interrogation_targets"] = []any{
		map[string]any{"workflow_job_id": "draft", "required": true, "extra": "warn"},
	}
	rules := lintRuleSet(t, workflow)
	for _, rule := range []string{
		"interrogation_target_unknown_field",
		"interrogation_target_missing_interrogate_capability",
	} {
		if !rules[rule] {
			t.Fatalf("lint rules missing %s: %#v", rule, rules)
		}
	}

	workflow = interrogationTargetWorkflow()
	jobs := workflow["jobs"].([]any)
	review := jobs[1].(map[string]any)
	review["interrogation_targets"] = []any{map[string]any{"workflow_job_id": "draft"}}
	if !lintRuleSet(t, workflow)["interrogation_target_redundant_direct_dependency"] {
		t.Fatalf("expected redundant direct-dependency warning")
	}

	workflow = interrogationTargetWorkflow()
	jobs = workflow["jobs"].([]any)
	for _, id := range []string{"target_2", "target_3", "target_4"} {
		target := interrogationBuildJob(id)
		target["interrogable"] = true
		jobs = append(jobs, target)
		workflow["edges"] = append(workflow["edges"].([]any), map[string]any{"from": id, "to": "apply", "on": "completed"})
	}
	workflow["jobs"] = jobs
	interrogationConsumerJob(workflow)["interrogation_targets"] = []any{
		map[string]any{"workflow_job_id": "draft"},
		map[string]any{"workflow_job_id": "target_2"},
		map[string]any{"workflow_job_id": "target_3"},
		map[string]any{"workflow_job_id": "target_4"},
	}
	if !lintRuleSet(t, workflow)["interrogation_target_count_high"] {
		t.Fatalf("expected high target-count warning")
	}
}

func TestLintAllowsDistinctSharedResourceNamespaces(t *testing.T) {
	workflow := validWorkflow()
	jobs := workflow["jobs"].([]any)
	review := jobs[1].(map[string]any)
	review["parallel_group"] = "db_reviews"
	review["shared_resources"] = []any{map[string]any{
		"id":        "postgres:test-db",
		"mode":      "per_lane_namespace",
		"namespace": "review_a",
	}}
	jobs = append(jobs, map[string]any{
		"id":                     "db_review",
		"type":                   "review",
		"role_id":                "reviewer",
		"lane_id":                "codex",
		"parallel_group":         "db_reviews",
		"fresh_session_required": true,
		"shared_resources": []any{map[string]any{
			"id":        "postgres:test-db",
			"mode":      "per_lane_namespace",
			"namespace": "review_b",
		}},
		"write_scope": map[string]any{
			"mode":            "review_only_artifact",
			"allowed_paths":   []any{"reviews/db/"},
			"forbidden_paths": []any{".striatum/"},
		},
		"expected_artifacts": []any{map[string]any{
			"logical_name": "db_review",
			"kind":         "finding",
			"path":         "reviews/db/REVIEW.md",
			"required":     true,
		}},
	})
	workflow["jobs"] = jobs
	workflow["edges"] = append(workflow["edges"].([]any), map[string]any{"from": "draft", "to": "db_review", "on": "completed"})

	if lintRuleSet(t, workflow)["parallel_shared_resource_contention"] {
		t.Fatalf("distinct per-lane shared resource namespaces must not warn")
	}
}

func TestLintReportsReviewDiversityFindingsWithFingerprints(t *testing.T) {
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{
		"adapter":       "process",
		"command":       []any{"true"},
		"display_model": "codex-gpt-5",
	}
	jobs := workflow["jobs"].([]any)
	draft := jobs[0].(map[string]any)
	draft["write_scope"] = map[string]any{
		"mode":            "repo_write",
		"repo_write":      true,
		"allowed_paths":   []any{"."},
		"forbidden_paths": []any{".striatum/"},
	}
	review := jobs[1].(map[string]any)
	delete(review, "fresh_session_required")

	payload, err := Lint(workflow)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if payload["valid"] != true {
		t.Fatalf("lint payload valid = %#v", payload["valid"])
	}
	warnings := payload["warnings"].([]map[string]any)
	rules := map[string]bool{}
	for _, warning := range warnings {
		rules[warning["rule"].(string)] = true
		if warning["fingerprint"] == "" {
			t.Fatalf("warning missing fingerprint: %#v", warning)
		}
	}
	for _, rule := range []string{
		"same_model_review_pair",
		"same_model_revision_cycle",
		"review_without_fresh_context",
		"broad_write_scope",
		"repo_write_without_worktree_isolation",
	} {
		if !rules[rule] {
			t.Fatalf("lint rules missing %s: %#v", rule, rules)
		}
	}
	coverage := payload["coverage"].(map[string]any)
	if coverage["level"] == "strong" {
		t.Fatalf("coverage unexpectedly strong: %#v", coverage)
	}
}

func TestLintReportsSameModelCollaborationAdjudicator(t *testing.T) {
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["codex"] = map[string]any{
		"adapter":       "process",
		"command":       []any{"true"},
		"display_model": "codex-gpt-5",
	}
	roles := workflow["roles"].(map[string]any)
	roles["adjudicator"] = map[string]any{}
	jobs := workflow["jobs"].([]any)
	jobs = append(jobs, map[string]any{
		"id":                     "adjudicate",
		"type":                   "review",
		"role_id":                "adjudicator",
		"lane_id":                "codex",
		"fresh_session_required": true,
		"write_scope": map[string]any{
			"mode":            "review_only_artifact",
			"allowed_paths":   []any{"reviews/adjudicator/"},
			"forbidden_paths": []any{".striatum/"},
		},
		"expected_artifacts": []any{map[string]any{
			"logical_name": "collaboration_ledger",
			"kind":         "collaboration_ledger",
			"path":         "reviews/adjudicator/COLLABORATION_LEDGER.md",
			"required":     true,
		}},
	})
	workflow["jobs"] = jobs
	edges := workflow["edges"].([]any)
	workflow["edges"] = append(edges, map[string]any{"from": "draft", "to": "adjudicate", "on": "completed"})

	payload, err := Lint(workflow)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	warnings := payload["warnings"].([]map[string]any)
	found := false
	for _, warning := range warnings {
		if warning["rule"] == "same_model_adjudicator_pair" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing same_model_adjudicator_pair warning: %#v", warnings)
	}
	coverage := payload["coverage"].(map[string]any)
	checks := coverage["checks"].([]map[string]any)
	independenceFailed := false
	for _, check := range checks {
		if check["id"] == "reviewer_independence" && check["passed"] == false {
			independenceFailed = true
		}
	}
	if !independenceFailed {
		t.Fatalf("coverage = %#v", coverage)
	}
}

func lintRuleSet(t *testing.T, workflow map[string]any) map[string]bool {
	t.Helper()
	payload, err := Lint(workflow)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if payload["valid"] != true {
		t.Fatalf("lint payload valid = %#v", payload["valid"])
	}
	rules := map[string]bool{}
	for _, warning := range payload["warnings"].([]map[string]any) {
		rules[warning["rule"].(string)] = true
	}
	return rules
}

func TestLintFlagsAgyOneShotPipeLane(t *testing.T) {
	// Shell-shim one-shot agy lane without agent_loop must be flagged.
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["agy"] = map[string]any{
		"adapter":       "process",
		"display_model": "Antigravity",
		"command":       []any{"sh", "-c", "IFS= read -r prompt; exec agy --dangerously-skip-permissions --print-timeout 30m --print \"$prompt\" </dev/null"},
	}
	if !lintRuleSet(t, workflow)["agy_one_shot_pipe_lane"] {
		t.Fatalf("expected agy_one_shot_pipe_lane warning for shell-shim one-shot agy lane")
	}

	// Direct argv one-shot agy lane must be flagged too.
	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["agy"] = map[string]any{
		"adapter": "process",
		"command": []any{"agy", "--print", "$prompt"},
	}
	if !lintRuleSet(t, workflow)["agy_one_shot_pipe_lane"] {
		t.Fatalf("expected agy_one_shot_pipe_lane warning for direct one-shot agy lane")
	}
}

func TestLintAllowsAgyAgentLoopAndOtherPipeLanes(t *testing.T) {
	// agy as an agent-loop lane must NOT be flagged.
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["agy"] = map[string]any{
		"adapter":              "process",
		"command":              []any{"agy", "--dangerously-skip-permissions"},
		"adapter_capabilities": map[string]any{"agent_loop": true},
	}
	if lintRuleSet(t, workflow)["agy_one_shot_pipe_lane"] {
		t.Fatalf("agy agent-loop lane must not be flagged as one-shot pipe")
	}

	// The agy warning must stay agy-specific. Deprecated Claude/Codex
	// one-shot commands are covered by their own refusal rules.
	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["claude_code"] = map[string]any{
		"adapter": "process",
		"command": []any{"claude", "--model", "claude-opus-4-7", "--print"},
	}
	lanes["codex"] = map[string]any{
		"adapter": "process",
		"command": []any{"sh", "-c", "IFS= read -r prompt; exec codex exec --model gpt-5.5 \"$prompt\" </dev/null"},
	}
	if lintRuleSet(t, workflow)["agy_one_shot_pipe_lane"] {
		t.Fatalf("non-agy one-shot pipe lanes must not fire the agy-specific warning")
	}
}

func TestWorkflowFingerprintMatchesCanonicalJSON(t *testing.T) {
	workflow := validWorkflow()
	fingerprint, err := WorkflowFingerprint(workflow)
	if err != nil {
		t.Fatalf("WorkflowFingerprint: %v", err)
	}
	raw, err := json.Marshal(workflow)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sum := sha256.Sum256(raw)
	if fingerprint != hex.EncodeToString(sum[:]) {
		t.Fatalf("fingerprint = %s, want canonical JSON sha", fingerprint)
	}
}

// --- GH #66: workflow validate must share run.prepare phase-shape rules ---

// phasedWorkflow returns a minimal valid two-phase v1.1 workflow:
//
//	phase "p1": build -> p1_synth (phase_synthesis)
//	phase "p2": follow -> p2_synth (phase_synthesis)
//
// with a single cross-phase edge p1_synth -> follow.
func phasedWorkflow() map[string]any {
	return map[string]any{
		"schema_version":   SchemaV11,
		"workflow_id":      "go-phased",
		"workflow_version": "1",
		"name":             "Go Phased",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/go-phased"},
		"coordinator":      map[string]any{"role_id": "author", "lane_id": "codex"},
		"lanes":            map[string]any{"codex": map[string]any{"adapter": "process", "command": []any{"true"}}},
		"roles":            map[string]any{"author": map[string]any{}, "synth": map[string]any{}},
		"context_docs":     []any{},
		"parallelism":      map[string]any{"mode": "declared", "max_active_jobs": 1},
		"phases": []any{
			map[string]any{"id": "p1", "name": "Phase 1", "synthesis_job_id": "p1_synth"},
			map[string]any{"id": "p2", "name": "Phase 2", "synthesis_job_id": "p2_synth"},
		},
		"edges": []any{
			map[string]any{"from": "build", "to": "p1_synth", "on": "completed"},
			map[string]any{"from": "p1_synth", "to": "follow", "on": "completed"},
			map[string]any{"from": "follow", "to": "p2_synth", "on": "completed"},
		},
		"cycles": []any{},
		"jobs": []any{
			phasedJob("build", "build", "p1", "author"),
			phasedSynthJob("p1_synth", "p1"),
			phasedJob("follow", "build", "p2", "author"),
			phasedSynthJob("p2_synth", "p2"),
		},
	}
}

func phasedJob(id, jobType, phaseID, roleID string) map[string]any {
	return map[string]any{
		"id":       id,
		"type":     jobType,
		"role_id":  roleID,
		"lane_id":  "codex",
		"phase_id": phaseID,
		"write_scope": map[string]any{
			"mode":            "repo_write",
			"allowed_paths":   []any{"src/"},
			"forbidden_paths": []any{".striatum/"},
		},
		"expected_artifacts": []any{map[string]any{
			"logical_name": id,
			"kind":         "handoff",
			"path":         "src/" + id + ".md",
			"required":     true,
		}},
	}
}

func phasedSynthJob(id, phaseID string) map[string]any {
	return map[string]any{
		"id":                     id,
		"type":                   "phase_synthesis",
		"role_id":                "synth",
		"lane_id":                "codex",
		"phase_id":               phaseID,
		"fresh_session_required": true,
		"write_scope": map[string]any{
			"mode":            "review_only_artifact",
			"allowed_paths":   []any{"reviews/"},
			"forbidden_paths": []any{".striatum/"},
		},
		"expected_artifacts": []any{map[string]any{
			"logical_name": id,
			"kind":         "synthesis",
			"path":         "reviews/" + id + ".md",
			"required":     true,
		}},
	}
}

func TestValidatePhasedWorkflowAccepted(t *testing.T) {
	if err := Validate(phasedWorkflow()); err != nil {
		t.Fatalf("Validate valid phased workflow: %v", err)
	}
	if err := ValidatePhaseShapes(phasedWorkflow()); err != nil {
		t.Fatalf("ValidatePhaseShapes valid phased workflow: %v", err)
	}
}

func TestValidateRejectsMultiplePhaseSynthesisJobs(t *testing.T) {
	workflow := phasedWorkflow()
	jobs := workflow["jobs"].([]any)
	// Add a second phase_synthesis job to phase p1.
	jobs = append(jobs, phasedSynthJob("p1_synth_dup", "p1"))
	workflow["jobs"] = jobs
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "has multiple phase_synthesis jobs") {
		t.Fatalf("Validate multiple synthesis error = %v", err)
	}
}

func TestValidateRejectsPhaseWithoutSynthesisJob(t *testing.T) {
	workflow := phasedWorkflow()
	jobs := workflow["jobs"].([]any)
	// Drop p2_synth by demoting it to a build job, leaving phase p2 without one.
	for _, item := range jobs {
		job := item.(map[string]any)
		if job["id"] == "p2_synth" {
			job["type"] = "build"
		}
	}
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "must declare exactly one phase_synthesis job") {
		t.Fatalf("Validate missing synthesis error = %v", err)
	}
}

func TestValidateRejectsEdgeTargetingLaterPhaseSynthesis(t *testing.T) {
	workflow := phasedWorkflow()
	edges := workflow["edges"].([]any)
	// Cross-phase edge from p1 synthesis directly into p2's synthesis job.
	workflow["edges"] = append(edges, map[string]any{"from": "p1_synth", "to": "p2_synth", "on": "completed"})
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "cannot target a later phase_synthesis job") {
		t.Fatalf("Validate edge-to-later-synthesis error = %v", err)
	}
}

func TestValidateRejectsCrossPhaseEdgeNotFromSynthesis(t *testing.T) {
	workflow := phasedWorkflow()
	edges := workflow["edges"].([]any)
	// Cross-phase edge originating from a non-synthesis job in p1.
	workflow["edges"] = append(edges, map[string]any{"from": "build", "to": "follow", "on": "completed"})
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "without using source phase") {
		t.Fatalf("Validate cross-phase non-synthesis edge error = %v", err)
	}
}

func TestValidateRejectsMismatchedSynthesisJobID(t *testing.T) {
	workflow := phasedWorkflow()
	phases := workflow["phases"].([]any)
	// Point phase p1's synthesis_job_id at a job that is not its synthesis job.
	phases[0].(map[string]any)["synthesis_job_id"] = "build"
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "synthesis_job_id") {
		t.Fatalf("Validate mismatched synthesis_job_id error = %v", err)
	}
}

func TestValidateRejectsPhaseJobInSchemaV1(t *testing.T) {
	workflow := validWorkflow() // SchemaV1
	jobs := workflow["jobs"].([]any)
	jobs[0].(map[string]any)["phase_id"] = "p1"
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "must not declare phase_id") {
		t.Fatalf("Validate v1 phase_id error = %v", err)
	}
}

// --- GH #99: a tracked workflow.json that isn't JSON gives a clear error ---

func TestLoadFileRejectsNonJSONWorkflowFile(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "workflow.json")
	markdown := "# Retired scaffold\n\nThis workflow has been superseded.\n"
	if err := os.WriteFile(path, []byte(markdown), 0o600); err != nil {
		t.Fatalf("write markdown workflow: %v", err)
	}
	_, _, err := LoadFile(repo, "workflow.json")
	if err == nil || !strings.Contains(err.Error(), "is not valid JSON") {
		t.Fatalf("LoadFile non-JSON error = %v", err)
	}
	if !strings.Contains(err.Error(), "workflow.json") {
		t.Fatalf("LoadFile non-JSON error should name the path: %v", err)
	}
}

func TestLoadFileRejectsMalformedJSONWorkflowFile(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "workflow.json")
	// Starts with '{' so it passes the object-shape gate but fails to decode.
	if err := os.WriteFile(path, []byte(`{"schema_version": `), 0o600); err != nil {
		t.Fatalf("write malformed workflow: %v", err)
	}
	_, _, err := LoadFile(repo, "workflow.json")
	if err == nil || !strings.Contains(err.Error(), "is not valid JSON") {
		t.Fatalf("LoadFile malformed JSON error = %v", err)
	}
}

// --- GH #114: workflow validate must reject duplicate top-level keys ---

func TestLoadFileRejectsDuplicateTopLevelKey(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "workflow.json")
	// Hand-craft JSON with a duplicate "lanes" key. encoding/json would
	// silently take last-wins, but our duplicate-key scanner must catch it
	// and return a clear error naming the duplicated key.
	raw := `{
  "schema_version": "striatum.workflow.v1",
  "workflow_id": "dup-key-test",
  "lanes": {"codex": {"adapter": "process", "command": ["true"]}},
  "lanes": {"other": {"adapter": "process", "command": ["true"]}},
  "workflow_version": "1",
  "name": "Dup Key Test",
  "branch": {"mode": "confirm", "suggested_name": "main"},
  "coordinator": {"role_id": "author", "lane_id": "codex"},
  "roles": {"author": {}},
  "context_docs": [],
  "parallelism": {"mode": "declared", "max_active_jobs": 1},
  "jobs": [],
  "edges": [],
  "cycles": []
}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, _, err := LoadFile(repo, "workflow.json")
	if err == nil {
		t.Fatal("LoadFile must reject duplicate top-level key but returned nil error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("LoadFile duplicate key error should mention 'duplicate': %v", err)
	}
	if !strings.Contains(err.Error(), "lanes") {
		t.Fatalf("LoadFile duplicate key error should name the duplicated key 'lanes': %v", err)
	}
}

func TestLoadFileAcceptsWorkflowWithNoDuplicateKeys(t *testing.T) {
	// Sanity-check: a well-formed workflow must still load successfully.
	repo := t.TempDir()
	writeWorkflow(t, filepath.Join(repo, "workflow.json"), validWorkflow())
	_, _, err := LoadFile(repo, "workflow.json")
	if err != nil {
		t.Fatalf("LoadFile valid workflow error = %v", err)
	}
}

// --- GH #97: document_only reviewer with empty inputs is inconsistent ---

func TestValidateRejectsDocumentOnlyReviewerWithoutInputs(t *testing.T) {
	workflow := validWorkflow()
	jobs := workflow["jobs"].([]any)
	review := jobs[1].(map[string]any)
	review["reviewer_access_scope"] = "document_only"
	// inputs intentionally absent/null.
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), `review_policy.access_scope "document_only" requires a non-empty inputs list`) {
		t.Fatalf("Validate document_only without inputs error = %v", err)
	}

	// Empty list is equally invalid.
	workflow = validWorkflow()
	jobs = workflow["jobs"].([]any)
	review = jobs[1].(map[string]any)
	review["reviewer_access_scope"] = "document_only"
	review["inputs"] = []any{}
	err = Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "requires a non-empty inputs list") {
		t.Fatalf("Validate document_only with empty inputs error = %v", err)
	}
}

func TestValidateAcceptsDocumentOnlyReviewerWithInputs(t *testing.T) {
	workflow := validWorkflow()
	jobs := workflow["jobs"].([]any)
	review := jobs[1].(map[string]any)
	review["reviewer_access_scope"] = "document_only"
	review["inputs"] = []any{map[string]any{"from": "draft", "logical_name": "draft"}}
	if err := Validate(workflow); err != nil {
		t.Fatalf("Validate document_only with inputs: %v", err)
	}
}

// --- GH #119: adapter_capabilities.agent_loop required when supervision uses pty_helper ---

// TestValidateRejectsAgentLoopSupervisionWithoutCapability verifies that a lane
// which enables PTY/tmux supervision (require_tmux: true or transport: pty_helper)
// without declaring adapter_capabilities.agent_loop: true is rejected at validate
// time. Without the flag laneUsesAgentLoop() returns false and the daemon skips
// the agent-loop executor wrapper, so the bootstrap prompt is never delivered and
// the lane stalls silently.
func TestValidateRejectsAgentLoopSupervisionWithoutCapability(t *testing.T) {
	// require_tmux: true without adapter_capabilities.agent_loop must fail.
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["agy"] = map[string]any{
		"adapter": "process",
		"command": []any{"agy", "--dangerously-skip-permissions"},
		"supervision": map[string]any{
			"require_tmux": true,
		},
		// adapter_capabilities intentionally absent
	}
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "adapter_capabilities.agent_loop") {
		t.Fatalf("Validate require_tmux without adapter_capabilities error = %v", err)
	}
	if !strings.Contains(err.Error(), "agy") {
		t.Fatalf("Validate error must name the offending lane: %v", err)
	}

	// transport: pty_helper without adapter_capabilities.agent_loop must also fail.
	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["claude_interactive"] = map[string]any{
		"adapter": "process",
		"command": []any{"claude", "--dangerously-skip-permissions"},
		"supervision": map[string]any{
			"transport": "pty_helper",
		},
		// adapter_capabilities intentionally absent
	}
	err = Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "adapter_capabilities.agent_loop") {
		t.Fatalf("Validate transport=pty_helper without adapter_capabilities error = %v", err)
	}
	if !strings.Contains(err.Error(), "claude_interactive") {
		t.Fatalf("Validate error must name the offending lane: %v", err)
	}
}

// TestValidateAcceptsAgentLoopLaneWithCapability verifies that a lane with PTY
// supervision and adapter_capabilities.agent_loop: true passes validation, and
// that a plain pipe lane with no supervision is also unaffected.
func TestValidateAcceptsAgentLoopLaneWithCapability(t *testing.T) {
	// require_tmux: true WITH adapter_capabilities.agent_loop must pass.
	workflow := validWorkflow()
	lanes := workflow["lanes"].(map[string]any)
	lanes["agy"] = map[string]any{
		"adapter": "process",
		"command": []any{"agy", "--dangerously-skip-permissions"},
		"adapter_capabilities": map[string]any{
			"agent_loop": true,
		},
		"supervision": map[string]any{
			"require_tmux": true,
		},
	}
	if err := Validate(workflow); err != nil {
		t.Fatalf("Validate agent-loop lane with capability and require_tmux: %v", err)
	}

	// transport: pty_helper WITH adapter_capabilities.agent_loop must pass.
	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["claude_interactive"] = map[string]any{
		"adapter": "process",
		"command": []any{"claude", "--dangerously-skip-permissions"},
		"adapter_capabilities": map[string]any{
			"agent_loop": true,
		},
		"supervision": map[string]any{
			"transport": "pty_helper",
		},
	}
	if err := Validate(workflow); err != nil {
		t.Fatalf("Validate agent-loop lane with capability and transport=pty_helper: %v", err)
	}

	// A plain pipe lane with no supervision at all is unaffected.
	workflow = validWorkflow()
	lanes = workflow["lanes"].(map[string]any)
	lanes["pipe_lane"] = map[string]any{
		"adapter": "process",
		"command": []any{"codex", "--model", "gpt-4o", "--print"},
	}
	if err := Validate(workflow); err != nil {
		t.Fatalf("Validate pipe lane without supervision: %v", err)
	}
}

// --- GH #119: expected_artifacts[].kind must be a known artifact kind ---

// TestValidateRejectsUnknownArtifactKind verifies that a job declaring an
// expected_artifacts entry with an unknown kind is rejected at validate time.
func TestValidateRejectsUnknownArtifactKind(t *testing.T) {
	workflow := validWorkflow()
	jobs := workflow["jobs"].([]any)
	draft := jobs[0].(map[string]any)
	draft["expected_artifacts"] = []any{map[string]any{
		"logical_name": "draft",
		"kind":         "totally_unknown_kind",
		"path":         "src/draft.md",
		"required":     true,
	}}
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "unknown artifact kind") {
		t.Fatalf("Validate unknown artifact kind error = %v", err)
	}
	if !strings.Contains(err.Error(), "totally_unknown_kind") {
		t.Fatalf("Validate error must name the invalid kind: %v", err)
	}
	if !strings.Contains(err.Error(), "draft") {
		t.Fatalf("Validate error must name the offending job: %v", err)
	}
}

// TestValidateRejectsMissingArtifactKind verifies that a job declaring an
// expected_artifacts entry without a kind field is rejected at validate time.
func TestValidateRejectsMissingArtifactKind(t *testing.T) {
	workflow := validWorkflow()
	jobs := workflow["jobs"].([]any)
	draft := jobs[0].(map[string]any)
	draft["expected_artifacts"] = []any{map[string]any{
		"logical_name": "draft",
		// kind intentionally absent
		"path":     "src/draft.md",
		"required": true,
	}}
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "missing required field") {
		t.Fatalf("Validate missing artifact kind error = %v", err)
	}
	if !strings.Contains(err.Error(), "draft") {
		t.Fatalf("Validate error must name the offending job: %v", err)
	}
}

// TestValidateAcceptsKnownArtifactKinds verifies that all well-known artifact
// kinds pass validation without requiring an exhaustive fixture per kind.
func TestValidateAcceptsKnownArtifactKinds(t *testing.T) {
	knownKinds := []string{
		"finding", "findings_ledger", "synthesis", "decision", "handoff",
		"marker", "other", "support_ledger", "action_item_ledger",
		"harness_improvement_proposal", "escalation", "operator_brief",
		"work_plan", "progress_note", "operator_report", "commit_request",
		"pr_request", "auto_finalize_gate_evidence", "collaboration_ledger",
		"patch_summary", "test_report", "prompt",
	}
	for _, kind := range knownKinds {
		workflow := validWorkflow()
		jobs := workflow["jobs"].([]any)
		draft := jobs[0].(map[string]any)
		draft["expected_artifacts"] = []any{map[string]any{
			"logical_name": "artifact",
			"kind":         kind,
			"path":         "src/artifact.md",
			"required":     true,
		}}
		if err := Validate(workflow); err != nil {
			t.Fatalf("Validate known kind %q failed: %v", kind, err)
		}
	}
}

func TestValidateAcceptsKnownArtifactPlacements(t *testing.T) {
	for _, placement := range []string{"blob_exhaust", "git_publication", "git_pointer_manifest"} {
		workflow := validWorkflow()
		jobs := workflow["jobs"].([]any)
		draft := jobs[0].(map[string]any)
		draft["expected_artifacts"] = []any{map[string]any{
			"logical_name": "artifact",
			"kind":         "synthesis",
			"path":         "src/artifact.md",
			"required":     true,
			"placement":    placement,
		}}
		if err := Validate(workflow); err != nil {
			t.Fatalf("Validate placement %q failed: %v", placement, err)
		}
	}
}

func TestValidateRejectsUnknownArtifactPlacement(t *testing.T) {
	workflow := validWorkflow()
	jobs := workflow["jobs"].([]any)
	draft := jobs[0].(map[string]any)
	draft["expected_artifacts"] = []any{map[string]any{
		"logical_name": "artifact",
		"kind":         "synthesis",
		"path":         "src/artifact.md",
		"required":     true,
		"placement":    "warehouse",
	}}
	err := Validate(workflow)
	if err == nil || !strings.Contains(err.Error(), "unknown artifact placement") {
		t.Fatalf("Validate unknown artifact placement error = %v", err)
	}
	if !strings.Contains(err.Error(), "warehouse") || !strings.Contains(err.Error(), "draft") {
		t.Fatalf("Validate error must name placement and job: %v", err)
	}
}

func writeWorkflow(t *testing.T, path string, workflow map[string]any) {
	t.Helper()
	raw, err := json.Marshal(workflow)
	if err != nil {
		t.Fatalf("marshal workflow: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
}

func validWorkflow() map[string]any {
	return map[string]any{
		"schema_version":   SchemaV1,
		"workflow_id":      "go-authoring",
		"workflow_version": "1",
		"name":             "Go Authoring",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/go-authoring"},
		"coordinator":      map[string]any{"role_id": "author", "lane_id": "codex"},
		"lanes":            map[string]any{"codex": map[string]any{"adapter": "process", "command": []any{"true"}}},
		"roles":            map[string]any{"author": map[string]any{}, "reviewer": map[string]any{}},
		"context_docs":     []any{},
		"parallelism":      map[string]any{"mode": "declared", "max_active_jobs": 1},
		"edges":            []any{map[string]any{"from": "draft", "to": "review", "on": "completed"}},
		"cycles":           []any{map[string]any{"from": "review", "to": "draft", "on_verdict": "needs_revision", "max_iterations": 1}},
		"jobs": []any{
			map[string]any{
				"id":      "draft",
				"type":    "draft",
				"role_id": "author",
				"lane_id": "codex",
				"write_scope": map[string]any{
					"mode":            "repo_write",
					"allowed_paths":   []any{"src/"},
					"forbidden_paths": []any{".striatum/"},
				},
				"expected_artifacts": []any{map[string]any{
					"logical_name": "draft",
					"kind":         "handoff",
					"path":         "src/draft.md",
					"required":     true,
				}},
			},
			map[string]any{
				"id":                     "review",
				"type":                   "review",
				"role_id":                "reviewer",
				"lane_id":                "codex",
				"fresh_session_required": true,
				"write_scope": map[string]any{
					"mode":            "review_only_artifact",
					"allowed_paths":   []any{"reviews/"},
					"forbidden_paths": []any{".striatum/"},
				},
				"expected_artifacts": []any{map[string]any{
					"logical_name": "review",
					"kind":         "finding",
					"path":         "reviews/REVIEW.md",
					"required":     true,
				}},
			},
		},
	}
}

func interrogationTargetWorkflow() map[string]any {
	workflow := validWorkflow()
	jobs := workflow["jobs"].([]any)
	draft := jobs[0].(map[string]any)
	draft["interrogable"] = true
	apply := interrogationBuildJob("apply")
	jobs = append(jobs, apply)
	workflow["jobs"] = jobs
	workflow["edges"] = append(workflow["edges"].([]any), map[string]any{"from": "review", "to": "apply", "on": "completed"})
	return workflow
}

func interrogationBuildJob(id string) map[string]any {
	return map[string]any{
		"id":      id,
		"type":    "build",
		"role_id": "author",
		"lane_id": "codex",
		"write_scope": map[string]any{
			"mode":            "repo_write",
			"allowed_paths":   []any{"src/"},
			"forbidden_paths": []any{".striatum/"},
		},
		"expected_artifacts": []any{map[string]any{
			"logical_name": id,
			"kind":         "handoff",
			"path":         "src/" + id + ".md",
			"required":     true,
		}},
	}
}

func interrogationConsumerJob(workflow map[string]any) map[string]any {
	for _, item := range workflow["jobs"].([]any) {
		job := item.(map[string]any)
		if job["id"] == "apply" {
			return job
		}
	}
	return nil
}
