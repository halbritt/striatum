package workflowgenerate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/halbritt/striatum/go/pkg/workflowauthoring"
)

func TestPreviewReturnsPlannedWritesWithoutWriting(t *testing.T) {
	repo := t.TempDir()
	generated := mustGenerate(t, map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            "review",
		"lane_set":         "local",
		"workflow_id":      "demo",
		"name":             "Demo",
		"workflow_version": "2026-05-17",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/demo", "allow_dirty": false},
		"scaffold_root":    "workflows/demo",
		"artifact_root":    "striatum/demo",
		"lanes":            map[string]any{},
		"options":          map[string]any{},
		"lane_modifiers":   []any{},
		"context_docs":     []any{},
	})
	planned, err := Preview(repo, generated)
	if err != nil {
		t.Fatal(err)
	}
	if len(planned) == 0 {
		t.Fatal("expected planned writes")
	}
	if planned[0]["status"] != "would_create" {
		t.Fatalf("first status = %#v", planned[0])
	}
	if _, err := os.Stat(filepath.Join(repo, "workflows", "demo", "workflow.json")); !os.IsNotExist(err) {
		t.Fatalf("preview wrote workflow.json or stat failed: %v", err)
	}
}

func TestWriteCreatesOnlySafeRepoRelativeTargets(t *testing.T) {
	repo := t.TempDir()
	generated := mustGenerate(t, map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            "minimal",
		"lane_set":         "local",
		"workflow_id":      "demo",
		"name":             "Demo",
		"workflow_version": "2026-05-17",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/demo", "allow_dirty": false},
		"scaffold_root":    "workflows/demo",
		"artifact_root":    "striatum/demo",
		"lanes":            map[string]any{},
		"options":          map[string]any{},
	})
	result, err := Write(repo, generated)
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "created" {
		t.Fatalf("status = %#v", result["status"])
	}
	if _, err := os.Stat(filepath.Join(repo, "workflows", "demo", "workflow.json")); err != nil {
		t.Fatalf("workflow.json not created: %v", err)
	}
	if _, err := Write(repo, generated); err == nil {
		t.Fatal("second write should refuse overwrite")
	}
}

func TestTraversalAndScratchPathsRejected(t *testing.T) {
	repo := t.TempDir()
	generated := Generated{
		Files:    []map[string]any{{"path": "../evil.md", "content": "bad\n"}},
		Metadata: map[string]any{"workflow_path": "../evil.md", "scaffold_root": ".."},
	}
	if _, err := Preview(repo, generated); err == nil || !strings.Contains(err.Error(), "path must not escape") {
		t.Fatalf("traversal preview error = %v", err)
	}
	generated.Files[0]["path"] = ".striatum/workflow.json"
	if _, err := Write(repo, generated); err == nil || !strings.Contains(err.Error(), ".git/.striatum") {
		t.Fatalf("scratch write error = %v", err)
	}
}

func TestMultiPhaseShapeEmitsV11PhasedGraph(t *testing.T) {
	spec := multiPhaseGeneratorSpec()
	generated, err := GenerateFromMap(spec)
	if err != nil {
		t.Fatal(err)
	}
	workflow := generated.Workflow
	if err := ValidateWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
	if workflow["schema_version"] != "striatum.workflow.v1.1" {
		t.Fatalf("schema_version = %#v", workflow["schema_version"])
	}
	phases := listFrom(workflow["phases"])
	if len(phases) != 2 {
		t.Fatalf("phases = %#v", workflow["phases"])
	}
	phase1 := mapFrom(phases[0])
	if phase1["id"] != "phase_1_design" || phase1["name"] != "Design" || phase1["description"] != "Parallel design tracks" || phase1["color"] != "#6b7280" || phase1["synthesis_job_id"] != "phase_1_design__synthesis" {
		t.Fatalf("first phase = %#v", phase1)
	}
	phase2 := mapFrom(phases[1])
	if phase2["id"] != "phase_2_build" || phase2["name"] != "Build" || phase2["synthesis_job_id"] != "phase_2_build__synthesis" {
		t.Fatalf("second phase = %#v", phase2)
	}

	jobs := jobsByID(workflow["jobs"])
	if jobs["phase_1_design__python__draft"]["phase_id"] != "phase_1_design" {
		t.Fatalf("phase_1_design python draft = %#v", jobs["phase_1_design__python__draft"])
	}
	if jobs["phase_1_design__python__draft"]["parallel_group"] != "phase_1_design:python" {
		t.Fatalf("phase_1_design python draft = %#v", jobs["phase_1_design__python__draft"])
	}
	if jobs["phase_1_design__synthesis"]["type"] != "phase_synthesis" || jobs["phase_1_design__synthesis"]["phase_id"] != "phase_1_design" {
		t.Fatalf("phase_1_design synthesis = %#v", jobs["phase_1_design__synthesis"])
	}
	if jobs["phase_2_build__synthesis"]["type"] != "phase_synthesis" {
		t.Fatalf("phase_2_build synthesis = %#v", jobs["phase_2_build__synthesis"])
	}

	for _, edge := range [][2]string{
		{"phase_1_design__synthesis", "phase_2_build__python__draft"},
		{"phase_1_design__synthesis", "phase_2_build__docs__draft"},
		{"phase_1_design__python__draft", "phase_1_design__synthesis"},
	} {
		if !hasEdge(workflow["edges"], edge[0], edge[1]) {
			t.Fatalf("missing edge %v in %#v", edge, workflow["edges"])
		}
	}
}

func TestConstrainedModifierDeclaresOperatorMode(t *testing.T) {
	generated := mustGenerate(t, map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            "minimal",
		"lane_set":         "local",
		"workflow_id":      "constrained-demo",
		"name":             "Constrained Demo",
		"workflow_version": "2026-06-04",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/constrained-demo", "allow_dirty": false},
		"scaffold_root":    "workflows/constrained-demo",
		"artifact_root":    "striatum/constrained-demo",
		"lanes":            map[string]any{},
		"options":          map[string]any{},
		"lane_modifiers":   []any{"constrained"},
		"context_docs":     []any{},
	})
	if generated.Workflow["operator_mode"] != "constrained" {
		t.Fatalf("operator_mode = %#v", generated.Workflow["operator_mode"])
	}
}

func TestMultiPhaseInvalidCasesCarryFieldPath(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(map[string]any)
		fieldPath string
	}{
		{
			name: "missing phases",
			mutate: func(spec map[string]any) {
				spec["options"] = map[string]any{}
			},
			fieldPath: "spec.options.phases",
		},
		{
			name: "duplicate phase id",
			mutate: func(spec map[string]any) {
				phases := listFrom(mapFrom(spec["options"])["phases"])
				mapFrom(phases[1])["id"] = "phase_1_design"
			},
			fieldPath: "spec.options.phases[1].id",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := multiPhaseGeneratorSpec()
			tc.mutate(spec)
			_, err := GenerateFromMap(spec)
			if err == nil {
				t.Fatal("expected error")
			}
			var genError *Error
			if !errors.As(err, &genError) {
				t.Fatalf("error type = %T, %v", err, err)
			}
			if genError.FieldPath != tc.fieldPath {
				t.Fatalf("field path = %q, want %q; err = %v", genError.FieldPath, tc.fieldPath, err)
			}
		})
	}
}

func TestImplementationPanelShapeUsesRoleAndAdversaryPacks(t *testing.T) {
	spec := implementationPanelGeneratorSpec()
	generated, err := GenerateFromMap(spec)
	if err != nil {
		t.Fatal(err)
	}
	workflow := generated.Workflow
	if err := ValidateWorkflow(workflow); err != nil {
		t.Fatal(err)
	}

	jobs := jobsByID(workflow["jobs"])
	if _, ok := jobs["propose_option_a"]; !ok {
		t.Fatalf("missing propose_option_a in %#v", jobs)
	}
	if _, ok := jobs["propose_option_b"]; !ok {
		t.Fatalf("missing propose_option_b in %#v", jobs)
	}
	if _, ok := jobs["propose_option_c"]; ok {
		t.Fatalf("unexpected propose_option_c in %#v", jobs)
	}
	if jobs["score_option_a"]["review_posture"] != "custom:operator_experience" {
		t.Fatalf("score_option_a = %#v", jobs["score_option_a"])
	}
	if jobs["review_dissent"]["role_id"] != "dissent_reviewer" {
		t.Fatalf("review_dissent = %#v", jobs["review_dissent"])
	}
	if mapFrom(workflow["coordinator"])["role_id"] != "problem_framer" {
		t.Fatalf("coordinator = %#v", workflow["coordinator"])
	}
	if mapFrom(workflow["parallelism"])["max_active_jobs"] != 2 {
		t.Fatalf("parallelism = %#v", workflow["parallelism"])
	}
	if strings.Join(generated.Metadata["role_packs"].([]string), ",") != "implementation_panel_roles" {
		t.Fatalf("role_packs = %#v", generated.Metadata["role_packs"])
	}
	if strings.Join(generated.Metadata["adversary_packs"].([]string), ",") != "operator_ergonomics" {
		t.Fatalf("adversary_packs = %#v", generated.Metadata["adversary_packs"])
	}
	if strings.Join(generated.Metadata["score_dimensions"].([]string), ",") != "operator_experience,recovery,documentation" {
		t.Fatalf("score_dimensions = %#v", generated.Metadata["score_dimensions"])
	}
	if len(generated.Warnings) == 0 || !strings.Contains(generated.Warnings[0], "high-artifact") {
		t.Fatalf("warnings = %#v", generated.Warnings)
	}
}

func TestCollaborationShapesEmitSubstanceGateV11Graphs(t *testing.T) {
	for _, shape := range []string{"falsification_gate", "cross_examination"} {
		t.Run(shape, func(t *testing.T) {
			spec := collaborationGeneratorSpec(shape)
			generated, err := GenerateFromMap(spec)
			if err != nil {
				t.Fatal(err)
			}
			workflow := generated.Workflow
			if err := ValidateWorkflow(workflow); err != nil {
				t.Fatal(err)
			}
			if workflow["schema_version"] != WorkflowSchemaVersionV11 {
				t.Fatalf("schema_version = %#v", workflow["schema_version"])
			}
			jobs := jobsByID(workflow["jobs"])
			adjudicate := jobs["adjudicate"]
			if adjudicate["type"] != "phase_synthesis" || adjudicate["role_id"] != "adjudicator" {
				t.Fatalf("adjudicate job = %#v", adjudicate)
			}
			artifacts := listFrom(adjudicate["expected_artifacts"])
			if len(artifacts) != 1 || mapFrom(artifacts[0])["kind"] != "collaboration_ledger" {
				t.Fatalf("adjudicate artifacts = %#v", adjudicate["expected_artifacts"])
			}
			// Build finding 1: the adjudicator ledger is cycle-scoped so a
			// needs_revision re-run publishes to a distinct logical name + path
			// instead of deadlocking on the content-hash guard.
			ledger := mapFrom(artifacts[0])
			if ledger["logical_name"] != "collaboration_ledger_${cycle}" {
				t.Fatalf("adjudicate ledger logical_name = %#v", ledger["logical_name"])
			}
			if path, _ := ledger["path"].(string); !strings.Contains(path, "COLLABORATION_LEDGER_${cycle}.md") {
				t.Fatalf("adjudicate ledger path = %#v", ledger["path"])
			}
			if !hasEdge(workflow["edges"], "adjudicate", "commit_proposal") {
				t.Fatalf("missing adjudicate -> commit_proposal edge: %#v", workflow["edges"])
			}
			cycles := listFrom(workflow["cycles"])
			if len(cycles) != 1 || mapFrom(cycles[0])["from"] != "adjudicate" || mapFrom(cycles[0])["on_verdict"] != "needs_revision" {
				t.Fatalf("cycles = %#v", workflow["cycles"])
			}
			phases := listFrom(workflow["phases"])
			if len(phases) != 2 || mapFrom(phases[0])["synthesis_job_id"] != "adjudicate" {
				t.Fatalf("phases = %#v", workflow["phases"])
			}
			if generated.Metadata["shape_family"] != "collaboration" {
				t.Fatalf("metadata = %#v", generated.Metadata)
			}
			for id, job := range jobs {
				objective, _ := job["objective"].(string)
				lowerObjective := strings.ToLower(objective)
				if strings.Contains(lowerObjective, "stay live") || strings.Contains(lowerObjective, "interrogat") {
					t.Fatalf("job %q objective still promises live interrogation: %q", id, objective)
				}
			}
			switch shape {
			case "falsification_gate":
				if jobs["holder"]["interrogable"] != nil {
					t.Fatalf("holder should be a static artifact producer, got %#v", jobs["holder"])
				}
				if objective := fmt.Sprint(jobs["falsifier_1"]["objective"]); !strings.Contains(objective, "published holder proposal") {
					t.Fatalf("falsifier objective = %q", objective)
				}
			case "cross_examination":
				if jobs["author_draft"]["interrogable"] != nil {
					t.Fatalf("author_draft should be a static artifact producer, got %#v", jobs["author_draft"])
				}
				if objective := fmt.Sprint(jobs["cross_examiner_1"]["objective"]); !strings.Contains(objective, "published draft") {
					t.Fatalf("cross examiner objective = %q", objective)
				}
			}
			for _, file := range generated.Files {
				path := fmt.Sprint(mapFrom(file)["path"])
				if !strings.Contains(path, "/roles/") && !strings.Contains(path, "/prompts/") {
					continue
				}
				content := strings.ToLower(fmt.Sprint(mapFrom(file)["content"]))
				if strings.Contains(content, "stay live") || strings.Contains(content, "interrogat") {
					t.Fatalf("generated support file %q still promises live interrogation", path)
				}
			}
			promptName := "collaboration_holder.md"
			if shape == "cross_examination" {
				promptName = "collaboration_author_draft.md"
			}
			promptPath := "workflows/" + shape + "/prompts/" + promptName
			promptContent := ""
			for _, file := range generated.Files {
				if fmt.Sprint(mapFrom(file)["path"]) == promptPath {
					promptContent = fmt.Sprint(mapFrom(file)["content"])
					break
				}
			}
			if !strings.Contains(promptContent, "substance gate") ||
				!strings.Contains(promptContent, "## Deliverable") ||
				!strings.Contains(promptContent, "## Claims To Make Falsifiable") ||
				!strings.Contains(promptContent, "Output Contract") {
				t.Fatalf("generated collaboration prompt %s lacks topic-rich structure:\n%s", promptPath, promptContent)
			}
		})
	}
}

func TestCrossExaminationIsStructurallyIsomorphicToFalsificationGate(t *testing.T) {
	for _, includeScribe := range []bool{false, true} {
		t.Run(fmt.Sprintf("include_scribe=%v", includeScribe), func(t *testing.T) {
			falsification := collaborationGeneratorSpec("falsification_gate")
			falsificationOptions := mapFrom(falsification["options"])
			falsificationOptions["falsifier_count"] = 2
			falsificationOptions["max_revision_cycles"] = 2
			falsificationOptions["include_scribe"] = includeScribe

			crossExamination := collaborationGeneratorSpec("cross_examination")
			crossOptions := mapFrom(crossExamination["options"])
			// cross_examination derives its challenger count from reviewer_count:
			// reviewers 1..N-1 challenge, reviewer N adjudicates. Match the
			// two-challenger chain exercised by falsification_gate's fixture.
			crossOptions["reviewer_count"] = 3
			crossOptions["max_revision_cycles"] = 2
			crossOptions["include_scribe"] = includeScribe

			falsificationGenerated, err := GenerateFromMap(falsification)
			if err != nil {
				t.Fatalf("GenerateFromMap(falsification_gate): %v", err)
			}
			crossGenerated, err := GenerateFromMap(crossExamination)
			if err != nil {
				t.Fatalf("GenerateFromMap(cross_examination): %v", err)
			}

			falsificationGraph := collaborationStructuralGraph(falsificationGenerated.Workflow)
			crossGraph := collaborationStructuralGraph(crossGenerated.Workflow)
			if !reflect.DeepEqual(falsificationGraph, crossGraph) {
				t.Fatalf("cross_examination no longer shares falsification_gate's structural reliability fixture\nfalsification_gate: %#v\ncross_examination: %#v", falsificationGraph, crossGraph)
			}
		})
	}
}

func TestFalsificationGateCanIncludeScribeModifier(t *testing.T) {
	spec := collaborationGeneratorSpec("falsification_gate")
	mapFrom(spec["options"])["include_scribe"] = true
	generated, err := GenerateFromMap(spec)
	if err != nil {
		t.Fatal(err)
	}
	jobs := jobsByID(generated.Workflow["jobs"])
	if jobs["scribe_note"]["role_id"] != "scribe" {
		t.Fatalf("scribe_note = %#v", jobs["scribe_note"])
	}
	artifacts := listFrom(jobs["scribe_note"]["expected_artifacts"])
	if len(artifacts) != 1 || mapFrom(artifacts[0])["kind"] != "progress_note" {
		t.Fatalf("scribe artifact = %#v", jobs["scribe_note"]["expected_artifacts"])
	}
}

func TestCollaborationLocalFixtureAllowsSameModelCycle(t *testing.T) {
	spec := collaborationGeneratorSpec("cross_examination")
	spec["lane_set"] = "local"
	spec["lanes"] = map[string]any{}
	generated, err := GenerateFromMap(spec)
	if err != nil {
		t.Fatal(err)
	}
	cycles := listFrom(generated.Workflow["cycles"])
	if len(cycles) != 1 || mapFrom(cycles[0])["allow_same_model"] != true {
		t.Fatalf("cycles = %#v", generated.Workflow["cycles"])
	}
	// Build finding 4: a single-lane local fixture is inherently same-model, so
	// the generator records the inline same-model review-pairing override; the
	// CLI same_model_adjudicator_pair refusal would otherwise reject the starter
	// workflow.
	if generated.Workflow["allow_same_model_review_pairing"] != true {
		t.Fatalf("allow_same_model_review_pairing = %#v", generated.Workflow["allow_same_model_review_pairing"])
	}
}

func TestCollaborationShapeRejectsSingleAgentLaneSet(t *testing.T) {
	spec := collaborationGeneratorSpec("cross_examination")
	spec["lane_set"] = "single_agent"
	spec["lanes"] = map[string]any{
		"agent": map[string]any{"command": []any{"agent", "run"}, "display_model": "Agent"},
	}
	_, err := GenerateFromMap(spec)
	if err == nil || !strings.Contains(err.Error(), "collaboration shapes require") {
		t.Fatalf("error = %v", err)
	}
}

func TestAdjudicatedConstraintExtractionEmitsEightPhaseGraph(t *testing.T) {
	spec := adjudicatedConstraintExtractionGeneratorSpec()
	generated, err := GenerateFromMap(spec)
	if err != nil {
		t.Fatal(err)
	}
	workflow := generated.Workflow
	// The emitted graph validates, and therefore passes the shared phase-shape
	// rules that run.prepare also enforces (GH #66).
	if err := ValidateWorkflow(workflow); err != nil {
		t.Fatalf("ValidateWorkflow: %v", err)
	}
	if err := workflowauthoring.ValidatePhaseShapes(workflow); err != nil {
		t.Fatalf("ValidatePhaseShapes (run.prepare rules): %v", err)
	}
	if workflow["schema_version"] != WorkflowSchemaVersionV11 {
		t.Fatalf("schema_version = %#v", workflow["schema_version"])
	}

	// Exactly the eight declared phases, in order, each naming one phase_synthesis.
	wantPhases := [][2]string{
		{"survey", "survey_synthesis"},
		{"convener_synthesis", "convener_synthesis"},
		{"cross_exam", "cross_exam_synthesis"},
		{"adjudication", "adjudicate"},
		{"revision_synthesis", "revision_synthesis"},
		{"constraint_discharge_review", "discharge_review_synthesis"},
		{"spec_publication", "spec_publication"},
		{"final_review", "final_review_synthesis"},
	}
	phases := listFrom(workflow["phases"])
	if len(phases) != len(wantPhases) {
		t.Fatalf("phase count = %d (%#v)", len(phases), workflow["phases"])
	}
	jobs := jobsByID(workflow["jobs"])
	synthCountByPhase := map[string]int{}
	for _, item := range listFrom(workflow["jobs"]) {
		job := mapFrom(item)
		if job["type"] == "phase_synthesis" {
			synthCountByPhase[fmt.Sprint(job["phase_id"])]++
		}
	}
	for idx, want := range wantPhases {
		phase := mapFrom(phases[idx])
		if phase["id"] != want[0] {
			t.Fatalf("phase[%d].id = %#v, want %q", idx, phase["id"], want[0])
		}
		if phase["synthesis_job_id"] != want[1] {
			t.Fatalf("phase %q synthesis_job_id = %#v, want %q", want[0], phase["synthesis_job_id"], want[1])
		}
		if synthCountByPhase[want[0]] != 1 {
			t.Fatalf("phase %q has %d phase_synthesis jobs, want exactly 1", want[0], synthCountByPhase[want[0]])
		}
		synth := jobs[want[1]]
		if synth["type"] != "phase_synthesis" || synth["phase_id"] != want[0] {
			t.Fatalf("synthesis job %q = %#v", want[1], synth)
		}
	}

	// The adjudication gate is the cycle-aware collaboration_ledger.
	adjudicate := jobs["adjudicate"]
	if adjudicate["role_id"] != "adjudicator" {
		t.Fatalf("adjudicate role = %#v", adjudicate["role_id"])
	}
	ledger := mapFrom(listFrom(adjudicate["expected_artifacts"])[0])
	if ledger["kind"] != "collaboration_ledger" || ledger["logical_name"] != "collaboration_ledger_${cycle}" {
		t.Fatalf("adjudicate ledger = %#v", ledger)
	}

	// The needs_revision cycle re-opens the convener synthesis (bounded by max_cycles).
	cycles := listFrom(workflow["cycles"])
	if len(cycles) != 1 {
		t.Fatalf("cycles = %#v", workflow["cycles"])
	}
	cycle := mapFrom(cycles[0])
	if cycle["from"] != "adjudicate" || cycle["to"] != "convener_draft" || cycle["on_verdict"] != "needs_revision" {
		t.Fatalf("revision cycle = %#v", cycle)
	}
	if maxIterations, _ := intFrom(cycle["max_iterations"]); maxIterations != 2 {
		t.Fatalf("cycle max_iterations = %#v, want 2", cycle["max_iterations"])
	}

	// All six RFC 0098 roles are present; one cross_examiner per default posture.
	roles := mapFrom(workflow["roles"])
	for _, role := range []string{"convener", "cross_examiner", "adjudicator", "revision_convener", "spec_author", "final_reviewer"} {
		if _, ok := roles[role]; !ok {
			t.Fatalf("missing role %q in %#v", role, roles)
		}
	}
	examinerCount := 0
	for id := range jobs {
		if strings.HasPrefix(id, "cross_examiner_") {
			examinerCount++
			targets := listFrom(jobs[id]["interrogation_targets"])
			if len(targets) != 1 {
				t.Fatalf("%s interrogation_targets = %#v, want exactly convener_draft", id, jobs[id]["interrogation_targets"])
			}
			target := mapFrom(targets[0])
			if target["workflow_job_id"] != "convener_draft" || target["required"] != true {
				t.Fatalf("%s interrogation target = %#v, want required convener_draft", id, target)
			}
		}
	}
	if examinerCount != 5 {
		t.Fatalf("cross_examiner count = %d, want 5 (default postures)", examinerCount)
	}
	for _, item := range listFrom(workflow["edges"]) {
		edge := mapFrom(item)
		if edge["from"] == "convener_draft" && strings.HasPrefix(fmt.Sprint(edge["to"]), "cross_examiner_") {
			t.Fatalf("ACE must not add fake convener_draft -> cross_examiner edge: %#v", edge)
		}
	}
	if generated.Metadata["shape_family"] != "collaboration" {
		t.Fatalf("metadata shape_family = %#v", generated.Metadata["shape_family"])
	}
}

func TestAdjudicatedConstraintExtractionUsesCycleAwareLogicalNames(t *testing.T) {
	generated, err := GenerateFromMap(adjudicatedConstraintExtractionGeneratorSpec())
	if err != nil {
		t.Fatal(err)
	}
	jobs := jobsByID(generated.Workflow["jobs"])

	// Every re-publishable artifact inside the revision cycle must carry a
	// ${cycle}-templated logical_name AND path so republish does not collide on
	// the append-only artifacts table (RFC 0098 Acceptance #4 / GH #84).
	cycleScoped := []string{
		"convener_draft", "convener_synthesis", "cross_exam_synthesis",
		"adjudication_intake", "adjudicate", "revision_draft",
		"revision_synthesis", "discharge_review", "discharge_review_synthesis",
	}
	for _, id := range cycleScoped {
		job := jobs[id]
		artifact := mapFrom(listFrom(job["expected_artifacts"])[0])
		logical, _ := artifact["logical_name"].(string)
		artifactPath, _ := artifact["path"].(string)
		if !strings.Contains(logical, "${cycle}") {
			t.Fatalf("job %q logical_name %q is not cycle-templated", id, logical)
		}
		if !strings.Contains(artifactPath, "${cycle}") {
			t.Fatalf("job %q path %q is not cycle-templated", id, artifactPath)
		}
	}

	// Run-once artifacts (outside the revision cycle) keep fixed names.
	for _, id := range []string{"survey_scan", "survey_synthesis", "spec_draft", "spec_publication", "final_discharge_check", "final_review_synthesis"} {
		job := jobs[id]
		logical, _ := mapFrom(listFrom(job["expected_artifacts"])[0])["logical_name"].(string)
		if strings.Contains(logical, "${cycle}") {
			t.Fatalf("run-once job %q logical_name %q should not be cycle-templated", id, logical)
		}
	}
}

func TestAdjudicatedConstraintExtractionRendersFixtureFiles(t *testing.T) {
	generated, err := GenerateFromMap(adjudicatedConstraintExtractionGeneratorSpec())
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]struct{}{}
	for _, f := range generated.Files {
		paths[fmt.Sprint(mapFrom(f)["path"])] = struct{}{}
	}
	for _, want := range []string{
		"workflows/ace/roles/convener.md",
		"workflows/ace/roles/revision_convener.md",
		"workflows/ace/roles/spec_author.md",
		"workflows/ace/roles/final_reviewer.md",
		"workflows/ace/prompts/ace_adjudicate.md",
		"workflows/ace/prompts/ace_final_review.md",
	} {
		if _, ok := paths[want]; !ok {
			t.Fatalf("missing generated file %q in %#v", want, paths)
		}
	}
}

func TestAdjudicatedConstraintExtractionDefaultPosturesOverridable(t *testing.T) {
	spec := adjudicatedConstraintExtractionGeneratorSpec()
	mapFrom(spec["options"])["review_postures"] = []any{"security", "cost"}
	generated, err := GenerateFromMap(spec)
	if err != nil {
		t.Fatal(err)
	}
	jobs := jobsByID(generated.Workflow["jobs"])
	if _, ok := jobs["cross_examiner_1"]; !ok {
		t.Fatalf("missing cross_examiner_1")
	}
	if _, ok := jobs["cross_examiner_3"]; ok {
		t.Fatalf("unexpected cross_examiner_3 with two-posture override: %#v", jobs)
	}
	if !strings.Contains(fmt.Sprint(jobs["cross_examiner_2"]["objective"]), "cost") {
		t.Fatalf("cross_examiner_2 objective = %#v", jobs["cross_examiner_2"]["objective"])
	}
}

func TestGenerateUsesSharedAuthoringLintPayload(t *testing.T) {
	generated := mustGenerate(t, map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            "review",
		"lane_set":         "author_reviewer",
		"workflow_id":      "demo",
		"name":             "Demo",
		"workflow_version": "2026-05-17",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/demo", "allow_dirty": false},
		"scaffold_root":    "workflows/demo",
		"artifact_root":    "striatum/demo",
		"lanes": map[string]any{
			"author":   map[string]any{"adapter": "process", "command": []any{"true"}, "display_model": "codex-gpt-5"},
			"reviewer": map[string]any{"adapter": "process", "command": []any{"true"}, "display_model": "codex-gpt-5.1"},
		},
		"options": map[string]any{},
	})
	warnings, ok := generated.Lint["warnings"].([]map[string]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("lint warnings = %#v", generated.Lint["warnings"])
	}
	if warnings[0]["fingerprint"] == "" {
		t.Fatalf("lint warning missing fingerprint: %#v", warnings[0])
	}
	coverage := generated.Lint["coverage"].(map[string]any)
	if coverage["checks"] == nil {
		t.Fatalf("lint coverage did not come from workflowauthoring: %#v", coverage)
	}
}

func TestGenerateDefaultsInteractiveCodexLanesToAgentLoopPTY(t *testing.T) {
	generated := mustGenerate(t, map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            "code_change",
		"lane_set":         "author_reviewer",
		"workflow_id":      "codex-demo",
		"name":             "Codex Demo",
		"workflow_version": "2026-06-13",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/codex-demo", "allow_dirty": false},
		"scaffold_root":    "workflows/codex-demo",
		"artifact_root":    "striatum/codex-demo",
		"lanes": map[string]any{
			"author":   map[string]any{"adapter": "process", "command": []any{"codex", "--dangerously-bypass-approvals-and-sandbox", "--no-alt-screen"}},
			"reviewer": map[string]any{"adapter": "process", "command": []any{"codex"}},
		},
		"options": map[string]any{},
	})
	lanes := mapFrom(generated.Workflow["lanes"])
	for _, laneID := range []string{"author", "reviewer"} {
		lane := mapFrom(lanes[laneID])
		capabilities := mapFrom(lane["adapter_capabilities"])
		if capabilities["agent_loop"] != true {
			t.Fatalf("lane %s adapter_capabilities = %#v, want agent_loop=true", laneID, capabilities)
		}
		supervision := mapFrom(lane["supervision"])
		if supervision["transport"] != "pty_helper" {
			t.Fatalf("lane %s supervision = %#v, want transport=pty_helper", laneID, supervision)
		}
	}
}

func TestGenerateRefusesCodexExecLane(t *testing.T) {
	_, err := GenerateFromMap(map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            "review",
		"lane_set":         "author_reviewer",
		"workflow_id":      "codex-exec-demo",
		"name":             "Codex Exec Demo",
		"workflow_version": "2026-06-13",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/codex-exec-demo", "allow_dirty": false},
		"scaffold_root":    "workflows/codex-exec-demo",
		"artifact_root":    "striatum/codex-exec-demo",
		"lanes": map[string]any{
			"author":   map[string]any{"adapter": "process", "command": []any{"codex", "exec", "-"}},
			"reviewer": map[string]any{"adapter": "process", "command": []any{"codex"}},
		},
		"options": map[string]any{},
	})
	if err == nil {
		t.Fatal("GenerateFromMap with codex exec lane: expected error")
	}
	var genError *Error
	if !errors.As(err, &genError) {
		t.Fatalf("error type = %T, %v", err, err)
	}
	if genError.FieldPath != "spec.lanes.author.command" {
		t.Fatalf("field path = %q, want spec.lanes.author.command; err = %v", genError.FieldPath, err)
	}
	if !strings.Contains(err.Error(), "codex exec") {
		t.Fatalf("error should name codex exec, got %v", err)
	}
}

func TestUpgradeAddPhasesPreviewWritesNothing(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "workflow.json")
	if err := os.WriteFile(path, mustJSON(t, parallelGroupWorkflow()), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Upgrade(context.Background(), workflowUpgradeQueryer{}, "repo_1", repo, UpgradeOptions{Path: "workflow.json", AddPhases: true})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "would_update" || result["mode"] != "add_phases" {
		t.Fatalf("unexpected result = %#v", result)
	}
	phases, ok := result["phases_added"].([]map[string]any)
	if !ok || len(phases) != 2 {
		t.Fatalf("phases = %#v", result["phases_added"])
	}
	if phases[0]["id"] != "phase_design" || phases[0]["synthesis_job_id"] != "phase_design__synthesis" {
		t.Fatalf("first phase = %#v", phases[0])
	}
	if phases[1]["id"] != "phase_build" || phases[1]["synthesis_job_id"] != "phase_build__synthesis" {
		t.Fatalf("second phase = %#v", phases[1])
	}
	if !containsMap(result["jobs_relabelled"], map[string]any{"job_id": "design_python", "phase_id": "phase_design"}) {
		t.Fatalf("jobs_relabelled = %#v", result["jobs_relabelled"])
	}
	current, err := os.ReadFile(path)
	if err == nil {
		if string(current) != string(snapshot) {
			t.Fatal("preview rewrote workflow")
		}
	} else {
		t.Fatal(err)
	}
}

func TestUpgradeAddPhasesApplyRewritesToV11(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "workflow.json")
	if err := os.WriteFile(path, mustJSON(t, parallelGroupWorkflow()), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Upgrade(context.Background(), workflowUpgradeQueryer{}, "repo_1", repo, UpgradeOptions{Path: "workflow.json", AddPhases: true, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "updated" {
		t.Fatalf("status = %#v", result["status"])
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk["schema_version"] != "striatum.workflow.v1.1" {
		t.Fatalf("schema_version = %#v", onDisk["schema_version"])
	}
	jobs := map[string]map[string]any{}
	for _, item := range listFrom(onDisk["jobs"]) {
		job := mapFrom(item)
		jobs[fmt.Sprint(job["id"])] = job
	}
	if jobs["design_python"]["phase_id"] != "phase_design" || jobs["build_python"]["phase_id"] != "phase_build" {
		t.Fatalf("phase ids = %#v %#v", jobs["design_python"], jobs["build_python"])
	}
	if jobs["phase_design__synthesis"]["type"] != "phase_synthesis" || jobs["phase_build__synthesis"]["type"] != "phase_synthesis" {
		t.Fatalf("synthesis jobs = %#v %#v", jobs["phase_design__synthesis"], jobs["phase_build__synthesis"])
	}
	edges := map[[2]string]struct{}{}
	for _, item := range listFrom(onDisk["edges"]) {
		edge := mapFrom(item)
		edges[[2]string{fmt.Sprint(edge["from"]), fmt.Sprint(edge["to"])}] = struct{}{}
	}
	for _, edge := range [][2]string{{"design_python", "phase_design__synthesis"}, {"phase_design__synthesis", "build_python"}, {"build_python", "phase_build__synthesis"}} {
		if _, ok := edges[edge]; !ok {
			t.Fatalf("missing edge %v in %#v", edge, edges)
		}
	}
}

func TestUpgradeAddPhasesPreviewReportsRunningRuns(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "workflow.json")
	if err := os.WriteFile(path, mustJSON(t, parallelGroupWorkflow()), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Upgrade(context.Background(), workflowUpgradeQueryer{running: []string{"run_active"}}, "repo_1", repo, UpgradeOptions{Path: "workflow.json", AddPhases: true})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "would_refuse_running" {
		t.Fatalf("result = %#v", result)
	}
}

type workflowUpgradeQueryer struct {
	running []string
}

func (q workflowUpgradeQueryer) Query(context.Context, string, ...any) (pgx.Rows, error) {
	rows := []string{}
	rows = append(rows, q.running...)
	return &workflowUpgradeRows{rows: rows, index: -1}, nil
}

type workflowUpgradeRows struct {
	rows  []string
	index int
}

func (r *workflowUpgradeRows) Close()     {}
func (r *workflowUpgradeRows) Err() error { return nil }
func (r *workflowUpgradeRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}
func (r *workflowUpgradeRows) FieldDescriptions() []pgconn.FieldDescription {
	return []pgconn.FieldDescription{{Name: "run_id"}}
}
func (r *workflowUpgradeRows) Next() bool {
	r.index++
	return r.index < len(r.rows)
}
func (r *workflowUpgradeRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("expected one scan destination")
	}
	ptr, ok := dest[0].(*string)
	if !ok {
		return errors.New("expected string scan destination")
	}
	*ptr = r.rows[r.index]
	return nil
}
func (r *workflowUpgradeRows) Values() ([]any, error) {
	if r.index < 0 || r.index >= len(r.rows) {
		return nil, errors.New("row index out of range")
	}
	return []any{r.rows[r.index]}, nil
}
func (r *workflowUpgradeRows) RawValues() [][]byte { return nil }
func (r *workflowUpgradeRows) Conn() *pgx.Conn     { return nil }

var _ pgx.Rows = (*workflowUpgradeRows)(nil)

func mustGenerate(t *testing.T, spec map[string]any) Generated {
	t.Helper()
	generated, err := GenerateFromMap(spec)
	if err != nil {
		t.Fatal(err)
	}
	return generated
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(body, '\n')
}

func parallelGroupWorkflow() map[string]any {
	workflow := map[string]any{
		"schema_version":   "striatum.workflow.v1",
		"workflow_id":      "demo",
		"workflow_version": "2026-05-17",
		"name":             "Demo",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/demo", "allow_dirty": false},
		"coordinator":      map[string]any{"role_id": "author", "lane_id": "claude_code"},
		"lanes": map[string]any{
			"claude_code": map[string]any{
				"adapter":       "process",
				"display_model": "Claude",
				"command":       []string{"claude", "--print"},
				"capabilities":  []string{"write"},
			},
		},
		"roles":        map[string]any{"author": map[string]any{"definition_path": "roles/author.md"}, "reviewer": map[string]any{"definition_path": "roles/reviewer.md"}},
		"context_docs": []any{},
		"parallelism":  map[string]any{"mode": "declared", "max_active_jobs": 1, "require_disjoint_write_scopes": true},
		"jobs": []any{
			map[string]any{
				"id":             "design_python",
				"type":           "draft",
				"title":          "Python Design",
				"role_id":        "author",
				"lane_id":        "claude_code",
				"parallel_group": "design_python",
				"objective":      "Draft design.",
				"task_prompt":    map[string]any{"path": "prompts/design.md"},
				"write_scope":    map[string]any{"mode": "repo_write", "repo_write": true, "allowed_paths": []string{"scratch/design/"}, "forbidden_paths": []string{".striatum/"}},
				"expected_artifacts": []any{
					map[string]any{"logical_name": "design", "kind": "handoff", "path": "scratch/design/DESIGN.md", "required": true},
				},
			},
			map[string]any{
				"id":             "build_python",
				"type":           "build",
				"title":          "Python Build",
				"role_id":        "author",
				"lane_id":        "claude_code",
				"parallel_group": "build_python",
				"objective":      "Build implementation.",
				"task_prompt":    map[string]any{"path": "prompts/build.md"},
				"write_scope":    map[string]any{"mode": "repo_write", "repo_write": true, "allowed_paths": []string{"scratch/build/"}, "forbidden_paths": []string{".striatum/"}},
				"expected_artifacts": []any{
					map[string]any{"logical_name": "build", "kind": "handoff", "path": "scratch/build/BUILD.md", "required": true},
				},
			},
		},
		"edges":  []any{map[string]any{"from": "design_python", "to": "build_python", "on": "completed"}},
		"cycles": []any{},
	}
	return workflow
}

func multiPhaseGeneratorSpec() map[string]any {
	return map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            "multi_phase",
		"lane_set":         "author_reviewer",
		"workflow_id":      "multi_phase-test",
		"name":             "multi_phase test",
		"workflow_version": "2026-05-12",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/multi_phase", "allow_dirty": false},
		"scaffold_root":    "workflows/multi_phase",
		"artifact_root":    "striatum/multi_phase",
		"lanes": map[string]any{
			"author":   map[string]any{"command": []any{"author", "run"}, "display_model": "Author"},
			"reviewer": map[string]any{"command": []any{"reviewer", "run"}, "display_model": "Reviewer"},
		},
		"options": map[string]any{
			"phases": []any{
				map[string]any{
					"id":          "phase_1_design",
					"name":        "Design",
					"description": "Parallel design tracks",
					"color":       "#6b7280",
					"tracks": []any{
						map[string]any{"id": "python", "shape": "minimal", "lane_id": "author"},
						map[string]any{"id": "docs", "shape": "review", "lane_id": "author"},
					},
					"synthesis_lane_id": "reviewer",
				},
				map[string]any{
					"id":   "phase_2_build",
					"name": "Build",
					"tracks": []any{
						map[string]any{"id": "python", "shape": "minimal", "lane_id": "author"},
						map[string]any{"id": "docs", "shape": "minimal", "lane_id": "author"},
					},
					"synthesis_lane_id": "reviewer",
				},
			},
		},
	}
}

func implementationPanelGeneratorSpec() map[string]any {
	return map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            "implementation_panel",
		"lane_set":         "multi_review",
		"workflow_id":      "implementation_panel-test",
		"name":             "implementation_panel test",
		"workflow_version": "2026-05-12",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/implementation_panel", "allow_dirty": false},
		"scaffold_root":    "workflows/implementation_panel",
		"artifact_root":    "striatum/implementation_panel",
		"lanes": map[string]any{
			"author":     map[string]any{"command": []any{"author", "run"}, "display_model": "Author"},
			"reviewer_1": map[string]any{"command": []any{"reviewer1", "run"}, "display_model": "Reviewer 1"},
			"reviewer_2": map[string]any{"command": []any{"reviewer2", "run"}, "display_model": "Reviewer 2"},
		},
		"options": map[string]any{
			"role_packs":      []any{"implementation_panel_roles"},
			"adversary_packs": []any{"operator_ergonomics"},
			"proposal_count":  2,
		},
	}
}

func collaborationGeneratorSpec(shape string) map[string]any {
	return map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            shape,
		"lane_set":         "multi_review",
		"workflow_id":      shape + "-test",
		"name":             shape + " test",
		"workflow_version": "2026-05-29",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/" + shape, "allow_dirty": false},
		"scaffold_root":    "workflows/" + shape,
		"artifact_root":    "striatum/" + shape,
		"lanes": map[string]any{
			"author":     map[string]any{"command": []any{"author", "run"}, "display_model": "Claude Opus"},
			"reviewer_1": map[string]any{"command": []any{"reviewer1", "run"}, "display_model": "Codex GPT-5.5"},
			"reviewer_2": map[string]any{"command": []any{"reviewer2", "run"}, "display_model": "Agy Gemini 2.5"},
			"reviewer_3": map[string]any{"command": []any{"reviewer3", "run"}, "display_model": "Codex GPT-5.4"},
		},
		"options": map[string]any{
			"topic":               "substance gate",
			"max_dialog_rounds":   3,
			"max_revision_cycles": 1,
		},
	}
}

func adjudicatedConstraintExtractionGeneratorSpec() map[string]any {
	return map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            "adjudicated_constraint_extraction",
		"lane_set":         "multi_review",
		"workflow_id":      "ace-test",
		"name":             "ace test",
		"workflow_version": "2026-05-30",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/ace", "allow_dirty": false},
		"scaffold_root":    "workflows/ace",
		"artifact_root":    "striatum/ace",
		"lanes": map[string]any{
			"author":     map[string]any{"command": []any{"author", "run"}, "display_model": "Claude Opus"},
			"reviewer_1": map[string]any{"command": []any{"reviewer1", "run"}, "display_model": "Codex GPT-5.5"},
			"reviewer_2": map[string]any{"command": []any{"reviewer2", "run"}, "display_model": "Agy Gemini 2.5"},
			"reviewer_3": map[string]any{"command": []any{"reviewer3", "run"}, "display_model": "Codex GPT-5.4"},
		},
		"options": map[string]any{
			"topic":               "constraint-extraction design",
			"max_revision_cycles": 2,
		},
	}
}

func containsMap(value any, expected map[string]any) bool {
	for _, item := range listFrom(value) {
		actual := mapFrom(item)
		matches := true
		for key, expectedValue := range expected {
			if actual[key] != expectedValue {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func jobsByID(value any) map[string]map[string]any {
	jobs := map[string]map[string]any{}
	for _, item := range listFrom(value) {
		job := mapFrom(item)
		jobs[fmt.Sprint(job["id"])] = job
	}
	return jobs
}

func hasEdge(value any, from, to string) bool {
	for _, item := range listFrom(value) {
		edge := mapFrom(item)
		if edge["from"] == from && edge["to"] == to {
			return true
		}
	}
	return false
}

func collaborationStructuralGraph(workflow map[string]any) map[string]any {
	jobs := []map[string]any{}
	for _, item := range listFrom(workflow["jobs"]) {
		job := mapFrom(item)
		artifactKinds := []string{}
		for _, artifactItem := range listFrom(job["expected_artifacts"]) {
			artifact := mapFrom(artifactItem)
			artifactKinds = append(artifactKinds, fmt.Sprint(artifact["kind"]))
		}
		sort.Strings(artifactKinds)
		freshSessionRequired, _ := job["fresh_session_required"].(bool)
		jobs = append(jobs, map[string]any{
			"id":                      canonicalCollaborationNodeID(fmt.Sprint(job["id"])),
			"type":                    fmt.Sprint(job["type"]),
			"lane_id":                 fmt.Sprint(job["lane_id"]),
			"phase_id":                fmt.Sprint(job["phase_id"]),
			"expected_artifact_kinds": artifactKinds,
			"fresh_session_required":  freshSessionRequired,
		})
	}
	sort.Slice(jobs, func(left, right int) bool {
		return fmt.Sprint(jobs[left]["id"]) < fmt.Sprint(jobs[right]["id"])
	})

	edges := []map[string]any{}
	for _, item := range listFrom(workflow["edges"]) {
		edge := mapFrom(item)
		edges = append(edges, map[string]any{
			"from": canonicalCollaborationNodeID(fmt.Sprint(edge["from"])),
			"to":   canonicalCollaborationNodeID(fmt.Sprint(edge["to"])),
			"on":   fmt.Sprint(edge["on"]),
		})
	}
	sort.Slice(edges, func(left, right int) bool {
		leftKey := fmt.Sprintf("%s>%s>%s", edges[left]["from"], edges[left]["to"], edges[left]["on"])
		rightKey := fmt.Sprintf("%s>%s>%s", edges[right]["from"], edges[right]["to"], edges[right]["on"])
		return leftKey < rightKey
	})

	cycles := []map[string]any{}
	for _, item := range listFrom(workflow["cycles"]) {
		cycle := mapFrom(item)
		allowSameLane, _ := cycle["allow_same_lane"].(bool)
		allowSameModel, _ := cycle["allow_same_model"].(bool)
		cycles = append(cycles, map[string]any{
			"from":             canonicalCollaborationNodeID(fmt.Sprint(cycle["from"])),
			"to":               canonicalCollaborationNodeID(fmt.Sprint(cycle["to"])),
			"on_verdict":       fmt.Sprint(cycle["on_verdict"]),
			"max_iterations":   cycle["max_iterations"],
			"allow_same_lane":  allowSameLane,
			"allow_same_model": allowSameModel,
		})
	}
	sort.Slice(cycles, func(left, right int) bool {
		leftKey := fmt.Sprintf("%s>%s>%s", cycles[left]["from"], cycles[left]["to"], cycles[left]["on_verdict"])
		rightKey := fmt.Sprintf("%s>%s>%s", cycles[right]["from"], cycles[right]["to"], cycles[right]["on_verdict"])
		return leftKey < rightKey
	})

	phases := []map[string]any{}
	for _, item := range listFrom(workflow["phases"]) {
		phase := mapFrom(item)
		phases = append(phases, map[string]any{
			"id":               fmt.Sprint(phase["id"]),
			"synthesis_job_id": canonicalCollaborationNodeID(fmt.Sprint(phase["synthesis_job_id"])),
		})
	}
	sort.Slice(phases, func(left, right int) bool {
		return fmt.Sprint(phases[left]["id"]) < fmt.Sprint(phases[right]["id"])
	})

	return map[string]any{
		"schema_version": fmt.Sprint(workflow["schema_version"]),
		"jobs":           jobs,
		"edges":          edges,
		"cycles":         cycles,
		"phases":         phases,
	}
}

func canonicalCollaborationNodeID(id string) string {
	switch id {
	case "holder", "author_draft":
		return "source"
	case "adjudicate", "commit_proposal", "final_summary", "scribe_note":
		return id
	}
	for _, prefix := range []string{"falsifier_", "cross_examiner_"} {
		if strings.HasPrefix(id, prefix) {
			return "challenger_" + strings.TrimPrefix(id, prefix)
		}
	}
	return id
}
